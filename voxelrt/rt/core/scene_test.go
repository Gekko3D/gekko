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

func testShadowCastingDirectionalLight() Light {
	return Light{
		Direction:    [4]float32{0, -1, 0, 0},
		Params:       [4]float32{0, 0, float32(LightTypeDirectional), 0},
		CastsShadows: true,
	}
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

func TestTransformMatrixCacheInvalidatesOnDirty(t *testing.T) {
	tr := NewTransform()
	initialO2W := tr.ObjectToWorld()
	initialW2O := tr.WorldToObject()

	if !tr.matricesValid {
		t.Fatal("expected transform matrices to be cached after first use")
	}

	tr.Dirty = false
	cachedO2W := tr.ObjectToWorld()
	cachedW2O := tr.WorldToObject()
	if cachedO2W != initialO2W {
		t.Fatalf("expected cached object-to-world matrix to be reused")
	}
	if cachedW2O != initialW2O {
		t.Fatalf("expected cached world-to-object matrix to be reused")
	}

	tr.Position = mgl32.Vec3{3, 4, 5}
	tr.Dirty = true
	updatedO2W := tr.ObjectToWorld()
	updatedW2O := tr.WorldToObject()
	if updatedO2W == initialO2W {
		t.Fatalf("expected dirty transform to recompute object-to-world matrix")
	}
	if updatedW2O == initialW2O {
		t.Fatalf("expected dirty transform to recompute world-to-object matrix")
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

func TestSceneCommitSkipsBVHRebuildOnStableFrame(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -10}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})
	visibleRevision := scene.visibleBVHRevision
	transparentRevision := scene.transparentBVHRevision
	shadowRevision := scene.shadowBVHRevision

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})

	if scene.visibleBVHRevision != visibleRevision {
		t.Fatalf("expected visible BVH revision to remain %d on stable frame, got %d", visibleRevision, scene.visibleBVHRevision)
	}
	if scene.transparentBVHRevision != transparentRevision {
		t.Fatalf("expected transparent BVH revision to remain %d on stable frame, got %d", transparentRevision, scene.transparentBVHRevision)
	}
	if scene.shadowBVHRevision != shadowRevision {
		t.Fatalf("expected shadow BVH revision to remain %d on stable frame, got %d", shadowRevision, scene.shadowBVHRevision)
	}
}

func TestSceneCommitRebuildsBVHWhenVisibleObjectChangesAABB(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}
	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -10}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})
	visibleRevision := scene.visibleBVHRevision
	transparentRevision := scene.transparentBVHRevision
	shadowRevision := scene.shadowBVHRevision

	obj.Transform.Position = mgl32.Vec3{2, 0, -10}
	obj.Transform.Dirty = true

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})

	if scene.visibleBVHRevision != visibleRevision+1 {
		t.Fatalf("expected visible BVH revision to increment to %d, got %d", visibleRevision+1, scene.visibleBVHRevision)
	}
	if scene.transparentBVHRevision != transparentRevision {
		t.Fatalf("expected transparent BVH revision to remain %d for opaque-only scene, got %d", transparentRevision, scene.transparentBVHRevision)
	}
	if scene.shadowBVHRevision != shadowRevision+1 {
		t.Fatalf("expected shadow BVH revision to increment to %d, got %d", shadowRevision+1, scene.shadowBVHRevision)
	}
}

func TestSceneCommitBuildsTransparentSubsetAndBVH(t *testing.T) {
	scene := NewScene()

	opaque := NewVoxelObject()
	opaque.Transform.Position = mgl32.Vec3{0, 0, -10}
	opaque.XBrickMap.SetVoxel(0, 0, 0, 1)
	opaque.MaterialTable = []Material{
		DefaultMaterial(),
	}

	glass := NewVoxelObject()
	glass.Transform.Position = mgl32.Vec3{2, 0, -10}
	glass.XBrickMap.SetVoxel(0, 0, 0, 1)
	glass.MaterialTable = []Material{
		DefaultMaterial(),
		{Transparency: 0.4},
	}

	scene.AddObject(opaque)
	scene.AddObject(glass)
	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})

	if got := len(scene.VisibleObjects); got != 2 {
		t.Fatalf("expected 2 visible objects, got %d", got)
	}
	if got := len(scene.TransparentVisibleObjects); got != 1 {
		t.Fatalf("expected 1 transparent visible object, got %d", got)
	}
	if scene.TransparentVisibleObjects[0] != glass {
		t.Fatalf("expected glass object in transparent subset")
	}
	if len(scene.TransparentBVHNodesBytes) == 0 {
		t.Fatal("expected transparent BVH to be built")
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

func TestSceneCommitKeepsOffFrustumObjectsAsShadowCasters(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}

	visible := NewVoxelObject()
	visible.Transform.Position = mgl32.Vec3{0, 0, -20}
	visible.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(visible)

	offFrustumCaster := NewVoxelObject()
	offFrustumCaster.Transform.Position = mgl32.Vec3{200, 0, -20}
	offFrustumCaster.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(offFrustumCaster)

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})

	if len(scene.VisibleObjects) != 1 {
		t.Fatalf("expected only the on-screen object to remain visible, got %d", len(scene.VisibleObjects))
	}
	if len(scene.ShadowObjects) != 2 {
		t.Fatalf("expected both loaded objects to remain shadow casters, got %d", len(scene.ShadowObjects))
	}
	if len(scene.ShadowBVHNodesBytes) == 0 {
		t.Fatal("expected shadow BVH to be built")
	}
}

