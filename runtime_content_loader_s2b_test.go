package gekko

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/gekko3d/gekko/content"
)

func s2bWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("S2b operation did not finish")
		var zero T
		return zero
	}
}
func s2bUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("S2b condition did not become true")
		}
		runtime.Gosched()
	}
}
func s2bBarrier(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(release)
	return ch, release
}

type s2bContentFile struct {
	kind, path string
	load       func(*RuntimeContentLoader) (any, error)
}

func s2bContentFiles(t *testing.T) []s2bContentFile {
	t.Helper()
	root := t.TempDir()
	var files []s2bContentFile
	add := func(kind string, save func(string) error, load func(*RuntimeContentLoader, string) (any, error)) {
		suffix := map[string]string{"asset": ".gkasset", "level": ".gklevel", "terrain-chunk": ".gkchunk", "terrain-manifest": ".gkterrainmanifest", "imported-chunk": ".gkchunk", "imported-world": ".gkworld", "aux": ".gkaux", "backing": ".gkvoxelbacking"}[kind]
		path := filepath.Join(root, kind+suffix)
		if err := save(path); err != nil {
			t.Fatal(err)
		}
		files = append(files, s2bContentFile{kind, path, func(l *RuntimeContentLoader) (any, error) { return load(l, path) }})
	}
	asset := content.NewAssetDef("s2b")
	asset.Parts = []content.AssetPartDef{{ID: "part", Name: "part", Source: testProceduralPartSource()}}
	asset.Tags = []string{strings.Repeat("a", 128)}
	add("asset", func(p string) error { return content.SaveAsset(p, asset) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadAsset(p) })
	level := content.NewLevelDef("s2b")
	level.Placements = []content.LevelPlacementDef{{ID: "p", AssetPath: "asset.gkasset", Tags: []string{strings.Repeat("l", 128)}}}
	add("level", func(p string) error { return content.SaveLevel(p, level) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadLevel(p) })
	terrain := &content.TerrainChunkDef{TerrainID: "s2b", ChunkSize: 16, VoxelResolution: 1, SolidValue: 1, Columns: []content.TerrainChunkColumnDef{{X: 1, Z: 1, FilledVoxels: 3}}, NonEmptyVoxelCount: 3}
	add("terrain-chunk", func(p string) error { return content.SaveTerrainChunk(p, terrain) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadTerrainChunk(p) })
	manifest := &content.TerrainChunkManifestDef{TerrainID: "s2b", ChunkSize: 16, VoxelResolution: 1, Entries: []content.TerrainChunkEntryDef{{ChunkPath: "terrain-chunk.gkchunk", TerrainID: "s2b", ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 3}}}
	add("terrain-manifest", func(p string) error { return content.SaveTerrainChunkManifest(p, manifest) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadTerrainChunkManifest(p) })
	chunk := &content.ImportedWorldChunkDef{WorldID: "s2b", ChunkSize: 16, VoxelResolution: 1, Voxels: streamedRuntimeTestFloorVoxels(0, 15, 0, 15, 0), NonEmptyVoxelCount: 256}
	add("imported-chunk", func(p string) error {
		return content.SaveImportedWorldChunkWithOptions(p, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
	}, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadImportedWorldChunk(p) })
	world := &content.ImportedWorldDef{WorldID: "s2b", Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: 16, VoxelResolution: 1, Entries: []content.ImportedWorldChunkEntryDef{{ChunkPath: "imported-chunk.gkchunk", NonEmptyVoxelCount: 256, Aux: &content.ImportedWorldChunkAuxRefDef{AuxPath: "aux.gkaux"}, Tags: []string{"nested"}}}, Sectors: []content.ImportedWorldSectorDef{{FullChunkRefs: []content.TerrainChunkCoordDef{{}}, Tags: []string{"sector"}}}}
	add("imported-world", func(p string) error { return content.SaveImportedWorld(p, world) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadImportedWorld(p) })
	aux := &content.ImportedWorldChunkAuxDef{WorldID: "s2b", ChunkSize: 16, VoxelResolution: 1, Records: []content.ImportedWorldBrickAuxDef{{Bytes: make([]byte, 4096)}}}
	add("aux", func(p string) error { return content.SaveImportedWorldChunkAux(p, aux) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadImportedWorldChunkAux(p) })
	backing := testPlaneTreeBackingDef()
	add("backing", func(p string) error { return content.SaveVoxelBacking(p, backing) }, func(l *RuntimeContentLoader, p string) (any, error) { return l.LoadVoxelBacking(p) })
	return files
}
func s2bLoad(t *testing.T, file s2bContentFile, loader *RuntimeContentLoader) any {
	t.Helper()
	def, err := file.load(loader)
	if err != nil || def == nil {
		t.Fatalf("load %s: definition=%v error=%v", file.kind, def, err)
	}
	return def
}

