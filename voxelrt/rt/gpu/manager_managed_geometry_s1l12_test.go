package gpu_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Both callbacks capture real sealed geometry; tokens and trusted callback
// side effects are controlled independently of immutable backing storage.
type s1l12Producer struct {
	*s1l7Producer
	reads      int
	readCoord  [3]int
	hook       func()
	wrongCoord bool
}

func s1l12NewProducer(n int) *s1l12Producer {
	p := &s1l12Producer{s1l7Producer: s1l7NewProducer(n)}
	p.installDual()
	return p
}
func (p *s1l12Producer) installDual() {
	p.object.SetManagedGeometryProducerWithSectorReader(p.object.XBrickMap,
		func() (volume.ManagedGeometryView, uint64, bool) {
			p.calls++
			v, ok := p.owner.CaptureGeometry()
			return v, p.generation, ok && p.available
		}, func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
			p.reads++
			p.readCoord = c
			if p.hook != nil {
				p.hook()
			}
			if p.wrongCoord {
				c[0]++
			}
			v, ok := p.owner.CaptureSector(c)
			return v, p.generation, ok && p.available
		})
}
func s1l12Fixture(t *testing.T, n int) (*gpu.GpuBufferManager, *s1l12Producer, core.ManagedGeometryInput) {
	t.Helper()
	m, p := s1l7Manager(), s1l12NewProducer(n)
	p.generation = 4
	s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	return m, p, in
}
func s1l12Reserve(t *testing.T, m *gpu.GpuBufferManager, p *s1l12Producer, in core.ManagedGeometryInput, c [3]int, want gpu.ManagedGeometrySectorReservationResult) {
	t.Helper()
	if got := m.ReserveManagedGeometrySector(p.object, in, c); got != want {
		t.Fatalf("reserve %v = %v, want %v", c, got, want)
	}
}
func s1l12Absent(t *testing.T, m *gpu.GpuBufferManager, object *core.VoxelObject) {
	t.Helper()
	in, ok := m.ManagedGeometrySectorReservation(object)
	if ok || in != (core.ManagedGeometrySectorInput{}) {
		t.Fatal("absent reservation did not return zero/false")
	}
}
func s1l12Held(t *testing.T, m *gpu.GpuBufferManager, p *s1l12Producer, expected core.ManagedGeometryInput, c [3]int, generation uint64) core.ManagedGeometrySectorInput {
	t.Helper()
	in, ok := m.ManagedGeometrySectorReservation(p.object)
	if !ok || !in.SameSource(expected) || in.Generation() != generation || in.Sector().Coord() != c {
		t.Fatalf("held input identity/coordinate = %+v/%v", in, ok)
	}
	return in
}
func s1l12Charges(t *testing.T, before, after gpu.ManagedGeometryAdmissionStats, in core.ManagedGeometrySectorInput) uint64 {
	t.Helper()
	if after.InputBytes != before.InputBytes+in.Sector().RetainedBytes() || after.ReservedCopiedStageBytes != before.ReservedCopiedStageBytes+in.Sector().CopyBytes() || after.OwnerCount != before.OwnerCount || after.GenerationCount != before.GenerationCount {
		t.Fatalf("reservation charges before %+v, after %+v, sector %d/%d", before, after, in.Sector().RetainedBytes(), in.Sector().CopyBytes())
	}
	// Canonical reservation accounting specifies this language-level value
	// and its cached charges; this does not inspect manager internals.
	type descriptor struct {
		input                core.ManagedGeometrySectorInput
		inputBytes, reserved uint64
	}
	metadata := uint64(unsafe.Sizeof(descriptor{}))
	if after.OwnedMetadataBytes != before.OwnedMetadataBytes+metadata || after.TotalStageBytes != after.OwnedMetadataBytes+after.ReservedCopiedStageBytes {
		t.Fatal("reservation descriptor metadata charge differs from canonical shape")
	}
	return after.OwnedMetadataBytes - before.OwnedMetadataBytes
}

