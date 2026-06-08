package content

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
)

const (
	CurrentImportedWorldSchemaVersion      = 2
	CurrentImportedWorldChunkSchemaVersion = 1

	DefaultImportedWorldSectorTargetWorldSize = 25.0
	DefaultImportedWorldSectorProxyDownsample = 4
)

type ImportedWorldKind string

const (
	ImportedWorldKindVoxelWorld ImportedWorldKind = "imported_voxel_world"

	ImportedWorldChunkPayloadSparseJSONV1     = "sparse_json_v1"
	ImportedWorldChunkPayloadDenseRLEBinaryV1 = "dense_rle_binary_v1"
)

type ImportedWorldDef struct {
	WorldID            string                       `json:"world_id"`
	SchemaVersion      int                          `json:"schema_version"`
	Kind               ImportedWorldKind            `json:"kind"`
	ChunkSize          int                          `json:"chunk_size"`
	VoxelResolution    float32                      `json:"voxel_resolution"`
	Palette            []ImportedWorldPaletteColor  `json:"palette,omitempty"`
	Materials          []ImportedWorldMaterialDef   `json:"materials,omitempty"`
	SourceMaterials    []ImportedWorldMaterialDef   `json:"source_materials,omitempty"`
	SourceBuildVersion string                       `json:"source_build_version,omitempty"`
	SourceHash         string                       `json:"source_hash,omitempty"`
	ChunkPayloadKind   string                       `json:"chunk_payload_kind,omitempty"`
	Tags               []string                     `json:"tags,omitempty"`
	Entries            []ImportedWorldChunkEntryDef `json:"entries,omitempty"`
	Sectors            []ImportedWorldSectorDef     `json:"sectors,omitempty"`
}

type ImportedWorldPaletteColor [4]uint8

type ImportedWorldMaterialDef struct {
	ID                int                       `json:"id"`
	PaletteIndex      uint8                     `json:"palette_index,omitempty"`
	SourceTextureName string                    `json:"source_texture_name,omitempty"`
	BaseColor         ImportedWorldPaletteColor `json:"base_color,omitempty"`
	Kind              string                    `json:"kind,omitempty"`
	CollisionKind     string                    `json:"collision_kind,omitempty"`
	Transparent       bool                      `json:"transparent,omitempty"`
	EmitsLight        bool                      `json:"emits_light,omitempty"`
	Emissive          float32                   `json:"emissive,omitempty"`
	Roughness         float32                   `json:"roughness,omitempty"`
	Metallic          float32                   `json:"metallic,omitempty"`
	Transparency      float32                   `json:"transparency,omitempty"`
	SourceWAD         string                    `json:"source_wad,omitempty"`
	Size              [2]uint32                 `json:"size,omitempty"`
	Tags              []string                  `json:"tags,omitempty"`
}

type ImportedWorldChunkEntryDef struct {
	Coord              TerrainChunkCoordDef `json:"coord"`
	ChunkPath          string               `json:"chunk_path"`
	NonEmptyVoxelCount int                  `json:"non_empty_voxel_count,omitempty"`
	PayloadKind        string               `json:"payload_kind,omitempty"`
	PayloadHash        string               `json:"payload_hash,omitempty"`
	PayloadSizeBytes   int                  `json:"payload_size_bytes,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
}

type ImportedWorldSectorDef struct {
	Coord              TerrainChunkCoordDef   `json:"coord"`
	BoundsMin          [3]float32             `json:"bounds_min"`
	BoundsMax          [3]float32             `json:"bounds_max"`
	FullChunkRefs      []TerrainChunkCoordDef `json:"full_chunk_refs,omitempty"`
	VisibilityID       string                 `json:"visibility_id,omitempty"`
	SourceLeafIDs      []int                  `json:"source_leaf_ids,omitempty"`
	VisibleSectorRefs  []TerrainChunkCoordDef `json:"visible_sector_refs,omitempty"`
	AdjacentSectorRefs []TerrainChunkCoordDef `json:"adjacent_sector_refs,omitempty"`
	NonEmptyVoxelCount int                    `json:"non_empty_voxel_count,omitempty"`
	LODs               []ImportedWorldLODDef  `json:"lods,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
}

