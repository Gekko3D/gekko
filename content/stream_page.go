package content

import (
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	StreamPageLevelLeaf uint8 = iota
	StreamPageLevelRegional
	StreamPageLevelMacro
	StreamPageLevelRoot
)

// StreamPageDef references manifest-local page and leaf array indices. These
// indices are qualified by that manifest's source identity, never save IDs.
type StreamPageDef struct {
	Level            uint8                `json:"level"`
	BoundsMin        [3]float32           `json:"bounds_min"`
	BoundsMax        [3]float32           `json:"bounds_max"`
	Payload          StreamPagePayloadDef `json:"payload"`
	ChildPageIndices []uint32             `json:"child_page_indices,omitempty"`
	LeafEntryIndices []uint32             `json:"leaf_entry_indices,omitempty"`
	CoverageGroup    string               `json:"coverage_group,omitempty"`
	Tags             []string             `json:"tags,omitempty"`
}

// StreamPagePayloadDef describes a referenced fallback payload. Its qualified
// world coverage is supplied separately by the owning decoder or compiler.
type StreamPagePayloadDef struct {
	Aux                 *ImportedWorldChunkAuxRefDef `json:"aux,omitempty"`
	Kind                string                       `json:"kind"`
	Path                string                       `json:"path"`
	WorldOrigin         [3]float32                   `json:"world_origin"`
	ChunkSize           int                          `json:"chunk_size"`
	VoxelResolution     float32                      `json:"voxel_resolution"`
	SampleSpacing       float32                      `json:"sample_spacing,omitempty"`
	PayloadHash         string                       `json:"payload_hash"`
	PayloadSizeBytes    int                          `json:"payload_size_bytes"`
	OccupiedSectorCount int                          `json:"occupied_sector_count,omitempty"`
	OccupiedBrickCount  int                          `json:"occupied_brick_count,omitempty"`
}

// ImportedWorldSectorV3Def uses manifest-local indices for visibility and full
// chunk membership without assigning persistent identities to array positions.
type ImportedWorldSectorV3Def struct {
	Coord                 TerrainChunkCoordDef `json:"coord"`
	BoundsMin             [3]float32           `json:"bounds_min"`
	BoundsMax             [3]float32           `json:"bounds_max"`
	FullChunkIndices      []uint32             `json:"full_chunk_indices,omitempty"`
	VisibilityID          string               `json:"visibility_id,omitempty"`
	SourceLeafIDs         []int                `json:"source_leaf_ids,omitempty"`
	VisibleSectorIndices  []uint32             `json:"visible_sector_indices,omitempty"`
	AdjacentSectorIndices []uint32             `json:"adjacent_sector_indices,omitempty"`
	Tags                  []string             `json:"tags,omitempty"`
}

// StreamPageBounds is finite qualified world coverage, which may be flat.
type StreamPageBounds struct{ Min, Max [3]float32 }

// StreamPageLeaf identifies whether a qualified leaf requires page ownership.
type StreamPageLeaf struct {
	Bounds   StreamPageBounds
	NonEmpty bool
}

// StreamPageForestIndex records immediate parents and leaf owners; -1 denotes
// a root or an unowned empty leaf respectively.
type StreamPageForestIndex struct {
	ParentPageIndices    []int
	LeafOwnerPageIndices []int
}

func validStreamPageBounds(bounds StreamPageBounds, positive bool) bool {
	for axis := 0; axis < 3; axis++ {
		if !terrainFinite(bounds.Min[axis]) || !terrainFinite(bounds.Max[axis]) || bounds.Min[axis] > bounds.Max[axis] || (positive && bounds.Min[axis] == bounds.Max[axis]) {
			return false
		}
	}
	return true
}

func containsStreamPageBounds(outer, inner StreamPageBounds) bool {
	for axis := 0; axis < 3; axis++ {
		if inner.Min[axis] < outer.Min[axis] || inner.Max[axis] > outer.Max[axis] {
			return false
		}
	}
	return true
}

