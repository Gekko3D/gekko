package gpu

// executeVoxelUpload receives only admitted work. Atlas reclamation across the
// complete unit precedes allocation, so a later cleared record can supply an
// earlier new brick. Service owns dirty/material completion and counters.
func (m *GpuBufferManager) executeVoxelUpload(context func() voxelNormalBakeContext, work voxelUploadWork) bool {
	if !work.targetCurrent() {
		return false
	}
	if work.kind == voxelUploadMaterial {
		mat := m.MaterialAllocations[work.object]
		mustQueueVoxelWrite(m.Device.GetQueue().WriteBuffer(m.MaterialBuf, uint64(mat.MaterialOffset)*64, buildMaterialData(work.object.MaterialTable)))
		return true
	}
	key := work.sectorCoordinate()
	xbm := work.targetMap()
	m.markRetainedVoxelMapAccountingDirty(xbm)
	sector := xbm.Sectors[key]
	info := m.SectorToInfo[sector]
	pointers := m.Allocations[xbm].Bricks[key]
	payload, auxiliary := m.voxelUploadReleases(work)
	for brick := range payload {
		m.releaseBrickSlot(brick)
	}
	for brick := range auxiliary {
		m.releaseVoxelAuxSlot(brick)
	}
	if work.kind == voxelUploadSector {
		m.writeSectorRecord(sector, info)
	}
	start, end := work.brickRange()
	for i := start; i < end; i++ {
		brick := sector.GetBrick(i%4, (i/4)%4, i/16)
		pointers[i] = brick
		if brick == nil {
			mustQueueVoxelWrite(m.Device.GetQueue().WriteBuffer(m.BrickTableBuf, uint64(info.BrickTableIndex+uint32(i))*BrickRecordSize, make([]byte, BrickRecordSize)))
		} else {
			m.uploadBrick(context, work.object, xbm, brick, info.BrickTableIndex+uint32(i), brickOriginForSectorIndex(key, i))
		}
	}
	return true
}

// Native validation errors can occur after earlier writes in a unit. Fail fast
// before service publishes completion; this is not a transactional GPU rollback.
func mustQueueVoxelWrite(err error) {
	if err != nil {
		panic(err)
	}
}
