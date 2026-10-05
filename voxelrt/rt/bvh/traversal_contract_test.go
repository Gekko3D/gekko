package bvh

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/bits"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

// Decode the public GPU buffer rather than examining the builder's private tree.
func decodeTLAS(t *testing.T, data []byte) []BVHNode {
	t.Helper()
	if len(data)%64 != 0 {
		t.Fatalf("TLAS buffer length %d is not a multiple of the node stride", len(data))
	}
	nodes := make([]BVHNode, len(data)/64)
	for i := range nodes {
		row := data[i*64 : (i+1)*64]
		for axis := 0; axis < 3; axis++ {
			nodes[i].Min[axis] = math.Float32frombits(binary.LittleEndian.Uint32(row[axis*4:]))
			nodes[i].Max[axis] = math.Float32frombits(binary.LittleEndian.Uint32(row[16+axis*4:]))
		}
		nodes[i].Left = int32(binary.LittleEndian.Uint32(row[32:]))
		nodes[i].Right = int32(binary.LittleEndian.Uint32(row[36:]))
		nodes[i].LeafFirst = int32(binary.LittleEndian.Uint32(row[40:]))
		nodes[i].LeafCount = int32(binary.LittleEndian.Uint32(row[44:]))
		for _, padding := range [][]byte{row[12:16], row[28:32], row[48:64]} {
			if !bytes.Equal(padding, make([]byte, len(padding))) {
				t.Fatalf("node %d has nonzero GPU padding", i)
			}
		}
	}
	return nodes
}

func TestTLASCompleteBalancedTraversalContract(t *testing.T) {
	for _, count := range []int{1, 2, 3, 7, 8, 9, 31, 32, 33, 127, 128, 129, 511, 512, 513, 1023, 1024, 1025, 4097} {
		for _, shape := range []string{"permuted", "overlapping", "degenerate"} {
			t.Run(fmt.Sprintf("%s/%d", shape, count), func(t *testing.T) {
				bounds := make([][2]mgl32.Vec3, count)
				for i := range bounds {
					// Reverse spatial order to catch loss of original instance identity.
					x := float32(count - i)
					switch shape {
					case "permuted":
						bounds[i] = [2]mgl32.Vec3{{x, float32(i % 5), -x}, {x + 1, float32(i%5) + 2, -x + 3}}
					case "overlapping":
						bounds[i] = [2]mgl32.Vec3{{-x, -x, -x}, {x, x, x}}
					case "degenerate":
						bounds[i] = [2]mgl32.Vec3{{3, 3, 3}, {3, 3, 3}}
					}
				}
				nodes := decodeTLAS(t, (&TLASBuilder{}).Build(bounds))
				if len(nodes) != 2*count-1 {
					t.Fatalf("got %d nodes for %d one-instance leaves", len(nodes), count)
				}
				seenNodes := make([]bool, len(nodes))
				seenLeaves := make([]bool, count)
				var visit func(int32) (int, int, [2]mgl32.Vec3)
				visit = func(index int32) (int, int, [2]mgl32.Vec3) {
					if index < 0 || int(index) >= len(nodes) {
						t.Fatalf("invalid child index %d", index)
					}
					if seenNodes[index] {
						t.Fatalf("node %d is cyclic or has multiple parents", index)
					}
					seenNodes[index] = true
					n := nodes[index]
					if n.LeafCount != 0 {
						if n.LeafCount != 1 || n.Left != -1 || n.Right != -1 || n.LeafFirst < 0 || int(n.LeafFirst) >= count {
							t.Fatalf("invalid one-instance leaf %d: %+v", index, n)
						}
						if seenLeaves[n.LeafFirst] {
							t.Fatalf("instance %d is repeated", n.LeafFirst)
						}
						seenLeaves[n.LeafFirst] = true
						want := bounds[n.LeafFirst]
						if n.Min != want[0] || n.Max != want[1] {
							t.Fatalf("instance %d lost original bounds", n.LeafFirst)
						}
						return 1, 1, want
					}
					if n.LeafFirst != -1 {
						t.Fatalf("interior node %d has leaf identity", index)
					}
					leftCount, leftPending, leftBounds := visit(n.Left)
					rightCount, rightPending, rightBounds := visit(n.Right)
					if difference := leftCount - rightCount; difference < -1 || difference > 1 {
						t.Fatalf("node %d is not median-balanced: %d/%d leaves", index, leftCount, rightCount)
					}
					union := leftBounds
					for axis := 0; axis < 3; axis++ {
						union[0][axis] = min(union[0][axis], rightBounds[0][axis])
						union[1][axis] = max(union[1][axis], rightBounds[1][axis])
					}
					if n.Min != union[0] || n.Max != union[1] {
						t.Fatalf("node %d does not exactly bound its descendants", index)
					}
					// Either child can be visited first. Its sibling waits on the
					// stack, so the worst pending count over ALL local orders is
					// one plus the larger child's worst count, not just a single
					// globally left-first or right-first traversal measurement.
					pending := leftPending
					if rightPending > pending {
						pending = rightPending
					}
					return leftCount + rightCount, pending + 1, union
				}
				leafCount, pending, _ := visit(0)
				if leafCount != count {
					t.Fatalf("root has %d leaves, want %d", leafCount, count)
				}
				for i, seen := range seenNodes {
					if !seen {
						t.Fatalf("unreachable node %d", i)
					}
				}
				for i, seen := range seenLeaves {
					if !seen {
						t.Fatalf("missing instance %d", i)
					}
				}
				if want := bits.Len(uint(count-1)) + 1; pending != want {
					t.Fatalf("worst DFS stack occupancy %d, want ceil(log2(%d))+1 = %d", pending, count, want)
				}
			})
		}
	}
}

