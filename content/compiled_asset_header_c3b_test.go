package content_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3bHeader() *content.CompiledAssetHeaderDef {
	transform := content.AssetTransformDef{Position: content.Vec3{1, 2, 3}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}, Pivot: content.Vec3{0.5, 0, 0}}
	shape := func(id string) content.AssetPartDef {
		return content.AssetPartDef{ID: id, Name: id, ParentID: "group", ModelScale: 2, VoxelResolution: 0.25, Transform: transform, Tags: []string{"shape-tag"}, Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 7, MaterialID: "material"}}}}}
	}
	return &content.CompiledAssetHeaderDef{SchemaVersion: content.CurrentCompiledAssetHeaderSchemaVersion, CompilerVersion: content.CurrentCompiledAssetCompilerVersion, Asset: &content.AssetDef{ID: "asset", SchemaVersion: 4, Name: "Compiled asset", Tags: []string{"asset-tag"}, Materials: []content.AssetMaterialDef{{ID: "material", Name: "Material", IOR: 1.5, Tags: []string{"material-tag"}, BaseColor: [4]uint8{1, 2, 3, 255}}}, Runtime: &content.AssetRuntimeDef{ShadowMaxDistance: 10}, Skeleton: &content.AssetSkeletonDef{Bones: []content.AssetBoneDef{{ID: "root-bone", JointID: "root-joint", Name: "Root bone", Transform: transform, Tags: []string{"bone-tag"}}, {ID: "child-bone", JointID: "child-joint", Name: "Child bone", ParentID: "root-bone", Transform: transform}}}, AnimationSetPaths: []string{"missing/animation.json"}, DefaultAnimationClipID: "idle", Parts: []content.AssetPartDef{shape("z-shape"), {ID: "group", Name: "Group", Transform: transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}}, shape("a-shape")}, Lights: []content.AssetLightDef{{ID: "light", Name: "Light", ParentID: "z-shape", Transform: transform, Type: content.AssetLightTypePoint, Range: 5, Intensity: 2}}, Emitters: []content.AssetEmitterDef{{ID: "emitter", Name: "Emitter", ParentID: "a-shape", Transform: transform, Emitter: content.EmitterDef{TexturePath: "missing/texture.png"}}}, Markers: []content.AssetMarkerDef{{ID: "marker", Name: "Marker", ParentID: "z-shape", Transform: transform, Kind: content.AssetMarkerKindMuzzle, Tags: []string{"marker-tag"}}}}, Shapes: []content.CompiledAssetShapeRefDef{
		{PartID: "z-shape", Path: "shapes/shared.gkvox", ContentID: strings.Repeat("a", 64), BaseIdentity: strings.Repeat("b", 64), EncodedBytes: 200, DecodedBytes: 400},
		{PartID: "a-shape", Path: "shapes/shared.gkvox", ContentID: strings.Repeat("a", 64), BaseIdentity: strings.Repeat("b", 64), EncodedBytes: 200, DecodedBytes: 400},
	}}
}

