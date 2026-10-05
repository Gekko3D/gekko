package gekko

import (
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Fake only queued GPU upload facts; production readiness still validates exact
// target, revision, object material table and pending generation.
func c3h13cUploaded(state *VoxelRtState, obj *core.VoxelObject, target *volume.XBrickMap) {
	previous := state.RtApp.BufferManager
	// The fake below models a completed private queued upload. Clear the
	// original proof before copying; GPU P4 tests own certified-block acknowledgement.
	obj.SetImmutableMaterialTable(nil)
	fake := *obj
	fake.XBrickMap = target
	completeStreamedVoxelUpload(state, &fake)
	manager := state.RtApp.BufferManager
	material := manager.MaterialAllocations[&fake]
	delete(manager.MaterialAllocations, &fake)
	manager.MaterialAllocations[obj] = material
	if previous != nil {
		for key, value := range previous.Allocations {
			if manager.Allocations[key] == nil {
				manager.Allocations[key] = value
			}
		}
		for key, value := range previous.SectorToInfo {
			if _, exists := manager.SectorToInfo[key]; !exists {
				manager.SectorToInfo[key] = value
			}
		}
		for key, value := range previous.MaterialAllocations {
			if key != obj {
				manager.MaterialAllocations[key] = value
			}
		}
	}
}

func c3h13cCandidate(t *testing.T, f *c3h13bFixture) (qualifiedCompiledAssetLOD, bool) {
	t.Helper()
	var q compiledAssetLODQualification
	return q.candidate(f.assets, f.intent, f.vox, f.object)
}

func c3h13cSync(t *testing.T, state *VoxelRtState, entity EntityId, f *c3h13bFixture, wantCoarse bool) bool {
	t.Helper()
	candidate, ok := c3h13cCandidate(t, f)
	return state.syncCompiledAssetLOD(entity, f.object, candidate, ok, wantCoarse)
}

func c3h13cController(t *testing.T) (*VoxelRtState, EntityId, *c3h13bFixture) {
	t.Helper()
	f := c3h13bPrepare(t)
	state := newVoxelRtStateTest()
	entity := EntityId(73)
	state.instanceMap[entity] = f.object
	return state, entity, f
}

func c3h13cCoarseReady(t *testing.T, state *VoxelRtState, entity EntityId, f *c3h13bFixture, wantCoarse bool) {
	t.Helper()
	if !c3h13cSync(t, state, entity, f, wantCoarse) || f.object.RenderVoxelMap() != f.coarse || f.object.PendingFullUploadMap() != nil {
		t.Fatal("cold compiled instance did not wait on coarse-only first upload")
	}
	c3h13cUploaded(state, f.object, f.coarse)
	state.refreshCompiledAssetLODStatuses()
	if c3h13cSync(t, state, entity, f, wantCoarse) {
		t.Fatal("ready coarse display still locally waiting")
	}
}

func TestC3h13cCoarseFirstFineStagingAndDelayedQualifiedPromotion(t *testing.T) {
	state, entity, f := c3h13cController(t)
	fine, transform := f.object.XBrickMap, *f.object.Transform
	c3h13cCoarseReady(t, state, entity, f, false)
	if f.object.XBrickMap != fine || *f.object.Transform != transform || f.object.RenderVoxelMap() != f.coarse || f.object.PendingFullUploadMap() != fine {
		t.Fatal("coarse-first staging changed CPU fine authority or transform")
	}
	generation := f.object.PendingFullUploadGeneration()
	if generation == 0 {
		t.Fatal("fine upload staging lacks request identity")
	}
	// Several pre-Update syncs cannot invent upload readiness or promote.
	for i := 0; i < 2; i++ {
		if c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != f.coarse || f.object.PendingFullUploadGeneration() != generation {
			t.Fatal("unstamped staging promoted/restaged early")
		}
	}
	c3h13cUploaded(state, f.object, fine)
	if c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != f.coarse {
		t.Fatal("allocation-ready fine promoted before post-Update observation")
	}
	state.refreshCompiledAssetLODStatuses()
	if f.object.RenderVoxelMap() != f.coarse {
		t.Fatal("post-Update observation promoted display inside update phase")
	}
	if c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != fine || f.object.PendingFullUploadMap() != nil || f.object.XBrickMap != fine || *f.object.Transform != transform {
		t.Fatal("matching prior fine readiness stamp did not promote on next sync")
	}
}

func TestC3h13cCoarseDemandCancelsStagingAndNearRestagesNewGeneration(t *testing.T) {
	state, entity, f := c3h13cController(t)
	c3h13cCoarseReady(t, state, entity, f, true)
	if f.object.PendingFullUploadMap() != nil {
		t.Fatal("far demand staged fine unnecessarily")
	}
	if c3h13cSync(t, state, entity, f, false) || f.object.PendingFullUploadMap() != f.object.XBrickMap {
		t.Fatal("near demand failed to stage fine behind ready coarse")
	}
	old := f.object.PendingFullUploadGeneration()
	c3h13cUploaded(state, f.object, f.object.XBrickMap)
	state.refreshCompiledAssetLODStatuses()
	if c3h13cSync(t, state, entity, f, true) || f.object.RenderVoxelMap() != f.coarse || f.object.PendingFullUploadMap() != nil {
		t.Fatal("far demand failed to cancel ready pending promotion")
	}
	if c3h13cSync(t, state, entity, f, false) || f.object.PendingFullUploadGeneration() <= old || f.object.RenderVoxelMap() != f.coarse {
		t.Fatal("near demand reused retired staging generation")
	}
}

func TestC3h13cStaleFineStampsCannotPromote(t *testing.T) {
	for _, kind := range []string{"generation", "fine-pointer", "fine-id", "fine-revision", "material", "intent"} {
		t.Run(kind, func(t *testing.T) {
			state, entity, f := c3h13cController(t)
			c3h13cCoarseReady(t, state, entity, f, false)
			c3h13cUploaded(state, f.object, f.object.XBrickMap)
			state.refreshCompiledAssetLODStatuses()
			switch kind {
			case "generation":
				f.object.ClearPendingFullUpload()
				if !f.object.SetPendingFullUpload() {
					t.Fatal("fresh staging fixture failed")
				}
			case "fine-pointer":
				f.object.XBrickMap = f.object.XBrickMap.Copy()
			case "fine-id":
				f.object.XBrickMap.ID++
			case "fine-revision":
				f.object.XBrickMap.Revision++
			case "material":
				f.object.MaterialTable[f.binding.proof.value].Transparency = .5
			case "intent":
				f.intent = compiledAssetLODComponent{}
			}
			candidate, qualified := c3h13cCandidate(t, f)
			if kind == "intent" || kind == "material" {
				if qualified {
					t.Fatal("changed eligibility still qualified")
				}
			}
			state.syncCompiledAssetLOD(entity, f.object, candidate, qualified, false)
			if (kind == "generation" || kind == "fine-pointer" || kind == "fine-id" || kind == "fine-revision") && f.object.RenderVoxelMap() == f.object.XBrickMap {
				t.Fatal("stale capture promoted fine", kind)
			}
			if !qualified && f.object.PendingFullUploadMap() != nil {
				t.Fatal("ineligible instance retained fine staging")
			}
		})
	}
}

func TestC3h13cInvalidColdFallbackWaitsButInitializedFineEditsRemainVisible(t *testing.T) {
	state, entity, f := c3h13cController(t)
	if !c3h13cSync(t, state, entity, f, true) {
		t.Fatal("cold upload fixture already ready")
	}
	f.object.MaterialTable[f.binding.proof.value].Transparency = .5
	if !c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != f.object.XBrickMap || f.object.PendingFullUploadMap() != nil {
		t.Fatal("cold ineligibility did not wait on explicit full fallback")
	}
	c3h13cUploaded(state, f.object, f.object.XBrickMap)
	state.refreshCompiledAssetLODStatuses()
	if c3h13cSync(t, state, entity, f, false) {
		t.Fatal("ready invalid fallback still waiting")
	}
	// Once full is initialized, ordinary tracked edits use existing dirty uploads
	// and do not become a new cold handoff hidden period.
	f.object.XBrickMap.SetVoxel(-2, 0, 0, 91)
	if c3h13cSync(t, state, entity, f, false) {
		t.Fatal("initialized full edit hid legacy dirty-update rendering")
	}
}

type c3h13cBridgeFixture struct {
	app    *App
	cmd    *Commands
	assets *AssetServer
	state  *VoxelRtState
	entity EntityId
	part   preparedAuthoredPart
}

func c3h13cBridge(t *testing.T, streamed bool) *c3h13cBridgeFixture {
	t.Helper()
	path := c3h13aFixture(t, "eligible")
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
	if err != nil {
		t.Fatal(err)
	}
	part := prepared.parts["shape"]
	f := &c3h13cBridgeFixture{app: NewApp(), assets: assets, state: newVoxelRtStateTest(), part: part}
	f.cmd = f.app.Commands()
	components := []any{&TransformComponent{Position: mgl32.Vec3{0, 0, -10}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, &VoxelModelComponent{SharedGeometry: part.model, VoxelPalette: part.palette, VoxelResolution: prepared.def.Parts[0].VoxelResolution}, &compiledAssetLODComponent{fullID: part.model, coarseID: part.compiledLOD}, &EntityLODComponent{SelectionValid: true, ActiveRepresentation: EntityLODRepresentationSimplifiedVoxel}}
	if streamed {
		components = append(components, &StreamedVoxelRenderComponent{Ticket: 103, Generation: 7, Priority: StreamedVoxelPriorityVisible})
	}
	f.entity = f.cmd.AddEntity(components...)
	f.app.FlushCommands()
	return f
}
func (f *c3h13cBridgeFixture) sync() {
	voxelRtSystem(nil, f.state, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
}

func TestC3h13cActualBridgeKeepsFineAuthorityAndLocalReadinessHidden(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		f := c3h13cBridge(t, streamed)
		f.sync()
		obj := f.state.GetVoxelObject(f.entity)
		binding := c3h12bBinding(t, f.assets, f.part.model)
		if obj == nil || obj.XBrickMap.GetVoxelCount() != 4 || obj.RenderVoxelMap() != f.assets.voxModels[binding.coarseID].XBrickMap || obj.RenderEnabled || obj.PendingFullUploadMap() != nil {
			t.Fatal("bridge did not choose compiled coarse while keeping cold fine authority", streamed)
		}
		model := s3cComponent[VoxelModelComponent](t, f.cmd, f.entity)
		if model.GeometryAsset() != f.part.model || model.SharedGeometry != f.part.model || model.OverrideGeometry != (AssetId{}) || hasComponentOfType[VoxelRenderHiddenComponent](f.cmd, f.entity) {
			t.Fatal("compiled selection used legacy CPU resampling/hidden component")
		}
		if _, exists := f.assets.voxModelKeys[entityLODCacheKey("simplified", f.part.model, f.part.palette)]; exists {
			t.Fatal("compiled instance allocated legacy CPU simplified geometry")
		}
		expectedScale := mgl32.Vec3{model.VoxelResolution, model.VoxelResolution, model.VoxelResolution}
		if obj.Transform.Scale != expectedScale {
			t.Fatal("compiled render representation changed CPU fine transform", obj.Transform.Scale, expectedScale)
		}
		c3h13cUploaded(f.state, obj, obj.RenderVoxelMap())
		f.state.refreshCompiledAssetLODStatuses()
		f.state.refreshStreamedVoxelStatuses()
		f.sync()
		if f.state.GetVoxelObject(f.entity) != obj || !obj.RenderEnabled || obj.XBrickMap.GetVoxelCount() != 4 {
			t.Fatal("ready coarse failed to become visible on same instance")
		}
		if streamed {
			status := streamedStatus(t, f.state, 103, StreamedVoxelRenderReady)
			if status.MapID != obj.RenderVoxelMap().ID {
				t.Fatal("compiled streamed ticket latched wrong target")
			}
		}
		// Parent visibility is independent of local readiness.
		f.cmd.AddComponents(f.entity, &StreamedVoxelRenderComponent{Ticket: 104, Generation: 8}, &VoxelRenderHiddenComponent{})
		f.app.FlushCommands()
		f.sync()
		if obj.RenderEnabled {
			t.Fatal("ready compiled local state overrode parent hidden marker")
		}
	}
}

func TestC3h13cBridgePrunesRemovedHiddenAndSpriteInstances(t *testing.T) {
	for _, kind := range []string{"remove", "hidden", "sprite"} {
		t.Run(kind, func(t *testing.T) {
			f := c3h13cBridge(t, false)
			f.sync()
			obj := f.state.GetVoxelObject(f.entity)
			if f.state.compiledLODDisplays[f.entity] == nil {
				t.Fatal("bridge did not retain local display controller")
			}
			switch kind {
			case "remove":
				f.cmd.RemoveEntity(f.entity)
			case "hidden":
				f.cmd.AddComponents(f.entity, &VoxelRenderHiddenComponent{})
			case "sprite":
				f.state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
				lod := *s3cComponent[EntityLODComponent](t, f.cmd, f.entity)
				lod.ActiveRepresentation = EntityLODRepresentationDot
				f.cmd.AddComponents(f.entity, &lod)
			}
			f.app.FlushCommands()
			f.sync()
			if f.state.compiledLODDisplays[f.entity] != nil || obj.PendingFullUploadMap() != nil || obj.RenderRepresentationValid() {
				t.Fatal("pruned instance retained owned staging/coarse selection", kind)
			}
			if kind == "sprite" && len(f.state.runtimeSprites) == 0 {
				t.Fatal("compiled opt-in changed existing sprite branch")
			}
		})
	}
}

func TestC3h13cSiblingControllersWithSharedCoarseKeepIndependentStaging(t *testing.T) {
	state, first, f := c3h13cController(t)
	c3h13cCoarseReady(t, state, first, f, false)
	sibling := *f.object
	sibling.XBrickMap = f.object.XBrickMap.Copy()
	sibling.ClearRenderRepresentation()
	other := *f
	other.object = &sibling
	second := EntityId(74)
	state.instanceMap[second] = &sibling
	if !c3h13cSync(t, state, second, &other, true) {
		t.Fatal("sibling stole first instance readiness stamp")
	}
	c3h13cUploaded(state, &sibling, f.coarse)
	state.refreshCompiledAssetLODStatuses()
	c3h13cSync(t, state, second, &other, true)
	if sibling.PendingFullUploadMap() != nil || f.object.PendingFullUploadMap() != f.object.XBrickMap {
		t.Fatal("sibling coarse demand changed first instance staging")
	}
	c3h13cUploaded(state, f.object, f.object.XBrickMap)
	state.refreshCompiledAssetLODStatuses()
	c3h13cSync(t, state, first, f, false)
	if f.object.RenderVoxelMap() != f.object.XBrickMap || sibling.RenderVoxelMap() != f.coarse {
		t.Fatal("one instance promotion changed sibling selection")
	}
}

func TestC3h13cStreamedSelectedTargetReadinessRetargetAndTerminalLatch(t *testing.T) {
	for _, role := range []bool{false, true} {
		state, entity, f := c3h13cController(t)
		if !f.object.SetRenderLOD2(f.coarse) {
			t.Fatal("representation fixture failed")
		}
		marker := StreamedVoxelRenderComponent{Ticket: 201, Generation: 5}
		state.streamedVoxelTickets = map[uint64]*streamedVoxelTicket{201: {status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderPendingBridge, Entity: entity, Generation: 5}}}
		state.adoptStreamedVoxelTarget(entity, marker, f.object, role)
		target := f.object.XBrickMap
		if role {
			target = f.coarse
		}
		status := streamedStatus(t, state, 201, StreamedVoxelRenderUploading)
		if status.MapID != target.ID || status.TargetRevision != target.Revision {
			t.Fatal("ticket adopted wrong role target")
		}
		c3h13cUploaded(state, f.object, target)
		state.refreshStreamedVoxelStatuses()
		streamedStatus(t, state, 201, StreamedVoxelRenderReady)
		f.object.ClearRenderRepresentation()
		f.object.XBrickMap.SetVoxel(0, 0, 0, 99)
		state.refreshStreamedVoxelStatuses()
		streamedStatus(t, state, 201, StreamedVoxelRenderReady)
	}
	state, entity, f := c3h13cController(t)
	f.object.SetRenderLOD2(f.coarse)
	state.streamedVoxelTickets = map[uint64]*streamedVoxelTicket{202: {status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderPendingBridge, Entity: entity, Generation: 6}}}
	state.adoptStreamedVoxelTarget(entity, StreamedVoxelRenderComponent{Ticket: 202, Generation: 6}, f.object, true)
	f.object.ClearRenderRepresentation()
	state.endStreamedVoxelSync()
	streamedStatus(t, state, 202, StreamedVoxelRenderCancelled)
}

