package gekko

import (
	"math"
	"time"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// NavNode represents a cell in the navigation grid.
type NavNode struct {
	Height    float32 // Surface height in meters
	Walkable  bool
	Clearance float32 // distance to nearest obstacle
}

// NavGrid is a 2.5D navigation grid covering a Region.
type NavGrid struct {
	RegionCoords [3]int
	Size         int // Resolution: 1 cell = 1 voxel column
	Nodes        []NavNode
	DirtySectors map[[2]int]bool // Tracks 32x32 sectors that need re-baking
}

func NewNavGrid(coords [3]int, regionSize int) *NavGrid {
	cells := regionSize * volume.SectorSize
	return &NavGrid{
		RegionCoords: coords,
		Size:         cells,
		Nodes:        make([]NavNode, cells*cells),
		DirtySectors: make(map[[2]int]bool),
	}
}

func (g *NavGrid) GetNode(x, y int) *NavNode {
	if x < 0 || x >= g.Size || y < 0 || y >= g.Size {
		return nil
	}
	return &g.Nodes[y*g.Size+x]
}

// NavigationSystem manages the navigation grids and asynchronous baking.
type NavigationSystem struct {
	PendingDirtySectors map[[3]int]bool // Key: [rx, ry, rz, sx, sy] - packed or struct? Let's use [5]int{rx, ry, rz, sx, sy}
	// Actually simpler: map of RegionKey -> Set of SectorKeys
	DirtyRegions map[[3]int]map[[2]int]bool
}

func NewNavigationSystem() *NavigationSystem {
	return &NavigationSystem{
		DirtyRegions: make(map[[3]int]map[[2]int]bool),
	}
}

// MarkDirtyRegion marks a specific sector in a region as dirty.
func (ns *NavigationSystem) MarkDirtySector(rKey [3]int, sKey [2]int) {
	if ns.DirtyRegions[rKey] == nil {
		ns.DirtyRegions[rKey] = make(map[[2]int]bool)
	}
	ns.DirtyRegions[rKey][sKey] = true
}

// MarkDirtyArea calculates affected sectors from a world AABB and marks them.
func (ns *NavigationSystem) MarkDirtyArea(min, max mgl32.Vec3, vSize float32, regionSize int) {
	if vSize <= 0 {
		vSize = 0.1
	}

	sectorSizeWorld := float32(volume.SectorSize) * vSize
	regionSizeWorld := float32(regionSize) * sectorSizeWorld

	// Region bounds
	minRX := int(math.Floor(float64(min.X() / regionSizeWorld)))
	minRY := int(math.Floor(float64(min.Y() / regionSizeWorld)))
	minRZ := int(math.Floor(float64(min.Z() / regionSizeWorld)))

	maxRX := int(math.Floor(float64(max.X() / regionSizeWorld)))
	maxRY := int(math.Floor(float64(max.Y() / regionSizeWorld)))
	maxRZ := int(math.Floor(float64(max.Z() / regionSizeWorld)))

	for rz := minRZ; rz <= maxRZ; rz++ {
		for ry := minRY; ry <= maxRY; ry++ {
			for rx := minRX; rx <= maxRX; rx++ {
				rKey := [3]int{rx, ry, rz}
				// Bounds of intersection with AABB relative to Region Origin
				// localMinX = max(min.X, regOriginX) - regOriginX
				// This is getting complicated. Simpler: convert min/max to grid coords directly.

				// Global voxel coords
				minGX := int(math.Floor(float64(min.X() / vSize)))
				minGY := int(math.Floor(float64(min.Y() / vSize)))
				maxGX := int(math.Ceil(float64(max.X() / vSize)))
				maxGY := int(math.Ceil(float64(max.Y() / vSize)))

				// Clamp to region bounds in voxel coords
				regMinVX := rx * regionSize * volume.SectorSize
				regMinVY := ry * regionSize * volume.SectorSize
				regMaxVX := (rx + 1) * regionSize * volume.SectorSize
				regMaxVY := (ry + 1) * regionSize * volume.SectorSize

				startVX := math.Max(float64(minGX), float64(regMinVX))
				endVX := math.Min(float64(maxGX), float64(regMaxVX))
				startVY := math.Max(float64(minGY), float64(regMinVY))
				endVY := math.Min(float64(maxGY), float64(regMaxVY))

				if startVX >= endVX || startVY >= endVY {
					continue
				}

				// Convert to local sector coords
				minSX := (int(startVX) - regMinVX) / volume.SectorSize
				maxSX := (int(endVX) - regMinVX - 1) / volume.SectorSize // Inclusive
				minSY := (int(startVY) - regMinVY) / volume.SectorSize
				maxSY := (int(endVY) - regMinVY - 1) / volume.SectorSize

				for sy := minSY; sy <= maxSY; sy++ {
					for sx := minSX; sx <= maxSX; sx++ {
						ns.MarkDirtySector(rKey, [2]int{sx, sy})
					}
				}
			}
		}
	}
}

// Update processes the dirty queue.
func (ns *NavigationSystem) Update(cmd *Commands, vrs *VoxelRtState) {
	var world *WorldComponent
	MakeQuery1[WorldComponent](cmd).Map(func(eid EntityId, w *WorldComponent) bool {
		world = w
		return false
	})

	if world == nil || world.MainXBM == nil {
		return
	}

	xbm := world.MainXBM
	vSize := vrs.RtApp.Scene.TargetVoxelSize
	if vSize <= 0 {
		vSize = 0.1
	}

	// Process dirty queue
	start := time.Now()
	maxDuration := 2 * time.Millisecond // 2ms budget
	sectorsBaked := 0

	// We iterate over our internal dirty map
	for rKey, sectors := range ns.DirtyRegions {
		world.mu.Lock()
		reg, loaded := world.loadedRegions[rKey]

		if !loaded {
			delete(ns.DirtyRegions, rKey)
			world.mu.Unlock()
			continue
		}

		if reg.NavGrid == nil {
			reg.NavGrid = NewNavGrid(rKey, world.RegionSize)
		}

		for sKey := range sectors {
			bakeSectorNav(reg, sKey, xbm, vSize)
			delete(sectors, sKey)
			sectorsBaked++

			if time.Since(start) >= maxDuration {
				break
			}
		}

		if len(sectors) == 0 {
			delete(ns.DirtyRegions, rKey)
		}
		world.mu.Unlock()

		if time.Since(start) >= maxDuration {
			break
		}
	}
}

// Legacy system function wrapper to maintain ECS signature if needed,
// strictly speaking we should just register a method call in module.
func IncrementalNavBakeSystem(cmd *Commands, vrs *VoxelRtState, ns *NavigationSystem, prof *Profiler) {
	if prof != nil {
		start := time.Now()
		defer func() { prof.NavBakeTime += time.Since(start) }()
	}
	if ns != nil {
		ns.Update(cmd, vrs)
	}
}

func bakeSectorNav(reg *Region, sKey [2]int, xbm *volume.XBrickMap, vSize float32) {
	grid := reg.NavGrid
	regSize := grid.Size // typically 256 for RegSize=8

	// Sector bounds in local grid coords (2D) relative to region start
	minGX := sKey[0] * volume.SectorSize
	minGY := sKey[1] * volume.SectorSize
	maxGX := minGX + volume.SectorSize
	maxGY := minGY + volume.SectorSize

	// Region origin in sector coords
	// Actually in typical setup RegionSize=8 sectors.
	// NavGrid covers one Region.
	// We need GLOBAL sector coordinates to find the XBM sector.
	// Region Origin Sector Coords:
	// Global Region Size = 8 sectors.
	// Region [rx, ry, rz] starts at sector [rx*8, ry*8, rz*8].

	// CAUTION: reg.RegionSize isn't stored in Region, passed via World usually.
	// But NavGrid knows grid size.
	// Let's assume standard layout:
	// Global Sector X = (reg.Coords[0] * (grid.Size / 32)) + sKey[0]
	// Global Sector Y = (reg.Coords[1] * (grid.Size / 32)) + sKey[1]

	sectorsPerRegion := grid.Size / volume.SectorSize // 256 / 32 = 8
	globalSX := reg.Coords[0]*sectorsPerRegion + sKey[0]
	globalSY := reg.Coords[1]*sectorsPerRegion + sKey[1]

	// We iterate Z sectors in the region column.
	minGlobalSZ := reg.Coords[2] * sectorsPerRegion

	// Pre-fetch Z-column sectors involved
	// We scan from Top to Bottom globally within the region.
	// Optimization: Fetch the Sector pointer for each vertical slice ONCE.

	// Actually simple optimization:
	// Iterate 2D (gx, gy).
	// For each (gx, gy), we need to check voxels in Z down.
	// Instead of calling GetVoxel (which resolves sector every time),
	// We can manually traverse sectors.

	// But NavGrid 'Nodes' are 2D.
	// So for loop is gy, gx.

	// Let's grab the vertical list of sectors for this (gx,gy) column sector.
	// sKey is local sector X,Y.
	// The vertical column of sectors corresponds to keys: [globalSX, globalSY, sz] for sz in minGlobalSZ..maxGlobalSZ.

	verticalSectors := make([]*volume.Sector, sectorsPerRegion)
	for i := 0; i < sectorsPerRegion; i++ {
		secKey := [3]int{globalSX, globalSY, minGlobalSZ + i}
		if s, ok := xbm.Sectors[secKey]; ok {
			verticalSectors[i] = s
		}
	}

	getVoxelVertical := func(gz, lvx, lvy int) bool {
		relZ := gz - minGlobalSZ*volume.SectorSize
		if relZ < 0 || relZ >= sectorsPerRegion*volume.SectorSize {
			return false
		}
		sIdx := relZ / volume.SectorSize
		sect := verticalSectors[sIdx]
		if sect == nil {
			return false
		}
		lsz := relZ % volume.SectorSize
		bx, by, bz := lvx/volume.BrickSize, lvy/volume.BrickSize, lsz/volume.BrickSize
		brick := sect.GetBrick(bx, by, bz)
		if brick == nil {
			return false
		}
		lx, ly, lz := lvx%volume.BrickSize, lvy%volume.BrickSize, lsz%volume.BrickSize
		return brick.Payload[lx][ly][lz] != 0
	}

	for gy := minGY; gy < maxGY; gy++ {
		for gx := minGX; gx < maxGX; gx++ {
			node := &grid.Nodes[gy*regSize+gx]

			// Local voxel index within the 2D sector
			vx := gx % volume.SectorSize
			vy := gy % volume.SectorSize

			// Local brick coordinates (2D)
			lbx, lby := vx/volume.BrickSize, vy/volume.BrickSize
			// Local microcell coordinates (2D) within brick
			lmx, lmy := (vx%volume.BrickSize)/volume.MicroSize, (vy%volume.BrickSize)/volume.MicroSize

			found := false

			// Scan Z downwards through sectors
			for i := sectorsPerRegion - 1; i >= 0; i-- {
				sect := verticalSectors[i]
				if sect == nil || sect.BrickMask64 == 0 {
					continue
				}

				// Scan bricks in this sector column
				for bz := 3; bz >= 0; bz-- {
					flatBIdx := lbx + lby*4 + bz*16
					if (sect.BrickMask64 & (1 << flatBIdx)) == 0 {
						continue
					}

					brick := sect.GetBrick(lbx, lby, bz)
					if brick == nil || brick.IsEmpty() {
						continue
					}

					// Scan microcells in this brick column
					for mz := 3; mz >= 0; mz-- {
						microIdx := lmx + lmy*4 + mz*16
						if (brick.OccupancyMask64 & (1 << microIdx)) == 0 {
							continue
						}

						// Scan voxels in this microcell
						secOriginVZ := (minGlobalSZ + i) * volume.SectorSize
						baseVZ := bz*volume.BrickSize + mz*volume.MicroSize

						for lvz := volume.MicroSize - 1; lvz >= 0; lvz-- {
							vz := baseVZ + lvz
							lx, ly, lz := vx%volume.BrickSize, vy%volume.BrickSize, vz%volume.BrickSize

							if brick.Payload[lx][ly][lz] != 0 {
								// Found surface
								globalZ := secOriginVZ + vz
								node.Height = float32(globalZ+1) * vSize
								node.Walkable = true

								// Headroom check: Z+1 and Z+2
								if getVoxelVertical(globalZ+1, vx, vy) || getVoxelVertical(globalZ+2, vx, vy) {
									node.Walkable = false
								}

								found = true
								goto NodeDone
							}
						}
					}
				}
			}
		NodeDone:
			if !found {
				node.Walkable = false
				node.Height = -1000
			}
		}
	}

	// Dilation commented out
	// dilateSector(grid, minGX, minGY, maxGX, maxGY, vSize)
}

func dilateSector(grid *NavGrid, minX, minY, maxX, maxY int, vSize float32) {
	regSize := grid.Size

	// Create a local copy of walkability for dilation to avoid using already-dilated neighbors
	// We expand by 1 to handle edges correctly
	// For correctness, dilation should ideally be a post-process after ALL modified sectors are baked.
	// But per-sector dilation is a good approximation for performance.

	for gy := minY; gy < maxY; gy++ {
		for gx := minX; gx < maxX; gx++ {
			node := &grid.Nodes[gy*regSize+gx]
			if !node.Walkable {
				continue
			}

			// Cliff/Wall check
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					nx, ny := gx+dx, gy+dy
					if nx < 0 || nx >= regSize || ny < 0 || ny >= regSize {
						continue
					}
					neighbor := &grid.Nodes[ny*regSize+nx]
					if !neighbor.Walkable || math.Abs(float64(neighbor.Height-node.Height)) > float64(vSize*3) {
						// Note: This modifies the node immediately.
						// To be perfectly accurate, we'd use a temp buffer.
						node.Walkable = false
						goto nextNode
					}
				}
			}
		nextNode:
		}
	}
}

// NavGridToWorld converts grid coords to world space.
func (g *NavGrid) NavGridToWorld(gx, gy int, vSize float32) mgl32.Vec3 {
	node := g.GetNode(gx, gy)
	wx := float32(g.RegionCoords[0]*g.Size+gx) * vSize
	wy := float32(g.RegionCoords[1]*g.Size+gy) * vSize
	wz := float32(0.0)
	if node != nil {
		wz = node.Height
	}
	return mgl32.Vec3{wx, wy, wz}
}

// WorldToNavGrid converts world pos to grid coords.
func (g *NavGrid) WorldToNavGrid(pos mgl32.Vec3, vSize float32) (int, int) {
	minVX := g.RegionCoords[0] * g.Size
	minVY := g.RegionCoords[1] * g.Size
	gx := int(math.Floor(float64(pos.X()/vSize))) - minVX
	gy := int(math.Floor(float64(pos.Y()/vSize))) - minVY
	return gx, gy
}
