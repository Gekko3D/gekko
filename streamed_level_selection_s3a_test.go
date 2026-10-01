package gekko

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// The reference deliberately enumerates complete cubes. It shares no cube,
// shell, sector-selection, or filtering helper with the production selector.
func s3aCube(center ChunkCoord, radius int) map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{})
	for x := center.X - radius; x <= center.X+radius; x++ {
		for y := center.Y - radius; y <= center.Y+radius; y++ {
			for z := center.Z - radius; z <= center.Z+radius; z++ {
				out[ChunkCoord{X: x, Y: y, Z: z}] = struct{}{}
			}
		}
	}
	return out
}

func s3aSet(coords ...ChunkCoord) map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{}, len(coords))
	for _, coord := range coords {
		out[coord] = struct{}{}
	}
	return out
}

func s3aUnion(sets ...map[ChunkCoord]struct{}) map[ChunkCoord]struct{} {
	out := s3aSet()
	for _, set := range sets {
		for coord := range set {
			out[coord] = struct{}{}
		}
	}
	return out
}

func s3aAssertSet(t *testing.T, name string, got, want map[ChunkCoord]struct{}) {
	t.Helper()
	for coord := range want {
		if _, present := got[coord]; !present {
			t.Errorf("%s missing %v (got %d, want %d memberships)", name, coord, len(got), len(want))
			return
		}
	}
	for coord := range got {
		if _, present := want[coord]; !present {
			t.Errorf("%s unexpectedly contains %v (got %d, want %d memberships)", name, coord, len(got), len(want))
			return
		}
	}
}

type s3aDemand struct {
	desired, keep, collision, destruction map[ChunkCoord]struct{}
	desiredSectors, keepSectors           map[ChunkCoord]struct{}
	desiredProxies, keepProxies           map[ChunkCoord]struct{}
}

func s3aAssertDemand(t *testing.T, state *StreamedLevelRuntimeState, want s3aDemand) {
	t.Helper()
	for _, view := range []struct {
		name      string
		got, want map[ChunkCoord]struct{}
	}{
		{"desired chunks", state.DesiredChunks, want.desired},
		{"keep chunks", state.KeepChunks, want.keep},
		{"collision chunks", state.CollisionChunks, want.collision},
		{"destruction chunks", state.DestructionChunks, want.destruction},
		{"desired sectors", state.DesiredSectors, want.desiredSectors},
		{"keep sectors", state.KeepSectors, want.keepSectors},
		{"desired proxies", state.DesiredProxySectors, want.desiredProxies},
		{"keep proxies", state.KeepProxySectors, want.keepProxies},
	} {
		s3aAssertSet(t, view.name, view.got, view.want)
	}
}

func s3aReference(state *StreamedLevelRuntimeState, position mgl32.Vec3, observer StreamedLevelObserverComponent) s3aDemand {
	center := ChunkCoord{
		X: int(math.Floor(float64(position[0] / state.ChunkSize))),
		Y: int(math.Floor(float64(position[1] / state.ChunkSize))),
		Z: int(math.Floor(float64(position[2] / state.ChunkSize))),
	}
	load := observer.Radius
	if load <= 0 {
		load = state.StreamingRadius
	}
	keep := observer.KeepRadius
	if keep <= 0 {
		keep = state.StreamingKeepRadius
	}
	if keep < load {
		keep = load
	}
	prefetch := observer.PrefetchRadius
	if prefetch <= 0 {
		prefetch = state.StreamingPrefetchRadius
	}
	if prefetch < load {
		prefetch = load
	}
	collision := observer.CollisionRadius
	if collision <= 0 {
		collision = state.StreamingCollisionRadius
	}
	if collision <= 0 {
		collision = load
	}
	destruction := observer.DestructionRadius
	if destruction <= 0 {
		destruction = state.StreamingDestructionRadius
	}
	if destruction <= 0 {
		destruction = collision
	}
	return s3aDemand{desired: s3aCube(center, prefetch), keep: s3aCube(center, keep), collision: s3aCube(center, collision), destruction: s3aCube(center, destruction)}
}

