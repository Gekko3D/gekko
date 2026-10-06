package gpu_test

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"math"
	"reflect"
	"testing"
	"unsafe"
)

type s1l13Producer struct {
	*s1l12Producer
	scalarCalls     int
	scalarAvailable bool
	scalarHook      func()
}

func s1l13NewProducer(n int) *s1l13Producer {
	p := &s1l13Producer{s1l12Producer: s1l12NewProducer(n), scalarAvailable: true}
	// The older fixture uses uint8(i+1), creating accidental tombstones at
	// multiples of 256. Large overflow fixtures need exactly n accepted sectors.
	if n > 254 {
		raw := volume.NewXBrickMap()
		for i := 0; i < n; i++ {
			raw.SetVoxel(i*32, 0, 0, uint8(i%254+1))
		}
		p.owner = volume.NewManagedXBrickMap(raw)
		p.object.XBrickMap = raw.Copy()
	}
	p.object.SetManagedGeometryProducerWithGenerationReader(p.object.XBrickMap,
		func() (volume.ManagedGeometryView, uint64, bool) {
			p.calls++
			v, ok := p.owner.CaptureGeometry()
			return v, p.generation, ok && p.available
		},
		func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
			p.reads++
			p.readCoord = c
			if p.hook != nil {
				p.hook()
			}
			v, ok := p.owner.CaptureSector(c)
			return v, p.generation, ok && p.available
		},
		func() (uint64, bool) {
			p.scalarCalls++
			if p.scalarHook != nil {
				p.scalarHook()
			}
			return p.generation, p.scalarAvailable
		})
	return p
}
func s1l13Fixture(t *testing.T, n int) (*gpu.GpuBufferManager, *s1l13Producer, core.ManagedGeometryInput) {
	t.Helper()
	m, p := s1l7Manager(), s1l13NewProducer(n)
	p.generation = 4
	s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	return m, p, in
}
func s1l13Service(t *testing.T, m *gpu.GpuBufferManager, p *s1l13Producer, in core.ManagedGeometryInput, limit, want int) {
	t.Helper()
	if got := m.ServiceManagedGeometryReconciliation(p.object, in, limit); got != want {
		t.Fatalf("service limit %d attempts %d want %d", limit, got, want)
	}
}
func s1l13Status(t *testing.T, m *gpu.GpuBufferManager, p *s1l13Producer, coherent bool) {
	t.Helper()
	s, ok := m.ManagedGeometryReconciliationStatus(p.object)
	if !ok || !s.Qualified || s.Generation != p.generation || s.Coherent != coherent {
		t.Fatalf("status %+v/%v expected generation %d coherent %v", s, ok, p.generation, coherent)
	}
}
func s1l13Edit(t *testing.T, m *gpu.GpuBufferManager, p *s1l13Producer, in core.ManagedGeometryInput, c [3]int, value uint8) {
	t.Helper()
	previous := p.generation
	p.owner.SetVoxel(c[0]*32, c[1]*32, c[2]*32, value)
	p.generation++
	if m.QueueManagedGeometryContent(p.object, in, c) != gpu.ManagedGeometryContentQueued {
		t.Fatal("queue edit")
	}
	if !m.RecordManagedGeometryContentPublication(p.object, in, previous, p.generation) {
		t.Fatal("continuous publication")
	}
}
func s1l13StageValue(t *testing.T, m *gpu.GpuBufferManager, p *s1l13Producer, index int, want uint8, generation uint64) {
	t.Helper()
	v, ok := m.ManagedGeometryStage(p.object)
	if !ok {
		t.Fatal("stage absent")
	}
	g, ok := v.SectorGeneration(index)
	if !ok || g != generation {
		t.Fatalf("leaf generation %d/%v want %d", g, ok, generation)
	}
	c, _ := v.Coord(index)
	s, ok := v.CopySector(index)
	if !ok {
		t.Fatal("leaf absent")
	}
	if want == 0 {
		if s != nil {
			t.Fatal("tombstone retained sector")
		}
		return
	}
	raw := volume.NewXBrickMap()
	raw.Sectors[c] = s
	present, value := raw.GetVoxel(c[0]*32, c[1]*32, c[2]*32)
	if !present || value != want {
		t.Fatalf("leaf %d value %d/%v want %d", index, value, present, want)
	}
}
func TestS1l13StageStorageExactMetadataAndOverflow(t *testing.T) {
	type node struct {
		left, right       *node
		sector            *volume.Sector
		bytes, generation uint64
	}
	size := uint64(unsafe.Sizeof(node{}))
	for _, tc := range []struct {
		n     int
		nodes uint64
	}{{0, 0}, {1, 2}, {2, 5}, {3, 8}, {4, 10}, {5, 13}, {8, 19}, {9, 22}, {1025, 2061}} {
		got, ok := gpu.ManagedGeometryStageStorageBytes(tc.n)
		if !ok || got != tc.nodes*size {
			t.Fatalf("storage %d = %d/%v want %d", tc.n, got, ok, tc.nodes*size)
		}
	}
	for _, n := range []int{-1, math.MaxInt} {
		if got, ok := gpu.ManagedGeometryStageStorageBytes(n); ok || got != 0 {
			t.Fatalf("invalid count %d accepted %d", n, got)
		}
	}
	for _, n := range []int{0, 1, 3, 9} {
		m, p, in := s1l13Fixture(t, n)
		s1l7Stats(t, m, []core.ManagedGeometryInput{in}, 1)
		_ = p
	}
}
func TestS1l13ReplacementFreezesArbitraryOrdinalsAndTransfersLedger(t *testing.T) {
	m, p, in := s1l13Fixture(t, 5)
	s1l13Service(t, m, p, in, 5, 5)
	original, _ := m.ManagedGeometryStage(p.object)
	baseline := m.ManagedGeometryAdmissionStats()
	var history []gpu.ManagedGeometryStageView
	var snapshots []*volume.Sector
	charges := make([]uint64, 5)
	for i := range charges {
		charges[i], _ = in.Geometry().CopySectorBytes(i)
	}
	for _, index := range []int{4, 0, 2, 4, 1} {
		history = append(history, original)
		snapshot, _ := original.CopySector(4)
		snapshots = append(snapshots, snapshot)
		c, _ := original.Coord(index)
		oldBytes := charges[index]
		p.owner.SetVoxel(c[0]*32, 0, 0, uint8(20+index))
		p.generation++
		before := m.ManagedGeometryAdmissionStats()
		if m.ReserveManagedGeometrySector(p.object, in, c) != gpu.ManagedGeometrySectorReservationReserved {
			t.Fatal("reserve")
		}
		held, _ := m.ManagedGeometrySectorReservation(p.object)
		incoming := held.Sector().CopyBytes()
		charges[index] = incoming
		// Reservations already include the simultaneous replacement output; apply
		// must work even when new admission is paused by enabled zero caps.
		m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
		if got := m.ApplyManagedGeometrySectorReservation(p.object, in); got != gpu.ManagedGeometrySectorApplyApplied {
			t.Fatalf("apply %v", got)
		}
		m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
		after := m.ManagedGeometryAdmissionStats()
		if after.InputBytes != before.InputBytes || after.OwnedMetadataBytes != before.OwnedMetadataBytes || after.ReservedCopiedStageBytes != before.ReservedCopiedStageBytes-oldBytes+incoming {
			t.Fatalf("transfer before %+v after %+v old/new %d/%d", before, after, oldBytes, incoming)
		}
		s1l12Absent(t, m, p.object)
		s1l13StageValue(t, m, p, index, uint8(20+index), p.generation)
		original, _ = m.ManagedGeometryStage(p.object)
	}
	for i, v := range history {
		got, _ := v.CopySector(4)
		if !reflect.DeepEqual(got, snapshots[i]) {
			t.Fatalf("history %d changed", i)
		}
	}
	if baseline.InputBytes != m.ManagedGeometryAdmissionStats().InputBytes {
		t.Fatal("accepted input charge changed")
	}
	p.owner.SetVoxel(64, 0, 0, 0)
	p.generation++
	before := m.ManagedGeometryAdmissionStats()
	v, _ := m.ManagedGeometryStage(p.object)
	oldOutput := v.CopiedBytes()
	if m.ReserveManagedGeometrySector(p.object, in, [3]int{2, 0, 0}) != gpu.ManagedGeometrySectorReservationReserved || m.ApplyManagedGeometrySectorReservation(p.object, in) != gpu.ManagedGeometrySectorApplyApplied {
		t.Fatal("tombstone apply")
	}
	s1l13StageValue(t, m, p, 2, 0, p.generation)
	now, _ := m.ManagedGeometryStage(p.object)
	if now.Len() != 5 || !now.Complete() || now.CopiedBytes() >= oldOutput || m.ManagedGeometryAdmissionStats().ReservedCopiedStageBytes >= before.ReservedCopiedStageBytes {
		t.Fatal("tombstone did not release old output")
	}
	for _, index := range []int{-1, 5} {
		if s, ok := now.CopySector(index); ok || s != nil {
			t.Fatal("invalid index")
		}
		if g, ok := now.SectorGeneration(index); ok || g != 0 {
			t.Fatal("invalid generation index")
		}
	}
	if len(m.Allocations)+len(m.PendingUpdates)+len(m.SectorToInfo)+len(m.BrickToSlot) != 0 {
		t.Fatal("CPU service created GPU work")
	}
	m.CancelManagedGeometryInputs(p.object)
	if m.ManagedGeometryAdmissionStats() != (gpu.ManagedGeometryAdmissionStats{}) {
		t.Fatal("cancel retained charges")
	}
	if g, ok := now.SectorGeneration(2); !ok || g != p.generation {
		t.Fatal("cancel changed frozen view")
	}
}
func TestS1l13ApplyRefusalPreservesReservationStageAndJournal(t *testing.T) {
	for _, kind := range []string{"uncopied", "unqualified", "newer", "rollback", "source", "wrong-token"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l13Fixture(t, 2)
			if kind != "uncopied" {
				s1l13Service(t, m, p, in, 2, 2)
			}
			m.QueueManagedGeometryContent(p.object, in, [3]int{})
			p.generation = 5
			if m.ReserveManagedGeometrySector(p.object, in, [3]int{}) != gpu.ManagedGeometrySectorReservationReserved {
				t.Fatal("reserve")
			}
			held, _ := m.ManagedGeometrySectorReservation(p.object)
			stage, _ := m.ManagedGeometryStage(p.object)
			work, _ := m.ManagedGeometryContentStatus(p.object)
			stats := m.ManagedGeometryAdmissionStats()
			expected := in
			want := gpu.ManagedGeometrySectorApplyStale
			switch kind {
			case "uncopied":
				want = gpu.ManagedGeometrySectorApplyUncopied
			case "unqualified":
				p.scalarAvailable = false
				want = gpu.ManagedGeometrySectorApplyUnavailable
			case "newer":
				p.generation = 6
			case "rollback":
				p.generation = 3
				want = gpu.ManagedGeometrySectorApplyUnavailable
			case "source":
				p.object.XBrickMap = p.object.XBrickMap.Copy()
				want = gpu.ManagedGeometrySectorApplyUnavailable
			case "wrong-token":
				expected = core.ManagedGeometryInput{}
				want = gpu.ManagedGeometrySectorApplyMismatch
			}
			if got := m.ApplyManagedGeometrySectorReservation(p.object, expected); got != want {
				t.Fatalf("apply %v want %v", got, want)
			}
			after, _ := m.ManagedGeometryStage(p.object)
			afterWork, _ := m.ManagedGeometryContentStatus(p.object)
			afterHeld, _ := m.ManagedGeometrySectorReservation(p.object)
			if stage != after || work != afterWork || held != afterHeld || stats != m.ManagedGeometryAdmissionStats() {
				t.Fatal("refusal mutated ownership/progress")
			}
		})
	}
}
func TestS1l13ApplyRechecksExactOwnerAndPendingAfterScalar(t *testing.T) {
	for _, kind := range []string{"cancel", "readmit", "disable", "release", "promote", "panic"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l13Fixture(t, 1)
			s1l13Service(t, m, p, in, 1, 1)
			p.generation = 5
			m.ReserveManagedGeometrySector(p.object, in, [3]int{})
			frozen, _ := m.ManagedGeometryStage(p.object)
			if kind == "promote" {
				s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
			}
			sentinel := &struct{ n int }{4}
			var callbackStats gpu.ManagedGeometryAdmissionStats
			p.scalarHook = func() {
				switch kind {
				case "cancel":
					m.CancelManagedGeometryInputs(p.object)
				case "readmit":
					m.CancelManagedGeometryInputs(p.object)
					p.generation = 4
					s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
				case "disable":
					m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
				case "release":
					m.ReleaseManagedGeometrySectorReservation(p.object, in)
				case "promote":
					m.AdvanceManagedGeometryInput(p.object, in)
				case "panic":
					panic(sentinel)
				}
				callbackStats = m.ManagedGeometryAdmissionStats()
			}
			if kind == "panic" {
				func() {
					defer func() {
						if recover() != sentinel {
							t.Fatal("panic swallowed")
						}
					}()
					m.ApplyManagedGeometrySectorReservation(p.object, in)
				}()
				return
			}
			want := gpu.ManagedGeometrySectorApplyMismatch
			if kind == "disable" {
				want = gpu.ManagedGeometrySectorApplyDisabled
			}
			if got := m.ApplyManagedGeometrySectorReservation(p.object, in); got != want {
				t.Fatalf("apply after %s %v want %v", kind, got, want)
			}
			if m.ManagedGeometryAdmissionStats() != callbackStats {
				t.Fatal("detached apply changed callback ledger")
			}
			if g, ok := frozen.SectorGeneration(0); !ok || g != 4 {
				t.Fatal("callback changed frozen history")
			}
		})
	}
}
func TestS1l13EnumerationAndEarlyJournalShareOneAllowance(t *testing.T) {
	m, p, in := s1l13Fixture(t, 3)
	s1l13Edit(t, m, p, in, [3]int{}, 8)
	s1l13Service(t, m, p, in, 2, 2)
	stage, _ := m.ManagedGeometryStage(p.object)
	work, _ := m.ManagedGeometryContentStatus(p.object)
	if stage.Len() != 2 || work.PendingCoordinates != 1 || p.reads != 0 {
		t.Fatal("enumeration consumed early journal")
	}
	s1l13StageValue(t, m, p, 0, 1, 4)
	s1l13Service(t, m, p, in, 2, 2)
	s1l13StageValue(t, m, p, 0, 8, 5)
	s1l13Status(t, m, p, true)
	if p.reads != 1 || p.calls != 1 {
		t.Fatal("continuous sparse history caused full capture/sweep")
	}
}
func TestS1l13ApplyDoesNotAcknowledgeButFreshLeafCanDrainWithoutRecopy(t *testing.T) {
	m, p, in := s1l13Fixture(t, 2)
	s1l13Service(t, m, p, in, 2, 2)
	s1l13Edit(t, m, p, in, [3]int{}, 9)
	m.ReserveManagedGeometrySector(p.object, in, [3]int{})
	if m.ApplyManagedGeometrySectorReservation(p.object, in) != gpu.ManagedGeometrySectorApplyApplied {
		t.Fatal("apply")
	}
	work, _ := m.ManagedGeometryContentStatus(p.object)
	if work.PendingCoordinates != 1 {
		t.Fatal("apply acknowledged journal")
	}
	reads := p.reads
	stats := m.ManagedGeometryAdmissionStats()
	s1l13Service(t, m, p, in, 1, 1)
	s1l13Status(t, m, p, true)
	if p.reads != reads || m.ManagedGeometryAdmissionStats() != stats {
		t.Fatal("fresh leaf was recaptured/charged")
	}
}
func TestS1l13PressureStopsAndRetainsSelectedCoordinateForRetry(t *testing.T) {
	m, p, in := s1l13Fixture(t, 3)
	s1l13Service(t, m, p, in, 3, 3)
	s1l13Edit(t, m, p, in, [3]int{}, 7)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
	s1l13Service(t, m, p, in, 8, 1)
	work, _ := m.ManagedGeometryContentStatus(p.object)
	if work.PendingCoordinates != 1 {
		t.Fatal("pressure acknowledged")
	}
	s1l13StageValue(t, m, p, 0, 1, 4)
	m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	s1l13Service(t, m, p, in, 8, 1)
	s1l13StageValue(t, m, p, 0, 7, 5)
	s1l13Status(t, m, p, true)
}
func TestS1l13PendingStaleRefreshUsesGlobalOldPlusIncomingPeak(t *testing.T) {
	m, p, in := s1l13Fixture(t, 2)
	s1l13Service(t, m, p, in, 2, 2)
	p.generation = 5
	m.ReserveManagedGeometrySector(p.object, in, [3]int{})
	held, _ := m.ManagedGeometrySectorReservation(p.object)
	p.owner.SetVoxel(0, 0, 0, 9)
	p.generation = 6
	baseline := m.ManagedGeometryAdmissionStats()
	budget := gpu.DefaultManagedGeometryAdmissionBudget()
	budget.MaxInputBytes = baseline.InputBytes + held.Sector().RetainedBytes() - 1
	m.SetManagedGeometryAdmissionBudget(budget)
	s1l13Service(t, m, p, in, 4, 1)
	if after, _ := m.ManagedGeometrySectorReservation(p.object); after != held || m.ManagedGeometryAdmissionStats().InputBytes != baseline.InputBytes {
		t.Fatal("failed refresh lost old pending")
	}
	budget.MaxInputBytes++
	m.SetManagedGeometryAdmissionBudget(budget)
	s1l13Service(t, m, p, in, 1, 1)
	s1l13StageValue(t, m, p, 0, 9, 6)
	s1l12Absent(t, m, p.object)
}
func TestS1l13CoverageGapsDuplicatesAndLegacyAckNeedOwnStableSweep(t *testing.T) {
	for _, kind := range []string{"unreported", "gap", "duplicate-invalid", "legacy-journal", "legacy-partial-sweep"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l13Fixture(t, 3)
			s1l13Service(t, m, p, in, 3, 3)
			p.owner.SetVoxel(0, 0, 0, 9)
			p.generation = 7
			switch kind {
			case "gap", "duplicate-invalid":
				m.QueueManagedGeometryContent(p.object, in, [3]int{})
				m.RecordManagedGeometryContentPublication(p.object, in, 6, 7)
				if kind == "duplicate-invalid" {
					m.RecordManagedGeometryContentPublication(p.object, in, 4, 7)
				}
			case "legacy-journal":
				m.QueueManagedGeometryContent(p.object, in, [3]int{})
				m.RecordManagedGeometryContentPublication(p.object, in, 4, 7)
				m.ServiceManagedGeometryContent(p.object, in, 1, func([3]int) bool { return true })
			case "legacy-partial-sweep":
				m.RequestManagedGeometryContentSweep(p.object, in)
				m.ServiceManagedGeometryContent(p.object, in, 1, func([3]int) bool { return true })
			}
			s1l13Status(t, m, p, false)
			s1l13Service(t, m, p, in, 1, 1)
			s, ok := m.ManagedGeometryReconciliationStatus(p.object)
			if !ok || s.Coherent || !s.RepairRequired {
				t.Fatalf("partial repair certified %+v", s)
			}
			for calls := 0; calls < 8; calls++ {
				s, _ = m.ManagedGeometryReconciliationStatus(p.object)
				if s.Coherent {
					break
				}
				m.ServiceManagedGeometryReconciliation(p.object, in, 1)
			}
			s1l13Status(t, m, p, true)
			s1l13StageValue(t, m, p, 0, 9, 7)
			s, _ = m.ManagedGeometryReconciliationStatus(p.object)
			if s.RepairRequired {
				t.Fatal("stable own sweep did not repair")
			}
		})
	}
}
func TestS1l13SweepGenerationChangesPreserveCursorAndSingleFollowup(t *testing.T) {
	m, p, in := s1l13Fixture(t, 4)
	s1l13Service(t, m, p, in, 4, 4)
	p.owner.SetVoxel(0, 0, 0, 8)
	p.generation = 5
	s1l13Service(t, m, p, in, 1, 1)
	before, _ := m.ManagedGeometryContentStatus(p.object)
	if !before.SweepPending || before.SweepCursor != 1 {
		t.Fatalf("sweep %+v", before)
	}
	for _, g := range []uint64{6, 7, 8} {
		p.owner.SetVoxel(0, 0, 0, uint8(g))
		p.generation = g
		m.QueueManagedGeometryContent(p.object, in, [3]int{})
		m.RequestManagedGeometryContentSweep(p.object, in)
	}
	after, _ := m.ManagedGeometryContentStatus(p.object)
	if after.SweepCursor != 1 || !after.SweepAgain {
		t.Fatalf("lost cursor %+v", after)
	}
	s1l13Service(t, m, p, in, 3, 3)
	s1l13Status(t, m, p, false)
	s1l13Service(t, m, p, in, 4, 4)
	s1l13Status(t, m, p, true)
	s1l13StageValue(t, m, p, 0, 8, 8)
}
func TestS1l13QualifiedEmptyRepairAndNonpositiveCalls(t *testing.T) {
	m, p, in := s1l13Fixture(t, 0)
	full, reads, scalar := p.calls, p.reads, p.scalarCalls
	for _, limit := range []int{-1, 0} {
		s1l13Service(t, m, p, in, limit, 0)
	}
	if p.calls != full || p.reads != reads || p.scalarCalls != scalar {
		t.Fatal("nonpositive allowance invoked reader")
	}
	s1l13Status(t, m, p, true)
	p.generation = 8
	s1l13Service(t, m, p, in, 1, 0)
	s1l13Status(t, m, p, true)
	p.generation = 7
	s, _ := m.ManagedGeometryReconciliationStatus(p.object)
	if s.Qualified {
		t.Fatal("rollback below repaired token qualified")
	}
}
func TestS1l13StatusIsScalarOnlyAndRechecksDetachedOwner(t *testing.T) {
	m, p, in := s1l13Fixture(t, 1)
	s1l13Service(t, m, p, in, 1, 1)
	calls, reads := p.calls, p.reads
	s1l13Status(t, m, p, true)
	if p.calls != calls || p.reads != reads {
		t.Fatal("status captured payload")
	}
	p.scalarHook = func() {
		m.CancelManagedGeometryInputs(p.object)
		s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
	}
	s, ok := m.ManagedGeometryReconciliationStatus(p.object)
	if ok || s != (gpu.ManagedGeometryReconciliationStatus{}) {
		t.Fatalf("detached status %+v/%v", s, ok)
	}
}

