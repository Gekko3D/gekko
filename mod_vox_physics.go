package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type VoxPhysicsModule struct{}

func (m VoxPhysicsModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&VoxelGridCache{
		Snapshots:   make(map[EntityId]*voxelGridSnapshot),
		AssetGrids:  make(map[AssetId]*voxelGridAssetCache),
		BuildStamps: make(map[EntityId]voxelPhysicsBuildStamp),
	})
	app.UseSystem(
		System(VoxPhysicsPreCalcSystem).
			InStage(Update).
			RunAlways(),
	)
}

type voxelGridSnapshot struct {
	xbm       *volume.XBrickMap
	vSize     float32
	cachedMin mgl32.Vec3
	cachedMax mgl32.Vec3
}

type voxelGridAssetCache struct {
	xbm       *volume.XBrickMap
	cachedMin mgl32.Vec3
	cachedMax mgl32.Vec3
}

func (v *voxelGridAssetCache) Snapshot(vSize float32) *voxelGridSnapshot {
	if v == nil {
		return nil
	}

	return &voxelGridSnapshot{
		xbm:       v.xbm,
		vSize:     vSize,
		cachedMin: v.cachedMin,
		cachedMax: v.cachedMax,
	}
}

func (v *voxelGridSnapshot) GetVoxel(gx, gy, gz int) (bool, uint8) {
	return v.xbm.GetVoxel(gx, gy, gz)
}

func (v *voxelGridSnapshot) GetAABBMin() mgl32.Vec3 {
	return v.cachedMin
}

func (v *voxelGridSnapshot) GetAABBMax() mgl32.Vec3 {
	return v.cachedMax
}

func (v *voxelGridSnapshot) VoxelSize() float32 {
	return v.vSize
}

type VoxelGridCache struct {
	Snapshots   map[EntityId]*voxelGridSnapshot
	AssetGrids  map[AssetId]*voxelGridAssetCache
	BuildStamps map[EntityId]voxelPhysicsBuildStamp
}

type voxelPhysicsSourceKind uint8

const (
	voxelPhysicsSourceAsset voxelPhysicsSourceKind = iota
	voxelPhysicsSourceRuntime
	voxelPhysicsSourceCustom
)

type voxelPhysicsBuildStamp struct {
	Source voxelPhysicsSourceKind
	Asset  AssetId
	Scale  mgl32.Vec3
	MapPtr *volume.XBrickMap
}

func currentVoxelPhysicsBuildStamp(vmc *VoxelModelComponent, tr *TransformComponent, runtimeMap *volume.XBrickMap) voxelPhysicsBuildStamp {
	stamp := voxelPhysicsBuildStamp{
		Scale: tr.Scale,
	}

	switch {
	case runtimeMap != nil:
		stamp.Source = voxelPhysicsSourceRuntime
		stamp.MapPtr = runtimeMap
	case vmc.CustomMap != nil:
		stamp.Source = voxelPhysicsSourceCustom
		stamp.MapPtr = vmc.CustomMap
	default:
		stamp.Source = voxelPhysicsSourceAsset
		stamp.Asset = vmc.VoxelModel
	}

	return stamp
}

