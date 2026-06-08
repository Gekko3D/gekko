package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func (state *StreamedLevelRuntimeState) BaseWorldMaterialForPalette(paletteIndex uint8) (content.ImportedWorldMaterialDef, bool) {
	if state == nil {
		return content.ImportedWorldMaterialDef{}, false
	}
	state.mu.RLock()
	lookup := state.BaseWorldMaterialLookup
	state.mu.RUnlock()
	return lookup.MaterialForPalette(paletteIndex)
}

func (state *StreamedLevelRuntimeState) BaseWorldMaterialForChunkVoxel(chunk *content.ImportedWorldChunkDef, local [3]int) (content.ImportedWorldMaterialDef, uint8, bool) {
	if state == nil {
		return content.ImportedWorldMaterialDef{}, 0, false
	}
	state.mu.RLock()
	lookup := state.BaseWorldMaterialLookup
	state.mu.RUnlock()
	return lookup.MaterialForChunkVoxel(chunk, local)
}

func (state *StreamedLevelRuntimeState) BaseWorldMaterialForXBrickMapVoxel(xbm *volume.XBrickMap, local [3]int) (content.ImportedWorldMaterialDef, uint8, bool) {
	if state == nil {
		return content.ImportedWorldMaterialDef{}, 0, false
	}
	state.mu.RLock()
	lookup := state.BaseWorldMaterialLookup
	state.mu.RUnlock()
	return lookup.MaterialForXBrickMapVoxel(xbm, local)
}
