package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func i12bComposite(t *testing.T) (streamedChunkLoadJob, streamedPreparedChunk) {
	t.Helper()
	proxy, _ := i12ProxyFixture(t)
	dir := filepath.Dir(proxy.ManifestPath)
	imported, err := content.LoadImportedWorldChunk(filepath.Join(dir, proxy.LOD.ChunkPath))
	if err != nil {
		t.Fatal(err)
	}
	aux, err := content.LoadImportedWorldChunkAux(filepath.Join(dir, proxy.LOD.Aux.AuxPath))
	if err != nil {
		t.Fatal(err)
	}
	terrain := &content.TerrainChunkDef{TerrainID: "i12b-terrain", SourceHash: "source", ChunkSize: 16, VoxelResolution: .25, SolidValue: 3, NonEmptyVoxelCount: 49, Columns: []content.TerrainChunkColumnDef{{X: -1, Z: -8, FilledVoxels: 17}, {X: 8, Z: 0, FilledVoxels: 9}, {X: -1, Z: -8, FilledVoxels: 23}, {X: 0, Z: 0, FilledVoxels: 0}}}
	if err := content.SaveTerrainChunk(filepath.Join(dir, "terrain.gkterrainchunk"), terrain); err != nil {
		t.Fatal(err)
	}
	snapshot := &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{{X: -1, Y: -8, Z: -32, Value: 2}, {X: 32, Y: 8, Z: 0, Value: 4}, {X: -1, Y: -8, Z: -32, Value: 6}, {X: 64, Value: 0}}}
	if err := content.SaveVoxelObjectSnapshot(filepath.Join(dir, "object.gkvoxobj"), snapshot); err != nil {
		t.Fatal(err)
	}
	key := voxelObjectRuntimeKey("placement", "body")
	entry := &content.ImportedWorldChunkEntryDef{ChunkPath: proxy.LOD.ChunkPath, NonEmptyVoxelCount: imported.NonEmptyVoxelCount, PayloadKind: proxy.LOD.PayloadKind, PayloadHash: proxy.LOD.PayloadHash, PayloadSizeBytes: proxy.LOD.PayloadSizeBytes, Aux: proxy.LOD.Aux}
	job := streamedChunkLoadJob{Generation: 81, LevelPath: filepath.Join(dir, "level.gklevel"), WorldDeltaPath: filepath.Join(dir, "delta.gkworlddelta"), Loader: proxy.Loader, PreparedGeometryCache: proxy.PreparedGeometryCache, ImportedWorldManifestPath: proxy.ManifestPath, ImportedWorldEntry: entry, TerrainManifestPath: filepath.Join(dir, "terrain.gkterrainmanifest"), TerrainEntry: &content.TerrainChunkEntryDef{ChunkPath: "terrain.gkterrainchunk", NonEmptyVoxelCount: 49}, VoxelOverrides: map[string]content.VoxelObjectOverrideDef{key: {PlacementID: "placement", ItemID: "body", SnapshotPath: "object.gkvoxobj"}}}
	resolved := streamedPreparedChunk{Generation: job.Generation, Coord: job.Coord, TerrainChunk: terrain, ImportedWorldChunk: imported, ImportedWorldAux: aux, ImportedWorldAuxHit: true, ObjectSnapshots: map[string]*content.VoxelObjectSnapshotDef{key: snapshot}, PreparedImportedWorldGeometryCacheKey: streamedImportedWorldGeometryCacheKey("imported_full", filepath.Join(dir, entry.ChunkPath), streamedImportedWorldPayloadAndAuxHash(entry.PayloadHash, aux), entry.PayloadSizeBytes)}
	return job, resolved
}

