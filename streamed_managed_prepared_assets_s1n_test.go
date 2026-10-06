package gekko

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func s1nRuntime(t *testing.T, enabled bool, count int) (*s1gFixture, string) {
	t.Helper()
	f, path := c3f3Runtime(t, count, count)
	f.runtime.Config.EnableManagedPreparedAssets = enabled
	return f, path
}

func s1nBody(t *testing.T, f *s1gFixture, index int) EntityId {
	t.Helper()
	eid := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, index), "body")
	if eid == 0 {
		t.Fatal("compiled ordinary placement missing")
	}
	return eid
}

func s1nRenderer(f *s1gFixture) *VoxelRtState {
	if state := voxelRtStateFromApp(f.app); state != nil {
		return state
	}
	state := s1nRenderer(f)
	f.cmd.AddResources(state)
	return state
}

func s1nBridge(f *s1gFixture, state *VoxelRtState) {
	voxelRtSystem(nil, state, f.assets, &Time{Dt: 1.0 / 60.0}, f.cmd, nil)
}

func TestS1nOptOutPreservesOrdinaryGlobalPublication(t *testing.T) {
	f, path := s1nRuntime(t, false, 2)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	packet := c3f3Packet(t, prepared, path)
	shape := packet.shapes[packet.parts["body"]]
	if len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 {
		t.Fatal("worker published live assets")
	}
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	a := s3cComponent[VoxelModelComponent](t, f.cmd, s1nBody(t, f, 0))
	b := s3cComponent[VoxelModelComponent](t, f.cmd, s1nBody(t, f, 1))
	if a.OverrideGeometry != (AssetId{}) || b.OverrideGeometry != (AssetId{}) || a.SharedGeometry != b.SharedGeometry || len(f.assets.managedVoxelGeometry) != 0 {
		t.Fatal("zero config changed ordinary ownership")
	}
	if f.assets.authoredVoxelBaseIdentity(a.SharedGeometry, shape.lattice) != shape.baseIdentity || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("opt-out lost provenance or pending credit")
	}
	state := s1nRenderer(f)
	s1nBridge(f, state)
	s1l6NoInput(t, state.GetVoxelObject(s1nBody(t, f, 0)))
}

