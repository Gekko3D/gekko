package gpu_test

import (
	"maps"
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// The producer controls publication tokens but captures genuine sealed geometry.
type s1l7Producer struct {
	object     *core.VoxelObject
	owner      *volume.ManagedXBrickMap
	generation uint64
	calls      int
	available  bool
}

func s1l7NewProducer(sectors int) *s1l7Producer {
	raw := volume.NewXBrickMap()
	for i := 0; i < sectors; i++ {
		raw.SetVoxel(i*32, 0, 0, uint8(i+1))
	}
	p := &s1l7Producer{object: core.NewVoxelObject(), owner: volume.NewManagedXBrickMap(raw), available: true}
	p.object.XBrickMap = raw.Copy()
	p.install()
	return p
}
func (p *s1l7Producer) install() {
	p.object.SetManagedGeometryProducer(p.object.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) {
		p.calls++
		v, ok := p.owner.CaptureGeometry()
		return v, p.generation, ok && p.available
	})
}
func s1l7Manager() *gpu.GpuBufferManager {
	m := &gpu.GpuBufferManager{}
	m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	return m
}
func s1l7Admit(t *testing.T, m *gpu.GpuBufferManager, p *s1l7Producer, want gpu.ManagedGeometryAdmissionResult) {
	t.Helper()
	if got := m.AdmitManagedGeometry(p.object); got != want {
		t.Fatalf("admission = %v, want %v", got, want)
	}
}
func s1l7Absent(t *testing.T, input core.ManagedGeometryInput) {
	t.Helper()
	if input.SameSource(input) || input.Generation() != 0 || input.Geometry().Len() != 0 {
		t.Fatal("expected absent input")
	}
}
func s1l7Same(t *testing.T, got, want core.ManagedGeometryInput) {
	t.Helper()
	if !got.SameSource(want) || got.Generation() != want.Generation() || !reflect.DeepEqual(got.Geometry(), want.Geometry()) {
		t.Fatal("retained input changed")
	}
}
func s1l7Stats(t *testing.T, m *gpu.GpuBufferManager, inputs []core.ManagedGeometryInput, owners int) gpu.ManagedGeometryAdmissionStats {
	t.Helper()
	s := m.ManagedGeometryAdmissionStats()
	var input, reserved uint64
	// These language-level sizes specify reservations, not private ledger layout.
	type node struct {
		left, right       *node
		sector            *volume.Sector
		bytes, generation uint64
	}
	for _, in := range inputs {
		v := in.Geometry()
		input += v.RetainedBytes()
		var nodes, scratch uint64
		if v.Len() > 0 {
			nodes = 2*uint64(v.Len()) - 1
			scratch = 1
			for span := uint64(1); span < uint64(v.Len()); span *= 2 {
				scratch++
			}
		}
		reserved += v.CopyBytes() + (nodes+scratch)*uint64(unsafe.Sizeof(node{})) + uint64(unsafe.Sizeof([1024][3]int{}))
	}
	if s.InputBytes != input || s.ReservedCopiedStageBytes != reserved || s.OwnerCount != owners || s.GenerationCount != len(inputs) {
		t.Fatalf("ownership stats = %+v, want input %d, reserved %d, owners %d, generations %d", s, input, reserved, owners, len(inputs))
	}
	if s.TotalStageBytes != s.ReservedCopiedStageBytes+s.OwnedMetadataBytes {
		t.Fatal("stage total omits owned metadata")
	}
	if len(inputs) > 0 && s.OwnedMetadataBytes == 0 {
		t.Fatal("owned ledger metadata is uncharged")
	}
	if len(inputs) == 0 && s != (gpu.ManagedGeometryAdmissionStats{}) {
		t.Fatalf("empty ledger retains charges: %+v", s)
	}
	b := m.ManagedGeometryAdmissionBudget()
	var ip, sp uint64
	if s.InputBytes > b.MaxInputBytes {
		ip = s.InputBytes - b.MaxInputBytes
	}
	if s.TotalStageBytes > b.MaxCopiedStageBytes {
		sp = s.TotalStageBytes - b.MaxCopiedStageBytes
	}
	if s.InputPressureBytes != ip || s.StagePressureBytes != sp {
		t.Fatalf("pressure = %+v, want %d/%d", s, ip, sp)
	}
	return s
}

