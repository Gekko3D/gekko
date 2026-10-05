package content

import (
	"fmt"
	"math"
	"strconv"
)

// ImportedWorldPageIndex is a metadata-only legacy compatibility view. It
// preserves distance/PVS selection and does not promise strict v3 fallback
// coverage. All slices and maps are owned independently of authored content.
type ImportedWorldPageIndex struct {
	LegacyDistance       bool
	Pages                []StreamPageDef
	RootPageIndices      []uint32
	ProxylessPageIndices []uint32
	Sectors              []ImportedWorldSectorV3Def
	Forest               *StreamPageForestIndex
	EntryIndexByCoord    map[TerrainChunkCoordDef]int
	SectorIndexByCoord   map[TerrainChunkCoordDef]int
}

func legacyImportedWorldVersion(version int) bool { return version >= 0 && version <= 2 }

func importedPageGridBounds(coord TerrainChunkCoordDef, size int, spacing float32) (StreamPageBounds, error) {
	side := float32(size) * spacing
	if size <= 0 || !terrainFinite(spacing) || spacing <= 0 || !terrainFinite(side) || side <= 0 {
		return StreamPageBounds{}, fmt.Errorf("invalid imported page grid")
	}
	bounds := StreamPageBounds{Min: [3]float32{float32(coord.X) * side, float32(coord.Y) * side, float32(coord.Z) * side}}
	for axis := 0; axis < 3; axis++ {
		bounds.Max[axis] = bounds.Min[axis] + side
	}
	if !validStreamPageBounds(bounds, true) {
		return StreamPageBounds{}, fmt.Errorf("imported page grid overflows or loses cell precision")
	}
	return bounds, nil
}

func unionStreamPageBounds(a, b StreamPageBounds) StreamPageBounds {
	for axis := 0; axis < 3; axis++ {
		a.Min[axis] = min(a.Min[axis], b.Min[axis])
		a.Max[axis] = max(a.Max[axis], b.Max[axis])
	}
	return a
}

// checkedLegacySectorDefaults establishes the arithmetic preconditions of the
// existing deterministic sector builder before it performs integer products.
func checkedLegacySectorDefaults(world *ImportedWorldDef) ([]ImportedWorldSectorDef, error) {
	if len(world.Entries) == 0 {
		return nil, nil
	}
	side64 := float64(world.ChunkSize) * float64(world.VoxelResolution)
	span64 := math.Max(1, math.Ceil(DefaultImportedWorldSectorTargetWorldSize/side64))
	limit := math.Ldexp(1, strconv.IntSize-1)
	if math.IsNaN(span64) || math.IsInf(span64, 0) || span64 >= limit {
		return nil, fmt.Errorf("imported sector span exceeds integer range")
	}
	span := int(span64)
	maxInt, minInt := int(^uint(0)>>1), -int(^uint(0)>>1)-1
	side := float32(world.ChunkSize) * world.VoxelResolution
	for _, entry := range world.Entries {
		for _, coordinate := range []int{entry.Coord.X, entry.Coord.Y, entry.Coord.Z} {
			q := importedWorldFloorDiv(coordinate, span)
			if q == maxInt || q < minInt/span || q > maxInt/span || q+1 < minInt/span || q+1 > maxInt/span {
				return nil, fmt.Errorf("imported sector coordinate product overflows")
			}
			lo, hi := float32(q*span)*side, float32((q+1)*span)*side
			if !terrainFinite(lo) || !terrainFinite(hi) || lo >= hi {
				return nil, fmt.Errorf("imported sector bounds overflow or lose precision")
			}
		}
	}
	return BuildImportedWorldSectors(world.Entries, world.ChunkSize, world.VoxelResolution, DefaultImportedWorldSectorTargetWorldSize), nil
}