func TestS1nColdWorkerCommitHasIndependentManagedAuthorityAndRenderer(t *testing.T) {
	f, path := s1nRuntime(t, true, 2)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	packet := c3f3Packet(t, prepared, path)
	shape := packet.shapes[packet.parts["body"]]
	if len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != 0 {
		t.Fatal("worker mutated ECS or AssetServer")
	}
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	first, second := s1nBody(t, f, 0), s1nBody(t, f, 1)
	a := *s3cComponent[VoxelModelComponent](t, f.cmd, first)
	b := *s3cComponent[VoxelModelComponent](t, f.cmd, second)
	if a.OverrideGeometry == (AssetId{}) || f.assets.managedVoxelEntry(a.OverrideGeometry) == nil {
		t.Fatal("opt-in cold compiled placement has no managed authority")
	}
	if a.SharedGeometry != b.SharedGeometry || b.OverrideGeometry != (AssetId{}) {
		t.Fatal("cold adoption changed shared global ID or warm compatibility")
	}
	if f.assets.authoredVoxelBaseIdentity(a.SharedGeometry, shape.lattice) != shape.baseIdentity {
		t.Fatal("global authored provenance changed")
	}
	if _, ok := ManagedVoxelGeometryChanges(f.cmd, f.assets, first); !ok {
		t.Fatal("prepared authority has no construction-relative tracking")
	}
	state := s1nRenderer(f)
	beforeCopies := f.assets.PreparedVoxelRendererCopyStats()
	if beforeCopies.Entries != 1 || beforeCopies.Bytes <= 0 {
		t.Fatal("cold adoption did not retain exactly one first renderer derivative")
	}
	s1nBridge(f, state)
	obj := state.GetVoxelObject(first)
	afterCopies := f.assets.PreparedVoxelRendererCopyStats()
	if afterCopies.Adoptions != beforeCopies.Adoptions+1 || afterCopies.Entries != 0 || afterCopies.Bytes != 0 {
		t.Fatal("bridge did not consume first renderer derivative exactly once")
	}
	initial := s1l6Input(t, obj)
	authority, ok := f.assets.getVoxelGeometry(a.OverrideGeometry)
	if !ok || obj.XBrickMap == authority.XBrickMap || obj.XBrickMap == shape.source {
		t.Fatal("authority, source and first renderer derivative alias")
	}
	prepared.release()
	packet.release()
	prepared.release()
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, first, p1dWrites(volume.VoxelWrite{Value: 7})); err != nil {
		t.Fatal(err)
	}
	edited := s1l6Input(t, obj)
	if !initial.SameSource(edited) || edited.Generation() <= initial.Generation() {
		t.Fatal("adopted edit lost producer identity/generation")
	}
	p1dVoxel(t, s1l6Map(t, edited), 0, 7)
	p1dVoxel(t, s1l6Map(t, initial), 0, 1)
	global, _ := f.assets.getVoxelGeometry(a.SharedGeometry)
	p1dVoxel(t, global.XBrickMap, 0, 1)
	// The warm sibling retains legacy ownership until its consumer explicitly enables edits.
	if err := EnableManagedVoxelGeometry(f.cmd, f.assets, second); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	s1nBridge(f, state)
	sibling := s1l6Input(t, state.GetVoxelObject(second))
	if sibling.SameSource(edited) {
		t.Fatal("repeated placements share managed authority")
	}
	p1dVoxel(t, s1l6Map(t, sibling), 0, 1)
	// Neither streamed opt-in nor bridge observation may enable GPU frame policies.
	manager := state.RtApp.BufferManager
	if manager == nil {
		manager = &gpu.GpuBufferManager{}
		state.RtApp.BufferManager = manager
	}
	manager.PrepareManagedGeometryFrame(state.RtApp.Scene)
	if _, admitted := manager.ManagedGeometryGPUStatus(obj); admitted {
		t.Fatal("streamed flag auto-enabled managed GPU service")
	}
	manager.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	manager.SetManagedGeometryFrameBudget(gpu.DefaultManagedGeometryFrameBudget())
	for i := 0; i < 8; i++ {
		s1nBridge(f, state)
		manager.PrepareManagedGeometryFrame(state.RtApp.Scene)
	}
	status, staged := manager.ManagedGeometryGPUStatus(obj)
	if !staged || !status.StageInput.SameSource(status.StageInput) || status.CurrentInput.SameSource(status.CurrentInput) || status.StageReady || obj.RenderVoxelMap() != nil {
		t.Fatal("multi-frame service did not retain a valid hidden managed stage without native certificate")
	}
	state.RtApp.Scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	for _, visible := range state.RtApp.Scene.VisibleObjects {
		if visible == obj {
			t.Fatal("unuploaded managed object entered visible pass")
		}
	}
	if f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("committed prepared records retain credit")
	}
}

