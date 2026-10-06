package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"
)

// TerrainPageIndex keeps backing source membership independent of the visual
// forest. Legacy leaf ownership is compatibility metadata, not no-hole coverage.
type TerrainPageIndex struct {
	LegacyDistance    bool
	SourceOnly        bool
	Pages             []StreamPageDef
	RootPageIndices   []uint32
	Forest            *StreamPageForestIndex
	EntryIndexByCoord map[TerrainChunkCoordDef]int
}

func validateTerrainPageHeight(offset, scale float32) error {
	if !terrainFinite(offset) || !terrainFinite(scale) || scale <= 0 || !terrainFinite(offset+scale) || offset+scale <= offset {
		return fmt.Errorf("invalid terrain height calibration")
	}
	return nil
}

// NormalizeTerrainPages does not decode files or mutate authored defaults.
func NormalizeTerrainPages(d *TerrainChunkManifestDef) (*TerrainPageIndex, error) {
	if d == nil {
		return nil, fmt.Errorf("terrain manifest is nil")
	}
	if d.SchemaVersion == 3 {
		return ValidateTerrainPageManifest(d)
	}
	if d.SchemaVersion != 0 && d.SchemaVersion != 2 {
		return nil, fmt.Errorf("unsupported terrain manifest schema version %d", d.SchemaVersion)
	}
	if d.Pages != nil || d.RootPageIndices != nil {
		return nil, fmt.Errorf("legacy terrain cannot carry page fields")
	}
	if d.ChunkSize < 0 || !terrainFinite(d.VoxelResolution) || d.VoxelResolution < 0 {
		return nil, fmt.Errorf("invalid legacy terrain grid")
	}
	index := &TerrainPageIndex{LegacyDistance: true, EntryIndexByCoord: make(map[TerrainChunkCoordDef]int, len(d.Entries)), Forest: &StreamPageForestIndex{LeafOwnerPageIndices: make([]int, len(d.Entries))}}
	for i, e := range d.Entries {
		if _, ok := index.EntryIndexByCoord[e.Coord]; ok {
			return nil, fmt.Errorf("duplicate legacy terrain coordinate")
		}
		index.EntryIndexByCoord[e.Coord] = i
		index.Forest.LeafOwnerPageIndices[i] = -1
		size, res := e.ChunkSize, e.VoxelResolution
		if size == 0 {
			size = d.ChunkSize
		}
		if res == 0 {
			res = d.VoxelResolution
		}
		side := float32(size) * res
		if size <= 0 || !terrainFinite(res) || res <= 0 || !terrainFinite(side) || side <= 0 || e.NonEmptyVoxelCount < 0 || e.OccupiedSectorCount < 0 || e.OccupiedBrickCount < 0 || e.PayloadSizeBytes < 0 {
			return nil, fmt.Errorf("invalid legacy terrain entry grid/count")
		}
		origin := [3]float32{float32(e.Coord.X) * side, float32(e.Coord.Y) * side, float32(e.Coord.Z) * side}
		bounds := StreamPageBounds{Min: origin, Max: [3]float32{origin[0] + side, origin[1] + float32(e.NonEmptyVoxelCount)*res, origin[2] + side}}
		if !validStreamPageBounds(bounds, false) || bounds.Max[0] <= origin[0] || bounds.Max[2] <= origin[2] || (e.NonEmptyVoxelCount > 0 && bounds.Max[1] <= origin[1]) {
			return nil, fmt.Errorf("legacy terrain bounds overflow or lose precision")
		}
		if e.NonEmptyVoxelCount == 0 {
			continue
		}
		payload := StreamPagePayloadDef{Kind: e.PayloadKind, Path: e.ChunkPath, WorldOrigin: origin, ChunkSize: size, VoxelResolution: res, PayloadHash: e.PayloadHash, PayloadSizeBytes: e.PayloadSizeBytes, OccupiedSectorCount: e.OccupiedSectorCount, OccupiedBrickCount: e.OccupiedBrickCount}
		page := uint32(len(index.Pages))
		index.Pages = append(index.Pages, StreamPageDef{Level: StreamPageLevelLeaf, BoundsMin: bounds.Min, BoundsMax: bounds.Max, Payload: payload, LeafEntryIndices: []uint32{uint32(i)}})
		index.RootPageIndices = append(index.RootPageIndices, page)
		index.Forest.ParentPageIndices = append(index.Forest.ParentPageIndices, -1)
		index.Forest.LeafOwnerPageIndices[i] = int(page)
	}
	return index, nil
}

