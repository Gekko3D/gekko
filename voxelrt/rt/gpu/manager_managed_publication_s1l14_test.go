package gpu

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Run the real admission, upload, snapshot, lookup and retirement code. The
// existing native fixture substitutes only physical buffers and queue writes.
type s1l14Producer struct {
	o                    *core.VoxelObject
	owner                *volume.ManagedXBrickMap
	g                    uint64
	full, sector, scalar int
	qualified            bool
}

func TestS1l14CancelCPUEnumerationBeforeGPUStageRetainsAndRecoversOwner(t *testing.T) {
	m, b, s, p := s1l14FiniteFixture(t, 16)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Prepare(t, m, s)
	stage, ok := m.ManagedGeometryStage(p.o)
	if !ok || stage.Len() != 1 || s1l14Status(t, m, p).StageInput.SameSource(s1l14Status(t, m, p).StageInput) {
		t.Fatal("fixture did not stop during CPU enumeration before GPU stage")
	}
	m.CancelManagedGeometryInputs(p.o)
	charges := m.ManagedGeometryAdmissionStats()
	if charges.InputBytes == 0 || charges.TotalStageBytes == 0 || p.o.RenderVoxelMap() != nil {
		t.Fatal("CPU cancellation erased captured GPU owner or exposed initial coverage")
	}
	s1l14Publish(t, m, b, s, p)
	full := p.full
	for i := 0; i < 8; i++ {
		s1l14Frame(t, m, b, s)
	}
	if p.full != full {
		t.Fatal("recovered idle publication recaptured full geometry")
	}
	s.Objects = nil
	for i := 0; i < 256; i++ {
		s1l14Frame(t, m, b, s)
		if m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
			return
		}
	}
	t.Fatal("recovered cancelled enumeration retained charges after removal")
}

func TestS1l14CurrentCoordinateMetadataRefusalPreservesLedgerAndRetries(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	s1l14Publish(t, m, b, s, p)
	m.CancelManagedGeometryInputs(p.o)
	before := m.ManagedGeometryAdmissionStats()
	selected := p.o.RenderVoxelMap()
	committed := selected.Sectors[[3]int{}]
	var writes []volume.VoxelWrite
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			writes = append(writes, volume.VoxelWrite{X: x, Y: y, Z: 16, Value: 2})
		}
	}
	s1l14Edit(m, p, writes...)
	incoming, ok := p.owner.CaptureSector([3]int{})
	if !ok || incoming.CopyBytes() <= uint64(unsafe.Sizeof(volume.Sector{})) {
		t.Fatal("larger incoming payload fixture")
	}
	headers, ok := managedTargetMetadata(0)
	if !ok {
		t.Fatal("target header charge overflow")
	}
	coordinateMetadata := 2*uint64(unsafe.Sizeof(managedCoordinateRecord{})) + uint64(unsafe.Sizeof([64]*volume.Brick{}))
	payload := 2 * incoming.CopyBytes()
	// Fit the hidden candidate headers and its independent payload domain while
	// refusing the simultaneous coordinate descriptor and allocation pointers.
	cap := before.TotalStageBytes + 2*headers + payload
	if coordinateMetadata == 0 || cap <= before.TotalStageBytes+2*headers {
		t.Fatal("refusal boundary is not representable")
	}
	m.SetManagedGeometryAdmissionBudget(ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: before.InputBytes + incoming.RetainedBytes(), MaxCopiedStageBytes: cap})
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Frame(t, m, b, s)
	stable := m.ManagedGeometryAdmissionStats()
	if stable.InputBytes != before.InputBytes || stable.ReservedCopiedStageBytes != before.ReservedCopiedStageBytes || stable.OwnedMetadataBytes != before.OwnedMetadataBytes+2*headers || stable.TotalStageBytes != before.TotalStageBytes+2*headers {
		t.Fatalf("refused coordinate retained payload beyond candidate headers: before %+v after %+v", before, stable)
	}
	owner := m.managedGPUOwners[p.o]
	if owner == nil || owner.candidate == nil || !s1l14Status(t, m, p).Pending {
		t.Fatal("fixture did not retain a refused current candidate")
	}
	cursor, notifications := owner.cursor, owner.count
	for i := 0; i < 12; i++ {
		s1l14Frame(t, m, b, s)
		if got := m.ManagedGeometryAdmissionStats(); got != stable {
			t.Fatalf("repeated refused coordinate accumulated charges: before %+v after %+v", stable, got)
		}
		if owner.cursor != cursor || owner.count != notifications || !s1l14Status(t, m, p).Pending {
			t.Fatal("refusal consumed repair cursor or notifications")
		}
		if p.o.RenderVoxelMap() != selected || selected.Sectors[[3]int{}] != committed || m.Allocations[selected].Sectors[[3]int{}] != committed {
			t.Fatal("refusal replaced committed selected snapshot")
		}
		s1l14Value(t, p.o, 16, 16, 16, 1)
	}
	m.SetManagedGeometryAdmissionBudget(DefaultManagedGeometryAdmissionBudget())
	s1l14Publish(t, m, b, s, p)
	s1l14Value(t, p.o, 0, 0, 16, 2)
	s.Objects = nil
	for i := 0; i < 128; i++ {
		s1l14Frame(t, m, b, s)
		if m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
			return
		}
	}
	t.Fatal("retried coordinate retained charges after removal")
}

func s1l14ProducerFor(s *core.Scene, coords ...[3]int) *s1l14Producer {
	raw := volume.NewXBrickMap()
	for _, c := range coords {
		raw.SetVoxel(c[0]*32+16, c[1]*32+16, c[2]*32+16, 1)
	}
	p := &s1l14Producer{o: core.NewVoxelObject(), owner: volume.NewManagedXBrickMap(raw), g: 7, qualified: true}
	p.o.XBrickMap = raw.Copy()
	p.install()
	s.AddObject(p.o)
	return p
}

func (p *s1l14Producer) install() {
	p.o.SetManagedGeometryProducerWithGenerationReader(p.o.XBrickMap,
		func() (volume.ManagedGeometryView, uint64, bool) {
			p.full++
			v, ok := p.owner.CaptureGeometry()
			return v, p.g, ok && p.qualified
		},
		func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
			p.sector++
			v, ok := p.owner.CaptureSector(c)
			return v, p.g, ok && p.qualified
		},
		func() (uint64, bool) {
			p.scalar++
			_, _, sealed := p.owner.CurrentGeometryCounts()
			return p.g, p.qualified && sealed
		})
}

