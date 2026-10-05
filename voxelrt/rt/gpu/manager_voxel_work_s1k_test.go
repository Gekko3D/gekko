package gpu

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// The production update, admission, upload executor, lookup writer and retirement
// run unchanged. Only native operations use byte arrays instead of a GPU queue.
type s1kNative struct {
	t                                       *testing.T
	buffers                                 map[*wgpu.Buffer][]byte
	labels                                  map[*wgpu.Buffer]string
	payload                                 []byte
	createdBytes, copiedBytes, contentBytes uint64
	creates, payloadWrites                  uint32
	failCreate, failCopy                    bool
	failCreateAfter                         uint32
}

func (b *s1kNative) BufferSize(buffer *wgpu.Buffer) uint64 { return uint64(len(b.buffers[buffer])) }
func (b *s1kNative) Limits() (uint64, uint64, uint64)      { return 1 << 24, 1 << 24, 256 }
func (b *s1kNative) CreateBuffer(label string, size uint64, uniform bool) (*wgpu.Buffer, error) {
	if b.failCreate && b.creates >= b.failCreateAfter {
		return nil, errors.New("injected S1k creation failure")
	}
	if size == 0 || size > 1<<24 || size%4 != 0 || (uniform && size > 256) {
		b.t.Fatalf("invalid native buffer creation %s: %d bytes", label, size)
	}
	buffer := new(wgpu.Buffer)
	b.buffers[buffer], b.labels[buffer] = make([]byte, size), label
	b.createdBytes += size
	b.creates++
	return buffer, nil
}
func (b *s1kNative) CopyBuffers(copies []voxelBufferCopy) error {
	if b.failCopy {
		return errors.New("injected S1k copy failure")
	}
	for _, c := range copies {
		src, dst := b.buffers[c.src], b.buffers[c.dst]
		if c.size == 0 || c.size%4 != 0 || c.srcOffset%4 != 0 || c.dstOffset%4 != 0 ||
			c.srcOffset > uint64(len(src)) || c.size > uint64(len(src))-c.srcOffset ||
			c.dstOffset > uint64(len(dst)) || c.size > uint64(len(dst))-c.dstOffset {
			b.t.Fatalf("invalid native copy: %s[%d:+%d] to %s[%d]", b.labels[c.src], c.srcOffset, c.size, b.labels[c.dst], c.dstOffset)
		}
		copy(dst[c.dstOffset:c.dstOffset+c.size], src[c.srcOffset:c.srcOffset+c.size])
		b.copiedBytes += c.size
	}
	return nil
}
func (b *s1kNative) WriteBuffer(buffer *wgpu.Buffer, offset uint64, data []byte) error {
	dst, exists := b.buffers[buffer]
	if !exists || offset%4 != 0 || len(data)%4 != 0 || offset > uint64(len(dst)) || uint64(len(data)) > uint64(len(dst))-offset {
		b.t.Fatalf("invalid native write: %s[%d:+%d], capacity %d", b.labels[buffer], offset, len(data), len(dst))
	}
	copy(dst[offset:offset+uint64(len(data))], data)
	switch b.labels[buffer] {
	case "SectorGridBuf", "DirectSectorLookupBuf", "SectorGridParamsBuf":
	default:
		b.contentBytes += uint64(len(data))
	}
	return nil
}
func (b *s1kNative) WritePayload(page uint32, origin [3]uint32, data []byte) error {
	if page != 0 || len(data) != payloadBytesPerBrick || origin[0]+8 > 16 || origin[1]+8 > 16 || origin[2]+8 > 16 {
		b.t.Fatalf("invalid payload write: page %d origin %v bytes %d", page, origin, len(data))
	}
	for z := uint32(0); z < 8; z++ {
		for y := uint32(0); y < 8; y++ {
			start := origin[0] + (origin[1]+y)*16 + (origin[2]+z)*16*16
			copy(b.payload[start:start+8], data[(z*8+y)*8:(z*8+y+1)*8])
		}
	}
	b.payloadWrites++
	b.contentBytes += uint64(len(data))
	return nil
}
func (b *s1kNative) ReleaseBuffer(buffer *wgpu.Buffer) {
	if _, exists := b.buffers[buffer]; !exists {
		b.t.Fatal("native buffer released twice")
	}
	delete(b.buffers, buffer)
	delete(b.labels, buffer)
}
func (b *s1kNative) reset() {
	b.createdBytes, b.copiedBytes, b.contentBytes, b.creates, b.payloadWrites = 0, 0, 0, 0, 0
}
func (b *s1kNative) liveBytes() uint64 {
	var total uint64
	for _, data := range b.buffers {
		total += uint64(len(data))
	}
	return total
}
func s1kPublished(m *GpuBufferManager) [7]*wgpu.Buffer {
	return [7]*wgpu.Buffer{m.SectorTableBuf, m.BrickTableBuf, m.DenseOccupancyBuf, m.MaterialBuf, m.SectorGridBuf, m.DirectSectorLookupBuf, m.SectorGridParamsBuf}
}
func s1kFixture(t *testing.T) (*GpuBufferManager, *s1kNative, *core.Scene, *core.VoxelObject) {
	t.Helper()
	m := s1iManager()
	m.VoxelPayloadPageSize, m.VoxelPayloadBricks, m.VoxelPayloadPageCount = 16, 2, 1
	m.VoxelPayloadTex[0] = new(wgpu.Texture)
	b := &s1kNative{t: t, buffers: make(map[*wgpu.Buffer][]byte), labels: make(map[*wgpu.Buffer]string), payload: make([]byte, 16*16*16)}
	fields := [7]**wgpu.Buffer{&m.SectorTableBuf, &m.BrickTableBuf, &m.DenseOccupancyBuf, &m.MaterialBuf, &m.SectorGridBuf, &m.DirectSectorLookupBuf, &m.SectorGridParamsBuf}
	labels := [7]string{"SectorTableBuf", "BrickTableBuf", "DenseOccupancyBuf", "MaterialBuf", "SectorGridBuf", "DirectSectorLookupBuf", "SectorGridParamsBuf"}
	sizes := [7]uint64{256, 128 * BrickRecordSize, 128 * VoxelAuxRecordBytes, materialBlockCapacity * 64, 1024 * 32, 256, 256}
	for i, field := range fields {
		buffer, err := b.CreateBuffer(labels[i], sizes[i], i == 6)
		if err != nil {
			t.Fatal(err)
		}
		*field = buffer
	}
	resident := s1iObject(10, 2, false)
	schedulePutBrick(resident, [6]int{}, scheduleBrick("mixed"))
	scene := &core.Scene{Objects: []*core.VoxelObject{resident}}
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 4096, MaxBricks: 1 << 20})
	s1kStep(t, m, b, scene)
	if ready, _, _ := m.RenderVoxelObjectReady(resident, resident.XBrickMap, resident.XBrickMap.Revision); !ready {
		t.Fatal("resident fixture not ready")
	}
	return m, b, scene, resident
}
func s1kStep(t *testing.T, m *GpuBufferManager, b *s1kNative, scene *core.Scene) bool {
	t.Helper()
	b.reset()
	recreated := m.updateVoxelData(scene, b)
	m.updateSectorGrid(scene)
	work := m.VoxelGPUWorkStats()
	if work.CreatedBytes != b.createdBytes || work.Creates != b.creates || work.CopiedBytes != b.copiedBytes {
		t.Fatalf("work stats %+v disagree with native work: created=%d creates=%d copied=%d", work, b.createdBytes, b.creates, b.copiedBytes)
	}
	if m.VoxelUploadBytes != b.contentBytes || b.contentBytes > m.VoxelUploadBudget().MaxBytes {
		t.Fatalf("upload accounting=%d actual=%d cap=%d", m.VoxelUploadBytes, b.contentBytes, m.VoxelUploadBudget().MaxBytes)
	}
	s1kCharge(t, m, b)
	return recreated
}
func s1kCharge(t *testing.T, m *GpuBufferManager, b *s1kNative) {
	t.Helper()
	stats := m.VoxelGPUAdmissionStats()
	var current uint64
	for _, buffer := range s1kPublished(m) {
		current += b.BufferSize(buffer)
	}
	if stats.CurrentBufferBytes != current || stats.AtlasBytes != uint64(len(b.payload)) ||
		stats.TotalBytes != b.liveBytes()+uint64(len(b.payload)) ||
		stats.TotalBytes != stats.CurrentBufferBytes+stats.StagingBytes+stats.RetiredBufferBytes+stats.AtlasBytes {
		t.Fatalf("physical charge %+v; current=%d native live=%d atlas=%d", stats, current, b.liveBytes(), len(b.payload))
	}
	if stats.MaxBytes != 0 && stats.PressureBytes != stats.TotalBytes-min(stats.TotalBytes, stats.MaxBytes) {
		t.Fatalf("wrong pressure: %+v", stats)
	}
}
func s1kReady(t *testing.T, m *GpuBufferManager, obj *core.VoxelObject, want bool) {
	t.Helper()
	if ready, _, _ := m.RenderVoxelObjectReady(obj, obj.RenderVoxelMap(), obj.RenderVoxelMap().Revision); ready != want {
		t.Fatalf("object %d ready=%t want %t", obj.XBrickMap.ID, ready, want)
	}
}
func s1kFinish(t *testing.T, m *GpuBufferManager, b *s1kNative, scene *core.Scene) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		s1kStep(t, m, b, scene)
		if !m.VoxelGPUWorkStats().Pending {
			return
		}
	}
	t.Fatal("fitting frozen migration did not finish within 1000 updates")
}
func s1kArrival(scene *core.Scene, id uint32, sectors int, optional bool) *core.VoxelObject {
	obj := s1iObject(id, sectors, optional)
	scene.Objects = append(scene.Objects, obj)
	return obj
}