type ImportedWorldLODDef struct {
	Level              int      `json:"level"`
	Kind               string   `json:"kind"`
	ChunkPath          string   `json:"chunk_path"`
	ChunkSize          int      `json:"chunk_size,omitempty"`
	VoxelResolution    float32  `json:"voxel_resolution,omitempty"`
	NonEmptyVoxelCount int      `json:"non_empty_voxel_count,omitempty"`
	PayloadKind        string   `json:"payload_kind,omitempty"`
	PayloadHash        string   `json:"payload_hash,omitempty"`
	PayloadSizeBytes   int      `json:"payload_size_bytes,omitempty"`
	Tags               []string `json:"tags,omitempty"`
}

type ImportedWorldChunkDef struct {
	WorldID            string                  `json:"world_id"`
	SchemaVersion      int                     `json:"schema_version"`
	Coord              TerrainChunkCoordDef    `json:"coord"`
	ChunkSize          int                     `json:"chunk_size"`
	VoxelResolution    float32                 `json:"voxel_resolution"`
	PayloadKind        string                  `json:"payload_kind,omitempty"`
	PayloadHash        string                  `json:"payload_hash,omitempty"`
	PayloadSizeBytes   int                     `json:"payload_size_bytes,omitempty"`
	Voxels             []ImportedWorldVoxelDef `json:"voxels,omitempty"`
	NonEmptyVoxelCount int                     `json:"non_empty_voxel_count,omitempty"`
	Tags               []string                `json:"tags,omitempty"`
}

type ImportedWorldVoxelDef struct {
	X     int   `json:"x"`
	Y     int   `json:"y"`
	Z     int   `json:"z"`
	Value uint8 `json:"value"`
}

type ImportedWorldSectorProxyOptions struct {
	WorldID            string
	ChunkSize          int
	VoxelResolution    float32
	Downsample         int
	ProxyDirectoryName string
	Tags               []string
}

func EnsureImportedWorldDefaults(def *ImportedWorldDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentImportedWorldSchemaVersion
	}
	if def.Kind == "" {
		def.Kind = ImportedWorldKindVoxelWorld
	}
	if def.ChunkPayloadKind == "" {
		def.ChunkPayloadKind = ImportedWorldChunkPayloadSparseJSONV1
	}
	EnsureImportedWorldSectors(def)
}

func BuildImportedWorldSectorProxyChunks(sectors []ImportedWorldSectorDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, opts ImportedWorldSectorProxyOptions) ([]ImportedWorldSectorDef, map[string]*ImportedWorldChunkDef) {
	if len(sectors) == 0 || len(chunks) == 0 || opts.ChunkSize <= 0 || opts.VoxelResolution <= 0 {
		return sectors, nil
	}
	if opts.Downsample <= 0 {
		opts.Downsample = DefaultImportedWorldSectorProxyDownsample
	}
	if opts.ProxyDirectoryName == "" {
		opts.ProxyDirectoryName = "lods"
	}
	if opts.WorldID == "" {
		for _, chunk := range chunks {
			if chunk != nil && chunk.WorldID != "" {
				opts.WorldID = chunk.WorldID
				break
			}
		}
	}
	out := append([]ImportedWorldSectorDef(nil), sectors...)
	proxies := make(map[string]*ImportedWorldChunkDef)
	for i := range out {
		sector := &out[i]
		proxy := buildImportedWorldSectorProxyChunk(*sector, chunks, opts)
		if proxy == nil || proxy.NonEmptyVoxelCount == 0 {
			continue
		}
		path := filepath.ToSlash(filepath.Join(opts.ProxyDirectoryName, fmt.Sprintf("%s_sector_%d_%d_%d_lod1.gkchunk", opts.WorldID, sector.Coord.X, sector.Coord.Y, sector.Coord.Z)))
		proxy.Tags = appendSectorTags(proxy.Tags, opts.Tags)
		proxy.Tags = appendSectorTags(proxy.Tags, []string{"lod:1", "sector_proxy"})
		proxies[path] = proxy
		sector.LODs = []ImportedWorldLODDef{{
			Level:              1,
			Kind:               "voxel_proxy",
			ChunkPath:          path,
			ChunkSize:          proxy.ChunkSize,
			VoxelResolution:    proxy.VoxelResolution,
			NonEmptyVoxelCount: proxy.NonEmptyVoxelCount,
			Tags:               append([]string(nil), proxy.Tags...),
		}}
	}
	if len(proxies) == 0 {
		return out, nil
	}
	return out, proxies
}

