package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func newPhysicsSceneHarness() (*Commands, *AssetServer, *PhysicsWorld, *PhysicsProxy, *PhysicsSimulator, *Time) {
	ecs := MakeEcs()
	app := &App{
		resources: make(map[reflect.Type]any),
		ecs:       &ecs,
	}

	assets := &AssetServer{
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
	world := NewPhysicsWorld()
	proxy := &PhysicsProxy{}
	sim := NewPhysicsSimulator(world.SpatialGridCellSize)
	timeRes := &Time{Dt: 1.0 / 60.0, Alpha: 1.0}

	app.resources[reflect.TypeOf(AssetServer{})] = assets
	app.resources[reflect.TypeOf(PhysicsWorld{})] = world
	app.resources[reflect.TypeOf(PhysicsProxy{})] = proxy
	app.resources[reflect.TypeOf(Time{})] = timeRes

	return &Commands{app: app}, assets, world, proxy, sim, timeRes
}

func testEntityPosition(cmd *Commands, target EntityId) (mgl32.Vec3, bool) {
	var pos mgl32.Vec3
	found := false
	MakeQuery1[TransformComponent](cmd).Map(func(eid EntityId, tr *TransformComponent) bool {
		if eid != target {
			return true
		}
		pos = tr.Position
		found = true
		return false
	})
	return pos, found
}

func testEntityVelocity(cmd *Commands, target EntityId) (mgl32.Vec3, bool) {
	var vel mgl32.Vec3
	found := false
	MakeQuery1[RigidBodyComponent](cmd).Map(func(eid EntityId, rb *RigidBodyComponent) bool {
		if eid != target {
			return true
		}
		vel = rb.Velocity
		found = true
		return false
	})
	return vel, found
}

func stepSynchronousPhysics(cmd *Commands, world *PhysicsWorld, proxy *PhysicsProxy, sim *PhysicsSimulator, timeRes *Time, steps int) {
	for i := 0; i < steps; i++ {
		SynchronousPhysicsSystem(cmd, timeRes, world, proxy, sim)
		cmd.app.FlushCommands()
		PhysicsPullSystem(cmd, timeRes, proxy, world)
		cmd.app.FlushCommands()
	}
}

func TestSynchronousPhysicsScene_BallKnocksPinsAndProducesCollisionEvents(t *testing.T) {
	cmd, assets, world, proxy, sim, timeRes := newPhysicsSceneHarness()

	const voxelResolution = float32(0.2)

	floorModel := assets.CreateCubeModel(220, 4, 220, 1.0)
	laneModel := assets.CreateCubeModel(18, 1, 180, 1.0)
	pinModel := assets.CreateCubeModel(3, 8, 3, 1.0)
	ballModel := assets.CreateSphereModel(3.0, 1.0)

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{-22, -0.9, -34}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{VoxelModel: floorModel, PivotMode: PivotModeCorner, VoxelResolution: voxelResolution},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Friction: 0.7, Restitution: 0.1},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{-1.8, 0.1, -28}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{VoxelModel: laneModel, PivotMode: PivotModeCorner, VoxelResolution: voxelResolution},
		&RigidBodyComponent{IsStatic: true, Mass: 0},
		&ColliderComponent{Friction: 0.14, Restitution: 0.05},
	)

	pinIDs := make([]EntityId, 0, 10)
	baseZ := float32(-22.8)
	rowSpacing := float32(1.05)
	columnSpacing := float32(0.74)
	for row := 0; row < 4; row++ {
		z := baseZ - float32(row)*rowSpacing
		startX := -0.5 * float32(row) * columnSpacing
		for col := 0; col <= row; col++ {
			x := startX + float32(col)*columnSpacing
			pinIDs = append(pinIDs, cmd.AddEntity(
				&TransformComponent{Position: mgl32.Vec3{x, 1.1, z}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&VoxelModelComponent{VoxelModel: pinModel, VoxelResolution: voxelResolution},
				&RigidBodyComponent{Mass: 0.8, GravityScale: 1.0, LinearDamping: 0.01, AngularDamping: 0.04},
				&ColliderComponent{Friction: 0.45, Restitution: 0.12},
			))
		}
	}

	ballID := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0.9, 6.3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{VoxelModel: ballModel, VoxelResolution: voxelResolution},
		&RigidBodyComponent{
			Mass:           1.6,
			GravityScale:   1.0,
			Velocity:       mgl32.Vec3{0, 0, -18},
			LinearDamping:  0.002,
			AngularDamping: 0.01,
		},
		&ColliderComponent{Friction: 0.05, Restitution: 0.12},
	)
	cmd.app.FlushCommands()

	initialBallPos, ok := testEntityPosition(cmd, ballID)
	if !ok {
		t.Fatal("expected launched ball transform to exist")
	}
	initialPinPositions := make(map[EntityId]mgl32.Vec3, len(pinIDs))
	for _, pinID := range pinIDs {
		initialPinPos, ok := testEntityPosition(cmd, pinID)
		if !ok {
			t.Fatal("expected pin transform to exist")
		}
		initialPinPositions[pinID] = initialPinPos
	}
	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 1)
	proxy.DrainCollisionEvents()

	stepSynchronousPhysics(cmd, world, proxy, sim, timeRes, 180)

	finalBallPos, ok := testEntityPosition(cmd, ballID)
	if !ok {
		t.Fatal("expected launched ball transform after stepping")
	}
	if finalBallPos.Z() >= initialBallPos.Z()-10.0 {
		t.Fatalf("expected ball to move down-lane, initial=%v final=%v", initialBallPos, finalBallPos)
	}

	maxPinDisplacement := float32(0)
	for _, pinID := range pinIDs {
		finalPinPos, ok := testEntityPosition(cmd, pinID)
		if !ok {
			t.Fatal("expected pin transform after stepping")
		}
		maxPinDisplacement = maxf(maxPinDisplacement, finalPinPos.Sub(initialPinPositions[pinID]).Len())
	}
	if maxPinDisplacement < 0.015 {
		t.Fatalf("expected at least one pin to be displaced by collision, max displacement=%.4f", maxPinDisplacement)
	}

	events := proxy.DrainCollisionEvents()
	if len(events) == 0 {
		t.Fatal("expected collision events after launched ball hit the lane and pins")
	}
}