func s1l14Fixture(t *testing.T, coords ...[3]int) (*GpuBufferManager, *p2cNative, *core.Scene, *s1l14Producer) {
	t.Helper()
	m, b, s := p2cFixture(t)
	m.SetManagedGeometryAdmissionBudget(DefaultManagedGeometryAdmissionBudget())
	m.SetManagedGeometryFrameBudget(DefaultManagedGeometryFrameBudget())
	return m, b, s, s1l14ProducerFor(s, coords...)
}

func s1l14Prepare(t *testing.T, m *GpuBufferManager, s *core.Scene) {
	t.Helper()
	m.PrepareManagedGeometryFrame(s)
	b := m.ManagedGeometryFrameBudget()
	stats := m.ManagedGeometryFrameStats()
	if b.Enabled && stats.AttemptedEntries > b.MaxEntries {
		t.Fatalf("shared frame cap exceeded: %+v / %+v", stats, b)
	}
}

func s1l14Frame(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene) {
	t.Helper()
	s1l14Prepare(t, m, s)
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	p2cStep(t, m, b, s)
}

func s1l14Status(t *testing.T, m *GpuBufferManager, p *s1l14Producer) ManagedGeometryGPUStatus {
	t.Helper()
	before := [3]int{p.full, p.sector, p.scalar}
	status, ok := m.ManagedGeometryGPUStatus(p.o)
	if !ok {
		t.Fatal("managed GPU ownership absent")
	}
	if before != [3]int{p.full, p.sector, p.scalar} {
		t.Fatal("status invoked producer")
	}
	return status
}

func s1l14Publish(t *testing.T, m *GpuBufferManager, b *p2cNative, s *core.Scene, p *s1l14Producer) {
	t.Helper()
	for i := 0; i < 4096; i++ {
		s1l14Frame(t, m, b, s)
		status, ok := m.ManagedGeometryGPUStatus(p.o)
		if ok && status.CurrentInput.SameSource(status.CurrentInput) && status.CurrentGeneration == p.g && !status.Pending {
			if p.o.RenderVoxelMap() == nil || p.o.RenderVoxelMap() == p.o.XBrickMap {
				t.Fatal("published geometry not a private selection")
			}
			if ready, _, _ := m.RenderVoxelObjectReady(p.o, p.o.RenderVoxelMap(), p.o.RenderVoxelMap().Revision); !ready {
				t.Fatal("publication lacks GPU/material readiness")
			}
			return
		}
	}
	t.Fatal("fitting managed work did not drain in 4096 frames")
}

func s1l14Edit(m *GpuBufferManager, p *s1l14Producer, writes ...volume.VoxelWrite) {
	previous := p.g
	for _, w := range writes {
		p.owner.SetVoxel(w.X, w.Y, w.Z, w.Value)
		p.o.XBrickMap.SetVoxel(w.X, w.Y, w.Z, w.Value)
	}
	p.g++
	m.NotifyManagedGeometryGPUContent(p.o, previous, p.g, writes)
}

func s1l14Value(t *testing.T, o *core.VoxelObject, x, y, z int, want uint8) {
	t.Helper()
	if o.RenderVoxelMap() == nil {
		t.Fatal("selected coverage missing")
	}
	present, value := o.RenderVoxelMap().GetVoxel(x, y, z)
	if value != want || present != (want != 0) {
		t.Fatalf("selected voxel %v/%d want %d", present, value, want)
	}
}

func TestS1l14DefaultDisabledAndEnabledZeroBudgets(t *testing.T) {
	if got := DefaultManagedGeometryFrameBudget(); got != (ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 16}) {
		t.Fatalf("default %+v", got)
	}
	var nilManager *GpuBufferManager
	nilManager.SetManagedGeometryFrameBudget(DefaultManagedGeometryFrameBudget())
	nilManager.PrepareManagedGeometryFrame(nil)
	if nilManager.ManagedGeometryFrameBudget() != (ManagedGeometryFrameBudget{}) || nilManager.ManagedGeometryFrameStats().AttemptedEntries != 0 {
		t.Fatal("nil manager nonzero policy/work")
	}
	for _, budget := range []ManagedGeometryFrameBudget{{}, {Enabled: true}} {
		m, b, s, p := s1l14Fixture(t, [3]int{})
		m.SetManagedGeometryFrameBudget(budget)
		for i := 0; i < 3; i++ {
			s1l14Prepare(t, m, s)
		}
		if m.ManagedGeometryFrameBudget() != budget || m.ManagedGeometryFrameStats().AttemptedEntries != 0 || p.full+p.sector != 0 {
			t.Fatal("disabled/zero service captured geometry")
		}
		if budget.Enabled {
			if p.o.RenderVoxelMap() != p.o.XBrickMap {
				t.Fatal("zero-entry service changed selection")
			}
		} else {
			p2cStep(t, m, b, s)
			if p.o.RenderVoxelMap() != p.o.XBrickMap {
				t.Fatal("opt-out changed ordinary path")
			}
		}
	}
}

func TestS1l14InitialPrefixHiddenAndOnlyNextPrecommitCanPromote(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{-2, -1, 0}, [3]int{0, 0, 0}, [3]int{1, 0, 0})
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	authority, transform := p.o.XBrickMap, *p.o.Transform
	s1l14Prepare(t, m, s)
	if p.o.RenderVoxelMap() != nil || s1l14Status(t, m, p).CurrentInput.SameSource(s1l14Status(t, m, p).CurrentInput) {
		t.Fatal("initial prefix exposed current coverage")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	for i := 0; i < 32; i++ {
		s1l14Frame(t, m, b, s)
		if p.o.RenderVoxelMap() != nil {
			t.Fatal("CPU completion/dirty emptiness published without GPU coverage")
		}
	}
	if status := s1l14Status(t, m, p); status.StageReady || !status.Pending {
		t.Fatal("zero upload cap certifies ready stage")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 4096, MaxBricks: 1 << 20})
	for i := 0; i < 64; i++ {
		s1l14Frame(t, m, b, s)
		if s1l14Status(t, m, p).StageReady {
			if p.o.RenderVoxelMap() != nil || len(s.VisibleObjects) != 0 {
				t.Fatal("post-commit native update promoted in the same frame")
			}
			s1l14Prepare(t, m, s)
			if p.o.RenderVoxelMap() == nil {
				t.Fatal("next precommit failed to promote completed stage")
			}
			s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
			p2cStep(t, m, b, s)
			if len(s.VisibleObjects) != 1 || len(s.BVHNodesBytes) == 0 {
				t.Fatal("promoted bounds absent from this commit")
			}
			liveTransform := p.o.Transform
			if p.o.XBrickMap != authority || liveTransform.Position != transform.Position || liveTransform.Rotation != transform.Rotation || liveTransform.Scale != transform.Scale || liveTransform.Pivot != transform.Pivot {
				t.Fatal("publication modified authority/instance transform")
			}
			for _, c := range [][3]int{{-2, -1, 0}, {0, 0, 0}, {1, 0, 0}} {
				p2bAssertLookup(t, m, b.p2bNative, p.o, c, true)
			}
			return
		}
	}
	t.Fatal("stage failed to become ready")
}