func s3aObserver(cmd *Commands, position mgl32.Vec3, observer StreamedLevelObserverComponent) EntityId {
	return cmd.AddEntity(&TransformComponent{Position: position, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, &observer)
}

// ECS rows can move at flush, so these helpers find live components by identity.
func s3aMove(cmd *Commands, id EntityId, position mgl32.Vec3) {
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, transform *TransformComponent) bool {
		if eid == id {
			transform.Position = position
		}
		return true
	})
}

func s3aChangeRadii(cmd *Commands, id EntityId, observer StreamedLevelObserverComponent) {
	MakeQuery1[StreamedLevelObserverComponent](cmd).Map(func(eid EntityId, value *StreamedLevelObserverComponent) bool {
		if eid == id {
			*value = observer
		}
		return true
	})
}

func TestS3aSelectionIdleAndWithinChunkReuse(t *testing.T) {
	app, cmd, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{StreamingRadius: 2, StreamingKeepRadius: 3, StreamingPrefetchRadius: 4, StreamingCollisionRadius: 1, StreamingDestructionRadius: 1})
	position := mgl32.Vec3{-15, 1, 1}
	id := s3aObserver(cmd, position, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aReference(state, position, StreamedLevelObserverComponent{}))
	builds, visits := state.Metrics.ObserverSelectionBuildCount, state.Metrics.ObserverSelectionChunkVisitCount
	if builds == 0 || visits == 0 {
		t.Fatal("first observer evaluation did not report selection work")
	}
	for _, position := range []mgl32.Vec3{{-15, 1, 1}, {-0.25, 15.5, 15.5}, {-8, 4, 6}} {
		s3aMove(cmd, id, position)
		for range 3 {
			updateStreamedLevelObserverSystem(cmd, state)
			s3aAssertDemand(t, state, s3aReference(state, position, StreamedLevelObserverComponent{}))
		}
	}
	if state.Metrics.ObserverSelectionBuildCount != builds || state.Metrics.ObserverSelectionChunkVisitCount != visits {
		t.Fatalf("unchanged observer chunk/radii rebuilt selection: builds %v -> %v, visits %v -> %v", builds, state.Metrics.ObserverSelectionBuildCount, visits, state.Metrics.ObserverSelectionChunkVisitCount)
	}
}

func TestS3aSelectionMovementAndRadiusParity(t *testing.T) {
	app, cmd, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{StreamingRadius: 2, StreamingKeepRadius: 3, StreamingPrefetchRadius: 4, StreamingCollisionRadius: 1, StreamingDestructionRadius: 1})
	id := s3aObserver(cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	for _, tc := range []struct {
		name           string
		position       mgl32.Vec3
		observer       StreamedLevelObserverComponent
		chunkSize      float32
		changeDefaults bool
	}{
		{name: "origin", chunkSize: 16},
		{name: "axis", position: mgl32.Vec3{16.5, 1, 1}, chunkSize: 16},
		{name: "diagonal", position: mgl32.Vec3{32.5, 16.5, -0.5}, chunkSize: 16},
		{name: "negative diagonal", position: mgl32.Vec3{-0.5, -16.5, -32.5}, chunkSize: 16},
		{name: "teleport", position: mgl32.Vec3{16000.5, -16000.5, 8000.5}, chunkSize: 16},
		{name: "unequal radii", position: mgl32.Vec3{-16, 32, -48}, observer: StreamedLevelObserverComponent{Radius: 3, KeepRadius: 4, PrefetchRadius: 5, CollisionRadius: 2, DestructionRadius: 1}, chunkSize: 16},
		{name: "clamped keep and prefetch", position: mgl32.Vec3{-16, 32, -48}, observer: StreamedLevelObserverComponent{Radius: 3, KeepRadius: 1, PrefetchRadius: 1, CollisionRadius: 4, DestructionRadius: 2}, chunkSize: 16},
		{name: "negative radii use changed defaults", position: mgl32.Vec3{-16, 32, -48}, observer: StreamedLevelObserverComponent{Radius: -1, KeepRadius: -2, PrefetchRadius: -3, CollisionRadius: -4, DestructionRadius: -5}, chunkSize: 16, changeDefaults: true},
		{name: "chunk size", position: mgl32.Vec3{-16, 32, -48}, chunkSize: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s3aMove(cmd, id, tc.position)
			s3aChangeRadii(cmd, id, tc.observer)
			state.ChunkSize = tc.chunkSize
			if tc.changeDefaults {
				state.StreamingRadius, state.StreamingKeepRadius, state.StreamingPrefetchRadius = 1, 2, 3
				state.StreamingCollisionRadius, state.StreamingDestructionRadius = 0, 0
			}
			before := state.Metrics.ObserverSelectionBuildCount
			updateStreamedLevelObserverSystem(cmd, state)
			s3aAssertDemand(t, state, s3aReference(state, tc.position, tc.observer))
			if state.Metrics.ObserverSelectionBuildCount <= before {
				t.Fatal("changed selection inputs did not report a demand evaluation")
			}
		})
	}
}

