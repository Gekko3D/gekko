package gekko

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/bits"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3cFixture(t *testing.T) (string, string, *content.AssetDef) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "source")
	out := filepath.Join(root, "compiled", "asset.gkasset")
	if err := os.MkdirAll(source, 0700); err != nil {
		t.Fatal(err)
	}
	transform := content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}, Pivot: content.Vec3{0.5, 0, 0}}
	part := content.AssetPartDef{ID: "shape", Name: "Shape", ParentID: "root", Transform: transform, ModelScale: 2, VoxelResolution: 0.25, Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "mat"}, {Value: 4, MaterialID: "mat"}}, Voxels: []content.VoxelObjectVoxelDef{{X: -9, Value: 3}, {X: -9, Value: 4}, {X: -8, Y: 1, Z: -1, Value: 3}, {X: 1, Y: -1, Z: 1, Value: 4}}}}}
	duplicate := part
	duplicate.ID = "duplicate"
	duplicate.Name = "Duplicate"
	duplicate.Transform.Position = content.Vec3{3, 0, 0}
	asset := &content.AssetDef{ID: "asset", SchemaVersion: 4, Name: "Asset", Materials: []content.AssetMaterialDef{{ID: "mat", Name: "Material", IOR: 1.5, BaseColor: [4]uint8{10, 20, 30, 255}}}, Parts: []content.AssetPartDef{part, {ID: "root", Name: "Root", Transform: transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}}, duplicate}, Markers: []content.AssetMarkerDef{{ID: "marker", Name: "Marker", ParentID: "shape", Kind: content.AssetMarkerKindMuzzle, Transform: transform}}, Emitters: []content.AssetEmitterDef{{ID: "emitter", Name: "Emitter", ParentID: "shape", Transform: transform, Emitter: content.EmitterDef{TexturePath: "sprite.png"}}}, Skeleton: &content.AssetSkeletonDef{Bones: []content.AssetBoneDef{{ID: "root", JointID: "root-joint", Name: "Root", Transform: transform}}}, AnimationSetPaths: []string{"rigged.gkanim", "direct.gkanim"}, DefaultAnimationClipID: "idle"}
	if err := content.SaveAnimationRig(filepath.Join(source, "rig.gkrig"), &content.AnimationRigDef{ID: "rig", SchemaVersion: 1, Name: "Rig", Joints: []content.AnimationRigJointDef{{ID: "root-joint", Transform: transform}}}); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveAnimationSet(filepath.Join(source, "rigged.gkanim"), &content.AnimationSetDef{ID: "rigged", SchemaVersion: 1, Name: "Rigged", RigPath: "rig.gkrig", Clips: []content.AssetAnimationClipDef{{ID: "idle", Name: "Idle", Duration: 1, Tracks: []content.AssetAnimationTrackDef{{TargetID: "root-joint", PositionKeys: []content.AssetVec3KeyDef{{Value: content.Vec3{0, 1, 0}}}}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveAnimationSet(filepath.Join(source, "direct.gkanim"), &content.AnimationSetDef{ID: "direct", SchemaVersion: 1, Name: "Direct", TargetAssetID: "asset", Clips: []content.AssetAnimationClipDef{{ID: "open", Name: "Open", Duration: 1, Tracks: []content.AssetAnimationTrackDef{{TargetID: "shape", PositionKeys: []content.AssetVec3KeyDef{{Value: content.Vec3{1, 0, 0}}}}}}}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "sprite.png"), []byte{0, 1, 2, 255, 17}, 0600); err != nil {
		t.Fatal(err)
	}
	input := filepath.Join(source, "asset.gkasset")
	c3cWrite(t, input, asset)
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{}); validation.HardErrorCount != 0 {
		t.Fatal("fixture validation", validation)
	}
	if _, err := content.ResolveAssetAnimations(asset, input); err != nil {
		t.Fatal("fixture animations", err)
	}
	return input, out, asset
}

func c3cWrite(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func c3cRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func c3cCodec(t *testing.T, options voxelcodec.Options) *voxelcodec.Codec {
	t.Helper()
	c, err := voxelcodec.New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func c3cSnapshot(shape *content.CompiledAssetShapeDef) *content.VoxelObjectSnapshotDef {
	snapshot := &content.VoxelObjectSnapshotDef{SchemaVersion: 1}
	for _, brick := range shape.Bricks {
		value := 0
		for word, mask := range brick.Occupancy {
			for mask != 0 {
				bit := bits.TrailingZeros64(mask)
				linear := word*64 + bit
				snapshot.Voxels = append(snapshot.Voxels, content.VoxelObjectVoxelDef{X: int(brick.Coord[0])*8 + linear%8, Y: int(brick.Coord[1])*8 + (linear/8)%8, Z: int(brick.Coord[2])*8 + linear/64, Value: brick.Values[value]})
				value++
				mask &= mask - 1
			}
		}
	}
	return snapshot
}
func c3cGeometry(records []content.VoxelObjectVoxelDef) map[[3]int]uint8 {
	m := make(map[[3]int]uint8)
	for _, v := range records {
		m[[3]int{v.X, v.Y, v.Z}] = v.Value
	}
	return m
}

func TestC3cCompileGeometryClosureAndNoOp(t *testing.T) {
	input, output, asset := c3cFixture(t)
	sourceBytes := c3cRead(t, input)
	rigBytes := c3cRead(t, filepath.Join(filepath.Dir(input), "rig.gkrig"))
	directBytes := c3cRead(t, filepath.Join(filepath.Dir(input), "direct.gkanim"))
	textureBytes := c3cRead(t, filepath.Join(filepath.Dir(input), "sprite.png"))
	codec := c3cCodec(t, voxelcodec.Options{})
	result, err := CompileAuthoredAsset(input, output, codec)
	if err != nil || !result.HeaderWrote || result.ShapesWritten != 1 || result.ShapesReused != 0 || result.DependenciesWritten != 4 || result.DependenciesReused != 0 {
		t.Fatalf("compile result=%+v err=%v", result, err)
	}
	if !bytes.Equal(sourceBytes, c3cRead(t, input)) {
		t.Fatal("compiler mutated authoring file")
	}
	header, info, err := content.LoadCompiledAssetHeader(output, codec)
	if err != nil || info != result.HeaderInfo {
		t.Fatal("header load", err)
	}
	if len(header.Shapes) != 2 || header.Shapes[0].Path != header.Shapes[1].Path {
		t.Fatal("identical postscale shapes were not deduplicated")
	}
	shapePath := filepath.Join(filepath.Dir(output), filepath.FromSlash(header.Shapes[0].Path))
	frame := c3cRead(t, shapePath)
	hash := sha256.Sum256(frame)
	if header.Shapes[0].Path != "shapes/"+hex.EncodeToString(hash[:])+".gkshape" {
		t.Fatal("physical shape path is not exact frame hash")
	}
	shape, shapeInfo, err := content.LoadCompiledAssetShape(shapePath, codec)
	if err != nil || shapeInfo.ContentID != header.Shapes[0].ContentID || shapeInfo.EncodedBytes != header.Shapes[0].EncodedBytes || shapeInfo.DecodedBytes != header.Shapes[0].DecodedBytes {
		t.Fatal("shape reference info mismatch", err)
	}
	expectedMap := XBrickMapFromVoxelObjectSnapshot(&content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: asset.Parts[0].Source.VoxelShape.Voxels}).Resample(2)
	expected := VoxelObjectSnapshotFromXBrickMap(expectedMap)
	if !reflect.DeepEqual(c3cGeometry(c3cSnapshot(shape).Voxels), c3cGeometry(expected.Voxels)) {
		t.Fatal("compiled geometry differs from signed runtime postscale construction")
	}
	lattice := content.VoxelObjectLatticeDef{VoxelResolution: 0.25, RasterizationVersion: "gekko-voxel-shape-v1"}
	baseID, _, err := content.VoxelObjectBaseIdentity(expected, lattice, codec)
	if err != nil || shape.Lattice != lattice || header.Shapes[0].BaseIdentity != baseID {
		t.Fatal("compiled lattice/base identity differs from runtime", err)
	}
	for i, part := range header.Asset.Parts {
		if part.ID != asset.Parts[i].ID || part.ParentID != asset.Parts[i].ParentID || part.Transform != asset.Parts[i].Transform {
			t.Fatal("part order/hierarchy/transform changed")
		}
		if part.Source.Kind == content.AssetSourceKindVoxelShape && (len(part.Source.VoxelShape.Voxels) != 0 || !reflect.DeepEqual(part.Source.VoxelShape.Palette, asset.Parts[i].Source.VoxelShape.Palette)) {
			t.Fatal("header geometry removal lost authored palette")
		}
	}
	if !reflect.DeepEqual(header.Asset.Markers, asset.Markers) || !reflect.DeepEqual(header.Asset.Skeleton, asset.Skeleton) {
		t.Fatal("markers/skeleton changed")
	}
	for _, relative := range header.Asset.AnimationSetPaths {
		setPath := filepath.Join(filepath.Dir(output), filepath.FromSlash(relative))
		if !strings.HasPrefix(relative, "dependencies/") {
			t.Fatal("animation dependency path not relative")
		}
		set, err := content.LoadAnimationSet(setPath)
		if err != nil {
			t.Fatal(err)
		}
		if set.RigPath != "" {
			if filepath.Base(set.RigPath) != set.RigPath || !bytes.Equal(rigBytes, c3cRead(t, filepath.Join(filepath.Dir(setPath), set.RigPath))) {
				t.Fatal("rig not byteexact or relative to animation set")
			}
		} else if !bytes.Equal(directBytes, c3cRead(t, setPath)) {
			t.Fatal("unrewritten direct animation changed bytes")
		}
	}
	texturePath := filepath.Join(filepath.Dir(output), filepath.FromSlash(header.Asset.Emitters[0].Emitter.TexturePath))
	if !bytes.Equal(textureBytes, c3cRead(t, texturePath)) {
		t.Fatal("texture copy changed bytes")
	}
	fixedTime := time.Date(2001, time.January, 2, 3, 4, 5, 0, time.UTC)
	for _, path := range []string{output, shapePath, texturePath} {
		if err := os.Chtimes(path, fixedTime, fixedTime); err != nil {
			t.Fatal(err)
		}
	}
	oldHeader := c3cRead(t, output)
	again, err := CompileAuthoredAsset(input, output, codec)
	if err != nil || again.HeaderWrote || again.HeaderInfo != result.HeaderInfo || again.ShapesWritten != 0 || again.ShapesReused != 1 || again.DependenciesWritten != 0 || again.DependenciesReused != 4 || !bytes.Equal(oldHeader, c3cRead(t, output)) {
		t.Fatalf("no-op compile result=%+v err=%v", again, err)
	}
	for _, path := range []string{output, shapePath, texturePath} {
		stat, err := os.Stat(path)
		if err != nil || !stat.ModTime().Equal(fixedTime) {
			t.Fatal("no-op compile replaced artifact or altered mtime", path, err)
		}
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("compiler closed borrowed codec", err)
	}
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	resolved, err := content.ResolveAssetAnimations(header.Asset, output)
	if err != nil || len(resolved.Clips) != 2 || resolved.Clips[0].Tracks[0].TargetID != "root" || resolved.Clips[1].Tracks[0].TargetID != "shape" {
		t.Fatal("compiled closure cannot resolve rig/direct animations after source removal", err)
	}
	if !bytes.Equal(textureBytes, c3cRead(t, texturePath)) {
		t.Fatal("texture closure depends on source")
	}
}

func TestC3cCodecProfilesUseIndependentPhysicalShapes(t *testing.T) {
	input, out, _ := c3cFixture(t)
	first, err := CompileAuthoredAsset(input, out, nil)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := content.LoadCompiledAssetHeader(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldShape := c3cRead(t, filepath.Join(filepath.Dir(out), plain.Shapes[0].Path))
	dictionary := c3cCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 43, Bytes: bytes.Repeat([]byte("compiled_asset_shape compiled_asset_header lattice dense-v1 geometry parts"), 8)}})
	second, err := CompileAuthoredAsset(input, out, dictionary)
	if err != nil || !second.HeaderWrote || second.ShapesWritten != 1 || second.DependenciesReused != 4 {
		t.Fatalf("profile compile=%+v err=%v", second, err)
	}
	header, _, err := content.LoadCompiledAssetHeader(out, dictionary)
	if err != nil {
		t.Fatal(err)
	}
	if header.Shapes[0].Path == plain.Shapes[0].Path || header.Shapes[0].ContentID != plain.Shapes[0].ContentID || header.Shapes[0].BaseIdentity != plain.Shapes[0].BaseIdentity {
		t.Fatal("dictionary profile confused physical and logical identity")
	}
	if !bytes.Equal(oldShape, c3cRead(t, filepath.Join(filepath.Dir(out), plain.Shapes[0].Path))) {
		t.Fatal("profile switch replaced old immutable shape")
	}
	third, err := CompileAuthoredAsset(input, out, nil)
	if err != nil || !third.HeaderWrote || third.ShapesWritten != 0 || third.ShapesReused != 1 || third.DependenciesReused != 4 || third.HeaderInfo != first.HeaderInfo {
		t.Fatalf("original profile not reused: %+v err=%v", third, err)
	}
}

func TestC3cRejectsInvalidInputsBeforePublication(t *testing.T) {
	mutations := map[string]func(*content.AssetDef){"schema": func(a *content.AssetDef) { a.SchemaVersion = 0 }, "collapse": func(a *content.AssetDef) { a.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true} }, "vox": func(a *content.AssetDef) {
		a.Parts[0].Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "missing.vox"}
	}, "procedural": func(a *content.AssetDef) {
		a.Parts[0].Source = content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube"}
	}, "asset-id": func(a *content.AssetDef) { a.ID = "" }, "material-id": func(a *content.AssetDef) { a.Materials[0].ID = "" }, "part-id": func(a *content.AssetDef) { a.Parts[0].ID = "" }, "marker-id": func(a *content.AssetDef) { a.Markers[0].ID = " " }, "emitter-id": func(a *content.AssetDef) { a.Emitters[0].ID = "" }, "bone-id": func(a *content.AssetDef) { a.Skeleton.Bones[0].ID = "" }, "joint-id": func(a *content.AssetDef) { a.Skeleton.Bones[0].JointID = "" }, "missing-animation": func(a *content.AssetDef) { a.AnimationSetPaths[0] = "absent.gkanim" }, "missing-texture": func(a *content.AssetDef) { a.Emitters[0].Emitter.TexturePath = "absent.png" }}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			input, out, asset := c3cFixture(t)
			if _, err := CompileAuthoredAsset(input, out, nil); err != nil {
				t.Fatal(err)
			}
			old := c3cRead(t, out)
			mutate(asset)
			c3cWrite(t, input, asset)
			result, err := CompileAuthoredAsset(input, out, nil)
			if err == nil || result != (CompiledAssetCompileResult{}) || !bytes.Equal(old, c3cRead(t, out)) {
				t.Fatalf("invalid input altered header: result=%+v err=%v", result, err)
			}
		})
	}
	t.Run("strict-unknown-field", func(t *testing.T) {
		input, out, _ := c3cFixture(t)
		raw := c3cRead(t, input)
		raw = append([]byte(`{"unknown":true,`), raw[1:]...)
		if err := os.WriteFile(input, raw, 0600); err != nil {
			t.Fatal(err)
		}
		result, err := CompileAuthoredAsset(input, out, nil)
		if err == nil || result != (CompiledAssetCompileResult{}) {
			t.Fatal("unknown authored field accepted")
		}
		if _, err := os.Stat(filepath.Dir(out)); !os.IsNotExist(err) {
			t.Fatal("invalid source created output files")
		}
	})
}

