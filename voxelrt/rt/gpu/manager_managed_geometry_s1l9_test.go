package gpu_test

import (
	"maps"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Empty sectors keep large topology fixtures small and avoid voxel arithmetic
// when exercising the entire signed coordinate domain.
func s1l9Producer(coords [][3]int) *s1l7Producer {
	raw := volume.NewXBrickMap()
	for _, c := range coords {
		raw.Sectors[c] = volume.NewSector(c[0], c[1], c[2])
	}
	p := s1l7NewProducer(0)
	p.owner = volume.NewManagedXBrickMap(raw)
	p.object.XBrickMap = raw.Copy()
	p.install()
	return p
}
func s1l9Fixture(t *testing.T, n int) (*gpu.GpuBufferManager, *s1l7Producer, core.ManagedGeometryInput) {
	t.Helper()
	coords := make([][3]int, n)
	for i := range coords {
		coords[i] = [3]int{i - n/2, i%3 - 1, i%5 - 2}
	}
	p := s1l9Producer(coords)
	m := s1l7Manager()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	if in.Geometry().Len() != n {
		t.Fatal("fixture lost empty sectors")
	}
	return m, p, in
}
func s1l9Coord(t *testing.T, in core.ManagedGeometryInput, i int) [3]int {
	t.Helper()
	c, ok := in.Geometry().Coord(i)
	if !ok {
		t.Fatalf("fixture coordinate %d absent", i)
	}
	return c
}
func s1l9Queue(t *testing.T, m *gpu.GpuBufferManager, p *s1l7Producer, in core.ManagedGeometryInput, c [3]int, want gpu.ManagedGeometryContentResult) {
	t.Helper()
	if got := m.QueueManagedGeometryContent(p.object, in, c); got != want {
		t.Fatalf("queue %v = %v, want %v", c, got, want)
	}
}
func s1l9Status(t *testing.T, m *gpu.GpuBufferManager, p *s1l7Producer, in core.ManagedGeometryInput, count int, sweep bool, cursor int, again bool) gpu.ManagedGeometryContentStatus {
	t.Helper()
	s, ok := m.ManagedGeometryContentStatus(p.object)
	if !ok {
		t.Fatal("accepted content status absent")
	}
	if !s.Input.SameSource(in) || s.Input.Generation() != in.Generation() {
		t.Fatal("content status identity changed")
	}
	if s.PendingCoordinates != count || s.SweepPending != sweep || s.SweepCursor != cursor || s.SweepAgain != again || s.Pending != (count > 0 || sweep) {
		t.Fatalf("content status = %+v, want count %d sweep %v cursor %d again %v", s, count, sweep, cursor, again)
	}
	return s
}
func s1l9Absent(t *testing.T, m *gpu.GpuBufferManager, o *core.VoxelObject) {
	t.Helper()
	s, ok := m.ManagedGeometryContentStatus(o)
	s1l7Absent(t, s.Input)
	if ok || s.PendingCoordinates != 0 || s.SweepPending || s.SweepCursor != 0 || s.SweepAgain || s.Pending {
		t.Fatalf("absent status = %+v/%v", s, ok)
	}
}

func TestS1l9NilMissingDisabledEmptyAndNoOpService(t *testing.T) {
	p := s1l7NewProducer(1)
	var nilManager *gpu.GpuBufferManager
	for _, m := range []*gpu.GpuBufferManager{nilManager, {}, s1l7Manager()} {
		for _, o := range []*core.VoxelObject{nil, p.object, core.NewVoxelObject()} {
			if got := m.QueueManagedGeometryContent(o, core.ManagedGeometryInput{}, [3]int{}); got != gpu.ManagedGeometryContentUnavailable {
				t.Fatalf("absent queue = %v", got)
			}
			if m.RequestManagedGeometryContentSweep(o, core.ManagedGeometryInput{}) {
				t.Fatal("absent sweep accepted")
			}
			if m.ServiceManagedGeometryContent(o, core.ManagedGeometryInput{}, 99, func([3]int) bool { t.Fatal("absent callback"); return true }) != 0 {
				t.Fatal("absent service")
			}
			s1l9Absent(t, m, o)
		}
	}
	if p.calls != 0 {
		t.Fatal("content API captured producer")
	}
	m, p, in := s1l9Fixture(t, 0)
	if !m.RequestManagedGeometryContentSweep(p.object, in) {
		t.Fatal("empty owned sweep refused")
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
	s1l9Queue(t, m, p, in, [3]int{}, gpu.ManagedGeometryContentIgnored)
	if m.ServiceManagedGeometryContent(p.object, in, 99, func([3]int) bool { t.Fatal("empty callback"); return true }) != 0 {
		t.Fatal("empty service")
	}
	m, p, in = s1l9Fixture(t, 2)
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 0), gpu.ManagedGeometryContentQueued)
	s1l9Status(t, m, p, in, 1, false, 0, false)
	for _, n := range []int{0, -1} {
		if m.ServiceManagedGeometryContent(p.object, in, n, func([3]int) bool { t.Fatal("nonpositive callback"); return true }) != 0 {
			t.Fatal("nonpositive service")
		}
	}
	if m.ServiceManagedGeometryContent(p.object, in, 10, nil) != 0 {
		t.Fatal("nil callback service")
	}
	s1l9Status(t, m, p, in, 1, false, 0, false)
}

