package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"sort"
	"sync"
	"unicode/utf8"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

const (
	CurrentVoxelObjectPayloadSchemaVersion = 2
	HybridVoxelObjectPayloadSchemaVersion  = 3
	VoxelObjectPayloadHybridDelta          = "hybrid_delta"
	VoxelObjectPayloadFull                 = "full"
	VoxelObjectPayloadBaseDelta            = "base_delta"
)

// VoxelObjectLatticeDef binds geometry to its authoritative authored lattice.
// Paths, placement transforms and renderer auxiliary data are not identity inputs.
type VoxelObjectLatticeDef struct {
	VoxelResolution      float32 `json:"voxel_resolution"`
	RasterizationVersion string  `json:"rasterization_version"`
}

// VoxelObjectPayloadDef contains full geometry, base-relative assignments, or
// schema-3 hybrid assignments with selected whole-brick replacements. Schema 1
// denotes legacy unbound full records.
type VoxelObjectPayloadDef struct {
	SchemaVersion     int                   `json:"schema_version"`
	Mode              string                `json:"mode"`
	PlacementID       string                `json:"placement_id"`
	ItemID            string                `json:"item_id"`
	Lattice           VoxelObjectLatticeDef `json:"lattice"`
	BaseIdentity      string                `json:"base_identity,omitempty"`
	ReplacementBricks [][3]int32            `json:"replacement_bricks,omitempty"`
	Voxels            []VoxelObjectVoxelDef `json:"voxels,omitempty"`
}

type voxelObjectPayloadMetadata struct {
	SchemaVersion     int                   `json:"schema_version"`
	Mode              string                `json:"mode"`
	PlacementID       string                `json:"placement_id"`
	ItemID            string                `json:"item_id"`
	Lattice           VoxelObjectLatticeDef `json:"lattice"`
	BaseIdentity      string                `json:"base_identity,omitempty"`
	ReplacementBricks [][3]int32            `json:"replacement_bricks,omitempty"`
}

type voxelObjectBaseMetadata struct {
	Lattice VoxelObjectLatticeDef `json:"lattice"`
}

var defaultVoxelObjectPayloadCodec = sync.OnceValues(func() (*voxelcodec.Codec, error) { return voxelcodec.New(voxelcodec.Options{}) })

func voxelObjectCodec(codec *voxelcodec.Codec) (*voxelcodec.Codec, error) {
	if codec != nil {
		return codec, nil
	}
	return defaultVoxelObjectPayloadCodec()
}

func validVoxelObjectText(value string, limit int) bool {
	return len(value) > 0 && len(value) <= limit && utf8.ValidString(value)
}

func validateVoxelObjectLattice(lattice VoxelObjectLatticeDef) error {
	if lattice.VoxelResolution <= 0 || math.IsNaN(float64(lattice.VoxelResolution)) || math.IsInf(float64(lattice.VoxelResolution), 0) || !validVoxelObjectText(lattice.RasterizationVersion, 128) {
		return fmt.Errorf("invalid voxel-object lattice/rasterization version")
	}
	return nil
}

func voxelObjectMetadata(payload *VoxelObjectPayloadDef) (voxelObjectPayloadMetadata, error) {
	if payload == nil {
		return voxelObjectPayloadMetadata{}, fmt.Errorf("voxel-object payload is nil")
	}
	version := payload.SchemaVersion
	if version == 0 {
		version = CurrentVoxelObjectPayloadSchemaVersion
	}
	m := voxelObjectPayloadMetadata{SchemaVersion: version, Mode: payload.Mode, PlacementID: payload.PlacementID, ItemID: payload.ItemID, Lattice: payload.Lattice, BaseIdentity: payload.BaseIdentity, ReplacementBricks: payload.ReplacementBricks}
	if (m.SchemaVersion != CurrentVoxelObjectPayloadSchemaVersion && m.SchemaVersion != HybridVoxelObjectPayloadSchemaVersion) || !validVoxelObjectText(m.PlacementID, 1024) || !validVoxelObjectText(m.ItemID, 1024) {
		return m, fmt.Errorf("invalid voxel-object payload schema/owner")
	}
	if m.SchemaVersion == HybridVoxelObjectPayloadSchemaVersion {
		if m.Mode != VoxelObjectPayloadHybridDelta {
			return m, fmt.Errorf("unsupported hybrid voxel-object mode")
		}
		if err := validateVoxelObjectSelectors(m.ReplacementBricks); err != nil {
			return m, err
		}
	} else if m.Mode == VoxelObjectPayloadHybridDelta || len(m.ReplacementBricks) > 0 {
		return m, fmt.Errorf("replacement selectors require hybrid schema")
	}
	if err := validateVoxelObjectLattice(m.Lattice); err != nil {
		return m, err
	}
	switch m.Mode {
	case VoxelObjectPayloadFull:
		if m.BaseIdentity != "" {
			return m, fmt.Errorf("full voxel-object payload has base identity")
		}
	case VoxelObjectPayloadBaseDelta, VoxelObjectPayloadHybridDelta:
		if len(m.BaseIdentity) != 64 {
			return m, fmt.Errorf("invalid voxel-object base identity")
		}
		for i := 0; i < len(m.BaseIdentity); i++ {
			char := m.BaseIdentity[i]
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
				return m, fmt.Errorf("noncanonical voxel-object base identity")
			}
		}
	default:
		return m, fmt.Errorf("unsupported voxel-object payload mode %q", m.Mode)
	}
	return m, nil
}

