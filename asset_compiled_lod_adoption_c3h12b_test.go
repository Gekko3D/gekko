package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/content"
)

func c3h12bPublish(t *testing.T, packet *compiledAssetPacket, server *AssetServer) *PreparedAuthoredAsset {
	t.Helper()
	prepared, err := publishCompiledAssetPacket(packet, server, nil)
	if err != nil || prepared == nil {
		t.Fatal("LOD packet publication failed", err)
	}
	return prepared
}

func c3h12bBinding(t *testing.T, server *AssetServer, fullID AssetId) *compiledAssetLODBinding {
	t.Helper()
	binding, ok := server.compiledAssetLODForGeometry(fullID)
	if !ok || binding == nil || binding.fullID != fullID || binding.coarseID == (AssetId{}) || binding.proof == nil {
		t.Fatal("ordinary full geometry missing private derivative association")
	}
	return binding
}

func c3h12bBindingCharge(binding *compiledAssetLODBinding) int64 {
	proof := *binding.proof
	proof.full, proof.coarse = nil, nil
	return runtimeContentChargeSum(int64(unsafe.Sizeof(compiledAssetLODBinding{})), runtimeContentGraphCharge(&proof), streamedPendingGeometryCharge(binding.proof.full), streamedPendingGeometryCharge(binding.proof.coarse))
}

func c3h12bCheckInactive(t *testing.T, server *AssetServer, binding *compiledAssetLODBinding) {
	t.Helper()
	full, ok := server.GetVoxelGeometry(binding.fullID)
	if !ok {
		t.Fatal("full geometry missing")
	}
	coarse, ok := server.GetVoxelGeometry(binding.coarseID)
	if !ok {
		t.Fatal("coarse geometry missing")
	}
	if full.SourcePath != "compiled-asset-shape:"+binding.proof.sourceContentID || coarse.SourcePath != "compiled-asset-lod:"+binding.proof.contentID {
		t.Fatal("ordinary full/coarse namespaces collided")
	}
	if server.authoredVoxelBaseIdentity(binding.coarseID, binding.proof.lattice) != "" || len(server.authoredVoxelBases[binding.coarseID]) != 0 {
		t.Fatal("derivative became canonical E2 authority")
	}
	if !compiledAssetPrimaryGeometryMatches(full.XBrickMap, binding.proof.full) || !compiledAssetPrimaryGeometryMatches(coarse.XBrickMap, binding.proof.coarse) {
		t.Fatal("published geometry does not match independent proof")
	}
	if !reflect.DeepEqual(c3h12aGeometry(coarse.XBrickMap), map[[3]int]uint8{{-1, 0, 0}: 3, {0, 0, 0}: 3}) || full.XBrickMap.GetVoxelCount() != 4 {
		t.Fatal("ordinary adoption changed signed geometry")
	}
	if server.compiledAssetLODStats.Entries != 1 || server.compiledAssetLODStats.Bytes != binding.bytes || binding.bytes != c3h12bBindingCharge(binding) {
		t.Fatal("AS proof charge not exact retained storage and metadata", server.compiledAssetLODStats, binding.bytes, c3h12bBindingCharge(binding))
	}
}