func TestS1l9SignedMembershipIdentityAndCoalescing(t *testing.T) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	coords := [][3]int{{maxInt, maxInt, maxInt}, {0, 0, 1}, {minInt, minInt, minInt}, {0, -1, maxInt}, {0, 0, -1}, {minInt, maxInt, 0}, {maxInt, minInt, 0}}
	p := s1l9Producer(coords)
	m := s1l7Manager()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	other := s1l9Producer(coords)
	s1l7Admit(t, m, other, gpu.ManagedGeometryAdmissionAccepted)
	foreign, _ := m.ManagedGeometryInputs(other.object)
	s1l9Queue(t, m, p, foreign, coords[0], gpu.ManagedGeometryContentMismatch)
	s1l9Queue(t, m, p, core.ManagedGeometryInput{}, coords[0], gpu.ManagedGeometryContentMismatch)
	p.owner.SetVoxel(64, 0, 0, 9)
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, next := m.ManagedGeometryInputs(p.object)
	s1l9Queue(t, m, p, next, coords[0], gpu.ManagedGeometryContentMismatch)
	for _, c := range [][3]int{{2, 0, 0}, {0, 0, 0}, {minInt, minInt, minInt + 1}, {maxInt, maxInt, maxInt - 1}} {
		s1l9Queue(t, m, p, in, c, gpu.ManagedGeometryContentIgnored)
	}
	for _, c := range coords {
		s1l9Queue(t, m, p, in, c, gpu.ManagedGeometryContentQueued)
		s1l9Queue(t, m, p, in, c, gpu.ManagedGeometryContentCoalesced)
	}
	s1l9Status(t, m, p, in, len(coords), false, 0, false)
	s1l9Status(t, m, p, in, len(coords), false, 0, false)
	if m.RequestManagedGeometryContentSweep(p.object, next) || m.ServiceManagedGeometryContent(p.object, foreign, 99, func([3]int) bool { t.Fatal("mismatched callback"); return true }) != 0 {
		t.Fatal("mismatched work accepted")
	}
	s1l9Status(t, m, p, in, len(coords), false, 0, false)
	seen := map[[3]int]int{}
	if n := m.ServiceManagedGeometryContent(p.object, in, 99, func(c [3]int) bool { seen[c]++; return true }); n != len(coords) {
		t.Fatalf("journal attempts = %d", n)
	}
	for _, c := range coords {
		if seen[c] != 1 {
			t.Fatalf("coordinate %v visits = %d", c, seen[c])
		}
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
	// A consumed notification does not suppress a later event at the same coordinate.
	c := coords[0]
	for event := 0; event < 2; event++ {
		s1l9Queue(t, m, p, in, c, gpu.ManagedGeometryContentQueued)
		s1l9Status(t, m, p, in, 1, false, 0, false)
		if n := m.ServiceManagedGeometryContent(p.object, in, 99, func(got [3]int) bool {
			if got != c {
				t.Fatal("requeued coordinate changed")
			}
			return true
		}); n != 1 {
			t.Fatalf("event %d attempts = %d", event, n)
		}
		s1l9Status(t, m, p, in, 0, false, 0, false)
	}
}

