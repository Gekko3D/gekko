package gpu

import (
	"math"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

const DefaultObjectLookupCacheBudgetBytes int64 = 4 << 20

// Only encoded scalar inputs and bytes belong to this owner. No scene or live
// object reference survives preparation.
type objectLookupCacheOwner struct {
	terrain    []terrainChunkLookupEntry
	planet     []planetTileLookupEntry
	combined   []byte
	standalone []byte
	valid      bool
}

func (owner *objectLookupCacheOwner) charge() int64 {
	var bytes int64
	for _, storage := range []struct {
		capacity int
		size     int64
	}{
		{cap(owner.terrain), int64(unsafe.Sizeof(terrainChunkLookupEntry{}))},
		{cap(owner.planet), int64(unsafe.Sizeof(planetTileLookupEntry{}))},
		{cap(owner.combined), 1},
		{cap(owner.standalone), 1},
	} {
		if int64(storage.capacity) > (math.MaxInt64-bytes)/storage.size {
			return math.MaxInt64
		}
		bytes += int64(storage.capacity) * storage.size
	}
	return bytes
}

// Returned views are read-only and borrowed until the next preparation/reset.
// Capture and compare in one live pass, reusing scalar storage on idle frames.
func (m *GpuBufferManager) prepareObjectLookupBytes(scene *core.Scene) ([]byte, []byte) {
	owner := &m.objectLookupCache
	budget := m.ObjectLookupCacheBudgetBytes
	if budget <= 0 || owner.charge() > budget {
		*owner = objectLookupCacheOwner{}
	}
	m.ObjectLookupInputVisitsLastPrepare = 0
	changed := !owner.valid
	terrainCount, planetCount := 0, 0
	if scene != nil {
		for objectID, object := range scene.VisibleObjects {
			m.ObjectLookupInputVisitsLastPrepare++
			if object == nil {
				continue
			}
			if object.IsTerrainChunk && object.TerrainGroupID != 0 && object.TerrainChunkSize > 0 {
				entry := terrainChunkLookupEntry{
					ChunkCoord:     [3]int32{int32(object.TerrainChunkCoord[0]), int32(object.TerrainChunkCoord[1]), int32(object.TerrainChunkCoord[2])},
					TerrainGroupID: object.TerrainGroupID, ObjectID: int32(objectID),
				}
				if terrainCount == len(owner.terrain) {
					owner.terrain = append(owner.terrain, entry)
					changed = true
				} else {
					changed = changed || owner.terrain[terrainCount] != entry
					owner.terrain[terrainCount] = entry
				}
				terrainCount++
			}
			if object.IsPlanetTile && object.PlanetTileGroupID != 0 {
				entry := planetTileLookupEntry{
					PlanetTile:    [4]int32{int32(object.PlanetTileFace), int32(object.PlanetTileLevel), int32(object.PlanetTileX), int32(object.PlanetTileY)},
					PlanetGroupID: object.PlanetTileGroupID, ObjectID: int32(objectID),
				}
				if planetCount == len(owner.planet) {
					owner.planet = append(owner.planet, entry)
					changed = true
				} else {
					changed = changed || owner.planet[planetCount] != entry
					owner.planet[planetCount] = entry
				}
				planetCount++
			}
		}
	}
	changed = changed || terrainCount != len(owner.terrain) || planetCount != len(owner.planet)
	owner.terrain, owner.planet = owner.terrain[:terrainCount], owner.planet[:planetCount]
	if changed {
		terrainEntries, terrainParams := packTerrainChunkLookup(owner.terrain)
		planetEntries, planetParams := packPlanetTileLookup(owner.planet)
		owner.combined = serializeCombinedObjectLookupBuffer(terrainEntries, terrainParams, planetEntries, planetParams)
		owner.standalone = serializePlanetTileLookupBuffer(planetEntries, planetParams)
		m.ObjectLookupBuildCount++
	}
	combined, standalone := owner.combined, owner.standalone
	bytes := owner.charge()
	if budget <= 0 || bytes > budget {
		*owner = objectLookupCacheOwner{}
		bytes = 0
	} else {
		owner.valid = true
	}
	m.ObjectLookupCacheBytes = bytes
	return combined, standalone
}