func TestS1l7NilDefaultAndLazyReads(t *testing.T) {
	p := s1l7NewProducer(1)
	var nilManager *gpu.GpuBufferManager
	if got := nilManager.AdmitManagedGeometry(p.object); got != gpu.ManagedGeometryAdmissionDisabled {
		t.Fatal("nil manager must be disabled")
	}
	nilManager.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	if nilManager.ManagedGeometryAdmissionBudget() != (gpu.ManagedGeometryAdmissionBudget{}) {
		t.Fatal("nil manager retained policy")
	}
	s1l7Stats(t, nilManager, nil, 0)
	a, b := nilManager.ManagedGeometryInputs(p.object)
	s1l7Absent(t, a)
	s1l7Absent(t, b)
	if nilManager.CancelManagedGeometryInputs(p.object) || nilManager.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("nil manager mutated ownership")
	}
	m := &gpu.GpuBufferManager{}
	if m.ManagedGeometryAdmissionBudget() != (gpu.ManagedGeometryAdmissionBudget{}) {
		t.Fatal("zero manager enabled by default")
	}
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionDisabled)
	def := gpu.DefaultManagedGeometryAdmissionBudget()
	if !def.Enabled || def.MaxInputBytes != 128<<20 || def.MaxCopiedStageBytes != 128<<20 {
		t.Fatalf("default policy = %+v", def)
	}
	m.SetManagedGeometryAdmissionBudget(def)
	if m.ManagedGeometryAdmissionBudget() != def {
		t.Fatal("budget getter changed policy")
	}
	a, b = m.ManagedGeometryInputs(p.object)
	s1l7Absent(t, a)
	s1l7Absent(t, b)
	s1l7Stats(t, m, nil, 0)
	if p.calls != 0 {
		t.Fatal("policy or reads captured producer")
	}
	if m.AdmitManagedGeometry(nil) != gpu.ManagedGeometryAdmissionUnavailable || m.CancelManagedGeometryInputs(nil) || m.AdvanceManagedGeometryInput(nil, a) {
		t.Fatal("nil object is not safe")
	}
	a, b = m.ManagedGeometryInputs(nil)
	s1l7Absent(t, a)
	s1l7Absent(t, b)
}

func TestS1l7QualifiedEmptyAndUnavailablePreserveHistory(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(0)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, b := m.ManagedGeometryInputs(p.object)
	s1l7Absent(t, b)
	if !a.SameSource(a) || a.Geometry().Len() != 0 {
		t.Fatal("qualified empty lost identity")
	}
	before := s1l7Stats(t, m, []core.ManagedGeometryInput{a}, 1)
	p.available = false
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnavailable)
	if m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("unavailable producer dropped ownership")
	}
	retained, _ := m.ManagedGeometryInputs(p.object)
	s1l7Same(t, retained, a)
	for name, invalidate := range map[string]func(*core.VoxelObject){
		"raw":        func(o *core.VoxelObject) { o.SetManagedGeometryProducer(o.XBrickMap, nil) },
		"gpu":        func(o *core.VoxelObject) { o.XBrickMap.GPUEditMode = true },
		"derivative": func(o *core.VoxelObject) { o.XBrickMap = o.XBrickMap.Copy() },
		"terrain":    func(o *core.VoxelObject) { o.IsTerrainChunk = true },
		"planet":     func(o *core.VoxelObject) { o.IsPlanetTile = true },
		"lod": func(o *core.VoxelObject) {
			if !o.SetRenderLOD2(o.XBrickMap.Copy()) {
				t.Fatal("LOD setup")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p := s1l7NewProducer(1)
			m := s1l7Manager()
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
			a, _ := m.ManagedGeometryInputs(p.object)
			before := m.ManagedGeometryAdmissionStats()
			calls := p.calls
			invalidate(p.object)
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnavailable)
			if p.calls != calls || m.ManagedGeometryAdmissionStats() != before {
				t.Fatal("ineligible producer captured or changed ledger")
			}
			got, _ := m.ManagedGeometryInputs(p.object)
			s1l7Same(t, got, a)
		})
	}
}

