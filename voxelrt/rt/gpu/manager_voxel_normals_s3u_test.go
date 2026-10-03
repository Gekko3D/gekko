package gpu

import (
	"bytes"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func s3uObject(voxel [3]int) *core.VoxelObject {
	object := core.NewVoxelObject()
	object.MaterialTable = []core.Material{core.DefaultMaterial()}
	object.XBrickMap.SetVoxel(voxel[0], voxel[1], voxel[2], 1)
	object.XBrickMap.ClearDirty()
	return object
}

func s3uAdjacency(object *core.VoxelObject, coord int) {
	object.VoxelAdjacencyGroupID, object.VoxelAdjacencyChunkSize = 12, 32
	object.VoxelAdjacencyChunkCoord = [3]int{coord, 0, 0}
}

func s3uPlanet(object *core.VoxelObject, tile int) {
	object.IsPlanetTile, object.PlanetTileGroupID = true, 21
	object.PlanetTileFace, object.PlanetTileLevel, object.PlanetTileX = 2, 1, tile
}

func s3uBrick(t *testing.T, object *core.VoxelObject, origin [3]int) *volume.Brick {
	t.Helper()
	sector := object.XBrickMap.Sectors[[3]int{floorDivInt(origin[0], 32), floorDivInt(origin[1], 32), floorDivInt(origin[2], 32)}]
	if sector == nil {
		t.Fatal("normal fixture has no requested sector")
	}
	brick := sector.GetBrick(positiveModInt(origin[0]/8, 4), positiveModInt(origin[1]/8, 4), positiveModInt(origin[2]/8, 4))
	if brick == nil {
		t.Fatal("normal fixture has no requested brick")
	}
	return brick
}

func s3uWork(t *testing.T, manager *GpuBufferManager, builds uint64, visits int) {
	t.Helper()
	if manager.VoxelNormalContextBuildCount != builds || manager.VoxelNormalContextObjectVisitsLastUpdate != visits {
		t.Fatalf("normal context work: builds=%d visits=%d, want %d/%d", manager.VoxelNormalContextBuildCount, manager.VoxelNormalContextObjectVisitsLastUpdate, builds, visits)
	}
}

func s3uAux(t *testing.T, getter func() voxelNormalBakeContext, scene *core.Scene, object *core.VoxelObject, origin [3]int) []byte {
	t.Helper()
	brick := s3uBrick(t, object, origin)
	got := buildVoxelAuxBytesWithContext(getter, object, brick, origin)
	want := buildVoxelAuxBytes(newVoxelNormalBakeContext(scene), object, brick, origin)
	if len(got) != volume.VoxelAuxRecordBytes || !bytes.Equal(got, want) {
		t.Fatal("lazy context changed packed occupancy/normal bytes")
	}
	return got
}

func s3uNormal(t *testing.T, data []byte, voxelIndex int, want mgl32.Vec3) {
	t.Helper()
	x, y, z, valid, _ := decodeBakedNormalWordForTest(bakedNormalWord(data, voxelIndex))
	if !valid || math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) ||
		math.Abs(x-float64(want[0])) > .03 || math.Abs(y-float64(want[1])) > .03 || math.Abs(z-float64(want[2])) > .03 {
		t.Fatalf("seam normal=(%.3f,%.3f,%.3f) valid=%t, want %v", x, y, z, valid, want)
	}
}

func TestS3uNormalContextIdleNilMaterialAndPrecomputedWorkBypassBuild(t *testing.T) {
	object := s3uObject([3]int{})
	s3uAdjacency(object, 0)
	manager, scene := scheduleFixture(t, object)
	manager.prepareVoxelStructureDirtyState(scene)
	manager.prepareVoxelNormalBakeContext(scene)
	s3uWork(t, manager, 0, 0)
	manager.prepareVoxelNormalBakeContext(nil)
	s3uWork(t, manager, 0, 0)
	// A false dirty entry is not an original dirty cross-object source.
	object.XBrickMap.DirtyBricks[[6]int{}] = false
	object.MaterialTable = []core.Material{core.DefaultMaterial(), {Roughness: .7}}
	manager.prepareVoxelNormalBakeContext(scene)
	materials := 0
	manager.serviceVoxelUploads(scene, func(work voxelUploadWork) bool {
		if work.kind != voxelUploadMaterial {
			t.Fatal("material-only fixture requested geometry execution")
		}
		materials++
		return true
	})
	if materials != 1 {
		t.Fatal("material-only fixture did not execute its actual upload unit")
	}
	s3uWork(t, manager, 0, 0)
	// Real changed geometry without cross-object metadata can upload a full
	// precomputed sidecar without constructing any neighbor context.
	object.VoxelAdjacencyGroupID = 0
	object.XBrickMap.SetVoxel(0, 0, 0, 2)
	precomputed := make([]byte, volume.VoxelAuxRecordBytes)
	precomputed[0], precomputed[len(precomputed)-1] = 1, 42
	s3uBrick(t, object, [3]int{}).PrecomputedAux = precomputed
	manager.prepareVoxelStructureDirtyState(scene)
	getter := manager.prepareVoxelNormalBakeContext(scene)
	geometry := 0
	manager.serviceVoxelUploads(scene, func(work voxelUploadWork) bool {
		if work.kind == voxelUploadMaterial {
			t.Fatal("precomputed fixture unexpectedly needed another material upload")
		}
		geometry++
		got := buildVoxelAuxBytesWithContext(getter, object, s3uBrick(t, object, [3]int{}), [3]int{})
		if !bytes.Equal(got, precomputed) {
			t.Fatal("precomputed auxiliary upload changed its supplied bytes")
		}
		return true
	})
	if geometry != 1 || manager.VoxelDirtyBricksPending != 0 {
		t.Fatal("precomputed upload did not complete its real dirty geometry unit")
	}
	s3uWork(t, manager, 0, 0)
}

