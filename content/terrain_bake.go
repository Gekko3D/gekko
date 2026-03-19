package content

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"sort"
)

func TerrainChunkCoords(def *TerrainSourceDef) []TerrainChunkCoordDef {
	if def == nil || def.ChunkSize <= 0 || def.VoxelResolution <= 0 || def.WorldSize[0] <= 0 || def.WorldSize[1] <= 0 {
		return nil
	}
	chunkWorldSize := float64(float32(def.ChunkSize) * def.VoxelResolution)
	if chunkWorldSize <= 0 {
		return nil
	}
	minChunkX, maxChunkX := terrainChunkCoordRange(def.WorldSize[0], chunkWorldSize)
	minChunkZ, maxChunkZ := terrainChunkCoordRange(def.WorldSize[1], chunkWorldSize)
	chunksX := maxChunkX - minChunkX + 1
	chunksZ := maxChunkZ - minChunkZ + 1
	coords := make([]TerrainChunkCoordDef, 0, chunksX*chunksZ)
	for z := minChunkZ; z <= maxChunkZ; z++ {
		for x := minChunkX; x <= maxChunkX; x++ {
			coords = append(coords, TerrainChunkCoordDef{X: x, Y: 0, Z: z})
		}
	}
	return coords
}

func TerrainBakeSourceHash(def *TerrainSourceDef) string {
	if def == nil {
		return ""
	}
	h := sha256.New()
	writeStringHash(h, def.ID)
	writeIntHash(h, def.SampleWidth)
	writeIntHash(h, def.SampleHeight)
	writeFloat32Hash(h, def.WorldSize[0])
	writeFloat32Hash(h, def.WorldSize[1])
	writeFloat32Hash(h, def.HeightScale)
	writeFloat32Hash(h, def.VoxelResolution)
	writeIntHash(h, def.ChunkSize)
	for _, sample := range def.HeightSamples {
		var buf [2]byte
		binary.LittleEndian.PutUint16(buf[:], sample)
		_, _ = h.Write(buf[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func DefaultTerrainManifestPath(terrainPath string) string {
	base := trimTerrainExtension(filepath.Base(terrainPath))
	if base == "" {
		base = "terrain"
	}
	return filepath.Join(filepath.Dir(terrainPath), base+".gkterrainmanifest")
}

func DefaultTerrainChunkDir(manifestPath string) string {
	base := trimTerrainManifestExtension(filepath.Base(manifestPath))
	if base == "" {
		base = "terrain"
	}
	return filepath.Join(filepath.Dir(manifestPath), base+"_chunks")
}

func BakeTerrainChunks(def *TerrainSourceDef, manifestPath string, coords []TerrainChunkCoordDef) (*TerrainChunkManifestDef, map[string]*TerrainChunkDef, error) {
	if def == nil {
		return nil, nil, fmt.Errorf("terrain definition is nil")
	}
	EnsureTerrainSourceDefaults(def)
	if manifestPath == "" {
		return nil, nil, fmt.Errorf("manifest path is empty")
	}
	sourceHash := TerrainBakeSourceHash(def)
	chunkDir := DefaultTerrainChunkDir(manifestPath)
	chunks := make(map[string]*TerrainChunkDef, len(coords))
	entries := make([]TerrainChunkEntryDef, 0, len(coords))
	sorted := append([]TerrainChunkCoordDef(nil), coords...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Z != sorted[j].Z {
			return sorted[i].Z < sorted[j].Z
		}
		if sorted[i].Y != sorted[j].Y {
			return sorted[i].Y < sorted[j].Y
		}
		return sorted[i].X < sorted[j].X
	})
	for _, coord := range sorted {
		chunk := bakeTerrainChunk(def, coord, sourceHash)
		chunkPath := filepath.Join(chunkDir, terrainChunkFileName(coord))
		entry := TerrainChunkEntryDef{
			Coord:              coord,
			ChunkSize:          def.ChunkSize,
			VoxelResolution:    def.VoxelResolution,
			TerrainID:          def.ID,
			SourceHash:         sourceHash,
			ChunkPath:          authorTerrainChunkPath(chunkPath, manifestPath),
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
		}
		key := TerrainChunkKey(coord)
		chunks[key] = chunk
		entries = append(entries, entry)
	}
	manifest := &TerrainChunkManifestDef{
		SchemaVersion:   CurrentTerrainChunkManifestSchemaVersion,
		TerrainID:       def.ID,
		SourceHash:      sourceHash,
		ChunkSize:       def.ChunkSize,
		VoxelResolution: def.VoxelResolution,
		Entries:         entries,
	}
	return manifest, chunks, nil
}

func BakeTerrainChunkSet(def *TerrainSourceDef, manifestPath string) (*TerrainChunkManifestDef, map[string]*TerrainChunkDef, error) {
	return BakeTerrainChunks(def, manifestPath, TerrainChunkCoords(def))
}

func ResolveTerrainChunkPath(entry TerrainChunkEntryDef, manifestPath string) string {
	return ResolveDocumentPath(entry.ChunkPath, manifestPath)
}

func authorTerrainChunkPath(chunkPath string, manifestPath string) string {
	if manifestPath == "" {
		return filepath.Clean(chunkPath)
	}
	rel, err := filepath.Rel(filepath.Dir(manifestPath), chunkPath)
	if err != nil {
		return filepath.Clean(chunkPath)
	}
	return filepath.Clean(rel)
}

func trimTerrainExtension(base string) string {
	return trimKnownSuffix(base, ".gkterrain")
}

func trimTerrainManifestExtension(base string) string {
	return trimKnownSuffix(base, ".gkterrainmanifest")
}

func trimKnownSuffix(base string, suffix string) string {
	if len(base) >= len(suffix) && base[len(base)-len(suffix):] == suffix {
		return base[:len(base)-len(suffix)]
	}
	return base
}

func terrainChunkFileName(coord TerrainChunkCoordDef) string {
	return fmt.Sprintf("%d_%d_%d.gkchunk", coord.X, coord.Y, coord.Z)
}

func terrainChunkCoordRange(worldSize float32, chunkWorldSize float64) (int, int) {
	if worldSize <= 0 || chunkWorldSize <= 0 {
		return 0, 0
	}
	half := float64(worldSize) * 0.5
	maxChunk := int(math.Ceil(half/chunkWorldSize)) - 1
	minChunk := -int(math.Ceil(half / chunkWorldSize))
	return minChunk, maxChunk
}

func bakeTerrainChunk(def *TerrainSourceDef, coord TerrainChunkCoordDef, sourceHash string) *TerrainChunkDef {
	chunk := &TerrainChunkDef{
		SchemaVersion:   CurrentTerrainChunkSchemaVersion,
		TerrainID:       def.ID,
		SourceHash:      sourceHash,
		Coord:           coord,
		ChunkSize:       def.ChunkSize,
		VoxelResolution: def.VoxelResolution,
		SolidValue:      DefaultTerrainChunkSolidValue,
	}

	chunkWorldSize := float32(def.ChunkSize) * def.VoxelResolution
	startWorldX := float32(coord.X) * chunkWorldSize
	startWorldZ := float32(coord.Z) * chunkWorldSize
	halfX := def.WorldSize[0] * 0.5
	halfZ := def.WorldSize[1] * 0.5
	for localZ := 0; localZ < def.ChunkSize; localZ++ {
		for localX := 0; localX < def.ChunkSize; localX++ {
			worldX := startWorldX + (float32(localX)+0.5)*def.VoxelResolution
			worldZ := startWorldZ + (float32(localZ)+0.5)*def.VoxelResolution
			if worldX < -halfX || worldZ < -halfZ || worldX >= halfX || worldZ >= halfZ {
				continue
			}
			height := sampleTerrainHeight(def, worldX, worldZ)
			filled := terrainFilledVoxels(height, def.VoxelResolution)
			if filled <= 0 {
				continue
			}
			chunk.Columns = append(chunk.Columns, TerrainChunkColumnDef{
				X:            localX,
				Z:            localZ,
				FilledVoxels: filled,
			})
			chunk.NonEmptyVoxelCount += filled
		}
	}
	return chunk
}

func terrainFilledVoxels(height float32, voxelResolution float32) int {
	if voxelResolution <= 0 || height < 0 {
		return 0
	}
	return int(math.Floor(float64(height/voxelResolution))) + 1
}

func sampleTerrainHeight(def *TerrainSourceDef, worldX, worldZ float32) float32 {
	if def == nil || def.SampleWidth <= 0 || def.SampleHeight <= 0 || len(def.HeightSamples) == 0 {
		return 0
	}
	if def.SampleWidth == 1 && def.SampleHeight == 1 {
		return terrainSampleHeight(def, 0, 0)
	}
	maxX := maxFloat32(def.WorldSize[0], 1)
	maxZ := maxFloat32(def.WorldSize[1], 1)
	nx := clampFloat32((worldX+maxX*0.5)/maxX, 0, 1)
	nz := clampFloat32((worldZ+maxZ*0.5)/maxZ, 0, 1)
	sx := nx * float32(def.SampleWidth-1)
	sz := nz * float32(def.SampleHeight-1)
	x0 := int(math.Floor(float64(sx)))
	z0 := int(math.Floor(float64(sz)))
	x1 := minInt(x0+1, def.SampleWidth-1)
	z1 := minInt(z0+1, def.SampleHeight-1)
	tx := sx - float32(x0)
	tz := sz - float32(z0)
	h00 := terrainSampleHeight(def, x0, z0)
	h10 := terrainSampleHeight(def, x1, z0)
	h01 := terrainSampleHeight(def, x0, z1)
	h11 := terrainSampleHeight(def, x1, z1)
	h0 := lerpFloat32(h00, h10, tx)
	h1 := lerpFloat32(h01, h11, tx)
	return lerpFloat32(h0, h1, tz)
}

func terrainSampleHeight(def *TerrainSourceDef, x, z int) float32 {
	if def == nil || x < 0 || z < 0 || x >= def.SampleWidth || z >= def.SampleHeight {
		return 0
	}
	return float32(def.HeightSamples[z*def.SampleWidth+x]) / 65535.0 * def.HeightScale
}

func writeStringHash(h interface{ Write([]byte) (int, error) }, v string) {
	_, _ = h.Write([]byte(v))
	_, _ = h.Write([]byte{0})
}

func writeIntHash(h interface{ Write([]byte) (int, error) }, v int) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	_, _ = h.Write(buf[:])
}

func writeFloat32Hash(h interface{ Write([]byte) (int, error) }, v float32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
	_, _ = h.Write(buf[:])
}

func clampFloat32(v, minV, maxV float32) float32 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func lerpFloat32(a, b, t float32) float32 {
	return a + (b-a)*t
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