func TestS1kDefaultAndDisabledBudgetCompleteGrowthInSameUpdate(t *testing.T) {
	for _, budget := range []VoxelGPUWorkBudget{{}, {MaxCreates: 1, MaxCreateBytes: 1, MaxCopyBytes: 1}} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			m, b, scene, resident := s1kFixture(t)
			if got := m.VoxelGPUWorkBudget(); got != (VoxelGPUWorkBudget{}) {
				t.Fatalf("default budget=%+v", got)
			}
			m.SetVoxelGPUWorkBudget(budget)
			if got := m.VoxelGPUWorkBudget(); got != budget {
				t.Fatalf("budget readback=%+v want %+v", got, budget)
			}
			arrival := s1kArrival(scene, 20, 2, false)
			before := s1kPublished(m)
			if !s1kStep(t, m, b, scene) || s1kPublished(m) == before {
				t.Fatal("unlimited growth did not publish in same update")
			}
			if work := m.VoxelGPUWorkStats(); work.Pending || work.Creates == 0 || work.CopiedBytes == 0 || work.OversizedCreates != 0 {
				t.Fatalf("unlimited work=%+v", work)
			}
			s1kReady(t, m, resident, true)
			s1kReady(t, m, arrival, true)
		})
	}
}

func TestS1kBudgetPausesCreationAndAlignedCopies(t *testing.T) {
	for _, budget := range []VoxelGPUWorkBudget{
		{Enabled: true, MaxCreates: 0, MaxCreateBytes: 1 << 30, MaxCopyBytes: 1 << 30},
		{Enabled: true, MaxCreates: 7, MaxCreateBytes: 0, MaxCopyBytes: 1 << 30},
	} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			m, b, scene, resident := s1kFixture(t)
			arrival := s1kArrival(scene, 20, 2, false)
			m.SetVoxelGPUWorkBudget(budget)
			before := s1kPublished(m)
			for i := 0; i < 3; i++ {
				if s1kStep(t, m, b, scene) || s1kPublished(m) != before {
					t.Fatal("paused creation published resources")
				}
				if w := m.VoxelGPUWorkStats(); !w.Pending || w.Creates != 0 || w.CreatedBytes != 0 || w.CopiedBytes != 0 {
					t.Fatalf("creation pause work=%+v", w)
				}
				s1kReady(t, m, resident, true)
				s1kReady(t, m, arrival, false)
				if !arrival.XBrickMap.StructureDirty || m.Allocations[arrival.XBrickMap] != nil || m.MaterialAllocations[arrival] != nil {
					t.Fatal("paused arrival acquired ownership or lost dirty authority")
				}
			}
		})
	}
	for _, cap := range []uint64{0, 1, 2, 3, 7, 4103} {
		t.Run(fmt.Sprintf("copy %d", cap), func(t *testing.T) {
			m, b, scene, resident := s1kFixture(t)
			arrival := s1kArrival(scene, 20, 2, false)
			before := s1kPublished(m)
			m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: cap})
			if s1kStep(t, m, b, scene) || s1kPublished(m) != before {
				t.Fatal("partial migration published resources")
			}
			w := m.VoxelGPUWorkStats()
			if !w.Pending || w.CopiedBytes > cap || w.CopiedBytes%4 != 0 || (cap < 4 && w.CopiedBytes != 0) || (cap >= 4 && w.CopiedBytes == 0) {
				t.Fatalf("aligned copy cap=%d work=%+v", cap, w)
			}
			s1kReady(t, m, resident, true)
			s1kReady(t, m, arrival, false)
			// Disabling limits must finish this existing stage, not abandon it.
			m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{})
			if !s1kStep(t, m, b, scene) || m.VoxelGPUWorkStats().Pending {
				t.Fatal("disabling budget did not finish accepted stage")
			}
			s1kReady(t, m, arrival, true)
		})
	}
}