func c3h13cFineReady(t *testing.T, state *VoxelRtState, entity EntityId, f *c3h13bFixture) {
	t.Helper()
	c3h13cCoarseReady(t, state, entity, f, false)
	c3h13cUploaded(state, f.object, f.object.XBrickMap)
	state.refreshCompiledAssetLODStatuses()
	if c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != f.object.XBrickMap {
		t.Fatal("fine promotion fixture failed")
	}
}

func TestC3h13cHistoricalFineReadinessDoesNotSurviveCoarseHoldOrEviction(t *testing.T) {
	for _, next := range []string{"near", "invalid"} {
		t.Run(next, func(t *testing.T) {
			state, entity, f := c3h13cController(t)
			c3h13cFineReady(t, state, entity, f)
			c3h13cSync(t, state, entity, f, true)
			if f.object.RenderVoxelMap() != f.coarse {
				t.Fatal("far demand failed to select coarse after initialized fine")
			}
			delete(state.RtApp.BufferManager.Allocations, f.object.XBrickMap)
			if next == "invalid" {
				f.object.MaterialTable[f.binding.proof.value].Transparency = .5
				if !c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != f.object.XBrickMap {
					t.Fatal("historical fine-ready latch exposed evicted invalid fallback")
				}
			} else {
				c3h13cSync(t, state, entity, f, false)
				if f.object.RenderVoxelMap() != f.coarse || f.object.PendingFullUploadMap() != f.object.XBrickMap {
					t.Fatal("historical fine readiness bypassed restaging after coarse hold")
				}
			}
		})
	}
}

