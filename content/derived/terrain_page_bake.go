package derived

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
)

const terrainPageBakeFormatVersion = 1
const terrainPageMaxSources = 4096
const terrainPageMaxPages = 16384

// TerrainPageBakeOptions separates source-data spacing from target visual quality.
// Zero values select the documented regional/macro/root profile.
type TerrainPageBakeOptions struct {
	RegionalSpan, MacroSpan, RootSpan                                  float32
	RegionalSampleSpacing, MacroSampleSpacing, RootSampleSpacing       float32
	RegionalVoxelResolution, MacroVoxelResolution, RootVoxelResolution float32
	MaxPages, MaxSourceTiles                                           int
}

// TerrainPageBake owns backing source tiles and an independent visual page forest.
// Mutating its public draft invalidates the sealed generation until rebuilt.
type TerrainPageBake struct {
	Manifest    *content.TerrainChunkManifestDef
	SourceTiles map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef
	PageTiles   map[string]*content.TerrainHeightTileDef
	fingerprint string
}

func terrainPageClone(t *content.TerrainHeightTileDef) *content.TerrainHeightTileDef {
	if t == nil {
		return nil
	}
	c := *t
	c.HeightSamples = append([]uint16(nil), t.HeightSamples...)
	c.SurfaceMask = append([]byte(nil), t.SurfaceMask...)
	c.OutdoorNavExclusionMask = append([]byte(nil), t.OutdoorNavExclusionMask...)
	return &c
}
func terrainCoordLess(a, b content.TerrainChunkCoordDef) bool {
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	if a.X != b.X {
		return a.X < b.X
	}
	return a.Y < b.Y
}
func terrainHashMetadata(h hash.Hash, v any) error {
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(raw)))
	h.Write(length[:])
	h.Write(raw)
	return nil
}
func terrainHashTile(h hash.Hash, t *content.TerrainHeightTileDef) error {
	if e := terrainHashMetadata(h, t); e != nil {
		return e
	}
	if t == nil {
		return nil
	}
	var length [8]byte
	binary.LittleEndian.PutUint64(length[:], uint64(len(t.HeightSamples)))
	h.Write(length[:])
	var buf [8192]byte
	for offset := 0; offset < len(t.HeightSamples); {
		n := min(len(buf)/2, len(t.HeightSamples)-offset)
		for i := 0; i < n; i++ {
			binary.LittleEndian.PutUint16(buf[2*i:], t.HeightSamples[offset+i])
		}
		h.Write(buf[:2*n])
		offset += n
	}
	for _, data := range [][]byte{t.SurfaceMask, t.OutdoorNavExclusionMask} {
		binary.LittleEndian.PutUint64(length[:], uint64(len(data)))
		h.Write(length[:])
		h.Write(data)
	}
	return nil
}
func terrainPageFingerprint(b *TerrainPageBake) (string, error) {
	h := sha256.New()
	if e := terrainHashMetadata(h, b.Manifest); e != nil {
		return "", e
	}
	coords := make([]content.TerrainChunkCoordDef, 0, len(b.SourceTiles))
	for c := range b.SourceTiles {
		coords = append(coords, c)
	}
	sort.Slice(coords, func(i, j int) bool { return terrainCoordLess(coords[i], coords[j]) })
	if e := terrainHashMetadata(h, coords); e != nil {
		return "", e
	}
	for _, c := range coords {
		if e := terrainHashTile(h, b.SourceTiles[c]); e != nil {
			return "", e
		}
	}
	paths := make([]string, 0, len(b.PageTiles))
	for path := range b.PageTiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if e := terrainHashMetadata(h, paths); e != nil {
		return "", e
	}
	for _, path := range paths {
		if e := terrainHashTile(h, b.PageTiles[path]); e != nil {
			return "", e
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func terrainValidCell(t *content.TerrainHeightTileDef, i int) bool {
	return len(t.SurfaceMask) == 0 || t.SurfaceMask[i/8]&(1<<uint(i%8)) != 0
}
func terrainCellMultiply(c, size int) (int, bool) {
	if c > math.MaxInt/size || c < math.MinInt/size {
		return 0, false
	}
	v := c * size
	if v > math.MaxInt-size {
		return 0, false
	}
	return v, true
}

// terrainGridMultiple validates the stored float32 lattice by reconstructing
// the span, rather than giving large fractional ratios a relative tolerance.
func terrainGridMultiple(a, b float32) (int, bool) {
	if !pageFinite(float64(a)) || !pageFinite(float64(b)) || a <= 0 || b <= 0 {
		return 0, false
	}
	n := math.Round(float64(a) / float64(b))
	if !pageFinite(n) || n < 1 || n >= float64(math.MaxInt) || float32(n)*b != a {
		return 0, false
	}
	return int(n), true
}

// Round the upper endpoint outward before deriving the stored scale. This also
// reserves enough range for float32 offset+scale addition, not just float64 math.
func terrainGlobalCalibration(lo, hi float64) (float32, float32, bool) {
	offset, end := float32(lo), float32(hi)
	if float64(end) < hi {
		end = math.Nextafter32(end, float32(math.Inf(1)))
	}
	scale := float32(float64(end) - float64(offset))
	if float64(offset)+float64(scale) < float64(end) || float64(offset+scale) < hi {
		scale = math.Nextafter32(scale, float32(math.Inf(1)))
	}
	valid := pageFinite(float64(offset)) && pageFinite(float64(scale)) && scale > 0 && pageFinite(float64(offset+scale)) && offset+scale > offset && float64(offset) <= lo && float64(offset)+float64(scale) >= hi && float64(offset+scale) >= hi
	return offset, scale, valid
}

// BuildTerrainPageBake averages original valid source cells independently at
// each visual tier. It never materializes filled voxel columns or source points.
func BuildTerrainPageBake(source *content.TerrainChunkManifestDef, tiles map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef, options TerrainPageBakeOptions) (*TerrainPageBake, error) {
	fail := func(s string) (*TerrainPageBake, error) { return nil, fmt.Errorf("terrain page bake: %s", s) }
	if source == nil || source.SchemaVersion != 3 || len(source.Pages) > 0 || len(source.RootPageIndices) > 0 {
		return fail("raw tiled source required")
	}
	if e := content.ValidateTerrainHeightTileManifest(source); e != nil {
		return nil, e
	}
	o := options
	if o.MaxPages == 0 {
		o.MaxPages = terrainPageMaxPages
	}
	if o.MaxSourceTiles == 0 {
		o.MaxSourceTiles = terrainPageMaxSources
	}
	if o.MaxPages < 1 || o.MaxPages > terrainPageMaxPages || o.MaxSourceTiles < 1 || o.MaxSourceTiles > terrainPageMaxSources || len(source.Entries) > o.MaxSourceTiles || len(tiles) != len(source.Entries) {
		return fail("source/page budget or tile count")
	}
	spans := []float32{o.RegionalSpan, o.MacroSpan, o.RootSpan}
	spacing := []float32{o.RegionalSampleSpacing, o.MacroSampleSpacing, o.RootSampleSpacing}
	visual := []float32{o.RegionalVoxelResolution, o.MacroVoxelResolution, o.RootVoxelResolution}
	defaultSpans := []float32{128, 512, 2048}
	defaultData := []float32{2, 4, 16}
	defaultVisual := []float32{1, 4, 16}
	spanCells := [3]int{}
	dataCells := [3]int{}
	for i := range spans {
		if spans[i] == 0 {
			spans[i] = defaultSpans[i]
		}
		if spacing[i] == 0 {
			spacing[i] = defaultData[i]
		}
		if visual[i] == 0 {
			visual[i] = defaultVisual[i]
		}
		spanCount, spanOK := terrainGridMultiple(spans[i], source.VoxelResolution)
		dataCount, dataOK := terrainGridMultiple(spacing[i], source.VoxelResolution)
		width, widthOK := terrainGridMultiple(spans[i], spacing[i])
		_, visualOK := terrainGridMultiple(spacing[i], visual[i])
		// Large rounded integers lose units when cast to float32. Keep the
		// visual ratio's fractional allowance bounded, as in strict metadata.
		visualRatio := float64(spacing[i]) / float64(visual[i])
		visualTolerance := math.Min(1e-6*math.Max(1, math.Abs(visualRatio)), 1e-4)
		visualOK = visualOK && math.Abs(visualRatio-math.Round(visualRatio)) <= visualTolerance
		if !spanOK || !dataOK || !widthOK || !visualOK || width > 128 || spanCount%dataCount != 0 || spanCount/dataCount != width {
			return fail("invalid tier lattice")
		}
		if i > 0 {
			parentRatio, aligned := terrainGridMultiple(spans[i], spans[i-1])
			if !aligned || parentRatio&(parentRatio-1) != 0 || spanCount%spanCells[i-1] != 0 || spanCount/spanCells[i-1] != parentRatio {
				return fail("parent spans require powers of two on the source lattice")
			}
		}
		spanCells[i], dataCells[i] = spanCount, dataCount
	}
	o.RegionalSpan, o.MacroSpan, o.RootSpan = spans[0], spans[1], spans[2]
	o.RegionalSampleSpacing, o.MacroSampleSpacing, o.RootSampleSpacing = spacing[0], spacing[1], spacing[2]
	o.RegionalVoxelResolution, o.MacroVoxelResolution, o.RootVoxelResolution = visual[0], visual[1], visual[2]
	d, e := pageClone(source)
	if e != nil {
		return nil, e
	}
	d.PageIndex = nil
	sort.Slice(d.Entries, func(i, j int) bool { return terrainCoordLess(d.Entries[i].Coord, d.Entries[j].Coord) })
	b := &TerrainPageBake{Manifest: d, SourceTiles: map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}, PageTiles: map[string]*content.TerrainHeightTileDef{}}
	globalMin, globalMax := 0.0, 0.0
	for i, entry := range d.Entries {
		tile := tiles[entry.Coord]
		if tile == nil {
			return fail("missing source tile")
		}
		if e := content.ValidateTerrainHeightTileReference(entry, tile); e != nil {
			return nil, e
		}
		if _, ok := b.SourceTiles[entry.Coord]; ok {
			return fail("duplicate source tile")
		}
		for _, c := range []int{entry.Coord.X, entry.Coord.Z} {
			if _, ok := terrainCellMultiply(c, d.ChunkSize); !ok {
				return fail("source cell coordinate overflow")
			}
		}
		for _, axis := range []int{0, 2} {
			origin := tile.WorldOrigin[axis]
			if origin+tile.SampleSpacing == origin || origin+tile.SampleSpacing*.5 == origin {
				return fail("source cell coordinate precision")
			}
		}
		lo, hi := float64(tile.HeightOffset), float64(tile.HeightOffset)+float64(tile.HeightScale)
		if !pageFinite(hi) || hi <= lo || !pageFinite(float64(tile.HeightOffset+tile.HeightScale)) || tile.HeightOffset+tile.HeightScale <= tile.HeightOffset {
			return fail("source height calibration precision")
		}
		if i == 0 || lo < globalMin {
			globalMin = lo
		}
		if i == 0 || hi > globalMax {
			globalMax = hi
		}
		b.SourceTiles[entry.Coord] = terrainPageClone(tile)
	}
	offset, scale, calibrationOK := terrainGlobalCalibration(globalMin, globalMax)
	if len(d.Entries) > 0 && !calibrationOK {
		return fail("global calibration range")
	}
	h := sha256.New()
	if e := terrainHashMetadata(h, struct {
		Source        *content.TerrainChunkManifestDef
		Options       TerrainPageBakeOptions
		FormatVersion int
	}{d, o, terrainPageBakeFormatVersion}); e != nil {
		return nil, e
	}
	for _, entry := range d.Entries {
		if e := terrainHashTile(h, b.SourceTiles[entry.Coord]); e != nil {
			return nil, e
		}
	}
	generation := hex.EncodeToString(h.Sum(nil))
	d.SourceHash = generation
	for i := range d.Entries {
		entry := &d.Entries[i]
		tile := b.SourceTiles[entry.Coord]
		tile.SourceHash = generation
		entry.SourceHash = generation
		entry.HeightOffset = tile.HeightOffset
		entry.HeightScale = tile.HeightScale
		entry.ChunkPath = fmt.Sprintf("terrain-pages/%s/source_%d_%d.gkchunk", generation, entry.Coord.X, entry.Coord.Z)
	}
	var previous map[[2]int]uint32
	totalPages := 0
	for tier := 0; tier < 3; tier++ {
		groups := map[[2]int]bool{}
		// Only occupied source cells establish visual coverage. No source arrays are
		// expanded into points; this set is bounded by the explicit visual-page cap.
		for _, entry := range d.Entries {
			tile := b.SourceTiles[entry.Coord]
			baseX, _ := terrainCellMultiply(entry.Coord.X, d.ChunkSize)
			baseZ, _ := terrainCellMultiply(entry.Coord.Z, d.ChunkSize)
			for z := 0; z < tile.SampleHeight; z++ {
				for x := 0; x < tile.SampleWidth; x++ {
					if !terrainValidCell(tile, z*tile.SampleWidth+x) {
						continue
					}
					key := [2]int{pageDiv(baseX+x, spanCells[tier]), pageDiv(baseZ+z, spanCells[tier])}
					groups[key] = true
					if totalPages+len(groups) > o.MaxPages {
						return fail("render page budget")
					}
				}
			}
		}
		keys := make([][2]int, 0, len(groups))
		for key := range groups {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][1] != keys[j][1] {
				return keys[i][1] < keys[j][1]
			}
			return keys[i][0] < keys[j][0]
		})
		current := make(map[[2]int]uint32, len(keys))
		childGroups := map[[2]int][]uint32{}
		if tier > 0 {
			ratio := spanCells[tier] / spanCells[tier-1]
			for key, index := range previous {
				parent := [2]int{pageDiv(key[0], ratio), pageDiv(key[1], ratio)}
				childGroups[parent] = append(childGroups[parent], index)
			}
		}
		for _, key := range keys {
			baseX, ok := terrainCellMultiply(key[0], spanCells[tier])
			if !ok {
				return fail("page cell coordinate overflow")
			}
			baseZ, ok := terrainCellMultiply(key[1], spanCells[tier])
			if !ok {
				return fail("page cell coordinate overflow")
			}
			width := int(math.Round(float64(spans[tier]) / float64(spacing[tier])))
			sums := make([]float64, width*width)
			counts := make([]uint32, width*width)
			// A page needs at most 128² aggregates. Iterate only intersecting source
			// tiles, retaining original-cell weights even across differently masked tiles.
			for _, entry := range d.Entries {
				tile := b.SourceTiles[entry.Coord]
				tileX, _ := terrainCellMultiply(entry.Coord.X, d.ChunkSize)
				tileZ, _ := terrainCellMultiply(entry.Coord.Z, d.ChunkSize)
				if tileX >= baseX+spanCells[tier] || tileX+d.ChunkSize <= baseX || tileZ >= baseZ+spanCells[tier] || tileZ+d.ChunkSize <= baseZ {
					continue
				}
				startX, endX := max(baseX, tileX)-tileX, min(baseX+spanCells[tier], tileX+d.ChunkSize)-tileX
				startZ, endZ := max(baseZ, tileZ)-tileZ, min(baseZ+spanCells[tier], tileZ+d.ChunkSize)-tileZ
				for z := startZ; z < endZ; z++ {
					for x := startX; x < endX; x++ {
						i := z*tile.SampleWidth + x
						if !terrainValidCell(tile, i) {
							continue
						}
						outX, outZ := (tileX+x-baseX)/dataCells[tier], (tileZ+z-baseZ)/dataCells[tier]
						j := outZ*width + outX
						if counts[j] == math.MaxUint32 {
							return fail("sample count overflow")
						}
						sums[j] += float64(tile.HeightOffset) + float64(tile.HeightSamples[i])/65535*float64(tile.HeightScale)
						counts[j]++
					}
				}
			}
			payloadSpan64 := float64(width) * float64(spacing[tier])
			payloadSpan := float32(width) * spacing[tier]
			origin := [3]float32{float32(float64(key[0]) * payloadSpan64), 0, float32(float64(key[1]) * payloadSpan64)}
			for _, axis := range []int{0, 2} {
				if !pageFinite(float64(origin[axis])) || origin[axis]+spacing[tier]*.5 == origin[axis] || origin[axis]+payloadSpan <= origin[axis] {
					return fail("page coordinate precision")
				}
			}
			tile := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: generation, Coord: content.TerrainChunkCoordDef{X: key[0], Z: key[1]}, WorldOrigin: origin, SampleWidth: width, SampleHeight: width, SampleSpacing: spacing[tier], HeightOffset: offset, HeightScale: scale, HeightSamples: make([]uint16, width*width), SurfaceMask: make([]byte, (width*width+7)/8)}
			allValid := true
			for i, count := range counts {
				if count == 0 {
					allValid = false
					continue
				}
				mean := sums[i] / float64(count)
				v := math.Round((mean - float64(offset)) / float64(scale) * 65535)
				if !pageFinite(v) {
					return fail("height mean overflow")
				}
				tile.HeightSamples[i] = uint16(math.Max(0, math.Min(65535, v)))
				tile.SurfaceMask[i/8] |= 1 << uint(i%8)
			}
			if allValid {
				tile.SurfaceMask = nil
			}
			if e := content.ValidateTerrainHeightTile(tile); e != nil {
				return nil, e
			}
			path := fmt.Sprintf("terrain-pages/%s/page_%d_%d_%d.gkchunk", generation, tier+1, key[0], key[1])
			b.PageTiles[path] = tile
			page := content.StreamPageDef{Level: uint8(tier + 1), BoundsMin: [3]float32{origin[0], offset, origin[2]}, BoundsMax: [3]float32{origin[0] + payloadSpan, offset + scale, origin[2] + payloadSpan}, Payload: content.StreamPagePayloadDef{Kind: content.TerrainHeightTilePayloadKind, Path: path, WorldOrigin: origin, ChunkSize: width, SampleSpacing: spacing[tier], VoxelResolution: visual[tier], HeightOffset: offset, HeightScale: scale}, ChildPageIndices: childGroups[key]}
			sort.Slice(page.ChildPageIndices, func(i, j int) bool { return page.ChildPageIndices[i] < page.ChildPageIndices[j] })
			for _, child := range page.ChildPageIndices {
				for axis := 0; axis < 3; axis++ {
					page.BoundsMin[axis] = min(page.BoundsMin[axis], d.Pages[child].BoundsMin[axis])
					page.BoundsMax[axis] = max(page.BoundsMax[axis], d.Pages[child].BoundsMax[axis])
				}
			}
			index := uint32(len(d.Pages))
			current[key] = index
			d.Pages = append(d.Pages, page)
			if tier == 2 {
				d.RootPageIndices = append(d.RootPageIndices, index)
			}
		}
		totalPages += len(keys)
		previous = current
	}
	b.fingerprint, e = terrainPageFingerprint(b)
	if e != nil {
		return nil, e
	}
	return b, nil
}
