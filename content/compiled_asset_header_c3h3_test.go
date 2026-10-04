package content_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3h3Header() *content.CompiledAssetHeaderDef {
	h := c3bHeader()
	h.SchemaVersion = content.CompiledAssetLODHeaderSchemaVersion
	h.CompilerVersion = content.CompiledAssetLODHeaderCompilerVersion
	h.LODs = []content.CompiledAssetLODRefDef{
		{PartID: "z-shape", Path: "lods/shared.gkvox", ContentID: strings.Repeat("c", 64), SourceContentID: h.Shapes[0].ContentID, EncodedBytes: 180, DecodedBytes: 300, Factor: 2, ReductionVersion: content.CompiledAssetLOD2xReductionVersion},
		{PartID: "a-shape", Path: "lods/shared.gkvox", ContentID: strings.Repeat("c", 64), SourceContentID: h.Shapes[1].ContentID, EncodedBytes: 180, DecodedBytes: 300, Factor: 2, ReductionVersion: content.CompiledAssetLOD2xReductionVersion},
	}
	return h
}

func c3h3Canonical(h *content.CompiledAssetHeaderDef) {
	sort.Slice(h.Shapes, func(i, j int) bool { return h.Shapes[i].PartID < h.Shapes[j].PartID })
	sort.Slice(h.LODs, func(i, j int) bool { return h.LODs[i].PartID < h.LODs[j].PartID })
}

func TestC3h3LegacyBytesAndDefaultsUnchanged(t *testing.T) {
	if content.CurrentCompiledAssetHeaderSchemaVersion != 1 || content.CurrentCompiledAssetCompilerVersion != "gekko-compiled-asset-v1" {
		t.Fatal("default compiler/header version changed")
	}
	if content.CompiledAssetLODHeaderSchemaVersion != 2 || content.CompiledAssetLODHeaderCompilerVersion != "gekko-compiled-asset-v2" {
		t.Fatal("wrong explicit LOD version constants")
	}
	h := c3bHeader()
	codec := c1aContentCodec(t, voxelcodec.Options{})
	frame, info, err := content.EncodeCompiledAssetHeader(h, nil)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(frame)
	// Captured from schema1 production before this batch; the complete frame stays stable.
	if hex.EncodeToString(hash[:]) != "37f59db7f1b1dc8e4ca85b817617148b6207ab78e0caf9b127bd4d3826026277" {
		t.Fatal("schema1 frame bytes changed")
	}
	c3h3Canonical(h)
	oldMetadata, err := json.Marshal(struct {
		SchemaVersion   int                                `json:"schema_version"`
		CompilerVersion string                             `json:"compiler_version"`
		Asset           *content.AssetDef                  `json:"asset"`
		Shapes          []content.CompiledAssetShapeRefDef `json:"shapes,omitempty"`
	}{h.SchemaVersion, h.CompilerVersion, h.Asset, h.Shapes})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := codec.Decode(frame)
	if err != nil || !bytes.Equal(doc.Metadata, oldMetadata) {
		t.Fatal("legacy metadata fields/order changed", err)
	}
	out, loaded, err := content.DecodeCompiledAssetHeader(frame, nil)
	if err != nil || loaded != info || !reflect.DeepEqual(out, h) {
		t.Fatal("legacy read changed", err)
	}
	again, againInfo, err := content.EncodeCompiledAssetHeader(out, nil)
	if err != nil || againInfo != info || !bytes.Equal(frame, again) {
		t.Fatal("legacy reencode changed", err)
	}
	for _, refs := range [][]content.CompiledAssetLODRefDef{nil, {}} {
		h.LODs = refs
		b, _, err := content.EncodeCompiledAssetHeader(h, nil)
		if err != nil || !bytes.Equal(frame, b) {
			t.Fatal("empty LOD table changed v1", err)
		}
	}
}

