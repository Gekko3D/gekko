package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

type VoxPhysicsModule struct{}

func (m VoxPhysicsModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(VoxPhysicsPreCalcSystem).
			InStage(Update).
			RunAlways(),
	)
}

func DecomposeVoxModel(model VoxModel, vSize float32) []CollisionBox {
	if len(model.Voxels) == 0 {
		return nil
	}

	occupied := make(map[[3]uint32]bool)
	for _, v := range model.Voxels {
		occupied[[3]uint32{v.X, v.Y, v.Z}] = true
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

func DecomposeXBrickMap(xbm *volume.XBrickMap, vSize float32) []CollisionBox {
	if xbm == nil {
		return nil
	}

	// voxels map to track what's left to process
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

func VoxPhysicsPreCalcSystem(cmd *Commands, server *AssetServer, rtState *VoxelRtState) {
	MakeQuery3[VoxelModelComponent, RigidBodyComponent, TransformComponent](cmd).Map(func(eid EntityId, vmc *VoxelModelComponent, rb *RigidBodyComponent, tr *TransformComponent) bool {
		// 1. Try to get runtime XBrickMap
		var xbm *volume.XBrickMap
		if rtState != nil {
			if obj := rtState.GetVoxelObject(eid); obj != nil {
				xbm = obj.XBrickMap
			}
		}

		// 2. Check if PhysicsModel already exists
		found := false
		allComps := cmd.GetAllComponents(eid)
		for _, c := range allComps {
			if _, ok := c.(PhysicsModel); ok {
				found = true
				break
			}
		}

		// 3. Determine if we need to (re)build
		needsBuild := !found
		if xbm != nil && xbm.StructureDirty {
			needsBuild = true
		}

		if !needsBuild {
			return true
		}

		var boxes []CollisionBox
		initialized := false

		if xbm != nil {
			// Prefer runtime edited map
			vSize := VoxelSize
			boxes = DecomposeXBrickMap(xbm, vSize)
			// Apply scale from transform
			scale := tr.Scale.X()
			for i := range boxes {
				boxes[i].HalfExtents = boxes[i].HalfExtents.Mul(scale)
				boxes[i].LocalOffset = boxes[i].LocalOffset.Mul(scale)
			}
			initialized = len(boxes) > 0
		} else if vmc.CustomMap != nil {
			vSize := VoxelSize
			boxes = DecomposeXBrickMap(vmc.CustomMap, vSize)
			scale := tr.Scale.X()
			for i := range boxes {
				boxes[i].HalfExtents = boxes[i].HalfExtents.Mul(scale)
				boxes[i].LocalOffset = boxes[i].LocalOffset.Mul(scale)
			}
			initialized = len(boxes) > 0
		} else {
			server.mu.RLock()
			asset, ok := server.voxModels[vmc.VoxelModel]
			server.mu.RUnlock()

			if ok {
				vSize := VoxelSize
				boxes = DecomposeVoxModel(asset.VoxModel, vSize)
				// Apply scale from transform
				scale := tr.Scale.X()
				for i := range boxes {
					boxes[i].HalfExtents = boxes[i].HalfExtents.Mul(scale)
					boxes[i].LocalOffset = boxes[i].LocalOffset.Mul(scale)
				}
				initialized = len(boxes) > 0
			}
		}

		if initialized {
			// Calculate volume-weighted geometric center of all boxes
			weightedCenter := mgl32.Vec3{0, 0, 0}
			totalVolume := float32(0)
			if len(boxes) > 0 {
				for _, b := range boxes {
					volume := b.HalfExtents.X() * b.HalfExtents.Y() * b.HalfExtents.Z() * 8.0
					weightedCenter = weightedCenter.Add(b.LocalOffset.Mul(volume))
					totalVolume += volume
				}
				if totalVolume > 0 {
					weightedCenter = weightedCenter.Mul(1.0 / totalVolume)
				}

				// Shift all boxes to be relative to the new weighted center
				for i := range boxes {
					boxes[i].LocalOffset = boxes[i].LocalOffset.Sub(weightedCenter)
				}
			}

			cmd.AddComponents(eid, PhysicsModel{
				Boxes:        boxes,
				CenterOffset: weightedCenter,
			})
		} else {
			// No boxes (empty object), but we were asked to rebuild.
			// Set an empty physics model to clear any previous state.
			cmd.AddComponents(eid, PhysicsModel{
				Boxes: nil,
			})
		}
		return true
	})
}
