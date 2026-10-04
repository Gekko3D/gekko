package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

const (
	CurrentCompiledAssetModelHeaderSchemaVersion   = 1
	CurrentCompiledAssetModelHeaderCompilerVersion = "gekko-compiled-model-asset-v1"
)

// CompiledAssetModelHeaderDef binds separate model frames and static palettes.
// Legacy header types remain unchanged. Authoring source paths are provenance;
// runtime consumers must use only the explicit compiled references.
type CompiledAssetModelHeaderDef struct {
	SchemaVersion   int                            `json:"schema_version"`
	CompilerVersion string                         `json:"compiler_version"`
	Asset           *AssetDef                      `json:"asset"`
	Shapes          []CompiledAssetShapeRefDef     `json:"shapes,omitempty"`
	LODs            []CompiledAssetLODRefDef       `json:"lods,omitempty"`
	Models          []CompiledAssetModelRefDef     `json:"models,omitempty"`
	Palettes        []CompiledAssetModelPaletteDef `json:"palettes,omitempty"`
}

type CompiledAssetModelRefDef struct {
	PartID       string `json:"part_id"`
	Path         string `json:"path"`
	ContentID    string `json:"content_id"`
	BaseIdentity string `json:"base_identity"`
	EncodedBytes int64  `json:"encoded_bytes"`
	DecodedBytes int64  `json:"decoded_bytes"`
	PaletteID    string `json:"palette_id"`
}

// CompiledAssetModelPaletteDef retains static source palette facts. Container
// fields deliberately retain nil/empty distinctions. No process-local paths,
// registered IDs, palette animations or frame overrides enter this format.
type CompiledAssetModelPaletteDef struct {
	ID               string                                         `json:"id"`
	Colors           [256][4]uint8                                  `json:"colors"`
	Materials        []CompiledAssetModelMaterialDef                `json:"materials"`
	SurfaceMaterials map[uint8]CompiledAssetModelSurfaceMaterialDef `json:"surface_materials"`
	IsPBR            bool                                           `json:"is_pbr"`
	Roughness        float32                                        `json:"roughness"`
	Metalness        float32                                        `json:"metalness"`
	Emission         float32                                        `json:"emission"`
	IOR              float32                                        `json:"ior"`
	Transparency     float32                                        `json:"transparency"`
}

type CompiledAssetModelMaterialDef struct {
	ID       int            `json:"id"`
	Type     int            `json:"type"`
	Weight   float32        `json:"weight"`
	Property map[string]any `json:"property"`
}

type CompiledAssetModelSurfaceMaterialDef struct {
	Kind string   `json:"kind"`
	Tags []string `json:"tags"`
}

func compiledModelFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validateCompiledModelPalette(p *CompiledAssetModelPaletteDef) error {
	if p == nil || len(p.Materials) > MaxCompiledAssetParts {
		return fmt.Errorf("invalid compiled model palette or material count")
	}
	for _, value := range []float32{p.Roughness, p.Metalness, p.Emission, p.IOR, p.Transparency} {
		if !compiledModelFinite(float64(value)) {
			return fmt.Errorf("nonfinite compiled model palette scalar")
		}
	}
	for _, material := range p.Materials {
		if !compiledModelFinite(float64(material.Weight)) {
			return fmt.Errorf("nonfinite compiled model material weight")
		}
		for key, property := range material.Property {
			if !utf8.ValidString(key) {
				return fmt.Errorf("invalid compiled model material property key")
			}
			valid := false
			switch value := property.(type) {
			case nil, bool:
				valid = true
			case string:
				valid = utf8.ValidString(value)
			case float32:
				valid = compiledModelFinite(float64(value))
			case float64:
				valid = compiledModelFinite(value)
			}
			if !valid {
				return fmt.Errorf("invalid compiled model material property %q", key)
			}
		}
	}
	for index, surface := range p.SurfaceMaterials {
		if index == 0 || !utf8.ValidString(surface.Kind) || strings.ToLower(strings.TrimSpace(surface.Kind)) != surface.Kind || surface.Kind == "" && len(surface.Tags) == 0 {
			return fmt.Errorf("invalid compiled model surface facts")
		}
		seen := make(map[string]struct{}, len(surface.Tags))
		for _, tag := range surface.Tags {
			if tag == "" || !utf8.ValidString(tag) || strings.ToLower(strings.TrimSpace(tag)) != tag {
				return fmt.Errorf("unnormalized compiled model surface tag")
			}
			if _, exists := seen[tag]; exists {
				return fmt.Errorf("duplicate compiled model surface tag")
			}
			seen[tag] = struct{}{}
		}
	}
	return nil
}

