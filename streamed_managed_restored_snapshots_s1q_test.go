package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// These fixtures exercise the ordinary compiled placement worker, rather than
// installing a managed owner manually after a restored placement is published.
func s1qRestore(t *testing.T, mode string, enabled bool, count int) (*s1gFixture, content.AssetPartDef, string, string, *content.VoxelObjectSnapshotDef) {
	t.Helper()
	f, part, assetPath, snapshotPath := c3d4aRuntime(t)
	f.runtime.Config.EnableManagedPreparedAssets = enabled
	first := f.runtime.PlacementsByChunk[ChunkCoord{}][0]
	f.runtime.PlacementsByChunk[ChunkCoord{}] = nil
	for i := 0; i < count; i++ {
		p := first
		p.PlacementID = s1gID(0, i)
		p.Transform.Position = content.Vec3{float32(i + 1), 1, 1}
		f.runtime.PlacementsByChunk[ChunkCoord{}] = append(f.runtime.PlacementsByChunk[ChunkCoord{}], p)
	}
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = count
	base, lattice, identity := e2b1Canonical(t, part)
	payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: mode, PlacementID: s1gID(0, 0), ItemID: "body", Lattice: lattice}
	switch mode {
	case "v1":
		payload.SchemaVersion, payload.Mode = 1, content.VoxelObjectPayloadFull
		payload.Voxels = []content.VoxelObjectVoxelDef{{X: -9, Value: 7}}
	case content.VoxelObjectPayloadFull:
		payload.Voxels = []content.VoxelObjectVoxelDef{{X: -9, Value: 7}}
	case content.VoxelObjectPayloadBaseDelta:
		payload.BaseIdentity = identity
		payload.Voxels = []content.VoxelObjectVoxelDef{{}, {X: -9, Value: 7}}
	case content.VoxelObjectPayloadHybridDelta:
		payload.SchemaVersion, payload.BaseIdentity = 3, identity
		payload.ReplacementBricks = [][3]int32{{0, 0, 0}}
		payload.Voxels = []content.VoxelObjectVoxelDef{{X: 1, Value: 1}, {X: -9, Value: 7}}
	case "empty-delta":
		payload.Mode, payload.BaseIdentity = content.VoxelObjectPayloadBaseDelta, identity
	case "empty-full":
		payload.Mode = content.VoxelObjectPayloadFull
	case "empty-geometry":
		payload.Mode, payload.BaseIdentity = content.VoxelObjectPayloadBaseDelta, identity
		for _, v := range part.Source.VoxelShape.Voxels {
			v.Value = 0
			payload.Voxels = append(payload.Voxels, v)
		}
	default:
		t.Fatal("unknown restore mode", mode)
	}
	want := &content.VoxelObjectSnapshotDef{Voxels: payload.Voxels}
	if mode != "v1" {
		var err error
		want, err = content.ResolveVoxelObjectPayload(payload, base, lattice, payload.PlacementID, payload.ItemID, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < count; i++ {
		path := snapshotPath
		if i != 0 {
			path = filepath.Join(filepath.Dir(snapshotPath), s1gID(0, i)+".gkvoxobj")
		}
		payload.PlacementID = s1gID(0, i)
		if mode == "v1" {
			if err := content.SaveVoxelObjectSnapshot(path, want); err != nil {
				t.Fatal(err)
			}
			f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(payload.PlacementID, "body")] = content.VoxelObjectOverrideDef{PlacementID: payload.PlacementID, ItemID: "body", SnapshotPath: path}
		} else {
			e2b2Save(t, f, path, payload)
		}
	}
	return f, part, assetPath, snapshotPath, want
}

func s1qManaged(t *testing.T, f *s1gFixture, eid EntityId) AssetId {
	t.Helper()
	vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	if vm.OverrideGeometry == (AssetId{}) || f.assets.managedVoxelEntry(vm.OverrideGeometry) == nil {
		t.Fatal("worker-captured compiled restored snapshot did not automatically acquire managed authority")
	}
	if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != vm.OverrideGeometry || lease.Server != f.assets {
		t.Fatal("managed restore did not retain exact streamed geometry lease")
	}
	return vm.OverrideGeometry
}

