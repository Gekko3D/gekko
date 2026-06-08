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
	if got := nilBody.NormalizedVisualCellSize(); got != DefaultWaterVisualCellSize {
		t.Fatalf("expected default visual cell size %v, got %v", DefaultWaterVisualCellSize, got)
	}
	if got := nilBody.NormalizedDirectLightOcclusion(); got != 0 {
		t.Fatalf("expected default direct light occlusion 0, got %v", got)
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
	body.DirectLightOcclusion = 0.4
	if got := body.NormalizedDirectLightOcclusion(); got != 0.4 {
		t.Fatalf("expected direct light occlusion 0.4, got %v", got)
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
	body.MinCellSize = 0.35
	if got := body.NormalizedVisualCellSize(); got != 0.35 {
		t.Fatalf("expected visual cell size fallback from min cell size 0.35, got %v", got)
	}
	body.VisualCellSize = 0.5
	if got := body.NormalizedVisualCellSize(); got != 0.5 {
		t.Fatalf("expected explicit visual cell size 0.5, got %v", got)
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
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
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

func TestWaterBodyResolutionSystemUsesExplicitRectTransform(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{-15, 0, -20}},
		&WaterBodyComponent{
			Mode:            WaterBodyModeExplicitRect,
			SurfaceY:        -3,
			Depth:           2,
			RectHalfExtents: [2]float32{4, 5},
			ContinuityGroup: "pool-a",
			VisualCellSize:  0.4,
		},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	var found bool
	MakeQuery1[ResolvedWaterPatchComponent](cmd).Map(func(_ EntityId, patch *ResolvedWaterPatchComponent) bool {
		found = true
		if patch.Center != (mgl32.Vec3{-15, -3, -20}) {
			t.Fatalf("patch center = %+v", patch.Center)
		}
		if patch.HalfExtents != ([2]float32{4, 5}) {
			t.Fatalf("patch half extents = %+v", patch.HalfExtents)
		}
		if patch.VisualCellSize != 0.4 {
			t.Fatalf("patch visual cell size = %+v", patch.VisualCellSize)
		}
		if patch.ContinuityGroup != "pool-a" {
			t.Fatalf("patch continuity group = %q", patch.ContinuityGroup)
		}
		return true
	})
	if !found {
		t.Fatal("expected resolved explicit water patch")
	}
}

func TestWaterBodyResolutionSystemRebuildsWhenExplicitBodyChanges(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}

	owner := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{1, 0, 2}},
		&WaterBodyComponent{
			Mode:            WaterBodyModeExplicitRect,
			SurfaceY:        1,
			Depth:           2,
			RectHalfExtents: [2]float32{2, 3},
		},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	MakeQuery1[WaterBodyComponent](cmd).Map(func(eid EntityId, body *WaterBodyComponent) bool {
		if eid == owner {
			body.SurfaceY = 4
			body.RectHalfExtents = [2]float32{5, 6}
			return false
		}
		return true
	})

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	patchCount := 0
	MakeQuery1[ResolvedWaterPatchComponent](cmd).Map(func(_ EntityId, patch *ResolvedWaterPatchComponent) bool {
		if patch != nil && patch.Owner == owner {
			patchCount++
			if patch.Center != (mgl32.Vec3{1, 4, 2}) {
				t.Fatalf("patch center after rebuild = %+v", patch.Center)
			}
			if patch.HalfExtents != ([2]float32{5, 6}) {
				t.Fatalf("patch extents after rebuild = %+v", patch.HalfExtents)
			}
		}
		return true
	})
	if patchCount != 1 {
		t.Fatalf("expected one rebuilt patch, got %d", patchCount)
	}
	if got := state.ByEntity[owner].PatchCount; got != 1 {
		t.Fatalf("state patch count after rebuild = %d", got)
	}
}

func TestWaterBodyResolutionSystemRetriesFailedFitWhenSourcesArrive(t *testing.T) {
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
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()
	if got := state.ByEntity[owner].Status; got != WaterBodyResolutionStatusFailed {
		t.Fatalf("expected initial failed resolution, got %q", got)
	}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 2, 0}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	record := state.ByEntity[owner]
	if record.Status != WaterBodyResolutionStatusResolved || record.PatchCount != 1 {
		t.Fatalf("expected retry to resolve one patch, got %+v", record)
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
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 0}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
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

