package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

const (
	CurrentCompiledAssetHeaderSchemaVersion = 1
	CurrentCompiledAssetCompilerVersion     = "gekko-compiled-asset-v1"
	CompiledAssetLODHeaderSchemaVersion     = 2
	CompiledAssetLODHeaderCompilerVersion   = "gekko-compiled-asset-v2"
	MaxCompiledAssetParts                   = 4096
)

// CompiledAssetHeaderDef retains asset metadata and explicit part geometry references.
// It contains neither inline geometry nor registered runtime resources.
type CompiledAssetHeaderDef struct {
	SchemaVersion   int                        `json:"schema_version"`
	CompilerVersion string                     `json:"compiler_version"`
	Asset           *AssetDef                  `json:"asset"`
	Shapes          []CompiledAssetShapeRefDef `json:"shapes,omitempty"`
	LODs            []CompiledAssetLODRefDef   `json:"lods,omitempty"`
}

// CompiledAssetShapeRefDef binds a part to an independent compiled shape frame.
// Header validation checks this reference without loading the referenced file.
type CompiledAssetShapeRefDef struct {
	PartID       string `json:"part_id"`
	Path         string `json:"path"`
	ContentID    string `json:"content_id"`
	BaseIdentity string `json:"base_identity"`
	EncodedBytes int64  `json:"encoded_bytes"`
	DecodedBytes int64  `json:"decoded_bytes"`
}

// CompiledAssetLODRefDef binds an optional derivative to its authoritative shape.
// Header validation checks structure only; referenced frames remain unresolved.
type CompiledAssetLODRefDef struct {
	PartID           string `json:"part_id"`
	Path             string `json:"path"`
	ContentID        string `json:"content_id"`
	SourceContentID  string `json:"source_content_id"`
	EncodedBytes     int64  `json:"encoded_bytes"`
	DecodedBytes     int64  `json:"decoded_bytes"`
	Factor           int    `json:"factor"`
	ReductionVersion string `json:"reduction_version"`
}

func compiledAssetExplicitID(id string) bool {
	return strings.TrimSpace(id) != "" && utf8.ValidString(id)
}

func compiledAssetReferencePath(value string) bool {
	if value == "" || value == "." || !utf8.ValidString(value) || strings.ContainsAny(value, "\\:\x00") || path.IsAbs(value) || path.Clean(value) != value {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func compiledAssetHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if !(digit >= '0' && digit <= '9' || digit >= 'a' && digit <= 'f') {
			return false
		}
	}
	return true
}

func compiledAssetPositiveFinite(value float32) bool {
	return value > 0 && !math.IsInf(float64(value), 0) && !math.IsNaN(float64(value))
}

