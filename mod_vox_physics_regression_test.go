package gekko

import (
	"reflect"
	"testing"

	rootassets "github.com/gekko3d/gekko/assets"
	"github.com/go-gl/mathgl/mgl32"
)

func newVoxelPhysicsPrecalcTestHarness() (*Commands, *AssetServer, *VoxelGridCache) {
	ecs := MakeEcs()
	app := &App{
		resources: make(map[reflect.Type]any),
		ecs:       &ecs,
	}

	return &Commands{app: app}, &AssetServer{
			voxModels:   make(map[AssetId]VoxelModelAsset),
			voxPalettes: make(map[AssetId]VoxelPaletteAsset),
		}, &VoxelGridCache{
			Snapshots:   make(map[EntityId]*voxelGridSnapshot),
			AssetGrids:  make(map[AssetId]*voxelGridAssetCache),
			BuildStamps: make(map[EntityId]voxelPhysicsBuildStamp),
		}
}

func mustPhysicsModel(t *testing.T, cmd *Commands, eid EntityId) PhysicsModel {
	t.Helper()

	for _, component := range cmd.GetAllComponents(eid) {
		if model, ok := component.(PhysicsModel); ok {
			return model
		}
	}

	t.Fatalf("missing PhysicsModel for entity %d", eid)
	return PhysicsModel{}
}

func mustTransformComponent(t *testing.T, cmd *Commands, eid EntityId) TransformComponent {
	t.Helper()

	for _, component := range cmd.GetAllComponents(eid) {
		if tr, ok := component.(TransformComponent); ok {
			return tr
		}
	}

	t.Fatalf("missing TransformComponent for entity %d", eid)
	return TransformComponent{}
}

func mustVoxelModelComponent(t *testing.T, cmd *Commands, eid EntityId) VoxelModelComponent {
	t.Helper()

	for _, component := range cmd.GetAllComponents(eid) {
		if vm, ok := component.(VoxelModelComponent); ok {
			return vm
		}
	}

	t.Fatalf("missing VoxelModelComponent for entity %d", eid)
	return VoxelModelComponent{}
}

func TestVoxPhysicsPreCalcSystem_AssetGridUsesPerEntityScale(t *testing.T) {
	cmd, server, cache := newVoxelPhysicsPrecalcTestHarness()

	assetID := rootassets.NewID()
	server.voxModels[assetID] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: 1,
			SizeY: 1,
			SizeZ: 1,
			Voxels: []Voxel{{
				X:          0,
				Y:          0,
				Z:          0,
				ColorIndex: 1,
			}},
		},
	}

	eidA := cmd.AddEntity(
		VoxelModelComponent{VoxelModel: assetID},
		RigidBodyComponent{Mass: 1},
		TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
	)
	eidB := cmd.AddEntity(
		VoxelModelComponent{VoxelModel: assetID},
		RigidBodyComponent{Mass: 1},
		TransformComponent{Scale: mgl32.Vec3{2, 2, 2}},
	)
	cmd.app.FlushCommands()

	VoxPhysicsPreCalcSystem(cmd, server, nil, cache)
	cmd.app.FlushCommands()

	modelA := mustPhysicsModel(t, cmd, eidA)
	modelB := mustPhysicsModel(t, cmd, eidB)
	if modelA.Grid == nil || modelB.Grid == nil {
		t.Fatal("expected both asset-backed entities to receive voxel grids")
	}

	if got, want := modelA.Grid.VoxelSize(), VoxelSize; got != want {
		t.Fatalf("expected first entity voxel size %.3f, got %.3f", want, got)
	}
	if got, want := modelB.Grid.VoxelSize(), VoxelSize*2; got != want {
		t.Fatalf("expected second entity voxel size %.3f, got %.3f", want, got)
	}
	if len(cache.AssetGrids) != 1 {
		t.Fatalf("expected one shared asset-geometry cache entry, got %d", len(cache.AssetGrids))
	}
	if cache.Snapshots[eidA] == cache.Snapshots[eidB] {
		t.Fatal("expected per-entity snapshots to differ when scale differs")
	}
}

