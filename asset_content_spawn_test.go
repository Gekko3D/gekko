package gekko

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSpawnAuthoredAssetResolvesChildBeforeParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("ordered")
	parent := content.AssetPartDef{
		ID:     "parent",
		Name:   "parent",
		Source: testProceduralPartSource(),
		Transform: content.AssetTransformDef{
			Position: content.Vec3{5, 0, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}
	child := content.AssetPartDef{
		ID:       "child",
		Name:     "child",
		ParentID: "parent",
		Source:   testProceduralPartSource(),
		Transform: content.AssetTransformDef{
			Position: content.Vec3{0, 2, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}
	def.Parts = []content.AssetPartDef{child, parent}

	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{
		Position: mgl32.Vec3{0, 0, 0},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()
	childEntity := result.EntitiesByAssetID["child"]
	if childEntity == 0 {
		t.Fatal("expected child entity mapping")
	}

	var childWorld *TransformComponent
	for _, comp := range cmd.GetAllComponents(childEntity) {
		if tr, ok := comp.(TransformComponent); ok {
			childWorld = &tr
		}
	}
	if childWorld == nil {
		t.Fatal("expected child world transform")
	}
	if got := childWorld.Position; got.Sub(mgl32.Vec3{5, 2, 0}).Len() > 1e-5 {
		t.Fatalf("expected child world position [5 2 0], got %v", got)
	}
}

func TestSpawnAuthoredAssetRejectsMissingParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("invalid")
	def.Parts = []content.AssetPartDef{{
		ID:       "child",
		Name:     "child",
		ParentID: "missing",
		Source:   testProceduralPartSource(),
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}

	if _, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}); err == nil {
		t.Fatal("expected missing parent to be rejected")
	}
}

func TestSpawnAuthoredAssetRejectsCycle(t *testing.T) {
	def := content.NewAssetDef("cycle")
	def.Parts = []content.AssetPartDef{
		{ID: "a", Name: "a", ParentID: "b", Source: testProceduralPartSource()},
		{ID: "b", Name: "b", ParentID: "a", Source: testProceduralPartSource()},
	}
	if err := ValidateAssetHierarchy(def); err == nil {
		t.Fatal("expected cycle to be rejected")
	}
}

func TestSpawnAuthoredAssetRejectsInvalidSourcePayload(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("invalid-source")
	def.Parts = []content.AssetPartDef{{
		ID:   "part",
		Name: "part",
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxModel,
		},
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}

	if _, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}); err == nil {
		t.Fatal("expected invalid source payload to be rejected")
	}
}

func TestSpawnAuthoredAssetSpawnsPartLightEmitterAndMarkerHierarchy(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := representativeAuthoredAssetForTest()
	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()

	if result.AssetID != def.ID {
		t.Fatalf("expected result asset ID %q, got %q", def.ID, result.AssetID)
	}
	if _, ok := result.PartIDs["root-part"]; !ok {
		t.Fatal("expected part id map to include root-part")
	}

	wantKinds := map[string]AuthoredItemKind{
		"root-part":  AuthoredItemKindPart,
		"child-part": AuthoredItemKindPart,
		"light":      AuthoredItemKindLight,
		"emitter":    AuthoredItemKindEmitter,
		"marker":     AuthoredItemKindMarker,
	}
	for assetID, wantKind := range wantKinds {
		eid := result.EntitiesByAssetID[assetID]
		if eid == 0 {
			t.Fatalf("expected entity mapping for %s", assetID)
		}
		if result.ItemKindsByAssetID[assetID] != wantKind {
			t.Fatalf("expected kind %q for %s, got %q", wantKind, assetID, result.ItemKindsByAssetID[assetID])
		}
		assertAuthoredRefForTest(t, cmd, eid, def.ID, assetID, wantKind)
	}

	rootPartEntity := result.EntitiesByAssetID["root-part"]
	rootParent, ok := parentEntityForTest(cmd, rootPartEntity)
	if !ok || rootParent != result.RootEntity {
		t.Fatalf("expected root part to be parented to root entity %d, got %d present=%v", result.RootEntity, rootParent, ok)
	}

	childPartParent, ok := parentEntityForTest(cmd, result.EntitiesByAssetID["child-part"])
	if !ok || childPartParent != result.EntitiesByAssetID["root-part"] {
		t.Fatalf("expected child part parented to root-part entity, got %d present=%v", childPartParent, ok)
	}

	for _, childAssetID := range []string{"light", "emitter", "marker"} {
		parentEntity, ok := parentEntityForTest(cmd, result.EntitiesByAssetID[childAssetID])
		if !ok || parentEntity != result.EntitiesByAssetID["child-part"] {
			t.Fatalf("expected %s parented to child-part entity, got %d present=%v", childAssetID, parentEntity, ok)
		}
	}

	marker, ok := AuthoredMarkerForEntity(cmd, result.EntitiesByAssetID["marker"])
	if !ok {
		t.Fatal("expected marker metadata component")
	}
	if marker.Kind != "socket" {
		t.Fatalf("expected marker kind socket, got %q", marker.Kind)
	}
	if len(marker.Tags) != 2 || marker.Tags[0] != "attach" || marker.Tags[1] != "hand" {
		t.Fatalf("unexpected marker tags %+v", marker.Tags)
	}

	if !IsAuthoredAssetRootEntity(cmd, result.RootEntity) {
		t.Fatal("expected shared root entity to carry authored root metadata")
	}
}

