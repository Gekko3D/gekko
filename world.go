package gekko

import (
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
	MainXBM        *volume.XBrickMap
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
		MainXBM:        volume.NewXBrickMap(),
	}
}

// GetXBrickMap returns the active xbrickmap for rendering.
func (w *WorldComponent) GetXBrickMap() *volume.XBrickMap {
	return w.MainXBM
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
		// Apply pending sectors from workers with throttling
		world.mu.Lock()
		if len(world.pendingSectors) > 0 {
			count := 0
			// Batch size: process up to 64 sectors per frame to avoid CPU spikes
			for k, s := range world.pendingSectors {
				world.MainXBM.Sectors[k] = s
				delete(world.pendingSectors, k)
				count++
				if count >= 64 {
					break
				}
			}
			if count > 0 {
				world.MainXBM.StructureDirty = true
				world.MainXBM.AABBDirty = true
			}
		}
		world.mu.Unlock()

		updateWorldStreaming(world, camPos, state)
		return true
	})
}

func updateWorldStreaming(world *WorldComponent, camPos mgl32.Vec3, state *VoxelRtState) {
	vSize := state.RtApp.Scene.TargetVoxelSize
	if vSize <= 0 {
		vSize = 0.1
	}

	// Calculate current region coords (Floor division for negative support)
	// Region size in world units = (sectors per region) * (voxels per sector) * (meters per voxel)
	regSizeUnits := float64(world.RegionSize*volume.SectorSize) * float64(vSize)
	rx := int(math.Floor(float64(camPos.X()) / regSizeUnits))
	ry := int(math.Floor(float64(camPos.Y()) / regSizeUnits))
	rz := int(math.Floor(float64(camPos.Z()) / regSizeUnits))

	// Radius in regions
	regRad := int(math.Ceil(float64(world.RegionRadius) / regSizeUnits))
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
				delete(world.MainXBM.Sectors, sKey)
			}
			world.MainXBM.StructureDirty = true
			world.MainXBM.AABBDirty = true
			delete(world.loadedRegions, coords)
		}
	}
}

func loadRegion(world *WorldComponent, reg *Region) {
	// fmt.Printf("STREAM: Loading region %v\n", reg.Coords)
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