// CompiledAssetModelPaletteIdentity hashes the static payload in its own domain.
// ID is excluded and input containers are borrowed read-only. Only finite float,
// string, bool and nil material properties are supported, matching VOX parsing.
func CompiledAssetModelPaletteIdentity(p *CompiledAssetModelPaletteDef) (string, error) {
	if err := validateCompiledModelPalette(p); err != nil {
		return "", err
	}
	payload, err := json.Marshal(struct {
		Colors           [256][4]uint8                                  `json:"colors"`
		Materials        []CompiledAssetModelMaterialDef                `json:"materials"`
		SurfaceMaterials map[uint8]CompiledAssetModelSurfaceMaterialDef `json:"surface_materials"`
		IsPBR            bool                                           `json:"is_pbr"`
		Roughness        float32                                        `json:"roughness"`
		Metalness        float32                                        `json:"metalness"`
		Emission         float32                                        `json:"emission"`
		IOR              float32                                        `json:"ior"`
		Transparency     float32                                        `json:"transparency"`
	}{p.Colors, p.Materials, p.SurfaceMaterials, p.IsPBR, p.Roughness, p.Metalness, p.Emission, p.IOR, p.Transparency})
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	hash.Write([]byte("compiled_asset_model_palette-v1\n"))
	hash.Write(payload)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func validateCompiledAssetModelHeader(h *CompiledAssetModelHeaderDef, limits voxelcodec.Limits) error {
	if h == nil || h.Asset == nil || h.SchemaVersion != CurrentCompiledAssetModelHeaderSchemaVersion || h.CompilerVersion != CurrentCompiledAssetModelHeaderCompilerVersion || h.Asset.SchemaVersion != 4 {
		return fmt.Errorf("invalid compiled model header schema or compiler")
	}
	if len(h.Asset.Parts) > MaxCompiledAssetParts || len(h.Models) > MaxCompiledAssetParts || len(h.Palettes) > MaxCompiledAssetParts {
		return fmt.Errorf("compiled model header exceeds table limits")
	}
	if validation := validateAsset(h.Asset, AssetValidationOptions{}, false); validation.HasErrors() {
		return fmt.Errorf("invalid compiled model asset metadata: %s", validation.Error())
	}
	// Validate original source descriptors before replacing only copied part kinds
	// in the legacy structural view. That view owns its Parts slice.
	viewAsset := *h.Asset
	viewAsset.Parts = append([]AssetPartDef(nil), h.Asset.Parts...)
	modelParts := make(map[string]struct{}, len(h.Models))
	for i, part := range h.Asset.Parts {
		switch part.Source.Kind {
		case AssetSourceKindGroup, AssetSourceKindVoxelShape:
			continue
		case AssetSourceKindProceduralPrimitive, AssetSourceKindVoxModel, AssetSourceKindVoxSceneNode:
			if !compiledAssetPositiveFinite(part.ModelScale) || !compiledAssetPositiveFinite(part.VoxelResolution) {
				return fmt.Errorf("compiled model part requires finite positive scale and resolution")
			}
			if part.Source.Kind == AssetSourceKindProceduralPrimitive {
				spec, exists := ProceduralPrimitiveSpecFor(part.Source.Primitive)
				if !exists {
					return fmt.Errorf("unsupported compiled model primitive")
				}
				for _, key := range spec.Params {
					if !compiledAssetPositiveFinite(part.Source.Params[key]) || !compiledAssetPositiveFinite(part.Source.Params[key]*part.ModelScale) {
						return fmt.Errorf("invalid compiled model primitive parameter")
					}
				}
			}
			modelParts[part.ID] = struct{}{}
			viewAsset.Parts[i].Source.Kind = AssetSourceKindGroup
			viewAsset.Parts[i].Source.VoxelShape = nil
		default:
			return fmt.Errorf("unsupported compiled model header source")
		}
	}
	view := &CompiledAssetHeaderDef{SchemaVersion: CompiledAssetLODHeaderSchemaVersion, CompilerVersion: CompiledAssetLODHeaderCompilerVersion, Asset: &viewAsset, Shapes: h.Shapes, LODs: h.LODs}
	if err := validateCompiledAssetHeader(view, limits); err != nil {
		return err
	}
	palettes := make(map[string]struct{}, len(h.Palettes))
	for i := range h.Palettes {
		p := &h.Palettes[i]
		id, err := CompiledAssetModelPaletteIdentity(p)
		if err != nil {
			return err
		}
		if p.ID != id {
			return fmt.Errorf("compiled model palette identity mismatch")
		}
		if _, exists := palettes[id]; exists {
			return fmt.Errorf("duplicate compiled model palette")
		}
		palettes[id] = struct{}{}
	}
	occupiedPaths := make(map[string]struct{}, len(h.Shapes)+len(h.LODs))
	for _, ref := range h.Shapes {
		occupiedPaths[ref.Path] = struct{}{}
	}
	for _, ref := range h.LODs {
		occupiedPaths[ref.Path] = struct{}{}
	}
	seen := make(map[string]struct{}, len(h.Models))
	paths := make(map[string]CompiledAssetModelRefDef, len(h.Models))
	identities := make(map[string]CompiledAssetModelRefDef, len(h.Models))
	usedPalettes := make(map[string]struct{}, len(h.Palettes))
	for _, ref := range h.Models {
		if _, exists := modelParts[ref.PartID]; !exists {
			return fmt.Errorf("compiled model reference does not identify a model part")
		}
		if _, exists := seen[ref.PartID]; exists {
			return fmt.Errorf("duplicate compiled model part reference")
		}
		seen[ref.PartID] = struct{}{}
		if !compiledAssetReferencePath(ref.Path) || !compiledAssetHash(ref.ContentID) || !compiledAssetHash(ref.BaseIdentity) || ref.EncodedBytes < 96 || ref.EncodedBytes > limits.MaxEncodedBytes || ref.DecodedBytes <= 0 || ref.DecodedBytes > limits.MaxDecodedBytes {
			return fmt.Errorf("invalid compiled model reference path, identity or size")
		}
		if _, exists := occupiedPaths[ref.Path]; exists {
			return fmt.Errorf("compiled model path overlaps shape or LOD")
		}
		if previous, exists := paths[ref.Path]; exists && (previous.ContentID != ref.ContentID || previous.BaseIdentity != ref.BaseIdentity || previous.EncodedBytes != ref.EncodedBytes || previous.DecodedBytes != ref.DecodedBytes) {
			return fmt.Errorf("inconsistent shared compiled model path")
		}
		if previous, exists := identities[ref.ContentID]; exists && (previous.BaseIdentity != ref.BaseIdentity || previous.DecodedBytes != ref.DecodedBytes) {
			return fmt.Errorf("inconsistent compiled model logical identity")
		}
		paths[ref.Path] = ref
		identities[ref.ContentID] = ref
		if _, exists := palettes[ref.PaletteID]; !exists {
			return fmt.Errorf("compiled model palette is missing")
		}
		usedPalettes[ref.PaletteID] = struct{}{}
	}
	if len(seen) != len(modelParts) {
		return fmt.Errorf("compiled model part reference is missing")
	}
	if len(usedPalettes) != len(palettes) {
		return fmt.Errorf("unused compiled model palette")
	}
	return nil
}

func compiledAssetModelHeaderMetadata(h *CompiledAssetModelHeaderDef, limits voxelcodec.Limits) ([]byte, error) {
	if err := validateCompiledAssetModelHeader(h, limits); err != nil {
		return nil, err
	}
	canonical := *h
	canonical.Shapes = append([]CompiledAssetShapeRefDef(nil), h.Shapes...)
	canonical.LODs = append([]CompiledAssetLODRefDef(nil), h.LODs...)
	canonical.Models = append([]CompiledAssetModelRefDef(nil), h.Models...)
	canonical.Palettes = append([]CompiledAssetModelPaletteDef(nil), h.Palettes...)
	sort.Slice(canonical.Shapes, func(i, j int) bool { return canonical.Shapes[i].PartID < canonical.Shapes[j].PartID })
	sort.Slice(canonical.LODs, func(i, j int) bool { return canonical.LODs[i].PartID < canonical.LODs[j].PartID })
	sort.Slice(canonical.Models, func(i, j int) bool { return canonical.Models[i].PartID < canonical.Models[j].PartID })
	sort.Slice(canonical.Palettes, func(i, j int) bool { return canonical.Palettes[i].ID < canonical.Palettes[j].ID })
	return json.Marshal(&canonical)
}

func compiledAssetModelHeaderFromDocument(doc voxelcodec.Document, limits voxelcodec.Limits) (*CompiledAssetModelHeaderDef, error) {
	if doc.Kind != "compiled_asset_model_header" || doc.NormalBakeVersion != "" || len(doc.Bricks) != 0 {
		return nil, fmt.Errorf("invalid compiled model header kind, bake or geometry")
	}
	var h CompiledAssetModelHeaderDef
	if err := json.Unmarshal(doc.Metadata, &h); err != nil {
		return nil, err
	}
	canonical, err := compiledAssetModelHeaderMetadata(&h, limits)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("compiled model header metadata is not canonical")
	}
	return &h, nil
}