func TestS2bLoaderAllKindsShareByteBudget(t *testing.T) {
	files := s2bContentFiles(t)
	charges := make([]int64, len(files))
	var maximum, total int64
	for i, file := range files {
		loader := NewRuntimeContentLoader()
		s2bLoad(t, file, loader)
		charges[i] = loader.Stats().Bytes
		if charges[i] <= 0 {
			t.Fatalf("%s has no decoded charge", file.kind)
		}
		total += charges[i]
		if charges[i] > maximum {
			maximum = charges[i]
		}
	}
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: maximum})
	var evictions int
	for i, file := range files {
		first := s2bLoad(t, file, loader)
		if second := s2bLoad(t, file, loader); first != second {
			t.Fatalf("%s exact-budget admission was not warm", file.kind)
		}
		stats := loader.Stats()
		if stats.Bytes > maximum || stats.Bytes < charges[i] || stats.PinnedBytes != 0 || stats.OverBudgetBytes != 0 {
			t.Fatalf("%s violated shared byte ceiling: %+v", file.kind, stats)
		}
		evictions = stats.Evictions
	}
	if total <= maximum || evictions == 0 {
		t.Fatalf("all eight kinds must compete for one ceiling: total=%d max=%d stats=%+v", total, maximum, loader.Stats())
	}
}

func TestS2bLoaderAliasesDeletedHitsKindsAndNilCompatibility(t *testing.T) {
	files := s2bContentFiles(t)
	file := files[0]
	loader := NewRuntimeContentLoader()
	first := s2bLoad(t, file, loader)
	var nilLoader *RuntimeContentLoader
	s2bLoad(t, file, nilLoader)
	relative, err := filepath.Rel(".", file.path)
	if err != nil { // Rel requires like absolute roots.
		cwd, e := os.Getwd()
		if e != nil {
			t.Fatal(e)
		}
		relative, err = filepath.Rel(cwd, file.path)
	}
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Dir(file.path) + "/unused/../" + filepath.Base(file.path)
	for _, path := range []string{relative, alias} {
		got, e := loader.LoadAsset(path)
		if e != nil || got != first {
			t.Fatalf("alias %q failed to share identity: %v", path, e)
		}
	}
	if err := os.Remove(file.path); err != nil {
		t.Fatal(err)
	}
	if got, e := loader.LoadAsset(alias); e != nil || got != first {
		t.Fatalf("warm deleted-file hit failed: %v", e)
	}
	if loader.Stats().Entries != 1 {
		t.Fatalf("aliases duplicated ownership: %+v", loader.Stats())
	}
	// Rewrite a path between typed requests. Kind identity must remain distinct
	// even when another definition at this path is already warm.
	path := filepath.Join(t.TempDir(), "both.json")
	if err := content.SaveAsset(path, content.NewAssetDef("both")); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.LoadAsset(path); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveLevel(path, content.NewLevelDef("both")); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.LoadLevel(path); err != nil {
		t.Fatal(err)
	}
	if loader.Stats().Entries != 3 {
		t.Fatalf("content kinds collided: %+v", loader.Stats())
	}
	for _, other := range files[1:] {
		s2bLoad(t, other, nilLoader)
	}
	if _, err := nilLoader.LoadAsset(""); err == nil {
		t.Fatal("nil passthrough lost empty-path error")
	}
}

