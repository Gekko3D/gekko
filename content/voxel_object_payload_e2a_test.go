package content_test

import (
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func e2aLattice() content.VoxelObjectLatticeDef {
	return content.VoxelObjectLatticeDef{VoxelResolution: 0.25, RasterizationVersion: "dense-v1"}
}

func e2aBase() *content.VoxelObjectSnapshotDef {
	return &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: []content.VoxelObjectVoxelDef{{X: -9, Value: 2}, {X: -8, Value: 3}, {X: 8, Value: 4}}}
}

func e2aPayload(mode string, voxels ...content.VoxelObjectVoxelDef) *content.VoxelObjectPayloadDef {
	return &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: mode, PlacementID: "placement", ItemID: "item", Lattice: e2aLattice(), Voxels: voxels}
}

func e2aValues(voxels []content.VoxelObjectVoxelDef) map[[3]int]uint8 {
	result := make(map[[3]int]uint8)
	for _, v := range voxels {
		key := [3]int{v.X, v.Y, v.Z}
		if v.Value == 0 {
			delete(result, key)
		} else {
			result[key] = v.Value
		}
	}
	return result
}

func TestE2aFullDeltaRoundTripsResolveAndIndependentRecords(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	base := e2aBase()
	baseID, _, err := content.VoxelObjectBaseIdentity(base, e2aLattice(), codec)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta} {
		for _, empty := range []bool{false, true} {
			t.Run(mode+map[bool]string{true: "-empty", false: "-negative-seams"}[empty], func(t *testing.T) {
				payload := e2aPayload(mode)
				if !empty {
					payload.Voxels = []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {X: -8, Value: 6}, {X: 8, Value: 5}, {X: -8, Y: 1, Z: 1, Value: 9}}
				}
				if mode == content.VoxelObjectPayloadBaseDelta {
					payload.BaseIdentity = baseID
					if !empty {
						payload.Voxels[1].Value = 0
					}
				}
				before, _ := json.Marshal(payload)
				data, info, err := content.EncodeVoxelObjectPayload(payload, codec)
				if err != nil || info.ContentID == "" || info.EncodedBytes != int64(len(data)) {
					t.Fatalf("encode info=%+v err=%v", info, err)
				}
				decoded, decodedInfo, err := content.DecodeVoxelObjectPayload(data, codec)
				if err != nil || decodedInfo.ContentID != info.ContentID || decoded.Mode != mode || !reflect.DeepEqual(e2aValues(decoded.Voxels), e2aValues(payload.Voxels)) {
					t.Fatalf("decode=%+v info=%+v err=%v", decoded, decodedInfo, err)
				}
				resolved, err := content.ResolveVoxelObjectPayload(decoded, base, e2aLattice(), "placement", "item", codec)
				if err != nil {
					t.Fatal(err)
				}
				want := e2aValues(payload.Voxels)
				if mode == content.VoxelObjectPayloadBaseDelta {
					want = e2aValues(append(append([]content.VoxelObjectVoxelDef(nil), base.Voxels...), payload.Voxels...))
				}
				if !reflect.DeepEqual(e2aValues(resolved.Voxels), want) {
					t.Fatalf("resolved=%v want=%v", resolved.Voxels, want)
				}
				path := filepath.Join(t.TempDir(), "nested", "override.gkvox")
				if saved, err := content.SaveVoxelObjectPayload(path, payload, codec); err != nil || saved.ContentID != info.ContentID {
					t.Fatal("save failed", err)
				}
				loaded, loadedInfo, err := content.LoadVoxelObjectPayload(path, codec)
				if err != nil || loadedInfo.ContentID != info.ContentID || !reflect.DeepEqual(e2aValues(loaded.Voxels), e2aValues(payload.Voxels)) {
					t.Fatal("load failed", err)
				}
				if len(resolved.Voxels) > 0 {
					resolved.Voxels[0].Value = 99
				}
				if !reflect.DeepEqual(e2aValues(decoded.Voxels), e2aValues(payload.Voxels)) {
					t.Fatal("resolved geometry aliased decoded payload records")
				}
				if len(decoded.Voxels) > 0 {
					decoded.Voxels[0].Value = 98
				}
				if !reflect.DeepEqual(e2aValues(loaded.Voxels), e2aValues(payload.Voxels)) {
					t.Fatal("independent loaded records aliased decoded or resolved output")
				}
				after, _ := json.Marshal(payload)
				if string(before) != string(after) || !reflect.DeepEqual(base, e2aBase()) {
					t.Fatal("encode/decode/resolve mutated caller records")
				}
			})
		}
	}
	// Explicit codecs are borrowed across every operation.
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("content helper closed borrowed codec", err)
	}
}

