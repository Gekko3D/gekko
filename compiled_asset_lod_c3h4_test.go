package gekko

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3h4Fixture(t *testing.T) (string, string, *content.AssetDef) {
	t.Helper()
	input, output, a := c3cFixture(t)
	for i := range a.Parts {
		if a.Parts[i].Source.Kind == content.AssetSourceKindVoxelShape {
			a.Parts[i].ModelScale = 1
			a.Parts[i].Source.VoxelShape = &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "mat"}, {Value: 4, MaterialID: "unused"}}, Voxels: []content.VoxelObjectVoxelDef{{X: -2, Value: 3}, {X: -1, Value: 3}, {X: 0, Value: 3}, {X: 1, Value: 3}}}
		}
	}
	a.Materials = append(a.Materials, content.AssetMaterialDef{ID: "unused", Name: "Unused transparent", IOR: 1.5, BaseColor: [4]uint8{1, 2, 3, 12}, Transparency: .8})
	c3cWrite(t, input, a)
	return input, output, a
}
func c3h4Files(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			files[rel] = c3cRead(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Preserve positional source compatibility of the existing exported result.
var _ = CompiledAssetCompileResult{voxelcodec.Info{}, false, 0, 0, 0, 0}

func TestC3h4DefaultOptionsExactLegacyClosure(t *testing.T) {
	input, output, _ := c3h4Fixture(t)
	old, err := CompileAuthoredAsset(input, output, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldFiles := c3h4Files(t, filepath.Dir(output))
	other := filepath.Join(t.TempDir(), "asset.gkasset")
	detailed, err := CompileAuthoredAssetWithOptions(input, other, nil, CompiledAssetCompileOptions{})
	if err != nil || detailed.CompiledAssetCompileResult != old || detailed.LODsWritten != 0 || detailed.LODsReused != 0 {
		t.Fatalf("default result=%+v legacy=%+v err=%v", detailed, old, err)
	}
	if !reflect.DeepEqual(oldFiles, c3h4Files(t, filepath.Dir(other))) {
		t.Fatal("disabled option changes shipping closure bytes")
	}
	h, _, err := content.LoadCompiledAssetHeader(other, nil)
	if err != nil || h.SchemaVersion != 1 || len(h.LODs) != 0 {
		t.Fatal("disabled options change version", err)
	}
}

func TestC3h4CompileSourceBoundLODClosureAndNoOp(t *testing.T) {
	input, output, a := c3h4Fixture(t)
	source := c3cRead(t, input)
	legacy := filepath.Join(t.TempDir(), "asset.gkasset")
	if _, err := CompileAuthoredAsset(input, legacy, nil); err != nil {
		t.Fatal(err)
	}
	old, _, err := content.LoadCompiledAssetHeader(legacy, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err != nil || !result.HeaderWrote || result.ShapesWritten != 1 || result.DependenciesWritten != 4 || result.LODsWritten != 1 || result.LODsReused != 0 {
		t.Fatalf("enabled result=%+v err=%v", result, err)
	}
	h, info, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil || info != result.HeaderInfo || h.SchemaVersion != content.CompiledAssetLODHeaderSchemaVersion || h.CompilerVersion != content.CompiledAssetLODHeaderCompilerVersion || len(h.LODs) != 2 {
		t.Fatal("missing versioned LOD header", err)
	}
	if !reflect.DeepEqual(h.Asset, old.Asset) || !reflect.DeepEqual(h.Shapes, old.Shapes) {
		t.Fatal("LOD compilation changed authoritative metadata/geometry proofs")
	}
	if h.LODs[0].Path != h.LODs[1].Path {
		t.Fatal("identical geometry LOD files not deduplicated")
	}
	ref := h.LODs[0]
	path := filepath.Join(filepath.Dir(output), filepath.FromSlash(ref.Path))
	raw := c3cRead(t, path)
	hash := sha256.Sum256(raw)
	if ref.Path != "lods/"+hex.EncodeToString(hash[:])+".gklod" {
		t.Fatal("LOD physical path not exact frame hash")
	}
	lod, li, err := content.LoadCompiledAssetLOD(path, nil)
	if err != nil || li.ContentID != ref.ContentID || li.EncodedBytes != ref.EncodedBytes || li.DecodedBytes != ref.DecodedBytes {
		t.Fatal("LOD frame proof mismatch", err)
	}
	sr := h.Shapes[0]
	shape, si, err := content.LoadCompiledAssetShape(filepath.Join(filepath.Dir(output), filepath.FromSlash(sr.Path)), nil)
	if err != nil {
		t.Fatal(err)
	}
	if lod.SourceContentID != si.ContentID || lod.SourceContentID != sr.ContentID || lod.SourceContentID == sr.BaseIdentity || lod.SourceLattice != shape.Lattice || lod.Factor != 2 || lod.ReductionVersion != content.CompiledAssetLOD2xReductionVersion || lod.Value != 3 || lod.SourceVoxelCount != 4 || lod.SourceMin != [3]int64{-2, 0, 0} || lod.SourceMax != [3]int64{2, 1, 1} || lod.CoarseMin != [3]int64{-1, 0, 0} || lod.CoarseMax != [3]int64{1, 1, 1} {
		t.Fatalf("wrong source binding/geometry metadata: %+v", lod)
	}
	// Fixed oracle: negative source pair (-2,-1) maps to -1, positive (0,1) to 0.
	coarse := c3cSnapshot(&content.CompiledAssetShapeDef{Bricks: lod.Bricks})
	want := map[[3]int]uint8{{-1, 0, 0}: 3, {0, 0, 0}: 3}
	if !reflect.DeepEqual(c3cGeometry(coarse.Voxels), want) {
		t.Fatal("zero-anchored signed conservative cells differ", coarse)
	}
	base, _, err := content.CompiledAssetShapeBaseIdentity(shape, nil)
	if err != nil || base != sr.BaseIdentity {
		t.Fatal("full resolution E2 base changed", err)
	}
	if !bytes.Equal(source, c3cRead(t, input)) {
		t.Fatal("compiler changed source")
	}
	resolved, err := content.ResolveAssetAnimations(h.Asset, output)
	if err != nil || resolved == nil || a.Skeleton == nil {
		t.Fatal("geometric/skeletal animation closure lost", err)
	}
	oldFiles := c3h4Files(t, filepath.Dir(output))
	fixed := time.Date(2001, 1, 2, 3, 4, 5, 0, time.UTC)
	for rel := range oldFiles {
		p := filepath.Join(filepath.Dir(output), rel)
		if err := os.Chtimes(p, fixed, fixed); err != nil {
			t.Fatal(err)
		}
	}
	repeat, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err != nil || repeat.HeaderWrote || repeat.ShapesWritten != 0 || repeat.ShapesReused != 1 || repeat.DependenciesWritten != 0 || repeat.DependenciesReused != 4 || repeat.LODsWritten != 0 || repeat.LODsReused != 1 {
		t.Fatalf("no-op=%+v err=%v", repeat, err)
	}
	if !reflect.DeepEqual(oldFiles, c3h4Files(t, filepath.Dir(output))) {
		t.Fatal("no-op changes closure bytes")
	}
	for rel := range oldFiles {
		st, err := os.Stat(filepath.Join(filepath.Dir(output), rel))
		if err != nil || !st.ModTime().Equal(fixed) {
			t.Fatal("no-op rewrites immutable file", rel, err)
		}
	}
}

func TestC3h4FailClosedEligibilityAndUnusedMaterialAnimation(t *testing.T) {
	cases := map[string]struct {
		change func(*content.AssetDef)
		want   int
	}{
		"alpha":        {func(a *content.AssetDef) { a.Materials[0].BaseColor[3] = 254 }, 0},
		"transparency": {func(a *content.AssetDef) { a.Materials[0].Transparency = .1 }, 0},
		"mixed-values": {func(a *content.AssetDef) {
			for i := range a.Parts {
				if a.Parts[i].Source.VoxelShape != nil {
					a.Parts[i].Source.VoxelShape.Voxels[0].Value = 4
				}
			}
		}, 0},
		"mixed-values-same-opaque-material": {func(a *content.AssetDef) {
			for i := range a.Parts {
				if a.Parts[i].Source.VoxelShape != nil {
					a.Parts[i].Source.VoxelShape.Palette[1].MaterialID = "mat"
					a.Parts[i].Source.VoxelShape.Voxels[0].Value = 4
				}
			}
		}, 0},
		"no-count-reduction": {func(a *content.AssetDef) {
			for i := range a.Parts {
				if a.Parts[i].Source.VoxelShape != nil {
					a.Parts[i].Source.VoxelShape.Voxels = []content.VoxelObjectVoxelDef{{X: -1, Value: 3}, {X: 0, Value: 3}}
				}
			}
		}, 0},
		"empty-geometry": {func(a *content.AssetDef) {
			for i := range a.Parts {
				if a.Parts[i].Source.VoxelShape != nil {
					a.Parts[i].Source.VoxelShape.Voxels = nil
				}
			}
		}, 0},
		"rgb-with-zero-alpha":      {func(a *content.AssetDef) { a.Materials[0].BaseColor = [4]uint8{1, 2, 3, 0} }, 0},
		"normalized-default-color": {func(a *content.AssetDef) { a.Materials[0].BaseColor = [4]uint8{} }, 2},
		"animated-used-value-empty-frames": {func(a *content.AssetDef) {
			a.MaterialAnimations = []content.AssetMaterialAnimationDef{{ID: "animated", Kind: "frames", PaletteIndices: []uint8{3}}}
		}, 0},
		"animated-used-value-uv": {func(a *content.AssetDef) {
			a.MaterialAnimations = []content.AssetMaterialAnimationDef{{ID: "animated", Kind: "uv_scroll", PaletteIndices: []uint8{3}, UVScroll: &content.AssetMaterialUVScrollDef{Velocity: [2]float32{1, 0}}}}
		}, 0},
		"animated-unused-value": {func(a *content.AssetDef) {
			a.MaterialAnimations = []content.AssetMaterialAnimationDef{{ID: "animated", Kind: "frames", PaletteIndices: []uint8{4}}}
		}, 2},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			input, output, a := c3h4Fixture(t)
			c.change(a)
			c3cWrite(t, input, a)
			result, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
			if err != nil {
				t.Fatal("valid source compile failed", err)
			}
			h, _, err := content.LoadCompiledAssetHeader(output, nil)
			if err != nil || h.SchemaVersion != 2 || len(h.LODs) != c.want {
				t.Fatalf("wrong eligibility lods=%d want=%d err=%v", len(h.LODs), c.want, err)
			}
			if c.want == 0 && result.LODsWritten != 0 {
				t.Fatal("skipped shapes published derivative")
			}
		})
	}
}

func TestC3h4BorrowedDictionaryChangesPhysicalNotLogicalLODIdentity(t *testing.T) {
	input, output, _ := c3h4Fixture(t)
	plain := c3cCodec(t, voxelcodec.Options{})
	dict := c3cCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 45, Bytes: bytes.Repeat([]byte("compiled_asset_lod source_content_id source_lattice occupancy-or-zero-anchored-2x-v1"), 8)}})
	other := filepath.Join(t.TempDir(), "asset.gkasset")
	for _, c := range []struct {
		out   string
		codec *voxelcodec.Codec
	}{{output, plain}, {other, dict}} {
		if _, err := CompileAuthoredAssetWithOptions(input, c.out, c.codec, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := c.codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
			t.Fatal("compiler closed borrowed codec", err)
		}
	}
	a, _, err := content.LoadCompiledAssetHeader(output, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := content.LoadCompiledAssetHeader(other, dict)
	if err != nil {
		t.Fatal(err)
	}
	if a.LODs[0].ContentID != b.LODs[0].ContentID || a.LODs[0].SourceContentID != b.LODs[0].SourceContentID || a.LODs[0].Path == b.LODs[0].Path {
		t.Fatal("dictionary logical/physical identity contract violated")
	}
}