func s1qGeometry(t *testing.T, got *volume.XBrickMap, want *content.VoxelObjectSnapshotDef) {
	t.Helper()
	if got == nil || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(got).Voxels), c3cGeometry(want.Voxels)) {
		t.Fatal("restored geometry differs from current selected payload")
	}
}

func TestS1qCompiledRestoredModesAutomaticallyManageAndConsumeWorkerRenderer(t *testing.T) {
	for _, mode := range []string{"v1", content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta, content.VoxelObjectPayloadHybridDelta, "empty-delta", "empty-full", "empty-geometry"} {
		t.Run(mode, func(t *testing.T) {
			f, part, _, _, want := s1qRestore(t, mode, true, 2)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			if len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != 0 {
				t.Fatal("restore worker published assets or ECS")
			}
			f.commitStage()
			first, second := s1nBody(t, f, 0), s1nBody(t, f, 1)
			a, b := s1qManaged(t, f, first), s1qManaged(t, f, second)
			if a == b {
				t.Fatal("restored siblings share managed authority")
			}
			authority, _ := f.assets.getVoxelGeometry(a)
			s1qGeometry(t, authority.XBrickMap, want)
			bounds := XBrickMapFromVoxelObjectSnapshot(want)
			min, max := bounds.ComputeAABB()
			if authority.LocalMin != min || authority.LocalMax != max {
				t.Fatal("managed restore replaced snapshot bounds with canonical base bounds")
			}
			bound := mode == content.VoxelObjectPayloadBaseDelta || mode == content.VoxelObjectPayloadHybridDelta || mode == "empty-delta" || mode == "empty-geometry"
			if bound {
				e2b3Qualified(t, f, first, s1gID(0, 0), part)
			} else {
				e2b3Fallback(t, f, f.runtime, first, s1gID(0, 0), "body")
			}
			before := f.assets.PreparedVoxelRendererCopyStats()
			if before.Entries != 2 || before.Bytes <= 0 {
				t.Fatal("restore did not retain independent worker-prepared first renderer maps")
			}
			state := s1nRenderer(f)
			s1nBridge(f, state)
			left, right := s1l6Input(t, state.GetVoxelObject(first)), s1l6Input(t, state.GetVoxelObject(second))
			if left.SameSource(right) || state.GetVoxelObject(first).XBrickMap == authority.XBrickMap || state.GetVoxelObject(first).XBrickMap == state.GetVoxelObject(second).XBrickMap {
				t.Fatal("restored owner, CPU authority, or renderer maps alias")
			}
			if after := f.assets.PreparedVoxelRendererCopyStats(); after.Adoptions != before.Adoptions+2 || after.Entries != 0 || after.Bytes != 0 {
				t.Fatal("bridge did not consume restored worker renderer derivatives exactly once")
			}
			e2b4Edit(t, f, first, volume.VoxelWrite{X: -9, Value: 9})
			s1qGeometry(t, s1l6Map(t, right), want)
			if f.runtime.pendingPrepared.snapshot().Bytes != 0 {
				t.Fatal("restored commit retained pending credit")
			}
		})
	}
}