func TestTLASEmptyPaddedSentinel(t *testing.T) {
	for _, bounds := range [][][2]mgl32.Vec3{nil, {}} {
		data := (&TLASBuilder{}).Build(bounds)
		if !bytes.Equal(data, make([]byte, 64)) {
			t.Fatalf("empty TLAS must retain its single all-zero padded sentinel, got %x", data)
		}
	}
}

// rayBoxEntry is an independent slab oracle. In particular a ray parallel to
// a slab has no reciprocal infinity/NaN dependency.
func rayBoxEntry(origin, direction mgl32.Vec3, bounds [2]mgl32.Vec3) (float32, bool) {
	near, far := float32(0), float32(math.Inf(1))
	for axis := 0; axis < 3; axis++ {
		if direction[axis] == 0 {
			if origin[axis] < bounds[0][axis] || origin[axis] > bounds[1][axis] {
				return 0, false
			}
			continue
		}
		a := (bounds[0][axis] - origin[axis]) / direction[axis]
		b := (bounds[1][axis] - origin[axis]) / direction[axis]
		if a > b {
			a, b = b, a
		}
		near, far = max(near, a), min(far, b)
	}
	return near, near <= far
}

type candidateResult struct {
	instance int
	distance float32
	visits   int
	pruned   int
}

// Leaf distances model the inner voxel traversal's result: a bounds hit need
// not contain a voxel hit. This deliberately tests only scene-level traversal.
func bruteForceCandidate(bounds [][2]mgl32.Vec3, distances []float32, origin, direction mgl32.Vec3) candidateResult {
	result := candidateResult{instance: -1, distance: float32(math.Inf(1))}
	for i, box := range bounds {
		entry, hit := rayBoxEntry(origin, direction, box)
		if hit && entry <= distances[i] && distances[i] < result.distance {
			result.instance, result.distance = i, distances[i]
		}
	}
	return result
}

func nearFirstCandidate(nodes []BVHNode, distances []float32, origin, direction mgl32.Vec3, visitLimit int) candidateResult {
	result := candidateResult{instance: -1, distance: float32(math.Inf(1))}
	// An empty scene keeps a zero root even when its GPU allocation retains
	// stale trailing rows. The root, not buffer capacity, identifies emptiness.
	if len(nodes) == 0 || (nodes[0].LeafCount == 0 && nodes[0].Left <= 0 && nodes[0].Right <= 0) {
		return result
	}
	stack := []int32{0}
	for len(stack) != 0 && (visitLimit == 0 || result.visits < visitLimit) {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		result.visits++
		n := nodes[index]
		entry, hit := rayBoxEntry(origin, direction, [2]mgl32.Vec3{n.Min, n.Max})
		if !hit || entry > result.distance {
			result.pruned++
			continue
		}
		if n.LeafCount == 1 {
			distance := distances[n.LeafFirst]
			if entry <= distance && distance < result.distance {
				result.instance, result.distance = int(n.LeafFirst), distance
			}
			continue
		}
		left, right := nodes[n.Left], nodes[n.Right]
		leftEntry, leftHit := rayBoxEntry(origin, direction, [2]mgl32.Vec3{left.Min, left.Max})
		rightEntry, rightHit := rayBoxEntry(origin, direction, [2]mgl32.Vec3{right.Min, right.Max})
		if leftHit && rightHit {
			// Match the shader tie policy: equal entry distances visit right first.
			if leftEntry < rightEntry {
				stack = append(stack, n.Right, n.Left)
			} else {
				stack = append(stack, n.Left, n.Right)
			}
		} else if leftHit {
			stack = append(stack, n.Left)
		} else if rightHit {
			stack = append(stack, n.Right)
		}
	}
	return result
}