func TestS1l9JournalCapacityOverflowAndUnstartedCoalescing(t *testing.T) {
	m, p, in := s1l9Fixture(t, 1025)
	calls := p.calls
	charges := m.ManagedGeometryAdmissionStats()
	for i := 0; i < 1024; i++ {
		s1l9Queue(t, m, p, in, s1l9Coord(t, in, i), gpu.ManagedGeometryContentQueued)
	}
	s1l9Status(t, m, p, in, 1024, false, 0, false)
	s1l9Queue(t, m, p, in, [3]int{9999, 0, 0}, gpu.ManagedGeometryContentIgnored)
	s1l9Status(t, m, p, in, 1024, false, 0, false)
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 777), gpu.ManagedGeometryContentCoalesced)
	s1l9Status(t, m, p, in, 1024, false, 0, false)
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 1024), gpu.ManagedGeometryContentSweepScheduled)
	s1l9Status(t, m, p, in, 0, true, 0, false)
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 3), gpu.ManagedGeometryContentQueued)
	if !m.RequestManagedGeometryContentSweep(p.object, in) {
		t.Fatal("owned request refused")
	}
	s1l9Status(t, m, p, in, 0, true, 0, false)
	for i := 0; i < 1024; i++ {
		s1l9Queue(t, m, p, in, s1l9Coord(t, in, i), gpu.ManagedGeometryContentQueued)
	}
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 1024), gpu.ManagedGeometryContentSweepScheduled)
	s1l9Status(t, m, p, in, 0, true, 0, false)
	index := 0
	if n := m.ServiceManagedGeometryContent(p.object, in, int(^uint(0)>>1), func(c [3]int) bool {
		if c != s1l9Coord(t, in, index) {
			t.Fatal("sweep order")
		}
		index++
		return true
	}); n != 1025 {
		t.Fatalf("sweep attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
	if p.calls != calls || m.ManagedGeometryAdmissionStats() != charges {
		t.Fatal("scheduling captured or recharged")
	}
}

func TestS1l9ProgressedOverflowPreservesCursorAndOneFollowup(t *testing.T) {
	m, p, in := s1l9Fixture(t, 1025)
	if !m.RequestManagedGeometryContentSweep(p.object, in) {
		t.Fatal("initial sweep")
	}
	if m.ServiceManagedGeometryContent(p.object, in, 7, func([3]int) bool { return true }) != 7 {
		t.Fatal("partial sweep")
	}
	for round := 0; round < 2; round++ {
		for i := 0; i < 1024; i++ {
			s1l9Queue(t, m, p, in, s1l9Coord(t, in, i), gpu.ManagedGeometryContentQueued)
		}
		s1l9Queue(t, m, p, in, s1l9Coord(t, in, 1024), gpu.ManagedGeometryContentSweepScheduled)
		s1l9Status(t, m, p, in, 0, true, 7, true)
	}
	for i := 0; i < 3; i++ {
		if !m.RequestManagedGeometryContentSweep(p.object, in) {
			t.Fatal("repeat request")
		}
		s1l9Status(t, m, p, in, 0, true, 7, true)
	}
	// This event follows the last request and must survive both sweep completions.
	late := s1l9Coord(t, in, 0)
	s1l9Queue(t, m, p, in, late, gpu.ManagedGeometryContentQueued)
	index := 7
	if n := m.ServiceManagedGeometryContent(p.object, in, 1018, func(c [3]int) bool {
		if c != s1l9Coord(t, in, index) {
			t.Fatal("active cursor restarted")
		}
		index++
		return true
	}); n != 1018 {
		t.Fatalf("remaining attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, true, 0, false)
	index = 0
	if n := m.ServiceManagedGeometryContent(p.object, in, 1025, func(c [3]int) bool {
		if c != s1l9Coord(t, in, index) {
			t.Fatal("followup order")
		}
		index++
		return true
	}); n != 1025 {
		t.Fatalf("followup attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, false, 0, false)
	if n := m.ServiceManagedGeometryContent(p.object, in, 99, func(c [3]int) bool {
		if c != late {
			t.Fatal("late journal event lost")
		}
		return true
	}); n != 1 {
		t.Fatalf("late attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
}

func TestS1l9ServiceBoundsFailurePanicRetryAndSweepPriority(t *testing.T) {
	for _, sweep := range []bool{false, true} {
		t.Run(map[bool]string{false: "journal", true: "sweep"}[sweep], func(t *testing.T) {
			m, p, in := s1l9Fixture(t, 5)
			if sweep {
				m.RequestManagedGeometryContentSweep(p.object, in)
			} else {
				for i := 0; i < 5; i++ {
					s1l9Queue(t, m, p, in, s1l9Coord(t, in, i), gpu.ManagedGeometryContentQueued)
				}
			}
			calls := 0
			var failed [3]int
			if n := m.ServiceManagedGeometryContent(p.object, in, 4, func(c [3]int) bool {
				calls++
				if calls == 2 {
					failed = c
					return false
				}
				return true
			}); n != 2 || calls != 2 {
				t.Fatal("false must count and stop attempts")
			}
			if sweep {
				s1l9Status(t, m, p, in, 0, true, 1, false)
			} else {
				s1l9Status(t, m, p, in, 4, false, 0, false)
			}
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("callback panic swallowed")
					}
				}()
				m.ServiceManagedGeometryContent(p.object, in, 99, func(c [3]int) bool {
					if c != failed {
						t.Fatal("failed coordinate not retried")
					}
					panic("visit")
				})
			}()
			if sweep {
				s1l9Status(t, m, p, in, 0, true, 1, false)
			} else {
				s1l9Status(t, m, p, in, 4, false, 0, false)
			}
			retry := true
			if n := m.ServiceManagedGeometryContent(p.object, in, int(^uint(0)>>1), func(c [3]int) bool {
				if retry && c != failed {
					t.Fatal("panic consumed visit")
				}
				retry = false
				return true
			}); n != 4 {
				t.Fatalf("max-int remaining attempts = %d", n)
			}
			s1l9Status(t, m, p, in, 0, false, 0, false)
		})
	}
	m, p, in := s1l9Fixture(t, 3)
	m.RequestManagedGeometryContentSweep(p.object, in)
	late := s1l9Coord(t, in, 0)
	s1l9Queue(t, m, p, in, late, gpu.ManagedGeometryContentQueued)
	var seen [][3]int
	if n := m.ServiceManagedGeometryContent(p.object, in, 4, func(c [3]int) bool { seen = append(seen, c); return true }); n != 4 {
		t.Fatal("combined allowance")
	}
	want := [][3]int{s1l9Coord(t, in, 0), s1l9Coord(t, in, 1), s1l9Coord(t, in, 2), late}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("sweep priority = %v", seen)
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
}

func TestS1l9LifecyclePreservesAcceptedWorkAndFrozenStage(t *testing.T) {
	m := s1l7Manager()
	p := s1l8Producer(4)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	if m.ServiceManagedGeometry(p.object, 2) != 2 {
		t.Fatal("structural prefix")
	}
	old := s1l8Stage(t, m, p)
	m.RequestManagedGeometryContentSweep(p.object, in)
	m.ServiceManagedGeometryContent(p.object, in, 1, func([3]int) bool { return true })
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 0), gpu.ManagedGeometryContentQueued)
	s1l9Status(t, m, p, in, 1, true, 1, false)
	for _, generation := range []uint64{1, 2} {
		p.owner.SetVoxel(0, 0, 0, uint8(generation+20))
		p.generation = generation
		s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	}
	_, next := m.ManagedGeometryInputs(p.object)
	p.available = false
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnavailable)
	p.available = true
	p.install()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionSourceChanged)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionSourceChanged)
	s1l9Status(t, m, p, in, 1, true, 1, false)
	s1l8Check(t, s1l8Stage(t, m, p), in, 2)
	s1l8Check(t, old, in, 2)
	calls := p.calls
	charges := m.ManagedGeometryAdmissionStats()
	if n := m.ServiceManagedGeometryContent(p.object, in, 99, func([3]int) bool { return true }); n != 4 {
		t.Fatalf("reserved work under zero caps = %d", n)
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
	s1l8Check(t, s1l8Stage(t, m, p), in, 2)
	s1l8Check(t, old, in, 2)
	if p.calls != calls || m.ManagedGeometryAdmissionStats() != charges {
		t.Fatal("content service captured or charged")
	}
	m.RequestManagedGeometryContentSweep(p.object, in)
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 0), gpu.ManagedGeometryContentQueued)
	if !m.AdvanceManagedGeometryInput(p.object, in) {
		t.Fatal("promotion")
	}
	s1l9Status(t, m, p, next, 0, false, 0, false)
	s1l8Check(t, s1l8Stage(t, m, p), next, 0)
	s1l8Check(t, old, in, 2)
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 0), gpu.ManagedGeometryContentMismatch)
	if m.RequestManagedGeometryContentSweep(p.object, in) || m.ServiceManagedGeometryContent(p.object, in, 99, func([3]int) bool { t.Fatal("stale callback"); return true }) != 0 {
		t.Fatal("stale accepted after promotion")
	}
	m.RequestManagedGeometryContentSweep(p.object, next)
	if !m.CancelManagedGeometryInputs(p.object) {
		t.Fatal("cancel")
	}
	s1l9Absent(t, m, p.object)
	s1l7Stats(t, m, nil, 0)
	m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	fresh, _ := m.ManagedGeometryInputs(p.object)
	s1l9Status(t, m, p, fresh, 0, false, 0, false)
	m.RequestManagedGeometryContentSweep(p.object, fresh)
	s1l9Queue(t, m, p, fresh, s1l9Coord(t, fresh, 0), gpu.ManagedGeometryContentQueued)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
	s1l9Absent(t, m, p.object)
	s1l7Stats(t, m, nil, 0)
	s1l8Check(t, old, in, 2)
}

