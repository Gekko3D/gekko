package content

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// LevelStreamingIndex owns metadata in each layer's identity world space. It
// does not load payloads or select runtime residency.
type LevelStreamingIndex struct {
	IndependentLayers bool
	StreamingBounds   *LevelStreamingBoundsDef
	Terrain, POI      *LevelStreamingLayerIndex
}

type LevelStreamingLayerIndex struct {
	OwnerID, SourceHash                    string
	ChunkSize                              int
	VoxelResolution                        float32
	LegacyDistance, SourceOnly             bool
	Pages                                  []StreamPageDef
	RootPageIndices                        []uint32
	ProxylessPageIndices                   []uint32
	Sectors                                []ImportedWorldSectorV3Def
	Forest                                 *StreamPageForestIndex
	EntryIndexByCoord, SectorIndexByCoord  map[TerrainChunkCoordDef]int
	SourceBounds, PageBounds, SectorBounds []LevelStreamingBoundsRecord
	SourceIndex, PageIndex, SectorIndex    *LevelStreamingBoundsIndex
}

// Known denotes full XYZ coverage. FootprintKnown denotes XZ coverage even
// when legacy source metadata cannot qualify height. Empty backing entries
// retain records; known flat bounds are valid and use closed intersections.
type LevelStreamingBoundsRecord struct {
	ManifestIndex         uint32
	BoundsMin, BoundsMax  [3]float32
	Known, FootprintKnown bool
}

// LevelStreamingBoundsIndex privately copies records into balanced AABB trees.
// Its public layer records may be inspected or changed without changing queries.
type LevelStreamingBoundsIndex struct {
	xyz, xz *levelBoundsNode
	unknown []uint32
}

type levelBoundsNode struct {
	bounds      StreamPageBounds
	left, right *levelBoundsNode
	index       uint32
}

func levelBoundsTree(records []LevelStreamingBoundsRecord, footprint bool) *levelBoundsNode {
	if len(records) == 0 {
		return nil
	}
	b := StreamPageBounds{Min: records[0].BoundsMin, Max: records[0].BoundsMax}
	for _, r := range records[1:] {
		b = unionStreamPageBounds(b, StreamPageBounds{Min: r.BoundsMin, Max: r.BoundsMax})
	}
	n := &levelBoundsNode{bounds: b}
	if len(records) == 1 {
		n.index = records[0].ManifestIndex
		return n
	}
	axis := 0
	for a := 1; a < 3; a++ {
		if (!footprint || a != 1) && float64(b.Max[a])-float64(b.Min[a]) > float64(b.Max[axis])-float64(b.Min[axis]) {
			axis = a
		}
	}
	sort.Slice(records, func(i, j int) bool {
		a := float64(records[i].BoundsMin[axis]) + float64(records[i].BoundsMax[axis])
		b := float64(records[j].BoundsMin[axis]) + float64(records[j].BoundsMax[axis])
		if a == b {
			return records[i].ManifestIndex < records[j].ManifestIndex
		}
		return a < b
	})
	mid := len(records) / 2
	n.left, n.right = levelBoundsTree(records[:mid], footprint), levelBoundsTree(records[mid:], footprint)
	return n
}

func newLevelBoundsIndex(records []LevelStreamingBoundsRecord) *LevelStreamingBoundsIndex {
	i := &LevelStreamingBoundsIndex{}
	var xyz, xz []LevelStreamingBoundsRecord
	for _, r := range records {
		if r.Known {
			xyz = append(xyz, r)
		} else if r.FootprintKnown {
			xz = append(xz, r)
		} else {
			i.unknown = append(i.unknown, r.ManifestIndex)
		}
	}
	i.xyz, i.xz = levelBoundsTree(xyz, false), levelBoundsTree(xz, true)
	return i
}

func levelBoundsIntersect(a, b StreamPageBounds, footprint bool) bool {
	for axis := 0; axis < 3; axis++ {
		if footprint && axis == 1 {
			continue
		}
		if a.Min[axis] > b.Max[axis] || a.Max[axis] < b.Min[axis] {
			return false
		}
	}
	return true
}

func queryLevelBounds(n *levelBoundsNode, b StreamPageBounds, footprint bool, result *[]uint32) {
	if n == nil || !levelBoundsIntersect(n.bounds, b, footprint) {
		return
	}
	if n.left == nil {
		*result = append(*result, n.index)
		return
	}
	queryLevelBounds(n.left, b, footprint, result)
	queryLevelBounds(n.right, b, footprint, result)
}

