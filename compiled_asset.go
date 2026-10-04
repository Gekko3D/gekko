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

type compiledAssetFile struct {
	relative string
	data     []byte
	shape    bool
}

// CompileAuthoredAsset compiles inline shapes and groups into an explicit header
// and immutable geometry/dependency files. Authoring inputs must remain stable
// during compilation. The caller retains ownership of an explicit codec.
func CompileAuthoredAsset(inputPath, outputPath string, codec *voxelcodec.Codec) (CompiledAssetCompileResult, error) {
	result, err := compileAuthoredAsset(inputPath, outputPath, codec)
	if err != nil {
		return CompiledAssetCompileResult{}, err
	}
	return result, nil
}

func compileAuthoredAsset(inputPath, outputPath string, codec *voxelcodec.Codec) (CompiledAssetCompileResult, error) {
	var result CompiledAssetCompileResult
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
	if err := validateCompiledSource(&asset); err != nil {
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
	add := func(data []byte, suffix string, shape bool) string {
		hash := sha256.Sum256(data)
		folder := "dependencies"
		if shape {
			folder = "shapes"
		}
		relative := folder + "/" + hex.EncodeToString(hash[:]) + suffix
		files[relative] = compiledAssetFile{relative, data, shape}
		return relative
	}
	header := &content.CompiledAssetHeaderDef{SchemaVersion: content.CurrentCompiledAssetHeaderSchemaVersion, CompilerVersion: content.CurrentCompiledAssetCompilerVersion, Asset: &asset}
	for i := range asset.Parts {
		part := &asset.Parts[i]
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
		relative := add(data, ".gkshape", true)
		header.Shapes = append(header.Shapes, content.CompiledAssetShapeRefDef{PartID: part.ID, Path: relative, ContentID: info.ContentID, BaseIdentity: base, EncodedBytes: info.EncodedBytes, DecodedBytes: info.DecodedBytes})
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
			set.RigPath = filepath.Base(add(rigData, ".gkrig", false))
			data, err = json.MarshalIndent(set, "", "  ")
			if err != nil {
				return result, err
			}
		}
		asset.AnimationSetPaths[i] = add(data, ".gkanim", false)
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
		emitter.TexturePath = add(data, ".texture", false)
	}
	headerData, info, err := content.EncodeCompiledAssetHeader(header, codec)
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
		if file.shape {
			if wrote {
				result.ShapesWritten++
			} else {
				result.ShapesReused++
			}
		} else {
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
		if _, err := content.SaveCompiledAssetHeader(outputPath, header, codec); err != nil {
			return result, err
		}
		result.HeaderWrote = true
	}
	result.HeaderInfo = info
	return result, nil
}

func validateCompiledSource(asset *content.AssetDef) error {
	valid := func(id string) bool { return strings.TrimSpace(id) != "" && utf8.ValidString(id) }
	if asset.SchemaVersion != 4 || !valid(asset.ID) || asset.Runtime != nil && asset.Runtime.CollapseVoxelParts {
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
	shape := &content.CompiledAssetShapeDef{SchemaVersion: content.CurrentCompiledAssetShapeSchemaVersion, Lattice: authoredVoxelShapeLattice(part.VoxelResolution)}
	for sectorCoord, sector := range geometry.Sectors {
		for bz := 0; bz < 4; bz++ {
			for by := 0; by < 4; by++ {
				for bx := 0; bx < 4; bx++ {
					brick := sector.GetBrick(bx, by, bz)
					if brick == nil {
						continue
					}
					var encoded voxelcodec.Brick
					for axis, local := range [3]int{bx, by, bz} {
						// Check sector bounds before multiplying native coordinates.
						if sectorCoord[axis] < -67108864 || sectorCoord[axis] > 67108863 {
							return nil, fmt.Errorf("compiled geometry exceeds portable coordinates")
						}
						coord := int64(sectorCoord[axis])*4 + int64(local)
						if coord < -268435456 || coord > 268435455 {
							return nil, fmt.Errorf("compiled geometry exceeds portable coordinates")
						}
						encoded.Coord[axis] = int32(coord)
					}
					for z := 0; z < 8; z++ {
						for y := 0; y < 8; y++ {
							for x := 0; x < 8; x++ {
								value := brick.VoxelValue(x, y, z)
								if value == 0 {
									continue
								}
								linear := x + 8*y + 64*z
								encoded.Occupancy[linear/64] |= uint64(1) << uint(linear%64)
								encoded.Values = append(encoded.Values, value)
							}
						}
					}
					if len(encoded.Values) > 0 {
						shape.Bricks = append(shape.Bricks, encoded)
					}
				}
			}
		}
	}
	return shape, nil
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