func TestC3h13cOpaqueMaterialReplacementInvalidatesReadinessCapture(t *testing.T) {
	for _, phase := range []string{"coarse", "pending-full"} {
		t.Run(phase, func(t *testing.T) {
			state, entity, f := c3h13cController(t)
			if phase == "coarse" {
				c3h13cSync(t, state, entity, f, true)
				c3h13cUploaded(state, f.object, f.coarse)
				state.refreshCompiledAssetLODStatuses()
			} else {
				c3h13cCoarseReady(t, state, entity, f, false)
				c3h13cUploaded(state, f.object, f.object.XBrickMap)
				state.refreshCompiledAssetLODStatuses()
			}
			f.object.MaterialTable = append([]core.Material(nil), f.object.MaterialTable...)
			wait := c3h13cSync(t, state, entity, f, phase == "coarse")
			if f.object.RenderVoxelMap() != f.coarse || (phase == "coarse" && !wait) {
				t.Fatal("new opaque material table reused old GPU readiness capture", phase)
			}
			c3h13cUploaded(state, f.object, f.coarse)
			if phase == "pending-full" {
				c3h13cUploaded(state, f.object, f.object.XBrickMap)
			}
			state.refreshCompiledAssetLODStatuses()
			wait = c3h13cSync(t, state, entity, f, phase == "coarse")
			if wait || (phase == "pending-full" && f.object.RenderVoxelMap() != f.object.XBrickMap) {
				t.Fatal("fresh material upload did not resume qualified display", phase)
			}
		})
	}
}

