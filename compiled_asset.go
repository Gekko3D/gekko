package gekko

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

// CompiledAssetCompileResult acknowledges a complete compiled shipping closure.
// Counts describe unique immutable files, rather than individual references.
type CompiledAssetCompileResult struct {
	HeaderInfo                              voxelcodec.Info
	HeaderWrote                             bool
	ShapesWritten, ShapesReused             int
	DependenciesWritten, DependenciesReused int
}

// CompiledAssetCompileOptions explicitly enables optional shipping derivatives.
type CompiledAssetCompileOptions struct {
	EnableLOD2 bool
}

// CompiledAssetCompileDetailedResult adds unique derivative file counts without
// changing the existing result's positional source compatibility.
type CompiledAssetCompileDetailedResult struct {
	CompiledAssetCompileResult
	LODsWritten, LODsReused int
}

type compiledAssetFileKind uint8

const (
	compiledAssetDependencyFile compiledAssetFileKind = iota
	compiledAssetShapeFile
	compiledAssetLODFile
	compiledAssetModelFile
)

type compiledAssetFile struct {
	relative string
	data     []byte
	kind     compiledAssetFileKind
}

// CompileAuthoredAsset compiles inline shapes and groups into an explicit header
// and immutable geometry/dependency files. Authoring inputs must remain stable
// during compilation. The caller retains ownership of an explicit codec.
func CompileAuthoredAsset(inputPath, outputPath string, codec *voxelcodec.Codec) (CompiledAssetCompileResult, error) {
	result, err := CompileAuthoredAssetWithOptions(inputPath, outputPath, codec, CompiledAssetCompileOptions{})
	if err != nil {
		return CompiledAssetCompileResult{}, err
	}
	return result.CompiledAssetCompileResult, nil
}

// CompileAuthoredAssetWithOptions emits optional source-bound 2x LOD frames.
// Failures return a zero result; the caller retains explicit codec ownership.
func CompileAuthoredAssetWithOptions(inputPath, outputPath string, codec *voxelcodec.Codec, options CompiledAssetCompileOptions) (CompiledAssetCompileDetailedResult, error) {
	result, err := compileAuthoredAsset(inputPath, outputPath, codec, options)
	if err != nil {
		return CompiledAssetCompileDetailedResult{}, err
	}
	return result, nil
}

// CompileAuthoredCollapsedAssetWithOptions explicitly permits inline static
// collapse in schema 3. Ordinary inputs retain their existing schema and bytes.
// Model inputs remain unsupported because canonical frames omit raw samples.
func CompileAuthoredCollapsedAssetWithOptions(inputPath, outputPath string, codec *voxelcodec.Codec, options CompiledAssetCompileOptions) (CompiledAssetCompileDetailedResult, error) {
	result, err := compileAuthoredAssetClosureWithCollapse(inputPath, outputPath, codec, options, false, true)
	if err != nil {
		return CompiledAssetCompileDetailedResult{}, err
	}
	return result.CompiledAssetCompileDetailedResult, nil
}

func compileAuthoredAsset(inputPath, outputPath string, codec *voxelcodec.Codec, options CompiledAssetCompileOptions) (CompiledAssetCompileDetailedResult, error) {
	result, err := compileAuthoredAssetClosure(inputPath, outputPath, codec, options, false)
	return result.CompiledAssetCompileDetailedResult, err
}

// One pipeline owns dependency rewriting, preflight and durable publication for
// both explicit shipping formats. Legacy wrappers retain their result and bytes.
func compileAuthoredAssetClosure(inputPath, outputPath string, codec *voxelcodec.Codec, options CompiledAssetCompileOptions, includeModels bool) (CompiledAssetModelCompileResult, error) {
	return compileAuthoredAssetClosureWithCollapse(inputPath, outputPath, codec, options, includeModels, false)
}

