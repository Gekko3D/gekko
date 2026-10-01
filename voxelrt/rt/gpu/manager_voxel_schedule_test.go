package gpu

import (
	"math"
	"reflect"
	"strconv"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// These fixtures represent completed allocation/preparation state. The executor
// substitutes only for queue writes; selection and completion use the service
// called by UpdateVoxelData in production.
func scheduleObject(id uint32, coords ...[3]int) *core.VoxelObject {
	obj := core.NewVoxelObject()
	obj.XBrickMap.ID = id
	obj.MaterialTable = []core.Material{core.DefaultMaterial()}
	for _, key := range coords {
		obj.XBrickMap.Sectors[key] = volume.NewSector(key[0], key[1], key[2])
	}
	obj.XBrickMap.ClearDirty()
	return obj
}

func scheduleBrick(kind string) *volume.Brick {
	if kind == "empty" {
		return nil
	}
	b := volume.NewBrick()
	switch kind {
	case "solid":
		b.Expand(1)
	case "uniform":
		b.SetVoxel(0, 0, 0, 1)
	case "mixed":
		b.SetVoxel(0, 0, 0, 1)
		b.SetVoxel(1, 0, 0, 2)
	default:
		panic("unknown fixture brick kind")
	}
	b.RefreshMaterialFlags()
	b.PrecomputedAux = make([]byte, VoxelAuxRecordBytes)
	return b
}

func schedulePutBrick(obj *core.VoxelObject, key [6]int, b *volume.Brick) {
	s := obj.XBrickMap.Sectors[[3]int{key[0], key[1], key[2]}]
	i := key[3] + key[4]*4 + key[5]*16
	if b == nil {
		if old := s.GetBrick(key[3], key[4], key[5]); old != nil {
			old.OccupancyMask64 = 0
			s.RemoveBrickIfEmpty(key[3], key[4], key[5])
		}
	} else {
		_, _ = s.GetOrCreateBrick(key[3], key[4], key[5])
		s.PackedBricks[s.GetPackedIndex(i)] = b
	}
	obj.XBrickMap.Revision++
}

func scheduleFixture(t *testing.T, objects ...*core.VoxelObject) (*GpuBufferManager, *core.Scene) {
	t.Helper()
	m := &GpuBufferManager{
		Allocations:         make(map[*volume.XBrickMap]*ObjectGpuAllocation),
		SectorToInfo:        make(map[*volume.Sector]SectorGpuInfo),
		MaterialAllocations: make(map[*core.VoxelObject]*MaterialGpuAllocation),
		BrickToSlot:         make(map[*volume.Brick]PayloadSlot), BrickToAuxSlot: make(map[*volume.Brick]uint32),
		VoxelPayloadBricks: 16, VoxelPayloadPageCount: 1,
		MaterialBufferGeneration: 7, sectorTopologyRevision: 3, lastSectorGridTopologyRevision: 3,
	}
	scene := &core.Scene{Objects: objects}
	for _, obj := range objects {
		if obj == nil || obj.XBrickMap == nil {
			continue
		}
		xbm := obj.XBrickMap
		if m.Allocations[xbm] != nil {
			continue
		}
		alloc := &ObjectGpuAllocation{Sectors: make(map[[3]int]*volume.Sector), Bricks: make(map[[3]int]*[64]*volume.Brick)}
		m.Allocations[xbm] = alloc
		for key, sector := range xbm.Sectors {
			alloc.Sectors[key] = sector
			pointers := new([64]*volume.Brick)
			for i := range pointers {
				b := sector.GetBrick(i%4, (i/4)%4, i/16)
				pointers[i] = b
				if b == nil {
					continue
				}
				m.BrickToAuxSlot[b] = m.VoxelAuxAlloc.Alloc()
				if resolveBrickUploadMode(b.Flags).usesPayload {
					slot, ok := m.allocPayloadSlot()
					if !ok {
						t.Fatal("fixture payload capacity exhausted")
					}
					m.BrickToSlot[b] = slot
				}
			}
			alloc.Bricks[key] = pointers
			slot := m.SectorAlloc.Alloc()
			table := m.BrickAlloc.Alloc()
			m.SectorToInfo[sector] = SectorGpuInfo{SlotIndex: slot, BrickTableIndex: table * 64}
		}
	}
	m.SetVoxelUploadBudget(DefaultVoxelUploadBudget())
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		if w.kind != voxelUploadMaterial {
			t.Fatalf("clean fixture requested geometry work: %+v", w)
		}
		return true
	})
	for _, obj := range objects {
		if obj != nil && obj.XBrickMap != nil {
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
				t.Fatal("completed fixture must be ready")
			}
		}
	}
	return m, scene
}

func scheduleRun(t *testing.T, m *GpuBufferManager, scene *core.Scene) []voxelUploadWork {
	t.Helper()
	var work []voxelUploadWork
	var bytes uint64
	var sectors, bricks uint32
	materials := 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		work = append(work, w)
		bytes += w.bytes
		sectors += w.sectors
		bricks += w.bricks
		if w.kind == voxelUploadMaterial {
			materials++
		}
		return true
	})
	budget := m.VoxelUploadBudget()
	if bytes > budget.MaxBytes || sectors > budget.MaxSectors || bricks > budget.MaxBricks {
		t.Fatalf("global cap exceeded: emitted %d/%d/%d budget %+v", bytes, sectors, bricks, budget)
	}
	if m.VoxelUploadBytes != bytes || m.VoxelSectorsUploaded != int(sectors) || m.VoxelBricksUploaded != int(bricks) || m.VoxelMaterialsUploaded != materials {
		t.Fatalf("counters %d/%d/%d/%d differ from executed work %d/%d/%d/%d", m.VoxelUploadBytes, m.VoxelSectorsUploaded, m.VoxelBricksUploaded, m.VoxelMaterialsUploaded, bytes, sectors, bricks, materials)
	}
	return work
}

