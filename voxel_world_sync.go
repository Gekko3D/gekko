package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// voxelSyncWorld syncs WorldComponent-backed XBrickMap objects into the renderer,
// creating a single VoxelObject per world and keeping transforms in metric scale.
func voxelSyncWorld(state *VoxelRtState, cmd *Commands) {
	if state == nil || state.RtApp == nil {
		return
	}
	currentWorlds := make(map[EntityId]bool)
	MakeQuery1[WorldComponent](cmd).Map(func(eid EntityId, world *WorldComponent) bool {
		currentWorlds[eid] = true
		obj, exists := state.worldMap[eid]
		if !exists {
			obj = core.NewVoxelObject()
			// Default material table for world
			mats := make([]core.Material, 256)
			for i := range mats {
				mats[i] = core.DefaultMaterial()
				mats[i].BaseColor = [4]uint8{120, 120, 120, 255}
				if i == 0 {
					mats[i].Transparency = 1.0 // Air is transparent
				}
			}
			// Ground color (index 1)
			mats[1].BaseColor = [4]uint8{100, 255, 100, 255}

			obj.MaterialTable = mats
			state.RtApp.Scene.AddObject(obj)
			state.worldMap[eid] = obj
		}

		// Bind the shared world XBrickMap
		obj.XBrickMap = world.GetXBrickMap()

		// World is anchored at origin; scale uses target voxel size
		vSize := state.RtApp.Scene.TargetVoxelSize
		if vSize == 0 {
			vSize = 0.1
		}
		obj.Transform.Position = mgl32.Vec3{0, 0, 0}
		obj.Transform.Rotation = mgl32.QuatIdent()
		obj.Transform.Scale = mgl32.Vec3{vSize, vSize, vSize}
		obj.Transform.Dirty = true

		return true
	})
	// Remove stale world objects
	for eid, obj := range state.worldMap {
		if !currentWorlds[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.worldMap, eid)
		}
	}
}