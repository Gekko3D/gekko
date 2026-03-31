package gekko

import (
	"fmt"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func ResolveVoxelGeometry(assets *AssetServer, vmc *VoxelModelComponent) (AssetId, *VoxelGeometryAsset, bool) {
	if assets == nil || vmc == nil {
		return AssetId{}, nil, false
	}
	vmc.NormalizeGeometryRefs()
	assetID := vmc.GeometryAsset()
	if assetID == (AssetId{}) {
		return AssetId{}, nil, false
	}
	asset, ok := assets.GetVoxelGeometry(assetID)
	if !ok {
		return AssetId{}, nil, false
	}
	return assetID, &asset, true
}

func ResolveVoxelGeometryMap(assets *AssetServer, vmc *VoxelModelComponent) (*volume.XBrickMap, bool) {
	_, asset, ok := ResolveVoxelGeometry(assets, vmc)
	if !ok || asset == nil || asset.XBrickMap == nil {
		return nil, false
	}
	return asset.XBrickMap, true
}

func voxelModelComponentForEdit(cmd *Commands, eid EntityId) (VoxelModelComponent, bool) {
	if cmd == nil {
		return VoxelModelComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		switch typed := comp.(type) {
		case *VoxelModelComponent:
			vmc := *typed
			vmc.NormalizeGeometryRefs()
			return vmc, true
		case VoxelModelComponent:
			typed.NormalizeGeometryRefs()
			return typed, true
		}
	}
	return VoxelModelComponent{}, false
}

func EnsureEditableVoxelGeometry(cmd *Commands, assets *AssetServer, eid EntityId) (VoxelModelComponent, AssetId, *volume.XBrickMap, error) {
	vmc, ok := voxelModelComponentForEdit(cmd, eid)
	if !ok {
		return VoxelModelComponent{}, AssetId{}, nil, fmt.Errorf("entity %d has no VoxelModelComponent", eid)
	}
	assetID, asset, ok := ResolveVoxelGeometry(assets, &vmc)
	if !ok || asset == nil || asset.XBrickMap == nil {
		return VoxelModelComponent{}, AssetId{}, nil, fmt.Errorf("entity %d has no voxel geometry", eid)
	}
	if vmc.OverrideGeometry == (AssetId{}) {
		clonedID, cloned := assets.CloneVoxelGeometry(assetID)
		if !cloned {
			return VoxelModelComponent{}, AssetId{}, nil, fmt.Errorf("failed to clone voxel geometry for entity %d", eid)
		}
		vmc.OverrideGeometry = clonedID
		cmd.AddComponents(eid, &vmc)
		assetID = clonedID
		clonedAsset, exists := assets.GetVoxelGeometry(assetID)
		if !exists || clonedAsset.XBrickMap == nil {
			return VoxelModelComponent{}, AssetId{}, nil, fmt.Errorf("cloned voxel geometry missing for entity %d", eid)
		}
		asset = &clonedAsset
	}
	return vmc, assetID, asset.XBrickMap, nil
}

func EditVoxelGeometry(cmd *Commands, assets *AssetServer, eid EntityId, edit func(*volume.XBrickMap) error) error {
	if edit == nil {
		return nil
	}
	_, _, xbm, err := EnsureEditableVoxelGeometry(cmd, assets, eid)
	if err != nil {
		return err
	}
	if xbm == nil {
		return fmt.Errorf("entity %d has no editable voxel map", eid)
	}
	return edit(xbm)
}