func validateCompiledAssetHeader(header *CompiledAssetHeaderDef, limits voxelcodec.Limits) error {
	if header == nil || header.Asset == nil || header.Asset.SchemaVersion != 4 {
		return fmt.Errorf("invalid compiled asset header schema or compiler version")
	}
	switch header.SchemaVersion {
	case CurrentCompiledAssetHeaderSchemaVersion:
		if header.CompilerVersion != CurrentCompiledAssetCompilerVersion || len(header.LODs) != 0 {
			return fmt.Errorf("invalid compiled asset header schema or compiler version")
		}
	case CompiledAssetLODHeaderSchemaVersion:
		if header.CompilerVersion != CompiledAssetLODHeaderCompilerVersion {
			return fmt.Errorf("invalid compiled asset header schema or compiler version")
		}
	default:
		return fmt.Errorf("invalid compiled asset header schema or compiler version")
	}
	asset := header.Asset
	if len(asset.Parts) > MaxCompiledAssetParts || len(header.Shapes) > MaxCompiledAssetParts || len(header.LODs) > MaxCompiledAssetParts {
		return fmt.Errorf("compiled asset exceeds part/reference limits")
	}
	if !compiledAssetExplicitID(asset.ID) || asset.Runtime != nil && asset.Runtime.CollapseVoxelParts {
		return fmt.Errorf("compiled asset requires an explicit ID and separate parts")
	}
	for _, material := range asset.Materials {
		if !compiledAssetExplicitID(material.ID) {
			return fmt.Errorf("compiled asset material requires an explicit ID")
		}
	}
	parts := make(map[string]struct{}, len(asset.Parts))
	for _, part := range asset.Parts {
		if !compiledAssetExplicitID(part.ID) {
			return fmt.Errorf("compiled asset part requires an explicit ID")
		}
		switch part.Source.Kind {
		case AssetSourceKindGroup:
			if part.Source.VoxelShape != nil {
				return fmt.Errorf("compiled asset group cannot carry voxel geometry")
			}
		case AssetSourceKindVoxelShape:
			if part.Source.VoxelShape == nil || len(part.Source.VoxelShape.Voxels) != 0 || !compiledAssetPositiveFinite(part.ModelScale) || !compiledAssetPositiveFinite(part.VoxelResolution) {
				return fmt.Errorf("compiled asset shape requires empty inline geometry and finite positive scale/resolution")
			}
			parts[part.ID] = struct{}{}
		default:
			return fmt.Errorf("unsupported compiled asset source kind %q", part.Source.Kind)
		}
	}
	for _, light := range asset.Lights {
		if !compiledAssetExplicitID(light.ID) {
			return fmt.Errorf("compiled asset light requires an explicit ID")
		}
	}
	for _, emitter := range asset.Emitters {
		if !compiledAssetExplicitID(emitter.ID) {
			return fmt.Errorf("compiled asset emitter requires an explicit ID")
		}
	}
	for _, marker := range asset.Markers {
		if !compiledAssetExplicitID(marker.ID) {
			return fmt.Errorf("compiled asset marker requires an explicit ID")
		}
	}
	if asset.Skeleton != nil {
		for _, bone := range asset.Skeleton.Bones {
			if !compiledAssetExplicitID(bone.ID) || !compiledAssetExplicitID(bone.JointID) {
				return fmt.Errorf("compiled asset bone and joint require explicit IDs")
			}
		}
	}
	if validation := ValidateAsset(asset, AssetValidationOptions{}); validation.HasErrors() {
		return fmt.Errorf("invalid compiled asset metadata: %s", validation.Error())
	}
	seen := make(map[string]struct{}, len(header.Shapes))
	paths := make(map[string]CompiledAssetShapeRefDef, len(header.Shapes))
	var shapeByPart map[string]CompiledAssetShapeRefDef
	if len(header.LODs) > 0 {
		shapeByPart = make(map[string]CompiledAssetShapeRefDef, len(header.Shapes))
	}
	for _, ref := range header.Shapes {
		if _, ok := parts[ref.PartID]; !ok {
			return fmt.Errorf("compiled shape reference does not identify a voxel part")
		}
		if _, ok := seen[ref.PartID]; ok {
			return fmt.Errorf("duplicate compiled shape part reference")
		}
		seen[ref.PartID] = struct{}{}
		if !compiledAssetReferencePath(ref.Path) || !compiledAssetHash(ref.ContentID) || !compiledAssetHash(ref.BaseIdentity) || ref.EncodedBytes < 96 || ref.EncodedBytes > limits.MaxEncodedBytes || ref.DecodedBytes <= 0 || ref.DecodedBytes > limits.MaxDecodedBytes {
			return fmt.Errorf("invalid compiled shape path, identity or frame size")
		}
		if previous, ok := paths[ref.Path]; ok && (previous.ContentID != ref.ContentID || previous.BaseIdentity != ref.BaseIdentity || previous.EncodedBytes != ref.EncodedBytes || previous.DecodedBytes != ref.DecodedBytes) {
			return fmt.Errorf("inconsistent shared compiled shape reference")
		}
		paths[ref.Path] = ref
		if shapeByPart != nil {
			shapeByPart[ref.PartID] = ref
		}
	}
	if len(seen) != len(parts) {
		return fmt.Errorf("compiled asset shape reference is missing")
	}
	lodParts := make(map[string]struct{}, len(header.LODs))
	lodPaths := make(map[string]CompiledAssetLODRefDef, len(header.LODs))
	lodIdentities := make(map[string]CompiledAssetLODRefDef, len(header.LODs))
	for _, ref := range header.LODs {
		shape, ok := shapeByPart[ref.PartID]
		if !ok {
			return fmt.Errorf("compiled LOD reference does not identify a voxel part")
		}
		if _, ok := lodParts[ref.PartID]; ok {
			return fmt.Errorf("duplicate compiled LOD part reference")
		}
		lodParts[ref.PartID] = struct{}{}
		if !compiledAssetReferencePath(ref.Path) || !compiledAssetHash(ref.ContentID) || !compiledAssetHash(ref.SourceContentID) || ref.SourceContentID != shape.ContentID || ref.Factor != 2 || ref.ReductionVersion != CompiledAssetLOD2xReductionVersion || ref.EncodedBytes < 96 || ref.EncodedBytes > limits.MaxEncodedBytes || ref.DecodedBytes <= 0 || ref.DecodedBytes > limits.MaxDecodedBytes {
			return fmt.Errorf("invalid compiled LOD path, source, reduction or frame size")
		}
		if _, ok := paths[ref.Path]; ok {
			return fmt.Errorf("compiled LOD path overlaps authoritative shape")
		}
		if previous, ok := lodPaths[ref.Path]; ok && (previous.ContentID != ref.ContentID || previous.SourceContentID != ref.SourceContentID || previous.Factor != ref.Factor || previous.ReductionVersion != ref.ReductionVersion || previous.EncodedBytes != ref.EncodedBytes || previous.DecodedBytes != ref.DecodedBytes) {
			return fmt.Errorf("inconsistent shared compiled LOD reference")
		}
		if previous, ok := lodIdentities[ref.ContentID]; ok && (previous.SourceContentID != ref.SourceContentID || previous.Factor != ref.Factor || previous.ReductionVersion != ref.ReductionVersion || previous.DecodedBytes != ref.DecodedBytes) {
			return fmt.Errorf("inconsistent compiled LOD logical identity")
		}
		lodPaths[ref.Path] = ref
		lodIdentities[ref.ContentID] = ref
	}
	return nil
}

