package gekko

import (
	"reflect"
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l11FeedFixture(t *testing.T, source *volume.XBrickMap) (*s3cVoxelFixture, EntityId, *core.VoxelObject, *gpu.GpuBufferManager, core.ManagedGeometryInput) {
	t.Helper()
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	f.model = f.server.RegisterManagedVoxelGeometry(source, "s1l11-feed")
	eid := f.add(0)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	m := &gpu.GpuBufferManager{}
	m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	f.state.RtApp = &app_rt.App{BufferManager: m, Scene: core.NewScene()}
	if got := m.AdmitManagedGeometry(obj); got != gpu.ManagedGeometryAdmissionAccepted {
		t.Fatalf("admission=%v", got)
	}
	in, _ := m.ManagedGeometryInputs(obj)
	return f, eid, obj, m, in
}
func s1l11Source(xs ...int) *volume.XBrickMap {
	source := volume.NewXBrickMap()
	for _, x := range xs {
		source.SetVoxel(x, 16, 16, 1)
	}
	return source
}
func s1l11Drain(t *testing.T, m *gpu.GpuBufferManager, obj *core.VoxelObject, in core.ManagedGeometryInput, want ...[3]int) {
	t.Helper()
	seen := map[[3]int]int{}
	n := m.ServiceManagedGeometryContent(obj, in, 10000, func(c [3]int) bool { seen[c]++; return true })
	expected := map[[3]int]int{}
	for _, c := range want {
		expected[c]++
	}
	if n != len(want) || !reflect.DeepEqual(seen, expected) {
		t.Fatalf("visits=%v attempts=%d want=%v", seen, n, expected)
	}
	status, ok := m.ManagedGeometryContentStatus(obj)
	if !ok || status.Pending || !status.Input.SameSource(in) || status.Input.Generation() != in.Generation() {
		t.Fatalf("drained status=%+v/%v", status, ok)
	}
}

func TestS1l11FeedFinalizedEditsCoalesceWithoutAdmissionOrCopyService(t *testing.T) {
	f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16, 80))
	sibling := f.add(20)
	f.app.FlushCommands()
	p1dEnable(t, f, sibling)
	f.app.FlushCommands()
	f.sync()
	other := f.state.GetVoxelObject(sibling)
	if m.AdmitManagedGeometry(other) != gpu.ManagedGeometryAdmissionAccepted {
		t.Fatal("sibling admission")
	}
	otherInput, _ := m.ManagedGeometryInputs(other)
	if m.ServiceManagedGeometry(obj, 1) != 1 {
		t.Fatal("copy setup")
	}
	stage, _ := m.ManagedGeometryStage(obj)
	stats := m.ManagedGeometryAdmissionStats()
	for _, writes := range [][]volume.VoxelWrite{nil, {}, {{X: 16, Y: 16, Z: 16, Value: 1}}, {{X: 500, Y: 16, Z: 16}}} {
		p1dApply(t, f, eid, writes...)
	}
	if err := ApplyManagedVoxelWrites(f.cmd, f.server, eid, nil); err != nil {
		t.Fatal(err)
	}
	s1l11Drain(t, m, obj, in)
	calls := 0
	err := ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
		calls++
		status, _ := m.ManagedGeometryContentStatus(obj)
		if status.Pending {
			t.Fatal("work visible before yield")
		}
		yield(volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
		status, _ = m.ManagedGeometryContentStatus(obj)
		if status.Pending {
			t.Fatal("work visible during producer")
		}
		yield(volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 1}) // Transient edit returns to base.
		yield(volume.VoxelWrite{X: 17, Y: 16, Z: 16, Value: 3})
		yield(volume.VoxelWrite{X: 144, Y: 16, Z: 16, Value: 4}) // Added topology is successor work.
	})
	if err != nil || calls != 1 {
		t.Fatalf("error=%v producer calls=%d", err, calls)
	}
	s1l11Drain(t, m, obj, in, [3]int{})
	s1l11Drain(t, m, other, otherInput)
	accepted, next := m.ManagedGeometryInputs(obj)
	after, _ := m.ManagedGeometryStage(obj)
	if !accepted.SameSource(in) || accepted.Generation() != in.Generation() || next.SameSource(next) || m.ManagedGeometryAdmissionStats() != stats || after.Len() != stage.Len() || after.CopiedBytes() != stage.CopiedBytes() {
		t.Fatal("feed changed ledger/admission/copy progress")
	}
	current := s1l10BridgeSector(t, obj, in, [3]int{})
	if current.Generation() <= in.Generation() {
		t.Fatal("feed preceded publication")
	}
	got, _ := current.Sector().CopySector()
	snapshot := volume.NewXBrickMap()
	snapshot.Sectors[[3]int{}] = got
	if present, v := snapshot.GetVoxel(17, 16, 16); !present || v != 3 {
		t.Fatal("finalized content unavailable")
	}
}

