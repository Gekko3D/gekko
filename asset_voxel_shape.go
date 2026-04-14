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
	return assets.CreateVoxelPaletteAsset(asset), nil
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
