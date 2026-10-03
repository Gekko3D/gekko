package content_test

import (
	"encoding/json"
	"github.com/gekko3d/gekko/content"
	"reflect"
	"testing"
)

func TestE2c1LegacyIgnoresUnrelatedModeAndEmptySelectors(t *testing.T) {
	records := []content.VoxelObjectVoxelDef{{X: -1, Value: 2}, {X: -1}, {X: -1, Value: 3}}
	for _, schema := range []int{0, 1} {
		for _, extra := range []map[string]any{{"mode": 123}, {"mode": map[string]any{"unrelated": true}}, {"replacement_bricks": [][3]int32{}}, {"replacement_bricks": nil}} {
			document := map[string]any{"schema_version": schema, "voxels": records}
			for key, value := range extra {
				document[key] = value
			}
			encoded, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			decoded, _, err := content.DecodeVoxelObjectPayload(encoded, nil)
			if err != nil || decoded == nil {
				t.Fatalf("legacy schema%d unrelated fields %v rejected: %v", schema, extra, err)
			}
			if decoded.SchemaVersion != 1 || decoded.Mode != content.VoxelObjectPayloadFull || !reflect.DeepEqual(decoded.Voxels, records) {
				t.Fatal("legacy compatibility changed raw record ordering or assignments")
			}
		}
	}
	legacy := &content.VoxelObjectPayloadDef{SchemaVersion: 1, Mode: content.VoxelObjectPayloadFull, Voxels: append([]content.VoxelObjectVoxelDef(nil), records...), ReplacementBricks: [][3]int32{}}
	resolved, err := content.ResolveVoxelObjectPayload(legacy, nil, content.VoxelObjectLatticeDef{}, "", "", nil)
	if err != nil || resolved == nil || !reflect.DeepEqual(resolved.Voxels, records) {
		t.Fatal("empty legacy selector slice changed resolve acceptance", err)
	}
	resolved.Voxels[0].Value = 9
	if !reflect.DeepEqual(legacy.Voxels, records) {
		t.Fatal("legacy result aliases input records")
	}
	for _, mode := range []string{content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta} {
		payload := e2aPayload(mode, content.VoxelObjectVoxelDef{Value: 3})
		if mode == content.VoxelObjectPayloadBaseDelta {
			identity, _, err := content.VoxelObjectBaseIdentity(e2aBase(), e2aLattice(), nil)
			if err != nil {
				t.Fatal(err)
			}
			payload.BaseIdentity = identity
		}
		_, nilInfo, err := content.EncodeVoxelObjectPayload(payload, nil)
		if err != nil {
			t.Fatal(err)
		}
		payload.ReplacementBricks = [][3]int32{}
		_, emptyInfo, err := content.EncodeVoxelObjectPayload(payload, nil)
		if err != nil || emptyInfo.ContentID != nilInfo.ContentID {
			t.Fatalf("schema2 %s empty selectors changed canonical acceptance/identity: %v", mode, err)
		}
		if payload.ReplacementBricks == nil {
			t.Fatal("encode mutated caller's empty selector slice")
		}
	}
}