func i12bMapEqual(t *testing.T, actual, want *volume.XBrickMap, clean ...bool) {
	t.Helper()
	if actual == nil || want == nil || !reflect.DeepEqual(actual.Sectors, want.Sectors) || actual.GetVoxelCount() != want.GetVoxelCount() {
		t.Fatal("final map changed ordered occupancy/material/aux")
	}
	amin, amax := actual.ComputeAABB()
	wmin, wmax := want.ComputeAABB()
	if amin != wmin || amax != wmax || ((len(clean) == 0 || clean[0]) && (len(actual.DirtySectors) != 0 || len(actual.DirtyBricks) != 0 || actual.StructureDirty)) {
		t.Fatal("final map bounds/clean state changed")
	}
}
func i12bCompositeGeometry(t *testing.T, p streamedPreparedChunk, resolved streamedPreparedChunk, job streamedChunkLoadJob) {
	t.Helper()
	if p.Err != nil || p.retryCost != 0 || p.ImportedWorldChunk == nil || len(p.ImportedWorldChunk.Voxels) != len(resolved.ImportedWorldChunk.Voxels) {
		t.Fatalf("dense full payload lost: %v retry%d", p.Err, p.retryCost)
	}
	imported := p.PreparedImportedWorldGeometry
	if p.geometrySource != nil {
		imported = p.geometrySource.materialize()
	}
	i12bMapEqual(t, imported, prepareImportedWorldChunkGeometry(resolved.ImportedWorldChunk, resolved.ImportedWorldAux))
	if job.HasImportedWorldBacking {
		if p.registration != nil || p.geometrySource != nil {
			t.Fatal("backed full payload acquired immutable registration")
		}
	} else {
		if p.registration == nil || p.registration.geometry == imported {
			t.Fatal("full registration absent/aliased source")
		}
		i12bMapEqual(t, p.registration.geometry, imported)
	}
	i12bMapEqual(t, p.preparedTerrainGeometry, terrainChunkToXBrickMapClean(resolved.TerrainChunk))
	if p.terrainRegistration == nil || p.terrainRegistration.geometry == p.preparedTerrainGeometry {
		t.Fatal("terrain registration absent/aliased")
	}
	i12bMapEqual(t, p.terrainRegistration.geometry, p.preparedTerrainGeometry)
	if (p.terrainRegistration.rendererCopy != nil) != job.renderManaged {
		t.Fatal("managed third terrain copy eligibility changed")
	}
	if job.renderManaged {
		i12bMapEqual(t, p.terrainRegistration.rendererCopy, p.preparedTerrainGeometry, false)
		if !p.terrainRegistration.rendererCopy.StructureDirty {
			t.Fatal("renderer copy lost fresh structural upload work")
		}
	}
	for key, snapshot := range resolved.ObjectSnapshots {
		packet := p.objectSnapshotGeometry[key]
		if packet == nil || packet.registration == nil || packet.registration.geometry == packet.source {
			t.Fatal("snapshot registration absent/aliased")
		}
		want := XBrickMapFromVoxelObjectSnapshot(snapshot)
		want.ComputeAABB()
		want.ClearDirty()
		i12bMapEqual(t, packet.source, want)
		i12bMapEqual(t, packet.registration.geometry, want)
	}
}
func terrainChunkToXBrickMapClean(chunk *content.TerrainChunkDef) *volume.XBrickMap {
	x := terrainChunkToXBrickMap(chunk)
	x.ComputeAABB()
	x.ClearDirty()
	return x
}

func TestI12bCompositeBoundsAndDenseFullGeometryRemainCompatible(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, backed := range []bool{false, true} {
			for _, managed := range []bool{false, true} {
				t.Run(map[bool]string{false: "dense", true: "compact"}[compact]+map[bool]string{false: "/ordinary", true: "/backed"}[backed]+map[bool]string{false: "/legacy", true: "/managed"}[managed], func(t *testing.T) {
					job, resolved := i12bComposite(t)
					job.compactPreparedGeometry = compact
					job.HasImportedWorldBacking = backed
					job.renderManaged = managed
					estimate, err := streamedChunkPrebuildCharge(resolved, job)
					if err != nil || estimate <= 0 {
						t.Fatalf("composite estimate: %d %v", estimate, err)
					}
					owner := newStreamedPendingPreparedOwner(estimate)
					job.pendingOwner = owner
					p := prepareStreamedChunkLoad(job)
					defer p.release()
					i12bCompositeGeometry(t, p, resolved, job)
					if p.pendingCredit == nil || owner.snapshot().Bytes != estimate || streamedPreparedChunkCharge(p) > estimate {
						t.Fatalf("composite source/copies/descriptor/aux envelope exceeds prebuild credit: held%d estimate%d actual%d", owner.snapshot().Bytes, estimate, streamedPreparedChunkCharge(p))
					}
					credit := p.pendingCredit
					p = admitStreamedPreparedChunk(owner, p)
					if p.pendingCredit != credit || owner.snapshot().Bytes != streamedPreparedChunkCharge(p) {
						t.Fatal("full actual reconciliation replaced credit or double reserved")
					}
					p.release()
					p.release()
					if owner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 {
						t.Fatal("full result released credit/scope incorrectly")
					}
					// Nil-owner synchronous construction retains the same ordinary API behavior.
					job.pendingOwner = nil
					q := prepareStreamedChunkLoad(job)
					defer q.release()
					i12bCompositeGeometry(t, q, resolved, job)
				})
			}
		}
	}
}

