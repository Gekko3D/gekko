package gekko

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSpawnAuthoredAssetResolvesChildBeforeParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := content.NewAssetDef("ordered")
	parent := content.AssetPartDef{
		ID:   "parent",
		Name: "parent",
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
		{ID: "a", Name: "a", ParentID: "b"},
		{ID: "b", Name: "b", ParentID: "a"},
	}
	if err := ValidateAssetHierarchy(def); err == nil {
		t.Fatal("expected cycle to be rejected")
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

func representativeAuthoredAssetForTest() *content.AssetDef {
	def := content.NewAssetDef("parity")
	def.Parts = []content.AssetPartDef{
		{
			ID:   "root-part",
			Name: "root-part",
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