func TestS1qRestoredEditRevertSaveReloadRetainsOriginalBaseline(t *testing.T) {
	for _, mode := range []string{"v1", content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta, content.VoxelObjectPayloadHybridDelta, "empty-delta", "empty-geometry"} {
		t.Run(mode, func(t *testing.T) {
			f, part, _, _, _ := s1qRestore(t, mode, true, 1)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := s1nBody(t, f, 0)
			override := s1qManaged(t, f, eid)
			// Editing and reverting an unchanged cell must leave prior restored
			// assignments intact, while reverting x=0 removes its prior deletion.
			e2b4Edit(t, f, eid, volume.VoxelWrite{X: 40, Value: 6}, volume.VoxelWrite{X: 40}, volume.VoxelWrite{Value: 1}, volume.VoxelWrite{X: 1, Value: 4})
			current, _, ok := currentVoxelMapForEntity(f.cmd, eid)
			if !ok {
				t.Fatal("managed restore has no CPU interaction authority")
			}
			want := VoxelObjectSnapshotFromXBrickMap(current)
			e2b4Depart(f)
			e2b4Unloaded(t, f)
			durable := e2b4Durable(t, f)
			bound := mode != "v1" && mode != content.VoxelObjectPayloadFull
			if bound {
				base, lattice, identity := e2b1Canonical(t, part)
				if durable.BaseIdentity != identity || durable.Lattice != lattice || (durable.Mode != content.VoxelObjectPayloadBaseDelta && durable.Mode != content.VoxelObjectPayloadHybridDelta) {
					t.Fatal("saved restore lost original canonical identity/lattice")
				}
				resolved, err := content.ResolveVoxelObjectPayload(durable, base, lattice, s1gID(0, 0), "body", nil)
				if err != nil {
					t.Fatal(err)
				}
				s1qGeometry(t, XBrickMapFromVoxelObjectSnapshot(resolved), want)
			} else if durable.Mode != content.VoxelObjectPayloadFull || durable.BaseIdentity != "" {
				t.Fatal("full/v1 restore inferred original-base provenance")
			}
			p5cAssetPresent(t, f, override, false)
			f.hooks[s1gID(0, 0)] = 0
			s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
			updateStreamedObserverSelection(f.cmd, f.runtime)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			reloaded := s1nBody(t, f, 0)
			fresh := s1qManaged(t, f, reloaded)
			if fresh == override {
				t.Fatal("reload reused retired snapshot owner")
			}
			geometry, _ := f.assets.getVoxelGeometry(fresh)
			s1qGeometry(t, geometry.XBrickMap, want)
		})
	}
}

func TestS1qCommitRevalidatesCurrentSnapshotProofAndGeometry(t *testing.T) {
	for _, change := range []string{"same-path-delta", "same-path-geometry", "equal-full-replacement", "removed", "late-snapshot", "late-disable"} {
		t.Run(change, func(t *testing.T) {
			f, part, _, path, captured := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
			key := voxelObjectRuntimeKey(s1gID(0, 0), "body")
			if change == "late-snapshot" {
				delete(f.runtime.voxelOverrideMap, key)
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			want := captured
			switch change {
			case "same-path-delta":
				payload := e2b2Payload(t, part, s1gID(0, 0), "body", content.VoxelObjectPayloadBaseDelta, 9, false)
				e2b2Save(t, f, path, payload)
				base, lattice, _ := e2b1Canonical(t, part)
				var err error
				want, err = content.ResolveVoxelObjectPayload(payload, base, lattice, s1gID(0, 0), "body", nil)
				if err != nil {
					t.Fatal(err)
				}
			case "removed":
				delete(f.runtime.voxelOverrideMap, key)
				want = &content.VoxelObjectSnapshotDef{Voxels: part.Source.VoxelShape.Voxels}
			case "late-disable":
				f.runtime.Config.EnableManagedPreparedAssets = false
			default:
				_, lattice, _ := e2b1Canonical(t, part)
				voxels := captured.Voxels
				if change != "equal-full-replacement" {
					voxels = []content.VoxelObjectVoxelDef{{X: -9, Value: 9}}
				}
				e2b2Save(t, f, path, &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: s1gID(0, 0), ItemID: "body", Lattice: lattice, Voxels: voxels})
				want = &content.VoxelObjectSnapshotDef{Voxels: voxels}
			}
			f.commitStage()
			eid := s1nBody(t, f, 0)
			vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if f.assets.managedVoxelEntry(vm.GeometryAsset()) != nil {
				t.Fatal("changed/removed/late snapshot adopted stale worker managed proof")
			}
			geometry, _ := f.assets.getVoxelGeometry(vm.GeometryAsset())
			s1qGeometry(t, geometry.XBrickMap, want)
			state := s1nRenderer(f)
			s1nBridge(f, state)
			s1l6NoInput(t, state.GetVoxelObject(eid))
			if f.assets.PreparedVoxelRendererCopyStats().Entries != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 {
				t.Fatal("revalidation fallback retained worker maps or credit")
			}
			// Equal voxels with full metadata must never retain the delta binding.
			if change == "equal-full-replacement" {
				e2b3Enable(t, f, eid)
				e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
			}
		})
	}
}