func TestS1l13CopyPreservesSealedAuxShapeAndIndependentBacking(t *testing.T) {
	for _, kind := range []string{"nil", "empty", "spare-capacity"} {
		t.Run(kind, func(t *testing.T) {
			raw := volume.NewXBrickMap()
			raw.SetVoxel(0, 0, 0, 3)
			switch kind {
			case "empty":
				raw.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = []byte{}
			case "spare-capacity":
				raw.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = make([]byte, 3, 80)
				copy(raw.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux, []byte{4, 5, 6})
			}
			p := s1l13NewProducer(1)
			p.owner = volume.NewManagedXBrickMap(raw)
			p.generation = 4
			m := s1l7Manager()
			s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
			in, _ := m.ManagedGeometryInputs(p.object)
			expected, _ := in.Geometry().CopySector(0)
			s1l13Service(t, m, p, in, 1, 1)
			view, _ := m.ManagedGeometryStage(p.object)
			a, _ := view.CopySector(0)
			b, _ := view.CopySector(0)
			if !reflect.DeepEqual(a, expected) {
				t.Fatal("scalars/brick values differ")
			}
			want := expected.PackedBricks[0].PrecomputedAux
			for _, s := range []*volume.Sector{a, b} {
				aux := s.PackedBricks[0].PrecomputedAux
				if len(aux) != len(want) || cap(aux) != cap(want) || (aux == nil) != (want == nil) {
					t.Fatalf("sealed shape %d/%d nil %v want %d/%d nil %v", len(aux), cap(aux), aux == nil, len(want), cap(want), want == nil)
				}
			}
			a.Coords[0] = 99
			a.BrickMask64 = 0
			a.PackedBricks[0].AtlasOffset = 99
			if len(a.PackedBricks[0].PrecomputedAux) > 0 {
				a.PackedBricks[0].PrecomputedAux[0] = 99
			}
			again, _ := view.CopySector(0)
			if !reflect.DeepEqual(again, expected) || !reflect.DeepEqual(b, expected) {
				t.Fatal("inspection exposed manager backing")
			}
		})
	}
}
func TestS1l13StandaloneEnumerationAfterGrowingEarlyReplacement(t *testing.T) {
	m, p, in := s1l13Fixture(t, 3)
	if m.ServiceManagedGeometry(p.object, 1) != 1 {
		t.Fatal("first standalone copy")
	}
	p.owner.SetVoxel(8, 0, 0, 7)
	p.generation = 5
	if m.ReserveManagedGeometrySector(p.object, in, [3]int{}) != gpu.ManagedGeometrySectorReservationReserved || m.ApplyManagedGeometrySectorReservation(p.object, in) != gpu.ManagedGeometrySectorApplyApplied {
		t.Fatal("early growing replacement")
	}
	stage, _ := m.ManagedGeometryStage(p.object)
	grown := stage.CopiedBytes()
	if m.ServiceManagedGeometry(p.object, 8) != 2 {
		t.Fatal("original aggregate copy cap blocked already reserved remaining leaves")
	}
	after, _ := m.ManagedGeometryStage(p.object)
	if !after.Complete() || after.CopiedBytes() <= grown {
		t.Fatal("remaining enumeration did not advance")
	}
	s1l13StageValue(t, m, p, 0, 1, 5)
	s1l13StageValue(t, m, p, 1, 2, 4)
}
func TestS1l13ManagerGenerationFloorCannotRepairRollback(t *testing.T) {
	m, p, in := s1l13Fixture(t, 2)
	s1l13Service(t, m, p, in, 2, 2)
	p.owner.SetVoxel(0, 0, 0, 9)
	p.generation = 9
	m.QueueManagedGeometryContent(p.object, in, [3]int{})
	m.RecordManagedGeometryContentPublication(p.object, in, 4, 9)
	s1l13Service(t, m, p, in, 1, 1)
	s1l13Status(t, m, p, true)
	p.generation = 8
	// Core qualification has no manager history; manager must preserve its own floor.
	if g, ok := p.object.CurrentManagedGeometryGeneration(in); !ok || g != 8 {
		t.Fatal("core unexpectedly tracks manager floor")
	}
	s, _ := m.ManagedGeometryReconciliationStatus(p.object)
	if s.Qualified || s.Coherent {
		t.Fatal("manager qualified rollback below covered version")
	}
	m.RequestManagedGeometryContentSweep(p.object, in)
	m.ServiceManagedGeometryReconciliation(p.object, in, 10)
	s, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if s.Qualified || s.Coherent {
		t.Fatal("rollback healed by fallback sweep")
	}
	s1l13StageValue(t, m, p, 0, 9, 9)
}
func TestS1l13CombinedServiceCallbackCannotAckDetachedJournal(t *testing.T) {
	for _, kind := range []string{"cancel-readmit", "promote", "disable", "unavailable", "panic"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l13Fixture(t, 2)
			s1l13Service(t, m, p, in, 2, 2)
			s1l13Edit(t, m, p, in, [3]int{}, 9)
			if kind == "promote" {
				s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
			}
			sentinel := &struct{ n int }{9}
			var callbackStats gpu.ManagedGeometryAdmissionStats
			p.hook = func() {
				switch kind {
				case "cancel-readmit":
					m.CancelManagedGeometryInputs(p.object)
					p.generation = 4
					s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
				case "promote":
					m.AdvanceManagedGeometryInput(p.object, in)
				case "disable":
					m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
				case "unavailable":
					p.available = false
				case "panic":
					panic(sentinel)
				}
				callbackStats = m.ManagedGeometryAdmissionStats()
			}
			if kind == "panic" {
				before, _ := m.ManagedGeometryContentStatus(p.object)
				func() {
					defer func() {
						if recover() != sentinel {
							t.Fatal("panic changed")
						}
					}()
					m.ServiceManagedGeometryReconciliation(p.object, in, 10)
				}()
				after, _ := m.ManagedGeometryContentStatus(p.object)
				if before != after {
					t.Fatal("panic acknowledged work")
				}
				return
			}
			s1l13Service(t, m, p, in, 10, 1)
			if m.ManagedGeometryAdmissionStats() != callbackStats {
				t.Fatal("service changed detached callback ledger")
			}
			work, ok := m.ManagedGeometryContentStatus(p.object)
			switch kind {
			case "cancel-readmit", "promote":
				v, _ := m.ManagedGeometryStage(p.object)
				if !ok || work.Pending || v.Len() != 0 {
					t.Fatal("service mutated replacement owner")
				}
			case "disable":
				if ok {
					t.Fatal("disabled owner retained")
				}
			case "unavailable":
				if !ok || work.PendingCoordinates != 1 {
					t.Fatal("unavailable acknowledged selected work")
				}
			}
		})
	}
}
func TestS1l13UpdatedStageLifecycleReleasesTransferredOutput(t *testing.T) {
	for _, kind := range []string{"cancel", "disable", "promote"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l13Fixture(t, 1)
			s1l13Service(t, m, p, in, 1, 1)
			p.owner.SetVoxel(8, 0, 0, 8)
			p.generation = 5
			m.ReserveManagedGeometrySector(p.object, in, [3]int{})
			if m.ApplyManagedGeometrySectorReservation(p.object, in) != gpu.ManagedGeometrySectorApplyApplied {
				t.Fatal("apply")
			}
			frozen, _ := m.ManagedGeometryStage(p.object)
			snapshot, _ := frozen.CopySector(0)
			p.generation = 6
			m.ReserveManagedGeometrySector(p.object, in, [3]int{})
			switch kind {
			case "cancel":
				m.CancelManagedGeometryInputs(p.object)
			case "disable":
				m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
			case "promote":
				s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
				_, next := m.ManagedGeometryInputs(p.object)
				if !m.AdvanceManagedGeometryInput(p.object, in) {
					t.Fatal("promote")
				}
				s1l7Stats(t, m, []core.ManagedGeometryInput{next}, 1)
				m.CancelManagedGeometryInputs(p.object)
			}
			if m.ManagedGeometryAdmissionStats() != (gpu.ManagedGeometryAdmissionStats{}) {
				t.Fatal("transferred output leaked on lifecycle release")
			}
			again, _ := frozen.CopySector(0)
			if !reflect.DeepEqual(again, snapshot) {
				t.Fatal("release changed caller history")
			}
		})
	}
}
func TestS1l13LegacyWithoutScalarReaderCannotCertify(t *testing.T) {
	m, p, in := s1l12Fixture(t, 1)
	if m.ServiceManagedGeometryReconciliation(p.object, in, 1) != 1 {
		t.Fatal("legacy initial enumeration")
	}
	s, ok := m.ManagedGeometryReconciliationStatus(p.object)
	if !ok || s.Copied != 1 || s.Total != 1 || s.Qualified || s.Coherent {
		t.Fatalf("legacy status %+v/%v", s, ok)
	}
	calls, reads := p.calls, p.reads
	m.ServiceManagedGeometryReconciliation(p.object, in, 10)
	if p.calls != calls || p.reads != reads {
		t.Fatal("missing scalar fell back to capture")
	}
}
func TestS1l13OverflowAndLateEditsPreserveSweepProgress(t *testing.T) {
	m, p, in := s1l13Fixture(t, 1025)
	s1l13Service(t, m, p, in, 1025, 1025)
	p.generation = 5
	for i := 0; i < 1025; i++ {
		m.QueueManagedGeometryContent(p.object, in, [3]int{i, 0, 0})
	}
	m.RecordManagedGeometryContentPublication(p.object, in, 4, 5)
	s1l13Service(t, m, p, in, 7, 7)
	before, _ := m.ManagedGeometryContentStatus(p.object)
	if before.SweepCursor != 7 {
		t.Fatalf("cursor %+v", before)
	}
	p.owner.SetVoxel(0, 0, 0, 9)
	p.generation = 6
	for i := 0; i < 1025; i++ {
		m.QueueManagedGeometryContent(p.object, in, [3]int{i, 0, 0})
	}
	m.RecordManagedGeometryContentPublication(p.object, in, 5, 6)
	after, _ := m.ManagedGeometryContentStatus(p.object)
	if after.SweepCursor != 7 || !after.SweepAgain {
		t.Fatalf("overflow reset progress %+v", after)
	}
	m.QueueManagedGeometryContent(p.object, in, [3]int{})
	m.ServiceManagedGeometryReconciliation(p.object, in, 1018)
	s1l13Status(t, m, p, false)
	m.ServiceManagedGeometryReconciliation(p.object, in, 1026)
	s1l13Status(t, m, p, true)
	s1l13StageValue(t, m, p, 0, 9, 6)
}