func TestS1l7SuccessorCoalescingMonotonicAndSourceIdentity(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(1)
	p.generation = 7
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnchanged)
	p.generation = 8
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	got, b := m.ManagedGeometryInputs(p.object)
	s1l7Same(t, got, a)
	s1l7Stats(t, m, []core.ManagedGeometryInput{a, b}, 1)
	p.generation = 9
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	got, c := m.ManagedGeometryInputs(p.object)
	s1l7Same(t, got, a)
	if c.Generation() != 9 || !c.SameSource(b) {
		t.Fatal("successor did not coalesce")
	}
	before := s1l7Stats(t, m, []core.ManagedGeometryInput{a, c}, 1)
	for _, gen := range []uint64{8, 7, 0} {
		p.generation = gen
		s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionStale)
	}
	p.generation = 9
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnchanged)
	p.install()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionSourceChanged)
	if m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("refusal changed charges")
	}
	got, b = m.ManagedGeometryInputs(p.object)
	s1l7Same(t, got, a)
	s1l7Same(t, b, c)
	if !m.CancelManagedGeometryInputs(p.object) || m.CancelManagedGeometryInputs(p.object) {
		t.Fatal("cancel presence semantics")
	}
	s1l7Stats(t, m, nil, 0)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	fresh, _ := m.ManagedGeometryInputs(p.object)
	if fresh.SameSource(a) || fresh.Generation() != 9 {
		t.Fatal("cancel did not permit new attachment")
	}
	p.generation = math.MaxUint64
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	before = m.ManagedGeometryAdmissionStats()
	p.generation = 0
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionStale)
	if m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("generation wrap released input")
	}
}

func TestS1l7AdvanceRequiresAcceptedSourceAndGeneration(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(1)
	p.generation = 1
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	if m.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("advance without successor")
	}
	p.generation = 2
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, b := m.ManagedGeometryInputs(p.object)
	other := s1l7NewProducer(1)
	other.generation = 1
	wrong, _ := other.object.CaptureManagedGeometryInput()
	before := m.ManagedGeometryAdmissionStats()
	for _, expected := range []core.ManagedGeometryInput{{}, wrong, b} {
		if m.AdvanceManagedGeometryInput(p.object, expected) || m.ManagedGeometryAdmissionStats() != before {
			t.Fatal("stale expectation advanced or changed charges")
		}
	}
	if !m.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("matching accepted did not advance")
	}
	got, next := m.ManagedGeometryInputs(p.object)
	s1l7Same(t, got, b)
	s1l7Absent(t, next)
	s1l7Stats(t, m, []core.ManagedGeometryInput{b}, 1)
	p.generation = 3
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	if m.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("historical accepted advanced newer pair")
	}
}

func TestS1l7GlobalChargesSharedBackingAndRelease(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(2)
	q := s1l7NewProducer(0)
	q.owner = p.owner
	q.object.XBrickMap = p.object.XBrickMap
	q.install()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	one := s1l7Stats(t, m, []core.ManagedGeometryInput{a}, 1)
	s1l7Admit(t, m, q, gpu.ManagedGeometryAdmissionAccepted)
	b, _ := m.ManagedGeometryInputs(q.object)
	two := s1l7Stats(t, m, []core.ManagedGeometryInput{a, b}, 2)
	if two.InputBytes != 2*one.InputBytes || two.TotalStageBytes != 2*one.TotalStageBytes {
		t.Fatal("shared views must retain conservative independent charges")
	}
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, c := m.ManagedGeometryInputs(p.object)
	s1l7Stats(t, m, []core.ManagedGeometryInput{a, b, c}, 2)
	if !m.CancelManagedGeometryInputs(q.object) {
		t.Fatal("second owner not canceled")
	}
	s1l7Stats(t, m, []core.ManagedGeometryInput{a, c}, 1)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
	s1l7Stats(t, m, nil, 0)
	x, y := m.ManagedGeometryInputs(p.object)
	s1l7Absent(t, x)
	s1l7Absent(t, y)
	if a.Geometry().Len() != 2 || b.Geometry().Len() != 2 || c.Geometry().Len() != 2 {
		t.Fatal("release invalidated caller inputs")
	}
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionDisabled)
	m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
}