func TestS3aSelectionSmallMovesVisitShells(t *testing.T) {
	const radius = 20
	app, cmd, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{StreamingRadius: radius, StreamingKeepRadius: radius, StreamingPrefetchRadius: radius, StreamingCollisionRadius: radius, StreamingDestructionRadius: radius})
	id := s3aObserver(cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	// Even one shared complete cube would repeat unchanged interior work. Five
	// independent shell traversals for these unit moves fit below this baseline.
	fullVolumeVisits := (2*radius + 1) * (2*radius + 1) * (2*radius + 1)
	cell := state.ChunkSize
	for _, position := range []mgl32.Vec3{{cell, 0, 0}, {2 * cell, cell, -cell}, {cell, 0, -2 * cell}} {
		before := state.Metrics.ObserverSelectionChunkVisitCount
		s3aMove(cmd, id, position)
		updateStreamedLevelObserverSystem(cmd, state)
		s3aAssertDemand(t, state, s3aReference(state, position, StreamedLevelObserverComponent{}))
		delta := state.Metrics.ObserverSelectionChunkVisitCount - before
		if delta == 0 || delta >= uint64(fullVolumeVisits) {
			t.Fatalf("one-cell move visited %v coordinates, want nonzero boundary work below %d visits for one complete cube", delta, fullVolumeVisits)
		}
	}
}

func TestS3aSelectionObserverLifetimePreservesSharedDemand(t *testing.T) {
	app, cmd, state, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
	firstRadii := StreamedLevelObserverComponent{Radius: 1, KeepRadius: 2, PrefetchRadius: 3, CollisionRadius: 2, DestructionRadius: 1}
	secondRadii := StreamedLevelObserverComponent{Radius: 2, KeepRadius: 3, PrefetchRadius: 2, CollisionRadius: 1, DestructionRadius: 2}
	firstPos, secondPos := mgl32.Vec3{}, mgl32.Vec3{16, 0, 0}
	first := s3aObserver(cmd, firstPos, firstRadii)
	second := s3aObserver(cmd, secondPos, secondRadii)
	// Queued additions become observers at the existing flush boundary.
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aDemand{})
	app.FlushCommands()
	assertBoth := func() {
		a, b := s3aReference(state, firstPos, firstRadii), s3aReference(state, secondPos, secondRadii)
		s3aAssertDemand(t, state, s3aDemand{desired: s3aUnion(a.desired, b.desired), keep: s3aUnion(a.keep, b.keep), collision: s3aUnion(a.collision, b.collision), destruction: s3aUnion(a.destruction, b.destruction)})
	}
	updateStreamedLevelObserverSystem(cmd, state)
	assertBoth()
	firstPos = mgl32.Vec3{-16, 16, 0}
	s3aMove(cmd, first, firstPos)
	updateStreamedLevelObserverSystem(cmd, state)
	assertBoth()
	firstPos = mgl32.Vec3{320, 0, 0}
	s3aMove(cmd, first, firstPos)
	updateStreamedLevelObserverSystem(cmd, state)
	assertBoth()
	cmd.RemoveComponents(first, &StreamedLevelObserverComponent{})
	updateStreamedLevelObserverSystem(cmd, state)
	assertBoth()
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aReference(state, secondPos, secondRadii))
	cmd.RemoveEntity(second)
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aReference(state, secondPos, secondRadii))
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aDemand{})
}