func TestS1l12EarlyRefusalsDoNotCapture(t *testing.T) {
	p := s1l12NewProducer(1)
	var nilManager *gpu.GpuBufferManager
	for _, m := range []*gpu.GpuBufferManager{nilManager, {}} {
		for _, o := range []*core.VoxelObject{nil, p.object} {
			if m.ReserveManagedGeometrySector(o, core.ManagedGeometryInput{}, [3]int{}) != gpu.ManagedGeometrySectorReservationDisabled {
				t.Fatal("disabled result")
			}
			s1l12Absent(t, m, o)
			if m.ReleaseManagedGeometrySectorReservation(o, core.ManagedGeometryInput{}) {
				t.Fatal("absent release")
			}
		}
	}
	m := s1l7Manager()
	for _, o := range []*core.VoxelObject{nil, p.object, core.NewVoxelObject()} {
		if m.ReserveManagedGeometrySector(o, core.ManagedGeometryInput{}, [3]int{}) != gpu.ManagedGeometrySectorReservationUnavailable {
			t.Fatal("missing owner result")
		}
	}
	if p.calls != 0 || p.reads != 0 {
		t.Fatal("early refusal captured")
	}
	m, p, in := s1l12Fixture(t, 2)
	other := s1l12NewProducer(1)
	foreign, _ := other.object.CaptureManagedGeometryInput()
	p.generation++
	s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
	_, next := m.ManagedGeometryInputs(p.object)
	calls := p.calls
	before := m.ManagedGeometryAdmissionStats()
	for _, token := range []core.ManagedGeometryInput{{}, foreign, next} {
		s1l12Reserve(t, m, p, token, [3]int{}, gpu.ManagedGeometrySectorReservationMismatch)
	}
	p.owner.SetVoxel(64, 0, 0, 9)
	for _, c := range [][3]int{{2, 0, 0}, {-1, 0, 0}, {0, 1, 0}} {
		s1l12Reserve(t, m, p, in, c, gpu.ManagedGeometrySectorReservationIgnored)
	}
	if p.reads != 0 || p.calls != calls || m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("filter captured or mutated ledger")
	}
	m, p, in = s1l12Fixture(t, 0)
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationIgnored)
	if p.reads != 0 {
		t.Fatal("empty topology invoked reader")
	}
}

