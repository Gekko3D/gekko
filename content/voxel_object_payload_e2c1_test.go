package content_test

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func e2c1Base() *content.VoxelObjectSnapshotDef {
	return &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: []content.VoxelObjectVoxelDef{{X: -16, Value: 2}, {X: -9, Value: 3}, {X: -8, Value: 5}, {X: -1, Value: 6}, {Value: 1}, {X: 8, Value: 4}}}
}

func e2c1Payload(t *testing.T) *content.VoxelObjectPayloadDef {
	t.Helper()
	identity, _, err := content.VoxelObjectBaseIdentity(e2c1Base(), e2aLattice(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &content.VoxelObjectPayloadDef{SchemaVersion: content.HybridVoxelObjectPayloadSchemaVersion, Mode: content.VoxelObjectPayloadHybridDelta, PlacementID: "placement", ItemID: "item", Lattice: e2aLattice(), BaseIdentity: identity, ReplacementBricks: [][3]int32{{1, 0, 0}, {-2, 0, 0}}, Voxels: []content.VoxelObjectVoxelDef{{X: -16, Value: 7}, {X: -16, Y: 1, Z: 1, Value: 8}, {X: -8, Value: 0}, {Value: 9}, {X: 1, Value: 2}}}
}

func e2c1Clone(payload *content.VoxelObjectPayloadDef) *content.VoxelObjectPayloadDef {
	clone := *payload
	clone.ReplacementBricks = append([][3]int32(nil), payload.ReplacementBricks...)
	clone.Voxels = append([]content.VoxelObjectVoxelDef(nil), payload.Voxels...)
	return &clone
}

func TestE2c1MixedHybridRoundTripResolutionAndOwnership(t *testing.T) {
	dictionary := c1aContentCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 21, Bytes: []byte("voxel_object_override hybrid_delta replacement_bricks geometry lattice materials")}})
	for _, codec := range []*voxelcodec.Codec{nil, dictionary} {
		payload := e2c1Payload(t)
		base := e2c1Base()
		before, _ := json.Marshal(payload)
		frame, info, err := content.EncodeVoxelObjectPayload(payload, codec)
		if err != nil {
			t.Fatal(err)
		}
		decoded, decodedInfo, err := content.DecodeVoxelObjectPayload(frame, codec)
		if err != nil || decodedInfo.ContentID != info.ContentID {
			t.Fatal("hybrid decode", err)
		}
		selectors := [][3]int32{{-2, 0, 0}, {1, 0, 0}}
		canonical := e2c1Clone(payload)
		canonical.ReplacementBricks = append([][3]int32(nil), selectors...)
		if _, other, err := content.EncodeVoxelObjectPayload(canonical, codec); err != nil || other.ContentID != info.ContentID {
			t.Fatal("caller selector ordering changed canonical content identity", err)
		}
		if decoded.SchemaVersion != 3 || decoded.Mode != content.VoxelObjectPayloadHybridDelta || !reflect.DeepEqual(decoded.ReplacementBricks, selectors) || !reflect.DeepEqual(e2aValues(decoded.Voxels), e2aValues(payload.Voxels)) {
			t.Fatalf("decoded hybrid=%+v", decoded)
		}
		actualCodec := codec
		if actualCodec == nil {
			actualCodec = c1aContentCodec(t, voxelcodec.Options{})
		}
		doc, _, err := actualCodec.Decode(frame)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Kind != "voxel_object_override" || doc.NormalBakeVersion != "" || len(doc.Bricks) != 3 {
			t.Fatal("hybrid changed C1 kind or materialized empty replacement brick")
		}
		for _, brick := range doc.Bricks {
			if brick.Aux != nil {
				t.Fatal("hybrid emitted auxiliary layer")
			}
			if brick.Coord == ([3]int32{-2, 0, 0}) {
				if brick.Materials != nil || !bytes.Equal(brick.Values, []byte{7, 8}) || brick.Occupancy[0] != 1 || brick.Occupancy[1] != (1<<8) {
					t.Fatalf("replacement primary/rank layers=%+v", brick)
				}
			} else {
				if brick.Materials == nil {
					t.Fatal("assignment brick lacks final materials")
				}
				for _, value := range brick.Values {
					if value != 1 {
						t.Fatal("assignment marker is not one")
					}
				}
			}
		}
		resolved, err := content.ResolveVoxelObjectPayload(decoded, base, e2aLattice(), "placement", "item", codec)
		if err != nil {
			t.Fatal(err)
		}
		want := map[[3]int]uint8{{-16, 0, 0}: 7, {-16, 1, 1}: 8, {-1, 0, 0}: 6, {0, 0, 0}: 9, {1, 0, 0}: 2}
		if !reflect.DeepEqual(e2aValues(resolved.Voxels), want) {
			t.Fatalf("mixed resolve=%v want=%v", resolved.Voxels, want)
		}
		path := filepath.Join(t.TempDir(), "hybrid.gkvox")
		saved, err := content.SaveVoxelObjectPayload(path, payload, codec)
		if err != nil || saved.ContentID != info.ContentID {
			t.Fatal("hybrid save", err)
		}
		loaded, _, err := content.LoadVoxelObjectPayload(path, codec)
		if err != nil {
			t.Fatal(err)
		}
		resolved.Voxels[0].Value = 99
		if !reflect.DeepEqual(e2aValues(decoded.Voxels), e2aValues(payload.Voxels)) {
			t.Fatal("resolved geometry aliases decoded payload")
		}
		decoded.Voxels[0].Value = 98
		decoded.ReplacementBricks[0][0] = 99
		if !reflect.DeepEqual(loaded.ReplacementBricks, selectors) || !reflect.DeepEqual(e2aValues(loaded.Voxels), e2aValues(payload.Voxels)) {
			t.Fatal("independent load aliases prior decoded output")
		}
		after, _ := json.Marshal(payload)
		if !bytes.Equal(before, after) || !reflect.DeepEqual(base, e2c1Base()) {
			t.Fatal("hybrid encode/resolve mutated caller inputs")
		}
	}
	if _, _, err := dictionary.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("hybrid helpers closed borrowed codec", err)
	}
	empty := e2c1Payload(t)
	empty.ReplacementBricks = [][3]int32{{-2, 0, 0}, {-1, 0, 0}, {0, 0, 0}, {1, 0, 0}}
	empty.Voxels = nil
	frame, _, err := content.EncodeVoxelObjectPayload(empty, nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := content.DecodeVoxelObjectPayload(frame, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := content.ResolveVoxelObjectPayload(decoded, e2c1Base(), e2aLattice(), "placement", "item", nil)
	if err != nil || len(resolved.Voxels) != 0 {
		t.Fatal("empty replacement selectors failed to clear complete base", err)
	}
}

func TestE2c1HybridSchemaSelectorsAndBindingValidation(t *testing.T) {
	for _, invalid := range []string{"schema0", "schema1", "schema2", "v3_full", "v3_base_delta", "v2_selectors", "duplicate_selector", "low_selector", "high_selector", "selected_zero", "duplicate_voxel"} {
		p := e2c1Payload(t)
		switch invalid {
		case "schema0":
			p.SchemaVersion = 0
		case "schema1":
			p.SchemaVersion = 1
		case "schema2":
			p.SchemaVersion = 2
		case "v3_full":
			p.Mode = content.VoxelObjectPayloadFull
			p.BaseIdentity = ""
		case "v3_base_delta":
			p.Mode = content.VoxelObjectPayloadBaseDelta
		case "v2_selectors":
			p.SchemaVersion = 2
			p.Mode = content.VoxelObjectPayloadBaseDelta
		case "duplicate_selector":
			p.ReplacementBricks = append(p.ReplacementBricks, p.ReplacementBricks[0])
		case "low_selector":
			p.ReplacementBricks = [][3]int32{{-268435457, 0, 0}}
		case "high_selector":
			p.ReplacementBricks = [][3]int32{{0, 0, 268435456}}
		case "selected_zero":
			p.Voxels[0].Value = 0
		case "duplicate_voxel":
			p.Voxels = append(p.Voxels, p.Voxels[0])
		}
		if data, _, err := content.EncodeVoxelObjectPayload(p, nil); err == nil || data != nil {
			t.Fatal("invalid hybrid encoded", invalid)
		}
	}
	// Schema zero remains the existing schema-two default, rather than opting in.
	full := e2aPayload(content.VoxelObjectPayloadFull, content.VoxelObjectVoxelDef{Value: 1})
	full.SchemaVersion = 0
	frame, _, err := content.EncodeVoxelObjectPayload(full, nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := content.DecodeVoxelObjectPayload(frame, nil)
	if content.CurrentVoxelObjectPayloadSchemaVersion != 2 {
		t.Fatal("hybrid changed the existing default schema constant")
	}
	if err != nil || decoded.SchemaVersion != content.CurrentVoxelObjectPayloadSchemaVersion || full.SchemaVersion != 0 {
		t.Fatal("hybrid changed schema-zero default", err)
	}
	legacy := &content.VoxelObjectPayloadDef{SchemaVersion: 1, Mode: content.VoxelObjectPayloadFull, ReplacementBricks: [][3]int32{{0, 0, 0}}}
	if result, err := content.ResolveVoxelObjectPayload(legacy, nil, content.VoxelObjectLatticeDef{}, "", "", nil); err == nil || result != nil {
		t.Fatal("legacy resolve ignored hybrid selectors")
	}
	for _, legacyJSON := range []string{`{"schema_version":1,"replacement_bricks":[[0,0,0]]}`, `{"schema_version":0,"mode":"hybrid_delta"}`} {
		if result, _, err := content.DecodeVoxelObjectPayload([]byte(legacyJSON), nil); err == nil || result != nil {
			t.Fatal("legacy JSON silently discarded hybrid semantics")
		}
	}
	p := e2c1Payload(t)
	p.ReplacementBricks = [][3]int32{{-2, 0, 0}, {1, 0, 0}}
	if err := content.ValidateVoxelObjectPayloadMetadata(p); err != nil {
		t.Fatal(err)
	}
	p.ReplacementBricks = [][3]int32{{1, 0, 0}, {-2, 0, 0}}
	if err := content.ValidateVoxelObjectPayloadMetadata(p); err == nil {
		t.Fatal("metadata preflight accepted noncanonical selector ordering")
	}
	p.ReplacementBricks = [][3]int32{{0, 1, 0}, {0, 0, 1}, {0, 0, 0}}
	orderFrame, _, err := content.EncodeVoxelObjectPayload(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	ordered, _, err := content.DecodeVoxelObjectPayload(orderFrame, nil)
	if err != nil || !reflect.DeepEqual(ordered.ReplacementBricks, [][3]int32{{0, 0, 0}, {0, 0, 1}, {0, 1, 0}}) {
		t.Fatal("replacement selectors must sort lexicographically X, Y, Z", err)
	}
	for _, mismatch := range []string{"owner", "item", "lattice", "base"} {
		p := e2c1Payload(t)
		base := e2c1Base()
		lattice := e2aLattice()
		placement, item := "placement", "item"
		switch mismatch {
		case "owner":
			placement = "other"
		case "item":
			item = "other"
		case "lattice":
			lattice.VoxelResolution = 1
		case "base":
			base.Voxels[0].Value = 9
		}
		if result, err := content.ResolveVoxelObjectPayload(p, base, lattice, placement, item, nil); err == nil || result != nil {
			t.Fatal("hybrid mismatch returned geometry", mismatch)
		}
	}
}

func TestE2c1HybridCompiledLayersAndCanonicalSelectorMetadata(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	frame, _, err := content.EncodeVoxelObjectPayload(e2c1Payload(t), codec)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := codec.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{"selected_secondary", "assignment_missing_secondary", "assignment_marker", "aux", "bake", "selector_order", "selector_duplicate", "selector_nonportable", "selector_arity", "unknown_metadata"} {
		bad := doc
		bad.Metadata = append([]byte(nil), doc.Metadata...)
		bad.Bricks = append([]voxelcodec.Brick(nil), doc.Bricks...)
		switch mutation {
		case "selected_secondary":
			bad.Bricks[0].Materials = []byte{7, 8}
		case "assignment_missing_secondary":
			bad.Bricks[1].Materials = nil
		case "assignment_marker":
			bad.Bricks[1].Values = []byte{2}
		case "aux":
			bad.Bricks[0].Aux = []byte{1}
		case "bake":
			bad.NormalBakeVersion = "unexpected"
		case "selector_order":
			bad.Metadata = bytes.Replace(bad.Metadata, []byte(`[[-2,0,0],[1,0,0]]`), []byte(`[[1,0,0],[-2,0,0]]`), 1)
		case "selector_duplicate":
			bad.Metadata = bytes.Replace(bad.Metadata, []byte(`[[-2,0,0],[1,0,0]]`), []byte(`[[-2,0,0],[-2,0,0],[1,0,0]]`), 1)
		case "selector_nonportable":
			bad.Metadata = bytes.Replace(bad.Metadata, []byte(`[[-2,0,0],[1,0,0]]`), []byte(`[[-2,0,0],[268435456,0,0]]`), 1)
		case "selector_arity":
			bad.Metadata = bytes.Replace(bad.Metadata, []byte(`[[-2,0,0],[1,0,0]]`), []byte(`[[-2,0],[1,0,0]]`), 1)
		case "unknown_metadata":
			bad.Metadata = append(append([]byte(nil), bad.Metadata[:len(bad.Metadata)-1]...), []byte(`,"unknown":true}`)...)
		}
		if strings.HasPrefix(mutation, "selector_") && bytes.Equal(bad.Metadata, doc.Metadata) {
			t.Fatal("selector mutation fixture did not change metadata")
		}
		encoded, _, err := codec.Encode(bad)
		if err != nil {
			t.Fatalf("generic C1 rejected opaque owner fixture %s: %v", mutation, err)
		}
		if result, _, err := content.DecodeVoxelObjectPayload(encoded, codec); err == nil || result != nil {
			t.Fatal("malicious hybrid document accepted", mutation)
		}
	}
}

func TestE2c1HybridLogicalProfilePortableEdgesAndMergedLimits(t *testing.T) {
	p := e2c1Payload(t)
	four := c1aContentCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxBricks: 4}})
	if _, _, err := content.EncodeVoxelObjectPayload(p, four); err != nil {
		t.Fatal("selectors overlapping physical bricks were double counted", err)
	}
	frame, _, err := content.EncodeVoxelObjectPayload(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "hybrid.gkvox")
	if _, err := content.SaveVoxelObjectPayload(path, p, nil); err != nil {
		t.Fatal(err)
	}
	for _, profile := range []voxelcodec.Limits{{MaxBricks: 3}, {Bounds: &voxelcodec.Bounds{Min: [3]int32{-2, 0, 0}, Max: [3]int32{0, 0, 0}}}, {MaxMetadataBytes: 64}} {
		codec := c1aContentCodec(t, voxelcodec.Options{Limits: profile})
		if data, _, err := content.EncodeVoxelObjectPayload(p, codec); err == nil || data != nil {
			t.Fatal("hybrid encode ignored logical selector profile", profile)
		}
		if result, _, err := content.DecodeVoxelObjectPayload(frame, codec); err == nil || result != nil {
			t.Fatal("hybrid decode ignored logical selector profile", profile)
		}
		if result, _, err := content.LoadVoxelObjectPayload(path, codec); err == nil || result != nil {
			t.Fatal("hybrid load ignored borrowed selector profile", profile)
		}
	}
	emptyBase := &content.VoxelObjectSnapshotDef{SchemaVersion: 1}
	identity, _, err := content.VoxelObjectBaseIdentity(emptyBase, e2aLattice(), nil)
	if err != nil {
		t.Fatal(err)
	}
	edge := e2c1Payload(t)
	edge.BaseIdentity = identity
	edge.Voxels = nil
	edge.ReplacementBricks = [][3]int32{{math.MinInt32 / 8, 0, 0}, {math.MaxInt32 / 8, 0, 0}}
	data, _, err := content.EncodeVoxelObjectPayload(edge, nil)
	if err != nil {
		t.Fatal("portable whole-cube edge rejected", err)
	}
	decoded, _, err := content.DecodeVoxelObjectPayload(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := content.ResolveVoxelObjectPayload(decoded, emptyBase, e2aLattice(), "placement", "item", nil); err != nil || len(result.Voxels) != 0 {
		t.Fatal("empty edge selectors failed resolve", err)
	}
	base := &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: []content.VoxelObjectVoxelDef{{Value: 1}}}
	identity, _, err = content.VoxelObjectBaseIdentity(base, e2aLattice(), nil)
	if err != nil {
		t.Fatal(err)
	}
	merged := e2c1Payload(t)
	merged.BaseIdentity = identity
	merged.ReplacementBricks = [][3]int32{{-1, 0, 0}}
	merged.Voxels = []content.VoxelObjectVoxelDef{{X: -8, Value: 7}, {X: 8, Value: 5}}
	for _, limits := range []voxelcodec.Limits{{MaxBricks: 2}, {MaxVoxels: 2}} {
		codec := c1aContentCodec(t, voxelcodec.Options{Limits: limits})
		if _, _, err := content.EncodeVoxelObjectPayload(merged, codec); err != nil {
			t.Fatal("merge fixture incoming hybrid should fit", err)
		}
		if result, err := content.ResolveVoxelObjectPayload(merged, base, e2aLattice(), "placement", "item", codec); err == nil || result != nil {
			t.Fatal("hybrid merged final geometry exceeded profile", limits)
		}
	}
}