func s3aPolicyWorld(t *testing.T) string {
	t.Helper()
	path := s2bWorldPath(t)
	root := filepath.Dir(path)
	worldPath := filepath.Join(root, "imported-world.gkworld")
	world, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		t.Fatal(err)
	}
	world.Entries = nil
	coords := []ChunkCoord{{}, {X: 1}, {X: 2}, {X: 3}, {X: 3, Y: 1}, {X: 3, Y: -1}, {X: 8}, {X: 9}, {X: 10}, {X: 11}, {X: 12}, {X: 13}, {X: 14}, {X: 15}}
	for i, coord := range coords {
		chunkPath := filepath.Join(root, "selection-"+string(rune('a'+i))+".gkchunk")
		chunk := &content.ImportedWorldChunkDef{WorldID: world.WorldID, Coord: terrainCoordFromChunk(coord), ChunkSize: 16, VoxelResolution: 1}
		if coord.X != 11 {
			chunk.Voxels = []content.ImportedWorldVoxelDef{{Value: 1}}
			chunk.NonEmptyVoxelCount = 1
		}
		writeImportedWorldChunkForStreamedTest(t, chunkPath, chunk)
		world.Entries = append(world.Entries, content.ImportedWorldChunkEntryDef{Coord: chunk.Coord, ChunkPath: content.AuthorDocumentPath(chunkPath, worldPath), NonEmptyVoxelCount: chunk.NonEmptyVoxelCount})
	}
	world.Sectors = nil
	for _, x := range []int{0, 1, 2, 8, 9, 12, 13, 14} {
		sector := content.ImportedWorldSectorDef{Coord: content.TerrainChunkCoordDef{X: x}, BoundsMin: [3]float32{float32(x * 16), 0, 0}, BoundsMax: [3]float32{float32((x + 1) * 16), 16, 16}, FullChunkRefs: []content.TerrainChunkCoordDef{{X: x}}, NonEmptyVoxelCount: 1}
		switch x {
		case 0:
			sector.SourceLeafIDs = []int{1}
			sector.VisibleSectorRefs = []content.TerrainChunkCoordDef{{X: 8}}
			sector.AdjacentSectorRefs = []content.TerrainChunkCoordDef{{X: 9}}
		case 1:
			sector.VisibleSectorRefs = []content.TerrainChunkCoordDef{{X: 12}}
		case 2:
			sector.FullChunkRefs = append(sector.FullChunkRefs,
				content.TerrainChunkCoordDef{X: 3},
				content.TerrainChunkCoordDef{X: 3, Y: 1},
				content.TerrainChunkCoordDef{X: 3, Y: -1})
		case 8:
			sector.FullChunkRefs = append(sector.FullChunkRefs, content.TerrainChunkCoordDef{X: 10}, content.TerrainChunkCoordDef{X: 11}, content.TerrainChunkCoordDef{X: 15})
		case 13:
			sector.LODs = []content.ImportedWorldLODDef{{Level: 1, Kind: "voxel_proxy", ChunkPath: world.Entries[0].ChunkPath, ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 1}}
		case 14:
			sector.VisibleSectorRefs = []content.TerrainChunkCoordDef{{X: 2}}
		}
		world.Sectors = append(world.Sectors, sector)
	}
	if err := content.SaveImportedWorld(worldPath, world); err != nil {
		t.Fatal(err)
	}
	// Existing valid terrain and asset files provide real non-imported content.
	level, err := content.LoadLevel(path)
	if err != nil {
		t.Fatal(err)
	}
	level.Placements[0].Transform.Position = content.Vec3{48, 16, 0}
	if err := content.SaveLevel(path, level); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestS3aSelectionImportedPolicyAndExplicitInvalidation(t *testing.T) {
	app, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{LevelPath: s3aPolicyWorld(t), StreamingRadius: 1, StreamingKeepRadius: 1, StreamingPrefetchRadius: 3, StreamingCollisionRadius: 1, StreamingDestructionRadius: 1, MaxPrepareJobs: 1})
	backing := state.BaseWorldBacking
	state.BaseWorldBacking = nil
	hiddenSector := state.ImportedWorldSectors[ChunkCoord{X: 2}]
	hiddenSector.FullChunkRefs = []content.TerrainChunkCoordDef{{X: 2}}
	state.ImportedWorldSectors[ChunkCoord{X: 2}] = hiddenSector
	// Exercise metadata edits through their declared main-thread boundary. These
	// memberships need not equal FullChunkRefs: radius filtering and full-sector
	// expansion have distinct contracts.
	for _, coord := range []ChunkCoord{{X: 3}, {X: 3, Y: 1}, {X: 3, Y: -1}} {
		state.ImportedChunkSector[coord] = ChunkCoord{X: 2}
	}
	terrain := state.TerrainEntries[ChunkCoord{}]
	terrain.Coord.X, terrain.NonEmptyVoxelCount = 3, 1
	state.TerrainEntries[ChunkCoord{X: 3}] = terrain
	delete(state.ImportedWorldEntries, ChunkCoord{X: 15})
	state.InvalidateObserverSelection()
	first := s3aObserver(cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	expected := s3aReference(state, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	delete(expected.desired, ChunkCoord{X: 2})
	delete(expected.desired, ChunkCoord{X: 3, Y: -1})
	remote := s3aSet(ChunkCoord{X: 8}, ChunkCoord{X: 9}, ChunkCoord{X: 10}, ChunkCoord{X: 12})
	expected.desired = s3aUnion(expected.desired, remote)
	expected.keep = s3aUnion(expected.keep, remote)
	expected.desiredSectors = s3aSet(ChunkCoord{}, ChunkCoord{X: 1}, ChunkCoord{X: 8}, ChunkCoord{X: 9}, ChunkCoord{X: 12})
	expected.keepSectors = s3aUnion(expected.desiredSectors)
	expected.desiredProxies = s3aUnion(expected.desiredSectors, s3aSet(ChunkCoord{X: 13}))
	expected.keepProxies = s3aUnion(expected.desiredProxies)
	s3aAssertDemand(t, state, expected)
	builds, visits := state.Metrics.ObserverSelectionBuildCount, state.Metrics.ObserverSelectionChunkVisitCount
	updateStreamedLevelObserverSystem(cmd, state)
	if state.Metrics.ObserverSelectionBuildCount != builds || state.Metrics.ObserverSelectionChunkVisitCount != visits {
		t.Fatal("idle imported/PVS selection rebuilt radius demand")
	}
	// A second observer supplies sector visibility that preserves a coordinate
	// from the first observer's raw prefetch cube, absent from FullChunkRefs.
	secondPos := mgl32.Vec3{13 * 16, 0, 0}
	second := s3aObserver(cmd, secondPos, StreamedLevelObserverComponent{Radius: 1, KeepRadius: 1, PrefetchRadius: 1})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	secondDemand := s3aReference(state, secondPos, StreamedLevelObserverComponent{Radius: 1, KeepRadius: 1, PrefetchRadius: 1})
	bothSectors := s3aUnion(expected.desiredSectors, s3aSet(ChunkCoord{X: 2}, ChunkCoord{X: 13}, ChunkCoord{X: 14}))
	s3aAssertDemand(t, state, s3aDemand{
		desired:        s3aUnion(expected.desired, secondDemand.desired, s3aSet(ChunkCoord{X: 2}, ChunkCoord{X: 3, Y: -1})),
		keep:           s3aUnion(expected.keep, secondDemand.keep, s3aSet(ChunkCoord{X: 2})),
		collision:      s3aUnion(expected.collision, secondDemand.collision),
		destruction:    s3aUnion(expected.destruction, secondDemand.destruction),
		desiredSectors: bothSectors, keepSectors: bothSectors,
		desiredProxies: bothSectors, keepProxies: bothSectors,
	})
	cmd.RemoveEntity(second)
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, expected)
	// An in-place slice edit preserves map/slice sizes and observer inputs.
	sector := state.ImportedWorldSectors[ChunkCoord{}]
	sector.VisibleSectorRefs[0].X = 13
	state.InvalidateObserverSelection()
	updateStreamedLevelObserverSystem(cmd, state)
	delete(expected.desiredSectors, ChunkCoord{X: 8})
	expected.desiredSectors[ChunkCoord{X: 13}] = struct{}{}
	expected.keepSectors = s3aUnion(expected.desiredSectors)
	for _, coord := range []ChunkCoord{{X: 8}, {X: 10}} {
		delete(expected.desired, coord)
		delete(expected.keep, coord)
	}
	expected.desired[ChunkCoord{X: 13}], expected.keep[ChunkCoord{X: 13}] = struct{}{}, struct{}{}
	expected.desiredProxies, expected.keepProxies = s3aUnion(expected.desiredSectors), s3aUnion(expected.keepSectors)
	s3aAssertDemand(t, state, expected)
	// Restoring visibility and enabling real backing admits the empty entry;
	// a missing entry is still excluded by sector expansion.
	sector.VisibleSectorRefs[0].X = 8
	state.BaseWorldBacking = backing
	state.InvalidateObserverSelection()
	updateStreamedLevelObserverSystem(cmd, state)
	if _, present := state.DesiredChunks[ChunkCoord{X: 11}]; !present {
		t.Fatal("backed empty sector entry was not expanded after invalidation")
	}
	if _, present := state.DesiredChunks[ChunkCoord{X: 15}]; present {
		t.Fatal("sector expansion invented an absent imported entry")
	}
	state.Config.DisableSectorProxies = true
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertSet(t, "proxy toggle", state.DesiredProxySectors, state.DesiredSectors)
	state.Config.DisableSectorProxies = false
	cmd.RemoveEntity(first)
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aDemand{desiredProxies: s3aSet(ChunkCoord{X: 13}), keepProxies: s3aSet(ChunkCoord{X: 13})})
}