func TestS3uNormalHaloRunsAtZeroUploadBudgetWithoutSameFrameCascade(t *testing.T) {
	source, immediate, distant := s3uObject([3]int{31, 0, 0}), s3uObject([3]int{}), s3uObject([3]int{})
	for index, object := range []*core.VoxelObject{source, immediate, distant} {
		s3uAdjacency(object, index)
		s3uPlanet(object, index)
	}
	manager, scene := scheduleFixture(t, immediate, distant)
	for _, object := range []*core.VoxelObject{immediate, distant} {
		s3uBrick(t, object, [3]int{}).PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes)
	}
	s3uBrick(t, source, [3]int{24, 0, 0}).PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes)
	// Source geometry arrives with clean flags, as decoded geometry can. New
	// allocation preparation must expose its dirty bricks before halo capture.
	scene.Objects = []*core.VoxelObject{nil, source, immediate, distant}
	scene.VisibleObjects = []*core.VoxelObject{source}
	manager.SetVoxelUploadBudget(VoxelUploadBudget{})
	manager.prepareVoxelStructureDirtyState(scene)
	getter := manager.prepareVoxelNormalBakeContext(scene)
	manager.serviceVoxelUploads(scene, func(voxelUploadWork) bool {
		t.Fatal("paused upload budget executed content work")
		return false
	})
	if !immediate.XBrickMap.DirtyBricks[[6]int{}] || s3uBrick(t, immediate, [3]int{}).PrecomputedAux != nil {
		t.Fatal("new source did not invalidate its hidden immediate normal halo")
	}
	if distant.XBrickMap.DirtyBricks[[6]int{}] || len(s3uBrick(t, distant, [3]int{}).PrecomputedAux) != volume.VoxelAuxRecordBytes {
		t.Fatal("newly dirtied tile cascaded beyond the original dirty source")
	}
	if manager.VoxelDirtyBricksPending == 0 || manager.VoxelBricksUploaded != 0 || manager.VoxelUploadBytes != 0 {
		t.Fatal("paused halo did not retain its pending upload ownership")
	}
	s3uWork(t, manager, 1, len(scene.Objects))
	s3uAux(t, getter, scene, immediate, [3]int{})
	s3uAux(t, getter, scene, source, [3]int{24, 0, 0})
	s3uWork(t, manager, 1, len(scene.Objects))
}

func TestS3uRuntimeNormalBytesUseLiveFullSceneNeighborsAndLastDuplicate(t *testing.T) {
	left, right, duplicate := s3uObject([3]int{31, 0, 0}), s3uObject([3]int{}), s3uObject([3]int{10, 0, 0})
	for index, object := range []*core.VoxelObject{left, right, duplicate} {
		coord := min(index, 1)
		s3uAdjacency(object, coord)
	}
	// Explicit adjacency must win over a simultaneously valid terrain fallback.
	left.IsTerrainChunk, left.TerrainGroupID, left.TerrainChunkSize = true, 77, 32
	terrainNeighbor := s3uObject([3]int{})
	terrainNeighbor.IsTerrainChunk, terrainNeighbor.TerrainGroupID, terrainNeighbor.TerrainChunkSize = true, 77, 32
	terrainNeighbor.TerrainChunkCoord = [3]int{1, 0, 0}
	plane, overlap := s3uObject([3]int{}), s3uObject([3]int{-1, 0, 0})
	s3uPlanet(plane, 0)
	s3uPlanet(overlap, 1)
	manager, scene := scheduleFixture(t, left, terrainNeighbor, right, duplicate, plane, overlap)
	scene.Objects = append([]*core.VoxelObject{nil}, scene.Objects...)
	scene.VisibleObjects = []*core.VoxelObject{left, plane}
	isolated := mgl32.Vec3{1, 1, 1}.Normalize()
	bake := func(wantLeft, wantPlanet mgl32.Vec3) {
		t.Helper()
		before := manager.VoxelNormalContextBuildCount
		manager.prepareVoxelStructureDirtyState(scene)
		getter := manager.prepareVoxelNormalBakeContext(scene)
		s3uWork(t, manager, before, 0)
		leftBytes := s3uAux(t, getter, scene, left, [3]int{24, 0, 0})
		s3uNormal(t, leftBytes, 7, wantLeft)
		planetBytes := s3uAux(t, getter, scene, plane, [3]int{})
		s3uNormal(t, planetBytes, 0, wantPlanet)
		s3uWork(t, manager, before+1, len(scene.Objects))
	}
	// Duplicate is last and lacks the boundary voxel. Hidden planet overlap
	// supplies -X occupancy and therefore a +X normal at the plane's voxel.
	bake(isolated, mgl32.Vec3{1, 0, 0})
	scene.Objects[3], scene.Objects[4] = scene.Objects[4], scene.Objects[3]
	bake(mgl32.Vec3{-1, 0, 0}, mgl32.Vec3{1, 0, 0})
	// Direct writes are observed by each invocation, without any dirty marker.
	right.VoxelAdjacencyChunkCoord = [3]int{3, 0, 0}
	overlap.Transform.Position = mgl32.Vec3{10, 0, 0}
	bake(isolated, isolated)
	left.VoxelAdjacencyGroupID = 0
	overlap.Transform.Position = mgl32.Vec3{}
	overlap.PlanetTileGroupID = 0
	bake(mgl32.Vec3{-1, 0, 0}, isolated)
	terrainNeighbor.TerrainGroupID = 0
	bake(isolated, isolated)
	before := manager.VoxelNormalContextBuildCount
	manager.prepareVoxelNormalBakeContext(nil)
	s3uWork(t, manager, before, 0)
}
