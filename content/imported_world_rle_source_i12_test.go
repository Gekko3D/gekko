package content_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func i12RLEWrite(t testing.TB, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chunk.gkchunk")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func i12RLEPayload(material bool, runs ...[3]uint32) []byte {
	width := 5
	if material {
		width = 6
	}
	data := make([]byte, 4+width*len(runs))
	binary.LittleEndian.PutUint32(data, uint32(len(runs)))
	for i, r := range runs {
		offset := 4 + i*width
		data[offset] = byte(r[0])
		if material {
			data[offset+1] = byte(r[1])
		}
		binary.LittleEndian.PutUint32(data[offset+width-4:], r[2])
	}
	return data
}

func i12RLEHeader(size int, material bool) map[string]any {
	kind := content.ImportedWorldChunkPayloadDenseRLEBinaryV1
	if material {
		kind = content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1
	}
	return map[string]any{"world_id": "i12-source", "schema_version": content.CurrentImportedWorldChunkSchemaVersion, "coord": map[string]int{"x": -2, "y": 3, "z": 1}, "chunk_size": size, "voxel_resolution": 0.25, "payload_kind": kind, "tags": []string{"immutable", "proxy"}}
}

func i12RLEFrame(t testing.TB, header map[string]any, payload []byte) []byte {
	t.Helper()
	metadata, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	data := append([]byte{}, []byte("GKCHNK1\n")...)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(metadata)))
	data = append(data, metadata...)
	return append(data, payload...)
}

func i12RLEWantEquivalent(t *testing.T, path string) *content.ImportedWorldChunkRLESource {
	t.Helper()
	dense, err := content.LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatalf("legacy fixture rejected: %v", err)
	}
	source, err := content.LoadImportedWorldChunkRLESource(path)
	if err != nil || source == nil {
		t.Fatalf("RLE source load: %v", err)
	}
	got := slices.Collect(source.Voxels())
	if !slices.Equal(got, dense.Voxels) {
		t.Fatalf("source voxels=%+v dense=%+v", got, dense.Voxels)
	}
	metadata := source.Metadata()
	if metadata == nil || metadata.Voxels != nil {
		t.Fatal("source metadata materialized voxel records")
	}
	dense.Voxels = nil
	if !reflect.DeepEqual(metadata, dense) {
		t.Fatalf("source header=%+v dense=%+v", metadata, dense)
	}
	return source
}

func TestI12RLESourceMatchesExistingSaverAndDenseDecoder(t *testing.T) {
	for _, material := range []bool{false, true} {
		for _, empty := range []bool{false, true} {
			t.Run(map[bool]string{false: "primary", true: "material"}[material]+map[bool]string{false: "-populated", true: "-empty"}[empty], func(t *testing.T) {
				chunk := &content.ImportedWorldChunkDef{WorldID: "saved-i12", ChunkSize: 8, VoxelResolution: 0.125, Tags: []string{"save"}}
				if !empty {
					// Unsorted input checks X-fast order and row/layer boundaries.
					chunk.Voxels = []content.ImportedWorldVoxelDef{{X: 7, Y: 7, Z: 7, Value: 5}, {X: 0, Y: 1, Value: 7}, {X: 7, Value: 7}, {X: 0, Value: 9}, {Z: 1, Value: 11}, {X: 3, Value: 0}}
					if material {
						chunk.Voxels[0].MaterialValue = 255
						chunk.Voxels[1].MaterialValue = 7
					}
				}
				path := filepath.Join(t.TempDir(), "saved.gkchunk")
				if err := content.SaveImportedWorldChunkWithOptions(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
					t.Fatal(err)
				}
				i12RLEWantEquivalent(t, path)
			})
		}
	}
}