func TestS1l14EmptyGeometryStillRequiresMaterialCoverage(t *testing.T) {
	m, b, s, p := s1l14Fixture(t)
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	for i := 0; i < 5; i++ {
		s1l14Frame(t, m, b, s)
	}
	if p.o.RenderVoxelMap() != nil || s1l14Status(t, m, p).StageReady {
		t.Fatal("empty geometry bypassed material readiness")
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	s1l14Publish(t, m, b, s, p)
	if len(p.o.RenderVoxelMap().Sectors) != 0 {
		t.Fatal("empty publication acquired coordinates")
	}
}

func TestS1l14SharedEntryAllowanceAndIdleNoCapture(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	q := s1l14ProducerFor(s, [3]int{2, 0, 0})
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Publish(t, m, b, s, p)
	s1l14Publish(t, m, b, s, q)
	before := [4]int{p.full, p.sector, q.full, q.sector}
	for i := 0; i < 5; i++ {
		s1l14Frame(t, m, b, s)
		if m.ManagedGeometryFrameStats().AttemptedEntries != 0 {
			t.Fatal("idle sector work")
		}
	}
	if before != [4]int{p.full, p.sector, q.full, q.sector} {
		t.Fatal("idle frame captured/walked geometry")
	}
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
	s1l14Edit(m, q, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 3})
	s1l14Frame(t, m, b, s)
	if m.ManagedGeometryFrameStats().AttemptedEntries > 1 {
		t.Fatal("allowance multiplied by object count")
	}
	s1l14Publish(t, m, b, s, p)
	s1l14Publish(t, m, b, s, q)
	s1l14Value(t, p.o, 16, 16, 16, 2)
	s1l14Value(t, q.o, 80, 16, 16, 3)
}

func TestS1l14CurrentContentCommitKeepsOldCoordinateUntilUploadAndAvoidsRestage(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{}, [3]int{1, 0, 0})
	s1l14Publish(t, m, b, s, p)
	selected := p.o.RenderVoxelMap()
	old := selected.Sectors[[3]int{}]
	before := s1l14Status(t, m, p)
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 3})
	for i := 0; i < 6; i++ {
		s1l14Frame(t, m, b, s)
	}
	if selected != p.o.RenderVoxelMap() || selected.Sectors[[3]int{}] != old {
		t.Fatal("pending candidate overwrote committed coordinate")
	}
	s1l14Value(t, p.o, 16, 16, 16, 1)
	if status := s1l14Status(t, m, p); status.StageInput.SameSource(status.StageInput) || status.CurrentGeneration != before.CurrentGeneration {
		t.Fatal("content edit restaged structural topology or acknowledged unfinished upload")
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	s1l14Publish(t, m, b, s, p)
	if p.o.RenderVoxelMap() != selected {
		t.Fatal("content-only edit replaced whole-object selection")
	}
	s1l14Value(t, p.o, 16, 16, 16, 3)
}

func TestS1l14StructuralRefusalPreservesCurrentAndContinuesCurrentEdits(t *testing.T) {
	for _, pressure := range []string{"cpu", "native", "upload"} {
		t.Run(pressure, func(t *testing.T) {
			m, b, s, p := s1l14Fixture(t, [3]int{})
			// Leave room for old current plus one coordinate replacement. The
			// structural successor below exceeds sector capacity independently.
			b.buffers[m.DenseOccupancyBuf] = make([]byte, 4*VoxelAuxRecordBytes)
			s1l14Publish(t, m, b, s, p)
			current := p.o.RenderVoxelMap()
			s1l14Edit(m, p, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 4})
			if pressure == "native" {
				for i := 3; i < 24; i++ {
					c := [3]int{i, 0, 0}
					p.o.XBrickMap.Sectors[c] = volume.NewSector(i, 0, 0)
				}
				p.owner = volume.NewManagedXBrickMap(p.o.XBrickMap)
			}
			switch pressure {
			case "cpu":
				stats := m.ManagedGeometryAdmissionStats()
				m.SetManagedGeometryAdmissionBudget(ManagedGeometryAdmissionBudget{Enabled: true, MaxInputBytes: stats.InputBytes, MaxCopiedStageBytes: stats.TotalStageBytes})
			case "native":
				m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
			case "upload":
				m.SetVoxelUploadBudget(VoxelUploadBudget{})
			}
			for i := 0; i < 8; i++ {
				s1l14Frame(t, m, b, s)
				if p.o.RenderVoxelMap() != current {
					t.Fatal("refused/partial successor replaced current")
				}
			}
			if !s1l14Status(t, m, p).Pending {
				t.Fatal("pressure fixture did not retain structural work")
			}
			s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2})
			if pressure == "native" {
				for i := 0; i < 32; i++ {
					s1l14Frame(t, m, b, s)
					present, value := current.GetVoxel(16, 16, 16)
					if present && value == 2 {
						break
					}
					if i == 31 {
						t.Fatal("structural refusal starved current content")
					}
				}
				if current.Sectors[[3]int{2, 0, 0}] != nil {
					t.Fatal("new coordinate leaked into current finite topology")
				}
			}
			m.SetManagedGeometryAdmissionBudget(DefaultManagedGeometryAdmissionBudget())
			m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{})
			m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
			s1l14Publish(t, m, b, s, p)
			s1l14Value(t, p.o, 16, 16, 16, 2)
			s1l14Value(t, p.o, 80, 16, 16, 4)
		})
	}
}

func TestS1l14EqualCountReplacementRebuildsCoordinateLookups(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{-1, 0, 0})
	s1l14Publish(t, m, b, s, p)
	s1l14Edit(m, p, volume.VoxelWrite{X: -16, Y: 16, Z: 16}, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 3})
	s1l14Publish(t, m, b, s, p)
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{-1, 0, 0}, false)
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{2, 0, 0}, true)
	s1l14Value(t, p.o, -16, 16, 16, 0)
	s1l14Value(t, p.o, 80, 16, 16, 3)
}

