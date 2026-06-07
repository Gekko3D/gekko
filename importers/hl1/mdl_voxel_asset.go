package hl1

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

type MDLVoxelAssetOptions struct {
	Name            string
	SourceRef       string
	VoxelResolution float32
}

func BuildMDLVoxelAsset(geometry MDLGeometry, opts MDLVoxelAssetOptions) (*content.AssetDef, int, error) {
	resolution := opts.VoxelResolution
	if resolution <= 0 {
		resolution = DefaultImportedVoxelResolution
	}
	if len(geometry.Triangles) == 0 {
		return nil, 0, fmt.Errorf("mdl contains no decoded triangles")
	}
	voxels := voxelizeMDLGeometry(geometry, resolution)
	if len(voxels) == 0 {
		return nil, 0, fmt.Errorf("mdl voxelization produced no voxels")
	}
	localVoxels, origin := localizeMDLVoxels(voxels, resolution)
	materials, palette := mdlAssetMaterialsAndPalette(voxels)
	name := opts.Name
	if name == "" {
		name = geometry.Info.Name
	}
	if name == "" {
		name = "hl1_model"
	}
	asset := content.NewAssetDef(name)
	asset.Tags = []string{"source:hl1", "source_asset:mdl", "generated:mdl_voxel_surface"}
	if opts.SourceRef != "" {
		asset.Tags = append(asset.Tags, "source_ref:"+opts.SourceRef)
	}
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	asset.Materials = materials
	asset.Parts = []content.AssetPartDef{{
		ID:              "mdl_surface",
		Name:            "mdl_surface",
		VoxelResolution: resolution,
		Transform: content.AssetTransformDef{
			Position: content.Vec3{origin.X, origin.Y, origin.Z},
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxelShape,
			VoxelShape: &content.AssetVoxelShapeDef{
				Palette: palette,
				Voxels:  localVoxels,
			},
		},
		Tags: []string{"source:hl1", "source_asset:mdl", "generated:mdl_voxel_surface"},
	}}
	return asset, len(localVoxels), nil
}

func voxelizeMDLGeometry(geometry MDLGeometry, resolution float32) map[[3]int]mdlVoxelSample {
	out := map[[3]int]mdlVoxelSample{}
	half := importcommon.Vec3{X: resolution * 0.5, Y: resolution * 0.5, Z: resolution * 0.5}
	for _, tri := range geometry.Triangles {
		triWorld := [3]importcommon.Vec3{
			HammerToGekko(tri.Vertices[0].Position),
			HammerToGekko(tri.Vertices[1].Position),
			HammerToGekko(tri.Vertices[2].Position),
		}
		minB, maxB := triangleVoxelBounds(triWorld, resolution)
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					key := [3]int{x, y, z}
					if !triangleIntersectsVoxel(triWorld, key, half, resolution) {
						continue
					}
					color := sampleMDLTriangleColor(geometry, tri, triWorld, voxelCenter(key, resolution))
					if color[3] == 0 {
						continue
					}
					out[key] = mdlVoxelSample{Color: color}
				}
			}
		}
	}
	return out
}

type mdlVoxelSample struct {
	Color [4]uint8
}

func sampleMDLTriangleColor(geometry MDLGeometry, tri MDLTriangle, triWorld [3]importcommon.Vec3, point importcommon.Vec3) [4]uint8 {
	bary, ok := barycentricPoint(triWorld, point)
	if !ok {
		return [4]uint8{180, 180, 180, 255}
	}
	u := bary[0]*tri.Vertices[0].UV[0] + bary[1]*tri.Vertices[1].UV[0] + bary[2]*tri.Vertices[2].UV[0]
	v := bary[0]*tri.Vertices[0].UV[1] + bary[1]*tri.Vertices[1].UV[1] + bary[2]*tri.Vertices[2].UV[1]
	if tri.TextureIndex < 0 || tri.TextureIndex >= len(geometry.Textures) {
		return [4]uint8{180, 180, 180, 255}
	}
	texture := geometry.Textures[tri.TextureIndex]
	color, ok := sampleMDLTexture(texture, u, v)
	if !ok {
		return [4]uint8{180, 180, 180, 255}
	}
	return color
}

func barycentricPoint(tri [3]importcommon.Vec3, point importcommon.Vec3) ([3]float32, bool) {
	v0 := subVec3(tri[1], tri[0])
	v1 := subVec3(tri[2], tri[0])
	v2 := subVec3(point, tri[0])
	d00 := dotVec3(v0, v0)
	d01 := dotVec3(v0, v1)
	d11 := dotVec3(v1, v1)
	d20 := dotVec3(v2, v0)
	d21 := dotVec3(v2, v1)
	denom := d00*d11 - d01*d01
	if float32(math.Abs(float64(denom))) < 1e-8 {
		return [3]float32{}, false
	}
	v := (d11*d20 - d01*d21) / denom
	w := (d00*d21 - d01*d20) / denom
	u := 1 - v - w
	u = clampFloat32(u, 0, 1)
	v = clampFloat32(v, 0, 1)
	w = clampFloat32(w, 0, 1)
	sum := u + v + w
	if sum <= 1e-6 {
		return [3]float32{}, false
	}
	return [3]float32{u / sum, v / sum, w / sum}, true
}