func TestS3aSelectionLastVisibilityMetadataRemovalUsesPrefetchSectors(t *testing.T) {
	app, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{LevelPath: s3aPolicyWorld(t), StreamingRadius: 1, StreamingPrefetchRadius: 3, StreamingKeepRadius: 3, DisableSectorProxies: true})
	s3aObserver(cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		sector := state.ImportedWorldSectors[coord]
		sector.VisibleSectorRefs, sector.AdjacentSectorRefs, sector.SourceLeafIDs = nil, nil, nil
		state.ImportedWorldSectors[coord] = sector
	}
	state.InvalidateObserverSelection()
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertSet(t, "prefetch sectors after final PVS metadata removal", state.DesiredSectors, s3aSet(ChunkCoord{}, ChunkCoord{X: 1}, ChunkCoord{X: 2}))
	if _, present := state.DesiredChunks[ChunkCoord{X: 2}]; !present {
		t.Fatal("prefetch imported content stayed hidden after PVS metadata removal")
	}
}

func s3aTwoChunkWorld(t *testing.T) string {
	t.Helper()
	path := s2bWorldPath(t)
	root := filepath.Dir(path)
	worldPath := filepath.Join(root, "imported-world.gkworld")
	world, err := content.LoadImportedWorld(worldPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := content.LoadImportedWorldChunk(filepath.Join(root, "imported-chunk.gkchunk"))
	if err != nil {
		t.Fatal(err)
	}
	second.Coord.X = 1
	writeImportedWorldChunkForStreamedTest(t, filepath.Join(root, "selection-second.gkchunk"), second)
	world.Entries = append(world.Entries, content.ImportedWorldChunkEntryDef{Coord: second.Coord, ChunkPath: "selection-second.gkchunk", NonEmptyVoxelCount: second.NonEmptyVoxelCount})
	world.Sectors[0].FullChunkRefs = append(world.Sectors[0].FullChunkRefs, second.Coord)
	if err := content.SaveImportedWorld(worldPath, world); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestS3aSelectionIdleContinuesPrepareSlotsAndByteRetry(t *testing.T) {
	app, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{LevelPath: s3aTwoChunkWorld(t), StreamingRadius: 1, DisableSectorProxies: true, MaxPrepareJobs: 1, MaxPendingPreparedBytes: 1, MaxChunkCommitsPerFrame: 1})
	s3aObserver(cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 })
	state.jobs.Wait()
	builds, visits := state.Metrics.ObserverSelectionBuildCount, state.Metrics.ObserverSelectionChunkVisitCount
	// The first completed result occupies credit. A freed worker slot must let
	// the other demanded chunk attempt admission while the observer stays idle.
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return len(state.PreparedLoads) == 2 })
	state.jobs.Wait()
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	app.FlushCommands()
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	app.FlushCommands()
	if state.Metrics.PendingPreparedAdmissionRetries == 0 || state.Metrics.PendingPreparedBytes != 0 || len(state.LoadedChunks) != 1 {
		t.Fatalf("real byte-pressure fixture did not commit one result and defer the other: %+v loaded=%d", state.Metrics, len(state.LoadedChunks))
	}
	// Committing the first payload restores byte credit. Retry/admission must
	// continue on unchanged demand, including a previously learned cost hint.
	updateStreamedLevelObserverSystem(cmd, state)
	s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 })
	state.jobs.Wait()
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	app.FlushCommands()
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		if state.LoadedChunks[coord] == nil {
			t.Fatalf("idle observer failed to load demanded chunk %v after credit returned", coord)
		}
	}
	if state.InitErr != nil || state.Metrics.PendingPreparedBytes != 0 {
		t.Fatalf("idle progress left an error or retained payload credit: err=%v bytes=%d", state.InitErr, state.Metrics.PendingPreparedBytes)
	}
	if state.Metrics.ObserverSelectionBuildCount != builds || state.Metrics.ObserverSelectionChunkVisitCount != visits {
		t.Fatal("preparation/retry progress rebuilt unchanged radius demand")
	}
	if !StreamedLevelCollisionReadyInBounds(cmd, state, mgl32.Vec3{1, 1, 1}, mgl32.Vec3{31, 2, 2}) {
		t.Fatal("idle committed chunks did not publish collision readiness")
	}
}

