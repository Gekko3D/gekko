package gekko

import (
	"fmt"
	"strings"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func clampF(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func voxMaterialFloat(vMat VoxMaterial, key string) (float32, bool) {
	value, ok := vMat.Property[key]
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float32:
		return v, true
	case float64:
		return float32(v), true
	case string:
		var parsed float32
		if _, err := fmt.Sscanf(v, "%f", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func voxMaterialString(vMat VoxMaterial, key string) (string, bool) {
	value, ok := vMat.Property[key]
	if !ok {
		return "", false
	}
	switch v := value.(type) {
	case string:
		return v, true
	}
	return "", false
}

func voxMaterialKind(vMat VoxMaterial) string {
	rawType, ok := voxMaterialString(vMat, "_type")
	if !ok {
		return "diffuse"
	}
	kind := strings.ToLower(strings.TrimSpace(rawType))
	kind = strings.TrimPrefix(kind, "_")
	if kind == "" {
		return "diffuse"
	}
	return kind
}

func applyMagicaVoxelMaterial(mat *core.Material, color [4]uint8, vMat VoxMaterial) {
	typeDefaults := struct {
		roughness    float32
		metalness    float32
		ior          float32
		transparency float32
		emission     float32
		transmission float32
		density      float32
		refraction   float32
	}{
		roughness:    1.0,
		metalness:    0.0,
		ior:          1.5,
		transmission: 0.0,
		density:      0.0,
		refraction:   0.0,
	}

	switch voxMaterialKind(vMat) {
	case "metal":
		typeDefaults.roughness = 0.22
		typeDefaults.metalness = 1.0
	case "glass":
		typeDefaults.roughness = 0.08
		typeDefaults.ior = 1.52
		typeDefaults.transparency = 0.72
		typeDefaults.transmission = 1.0
		typeDefaults.density = 1.35
		typeDefaults.refraction = 0.9
	case "emit":
		typeDefaults.roughness = 0.35
		typeDefaults.emission = 1.0
	case "blend":
		typeDefaults.roughness = 0.4
		typeDefaults.ior = 1.33
		typeDefaults.transparency = 0.4
		typeDefaults.transmission = 0.7
		typeDefaults.density = 0.65
		typeDefaults.refraction = 0.35
	case "media":
		// Gekko has no volumetric surface material here, so approximate as a soft translucent dielectric.
		typeDefaults.roughness = 0.65
		typeDefaults.ior = 1.1
		typeDefaults.transparency = 0.55
		typeDefaults.transmission = 1.0
		typeDefaults.density = 1.85
		typeDefaults.refraction = 0.1
	}

	mat.Roughness = typeDefaults.roughness
	mat.Metalness = typeDefaults.metalness
	mat.IOR = typeDefaults.ior
	mat.Transmission = typeDefaults.transmission
	mat.Density = typeDefaults.density
	mat.Refraction = typeDefaults.refraction
	if typeDefaults.transparency > 0 {
		mat.Transparency = max(mat.Transparency, typeDefaults.transparency)
	}

	if r, ok := voxMaterialFloat(vMat, "_rough"); ok {
		mat.Roughness = r
	}
	if m, ok := voxMaterialFloat(vMat, "_metal"); ok {
		mat.Metalness = m
	}
	if ior, ok := voxMaterialFloat(vMat, "_ior"); ok {
		mat.IOR = ior
	}
	if trans, ok := voxMaterialFloat(vMat, "_trans"); ok {
		mat.Transparency = trans
	}
	if density, ok := voxMaterialFloat(vMat, "_d"); ok {
		mat.Density = density
	}
	if att, ok := voxMaterialFloat(vMat, "_att"); ok {
		mat.Density = att
	}
	if alpha, ok := voxMaterialFloat(vMat, "_alpha"); ok {
		mat.Transparency = clampF(1.0-alpha, 0.0, 1.0)
	}
	if ri, ok := voxMaterialFloat(vMat, "_ri"); ok {
		mat.IOR = ri
	}

	emissionStrength := typeDefaults.emission
	if emit, ok := voxMaterialFloat(vMat, "_emit"); ok {
		emissionStrength = emit
	}
	if flux, ok := voxMaterialFloat(vMat, "_flux"); ok {
		if emissionStrength <= 0 {
			emissionStrength = 1.0
		}
		emissionStrength *= flux
	}
	if emissionStrength > 0 {
		mat.Emissive = [4]uint8{color[0], color[1], color[2], 255}
		mat.Emission = emissionStrength
	}

	if mat.Transmission > 0 {
		if mat.Refraction <= 0 {
			mat.Refraction = clampF((mat.IOR-1.0)*0.6, 0.0, 1.0)
		}
		if mat.Density <= 0 {
			mat.Density = clampF(0.35+(1.0-mat.Transparency)*1.25, 0.15, 3.0)
		}
	}

	mat.Roughness = clampF(mat.Roughness, 0.0, 1.0)
	mat.Metalness = clampF(mat.Metalness, 0.0, 1.0)
	mat.Transparency = clampF(mat.Transparency, 0.0, 1.0)
	mat.Transmission = clampF(mat.Transmission, 0.0, 1.0)
	mat.Density = clampF(mat.Density, 0.0, 8.0)
	mat.Refraction = clampF(mat.Refraction, 0.0, 2.0)
	if mat.IOR < 1.0 {
		mat.IOR = 1.0
	}
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
			applyMagicaVoxelMaterial(&mat, color, vMat)
		}

		if gekkoPalette.IsPBR {
			mat.Roughness = gekkoPalette.Roughness
			mat.Metalness = gekkoPalette.Metalness
			mat.IOR = gekkoPalette.IOR
			if gekkoPalette.Emission > 0 {
				mat.Emissive = [4]uint8{color[0], color[1], color[2], 255}
				mat.Emission = gekkoPalette.Emission
			}
			if gekkoPalette.Transparency > mat.Transparency {
				mat.Transparency = gekkoPalette.Transparency
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
		if mat.Transparency > 0.001 && mat.Transmission <= 0.0 && mat.Metalness < 0.5 {
			opacity := 1.0 - mat.Transparency
			mat.Transmission = 1.0
			if mat.Density <= 0.0 {
				mat.Density = clampF(0.12+opacity*0.9, 0.08, 1.2)
			}
			if mat.Refraction <= 0.0 {
				mat.Refraction = clampF((mat.IOR-1.0)*(0.24+opacity*0.6), 0.0, 0.65)
			}
		}
		materialTable[i] = mat
	}
	return materialTable
}
