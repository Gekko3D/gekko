package gpu

import (
	"encoding/binary"

	"github.com/gekko3d/gekko/voxelrt/rt/core"

	"github.com/cogentcore/webgpu/wgpu"
)

const (
	planetTileLookupEntrySize = 32
)

type planetTileLookupEntry struct {
	PlanetTile    [4]int32
	PlanetGroupID uint32
	ObjectID      int32
}

type planetTileLookupParams struct {
	GridSize uint32
	GridMask uint32
}

func (m *GpuBufferManager) updatePlanetTileLookup(scene *core.Scene) bool {
	entries, params := buildPlanetTileLookup(scene)
	entryBytes := serializePlanetTileLookupBuffer(entries, params)
	return m.ensureBuffer("PlanetTileLookupBuf", &m.PlanetTileLookupBuf, entryBytes, wgpu.BufferUsageStorage, 0)
}

func buildPlanetTileLookup(scene *core.Scene) ([]planetTileLookupEntry, planetTileLookupParams) {
	planetEntries := make([]planetTileLookupEntry, 0)
	if scene != nil {
		for objectID, obj := range scene.VisibleObjects {
			if obj == nil || !obj.IsPlanetTile || obj.PlanetTileGroupID == 0 {
				continue
			}
			planetEntries = append(planetEntries, planetTileLookupEntry{
				PlanetTile: [4]int32{
					int32(obj.PlanetTileFace),
					int32(obj.PlanetTileLevel),
					int32(obj.PlanetTileX),
					int32(obj.PlanetTileY),
				},
				PlanetGroupID: obj.PlanetTileGroupID,
				ObjectID:      int32(objectID),
			})
		}
	}

	if len(planetEntries) == 0 {
		return nil, planetTileLookupParams{}
	}

	gridSize := 1
	for gridSize < len(planetEntries)*4 {
		gridSize <<= 1
	}
	lookup := make([]planetTileLookupEntry, gridSize)
	for i := range lookup {
		lookup[i].ObjectID = -1
	}

	mask := uint32(gridSize - 1)
	for _, entry := range planetEntries {
		hash := planetTileLookupHash(entry.PlanetTile[0], entry.PlanetTile[1], entry.PlanetTile[2], entry.PlanetTile[3], entry.PlanetGroupID) & mask
		for probe := 0; probe < gridSize; probe++ {
			idx := int((hash + uint32(probe)) & mask)
			if lookup[idx].ObjectID == -1 {
				lookup[idx] = entry
				break
			}
		}
	}

	return lookup, planetTileLookupParams{
		GridSize: uint32(gridSize),
		GridMask: mask,
	}
}

func planetTileLookupHash(face, level, x, y int32, planetGroupID uint32) uint32 {
	return uint32(face)*2654435761 ^ uint32(level)*2246822519 ^ uint32(x)*3266489917 ^ uint32(y)*668265263 ^ planetGroupID*1640531513
}

func findPlanetTileLookupObjectID(entries []planetTileLookupEntry, params planetTileLookupParams, planetGroupID uint32, tile [4]int32) int32 {
	if params.GridSize == 0 || len(entries) == 0 {
		return -1
	}
	mask := params.GridMask
	hash := planetTileLookupHash(tile[0], tile[1], tile[2], tile[3], planetGroupID) & mask
	for probe := uint32(0); probe < params.GridSize; probe++ {
		idx := (hash + probe) & mask
		entry := entries[idx]
		if entry.ObjectID == -1 {
			return -1
		}
		if entry.PlanetGroupID == planetGroupID && entry.PlanetTile == tile {
			return entry.ObjectID
		}
	}
	return -1
}

func serializePlanetTileLookupBuffer(entries []planetTileLookupEntry, params planetTileLookupParams) []byte {
	buf := make([]byte, (len(entries)+1)*planetTileLookupEntrySize)
	binary.LittleEndian.PutUint32(buf[0:4], params.GridSize)
	binary.LittleEndian.PutUint32(buf[4:8], params.GridMask)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(^uint32(0)))

	for i, entry := range entries {
		offset := (i + 1) * planetTileLookupEntrySize
		binary.LittleEndian.PutUint32(buf[offset+0:offset+4], uint32(entry.PlanetTile[0]))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(entry.PlanetTile[1]))
		binary.LittleEndian.PutUint32(buf[offset+8:offset+12], uint32(entry.PlanetTile[2]))
		binary.LittleEndian.PutUint32(buf[offset+12:offset+16], uint32(entry.PlanetTile[3]))
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], entry.PlanetGroupID)
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], uint32(entry.ObjectID))
	}
	return buf
}