func TestS1l14CPUStageStaysFrozenAcrossNativeNormalBake(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{}, [3]int{1, 0, 0})
	s1l14Prepare(t, m, s)
	for i := 0; i < 32; i++ {
		stage, ok := m.ManagedGeometryStage(p.o)
		if ok && stage.Complete() {
			break
		}
		s1l14Prepare(t, m, s)
	}
	stage, ok := m.ManagedGeometryStage(p.o)
	if !ok || !stage.Complete() {
		t.Fatal("CPU stage incomplete")
	}
	var saved []*volume.Sector
	for i := 0; i < stage.Len(); i++ {
		sector, _ := stage.CopySector(i)
		saved = append(saved, sector)
	}
	s1l14Publish(t, m, b, s, p)
	for i, want := range saved {
		got, _ := stage.CopySector(i)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("native bake mutated borrowed CPU payload")
		}
	}
}

func TestS1l14ExecutorInvalidationNeverCertifiesObsoleteStage(t *testing.T) {
	for _, kind := range []string{"generation", "source", "materials"} {
		t.Run(kind, func(t *testing.T) {
			m, b, s, p := s1l14Fixture(t, [3]int{})
			if kind == "materials" {
				p.o.MaterialTable = []core.Material{core.DefaultMaterial(), core.DefaultMaterial()}
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{})
			for i := 0; i < 8; i++ {
				s1l14Frame(t, m, b, s)
			}
			fired := false
			b.afterMaterialWrite = func() {
				fired = true
				switch kind {
				case "generation":
					p.owner.SetVoxel(16, 16, 16, 3)
					p.g++
				case "source":
					p.install()
				case "materials":
					p.o.MaterialTable = append([]core.Material(nil), p.o.MaterialTable...)
				}
			}
			m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
			s1l14Frame(t, m, b, s)
			if !fired {
				t.Fatal("fixture did not execute material write")
			}
			if p.o.RenderVoxelMap() != nil {
				t.Fatal("executor invalidation exposed private stage")
			}
			s1l14Prepare(t, m, s)
			if p.o.RenderVoxelMap() != nil {
				t.Fatal("precommit accepted obsolete completion certificate")
			}
			s1l14Publish(t, m, b, s, p)
		})
	}
}

func TestS1l14PauseCancelAndRemovalPreserveOwnedSnapshotChargesThenDrain(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{}, [3]int{1, 0, 0}, [3]int{2, 0, 0})
	s1l14Publish(t, m, b, s, p)
	selected := p.o.RenderVoxelMap()
	m.CancelManagedGeometryInputs(p.o)
	stats := m.ManagedGeometryAdmissionStats()
	if stats.InputBytes == 0 || stats.TotalStageBytes == 0 {
		t.Fatal("CPU cancellation erased retained GPU ownership charges")
	}
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true})
	for i := 0; i < 4; i++ {
		s1l14Prepare(t, m, s)
		if p.o.RenderVoxelMap() != selected || m.ManagedGeometryAdmissionStats() != stats {
			t.Fatal("zero allowance changed managed coverage/charges")
		}
	}
	s.Objects = nil
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	for i := 0; i < 128; i++ {
		s1l14Frame(t, m, b, s)
		if m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
			return
		}
	}
	t.Fatal("fully removed managed owners retained admission charges")
}

func TestS1l14DisableKeepsManagedCoverageUntilOrdinaryReplacementReady(t *testing.T) {
	for _, reason := range []string{"disable", "exposure", "source"} {
		t.Run(reason, func(t *testing.T) {
			m, b, s, p := s1l14Fixture(t, [3]int{})
			s1l14Publish(t, m, b, s, p)
			selected := p.o.RenderVoxelMap()
			m.SetVoxelUploadBudget(VoxelUploadBudget{})
			switch reason {
			case "disable":
				m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{})
			case "exposure":
				p.owner.ExposeMutable()
			case "source":
				p.o.SetManagedGeometryProducer(nil, nil)
			}
			for i := 0; i < 4; i++ {
				s1l14Frame(t, m, b, s)
				if p.o.RenderVoxelMap() != selected {
					t.Fatal("handoff cleared coverage before compatibility target ready")
				}
				if charges := m.ManagedGeometryAdmissionStats(); charges.InputBytes == 0 || charges.TotalStageBytes == 0 {
					t.Fatal("opt-out erased still-owned GPU snapshot charge")
				}
			}
			m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
			for i := 0; i < 128; i++ {
				s1l14Frame(t, m, b, s)
				if p.o.RenderVoxelMap() == p.o.XBrickMap && m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
					if ready, _, _ := m.RenderVoxelObjectReady(p.o, p.o.XBrickMap, p.o.XBrickMap.Revision); !ready {
						t.Fatal("ordinary handoff not ready")
					}
					return
				}
			}
			t.Fatal("opt-out handoff/retirement failed to drain")
		})
	}
}

func TestS1l14CurrentTombstoneKeepsCommittedLookupUntilUnitCompletes(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{}, [3]int{1, 0, 0})
	s1l14Publish(t, m, b, s, p)
	old := p.o.RenderVoxelMap().Sectors[[3]int{}]
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	// A removal consumes a sector/header/lookup commit unit even when it has no
	// payload write. Zero upload allowance must pause that visibility change.
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16})
	for i := 0; i < 8; i++ {
		s1l14Frame(t, m, b, s)
		if p.o.RenderVoxelMap().Sectors[[3]int{}] != old {
			t.Fatal("pending tombstone removed committed coordinate")
		}
		p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, true)
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	s1l14Publish(t, m, b, s, p)
	p2bAssertLookup(t, m, b.p2bNative, p.o, [3]int{}, false)
	s1l14Value(t, p.o, 16, 16, 16, 0)
}

