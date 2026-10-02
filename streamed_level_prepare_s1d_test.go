package gekko

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type s1dChunkSpec struct {
	coord   ChunkCoord
	proxy   bool
	visible []ChunkCoord
}

// Every full/proxy reference points to a real, occupied chunk with matching
// coordinates. One chunk per sector keeps the priority examples unambiguous.
func s1dRuntime(t *testing.T, specs []s1dChunkSpec, radius, prefetch, keep int, editWorld ...func(*content.ImportedWorldDef)) (*Commands, *StreamedLevelRuntimeState, *AssetServer) {
	t.Helper()
	root := t.TempDir()
	world := &content.ImportedWorldDef{
		WorldID: "s1d", SchemaVersion: content.CurrentImportedWorldSchemaVersion,
		Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: 16, VoxelResolution: 1,
		Palette: []content.ImportedWorldPaletteColor{{0, 0, 0, 0}, {255, 100, 40, 255}},
	}
	for _, spec := range specs {
		coord := content.TerrainChunkCoordDef{X: spec.coord.X, Y: spec.coord.Y, Z: spec.coord.Z}
		name := fmt.Sprintf("chunk_%d_%d_%d.gkchunk", coord.X, coord.Y, coord.Z)
		chunk := &content.ImportedWorldChunkDef{
			WorldID: world.WorldID, SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion,
			Coord: coord, ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 1,
			Voxels: []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		}
		if err := content.SaveImportedWorldChunk(filepath.Join(root, name), chunk); err != nil {
			t.Fatal(err)
		}
		world.Entries = append(world.Entries, content.ImportedWorldChunkEntryDef{Coord: coord, ChunkPath: name, NonEmptyVoxelCount: 1})
		sector := content.ImportedWorldSectorDef{
			Coord: coord, FullChunkRefs: []content.TerrainChunkCoordDef{coord}, NonEmptyVoxelCount: 1,
			BoundsMin: [3]float32{float32(coord.X * 16), float32(coord.Y * 16), float32(coord.Z * 16)},
			BoundsMax: [3]float32{float32((coord.X + 1) * 16), float32((coord.Y + 1) * 16), float32((coord.Z + 1) * 16)},
		}
		if spec.proxy {
			sector.LODs = []content.ImportedWorldLODDef{{Level: 1, Kind: "voxel_proxy", ChunkPath: name, ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 1}}
		}
		if spec.visible != nil {
			sector.VisibilityID = "s1d-pvs"
			for _, visible := range spec.visible {
				sector.VisibleSectorRefs = append(sector.VisibleSectorRefs, content.TerrainChunkCoordDef{X: visible.X, Y: visible.Y, Z: visible.Z})
			}
		}
		world.Sectors = append(world.Sectors, sector)
	}
	for _, edit := range editWorld {
		edit(world)
	}
	worldPath := filepath.Join(root, "world.gkworld")
	if err := content.SaveImportedWorld(worldPath, world); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateImportedWorld(world, content.ImportedWorldValidationOptions{DocumentPath: worldPath}); validation.HasErrors() {
		t.Fatalf("invalid S1d world: %s", validation.Error())
	}
	level := content.NewLevelDef("s1d")
	level.ChunkSize, level.VoxelResolution = 16, 1
	level.BaseWorld = &content.LevelBaseWorldDef{Kind: content.ImportedWorldKindVoxelWorld, ManifestPath: "world.gkworld", CollisionEnabled: true}
	path := filepath.Join(root, "level.gklevel")
	if err := content.SaveLevel(path, level); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateLevel(level, content.LevelValidationOptions{DocumentPath: path}); validation.HasErrors() {
		t.Fatalf("invalid S1d level: %s", validation.Error())
	}
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{
		LevelPath: path, MaxPrepareJobs: 1, MaxChunkCommitsPerFrame: 1,
		StreamingRadius: radius, StreamingPrefetchRadius: prefetch, StreamingKeepRadius: keep,
		StreamingCollisionRadius: 1, StreamingDestructionRadius: 1,
	})
	return cmd, state, assets
}

