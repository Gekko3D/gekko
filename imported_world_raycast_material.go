package gekko

import "github.com/gekko3d/gekko/content"

type ImportedWorldRaycastMaterialHit struct {
	RaycastHit
	ChunkRef AuthoredImportedWorldChunkRefComponent
	Material content.ImportedWorldMaterialDef
}

func ImportedWorldMaterialForRaycastHit(cmd *Commands, state *StreamedLevelRuntimeState, hit RaycastHit) (ImportedWorldRaycastMaterialHit, bool) {
	if cmd == nil || state == nil || !hit.Hit || hit.Entity == 0 || hit.PaletteIndex == 0 {
		return ImportedWorldRaycastMaterialHit{}, false
	}
	ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, hit.Entity)
	if !ok {
		return ImportedWorldRaycastMaterialHit{}, false
	}
	if state.BaseWorldID != "" && ref.WorldID != state.BaseWorldID {
		return ImportedWorldRaycastMaterialHit{}, false
	}
	material, ok := state.BaseWorldMaterialForPalette(hit.PaletteIndex)
	if !ok {
		return ImportedWorldRaycastMaterialHit{}, false
	}
	return ImportedWorldRaycastMaterialHit{
		RaycastHit: hit,
		ChunkRef:   ref,
		Material:   material,
	}, true
}
