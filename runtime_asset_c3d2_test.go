package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func c3d2Resolve(t *testing.T, loader *RuntimeContentLoader, assetPath string, payload *content.VoxelObjectPayloadDef) (*content.VoxelObjectSnapshotDef, bool, error) {
	t.Helper()
	root := t.TempDir()
	payloadPath := filepath.Join(root, "override.gkvoxobj")
	if _, err := content.SaveVoxelObjectPayload(payloadPath, payload, nil); err != nil {
		t.Fatal(err)
	}
	override := content.VoxelObjectOverrideDef{PlacementID: payload.PlacementID, ItemID: payload.ItemID, SnapshotPath: payloadPath}
	return resolveStreamedVoxelObjectPayload(loader, []streamedPlacementInstance{{PlacementID: payload.PlacementID, AssetPath: assetPath}}, voxelObjectRuntimeKey(payload.PlacementID, payload.ItemID), override, filepath.Join(root, "level.gklevel"), filepath.Join(root, "delta.gkworlddelta"))
}

func c3d2Compiled(t *testing.T) (string, *content.AssetDef, *content.VoxelObjectSnapshotDef, content.VoxelObjectLatticeDef, string) {
	t.Helper()
	input, output, asset := c3cFixture(t)
	output = strings.TrimSuffix(output, ".gkasset") + ".gkassetc"
	if _, err := CompileAuthoredAsset(input, output, nil); err != nil {
		t.Fatal(err)
	}
	expected := VoxelObjectSnapshotFromXBrickMap(XBrickMapFromVoxelObjectSnapshot(&content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: asset.Parts[0].Source.VoxelShape.Voxels}).Resample(asset.Parts[0].ModelScale))
	lattice := authoredVoxelShapeLattice(asset.Parts[0].VoxelResolution)
	baseID, _, err := content.VoxelObjectBaseIdentity(expected, lattice, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	return output, asset, expected, lattice, baseID
}

func TestC3d2CompiledOriginalBaseSparseHybridAndOwnedSnapshots(t *testing.T) {
	path, _, expected, lattice, baseID := c3d2Compiled(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	for _, mode := range []string{content.VoxelObjectPayloadBaseDelta, content.VoxelObjectPayloadHybridDelta} {
		t.Run(mode, func(t *testing.T) {
			payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: mode, PlacementID: "placement", ItemID: "shape", Lattice: lattice, BaseIdentity: baseID, Voxels: []content.VoxelObjectVoxelDef{{X: -18, Value: 7}, {X: 2, Y: -2, Z: 2, Value: 0}}}
			if mode == content.VoxelObjectPayloadHybridDelta {
				payload.SchemaVersion = 3
				payload.ReplacementBricks = [][3]int32{{-3, 0, 0}}
				payload.Voxels = []content.VoxelObjectVoxelDef{{X: -18, Value: 7}}
			}
			want, err := content.ResolveVoxelObjectPayload(payload, expected, lattice, "placement", "shape", nil)
			if err != nil {
				t.Fatal(err)
			}
			first, bound, err := c3d2Resolve(t, owner, path, payload)
			if err != nil || !bound || !reflect.DeepEqual(c3cGeometry(first.Voxels), c3cGeometry(want.Voxels)) {
				t.Fatal("compiled delta differs from signed postscale canonical base", err)
			}
			first.Voxels[0].Value = 99
			again, bound, err := c3d2Resolve(t, owner, path, payload)
			if err != nil || !bound || !reflect.DeepEqual(c3cGeometry(again.Voxels), c3cGeometry(want.Voxels)) {
				t.Fatal("resolved snapshot aliased compiled cache or previous output", err)
			}
			if stats := owner.Stats(); stats.Entries != 0 || stats.PinnedBytes != 0 {
				t.Fatalf("verification leaked private scope pins: %+v", stats)
			}
		})
	}
}

