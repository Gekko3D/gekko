package content_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3h2LOD(t *testing.T) *content.CompiledAssetLODDef {
	t.Helper()
	source := &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: e2aLattice(), Bricks: []voxelcodec.Brick{
		{Coord: [3]int32{-1, 0, 0}, Occupancy: [8]uint64{1<<7 | 1<<15}, Values: []uint8{4, 4}},
		{Coord: [3]int32{}, Occupancy: [8]uint64{3}, Values: []uint8{4, 4}},
	}}
	_, sourceInfo, err := content.EncodeCompiledAssetShape(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &content.CompiledAssetLODDef{SchemaVersion: content.CurrentCompiledAssetLODSchemaVersion, SourceContentID: sourceInfo.ContentID, SourceLattice: source.Lattice, Factor: 2, ReductionVersion: content.CompiledAssetLOD2xReductionVersion, Value: 4, SourceVoxelCount: 4, SourceMin: [3]int64{-1, 0, 0}, SourceMax: [3]int64{2, 2, 1}, CoarseMin: [3]int64{-1, 0, 0}, CoarseMax: [3]int64{1, 1, 1}, Bricks: []voxelcodec.Brick{
		{Coord: [3]int32{}, Occupancy: [8]uint64{1}, Values: []uint8{4}},
		{Coord: [3]int32{-1, 0, 0}, Occupancy: [8]uint64{1 << 7}, Values: []uint8{4}},
	}}
}

func c3h2Metadata(t *testing.T, d *content.CompiledAssetLODDef) []byte {
	t.Helper()
	b, err := json.Marshal(struct {
		SchemaVersion    int                           `json:"schema_version"`
		SourceContentID  string                        `json:"source_content_id"`
		SourceLattice    content.VoxelObjectLatticeDef `json:"source_lattice"`
		Factor           int                           `json:"factor"`
		ReductionVersion string                        `json:"reduction_version"`
		Value            uint8                         `json:"value"`
		SourceVoxelCount int64                         `json:"source_voxel_count"`
		SourceMin        [3]int64                      `json:"source_min"`
		SourceMax        [3]int64                      `json:"source_max"`
		CoarseMin        [3]int64                      `json:"coarse_min"`
		CoarseMax        [3]int64                      `json:"coarse_max"`
	}{d.SchemaVersion, d.SourceContentID, d.SourceLattice, d.Factor, d.ReductionVersion, d.Value, d.SourceVoxelCount, d.SourceMin, d.SourceMax, d.CoarseMin, d.CoarseMax})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func c3h2Reject(t *testing.T, d *content.CompiledAssetLODDef, c *voxelcodec.Codec) {
	t.Helper()
	if b, info, err := content.EncodeCompiledAssetLOD(d, c); err == nil || b != nil || info != (voxelcodec.Info{}) {
		t.Fatalf("invalid encode returned partial success: bytes=%d info=%+v err=%v", len(b), info, err)
	}
}

func TestC3h2CanonicalSourceBindingAndOwnership(t *testing.T) {
	plain := c1aContentCodec(t, voxelcodec.Options{})
	dict := c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 38, Bytes: bytes.Repeat([]byte("compiled_asset_lod source_content_id source_lattice reduction_version"), 8)}})
	input := c3h2LOD(t)
	before, _ := json.Marshal(input)
	canonical := *input
	canonical.Bricks = []voxelcodec.Brick{input.Bricks[1], input.Bricks[0]}
	frame, info, err := content.EncodeCompiledAssetLOD(input, plain)
	if err != nil || info.ContentID == "" || info.EncodedBytes != int64(len(frame)) {
		t.Fatalf("encode: info=%+v err=%v", info, err)
	}
	doc, _, err := plain.Decode(frame)
	if err != nil || doc.Kind != "compiled_asset_lod" || doc.NormalBakeVersion != "" || !bytes.Equal(doc.Metadata, c3h2Metadata(t, input)) {
		t.Fatalf("wrong typed document: %+v err=%v", doc, err)
	}
	reordered, ri, err := content.EncodeCompiledAssetLOD(&canonical, plain)
	if err != nil || !bytes.Equal(frame, reordered) || ri != info {
		t.Fatal("input order changed canonical frame", err)
	}
	for _, codec := range []*voxelcodec.Codec{nil, plain, dict} {
		encoded, ei, err := content.EncodeCompiledAssetLOD(input, codec)
		if err != nil || ei.ContentID != info.ContentID {
			t.Fatal("codec profile changed logical identity", err)
		}
		out, oi, err := content.DecodeCompiledAssetLOD(encoded, codec)
		if err != nil || oi != ei || !reflect.DeepEqual(out, &canonical) {
			t.Fatalf("roundtrip out=%+v info=%+v err=%v", out, oi, err)
		}
		if codec == dict && ei.DictionaryID != 38 {
			t.Fatal("dictionary unused")
		}
	}
	a, _, err := content.DecodeCompiledAssetLOD(frame, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := content.DecodeCompiledAssetLOD(frame, plain)
	if err != nil {
		t.Fatal(err)
	}
	a.Bricks[0].Values[0] = 99
	a.Bricks[0].Occupancy[0] = 0
	if !reflect.DeepEqual(b, &canonical) {
		t.Fatal("decoded geometry shared")
	}
	after, _ := json.Marshal(input)
	if !bytes.Equal(before, after) {
		t.Fatal("encode mutated input")
	}
	input.Bricks[1].Values[0] = 77
	if b.Bricks[0].Values[0] != 4 {
		t.Fatal("decode aliases caller input")
	}
	// Source identifiers are typed logical bindings, not projected E2 base identities.
	changed := c3h2LOD(t)
	changed.SourceContentID = strings.Repeat("a", 64)
	_, changedInfo, err := content.EncodeCompiledAssetLOD(changed, plain)
	if err != nil || changedInfo.ContentID == info.ContentID {
		t.Fatal("source binding absent from identity", err)
	}
	if _, _, err := plain.Encode(voxelcodec.Document{Kind: "borrowed-probe"}); err != nil {
		t.Fatal("borrowed codec closed", err)
	}
	if out, _, err := content.DecodeCompiledAssetShape(frame, plain); err == nil || out != nil {
		t.Fatal("shape reader accepted LOD")
	}
	if out, _, err := content.DecodeCompiledAssetHeader(frame, plain); err == nil || out != nil {
		t.Fatal("header reader accepted LOD")
	}
	if out, _, err := content.DecodeVoxelObjectPayload(frame, plain); err == nil || out != nil {
		t.Fatal("E2 payload reader accepted LOD")
	}
}