func TestS1l12CurrentContentFrozenHistoryAndNoWorkProgress(t *testing.T) {
	m, p, in := s1l12Fixture(t, 2)
	c := [3]int{}
	s1l9Queue(t, m, p.s1l7Producer, in, c, gpu.ManagedGeometryContentQueued)
	if !m.RequestManagedGeometryContentSweep(p.object, in) {
		t.Fatal("sweep setup")
	}
	if m.ServiceManagedGeometryContent(p.object, in, 1, func([3]int) bool { return true }) != 1 || m.ServiceManagedGeometry(p.object, 1) != 1 {
		t.Fatal("progress setup")
	}
	stage, _ := m.ManagedGeometryStage(p.object)
	status, _ := m.ManagedGeometryContentStatus(p.object)
	stageCopy, _ := stage.CopySector(0)
	oldCopy, _ := in.Geometry().CopySector(0)
	p.owner.SetVoxel(0, 0, 0, 7)
	p.generation = 9
	before := m.ManagedGeometryAdmissionStats()
	calls := p.calls
	s1l12Reserve(t, m, p, in, c, gpu.ManagedGeometrySectorReservationReserved)
	held := s1l12Held(t, m, p, in, c, 9)
	s1l12Charges(t, before, m.ManagedGeometryAdmissionStats(), held)
	current, _ := held.Sector().CopySector()
	want, _ := p.owner.CaptureSector(c)
	expected, _ := want.CopySector()
	if !reflect.DeepEqual(current, expected) || reflect.DeepEqual(current, oldCopy) {
		t.Fatal("reservation did not retain current sector")
	}
	nowStage, _ := m.ManagedGeometryStage(p.object)
	nowStatus, _ := m.ManagedGeometryContentStatus(p.object)
	if nowStage != stage || nowStatus != status || p.calls != calls || p.reads != 1 || p.readCoord != c {
		t.Fatal("reservation changed progress or used full capture")
	}
	if len(m.Allocations) != 0 || len(m.PendingUpdates) != 0 || len(m.SectorToInfo) != 0 || len(m.BrickToSlot) != 0 {
		t.Fatal("reservation published GPU work")
	}
	// Boundary writes change the neighbor's normal halo; retained target, halo
	// sector and copied-prefix snapshots must each preserve their own history.
	p.owner.SetVoxel(31, 0, 0, 8)
	p.owner.SetVoxel(32, 0, 0, 8)
	p.generation++
	s1l12Reserve(t, m, p, in, [3]int{1, 0, 0}, gpu.ManagedGeometrySectorReservationReserved)
	halo := s1l12Held(t, m, p, in, [3]int{1, 0, 0}, 10)
	haloCopy, _ := halo.Sector().CopySector()
	p.owner.SetVoxel(31, 0, 0, 0)
	p.owner.SetVoxel(32, 0, 0, 0)
	p.object.SetManagedGeometryProducer(p.object.XBrickMap, nil)
	if !m.ReleaseManagedGeometrySectorReservation(p.object, in) {
		t.Fatal("release after producer clear")
	}
	for _, tc := range []struct {
		held core.ManagedGeometrySectorInput
		want *volume.Sector
	}{{held, current}, {halo, haloCopy}} {
		got, _ := tc.held.Sector().CopySector()
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatal("held history changed after writes and release")
		}
	}
	again, _ := stage.CopySector(0)
	accepted, _ := in.Geometry().CopySector(0)
	if !reflect.DeepEqual(again, stageCopy) || !reflect.DeepEqual(accepted, oldCopy) {
		t.Fatal("accepted/stage history changed")
	}
	if m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("release did not restore original charges")
	}
}

func TestS1l12CaptureFailuresAndPendingRollbackPreserveReservation(t *testing.T) {
	m, p, in := s1l12Fixture(t, 2)
	p.generation = 10
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
	held, _ := m.ManagedGeometrySectorReservation(p.object)
	before := m.ManagedGeometryAdmissionStats()
	for _, tc := range []struct {
		name  string
		setup func()
		want  gpu.ManagedGeometrySectorReservationResult
	}{
		{"unavailable", func() { p.available = false }, gpu.ManagedGeometrySectorReservationUnavailable},
		{"wrong-coordinate", func() { p.available = true; p.wrongCoord = true }, gpu.ManagedGeometrySectorReservationUnavailable},
		{"accepted-rollback", func() { p.wrongCoord = false; p.generation = 3 }, gpu.ManagedGeometrySectorReservationUnavailable},
		{"pending-rollback-other-coordinate", func() { p.generation = 9 }, gpu.ManagedGeometrySectorReservationStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup()
			calls := p.calls
			s1l12Reserve(t, m, p, in, [3]int{1, 0, 0}, tc.want)
			got, ok := m.ManagedGeometrySectorReservation(p.object)
			if !ok || got != held || m.ManagedGeometryAdmissionStats() != before || p.calls != calls {
				t.Fatal("refusal changed retained reservation")
			}
		})
	}
}