func TestS1qBorrowedGlobalDoesNotDisqualifyIndependentRestore(t *testing.T) {
	for _, timing := range []string{"before-worker", "after-worker"} {
		t.Run(timing, func(t *testing.T) {
			f, part, path, _, want := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
			shared, _ := s1pWarm(t, f, path, "body")
			borrow := func() { global, _ := f.assets.GetVoxelGeometry(shared); global.XBrickMap.SetVoxel(0, 0, 0, 9) }
			if timing == "before-worker" {
				borrow()
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			if timing == "after-worker" {
				borrow()
			}
			f.commitStage()
			eid := s1nBody(t, f, 0)
			id := s1qManaged(t, f, eid)
			geometry, _ := f.assets.getVoxelGeometry(id)
			s1qGeometry(t, geometry.XBrickMap, want)
			e2b3Qualified(t, f, eid, s1gID(0, 0), part)
			state := s1nRenderer(f)
			s1nBridge(f, state)
			s1qGeometry(t, s1l6Map(t, s1l6Input(t, state.GetVoxelObject(eid))), want)
		})
	}
}

func TestS1qRestoredOptOutAndLegacyAuthoredCompatibility(t *testing.T) {
	for _, kind := range []string{"opt-out", "legacy-authored"} {
		t.Run(kind, func(t *testing.T) {
			f, _, _, _, want := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, kind != "opt-out", 1)
			if kind == "legacy-authored" {
				legacy, part, path := e2b2Runtime(t)
				legacy.runtime.Config.EnableManagedPreparedAssets = true
				e2b2Save(t, legacy, path, e2b2Payload(t, part, s1gID(0, 0), "body", content.VoxelObjectPayloadBaseDelta, 7, false))
				f = legacy
				want = &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {X: 1, Value: 1}}}
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := s1nBody(t, f, 0)
			vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if f.assets.managedVoxelEntry(vm.GeometryAsset()) != nil {
				t.Fatal("excluded restored path automatically managed")
			}
			geometry, _ := f.assets.getVoxelGeometry(vm.GeometryAsset())
			s1qGeometry(t, geometry.XBrickMap, want)
			if f.assets.PreparedVoxelRendererCopyStats().Entries != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 {
				t.Fatal("excluded snapshot retained managed copies")
			}
		})
	}
}

func TestS1qRestoredPlacementHookEditsAndExposurePreserveCurrentContent(t *testing.T) {
	for _, operation := range []string{"managed-edit", "raw-exposure", "unsupported-adjacency"} {
		t.Run(operation, func(t *testing.T) {
			f, _, _, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
			var adopted AssetId
			f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, ctx PostSpawnPlacementContext) {
				eid := ctx.SpawnResult.EntitiesByAssetID["body"]
				adopted = s1qManaged(t, f, eid)
				switch operation {
				case "managed-edit":
					if err := ApplyManagedVoxelWrites(cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: -9, Value: 8})); err != nil {
						t.Fatal(err)
					}
				case "raw-exposure":
					raw, _ := f.assets.GetVoxelGeometry(adopted)
					raw.XBrickMap.SetVoxel(-9, 0, 0, 8)
				case "unsupported-adjacency":
					vm := *s3cComponent[VoxelModelComponent](t, cmd, eid)
					vm.VoxelAdjacencyGroupID = 17
					cmd.AddComponents(eid, vm)
				}
			}}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := s1nBody(t, f, 0)
			state := s1nRenderer(f)
			s1nBridge(f, state)
			if operation == "managed-edit" {
				p1dVoxel(t, s1l6Map(t, s1l6Input(t, state.GetVoxelObject(eid))), -9, 8)
			} else {
				s1l6NoInput(t, state.GetVoxelObject(eid))
				want := uint8(8)
				if operation == "unsupported-adjacency" {
					want = 7
				}
				p1dVoxel(t, state.GetVoxelObject(eid).XBrickMap, -9, want)
			}
			if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != adopted || lease.Server != f.assets {
				t.Fatal("hook demotion lost streamed lease")
			}
			if f.assets.PreparedVoxelRendererCopyStats().Entries != 0 {
				t.Fatal("hook left obsolete first renderer derivative")
			}
		})
	}
}