func TestS1l14CurrentGapAndOverflowRepairKeepBoundedCursor(t *testing.T) {
	for _, n := range []int{12, 1025} {
		t.Run(map[int]string{12: "gap", 1025: "overflow"}[n], func(t *testing.T) {
			m, b, s := p2cFixture(t)
			m.SetManagedGeometryAdmissionBudget(DefaultManagedGeometryAdmissionBudget())
			m.SetManagedGeometryFrameBudget(DefaultManagedGeometryFrameBudget())
			raw := volume.NewXBrickMap()
			for i := 0; i < n; i++ {
				raw.Sectors[[3]int{i, 0, 0}] = volume.NewSector(i, 0, 0)
			}
			raw.SetVoxel(16, 16, 16, 1)
			p := &s1l14Producer{o: core.NewVoxelObject(), owner: volume.NewManagedXBrickMap(raw), g: 7, qualified: true}
			p.o.XBrickMap = raw.Copy()
			p.install()
			s.AddObject(p.o)
			s1l14Publish(t, m, b, s, p)
			selected := p.o.RenderVoxelMap()
			previous := p.g
			p.owner.SetVoxel(16, 16, 16, 3)
			p.o.XBrickMap.SetVoxel(16, 16, 16, 3)
			p.g++
			if n == 1025 {
				writes := make([]volume.VoxelWrite, n)
				for i := range writes {
					writes[i] = volume.VoxelWrite{X: i*32 + 16, Y: 16, Z: 16, Value: 3}
				}
				m.NotifyManagedGeometryGPUContent(p.o, previous, p.g, writes)
			}
			m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
			for i := 0; i < 4; i++ {
				reads := p.sector
				s1l14Frame(t, m, b, s)
				if p.sector-reads > 1 {
					t.Fatal("repair entry walked multiple sectors")
				}
			}
			if !s1l14Status(t, m, p).Pending {
				t.Fatal("finite repair acknowledged before all coordinates were qualified")
			}
			// A second generation in an already-running sweep must preserve progress
			// and arrange another finite pass rather than falsely closing coverage.
			p.owner.SetVoxel(16, 16, 16, 4)
			p.o.XBrickMap.SetVoxel(16, 16, 16, 4)
			p.g++
			m.SetManagedGeometryFrameBudget(DefaultManagedGeometryFrameBudget())
			s1l14Publish(t, m, b, s, p)
			if p.o.RenderVoxelMap() != selected {
				t.Fatal("repair rebuilt structural current with unchanged topology")
			}
			s1l14Value(t, p.o, 16, 16, 16, 4)
		})
	}
}

func TestS1l14QueuedWriteInvalidationRetainsCapturedSnapshotOwnership(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	s1l14Publish(t, m, b, s, p)
	current := p.o.RenderVoxelMap()
	old := current.Sectors[[3]int{}]
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 3})
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	for i := 0; i < 8; i++ {
		s1l14Frame(t, m, b, s)
	}
	fired := false
	b.afterAuxWrite = func() { fired = true; m.CancelManagedGeometryInputs(p.o); p.install() }
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	s1l14Frame(t, m, b, s)
	if !fired {
		t.Fatal("fixture missed actual auxiliary queue write")
	}
	if p.o.RenderVoxelMap() != current || current.Sectors[[3]int{}] != old {
		t.Fatal("obsolete queued write changed committed coordinate")
	}
	charges := m.ManagedGeometryAdmissionStats()
	if charges.InputBytes == 0 || charges.TotalStageBytes == 0 {
		t.Fatal("cancellation after native write erased snapshot ownership")
	}
	if _, ok := m.SectorToInfo[old]; !ok {
		t.Fatal("callback released old current snapshot edges")
	}
	s1l14Publish(t, m, b, s, p)
	s1l14Value(t, p.o, 16, 16, 16, 3)
}

func TestS1l14CompletePrivateTopologyAvailableToFirstNormalBake(t *testing.T) {
	m, b, s, p := s1l14Fixture(t)
	raw := volume.NewXBrickMap()
	for x := 29; x <= 34; x++ {
		for y := 14; y <= 17; y++ {
			raw.SetVoxel(x, y, 16, 1)
		}
	}
	p.owner = volume.NewManagedXBrickMap(raw)
	p.o.XBrickMap = raw.Copy()
	p.install()
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Publish(t, m, b, s, p)
	for _, c := range [][3]int{{0, 0, 0}, {1, 0, 0}} {
		sector := p.o.RenderVoxelMap().Sectors[c]
		base, mask := p2cHeader(t, m, b, sector)
		if mask == 0 {
			t.Fatal("published sector has no normal row")
		}
		brick := sector.PackedBricks[0]
		origin := [3]int{24, 8, 16}
		if c[0] == 1 {
			origin[0] = 32
		}
		want := volume.BuildVoxelAuxBytes(brick, origin, volume.VoxelNormalBakeOptions{HasBounds: true, BoundsMin: mgl32.Vec3{}, BoundsMax: mgl32.Vec3{64, 32, 32}, SampleOccupancy: func(v [3]int) bool { present, _ := raw.GetVoxel(v[0], v[1], v[2]); return present }})
		row := b.buffers[m.BrickTableBuf][uint64(base)*BrickRecordSize:]
		offset := uint64(binary.LittleEndian.Uint32(row[24:])) * 4
		if !bytes.Equal(b.buffers[m.DenseOccupancyBuf][offset:offset+VoxelAuxRecordBytes], want) {
			t.Fatal("first bake omitted private neighbor topology")
		}
	}
}

func TestS1l14ManagedRetirementCannotReuseSubmittedRangesBeforeFence(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	s1l14Publish(t, m, b, s, p)
	sector := p.o.RenderVoxelMap().Sectors[[3]int{}]
	base, _ := p2cHeader(t, m, b, sector)
	s.Objects = nil
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	for i := 0; i < 32; i++ {
		s1l14Frame(t, m, b, s)
	}
	queue := new(wgpu.Queue)
	m.MarkRetiredBuffersSubmitted(queue, wgpu.SubmissionIndex(914))
	for i := 0; i < RetiredBufferFrameDelay+2; i++ {
		m.AdvanceRetiredBuffers()
		s1l14Frame(t, m, b, s)
	}
	q := s1l14ProducerFor(s, [3]int{2, 0, 0})
	s1l14Publish(t, m, b, s, q)
	next, _ := p2cHeader(t, m, b, q.o.RenderVoxelMap().Sectors[[3]int{2, 0, 0}])
	if next == base {
		t.Fatal("managed retired range reused before completion fence")
	}
	b.completed[914] = true
	m.AdvanceRetiredBuffers()
	s1l14Frame(t, m, b, s)
}

func s1l14FiniteFixture(t *testing.T, n int) (*GpuBufferManager, *p2cNative, *core.Scene, *s1l14Producer) {
	t.Helper()
	m, b, s := p2cFixture(t)
	m.SetManagedGeometryAdmissionBudget(DefaultManagedGeometryAdmissionBudget())
	m.SetManagedGeometryFrameBudget(DefaultManagedGeometryFrameBudget())
	raw := volume.NewXBrickMap()
	for i := 0; i < n; i++ {
		raw.Sectors[[3]int{i, 0, 0}] = volume.NewSector(i, 0, 0)
	}
	raw.SetVoxel(16, 16, 16, 1)
	p := &s1l14Producer{o: core.NewVoxelObject(), owner: volume.NewManagedXBrickMap(raw), g: 7, qualified: true}
	p.o.XBrickMap = raw.Copy()
	p.install()
	s.AddObject(p.o)
	return m, b, s, p
}

