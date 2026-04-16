package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestWaterBodyComponentExplicitRectNormalization(t *testing.T) {
	var nilBody *WaterBodyComponent
	if nilBody.Enabled() {
		t.Fatal("expected nil water body to be disabled")
	}
	if got := nilBody.NormalizedMode(); got != WaterBodyModeExplicitRect {
		t.Fatalf("expected default mode %q, got %q", WaterBodyModeExplicitRect, got)
	}
	if got := nilBody.NormalizedInset(); got != DefaultWaterBodyInset {
		t.Fatalf("expected default inset %v, got %v", DefaultWaterBodyInset, got)
	}
	if got := nilBody.NormalizedOverlap(); got != DefaultWaterBodyOverlap {
		t.Fatalf("expected default overlap %v, got %v", DefaultWaterBodyOverlap, got)
	}
	if got := nilBody.NormalizedMinCellSize(); got != DefaultWaterBodyMinCellSize {
		t.Fatalf("expected default min cell size %v, got %v", DefaultWaterBodyMinCellSize, got)
	}
	if got := nilBody.NormalizedEnableSkirt(); !got {
		t.Fatal("expected default skirt enablement")
	}

	body := &WaterBodyComponent{
		SurfaceY:        3,
		Depth:           2.5,
		RectHalfExtents: [2]float32{-1, 5},
		Color:           [3]float32{1.2, -1, 0.5},
		Opacity:         0,
	}
	if body.Enabled() {
		t.Fatal("expected invalid explicit-rect water body to be disabled")
	}
	if got := body.NormalizedRectHalfExtents(); got != ([2]float32{0, 5}) {
		t.Fatalf("unexpected normalized rect extents %v", got)
	}
	if got := body.NormalizedColor(); got != ([3]float32{1, 0, 0.5}) {
		t.Fatalf("unexpected normalized color %v", got)
	}
	if got := body.NormalizedOpacity(); got != 0.68 {
		t.Fatalf("expected default opacity 0.68, got %v", got)
	}
}

func TestWaterBodyComponentFitBoundsNormalizationAndValidation(t *testing.T) {
	enableSkirt := false
	body := &WaterBodyComponent{
		Mode:              WaterBodyModeFitBounds,
		SurfaceY:          1.5,
		Depth:             4.0,
		BoundsCenter:      mgl32.Vec3{1, 2, 3},
		BoundsHalfExtents: mgl32.Vec3{4, -2, 6},
		Inset:             -1,
		Overlap:           -1,
		MinCellSize:       0,
		MaxPatchCount:     0,
		EnableSkirt:       &enableSkirt,
	}
	if got := body.NormalizedMode(); got != WaterBodyModeFitBounds {
		t.Fatalf("expected fit-bounds mode, got %q", got)
	}
	if got := body.NormalizedBoundsCenter(); got != (mgl32.Vec3{1, 2, 3}) {
		t.Fatalf("unexpected bounds center %v", got)
	}
	if got := body.NormalizedBoundsHalfExtents(); got != (mgl32.Vec3{4, 0, 6}) {
		t.Fatalf("unexpected normalized bounds extents %v", got)
	}
	if got := body.NormalizedInset(); got != DefaultWaterBodyInset {
		t.Fatalf("expected default inset %v, got %v", DefaultWaterBodyInset, got)
	}
	if got := body.NormalizedOverlap(); got != DefaultWaterBodyOverlap {
		t.Fatalf("expected default overlap %v, got %v", DefaultWaterBodyOverlap, got)
	}
	if got := body.NormalizedMinCellSize(); got != DefaultWaterBodyMinCellSize {
		t.Fatalf("expected default min cell size %v, got %v", DefaultWaterBodyMinCellSize, got)
	}
	if got := body.NormalizedMaxPatchCount(); got != DefaultWaterBodyMaxPatchCount {
		t.Fatalf("expected default max patch count %v, got %v", DefaultWaterBodyMaxPatchCount, got)
	}
	if got := body.NormalizedEnableSkirt(); got {
		t.Fatal("expected explicit skirt disablement")
	}
	issues := body.ValidationIssues()
	if len(issues) == 0 {
		t.Fatal("expected validation issues for invalid fit-bounds extents and inset/overlap")
	}
}