func TestC3bCanonicalHeaderRoundTripAndOwnedMetadata(t *testing.T) {
	input := c3bHeader()
	if validation := content.ValidateAsset(input.Asset, content.AssetValidationOptions{}); validation.HardErrorCount != 0 {
		t.Fatalf("invalid fixture: %+v", validation)
	}
	before, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	codec := c1aContentCodec(t, voxelcodec.Options{})
	frame, info, err := content.EncodeCompiledAssetHeader(input, codec)
	if err != nil || info.ContentID == "" || info.EncodedBytes != int64(len(frame)) {
		t.Fatalf("encode info=%+v err=%v", info, err)
	}
	canonical := c3bHeader()
	canonical.Shapes[0], canonical.Shapes[1] = canonical.Shapes[1], canonical.Shapes[0]
	wantMetadata, _ := json.Marshal(canonical)
	doc, _, err := codec.Decode(frame)
	if err != nil || doc.Kind != "compiled_asset_header" || doc.NormalBakeVersion != "" || len(doc.Bricks) != 0 || !bytes.Equal(doc.Metadata, wantMetadata) {
		t.Fatalf("typed document mismatch: %+v err=%v", doc, err)
	}
	reordered, reorderedInfo, err := content.EncodeCompiledAssetHeader(canonical, codec)
	if err != nil || !bytes.Equal(frame, reordered) || reorderedInfo != info {
		t.Fatal("reference order changed canonical frame", err)
	}
	for _, borrowed := range []*voxelcodec.Codec{nil, codec, c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 41, Bytes: bytes.Repeat([]byte("compiled_asset_header schema_version compiler_version parts voxel_shape material"), 8)}})} {
		data, saved, err := content.EncodeCompiledAssetHeader(input, borrowed)
		if err != nil || saved.ContentID != info.ContentID {
			t.Fatal("profile changed canonical header identity", err)
		}
		output, loaded, err := content.DecodeCompiledAssetHeader(data, borrowed)
		if err != nil || loaded != saved || !reflect.DeepEqual(output, canonical) {
			t.Fatalf("header metadata not preserved: output=%+v err=%v", output, err)
		}
		if borrowed != nil {
			if _, _, err := borrowed.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
				t.Fatal("borrowed codec closed", err)
			}
		}
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("encode normalized or reordered caller metadata")
	}
	first, _, err := content.DecodeCompiledAssetHeader(frame, codec)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := content.DecodeCompiledAssetHeader(frame, codec)
	if err != nil {
		t.Fatal(err)
	}
	first.Asset.Parts[0].Source.VoxelShape.Palette[0].MaterialID = "changed"
	first.Asset.Materials[0].Tags[0] = "changed"
	first.Asset.Markers[0].Tags[0] = "changed"
	first.Asset.AnimationSetPaths[0] = "changed"
	first.Asset.Skeleton.Bones[0].Tags[0] = "changed"
	first.Asset.Skeleton.Bones[1].Transform.Pivot[0] = 99
	first.Shapes[0].Path = "changed"
	if !reflect.DeepEqual(second, canonical) {
		t.Fatal("decoded nested metadata aliases another decode")
	}
	input.Asset.Parts[0].Source.VoxelShape.Palette[0].Value = 88
	if second.Asset.Parts[0].Source.VoxelShape.Palette[0].Value != 7 {
		t.Fatal("decoded metadata aliases input")
	}
	// Referenced files intentionally do not exist: this adapter validates structure only.
}

func TestC3bGroupOnlyEmptyReferences(t *testing.T) {
	for _, refs := range [][]content.CompiledAssetShapeRefDef{nil, {}} {
		input := c3bHeader()
		input.Asset.Parts = input.Asset.Parts[1:2]
		input.Asset.Lights = nil
		input.Asset.Emitters = nil
		input.Asset.Markers = nil
		input.Shapes = refs
		data, _, err := content.EncodeCompiledAssetHeader(input, nil)
		if err != nil {
			t.Fatal(err)
		}
		output, _, err := content.DecodeCompiledAssetHeader(data, nil)
		if err != nil || len(output.Shapes) != 0 || len(output.Asset.Parts) != 1 {
			t.Fatal("group-only header failed", err)
		}
	}
}

func c3bReject(t *testing.T, input *content.CompiledAssetHeaderDef, codec *voxelcodec.Codec) {
	t.Helper()
	if data, info, err := content.EncodeCompiledAssetHeader(input, codec); err == nil || data != nil || info != (voxelcodec.Info{}) {
		t.Fatalf("invalid header accepted or partial: len=%d info=%+v err=%v", len(data), info, err)
	}
}