func TestC3cPublicationConflictsAndSourceAliases(t *testing.T) {
	for _, kind := range []string{"shape-conflict", "shape-symlink", "shapes-directory-symlink", "dependency-conflict", "header-directory", "input-path", "input-symlink", "input-hardlink", "dependency-output", "dependency-hardlink"} {
		t.Run(kind, func(t *testing.T) {
			input, out, _ := c3cFixture(t)
			if _, err := CompileAuthoredAsset(input, out, nil); err != nil {
				t.Fatal(err)
			}
			oldHeader := c3cRead(t, out)
			sourceBytes := c3cRead(t, input)
			dependency := filepath.Join(filepath.Dir(input), "sprite.png")
			depBytes := c3cRead(t, dependency)
			header, _, err := content.LoadCompiledAssetHeader(out, nil)
			if err != nil {
				t.Fatal(err)
			}
			targetOut := out
			artifact := ""
			sentinel := []byte("existing conflict")
			switch kind {
			case "shape-conflict":
				artifact = filepath.Join(filepath.Dir(out), header.Shapes[0].Path)
				if err := os.WriteFile(artifact, sentinel, 0600); err != nil {
					t.Fatal(err)
				}
			case "shape-symlink":
				artifact = filepath.Join(filepath.Dir(out), header.Shapes[0].Path)
				if err := os.Remove(artifact); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(input, artifact); err != nil {
					t.Fatal(err)
				}
			case "shapes-directory-symlink":
				artifact = filepath.Join(filepath.Dir(out), "shapes")
				relocated := filepath.Join(t.TempDir(), "relocated-shapes")
				if err := os.Rename(artifact, relocated); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(relocated, artifact); err != nil {
					t.Fatal(err)
				}
			case "dependency-conflict":
				artifact = filepath.Join(filepath.Dir(out), header.Asset.Emitters[0].Emitter.TexturePath)
				if err := os.WriteFile(artifact, sentinel, 0600); err != nil {
					t.Fatal(err)
				}
			case "header-directory":
				targetOut = filepath.Join(filepath.Dir(out), "directory")
				if err := os.Mkdir(targetOut, 0700); err != nil {
					t.Fatal(err)
				}
			case "input-path":
				targetOut = input
			case "input-symlink":
				targetOut = filepath.Join(filepath.Dir(out), "alias")
				if err := os.Symlink(input, targetOut); err != nil {
					t.Fatal(err)
				}
			case "input-hardlink":
				targetOut = filepath.Join(filepath.Dir(out), "alias")
				if err := os.Link(input, targetOut); err != nil {
					t.Fatal(err)
				}
			case "dependency-output":
				targetOut = dependency
			case "dependency-hardlink":
				targetOut = filepath.Join(filepath.Dir(out), "alias")
				if err := os.Link(dependency, targetOut); err != nil {
					t.Fatal(err)
				}
			}
			result, err := CompileAuthoredAsset(input, targetOut, nil)
			if err == nil || result != (CompiledAssetCompileResult{}) {
				t.Fatalf("publication conflict/alias accepted: %+v err=%v", result, err)
			}
			if !bytes.Equal(oldHeader, c3cRead(t, out)) || !bytes.Equal(sourceBytes, c3cRead(t, input)) || !bytes.Equal(depBytes, c3cRead(t, dependency)) {
				t.Fatal("failure replaced header or source")
			}
			if artifact != "" && kind != "shape-symlink" && kind != "shapes-directory-symlink" && !bytes.Equal(sentinel, c3cRead(t, artifact)) {
				t.Fatal("immutable conflict was clobbered")
			}
			if kind == "shape-symlink" || kind == "shapes-directory-symlink" {
				if stat, err := os.Lstat(artifact); err != nil || stat.Mode()&os.ModeSymlink == 0 {
					t.Fatal("artifact symlink replaced")
				}
			}
		})
	}
}