func TestC3h13cActualBridgeRefreshesPaletteAndRemovedIntentFallback(t *testing.T) {
	for _, kind := range []string{"palette", "removed-intent"} {
		t.Run(kind, func(t *testing.T) {
			f := c3h13cBridge(t, false)
			f.sync()
			obj := f.state.GetVoxelObject(f.entity)
			c3h13cUploaded(f.state, obj, obj.RenderVoxelMap())
			f.state.refreshCompiledAssetLODStatuses()
			f.sync()
			if !obj.RenderEnabled {
				t.Fatal("ready coarse bridge fixture not visible")
			}
			if kind == "palette" {
				palette := f.assets.voxPalettes[f.part.palette]
				palette.VoxPalette[3][3] = 254
				f.assets.voxPalettes[f.part.palette] = palette
			} else {
				f.cmd.RemoveComponents(f.entity, &compiledAssetLODComponent{})
				f.app.FlushCommands()
			}
			f.sync()
			if obj.RenderVoxelMap() != obj.XBrickMap || obj.PendingFullUploadMap() != nil || obj.RenderEnabled {
				t.Fatal("bridge reused stale eligibility or exposed cold full fallback", kind)
			}
			if _, exists := f.assets.voxModelKeys[entityLODCacheKey("simplified", f.part.model, f.part.palette)]; exists {
				t.Fatal("owned invalid fallback entered legacy CPU simplification")
			}
			if kind == "removed-intent" {
				if f.state.compiledLODDisplays[f.entity] == nil {
					t.Fatal("removed intent retired cold full wait prematurely")
				}
				f.sync()
				if obj.RenderEnabled || f.state.compiledLODDisplays[f.entity] == nil {
					t.Fatal("repeat sync lost removed-intent cold fallback wait")
				}
				c3h13cUploaded(f.state, obj, obj.XBrickMap)
				f.state.refreshCompiledAssetLODStatuses()
				f.sync()
				if !obj.RenderEnabled || obj.RenderVoxelMap() != obj.XBrickMap || obj.PendingFullUploadMap() != nil || f.state.compiledLODDisplays[f.entity] != nil {
					t.Fatal("removed intent failed to finish full fallback and retire ownership controller")
				}
			}
		})
	}
}