func TestC3h2RejectInvalidDefinitions(t *testing.T) {
	cases := map[string]func(*content.CompiledAssetLODDef){
		"schema-zero":              func(d *content.CompiledAssetLODDef) { d.SchemaVersion = 0 },
		"schema-future":            func(d *content.CompiledAssetLODDef) { d.SchemaVersion = 2 },
		"factor":                   func(d *content.CompiledAssetLODDef) { d.Factor = 3 },
		"reducer":                  func(d *content.CompiledAssetLODDef) { d.ReductionVersion = "nearest" },
		"hash-empty":               func(d *content.CompiledAssetLODDef) { d.SourceContentID = "" },
		"hash-uppercase":           func(d *content.CompiledAssetLODDef) { d.SourceContentID = strings.Repeat("A", 64) },
		"hash-nonhex":              func(d *content.CompiledAssetLODDef) { d.SourceContentID = strings.Repeat("g", 64) },
		"hash-short":               func(d *content.CompiledAssetLODDef) { d.SourceContentID = strings.Repeat("a", 63) },
		"resolution":               func(d *content.CompiledAssetLODDef) { d.SourceLattice.VoxelResolution = 0 },
		"nan-resolution":           func(d *content.CompiledAssetLODDef) { d.SourceLattice.VoxelResolution = float32(math.NaN()) },
		"rasterization":            func(d *content.CompiledAssetLODDef) { d.SourceLattice.RasterizationVersion = "" },
		"zero-value":               func(d *content.CompiledAssetLODDef) { d.Value = 0 },
		"mixed-value":              func(d *content.CompiledAssetLODDef) { d.Bricks[0].Values[0] = 5 },
		"zero-brick-value":         func(d *content.CompiledAssetLODDef) { d.Bricks[0].Values[0] = 0 },
		"cardinality":              func(d *content.CompiledAssetLODDef) { d.Bricks[0].Values = append(d.Bricks[0].Values, 4) },
		"duplicate":                func(d *content.CompiledAssetLODDef) { d.Bricks[0] = d.Bricks[1] },
		"empty-brick":              func(d *content.CompiledAssetLODDef) { d.Bricks[0].Occupancy = [8]uint64{}; d.Bricks[0].Values = nil },
		"empty-geometry":           func(d *content.CompiledAssetLODDef) { d.Bricks = nil },
		"materials":                func(d *content.CompiledAssetLODDef) { d.Bricks[0].Materials = []uint8{4} },
		"empty-materials":          func(d *content.CompiledAssetLODDef) { d.Bricks[0].Materials = []uint8{} },
		"aux":                      func(d *content.CompiledAssetLODDef) { d.Bricks[0].Aux = []byte{1} },
		"empty-aux":                func(d *content.CompiledAssetLODDef) { d.Bricks[0].Aux = []byte{} },
		"count-zero":               func(d *content.CompiledAssetLODDef) { d.SourceVoxelCount = 0 },
		"count-negative":           func(d *content.CompiledAssetLODDef) { d.SourceVoxelCount = -1 },
		"count-no-reduction":       func(d *content.CompiledAssetLODDef) { d.SourceVoxelCount = 2 },
		"count-over-eight":         func(d *content.CompiledAssetLODDef) { d.SourceVoxelCount = 17 },
		"count-over-source-volume": func(d *content.CompiledAssetLODDef) { d.SourceVoxelCount = 7 },
	}
	for axis := 0; axis < 3; axis++ {
		axis := axis
		cases["source-min-range-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.SourceMin[axis] = -2147483649 }
		cases["source-max-range-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.SourceMax[axis] = 2147483649 }
		cases["source-empty-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.SourceMax[axis] = d.SourceMin[axis] }
		cases["source-reversed-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.SourceMax[axis] = d.SourceMin[axis] - 1 }
		cases["coarse-min-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.CoarseMin[axis]-- }
		cases["coarse-max-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.CoarseMax[axis]++ }
		cases["source-coarse-mismatch-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.SourceMax[axis] += 2 }
		cases["brick-range-low-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.Bricks[0].Coord[axis] = -134217729 }
		cases["brick-range-high-"+string(rune('x'+axis))] = func(d *content.CompiledAssetLODDef) { d.Bricks[0].Coord[axis] = 134217728 }
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) { d := c3h2LOD(t); change(d); c3h2Reject(t, d, nil) })
	}
	c3h2Reject(t, nil, nil)
}

func TestC3h2RejectTypedAndCorruptFrames(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	valid := c3h2LOD(t)
	metadata := c3h2Metadata(t, valid)
	cases := map[string]func(*voxelcodec.Document){
		"kind":          func(d *voxelcodec.Document) { d.Kind = "compiled_asset_shape" },
		"normal-bake":   func(d *voxelcodec.Document) { d.NormalBakeVersion = "bake-v1" },
		"unknown-key":   func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"extra":1,`), metadata[1:]...) },
		"whitespace":    func(d *voxelcodec.Document) { d.Metadata = append([]byte(" "), metadata...) },
		"duplicate-key": func(d *voxelcodec.Document) { d.Metadata = append([]byte(`{"schema_version":1,`), metadata[1:]...) },
		"missing-key":   func(d *voxelcodec.Document) { d.Metadata = bytes.Replace(metadata, []byte(`"factor":2,`), nil, 1) },
		"factor": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"factor":2`), []byte(`"factor":3`), 1)
		},
		"version": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"schema_version":1`), []byte(`"schema_version":2`), 1)
		},
		"source-count": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"source_voxel_count":4`), []byte(`"source_voxel_count":2`), 1)
		},
		"hash": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(valid.SourceContentID), []byte(strings.Repeat("A", 64)), 1)
		},
		"lattice-resolution": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"voxel_resolution":0.25`), []byte(`"voxel_resolution":0`), 1)
		},
		"source-coarse-mapping": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"source_max":[2,2,1]`), []byte(`"source_max":[4,2,1]`), 1)
		},
		"coarse-geometry-bounds": func(d *voxelcodec.Document) {
			d.Metadata = bytes.Replace(metadata, []byte(`"coarse_max":[1,1,1]`), []byte(`"coarse_max":[2,1,1]`), 1)
			d.Metadata = bytes.Replace(d.Metadata, []byte(`"source_max":[2,2,1]`), []byte(`"source_max":[4,2,1]`), 1)
		},
		"mixed-value": func(d *voxelcodec.Document) { d.Bricks[0].Values[0] = 5 },
		"materials":   func(d *voxelcodec.Document) { d.Bricks[0].Materials = []uint8{4} },
		"aux":         func(d *voxelcodec.Document) { d.Bricks[0].Aux = []byte{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := c3h2LOD(t)
			doc := voxelcodec.Document{Kind: "compiled_asset_lod", Metadata: bytes.Clone(metadata), Bricks: s.Bricks}
			change(&doc)
			frame, _, err := codec.Encode(doc)
			if err != nil {
				t.Fatal("generic fixture rejected", err)
			}
			if out, info, err := content.DecodeCompiledAssetLOD(frame, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatalf("typed invalid accepted or partial: out=%+v info=%+v err=%v", out, info, err)
			}
		})
	}
	frame, _, err := content.EncodeCompiledAssetLOD(valid, codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(bytes.Clone(frame), 0), frame[:len(frame)-1], func() []byte { b := bytes.Clone(frame); b[len(b)-1] ^= 1; return b }(), []byte(`{"schema_version":1}`)} {
		if out, info, err := content.DecodeCompiledAssetLOD(bad, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("corrupt frame accepted or partial")
		}
	}
}

