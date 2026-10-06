package derived

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
)

func islandHarnessSourceBounds(d *content.ImportedWorldDef, o IslandStreamHarnessOptions) ([]content.StreamPageBounds, error) {
	bounds := make([]content.StreamPageBounds, len(d.Entries))
	seen := map[content.TerrainChunkCoordDef]bool{}
	side := float32(d.ChunkSize) * d.VoxelResolution
	if !pageFinite(float64(side)) || side <= 0 {
		return nil, fmt.Errorf("invalid source side")
	}
	for _, span := range []float32{o.RegionalSpan, o.MacroSpan, o.RootSpan} {
		if !pageIntegerRatio(float64(span), float64(side)) {
			return nil, fmt.Errorf("POI source side does not divide hierarchy spans")
		}
	}
	const maxFixtureVoxels = 64_000_000
	totalVoxels := 0
	for _, mats := range [][]content.ImportedWorldMaterialDef{d.SourceMaterials, d.Materials} {
		seen := map[uint8]bool{}
		for _, m := range mats {
			if seen[m.PaletteIndex] || !pageFinite(float64(m.Transparency)) {
				return nil, fmt.Errorf("ambiguous source material")
			}
			seen[m.PaletteIndex] = true
		}
	}
	for i, e := range d.Entries {
		if seen[e.Coord] || e.NonEmptyVoxelCount < 0 || e.NonEmptyVoxelCount > d.ChunkSize*d.ChunkSize*d.ChunkSize || e.ChunkPath == "" || len(e.PayloadHash) != 64 || e.PayloadSizeBytes <= 0 {
			return nil, fmt.Errorf("invalid source entry metadata")
		}
		seen[e.Coord] = true
		if e.NonEmptyVoxelCount > maxFixtureVoxels-totalVoxels {
			return nil, fmt.Errorf("harness source exceeds 64 million voxel budget")
		}
		totalVoxels += e.NonEmptyVoxelCount
		if _, err := hex.DecodeString(e.PayloadHash); err != nil {
			return nil, err
		}
		kind, kindErr := content.NormalizeImportedWorldChunkPayloadKind(e.PayloadKind)
		if kindErr != nil || kind != e.PayloadKind || kind == content.ImportedWorldChunkPayloadSparseJSONV1 {
			return nil, fmt.Errorf("source must have qualified binary payloads")
		}
		b := content.StreamPageBounds{Min: [3]float32{float32(e.Coord.X) * side, float32(e.Coord.Y) * side, float32(e.Coord.Z) * side}}
		for a := 0; a < 3; a++ {
			b.Max[a] = b.Min[a] + side
			if !pageFinite(float64(b.Min[a])) || !pageFinite(float64(b.Max[a])) || b.Max[a] <= b.Min[a] || b.Min[a]+d.VoxelResolution == b.Min[a] {
				return nil, fmt.Errorf("unsafe source world coordinates")
			}
		}
		if b.Min[0] < -o.PlayableSpan/2 || b.Max[0] > o.PlayableSpan/2 || b.Min[2] < -o.PlayableSpan/2 || b.Max[2] > o.PlayableSpan/2 {
			return nil, fmt.Errorf("POI does not fit playable bounds")
		}
		bounds[i] = b
	}
	return bounds, nil
}

type islandPOIGroupKey struct {
	level uint8
	x, z  int
}
type islandPOICellPair struct {
	x, y, z int
	pair    pagePair
}
type islandPOIGroup struct {
	counts    map[islandPOICellPair]uint32
	children  []uint32
	bounds    content.StreamPageBounds
	hasBounds bool
}

