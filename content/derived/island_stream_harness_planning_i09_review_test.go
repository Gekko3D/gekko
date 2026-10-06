package derived

import (
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI09HarnessImpossiblePageBudgetDoesNotExpandPlan(t *testing.T) {
	d := &content.ImportedWorldDef{SchemaVersion: 2, WorldID: "bounded-plan", ChunkSize: 4, VoxelResolution: 1, Entries: []content.ImportedWorldChunkEntryDef{{ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 24}}}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	calls := 0
	b, err := BuildIslandStreamHarness(d, func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
		calls++
		return nil, errors.New("unexpected callback")
	}, IslandStreamHarnessOptions{RootSpan: 256, MacroSpan: 64, RegionalSpan: 16, MaxPages: 1})
	runtime.ReadMemStats(&after)
	if b != nil || err == nil || calls != 0 {
		t.Fatal("invalid page budget published or consumed source")
	}
	// Even the minimum4096 roots exceed this budget. Metadata checking should
	// not allocate the roughly23MiB full route refinement plan first.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 4<<20 {
		t.Fatalf("impossible page budget expanded planning: %d bytes", allocated)
	}
}