type s1dAdmission struct {
	coord ChunkCoord
	kind  string
}

// Reflection lets the pre-S1d runtime reach ordering/aging failures. The public
// metric contract is checked separately and, once present, on every admission.
func s1dDispatchMetric(t *testing.T, state *StreamedLevelRuntimeState, required bool) (uint64, s1dAdmission, bool) {
	t.Helper()
	value := reflect.ValueOf(state.Metrics)
	count := value.FieldByName("PrepareDispatchCount")
	coord := value.FieldByName("LastPrepareDispatchCoord")
	kind := value.FieldByName("LastPrepareDispatchKind")
	if !count.IsValid() || !coord.IsValid() || !kind.IsValid() {
		if required {
			t.Fatal("missing public preparation admission metrics: PrepareDispatchCount, LastPrepareDispatchCoord, LastPrepareDispatchKind")
		}
		return 0, s1dAdmission{}, false
	}
	if count.Kind() != reflect.Uint64 || coord.Type() != reflect.TypeOf(ChunkCoord{}) || kind.Kind() != reflect.String {
		t.Fatal("preparation admission metric types must be uint64, ChunkCoord, string")
	}
	return count.Uint(), s1dAdmission{coord.Interface().(ChunkCoord), kind.String()}, true
}

func s1dPrepareAndCommit(t *testing.T, cmd *Commands, state *StreamedLevelRuntimeState, assets *AssetServer) s1dAdmission {
	t.Helper()
	before, _, metrics := s1dDispatchMetric(t, state, false)
	updateStreamedLevelObserverSystem(cmd, state)
	if len(state.PendingLoads)+len(state.PendingProxyLoads) != 1 {
		t.Fatalf("one-worker fixture admitted %d full and %d proxy jobs", len(state.PendingLoads), len(state.PendingProxyLoads))
	}
	var admission s1dAdmission
	for coord := range state.PendingLoads {
		admission = s1dAdmission{coord, "full"}
	}
	for coord := range state.PendingProxyLoads {
		admission = s1dAdmission{coord, "proxy"}
	}
	if metrics {
		count, reported, _ := s1dDispatchMetric(t, state, true)
		if count != before+1 || reported != admission {
			t.Fatalf("admission metrics = %d %v, want %d %v", count, reported, before+1, admission)
		}
	}
	s2bUntil(t, func() bool {
		return len(state.PreparedLoads)+len(state.PreparedProxyLoads) == 1 && streamedActivePrepareJobCounts(state) == 0
	})
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	cmd.app.FlushCommands()
	if state.InitErr != nil || state.Metrics.PrepareErrorCount != 0 {
		t.Fatalf("real preparation failed: %v", state.InitErr)
	}
	var entity EntityId
	if admission.kind == "proxy" {
		loaded := state.LoadedSectorProxies[admission.coord]
		if loaded == nil {
			t.Fatalf("admitted proxy %v did not commit", admission.coord)
		}
		entity = loaded.Entity
	} else {
		loaded := state.LoadedChunks[admission.coord]
		if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
			t.Fatalf("admitted full %v did not commit imported geometry", admission.coord)
		}
		for id := range loaded.ImportedWorldEntities {
			entity = id
		}
	}
	model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
	if geometry, ok := ResolveVoxelGeometryMap(assets, &model); !ok || geometry == nil || geometry.GetVoxelCount() != 1 {
		t.Fatalf("admitted %v published unusable geometry", admission)
	}
	return admission
}

func TestS1dPreparePublicAdmissionMetrics(t *testing.T) {
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}}, 1, 1, 1)
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	s1dDispatchMetric(t, state, true)
	s1dPrepareAndCommit(t, cmd, state, assets)
	count, last, _ := s1dDispatchMetric(t, state, true)
	for range 3 {
		updateStreamedLevelObserverSystem(cmd, state)
	}
	after, unchanged, _ := s1dDispatchMetric(t, state, true)
	if after != count || unchanged != last || count != 1 {
		t.Fatal("idle loaded demand changed actual admission metrics")
	}
}