func islandHarnessPOI(source *content.ImportedWorldDef, load func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error), o IslandStreamHarnessOptions, leafBounds []content.StreamPageBounds, payloads map[string]*content.ImportedWorldChunkDef) (*content.ImportedWorldDef, string, error) {
	d, err := pageClone(source)
	if err != nil {
		return nil, "", err
	}
	d.SchemaVersion = 3
	d.Kind = content.ImportedWorldKindVoxelWorld
	d.Pages = nil
	d.RootPageIndices = nil
	d.IndexedSectors = nil
	d.PageIndex = nil
	groups := map[islandPOIGroupKey]*islandPOIGroup{}
	spans := []float32{0, o.RegionalSpan, o.MacroSpan, o.RootSpan}
	res := []float32{0, max(source.VoxelResolution, o.RegionalSpan/128), max(source.VoxelResolution, o.MacroSpan/128), max(source.VoxelResolution, o.RootSpan/128)}
	for level := 1; level <= 3; level++ {
		if !pageIntegerRatio(float64(spans[level]), float64(res[level])) || !pageIntegerRatio(float64(res[level]), float64(source.VoxelResolution)) {
			return nil, "", fmt.Errorf("unaligned POI coarse grid")
		}
	}
	group := func(key islandPOIGroupKey) *islandPOIGroup {
		g := groups[key]
		if g == nil {
			g = &islandPOIGroup{counts: map[islandPOICellPair]uint32{}}
			groups[key] = g
		}
		return g
	}
	hash := sha256.New()
	materials := map[uint8]content.ImportedWorldMaterialDef{}
	for _, list := range [][]content.ImportedWorldMaterialDef{d.SourceMaterials, d.Materials} {
		for _, m := range list {
			materials[m.PaletteIndex] = m
		}
	}
	for i, e := range d.Entries {
		c, err := load(e)
		if err != nil {
			return nil, "", err
		}
		if c == nil || c.WorldID != d.WorldID || c.Coord != e.Coord || c.ChunkSize != d.ChunkSize || c.VoxelResolution != d.VoxelResolution || c.SchemaVersion != 1 || c.NonEmptyVoxelCount != e.NonEmptyVoxelCount {
			return nil, "", fmt.Errorf("source callback owner/grid/count mismatch")
		}
		voxels := append([]content.ImportedWorldVoxelDef(nil), c.Voxels...)
		sort.Slice(voxels, func(i, j int) bool { return pageVoxelLess(voxels[i], voxels[j]) })
		count := 0
		occupied := make([]byte, (d.ChunkSize*d.ChunkSize*d.ChunkSize+7)/8)
		for _, v := range voxels {
			if v.X < 0 || v.Y < 0 || v.Z < 0 || v.X >= d.ChunkSize || v.Y >= d.ChunkSize || v.Z >= d.ChunkSize {
				return nil, "", fmt.Errorf("source voxel outside grid")
			}
			index := v.X + d.ChunkSize*(v.Y+d.ChunkSize*v.Z)
			bit := byte(1 << uint(index%8))
			if occupied[index/8]&bit != 0 {
				return nil, "", fmt.Errorf("duplicate source voxel")
			}
			occupied[index/8] |= bit
			if v.Value == 0 {
				continue
			}
			count++
			var raw [14]byte
			binary.LittleEndian.PutUint32(raw[0:], uint32(v.X))
			binary.LittleEndian.PutUint32(raw[4:], uint32(v.Y))
			binary.LittleEndian.PutUint32(raw[8:], uint32(v.Z))
			raw[12], raw[13] = v.Value, content.ImportedWorldVoxelMaterialValue(v)
			hash.Write(raw[:])
			material := content.ImportedWorldVoxelMaterialValue(v)
			m := materials[material]
			if m.Transparent || m.Transparency > 0 || m.Kind == "water" || m.Kind == "glass" || m.Kind == "transparent" {
				continue
			}
			p := [3]float64{float64(leafBounds[i].Min[0]) + (float64(v.X)+.5)*float64(c.VoxelResolution), float64(leafBounds[i].Min[1]) + (float64(v.Y)+.5)*float64(c.VoxelResolution), float64(leafBounds[i].Min[2]) + (float64(v.Z)+.5)*float64(c.VoxelResolution)}
			for level := uint8(1); level <= 3; level++ {
				key := islandPOIGroupKey{level, int(math.Floor(p[0] / float64(spans[level]))), int(math.Floor(p[2] / float64(spans[level])))}
				cell := islandPOICellPair{int(math.Floor(p[0] / float64(res[level]))), int(math.Floor(p[1] / float64(res[level]))), int(math.Floor(p[2] / float64(res[level]))), pagePair{v.Value, material}}
				group(key).counts[cell]++
			}
		}
		if count != e.NonEmptyVoxelCount {
			return nil, "", fmt.Errorf("actual source voxel count mismatch")
		}
		d.Entries[i].OccupiedSectorCount, d.Entries[i].OccupiedBrickCount = pageOccupiedCosts(c)
		if e.NonEmptyVoxelCount > 0 {
			b := leafBounds[i]
			leaf := uint32(len(d.Pages))
			d.Pages = append(d.Pages, content.StreamPageDef{Level: 0, BoundsMin: b.Min, BoundsMax: b.Max, LeafEntryIndices: []uint32{uint32(i)}, Payload: content.StreamPagePayloadDef{Kind: e.PayloadKind, Path: e.ChunkPath, WorldOrigin: b.Min, ChunkSize: d.ChunkSize, VoxelResolution: d.VoxelResolution, PayloadHash: e.PayloadHash, PayloadSizeBytes: e.PayloadSizeBytes, Aux: e.Aux, OccupiedSectorCount: d.Entries[i].OccupiedSectorCount, OccupiedBrickCount: d.Entries[i].OccupiedBrickCount}})
			key := islandPOIGroupKey{1, int(math.Floor(float64(b.Min[0]) / float64(o.RegionalSpan))), int(math.Floor(float64(b.Min[2]) / float64(o.RegionalSpan)))}
			g := group(key)
			g.children = append(g.children, leaf)
			if g.hasBounds {
				g.bounds = islandUnionBounds(g.bounds, b)
			} else {
				g.bounds, g.hasBounds = b, true
			}
		}
	}
	for level := uint8(1); level <= 3; level++ {
		keys := []islandPOIGroupKey{}
		for k := range groups {
			if k.level == level {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].z != keys[j].z {
				return keys[i].z < keys[j].z
			}
			return keys[i].x < keys[j].x
		})
		for _, key := range keys {
			g := groups[key]
			resolution := res[level]
			width := int(math.Round(float64(spans[level]) / float64(resolution)))
			minY, maxY := 0, 0
			first := true
			for cell := range g.counts {
				if first {
					minY, maxY, first = cell.y, cell.y, false
				}
				minY = min(minY, cell.y)
				maxY = max(maxY, cell.y)
			}
			if g.hasBounds {
				minY = min(minY, int(math.Floor(float64(g.bounds.Min[1])/float64(resolution))))
				maxY = max(maxY, int(math.Ceil(float64(g.bounds.Max[1])/float64(resolution)))-1)
			}
			width = max(width, maxY-minY+1)
			if width > 256 {
				return nil, "", fmt.Errorf("POI page exceeds bounded packing side")
			}
			origin := [3]float32{float32(key.x) * spans[level], float32(minY) * resolution, float32(key.z) * spans[level]}
			cube := content.StreamPageBounds{Min: origin, Max: [3]float32{origin[0] + float32(width)*resolution, origin[1] + float32(width)*resolution, origin[2] + float32(width)*resolution}}
			bounds := cube
			if g.hasBounds {
				bounds = islandUnionBounds(bounds, g.bounds)
			}
			if bounds.Min[0] < -o.CoverageSpan/2 || bounds.Max[0] > o.CoverageSpan/2 || bounds.Min[2] < -o.CoverageSpan/2 || bounds.Max[2] > o.CoverageSpan/2 {
				return nil, "", fmt.Errorf("POI cube outside padded coverage")
			}
			path := fmt.Sprintf("page_%d_%d_%d.gkchunk", level, key.x, key.z)
			chunk := &content.ImportedWorldChunkDef{WorldID: d.WorldID, SchemaVersion: 1, ChunkSize: width, VoxelResolution: resolution}
			type choice struct {
				pair  pagePair
				count uint32
			}
			winners := map[[3]int]choice{}
			for cell, n := range g.counts {
				pos := [3]int{cell.x - int(math.Round(float64(origin[0])/float64(resolution))), cell.y - minY, cell.z - int(math.Round(float64(origin[2])/float64(resolution)))}
				if pos[0] < 0 || pos[1] < 0 || pos[2] < 0 || pos[0] >= width || pos[1] >= width || pos[2] >= width {
					return nil, "", fmt.Errorf("coarse voxel outside declared grid")
				}
				winner, ok := winners[pos]
				if !ok || n > winner.count || n == winner.count && (cell.pair.material < winner.pair.material || cell.pair.material == winner.pair.material && cell.pair.value < winner.pair.value) {
					winners[pos] = choice{cell.pair, n}
				}
			}
			for pos, w := range winners {
				chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: pos[0], Y: pos[1], Z: pos[2], Value: w.pair.value, MaterialValue: w.pair.material})
			}
			sort.Slice(chunk.Voxels, func(i, j int) bool { return pageVoxelLess(chunk.Voxels[i], chunk.Voxels[j]) })
			chunk.NonEmptyVoxelCount = len(chunk.Voxels)
			payloads[path] = chunk
			index := uint32(len(d.Pages))
			d.Pages = append(d.Pages, content.StreamPageDef{Level: level, BoundsMin: bounds.Min, BoundsMax: bounds.Max, ChildPageIndices: g.children, Payload: content.StreamPagePayloadDef{Kind: content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1, Path: path, WorldOrigin: origin, ChunkSize: width, VoxelResolution: resolution}})
			if level == 3 {
				d.RootPageIndices = append(d.RootPageIndices, index)
			} else {
				parent := group(islandPOIGroupKey{level + 1, pageDiv(key.x, 4), pageDiv(key.z, 4)})
				parent.children = append(parent.children, index)
				if parent.hasBounds {
					parent.bounds = islandUnionBounds(parent.bounds, bounds)
				} else {
					parent.bounds, parent.hasBounds = bounds, true
				}
			}
		}
	}
	entryIndices := map[content.TerrainChunkCoordDef]uint32{}
	for i, e := range d.Entries {
		entryIndices[e.Coord] = uint32(i)
	}
	sectors := source.Sectors
	if len(sectors) == 0 {
		sectors = content.BuildImportedWorldSectors(source.Entries, source.ChunkSize, source.VoxelResolution, content.DefaultImportedWorldSectorTargetWorldSize)
	}
	sectorIndices := map[content.TerrainChunkCoordDef]uint32{}
	for i, s := range sectors {
		if _, exists := sectorIndices[s.Coord]; exists {
			return nil, "", fmt.Errorf("duplicate visibility sector")
		}
		sectorIndices[s.Coord] = uint32(i)
	}
	refs := func(coords []content.TerrainChunkCoordDef, index map[content.TerrainChunkCoordDef]uint32) ([]uint32, error) {
		out := []uint32{}
		seen := map[uint32]bool{}
		for _, c := range coords {
			i, ok := index[c]
			if !ok || seen[i] {
				return nil, fmt.Errorf("invalid sector membership")
			}
			seen[i] = true
			out = append(out, i)
		}
		return out, nil
	}
	for _, s := range sectors {
		full, err := refs(s.FullChunkRefs, entryIndices)
		if err != nil {
			return nil, "", err
		}
		visible, err := refs(s.VisibleSectorRefs, sectorIndices)
		if err != nil {
			return nil, "", err
		}
		adjacent, err := refs(s.AdjacentSectorRefs, sectorIndices)
		if err != nil {
			return nil, "", err
		}
		bounds := content.StreamPageBounds{Min: s.BoundsMin, Max: s.BoundsMax}
		if s.BoundsMin == ([3]float32{}) && s.BoundsMax == ([3]float32{}) {
			for i, index := range full {
				if i == 0 {
					bounds = leafBounds[index]
				} else {
					bounds = islandUnionBounds(bounds, leafBounds[index])
				}
			}
		}
		d.IndexedSectors = append(d.IndexedSectors, content.ImportedWorldSectorV3Def{Coord: s.Coord, BoundsMin: bounds.Min, BoundsMax: bounds.Max, FullChunkIndices: full, VisibleSectorIndices: visible, AdjacentSectorIndices: adjacent, VisibilityID: s.VisibilityID, SourceLeafIDs: s.SourceLeafIDs, Tags: s.Tags})
	}
	d.Sectors = nil
	return d, hex.EncodeToString(hash.Sum(nil)), nil
}

func islandUnionBounds(a, b content.StreamPageBounds) content.StreamPageBounds {
	for i := 0; i < 3; i++ {
		a.Min[i] = min(a.Min[i], b.Min[i])
		a.Max[i] = max(a.Max[i], b.Max[i])
	}
	return a
}