func buildImportedWorldSectorProxyChunk(sector ImportedWorldSectorDef, chunks map[TerrainChunkCoordDef]*ImportedWorldChunkDef, opts ImportedWorldSectorProxyOptions) *ImportedWorldChunkDef {
	spanChunks := ImportedWorldSectorSpanChunks(opts.ChunkSize, opts.VoxelResolution, DefaultImportedWorldSectorTargetWorldSize)
	fullSide := spanChunks * opts.ChunkSize
	proxySize := importedWorldCeilDiv(fullSide, opts.Downsample)
	if proxySize <= 0 {
		return nil
	}
	type cellMaterialCount struct {
		value uint8
		count int
	}
	counts := make(map[[4]int]int)
	best := make(map[[3]int]cellMaterialCount)
	for _, ref := range sector.FullChunkRefs {
		chunk := chunks[ref]
		if chunk == nil || (chunk.NonEmptyVoxelCount == 0 && len(chunk.Voxels) == 0) {
			continue
		}
		baseX := (ref.X - sector.Coord.X*spanChunks) * opts.ChunkSize
		baseY := (ref.Y - sector.Coord.Y*spanChunks) * opts.ChunkSize
		baseZ := (ref.Z - sector.Coord.Z*spanChunks) * opts.ChunkSize
		for _, voxel := range chunk.Voxels {
			if voxel.Value == 0 {
				continue
			}
			x := (baseX + voxel.X) / opts.Downsample
			y := (baseY + voxel.Y) / opts.Downsample
			z := (baseZ + voxel.Z) / opts.Downsample
			if x < 0 || y < 0 || z < 0 || x >= proxySize || y >= proxySize || z >= proxySize {
				continue
			}
			key4 := [4]int{x, y, z, int(voxel.Value)}
			counts[key4]++
			key3 := [3]int{x, y, z}
			current := best[key3]
			nextCount := counts[key4]
			if nextCount > current.count || current.value == 0 {
				best[key3] = cellMaterialCount{value: voxel.Value, count: nextCount}
			}
		}
	}
	if len(best) == 0 {
		return nil
	}
	keys := make([][3]int, 0, len(best))
	for key := range best {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][2] < keys[j][2]
	})
	voxels := make([]ImportedWorldVoxelDef, 0, len(keys))
	for _, key := range keys {
		voxels = append(voxels, ImportedWorldVoxelDef{
			X:     key[0],
			Y:     key[1],
			Z:     key[2],
			Value: best[key].value,
		})
	}
	return &ImportedWorldChunkDef{
		WorldID:            opts.WorldID,
		SchemaVersion:      CurrentImportedWorldChunkSchemaVersion,
		Coord:              sector.Coord,
		ChunkSize:          proxySize,
		VoxelResolution:    opts.VoxelResolution * float32(opts.Downsample),
		Voxels:             voxels,
		NonEmptyVoxelCount: len(voxels),
	}
}

func importedWorldCeilDiv(value int, divisor int) int {
	if divisor <= 0 {
		return value
	}
	if value <= 0 {
		return 0
	}
	return (value + divisor - 1) / divisor
}

func EnsureImportedWorldChunkDefaults(def *ImportedWorldChunkDef) {
	if def == nil {
		return
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentImportedWorldChunkSchemaVersion
	}
	if def.PayloadKind == "" {
		def.PayloadKind = ImportedWorldChunkPayloadSparseJSONV1
	}
	if def.NonEmptyVoxelCount == 0 {
		def.NonEmptyVoxelCount = len(def.Voxels)
	}
}

func EnsureImportedWorldSectors(def *ImportedWorldDef) {
	if def == nil || len(def.Sectors) > 0 || len(def.Entries) == 0 {
		return
	}
	def.Sectors = BuildImportedWorldSectors(def.Entries, def.ChunkSize, def.VoxelResolution, DefaultImportedWorldSectorTargetWorldSize)
}

