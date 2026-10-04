package gpu

import (
	"bytes"
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func c3h9Map(id uint32, cells ...[3]int) *volume.XBrickMap {
	m := volume.NewXBrickMap()
	m.ID = id
	for _, p := range cells {
		m.SetVoxel(p[0], p[1], p[2], 1)
	}
	return m
}
func c3h9GPUFixture(t *testing.T, extraCoarseCells ...[3]int) (*GpuBufferManager, *core.Scene, *core.VoxelObject, *volume.XBrickMap) {
	t.Helper()
	coarse := c3h9Map(22, append([][3]int{{0, 0, 0}, {1, 0, 0}}, extraCoarseCells...)...)
	seed := core.NewVoxelObject()
	seed.XBrickMap = coarse
	seed.MaterialTable = []core.Material{core.DefaultMaterial(), core.DefaultMaterial()}
	coarse.ClearDirty()
	m, _ := scheduleFixture(t, seed)
	obj := core.NewVoxelObject()
	obj.XBrickMap = c3h9Map(11, [3]int{1, 0, 0}, [3]int{3, 0, 0}, [3]int{65, 0, 0})
	obj.MaterialTable = seed.MaterialTable
	obj.UpdateWorldAABB()
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	m.MaterialAllocations[obj] = m.MaterialAllocations[seed]
	delete(m.MaterialAllocations, seed)
	scene := &core.Scene{Objects: []*core.VoxelObject{obj}, VisibleObjects: []*core.VoxelObject{obj}, ShadowObjects: []*core.VoxelObject{obj}}
	return m, scene, obj, coarse
}
func c3h9MarkDirty(m *volume.XBrickMap) {
	m.DirtySectors[[3]int{}] = true
	m.DirtyBricks[[6]int{}] = true
}

func TestC3h9SelectedMapCapacityUploadDedupeAndReadiness(t *testing.T) {
	m, scene, obj, coarse := c3h9GPUFixture(t, [3]int{65, 0, 0})
	full := obj.XBrickMap
	fullSectors := map[[3]int]bool{}
	for k, v := range full.DirtySectors {
		fullSectors[k] = v
	}
	fullBricks := map[[6]int]bool{}
	for k, v := range full.DirtyBricks {
		fullBricks[k] = v
	}
	fullStructure := full.StructureDirty
	if ready, _, _ := m.RenderVoxelObjectReady(obj, coarse, coarse.Revision); !ready {
		t.Fatal("coarse allocation/materials not accepted by render readiness")
	}
	if ready, _, _ := m.VoxelObjectReady(obj, coarse, coarse.Revision); ready {
		t.Fatal("old full-target readiness accepted coarse")
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, coarse, coarse.Revision+1); ready {
		t.Fatal("render readiness accepted stale revision")
	}
	alias := core.NewVoxelObject()
	alias.XBrickMap = c3h9Map(33, [3]int{1, 0, 0})
	alias.MaterialTable = obj.MaterialTable
	alias.VoxelUploadOrder = 1
	obj.VoxelUploadOrder = 100
	if !alias.SetRenderLOD2(coarse) {
		t.Fatal("alias setup")
	}
	m.MaterialAllocations[alias] = m.MaterialAllocations[obj]
	scene.Objects = append(scene.Objects, alias)
	sectors, bricks := m.voxelAllocationRequirements(scene)
	if sectors != m.SectorAlloc.Tail || bricks != m.BrickAlloc.Tail*64 {
		t.Fatal("coarse-only capacity planned authoritative full sectors", sectors, bricks)
	}
	m.prepareVoxelStructureDirtyState(scene)
	if m.Allocations[full] != nil || m.Allocations[alias.XBrickMap] != nil || len(m.Allocations) != 1 {
		t.Fatal("structural preparation allocated full maps for coarse-only rendering")
	}
	c3h9MarkDirty(coarse)
	// Sector0 upload subsumes brick0; sector2's dirty brick is independent work.
	coarse.DirtyBricks[[6]int{2, 0, 0, 0, 0, 0}] = true
	work := scheduleRun(t, m, scene)
	var sectorJobs, brickJobs int
	for _, w := range work {
		if w.kind == voxelUploadSector {
			sectorJobs++
		}
		if w.kind == voxelUploadBrick {
			brickJobs++
		}
		if w.kind != voxelUploadMaterial && (w.object != alias || w.identity().xbm != coarse) {
			t.Fatal("shared coarse upload not deduped/ordered/identified by selected map", w)
		}
	}
	if sectorJobs != 1 || brickJobs != 1 {
		t.Fatal("shared coarse geometry uploaded more than once", sectorJobs, brickJobs)
	}
	if !reflect.DeepEqual(full.DirtySectors, fullSectors) || !reflect.DeepEqual(full.DirtyBricks, fullBricks) || full.StructureDirty != fullStructure {
		t.Fatal("coarse upload consumed authoritative dirty work")
	}
	if ready, s, b := m.RenderVoxelObjectReady(obj, coarse, coarse.Revision); !ready || s != 0 || b != 0 {
		t.Fatal("completed selected map not ready", ready, s, b)
	}
	if ready, _, _ := m.VoxelObjectReady(obj, full, full.Revision); ready {
		t.Fatal("coarse-only upload established full readiness")
	}
	legacy, legacyObj := voxelReadinessFixture(false)
	if ready, _, _ := legacy.RenderVoxelObjectReady(legacyObj, legacyObj.XBrickMap, legacyObj.XBrickMap.Revision); !ready {
		t.Fatal("new render readiness broke default full path")
	}
}

func TestC3h9UploadCapturedTargetDoesNotRetargetAfterClear(t *testing.T) {
	m, scene, obj, coarse := c3h9GPUFixture(t)
	full := obj.XBrickMap
	beforeSectors, beforeBricks := len(full.DirtySectors), len(full.DirtyBricks)
	beforeStructure := full.StructureDirty
	c3h9MarkDirty(coarse)
	delete(m.MaterialAllocations, obj)
	writes := 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		writes++
		if writes != 1 || w.kind != voxelUploadMaterial {
			t.Fatal("stale queued coarse work executed after target clear", w)
		}
		obj.ClearRenderRepresentation()
		return true
	})
	if writes != 1 || len(full.DirtySectors) != beforeSectors || len(full.DirtyBricks) != beforeBricks || full.StructureDirty != beforeStructure || len(coarse.DirtySectors) != 1 || len(coarse.DirtyBricks) != 1 {
		t.Fatal("completion retargeted full map or consumed stale coarse jobs")
	}
}

