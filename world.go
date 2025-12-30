package gekko

import (
	"fmt"
	"math"
	"sync"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// WorldComponent defines the streaming parameters for a large-scale voxel world.
type WorldComponent struct {
	RegionRadius float32 // Radius in meters to load regions
	RegionSize   int     // Number of sectors per region side (e.g. 4 or 8)
	WorldPath    string  // Directory where sector data is stored

	// Internal state
	loadedRegions  map[[3]int]*Region
	pendingSectors map[[3]int]*volume.Sector
	mainXBM        *volume.XBrickMap
	mu             sync.Mutex
}

// Region represents a spatial block of sectors.
type Region struct {
	Coords  [3]int
	Sectors map[[3]int]*volume.Sector
}

func NewWorldComponent(path string, radius float32) *WorldComponent {
	return &WorldComponent{
		RegionRadius:   radius,
		RegionSize:     8, // Default 8x8x8 sectors = 256x256x256 voxels
		WorldPath:      path,
		loadedRegions:  make(map[[3]int]*Region),
		pendingSectors: make(map[[3]int]*volume.Sector),
		mainXBM:        volume.NewXBrickMap(),
	}
}

// GetXBrickMap returns the active xbrickmap for rendering.
func (w *WorldComponent) GetXBrickMap() *volume.XBrickMap {
	return w.mainXBM
}

// WorldStreamingSystem handles the lifecycle of voxel regions.
func WorldStreamingSystem(cmd *Commands, time *Time, state *VoxelRtState) {
	// Find the player/camera position
	var camPos mgl32.Vec3
	foundCam := false
	MakeQuery1[CameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent) bool {
		camPos = cam.Position
		foundCam = true
		return false
	})

	if !foundCam {
		return
	}

	// Update worlds
	MakeQuery1[WorldComponent](cmd).Map(func(eid EntityId, world *WorldComponent) bool {
		// Apply pending sectors from workers
		world.mu.Lock()
		if len(world.pendingSectors) > 0 {
			fmt.Printf("STREAM: Applying %d pending sectors to main XBM\n", len(world.pendingSectors))
			for k, s := range world.pendingSectors {
				world.mainXBM.Sectors[k] = s
			}
			world.mainXBM.StructureDirty = true
			world.mainXBM.AABBDirty = true
			world.pendingSectors = make(map[[3]int]*volume.Sector)
		}
		world.mu.Unlock()

		updateWorldStreaming(world, camPos, state)
		return true
	})
}

func updateWorldStreaming(world *WorldComponent, camPos mgl32.Vec3, state *VoxelRtState) {
	// Calculate current region coords (Floor division for negative support)
	regSizeVox := float64(world.RegionSize * volume.SectorSize)
	rx := int(math.Floor(float64(camPos.X()) / regSizeVox))
	ry := int(math.Floor(float64(camPos.Y()) / regSizeVox))
	rz := int(math.Floor(float64(camPos.Z()) / regSizeVox))

	// Radius in regions
	regRad := int(math.Ceil(float64(world.RegionRadius) / regSizeVox))
	if regRad < 1 {
		regRad = 1
	}

	// Identify regions that should be loaded
	shouldBeLoaded := make(map[[3]int]bool)
	for dx := -regRad; dx <= regRad; dx++ {
		for dy := -regRad; dy <= regRad; dy++ {
			for dz := -regRad; dz <= regRad; dz++ {
				coords := [3]int{rx + dx, ry + dy, rz + dz}
				shouldBeLoaded[coords] = true
			}
		}
	}

	world.mu.Lock()
	defer world.mu.Unlock()

	// Load new regions
	for coords := range shouldBeLoaded {
		if _, ok := world.loadedRegions[coords]; !ok {
			// Placeholder for a loaded region
			reg := &Region{
				Coords:  coords,
				Sectors: make(map[[3]int]*volume.Sector),
			}
			world.loadedRegions[coords] = reg

			// Background load
			go loadRegion(world, reg)
		}
	}

	// Unload distant regions
	for coords, reg := range world.loadedRegions {
		if !shouldBeLoaded[coords] {
			// Remove sectors from main XBM (Main thread)
			for sKey := range reg.Sectors {
				delete(world.mainXBM.Sectors, sKey)
			}
			world.mainXBM.StructureDirty = true
			world.mainXBM.AABBDirty = true
			delete(world.loadedRegions, coords)
		}
	}
}

func loadRegion(world *WorldComponent, reg *Region) {
	fmt.Printf("STREAM: Loading region %v\n", reg.Coords)
	// Simulate disk I/O or generation
	// In a real implementation, we would scan the WorldPath for sectors in this region.
	// For now, let's just generate some dummy terrain if no file exists.

	regSize := world.RegionSize
	for dx := 0; dx < regSize; dx++ {
		for dy := 0; dy < regSize; dy++ {
			for dz := 0; dz < regSize; dz++ {
				sx := reg.Coords[0]*regSize + dx
				sy := reg.Coords[1]*regSize + dy
				sz := reg.Coords[2]*regSize + dz

				sKey := [3]int{sx, sy, sz}

				// Try load from disk
				sector := diskLoadSector(world.WorldPath, sKey)
				if sector == nil {
					// Generate something simple (e.g. flat floor at y=0)
					if sy == 0 {
						sector = volume.NewSector(sx, sy, sz)
						// Fill bottom layers
						for bx := 0; bx < volume.SectorBricks; bx++ {
							for bz := 0; bz < volume.SectorBricks; bz++ {
								brick, _ := sector.GetOrCreateBrick(bx, 0, bz)
								for vx := 0; vx < volume.BrickSize; vx++ {
									for vz := 0; vz < volume.BrickSize; vz++ {
										brick.SetVoxel(vx, 0, vz, 1) // Layer 0
										brick.SetVoxel(vx, 1, vz, 1) // Layer 1
									}
								}
							}
						}
					}
				}

				if sector != nil {
					world.mu.Lock()
					reg.Sectors[sKey] = sector
					world.pendingSectors[sKey] = sector
					world.mu.Unlock()
				}
			}
		}
	}
}

func diskLoadSector(path string, sKey [3]int) *volume.Sector {
	// TODO: Implement binary serialization/deserialization for sectors
	return nil
}

func diskSaveSector(path string, sector *volume.Sector) error {
	// TODO: Implement binary serialization
	return nil
}
