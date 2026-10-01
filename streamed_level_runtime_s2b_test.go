package gekko

import (
	"github.com/gekko3d/gekko/content"
	"os"
	"path/filepath"
	"testing"
)

func s2bWorldPath(t *testing.T) string {
	t.Helper()
	files := s2bContentFiles(t)
	world, err := content.LoadImportedWorld(files[5].path)
	if err != nil {
		t.Fatal(err)
	}
	backing, err := content.LoadVoxelBacking(files[7].path)
	if err != nil {
		t.Fatal(err)
	}
	world.Backing = &content.VoxelBackingRefDef{Path: "backing.gkvoxelbacking", Kind: backing.Kind, SourceHash: backing.SourceHash, BoundsMin: backing.BoundsMin, BoundsMax: backing.BoundsMax}
	world.Entries[0].Aux = nil
	world.Sectors[0].NonEmptyVoxelCount = 256
	world.Sectors[0].BoundsMax = [3]float32{16, 16, 16}
	world.Sectors[0].LODs = []content.ImportedWorldLODDef{{Level: 1, Kind: "voxel_proxy", ChunkPath: "imported-chunk.gkchunk", ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 256}}
	if err := content.SaveImportedWorld(files[5].path, world); err != nil {
		t.Fatal(err)
	}
	level := content.NewLevelDef("s2b-world")
	level.ChunkSize = 16
	level.VoxelResolution = 1
	level.BaseWorld = &content.LevelBaseWorldDef{Kind: content.ImportedWorldKindVoxelWorld, ManifestPath: "imported-world.gkworld", CollisionEnabled: true}
	writeTerrainSourceForStreamedTest(t, filepath.Join(filepath.Dir(files[1].path), "terrain.gkterrain"))
	level.Terrain = &content.LevelTerrainDef{Kind: content.TerrainKindHeightfield, SourcePath: "terrain.gkterrain", ManifestPath: "terrain-manifest.gkterrainmanifest"}
	// Place an ordinary procedural asset away from the chunk under test. This
	// gives Stop an authored placement index to release without adding work to
	// full/proxy preparation at coordinate zero.
	level.Placements = []content.LevelPlacementDef{{ID: "p", AssetPath: "asset.gkasset", Transform: content.LevelTransformDef{Position: content.Vec3{160, 0, 0}}}}
	level.Markers = []content.LevelMarkerDef{{
		ID: "s2b-marker", Name: "S2b reference", Kind: "reference",
		Transform: content.LevelTransformDef{Position: content.Vec3{2, 2, 2}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
	}}
	level.Lights = []content.LevelLightDef{{
		ID: "s2b-light", Name: "S2b light", Type: content.LevelLightTypePoint,
		Color: [3]float32{1, 1, 1}, Intensity: 2, Range: 8,
		Transform: content.LevelTransformDef{Position: content.Vec3{2, 4, 2}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
	}}
	if err := content.SaveLevel(files[1].path, level); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateLevel(level, content.LevelValidationOptions{DocumentPath: files[1].path}); validation.HasErrors() {
		t.Fatalf("invalid S2b world fixture: %s", validation.Error())
	}
	return files[1].path
}
func s2bStartWorld(t *testing.T, cfg StreamedLevelRuntimeConfig) (*App, *Commands, *StreamedLevelRuntimeState, *AssetServer) {
	t.Helper()
	if cfg.LevelPath == "" {
		cfg.LevelPath = s2bWorldPath(t)
	}
	return s2aStartRuntime(t, cfg)
}
func s2bAssertDecodedMetrics(t *testing.T, state *StreamedLevelRuntimeState) {
	t.Helper()
	stats := state.Loader.Stats()
	metrics := state.Metrics
	if metrics.DecodedContentCacheBytes != stats.Bytes || metrics.DecodedContentCachePinnedBytes != stats.PinnedBytes || metrics.DecodedContentCacheMaxBytes != stats.MaxBytes || metrics.DecodedContentCacheOverBudgetBytes != stats.OverBudgetBytes || metrics.DecodedContentCacheEntries != stats.Entries {
		t.Fatalf("decoded owner pressure not exposed: metrics=%+v owner=%+v", metrics, stats)
	}
}

func TestS2bRuntimeMetadataPinsBackingAndReleasesOwnedLoaderOnStop(t *testing.T) {
	_, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1, MaxPendingPreparedBytes: 37})
	cmd.app.FlushCommands()
	marker, light := state.MarkerEntities["s2b-marker"], state.LightEntities["s2b-light"]
	if marker == 0 || light == 0 || !hasComponentOfType[AuthoredLevelMarkerRefComponent](cmd, marker) || !hasComponentOfType[LightComponent](cmd, light) || state.TerrainID == "" || state.BaseWorldID == "" {
		t.Fatal("fixture missing live marker/light indexes or world IDs")
	}
	loader := state.Loader
	stats := loader.Stats()
	if stats.MaxBytes != 1 || stats.PinnedBytes <= 1 || stats.Entries < 4 {
		t.Fatalf("world metadata/backing not leased under tiny ceiling: %+v", stats)
	}
	if len(state.TerrainEntries) != 1 || len(state.ImportedWorldEntries) != 1 || len(state.ImportedWorldSectors) != 1 || len(state.PlacementsByChunk) != 1 {
		t.Fatal("fixture missing shallow metadata indexes")
	}
	provider := state.BaseWorldBacking
	if provider == nil {
		t.Fatal("fixture missing voxel backing")
	}
	before := provider.VoxelValue([3]int{6, 2, 2})
	if before == 0 {
		t.Fatal("backing query fixture is not occupied")
	}
	rawPath := filepath.Join(filepath.Dir(state.LevelPath), "asset.gkasset")
	if _, err := loader.LoadAsset(rawPath); err != nil {
		t.Fatal(err)
	}
	if loader.Stats().PinnedBytes != stats.PinnedBytes {
		t.Fatal("State.Loader accidentally joins metadata scope")
	}
	loader.Clear()
	if provider.VoxelValue([3]int{6, 2, 2}) != before || len(state.BaseWorldManifest.Entries) != 1 {
		t.Fatal("pressure invalidated live metadata/backing query")
	}
	refreshStreamedRuntimeMetricsCounts(state)
	s2bAssertDecodedMetrics(t, state)
	if state.Metrics.PendingPreparedMaxBytes != 37 {
		t.Fatalf("pending budget ignored: %+v", state.Metrics)
	}
	path := state.LevelPath
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	if stats := loader.Stats(); stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
		t.Fatalf("owned loader retained stopped session: %+v", stats)
	}
	if len(state.TerrainEntries) != 0 || len(state.ImportedWorldEntries) != 0 || len(state.ImportedWorldSectors) != 0 || len(state.PlacementsByChunk) != 0 || len(state.PlacementChunk) != 0 || len(state.ImportedChunkSector) != 0 {
		t.Fatal("Stop retained shallow decoded metadata")
	}
	if len(cmd.GetAllComponents(marker)) != 0 || len(cmd.GetAllComponents(light)) != 0 {
		t.Fatal("successful Stop left marker/light entities alive")
	}
	if len(state.MarkerEntities) != 0 || len(state.LightEntities) != 0 || state.TerrainID != "" || state.BaseWorldID != "" {
		t.Fatalf("successful Stop retained ended-session metadata: markers=%d lights=%d terrain_id=%q base_world_id=%q; want empty indexes and IDs",
			len(state.MarkerEntities), len(state.LightEntities), state.TerrainID, state.BaseWorldID)
	}
	if state.Metrics.DecodedContentCacheBytes != 0 || state.Metrics.DecodedContentCachePinnedBytes != 0 || state.Metrics.DecodedContentCacheOverBudgetBytes != 0 {
		t.Fatalf("Stop left stale decoded pressure: %+v", state.Metrics)
	}
	if err := StartStreamedLevelRuntime(cmd, assetServerFromApp(cmd.app), StreamedLevelRuntimeConfig{LevelPath: path, MaxDecodedContentCacheBytes: 1234, MaxPendingPreparedBytes: 19}); err != nil {
		t.Fatal(err)
	}
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Loader == loader || state.Loader.Stats().MaxBytes != 1234 || state.Metrics.PendingPreparedBytes != 0 || state.Metrics.PendingPreparedMaxBytes != 19 {
		t.Fatal("restart reused stopped owner or old budget")
	}
}

