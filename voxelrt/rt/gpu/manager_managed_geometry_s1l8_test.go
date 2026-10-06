package gpu_test

import (
	"maps"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Real sealed geometry, with signed XYZ ordering, varied packed occupancy and
// nil, empty and populated auxiliary slices. No service instrumentation is used.
func s1l8Producer(n int) *s1l7Producer {
	raw := volume.NewXBrickMap()
	for i := n - 1; i >= 0; i-- {
		x, y, z := i/9-2, (i/3)%3-1, i%3-1
		for b := 0; b <= i%3; b++ {
			raw.SetVoxel(x*32+b*8+4, y*32+4, z*32+4, uint8(i+b+1))
		}
	}
	for key, sector := range raw.Sectors {
		for b, brick := range sector.PackedBricks {
			switch (key[0] + key[1] + key[2] + b + 12) % 3 {
			case 1:
				brick.PrecomputedAux = make([]byte, 0)
			case 2:
				brick.PrecomputedAux = make([]byte, 7, 4096)
				for j := range brick.PrecomputedAux {
					brick.PrecomputedAux[j] = byte(j + b + 1)
				}
			}
		}
	}
	p := s1l7NewProducer(0)
	p.owner = volume.NewManagedXBrickMap(raw)
	p.object.XBrickMap = raw.Copy()
	p.install()
	return p
}

func s1l8Check(t *testing.T, stage gpu.ManagedGeometryStageView, input core.ManagedGeometryInput, count int) {
	t.Helper()
	if !stage.Input().SameSource(input) || stage.Input().Generation() != input.Generation() {
		t.Fatal("stage input source or generation changed")
	}
	if stage.Len() != count || stage.Total() != input.Geometry().Len() || stage.Complete() != (count == input.Geometry().Len()) {
		t.Fatalf("stage count/total/complete = %d/%d/%v, want %d/%d/%v", stage.Len(), stage.Total(), stage.Complete(), count, input.Geometry().Len(), count == input.Geometry().Len())
	}
	var bytes uint64
	for i := 0; i < count; i++ {
		coord, ok := stage.Coord(i)
		wantCoord, _ := input.Geometry().Coord(i)
		if !ok || coord != wantCoord {
			t.Fatalf("coordinate %d = %v/%v, want %v", i, coord, ok, wantCoord)
		}
		sector, ok := stage.CopySector(i)
		wantSector, _ := input.Geometry().CopySector(i)
		if !ok || !reflect.DeepEqual(sector, wantSector) {
			t.Fatalf("sector %d differs from captured source", i)
		}
		charge, ok := input.Geometry().CopySectorBytes(i)
		if !ok {
			t.Fatal("source copy charge unavailable")
		}
		bytes += charge
	}
	if stage.CopiedBytes() != bytes {
		t.Fatalf("copied bytes = %d, want %d", stage.CopiedBytes(), bytes)
	}
	for _, i := range []int{-1, count, count + 1, int(^uint(0) >> 1)} {
		if coord, ok := stage.Coord(i); ok || coord != ([3]int{}) {
			t.Fatalf("invalid coordinate %d = %v/%v", i, coord, ok)
		}
		if sector, ok := stage.CopySector(i); ok || sector != nil {
			t.Fatalf("invalid sector %d = %v/%v", i, sector, ok)
		}
	}
}
func s1l8Stage(t *testing.T, m *gpu.GpuBufferManager, p *s1l7Producer) gpu.ManagedGeometryStageView {
	t.Helper()
	stage, ok := m.ManagedGeometryStage(p.object)
	if !ok {
		t.Fatal("accepted stage absent")
	}
	return stage
}
func s1l8Absent(t *testing.T, stage gpu.ManagedGeometryStageView) {
	t.Helper()
	s1l7Absent(t, stage.Input())
	if stage.Len() != 0 || stage.Total() != 0 || stage.CopiedBytes() != 0 || !stage.Complete() {
		t.Fatal("zero stage semantics")
	}
	for _, i := range []int{-1, 0, 1, int(^uint(0) >> 1)} {
		if c, ok := stage.Coord(i); ok || c != ([3]int{}) {
			t.Fatal("absent coordinate present")
		}
		if s, ok := stage.CopySector(i); ok || s != nil {
			t.Fatal("absent sector present")
		}
	}
}

func TestS1l8NilMissingDisabledAndEmpty(t *testing.T) {
	p := s1l8Producer(1)
	var nilManager *gpu.GpuBufferManager
	s1l8Absent(t, gpu.ManagedGeometryStageView{})
	for _, m := range []*gpu.GpuBufferManager{nilManager, {}, s1l7Manager()} {
		for _, object := range []*core.VoxelObject{nil, p.object, core.NewVoxelObject()} {
			if m.ServiceManagedGeometry(object, 99) != 0 {
				t.Fatal("absent service did work")
			}
			stage, ok := m.ManagedGeometryStage(object)
			if ok {
				t.Fatal("absent stage available")
			}
			s1l8Absent(t, stage)
		}
	}
	if p.calls != 0 {
		t.Fatal("service or stage read captured producer")
	}
	m := s1l7Manager()
	empty := s1l8Producer(0)
	s1l7Admit(t, m, empty, gpu.ManagedGeometryAdmissionAccepted)
	input, _ := m.ManagedGeometryInputs(empty.object)
	s1l8Check(t, s1l8Stage(t, m, empty), input, 0)
	calls := empty.calls
	before := m.ManagedGeometryAdmissionStats()
	if m.ServiceManagedGeometry(empty.object, 99) != 0 || empty.calls != calls || m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("empty service changed state")
	}
}