func TestVoxelUploadBudgetConfiguration(t *testing.T) {
	want := VoxelUploadBudget{MaxBytes: 4 * 1024 * 1024, MaxSectors: MaxUpdatesPerFrame, MaxBricks: 65536}
	if got := DefaultVoxelUploadBudget(); got != want {
		t.Fatalf("default = %+v, want %+v", got, want)
	}
	m := &GpuBufferManager{}
	for _, budget := range []VoxelUploadBudget{want, {}, {MaxBytes: math.MaxUint64, MaxSectors: math.MaxUint32, MaxBricks: math.MaxUint32}, {MaxBytes: 4 * 1024 * 1024, MaxSectors: 16, MaxBricks: 1024}} {
		m.SetVoxelUploadBudget(budget)
		if got := m.VoxelUploadBudget(); got != budget {
			t.Fatalf("round trip %+v != %+v", got, budget)
		}
		if m.SectorsPerFrame != budget.MaxSectors || m.VoxelUploadBytesPerFrame != budget.MaxBytes || m.VoxelUploadBricksPerFrame != budget.MaxBricks {
			t.Fatal("public legacy/config fields must agree with budget")
		}
	}
	m.SectorsPerFrame = 9
	if m.VoxelUploadBudget().MaxSectors != 9 {
		t.Fatal("existing SectorsPerFrame callers must still configure the global sector cap")
	}
}

func TestVoxelUploadGlobalLimits(t *testing.T) {
	// Each empty sector still writes a header and all 64 cleared records.
	for _, tc := range []struct {
		name   string
		budget VoxelUploadBudget
		count  int
	}{
		{"exact bytes", VoxelUploadBudget{4160, 9, 999}, 2},
		{"one byte short", VoxelUploadBudget{4159, 9, 999}, 1},
		{"sector global", VoxelUploadBudget{math.MaxUint64, 2, 999}, 2},
		{"brick records global", VoxelUploadBudget{math.MaxUint64, 9, 127}, 1},
		{"zero bytes", VoxelUploadBudget{0, 9, 999}, 0},
		{"zero sectors", VoxelUploadBudget{99999, 0, 999}, 0},
		{"zero bricks", VoxelUploadBudget{99999, 9, 0}, 0},
		{"numeric maxima", VoxelUploadBudget{math.MaxUint64, math.MaxUint32, math.MaxUint32}, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := []*core.VoxelObject{scheduleObject(1, [3]int{}), scheduleObject(2, [3]int{}), scheduleObject(3, [3]int{}), scheduleObject(4, [3]int{})}
			m, scene := scheduleFixture(t, objects...)
			for _, obj := range objects {
				obj.XBrickMap.DirtySectors[[3]int{}] = true
			}
			m.SetVoxelUploadBudget(tc.budget)
			if work := scheduleRun(t, m, scene); len(work) != tc.count {
				t.Fatalf("executed %d units, want %d", len(work), tc.count)
			}
			if m.VoxelDirtySectorsPending != 4-tc.count {
				t.Fatalf("pending = %d, want %d", m.VoxelDirtySectorsPending, 4-tc.count)
			}
			if work := scheduleRun(t, m, scene); len(work) != min(tc.count, 4-tc.count) {
				t.Fatalf("next frame did not receive fresh budget: %d units", len(work))
			}
		})
	}
}

func TestVoxelUploadSectorAndStandaloneShareBrickLimit(t *testing.T) {
	sector := scheduleObject(1, [3]int{})
	standalone := scheduleObject(2, [3]int{})
	m, scene := scheduleFixture(t, sector, standalone)
	sector.XBrickMap.DirtySectors[[3]int{}] = true
	standalone.XBrickMap.DirtyBricks[[6]int{}] = true
	m.SetVoxelUploadBudget(VoxelUploadBudget{99999, 1, 64})
	if work := scheduleRun(t, m, scene); len(work) != 1 || work[0].kind != voxelUploadSector {
		t.Fatalf("sector frame: %+v", work)
	}
	if !standalone.XBrickMap.DirtyBricks[[6]int{}] {
		t.Fatal("standalone brick bypassed global record cap")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{32, 0, 1})
	if work := scheduleRun(t, m, scene); len(work) != 1 || work[0].kind != voxelUploadBrick {
		t.Fatalf("zero sector limit must allow standalone writes: %+v", work)
	}
}

