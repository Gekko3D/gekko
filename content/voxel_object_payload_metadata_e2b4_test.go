package content_test

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"reflect"
	"strings"
	"testing"
)

func TestE2b4PayloadMetadataPreflightAndDefaultProfile(t *testing.T) {
	want := voxelcodec.Limits{MaxEncodedBytes: 32 << 20, MaxDecodedBytes: 32 << 20, MaxBricks: 16384, MaxVoxels: 1048576, MaxMetadataBytes: 1 << 20, MaxAuxBytes: 1 << 20, MaxDictionaryBytes: 64 << 10, MaxKindBytes: 64, MaxBakeVersionBytes: 128}
	limits := voxelcodec.DefaultLimits()
	if !reflect.DeepEqual(limits, want) {
		t.Fatalf("default profile changed: %+v", limits)
	}
	limits.MaxBricks = 1
	if !reflect.DeepEqual(voxelcodec.DefaultLimits(), want) {
		t.Fatal("default profile aliases caller-owned value")
	}
	payload := e2aPayload(content.VoxelObjectPayloadBaseDelta)
	payload.BaseIdentity = strings.Repeat("a", 64)
	before := *payload
	if err := content.ValidateVoxelObjectPayloadMetadata(payload); err != nil {
		t.Fatal(err)
	}
	if allocations := testing.AllocsPerRun(100, func() {
		if err := content.ValidateVoxelObjectPayloadMetadata(payload); err != nil {
			panic(err)
		}
	}); allocations != 0 {
		t.Fatalf("valid metadata preflight allocates: %g", allocations)
	}
	if !reflect.DeepEqual(*payload, before) {
		t.Fatal("metadata validation mutated input")
	}
	payload.BaseIdentity = strings.Repeat("A", 64)
	if err := content.ValidateVoxelObjectPayloadMetadata(payload); err == nil {
		t.Fatal("metadata validation accepted noncanonical base identity")
	}
	payload.BaseIdentity = before.BaseIdentity
	payload.PlacementID = strings.Repeat("P", 1025)
	if err := content.ValidateVoxelObjectPayloadMetadata(payload); err == nil {
		t.Fatal("metadata validation accepted unrepresentable owner")
	}
	payload.PlacementID = before.PlacementID
	payload.ItemID = "\xff"
	if err := content.ValidateVoxelObjectPayloadMetadata(payload); err == nil {
		t.Fatal("metadata validation accepted invalid UTF-8 owner")
	}
}
