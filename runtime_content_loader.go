package gekko

import (
	"container/heap"
	"container/list"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

const defaultRuntimeContentCacheBytes int64 = 128 << 20

// RuntimeContentLoaderOptions configures shared decoded warm retention.
// Zero selects 128 MiB; a negative limit disables unscoped warm retention.
type RuntimeContentLoaderOptions struct {
	MaxCacheBytes int64
	// ImportedWorldCodec is borrowed and fixed for this owner and all its scopes.
	// Nil selects bounded default imported IO; the caller owns codec lifetime.
	ImportedWorldCodec *voxelcodec.Codec
	// CompiledAssetCodec is borrowed and fixed for this owner and all its scopes.
	// Nil selects the bounded default compiled-asset profile.
	CompiledAssetCodec *voxelcodec.Codec
}

// RuntimeContentLoaderStats reports admission-time decoded storage estimates.
// Bytes includes pinned entries; external borrowers and decoder temporaries are
// not tracked. These numbers are not a total process memory measurement.
type RuntimeContentLoaderStats struct {
	Entries                                               int
	Bytes, PinnedBytes, MaxBytes, OverBudgetBytes         int64
	Hits, Misses, Evictions, LoadWaits, OversizedBypasses int
	EvictionCandidateVisits                               int
}

// Derived loaders share one owner. The scope declares decoded-data lifetime;
// eviction never changes a returned definition or reuses its storage.
type RuntimeContentLoader struct {
	owner *runtimeContentCache
	scope *RuntimeContentLoadScope
}
type runtimeContentKey struct{ kind, path string }
type runtimeContentEntry struct {
	key       runtimeContentKey
	value     any
	bytes     int64
	pins      int
	lru       *list.Element
	lastLoad  uint64
	heapIndex int
}

// Only unpinned entries participate, ordered by their most recent load/hit.
// Heap membership and recency are protected by runtimeContentCache.mu.
type runtimeContentEvictionHeap []*runtimeContentEntry

func (h runtimeContentEvictionHeap) Len() int           { return len(h) }
func (h runtimeContentEvictionHeap) Less(i, j int) bool { return h[i].lastLoad < h[j].lastLoad }
func (h runtimeContentEvictionHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].heapIndex, h[j].heapIndex = i, j
}
func (h *runtimeContentEvictionHeap) Push(value any) {
	e := value.(*runtimeContentEntry)
	e.heapIndex = len(*h)
	*h = append(*h, e)
}
func (h *runtimeContentEvictionHeap) Pop() any {
	last := len(*h) - 1
	e := (*h)[last]
	(*h)[last] = nil
	*h = (*h)[:last]
	e.heapIndex = -1
	return e
}

type runtimeContentFlight struct {
	done       chan struct{}
	scopes     map[*RuntimeContentLoadScope]struct{}
	rawEpoch   uint64
	hasRaw     bool
	value      any
	err        error
	panicValue any
}
type runtimeContentCache struct {
	importedWorldCodec *voxelcodec.Codec // immutable borrowed profile
	compiledAssetCodec *voxelcodec.Codec // immutable borrowed profile
	mu                 sync.Mutex
	entries            map[runtimeContentKey]*runtimeContentEntry
	flights            map[runtimeContentKey]*runtimeContentFlight
	lru                list.List
	unpinned           runtimeContentEvictionHeap
	loadSequence       uint64
	epoch              uint64
	stats              RuntimeContentLoaderStats
}

// RuntimeContentLoadScope leases each loaded decoded entry once until Close.
// Derived loaders share the base cache and fail loads after the scope closes.
type RuntimeContentLoadScope struct {
	owner   *runtimeContentCache
	loader  *RuntimeContentLoader
	closed  bool // all scope state is protected by owner.mu
	entries map[*runtimeContentEntry]struct{}
}

