package core

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/go-gl/mathgl/mgl32"
)

func testSceneViewProj() mgl32.Mat4 {
	proj := mgl32.Perspective(mgl32.DegToRad(90), 1.0, 1.0, 100.0)
	view := mgl32.LookAtV(mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 0, -1}, mgl32.Vec3{0, 1, 0})
	return proj.Mul4(view)
}

func testSceneFrustumPlanes() [6]mgl32.Vec4 {
	return (&CameraState{}).ExtractFrustum(testSceneViewProj())
}

func TestTLASSeparation(t *testing.T) {
	scene := NewScene()

	obj1 := NewVoxelObject()
	obj1.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj1.Transform.Position = mgl32.Vec3{0, 0, 0}

	obj2 := NewVoxelObject()
	obj2.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj2.Transform.Position = mgl32.Vec3{100, 100, 100}

	scene.AddObject(obj1)
	scene.AddObject(obj2)
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})

	// Verify AABBs are separate
	if obj1.WorldAABB == nil || obj2.WorldAABB == nil {
		t.Fatal("World AABBs should be computed")
	}

	b1Min, b1Max := obj1.WorldAABB[0], obj1.WorldAABB[1]
	b2Min, b2Max := obj2.WorldAABB[0], obj2.WorldAABB[1]

	t.Logf("Object 1 AABB: %v -> %v", b1Min, b1Max)
	t.Logf("Object 2 AABB: %v -> %v", b2Min, b2Max)

	if b1Max[0] >= b2Min[0] {
		t.Errorf("Object 1 max X (%f) should be less than Object 2 min X (%f)", b1Max[0], b2Min[0])
	}
	if b1Max[1] >= b2Min[1] {
		t.Errorf("Object 1 max Y (%f) should be less than Object 2 min Y (%f)", b1Max[1], b2Min[1])
	}
	if b1Max[2] >= b2Min[2] {
		t.Errorf("Object 1 max Z (%f) should be less than Object 2 min Z (%f)", b1Max[2], b2Min[2])
	}
}

func TestMaterial(t *testing.T) {
	m := Material{
		BaseColor: [4]uint8{255, 0, 0, 255},
		Emissive:  [4]uint8{0, 0, 0, 0},
	}

	if m.BaseColor[0] != 255 {
		t.Errorf("Expected red=255, got %d", m.BaseColor[0])
	}
}

func TestTransformComposition(t *testing.T) {
	tr := NewTransform()
	tr.Position = mgl32.Vec3{10, 20, 30}
	tr.Scale = mgl32.Vec3{2, 2, 2}

	o2w := tr.ObjectToWorld()
	w2o := tr.WorldToObject()

	// Test that they are inverses
	identity := o2w.Mul4(w2o)

	// Check diagonal is close to 1
	for i := 0; i < 4; i++ {
		if !closeEnough(identity.At(i, i), 1.0, 0.001) {
			t.Errorf("Identity matrix element [%d,%d] should be 1.0, got %f", i, i, identity.At(i, i))
		}
	}
}

func TestVoxelObjectAABBUpdate(t *testing.T) {
	obj := NewVoxelObject()
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj.XBrickMap.SetVoxel(10, 10, 10, 1)

	// No transform, identity
	obj.UpdateWorldAABB()

	if obj.WorldAABB == nil {
		t.Fatal("World AABB should be computed")
	}

	minB, maxB := obj.WorldAABB[0], obj.WorldAABB[1]
	t.Logf("World AABB: %v -> %v", minB, maxB)

	// Should include both voxels (accounting for microcell size of 2)
	if maxB[0] < 10 || maxB[1] < 10 || maxB[2] < 10 {
		t.Errorf("AABB should include voxel at (10,10,10)")
	}
}

func TestSceneCommit(t *testing.T) {
	scene := NewScene()

	// Add objects
	for i := 0; i < 3; i++ {
		obj := NewVoxelObject()
		obj.XBrickMap.SetVoxel(i, i, i, 1)
		obj.Transform.Position = mgl32.Vec3{float32(i * 10), 0, 0}
		scene.AddObject(obj)
	}

	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})

	// Should have BVH data
	if len(scene.BVHNodesBytes) == 0 {
		t.Error("BVH should be built after commit")
	}

	t.Logf("BVH size: %d bytes", len(scene.BVHNodesBytes))

	// Each object should have a world AABB
	for i, obj := range scene.Objects {
		if obj.WorldAABB == nil {
			t.Errorf("Object %d should have world AABB", i)
		}
	}
}

