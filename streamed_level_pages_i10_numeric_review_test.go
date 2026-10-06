package gekko

import (
	"math"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func TestI10PageDecimalCellEnvelopeIsConservative(t *testing.T) {
	poi, leaves := i10Branches(mgl32.Vec3{})
	edge := math.Nextafter32(float32(.3), float32(math.Inf(-1)))
	for i := range poi.Pages {
		poi.Pages[i].BoundsMin[0] = 0
		poi.Pages[i].BoundsMax[0] = edge
		poi.Pages[i].Payload.VoxelResolution = .1
	}
	if _, err := content.BuildLevelStreamingIndex(&content.LevelDef{}, nil, poi); err != nil {
		t.Fatalf("valid decimal coverage fixture: %v", err)
	}
	cellSize, desired := float32(.1), float32(2.5e-8)
	position := float32(.3)
	cell := math.Floor(float64(position) / float64(cellSize))
	exactGap := cell*float64(cellSize) - float64(edge)
	roundedGap := float64(float32(cell*float64(cellSize))) - float64(edge)
	if !(exactGap >= 0 && exactGap <= float64(desired) && roundedGap > float64(desired)) {
		t.Fatalf("fixture does not distinguish conservative bounds: exact=%g rounded=%g desired=%g", exactGap, roundedGap, desired)
	}
	distances := StreamedPageDistances{Desired: desired, Keep: desired, Prefetch: desired}
	profile := StreamedPageProfile{Macro: distances, Regional: distances, FullPOI: distances, SelectionCellSize: cellSize}
	state := i10Configure(t, nil, poi, profile)
	selected := i10Select(t, state, i10Observer(1, position, 0, 0))
	i10Want(t, selected.Desired, StreamedPagePOI, leaves[0], true)
}

func TestI10PageForwardPrefetchIncludesExactProfileBoundary(t *testing.T) {
	poi, leaves := i10Branches(mgl32.Vec3{1, 0, 0})
	if _, err := content.BuildLevelStreamingIndex(&content.LevelDef{}, nil, poi); err != nil {
		t.Fatal(err)
	}
	distances := StreamedPageDistances{Desired: .1, Keep: .2, Prefetch: 1}
	profile := StreamedPageProfile{Macro: distances, Regional: distances, FullPOI: distances, SelectionCellSize: .1}
	observer := i10Observer(1, -.05, .05, .05)
	observer.Velocity = mgl32.Vec3{1, 0, 0}
	// The negative cell ends at zero. The exact difference of stored profile
	// fields plus Desired reaches x=1; f32 subtraction alone falls short.
	exactExtra := float64(distances.Prefetch) - float64(distances.Desired)
	if exactExtra+float64(distances.Desired) != 1 || float64(distances.Prefetch-distances.Desired)+float64(distances.Desired) >= 1 {
		t.Fatal("fixture lost the exact forward boundary distinction")
	}
	selected := i10Select(t, i10Configure(t, nil, poi, profile), observer)
	i10Want(t, selected.Prefetch, StreamedPagePOI, leaves[0], true)
	i10Want(t, selected.Desired, StreamedPagePOI, leaves[0], false)
}
