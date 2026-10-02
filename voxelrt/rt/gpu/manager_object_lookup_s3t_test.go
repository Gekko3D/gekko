package gpu

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func s3tTerrain(group uint32, coord [3]int) *core.VoxelObject {
	object := core.NewVoxelObject()
	object.IsTerrainChunk, object.TerrainGroupID, object.TerrainChunkSize, object.TerrainChunkCoord = true, group, 16, coord
	return object
}

func s3tPlanet(group uint32, tile [4]int) *core.VoxelObject {
	object := core.NewVoxelObject()
	object.IsPlanetTile, object.PlanetTileGroupID = true, group
	object.PlanetTileFace, object.PlanetTileLevel, object.PlanetTileX, object.PlanetTileY = tile[0], tile[1], tile[2], tile[3]
	return object
}

func s3tFixture() (*GpuBufferManager, *core.Scene) {
	dual := s3tTerrain(11, [3]int{-5, 0, 7})
	dual.IsPlanetTile, dual.PlanetTileGroupID = true, 13
	dual.PlanetTileFace, dual.PlanetTileLevel, dual.PlanetTileX, dual.PlanetTileY = 2, 3, -9, 4
	wrapped := int64(1)<<32 - 1
	scene := core.NewScene()
	scene.VisibleObjects = []*core.VoxelObject{
		nil, core.NewVoxelObject(),
		s3tTerrain(7, [3]int{-1, 2, -3}), s3tTerrain(7, [3]int{31, 2, -3}),
		s3tTerrain(7, [3]int{int(wrapped), 2, -3}), dual,
		s3tPlanet(13, [4]int{0, 1, -2, -3}), s3tPlanet(13, [4]int{0, 1, 30, -3}),
	}
	return &GpuBufferManager{ObjectLookupCacheBudgetBytes: DefaultObjectLookupCacheBudgetBytes}, scene
}

func s3tPrepare(t *testing.T, manager *GpuBufferManager, scene *core.Scene) (combined, planet []byte) {
	t.Helper()
	combined, planet = manager.prepareObjectLookupBytes(scene)
	terrainRows, terrainParams := buildTerrainChunkLookup(scene)
	planetRows, planetParams := buildPlanetTileLookup(scene)
	if !bytes.Equal(combined, serializeCombinedObjectLookupBuffer(terrainRows, terrainParams, planetRows, planetParams)) ||
		!bytes.Equal(planet, serializePlanetTileLookupBuffer(planetRows, planetParams)) {
		t.Fatal("prepared lookup bytes differ from existing shader serialization")
	}
	visits := 0
	if scene != nil {
		visits = len(scene.VisibleObjects)
	}
	if manager.ObjectLookupInputVisitsLastPrepare != visits {
		t.Fatalf("lookup input visits=%d, want one visible-object scan of %d entries", manager.ObjectLookupInputVisitsLastPrepare, visits)
	}
	if manager.ObjectLookupCacheBytes < 0 || manager.ObjectLookupCacheBytes > max(int64(0), manager.ObjectLookupCacheBudgetBytes) {
		t.Fatalf("lookup retention exceeds configured ceiling: bytes=%d budget=%d", manager.ObjectLookupCacheBytes, manager.ObjectLookupCacheBudgetBytes)
	}
	return combined, planet
}