func TestC3h13cCoarseReadinessStampRejectsChangedPointerIDAndRevision(t *testing.T) {
	for _, kind := range []string{"pointer", "id", "revision"} {
		t.Run(kind, func(t *testing.T) {
			state, entity, f := c3h13cController(t)
			c3h13cSync(t, state, entity, f, true)
			c3h13cUploaded(state, f.object, f.coarse)
			state.refreshCompiledAssetLODStatuses()
			switch kind {
			case "pointer":
				asset := f.assets.voxModels[f.intent.coarseID]
				asset.XBrickMap = asset.XBrickMap.Copy()
				f.assets.voxModels[f.intent.coarseID] = asset
				f.coarse = asset.XBrickMap
			case "id":
				f.coarse.ID++
			case "revision":
				f.coarse.Revision++
			}
			if !c3h13cSync(t, state, entity, f, true) || f.object.RenderVoxelMap() != f.coarse {
				t.Fatal("stale coarse ready capture revealed changed target", kind)
			}
			c3h13cUploaded(state, f.object, f.coarse)
			state.refreshCompiledAssetLODStatuses()
			if c3h13cSync(t, state, entity, f, true) {
				t.Fatal("fresh changed coarse observation still waiting", kind)
			}
		})
	}
}

func TestC3h13cObjectReplacementClearsOldBorrowedStagingAndStartsCold(t *testing.T) {
	state, entity, f := c3h13cController(t)
	c3h13cCoarseReady(t, state, entity, f, false)
	old := f.object
	if old.PendingFullUploadMap() == nil {
		t.Fatal("replacement fixture lacks owned old staging")
	}
	replacement := core.NewVoxelObject()
	replacement.XBrickMap = old.XBrickMap.Copy()
	replacement.MaterialTable = append([]core.Material(nil), old.MaterialTable...)
	transform := *old.Transform
	replacement.Transform = &transform
	f.object = replacement
	state.instanceMap[entity] = replacement
	if !c3h13cSync(t, state, entity, f, false) || replacement.RenderVoxelMap() != f.coarse || replacement.PendingFullUploadMap() != nil {
		t.Fatal("new object inherited old object's readiness/staging capture")
	}
	if old.PendingFullUploadMap() != nil || old.RenderRepresentationValid() {
		t.Fatal("replaced object retained controller-owned coarse/full references")
	}
}