// Deprecated: DecomposeVoxModel uses greedy meshing to produce multiple boxes.
// Use AABB-based approach and VoxelGrid collision instead.
func DecomposeVoxModel(model VoxModel, vSize float32) []CollisionBox {
	if len(model.Voxels) == 0 {
		return nil
	}

	voxels := make(map[[3]uint32]bool)
	for _, v := range model.Voxels {
		voxels[[3]uint32{v.X, v.Y, v.Z}] = true
	}

	var boxes []CollisionBox

	for z := uint32(0); z < model.SizeZ; z++ {
		for y := uint32(0); y < model.SizeY; y++ {
			for x := uint32(0); x < model.SizeX; x++ {
				pos := [3]uint32{x, y, z}
				if !voxels[pos] {
					continue
				}

				// Find maximal box starting here
				width, height, depth := uint32(1), uint32(1), uint32(1)

				// Grow X
				for tx := x + 1; tx < model.SizeX && voxels[[3]uint32{tx, y, z}]; tx++ {
					width++
				}
				// Grow Y
				for ty := y + 1; ty < model.SizeY; ty++ {
					canGrow := true
					for tx := x; tx < x+width; tx++ {
						if !voxels[[3]uint32{tx, ty, z}] {
							canGrow = false
							break
						}
					}
					if !canGrow {
						break
					}
					height++
				}
				// Grow Z
				for tz := z + 1; tz < model.SizeZ; tz++ {
					canGrow := true
					for ty := y; ty < y+height; ty++ {
						for tx := x; tx < x+width; tx++ {
							if !voxels[[3]uint32{tx, ty, tz}] {
								canGrow = false
								break
							}
						}
						if !canGrow {
							break
						}
					}
					if !canGrow {
						break
					}
					depth++
				}

				// Mark used
				for tz := z; tz < z+depth; tz++ {
					for ty := y; ty < y+height; ty++ {
						for tx := x; tx < x+width; tx++ {
							delete(voxels, [3]uint32{tx, ty, tz})
						}
					}
				}

				// Local origin of model is at 0,0,0
				// But we want LocalOffset relative to model's center for Physics
				// Wait, the engine used to use AABBMin/AABBMax.
				// PhysicsModel.CenterOffset helped sync.
				// With multiple boxes, each box.LocalOffset is relative to the Entity Position.
				// In Gekko, the Entity Position corresponds to the model's (0,0,0) [min point of the model's bounds].

				boxes = append(boxes, CollisionBox{
					HalfExtents: mgl32.Vec3{float32(width), float32(height), float32(depth)}.Mul(0.5 * vSize),
					LocalOffset: mgl32.Vec3{float32(x) + float32(width)*0.5, float32(y) + float32(height)*0.5, float32(z) + float32(depth)*0.5}.Mul(vSize),
				})
			}
		}
	}

	return boxes
}

// Deprecated: DecomposeXBrickMap uses greedy meshing to produce multiple boxes.
// Use AABB-based approach and VoxelGrid collision instead.
func DecomposeXBrickMap(xbm *volume.XBrickMap, vSize float32) []CollisionBox {
	if xbm == nil {
		return nil
	}

	// Flatten all voxels into a single map for global greedy meshing.
	// This produces the fewest possible collision boxes, which is critical
	// because the narrow-phase collision detection is O(boxesA × boxesB).
	voxels := make(map[[3]int]bool)
	for sKey, sector := range xbm.Sectors {
		if sector.IsEmpty() {
			continue
		}
		ox, oy, oz := sKey[0]*volume.SectorSize, sKey[1]*volume.SectorSize, sKey[2]*volume.SectorSize

		for i := 0; i < 64; i++ {
			if (sector.BrickMask64 & (1 << i)) != 0 {
				bx, by, bz := i%4, (i/4)%4, i/16
				brick := sector.GetBrick(bx, by, bz)
				if brick == nil || brick.IsEmpty() {
					continue
				}

				boxox := ox + bx*volume.BrickSize
				boxoy := oy + by*volume.BrickSize
				boxoz := oz + bz*volume.BrickSize

				if brick.Flags&volume.BrickFlagSolid != 0 {
					// Add all voxels in this brick
					for vz := 0; vz < volume.BrickSize; vz++ {
						for vy := 0; vy < volume.BrickSize; vy++ {
							for vx := 0; vx < volume.BrickSize; vx++ {
								voxels[[3]int{boxox + vx, boxoy + vy, boxoz + vz}] = true
							}
						}
					}
				} else {
					for vz := 0; vz < volume.BrickSize; vz++ {
						for vy := 0; vy < volume.BrickSize; vy++ {
							for vx := 0; vx < volume.BrickSize; vx++ {
								if brick.Payload[vx][vy][vz] != 0 {
									voxels[[3]int{boxox + vx, boxoy + vy, boxoz + vz}] = true
								}
							}
						}
					}
				}
			}
		}
	}

	if len(voxels) == 0 {
		return nil
	}

	// Find bounds of occupied voxels
	var minX, minY, minZ, maxX, maxY, maxZ int
	first := true
	for p := range voxels {
		if first {
			minX, maxX = p[0], p[0]+1
			minY, maxY = p[1], p[1]+1
			minZ, maxZ = p[2], p[2]+1
			first = false
		} else {
			if p[0] < minX {
				minX = p[0]
			}
			if p[0]+1 > maxX {
				maxX = p[0] + 1
			}
			if p[1] < minY {
				minY = p[1]
			}
			if p[1]+1 > maxY {
				maxY = p[1] + 1
			}
			if p[2] < minZ {
				minZ = p[2]
			}
			if p[2]+1 > maxZ {
				maxZ = p[2] + 1
			}
		}
	}

	var boxes []CollisionBox

	for z := minZ; z < maxZ; z++ {
		for y := minY; y < maxY; y++ {
			for x := minX; x < maxX; x++ {
				pos := [3]int{x, y, z}
				if !voxels[pos] {
					continue
				}

				width, height, depth := 1, 1, 1

				// Grow X
				for tx := x + 1; tx < maxX && voxels[[3]int{tx, y, z}]; tx++ {
					width++
				}
				// Grow Y
				for ty := y + 1; ty < maxY; ty++ {
					canGrow := true
					for tx := x; tx < x+width; tx++ {
						if !voxels[[3]int{tx, ty, z}] {
							canGrow = false
							break
						}
					}
					if !canGrow {
						break
					}
					height++
				}
				// Grow Z
				for tz := z + 1; tz < maxZ; tz++ {
					canGrow := true
					for ty := y; ty < y+height; ty++ {
						for tx := x; tx < x+width; tx++ {
							if !voxels[[3]int{tx, ty, tz}] {
								canGrow = false
								break
							}
						}
						if !canGrow {
							break
						}
					}
					if !canGrow {
						break
					}
					depth++
				}

				// Mark used
				for tz := z; tz < z+depth; tz++ {
					for ty := y; ty < y+height; ty++ {
						for tx := x; tx < x+width; tx++ {
							delete(voxels, [3]int{tx, ty, tz})
						}
					}
				}

				boxes = append(boxes, CollisionBox{
					HalfExtents: mgl32.Vec3{float32(width), float32(height), float32(depth)}.Mul(0.5 * vSize),
					LocalOffset: mgl32.Vec3{float32(x) + float32(width)*0.5, float32(y) + float32(height)*0.5, float32(z) + float32(depth)*0.5}.Mul(vSize),
				})
			}
		}
	}

	return boxes
}