func TestS1l14SparseCurrentContentCostIndependentOfFiniteTopologySize(t *testing.T) {
	m, b, s, p := s1l14FiniteFixture(t, 128)
	s1l14Publish(t, m, b, s, p)
	selected := p.o.RenderVoxelMap()
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 3})
	var attempted uint32
	for i := 0; i < 24; i++ {
		s1l14Frame(t, m, b, s)
		attempted += m.ManagedGeometryFrameStats().AttemptedEntries
		if p.o.RenderVoxelMap() != selected {
			t.Fatal("sparse edit replaced whole selection")
		}
		present, value := selected.GetVoxel(16, 16, 16)
		if present && value == 3 && s1l14Status(t, m, p).CurrentGeneration == p.g {
			return
		}
	}
	t.Fatalf("one interior coordinate needed more than 24 frames/%d attempted entries for 128-coordinate current topology", attempted)
}

func TestS1l14AutomaticSuccessorCoalescingPreservesAcceptedCPUProgress(t *testing.T) {
	m, _, s, p := s1l14FiniteFixture(t, 128)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Prepare(t, m, s)
	accepted, _ := m.ManagedGeometryInputs(p.o)
	before, ok := m.ManagedGeometryStage(p.o)
	if !ok || before.Len() != 1 || accepted.Generation() != p.g {
		t.Fatal("initial one-entry accepted progress")
	}
	s1l14Edit(m, p, volume.VoxelWrite{X: 128*32 + 16, Y: 16, Z: 16, Value: 2})
	s1l14Prepare(t, m, s)
	retained, successor := m.ManagedGeometryInputs(p.o)
	after, ok := m.ManagedGeometryStage(p.o)
	if !retained.SameSource(accepted) || retained.Generation() != accepted.Generation() || retained.Geometry().Len() != 128 {
		t.Fatal("automatic admission replaced accepted input with newer successor")
	}
	if !ok || !after.Input().SameSource(accepted) || after.Input().Generation() != accepted.Generation() || after.Len() <= before.Len() {
		t.Fatal("coalescing reset/stalled accepted enumeration progress")
	}
	if successor.Generation() != p.g || successor.Geometry().Len() != 129 {
		t.Fatal("newer structural input was not coalesced independently")
	}
}

func TestS1l14StageContentChangeKeepsUploadedCoordinateProgress(t *testing.T) {
	m, b, s, p := s1l14FiniteFixture(t, 16)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	var stageMap *volume.XBrickMap
	var uploaded *volume.Sector
	for i := 0; i < 128; i++ {
		s1l14Frame(t, m, b, s)
		for mapRef, a := range m.Allocations {
			if mapRef != p.o.XBrickMap && a.Sectors[[3]int{}] != nil {
				stageMap = mapRef
				uploaded = a.Sectors[[3]int{}]
				break
			}
		}
		if uploaded != nil {
			break
		}
	}
	if uploaded == nil || p.o.RenderVoxelMap() != nil {
		t.Fatal("fixture did not stop at a partially uploaded hidden stage")
	}
	before := s1l14Status(t, m, p)
	if !before.StageInput.SameSource(before.StageInput) || before.RetiringEntries != 0 {
		t.Fatal("initial stage identity/retirement")
	}
	s1l14Edit(m, p, volume.VoxelWrite{X: 15*32 + 16, Y: 16, Z: 16, Value: 3})
	s1l14Frame(t, m, b, s)
	after := s1l14Status(t, m, p)
	if !after.StageInput.SameSource(before.StageInput) || after.StageInput.Generation() != before.StageInput.Generation() || after.RetiringEntries != 0 {
		t.Fatal("stage scalar/content change retired accepted whole-generation progress")
	}
	allocation := m.Allocations[stageMap]
	if allocation == nil || allocation.Sectors[[3]int{}] != uploaded {
		t.Fatal("unrelated stage edit replaced already uploaded coordinate snapshot")
	}
	s1l14Publish(t, m, b, s, p)
	s1l14Value(t, p.o, 15*32+16, 16, 16, 3)
}

func TestS1l14CurrentLargerPayloadChargeTransfersAndSurvivesCPUCancel(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	s1l14Publish(t, m, b, s, p)
	m.CancelManagedGeometryInputs(p.o)
	before := m.ManagedGeometryAdmissionStats()
	if before.TotalStageBytes == 0 {
		t.Fatal("initial GPU charge absent")
	}
	var writes []volume.VoxelWrite
	for x := 0; x < 16; x++ {
		for y := 0; y < 16; y++ {
			writes = append(writes, volume.VoxelWrite{X: x, Y: y, Z: 16, Value: 2})
		}
	}
	s1l14Edit(m, p, writes...)
	s1l14Publish(t, m, b, s, p)
	if status := s1l14Status(t, m, p); status.RetiringEntries != 0 || status.Pending {
		t.Fatal("replacement did not drain old snapshot retirement")
	}
	m.CancelManagedGeometryInputs(p.o)
	after := m.ManagedGeometryAdmissionStats()
	if after.TotalStageBytes <= before.TotalStageBytes {
		t.Fatalf("larger retained GPU payload reverted to original charge after CPU cancellation: before %+v after %+v", before, after)
	}
	if after.InputBytes == 0 {
		t.Fatal("larger current lost retained input charge")
	}
	s1l14Value(t, p.o, 0, 0, 16, 2)
	s.Objects = nil
	for i := 0; i < 128; i++ {
		s1l14Frame(t, m, b, s)
		if m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
			return
		}
	}
	t.Fatal("larger current payload charge did not release after full removal")
}

func TestS1l14RemovalAfterPartialCurrentCommitDrainsEveryPrivateOwner(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{}, [3]int{1, 0, 0})
	s1l14Publish(t, m, b, s, p)
	selected := p.o.RenderVoxelMap()
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2}, volume.VoxelWrite{X: 48, Y: 16, Z: 16, Value: 3})
	partial := false
	for i := 0; i < 128; i++ {
		s1l14Frame(t, m, b, s)
		_, first := selected.GetVoxel(16, 16, 16)
		_, second := selected.GetVoxel(48, 16, 16)
		if (first == 2 && second == 1) || (first == 1 && second == 3) {
			partial = true
			break
		}
	}
	if !partial {
		t.Fatal("fixture did not observe a partial two-coordinate current commit")
	}
	if p.o.RenderVoxelMap() != selected {
		t.Fatal("partial content commit replaced selection")
	}
	s.Objects = nil
	for i := 0; i < 256; i++ {
		s1l14Frame(t, m, b, s)
		if len(m.Allocations) == 0 && len(m.SectorToInfo) == 0 && len(m.BrickToSlot) == 0 && m.ManagedGeometryAdmissionStats() == (ManagedGeometryAdmissionStats{}) {
			return
		}
	}
	t.Fatalf("partial commit orphaned managed ownership after removal: allocations=%d sectors=%d bricks=%d charges=%+v", len(m.Allocations), len(m.SectorToInfo), len(m.BrickToSlot), m.ManagedGeometryAdmissionStats())
}

