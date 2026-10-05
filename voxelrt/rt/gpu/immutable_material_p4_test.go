package gpu

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// Replace only native writes, preserving admission, scheduling, execution,
// completion, generation publication and readiness on the production path.
type p4Native struct {
	*s1kNative
	materialWrites     int
	materialBytes      uint64
	afterMaterialWrite func()
}

func (b *p4Native) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	if err := b.s1kNative.WriteBuffer(buffer, offset, data); err != nil {
		return err
	}
	if b.labels[buffer] == "MaterialBuf" {
		b.materialWrites++
		b.materialBytes += uint64(len(data))
		if callback := b.afterMaterialWrite; callback != nil {
			b.afterMaterialWrite = nil
			callback()
		}
	}
	return nil
}

func p4Table(t *testing.T, rows []core.Material, key string) *core.ImmutableMaterialTable {
	t.Helper()
	table, err := core.NewImmutableMaterialTable(rows, key)
	if err != nil {
		t.Fatal(err)
	}
	return table
}

func p4Fixture(t *testing.T) (*GpuBufferManager, *p4Native, *core.Scene) {
	t.Helper()
	m := s1iManager()
	m.VoxelPayloadPageSize, m.VoxelPayloadBricks, m.VoxelPayloadPageCount = 16, 2, 1
	m.VoxelPayloadTex[0] = new(wgpu.Texture)
	b := &p4Native{s1kNative: &s1kNative{t: t, buffers: make(map[*wgpu.Buffer][]byte), labels: make(map[*wgpu.Buffer]string), payload: make([]byte, 4096)}}
	fields := [7]**wgpu.Buffer{&m.SectorTableBuf, &m.BrickTableBuf, &m.DenseOccupancyBuf, &m.MaterialBuf, &m.SectorGridBuf, &m.DirectSectorLookupBuf, &m.SectorGridParamsBuf}
	sizes := [7]uint64{256, 128 * BrickRecordSize, 128 * VoxelAuxRecordBytes, 256 * 64, 1024 * 32, 256, 256}
	for i, field := range fields {
		buffer, err := b.CreateBuffer(voxelBufferLabels[i], sizes[i], i == 6)
		if err != nil {
			t.Fatal(err)
		}
		*field = buffer
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 4096, MaxBricks: 1 << 20})
	return m, b, core.NewScene()
}

func p4Object(scene *core.Scene, table *core.ImmutableMaterialTable) *core.VoxelObject {
	o := core.NewVoxelObject()
	if len(scene.Objects) == 0 {
		o.XBrickMap.SetVoxel(0, 0, 0, 1)
	} else {
		o.XBrickMap = scene.Objects[0].XBrickMap
	}
	o.SetImmutableMaterialTable(table)
	scene.Objects = append(scene.Objects, o)
	return o
}

func p4Step(t *testing.T, m *GpuBufferManager, b *p4Native, scene *core.Scene) bool {
	t.Helper()
	b.reset()
	b.materialWrites = 0
	b.materialBytes = 0
	recreated := m.updateVoxelData(scene, b)
	m.updateSectorGrid(scene)
	if m.VoxelUploadBytes != b.contentBytes {
		t.Fatalf("upload bytes=%d, native content bytes=%d", m.VoxelUploadBytes, b.contentBytes)
	}
	s1kCharge(t, m, b.s1kNative)
	return recreated
}

func p4Offset(t *testing.T, m *GpuBufferManager, o *core.VoxelObject) uint32 {
	t.Helper()
	a := m.MaterialAllocations[o]
	if a == nil {
		t.Fatal("material attachment was not admitted")
	}
	return a.MaterialOffset
}

func p4Bytes(t *testing.T, m *GpuBufferManager, b *p4Native, o *core.VoxelObject) []byte {
	t.Helper()
	start := uint64(p4Offset(t, m, o)) * 64
	data := b.buffers[m.MaterialBuf]
	if start+256*64 > uint64(len(data)) {
		t.Fatal("local 256-row material block exceeds published buffer")
	}
	return data[start : start+256*64]
}

