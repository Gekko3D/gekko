package hl1

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestMaterialKindTreatsBangPrefixAsWater(t *testing.T) {
	if kind := materialKind("!c2a54b"); kind != "water" {
		t.Fatalf("kind = %q, want water", kind)
	}
}

func TestBuildHL1WaterBodiesExtractsHorizontalTopFace(t *testing.T) {
	faces := []Face{
		{
			TextureName: "!WATERBLUE",
			Normal:      vec3(0, 0, 1),
			Vertices: []importcommon.Vec3{
				vec3(0, 0, 64),
				vec3(128, 0, 64),
				vec3(128, 128, 64),
				vec3(0, 128, 64),
			},
		},
		{
			TextureName: "!WATERBLUE",
			Normal:      vec3(0, 0, -1),
			Vertices: []importcommon.Vec3{
				vec3(0, 0, 0),
				vec3(0, 128, 0),
				vec3(128, 128, 0),
				vec3(128, 0, 0),
			},
		},
	}
	bodies := buildHL1WaterBodies(nil, faces, 0.1)
	if len(bodies) != 1 {
		t.Fatalf("water bodies = %d, got %+v", len(bodies), bodies)
	}
	body := bodies[0]
	if body.Mode != content.LevelWaterBodyModeExplicitRect {
		t.Fatalf("mode = %q", body.Mode)
	}
	if body.RectHalfExtents[0] < 1.62 || body.RectHalfExtents[0] > 1.63 ||
		body.RectHalfExtents[1] < 1.62 || body.RectHalfExtents[1] > 1.63 {
		t.Fatalf("rect half extents = %+v", body.RectHalfExtents)
	}
	if body.SurfaceY < 1.62 || body.SurfaceY > 1.63 {
		t.Fatalf("surface y = %f", body.SurfaceY)
	}
	if body.Depth < 1.62 || body.Depth > 1.63 {
		t.Fatalf("depth = %f", body.Depth)
	}
	if body.Transform.Position[0] < 1.62 || body.Transform.Position[0] > 1.63 ||
		body.Transform.Position[2] > -1.62 || body.Transform.Position[2] < -1.63 {
		t.Fatalf("transform position = %+v", body.Transform.Position)
	}
}

func TestBuildHL1WaterBodiesMergesAdjacentRects(t *testing.T) {
	faces := []Face{
		{
			TextureName: "!WATERBLUE",
			Normal:      vec3(0, 0, 1),
			Vertices: []importcommon.Vec3{
				vec3(0, 0, 64),
				vec3(64, 0, 64),
				vec3(64, 64, 64),
				vec3(0, 64, 64),
			},
		},
		{
			TextureName: "!WATERBLUE",
			Normal:      vec3(0, 0, 1),
			Vertices: []importcommon.Vec3{
				vec3(64, 0, 64),
				vec3(128, 0, 64),
				vec3(128, 64, 64),
				vec3(64, 64, 64),
			},
		},
	}
	bodies := buildHL1WaterBodies(nil, faces, 0.1)
	if len(bodies) != 1 {
		t.Fatalf("water bodies = %d, got %+v", len(bodies), bodies)
	}
	if bodies[0].RectHalfExtents[0] < 1.62 || bodies[0].RectHalfExtents[0] > 1.63 {
		t.Fatalf("merged x half extent = %+v", bodies[0].RectHalfExtents)
	}
}

func TestBuildHL1WaterBodiesPrefersLiquidLeafVolume(t *testing.T) {
	bsp := &BSP{
		Leafs: []Leaf{
			{Contents: ContentsWater, Min: [3]int16{0, 0, 0}, Max: [3]int16{128, 128, 32}},
			{Contents: ContentsWater, Min: [3]int16{0, 0, 32}, Max: [3]int16{128, 128, 64}},
		},
	}
	faces := []Face{{
		TextureName: "!WATERBLUE",
		Normal:      vec3(0, 0, 1),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 64),
			vec3(32, 0, 64),
			vec3(32, 32, 64),
			vec3(0, 32, 64),
		},
	}}
	bodies := buildHL1WaterBodies(bsp, faces, 0.1)
	if len(bodies) != 1 {
		t.Fatalf("water bodies = %d, got %+v", len(bodies), bodies)
	}
	body := bodies[0]
	if body.RectHalfExtents[0] < 1.62 || body.RectHalfExtents[0] > 1.63 ||
		body.RectHalfExtents[1] < 1.62 || body.RectHalfExtents[1] > 1.63 {
		t.Fatalf("leaf rect half extents = %+v", body.RectHalfExtents)
	}
	if body.Depth < 1.62 || body.Depth > 1.63 {
		t.Fatalf("leaf depth = %f", body.Depth)
	}
}

func TestBuildHL1WaterBodiesCollapsesConnectedLeafVolume(t *testing.T) {
	bsp := &BSP{
		Leafs: []Leaf{
			{Contents: ContentsWater, Min: [3]int16{0, 0, 0}, Max: [3]int16{64, 64, 64}},
			{Contents: ContentsWater, Min: [3]int16{64, 0, 0}, Max: [3]int16{128, 64, 64}},
			{Contents: ContentsWater, Min: [3]int16{0, 64, 0}, Max: [3]int16{64, 128, 64}},
		},
	}
	bodies := buildHL1WaterBodies(bsp, nil, 0.1)
	if len(bodies) != 1 {
		t.Fatalf("water bodies = %d, got %+v", len(bodies), bodies)
	}
	body := bodies[0]
	if body.RectHalfExtents[0] < 1.62 || body.RectHalfExtents[0] > 1.63 ||
		body.RectHalfExtents[1] < 1.62 || body.RectHalfExtents[1] > 1.63 {
		t.Fatalf("connected volume half extents = %+v", body.RectHalfExtents)
	}
}