func TestS1nWarmMutableSourceFallbackAndLateOverrideExposure(t *testing.T) {
	for _, scenario := range []string{"warm-before-worker", "warm-after-worker", "override-before-bridge", "override-after-bridge"} {
		t.Run(scenario, func(t *testing.T) {
			f, path := s1nRuntime(t, true, 1)
			warm := func() {
				authored, err := LoadAndPrepareAuthoredAsset(path, f.assets, f.runtime.Loader)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := PreparedAuthoredAssetPartGeometry(authored, "body")
				raw, _ := f.assets.GetVoxelGeometry(id)
				raw.XBrickMap.SetVoxel(0, 0, 0, 9)
			}
			if scenario == "warm-before-worker" {
				warm()
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			if scenario == "warm-after-worker" {
				warm()
			}
			f.commitStage()
			eid := s1nBody(t, f, 0)
			model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			state := s1nRenderer(f)
			if scenario == "warm-before-worker" || scenario == "warm-after-worker" {
				if model.OverrideGeometry != (AssetId{}) || f.assets.managedVoxelEntry(model.GeometryAsset()) != nil {
					t.Fatal("warm mutable source rebuilt managed authority")
				}
				s1nBridge(f, state)
				p1dVoxel(t, state.GetVoxelObject(eid).XBrickMap, 0, 9)
				s1l6NoInput(t, state.GetVoxelObject(eid))
				return
			}
			if model.OverrideGeometry == (AssetId{}) {
				t.Fatal("cold placement missing prepared authority")
			}
			if scenario == "override-after-bridge" {
				s1nBridge(f, state)
				s1l6Input(t, state.GetVoxelObject(eid))
			}
			raw, _ := f.assets.GetVoxelGeometry(model.OverrideGeometry)
			raw.XBrickMap.SetVoxel(0, 0, 0, 8)
			s1nBridge(f, state)
			p1dVoxel(t, state.GetVoxelObject(eid).XBrickMap, 0, 8)
			s1l6NoInput(t, state.GetVoxelObject(eid))
		})
	}
}

func TestS1nTerminalWorkerResultsDoNotPublishOrRetainCredit(t *testing.T) {
	for _, terminal := range []string{"cancelled", "stale", "stop"} {
		t.Run(terminal, func(t *testing.T) {
			f, _ := s1nRuntime(t, true, 1)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			prepared := <-f.runtime.PreparedLoads
			alias := prepared
			switch terminal {
			case "cancelled":
				cancel := make(chan struct{})
				close(cancel)
				prepared.prepareCancel = cancel
				prepared = admitStreamedPreparedChunk(f.runtime.pendingPrepared, prepared)
				prepared.release()
			case "stale":
				prepared.Generation = f.runtime.Generation - 1
				f.runtime.PreparedLoads <- prepared
				f.commitStage()
			case "stop":
				f.runtime.PreparedLoads <- prepared
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			}
			alias.release()
			prepared.release()
			if len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 {
				t.Fatal("terminal envelope published or retained prepared storage")
			}
		})
	}
}

func TestS1nOverrideLeaseAndPersistenceProvenanceSurviveUntilUnload(t *testing.T) {
	for _, terminal := range []string{"stop", "unload"} {
		t.Run(terminal, func(t *testing.T) {
			f, _ := s1nRuntime(t, true, 1)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := s1nBody(t, f, 0)
			model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if model.OverrideGeometry == (AssetId{}) {
				t.Fatal("cold placement missing managed override")
			}
			if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != model.OverrideGeometry || lease.Server != f.assets {
				t.Fatal("prepared override bypassed existing chunk-owned lease")
			}
			if identity, _, qualified := managedVoxelPersistenceBase(f.cmd, f.assets, f.runtime, eid, s1gID(0, 0), "body"); !qualified || identity == "" {
				t.Fatal("prepared adoption lost authored persistence provenance")
			}
			if terminal == "stop" {
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
					t.Fatal(err)
				}
				f.app.FlushCommands()
			}
			if _, ok := f.assets.getVoxelGeometry(model.OverrideGeometry); ok {
				t.Fatal("chunk terminal retained managed override")
			}
			if _, ok := f.assets.getVoxelGeometry(model.SharedGeometry); !ok {
				t.Fatal("chunk terminal revoked ordinary shared source")
			}
		})
	}
}

func TestS1nLatestRestoredSnapshotExcludesPreparedAdoption(t *testing.T) {
	f, path := s1nRuntime(t, true, 1)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	payloadPath := filepath.Join(t.TempDir(), "late.gkvoxobj")
	placement := s1gID(0, 0)
	payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: placement, ItemID: "body", Lattice: authoredVoxelShapeLattice(header.Asset.Parts[0].VoxelResolution), Voxels: []content.VoxelObjectVoxelDef{{Value: 5}}}
	if _, err := content.SaveVoxelObjectPayload(payloadPath, payload, nil); err != nil {
		t.Fatal(err)
	}
	f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(placement, "body")] = content.VoxelObjectOverrideDef{PlacementID: placement, ItemID: "body", SnapshotPath: payloadPath}
	f.commitStage()
	eid := s1nBody(t, f, 0)
	model := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	if f.assets.managedVoxelEntry(model.GeometryAsset()) != nil {
		t.Fatal("restored snapshot was rebuilt as a managed worker candidate")
	}
	current, _, ok := currentVoxelMapForEntity(f.cmd, eid)
	if !ok {
		t.Fatal("snapshot geometry missing")
	}
	p1dVoxel(t, current, 0, 5)
}

func TestS1nLateDisableKeepsLegacyCommit(t *testing.T) {
	f, _ := s1nRuntime(t, true, 1)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.runtime.Config.EnableManagedPreparedAssets = false
	f.commitStage()
	model := s3cComponent[VoxelModelComponent](t, f.cmd, s1nBody(t, f, 0))
	if model.OverrideGeometry != (AssetId{}) || len(f.assets.managedVoxelGeometry) != 0 || f.assets.PreparedVoxelRendererCopyStats().Entries != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("late opt-out consumed managed candidate or retained owned storage")
	}
}