func BuildImportedWorldSectors(entries []ImportedWorldChunkEntryDef, chunkSize int, voxelResolution float32, targetWorldSize float32) []ImportedWorldSectorDef {
	if len(entries) == 0 {
		return nil
	}
	spanChunks := ImportedWorldSectorSpanChunks(chunkSize, voxelResolution, targetWorldSize)
	chunkWorldSize := float32(chunkSize) * voxelResolution
	if chunkWorldSize <= 0 {
		chunkWorldSize = 1
	}
	type sectorAccum struct {
		coord TerrainChunkCoordDef
		refs  []TerrainChunkCoordDef
		count int
		tags  []string
	}
	sectorMap := make(map[TerrainChunkCoordDef]*sectorAccum)
	for _, entry := range entries {
		sectorCoord := TerrainChunkCoordDef{
			X: importedWorldFloorDiv(entry.Coord.X, spanChunks),
			Y: importedWorldFloorDiv(entry.Coord.Y, spanChunks),
			Z: importedWorldFloorDiv(entry.Coord.Z, spanChunks),
		}
		sector := sectorMap[sectorCoord]
		if sector == nil {
			sector = &sectorAccum{coord: sectorCoord}
			sectorMap[sectorCoord] = sector
		}
		sector.refs = append(sector.refs, entry.Coord)
		sector.count += entry.NonEmptyVoxelCount
		sector.tags = appendSectorTags(sector.tags, entry.Tags)
	}
	sectorCoords := make([]TerrainChunkCoordDef, 0, len(sectorMap))
	for coord := range sectorMap {
		sectorCoords = append(sectorCoords, coord)
	}
	sort.Slice(sectorCoords, func(i, j int) bool {
		return terrainCoordLess(sectorCoords[i], sectorCoords[j])
	})
	sectors := make([]ImportedWorldSectorDef, 0, len(sectorCoords))
	for _, coord := range sectorCoords {
		sector := sectorMap[coord]
		sort.Slice(sector.refs, func(i, j int) bool {
			return terrainCoordLess(sector.refs[i], sector.refs[j])
		})
		min := [3]float32{
			float32(coord.X*spanChunks) * chunkWorldSize,
			float32(coord.Y*spanChunks) * chunkWorldSize,
			float32(coord.Z*spanChunks) * chunkWorldSize,
		}
		max := [3]float32{
			float32((coord.X+1)*spanChunks) * chunkWorldSize,
			float32((coord.Y+1)*spanChunks) * chunkWorldSize,
			float32((coord.Z+1)*spanChunks) * chunkWorldSize,
		}
		sectors = append(sectors, ImportedWorldSectorDef{
			Coord:              coord,
			BoundsMin:          min,
			BoundsMax:          max,
			FullChunkRefs:      append([]TerrainChunkCoordDef(nil), sector.refs...),
			VisibilityID:       TerrainChunkKey(coord),
			NonEmptyVoxelCount: sector.count,
			Tags:               append([]string(nil), sector.tags...),
		})
	}
	return sectors
}

func ImportedWorldSectorSpanChunks(chunkSize int, voxelResolution float32, targetWorldSize float32) int {
	chunkWorldSize := float64(chunkSize) * float64(voxelResolution)
	if chunkWorldSize <= 0 || targetWorldSize <= 0 {
		return 1
	}
	span := int(math.Ceil(float64(targetWorldSize) / chunkWorldSize))
	if span < 1 {
		return 1
	}
	return span
}

func importedWorldFloorDiv(value int, divisor int) int {
	if divisor <= 0 {
		return value
	}
	quotient := value / divisor
	remainder := value % divisor
	if remainder != 0 && ((remainder < 0) != (divisor < 0)) {
		quotient--
	}
	return quotient
}

func terrainCoordLess(a TerrainChunkCoordDef, b TerrainChunkCoordDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}

func appendSectorTags(dst []string, tags []string) []string {
	if len(tags) == 0 {
		return dst
	}
	seen := make(map[string]struct{}, len(dst)+len(tags))
	for _, tag := range dst {
		seen[tag] = struct{}{}
	}
	for _, tag := range tags {
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		dst = append(dst, tag)
	}
	return dst
}