func TestE2aCanonicalBaseIdentityAndBindingFailures(t *testing.T) {
	base, lattice := e2aBase(), e2aLattice()
	id, size, err := content.VoxelObjectBaseIdentity(base, lattice, nil)
	if err != nil || len(id) != 64 || size <= 0 {
		t.Fatalf("identity=%q size=%d err=%v", id, size, err)
	}
	reordered := &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: []content.VoxelObjectVoxelDef{{X: 8, Value: 4}, {X: -9, Value: 9}, {X: 40, Value: 8}, {X: -8, Value: 3}, {X: -9, Value: 2}, {X: 40, Value: 0}}}
	dict := c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 17, Bytes: []byte("voxel_object_base geometry dense-v1 lattice resolution")}})
	otherID, otherSize, err := content.VoxelObjectBaseIdentity(reordered, lattice, dict)
	if err != nil || otherID != id || otherSize != size {
		t.Fatal("base identity depends on order, removed history or dictionary")
	}
	for _, change := range []string{"geometry", "resolution", "rasterization"} {
		b, l := e2aBase(), e2aLattice()
		switch change {
		case "geometry":
			b.Voxels[0].Value = 7
		case "resolution":
			l.VoxelResolution = 0.5
		case "rasterization":
			l.RasterizationVersion = "dense-v2"
		}
		changed, _, err := content.VoxelObjectBaseIdentity(b, l, nil)
		if err != nil || changed == id {
			t.Fatal("identity failed to bind", change)
		}
	}
	payload := e2aPayload(content.VoxelObjectPayloadBaseDelta, content.VoxelObjectVoxelDef{X: -9, Value: 0})
	payload.BaseIdentity = id
	for _, mismatch := range []string{"base", "lattice", "placement", "item"} {
		b, l, p, i := e2aBase(), e2aLattice(), "placement", "item"
		switch mismatch {
		case "base":
			b.Voxels[0].Value = 8
		case "lattice":
			l.VoxelResolution = 1
		case "placement":
			p = "other"
		case "item":
			i = "other"
		}
		if result, err := content.ResolveVoxelObjectPayload(payload, b, l, p, i, nil); err == nil || result != nil {
			t.Fatal("mismatched binding returned geometry", mismatch)
		}
	}
	if result, err := content.ResolveVoxelObjectPayload(payload, nil, lattice, "placement", "item", nil); err == nil || result != nil {
		t.Fatal("base delta without base returned partial geometry")
	}
	unsupported := e2aBase()
	unsupported.SchemaVersion = 99
	if identity, _, err := content.VoxelObjectBaseIdentity(unsupported, lattice, nil); err == nil || identity != "" {
		t.Fatal("unsupported base schema acquired an identity")
	}
	if result, err := content.ResolveVoxelObjectPayload(payload, unsupported, lattice, "placement", "item", nil); err == nil || result != nil {
		t.Fatal("unsupported base schema returned partial geometry")
	}
	if identity, _, err := content.VoxelObjectBaseIdentity(nil, lattice, nil); err == nil || identity != "" {
		t.Fatal("nil base acquired an identity")
	}
}

func TestE2aLegacyDispatchAndSchemaDefaultDoesNotMutate(t *testing.T) {
	legacy := &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{{X: -1, Value: 2}, {X: -1, Value: 0}}}
	data, _ := json.Marshal(legacy)
	decoded, _, err := content.DecodeVoxelObjectPayload(data, nil)
	if err != nil || decoded.SchemaVersion != 1 || decoded.Mode != content.VoxelObjectPayloadFull {
		t.Fatal("legacy schema-zero JSON dispatch failed", err)
	}
	resolved, err := content.ResolveVoxelObjectPayload(decoded, nil, content.VoxelObjectLatticeDef{}, "unbound", "unbound", nil)
	if err != nil || !reflect.DeepEqual(resolved.Voxels, legacy.Voxels) {
		t.Fatal("legacy record acceptance changed", err)
	}
	resolved.Voxels[0].Value = 9
	if decoded.Voxels[0].Value != 2 {
		t.Fatal("legacy resolve reused input records")
	}
	path := filepath.Join(t.TempDir(), "legacy.gkvox")
	if err := content.SaveVoxelObjectSnapshot(path, legacy); err != nil {
		t.Fatal(err)
	}
	if loaded, err := content.LoadVoxelObjectSnapshot(path); err != nil || !reflect.DeepEqual(loaded.Voxels, legacy.Voxels) {
		t.Fatal("existing legacy API changed", err)
	}
	full := e2aPayload(content.VoxelObjectPayloadFull, content.VoxelObjectVoxelDef{Value: 3})
	full.SchemaVersion = 0
	encoded, _, err := content.EncodeVoxelObjectPayload(full, nil)
	if err != nil || full.SchemaVersion != 0 {
		t.Fatal("schema default mutated input", err)
	}
	decoded, _, err = content.DecodeVoxelObjectPayload(encoded, nil)
	if err != nil || decoded.SchemaVersion != content.CurrentVoxelObjectPayloadSchemaVersion {
		t.Fatal("new compiled schema default incorrect", err)
	}
}

