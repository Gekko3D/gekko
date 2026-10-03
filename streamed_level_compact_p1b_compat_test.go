package gekko

import (
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestP1bCompactWinnerSurvivesDirectDenseAcquire(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "caller registration"}[prepared], func(t *testing.T) {
			f := p1bRuntime(t, 8, 1<<30)
			cache := f.runtime.PreparedGeometryCache
			winner, caller := s2aGeometry(8), s2aGeometry(1)
			caller.SetVoxel(0, 0, 0, 7)
			caller.ComputeAABB()
			caller.ClearDirty()
			cache.getOrBuildSource("winner", true, func() *volume.XBrickMap { return winner })
			var id AssetId
			if prepared {
				registration := prepareCachedStreamedGeometryRegistration(caller)
				var adopted bool
				id, _, adopted = cache.acquirePreparedAsset(f.assets, "winner", caller, registration)
				if adopted {
					t.Fatal("caller-matching registration was adopted over the winning compact source")
				}
			} else {
				id, _ = cache.acquireAsset(f.assets, "winner", caller)
			}
			asset, ok := f.assets.GetVoxelGeometry(id)
			if id == (AssetId{}) || !ok || asset.XBrickMap == nil || asset.XBrickMap == caller || asset.XBrickMap == winner {
				t.Fatal("direct dense acquisition did not register independent current winning geometry")
			}
			p1bGeometry(t, asset.XBrickMap, winner)
			stats := cache.snapshot()
			if stats.PreparedBytes <= 0 || stats.AssetBytes != s2aCharge(t, asset.XBrickMap) || stats.PinnedBytes != stats.Bytes {
				t.Fatalf("direct acquisition lost prepared/asset attribution or pins: %+v", stats)
			}
			if reusedID, reused := cache.acquireAsset(f.assets, "winner", caller); !reused || reusedID != id {
				t.Fatal("direct acquisition lost warm asset identity")
			}
			cache.releaseAssetID(f.assets, id)
			cache.releaseAssetID(f.assets, id)
			if warmID, reused := cache.acquireAsset(f.assets, "winner", caller); !reused || warmID != id {
				t.Fatal("released winning entry lost warm reuse")
			}
			source, hit := cache.getOrBuild("winner", nil)
			if !hit || source == asset.XBrickMap || source == caller {
				t.Fatal("direct acquisition replaced winning source authority or aliased the registered asset")
			}
			p1bGeometry(t, source, winner)
			if again, hit := cache.getOrBuild("winner", nil); !hit || again != source {
				t.Fatal("direct acquisition failed to preserve stable dense source identity")
			}
		})
	}
}

func TestP1bDenseWinnerPendingHandleMetadataIsCharged(t *testing.T) {
	f := p1bRuntime(t, 8, 1<<30)
	cache, dense := f.runtime.PreparedGeometryCache, s2aGeometry(1)
	cache.getOrBuild("dense winner", func() *volume.XBrickMap { return dense })
	handle, hit := cache.getOrBuildSource("dense winner", true, nil)
	if !hit || handle == nil {
		t.Fatal("qualified caller did not capture the current dense winner")
	}
	// Both envelopes retain the same geometry; the qualified packet additionally
	// retains its source handle allocation. No representation fields are assumed.
	legacyFull := streamedPreparedChunk{PreparedImportedWorldGeometry: dense}
	qualifiedFull := streamedPreparedChunk{geometrySource: handle}
	legacyProxy := streamedPreparedSectorProxy{PreparedGeometry: dense}
	qualifiedProxy := streamedPreparedSectorProxy{geometrySource: handle}
	metadata := int64(unsafe.Sizeof(*handle))
	if got, want := streamedPreparedChunkCharge(qualifiedFull), streamedPreparedChunkCharge(legacyFull)+metadata; got != want {
		t.Errorf("qualified full packet charge = %d, want dense packet plus retained handle %d", got, want)
	}
	if got, want := streamedPreparedProxyCharge(qualifiedProxy), streamedPreparedProxyCharge(legacyProxy)+metadata; got != want {
		t.Errorf("qualified proxy packet charge = %d, want dense packet plus retained handle %d", got, want)
	}
}

func TestP1bPromotedAuthorityRejectsLaterCapturedRegistration(t *testing.T) {
	f := p1bRuntime(t, 8, 1<<30)
	s1fPrepared(t, f, ChunkCoord{}, false)
	first := <-f.runtime.PreparedLoads
	source, hit := f.runtime.PreparedGeometryCache.getOrBuild(first.PreparedImportedWorldGeometryCacheKey, nil)
	if !hit || source == nil {
		t.Fatal("first qualified preparation did not expose dense authority")
	}
	first.release()
	s1fPrepared(t, f, ChunkCoord{}, false)
	second := <-f.runtime.PreparedLoads
	// The second worker captured the already exposed owner before this raw edit.
	source.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[1][2][3] = 255
	f.runtime.PreparedLoads <- second
	f.commitStage()
	_, asset := p5aAsset(t, f, false)
	if _, value := asset.XBrickMap.GetVoxel(1, 2, 3); value != 255 || asset.XBrickMap == source {
		t.Fatal("later qualified candidate hid current raw edits or lost independent registration")
	}
	if p5aAdoptions(t, f.runtime) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("exposed-authority candidate was adopted or retained after current-source fallback")
	}
	source.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[1][2][3] = 0
	if _, value := asset.XBrickMap.GetVoxel(1, 2, 3); value != 255 {
		t.Fatal("editing exposed authority changed its independently registered asset")
	}
}
