package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

// CurrentCompiledAssetShapeSchemaVersion identifies canonical compiled part geometry.
const CurrentCompiledAssetShapeSchemaVersion = 1

// CompiledAssetShapeDef contains canonical actual geometry and its authored lattice.
// It does not contain asset hierarchy, materials, source history or renderer data.
type CompiledAssetShapeDef struct {
	SchemaVersion int                   `json:"schema_version"`
	Lattice       VoxelObjectLatticeDef `json:"lattice"`
	Bricks        []voxelcodec.Brick    `json:"bricks"`
}

type compiledAssetShapeMetadata struct {
	SchemaVersion int                   `json:"schema_version"`
	Lattice       VoxelObjectLatticeDef `json:"lattice"`
}

func validateCompiledAssetShape(shape *CompiledAssetShapeDef) error {
	if shape == nil || shape.SchemaVersion != CurrentCompiledAssetShapeSchemaVersion {
		return fmt.Errorf("invalid compiled asset shape schema")
	}
	if err := validateVoxelObjectLattice(shape.Lattice); err != nil {
		return err
	}
	for _, brick := range shape.Bricks {
		if brick.Materials != nil || brick.Aux != nil {
			return fmt.Errorf("compiled asset shape cannot contain secondary or auxiliary layers")
		}
		for _, axis := range brick.Coord {
			if axis < -268435456 || axis > 268435455 {
				return fmt.Errorf("compiled asset shape brick exceeds portable voxel coordinates")
			}
		}
	}
	return nil
}

func compiledAssetShapeDocument(shape *CompiledAssetShapeDef, base bool) (voxelcodec.Document, error) {
	if err := validateCompiledAssetShape(shape); err != nil {
		return voxelcodec.Document{}, err
	}
	kind := "compiled_asset_shape"
	var metadata any = compiledAssetShapeMetadata{SchemaVersion: shape.SchemaVersion, Lattice: shape.Lattice}
	if base {
		kind = "voxel_object_base"
		metadata = voxelObjectBaseMetadata{Lattice: shape.Lattice}
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return voxelcodec.Document{}, err
	}
	return voxelcodec.Document{Kind: kind, Metadata: data, Bricks: shape.Bricks}, nil
}

func compiledAssetShapeFromDocument(doc voxelcodec.Document) (*CompiledAssetShapeDef, error) {
	if doc.Kind != "compiled_asset_shape" || doc.NormalBakeVersion != "" {
		return nil, fmt.Errorf("invalid compiled asset shape kind or bake version")
	}
	var metadata compiledAssetShapeMetadata
	if err := json.Unmarshal(doc.Metadata, &metadata); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("compiled asset shape metadata is not canonical")
	}
	shape := &CompiledAssetShapeDef{SchemaVersion: metadata.SchemaVersion, Lattice: metadata.Lattice, Bricks: doc.Bricks}
	if err := validateCompiledAssetShape(shape); err != nil {
		return nil, err
	}
	return shape, nil
}

// EncodeCompiledAssetShape writes a canonical C1 shape frame without mutating input.
// Explicit codecs are borrowed; nil selects the reusable default profile.
func EncodeCompiledAssetShape(shape *CompiledAssetShapeDef, codec *voxelcodec.Codec) ([]byte, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, err := compiledAssetShapeDocument(shape, false)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return codec.Encode(doc)
}

// DecodeCompiledAssetShape returns independently owned canonical bricks.
// Both generic C1 validation and typed shape requirements apply.
func DecodeCompiledAssetShape(data []byte, codec *voxelcodec.Codec) (*CompiledAssetShapeDef, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, info, err := codec.Decode(data)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	shape, err := compiledAssetShapeFromDocument(doc)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return shape, info, nil
}

// CompiledAssetShapeBaseIdentity hashes the existing voxel_object_base projection.
// Logical codec limits apply to the base projection; encoded-size limits do not.
func CompiledAssetShapeBaseIdentity(shape *CompiledAssetShapeDef, codec *voxelcodec.Codec) (string, int64, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return "", 0, err
	}
	doc, err := compiledAssetShapeDocument(shape, true)
	if err != nil {
		return "", 0, err
	}
	return codec.Identity(doc)
}

// SaveCompiledAssetShape durably publishes a validated shape using atomic replacement.
func SaveCompiledAssetShape(path string, shape *CompiledAssetShapeDef, codec *voxelcodec.Codec) (voxelcodec.Info, error) {
	data, info, err := EncodeCompiledAssetShape(shape, codec)
	if err != nil {
		return voxelcodec.Info{}, err
	}
	if err := saveFileAtomically(path, data, 0644); err != nil {
		return voxelcodec.Info{}, err
	}
	return info, nil
}

// LoadCompiledAssetShape reads a bounded C1 frame without an authoring JSON fallback.
// The codec is borrowed and remains owned by the caller.
func LoadCompiledAssetShape(path string, codec *voxelcodec.Codec) (*CompiledAssetShapeDef, voxelcodec.Info, error) {
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
	shape, err := compiledAssetShapeFromDocument(doc)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return shape, info, nil
}