func TestS1l11FeedPanicPrefixAndRemovalTombstone(t *testing.T) {
	source := s1l11Source(16)
	f, eid, obj, m, in := s1l11FeedFixture(t, source)
	expected := source.Copy()
	expected.ApplyVoxelWrites(p1dWrites(volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2}))
	token := &struct{}{}
	calls := 0
	func() {
		defer func() {
			if recover() != token {
				t.Fatal("panic value changed")
			}
		}()
		_ = ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
			calls++
			yield(volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
			status, _ := m.ManagedGeometryContentStatus(obj)
			if status.Pending {
				t.Fatal("panic prefix notified before finalization")
			}
			panic(token)
		})
	}()
	if calls != 1 {
		t.Fatal("producer consumed twice")
	}
	s1l11Drain(t, m, obj, in, [3]int{})
	current := s1l10BridgeSector(t, obj, in, [3]int{})
	if current.Generation() <= in.Generation() {
		t.Fatal("panic prefix not finalized")
	}
	sector, _ := current.Sector().CopySector()
	if !reflect.DeepEqual(sector, expected.Sectors[[3]int{}]) {
		t.Fatal("panic prefix payload/material metadata differs from finalized ordinary write")
	}
	brick := sector.PackedBricks[0]
	if brick.Flags&volume.BrickFlagUniformMaterial == 0 || brick.AtlasOffset != 2 {
		t.Fatal("panic prefix material classification was not finalized")
	}
	prefix := volume.NewXBrickMap()
	prefix.Sectors[[3]int{}] = sector
	if present, value := prefix.GetVoxel(16, 16, 16); !present || value != 2 {
		t.Fatal("panic prefix payload unavailable after return")
	}
	p1dApply(t, f, eid, volume.VoxelWrite{X: 16, Y: 16, Z: 16})
	s1l11Drain(t, m, obj, in, [3]int{})
	if s1l10BridgeSector(t, obj, in, [3]int{}).Sector().Present() {
		t.Fatal("removal lost tombstone")
	}
}

func TestS1l11FeedHaloOnlyAuxInvalidation(t *testing.T) {
	source := s1l11Source(31, 32)
	source.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux = []byte{7, 8}
	f, eid, obj, m, in := s1l11FeedFixture(t, source)
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31, Y: 16, Z: 16, Value: 2})
	s1l11Drain(t, m, obj, in, [3]int{}, [3]int{1, 0, 0})
	sector, _ := s1l10BridgeSector(t, obj, in, [3]int{1, 0, 0}).Sector().CopySector()
	if sector.PackedBricks[0].PrecomputedAux != nil {
		t.Fatal("halo auxiliary data not invalidated")
	}
}

