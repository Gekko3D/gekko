package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/bits"
	"os"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

// CurrentCompiledAssetLODSchemaVersion identifies source-bound compiled derivatives.
const CurrentCompiledAssetLODSchemaVersion = 1

// CompiledAssetLOD2xReductionVersion identifies zero-anchored conservative coverage.
const CompiledAssetLOD2xReductionVersion = "occupancy-or-zero-anchored-2x-v1"

// CompiledAssetLODDef describes a derivative, never authoritative geometry.
// SourceContentID is the ordinary compiled shape's C1 logical identity.
// Bounds have inclusive minima and exclusive maxima in their respective grids.
type CompiledAssetLODDef struct {
	SchemaVersion    int                   `json:"schema_version"`
	SourceContentID  string                `json:"source_content_id"`
	SourceLattice    VoxelObjectLatticeDef `json:"source_lattice"`
	Factor           int                   `json:"factor"`
	ReductionVersion string                `json:"reduction_version"`
	Value            uint8                 `json:"value"`
	SourceVoxelCount int64                 `json:"source_voxel_count"`
	SourceMin        [3]int64              `json:"source_min"`
	SourceMax        [3]int64              `json:"source_max"`
	CoarseMin        [3]int64              `json:"coarse_min"`
	CoarseMax        [3]int64              `json:"coarse_max"`
	Bricks           []voxelcodec.Brick    `json:"bricks"`
}

type compiledAssetLODMetadata struct {
	SchemaVersion    int                   `json:"schema_version"`
	SourceContentID  string                `json:"source_content_id"`
	SourceLattice    VoxelObjectLatticeDef `json:"source_lattice"`
	Factor           int                   `json:"factor"`
	ReductionVersion string                `json:"reduction_version"`
	Value            uint8                 `json:"value"`
	SourceVoxelCount int64                 `json:"source_voxel_count"`
	SourceMin        [3]int64              `json:"source_min"`
	SourceMax        [3]int64              `json:"source_max"`
	CoarseMin        [3]int64              `json:"coarse_min"`
	CoarseMax        [3]int64              `json:"coarse_max"`
}

func compiledAssetLODFloor2(value int64) int64 {
	result := value / 2
	if value < 0 && value%2 != 0 {
		result--
	}
	return result
}

// This validates internal consistency, not coverage of an unavailable source
// or palette opacity. Those require independent source/material verification.
func validateCompiledAssetLOD(lod *CompiledAssetLODDef) error {
	if lod == nil || lod.SchemaVersion != CurrentCompiledAssetLODSchemaVersion || !compiledAssetHash(lod.SourceContentID) || lod.Factor != 2 || lod.ReductionVersion != CompiledAssetLOD2xReductionVersion || lod.Value == 0 {
		return fmt.Errorf("invalid compiled asset LOD metadata")
	}
	if err := validateVoxelObjectLattice(lod.SourceLattice); err != nil {
		return err
	}
	for axis := 0; axis < 3; axis++ {
		if lod.SourceMin[axis] < -2147483648 || lod.SourceMax[axis] > 2147483648 || lod.SourceMin[axis] >= lod.SourceMax[axis] {
			return fmt.Errorf("invalid compiled asset LOD source bounds")
		}
		if lod.CoarseMin[axis] != compiledAssetLODFloor2(lod.SourceMin[axis]) || lod.CoarseMax[axis] != compiledAssetLODFloor2(lod.SourceMax[axis]-1)+1 {
			return fmt.Errorf("compiled asset LOD grid bounds mismatch")
		}
	}
	minimum := [3]int64{math.MaxInt64, math.MaxInt64, math.MaxInt64}
	maximum := [3]int64{math.MinInt64, math.MinInt64, math.MinInt64}
	var count, capacity int64
	for _, brick := range lod.Bricks {
		if brick.Materials != nil || brick.Aux != nil {
			return fmt.Errorf("compiled asset LOD cannot contain secondary or auxiliary layers")
		}
		for _, axis := range brick.Coord {
			if axis < -134217728 || axis > 134217727 {
				return fmt.Errorf("compiled asset LOD brick exceeds portable coordinates")
			}
		}
		occupied := 0
		for _, word := range brick.Occupancy {
			occupied += bits.OnesCount64(word)
		}
		if occupied == 0 || occupied != len(brick.Values) {
			return fmt.Errorf("invalid compiled asset LOD occupancy/value cardinality")
		}
		for _, value := range brick.Values {
			if value != lod.Value {
				return fmt.Errorf("compiled asset LOD requires one nonzero value")
			}
		}
		for wordIndex, word := range brick.Occupancy {
			for word != 0 {
				local := wordIndex*64 + bits.TrailingZeros64(word)
				word &= word - 1
				q := [3]int64{int64(brick.Coord[0])*8 + int64(local%8), int64(brick.Coord[1])*8 + int64(local/8%8), int64(brick.Coord[2])*8 + int64(local/64)}
				footprint := int64(1)
				for axis := 0; axis < 3; axis++ {
					minimum[axis] = min(minimum[axis], q[axis])
					maximum[axis] = max(maximum[axis], q[axis]+1)
					lo := max(2*q[axis], lod.SourceMin[axis])
					hi := min(2*q[axis]+2, lod.SourceMax[axis])
					if hi <= lo {
						return fmt.Errorf("compiled asset LOD cell lies outside source bounds")
					}
					footprint *= hi - lo // Each clipped dimension is at most two.
				}
				if capacity > math.MaxInt64-footprint {
					capacity = math.MaxInt64
				} else {
					capacity += footprint
				}
				count++
			}
		}
	}
	if count == 0 || minimum != lod.CoarseMin || maximum != lod.CoarseMax || lod.SourceVoxelCount <= count || lod.SourceVoxelCount > capacity {
		return fmt.Errorf("invalid compiled asset LOD bounds or reduction counts")
	}
	return nil
}