func TestSceneCommitRespectsShadowFlagsAndDistance(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}

	disabled := NewVoxelObject()
	disabled.Transform.Position = mgl32.Vec3{0, 0, -10}
	disabled.XBrickMap.SetVoxel(0, 0, 0, 1)
	disabled.CastsShadows = false
	scene.AddObject(disabled)

	near := NewVoxelObject()
	near.Transform.Position = mgl32.Vec3{0, 0, -12}
	near.XBrickMap.SetVoxel(0, 0, 0, 1)
	near.ShadowMaxDistance = 20
	scene.AddObject(near)

	far := NewVoxelObject()
	far.Transform.Position = mgl32.Vec3{0, 0, -60}
	far.XBrickMap.SetVoxel(0, 0, 0, 1)
	far.ShadowMaxDistance = 20
	scene.AddObject(far)

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		CameraPosition: mgl32.Vec3{0, 0, 0},
	})

	if len(scene.ShadowObjects) != 1 {
		t.Fatalf("expected only one shadow caster after filtering, got %d", len(scene.ShadowObjects))
	}
	if scene.ShadowObjects[0] != near {
		t.Fatal("expected only the near shadow-enabled object to remain a shadow caster")
	}
}

func TestSceneCommitRebuildsShadowBVHWhenShadowCasterSetChanges(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}
	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -10}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})
	visibleRevision := scene.visibleBVHRevision
	shadowRevision := scene.shadowBVHRevision

	obj.CastsShadows = false

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})

	if scene.visibleBVHRevision != visibleRevision {
		t.Fatalf("expected visible BVH revision to remain %d, got %d", visibleRevision, scene.visibleBVHRevision)
	}
	if scene.shadowBVHRevision != shadowRevision+1 {
		t.Fatalf("expected shadow BVH revision to increment to %d, got %d", shadowRevision+1, scene.shadowBVHRevision)
	}
}

func TestSceneCommitCapsShadowCastersPerGroupByNearestDistance(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}

	near := NewVoxelObject()
	near.Transform.Position = mgl32.Vec3{0, 0, -8}
	near.XBrickMap.SetVoxel(0, 0, 0, 1)
	near.ShadowCasterGroupID = 77
	near.ShadowCasterGroupLimit = 2
	scene.AddObject(near)

	mid := NewVoxelObject()
	mid.Transform.Position = mgl32.Vec3{0, 0, -16}
	mid.XBrickMap.SetVoxel(0, 0, 0, 1)
	mid.ShadowCasterGroupID = 77
	mid.ShadowCasterGroupLimit = 2
	scene.AddObject(mid)

	far := NewVoxelObject()
	far.Transform.Position = mgl32.Vec3{0, 0, -32}
	far.XBrickMap.SetVoxel(0, 0, 0, 1)
	far.ShadowCasterGroupID = 77
	far.ShadowCasterGroupLimit = 2
	scene.AddObject(far)

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		CameraPosition: mgl32.Vec3{0, 0, 0},
	})

	if len(scene.ShadowObjects) != 2 {
		t.Fatalf("expected 2 capped shadow casters, got %d", len(scene.ShadowObjects))
	}
	if scene.ShadowObjects[0] != near && scene.ShadowObjects[1] != near {
		t.Fatal("expected nearest object to survive capped shadow selection")
	}
	if scene.ShadowObjects[0] != mid && scene.ShadowObjects[1] != mid {
		t.Fatal("expected second-nearest object to survive capped shadow selection")
	}
	if scene.ShadowObjects[0] == far || scene.ShadowObjects[1] == far {
		t.Fatal("expected farthest object to be dropped by capped shadow selection")
	}
}

func TestSceneCommitSkipsShadowCasterWorkWithoutShadowCastingLights(t *testing.T) {
	scene := NewScene()

	obj := NewVoxelObject()
	obj.Transform.Position = mgl32.Vec3{0, 0, -10}
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	scene.AddObject(obj)

	scene.Lights = []Light{
		{
			Params:       [4]float32{12, 0, float32(LightTypePoint), 0},
			CastsShadows: false,
		},
	}

	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{
		CameraPosition: mgl32.Vec3{0, 0, 0},
	})

	if len(scene.ShadowObjects) != 0 {
		t.Fatalf("expected no shadow casters when lights do not cast shadows, got %d", len(scene.ShadowObjects))
	}
	if len(scene.ShadowBVHNodesBytes) != 64 {
		t.Fatalf("expected empty shadow BVH payload, got %d bytes", len(scene.ShadowBVHNodesBytes))
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