func TestC3h2ProfilesAndAtomicFiles(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	d := c3h2LOD(t)
	path := filepath.Join(t.TempDir(), "nested", "lod.gkvox")
	saved, err := content.SaveCompiledAssetLOD(path, d, codec)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out, info, err := content.LoadCompiledAssetLOD(path, codec)
	if err != nil || info != saved || len(out.Bricks) != 2 {
		t.Fatal("file roundtrip", err)
	}
	// Source count is metadata, not an allocation of four source voxels.
	actualLimit := c1aContentCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxVoxels: 2}})
	if _, _, err := content.EncodeCompiledAssetLOD(d, actualLimit); err != nil {
		t.Fatal("source count incorrectly consumes voxel profile limit", err)
	}
	if _, _, err := content.LoadCompiledAssetLOD(path, actualLimit); err != nil {
		t.Fatal("source count incorrectly consumes decode voxel limit", err)
	}
	for name, limits := range map[string]voxelcodec.Limits{"voxels": {MaxVoxels: 1}, "bricks": {MaxBricks: 1}, "metadata": {MaxMetadataBytes: 1}, "decoded": {MaxDecodedBytes: 32}, "encoded": {MaxEncodedBytes: int64(len(original) - 1)}, "bounds": {Bounds: &voxelcodec.Bounds{Min: [3]int32{}, Max: [3]int32{1, 1, 1}}}} {
		t.Run(name, func(t *testing.T) {
			limited := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
			c3h2Reject(t, d, limited)
			if out, info, err := content.DecodeCompiledAssetLOD(original, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("decode ignored profile")
			}
			if out, info, err := content.LoadCompiledAssetLOD(path, limited); err == nil || out != nil || info != (voxelcodec.Info{}) {
				t.Fatal("load ignored profile")
			}
		})
	}
	bad := c3h2LOD(t)
	bad.Value = 0
	if info, err := content.SaveCompiledAssetLOD(path, bad, codec); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("invalid save succeeded")
	}
	preserved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, preserved) {
		t.Fatal("failed save changed existing frame")
	}
	closed := c1aContentCodec(t, voxelcodec.Options{})
	closed.Close()
	c3h2Reject(t, d, closed)
	if out, info, err := content.DecodeCompiledAssetLOD(original, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed decode succeeded")
	}
	if out, info, err := content.LoadCompiledAssetLOD(path, closed); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed load succeeded")
	}
	if info, err := content.SaveCompiledAssetLOD(path, d, closed); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed save succeeded")
	}
	preserved, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(original, preserved) {
		t.Fatal("closed save changed file")
	}
	for _, badFrame := range [][]byte{append(bytes.Clone(original), 0), original[:len(original)-1]} {
		if err := os.WriteFile(path, badFrame, 0600); err != nil {
			t.Fatal(err)
		}
		if out, info, err := content.LoadCompiledAssetLOD(path, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("file accepted trailing/truncated frame")
		}
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("file helpers closed borrowed codec", err)
	}
	destination := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	if info, err := content.SaveCompiledAssetLOD(destination, d, nil); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("filesystem failure returned success")
	}
}