func TestS1l11FeedStaleFinalBindingPreservesHistoricJournal(t *testing.T) {
	for _, kind := range []string{"direct-rebind", "derivative", "reinstall", "clear", "exposure", "promotion", "pending-model", "pending-transform", "pending-remove", "pending-special", "group", "lod"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16, 80))
			if m.QueueManagedGeometryContent(obj, in, [3]int{2, 0, 0}) != gpu.ManagedGeometryContentQueued {
				t.Fatal("historic queue")
			}
			stats := m.ManagedGeometryAdmissionStats()
			invalidate := func() {
				vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
				switch kind {
				case "direct-rebind":
					s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 4)
				case "derivative":
					obj.XBrickMap = obj.XBrickMap.Copy()
				case "reinstall":
					obj.SetManagedGeometryProducer(obj.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) {
						t.Fatal("feed captured full input")
						return volume.ManagedGeometryView{}, 0, false
					})
				case "clear":
					obj.SetManagedGeometryProducer(nil, nil)
				case "exposure":
					_, _ = f.server.GetVoxelGeometry(vmc.OverrideGeometry)
				case "promotion":
					if _, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid); err != nil {
						t.Fatal(err)
					}
				case "pending-model":
					vmc.OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 4)
					f.cmd.AddComponents(eid, vmc)
				case "pending-transform":
					f.cmd.RemoveComponents(eid, TransformComponent{})
				case "pending-remove":
					f.cmd.RemoveEntity(eid)
				case "pending-special":
					f.cmd.AddComponents(eid, VoxelBackingComponent{})
				case "group":
					obj.TerrainGroupID = 1
				case "lod":
					if !obj.SetRenderLOD2(obj.XBrickMap.Copy()) {
						t.Fatal("LOD setup")
					}
				}
			}
			invalidate()
			err := ApplyManagedVoxelWrites(f.cmd, f.server, eid, p1dWrites(volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2}))
			if err != nil && kind != "pending-model" && kind != "pending-remove" && kind != "pending-special" && kind != "direct-rebind" {
				t.Fatal(err)
			}
			if kind == "pending-transform" {
				current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
				if present, value := current.GetVoxel(16, 16, 16); !present || value != 2 {
					t.Fatal("pending transform removal prevented CPU publication")
				}
			}
			s1l11Drain(t, m, obj, in, [3]int{2, 0, 0})
			if m.ManagedGeometryAdmissionStats() != stats {
				t.Fatal("stale feed damaged ledger")
			}
		})
	}
}

func TestS1l11FeedDisconnectedDisabledAndUnadmittedAreSilent(t *testing.T) {
	for _, kind := range []string{"missing-state", "missing-app", "missing-manager", "disabled", "unadmitted"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16))
			switch kind {
			case "missing-state":
				delete(f.app.resources, reflect.TypeOf(VoxelRtState{}))
			case "missing-app":
				f.state.RtApp = nil
			case "missing-manager":
				f.state.RtApp.BufferManager = nil
			case "disabled":
				m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
			case "unadmitted":
				m.CancelManagedGeometryInputs(obj)
			}
			p1dApply(t, f, eid, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
			status, ok := m.ManagedGeometryContentStatus(obj)
			if status.Pending {
				t.Fatal("disconnected feed queued work")
			}
			if kind == "disabled" || kind == "unadmitted" {
				if ok || m.ManagedGeometryAdmissionStats() != (gpu.ManagedGeometryAdmissionStats{}) {
					t.Fatal("automatic admission")
				}
			} else {
				s1l11Drain(t, m, obj, in)
			}
		})
	}
}

