package gekko

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func c3g9Fixture(t *testing.T, kind string) (string, *content.CompiledAssetModelHeaderDef) {
	t.Helper()
	path, h := c3g8Fixture(t, [3]uint32{2, 0, 4})
	h.Asset.Emitters = nil
	if kind != "mixed" {
		parts := h.Asset.Parts[:0]
		for _, p := range h.Asset.Parts {
			if p.ID == "model" || p.ID == "other-model" {
				parts = append(parts, p)
			}
		}
		h.Asset.Parts = parts
		h.Shapes = nil
		h.LODs = nil
		h.Asset.Lights = nil
		h.Asset.Markers = nil
		h.Asset.Skeleton = nil
		h.Asset.AnimationSetPaths = nil
		h.Asset.DefaultAnimationClipID = ""
	}
	if kind == "group-first" {
		h.Asset.Parts = append([]content.AssetPartDef{{ID: "first-group", Name: "First group", Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}, Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}}}, h.Asset.Parts...)
	}
	c3g7Save(t, path, h)
	return path, h
}

func c3g9ModelEntity(t *testing.T, cmd *Commands, assets *AssetServer, entity EntityId) AssetId {
	t.Helper()
	if entity == 0 {
		t.Fatal("model entity missing")
	}
	vm := s3cComponent[VoxelModelComponent](t, cmd, entity)
	if vm.SharedGeometry == (AssetId{}) || vm.PivotMode != PivotModeCenter || vm.VoxelResolution != .125 || hasComponentOfType[compiledAssetLODComponent](cmd, entity) {
		t.Fatal("model pivot/lattice or inferred intent changed")
	}
	geometry, ok := assets.GetVoxelGeometry(vm.SharedGeometry)
	if !ok || geometry.XBrickMap.GetVoxelCount() != 1 || geometry.LocalMin != (mgl32.Vec3{}) || geometry.LocalMax != (mgl32.Vec3{2, 0, 4}) || geometry.VoxModel.SizeX != 2 || len(geometry.VoxModel.Voxels) != 0 {
		t.Fatal("consumer lost canonical model geometry/declared bounds")
	}
	return vm.SharedGeometry
}

func TestC3g9PublicDirectPrepareSpawnAndFirstPart(t *testing.T) {
	for _, kind := range []string{"models", "mixed", "group-first"} {
		t.Run(kind, func(t *testing.T) {
			path, h := c3g9Fixture(t, kind)
			assets := c3d3Server()
			owner := NewRuntimeContentLoader()
			prepared, err := LoadAndPrepareAuthoredAsset(path, assets, owner)
			if err != nil {
				t.Fatal("new compiled public prepare", err)
			}
			if prepared.parts["model"].model == (AssetId{}) || prepared.parts["model"].model != prepared.parts["other-model"].model || prepared.parts["model"].palette == prepared.parts["other-model"].palette || prepared.parts["model"].compiledLOD != (AssetId{}) {
				t.Fatal("model prepared geometry/palette membership changed")
			}
			app := NewApp()
			app.Commands().AddResources(assets)
			result, err := SpawnPreparedAuthoredAsset(app.Commands(), assets, prepared, c3h13aRoot())
			if err != nil {
				t.Fatal(err)
			}
			app.FlushCommands()
			id := c3g9ModelEntity(t, app.Commands(), assets, result.EntitiesByAssetID["model"])
			direct, err := LoadAndSpawnAuthoredAsset(path, app.Commands(), assets, c3h13aRoot())
			if err != nil {
				t.Fatal("new compiled direct spawn", err)
			}
			app.FlushCommands()
			if c3g9ModelEntity(t, app.Commands(), assets, direct.EntitiesByAssetID["model"]) != id {
				t.Fatal("direct spawn bypassed warm model owner")
			}
			if kind == "mixed" {
				inline := result.EntitiesByAssetID[h.Shapes[0].PartID]
				vm := s3cComponent[VoxelModelComponent](t, app.Commands(), inline)
				if vm.PivotMode != PivotModeCustom || !hasComponentOfType[compiledAssetLODComponent](app.Commands(), inline) {
					t.Fatal("mixed inline custom pivot/explicit LOD changed")
				}
			}
			selected, res, err := loadAuthoredLevelVoxelPart(assets, owner, path)
			if err != nil {
				t.Fatal("new compiled first part", err)
			}
			model, palette, tupleRes, err := loadAuthoredLevelVoxelModel(assets, owner, path)
			if err != nil || selected.model != model || selected.palette != palette || res != tupleRes {
				t.Fatal("stable first part tuple changed", err)
			}
			if kind == "models" && (selected.model != id || res != .125 || selected.compiledLOD != (AssetId{})) {
				t.Fatal("selected model first part differs")
			}
			if kind == "group-first" && (selected.model != (AssetId{}) || selected.palette != (AssetId{}) || res != content.DefaultAssetVoxelSize) {
				t.Fatal("group first widened selection")
			}
			nilPrepared, err := LoadAndPrepareAuthoredAsset(path, nil, owner)
			if err != nil || nilPrepared == nil {
				t.Fatal("nil AS metadata verification", err)
			}
			for _, p := range nilPrepared.parts {
				if p.model != (AssetId{}) || p.palette != (AssetId{}) || p.compiledLOD != (AssetId{}) {
					t.Fatal("nil AS retained global resources")
				}
			}
			nilPart, _, err := loadAuthoredLevelVoxelPart(nil, owner, path)
			if err != nil || nilPart != (preparedAuthoredPart{}) {
				t.Fatal("nil AS first part widened publication", err)
			}
		})
	}
}