func TestC3d2CompiledReferenceProofPreservesCallerPins(t *testing.T) {
	for _, kind := range []string{"content-id", "encoded-size", "decoded-size", "base-id", "lattice"} {
		t.Run(kind, func(t *testing.T) {
			path, _, expectedBase, lattice, baseID := c3d2Compiled(t)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			scope := owner.NewScope()
			defer scope.Close()
			accepted, _, err := scope.Loader().LoadCompiledAssetHeader(path)
			if err != nil {
				t.Fatal(err)
			}
			shapePath := filepath.Join(filepath.Dir(path), accepted.Shapes[0].Path)
			acceptedShape, _, err := scope.Loader().LoadCompiledAssetShape(shapePath)
			if err != nil {
				t.Fatal(err)
			}
			before := owner.Stats()
			bad, _, err := content.LoadCompiledAssetHeader(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "content-id":
				for i := range bad.Shapes {
					bad.Shapes[i].ContentID = strings.Repeat("c", 64)
				}
			case "encoded-size":
				for i := range bad.Shapes {
					bad.Shapes[i].EncodedBytes++
				}
			case "decoded-size":
				for i := range bad.Shapes {
					bad.Shapes[i].DecodedBytes++
				}
			case "base-id":
				for i := range bad.Shapes {
					bad.Shapes[i].BaseIdentity = strings.Repeat("c", 64)
				}
			case "lattice":
				bad.Asset.Parts[0].VoxelResolution *= 2
			}
			badPath := filepath.Join(filepath.Dir(path), "bad.gkassetc")
			if _, err := content.SaveCompiledAssetHeader(badPath, bad, nil); err != nil {
				t.Fatal(err)
			}
			payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadBaseDelta, PlacementID: "placement", ItemID: "shape", Lattice: lattice, BaseIdentity: baseID}
			if kind == "lattice" {
				payload.Lattice = authoredVoxelShapeLattice(bad.Asset.Parts[0].VoxelResolution)
				payload.BaseIdentity, _, err = content.VoxelObjectBaseIdentity(expectedBase, payload.Lattice, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			if result, _, err := c3d2Resolve(t, scope.Loader(), badPath, payload); err == nil || result != nil {
				t.Fatal("compiled reference proof accepted mismatch")
			}
			after := owner.Stats()
			if after.Entries != before.Entries || after.PinnedBytes != before.PinnedBytes || after.Bytes != before.Bytes {
				t.Fatalf("failed verification revoked caller pins or leaked children: before=%+v after=%+v", before, after)
			}
			if header, _, err := scope.Loader().LoadCompiledAssetHeader(path); err != nil || header != accepted {
				t.Fatal("preaccepted header pin was dropped", err)
			}
			if shape, _, err := scope.Loader().LoadCompiledAssetShape(shapePath); err != nil || shape != acceptedShape {
				t.Fatal("preaccepted SAME shape pin was dropped", err)
			}
		})
	}
}

func TestC3d2ExplicitSuffixAndClosedOrigin(t *testing.T) {
	path, asset, _, lattice, baseID := c3d2Compiled(t)
	payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadBaseDelta, PlacementID: "placement", ItemID: "shape", Lattice: lattice, BaseIdentity: baseID}
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	scope := owner.NewScope()
	scope.Close()
	if result, _, err := c3d2Resolve(t, scope.Loader(), path, payload); err == nil || result != nil {
		t.Fatal("closed origin was bypassed using a new verification scope")
	}
	jsonSelected := filepath.Join(filepath.Dir(path), "json.gkassetc")
	c3cWrite(t, jsonSelected, asset)
	if result, _, err := c3d2Resolve(t, owner, jsonSelected, payload); err == nil || result != nil {
		t.Fatal("selected compiled header fell back to authored JSON")
	}
	uppercase := filepath.Join(filepath.Dir(path), "asset.GKASSETC")
	if err := os.WriteFile(uppercase, c3cRead(t, path), 0600); err != nil {
		t.Fatal(err)
	}
	if result, _, err := c3d2Resolve(t, owner, uppercase, payload); err == nil || result != nil {
		t.Fatal("uppercase suffix autodetected compiled frame")
	}
	if def, err := owner.LoadAsset(path); err == nil || def != nil {
		t.Fatal("public legacy LoadAsset changed to autodetection")
	}
	legacy := filepath.Join(filepath.Dir(path), "legacy.gkasset")
	c3cWrite(t, legacy, asset)
	result, bound, err := c3d2Resolve(t, owner, legacy, payload)
	if err != nil || !bound || len(result.Voxels) == 0 {
		t.Fatal("legacy JSON canonical route changed", err)
	}
}

