package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p5bRuntime(t *testing.T, coords []ChunkCoord, hooks ...PostSpawnTerrainHook) (*streamedRenderHarness, EntityId, map[ChunkCoord]*content.TerrainChunkDef) {
	t.Helper()
	root := t.TempDir()
	levelPath, manifestPath := filepath.Join(root, "terrain.gklevel"), filepath.Join(root, "terrain.gkterrainmanifest")
	writeTerrainSourceForStreamedTest(t, filepath.Join(root, "terrain.gkterrain"))
	chunks := make(map[ChunkCoord]*content.TerrainChunkDef)
	var entries []content.TerrainChunkEntryDef
	for index, coord := range coords {
		chunk := &content.TerrainChunkDef{
			TerrainID: "terrain-a", SourceHash: "p5b-columns", Coord: content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z},
			ChunkSize: 16, VoxelResolution: 1, SolidValue: 3,
			Columns: []content.TerrainChunkColumnDef{{X: 2, Z: 3, FilledVoxels: 4}, {X: 9, Z: 11, FilledVoxels: 2}}, NonEmptyVoxelCount: 6,
		}
		path := filepath.Join(root, fmt.Sprintf("chunk_%d.gkchunk", index))
		writeTerrainChunkForStreamedTest(t, path, chunk)
		entry := terrainEntryForStreamedTest(path, manifestPath, chunk.Coord)
		entry.SourceHash, entry.NonEmptyVoxelCount = chunk.SourceHash, chunk.NonEmptyVoxelCount
		entries = append(entries, entry)
		chunks[coord] = chunk
	}
	writeTerrainManifestForStreamedTest(t, manifestPath, "terrain-a", entries)
	level := content.NewLevelDef("p5b-terrain")
	level.ChunkSize, level.VoxelResolution = 16, 1
	level.Terrain = &content.LevelTerrainDef{Kind: content.TerrainKindHeightfield, SourcePath: "terrain.gkterrain", ManifestPath: "terrain.gkterrainmanifest"}
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateLevel(level, content.LevelValidationOptions{DocumentPath: levelPath}); validation.HasErrors() {
		t.Fatalf("invalid terrain fixture: %s", validation.Error())
	}
	app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
		LevelPath: levelPath, StreamingRadius: 2, StreamingPrefetchRadius: 2, StreamingKeepRadius: 2,
		MaxPrepareJobs: 2, MaxChunkCommitsPerFrame: 1, TerrainHooks: hooks,
	})
	observer := s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedObserverSelection(cmd, state)
	return &streamedRenderHarness{t: t, app: app, cmd: cmd, assets: assets, runtime: state}, observer, chunks
}

func p5bTerrainAsset(t *testing.T, f *streamedRenderHarness, coord ChunkCoord) (EntityId, AssetId, VoxelGeometryAsset) {
	t.Helper()
	entity := terrainChunkEntityByCoordForStreamedTest(f.cmd, [3]int{coord.X, coord.Y, coord.Z})
	if entity == 0 {
		t.Fatal("real prepared terrain did not flush an entity")
	}
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	id := model.GeometryAsset()
	asset, ok := f.assets.GetVoxelGeometry(id)
	if !ok || asset.XBrickMap == nil {
		t.Fatal("flushed terrain has no usable registered geometry")
	}
	return entity, id, asset
}

func p5bTerrainGeometry(t *testing.T, geometry *volume.XBrickMap, removed bool) {
	t.Helper()
	wantCount := 6
	if removed {
		wantCount--
	}
	if geometry == nil || geometry.GetVoxelCount() != wantCount {
		t.Fatalf("terrain geometry voxel count differs from authored columns/removal; want %d", wantCount)
	}
	for _, column := range []content.TerrainChunkColumnDef{{X: 2, Z: 3, FilledVoxels: 4}, {X: 9, Z: 11, FilledVoxels: 2}} {
		for y := 0; y < column.FilledVoxels; y++ {
			occupied, value := geometry.GetVoxel(column.X, y, column.Z)
			if removed && column.X == 2 && y == 1 {
				if occupied {
					t.Fatal("current backing removal was restored by prepared terrain")
				}
			} else if !occupied || value != 3 {
				t.Fatalf("terrain voxel %v lost authored material: occupied=%t value=%d", [3]int{column.X, y, column.Z}, occupied, value)
			}
		}
	}
}

