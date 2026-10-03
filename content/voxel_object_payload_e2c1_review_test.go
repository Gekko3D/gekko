package content_test

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"testing"
)

func TestE2c1ResolveChargesEmptyReplacementSelectors(t *testing.T) {
	base := &content.VoxelObjectSnapshotDef{SchemaVersion: 1}
	identity, _, err := content.VoxelObjectBaseIdentity(base, e2aLattice(), nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := &content.VoxelObjectPayloadDef{SchemaVersion: content.HybridVoxelObjectPayloadSchemaVersion, Mode: content.VoxelObjectPayloadHybridDelta, PlacementID: "placement", ItemID: "item", Lattice: e2aLattice(), BaseIdentity: identity, ReplacementBricks: [][3]int32{{-1, 0, 0}, {0, 0, 0}}}
	for _, profile := range []voxelcodec.Limits{{MaxBricks: 1}, {Bounds: &voxelcodec.Bounds{Min: [3]int32{}, Max: [3]int32{}}}} {
		codec := c1aContentCodec(t, voxelcodec.Options{Limits: profile})
		if result, err := content.ResolveVoxelObjectPayload(payload, base, e2aLattice(), "placement", "item", codec); err == nil || result != nil {
			t.Fatal("resolve ignored empty selector logical profile", profile)
		}
	}
	codec := c1aContentCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxBricks: 2, Bounds: &voxelcodec.Bounds{Min: [3]int32{-1, 0, 0}, Max: [3]int32{}}}})
	result, err := content.ResolveVoxelObjectPayload(payload, base, e2aLattice(), "placement", "item", codec)
	if err != nil || result == nil || len(result.Voxels) != 0 {
		t.Fatal("matching empty selector profile failed", err)
	}
}
