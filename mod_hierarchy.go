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
			for _, c := range allComps {
				if pw, ok := c.(*TransformComponent); ok {
					parentWorld = pw
					break
				}
				if pw, ok := c.(TransformComponent); ok {
					tmp := pw
					parentWorld = &tmp
					break
				}
			}

			if parentWorld != nil {
				// Propagate components directly to preserve scale signs (reflections)
				// WorldPos = ParentPos + ParentRot * (ParentScale * LocalPos)
				scaledLocalPos := mgl32.Vec3{
					local.Position.X() * parentWorld.Scale.X(),
					local.Position.Y() * parentWorld.Scale.Y(),
					local.Position.Z() * parentWorld.Scale.Z(),
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