// Read actual shader buffers, independently of the production lookup builders.
// The header chooses its table offset; first-match probing preserves duplicates.
func s3tQuery(t *testing.T, data []byte, header int, standalone, planet bool, group uint32, key [4]int32) int32 {
	t.Helper()
	word := func(offset int) uint32 {
		if offset < 0 || offset+4 > len(data) {
			t.Fatalf("lookup shader word outside %d-byte buffer: %d", len(data), offset)
		}
		return binary.LittleEndian.Uint32(data[offset : offset+4])
	}
	size, mask := word(header), word(header+4)
	if size == 0 {
		return -1
	}
	start := word(header + 8)
	if standalone {
		start = 1
	}
	hash := uint32(key[0])*73856093 ^ uint32(key[1])*19349663 ^ uint32(key[2])*83492791 ^ group*1640531513
	if planet {
		hash = uint32(key[0])*2654435761 ^ uint32(key[1])*2246822519 ^ uint32(key[2])*3266489917 ^ uint32(key[3])*668265263 ^ group*1640531513
	}
	for probe := uint32(0); probe < size; probe++ {
		offset := int(start+((hash+probe)&mask)) * 32
		id := int32(word(offset + 20))
		if id == -1 {
			return -1
		}
		match := word(offset+16) == group
		axes := 3
		if planet {
			axes = 4
		}
		for axis := 0; axis < axes; axis++ {
			match = match && int32(word(offset+axis*4)) == key[axis]
		}
		if match {
			return id
		}
	}
	return -1
}

func TestS3tLookupBytesPreserveShaderIDsDuplicatesCollisionsAndDualEligibility(t *testing.T) {
	manager, scene := s3tFixture()
	combined, planet := s3tPrepare(t, manager, scene)
	// Four terrain rows (including the wrapped duplicate) and three planet
	// rows each require a 16-slot table. Duplicate keys must not shrink it.
	for _, header := range []int{0, 32} {
		s3bWord(t, combined, header, 16)
		s3bWord(t, combined, header+4, 15)
		s3bWord(t, combined, header+20, ^uint32(0))
	}
	s3bWord(t, combined, 8, 2)
	s3bWord(t, combined, 40, 18)
	s3bWord(t, planet, 0, 16)
	if len(combined) != 34*32 || len(planet) != 17*32 {
		t.Fatal("lookup shader table lengths changed")
	}
	for _, query := range []struct {
		planet bool
		group  uint32
		key    [4]int32
		want   int32
	}{
		{false, 7, [4]int32{-1, 2, -3}, 2},
		{false, 7, [4]int32{31, 2, -3}, 3}, // Same initial bucket as -1.
		{false, 11, [4]int32{-5, 0, 7}, 5},
		{true, 13, [4]int32{2, 3, -9, 4}, 5},
		{true, 13, [4]int32{0, 1, -2, -3}, 6},
		{true, 13, [4]int32{0, 1, 30, -3}, 7}, // Same initial bucket as -2.
		{false, 7, [4]int32{99, 2, -3}, -1},
		{true, 99, [4]int32{0, 1, -2, -3}, -1},
	} {
		header := 0
		if query.planet {
			header = 32
		}
		if got := s3tQuery(t, combined, header, false, query.planet, query.group, query.key); got != query.want {
			t.Fatalf("combined lookup planet=%t group=%d key=%v ID=%d, want %d", query.planet, query.group, query.key, got, query.want)
		}
		if query.planet && s3tQuery(t, planet, 0, true, true, query.group, query.key) != query.want {
			t.Fatal("standalone planet lookup disagrees with combined shader lookup")
		}
	}
	if manager.ObjectLookupBuildCount == 0 || manager.ObjectLookupCacheBytes < int64(len(combined)+len(planet)) {
		t.Fatal("first lookup preparation did not build and account retained shader bytes")
	}
}