func TestS2bLoaderLRUBoundaryBypassAndPointerSurvival(t *testing.T) {
	files := s2bContentFiles(t)
	file := files[0]
	probe := NewRuntimeContentLoader()
	s2bLoad(t, file, probe)
	charge := probe.Stats().Bytes
	paths := []string{file.path, filepath.Join(filepath.Dir(file.path), "copy-b"), filepath.Join(filepath.Dir(file.path), "copy-c")}
	bytes, err := os.ReadFile(file.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths[1:] {
		if err := os.WriteFile(p, bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 2 * charge})
	a, _ := l.LoadAsset(paths[0])
	b, _ := l.LoadAsset(paths[1])
	l.LoadAsset(paths[0])
	l.LoadAsset(paths[2])
	if got, _ := l.LoadAsset(paths[0]); got != a {
		t.Fatal("recent hit was evicted")
	}
	if got, _ := l.LoadAsset(paths[1]); got == b {
		t.Fatal("oldest entry survived byte pressure")
	}
	if a.Name != "s2b" || b.Parts[0].ID != "part" {
		t.Fatal("eviction mutated returned definitions")
	}
	for _, budget := range []int64{charge, charge - 1, -1} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: budget})
			first := s2bLoad(t, file, l)
			second := s2bLoad(t, file, l)
			warm := budget == charge
			if (first == second) != warm {
				t.Fatalf("warm=%t budget=%d", first == second, budget)
			}
			stats := l.Stats()
			if warm && (stats.Bytes != charge || stats.Entries != 1) {
				t.Fatalf("exact boundary not retained: %+v", stats)
			}
			if !warm && (stats.Bytes != 0 || stats.Entries != 0) {
				t.Fatalf("bypass retained decoded ownership: %+v", stats)
			}
			if budget > 0 && !warm && stats.OversizedBypasses != 2 {
				t.Fatalf("oversized bypasses not reported: %+v", stats)
			}
		})
	}
	if NewRuntimeContentLoader().Stats().MaxBytes != 128<<20 || NewRuntimeContentLoader(RuntimeContentLoaderOptions{}).Stats().MaxBytes != 128<<20 {
		t.Fatal("zero must select 128 MiB")
	}
}

func TestS2bLoaderScopesBalanceAndClearPreservesLiveData(t *testing.T) {
	files := s2bContentFiles(t)
	l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1})
	a, b := l.NewScope(), l.NewScope()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	first := s2bLoad(t, files[4], a.Loader())
	s2bLoad(t, files[4], a.Loader())
	second := s2bLoad(t, files[4], b.Loader())
	if first != second {
		t.Fatal("independent scopes did not share one decoded entry")
	}
	initial := l.Stats()
	if initial.Bytes <= 1 || initial.PinnedBytes != initial.Bytes || initial.Entries != 1 || initial.OverBudgetBytes != initial.Bytes-1 {
		t.Fatalf("pinned oversize pressure missing: %+v", initial)
	}
	l.Clear()
	if got := s2bLoad(t, files[4], a.Loader()); got != first {
		t.Fatal("Clear removed active scope identity")
	}
	a.Close()
	a.Close()
	if stats := l.Stats(); stats.PinnedBytes != initial.PinnedBytes {
		t.Fatalf("one scope removed another scope's pin: %+v", stats)
	}
	b.Close()
	if stats := l.Stats(); stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
		t.Fatalf("last scope retained oversize storage: %+v", stats)
	}
	if _, err := a.Loader().LoadImportedWorldChunk(files[4].path); err == nil {
		t.Fatal("closed scope accepted load")
	}
	chunk := first.(*content.ImportedWorldChunkDef)
	if len(chunk.Voxels) != 256 || chunk.Voxels[0].Value == 0 {
		t.Fatal("released pointer became unusable")
	}
}