func TestC3h13cUnfinishedSelectedStreamedTicketCancelsOnCoarseIdentityChanges(t *testing.T) {
	for _, kind := range []string{"id", "revision"} {
		t.Run(kind, func(t *testing.T) {
			state, entity, f := c3h13cController(t)
			f.object.SetRenderLOD2(f.coarse)
			state.streamedVoxelTickets = map[uint64]*streamedVoxelTicket{203: {status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderPendingBridge, Entity: entity, Generation: 7}}}
			state.adoptStreamedVoxelTarget(entity, StreamedVoxelRenderComponent{Ticket: 203, Generation: 7}, f.object, true)
			if kind == "id" {
				f.coarse.ID++
			} else {
				f.coarse.Revision++
			}
			state.endStreamedVoxelSync()
			streamedStatus(t, state, 203, StreamedVoxelRenderCancelled)
		})
	}
}

func TestC3h13cBridgeWithoutDistanceBandsStagesFineBehindReadyCoarse(t *testing.T) {
	f := c3h13cBridge(t, false)
	f.cmd.RemoveComponents(f.entity, &EntityLODComponent{})
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil || obj.RenderEnabled || obj.RenderVoxelMap() == obj.XBrickMap || obj.PendingFullUploadMap() != nil {
		t.Fatal("no-band cold instance bypassed coarse-only upload startup")
	}
	c3h13cUploaded(f.state, obj, obj.RenderVoxelMap())
	f.state.refreshCompiledAssetLODStatuses()
	f.sync()
	if !obj.RenderEnabled || obj.PendingFullUploadMap() != obj.XBrickMap || obj.RenderVoxelMap() == obj.XBrickMap {
		t.Fatal("no-band full demand failed to stage fine behind ready coarse")
	}
	c3h13cUploaded(f.state, obj, obj.XBrickMap)
	f.state.refreshCompiledAssetLODStatuses()
	f.sync()
	if !obj.RenderEnabled || obj.RenderVoxelMap() != obj.XBrickMap || obj.PendingFullUploadMap() != nil {
		t.Fatal("no-band bridge did not promote matching previously ready fine")
	}
}

