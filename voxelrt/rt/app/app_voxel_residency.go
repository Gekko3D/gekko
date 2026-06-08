package app

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

func (a *App) ActivateRetainedVoxelMap(xbm *volume.XBrickMap) bool {
	if a == nil || a.BufferManager == nil {
		return false
	}
	return a.BufferManager.ActivateRetainedVoxelMap(xbm)
}