// Deprecated: applyScaleToBoxes is no longer needed with single-AABB approach.
func applyScaleToBoxes(boxes []CollisionBox, scale float32) {
	if scale == 1.0 {
		return
	}
	for i := range boxes {
		boxes[i].HalfExtents = boxes[i].HalfExtents.Mul(scale)
		boxes[i].LocalOffset = boxes[i].LocalOffset.Mul(scale)
	}
}

func VoxPhysicsPreCalcSystem(cmd *Commands, server *AssetServer, rtState *VoxelRtState, cache *VoxelGridCache) {
	// Cleanup snapshots for destroyed entities to prevent memory leaks
	for eid := range cache.Snapshots {
		if comps := cmd.GetAllComponents(eid); len(comps) == 0 {
			delete(cache.Snapshots, eid)
			delete(cache.BuildStamps, eid)
		}
	}

	MakeQuery4[VoxelModelComponent, RigidBodyComponent, TransformComponent, PhysicsModel](cmd).Map(func(eid EntityId, vmc *VoxelModelComponent, rb *RigidBodyComponent, tr *TransformComponent, pm *PhysicsModel) bool {
		// 1. Try to get runtime XBrickMap
		var xbm *volume.XBrickMap
		if rtState != nil {
			if obj := rtState.GetVoxelObject(eid); obj != nil {
				xbm = obj.XBrickMap
			}
		}

		// 2. Determine if we need to (re)build
		found := pm != nil
		stamp := currentVoxelPhysicsBuildStamp(vmc, tr, xbm)
		previousStamp, hasStamp := cache.BuildStamps[eid]
		needsBuild := !found || !hasStamp || previousStamp != stamp

		// Structural dirty check
		if xbm != nil && (xbm.StructureDirty || len(xbm.DirtyBricks) > 0 || len(xbm.DirtySectors) > 0) {
			needsBuild = true
		} else if vmc.CustomMap != nil && (vmc.CustomMap.StructureDirty || len(vmc.CustomMap.DirtyBricks) > 0 || len(vmc.CustomMap.DirtySectors) > 0) {
			needsBuild = true
		}
		if found && pm.Grid == nil {
			switch {
			case xbm != nil, vmc.CustomMap != nil:
				needsBuild = true
			default:
				server.mu.RLock()
				_, ok := server.voxModels[vmc.VoxelModel]
				server.mu.RUnlock()
				if ok {
					needsBuild = true
				}
			}
		}

		if !needsBuild {
			return true
		}

		var box *CollisionBox
		var center mgl32.Vec3
		var grid *voxelGridSnapshot

		if xbm != nil {
			vMin, vMax := xbm.ComputeAABB()
			xbm.ClearDirty()

			if vMin != vMax {
				vSize := VoxelSize * tr.Scale.X()
				minW := vMin.Mul(vSize)
				maxW := vMax.Mul(vSize)
				center = minW.Add(maxW).Mul(0.5)
				half := maxW.Sub(minW).Mul(0.5)

				box = &CollisionBox{
					HalfExtents: half,
					LocalOffset: mgl32.Vec3{0, 0, 0}, // Relative to center
				}
			}

			grid = &voxelGridSnapshot{
				xbm:       xbm.Copy(),
				vSize:     VoxelSize * tr.Scale.X(),
				cachedMin: xbm.GetAABBMin(),
				cachedMax: xbm.GetAABBMax(),
			}
		} else if vmc.CustomMap != nil {
			vMin, vMax := vmc.CustomMap.ComputeAABB()
			vmc.CustomMap.ClearDirty()

			if vMin != vMax {
				vSize := VoxelSize * tr.Scale.X()
				minW := vMin.Mul(vSize)
				maxW := vMax.Mul(vSize)
				center = minW.Add(maxW).Mul(0.5)
				half := maxW.Sub(minW).Mul(0.5)

				box = &CollisionBox{
					HalfExtents: half,
					LocalOffset: mgl32.Vec3{0, 0, 0},
				}
			}

			grid = &voxelGridSnapshot{
				xbm:       vmc.CustomMap.Copy(),
				vSize:     VoxelSize * tr.Scale.X(),
				cachedMin: vmc.CustomMap.GetAABBMin(),
				cachedMax: vmc.CustomMap.GetAABBMax(),
			}
		} else {
			// Asset path
			server.mu.RLock()
			asset, ok := server.voxModels[vmc.VoxelModel]
			server.mu.RUnlock()

			if ok {
				vSize := VoxelSize * tr.Scale.X()
				minW := mgl32.Vec3{0, 0, 0}
				maxW := mgl32.Vec3{float32(asset.VoxModel.SizeX), float32(asset.VoxModel.SizeY), float32(asset.VoxModel.SizeZ)}.Mul(vSize)
				center = minW.Add(maxW).Mul(0.5)
				half := maxW.Sub(minW).Mul(0.5)

				box = &CollisionBox{
					HalfExtents: half,
					LocalOffset: mgl32.Vec3{0, 0, 0},
				}

				// Cache grid for asset to avoid expensive XBrickMap conversion
				assetGrid := cache.AssetGrids[vmc.VoxelModel]
				if assetGrid == nil {
					// Build XBrickMap from asset voxels
					xbm := volume.NewXBrickMap()
					for _, v := range asset.VoxModel.Voxels {
						xbm.SetVoxel(int(v.X), int(v.Y), int(v.Z), v.ColorIndex)
					}
					gMin, gMax := xbm.ComputeAABB()
					assetGrid = &voxelGridAssetCache{
						xbm:       xbm,
						cachedMin: gMin,
						cachedMax: gMax,
					}
					cache.AssetGrids[vmc.VoxelModel] = assetGrid
				}
				grid = assetGrid.Snapshot(vSize)
			}
		}

		newPM := PhysicsModel{
			CenterOffset: center,
		}
		if box != nil {
			newPM.Boxes = []CollisionBox{*box}
		}
		if grid != nil {
			newPM.Grid = grid
			cache.Snapshots[eid] = grid
		} else {
			delete(cache.Snapshots, eid)
		}
		cache.BuildStamps[eid] = stamp

		// Only update if something actually changed to avoid component update noise
		if !found || pm.CenterOffset != newPM.CenterOffset || len(pm.Boxes) != len(newPM.Boxes) || pm.Grid != newPM.Grid {
			cmd.AddComponents(eid, newPM)
		}

		return true
	}, PhysicsModel{})
}