func sampleMDLTexture(texture MDLTexturePixels, u float32, v float32) ([4]uint8, bool) {
	width := texture.Info.Width
	height := texture.Info.Height
	if width <= 0 || height <= 0 || len(texture.Pixels) < width*height || len(texture.Palette) == 0 {
		return [4]uint8{}, false
	}
	x := wrapTextureCoord(int(math.Floor(float64(u*float32(width)))), width)
	y := wrapTextureCoord(int(math.Floor(float64(v*float32(height)))), height)
	paletteIndex := int(texture.Pixels[y*width+x])
	if paletteIndex < 0 || paletteIndex >= len(texture.Palette) {
		return [4]uint8{}, false
	}
	color := texture.Palette[paletteIndex]
	return [4]uint8{color[0], color[1], color[2], 255}, true
}

func localizeMDLVoxels(voxels map[[3]int]mdlVoxelSample, resolution float32) ([]content.VoxelObjectVoxelDef, importcommon.Vec3) {
	keys := make([][3]int, 0, len(voxels))
	first := true
	var minK [3]int
	for key := range voxels {
		keys = append(keys, key)
		if first {
			minK = key
			first = false
			continue
		}
		minK[0] = min(minK[0], key[0])
		minK[1] = min(minK[1], key[1])
		minK[2] = min(minK[2], key[2])
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][0] != keys[j][0] {
			return keys[i][0] < keys[j][0]
		}
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][2] < keys[j][2]
	})
	palette := newMDLColorPalette(voxels)
	out := make([]content.VoxelObjectVoxelDef, 0, len(keys))
	for _, key := range keys {
		out = append(out, content.VoxelObjectVoxelDef{
			X:     key[0] - minK[0],
			Y:     key[1] - minK[1],
			Z:     key[2] - minK[2],
			Value: palette.valueForColor(voxels[key].Color),
		})
	}
	return out, importcommon.Vec3{X: float32(minK[0]) * resolution, Y: float32(minK[1]) * resolution, Z: float32(minK[2]) * resolution}
}

func mdlAssetMaterialsAndPalette(voxels map[[3]int]mdlVoxelSample) ([]content.AssetMaterialDef, []content.AssetVoxelPaletteEntryDef) {
	pal := newMDLColorPalette(voxels)
	materials := make([]content.AssetMaterialDef, 0, len(pal.colors))
	shapePalette := make([]content.AssetVoxelPaletteEntryDef, 0, len(pal.colors))
	for _, entry := range pal.colors {
		materialID := fmt.Sprintf("mat_%d", entry.Value)
		materials = append(materials, content.AssetMaterialDef{
			ID:        materialID,
			Name:      materialID,
			BaseColor: entry.Color,
			Roughness: 0.85,
			Tags:      []string{"source:hl1", "source_asset:mdl", "material:texture_baked", "material:static_prop"},
		})
		shapePalette = append(shapePalette, content.AssetVoxelPaletteEntryDef{Value: entry.Value, MaterialID: materialID})
	}
	return materials, shapePalette
}

type mdlPaletteColor struct {
	Value uint8
	Color [4]uint8
}

type mdlColorPalette struct {
	colors  []mdlPaletteColor
	byColor map[[4]uint8]uint8
}

func newMDLColorPalette(voxels map[[3]int]mdlVoxelSample) mdlColorPalette {
	counts := map[[4]uint8]int{}
	for _, sample := range voxels {
		counts[sample.Color]++
	}
	type counted struct {
		color [4]uint8
		count int
	}
	values := make([]counted, 0, len(counts))
	for color, count := range counts {
		values = append(values, counted{color: color, count: count})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].count != values[j].count {
			return values[i].count > values[j].count
		}
		return colorKey(values[i].color) < colorKey(values[j].color)
	})
	limit := min(len(values), 255)
	pal := mdlColorPalette{colors: make([]mdlPaletteColor, 0, limit), byColor: map[[4]uint8]uint8{}}
	for i := 0; i < limit; i++ {
		value := uint8(i + 1)
		pal.colors = append(pal.colors, mdlPaletteColor{Value: value, Color: values[i].color})
		pal.byColor[values[i].color] = value
	}
	return pal
}

func (p mdlColorPalette) valueForColor(color [4]uint8) uint8 {
	if value, ok := p.byColor[color]; ok {
		return value
	}
	bestValue := uint8(1)
	bestDistance := int(^uint(0) >> 1)
	for _, entry := range p.colors {
		distance := colorDistanceSquared(color, entry.Color)
		if distance < bestDistance {
			bestDistance = distance
			bestValue = entry.Value
		}
	}
	return bestValue
}

func colorDistanceSquared(a, b [4]uint8) int {
	dr := int(a[0]) - int(b[0])
	dg := int(a[1]) - int(b[1])
	db := int(a[2]) - int(b[2])
	return dr*dr + dg*dg + db*db
}

func colorKey(color [4]uint8) int {
	return int(color[0])<<24 | int(color[1])<<16 | int(color[2])<<8 | int(color[3])
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