func TestTLASEmptyTraversalIgnoresRetainedCapacity(t *testing.T) {
	root := decodeTLAS(t, (&TLASBuilder{}).Build(nil))[0]
	for _, capacity := range []int{1, 17} {
		nodes := make([]BVHNode, capacity)
		nodes[0] = root
		for i := 1; i < len(nodes); i++ {
			// Model stale, formerly live rows after a populated scene is cleared.
			nodes[i] = BVHNode{Min: mgl32.Vec3{-1, -1, -1}, Max: mgl32.Vec3{1, 1, 1}, Left: -1, Right: -1, LeafCount: 1, LeafFirst: 0}
		}
		origin, direction := mgl32.Vec3{0, 0, 10}, mgl32.Vec3{0, 0, -1}
		if _, hit := rayBoxEntry(origin, direction, [2]mgl32.Vec3{root.Min, root.Max}); !hit {
			t.Fatal("empty fixture must intersect the zero root bounds")
		}
		got := nearFirstCandidate(nodes, []float32{9}, origin, direction, 0)
		if got.instance != -1 || !math.IsInf(float64(got.distance), 1) || got.visits != 0 {
			t.Fatalf("empty scene with %d retained rows did not terminate as a miss: %+v", capacity, got)
		}
	}
}

func TestTLASNearestCandidateBeyondVisitBudget(t *testing.T) {
	const count = 1025
	bounds := make([][2]mgl32.Vec3, count)
	distances := make([]float32, count)
	for i := range bounds {
		// Nested boxes share an entry distance but have unique ordered
		// centroids. With right-first entry ties the leftmost leaf is last.
		bounds[i] = [2]mgl32.Vec3{{1, -1, -1}, {float32(i + 2), 1, 1}}
		distances[i] = float32(math.Inf(1))
	}
	origin, direction := mgl32.Vec3{}, mgl32.Vec3{1, 0, 0}
	nodes := decodeTLAS(t, (&TLASBuilder{}).Build(bounds))
	allMiss := nearFirstCandidate(nodes, distances, origin, direction, 0)
	if allMiss.instance != -1 || !math.IsInf(float64(allMiss.distance), 1) || allMiss.visits != len(nodes) {
		t.Fatalf("all inner-voxel misses must exhaust the complete tree: %+v, nodes=%d", allMiss, len(nodes))
	}
	distances[0] = 1.5
	want := bruteForceCandidate(bounds, distances, origin, direction)
	got := nearFirstCandidate(nodes, distances, origin, direction, 0)
	if want.instance != 0 || got.instance != want.instance || got.distance != want.distance {
		t.Fatalf("exhaustive DFS %+v does not match brute force %+v beyond 512 visits", got, want)
	}
	if got.visits <= 512 {
		t.Fatalf("late-hit fixture must require more than 512 visits: %+v", got)
	}
	for _, limit := range []int{128, 512} {
		limited := nearFirstCandidate(nodes, distances, origin, direction, limit)
		if limited.instance != -1 || !math.IsInf(float64(limited.distance), 1) {
			t.Fatalf("fixture failed to expose truncation at %d visits: %+v", limit, limited)
		}
	}
}

func TestTLASNearestCandidateRejectsBoundsMisses(t *testing.T) {
	bounds := [][2]mgl32.Vec3{
		{{1, 2, -1}, {2, 3, 1}}, // Off the ray, despite a nearer candidate distance.
		{{7, -1, -1}, {9, 1, 1}},
		{{-3, -1, -1}, {-1, 1, 1}}, // Entirely behind the ray origin.
	}
	distances := []float32{1.5, 8, 1}
	origin, direction := mgl32.Vec3{}, mgl32.Vec3{1, 0, 0}
	want := bruteForceCandidate(bounds, distances, origin, direction)
	got := nearFirstCandidate(decodeTLAS(t, (&TLASBuilder{}).Build(bounds)), distances, origin, direction, 0)
	if want.instance != 1 || got.instance != want.instance || got.distance != want.distance {
		t.Fatalf("bounds misses admitted an invalid candidate: DFS %+v, brute force %+v", got, want)
	}
}

func TestTLASNearestCandidatePreservesPruning(t *testing.T) {
	const count = 513
	bounds := make([][2]mgl32.Vec3, count)
	distances := make([]float32, count)
	for i := range bounds {
		// Reverse instance order to require near-first traversal of the right
		// spatial child for negative-x rays, and the left for positive-x rays.
		x := float32((count - i) * 3)
		bounds[i] = [2]mgl32.Vec3{{x, -1, -1}, {x + 1, 1, 1}}
	}
	nodes := decodeTLAS(t, (&TLASBuilder{}).Build(bounds))
	for _, direction := range []mgl32.Vec3{{1, 0, 0}, {-1, 0, 0}} {
		origin := mgl32.Vec3{}
		if direction[0] < 0 {
			origin[0] = count*3 + 10
		}
		for i, box := range bounds {
			entry, _ := rayBoxEntry(origin, direction, box)
			distances[i] = entry + 0.5
		}
		want := bruteForceCandidate(bounds, distances, origin, direction)
		got := nearFirstCandidate(nodes, distances, origin, direction, 0)
		if got.instance != want.instance || got.distance != want.distance {
			t.Fatalf("direction %v: DFS %+v != brute force %+v", direction, got, want)
		}
		if got.pruned == 0 || got.visits >= len(nodes) {
			t.Fatalf("direction %v: closest-distance pruning was not exercised: %+v", direction, got)
		}
	}
}