func TestI12bFullCreditPrecedesImportedCacheBuildAndSharesProxyBudget(t *testing.T) {
	job, resolved := i12bComposite(t)
	estimate, err := streamedChunkPrebuildCharge(resolved, job)
	if err != nil {
		t.Fatal(err)
	}
	owner := newStreamedPendingPreparedOwner(estimate)
	job.pendingOwner = owner
	held, release := s2bBarrier(t)
	started, leaderDone := make(chan struct{}), make(chan struct{})
	want := prepareImportedWorldChunkGeometry(resolved.ImportedWorldChunk, resolved.ImportedWorldAux)
	go func() {
		defer close(leaderDone)
		job.PreparedGeometryCache.getOrBuild(resolved.PreparedImportedWorldGeometryCacheKey, func() *volume.XBrickMap { close(started); <-held; return want })
	}()
	s2bWait(t, started)
	done := make(chan streamedPreparedChunk, 1)
	go func() { done <- prepareStreamedChunkLoad(job) }()
	s2bUntil(t, func() bool { return job.PreparedGeometryCache.snapshot().BuildWaits > 0 })
	if owner.snapshot().Bytes != estimate {
		t.Fatal("full worker reached final geometry before composite credit")
	}
	second := job
	entry := *job.ImportedWorldEntry
	entry.ChunkPath = "other.gkchunk"
	second.ImportedWorldEntry = &entry
	bytes, err := os.ReadFile(content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(content.ResolveImportedWorldChunkPath(entry, job.ImportedWorldManifestPath), bytes, 0600); err != nil {
		t.Fatal(err)
	}
	denied := prepareStreamedChunkLoad(second)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= 0 || denied.ImportedWorldChunk != nil || denied.TerrainChunk != nil || len(denied.ObjectSnapshots) != 0 || denied.registration != nil || denied.pendingCredit != nil || job.PreparedGeometryCache.snapshot().Entries != 0 || owner.snapshot().Bytes != estimate {
		t.Fatal("denied full retained payload or entered final geometry")
	}
	denied = admitStreamedPreparedChunk(owner, denied)
	if denied.retryCost == 0 || owner.snapshot().Bytes != estimate {
		t.Fatal("tiny full retry was readmitted as empty success")
	}
	proxy := streamedSectorProxyLoadJob{Generation: job.Generation, ManifestPath: job.ImportedWorldManifestPath, LOD: content.ImportedWorldLODDef{ChunkPath: entry.ChunkPath, PayloadHash: entry.PayloadHash, PayloadSizeBytes: entry.PayloadSizeBytes}, Loader: job.Loader, PreparedGeometryCache: job.PreparedGeometryCache, pendingOwner: owner}
	pd := prepareStreamedSectorProxyLoad(proxy)
	defer pd.release()
	if pd.retryCost <= 0 || pd.registration != nil || owner.snapshot().Bytes != estimate {
		t.Fatal("full and proxy used separate pending owners")
	}
	release()
	s2bWait(t, leaderDone)
	first := s2bWait(t, done)
	defer first.release()
	i12bCompositeGeometry(t, first, resolved, job)
	first = admitStreamedPreparedChunk(owner, first)
	first.release()
	retry := prepareStreamedChunkLoad(second)
	defer retry.release()
	i12bCompositeGeometry(t, retry, resolved, second)
	if retry.pendingCredit == nil {
		t.Fatal("released capacity did not retry full construction")
	}
}

func TestI12bWarmLargerFullCachePreflightPreservesAllCompositeCosts(t *testing.T) {
	for _, shape := range []struct {
		name                         string
		compact, wantCompact, backed bool
	}{{"dense", false, false, false}, {"compact", true, true, false}, {"compact-to-dense", true, false, false}, {"compact-backed", true, true, true}} {
		t.Run(shape.name, func(t *testing.T) {
			job, resolved := i12bComposite(t)
			job.compactPreparedGeometry = shape.wantCompact
			job.HasImportedWorldBacking = shape.backed
			job.renderManaged = true
			// Input metadata changes independently of the geometry cache identity.
			small := &content.ImportedWorldChunkDef{SchemaVersion: 1, WorldID: "small", ChunkSize: 1, VoxelResolution: 1, NonEmptyVoxelCount: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 7}}}
			path := content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath)
			if err := content.SaveImportedWorldChunk(path, small); err != nil {
				t.Fatal(err)
			}
			job.ImportedWorldEntry.Aux = nil
			loadedSmall, loadErr := content.LoadImportedWorldChunk(path)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			resolved.ImportedWorldChunk = loadedSmall
			resolved.ImportedWorldAux = nil
			resolved.ImportedWorldAuxHit = false
			resolved.ImportedWorldAuxMiss = true
			resolved.PreparedImportedWorldGeometryCacheKey = streamedImportedWorldGeometryCacheKey("imported_full", path, job.ImportedWorldEntry.PayloadHash, job.ImportedWorldEntry.PayloadSizeBytes)
			estimate, err := streamedChunkPrebuildCharge(resolved, job)
			if err != nil {
				t.Fatal(err)
			}
			source, promotion := i12bSeedWarm(t, job.PreparedGeometryCache, resolved.PreparedImportedWorldGeometryCacheKey, shape.compact)
			owner := newStreamedPendingPreparedOwner(estimate + 1)
			hold, ok := owner.reserve(1)
			if !ok {
				t.Fatal("hold")
			}
			defer hold.release()
			job.pendingOwner = owner
			denied := prepareStreamedChunkLoad(job)
			defer denied.release()
			if denied.Err != nil || denied.retryCost <= estimate || denied.ImportedWorldChunk != nil || denied.TerrainChunk != nil || len(denied.ObjectSnapshots) != 0 || denied.registration != nil || denied.terrainRegistration != nil || len(denied.objectSnapshotGeometry) != 0 || denied.pendingCredit != nil {
				t.Fatalf("warm larger source escaped composite growth admission: input%d retry%d err%v", estimate, denied.retryCost, denied.Err)
			}
			hint := denied.retryCost
			if owner.snapshot().Bytes != 1 || job.Loader.Stats().PinnedBytes != 0 || source.exposed.Load() || promotion.dense != nil {
				t.Fatal("denied warm full promoted/exposed cache or leaked payload ownership")
			}
			captured, _, hit := job.PreparedGeometryCache.getOrBuildSourceRequest(resolved.PreparedImportedWorldGeometryCacheKey, true, nil)
			if !hit || captured != source {
				t.Fatal("grow denial invalidated warm source")
			}
			hold.release()
			retry := prepareStreamedChunkLoad(job)
			defer retry.release()
			if retry.Err != nil || retry.retryCost != 0 || retry.ImportedWorldChunk == nil || len(retry.ImportedWorldChunk.Voxels) != 1 || retry.terrainRegistration == nil || len(retry.objectSnapshotGeometry) != 1 {
				t.Fatal("retry dropped dense payload or unrelated geometry components")
			}
			geometry := retry.PreparedImportedWorldGeometry
			if retry.geometrySource != nil {
				geometry = retry.geometrySource.materialize()
			}
			if geometry == nil || geometry.GetVoxelCount() != 64 {
				t.Fatal("retry did not retain actual captured larger cache source")
			}
			if actual := streamedPreparedChunkCharge(retry); actual > hint {
				t.Fatalf("grow hint dropped composite components: actual%d hint%d", actual, hint)
			}
			credit := retry.pendingCredit
			retry = admitStreamedPreparedChunk(owner, retry)
			if credit == nil || credit != retry.pendingCredit || owner.snapshot().Bytes != streamedPreparedChunkCharge(retry) || owner.snapshot().OversizedAdmissions != 1 {
				t.Fatal("warm full did not reconcile same oversized credit once")
			}
			retry.release()
			if owner.snapshot().Bytes != 0 {
				t.Fatal("grown full credit leaked")
			}
		})
	}
}

