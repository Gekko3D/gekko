package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func makeVoxelGridSnapshot(coords [][3]int, vSize float32) *voxelGridSnapshot {
	xbm := volume.NewXBrickMap()
	for _, coord := range coords {
		xbm.SetVoxel(coord[0], coord[1], coord[2], 1)
	}
	min, max := xbm.ComputeAABB()
	return &voxelGridSnapshot{
		xbm:       xbm,
		vSize:     vSize,
		cachedMin: min,
		cachedMax: max,
	}
}

func makeGridState(grid *voxelGridSnapshot) PhysicsEntityState {
	min := grid.GetAABBMin().Mul(grid.VoxelSize())
	max := grid.GetAABBMax().Mul(grid.VoxelSize())
	center := min.Add(max).Mul(0.5)
	return PhysicsEntityState{
		Eid:  42,
		Pos:  mgl32.Vec3{2, 3, 4},
		Rot:  mgl32.QuatIdent(),
		Mass: 1,
		Model: PhysicsModel{
			CenterOffset: center,
			Boxes: []CollisionBox{{
				HalfExtents: max.Sub(min).Mul(0.5),
			}},
			Grid: grid,
		},
	}
}

func mat3MaxDiff(a, b mgl32.Mat3) float32 {
	maxDiff := float32(0)
	for i := 0; i < 9; i++ {
		diff := absf(a[i] - b[i])
		if diff > maxDiff {
			maxDiff = diff
		}
	}
	return maxDiff
}

func TestSyncInternalBodyRecalculatesInertiaWhenVoxelGridChanges(t *testing.T) {
	initialGrid := makeVoxelGridSnapshot([][3]int{
		{0, 0, 0},
		{2, 0, 0},
		{0, 1, 1},
		{2, 1, 1},
	}, VoxelSize)
	var filledCoords [][3]int
	for z := 0; z < 2; z++ {
		for y := 0; y < 2; y++ {
			for x := 0; x < 3; x++ {
				filledCoords = append(filledCoords, [3]int{x, y, z})
			}
		}
	}
	editedGrid := makeVoxelGridSnapshot(filledCoords, VoxelSize)

	initialState := makeGridState(initialGrid)
	editedState := makeGridState(editedGrid)

	body := &internalBody{Eid: initialState.Eid}
	syncInternalBody(body, initialState, true)

	if !sameCollisionBoxes(body.boxes, editedState.Model.Boxes) {
		t.Fatal("expected regression test to keep the same AABB boxes across voxel edits")
	}
	if !physicsModelChanged(body, editedState.Model) {
		t.Fatal("expected voxel-grid edit to invalidate the cached physics model")
	}

	sentinel := mgl32.Mat3{
		11, 12, 13,
		21, 22, 23,
		31, 32, 33,
	}
	body.invInertiaLocal = sentinel

	syncInternalBody(body, editedState, false)
	if diff := mat3MaxDiff(sentinel, body.invInertiaLocal); diff <= 1e-4 {
		t.Fatalf("expected syncInternalBody to recalculate inertia after voxel edit, max diff from sentinel was %.6f", diff)
	}
}

func TestWakeBodyForContact_DoesNotResetAwakeRestingBody(t *testing.T) {
	body := &internalBody{
		sleeping: false,
		idleTime: 0.75,
	}

	wakeBodyForContact(body, false, true)

	if body.sleeping {
		t.Fatal("expected awake resting body to stay awake")
	}
	if body.idleTime != 0.75 {
		t.Fatalf("expected resting contact not to reset idle time, got %.2f", body.idleTime)
	}
}

func TestWakeBodyForContact_WakesSleepingBodyOnDeepPenetration(t *testing.T) {
	body := &internalBody{
		sleeping: true,
		idleTime: 0.75,
	}

	wakeBodyForContact(body, false, true)

	if body.sleeping {
		t.Fatal("expected deep penetration to wake a sleeping body")
	}
	if body.idleTime != 0 {
		t.Fatalf("expected wake to reset idle time, got %.2f", body.idleTime)
	}
}

func TestWakeBodyForContact_ResetsIdleTimeOnHighImpact(t *testing.T) {
	body := &internalBody{
		sleeping: false,
		idleTime: 0.75,
	}

	wakeBodyForContact(body, true, false)

	if body.sleeping {
		t.Fatal("expected high-impact wake on an awake body to keep it awake")
	}
	if body.idleTime != 0 {
		t.Fatalf("expected high-impact wake to reset idle time, got %.2f", body.idleTime)
	}
}