func TestS1nDeclaredLODAndCollapseRemainLegacy(t *testing.T) {
	for _, kind := range []string{"lod", "collapse"} {
		t.Run(kind, func(t *testing.T) {
			f, path := s1nRuntime(t, true, 1)
			f.runtime.Config.PlacementHooks = nil
			output := ""
			if kind == "lod" {
				output = c3h13aFixture(t, "eligible")
				header, _, err := content.LoadCompiledAssetHeader(output, nil)
				if err != nil || len(header.LODs) == 0 {
					t.Fatal("LOD exclusion fixture has no declared derivative", err)
				}
			} else {
				header, _, err := content.LoadCompiledAssetHeader(path, nil)
				if err != nil {
					t.Fatal(err)
				}
				header.Asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
				output = filepath.Join(t.TempDir(), "collapsed.gkasset")
				c3cWrite(t, output, header.Asset)
			}
			f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = output
			f.runtime.Level.Placements[0].AssetPath = output
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			prepared := <-f.runtime.PreparedLoads
			if kind == "lod" && len(c3f3Packet(t, prepared, output).partLODs) == 0 {
				t.Fatal("LOD worker omitted declared derivative intent")
			}
			f.runtime.PreparedLoads <- prepared

			f.commitStage()
			models := 0
			MakeQuery1[VoxelModelComponent](f.cmd).Map(func(_ EntityId, model *VoxelModelComponent) bool {
				models++
				if f.assets.managedVoxelEntry(model.GeometryAsset()) != nil || model.OverrideGeometry != (AssetId{}) {
					t.Fatal("unsupported declared LOD/collapse owner gained prepared authority")
				}
				return true
			})
			if models == 0 || f.assets.PreparedVoxelRendererCopyStats().Entries != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("excluded owner failed legacy spawn or leaked derivative/credit")
			}
		})
	}
}

func TestS1nPendingCreditIncludesPreparedStorageAndRejectsSharedPressure(t *testing.T) {
	f, _ := s1nRuntime(t, false, 1)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	legacy := <-f.runtime.PreparedLoads
	legacyCharge := streamedPreparedChunkCharge(legacy)
	legacy.release()
	f.runtime.Config.EnableManagedPreparedAssets = true
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	managedCharge := streamedPreparedChunkCharge(prepared)
	if managedCharge <= legacyCharge || f.runtime.pendingPrepared.snapshot().Bytes != managedCharge {
		t.Fatal("pending credit omitted new managed owner/snapshot/derivative storage")
	}
	alias := prepared
	prepared.release()
	alias.release()
	if f.runtime.pendingPrepared.snapshot().Bytes != 0 {
		t.Fatal("aliased prepared records retained or double-released credit")
	}
	// Existing full/proxy owner credit also gates the new ordinary worker path.
	capBytes := legacyCharge + (managedCharge-legacyCharge)/2
	owner := newStreamedPendingPreparedOwner(capBytes)
	blocking, ok := owner.reserve(capBytes)
	if !ok {
		t.Fatal("pressure reservation fixture")
	}
	defer blocking.release()
	f.runtime.pendingPrepared = owner
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	rejected := <-f.runtime.PreparedLoads
	defer rejected.release()
	if rejected.retryCost <= 0 || rejected.pendingCredit != nil || owner.snapshot().Bytes != capBytes || len(f.assets.voxModels) != 0 || len(f.assets.managedVoxelGeometry) != 0 {
		t.Fatal("ordinary prepared admission bypassed shared pending credit")
	}
	blocking.release()
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	retry := <-f.runtime.PreparedLoads
	if retry.retryCost != 0 || retry.pendingCredit == nil {
		t.Fatal("sole oversized ordinary worker could not retry after competing credit release")
	}
	retry.release()
	if owner.snapshot().Bytes != 0 {
		t.Fatal("retry release retained credit")
	}
}

func TestS1nManagedEditBeforeFirstBridgePreservesCurrentAuthority(t *testing.T) {
	f, _ := s1nRuntime(t, true, 1)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := s1nBody(t, f, 0)
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{Value: 6})); err != nil {
		t.Fatal(err)
	}
	state := s1nRenderer(f)
	s1nBridge(f, state)
	obj := state.GetVoxelObject(eid)
	p1dVoxel(t, s1l6Map(t, s1l6Input(t, obj)), 0, 6)
	p1dVoxel(t, obj.XBrickMap, 0, 6)
	if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Entries != 0 || stats.Bytes != 0 || stats.Adoptions != 0 {
		t.Fatal("pre-bridge managed edit admitted stale prepared derivative")
	}
}