func TestS1l13StatusObservationSetsRollbackFloorWithoutChangingWork(t *testing.T) {
	m, p, in := s1l13Fixture(t, 2)
	s1l13Service(t, m, p, in, 2, 2)
	stage, _ := m.ManagedGeometryStage(p.object)
	work, _ := m.ManagedGeometryContentStatus(p.object)
	stats := m.ManagedGeometryAdmissionStats()
	p.generation = 9
	s1l13Status(t, m, p, false)
	afterStage, _ := m.ManagedGeometryStage(p.object)
	afterWork, _ := m.ManagedGeometryContentStatus(p.object)
	if stage != afterStage || work != afterWork || stats != m.ManagedGeometryAdmissionStats() {
		t.Fatal("scalar observation changed work/copies/ledger")
	}
	p.generation = 8
	s, _ := m.ManagedGeometryReconciliationStatus(p.object)
	if s.Qualified || s.Coherent {
		t.Fatal("rollback below observed scalar qualified")
	}
	m.ServiceManagedGeometryReconciliation(p.object, in, 20)
	s, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if s.Qualified || s.Coherent {
		t.Fatal("repair certified rollback below observed generation")
	}
}
func TestS1l13ApplyStaleRefusalDoesNotAllocateOrRegressLeaf(t *testing.T) {
	m, p, in := s1l13Fixture(t, 1)
	s1l13Service(t, m, p, in, 1, 1)
	p.owner.SetVoxel(0, 0, 0, 8)
	p.generation = 8
	m.ReserveManagedGeometrySector(p.object, in, [3]int{})
	if m.ApplyManagedGeometrySectorReservation(p.object, in) != gpu.ManagedGeometrySectorApplyApplied {
		t.Fatal("apply8")
	}
	p.generation = 7
	if m.ReserveManagedGeometrySector(p.object, in, [3]int{}) != gpu.ManagedGeometrySectorReservationReserved {
		t.Fatal("reserve7")
	}
	held, _ := m.ManagedGeometrySectorReservation(p.object)
	stage, _ := m.ManagedGeometryStage(p.object)
	stats := m.ManagedGeometryAdmissionStats()
	valid := true
	allocations := testing.AllocsPerRun(50, func() {
		valid = valid && m.ApplyManagedGeometrySectorReservation(p.object, in) == gpu.ManagedGeometrySectorApplyStale
	})
	if !valid || allocations != 0 {
		t.Fatalf("stale apply valid %v allocated %g", valid, allocations)
	}
	after, _ := m.ManagedGeometryStage(p.object)
	afterHeld, _ := m.ManagedGeometrySectorReservation(p.object)
	if stage != after || held != afterHeld || stats != m.ManagedGeometryAdmissionStats() {
		t.Fatal("older candidate regressed leaf or ownership")
	}
}

