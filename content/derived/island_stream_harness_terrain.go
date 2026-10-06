package derived

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
)

type islandTerrainKey struct {
	level uint8
	x, z  int
}
type islandTerrainPlan struct {
	sources                     []content.TerrainChunkCoordDef
	pages                       []islandTerrainKey
	refinedRoots, refinedMacros map[[2]int]bool
}

func islandHarnessTerrainPlan(o IslandStreamHarnessOptions, corridors []islandHarnessRect) (*islandTerrainPlan, error) {
	rootWidth := int(o.CoverageSpan / o.RootSpan)
	if rootWidth > o.MaxPages/rootWidth {
		return nil, fmt.Errorf("harness minimum root pages exceed budget")
	}
	p := &islandTerrainPlan{refinedRoots: map[[2]int]bool{}, refinedMacros: map[[2]int]bool{}}
	intersects := func(x, z, span float64) bool {
		for _, r := range corridors {
			if r.intersects(x, z, span) {
				return true
			}
		}
		return false
	}
	macroHalf := int(o.CoverageSpan/o.MacroSpan) / 2
	for z := -macroHalf; z < macroHalf; z++ {
		for x := -macroHalf; x < macroHalf; x++ {
			if intersects(float64(x)*float64(o.MacroSpan), float64(z)*float64(o.MacroSpan), float64(o.MacroSpan)) {
				p.refinedMacros[[2]int{x, z}] = true
				p.refinedRoots[[2]int{pageDiv(x, 4), pageDiv(z, 4)}] = true
			}
		}
	}
	rootHalf := int(o.CoverageSpan/o.RootSpan) / 2
	pageCount := rootWidth*rootWidth + 16*len(p.refinedRoots) + 16*len(p.refinedMacros)
	if pageCount > o.MaxPages {
		return nil, fmt.Errorf("harness terrain partition budget exceeded")
	}
	p.pages = make([]islandTerrainKey, 0, pageCount)
	for z := -rootHalf; z < rootHalf; z++ {
		for x := -rootHalf; x < rootHalf; x++ {
			p.pages = append(p.pages, islandTerrainKey{3, x, z})
			if p.refinedRoots[[2]int{x, z}] {
				for dz := 0; dz < 4; dz++ {
					for dx := 0; dx < 4; dx++ {
						mx, mz := x*4+dx, z*4+dz
						p.pages = append(p.pages, islandTerrainKey{2, mx, mz})
						if p.refinedMacros[[2]int{mx, mz}] {
							for rz := 0; rz < 4; rz++ {
								for rx := 0; rx < 4; rx++ {
									p.pages = append(p.pages, islandTerrainKey{1, mx*4 + rx, mz*4 + rz})
								}
							}
						}
					}
				}
			}
		}
	}
	if len(p.pages) > o.MaxPages {
		return nil, fmt.Errorf("harness terrain page budget exceeded")
	}
	sourceHalf := int(o.CoverageSpan/o.HeightTileSpan) / 2
	for z := -sourceHalf; z < sourceHalf; z++ {
		for x := -sourceHalf; x < sourceHalf; x++ {
			if intersects(float64(x)*float64(o.HeightTileSpan), float64(z)*float64(o.HeightTileSpan), float64(o.HeightTileSpan)) {
				p.sources = append(p.sources, content.TerrainChunkCoordDef{X: x, Z: z})
			}
		}
	}
	if len(p.sources) > o.MaxSourceTiles {
		return nil, fmt.Errorf("harness backing tile budget exceeded")
	}
	sort.Slice(p.pages, func(i, j int) bool {
		a, b := p.pages[i], p.pages[j]
		if a.level != b.level {
			return a.level < b.level
		}
		if a.z != b.z {
			return a.z < b.z
		}
		return a.x < b.x
	})
	return p, nil
}

