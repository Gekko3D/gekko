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

func resolveVoxelGeometry(assets *AssetServer, vmc *VoxelModelComponent) (AssetId, *VoxelGeometryAsset, bool) {
	if assets == nil || vmc == nil {
		return AssetId{}, nil, false
	}
	vmc.NormalizeGeometryRefs()
	assetID := vmc.GeometryAsset()
	if assetID == (AssetId{}) {
		return AssetId{}, nil, false
	}
	asset, ok := assets.getVoxelGeometry(assetID)
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

func resolveVoxelGeometryMap(assets *AssetServer, vmc *VoxelModelComponent) (*volume.XBrickMap, bool) {
	_, asset, ok := resolveVoxelGeometry(assets, vmc)
	if !ok || asset == nil || asset.XBrickMap == nil {
		return nil, false
	}
	return asset.XBrickMap, true
}

func voxelModelComponentForEdit(cmd *Commands, eid EntityId) (VoxelModelComponent, bool) {
	if cmd == nil {
		return VoxelModelComponent{}, false
	}
	components := voxelEditComponents(cmd, eid)
	for i := len(components) - 1; i >= 0; i-- {
		comp := components[i]
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
	assetID, asset, ok := resolveVoxelGeometry(assets, &vmc)
	if !ok || asset == nil || asset.XBrickMap == nil {
		return VoxelModelComponent{}, AssetId{}, nil, fmt.Errorf("entity %d has no voxel geometry", eid)
	}
	_, managed, enabled := managedVoxelEntity(cmd, assets, eid)
	if managed != nil {
		if err := managedVoxelRuntimeQualification(cmd, assets, eid); err != nil {
			return VoxelModelComponent{}, AssetId{}, nil, err
		}
	}
	if vmc.OverrideGeometry == (AssetId{}) || managed != nil && !enabled {
		clonedID, cloned := assets.CloneVoxelGeometry(assetID)
		if !cloned {
			return VoxelModelComponent{}, AssetId{}, nil, fmt.Errorf("failed to clone voxel geometry for entity %d", eid)
		}
		vmc.OverrideGeometry = clonedID
		cmd.AddComponents(eid, &vmc)
		assetID = clonedID
	}
	exposed, exists := assets.GetVoxelGeometry(assetID)
	if !exists || exposed.XBrickMap == nil {
		return vmc, assetID, nil, fmt.Errorf("entity %d editable geometry missing", eid)
	}
	asset = &exposed
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
