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

func c3aShape() *content.CompiledAssetShapeDef {
	return &content.CompiledAssetShapeDef{SchemaVersion: content.CurrentCompiledAssetShapeSchemaVersion, Lattice: e2aLattice(), Bricks: []voxelcodec.Brick{
		{Coord: [3]int32{1, 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{4}},
		{Coord: [3]int32{-1, 0, 0}, Occupancy: [8]uint64{3}, Values: []uint8{2, 3}},
	}}
}

func c3aMetadata(t *testing.T, s *content.CompiledAssetShapeDef) []byte {
	t.Helper()
	data, err := json.Marshal(struct {
		SchemaVersion int                           `json:"schema_version"`
		Lattice       content.VoxelObjectLatticeDef `json:"lattice"`
	}{s.SchemaVersion, s.Lattice})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestC3aRoundTripCanonicalIdentityAndOwnership(t *testing.T) {
	plain := c1aContentCodec(t, voxelcodec.Options{})
	dict := c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 37, Bytes: bytes.Repeat([]byte("compiled_asset_shape voxel_object_base lattice dense-v1 schema_version"), 8)}})
	input := c3aShape()
	before, _ := json.Marshal(input)
	data, info, err := content.EncodeCompiledAssetShape(input, plain)
	if err != nil || info.ContentID == "" || info.EncodedBytes != int64(len(data)) {
		t.Fatalf("encode: info=%+v err=%v", info, err)
	}
	doc, _, err := plain.Decode(data)
	if err != nil || doc.Kind != "compiled_asset_shape" || doc.NormalBakeVersion != "" || !bytes.Equal(doc.Metadata, c3aMetadata(t, input)) {
		t.Fatalf("document=%+v err=%v", doc, err)
	}
	canonical := *input
	canonical.Bricks = []voxelcodec.Brick{input.Bricks[1], input.Bricks[0]}
	again, againInfo, err := content.EncodeCompiledAssetShape(&canonical, plain)
	if err != nil || !bytes.Equal(data, again) || againInfo != info {
		t.Fatal("input ordering changed canonical frame", err)
	}
	decoded, decodedInfo, err := content.DecodeCompiledAssetShape(data, plain)
	if err != nil || decodedInfo != info || !reflect.DeepEqual(decoded, &canonical) {
		t.Fatalf("decode=%+v info=%+v err=%v", decoded, decodedInfo, err)
	}
	snapshot := &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: []content.VoxelObjectVoxelDef{{X: -8, Value: 2}, {X: -7, Value: 3}, {X: 8, Value: 4}}}
	wantID, wantSize, err := content.VoxelObjectBaseIdentity(snapshot, input.Lattice, plain)
	if err != nil {
		t.Fatal(err)
	}
	for _, codec := range []*voxelcodec.Codec{nil, plain, dict} {
		id, size, err := content.CompiledAssetShapeBaseIdentity(input, codec)
		if err != nil || id != wantID || size != wantSize {
			t.Fatalf("base projection id=%q size=%d want=%q/%d err=%v", id, size, wantID, wantSize, err)
		}
		frame, frameInfo, err := content.EncodeCompiledAssetShape(input, codec)
		if err != nil || frameInfo.ContentID != info.ContentID {
			t.Fatal("dictionary changed logical identity", err)
		}
		out, outInfo, err := content.DecodeCompiledAssetShape(frame, codec)
		if err != nil || outInfo != frameInfo || !reflect.DeepEqual(out, &canonical) {
			t.Fatal("codec roundtrip failed", err)
		}
		if codec == dict && frameInfo.DictionaryID != 37 {
			t.Fatal("dictionary profile unused")
		}
	}
	// A second decode must own its values, separate from the frame and first decode.
	other, _, err := content.DecodeCompiledAssetShape(data, plain)
	if err != nil {
		t.Fatal(err)
	}
	decoded.Bricks[0].Values[0] = 99
	decoded.Bricks[0].Occupancy[0] = 0
	if !reflect.DeepEqual(other, &canonical) {
		t.Fatal("decoded brick storage shared")
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("adapter mutated caller geometry")
	}
	input.Bricks[1].Values[0] = 88
	if other.Bricks[0].Values[0] != 2 {
		t.Fatal("decoded values alias input")
	}
	if _, _, err := plain.Encode(voxelcodec.Document{Kind: "borrowed-probe"}); err != nil {
		t.Fatal("borrowed codec closed", err)
	}
}