func TestS1l7ExactCapsZeroPauseAndLoweredPressure(t *testing.T) {
	p := s1l7NewProducer(1)
	probe := s1l7Manager()
	s1l7Admit(t, probe, p, gpu.ManagedGeometryAdmissionAccepted)
	charge := probe.ManagedGeometryAdmissionStats()
	for _, tc := range []struct {
		name         string
		input, stage uint64
		want         gpu.ManagedGeometryAdmissionResult
	}{
		{"exact", charge.InputBytes, charge.TotalStageBytes, gpu.ManagedGeometryAdmissionAccepted},
		{"input-minus-one", charge.InputBytes - 1, charge.TotalStageBytes, gpu.ManagedGeometryAdmissionPressure},
		{"stage-minus-one", charge.InputBytes, charge.TotalStageBytes - 1, gpu.ManagedGeometryAdmissionPressure},
		{"zero-input", 0, charge.TotalStageBytes, gpu.ManagedGeometryAdmissionPressure},
		{"zero-stage", charge.InputBytes, 0, gpu.ManagedGeometryAdmissionPressure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := s1l7Manager()
			m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: tc.input, MaxCopiedStageBytes: tc.stage})
			s1l7Admit(t, m, p, tc.want)
			if tc.want == gpu.ManagedGeometryAdmissionPressure {
				s1l7Stats(t, m, nil, 0)
			}
		})
	}
	// Empty inputs still require a nonzero input policy and stage reservations.
	empty := s1l7NewProducer(0)
	m := s1l7Manager()
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true, MaxCopiedStageBytes: 128 << 20})
	s1l7Admit(t, m, empty, gpu.ManagedGeometryAdmissionPressure)
	m = probe
	a, _ := m.ManagedGeometryInputs(p.object)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: charge.InputBytes - 1, MaxCopiedStageBytes: charge.TotalStageBytes - 1})
	s1l7Stats(t, m, []core.ManagedGeometryInput{a}, 1)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnchanged)
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionPressure)
	got, next := m.ManagedGeometryInputs(p.object)
	s1l7Same(t, got, a)
	s1l7Absent(t, next)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
	s1l7Stats(t, m, []core.ManagedGeometryInput{a}, 1)
	p.generation--
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnchanged)
}

