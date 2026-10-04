package core

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

// SetPendingFullUpload borrows authoritative geometry for upload while keeping
// a valid ordinary coarse representation selected. It does not certify readiness.
func (obj *VoxelObject) SetPendingFullUpload() bool {
	if !obj.RenderRepresentationValid() {
		obj.ClearPendingFullUpload()
		return false
	}
	r := obj.renderRepresentation
	if r.pendingFullGeneration == 0 {
		obj.pendingFullUploadGeneration++
		if obj.pendingFullUploadGeneration == 0 {
			obj.pendingFullUploadGeneration++
		}
		r.pendingFullGeneration = obj.pendingFullUploadGeneration
	}
	return true
}

// ClearPendingFullUpload cancels staging without changing display selection.
func (obj *VoxelObject) ClearPendingFullUpload() {
	if obj != nil && obj.renderRepresentation != nil {
		obj.renderRepresentation.pendingFullGeneration = 0
	}
}

func (obj *VoxelObject) PendingFullUploadMap() *volume.XBrickMap {
	if obj.PendingFullUploadGeneration() == 0 {
		return nil
	}
	return obj.renderRepresentation.source
}

func (obj *VoxelObject) PendingFullUploadGeneration() uint64 {
	if !obj.RenderRepresentationValid() {
		obj.ClearPendingFullUpload()
		return 0
	}
	return obj.renderRepresentation.pendingFullGeneration
}