// Query returns original manifest indices in ascending order. Intersections are
// closed, so points and shared edges match. Unknown heights filter only XZ;
// entirely unspecified coverage matches every valid query conservatively.
func (i *LevelStreamingBoundsIndex) Query(min, max [3]float32) ([]uint32, error) {
	b := StreamPageBounds{Min: min, Max: max}
	if i == nil || !validStreamPageBounds(b, false) {
		return nil, fmt.Errorf("invalid level streaming bounds query")
	}
	result := append([]uint32(nil), i.unknown...)
	queryLevelBounds(i.xyz, b, false, &result)
	queryLevelBounds(i.xz, b, true, &result)
	sort.Slice(result, func(a, b int) bool { return result[a] < result[b] })
	return result, nil
}

func levelKnownRecord(index int, b StreamPageBounds) LevelStreamingBoundsRecord {
	return LevelStreamingBoundsRecord{ManifestIndex: uint32(index), BoundsMin: b.Min, BoundsMax: b.Max, Known: true, FootprintKnown: true}
}

func levelLayerIndexes(layer *LevelStreamingLayerIndex) {
	layer.PageBounds = make([]LevelStreamingBoundsRecord, len(layer.Pages))
	for i, p := range layer.Pages {
		layer.PageBounds[i] = levelKnownRecord(i, StreamPageBounds{Min: p.BoundsMin, Max: p.BoundsMax})
	}
	layer.SourceIndex, layer.PageIndex, layer.SectorIndex = newLevelBoundsIndex(layer.SourceBounds), newLevelBoundsIndex(layer.PageBounds), newLevelBoundsIndex(layer.SectorBounds)
}

func levelTerrainIndex(d *TerrainChunkManifestDef) (*LevelStreamingLayerIndex, error) {
	if uint64(len(d.Entries)) > math.MaxUint32 || uint64(len(d.Pages)) > math.MaxUint32 {
		return nil, fmt.Errorf("terrain index exceeds uint32 range")
	}
	n, err := NormalizeTerrainPages(d)
	if err != nil {
		return nil, err
	}
	l := &LevelStreamingLayerIndex{OwnerID: d.TerrainID, SourceHash: d.SourceHash, ChunkSize: d.ChunkSize, VoxelResolution: d.VoxelResolution, LegacyDistance: n.LegacyDistance, SourceOnly: n.SourceOnly, Pages: n.Pages, RootPageIndices: n.RootPageIndices, Forest: n.Forest, EntryIndexByCoord: n.EntryIndexByCoord, SectorIndexByCoord: map[TerrainChunkCoordDef]int{}}
	for i, e := range d.Entries {
		size, res := e.ChunkSize, e.VoxelResolution
		if size == 0 {
			size = d.ChunkSize
		}
		if res == 0 {
			res = d.VoxelResolution
		}
		span := float32(size) * res
		var b StreamPageBounds
		known := true
		if d.SchemaVersion == 3 {
			b = StreamPageBounds{Min: [3]float32{e.WorldOrigin[0], e.HeightOffset, e.WorldOrigin[2]}, Max: [3]float32{e.WorldOrigin[0] + span, e.HeightOffset + e.HeightScale, e.WorldOrigin[2] + span}}
			known = e.HeightOffset != 0 || e.HeightScale != 0
		} else {
			origin := [3]float32{float32(e.Coord.X) * span, float32(e.Coord.Y) * span, float32(e.Coord.Z) * span}
			b = StreamPageBounds{Min: origin, Max: [3]float32{origin[0] + span, origin[1] + float32(e.NonEmptyVoxelCount)*res, origin[2] + span}}
		}
		if !validStreamPageBounds(b, false) || b.Min[0] >= b.Max[0] || b.Min[2] >= b.Max[2] || (known && d.SchemaVersion == 3 && b.Min[1] >= b.Max[1]) {
			return nil, fmt.Errorf("invalid terrain source world coverage")
		}
		r := levelKnownRecord(i, b)
		r.Known = known
		l.SourceBounds = append(l.SourceBounds, r)
	}
	levelLayerIndexes(l)
	return l, nil
}

