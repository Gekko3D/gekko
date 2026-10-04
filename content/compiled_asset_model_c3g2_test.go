package content_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3g2Model() *content.CompiledAssetModelDef {
	s := c3aShape()
	return &content.CompiledAssetModelDef{SchemaVersion: content.CurrentCompiledAssetModelSchemaVersion, Lattice: s.Lattice, Dimensions: [3]uint32{2, 3, 4}, Bricks: s.Bricks}
}

func c3g2Metadata(t *testing.T, m *content.CompiledAssetModelDef) []byte {
	t.Helper()
	b, err := json.Marshal(struct {
		SchemaVersion int                           `json:"schema_version"`
		Lattice       content.VoxelObjectLatticeDef `json:"lattice"`
		Dimensions    [3]uint32                     `json:"dimensions"`
	}{m.SchemaVersion, m.Lattice, m.Dimensions})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestC3g2CanonicalModelOwnershipAndProjection(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	input := c3g2Model()
	before, _ := json.Marshal(input)
	data, info, err := content.EncodeCompiledAssetModel(input, codec)
	if err != nil || info.ContentID == "" || info.EncodedBytes != int64(len(data)) {
		t.Fatalf("encode: %+v %v", info, err)
	}
	doc, _, err := codec.Decode(data)
	if err != nil || doc.Kind != "compiled_asset_model" || doc.NormalBakeVersion != "" || !bytes.Equal(doc.Metadata, c3g2Metadata(t, input)) {
		t.Fatalf("model document: %+v %v", doc, err)
	}
	canonical := *input
	canonical.Bricks = []voxelcodec.Brick{input.Bricks[1], input.Bricks[0]}
	again, otherInfo, err := content.EncodeCompiledAssetModel(&canonical, codec)
	if err != nil || !bytes.Equal(data, again) || otherInfo != info {
		t.Fatal("brick order changed canonical model", err)
	}
	decoded, decodedInfo, err := content.DecodeCompiledAssetModel(data, codec)
	if err != nil || decodedInfo != info || !reflect.DeepEqual(decoded, &canonical) {
		t.Fatalf("decode: %+v %v", decoded, err)
	}
	other, _, err := content.DecodeCompiledAssetModel(data, codec)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("encode mutated caller")
	}
	decoded.Bricks[0].Values[0] = 99
	decoded.Bricks[0].Occupancy[0] = 0
	if !reflect.DeepEqual(other, &canonical) {
		t.Fatal("decodes alias each other")
	}
	input.Bricks[1].Values[0] = 88
	if other.Bricks[0].Values[0] != 2 {
		t.Fatal("decoded values alias input")
	}
	input.Bricks[1].Values[0] = 2
	shape := c3aShape()
	wantID, wantSize, err := content.CompiledAssetShapeBaseIdentity(shape, nil)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, dims := range [][3]uint32{{}, {0, 3, 0}, {2, 3, 4}, {math.MaxUint32, math.MaxUint32, math.MaxUint32}} {
		m := c3g2Model()
		m.Dimensions = dims
		frame, frameInfo, err := content.EncodeCompiledAssetModel(m, nil)
		if err != nil {
			t.Fatal("declared dimensions incorrectly constrain occupancy", err)
		}
		out, _, err := content.DecodeCompiledAssetModel(frame, nil)
		if err != nil || out.Dimensions != dims || !reflect.DeepEqual(out.Bricks, canonical.Bricks) {
			t.Fatal("dimensions or primary changed", err)
		}
		if ids[frameInfo.ContentID] {
			t.Fatal("dimensions omitted from model identity")
		}
		ids[frameInfo.ContentID] = true
		id, size, err := content.CompiledAssetModelBaseIdentity(m, nil)
		if err != nil || id != wantID || size != wantSize {
			t.Fatalf("dimensions changed base projection: %q/%d %v", id, size, err)
		}
	}
	empty := c3g2Model()
	empty.Bricks = nil
	if _, _, err := content.EncodeCompiledAssetModel(empty, nil); err != nil {
		t.Fatal("empty declared model rejected", err)
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "borrowed-probe"}); err != nil {
		t.Fatal("borrowed codec closed", err)
	}
}

func c3g2Reject(t *testing.T, m *content.CompiledAssetModelDef, c *voxelcodec.Codec) {
	t.Helper()
	if data, info, err := content.EncodeCompiledAssetModel(m, c); err == nil || data != nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid model encode returned partial success")
	}
	if id, size, err := content.CompiledAssetModelBaseIdentity(m, c); err == nil || id != "" || size != 0 {
		t.Fatal("invalid model identity returned partial success")
	}
}