func TestC3h9SceneRecordsEncodeSelectedMatricesBoundsMapAndInvalidate(t *testing.T) {
	m, scene, obj, coarse := c3h9GPUFixture(t)
	obj.Transform.Position = mgl32.Vec3{10, -5, 3}
	obj.Transform.Scale = mgl32.Vec3{.5, 2, 1.5}
	obj.Transform.Pivot = mgl32.Vec3{1, -.5, .25}
	obj.Transform.Rotation = mgl32.QuatRotate(.7, mgl32.Vec3{0, 0, 1})
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	origin := mgl32.Vec3{2, -3, 1}
	batch := s3bClone(m.prepareSceneRecords(scene, origin).visible)
	matrix := obj.Transform.ObjectToWorld().Mul4(mgl32.Scale3D(2, 2, 2))
	inverse := mgl32.Scale3D(.5, .5, .5).Mul4(obj.Transform.WorldToObject())
	for i, v := range matrix {
		if i >= 12 && i <= 14 {
			v -= origin[i-12]
		}
		s3bWord(t, batch.instances, i*4, math.Float32bits(v))
	}
	for i, v := range renderRelativeWorldToObject(inverse, origin) {
		s3bWord(t, batch.instances, 64+i*4, math.Float32bits(v))
	}
	bounds := obj.RenderWorldBounds()
	for axis := 0; axis < 3; axis++ {
		s3bWord(t, batch.instances, 128+axis*4, math.Float32bits(bounds[0][axis]-origin[axis]))
		s3bWord(t, batch.instances, 144+axis*4, math.Float32bits(bounds[1][axis]-origin[axis]))
		s3bWord(t, batch.instances, 160+axis*4, math.Float32bits(0))
		wantMax := float32(1)
		if axis == 0 {
			wantMax = 2
		}
		s3bWord(t, batch.instances, 176+axis*4, math.Float32bits(wantMax))
	}
	for axis := 0; axis < 3; axis++ {
		s3bWord(t, batch.bvh, axis*4, math.Float32bits(bounds[0][axis]-origin[axis]))
		s3bWord(t, batch.bvh, 16+axis*4, math.Float32bits(bounds[1][axis]-origin[axis]))
	}
	// Fresh writers must encode the same selected fields independently verified
	// above; their default-path golden tests alone cannot catch missed migration.
	if !bytes.Equal(buildInstanceData(scene.VisibleObjects, origin), batch.instances) || !bytes.Equal(buildObjectParamsData(scene.VisibleObjects, m.Allocations, m.MaterialAllocations), batch.params) || !bytes.Equal(buildRenderBVHData(scene.VisibleObjects, origin), batch.bvh) {
		t.Fatal("uncached scene writers differ from verified selected records")
	}
	s3bWord(t, batch.params, 0, coarse.ID)
	s3bWord(t, batch.params, 24, 1)
	direct := buildDirectSectorLookupData(scene, m.SectorToInfo, m.Allocations, 9)
	alloc := m.Allocations[coarse]
	if alloc.DirectLookup.LookupMode == 0 || alloc.DirectLookup.TableBase != 9 || len(direct) != 4 {
		t.Fatal("direct sector lookup didn't use selected allocation", alloc.DirectLookup)
	}
	var slot uint32
	for _, info := range m.SectorToInfo {
		slot = info.SlotIndex
	}
	if binary.LittleEndian.Uint32(direct) != slot {
		t.Fatal("selected lookup sector index differs")
	}
	// Settle the direct lookup metadata change before asserting no further work.
	m.prepareSceneRecords(scene, origin)
	unchanged := s3bWorkOf(m)
	m.prepareSceneRecords(scene, origin)
	if s3bWorkOf(m) != unchanged {
		t.Fatal("stable render rows rebuilt")
	}
	obj.ClearRenderRepresentation()
	m.Allocations[obj.XBrickMap] = &ObjectGpuAllocation{DirectLookup: defaultDirectSectorLookupMetadata()}
	restored := s3bClone(m.prepareSceneRecords(scene, origin).visible)
	if bytes.Equal(restored.instances, batch.instances) || bytes.Equal(restored.params, batch.params) || bytes.Equal(restored.bvh, batch.bvh) {
		t.Fatal("object-stable representation change didn't invalidate matrix/map/bounds records")
	}
	s3bWord(t, restored.params, 0, obj.XBrickMap.ID)
}