func TestC3h12bDirectWorkerFirstPartAdoptSameInactiveOrdinaryBoundary(t *testing.T) {
	for _, route := range []string{"direct", "worker", "first-part"} {
		t.Run(route, func(t *testing.T) {
			path, header := c3h7Fixture(t)
			server := c3d3Server()
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			var fullID AssetId
			var packet *compiledAssetPacket
			switch route {
			case "direct":
				prepared, err := LoadAndPrepareAuthoredAsset(path, server, owner)
				if err != nil {
					t.Fatal(err)
				}
				fullID, _ = PreparedAuthoredAssetPartGeometry(prepared, "shape")
				duplicate, _ := PreparedAuthoredAssetPartGeometry(prepared, "duplicate")
				if duplicate != fullID {
					t.Fatal("duplicate full geometry lost sharing")
				}
			case "worker":
				packet = c3h12aPacket(t, path, owner)
				if len(server.voxModels) != 0 || len(server.compiledAssetLODs) != 0 {
					t.Fatal("worker published ordinary assets")
				}
				prepared := c3h12bPublish(t, packet, server)
				fullID, _ = PreparedAuthoredAssetPartGeometry(prepared, "shape")
			case "first-part":
				var err error
				fullID, _, _, err = loadAuthoredLevelVoxelModel(server, owner, path)
				if err != nil {
					t.Fatal(err)
				}
			}
			binding := c3h12bBinding(t, server, fullID)
			c3h12bCheckInactive(t, server, binding)
			full, _ := server.GetVoxelGeometry(fullID)
			coarse, _ := server.GetVoxelGeometry(binding.coarseID)
			if server.authoredVoxelBaseIdentity(fullID, binding.proof.lattice) != header.Shapes[0].BaseIdentity {
				t.Fatal("full E2 authority changed")
			}
			if packet != nil {
				lod := packet.lods[packet.partLODs["shape"]]
				shape := packet.shapes[packet.parts["shape"]]
				if binding.proof == lod.proof {
					t.Fatal("AS proof implicitly moved/shared packet ownership")
				}
				c3h12aIndependentStorage(t, full.XBrickMap, coarse.XBrickMap, binding.proof.full, binding.proof.coarse, lod.proof.full, lod.proof.coarse, lod.source, shape.source)
				if lod.registration.charge() != 0 || shape.registration.charge() != 0 {
					t.Fatal("successful adoption retained pending geometry")
				}
				pendingAfterPublication := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"packet": packet})
				if pendingAfterPublication < streamedPendingGeometryCharge(lod.proof.full)+streamedPendingGeometryCharge(lod.proof.coarse) {
					t.Fatal("AS adoption removed retained packet proof charge")
				}
				packet.release()
				packet.release()
				if streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"packet": packet}) != pendingAfterPublication {
					t.Fatal("release erased adopted packet proof storage")
				}
				if found, ok := server.compiledAssetLODForGeometry(fullID); !ok || found != binding {
					t.Fatal("packet release removed ordinary proof association")
				}
			} else {
				c3h12aIndependentStorage(t, full.XBrickMap, coarse.XBrickMap, binding.proof.full, binding.proof.coarse)
			}
			if s := owner.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
				t.Fatal("ordinary adoption retained transient decode scope", s)
			}
		})
	}
}

