package derived

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
)

// ImportedWorldLandmarkDef supplies an authored opaque replacement from its minimum tier upward.
type ImportedWorldLandmarkDef struct {
	ID              string
	MinimumLevel    uint8
	WorldOrigin     [3]float32
	VoxelResolution float32
	Voxels          []content.ImportedWorldVoxelDef
}

// ImportedWorldPageBakeOptions bounds an offline, independent X/Z page forest.
// Zero values select the documented profile. Limits cannot exceed the hard caps.
type ImportedWorldPageBakeOptions struct {
	RegionalSpan, MacroSpan, RootSpan                   float32
	RegionalResolution, MacroResolution, RootResolution float32
	MaxPayloadSide, MaxPages                            int
	Landmarks                                           []ImportedWorldLandmarkDef
}

// ImportedWorldPageBake owns a draft and its source/payload copies. Mutation
// invalidates its generation identity; rebuilding is required before publication.
type ImportedWorldPageBake struct {
	Manifest    *content.ImportedWorldDef
	Chunks      map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef
	PageChunks  map[string]*content.ImportedWorldChunkDef
	fingerprint string
}

type pagePoint struct {
	p               [3]float64
	value, material uint8
	authored        bool
}
type pageGroup struct {
	x, z     int
	children []uint32
	points   []pagePoint
}
type pagePair struct{ value, material uint8 }

func pageClone[T any](v T) (T, error) {
	var out T
	b, e := json.Marshal(v)
	if e != nil {
		return out, e
	}
	e = json.Unmarshal(b, &out)
	return out, e
}
func pageFinite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func pageDiv(v, d int) int {
	q, r := v/d, v%d
	if r < 0 {
		q--
	}
	return q
}
func pageCoordLess(a, b content.TerrainChunkCoordDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}
func pageVoxelLess(a, b content.ImportedWorldVoxelDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
}
func pageIntegerRatio(a, b float64) bool {
	r := a / b
	return pageFinite(r) && r >= 1 && math.Abs(r-math.Round(r)) <= 1e-5*math.Max(1, math.Abs(r))
}

const importedWorldPageBakeFormatVersion = 1

func pagePowerOfTwoRatio(a, b float64) bool {
	if !pageIntegerRatio(a, b) {
		return false
	}
	ratio := math.Round(a / b)
	if ratio >= float64(int(^uint(0)>>1)) {
		return false
	}
	n := uint64(ratio)
	return n > 0 && n&(n-1) == 0
}