func TestWaterBodyFitBoundsOverlapExpandsInteriorAndClampsToBounds(t *testing.T) {
	body := &WaterBodyComponent{
		Mode:              WaterBodyModeFitBounds,
		SurfaceY:          0,
		Depth:             1,
		BoundsCenter:      mgl32.Vec3{0, 0, 0},
		BoundsHalfExtents: mgl32.Vec3{3, 1, 3},
		MinCellSize:       1,
		Inset:             0,
		Overlap:           0.25,
	}
	colliders := []waterStaticCollider{
		{Min: mgl32.Vec3{-3, -1, -3}, Max: mgl32.Vec3{-2, 1, 3}},
		{Min: mgl32.Vec3{2, -1, -3}, Max: mgl32.Vec3{3, 1, 3}},
		{Min: mgl32.Vec3{-3, -1, -3}, Max: mgl32.Vec3{3, 1, -2}},
		{Min: mgl32.Vec3{-3, -1, 2}, Max: mgl32.Vec3{3, 1, 3}},
	}

	rects := fitWaterBodyBoundsFromStaticColliders(body, colliders)
	if len(rects) != 1 {
		t.Fatalf("expected one fitted rect, got %d", len(rects))
	}
	rect := rects[0]
	if rect.minX != -2.25 || rect.maxX != 2.25 || rect.minZ != -2.25 || rect.maxZ != 2.25 {
		t.Fatalf("expected overlap-expanded interior rect, got %+v", rect)
	}

	body.Overlap = 2
	rects = fitWaterBodyBoundsFromStaticColliders(body, colliders)
	if len(rects) != 1 {
		t.Fatalf("expected one fitted rect after large overlap, got %d", len(rects))
	}
	rect = rects[0]
	if rect.minX != -3 || rect.maxX != 3 || rect.minZ != -3 || rect.maxZ != 3 {
		t.Fatalf("expected overlap to clamp to authored bounds, got %+v", rect)
	}
}

func TestWaterBodyFitBoundsDisableSkirtSuppressesOverlapExpansion(t *testing.T) {
	disableSkirt := false
	body := &WaterBodyComponent{
		Mode:              WaterBodyModeFitBounds,
		SurfaceY:          0,
		Depth:             1,
		BoundsCenter:      mgl32.Vec3{0, 0, 0},
		BoundsHalfExtents: mgl32.Vec3{3, 1, 3},
		MinCellSize:       1,
		Inset:             0,
		Overlap:           0.25,
		EnableSkirt:       &disableSkirt,
	}
	colliders := []waterStaticCollider{
		{Min: mgl32.Vec3{-3, -1, -3}, Max: mgl32.Vec3{-2, 1, 3}},
		{Min: mgl32.Vec3{2, -1, -3}, Max: mgl32.Vec3{3, 1, 3}},
		{Min: mgl32.Vec3{-3, -1, -3}, Max: mgl32.Vec3{3, 1, -2}},
		{Min: mgl32.Vec3{-3, -1, 2}, Max: mgl32.Vec3{3, 1, 3}},
	}

	rects := fitWaterBodyBoundsFromStaticColliders(body, colliders)
	if len(rects) != 1 {
		t.Fatalf("expected one fitted rect, got %d", len(rects))
	}
	rect := rects[0]
	if rect.minX != -2 || rect.maxX != 2 || rect.minZ != -2 || rect.maxZ != 2 {
		t.Fatalf("expected disabled skirt to suppress overlap expansion, got %+v", rect)
	}
}

func TestWaterBodyFitBoundsHonorsAuthoredYBounds(t *testing.T) {
	body := &WaterBodyComponent{
		Mode:              WaterBodyModeFitBounds,
		SurfaceY:          0,
		Depth:             1,
		BoundsCenter:      mgl32.Vec3{0, 10, 0},
		BoundsHalfExtents: mgl32.Vec3{3, 1, 3},
		MinCellSize:       1,
		Inset:             0,
	}
	colliders := []waterStaticCollider{
		{Min: mgl32.Vec3{-3, -1, -3}, Max: mgl32.Vec3{-2, 1, 3}},
		{Min: mgl32.Vec3{2, -1, -3}, Max: mgl32.Vec3{3, 1, 3}},
		{Min: mgl32.Vec3{-3, -1, -3}, Max: mgl32.Vec3{3, 1, -2}},
		{Min: mgl32.Vec3{-3, -1, 2}, Max: mgl32.Vec3{3, 1, 3}},
	}

	if rects := fitWaterBodyBoundsFromStaticColliders(body, colliders); len(rects) != 0 {
		t.Fatalf("expected surface outside authored Y bounds to fail, got %+v", rects)
	}

	body.BoundsCenter = mgl32.Vec3{0, 0, 0}
	body.BoundsHalfExtents = mgl32.Vec3{3, 0.01, 3}
	colliders = []waterStaticCollider{
		{Min: mgl32.Vec3{-3, 0.02, -3}, Max: mgl32.Vec3{-2, 0.04, 3}},
		{Min: mgl32.Vec3{2, 0.02, -3}, Max: mgl32.Vec3{3, 0.04, 3}},
		{Min: mgl32.Vec3{-3, 0.02, -3}, Max: mgl32.Vec3{3, 0.04, -2}},
		{Min: mgl32.Vec3{-3, 0.02, 2}, Max: mgl32.Vec3{3, 0.04, 3}},
	}

	if rects := fitWaterBodyBoundsFromStaticColliders(body, colliders); len(rects) != 0 {
		t.Fatalf("expected sources outside authored Y bounds to be ignored, got %+v", rects)
	}
}