func TestS3aSelectionIdleContinuesRenderReadiness(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	f.runtime.StreamingRadius, f.runtime.StreamingKeepRadius, f.runtime.StreamingPrefetchRadius = 1, 1, 1
	s3aObserver(f.cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	f.app.FlushCommands()
	f.observerStage()
	proxy := f.proxy()
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	f.status(proxy, StreamedVoxelRenderReady)
	f.status(left, StreamedVoxelRenderReady)
	f.status(right, StreamedVoxelRenderUploading)
	f.commitStage()
	f.visibility(proxy, false)
	f.visibility(left, true)
	f.visibility(right, true)
	f.observerStage()
	builds, visits := f.runtime.Metrics.ObserverSelectionBuildCount, f.runtime.Metrics.ObserverSelectionChunkVisitCount
	f.status(right, StreamedVoxelRenderReady)
	f.observerStage()
	f.visibility(proxy, true)
	f.visibility(left, false)
	f.visibility(right, false)
	if f.runtime.Metrics.ObserverSelectionBuildCount != builds || f.runtime.Metrics.ObserverSelectionChunkVisitCount != visits {
		t.Fatal("render readiness rebuilt unchanged observer selection")
	}
}

func TestS3aSelectionSynchronousStartupDemandExpiresOnIdle(t *testing.T) {
	app, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{StreamingRadius: 1, DisableSectorProxies: true})
	position := mgl32.Vec3{1600, 0, 0}
	s3aObserver(cmd, position, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	builds, visits := state.Metrics.ObserverSelectionBuildCount, state.Metrics.ObserverSelectionChunkVisitCount
	if err := ensureStreamedChunkLoadedForPosition(cmd, assets, state, content.Vec3{1, 1, 1}); err != nil {
		t.Fatal(err)
	}
	app.FlushCommands()
	for _, set := range []map[ChunkCoord]struct{}{state.CollisionChunks, state.DestructionChunks} {
		if _, present := set[ChunkCoord{}]; !present {
			t.Fatal("synchronous startup did not publish temporary residency demand")
		}
	}
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aReference(state, position, StreamedLevelObserverComponent{}))
	if state.LoadedChunks[ChunkCoord{}] != nil {
		t.Fatal("synchronous out-of-keep chunk stayed pinned after idle update")
	}
	if state.Metrics.ObserverSelectionBuildCount != builds || state.Metrics.ObserverSelectionChunkVisitCount != visits {
		t.Fatal("temporary startup residency contaminated cached observer demand")
	}
}