func TestS1kOversizedCreationIsSoleCreationAndCountsStagingOnce(t *testing.T) {
	m, b, scene, resident := s1kFixture(t)
	arrival := s1kArrival(scene, 20, 129, true)
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1, MaxCopyBytes: 0})
	before := s1kPublished(m)
	for i := 0; i < 7; i++ {
		s1kStep(t, m, b, scene)
		w := m.VoxelGPUWorkStats()
		if s1kPublished(m) != before || !w.Pending || w.Creates > 1 {
			t.Fatalf("oversized creation published early or batched: %+v", w)
		}
		if w.Creates == 1 && (w.OversizedCreates != 1 || w.CreatedBytes <= 1) {
			t.Fatalf("oversized creation not reported: %+v", w)
		}
		if w.Creates == 0 && w.OversizedCreates != 0 {
			t.Fatalf("false oversized diagnostic: %+v", w)
		}
		s1kReady(t, m, resident, true)
		s1kReady(t, m, arrival, false)
	}
	if stats := m.VoxelGPUAdmissionStats(); stats.StagingBytes == 0 || stats.AllocationFailures != 0 {
		t.Fatalf("accepted staging charge/failure=%+v", stats)
	}
	// Already accepted capacity keeps its reservation after a soft-cap reduction.
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1, MaxCopyBytes: 65536})
	s1kFinish(t, m, b, scene)
	s1kReady(t, m, arrival, true)
	if stats := m.VoxelGPUAdmissionStats(); stats.StagingBytes != 0 || stats.RetiredBufferBytes == 0 || stats.AllocationFailures != 0 {
		t.Fatalf("finished stage accounting=%+v", stats)
	}
}