func TestS1l8BoundedPrefixAndFrozenViews(t *testing.T) {
	m := s1l7Manager()
	p := s1l8Producer(33)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	input, _ := m.ManagedGeometryInputs(p.object)
	if input.Geometry().Len() != 33 {
		t.Fatal("fixture sector count")
	}
	spareAux := false
	for i := 0; i < input.Geometry().Len(); i++ {
		sector, _ := input.Geometry().CopySector(i)
		for _, brick := range sector.PackedBricks {
			if len(brick.PrecomputedAux) > 0 && cap(brick.PrecomputedAux) > len(brick.PrecomputedAux) {
				spareAux = true
			}
		}
	}
	if !spareAux {
		t.Fatal("captured fixture lacks nonempty auxiliary backing with spare capacity")
	}
	stats := s1l7Stats(t, m, []core.ManagedGeometryInput{input}, 1)
	calls := p.calls
	count := 0
	type history struct {
		stage gpu.ManagedGeometryStageView
		count int
	}
	saved := []history{{s1l8Stage(t, m, p), 0}}
	for _, allowance := range []int{0, -1, 1, 0, -1, 1, 16, 0, -1, 16, 1000, 1} {
		want := min(max(allowance, 0), 33-count)
		if got := m.ServiceManagedGeometry(p.object, allowance); got != want {
			t.Fatalf("allowance %d copied %d, want %d", allowance, got, want)
		}
		count += want
		current := s1l8Stage(t, m, p)
		s1l8Check(t, current, input, count)
		for _, old := range saved {
			s1l8Check(t, old.stage, input, old.count)
		}
		saved = append(saved, history{current, count})
		if p.calls != calls || m.ManagedGeometryAdmissionStats() != stats {
			t.Fatal("service captured producer or changed reservations")
		}
	}
	if count != 33 {
		t.Fatal("fixture not fully serviced")
	}
}

func TestS1l8EmptyAndFullSectorOccupancy(t *testing.T) {
	raw := volume.NewXBrickMap()
	emptyKey := [3]int{-2, 1, -1}
	legacyHeader := [3]int{9, 8, 7}
	raw.Sectors[emptyKey] = volume.NewSector(legacyHeader[0], legacyHeader[1], legacyHeader[2])
	fullKey := [3]int{1, -1, 2}
	for b := 0; b < 64; b++ {
		raw.SetVoxel(fullKey[0]*32+b%4*8+4, fullKey[1]*32+b/4%4*8+4, fullKey[2]*32+b/16*8+4, uint8(b+1))
	}
	p := s1l7NewProducer(0)
	p.owner = volume.NewManagedXBrickMap(raw)
	p.object.XBrickMap = raw.Copy()
	p.install()
	m := s1l7Manager()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	input, _ := m.ManagedGeometryInputs(p.object)
	if input.Geometry().Len() != 2 {
		t.Fatal("empty and full sectors must both be captured")
	}
	first, _ := input.Geometry().CopySector(0)
	second, _ := input.Geometry().CopySector(1)
	coord, ok := input.Geometry().Coord(0)
	if !ok || coord != emptyKey || first.Coords != legacyHeader || len(first.PackedBricks) != 0 || first.BrickMask64 != 0 || len(second.PackedBricks) != 64 || second.BrickMask64 != ^uint64(0) {
		t.Fatal("empty/full occupancy or legacy header fixture changed")
	}
	charges := m.ManagedGeometryAdmissionStats()
	calls := p.calls
	for count := 1; count <= 2; count++ {
		if m.ServiceManagedGeometry(p.object, 1) != 1 {
			t.Fatal("one sector allowance depended on brick occupancy")
		}
		s1l8Check(t, s1l8Stage(t, m, p), input, count)
	}
	if p.calls != calls || m.ManagedGeometryAdmissionStats() != charges || m.ServiceManagedGeometry(p.object, 1) != 0 {
		t.Fatal("occupancy service recaptured, recharged or copied past completion")
	}
}