func TestS3tLookupReuseObservesLiveEncodedMetadataAndVisibleIndices(t *testing.T) {
	manager, scene := s3tFixture()
	combined, planet := s3tPrepare(t, manager, scene)
	oldCombined, oldPlanet := bytes.Clone(combined), bytes.Clone(planet)
	before := manager.ObjectLookupBuildCount
	s3tPrepare(t, manager, scene)
	// These direct writes change no encoded lookup row or its eligibility.
	scene.VisibleObjects[2].Transform.Position = mgl32.Vec3{100, 2, -3}
	scene.VisibleObjects[2].MaterialTable = []core.Material{core.DefaultMaterial()}
	scene.VisibleObjects[2].TerrainChunkSize = 32
	scene.VisibleObjects[1].TerrainChunkCoord = [3]int{44, 55, 66}
	scene.VisibleObjects[1].PlanetTileGroupID = 99
	wrapped := int64(1) << 32
	scene.VisibleObjects[2].TerrainChunkCoord[0] += int(wrapped)
	replacement := *scene.VisibleObjects[2]
	scene.VisibleObjects[2] = &replacement
	other := core.NewScene()
	other.VisibleObjects = scene.VisibleObjects
	combined, planet = s3tPrepare(t, manager, other)
	if manager.ObjectLookupBuildCount != before || !bytes.Equal(combined, oldCombined) || !bytes.Equal(planet, oldPlanet) {
		t.Fatal("idle or lookup-irrelevant live changes rebuilt shader lookup tables")
	}
	// Each encoded scalar and eligibility boundary must invalidate on its own;
	// no scene revision, dirty marker or producer notification accompanies writes.
	dual := scene.VisibleObjects[5]
	for _, change := range []struct {
		name  string
		apply func()
	}{
		{"terrain x", func() { dual.TerrainChunkCoord[0]-- }},
		{"terrain y", func() { dual.TerrainChunkCoord[1]++ }},
		{"terrain z", func() { dual.TerrainChunkCoord[2]++ }},
		{"terrain group", func() { dual.TerrainGroupID++ }},
		{"terrain zero group", func() { dual.TerrainGroupID = 0 }},
		{"terrain group restored", func() { dual.TerrainGroupID = 11 }},
		{"terrain size eligibility", func() { dual.TerrainChunkSize = 0 }},
		{"terrain size restored", func() { dual.TerrainChunkSize = 16 }},
		{"terrain flag", func() { dual.IsTerrainChunk = false }},
		{"terrain flag restored", func() { dual.IsTerrainChunk = true }},
		{"planet face", func() { dual.PlanetTileFace-- }},
		{"planet level", func() { dual.PlanetTileLevel++ }},
		{"planet x", func() { dual.PlanetTileX-- }},
		{"planet y", func() { dual.PlanetTileY++ }},
		{"planet group", func() { dual.PlanetTileGroupID++ }},
		{"planet zero group", func() { dual.PlanetTileGroupID = 0 }},
		{"planet group restored", func() { dual.PlanetTileGroupID = 13 }},
		{"planet flag", func() { dual.IsPlanetTile = false }},
	} {
		t.Run(change.name, func(t *testing.T) {
			before := manager.ObjectLookupBuildCount
			change.apply()
			s3tPrepare(t, manager, scene)
			if manager.ObjectLookupBuildCount <= before {
				t.Fatal("live lookup change reused stale preparation")
			}
		})
	}
	// A previously unrelated row becoming eligible changes the encoded table.
	scene.VisibleObjects[1].IsPlanetTile = true
	combined, _ = s3tPrepare(t, manager, scene)
	if s3tQuery(t, combined, 32, false, true, 99, [4]int32{}) != 1 {
		t.Fatal("newly eligible live row was not inserted")
	}
	scene.VisibleObjects[2], scene.VisibleObjects[3] = scene.VisibleObjects[3], scene.VisibleObjects[2]
	combined, _ = s3tPrepare(t, manager, scene)
	if s3tQuery(t, combined, 0, false, false, 7, [4]int32{-1, 2, -3}) != 3 {
		t.Fatal("visible reorder lost first-match duplicate ObjectID")
	}
	scene.VisibleObjects = scene.VisibleObjects[2:]
	combined, _ = s3tPrepare(t, manager, scene)
	if s3tQuery(t, combined, 0, false, false, 7, [4]int32{-1, 2, -3}) != 1 {
		t.Fatal("removing nil/unrelated prefix did not shift shader ObjectIDs")
	}
	scene.VisibleObjects[1] = nil
	combined, _ = s3tPrepare(t, manager, scene)
	if s3tQuery(t, combined, 0, false, false, 7, [4]int32{-1, 2, -3}) != 2 {
		t.Fatal("nil replacement did not reveal the later duplicate")
	}
}

