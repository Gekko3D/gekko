package gekko

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/derived"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func i12ProxyFixture(t *testing.T) (streamedSectorProxyLoadJob, *volume.XBrickMap) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "alpha.gkchunk")
	chunk := &content.ImportedWorldChunkDef{WorldID: "proxy-rle-i12", ChunkSize: 16, VoxelResolution: .25, Tags: []string{"immutable"}}
	for z := 0; z < 8; z++ {
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: 7, MaterialValue: 5})
			}
		}
	}
	chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: 8, Y: 1, Z: 2, Value: 9, MaterialValue: 6})
	chunk.NonEmptyVoxelCount = len(chunk.Voxels)
	saved, err := content.SaveImportedWorldChunkWithOptionsResult(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if err != nil {
		t.Fatal(err)
	}
	if saved.PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
		t.Fatal("fixture is not material RLE")
	}
	dense, err := content.LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatal(err)
	}
	aux := derived.BuildImportedWorldChunkAux(dense, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{{}: dense}, saved.PayloadHash, saved.PayloadSizeBytes, true)
	auxPath := filepath.Join(dir, "proxy.gkaux")
	if aux == nil {
		t.Fatal("missing normal aux control")
	}
	if err := content.SaveImportedWorldChunkAux(auxPath, aux); err != nil {
		t.Fatal(err)
	}
	loader := NewRuntimeContentLoader()
	cache := newStreamedPreparedGeometryCache(8, 1<<30)
	t.Cleanup(func() { loader.Clear(); cache.close(nil) })
	job := streamedSectorProxyLoadJob{Generation: 12, ManifestPath: filepath.Join(dir, "world.gkworld"), LOD: content.ImportedWorldLODDef{ChunkPath: "alpha.gkchunk", ChunkSize: 16, VoxelResolution: .25, PayloadKind: saved.PayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, Aux: content.ImportedWorldChunkAuxRef("proxy.gkaux", aux)}, Loader: loader, PreparedGeometryCache: cache}
	return job, prepareImportedWorldChunkGeometry(dense, aux)
}
func i12AssertProxyGeometry(t *testing.T, p streamedPreparedSectorProxy, want *volume.XBrickMap) {
	t.Helper()
	if p.Err != nil || p.retryCost != 0 || p.Chunk == nil || p.registration == nil {
		t.Fatalf("proxy preparation failed: err=%v retry=%d", p.Err, p.retryCost)
	}
	got := p.registration.geometry
	if got == nil || !reflect.DeepEqual(got.Sectors, want.Sectors) || got.GetVoxelCount() != want.GetVoxelCount() {
		t.Fatal("proxy material/occupancy/normal aux differs from dense oracle")
	}
	min, max := got.ComputeAABB()
	wmin, wmax := want.ComputeAABB()
	if min != wmin || max != wmax {
		t.Fatal("proxy AABB differs")
	}
	if len(got.DirtyBricks) != 0 || len(got.DirtySectors) != 0 || got.StructureDirty {
		t.Fatal("registration geometry is dirty")
	}
}

func TestI12ProxyRLEMetadataGeometryAuxAndCompactCacheReuse(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(map[bool]string{false: "dense prepared", true: "compact prepared"}[compact], func(t *testing.T) {
			job, want := i12ProxyFixture(t)
			job.compactPreparedGeometry = compact
			first := prepareStreamedSectorProxyLoad(job)
			defer first.release()
			i12AssertProxyGeometry(t, first, want)
			if len(first.Chunk.Voxels) != 0 || first.Chunk.NonEmptyVoxelCount != want.GetVoxelCount() || !first.AuxHit || first.AuxMiss {
				t.Fatal("RLE retained dense voxels or lost aux metadata")
			}
			if compact && (first.geometrySource == nil || first.geometrySource.compact == nil) {
				t.Fatal("compact fixture did not prepare a compact source")
			}
			stats := job.Loader.Stats()
			if stats.PinnedBytes == 0 {
				t.Fatal("prepared source has no scope pin")
			}
			second := prepareStreamedSectorProxyLoad(job)
			defer second.release()
			i12AssertProxyGeometry(t, second, want)
			if job.PreparedGeometryCache.snapshot().Misses != 1 || job.PreparedGeometryCache.snapshot().Hits == 0 || job.Loader.Stats().Hits <= stats.Hits {
				t.Fatal("immutable source/geometry did not reuse cache")
			}
			if first.registration.geometry == second.registration.geometry {
				t.Fatal("registration copies alias")
			}
			first.registration.geometry.SetVoxel(0, 0, 0, 1)
			_, value := second.registration.geometry.GetVoxel(0, 0, 0)
			if value != 5 {
				t.Fatal("registration mutation reached another result")
			}
			job.Loader.owner.mu.Lock()
			for key := range job.Loader.owner.entries {
				if key.kind == "imported-chunk" {
					t.Error("successful RLE proxy populated dense chunk cache")
				}
			}
			job.Loader.owner.mu.Unlock()
			first.release()
			if job.Loader.Stats().PinnedBytes == 0 {
				t.Fatal("one result released another source lease")
			}
			second.release()
			if job.Loader.Stats().PinnedBytes != 0 {
				t.Fatal("source scope leaked")
			}
		})
	}
}

