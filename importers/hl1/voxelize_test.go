package hl1

import (
	"testing"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestVoxelizeFacesCPUSquareSurface(t *testing.T) {
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(0, 1, 0),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 0, 16),
			vec3(0, 0, 16),
		},
	}
	result := VoxelizeFacesCPU([]Face{face}, VoxelizeOptions{VoxelResolution: 0.1})
	if result.SurfaceCount != 25 {
		t.Fatalf("surface count = %d, want 25; voxels=%+v", result.SurfaceCount, result.Voxels)
	}
	if result.FilledCount != 0 {
		t.Fatalf("filled count = %d", result.FilledCount)
	}
	if result.BoundsMin != [3]int{0, 0, 0} || result.BoundsMax != [3]int{4, 4, 0} {
		t.Fatalf("bounds = %+v..%+v", result.BoundsMin, result.BoundsMax)
	}
	for _, voxel := range result.Voxels {
		if voxel.Palette != 1 || voxel.MaterialID != 1 || voxel.SolidKind != "structural" {
			t.Fatalf("bad voxel metadata: %+v", voxel)
		}
	}
}

func TestRasterizeFaceSurfaceKeysConservativelyFollowsInclinedTriangle(t *testing.T) {
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(0, 1, 1),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(40, 0, 0),
			vec3(40, 40, 40),
		},
	}
	keys := rasterizeFaceSurfaceKeys(face, VoxelizeOptions{VoxelResolution: 0.5})
	if len(keys) == 0 {
		t.Fatalf("inclined face produced no voxels")
	}
	distinctX := map[int]struct{}{}
	distinctY := map[int]struct{}{}
	distinctZ := map[int]struct{}{}
	for _, key := range keys {
		distinctX[key[0]] = struct{}{}
		distinctY[key[1]] = struct{}{}
		distinctZ[key[2]] = struct{}{}
	}
	if len(distinctX) < 2 || len(distinctY) < 2 || len(distinctZ) < 2 {
		t.Fatalf("inclined face collapsed to axis sheet: keys=%+v", keys)
	}
}

func TestVoxelizeFacesCPUSkipsSkyFaces(t *testing.T) {
	face := Face{
		TextureID:   0,
		TextureName: "SKY",
		Normal:      vec3(0, 1, 0),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 0, 16),
			vec3(0, 0, 16),
		},
	}
	result := VoxelizeFacesCPU([]Face{face}, VoxelizeOptions{VoxelResolution: 0.1})
	if result.SurfaceCount != 0 || len(result.Voxels) != 0 {
		t.Fatalf("sky face produced voxels: surface=%d voxels=%+v", result.SurfaceCount, result.Voxels)
	}
}

func TestVoxelizeFacesCPUSkipsLiquidFaces(t *testing.T) {
	face := Face{
		TextureID:   0,
		TextureName: "!WATERBLUE",
		Normal:      vec3(0, 0, 1),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 16, 0),
			vec3(0, 16, 0),
		},
	}
	result := VoxelizeFacesCPU([]Face{face}, VoxelizeOptions{VoxelResolution: 0.1})
	if result.SurfaceCount != 0 || len(result.Voxels) != 0 {
		t.Fatalf("liquid face produced voxels: surface=%d voxels=%+v", result.SurfaceCount, result.Voxels)
	}
}

func TestVoxelizeFacesCPUBakesTextureSampleIntoPalette(t *testing.T) {
	texture := TexturePixels{
		Name:   "TESTWALL",
		Width:  1,
		Height: 1,
		Pixels: []byte{0},
		Colors: [][3]uint8{{250, 10, 10}},
	}
	store := &TextureStore{byName: map[string]TexturePixels{"testwall": texture}}
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(0, 0, 1),
		TexInfo: TexInfo{
			S: TextureAxis{Axis: vec3(1, 0, 0)},
			T: TextureAxis{Axis: vec3(0, 1, 0)},
		},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 16, 0),
			vec3(0, 16, 0),
		},
	}
	result := VoxelizeFacesCPU([]Face{face}, VoxelizeOptions{VoxelResolution: 0.1, TextureStore: store})
	if len(result.Voxels) == 0 {
		t.Fatal("no voxels")
	}
	want := uint8(bakedPaletteIndex([4]uint8{250, 10, 10, 255}))
	for _, voxel := range result.Voxels {
		if voxel.Palette != want || voxel.MaterialID != int(want) {
			t.Fatalf("voxel baked palette = %+v, want %d", voxel, want)
		}
	}
	if len(result.Materials) != 252 {
		t.Fatalf("baked palette materials = %d", len(result.Materials))
	}
}

