package content

import (
	"encoding/hex"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

func importedPageHashValid(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}
func importedPageHashEqual(a, b string) bool {
	return importedPageHashValid(a) && importedPageHashValid(b) && strings.EqualFold(a, b)
}
func importedVoxelPayloadKind(kind string) bool {
	normalized, err := NormalizeImportedWorldChunkPayloadKind(kind)
	return err == nil && kind != "" && kind != ImportedWorldChunkPayloadSparseJSONV1 && normalized == kind
}
func validateImportedPageAux(ref *ImportedWorldChunkAuxRefDef, hash string, size int) error {
	if ref == nil {
		return nil
	}
	if strings.TrimSpace(ref.AuxPath) == "" || ref.PayloadKind != ImportedWorldChunkAuxPayloadBinaryV1 || !importedPageHashValid(ref.PayloadHash) || ref.PayloadSizeBytes <= 0 || ref.NormalBakeVersion != ImportedWorldNormalBakeVersion || !importedPageHashEqual(ref.SourcePayloadHash, hash) || ref.SourcePayloadSizeBytes != size {
		return fmt.Errorf("invalid imported page normal aux reference")
	}
	return nil
}
func importedPayloadCube(origin [3]float32, size int, resolution float32) (StreamPageBounds, error) {
	if size <= 0 || size > ImportedWorldPageMaxPayloadSide || !terrainFinite(resolution) || resolution <= 0 {
		return StreamPageBounds{}, fmt.Errorf("invalid bounded imported payload grid")
	}
	side := float32(size) * resolution
	bounds := StreamPageBounds{Min: origin}
	for axis := 0; axis < 3; axis++ {
		bounds.Max[axis] = origin[axis] + side
	}
	if !validStreamPageBounds(bounds, true) {
		return StreamPageBounds{}, fmt.Errorf("imported payload cube overflows or loses precision")
	}
	return bounds, nil
}
func cloneImportedAux(ref *ImportedWorldChunkAuxRefDef) *ImportedWorldChunkAuxRefDef {
	if ref == nil {
		return nil
	}
	copy := *ref
	return &copy
}
func cloneStreamPages(pages []StreamPageDef) []StreamPageDef {
	out := append([]StreamPageDef(nil), pages...)
	for i := range out {
		out[i].ChildPageIndices = append([]uint32(nil), pages[i].ChildPageIndices...)
		out[i].LeafEntryIndices = append([]uint32(nil), pages[i].LeafEntryIndices...)
		out[i].Tags = append([]string(nil), pages[i].Tags...)
		out[i].Payload.Aux = cloneImportedAux(pages[i].Payload.Aux)
	}
	return out
}
func cloneIndexedSectors(sectors []ImportedWorldSectorV3Def) []ImportedWorldSectorV3Def {
	out := append([]ImportedWorldSectorV3Def(nil), sectors...)
	for i := range out {
		out[i].FullChunkIndices = append([]uint32(nil), sectors[i].FullChunkIndices...)
		out[i].VisibleSectorIndices = append([]uint32(nil), sectors[i].VisibleSectorIndices...)
		out[i].AdjacentSectorIndices = append([]uint32(nil), sectors[i].AdjacentSectorIndices...)
		out[i].SourceLeafIDs = append([]int(nil), sectors[i].SourceLeafIDs...)
		out[i].Tags = append([]string(nil), sectors[i].Tags...)
	}
	return out
}

// ValidateImportedWorldV3 validates declared metadata and coverage without payload I/O.
// Visibility sectors may share membership; the page forest alone owns visual leaves.
func ValidateImportedWorldV3(d *ImportedWorldDef) (*ImportedWorldPageIndex, error) {
	if d == nil || d.SchemaVersion != ImportedWorldPageSchemaVersion {
		return nil, fmt.Errorf("imported page world requires schema version 3")
	}
	if strings.TrimSpace(d.WorldID) == "" || !importedPageHashValid(d.SourceHash) || (d.Kind != "" && d.Kind != ImportedWorldKindVoxelWorld) || len(d.Sectors) > 0 {
		return nil, fmt.Errorf("invalid imported page world schema version 3 identity or sector representation")
	}
	if d.ChunkSize <= 0 || d.ChunkSize > ImportedWorldPageMaxPayloadSide || !terrainFinite(d.VoxelResolution) || d.VoxelResolution <= 0 {
		return nil, fmt.Errorf("invalid imported page world grid")
	}
	if uint64(len(d.Entries)) > math.MaxUint32 || uint64(len(d.Pages)) > math.MaxUint32 || uint64(len(d.IndexedSectors)) > math.MaxUint32 {
		return nil, fmt.Errorf("imported page index exceeds uint32 range")
	}
	index := &ImportedWorldPageIndex{Pages: cloneStreamPages(d.Pages), RootPageIndices: append([]uint32(nil), d.RootPageIndices...), Sectors: cloneIndexedSectors(d.IndexedSectors), EntryIndexByCoord: make(map[TerrainChunkCoordDef]int, len(d.Entries)), SectorIndexByCoord: make(map[TerrainChunkCoordDef]int, len(d.IndexedSectors))}
	leaves := make([]StreamPageLeaf, len(d.Entries))
	paths := make(map[string]int, len(d.Entries))
	maxCount := d.ChunkSize * d.ChunkSize * d.ChunkSize
	for i, e := range d.Entries {
		if _, duplicate := index.EntryIndexByCoord[e.Coord]; duplicate {
			return nil, fmt.Errorf("duplicate imported page leaf coordinate")
		}
		if strings.TrimSpace(e.ChunkPath) == "" || e.NonEmptyVoxelCount < 0 || e.NonEmptyVoxelCount > maxCount || e.OccupiedSectorCount < 0 || e.OccupiedBrickCount < 0 || e.PayloadSizeBytes < 0 {
			return nil, fmt.Errorf("invalid imported page leaf metadata")
		}
		if !importedVoxelPayloadKind(e.PayloadKind) || !importedPageHashValid(e.PayloadHash) || e.PayloadSizeBytes <= 0 {
			return nil, fmt.Errorf("imported page leaf lacks qualified payload")
		}
		if e.PayloadKind != "" && !importedVoxelPayloadKind(e.PayloadKind) {
			return nil, fmt.Errorf("invalid imported page leaf payload kind")
		}
		maxSectors := ((d.ChunkSize + 31) / 32) * ((d.ChunkSize + 31) / 32) * ((d.ChunkSize + 31) / 32)
		maxBricks := ((d.ChunkSize + 7) / 8) * ((d.ChunkSize + 7) / 8) * ((d.ChunkSize + 7) / 8)
		if e.OccupiedSectorCount > maxSectors || e.OccupiedBrickCount > maxBricks {
			return nil, fmt.Errorf("imported page leaf occupied cost exceeds grid")
		}
		if e.Aux != nil {
			if err := validateImportedPageAux(e.Aux, e.PayloadHash, e.PayloadSizeBytes); err != nil {
				return nil, err
			}
		}
		bounds, err := importedPageGridBounds(e.Coord, d.ChunkSize, d.VoxelResolution)
		if err != nil {
			return nil, err
		}
		leaves[i] = StreamPageLeaf{Bounds: bounds, NonEmpty: e.NonEmptyVoxelCount > 0}
		index.EntryIndexByCoord[e.Coord] = i
		path := cleanImportedPagePath(e.ChunkPath)
		if _, duplicate := paths[path]; duplicate {
			return nil, fmt.Errorf("imported page leaves share payload path")
		}
		paths[path] = i
	}
	payloadBounds := make([]StreamPageBounds, len(d.Pages))
	for i, p := range d.Pages {
		if p.Payload.HeightOffset != 0 || p.Payload.HeightScale != 0 {
			return nil, fmt.Errorf("voxel page cannot carry height calibration")
		}
		if !importedVoxelPayloadKind(p.Payload.Kind) {
			return nil, fmt.Errorf("imported world pages require voxel payload kinds")
		}
		bounds, err := importedPayloadCube(p.Payload.WorldOrigin, p.Payload.ChunkSize, p.Payload.VoxelResolution)
		if err != nil {
			return nil, err
		}
		payloadBounds[i] = bounds
		maxSectors := ((p.Payload.ChunkSize + 31) / 32) * ((p.Payload.ChunkSize + 31) / 32) * ((p.Payload.ChunkSize + 31) / 32)
		maxBricks := ((p.Payload.ChunkSize + 7) / 8) * ((p.Payload.ChunkSize + 7) / 8) * ((p.Payload.ChunkSize + 7) / 8)
		if p.Payload.OccupiedSectorCount > maxSectors || p.Payload.OccupiedBrickCount > maxBricks {
			return nil, fmt.Errorf("imported page payload occupied cost exceeds grid")
		}
		if leaf, shared := paths[cleanImportedPagePath(p.Payload.Path)]; shared {
			e := d.Entries[leaf]
			referenced := false
			for _, ref := range p.LeafEntryIndices {
				if uint64(ref) == uint64(leaf) {
					referenced = true
				}
			}
			if !referenced || p.Payload.ChunkSize != d.ChunkSize || p.Payload.VoxelResolution != d.VoxelResolution || p.Payload.WorldOrigin != leaves[leaf].Bounds.Min || p.Payload.Kind != e.PayloadKind || !importedPageHashEqual(p.Payload.PayloadHash, e.PayloadHash) || p.Payload.PayloadSizeBytes != e.PayloadSizeBytes {
				return nil, fmt.Errorf("shared imported leaf/page payload metadata mismatch")
			}
		}
	}
	forest, err := ValidateStreamPageForest(d.Pages, d.RootPageIndices, leaves, payloadBounds)
	if err != nil {
		return nil, err
	}
	index.Forest = forest
	for i, s := range d.IndexedSectors {
		if _, duplicate := index.SectorIndexByCoord[s.Coord]; duplicate {
			return nil, fmt.Errorf("duplicate indexed imported sector coordinate")
		}
		if !validStreamPageBounds(StreamPageBounds{Min: s.BoundsMin, Max: s.BoundsMax}, true) {
			return nil, fmt.Errorf("invalid indexed imported sector bounds")
		}
		index.SectorIndexByCoord[s.Coord] = i
	}
	for _, s := range d.IndexedSectors {
		for _, list := range []struct {
			refs []uint32
			n    int
		}{{s.FullChunkIndices, len(d.Entries)}, {s.VisibleSectorIndices, len(d.IndexedSectors)}, {s.AdjacentSectorIndices, len(d.IndexedSectors)}} {
			seen := make(map[uint32]bool, len(list.refs))
			for _, ref := range list.refs {
				if uint64(ref) >= uint64(list.n) || seen[ref] {
					return nil, fmt.Errorf("invalid or duplicate indexed sector reference")
				}
				seen[ref] = true
			}
		}
	}
	return index, nil
}

func cleanImportedPagePath(path string) string { return filepath.Clean(filepath.FromSlash(path)) }