func TestP4SharedMaterialThousandsOfBindingsUseOnePhysicalBlock(t *testing.T) {
	m, b, scene := p4Fixture(t)
	rows := make([]core.Material, 256)
	rows[1] = core.DefaultMaterial()
	for i := 0; i < 1000; i++ {
		// Independent sealed copies must intern equally; pointer equality is insufficient.
		p4Object(scene, p4Table(t, rows, "full semantics including gameplay tags"))
	}
	p4Step(t, m, b, scene)
	first := p4Offset(t, m, scene.Objects[0])
	for _, o := range scene.Objects {
		if p4Offset(t, m, o) != first {
			t.Fatal("equal immutable rows/semantics allocated different GPU blocks")
		}
		s1kReady(t, m, o, true)
	}
	if b.BufferSize(m.MaterialBuf) != 16384 || b.materialWrites != 1 || m.VoxelMaterialsUploaded != 1 {
		t.Fatalf("1000 bindings: material capacity=%d writes=%d completed=%d; want 16384/1/1", b.BufferSize(m.MaterialBuf), b.materialWrites, m.VoxelMaterialsUploaded)
	}
	sharedCapacity, sharedWrites, sharedBytes := b.BufferSize(m.MaterialBuf), b.materialWrites, b.materialBytes
	// Measure the same logical workload without certification through exactly
	// the same fake-native production path, rather than infer a baseline ratio.
	privateManager, privateBackend, privateScene := p4Fixture(t)
	for i := 0; i < 1000; i++ {
		o := p4Object(privateScene, nil)
		o.MaterialTable = rows
	}
	p4Step(t, privateManager, privateBackend, privateScene)
	privateOffsets := make(map[uint32]bool)
	for _, o := range privateScene.Objects {
		privateOffsets[p4Offset(t, privateManager, o)] = true
	}
	if len(privateOffsets) != 1000 || privateBackend.materialWrites != 1000 || privateBackend.materialBytes != 1000*256*64 {
		t.Fatal("uncertified baseline unexpectedly shared blocks or skipped physical writes")
	}
	if sharedCapacity >= privateBackend.BufferSize(privateManager.MaterialBuf) || sharedBytes != 256*64 {
		t.Fatal("certification failed to reduce measured material capacity and write bytes")
	}
	t.Logf("1000 bindings x 256 locals: uncertified material capacity=%d writes=%d bytes=%d; shared material capacity=%d writes=%d bytes=%d", privateBackend.BufferSize(privateManager.MaterialBuf), privateBackend.materialWrites, privateBackend.materialBytes, sharedCapacity, sharedWrites, sharedBytes)
	p4Step(t, m, b, scene)
	if b.materialWrites != 0 || m.VoxelMaterialsUploaded != 0 {
		t.Fatal("idle shared block was uploaded again")
	}
}

func TestP4DistinctSemanticsRowsAndUncertifiedTablesStaySeparate(t *testing.T) {
	m, b, scene := p4Fixture(t)
	rows := []core.Material{{}, core.DefaultMaterial()}
	p4Object(scene, p4Table(t, rows, "surface:wood"))
	p4Object(scene, p4Table(t, rows, "surface:stone"))
	changed := append([]core.Material(nil), rows...)
	changed[1].Emission = 2
	p4Object(scene, p4Table(t, changed, "surface:wood"))
	for i := 0; i < 2; i++ {
		o := p4Object(scene, nil)
		o.MaterialTable = rows // Deliberately the same mutable backing slice.
	}
	p4Step(t, m, b, scene)
	seen := make(map[uint32]bool)
	for _, o := range scene.Objects {
		offset := p4Offset(t, m, o)
		if seen[offset] {
			t.Fatal("distinct semantics/rows or uncertified mutable rows shared a block")
		}
		seen[offset] = true
		s1kReady(t, m, o, true)
	}
	if b.materialWrites != 5 || m.VoxelMaterialsUploaded != 5 {
		t.Fatal("each distinct material block must receive its own write")
	}
}

func TestP4RawMutationAndReplacementDetachOnlyAffectedBinding(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw mutation", true: "replacement"}[replacement], func(t *testing.T) {
			m, b, scene := p4Fixture(t)
			handle := p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "shared")
			a, other := p4Object(scene, handle), p4Object(scene, handle)
			p4Step(t, m, b, scene)
			oldOffset, oldBytes := p4Offset(t, m, other), bytes.Clone(p4Bytes(t, m, b, other))
			if replacement {
				rows := handle.MaterialTable()
				rows[1].Transparency = .75
				a.SetImmutableMaterialTable(p4Table(t, rows, "replacement"))
			} else {
				a.MaterialTable[1].Transparency = .75
			}
			s1kReady(t, m, a, false)
			s1kReady(t, m, other, true)
			p4Step(t, m, b, scene)
			if p4Offset(t, m, a) == oldOffset || p4Offset(t, m, other) != oldOffset || !bytes.Equal(oldBytes, p4Bytes(t, m, b, other)) {
				t.Fatal("editing one attachment altered the surviving shared block")
			}
			data := p4Bytes(t, m, b, a)
			if binary.LittleEndian.Uint32(data[64+44:]) != math.Float32bits(.75) {
				t.Fatal("detached object's transparency was not uploaded")
			}
			if m.VoxelMaterialsUploaded != 1 {
				t.Fatal("detachment redundantly uploaded unaffected shared block")
			}
			s1kReady(t, m, a, true)
			s1kReady(t, m, other, true)
		})
	}
}