func TestVoxelUploadExactCosts(t *testing.T) {
	for _, tc := range []struct {
		kind  string
		bytes uint64
	}{{"empty", 32}, {"solid", 1120}, {"uniform", 1120}, {"mixed", 1632}} {
		t.Run("brick/"+tc.kind, func(t *testing.T) {
			obj := scheduleObject(1, [3]int{})
			b := scheduleBrick(tc.kind)
			schedulePutBrick(obj, [6]int{}, b)
			m, scene := scheduleFixture(t, obj)
			obj.XBrickMap.DirtyBricks[[6]int{}] = true
			m.SetVoxelUploadBudget(VoxelUploadBudget{tc.bytes - 1, 0, 1})
			if work := scheduleRun(t, m, scene); len(work) != 0 || !obj.XBrickMap.DirtyBricks[[6]int{}] {
				t.Fatal("one-byte standalone deficit must preserve dirty work without execution")
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{tc.bytes, 0, 1})
			work := scheduleRun(t, m, scene)
			if len(work) != 1 || work[0].bytes != tc.bytes || work[0].bricks != 1 {
				t.Fatalf("cost: %+v, want %d bytes and 1 record", work, tc.bytes)
			}
			// Compare against the existing writer's actual encoding/layout lengths.
			emitted := len(encodeGpuBrickRecord(gpuBrickRecord{}))
			if b != nil {
				emitted += len(b.PrecomputedAux)
				if resolveBrickUploadMode(b.Flags).usesPayload {
					emitted += len(b.Payload) * len(b.Payload[0]) * len(b.Payload[0][0])
				}
			}
			if uint64(emitted) != tc.bytes {
				t.Fatalf("writer layout emits %d, estimate %d", emitted, tc.bytes)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		kinds []string
		bytes uint64
	}{
		{"empty", nil, 2080}, {"partial", []string{"solid", "uniform", "mixed"}, 5856}, {"maximum", nil, 104480},
	} {
		t.Run("sector/"+tc.name, func(t *testing.T) {
			obj := scheduleObject(1, [3]int{})
			kinds := tc.kinds
			if tc.name == "maximum" {
				kinds = make([]string, 64)
				for i := range kinds {
					kinds[i] = "mixed"
				}
			}
			for i, kind := range kinds {
				schedulePutBrick(obj, [6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}, scheduleBrick(kind))
			}
			m, scene := scheduleFixture(t, obj)
			obj.XBrickMap.DirtySectors[[3]int{}] = true
			m.SetVoxelUploadBudget(VoxelUploadBudget{tc.bytes, 1, 64})
			work := scheduleRun(t, m, scene)
			if len(work) != 1 || work[0].bytes != tc.bytes || work[0].bricks != 64 || work[0].sectors != 1 {
				t.Fatalf("sector cost: %+v", work)
			}
		})
	}
	for _, n := range []int{0, 1, 2, 256, 300} {
		t.Run("materials/"+strconv.Itoa(n), func(t *testing.T) {
			obj := scheduleObject(1)
			m, scene := scheduleFixture(t, obj)
			obj.MaterialTable = make([]core.Material, n)
			rows := min(n, 256)
			if n == 0 {
				rows = 256
			}
			want := uint64(rows * 64)
			m.SetVoxelUploadBudget(VoxelUploadBudget{want - 1, 0, 0})
			if work := scheduleRun(t, m, scene); len(work) != 0 {
				t.Fatal("one-byte material deficit must defer without execution")
			}
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
				t.Fatal("deferred material table reported ready")
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{want, 0, 0})
			work := scheduleRun(t, m, scene)
			if len(work) != 1 || work[0].kind != voxelUploadMaterial || work[0].bytes != want || work[0].bricks != 0 || work[0].sectors != 0 {
				t.Fatalf("material cost: %+v", work)
			}
			if uint64(len(buildMaterialData(obj.MaterialTable))) != want {
				t.Fatal("estimate differs from emitted material buffer")
			}
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
				t.Fatalf("uploaded %d source rows must settle using capped emitted capacity", n)
			}
			obj.MaterialTable = append([]core.Material(nil), obj.MaterialTable...)
			if n > 0 {
				if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
					t.Fatal("source identity must remain part of readiness")
				}
			}
		})
	}
}

func TestVoxelUploadMaterialsShareByteBudget(t *testing.T) {
	obj := scheduleObject(1, [3]int{})
	m, scene := scheduleFixture(t, obj)
	obj.MaterialTable = make([]core.Material, 2)
	obj.XBrickMap.DirtySectors[[3]int{}] = true
	m.SetVoxelUploadBudget(VoxelUploadBudget{2207, 1, 64}) // 128 + 2080 requires one more byte.
	work := scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].kind != voxelUploadMaterial || m.VoxelDirtySectorsPending != 1 {
		t.Fatalf("materials must consume the geometry byte budget: %+v", work)
	}
	if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
		t.Fatal("partial frame reported ready")
	}
	if work = scheduleRun(t, m, scene); len(work) != 1 || work[0].kind != voxelUploadSector {
		t.Fatalf("deferred geometry must drain next frame: %+v", work)
	}
	if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
		t.Fatal("completed content/current lookup must settle")
	}
	m.sectorTopologyRevision++
	if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
		t.Fatal("content completion cannot bypass stale lookup")
	}
}

