package bvh

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestTwoObjectsSplit(t *testing.T) {
	// Create two AABBs far apart
	aabbs := [][2]mgl32.Vec3{
		// Object 1 at -100
		{{-100, -1, -1}, {-98, 1, 1}},
		// Object 2 at 100
		{{100, -1, -1}, {102, 1, 1}},
	}

	builder := &TLASBuilder{}
	data := builder.Build(aabbs)

	// Should have Root, Left, Right (3 nodes total)
	// 64 bytes per node
	if len(data) != 64*3 {
		t.Fatalf("Expected 192 bytes (3 nodes), got %d", len(data))
	}

	// Parse Root node
	// struct BVHNode {
	//     aabb_min: vec4<f32>,  // 16 bytes at offset 0
	//     aabb_max: vec4<f32>,  // 16 bytes at offset 16
	//     left: i32,            // 4 bytes at offset 32
	//     right: i32,           // 4 bytes at offset 36
	//     leaf_first: i32,      // 4 bytes at offset 40
	//     leaf_count: i32,      // 4 bytes at offset 44
	//     padding: vec4<i32>,   // 16 bytes at offset 48
	// }

	// Root AABB should encompass both objects
	rootMin := make([]float32, 3)
	rootMax := make([]float32, 3)

	for i := 0; i < 3; i++ {
		rootMin[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4 : i*4+4]))
		rootMax[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[16+i*4 : 16+i*4+4]))
	}

	t.Logf("Root AABB: min=%v max=%v", rootMin, rootMax)

	if rootMin[0] > -100 {
		t.Errorf("Root min X should be <= -100, got %f", rootMin[0])
	}
	if rootMax[0] < 100 {
		t.Errorf("Root max X should be >= 100, got %f", rootMax[0])
	}

	// Check left and right indices
	leftIdx := int32(binary.LittleEndian.Uint32(data[32:36]))
	rightIdx := int32(binary.LittleEndian.Uint32(data[36:40]))

	t.Logf("Left index: %d, Right index: %d", leftIdx, rightIdx)

	if leftIdx == -1 {
		t.Error("Left index should not be -1 (should point to child)")
	}
	if rightIdx == -1 {
		t.Error("Right index should not be -1 (should point to child)")
	}
	if leftIdx == rightIdx {
		t.Error("Left and right indices should be different")
	}

	// Check children are leaves
	offsetL := leftIdx * 64
	lLeft := int32(binary.LittleEndian.Uint32(data[offsetL+32 : offsetL+36]))
	if lLeft != -1 {
		t.Errorf("Left child should be a leaf (left=-1), got %d", lLeft)
	}

	offsetR := rightIdx * 64
	rLeft := int32(binary.LittleEndian.Uint32(data[offsetR+32 : offsetR+36]))
	if rLeft != -1 {
		t.Errorf("Right child should be a leaf (left=-1), got %d", rLeft)
	}
}

func TestSingleObject(t *testing.T) {
	aabbs := [][2]mgl32.Vec3{
		{{0, 0, 0}, {1, 1, 1}},
	}

	builder := &TLASBuilder{}
	data := builder.Build(aabbs)

	// Should have 1 node (root is leaf)
	if len(data) != 64 {
		t.Fatalf("Expected 64 bytes (1 node), got %d", len(data))
	}

	// Root should be a leaf
	leftIdx := int32(binary.LittleEndian.Uint32(data[32:36]))
	rightIdx := int32(binary.LittleEndian.Uint32(data[36:40]))
	leafFirst := int32(binary.LittleEndian.Uint32(data[40:44]))
	leafCount := int32(binary.LittleEndian.Uint32(data[44:48]))

	if leftIdx != -1 || rightIdx != -1 {
		t.Error("Root should be a leaf (left and right = -1)")
	}
	if leafFirst != 0 || leafCount != 1 {
		t.Errorf("Leaf should reference object 0, got first=%d count=%d", leafFirst, leafCount)
	}
}

func TestEmptyBVH(t *testing.T) {
	aabbs := [][2]mgl32.Vec3{}

	builder := &TLASBuilder{}
	data := builder.Build(aabbs)

	// Should still create a minimal root node
	if len(data) < 64 {
		t.Fatalf("Expected at least 64 bytes, got %d", len(data))
	}
}
