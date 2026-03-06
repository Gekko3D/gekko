package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

type HierarchyModule struct{}

func (HierarchyModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(TransformHierarchySystem).
			InStage(PostUpdate).
			RunAlways(),
	)
}

func TransformHierarchySystem(cmd *Commands) {
	// Root objects: have TransformComponent but NO Parent
	MakeQuery2[LocalTransformComponent, TransformComponent](cmd).Without(Parent{}).Map(func(eid EntityId, local *LocalTransformComponent, tr *TransformComponent) bool {
		// Roots use world transform as authoritative source
		local.Position = tr.Position
		local.Rotation = tr.Rotation
		local.Scale = tr.Scale
		return true
	})
	// Looking at ca_ecs.go and ecs_query.go, MakeQuery doesn't seem to have "Without".
	// The manual check inside Map is correct.

	// Children: have Parent, LocalTransformComponent, and TransformComponent
	// We iterate multiple passes to handle deep hierarchies in a simple way.
PassLoop:
	for pass := 0; pass < 8; pass++ {
		changed := false
		MakeQuery3[LocalTransformComponent, Parent, TransformComponent](cmd).Map(func(eid EntityId, local *LocalTransformComponent, parent *Parent, world *TransformComponent) bool {
			// Get parent's world transform
			allComps := cmd.GetAllComponents(parent.Entity)
			var parentWorld *TransformComponent
			var isVoxel bool
			for _, c := range allComps {
				if pw, ok := c.(*TransformComponent); ok {
					parentWorld = pw
				}
				if pw, ok := c.(TransformComponent); ok {
					tmp := pw
					parentWorld = &tmp
				}
				if _, ok := c.(*VoxelModelComponent); ok {
					isVoxel = true
				}
				if _, ok := c.(VoxelModelComponent); ok {
					isVoxel = true
				}
			}

			if parentWorld != nil {
				// We need to apply the parent's pivot before rotating, just like the rendering pipeline!
				// If parent is a VoxelModel, its Pivot is in unscaled voxel units, so we must scale it to world units.
				// VoxelSize is in world units (e.g. 0.1)
				vSize := float32(1.0)
				if isVoxel {
					vSize = VoxelSize
				}

				scaledPivot := mgl32.Vec3{
					parentWorld.Pivot.X() * vSize,
					parentWorld.Pivot.Y() * vSize,
					parentWorld.Pivot.Z() * vSize,
				}

				diff := local.Position.Sub(scaledPivot)

				scaledLocalPos := mgl32.Vec3{
					diff.X() * parentWorld.Scale.X(),
					diff.Y() * parentWorld.Scale.Y(),
					diff.Z() * parentWorld.Scale.Z(),
				}

				newPos := parentWorld.Position.Add(parentWorld.Rotation.Rotate(scaledLocalPos))

				// WorldRot = ParentRot * LocalRot
				newRot := parentWorld.Rotation.Mul(local.Rotation).Normalize()

				// WorldScale = ParentScale * LocalScale
				newScale := mgl32.Vec3{
					parentWorld.Scale.X() * local.Scale.X(),
					parentWorld.Scale.Y() * local.Scale.Y(),
					parentWorld.Scale.Z() * local.Scale.Z(),
				}

				if newPos != world.Position || newRot != world.Rotation || newScale != world.Scale {
					world.Position = newPos
					world.Rotation = newRot
					world.Scale = newScale
					changed = true
				}
			}
			return true
		})
		if !changed {
			break PassLoop
		}
	}
}