// The only decode seam requested by these tests is
// loadRuntimeContent[T](loader, kind, path, decode). It is the same cache owner
// used by the public Load methods, with injected IO for deterministic barriers.
func TestS2bLoaderConcurrentSingleflightAndFailureRetry(t *testing.T) {
	for _, budget := range []int64{1 << 20, 1, -1} {
		for _, mode := range []string{"success", "error", "nil", "panic"} {
			t.Run(fmt.Sprintf("%d/%s", budget, mode), func(t *testing.T) {
				l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: budget})
				block, release := s2bBarrier(t)
				entered := make(chan struct{})
				var calls atomic.Int32
				def := &content.AssetDef{Name: "shared"}
				failure := errors.New("decode failure")
				type outcome struct {
					def   *content.AssetDef
					err   error
					panic any
				}
				results := make(chan outcome, 3)
				request := func(loader *RuntimeContentLoader, decode func(string) (*content.AssetDef, error)) {
					r := outcome{}
					defer func() { r.panic = recover(); results <- r }()
					r.def, r.err = loadRuntimeContent(loader, "asset", "same", decode)
				}
				firstScope, secondScope := l.NewScope(), l.NewScope()
				t.Cleanup(firstScope.Close)
				t.Cleanup(secondScope.Close)
				go request(firstScope.Loader(), func(string) (*content.AssetDef, error) {
					calls.Add(1)
					close(entered)
					<-block
					switch mode {
					case "error":
						return nil, failure
					case "nil":
						return nil, nil
					case "panic":
						panic("decode panic")
					}
					return def, nil
				})
				s2bWait(t, entered)
				go request(secondScope.Loader(), func(string) (*content.AssetDef, error) { calls.Add(1); return &content.AssetDef{}, nil })
				go request(l, func(string) (*content.AssetDef, error) { calls.Add(1); return &content.AssetDef{}, nil })
				s2bUntil(t, func() bool { return l.Stats().LoadWaits >= 2 })
				independent := make(chan struct{})
				go func() {
					defer close(independent)
					loadRuntimeContent(l, "asset", "other", func(string) (*content.AssetDef, error) { return &content.AssetDef{Name: "other"}, nil })
				}()
				s2bWait(t, independent)
				release()
				for range 3 {
					r := s2bWait(t, results)
					switch mode {
					case "success":
						if r.def != def || r.err != nil || r.panic != nil {
							t.Fatalf("did not share success: %+v", r)
						}
					case "error":
						if r.def != nil || !errors.Is(r.err, failure) {
							t.Fatalf("did not share error: %+v", r)
						}
					case "nil":
						if r.def != nil {
							t.Fatal("nil decoder acquired definition")
						}
					case "panic":
						if r.def != nil || r.panic != "decode panic" {
							t.Fatalf("panic did not wake waiter: %+v", r)
						}
					}
				}
				if calls.Load() != 1 {
					t.Fatalf("same identity decoded %d times", calls.Load())
				}
				if mode == "success" {
					if stats := l.Stats(); stats.PinnedBytes <= 0 {
						t.Fatalf("waiter scope lost atomic pin: %+v", stats)
					}
				} else {
					if got, err := loadRuntimeContent(l, "asset", "same", func(string) (*content.AssetDef, error) { return def, nil }); got != def || err != nil {
						t.Fatalf("failed decode prevented retry: %v", err)
					}
				}
			})
		}
	}
}

func TestS2bLoaderCloseAndClearDuringDecode(t *testing.T) {
	for _, operation := range []string{"scope close", "clear raw", "clear active scope"} {
		t.Run(operation, func(t *testing.T) {
			l := NewRuntimeContentLoader()
			scope := l.NewScope()
			t.Cleanup(scope.Close)
			requestLoader := l
			if operation != "clear raw" {
				requestLoader = scope.Loader()
			}
			block, release := s2bBarrier(t)
			entered := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				_, err := loadRuntimeContent(requestLoader, "asset", "blocked", func(string) (*content.AssetDef, error) {
					close(entered)
					<-block
					return &content.AssetDef{Name: "late"}, nil
				})
				done <- err
			}()
			s2bWait(t, entered)
			if operation == "scope close" {
				scope.Close()
			} else {
				l.Clear()
			}
			release()
			err := s2bWait(t, done)
			stats := l.Stats()
			if operation == "clear active scope" {
				if err != nil || stats.PinnedBytes <= 0 {
					t.Fatalf("Clear lost active scoped request: stats=%+v err=%v", stats, err)
				}
				scope.Close()
				l.Clear()
			} else if stats.Bytes != 0 || stats.PinnedBytes != 0 || stats.Entries != 0 {
				t.Fatalf("late decode repopulated released ownership: %+v", stats)
			}
			if operation == "scope close" && err == nil {
				t.Fatal("decode published success through closed scope")
			}
			if _, err := loadRuntimeContent(l, "asset", "blocked", func(string) (*content.AssetDef, error) { return &content.AssetDef{Name: "new request"}, nil }); err != nil || l.Stats().Bytes == 0 {
				t.Fatalf("Clear permanently disabled later request: %v", err)
			}
		})
	}
}