// Strict visual manifests must preserve distinct float32 cell centers throughout
// each bounded tile. Established source-only metadata keeps its compatibility.
func terrainPageCellProgress(origin, spacing float32, size int) bool {
	end := origin + float32(size)*spacing
	previous := origin
	for i := 0; i < size; i++ {
		center := origin + (float32(i)+.5)*spacing
		if !terrainFinite(center) || center <= previous || center >= end {
			return false
		}
		previous = center
	}
	return true
}

type terrainPagePathIdentity struct {
	Coord                              TerrainChunkCoordDef
	Origin                             [3]float32
	Size                               int
	Spacing, HeightOffset, HeightScale float32
	Kind, Hash                         string
	Bytes                              int
}

func terrainPagePathCheck(paths map[string]terrainPagePathIdentity, path string, identity terrainPagePathIdentity) error {
	key := filepath.Clean(filepath.FromSlash(path))
	if previous, ok := paths[key]; ok && previous != identity {
		return fmt.Errorf("terrain payload path has conflicting qualified headers: %s", path)
	}
	paths[key] = identity
	return nil
}

func terrainPagePayloadCoverage(p StreamPagePayloadDef) (StreamPageBounds, TerrainChunkCoordDef, error) {
	fail := func(s string) (StreamPageBounds, TerrainChunkCoordDef, error) {
		return StreamPageBounds{}, TerrainChunkCoordDef{}, fmt.Errorf("terrain page %s", s)
	}
	if p.Kind != TerrainHeightTilePayloadKind || p.Aux != nil || p.ChunkSize < 1 || p.ChunkSize > 128 || !terrainFinite(p.SampleSpacing) || p.SampleSpacing <= 0 || !terrainFinite(p.VoxelResolution) || p.VoxelResolution <= 0 || p.OccupiedSectorCount < 0 || p.OccupiedBrickCount < 0 || strings.TrimSpace(p.Path) == "" || !terrainPayloadHashValid(p.PayloadHash) {
		return fail("invalid payload/grid/identity")
	}
	if err := validateTerrainPageHeight(p.HeightOffset, p.HeightScale); err != nil {
		return StreamPageBounds{}, TerrainChunkCoordDef{}, err
	}
	ratio := float64(p.SampleSpacing) / float64(p.VoxelResolution)
	if ratio < 1 || !pageRatioIntegral(ratio) {
		return fail("visual resolution does not divide sample spacing")
	}
	n := p.ChunkSize * p.ChunkSize
	if p.PayloadSizeBytes != 2*n && p.PayloadSizeBytes != 2*n+(n+7)/8 {
		return fail("invalid render payload body size")
	}
	if p.WorldOrigin[1] != 0 {
		return fail("world origin Y must be zero")
	}
	span64 := float64(p.ChunkSize) * float64(p.SampleSpacing)
	span := float32(p.ChunkSize) * p.SampleSpacing
	if !terrainFinite(span) || span <= 0 {
		return fail("span overflow")
	}
	coord := TerrainChunkCoordDef{}
	components := []*int{&coord.X, &coord.Z}
	limit := math.Ldexp(1, strconv.IntSize-1)
	for i, axis := range []int{0, 2} {
		v := p.WorldOrigin[axis]
		q := math.Round(float64(v) / span64)
		if !terrainFinite(v) || math.IsNaN(q) || math.IsInf(q, 0) || q < -limit || q >= limit || float32(q*span64) != v {
			return fail("origin is outside signed payload lattice")
		}
		if !terrainPageCellProgress(v, p.SampleSpacing, p.ChunkSize) {
			return fail("cell centers lose float32 precision")
		}
		*components[i] = int(q)
	}
	bounds := StreamPageBounds{Min: [3]float32{p.WorldOrigin[0], p.HeightOffset, p.WorldOrigin[2]}, Max: [3]float32{p.WorldOrigin[0] + span, p.HeightOffset + p.HeightScale, p.WorldOrigin[2] + span}}
	if !validStreamPageBounds(bounds, true) {
		return fail("coverage overflows or collapses")
	}
	return bounds, coord, nil
}
func pageRatioIntegral(ratio float64) bool {
	// Float32 decimal inputs need a small relative allowance, but a large
	// quotient must never turn a genuine fractional cell into an integer.
	tolerance := math.Min(1e-6*math.Max(1, math.Abs(ratio)), 1e-4)
	return !math.IsNaN(ratio) && !math.IsInf(ratio, 0) && math.Abs(ratio-math.Round(ratio)) <= tolerance
}