func islandHarnessTerrain(b *IslandStreamHarness, o IslandStreamHarnessOptions, plan *islandTerrainPlan, full []content.StreamPageBounds, generation string) error {
	d := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "island_streaming_harness", SourceHash: generation, ChunkSize: 128, VoxelResolution: o.HeightSampleSpacing}
	b.Terrain = d
	makeTile := func(coord content.TerrainChunkCoordDef, width int, spacing float32) *content.TerrainHeightTileDef {
		tile := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: generation, Coord: coord, WorldOrigin: [3]float32{float32(coord.X) * float32(width) * spacing, 0, float32(coord.Z) * float32(width) * spacing}, SampleWidth: width, SampleHeight: width, SampleSpacing: spacing, HeightOffset: -64, HeightScale: 128, HeightSamples: make([]uint16, width*width)}
		for z := 0; z < width; z++ {
			for x := 0; x < width; x++ {
				wx, wz := float64(tile.WorldOrigin[0])+(float64(x)+.5)*float64(spacing), float64(tile.WorldOrigin[2])+(float64(z)+.5)*float64(spacing)
				height := float64(0)
				if wx < -float64(o.PlayableSpan)/2 || wx > float64(o.PlayableSpan)/2 || wz < -float64(o.PlayableSpan)/2 || wz > float64(o.PlayableSpan)/2 {
					height = -16
				}
				tile.HeightSamples[x+z*width] = uint16(math.Round((height + 64) / 128 * 65535))
			}
		}
		return tile
	}
	for _, coord := range plan.sources {
		tile := makeTile(coord, 128, o.HeightSampleSpacing)
		tile.OutdoorNavCellSize = 1
		tile.OutdoorNavExclusionMask = make([]byte, 8192)
		for z := 0; z < 256; z++ {
			for x := 0; x < 256; x++ {
				wx, wz := float64(tile.WorldOrigin[0])+float64(x)+.5, float64(tile.WorldOrigin[2])+float64(z)+.5
				excluded := wx < -float64(o.PlayableSpan)/2 || wx > float64(o.PlayableSpan)/2 || wz < -float64(o.PlayableSpan)/2 || wz > float64(o.PlayableSpan)/2
				for _, f := range full {
					if wx >= float64(f.Min[0]) && wx <= float64(f.Max[0]) && wz >= float64(f.Min[2]) && wz <= float64(f.Max[2]) {
						excluded = true
						break
					}
				}
				if excluded {
					index := x + z*256
					tile.OutdoorNavExclusionMask[index/8] |= 1 << uint(index%8)
				}
			}
		}
		path := fmt.Sprintf("terrain/tiles/%s/source_%d_%d.gkchunk", generation, coord.X, coord.Z)
		b.SourceTiles[coord] = tile
		d.Entries = append(d.Entries, content.TerrainChunkEntryDef{Coord: coord, WorldOrigin: tile.WorldOrigin, TerrainID: d.TerrainID, SourceHash: generation, ChunkSize: 128, VoxelResolution: o.HeightSampleSpacing, HeightOffset: -64, HeightScale: 128, ChunkPath: path, PayloadKind: content.TerrainHeightTilePayloadKind})
	}
	indices := map[islandTerrainKey]uint32{}
	for _, key := range plan.pages {
		span := o.RegionalSpan
		spacing := o.HeightSampleSpacing
		if key.level == 2 {
			span = o.MacroSpan
			spacing = max(o.HeightSampleSpacing, span/128)
		}
		if key.level == 3 {
			span = o.RootSpan
			spacing = span / 128
		}
		width := int(math.Round(float64(span) / float64(spacing)))
		tile := makeTile(content.TerrainChunkCoordDef{X: key.x, Z: key.z}, width, spacing)
		path := fmt.Sprintf("terrain/pages/%s/page_%d_%d_%d.gkchunk", generation, key.level, key.x, key.z)
		b.TerrainPageTiles[path] = tile
		indices[key] = uint32(len(d.Pages))
		p := content.StreamPageDef{Level: key.level, BoundsMin: [3]float32{tile.WorldOrigin[0], -64, tile.WorldOrigin[2]}, BoundsMax: [3]float32{tile.WorldOrigin[0] + span, 64, tile.WorldOrigin[2] + span}, Payload: content.StreamPagePayloadDef{Kind: content.TerrainHeightTilePayloadKind, Path: path, WorldOrigin: tile.WorldOrigin, ChunkSize: width, SampleSpacing: spacing, VoxelResolution: spacing, HeightOffset: -64, HeightScale: 128}}
		if key.level == 1 {
			p.Payload.VoxelResolution = max(.5, spacing/2)
		}
		if key.level == 3 && plan.refinedRoots[[2]int{key.x, key.z}] || key.level == 2 && plan.refinedMacros[[2]int{key.x, key.z}] {
			for dz := 0; dz < 4; dz++ {
				for dx := 0; dx < 4; dx++ {
					child, ok := indices[islandTerrainKey{key.level - 1, key.x*4 + dx, key.z*4 + dz}]
					if !ok {
						return fmt.Errorf("incomplete fixture child partition")
					}
					p.ChildPageIndices = append(p.ChildPageIndices, child)
				}
			}
		}
		d.Pages = append(d.Pages, p)
		if key.level == 3 {
			d.RootPageIndices = append(d.RootPageIndices, indices[key])
		}
	}
	return nil
}
