package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type AABBComponent struct {
	Min mgl32.Vec3
	Max mgl32.Vec3
}

const maxFreeSpatialBuckets = 4096

type SpatialHashGrid struct {
	cellSize    float32
	cells       map[uint64][]EntityId
	freeBuckets [][]EntityId
}

func NewSpatialHashGrid(cellSize float32) *SpatialHashGrid {
	return &SpatialHashGrid{
		cellSize:    cellSize,
		cells:       make(map[uint64][]EntityId),
		freeBuckets: make([][]EntityId, 0, 64),
	}
}

func (grid *SpatialHashGrid) Clear() {
	for key, bucket := range grid.cells {
		if cap(bucket) > 0 && len(grid.freeBuckets) < maxFreeSpatialBuckets {
			grid.freeBuckets = append(grid.freeBuckets, bucket[:0])
		}
		delete(grid.cells, key)
	}
}

func (grid *SpatialHashGrid) Insert(id EntityId, aabb AABBComponent) {
	minX, maxX := grid.getCellIndex(aabb.Min.X()), grid.getCellIndex(aabb.Max.X())
	minY, maxY := grid.getCellIndex(aabb.Min.Y()), grid.getCellIndex(aabb.Max.Y())
	minZ, maxZ := grid.getCellIndex(aabb.Min.Z()), grid.getCellIndex(aabb.Max.Z())

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				key := grid.hashKey(x, y, z)
				bucket, ok := grid.cells[key]
				if !ok && len(grid.freeBuckets) > 0 {
					last := len(grid.freeBuckets) - 1
					bucket = grid.freeBuckets[last]
					grid.freeBuckets = grid.freeBuckets[:last]
				}
				bucket = append(bucket, id)
				grid.cells[key] = bucket
			}
		}
	}
}

func (grid *SpatialHashGrid) QueryAABB(aabb AABBComponent) []EntityId {
	return grid.QueryAABBInto(aabb, make(map[EntityId]struct{}), nil)
}

func (grid *SpatialHashGrid) QueryAABBInto(aabb AABBComponent, unique map[EntityId]struct{}, results []EntityId) []EntityId {
	minX, maxX := grid.getCellIndex(aabb.Min.X()), grid.getCellIndex(aabb.Max.X())
	minY, maxY := grid.getCellIndex(aabb.Min.Y()), grid.getCellIndex(aabb.Max.Y())
	minZ, maxZ := grid.getCellIndex(aabb.Min.Z()), grid.getCellIndex(aabb.Max.Z())

	clear(unique)
	results = results[:0]

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				key := grid.hashKey(x, y, z)
				for _, id := range grid.cells[key] {
					if _, ok := unique[id]; ok {
						continue
					}
					unique[id] = struct{}{}
					results = append(results, id)
				}
			}
		}
	}
	return results
}

func (grid *SpatialHashGrid) QueryRadius(center mgl32.Vec3, radius float32) []EntityId {
	aabb := AABBComponent{
		Min: center.Sub(mgl32.Vec3{radius, radius, radius}),
		Max: center.Add(mgl32.Vec3{radius, radius, radius}),
	}
	// Broadphase using AABB of the sphere
	candidates := grid.QueryAABB(aabb)

	// We could filter strictly by radius here, but usually SpatialGrid just returns broadphase candidates.
	// However, the interface implies "QueryRadius".
	// Let's refine it to be broadphase candidates (AABB query) effectively,
	// because `SpatialHashGrid` doesn't store positions, only IDs.
	// To check exact radius, we'd need to look up components, which `SpatialHashGrid` doesn't have access to explicitly without ECS.
	// So returning candidates is the correct behavior for a "SpatialGrid" struct that only knows about IDs and Grid Cells.
	return candidates
}

func (grid *SpatialHashGrid) getCellIndex(pos float32) int {
	return int(math.Floor(float64(pos / grid.cellSize)))
}

// Simple hash function for 3D coordinates
func (grid *SpatialHashGrid) hashKey(x, y, z int) uint64 {
	// large primes for mixing
	const p1 = 73856093
	const p2 = 19349663
	const p3 = 83492791
	return uint64(x*p1 ^ y*p2 ^ z*p3)
}

type SpatialGridModule struct{}

func (m SpatialGridModule) Install(app *App, cmd *Commands) {
	// Default cell size 2.0 (reasonable for objects ~1-2 units size)
	cmd.AddResources(NewSpatialHashGrid(2.0))

	app.UseSystem(
		System(UpdateAABBsSystem).InStage(PreUpdate),
	).UseSystem(
		System(UpdateSpatialGridSystem).InStage(PreUpdate),
	)
}

func UpdateAABBsSystem(cmd *Commands) {
	// 1. Update AABBs for entities with Transform + Collider + AABBComponent
	//    We only update if they ALREADY have AABBComponent.
	MakeQuery3[TransformComponent, ColliderComponent, AABBComponent](cmd).Map(func(id EntityId, tr *TransformComponent, col *ColliderComponent, aabb *AABBComponent) bool {
		// Calculate world space AABB
		// Center is tr.Position
		// Extents are col.AABBHalfExtents scaled by tr.Scale

		scale := tr.Scale
		// Abs scale just in case
		scaleX := absf(scale.X())
		scaleY := absf(scale.Y())
		scaleZ := absf(scale.Z())

		halfExtents := mgl32.Vec3{
			col.AABBHalfExtents.X() * scaleX,
			col.AABBHalfExtents.Y() * scaleY,
			col.AABBHalfExtents.Z() * scaleZ,
		}

		aabb.Min = tr.Position.Sub(halfExtents)
		aabb.Max = tr.Position.Add(halfExtents)
		return true
	})

	// Note: If an entity moves but doesn't have AABBComponent yet, it won't be updated here.
	// The Scene Spawning logic ensures they get added initially.
}

func UpdateSpatialGridSystem(cmd *Commands, grid *SpatialHashGrid) {
	grid.Clear()

	MakeQuery1[AABBComponent](cmd).Map(func(id EntityId, aabb *AABBComponent) bool {
		grid.Insert(id, *aabb)
		return true
	})
}