func TestSpawnAuthoredAssetCollapsesOptedInVoxelPartsIntoSingleRuntimeModel(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	def := content.NewAssetDef("collapsed")
	def.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	def.Parts = []content.AssetPartDef{
		{
			ID:     "root-part",
			Name:   "root-part",
			Source: testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:       "child-part",
			Name:     "child-part",
			ParentID: "root-part",
			Source: content.AssetSourceDef{
				Kind:      content.AssetSourceKindProceduralPrimitive,
				Primitive: "sphere",
				Params: map[string]float32{
					"radius": 1.5,
				},
			},
			Transform: content.AssetTransformDef{
				Position: content.Vec3{1.5, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	result, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{CollapseVoxelParts: VoxelPartCollapseForce})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()

	if !result.Collapsed {
		t.Fatal("expected authored asset to collapse")
	}
	if len(result.EntitiesByAssetID) != 0 {
		t.Fatalf("expected no per-part runtime entities, got %+v", result.EntitiesByAssetID)
	}
	if _, ok := result.CollapsedPartIDs["root-part"]; !ok {
		t.Fatal("expected collapsed part ids to include root-part")
	}
	if _, ok := result.CollapsedPartIDs["child-part"]; !ok {
		t.Fatal("expected collapsed part ids to include child-part")
	}

	collapsedMeta, ok := collapsedVoxelPartsForSpawnTest(cmd, result.RootEntity)
	if !ok {
		t.Fatal("expected collapsed voxel metadata on root")
	}
	if len(collapsedMeta.PartIDs) != 2 {
		t.Fatalf("expected 2 collapsed part ids, got %+v", collapsedMeta.PartIDs)
	}

	voxelEntity := onlyVoxelEntityForSpawnTest(t, cmd, result.RootEntity)
	if _, ok := AuthoredAssetRefForEntity(cmd, voxelEntity); ok {
		t.Fatal("expected collapsed voxel entity to omit authored item refs")
	}
	parent, ok := parentEntityForTest(cmd, voxelEntity)
	if !ok || parent != result.RootEntity {
		t.Fatalf("expected collapsed voxel child parented to root %d, got %d present=%v", result.RootEntity, parent, ok)
	}
}

func TestSpawnAuthoredAssetCollapseFallsBackForMarkerAssets(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	def := representativeAuthoredAssetForTest()
	def.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}

	result, err := SpawnAuthoredAsset(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()

	if result.Collapsed {
		t.Fatal("expected mixed authored asset to fall back to normal spawn")
	}
	if len(result.EntitiesByAssetID) == 0 {
		t.Fatal("expected normal authored items to be spawned")
	}
	if _, ok := result.EntitiesByAssetID["marker"]; !ok {
		t.Fatal("expected marker entity mapping after fallback")
	}
}

func TestSpawnAuthoredAssetCollapseForceRejectsIneligibleAsset(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	def := representativeAuthoredAssetForTest()
	def.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}

	_, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{CollapseVoxelParts: VoxelPartCollapseForce})
	if err == nil {
		t.Fatal("expected force collapse to reject ineligible asset")
	}
}

func TestLoadAndSpawnAuthoredAssetMatchesDirectSpawn(t *testing.T) {
	withTempAssetFile(t, representativeAuthoredAssetForTest(), func(path string, def *content.AssetDef) {
		rootTransform := TransformComponent{
			Position: mgl32.Vec3{4, 5, 6},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		}

		directApp := NewApp()
		directCmd := directApp.Commands()
		directResult, err := SpawnAuthoredAsset(directCmd, nil, def, rootTransform)
		if err != nil {
			t.Fatalf("SpawnAuthoredAsset failed: %v", err)
		}
		directApp.FlushCommands()

		loadedApp := NewApp()
		loadedCmd := loadedApp.Commands()
		loadedResult, err := LoadAndSpawnAuthoredAsset(path, loadedCmd, nil, rootTransform)
		if err != nil {
			t.Fatalf("LoadAndSpawnAuthoredAsset failed: %v", err)
		}
		loadedApp.FlushCommands()

		assertSpawnStructuresMatch(t, directCmd, directResult, loadedCmd, loadedResult)
	})
}

func TestLoadAndSpawnAuthoredAssetResolvesSourcePathsRelativeToAssetDocument(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "library", "assets", "relative_source.gkasset")
	voxPath := filepath.Join(root, "library", "models", "human.vox")
	copySpawnTestVoxFixture(t, voxPath)

	def := content.NewAssetDef("relative-source")
	def.Parts = []content.AssetPartDef{{
		ID:   "part",
		Name: "part",
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxModel,
			Path: filepath.Join("..", "models", "human.vox"),
		},
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}
	content.EnsureAssetIDs(def)
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := content.SaveAsset(assetPath, def); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()
	result, err := LoadAndSpawnAuthoredAsset(assetPath, cmd, assets, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("LoadAndSpawnAuthoredAsset failed: %v", err)
	}
	app.FlushCommands()

	entity := result.EntitiesByAssetID["part"]
	if entity == 0 {
		t.Fatal("expected spawned entity for part")
	}
	if _, ok := voxelModelForSpawnTest(cmd, entity); !ok {
		t.Fatal("expected relative vox source to load into preview/runtime model component")
	}
}