func TestVoxPhysicsPreCalcSystem_RebuildsWhenScaleChanges(t *testing.T) {
	cmd, server, cache := newVoxelPhysicsPrecalcTestHarness()

	assetID := rootassets.NewID()
	server.voxModels[assetID] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: 2,
			SizeY: 2,
			SizeZ: 2,
			Voxels: []Voxel{{
				X:          0,
				Y:          0,
				Z:          0,
				ColorIndex: 1,
			}},
		},
	}

	eid := cmd.AddEntity(
		VoxelModelComponent{VoxelModel: assetID},
		RigidBodyComponent{Mass: 1},
		TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
	)
	cmd.app.FlushCommands()

	VoxPhysicsPreCalcSystem(cmd, server, nil, cache)
	cmd.app.FlushCommands()
	initial := mustPhysicsModel(t, cmd, eid)

	updatedTransform := mustTransformComponent(t, cmd, eid)
	updatedTransform.Scale = mgl32.Vec3{2, 2, 2}
	cmd.AddComponents(eid, updatedTransform)
	cmd.app.FlushCommands()

	VoxPhysicsPreCalcSystem(cmd, server, nil, cache)
	cmd.app.FlushCommands()
	rebuilt := mustPhysicsModel(t, cmd, eid)

	if rebuilt.Grid == nil {
		t.Fatal("expected rebuilt PhysicsModel to keep voxel grid")
	}
	if rebuilt.Grid.VoxelSize() != VoxelSize*2 {
		t.Fatalf("expected rebuilt voxel size %.3f, got %.3f", VoxelSize*2, rebuilt.Grid.VoxelSize())
	}
	if rebuilt.CenterOffset != initial.CenterOffset.Mul(2) {
		t.Fatalf("expected center offset to rescale from %v to %v, got %v", initial.CenterOffset, initial.CenterOffset.Mul(2), rebuilt.CenterOffset)
	}
	if len(rebuilt.Boxes) != 1 || len(initial.Boxes) != 1 {
		t.Fatalf("expected single-box models before and after rebuild, got %d and %d", len(initial.Boxes), len(rebuilt.Boxes))
	}
	if rebuilt.Boxes[0].HalfExtents != initial.Boxes[0].HalfExtents.Mul(2) {
		t.Fatalf("expected half extents to rescale from %v to %v, got %v", initial.Boxes[0].HalfExtents, initial.Boxes[0].HalfExtents.Mul(2), rebuilt.Boxes[0].HalfExtents)
	}
	if initial.Grid == rebuilt.Grid {
		t.Fatal("expected scale change to rebuild the per-entity voxel snapshot")
	}
}

func TestVoxPhysicsPreCalcSystem_RebuildsWhenAssetChanges(t *testing.T) {
	cmd, server, cache := newVoxelPhysicsPrecalcTestHarness()

	smallAsset := rootassets.NewID()
	server.voxModels[smallAsset] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: 1,
			SizeY: 1,
			SizeZ: 1,
			Voxels: []Voxel{{
				X:          0,
				Y:          0,
				Z:          0,
				ColorIndex: 1,
			}},
		},
	}
	largeAsset := rootassets.NewID()
	server.voxModels[largeAsset] = VoxelModelAsset{
		VoxModel: VoxModel{
			SizeX: 3,
			SizeY: 2,
			SizeZ: 1,
			Voxels: []Voxel{{
				X:          2,
				Y:          1,
				Z:          0,
				ColorIndex: 1,
			}},
		},
	}

	eid := cmd.AddEntity(
		VoxelModelComponent{VoxelModel: smallAsset},
		RigidBodyComponent{Mass: 1},
		TransformComponent{Scale: mgl32.Vec3{1, 1, 1}},
	)
	cmd.app.FlushCommands()

	VoxPhysicsPreCalcSystem(cmd, server, nil, cache)
	cmd.app.FlushCommands()
	initial := mustPhysicsModel(t, cmd, eid)

	updatedModel := mustVoxelModelComponent(t, cmd, eid)
	updatedModel.VoxelModel = largeAsset
	cmd.AddComponents(eid, updatedModel)
	cmd.app.FlushCommands()

	VoxPhysicsPreCalcSystem(cmd, server, nil, cache)
	cmd.app.FlushCommands()
	rebuilt := mustPhysicsModel(t, cmd, eid)

	if rebuilt.CenterOffset == initial.CenterOffset {
		t.Fatalf("expected asset change to rebuild center offset, both were %v", rebuilt.CenterOffset)
	}
	if len(rebuilt.Boxes) != 1 {
		t.Fatalf("expected rebuilt PhysicsModel to keep one box, got %d", len(rebuilt.Boxes))
	}
	wantHalfExtents := mgl32.Vec3{1.5 * VoxelSize, 1.0 * VoxelSize, 0.5 * VoxelSize}
	if rebuilt.Boxes[0].HalfExtents != wantHalfExtents {
		t.Fatalf("expected rebuilt half extents %v, got %v", wantHalfExtents, rebuilt.Boxes[0].HalfExtents)
	}
	if rebuilt.Grid == initial.Grid {
		t.Fatal("expected asset change to rebuild the voxel grid snapshot")
	}
}