func TestS1dPrepareSignedTiesIgnoreAuthoredInsertion(t *testing.T) {
	want := []ChunkCoord{{X: -1, Y: -1, Z: -1}, {X: -1, Y: -1, Z: 1}, {X: -1, Y: 1, Z: -1}, {X: 1, Y: -1, Z: -1}}
	for _, order := range [][]int{{3, 2, 1, 0}, {1, 3, 0, 2}, {2, 0, 3, 1}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			var specs []s1dChunkSpec
			for _, index := range order {
				specs = append(specs, s1dChunkSpec{coord: want[index]})
			}
			cmd, state, assets := s1dRuntime(t, specs, 1, 1, 1)
			s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
			cmd.app.FlushCommands()
			for _, coord := range want {
				if got := s1dPrepareAndCommit(t, cmd, state, assets); got != (s1dAdmission{coord, "full"}) {
					t.Fatalf("signed tie admission = %v, want full %v", got, coord)
				}
			}
		})
	}
}

func TestS1dPrepareSharedFallbackCollisionCurrentPrefetchOrder(t *testing.T) {
	proxy, collision, visible, prefetch, keep := ChunkCoord{X: 100}, ChunkCoord{}, ChunkCoord{X: 2}, ChunkCoord{X: -3}, ChunkCoord{X: -4}
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: prefetch}, {coord: visible}, {coord: keep}, {coord: collision}, {coord: proxy, proxy: true}}, 2, 3, 4)
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	for _, want := range []s1dAdmission{{proxy, "proxy"}, {collision, "full"}, {visible, "full"}, {prefetch, "full"}} {
		if got := s1dPrepareAndCommit(t, cmd, state, assets); got != want {
			t.Fatalf("shared priority admission = %v, want %v", got, want)
		}
	}
	updateStreamedLevelObserverSystem(cmd, state)
	if state.LoadedChunks[keep] != nil || len(state.PendingLoads)+len(state.PendingProxyLoads) != 0 {
		t.Fatal("keep-only demand initiated preparation")
	}
}

func TestS1dPrepareDestructionDemandBeforeVisibleDetail(t *testing.T) {
	destruction, visible := ChunkCoord{X: 2}, ChunkCoord{X: -3}
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: visible}, {coord: destruction}}, 3, 3, 3)
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{CollisionRadius: 1, DestructionRadius: 2})
	cmd.app.FlushCommands()
	for _, want := range []s1dAdmission{{destruction, "full"}, {visible, "full"}} {
		if got := s1dPrepareAndCommit(t, cmd, state, assets); got != want {
			t.Fatalf("destruction priority admission = %v, want %v", got, want)
		}
	}
}

func TestS1dPreparePVSFarDetailBeforeOtherObserverPrefetch(t *testing.T) {
	current, far, prefetch := ChunkCoord{}, ChunkCoord{X: 20}, ChunkCoord{X: 7}
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: prefetch}, {coord: far}, {coord: current, visible: []ChunkCoord{far}}}, 1, 3, 3)
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	s3aObserver(cmd, mgl32.Vec3{161, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	for _, want := range []s1dAdmission{{current, "full"}, {far, "full"}, {prefetch, "full"}} {
		if got := s1dPrepareAndCommit(t, cmd, state, assets); got != want {
			t.Fatalf("PVS detail admission = %v, want %v", got, want)
		}
	}
}

func TestS1dPrepareCurrentSectorExpansionBeforePrefetchSector(t *testing.T) {
	current, far, prefetch := ChunkCoord{}, ChunkCoord{X: 20}, ChunkCoord{X: -3}
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: current}, {coord: far}, {coord: prefetch}}, 1, 3, 3, func(world *content.ImportedWorldDef) {
		// One current sector owns a full chunk beyond the observer's radius.
		// No PVS metadata is present. The negative sector is prefetch-only.
		world.Sectors[0].FullChunkRefs = append(world.Sectors[0].FullChunkRefs, world.Sectors[1].FullChunkRefs...)
		world.Sectors[0].BoundsMax[0] = world.Sectors[1].BoundsMax[0]
		world.Sectors[0].NonEmptyVoxelCount = 2
		world.Sectors = append(world.Sectors[:1], world.Sectors[2:]...)
	})
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	for _, want := range []s1dAdmission{{current, "full"}, {far, "full"}, {prefetch, "full"}} {
		if got := s1dPrepareAndCommit(t, cmd, state, assets); got != want {
			t.Fatalf("current-sector detail admission = %v, want %v", got, want)
		}
	}
}