func TestSpawnAuthoredAssetWithOptionsResolvesVoxSceneNodeSingleModelNode(t *testing.T) {
	assetPath, assets, result, cmd, err := spawnSceneNodeAssetForTest(t, content.AssetSourceDef{
		Kind:       content.AssetSourceKindVoxSceneNode,
		Path:       "scene.vox",
		NodeName:   "arm",
		ModelIndex: -1,
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAssetWithOptions failed for %s: %v", assetPath, err)
	}

	model := mustSpawnedVoxelAssetForTest(t, cmd, assets, result, "part")
	if model.VoxModel.SizeX != 4 || model.VoxModel.SizeY != 2 || model.VoxModel.SizeZ != 2 {
		t.Fatalf("expected arm model dimensions 4x2x2, got %+v", model.VoxModel)
	}
}

func TestSpawnAuthoredAssetWithOptionsResolvesVoxSceneNodeSubtreeModelIndex(t *testing.T) {
	_, assets, result, cmd, err := spawnSceneNodeAssetForTest(t, content.AssetSourceDef{
		Kind:       content.AssetSourceKindVoxSceneNode,
		Path:       "scene.vox",
		NodeName:   "body",
		ModelIndex: 1,
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAssetWithOptions failed: %v", err)
	}

	model := mustSpawnedVoxelAssetForTest(t, cmd, assets, result, "part")
	if model.VoxModel.SizeX != 4 || model.VoxModel.SizeY != 2 || model.VoxModel.SizeZ != 2 {
		t.Fatalf("expected body subtree model_index 1 to resolve arm model, got %+v", model.VoxModel)
	}
}

func TestSpawnAuthoredAssetWithOptionsSupportsGroupPartWithoutGeometry(t *testing.T) {
	_, _, result, cmd, err := spawnSceneNodeAssetForTest(t, content.AssetSourceDef{
		Kind: content.AssetSourceKindGroup,
	})
	if err != nil {
		t.Fatalf("SpawnAuthoredAssetWithOptions failed: %v", err)
	}

	entity := result.EntitiesByAssetID["part"]
	if entity == 0 {
		t.Fatal("expected spawned entity for group part")
	}
	if _, ok := voxelModelForSpawnTest(cmd, entity); ok {
		t.Fatal("expected group part to spawn without voxel model geometry")
	}
	if mustWorldTransformForSpawnTest(t, cmd, entity).Scale != (mgl32.Vec3{1, 1, 1}) {
		t.Fatal("expected group part to retain authored transform")
	}
}

func TestSpawnAuthoredAssetWithOptionsRejectsInvalidVoxSceneNodeResolution(t *testing.T) {
	tests := []struct {
		name      string
		source    content.AssetSourceDef
		writeVox  func(t *testing.T, path string)
		wantError string
	}{
		{
			name: "missing node name",
			source: content.AssetSourceDef{
				Kind:       content.AssetSourceKindVoxSceneNode,
				Path:       "scene.vox",
				NodeName:   "missing",
				ModelIndex: -1,
			},
			writeVox:  writeNamedSceneVoxFixture,
			wantError: `node_name "missing" not found`,
		},
		{
			name: "duplicate node name",
			source: content.AssetSourceDef{
				Kind:       content.AssetSourceKindVoxSceneNode,
				Path:       "scene.vox",
				NodeName:   "arm",
				ModelIndex: -1,
			},
			writeVox:  writeDuplicateNameSceneVoxFixture,
			wantError: `node_name "arm" is ambiguous`,
		},
		{
			name: "model outside subtree",
			source: content.AssetSourceDef{
				Kind:       content.AssetSourceKindVoxSceneNode,
				Path:       "scene.vox",
				NodeName:   "arm",
				ModelIndex: 0,
			},
			writeVox:  writeNamedSceneVoxFixture,
			wantError: `does not contain model_index 0`,
		},
		{
			name: "ambiguous subtree needs model index",
			source: content.AssetSourceDef{
				Kind:       content.AssetSourceKindVoxSceneNode,
				Path:       "scene.vox",
				NodeName:   "body",
				ModelIndex: -1,
			},
			writeVox:  writeNamedSceneVoxFixture,
			wantError: `model_index is required`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, _, err := spawnSceneNodeAssetForTestWithWriter(t, tc.source, tc.writeVox)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected error containing %q, got %v", tc.wantError, err)
			}
		})
	}
}

