package gekko

import (
	"reflect"
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
	if cmd == nil || cmd.app == nil || cmd.app.ecs == nil || cmd.app.ecs.storage == nil {
		return
	}
	cmd.app.ecs.storage.transformHierarchy.update(cmd.app.ecs)
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
	if parent.Entity != previousParent {
		cmd.MarkComponentChanged(entity, reflect.TypeOf(Parent{}))
	}
	TransformHierarchySystem(cmd)
	return true
}