func TestS1l9PressureAndIndependentObjects(t *testing.T) {
	m, p, in := s1l9Fixture(t, 3)
	q := s1l9Producer([][3]int{{99, 0, 0}})
	s1l7Admit(t, m, q, gpu.ManagedGeometryAdmissionAccepted)
	other, _ := m.ManagedGeometryInputs(q.object)
	m.RequestManagedGeometryContentSweep(p.object, in)
	s1l9Queue(t, m, q, other, [3]int{99, 0, 0}, gpu.ManagedGeometryContentQueued)
	charge := m.ManagedGeometryAdmissionStats()
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: charge.InputBytes, MaxCopiedStageBytes: charge.TotalStageBytes})
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionPressure)
	s1l9Status(t, m, p, in, 0, true, 0, false)
	s1l9Status(t, m, q, other, 1, false, 0, false)
	callsP, callsQ := p.calls, q.calls
	for _, budget := range []gpu.ManagedGeometryAdmissionBudget{{Enabled: true, MaxInputBytes: charge.InputBytes - 1, MaxCopiedStageBytes: charge.TotalStageBytes - 1}, {Enabled: true}} {
		m.SetManagedGeometryAdmissionBudget(budget)
		before := m.ManagedGeometryAdmissionStats()
		if m.ServiceManagedGeometryContent(p.object, in, 1, func([3]int) bool { return true }) != 1 {
			t.Fatal("lowered cap service")
		}
		s1l9Status(t, m, q, other, 1, false, 0, false)
		if m.ManagedGeometryAdmissionStats() != before {
			t.Fatal("lowered cap service charges")
		}
	}
	if m.ServiceManagedGeometryContent(q.object, other, 99, func(c [3]int) bool {
		if c != ([3]int{99, 0, 0}) {
			t.Fatal("cross-owner work")
		}
		return true
	}) != 1 {
		t.Fatal("independent journal")
	}
	s1l9Status(t, m, p, in, 0, true, 2, false)
	s1l9Status(t, m, q, other, 0, false, 0, false)
	if p.calls != callsP || q.calls != callsQ {
		t.Fatal("status/service captured producers")
	}
}

