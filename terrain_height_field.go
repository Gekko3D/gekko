package gekko

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type TerrainHeightSampleStatus uint8

const (
	TerrainHeightInvalid TerrainHeightSampleStatus = iota
	TerrainHeightOutside
	TerrainHeightNotResident
	TerrainHeightNoSurface
	TerrainHeightPresent
)

type TerrainHeightSample struct {
	Status     TerrainHeightSampleStatus
	Height     float32
	Normal     mgl32.Vec3
	TerrainID  string
	SourceHash string
	Generation uint64
}

type TerrainHeightProbe struct {
	Sample TerrainHeightSample
	Hit    bool
}

type TerrainHeightFieldOptions struct{ MaxResidentTiles int }

type terrainHeightSource struct {
	terrainID, sourceHash, manifestPath string
	tileSize                            int
	spacing                             float64
	entries                             map[content.TerrainChunkCoordDef]content.TerrainChunkEntryDef
}

// TerrainHeightField owns bounded resident copies of manifest-qualified tiles.
// It provides CPU queries only and does not activate live terrain collision.
type TerrainHeightField struct {
	mu               sync.RWMutex
	source           *terrainHeightSource
	resident         map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef
	maxResidentTiles int
	generation       uint64
}

// TerrainHeightSnapshot retains immutable private tile arrays independently of
// later publications. Its lifetime belongs to the caller; the field's capacity
// limit counts current residents, not arrays retained by snapshots.
type TerrainHeightSnapshot struct {
	source     *terrainHeightSource
	resident   map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef
	generation uint64
}

func NewTerrainHeightField(manifest *content.TerrainChunkManifestDef, manifestPath string, options TerrainHeightFieldOptions) (*TerrainHeightField, error) {
	if err := content.ValidateTerrainHeightTileManifest(manifest); err != nil {
		return nil, err
	}
	if strings.TrimSpace(manifestPath) == "" || options.MaxResidentTiles < 0 {
		return nil, fmt.Errorf("invalid terrain height field options or manifest path")
	}
	if options.MaxResidentTiles == 0 {
		options.MaxResidentTiles = 256
	}
	source := &terrainHeightSource{terrainID: manifest.TerrainID, sourceHash: manifest.SourceHash, manifestPath: manifestPath, tileSize: manifest.ChunkSize, spacing: float64(manifest.VoxelResolution), entries: make(map[content.TerrainChunkCoordDef]content.TerrainChunkEntryDef, len(manifest.Entries))}
	for _, entry := range manifest.Entries {
		source.entries[entry.Coord] = entry
	}
	return &TerrainHeightField{source: source, resident: make(map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef), maxResidentTiles: options.MaxResidentTiles}, nil
}

// PublishTile validates and copies all payloads before atomically replacing a
// resident. The caller must not mutate the tile while this method copies it.
func (f *TerrainHeightField) PublishTile(tile *content.TerrainHeightTileDef) error {
	if f == nil || f.source == nil || tile == nil {
		return fmt.Errorf("invalid terrain height field or tile")
	}
	entry, declared := f.source.entries[tile.Coord]
	if !declared {
		return fmt.Errorf("undeclared terrain height tile")
	}
	if err := content.ValidateTerrainHeightTileReference(entry, tile); err != nil {
		return err
	}
	private := *tile
	private.HeightSamples = append([]uint16(nil), tile.HeightSamples...)
	private.SurfaceMask = append([]byte(nil), tile.SurfaceMask...)
	private.OutdoorNavExclusionMask = append([]byte(nil), tile.OutdoorNavExclusionMask...)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resident[tile.Coord] == nil && len(f.resident) >= f.maxResidentTiles {
		return fmt.Errorf("terrain height field resident capacity exceeded")
	}
	f.resident[private.Coord] = &private
	f.generation++
	return nil
}

func (f *TerrainHeightField) LoadTile(coord content.TerrainChunkCoordDef) error {
	if f == nil || f.source == nil {
		return fmt.Errorf("invalid terrain height field")
	}
	entry, declared := f.source.entries[coord]
	if !declared {
		return fmt.Errorf("undeclared terrain height tile")
	}
	tile, err := content.LoadTerrainHeightTileEntry(entry, f.source.manifestPath)
	if err != nil {
		return err
	}
	return f.PublishTile(tile)
}

func (f *TerrainHeightField) RemoveTile(coord content.TerrainChunkCoordDef) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resident[coord] == nil {
		return false
	}
	delete(f.resident, coord)
	f.generation++
	return true
}

func (f *TerrainHeightField) ResidentTileCount() int {
	if f == nil {
		return 0
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.resident)
}

