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
	xbm        *volume.XBrickMap
	vSize      float32
	voxelScale mgl32.Vec3
	cachedMin  mgl32.Vec3
	cachedMax  mgl32.Vec3
}

type voxelGridAssetCache struct {
	xbm       *volume.XBrickMap
	cachedMin mgl32.Vec3
	cachedMax mgl32.Vec3
}

func (v *voxelGridAssetCache) Snapshot(voxelScale mgl32.Vec3) *voxelGridSnapshot {
	if v == nil {
		return nil
	}

	return &voxelGridSnapshot{
		xbm:        v.xbm,
		vSize:      voxelScale.X(),
		voxelScale: voxelScale,
		cachedMin:  v.cachedMin,
		cachedMax:  v.cachedMax,
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
	if v.voxelScale != (mgl32.Vec3{}) {
		return v.voxelScale.X()
	}
	return v.vSize
}

func (v *voxelGridSnapshot) VoxelScale() mgl32.Vec3 {
	if v.voxelScale != (mgl32.Vec3{}) {
		return v.voxelScale
	}
	return mgl32.Vec3{v.vSize, v.vSize, v.vSize}
}

func (v *voxelGridSnapshot) ForEachPrimitiveInRange(minX, minY, minZ, maxX, maxY, maxZ int, fn func(localCenter, halfExtents mgl32.Vec3) bool) bool {
	if v == nil || v.xbm == nil {
		return true
	}

	voxelScale := v.VoxelScale()
	for sKey, sector := range v.xbm.Sectors {
		sectorMinX := sKey[0] * volume.SectorSize
		sectorMinY := sKey[1] * volume.SectorSize
		sectorMinZ := sKey[2] * volume.SectorSize
		sectorMaxX := sectorMinX + volume.SectorSize
		sectorMaxY := sectorMinY + volume.SectorSize
		sectorMaxZ := sectorMinZ + volume.SectorSize
		if sectorMaxX <= minX || sectorMinX >= maxX ||
			sectorMaxY <= minY || sectorMinY >= maxY ||
			sectorMaxZ <= minZ || sectorMinZ >= maxZ {
			continue
		}

		for brickIdx := 0; brickIdx < 64; brickIdx++ {
			if (sector.BrickMask64 & (1 << brickIdx)) == 0 {
				continue
			}

			bx, by, bz := brickIdx%4, (brickIdx/4)%4, brickIdx/16
			brickMinX := sectorMinX + bx*volume.BrickSize
			brickMinY := sectorMinY + by*volume.BrickSize
			brickMinZ := sectorMinZ + bz*volume.BrickSize
			brickMaxX := brickMinX + volume.BrickSize
			brickMaxY := brickMinY + volume.BrickSize
			brickMaxZ := brickMinZ + volume.BrickSize

			rangeMinX := max(minX, brickMinX)
			rangeMinY := max(minY, brickMinY)
			rangeMinZ := max(minZ, brickMinZ)
			rangeMaxX := min(maxX, brickMaxX)
			rangeMaxY := min(maxY, brickMaxY)
			rangeMaxZ := min(maxZ, brickMaxZ)
			if rangeMinX >= rangeMaxX || rangeMinY >= rangeMaxY || rangeMinZ >= rangeMaxZ {
				continue
			}

			brick := sector.GetBrick(bx, by, bz)
			if brick == nil {
				continue
			}

			if brick.Flags&volume.BrickFlagSolid != 0 {
				// Emit the whole solid brick, not only the clipped overlap slice.
				// Using the clipped range creates artificial side faces that can
				// produce incorrect contact normals for resting/sliding bodies.
				if !emitVoxelPrimitiveRange(brickMinX, brickMinY, brickMinZ, brickMaxX, brickMaxY, brickMaxZ, voxelScale, fn) {
					return true
				}
				continue
			}

			for gz := rangeMinZ; gz < rangeMaxZ; gz++ {
				localZ := gz - brickMinZ
				for gy := rangeMinY; gy < rangeMaxY; gy++ {
					localY := gy - brickMinY
					for gx := rangeMinX; gx < rangeMaxX; gx++ {
						localX := gx - brickMinX
						if brick.Payload[localX][localY][localZ] == 0 {
							continue
						}
						if !emitVoxelPrimitiveRange(gx, gy, gz, gx+1, gy+1, gz+1, voxelScale, fn) {
							return true
						}
					}
				}
			}
		}
	}

	return true
}

type VoxelGridCache struct {
	Snapshots   map[EntityId]*voxelGridSnapshot
	AssetGrids  map[AssetId]*voxelGridAssetCache
	BuildStamps map[EntityId]voxelPhysicsBuildStamp
}

type voxelPhysicsSourceKind uint8

const (
	voxelPhysicsSourceGeometry voxelPhysicsSourceKind = iota
	voxelPhysicsSourceRuntime
)

type voxelPhysicsBuildStamp struct {
	Source voxelPhysicsSourceKind
	Asset  AssetId
	Scale  mgl32.Vec3
	MapPtr *volume.XBrickMap
}

func currentVoxelPhysicsBuildStamp(tr *TransformComponent, geometry AssetId, runtimeMap *volume.XBrickMap) voxelPhysicsBuildStamp {
	stamp := voxelPhysicsBuildStamp{
		Scale: tr.Scale,
	}
	if runtimeMap != nil {
		stamp.Source = voxelPhysicsSourceRuntime
		stamp.MapPtr = runtimeMap
		return stamp
	}
	stamp.Source = voxelPhysicsSourceGeometry
	stamp.Asset = geometry
	return stamp
}

func scaledVoxelScale(vmc *VoxelModelComponent, tr *TransformComponent) mgl32.Vec3 {
	return EffectiveVoxelScale(vmc, tr)
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
		var runtimeMap *volume.XBrickMap
		if rtState != nil {
			if obj := rtState.GetVoxelObject(eid); obj != nil {
				runtimeMap = obj.XBrickMap
			}
		}
		geometryID, geometryAsset, hasGeometry := ResolveVoxelGeometry(server, vmc)
		var xbm *volume.XBrickMap
		if runtimeMap != nil {
			xbm = runtimeMap
		} else if hasGeometry && geometryAsset != nil {
			xbm = geometryAsset.XBrickMap
		}

		// 2. Determine if we need to (re)build
		found := pm != nil
		stamp := currentVoxelPhysicsBuildStamp(tr, geometryID, runtimeMap)
		previousStamp, hasStamp := cache.BuildStamps[eid]
		needsBuild := !found || !hasStamp || previousStamp != stamp

		// Structural dirty check
		if xbm != nil && (xbm.StructureDirty || len(xbm.DirtyBricks) > 0 || len(xbm.DirtySectors) > 0) {
			needsBuild = true
		}
		if found && pm.Grid == nil {
			if xbm != nil {
				needsBuild = true
			}
		}

		if !needsBuild {
			return true
		}

		var box *CollisionBox
		var center mgl32.Vec3
		var grid *voxelGridSnapshot
		voxelScale := scaledVoxelScale(vmc, tr)

		if runtimeMap != nil && xbm != nil {
			vMin, vMax := xbm.ComputeAABB()
			xbm.ClearDirty()

			if vMin != vMax {
				minW := vec3MulComponents(vMin, voxelScale)
				maxW := vec3MulComponents(vMax, voxelScale)
				center = minW.Add(maxW).Mul(0.5)
				half := maxW.Sub(minW).Mul(0.5)

				box = &CollisionBox{
					HalfExtents: half,
					LocalOffset: mgl32.Vec3{0, 0, 0}, // Relative to center
				}
			}

			grid = &voxelGridSnapshot{
				xbm:        xbm.Copy(),
				vSize:      voxelScale.X(),
				voxelScale: voxelScale,
				cachedMin:  xbm.GetAABBMin(),
				cachedMax:  xbm.GetAABBMax(),
			}
		} else if hasGeometry && geometryAsset != nil {
			minW := vec3MulComponents(geometryAsset.LocalMin, voxelScale)
			maxW := vec3MulComponents(geometryAsset.LocalMax, voxelScale)
			center = minW.Add(maxW).Mul(0.5)
			half := maxW.Sub(minW).Mul(0.5)

			box = &CollisionBox{
				HalfExtents: half,
				LocalOffset: mgl32.Vec3{0, 0, 0},
			}

			assetGrid := cache.AssetGrids[geometryID]
			if assetGrid == nil || (geometryAsset.XBrickMap != nil && (geometryAsset.XBrickMap.StructureDirty || len(geometryAsset.XBrickMap.DirtyBricks) > 0 || len(geometryAsset.XBrickMap.DirtySectors) > 0)) {
				gMin, gMax := geometryAsset.LocalMin, geometryAsset.LocalMax
				if geometryAsset.XBrickMap != nil {
					geometryAsset.XBrickMap.ClearDirty()
				}
				assetGrid = &voxelGridAssetCache{
					xbm:       geometryAsset.XBrickMap,
					cachedMin: gMin,
					cachedMax: gMax,
				}
				cache.AssetGrids[geometryID] = assetGrid
			}
			grid = assetGrid.Snapshot(voxelScale)
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