func TestS1l14MaterialsAdvanceWhileCurrentCandidateAndStructuralStagePending(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	p.o.MaterialTable = []core.Material{core.DefaultMaterial(), core.DefaultMaterial()}
	s1l14Publish(t, m, b, s, p)
	selected := p.o.RenderVoxelMap()
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	// Geometry units are paused independently of byte/brick allowance, leaving
	// the production material upload unit able to replace the object binding.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxBricks: 1 << 20, MaxSectors: 0})
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 16, Value: 2}, volume.VoxelWrite{X: 80, Y: 16, Z: 16, Value: 3})
	p.o.MaterialTable = []core.Material{core.DefaultMaterial(), {Emission: 4}}
	uploaded := false
	for i := 0; i < 32; i++ {
		s1l14Frame(t, m, b, s)
		if p.o.RenderVoxelMap() != selected {
			t.Fatal("paused geometry replaced retained current")
		}
		s1l14Value(t, p.o, 16, 16, 16, 1)
		status := s1l14Status(t, m, p)
		if !status.Pending {
			t.Fatal("paused candidate/structural work was falsely complete")
		}
		uploaded = uploaded || m.VoxelMaterialsUploaded > 0
		if uploaded && status.StageInput.SameSource(status.StageInput) && materialBindingReady(p.o, m.MaterialAllocations[p.o], m.MaterialBufferGeneration) {
			if ready, _, _ := m.RenderVoxelObjectReady(p.o, selected, selected.Revision); !ready {
				t.Fatal("material replacement lost ready retained current coverage")
			}
			return
		}
	}
	t.Fatal("material ownership did not advance while newer current content and structural topology were paused")
}

func TestS1l14RefusedLargeStageKeepsNativeCapacityPlanningBounded(t *testing.T) {
	m, b, s, p := s1l14Fixture(t, [3]int{})
	b.buffers[m.DenseOccupancyBuf] = make([]byte, 4*VoxelAuxRecordBytes)
	s1l14Publish(t, m, b, s, p)
	selected := p.o.RenderVoxelMap()
	previous := p.g
	// Empty allocated coordinates exercise finite topology without adding 512
	// sparse payload bricks or exhausting the tiny native fixture's atlas.
	for i := 1; i < 512; i++ {
		c := [3]int{i, 0, 0}
		p.o.XBrickMap.Sectors[c] = volume.NewSector(i, 0, 0)
	}
	p.owner = volume.NewManagedXBrickMap(p.o.XBrickMap)
	p.g++
	m.NotifyManagedGeometryGPUContent(p.o, previous, p.g, nil)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	for i := 0; i < 1700; i++ {
		s1l14Frame(t, m, b, s)
		if visits := m.VoxelCapacityPlanningSectorVisitsLastUpdate; visits > 8 {
			t.Fatalf("frame %d planned %d sectors for one-entry managed allowance; refused frontier accumulated", i, visits)
		}
		if p.o.RenderVoxelMap() != selected {
			t.Fatal("refused large stage replaced current coverage")
		}
	}
	status := s1l14Status(t, m, p)
	if !status.Pending || !status.StageInput.SameSource(status.StageInput) || status.StageInput.Geometry().Len() != 512 {
		t.Fatalf("large native-refused stage not retained: %+v", status)
	}
}

func s1l14OccupiedBoundsNormalFixture(t *testing.T, highAnchor bool) (*GpuBufferManager, *p2cNative, *core.Scene, *s1l14Producer) {
	t.Helper()
	m, b, s, p := s1l14Fixture(t)
	raw := volume.NewXBrickMap()
	// An odd 5x5 sheet has a balanced center voxel: density gradients vanish
	// and its bounds-dependent normal tie-break is observable. It stays in one
	// brick, leaving the tiny native fake room for old+incoming snapshots.
	for x := -24; x < -19; x++ {
		for y := 8; y < 13; y++ {
			raw.SetVoxel(x, y, 4, 1)
		}
	}
	z := 4
	if highAnchor {
		z = 28
		raw.SetVoxel(16, 16, 4, 1)
	}
	raw.SetVoxel(16, 16, z, 1)
	p.o.XBrickMap = raw.Copy()
	p.owner = volume.NewManagedXBrickMap(raw)
	p.install()
	return m, b, s, p
}

// Compare actual uploaded auxiliary records with a fresh ordinary bake over the
// complete occupied map. Display/culling sector bounds are intentionally not
// supplied as normal bounds. Only the named refreshed coordinate is asserted:
// ordinary local-halo invalidation does not rebake unrelated distant sectors.
func s1l14AssertUploadedOccupiedNormals(t *testing.T, m *GpuBufferManager, b *p2cNative, p *s1l14Producer, coordinate [3]int) {
	t.Helper()
	raw := p.o.XBrickMap
	minimum, maximum := raw.ComputeAABB()
	source := raw.Sectors[coordinate]
	selected := p.o.RenderVoxelMap().Sectors[coordinate]
	if source == nil || selected == nil {
		t.Fatal("normal oracle coordinate absent")
	}
	base, mask := p2cHeader(t, m, b, selected)
	var rank uint32
	for index := 0; index < 64; index++ {
		if source.BrickMask64&(uint64(1)<<index) == 0 {
			continue
		}
		if mask&(uint64(1)<<index) == 0 {
			t.Fatal("uploaded normal oracle brick absent")
		}
		brick := source.GetBrick(index%4, (index/4)%4, index/16)
		origin := [3]int{coordinate[0]*32 + index%4*8, coordinate[1]*32 + (index/4)%4*8, coordinate[2]*32 + index/16*8}
		want := volume.BuildVoxelAuxBytes(brick, origin, volume.VoxelNormalBakeOptions{HasBounds: true, BoundsMin: minimum, BoundsMax: maximum, SampleOccupancy: func(v [3]int) bool { present, _ := raw.GetVoxel(v[0], v[1], v[2]); return present }})
		row := b.buffers[m.BrickTableBuf][uint64(base+rank)*BrickRecordSize:]
		offset := uint64(binary.LittleEndian.Uint32(row[24:])) * 4
		if !bytes.Equal(b.buffers[m.DenseOccupancyBuf][offset:offset+VoxelAuxRecordBytes], want) {
			t.Fatalf("uploaded normals at sector %v brick %d differ from complete occupied-bounds bake %v..%v", coordinate, index, minimum, maximum)
		}
		rank++
	}
}

