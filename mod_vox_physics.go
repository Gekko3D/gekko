package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

type VoxPhysicsModule struct{}

func (m VoxPhysicsModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(VoxPhysicsPreCalcSystem).
			InStage(Update).
			RunAlways(),
	)
}

func DecomposeVoxModel(model VoxModel) []CollisionBox {
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
	const vSize = 0.1

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

func VoxPhysicsPreCalcSystem(cmd *Commands, server *AssetServer) {
	MakeQuery2[VoxelModelComponent, RigidBodyComponent](cmd).Map(func(eid EntityId, vmc *VoxelModelComponent, rb *RigidBodyComponent) bool {
		// Check if PhysicsModel already exists
		found := false
		allComps := cmd.GetAllComponents(eid)
		for _, c := range allComps {
			if _, ok := c.(PhysicsModel); ok {
				found = true
				break
			}
		}
		if found {
			return true
		}

		var boxes []CollisionBox
		initialized := false

		if vmc.CustomMap != nil {
			// Fallback for custom maps
			boxes = []CollisionBox{{
				HalfExtents: mgl32.Vec3{0.5, 0.5, 0.5},
				LocalOffset: mgl32.Vec3{0.5, 0.5, 0.5},
			}}
			initialized = true
		} else {
			if asset, ok := server.voxModels[vmc.VoxelModel]; ok {
				boxes = DecomposeVoxModel(asset.VoxModel)
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
		}
		return true
	})
}
