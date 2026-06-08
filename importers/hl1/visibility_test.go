package hl1

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestApplyHL1SectorVisibilityAnnotatesPVSAndAdjacency(t *testing.T) {
	manifest := &content.ImportedWorldDef{
		WorldID:         "test",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       16,
		VoxelResolution: 1,
		Sectors: []content.ImportedWorldSectorDef{{
			Coord:         content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
			BoundsMin:     [3]float32{0.1, 0.1, -1.0},
			BoundsMax:     [3]float32{1.0, 1.0, -0.1},
			FullChunkRefs: []content.TerrainChunkCoordDef{{X: 0, Y: 0, Z: 0}},
		}, {
			Coord:         content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
			BoundsMin:     [3]float32{3.0, 0.1, -1.0},
			BoundsMax:     [3]float32{4.0, 1.0, -0.1},
			FullChunkRefs: []content.TerrainChunkCoordDef{{X: 1, Y: 0, Z: 0}},
		}, {
			Coord:         content.TerrainChunkCoordDef{X: 4, Y: 0, Z: 0},
			BoundsMin:     [3]float32{20.0, 20.0, 20.0},
			BoundsMax:     [3]float32{21.0, 21.0, 21.0},
			FullChunkRefs: []content.TerrainChunkCoordDef{{X: 4, Y: 0, Z: 0}},
		}},
	}
	bsp := &BSP{
		VisibilityData: []byte{
			0b00000011,
			0b00000010,
		},
		Leafs: []Leaf{{
			Contents: ContentsSolid,
		}, {
			Contents:         ContentsEmpty,
			VisibilityOffset: 0,
			Min:              [3]int16{0, 0, 0},
			Max:              [3]int16{100, 100, 100},
		}, {
			Contents:         ContentsEmpty,
			VisibilityOffset: 1,
			Min:              [3]int16{110, 0, 0},
			Max:              [3]int16{200, 100, 100},
		}},
		Models: []Model{{VisLeafs: 2}},
	}

	ApplyHL1SectorVisibility(manifest, bsp)

	first := manifest.Sectors[0]
	if first.VisibilityID != "hl1_leaf:1" {
		t.Fatalf("visibility id = %q", first.VisibilityID)
	}
	if len(first.SourceLeafIDs) != 1 || first.SourceLeafIDs[0] != 1 {
		t.Fatalf("source leaf ids = %+v", first.SourceLeafIDs)
	}
	if !sectorRefsContain(first.VisibleSectorRefs, content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("expected first sector to see second sector through PVS, got %+v", first.VisibleSectorRefs)
	}
	if !sectorRefsContain(first.AdjacentSectorRefs, content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}) {
		t.Fatalf("expected first sector adjacency to include second sector, got %+v", first.AdjacentSectorRefs)
	}
	if len(manifest.Sectors[2].SourceLeafIDs) != 0 || !sectorRefsContain(manifest.Sectors[2].VisibleSectorRefs, manifest.Sectors[2].Coord) {
		t.Fatalf("expected unmatched sector to keep self fallback visibility, got %+v", manifest.Sectors[2])
	}
}

func sectorRefsContain(refs []content.TerrainChunkCoordDef, want content.TerrainChunkCoordDef) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