func TestP4SharedMaterialReleaseKeepsSurvivorThenReusesFinalSlot(t *testing.T) {
	m, b, scene := p4Fixture(t)
	handle := p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "shared")
	a, survivor := p4Object(scene, handle), p4Object(scene, handle)
	p4Step(t, m, b, scene)
	offset, saved := p4Offset(t, m, survivor), bytes.Clone(p4Bytes(t, m, b, survivor))
	scene.Objects = []*core.VoxelObject{survivor}
	p4Step(t, m, b, scene)
	if m.MaterialAllocations[a] != nil || p4Offset(t, m, survivor) != offset || !bytes.Equal(saved, p4Bytes(t, m, b, survivor)) || b.materialWrites != 0 {
		t.Fatal("removing one shared owner released or rewrote surviving block")
	}
	s1kReady(t, m, survivor, true)
	scene.Objects = nil
	p4Step(t, m, b, scene)
	if m.MaterialAllocations[survivor] != nil {
		t.Fatal("removed final binding retained attachment")
	}
	arrival := p4Object(scene, p4Table(t, []core.Material{{}, {Emission: 3}}, "new"))
	p4Step(t, m, b, scene)
	if p4Offset(t, m, arrival) != offset || b.BufferSize(m.MaterialBuf) != 16384 {
		t.Fatal("last owner failed to return its material slot for safe reuse")
	}
	s1kReady(t, m, arrival, true)
}

func TestP4PausedSharedUploadHasNoFalseAcknowledgement(t *testing.T) {
	m, b, scene := p4Fixture(t)
	handle := p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "shared")
	a, other := p4Object(scene, handle), p4Object(scene, handle)
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	p4Step(t, m, b, scene)
	if b.materialWrites != 0 || m.VoxelMaterialsUploaded != 0 {
		t.Fatal("paused content service wrote or acknowledged material")
	}
	s1kReady(t, m, a, false)
	s1kReady(t, m, other, false)
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	p4Step(t, m, b, scene)
	if b.materialWrites != 1 || m.VoxelMaterialsUploaded != 1 {
		t.Fatal("resumed shared block must upload once")
	}
	s1kReady(t, m, a, true)
	s1kReady(t, m, other, true)
}

func TestP4ShortAndEmptyMaterialBlocksClearReusedGPUTails(t *testing.T) {
	for _, rows := range [][]core.Material{nil, {{Emission: 2}}} {
		m, b, scene := p4Fixture(t)
		full := make([]core.Material, 256)
		for i := range full {
			full[i] = core.DefaultMaterial()
		}
		old := p4Object(scene, p4Table(t, full, "full"))
		p4Step(t, m, b, scene)
		offset := p4Offset(t, m, old)
		scene.Objects = nil
		p4Step(t, m, b, scene)
		next := p4Object(scene, p4Table(t, rows, "short"))
		p4Step(t, m, b, scene)
		if p4Offset(t, m, next) != offset {
			t.Fatal("fixture did not reuse released material block")
		}
		data := p4Bytes(t, m, b, next)
		if !bytes.Equal(data[len(rows)*64:], make([]byte, (256-len(rows))*64)) {
			t.Fatal("new short/empty shared palette exposed previous owner's GPU tail")
		}
		if len(rows) != 0 && binary.LittleEndian.Uint32(data[48:]) != math.Float32bits(2) {
			t.Fatal("short palette row missing from GPU block")
		}
		s1kReady(t, m, next, true)
	}
}

func TestP4InFlightMaterialWriteCannotAcknowledgeChangedBinding(t *testing.T) {
	for _, change := range []string{"raw mutation", "replacement", "removal"} {
		t.Run(change, func(t *testing.T) {
			m, b, scene := p4Fixture(t)
			handle := p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "original")
			a, other := p4Object(scene, handle), p4Object(scene, handle)
			b.afterMaterialWrite = func() {
				switch change {
				case "raw mutation":
					a.MaterialTable[1].Transparency = .75
				case "replacement":
					rows := handle.MaterialTable()
					rows[1].Transparency = .75
					a.SetImmutableMaterialTable(p4Table(t, rows, "replacement"))
				case "removal":
					scene.Objects = []*core.VoxelObject{other}
				}
			}
			p4Step(t, m, b, scene)
			if b.afterMaterialWrite != nil {
				t.Fatal("fixture did not exercise in-flight material change")
			}
			s1kReady(t, m, a, false)
			s1kReady(t, m, other, true)
			p4Step(t, m, b, scene)
			s1kReady(t, m, other, true)
			if binary.LittleEndian.Uint32(p4Bytes(t, m, b, other)[64+44:]) != 0 {
				t.Fatal("stale write contaminated another binding's shared block")
			}
			if change == "removal" {
				if m.MaterialAllocations[a] != nil {
					t.Fatal("in-flight removed object retained material attachment")
				}
			} else {
				s1kReady(t, m, a, true)
				if p4Offset(t, m, a) == p4Offset(t, m, other) || binary.LittleEndian.Uint32(p4Bytes(t, m, b, a)[64+44:]) != math.Float32bits(.75) {
					t.Fatal("changed binding was acknowledged against stale shared content")
				}
			}
		})
	}
}