func TestC3h9NormalsUseSelectedOccupancyAndStalePassInputsAreSkipped(t *testing.T) {
	m, scene, obj, coarse := c3h9GPUFixture(t)
	sector := coarse.Sectors[[3]int{}]
	brick := sector.GetBrick(0, 0, 0)
	if brick == nil {
		t.Fatal("fixture coarse brick missing")
	}
	got := buildVoxelAuxBytes(newVoxelNormalBakeContext(scene), obj, brick, [3]int{})
	min, max := coarse.ComputeAABB()
	want := volume.BuildVoxelAuxBytes(brick, [3]int{}, volume.VoxelNormalBakeOptions{BoundsMin: min, BoundsMax: max, HasBounds: true, SampleOccupancy: func(p [3]int) bool { occupied, _ := coarse.GetVoxel(p[0], p[1], p[2]); return occupied }})
	if !bytes.Equal(got, want) {
		t.Fatal("baked occupancy/normal payload sampled authoritative full geometry")
	}
	coarse.SetVoxel(2, 0, 0, 1)
	if obj.RenderRepresentationValid() {
		t.Fatal("fixture should be invalid")
	}
	scene.TransparentVisibleObjects = scene.VisibleObjects
	empty := (&GpuBufferManager{}).prepareSceneRecords(&core.Scene{}, mgl32.Vec3{})
	stale := m.prepareSceneRecords(scene, mgl32.Vec3{})
	for _, pair := range [][2]sceneRecordBatch{{stale.visible, empty.visible}, {stale.transparent, empty.transparent}, {stale.shadow, empty.shadow}} {
		if !bytes.Equal(pair[0].instances, pair[1].instances) || !bytes.Equal(pair[0].params, pair[1].params) || !bytes.Equal(pair[0].bvh, pair[1].bvh) {
			t.Fatal("stale invalid representation emitted GPU records")
		}
	}
	if !bytes.Equal(buildInstanceData(scene.VisibleObjects, mgl32.Vec3{}), buildInstanceData(nil, mgl32.Vec3{})) || !bytes.Equal(buildObjectParamsData(scene.VisibleObjects, m.Allocations, m.MaterialAllocations), buildObjectParamsData(nil, m.Allocations, m.MaterialAllocations)) || !bytes.Equal(buildRenderBVHData(scene.VisibleObjects, mgl32.Vec3{}), buildRenderBVHData(nil, mgl32.Vec3{})) {
		t.Fatal("invalid active representation emitted uncached GPU row")
	}
	if m.SceneRecordObjectCount != 0 {
		t.Fatal("stale invalid object retained record ownership")
	}
	work := scheduleRun(t, m, scene)
	if len(work) != 0 {
		t.Fatal("invalid active representation scheduled uploads", work)
	}
	if ready, _, _ := m.RenderVoxelObjectReady(obj, coarse, coarse.Revision); ready {
		t.Fatal("invalid selection reported ready")
	}
	for _, manager := range []*GpuBufferManager{nil, {}} {
		if ready, _, _ := manager.RenderVoxelObjectReady(nil, nil, 0); ready {
			t.Fatal("null target ready")
		}
	}
}