func TestC3h2SignedEndpointsAndOverflowSafeVolume(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		for _, high := range []bool{false, true} {
			d := c3h2LOD(t)
			d.Bricks = []voxelcodec.Brick{{Occupancy: [8]uint64{1}, Values: []uint8{4}}}
			d.SourceVoxelCount = 2
			d.SourceMin = [3]int64{}
			d.SourceMax = [3]int64{2, 1, 1}
			d.CoarseMin = [3]int64{}
			d.CoarseMax = [3]int64{1, 1, 1}
			if high {
				d.SourceMin[axis] = 2147483646
				d.SourceMax[axis] = 2147483648
				d.CoarseMin[axis] = 1073741823
				d.CoarseMax[axis] = 1073741824
				d.Bricks[0].Coord[axis] = 134217727
				// Local cell 7 on the endpoint axis; x fastest, then y, then z.
				local := 7
				if axis == 1 {
					local = 56
				}
				if axis == 2 {
					local = 448
				}
				d.Bricks[0].Occupancy = [8]uint64{}
				d.Bricks[0].Occupancy[local/64] = 1 << uint(local%64)
			} else {
				d.SourceMin[axis] = -2147483648
				d.SourceMax[axis] = -2147483646
				d.CoarseMin[axis] = -1073741824
				d.CoarseMax[axis] = -1073741823
				d.Bricks[0].Coord[axis] = -134217728
			}
			frame, _, err := content.EncodeCompiledAssetLOD(d, nil)
			if err != nil {
				t.Fatalf("axis %d high %v endpoint rejected: %v", axis, high, err)
			}
			out, _, err := content.DecodeCompiledAssetLOD(frame, nil)
			if err != nil || !reflect.DeepEqual(out, d) {
				t.Fatal("signed endpoint changed", err)
			}
		}
	}
	// Bounding volume is 2^96. Valid metadata must not fail through int64 product overflow.
	d := c3h2LOD(t)
	d.SourceMin = [3]int64{-2147483648, -2147483648, -2147483648}
	d.SourceMax = [3]int64{2147483648, 2147483648, 2147483648}
	d.CoarseMin = [3]int64{-1073741824, -1073741824, -1073741824}
	d.CoarseMax = [3]int64{1073741824, 1073741824, 1073741824}
	d.SourceVoxelCount = 3
	d.Bricks = []voxelcodec.Brick{{Coord: [3]int32{-134217728, -134217728, -134217728}, Occupancy: [8]uint64{1}, Values: []uint8{4}}, {Coord: [3]int32{134217727, 134217727, 134217727}, Occupancy: [8]uint64{0, 0, 0, 0, 0, 0, 0, 1 << 63}, Values: []uint8{4}}}
	frame, _, err := content.EncodeCompiledAssetLOD(d, nil)
	if err != nil {
		t.Fatal("overflow-safe sparse bounds rejected", err)
	}
	if out, _, err := content.DecodeCompiledAssetLOD(frame, nil); err != nil || !reflect.DeepEqual(out, d) {
		t.Fatal("extreme sparse bounds changed", err)
	}
}