func TestP4MaterialSharingSurvivesGrowthAndMirroredLiveEdit(t *testing.T) {
	m, b, scene := p4Fixture(t)
	handle := p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "shared")
	a, other := p4Object(scene, handle), p4Object(scene, handle)
	p4Step(t, m, b, scene)
	oldBuffer, oldOffset := m.MaterialBuf, p4Offset(t, m, other)
	saved := bytes.Clone(p4Bytes(t, m, b, other))
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 64})
	arrival := p4Object(scene, p4Table(t, []core.Material{{}, {Emission: 2}}, "arrival"))
	p4Step(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending || m.MaterialBuf != oldBuffer {
		t.Fatal("growth fixture must leave new generation unpublished")
	}
	s1kReady(t, m, other, true)
	// This new attachment arrives while the old buffer is being copied. Its
	// content must survive both staged publication and any subsequent growth.
	a.MaterialTable[1].Transparency = .75
	p4Step(t, m, b, scene)
	for i := 0; i < 2000; i++ {
		p4Step(t, m, b, scene)
		readyA, _, _ := m.RenderVoxelObjectReady(a, a.RenderVoxelMap(), a.RenderVoxelMap().Revision)
		readyArrival, _, _ := m.RenderVoxelObjectReady(arrival, arrival.RenderVoxelMap(), arrival.RenderVoxelMap().Revision)
		if !m.VoxelGPUWorkStats().Pending && readyA && readyArrival {
			break
		}
		if i == 1999 {
			t.Fatal("shared material growth did not converge")
		}
	}
	if m.MaterialBuf == oldBuffer || p4Offset(t, m, other) != oldOffset || !bytes.Equal(saved, p4Bytes(t, m, b, other)) {
		t.Fatal("growth changed unaffected shared offset/content")
	}
	if p4Offset(t, m, a) == oldOffset || binary.LittleEndian.Uint32(p4Bytes(t, m, b, a)[64+44:]) != math.Float32bits(.75) {
		t.Fatal("growth publication lost detached live material edit")
	}
	for _, o := range scene.Objects {
		s1kReady(t, m, o, true)
	}
	// Every binding's scene record must publish its own current shader offset.
	params := buildObjectParamsData(scene.Objects, m.Allocations, m.MaterialAllocations)
	for i, o := range scene.Objects {
		if binary.LittleEndian.Uint32(params[i*128+12:]) != p4Offset(t, m, o)*4 {
			t.Fatal("growth published stale object material offset")
		}
	}
}

func TestP4DeniedSharedCandidateDoesNotLeakReservation(t *testing.T) {
	m, b, scene := p4Fixture(t)
	resident := p4Object(scene, p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "resident"))
	p4Step(t, m, b, scene)
	baseline := m.VoxelGPUAdmissionStats().TotalBytes
	oldOffset, oldBytes := p4Offset(t, m, resident), bytes.Clone(p4Bytes(t, m, b, resident))
	newTable := p4Table(t, []core.Material{{}, {Emission: 2}}, "arrival")
	a, other := p4Object(scene, newTable), p4Object(scene, newTable)
	a.VoxelGPUAdmissionOptional, other.VoxelGPUAdmissionOptional = true, true
	// A refused candidate earlier in the admission order must not contaminate
	// the later trial for a binding to an already-current physical block.
	sharedArrival := p4Object(scene, p4Table(t, resident.MaterialTable, "resident"))
	sharedArrival.VoxelGPUAdmissionOptional = true
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: baseline})
	for i := 0; i < 3; i++ {
		p4Step(t, m, b, scene)
		if m.MaterialAllocations[a] != nil || m.MaterialAllocations[other] != nil || b.materialWrites != 0 || m.VoxelGPUAdmissionStats().TotalBytes != baseline {
			t.Fatal("denied shared candidate published attachment, writes or physical reservation")
		}
		s1kReady(t, m, a, false)
		s1kReady(t, m, resident, true)
		s1kReady(t, m, sharedArrival, true)
		if p4Offset(t, m, sharedArrival) != oldOffset {
			t.Fatal("later sharing candidate lost current block after refused trial")
		}
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{})
	p4Step(t, m, b, scene)
	if p4Offset(t, m, a) != p4Offset(t, m, other) || p4Offset(t, m, a) == oldOffset || b.BufferSize(m.MaterialBuf) != 32768 {
		t.Fatal("rolled-back candidates leaked slots or were not interned on retry")
	}
	if !bytes.Equal(oldBytes, p4Bytes(t, m, b, resident)) {
		t.Fatal("admission retry changed resident block")
	}
	for _, o := range scene.Objects {
		s1kReady(t, m, o, true)
	}
}