func TestS1dPrepareAgingDefeatsContinuouslyNewFallbacks(t *testing.T) {
	for _, detail := range []bool{false, true} {
		t.Run(map[bool]string{false: "collision", true: "visible-detail"}[detail], func(t *testing.T) {
			target, limit := ChunkCoord{}, 9
			if detail {
				target, limit = ChunkCoord{X: 2}, 17
			}
			specs := []s1dChunkSpec{{coord: target}}
			for step := range limit {
				specs = append(specs, s1dChunkSpec{coord: ChunkCoord{X: 100 + step}, proxy: true})
			}
			cmd, state, assets := s1dRuntime(t, specs, 2, 2, 2)
			lods := make(map[ChunkCoord][]content.ImportedWorldLODDef)
			for coord, sector := range state.ImportedWorldSectors {
				lods[coord] = sector.LODs
				sector.LODs = nil
				state.ImportedWorldSectors[coord] = sector
			}
			s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
			cmd.app.FlushCommands()
			for step := range limit {
				coord := ChunkCoord{X: 100 + step}
				sector := state.ImportedWorldSectors[coord]
				sector.LODs = lods[coord]
				state.ImportedWorldSectors[coord] = sector
				state.InvalidateObserverSelection()
				got := s1dPrepareAndCommit(t, cmd, state, assets)
				if got == (s1dAdmission{target, "full"}) {
					return
				}
				if got.kind != "proxy" {
					t.Fatalf("unexpected competing admission %v", got)
				}
			}
			t.Fatalf("fit-capable %v starved through %d continuously new fallback admissions", target, limit)
		})
	}
}

func TestS1dPrepareRenewedDemandAgeSurvivesCancelledAttemptAcknowledgement(t *testing.T) {
	for _, waitAfterReturn := range []int{0, 16} {
		t.Run(fmt.Sprintf("waiting_updates_%d", waitAfterReturn), func(t *testing.T) {
			target, fallback := ChunkCoord{X: 2}, ChunkCoord{X: 101}
			cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: target}, {coord: fallback, proxy: true}}, 2, 2, 2)
			sector := state.ImportedWorldSectors[fallback]
			lods := sector.LODs
			sector.LODs = nil
			state.ImportedWorldSectors[fallback] = sector
			observer := s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
			cmd.app.FlushCommands()
			manifestPath := content.ResolveDocumentPath(state.Level.BaseWorld.ManifestPath, state.LevelPath)
			path := content.ResolveImportedWorldChunkPath(state.ImportedWorldEntries[target], manifestPath)
			_, release, done := s2eHoldDecode(t, state.Loader, path, nil)
			s2eDispatchHeld(t, cmd, state)
			for range 16 {
				updateStreamedLevelObserverSystem(cmd, state)
			}
			s3aMove(cmd, observer, mgl32.Vec3{16000, 1, 1})
			updateStreamedLevelObserverSystem(cmd, state)
			if _, desired := state.DesiredChunks[target]; desired {
				t.Fatal("fixture did not withdraw target demand")
			}
			s3aMove(cmd, observer, mgl32.Vec3{1, 1, 1})
			sector.LODs = lods
			state.ImportedWorldSectors[fallback] = sector
			state.InvalidateObserverSelection()
			updateStreamedLevelObserverSystem(cmd, state)
			for range waitAfterReturn {
				updateStreamedLevelObserverSystem(cmd, state)
			}
			if len(state.PendingLoads) != 1 || len(state.PendingProxyLoads) != 0 || state.Loader.Stats().LoadWaits != 1 {
				t.Fatal("renewed demand overlapped the held, cancelled attempt")
			}
			if count, _, present := s1dDispatchMetric(t, state, false); present && count != 1 {
				t.Fatalf("held attempt reported %d admissions, want 1", count)
			}
			release()
			if result := s2bWait(t, done); result.err != nil {
				t.Fatal(result.err)
			}
			s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 && streamedActivePrepareJobCounts(state) == 0 })
			commitPreparedStreamedChunksSystem(cmd, assets, state)
			cmd.app.FlushCommands()
			if state.LoadedChunks[target] != nil || len(state.PendingLoads) != 0 {
				t.Fatal("obsolete attempt committed or retained pending ownership")
			}
			want := s1dAdmission{fallback, "proxy"}
			if waitAfterReturn > 0 {
				want = s1dAdmission{target, "full"}
			}
			if got := s1dPrepareAndCommit(t, cmd, state, assets); got != want {
				t.Fatalf("renewed waiting demand admission = %v, want %v", got, want)
			}
		})
	}
}