// ValidateTerrainPageManifest accepts the established source-only v3 format and
// optional root/macro/regional visual pages. Source entries are shared backing
// dependencies; they are never passed as visual leaves to the forest validator.
func ValidateTerrainPageManifest(d *TerrainChunkManifestDef) (*TerrainPageIndex, error) {
	if err := validateTerrainHeightTileManifestSource(d); err != nil {
		return nil, err
	}
	index := &TerrainPageIndex{SourceOnly: len(d.Pages) == 0 && len(d.RootPageIndices) == 0, EntryIndexByCoord: make(map[TerrainChunkCoordDef]int, len(d.Entries)), Forest: &StreamPageForestIndex{}}
	for i, e := range d.Entries {
		index.EntryIndexByCoord[e.Coord] = i
	}
	if index.SourceOnly {
		return index, nil
	}
	if !terrainPayloadHashValid(d.SourceHash) {
		return nil, fmt.Errorf("paged terrain requires canonical SHA256 generation")
	}
	paths := make(map[string]terrainPagePathIdentity, len(d.Entries)+len(d.Pages))
	for _, e := range d.Entries {
		if err := validateTerrainPageHeight(e.HeightOffset, e.HeightScale); err != nil {
			return nil, err
		}
		for _, axis := range []int{0, 2} {
			if !terrainPageCellProgress(e.WorldOrigin[axis], e.VoxelResolution, e.ChunkSize) {
				return nil, fmt.Errorf("terrain source cell centers lose float32 precision")
			}
		}
		if err := terrainPagePathCheck(paths, e.ChunkPath, terrainPagePathIdentity{e.Coord, e.WorldOrigin, e.ChunkSize, e.VoxelResolution, e.HeightOffset, e.HeightScale, e.PayloadKind, e.PayloadHash, e.PayloadSizeBytes}); err != nil {
			return nil, err
		}
		span := float32(e.ChunkSize) * e.VoxelResolution
		bounds := StreamPageBounds{Min: [3]float32{e.WorldOrigin[0], e.HeightOffset, e.WorldOrigin[2]}, Max: [3]float32{e.WorldOrigin[0] + span, e.HeightOffset + e.HeightScale, e.WorldOrigin[2] + span}}
		if !validStreamPageBounds(bounds, true) {
			return nil, fmt.Errorf("terrain source coverage overflows or collapses")
		}
	}
	payloadBounds := make([]StreamPageBounds, len(d.Pages))
	for i, p := range d.Pages {
		if p.Level < StreamPageLevelRegional || p.Level > StreamPageLevelRoot || len(p.LeafEntryIndices) > 0 {
			return nil, fmt.Errorf("terrain visual pages cannot own source leaves")
		}
		bounds, coord, err := terrainPagePayloadCoverage(p.Payload)
		if err != nil {
			return nil, err
		}
		v := p.Payload
		if err := terrainPagePathCheck(paths, v.Path, terrainPagePathIdentity{coord, v.WorldOrigin, v.ChunkSize, v.SampleSpacing, v.HeightOffset, v.HeightScale, v.Kind, v.PayloadHash, v.PayloadSizeBytes}); err != nil {
			return nil, err
		}
		payloadBounds[i] = bounds
	}
	for _, root := range d.RootPageIndices {
		if uint64(root) >= uint64(len(d.Pages)) || d.Pages[root].Level != StreamPageLevelRoot {
			return nil, fmt.Errorf("terrain root index must identify root tier")
		}
	}
	forest, err := ValidateStreamPageForest(d.Pages, d.RootPageIndices, nil, payloadBounds)
	if err != nil {
		return nil, err
	}
	index.Forest = forest
	index.Pages = cloneStreamPages(d.Pages)
	index.RootPageIndices = append([]uint32(nil), d.RootPageIndices...)
	return index, nil
}

