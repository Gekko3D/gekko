package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestBuildMaterialTable_MagicaVoxelGlassDefaults(t *testing.T) {
	state := &VoxelRtState{materialTableCache: make(map[materialTableCacheKey][]core.Material)}
	var palette VoxPalette
	palette[1] = [4]uint8{180, 220, 255, 255}
	asset := &VoxelPaletteAsset{
		VoxPalette: palette,
		Materials: []VoxMaterial{
			{
				ID: 1,
				Property: map[string]interface{}{
					"_type": "_glass",
				},
			},
		},
	}
	table := state.buildMaterialTable(state.materialTableKey(AssetId{}, asset), asset)

	mat := table[1]
	if mat.Transparency < 0.7 {
		t.Fatalf("expected glass transparency default, got %f", mat.Transparency)
	}
	if mat.Metalness != 0 {
		t.Fatalf("expected glass to remain dielectric, got metalness %f", mat.Metalness)
	}
	if mat.IOR < 1.5 {
		t.Fatalf("expected glass IoR default, got %f", mat.IOR)
	}
	if mat.Transmission < 0.99 {
		t.Fatalf("expected glass transmission default, got %f", mat.Transmission)
	}
	if mat.Refraction <= 0 {
		t.Fatalf("expected glass refraction default, got %f", mat.Refraction)
	}
}

func TestBuildMaterialTable_MagicaVoxelMetalRespectsOverrides(t *testing.T) {
	state := &VoxelRtState{materialTableCache: make(map[materialTableCacheKey][]core.Material)}
	var palette VoxPalette
	palette[2] = [4]uint8{190, 150, 80, 255}
	asset := &VoxelPaletteAsset{
		VoxPalette: palette,
		Materials: []VoxMaterial{
			{
				ID: 2,
				Property: map[string]interface{}{
					"_type":  "_metal",
					"_rough": float32(0.64),
				},
			},
		},
	}
	table := state.buildMaterialTable(state.materialTableKey(AssetId{}, asset), asset)

	mat := table[2]
	if mat.Metalness != 1.0 {
		t.Fatalf("expected metal default metalness, got %f", mat.Metalness)
	}
	if mat.Roughness != 0.64 {
		t.Fatalf("expected explicit roughness override, got %f", mat.Roughness)
	}
	if mat.Transmission != 0.0 {
		t.Fatalf("expected opaque metal to avoid transmission, got %f", mat.Transmission)
	}
}

func TestBuildMaterialTable_MagicaVoxelEmitUsesFlux(t *testing.T) {
	state := &VoxelRtState{materialTableCache: make(map[materialTableCacheKey][]core.Material)}
	var palette VoxPalette
	palette[3] = [4]uint8{120, 255, 120, 255}
	asset := &VoxelPaletteAsset{
		VoxPalette: palette,
		Materials: []VoxMaterial{
			{
				ID: 3,
				Property: map[string]interface{}{
					"_type": "_emit",
					"_emit": "0.5",
					"_flux": "2.0",
				},
			},
		},
	}
	table := state.buildMaterialTable(state.materialTableKey(AssetId{}, asset), asset)

	mat := table[3]
	if mat.Emission != 1.0 {
		t.Fatalf("expected emission strength 1.0, got %f", mat.Emission)
	}
	if mat.Emissive[1] != 255 {
		t.Fatalf("expected emissive tint to preserve palette color, got %+v", mat.Emissive)
	}
}

func TestBuildMaterialTable_PaletteAlphaGetsSurfaceGlassDefaults(t *testing.T) {
	state := &VoxelRtState{materialTableCache: make(map[materialTableCacheKey][]core.Material)}
	var palette VoxPalette
	palette[4] = [4]uint8{200, 220, 255, 204} // 0.8 alpha -> light glassy sphere
	asset := &VoxelPaletteAsset{
		VoxPalette: palette,
	}
	table := state.buildMaterialTable(state.materialTableKey(AssetId{}, asset), asset)

	mat := table[4]
	if mat.Transparency < 0.19 || mat.Transparency > 0.21 {
		t.Fatalf("expected inferred transparency near 0.2, got %f", mat.Transparency)
	}
	if mat.Transmission < 0.99 {
		t.Fatalf("expected alpha glass to opt into transmission, got %f", mat.Transmission)
	}
	if mat.Density != 0.0 {
		t.Fatalf("expected alpha glass density to stay zero for surface-glass path, got %f", mat.Density)
	}
	if mat.Refraction < 0.35 {
		t.Fatalf("expected alpha glass refraction to be visibly stronger, got %f", mat.Refraction)
	}
}