func pageHash(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func pageFingerprint(b *ImportedWorldPageBake) (string, error) {
	coords := make([]content.TerrainChunkCoordDef, 0, len(b.Chunks))
	for c := range b.Chunks {
		coords = append(coords, c)
	}
	sort.Slice(coords, func(i, j int) bool { return pageCoordLess(coords[i], coords[j]) })
	ordered := make([]*content.ImportedWorldChunkDef, 0, len(coords))
	for _, c := range coords {
		ordered = append(ordered, b.Chunks[c])
	}
	return pageHash(struct {
		Manifest *content.ImportedWorldDef
		Chunks   []*content.ImportedWorldChunkDef
		Pages    map[string]*content.ImportedWorldChunkDef
	}{b.Manifest, ordered, b.PageChunks})
}

// BuildImportedWorldPageBake constructs each tier directly from original source
// voxel centers. It does not change collision/source chunks or admit live v3 worlds.
func BuildImportedWorldPageBake(source *content.ImportedWorldDef, chunks map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, options ImportedWorldPageBakeOptions) (*ImportedWorldPageBake, error) {
	fail := func(s string) (*ImportedWorldPageBake, error) { return nil, fmt.Errorf("page bake: %s", s) }
	if source == nil || source.SchemaVersion < 0 || source.SchemaVersion > 2 || source.WorldID == "" {
		return fail("invalid source identity/version")
	}
	o, e := pageClone(options)
	if e != nil {
		return nil, e
	}
	spans := []float32{o.RegionalSpan, o.MacroSpan, o.RootSpan}
	resolutions := []float32{o.RegionalResolution, o.MacroResolution, o.RootResolution}
	defaultsSpan := []float32{128, 512, 2048}
	defaultsRes := []float32{1, 4, 16}
	for i := range spans {
		if spans[i] == 0 {
			spans[i] = defaultsSpan[i]
		}
		if resolutions[i] == 0 {
			resolutions[i] = defaultsRes[i]
		}
	}
	if o.MaxPayloadSide == 0 {
		o.MaxPayloadSide = content.ImportedWorldPageMaxPayloadSide
	}
	if o.MaxPages == 0 {
		o.MaxPages = 4096
	}
	if o.MaxPayloadSide < 1 || o.MaxPayloadSide > content.ImportedWorldPageMaxPayloadSide || o.MaxPages < 1 || o.MaxPages > 4096 || source.ChunkSize < 1 || source.ChunkSize > o.MaxPayloadSide || !pageFinite(float64(source.VoxelResolution)) || source.VoxelResolution <= 0 {
		return fail("invalid grid/budget")
	}
	sourceSide := float64(float32(source.ChunkSize) * source.VoxelResolution)
	for i := range spans {
		if float64(spans[i])/sourceSide >= float64(int(^uint(0)>>1)) || !pageFinite(float64(spans[i])) || !pageFinite(float64(resolutions[i])) || !pageIntegerRatio(float64(spans[i]), sourceSide) || !pageIntegerRatio(float64(spans[i]), float64(resolutions[i])) || resolutions[i] < source.VoxelResolution || !pageIntegerRatio(float64(resolutions[i]), float64(source.VoxelResolution)) {
			return fail("unaligned tier grid")
		}
		if i > 0 && (!pagePowerOfTwoRatio(float64(spans[i]), float64(spans[i-1])) || !pageIntegerRatio(float64(resolutions[i]), float64(resolutions[i-1]))) {
			return fail("unaligned parent grid")
		}
	}
	o.RegionalSpan, o.MacroSpan, o.RootSpan = spans[0], spans[1], spans[2]
	o.RegionalResolution, o.MacroResolution, o.RootResolution = resolutions[0], resolutions[1], resolutions[2]
	if len(source.Entries) > o.MaxPages {
		return fail("page count budget")
	}
	d, e := pageClone(source)
	if e != nil {
		return nil, e
	}
	d.PageIndex = nil
	sort.Slice(d.Entries, func(i, j int) bool { return pageCoordLess(d.Entries[i].Coord, d.Entries[j].Coord) })
	sort.Slice(d.Sectors, func(i, j int) bool { return pageCoordLess(d.Sectors[i].Coord, d.Sectors[j].Coord) })
	for i := range d.Sectors {
		for _, refs := range [][]content.TerrainChunkCoordDef{d.Sectors[i].FullChunkRefs, d.Sectors[i].VisibleSectorRefs, d.Sectors[i].AdjacentSectorRefs} {
			sort.Slice(refs, func(a, b int) bool { return pageCoordLess(refs[a], refs[b]) })
		}
	}
	materials := map[uint8]content.ImportedWorldMaterialDef{}
	for _, list := range [][]content.ImportedWorldMaterialDef{d.SourceMaterials, d.Materials} {
		seen := map[uint8]bool{}
		for _, m := range list {
			if seen[m.PaletteIndex] || !pageFinite(float64(m.Transparency)) {
				return fail("ambiguous material palette")
			}
			seen[m.PaletteIndex] = true
			materials[m.PaletteIndex] = m
		}
	}
	opaque := func(v content.ImportedWorldVoxelDef) bool {
		m := materials[content.ImportedWorldVoxelMaterialValue(v)]
		return !m.Transparent && m.Transparency <= 0 && m.Kind != "water" && m.Kind != "glass" && m.Kind != "transparent"
	}
	b := &ImportedWorldPageBake{Manifest: d, Chunks: map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{}, PageChunks: map[string]*content.ImportedWorldChunkDef{}}
	var points []pagePoint
	leafBounds := make([]content.StreamPageBounds, len(d.Entries))
	for i, entry := range d.Entries {
		if _, ok := b.Chunks[entry.Coord]; ok {
			return fail("duplicate source entry")
		}
		c := chunks[entry.Coord]
		if c == nil || c.WorldID != d.WorldID || c.SchemaVersion != 1 || c.Coord != entry.Coord || c.ChunkSize != d.ChunkSize || c.VoxelResolution != d.VoxelResolution || c.NonEmptyVoxelCount != entry.NonEmptyVoxelCount {
			return fail("source chunk mismatch")
		}
		c, e = pageClone(c)
		if e != nil {
			return nil, e
		}
		c.PayloadHash = ""
		c.PayloadSizeBytes = 0
		c.PayloadKind = ""
		c.EmbeddedAux = nil
		sort.Slice(c.Voxels, func(i, j int) bool { return pageVoxelLess(c.Voxels[i], c.Voxels[j]) })
		count := 0
		for j, v := range c.Voxels {
			if v.X < 0 || v.Y < 0 || v.Z < 0 || v.X >= c.ChunkSize || v.Y >= c.ChunkSize || v.Z >= c.ChunkSize || (j > 0 && v.X == c.Voxels[j-1].X && v.Y == c.Voxels[j-1].Y && v.Z == c.Voxels[j-1].Z) {
				return fail("invalid/duplicate source cell")
			}
			if v.Value != 0 {
				count++
			}
		}
		if count != c.NonEmptyVoxelCount {
			return fail("source cell count mismatch")
		}
		origin := [3]float32{float32(c.Coord.X) * float32(sourceSide), float32(c.Coord.Y) * float32(sourceSide), float32(c.Coord.Z) * float32(sourceSide)}
		bounds := content.StreamPageBounds{Min: origin}
		for axis := 0; axis < 3; axis++ {
			bounds.Max[axis] = origin[axis] + float32(sourceSide)
			if !pageFinite(float64(bounds.Max[axis])) || !(bounds.Max[axis] > origin[axis]) || origin[axis]+c.VoxelResolution == origin[axis] {
				return fail("lost source coordinate precision")
			}
		}
		// Conservative coverage also contains fused multiply-add endpoint rounding.
		for axis := 0; axis < 3; axis++ {
			bounds.Max[axis] = math.Nextafter32(bounds.Max[axis], float32(math.Inf(1)))
			if !pageFinite(float64(bounds.Max[axis])) {
				return fail("source coverage overflow")
			}
		}
		leafBounds[i] = bounds
		b.Chunks[c.Coord] = c
		for _, v := range c.Voxels {
			if v.Value == 0 || !opaque(v) {
				continue
			}
			p := [3]float64{}
			for axis, cell := range []int{v.X, v.Y, v.Z} {
				p[axis] = float64(origin[axis]) + (float64(cell)+.5)*float64(c.VoxelResolution)
			}
			points = append(points, pagePoint{p: p, value: v.Value, material: content.ImportedWorldVoxelMaterialValue(v)})
		}
	}
	if len(chunks) != len(b.Chunks) {
		return fail("unreferenced source chunk")
	}
	sort.Slice(o.Landmarks, func(i, j int) bool { return o.Landmarks[i].ID < o.Landmarks[j].ID })
	var landmarks []struct {
		level uint8
		point pagePoint
	}
	for i, l := range o.Landmarks {
		if l.ID == "" || (i > 0 && l.ID == o.Landmarks[i-1].ID) || l.MinimumLevel < 1 || l.MinimumLevel > 3 || !pageFinite(float64(l.VoxelResolution)) || l.VoxelResolution <= 0 {
			return fail("invalid landmark")
		}
		sort.Slice(o.Landmarks[i].Voxels, func(a, b int) bool { return pageVoxelLess(o.Landmarks[i].Voxels[a], o.Landmarks[i].Voxels[b]) })
		for _, v := range l.Voxels {
			if v.X < 0 || v.Y < 0 || v.Z < 0 || v.Value == 0 || !opaque(v) {
				return fail("invalid/transparent landmark cell")
			}
			p := [3]float64{}
			for axis, cell := range []int{v.X, v.Y, v.Z} {
				p[axis] = float64(l.WorldOrigin[axis]) + (float64(cell)+.5)*float64(l.VoxelResolution)
				if !pageFinite(p[axis]) || math.Abs(p[axis]) >= 1<<24 {
					return fail("landmark coordinate precision")
				}
			}
			landmarks = append(landmarks, struct {
				level uint8
				point pagePoint
			}{l.MinimumLevel, pagePoint{p: p, value: v.Value, material: content.ImportedWorldVoxelMaterialValue(v), authored: true}})
		}
	}
	// Canonical source chunks are serialized as an ordered slice, not struct-key maps.
	orderedChunks := make([]*content.ImportedWorldChunkDef, 0, len(d.Entries))
	for _, entry := range d.Entries {
		orderedChunks = append(orderedChunks, b.Chunks[entry.Coord])
	}
	identity, e := pageHash(struct {
		Source        *content.ImportedWorldDef
		Chunks        []*content.ImportedWorldChunkDef
		Options       ImportedWorldPageBakeOptions
		NormalVersion string
		FormatVersion int
	}{d, orderedChunks, o, content.ImportedWorldNormalBakeVersion, importedWorldPageBakeFormatVersion})
	if e != nil {
		return nil, e
	}
	d.SchemaVersion = 3
	d.SourceHash = identity
	d.Pages = nil
	d.RootPageIndices = nil
	d.IndexedSectors = nil
	// Visibility sectors may independently reference the same visual leaf.
	entryIndex := map[content.TerrainChunkCoordDef]uint32{}
	for i, entry := range d.Entries {
		entryIndex[entry.Coord] = uint32(i)
	}
	sectorIndex := map[content.TerrainChunkCoordDef]uint32{}
	for i, sector := range d.Sectors {
		if _, ok := sectorIndex[sector.Coord]; ok {
			return fail("duplicate visibility sector")
		}
		sectorIndex[sector.Coord] = uint32(i)
	}
	for _, sector := range d.Sectors {
		s := content.ImportedWorldSectorV3Def{Coord: sector.Coord, BoundsMin: sector.BoundsMin, BoundsMax: sector.BoundsMax, VisibilityID: sector.VisibilityID, SourceLeafIDs: sector.SourceLeafIDs, Tags: sector.Tags}
		for _, coord := range sector.FullChunkRefs {
			index, ok := entryIndex[coord]
			if !ok {
				return fail("unknown visibility leaf")
			}
			s.FullChunkIndices = append(s.FullChunkIndices, index)
		}
		for _, coord := range sector.VisibleSectorRefs {
			index, ok := sectorIndex[coord]
			if !ok {
				return fail("unknown PVS sector")
			}
			s.VisibleSectorIndices = append(s.VisibleSectorIndices, index)
		}
		for _, coord := range sector.AdjacentSectorRefs {
			index, ok := sectorIndex[coord]
			if !ok {
				return fail("unknown adjacent sector")
			}
			s.AdjacentSectorIndices = append(s.AdjacentSectorIndices, index)
		}
		if s.BoundsMin == ([3]float32{}) && s.BoundsMax == ([3]float32{}) && len(s.FullChunkIndices) > 0 {
			bounds := leafBounds[s.FullChunkIndices[0]]
			for _, j := range s.FullChunkIndices[1:] {
				for a := 0; a < 3; a++ {
					bounds.Min[a] = min(bounds.Min[a], leafBounds[j].Min[a])
					bounds.Max[a] = max(bounds.Max[a], leafBounds[j].Max[a])
				}
			}
			s.BoundsMin, s.BoundsMax = bounds.Min, bounds.Max
		}
		d.IndexedSectors = append(d.IndexedSectors, s)
	}
	d.Sectors = nil
	for i := range d.Entries {
		entry := &d.Entries[i]
		entry.ChunkPath = fmt.Sprintf("pages/%s/full_%d_%d_%d.gkchunk", identity, entry.Coord.X, entry.Coord.Y, entry.Coord.Z)
		entry.PayloadHash = ""
		entry.PayloadSizeBytes = 0
		entry.PayloadKind = ""
		entry.Aux = nil
		bounds := leafBounds[i]
		d.Pages = append(d.Pages, content.StreamPageDef{Level: 0, BoundsMin: bounds.Min, BoundsMax: bounds.Max, LeafEntryIndices: []uint32{uint32(i)}, Payload: content.StreamPagePayloadDef{Path: entry.ChunkPath, WorldOrigin: bounds.Min, ChunkSize: d.ChunkSize, VoxelResolution: d.VoxelResolution}})
	}
	for tier := 0; tier < 3; tier++ {
		level := uint8(tier + 1)
		span, res := float64(spans[tier]), float64(resolutions[tier])
		groups := map[[2]int]*pageGroup{}
		groupFor := func(x, z int) *pageGroup {
			k := [2]int{x, z}
			g := groups[k]
			if g == nil {
				g = &pageGroup{x: x, z: z}
				groups[k] = g
			}
			return g
		}
		if tier == 0 {
			ratio := int(math.Round(span / sourceSide))
			for i, entry := range d.Entries {
				groupFor(pageDiv(entry.Coord.X, ratio), pageDiv(entry.Coord.Z, ratio)).children = append(groupFor(pageDiv(entry.Coord.X, ratio), pageDiv(entry.Coord.Z, ratio)).children, uint32(i))
			}
		} else {
			for i, p := range d.Pages {
				if p.Level != level-1 {
					continue
				}
				x, z := int(math.Floor(float64(p.Payload.WorldOrigin[0])/span)), int(math.Floor(float64(p.Payload.WorldOrigin[2])/span))
				groupFor(x, z).children = append(groupFor(x, z).children, uint32(i))
			}
		}
		for _, p := range points {
			groupFor(int(math.Floor(p.p[0]/span)), int(math.Floor(p.p[2]/span))).points = append(groupFor(int(math.Floor(p.p[0]/span)), int(math.Floor(p.p[2]/span))).points, p)
		}
		for _, l := range landmarks {
			if level >= l.level {
				p := l.point
				groupFor(int(math.Floor(p.p[0]/span)), int(math.Floor(p.p[2]/span))).points = append(groupFor(int(math.Floor(p.p[0]/span)), int(math.Floor(p.p[2]/span))).points, p)
			}
		}
		if len(d.Pages)+len(groups) > o.MaxPages {
			return fail("page count budget")
		}
		keys := make([][2]int, 0, len(groups))
		for k := range groups {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		for _, key := range keys {
			g := groups[key]
			origin := [3]float32{float32(float64(g.x) * span), 0, float32(float64(g.z) * span)}
			minY, maxY := 0.0, 0.0
			haveY := false
			for _, j := range g.children {
				p := d.Pages[j]
				if !haveY || float64(p.Payload.WorldOrigin[1]) < minY {
					minY = float64(p.Payload.WorldOrigin[1])
				}
				if !haveY || float64(p.Payload.WorldOrigin[1]+float32(p.Payload.ChunkSize)*p.Payload.VoxelResolution) > maxY {
					maxY = float64(p.Payload.WorldOrigin[1] + float32(p.Payload.ChunkSize)*p.Payload.VoxelResolution)
				}
				haveY = true
			}
			for _, p := range g.points {
				if !haveY || p.p[1] < minY {
					minY = p.p[1]
				}
				if !haveY || p.p[1] > maxY {
					maxY = p.p[1]
				}
				haveY = true
			}
			origin[1] = float32(math.Floor(minY/res) * res)
			sideFloat := math.Ceil(math.Max(span, maxY-float64(origin[1])) / res)
			// Coverage padding is separate from physical rows. Every point needs
			// only its containing row, including an exact boundary point.
			for _, point := range g.points {
				sideFloat = math.Max(sideFloat, math.Floor((point.p[1]-float64(origin[1]))/res)+1)
			}
			if !pageFinite(sideFloat) || sideFloat > float64(o.MaxPayloadSide) {
				return fail("payload side budget")
			}
			side := int(sideFloat)
			if side < 1 || side > o.MaxPayloadSide {
				return fail("payload side budget")
			}
			votes := map[[3]int]map[pagePair]int{}
			overrides := map[[3]int]pagePair{}
			for _, p := range g.points {
				cell := [3]int{}
				for axis := 0; axis < 3; axis++ {
					cell[axis] = int(math.Floor((p.p[axis] - float64(origin[axis])) / res))
					if cell[axis] < 0 || cell[axis] >= side {
						return fail("point outside page cube")
					}
				}
				pair := pagePair{p.value, p.material}
				if p.authored {
					if old, ok := overrides[cell]; ok && old != pair {
						return fail("conflicting authored coarse cells")
					}
					overrides[cell] = pair
				} else {
					if votes[cell] == nil {
						votes[cell] = map[pagePair]int{}
					}
					votes[cell][pair]++
				}
			}
			cells := map[[3]int]pagePair{}
			for cell, v := range votes {
				best := pagePair{}
				count := -1
				for pair, n := range v {
					if n > count || (n == count && (pair.material < best.material || (pair.material == best.material && pair.value < best.value))) {
						best, count = pair, n
					}
				}
				cells[cell] = best
			}
			for cell, pair := range overrides {
				cells[cell] = pair
			}
			c := &content.ImportedWorldChunkDef{WorldID: d.WorldID, SchemaVersion: 1, ChunkSize: side, VoxelResolution: float32(res), NonEmptyVoxelCount: len(cells)}
			for cell, pair := range cells {
				c.Voxels = append(c.Voxels, content.ImportedWorldVoxelDef{X: cell[0], Y: cell[1], Z: cell[2], Value: pair.value, MaterialValue: pair.material})
			}
			sort.Slice(c.Voxels, func(i, j int) bool { return pageVoxelLess(c.Voxels[i], c.Voxels[j]) })
			path := fmt.Sprintf("pages/%s/tier_%d_%d_%d.gkchunk", identity, level, g.x, g.z)
			b.PageChunks[path] = c
			bounds := content.StreamPageBounds{Min: origin}
			for axis := 0; axis < 3; axis++ {
				bounds.Max[axis] = origin[axis] + float32(side)*float32(res)
				if !pageFinite(float64(bounds.Max[axis])) || bounds.Max[axis] <= origin[axis] {
					return fail("page coordinate precision")
				}
			}
			for _, j := range g.children {
				p := d.Pages[j]
				for axis := 0; axis < 3; axis++ {
					bounds.Min[axis] = min(bounds.Min[axis], p.BoundsMin[axis])
					bounds.Max[axis] = max(bounds.Max[axis], p.BoundsMax[axis])
				}
			}
			index := uint32(len(d.Pages))
			d.Pages = append(d.Pages, content.StreamPageDef{Level: level, BoundsMin: bounds.Min, BoundsMax: bounds.Max, ChildPageIndices: g.children, Payload: content.StreamPagePayloadDef{Path: path, WorldOrigin: origin, ChunkSize: side, VoxelResolution: float32(res)}})
			if level == 3 {
				d.RootPageIndices = append(d.RootPageIndices, index)
			}
		}
	}
	if len(d.Pages) > o.MaxPages {
		return fail("page count budget")
	}
	b.fingerprint, e = pageFingerprint(b)
	if e != nil {
		return nil, e
	}
	return b, nil
}