func TestC3aEmptyAndPortableSparseShapes(t *testing.T) {
	for _, bricks := range [][]voxelcodec.Brick{nil, {}, {{Coord: [3]int32{-268435456, 268435455, 0}, Occupancy: [8]uint64{1}, Values: []uint8{7}}}} {
		input := &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: e2aLattice(), Bricks: bricks}
		data, _, err := content.EncodeCompiledAssetShape(input, nil)
		if err != nil {
			t.Fatal(err)
		}
		output, _, err := content.DecodeCompiledAssetShape(data, nil)
		if err != nil || len(output.Bricks) != len(bricks) {
			t.Fatal("empty/extreme sparse roundtrip failed", err)
		}
		if len(bricks) > 0 && !reflect.DeepEqual(output.Bricks, bricks) {
			t.Fatal("sparse extreme coordinates changed")
		}
		if _, size, err := content.CompiledAssetShapeBaseIdentity(input, nil); err != nil || size <= 0 || size > 1024 {
			t.Fatalf("sparse identity allocated extent-sized document: size=%d err=%v", size, err)
		}
	}
}

func TestC3aRejectInvalidShapesWithoutPartialResults(t *testing.T) {
	cases := map[string]func(*content.CompiledAssetShapeDef){
		"schema-zero":         func(s *content.CompiledAssetShapeDef) { s.SchemaVersion = 0 },
		"schema-future":       func(s *content.CompiledAssetShapeDef) { s.SchemaVersion = 2 },
		"resolution-zero":     func(s *content.CompiledAssetShapeDef) { s.Lattice.VoxelResolution = 0 },
		"resolution-nan":      func(s *content.CompiledAssetShapeDef) { s.Lattice.VoxelResolution = float32(math.NaN()) },
		"rasterization-empty": func(s *content.CompiledAssetShapeDef) { s.Lattice.RasterizationVersion = "" },
		"zero-value":          func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Values[0] = 0 },
		"value-count":         func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Values = append(s.Bricks[0].Values, 2) },
		"empty-brick":         func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Occupancy = [8]uint64{}; s.Bricks[0].Values = nil },
		"duplicate":           func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Coord = s.Bricks[1].Coord },
		"materials":           func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Materials = []uint8{4} },
		"empty-materials":     func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Materials = []uint8{} },
		"aux":                 func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Aux = []byte{4} },
		"empty-aux":           func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Aux = []byte{} },
	}
	for axis := 0; axis < 3; axis++ {
		for _, coord := range []int32{-268435457, 268435456} {
			axis, coord := axis, coord
			cases[fmtC3a(axis, coord)] = func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Coord[axis] = coord }
		}
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) { s := c3aShape(); change(s); c3aReject(t, s, nil) })
	}
	c3aReject(t, nil, nil)
}

func fmtC3a(axis int, coord int32) string {
	data, _ := json.Marshal([2]int64{int64(axis), int64(coord)})
	return string(data)
}

func c3aReject(t *testing.T, s *content.CompiledAssetShapeDef, c *voxelcodec.Codec) {
	t.Helper()
	if data, info, err := content.EncodeCompiledAssetShape(s, c); err == nil || data != nil || info != (voxelcodec.Info{}) {
		t.Fatalf("invalid encode returned partial success: bytes=%d info=%+v err=%v", len(data), info, err)
	}
	if id, size, err := content.CompiledAssetShapeBaseIdentity(s, c); err == nil || id != "" || size != 0 {
		t.Fatalf("invalid identity returned partial success: %q/%d err=%v", id, size, err)
	}
}