func TestS1kNormalCreationBoundaryAndOversizedContinuation(t *testing.T) {
	// Measure one normal indivisible creation without assuming a capacity formula.
	calibration, native, calibrationScene, _ := s1kFixture(t)
	s1kArrival(calibrationScene, 20, 129, false)
	calibration.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30})
	s1kStep(t, calibration, native, calibrationScene)
	first := calibration.VoxelGPUWorkStats()
	if first.Creates != 1 || first.OversizedCreates != 0 || !first.Pending {
		t.Fatalf("one normal creation boundary=%+v", first)
	}
	m, b, scene, _ := s1kFixture(t)
	s1kArrival(scene, 20, 129, false)
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: first.CreatedBytes})
	s1kStep(t, m, b, scene)
	w := m.VoxelGPUWorkStats()
	if w.Creates != 1 || w.CreatedBytes != first.CreatedBytes || w.OversizedCreates != 0 || !w.Pending {
		t.Fatalf("exact fitting byte boundary allowed oversized follow-on: %+v", w)
	}
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1})
	s1kStep(t, m, b, scene)
	w = m.VoxelGPUWorkStats()
	if w.Creates != 1 || w.OversizedCreates != 1 || w.CreatedBytes <= 1 || !w.Pending {
		t.Fatalf("deferred oversized creation was not sole next creation: %+v", w)
	}
}