func TestP5bTerrainWorkerAdoptionPreservesRuntimeGeometryAndUnloadsOwnedAsset(t *testing.T) {
	coord := ChunkCoord{X: 1, Z: -1}
	f, _, chunks := p5bRuntime(t, []ChunkCoord{coord})
	unrelatedMap := volume.NewXBrickMap()
	unrelatedMap.SetVoxel(0, 0, 0, 7)
	unrelated := f.assets.RegisterSharedVoxelGeometry(unrelatedMap, "unrelated-terrain")
	s1fPrepared(t, f, coord, false)
	f.commitStage()
	entity, id, asset := p5bTerrainAsset(t, f, coord)
	p5bTerrainGeometry(t, asset.XBrickMap, false)
	if asset.LocalMin != (mgl32.Vec3{2, 0, 3}) || asset.LocalMax != (mgl32.Vec3{10, 4, 12}) {
		t.Fatalf("terrain asset bounds are not tight: min=%v max=%v", asset.LocalMin, asset.LocalMax)
	}
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	if !model.IsTerrainChunk || model.OverrideGeometry != id || model.ShareTerrainGeometry || model.TerrainGroupID == 0 ||
		model.TerrainChunkCoord != ([3]int{1, 0, -1}) || model.TerrainChunkSize != 16 ||
		model.VoxelAdjacencyGroupID != model.TerrainGroupID || model.VoxelAdjacencyChunkCoord != model.TerrainChunkCoord || model.VoxelAdjacencyChunkSize != 16 {
		t.Fatal("terrain adoption changed mutable geometry or adjacency metadata")
	}
	ref, ok := AuthoredTerrainChunkRefForEntity(f.cmd, entity)
	if !ok || ref.LevelID != f.runtime.LevelID || ref.TerrainID != "terrain-a" || ref.ChunkCoord != model.TerrainChunkCoord {
		t.Fatal("terrain adoption changed authored provenance")
	}
	transform := s3cComponent[TransformComponent](t, f.cmd, entity)
	if transform.Position != (mgl32.Vec3{16, 0, -16}) || transform.Scale != (mgl32.Vec3{1 / VoxelSize, 1 / VoxelSize, 1 / VoxelSize}) {
		t.Fatalf("terrain adoption changed world transform: %+v", transform)
	}
	backing := s3cComponent[VoxelBackingComponent](t, f.cmd, entity)
	chunk := chunks[coord]
	if backing.OwnerKind != content.VoxelBackingOwnerTerrain || backing.OwnerID != chunk.TerrainID || backing.SourceHash != chunk.SourceHash ||
		backing.ChunkCoord != model.TerrainChunkCoord || backing.ChunkSize != 16 || backing.Provider == nil ||
		backing.Provider.VoxelValue([3]int{18, 1, -13}) != 3 || backing.Provider.VoxelValue([3]int{19, 1, -13}) != 0 {
		t.Fatal("terrain adoption lost immutable column backing")
	}
	if f.runtime.LoadedChunks[coord] == nil || f.runtime.Metrics.CommittedChunkCount != 1 || p5aAdoptions(t, f.runtime) != 1 {
		t.Error("real terrain worker commit did not publish one chunk and adopt one prepared asset")
	}
	// Normal renderer extraction still gives editable terrain its own map.
	renderer := newVoxelRtStateTest()
	f.cmd.AddResources(renderer)
	f.app.FlushCommands()
	voxelRtSystem(nil, renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
	object := renderer.GetVoxelObject(entity)
	if object == nil || object.XBrickMap == nil {
		t.Fatal("terrain core bridge did not create usable renderer geometry")
	}
	object.XBrickMap.SetVoxel(9, 0, 11, 7)
	p5bTerrainGeometry(t, asset.XBrickMap, false)
	if err := unloadStreamedChunk(f.cmd, f.runtime, coord); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if f.cmd.EntityExists(entity) || f.runtime.LoadedChunks[coord] != nil {
		t.Fatal("normal terrain unload retained CPU residency")
	}
	if _, exists := f.assets.GetVoxelGeometry(id); exists {
		t.Error("normal terrain unload retained its adopted asset")
	}
	if _, exists := f.assets.GetVoxelGeometry(unrelated); !exists {
		t.Fatal("terrain asset retirement deleted an unrelated server asset")
	}
	p5bTerrainGeometry(t, asset.XBrickMap, false)
	if occupied, value := object.XBrickMap.GetVoxel(9, 0, 11); !occupied || value != 7 {
		t.Fatal("asset retirement cleared retained renderer geometry")
	}
}

func TestP5bTerrainCurrentRemovalAfterWorkerPublicationUsesDefensiveFallback(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "current-removal", true: "non-nil-stale-source"}[stale], func(t *testing.T) {
			coord := ChunkCoord{}
			f, _, chunks := p5bRuntime(t, []ChunkCoord{coord})
			s1fPrepared(t, f, coord, false)
			chunk := chunks[coord]
			backing := NewVoxelBackingComponent(content.VoxelBackingOwnerTerrain, chunk.TerrainID, chunk.SourceHash, [3]int{}, 16, NewTerrainColumnVoxelBacking(chunk), nil)
			if !backing.MaterializeSphere(terrainChunkToXBrickMap(chunk), mgl32.Vec3{2.5, 1.5, 3.5}, 0.1) {
				t.Fatal("fixture did not record a real terrain removal")
			}
			if stale {
				backing.SourceHash = "older-columns"
			}
			// The worker already published. Commit must observe current backing,
			// including a non-nil overlay rejected by source-hash validation.
			f.runtime.recordVoxelBackingRemoval(backing)
			f.commitStage()
			entity, id, asset := p5bTerrainAsset(t, f, coord)
			p5bTerrainGeometry(t, asset.XBrickMap, !stale)
			liveBacking := s3cComponent[VoxelBackingComponent](t, f.cmd, entity)
			if liveBacking.Provider.VoxelValue([3]int{2, 1, 3}) != 3 || p5aAdoptions(t, f.runtime) != 0 {
				t.Fatal("live backing fallback lost base columns or adopted an ineligible payload")
			}
			if f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("terrain fallback retained discarded prepared storage")
			}
			if err := unloadStreamedChunk(f.cmd, f.runtime, coord); err != nil {
				t.Fatal(err)
			}
			f.app.FlushCommands()
			if _, exists := f.assets.GetVoxelGeometry(id); !exists {
				t.Fatal("ordinary defensive terrain registration changed its existing asset lifetime")
			}
		})
	}
}