func TestP4ExistingSharedBlockArrivalAndDuplicateSceneOwnership(t *testing.T) {
	m, b, scene := p4Fixture(t)
	handle := p4Table(t, []core.Material{{}, core.DefaultMaterial()}, "shared")
	resident := p4Object(scene, handle)
	scene.Objects = append(scene.Objects, resident, resident)
	p4Step(t, m, b, scene)
	if b.materialWrites != 1 {
		t.Fatal("duplicate scene occurrence duplicated material upload")
	}
	offset := p4Offset(t, m, resident)
	arrival := p4Object(scene, p4Table(t, handle.MaterialTable(), "shared"))
	p4Step(t, m, b, scene)
	if b.materialWrites != 0 || m.VoxelMaterialsUploaded != 0 || p4Offset(t, m, arrival) != offset {
		t.Fatal("arrival with separate legacy slice rewrote already-current shared block")
	}
	s1kReady(t, m, arrival, true)
	scene.Objects = []*core.VoxelObject{arrival, arrival}
	p4Step(t, m, b, scene)
	s1kReady(t, m, arrival, true)
	scene.Objects = nil
	p4Step(t, m, b, scene)
	a := p4Object(scene, p4Table(t, []core.Material{{}, {Emission: 3}}, "new-a"))
	other := p4Object(scene, p4Table(t, []core.Material{{}, {Emission: 4}}, "new-b"))
	p4Step(t, m, b, scene)
	if p4Offset(t, m, a) == p4Offset(t, m, other) {
		t.Fatal("duplicate removal freed material slot twice")
	}
	for _, o := range scene.Objects {
		s1kReady(t, m, o, true)
	}
}

func TestP4BindingSwapAtExactTwoBlockCapacity(t *testing.T) {
	m, b, scene := p4Fixture(t)
	handleA := p4Table(t, []core.Material{{}, {Emission: 2}}, "A")
	handleB := p4Table(t, []core.Material{{}, {Emission: 3}}, "B")
	a, other := p4Object(scene, handleA), p4Object(scene, handleB)
	p4Step(t, m, b, scene)
	if b.BufferSize(m.MaterialBuf) != 32768 {
		t.Fatal("fixture requires exactly two physical blocks")
	}
	baseline, oldBuffer := m.VoxelGPUAdmissionStats().TotalBytes, m.MaterialBuf
	oldA, oldB := p4Offset(t, m, a), p4Offset(t, m, other)
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: baseline})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true})
	a.SetImmutableMaterialTable(handleB)
	other.SetImmutableMaterialTable(handleA)
	p4Step(t, m, b, scene)
	if p4Offset(t, m, a) != oldB || p4Offset(t, m, other) != oldA || m.MaterialBuf != oldBuffer || m.VoxelGPUWorkStats().Pending || b.materialWrites != 0 {
		t.Fatal("binding swap needs a transient third block or redundant upload")
	}
	s1kReady(t, m, a, true)
	s1kReady(t, m, other, true)
}

func TestP4SharedMaterialOpacityInvalidatesOnlyAffectedShadow(t *testing.T) {
	m, scene, camera := r2aFixture(t, core.LightTypeSpot)
	handle := p4Table(t, scene.Objects[0].MaterialTable, "shared")
	for _, o := range scene.Objects {
		o.SetImmutableMaterialTable(handle)
	}
	resources := s1iResources(1 << 24)
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if p4Offset(t, m, scene.Objects[0]) != p4Offset(t, m, scene.Objects[1]) {
		t.Fatal("shadow fixture materials did not share")
	}
	r2aWarm(t, m, scene, camera)
	geometryRevision := scene.Objects[0].XBrickMap.Revision
	scene.Objects[0].MaterialTable[1].Transparency = 1
	s1iRun(t, m, scene, &resources, s1iGrow(&resources))
	if scene.Objects[0].XBrickMap.Revision != geometryRevision || scene.Objects[1].MaterialTable[1].Transparency != 0 {
		t.Fatal("material detach altered geometry or remote palette")
	}
	r2aCommit(m, scene, camera)
	r2aAssert(t, m, scene, camera, r2aWarmFrame+1, []int{0})
}

func TestP4SharedMaterialUsesHighestPriorityCurrentBinding(t *testing.T) {
	m, b, scene := p4Fixture(t)
	shared := p4Table(t, []core.Material{{}, {Emission: 2}}, "shared-A")
	low := p4Object(scene, shared)
	medium := p4Object(scene, p4Table(t, []core.Material{{}, {Emission: 3}}, "distinct-B"))
	high := p4Object(scene, shared)
	low.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	medium.VoxelUploadPriority = core.VoxelUploadPriorityVisible
	high.VoxelUploadPriority = core.VoxelUploadPriorityFallback
	// One complete material block fits; geometry cannot consume this budget.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 256 * 64})
	p4Step(t, m, b, scene)
	if p4Offset(t, m, low) != p4Offset(t, m, high) {
		t.Fatal("shared-A owners received different material blocks")
	}
	if b.materialWrites != 1 || b.materialBytes != 256*64 || m.VoxelMaterialsUploaded != 1 {
		t.Fatal("first budget step must complete exactly one material block")
	}
	if binary.LittleEndian.Uint32(p4Bytes(t, m, b, high)[64+48:]) != math.Float32bits(2) || binary.LittleEndian.Uint32(p4Bytes(t, m, b, medium)[64+48:]) != 0 {
		t.Fatal("shared block used the early low-priority owner instead of its later high-priority binding")
	}
	p4Step(t, m, b, scene)
	if b.materialWrites != 1 || m.VoxelMaterialsUploaded != 1 || binary.LittleEndian.Uint32(p4Bytes(t, m, b, medium)[64+48:]) != math.Float32bits(3) {
		t.Fatal("medium-priority distinct block did not receive the next material budget")
	}
}