func TestS1l7ReplacementPreflightsTransientGlobalPeak(t *testing.T) {
	for _, domain := range []string{"input", "stage"} {
		t.Run(domain, func(t *testing.T) {
			p := s1l7NewProducer(1)
			q := s1l7NewProducer(1)
			m := s1l7Manager()
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
			a, _ := m.ManagedGeometryInputs(p.object)
			one := m.ManagedGeometryAdmissionStats()
			p.generation = 1
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
			_, b := m.ManagedGeometryInputs(p.object)
			pair := m.ManagedGeometryAdmissionStats()
			s1l7Admit(t, m, q, gpu.ManagedGeometryAdmissionAccepted)
			c, _ := m.ManagedGeometryInputs(q.object)
			before := m.ManagedGeometryAdmissionStats()
			incomingInput := pair.InputBytes - one.InputBytes
			incomingStage := pair.TotalStageBytes - one.TotalStageBytes
			budget := gpu.DefaultManagedGeometryAdmissionBudget()
			if domain == "input" {
				budget.MaxInputBytes = before.InputBytes + incomingInput - 1
			} else {
				budget.MaxCopiedStageBytes = before.TotalStageBytes + incomingStage - 1
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			p.generation = 2
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionPressure)
			if m.ManagedGeometryAdmissionStats() != before {
				t.Fatal("transient refusal released old successor")
			}
			got, next := m.ManagedGeometryInputs(p.object)
			s1l7Same(t, got, a)
			s1l7Same(t, next, b)
			if domain == "input" {
				budget.MaxInputBytes++
			} else {
				budget.MaxCopiedStageBytes++
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
			got, next = m.ManagedGeometryInputs(p.object)
			s1l7Same(t, got, a)
			if next.Generation() != 2 {
				t.Fatal("larger cap did not replace successor")
			}
			s1l7Stats(t, m, []core.ManagedGeometryInput{a, next, c}, 2)
			if m.ManagedGeometryAdmissionStats() != before {
				t.Fatal("replacement retained obsolete descriptor")
			}
		})
	}
}

func TestS1l7HistoricalIsolationAndNoRendererPublication(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(1)
	derivative := p.object.XBrickMap
	before := derivative.Copy()
	rev := derivative.Revision
	id, structureDirty, gpuEdit := derivative.ID, derivative.StructureDirty, derivative.GPUEditMode
	dirtySectors, dirtyBricks := maps.Clone(derivative.DirtySectors), maps.Clone(derivative.DirtyBricks)
	ready, ps, pb := m.VoxelObjectReady(p.object, derivative, rev)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	frozen, _ := a.Geometry().CopySector(0)
	p.owner.SetVoxel(0, 0, 0, 9)
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, b := m.ManagedGeometryInputs(p.object)
	current, _ := b.Geometry().CopySector(0)
	if reflect.DeepEqual(current, frozen) {
		t.Fatal("real producer edit missing from successor")
	}
	copied, _ := a.Geometry().CopySector(0)
	copied.PackedBricks[0].Payload[0][0][0] = 99
	again, _ := a.Geometry().CopySector(0)
	if !reflect.DeepEqual(again, frozen) {
		t.Fatal("caller sector copy mutated retained generation")
	}
	if !m.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("advance failed")
	}
	m.CancelManagedGeometryInputs(p.object)
	p.object.SetManagedGeometryProducer(derivative, nil)
	p.owner.ExposeMutable().SetVoxel(0, 0, 0, 3)
	again, _ = a.Geometry().CopySector(0)
	if !reflect.DeepEqual(again, frozen) {
		t.Fatal("history lost after cancel, exposure and producer clear")
	}
	if p.object.XBrickMap != derivative || derivative.Revision != rev || !reflect.DeepEqual(derivative.Sectors, before.Sectors) || !reflect.DeepEqual(derivative.SectorRevisions, before.SectorRevisions) || derivative.CachedMin != before.CachedMin || derivative.CachedMax != before.CachedMax || derivative.AABBDirty != before.AABBDirty || derivative.ID != id || derivative.StructureDirty != structureDirty || derivative.GPUEditMode != gpuEdit || !maps.Equal(derivative.DirtySectors, dirtySectors) || !maps.Equal(derivative.DirtyBricks, dirtyBricks) {
		t.Fatal("CPU ledger changed renderer derivative")
	}
	r, s, br := m.VoxelObjectReady(p.object, derivative, rev)
	if r != ready || s != ps || br != pb {
		t.Fatal("CPU ledger changed GPU readiness")
	}
	if len(m.Allocations) != 0 || len(m.MaterialAllocations) != 0 || len(m.SectorToInfo) != 0 || len(m.BrickToSlot) != 0 || len(m.PendingUpdates) != 0 || m.SectorTableBuf != nil || m.BrickTableBuf != nil || m.VoxelPayloadPageCount != 0 {
		t.Fatal("CPU ledger allocated or queued GPU state")
	}
}

