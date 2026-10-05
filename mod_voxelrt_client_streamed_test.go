package gekko

import (
	"testing"
	"unsafe"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type streamedVoxelFixture struct {
	app    *App
	cmd    *Commands
	server *AssetServer
	state  *VoxelRtState
	entity EntityId
	model  *VoxelModelComponent
	marker *StreamedVoxelRenderComponent
}

func newStreamedVoxelFixture(t *testing.T) *streamedVoxelFixture {
	t.Helper()
	f := &streamedVoxelFixture{app: NewApp(), server: newVoxelRtAssetServerTest(t), state: newVoxelRtStateTest()}
	f.cmd = f.app.Commands()
	f.entity = f.addEntity(101, 9, true)
	f.flush()
	return f
}

func (f *streamedVoxelFixture) flush() {
	f.app.FlushCommands()
	f.refreshComponents()
}

func (f *streamedVoxelFixture) refreshComponents() {
	// Structural changes can move value-backed components or reallocate storage.
	f.model, f.marker = nil, nil
	MakeQuery1[VoxelModelComponent](f.cmd).Map(func(e EntityId, v *VoxelModelComponent) bool {
		if e == f.entity {
			f.model = v
		}
		return true
	})
	MakeQuery1[StreamedVoxelRenderComponent](f.cmd).Map(func(e EntityId, v *StreamedVoxelRenderComponent) bool {
		if e == f.entity {
			f.marker = v
		}
		return true
	})
}

func (f *streamedVoxelFixture) addEntity(ticket, generation uint64, hidden bool) EntityId {
	components := []any{
		&TransformComponent{Position: mgl32.Vec3{0, 0, -10}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{VoxelModel: f.server.CreateCubeModel(8, 8, 8, 1), VoxelPalette: f.server.CreateSimplePalette([4]uint8{200, 200, 200, 255}), PivotMode: PivotModeCenter},
		&StreamedVoxelRenderComponent{Ticket: ticket, Generation: generation, Priority: StreamedVoxelPriorityPrefetch},
	}
	if hidden {
		components = append(components, &VoxelRenderHiddenComponent{})
	}
	return f.cmd.AddEntity(components...)
}

func (f *streamedVoxelFixture) sync() {
	voxelRtSystem(nil, f.state, f.server, &Time{Dt: 1.0 / 60.0}, f.cmd, nil)
}

func streamedStatus(t *testing.T, state *VoxelRtState, ticket uint64, want StreamedVoxelRenderState) StreamedVoxelRenderStatus {
	t.Helper()
	status, ok := state.StreamedVoxelStatus(ticket)
	if !ok || status.State != want {
		t.Fatalf("ticket %d: status=%+v known=%v, want state %v", ticket, status, ok, want)
	}
	return status
}

// This fixture represents a completed private queued upload; no App.Update or
// physical GPU is needed. GPU P4 tests own certified-block acknowledgement.
func completeStreamedVoxelUpload(state *VoxelRtState, obj *core.VoxelObject) {
	obj.SetImmutableMaterialTable(nil) // Preserve rows while modeling private ownership.
	xbm := obj.XBrickMap
	xbm.ClearDirty()
	alloc := &gpu.ObjectGpuAllocation{Sectors: make(map[[3]int]*volume.Sector), Bricks: make(map[[3]int]*[64]*volume.Brick)}
	sectorInfo := make(map[*volume.Sector]gpu.SectorGpuInfo)
	for coord, sector := range xbm.Sectors {
		alloc.Sectors[coord] = sector
		pointers := new([64]*volume.Brick)
		for i := range pointers {
			pointers[i] = sector.GetBrick(i%4, (i/4)%4, i/16)
		}
		alloc.Bricks[coord] = pointers
		slot := uint32(len(sectorInfo))
		sectorInfo[sector] = gpu.SectorGpuInfo{SlotIndex: slot, BrickTableIndex: slot * 64}
	}
	var tablePtr uintptr
	if len(obj.MaterialTable) > 0 {
		tablePtr = uintptr(unsafe.Pointer(&obj.MaterialTable[0]))
	}
	state.RtApp.BufferManager = &gpu.GpuBufferManager{
		Allocations:  map[*volume.XBrickMap]*gpu.ObjectGpuAllocation{xbm: alloc},
		SectorToInfo: sectorInfo,
		MaterialAllocations: map[*core.VoxelObject]*gpu.MaterialGpuAllocation{obj: {
			MaterialCapacity: 256, MaterialTablePtr: tablePtr, MaterialTableLen: len(obj.MaterialTable), BufferGeneration: 7,
		}},
		MaterialBufferGeneration: 7,
	}
}

func TestStreamedVoxelAdoptionPreservesHiddenResidency(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	if _, ok := f.state.StreamedVoxelStatus(101); ok {
		t.Fatal("ticket must remain unknown before bridge observation")
	}
	f.sync()
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil || len(f.state.RtApp.Scene.Objects) != 1 || f.state.RtApp.Scene.Objects[0] != obj {
		t.Fatal("hidden staged entity must remain resident")
	}
	if obj.RenderEnabled {
		t.Fatal("staged object must remain render-disabled")
	}
	status := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	if status.Entity != f.entity || status.Generation != 9 || status.MapID != obj.XBrickMap.ID || status.TargetRevision != obj.XBrickMap.Revision {
		t.Fatalf("adoption must capture actual target and owner's generation: %+v", status)
	}
	if obj.VoxelUploadPriority != uint8(StreamedVoxelPriorityPrefetch) || obj.VoxelUploadOrder != 101 {
		t.Fatal("streamed scheduling metadata must use component priority and ticket")
	}
	target := obj.XBrickMap
	f.cmd.RemoveComponents(f.entity, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	if f.state.GetVoxelObject(f.entity) != obj || obj.XBrickMap != target || !obj.RenderEnabled {
		t.Fatal("revealing staged object must preserve its renderer identity and target")
	}
	f.marker.Priority = StreamedVoxelPriorityCollision
	f.sync()
	if obj.VoxelUploadPriority != uint8(StreamedVoxelPriorityCollision) || obj.VoxelUploadOrder != 101 {
		t.Fatal("priority updates must propagate without rebinding the ticket")
	}
	if got := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading); got.MapID != status.MapID || got.TargetRevision != status.TargetRevision {
		t.Fatal("visibility/priority changes must preserve target")
	}
}

func TestStreamedVoxelBypassesAutomaticEntityLOD(t *testing.T) {
	for _, representation := range []EntityLODRepresentation{EntityLODRepresentationSimplifiedVoxel, EntityLODRepresentationImpostor, EntityLODRepresentationDot} {
		for _, hidden := range []bool{true, false} {
			t.Run(representation.String()+map[bool]string{true: "/hidden", false: "/visible"}[hidden], func(t *testing.T) {
				f := newStreamedVoxelFixture(t)
				f.state.RtApp.RegisterFeature(&app_rt.SpriteFeature{})
				f.cmd.AddComponents(f.entity, &EntityLODComponent{SelectionValid: true, ActiveRepresentation: representation, ActiveDistance: 200})
				if !hidden {
					f.cmd.RemoveComponents(f.entity, &VoxelRenderHiddenComponent{})
				}
				f.flush()
				f.sync()
				geometry, _ := f.server.GetVoxelGeometry(f.model.VoxelModel)
				obj := f.state.GetVoxelObject(f.entity)
				if obj == nil || obj.XBrickMap != geometry.XBrickMap || f.state.RuntimeSpriteCount() != 0 {
					t.Fatal("streamed target must use requested voxel geometry despite automatic LOD")
				}
				streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
			})
		}
	}
}

func TestStreamedVoxelCapturesObjectScopedGeometry(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.model.IsTerrainChunk, f.model.TerrainGroupID, f.model.TerrainChunkSize = true, 1, 8
	f.sync()
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil {
		t.Fatal("expected staged terrain object")
	}
	geometry, _ := f.server.GetVoxelGeometry(f.model.VoxelModel)
	if obj.XBrickMap == geometry.XBrickMap {
		t.Fatal("fixture must exercise existing object-scoped geometry contract")
	}
	status := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	if status.MapID != obj.XBrickMap.ID || status.TargetRevision != obj.XBrickMap.Revision {
		t.Fatal("status must capture runtime object geometry, not the source asset")
	}
}

func TestOrdinaryHiddenVoxelStillLeavesRenderer(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.cmd.RemoveComponents(f.entity, &StreamedVoxelRenderComponent{}, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil || !obj.RenderEnabled {
		t.Fatal("ordinary visible object should render")
	}
	if obj.VoxelUploadPriority != uint8(StreamedVoxelPriorityVisible) || obj.VoxelUploadOrder != uint64(obj.XBrickMap.ID) {
		t.Fatal("ordinary scheduling must use visible priority and map ID")
	}
	f.cmd.AddComponents(f.entity, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	if f.state.GetVoxelObject(f.entity) != nil || len(f.state.RtApp.Scene.Objects) != 0 {
		t.Fatal("ordinary hidden entity must leave renderer residency")
	}
	f.cmd.RemoveComponents(f.entity, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	if obj := f.state.GetVoxelObject(f.entity); obj == nil || !obj.RenderEnabled {
		t.Fatal("ordinary unhidden entity must return to renderer")
	}
}

func TestStreamedVoxelReadinessLatchesAndForgettingIsTerminalOnly(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.state.ForgetStreamedVoxel(999)
	if _, ok := f.state.StreamedVoxelStatus(999); ok {
		t.Fatal("forgetting unknown ticket must not create a record")
	}
	f.sync()
	f.state.ForgetStreamedVoxel(101)
	streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	for i := 0; i < 2; i++ {
		f.state.refreshStreamedVoxelStatuses()
		streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	}
	obj := f.state.GetVoxelObject(f.entity)
	completeStreamedVoxelUpload(f.state, obj)
	// Pending counts describe actual outstanding upload entries.
	obj.XBrickMap.DirtySectors[[3]int{}] = true
	obj.XBrickMap.DirtyBricks[[6]int{}] = true
	f.state.refreshStreamedVoxelStatuses()
	status := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	if status.PendingSectors != 1 || status.PendingBricks != 1 {
		t.Fatalf("outstanding upload counts=%d/%d, want 1/1", status.PendingSectors, status.PendingBricks)
	}
	obj.XBrickMap.ClearDirty()
	f.state.refreshStreamedVoxelStatuses()
	ready := streamedStatus(t, f.state, 101, StreamedVoxelRenderReady)
	if ready.PendingSectors != 0 || ready.PendingBricks != 0 || obj.RenderEnabled {
		t.Fatal("ready target should settle while still hidden")
	}
	obj.XBrickMap.SetVoxel(0, 0, 0, 2)
	f.sync()
	f.state.refreshStreamedVoxelStatuses()
	if got := streamedStatus(t, f.state, 101, StreamedVoxelRenderReady); got.MapID != ready.MapID || got.TargetRevision != ready.TargetRevision || got.Generation != ready.Generation {
		t.Fatal("ready must remain latched to its original target after ordinary edits")
	}
	f.cmd.RemoveComponents(f.entity, &StreamedVoxelRenderComponent{})
	f.flush()
	f.sync()
	streamedStatus(t, f.state, 101, StreamedVoxelRenderReady)
	f.state.ForgetStreamedVoxel(101)
	if _, ok := f.state.StreamedVoxelStatus(101); ok {
		t.Fatal("terminal ticket should be forgotten after owner cleanup")
	}
}

func TestStreamedVoxelCancelsUnfinishedTargetChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*streamedVoxelFixture)
	}{
		{"geometry revision", func(f *streamedVoxelFixture) { f.state.GetVoxelObject(f.entity).XBrickMap.SetVoxel(0, 0, 0, 2) }},
		{"geometry pointer", func(f *streamedVoxelFixture) {
			f.model.VoxelModel = f.server.CreateCubeModel(4, 4, 4, 1)
			f.model.SharedGeometry = f.model.VoxelModel
		}},
		{"entity removed", func(f *streamedVoxelFixture) { f.cmd.RemoveEntity(f.entity) }},
		{"marker removed", func(f *streamedVoxelFixture) { f.cmd.RemoveComponents(f.entity, &StreamedVoxelRenderComponent{}) }},
		{"transform removed", func(f *streamedVoxelFixture) { f.cmd.RemoveComponents(f.entity, &TransformComponent{}) }},
		{"model removed", func(f *streamedVoxelFixture) { f.cmd.RemoveComponents(f.entity, &VoxelModelComponent{}) }},
		{"generation changed", func(f *streamedVoxelFixture) { f.marker.Generation++ }},
		{"ticket changed", func(f *streamedVoxelFixture) { f.marker.Ticket++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStreamedVoxelFixture(t)
			f.sync()
			original := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
			tc.change(f)
			f.flush()
			f.sync()
			f.state.refreshStreamedVoxelStatuses()
			status := streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
			if status.Entity != original.Entity || status.Generation != original.Generation || status.MapID != original.MapID || status.TargetRevision != original.TargetRevision {
				t.Fatal("cancellation must preserve the old ticket's ownership and target")
			}
			f.sync()
			f.state.refreshStreamedVoxelStatuses()
			streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
			if tc.name == "ticket changed" {
				streamedStatus(t, f.state, 102, StreamedVoxelRenderUploading)
			}
			f.cmd.RemoveEntity(f.entity)
			f.flush()
			f.sync()
			f.state.ForgetStreamedVoxel(101)
			if _, ok := f.state.StreamedVoxelStatus(101); ok {
				t.Fatal("cancelled ticket should be forgettable")
			}
		})
	}
}

func TestStreamedVoxelRefreshCancelsDirectTargetMutation(t *testing.T) {
	for _, replace := range []bool{false, true} {
		f := newStreamedVoxelFixture(t)
		f.sync()
		obj := f.state.GetVoxelObject(f.entity)
		if replace {
			obj.XBrickMap = obj.XBrickMap.Copy()
		} else {
			obj.XBrickMap.SetVoxel(0, 0, 0, 2)
		}
		// Mutation after bridge sync must be caught before readiness refresh can latch.
		completeStreamedVoxelUpload(f.state, obj)
		f.state.refreshStreamedVoxelStatuses()
		streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
	}
}

func TestStreamedVoxelInitialAdoptionRequiresTransformAndModel(t *testing.T) {
	for _, tc := range []struct {
		name    string
		missing any
	}{
		{"transform", &TransformComponent{}},
		{"model", &VoxelModelComponent{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newStreamedVoxelFixture(t)
			f.cmd.RemoveComponents(f.entity, tc.missing)
			f.flush()
			if _, known := f.state.StreamedVoxelStatus(101); known {
				t.Fatal("malformed entity must still be unknown before bridge observation")
			}
			f.sync()
			failed := streamedStatus(t, f.state, 101, StreamedVoxelRenderFailed)
			if failed.Failure == "" || failed.Entity != f.entity || failed.Generation != 9 {
				t.Fatalf("malformed initial adoption must fail with diagnostic and owner metadata: %+v", failed)
			}
			f.state.refreshStreamedVoxelStatuses()
			streamedStatus(t, f.state, 101, StreamedVoxelRenderFailed)
		})
	}
}

func TestStreamedVoxelFailedAdoptionIsDiagnosticAndTerminal(t *testing.T) {
	for _, missing := range []string{"geometry", "palette"} {
		t.Run(missing, func(t *testing.T) {
			f := newStreamedVoxelFixture(t)
			geometryID, paletteID := f.model.VoxelModel, f.model.VoxelPalette
			if missing == "geometry" {
				missingID := makeAssetId()
				f.model.VoxelModel, f.model.SharedGeometry = missingID, missingID
			} else {
				f.model.VoxelPalette = makeAssetId()
			}
			f.sync()
			failed := streamedStatus(t, f.state, 101, StreamedVoxelRenderFailed)
			if failed.Failure == "" || failed.Entity != f.entity || failed.Generation != 9 {
				t.Fatalf("failed adoption must identify owner and explain failure: %+v", failed)
			}
			f.model.VoxelModel, f.model.SharedGeometry, f.model.VoxelPalette = geometryID, geometryID, paletteID
			f.sync()
			f.state.refreshStreamedVoxelStatuses()
			streamedStatus(t, f.state, 101, StreamedVoxelRenderFailed)
			f.cmd.RemoveEntity(f.entity)
			f.flush()
			f.sync()
			f.state.ForgetStreamedVoxel(101)
			if _, ok := f.state.StreamedVoxelStatus(101); ok {
				t.Fatal("failed ticket should be forgettable")
			}
		})
	}
	t.Run("zero palette keeps default material behavior", func(t *testing.T) {
		f := newStreamedVoxelFixture(t)
		f.model.VoxelPalette = AssetId{}
		f.sync()
		streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
		if obj := f.state.GetVoxelObject(f.entity); obj == nil || len(obj.MaterialTable) == 0 {
			t.Fatal("zero palette should adopt with default materials")
		}
	})
}

func TestStreamedVoxelDuplicateTicketCannotStealOwnership(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.sync()
	original := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	f.addEntity(101, 99, true)
	f.flush()
	f.sync()
	if got := streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading); got.Entity != original.Entity || got.Generation != original.Generation || got.MapID != original.MapID {
		t.Fatal("duplicate ticket stole existing owner")
	}
	f.cmd.RemoveEntity(f.entity)
	f.flush()
	f.sync()
	streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
	f.sync()
	streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
}

func TestStreamedVoxelNewTicketAdoptsNewGeneration(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.sync()
	f.marker.Ticket, f.marker.Generation = 102, 10
	f.sync()
	streamedStatus(t, f.state, 101, StreamedVoxelRenderCancelled)
	if status := streamedStatus(t, f.state, 102, StreamedVoxelRenderUploading); status.Generation != 10 || status.Entity != f.entity {
		t.Fatal("new ticket must retain generation chosen by streaming owner")
	}
}

func TestStreamedVoxelAtomicParentChildVisibilitySwap(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.cmd.RemoveComponents(f.entity, &VoxelRenderHiddenComponent{})
	child := f.addEntity(102, 9, true)
	f.cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LightComponent{Type: LightTypeDirectional, Color: [3]float32{1, 1, 1}, Intensity: 1, CastsShadows: true},
	)
	f.flush()
	f.sync()
	scene := f.state.RtApp.Scene
	parentObj, childObj := f.state.GetVoxelObject(f.entity), f.state.GetVoxelObject(child)
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	if len(scene.VisibleObjects) != 1 || scene.VisibleObjects[0] != parentObj {
		t.Fatal("parent must cover staged child")
	}
	completeStreamedVoxelUpload(f.state, childObj)
	f.state.refreshStreamedVoxelStatuses()
	streamedStatus(t, f.state, 102, StreamedVoxelRenderReady)
	f.cmd.AddComponents(f.entity, &VoxelRenderHiddenComponent{})
	f.cmd.RemoveComponents(child, &VoxelRenderHiddenComponent{})
	if !parentObj.RenderEnabled || childObj.RenderEnabled {
		t.Fatal("buffered visibility commands must not expose an intermediate swap")
	}
	f.flush()
	f.sync()
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	if len(scene.Objects) != 2 || f.state.GetVoxelObject(f.entity) != parentObj || f.state.GetVoxelObject(child) != childObj {
		t.Fatal("atomic handoff must retain both resident identities")
	}
	if len(scene.VisibleObjects) != 1 || scene.VisibleObjects[0] != childObj || len(scene.ShadowObjects) != 1 || scene.ShadowObjects[0] != childObj {
		t.Fatal("one bridge/scene commit must expose only the final child coverage")
	}
}