func TestC3cClosedBorrowedCodecLeavesNoOutput(t *testing.T) {
	input, out, _ := c3cFixture(t)
	codec := c3cCodec(t, voxelcodec.Options{})
	codec.Close()
	result, err := CompileAuthoredAsset(input, out, codec)
	if err == nil || result != (CompiledAssetCompileResult{}) {
		t.Fatal("closed borrowed codec accepted")
	}
	if _, err := os.Stat(filepath.Dir(out)); !os.IsNotExist(err) {
		t.Fatal("closed codec failure wrote output")
	}
}

func TestC3cDefaultsAndGroupOnlyCompilation(t *testing.T) {
	for _, groupOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "voxel-defaults", true: "group-only"}[groupOnly], func(t *testing.T) {
			root := t.TempDir()
			input := filepath.Join(root, "source.gkasset")
			output := filepath.Join(root, "compiled", "asset.gkasset")
			asset := &content.AssetDef{ID: "asset", SchemaVersion: 4, Name: "Defaults", Materials: []content.AssetMaterialDef{{ID: "mat", Name: "Material"}}, Parts: []content.AssetPartDef{{ID: "part", Name: "Part", Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "mat"}}, Voxels: []content.VoxelObjectVoxelDef{{X: -1, Y: 1, Value: 3}}}}}}}
			if groupOnly {
				asset.Parts[0].Source = content.AssetSourceDef{Kind: content.AssetSourceKindGroup}
			}
			c3cWrite(t, input, asset)
			expected, err := content.LoadAsset(input)
			if err != nil {
				t.Fatal(err)
			}
			result, err := CompileAuthoredAsset(input, output, nil)
			if err != nil {
				t.Fatal("accepted authored defaults rejected", err)
			}
			header, _, err := content.LoadCompiledAssetHeader(output, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(header.Asset.Materials, expected.Materials) || header.Asset.Parts[0].ModelScale != expected.Parts[0].ModelScale || header.Asset.Parts[0].VoxelResolution != expected.Parts[0].VoxelResolution {
				t.Fatal("compiler differs from legacy loader normalization")
			}
			if groupOnly {
				if len(header.Shapes) != 0 || result.ShapesWritten != 0 || result.ShapesReused != 0 {
					t.Fatal("group-only asset emitted geometry")
				}
			} else {
				if len(header.Shapes) != 1 || result.ShapesWritten != 1 {
					t.Fatal("normalized voxel shape missing")
				}
				shape, _, err := content.LoadCompiledAssetShape(filepath.Join(filepath.Dir(output), header.Shapes[0].Path), nil)
				if err != nil || shape.Lattice.VoxelResolution != expected.Parts[0].VoxelResolution {
					t.Fatal("default lattice missing", err)
				}
			}
		})
	}
}