// EncodeCompiledAssetModelHeader validates structure and writes a canonical C1
// header without changing input or following frame/authoring references.
func EncodeCompiledAssetModelHeader(h *CompiledAssetModelHeaderDef, codec *voxelcodec.Codec) ([]byte, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	metadata, err := compiledAssetModelHeaderMetadata(h, codec.Limits())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return codec.Encode(voxelcodec.Document{Kind: "compiled_asset_model_header", Metadata: metadata})
}

// DecodeCompiledAssetModelHeader returns independently owned metadata. Numeric
// material properties decode to float64; renderer float accessors accept both
// float32 and float64 without changing their float32 material values.
func DecodeCompiledAssetModelHeader(data []byte, codec *voxelcodec.Codec) (*CompiledAssetModelHeaderDef, voxelcodec.Info, error) {
	codec, err := voxelObjectCodec(codec)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	doc, info, err := codec.Decode(data)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	h, err := compiledAssetModelHeaderFromDocument(doc, codec.Limits())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return h, info, nil
}

// SaveCompiledAssetModelHeader publishes a validated header by synced atomic replacement.
func SaveCompiledAssetModelHeader(path string, h *CompiledAssetModelHeaderDef, codec *voxelcodec.Codec) (voxelcodec.Info, error) {
	data, info, err := EncodeCompiledAssetModelHeader(h, codec)
	if err != nil {
		return voxelcodec.Info{}, err
	}
	if err := saveFileAtomically(path, data, 0644); err != nil {
		return voxelcodec.Info{}, err
	}
	return info, nil
}

// LoadCompiledAssetModelHeader reads a bounded C1 header without authoring fallback.
// Explicit codecs are borrowed; nil selects the shared default profile.
func LoadCompiledAssetModelHeader(path string, codec *voxelcodec.Codec) (*CompiledAssetModelHeaderDef, voxelcodec.Info, error) {
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
	h, err := compiledAssetModelHeaderFromDocument(doc, codec.Limits())
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return h, info, nil
}