func TestS1kOptionalStageFitsExactAcceptedPeak(t *testing.T) {
	calibration, native, calibrationScene, _ := s1kFixture(t)
	s1kArrival(calibrationScene, 20, 129, true)
	s1kStep(t, calibration, native, calibrationScene)
	peak := calibration.VoxelGPUAdmissionStats().TotalBytes
	m, b, scene, _ := s1kFixture(t)
	arrival := s1kArrival(scene, 20, 129, true)
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: peak})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 65536})
	s1kStep(t, m, b, scene)
	if w := m.VoxelGPUWorkStats(); !w.Pending || w.Creates != 1 {
		t.Fatalf("exact peak wrongly refused stage start: %+v", w)
	}
	s1kFinish(t, m, b, scene)
	s1kReady(t, m, arrival, true)
	if stats := m.VoxelGPUAdmissionStats(); stats.TotalBytes != peak || stats.PressureBytes != 0 {
		t.Fatalf("exact peak stage charge=%+v want %d", stats, peak)
	}
}

func TestS1kAcknowledgedEditsSurvivePublicationWhileUploadsPaused(t *testing.T) {
	m, b, scene, resident := s1kFixture(t)
	secondKey := [6]int{0, 0, 0, 1, 0, 0}
	schedulePutBrick(resident, secondKey, scheduleBrick("mixed"))
	resident.XBrickMap.DirtyBricks[secondKey] = true
	s1kStep(t, m, b, scene)
	before, generation := s1kPublished(m), m.MaterialBufferGeneration
	// This arrival grows all six storage buffers. Copy all their old bytes except
	// four tail bytes, ensuring resident records and lookup prefixes are copied.
	arrival := s1kArrival(scene, 20, 129, false)
	// Auxiliary capacity follows occupied bricks. Populate sparse uniform
	// records so the intended six-buffer migration still includes auxiliary
	// copies and mirrors without requiring additional payload atlas slots.
	for i := 1; i < 129; i++ {
		schedulePutBrick(arrival, [6]int{i, 0, 0, 0, 0, 0}, scheduleBrick("uniform"))
	}
	var oldStorageBytes uint64
	for _, buffer := range before[:6] {
		oldStorageBytes += b.BufferSize(buffer)
	}
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: oldStorageBytes - 4})
	s1kStep(t, m, b, scene)
	if w := m.VoxelGPUWorkStats(); !w.Pending || w.CopiedBytes != oldStorageBytes-4 {
		t.Fatalf("near-complete migration fixture=%+v", w)
	}
	// Cancel arriving logical demand and stabilize a fitting current-owner edit.
	// Publication then has no new lookup topology that could repair a lost mirror.
	scene.Objects = []*core.VoxelObject{resident}
	resident.MaterialTable = []core.Material{core.DefaultMaterial()}
	resident.MaterialTable[0].Roughness = 0.375
	brick := scheduleBrick("mixed")
	brick.PrecomputedAux[0] = 0x9d
	schedulePutBrick(resident, [6]int{}, brick)
	resident.XBrickMap.DirtyBricks[[6]int{}] = true
	schedulePutBrick(resident, secondKey, nil)
	resident.XBrickMap.DirtyBricks[secondKey] = true
	delete(resident.XBrickMap.Sectors, [3]int{1, 0, 0})
	resident.XBrickMap.StructureDirty = true
	scene.StructureRevision++
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
	s1kStep(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending || len(resident.XBrickMap.DirtyBricks) != 0 || len(resident.XBrickMap.DirtySectors) != 0 || b.payloadWrites != 1 {
		t.Fatal("stationary edit did not complete while old generation remained published")
	}
	s1kReady(t, m, resident, true)
	s1kAssertResidentBytes(t, m, b, scene, resident)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 0, MaxSectors: 4096, MaxBricks: 1 << 20})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 4})
	if !s1kStep(t, m, b, scene) || m.VoxelGPUWorkStats().Pending || b.copiedBytes != 4 || b.contentBytes != 0 || m.MaterialBufferGeneration != generation+1 {
		t.Fatal("remaining copy did not publish without content rewriting")
	}
	s1kAssertResidentBytes(t, m, b, scene, resident)
	info := m.SectorToInfo[resident.XBrickMap.Sectors[[3]int{}]]
	clearOffset := uint64(info.BrickTableIndex+1) * BrickRecordSize
	if !bytes.Equal(b.buffers[m.BrickTableBuf][clearOffset:clearOffset+BrickRecordSize], make([]byte, BrickRecordSize)) {
		t.Fatal("already acknowledged empty-brick clear was lost at publication")
	}
	// Established material-generation invalidation is allowed while uploads pause.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 4096, MaxBricks: 1 << 20})
	s1kStep(t, m, b, scene)
	s1kReady(t, m, resident, true)
}