func TestC3h12bWarmReusePreservesEditsAndProofWithoutRawRevisionWrites(t *testing.T) {
	path, _ := c3h7Fixture(t)
	packet := c3h12aPacket(t, path, nil)
	server := c3d3Server()
	prepared := c3h12bPublish(t, packet, server)
	fullID, _ := PreparedAuthoredAssetPartGeometry(prepared, "shape")
	binding := c3h12bBinding(t, server, fullID)
	full, _ := server.GetVoxelGeometry(fullID)
	coarse, _ := server.GetVoxelGeometry(binding.coarseID)
	fullBefore, coarseBefore := full.XBrickMap.Revision, coarse.XBrickMap.Revision
	// Public raw writes bypass Revision; the private proof must still reject them.
	for _, s := range full.XBrickMap.Sectors {
		s.PackedBricks[0].Payload[0][0][0] = 91
		break
	}
	for _, s := range coarse.XBrickMap.Sectors {
		s.PackedBricks[0].Payload[0][0][0] = 92
		break
	}
	if full.XBrickMap.Revision != fullBefore || coarse.XBrickMap.Revision != coarseBefore {
		t.Fatal("raw fixture unexpectedly changed revision")
	}
	c3h11CheckGeometry(t, full.XBrickMap, binding.proof.full, false)
	c3h11CheckGeometry(t, coarse.XBrickMap, binding.proof.coarse, false)
	wantFull, wantCoarse := c3h11OwnedCopy(full.XBrickMap), c3h11OwnedCopy(coarse.XBrickMap)
	stats := server.compiledAssetLODStats
	for _, attempt := range []string{"same-packet", "fresh-packet", "direct"} {
		var result *PreparedAuthoredAsset
		switch attempt {
		case "same-packet":
			result = c3h12bPublish(t, packet, server)
		case "fresh-packet":
			result = c3h12bPublish(t, c3h12aPacket(t, path, nil), server)
		case "direct":
			var err error
			result, err = LoadAndPrepareAuthoredAsset(path, server, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		id, _ := PreparedAuthoredAssetPartGeometry(result, "shape")
		got := c3h12bBinding(t, server, id)
		if id != fullID || got != binding || server.compiledAssetLODStats != stats || !reflect.DeepEqual(full.XBrickMap, wantFull) || !reflect.DeepEqual(coarse.XBrickMap, wantCoarse) {
			t.Fatal("warm reuse replaced edited ordinary asset or duplicated proof/statistics", attempt)
		}
	}
}

func TestC3h12bDeleteAndConsumedRebuildUsesImmutableProofWithoutFiles(t *testing.T) {
	for _, removed := range []string{"full", "coarse", "both"} {
		t.Run(removed, func(t *testing.T) {
			path, _ := c3h7Fixture(t)
			packet := c3h12aPacket(t, path, nil)
			server := c3d3Server()
			prepared := c3h12bPublish(t, packet, server)
			oldFull, _ := PreparedAuthoredAssetPartGeometry(prepared, "shape")
			binding := c3h12bBinding(t, server, oldFull)
			oldCoarse := binding.coarseID
			lod := packet.lods[packet.partLODs["shape"]]
			shape := packet.shapes[packet.parts["shape"]]
			shape.source.SetVoxel(-2, 0, 0, 91)
			lod.source.SetVoxel(-1, 0, 0, 92)
			if err := os.RemoveAll(filepath.Dir(path)); err != nil {
				t.Fatal(err)
			}
			if removed == "full" || removed == "both" {
				if !server.DeleteVoxelGeometry(oldFull) {
					t.Fatal("full delete failed")
				}
			}
			if removed == "coarse" || removed == "both" {
				if !server.DeleteVoxelGeometry(oldCoarse) {
					t.Fatal("coarse delete failed")
				}
			}
			if _, ok := server.compiledAssetLODForGeometry(oldFull); ok || server.compiledAssetLODStats.Entries != 0 || server.compiledAssetLODStats.Bytes != 0 {
				t.Fatal("delete retained dead association/statistics")
			}
			if removed == "full" {
				if _, ok := server.GetVoxelGeometry(oldCoarse); !ok {
					t.Fatal("full deletion revoked independent coarse lifetime")
				}
			}
			if removed == "coarse" {
				if _, ok := server.GetVoxelGeometry(oldFull); !ok {
					t.Fatal("coarse deletion revoked full authority")
				}
			}
			rebuilt := c3h12bPublish(t, packet, server)
			fullID, _ := PreparedAuthoredAssetPartGeometry(rebuilt, "shape")
			fresh := c3h12bBinding(t, server, fullID)
			c3h12bCheckInactive(t, server, fresh)
			if (removed == "full" || removed == "both") && fullID == oldFull {
				t.Fatal("rebuild returned deleted full ID")
			}
			if (removed == "coarse" || removed == "both") && fresh.coarseID == oldCoarse {
				t.Fatal("rebuild returned deleted coarse ID")
			}
			if removed == "full" && fresh.coarseID != oldCoarse {
				t.Fatal("full rebuild discarded independent warm coarse")
			}
			if removed == "coarse" && fullID != oldFull {
				t.Fatal("coarse rebuild replaced warm full")
			}
			if fresh.proof == lod.proof {
				t.Fatal("rebuild shared packet proof")
			}
		})
	}
}

func TestC3h12bCoarseDeletionDropsAllReferencesAndGetterRequiresBothAssets(t *testing.T) {
	path, _ := c3h7Fixture(t)
	server := c3d3Server()
	prepared := c3h12bPublish(t, c3h12aPacket(t, path, nil), server)
	id, _ := PreparedAuthoredAssetPartGeometry(prepared, "shape")
	binding := c3h12bBinding(t, server, id)
	full, _ := server.GetVoxelGeometry(id)
	// Model multiple references to the same independent ordinary coarse asset.
	secondID := server.RegisterSharedVoxelGeometry(full.XBrickMap.Copy(), "second-owner")
	second := *binding
	second.fullID = secondID
	server.compiledAssetLODs[secondID] = &second
	server.compiledAssetLODStats.Entries++
	server.compiledAssetLODStats.Bytes += second.bytes
	if !server.DeleteVoxelGeometry(binding.coarseID) {
		t.Fatal("coarse delete failed")
	}
	if len(server.compiledAssetLODs) != 0 || server.compiledAssetLODStats.Entries != 0 || server.compiledAssetLODStats.Bytes != 0 {
		t.Fatal("coarse deletion drained only one association")
	}
	for _, fullID := range []AssetId{id, secondID} {
		if _, ok := server.GetVoxelGeometry(fullID); !ok {
			t.Fatal("coarse deletion removed independent full asset")
		}
	}
	// A stale private table entry must not expose an association to absent assets.
	server.compiledAssetLODs[id] = binding
	if got, ok := server.compiledAssetLODForGeometry(id); ok || got != nil {
		t.Fatal("getter returned association with missing coarse")
	}
	delete(server.voxModels, id)
	if got, ok := server.compiledAssetLODForGeometry(id); ok || got != nil {
		t.Fatal("getter returned association with missing full")
	}
}

func TestC3h12bWarmConflictsRejectWithoutOverwriteOrStatsDrift(t *testing.T) {
	for _, kind := range []string{"stale-full-key", "stale-coarse-key", "source-id", "lattice", "derivative-id", "binding-full-id", "binding-coarse-id", "base-provenance", "used-value", "nil-proof", "nil-full-baseline", "nil-coarse-baseline"} {
		t.Run(kind, func(t *testing.T) {
			path, _ := c3h7Fixture(t)
			server := c3d3Server()
			first := c3h12bPublish(t, c3h12aPacket(t, path, nil), server)
			id, _ := PreparedAuthoredAssetPartGeometry(first, "shape")
			binding := c3h12bBinding(t, server, id)
			full, _ := server.GetVoxelGeometry(id)
			coarse, _ := server.GetVoxelGeometry(binding.coarseID)
			full.XBrickMap.SetVoxel(-2, 0, 0, 91)
			coarse.XBrickMap.SetVoxel(-1, 0, 0, 92)
			wantFull, wantCoarse := c3h11OwnedCopy(full.XBrickMap), c3h11OwnedCopy(coarse.XBrickMap)
			switch kind {
			case "stale-full-key":
				delete(server.voxModels, id)
			case "stale-coarse-key":
				delete(server.voxModels, binding.coarseID)
			case "source-id":
				binding.proof.sourceContentID = strings.Repeat("a", 64)
			case "lattice":
				binding.proof.lattice.VoxelResolution *= 2
			case "derivative-id":
				binding.proof.contentID = strings.Repeat("b", 64)
			case "binding-full-id":
				binding.fullID = makeAssetId()
			case "binding-coarse-id":
				binding.coarseID = makeAssetId()
			case "base-provenance":
				server.authoredVoxelBases[id][binding.proof.lattice] = strings.Repeat("c", 64)
			case "used-value":
				binding.proof.value = 7
			case "nil-proof":
				binding.proof = nil
			case "nil-full-baseline":
				binding.proof.full = nil
			case "nil-coarse-baseline":
				binding.proof.coarse = nil
			}
			stats := server.compiledAssetLODStats
			next := c3h12aPacket(t, path, nil)
			result, err := publishCompiledAssetPacket(next, server, nil)
			if err == nil || result != nil {
				t.Fatal("conflicting warm association accepted", kind)
			}
			if server.compiledAssetLODs[id] != binding || server.compiledAssetLODStats != stats || !reflect.DeepEqual(full.XBrickMap, wantFull) || !reflect.DeepEqual(coarse.XBrickMap, wantCoarse) {
				t.Fatal("warm conflict replaced proof/current assets or changed counters", kind)
			}
		})
	}
}

func c3h12bSelectedFixture(t *testing.T, kind string) string {
	t.Helper()
	input, output, asset := c3h4Fixture(t)
	output = strings.TrimSuffix(output, ".gkasset") + ".gkassetc"
	if kind == "ineligible-first" {
		asset.Parts[0].Source.VoxelShape.Palette[0].MaterialID = "unused"
	}
	if kind == "group-first" {
		asset.Parts[0], asset.Parts[1] = asset.Parts[1], asset.Parts[0]
	}
	if kind == "distinct-unused" {
		part := asset.Parts[0]
		part.ID = "unused-distinct"
		part.Source.VoxelShape = &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "mat"}}, Voxels: []content.VoxelObjectVoxelDef{{X: 20, Value: 3}, {X: 21, Value: 3}, {X: 22, Value: 3}, {X: 23, Value: 3}}}
		asset.Parts = append(asset.Parts, part)
	}
	c3cWrite(t, input, asset)
	if _, err := CompileAuthoredAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: kind != "noLOD"}); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestC3h12bFirstPartMembershipNoLODAndNilServer(t *testing.T) {
	for _, kind := range []string{"ineligible-first", "group-first", "distinct-unused", "noLOD"} {
		t.Run(kind, func(t *testing.T) {
			path := c3h12bSelectedFixture(t, kind)
			server := c3d3Server()
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			id, _, _, err := loadAuthoredLevelVoxelModel(server, owner, path)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "distinct-unused" {
				if len(server.voxModels) != 2 || len(server.compiledAssetLODs) != 1 {
					t.Fatal("first-part publication widened to unused declared geometry")
				}
				c3h12bCheckInactive(t, server, c3h12bBinding(t, server, id))
			} else {
				if len(server.compiledAssetLODs) != 0 || server.compiledAssetLODStats.Entries != 0 || server.compiledAssetLODStats.Bytes != 0 {
					t.Fatal("unselected/noneligible derivative gained association")
				}
				want := 1
				if kind == "group-first" {
					want = 0
				}
				if len(server.voxModels) != want {
					t.Fatal("first-part publication registered unused ordinary/coarse geometry")
				}
			}
			if prepared, err := LoadAndPrepareAuthoredAsset(path, nil, owner); err != nil || prepared == nil {
				t.Fatal("nil-server direct verify-only contract changed", err)
			}
			if nilID, nilPalette, _, err := loadAuthoredLevelVoxelModel(nil, owner, path); err != nil || nilID != (AssetId{}) || nilPalette != (AssetId{}) {
				t.Fatal("nil-server first-part verify-only contract changed", err)
			}
			if s := owner.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
				t.Fatal("selected or nil publication leaked decode ownership", s)
			}
		})
	}
	// Entire declared closure still verifies before first-part or nil publication.
	path, header := c3h7Fixture(t)
	if err := os.Remove(c3h7LODPath(path, header)); err != nil {
		t.Fatal(err)
	}
	server := c3d3Server()
	if id, _, _, err := loadAuthoredLevelVoxelModel(server, nil, path); err == nil || id != (AssetId{}) || len(server.voxModels) != 0 {
		t.Fatal("first-part ignored invalid closure")
	}
	if result, err := LoadAndPrepareAuthoredAsset(path, nil, nil); err == nil || result != nil {
		t.Fatal("nil-server bypassed declared LOD verification")
	}
}