// ValidateVoxelObjectPayloadMetadata validates schema and scalar owner/lattice
// bindings without inspecting records or allocating for valid metadata.
func ValidateVoxelObjectPayloadMetadata(payload *VoxelObjectPayloadDef) error {
	_, err := voxelObjectMetadata(payload)
	return err
}

// VoxelObjectPayloadMetadataSize validates canonical typed metadata and returns
// its JSON byte length without inspecting voxel records or modifying the input.
func VoxelObjectPayloadMetadataSize(payload *VoxelObjectPayloadDef) (int, error) {
	metadata, err := voxelObjectMetadata(payload)
	if err != nil {
		return 0, err
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func voxelObjectVoxelLess(a, b VoxelObjectVoxelDef) bool {
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.X < b.X
}

func validateVoxelObjectCoordinate(v VoxelObjectVoxelDef) error {
	for _, coordinate := range [3]int{v.X, v.Y, v.Z} {
		if int64(coordinate) < math.MinInt32 || int64(coordinate) > math.MaxInt32 {
			return fmt.Errorf("voxel-object coordinate outside signed int32")
		}
	}
	return nil
}

func voxelObjectBrickCoordinate(v int) (int32, int) {
	quotient, remainder := v/8, v%8
	if remainder < 0 {
		quotient--
		remainder += 8
	}
	return int32(quotient), remainder
}

// Conversion sorts copies of actual records; no coordinate-extent allocation.
func voxelObjectBricks(records []VoxelObjectVoxelDef, delta bool) ([]voxelcodec.Brick, error) {
	voxels := append([]VoxelObjectVoxelDef(nil), records...)
	sort.Slice(voxels, func(i, j int) bool { return voxelObjectVoxelLess(voxels[i], voxels[j]) })
	byCoordinate := make(map[[3]int32]*voxelcodec.Brick)
	for i, v := range voxels {
		if err := validateVoxelObjectCoordinate(v); err != nil {
			return nil, err
		}
		if !delta && v.Value == 0 {
			return nil, fmt.Errorf("full voxel-object geometry contains zero")
		}
		if i > 0 && v.X == voxels[i-1].X && v.Y == voxels[i-1].Y && v.Z == voxels[i-1].Z {
			return nil, fmt.Errorf("duplicate voxel-object assignment")
		}
		bx, x := voxelObjectBrickCoordinate(v.X)
		by, y := voxelObjectBrickCoordinate(v.Y)
		bz, z := voxelObjectBrickCoordinate(v.Z)
		key := [3]int32{bx, by, bz}
		brick := byCoordinate[key]
		if brick == nil {
			brick = &voxelcodec.Brick{Coord: key}
			byCoordinate[key] = brick
		}
		linear := x + 8*y + 64*z
		brick.Occupancy[linear/64] |= uint64(1) << uint(linear%64)
		if delta {
			brick.Values = append(brick.Values, 1)
			brick.Materials = append(brick.Materials, v.Value)
		} else {
			brick.Values = append(brick.Values, v.Value)
		}
	}
	bricks := make([]voxelcodec.Brick, 0, len(byCoordinate))
	for _, brick := range byCoordinate {
		bricks = append(bricks, *brick)
	}
	return bricks, nil
}

func voxelObjectPayloadDocument(payload *VoxelObjectPayloadDef) (voxelcodec.Document, error) {
	if payload != nil && payload.SchemaVersion == HybridVoxelObjectPayloadSchemaVersion {
		copy := *payload
		copy.ReplacementBricks = append([][3]int32(nil), payload.ReplacementBricks...)
		sort.Slice(copy.ReplacementBricks, func(i, j int) bool {
			return voxelObjectSelectorLess(copy.ReplacementBricks[i], copy.ReplacementBricks[j])
		})
		payload = &copy
	}
	metadata, err := voxelObjectMetadata(payload)
	if err != nil {
		return voxelcodec.Document{}, err
	}
	var bricks []voxelcodec.Brick
	if metadata.Mode == VoxelObjectPayloadHybridDelta {
		bricks, err = voxelObjectHybridBricks(payload.Voxels, metadata.ReplacementBricks)
	} else {
		bricks, err = voxelObjectBricks(payload.Voxels, metadata.Mode == VoxelObjectPayloadBaseDelta)
	}
	if err != nil {
		return voxelcodec.Document{}, err
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return voxelcodec.Document{}, err
	}
	return voxelcodec.Document{Kind: "voxel_object_override", Metadata: data, Bricks: bricks}, nil
}

func voxelObjectCanonicalGeometry(records []VoxelObjectVoxelDef) []VoxelObjectVoxelDef {
	cells := make(map[[3]int]uint8)
	for _, v := range records {
		key := [3]int{v.X, v.Y, v.Z}
		if v.Value == 0 {
			delete(cells, key)
		} else {
			cells[key] = v.Value
		}
	}
	result := make([]VoxelObjectVoxelDef, 0, len(cells))
	for key, value := range cells {
		result = append(result, VoxelObjectVoxelDef{X: key[0], Y: key[1], Z: key[2], Value: value})
	}
	sort.Slice(result, func(i, j int) bool { return voxelObjectVoxelLess(result[i], result[j]) })
	return result
}

func voxelObjectBaseDocument(base *VoxelObjectSnapshotDef, lattice VoxelObjectLatticeDef) (voxelcodec.Document, error) {
	if base == nil || (base.SchemaVersion != 0 && base.SchemaVersion != CurrentVoxelObjectSnapshotSchemaVersion) {
		return voxelcodec.Document{}, fmt.Errorf("invalid voxel-object base schema")
	}
	if err := validateVoxelObjectLattice(lattice); err != nil {
		return voxelcodec.Document{}, err
	}
	bricks, err := voxelObjectBricks(voxelObjectCanonicalGeometry(base.Voxels), false)
	if err != nil {
		return voxelcodec.Document{}, err
	}
	metadata, err := json.Marshal(voxelObjectBaseMetadata{Lattice: lattice})
	if err != nil {
		return voxelcodec.Document{}, err
	}
	return voxelcodec.Document{Kind: "voxel_object_base", Metadata: metadata, Bricks: bricks}, nil
}

// VoxelObjectBaseIdentity hashes canonical actual geometry and lattice metadata.
// Legacy duplicate/zero history resolves before hashing; the codec is borrowed.
func VoxelObjectBaseIdentity(base *VoxelObjectSnapshotDef, lattice VoxelObjectLatticeDef, codec *voxelcodec.Codec) (string, int64, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return "", 0, err
	}
	doc, err := voxelObjectBaseDocument(base, lattice)
	if err != nil {
		return "", 0, err
	}
	return codec.Identity(doc)
}

// EncodeVoxelObjectPayload writes schema 2 or 3 C1 frames without mutating input.
// Explicit codecs remain caller-owned; nil uses one reusable default profile.
func EncodeVoxelObjectPayload(payload *VoxelObjectPayloadDef, codec *voxelcodec.Codec) ([]byte, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, err := voxelObjectPayloadDocument(payload)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	if err := validateVoxelObjectSelectorProfile(payload, doc, codec.Limits()); err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return codec.Encode(doc)
}

func voxelObjectPayloadFromDocument(doc voxelcodec.Document, codec *voxelcodec.Codec) (*VoxelObjectPayloadDef, error) {
	if doc.Kind != "voxel_object_override" || doc.NormalBakeVersion != "" {
		return nil, fmt.Errorf("invalid voxel-object override kind/bake version")
	}
	var metadata voxelObjectPayloadMetadata
	if err := json.Unmarshal(doc.Metadata, &metadata); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(metadata)
	if err != nil || !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("noncanonical voxel-object owner metadata")
	}
	payload := &VoxelObjectPayloadDef{SchemaVersion: metadata.SchemaVersion, Mode: metadata.Mode, PlacementID: metadata.PlacementID, ItemID: metadata.ItemID, Lattice: metadata.Lattice, BaseIdentity: metadata.BaseIdentity, ReplacementBricks: append([][3]int32(nil), metadata.ReplacementBricks...)}
	if payload.SchemaVersion != CurrentVoxelObjectPayloadSchemaVersion && payload.SchemaVersion != HybridVoxelObjectPayloadSchemaVersion {
		return nil, fmt.Errorf("unsupported compiled voxel-object schema")
	}
	if _, err := voxelObjectMetadata(payload); err != nil {
		return nil, err
	}
	if err := validateVoxelObjectSelectorProfile(payload, doc, codec.Limits()); err != nil {
		return nil, err
	}
	selected := voxelObjectSelectorSet(payload.ReplacementBricks)
	delta := payload.Mode == VoxelObjectPayloadBaseDelta
	count := 0
	maxInt := int(^uint(0) >> 1)
	for _, brick := range doc.Bricks {
		_, replacement := selected[brick.Coord]
		assignment := delta || payload.Mode == VoxelObjectPayloadHybridDelta && !replacement
		if brick.Aux != nil || assignment && brick.Materials == nil || !assignment && brick.Materials != nil {
			return nil, fmt.Errorf("invalid voxel-object override layers")
		}
		for _, word := range brick.Occupancy {
			n := bits.OnesCount64(word)
			if count > maxInt-n {
				return nil, fmt.Errorf("voxel-object count overflow")
			}
			count += n
		}
	}
	payload.Voxels = make([]VoxelObjectVoxelDef, 0, count)
	for _, brick := range doc.Bricks {
		_, replacement := selected[brick.Coord]
		assignment := delta || payload.Mode == VoxelObjectPayloadHybridDelta && !replacement
		channel := 0
		for linear := 0; linear < 512; linear++ {
			if brick.Occupancy[linear/64]&(uint64(1)<<uint(linear%64)) == 0 {
				continue
			}
			xyz := [3]int64{int64(brick.Coord[0])*8 + int64(linear%8), int64(brick.Coord[1])*8 + int64(linear/8%8), int64(brick.Coord[2])*8 + int64(linear/64)}
			for _, coordinate := range xyz {
				if coordinate < math.MinInt32 || coordinate > math.MaxInt32 {
					return nil, fmt.Errorf("compiled voxel-object coordinate outside signed int32")
				}
			}
			value := brick.Values[channel]
			if assignment {
				if value != 1 {
					return nil, fmt.Errorf("noncanonical delta assignment marker")
				}
				value = brick.Materials[channel]
			}
			payload.Voxels = append(payload.Voxels, VoxelObjectVoxelDef{X: int(xyz[0]), Y: int(xyz[1]), Z: int(xyz[2]), Value: value})
			channel++
		}
	}
	sort.Slice(payload.Voxels, func(i, j int) bool { return voxelObjectVoxelLess(payload.Voxels[i], payload.Voxels[j]) })
	return payload, nil
}