func TestVoxelizeFacesCPUSkipsCutoutTextureTransparentTexels(t *testing.T) {
	texture := TexturePixels{
		Name:   "{LADDER",
		Width:  1,
		Height: 1,
		Pixels: []byte{255},
		Colors: make([][3]uint8, 256),
	}
	texture.Colors[255] = [3]uint8{0, 0, 255}
	store := &TextureStore{byName: map[string]TexturePixels{"{ladder": texture}}
	face := Face{
		TextureID:   0,
		TextureName: "{LADDER",
		Normal:      vec3(0, 0, 1),
		TexInfo: TexInfo{
			S: TextureAxis{Axis: vec3(1, 0, 0)},
			T: TextureAxis{Axis: vec3(0, 1, 0)},
		},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 16, 0),
			vec3(0, 16, 0),
		},
	}
	result := VoxelizeFacesCPU([]Face{face}, VoxelizeOptions{VoxelResolution: 0.1, TextureStore: store})
	if len(result.Voxels) != 0 {
		t.Fatalf("cutout transparent texels produced voxels: %+v", result.Voxels[:min(len(result.Voxels), 5)])
	}
}

func TestVoxelizeFacesCPUKeepsCutoutVoxelWhenFootprintTouchesOpaqueTexel(t *testing.T) {
	texture := TexturePixels{
		Name:   "{LADDER",
		Width:  4,
		Height: 1,
		Pixels: []byte{1, 255, 255, 255},
		Colors: make([][3]uint8, 256),
	}
	texture.Colors[1] = [3]uint8{120, 80, 40}
	texture.Colors[255] = [3]uint8{0, 0, 255}
	store := &TextureStore{byName: map[string]TexturePixels{"{ladder": texture}}
	face := Face{
		TextureID:   0,
		TextureName: "{LADDER",
		Normal:      vec3(0, 0, 1),
		TexInfo: TexInfo{
			S: TextureAxis{Axis: vec3(1, 0, 0)},
			T: TextureAxis{Axis: vec3(0, 1, 0)},
		},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 16, 0),
			vec3(0, 16, 0),
		},
	}
	result := VoxelizeFacesCPU([]Face{face}, VoxelizeOptions{VoxelResolution: 0.1, TextureStore: store})
	if len(result.Voxels) == 0 {
		t.Fatal("cutout footprint touching opaque texel produced no voxels")
	}
}

func TestPropagateStructuralFillMaterialsUsesNearestSurface(t *testing.T) {
	surface := map[[3]int]importcommon.Voxel{
		{0, 0, 0}: {X: 0, Y: 0, Z: 0, MaterialID: 2, SolidKind: "structural"},
		{4, 0, 0}: {X: 4, Y: 0, Z: 0, MaterialID: 7, SolidKind: "structural"},
	}
	candidates := map[[3]int]struct{}{
		{1, 0, 0}: {},
		{2, 0, 0}: {},
		{3, 0, 0}: {},
	}
	materials := propagateStructuralFillMaterials(surface, candidates, 1)
	if materials[[3]int{1, 0, 0}] != 2 {
		t.Fatalf("near left material = %d, want 2", materials[[3]int{1, 0, 0}])
	}
	if materials[[3]int{3, 0, 0}] != 7 {
		t.Fatalf("near right material = %d, want 7", materials[[3]int{3, 0, 0}])
	}
}

func TestVoxelizeBSPSolidCPUCarvesLiquidContents(t *testing.T) {
	bsp := &BSP{
		Leafs: []Leaf{{Contents: ContentsWater}},
		Models: []Model{{
			Min:       vec3(-32, -32, -32),
			Max:       vec3(32, 32, 32),
			HeadNodes: [4]int32{-1, -1, -1, -1},
		}},
	}
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(0, 0, 1),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 16, 0),
			vec3(0, 16, 0),
		},
	}
	result, err := VoxelizeBSPSolidCPU(bsp, []Face{face}, nil, VoxelizeOptions{
		VoxelResolution:     0.1,
		MaxSolidSampleCells: 1000,
		SolidBandDepth:      1,
	})
	if err != nil {
		t.Fatalf("VoxelizeBSPSolidCPU failed: %v", err)
	}
	if len(result.Voxels) != 0 || result.SurfaceCount != 0 {
		t.Fatalf("liquid carve left voxels: surface=%d voxels=%+v", result.SurfaceCount, result.Voxels)
	}
}