func TestS2bLoaderGraphChargeCapacityNestedAndSharedIdentity(t *testing.T) {
	type graph struct {
		Data   []byte
		Alias  []byte
		Nested map[string]*graph
		Text   string
	}
	charge := func(def *graph) int64 {
		l := NewRuntimeContentLoader()
		if _, err := loadRuntimeContent(l, "graph", "graph", func(string) (*graph, error) { return def, nil }); err != nil {
			t.Fatal(err)
		}
		return l.Stats().Bytes
	}
	base := charge(&graph{})
	data := make([]byte, 1, 8192)
	single := charge(&graph{Data: data})
	shared := charge(&graph{Data: data, Alias: data})
	distinct := charge(&graph{Data: data, Alias: make([]byte, 1, 8192)})
	if single-base < 8192 || shared != single || distinct-shared < 8192 {
		t.Fatalf("capacity/shared backing not charged consistently: base=%d single=%d shared=%d distinct=%d", base, single, shared, distinct)
	}
	child := &graph{Data: make([]byte, 4096), Text: strings.Repeat("s", 1024)}
	nested := charge(&graph{Nested: map[string]*graph{"a": child, "b": child}})
	independent := charge(&graph{Nested: map[string]*graph{"a": child, "b": {Data: make([]byte, 4096), Text: strings.Repeat("x", 1024)}}})
	if nested-base < 4096+1024 || independent-nested < 4096 {
		t.Fatalf("nested map/pointer storage omitted or duplicated: base=%d nested=%d independent=%d", base, nested, independent)
	}
	files := s2bContentFiles(t)
	l := NewRuntimeContentLoader()
	def := s2bLoad(t, files[4], l).(*content.ImportedWorldChunkDef)
	if l.Stats().Bytes < int64(unsafe.Sizeof(*def))+int64(cap(def.Voxels))*int64(unsafe.Sizeof(content.ImportedWorldVoxelDef{})) {
		t.Fatalf("decoded RLE voxels not fully charged: %+v", l.Stats())
	}
}

func TestS2bLoaderRawUnretainedFlightsShareAndClosedWaiterDoesNotDropLivePin(t *testing.T) {
	for _, budget := range []int64{1, -1} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: budget})
			block, release := s2bBarrier(t)
			entered := make(chan struct{})
			result := make(chan *content.AssetDef, 2)
			def := &content.AssetDef{Name: "uncached"}
			var calls atomic.Int32
			go func() {
				got, _ := loadRuntimeContent(l, "asset", "raw", func(string) (*content.AssetDef, error) { calls.Add(1); close(entered); <-block; return def, nil })
				result <- got
			}()
			s2bWait(t, entered)
			go func() {
				got, _ := loadRuntimeContent(l, "asset", "raw", func(string) (*content.AssetDef, error) { calls.Add(1); return &content.AssetDef{}, nil })
				result <- got
			}()
			s2bUntil(t, func() bool { return l.Stats().LoadWaits == 1 })
			release()
			for range 2 {
				if s2bWait(t, result) != def {
					t.Fatal("overlapping raw borrowers did not share bypass result")
				}
			}
			if calls.Load() != 1 || l.Stats().Bytes != 0 {
				t.Fatalf("uncached flight duplicated decode or retained storage: calls=%d stats=%+v", calls.Load(), l.Stats())
			}
		})
	}
	l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1})
	a, b := l.NewScope(), l.NewScope()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	block, release := s2bBarrier(t)
	entered := make(chan struct{})
	result := make(chan error, 2)
	go func() {
		_, err := loadRuntimeContent(a.Loader(), "asset", "scoped", func(string) (*content.AssetDef, error) {
			close(entered)
			<-block
			return &content.AssetDef{Name: "live"}, nil
		})
		result <- err
	}()
	s2bWait(t, entered)
	go func() {
		_, err := loadRuntimeContent(b.Loader(), "asset", "scoped", func(string) (*content.AssetDef, error) { t.Error("waiter ran duplicate decoder"); return nil, nil })
		result <- err
	}()
	s2bUntil(t, func() bool { return l.Stats().LoadWaits == 1 })
	a.Close()
	release()
	first, second := s2bWait(t, result), s2bWait(t, result)
	if (first == nil) == (second == nil) {
		t.Fatalf("closed/live scope outcomes not independent: first=%v second=%v", first, second)
	}
	if stats := l.Stats(); stats.PinnedBytes <= 1 {
		t.Fatalf("closed requester dropped live waiter's pin: %+v", stats)
	}
	b.Close()
	if l.Stats().Bytes != 0 {
		t.Fatal("final live waiter release leaked storage")
	}
}