func TestP4AllSharedOwnersReplaceWithinOneBlockBudget(t *testing.T) {
	m, b, scene := p4Fixture(t)
	oldTable := p4Table(t, []core.Material{{}, {Emission: 2}}, "old")
	newTable := p4Table(t, []core.Material{{}, {Emission: 3}}, "new")
	for i := 0; i < 8; i++ {
		p4Object(scene, oldTable)
	}
	p4Step(t, m, b, scene)
	baseline, buffer := m.VoxelGPUAdmissionStats().TotalBytes, m.MaterialBuf
	offset := p4Offset(t, m, scene.Objects[0])
	if b.BufferSize(buffer) != 16384 {
		t.Fatal("fixture requires one physical material block")
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: baseline})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true})
	for _, o := range scene.Objects {
		o.VoxelGPUAdmissionOptional = true
	}
	for _, next := range []*core.ImmutableMaterialTable{newTable, oldTable, newTable, oldTable} {
		for _, o := range scene.Objects {
			o.SetImmutableMaterialTable(next)
		}
		p4Step(t, m, b, scene)
		if m.MaterialBuf != buffer || b.BufferSize(m.MaterialBuf) != 16384 || m.VoxelGPUWorkStats().Pending || m.VoxelGPUAdmissionStats().TotalBytes != baseline {
			t.Fatal("replacing every shared owner required extra physical capacity")
		}
		if b.materialWrites != 1 || m.VoxelMaterialsUploaded != 1 {
			t.Fatal("group replacement must upload exactly one newly shared block")
		}
		want := math.Float32bits(next.MaterialTable()[1].Emission)
		for _, o := range scene.Objects {
			if p4Offset(t, m, o) != offset || binary.LittleEndian.Uint32(p4Bytes(t, m, b, o)[64+48:]) != want {
				t.Fatal("group replacement left stale content or a leaked material slot")
			}
			s1kReady(t, m, o, true)
		}
	}
}

func TestP4FailedMaterialGrowthPreservesOldAttachmentsUntilPhysicalSuccess(t *testing.T) {
	m, b, scene := p4Fixture(t)
	oldTable := p4Table(t, []core.Material{{}, {Emission: 2}}, "old-shared")
	a, other := p4Object(scene, oldTable), p4Object(scene, oldTable)
	p4Step(t, m, b, scene)
	buffer, baseline := m.MaterialBuf, m.VoxelGPUAdmissionStats()
	allocationA, allocationB := m.MaterialAllocations[a], m.MaterialAllocations[other]
	offset := p4Offset(t, m, a)
	saved := bytes.Clone(p4Bytes(t, m, b, a))
	a.SetImmutableMaterialTable(p4Table(t, []core.Material{{}, {Emission: 3}}, "new-A"))
	other.SetImmutableMaterialTable(p4Table(t, []core.Material{{}, {Emission: 4}}, "new-B"))
	b.failCreate, b.failCreateAfter = true, 0
	p4Step(t, m, b, scene)
	if m.MaterialBuf != buffer || m.MaterialAllocations[a] != allocationA || m.MaterialAllocations[other] != allocationB || p4Offset(t, m, a) != offset || p4Offset(t, m, other) != offset || !bytes.Equal(saved, p4Bytes(t, m, b, a)) {
		t.Fatal("failed physical material growth changed old attachment ownership or content")
	}
	if b.materialWrites != 0 || m.VoxelMaterialsUploaded != 0 || m.VoxelGPUAdmissionStats().AllocationFailures <= baseline.AllocationFailures || m.VoxelGPUAdmissionStats().TotalBytes != baseline.TotalBytes {
		t.Fatal("failed growth wrote, acknowledged or leaked replacement resources")
	}
	s1kReady(t, m, a, false)
	s1kReady(t, m, other, false)
	b.failCreate = false
	p4Step(t, m, b, scene)
	if p4Offset(t, m, a) == p4Offset(t, m, other) || binary.LittleEndian.Uint32(p4Bytes(t, m, b, a)[64+48:]) != math.Float32bits(3) || binary.LittleEndian.Uint32(p4Bytes(t, m, b, other)[64+48:]) != math.Float32bits(4) {
		t.Fatal("successful retry failed to publish distinct current material blocks")
	}
	s1kReady(t, m, a, true)
	s1kReady(t, m, other, true)
}