func TestS1qRestoredPendingCreditAndTerminalCleanup(t *testing.T) {
	f, _, _, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, false, 2)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	legacy := <-f.runtime.PreparedLoads
	legacyCharge := streamedPreparedChunkCharge(legacy)
	legacy.release()
	f.runtime.Config.EnableManagedPreparedAssets = true
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	p := <-f.runtime.PreparedLoads
	charge := streamedPreparedChunkCharge(p)
	if charge <= legacyCharge || f.runtime.pendingPrepared.snapshot().Bytes != charge {
		p.release()
		t.Fatal("restored managed construction omitted pending owner/CPU/renderer storage")
	}
	alias := p
	p.release()
	alias.release()
	if f.runtime.pendingPrepared.snapshot().Bytes != 0 {
		t.Fatal("aliased restored packet release leaked credit")
	}
	owner := newStreamedPendingPreparedOwner(1)
	block, ok := owner.reserve(1)
	if !ok {
		t.Fatal("pressure fixture")
	}
	defer block.release()
	f.runtime.pendingPrepared = owner
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	denied := <-f.runtime.PreparedLoads
	if denied.retryCost <= 0 || denied.pendingCredit != nil || owner.snapshot().Bytes != 1 || len(f.assets.managedVoxelGeometry) != 0 {
		denied.release()
		t.Fatal("restore bypassed competing shared pending pressure")
	}
	denied.release()
	block.release()
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	retry := <-f.runtime.PreparedLoads
	if retry.Err != nil || retry.retryCost != 0 || retry.pendingCredit == nil {
		retry.release()
		t.Fatal("restore did not retry after pending pressure cleared", retry.Err)
	}
	f.runtime.PreparedLoads <- retry
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 1
	f.commitStage()
	first := s1nBody(t, f, 0)
	adopted := s1qManaged(t, f, first)
	if owner.snapshot().Bytes <= 0 {
		t.Fatal("partial restored commit lost pending sibling credit")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if f.cmd.EntityExists(first) || f.assets.managedVoxelEntry(adopted) != nil || owner.snapshot().Bytes != 0 || f.assets.PreparedVoxelRendererCopyStats().Entries != 0 {
		t.Fatal("partial restored Stop retained entity, managed authority, derivative, or credit")
	}
}

func TestS1qRestoredStaleAndCancelledResultsReleaseWithoutPublication(t *testing.T) {
	for _, terminal := range []string{"stale", "cancelled", "stop"} {
		t.Run(terminal, func(t *testing.T) {
			f, _, _, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			p := <-f.runtime.PreparedLoads
			alias := p
			switch terminal {
			case "stale":
				p.Generation = f.runtime.Generation - 1
				f.runtime.PreparedLoads <- p
				f.commitStage()
			case "cancelled":
				cancel := make(chan struct{})
				close(cancel)
				p.prepareCancel = cancel
				p = admitStreamedPreparedChunk(f.runtime.pendingPrepared, p)
				p.release()
			case "stop":
				f.runtime.PreparedLoads <- p
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			}
			alias.release()
			if len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 || f.assets.PreparedVoxelRendererCopyStats().Entries != 0 {
				t.Fatal("terminal restored packet published or retained storage")
			}
		})
	}
}