func TestResolvedWaterPatchComponentEnablement(t *testing.T) {
	patch := &ResolvedWaterPatchComponent{
		Owner:       7,
		PatchIndex:  0,
		Kind:        WaterPatchKindSurface,
		HalfExtents: [2]float32{2, 1},
		Depth:       3,
	}
	if !patch.Enabled() {
		t.Fatal("expected resolved water patch to be enabled")
	}
	patch.Owner = 0
	if patch.Enabled() {
		t.Fatal("expected missing owner to disable patch")
	}
}

func TestWaterBodyResolutionSystemSeedsStateDeterministically(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}

	fitOwner := cmd.AddEntity(
		&WaterBodyComponent{
			Mode:              WaterBodyModeFitBounds,
			SurfaceY:          2,
			Depth:             3,
			BoundsCenter:      mgl32.Vec3{3, 0, 0},
			BoundsHalfExtents: mgl32.Vec3{4, 2, 4},
			MinCellSize:       1,
			Inset:             0,
		},
	)
	cmd.AddEntity(
		&WaterBodyComponent{
			Mode:            WaterBodyModeExplicitRect,
			SurfaceY:        1,
			Depth:           0,
			RectHalfExtents: [2]float32{2, 2},
		},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 0, 4}},
		&WaterBodyComponent{
			Mode:            WaterBodyModeExplicitRect,
			SurfaceY:        1.5,
			Depth:           2.0,
			RectHalfExtents: [2]float32{2, 1},
		},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	if len(state.ByEntity) != 3 {
		t.Fatalf("expected three resolution records, got %d", len(state.ByEntity))
	}
	if got := state.ByEntity[fitOwner].Status; got != WaterBodyResolutionStatusResolved {
		t.Fatalf("expected fit-bounds body to resolve, got %q", got)
	}
	resolvedCount := 0
	failedCount := 0
	patchCount := 0
	MakeQuery1[ResolvedWaterPatchComponent](cmd).Map(func(eid EntityId, patch *ResolvedWaterPatchComponent) bool {
		if patch != nil {
			patchCount++
		}
		return true
	})
	for owner, record := range state.ByEntity {
		switch record.Status {
		case WaterBodyResolutionStatusResolved:
			resolvedCount++
			if owner == fitOwner && record.PrimarySource != WaterFitSourceStaticCollider {
				t.Fatalf("expected fit-bounds body to resolve from static colliders, got %q", record.PrimarySource)
			}
		case WaterBodyResolutionStatusFailed:
			failedCount++
		default:
			t.Fatalf("unexpected resolution status %q", record.Status)
		}
	}
	if resolvedCount != 2 || failedCount != 1 {
		t.Fatalf("expected two resolved and one failed body, got resolved=%d failed=%d", resolvedCount, failedCount)
	}
	if patchCount != 2 {
		t.Fatalf("expected two resolved patch entities, got %d", patchCount)
	}
}

func TestWaterBodyResolutionSystemSplitsInteriorIntoDeterministicPatches(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}

	owner := cmd.AddEntity(
		&WaterBodyComponent{
			Mode:              WaterBodyModeFitBounds,
			SurfaceY:          2,
			Depth:             3,
			BoundsCenter:      mgl32.Vec3{3, 0, 0},
			BoundsHalfExtents: mgl32.Vec3{4, 2, 4},
			MinCellSize:       1,
			Inset:             0,
		},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 2}},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	record := state.ByEntity[owner]
	if record.Status != WaterBodyResolutionStatusResolved {
		t.Fatalf("expected resolved status, got %q", record.Status)
	}
	if record.PatchCount != 2 {
		t.Fatalf("expected two resolved patches, got %d", record.PatchCount)
	}

	centers := make([]mgl32.Vec3, 0, 2)
	MakeQuery1[ResolvedWaterPatchComponent](cmd).Map(func(eid EntityId, patch *ResolvedWaterPatchComponent) bool {
		if patch != nil && patch.Owner == owner {
			centers = append(centers, patch.Center)
		}
		return true
	})
	if len(centers) != 2 {
		t.Fatalf("expected two patch centers, got %d", len(centers))
	}
	if !(centers[0].X() < centers[1].X() || centers[1].X() < centers[0].X()) {
		t.Fatalf("expected split patches with distinct x centers, got %v", centers)
	}
}