func TestS1kContinuousEditsAndMaterialsReachPublishedBytes(t *testing.T) {
	m, b, scene, resident := s1kFixture(t)
	arrival := s1kArrival(scene, 20, 129, false)
	before, generation := s1kPublished(m), m.MaterialBufferGeneration
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
	s1kStep(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending {
		t.Fatal("fixture failed to establish staged migration")
	}
	// This material write fits once, but cannot fit both current and staging.
	resident.MaterialTable = []core.Material{core.DefaultMaterial()}
	resident.MaterialTable[0].Roughness = 0.125
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 64, MaxSectors: 4096, MaxBricks: 1 << 20})
	s1kStep(t, m, b, scene)
	if m.VoxelMaterialsUploaded != 0 || b.contentBytes != 0 {
		t.Fatal("mirror write bypassed global content byte cap")
	}
	s1kReady(t, m, resident, false)
	// Permit exactly both copies of the one material row.
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 128, MaxSectors: 4096, MaxBricks: 1 << 20})
	s1kStep(t, m, b, scene)
	if m.VoxelMaterialsUploaded != 1 || b.contentBytes != 128 {
		t.Fatal("material mirror cost or completion incorrect")
	}
	s1kReady(t, m, resident, true)
	m.SetVoxelUploadBudget(VoxelUploadBudget{MaxBytes: 1 << 30, MaxSectors: 4096, MaxBricks: 1 << 20})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 65536})
	published := false
	for frame := 0; frame < 100; frame++ {
		resident.MaterialTable = []core.Material{core.DefaultMaterial()}
		resident.MaterialTable[0].Roughness = float32(frame+1) / 128
		// Replace one mixed brick, then clear it, repeatedly while copies progress.
		var brick *volume.Brick
		if frame%2 == 0 {
			brick = scheduleBrick("mixed")
			brick.PrecomputedAux[0] = byte(frame + 1)
		}
		schedulePutBrick(resident, [6]int{}, brick)
		resident.XBrickMap.DirtyBricks[[6]int{}] = true
		if frame == 1 {
			// Lookup changes fitting current capacity must also reach staging.
			delete(resident.XBrickMap.Sectors, [3]int{1, 0, 0})
			resident.XBrickMap.StructureDirty = true
			scene.StructureRevision++
		}
		changed := s1kStep(t, m, b, scene)
		wantPayloadWrites := uint32(0)
		if brick != nil {
			wantPayloadWrites = 1
		}
		if b.payloadWrites != wantPayloadWrites {
			t.Fatalf("payload writes=%d want %d; staging buffers must not duplicate atlas writes", b.payloadWrites, wantPayloadWrites)
		}
		w := m.VoxelGPUWorkStats()
		if w.CopiedBytes > 65536 || w.Creates > 1 {
			t.Fatalf("bounded continuation exceeded work limits: %+v", w)
		}
		if w.Pending {
			if changed || s1kPublished(m) != before || m.MaterialBufferGeneration != generation {
				t.Fatal("partial migration changed published generation")
			}
			s1kReady(t, m, resident, true)
			s1kReady(t, m, arrival, false)
		} else {
			if !changed || s1kPublished(m) == before || m.MaterialBufferGeneration != generation+1 {
				t.Fatal("completed migration did not publish once")
			}
			published = true
			break
		}
	}
	if !published {
		t.Fatal("continuous edits prevented finite migration completion")
	}
	s1kAssertResidentBytes(t, m, b, scene, resident)
	s1kReady(t, m, arrival, true)
	// Publication is a single recreation event; idle updates do not republish.
	if s1kStep(t, m, b, scene) || m.MaterialBufferGeneration != generation+1 {
		t.Fatal("idle update republished completed stage")
	}
}