func TestS1l8IndependentObjects(t *testing.T) {
	m := s1l7Manager()
	p, q := s1l8Producer(4), s1l8Producer(7)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	s1l7Admit(t, m, q, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	b, _ := m.ManagedGeometryInputs(q.object)
	callsP, callsQ := p.calls, q.calls
	charges := m.ManagedGeometryAdmissionStats()
	for _, tc := range []struct {
		p               *s1l7Producer
		allowance, want int
	}{{p, 1, 1}, {q, 2, 2}, {p, 99, 3}, {q, 1, 1}, {p, 99, 0}, {q, int(^uint(0) >> 1), 4}} {
		if got := m.ServiceManagedGeometry(tc.p.object, tc.allowance); got != tc.want {
			t.Fatalf("independent service = %d, want %d", got, tc.want)
		}
	}
	s1l8Check(t, s1l8Stage(t, m, p), a, 4)
	s1l8Check(t, s1l8Stage(t, m, q), b, 7)
	s1l7Stats(t, m, []core.ManagedGeometryInput{a, b}, 2)
	if p.calls != callsP || q.calls != callsQ || m.ManagedGeometryAdmissionStats() != charges {
		t.Fatal("service captured independent producer or changed reservations")
	}
}

func TestS1l8SuccessorRefusalsAndExplicitPromotion(t *testing.T) {
	m := s1l7Manager()
	p := s1l7NewProducer(3)
	p.generation = 1
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	if m.ServiceManagedGeometry(p.object, 1) != 1 {
		t.Fatal("initial prefix")
	}
	old := s1l8Stage(t, m, p)
	// Change both content and topology without changing sector count.
	p.owner.SetVoxel(0, 0, 0, 9)
	p.owner.SetVoxel(64, 0, 0, 0)
	p.owner.SetVoxel(-32, 0, 0, 7)
	p.generation = 2
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, b := m.ManagedGeometryInputs(p.object)
	beforeCoord, beforeOK := a.Geometry().Coord(0)
	afterCoord, afterOK := b.Geometry().Coord(0)
	if b.Geometry().Len() != a.Geometry().Len() || !beforeOK || !afterOK || beforeCoord != ([3]int{}) || afterCoord != ([3]int{-1, 0, 0}) {
		t.Fatal("equal-count topology edit missing")
	}
	s1l8Check(t, s1l8Stage(t, m, p), a, 1)
	p.owner.SetVoxel(-32, 0, 0, 8)
	p.generation = 3
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	_, c := m.ManagedGeometryInputs(p.object)
	p.available = false
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionUnavailable)
	p.available = true
	p.install()
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionSourceChanged)
	calls := p.calls
	charges := m.ManagedGeometryAdmissionStats()
	if m.ServiceManagedGeometry(p.object, 1) != 1 {
		t.Fatal("refusal reset or stopped historical service")
	}
	s1l8Check(t, s1l8Stage(t, m, p), a, 2)
	s1l8Check(t, old, a, 1)
	if p.calls != calls || m.ManagedGeometryAdmissionStats() != charges {
		t.Fatal("historical service changed input or reservations")
	}
	if !m.AdvanceManagedGeometryInput(p.object, a) {
		t.Fatal("explicit promotion before completion failed")
	}
	s1l8Check(t, s1l8Stage(t, m, p), c, 0)
	s1l8Check(t, old, a, 1)
	if m.ServiceManagedGeometry(p.object, 1) != 1 {
		t.Fatal("promoted service")
	}
	promoted := s1l8Stage(t, m, p)
	s1l8Check(t, promoted, c, 1)
	if !m.CancelManagedGeometryInputs(p.object) {
		t.Fatal("cancel failed")
	}
	absent, ok := m.ManagedGeometryStage(p.object)
	if ok {
		t.Fatal("canceled stage present")
	}
	s1l8Absent(t, absent)
	s1l8Check(t, old, a, 1)
	s1l8Check(t, promoted, c, 1)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	if m.ServiceManagedGeometry(p.object, 2) != 2 {
		t.Fatal("fresh service")
	}
	freshInput, _ := m.ManagedGeometryInputs(p.object)
	fresh := s1l8Stage(t, m, p)
	m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
	s1l8Check(t, fresh, freshInput, 2)
	s1l8Check(t, old, a, 1)
	s1l8Check(t, promoted, c, 1)
	absent, ok = m.ManagedGeometryStage(p.object)
	if ok || m.ServiceManagedGeometry(p.object, 99) != 0 {
		t.Fatal("disabled stage or service present")
	}
	s1l8Absent(t, absent)
}