func TestI12bLegacyRecordBoundsIgnoreHintsAndCapHugeSparseExtents(t *testing.T) {
	aux := &content.ImportedWorldChunkAuxDef{Records: []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: make([]byte, volume.VoxelAuxRecordBytes)}, {Origin: [3]int{}, Bytes: make([]byte, volume.VoxelAuxRecordBytes)}}}
	writes := []content.ImportedWorldVoxelDef{{X: -1, Value: 2}, {X: -8, Value: 4}, {X: -32, Value: 5}, {X: 0, Value: 3, MaterialValue: 6}, {X: -1, Value: 7}, {X: 1 << 60, Y: 1 << 60, Z: 1 << 60, Value: 8}, {X: -(1 << 60), Y: -(1 << 60), Z: -(1 << 60), Value: 9}, {X: 12, Value: 0}}
	for _, hint := range []int{-3, 0, 1, int(^uint(0) >> 1)} {
		for _, side := range []int{-4, 0, 1} {
			p := streamedPreparedChunk{ImportedWorldChunk: &content.ImportedWorldChunkDef{ChunkSize: side, NonEmptyVoxelCount: hint, Voxels: writes, EmbeddedAux: aux}, ImportedWorldAux: &content.ImportedWorldChunkAuxDef{Records: []content.ImportedWorldBrickAuxDef{{Bytes: make([]byte, volume.VoxelAuxRecordBytes)}}}, PreparedImportedWorldGeometryCacheKey: string(make([]byte, 2048))}
			snapshot := &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{{X: -1, Value: 2}, {X: -8, Value: 3}, {X: -32, Value: 4}, {X: 1 << 60, Y: 1 << 60, Z: 1 << 60, Value: 5}, {X: -(1 << 60), Y: -(1 << 60), Z: -(1 << 60), Value: 6}, {X: -1, Value: 7}, {Value: 0}}}
			p.ObjectSnapshots = map[string]*content.VoxelObjectSnapshotDef{"legacy": snapshot}
			estimate, err := streamedChunkPrebuildCharge(p, streamedChunkLoadJob{})
			if err != nil {
				t.Fatalf("legacy sparse records rejected: side%d hint%d %v", side, hint, err)
			}
			p.PreparedImportedWorldGeometry = prepareImportedWorldChunkGeometry(p.ImportedWorldChunk, p.ImportedWorldAux)
			p.registration = prepareCachedStreamedGeometryRegistration(p.PreparedImportedWorldGeometry)
			geometry := XBrickMapFromVoxelObjectSnapshot(snapshot)
			geometry.ComputeAABB()
			geometry.ClearDirty()
			p.objectSnapshotGeometry = map[string]*streamedObjectSnapshotGeometry{"legacy": {snapshot: snapshot, source: geometry, registration: prepareStreamedGeometryRegistration(geometry)}}
			if actual := streamedPreparedChunkCharge(p); actual > estimate {
				t.Fatalf("actual emitted duplicate/negative/out-of-grid records underbounded: %d > %d", actual, estimate)
			}
			if (p.PreparedImportedWorldGeometry == nil) != (hint == 0) {
				t.Fatal("legacy zero-only imported build guard changed")
			}
			p.release()
		}
	}
}