func TestS1qNativeRestoredWorkerBridgePublishesUnderFiniteNativeWork(t *testing.T) {
	config := DefaultVoxelRtStreamingConfig()
	config.ManagedFrame = gpu.ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1}
	config.SectorLookup = gpu.SectorLookupFrameBudget{Enabled: true, MaxEntries: 64, MaxUploadBytes: 256, MaxStageBytes: 128 << 20}
	config.NativeWork = &gpu.VoxelGPUWorkBudget{Enabled: true, MaxCreateBytes: 1 << 20, MaxCreates: 1, MaxCopyBytes: 64 << 10}
	s1nNativeWorkerBridgePublishesBoundedGeometryAndLookup(t, &config, false, func(t *testing.T) (*s1gFixture, string) {
		f, _, path, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
		return f, path
	})
}

func TestS1qMixedFullAndDeltaItemsHaveIndependentPersistenceProof(t *testing.T) {
	f, part, assetPath, snapshotPath, want := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
	header, _, err := content.LoadCompiledAssetHeader(assetPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	fullPart := header.Asset.Parts[0]
	fullPart.ID, fullPart.Name = "full", "Full snapshot item"
	header.Asset.Parts = append(header.Asset.Parts, fullPart)
	fullShape := header.Shapes[0]
	fullShape.PartID = fullPart.ID
	header.Shapes = append(header.Shapes, fullShape)
	if _, err := content.SaveCompiledAssetHeader(assetPath, header, nil); err != nil {
		t.Fatal(err)
	}
	_, lattice, _ := e2b1Canonical(t, part)
	// Equal resolved geometry must not let the full item inherit the neighboring
	// delta item's proof, even when compiled full geometry is shared.
	e2b2Save(t, f, snapshotPath+"-full.gkvoxobj", &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: s1gID(0, 0), ItemID: fullPart.ID, Lattice: lattice, Voxels: want.Voxels})
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	deltaEntity := s1nBody(t, f, 0)
	fullEntity := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), fullPart.ID)
	if fullEntity == 0 {
		t.Fatal("mixed full item missing")
	}
	deltaID, fullID := s1qManaged(t, f, deltaEntity), s1qManaged(t, f, fullEntity)
	if deltaID == fullID {
		t.Fatal("mixed items share restored managed authority")
	}
	e2b3Qualified(t, f, deltaEntity, s1gID(0, 0), part)
	e2b3Fallback(t, f, f.runtime, fullEntity, s1gID(0, 0), fullPart.ID)
	for _, eid := range []EntityId{deltaEntity, fullEntity} {
		e2b4Edit(t, f, eid, volume.VoxelWrite{X: 1, Value: 4})
	}
	deltaPath := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	durable := s4aLoadDelta(t, deltaPath)
	if len(durable.VoxelObjectOverrides) != 2 {
		t.Fatal("mixed items did not independently persist")
	}
	for _, ref := range durable.VoxelObjectOverrides {
		path := ref.SnapshotPath
		if !filepath.IsAbs(path) {
			path = filepath.Join(filepath.Dir(deltaPath), path)
		}
		payload, _, err := content.LoadVoxelObjectPayload(path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ref.ItemID == "body" {
			if payload.Mode != content.VoxelObjectPayloadBaseDelta || payload.BaseIdentity == "" {
				t.Fatal("delta item lost its own proof")
			}
		} else if ref.ItemID != fullPart.ID || payload.Mode != content.VoxelObjectPayloadFull || payload.BaseIdentity != "" {
			t.Fatal("full item inherited placement-level delta proof")
		}
	}
}