func s1kAssertResidentBytes(t *testing.T, m *GpuBufferManager, b *s1kNative, scene *core.Scene, obj *core.VoxelObject) {
	t.Helper()
	mat := m.MaterialAllocations[obj]
	wantMaterial := buildMaterialData(obj.MaterialTable)
	start := uint64(mat.MaterialOffset) * 64
	if !bytes.Equal(b.buffers[m.MaterialBuf][start:start+uint64(len(wantMaterial))], wantMaterial) {
		t.Fatal("published material bytes lost latest animation")
	}
	sector := obj.XBrickMap.Sectors[[3]int{}]
	info := m.SectorToInfo[sector]
	brick := sector.GetBrick(0, 0, 0)
	wantRecord := make([]byte, BrickRecordSize)
	if brick != nil {
		mode := resolveBrickUploadMode(brick.Flags)
		slot := m.BrickToSlot[brick]
		ax, ay, az := (slot.Slot%2)*8, ((slot.Slot/2)%2)*8, (slot.Slot/4)*8
		wantRecord = encodeGpuBrickRecord(buildGpuBrickRecord(brick, mode, packVoxelAtlasOffset(ax, ay, az), slot.Page, voxelAuxWordBase(m.BrickToAuxSlot[brick])))
		auxStart := uint64(m.BrickToAuxSlot[brick]) * VoxelAuxRecordBytes
		if !bytes.Equal(b.buffers[m.DenseOccupancyBuf][auxStart:auxStart+VoxelAuxRecordBytes], brick.PrecomputedAux) {
			t.Fatal("published auxiliary bytes lost latest edit")
		}
		if b.payload[ax+ay*16+az*16*16] != brick.VoxelValue(0, 0, 0) {
			t.Fatal("published payload lost latest edit")
		}
	}
	start = uint64(info.BrickTableIndex) * BrickRecordSize
	if !bytes.Equal(b.buffers[m.BrickTableBuf][start:start+BrickRecordSize], wantRecord) {
		t.Fatal("published brick bytes lost latest replacement/clear")
	}
	wantGrid, gridSize := m.buildSectorGridData(scene)
	if !bytes.Equal(b.buffers[m.SectorGridBuf][:len(wantGrid)], wantGrid) {
		t.Fatal("published hash lookup differs from live admitted ownership")
	}
	wantDirect := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 0)
	if !bytes.Equal(b.buffers[m.DirectSectorLookupBuf][:len(wantDirect)], wantDirect) {
		t.Fatal("published direct lookup differs from live admitted ownership")
	}
	params := b.buffers[m.SectorGridParamsBuf]
	if binary.LittleEndian.Uint32(params) != gridSize || binary.LittleEndian.Uint32(params[4:]) != gridSize-1 {
		t.Fatal("published hash parameters stale")
	}
}