func TestI12bTallTerrainOverflowFailsBeforeGeometryAndIgnoredColumnsStayCheap(t *testing.T) {
	job, resolved := i12bComposite(t)
	huge := &content.TerrainChunkDef{ChunkSize: 1, SolidValue: 3, NonEmptyVoxelCount: 1, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: int(^uint(0) >> 1)}}}
	resolved.TerrainChunk = huge
	if _, err := streamedChunkPrebuildCharge(resolved, job); err == nil {
		t.Fatal("unrepresentable tall-column storage admitted as saturated sole oversized credit")
	}
	if err := content.SaveTerrainChunk(content.ResolveTerrainChunkPath(*job.TerrainEntry, job.TerrainManifestPath), huge); err != nil {
		t.Fatal(err)
	}
	job.pendingOwner = newStreamedPendingPreparedOwner(1)
	result := prepareStreamedChunkLoad(job)
	defer result.release()
	if result.Err == nil || result.TerrainChunk != nil || result.ImportedWorldChunk != nil || result.pendingCredit != nil || job.PreparedGeometryCache.snapshot().Entries != 0 || job.pendingOwner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("overflow reached geometry construction or retained payload")
	}
	// Direct payload ownership deliberately bypasses file defaults: a zero solid
	// value and nonpositive heights emit no writes, regardless of height magnitude.
	for _, solid := range []uint8{0, 3} {
		columns := []content.TerrainChunkColumnDef{{FilledVoxels: 0}, {X: -1, FilledVoxels: -1}}
		if solid == 0 {
			columns = append(columns, content.TerrainChunkColumnDef{FilledVoxels: int(^uint(0) >> 1)})
		}
		p := streamedPreparedChunk{TerrainChunk: &content.TerrainChunkDef{SolidValue: solid, NonEmptyVoxelCount: 1, Columns: columns}}
		estimate, err := streamedChunkPrebuildCharge(p, streamedChunkLoadJob{renderManaged: true})
		if err != nil {
			t.Fatalf("ignored column generated overflow: %v", err)
		}
		p.preparedTerrainGeometry = terrainChunkToXBrickMapClean(p.TerrainChunk)
		p.terrainRegistration = prepareStreamedGeometryRegistration(p.preparedTerrainGeometry)
		p.terrainRegistration.prepareRendererCopy()
		if p.preparedTerrainGeometry.GetVoxelCount() != 0 || streamedPreparedChunkCharge(p) > estimate {
			t.Fatal("empty terrain map/copies not bounded")
		}
		p.release()
	}
}

func TestI12bPayloadFailureAndCancelledCacheWaitReleaseCompositeOwnership(t *testing.T) {
	t.Run("late-payload-error", func(t *testing.T) {
		job, _ := i12bComposite(t)
		job.pendingOwner = newStreamedPendingPreparedOwner(1)
		job.VoxelOverrides = map[string]content.VoxelObjectOverrideDef{"missing": {SnapshotPath: "missing.gkvoxobj"}}
		result := prepareStreamedChunkLoad(job)
		defer result.release()
		if result.Err == nil || result.ImportedWorldChunk != nil || result.TerrainChunk != nil || result.pendingCredit != nil || job.pendingOwner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 || job.PreparedGeometryCache.snapshot().Entries != 0 {
			t.Fatal("late payload failure built geometry or leaked decoded ownership")
		}
	})
	t.Run("cancel-shared-cache-wait", func(t *testing.T) {
		job, resolved := i12bComposite(t)
		estimate, err := streamedChunkPrebuildCharge(resolved, job)
		if err != nil {
			t.Fatal(err)
		}
		job.pendingOwner = newStreamedPendingPreparedOwner(estimate)
		cancel := make(chan struct{})
		job.prepareCancel = cancel
		held, release := s2bBarrier(t)
		started, leaderDone := make(chan struct{}), make(chan struct{})
		want := prepareImportedWorldChunkGeometry(resolved.ImportedWorldChunk, resolved.ImportedWorldAux)
		go func() {
			defer close(leaderDone)
			job.PreparedGeometryCache.getOrBuild(resolved.PreparedImportedWorldGeometryCacheKey, func() *volume.XBrickMap { close(started); <-held; return want })
		}()
		s2bWait(t, started)
		done := make(chan streamedPreparedChunk, 1)
		go func() { done <- prepareStreamedChunkLoad(job) }()
		s2bUntil(t, func() bool { return job.PreparedGeometryCache.snapshot().BuildWaits > 0 })
		if job.pendingOwner.snapshot().Bytes != estimate {
			t.Fatal("cache waiter not precharged")
		}
		close(cancel)
		release()
		s2bWait(t, leaderDone)
		result := s2bWait(t, done)
		defer result.release()
		if result.Err != nil || result.prepareCancel != cancel || !streamedPreparationCancelled(result.prepareCancel) || result.retryCost != 0 || result.pendingCredit != nil || result.ImportedWorldChunk != nil || result.TerrainChunk != nil || len(result.ObjectSnapshots) != 0 || result.registration != nil || result.terrainRegistration != nil || job.pendingOwner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 {
			t.Fatal("cancelled waiter retained composite credit/scope")
		}
		job.prepareCancel = nil
		job.pendingOwner = nil
		fresh := prepareStreamedChunkLoad(job)
		defer fresh.release()
		i12bCompositeGeometry(t, fresh, resolved, job)
		if job.PreparedGeometryCache.snapshot().Hits == 0 {
			t.Fatal("cancelled waiter invalidated completed shared geometry")
		}
	})
}

