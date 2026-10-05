package gpu

import "github.com/gekko3d/gekko/voxelrt/rt/volume"

// Only admitted full-sector work owns this bounded topology capture. Brick data
// and normal bake inputs retain their existing runtime ownership contract.
type voxelSectorUploadSnapshot struct {
	newRange bool
	sector   *volume.Sector
	info     SectorGpuInfo
	coords   [3]int
	mask     uint64
	desired  [64]*volume.Brick
}

func (m *GpuBufferManager) captureSectorUpload(w voxelUploadWork) *voxelSectorUploadSnapshot {
	sector := w.targetMap().Sectors[w.sectorKey]
	s := &voxelSectorUploadSnapshot{sector: sector, info: m.SectorToInfo[sector], coords: sector.Coords, mask: sector.BrickMask64}
	for i := range s.desired {
		s.desired[i] = sector.GetBrick(i%4, (i/4)%4, i/16)
	}
	return s
}

func (w voxelUploadWork) desiredBrick(sector *volume.Sector, i int) *volume.Brick {
	if w.brickSnapshot != nil && i == w.brickSnapshot.index {
		return w.brickSnapshot.brick
	}
	if w.sectorSnapshot != nil {
		return w.sectorSnapshot.desired[i]
	}
	return sector.GetBrick(i%4, (i/4)%4, i/16)
}

func (s *voxelSectorUploadSnapshot) current(w voxelUploadWork) bool {
	sector := w.targetMap().Sectors[w.sectorKey]
	if sector != s.sector || sector.Coords != s.coords || sector.BrickMask64 != s.mask {
		return false
	}
	for i, brick := range s.desired {
		if sector.GetBrick(i%4, (i/4)%4, i/16) != brick {
			return false
		}
	}
	return true
}

// Ordinary dirty-brick updates capture one written pointer, never all 64 rows.
type voxelBrickUploadSnapshot struct {
	coords [3]int
	sector *volume.Sector
	mask   uint64
	index  int
	brick  *volume.Brick
}

func (m *GpuBufferManager) captureBrickUpload(w voxelUploadWork) *voxelBrickUploadSnapshot {
	sector := w.targetMap().Sectors[w.sectorCoordinate()]
	start, _ := w.brickRange()
	return &voxelBrickUploadSnapshot{sector.Coords, sector, sector.BrickMask64, start, sector.GetBrick(start%4, (start/4)%4, start/16)}
}
func (s *voxelBrickUploadSnapshot) current(w voxelUploadWork) bool {
	sector := w.targetMap().Sectors[w.sectorCoordinate()]
	return sector == s.sector && sector.Coords == s.coords && sector.BrickMask64 == s.mask && sector.GetBrick(s.index%4, (s.index/4)%4, s.index/16) == s.brick
}
