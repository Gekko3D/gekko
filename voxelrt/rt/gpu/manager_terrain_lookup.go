package gpu

import (
	"encoding/binary"

	"github.com/gekko3d/gekko/voxelrt/rt/core"

	"github.com/cogentcore/webgpu/wgpu"
)

const (
	terrainChunkLookupEntrySize = 32
)

type terrainChunkLookupEntry struct {
	ChunkCoord     [3]int32
	TerrainGroupID uint32
	ObjectID       int32
}

type terrainChunkLookupParams struct {
	GridSize uint32
	GridMask uint32
}

func (m *GpuBufferManager) updateTerrainChunkLookup(scene *core.Scene) bool {
	terrainEntries, terrainParams := buildTerrainChunkLookup(scene)
	planetEntries, planetParams := buildPlanetTileLookup(scene)
	entryBytes := serializeCombinedObjectLookupBuffer(terrainEntries, terrainParams, planetEntries, planetParams)

	recreated := false
	if m.ensureBuffer("TerrainChunkLookupBuf", &m.TerrainChunkLookupBuf, entryBytes, wgpu.BufferUsageStorage, 0) {
		recreated = true
	}
	return recreated
}

func buildTerrainChunkLookup(scene *core.Scene) ([]terrainChunkLookupEntry, terrainChunkLookupParams) {
	terrainEntries := make([]terrainChunkLookupEntry, 0)
	if scene != nil {
		for objectID, obj := range scene.VisibleObjects {
			if obj == nil || !obj.IsTerrainChunk || obj.TerrainGroupID == 0 || obj.TerrainChunkSize <= 0 {
				continue
			}
			terrainEntries = append(terrainEntries, terrainChunkLookupEntry{
				ChunkCoord: [3]int32{
					int32(obj.TerrainChunkCoord[0]),
					int32(obj.TerrainChunkCoord[1]),
					int32(obj.TerrainChunkCoord[2]),
				},
				TerrainGroupID: obj.TerrainGroupID,
				ObjectID:       int32(objectID),
			})
		}
	}

	if len(terrainEntries) == 0 {
		return nil, terrainChunkLookupParams{}
	}

	gridSize := 1
	for gridSize < len(terrainEntries)*4 {
		gridSize <<= 1
	}
	lookup := make([]terrainChunkLookupEntry, gridSize)
	for i := range lookup {
		lookup[i].ObjectID = -1
	}

	mask := uint32(gridSize - 1)
	for _, entry := range terrainEntries {
		hash := terrainChunkLookupHash(entry.ChunkCoord[0], entry.ChunkCoord[1], entry.ChunkCoord[2], entry.TerrainGroupID) & mask
		for probe := 0; probe < gridSize; probe++ {
			idx := int((hash + uint32(probe)) & mask)
			if lookup[idx].ObjectID == -1 {
				lookup[idx] = entry
				break
			}
		}
	}

	return lookup, terrainChunkLookupParams{
		GridSize: uint32(gridSize),
		GridMask: mask,
	}
}

func terrainChunkLookupHash(x, y, z int32, terrainGroupID uint32) uint32 {
	return uint32(x)*73856093 ^ uint32(y)*19349663 ^ uint32(z)*83492791 ^ terrainGroupID*1640531513
}

func findTerrainChunkLookupObjectID(entries []terrainChunkLookupEntry, params terrainChunkLookupParams, terrainGroupID uint32, coord [3]int32) int32 {
	if params.GridSize == 0 || len(entries) == 0 {
		return -1
	}
	mask := params.GridMask
	hash := terrainChunkLookupHash(coord[0], coord[1], coord[2], terrainGroupID) & mask
	for probe := uint32(0); probe < params.GridSize; probe++ {
		idx := (hash + probe) & mask
		entry := entries[idx]
		if entry.ObjectID == -1 {
			return -1
		}
		if entry.TerrainGroupID == terrainGroupID && entry.ChunkCoord == coord {
			return entry.ObjectID
		}
	}
	return -1
}