// NewRuntimeContentLoader creates one LRU shared by all content kinds.
func NewRuntimeContentLoader(options ...RuntimeContentLoaderOptions) *RuntimeContentLoader {
	max := defaultRuntimeContentCacheBytes
	var importedWorldCodec, compiledAssetCodec *voxelcodec.Codec
	if len(options) > 0 {
		if options[0].MaxCacheBytes != 0 {
			max = options[0].MaxCacheBytes
		}
		importedWorldCodec = options[0].ImportedWorldCodec
		compiledAssetCodec = options[0].CompiledAssetCodec
	}
	return &RuntimeContentLoader{owner: &runtimeContentCache{
		entries: make(map[runtimeContentKey]*runtimeContentEntry), flights: make(map[runtimeContentKey]*runtimeContentFlight),
		stats: RuntimeContentLoaderStats{MaxBytes: max}, importedWorldCodec: importedWorldCodec, compiledAssetCodec: compiledAssetCodec,
	}}
}

// NewScope creates an independent lease. A nil loader creates a private cache.
func (l *RuntimeContentLoader) NewScope() *RuntimeContentLoadScope {
	if l == nil {
		l = NewRuntimeContentLoader()
	}
	s := &RuntimeContentLoadScope{owner: l.owner, entries: make(map[*runtimeContentEntry]struct{})}
	s.loader = &RuntimeContentLoader{owner: l.owner, scope: s}
	return s
}

// Loader returns the derived loader that pins successful loads in this scope.
func (s *RuntimeContentLoadScope) Loader() *RuntimeContentLoader {
	if s == nil {
		return nil
	}
	return s.loader
}

// Close releases pins once. Closing a nil scope is harmless.
func (s *RuntimeContentLoadScope) Close() {
	if s == nil {
		return
	}
	c := s.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for e := range s.entries {
		c.unpin(e)
	}
	s.entries = nil
	c.trim()
}

// releaseScopedValue ends this scope's lease when a loaded definition is
// rejected before becoming consumer data. Other entries and scopes keep their
// pins; unscoped warm retention remains subject to the owner's normal budget.
func (l *RuntimeContentLoader) releaseScopedValue(value any) {
	if l == nil || l.scope == nil {
		return
	}
	s := l.scope
	c := s.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	for e := range s.entries {
		definition := e.value
		if wrapped, ok := definition.(interface{ runtimeContentDefinition() any }); ok {
			definition = wrapped.runtimeContentDefinition()
		}
		if e.value != value && definition != value {
			continue
		}
		delete(s.entries, e)
		c.unpin(e)
		c.trim()
		return
	}
}

// Stats snapshots shared cache ownership. A nil loader reports zero values.
func (l *RuntimeContentLoader) Stats() RuntimeContentLoaderStats {
	if l == nil {
		return RuntimeContentLoaderStats{}
	}
	c := l.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := c.stats
	stats.Entries = len(c.entries)
	if stats.MaxBytes >= 0 && stats.Bytes > stats.MaxBytes {
		stats.OverBudgetBytes = stats.Bytes - stats.MaxBytes
	}
	if stats.MaxBytes < 0 {
		stats.OverBudgetBytes = stats.Bytes
	}
	return stats
}

// Clear drops unpinned warm entries and revokes earlier raw in-flight warm
// requests. Live scopes remain protected. A nil loader is harmless.
func (l *RuntimeContentLoader) Clear() {
	if l == nil {
		return
	}
	c := l.owner
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	for _, e := range c.entries {
		if e.pins == 0 {
			c.remove(e)
		}
	}
}
func (c *runtimeContentCache) pin(s *RuntimeContentLoadScope, e *runtimeContentEntry) {
	if _, exists := s.entries[e]; exists {
		return
	}
	s.entries[e] = struct{}{}
	if e.pins == 0 {
		c.unlinkEligible(e)
		c.stats.PinnedBytes += e.bytes
	}
	e.pins++
}

func (c *runtimeContentCache) unpin(e *runtimeContentEntry) {
	e.pins--
	if e.pins == 0 {
		c.stats.PinnedBytes -= e.bytes
		c.makeEligible(e)
	}
}