func TestS1dPrepareKnownByteBlockedFallbackAdmitsSmallerFull(t *testing.T) {
	first, second, fitting, fallback := ChunkCoord{}, ChunkCoord{X: 1}, ChunkCoord{X: 2}, ChunkCoord{X: 100}
	cmd, state, assets := s1dRuntime(t, []s1dChunkSpec{{coord: first}, {coord: second}, {coord: fitting}, {coord: fallback, proxy: true}}, 1, 1, 2)
	state.Config.DisableSectorProxies = true
	s3aObserver(cmd, mgl32.Vec3{1, 1, 1}, StreamedLevelObserverComponent{})
	cmd.app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 && streamedActivePrepareJobCounts(state) == 0 })
	refreshStreamedRuntimeMetricsCounts(state)
	smallBytes := state.Metrics.PendingPreparedBytes
	if smallBytes <= 0 {
		t.Fatal("real full preparation did not expose its pending charge")
	}

	// Persist a genuinely larger fallback before restarting the measured
	// fixture. Both its chunk and authored metadata report the dense payload.
	manifestPath := content.ResolveDocumentPath(state.Level.BaseWorld.ManifestPath, state.LevelPath)
	world, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for index := range world.Entries {
		entry := &world.Entries[index]
		if entry.Coord.X != fallback.X {
			continue
		}
		path := content.ResolveImportedWorldChunkPath(*entry, manifestPath)
		chunk, err := content.LoadImportedWorldChunk(path)
		if err != nil {
			t.Fatal(err)
		}
		chunk.Voxels = nil
		for x := 0; x < 16; x++ {
			for y := 0; y < 16; y++ {
				for z := 0; z < 16; z++ {
					chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 1})
				}
			}
		}
		chunk.NonEmptyVoxelCount = len(chunk.Voxels)
		entry.NonEmptyVoxelCount = chunk.NonEmptyVoxelCount
		if err := content.SaveImportedWorldChunk(path, chunk); err != nil {
			t.Fatal(err)
		}
	}
	for index := range world.Sectors {
		sector := &world.Sectors[index]
		if sector.Coord.X == fallback.X {
			sector.NonEmptyVoxelCount = 4096
			sector.LODs[0].NonEmptyVoxelCount = 4096
		}
	}
	if err := content.SaveImportedWorld(manifestPath, world); err != nil {
		t.Fatal(err)
	}
	if validation := content.ValidateImportedWorld(world, content.ImportedWorldValidationOptions{DocumentPath: manifestPath}); validation.HasErrors() {
		t.Fatalf("invalid byte-pressure world: %s", validation.Error())
	}
	config := state.Config
	config.MaxPendingPreparedBytes = 3 * smallBytes
	if err := RestartStreamedLevelRuntime(cmd, assets, config); err != nil {
		t.Fatal(err)
	}
	cmd.app.FlushCommands()
	for queued := 1; queued <= 2; queued++ {
		updateStreamedLevelObserverSystem(cmd, state)
		s2bUntil(t, func() bool { return len(state.PreparedLoads) == queued && streamedActivePrepareJobCounts(state) == 0 })
	}
	refreshStreamedRuntimeMetricsCounts(state)
	queuedBytes := state.Metrics.PendingPreparedBytes
	if queuedBytes <= 0 || queuedBytes > config.MaxPendingPreparedBytes || len(state.PendingLoads) != 2 {
		t.Fatal("fixture did not queue two fitting full payloads")
	}
	state.Config.DisableSectorProxies = false
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return len(state.PreparedProxyLoads) == 1 && streamedActivePrepareJobCounts(state) == 0 })
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PendingPreparedAdmissionRetries != 1 || state.Metrics.PendingPreparedBytes != queuedBytes {
		t.Fatal("larger fallback did not produce a real byte-admission retry")
	}
	// The normal commit budget acknowledges the retry and commits one full,
	// leaving the other actual prepared result charged in the public queue.
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	cmd.app.FlushCommands()
	retainedBytes := state.Metrics.PendingPreparedBytes
	if retainedBytes <= 0 || retainedBytes >= queuedBytes || len(state.PreparedLoads) != 1 || len(state.PendingProxyLoads) != 0 {
		t.Fatal("commit fixture did not retain genuine nonzero byte pressure")
	}
	before, _, metrics := s1dDispatchMetric(t, state, false)
	state.StreamingRadius, state.StreamingPrefetchRadius = 2, 2
	updateStreamedLevelObserverSystem(cmd, state)
	if _, pending := state.PendingLoads[fitting]; !pending || len(state.PendingLoads) != 2 || len(state.PendingProxyLoads) != 0 {
		t.Fatal("known byte-blocked fallback prevented fitting full admission")
	}
	if metrics {
		count, last, _ := s1dDispatchMetric(t, state, true)
		if count != before+1 || last != (s1dAdmission{fitting, "full"}) {
			t.Fatalf("fitting admission metrics = %d %v, want %d full %v", count, last, before+1, fitting)
		}
	}
	s2bUntil(t, func() bool { return len(state.PreparedLoads) == 2 && streamedActivePrepareJobCounts(state) == 0 })
	refreshStreamedRuntimeMetricsCounts(state)
	if state.Metrics.PendingPreparedBytes <= retainedBytes || state.Metrics.PendingPreparedBytes > config.MaxPendingPreparedBytes || state.Metrics.PendingPreparedAdmissionRetries != 1 {
		t.Fatal("alternate full failed real byte admission or rebuilt the blocked fallback")
	}
	for range 2 {
		commitPreparedStreamedChunksSystem(cmd, assets, state)
		cmd.app.FlushCommands()
	}
	for _, coord := range []ChunkCoord{first, second, fitting} {
		loaded := state.LoadedChunks[coord]
		if loaded == nil || len(loaded.ImportedWorldEntities) != 1 {
			t.Fatalf("real full %v did not commit under byte pressure", coord)
		}
		for entity := range loaded.ImportedWorldEntities {
			model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
			if geometry, ok := ResolveVoxelGeometryMap(assets, &model); !ok || geometry == nil || geometry.GetVoxelCount() != 1 {
				t.Fatalf("full %v published unusable geometry", coord)
			}
		}
	}
	if state.InitErr != nil || state.Metrics.PendingPreparedBytes != 0 || len(state.PendingLoads) != 0 {
		t.Fatal("pressure fixture did not drain real payload ownership cleanly")
	}
	// The baseline can establish this fixture's behavioral validity before
	// failing the new public admission-metric contract.
	s1dDispatchMetric(t, state, true)
}