func TestC3h3CanonicalLODReferencesAndOwnedDecode(t *testing.T) {
	h := c3h3Header()
	before, _ := json.Marshal(h)
	canonical := c3h3Header()
	c3h3Canonical(canonical)
	codec := c1aContentCodec(t, voxelcodec.Options{})
	frame, info, err := content.EncodeCompiledAssetHeader(h, codec)
	if err != nil {
		t.Fatal(err)
	}
	reordered, ri, err := content.EncodeCompiledAssetHeader(canonical, codec)
	if err != nil || ri != info || !bytes.Equal(frame, reordered) {
		t.Fatal("LOD order changes frame", err)
	}
	want, _ := json.Marshal(canonical)
	doc, _, err := codec.Decode(frame)
	if err != nil || !bytes.Equal(want, doc.Metadata) {
		t.Fatal("wrong canonical LOD metadata", err)
	}
	if !bytes.Contains(doc.Metadata, []byte(`"shapes":`)) || bytes.Index(doc.Metadata, []byte(`"lods":`)) < bytes.Index(doc.Metadata, []byte(`"shapes":`)) {
		t.Fatal("LOD table must follow authoritative shapes")
	}
	for _, borrowed := range []*voxelcodec.Codec{nil, codec, c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 42, Bytes: bytes.Repeat([]byte("compiled_asset_header source_content_id lods reduction_version"), 8)}})} {
		b, bi, err := content.EncodeCompiledAssetHeader(h, borrowed)
		if err != nil || bi.ContentID != info.ContentID {
			t.Fatal("profile changed header identity", err)
		}
		out, oi, err := content.DecodeCompiledAssetHeader(b, borrowed)
		if err != nil || oi != bi || !reflect.DeepEqual(out, canonical) {
			t.Fatal("LOD roundtrip", err)
		}
		if borrowed != nil {
			if _, _, err := borrowed.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
				t.Fatal("borrowed codec closed", err)
			}
		}
	}
	a, _, err := content.DecodeCompiledAssetHeader(frame, codec)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := content.DecodeCompiledAssetHeader(frame, codec)
	if err != nil {
		t.Fatal(err)
	}
	a.LODs[0].Path = "changed"
	a.Asset.Parts[0].Source.VoxelShape.Palette[0].MaterialID = "changed"
	a.Shapes[0].ContentID = "changed"
	if !reflect.DeepEqual(b, canonical) {
		t.Fatal("decoded LOD/source/asset metadata shared")
	}
	after, _ := json.Marshal(h)
	if !bytes.Equal(before, after) {
		t.Fatal("encoding mutates caller")
	}
	h.LODs[1].ContentID = "changed"
	if b.LODs[0].ContentID != strings.Repeat("c", 64) {
		t.Fatal("decoded LOD aliases input")
	}
	// References intentionally do not exist: typed header validation performs no IO.
	for _, refs := range [][]content.CompiledAssetLODRefDef{nil, {}} {
		empty := c3h3Header()
		empty.LODs = refs
		frame, _, err := content.EncodeCompiledAssetHeader(empty, nil)
		if err != nil {
			t.Fatal("v2 empty LOD table rejected", err)
		}
		out, _, err := content.DecodeCompiledAssetHeader(frame, nil)
		if err != nil || len(out.LODs) != 0 || len(out.Shapes) != 2 {
			t.Fatal("empty v2 metadata changed", err)
		}
	}
	// LOD is optional per part; independent derivative paths can use distinct identities.
	single := c3h3Header()
	single.LODs = single.LODs[:1]
	single.LODs[0].EncodedBytes = 96
	single.LODs[0].DecodedBytes = 1
	if _, _, err := content.EncodeCompiledAssetHeader(single, nil); err != nil {
		t.Fatal("single optional LOD/lower size bounds rejected", err)
	}
	alias := c3h3Header()
	alias.LODs[1].Path = "lods/alias.gkvox"
	alias.LODs[1].EncodedBytes++
	if _, _, err := content.EncodeCompiledAssetHeader(alias, nil); err != nil {
		t.Fatal("logical LOD alias with distinct encoded size rejected", err)
	}
	independent := c3h3Header()
	independent.LODs[1].Path = "lods/other.gkvox"
	independent.LODs[1].ContentID = strings.Repeat("d", 64)
	independent.LODs[1].EncodedBytes++
	if _, _, err := content.EncodeCompiledAssetHeader(independent, nil); err != nil {
		t.Fatal("independent LOD paths rejected", err)
	}
}