func TestVoxelUploadStableOrder(t *testing.T) {
	t.Run("five priorities including hidden", func(t *testing.T) {
		var objects []*core.VoxelObject
		for i := 4; i >= 0; i-- {
			obj := scheduleObject(uint32(100+i), [3]int{})
			obj.VoxelUploadPriority = uint8(i)
			obj.VoxelUploadOrder = uint64(4 - i)
			obj.RenderEnabled = false
			objects = append(objects, obj)
		}
		m, scene := scheduleFixture(t, objects...)
		for _, obj := range objects {
			obj.XBrickMap.DirtySectors[[3]int{}] = true
		}
		work := scheduleRun(t, m, scene)
		if len(work) != 5 {
			t.Fatalf("hidden residents must remain eligible: %+v", work)
		}
		for i, w := range work {
			if w.object.VoxelUploadPriority != uint8(i) {
				t.Fatalf("priority order at %d: %+v", i, w)
			}
		}
	})
	t.Run("ticket order and map ID fallback independent of scene insertion", func(t *testing.T) {
		for _, permutation := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}, {2, 0, 3, 1}} {
			a, b, c, d := scheduleObject(40, [3]int{}), scheduleObject(30, [3]int{}), scheduleObject(20, [3]int{}), scheduleObject(10, [3]int{})
			a.VoxelUploadOrder = 2
			b.VoxelUploadOrder = 2
			c.VoxelUploadOrder = 1 // d uses map ID 10.
			base := []*core.VoxelObject{a, b, c, d}
			var objects []*core.VoxelObject
			for _, i := range permutation {
				objects = append(objects, base[i])
			}
			m, scene := scheduleFixture(t, objects...)
			for _, obj := range objects {
				obj.XBrickMap.DirtySectors[[3]int{}] = true
			}
			work := scheduleRun(t, m, scene)
			var ids []uint32
			for _, w := range work {
				ids = append(ids, w.object.XBrickMap.ID)
			}
			if !reflect.DeepEqual(ids, []uint32{20, 30, 40, 10}) {
				t.Fatalf("permutation %v emitted map IDs %v", permutation, ids)
			}
		}
	})
	t.Run("signed sector and brick coordinates", func(t *testing.T) {
		sectorKeys := [][3]int{{1, 0, 0}, {-1, 0, 0}, {0, -2, 0}, {0, 0, -3}, {0, 0, 0}, {-1, -1, 0}}
		want := [][3]int{{-1, -1, 0}, {-1, 0, 0}, {0, -2, 0}, {0, 0, -3}, {0, 0, 0}, {1, 0, 0}}
		obj := scheduleObject(1, sectorKeys...)
		m, scene := scheduleFixture(t, obj)
		for _, key := range sectorKeys {
			obj.XBrickMap.DirtySectors[key] = true
		}
		work := scheduleRun(t, m, scene)
		var got [][3]int
		for _, w := range work {
			got = append(got, w.sectorKey)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("signed sectors %v, want %v", got, want)
		}
		brickKeys := [][6]int{{1, 0, 0, 0, 0, 0}, {-1, 0, 0, 0, 0, 0}, {0, 0, 0, 1, 0, 0}, {0, 0, 0, 0, 1, 0}, {0, 0, 0, 0, 0, 1}, {-1, -1, 0, 3, 3, 3}}
		for _, key := range brickKeys {
			obj.XBrickMap.DirtyBricks[key] = true
		}
		work = scheduleRun(t, m, scene)
		var bricks [][6]int
		for _, w := range work {
			bricks = append(bricks, w.brickKey)
		}
		expected := [][6]int{{-1, -1, 0, 3, 3, 3}, {-1, 0, 0, 0, 0, 0}, {0, 0, 0, 0, 0, 1}, {0, 0, 0, 0, 1, 0}, {0, 0, 0, 1, 0, 0}, {1, 0, 0, 0, 0, 0}}
		if !reflect.DeepEqual(bricks, expected) {
			t.Fatalf("signed bricks %v, want %v", bricks, expected)
		}
	})
	t.Run("work kind and equivalent material scene ties", func(t *testing.T) {
		first := scheduleObject(1, [3]int{}, [3]int{1, 0, 0})
		second := scheduleObject(2)
		second.XBrickMap = first.XBrickMap
		m, scene := scheduleFixture(t, second, first)
		first.MaterialTable = make([]core.Material, 2)
		second.MaterialTable = make([]core.Material, 2)
		first.XBrickMap.DirtySectors[[3]int{}] = true
		first.XBrickMap.DirtyBricks[[6]int{1, 0, 0, 0, 0, 0}] = true
		work := scheduleRun(t, m, scene)
		if len(work) != 4 || work[0].kind != voxelUploadMaterial || work[0].object != second || work[1].kind != voxelUploadMaterial || work[1].object != first || work[2].kind != voxelUploadSector || work[3].kind != voxelUploadBrick {
			t.Fatalf("stable kind/scene tie order: %+v", work)
		}
	})
}

func TestVoxelUploadSharedGeometryAndCoveredDedup(t *testing.T) {
	first := scheduleObject(10, [3]int{})
	first.VoxelUploadPriority = core.VoxelUploadPriorityKeep
	first.VoxelUploadOrder = 99
	best := scheduleObject(20)
	best.XBrickMap = first.XBrickMap
	best.VoxelUploadPriority = core.VoxelUploadPriorityCollision
	best.VoxelUploadOrder = 7
	best.RenderEnabled = false
	peer := scheduleObject(30, [3]int{})
	peer.VoxelUploadPriority = core.VoxelUploadPriorityCollision
	peer.VoxelUploadOrder = 8
	m, scene := scheduleFixture(t, first, peer, best)
	first.MaterialTable = make([]core.Material, 2)
	best.MaterialTable = make([]core.Material, 2)
	first.XBrickMap.DirtySectors[[3]int{}] = true
	for i := 0; i < 64; i++ {
		first.XBrickMap.DirtyBricks[[6]int{0, 0, 0, i % 4, (i / 4) % 4, i / 16}] = true
	}
	peer.XBrickMap.DirtySectors[[3]int{}] = true
	m.SetVoxelUploadBudget(VoxelUploadBudget{math.MaxUint64, 0, 64})
	work := scheduleRun(t, m, scene)
	if len(work) != 2 || work[0].kind != voxelUploadMaterial || work[1].kind != voxelUploadMaterial {
		t.Fatalf("deferred sector's covered bricks must not bypass it: %+v", work)
	}
	if m.VoxelDirtySectorsPending != 2 || m.VoxelDirtyBricksPending != 64 {
		t.Fatalf("physical shared-map pending queues counted twice: %d/%d", m.VoxelDirtySectorsPending, m.VoxelDirtyBricksPending)
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{math.MaxUint64, 1, 64})
	work = scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].object.XBrickMap != first.XBrickMap {
		t.Fatalf("shared geometry must use its best instance priority/order: %+v", work)
	}
	if len(first.XBrickMap.DirtySectors) != 0 || len(first.XBrickMap.DirtyBricks) != 0 {
		t.Fatal("sector completion must consume every covered brick")
	}
	if m.VoxelDirtySectorsPending != 1 || m.VoxelDirtyBricksPending != 0 {
		t.Fatal("pending physical map count after completion is wrong")
	}
	work = scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].object != peer {
		t.Fatalf("shared sector must not execute twice: %+v", work)
	}
}