func TestC3bRejectInvalidAuthoredHeaderAndReferences(t *testing.T) {
	cases := map[string]func(*content.CompiledAssetHeaderDef){
		"header-schema-zero":  func(h *content.CompiledAssetHeaderDef) { h.SchemaVersion = 0 },
		"compiler-version":    func(h *content.CompiledAssetHeaderDef) { h.CompilerVersion = "other" },
		"nil-asset":           func(h *content.CompiledAssetHeaderDef) { h.Asset = nil },
		"asset-schema-zero":   func(h *content.CompiledAssetHeaderDef) { h.Asset.SchemaVersion = 0 },
		"asset-schema-future": func(h *content.CompiledAssetHeaderDef) { h.Asset.SchemaVersion = 5 },
		"collapse":            func(h *content.CompiledAssetHeaderDef) { h.Asset.Runtime.CollapseVoxelParts = true },
		"vox-source": func(h *content.CompiledAssetHeaderDef) {
			h.Asset.Parts[0].Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "missing.vox"}
		},
		"procedural-source": func(h *content.CompiledAssetHeaderDef) {
			h.Asset.Parts[0].Source = content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube"}
		},
		"group-shape": func(h *content.CompiledAssetHeaderDef) {
			h.Asset.Parts[1].Source.VoxelShape = &content.AssetVoxelShapeDef{}
		},
		"nil-inline-shape": func(h *content.CompiledAssetHeaderDef) { h.Asset.Parts[0].Source.VoxelShape = nil },
		"inline-geometry": func(h *content.CompiledAssetHeaderDef) {
			h.Asset.Parts[0].Source.VoxelShape.Voxels = []content.VoxelObjectVoxelDef{{Value: 7}}
		},
		"scale-zero":        func(h *content.CompiledAssetHeaderDef) { h.Asset.Parts[0].ModelScale = 0 },
		"resolution-zero":   func(h *content.CompiledAssetHeaderDef) { h.Asset.Parts[0].VoxelResolution = 0 },
		"scale-inf":         func(h *content.CompiledAssetHeaderDef) { h.Asset.Parts[0].ModelScale = float32(math.Inf(1)) },
		"metadata-nan":      func(h *content.CompiledAssetHeaderDef) { h.Asset.Lights[0].Intensity = float32(math.NaN()) },
		"bad-parent":        func(h *content.CompiledAssetHeaderDef) { h.Asset.Parts[0].ParentID = "absent" },
		"duplicate-item-id": func(h *content.CompiledAssetHeaderDef) { h.Asset.Markers[0].ID = h.Asset.Parts[0].ID },
		"missing-ref":       func(h *content.CompiledAssetHeaderDef) { h.Shapes = h.Shapes[:1] },
		"duplicate-ref":     func(h *content.CompiledAssetHeaderDef) { h.Shapes[1].PartID = h.Shapes[0].PartID },
		"extra-ref": func(h *content.CompiledAssetHeaderDef) {
			h.Shapes = append(h.Shapes, h.Shapes[0])
			h.Shapes[2].PartID = "absent"
		},
		"group-ref":               func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].PartID = "group" },
		"encoded-small":           func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].EncodedBytes = 95 },
		"decoded-zero":            func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].DecodedBytes = 0 },
		"content-uppercase":       func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].ContentID = strings.Repeat("A", 64) },
		"base-nonhex":             func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].BaseIdentity = strings.Repeat("g", 64) },
		"content-short":           func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].ContentID = "abcd" },
		"shared-content-conflict": func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].ContentID = strings.Repeat("c", 64) },
		"shared-base-conflict":    func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].BaseIdentity = strings.Repeat("c", 64) },
		"shared-encoded-conflict": func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].EncodedBytes++ },
		"shared-decoded-conflict": func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].DecodedBytes++ },
	}
	for name, set := range map[string]func(*content.CompiledAssetHeaderDef, string){"asset": func(h *content.CompiledAssetHeaderDef, id string) { h.Asset.ID = id }, "material": func(h *content.CompiledAssetHeaderDef, id string) { h.Asset.Materials[0].ID = id }, "part": func(h *content.CompiledAssetHeaderDef, id string) { h.Asset.Parts[0].ID = id }, "light": func(h *content.CompiledAssetHeaderDef, id string) { h.Asset.Lights[0].ID = id }, "emitter": func(h *content.CompiledAssetHeaderDef, id string) { h.Asset.Emitters[0].ID = id }, "marker": func(h *content.CompiledAssetHeaderDef, id string) { h.Asset.Markers[0].ID = id }} {
		for label, id := range map[string]string{"blank": " ", "utf8": string([]byte{0xff})} {
			set, id := set, id
			cases[name+"-"+label] = func(h *content.CompiledAssetHeaderDef) { set(h, id) }
		}
	}
	for name, path := range map[string]string{"empty": "", "absolute": "/shape.gkvox", "parent": "a/../shape.gkvox", "dot": ".", "redundant": "a//shape.gkvox", "backslash": `a\shape.gkvox`, "drive": "C:shape.gkvox", "nul": "a" + string([]byte{0}) + "b", "utf8": string([]byte{0xff})} {
		path := path
		cases["path-"+name] = func(h *content.CompiledAssetHeaderDef) { h.Shapes[0].Path = path }
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			input := c3bHeader()
			if !strings.HasPrefix(name, "shared-") {
				input.Shapes[1].Path = "shapes/other.gkvox"
			}
			change(input)
			c3bReject(t, input, nil)
		})
	}
	c3bReject(t, nil, nil)
	for _, kind := range []string{"parts", "refs"} {
		t.Run("limit-"+kind, func(t *testing.T) {
			h := c3bHeader()
			if kind == "refs" {
				h.Shapes = make([]content.CompiledAssetShapeRefDef, content.MaxCompiledAssetParts+1)
			} else {
				h.Asset.Parts = make([]content.AssetPartDef, content.MaxCompiledAssetParts+1)
				h.Shapes = nil
				h.Asset.Lights = nil
				h.Asset.Emitters = nil
				h.Asset.Markers = nil
				for i := range h.Asset.Parts {
					h.Asset.Parts[i] = content.AssetPartDef{ID: fmt.Sprint("group-", i), Name: "Group", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}}
				}
			}
			c3bReject(t, h, nil)
		})
	}
}