func TestSpawnAuthoredAssetCompositeGoldenResolvesChildBeforeParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := loadGoldenAssetForSpawnTest(t, "composite_authored_asset.gkasset")
	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()

	if result.AssetID != "asset-composite" {
		t.Fatalf("expected asset-composite id, got %q", result.AssetID)
	}
	if result.ItemKindsByAssetID["marker-child"] != AuthoredItemKindMarker {
		t.Fatalf("expected marker kind mapping for golden asset, got %+v", result.ItemKindsByAssetID)
	}

	childWorld := mustWorldTransformForSpawnTest(t, cmd, result.EntitiesByAssetID["part-child"])
	if got := childWorld.Position; got.Sub(mgl32.Vec3{10, 2, 0}).Len() > 1e-5 {
		t.Fatalf("expected child world position [10 2 0], got %v", got)
	}

	marker, ok := AuthoredMarkerForEntity(cmd, result.EntitiesByAssetID["marker-child"])
	if !ok {
		t.Fatal("expected marker metadata on spawned golden asset")
	}
	if marker.Kind != content.AssetMarkerKindMuzzle {
		t.Fatalf("expected muzzle marker kind, got %q", marker.Kind)
	}
}

func TestLoadAndSpawnAuthoredAssetCompositeGoldenMatchesDirectSpawn(t *testing.T) {
	def := loadGoldenAssetForSpawnTest(t, "composite_authored_asset.gkasset")
	path := goldenAssetPathForSpawnTest(t, "composite_authored_asset.gkasset")
	rootTransform := TransformComponent{
		Position: mgl32.Vec3{4, 5, 6},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}

	directApp := NewApp()
	directCmd := directApp.Commands()
	directResult, err := SpawnAuthoredAsset(directCmd, nil, def, rootTransform)
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset failed: %v", err)
	}
	directApp.FlushCommands()

	loadedApp := NewApp()
	loadedCmd := loadedApp.Commands()
	loadedResult, err := LoadAndSpawnAuthoredAsset(path, loadedCmd, nil, rootTransform)
	if err != nil {
		t.Fatalf("LoadAndSpawnAuthoredAsset failed: %v", err)
	}
	loadedApp.FlushCommands()

	assertSpawnStructuresMatch(t, directCmd, directResult, loadedCmd, loadedResult)
}