func TestC3aRejectTypedFrameContracts(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	valid := c3aShape()
	metadata := c3aMetadata(t, valid)
	cases := map[string]func(*voxelcodec.Document){
		"wrong-kind":       func(d *voxelcodec.Document) { d.Kind = "voxel_object_base" },
		"bake":             func(d *voxelcodec.Document) { d.NormalBakeVersion = "bake-v1" },
		"materials":        func(d *voxelcodec.Document) { d.Bricks[0].Materials = []uint8{4} },
		"aux":              func(d *voxelcodec.Document) { d.Bricks[0].Aux = []byte{} },
		"coord-overflow":   func(d *voxelcodec.Document) { d.Bricks[0].Coord[2] = 268435456 },
		"unknown-metadata": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"extra":1,`), metadata[1:]...) },
		"whitespace":       func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), metadata...) },
		"key-order": func(d *voxelcodec.Document) {
			lattice, err := json.Marshal(valid.Lattice)
			if err != nil {
				t.Fatal(err)
			}
			d.Metadata = append(append([]byte(`{"lattice":`), lattice...), []byte(`,"schema_version":1}`)...)
		},
		"schema-zero": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"schema_version":1`), []byte(`"schema_version":0`), 1)
		},
		"duplicate-key": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"schema_version":1,`), metadata[1:]...) },
		"missing-schema": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"schema_version":1,`), nil, 1)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := c3aShape()
			d := voxelcodec.Document{Kind: "compiled_asset_shape", Metadata: bytes.Clone(metadata), Bricks: s.Bricks}
			change(&d)
			frame, _, err := codec.Encode(d)
			if err != nil {
				t.Fatal("invalid typed fixture rejected by generic codec", err)
			}
			out, info, err := content.DecodeCompiledAssetShape(frame, codec)
			if err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatalf("typed contract accepted or partial result: out=%+v info=%+v err=%v", out, info, err)
			}
		})
	}
	frame, _, err := content.EncodeCompiledAssetShape(valid, codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(bytes.Clone(frame), 0), frame[:len(frame)-1], func() []byte { b := bytes.Clone(frame); b[len(b)-1] ^= 1; return b }(), []byte(`{"schema_version":1}`)} {
		if out, info, err := content.DecodeCompiledAssetShape(bad, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("corrupt/non-C1 frame accepted or partial result")
		}
	}
}

func TestC3aBorrowedLimitsAndAtomicFiles(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	shape := c3aShape()
	path := filepath.Join(t.TempDir(), "nested", "shape.gkvox")
	saved, err := content.SaveCompiledAssetShape(path, shape, codec)
	if err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, info, err := content.LoadCompiledAssetShape(path, codec)
	if err != nil || info != saved || len(loaded.Bricks) != 2 {
		t.Fatal("file roundtrip failed", err)
	}
	bad := c3aShape()
	bad.Bricks[0].Aux = []byte{}
	if info, err := content.SaveCompiledAssetShape(path, bad, codec); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid save succeeded")
	}
	preserved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(old, preserved) {
		t.Fatal("failed save changed existing file", err)
	}
	for name, limits := range map[string]voxelcodec.Limits{
		"bricks": {MaxBricks: 1}, "voxels": {MaxVoxels: 2}, "metadata": {MaxMetadataBytes: 1}, "decoded": {MaxDecodedBytes: 32}, "encoded": {MaxEncodedBytes: int64(len(old) - 1)}, "bounds": {Bounds: &voxelcodec.Bounds{Min: [3]int32{}, Max: [3]int32{2, 2, 2}}},
	} {
		t.Run(name, func(t *testing.T) {
			limited := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
			if out, info, err := content.DecodeCompiledAssetShape(old, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("decode ignored borrowed limit")
			}
			if out, info, err := content.LoadCompiledAssetShape(path, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("load ignored borrowed limit")
			}
			if name != "encoded" {
				c3aReject(t, shape, limited)
			}
			if _, _, err := limited.Encode(voxelcodec.Document{Kind: "probe"}); err != nil && name == "bricks" {
				t.Fatal("adapter closed borrowed limited codec", err)
			}
		})
	}
	for _, badFrame := range [][]byte{append(bytes.Clone(old), 0), old[:len(old)-1]} {
		if err := os.WriteFile(path, badFrame, 0600); err != nil {
			t.Fatal(err)
		}
		if out, info, err := content.LoadCompiledAssetShape(path, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("file load accepted trailing/truncated frame")
		}
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("file helper closed borrowed codec", err)
	}
}