func TestE2aPayloadValidationBoundsLimitsCorruptionAndCodecLifetime(t *testing.T) {
	valid := e2aPayload(content.VoxelObjectPayloadFull, content.VoxelObjectVoxelDef{Value: 1})
	for _, invalid := range []string{"schema", "duplicate", "full-zero", "full-base", "mode", "range", "resolution", "nan", "version", "invalid-utf8", "delta-id", "empty-placement", "empty-item", "invalid-placement", "invalid-item"} {
		p := *valid
		p.Voxels = append([]content.VoxelObjectVoxelDef(nil), valid.Voxels...)
		switch invalid {
		case "schema":
			p.SchemaVersion = 1
		case "duplicate":
			p.Voxels = append(p.Voxels, p.Voxels[0])
		case "full-zero":
			p.Voxels[0].Value = 0
		case "full-base":
			p.BaseIdentity = strings.Repeat("a", 64)
		case "mode":
			p.Mode = "unknown"
		case "range":
			p.Voxels[0].X = int(int64(math.MaxInt32) + 1)
		case "resolution":
			p.Lattice.VoxelResolution = 0
		case "nan":
			p.Lattice.VoxelResolution = float32(math.NaN())
		case "version":
			p.Lattice.RasterizationVersion = strings.Repeat("v", 129)
		case "invalid-utf8":
			p.Lattice.RasterizationVersion = string([]byte{255})
		case "delta-id":
			p.Mode = content.VoxelObjectPayloadBaseDelta
			p.BaseIdentity = strings.Repeat("A", 64)
		case "empty-placement":
			p.PlacementID = ""
		case "empty-item":
			p.ItemID = ""
		case "invalid-placement":
			p.PlacementID = string([]byte{255})
		case "invalid-item":
			p.ItemID = string([]byte{255})
		}
		if data, _, err := content.EncodeVoxelObjectPayload(&p, nil); err == nil || data != nil {
			t.Fatal("invalid payload encoded", invalid)
		}
	}
	limited := c1aContentCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxVoxels: 1, Bounds: &voxelcodec.Bounds{Min: [3]int32{}, Max: [3]int32{}}}})
	two := e2aPayload(content.VoxelObjectPayloadFull, content.VoxelObjectVoxelDef{Value: 1}, content.VoxelObjectVoxelDef{X: 1, Value: 2})
	if _, _, err := content.EncodeVoxelObjectPayload(two, limited); err == nil {
		t.Fatal("voxel limit ignored")
	}
	twoFrame, _, err := content.EncodeVoxelObjectPayload(two, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result, _, err := content.DecodeVoxelObjectPayload(twoFrame, limited); err == nil || result != nil {
		t.Fatal("decode ignored borrowed codec voxel limit")
	}
	twoPath := filepath.Join(t.TempDir(), "two.gkvox")
	if _, err := content.SaveVoxelObjectPayload(twoPath, two, nil); err != nil {
		t.Fatal(err)
	}
	if result, _, err := content.LoadVoxelObjectPayload(twoPath, limited); err == nil || result != nil {
		t.Fatal("load ignored borrowed codec voxel limit")
	}
	negative := e2aPayload(content.VoxelObjectPayloadFull, content.VoxelObjectVoxelDef{X: -1, Value: 1})
	if _, _, err := content.EncodeVoxelObjectPayload(negative, limited); err == nil {
		t.Fatal("brick bounds ignored")
	}
	encoded, _, err := content.EncodeVoxelObjectPayload(valid, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded[len(encoded)-1] ^= 255
	if p, _, err := content.DecodeVoxelObjectPayload(encoded, nil); err == nil || p != nil {
		t.Fatal("corrupt frame accepted")
	}
	closed := c1aContentCodec(t, voxelcodec.Options{})
	_ = closed.Close()
	if _, _, err := content.EncodeVoxelObjectPayload(valid, closed); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("closed borrowed codec ignored", err)
	}
}