func TestS2bRuntimeSuppliedLoaderKeepsOptionsExternalScopeAndWarmEntries(t *testing.T) {
	path := s2bWorldPath(t)
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1 << 30})
	external := loader.NewScope()
	t.Cleanup(external.Close)
	assetPath := filepath.Join(filepath.Dir(path), "asset.gkasset")
	asset, err := external.Loader().LoadAsset(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	externalPin := loader.Stats().PinnedBytes
	_, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{LevelPath: path, Loader: loader, MaxDecodedContentCacheBytes: 1})
	if state.Loader != loader || loader.Stats().MaxBytes != 1<<30 || loader.Stats().PinnedBytes <= externalPin {
		t.Fatalf("runtime retuned supplied loader or skipped metadata scope: %+v", loader.Stats())
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	// Capture the public metrics directly on return. A frame or explicit
	// refresh would hide stale ownership reporting after the loader detaches.
	stoppedMetrics := state.Metrics
	if state.Loader != nil {
		t.Fatal("successful Stop retained the runtime's loader reference")
	}
	if stats := loader.Stats(); stats.PinnedBytes != externalPin || stats.Bytes <= externalPin {
		t.Fatalf("Stop cleared supplied warm/external ownership: %+v", stats)
	}
	if err := os.Remove(assetPath); err != nil {
		t.Fatal(err)
	}
	got, err := external.Loader().LoadAsset(assetPath)
	if err != nil || got != asset {
		t.Fatalf("external scope stopped being usable: %v", err)
	}
	if stoppedMetrics.DecodedContentCacheBytes != 0 || stoppedMetrics.DecodedContentCachePinnedBytes != 0 ||
		stoppedMetrics.DecodedContentCacheEntries != 0 || stoppedMetrics.DecodedContentCacheOverBudgetBytes != 0 {
		t.Fatalf("successful Stop reported external loader ownership as runtime ownership: bytes=%d pinned=%d entries=%d over_budget=%d; want all zero",
			stoppedMetrics.DecodedContentCacheBytes, stoppedMetrics.DecodedContentCachePinnedBytes,
			stoppedMetrics.DecodedContentCacheEntries, stoppedMetrics.DecodedContentCacheOverBudgetBytes)
	}
}