func representativeAuthoredAssetForTest() *content.AssetDef {
	def := content.NewAssetDef("parity")
	def.Parts = []content.AssetPartDef{
		{
			ID:     "root-part",
			Name:   "root-part",
			Source: testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Position: content.Vec3{1, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:       "child-part",
			Name:     "child-part",
			ParentID: "root-part",
			Source:   testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Position: content.Vec3{0, 2, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}
	def.Lights = []content.AssetLightDef{{
		ID:       "light",
		Name:     "light",
		ParentID: "child-part",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{0, 3, 0},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Type: content.AssetLightTypePoint,
	}}
	def.Emitters = []content.AssetEmitterDef{{
		ID:       "emitter",
		Name:     "emitter",
		ParentID: "child-part",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{0, 0, 2},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Emitter: content.EmitterDef{Enabled: true, AlphaMode: content.AssetAlphaModeTexture},
	}}
	def.Markers = []content.AssetMarkerDef{{
		ID:       "marker",
		Name:     "marker",
		ParentID: "child-part",
		Transform: content.AssetTransformDef{
			Position: content.Vec3{1, 0, 1},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Kind: "socket",
		Tags: []string{"attach", "hand"},
	}}
	return def
}

func hasParentComponentForTest(cmd *Commands, eid EntityId) bool {
	for _, comp := range cmd.GetAllComponents(eid) {
		if _, ok := comp.(*Parent); ok {
			return true
		}
		if _, ok := comp.(Parent); ok {
			return true
		}
	}
	return false
}

func parentEntityForTest(cmd *Commands, eid EntityId) (EntityId, bool) {
	for _, comp := range cmd.GetAllComponents(eid) {
		if parent, ok := comp.(*Parent); ok {
			return parent.Entity, true
		}
		if parent, ok := comp.(Parent); ok {
			return parent.Entity, true
		}
	}
	return 0, false
}

func assertAuthoredRefForTest(t *testing.T, cmd *Commands, eid EntityId, assetID string, itemID string, kind AuthoredItemKind) {
	t.Helper()
	ref, ok := AuthoredAssetRefForEntity(cmd, eid)
	if !ok {
		t.Fatalf("expected authored ref on entity %d", eid)
	}
	if ref.AssetID != assetID || ref.ItemID != itemID || ref.Kind != kind {
		t.Fatalf("unexpected authored ref %+v", ref)
	}
}

func assertSpawnStructuresMatch(t *testing.T, leftCmd *Commands, leftResult AuthoredAssetSpawnResult, rightCmd *Commands, rightResult AuthoredAssetSpawnResult) {
	t.Helper()
	if leftResult.AssetID != rightResult.AssetID {
		t.Fatalf("asset IDs differ: %q vs %q", leftResult.AssetID, rightResult.AssetID)
	}
	if len(leftResult.EntitiesByAssetID) != len(rightResult.EntitiesByAssetID) {
		t.Fatalf("entity counts differ: %d vs %d", len(leftResult.EntitiesByAssetID), len(rightResult.EntitiesByAssetID))
	}
	for assetID, leftEntity := range leftResult.EntitiesByAssetID {
		rightEntity := rightResult.EntitiesByAssetID[assetID]
		if rightEntity == 0 {
			t.Fatalf("missing entity for asset %s in right result", assetID)
		}

		leftRef, ok := AuthoredAssetRefForEntity(leftCmd, leftEntity)
		if !ok {
			t.Fatalf("missing left authored ref for %s", assetID)
		}
		rightRef, ok := AuthoredAssetRefForEntity(rightCmd, rightEntity)
		if !ok {
			t.Fatalf("missing right authored ref for %s", assetID)
		}
		if leftRef != rightRef {
			t.Fatalf("authored refs differ for %s: %+v vs %+v", assetID, leftRef, rightRef)
		}

		leftParentID := authoredParentIDForTest(leftCmd, leftResult.RootEntity, leftEntity)
		rightParentID := authoredParentIDForTest(rightCmd, rightResult.RootEntity, rightEntity)
		if leftParentID != rightParentID {
			t.Fatalf("parent IDs differ for %s: %q vs %q", assetID, leftParentID, rightParentID)
		}

		if leftRef.Kind == AuthoredItemKindMarker {
			leftMarker, ok := AuthoredMarkerForEntity(leftCmd, leftEntity)
			if !ok {
				t.Fatalf("missing left marker metadata for %s", assetID)
			}
			rightMarker, ok := AuthoredMarkerForEntity(rightCmd, rightEntity)
			if !ok {
				t.Fatalf("missing right marker metadata for %s", assetID)
			}
			if leftMarker.Kind != rightMarker.Kind || len(leftMarker.Tags) != len(rightMarker.Tags) {
				t.Fatalf("marker metadata differs for %s: %+v vs %+v", assetID, leftMarker, rightMarker)
			}
		}
	}
}

func authoredParentIDForTest(cmd *Commands, rootEntity EntityId, eid EntityId) string {
	parentEntity, ok := parentEntityForTest(cmd, eid)
	if !ok || parentEntity == rootEntity || IsAuthoredAssetRootEntity(cmd, parentEntity) {
		return ""
	}
	ref, ok := AuthoredAssetRefForEntity(cmd, parentEntity)
	if !ok {
		return ""
	}
	return ref.ItemID
}

func withTempAssetFile(t *testing.T, def *content.AssetDef, fn func(path string, def *content.AssetDef)) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "asset.gkasset")
	if err := content.SaveAsset(path, def); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}
	fn(path, def)
}

func testProceduralPartSource() content.AssetSourceDef {
	return content.AssetSourceDef{
		Kind:      content.AssetSourceKindProceduralPrimitive,
		Primitive: "cube",
		Params: map[string]float32{
			"sx": 1,
			"sy": 1,
			"sz": 1,
		},
	}
}

func loadGoldenAssetForSpawnTest(t *testing.T, fileName string) *content.AssetDef {
	t.Helper()
	def, err := content.LoadAsset(goldenAssetPathForSpawnTest(t, fileName))
	if err != nil {
		t.Fatalf("LoadAsset(%s) failed: %v", fileName, err)
	}
	return def
}

func goldenAssetPathForSpawnTest(t *testing.T, fileName string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(currentFile), "content", "testdata", fileName)
}

func copySpawnTestVoxFixture(t *testing.T, dst string) {
	t.Helper()
	src := filepath.Join(repoRootForSpawnTest(t), "gekko-editor", "assets", "human.vox")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile(%s) failed: %v", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatalf("MkdirAll(%s) failed: %v", dst, err)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		t.Fatalf("WriteFile(%s) failed: %v", dst, err)
	}
}

func repoRootForSpawnTest(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(currentFile))
}