func TestS1l13FinalSweepQualificationPanicPreservesRetryCursor(t *testing.T) {
	m, p, in := s1l13Fixture(t, 2)
	s1l13Service(t, m, p, in, 2, 2)
	original, _ := m.ManagedGeometryStage(p.object)
	originalLast, _ := original.CopySector(1)
	// No exhaustive publication marker: this change requires a materializing pass.
	p.owner.SetVoxel(0, 0, 0, 8)
	p.owner.SetVoxel(32, 0, 0, 9)
	p.generation = 5
	s1l13Service(t, m, p, in, 1, 1)
	halfway, _ := m.ManagedGeometryStage(p.object)
	halfwayFirst, _ := halfway.CopySector(0)
	halfwayLast, _ := halfway.CopySector(1)
	work, _ := m.ManagedGeometryContentStatus(p.object)
	if !work.SweepPending || work.SweepCursor != 1 {
		t.Fatalf("repair progress %+v", work)
	}
	sentinel := &struct{ label string }{"end qualification"}
	p.scalarHook = func() {
		// The end probe is the first scalar callback that can observe the final
		// replacement installed. Coordinate and apply probes see its old leaf.
		current, _ := m.ManagedGeometryStage(p.object)
		if generation, ok := current.SectorGeneration(1); ok && generation == p.generation {
			panic(sentinel)
		}
	}
	func() {
		defer func() {
			if got := recover(); got != sentinel {
				t.Fatalf("end probe panic %v want sentinel", got)
			}
		}()
		m.ServiceManagedGeometryReconciliation(p.object, in, 1)
	}()
	p.scalarHook = nil
	work, _ = m.ManagedGeometryContentStatus(p.object)
	if !work.SweepPending || work.SweepCursor != 1 {
		t.Fatalf("panic acknowledged final ordinal or wedged cursor %+v", work)
	}
	status, _ := m.ManagedGeometryReconciliationStatus(p.object)
	if !status.Pending || status.Coherent {
		t.Fatalf("incomplete end qualification %+v", status)
	}
	installed, _ := m.ManagedGeometryStage(p.object)
	installedLast, _ := installed.CopySector(1)
	s1l13Service(t, m, p, in, 1, 1)
	s1l13Status(t, m, p, true)
	s1l13StageValue(t, m, p, 0, 8, 5)
	s1l13StageValue(t, m, p, 1, 9, 5)
	for _, tc := range []struct {
		view     gpu.ManagedGeometryStageView
		index    int
		expected *volume.Sector
	}{
		{original, 1, originalLast}, {halfway, 0, halfwayFirst}, {halfway, 1, halfwayLast}, {installed, 1, installedLast},
	} {
		got, ok := tc.view.CopySector(tc.index)
		if !ok || !reflect.DeepEqual(got, tc.expected) {
			t.Fatal("panic/retry changed frozen stage history")
		}
	}
}