// Metadata cannot prove actual source coverage. It can reject occupancy counts
// exceeding the source cells available in the declared coarse footprints.
func TestC3h2ClippedSparseFootprintCapacity(t *testing.T) {
	d := c3h2LOD(t)
	d.SourceMin = [3]int64{}
	d.SourceMax = [3]int64{201, 1, 1}
	d.CoarseMin = [3]int64{}
	d.CoarseMax = [3]int64{101, 1, 1}
	d.Bricks = []voxelcodec.Brick{{Occupancy: [8]uint64{1}, Values: []uint8{4}}, {Coord: [3]int32{12, 0, 0}, Occupancy: [8]uint64{1 << 4}, Values: []uint8{4}}}
	d.SourceVoxelCount = 3
	frame, _, err := content.EncodeCompiledAssetLOD(d, nil)
	if err != nil {
		t.Fatal("valid clipped sparse footprint rejected", err)
	}
	out, _, err := content.DecodeCompiledAssetLOD(frame, nil)
	if err != nil || !reflect.DeepEqual(out, d) {
		t.Fatal("valid clipped sparse footprint changed", err)
	}
	d.SourceVoxelCount = 16
	c3h2Reject(t, d, nil)
	codec := c1aContentCodec(t, voxelcodec.Options{})
	doc := voxelcodec.Document{Kind: "compiled_asset_lod", Metadata: c3h2Metadata(t, d), Bricks: d.Bricks}
	bad, _, err := codec.Encode(doc)
	if err != nil {
		t.Fatal("generic fixture rejected", err)
	}
	if out, info, err := content.DecodeCompiledAssetLOD(bad, codec); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("typed decode accepted impossible clipped source count")
	}
}