func TestC3h3RejectInvalidLODReferencesOnEncodeAndDecode(t *testing.T) {
	cases := map[string]func(*content.CompiledAssetHeaderDef){
		"schema1-lods": func(h *content.CompiledAssetHeaderDef) {
			h.SchemaVersion = 1
			h.CompilerVersion = "gekko-compiled-asset-v1"
		},
		"schema1-compiler2":       func(h *content.CompiledAssetHeaderDef) { h.SchemaVersion = 1 },
		"schema2-compiler1":       func(h *content.CompiledAssetHeaderDef) { h.CompilerVersion = "gekko-compiled-asset-v1" },
		"future-schema":           func(h *content.CompiledAssetHeaderDef) { h.SchemaVersion = 3 },
		"group-part":              func(h *content.CompiledAssetHeaderDef) { h.LODs[0].PartID = "group" },
		"missing-part":            func(h *content.CompiledAssetHeaderDef) { h.LODs[0].PartID = "missing" },
		"duplicate-part":          func(h *content.CompiledAssetHeaderDef) { h.LODs[1].PartID = h.LODs[0].PartID },
		"missing-source-ref":      func(h *content.CompiledAssetHeaderDef) { h.Shapes = h.Shapes[1:] },
		"source-hash-mismatch":    func(h *content.CompiledAssetHeaderDef) { h.LODs[0].SourceContentID = strings.Repeat("d", 64) },
		"source-base-not-content": func(h *content.CompiledAssetHeaderDef) { h.LODs[0].SourceContentID = h.Shapes[0].BaseIdentity },
		"source-uppercase":        func(h *content.CompiledAssetHeaderDef) { h.LODs[0].SourceContentID = strings.Repeat("A", 64) },
		"content-uppercase":       func(h *content.CompiledAssetHeaderDef) { h.LODs[0].ContentID = strings.Repeat("C", 64) },
		"content-nonhex":          func(h *content.CompiledAssetHeaderDef) { h.LODs[0].ContentID = strings.Repeat("g", 64) },
		"content-short":           func(h *content.CompiledAssetHeaderDef) { h.LODs[0].ContentID = "abcd" },
		"encoded-small":           func(h *content.CompiledAssetHeaderDef) { h.LODs[0].EncodedBytes = 95 },
		"encoded-negative":        func(h *content.CompiledAssetHeaderDef) { h.LODs[0].EncodedBytes = -1 },
		"decoded-zero":            func(h *content.CompiledAssetHeaderDef) { h.LODs[0].DecodedBytes = 0 },
		"decoded-negative":        func(h *content.CompiledAssetHeaderDef) { h.LODs[0].DecodedBytes = -1 },
		"factor":                  func(h *content.CompiledAssetHeaderDef) { h.LODs[0].Factor = 3 },
		"reduction":               func(h *content.CompiledAssetHeaderDef) { h.LODs[0].ReductionVersion = "nearest" },
		"shape-path-collision":    func(h *content.CompiledAssetHeaderDef) { h.LODs[0].Path = h.Shapes[0].Path },
		"other-shape-path-collision": func(h *content.CompiledAssetHeaderDef) {
			h.Shapes[1].Path = "shapes/other.gkvox"
			h.LODs[0].Path = h.Shapes[1].Path
		},
		"shared-content-conflict": func(h *content.CompiledAssetHeaderDef) { h.LODs[0].ContentID = strings.Repeat("d", 64) },
		"shared-encoded-conflict": func(h *content.CompiledAssetHeaderDef) { h.LODs[0].EncodedBytes++ },
		"shared-decoded-conflict": func(h *content.CompiledAssetHeaderDef) { h.LODs[0].DecodedBytes++ },
		"shared-source-conflict": func(h *content.CompiledAssetHeaderDef) {
			h.Shapes[0].Path = "shapes/other.gkvox"
			h.Shapes[0].ContentID = strings.Repeat("d", 64)
			h.LODs[0].SourceContentID = h.Shapes[0].ContentID
		},
		"logical-alias-source-conflict": func(h *content.CompiledAssetHeaderDef) {
			h.LODs[1].Path = "lods/alias.gkvox"
			h.Shapes[1].Path = "shapes/other.gkvox"
			h.Shapes[1].ContentID = strings.Repeat("d", 64)
			h.LODs[1].SourceContentID = h.Shapes[1].ContentID
		},
		"logical-alias-decoded-conflict": func(h *content.CompiledAssetHeaderDef) { h.LODs[1].Path = "lods/alias.gkvox"; h.LODs[1].DecodedBytes++ },
		"oversize-encoded": func(h *content.CompiledAssetHeaderDef) {
			h.LODs[0].EncodedBytes = voxelcodec.DefaultLimits().MaxEncodedBytes + 1
		},
		"oversize-decoded": func(h *content.CompiledAssetHeaderDef) {
			h.LODs[0].DecodedBytes = voxelcodec.DefaultLimits().MaxDecodedBytes + 1
		},
		"too-many-refs": func(h *content.CompiledAssetHeaderDef) {
			h.LODs = make([]content.CompiledAssetLODRefDef, content.MaxCompiledAssetParts+1)
		},
	}
	// A second reference must not mask the scalar/source validation through a
	// conflicting shared path; isolate those checks on a single optional LOD.
	for _, name := range []string{"source-hash-mismatch", "source-base-not-content", "source-uppercase", "content-uppercase", "content-nonhex", "content-short", "encoded-small", "encoded-negative", "decoded-zero", "decoded-negative", "factor", "reduction"} {
		change := cases[name]
		cases["single-"+name] = func(h *content.CompiledAssetHeaderDef) { h.LODs = h.LODs[:1]; change(h) }
	}
	for name, path := range map[string]string{"empty": "", "absolute": "/lod.gkvox", "parent": "a/../lod.gkvox", "dot": ".", "redundant": "a//lod.gkvox", "backslash": `a\lod.gkvox`, "drive": "C:lod.gkvox", "nul": "a\x00b", "utf8": string([]byte{0xff})} {
		path := path
		cases["path-"+name] = func(h *content.CompiledAssetHeaderDef) { h.LODs[0].Path = path }
	}
	codec := c1aContentCodec(t, voxelcodec.Options{})
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			h := c3h3Header()
			change(h)
			c3bReject(t, h, codec)
			// Valid C1 envelope isolates typed header validation from generic frame checks.
			c3h3Canonical(h)
			metadata, err := json.Marshal(h)
			if err != nil {
				t.Fatal(err)
			}
			raw, _, err := codec.Encode(voxelcodec.Document{Kind: "compiled_asset_header", Metadata: metadata})
			if err != nil {
				t.Fatal("generic fixture rejected", err)
			}
			if out, info, err := content.DecodeCompiledAssetHeader(raw, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("typed invalid LOD header accepted or partial")
			}
		})
	}
}