func TestS1l9NoRendererPublicationOrStructuralCopies(t *testing.T) {
	m := s1l7Manager()
	p := s1l8Producer(4)
	derivative := p.object.XBrickMap
	before := derivative.Copy()
	rev, id, dirty, edit := derivative.Revision, derivative.ID, derivative.StructureDirty, derivative.GPUEditMode
	sectors, bricks := maps.Clone(derivative.DirtySectors), maps.Clone(derivative.DirtyBricks)
	ready, ps, pb := m.VoxelObjectReady(p.object, derivative, rev)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	m.ServiceManagedGeometry(p.object, 1)
	old := s1l8Stage(t, m, p)
	calls := p.calls
	charge := m.ManagedGeometryAdmissionStats()
	s1l9Queue(t, m, p, in, s1l9Coord(t, in, 2), gpu.ManagedGeometryContentQueued)
	m.RequestManagedGeometryContentSweep(p.object, in)
	m.ServiceManagedGeometryContent(p.object, in, 99, func([3]int) bool { return true })
	s1l9Status(t, m, p, in, 0, false, 0, false)
	s1l8Check(t, s1l8Stage(t, m, p), in, 1)
	s1l8Check(t, old, in, 1)
	if p.calls != calls || m.ManagedGeometryAdmissionStats() != charge {
		t.Fatal("content captured or changed reservations")
	}
	if p.object.XBrickMap != derivative || derivative.Revision != rev || derivative.ID != id || derivative.StructureDirty != dirty || derivative.GPUEditMode != edit || !reflect.DeepEqual(derivative.Sectors, before.Sectors) || !reflect.DeepEqual(derivative.SectorRevisions, before.SectorRevisions) || derivative.CachedMin != before.CachedMin || derivative.CachedMax != before.CachedMax || derivative.AABBDirty != before.AABBDirty || !maps.Equal(derivative.DirtySectors, sectors) || !maps.Equal(derivative.DirtyBricks, bricks) {
		t.Fatal("content scheduling changed derivative")
	}
	if r, s, b := m.VoxelObjectReady(p.object, derivative, rev); r != ready || s != ps || b != pb {
		t.Fatal("content scheduling changed readiness")
	}
	if len(m.Allocations) != 0 || len(m.MaterialAllocations) != 0 || len(m.SectorToInfo) != 0 || len(m.BrickToSlot) != 0 || len(m.PendingUpdates) != 0 || m.SectorTableBuf != nil || m.BrickTableBuf != nil || m.VoxelPayloadPageCount != 0 {
		t.Fatal("content scheduling touched GPU state")
	}
}