func TestWaterBodyResolutionSystemFallsBackToVoxelOccupancy(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}
	assets := &AssetServer{}

	owner := cmd.AddEntity(
		&WaterBodyComponent{
			Mode:              WaterBodyModeFitBounds,
			SurfaceY:          2,
			Depth:             3,
			BoundsCenter:      mgl32.Vec3{3, 0, 0},
			BoundsHalfExtents: mgl32.Vec3{4, 2, 4},
			MinCellSize:       1,
			Inset:             0,
		},
	)

	xbm := volume.NewXBrickMap()
	for y := 0; y < 4; y++ {
		for z := 0; z < 7; z++ {
			xbm.SetVoxel(0, y, z, 1)
			xbm.SetVoxel(6, y, z, 1)
		}
		for x := 0; x < 7; x++ {
			xbm.SetVoxel(x, y, 0, 1)
			xbm.SetVoxel(x, y, 6, 1)
		}
	}
	geomID := assets.RegisterSharedVoxelGeometry(xbm, "")
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, -3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true},
		&VoxelModelComponent{SharedGeometry: geomID, VoxelResolution: 1, PivotMode: PivotModeCorner},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, assets, state)
	app.FlushCommands()

	record := state.ByEntity[owner]
	if record.Status != WaterBodyResolutionStatusResolved {
		t.Fatalf("expected voxel occupancy fallback to resolve, got %q", record.Status)
	}
	if record.PrimarySource != WaterFitSourceVoxelOccupancy {
		t.Fatalf("expected voxel occupancy source, got %q", record.PrimarySource)
	}
	if record.PatchCount != 1 {
		t.Fatalf("expected one resolved patch, got %d", record.PatchCount)
	}
}

func TestWaterBodyResolutionSystemPrefersColliderFitOverVoxelOccupancy(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}
	assets := &AssetServer{}

	owner := cmd.AddEntity(
		&WaterBodyComponent{
			Mode:              WaterBodyModeFitBounds,
			SurfaceY:          2,
			Depth:             3,
			BoundsCenter:      mgl32.Vec3{3, 0, 0},
			BoundsHalfExtents: mgl32.Vec3{4, 2, 4},
			MinCellSize:       1,
			Inset:             0,
		},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{IsStatic: true},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)

	xbm := volume.NewXBrickMap()
	for y := 0; y < 4; y++ {
		for z := 0; z < 7; z++ {
			xbm.SetVoxel(0, y, z, 1)
			xbm.SetVoxel(6, y, z, 1)
		}
		for x := 0; x < 7; x++ {
			xbm.SetVoxel(x, y, 0, 1)
			xbm.SetVoxel(x, y, 6, 1)
		}
	}
	geomID := assets.RegisterSharedVoxelGeometry(xbm, "")
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{20, 0, -3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&RigidBodyComponent{IsStatic: true},
		&VoxelModelComponent{SharedGeometry: geomID, VoxelResolution: 1, PivotMode: PivotModeCorner},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, assets, state)
	app.FlushCommands()

	record := state.ByEntity[owner]
	if record.Status != WaterBodyResolutionStatusResolved {
		t.Fatalf("expected collider fit to resolve, got %q", record.Status)
	}
	if record.PrimarySource != WaterFitSourceStaticCollider {
		t.Fatalf("expected static collider source to win, got %q", record.PrimarySource)
	}
}