func TestP4ActiveOwnerWithoutUploadTargetProtectsSharedBlock(t *testing.T) {
	m, b, scene := p4Fixture(t)
	oldTable := p4Table(t, []core.Material{{}, {Emission: 2}}, "old-shared")
	a, unselected := p4Object(scene, oldTable), p4Object(scene, oldTable)
	p4Step(t, m, b, scene)
	buffer, baseline := m.MaterialBuf, m.VoxelGPUAdmissionStats().TotalBytes
	oldAllocation, offset := m.MaterialAllocations[unselected], p4Offset(t, m, unselected)
	saved := bytes.Clone(p4Bytes(t, m, b, unselected))
	// This owner remains active in the scene and retains the old seal, but
	// offers no selected voxel target to the admission/service candidate list.
	unselected.XBrickMap = nil
	a.SetImmutableMaterialTable(p4Table(t, []core.Material{{}, {Emission: 3}}, "new"))
	a.VoxelGPUAdmissionOptional = true
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: baseline})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true})
	p4Step(t, m, b, scene)
	if m.MaterialAllocations[unselected] != oldAllocation || p4Offset(t, m, unselected) != offset || !bytes.Equal(saved, p4Bytes(t, m, b, unselected)) {
		t.Fatal("owner without an upload target lost its still-requested shared palette")
	}
	if m.MaterialBuf != buffer || b.materialWrites != 0 || m.VoxelGPUWorkStats().Pending {
		t.Fatal("protected one-block budget admitted a replacement or extra capacity")
	}
	s1kReady(t, m, a, false)
	scene.Objects = []*core.VoxelObject{a}
	p4Step(t, m, b, scene)
	if m.MaterialAllocations[unselected] != nil || p4Offset(t, m, a) != offset || m.MaterialBuf != buffer || b.materialWrites != 1 || binary.LittleEndian.Uint32(p4Bytes(t, m, b, a)[64+48:]) != math.Float32bits(3) {
		t.Fatal("removing final old owner did not safely recycle the protected block")
	}
	s1kReady(t, m, a, true)
}

func TestP4PartialOptionalReplacementCannotPublishDeniedStaleOffset(t *testing.T) {
	m, b, scene := p4Fixture(t)
	oldTable := p4Table(t, []core.Material{{}, {Emission: 2}}, "old-shared")
	a, other := p4Object(scene, oldTable), p4Object(scene, oldTable)
	p4Step(t, m, b, scene)
	buffer, baseline := m.MaterialBuf, m.VoxelGPUAdmissionStats().TotalBytes
	a.SetImmutableMaterialTable(p4Table(t, []core.Material{{}, {Emission: 3}}, "new-A"))
	other.SetImmutableMaterialTable(p4Table(t, []core.Material{{}, {Emission: 4}}, "new-B"))
	a.VoxelGPUAdmissionOptional, other.VoxelGPUAdmissionOptional = true, true
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: baseline})
	p4Step(t, m, b, scene)
	if m.MaterialBuf != buffer || b.BufferSize(buffer) != 16384 || m.VoxelGPUAdmissionStats().TotalBytes != baseline || b.materialWrites > 1 {
		t.Fatal("one-block optional plan exceeded physical capacity")
	}
	var admitted *core.VoxelObject
	var admittedBytes []byte
	for i, o := range scene.Objects {
		ready, _, _ := m.RenderVoxelObjectReady(o, o.RenderVoxelMap(), o.RenderVoxelMap().Revision)
		if !ready {
			continue
		}
		if admitted != nil {
			t.Fatal("two distinct new palettes were acknowledged against one material block")
		}
		admitted = o
		admittedBytes = bytes.Clone(p4Bytes(t, m, b, o))
		if binary.LittleEndian.Uint32(admittedBytes[64+48:]) != math.Float32bits(float32(3+i)) {
			t.Fatal("partial admission acknowledged wrong palette bytes")
		}
	}
	if admitted != nil {
		for _, o := range scene.Objects {
			if o != admitted && m.MaterialAllocations[o] != nil {
				t.Fatal("denied replacement retained an old offset now containing another object's new palette")
			}
		}
	}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{})
	p4Step(t, m, b, scene)
	if p4Offset(t, m, a) == p4Offset(t, m, other) {
		t.Fatal("lifting budget did not create distinct material bindings")
	}
	for i, o := range scene.Objects {
		s1kReady(t, m, o, true)
		if binary.LittleEndian.Uint32(p4Bytes(t, m, b, o)[64+48:]) != math.Float32bits(float32(3+i)) {
			t.Fatal("admission retry published wrong current palette")
		}
	}
	if admitted != nil && !bytes.Equal(admittedBytes, p4Bytes(t, m, b, admitted)) {
		t.Fatal("admitting deferred replacement changed previously admitted material content")
	}
}

