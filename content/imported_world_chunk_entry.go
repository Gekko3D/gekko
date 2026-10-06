package content

import (
	"fmt"
	"strings"
)

// LoadImportedWorldChunkEntry loads one fully qualified binary source reference.
// It checks declared ownership and the bounded grid before expanding voxels, and
// qualifies any normal sidecar without decoding the other entries or forest.
// Legacy optional hash/size metadata must be populated before using this API.
func LoadImportedWorldChunkEntry(manifest *ImportedWorldDef, manifestPath string, entryIndex uint32) (*ImportedWorldChunkDef, error) {
	if manifest == nil || strings.TrimSpace(manifestPath) == "" || strings.TrimSpace(manifest.WorldID) == "" || manifest.Kind != ImportedWorldKindVoxelWorld || manifest.SchemaVersion < 0 || manifest.SchemaVersion > ImportedWorldPageSchemaVersion || manifest.ChunkSize < 1 || manifest.ChunkSize > ImportedWorldPageMaxPayloadSide || !terrainFinite(manifest.VoxelResolution) || manifest.VoxelResolution <= 0 || uint64(entryIndex) >= uint64(len(manifest.Entries)) {
		return nil, fmt.Errorf("invalid bounded imported full entry context")
	}
	e := manifest.Entries[entryIndex]
	if strings.TrimSpace(e.ChunkPath) == "" || !importedVoxelPayloadKind(e.PayloadKind) || !importedPageHashValid(e.PayloadHash) || e.PayloadSizeBytes <= 0 || e.NonEmptyVoxelCount < 0 || e.NonEmptyVoxelCount > manifest.ChunkSize*manifest.ChunkSize*manifest.ChunkSize || e.OccupiedSectorCount < 0 || e.OccupiedBrickCount < 0 {
		return nil, fmt.Errorf("invalid qualified imported full entry reference")
	}
	sectorSide, brickSide := (manifest.ChunkSize+31)/32, (manifest.ChunkSize+7)/8
	if e.OccupiedSectorCount > sectorSide*sectorSide*sectorSide || e.OccupiedBrickCount > brickSide*brickSide*brickSide {
		return nil, fmt.Errorf("imported full entry costs exceed grid")
	}
	if _, err := importedPageGridBounds(e.Coord, manifest.ChunkSize, manifest.VoxelResolution); err != nil {
		return nil, err
	}
	if err := validateImportedPageAux(e.Aux, e.PayloadHash, e.PayloadSizeBytes); err != nil {
		return nil, err
	}
	q := importedPageQualification{worldID: manifest.WorldID, coord: e.Coord, side: manifest.ChunkSize, resolution: manifest.VoxelResolution, kind: e.PayloadKind, hash: e.PayloadHash, size: e.PayloadSizeBytes, count: e.NonEmptyVoxelCount, sectorCost: e.OccupiedSectorCount, brickCost: e.OccupiedBrickCount}
	if e.Aux != nil {
		if err := loadQualifiedImportedPageAux(ResolveDocumentPath(e.Aux.AuxPath, manifestPath), q, e.Aux); err != nil {
			return nil, err
		}
	}
	return loadQualifiedImportedPage(ResolveImportedWorldChunkPath(e, manifestPath), q)
}