func newSpawnTestAssetServer() *AssetServer {
	return &AssetServer{
		voxModels:      make(map[AssetId]VoxelModelAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
	}
}

func voxelModelForSpawnTest(cmd *Commands, eid EntityId) (VoxelModelComponent, bool) {
	for _, comp := range cmd.GetAllComponents(eid) {
		if model, ok := comp.(VoxelModelComponent); ok {
			return model, true
		}
		if model, ok := comp.(*VoxelModelComponent); ok {
			return *model, true
		}
	}
	return VoxelModelComponent{}, false
}

func mustVoxelModelForSpawnTest(t *testing.T, cmd *Commands, eid EntityId) VoxelModelComponent {
	t.Helper()
	model, ok := voxelModelForSpawnTest(cmd, eid)
	if !ok {
		t.Fatalf("missing voxel model for entity %d", eid)
	}
	return model
}

func mustWorldTransformForSpawnTest(t *testing.T, cmd *Commands, eid EntityId) TransformComponent {
	t.Helper()
	for _, comp := range cmd.GetAllComponents(eid) {
		if tr, ok := comp.(TransformComponent); ok {
			return tr
		}
		if tr, ok := comp.(*TransformComponent); ok {
			return *tr
		}
	}
	t.Fatalf("missing world transform for entity %d", eid)
	return TransformComponent{}
}

func collapsedVoxelPartsForSpawnTest(cmd *Commands, eid EntityId) (CollapsedAuthoredVoxelPartsComponent, bool) {
	for _, comp := range cmd.GetAllComponents(eid) {
		if collapsed, ok := comp.(CollapsedAuthoredVoxelPartsComponent); ok {
			return collapsed, true
		}
		if collapsed, ok := comp.(*CollapsedAuthoredVoxelPartsComponent); ok {
			return *collapsed, true
		}
	}
	return CollapsedAuthoredVoxelPartsComponent{}, false
}

func onlyVoxelEntityForSpawnTest(t *testing.T, cmd *Commands, root EntityId) EntityId {
	t.Helper()
	var found EntityId
	MakeQuery1[VoxelModelComponent](cmd).Map(func(eid EntityId, _ *VoxelModelComponent) bool {
		parent, ok := parentEntityForTest(cmd, eid)
		if ok && parent == root {
			if found != 0 {
				t.Fatalf("expected only one voxel child under root %d", root)
			}
			found = eid
		}
		return true
	})
	if found == 0 {
		t.Fatalf("expected voxel child under root %d", root)
	}
	return found
}

func spawnSceneNodeAssetForTest(t *testing.T, source content.AssetSourceDef) (string, *AssetServer, AuthoredAssetSpawnResult, *Commands, error) {
	t.Helper()
	return spawnSceneNodeAssetForTestWithWriter(t, source, writeNamedSceneVoxFixture)
}

func spawnSceneNodeAssetForTestWithWriter(t *testing.T, source content.AssetSourceDef, writeVox func(t *testing.T, path string)) (string, *AssetServer, AuthoredAssetSpawnResult, *Commands, error) {
	t.Helper()
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "scene_asset.gkasset")
	voxPath := filepath.Join(root, "assets", "scene.vox")
	writeVox(t, voxPath)

	def := content.NewAssetDef("scene-source")
	def.Parts = []content.AssetPartDef{{
		ID:     "part",
		Name:   "part",
		Source: source,
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	}}
	content.EnsureAssetIDs(def)
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := content.SaveAsset(assetPath, def); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()
	result, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{DocumentPath: assetPath})
	if err == nil {
		app.FlushCommands()
	}
	return assetPath, assets, result, cmd, err
}

func mustSpawnedVoxelAssetForTest(t *testing.T, cmd *Commands, assets *AssetServer, result AuthoredAssetSpawnResult, assetID string) VoxelModelAsset {
	t.Helper()
	entity := result.EntitiesByAssetID[assetID]
	if entity == 0 {
		t.Fatalf("expected spawned entity for %s", assetID)
	}
	modelComp, ok := voxelModelForSpawnTest(cmd, entity)
	if !ok {
		t.Fatalf("expected voxel model component for %s", assetID)
	}
	model, ok := assets.GetVoxelModel(modelComp.GeometryAsset())
	if !ok {
		t.Fatalf("expected voxel model asset for %s", assetID)
	}
	return model
}

func TestSpawnAuthoredAssetReusesGeometryAndPaletteAssetsBySource(t *testing.T) {
	root := t.TempDir()
	assetPath := filepath.Join(root, "assets", "ship.gkasset")
	voxPath := filepath.Join(root, "assets", "ship.vox")
	writeNamedSceneVoxFixture(t, voxPath)

	def := content.NewAssetDef("ship")
	def.Parts = []content.AssetPartDef{{
		ID:   "part",
		Name: "part",
		Source: content.AssetSourceDef{
			Kind:       content.AssetSourceKindVoxModel,
			Path:       "ship.vox",
			ModelIndex: 0,
		},
	}}
	content.EnsureAssetIDs(def)
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := content.SaveAsset(assetPath, def); err != nil {
		t.Fatalf("SaveAsset failed: %v", err)
	}

	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	first, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{DocumentPath: assetPath})
	if err != nil {
		t.Fatalf("first spawn failed: %v", err)
	}
	second, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{DocumentPath: assetPath})
	if err != nil {
		t.Fatalf("second spawn failed: %v", err)
	}
	app.FlushCommands()

	firstComp, ok := voxelModelForSpawnTest(cmd, first.EntitiesByAssetID["part"])
	if !ok {
		t.Fatal("expected first voxel component")
	}
	secondComp, ok := voxelModelForSpawnTest(cmd, second.EntitiesByAssetID["part"])
	if !ok {
		t.Fatal("expected second voxel component")
	}

	if firstComp.GeometryAsset() != secondComp.GeometryAsset() {
		t.Fatalf("expected repeated spawns to reuse geometry asset, got %v and %v", firstComp.GeometryAsset(), secondComp.GeometryAsset())
	}
	if firstComp.VoxelPalette != secondComp.VoxelPalette {
		t.Fatalf("expected repeated spawns to reuse palette asset, got %v and %v", firstComp.VoxelPalette, secondComp.VoxelPalette)
	}
}

