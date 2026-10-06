package gekko

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI12EmptyRLELongCacheKeyReservationBoundsRetainedEnvelope(t *testing.T) {
	job, _ := i12ProxyFixture(t)
	dir := filepath.Dir(job.ManifestPath)
	for i := 0; i < 4; i++ {
		dir = filepath.Join(dir, strings.Repeat("p", 180))
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "empty.gkchunk")
	chunk := &content.ImportedWorldChunkDef{WorldID: "empty-i12", SchemaVersion: 1, ChunkSize: 1, VoxelResolution: 1}
	saved, err := content.SaveImportedWorldChunkWithOptionsResult(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if err != nil {
		t.Fatal(err)
	}
	job.ManifestPath = filepath.Join(dir, "world.gkworld")
	job.LOD = content.ImportedWorldLODDef{ChunkPath: "empty.gkchunk", ChunkSize: 1, VoxelResolution: 1, PayloadKind: saved.PayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes}
	job.pendingOwner = newStreamedPendingPreparedOwner(1 << 20)
	prepared := prepareStreamedSectorProxyLoad(job)
	defer prepared.release()
	if prepared.Err != nil || prepared.pendingCredit == nil || prepared.Chunk == nil || prepared.Chunk.NonEmptyVoxelCount != 0 {
		t.Fatalf("empty RLE preparation failed: %+v", prepared)
	}
	actual := streamedPreparedProxyCharge(prepared)
	if reserved := job.pendingOwner.snapshot().Bytes; reserved < actual {
		t.Fatalf("prebuild reservation %d undercharged retained long-key envelope %d (key bytes %d)", reserved, actual, len(prepared.PreparedGeometryCacheKey))
	}
}
