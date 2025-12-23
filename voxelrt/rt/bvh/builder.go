package bvh

import (
	"encoding/binary"
	"math"
	"sort"

	"github.com/go-gl/mathgl/mgl32"
)

// Matches WGSL BVHNode
// struct BVHNode {
//    aabb_min : vec4<f32>; (16)
//    aabb_max : vec4<f32>; (16)
//    left : i32; (4)
//    right : i32; (4)
//    leaf_first : i32; (4)
//    leaf_count : i32; (4)
//    padding : i32[2]; (8)
// }; -> 64 bytes

type BVHNode struct {
	Min       mgl32.Vec3
	Max       mgl32.Vec3
	Left      int32
	Right     int32
	LeafFirst int32
	LeafCount int32
}

func (n *BVHNode) ToBytes() []byte {
	buf := make([]byte, 64)

	// Min (vec4)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(n.Min.X()))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(n.Min.Y()))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(n.Min.Z()))
	binary.LittleEndian.PutUint32(buf[12:16], 0)

	// Max (vec4)
	binary.LittleEndian.PutUint32(buf[16:20], math.Float32bits(n.Max.X()))
	binary.LittleEndian.PutUint32(buf[20:24], math.Float32bits(n.Max.Y()))
	binary.LittleEndian.PutUint32(buf[24:28], math.Float32bits(n.Max.Z()))
	binary.LittleEndian.PutUint32(buf[28:32], 0)

	// Ints
	binary.LittleEndian.PutUint32(buf[32:36], uint32(n.Left))
	binary.LittleEndian.PutUint32(buf[36:40], uint32(n.Right))
	binary.LittleEndian.PutUint32(buf[40:44], uint32(n.LeafFirst))
	binary.LittleEndian.PutUint32(buf[44:48], uint32(n.LeafCount))

	// Padding
	return buf
}

type AABBItem struct {
	Min      mgl32.Vec3
	Max      mgl32.Vec3
	Centroid mgl32.Vec3
	Index    int
}

type TLASBuilder struct{}

func (b *TLASBuilder) Build(aabbs [][2]mgl32.Vec3) []byte {
	if len(aabbs) == 0 {
		return make([]byte, 64)
	}

	items := make([]AABBItem, len(aabbs))
	for i, bounds := range aabbs {
		items[i] = AABBItem{
			Min:      bounds[0],
			Max:      bounds[1],
			Centroid: bounds[0].Add(bounds[1]).Mul(0.5),
			Index:    i,
		}
	}

	nodes := []BVHNode{}
	b.recursiveBuild(items, &nodes)

	out := []byte{}
	for _, n := range nodes {
		out = append(out, n.ToBytes()...)
	}
	return out
}

func (b *TLASBuilder) recursiveBuild(items []AABBItem, nodes *[]BVHNode) int32 {
	idx := int32(len(*nodes))
	*nodes = append(*nodes, BVHNode{Left: -1, Right: -1, LeafFirst: -1, LeafCount: 0})

	// Compute bounds
	minB := mgl32.Vec3{float32(math.Inf(1)), float32(math.Inf(1)), float32(math.Inf(1))}
	maxB := mgl32.Vec3{float32(math.Inf(-1)), float32(math.Inf(-1)), float32(math.Inf(-1))}

	for _, it := range items {
		minB = mgl32.Vec3{min(minB.X(), it.Min.X()), min(minB.Y(), it.Min.Y()), min(minB.Z(), it.Min.Z())}
		maxB = mgl32.Vec3{max(maxB.X(), it.Max.X()), max(maxB.Y(), it.Max.Y()), max(maxB.Z(), it.Max.Z())}
	}

	(*nodes)[idx].Min = minB
	(*nodes)[idx].Max = maxB

	if len(items) == 1 {
		(*nodes)[idx].LeafFirst = int32(items[0].Index)
		(*nodes)[idx].LeafCount = 1
		return idx
	}

	// Split
	extent := maxB.Sub(minB)
	axis := 0
	if extent.Y() > extent.X() {
		axis = 1
	}
	if extent.Z() > extent[axis] {
		axis = 2
	} // Fix: access vector by index? mgl32 Vec3 is array? No it's struct.
	// mgl32 Vec3 is [3]float32 type alias actually. So index works.

	sort.Slice(items, func(i, j int) bool {
		return items[i].Centroid[axis] < items[j].Centroid[axis]
	})

	mid := len(items) / 2
	(*nodes)[idx].Left = b.recursiveBuild(items[:mid], nodes)
	(*nodes)[idx].Right = b.recursiveBuild(items[mid:], nodes)

	return idx
}

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
