package derived

import (
	"errors"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI09HarnessRejectsUnalignedSourceSpanBeforeLoading(t *testing.T) {
	d, _ := i09Fixture(t)
	d.ChunkSize = 3 // Valid standalone source grid; 3 does not divide128/512/2048.
	calls := 0
	b, err := BuildIslandStreamHarness(d, func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
		calls++
		return nil, errors.New("unexpected callback")
	}, IslandStreamHarnessOptions{})
	if b != nil || err == nil || calls != 0 {
		t.Fatalf("unaligned source consumed callback: result%v err%v calls%d", b, err, calls)
	}
}

func TestI09HarnessRejectsOver64MillionDeclaredVoxelsBeforeLoading(t *testing.T) {
	d := &content.ImportedWorldDef{SchemaVersion: 2, WorldID: "bounded-source", Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: 256, VoxelResolution: .1}
	for x := -2; x <= 2; x++ {
		d.Entries = append(d.Entries, content.ImportedWorldChunkEntryDef{Coord: content.TerrainChunkCoordDef{X: x}, ChunkPath: "full" + content.TerrainChunkCoordDef{X: x}.String() + ".gkchunk", NonEmptyVoxelCount: 256 * 256 * 256, PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 24})
	}
	// Each declared chunk can be one uniform RLE run and individually fits the
	// supported side256; only the fixture's aggregate input budget is exceeded.
	calls := 0
	b, err := BuildIslandStreamHarness(d, func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
		calls++
		return nil, errors.New("unexpected callback")
	}, IslandStreamHarnessOptions{})
	if b != nil || err == nil || calls != 0 {
		t.Fatalf("oversized fixture consumed callback: result%v err%v calls%d", b, err, calls)
	}
}
