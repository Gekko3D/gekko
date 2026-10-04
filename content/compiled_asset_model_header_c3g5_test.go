package content_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3g5Palette(t *testing.T, color uint8) content.CompiledAssetModelPaletteDef {
	t.Helper()
	p := content.CompiledAssetModelPaletteDef{IsPBR: true, Roughness: .2, Metalness: .3, Emission: .4, IOR: 1.5, Transparency: .6, Materials: []content.CompiledAssetModelMaterialDef{{ID: 3, Type: 2, Weight: .7, Property: map[string]any{"nil": nil, "bool": true, "text": "metal", "f32": float32(.25), "f64": float64(.5)}}}, SurfaceMaterials: map[uint8]content.CompiledAssetModelSurfaceMaterialDef{3: {Kind: "stone", Tags: []string{"kind:stone", "solid"}}}}
	p.Colors[3] = [4]uint8{color, 2, 3, 255}
	id, err := content.CompiledAssetModelPaletteIdentity(&p)
	if err != nil {
		t.Fatal(err)
	}
	p.ID = id
	return p
}

func c3g5Header(t *testing.T) *content.CompiledAssetModelHeaderDef {
	t.Helper()
	old := c3h3Header()
	palettes := []content.CompiledAssetModelPaletteDef{c3g5Palette(t, 11), c3g5Palette(t, 22)}
	for _, id := range []string{"z-model", "a-model"} {
		old.Asset.Parts = append(old.Asset.Parts, content.AssetPartDef{ID: id, Name: id, ParentID: "group", Transform: old.Asset.Parts[0].Transform, ModelScale: 1.5, VoxelResolution: .125, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube", Params: map[string]float32{"sx": 2, "sy": 3, "sz": 4}, MaterialID: "material"}})
	}
	return &content.CompiledAssetModelHeaderDef{SchemaVersion: content.CurrentCompiledAssetModelHeaderSchemaVersion, CompilerVersion: content.CurrentCompiledAssetModelHeaderCompilerVersion, Asset: old.Asset, Shapes: old.Shapes, LODs: old.LODs, Palettes: palettes, Models: []content.CompiledAssetModelRefDef{{PartID: "z-model", Path: "models/z.gkvox", ContentID: strings.Repeat("d", 64), BaseIdentity: strings.Repeat("e", 64), EncodedBytes: 200, DecodedBytes: 400, PaletteID: palettes[0].ID}, {PartID: "a-model", Path: "models/a.gkvox", ContentID: strings.Repeat("d", 64), BaseIdentity: strings.Repeat("e", 64), EncodedBytes: 210, DecodedBytes: 400, PaletteID: palettes[1].ID}}}
}

func c3g5Canonical(h *content.CompiledAssetModelHeaderDef) {
	sort.Slice(h.Shapes, func(i, j int) bool { return h.Shapes[i].PartID < h.Shapes[j].PartID })
	sort.Slice(h.LODs, func(i, j int) bool { return h.LODs[i].PartID < h.LODs[j].PartID })
	sort.Slice(h.Models, func(i, j int) bool { return h.Models[i].PartID < h.Models[j].PartID })
	sort.Slice(h.Palettes, func(i, j int) bool { return h.Palettes[i].ID < h.Palettes[j].ID })
}

func TestC3g5PaletteIdentityAndScalarContract(t *testing.T) {
	p := c3g5Palette(t, 11)
	before, _ := json.Marshal(p)
	payload, err := json.Marshal(struct {
		Colors           [256][4]uint8                                          `json:"colors"`
		Materials        []content.CompiledAssetModelMaterialDef                `json:"materials"`
		SurfaceMaterials map[uint8]content.CompiledAssetModelSurfaceMaterialDef `json:"surface_materials"`
		IsPBR            bool                                                   `json:"is_pbr"`
		Roughness        float32                                                `json:"roughness"`
		Metalness        float32                                                `json:"metalness"`
		Emission         float32                                                `json:"emission"`
		IOR              float32                                                `json:"ior"`
		Transparency     float32                                                `json:"transparency"`
	}{p.Colors, p.Materials, p.SurfaceMaterials, p.IsPBR, p.Roughness, p.Metalness, p.Emission, p.IOR, p.Transparency})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append([]byte("compiled_asset_model_palette-v1\n"), payload...))
	want := hex.EncodeToString(sum[:])
	p.ID = "ignored-id"
	id, err := content.CompiledAssetModelPaletteIdentity(&p)
	if err != nil || id != want {
		t.Fatalf("identity projection %q want %q: %v", id, want, err)
	}
	p.ID = want
	after, _ := json.Marshal(p)
	if !bytes.Equal(before, after) {
		t.Fatal("identity mutated palette")
	}
	ids := map[string]bool{}
	for _, container := range []int{0, 1, 2} {
		q := content.CompiledAssetModelPaletteDef{}
		if container == 1 {
			q.Materials = []content.CompiledAssetModelMaterialDef{}
		}
		if container == 2 {
			q.SurfaceMaterials = map[uint8]content.CompiledAssetModelSurfaceMaterialDef{}
		}
		id, err := content.CompiledAssetModelPaletteIdentity(&q)
		if err != nil || ids[id] {
			t.Fatal("nil/empty payload containers collapsed", err)
		}
		ids[id] = true
	}
	for name, change := range map[string]func(*content.CompiledAssetModelPaletteDef){
		"integer": func(p *content.CompiledAssetModelPaletteDef) { p.Materials[0].Property["bad"] = 1 }, "nested": func(p *content.CompiledAssetModelPaletteDef) {
			p.Materials[0].Property["bad"] = map[string]any{"x": true}
		}, "array": func(p *content.CompiledAssetModelPaletteDef) { p.Materials[0].Property["bad"] = []any{nil} }, "property-nan": func(p *content.CompiledAssetModelPaletteDef) { p.Materials[0].Property["bad"] = math.NaN() }, "scalar-inf": func(p *content.CompiledAssetModelPaletteDef) { p.IOR = float32(math.Inf(1)) }, "zero-surface": func(p *content.CompiledAssetModelPaletteDef) { p.SurfaceMaterials[0] = p.SurfaceMaterials[3] }, "unnormalized-kind": func(p *content.CompiledAssetModelPaletteDef) {
			v := p.SurfaceMaterials[3]
			v.Kind = " Stone "
			p.SurfaceMaterials[3] = v
		}, "duplicate-tags": func(p *content.CompiledAssetModelPaletteDef) {
			v := p.SurfaceMaterials[3]
			v.Tags = append(v.Tags, "solid")
			p.SurfaceMaterials[3] = v
		}, "unnormalized-tag": func(p *content.CompiledAssetModelPaletteDef) {
			v := p.SurfaceMaterials[3]
			v.Tags[0] = " Solid "
			p.SurfaceMaterials[3] = v
		},
	} {
		t.Run(name, func(t *testing.T) {
			q := c3g5Palette(t, 11)
			change(&q)
			if id, err := content.CompiledAssetModelPaletteIdentity(&q); err == nil || id != "" {
				t.Fatal("invalid palette identity accepted")
			}
		})
	}
	if id, err := content.CompiledAssetModelPaletteIdentity(nil); err == nil || id != "" {
		t.Fatal("nil palette accepted")
	}
}

func TestC3g5MixedHeaderCanonicalOwnership(t *testing.T) {
	input := c3g5Header(t)
	before, _ := json.Marshal(input)
	codec := c1aContentCodec(t, voxelcodec.Options{})
	frame, info, err := content.EncodeCompiledAssetModelHeader(input, codec)
	if err != nil {
		t.Fatal(err)
	}
	canonical := c3g5Header(t)
	c3g5Canonical(canonical)
	want, _ := json.Marshal(canonical)
	doc, _, err := codec.Decode(frame)
	if err != nil || doc.Kind != "compiled_asset_model_header" || doc.NormalBakeVersion != "" || len(doc.Bricks) != 0 || !bytes.Equal(doc.Metadata, want) {
		t.Fatal("header canonical envelope differs", err)
	}
	again, againInfo, err := content.EncodeCompiledAssetModelHeader(canonical, codec)
	if err != nil || againInfo != info || !bytes.Equal(frame, again) {
		t.Fatal("reference sorting changed identity", err)
	}
	out, outInfo, err := content.DecodeCompiledAssetModelHeader(frame, codec)
	if err != nil || outInfo != info {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(out)
	if !bytes.Equal(actual, want) {
		t.Fatal("decoded metadata changed")
	}
	second, _, err := content.DecodeCompiledAssetModelHeader(frame, codec)
	if err != nil {
		t.Fatal(err)
	}
	out.Asset.Parts[3].Source.Params["sx"] = 99
	out.Asset.Materials[0].Tags[0] = "changed"
	out.Models[0].Path = "changed"
	out.Palettes[0].Materials[0].Property["bool"] = false
	facts := out.Palettes[0].SurfaceMaterials[3]
	facts.Tags[0] = "changed"
	unchanged, _ := json.Marshal(second)
	if !bytes.Equal(unchanged, want) {
		t.Fatal("decoded header nested storage shared")
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("encode normalized/sorted caller")
	}
	// All legacy readers remain restricted to their own kind.
	if old, oldInfo, err := content.DecodeCompiledAssetHeader(frame, codec); err == nil || old != nil || oldInfo != (voxelcodec.Info{}) {
		t.Fatal("legacy header accepted model envelope")
	}
	oldFrame, _, err := content.EncodeCompiledAssetHeader(c3bHeader(), codec)
	if err != nil {
		t.Fatal(err)
	}
	if model, modelInfo, err := content.DecodeCompiledAssetModelHeader(oldFrame, codec); err == nil || model != nil || modelInfo != (voxelcodec.Info{}) {
		t.Fatal("model header accepted legacy envelope")
	}
}

func c3g5Reject(t *testing.T, h *content.CompiledAssetModelHeaderDef) {
	t.Helper()
	if data, info, err := content.EncodeCompiledAssetModelHeader(h, nil); err == nil || data != nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid model header accepted or partial")
	}
}

func TestC3g5ModelHeaderClosureAndAuthoredValidation(t *testing.T) {
	for name, change := range map[string]func(*content.CompiledAssetModelHeaderDef){
		"schema": func(h *content.CompiledAssetModelHeaderDef) { h.SchemaVersion = 2 }, "compiler": func(h *content.CompiledAssetModelHeaderDef) { h.CompilerVersion = "other" }, "collapse": func(h *content.CompiledAssetModelHeaderDef) { h.Asset.Runtime.CollapseVoxelParts = true }, "parent": func(h *content.CompiledAssetModelHeaderDef) { h.Asset.Parts[3].ParentID = "absent" }, "missing-material": func(h *content.CompiledAssetModelHeaderDef) { h.Asset.Parts[3].Source.MaterialID = "absent" }, "used-param": func(h *content.CompiledAssetModelHeaderDef) { h.Asset.Parts[3].Source.Params["sx"] = 0 }, "scale": func(h *content.CompiledAssetModelHeaderDef) { h.Asset.Parts[3].ModelScale = 0 }, "resolution": func(h *content.CompiledAssetModelHeaderDef) { h.Asset.Parts[3].VoxelResolution = 0 },
		"missing-model": func(h *content.CompiledAssetModelHeaderDef) { h.Models = h.Models[:1] }, "duplicate-model": func(h *content.CompiledAssetModelHeaderDef) { h.Models[1].PartID = h.Models[0].PartID }, "extra-model": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].PartID = "absent" }, "group-model": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].PartID = "group" }, "shape-model": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].PartID = "z-shape" }, "missing-shape": func(h *content.CompiledAssetModelHeaderDef) { h.Shapes = h.Shapes[:1] }, "model-lod": func(h *content.CompiledAssetModelHeaderDef) { h.LODs[0].PartID = "z-model" }, "path-shape": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].Path = h.Shapes[0].Path }, "path-lod": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].Path = h.LODs[0].Path }, "unsafe-path": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].Path = "../model.gkvox" }, "hash": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].ContentID = strings.Repeat("A", 64) }, "size": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].EncodedBytes = 95 }, "cid-base": func(h *content.CompiledAssetModelHeaderDef) { h.Models[1].BaseIdentity = strings.Repeat("f", 64) }, "cid-decoded": func(h *content.CompiledAssetModelHeaderDef) { h.Models[1].DecodedBytes++ }, "path-profile": func(h *content.CompiledAssetModelHeaderDef) { h.Models[1].Path = h.Models[0].Path },
		"missing-palette": func(h *content.CompiledAssetModelHeaderDef) { h.Palettes = h.Palettes[:1] }, "duplicate-palette": func(h *content.CompiledAssetModelHeaderDef) { h.Palettes = append(h.Palettes, h.Palettes[0]) }, "extra-palette": func(h *content.CompiledAssetModelHeaderDef) { h.Palettes = append(h.Palettes, c3g5Palette(t, 33)) }, "palette-id": func(h *content.CompiledAssetModelHeaderDef) { h.Palettes[0].Colors[3][0]++ }, "unknown-palette": func(h *content.CompiledAssetModelHeaderDef) { h.Models[0].PaletteID = strings.Repeat("f", 64) },
	} {
		t.Run(name, func(t *testing.T) { h := c3g5Header(t); change(h); c3g5Reject(t, h) })
	}
	c3g5Reject(t, nil)
	// VOX and scene model references retain authoring paths without loading them.
	for _, kind := range []content.AssetSourceKind{content.AssetSourceKindVoxModel, content.AssetSourceKindVoxSceneNode} {
		h := c3g5Header(t)
		h.Asset.Parts[3].Source = content.AssetSourceDef{Kind: kind, Path: "missing/source.vox", ModelIndex: 0, NodeName: "arm", MaterialID: "material"}
		if _, _, err := content.EncodeCompiledAssetModelHeader(h, nil); err != nil {
			t.Fatal("valid unresolved model source rejected", err)
		}
	}
	for _, kind := range []string{"parts", "models", "palettes", "materials"} {
		t.Run("limit-"+kind, func(t *testing.T) {
			h := c3g5Header(t)
			switch kind {
			case "parts":
				h.Asset.Parts = make([]content.AssetPartDef, 4097)
			case "models":
				h.Models = make([]content.CompiledAssetModelRefDef, 4097)
			case "palettes":
				h.Palettes = make([]content.CompiledAssetModelPaletteDef, 4097)
			case "materials":
				h.Palettes[0].Materials = make([]content.CompiledAssetModelMaterialDef, 4097)
			}
			c3g5Reject(t, h)
		})
	}
}