func serializeTerrainChunkLookupBuffer(entries []terrainChunkLookupEntry, params terrainChunkLookupParams) []byte {
	buf := make([]byte, (len(entries)+1)*terrainChunkLookupEntrySize)
	binary.LittleEndian.PutUint32(buf[0:4], params.GridSize)
	binary.LittleEndian.PutUint32(buf[4:8], params.GridMask)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(^uint32(0)))

	for i, entry := range entries {
		offset := (i + 1) * terrainChunkLookupEntrySize
		binary.LittleEndian.PutUint32(buf[offset+0:offset+4], uint32(entry.ChunkCoord[0]))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(entry.ChunkCoord[1]))
		binary.LittleEndian.PutUint32(buf[offset+8:offset+12], uint32(entry.ChunkCoord[2]))
		binary.LittleEndian.PutUint32(buf[offset+12:offset+16], 0)
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], entry.TerrainGroupID)
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], uint32(entry.ObjectID))
	}
	return buf
}

func serializeCombinedObjectLookupBuffer(terrainEntries []terrainChunkLookupEntry, terrainParams terrainChunkLookupParams, planetEntries []planetTileLookupEntry, planetParams planetTileLookupParams) []byte {
	const headerCount = 2
	totalEntries := headerCount + len(terrainEntries) + len(planetEntries)
	buf := make([]byte, totalEntries*terrainChunkLookupEntrySize)

	terrainStart := uint32(headerCount)
	planetStart := terrainStart + uint32(len(terrainEntries))

	binary.LittleEndian.PutUint32(buf[0:4], terrainParams.GridSize)
	binary.LittleEndian.PutUint32(buf[4:8], terrainParams.GridMask)
	binary.LittleEndian.PutUint32(buf[8:12], terrainStart)
	binary.LittleEndian.PutUint32(buf[20:24], uint32(^uint32(0)))

	binary.LittleEndian.PutUint32(buf[terrainChunkLookupEntrySize+0:terrainChunkLookupEntrySize+4], planetParams.GridSize)
	binary.LittleEndian.PutUint32(buf[terrainChunkLookupEntrySize+4:terrainChunkLookupEntrySize+8], planetParams.GridMask)
	binary.LittleEndian.PutUint32(buf[terrainChunkLookupEntrySize+8:terrainChunkLookupEntrySize+12], planetStart)
	binary.LittleEndian.PutUint32(buf[terrainChunkLookupEntrySize+20:terrainChunkLookupEntrySize+24], uint32(^uint32(0)))

	for i, entry := range terrainEntries {
		offset := (headerCount + i) * terrainChunkLookupEntrySize
		binary.LittleEndian.PutUint32(buf[offset+0:offset+4], uint32(entry.ChunkCoord[0]))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(entry.ChunkCoord[1]))
		binary.LittleEndian.PutUint32(buf[offset+8:offset+12], uint32(entry.ChunkCoord[2]))
		binary.LittleEndian.PutUint32(buf[offset+12:offset+16], 0)
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], entry.TerrainGroupID)
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], uint32(entry.ObjectID))
	}

	for i, entry := range planetEntries {
		offset := int(planetStart)*terrainChunkLookupEntrySize + i*terrainChunkLookupEntrySize
		binary.LittleEndian.PutUint32(buf[offset+0:offset+4], uint32(entry.PlanetTile[0]))
		binary.LittleEndian.PutUint32(buf[offset+4:offset+8], uint32(entry.PlanetTile[1]))
		binary.LittleEndian.PutUint32(buf[offset+8:offset+12], uint32(entry.PlanetTile[2]))
		binary.LittleEndian.PutUint32(buf[offset+12:offset+16], uint32(entry.PlanetTile[3]))
		binary.LittleEndian.PutUint32(buf[offset+16:offset+20], entry.PlanetGroupID)
		binary.LittleEndian.PutUint32(buf[offset+20:offset+24], uint32(entry.ObjectID))
	}

	return buf
}