func TestI12RLESourceCrossesRowsAndNormalizesMaterialChannels(t *testing.T) {
	payload := i12RLEPayload(true, [3]uint32{0, 255, 2}, [3]uint32{4, 0, 4}, [3]uint32{5, 5, 4}, [3]uint32{6, 255, 5}, [3]uint32{0, 0, 49})
	path := i12RLEWrite(t, i12RLEFrame(t, i12RLEHeader(4, true), payload))
	source := i12RLEWantEquivalent(t, path)
	voxels := slices.Collect(source.Voxels())
	if len(voxels) != 13 || voxels[0] != (content.ImportedWorldVoxelDef{X: 2, Value: 4}) || voxels[2] != (content.ImportedWorldVoxelDef{Y: 1, Value: 4}) || voxels[8] != (content.ImportedWorldVoxelDef{X: 2, Y: 2, Value: 6, MaterialValue: 255}) {
		t.Fatalf("boundary/material normalization lost: %+v", voxels)
	}
}

func TestI12RLESourceEmptyMaterialBodyMatchesDenseDecoder(t *testing.T) {
	// The saver chooses plain RLE for empty chunks, so explicitly encode the
	// material format to cover its empty-body path and ignored material bytes.
	path := i12RLEWrite(t, i12RLEFrame(t, i12RLEHeader(4, true), i12RLEPayload(true, [3]uint32{0, 255, 64})))
	source := i12RLEWantEquivalent(t, path)
	if source.Metadata().PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 || source.Metadata().NonEmptyVoxelCount != 0 {
		t.Fatal("empty material source changed payload kind or occupied count")
	}
	if got := slices.Collect(source.Voxels()); len(got) != 0 {
		t.Fatalf("zero primary cells acquired material occupancy: %+v", got)
	}
}

func TestI12RLESourcePreservesLegacyResolutionDefaults(t *testing.T) {
	for _, resolution := range []float32{0, -0.25} {
		header := i12RLEHeader(2, false)
		header["voxel_resolution"] = resolution
		path := i12RLEWrite(t, i12RLEFrame(t, header, i12RLEPayload(false, [3]uint32{7, 0, 8})))
		source := i12RLEWantEquivalent(t, path)
		if source.Metadata().VoxelResolution != resolution {
			t.Fatalf("legacy resolution %g changed to %g", resolution, source.Metadata().VoxelResolution)
		}
	}
}

func TestI12RLESourcePreservesLegacyHeaderHintsAndOptionalHash(t *testing.T) {
	for _, hint := range []int{-7, 0, 99} {
		for _, declaredSize := range []int{-3, 0, 12345} {
			for _, hashed := range []bool{false, true} {
				header := i12RLEHeader(2, false)
				payload := i12RLEPayload(false, [3]uint32{0, 0, 3}, [3]uint32{9, 0, 2}, [3]uint32{0, 0, 3})
				header["non_empty_voxel_count"] = hint
				header["payload_size_bytes"] = declaredSize
				if hashed {
					sum := sha256.Sum256(payload)
					header["payload_hash"] = hex.EncodeToString(sum[:])
				}
				source := i12RLEWantEquivalent(t, i12RLEWrite(t, i12RLEFrame(t, header, payload)))
				if source.Metadata().NonEmptyVoxelCount != 2 || source.Metadata().PayloadSizeBytes != declaredSize {
					t.Fatal("normalized count or declared size changed")
				}
			}
		}
	}
}