func TestS1l9LastVisitRetryAndOverflowDuringFollowup(t *testing.T) {
	m, p, in := s1l9Fixture(t, 1025)
	m.RequestManagedGeometryContentSweep(p.object, in)
	if n := m.ServiceManagedGeometryContent(p.object, in, 1024, func([3]int) bool { return true }); n != 1024 {
		t.Fatalf("initial prefix attempts = %d", n)
	}
	m.RequestManagedGeometryContentSweep(p.object, in)
	late := s1l9Coord(t, in, 0)
	last := s1l9Coord(t, in, 1024)
	s1l9Queue(t, m, p, in, late, gpu.ManagedGeometryContentQueued)
	inspectLast := func(c [3]int) {
		if c != last {
			t.Fatalf("final active coordinate = %v, want %v", c, last)
		}
		// Status inspection inside the callback is part of the public read protocol:
		// acknowledgement has not consumed this visit or transitioned the sweep yet.
		s1l9Status(t, m, p, in, 1, true, 1024, true)
	}
	if n := m.ServiceManagedGeometryContent(p.object, in, 99, func(c [3]int) bool { inspectLast(c); return false }); n != 1 {
		t.Fatalf("failed final attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, true, 1024, true)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("final callback panic swallowed")
			}
		}()
		m.ServiceManagedGeometryContent(p.object, in, 99, func(c [3]int) bool { inspectLast(c); panic("final visit") })
	}()
	s1l9Status(t, m, p, in, 1, true, 1024, true)
	if n := m.ServiceManagedGeometryContent(p.object, in, 1, func(c [3]int) bool { inspectLast(c); return true }); n != 1 {
		t.Fatalf("successful final retry attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, true, 0, false)
	index := 0
	if n := m.ServiceManagedGeometryContent(p.object, in, 5, func(c [3]int) bool {
		if c != s1l9Coord(t, in, index) {
			t.Fatal("followup prefix order")
		}
		index++
		return true
	}); n != 5 {
		t.Fatalf("followup prefix attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, true, 5, false)
	for round := 0; round < 2; round++ {
		for i := 0; i < 1024; i++ {
			want := gpu.ManagedGeometryContentQueued
			if round == 0 && i == 0 {
				want = gpu.ManagedGeometryContentCoalesced
			}
			s1l9Queue(t, m, p, in, s1l9Coord(t, in, i), want)
		}
		s1l9Status(t, m, p, in, 1024, true, 5, round > 0)
		s1l9Queue(t, m, p, in, s1l9Coord(t, in, 1024), gpu.ManagedGeometryContentSweepScheduled)
		s1l9Status(t, m, p, in, 0, true, 5, true)
	}
	// This notification follows the final overflow and remains after both passes.
	s1l9Queue(t, m, p, in, late, gpu.ManagedGeometryContentQueued)
	index = 5
	if n := m.ServiceManagedGeometryContent(p.object, in, 1020, func(c [3]int) bool {
		if c != s1l9Coord(t, in, index) {
			t.Fatal("followup overflow reset cursor")
		}
		index++
		return true
	}); n != 1020 {
		t.Fatalf("followup remainder attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, true, 0, false)
	index = 0
	if n := m.ServiceManagedGeometryContent(p.object, in, 1025, func(c [3]int) bool {
		if c != s1l9Coord(t, in, index) {
			t.Fatal("third pass order")
		}
		index++
		return true
	}); n != 1025 {
		t.Fatalf("third pass attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 1, false, 0, false)
	if n := m.ServiceManagedGeometryContent(p.object, in, 99, func(c [3]int) bool {
		if c != late {
			t.Fatal("late event lost across third pass")
		}
		return true
	}); n != 1 {
		t.Fatalf("late journal attempts = %d", n)
	}
	s1l9Status(t, m, p, in, 0, false, 0, false)
}