func TestVoxelUploadOversizedUnitDoesNotBlockSmallerWork(t *testing.T) {
	large := scheduleObject(1, [3]int{})
	small := scheduleObject(2, [3]int{})
	schedulePutBrick(large, [6]int{}, scheduleBrick("mixed"))
	m, scene := scheduleFixture(t, large, small)
	large.XBrickMap.DirtySectors[[3]int{}] = true
	small.XBrickMap.DirtyBricks[[6]int{}] = true
	m.SetVoxelUploadBudget(VoxelUploadBudget{32, 1, 64})
	work := scheduleRun(t, m, scene)
	if len(work) != 1 || work[0].object != small || !large.XBrickMap.DirtySectors[[3]int{}] {
		t.Fatalf("oversized first unit blocked smaller work or lost dirtiness: %+v", work)
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{3712, 1, 64})
	if work = scheduleRun(t, m, scene); len(work) != 1 || work[0].object != large {
		t.Fatalf("raising limits must allow retained unit: %+v", work)
	}
}

func TestVoxelUploadDrainAndCurrentEdits(t *testing.T) {
	obj := scheduleObject(1, [3]int{})
	m, scene := scheduleFixture(t, obj)
	for i := 0; i < 4; i++ {
		obj.XBrickMap.DirtyBricks[[6]int{0, 0, 0, i, 0, 0}] = true
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{1632, 0, 1})
	if work := scheduleRun(t, m, scene); len(work) != 1 || work[0].brickKey[3] != 0 {
		t.Fatalf("first drain: %+v", work)
	}
	// Pending coordinates retain no payload snapshot: a later edit changes cost.
	replacement := scheduleBrick("mixed")
	schedulePutBrick(obj, [6]int{0, 0, 0, 1, 0, 0}, replacement)
	for frame := 1; frame <= 3; frame++ {
		work := scheduleRun(t, m, scene)
		if len(work) != 1 || work[0].brickKey[3] != frame {
			t.Fatalf("frame %d: %+v", frame, work)
		}
		want := uint64(32)
		if frame == 1 {
			want = 1632
		}
		if work[0].bytes != want {
			t.Fatalf("frame %d retained stale cost/payload: %d want %d", frame, work[0].bytes, want)
		}
		if m.VoxelDirtyBricksPending != 3-frame {
			t.Fatal("bounded drain left wrong pending work")
		}
	}
	if work := scheduleRun(t, m, scene); len(work) != 0 {
		t.Fatal("completed work repeated")
	}
}

func TestVoxelUploadAgingFairness(t *testing.T) {
	for _, tc := range []struct {
		name string
		high uint8
		due  int
	}{{"one level every eight frames", core.VoxelUploadPriorityPrefetch, 8}, {"sustained fallback arrivals", core.VoxelUploadPriorityFallback, 32}} {
		t.Run(tc.name, func(t *testing.T) {
			old := scheduleObject(99, [3]int{})
			old.VoxelUploadPriority = core.VoxelUploadPriorityKeep
			old.VoxelUploadOrder = 999
			m, scene := scheduleFixture(t, old)
			old.XBrickMap.DirtyBricks[[6]int{}] = true
			m.SetVoxelUploadBudget(VoxelUploadBudget{0, 0, 1})
			scheduleRun(t, m, scene)
			m.SetVoxelUploadBudget(VoxelUploadBudget{32, 0, 1})
			for frame := 1; frame <= tc.due; frame++ {
				incoming := scheduleObject(uint32(100+frame), [3]int{})
				incoming.VoxelUploadPriority = tc.high
				incoming.VoxelUploadOrder = 1
				// Material-free arrivals here have their completed metadata seeded from a
				// second fixture, so the only contested resource is one brick record.
				fresh, _ := scheduleFixture(t, incoming)
				m.Allocations[incoming.XBrickMap] = fresh.Allocations[incoming.XBrickMap]
				for s := range fresh.SectorToInfo {
					m.SectorToInfo[s] = SectorGpuInfo{SlotIndex: m.SectorAlloc.Alloc(), BrickTableIndex: m.BrickAlloc.Alloc() * 64}
				}
				m.MaterialAllocations[incoming] = fresh.MaterialAllocations[incoming]
				m.MaterialAllocations[incoming].MaterialOffset = m.MaterialAlloc.Alloc() * materialBlockCapacity
				incoming.XBrickMap.DirtyBricks[[6]int{}] = true
				scene.Objects = []*core.VoxelObject{incoming, old}
				work := scheduleRun(t, m, scene)
				if len(work) != 1 {
					t.Fatalf("frame %d work %+v", frame, work)
				}
				want := incoming
				if frame == tc.due {
					want = old
				}
				if work[0].object != want {
					t.Fatalf("frame %d served %d, want %d; older equal effective priority must win", frame, work[0].object.XBrickMap.ID, want.XBrickMap.ID)
				}
			}
		})
	}
}

