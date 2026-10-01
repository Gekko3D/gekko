package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Seed the state left by completed queue writes without opening a GPU device.
// Assertions below concern VoxelObjectReady, not the allocation representation.
func voxelReadinessFixture(empty bool) (*GpuBufferManager, *core.VoxelObject) {
	obj := core.NewVoxelObject()
	if !empty {
		obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	}
	obj.RenderEnabled = false
	obj.MaterialTable = []core.Material{core.DefaultMaterial(), core.DefaultMaterial()}
	xbm := obj.XBrickMap
	xbm.ClearDirty()
	alloc := &ObjectGpuAllocation{Sectors: make(map[[3]int]*volume.Sector), Bricks: make(map[[3]int]*[64]*volume.Brick)}
	sectorInfo := make(map[*volume.Sector]SectorGpuInfo)
	for coord, sector := range xbm.Sectors {
		alloc.Sectors[coord] = sector
		pointers := new([64]*volume.Brick)
		for i := range pointers {
			pointers[i] = sector.GetBrick(i%4, (i/4)%4, i/16)
		}
		alloc.Bricks[coord] = pointers
		slot := uint32(len(sectorInfo))
		sectorInfo[sector] = SectorGpuInfo{SlotIndex: slot, BrickTableIndex: slot * 64}
	}
	ptr, length := materialTableIdentity(obj.MaterialTable)
	m := &GpuBufferManager{
		Allocations:  map[*volume.XBrickMap]*ObjectGpuAllocation{xbm: alloc},
		SectorToInfo: sectorInfo,
		MaterialAllocations: map[*core.VoxelObject]*MaterialGpuAllocation{obj: {
			MaterialCapacity: materialBlockCapacity, MaterialTablePtr: ptr, MaterialTableLen: length, BufferGeneration: 7,
		}},
		MaterialBufferGeneration: 7, sectorTopologyRevision: 3, lastSectorGridTopologyRevision: 3,
	}
	return m, obj
}

func TestVoxelObjectReadyRequiresCompleteCurrentUpload(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*GpuBufferManager, *core.VoxelObject)
	}{
		{"missing allocation", func(m *GpuBufferManager, o *core.VoxelObject) { delete(m.Allocations, o.XBrickMap) }},
		{"nil allocation", func(m *GpuBufferManager, o *core.VoxelObject) { m.Allocations[o.XBrickMap] = nil }},
		{"partial sector allocation", func(m *GpuBufferManager, o *core.VoxelObject) { clear(m.Allocations[o.XBrickMap].Sectors) }},
		{"partial brick pointer allocation", func(m *GpuBufferManager, o *core.VoxelObject) { clear(m.Allocations[o.XBrickMap].Bricks) }},
		{"dirty structure", func(_ *GpuBufferManager, o *core.VoxelObject) { o.XBrickMap.StructureDirty = true }},
		{"dirty sector", func(_ *GpuBufferManager, o *core.VoxelObject) { o.XBrickMap.DirtySectors[[3]int{}] = true }},
		{"dirty brick", func(_ *GpuBufferManager, o *core.VoxelObject) { o.XBrickMap.DirtyBricks[[6]int{}] = true }},
		{"missing materials", func(m *GpuBufferManager, o *core.VoxelObject) { delete(m.MaterialAllocations, o) }},
		{"nil materials", func(m *GpuBufferManager, o *core.VoxelObject) { m.MaterialAllocations[o] = nil }},
		{"stale material backing", func(_ *GpuBufferManager, o *core.VoxelObject) {
			o.MaterialTable = append([]core.Material(nil), o.MaterialTable...)
		}},
		{"stale material length", func(_ *GpuBufferManager, o *core.VoxelObject) { o.MaterialTable = o.MaterialTable[:1] }},
		{"stale material generation", func(m *GpuBufferManager, _ *core.VoxelObject) { m.MaterialBufferGeneration++ }},
		{"insufficient material capacity", func(m *GpuBufferManager, o *core.VoxelObject) { m.MaterialAllocations[o].MaterialCapacity = 1 }},
		{"stale lookup topology", func(m *GpuBufferManager, _ *core.VoxelObject) { m.sectorTopologyRevision++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, obj := voxelReadinessFixture(false)
			revision := obj.XBrickMap.Revision
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, revision); !ready {
				t.Fatal("fixture must start ready")
			}
			tc.change(m, obj)
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, revision); ready {
				t.Fatal("incomplete or stale upload reported ready")
			}
		})
	}
}