func TestC3h4PublicationFailuresPreservePreviousHeader(t *testing.T) {
	for _, kind := range []string{"immutable-lod-conflict", "lod-directory-symlink", "invalid-source", "codec-profile"} {
		t.Run(kind, func(t *testing.T) {
			input, output, a := c3h4Fixture(t)
			if _, err := CompileAuthoredAsset(input, output, nil); err != nil {
				t.Fatal(err)
			}
			old := c3cRead(t, output)
			var codec *voxelcodec.Codec
			if kind == "immutable-lod-conflict" {
				probe := filepath.Join(t.TempDir(), "asset.gkasset")
				if _, err := CompileAuthoredAssetWithOptions(input, probe, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
					t.Fatal(err)
				}
				h, _, err := content.LoadCompiledAssetHeader(probe, nil)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(filepath.Dir(output), filepath.FromSlash(h.LODs[0].Path))
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("conflicting immutable derivative"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "lod-directory-symlink" {
				if err := os.Symlink(t.TempDir(), filepath.Join(filepath.Dir(output), "lods")); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "invalid-source" {
				a.Materials[0].Transparency = float32(math.Inf(1)) // JSON rejects Inf; malformed persisted authoring must fail without publication.
				if err := os.WriteFile(input, []byte(`{"schema_version":4,"id":"asset","materials":[{"id":"mat","transparency":1e999}]}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "codec-profile" {
				probe := filepath.Join(t.TempDir(), "asset.gkasset")
				if _, err := CompileAuthoredAssetWithOptions(input, probe, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
					t.Fatal(err)
				}
				h, _, err := content.LoadCompiledAssetHeader(probe, nil)
				if err != nil {
					t.Fatal(err)
				}
				shape := h.Shapes[0]
				lod := h.LODs[0]
				if lod.DecodedBytes <= shape.DecodedBytes {
					t.Fatal("fixture must isolate larger derivative metadata", shape, lod)
				}
				codec = c3cCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxDecodedBytes: lod.DecodedBytes - 1}})
			}
			source := c3cRead(t, input)
			result, err := CompileAuthoredAssetWithOptions(input, output, codec, CompiledAssetCompileOptions{EnableLOD2: true})
			if err == nil || result != (CompiledAssetCompileDetailedResult{}) {
				t.Fatalf("failure returned success/partial: %+v err=%v", result, err)
			}
			if !bytes.Equal(old, c3cRead(t, output)) || !bytes.Equal(source, c3cRead(t, input)) {
				t.Fatal("failed publication changed source or old header")
			}
		})
	}
}

func TestC3h4MaterialAnimationEligibilityUsesPartLocalValues(t *testing.T) {
	input, output, a := c3h4Fixture(t)
	for i := range a.Parts {
		if a.Parts[i].ID == "duplicate" {
			p := a.Parts[i].Source.VoxelShape
			p.Palette[0].Value = 7
			for j := range p.Voxels {
				p.Voxels[j].Value = 7
			}
		}
	}
	a.MaterialAnimations = []content.AssetMaterialAnimationDef{{ID: "animated", Kind: "frames", PaletteIndices: []uint8{3}}}
	c3cWrite(t, input, a)
	result, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil || len(h.LODs) != 1 || h.LODs[0].PartID != "duplicate" || result.LODsWritten != 1 {
		t.Fatalf("animation eligibility not part local: header=%+v result=%+v err=%v", h, result, err)
	}
	lod, _, err := content.LoadCompiledAssetLOD(filepath.Join(filepath.Dir(output), filepath.FromSlash(h.LODs[0].Path)), nil)
	if err != nil || lod.Value != 7 {
		t.Fatal("wrong eligible value", err)
	}
}

func TestC3h4LODUsesPostScaleLevelZeroSource(t *testing.T) {
	input, output, a := c3h4Fixture(t)
	for i := range a.Parts {
		if a.Parts[i].Source.VoxelShape != nil {
			a.Parts[i].ModelScale = 2
		}
	}
	c3cWrite(t, input, a)
	if _, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
		t.Fatal(err)
	}
	h, _, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil || len(h.LODs) != 2 {
		t.Fatal("scaled LOD missing", err)
	}
	shape, si, err := content.LoadCompiledAssetShape(filepath.Join(filepath.Dir(output), filepath.FromSlash(h.Shapes[0].Path)), nil)
	if err != nil {
		t.Fatal(err)
	}
	lod, _, err := content.LoadCompiledAssetLOD(filepath.Join(filepath.Dir(output), filepath.FromSlash(h.LODs[0].Path)), nil)
	if err != nil {
		t.Fatal(err)
	}
	source := c3cSnapshot(shape).Voxels
	if len(source) <= 4 {
		t.Fatal("fixture did not exercise authored resampling")
	}
	want := map[[3]int]uint8{}
	var min, max [3]int64
	for i, v := range source {
		pos := [3]int{v.X, v.Y, v.Z}
		var coarse [3]int
		for axis, p := range pos {
			q := p / 2
			if p < 0 && p%2 != 0 {
				q--
			}
			coarse[axis] = q
			if i == 0 || int64(p) < min[axis] {
				min[axis] = int64(p)
			}
			if i == 0 || int64(p)+1 > max[axis] {
				max[axis] = int64(p) + 1
			}
		}
		want[coarse] = v.Value
	}
	got := c3cGeometry(c3cSnapshot(&content.CompiledAssetShapeDef{Bricks: lod.Bricks}).Voxels)
	if !reflect.DeepEqual(got, want) || lod.SourceVoxelCount != int64(len(source)) || lod.SourceMin != min || lod.SourceMax != max || lod.SourceLattice != shape.Lattice || lod.SourceContentID != si.ContentID {
		t.Fatalf("LOD not bound to post-scale emitted source: lod=%+v sourcecount=%d bounds=%v/%v", lod, len(source), min, max)
	}
}

func TestC3h4SameGeometryMaterialEligibilityIsPerPart(t *testing.T) {
	input, output, a := c3h4Fixture(t)
	for i := range a.Parts {
		if a.Parts[i].ID == "duplicate" {
			a.Parts[i].Source.VoxelShape.Palette[0].MaterialID = "unused"
		}
	}
	c3cWrite(t, input, a)
	result, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err != nil {
		t.Fatal(err)
	}
	h, _, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Shapes) != 2 || h.Shapes[0].ContentID != h.Shapes[1].ContentID || len(h.LODs) != 1 || h.LODs[0].PartID != "shape" || result.LODsWritten != 1 {
		t.Fatalf("material eligibility incorrectly shared by geometry: header=%+v result=%+v", h, result)
	}
}

func TestC3h4LODConflictPreflightPrecedesNewShapePublication(t *testing.T) {
	input, output, a := c3h4Fixture(t)
	if _, err := CompileAuthoredAsset(input, output, nil); err != nil {
		t.Fatal(err)
	}
	// Introduce new ordinary geometry alongside the existing geometry/LOD conflict.
	part := a.Parts[0]
	part.ID = "newshape"
	part.Name = "New shape"
	part.Source.VoxelShape = &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "mat"}}, Voxels: []content.VoxelObjectVoxelDef{{X: 100, Value: 3}, {X: 101, Value: 3}, {X: 102, Value: 3}, {X: 103, Value: 3}}}
	a.Parts = append(a.Parts, part)
	c3cWrite(t, input, a)
	probe := filepath.Join(t.TempDir(), "asset.gkasset")
	if _, err := CompileAuthoredAssetWithOptions(input, probe, nil, CompiledAssetCompileOptions{EnableLOD2: true}); err != nil {
		t.Fatal(err)
	}
	h, _, err := content.LoadCompiledAssetHeader(probe, nil)
	if err != nil {
		t.Fatal(err)
	}
	var conflictPath, newShapePath string
	for _, ref := range h.LODs {
		if ref.PartID == "shape" {
			conflictPath = filepath.Join(filepath.Dir(output), filepath.FromSlash(ref.Path))
		}
	}
	for _, ref := range h.Shapes {
		if ref.PartID == "newshape" {
			newShapePath = filepath.Join(filepath.Dir(output), filepath.FromSlash(ref.Path))
		}
	}
	if conflictPath == "" || newShapePath == "" {
		t.Fatal("probe fixture lacks conflict/new shape")
	}
	if _, err := os.Stat(newShapePath); !os.IsNotExist(err) {
		t.Fatal("new source shape already published", err)
	}
	if err := os.MkdirAll(filepath.Dir(conflictPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflictPath, []byte("conflicting derivative bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	before := c3h4Files(t, filepath.Dir(output))
	source := c3cRead(t, input)
	result, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err == nil || result != (CompiledAssetCompileDetailedResult{}) {
		t.Fatalf("conflict returned success/partial: %+v err=%v", result, err)
	}
	if !reflect.DeepEqual(before, c3h4Files(t, filepath.Dir(output))) || !bytes.Equal(source, c3cRead(t, input)) {
		t.Fatal("LOD conflict published a new ordinary/derivative file or changed prior bytes")
	}
}