func TestS1kFrozenCapacityDoesNotPublishRemovedOrChangedDemand(t *testing.T) {
	m, b, scene, resident := s1kFixture(t)
	removed := s1kArrival(scene, 20, 2, true)
	originalMap := removed.XBrickMap
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1 << 30, MaxCopyBytes: 0})
	s1kStep(t, m, b, scene)
	if !m.VoxelGPUWorkStats().Pending {
		t.Fatal("fixture failed to start accepted stage")
	}
	// Remove captured demand; replacement demand exceeds the frozen capacities.
	changedMap := s1iObject(30, 300, true)
	removed.XBrickMap = changedMap.XBrickMap
	scene.Objects = []*core.VoxelObject{resident, removed}
	m.SetVoxelGPUAdmissionBudget(VoxelGPUAdmissionBudget{MaxBytes: 1})
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 1, MaxCreateBytes: 1, MaxCopyBytes: 65536})
	s1kFinish(t, m, b, scene)
	if m.MaterialAllocations[removed] != nil || m.Allocations[originalMap] != nil || m.Allocations[changedMap.XBrickMap] != nil || !originalMap.StructureDirty || !changedMap.XBrickMap.StructureDirty {
		t.Fatal("stale stage acquired detached logical ownership")
	}
	s1kReady(t, m, resident, true)
	// A fresh live admission must defer larger new optional demand under pressure.
	s1kStep(t, m, b, scene)
	if m.VoxelGPUWorkStats().Pending || m.MaterialAllocations[removed] != nil || m.Allocations[removed.XBrickMap] != nil {
		t.Fatal("frozen plan admitted changed demand without fresh preflight")
	}
	s1kReady(t, m, removed, false)
	if m.VoxelGPUAdmissionStats().DeferredMaps == 0 {
		t.Fatal("new live optional deferral not exposed")
	}
}

func TestS1kFailurePreservesCurrentAuthorityAndRetiresUsedStaging(t *testing.T) {
	for _, submitted := range []bool{false, true} {
		t.Run(fmt.Sprintf("used staging %t", submitted), func(t *testing.T) {
			m, b, scene, resident := s1kFixture(t)
			arrival := s1kArrival(scene, 20, 2, false)
			before, generation, liveBefore := s1kPublished(m), m.MaterialBufferGeneration, b.liveBytes()
			m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: 7, MaxCreateBytes: 1 << 30, MaxCopyBytes: 4})
			if submitted {
				s1kStep(t, m, b, scene)
				if b.copiedBytes == 0 {
					t.Fatal("fixture did not submit migration work")
				}
				b.failCopy = true
			} else {
				b.failCreate, b.failCreateAfter = true, 1
			}
			s1kStep(t, m, b, scene)
			if s1kPublished(m) != before || m.MaterialBufferGeneration != generation || !arrival.XBrickMap.StructureDirty || m.Allocations[arrival.XBrickMap] != nil || m.MaterialAllocations[arrival] != nil {
				t.Fatal("native failure published resources/ownership or acknowledged dirty authority")
			}
			s1kReady(t, m, resident, true)
			s1kReady(t, m, arrival, false)
			stats := m.VoxelGPUAdmissionStats()
			if stats.AllocationFailures == 0 || stats.LastError == "" || stats.StagingBytes != 0 {
				t.Fatalf("failure diagnostic/aborted stage=%+v", stats)
			}
			if !submitted && (stats.RetiredBufferBytes != 0 || b.liveBytes() != liveBefore) {
				t.Fatal("untouched unpublished creation remained charged")
			}
			if submitted && (stats.RetiredBufferBytes == 0 || b.liveBytes() <= liveBefore) {
				t.Fatal("submitted staging released before retirement")
			}
			// Remove failing demand so subsequent updates cannot start another stage.
			scene.Objects = []*core.VoxelObject{resident}
			b.failCreate, b.failCopy = false, false
			for i := 0; i < RetiredBufferFrameDelay; i++ {
				m.AdvanceRetiredBuffers()
			}
			s1kStep(t, m, b, scene)
			if stats := m.VoxelGPUAdmissionStats(); stats.RetiredBufferBytes != 0 || b.liveBytes() != liveBefore {
				t.Fatalf("retirement did not release native charge: %+v", stats)
			}
			// Native failure leaves the same CPU request retryable.
			scene.Objects = append(scene.Objects, arrival)
			m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{})
			s1kStep(t, m, b, scene)
			s1kReady(t, m, arrival, true)
		})
	}
}

func TestS1kNilManagerBudgetAndStatsAreZero(t *testing.T) {
	var m *GpuBufferManager
	m.SetVoxelGPUWorkBudget(VoxelGPUWorkBudget{Enabled: true, MaxCreates: math.MaxUint32})
	if m.VoxelGPUWorkBudget() != (VoxelGPUWorkBudget{}) || m.VoxelGPUWorkStats() != (VoxelGPUWorkStats{}) {
		t.Fatal("nil manager returned nonzero work configuration or stats")
	}
}