func TestC3h12bClosedOriginRejectsPublicationBeforeOrdinaryWrites(t *testing.T) {
	path, _ := c3h7Fixture(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	packet := c3h12aPacket(t, path, caller.Loader())
	caller.Close()
	server := c3d3Server()
	if result, err := publishCompiledAssetPacket(packet, server, caller.Loader()); err == nil || result != nil {
		t.Fatal("closed-origin publication returned prepared asset")
	}
	if len(server.voxModels) != 0 || len(server.voxPalettes) != 0 || len(server.compiledAssetLODs) != 0 || server.compiledAssetLODStats.Entries != 0 || server.compiledAssetLODStats.Bytes != 0 {
		t.Fatal("closed origin published partial ordinary assets/proofs")
	}
	lod := packet.lods[packet.partLODs["shape"]]
	if lod.registration.charge() <= 0 {
		t.Fatal("rejected publication took caller-owned pending derivative")
	}
	packet.release()
	if lod.registration.charge() != 0 {
		t.Fatal("caller release failed to drain rejected pending derivative")
	}
}

func TestC3h12bConcurrentMatchingPublicationsShareOrdinaryProofOwner(t *testing.T) {
	path, _ := c3h7Fixture(t)
	server := c3d3Server()
	packets := make([]*compiledAssetPacket, 6)
	for i := range packets {
		packets[i] = c3h12aPacket(t, path, nil)
	}
	type outcome struct {
		prepared *PreparedAuthoredAsset
		err      error
	}
	results := make(chan outcome, len(packets))
	for _, packet := range packets {
		go func(packet *compiledAssetPacket) {
			prepared, err := publishCompiledAssetPacket(packet, server, nil)
			results <- outcome{prepared, err}
		}(packet)
	}
	var fullID AssetId
	for range packets {
		result := <-results
		if result.err != nil || result.prepared == nil {
			t.Fatal("concurrent matching adoption rejected", result.err)
		}
		id, _ := PreparedAuthoredAssetPartGeometry(result.prepared, "shape")
		if fullID == (AssetId{}) {
			fullID = id
		} else if id != fullID {
			t.Fatal("concurrent publication duplicated full owner")
		}
	}
	binding := c3h12bBinding(t, server, fullID)
	c3h12bCheckInactive(t, server, binding)
	if len(server.voxModels) != 2 || len(server.compiledAssetLODs) != 1 || server.compiledAssetLODStorageStats() != server.compiledAssetLODStats {
		t.Fatal("concurrent adoption duplicated coarse/proof owner or drifted scalar statistics")
	}
	for _, packet := range packets {
		lod := packet.lods[packet.partLODs["shape"]]
		if lod.registration.charge() != 0 {
			t.Fatal("concurrent successful publication retained unused registration")
		}
		if lod.proof == binding.proof {
			t.Fatal("concurrent publication moved packet proof into AS")
		}
	}
}

func TestC3h12bPreparedLODOptInIsPerPartAndPerInput(t *testing.T) {
	for _, route := range []string{"direct", "packet"} {
		t.Run(route, func(t *testing.T) {
			path := c3h12bSelectedFixture(t, "ineligible-first")
			server := c3d3Server()
			var prepared *PreparedAuthoredAsset
			if route == "direct" {
				var err error
				prepared, err = LoadAndPrepareAuthoredAsset(path, server, nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				prepared = c3h12bPublish(t, c3h12aPacket(t, path, nil), server)
			}
			fullID, _ := PreparedAuthoredAssetPartGeometry(prepared, "shape")
			duplicateID, _ := PreparedAuthoredAssetPartGeometry(prepared, "duplicate")
			binding := c3h12bBinding(t, server, fullID)
			if fullID != duplicateID || prepared.parts["shape"].compiledLOD != (AssetId{}) || prepared.parts["duplicate"].compiledLOD != binding.coarseID {
				t.Fatal("geometry availability widened opt-in to nondeclared part")
			}
			if prepared.parts["shape"].model != fullID || prepared.parts["duplicate"].model != fullID {
				t.Fatal("private LOD membership replaced public full model")
			}
			// A legacy input with the same full geometry does not opt in merely because
			// another asset previously installed geometry availability on AssetServer.
			legacy := c3h12bSelectedFixture(t, "noLOD")
			var later *PreparedAuthoredAsset
			if route == "direct" {
				var err error
				later, err = LoadAndPrepareAuthoredAsset(legacy, server, nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				later = c3h12bPublish(t, c3h12aPacket(t, legacy, nil), server)
			}
			legacyID, _ := PreparedAuthoredAssetPartGeometry(later, "shape")
			if legacyID != fullID {
				t.Fatal("fixture failed to exercise shared warm ordinary geometry")
			}
			for partID, part := range later.parts {
				if part.compiledLOD != (AssetId{}) {
					t.Fatal("nonLOD input inherited another input's private opt-in", partID)
				}
			}
			if got := c3h12bBinding(t, server, legacyID); got != binding {
				t.Fatal("nonLOD preparation discarded independent warm geometry availability")
			}
		})
	}
}
