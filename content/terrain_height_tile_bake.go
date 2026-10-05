package content

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

type TerrainHeightTileBakeOptions struct {
	TileSize      int
	SampleSpacing float32
	MaxTiles      int
}

func validateTerrainHeightTileSource(d *TerrainSourceDef) error {
	if d == nil || strings.TrimSpace(d.ID) == "" || d.SchemaVersion != CurrentTerrainSchemaVersion || d.Kind != TerrainKindHeightfield {
		return fmt.Errorf("invalid terrain height tile source identity")
	}
	if d.SampleWidth <= 0 || d.SampleHeight <= 0 || d.SampleWidth > len(d.HeightSamples)/d.SampleHeight || d.SampleWidth*d.SampleHeight != len(d.HeightSamples) {
		return fmt.Errorf("invalid terrain height tile source dimensions or samples")
	}
	if !terrainFinite(d.WorldSize[0]) || !terrainFinite(d.WorldSize[1]) || d.WorldSize[0] <= 0 || d.WorldSize[1] <= 0 || !terrainFinite(d.HeightScale) || d.HeightScale <= 0 || !terrainFinite(d.VoxelResolution) || d.VoxelResolution <= 0 || d.ChunkSize <= 0 {
		return fmt.Errorf("invalid terrain height tile source extent or scale")
	}
	return nil
}

// BakeTerrainHeightTiles resamples an immutable legacy source onto a signed
// cell-centered tile lattice. It returns content without writing any files.
func BakeTerrainHeightTiles(source *TerrainSourceDef, manifestPath string, options TerrainHeightTileBakeOptions) (*TerrainChunkManifestDef, map[string]*TerrainHeightTileDef, error) {
	if err := validateTerrainHeightTileSource(source); err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(manifestPath) == "" {
		return nil, nil, fmt.Errorf("terrain height tile manifest path is empty")
	}
	if options.TileSize == 0 {
		options.TileSize = 128
	}
	if options.SampleSpacing == 0 {
		options.SampleSpacing = 2
	}
	if options.MaxTiles == 0 {
		options.MaxTiles = 4096
	}
	if options.TileSize < 1 || options.TileSize > 128 || !terrainFinite(options.SampleSpacing) || options.SampleSpacing <= 0 || options.MaxTiles <= 0 {
		return nil, nil, fmt.Errorf("invalid terrain height tile bake options")
	}
	span := float64(options.TileSize) * float64(options.SampleSpacing)
	if !terrainFinite(float32(span)) {
		return nil, nil, fmt.Errorf("terrain height tile span overflows")
	}
	minX := math.Floor(-float64(source.WorldSize[0]) * .5 / span)
	maxX := math.Ceil(float64(source.WorldSize[0])*.5/span) - 1
	minZ := math.Floor(-float64(source.WorldSize[1]) * .5 / span)
	maxZ := math.Ceil(float64(source.WorldSize[1])*.5/span) - 1
	intLimit := math.Ldexp(1, strconv.IntSize-1)
	for _, v := range []float64{minX, maxX, minZ, maxZ} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -intLimit || v >= intLimit {
			return nil, nil, fmt.Errorf("terrain height tile coordinate exceeds integer range")
		}
	}
	nx, nz := maxX-minX+1, maxZ-minZ+1
	if nx < 1 || nz < 1 || nx >= intLimit || nz >= intLimit || nx > float64(options.MaxTiles) || nz > float64(options.MaxTiles)/nx {
		return nil, nil, fmt.Errorf("terrain height tile count exceeds limit")
	}
	if int(nx) > options.MaxTiles/int(nz) {
		return nil, nil, fmt.Errorf("terrain height tile count exceeds limit")
	}
	count := int(nx) * int(nz)
	sourceHash := TerrainBakeSourceHash(source)
	manifest := &TerrainChunkManifestDef{SchemaVersion: TerrainHeightTileManifestSchemaVersion, TerrainID: source.ID, SourceHash: sourceHash, ChunkSize: options.TileSize, VoxelResolution: options.SampleSpacing, Entries: make([]TerrainChunkEntryDef, 0, count)}
	tiles := make(map[string]*TerrainHeightTileDef, count)
	for zi := 0; zi < int(nz); zi++ {
		z := int(minZ) + zi
		for xi := 0; xi < int(nx); xi++ {
			x := int(minX) + xi
			coord := TerrainChunkCoordDef{X: x, Z: z}
			origin := [3]float32{float32(float64(x) * span), 0, float32(float64(z) * span)}
			n := options.TileSize * options.TileSize
			tile := &TerrainHeightTileDef{SchemaVersion: TerrainHeightTileSchemaVersion, TerrainID: source.ID, SourceHash: sourceHash, Coord: coord, WorldOrigin: origin, SampleWidth: options.TileSize, SampleHeight: options.TileSize, SampleSpacing: options.SampleSpacing, HeightScale: source.HeightScale, HeightSamples: make([]uint16, n), SurfaceMask: make([]byte, (n+7)/8)}
			if err := ValidateTerrainHeightTile(tile); err != nil {
				return nil, nil, err
			}
			valid := 0
			for localZ := 0; localZ < options.TileSize; localZ++ {
				for localX := 0; localX < options.TileSize; localX++ {
					wx := origin[0] + (float32(localX)+.5)*options.SampleSpacing
					wz := origin[2] + (float32(localZ)+.5)*options.SampleSpacing
					if wx < -source.WorldSize[0]*.5 || wx >= source.WorldSize[0]*.5 || wz < -source.WorldSize[1]*.5 || wz >= source.WorldSize[1]*.5 {
						continue
					}
					i := localZ*options.TileSize + localX
					height := sampleTerrainHeight(source, wx, wz)
					encoded := math.Round(float64((height / source.HeightScale) * 65535))
					if math.IsNaN(encoded) || math.IsInf(encoded, 0) || encoded < 0 || encoded > 65535 {
						return nil, nil, fmt.Errorf("terrain height tile sampled height is invalid")
					}
					tile.HeightSamples[i] = uint16(encoded)
					tile.SurfaceMask[i/8] |= 1 << uint(i%8)
					valid++
				}
			}
			if valid == n {
				tile.SurfaceMask = nil
			}
			_, result := terrainHeightTilePayload(tile)
			path := filepath.Join(DefaultTerrainChunkDir(manifestPath), fmt.Sprintf("height_%d_%d.gkchunk", x, z))
			entry := TerrainChunkEntryDef{Coord: coord, WorldOrigin: origin, ChunkSize: options.TileSize, VoxelResolution: options.SampleSpacing, TerrainID: source.ID, SourceHash: sourceHash, ChunkPath: authorTerrainChunkPath(path, manifestPath), PayloadKind: TerrainHeightTilePayloadKind, PayloadHash: result.PayloadHash, PayloadSizeBytes: result.PayloadSizeBytes}
			if err := validateTerrainHeightTileEntry(entry); err != nil {
				return nil, nil, err
			}
			manifest.Entries = append(manifest.Entries, entry)
			tiles[TerrainChunkKey(coord)] = tile
		}
	}
	return manifest, tiles, nil
}