func TestSpawnAuthoredAssetCollapsedSpawnsReuseCompositeGeometryAndPalette(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := newSpawnTestAssetServer()

	def := content.NewAssetDef("collapsed-cache")
	def.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	def.Parts = []content.AssetPartDef{
		{
			ID:     "left",
			Name:   "left",
			Source: testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Position: content.Vec3{-0.5, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
		{
			ID:     "right",
			Name:   "right",
			Source: testProceduralPartSource(),
			Transform: content.AssetTransformDef{
				Position: content.Vec3{0.5, 0, 0},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
		},
	}

	first, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{CollapseVoxelParts: VoxelPartCollapseForce})
	if err != nil {
		t.Fatalf("first collapsed spawn failed: %v", err)
	}
	second, err := SpawnAuthoredAssetWithOptions(cmd, assets, def, TransformComponent{
		Position: mgl32.Vec3{3, 0, 0},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}, AuthoredAssetSpawnOptions{CollapseVoxelParts: VoxelPartCollapseForce})
	if err != nil {
		t.Fatalf("second collapsed spawn failed: %v", err)
	}
	app.FlushCommands()

	firstComp := mustVoxelModelForSpawnTest(t, cmd, onlyVoxelEntityForSpawnTest(t, cmd, first.RootEntity))
	secondComp := mustVoxelModelForSpawnTest(t, cmd, onlyVoxelEntityForSpawnTest(t, cmd, second.RootEntity))

	if firstComp.GeometryAsset() != secondComp.GeometryAsset() {
		t.Fatalf("expected collapsed spawns to reuse geometry asset, got %v and %v", firstComp.GeometryAsset(), secondComp.GeometryAsset())
	}
	if firstComp.VoxelPalette != secondComp.VoxelPalette {
		t.Fatalf("expected collapsed spawns to reuse palette asset, got %v and %v", firstComp.VoxelPalette, secondComp.VoxelPalette)
	}
}

func writeNamedSceneVoxFixture(t *testing.T, path string) {
	t.Helper()
	writeSyntheticVoxFixture(t, path, syntheticNamedSceneNodes())
}

func writeDuplicateNameSceneVoxFixture(t *testing.T, path string) {
	t.Helper()
	writeSyntheticVoxFixture(t, path, syntheticDuplicateNameSceneNodes())
}

func writeSyntheticVoxFixture(t *testing.T, path string, nodes []syntheticVoxNodeChunk) {
	t.Helper()
	models := []VoxModel{
		{
			SizeX: 2,
			SizeY: 2,
			SizeZ: 2,
			Voxels: []Voxel{{
				X: 0, Y: 0, Z: 0, ColorIndex: 1,
			}},
		},
		{
			SizeX: 4,
			SizeY: 2,
			SizeZ: 2,
			Voxels: []Voxel{{
				X: 0, Y: 0, Z: 0, ColorIndex: 2,
			}},
		},
	}

	var file bytes.Buffer
	file.WriteString(VOXMagicNumber)
	writeInt32ForVoxFixture(t, &file, 150)
	writeChunkForVoxFixture(t, &file, "MAIN", nil, 0)
	writeChunkForVoxFixture(t, &file, "PACK", synthPackChunkData(len(models)), 0)
	for _, model := range models {
		writeChunkForVoxFixture(t, &file, "SIZE", synthSizeChunkData(model), 0)
		writeChunkForVoxFixture(t, &file, "XYZI", synthXYZIChunkData(model), 0)
	}
	for _, node := range nodes {
		writeChunkForVoxFixture(t, &file, node.id, node.data, 0)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(path, file.Bytes(), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
}

type syntheticVoxNodeChunk struct {
	id   string
	data []byte
}

func syntheticNamedSceneNodes() []syntheticVoxNodeChunk {
	return []syntheticVoxNodeChunk{
		{id: "nTRN", data: synthTransformNodeData(0, map[string]string{"_name": "body"}, 1, 0, 0, 0)},
		{id: "nGRP", data: synthGroupNodeData(1, nil, []int{2, 4})},
		{id: "nSHP", data: synthShapeNodeData(2, nil, []int{0})},
		{id: "nTRN", data: synthTransformNodeData(4, map[string]string{"_name": "arm"}, 5, 0, 2, 0)},
		{id: "nSHP", data: synthShapeNodeData(5, nil, []int{1})},
	}
}

func syntheticDuplicateNameSceneNodes() []syntheticVoxNodeChunk {
	return []syntheticVoxNodeChunk{
		{id: "nGRP", data: synthGroupNodeData(0, nil, []int{1, 3})},
		{id: "nTRN", data: synthTransformNodeData(1, map[string]string{"_name": "arm"}, 2, 0, 0, 0)},
		{id: "nSHP", data: synthShapeNodeData(2, nil, []int{0})},
		{id: "nTRN", data: synthTransformNodeData(3, map[string]string{"_name": "arm"}, 4, 0, 0, 0)},
		{id: "nSHP", data: synthShapeNodeData(4, nil, []int{1})},
	}
}

func synthPackChunkData(numModels int) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, uint32(numModels))
	return buf.Bytes()
}

func synthSizeChunkData(model VoxModel) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, model.SizeX)
	writeUint32ForVoxFixture(&buf, model.SizeZ)
	writeUint32ForVoxFixture(&buf, model.SizeY)
	return buf.Bytes()
}