func TestS1l14InitialThinSheetUploadedNormalsUseOccupiedBounds(t *testing.T) {
	m, b, s, p := s1l14OccupiedBoundsNormalFixture(t, false)
	s1l14Publish(t, m, b, s, p)
	s1l14AssertUploadedOccupiedNormals(t, m, b, p, [3]int{-1, 0, 0})
}

func TestS1l14CurrentExtremaChangesRefreshOccupiedNormalContext(t *testing.T) {
	for _, kind := range []string{"expand", "shrink", "tombstone"} {
		t.Run(kind, func(t *testing.T) {
			m, b, s, p := s1l14OccupiedBoundsNormalFixture(t, kind == "tombstone")
			if kind == "shrink" {
				p.owner.SetVoxel(-24, 8, 28, 2)
				p.o.XBrickMap.SetVoxel(-24, 8, 28, 2)
			}
			s1l14Publish(t, m, b, s, p)
			selected := p.o.RenderVoxelMap()
			switch kind {
			case "expand":
				s1l14Edit(m, p, volume.VoxelWrite{X: -24, Y: 8, Z: 28, Value: 2})
			case "shrink":
				s1l14Edit(m, p, volume.VoxelWrite{X: -24, Y: 8, Z: 28})
			case "tombstone":
				s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 28}, volume.VoxelWrite{X: -24, Y: 8, Z: 4, Value: 2})
			}
			s1l14Publish(t, m, b, s, p)
			if p.o.RenderVoxelMap() != selected {
				t.Fatal("equal-topology extrema edit replaced whole render selection")
			}
			s1l14AssertUploadedOccupiedNormals(t, m, b, p, [3]int{-1, 0, 0})
		})
	}
}

func TestS1l14StageExtremaChangeAfterPartialUploadRefreshesNormalContext(t *testing.T) {
	m, b, s, p := s1l14OccupiedBoundsNormalFixture(t, false)
	p.owner.SetVoxel(-24, 8, 28, 2)
	p.o.XBrickMap.SetVoxel(-24, 8, 28, 2)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	uploaded := false
	for i := 0; i < 64; i++ {
		s1l14Frame(t, m, b, s)
		for mapRef, allocation := range m.Allocations {
			if mapRef != p.o.XBrickMap && allocation.Sectors[[3]int{-1, 0, 0}] != nil {
				uploaded = true
				break
			}
		}
		if uploaded {
			break
		}
	}
	if !uploaded || p.o.RenderVoxelMap() != nil {
		t.Fatal("fixture missed hidden stage with one uploaded coordinate")
	}
	before := s1l14Status(t, m, p)
	s1l14Edit(m, p, volume.VoxelWrite{X: -24, Y: 8, Z: 28})
	s1l14Publish(t, m, b, s, p)
	after := s1l14Status(t, m, p)
	if !after.CurrentInput.SameSource(before.StageInput) || after.CurrentInput.Generation() != before.StageInput.Generation() {
		t.Fatal("stage content change replaced accepted captured identity")
	}
	s1l14AssertUploadedOccupiedNormals(t, m, b, p, [3]int{-1, 0, 0})
}

func TestS1l14StageDistantExtremaChangeInvalidatesPreviouslyUploadedNormalContext(t *testing.T) {
	m, b, s, p := s1l14OccupiedBoundsNormalFixture(t, true)
	m.SetManagedGeometryFrameBudget(ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})
	uploaded := false
	for i := 0; i < 64; i++ {
		s1l14Frame(t, m, b, s)
		for mapRef, allocation := range m.Allocations {
			if mapRef != p.o.XBrickMap && allocation.Sectors[[3]int{-1, 0, 0}] != nil {
				uploaded = true
				break
			}
		}
		if uploaded {
			break
		}
	}
	if !uploaded || p.o.RenderVoxelMap() != nil {
		t.Fatal("fixture missed hidden generation with sheet coordinate already uploaded")
	}
	before := s1l14Status(t, m, p)
	// This high point lies in another sector and outside the sheet's normal
	// halo. The low z=4 point keeps that sector present, so source topology and
	// accepted input identity stay fixed. A private full generation still must
	// rebake the sheet when its previously used global bounds context changes.
	s1l14Edit(m, p, volume.VoxelWrite{X: 16, Y: 16, Z: 28})
	s1l14Publish(t, m, b, s, p)
	after := s1l14Status(t, m, p)
	if !after.CurrentInput.SameSource(before.StageInput) || after.CurrentInput.Generation() != before.StageInput.Generation() {
		t.Fatal("distant stage edit replaced accepted captured identity")
	}
	s1l14AssertUploadedOccupiedNormals(t, m, b, p, [3]int{-1, 0, 0})
}

func TestS1l14InstanceBytesKeepConservativeBoundsWithTightNormalCache(t *testing.T) {
	m, _, s, p := s1l14Fixture(t)
	in, ok := p.o.CaptureManagedGeometryInput()
	if !ok {
		t.Fatal("managed input")
	}
	selected := volume.NewXBrickMap()
	selected.SetVoxel(1, 1, 4, 1)
	minimum, maximum := mgl32.Vec3{-32, 0, 0}, mgl32.Vec3{32, 32, 32}
	if !p.o.SetManagedRenderGeometry(in, selected, minimum, maximum) {
		t.Fatal("selection")
	}
	check := func(label string, data []byte) {
		t.Helper()
		if len(data) < 192 {
			t.Fatalf("%s instance absent", label)
		}
		for axis := 0; axis < 3; axis++ {
			gotMin := math.Float32frombits(binary.LittleEndian.Uint32(data[160+axis*4:]))
			gotMax := math.Float32frombits(binary.LittleEndian.Uint32(data[176+axis*4:]))
			if gotMin != minimum[axis] || gotMax != maximum[axis] {
				t.Fatalf("%s instance local axis %d cropped to normal-cache bounds %v..%v want %v..%v", label, axis, gotMin, gotMax, minimum[axis], maximum[axis])
			}
		}
	}
	for _, point := range [][3]int{{1, 1, 4}, {31, 1, 28}, {-24, 4, 8}} {
		selected.SetVoxel(point[0], point[1], point[2], 1)
		selected.ComputeAABB()
		s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
		check("direct builder", buildInstanceData(s.VisibleObjects, mgl32.Vec3{}))
		batch := m.prepareSceneRecords(s, mgl32.Vec3{})
		check("cached records", batch.visible.instances)
	}
}