func (f *TerrainHeightField) Snapshot() *TerrainHeightSnapshot {
	if f == nil || f.source == nil {
		return nil
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	resident := make(map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef, len(f.resident))
	for coord, tile := range f.resident {
		resident[coord] = tile
	}
	return &TerrainHeightSnapshot{source: f.source, resident: resident, generation: f.generation}
}

// SampleGroundXZ fails closed when a normalized coordinate has magnitude at
// least 2^52, where float64 cannot reliably retain the half-cell stencil offset.
func (f *TerrainHeightField) SampleGroundXZ(x, z float32) TerrainHeightSample {
	if f == nil || f.source == nil {
		return TerrainHeightSample{Status: TerrainHeightInvalid}
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return sampleTerrainHeightGround(f.source, f.resident, f.generation, x, z)
}

func (s *TerrainHeightSnapshot) SampleGroundXZ(x, z float32) TerrainHeightSample {
	if s == nil || s.source == nil {
		return TerrainHeightSample{Status: TerrainHeightInvalid}
	}
	return sampleTerrainHeightGround(s.source, s.resident, s.generation, x, z)
}

func (f *TerrainHeightField) ProbeGroundXZ(x, z, minY, maxY float32) TerrainHeightProbe {
	return terrainHeightProbe(f.SampleGroundXZ(x, z), minY, maxY)
}

func (s *TerrainHeightSnapshot) ProbeGroundXZ(x, z, minY, maxY float32) TerrainHeightProbe {
	return terrainHeightProbe(s.SampleGroundXZ(x, z), minY, maxY)
}

func terrainHeightProbe(sample TerrainHeightSample, minY, maxY float32) TerrainHeightProbe {
	if !isFiniteFloat32(minY) || !isFiniteFloat32(maxY) || minY > maxY {
		sample.Status, sample.Height, sample.Normal = TerrainHeightInvalid, 0, mgl32.Vec3{}
	}
	return TerrainHeightProbe{Sample: sample, Hit: sample.Status == TerrainHeightPresent && minY <= sample.Height && sample.Height <= maxY}
}

func terrainHeightCellAddress(cell, size int) (int, int) {
	q, r := cell/size, cell%size
	if r < 0 {
		q--
		r += size
	}
	return q, r
}

func sampleTerrainHeightGround(source *terrainHeightSource, resident map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef, generation uint64, x, z float32) TerrainHeightSample {
	result := TerrainHeightSample{Status: TerrainHeightInvalid, TerrainID: source.terrainID, SourceHash: source.sourceHash, Generation: generation}
	if !isFiniteFloat32(x) || !isFiniteFloat32(z) {
		return result
	}
	quotientX, quotientZ := float64(x)/source.spacing, float64(z)/source.spacing
	if math.Abs(quotientX) >= 1<<52 || math.Abs(quotientZ) >= 1<<52 {
		return result
	}
	cellX, cellZ := quotientX-.5, quotientZ-.5
	floorX, floorZ := math.Floor(cellX), math.Floor(cellZ)
	limit := math.Ldexp(1, strconv.IntSize-1)
	if math.IsNaN(floorX) || math.IsNaN(floorZ) || floorX < -limit || floorX >= limit || floorZ < -limit || floorZ >= limit {
		return result
	}
	ix, iz := int(floorX), int(floorZ)
	maxInt := int(^uint(0) >> 1)
	if ix == maxInt || iz == maxInt {
		return result
	}
	tx, tz := cellX-floorX, cellZ-floorZ
	weights := [4]float64{(1 - tx) * (1 - tz), tx * (1 - tz), (1 - tx) * tz, tx * tz}
	dx := [4]float64{-(1 - tz), 1 - tz, -tz, tz}
	dz := [4]float64{-(1 - tx), -tx, 1 - tx, tx}
	var heights [4]float64
	status := TerrainHeightPresent
	for i := 0; i < 4; i++ {
		if weights[i] == 0 && dx[i] == 0 && dz[i] == 0 {
			continue
		}
		sx, lx := terrainHeightCellAddress(ix+i%2, source.tileSize)
		sz, lz := terrainHeightCellAddress(iz+i/2, source.tileSize)
		coord := content.TerrainChunkCoordDef{X: sx, Z: sz}
		if _, declared := source.entries[coord]; !declared {
			status = TerrainHeightOutside
			continue
		}
		tile := resident[coord]
		if tile == nil {
			if status > TerrainHeightNotResident {
				status = TerrainHeightNotResident
			}
			continue
		}
		index := lz*source.tileSize + lx
		if len(tile.SurfaceMask) != 0 && tile.SurfaceMask[index/8]&(1<<uint(index%8)) == 0 {
			if status > TerrainHeightNoSurface {
				status = TerrainHeightNoSurface
			}
			continue
		}
		heights[i] = float64(tile.HeightOffset) + float64(tile.HeightSamples[index])/65535*float64(tile.HeightScale)
	}
	result.Status = status
	if status != TerrainHeightPresent {
		return result
	}
	var height, gradientX, gradientZ float64
	for i := 0; i < 4; i++ {
		height += weights[i] * heights[i]
		gradientX += dx[i] * heights[i]
		gradientZ += dz[i] * heights[i]
	}
	gradientX /= source.spacing
	gradientZ /= source.spacing
	if math.IsNaN(height) || math.IsInf(height, 0) || !isFiniteFloat32(float32(height)) || math.IsNaN(gradientX) || math.IsInf(gradientX, 0) || math.IsNaN(gradientZ) || math.IsInf(gradientZ, 0) {
		result.Status = TerrainHeightInvalid
		return result
	}
	scale := math.Max(1, math.Max(math.Abs(gradientX), math.Abs(gradientZ)))
	nx, ny, nz := -gradientX/scale, 1/scale, -gradientZ/scale
	length := math.Sqrt(nx*nx + ny*ny + nz*nz)
	result.Height = float32(height)
	result.Normal = mgl32.Vec3{float32(nx / length), float32(ny / length), float32(nz / length)}
	return result
}