func TestC3aIdentityUsesBaseProjectionLimits(t *testing.T) {
	shape := c3aShape()
	wantID, wantSize, err := content.CompiledAssetShapeBaseIdentity(shape, nil)
	if err != nil {
		t.Fatal(err)
	}
	baseMetadata, err := json.Marshal(struct {
		Lattice content.VoxelObjectLatticeDef `json:"lattice"`
	}{shape.Lattice})
	if err != nil {
		t.Fatal(err)
	}
	for name, limits := range map[string]voxelcodec.Limits{"encoded": {MaxEncodedBytes: 1}, "base-metadata": {MaxMetadataBytes: len(baseMetadata)}} {
		t.Run(name, func(t *testing.T) {
			codec := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
			id, size, err := content.CompiledAssetShapeBaseIdentity(shape, codec)
			if err != nil || id != wantID || size != wantSize {
				t.Fatalf("identity incorrectly applies compiled-frame limits: id=%q size=%d err=%v", id, size, err)
			}
			if data, info, err := content.EncodeCompiledAssetShape(shape, codec); err == nil || data != nil || info != (voxelcodec.Info{}) {
				t.Fatal("encode bypassed compiled-frame limits")
			}
		})
	}
}

func TestC3aAllPortableAxisEndpoints(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		for _, coord := range []int32{-268435456, 268435455} {
			t.Run(fmtC3a(axis, coord), func(t *testing.T) {
				shape := c3aShape()
				shape.Bricks = shape.Bricks[:1]
				shape.Bricks[0].Coord = [3]int32{}
				shape.Bricks[0].Coord[axis] = coord
				frame, _, err := content.EncodeCompiledAssetShape(shape, nil)
				if err != nil {
					t.Fatal("valid full cube endpoint rejected", err)
				}
				out, _, err := content.DecodeCompiledAssetShape(frame, nil)
				if err != nil || !reflect.DeepEqual(out, shape) {
					t.Fatal("valid full cube endpoint changed", err)
				}
			})
		}
	}
}

func TestC3aClosedCodecAndFilesystemSaveFailure(t *testing.T) {
	shape := c3aShape()
	frame, _, err := content.EncodeCompiledAssetShape(shape, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "shape.gkvox")
	if err := os.WriteFile(path, frame, 0600); err != nil {
		t.Fatal(err)
	}
	closed := c1aContentCodec(t, voxelcodec.Options{})
	closed.Close()
	c3aReject(t, shape, closed)
	if out, info, err := content.DecodeCompiledAssetShape(frame, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed borrowed codec decode succeeded or partial")
	}
	if out, info, err := content.LoadCompiledAssetShape(path, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed borrowed codec load succeeded or partial")
	}
	if info, err := content.SaveCompiledAssetShape(path, shape, closed); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed borrowed codec save succeeded or partial")
	}
	preserved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(frame, preserved) {
		t.Fatal("closed codec save changed old file", err)
	}
	destination := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if info, err := content.SaveCompiledAssetShape(destination, shape, nil); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("filesystem save failure returned success or info")
	}
	stat, err := os.Stat(destination)
	if err != nil || !stat.IsDir() {
		t.Fatal("failed save replaced destination directory", err)
	}
}

func TestC3aBaseProjectionAcrossOccupancyWords(t *testing.T) {
	shape := &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: e2aLattice(), Bricks: []voxelcodec.Brick{{Coord: [3]int32{-1, -2, -3}, Occupancy: [8]uint64{uint64(1) << 63, 1, 0, 0, 0, 0, 0, uint64(1) << 63}, Values: []uint8{11, 12, 13}}}}
	snapshot := &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: []content.VoxelObjectVoxelDef{{X: -1, Y: -9, Z: -24, Value: 11}, {X: -8, Y: -16, Z: -23, Value: 12}, {X: -1, Y: -9, Z: -17, Value: 13}}}
	wantID, wantSize, err := content.VoxelObjectBaseIdentity(snapshot, shape.Lattice, nil)
	if err != nil {
		t.Fatal(err)
	}
	gotID, gotSize, err := content.CompiledAssetShapeBaseIdentity(shape, nil)
	if err != nil || gotID != wantID || gotSize != wantSize {
		t.Fatalf("occupancy word/axis projection differs from signed voxel records: got=%q/%d want=%q/%d err=%v", gotID, gotSize, wantID, wantSize, err)
	}
}