func TestI12RLESourceMetadataAndReusableConcurrentIterationAreDetached(t *testing.T) {
	path := i12RLEWrite(t, i12RLEFrame(t, i12RLEHeader(4, true), i12RLEPayload(true, [3]uint32{4, 7, 64})))
	source := i12RLEWantEquivalent(t, path)
	want := slices.Collect(source.Voxels())
	metadata := source.Metadata()
	metadata.Tags[0] = "mutated"
	metadata.WorldID = "mutated"
	metadata.NonEmptyVoxelCount = 0
	metadata.Voxels = []content.ImportedWorldVoxelDef{{Value: 99}}
	if got := source.Metadata(); got.WorldID == "mutated" || got.Tags[0] == "mutated" || got.NonEmptyVoxelCount != 64 || got.Voxels != nil {
		t.Fatalf("metadata aliases immutable source: %+v", got)
	}
	sequence := source.Voxels()
	calls := 0
	sequence(func(v content.ImportedWorldVoxelDef) bool { calls++; v.Value = 99; return false })
	if calls != 1 {
		t.Fatalf("early stop called yield %d times", calls)
	}
	if got := slices.Collect(sequence); !slices.Equal(got, want) {
		t.Fatal("early stop or yielded-value mutation changed repeat iteration")
	}
	results := make(chan []content.ImportedWorldVoxelDef, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() { defer workers.Done(); results <- slices.Collect(sequence) }()
	}
	workers.Wait()
	close(results)
	for got := range results {
		if !slices.Equal(got, want) {
			t.Fatal("concurrent iterators share mutable cursor state")
		}
	}
}

func TestI12RLESourceNonRLEIsDistinctFromCorruption(t *testing.T) {
	for _, kind := range []string{content.ImportedWorldChunkPayloadSparseJSONV1, content.ImportedWorldChunkPayloadBrickZstdBinaryV1} {
		path := filepath.Join(t.TempDir(), "fallback.gkchunk")
		chunk := &content.ImportedWorldChunkDef{WorldID: "fallback", ChunkSize: 8, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 7}}}
		if err := content.SaveImportedWorldChunkWithOptions(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: kind}); err != nil {
			t.Fatal(err)
		}
		if _, err := content.LoadImportedWorldChunk(path); err != nil {
			t.Fatal("invalid fallback fixture:", err)
		}
		if source, err := content.LoadImportedWorldChunkRLESource(path); source != nil || !errors.Is(err, content.ErrImportedWorldChunkNotRLE) {
			t.Fatalf("kind %s did not request fallback: %v", kind, err)
		}
	}
	if source, err := content.LoadImportedWorldChunkRLESource(filepath.Join(t.TempDir(), "missing")); source != nil || err == nil || errors.Is(err, content.ErrImportedWorldChunkNotRLE) {
		t.Fatal("IO failure mistaken for codec fallback")
	}
	for _, data := range [][]byte{[]byte("GKCHNK1\n"), append([]byte("GKCHNK1\n"), 0, 0, 0, 0)} {
		if source, err := content.LoadImportedWorldChunkRLESource(i12RLEWrite(t, data)); source != nil || err == nil || errors.Is(err, content.ErrImportedWorldChunkNotRLE) {
			t.Fatal("RLE corruption mistaken for codec fallback")
		}
	}
}