func TestVoxelUploadExecutorRefusalPreservesState(t *testing.T) {
	for _, kind := range []voxelUploadKind{voxelUploadMaterial, voxelUploadSector, voxelUploadBrick} {
		t.Run(map[voxelUploadKind]string{voxelUploadMaterial: "material", voxelUploadSector: "sector", voxelUploadBrick: "brick"}[kind], func(t *testing.T) {
			obj := scheduleObject(1, [3]int{})
			m, scene := scheduleFixture(t, obj)
			switch kind {
			case voxelUploadMaterial:
				obj.MaterialTable = make([]core.Material, 2)
			case voxelUploadSector:
				obj.XBrickMap.DirtySectors[[3]int{}] = true
			case voxelUploadBrick:
				obj.XBrickMap.DirtyBricks[[6]int{}] = true
			}
			calls := 0
			m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
				calls++
				if w.kind != kind {
					t.Fatalf("unexpected work %+v", w)
				}
				return false
			})
			if calls != 1 {
				t.Fatalf("refusal attempted %d times in one frame", calls)
			}
			if kind == voxelUploadSector && !obj.XBrickMap.DirtySectors[[3]int{}] || kind == voxelUploadBrick && !obj.XBrickMap.DirtyBricks[[6]int{}] {
				t.Fatal("refused executor cleared dirty work")
			}
			if m.VoxelUploadBytes != 0 || m.VoxelSectorsUploaded != 0 || m.VoxelBricksUploaded != 0 || m.VoxelMaterialsUploaded != 0 {
				t.Fatal("refusal before writes counted emitted content")
			}
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
				t.Fatal("refused work reported ready")
			}
			if work := scheduleRun(t, m, scene); len(work) != 1 || work[0].kind != kind {
				t.Fatalf("refused work not retried: %+v", work)
			}
		})
	}
}

func TestVoxelUploadCleansFalseAndOrphanDirtyEntries(t *testing.T) {
	obj := scheduleObject(1, [3]int{})
	m, scene := scheduleFixture(t, obj)
	obj.XBrickMap.DirtySectors[[3]int{}] = false
	obj.XBrickMap.DirtySectors[[3]int{99, 0, 0}] = true
	obj.XBrickMap.DirtyBricks[[6]int{}] = false
	obj.XBrickMap.DirtyBricks[[6]int{99, 0, 0, 0, 0, 0}] = true
	m.SetVoxelUploadBudget(VoxelUploadBudget{})
	if work := scheduleRun(t, m, scene); len(work) != 0 {
		t.Fatal("false/orphan work emitted writes")
	}
	if len(obj.XBrickMap.DirtySectors) != 0 || len(obj.XBrickMap.DirtyBricks) != 0 || m.VoxelDirtySectorsPending != 0 || m.VoxelDirtyBricksPending != 0 {
		t.Fatal("false/orphan work survived cleanup")
	}
}

func TestVoxelUploadDeferredEmptyMaterialGenerationZero(t *testing.T) {
	for _, tc := range []struct {
		name   string
		budget uint64
		refuse bool
	}{{"byte pause", 0, false}, {"one byte deficit", 16383, false}, {"executor refusal", 16384, true}} {
		t.Run(tc.name, func(t *testing.T) {
			obj := scheduleObject(1)
			obj.MaterialTable = nil
			m, scene := scheduleFixture(t, obj)
			delete(m.MaterialAllocations, obj)
			m.MaterialBufferGeneration = 0
			m.SetVoxelUploadBudget(VoxelUploadBudget{tc.budget, 0, 0})
			calls := 0
			m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { calls++; return !tc.refuse })
			if (!tc.refuse && calls != 0) || (tc.refuse && calls != 1) {
				t.Fatalf("executor calls %d", calls)
			}
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
				t.Fatal("allocated but unwritten empty generation-zero materials reported ready")
			}
			m.SetVoxelUploadBudget(VoxelUploadBudget{16384, 0, 0})
			if work := scheduleRun(t, m, scene); len(work) != 1 || work[0].bytes != 16384 {
				t.Fatalf("empty table retry %+v", work)
			}
			if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
				t.Fatal("successful empty material upload must settle at generation zero")
			}
		})
	}
}

func TestVoxelUploadRemovalDropsAgingAndStaleWork(t *testing.T) {
	for _, changeMap := range []bool{false, true} {
		t.Run(map[bool]string{false: "object removed", true: "map detached"}[changeMap], func(t *testing.T) {
			old := scheduleObject(99, [3]int{})
			old.VoxelUploadPriority = core.VoxelUploadPriorityKeep
			incoming := scheduleObject(1, [3]int{})
			incoming.VoxelUploadPriority = core.VoxelUploadPriorityFallback
			m, scene := scheduleFixture(t, old, incoming)
			old.XBrickMap.DirtyBricks[[6]int{}] = true
			target := old.XBrickMap
			m.SetVoxelUploadBudget(VoxelUploadBudget{0, 0, 1})
			for i := 0; i < 40; i++ {
				scheduleRun(t, m, scene)
			}
			if changeMap {
				old.XBrickMap = volume.NewXBrickMap()
			} else {
				scene.Objects = []*core.VoxelObject{incoming}
			}
			// This service invocation observes the removal and must discard its age.
			scheduleRun(t, m, scene)
			old.XBrickMap = target
			scene.Objects = []*core.VoxelObject{old, incoming}
			incoming.XBrickMap.DirtyBricks[[6]int{}] = true
			m.SetVoxelUploadBudget(VoxelUploadBudget{32, 0, 1})
			work := scheduleRun(t, m, scene)
			if len(work) != 1 || work[0].object != incoming {
				t.Fatalf("removed work retained stale age: %+v", work)
			}
			if !target.DirtyBricks[[6]int{}] {
				t.Fatal("removed map's CPU dirty markers belong to the map")
			}
		})
	}
}