// NormalizeImportedWorldPages consumes legacy metadata without file I/O or
// source mutation. Unlike ValidateStreamPageForest, optional legacy payload
// identity/grid fields and proxy-less sectors remain valid compatibility data.
// A sector's exactly all-zero bounds denote unspecified legacy metadata; its
// page coverage is derived from referenced leaves while sector bounds stay zero.
func NormalizeImportedWorldPages(world *ImportedWorldDef) (*ImportedWorldPageIndex, error) {
	if world == nil {
		return nil, fmt.Errorf("imported world is nil")
	}
	if !legacyImportedWorldVersion(world.SchemaVersion) {
		return nil, fmt.Errorf("unsupported imported world schema version %d", world.SchemaVersion)
	}
	if world.ChunkSize < 0 || !terrainFinite(world.VoxelResolution) || world.VoxelResolution < 0 {
		return nil, fmt.Errorf("invalid legacy imported world grid")
	}
	if uint64(len(world.Entries)) > uint64(math.MaxUint32) || uint64(len(world.Sectors)) > uint64(math.MaxUint32) {
		return nil, fmt.Errorf("imported page index exceeds uint32 range")
	}
	index := &ImportedWorldPageIndex{LegacyDistance: true, EntryIndexByCoord: make(map[TerrainChunkCoordDef]int, len(world.Entries)), SectorIndexByCoord: make(map[TerrainChunkCoordDef]int, len(world.Sectors)), Forest: &StreamPageForestIndex{LeafOwnerPageIndices: make([]int, len(world.Entries))}}
	leaves := make([]StreamPageBounds, len(world.Entries))
	count := 0
	maxInt := int(^uint(0) >> 1)
	for i, entry := range world.Entries {
		if _, seen := index.EntryIndexByCoord[entry.Coord]; seen {
			return nil, fmt.Errorf("duplicate imported chunk coordinate")
		}
		if entry.NonEmptyVoxelCount < 0 || entry.PayloadSizeBytes < 0 || entry.NonEmptyVoxelCount > maxInt-count {
			return nil, fmt.Errorf("invalid imported chunk count")
		}
		count += entry.NonEmptyVoxelCount
		if _, err := NormalizeImportedWorldChunkPayloadKind(entry.PayloadKind); err != nil {
			return nil, err
		}
		bounds, err := importedPageGridBounds(entry.Coord, world.ChunkSize, world.VoxelResolution)
		if err != nil {
			return nil, err
		}
		leaves[i] = bounds
		index.EntryIndexByCoord[entry.Coord] = i
		index.Forest.LeafOwnerPageIndices[i] = -1
	}
	sectors := world.Sectors
	if len(sectors) == 0 {
		var err error
		sectors, err = checkedLegacySectorDefaults(world)
		if err != nil {
			return nil, err
		}
	}
	if uint64(len(sectors)) > uint64(math.MaxUint32) {
		return nil, fmt.Errorf("imported sector index exceeds uint32 range")
	}
	for i, sector := range sectors {
		if _, seen := index.SectorIndexByCoord[sector.Coord]; seen {
			return nil, fmt.Errorf("duplicate imported sector coordinate")
		}
		unspecifiedBounds := sector.BoundsMin == ([3]float32{}) && sector.BoundsMax == ([3]float32{})
		if (!unspecifiedBounds && !validStreamPageBounds(StreamPageBounds{Min: sector.BoundsMin, Max: sector.BoundsMax}, true)) || sector.NonEmptyVoxelCount < 0 || len(sector.FullChunkRefs) == 0 {
			return nil, fmt.Errorf("invalid imported sector bounds or count")
		}
		index.SectorIndexByCoord[sector.Coord] = i
	}
	index.Forest.ParentPageIndices = make([]int, len(sectors))
	for i, sector := range sectors {
		index.Forest.ParentPageIndices[i] = -1
		indexed := ImportedWorldSectorV3Def{Coord: sector.Coord, BoundsMin: sector.BoundsMin, BoundsMax: sector.BoundsMax, VisibilityID: sector.VisibilityID, SourceLeafIDs: append([]int(nil), sector.SourceLeafIDs...), Tags: append([]string(nil), sector.Tags...)}
		page := StreamPageDef{Level: StreamPageLevelLeaf, BoundsMin: sector.BoundsMin, BoundsMax: sector.BoundsMax, Tags: append([]string(nil), sector.Tags...)}
		bounds := StreamPageBounds{Min: sector.BoundsMin, Max: sector.BoundsMax}
		boundsKnown := sector.BoundsMin != ([3]float32{}) || sector.BoundsMax != ([3]float32{})
		for _, ref := range sector.FullChunkRefs {
			leaf, exists := index.EntryIndexByCoord[ref]
			if !exists || index.Forest.LeafOwnerPageIndices[leaf] != -1 {
				return nil, fmt.Errorf("missing, duplicate or shared imported full chunk reference")
			}
			index.Forest.LeafOwnerPageIndices[leaf] = i
			indexed.FullChunkIndices = append(indexed.FullChunkIndices, uint32(leaf))
			page.LeafEntryIndices = append(page.LeafEntryIndices, uint32(leaf))
			if boundsKnown {
				bounds = unionStreamPageBounds(bounds, leaves[leaf])
			} else {
				bounds = leaves[leaf]
				boundsKnown = true
			}
		}
		var err error
		indexed.VisibleSectorIndices, err = indexedLegacySectorRefs(sector.VisibleSectorRefs, index.SectorIndexByCoord)
		if err != nil {
			return nil, err
		}
		indexed.AdjacentSectorIndices, err = indexedLegacySectorRefs(sector.AdjacentSectorRefs, index.SectorIndexByCoord)
		if err != nil {
			return nil, err
		}
		for _, lod := range sector.LODs {
			if lod.ChunkSize < 0 || !terrainFinite(lod.VoxelResolution) || lod.VoxelResolution < 0 || lod.NonEmptyVoxelCount < 0 || lod.PayloadSizeBytes < 0 {
				return nil, fmt.Errorf("invalid optional imported LOD metadata")
			}
		}
		proxy := false
		if len(sector.LODs) > 0 {
			lod := sector.LODs[0]
			if lod.Kind == "voxel_proxy" && lod.NonEmptyVoxelCount > 0 && lod.ChunkPath != "" {
				kind, err := NormalizeImportedWorldChunkPayloadKind(lod.PayloadKind)
				if err != nil {
					return nil, err
				}
				page.Payload = StreamPagePayloadDef{Kind: kind, Path: lod.ChunkPath, ChunkSize: lod.ChunkSize, VoxelResolution: lod.VoxelResolution, PayloadHash: lod.PayloadHash, PayloadSizeBytes: lod.PayloadSizeBytes}
				if lod.ChunkSize > 0 && lod.VoxelResolution > 0 {
					coverage, err := importedPageGridBounds(sector.Coord, lod.ChunkSize, lod.VoxelResolution)
					if err != nil {
						return nil, err
					}
					page.Payload.WorldOrigin = coverage.Min
					bounds = unionStreamPageBounds(bounds, coverage)
				}
				proxy = true
			}
		}
		if !proxy {
			index.ProxylessPageIndices = append(index.ProxylessPageIndices, uint32(i))
		}
		page.BoundsMin, page.BoundsMax = bounds.Min, bounds.Max
		index.Pages = append(index.Pages, page)
		index.RootPageIndices = append(index.RootPageIndices, uint32(i))
		index.Sectors = append(index.Sectors, indexed)
	}
	for _, owner := range index.Forest.LeafOwnerPageIndices {
		if owner == -1 {
			return nil, fmt.Errorf("unowned imported chunk entry")
		}
	}
	return index, nil
}

func indexedLegacySectorRefs(refs []TerrainChunkCoordDef, indexes map[TerrainChunkCoordDef]int) ([]uint32, error) {
	var out []uint32
	seen := make(map[TerrainChunkCoordDef]bool, len(refs))
	for _, ref := range refs {
		i, exists := indexes[ref]
		if !exists || seen[ref] {
			return nil, fmt.Errorf("missing or duplicate imported sector reference")
		}
		seen[ref] = true
		out = append(out, uint32(i))
	}
	return out, nil
}