func TestS1l12ReplacementPreflightsGlobalPeakEvenSamePublication(t *testing.T) {
	for _, domain := range []string{"input", "stage"} {
		t.Run(domain, func(t *testing.T) {
			m, p, in := s1l12Fixture(t, 2)
			q := s1l12NewProducer(0)
			q.owner = p.owner
			q.object.XBrickMap = p.object.XBrickMap
			q.installDual()
			s1l7Admit(t, m, q.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
			p.generation++
			s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
			baseline := m.ManagedGeometryAdmissionStats()
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
			held := s1l12Held(t, m, p, in, [3]int{}, 5)
			before := m.ManagedGeometryAdmissionStats()
			metadata := s1l12Charges(t, baseline, before, held)
			candidate, _ := p.owner.CaptureSector([3]int{1, 0, 0})
			budget := gpu.DefaultManagedGeometryAdmissionBudget()
			if domain == "input" {
				budget.MaxInputBytes = before.InputBytes + candidate.RetainedBytes() - 1
			} else {
				budget.MaxCopiedStageBytes = before.TotalStageBytes + candidate.CopyBytes() + metadata - 1
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			s1l12Reserve(t, m, p, in, [3]int{1, 0, 0}, gpu.ManagedGeometrySectorReservationPressure)
			retained, _ := m.ManagedGeometrySectorReservation(p.object)
			if retained != held || m.ManagedGeometryAdmissionStats() != before {
				t.Fatal("peak refusal dropped old candidate or other ownership")
			}
			if domain == "input" {
				budget.MaxInputBytes++
			} else {
				budget.MaxCopiedStageBytes++
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			s1l12Reserve(t, m, p, in, [3]int{1, 0, 0}, gpu.ManagedGeometrySectorReservationReserved)
			replacement := s1l12Held(t, m, p, in, [3]int{1, 0, 0}, 5)
			after := m.ManagedGeometryAdmissionStats()
			if after.InputBytes != baseline.InputBytes+candidate.RetainedBytes() || after.ReservedCopiedStageBytes != baseline.ReservedCopiedStageBytes+candidate.CopyBytes() || after.OwnedMetadataBytes != baseline.OwnedMetadataBytes+metadata {
				t.Fatal("successful replacement retained old charges")
			}
			// Repeating the identical publication still needs old + incoming capacity.
			if domain == "input" {
				budget.MaxInputBytes = after.InputBytes + candidate.RetainedBytes() - 1
			} else {
				budget.MaxCopiedStageBytes = after.TotalStageBytes + candidate.CopyBytes() + metadata - 1
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			s1l12Reserve(t, m, p, in, [3]int{1, 0, 0}, gpu.ManagedGeometrySectorReservationPressure)
			got, _ := m.ManagedGeometrySectorReservation(p.object)
			if got != replacement || m.ManagedGeometryAdmissionStats() != after {
				t.Fatal("same-publication refusal changed reservation")
			}
		})
	}
}

func TestS1l12RemovalStillReservesMetadataAndRequiresPeak(t *testing.T) {
	m, p, in := s1l12Fixture(t, 1)
	baseline := m.ManagedGeometryAdmissionStats()
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
	held, _ := m.ManagedGeometrySectorReservation(p.object)
	before := m.ManagedGeometryAdmissionStats()
	metadata := s1l12Charges(t, baseline, before, held)
	p.owner.SetVoxel(0, 0, 0, 0)
	p.generation++
	budget := gpu.DefaultManagedGeometryAdmissionBudget()
	budget.MaxCopiedStageBytes = before.TotalStageBytes + metadata - 1
	m.SetManagedGeometryAdmissionBudget(budget)
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationPressure)
	if got, _ := m.ManagedGeometrySectorReservation(p.object); got != held || m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("shrink refusal changed ownership")
	}
	budget.MaxCopiedStageBytes++
	m.SetManagedGeometryAdmissionBudget(budget)
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
	tombstone := s1l12Held(t, m, p, in, [3]int{}, 5)
	if tombstone.Sector().Present() || tombstone.Sector().RetainedBytes() != 0 || tombstone.Sector().CopyBytes() != 0 {
		t.Fatal("removal is not qualified zero-payload reservation")
	}
	after := m.ManagedGeometryAdmissionStats()
	s1l12Charges(t, baseline, after, tombstone)
	if !m.ReleaseManagedGeometrySectorReservation(p.object, in) || m.ManagedGeometryAdmissionStats() != baseline {
		t.Fatal("tombstone release retained charges")
	}
	s1l12Absent(t, m, p.object)
}

func TestS1l12CallbackStateRechecked(t *testing.T) {
	for _, kind := range []string{"lower-budget", "disable", "cancel", "cancel-readmit-same-token", "promote", "coalesce-successor", "panic"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l12Fixture(t, 2)
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
			held, _ := m.ManagedGeometrySectorReservation(p.object)
			original := m.ManagedGeometryAdmissionStats()
			if kind == "promote" || kind == "coalesce-successor" {
				p.generation++
				s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
			}
			sentinel := &struct{ label string }{"reader panic"}
			want := gpu.ManagedGeometrySectorReservationMismatch
			var callbackStats gpu.ManagedGeometryAdmissionStats
			p.hook = func() {
				switch kind {
				case "lower-budget":
					m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
					want = gpu.ManagedGeometrySectorReservationPressure
				case "disable":
					m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
					want = gpu.ManagedGeometrySectorReservationDisabled
				case "cancel":
					m.CancelManagedGeometryInputs(p.object)
				case "cancel-readmit-same-token":
					m.CancelManagedGeometryInputs(p.object)
					s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
				case "promote":
					if !m.AdvanceManagedGeometryInput(p.object, in) {
						t.Fatal("promotion setup")
					}
				case "coalesce-successor":
					p.generation++
					s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
					want = gpu.ManagedGeometrySectorReservationReserved
				case "panic":
					panic(sentinel)
				}
				callbackStats = m.ManagedGeometryAdmissionStats()
			}
			if kind == "panic" {
				func() {
					defer func() {
						if recover() != sentinel {
							t.Fatal("reader panic changed or swallowed")
						}
					}()
					m.ReserveManagedGeometrySector(p.object, in, [3]int{1, 0, 0})
				}()
				if got, _ := m.ManagedGeometrySectorReservation(p.object); got != held || m.ManagedGeometryAdmissionStats() != original {
					t.Fatal("panic mutated reservation")
				}
				return
			}
			// The callback chooses the outcome before the result is checked.
			result := m.ReserveManagedGeometrySector(p.object, in, [3]int{1, 0, 0})
			if result != want {
				t.Fatalf("callback %s result %v, want %v", kind, result, want)
			}
			if kind == "coalesce-successor" {
				fresh := s1l12Held(t, m, p, in, [3]int{1, 0, 0}, 6)
				after := m.ManagedGeometryAdmissionStats()
				if after.InputBytes != callbackStats.InputBytes-held.Sector().RetainedBytes()+fresh.Sector().RetainedBytes() || after.ReservedCopiedStageBytes != callbackStats.ReservedCopiedStageBytes-held.Sector().CopyBytes()+fresh.Sector().CopyBytes() || after.OwnedMetadataBytes != callbackStats.OwnedMetadataBytes {
					t.Fatal("callback successor charges lost")
				}
			} else {
				if m.ManagedGeometryAdmissionStats() != callbackStats {
					t.Fatal("refusal changed callback-owned effects")
				}
				if kind == "lower-budget" {
					if got, _ := m.ManagedGeometrySectorReservation(p.object); got != held {
						t.Fatal("pressure dropped candidate")
					}
				} else {
					s1l12Absent(t, m, p.object)
				}
			}
		})
	}
}