func TestS1qInvalidRestoredBindingsFailWithoutPublicationOrCredit(t *testing.T) {
	for _, invalid := range []string{"owner", "lattice"} {
		t.Run(invalid, func(t *testing.T) {
			f, part, _, snapshotPath, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
			payload := e2b2Payload(t, part, s1gID(0, 0), "body", content.VoxelObjectPayloadBaseDelta, 7, false)
			if invalid == "owner" {
				payload.PlacementID = "another-placement"
			} else {
				payload.Lattice.VoxelResolution = 2
			}
			if _, err := content.SaveVoxelObjectPayload(snapshotPath, payload, nil); err != nil {
				t.Fatal(err)
			}
			p := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(f.runtime, ChunkCoord{}))
			defer p.release()
			if p.Err == nil {
				t.Fatal("invalid restored binding accepted")
			}
			if len(p.ObjectSnapshots) != 0 || len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 || len(f.runtime.snapshotGeometryAssets) != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 || f.assets.PreparedVoxelRendererCopyStats().Entries != 0 {
				t.Fatal("failed binding retained geometry, lease, derivative, or pending credit")
			}
		})
	}
}

func TestS1qRestoredHookOverrideReplacementReleasesOnlyAutomaticOwner(t *testing.T) {
	f, _, _, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
	var adopted, replacement AssetId
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, ctx PostSpawnPlacementContext) {
		eid := ctx.SpawnResult.EntitiesByAssetID["body"]
		adopted = s1qManaged(t, f, eid)
		custom := volume.NewXBrickMap()
		custom.SetVoxel(-9, 0, 0, 8)
		replacement = f.assets.RegisterSharedVoxelGeometry(custom, "hook-owned")
		vm := *s3cComponent[VoxelModelComponent](t, cmd, eid)
		vm.OverrideGeometry = replacement
		cmd.AddComponents(eid, vm)
	}}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := s1nBody(t, f, 0)
	if s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry != replacement {
		t.Fatal("automatic restore overwrote hook replacement")
	}
	p5cAssetPresent(t, f, adopted, false)
	if _, leased := f.runtime.snapshotGeometryAssets[eid]; leased {
		t.Fatal("retired automatic lease claimed foreign hook replacement")
	}
	state := s1nRenderer(f)
	s1nBridge(f, state)
	s1l6NoInput(t, state.GetVoxelObject(eid))
	p1dVoxel(t, state.GetVoxelObject(eid).XBrickMap, -9, 8)
	if f.assets.PreparedVoxelRendererCopyStats().Entries != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 {
		t.Fatal("hook replacement retained automatic derivative or credit")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	p5cAssetPresent(t, f, replacement, true)
}

func TestS1qWholeChunkCommitRevalidatesRestoredManagedProof(t *testing.T) {
	for _, change := range []string{"unchanged", "changed-delta", "equal-full"} {
		t.Run(change, func(t *testing.T) {
			f, part, _, path, want := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
			f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 0
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			if change == "changed-delta" {
				payload := e2b2Payload(t, part, s1gID(0, 0), "body", content.VoxelObjectPayloadBaseDelta, 9, false)
				e2b2Save(t, f, path, payload)
				base, lattice, _ := e2b1Canonical(t, part)
				var err error
				want, err = content.ResolveVoxelObjectPayload(payload, base, lattice, s1gID(0, 0), "body", nil)
				if err != nil {
					t.Fatal(err)
				}
			} else if change == "equal-full" {
				_, lattice, _ := e2b1Canonical(t, part)
				e2b2Save(t, f, path, &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: s1gID(0, 0), ItemID: "body", Lattice: lattice, Voxels: want.Voxels})
			}
			f.commitStage()
			eid := s1nBody(t, f, 0)
			vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if change == "unchanged" {
				s1qManaged(t, f, eid)
			} else if f.assets.managedVoxelEntry(vm.GeometryAsset()) != nil {
				t.Fatal("whole chunk commit adopted changed payload proof")
			}
			geometry, _ := f.assets.getVoxelGeometry(vm.GeometryAsset())
			s1qGeometry(t, geometry.XBrickMap, want)
			if change == "equal-full" {
				e2b3Enable(t, f, eid)
				e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
			}
		})
	}
}