func TestC3h3TypedCanonicalMetadataAndLODReferenceProfiles(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	h := c3h3Header()
	c3h3Canonical(h)
	metadata, _ := json.Marshal(h)
	cases := map[string]func(*voxelcodec.Document){
		"unknown-field": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"extra":1,`), metadata[1:]...) },
		"unknown-lod-field": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"lods":[{`), []byte(`"lods":[{"extra":1,`), 1)
		},
		"whitespace":     func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), metadata...) },
		"duplicate-lods": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"lods":[],`), metadata[1:]...) },
		"lod-order": func(d *voxelcodec.Document) {
			bad := c3h3Header()
			sort.Slice(bad.Shapes, func(i, j int) bool { return bad.Shapes[i].PartID < bad.Shapes[j].PartID })
			d.Metadata, _ = json.Marshal(bad)
		},
		"field-order": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`{"schema_version":2,"compiler_version":"gekko-compiled-asset-v2"`), []byte(`{"compiler_version":"gekko-compiled-asset-v2","schema_version":2`), 1)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			doc := voxelcodec.Document{Kind: "compiled_asset_header", Metadata: bytes.Clone(metadata)}
			change(&doc)
			raw, _, err := codec.Encode(doc)
			if err != nil {
				t.Fatal(err)
			}
			if out, info, err := content.DecodeCompiledAssetHeader(raw, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("noncanonical LOD metadata accepted")
			}
		})
	}
	// Oversized derivative reference fails even while authoritative shapes and header
	// frame itself satisfy the caller's borrowed profile.
	h.LODs[0].EncodedBytes = 100000
	h.LODs[1].EncodedBytes = 100000
	h.LODs[0].DecodedBytes = 100000
	h.LODs[1].DecodedBytes = 100000
	raw, _, err := content.EncodeCompiledAssetHeader(h, codec)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "header.gkassetc")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for name, limits := range map[string]voxelcodec.Limits{"encoded-ref": {MaxEncodedBytes: 99999}, "decoded-ref": {MaxDecodedBytes: 99999}} {
		t.Run(name, func(t *testing.T) {
			limited := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
			c3bReject(t, h, limited)
			if out, info, err := content.DecodeCompiledAssetHeader(raw, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("decode ignored LOD profile")
			}
			if out, info, err := content.LoadCompiledAssetHeader(path, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("load ignored LOD profile")
			}
			if _, _, err := limited.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
				t.Fatal("limited codec closed", err)
			}
		})
	}
}

func TestC3h3LODHeaderFileRoundTripAndFailedSave(t *testing.T) {
	h := c3h3Header()
	codec := c1aContentCodec(t, voxelcodec.Options{})
	path := filepath.Join(t.TempDir(), "nested", "header.gkassetc")
	saved, err := content.SaveCompiledAssetHeader(path, h, codec)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, loaded, err := content.LoadCompiledAssetHeader(path, codec)
	c3h3Canonical(h)
	if err != nil || saved != loaded || !reflect.DeepEqual(out, h) {
		t.Fatal("LOD file roundtrip", err)
	}
	bad := c3h3Header()
	bad.LODs[0].SourceContentID = "wrong"
	if info, err := content.SaveCompiledAssetHeader(path, bad, codec); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid LOD save succeeded")
	}
	preserved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, preserved) {
		t.Fatal("invalid LOD save changed published header")
	}
	closed := c1aContentCodec(t, voxelcodec.Options{})
	closed.Close()
	c3bReject(t, h, closed)
	if info, err := content.SaveCompiledAssetHeader(path, h, closed); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed LOD save succeeded")
	}
	if out, info, err := content.LoadCompiledAssetHeader(path, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed LOD load succeeded")
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("file adapter closed codec", err)
	}
}
