package content

import (
	"encoding/hex"
	"fmt"
	"math"
	"strings"
)

const (
	TerrainHeightTileSchemaVersion         = 1
	TerrainHeightTileManifestSchemaVersion = 3
	TerrainHeightTilePayloadKind           = "height_u16_binary_v1"
	terrainHeightTileMaxPayload            = 43008
)

type TerrainHeightTileDef struct {
	SchemaVersion           int                  `json:"schema_version"`
	TerrainID               string               `json:"terrain_id"`
	SourceHash              string               `json:"source_hash"`
	Coord                   TerrainChunkCoordDef `json:"coord"`
	WorldOrigin             [3]float32           `json:"world_origin"`
	SampleWidth             int                  `json:"sample_width"`
	SampleHeight            int                  `json:"sample_height"`
	SampleSpacing           float32              `json:"sample_spacing"`
	HeightOffset            float32              `json:"height_offset"`
	HeightScale             float32              `json:"height_scale"`
	HeightSamples           []uint16             `json:"-"`
	SurfaceMask             []byte               `json:"-"`
	OutdoorNavCellSize      float32              `json:"outdoor_nav_cell_size"`
	OutdoorNavExclusionMask []byte               `json:"-"`
}

func terrainFinite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func terrainPayloadHashValid(hash string) bool {
	if len(hash) != 64 || strings.ToLower(hash) != hash {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func validateTerrainHeightTileMetadata(d *TerrainHeightTileDef, maskBytes, navBytes int) error {
	if d == nil {
		return fmt.Errorf("terrain height tile is nil")
	}
	if d.SchemaVersion != TerrainHeightTileSchemaVersion || strings.TrimSpace(d.TerrainID) == "" || strings.TrimSpace(d.SourceHash) == "" {
		return fmt.Errorf("invalid terrain height tile identity or schema")
	}
	if d.SampleWidth < 1 || d.SampleWidth > 128 || d.SampleHeight < 1 || d.SampleHeight > 128 {
		return fmt.Errorf("invalid terrain height tile dimensions")
	}
	if !terrainFinite(d.SampleSpacing) || d.SampleSpacing <= 0 || !terrainFinite(d.HeightOffset) || !terrainFinite(d.HeightScale) || d.HeightScale <= 0 || !terrainFinite(d.HeightOffset+d.HeightScale) {
		return fmt.Errorf("invalid terrain height tile height or spacing")
	}
	for _, v := range d.WorldOrigin {
		if !terrainFinite(v) {
			return fmt.Errorf("invalid terrain height tile origin")
		}
	}
	if !terrainFinite(d.WorldOrigin[0]+float32(d.SampleWidth)*d.SampleSpacing) || !terrainFinite(d.WorldOrigin[2]+float32(d.SampleHeight)*d.SampleSpacing) {
		return fmt.Errorf("terrain height tile extent overflows")
	}
	n := d.SampleWidth * d.SampleHeight
	if maskBytes != 0 && maskBytes != (n+7)/8 {
		return fmt.Errorf("invalid terrain height tile surface mask length")
	}
	if navBytes == 0 {
		if d.OutdoorNavCellSize != 0 {
			return fmt.Errorf("navigation cell size requires a mask")
		}
	} else if navBytes != 8192 || d.SampleWidth != 128 || d.SampleHeight != 128 || d.SampleSpacing != 2 || d.OutdoorNavCellSize != 1 {
		return fmt.Errorf("invalid terrain height tile navigation mask")
	}
	return nil
}

func ValidateTerrainHeightTile(d *TerrainHeightTileDef) error {
	if d == nil {
		return fmt.Errorf("terrain height tile is nil")
	}
	if err := validateTerrainHeightTileMetadata(d, len(d.SurfaceMask), len(d.OutdoorNavExclusionMask)); err != nil {
		return err
	}
	n := d.SampleWidth * d.SampleHeight
	if len(d.HeightSamples) != n {
		return fmt.Errorf("invalid terrain height tile sample count")
	}
	if len(d.SurfaceMask) > 0 && n%8 != 0 && d.SurfaceMask[len(d.SurfaceMask)-1]>>uint(n%8) != 0 {
		return fmt.Errorf("nonzero terrain height tile surface mask padding")
	}
	return nil
}

func validateTerrainHeightTileEntry(e TerrainChunkEntryDef) error {
	if strings.TrimSpace(e.TerrainID) == "" || strings.TrimSpace(e.SourceHash) == "" || strings.TrimSpace(e.ChunkPath) == "" || e.PayloadKind != TerrainHeightTilePayloadKind || !terrainPayloadHashValid(e.PayloadHash) {
		return fmt.Errorf("invalid terrain height tile reference identity")
	}
	if e.ChunkSize < 1 || e.ChunkSize > 128 || !terrainFinite(e.VoxelResolution) || e.VoxelResolution <= 0 || e.Coord.Y != 0 || e.WorldOrigin[1] != 0 {
		return fmt.Errorf("invalid terrain height tile reference lattice")
	}
	span := float64(e.ChunkSize) * float64(e.VoxelResolution)
	wx, wz := float32(float64(e.Coord.X)*span), float32(float64(e.Coord.Z)*span)
	if !terrainFinite(wx) || !terrainFinite(wz) || e.WorldOrigin != [3]float32{wx, 0, wz} || !terrainFinite(wx+float32(span)) || !terrainFinite(wz+float32(span)) {
		return fmt.Errorf("invalid terrain height tile reference origin")
	}
	n := e.ChunkSize * e.ChunkSize
	validSize := e.PayloadSizeBytes == 2*n || e.PayloadSizeBytes == 2*n+(n+7)/8
	if e.ChunkSize == 128 && e.VoxelResolution == 2 {
		validSize = validSize || e.PayloadSizeBytes == 2*n+8192 || e.PayloadSizeBytes == 2*n+(n+7)/8+8192
	}
	if !validSize {
		return fmt.Errorf("invalid terrain height tile reference payload size")
	}
	return nil
}

func validateTerrainHeightTileManifest(d *TerrainChunkManifestDef) error {
	if strings.TrimSpace(d.TerrainID) == "" || strings.TrimSpace(d.SourceHash) == "" || d.ChunkSize < 1 || d.ChunkSize > 128 || !terrainFinite(d.VoxelResolution) || d.VoxelResolution <= 0 {
		return fmt.Errorf("invalid terrain height tile manifest")
	}
	seen := make(map[TerrainChunkCoordDef]bool, len(d.Entries))
	for _, e := range d.Entries {
		if err := validateTerrainHeightTileEntry(e); err != nil {
			return err
		}
		if e.TerrainID != d.TerrainID || e.SourceHash != d.SourceHash || e.ChunkSize != d.ChunkSize || e.VoxelResolution != d.VoxelResolution || seen[e.Coord] {
			return fmt.Errorf("mismatched or duplicate terrain height tile entry")
		}
		seen[e.Coord] = true
	}
	return nil
}