func TestS1l11FeedOverflowPreservesAdvancedSweepAndLateNotifications(t *testing.T) {
	source := volume.NewXBrickMap()
	for x := 0; x < 1025; x++ {
		source.SetVoxel(x*volume.SectorSize+16, 16, 16, 1)
	}
	f, eid, obj, m, in := s1l11FeedFixture(t, source)
	writes := func(value uint8) {
		if err := ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
			for x := 0; x < 1025; x++ {
				if !yield(volume.VoxelWrite{X: x*volume.SectorSize + 16, Y: 16, Z: 16, Value: value}) {
					return
				}
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	writes(2)
	status, _ := m.ManagedGeometryContentStatus(obj)
	if !status.SweepPending || status.SweepCursor != 0 {
		t.Fatalf("overflow=%+v", status)
	}
	if m.ServiceManagedGeometryContent(obj, in, 7, func([3]int) bool { return true }) != 7 {
		t.Fatal("sweep did not advance")
	}
	writes(3)
	status, _ = m.ManagedGeometryContentStatus(obj)
	if !status.SweepPending || status.SweepCursor != 7 || !status.SweepAgain {
		t.Fatalf("advanced overflow=%+v", status)
	}
	p1dApply(t, f, eid, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 4})
	status, _ = m.ManagedGeometryContentStatus(obj)
	if status.PendingCoordinates != 1 {
		t.Fatalf("late notification=%+v", status)
	}
	n := m.ServiceManagedGeometryContent(obj, in, 10000, func([3]int) bool { return true })
	if n != 1018+1025+1 {
		t.Fatalf("remaining attempts=%d", n)
	}
	status, _ = m.ManagedGeometryContentStatus(obj)
	if status.Pending {
		t.Fatal("sweep failed to drain")
	}
}

func TestS1l11FeedSignedCornerVisitsAllAcceptedHaloSectors(t *testing.T) {
	source := volume.NewXBrickMap()
	var coords [][3]int
	for x := -1; x <= 0; x++ {
		for y := -1; y <= 0; y++ {
			for z := -1; z <= 0; z++ {
				source.SetVoxel(x*volume.SectorSize+16, y*volume.SectorSize+16, z*volume.SectorSize+16, 1)
				coords = append(coords, [3]int{x, y, z})
			}
		}
	}
	source.SetVoxel(-1, -1, -1, 1)
	f, eid, obj, m, in := s1l11FeedFixture(t, source)
	p1dApply(t, f, eid, volume.VoxelWrite{X: -1, Y: -1, Z: -1, Value: 2})
	s1l11Drain(t, m, obj, in, coords...)
}

func TestS1l11FeedPreservesSuccessorAndServicesReservedWorkUnderPressure(t *testing.T) {
	f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16, 80))
	p1dApply(t, f, eid, volume.VoxelWrite{X: 144, Y: 16, Z: 16, Value: 2})
	if m.AdmitManagedGeometry(obj) != gpu.ManagedGeometryAdmissionCoalesced {
		t.Fatal("successor setup")
	}
	_, successor := m.ManagedGeometryInputs(obj)
	if m.ServiceManagedGeometry(obj, 1) != 1 {
		t.Fatal("stage setup")
	}
	stage, _ := m.ManagedGeometryStage(obj)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
	before := m.ManagedGeometryAdmissionStats()
	p1dApply(t, f, eid, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2}, volume.VoxelWrite{X: 144, Y: 16, Z: 16, Value: 3})
	accepted, afterSuccessor := m.ManagedGeometryInputs(obj)
	afterStage, _ := m.ManagedGeometryStage(obj)
	if !accepted.SameSource(in) || accepted.Generation() != in.Generation() || !afterSuccessor.SameSource(successor) || afterSuccessor.Generation() != successor.Generation() || afterStage.Len() != stage.Len() || afterStage.CopiedBytes() != stage.CopiedBytes() || m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("edit changed accepted/successor/stage or pressure ledger")
	}
	if present, value := s1l6Map(t, afterSuccessor).GetVoxel(144, 16, 16); !present || value != 2 {
		t.Fatal("later publication changed frozen successor payload")
	}
	status, _ := m.ManagedGeometryContentStatus(obj)
	if !status.Input.SameSource(in) || status.Input.Generation() != in.Generation() || status.PendingCoordinates != 1 {
		t.Fatalf("accepted notification status=%+v", status)
	}
	s1l11Drain(t, m, obj, in, [3]int{})
}

func TestS1l11FeedGenerationWrapRefusesNotificationsAndPreservesJournal(t *testing.T) {
	f, eid, obj, m, _ := s1l11FeedFixture(t, s1l11Source(16, 80))
	m.CancelManagedGeometryInputs(obj)
	// Set only the numeric boundary in the existing fixture; all observations use
	// immutable inputs and the public manager API.
	id := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
	entry := f.server.managedVoxelEntry(id)
	entry.generation = ^uint64(0)
	binding := f.state.managedVoxelBindings[eid]
	binding.generation = entry.generation
	f.state.managedVoxelBindings[eid] = binding
	if m.AdmitManagedGeometry(obj) != gpu.ManagedGeometryAdmissionAccepted {
		t.Fatal("maximum generation admission")
	}
	in, _ := m.ManagedGeometryInputs(obj)
	if in.Generation() != ^uint64(0) {
		t.Fatal("boundary fixture lost generation")
	}
	if m.QueueManagedGeometryContent(obj, in, [3]int{2, 0, 0}) != gpu.ManagedGeometryContentQueued {
		t.Fatal("historic queue")
	}
	stats := m.ManagedGeometryAdmissionStats()
	p1dApply(t, f, eid, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
	current := s1l6Input(t, obj)
	if current.Generation() != 0 || !current.SameSource(in) {
		t.Fatal("CPU publication did not wrap on same attachment")
	}
	s1l11Drain(t, m, obj, in, [3]int{2, 0, 0})
	if m.ManagedGeometryAdmissionStats() != stats {
		t.Fatal("wrap changed historic ledger")
	}
}