func TestS1l13StatusPendingIncludesEnumerationAndRequiredRepair(t *testing.T) {
	m, p, in := s1l13Fixture(t, 2)
	status, ok := m.ManagedGeometryReconciliationStatus(p.object)
	if !ok || !status.Pending || status.Coherent || status.Copied != 0 || status.Total != 2 {
		t.Fatalf("initial CPU work %+v/%v", status, ok)
	}
	s1l13Service(t, m, p, in, 1, 1)
	status, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if !status.Pending || status.Coherent || status.Copied != 1 {
		t.Fatalf("partial enumeration %+v", status)
	}
	s1l13Service(t, m, p, in, 1, 1)
	status, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if status.Pending || !status.Coherent {
		t.Fatalf("completed CPU work %+v", status)
	}
	p.generation = 7
	status, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if !status.Pending || status.Coherent {
		t.Fatalf("unreported version missing pending repair %+v", status)
	}
	s1l13Service(t, m, p, in, 1, 1)
	status, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if !status.Pending || !status.RepairRequired || status.Coherent {
		t.Fatalf("materializing repair %+v", status)
	}
	s1l13Service(t, m, p, in, 1, 1)
	status, _ = m.ManagedGeometryReconciliationStatus(p.object)
	if status.Pending || !status.Coherent {
		t.Fatalf("repaired CPU work %+v", status)
	}

	empty, producer, token := s1l13Fixture(t, 0)
	producer.generation = 9
	status, _ = empty.ManagedGeometryReconciliationStatus(producer.object)
	if !status.Pending || status.Coherent || status.Total != 0 {
		t.Fatalf("empty unknown history %+v", status)
	}
	s1l13Service(t, empty, producer, token, 0, 0)
	status, _ = empty.ManagedGeometryReconciliationStatus(producer.object)
	if !status.Pending || status.Coherent {
		t.Fatal("nonpositive service repaired empty topology")
	}
	s1l13Service(t, empty, producer, token, 1, 0)
	status, _ = empty.ManagedGeometryReconciliationStatus(producer.object)
	if status.Pending || !status.Coherent {
		t.Fatalf("vacuous stable repair %+v", status)
	}
}

