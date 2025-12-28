package core

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"

	"github.com/go-gl/mathgl/mgl32"
)

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
	scene.Commit([6]mgl32.Vec4{})

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

	scene.Commit([6]mgl32.Vec4{})

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
	scene.Commit([6]mgl32.Vec4{})

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

// Helper function
func closeEnough(a, b, epsilon float32) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < epsilon
}