func TestS1nCompiledModelsPreserveDeclaredPivotBounds(t *testing.T) {
	f, _ := s1nRuntime(t, true, 1)
	path, _ := c3g9Fixture(t, "models")
	f.runtime.Config.PlacementHooks = nil
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = path
	f.runtime.Level.Placements[0].AssetPath = path
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "model")
	if eid == 0 {
		t.Fatal("compiled model ordinary placement missing")
	}
	model := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	if model.OverrideGeometry == (AssetId{}) || model.PivotMode != PivotModeCenter || model.VoxelResolution != .125 {
		t.Fatal("compiled model lost managed authority or authored pivot/lattice")
	}
	global, ok := f.assets.getVoxelGeometry(model.SharedGeometry)
	if !ok {
		t.Fatal("global compiled model missing")
	}
	authority, ok := f.assets.getVoxelGeometry(model.OverrideGeometry)
	if !ok || authority.LocalMin != global.LocalMin || authority.LocalMax != global.LocalMax || authority.LocalMin != (mgl32.Vec3{}) || authority.LocalMax != (mgl32.Vec3{2, 0, 4}) {
		t.Fatal("managed model override recomputed authored pivot bounds from occupied geometry")
	}
	state := s1nRenderer(f)
	s1nBridge(f, state)
	if input := s1l6Input(t, state.GetVoxelObject(eid)); s1l6Map(t, input).GetVoxelCount() != 1 {
		t.Fatal("compiled model managed input lost out-of-bounds primary content")
	}
}

func TestS1nPlacementHookTerminalDrainsPublishedManagedOwnership(t *testing.T) {
	for _, terminal := range []string{"stop", "panic"} {
		t.Run(terminal, func(t *testing.T) {
			f, _ := s1nRuntime(t, true, 2)
			var published []EntityId
			var shared AssetId
			sentinel := &struct{}{}
			f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, context PostSpawnPlacementContext) {
				published = append(published, context.SpawnResult.Entities...)
				eid := context.SpawnResult.EntitiesByAssetID["body"]
				model := s3cComponent[VoxelModelComponent](t, cmd, eid)
				shared = model.SharedGeometry
				if model.OverrideGeometry == (AssetId{}) {
					t.Fatal("placement hook did not observe cold managed adoption")
				}
				if terminal == "panic" {
					panic(sentinel)
				}
				if err := StopStreamedLevelRuntime(cmd); err != nil {
					t.Fatal(err)
				}
			}}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			func() {
				defer func() {
					recovered := recover()
					if terminal == "panic" && recovered != sentinel {
						t.Fatal("placement hook panic changed")
					}
					if terminal == "stop" && recovered != nil {
						panic(recovered)
					}
				}()
				commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			}()
			if len(published) == 0 {
				t.Fatal("hook terminal fixture never published its first placement")
			}
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			f.app.FlushCommands()
			for _, eid := range published {
				if f.cmd.EntityExists(eid) {
					t.Fatal("hook terminal left partial placement alive")
				}
			}
			if len(f.assets.managedVoxelGeometry) != 0 || f.assets.PreparedVoxelRendererCopyStats().Entries != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 {
				t.Fatal("hook terminal stranded adopted or provisional managed storage")
			}
			if _, ok := f.assets.getVoxelGeometry(shared); !ok {
				t.Fatal("hook terminal revoked published ordinary global source")
			}
		})
	}
}

