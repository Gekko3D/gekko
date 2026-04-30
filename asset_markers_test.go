package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestFindAuthoredAssetMarkerByNameReturnsMarkerLookupWithTransforms(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	def := representativeAuthoredAssetForTest()
	result, err := SpawnAuthoredAsset(cmd, nil, def, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		t.Fatalf("SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()

	lookup, ok := FindAuthoredAssetMarkerByName(cmd, result.RootEntity, "marker")
	if !ok {
		t.Fatal("expected marker lookup by name")
	}
	if lookup.Entity != result.EntitiesByAssetID["marker"] {
		t.Fatalf("expected marker entity %d, got %d", result.EntitiesByAssetID["marker"], lookup.Entity)
	}
	if lookup.Marker.Name != "marker" {
		t.Fatalf("expected marker name to round-trip, got %q", lookup.Marker.Name)
	}
	if lookup.Marker.Kind != "socket" {
		t.Fatalf("expected marker kind socket, got %q", lookup.Marker.Kind)
	}
	if lookup.LocalTransform.Position != (mgl32.Vec3{1, 0, 1}) {
		t.Fatalf("expected local marker position [1 0 1], got %v", lookup.LocalTransform.Position)
	}
	if lookup.Transform.Position != (mgl32.Vec3{2, 2, 1}) {
		t.Fatalf("expected world marker position [2 2 1], got %v", lookup.Transform.Position)
	}
}

func TestFindAuthoredAssetMarkersByKindScopesToRequestedRoot(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()

	left, err := SpawnAuthoredAsset(cmd, nil, representativeAuthoredAssetForTest(), TransformComponent{
		Position: mgl32.Vec3{0, 0, 0},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("left SpawnAuthoredAsset returned error: %v", err)
	}
	right, err := SpawnAuthoredAsset(cmd, nil, representativeAuthoredAssetForTest(), TransformComponent{
		Position: mgl32.Vec3{100, 0, 0},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	})
	if err != nil {
		t.Fatalf("right SpawnAuthoredAsset returned error: %v", err)
	}
	app.FlushCommands()

	leftMarkers := FindAuthoredAssetMarkersByKind(cmd, left.RootEntity, "socket")
	if len(leftMarkers) != 1 {
		t.Fatalf("expected 1 marker under left root, got %d", len(leftMarkers))
	}
	if leftMarkers[0].Entity != left.EntitiesByAssetID["marker"] {
		t.Fatalf("expected left marker entity %d, got %d", left.EntitiesByAssetID["marker"], leftMarkers[0].Entity)
	}
	if leftMarkers[0].Transform.Position != (mgl32.Vec3{2, 2, 1}) {
		t.Fatalf("expected left marker world position [2 2 1], got %v", leftMarkers[0].Transform.Position)
	}

	rightMarker, ok := FindFirstAuthoredAssetMarkerByKind(cmd, right.RootEntity, "socket")
	if !ok {
		t.Fatal("expected first marker by kind under right root")
	}
	if rightMarker.Entity != right.EntitiesByAssetID["marker"] {
		t.Fatalf("expected right marker entity %d, got %d", right.EntitiesByAssetID["marker"], rightMarker.Entity)
	}
	if rightMarker.Transform.Position != (mgl32.Vec3{102, 2, 1}) {
		t.Fatalf("expected right marker world position [102 2 1], got %v", rightMarker.Transform.Position)
	}
}
