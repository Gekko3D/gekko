package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

func TransformHierarchySystem(cmd *Commands) {
	// Root objects
	MakeQuery3[LocalTransform, WorldTransform, TransformComponent](cmd).WithoutTypes(Parent{}).Map(func(eid EntityId, local *LocalTransform, world *WorldTransform, tr *TransformComponent) bool {
		// Physics/Gameplay Sync: If this is a root with a TransformComponent,
		// they might be updated by systems that don't know about the hierarchy.
		if tr != nil {
			local.Position = tr.Position
			local.Rotation = tr.Rotation
			local.Scale = tr.Scale
		}

		world.Position = local.Position
		world.Rotation = local.Rotation
		world.Scale = local.Scale
		return true
	}, TransformComponent{})

	// Children (recursively or iteratively - for now iterative simplest for ECS)
	// In a real engine we might need to handle depths, but let's try a few passes or a topological approach.
	// Since we are likely only one level deep for a skeleton, let's just run it.
	// We can repeat mapping for several passes to propagate deeper if needed.
	for pass := 0; pass < 4; pass++ {
		MakeQuery3[LocalTransform, Parent, WorldTransform](cmd).WithTypes(Parent{}).Map(func(eid EntityId, local *LocalTransform, parent *Parent, world *WorldTransform) bool {
			// Get parent's world transform
			allComps := cmd.GetAllComponents(parent.Entity)
			var parentWorld *WorldTransform
			for _, c := range allComps {
				if pw, ok := c.(WorldTransform); ok {
					parentWorld = &pw
					break
				}
			}

			if parentWorld != nil {
				// Propagate components directly to preserve scale signs (reflections) and avoid Mat4ToQuat decomposition errors.
				// WorldPos = ParentPos + ParentRot * (ParentScale * LocalPos)
				scaledLocalPos := mgl32.Vec3{
					local.Position.X() * parentWorld.Scale.X(),
					local.Position.Y() * parentWorld.Scale.Y(),
					local.Position.Z() * parentWorld.Scale.Z(),
				}
				world.Position = parentWorld.Position.Add(parentWorld.Rotation.Rotate(scaledLocalPos))

				// WorldRot = ParentRot * LocalRot
				// Note: Reflections are handled by the Scale component.
				world.Rotation = parentWorld.Rotation.Mul(local.Rotation).Normalize()

				// WorldScale = ParentScale * LocalScale
				world.Scale = mgl32.Vec3{
					parentWorld.Scale.X() * local.Scale.X(),
					parentWorld.Scale.Y() * local.Scale.Y(),
					parentWorld.Scale.Z() * local.Scale.Z(),
				}
			}
			return true
		})
	}
}