// DecodeVoxelObjectPayload dispatches compiled schema 2/3 or legacy schema 0/1
// JSON. Legacy records retain their original order and unbound acceptance.
func DecodeVoxelObjectPayload(data []byte, codec *voxelcodec.Codec) (*VoxelObjectPayloadDef, voxelcodec.Info, error) {
	if bytes.HasPrefix(data, []byte(importedCompiledMagic)) {
		codec, err := voxelObjectCodec(codec)
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		doc, info, err := codec.Decode(data)
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		payload, err := voxelObjectPayloadFromDocument(doc, codec)
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		return payload, info, nil
	}
	var semantics struct {
		Mode              json.RawMessage `json:"mode"`
		ReplacementBricks json.RawMessage `json:"replacement_bricks"`
	}
	if err := json.Unmarshal(data, &semantics); err != nil {
		return nil, voxelcodec.Info{}, err
	}
	// Legacy snapshots historically ignored unknown fields. Only explicit
	// hybrid semantics select rejection; unrelated mode values remain ignored.
	var mode string
	_ = json.Unmarshal(semantics.Mode, &mode)
	var selectors []json.RawMessage
	if semantics.ReplacementBricks != nil {
		if err := json.Unmarshal(semantics.ReplacementBricks, &selectors); err != nil {
			return nil, voxelcodec.Info{}, err
		}
	}
	if mode == VoxelObjectPayloadHybridDelta || len(selectors) > 0 {
		return nil, voxelcodec.Info{}, fmt.Errorf("hybrid semantics require compiled schema 3")
	}
	var legacy VoxelObjectSnapshotDef
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil, voxelcodec.Info{}, err
	}
	if legacy.SchemaVersion != 0 && legacy.SchemaVersion != CurrentVoxelObjectSnapshotSchemaVersion {
		return nil, voxelcodec.Info{}, fmt.Errorf("unsupported legacy voxel-object snapshot schema")
	}
	return &VoxelObjectPayloadDef{SchemaVersion: CurrentVoxelObjectSnapshotSchemaVersion, Mode: VoxelObjectPayloadFull, Voxels: legacy.Voxels}, voxelcodec.Info{}, nil
}