func TestS1l8DefensiveSectorCopies(t *testing.T) {
	m := s1l7Manager()
	p := s1l8Producer(5)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	input, _ := m.ManagedGeometryInputs(p.object)
	if m.ServiceManagedGeometry(p.object, 3) != 3 {
		t.Fatal("initial service")
	}
	old := s1l8Stage(t, m, p)
	populatedAux := false
	for i := 0; i < old.Len(); i++ {
		sector, _ := old.CopySector(i)
		sector.Coords = [3]int{99, 99, 99}
		sector.BrickMask64 = 0
		for _, brick := range sector.PackedBricks {
			brick.Payload[0][0][0] = 255
			brick.Flags ^= 1
			brick.AtlasOffset++
			if len(brick.PrecomputedAux) > 0 {
				populatedAux = true
				brick.PrecomputedAux[0] = 255
			}
			brick.PrecomputedAux = append(brick.PrecomputedAux, 99)
		}
		sector.PackedBricks[0] = nil
		sector.PackedBricks = append(sector.PackedBricks, nil)
	}
	if !populatedAux {
		t.Fatal("aux isolation fixture missing")
	}
	s1l8Check(t, old, input, 3)
	s1l8Check(t, s1l8Stage(t, m, p), input, 3)
	if m.ServiceManagedGeometry(p.object, 99) != 2 {
		t.Fatal("remaining service")
	}
	s1l8Check(t, old, input, 3)
	p.owner.ExposeMutable().SetVoxel(-60, -28, -28, 99)
	m.CancelManagedGeometryInputs(p.object)
	s1l8Check(t, old, input, 3)
}

func TestS1l8LoweredCapsPreserveServiceCharges(t *testing.T) {
	m := s1l7Manager()
	p := s1l8Producer(5)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	if m.ServiceManagedGeometry(p.object, 1) != 1 {
		t.Fatal("initial service")
	}
	charge := m.ManagedGeometryAdmissionStats()
	for _, budget := range []gpu.ManagedGeometryAdmissionBudget{
		{Enabled: true, MaxInputBytes: charge.InputBytes - 1, MaxCopiedStageBytes: charge.TotalStageBytes - 1},
		{Enabled: true},
	} {
		m.SetManagedGeometryAdmissionBudget(budget)
		before := s1l7Stats(t, m, []core.ManagedGeometryInput{a}, 1)
		if m.ServiceManagedGeometry(p.object, 2) != 2 {
			t.Fatal("lowered caps blocked reserved copies")
		}
		if m.ManagedGeometryAdmissionStats() != before {
			t.Fatal("service changed lowered-cap charges")
		}
	}
	s1l8Check(t, s1l8Stage(t, m, p), a, 5)
}

func TestS1l8NoRendererPublication(t *testing.T) {
	m := s1l7Manager()
	p := s1l8Producer(4)
	derivative := p.object.XBrickMap
	before := derivative.Copy()
	rev, id, structureDirty, gpuEdit := derivative.Revision, derivative.ID, derivative.StructureDirty, derivative.GPUEditMode
	dirtySectors, dirtyBricks := maps.Clone(derivative.DirtySectors), maps.Clone(derivative.DirtyBricks)
	ready, ps, pb := m.VoxelObjectReady(p.object, derivative, rev)
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionAccepted)
	a, _ := m.ManagedGeometryInputs(p.object)
	m.ServiceManagedGeometry(p.object, 2)
	old := s1l8Stage(t, m, p)
	p.generation++
	s1l7Admit(t, m, p, gpu.ManagedGeometryAdmissionCoalesced)
	m.ServiceManagedGeometry(p.object, 99)
	m.AdvanceManagedGeometryInput(p.object, a)
	m.ServiceManagedGeometry(p.object, 99)
	m.CancelManagedGeometryInputs(p.object)
	s1l8Check(t, old, a, 2)
	if p.object.XBrickMap != derivative || derivative.Revision != rev || derivative.ID != id || derivative.StructureDirty != structureDirty || derivative.GPUEditMode != gpuEdit || !reflect.DeepEqual(derivative.Sectors, before.Sectors) || !reflect.DeepEqual(derivative.SectorRevisions, before.SectorRevisions) || derivative.CachedMin != before.CachedMin || derivative.CachedMax != before.CachedMax || derivative.AABBDirty != before.AABBDirty || !maps.Equal(derivative.DirtySectors, dirtySectors) || !maps.Equal(derivative.DirtyBricks, dirtyBricks) {
		t.Fatal("CPU staging changed renderer derivative")
	}
	r, s, b := m.VoxelObjectReady(p.object, derivative, rev)
	if r != ready || s != ps || b != pb {
		t.Fatal("CPU staging changed GPU readiness")
	}
	if len(m.Allocations) != 0 || len(m.MaterialAllocations) != 0 || len(m.SectorToInfo) != 0 || len(m.BrickToSlot) != 0 || len(m.PendingUpdates) != 0 || m.SectorTableBuf != nil || m.BrickTableBuf != nil || m.VoxelPayloadPageCount != 0 {
		t.Fatal("CPU staging allocated or queued GPU state")
	}
}