func TestC3g9WholeClosureFirstPartErrorsAndLevelRoutes(t *testing.T) {
	path, h := c3g9Fixture(t, "models")
	assets := c3d3Server()
	selectedAssets := c3d3Server()
	selected, _, err := loadAuthoredLevelVoxelPart(selectedAssets, nil, path)
	if err != nil || selected.model == (AssetId{}) || len(selectedAssets.voxPalettes) != 1 || len(selectedAssets.voxModels) != 1 {
		t.Fatal("first model publication widened to later palette", err)
	}
	level := content.NewLevelDef("Model routes")
	transform := content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	level.Placements = []content.LevelPlacementDef{{ID: "placement", AssetPath: filepath.Base(path), Transform: transform}}
	level.NPCs = []content.LevelNPCDef{{ID: "npc", Name: "NPC", ClassName: "npc_model", AssetPath: filepath.Base(path), Transform: transform}}
	levelPath := filepath.Join(filepath.Dir(path), "level.gklevel")
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	app.Commands().AddResources(assets)
	spawned, err := LoadAndSpawnAuthoredLevel(levelPath, app.Commands(), assets, NewRuntimeContentLoader(), AuthoredLevelSpawnOptions{})
	if err != nil {
		t.Fatal("placement/NPC new extension route", err)
	}
	app.FlushCommands()
	entity := placementItemEntityByIDForStreamedTest(app.Commands(), "placement", "model")
	shared := c3g9ModelEntity(t, app.Commands(), assets, entity)
	if spawned.NPCEntities["npc"] == 0 {
		t.Fatal("NPC root missing")
	}
	// Both NPC and placement have used the same ordinary model geometry namespace.
	if assets.voxModelKeys["compiled-asset-model:"+h.Models[0].ContentID] != shared || len(assets.voxModels) != 1 {
		t.Fatal("level paths bypassed common model adoption")
	}
	h.Models[1].Path = "missing-unselected.gkvox"
	c3g7Save(t, path, h)
	freshAssets := c3d3Server()
	var inputFailure *authoredAssetInputLoadError
	if prepared, err := LoadAndPrepareAuthoredAsset(path, freshAssets, NewRuntimeContentLoader()); err == nil || prepared != nil || errors.As(err, &inputFailure) {
		t.Fatal("present header unselected dependency was tolerated as missing input")
	}
	if len(freshAssets.voxModels) != 0 || len(freshAssets.voxPalettes) != 0 {
		t.Fatal("failed whole closure published globals")
	}
	if part, _, err := loadAuthoredLevelVoxelPart(freshAssets, NewRuntimeContentLoader(), path); err == nil || part != (preparedAuthoredPart{}) || errors.As(err, &inputFailure) {
		t.Fatal("first part ignored unselected bad model")
	}
	missing := filepath.Join(t.TempDir(), "missing.gkmodelassetc")
	if part, _, err := loadAuthoredLevelVoxelPart(freshAssets, nil, missing); !errors.As(err, &inputFailure) || part != (preparedAuthoredPart{}) {
		t.Fatal("missing selected header classification changed")
	}
}

func TestC3g9StreamedWorkerPacketOnlyCommitAndStop(t *testing.T) {
	path, h := c3g9Fixture(t, "models")
	f, _ := c3f3Runtime(t, 2, 2)
	for i := range f.runtime.PlacementsByChunk[ChunkCoord{}] {
		f.runtime.PlacementsByChunk[ChunkCoord{}][i].AssetPath = path
	}
	f.runtime.Level.Placements[0].AssetPath = path
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, context PostSpawnPlacementContext) {
		f.hooks[context.Placement.PlacementID]++
		model := context.SpawnResult.EntitiesByAssetID["model"]
		if !cmd.EntityExists(context.RootEntity) || !cmd.EntityExists(model) || hasComponentOfType[compiledAssetLODComponent](cmd, model) {
			t.Fatal("atomic model ownership hook changed")
		}
	}}
	before := len(f.assets.voxModels)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	if prepared.Err != nil {
		prepared.release()
		t.Fatal("new extension worker", prepared.Err)
	}
	defer prepared.release()
	packet := c3f3Packet(t, prepared, path)
	shape := packet.shapes[h.Models[0].ContentID]
	if shape == nil || !shape.model || shape.registration.charge() <= 0 || len(f.assets.voxModels) != before {
		t.Fatal("worker failed private model preparation or published globals")
	}
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	f.runtime.Loader.Clear()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	first := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "model")
	second := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 1), "model")
	id := c3g9ModelEntity(t, f.cmd, f.assets, first)
	if c3g9ModelEntity(t, f.cmd, f.assets, second) != id || f.hooks[s1gID(0, 0)] != 1 || f.hooks[s1gID(0, 1)] != 1 {
		t.Fatal("streamed repeated model ownership changed")
	}
	if shape.registration.charge() != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("committed model packet retained pending credit")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	prepared.release()
	packet.release()
	if _, ok := f.assets.GetVoxelGeometry(id); !ok {
		t.Fatal("Stop/late release deleted adopted ordinary model")
	}
}