func compileAuthoredAssetClosureWithCollapse(inputPath, outputPath string, codec *voxelcodec.Codec, options CompiledAssetCompileOptions, includeModels, allowCollapse bool) (CompiledAssetModelCompileResult, error) {
	var result CompiledAssetModelCompileResult
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return result, err
	}
	if !utf8.Valid(raw) {
		return result, fmt.Errorf("authoring JSON is not valid UTF-8")
	}
	var asset content.AssetDef
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&asset); err != nil {
		return result, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return result, fmt.Errorf("authoring JSON has trailing data: %v", err)
	}
	if err := validateCompiledSourceKindsWithCollapse(&asset, includeModels, allowCollapse); err != nil {
		return result, err
	}
	content.NormalizeAssetDef(&asset)
	if validation := content.ValidateAsset(&asset, content.AssetValidationOptions{DocumentPath: inputPath}); validation.HasErrors() {
		return result, fmt.Errorf("invalid authoring asset: %s", validation.Error())
	}
	if _, err := content.ResolveAssetAnimations(&asset, inputPath); err != nil {
		return result, err
	}
	sources := []string{inputPath}
	files := map[string]compiledAssetFile{}
	add := func(data []byte, suffix string, kind compiledAssetFileKind) string {
		hash := sha256.Sum256(data)
		folder := "dependencies"
		if kind == compiledAssetShapeFile {
			folder = "shapes"
		} else if kind == compiledAssetLODFile {
			folder = "lods"
		} else if kind == compiledAssetModelFile {
			folder = "models"
		}
		relative := folder + "/" + hex.EncodeToString(hash[:]) + suffix
		files[relative] = compiledAssetFile{relative, data, kind}
		return relative
	}
	header := &content.CompiledAssetHeaderDef{SchemaVersion: content.CurrentCompiledAssetHeaderSchemaVersion, CompilerVersion: content.CurrentCompiledAssetCompilerVersion, Asset: &asset}
	if options.EnableLOD2 {
		header.SchemaVersion = content.CompiledAssetLODHeaderSchemaVersion
		header.CompilerVersion = content.CompiledAssetLODHeaderCompilerVersion
	}
	if asset.Runtime != nil && asset.Runtime.CollapseVoxelParts {
		header.SchemaVersion = content.CompiledAssetCollapseHeaderSchemaVersion
		header.CompilerVersion = content.CompiledAssetCollapseHeaderCompilerVersion
	}
	var modelHeader *content.CompiledAssetModelHeaderDef
	var modelPalettes map[string]struct{}
	if includeModels {
		modelHeader = &content.CompiledAssetModelHeaderDef{SchemaVersion: content.CurrentCompiledAssetModelHeaderSchemaVersion, CompilerVersion: content.CurrentCompiledAssetModelHeaderCompilerVersion, Asset: &asset}
		modelPalettes = make(map[string]struct{})
	}
	for i := range asset.Parts {
		part := &asset.Parts[i]
		if includeModels && part.Source.Kind != content.AssetSourceKindVoxelShape && part.Source.Kind != content.AssetSourceKindGroup {
			dependencies, err := compileAssetModelPartIntoHeader(&asset, *part, inputPath, codec, modelHeader, modelPalettes, add)
			if err != nil {
				return result, err
			}
			sources = append(sources, dependencies...)
			continue
		}
		if part.Source.Kind != content.AssetSourceKindVoxelShape {
			continue
		}
		shape, err := compileAssetPartShape(*part)
		if err != nil {
			return result, err
		}
		data, info, err := content.EncodeCompiledAssetShape(shape, codec)
		if err != nil {
			return result, err
		}
		base, _, err := content.CompiledAssetShapeBaseIdentity(shape, codec)
		if err != nil {
			return result, err
		}
		relative := add(data, ".gkshape", compiledAssetShapeFile)
		header.Shapes = append(header.Shapes, content.CompiledAssetShapeRefDef{PartID: part.ID, Path: relative, ContentID: info.ContentID, BaseIdentity: base, EncodedBytes: info.EncodedBytes, DecodedBytes: info.DecodedBytes})
		if options.EnableLOD2 {
			lod, err := compileAssetPartLOD(&asset, *part, shape, info.ContentID)
			if err != nil {
				return result, err
			}
			if lod != nil {
				data, info, err := content.EncodeCompiledAssetLOD(lod, codec)
				if err != nil {
					return result, err
				}
				relative := add(data, ".gklod", compiledAssetLODFile)
				header.LODs = append(header.LODs, content.CompiledAssetLODRefDef{PartID: part.ID, Path: relative, ContentID: info.ContentID, SourceContentID: lod.SourceContentID, EncodedBytes: info.EncodedBytes, DecodedBytes: info.DecodedBytes, Factor: lod.Factor, ReductionVersion: lod.ReductionVersion})
			}
		}
		part.Source.VoxelShape.Voxels = nil
	}
	for i, ref := range asset.AnimationSetPaths {
		source := content.ResolveDocumentPath(ref, inputPath)
		sources = append(sources, source)
		data, err := os.ReadFile(source)
		if err != nil {
			return result, err
		}
		set, err := content.LoadAnimationSet(source)
		if err != nil {
			return result, err
		}
		if set.RigPath != "" {
			rigSource := content.ResolveDocumentPath(set.RigPath, source)
			sources = append(sources, rigSource)
			rigData, err := os.ReadFile(rigSource)
			if err != nil {
				return result, err
			}
			set.RigPath = filepath.Base(add(rigData, ".gkrig", compiledAssetDependencyFile))
			data, err = json.MarshalIndent(set, "", "  ")
			if err != nil {
				return result, err
			}
		}
		asset.AnimationSetPaths[i] = add(data, ".gkanim", compiledAssetDependencyFile)
	}
	for i := range asset.Emitters {
		emitter := &asset.Emitters[i].Emitter
		if emitter.TexturePath == "" {
			continue
		}
		source := content.ResolveDocumentPath(emitter.TexturePath, inputPath)
		sources = append(sources, source)
		data, err := os.ReadFile(source)
		if err != nil {
			return result, err
		}
		emitter.TexturePath = add(data, ".texture", compiledAssetDependencyFile)
	}
	var headerData []byte
	var info voxelcodec.Info
	if includeModels {
		modelHeader.Shapes, modelHeader.LODs = header.Shapes, header.LODs
		headerData, info, err = content.EncodeCompiledAssetModelHeader(modelHeader, codec)
	} else {
		headerData, info, err = content.EncodeCompiledAssetHeader(header, codec)
	}
	if err != nil {
		return result, err
	}
	if err := compiledAssetOutputSafe(outputPath, sources); err != nil {
		return result, err
	}
	dir := filepath.Dir(outputPath)
	ordered := make([]string, 0, len(files))
	for key := range files {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	// Verify all existing immutable entries before publishing any new dependency.
	for _, key := range ordered {
		if err := compiledAssetDirectory(filepath.Join(dir, filepath.Dir(key)), false); err != nil {
			return result, err
		}
		if _, err := compiledAssetExisting(filepath.Join(dir, filepath.FromSlash(key)), files[key].data); err != nil {
			return result, err
		}
	}
	for _, key := range ordered {
		file := files[key]
		target := filepath.Join(dir, filepath.FromSlash(key))
		if err := compiledAssetDirectory(filepath.Dir(target), true); err != nil {
			return result, err
		}
		wrote, err := publishCompiledAssetFile(target, file.data)
		if err != nil {
			return result, err
		}
		switch file.kind {
		case compiledAssetShapeFile:
			if wrote {
				result.ShapesWritten++
			} else {
				result.ShapesReused++
			}
		case compiledAssetLODFile:
			if wrote {
				result.LODsWritten++
			} else {
				result.LODsReused++
			}
		case compiledAssetModelFile:
			if wrote {
				result.ModelsWritten++
			} else {
				result.ModelsReused++
			}
		case compiledAssetDependencyFile:
			if wrote {
				result.DependenciesWritten++
			} else {
				result.DependenciesReused++
			}
		}
	}
	// An identical header is a no-op, preserving modification time.
	matching, err := compiledAssetHeaderMatches(outputPath, headerData)
	if err != nil {
		return result, err
	}
	if matching {
		if err := syncCompiledAssetFile(outputPath); err != nil {
			return result, err
		}
	} else {
		if includeModels {
			_, err = content.SaveCompiledAssetModelHeader(outputPath, modelHeader, codec)
		} else {
			_, err = content.SaveCompiledAssetHeader(outputPath, header, codec)
		}
		if err != nil {
			return result, err
		}
		result.HeaderWrote = true
	}
	result.HeaderInfo = info
	return result, nil
}

