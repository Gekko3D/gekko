package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func newPhysicsBootstrapHarness() (*Commands, *AssetServer, *PhysicsWorld, *PhysicsProxy, *Time) {
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
	timeRes := &Time{Dt: 1.0 / 60.0}

	app.resources[reflect.TypeOf(AssetServer{})] = assets
	app.resources[reflect.TypeOf(PhysicsWorld{})] = world
	app.resources[reflect.TypeOf(PhysicsProxy{})] = proxy
	app.resources[reflect.TypeOf(Time{})] = timeRes

	return &Commands{app: app}, assets, world, proxy, timeRes
}

func TestRenderToPhysicsOffsetWithAssets_UsesGeometryPivotWhenTransformPivotIsUnset(t *testing.T) {
	_, assets, _, _, _ := newPhysicsBootstrapHarness()
	model := assets.CreateCubeModel(6, 6, 6, 1.0)

	tr := &TransformComponent{
		Position: mgl32.Vec3{10, 20, 30},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	vm := &VoxelModelComponent{VoxelModel: model}
	center := mgl32.Vec3{3 * VoxelSize, 3 * VoxelSize, 3 * VoxelSize}
	pm := &PhysicsModel{CenterOffset: center}

	got := renderToPhysicsOffsetWithAssets(assets, tr, pm, vm)
	if got != (mgl32.Vec3{}) {
		t.Fatalf("expected centered voxel offset to resolve to zero, got %v", got)
	}
}

func TestPhysicsPushSystem_BootstrapsVoxelBodiesWithoutPrecomputedPhysicsModel(t *testing.T) {
	cmd, assets, world, proxy, timeRes := newPhysicsBootstrapHarness()
	model := assets.CreateCubeModel(6, 6, 6, 1.0)

	eid := cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{10, 20, 30},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&VoxelModelComponent{VoxelModel: model},
		&RigidBodyComponent{Mass: 1, GravityScale: 1},
		&ColliderComponent{Friction: 0.4, Restitution: 0.2},
	)
	cmd.app.FlushCommands()

	PhysicsPushSystem(cmd, timeRes, world, proxy)
	snapshot := proxy.pendingState.Load()
	if snapshot == nil {
		t.Fatal("expected physics snapshot to be produced")
	}
	if len(snapshot.Entities) != 1 {
		t.Fatalf("expected one physics entity in snapshot, got %d", len(snapshot.Entities))
	}

	got := snapshot.Entities[0]
	if got.Eid != eid {
		t.Fatalf("expected entity %d, got %d", eid, got.Eid)
	}
	if got.Pos != (mgl32.Vec3{10, 20, 30}) {
		t.Fatalf("expected physics position to match centered render position, got %v", got.Pos)
	}
	if len(got.Model.Boxes) != 1 {
		t.Fatalf("expected synthesized physics model to contain one box, got %d", len(got.Model.Boxes))
	}
	wantCenter := mgl32.Vec3{3 * VoxelSize, 3 * VoxelSize, 3 * VoxelSize}
	if got.Model.CenterOffset != wantCenter {
		t.Fatalf("expected synthesized center offset %v, got %v", wantCenter, got.Model.CenterOffset)
	}

	cmd.app.FlushCommands()
	if !HasComponent[PhysicsModel](cmd, eid) {
		t.Fatal("expected fallback physics model to be persisted onto the entity")
	}
}

func TestDampingRetentionFactor_SupportsAmountAndRetentionStyles(t *testing.T) {
	if got := dampingRetentionFactor(0.02, 0.999); got != 0.98 {
		t.Fatalf("expected 0.02 to mean 2%% damping -> 0.98 retention, got %.3f", got)
	}
	if got := dampingRetentionFactor(0.99, 0.999); got != 0.99 {
		t.Fatalf("expected 0.99 to remain a 0.99 retention factor, got %.3f", got)
	}
}
