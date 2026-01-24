package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type AABBComponent struct {
	Min mgl32.Vec3
	Max mgl32.Vec3
}

type SpatialHashGrid struct {
	cellSize float32
	// Map from cell hash to list of entities
	cells map[uint64][]EntityId
}

func NewSpatialHashGrid(cellSize float32) *SpatialHashGrid {
	return &SpatialHashGrid{
		cellSize: cellSize,
		cells:    make(map[uint64][]EntityId),
	}
}

func (grid *SpatialHashGrid) Clear() {
	// Optimization: Reuse the map if possible or just make new one
	// For now, simple make new one to ensure no stale data
	// Or loop and shrink slices? making new map is safer/easier for GC sometimes
	// but let's try to clear map for performance if we knew how to do it efficiently in Go without realloc
	// Go 1.21 has clear(map), but we might be on older version? Assuming modern Go.
	// Actually `clear(grid.cells)` is valid in Go 1.21+.
	// If not, we re-make.
	for k := range grid.cells {
		delete(grid.cells, k)
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
				grid.cells[key] = append(grid.cells[key], id)
			}
		}
	}
}

func (grid *SpatialHashGrid) QueryAABB(aabb AABBComponent) []EntityId {
	minX, maxX := grid.getCellIndex(aabb.Min.X()), grid.getCellIndex(aabb.Max.X())
	minY, maxY := grid.getCellIndex(aabb.Min.Y()), grid.getCellIndex(aabb.Max.Y())
	minZ, maxZ := grid.getCellIndex(aabb.Min.Z()), grid.getCellIndex(aabb.Max.Z())

	unique := make(map[EntityId]struct{})
	var results []EntityId

	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				key := grid.hashKey(x, y, z)
				for _, id := range grid.cells[key] {
					if _, ok := unique[id]; !ok {
						unique[id] = struct{}{}
						results = append(results, id)
					}
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
		scaleX := float32(math.Abs(float64(scale.X())))
		scaleY := float32(math.Abs(float64(scale.Y())))
		scaleZ := float32(math.Abs(float64(scale.Z())))

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