func TestS1l13ObservedRollbackRequiresRepairAfterGenerationRestored(t *testing.T) {
	for _, kind := range []string{"below-accepted", "below-covered"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l13Fixture(t, 2)
			s1l13Service(t, m, p, in, 2, 2)
			covered, rollback := uint64(4), uint64(3)
			if kind == "below-covered" {
				covered, rollback = 9, 8
				p.owner.SetVoxel(0, 0, 0, 9)
				p.generation = covered
				if m.QueueManagedGeometryContent(p.object, in, [3]int{}) != gpu.ManagedGeometryContentQueued || !m.RecordManagedGeometryContentPublication(p.object, in, 4, covered) {
					t.Fatal("trusted publication setup")
				}
				s1l13Service(t, m, p, in, 1, 1)
			}
			s1l13Status(t, m, p, true)
			frozen, _ := m.ManagedGeometryStage(p.object)
			saved, _ := frozen.CopySector(0)
			stats := m.ManagedGeometryAdmissionStats()
			work, _ := m.ManagedGeometryContentStatus(p.object)
			// Even an unavailable core read can conceal rollback and changed authority.
			// Restoring the covered token cannot prove that copied payload is current.
			p.owner.SetVoxel(0, 0, 0, 8)
			p.generation = rollback
			if kind == "below-accepted" {
				if g, ok := p.object.CurrentManagedGeometryGeneration(in); ok || g != 0 {
					t.Fatal("core accepted generation rollback")
				}
			}
			rolledBack, ok := m.ManagedGeometryReconciliationStatus(p.object)
			if !ok || rolledBack.Qualified || rolledBack.Coherent {
				t.Fatalf("rollback status %+v/%v", rolledBack, ok)
			}
			p.generation = covered
			restored, ok := m.ManagedGeometryReconciliationStatus(p.object)
			if !ok || !restored.Qualified || restored.Coherent || !restored.RepairRequired || !restored.Pending {
				t.Fatalf("restored token bypassed repair %+v/%v", restored, ok)
			}
			unchanged, _ := m.ManagedGeometryStage(p.object)
			afterWork, _ := m.ManagedGeometryContentStatus(p.object)
			if unchanged != frozen || afterWork != work || m.ManagedGeometryAdmissionStats() != stats {
				t.Fatal("scalar rollback observation scheduled/copied/charged work")
			}
			s1l13Service(t, m, p, in, 1, 1)
			partial, _ := m.ManagedGeometryReconciliationStatus(p.object)
			if partial.Coherent || !partial.Pending || !partial.RepairRequired {
				t.Fatalf("partial pass repaired rollback %+v", partial)
			}
			s1l13Service(t, m, p, in, 1, 1)
			s1l13Status(t, m, p, true)
			repaired, _ := m.ManagedGeometryReconciliationStatus(p.object)
			if repaired.RepairRequired || repaired.Pending {
				t.Fatalf("whole stable pass did not repair %+v", repaired)
			}
			s1l13StageValue(t, m, p, 0, 8, covered)
			old, _ := frozen.CopySector(0)
			if !reflect.DeepEqual(old, saved) {
				t.Fatal("rollback repair changed frozen view")
			}
		})
	}
}
