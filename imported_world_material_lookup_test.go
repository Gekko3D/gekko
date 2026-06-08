package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestImportedWorldMaterialLookupFindsChunkAndXBrickMapVoxels(t *testing.T) {
	def := &content.ImportedWorldDef{
		WorldID: "world-a",
		Materials: []content.ImportedWorldMaterialDef{{
			ID:                10,
			PaletteIndex:      5,
			SourceTextureName: "CONCRETE01",
			Kind:              "stone",
			CollisionKind:     "solid",
			Tags:              []string{"material:stone"},
		}},
	}
	chunk := &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		ChunkSize:          8,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 2, Y: 3, Z: 4, Value: 5}},
		NonEmptyVoxelCount: 1,
	}

	lookup := NewImportedWorldMaterialLookup(def)
	material, paletteIndex, ok := lookup.MaterialForChunkVoxel(chunk, [3]int{2, 3, 4})
	if !ok || paletteIndex != 5 || material.Kind != "stone" {
		t.Fatalf("expected chunk material lookup, got material=%+v palette=%d ok=%t", material, paletteIndex, ok)
	}
	material, ok = lookup.MaterialForSourceTexture(" concrete01 ")
	if !ok || material.PaletteIndex != 5 {
		t.Fatalf("expected source texture lookup, got material=%+v ok=%t", material, ok)
	}

	xbm := ImportedWorldChunkToXBrickMap(chunk)
	material, paletteIndex, ok = lookup.MaterialForXBrickMapVoxel(xbm, [3]int{2, 3, 4})
	if !ok || paletteIndex != 5 || !content.ImportedWorldMaterialHasTag(material, "material:stone") {
		t.Fatalf("expected xbrickmap material lookup, got material=%+v palette=%d ok=%t", material, paletteIndex, ok)
	}
	if _, _, ok := lookup.MaterialForXBrickMapVoxel(xbm, [3]int{0, 0, 0}); ok {
		t.Fatal("expected empty xbrickmap voxel lookup to miss")
	}
}
