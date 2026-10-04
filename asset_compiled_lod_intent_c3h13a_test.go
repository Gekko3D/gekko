package gekko

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func c3h13aFixture(t *testing.T, kind string) string {
	t.Helper()
	input, output, asset := c3h4Fixture(t)
	output = strings.TrimSuffix(output, ".gkasset") + ".gkassetc"
	// The compiler fixture's dummy sprite bytes are for closure tests; actual
	// spawn coverage does not need an unrelated particle emitter.
	asset.Emitters = nil
	if kind == "mixed" {
		asset.Parts[0].Source.VoxelShape.Palette[0].MaterialID = "unused"
	}
	if kind == "group-first" {
		asset.Parts[0], asset.Parts[1] = asset.Parts[1], asset.Parts[0]
	}
	c3cWrite(t, input, asset)
	if _, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: kind != "noLOD"}); err != nil {
		t.Fatal(err)
	}
	return output
}

func c3h13aRoot() TransformComponent {
	return TransformComponent{Position: mgl32.Vec3{3, 4, 5}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
}

func c3h13aNoIntent(t *testing.T, cmd *Commands, entity EntityId) {
	t.Helper()
	if hasComponentOfType[compiledAssetLODComponent](cmd, entity) {
		t.Fatal("undeclared/group/nonvoxel entity gained compiled LOD intent", entity)
	}
}

func c3h13aIntent(t *testing.T, cmd *Commands, assets *AssetServer, entity EntityId, part preparedAuthoredPart) {
	t.Helper()
	intent := s3cComponent[compiledAssetLODComponent](t, cmd, entity)
	model := s3cComponent[VoxelModelComponent](t, cmd, entity)
	if intent.fullID != part.model || intent.coarseID != part.compiledLOD || intent.fullID == (AssetId{}) || intent.coarseID == (AssetId{}) || intent.fullID == intent.coarseID {
		t.Fatal("instance intent lost explicitly selected original identities")
	}
	if model.SharedGeometry != part.model || model.GeometryAsset() != part.model || model.OverrideGeometry != (AssetId{}) || model.VoxelPalette != part.palette {
		t.Fatal("intent replaced full CPU geometry or palette")
	}
	geometry, ok := assets.GetVoxelGeometry(model.GeometryAsset())
	if !ok || geometry.XBrickMap.GetVoxelCount() != 4 {
		t.Fatal("intent substituted coarse CPU geometry")
	}
}

func TestC3h13aPreparedAndDirectSpawnPropagateOnlyExplicitPartIntent(t *testing.T) {
	for _, route := range []string{"direct", "prepared-packet", "ownership"} {
		t.Run(route, func(t *testing.T) {
			path := c3h13aFixture(t, "mixed")
			assets := c3d3Server()
			app := NewApp()
			cmd := app.Commands()
			var prepared *PreparedAuthoredAsset
			if route == "prepared-packet" {
				prepared = c3h12bPublish(t, c3h12aPacket(t, path, nil), assets)
			} else {
				var err error
				prepared, err = LoadAndPrepareAuthoredAsset(path, assets, nil)
				if err != nil {
					t.Fatal(err)
				}
			}
			var result AuthoredAssetSpawnResult
			var err error
			callbacks := map[EntityId]int{}
			voxelCallbacks := map[string]bool{}
			if route == "ownership" {
				result, err = spawnAuthoredAssetWithOwnership(cmd, assets, prepared.def, prepared, c3h13aRoot(), AuthoredAssetSpawnOptions{DocumentPath: path}, func(id EntityId, item string, root, voxel bool) {
					callbacks[id]++
					if root && item != "" {
						t.Error("root ownership identity changed")
					}
					voxelCallbacks[item] = voxel
				})
			} else if route == "direct" {
				result, err = LoadAndSpawnAuthoredAsset(path, cmd, assets, c3h13aRoot())
			} else {
				result, err = SpawnPreparedAuthoredAsset(cmd, assets, prepared, c3h13aRoot())
			}
			if err != nil {
				t.Fatal(err)
			}
			app.FlushCommands()
			c3h13aNoIntent(t, cmd, result.RootEntity)
			for _, part := range prepared.def.Parts {
				id := result.EntitiesByAssetID[part.ID]
				owned := prepared.parts[part.ID]
				if owned.compiledLOD == (AssetId{}) {
					c3h13aNoIntent(t, cmd, id)
				} else {
					c3h13aIntent(t, cmd, assets, id, owned)
				}
				wantParent := result.RootEntity
				if part.ParentID != "" {
					wantParent = result.EntitiesByAssetID[part.ParentID]
				}
				if s3cComponent[Parent](t, cmd, id).Entity != wantParent {
					t.Fatal("explicit intent changed hierarchy")
				}
				if owned.model != (AssetId{}) {
					model := s3cComponent[VoxelModelComponent](t, cmd, id)
					if model.VoxelResolution != part.VoxelResolution || model.PivotMode != PivotModeCustom || model.CustomPivot != mgl32.Vec3(part.Transform.Pivot) {
						t.Fatal("intent changed pivot/resolution")
					}
				}
			}
			for _, marker := range prepared.def.Markers {
				c3h13aNoIntent(t, cmd, result.EntitiesByAssetID[marker.ID])
			}
			if route == "ownership" {
				for _, id := range result.Entities {
					if callbacks[id] != 1 {
						t.Fatal("ownership callback missed/duplicated created entity")
					}
				}
				if !voxelCallbacks["shape"] || !voxelCallbacks["duplicate"] || voxelCallbacks["root"] {
					t.Fatal("intent changed voxel ownership reporting")
				}
			}
			// Shared AS availability from the eligible duplicate grants no opt-in to
			// a later nonLOD input with exactly the same fine geometry.
			legacyPath := c3h13aFixture(t, "noLOD")
			later, err := LoadAndSpawnAuthoredAsset(legacyPath, cmd, assets, c3h13aRoot())
			if err != nil {
				t.Fatal(err)
			}
			app.FlushCommands()
			for _, id := range later.Entities {
				c3h13aNoIntent(t, cmd, id)
			}
			earlierModel := s3cComponent[VoxelModelComponent](t, cmd, result.EntitiesByAssetID["shape"])
			laterModel := s3cComponent[VoxelModelComponent](t, cmd, later.EntitiesByAssetID["shape"])
			if earlierModel.SharedGeometry != laterModel.SharedGeometry {
				t.Fatal("legacy exclusion fixture did not share warm fine geometry")
			}
		})
	}
}

func TestC3h13aFirstPartHelperKeepsTupleParityAndSelectedMembership(t *testing.T) {
	for _, kind := range []string{"eligible", "mixed", "group-first", "noLOD"} {
		t.Run(kind, func(t *testing.T) {
			path := c3h13aFixture(t, kind)
			assets := c3d3Server()
			// Availability installed by an independently opt-in input cannot override
			// membership of this selected first part.
			warm := c3h13aFixture(t, "eligible")
			if _, err := LoadAndPrepareAuthoredAsset(warm, assets, nil); err != nil {
				t.Fatal(err)
			}
			part, resolution, err := loadAuthoredLevelVoxelPart(assets, nil, path)
			if err != nil {
				t.Fatal(err)
			}
			model, palette, oldResolution, err := loadAuthoredLevelVoxelModel(assets, nil, path)
			if err != nil || part.model != model || part.palette != palette || resolution != oldResolution {
				t.Fatal("extended helper changed stable first-part tuple", err)
			}
			if kind == "eligible" {
				binding := c3h12bBinding(t, assets, part.model)
				if part.compiledLOD != binding.coarseID {
					t.Fatal("selected declared LOD membership lost")
				}
			} else if part.compiledLOD != (AssetId{}) {
				t.Fatal("extended helper inferred LOD from AS availability")
			}
			if kind == "group-first" && part.model != (AssetId{}) {
				t.Fatal("group-first selected later voxel model")
			}
			nilPart, _, err := loadAuthoredLevelVoxelPart(nil, nil, path)
			if err != nil || nilPart != (preparedAuthoredPart{}) {
				t.Fatal("nil server extended helper changed verify-only return", err)
			}
		})
	}
	// Ordinary authored inputs retain their model/palette and carry zero intent.
	input, _, _ := c3h4Fixture(t)
	assets := c3d3Server()
	part, res, err := loadAuthoredLevelVoxelPart(assets, nil, input)
	if err != nil {
		t.Fatal(err)
	}
	model, palette, oldRes, err := loadAuthoredLevelVoxelModel(assets, nil, input)
	if err != nil || part.model != model || part.palette != palette || res != oldRes || part.compiledLOD != (AssetId{}) {
		t.Fatal("ordinary authored helper inferred compiled intent", err)
	}
	missing := filepath.Join(t.TempDir(), "missing.gkassetc")
	part, _, err = loadAuthoredLevelVoxelPart(assets, nil, missing)
	var inputFailure *authoredAssetInputLoadError
	if !errors.As(err, &inputFailure) || part != (preparedAuthoredPart{}) {
		t.Fatal("selected missing-input classification changed", err)
	}
}

func TestC3h13aFourFirstPartConsumersAttachOnlySelectedIntent(t *testing.T) {
	for _, kind := range []string{"eligible", "mixed", "group-first", "noLOD"} {
		t.Run(kind, func(t *testing.T) {
			path := c3h13aFixture(t, kind)
			assets := c3d3Server()
			app := NewApp()
			cmd := app.Commands()
			warm := c3h13aFixture(t, "eligible")
			if _, err := LoadAndPrepareAuthoredAsset(warm, assets, nil); err != nil {
				t.Fatal(err)
			}
			selected, resolution, err := loadAuthoredLevelVoxelPart(assets, nil, path)
			if err != nil {
				t.Fatal(err)
			}
			parent := cmd.AddEntity(&TransformComponent{})
			for _, consumer := range c3d4cKinds {
				entity, err := c3d4cSpawn(consumer, cmd, assets, nil, parent, path)
				if err != nil {
					t.Fatal(consumer, err)
				}
				app.FlushCommands()
				if s3cComponent[Parent](t, cmd, entity).Entity != parent {
					t.Fatal("consumer parent changed")
				}
				if kind == "eligible" {
					c3h13aIntent(t, cmd, assets, entity, selected)
				} else {
					c3h13aNoIntent(t, cmd, entity)
				}
				if kind == "group-first" {
					if hasComponentOfType[VoxelModelComponent](cmd, entity) {
						t.Fatal("group-first consumer selected unused geometry")
					}
					continue
				}
				model := s3cComponent[VoxelModelComponent](t, cmd, entity)
				if model.SharedGeometry != selected.model || model.VoxelPalette != selected.palette || model.PivotMode != PivotModeCorner || model.VoxelResolution != resolution {
					t.Fatal("consumer intent changed fine model/palette/pivot/resolution", consumer)
				}
				geometry, ok := assets.GetVoxelGeometry(model.GeometryAsset())
				if !ok || geometry.XBrickMap.GetVoxelCount() != 4 {
					t.Fatal("consumer intent substituted coarse CPU authority")
				}
			}
		})
	}
}

func TestC3h13aPickupToleranceStillDistinguishesMissingSelectedInputAndDerivative(t *testing.T) {
	for _, kind := range []string{"missing-input", "missing-derivative"} {
		t.Run(kind, func(t *testing.T) {
			path := c3h13aFixture(t, "eligible")
			if kind == "missing-input" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else {
				header, _, err := content.LoadCompiledAssetHeader(path, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(c3h7LODPath(path, header)); err != nil {
					t.Fatal(err)
				}
			}
			app := NewApp()
			assets := c3d3Server()
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			entity, err := c3d4cSpawn("pickup", app.Commands(), assets, owner, 0, path)
			if kind == "missing-input" {
				if err != nil || entity == 0 {
					t.Fatal("missing pickup visual ceased to be tolerated", err)
				}
				app.FlushCommands()
				c3h13aNoIntent(t, app.Commands(), entity)
				if hasComponentOfType[VoxelModelComponent](app.Commands(), entity) {
					t.Fatal("missing visual created geometry")
				}
			} else if err == nil || entity != 0 {
				t.Fatal("present compiled header with absent derivative became tolerated")
			}
			if len(assets.voxModels) != 0 || len(assets.compiledAssetLODs) != 0 {
				t.Fatal("failed/missing visual published assets or association")
			}
			if s := owner.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
				t.Fatal("consumer failure retained decode scope", s)
			}
		})
	}
}

func TestC3h13aIntentDoesNotRetargetAfterSnapshotOrDeltaOverrideAndEntityRemoval(t *testing.T) {
	path := c3h13aFixture(t, "eligible")
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
	if err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	cmd := app.Commands()
	cmd.AddResources(assets)
	app.FlushCommands()
	result, err := SpawnPreparedAuthoredAsset(cmd, assets, prepared, c3h13aRoot())
	if err != nil {
		t.Fatal(err)
	}
	app.FlushCommands()
	entity := result.EntitiesByAssetID["shape"]
	intent := *s3cComponent[compiledAssetLODComponent](t, cmd, entity)
	binding := c3h12bBinding(t, assets, intent.fullID)
	stats := assets.compiledAssetLODStorageStats()
	base := VoxelObjectSnapshotFromXBrickMap(binding.proof.full)
	for _, mode := range []string{content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta} {
		payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: mode, PlacementID: "placement", ItemID: "shape", Lattice: binding.proof.lattice, Voxels: []content.VoxelObjectVoxelDef{{X: 7, Value: 9}}}
		if mode == content.VoxelObjectPayloadBaseDelta {
			payload.BaseIdentity = assets.authoredVoxelBaseIdentity(intent.fullID, binding.proof.lattice)
			payload.Voxels = append(payload.Voxels, content.VoxelObjectVoxelDef{X: -2, Value: 0})
		}
		snapshot, err := content.ResolveVoxelObjectPayload(payload, base, binding.proof.lattice, "placement", "shape", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := applyVoxelObjectSnapshotToEntity(cmd, entity, snapshot); err != nil {
			t.Fatal(err)
		}
		app.FlushCommands()
		current := s3cComponent[VoxelModelComponent](t, cmd, entity)
		if current.OverrideGeometry == (AssetId{}) || current.GeometryAsset() == intent.fullID || current.SharedGeometry != intent.fullID || *s3cComponent[compiledAssetLODComponent](t, cmd, entity) != intent {
			t.Fatal("override retargeted original intent/CPU base identity", mode)
		}
		if assets.compiledAssetLODStorageStats() != stats {
			t.Fatal("instance override created extra AS proof owner")
		}
	}
	cmd.RemoveEntity(entity)
	app.FlushCommands()
	if cmd.EntityExists(entity) {
		t.Fatal("ordinary ECS deletion retained intent entity")
	}
	if _, ok := assets.GetVoxelGeometry(intent.fullID); !ok {
		t.Fatal("intent entity deletion revoked ordinary fine asset")
	}
	if _, ok := assets.GetVoxelGeometry(intent.coarseID); !ok {
		t.Fatal("intent entity deletion revoked ordinary coarse asset")
	}
	if assets.compiledAssetLODStorageStats() != stats {
		t.Fatal("entity deletion implicitly owned AS proof")
	}
}

func TestC3h13aPreparedInvalidIDsAndCollapsedSpawnCarryNoIntent(t *testing.T) {
	for _, kind := range []string{"zero-coarse", "zero-full", "same-id"} {
		t.Run(kind, func(t *testing.T) {
			path := c3h13aFixture(t, "eligible")
			assets := c3d3Server()
			prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
			if err != nil {
				t.Fatal(err)
			}
			for partID, part := range prepared.parts {
				switch kind {
				case "zero-coarse":
					part.compiledLOD = AssetId{}
				case "zero-full":
					part.model = AssetId{}
				case "same-id":
					part.compiledLOD = part.model
				}
				prepared.parts[partID] = part
			}
			app := NewApp()
			result, err := SpawnPreparedAuthoredAsset(app.Commands(), assets, prepared, c3h13aRoot())
			if err != nil {
				t.Fatal(err)
			}
			app.FlushCommands()
			for _, entity := range result.Entities {
				c3h13aNoIntent(t, app.Commands(), entity)
			}
		})
	}
	// Existing collapsed spawning owns one composite and must not inherit
	// individual prepared part intent, even if supplied in private metadata.
	def := p5eShapeDef()
	assets := c3d3Server()
	prepared, err := PrepareAuthoredAsset(assets, def, "")
	if err != nil {
		t.Fatal(err)
	}
	for partID, part := range prepared.parts {
		part.compiledLOD = makeAssetId()
		prepared.parts[partID] = part
	}
	app := NewApp()
	result, err := SpawnPreparedAuthoredAsset(app.Commands(), assets, prepared, c3h13aRoot())
	if err != nil || !result.Collapsed {
		t.Fatal("existing collapsed fixture changed", err)
	}
	app.FlushCommands()
	for _, entity := range result.Entities {
		c3h13aNoIntent(t, app.Commands(), entity)
	}
}

func TestC3h13aStreamedPreparedSpawnCarriesIntentThroughOwnershipCommit(t *testing.T) {
	path := c3h13aFixture(t, "mixed")
	f, _ := c3f3Runtime(t, 1, 1)
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = path

	f.runtime.Level.Placements[0].AssetPath = path
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, context PostSpawnPlacementContext) {
		f.hooks[context.Placement.PlacementID]++
		eligible := context.SpawnResult.EntitiesByAssetID["duplicate"]
		if !cmd.EntityExists(context.RootEntity) || !cmd.EntityExists(eligible) {
			t.Fatal("intent placement hook missed atomic flushed entities")
		}
		if !hasComponentOfType[compiledAssetLODComponent](cmd, eligible) {
			t.Fatal("intent unavailable at ownership hook boundary")
		}
	}}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	if prepared.Err != nil {
		prepared.release()
		t.Fatal(prepared.Err)
	}
	defer prepared.release()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	placement := s1gID(0, 0)
	eligible := placementItemEntityByIDForStreamedTest(f.cmd, placement, "duplicate")
	undeclared := placementItemEntityByIDForStreamedTest(f.cmd, placement, "shape")
	if eligible == 0 || undeclared == 0 || f.hooks[placement] != 1 {
		t.Fatal("streamed atomic ownership/placement commit changed")
	}
	intent := s3cComponent[compiledAssetLODComponent](t, f.cmd, eligible)
	model := s3cComponent[VoxelModelComponent](t, f.cmd, eligible)
	if model.SharedGeometry != intent.fullID || model.GeometryAsset() != intent.fullID {
		t.Fatal("streamed intent replaced CPU fine authority")
	}
	c3h13aNoIntent(t, f.cmd, undeclared)
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if f.cmd.EntityExists(eligible) || f.cmd.EntityExists(undeclared) {
		t.Fatal("stream stop retained owned intent entities")
	}
	if _, ok := f.assets.GetVoxelGeometry(intent.fullID); !ok {
		t.Fatal("stream intent acquired extra asset lifetime ownership")
	}
}