func TestC3bRejectTypedFrameAndBoundedProfiles(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	input := c3bHeader()
	input.Shapes[0], input.Shapes[1] = input.Shapes[1], input.Shapes[0]
	metadata, _ := json.Marshal(input)
	cases := map[string]func(*voxelcodec.Document){
		"kind": func(d *voxelcodec.Document) { d.Kind = "compiled_asset_shape" },
		"bake": func(d *voxelcodec.Document) { d.NormalBakeVersion = "bake" },
		"brick": func(d *voxelcodec.Document) {
			d.Bricks = []voxelcodec.Brick{{Occupancy: [8]uint64{1}, Values: []uint8{7}}}
		},
		"whitespace":    func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), d.Metadata...) },
		"unknown-key":   func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"extra":1,`), d.Metadata[1:]...) },
		"duplicate-key": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"schema_version":1,`), d.Metadata[1:]...) },
		"key-order": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(d.Metadata, []byte(`{"schema_version":1,"compiler_version":"gekko-compiled-asset-v1"`), []byte(`{"compiler_version":"gekko-compiled-asset-v1","schema_version":1`), 1)
		},
		"refs-order":    func(d *voxelcodec.Document) { h := c3bHeader(); d.Metadata, _ = json.Marshal(h) },
		"typed-invalid": func(d *voxelcodec.Document) { h := c3bHeader(); h.SchemaVersion = 2; d.Metadata, _ = json.Marshal(h) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			doc := voxelcodec.Document{Kind: "compiled_asset_header", Metadata: bytes.Clone(metadata)}
			change(&doc)
			frame, _, err := codec.Encode(doc)
			if err != nil {
				t.Fatal("generic typed fixture invalid", err)
			}
			if out, info, err := content.DecodeCompiledAssetHeader(frame, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("typed invalid frame accepted or partial result")
			}
		})
	}
	frame, _, err := content.EncodeCompiledAssetHeader(input, codec)
	if err != nil {
		t.Fatal(err)
	}
	for name, limits := range map[string]voxelcodec.Limits{"metadata": {MaxMetadataBytes: 1}, "encoded-ref": {MaxEncodedBytes: 199}, "decoded-ref": {MaxDecodedBytes: 399}} {
		t.Run(name, func(t *testing.T) {
			limited := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
			c3bReject(t, input, limited)
			if out, info, err := content.DecodeCompiledAssetHeader(frame, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("borrowed profile ignored")
			}
		})
	}
	// Metadata bytes stay under the codec profile while an oversized referenced frame is rejected.
	for _, field := range []string{"encoded", "decoded"} {
		t.Run("reference-only-"+field, func(t *testing.T) {
			h := c3bHeader()
			profile := voxelcodec.DefaultLimits()
			if field == "encoded" {
				h.Shapes[0].EncodedBytes = profile.MaxEncodedBytes + 1
				h.Shapes[1].EncodedBytes = h.Shapes[0].EncodedBytes
			} else {
				h.Shapes[0].DecodedBytes = profile.MaxDecodedBytes + 1
				h.Shapes[1].DecodedBytes = h.Shapes[0].DecodedBytes
			}
			c3bReject(t, h, codec)
			h.Shapes[0], h.Shapes[1] = h.Shapes[1], h.Shapes[0]
			meta, _ := json.Marshal(h)
			raw, _, err := codec.Encode(voxelcodec.Document{Kind: "compiled_asset_header", Metadata: meta})
			if err != nil {
				t.Fatal(err)
			}
			if out, info, err := content.DecodeCompiledAssetHeader(raw, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("referenced frame size exceeds profile but header accepted")
			}
		})
	}
}