func (c *runtimeContentCache) makeEligible(e *runtimeContentEntry) {
	if e.pins == 0 && e.heapIndex < 0 {
		heap.Push(&c.unpinned, e)
	}
}

func (c *runtimeContentCache) unlinkEligible(e *runtimeContentEntry) {
	if e.heapIndex >= 0 {
		heap.Remove(&c.unpinned, e.heapIndex)
	}
}

func (c *runtimeContentCache) touch(e *runtimeContentEntry) {
	if c.loadSequence == ^uint64(0) {
		// Preserve complete load order across the rare counter wrap, including
		// pinned entries whose old position must survive their final release.
		c.loadSequence = 0
		for p := c.lru.Back(); p != nil; p = p.Prev() {
			c.loadSequence++
			p.Value.(*runtimeContentEntry).lastLoad = c.loadSequence
		}
		heap.Init(&c.unpinned)
	}
	c.loadSequence++
	e.lastLoad = c.loadSequence
	c.lru.MoveToFront(e.lru)
	if e.heapIndex >= 0 {
		heap.Fix(&c.unpinned, e.heapIndex)
	}
}

func (c *runtimeContentCache) remove(e *runtimeContentEntry) {
	c.unlinkEligible(e)
	delete(c.entries, e.key)
	c.lru.Remove(e.lru)
	e.lru = nil
	c.stats.Bytes -= e.bytes
	c.stats.Evictions++
}
func (c *runtimeContentCache) trim() {
	for len(c.unpinned) > 0 && (c.stats.MaxBytes < 0 || c.stats.Bytes > c.stats.MaxBytes) {
		c.stats.EvictionCandidateVisits++
		c.remove(c.unpinned[0])
	}
}

// Singleflight tracks all requesters before publication so a waiter cannot lose
// its pin to an eviction between completion and waking up. Decode and waits run
// without the cache mutex. A Clear epoch revokes only prior raw warm requests.
func loadRuntimeContent[T any](l *RuntimeContentLoader, kind, path string, decode func(string) (*T, error)) (*T, error) {
	if l == nil {
		return decode(path)
	}
	if path == "" {
		return nil, fmt.Errorf("content path is empty")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	key := runtimeContentKey{kind, filepath.Clean(absolute)}
	c := l.owner
	c.mu.Lock()
	if l.scope != nil && l.scope.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("content load scope is closed")
	}
	if e := c.entries[key]; e != nil {
		c.stats.Hits++
		c.touch(e)
		if l.scope != nil {
			c.pin(l.scope, e)
		}
		c.mu.Unlock()
		return e.value.(*T), nil
	}
	c.stats.Misses++
	f := c.flights[key]
	leader := f == nil
	if leader {
		f = &runtimeContentFlight{done: make(chan struct{}), scopes: make(map[*RuntimeContentLoadScope]struct{})}
		c.flights[key] = f
	} else {
		c.stats.LoadWaits++
	}
	if l.scope != nil {
		f.scopes[l.scope] = struct{}{}
	} else {
		f.hasRaw = true
		f.rawEpoch = c.epoch
	}
	c.mu.Unlock()
	if leader {
		var loaded *T
		var failure error
		var panicValue any
		var bytes int64
		func() {
			defer func() { panicValue = recover() }()
			loaded, failure = decode(key.path)
			// Both decoding and estimation must wake every flight on panic.
			if loaded != nil && failure == nil {
				bytes = runtimeContentGraphCharge(loaded)
			}
		}()
		c.mu.Lock()
		f.value, f.err, f.panicValue = loaded, failure, panicValue
		if loaded != nil && failure == nil && panicValue == nil {
			live := false
			for s := range f.scopes {
				if !s.closed {
					live = true
					break
				}
			}
			warm := f.hasRaw && f.rawEpoch == c.epoch && c.stats.MaxBytes >= 0 && bytes <= c.stats.MaxBytes
			if c.stats.MaxBytes > 0 && bytes > c.stats.MaxBytes && f.hasRaw {
				c.stats.OversizedBypasses++
			}
			if live || warm {
				e := &runtimeContentEntry{key: key, value: loaded, bytes: bytes, heapIndex: -1}
				e.lru = c.lru.PushFront(e)
				c.touch(e)
				c.entries[key] = e
				c.stats.Bytes += bytes
				for s := range f.scopes {
					if !s.closed {
						c.pin(s, e)
					}
				}
				c.makeEligible(e)
				c.trim()
			}
		}
		delete(c.flights, key)
		close(f.done)
		c.mu.Unlock()
	} else {
		<-f.done
	}
	c.mu.Lock()
	closed := l.scope != nil && l.scope.closed
	c.mu.Unlock()
	if closed {
		return nil, fmt.Errorf("content load scope is closed")
	}
	if f.panicValue != nil {
		panic(f.panicValue)
	}
	if f.err != nil {
		return nil, f.err
	}
	if value, ok := f.value.(*T); ok {
		return value, nil
	}
	return nil, nil
}