func levelPOIIndex(d *ImportedWorldDef) (*LevelStreamingLayerIndex, error) {
	var n *ImportedWorldPageIndex
	var err error
	if d.SchemaVersion == ImportedWorldPageSchemaVersion {
		n, err = ValidateImportedWorldV3(d)
	} else {
		n, err = NormalizeImportedWorldPages(d)
	}
	if err != nil {
		return nil, err
	}
	l := &LevelStreamingLayerIndex{OwnerID: d.WorldID, SourceHash: d.SourceHash, ChunkSize: d.ChunkSize, VoxelResolution: d.VoxelResolution, LegacyDistance: n.LegacyDistance, Pages: n.Pages, RootPageIndices: n.RootPageIndices, ProxylessPageIndices: n.ProxylessPageIndices, Sectors: n.Sectors, Forest: n.Forest, EntryIndexByCoord: n.EntryIndexByCoord, SectorIndexByCoord: n.SectorIndexByCoord}
	for i, e := range d.Entries {
		b, err := importedPageGridBounds(e.Coord, d.ChunkSize, d.VoxelResolution)
		if err != nil {
			return nil, err
		}
		l.SourceBounds = append(l.SourceBounds, levelKnownRecord(i, b))
	}
	for i, s := range n.Sectors {
		r := levelKnownRecord(i, StreamPageBounds{Min: s.BoundsMin, Max: s.BoundsMax})
		if s.BoundsMin == ([3]float32{}) && s.BoundsMax == ([3]float32{}) {
			r.Known, r.FootprintKnown = false, false
		}
		l.SectorBounds = append(l.SectorBounds, r)
	}
	levelLayerIndexes(l)
	return l, nil
}

func levelLayerContained(layer *LevelStreamingLayerIndex, bounds StreamPageBounds) bool {
	if layer == nil {
		return true
	}
	for _, records := range [][]LevelStreamingBoundsRecord{layer.SourceBounds, layer.PageBounds, layer.SectorBounds} {
		for _, r := range records {
			if !r.Known {
				return false
			}
			for a := 0; a < 3; a++ {
				if r.BoundsMin[a] < bounds.Min[a] || r.BoundsMax[a] > bounds.Max[a] {
					return false
				}
			}
		}
	}
	return true
}

func validateLevelCoverageGroups(terrain, poi *LevelStreamingLayerIndex) error {
	groups := [2]map[string][]StreamPageBounds{{}, {}}
	for layerIndex, layer := range []*LevelStreamingLayerIndex{terrain, poi} {
		if layer == nil {
			continue
		}
		for _, p := range layer.Pages {
			if p.CoverageGroup == "" {
				continue
			}
			if strings.TrimSpace(p.CoverageGroup) == "" {
				return fmt.Errorf("blank level coverage group")
			}
			groups[layerIndex][p.CoverageGroup] = append(groups[layerIndex][p.CoverageGroup], StreamPageBounds{Min: p.BoundsMin, Max: p.BoundsMax})
		}
	}
	for side := 0; side < 2; side++ {
		for id, members := range groups[side] {
			paired := false
			for _, a := range members {
				for _, b := range groups[1-side][id] {
					if levelBoundsIntersect(a, b, false) {
						paired = true
						break
					}
				}
				if paired {
					break
				}
			}
			if !paired {
				return fmt.Errorf("coverage group %q has no overlapping terrain/POI pair", id)
			}
		}
	}
	return nil
}

// BuildLevelStreamingIndex validates normalized metadata once per layer without
// I/O. Any explicit schema3 layer enables independent tooling grids; runtime
// version admission and legacy shared-grid validation remain separate owners.
func BuildLevelStreamingIndex(level *LevelDef, terrain *TerrainChunkManifestDef, poi *ImportedWorldDef) (*LevelStreamingIndex, error) {
	if level == nil {
		return nil, fmt.Errorf("level is nil")
	}
	i := &LevelStreamingIndex{}
	var err error
	if terrain != nil {
		i.Terrain, err = levelTerrainIndex(terrain)
		if err != nil {
			return nil, err
		}
		i.IndependentLayers = terrain.SchemaVersion == 3
	}
	if poi != nil {
		i.POI, err = levelPOIIndex(poi)
		if err != nil {
			return nil, err
		}
		i.IndependentLayers = i.IndependentLayers || poi.SchemaVersion == 3
	}
	if level.StreamingBounds != nil {
		b := StreamPageBounds{Min: level.StreamingBounds.BoundsMin, Max: level.StreamingBounds.BoundsMax}
		if !validStreamPageBounds(b, true) || !levelLayerContained(i.Terrain, b) || !levelLayerContained(i.POI, b) {
			return nil, fmt.Errorf("invalid streaming bounds or incomplete known layer containment")
		}
		copy := *level.StreamingBounds
		i.StreamingBounds = &copy
	}
	if err = validateLevelCoverageGroups(i.Terrain, i.POI); err != nil {
		return nil, err
	}
	return i, nil
}
