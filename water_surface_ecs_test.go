package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestWaterSurfaceComponentNormalizationAndEnablement(t *testing.T) {
	var nilWater *WaterSurfaceComponent
	if nilWater.Enabled() {
		t.Fatal("expected nil water surface to be disabled")
	}

	water := &WaterSurfaceComponent{
		HalfExtents: [2]float32{-1, 3},
		Depth:       -2,
	}
	if water.Enabled() {
		t.Fatal("expected invalid extents/depth to disable water")
	}

	water.HalfExtents = [2]float32{4, 6}
	water.Depth = 2
	if !water.Enabled() {
		t.Fatal("expected valid bounds to enable water")
	}
	if got := water.NormalizedOpacity(); got <= 0 || got >= 1 {
		t.Fatalf("expected normalized opacity in (0,1), got %f", got)
	}
	if got := water.NormalizedFlowDirection(); got != ([2]float32{1, 0}) {
		t.Fatalf("expected default flow direction, got %v", got)
	}
}

func TestWaterSurfaceWorldScalingUsesTransformScale(t *testing.T) {
	water := &WaterSurfaceComponent{
		HalfExtents: [2]float32{3, 5},
		Depth:       2,
	}
	tr := &TransformComponent{
		Position: mgl32.Vec3{1, 2, 3},
		Scale:    mgl32.Vec3{2, 3, 4},
	}

	if got := water.WorldCenter(tr); got != tr.Position {
		t.Fatalf("expected world center %v, got %v", tr.Position, got)
	}
	if got := water.WorldHalfExtents(tr); got != ([2]float32{6, 20}) {
		t.Fatalf("expected scaled half extents [6 20], got %v", got)
	}
	if got := water.WorldDepth(tr); got != 6 {
		t.Fatalf("expected scaled depth 6, got %f", got)
	}
}