func (l *RuntimeContentLoader) LoadVoxelBacking(path string) (*content.VoxelBackingDef, error) {
	return loadRuntimeContent(l, "backing", path, content.LoadVoxelBacking)
}
func (l *RuntimeContentLoader) LoadAsset(path string) (*content.AssetDef, error) {
	return loadRuntimeContent(l, "asset", path, content.LoadAsset)
}
func (l *RuntimeContentLoader) LoadLevel(path string) (*content.LevelDef, error) {
	return loadRuntimeContent(l, "level", path, content.LoadLevel)
}
func (l *RuntimeContentLoader) LoadTerrainChunkManifest(path string) (*content.TerrainChunkManifestDef, error) {
	return loadRuntimeContent(l, "terrain-manifest", path, func(path string) (*content.TerrainChunkManifestDef, error) {
		manifest, err := content.LoadTerrainChunkManifest(path)
		if err != nil {
			return nil, err
		}
		if manifest.SchemaVersion == content.TerrainHeightTileManifestSchemaVersion {
			return nil, fmt.Errorf("terrain height tile runtime requires resident height collision support")
		}
		return manifest, nil
	})
}
func (l *RuntimeContentLoader) LoadTerrainChunk(path string) (*content.TerrainChunkDef, error) {
	return loadRuntimeContent(l, "terrain-chunk", path, content.LoadTerrainChunk)
}
func (l *RuntimeContentLoader) LoadImportedWorld(path string) (*content.ImportedWorldDef, error) {
	return loadRuntimeContent(l, "imported-world", path, func(path string) (*content.ImportedWorldDef, error) {
		world, err := content.LoadImportedWorld(path)
		if err != nil {
			return nil, err
		}
		if world.SchemaVersion == content.ImportedWorldPageSchemaVersion {
			return nil, fmt.Errorf("imported world page runtime requires indexed page registration and handoff support")
		}
		return world, nil
	})
}
func (l *RuntimeContentLoader) LoadImportedWorldChunk(path string) (*content.ImportedWorldChunkDef, error) {
	var codec *voxelcodec.Codec
	if l != nil {
		codec = l.owner.importedWorldCodec
	}
	return loadRuntimeContent(l, "imported-chunk", path, func(path string) (*content.ImportedWorldChunkDef, error) {
		return content.LoadImportedWorldChunkWithCodec(path, codec)
	})
}

// LoadImportedWorldChunkRLESource borrows an immutable encoded source from the
// same scoped cache owner as decoded content, under a separate cache kind.
func (l *RuntimeContentLoader) LoadImportedWorldChunkRLESource(path string) (*content.ImportedWorldChunkRLESource, error) {
	return loadRuntimeContent(l, "imported-chunk-rle", path, content.LoadImportedWorldChunkRLESource)
}
func (l *RuntimeContentLoader) LoadImportedWorldChunkAux(path string) (*content.ImportedWorldChunkAuxDef, error) {
	return loadRuntimeContent(l, "aux", path, content.LoadImportedWorldChunkAux)
}