func TestC3cConcurrentMatchingCompilation(t *testing.T) {
	input, output, _ := c3cFixture(t)
	codec := c3cCodec(t, voxelcodec.Options{})
	type outcome struct {
		result CompiledAssetCompileResult
		err    error
	}
	results := make(chan outcome, 3)
	ready := make(chan struct{}, 3)
	start := make(chan struct{})
	for i := 0; i < 3; i++ {
		go func() {
			ready <- struct{}{}
			<-start
			result, err := CompileAuthoredAsset(input, output, codec)
			results <- outcome{result, err}
		}()
	}
	for i := 0; i < 3; i++ {
		<-ready
	}
	close(start)
	// Drain workers before assertions can trigger TempDir or codec cleanup.
	completed := make([]outcome, 3)
	for i := range completed {
		completed[i] = <-results
	}
	var first voxelcodec.Info
	shapesWritten, dependenciesWritten := 0, 0
	for i, got := range completed {
		if got.err != nil {
			t.Fatal("matching concurrent compile failed", got.err)
		}
		if i == 0 {
			first = got.result.HeaderInfo
		} else if got.result.HeaderInfo != first {
			t.Fatal("matching concurrent headers differ")
		}
		shapesWritten += got.result.ShapesWritten
		dependenciesWritten += got.result.DependenciesWritten
	}
	if shapesWritten != 1 || dependenciesWritten != 4 {
		t.Fatalf("immutable matching race produced duplicate writes: shapes=%d deps=%d", shapesWritten, dependenciesWritten)
	}
	header, info, err := content.LoadCompiledAssetHeader(output, codec)
	if err != nil || info != first {
		t.Fatal("concurrent header incomplete", err)
	}
	shape, shapeInfo, err := content.LoadCompiledAssetShape(filepath.Join(filepath.Dir(output), header.Shapes[0].Path), codec)
	if err != nil || shapeInfo.ContentID != header.Shapes[0].ContentID || len(shape.Bricks) == 0 {
		t.Fatal("concurrent shape incomplete", err)
	}
	if _, err := content.ResolveAssetAnimations(header.Asset, output); err != nil {
		t.Fatal("concurrent dependency closure incomplete", err)
	}
}

func TestC3cBorrowedGeometryLimitsBeforeWrites(t *testing.T) {
	input, output, _ := c3cFixture(t)
	codec := c3cCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxBricks: 1}})
	result, err := CompileAuthoredAsset(input, output, codec)
	if err == nil || result != (CompiledAssetCompileResult{}) {
		t.Fatal("compiled geometry ignored borrowed brick limit")
	}
	if _, err := os.Stat(filepath.Dir(output)); !os.IsNotExist(err) {
		t.Fatal("geometry-limit failure wrote output")
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("failed compile closed borrowed codec", err)
	}
}
