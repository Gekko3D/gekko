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
	if body.DirectLightOcclusion == nil || *body.DirectLightOcclusion != 1 {
		t.Fatalf("direct light occlusion = %v, want 1", body.DirectLightOcclusion)
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

func TestBuildHL1WaterBodiesFromTopCellsTilesExactFootprint(t *testing.T) {
	bodies := buildHL1WaterBodiesFromTopCells([]LiquidTopCell{
		{Kind: "water", SurfaceY: 2, Depth: 1, X: 0, Z: 0},
		{Kind: "water", SurfaceY: 2, Depth: 1, X: 1, Z: 0},
		{Kind: "water", SurfaceY: 2, Depth: 1, X: 0, Z: 1},
	}, 1)
	if len(bodies) != 2 {
		t.Fatalf("expected L-shaped occupancy to tile into two rectangles, got %+v", bodies)
	}
	area := float32(0)
	for _, body := range bodies {
		if body.SurfaceMode != content.LevelWaterSurfaceModeFootprint || body.ContinuityGroup == "" {
			t.Fatalf("expected grouped footprint water body, got %+v", body)
		}
		area += body.RectHalfExtents[0] * body.RectHalfExtents[1] * 4
	}
	if area != 3 {
		t.Fatalf("tiled water area = %v, want 3", area)
	}
	if bodies[0].ContinuityGroup != bodies[1].ContinuityGroup {
		t.Fatalf("tiled patches must share continuity group: %+v", bodies)
	}
}

func TestCollectLiquidTopCellsHidesSealedCeiling(t *testing.T) {
	faces := []Face{
		{
			TextureName: "!WATERBLUE", Normal: vec3(0, 0, 1),
			Vertices: []importcommon.Vec3{vec3(0, 0, 64), vec3(64, 0, 64), vec3(64, 64, 64), vec3(0, 64, 64)},
		},
		{
			TextureName: "CONCRETE", Normal: vec3(0, 0, -1),
			Vertices: []importcommon.Vec3{vec3(0, 0, 64), vec3(0, 64, 64), vec3(64, 64, 64), vec3(64, 0, 64)},
		},
	}
	cells := collectLiquidTopCells(nil, faces, VoxelizeOptions{VoxelResolution: 0.1})
	if len(cells) == 0 {
		t.Fatal("expected liquid top cells")
	}
	for _, cell := range cells {
		if !cell.SurfaceHidden {
			t.Fatalf("sealed liquid cell remained visible: %+v", cell)
		}
	}
	bodies := buildHL1WaterBodiesFromTopCells(cells, 0.1)
	if len(bodies) == 0 || bodies[0].SurfaceVisibility != content.LevelWaterSurfaceVisibilityHidden {
		t.Fatalf("sealed liquid bodies = %+v", bodies)
	}
}

func TestCollectLiquidTopCellsHidesWorldSealedVolume(t *testing.T) {
	bsp := &BSP{
		Models: []Model{{HeadNodes: [4]int32{-1}}},
		Leafs:  []Leaf{{Contents: ContentsSolid}},
	}
	face := Face{
		TextureName: "!WATERBLUE", Normal: vec3(0, 0, 1),
		Vertices: []importcommon.Vec3{vec3(0, 0, 64), vec3(64, 0, 64), vec3(64, 64, 64), vec3(0, 64, 64)},
	}
	for _, cell := range collectLiquidTopCells(bsp, []Face{face}, VoxelizeOptions{VoxelResolution: 0.1}) {
		if !cell.SurfaceHidden {
			t.Fatalf("BSP-sealed liquid cell remained visible: %+v", cell)
		}
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

func TestBuildHL1WaterBodyDefsAssignsContinuityGroupsForCoplanarConnectedRects(t *testing.T) {
	bodies := buildHL1WaterBodyDefs([]hl1WaterRect{
		{Kind: "water", SurfaceY: 2, Depth: 1, MinX: 0, MaxX: 2, MinZ: 0, MaxZ: 2},
		{Kind: "water", SurfaceY: 2, Depth: 3, MinX: 2, MaxX: 4, MinZ: 0, MaxZ: 2},
		{Kind: "water", SurfaceY: 2.5, Depth: 1, MinX: 4, MaxX: 6, MinZ: 0, MaxZ: 2},
		{Kind: "slime", SurfaceY: 2, Depth: 1, MinX: 6, MaxX: 8, MinZ: 0, MaxZ: 2},
	})
	if len(bodies) != 4 {
		t.Fatalf("water bodies = %d", len(bodies))
	}
	if bodies[0].ContinuityGroup == "" || bodies[0].ContinuityGroup != bodies[1].ContinuityGroup {
		t.Fatalf("expected first two coplanar water rects to share group, got %q and %q", bodies[0].ContinuityGroup, bodies[1].ContinuityGroup)
	}
	if bodies[1].VolumeGroup == "" || bodies[1].VolumeGroup != bodies[2].VolumeGroup {
		t.Fatalf("expected different-height touching volumes to share medium: %+v", bodies)
	}
	if bodies[2].ContinuityGroup != "" || bodies[3].ContinuityGroup != "" {
		t.Fatalf("expected height/kind mismatches to stay ungrouped, got %q and %q", bodies[2].ContinuityGroup, bodies[3].ContinuityGroup)
	}
	for _, body := range bodies {
		if body.SurfaceMode != content.LevelWaterSurfaceModeFootprint {
			t.Fatalf("expected HL1 water to use footprint surface mode, got %+v", body)
		}
	}
}