func TestS1l12ReleaseIdentityPressureAndLifecycle(t *testing.T) {
	for _, kind := range []string{"explicit", "cancel", "advance", "disable"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l12Fixture(t, 1)
			baseline := m.ManagedGeometryAdmissionStats()
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
			held, _ := m.ManagedGeometrySectorReservation(p.object)
			frozen, _ := held.Sector().CopySector()
			foreign := s1l12NewProducer(1)
			wrong, _ := foreign.object.CaptureManagedGeometryInput()
			before := m.ManagedGeometryAdmissionStats()
			for _, token := range []core.ManagedGeometryInput{{}, wrong} {
				if m.ReleaseManagedGeometrySectorReservation(p.object, token) || m.ManagedGeometryAdmissionStats() != before {
					t.Fatal("mismatched release")
				}
			}
			p.generation++
			s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
			_, next := m.ManagedGeometryInputs(p.object)
			if m.ReleaseManagedGeometrySectorReservation(p.object, next) {
				t.Fatal("successor released accepted candidate")
			}
			if got, _ := m.ManagedGeometrySectorReservation(p.object); got != held {
				t.Fatal("successor dropped candidate")
			}
			// Getter/release must remain lazy even when current selection is invalid.
			calls, reads := p.calls, p.reads
			p.owner.ExposeMutable()
			p.object.XBrickMap.GPUEditMode = true
			m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{Enabled: true})
			s1l12Held(t, m, p, in, [3]int{}, 4)
			switch kind {
			case "explicit":
				if !m.ReleaseManagedGeometrySectorReservation(p.object, in) || m.ReleaseManagedGeometrySectorReservation(p.object, in) {
					t.Fatal("release presence semantics")
				}
				if m.ManagedGeometryAdmissionStats().OwnedMetadataBytes == 0 {
					t.Fatal("release dropped admitted descriptors")
				}
				m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
				s1l7Stats(t, m, []core.ManagedGeometryInput{in, next}, 1)
			case "cancel":
				if !m.CancelManagedGeometryInputs(p.object) {
					t.Fatal("cancel failed")
				}
				s1l7Stats(t, m, nil, 0)
			case "advance":
				if !m.AdvanceManagedGeometryInput(p.object, in) {
					t.Fatal("advance failed")
				}
				m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
				s1l7Stats(t, m, []core.ManagedGeometryInput{next}, 1)
			case "disable":
				m.SetManagedGeometryAdmissionBudget(gpu.ManagedGeometryAdmissionBudget{})
				s1l7Stats(t, m, nil, 0)
			}
			s1l12Absent(t, m, p.object)
			if p.calls != calls || p.reads != reads {
				t.Fatal("getter/release/lifecycle captured")
			}
			copied, _ := held.Sector().CopySector()
			if !reflect.DeepEqual(copied, frozen) || !held.SameSource(in) || baseline.OwnerCount != 1 {
				t.Fatal("caller-held history invalidated")
			}
		})
	}
}