func TestC3g5TypedEnvelopeLimitsAndAtomicIO(t *testing.T) {
	h := c3g5Header(t)
	c3g5Canonical(h)
	metadata, _ := json.Marshal(h)
	codec := c1aContentCodec(t, voxelcodec.Options{})
	colors, _ := json.Marshal(h.Palettes[0].Colors)
	for name, malformed := range map[string][]byte{
		"short-colors": []byte(`[[0,0,0,0]]`),
		"long-colors":  append(append(bytes.Clone(colors[:len(colors)-1]), []byte(`,[0,0,0,0]`)...), ']'),
		"short-rgba":   bytes.Replace(colors, []byte(`[0,0,0,0]`), []byte(`[0,0,0]`), 1),
		"long-rgba":    bytes.Replace(colors, []byte(`[0,0,0,0]`), []byte(`[0,0,0,0,0]`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			d := voxelcodec.Document{Kind: "compiled_asset_model_header", Metadata: bytes.Replace(metadata, colors, malformed, 1)}
			frame, _, err := codec.Encode(d)
			if err != nil {
				t.Fatal(err)
			}
			if out, info, err := content.DecodeCompiledAssetModelHeader(frame, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("malformed fixed palette array accepted")
			}
		})
	}
	for name, change := range map[string]func(*voxelcodec.Document){"kind": func(d *voxelcodec.Document) { d.Kind = "compiled_asset_header" }, "bake": func(d *voxelcodec.Document) { d.NormalBakeVersion = "bake" }, "bricks": func(d *voxelcodec.Document) { d.Bricks = c3aShape().Bricks }, "whitespace": func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), metadata...) }, "unknown": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"extra":1,`), metadata[1:]...) }} {
		t.Run(name, func(t *testing.T) {
			d := voxelcodec.Document{Kind: "compiled_asset_model_header", Metadata: bytes.Clone(metadata)}
			change(&d)
			frame, _, err := codec.Encode(d)
			if err != nil {
				t.Fatal(err)
			}
			if out, info, err := content.DecodeCompiledAssetModelHeader(frame, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("invalid typed envelope accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "nested", "header.gkasset")
	saved, err := content.SaveCompiledAssetModelHeader(path, h, codec)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if out, info, err := content.LoadCompiledAssetModelHeader(path, codec); err != nil || info != saved || len(out.Models) != 2 {
		t.Fatal("header file roundtrip", err)
	}
	bad := c3g5Header(t)
	bad.Models = nil
	if info, err := content.SaveCompiledAssetModelHeader(path, bad, codec); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid save succeeded")
	}
	preserved, _ := os.ReadFile(path)
	if !bytes.Equal(old, preserved) {
		t.Fatal("failed save replaced old header")
	}
	for _, limits := range []voxelcodec.Limits{{MaxMetadataBytes: 1}, {MaxEncodedBytes: int64(len(old) - 1)}, {MaxDecodedBytes: 32}} {
		c := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
		if out, info, err := content.DecodeCompiledAssetModelHeader(old, c); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("decode ignored limit")
		}
		if out, info, err := content.LoadCompiledAssetModelHeader(path, c); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("load ignored limit")
		}
	}
	if err := os.WriteFile(path, append(bytes.Clone(old), 0), 0600); err != nil {
		t.Fatal(err)
	}
	if out, info, err := content.LoadCompiledAssetModelHeader(path, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("load accepted trailing frame")
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "borrowed-probe"}); err != nil {
		t.Fatal("header closed borrowed codec", err)
	}
}