func TestS2bRuntimeFailedStopKeepsMetadataUsable(t *testing.T) {
	_, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1})
	cmd.app.FlushCommands()
	marker, light := state.MarkerEntities["s2b-marker"], state.LightEntities["s2b-light"]
	terrainID, worldID := state.TerrainID, state.BaseWorldID
	if marker == 0 || light == 0 || !hasComponentOfType[AuthoredLevelMarkerRefComponent](cmd, marker) || !hasComponentOfType[LightComponent](cmd, light) || terrainID == "" || worldID == "" {
		t.Fatal("fixture missing live marker/light indexes or world IDs")
	}
	loader := state.Loader
	before := loader.Stats()
	generation := state.Generation
	manifest := state.BaseWorldManifest
	provider := state.BaseWorldBacking
	value := provider.VoxelValue([3]int{6, 2, 2})
	if value == 0 {
		t.Fatal("backing query fixture is not occupied")
	}
	original := state.WorldDeltaPath
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	state.WorldDeltaPath = filepath.Join(blocker, "delta")
	defer func() { state.WorldDeltaPath = original }()
	if err := StopStreamedLevelRuntime(cmd); err == nil {
		t.Fatal("fixture failed to reject persistence")
	}
	if !state.Initialized || state.Generation != generation || state.Loader != loader || state.BaseWorldManifest != manifest || state.BaseWorldBacking != provider {
		t.Fatal("failed Stop abandoned active metadata owner")
	}
	if state.TerrainID != terrainID || state.BaseWorldID != worldID || state.MarkerEntities["s2b-marker"] != marker || state.LightEntities["s2b-light"] != light ||
		!hasComponentOfType[AuthoredLevelMarkerRefComponent](cmd, marker) || !hasComponentOfType[LightComponent](cmd, light) {
		t.Fatal("failed Stop discarded active world IDs or live marker/light indexes")
	}
	if after := loader.Stats(); after.PinnedBytes != before.PinnedBytes || after.Bytes != before.Bytes {
		t.Fatalf("failed Stop dropped live decoded leases: before=%+v after=%+v", before, after)
	}
	loader.Clear()
	if provider.VoxelValue([3]int{6, 2, 2}) != value || len(state.ImportedWorldEntries) != 1 {
		t.Fatal("failed Stop made world queries unusable")
	}
}