func TestS1l12FrozenMetadataAndAuxCapacityCharges(t *testing.T) {
	raw := volume.NewXBrickMap()
	raw.SetVoxel(0, 0, 0, 1)
	sector := raw.Sectors[[3]int{}]
	brick := sector.PackedBricks[0]
	brick.Flags, brick.AtlasOffset, brick.OccupancyMask64 = 13, 29, 7
	brick.PrecomputedAux = make([]byte, 7, 4096)
	brick.PrecomputedAux[0] = 23
	sector.Coords = [3]int{99, -2, 7}
	p := s1l12NewProducer(0)
	p.owner = volume.NewManagedXBrickMap(raw)
	p.object.XBrickMap = raw.Copy()
	p.installDual()
	m := s1l7Manager()
	s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
	in, _ := m.ManagedGeometryInputs(p.object)
	before := m.ManagedGeometryAdmissionStats()
	wantView, ok := p.owner.CaptureSector([3]int{})
	if !ok {
		t.Fatal("qualified fixture")
	}
	want, _ := wantView.CopySector()
	// Sealing defensively copies the source; charge the observed sealed
	// backing capacity rather than assuming the raw constructor slack survives.
	wantAux := want.PackedBricks[0].PrecomputedAux
	if cap(wantAux) <= len(wantAux) {
		t.Fatal("sealed fixture needs observable auxiliary backing slack")
	}
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
	held := s1l12Held(t, m, p, in, [3]int{}, 0)
	s1l12Charges(t, before, m.ManagedGeometryAdmissionStats(), held)
	got, _ := held.Sector().CopySector()
	if !reflect.DeepEqual(got, want) || cap(got.PackedBricks[0].PrecomputedAux) != cap(wantAux) || held.Sector().RetainedBytes() != wantView.RetainedBytes() || held.Sector().CopyBytes() != wantView.CopyBytes() {
		t.Fatal("reservation changed frozen scalar metadata or aux capacity charges")
	}
	got.Coords = [3]int{}
	got.PackedBricks[0].PrecomputedAux[0] = 99
	again, _ := held.Sector().CopySector()
	if !reflect.DeepEqual(again, want) {
		t.Fatal("caller copy changed reservation")
	}
}