func TestWaterBodyResolutionSourceTagFiltersStaticColliders(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := &WaterBodyResolutionState{}

	owner := cmd.AddEntity(
		&WaterBodyComponent{
			Mode:              WaterBodyModeFitBounds,
			SurfaceY:          0,
			Depth:             1,
			BoundsCenter:      mgl32.Vec3{4, 0, 0},
			BoundsHalfExtents: mgl32.Vec3{8, 1, 4},
			MinCellSize:       1,
			Inset:             0,
			SourceTag:         "pool-a",
		},
	)
	addTaggedWaterTestBasin := func(centerX float32, tag string) {
		cmd.AddEntity(
			&TransformComponent{Position: mgl32.Vec3{centerX - 3, 0, 0}},
			&RigidBodyComponent{BodyMode: BodyModeStatic},
			&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 1, 3}},
			&AuthoredLevelItemRefComponent{Tags: []string{tag}},
		)
		cmd.AddEntity(
			&TransformComponent{Position: mgl32.Vec3{centerX + 3, 0, 0}},
			&RigidBodyComponent{BodyMode: BodyModeStatic},
			&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 1, 3}},
			&AuthoredLevelItemRefComponent{Tags: []string{tag}},
		)
		cmd.AddEntity(
			&TransformComponent{Position: mgl32.Vec3{centerX, 0, -3}},
			&RigidBodyComponent{BodyMode: BodyModeStatic},
			&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 1, 0.5}},
			&AuthoredLevelItemRefComponent{Tags: []string{tag}},
		)
		cmd.AddEntity(
			&TransformComponent{Position: mgl32.Vec3{centerX, 0, 3}},
			&RigidBodyComponent{BodyMode: BodyModeStatic},
			&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 1, 0.5}},
			&AuthoredLevelItemRefComponent{Tags: []string{tag}},
		)
	}
	addTaggedWaterTestBasin(0, "pool-a")
	addTaggedWaterTestBasin(8, "pool-b")
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, nil, state)
	app.FlushCommands()

	record := state.ByEntity[owner]
	if record.Status != WaterBodyResolutionStatusResolved || record.PatchCount != 1 {
		t.Fatalf("expected source-tagged resolution to select one basin, got %+v", record)
	}
	MakeQuery1[ResolvedWaterPatchComponent](cmd).Map(func(_ EntityId, patch *ResolvedWaterPatchComponent) bool {
		if patch != nil && patch.Owner == owner && patch.Center.X() > 4 {
			t.Fatalf("expected pool-a patch, got center %+v", patch.Center)
		}
		return true
	})
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
		&RigidBodyComponent{BodyMode: BodyModeStatic},
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

func TestWaterBodyResolutionSystemUsesVoxelOccupancyWithoutRigidBody(t *testing.T) {
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
		&VoxelModelComponent{SharedGeometry: geomID, VoxelResolution: 1, PivotMode: PivotModeCorner},
	)
	app.FlushCommands()

	waterBodyResolutionSystem(cmd, assets, state)
	app.FlushCommands()

	record := state.ByEntity[owner]
	if record.Status != WaterBodyResolutionStatusResolved {
		t.Fatalf("expected no-rigid-body voxel occupancy fallback to resolve, got %q", record.Status)
	}
	if record.PrimarySource != WaterFitSourceVoxelOccupancy {
		t.Fatalf("expected voxel occupancy source, got %q", record.PrimarySource)
	}
	if record.PatchCount != 1 {
		t.Fatalf("expected one resolved patch, got %d", record.PatchCount)
	}
}

func TestWaterBodySourceInventoryHashTracksRepeatedDirtyBrickEdits(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	assets := &AssetServer{}
	body := &WaterBodyComponent{Mode: WaterBodyModeFitBounds}

	xbm := volume.NewXBrickMap()
	geomID := assets.RegisterSharedVoxelGeometry(xbm, "")
	geometry, ok := assets.GetVoxelGeometry(geomID)
	if !ok || geometry.XBrickMap == nil {
		t.Fatal("expected registered voxel geometry")
	}
	geometry.XBrickMap.ClearDirty()
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&VoxelModelComponent{SharedGeometry: geomID, VoxelResolution: 1, PivotMode: PivotModeCorner},
	)
	app.FlushCommands()

	initial := waterBodySourceInventoryHash(cmd, assets, body)
	geometry.XBrickMap.SetVoxel(0, 0, 0, 1)
	firstEdit := waterBodySourceInventoryHash(cmd, assets, body)
	geometry.XBrickMap.SetVoxel(1, 0, 0, 1)
	secondEdit := waterBodySourceInventoryHash(cmd, assets, body)

	if initial == firstEdit {
		t.Fatal("expected first voxel edit to change water source inventory hash")
	}
	if firstEdit == secondEdit {
		t.Fatal("expected repeated edit in dirty brick to change water source inventory hash")
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
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{6, 2, 0}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{0.5, 2, 4}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, -3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
		&ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{3.5, 2, 0.5}},
	)
	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{3, 2, 3}},
		&RigidBodyComponent{BodyMode: BodyModeStatic},
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
		&RigidBodyComponent{BodyMode: BodyModeStatic},
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