func TestC3h13cOwnedCoarseUnqualifiedFineReplacementWaitsForFreshFullReadiness(t *testing.T) {
	state, entity, f := c3h13cController(t)
	c3h13cCoarseReady(t, state, entity, f, true)
	previous := f.object.XBrickMap
	replacement := previous.Copy()
	replacement.SetVoxel(-2, 0, 0, 99)
	if replacement.ID == previous.ID {
		t.Fatal("replacement fixture lacks new map identity")
	}
	f.object.XBrickMap = replacement
	if _, qualified := c3h13cCandidate(t, f); qualified {
		t.Fatal("edited replacement fixture unexpectedly qualified")
	}
	if !c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != replacement || f.object.RenderRepresentationValid() || f.object.PendingFullUploadMap() != nil {
		t.Fatal("owned coarse replacement exposed uninitialized full target or retained old selection")
	}
	if !c3h13cSync(t, state, entity, f, false) {
		t.Fatal("repeat replacement sync lost cold full readiness ownership")
	}
	c3h13cUploaded(state, f.object, replacement)
	state.refreshCompiledAssetLODStatuses()
	if c3h13cSync(t, state, entity, f, false) || f.object.RenderVoxelMap() != replacement {
		t.Fatal("fresh replacement full readiness failed to complete fallback")
	}
}

func TestC3h13cRejectedCoarseSetterCompletesFullFallbackDespiteFarDemand(t *testing.T) {
	for _, kind := range []string{"same-map", "nil-map"} {
		t.Run(kind, func(t *testing.T) {
			state, entity, f := c3h13cController(t)
			candidate := qualifiedCompiledAssetLOD{binding: f.binding, coarse: f.object.XBrickMap}
			if kind == "nil-map" {
				candidate.coarse = nil
			}
			// Defensive controller input can reject selection even with a claimed
			// qualification. Such a rejection still owns a recoverable full wait.
			if !state.syncCompiledAssetLOD(entity, f.object, candidate, true, true) || f.object.RenderVoxelMap() != f.object.XBrickMap || f.object.PendingFullUploadMap() != nil {
				t.Fatal("rejected coarse setter did not begin cold full fallback")
			}
			c3h13cUploaded(state, f.object, f.object.XBrickMap)
			state.refreshCompiledAssetLODStatuses()
			for i := 0; i < 2; i++ {
				if state.syncCompiledAssetLOD(entity, f.object, candidate, true, true) || f.object.RenderVoxelMap() != f.object.XBrickMap {
					t.Fatal("rejected coarse setter repeatedly reset initialized full fallback", kind)
				}
			}
		})
	}
}