func TestS1l12FirstReservationExactCapsAndPausedReads(t *testing.T) {
	probe, p, in := s1l12Fixture(t, 1)
	baseline := probe.ManagedGeometryAdmissionStats()
	s1l12Reserve(t, probe, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
	held, _ := probe.ManagedGeometrySectorReservation(p.object)
	charge := probe.ManagedGeometryAdmissionStats()
	s1l12Charges(t, baseline, charge, held)
	for _, domain := range []string{"input", "stage"} {
		t.Run(domain, func(t *testing.T) {
			m, p, in := s1l12Fixture(t, 1)
			budget := gpu.DefaultManagedGeometryAdmissionBudget()
			budget.MaxInputBytes, budget.MaxCopiedStageBytes = charge.InputBytes, charge.TotalStageBytes
			if domain == "input" {
				budget.MaxInputBytes--
			} else {
				budget.MaxCopiedStageBytes--
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			before := m.ManagedGeometryAdmissionStats()
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationPressure)
			s1l12Absent(t, m, p.object)
			if m.ManagedGeometryAdmissionStats() != before {
				t.Fatal("first refusal changed base reservation")
			}
			if domain == "input" {
				budget.MaxInputBytes++
			} else {
				budget.MaxCopiedStageBytes++
			}
			m.SetManagedGeometryAdmissionBudget(budget)
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
			if m.ManagedGeometryAdmissionStats() != charge {
				t.Fatal("exact cap did not fit")
			}
			prior, _ := m.ManagedGeometrySectorReservation(p.object)
			for _, caps := range []gpu.ManagedGeometryAdmissionBudget{{Enabled: true}, {Enabled: true, MaxInputBytes: charge.InputBytes - 1, MaxCopiedStageBytes: charge.TotalStageBytes - 1}} {
				m.SetManagedGeometryAdmissionBudget(caps)
				before = m.ManagedGeometryAdmissionStats()
				s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationPressure)
				if got, _ := m.ManagedGeometrySectorReservation(p.object); got != prior || m.ManagedGeometryAdmissionStats() != before {
					t.Fatal("enabled pause dropped held ownership")
				}
			}
			if !m.ReleaseManagedGeometrySectorReservation(p.object, in) {
				t.Fatal("paused release failed")
			}
			m.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
			if m.ManagedGeometryAdmissionStats() != baseline {
				t.Fatal("paused release did not restore base full reservations")
			}
		})
	}
}

func TestS1l12CallbackSuccessorGrowthUsesCurrentLedgerAndBudget(t *testing.T) {
	m, p, in := s1l12Fixture(t, 2)
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
	held, _ := m.ManagedGeometrySectorReservation(p.object)
	var afterCallback gpu.ManagedGeometryAdmissionStats
	p.hook = func() {
		p.generation++
		s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionCoalesced)
		// A cap sufficient before successor growth must now refuse even though
		// net candidate replacement would leave payload sizes unchanged.
		stats := m.ManagedGeometryAdmissionStats()
		b := gpu.DefaultManagedGeometryAdmissionBudget()
		b.MaxInputBytes = stats.InputBytes + held.Sector().RetainedBytes() - 1
		m.SetManagedGeometryAdmissionBudget(b)
		afterCallback = m.ManagedGeometryAdmissionStats()
	}
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationPressure)
	if got, _ := m.ManagedGeometrySectorReservation(p.object); got != held || m.ManagedGeometryAdmissionStats() != afterCallback {
		t.Fatal("post-callback preflight lost successor or pending charges")
	}
	_, next := m.ManagedGeometryInputs(p.object)
	if next.Generation() != 5 {
		t.Fatal("trusted callback successor was not retained")
	}
}