// LoadTerrainHeightPagePayload qualifies a bounded decoded height body against
// its generation, page lattice and calibration. Render pages never own nav data.
func LoadTerrainHeightPagePayload(d *TerrainChunkManifestDef, manifestPath string, pageIndex uint32) (*TerrainHeightTileDef, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("terrain page manifest path is empty")
	}
	if _, err := ValidateTerrainPageManifest(d); err != nil {
		return nil, err
	}
	if uint64(pageIndex) >= uint64(len(d.Pages)) {
		return nil, fmt.Errorf("terrain page index out of range")
	}
	p := d.Pages[pageIndex].Payload
	_, coord, err := terrainPagePayloadCoverage(p)
	if err != nil {
		return nil, err
	}
	tile, result, err := loadTerrainHeightTile(ResolveDocumentPath(p.Path, manifestPath))
	if err != nil {
		return nil, err
	}
	if tile.TerrainID != d.TerrainID || tile.SourceHash != d.SourceHash || tile.Coord != coord || tile.WorldOrigin != p.WorldOrigin || tile.SampleWidth != p.ChunkSize || tile.SampleHeight != p.ChunkSize || tile.SampleSpacing != p.SampleSpacing || tile.HeightOffset != p.HeightOffset || tile.HeightScale != p.HeightScale || result.PayloadHash != p.PayloadHash || result.PayloadSizeBytes != p.PayloadSizeBytes || tile.OutdoorNavCellSize != 0 || len(tile.OutdoorNavExclusionMask) != 0 {
		return nil, fmt.Errorf("terrain page owner/grid/calibration/payload mismatch")
	}
	return tile, nil
}

// Terrain v2 has no serialized page reference fields, including empty arrays.
func (d *TerrainChunkManifestDef) UnmarshalJSON(data []byte) error {
	type plain TerrainChunkManifestDef
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("terrain manifest must be a JSON object")
	}
	var version int
	seenVersion := false
	var pageKeys []string
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid terrain manifest key")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		if strings.EqualFold(key, "schema_version") {
			if seenVersion {
				return fmt.Errorf("duplicate terrain schema version field")
			}
			seenVersion = true
			if err := json.Unmarshal(raw, &version); err != nil {
				return err
			}
		}
		if strings.EqualFold(key, "pages") || strings.EqualFold(key, "root_page_indices") {
			pageKeys = append(pageKeys, key)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if version != 0 && version != 2 && version != 3 {
		return fmt.Errorf("unsupported terrain manifest schema version %d", version)
	}
	if version != 3 && len(pageKeys) > 0 {
		return fmt.Errorf("legacy terrain cannot carry %s", pageKeys[0])
	}
	var out plain
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*d = TerrainChunkManifestDef(out)
	return nil
}
