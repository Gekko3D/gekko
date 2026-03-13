package gekko

import (
	"fmt"
)

// LifetimeComponent allows an entity to automatically be removed after a set duration.
type LifetimeComponent struct {
	TimeLeft float32
}

// DebrisComponent marks small fragments spawned from destruction.
type DebrisComponent struct {
	Age        float32 // Seconds since spawn
	MaxAge     float32 // Despawn after this many seconds
	VoxelCount int     // Number of voxels at spawn
}

type LifecycleModule struct{}

func (mod LifecycleModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(lifetimeSystem).
			InStage(PostUpdate).
			RunAlways(),
	)
	app.UseSystem(
		System(debrisCleanupSystem).
			InStage(Update).
			RunAlways(),
	)
}

func debrisCleanupSystem(time *Time, state *VoxelRtState, cmd *Commands) {
	dt := float32(time.Dt)
	if dt <= 0 || state == nil {
		return
	}

	MakeQuery1[DebrisComponent](cmd).Map(func(eid EntityId, deb *DebrisComponent) bool {
		deb.Age += dt

		// 1. Remove if too old
		if deb.Age > deb.MaxAge {
			cmd.RemoveEntity(eid)
			return true
		}

		// 2. Remove if fully destroyed (0 voxels left)
		if state.IsEntityEmpty(eid) {
			cmd.RemoveEntity(eid)
			return true
		}

		// 3. Optional visual fade out in the last 2 seconds
		fadeStart := deb.MaxAge - 2.0
		if deb.Age > fadeStart {
			alpha := 1.0 - (deb.Age-fadeStart)/2.0
			obj := state.GetVoxelObject(eid)
			if obj != nil && len(obj.MaterialTable) > 0 {
				transparency := 1.0 - alpha
				for i := range obj.MaterialTable {
					// Don't fade air (i=0) or already more transparent materials
					if i > 0 && transparency > obj.MaterialTable[i].Transparency {
						obj.MaterialTable[i].Transparency = transparency
					}
				}
			}
		}

		return true
	})
}

func lifetimeSystem(time *Time, cmd *Commands) {
	dt := float32(time.Dt)
	if dt <= 0 {
		return
	}
	MakeQuery1[LifetimeComponent](cmd).Map(func(eid EntityId, lt *LifetimeComponent) bool {
		lt.TimeLeft -= dt
		if lt.TimeLeft <= 0 {
			fmt.Printf("ENGINE: Lifecycle marking entity %v for removal\n", eid)
			cmd.RemoveEntity(eid)
		}
		return true
	})
}