func TestE2aCompiledLayerAndCanonicalMetadataValidation(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	for _, mode := range []string{content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta} {
		p := e2aPayload(mode, content.VoxelObjectVoxelDef{X: -1, Value: 3})
		if mode == content.VoxelObjectPayloadBaseDelta {
			p.BaseIdentity = strings.Repeat("a", 64)
			p.Voxels[0].Value = 0
		}
		data, _, err := content.EncodeVoxelObjectPayload(p, codec)
		if err != nil {
			t.Fatal(err)
		}
		doc, _, err := codec.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "voxel_object_override" || doc.NormalBakeVersion != "" || len(doc.Bricks) != 1 || doc.Bricks[0].Aux != nil {
			t.Fatal("unexpected compiled override layers")
		}
		if mode == content.VoxelObjectPayloadFull {
			if doc.Bricks[0].Materials != nil || doc.Bricks[0].Values[0] != 3 {
				t.Fatal("full payload used delta layers")
			}
		} else if doc.Bricks[0].Values[0] != 1 || len(doc.Bricks[0].Materials) != 1 || doc.Bricks[0].Materials[0] != 0 {
			t.Fatal("delta failed explicit-zero assignment mask")
		}
		for _, mutation := range []string{"kind", "bake", "aux", "layer", "metadata", "coordinate"} {
			bad := doc
			bad.Metadata = append([]byte(nil), doc.Metadata...)
			bad.Bricks = append([]voxelcodec.Brick(nil), doc.Bricks...)
			switch mutation {
			case "kind":
				bad.Kind = "voxel_object_base"
			case "bake":
				bad.NormalBakeVersion = "unexpected"
			case "aux":
				bad.Bricks[0].Aux = []byte{1}
			case "layer":
				if mode == content.VoxelObjectPayloadFull {
					bad.Bricks[0].Materials = []byte{3}
				} else {
					bad.Bricks[0].Materials = nil
				}
			case "metadata":
				bad.Metadata = append([]byte(" "), bad.Metadata...)
			case "coordinate":
				bad.Bricks[0].Coord[0] = math.MaxInt32
			}
			frame, _, err := codec.Encode(bad)
			if err != nil {
				t.Fatalf("fixture codec rejected %s: %v", mutation, err)
			}
			if result, _, err := content.DecodeVoxelObjectPayload(frame, codec); err == nil || result != nil {
				t.Fatal("malicious compiled document accepted", mode, mutation)
			}
		}
		for _, field := range []string{"schema_version", "mode", "placement_id", "item_id", "unknown_field"} {
			bad := doc
			var metadata map[string]json.RawMessage
			if err := json.Unmarshal(doc.Metadata, &metadata); err != nil {
				t.Fatal(err)
			}
			switch field {
			case "schema_version":
				metadata[field] = json.RawMessage("1")
			case "mode":
				metadata[field] = json.RawMessage(`"unknown"`)
			case "placement_id", "item_id":
				metadata[field] = json.RawMessage(`""`)
			case "unknown_field":
				metadata[field] = json.RawMessage("true")
			}
			bad.Metadata, err = json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			frame, _, err := codec.Encode(bad)
			if err != nil {
				t.Fatal("opaque metadata fixture rejected by codec", err)
			}
			if result, _, err := content.DecodeVoxelObjectPayload(frame, codec); err == nil || result != nil {
				t.Fatal("invalid compiled owner metadata accepted", mode, field)
			}
		}
		if mode == content.VoxelObjectPayloadBaseDelta {
			bad := doc
			bad.Bricks = append([]voxelcodec.Brick(nil), doc.Bricks...)
			bad.Bricks[0].Values = []byte{2}
			frame, _, err := codec.Encode(bad)
			if err != nil {
				t.Fatal(err)
			}
			if result, _, err := content.DecodeVoxelObjectPayload(frame, codec); err == nil || result != nil {
				t.Fatal("noncanonical delta marker accepted")
			}
		}
	}
}