func c3d2SwitchManaged(t *testing.T, f *s1gFixture, eid EntityId) string {
	t.Helper()
	input := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
	output := filepath.Join(t.TempDir(), "placement.gkassetc")
	if _, err := CompileAuthoredAsset(input, output, nil); err != nil {
		t.Fatal(err)
	}
	ref := *s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid)
	ref.AssetPath = output
	f.cmd.AddComponents(eid, &ref)
	f.app.FlushCommands()
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = output
	f.runtime.Loader = NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	if err := os.Remove(input); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestC3d2CompiledManagedBindingAndDeltaRestoration(t *testing.T) {
	for _, restored := range []bool{false, true} {
		t.Run(map[bool]string{false: "binding", true: "restoration"}[restored], func(t *testing.T) {
			f, part, deltaPath := e2b2Runtime(t)
			placement := s1gID(0, 0)
			if restored {
				e2b2Save(t, f, deltaPath, e2b2Payload(t, part, placement, "body", content.VoxelObjectPayloadBaseDelta, 7, false))
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := e2b2Body(t, f, placement)
			original := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
			originalGeometry, ok := f.assets.getVoxelGeometry(original)
			if !ok {
				t.Fatal("source geometry missing")
			}
			originalValues := c3cGeometry(VoxelObjectSnapshotFromXBrickMap(originalGeometry.XBrickMap).Voxels)
			c3d2SwitchManaged(t, f, eid)
			managedID := e2b3Enable(t, f, eid)
			e2b3Qualified(t, f, eid, placement, part)
			entry := f.assets.managedVoxelEntry(managedID)
			base, lattice, baseID := e2b1Canonical(t, part)
			_, decodedBytes, err := content.VoxelObjectBaseIdentity(base, lattice, nil)
			if err != nil {
				t.Fatal(err)
			}
			if entry.persistenceBinding == nil || entry.persistenceBinding.baseVoxels != len(base.Voxels) || entry.persistenceBinding.baseBricks != persistenceMapBrickCount(XBrickMapFromVoxelObjectSnapshot(base)) || entry.persistenceBinding.baseDecodedBytes != decodedBytes || entry.authoredBase.identity != baseID {
				t.Fatal("compiled binding lost original base counts/identity")
			}
			if restored {
				changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, eid)
				if !tracked || !reflect.DeepEqual(changes, []volume.VoxelWrite{{X: -9, Value: 7}, {}}) {
					t.Fatal("compiled restoration lost original assignment history", changes)
				}
			}
			if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: 1, Value: 9})); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(originalGeometry.XBrickMap).Voxels), originalValues) {
				t.Fatal("compiled base/restoration edit mutated original borrowed source")
			}
			e2b3Qualified(t, f, eid, placement, part)
			f.runtime.Generation++
			e2b3Fallback(t, f, f.runtime, eid, placement, "body")
		})
	}
}

