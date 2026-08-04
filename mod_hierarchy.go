package gekko

import (
	"reflect"

	"github.com/go-gl/mathgl/mgl32"
)

type HierarchyModule struct{}

func (HierarchyModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(TransformHierarchySystem).
			ProfileCategory("animation").
			InStage(PostUpdate).
			RunAlways(),
	)
}

func TransformHierarchySystem(cmd *Commands) {
	if cmd == nil {
		return
	}
	worlds := make(map[EntityId]*TransformComponent)
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, world *TransformComponent) bool {
		worlds[eid] = world
		return true
	})
	// Root objects: have TransformComponent but NO Parent
	MakeQuery2[LocalTransformComponent, TransformComponent](cmd).Without(Parent{}).Map(func(eid EntityId, local *LocalTransformComponent, tr *TransformComponent) bool {
		// Roots use world transform as authoritative source
		local.Position = tr.Position
		local.Rotation = tr.Rotation
		local.Scale = tr.Scale
		return true
	})
	type childTransform struct {
		local  *LocalTransformComponent
		parent EntityId
		world  *TransformComponent
	}
	children := map[EntityId]childTransform{}
	MakeQuery3[LocalTransformComponent, Parent, TransformComponent](cmd).Map(func(eid EntityId, local *LocalTransformComponent, parent *Parent, world *TransformComponent) bool {
		children[eid] = childTransform{local: local, parent: parent.Entity, world: world}
		return true
	})
	resolved := map[EntityId]bool{}
	resolving := map[EntityId]bool{}
	var resolve func(EntityId) bool
	resolve = func(eid EntityId) bool {
		if resolved[eid] {
			return true
		}
		child, ok := children[eid]
		if !ok || resolving[eid] {
			return false
		}
		resolving[eid] = true
		if _, isChild := children[child.parent]; isChild && !resolve(child.parent) {
			delete(resolving, eid)
			return false
		}
		parentWorld := worlds[child.parent]
		if parentWorld == nil {
			delete(resolving, eid)
			return false
		}
		scaledLocalPos := mgl32.Vec3{
			child.local.Position.X() * parentWorld.Scale.X(),
			child.local.Position.Y() * parentWorld.Scale.Y(),
			child.local.Position.Z() * parentWorld.Scale.Z(),
		}
		child.world.Position = parentWorld.Position.Add(parentWorld.Rotation.Rotate(scaledLocalPos))
		child.world.Rotation = parentWorld.Rotation.Mul(child.local.Rotation).Normalize()
		child.world.Scale = mgl32.Vec3{
			parentWorld.Scale.X() * child.local.Scale.X(),
			parentWorld.Scale.Y() * child.local.Scale.Y(),
			parentWorld.Scale.Z() * child.local.Scale.Z(),
		}
		delete(resolving, eid)
		resolved[eid] = true
		return true
	}
	for eid := range children {
		resolve(eid)
	}
}

// ReparentPreservingWorldTransform changes hierarchy ownership without moving
// the entity in world space. Presentation systems use it when an animated
// marker temporarily takes ownership of an already calibrated attachment.
func ReparentPreservingWorldTransform(cmd *Commands, entity, newParent EntityId) bool {
	if cmd == nil || entity == 0 || newParent == 0 || entity == newParent {
		return false
	}
	TransformHierarchySystem(cmd)
	world, _ := cmd.GetComponent(entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	parent, _ := cmd.GetComponent(entity, reflect.TypeOf(Parent{})).(*Parent)
	if world == nil || parent == nil {
		return false
	}
	previousParent, previousWorld := parent.Entity, *world
	parent.Entity = newParent
	if !setEntityWorldTransform(cmd, entity, previousWorld) {
		parent.Entity = previousParent
		return false
	}
	TransformHierarchySystem(cmd)
	return true
}