func TestI12bCompiledAndV2ResolvedCostsIncludedAndDeniedPacketsReleased(t *testing.T) {
	f, part, _, delta := c3d4aRuntime(t)
	placement := s1gID(0, 0)
	e2b2Save(t, f, delta, e2b2Payload(t, part, placement, "body", content.VoxelObjectPayloadBaseDelta, 7, false))
	job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{})
	owner := newStreamedPendingPreparedOwner(1)
	job.pendingOwner = owner
	before := job.Loader.Stats().PinnedBytes
	result := prepareStreamedChunkLoad(job)
	defer result.release()
	if result.Err != nil || result.retryCost != 0 || len(result.compiledAssets) != 1 || len(result.ObjectSnapshots) != 1 || !result.v2Placements[placement] || len(result.objectSnapshotGeometry) != 1 {
		t.Fatalf("completed compiled packet/v2 resolution lost: %v", result.Err)
	}
	resolved := result
	resolved.objectSnapshotGeometry = nil
	resolved.pendingCredit = nil
	resolved.loadScope = nil
	estimate, err := streamedChunkPrebuildCharge(resolved, job)
	if err != nil {
		t.Fatal(err)
	}
	if estimate < streamedCompiledAssetPacketsCharge(result.compiledAssets) || owner.snapshot().Bytes != estimate || streamedPreparedChunkCharge(result) > estimate {
		t.Fatal("completed packet charges omitted from admitted composite bound")
	}
	credit := result.pendingCredit
	result = admitStreamedPreparedChunk(owner, result)
	if result.pendingCredit != credit || owner.snapshot().OversizedAdmissions != 1 {
		t.Fatal("compiled full reconciled a second oversized admission")
	}
	result.release()
	if owner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != before {
		t.Fatal("completed packet/source scope not released")
	}
	hold, ok := owner.reserve(1)
	if !ok {
		t.Fatal("hold")
	}
	defer hold.release()
	denied := prepareStreamedChunkLoad(job)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= 0 || len(denied.compiledAssets) != 0 || len(denied.ObjectSnapshots) != 0 || len(denied.objectSnapshotGeometry) != 0 || job.Loader.Stats().PinnedBytes != before || owner.snapshot().Bytes != 1 {
		t.Fatal("denied completed packet/v2 payload was retained")
	}
}

func TestI12bAsyncBlockedCompositePublicationRetainsCreditUntilStop(t *testing.T) {
	_, cmd, state, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxPendingPreparedBytes: 1, MaxDecodedContentCacheBytes: 1})
	job, _ := i12bComposite(t)
	job.Generation = state.Generation
	job.Loader = state.Loader
	job.PreparedGeometryCache = state.PreparedGeometryCache
	job.renderManaged = true
	before := state.Loader.Stats().PinnedBytes
	for i := 0; i < cap(state.PreparedLoads); i++ {
		state.PreparedLoads <- streamedPreparedChunk{Generation: state.Generation, Coord: ChunkCoord{X: 100 + i}}
	}
	startStreamedChunkPrepareJob(state, job)
	s2bUntil(t, func() bool {
		return state.pendingPrepared.snapshot().Bytes > 0 && state.PreparedGeometryCache.snapshot().Entries > 0
	})
	if state.pendingPrepared.snapshot().OversizedAdmissions != 1 || state.Loader.Stats().PinnedBytes <= before {
		t.Fatal("blocked composite worker has no shared byte/source ownership")
	}
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		t.Fatal(err)
	}
	if state.pendingPrepared.snapshot().Bytes != 0 || state.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("Stop leaked composite credit/scope")
	}
}