func TestC3bAtomicFilesClosedCodecAndFrameErrors(t *testing.T) {
	input := c3bHeader()
	codec := c1aContentCodec(t, voxelcodec.Options{})
	path := filepath.Join(t.TempDir(), "nested", "header.gkasset")
	saved, err := content.SaveCompiledAssetHeader(path, input, codec)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, info, err := content.LoadCompiledAssetHeader(path, codec)
	if err != nil || info != saved || out.Asset.ID != input.Asset.ID {
		t.Fatal("file load failed", err)
	}
	invalid := c3bHeader()
	invalid.Shapes = nil
	if info, err := content.SaveCompiledAssetHeader(path, invalid, codec); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid save succeeded")
	}
	preserved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, preserved) {
		t.Fatal("failed save changed existing file", err)
	}
	destination := t.TempDir()
	if info, err := content.SaveCompiledAssetHeader(destination, input, codec); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("filesystem failure returned success")
	}
	if stat, err := os.Stat(destination); err != nil || !stat.IsDir() {
		t.Fatal("failed save changed destination directory")
	}
	limited := c1aContentCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxMetadataBytes: 1}})
	if out, info, err := content.LoadCompiledAssetHeader(path, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("load ignored bounded profile")
	}
	closed := c1aContentCodec(t, voxelcodec.Options{})
	closed.Close()
	c3bReject(t, input, closed)
	if out, info, err := content.DecodeCompiledAssetHeader(before, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed decode accepted")
	}
	if out, info, err := content.LoadCompiledAssetHeader(path, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed load accepted")
	}
	if info, err := content.SaveCompiledAssetHeader(path, input, closed); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed save accepted")
	}
	for _, bad := range [][]byte{append(bytes.Clone(before), 0), before[:len(before)-1], []byte(`{"schema_version":1}`)} {
		if out, info, err := content.DecodeCompiledAssetHeader(bad, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("invalid frame decode accepted or partial")
		}
		if err := os.WriteFile(path, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if out, info, err := content.LoadCompiledAssetHeader(path, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("invalid frame load accepted or partial")
		}
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("file adapter closed borrowed codec", err)
	}
}

func TestC3bReferenceLowerBoundsAndIndependentPaths(t *testing.T) {
	input := c3bHeader()
	input.Shapes[0].EncodedBytes = 96
	input.Shapes[0].DecodedBytes = 1
	input.Shapes[1].Path = "shapes/other.gkvox"
	input.Shapes[1].ContentID = strings.Repeat("c", 64)
	input.Shapes[1].BaseIdentity = strings.Repeat("d", 64)
	frame, _, err := content.EncodeCompiledAssetHeader(input, nil)
	if err != nil {
		t.Fatal("valid reference lower bounds or independent paths rejected", err)
	}
	output, _, err := content.DecodeCompiledAssetHeader(frame, nil)
	input.Shapes[0], input.Shapes[1] = input.Shapes[1], input.Shapes[0]
	if err != nil || !reflect.DeepEqual(output, input) {
		t.Fatal("valid independent references changed", err)
	}
}

func TestC3bExactPartLimitAccepted(t *testing.T) {
	input := c3bHeader()
	input.Asset.Parts = make([]content.AssetPartDef, content.MaxCompiledAssetParts)
	input.Asset.Lights = nil
	input.Asset.Emitters = nil
	input.Asset.Markers = nil
	input.Shapes = nil
	for i := range input.Asset.Parts {
		input.Asset.Parts[i] = content.AssetPartDef{ID: fmt.Sprint("group-", i), Name: "Group", Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}}
	}
	frame, _, err := content.EncodeCompiledAssetHeader(input, nil)
	if err != nil {
		t.Fatal("exact part limit rejected", err)
	}
	output, _, err := content.DecodeCompiledAssetHeader(frame, nil)
	if err != nil || !reflect.DeepEqual(output, input) {
		t.Fatal("exact part limit metadata changed", err)
	}
}

func TestC3bSkeletonIdentityRejectsLossyUTF8(t *testing.T) {
	for _, kind := range []string{"bone-id", "joint-id", "distinct-joint-collision"} {
		t.Run(kind, func(t *testing.T) {
			input := c3bHeader()
			switch kind {
			case "bone-id":
				input.Asset.Skeleton.Bones[1].ID = string([]byte{0xff})
			case "joint-id":
				input.Asset.Skeleton.Bones[1].JointID = string([]byte{0xff})
			case "distinct-joint-collision":
				input.Asset.Skeleton.Bones[0].JointID = string([]byte{0xff})
				input.Asset.Skeleton.Bones[1].JointID = string([]byte{0xfe})
			}
			c3bReject(t, input, nil)
		})
	}
}