func TestI12RLESourceRejectsUnsafeBodyBeforeReturningSource(t *testing.T) {
	type mutation struct {
		name    string
		header  func(map[string]any)
		payload func([]byte) []byte
		raw     []byte
	}
	cases := []mutation{
		{name: "truncated metadata length", raw: append([]byte("GKCHNK1\n"), 1)},
		{name: "metadata length past EOF", raw: append([]byte("GKCHNK1\n"), 255, 255, 255, 255)},
		{name: "malformed metadata", raw: append(append([]byte("GKCHNK1\n"), 1, 0, 0, 0), '{')},
		{name: "schema zero", header: func(h map[string]any) { h["schema_version"] = 0 }},
		{name: "unknown schema", header: func(h map[string]any) { h["schema_version"] = 999 }},
		{name: "unknown kind", header: func(h map[string]any) { h["payload_kind"] = "other" }},
		{name: "bad hash", header: func(h map[string]any) { h["payload_hash"] = "wrong" }},
		{name: "zero lattice", header: func(h map[string]any) { h["chunk_size"] = 0 }},
		{name: "negative lattice", header: func(h map[string]any) { h["chunk_size"] = -1 }},
		{name: "cube overflow", header: func(h map[string]any) { h["chunk_size"] = 1 << 22 }},
		{name: "truncated run", payload: func(p []byte) []byte { return p[:len(p)-1] }},
		{name: "trailing byte", payload: func(p []byte) []byte { return append(p, 0) }},
		{name: "run count mismatch", payload: func(p []byte) []byte { binary.LittleEndian.PutUint32(p, 2); return p }},
		{name: "run count overflow", payload: func(p []byte) []byte { binary.LittleEndian.PutUint32(p, ^uint32(0)); return p }},
		{name: "zero run", payload: func(p []byte) []byte { binary.LittleEndian.PutUint32(p[len(p)-4:], 0); return p }},
		{name: "undercoverage", payload: func(p []byte) []byte { binary.LittleEndian.PutUint32(p[len(p)-4:], 7); return p }},
		{name: "overcoverage", payload: func(p []byte) []byte { binary.LittleEndian.PutUint32(p[len(p)-4:], 9); return p }},
	}
	for _, material := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(map[bool]string{false: "primary/", true: "material/"}[material]+tc.name, func(t *testing.T) {
				header := i12RLEHeader(2, material)
				payload := i12RLEPayload(material, [3]uint32{3, 7, 8})
				if tc.header != nil {
					tc.header(header)
				}
				if tc.payload != nil {
					payload = tc.payload(payload)
				}
				data := tc.raw
				if data == nil {
					data = i12RLEFrame(t, header, payload)
				}
				if source, err := content.LoadImportedWorldChunkRLESource(i12RLEWrite(t, data)); source != nil || err == nil || errors.Is(err, content.ErrImportedWorldChunkNotRLE) {
					t.Fatalf("unsafe RLE returned source or fallback: %v", err)
				}
			})
		}
	}
}

func i12RLEFilledFile(t testing.TB) string {
	t.Helper()
	header := i12RLEHeader(64, false)
	path := i12RLEWrite(t, i12RLEFrame(t, header, i12RLEPayload(false, [3]uint32{7, 0, 64 * 64 * 64})))
	dense, err := content.LoadImportedWorldChunk(path)
	if err != nil || len(dense.Voxels) != 64*64*64 {
		t.Fatalf("invalid filled benchmark fixture: %v", err)
	}
	return path
}

var i12RLESourceSink *content.ImportedWorldChunkRLESource
var i12RLEDenseSink *content.ImportedWorldChunkDef

func i12RLEBenchmarkLoad(b *testing.B, path string, dense bool) {
	b.Helper()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var err error
		if dense {
			i12RLEDenseSink, err = content.LoadImportedWorldChunk(path)
		} else {
			i12RLESourceSink, err = content.LoadImportedWorldChunkRLESource(path)
		}
		if err != nil {
			b.Fatal(err)
		}
	}
}

func TestI12RLESourceFilledChunkAllocationsAvoidDenseVoxelStaging(t *testing.T) {
	path := i12RLEFilledFile(t)
	dense := testing.Benchmark(func(b *testing.B) { i12RLEBenchmarkLoad(b, path, true) })
	source := testing.Benchmark(func(b *testing.B) { i12RLEBenchmarkLoad(b, path, false) })
	// Compare allocation bytes, never elapsed time. The same uniform source has
	// 262144 occupied cells but only one encoded run and a small metadata header.
	if source.AllocedBytesPerOp() >= dense.AllocedBytesPerOp()/16 {
		t.Fatalf("source retained dense-size staging: source=%d B/op dense=%d B/op", source.AllocedBytesPerOp(), dense.AllocedBytesPerOp())
	}
	t.Logf("source=%d B/op dense=%d B/op", source.AllocedBytesPerOp(), dense.AllocedBytesPerOp())
}

func BenchmarkI12RLESourceFilledChunk(b *testing.B) {
	path := i12RLEFilledFile(b)
	b.Run("source", func(b *testing.B) { i12RLEBenchmarkLoad(b, path, false) })
	b.Run("dense", func(b *testing.B) { i12RLEBenchmarkLoad(b, path, true) })
}