func TestP5bDeferredTerrainMapsRetainPendingChargeUntilCancellationOrStop(t *testing.T) {
	for _, stop := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "stop"}[stop], func(t *testing.T) {
			f, observer, chunks := p5bRuntime(t, []ChunkCoord{{}, {X: 1}})
			for _, coord := range []ChunkCoord{{}, {X: 1}} {
				s1fPrepared(t, f, coord, false)
			}
			// Measure actual map storage without assuming an envelope layout.
			source := terrainChunkToXBrickMap(chunks[ChunkCoord{X: 1}])
			source.ComputeAABB()
			source.ClearDirty()
			copy := source.Copy()
			copy.ClearDirty()
			minimum := 2 * min(s2aCharge(t, source), s2aCharge(t, copy))
			f.commitStage()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			retained := f.runtime.Metrics.PendingPreparedBytes
			if f.runtime.Metrics.PreparedChunkQueueDepth != 1 || retained < minimum {
				t.Errorf("deferred terrain omitted source/copy storage: depth=%d pending=%d minimum=%d", f.runtime.Metrics.PreparedChunkQueueDepth, retained, minimum)
			}
			s1eWork(t, f.runtime, 1, 32, 0, 0)
			if stop {
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			} else {
				s3aMove(f.cmd, observer, mgl32.Vec3{1600, 1, 1})
				f.observerStage()
				if f.runtime.Metrics.PendingPreparedBytes != retained {
					t.Fatal("terrain cancellation released storage before acknowledgement")
				}
				f.commitStage()
				if f.runtime.Metrics.PrepareCancelledCount != 1 {
					t.Fatal("deferred terrain did not reach cancellation acknowledgement")
				}
			}
			if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.PreparedQueueDepth != 0 {
				t.Fatal("terminal terrain consumption retained pending storage")
			}
			s1eWork(t, f.runtime, 0, 32, 0, 0)
		})
	}
}