func synthXYZIChunkData(model VoxModel) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, uint32(len(model.Voxels)))
	for _, voxel := range model.Voxels {
		buf.WriteByte(byte(voxel.X))
		buf.WriteByte(byte(voxel.Z))
		buf.WriteByte(byte(voxel.Y))
		buf.WriteByte(voxel.ColorIndex)
	}
	return buf.Bytes()
}

func synthTransformNodeData(id int, attrs map[string]string, childID int, tx int32, ty int32, tz int32) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, uint32(id))
	buf.Write(synthDICTData(attrs))
	writeUint32ForVoxFixture(&buf, uint32(childID))
	writeUint32ForVoxFixture(&buf, 0)
	writeUint32ForVoxFixture(&buf, 0)
	writeUint32ForVoxFixture(&buf, 1)
	frameAttrs := map[string]string{"_r": "0"}
	if tx != 0 || ty != 0 || tz != 0 {
		frameAttrs["_t"] = strings.TrimSpace(
			strconv.FormatInt(int64(tx), 10) + " " +
				strconv.FormatInt(int64(tz), 10) + " " +
				strconv.FormatInt(int64(ty), 10),
		)
	}
	buf.Write(synthDICTData(frameAttrs))
	return buf.Bytes()
}

func synthGroupNodeData(id int, attrs map[string]string, childIDs []int) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, uint32(id))
	buf.Write(synthDICTData(attrs))
	writeUint32ForVoxFixture(&buf, uint32(len(childIDs)))
	for _, childID := range childIDs {
		writeUint32ForVoxFixture(&buf, uint32(childID))
	}
	return buf.Bytes()
}

func synthShapeNodeData(id int, attrs map[string]string, modelIDs []int) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, uint32(id))
	buf.Write(synthDICTData(attrs))
	writeUint32ForVoxFixture(&buf, uint32(len(modelIDs)))
	for _, modelID := range modelIDs {
		writeUint32ForVoxFixture(&buf, uint32(modelID))
		buf.Write(synthDICTData(nil))
	}
	return buf.Bytes()
}

func synthDICTData(values map[string]string) []byte {
	var buf bytes.Buffer
	writeUint32ForVoxFixture(&buf, uint32(len(values)))
	for key, value := range values {
		writeStringForVoxFixture(&buf, key)
		writeStringForVoxFixture(&buf, value)
	}
	return buf.Bytes()
}

func writeChunkForVoxFixture(t *testing.T, buf *bytes.Buffer, chunkID string, chunkData []byte, childrenSize int32) {
	t.Helper()
	if len(chunkID) != 4 {
		t.Fatalf("chunk id must be 4 bytes, got %q", chunkID)
	}
	buf.WriteString(chunkID)
	writeInt32ForVoxFixture(t, buf, int32(len(chunkData)))
	writeInt32ForVoxFixture(t, buf, childrenSize)
	if len(chunkData) > 0 {
		buf.Write(chunkData)
	}
}

func writeStringForVoxFixture(buf *bytes.Buffer, value string) {
	writeUint32ForVoxFixture(buf, uint32(len(value)))
	buf.WriteString(value)
}

func writeUint32ForVoxFixture(buf *bytes.Buffer, value uint32) {
	_ = binary.Write(buf, binary.LittleEndian, value)
}

func writeInt32ForVoxFixture(t *testing.T, buf *bytes.Buffer, value int32) {
	t.Helper()
	if err := binary.Write(buf, binary.LittleEndian, value); err != nil {
		t.Fatalf("binary.Write failed: %v", err)
	}
}
