package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

// CurrentCompiledAssetModelSchemaVersion identifies canonical compiled model frames.
const CurrentCompiledAssetModelSchemaVersion = 1

// CompiledAssetModelDef contains canonical post-scale primary geometry, declared
// dimensions and lattice. Dimensions are not occupied bounds. It contains no raw
// sample history, hierarchy, palettes or renderer data.
type CompiledAssetModelDef struct {
	SchemaVersion int                   `json:"schema_version"`
	Lattice       VoxelObjectLatticeDef `json:"lattice"`
	Dimensions    [3]uint32             `json:"dimensions"`
	Bricks        []voxelcodec.Brick    `json:"bricks"`
}

type compiledAssetModelMetadata struct {
	SchemaVersion int                   `json:"schema_version"`
	Lattice       VoxelObjectLatticeDef `json:"lattice"`
	Dimensions    [3]uint32             `json:"dimensions"`
}

func validateCompiledAssetModel(model *CompiledAssetModelDef) error {
	if model == nil || model.SchemaVersion != CurrentCompiledAssetModelSchemaVersion {
		return fmt.Errorf("invalid compiled asset model schema")
	}
	return validateCompiledAssetShape(&CompiledAssetShapeDef{
		SchemaVersion: CurrentCompiledAssetShapeSchemaVersion,
		Lattice:       model.Lattice,
		Bricks:        model.Bricks,
	})
}

func compiledAssetModelDocument(model *CompiledAssetModelDef, base bool) (voxelcodec.Document, error) {
	if err := validateCompiledAssetModel(model); err != nil {
		return voxelcodec.Document{}, err
	}
	kind := "compiled_asset_model"
	var metadata any = compiledAssetModelMetadata{SchemaVersion: model.SchemaVersion, Lattice: model.Lattice, Dimensions: model.Dimensions}
	if base {
		kind = "voxel_object_base"
		metadata = voxelObjectBaseMetadata{Lattice: model.Lattice}
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return voxelcodec.Document{}, err
	}
	return voxelcodec.Document{Kind: kind, Metadata: data, Bricks: model.Bricks}, nil
}

func compiledAssetModelFromDocument(doc voxelcodec.Document) (*CompiledAssetModelDef, error) {
	if doc.Kind != "compiled_asset_model" || doc.NormalBakeVersion != "" {
		return nil, fmt.Errorf("invalid compiled asset model kind or bake version")
	}
	var metadata compiledAssetModelMetadata
	if err := json.Unmarshal(doc.Metadata, &metadata); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("compiled asset model metadata is not canonical")
	}
	model := &CompiledAssetModelDef{SchemaVersion: metadata.SchemaVersion, Lattice: metadata.Lattice, Dimensions: metadata.Dimensions, Bricks: doc.Bricks}
	if err := validateCompiledAssetModel(model); err != nil {
		return nil, err
	}
	return model, nil
}

// EncodeCompiledAssetModel writes a canonical C1 model frame without mutating input.
// Explicit codecs are borrowed; nil selects the reusable default profile.
func EncodeCompiledAssetModel(model *CompiledAssetModelDef, codec *voxelcodec.Codec) ([]byte, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, err := compiledAssetModelDocument(model, false)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return codec.Encode(doc)
}

// DecodeCompiledAssetModel returns independently owned canonical bricks.
// Both generic C1 validation and typed model requirements apply.
func DecodeCompiledAssetModel(data []byte, codec *voxelcodec.Codec) (*CompiledAssetModelDef, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, info, err := codec.Decode(data)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	model, err := compiledAssetModelFromDocument(doc)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return model, info, nil
}

// CompiledAssetModelBaseIdentity hashes the existing voxel_object_base projection.
// Declared dimensions affect model identity but do not enter this primary projection.
// Logical codec limits apply to the base projection; encoded-size limits do not.
func CompiledAssetModelBaseIdentity(model *CompiledAssetModelDef, codec *voxelcodec.Codec) (string, int64, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return "", 0, err
	}
	doc, err := compiledAssetModelDocument(model, true)
	if err != nil {
		return "", 0, err
	}
	return codec.Identity(doc)
}

// SaveCompiledAssetModel durably publishes a validated model using atomic replacement.
func SaveCompiledAssetModel(path string, model *CompiledAssetModelDef, codec *voxelcodec.Codec) (voxelcodec.Info, error) {
	data, info, err := EncodeCompiledAssetModel(model, codec)
	if err != nil {
		return voxelcodec.Info{}, err
	}
	if err := saveFileAtomically(path, data, 0644); err != nil {
		return voxelcodec.Info{}, err
	}
	return info, nil
}

// LoadCompiledAssetModel reads a bounded C1 frame without an authoring JSON fallback.
// The codec is borrowed and remains owned by the caller.
func LoadCompiledAssetModel(path string, codec *voxelcodec.Codec) (*CompiledAssetModelDef, voxelcodec.Info, error) {
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
	model, err := compiledAssetModelFromDocument(doc)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return model, info, nil
}