func TestVoxelizeBSPSolidCPUClassifiesAndFloodsPlayableEmpty(t *testing.T) {
	bsp := &BSP{
		Planes: []Plane{{Normal: vec3(1, 0, 0), Dist: 0}},
		Nodes: []Node{{
			PlaneID:  0,
			Children: [2]int16{-2, -1},
		}},
		Leafs: []Leaf{
			{Contents: ContentsEmpty},
			{Contents: ContentsSolid},
		},
		Models: []Model{{
			Min:       vec3(-40, -40, -40),
			Max:       vec3(40, 40, 40),
			HeadNodes: [4]int32{0, -1, -1, -1},
		}},
	}
	entities := []importcommon.Entity{{
		ClassName:     "info_player_start",
		WorldPosition: HammerToGekko(vec3(-20, 0, 0)),
	}}
	result, err := VoxelizeBSPSolidCPU(bsp, nil, entities, VoxelizeOptions{
		VoxelResolution:     1,
		MaxSolidSampleCells: 1000,
		SolidBandDepth:      2,
	})
	if err != nil {
		t.Fatalf("VoxelizeBSPSolidCPU failed: %v", err)
	}
	if result.PlayableEmptyCount != 256 {
		t.Fatalf("playable empty = %d", result.PlayableEmptyCount)
	}
	if result.SolidCount != 128 || result.FilledCount != 128 || len(result.Voxels) != 128 {
		t.Fatalf("solid/fill/voxels = %d/%d/%d", result.SolidCount, result.FilledCount, len(result.Voxels))
	}
	if result.EmptyCount != 256 || result.UnreachableEmptyCount != 0 {
		t.Fatalf("empty/playable/unreachable = %d/%d/%d", result.EmptyCount, result.PlayableEmptyCount, result.UnreachableEmptyCount)
	}
	for _, voxel := range result.Voxels {
		if voxel.X < 0 || voxel.X > 1 {
			t.Fatalf("voxel outside solid band: %+v", voxel)
		}
		if voxel.SolidKind != "structural_fill" {
			t.Fatalf("solid kind = %q", voxel.SolidKind)
		}
	}
}

func TestVoxelizeBSPSolidCPUDoesNotRequireFullGridWithoutSeeds(t *testing.T) {
	bsp := &BSP{
		Planes: []Plane{{Normal: vec3(1, 0, 0), Dist: 0}},
		Nodes:  []Node{{PlaneID: 0, Children: [2]int16{-1, -1}}},
		Leafs:  []Leaf{{Contents: ContentsSolid}},
		Models: []Model{{
			Min:       vec3(-40, -40, -40),
			Max:       vec3(40, 40, 40),
			HeadNodes: [4]int32{0, -1, -1, -1},
		}},
	}
	_, err := VoxelizeBSPSolidCPU(bsp, nil, nil, VoxelizeOptions{
		VoxelResolution:     1,
		MaxSolidSampleCells: 10,
	})
	if err != nil {
		t.Fatalf("VoxelizeBSPSolidCPU failed without playable seeds: %v", err)
	}
}

func TestVoxelizeBSPSolidCPURejectsSurfaceBandOverCap(t *testing.T) {
	bsp := &BSP{
		Leafs: []Leaf{{Contents: ContentsSolid}},
		Models: []Model{{
			Min:       vec3(-1000, -1000, -1000),
			Max:       vec3(1000, 1000, 1000),
			HeadNodes: [4]int32{-1, -1, -1, -1},
		}},
	}
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(1, 0, 0),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(0, 80, 0),
			vec3(0, 80, 80),
			vec3(0, 0, 80),
		},
	}
	_, err := VoxelizeBSPSolidCPU(bsp, []Face{face}, nil, VoxelizeOptions{
		VoxelResolution:     1,
		MaxSolidSampleCells: 1,
		SolidBandDepth:      2,
	})
	if err == nil {
		t.Fatalf("VoxelizeBSPSolidCPU accepted surface band over cap")
	}
}