func TestVoxelObjectReadyReportsOutstandingDirtyWork(t *testing.T) {
	m, obj := voxelReadinessFixture(false)
	xbm := obj.XBrickMap
	xbm.DirtySectors[[3]int{}] = true
	xbm.DirtyBricks[[6]int{}] = true
	xbm.DirtyBricks[[6]int{0, 0, 0, 1, 0, 0}] = true
	ready, sectors, bricks := m.VoxelObjectReady(obj, xbm, xbm.Revision)
	if ready || sectors != 1 || bricks != 2 {
		t.Fatalf("ready=%v pending=%d/%d, want false and 1/2", ready, sectors, bricks)
	}
	clear(xbm.DirtySectors)
	clear(xbm.DirtyBricks)
	if ready, sectors, bricks := m.VoxelObjectReady(obj, xbm, xbm.Revision); !ready || sectors != 0 || bricks != 0 {
		t.Fatalf("clean target: ready=%v pending=%d/%d", ready, sectors, bricks)
	}
}

func TestVoxelObjectReadyQualifiesMapAndRevision(t *testing.T) {
	m, obj := voxelReadinessFixture(false)
	target, revision := obj.XBrickMap, obj.XBrickMap.Revision
	otherManager, other := voxelReadinessFixture(false)
	m.Allocations[other.XBrickMap] = otherManager.Allocations[other.XBrickMap]
	obj.XBrickMap = other.XBrickMap
	if ready, _, _ := m.VoxelObjectReady(obj, target, revision); ready {
		t.Fatal("a different map must not satisfy the captured target")
	}
	obj.XBrickMap = target
	if ready, _, _ := m.VoxelObjectReady(obj, target, revision+1); ready {
		t.Fatal("a different revision must not satisfy the captured target")
	}
	target.SetVoxel(1, 0, 0, 1)
	target.ClearDirty()
	if ready, _, _ := m.VoxelObjectReady(obj, target, revision); ready {
		t.Fatal("completed later edits must not satisfy an older target")
	}
}

func TestVoxelObjectReadyEmptyAndSharedGeometry(t *testing.T) {
	t.Run("empty map still requires allocation and materials", func(t *testing.T) {
		m, obj := voxelReadinessFixture(true)
		if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
			t.Fatal("fully uploaded empty map should settle")
		}
		delete(m.Allocations, obj.XBrickMap)
		if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
			t.Fatal("empty map cannot bypass renderer allocation")
		}
	})
	t.Run("shared map has object-specific materials", func(t *testing.T) {
		m, first := voxelReadinessFixture(false)
		second := core.NewVoxelObject()
		second.XBrickMap = first.XBrickMap
		second.MaterialTable = append([]core.Material(nil), first.MaterialTable...)
		if ready, _, _ := m.VoxelObjectReady(second, second.XBrickMap, second.XBrickMap.Revision); ready {
			t.Fatal("another object's materials cannot establish readiness")
		}
		ptr, length := materialTableIdentity(second.MaterialTable)
		m.MaterialAllocations[second] = &MaterialGpuAllocation{MaterialCapacity: materialBlockCapacity, MaterialTablePtr: ptr, MaterialTableLen: length, BufferGeneration: m.MaterialBufferGeneration}
		for _, obj := range []*core.VoxelObject{first, second} {
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
				t.Fatal("shared geometry with each material upload current should be ready")
			}
		}
	})
}

func TestVoxelObjectReadyMissingRendererState(t *testing.T) {
	_, obj := voxelReadinessFixture(false)
	for _, m := range []*GpuBufferManager{nil, {}} {
		if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
			t.Fatal("missing renderer state cannot be ready")
		}
	}
	m, obj := voxelReadinessFixture(false)
	if ready, _, _ := m.VoxelObjectReady(nil, obj.XBrickMap, obj.XBrickMap.Revision); ready {
		t.Fatal("missing object cannot be ready")
	}
	if ready, _, _ := m.VoxelObjectReady(obj, nil, obj.XBrickMap.Revision); ready {
		t.Fatal("missing target cannot be ready")
	}
}