func compiledAssetHeaderMetadata(header *CompiledAssetHeaderDef, limits voxelcodec.Limits) ([]byte, error) {
	if err := validateCompiledAssetHeader(header, limits); err != nil {
		return nil, err
	}
	canonical := *header
	canonical.Shapes = append([]CompiledAssetShapeRefDef(nil), header.Shapes...)
	sort.Slice(canonical.Shapes, func(i, j int) bool { return canonical.Shapes[i].PartID < canonical.Shapes[j].PartID })
	canonical.LODs = append([]CompiledAssetLODRefDef(nil), header.LODs...)
	sort.Slice(canonical.LODs, func(i, j int) bool { return canonical.LODs[i].PartID < canonical.LODs[j].PartID })
	return json.Marshal(&canonical)
}

func compiledAssetHeaderFromDocument(doc voxelcodec.Document, limits voxelcodec.Limits) (*CompiledAssetHeaderDef, error) {
	if doc.Kind != "compiled_asset_header" || doc.NormalBakeVersion != "" || len(doc.Bricks) != 0 {
		return nil, fmt.Errorf("invalid compiled asset header kind, bake version or geometry")
	}
	var header CompiledAssetHeaderDef
	if err := json.Unmarshal(doc.Metadata, &header); err != nil {
		return nil, err
	}
	canonical, err := compiledAssetHeaderMetadata(&header, limits)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("compiled asset header metadata is not canonical")
	}
	return &header, nil
}

// EncodeCompiledAssetHeader writes a canonical header without changing caller metadata.
// Explicit codecs are borrowed; nil selects the reusable default profile.
func EncodeCompiledAssetHeader(header *CompiledAssetHeaderDef, codec *voxelcodec.Codec) ([]byte, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	metadata, err := compiledAssetHeaderMetadata(header, codec.Limits())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return codec.Encode(voxelcodec.Document{Kind: "compiled_asset_header", Metadata: metadata})
}

// DecodeCompiledAssetHeader returns independently owned asset metadata and references.
// It validates structure without reading shape or external animation/texture files.
func DecodeCompiledAssetHeader(data []byte, codec *voxelcodec.Codec) (*CompiledAssetHeaderDef, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, info, err := codec.Decode(data)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	header, err := compiledAssetHeaderFromDocument(doc, codec.Limits())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return header, info, nil
}

// SaveCompiledAssetHeader durably publishes a validated header with atomic replacement.
func SaveCompiledAssetHeader(path string, header *CompiledAssetHeaderDef, codec *voxelcodec.Codec) (voxelcodec.Info, error) {
	data, info, err := EncodeCompiledAssetHeader(header, codec)
	if err != nil {
		return voxelcodec.Info{}, err
	}
	if err := saveFileAtomically(path, data, 0644); err != nil {
		return voxelcodec.Info{}, err
	}
	return info, nil
}

// LoadCompiledAssetHeader reads a bounded C1 header without an authoring JSON fallback.
// Referenced files remain unresolved, and the caller retains codec ownership.
func LoadCompiledAssetHeader(path string, codec *voxelcodec.Codec) (*CompiledAssetHeaderDef, voxelcodec.Info, error) {
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
	header, err := compiledAssetHeaderFromDocument(doc, codec.Limits())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return header, info, nil
}