func compiledAssetLODDocument(lod *CompiledAssetLODDef) (voxelcodec.Document, error) {
	if err := validateCompiledAssetLOD(lod); err != nil {
		return voxelcodec.Document{}, err
	}
	metadata := compiledAssetLODMetadata{lod.SchemaVersion, lod.SourceContentID, lod.SourceLattice, lod.Factor, lod.ReductionVersion, lod.Value, lod.SourceVoxelCount, lod.SourceMin, lod.SourceMax, lod.CoarseMin, lod.CoarseMax}
	data, err := json.Marshal(metadata)
	if err != nil {
		return voxelcodec.Document{}, err
	}
	return voxelcodec.Document{Kind: "compiled_asset_lod", Metadata: data, Bricks: lod.Bricks}, nil
}

func compiledAssetLODFromDocument(doc voxelcodec.Document) (*CompiledAssetLODDef, error) {
	if doc.Kind != "compiled_asset_lod" || doc.NormalBakeVersion != "" {
		return nil, fmt.Errorf("invalid compiled asset LOD kind or bake version")
	}
	var metadata compiledAssetLODMetadata
	if err := json.Unmarshal(doc.Metadata, &metadata); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("compiled asset LOD metadata is not canonical")
	}
	lod := &CompiledAssetLODDef{metadata.SchemaVersion, metadata.SourceContentID, metadata.SourceLattice, metadata.Factor, metadata.ReductionVersion, metadata.Value, metadata.SourceVoxelCount, metadata.SourceMin, metadata.SourceMax, metadata.CoarseMin, metadata.CoarseMax, doc.Bricks}
	if err := validateCompiledAssetLOD(lod); err != nil {
		return nil, err
	}
	return lod, nil
}

// EncodeCompiledAssetLOD writes a canonical derivative without mutating input.
// Explicit codecs are borrowed; nil selects the reusable default profile.
func EncodeCompiledAssetLOD(lod *CompiledAssetLODDef, codec *voxelcodec.Codec) ([]byte, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, err := compiledAssetLODDocument(lod)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return codec.Encode(doc)
}

// DecodeCompiledAssetLOD validates the typed frame and returns owned geometry.
func DecodeCompiledAssetLOD(data []byte, codec *voxelcodec.Codec) (*CompiledAssetLODDef, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, info, err := codec.Decode(data)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	lod, err := compiledAssetLODFromDocument(doc)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return lod, info, nil
}

// SaveCompiledAssetLOD durably publishes a validated derivative atomically.
func SaveCompiledAssetLOD(path string, lod *CompiledAssetLODDef, codec *voxelcodec.Codec) (voxelcodec.Info, error) {
	data, info, err := EncodeCompiledAssetLOD(lod, codec)
	if err != nil {
		return voxelcodec.Info{}, err
	}
	if err := saveFileAtomically(path, data, 0644); err != nil {
		return voxelcodec.Info{}, err
	}
	return info, nil
}

// LoadCompiledAssetLOD reads a bounded C1 frame, without authoring fallback.
func LoadCompiledAssetLOD(path string, codec *voxelcodec.Codec) (*CompiledAssetLODDef, voxelcodec.Info, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	codec, err = voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, info, err := codec.ReadFrame(file, 0, stat.Size())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	lod, err := compiledAssetLODFromDocument(doc)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return lod, info, nil
}