func validateStreamPagePayload(payload StreamPagePayloadDef) error {
	if len(payload.PayloadHash) != 64 {
		return fmt.Errorf("invalid stream page payload hash length")
	}
	_, hashError := hex.DecodeString(payload.PayloadHash)
	if strings.TrimSpace(payload.Path) == "" || hashError != nil || payload.PayloadSizeBytes <= 0 || payload.OccupiedSectorCount < 0 || payload.OccupiedBrickCount < 0 || payload.ChunkSize <= 0 {
		return fmt.Errorf("invalid stream page payload metadata")
	}
	for _, v := range payload.WorldOrigin {
		if !terrainFinite(v) {
			return fmt.Errorf("invalid stream page payload origin")
		}
	}
	if !terrainFinite(payload.VoxelResolution) || payload.VoxelResolution < 0 || !terrainFinite(payload.SampleSpacing) || payload.SampleSpacing < 0 {
		return fmt.Errorf("invalid stream page payload grid")
	}
	if payload.Kind == TerrainHeightTilePayloadKind {
		if payload.Aux != nil {
			return fmt.Errorf("height stream page cannot use voxel normal aux")
		}
		if payload.ChunkSize > 128 || payload.SampleSpacing <= 0 {
			return fmt.Errorf("invalid height stream page grid")
		}
	} else {
		kind, err := NormalizeImportedWorldChunkPayloadKind(payload.Kind)
		if err != nil || payload.Kind == "" || kind != payload.Kind || payload.VoxelResolution <= 0 {
			return fmt.Errorf("invalid voxel stream page payload kind or grid")
		}
	}
	if payload.Aux != nil {
		if err := validateImportedPageAux(payload.Aux, payload.PayloadHash, payload.PayloadSizeBytes); err != nil {
			return err
		}
	}
	return nil
}

// ValidateStreamPageForest validates a strict page forest without file I/O.
// Payload coverage is supplied by its owning decoder; height coverage is not
// inferred as a cubic voxel grid. Pages have positive volume, while qualified
// payload and leaf bounds may describe flat surfaces.
func ValidateStreamPageForest(pages []StreamPageDef, roots []uint32, leaves []StreamPageLeaf, payloadBounds []StreamPageBounds) (*StreamPageForestIndex, error) {
	if len(payloadBounds) != len(pages) {
		return nil, fmt.Errorf("stream page payload coverage count mismatch")
	}
	index := &StreamPageForestIndex{ParentPageIndices: make([]int, len(pages)), LeafOwnerPageIndices: make([]int, len(leaves))}
	for i := range index.ParentPageIndices {
		index.ParentPageIndices[i] = -1
	}
	for i := range index.LeafOwnerPageIndices {
		index.LeafOwnerPageIndices[i] = -1
	}
	for _, leaf := range leaves {
		if !validStreamPageBounds(leaf.Bounds, false) {
			return nil, fmt.Errorf("invalid stream leaf bounds")
		}
	}
	for i, page := range pages {
		bounds := StreamPageBounds{Min: page.BoundsMin, Max: page.BoundsMax}
		if page.Level > StreamPageLevelRoot || !validStreamPageBounds(bounds, true) || !validStreamPageBounds(payloadBounds[i], false) || !containsStreamPageBounds(bounds, payloadBounds[i]) {
			return nil, fmt.Errorf("invalid stream page level or coverage")
		}
		if err := validateStreamPagePayload(page.Payload); err != nil {
			return nil, err
		}
		if len(page.ChildPageIndices) > 0 && len(page.LeafEntryIndices) > 0 {
			return nil, fmt.Errorf("stream page mixes child and leaf references")
		}
	}
	for i, page := range pages {
		bounds := StreamPageBounds{Min: page.BoundsMin, Max: page.BoundsMax}
		for _, ref := range page.ChildPageIndices {
			if uint64(ref) >= uint64(len(pages)) {
				return nil, fmt.Errorf("stream child index out of range")
			}
			child := pages[int(ref)]
			if index.ParentPageIndices[int(ref)] != -1 || page.Level <= child.Level || !containsStreamPageBounds(bounds, StreamPageBounds{Min: child.BoundsMin, Max: child.BoundsMax}) {
				return nil, fmt.Errorf("invalid stream child ownership, level or coverage")
			}
			index.ParentPageIndices[int(ref)] = i
		}
		for _, ref := range page.LeafEntryIndices {
			if uint64(ref) >= uint64(len(leaves)) {
				return nil, fmt.Errorf("stream leaf index out of range")
			}
			if index.LeafOwnerPageIndices[int(ref)] != -1 || !containsStreamPageBounds(bounds, leaves[int(ref)].Bounds) {
				return nil, fmt.Errorf("invalid stream leaf ownership or coverage")
			}
			index.LeafOwnerPageIndices[int(ref)] = i
		}
	}
	rootSet := make([]bool, len(pages))
	for _, ref := range roots {
		if uint64(ref) >= uint64(len(pages)) || rootSet[int(ref)] || index.ParentPageIndices[int(ref)] != -1 {
			return nil, fmt.Errorf("invalid stream root ownership")
		}
		rootSet[int(ref)] = true
	}
	// Strict level descent already excludes cycles. Every chain must end at
	// one listed root, including payload-only background pages.
	for i := range pages {
		root := i
		for index.ParentPageIndices[root] != -1 {
			root = index.ParentPageIndices[root]
		}
		if !rootSet[root] {
			return nil, fmt.Errorf("unreachable stream page")
		}
	}
	for i, leaf := range leaves {
		if leaf.NonEmpty && index.LeafOwnerPageIndices[i] == -1 {
			return nil, fmt.Errorf("unowned nonempty stream leaf")
		}
	}
	return index, nil
}
