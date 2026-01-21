package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// voxelSyncInstances synchronizes ECS voxel model entities to RtApp instances.
func voxelSyncInstances(state *VoxelRtState, server *AssetServer, cmd *Commands) {
	if state == nil || state.RtApp == nil {
		return
	}
	state.RtApp.Profiler.BeginScope("Sync Instances")
	currentEntities := make(map[EntityId]bool)

	// Collect instances from models
	MakeQuery3[TransformComponent, VoxelModelComponent, WorldTransform](cmd).Map(func(entityId EntityId, transform *TransformComponent, vox *VoxelModelComponent, wt *WorldTransform) bool {
		currentEntities[entityId] = true

		obj, exists := state.instanceMap[entityId]
		if !exists {
			// Create new object for this entity
			modelTemplate, ok := state.loadedModels[vox.VoxelModel]
			if !ok {
				// Load model from Gekko assets
				gekkoModel := server.voxModels[vox.VoxelModel]
				gekkoPalette := server.voxPalettes[vox.VoxelPalette]

				xbm := volume.NewXBrickMap()
				for _, v := range gekkoModel.VoxModel.Voxels {
					xbm.SetVoxel(int(v.X), int(v.Z), int(v.Y), v.ColorIndex)
				}

				modelTemplate = core.NewVoxelObject()
				modelTemplate.XBrickMap = xbm
				modelTemplate.MaterialTable = state.buildMaterialTable(&gekkoPalette)
				state.loadedModels[vox.VoxelModel] = modelTemplate
			}

			obj = core.NewVoxelObject()

			if vox.CustomMap != nil {
				obj.XBrickMap = vox.CustomMap.Copy()
				gekkoPalette := server.voxPalettes[vox.VoxelPalette]
				obj.MaterialTable = state.buildMaterialTable(&gekkoPalette)
			} else {
				obj.XBrickMap = modelTemplate.XBrickMap.Copy()
				obj.MaterialTable = modelTemplate.MaterialTable
			}

			state.RtApp.Scene.AddObject(obj)
			state.instanceMap[entityId] = obj
		}

		if wt != nil {
			obj.Transform.Position = wt.Position
			obj.Transform.Rotation = wt.Rotation
		} else {
			// Persistent scaling: we don't want to sync scale from ECS if we are using metric scaling.
			// However, we MUST sync Position back if it changed in the renderer.
			if state.RtApp.Editor.SelectedObject == obj {
				if obj.Transform.Position.Sub(transform.Position).Len() > 0.001 {
					transform.Position = obj.Transform.Position
				}
			} else {
				obj.Transform.Position = transform.Position
			}
			obj.Transform.Rotation = transform.Rotation
		}

		// Metric system: Renderer Scale is ALWAYS TargetVoxelSize.
		vSize := state.RtApp.Scene.TargetVoxelSize
		if vSize == 0 {
			vSize = 0.1
		}

		scale := transform.Scale
		if wt != nil {
			scale = wt.Scale
		}
		obj.Transform.Scale = mgl32.Vec3{vSize * scale.X(), vSize * scale.Y(), vSize * scale.Z()}
		obj.Transform.Dirty = true

		return true
	}, WorldTransform{})

	for eid, obj := range state.instanceMap {
		if !currentEntities[eid] {
			state.RtApp.Scene.RemoveObject(obj)
			delete(state.instanceMap, eid)
		}
	}
	state.RtApp.Profiler.EndScope("Sync Instances")
}