func TestS1qExactRestoredConstructionPressureRetainsCompositeReservation(t *testing.T) {
	f, _, _, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 2)
	job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{})
	job.pendingOwner = nil
	complete := prepareStreamedChunkLoad(job)
	defer complete.release()
	if complete.Err != nil {
		t.Fatal(complete.Err)
	}
	// Retain the resolved immutable input envelope and remove constructed
	// component handles to measure the ordinary composite prebuild reservation.
	resolved := complete
	resolved.objectSnapshotGeometry = nil
	resolved.managedPreparedAssets = nil
	resolved.preparedTerrainGeometry, resolved.terrainRegistration = nil, nil
	resolved.PreparedImportedWorldGeometry, resolved.geometrySource, resolved.registration = nil, nil, nil
	initial, err := streamedChunkPrebuildCharge(resolved, job)
	if err != nil || initial <= 0 {
		t.Fatal("initial composite admission fixture", err)
	}
	owner := newStreamedPendingPreparedOwner(initial + 1)
	other, accepted := owner.reserve(1)
	if !accepted {
		t.Fatal("competing credit fixture")
	}
	defer other.release()
	job.pendingOwner = owner
	denied := prepareStreamedChunkLoad(job)
	defer denied.release()
	if denied.Err != nil || denied.retryCost <= initial+1 || denied.pendingCredit != nil || len(denied.ObjectSnapshots) != 0 || len(denied.objectSnapshotGeometry) != 0 || len(denied.compiledAssets) != 0 || owner.snapshot().Bytes != 1 || len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 {
		t.Fatal("exact restored constructor peak bypassed pressure or retained provisional storage", denied.Err)
	}
	other.release()
	retry := prepareStreamedChunkLoad(job)
	if retry.Err != nil || retry.retryCost != 0 || retry.pendingCredit == nil {
		retry.release()
		t.Fatal("sole oversized restored construction did not retry", retry.Err)
	}
	if owner.snapshot().Bytes <= initial || owner.snapshot().OversizedAdmissions != 1 {
		retry.release()
		t.Fatal("restored construction did not retain its additional composite peak")
	}
	alias := retry
	retry.release()
	alias.release()
	if owner.snapshot().Bytes != 0 {
		t.Fatal("restored construction retry release retained credit")
	}
}

func TestS1qRelativeCompiledAssetPathKeepsManagedRestoreAndPersistence(t *testing.T) {
	f, part, assetPath, _, _ := s1qRestore(t, content.VoxelObjectPayloadBaseDelta, true, 1)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, assetPath)
	if err != nil || filepath.IsAbs(relative) {
		t.Fatal("relative compiled input fixture", err)
	}
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = relative
	f.runtime.Level.Placements[0].AssetPath = relative
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := s1nBody(t, f, 0)
	s1qManaged(t, f, eid)
	e2b3Qualified(t, f, eid, s1gID(0, 0), part)
	e2b4Edit(t, f, eid, volume.VoxelWrite{X: 1, Value: 4})
	current, _, ok := currentVoxelMapForEntity(f.cmd, eid)
	if !ok {
		t.Fatal("relative restored CPU authority missing")
	}
	want := VoxelObjectSnapshotFromXBrickMap(current)
	e2b4Depart(f)
	e2b4Unloaded(t, f)
	durable := e2b4Durable(t, f)
	e2b4Delta(t, durable, part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {}, {X: 1, Value: 4}})
	f.hooks[s1gID(0, 0)] = 0
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	reloaded := s1nBody(t, f, 0)
	fresh := s1qManaged(t, f, reloaded)
	e2b3Qualified(t, f, reloaded, s1gID(0, 0), part)
	geometry, _ := f.assets.getVoxelGeometry(fresh)
	s1qGeometry(t, geometry.XBrickMap, want)
}