func TestI12ProxyFallbackKeepsJSONAndDictionaryCodec(t *testing.T) {
	job, _ := i12ProxyFixture(t)
	dense, err := content.LoadImportedWorldChunk(content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	jsonPath := filepath.Join(t.TempDir(), "proxy.gkchunk")
	if err := content.SaveImportedWorldChunk(jsonPath, dense); err != nil {
		t.Fatal(err)
	}
	job.LOD = content.ImportedWorldLODDef{ChunkPath: jsonPath}
	p := prepareStreamedSectorProxyLoad(job)
	defer p.release()
	if p.Err != nil || p.Chunk == nil || len(p.Chunk.Voxels) == 0 {
		t.Fatalf("JSON fallback failed: %v", p.Err)
	}
	dictPath, codec, _ := c1bDictionaryChunk(t)
	job.Loader = NewRuntimeContentLoader(RuntimeContentLoaderOptions{ImportedWorldCodec: codec})
	t.Cleanup(job.Loader.Clear)
	job.LOD = content.ImportedWorldLODDef{ChunkPath: dictPath}
	p2 := prepareStreamedSectorProxyLoad(job)
	defer p2.release()
	if p2.Err != nil || p2.Chunk == nil || len(p2.Chunk.Voxels) == 0 {
		t.Fatalf("custom codec fallback failed: %v", p2.Err)
	}
	wrong := job
	wrong.Loader = NewRuntimeContentLoader()
	t.Cleanup(wrong.Loader.Clear)
	bad := prepareStreamedSectorProxyLoad(wrong)
	defer bad.release()
	if bad.Err == nil {
		t.Fatal("dictionary codec profile was ignored")
	}
}

func TestI12MalformedRLEHasNoDecodedOrPreparedPublication(t *testing.T) {
	job, _ := i12ProxyFixture(t)
	path := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	p := prepareStreamedSectorProxyLoad(job)
	defer p.release()
	if p.Err == nil || p.Chunk != nil || p.registration != nil || job.Loader.Stats().Entries != 0 || job.PreparedGeometryCache.snapshot().Entries != 0 {
		t.Fatal("corrupt RLE fell back or entered caches")
	}
}

func TestI12PrebuildCreditPrecedesGeometryAndConcurrentDenialRetries(t *testing.T) {
	job, want := i12ProxyFixture(t)
	path := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
	source, err := content.LoadImportedWorldChunkRLESource(path)
	if err != nil {
		t.Fatal(err)
	}
	aux, err := content.LoadImportedWorldChunkAux(content.ResolveDocumentPath(job.LOD.Aux.AuxPath, job.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := streamedRLEProxyPrebuildCharge(source, aux, job.LOD, i12ExactProxyCacheKey(job, aux))
	if err != nil || estimate <= 0 {
		t.Fatalf("invalid prebuild estimate %d %v", estimate, err)
	}
	owner := newStreamedPendingPreparedOwner(estimate)
	job.pendingOwner = owner
	key := streamedImportedWorldGeometryCacheKey("sector_proxy", path, streamedImportedWorldPayloadAndAuxHash(job.LOD.PayloadHash, aux), job.LOD.PayloadSizeBytes)
	block, unblock := s2bBarrier(t)
	started := make(chan struct{})
	leaderDone := make(chan struct{})
	go func() {
		defer close(leaderDone)
		job.PreparedGeometryCache.getOrBuild(key, func() *volume.XBrickMap { close(started); <-block; return want })
	}()
	s2bWait(t, started)
	resultCh := make(chan streamedPreparedSectorProxy, 1)
	go func() { resultCh <- prepareStreamedSectorProxyLoad(job) }()
	s2bUntil(t, func() bool { return job.PreparedGeometryCache.snapshot().BuildWaits > 0 })
	if owner.snapshot().Bytes < estimate {
		t.Fatal("proxy entered geometry builder before reserving retained charge")
	}
	deniedJob := job
	deniedJob.LOD.ChunkPath = "bravo.gkchunk"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content.ResolveDocumentPath(deniedJob.LOD.ChunkPath, job.ManifestPath), raw, 0600); err != nil {
		t.Fatal(err)
	}
	denied := prepareStreamedSectorProxyLoad(deniedJob)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= 0 || denied.Chunk != nil || denied.registration != nil || owner.snapshot().Bytes != estimate || job.PreparedGeometryCache.snapshot().Entries != 0 {
		t.Fatal("nonfitting worker constructed/cache-published instead of tiny retry")
	}
	unblock()
	s2bWait(t, leaderDone)
	first := s2bWait(t, resultCh)
	defer first.release()
	i12AssertProxyGeometry(t, first, want)
	credit := first.pendingCredit
	if credit == nil {
		t.Fatal("prebuild credit was not transferred to result")
	}
	first = admitStreamedPreparedProxy(owner, first)
	if first.pendingCredit != credit {
		t.Fatal("reconciliation reserved a second credit")
	}
	actual := streamedPreparedProxyCharge(first)
	if owner.snapshot().Bytes != actual || actual > estimate {
		t.Fatalf("retained accounting mismatch estimate=%d actual=%d ledger=%+v", estimate, actual, owner.snapshot())
	}
	lower := runtimeContentChargeSum(runtimeContentGraphCharge(source), streamedPendingGeometryCharge(first.PreparedGeometry), first.registration.charge())
	if actual < lower {
		t.Fatalf("reconciliation dropped retained encoded source: charge%d lowerbound%d", actual, lower)
	}
	first.release()
	first.release()
	if owner.snapshot().Bytes != 0 {
		t.Fatal("result release leaked/doubled credit")
	}
	retry := prepareStreamedSectorProxyLoad(deniedJob)
	defer retry.release()
	i12AssertProxyGeometry(t, retry, want)
	retry = admitStreamedPreparedProxy(owner, retry)
	if retry.pendingCredit == nil {
		t.Fatal("capacity return did not admit retry")
	}
	retry.release()
}

func TestI12WarmGeometryStillReservesBeforeRegistrationAndCancellation(t *testing.T) {
	job, _ := i12ProxyFixture(t)
	warm := prepareStreamedSectorProxyLoad(job)
	if warm.Err != nil {
		t.Fatal(warm.Err)
	}
	warm.release()
	source, err := content.LoadImportedWorldChunkRLESource(content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	aux, err := content.LoadImportedWorldChunkAux(content.ResolveDocumentPath(job.LOD.Aux.AuxPath, job.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := streamedRLEProxyPrebuildCharge(source, aux, job.LOD, i12ExactProxyCacheKey(job, aux))
	if err != nil {
		t.Fatal(err)
	}
	owner := newStreamedPendingPreparedOwner(estimate)
	hold, ok := owner.reserve(estimate)
	if !ok {
		t.Fatal("budget control")
	}
	job.pendingOwner = owner
	denied := prepareStreamedSectorProxyLoad(job)
	defer denied.release()
	if denied.retryCost <= 0 || denied.registration != nil || job.PreparedGeometryCache.snapshot().Hits != 0 {
		t.Fatal("warm geometry bypassed pre-registration admission")
	}
	hold.release()
	cancel := make(chan struct{})
	job.prepareCancel = cancel
	p := prepareStreamedSectorProxyLoad(job)
	if p.pendingCredit == nil {
		t.Fatal("successful prebuild not charged")
	}
	close(cancel)
	p = admitStreamedPreparedProxy(owner, p)
	p.release()
	if owner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("cancelled prebuild leaked credit/scope")
	}
	if _, err := streamedRLEProxyPrebuildCharge(nil, nil, job.LOD); err == nil {
		t.Fatal("invalid source estimate accepted")
	}
}

func TestI12PendingCreditResizeIsAtomicAliasSafeAndOverflowSafe(t *testing.T) {
	owner := newStreamedPendingPreparedOwner(100)
	a, ok := owner.reserve(60)
	if !ok {
		t.Fatal("reserve")
	}
	b, ok := owner.reserve(20)
	if !ok {
		t.Fatal("reserve")
	}
	if a.resize(90) || owner.snapshot().Bytes != 80 {
		t.Fatal("nonfitting resize changed ledger")
	}
	if !a.resize(70) || owner.snapshot().Bytes != 90 || !a.resize(10) || owner.snapshot().Bytes != 30 {
		t.Fatal("resize did not reconcile same credit")
	}
	if a.resize(-1) || a.resize(math.MaxInt64) || owner.snapshot().Bytes != 30 {
		t.Fatal("invalid/overflow resize changed ledger")
	}
	alias := a
	a.release()
	alias.release()
	if a.resize(1) || owner.snapshot().Bytes != 20 {
		t.Fatal("released alias resurrected credit")
	}
	b.release()
	solo, ok := owner.reserve(10)
	if !ok || !solo.resize(200) || owner.snapshot().Bytes != 200 || owner.snapshot().OverBudgetBytes != 100 {
		t.Fatal("sole finite oversize resize rejected")
	}
	if owner.snapshot().OversizedAdmissions != 1 || !solo.resize(250) || !solo.resize(80) || !solo.resize(200) || owner.snapshot().OversizedAdmissions != 1 {
		t.Fatal("resizing one credit double-counted oversized admission")
	}
	solo.release()
	if owner.snapshot().Bytes != 0 {
		t.Fatal("oversize resize leaked")
	}
}

func TestI12ProxyBlockedPublicationIsChargedUntilStop(t *testing.T) {
	_, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxPendingPreparedBytes: 1, MaxDecodedContentCacheBytes: 1})
	metadataPins := state.Loader.Stats().PinnedBytes
	for i := 0; i < cap(state.PreparedProxyLoads); i++ {
		state.PreparedProxyLoads <- streamedPreparedSectorProxy{Generation: state.Generation, SectorCoord: ChunkCoord{X: 100 + i}}
	}
	job := buildStreamedSectorProxyLoadJob(state, ChunkCoord{}, state.ImportedWorldSectors[ChunkCoord{}].LODs[0])
	source, err := content.LoadImportedWorldChunkRLESource(content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	estimate, err := streamedRLEProxyPrebuildCharge(source, nil, job.LOD, i12ExactProxyCacheKey(job, nil))
	if err != nil {
		t.Fatal(err)
	}
	startStreamedSectorProxyPrepareJob(state, job)
	s2bUntil(t, func() bool {
		bytes := state.pendingPrepared.snapshot().Bytes
		return bytes > 0 && bytes < estimate && state.PreparedGeometryCache.snapshot().Entries > 0
	})
	if state.pendingPrepared.snapshot().OversizedAdmissions != 1 {
		t.Fatal("reconciliation double-counted sole oversized worker")
	}
	if state.pendingPrepared.snapshot().OverBudgetBytes <= 0 || state.Loader.Stats().PinnedBytes <= metadataPins {
		t.Fatal("blocked proxy publication has no retained ownership")
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	if state.pendingPrepared.snapshot().Bytes != 0 || state.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("Stop leaked blocked proxy credit/scope")
	}
}

func TestI12ScatteredRLEAuxEstimateBoundsDenseAndCompactResults(t *testing.T) {
	for _, compact := range []bool{false, true} {
		t.Run(map[bool]string{false: "dense", true: "compact"}[compact], func(t *testing.T) {
			job, _ := i12ProxyFixture(t)
			cells := [][3]int{{0, 0, 0}, {7, 7, 7}, {8, 0, 0}, {31, 31, 31}, {32, 0, 0}, {0, 32, 0}, {0, 0, 32}, {32, 32, 32}, {63, 63, 63}, {64, 0, 0}, {0, 64, 0}, {0, 0, 64}, {64, 64, 64}}
			chunk := &content.ImportedWorldChunkDef{WorldID: "scattered-rle-i12", ChunkSize: 65, VoxelResolution: .5, NonEmptyVoxelCount: len(cells)}
			for i, p := range cells {
				chunk.Voxels = append(chunk.Voxels, content.ImportedWorldVoxelDef{X: p[0], Y: p[1], Z: p[2], Value: 7, MaterialValue: uint8(5 + i%2)})
			}
			path := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
			saved, err := content.SaveImportedWorldChunkWithOptionsResult(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
			if err != nil {
				t.Fatal(err)
			}
			dense, err := content.LoadImportedWorldChunk(path)
			if err != nil {
				t.Fatal(err)
			}
			aux := derived.BuildImportedWorldChunkAux(dense, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{{}: dense}, saved.PayloadHash, saved.PayloadSizeBytes, true)
			if aux == nil {
				t.Fatal("missing scattered aux")
			}
			auxPath := content.ResolveDocumentPath(job.LOD.Aux.AuxPath, job.ManifestPath)
			if err := content.SaveImportedWorldChunkAux(auxPath, aux); err != nil {
				t.Fatal(err)
			}
			job.LOD.ChunkSize = 65
			job.LOD.VoxelResolution = .5
			job.LOD.PayloadHash = saved.PayloadHash
			job.LOD.PayloadSizeBytes = saved.PayloadSizeBytes
			job.LOD.PayloadKind = saved.PayloadKind
			job.LOD.Aux = content.ImportedWorldChunkAuxRef(job.LOD.Aux.AuxPath, aux)
			job.compactPreparedGeometry = compact
			source, err := content.LoadImportedWorldChunkRLESource(path)
			if err != nil {
				t.Fatal(err)
			}
			estimate, err := streamedRLEProxyPrebuildCharge(source, aux, job.LOD, i12ExactProxyCacheKey(job, aux))
			if err != nil {
				t.Fatal(err)
			}
			result := prepareStreamedSectorProxyLoad(job)
			defer result.release()
			i12AssertProxyGeometry(t, result, prepareImportedWorldChunkGeometry(dense, aux))
			actual := streamedPreparedProxyCharge(result)
			if actual <= 0 || actual > estimate {
				t.Fatalf("scattered brick/sector/halo/aux charge underestimated: estimate%d actual%d", estimate, actual)
			}
		})
	}
}

func TestI12ConcurrentCreditResizeAndReleaseNeverResurrectsStorage(t *testing.T) {
	for iteration := 0; iteration < 64; iteration++ {
		owner := newStreamedPendingPreparedOwner(100)
		credit, ok := owner.reserve(40)
		if !ok {
			t.Fatal("reserve")
		}
		start := make(chan struct{})
		done := make(chan struct{}, 3)
		go func() {
			<-start
			for i := 0; i < 32; i++ {
				credit.resize(int64(10 + i))
			}
			done <- struct{}{}
		}()
		go func() { <-start; credit.release(); done <- struct{}{} }()
		alias := credit
		go func() { <-start; alias.release(); done <- struct{}{} }()
		close(start)
		for i := 0; i < 3; i++ {
			s2bWait(t, done)
		}
		if owner.snapshot().Bytes != 0 || credit.resize(1) {
			t.Fatal("concurrent alias release resurrected or leaked charge")
		}
	}
}

func TestI12MetadataOnlyRLEProxyCommitsPreparedGeometryWithoutRedecode(t *testing.T) {
	app, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{})
	coord := ChunkCoord{}
	state.DesiredProxySectors[coord] = struct{}{}
	state.KeepProxySectors[coord] = struct{}{}
	job := buildStreamedSectorProxyLoadJob(state, coord, state.ImportedWorldSectors[coord].LODs[0])
	job.compactPreparedGeometry = true
	prepared := prepareStreamedSectorProxyLoad(job)
	defer prepared.release()
	if prepared.Err != nil || prepared.Chunk == nil || len(prepared.Chunk.Voxels) != 0 || prepared.Chunk.NonEmptyVoxelCount != 256 {
		t.Fatalf("live RLE metadata control failed: %v", prepared.Err)
	}
	misses := state.Loader.Stats().Misses
	count, err := commitPreparedStreamedSectorProxy(cmd, assets, state, prepared)
	if err != nil || count != 1 {
		t.Fatalf("metadata-only proxy falsely empty: count%d err%v", count, err)
	}
	app.FlushCommands()
	loaded := state.LoadedSectorProxies[coord]
	if loaded == nil || !cmd.EntityExists(loaded.Entity) {
		t.Fatal("proxy entity not published")
	}
	geometry, ok := assets.GetVoxelGeometry(loaded.GeometryAsset.ID)
	if !ok || geometry.XBrickMap == nil || geometry.XBrickMap.GetVoxelCount() != 256 {
		t.Fatal("prepared proxy geometry was lost")
	}
	model, ok := cmd.GetComponent(loaded.Entity, reflect.TypeOf(VoxelModelComponent{})).(*VoxelModelComponent)
	if !ok || model.IsTerrainChunk || model.TerrainChunkSize != 0 || model.VoxelAdjacencyChunkSize != 0 || hasComponentOfType[ColliderComponent](cmd, loaded.Entity) || hasComponentOfType[RigidBodyComponent](cmd, loaded.Entity) {
		t.Fatal("proxy acquired collision/terrain/adjacency ownership")
	}
	if state.Loader.Stats().Misses != misses {
		t.Fatal("proxy commit decoded payload again")
	}
	prepared.release()
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
}

// Include the qualified, resolved key in the tested reservation, not a guessed
// path allowance. This is the same immutable geometry identity used by prepare.
func i12ExactProxyCacheKey(job streamedSectorProxyLoadJob, aux *content.ImportedWorldChunkAuxDef) string {
	return streamedImportedWorldGeometryCacheKey("sector_proxy", content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath), streamedImportedWorldPayloadAndAuxHash(job.LOD.PayloadHash, aux), job.LOD.PayloadSizeBytes)
}