func TestS3aSelectionRenderHandoffPinsExpireAfterProxyDisable(t *testing.T) {
	f := newStreamedRenderHarness(t, newVoxelRtStateTest())
	proxy := f.proxy()
	left, right := f.full(f.coords[0]), f.full(f.coords[1])
	f.status(left, StreamedVoxelRenderReady)
	f.status(right, StreamedVoxelRenderReady)
	f.status(proxy, StreamedVoxelRenderUploading)
	f.commitStage()
	s3aObserver(f.cmd, mgl32.Vec3{160, 0, 0}, StreamedLevelObserverComponent{})
	f.app.FlushCommands()
	f.observerStage()
	if f.runtime.LoadedChunks[f.coords[0]] == nil || f.runtime.LoadedChunks[f.coords[1]] == nil {
		t.Fatal("unready fallback did not retain the outgoing full cohort")
	}
	f.runtime.Config.DisableSectorProxies = true
	f.observerStage()
	if len(f.runtime.DesiredProxySectors) != 0 || len(f.runtime.KeepProxySectors) != 0 || len(f.runtime.LoadedChunks) != 0 {
		t.Fatal("disabled proxy handoff retained transient sector/full memberships")
	}
	builds, visits := f.runtime.Metrics.ObserverSelectionBuildCount, f.runtime.Metrics.ObserverSelectionChunkVisitCount
	f.observerStage()
	if len(f.runtime.DesiredProxySectors) != 0 || len(f.runtime.KeepProxySectors) != 0 {
		t.Fatal("idle selection resurrected an expired proxy pin")
	}
	if f.runtime.Metrics.ObserverSelectionBuildCount != builds || f.runtime.Metrics.ObserverSelectionChunkVisitCount != visits {
		t.Fatal("expired proxy pins caused repeated observer selection work")
	}
}