// ResolveVoxelObjectPayload verifies owner, lattice and independently supplied
// base before returning owned legacy-format full geometry. No partial result is
// returned on error. Both incoming assignments and merged final geometry obey
// the explicit codec's logical limits, without imposing encoded-frame limits.
func ResolveVoxelObjectPayload(payload *VoxelObjectPayloadDef, base *VoxelObjectSnapshotDef, lattice VoxelObjectLatticeDef, placementID, itemID string, codec *voxelcodec.Codec) (*VoxelObjectSnapshotDef, error) {
	if payload == nil {
		return nil, fmt.Errorf("voxel-object payload is nil")
	}
	if payload.SchemaVersion == CurrentVoxelObjectSnapshotSchemaVersion {
		if payload.Mode != VoxelObjectPayloadFull || payload.PlacementID != "" || payload.ItemID != "" || payload.BaseIdentity != "" || payload.Lattice != (VoxelObjectLatticeDef{}) || len(payload.ReplacementBricks) > 0 {
			return nil, fmt.Errorf("legacy voxel-object payload is not unbound full")
		}
		return &VoxelObjectSnapshotDef{SchemaVersion: CurrentVoxelObjectSnapshotSchemaVersion, Voxels: append([]VoxelObjectVoxelDef(nil), payload.Voxels...)}, nil
	}
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, err
	}
	doc, err := voxelObjectPayloadDocument(payload)
	if err != nil {
		return nil, err
	}
	if payload.PlacementID != placementID || payload.ItemID != itemID || payload.Lattice != lattice {
		return nil, fmt.Errorf("voxel-object owner/lattice binding mismatch")
	}
	if err := validateVoxelObjectSelectorProfile(payload, doc, codec.Limits()); err != nil {
		return nil, err
	}
	if _, _, err := codec.Identity(doc); err != nil {
		return nil, err
	}
	result := &VoxelObjectSnapshotDef{SchemaVersion: CurrentVoxelObjectSnapshotSchemaVersion, Voxels: append([]VoxelObjectVoxelDef(nil), payload.Voxels...)}
	if payload.Mode == VoxelObjectPayloadBaseDelta || payload.Mode == VoxelObjectPayloadHybridDelta {
		identity, _, err := VoxelObjectBaseIdentity(base, lattice, codec)
		if err != nil {
			return nil, err
		}
		if identity != payload.BaseIdentity {
			return nil, fmt.Errorf("voxel-object base identity mismatch")
		}
		// Append actual records only; coordinate magnitude never controls allocation.
		if len(base.Voxels) > int(^uint(0)>>1)-len(payload.Voxels) {
			return nil, fmt.Errorf("voxel-object merge count overflow")
		}
		records := make([]VoxelObjectVoxelDef, 0, len(base.Voxels)+len(payload.Voxels))
		if payload.Mode == VoxelObjectPayloadHybridDelta {
			selected := voxelObjectSelectorSet(payload.ReplacementBricks)
			for _, v := range base.Voxels {
				bx, _ := voxelObjectBrickCoordinate(v.X)
				by, _ := voxelObjectBrickCoordinate(v.Y)
				bz, _ := voxelObjectBrickCoordinate(v.Z)
				if _, replaced := selected[[3]int32{bx, by, bz}]; !replaced {
					records = append(records, v)
				}
			}
		} else {
			records = append(records, base.Voxels...)
		}
		records = append(records, payload.Voxels...)
		result.Voxels = voxelObjectCanonicalGeometry(records)
	}
	if _, _, err := VoxelObjectBaseIdentity(result, lattice, codec); err != nil {
		return nil, err
	}
	return result, nil
}