func TestS3tLookupCacheBudgetBypassShrinkResetAndCanonicalEmptyHeaders(t *testing.T) {
	if DefaultObjectLookupCacheBudgetBytes != int64(4<<20) {
		t.Fatal("default object lookup cache budget must be 4 MiB")
	}
	manager := &GpuBufferManager{ObjectLookupCacheBudgetBytes: DefaultObjectLookupCacheBudgetBytes}
	scene := core.NewScene()
	for index := 0; index < 128; index++ {
		scene.VisibleObjects = append(scene.VisibleObjects, s3tTerrain(3, [3]int{index, 0, -index}))
	}
	s3tPrepare(t, manager, scene)
	if manager.ObjectLookupCacheBytes <= 512 {
		t.Fatal("large scene fixture did not retain measurable lookup ownership")
	}
	// Lower the ceiling while shrinking: historical peak storage cannot remain
	// owned under the new ceiling, and a fitting current result can still reuse.
	manager.ObjectLookupCacheBudgetBytes = 512
	scene.VisibleObjects = scene.VisibleObjects[:1]
	s3tPrepare(t, manager, scene)
	if manager.ObjectLookupCacheBytes == 0 {
		t.Fatal("small lookup cannot reuse within a sufficient reduced budget")
	}
	before := manager.ObjectLookupBuildCount
	s3tPrepare(t, manager, scene)
	if manager.ObjectLookupBuildCount != before {
		t.Fatal("fitting reduced-budget lookup rebuilt on idle preparation")
	}
	for _, budget := range []int64{1, 0, -1} {
		manager.ObjectLookupCacheBudgetBytes = budget
		s3tPrepare(t, manager, scene)
		before := manager.ObjectLookupBuildCount
		s3tPrepare(t, manager, scene)
		if manager.ObjectLookupCacheBytes != 0 || manager.ObjectLookupBuildCount <= before {
			t.Fatalf("disabled/oversized cache retained preparation: budget=%d bytes=%d", budget, manager.ObjectLookupCacheBytes)
		}
	}
	manager.ObjectLookupCacheBudgetBytes = DefaultObjectLookupCacheBudgetBytes
	s3tPrepare(t, manager, scene)
	before = manager.ObjectLookupBuildCount
	s3tPrepare(t, manager, scene)
	if manager.ObjectLookupCacheBytes == 0 || manager.ObjectLookupBuildCount != before {
		t.Fatal("re-enabled cache did not retain/reuse current shader bytes")
	}
	// Only canonical headers fit this budget; old key/table peaks must leave.
	manager.ObjectLookupCacheBudgetBytes = 96
	scene.VisibleObjects = nil
	combined, planet := s3tPrepare(t, manager, scene)
	if len(combined) != 64 || len(planet) != 32 || manager.ObjectLookupCacheBytes != 96 {
		t.Fatal("empty cache did not retain only the fitting canonical headers")
	}
	for _, header := range []int{0, 32} {
		s3bWord(t, combined, header, 0)
		s3bWord(t, combined, header+4, 0)
		s3bWord(t, combined, header+8, 2)
		s3bWord(t, combined, header+20, ^uint32(0))
	}
	s3bWord(t, planet, 0, 0)
	s3bWord(t, planet, 20, ^uint32(0))
	before = manager.ObjectLookupBuildCount
	s3tPrepare(t, manager, nil)
	if manager.ObjectLookupInputVisitsLastPrepare != 0 || manager.ObjectLookupBuildCount != before {
		t.Fatal("nil scene did not reset visits and reuse identical empty headers")
	}
	manager.ObjectLookupCacheBudgetBytes = 95
	s3tPrepare(t, manager, nil)
	if manager.ObjectLookupCacheBytes != 0 {
		t.Fatal("oversized empty headers retained owned cache storage")
	}
}
