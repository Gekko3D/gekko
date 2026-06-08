package hl1

import (
	"fmt"
	"sort"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func (b *BSP) VisibilityLeafCount() int {
	if b == nil {
		return 0
	}
	if len(b.Models) > 0 && b.Models[0].VisLeafs > 0 {
		return int(b.Models[0].VisLeafs)
	}
	return len(b.visibilityLeafIDs())
}

func (b *BSP) PVSLeafIDs(leafID int) ([]int, bool) {
	bits, leafIDs, ok := b.decompressPVSBits(leafID)
	if !ok {
		return nil, false
	}
	out := make([]int, 0, len(leafIDs))
	for i, leaf := range leafIDs {
		if i < len(bits) && bits[i] {
			out = append(out, leaf)
		}
	}
	return out, true
}

func (b *BSP) decompressPVSBits(leafID int) ([]bool, []int, bool) {
	if b == nil || leafID < 0 || leafID >= len(b.Leafs) || len(b.VisibilityData) == 0 {
		return nil, nil, false
	}
	offset := int(b.Leafs[leafID].VisibilityOffset)
	if offset < 0 || offset >= len(b.VisibilityData) {
		return nil, nil, false
	}
	leafIDs := b.visibilityLeafIDs()
	if len(leafIDs) == 0 {
		return nil, nil, false
	}
	byteCount := (len(leafIDs) + 7) / 8
	decompressed := make([]byte, byteCount)
	src := b.VisibilityData[offset:]
	srcIndex := 0
	dstIndex := 0
	for dstIndex < byteCount && srcIndex < len(src) {
		value := src[srcIndex]
		srcIndex++
		if value != 0 {
			decompressed[dstIndex] = value
			dstIndex++
			continue
		}
		if srcIndex >= len(src) {
			return nil, nil, false
		}
		run := int(src[srcIndex])
		srcIndex++
		dstIndex += run
	}
	if dstIndex < byteCount {
		return nil, nil, false
	}
	bits := make([]bool, len(leafIDs))
	for i := range leafIDs {
		if decompressed[i/8]&(1<<uint(i%8)) != 0 {
			bits[i] = true
		}
	}
	return bits, leafIDs, true
}

func (b *BSP) visibilityLeafIDs() []int {
	if b == nil || len(b.Leafs) == 0 {
		return nil
	}
	limit := 0
	if len(b.Models) > 0 && b.Models[0].VisLeafs > 0 {
		limit = int(b.Models[0].VisLeafs)
	}
	ids := make([]int, 0, len(b.Leafs))
	for i, leaf := range b.Leafs {
		if IsPlayableEmptyContent(leaf.Contents) {
			ids = append(ids, i)
		}
	}
	if limit > 0 && len(ids) == 0 {
		for i := 1; i < len(b.Leafs) && len(ids) < limit; i++ {
			ids = append(ids, i)
		}
	}
	if limit > 0 && len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}

func ApplyHL1SectorVisibility(manifest *content.ImportedWorldDef, bsp *BSP) {
	if manifest == nil || bsp == nil || len(manifest.Sectors) == 0 {
		return
	}
	leafBounds := bsp.playableLeafBoundsGekko()
	sectorLeafs := map[content.TerrainChunkCoordDef][]int{}
	leafSectors := map[int]map[content.TerrainChunkCoordDef]struct{}{}
	for i := range manifest.Sectors {
		sector := &manifest.Sectors[i]
		bounds := importedSectorBounds(sector)
		for leafID, boundsGekko := range leafBounds {
			if !boundsOverlap(bounds, boundsGekko, 0.001) {
				continue
			}
			sectorLeafs[sector.Coord] = append(sectorLeafs[sector.Coord], leafID)
			if leafSectors[leafID] == nil {
				leafSectors[leafID] = map[content.TerrainChunkCoordDef]struct{}{}
			}
			leafSectors[leafID][sector.Coord] = struct{}{}
		}
	}
	adjacency := buildSectorAdjacency(manifest.Sectors)
	for i := range manifest.Sectors {
		sector := &manifest.Sectors[i]
		leafIDs := sectorLeafs[sector.Coord]
		sort.Ints(leafIDs)
		sector.SourceLeafIDs = leafIDs
		sector.AdjacentSectorRefs = sortedSectorCoords(adjacency[sector.Coord])
		visible := map[content.TerrainChunkCoordDef]struct{}{
			sector.Coord: {},
		}
		for _, ref := range sector.AdjacentSectorRefs {
			visible[ref] = struct{}{}
		}
		usedPVS := false
		for _, leafID := range leafIDs {
			visibleLeafIDs, ok := bsp.PVSLeafIDs(leafID)
			if !ok {
				continue
			}
			usedPVS = true
			for _, visibleLeafID := range visibleLeafIDs {
				for ref := range leafSectors[visibleLeafID] {
					visible[ref] = struct{}{}
				}
			}
		}
		sector.VisibleSectorRefs = sortedSectorCoords(visible)
		if len(leafIDs) > 0 {
			sector.VisibilityID = fmt.Sprintf("hl1_leaf:%d", leafIDs[0])
			sector.Tags = appendSectorVisibilityTag(sector.Tags, "visibility:hl1_leaf")
			if usedPVS {
				sector.Tags = appendSectorVisibilityTag(sector.Tags, "visibility:pvs")
			} else {
				sector.Tags = appendSectorVisibilityTag(sector.Tags, "visibility:adjacency")
			}
		} else if sector.VisibilityID == "" {
			sector.VisibilityID = content.TerrainChunkKey(sector.Coord)
			sector.Tags = appendSectorVisibilityTag(sector.Tags, "visibility:adjacency")
		}
	}
}

func (b *BSP) playableLeafBoundsGekko() map[int]importcommon.Bounds {
	out := map[int]importcommon.Bounds{}
	if b == nil {
		return out
	}
	for i, leaf := range b.Leafs {
		if !IsPlayableEmptyContent(leaf.Contents) || !leafHasUsableBounds(leaf) {
			continue
		}
		out[i] = HammerBoundsToGekko(
			importcommon.Vec3{X: float32(leaf.Min[0]), Y: float32(leaf.Min[1]), Z: float32(leaf.Min[2])},
			importcommon.Vec3{X: float32(leaf.Max[0]), Y: float32(leaf.Max[1]), Z: float32(leaf.Max[2])},
		)
	}
	return out
}

func importedSectorBounds(sector *content.ImportedWorldSectorDef) importcommon.Bounds {
	return importcommon.Bounds{
		Min: importcommon.Vec3{X: sector.BoundsMin[0], Y: sector.BoundsMin[1], Z: sector.BoundsMin[2]},
		Max: importcommon.Vec3{X: sector.BoundsMax[0], Y: sector.BoundsMax[1], Z: sector.BoundsMax[2]},
	}
}

func boundsOverlap(a, b importcommon.Bounds, epsilon float32) bool {
	return a.Min.X <= b.Max.X+epsilon && a.Max.X+epsilon >= b.Min.X &&
		a.Min.Y <= b.Max.Y+epsilon && a.Max.Y+epsilon >= b.Min.Y &&
		a.Min.Z <= b.Max.Z+epsilon && a.Max.Z+epsilon >= b.Min.Z
}

func buildSectorAdjacency(sectors []content.ImportedWorldSectorDef) map[content.TerrainChunkCoordDef]map[content.TerrainChunkCoordDef]struct{} {
	coords := make([]content.TerrainChunkCoordDef, 0, len(sectors))
	coordSet := map[content.TerrainChunkCoordDef]struct{}{}
	for _, sector := range sectors {
		coords = append(coords, sector.Coord)
		coordSet[sector.Coord] = struct{}{}
	}
	out := map[content.TerrainChunkCoordDef]map[content.TerrainChunkCoordDef]struct{}{}
	for _, coord := range coords {
		for dx := -1; dx <= 1; dx++ {
			for dy := -1; dy <= 1; dy++ {
				for dz := -1; dz <= 1; dz++ {
					if dx == 0 && dy == 0 && dz == 0 {
						continue
					}
					neighbor := content.TerrainChunkCoordDef{X: coord.X + dx, Y: coord.Y + dy, Z: coord.Z + dz}
					if _, ok := coordSet[neighbor]; !ok {
						continue
					}
					if out[coord] == nil {
						out[coord] = map[content.TerrainChunkCoordDef]struct{}{}
					}
					out[coord][neighbor] = struct{}{}
				}
			}
		}
	}
	return out
}

func sortedSectorCoords(coords map[content.TerrainChunkCoordDef]struct{}) []content.TerrainChunkCoordDef {
	if len(coords) == 0 {
		return nil
	}
	out := make([]content.TerrainChunkCoordDef, 0, len(coords))
	for coord := range coords {
		out = append(out, coord)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].Z < out[j].Z
	})
	return out
}

func appendSectorVisibilityTag(tags []string, tag string) []string {
	for _, existing := range tags {
		if existing == tag {
			return tags
		}
	}
	return append(tags, tag)
}
