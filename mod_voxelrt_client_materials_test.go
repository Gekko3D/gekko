package gekko

import "testing"

func TestBuildMaterialTable_MagicaVoxelGlassDefaults(t *testing.T) {
	state := &VoxelRtState{}
	var palette VoxPalette
	palette[1] = [4]uint8{180, 220, 255, 255}

	table := state.buildMaterialTable(&VoxelPaletteAsset{
		VoxPalette: palette,
		Materials: []VoxMaterial{
			{
				ID: 1,
				Property: map[string]interface{}{
					"_type": "_glass",
				},
			},
		},
	})

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
}

func TestBuildMaterialTable_MagicaVoxelMetalRespectsOverrides(t *testing.T) {
	state := &VoxelRtState{}
	var palette VoxPalette
	palette[2] = [4]uint8{190, 150, 80, 255}

	table := state.buildMaterialTable(&VoxelPaletteAsset{
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
	})

	mat := table[2]
	if mat.Metalness != 1.0 {
		t.Fatalf("expected metal default metalness, got %f", mat.Metalness)
	}
	if mat.Roughness != 0.64 {
		t.Fatalf("expected explicit roughness override, got %f", mat.Roughness)
	}
}

func TestBuildMaterialTable_MagicaVoxelEmitUsesFlux(t *testing.T) {
	state := &VoxelRtState{}
	var palette VoxPalette
	palette[3] = [4]uint8{120, 255, 120, 255}

	table := state.buildMaterialTable(&VoxelPaletteAsset{
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
	})

	mat := table[3]
	if mat.Emission != 1.0 {
		t.Fatalf("expected emission strength 1.0, got %f", mat.Emission)
	}
	if mat.Emissive[1] != 255 {
		t.Fatalf("expected emissive tint to preserve palette color, got %+v", mat.Emissive)
	}
}