func TestSharedXBrickMap(t *testing.T) {
	// Create shared geometry
	sharedMap := volume.NewXBrickMap()
	sharedMap.SetVoxel(0, 0, 0, 1)
	sharedMap.SetVoxel(1, 1, 1, 2)

	// Create two instances sharing the same XBrickMap
	obj1 := NewVoxelObject()
	obj1.XBrickMap = sharedMap
	obj1.Transform.Position = mgl32.Vec3{0, 0, 0}

	obj2 := NewVoxelObject()
	obj2.XBrickMap = sharedMap
	obj2.Transform.Position = mgl32.Vec3{100, 0, 0}

	scene := NewScene()
	scene.AddObject(obj1)
	scene.AddObject(obj2)
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})

	// Both should have different world AABBs despite sharing geometry
	if obj1.WorldAABB == nil || obj2.WorldAABB == nil {
		t.Fatal("Both objects should have world AABBs")
	}

	aabb1 := obj1.WorldAABB
	aabb2 := obj2.WorldAABB

	// They should not overlap (separated by 100 units in X)
	if aabb1[1][0] >= aabb2[0][0]-10 { // Some margin
		t.Error("Instances should have separate AABBs despite shared geometry")
	}
}

func TestSceneCommitSkipsHiZWhenObjectDisallowsOcclusion(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.AllowOcclusionCulling = false
	obj.Transform.Position = mgl32.Vec3{0, 0, -20}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	hiz := make([]float32, 16)
	for i := range hiz {
		hiz[i] = 1.0
	}

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		OcclusionMode: OcclusionConservative,
		HiZData:       hiz,
		HiZW:          4,
		HiZH:          4,
		LastViewProj:  testSceneViewProj(),
	})

	if len(scene.VisibleObjects) != 1 {
		t.Fatalf("expected non-occludable object to remain visible, got %d visible objects", len(scene.VisibleObjects))
	}
	if scene.OcclusionStats.HiZEligible != 0 {
		t.Fatalf("expected object to bypass Hi-Z eligibility, got %d eligible objects", scene.OcclusionStats.HiZEligible)
	}
}

func TestSceneCommitWarmupKeepsNewObjectVisible(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -20}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	hiz := make([]float32, 16)
	for i := range hiz {
		hiz[i] = 1.0
	}

	opts := SceneCommitOptions{
		OcclusionMode: OcclusionConservative,
		HiZData:       hiz,
		HiZW:          4,
		HiZH:          4,
		LastViewProj:  testSceneViewProj(),
	}

	for i := 0; i < occlusionWarmupFrames; i++ {
		scene.Commit(testSceneFrustumPlanes(), opts)
		if len(scene.VisibleObjects) != 1 {
			t.Fatalf("expected warmup frame %d to keep object visible", i)
		}
	}

	scene.Commit(testSceneFrustumPlanes(), opts)
	if len(scene.VisibleObjects) != 0 {
		t.Fatalf("expected object to be culled after warmup expires, got %d visible objects", len(scene.VisibleObjects))
	}
}

func TestSceneCommitHysteresisKeepsRecentlyVisibleObject(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -20}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	visibleHiZ := make([]float32, 16)
	for i := range visibleHiZ {
		visibleHiZ[i] = 100.0
	}
	occludedHiZ := make([]float32, 16)
	for i := range occludedHiZ {
		occludedHiZ[i] = 1.0
	}

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		OcclusionMode: OcclusionConservative,
		HiZData:       visibleHiZ,
		HiZW:          4,
		HiZH:          4,
		LastViewProj:  testSceneViewProj(),
	})
	if len(scene.VisibleObjects) != 1 {
		t.Fatal("expected object to be initially visible")
	}

	for i := 0; i < occlusionHysteresisFrames; i++ {
		scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
			OcclusionMode: OcclusionConservative,
			HiZData:       occludedHiZ,
			HiZW:          4,
			HiZH:          4,
			LastViewProj:  testSceneViewProj(),
		})
		if len(scene.VisibleObjects) != 1 {
			t.Fatalf("expected hysteresis frame %d to keep object visible", i)
		}
	}

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		OcclusionMode: OcclusionConservative,
		HiZData:       occludedHiZ,
		HiZW:          4,
		HiZH:          4,
		LastViewProj:  testSceneViewProj(),
	})
	if len(scene.VisibleObjects) != 0 {
		t.Fatalf("expected object to be culled after hysteresis expires, got %d visible objects", len(scene.VisibleObjects))
	}
}

func TestSceneCommitDisablesHiZDuringFastCameraMotion(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -20}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	hiz := make([]float32, 16)
	for i := range hiz {
		hiz[i] = 1.0
	}

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		OcclusionMode:    OcclusionConservative,
		HiZData:          hiz,
		HiZW:             4,
		HiZH:             4,
		LastViewProj:     testSceneViewProj(),
		FastCameraMotion: true,
	})

	if len(scene.VisibleObjects) != 1 {
		t.Fatalf("expected fast camera motion to bypass Hi-Z culling, got %d visible objects", len(scene.VisibleObjects))
	}
	if scene.OcclusionStats.HiZMotionDisabled != 1 {
		t.Fatalf("expected fast camera motion stat to be recorded, got %d", scene.OcclusionStats.HiZMotionDisabled)
	}
}

// Helper function
func closeEnough(a, b, epsilon float32) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
