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

		var aabbMin, aabbMax mgl32.Vec3
		initialized := false

		if vmc.CustomMap != nil {
			// For CustomMap, we'd ideally calculate the bounds from sectors.
			// For now, let's use a placeholder if we can't easily traverse sectors.
			// Actually, let's try to get it if possible, but keep it simple as requested.
			// We'll use a 1x1x1 box as fallback for custom maps for now.
			aabbMax = mgl32.Vec3{1, 1, 1}
			initialized = true
		} else {
			if asset, ok := server.voxModels[vmc.VoxelModel]; ok {
				if len(asset.VoxModel.Voxels) > 0 {
					v := asset.VoxModel.Voxels[0]
					minX, minY, minZ := float32(v.X), float32(v.Y), float32(v.Z)
					maxX, maxY, maxZ := minX+1, minY+1, minZ+1

					for i := 1; i < len(asset.VoxModel.Voxels); i++ {
						v := asset.VoxModel.Voxels[i]
						vx, vy, vz := float32(v.X), float32(v.Y), float32(v.Z)
						if vx < minX {
							minX = vx
						}
						if vy < minY {
							minY = vy
						}
						if vz < minZ {
							minZ = vz
						}
						if vx+1 > maxX {
							maxX = vx + 1
						}
						if vy+1 > maxY {
							maxY = vy + 1
						}
						if vz+1 > maxZ {
							maxZ = vz + 1
						}
					}
					aabbMin = mgl32.Vec3{minX, minY, minZ}
					aabbMax = mgl32.Vec3{maxX, maxY, maxZ}
					initialized = true
				}
			}
		}

		if initialized {
			cmd.AddComponents(eid, PhysicsModel{
				AABBMin: aabbMin,
				AABBMax: aabbMax,
			})
		}
		return true
	})
}