func validateCompiledSource(asset *content.AssetDef) error {
	return validateCompiledSourceKinds(asset, false)
}

func validateCompiledSourceKinds(asset *content.AssetDef, includeModels bool) error {
	return validateCompiledSourceKindsWithCollapse(asset, includeModels, false)
}

func validateCompiledSourceKindsWithCollapse(asset *content.AssetDef, includeModels, allowCollapse bool) error {
	valid := func(id string) bool { return strings.TrimSpace(id) != "" && utf8.ValidString(id) }
	if asset.SchemaVersion != 4 || !valid(asset.ID) || asset.Runtime != nil && asset.Runtime.CollapseVoxelParts && (!allowCollapse || includeModels) {
		return fmt.Errorf("unsupported authoring schema, missing ID or static collapse")
	}
	for _, m := range asset.Materials {
		if !valid(m.ID) {
			return fmt.Errorf("material requires persisted ID")
		}
	}
	for _, p := range asset.Parts {
		if !valid(p.ID) {
			return fmt.Errorf("part requires persisted ID")
		}
		switch p.Source.Kind {
		case content.AssetSourceKindVoxelShape:
			if p.Source.VoxelShape == nil {
				return fmt.Errorf("missing inline voxel shape")
			}
		case content.AssetSourceKindGroup:
			if p.Source.VoxelShape != nil {
				return fmt.Errorf("group carries inline geometry")
			}
		case content.AssetSourceKindProceduralPrimitive, content.AssetSourceKindVoxModel, content.AssetSourceKindVoxSceneNode:
			if !includeModels {
				return fmt.Errorf("unsupported compiled source %q", p.Source.Kind)
			}
		default:
			return fmt.Errorf("unsupported compiled source %q", p.Source.Kind)
		}
	}
	for _, p := range asset.Lights {
		if !valid(p.ID) {
			return fmt.Errorf("light requires persisted ID")
		}
	}
	for _, p := range asset.Emitters {
		if !valid(p.ID) {
			return fmt.Errorf("emitter requires persisted ID")
		}
	}
	for _, p := range asset.Markers {
		if !valid(p.ID) {
			return fmt.Errorf("marker requires persisted ID")
		}
	}
	if asset.Skeleton != nil {
		for _, bone := range asset.Skeleton.Bones {
			if !valid(bone.ID) || !valid(bone.JointID) {
				return fmt.Errorf("bone and joint require persisted IDs")
			}
		}
	}
	return nil
}

