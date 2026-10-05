package gpu

// executeVoxelUpload receives only admitted work. Atlas reclamation across the
// complete unit precedes allocation, so a later cleared record can supply an
// earlier new brick. Service owns dirty/material completion and counters.
func (m *GpuBufferManager) executeVoxelUpload(context func() voxelNormalBakeContext, work voxelUploadWork) bool {
	if !work.targetCurrent() || (work.kind == voxelUploadMaterial && !m.materialWorkCurrent(work)) {
		return false
	}
	if work.kind == voxelUploadMaterial {
		mat := m.MaterialAllocations[work.object]
		data := work.materialData
		if data == nil {
			data = buildMaterialData(work.object.MaterialTable)
		}
		mustQueueVoxelWrite(m.writeVoxelBuffer(m.MaterialBuf, uint64(mat.MaterialOffset)*64, data))
		return true
	}
	key := work.sectorCoordinate()
	xbm := work.targetMap()
	m.markRetainedVoxelMapAccountingDirty(xbm)
	sector := xbm.Sectors[key]
	info := m.SectorToInfo[sector]
	if work.sectorSnapshot != nil {
		sector, info = work.sectorSnapshot.sector, work.sectorSnapshot.info
	}
	pointers := m.Allocations[xbm].Bricks[key]
	payload, auxiliary := m.voxelUploadReleases(work)
	for brick := range payload {
		m.releaseBrickSlot(brick)
	}
	for brick := range auxiliary {
		m.releaseVoxelAuxSlot(brick)
	}
	start, end := work.brickRange()
	for i := start; i < end; i++ {
		if work.kind == voxelUploadSector && work.sparseRecordSet && work.recordMask&(uint64(1)<<i) == 0 {
			continue
		}
		brick := work.desiredBrick(sector, i)
		recordIndex := info.BrickTableIndex + uint32(i)
		if info.packed != nil {
			mask := info.packed.mask
			if work.sectorSnapshot != nil {
				mask = work.sectorSnapshot.mask
			}
			recordIndex = info.BrickTableIndex + packedBrickRank(mask, i)
		}
		pointers[i] = brick
		if brick == nil {
			mustQueueVoxelWrite(m.writeVoxelBuffer(m.BrickTableBuf, uint64(recordIndex)*BrickRecordSize, make([]byte, BrickRecordSize)))
		} else {
			m.uploadBrick(context, work.object, xbm, brick, recordIndex, brickOriginForSectorIndex(key, i))
		}
	}
	if work.kind == voxelUploadSector {
		if work.sectorSnapshot != nil {
			m.writeSectorRecord(work.sectorSnapshot.coords, work.sectorSnapshot.mask, info)
		} else {
			m.writeSectorRecord(sector.Coords, sector.BrickMask64, info)
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