func TestS1nPostSpawnUnsupportedOwnersRenderLegacyAndKeepHookEdits(t *testing.T) {
	for _, kind := range []string{"lod", "retained", "backing", "adjacency-group", "edited-retained"} {
		t.Run(kind, func(t *testing.T) {
			f, _ := s1nRuntime(t, true, 1)
			var adopted AssetId
			f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, context PostSpawnPlacementContext) {
				eid := context.SpawnResult.EntitiesByAssetID["body"]
				model := *s3cComponent[VoxelModelComponent](t, cmd, eid)
				adopted = model.OverrideGeometry
				if adopted == (AssetId{}) {
					t.Fatal("hook fixture never received auto-adopted override")
				}
				if kind == "edited-retained" {
					if err := ApplyManagedVoxelWrites(cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{Value: 7})); err != nil {
						t.Fatal(err)
					}
				}
				switch kind {
				case "lod":
					cmd.AddComponents(eid, &EntityLODComponent{})
				case "backing":
					cmd.AddComponents(eid, &VoxelBackingComponent{})
				case "adjacency-group":
					model.VoxelAdjacencyGroupID = 17
					cmd.AddComponents(eid, &model)
				default:
					model.RetainRendererGeometry = true
					cmd.AddComponents(eid, &model)
				}
			}}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := s1nBody(t, f, 0)
			model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if model.OverrideGeometry != adopted {
				t.Fatal("unsupported-owner fallback replaced or discarded hook authority")
			}
			if f.assets.managedVoxelEntry(model.GeometryAsset()) != nil {
				t.Fatal("post-spawn special owner retained sealed managed override")
			}
			expected := uint8(1)
			if kind == "edited-retained" {
				expected = 7
			}
			live, ok := f.assets.getVoxelGeometry(model.GeometryAsset())
			if !ok {
				t.Fatal("fallback authority missing")
			}
			p1dVoxel(t, live.XBrickMap, 0, expected)
			global, ok := f.assets.getVoxelGeometry(model.SharedGeometry)
			if !ok {
				t.Fatal("original shared source missing")
			}
			p1dVoxel(t, global.XBrickMap, 0, 1)
			state := s1nRenderer(f)
			s1nBridge(f, state)
			obj := state.GetVoxelObject(eid)
			if obj == nil || obj.RenderVoxelMap() == nil {
				t.Fatal("unsupported post-spawn owner disappeared instead of rendering legacy geometry")
			}
			s1l6NoInput(t, obj)
			p1dVoxel(t, obj.RenderVoxelMap(), 0, expected)
			if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Entries != 0 || stats.Bytes != 0 {
				t.Fatal("unsupported fallback retained sealed first derivative")
			}
			if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != adopted {
				t.Fatal("fallback authority lost existing chunk lease")
			}
			if kind == "lod" || kind == "edited-retained" {
				if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
					t.Fatal(err)
				}
				f.app.FlushCommands()
			} else {
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			}
			if _, ok := f.assets.getVoxelGeometry(adopted); ok {
				t.Fatal("fallback authority leaked after chunk terminal")
			}
			if _, ok := f.assets.getVoxelGeometry(model.SharedGeometry); !ok {
				t.Fatal("fallback terminal deleted ordinary global source")
			}
		})
	}
}

func TestS1nSamePublicationPartsHaveIndependentManagedOwners(t *testing.T) {
	f, _ := s1nRuntime(t, true, 1)
	path, _ := c3g9Fixture(t, "models")
	f.runtime.Config.PlacementHooks = nil
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = path
	f.runtime.Level.Placements[0].AssetPath = path
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	first := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "model")
	second := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "other-model")
	if first == 0 || second == 0 {
		t.Fatal("same-publication duplicate model parts missing")
	}
	a := s3cComponent[VoxelModelComponent](t, f.cmd, first)
	b := s3cComponent[VoxelModelComponent](t, f.cmd, second)
	if a.SharedGeometry != b.SharedGeometry || a.OverrideGeometry == (AssetId{}) || b.OverrideGeometry == (AssetId{}) || a.OverrideGeometry == b.OverrideGeometry {
		t.Fatal("same cold publication failed independent part adoption under shared global ID")
	}
	if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Entries != 2 {
		t.Fatal("same cold publication missing independent first derivatives")
	}
	state := s1nRenderer(f)
	s1nBridge(f, state)
	initial := s1l6Input(t, state.GetVoxelObject(first))
	sibling := s1l6Input(t, state.GetVoxelObject(second))
	if initial.SameSource(sibling) {
		t.Fatal("same-publication parts share sealed producer")
	}
	snapshot := VoxelObjectSnapshotFromXBrickMap(s1l6Map(t, initial))
	if len(snapshot.Voxels) == 0 {
		t.Fatal("same-publication fixture empty")
	}
	voxel := snapshot.Voxels[0]
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, first, p1dWrites(volume.VoxelWrite{X: voxel.X, Y: voxel.Y, Z: voxel.Z, Value: 7})); err != nil {
		t.Fatal(err)
	}
	current := s1l6Input(t, state.GetVoxelObject(second))
	if !current.SameSource(sibling) || current.Generation() != sibling.Generation() {
		t.Fatal("same-publication sibling inherited edit generation")
	}
	if occupied, value := s1l6Map(t, current).GetVoxel(voxel.X, voxel.Y, voxel.Z); !occupied || value != voxel.Value {
		t.Fatal("same-publication sibling inherited edited content")
	}
	if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Adoptions != 2 || stats.Entries != 0 {
		t.Fatal("same-publication bridge failed single-use derivative transfers")
	}
}
