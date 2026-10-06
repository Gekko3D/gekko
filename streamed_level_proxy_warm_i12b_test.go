package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// A valid immutable warm source may retain metadata beyond its occupied cells.
// These capacities/revisions are storage, even when they do not affect rendering.
func i12bWarmMap() *volume.XBrickMap {
	geometry := volume.NewXBrickMap()
	for i := 0; i < 64; i++ {
		geometry.SetVoxel(i*32, 0, 0, 9)
	}
	geometry.ComputeAABB()
	geometry.ClearDirty()
	for i := 0; i < 80; i++ {
		geometry.SectorRevisions[[3]int{3000 + i, 0, 0}] = uint64(i + 1)
		geometry.DirtySectors[[3]int{4000 + i, 0, 0}] = true
		geometry.DirtyBricks[[6]int{5000 + i, 0, 0, 0, 0, 0}] = true
	}
	geometry.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes, 8192)
	return geometry
}

func i12bSeedWarm(t *testing.T, cache *streamedPreparedGeometryCache, key string, compact bool) (*streamedGeometrySource, *streamedGeometryPromotion) {
	t.Helper()
	geometry := i12bWarmMap()
	if compact {
		source, promotion, _ := cache.getOrBuildSourceRequest(key, true, func() *volume.XBrickMap { return geometry })
		if source == nil || source.compact == nil {
			t.Fatal("fixture did not establish compact warm source")
		}
		return source, promotion
	}
	// Dense, qualified, not generically exposed is a supported immutable source
	// layout (the compact fallback). Install it without calling the public generic
	// getter, which would mark it exposed before the worker under test runs.
	source := &streamedGeometrySource{dense: geometry, qualified: true}
	promotion := &streamedGeometryPromotion{}
	cache.mu.Lock()
	cache.admitSourceLocked(key, source, promotion)
	cache.mu.Unlock()
	return source, promotion
}

func TestI12bProxyWarmLargerSourceRequiresGrowthBeforeRegistrationOrExposure(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, denseRequest := range []bool{false, true} {
			t.Run(map[bool]string{false: "dense-source", true: "compact-source"}[compact]+map[bool]string{false: "/compact-request", true: "/dense-request"}[denseRequest], func(t *testing.T) {
				job, _ := i12ProxyFixture(t)
				path := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
				// Keep the stale manifest identity, replace only the independently decoded
				// source payload. The established cache intentionally still owns larger data.
				small := &content.ImportedWorldChunkDef{WorldID: "small", SchemaVersion: 1, ChunkSize: 1, VoxelResolution: 1, NonEmptyVoxelCount: 1, Voxels: []content.ImportedWorldVoxelDef{{Value: 7}}}
				if err := content.SaveImportedWorldChunkWithOptions(path, small, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1}); err != nil {
					t.Fatal(err)
				}
				job.LOD.Aux = nil
				job.compactPreparedGeometry = !denseRequest
				source, err := content.LoadImportedWorldChunkRLESource(path)
				if err != nil {
					t.Fatal(err)
				}
				key := i12ExactProxyCacheKey(job, nil)
				estimate, err := streamedRLEProxyPrebuildCharge(source, nil, job.LOD, key)
				if err != nil {
					t.Fatal(err)
				}
				warm, promotion := i12bSeedWarm(t, job.PreparedGeometryCache, key, compact)
				owner := newStreamedPendingPreparedOwner(estimate + 1)
				hold, ok := owner.reserve(1)
				if !ok {
					t.Fatal("hold control")
				}
				defer hold.release()
				job.pendingOwner = owner
				denied := prepareStreamedSectorProxyLoad(job)
				defer denied.release()
				if denied.Err != nil || denied.retryCost <= estimate || denied.Chunk != nil || denied.registration != nil || denied.geometrySource != nil || denied.PreparedGeometry != nil || denied.pendingCredit != nil {
					t.Fatalf("larger cached proxy was not rejected before registration: retry=%d inputbound=%d err=%v", denied.retryCost, estimate, denied.Err)
				}
				if owner.snapshot().Bytes != 1 || job.Loader.Stats().PinnedBytes != 0 {
					t.Fatal("denied warm proxy retained decoded scope or credit")
				}
				if warm.exposed.Load() || promotion.dense != nil {
					t.Fatal("denied warm proxy exposed or promoted source before growth admission")
				}
				captured, _, hit := job.PreparedGeometryCache.getOrBuildSourceRequest(key, true, nil)
				if !hit || captured != warm {
					t.Fatal("denial replaced/invalidated immutable warm cache source")
				}
				denied = admitStreamedPreparedProxy(owner, denied)
				if denied.retryCost == 0 || owner.snapshot().Bytes != 1 {
					t.Fatal("admission converted a warm grow retry into success")
				}
				hold.release()
				retry := prepareStreamedSectorProxyLoad(job)
				defer retry.release()
				if retry.Err != nil || retry.retryCost != 0 || retry.registration == nil || retry.registration.geometry.GetVoxelCount() != 64 {
					t.Fatalf("sole retry did not use captured larger source: %v", retry.Err)
				}
				credit := retry.pendingCredit
				retry = admitStreamedPreparedProxy(owner, retry)
				if credit == nil || retry.pendingCredit != credit || owner.snapshot().Bytes != streamedPreparedProxyCharge(retry) || owner.snapshot().OversizedAdmissions != 1 {
					t.Fatal("warm proxy growth did not reconcile same sole credit once")
				}
				retry.release()
				if owner.snapshot().Bytes != 0 {
					t.Fatal("warm proxy credit leaked")
				}
			})
		}
	}
}