func TestSolidBandCandidatesFromSurfaceFacesFloodsAdjacentBSPSolid(t *testing.T) {
	bsp := &BSP{
		Planes: []Plane{{Normal: vec3(1, 0, 0), Dist: 50}},
		Nodes: []Node{{
			PlaneID:  0,
			Children: [2]int16{-2, -1},
		}},
		Leafs: []Leaf{
			{Contents: ContentsEmpty},
			{Contents: ContentsSolid},
		},
		Models: []Model{{
			Min:       vec3(-100, -100, -100),
			Max:       vec3(100, 100, 100),
			HeadNodes: [4]int32{0, -1, -1, -1},
		}},
	}
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(0, 0, 1),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(0, 40, 0),
			vec3(0, 40, 40),
			vec3(0, 0, 40),
		},
	}
	candidates, err := solidBandCandidatesFromSurfaceFaces(
		bsp,
		[]Face{face},
		[3]int{-2, -3, -3},
		[3]int{4, 4, 4},
		VoxelizeOptions{
			VoxelResolution:     1,
			MaxSolidSampleCells: 1000,
			SolidBandDepth:      1,
		},
	)
	if err != nil {
		t.Fatalf("solidBandCandidatesFromSurfaceFaces failed: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatalf("surface fallback found no BSP-solid neighbors")
	}
	foundPositiveX := false
	for key := range candidates {
		if key[0] > 0 {
			foundPositiveX = true
		}
		if key[0] <= 0 {
			t.Fatalf("candidate outside BSP-solid side: %+v", key)
		}
	}
	if !foundPositiveX {
		t.Fatalf("surface fallback did not flood into +X BSP solid: %+v", candidates)
	}
}

func TestVoxelizeBSPSolidCPUUsesNonSkyFaceBounds(t *testing.T) {
	bsp := &BSP{
		Leafs: []Leaf{{Contents: ContentsSolid}},
		Models: []Model{{
			Min:       vec3(-1000, -1000, -1000),
			Max:       vec3(1000, 1000, 1000),
			HeadNodes: [4]int32{-1, -1, -1, -1},
		}},
	}
	face := Face{
		TextureID:   0,
		TextureName: "TESTWALL",
		Normal:      vec3(0, 1, 0),
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(80, 0, 0),
			vec3(80, 0, 80),
			vec3(0, 0, 80),
		},
	}
	result, err := VoxelizeBSPSolidCPU(bsp, []Face{face}, nil, VoxelizeOptions{
		VoxelResolution:     1,
		MaxSolidSampleCells: 100,
		SolidBandDepth:      2,
	})
	if err != nil {
		t.Fatalf("VoxelizeBSPSolidCPU failed: %v", err)
	}
	if len(result.Voxels) == 0 || len(result.Voxels) > 100 {
		t.Fatalf("voxels = %d", len(result.Voxels))
	}
	for _, voxel := range result.Voxels {
		if voxel.X < -2 || voxel.X > 4 || voxel.Y < -2 || voxel.Y > 4 || voxel.Z < -2 || voxel.Z > 2 {
			t.Fatalf("voxel outside non-sky face band: %+v", voxel)
		}
	}
}

func TestFillClosedInterior(t *testing.T) {
	voxels := make(map[[3]int]importcommon.Voxel)
	for x := 0; x <= 4; x++ {
		for y := 0; y <= 4; y++ {
			for z := 0; z <= 4; z++ {
				if x != 0 && x != 4 && y != 0 && y != 4 && z != 0 && z != 4 {
					continue
				}
				voxels[[3]int{x, y, z}] = importcommon.Voxel{X: x, Y: y, Z: z, Palette: 1, MaterialID: 1, SolidKind: "surface"}
			}
		}
	}
	before := len(voxels)
	fillClosedInterior(voxels)
	if got := len(voxels) - before; got != 27 {
		t.Fatalf("filled = %d, want 27", got)
	}
	center, ok := voxels[[3]int{2, 2, 2}]
	if !ok {
		t.Fatalf("center voxel not filled")
	}
	if center.SolidKind != "interior_fill" {
		t.Fatalf("center solid kind = %q", center.SolidKind)
	}
}

func TestFillClosedInteriorDoesNotFillOpenShell(t *testing.T) {
	voxels := make(map[[3]int]importcommon.Voxel)
	for x := 0; x <= 4; x++ {
		for y := 0; y <= 4; y++ {
			for z := 0; z <= 4; z++ {
				if z == 4 {
					continue
				}
				if x != 0 && x != 4 && y != 0 && y != 4 && z != 0 {
					continue
				}
				voxels[[3]int{x, y, z}] = importcommon.Voxel{X: x, Y: y, Z: z, Palette: 1, MaterialID: 1, SolidKind: "surface"}
			}
		}
	}
	before := len(voxels)
	fillClosedInterior(voxels)
	if got := len(voxels) - before; got != 0 {
		t.Fatalf("open shell filled %d voxel(s)", got)
	}
}