func TestC3g2ModelValidationAndTypedFrames(t *testing.T) {
	for name, change := range map[string]func(*content.CompiledAssetModelDef){
		"schema":     func(m *content.CompiledAssetModelDef) { m.SchemaVersion = 2 },
		"lattice":    func(m *content.CompiledAssetModelDef) { m.Lattice.VoxelResolution = 0 },
		"portable":   func(m *content.CompiledAssetModelDef) { m.Bricks[0].Coord[2] = 268435456 },
		"materials":  func(m *content.CompiledAssetModelDef) { m.Bricks[0].Materials = []uint8{} },
		"aux":        func(m *content.CompiledAssetModelDef) { m.Bricks[0].Aux = []byte{} },
		"duplicate":  func(m *content.CompiledAssetModelDef) { m.Bricks[0].Coord = m.Bricks[1].Coord },
		"zero-value": func(m *content.CompiledAssetModelDef) { m.Bricks[0].Values[0] = 0 },
	} {
		t.Run(name, func(t *testing.T) { m := c3g2Model(); change(m); c3g2Reject(t, m, nil) })
	}
	c3g2Reject(t, nil, nil)
	codec := c1aContentCodec(t, voxelcodec.Options{})
	metadata := c3g2Metadata(t, c3g2Model())
	for name, change := range map[string]func(*voxelcodec.Document){
		"shape-kind": func(d *voxelcodec.Document) { d.Kind = "compiled_asset_shape" },
		"bake":       func(d *voxelcodec.Document) { d.NormalBakeVersion = "bake-v1" },
		"layer":      func(d *voxelcodec.Document) { d.Bricks[0].Materials = []uint8{4} },
		"portable":   func(d *voxelcodec.Document) { d.Bricks[0].Coord[0] = -268435457 },
		"schema": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"schema_version":1`), []byte(`"schema_version":0`), 1)
		},
		"unknown":    func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"extra":1,`), metadata[1:]...) },
		"whitespace": func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), metadata...) },
		"missing-dimensions": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`,"dimensions":[2,3,4]`), nil, 1)
		},
		"short-dimensions": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`[2,3,4]`), []byte(`[2,3]`), 1)
		},
		"long-dimensions": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`[2,3,4]`), []byte(`[2,3,4,5]`), 1)
		},
		"negative-dimensions": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`[2,3,4]`), []byte(`[-1,3,4]`), 1)
		},
		"overflow-dimensions": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`[2,3,4]`), []byte(`[4294967296,3,4]`), 1)
		},
		"null-dimensions": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`[2,3,4]`), []byte(`null`), 1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := voxelcodec.Document{Kind: "compiled_asset_model", Metadata: bytes.Clone(metadata), Bricks: c3g2Model().Bricks}
			change(&d)
			frame, _, err := codec.Encode(d)
			if err != nil {
				t.Fatal("generic fixture invalid", err)
			}
			if out, info, err := content.DecodeCompiledAssetModel(frame, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("typed malformed frame accepted or partial")
			}
		})
	}
}

func TestC3g2ModelLimitsAndAtomicIO(t *testing.T) {
	m := c3g2Model()
	path := filepath.Join(t.TempDir(), "nested", "model.gkvox")
	saved, err := content.SaveCompiledAssetModel(path, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, info, err := content.LoadCompiledAssetModel(path, nil)
	if err != nil || info != saved || out.Dimensions != m.Dimensions {
		t.Fatal("file roundtrip", err)
	}
	invalid := c3g2Model()
	invalid.SchemaVersion = 0
	if info, err := content.SaveCompiledAssetModel(path, invalid, nil); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid save succeeded")
	}
	preserved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, preserved) {
		t.Fatal("failed save replaced old file")
	}
	for name, limits := range map[string]voxelcodec.Limits{"bricks": {MaxBricks: 1}, "voxels": {MaxVoxels: 2}, "metadata": {MaxMetadataBytes: 1}, "decoded": {MaxDecodedBytes: 32}, "encoded": {MaxEncodedBytes: int64(len(old) - 1)}, "bounds": {Bounds: &voxelcodec.Bounds{Min: [3]int32{}, Max: [3]int32{2, 2, 2}}}} {
		t.Run(name, func(t *testing.T) {
			c := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
			if out, info, err := content.DecodeCompiledAssetModel(old, c); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("decode ignored limit")
			}
			if out, info, err := content.LoadCompiledAssetModel(path, c); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("load ignored limit")
			}
			if name != "encoded" {
				c3g2Reject(t, m, c)
			}
		})
	}
	baseID, baseSize, err := content.CompiledAssetModelBaseIdentity(m, nil)
	if err != nil {
		t.Fatal(err)
	}
	tiny := c1aContentCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxEncodedBytes: 1}})
	if id, size, err := content.CompiledAssetModelBaseIdentity(m, tiny); err != nil || id != baseID || size != baseSize {
		t.Fatal("base identity used encoded frame limit", err)
	}
	for _, bad := range [][]byte{append(bytes.Clone(old), 0), old[:len(old)-1], []byte(`{"schema_version":1}`)} {
		if err := os.WriteFile(path, bad, 0600); err != nil {
			t.Fatal(err)
		}
		if out, info, err := content.LoadCompiledAssetModel(path, nil); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("load accepted trailing/truncated/non-C1")
		}
	}
}
