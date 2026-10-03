package content_test

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestE2c3MetadataSizeMatchesTypedC1WithoutInspectingVoxels(t *testing.T) {
	codec := c1aContentCodec(t, voxelcodec.Options{})
	for _, kind := range []string{"default_full", "base_delta", "hybrid_nil", "hybrid_empty", "hybrid_multiple"} {
		t.Run(kind, func(t *testing.T) {
			payload := e2aPayload(content.VoxelObjectPayloadFull, content.VoxelObjectVoxelDef{Value: 3})
			payload.PlacementID = "P\x00\"<雪"
			payload.ItemID = "item\\雪"
			payload.Lattice.RasterizationVersion = "raster\x00雪"
			switch kind {
			case "default_full":
				payload.SchemaVersion = 0
			case "base_delta":
				payload.Mode = content.VoxelObjectPayloadBaseDelta
				payload.BaseIdentity = strings.Repeat("a", 64)
			default:
				payload.SchemaVersion = 3
				payload.Mode = content.VoxelObjectPayloadHybridDelta
				payload.BaseIdentity = strings.Repeat("a", 64)
				if kind == "hybrid_empty" {
					payload.ReplacementBricks = [][3]int32{}
				}
				if kind == "hybrid_multiple" {
					payload.ReplacementBricks = [][3]int32{{-1, 0, 0}, {0, 1, 0}}
				}
			}
			before := e2c1Clone(payload)
			if payload.ReplacementBricks != nil && len(payload.ReplacementBricks) == 0 {
				before.ReplacementBricks = make([][3]int32, 0)
			}
			size, err := content.VoxelObjectPayloadMetadataSize(payload)
			if err != nil {
				t.Fatal(err)
			}
			frame, _, err := content.EncodeVoxelObjectPayload(payload, codec)
			if err != nil {
				t.Fatal(err)
			}
			doc, _, err := codec.Decode(frame)
			if err != nil {
				t.Fatal(err)
			}
			if size != len(doc.Metadata) {
				t.Fatalf("metadata size=%d actual=%d", size, len(doc.Metadata))
			}
			if !reflect.DeepEqual(payload, before) {
				t.Fatal("metadata size mutated input")
			}
			payload.Voxels = []content.VoxelObjectVoxelDef{{X: int(^uint(0) >> 1)}, {X: 1, Value: 3}, {X: 1, Value: 4}}
			if junk, err := content.VoxelObjectPayloadMetadataSize(payload); err != nil || junk != size {
				t.Fatal("metadata size inspected invalid/duplicate voxel records", err)
			}
		})
	}
	for _, invalid := range []string{"nil", "owner", "lattice", "selector_order", "schema_mode"} {
		payload := e2c1Payload(t)
		payload.ReplacementBricks = [][3]int32{{-2, 0, 0}, {1, 0, 0}}
		switch invalid {
		case "nil":
			payload = nil
		case "owner":
			payload.ItemID = ""
		case "lattice":
			payload.Lattice.VoxelResolution = float32(math.Inf(1))
		case "selector_order":
			payload.ReplacementBricks = [][3]int32{{1, 0, 0}, {-2, 0, 0}}
		case "schema_mode":
			payload.Mode = content.VoxelObjectPayloadFull
		}
		if size, err := content.VoxelObjectPayloadMetadataSize(payload); err == nil || size != 0 {
			t.Fatal("invalid metadata acquired size", invalid, size, err)
		}
	}
}
