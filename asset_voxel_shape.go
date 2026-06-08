package gekko

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/gekko3d/gekko/content"
)

func authoredVoxelShapeGeometry(assets *AssetServer, part content.AssetPartDef) (AssetId, error) {
	if assets == nil {
		return AssetId{}, nil
	}
	if part.Source.VoxelShape == nil {
		return AssetId{}, fmt.Errorf("voxel_shape source for part %s is missing payload", part.ID)
	}

	cachePayload, err := json.Marshal(struct {
		Scale float32                     `json:"scale"`
		Shape *content.AssetVoxelShapeDef `json:"shape"`
	}{
		Scale: part.ModelScale,
		Shape: part.Source.VoxelShape,
	})
	if err != nil {
		return AssetId{}, err
	}

	xbm := XBrickMapFromVoxelObjectSnapshot(&content.VoxelObjectSnapshotDef{
		SchemaVersion: content.CurrentVoxelObjectSnapshotSchemaVersion,
		Voxels:        append([]content.VoxelObjectVoxelDef(nil), part.Source.VoxelShape.Voxels...),
	})
	if scale := part.ModelScale; scale > 0 && math.Abs(float64(scale-1.0)) > 1e-5 {
		xbm = xbm.Resample(scale)
	}
	xbm.ComputeAABB()
	xbm.ClearDirty()

	cacheKey := string(cachePayload)
	return assets.RegisterSharedVoxelGeometryWithCacheKey(cacheKey, xbm, cacheKey), nil
}

func authoredVoxelShapePalette(assets *AssetServer, def *content.AssetDef, part content.AssetPartDef) (AssetId, error) {
	if assets == nil {
		return AssetId{}, nil
	}
	if part.Source.VoxelShape == nil {
		return AssetId{}, fmt.Errorf("voxel_shape source for part %s is missing payload", part.ID)
	}

	asset := VoxelPaletteAsset{}
	for _, entry := range part.Source.VoxelShape.Palette {
		material, ok := content.FindAssetMaterialByID(def, entry.MaterialID)
		if !ok {
			return AssetId{}, fmt.Errorf("missing material %s for part %s", entry.MaterialID, part.ID)
		}
		asset.VoxPalette[entry.Value] = material.BaseColor
		asset.Materials = append(asset.Materials, authoredMaterialToVoxMaterial(int(entry.Value), material))
	}
	asset.Animations = authoredAssetVoxelPaletteAnimations(def.MaterialAnimations)
	return assets.CreateVoxelPaletteAsset(asset), nil
}

func authoredAssetVoxelPaletteAnimations(animations []content.AssetMaterialAnimationDef) []VoxelPaletteAnimation {
	out := make([]VoxelPaletteAnimation, 0, len(animations))
	for _, animation := range animations {
		if animation.ID == "" || len(animation.PaletteIndices) == 0 || len(animation.Frames) == 0 {
			continue
		}
		frames := make([]VoxelPaletteAnimationFrame, 0, len(animation.Frames))
		for _, frame := range animation.Frames {
			if len(frame.Colors) == 0 && len(frame.EmissiveColors) == 0 && len(frame.Emission) == 0 && len(frame.Roughness) == 0 && len(frame.Transparency) == 0 {
				continue
			}
			frames = append(frames, VoxelPaletteAnimationFrame{
				Duration:       frame.Duration,
				Colors:         append([][4]uint8(nil), frame.Colors...),
				EmissiveColors: append([][4]uint8(nil), frame.EmissiveColors...),
				Emission:       append([]float32(nil), frame.Emission...),
				Roughness:      append([]float32(nil), frame.Roughness...),
				Transparency:   append([]float32(nil), frame.Transparency...),
			})
		}
		if len(frames) == 0 {
			continue
		}
		var uvScroll *VoxelPaletteUVScroll
		if animation.UVScroll != nil {
			uvScroll = &VoxelPaletteUVScroll{Velocity: animation.UVScroll.Velocity}
		}
		out = append(out, VoxelPaletteAnimation{
			ID:             animation.ID,
			Kind:           animation.Kind,
			FPS:            animation.FPS,
			Mode:           animation.Mode,
			PaletteIndices: append([]uint8(nil), animation.PaletteIndices...),
			Frames:         frames,
			UVScroll:       uvScroll,
			Tags:           append([]string(nil), animation.Tags...),
		})
	}
	return out
}

func authoredMaterialToVoxMaterial(id int, material content.AssetMaterialDef) VoxMaterial {
	props := map[string]interface{}{
		"_rough": material.Roughness,
		"_metal": material.Metallic,
		"_ior":   material.IOR,
		"_trans": material.Transparency,
	}
	switch {
	case material.Transparency > 0.01:
		props["_type"] = "glass"
	case material.Emissive > 0.01:
		props["_type"] = "emit"
	case material.Metallic > 0.5:
		props["_type"] = "metal"
	default:
		props["_type"] = "diffuse"
	}
	if material.Emissive > 0 {
		props["_emit"] = material.Emissive
	}
	return VoxMaterial{
		ID:       id,
		Property: props,
	}
}