func TestS3aSelectionStopRestartDropsSessionMetadata(t *testing.T) {
	app, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{StreamingRadius: 1})
	id := s3aObserver(cmd, mgl32.Vec3{}, StreamedLevelObserverComponent{})
	app.FlushCommands()
	updateStreamedLevelObserverSystem(cmd, state)
	if len(state.DesiredSectors) == 0 || len(state.DesiredProxySectors) == 0 {
		t.Fatal("first session did not select its imported sector metadata")
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	s3aAssertDemand(t, state, s3aDemand{})
	// Keep the same observer entity and chunk/radii while starting a different
	// content session. Session ownership, rather than entity reuse, decides data.
	levelPath := filepath.Join(t.TempDir(), "selection-restart.gklevel")
	level := content.NewLevelDef("selection-restart")
	level.ChunkSize = 16
	if err := content.SaveLevel(levelPath, level); err != nil {
		t.Fatal(err)
	}
	if err := RestartStreamedLevelRuntime(cmd, assets, StreamedLevelRuntimeConfig{LevelPath: levelPath, StreamingRadius: 1}); err != nil {
		t.Fatal(err)
	}
	app.FlushCommands()
	if !cmd.EntityExists(id) {
		t.Fatal("fixture lost the independent observer across restart")
	}
	if state.Metrics.ObserverSelectionBuildCount != 0 || state.Metrics.ObserverSelectionChunkVisitCount != 0 {
		t.Fatal("new session inherited cumulative selection work metrics")
	}
	updateStreamedLevelObserverSystem(cmd, state)
	s3aAssertDemand(t, state, s3aReference(state, mgl32.Vec3{}, StreamedLevelObserverComponent{}))
	if state.Metrics.ObserverSelectionBuildCount == 0 || state.Metrics.ObserverSelectionChunkVisitCount == 0 {
		t.Fatal("retained observer skipped first demand evaluation in the new session")
	}
	var nilState *StreamedLevelRuntimeState
	nilState.InvalidateObserverSelection()
}