func TestI12bFullCommitRetainsDenseCollisionAndBackingContracts(t *testing.T) {
	for _, backed := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "backed"}[backed], func(t *testing.T) {
			app, cmd, state, assets := s2bStartWorld(t, StreamedLevelRuntimeConfig{MaxPendingPreparedBytes: 1})
			if !backed {
				state.BaseWorldBacking = nil
			}
			coord := ChunkCoord{}
			state.DesiredChunks[coord] = struct{}{}
			state.KeepChunks[coord] = struct{}{}
			state.PendingLoads[coord] = struct{}{}
			state.CollisionChunks[coord] = struct{}{}
			state.DestructionChunks[coord] = struct{}{}
			job := buildStreamedChunkLoadJob(state, coord)
			job.HasImportedWorldBacking = backed
			startStreamedChunkPrepareJob(state, job)
			s2bUntil(t, func() bool { return len(state.PreparedLoads) == 1 })
			state.jobs.Wait()
			result := <-state.PreparedLoads
			if result.Err != nil || result.ImportedWorldChunk == nil || len(result.ImportedWorldChunk.Voxels) == 0 || result.pendingCredit == nil {
				t.Fatal("full worker substituted metadata-only proxy payload")
			}
			state.PreparedLoads <- result
			commitPreparedStreamedChunksSystem(cmd, assets, state)
			app.FlushCommands()
			loaded := state.LoadedChunks[coord]
			if loaded == nil || len(loaded.ImportedWorldEntities) != 1 || state.pendingPrepared.snapshot().Bytes != 0 {
				t.Fatal("full commit lost entities or credit")
			}
			for entity := range loaded.ImportedWorldEntities {
				model := mustVoxelModelComponentForLevelTest(t, cmd, entity)
				geometry, ok := ResolveVoxelGeometryMap(assets, &model)
				if !ok || geometry == nil || geometry.GetVoxelCount() == 0 || !hasComponentOfType[ColliderComponent](cmd, entity) {
					t.Fatal("full geometry/collision was lost")
				}
				if backed && !hasComponentOfType[VoxelBackingComponent](cmd, entity) {
					t.Fatal("backed full commit lost backing owner")
				}
			}
			if err := StopStreamedLevelRuntime(cmd); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestI12bOrdinaryWarmFullStillPreflightsBeforeRegistration(t *testing.T) {
	job, resolved := i12bComposite(t)
	job.compactPreparedGeometry = true
	job.renderManaged = true
	warm := prepareStreamedChunkLoad(job)
	if warm.Err != nil {
		t.Fatal(warm.Err)
	}
	warm.release()
	hits := job.PreparedGeometryCache.snapshot().Hits
	estimate, err := streamedChunkPrebuildCharge(resolved, job)
	if err != nil {
		t.Fatal(err)
	}
	owner := newStreamedPendingPreparedOwner(estimate)
	hold, ok := owner.reserve(estimate)
	if !ok {
		t.Fatal("hold")
	}
	defer hold.release()
	job.pendingOwner = owner
	denied := prepareStreamedChunkLoad(job)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= 0 || denied.registration != nil || denied.terrainRegistration != nil || len(denied.objectSnapshotGeometry) != 0 || denied.ImportedWorldChunk != nil || job.PreparedGeometryCache.snapshot().Hits != hits || owner.snapshot().Bytes != estimate || job.Loader.Stats().PinnedBytes != 0 {
		t.Fatal("warm full bypassed initial composite admission")
	}
	hold.release()
	retry := prepareStreamedChunkLoad(job)
	defer retry.release()
	i12bCompositeGeometry(t, retry, resolved, job)
	if retry.pendingCredit == nil {
		t.Fatal("warm retry did not retain prebuild credit")
	}
}

func TestI12bFullDenseJSONRLEAndCustomC1ProfilesRemainDistinctFromProxySource(t *testing.T) {
	for _, kind := range []string{"RLE", "JSON", "C1"} {
		t.Run(kind, func(t *testing.T) {
			job, _ := i12bComposite(t)
			job.TerrainEntry = nil
			job.VoxelOverrides = nil
			job.ImportedWorldEntry.Aux = nil
			path := content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath)
			if kind == "JSON" {
				chunk, err := content.LoadImportedWorldChunk(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := content.SaveImportedWorldChunk(path, chunk); err != nil {
					t.Fatal(err)
				}
			}
			var expected *content.ImportedWorldChunkDef
			if kind == "C1" {
				customPath, codec, chunk := c1bDictionaryChunk(t)
				path = customPath
				expected = chunk
				job.Loader = NewRuntimeContentLoader(RuntimeContentLoaderOptions{ImportedWorldCodec: codec})
				t.Cleanup(job.Loader.Clear)
			} else {
				var err error
				expected, err = content.LoadImportedWorldChunk(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			job.ImportedWorldEntry = &content.ImportedWorldChunkEntryDef{ChunkPath: path, NonEmptyVoxelCount: expected.NonEmptyVoxelCount}
			job.pendingOwner = newStreamedPendingPreparedOwner(1)
			result := prepareStreamedChunkLoad(job)
			defer result.release()
			if result.Err != nil || result.pendingCredit == nil || result.ImportedWorldChunk == nil || !reflect.DeepEqual(result.ImportedWorldChunk.Voxels, expected.Voxels) || result.registration == nil {
				t.Fatalf("dense full %s profile changed: %v", kind, result.Err)
			}
			i12bMapEqual(t, result.registration.geometry, prepareImportedWorldChunkGeometry(expected))
			job.Loader.owner.mu.Lock()
			for key := range job.Loader.owner.entries {
				if key.kind == "imported-chunk-rle" {
					job.Loader.owner.mu.Unlock()
					t.Fatal("full job substituted metadata-only RLE cache kind")
				}
			}
			job.Loader.owner.mu.Unlock()
			if kind == "C1" {
				wrong := job
				wrong.Loader = NewRuntimeContentLoader()
				t.Cleanup(wrong.Loader.Clear)
				wrong.pendingOwner = newStreamedPendingPreparedOwner(1)
				bad := prepareStreamedChunkLoad(wrong)
				defer bad.release()
				if bad.Err == nil || bad.ImportedWorldChunk != nil || wrong.pendingOwner.snapshot().Bytes != 0 {
					t.Fatal("full C1 ignored custom profile or retained failed payload")
				}
			}
		})
	}
}

func TestI12bRealTerrainAndImportedOverridesRetainPriorityAndExactGeometryIdentity(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		for _, compact := range []bool{false, true} {
			t.Run(map[bool]string{false: "RLE-no-aux", true: "C1-embedded-aux"}[embedded]+map[bool]string{false: "/dense", true: "/compact"}[compact], func(t *testing.T) {
				job, resolved := i12bComposite(t)
				job.compactPreparedGeometry = compact
				job.renderManaged = true
				dir := filepath.Dir(job.WorldDeltaPath)
				imported := *resolved.ImportedWorldChunk
				importedPath := filepath.Join(dir, "edited.gkchunk")
				if embedded {
					hash, size, err := content.ImportedWorldChunkCompiledGeometryIdentity(&imported, nil)
					if err != nil {
						t.Fatal(err)
					}
					aux := *resolved.ImportedWorldAux
					aux.SourcePayloadHash = hash
					aux.SourcePayloadSizeBytes = size
					if _, err := content.SaveImportedWorldChunkCompiledWithAux(importedPath, &imported, &aux, nil); err != nil {
						t.Fatal(err)
					}
				} else if err := content.SaveImportedWorldChunkWithOptions(importedPath, &imported, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
					t.Fatal(err)
				}
				loaded, err := content.LoadImportedWorldChunk(importedPath)
				if err != nil {
					t.Fatal(err)
				}
				if (loaded.EmbeddedAux != nil) != embedded || len(loaded.Voxels) == 0 {
					t.Fatal("override fixture lost actual dense/embedded payload")
				}
				terrain := *resolved.TerrainChunk
				terrain.SolidValue = 6
				terrain.SourceHash = "edited-source"
				terrain.NonEmptyVoxelCount = 33
				terrain.Columns = []content.TerrainChunkColumnDef{{X: -8, Z: -1, FilledVoxels: 24}, {X: 9, Z: 3, FilledVoxels: 9}}
				if err := content.SaveTerrainChunk(filepath.Join(dir, "edited.gkterrainchunk"), &terrain); err != nil {
					t.Fatal(err)
				}
				loadedTerrain, err := content.LoadTerrainChunk(filepath.Join(dir, "edited.gkterrainchunk"))
				if err != nil {
					t.Fatal(err)
				}
				job.TerrainOverride = &content.TerrainChunkOverrideDef{TerrainID: loadedTerrain.TerrainID, SnapshotPath: "edited.gkterrainchunk"}
				job.ImportedWorldOverride = &content.ImportedWorldChunkOverrideDef{WorldID: loaded.WorldID, SnapshotPath: "edited.gkchunk"}
				// Valid override files are authoritative even when both base-entry paths and
				// the external aux ref are unavailable. Embedded aux is part of that authority.
				job.TerrainEntry.ChunkPath = "missing-base.gkterrainchunk"
				job.ImportedWorldEntry.ChunkPath = "missing-base.gkchunk"
				job.ImportedWorldEntry.Aux = &content.ImportedWorldChunkAuxRefDef{AuxPath: "missing-base.gkaux"}
				resolved.TerrainChunk = loadedTerrain
				resolved.ImportedWorldChunk = loaded
				resolved.ImportedWorldAux = loaded.EmbeddedAux
				resolved.ImportedWorldAuxHit = embedded
				resolved.ImportedWorldAuxMiss = false
				expectedKey := streamedImportedWorldGeometryCacheKey("imported_override", importedPath, loaded.PayloadHash, loaded.PayloadSizeBytes)
				resolved.PreparedImportedWorldGeometryCacheKey = expectedKey
				estimate, err := streamedChunkPrebuildCharge(resolved, job)
				if err != nil {
					t.Fatal(err)
				}
				owner := newStreamedPendingPreparedOwner(estimate)
				job.pendingOwner = owner
				result := prepareStreamedChunkLoad(job)
				defer result.release()
				i12bCompositeGeometry(t, result, resolved, job)
				if result.PreparedImportedWorldGeometryCacheKey != expectedKey || result.ImportedWorldAuxHit != embedded || result.ImportedWorldAuxMiss || (result.ImportedWorldAux != nil) != embedded {
					t.Fatal("override identity/embedded normal precedence changed")
				}
				if embedded && result.ImportedWorldAux != result.ImportedWorldChunk.EmbeddedAux {
					t.Fatal("override normal selection detached from embedded authority")
				}
				if result.pendingCredit == nil || owner.snapshot().Bytes != estimate || streamedPreparedChunkCharge(result) > estimate {
					t.Fatal("real override final geometry escaped its composite prebuild credit")
				}
				credit := result.pendingCredit
				result = admitStreamedPreparedChunk(owner, result)
				if result.pendingCredit != credit || owner.snapshot().Bytes != streamedPreparedChunkCharge(result) {
					t.Fatal("override result did not reconcile original credit")
				}
				result.release()
				if owner.snapshot().Bytes != 0 || job.Loader.Stats().PinnedBytes != 0 {
					t.Fatal("override ownership leaked")
				}
			})
		}
	}
}
