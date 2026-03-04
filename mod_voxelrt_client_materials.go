package gekko

import "github.com/gekko3d/gekko/voxelrt/rt/core"

func clampF(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func (s *VoxelRtState) CycleRenderMode() {
	if s != nil && s.RtApp != nil {
		s.RtApp.RenderMode = (s.RtApp.RenderMode + 1) % 4
	}
}

func (s *VoxelRtState) buildMaterialTable(gekkoPalette *VoxelPaletteAsset) []core.Material {
	materialTable := make([]core.Material, 256)

	matMap := make(map[int]VoxMaterial)
	for _, m := range gekkoPalette.Materials {
		matMap[m.ID] = m
	}

	for i, color := range gekkoPalette.VoxPalette {
		mat := core.DefaultMaterial()
		mat.BaseColor = color
		if i == 0 {
			mat.Transparency = 1.0 // Air is transparent!
		}

		if vMat, ok := matMap[i]; ok {
			if r, ok := vMat.Property["_rough"].(float32); ok {
				mat.Roughness = r
			}
			if m, ok := vMat.Property["_metal"].(float32); ok {
				mat.Metalness = m
			}
			if ior, ok := vMat.Property["_ior"].(float32); ok {
				mat.IOR = ior
			}
			if trans, ok := vMat.Property["_trans"].(float32); ok {
				mat.Transparency = trans
			}
			if emit, ok := vMat.Property["_emit"].(float32); ok {
				flux := float32(1.0)
				if f, ok := vMat.Property["_flux"].(float32); ok {
					flux = f
				}
				power := emit * flux
				mat.Emissive = [4]uint8{
					uint8(min(255, float32(color[0])*power)),
					uint8(min(255, float32(color[1])*power)),
					uint8(min(255, float32(color[2])*power)),
					255,
				}
			}
		}

		if gekkoPalette.IsPBR {
			mat.Roughness = gekkoPalette.Roughness
			mat.Metalness = gekkoPalette.Metalness
			mat.IOR = gekkoPalette.IOR
			if gekkoPalette.Emission > 0 {
				power := gekkoPalette.Emission
				mat.Emissive = [4]uint8{
					uint8(min(255, float32(color[0])*power)),
					uint8(min(255, float32(color[1])*power)),
					uint8(min(255, float32(color[2])*power)),
					255,
				}
			}
		}

		// Infer transparency from palette alpha channel if not explicitly provided
		if color[3] < 255 {
			a := float32(color[3]) / 255.0
			t := float32(1.0) - a
			if t > mat.Transparency {
				mat.Transparency = t
			}
		}

		materialTable[i] = mat
	}
	return materialTable
}