func TestP5bPartialTerrainCommitRetainsAssetAcrossFailedStopThenReleasesOnRetry(t *testing.T) {
	var f *streamedRenderHarness
	var hookEntity EntityId
	var hookAsset AssetId
	hook := func(cmd *Commands, context PostSpawnTerrainContext) {
		hookEntity = context.RootEntity
		model := mustVoxelModelComponentForLevelTest(t, cmd, hookEntity)
		hookAsset = model.GeometryAsset()
		asset, exists := f.assets.GetVoxelGeometry(hookAsset)
		if !cmd.EntityExists(hookEntity) || !exists {
			t.Fatal("terrain hook did not see synchronously flushed geometry")
		}
		p5bTerrainGeometry(t, asset.XBrickMap, false)
	}
	f, _, _ = p5bRuntime(t, []ChunkCoord{{}}, hook)
	s1fPrepared(t, f, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	// Fail the real later-placement branch after terrain registration/flush.
	prepared.PlacementItems = []streamedPlacementInstance{{PlacementID: "missing", AssetPath: filepath.Join(t.TempDir(), "missing.gkasset"), Transform: content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}}
	f.runtime.PreparedLoads <- prepared
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	entity, id, asset := p5bTerrainAsset(t, f, ChunkCoord{})
	if f.runtime.InitErr == nil || f.runtime.LoadedChunks[ChunkCoord{}] != nil || hookEntity != entity || hookAsset != id {
		t.Fatal("fixture did not fail after synchronously flushing terrain and running its hook")
	}
	if p5aAdoptions(t, f.runtime) != 1 {
		t.Error("partial terrain commit did not adopt its prepared geometry")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	original, generation := f.runtime.WorldDeltaPath, f.runtime.Generation
	f.runtime.WorldDeltaPath = filepath.Join(blocker, "delta.gkworlddelta")
	defer func() { f.runtime.WorldDeltaPath = original }()
	if err := StopStreamedLevelRuntime(f.cmd); err == nil {
		t.Fatal("fixture did not reject Stop persistence")
	}
	if !f.runtime.Initialized || f.runtime.Generation != generation || !f.cmd.EntityExists(entity) {
		t.Fatal("failed Stop discarded partial terrain residency")
	}
	if _, exists := f.assets.GetVoxelGeometry(id); !exists {
		t.Fatal("failed Stop released partial terrain asset ownership")
	}
	f.runtime.WorldDeltaPath = original
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if f.runtime.Initialized || f.cmd.EntityExists(entity) {
		t.Fatal("successful Stop retained partially committed terrain")
	}
	if _, exists := f.assets.GetVoxelGeometry(id); exists {
		t.Error("successful Stop leaked adopted terrain absent from LoadedChunks")
	}
	p5bTerrainGeometry(t, asset.XBrickMap, false)
	if f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("partial commit Stop retained pending terrain storage")
	}
}
