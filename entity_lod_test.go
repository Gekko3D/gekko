package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestSelectEntityLODByDistanceUsesDeterministicBands(t *testing.T) {
	component := &EntityLODComponent{
		Bands: []EntityLODBand{
			{MaxDistance: 50, Representation: EntityLODRepresentationFullVoxel},
			{MaxDistance: 100, Representation: EntityLODRepresentationSimplifiedVoxel},
			{MaxDistance: 200, Representation: EntityLODRepresentationImpostor},
			{MaxDistance: 0, Representation: EntityLODRepresentationDot},
		},
	}

	cases := []struct {
		distance           float32
		wantBand           int
		wantRepresentation EntityLODRepresentation
	}{
		{distance: 0, wantBand: 0, wantRepresentation: EntityLODRepresentationFullVoxel},
		{distance: 50, wantBand: 0, wantRepresentation: EntityLODRepresentationFullVoxel},
		{distance: 50.01, wantBand: 1, wantRepresentation: EntityLODRepresentationSimplifiedVoxel},
		{distance: 100, wantBand: 1, wantRepresentation: EntityLODRepresentationSimplifiedVoxel},
		{distance: 100.01, wantBand: 2, wantRepresentation: EntityLODRepresentationImpostor},
		{distance: 200, wantBand: 2, wantRepresentation: EntityLODRepresentationImpostor},
		{distance: 200.01, wantBand: 3, wantRepresentation: EntityLODRepresentationDot},
	}

	for _, tc := range cases {
		got, err := SelectEntityLODByDistance(component, tc.distance)
		if err != nil {
			t.Fatalf("SelectEntityLODByDistance(%v) returned error: %v", tc.distance, err)
		}
		if got.BandIndex != tc.wantBand {
			t.Fatalf("distance %v: expected band %d, got %d", tc.distance, tc.wantBand, got.BandIndex)
		}
		if got.Representation != tc.wantRepresentation {
			t.Fatalf("distance %v: expected representation %v, got %v", tc.distance, tc.wantRepresentation, got.Representation)
		}
	}
}

func TestEntityLODValidateRejectsInvalidBandLayouts(t *testing.T) {
	cases := []EntityLODComponent{
		{},
		{
			Bands: []EntityLODBand{
				{MaxDistance: 100, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 90, Representation: EntityLODRepresentationSimplifiedVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationDot},
			},
		},
		{
			Bands: []EntityLODBand{
				{MaxDistance: 100, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 200, Representation: EntityLODRepresentationSimplifiedVoxel},
			},
		},
		{
			Bands: []EntityLODBand{
				{MaxDistance: 0, Representation: EntityLODRepresentationFullVoxel},
				{MaxDistance: 0, Representation: EntityLODRepresentationDot},
			},
		},
	}

	for i := range cases {
		if err := cases[i].Validate(); err == nil {
			t.Fatalf("expected invalid entity LOD case %d to fail validation", i)
		}
	}
}

func TestSelectEntityLODUsesTransformPosition(t *testing.T) {
	component := &EntityLODComponent{
		Bands: []EntityLODBand{
			{MaxDistance: 10, Representation: EntityLODRepresentationFullVoxel},
			{MaxDistance: 0, Representation: EntityLODRepresentationDot},
		},
	}
	transform := &TransformComponent{
		Position: mgl32.Vec3{0, 0, -12},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}

	got, err := SelectEntityLOD(mgl32.Vec3{0, 0, 0}, transform, component)
	if err != nil {
		t.Fatalf("SelectEntityLOD returned error: %v", err)
	}
	if got.BandIndex != 1 {
		t.Fatalf("expected far band index 1, got %d", got.BandIndex)
	}
	if got.Representation != EntityLODRepresentationDot {
		t.Fatalf("expected dot representation, got %v", got.Representation)
	}
}