// Emulate the successful executor's physical slot writes only for admission
// recovery tests. Completion/queue behavior still runs in the production service.
func schedulePhysicalSlots(t *testing.T, m *GpuBufferManager, w voxelUploadWork) {
	t.Helper()
	if w.kind == voxelUploadMaterial {
		return
	}
	xbm := w.object.XBrickMap
	key := w.sectorKey
	if w.kind == voxelUploadBrick {
		key = [3]int{w.brickKey[0], w.brickKey[1], w.brickKey[2]}
	}
	sector := xbm.Sectors[key]
	pointers := m.Allocations[xbm].Bricks[key]
	indices := []int{w.brickKey[3] + w.brickKey[4]*4 + w.brickKey[5]*16}
	if w.kind == voxelUploadSector {
		indices = make([]int, 64)
		for i := range indices {
			indices[i] = i
		}
	}
	// Release obsolete records before new allocation, including later cleared
	// indices within a sector. Atomic admission counts these releasable slots.
	for _, i := range indices {
		current := sector.GetBrick(i%4, (i/4)%4, i/16)
		old := pointers[i]
		if old != nil && old != current {
			m.releaseBrickSlot(old)
			m.releaseVoxelAuxSlot(old)
		}
		if current != nil && !resolveBrickUploadMode(current.Flags).usesPayload {
			m.releaseBrickSlot(current)
		}
	}
	for _, i := range indices {
		b := sector.GetBrick(i%4, (i/4)%4, i/16)
		pointers[i] = b
		if b == nil {
			continue
		}
		if resolveBrickUploadMode(b.Flags).usesPayload {
			if _, ok := m.BrickToSlot[b]; !ok {
				slot, ok := m.allocPayloadSlot()
				if !ok {
					t.Fatal("admitted atomic unit cannot allocate promised payload slots")
				}
				m.BrickToSlot[b] = slot
			}
		}
		if _, ok := m.BrickToAuxSlot[b]; !ok {
			m.BrickToAuxSlot[b] = m.VoxelAuxAlloc.Alloc()
		}
	}
}

func TestVoxelUploadAtlasAtomicAdmission(t *testing.T) {
	target := scheduleObject(1, [3]int{})
	m, scene := scheduleFixture(t, target)
	m.VoxelPayloadBricks = 1
	m.VoxelPayloadPageCount = 1
	schedulePutBrick(target, [6]int{}, scheduleBrick("mixed"))
	schedulePutBrick(target, [6]int{0, 0, 0, 1, 0, 0}, scheduleBrick("mixed"))
	target.XBrickMap.DirtySectors[[3]int{}] = true
	target.XBrickMap.DirtyBricks[[6]int{}] = true
	calls := 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { calls++; return true })
	if calls != 0 || m.VoxelUploadBytes != 0 {
		t.Fatalf("atlas pressure reached executor before complete atomic capacity admission: calls=%d bytes=%d", calls, m.VoxelUploadBytes)
	}
	if !target.XBrickMap.DirtySectors[[3]int{}] || !target.XBrickMap.DirtyBricks[[6]int{}] {
		t.Fatal("capacity-blocked atomic work lost dirty markers")
	}
	if len(m.BrickToSlot) != 0 || m.PayloadAlloc[0].Tail != 0 {
		t.Fatal("failed preflight partially consumed atlas capacity")
	}
	m.VoxelPayloadPageCount = 2
	calls = 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { calls++; schedulePhysicalSlots(t, m, w); return true })
	if calls != 1 || len(target.XBrickMap.DirtySectors) != 0 || len(target.XBrickMap.DirtyBricks) != 0 {
		t.Fatal("capacity across pages must admit the whole sector exactly once")
	}
	if len(m.BrickToSlot) != 2 {
		t.Fatal("both mixed bricks must have payload slots after execution")
	}
}

