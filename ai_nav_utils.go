package gekko

import (
	"container/heap"
	"fmt"
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

// PathNode for A*
type PathNode struct {
	X, Y    int
	G, H, F float32
	Parent  *PathNode
	index   int // for heap
}

type PriorityQueue []*PathNode

func (pq PriorityQueue) Len() int           { return len(pq) }
func (pq PriorityQueue) Less(i, j int) bool { return pq[i].F < pq[j].F }
func (pq PriorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}
func (pq *PriorityQueue) Push(x any) {
	n := len(*pq)
	item := x.(*PathNode)
	item.index = n
	*pq = append(*pq, item)
}
func (pq *PriorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	item.index = -1
	*pq = old[0 : n-1]
	return item
}

// FindPath finds a path between two world positions within a Region's NavGrid.
func (g *NavGrid) FindPath(startPos, endPos mgl32.Vec3, vSize float32) []mgl32.Vec3 {
	startX, startY := g.WorldToNavGrid(startPos, vSize)
	endX, endY := g.WorldToNavGrid(endPos, vSize)

	if g.GetNode(startX, startY) == nil || g.GetNode(endX, endY) == nil {
		fmt.Printf("DEBUG: FindPath direct nil node check failed\n")
		return nil
	}
	fmt.Printf("DEBUG: FindPath from %d,%d to %d,%d\n", startX, startY, endX, endY)

	openSet := &PriorityQueue{}
	heap.Init(openSet)

	startNode := &PathNode{X: startX, Y: startY, H: heuristic(startX, startY, endX, endY)}
	heap.Push(openSet, startNode)

	visited := make(map[[2]int]*PathNode)
	visited[[2]int{startX, startY}] = startNode

	iterations := 0
	for openSet.Len() > 0 {
		iterations++
		if iterations > 10000 { // Iteration limit is already 10000, no change needed here.
			fmt.Printf("DEBUG: FindPath reached iteration limit (%d nodes visited)\n", iterations)
			break
		}
		current := heap.Pop(openSet).(*PathNode)

		if current.X == endX && current.Y == endY {
			// Reconstruct path
			var path []mgl32.Vec3
			for current != nil {
				path = append([]mgl32.Vec3{g.NavGridToWorld(current.X, current.Y, vSize)}, path...)
				current = current.Parent
			}
			return path
		}

		// Neighbors
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dx == 0 && dy == 0 {
					continue
				}

				nx, ny := current.X+dx, current.Y+dy
				neighborNode := g.GetNode(nx, ny)
				if neighborNode == nil || !neighborNode.Walkable {
					continue
				}

				cost := float32(1.0)
				if dx != 0 && dy != 0 {
					cost = 1.414 // Diagonal
				}

				newG := current.G + cost
				key := [2]int{nx, ny}
				vNode, exists := visited[key]

				if !exists || newG < vNode.G {
					if !exists {
						vNode = &PathNode{X: nx, Y: ny}
						visited[key] = vNode
					}
					vNode.G = newG
					vNode.H = heuristic(nx, ny, endX, endY)
					vNode.F = vNode.G + vNode.H
					vNode.Parent = current
					if !exists {
						heap.Push(openSet, vNode)
					} else {
						// Update priority
						heap.Fix(openSet, vNode.index)
					}
				}
			}
		}
	}

	return nil
}

func heuristic(x1, y1, x2, y2 int) float32 {
	dx := float64(x1 - x2)
	dy := float64(y1 - y2)
	return float32(math.Sqrt(dx*dx + dy*dy))
}

// SteerSeek returns a velocity vector to move from current to target.
func SteerSeek(currentPos, targetPos mgl32.Vec3, maxSpeed float32) mgl32.Vec3 {
	desired := targetPos.Sub(currentPos)
	if desired.Len() < 0.001 {
		return mgl32.Vec3{0, 0, 0}
	}
	return desired.Normalize().Mul(maxSpeed)
}

// LOSProbe checks if there is direct line of sight between two points.
func LOSProbe(state *VoxelRtState, start, end mgl32.Vec3) bool {
	if state == nil {
		return false
	}
	diff := end.Sub(start)
	dist := diff.Len()
	if dist < 0.001 {
		return true
	}
	dir := diff.Normalize()
	hit := state.Raycast(start, dir, dist)
	return !hit.Hit
}