func TestC3h9GPUShadowCollectorAndRebuildUseSelectedBounds(t *testing.T) {
	obj := core.NewVoxelObject()
	obj.XBrickMap = c3h9Map(11, [3]int{1, 0, 0})
	obj.UpdateWorldAABB()
	coarse := c3h9Map(22, [3]int{0, 0, 0})
	point := pointShadowCullVolume{Position: mgl32.Vec3{.25, .5, .5}, Range: .2}
	if got := collectShadowCasters([]*core.VoxelObject{obj}, nil, nil, []pointShadowCullVolume{point}, mgl32.Vec3{}); len(got) != 0 {
		t.Fatal("full fixture intersects point volume")
	}
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	got := collectShadowCasters([]*core.VoxelObject{obj}, nil, nil, []pointShadowCullVolume{point}, mgl32.Vec3{})
	if len(got) != 1 || got[0] != obj {
		t.Fatal("GPU shadow collector ignored expanded render bounds")
	}
	scene := &core.Scene{Objects: []*core.VoxelObject{obj}}
	rebuildShadowCasterScene(scene, got)
	for axis := 0; axis < 3; axis++ {
		s3bWord(t, scene.ShadowBVHNodesBytes, axis*4, math.Float32bits(0))
		s3bWord(t, scene.ShadowBVHNodesBytes, 16+axis*4, math.Float32bits(2))
	}
	coarse.SetVoxel(1, 0, 0, 1)
	if got := collectShadowCasters([]*core.VoxelObject{nil, obj}, nil, nil, []pointShadowCullVolume{point}, mgl32.Vec3{}); len(got) != 0 {
		t.Fatal("shadow collector includes invalid representation")
	}
	rebuildShadowCasterScene(scene, []*core.VoxelObject{obj})
	if len(scene.ShadowObjects) != 0 {
		t.Fatal("shadow rebuild retained stale invalid representation")
	}
}

func TestC3h9PointShadowAABBVolumeDistance(t *testing.T) {
	bounds := [2]mgl32.Vec3{{0, 0, 0}, {2, 2, 2}}
	for _, c := range []struct {
		name     string
		position mgl32.Vec3
		radius   float32
		want     bool
	}{
		{"inside", mgl32.Vec3{.25, .5, .5}, .2, true},
		{"outside", mgl32.Vec3{3, 1, 1}, .5, false},
		{"tangent", mgl32.Vec3{3, 1, 1}, 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := intersectsPointShadowVolume(bounds, pointShadowCullVolume{Position: c.position, Range: c.radius}); got != c.want {
				t.Fatalf("intersection got%v want%v", got, c.want)
			}
		})
	}
}

func TestC3h9SectorGridSelectionSnapshotTracksEqualCountSwapAndMapID(t *testing.T) {
	obj := core.NewVoxelObject()
	obj.XBrickMap = c3h9Map(11, [3]int{1, 0, 0})
	coarse := c3h9Map(22, [3]int{0, 0, 0})
	scene := &core.Scene{Objects: []*core.VoxelObject{obj}}
	manager := &GpuBufferManager{}
	if !manager.sectorGridSelectionChanged(scene) || manager.sectorGridSelectionChanged(scene) {
		t.Fatal("initial/stable full snapshot wrong")
	}
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	if !manager.sectorGridSelectionChanged(scene) || manager.sectorGridSelectionChanged(scene) {
		t.Fatal("equal object-count full/coarse swap missed or remains dirty")
	}
	coarse.ID++
	if !manager.sectorGridSelectionChanged(scene) || manager.sectorGridSelectionChanged(scene) {
		t.Fatal("mutable selected map ID change omitted from snapshot")
	}
	obj.ClearRenderRepresentation()
	if !manager.sectorGridSelectionChanged(scene) || manager.sectorGridSelectionChanged(scene) {
		t.Fatal("equal-count coarse/full swap omitted")
	}
	scene.Objects = []*core.VoxelObject{nil, obj, obj}
	if !manager.sectorGridSelectionChanged(scene) || manager.sectorGridSelectionChanged(scene) {
		t.Fatal("ordered duplicate map entries not captured stably")
	}
	scene.Objects = []*core.VoxelObject{obj}
	if !manager.sectorGridSelectionChanged(scene) {
		t.Fatal("duplicate removal not reflected")
	}
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	if !manager.sectorGridSelectionChanged(scene) {
		t.Fatal("coarse reselection not reflected")
	}
	coarse.SetVoxel(1, 0, 0, 1)
	if !manager.sectorGridSelectionChanged(scene) || manager.sectorGridSelectionChanged(scene) {
		t.Fatal("invalidated render map not removed from snapshot")
	}
	obj.ClearRenderRepresentation()
	if !manager.sectorGridSelectionChanged(scene) {
		t.Fatal("restored valid full map not reflected")
	}
	if !manager.sectorGridSelectionChanged(nil) || manager.sectorGridSelectionChanged(nil) {
		t.Fatal("nil-scene removal didn't empty retained snapshot")
	}
	if manager.sectorGridSelectionChanged(&core.Scene{Objects: []*core.VoxelObject{nil}}) {
		t.Fatal("nil objects counted as GPU map selection")
	}
}