func TestS1l7NewOwnerMustFitGlobalCaps(t *testing.T) {
	for _, domain := range []string{"input", "stage"} {
		t.Run(domain, func(t *testing.T) {
			m := s1l7Manager()
			p, q := s1l7NewProducer(1), s1l7NewProducer(1)
			s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
			a, _ := m.ManagedGeometryInputs(p.object)
			before := m.ManagedGeometryAdmissionStats()
			budget := gpu.DefaultManagedGeometryAdmissionBudget()
			if domain == "input" {
				budget.MaxInputBytes = 2*before.InputBytes - 1
			} else {
				budget.MaxCopiedStageBytes = 2*before.TotalStageBytes - 1
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			s1l7Admit(t, m, q, gpu.ManagedGeometryAdmissionPressure)
			if m.ManagedGeometryAdmissionStats() != before {
				t.Fatal("global cap refusal changed first owner")
			}
			got, _ := m.ManagedGeometryInputs(p.object)
			s1l7Same(t, got, a)
			x, y := m.ManagedGeometryInputs(q.object)
			s1l7Absent(t, x)
			s1l7Absent(t, y)
			// The refused object fits this policy alone once the first owner releases.
			m.CancelManagedGeometryInputs(p.object)
			s1l7Admit(t, m, q, gpu.ManagedGeometryAdmissionAccepted)
		})
	}
}

func TestS1l7AdvanceAndCancelAllowedUnderPressure(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(1)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, b := m.ManagedGeometryInputs(p.object)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
	s1l7Stats(t, m, []core.ManagedGeometryInput{a, b}, 1)
	if !m.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("pressure blocked ownership release")
	}
	got, next := m.ManagedGeometryInputs(p.object)
	s1l7Same(t, got, b)
	s1l7Absent(t, next)
	s1l7Stats(t, m, []core.ManagedGeometryInput{b}, 1)
	if !m.CancelManagedGeometryInputs(p.object) {
		t.Fatal("pressure blocked cancellation")
	}
	s1l7Stats(t, m, nil, 0)
}

func TestS1l7CancelThreeOwnersInVariedOrders(t *testing.T) {
	for _, order := range [][3]int{{1, 0, 2}, {0, 2, 1}, {2, 1, 0}} {
		t.Run(string(rune('a'+order[0])), func(t *testing.T) {
			m := s1l7Manager()
			ps := [3]*s1l7Producer{s1l7NewProducer(1), s1l7NewProducer(2), s1l7NewProducer(0)}
			var inputs [3]core.ManagedGeometryInput
			live := [3]bool{true, true, true}
			for i, p := range ps {
				s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
				inputs[i], _ = m.ManagedGeometryInputs(p.object)
			}
			for _, index := range order {
				if !m.CancelManagedGeometryInputs(ps[index].object) {
					t.Fatal("live owner missing")
				}
				live[index] = false
				var expected []core.ManagedGeometryInput
				for i, yes := range live {
					if yes {
						expected = append(expected, inputs[i])
						got, _ := m.ManagedGeometryInputs(ps[i].object)
						s1l7Same(t, got, inputs[i])
					}
				}
				s1l7Stats(t, m, expected, len(expected))
			}
		})
	}
}

func TestS1l7DisabledNonzeroPolicyReleasesAndStaysLazy(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(1)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	calls := p.calls
	budget := gpu.ManagedGeometryAdmissionBudget{Enabled: false, MaxInputBytes: 17, MaxCopiedStageBytes: 29}
	m.SetManagedGeometryAdmissionBudget(budget)
	if m.ManagedGeometryAdmissionBudget() != budget {
		t.Fatal("disabled policy getter changed configured caps")
	}
	s1l7Stats(t, m, nil, 0)
	a, b := m.ManagedGeometryInputs(p.object)
	s1l7Absent(t, a)
	s1l7Absent(t, b)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionDisabled)
	if p.calls != calls {
		t.Fatal("disabled policy captured producer")
	}
}

func TestS1l7CapturePolicyChangeToDisabledRefusesAdmission(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(1)
	disabled := gpu.ManagedGeometryAdmissionBudget{Enabled: false, MaxInputBytes: 128 << 20, MaxCopiedStageBytes: 128 << 20}
	calls := 0
	p.object.SetManagedGeometryProducer(p.object.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) {
		calls++
		m.SetManagedGeometryAdmissionBudget(disabled)
		view, ok := p.owner.CaptureGeometry()
		return view, 1, ok
	})
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionDisabled)
	if calls != 1 {
		t.Fatalf("capture calls = %d, want 1", calls)
	}
	if m.ManagedGeometryAdmissionBudget() != disabled {
		t.Fatal("capture policy change was lost")
	}
	a, b := m.ManagedGeometryInputs(p.object)
	s1l7Absent(t, a)
	s1l7Absent(t, b)
	s1l7Stats(t, m, nil, 0)
}