func TestVoxelUploadAtlasReplacementAndClearRecoverSlots(t *testing.T) {
	for _, unit := range []voxelUploadKind{voxelUploadSector, voxelUploadBrick} {
		for _, replacement := range []string{"empty", "solid", "mixed"} {
			name := map[voxelUploadKind]string{voxelUploadSector: "sector", voxelUploadBrick: "brick"}[unit] + "/" + replacement
			t.Run(name, func(t *testing.T) {
				target := scheduleObject(1, [3]int{})
				old := scheduleBrick("mixed")
				schedulePutBrick(target, [6]int{}, old)
				waiting := scheduleObject(2, [3]int{})
				m, scene := scheduleFixture(t, target, waiting)
				m.VoxelPayloadBricks = 1
				m.VoxelPayloadPageCount = 1 // Existing old brick fills this atlas.
				next := scheduleBrick(replacement)
				schedulePutBrick(target, [6]int{}, next)
				if unit == voxelUploadSector {
					target.XBrickMap.DirtySectors[[3]int{}] = true
				} else {
					target.XBrickMap.DirtyBricks[[6]int{}] = true
				}
				calls := 0
				m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
					calls++
					if w.kind != unit {
						t.Fatalf("replacement unit %+v", w)
					}
					schedulePhysicalSlots(t, m, w)
					return true
				})
				if calls != 1 {
					t.Fatalf("replacement must count its releasable old slot before admission: calls=%d", calls)
				}
				schedulePutBrick(waiting, [6]int{}, scheduleBrick("mixed"))
				waiting.XBrickMap.DirtyBricks[[6]int{}] = true
				calls = 0
				m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { calls++; schedulePhysicalSlots(t, m, w); return true })
				want := 1
				if replacement == "mixed" {
					want = 0
				}
				if calls != want {
					t.Fatalf("subsequent admission after %s: %d calls, want %d", replacement, calls, want)
				}
				if _, ok := m.BrickToSlot[old]; ok {
					t.Fatal("obsolete replacement payload slot survived")
				}
				if _, ok := m.BrickToAuxSlot[old]; ok {
					t.Fatal("obsolete replacement auxiliary slot survived")
				}
			})
		}
	}
}

func TestVoxelUploadAtlasSectorCanReuseLaterClearedRecord(t *testing.T) {
	target := scheduleObject(1, [3]int{})
	oldKey := [6]int{0, 0, 0, 3, 3, 3}
	schedulePutBrick(target, oldKey, scheduleBrick("mixed"))
	m, scene := scheduleFixture(t, target)
	m.VoxelPayloadBricks = 1
	m.VoxelPayloadPageCount = 1
	schedulePutBrick(target, oldKey, nil)
	schedulePutBrick(target, [6]int{}, scheduleBrick("mixed"))
	target.XBrickMap.DirtySectors[[3]int{}] = true
	calls := 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { calls++; schedulePhysicalSlots(t, m, w); return true })
	if calls != 1 || len(target.XBrickMap.DirtySectors) != 0 {
		t.Fatal("atomic sector must preflight later cleared slots before writing earlier new payloads")
	}
}

func TestVoxelUploadAtlasBlockedWorkDoesNotBlockClear(t *testing.T) {
	blocked := scheduleObject(1, [3]int{})
	clearable := scheduleObject(2, [3]int{})
	schedulePutBrick(clearable, [6]int{}, scheduleBrick("mixed"))
	m, scene := scheduleFixture(t, blocked, clearable)
	m.VoxelPayloadBricks = 1
	m.VoxelPayloadPageCount = 1
	schedulePutBrick(blocked, [6]int{}, scheduleBrick("mixed"))
	blocked.XBrickMap.DirtyBricks[[6]int{}] = true
	schedulePutBrick(clearable, [6]int{}, nil)
	clearable.XBrickMap.DirtyBricks[[6]int{}] = true
	var executed []*core.VoxelObject
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		executed = append(executed, w.object)
		schedulePhysicalSlots(t, m, w)
		return true
	})
	if len(executed) == 0 || executed[0] != clearable {
		t.Fatalf("capacity-blocked first unit prevented clear: %v", executed)
	}
	// A service may reconsider the first unit after capacity is freed, or wait
	// until the next frame; either way it must complete without a partial write.
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool { schedulePhysicalSlots(t, m, w); return true })
	if len(blocked.XBrickMap.DirtyBricks) != 0 {
		t.Fatal("freed capacity did not admit waiting payload on the following frame")
	}
}

func TestVoxelUploadExactCombinedMaterialAndGeometryBoundary(t *testing.T) {
	for _, tc := range []struct {
		bytes uint64
		units int
	}{{2208, 2}, {2207, 1}} {
		t.Run(strconv.FormatUint(tc.bytes, 10), func(t *testing.T) {
			obj := scheduleObject(1, [3]int{})
			m, scene := scheduleFixture(t, obj)
			obj.MaterialTable = make([]core.Material, 2)
			obj.XBrickMap.DirtySectors[[3]int{}] = true
			m.SetVoxelUploadBudget(VoxelUploadBudget{tc.bytes, 1, 64})
			work := scheduleRun(t, m, scene)
			if len(work) != tc.units || work[0].kind != voxelUploadMaterial {
				t.Fatalf("combined cap %d emitted %+v", tc.bytes, work)
			}
			if tc.units == 2 && work[1].kind != voxelUploadSector {
				t.Fatal("exact combined cap must include sector")
			}
		})
	}
}

func TestVoxelUploadMaterialGenerationDefersUntilSuccessfulWrite(t *testing.T) {
	obj := scheduleObject(1)
	m, scene := scheduleFixture(t, obj)
	m.MaterialBufferGeneration++
	m.SetVoxelUploadBudget(VoxelUploadBudget{63, 0, 0})
	if work := scheduleRun(t, m, scene); len(work) != 0 {
		t.Fatal("generation refresh exceeded byte cap")
	}
	if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); ready {
		t.Fatal("deferred generation refresh published readiness")
	}
	m.SetVoxelUploadBudget(VoxelUploadBudget{64, 0, 0})
	if work := scheduleRun(t, m, scene); len(work) != 1 || work[0].kind != voxelUploadMaterial {
		t.Fatalf("generation refresh retry %+v", work)
	}
	if ready, _, _ := m.VoxelObjectReady(obj, obj.XBrickMap, obj.XBrickMap.Revision); !ready {
		t.Fatal("successful generation refresh did not settle")
	}
}