func TestC3d2DefaultE2LimitsAndFullReplacementAvoidUnusedBase(t *testing.T) {
	root := t.TempDir()
	codec := c3cCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxBricks: 16385}})
	shape := &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: content.VoxelObjectLatticeDef{VoxelResolution: 1, RasterizationVersion: "gekko-voxel-shape-v1"}}
	for i := 0; i < 16385; i++ {
		shape.Bricks = append(shape.Bricks, voxelcodec.Brick{Coord: [3]int32{int32(i * 2), 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{1}})
	}
	shapePath := filepath.Join(root, "large.gkshape")
	shapeInfo, err := content.SaveCompiledAssetShape(shapePath, shape, codec)
	if err != nil {
		t.Fatal(err)
	}
	baseID, _, err := content.CompiledAssetShapeBaseIdentity(shape, codec)
	if err != nil {
		t.Fatal(err)
	}
	header := &content.CompiledAssetHeaderDef{SchemaVersion: 1, CompilerVersion: content.CurrentCompiledAssetCompilerVersion, Asset: &content.AssetDef{ID: "asset", SchemaVersion: 4, Name: "Large", Materials: []content.AssetMaterialDef{{ID: "mat", Name: "Material", IOR: 1.5}}, Parts: []content.AssetPartDef{{ID: "body", Name: "Body", ModelScale: 1, VoxelResolution: 1, Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "mat"}}}}}}}, Shapes: []content.CompiledAssetShapeRefDef{{PartID: "body", Path: "large.gkshape", ContentID: shapeInfo.ContentID, BaseIdentity: baseID, EncodedBytes: shapeInfo.EncodedBytes, DecodedBytes: shapeInfo.DecodedBytes}}}
	headerPath := filepath.Join(root, "large.gkassetc")
	if _, err := content.SaveCompiledAssetHeader(headerPath, header, codec); err != nil {
		t.Fatal(err)
	}
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec, MaxCacheBytes: -1})
	delta := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadBaseDelta, PlacementID: "placement", ItemID: "body", Lattice: shape.Lattice, BaseIdentity: baseID}
	if result, _, err := c3d2Resolve(t, owner, headerPath, delta); err == nil || result != nil {
		t.Fatal("larger compiled profile expanded an E2 base beyond default proof bounds")
	}
	if stats := owner.Stats(); stats.Entries != 0 || stats.PinnedBytes != 0 {
		t.Fatal("oversized base failure leaked verification pins")
	}
	if err := os.Remove(shapePath); err != nil {
		t.Fatal(err)
	}
	full := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: "placement", ItemID: "body", Lattice: shape.Lattice, Voxels: []content.VoxelObjectVoxelDef{{X: -1, Value: 7}}}
	result, bound, err := c3d2Resolve(t, owner, headerPath, full)
	if err != nil || !bound || !reflect.DeepEqual(result.Voxels, full.Voxels) {
		t.Fatal("full replacement unnecessarily loaded or admitted unused oversized base", err)
	}
}

func TestC3d2WarmCanonicalVerificationSurvivesCodecClose(t *testing.T) {
	path, _, expected, lattice, baseID := c3d2Compiled(t)
	codec := c3cCodec(t, voxelcodec.Options{})
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec, MaxCacheBytes: -1})
	scope := owner.NewScope()
	defer scope.Close()
	header, _, err := scope.Loader().LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	shapePath := filepath.Join(filepath.Dir(path), header.Shapes[0].Path)
	shape, _, err := scope.Loader().LoadCompiledAssetShape(shapePath)
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	if err := codec.Close(); err != nil {
		t.Fatal(err)
	}
	payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadBaseDelta, PlacementID: "placement", ItemID: "shape", Lattice: lattice, BaseIdentity: baseID, Voxels: []content.VoxelObjectVoxelDef{{X: -18, Value: 7}}}
	want, err := content.ResolveVoxelObjectPayload(payload, expected, lattice, "placement", "shape", nil)
	if err != nil {
		t.Fatal(err)
	}
	actual, bound, err := c3d2Resolve(t, scope.Loader(), path, payload)
	if err != nil || !bound || !reflect.DeepEqual(c3cGeometry(actual.Voxels), c3cGeometry(want.Voxels)) {
		t.Fatal("warm verified canonical base depends on closed borrowed codec", err)
	}
	after := owner.Stats()
	if after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("warm canonical verification revoked or leaked pins")
	}
	if got, _, err := scope.Loader().LoadCompiledAssetHeader(path); err != nil || got != header {
		t.Fatal("warm header pin changed", err)
	}
	if got, _, err := scope.Loader().LoadCompiledAssetShape(shapePath); err != nil || got != shape {
		t.Fatal("warm shape pin changed", err)
	}
}
