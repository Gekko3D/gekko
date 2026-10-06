package gekko

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI12ScopedRLESourceSingleflightPinsOwnedBytesAndSeparateDenseCache(t *testing.T) {
	job, _ := i12ProxyFixture(t)
	path := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
	const calls = 12
	scopes := make([]*RuntimeContentLoadScope, calls)
	sources := make([]*content.ImportedWorldChunkRLESource, calls)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range scopes {
		scopes[i] = job.Loader.NewScope()
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			source, err := scopes[i].Loader().LoadImportedWorldChunkRLESource(path)
			if err != nil {
				t.Errorf("source load: %v", err)
				return
			}
			sources[i] = source
		}(i)
	}
	close(start)
	wg.Wait()
	for _, source := range sources {
		if source == nil || source != sources[0] {
			t.Fatal("source cache/singleflight did not share immutable owned source")
		}
	}
	file, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stats := job.Loader.Stats()
	if stats.Entries != 1 || stats.Misses != 1+stats.LoadWaits || stats.Hits+stats.Misses != calls || stats.PinnedBytes != stats.Bytes || stats.Bytes < file.Size() {
		t.Fatalf("source retained backing undercharged or cache ownership changed: %+v file%d", stats, file.Size())
	}
	metadata := sources[0].Metadata()
	metadata.Tags[0] = "caller-mutated"
	if sources[0].Metadata().Tags[0] == "caller-mutated" || len(sources[0].Metadata().Voxels) != 0 {
		t.Fatal("source metadata aliases or retains voxels")
	}
	dense, err := scopes[0].Loader().LoadImportedWorldChunk(path)
	if err != nil || len(dense.Voxels) == 0 {
		t.Fatalf("dense API changed: %v", err)
	}
	if job.Loader.Stats().Entries != 2 {
		t.Fatal("source/dense cache kinds collided")
	}
	for i := 0; i < calls/2; i++ {
		scopes[i].Close()
		scopes[i].Close()
	}
	if job.Loader.Stats().PinnedBytes == 0 {
		t.Fatal("partial scope close released another source pin")
	}
	job.Loader.Clear()
	if job.Loader.Stats().PinnedBytes == 0 {
		t.Fatal("Clear invalidated active source leases")
	}
	for _, scope := range scopes {
		scope.Close()
	}
	if job.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("final close retained source/dense pins")
	}
	job.Loader.Clear()
	if job.Loader.Stats().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 || job.Loader.Stats().Entries != 0 {
		t.Fatal("source/dense ownership survived final cleared scope")
	}
	count := 0
	for range sources[0].Voxels() {
		count++
	}
	if count != sources[0].Metadata().NonEmptyVoxelCount {
		t.Fatal("source borrower mutated after cache eviction")
	}
}

func TestI12RLELoaderSentinelFallbackAndCorruptionNeverCache(t *testing.T) {
	job, _ := i12ProxyFixture(t)
	path := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
	dense, err := content.LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := path + ".json"
	if err := content.SaveImportedWorldChunk(jsonPath, dense); err != nil {
		t.Fatal(err)
	}
	if _, err := job.Loader.LoadImportedWorldChunkRLESource(jsonPath); !errors.Is(err, content.ErrImportedWorldChunkNotRLE) {
		t.Fatalf("non-RLE sentinel lost: %v", err)
	}
	if job.Loader.Stats().Entries != 0 {
		t.Fatal("sentinel source entered cache")
	}
	if _, err := job.Loader.LoadImportedWorldChunk(jsonPath); err != nil {
		t.Fatal(err)
	}
	job.Loader.Clear()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if source, err := job.Loader.LoadImportedWorldChunkRLESource(path); source != nil || err == nil || errors.Is(err, content.ErrImportedWorldChunkNotRLE) {
			t.Fatal("RLE corruption became successful/sentinel source")
		}
	}
	if job.Loader.Stats().Entries != 0 || job.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("corrupt source entered retained cache")
	}
}
