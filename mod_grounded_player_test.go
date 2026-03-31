package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestSpawnGroundedPlayerAtMarkerUsesModuleDefaults(t *testing.T) {
	app := NewApp()
	app.UseModules(GroundedPlayerControllerModule{
		Config: GroundedPlayerControllerConfig{
			Height:    1.65,
			EyeHeight: 1.5,
			Radius:    0.22,
		},
	})
	app.build()
	cmd := app.Commands()

	eid := SpawnGroundedPlayerAtMarker(cmd, content.LevelMarkerDef{
		ID:   "spawn",
		Kind: content.LevelMarkerKindPlayerSpawn,
		Transform: content.LevelTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
	})
	app.FlushCommands()

	var got *GroundedPlayerControllerComponent
	MakeQuery1[GroundedPlayerControllerComponent](cmd).Map(func(found EntityId, ctrl *GroundedPlayerControllerComponent) bool {
		if found == eid {
			got = ctrl
			return false
		}
		return true
	})
	if got == nil {
		t.Fatal("expected grounded player controller component")
	}
	if got.Height != 1.65 || got.EyeHeight != 1.5 || got.Radius != 0.22 {
		t.Fatalf("expected configured player dimensions, got %+v", *got)
	}
}

func TestGroundedMovementBlockedUsesPlayerRadiusAtDoorway(t *testing.T) {
	state := &VoxelRtState{
		RtApp: &app_rt.App{
			Scene:    core.NewScene(),
			Profiler: core.NewProfiler(),
		},
		instanceMap:    make(map[EntityId]*core.VoxelObject),
		caVolumeMap:    make(map[EntityId]*core.VoxelObject),
		objectToEntity: make(map[*core.VoxelObject]EntityId),
	}

	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	for y := 0; y < 3; y++ {
		obj.XBrickMap.SetVoxel(1, y, 1, 1)
	}
	obj.Transform.Scale = mgl32.Vec3{1, 1, 1}
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	state.RtApp.Scene.AddObject(obj)

	basePos := mgl32.Vec3{1.3, 0, 0}
	move := mgl32.Vec3{0, 0, 0.8}
	ctrl := &GroundedPlayerControllerComponent{
		Height:     1.7,
		Radius:     0.35,
		StepHeight: 0.6,
	}
	if !groundedMovementBlocked(state, basePos, move, ctrl) {
		t.Fatal("expected doorway side collision to block movement when player radius overlaps the jamb")
	}
}