func TestS1l12LegacyProducerNeverFallsBackToFullCapture(t *testing.T) {
	m, p, in := s1l12Fixture(t, 1)
	p.s1l7Producer.install()
	m.CancelManagedGeometryInputs(p.object)
	s1l7Admit(t, m, p.s1l7Producer, gpu.ManagedGeometryAdmissionAccepted)
	in, _ = m.ManagedGeometryInputs(p.object)
	calls := p.calls
	before := m.ManagedGeometryAdmissionStats()
	s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationUnavailable)
	if p.calls != calls || p.reads != 0 || m.ManagedGeometryAdmissionStats() != before {
		t.Fatal("missing sector reader fell back or changed ledger")
	}
}

func TestS1l12PressureRefusalAllocatesNothingBeforePreflight(t *testing.T) {
	for _, domain := range []string{"input", "stage"} {
		t.Run(domain, func(t *testing.T) {
			m, p, in := s1l12Fixture(t, 2)
			baseline := m.ManagedGeometryAdmissionStats()
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
			held, _ := m.ManagedGeometrySectorReservation(p.object)
			before := m.ManagedGeometryAdmissionStats()
			metadata := s1l12Charges(t, baseline, before, held)
			candidate, _ := p.owner.CaptureSector([3]int{1, 0, 0})
			b := gpu.DefaultManagedGeometryAdmissionBudget()
			if domain == "input" {
				b.MaxInputBytes = before.InputBytes + candidate.RetainedBytes() - 1
			} else {
				b.MaxCopiedStageBytes = before.TotalStageBytes + candidate.CopyBytes() + metadata - 1
			}
			m.SetManagedGeometryAdmissionBudget(b)
			calls := p.calls
			var result gpu.ManagedGeometrySectorReservationResult
			allocations := testing.AllocsPerRun(20, func() {
				result = m.ReserveManagedGeometrySector(p.object, in, [3]int{1, 0, 0})
			})
			if result != gpu.ManagedGeometrySectorReservationPressure || allocations != 0 {
				t.Fatalf("warmed pressure refusal result %v, allocations %g; want Pressure, zero", result, allocations)
			}
			if got, ok := m.ManagedGeometrySectorReservation(p.object); !ok || got != held || m.ManagedGeometryAdmissionStats() != before || p.calls != calls {
				t.Fatal("pressure probe changed pending/stats or invoked full capture")
			}
		})
	}
}

func TestS1l12CallbackProducerOrSelectionChangesPreserveHeldReservation(t *testing.T) {
	for _, kind := range []string{"clear-producer", "reinstall-producer", "gpu-selection", "derivative-selection"} {
		t.Run(kind, func(t *testing.T) {
			m, p, in := s1l12Fixture(t, 2)
			s1l12Reserve(t, m, p, in, [3]int{}, gpu.ManagedGeometrySectorReservationReserved)
			held, _ := m.ManagedGeometrySectorReservation(p.object)
			before := m.ManagedGeometryAdmissionStats()
			calls, reads := p.calls, p.reads
			p.hook = func() {
				switch kind {
				case "clear-producer":
					p.object.SetManagedGeometryProducer(p.object.XBrickMap, nil)
				case "reinstall-producer":
					p.installDual()
				case "gpu-selection":
					p.object.XBrickMap.GPUEditMode = true
				case "derivative-selection":
					p.object.XBrickMap = p.object.XBrickMap.Copy()
				}
			}
			s1l12Reserve(t, m, p, in, [3]int{1, 0, 0}, gpu.ManagedGeometrySectorReservationUnavailable)
			if got, ok := m.ManagedGeometrySectorReservation(p.object); !ok || got != held || m.ManagedGeometryAdmissionStats() != before || p.calls != calls || p.reads != reads+1 {
				t.Fatal("capture qualification failure changed held reservation or ledger")
			}
			accepted, _ := m.ManagedGeometryInputs(p.object)
			s1l7Same(t, accepted, in)
		})
	}
}