func TestS2bRuntimeStartupFailuresReleaseUnpublishedAndRetainPublishedScope(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "before publication", true: "partial session"}[published], func(t *testing.T) {
			path := s2bWorldPath(t)
			loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1})
			level, err := content.LoadLevel(path)
			if err != nil {
				t.Fatal(err)
			}
			if published {
				level.Terrain.ManifestPath = "missing"
			} else {
				level.Placements = append(level.Placements, level.Placements[0])
			}
			if err := content.SaveLevel(path, level); err != nil {
				t.Fatal(err)
			}
			app, cmd, state := newStreamedRuntimeHarness(t)
			t.Cleanup(func() {
				if state.Initialized {
					if err := StopStreamedLevelRuntime(cmd); err != nil {
						t.Errorf("partial startup cleanup: %v", err)
					}
				}
			})
			if err := StartStreamedLevelRuntime(cmd, assetServerFromApp(app), StreamedLevelRuntimeConfig{LevelPath: path, Loader: loader}); err == nil {
				t.Fatal("fixture did not fail startup")
			}
			if state.Initialized != published {
				t.Fatalf("startup publication fixture wrong: initialized=%t", state.Initialized)
			}
			if pinned := loader.Stats().PinnedBytes; (pinned > 0) != published {
				t.Fatalf("failed startup scope lifetime wrong: %+v", loader.Stats())
			}
		})
	}
}

func TestS2bRuntimeSynchronousCollisionUsesTransientDecodeScope(t *testing.T) {
	_, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxDecodedContentCacheBytes: 1})
	metadata := state.Loader.Stats().PinnedBytes
	if err := ensureStreamedChunkLoadedForPosition(cmd, assets, state, content.Vec3{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	if state.Loader.Stats().PinnedBytes != metadata || state.Metrics.PendingPreparedBytes != 0 {
		t.Fatalf("synchronous collision commit leaked transient ownership: loader=%+v metrics=%+v", state.Loader.Stats(), state.Metrics)
	}
	loaded := state.LoadedChunks[ChunkCoord{}]
	if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
		t.Fatal("synchronous startup failed to commit imported chunk")
	}
	state.Loader.Clear()
	for entity := range loaded.ImportedWorldEntities {
		model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
		physics, ok := buildFallbackPhysicsModelFromVoxel(assets, nil, &model)
		if !ok || physics.Grid == nil {
			t.Fatal("collision grid depends on released decoded scope")
		}
		if occupied, value := physics.Grid.GetVoxel(0, 0, 0); !occupied || value == 0 {
			t.Fatal("released decoded chunk invalidated collision geometry")
		}
	}
}

func TestS2bRuntimeNegativePendingBudgetRejectedAndDefaultBudgets(t *testing.T) {
	path := s2bWorldPath(t)
	app, cmd, state := newStreamedRuntimeHarness(t)
	if err := StartStreamedLevelRuntime(cmd, assetServerFromApp(app), StreamedLevelRuntimeConfig{LevelPath: path, MaxPendingPreparedBytes: -1}); err == nil {
		if state.Initialized {
			StopStreamedLevelRuntime(cmd)
		}
		t.Fatal("negative pending budget accepted")
	}
	_, _, defaults, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{})
	refreshStreamedRuntimeMetricsCounts(defaults)
	if defaults.Loader.Stats().MaxBytes != 128<<20 || defaults.Metrics.PendingPreparedMaxBytes != 128<<20 {
		t.Fatal("default decoded/pending budgets must be 128 MiB")
	}
}