func compileAssetPartShape(part content.AssetPartDef) (*content.CompiledAssetShapeDef, error) {
	geometry := buildAuthoredVoxelShapeMap(part)
	bricks, err := compileAssetPrimaryBricks(geometry)
	if err != nil {
		return nil, err
	}
	return &content.CompiledAssetShapeDef{SchemaVersion: content.CurrentCompiledAssetShapeSchemaVersion, Lattice: authoredVoxelShapeLattice(part.VoxelResolution), Bricks: bricks}, nil
}

func compiledAssetOutputSafe(output string, sources []string) error {
	target, err := filepath.Abs(output)
	if err != nil {
		return err
	}
	targetInfo, err := os.Stat(target)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for _, source := range sources {
		absolute, err := filepath.Abs(source)
		if err != nil {
			return err
		}
		info, err := os.Stat(absolute)
		if err != nil {
			return err
		}
		canonical, err := filepath.EvalSymlinks(absolute)
		if err != nil {
			return err
		}
		resolved, resolveErr := filepath.EvalSymlinks(target)
		if target == absolute || target == canonical || resolveErr == nil && resolved == canonical || targetInfo != nil && os.SameFile(targetInfo, info) {
			return fmt.Errorf("compiled output aliases an authoring input")
		}
	}
	if info, err := os.Lstat(target); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("compiled header target is not a regular file")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func compiledAssetDirectory(dir string, create bool) error {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) && create {
		if err = os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		info, err = os.Lstat(dir)
	}
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("compiled artifact directory is not an ordinary directory")
	}
	return nil
}

func compiledAssetExisting(target string, data []byte) (bool, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != int64(len(data)) {
		return false, fmt.Errorf("compiled immutable artifact conflicts: %s", target)
	}
	actual, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	if !bytes.Equal(actual, data) {
		return false, fmt.Errorf("compiled immutable artifact bytes conflict: %s", target)
	}
	return true, nil
}

func publishCompiledAssetFile(target string, data []byte) (bool, error) {
	if exists, err := compiledAssetExisting(target, data); exists || err != nil {
		if err != nil {
			return false, err
		}
		return false, syncCompiledAssetFile(target)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".compiled-tmp-")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	if err := tmp.Chmod(0644); err != nil {
		return false, err
	}
	if _, err := tmp.Write(data); err != nil {
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Link(tmp.Name(), target); err != nil {
		if os.IsExist(err) {
			exists, err := compiledAssetExisting(target, data)
			if err != nil {
				return false, err
			}
			if exists {
				return false, syncCompiledAssetFile(target)
			}
		}
		return false, err
	}
	if err := syncCompiledAssetDirectory(filepath.Dir(target)); err != nil {
		return false, err
	}
	return true, nil
}

func compiledAssetHeaderMatches(target string, data []byte) (bool, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("compiled header target is not a regular file")
	}
	if info.Size() != int64(len(data)) {
		return false, nil
	}
	actual, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	return bytes.Equal(actual, data), nil
}

// Reuse must recover durability from an earlier failed publication or a matching
// compiler whose directory sync is still pending. Sync leaves modification time intact.
func syncCompiledAssetFile(target string) error {
	file, err := os.Open(target)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return syncCompiledAssetDirectory(filepath.Dir(target))
}

func syncCompiledAssetDirectory(directory string) error {
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	return closeErr
}