func TestP4PendingCertifiedMaterialsCannotPublishTraversalParams(t *testing.T) {
	assertRows := func(t *testing.T, m *GpuBufferManager, scene *core.Scene, allowed map[*core.VoxelObject]bool) {
		t.Helper()
		// Exercise every pass consumer and the fresh record builder; cached
		// rows must change when acknowledgement changes without an offset edit.
		scene.VisibleObjects, scene.TransparentVisibleObjects, scene.ShadowObjects = scene.Objects, scene.Objects, scene.Objects
		prepared := m.prepareSceneRecords(scene, mgl32.Vec3{})
		for _, pass := range []struct {
			name string
			data []byte
		}{
			{"opaque", prepared.visible.params},
			{"transparent", prepared.transparent.params},
			{"shadow", prepared.shadow.params},
			{"fresh builder", buildObjectParamsData(scene.Objects, m.Allocations, m.MaterialAllocations)},
		} {
			for i, o := range scene.Objects {
				row := pass.data[i*128 : (i+1)*128]
				if allowed[o] {
					if binary.LittleEndian.Uint32(row[0:]) != o.RenderVoxelMap().ID || binary.LittleEndian.Uint32(row[24:]) != uint32(len(o.RenderVoxelMap().Sectors)) || binary.LittleEndian.Uint32(row[12:]) != p4Offset(t, m, o)*4 {
						t.Fatalf("%s ready object %d did not publish current geometry/material offset", pass.name, i)
					}
				} else if binary.LittleEndian.Uint32(row[0:]) != 0 || binary.LittleEndian.Uint32(row[24:]) != 0 || binary.LittleEndian.Uint32(row[108:]) != LookupModeDirect || binary.LittleEndian.Uint32(row[124:]) != DirectSectorLookupInvalid {
					t.Fatalf("%s pending certified object %d published traversal-capable parameters", pass.name, i)
				}
			}
		}
	}
	t.Run("initial shared block", func(t *testing.T) {
		m, b, scene := p4Fixture(t)
		legacy := p4Object(scene, nil)
		p4Step(t, m, b, scene)
		shared := p4Table(t, []core.Material{{}, {Emission: 2}}, "initial")
		a, other := p4Object(scene, shared), p4Object(scene, shared)
		m.SetVoxelUploadBudget(VoxelUploadBudget{})
		p4Step(t, m, b, scene)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{legacy: true})
		m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
		p4Step(t, m, b, scene)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{legacy: true, a: true, other: true})
	})
	t.Run("pending replacement preserves ready peer", func(t *testing.T) {
		m, b, scene := p4Fixture(t)
		shared := p4Table(t, []core.Material{{}, {Emission: 2}}, "initial")
		a, other := p4Object(scene, shared), p4Object(scene, shared)
		p4Step(t, m, b, scene)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{a: true, other: true})
		a.SetImmutableMaterialTable(p4Table(t, []core.Material{{}, {Emission: 3}}, "replacement"))
		m.SetVoxelUploadBudget(VoxelUploadBudget{})
		p4Step(t, m, b, scene)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{other: true})
		s1kReady(t, m, other, true)
		m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
		p4Step(t, m, b, scene)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{a: true, other: true})
	})
	t.Run("raw edit pending private upload preserves ready peer", func(t *testing.T) {
		m, b, scene := p4Fixture(t)
		shared := p4Table(t, []core.Material{{}, {Emission: 2}}, "initial")
		a, other := p4Object(scene, shared), p4Object(scene, shared)
		p4Step(t, m, b, scene)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{a: true, other: true})
		a.MaterialTable[1].Emission = 3
		m.SetVoxelUploadBudget(VoxelUploadBudget{})
		p4Step(t, m, b, scene)
		if a.ImmutableMaterialTable() != nil || b.materialWrites != 0 || m.VoxelMaterialsUploaded != 0 {
			t.Fatal("paused raw edit retained certification or acknowledged private material upload")
		}
		s1kReady(t, m, a, false)
		s1kReady(t, m, other, true)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{other: true})
		m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
		p4Step(t, m, b, scene)
		s1kReady(t, m, a, true)
		assertRows(t, m, scene, map[*core.VoxelObject]bool{a: true, other: true})
		if p4Offset(t, m, a) == p4Offset(t, m, other) || binary.LittleEndian.Uint32(p4Bytes(t, m, b, a)[64+48:]) != math.Float32bits(3) || binary.LittleEndian.Uint32(p4Bytes(t, m, b, other)[64+48:]) != math.Float32bits(2) {
			t.Fatal("resumed private material upload failed to isolate current content")
		}
	})
	t.Run("legacy manual allocation remains compatible", func(t *testing.T) {
		m, scene := s3bFixture(1)
		// Legacy/manual attachments have no immutable certification or GPU
		// generation acknowledgement; their existing publication stays valid.
		assertRows(t, m, scene, map[*core.VoxelObject]bool{scene.Objects[0]: true})
	})
}
