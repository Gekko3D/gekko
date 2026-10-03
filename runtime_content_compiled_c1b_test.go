package gekko

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c1bDictionaryChunk(t *testing.T) (string, *voxelcodec.Codec, *content.ImportedWorldChunkDef) {
	t.Helper()
	history := bytes.Repeat([]byte(`{"world_id":"dictionary-c1b","schema_version":1,"chunk_size":16,"voxel_resolution":0.25}`), 16)
	c, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 31, Bytes: history}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	chunk := &content.ImportedWorldChunkDef{WorldID: "dictionary-c1b", ChunkSize: 16, VoxelResolution: 0.25, Voxels: []content.ImportedWorldVoxelDef{{Value: 3}, {X: 8, Y: 1, Z: 2, Value: 4, MaterialValue: 255}}}
	path := filepath.Join(t.TempDir(), "dictionary.gkchunk")
	if _, err := content.SaveImportedWorldChunkCompiledWithCodec(path, chunk, c); err != nil {
		t.Fatal(err)
	}
	frame, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, info, err := c.Decode(frame); err != nil || info.DictionaryID != 31 {
		t.Fatal("fixture did not emit its required raw dictionary")
	}
	return path, c, chunk
}

func TestC1bLoaderFixedProfileSharedScopesAndSingleflight(t *testing.T) {
	path, c, want := c1bDictionaryChunk(t)
	wrong, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 31, Bytes: []byte("different raw history")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wrong.Close() })
	options := RuntimeContentLoaderOptions{MaxCacheBytes: 1 << 20, ImportedWorldCodec: c}
	loader := NewRuntimeContentLoader(options)
	options.ImportedWorldCodec = wrong // Constructor selection is fixed per owner.
	a, b := loader.NewScope(), loader.NewScope()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	type result struct {
		chunk *content.ImportedWorldChunkDef
		err   error
	}
	start, done := make(chan struct{}), make(chan result, 8)
	for i := 0; i < 8; i++ {
		requester := loader
		if i%3 == 1 {
			requester = a.Loader()
		} else if i%3 == 2 {
			requester = b.Loader()
		}
		go func(l *RuntimeContentLoader) {
			<-start
			chunk, err := l.LoadImportedWorldChunk(path)
			done <- result{chunk, err}
		}(requester)
	}
	close(start)
	var shared *content.ImportedWorldChunkDef
	for range 8 {
		got := <-done
		if got.err != nil || got.chunk == nil || !slices.Equal(got.chunk.Voxels, want.Voxels) {
			t.Fatal("fixed/scoped dictionary profile lost raw channels:", got.err)
		}
		if shared == nil {
			shared = got.chunk
		} else if shared != got.chunk {
			t.Fatal("same owner/path returned independent decodes")
		}
	}
	stats := loader.Stats()
	if stats.Misses-stats.LoadWaits != 1 || stats.Hits+stats.Misses != 8 || stats.Entries != 1 || stats.Bytes <= 0 || stats.PinnedBytes != stats.Bytes {
		t.Fatalf("profile bypassed existing singleflight/decoded ownership: %+v", stats)
	}
	for _, profile := range []*voxelcodec.Codec{nil, wrong} {
		other := NewRuntimeContentLoader(RuntimeContentLoaderOptions{ImportedWorldCodec: profile})
		if partial, err := other.LoadImportedWorldChunk(path); err == nil || partial != nil {
			t.Fatal("wrong owner profile published dictionary content")
		}
		if stats := other.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
			t.Fatalf("failed profile retained decoded ownership: %+v", stats)
		}
	}
	loader.Clear()
	if loader.Stats().PinnedBytes == 0 {
		t.Fatal("Clear invalidated active scoped ownership")
	}
	a.Close()
	b.Close()
	loader.Clear()
	if stats := loader.Stats(); stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("released scopes and Clear retained decoded data: %+v", stats)
	}
	if !slices.Equal(shared.Voxels, want.Voxels) {
		t.Fatal("scope/cache cleanup invalidated borrowed decoded output")
	}
	frame, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Decode(frame); err != nil {
		t.Fatal("loader/scope cleanup closed caller-owned codec:", err)
	}
}

func TestC1bBorrowedProfileSurvivesStopAndLegacyIgnoresClosedCodec(t *testing.T) {
	path, c, _ := c1bDictionaryChunk(t)
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{ImportedWorldCodec: c})
	_, cmd, _, _ := s2aStartRuntime(t, StreamedLevelRuntimeConfig{Loader: loader})
	if _, err := loader.LoadImportedWorldChunk(path); err != nil {
		t.Fatal(err)
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	frame, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Decode(frame); err != nil {
		t.Fatal("runtime Stop closed a borrowed profile:", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	loader.Clear() // Force a fresh decode; accepted cached hits need not use the codec.
	if partial, err := loader.LoadImportedWorldChunk(path); !errors.Is(err, voxelcodec.ErrClosed) || partial != nil {
		t.Fatal("fresh compiled load ignored closed caller profile:", err)
	}
	for _, kind := range []string{content.ImportedWorldChunkPayloadSparseJSONV1, content.ImportedWorldChunkPayloadDenseRLEBinaryV1} {
		legacy := &content.ImportedWorldChunkDef{WorldID: "legacy", ChunkSize: 8, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{X: 1, Value: 4, MaterialValue: 12}}}
		legacyPath := filepath.Join(t.TempDir(), "legacy.gkchunk")
		if err := content.SaveImportedWorldChunkWithOptions(legacyPath, legacy, content.ImportedWorldChunkSaveOptions{PayloadKind: kind}); err != nil {
			t.Fatal(err)
		}
		read, err := loader.LoadImportedWorldChunk(legacyPath)
		if err != nil || read == nil || !slices.Equal(read.Voxels, legacy.Voxels) {
			t.Fatal("legacy JSON/RLE loading used closed compiled profile:", err)
		}
	}
}
