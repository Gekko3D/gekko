package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestVoxelBackingDestructionRoutesAcrossChunks(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	state := newDestructionTestVoxelRtState()
	server := newDestructionTestAssetServer()
	backingDef := testPlaneTreeBackingDef()
	backingDef.BoundsMax = [3]int{32, 16, 16}
	backingDef.PlaneTree.Planes[0].Distance = -1
	provider, err := NewPlaneTreeVoxelBacking(backingDef)
	if err != nil {
		t.Fatalf("NewPlaneTreeVoxelBacking failed: %v", err)
	}

	addChunk := func(coord int) (EntityId, *volume.XBrickMap, *VoxelBackingComponent) {
		xbm := volume.NewXBrickMap()
		if coord == 0 {
			xbm.SetVoxel(15, 2, 2, 3)
		}
		geometry := server.RegisterSharedVoxelGeometry(xbm, "")
		backing := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{coord, 0, 0}, 16, provider, nil)
		entity := cmd.AddEntity(
			&TransformComponent{Position: mgl32.Vec3{float32(coord * 16), 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
			&VoxelModelComponent{SharedGeometry: geometry, VoxelResolution: 1},
			&AuthoredImportedWorldChunkRefComponent{LevelID: "level", WorldID: "world", ChunkCoord: [3]int{coord, 0, 0}},
			&StreamedDestructionResidentComponent{LevelID: "level", WorldID: "world", ChunkCoord: [3]int{coord, 0, 0}},
			backing,
		)
		app.FlushCommands()
		object := core.NewVoxelObject()
		object.XBrickMap = xbm
		object.Transform.Position = mgl32.Vec3{float32(coord * 16), 0, 0}
		object.Transform.Dirty = true
		state.instanceMap[entity] = object
		return entity, xbm, backing
	}

	first, firstMap, firstBacking := addChunk(0)
	_, secondMap, secondBacking := addChunk(1)
	queue := &DestructionQueue{Events: []DestructionEvent{{
		Entity: first, Center: mgl32.Vec3{15.5, 2.5, 2.5}, Radius: 0.1, CarveOnly: true,
	}}}
	destructionSystem(state, queue, cmd, server)

	if found, _ := firstMap.GetVoxel(15, 2, 2); found {
		t.Fatal("expected boundary voxel to be carved")
	}
	if _, value := secondMap.GetVoxel(0, 2, 2); value != 3 {
		t.Fatalf("expected pistol-size carve shell in adjacent chunk, got %d", value)
	}
	if len(firstBacking.Removals) == 0 || len(secondBacking.Removals) == 0 {
		t.Fatal("expected removal masks in both crossed chunks")
	}
	removal := secondBacking.RemovalDef()
	restored := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{1, 0, 0}, 16, provider, &removal)
	reloadedMap := volume.NewXBrickMap()
	restored.ApplyRemovals(reloadedMap)
	if _, value := reloadedMap.GetVoxel(0, 2, 2); value != 3 {
		t.Fatalf("expected restored cross-chunk carve shell, got %d", value)
	}
}

func TestVoxelBackingMaterializationPreservesRemoval(t *testing.T) {
	provider, err := NewPlaneTreeVoxelBacking(testPlaneTreeBackingDef())
	if err != nil {
		t.Fatalf("NewPlaneTreeVoxelBacking failed: %v", err)
	}
	component := NewVoxelBackingComponent(
		content.VoxelBackingOwnerImportedWorld,
		"world",
		"source",
		[3]int{},
		16,
		provider,
		nil,
	)
	xbm := volume.NewXBrickMap()
	xbm.SetVoxel(7, 2, 2, 9)
	component.MaterializeSphere(xbm, mgl32.Vec3{5, 2, 2}, 1)
	volume.Sphere(xbm, mgl32.Vec3{5, 2, 2}, 1, 0)

	if _, value := xbm.GetVoxel(7, 2, 2); value != 9 {
		t.Fatalf("backing overwrote authored surface material, got %d", value)
	}
	if _, value := xbm.GetVoxel(6, 2, 2); value != 7 {
		t.Fatalf("expected implicit interior to materialize, got %d", value)
	}
	if found, _ := xbm.GetVoxel(5, 2, 2); found {
		t.Fatal("expected carved voxel to be empty")
	}

	component.MaterializeSphere(xbm, mgl32.Vec3{5, 2, 2}, 1)
	if found, _ := xbm.GetVoxel(5, 2, 2); found {
		t.Fatal("repeated materialization restored a removed voxel")
	}

	restored := NewVoxelBackingComponent(
		content.VoxelBackingOwnerImportedWorld,
		"world",
		"source",
		[3]int{},
		16,
		provider,
		func() *content.VoxelBackingRemovalDef { def := component.RemovalDef(); return &def }(),
	)
	reloadedMap := volume.NewXBrickMap()
	restored.MaterializeSphere(reloadedMap, mgl32.Vec3{5, 2, 2}, 1)
	if found, _ := reloadedMap.GetVoxel(5, 2, 2); found {
		t.Fatal("restored removal delta did not mask the base")
	}
}

func TestVoxelBackingMaterializesExposedShellAcrossBrickBoundary(t *testing.T) {
	def := testPlaneTreeBackingDef()
	def.BoundsMax = [3]int{24, 16, 16}
	def.PlaneTree.Leaves[1].Solid = true
	provider, err := NewPlaneTreeVoxelBacking(def)
	if err != nil {
		t.Fatal(err)
	}
	component := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{}, 24, provider, nil)
	xbm := volume.NewXBrickMap()
	xbm.SetVoxel(8, 4, 4, 3)
	component.MaterializeSphere(xbm, mgl32.Vec3{8.5, 4.5, 4.5}, 0.1)
	volume.Sphere(xbm, mgl32.Vec3{8.5, 4.5, 4.5}, 0.1, 0)
	if _, value := xbm.GetVoxel(7, 4, 4); value != 3 {
		t.Fatalf("expected backing shell to preserve hit material, got %d", value)
	}
	if found, _ := xbm.GetVoxel(0, 0, 0); found {
		t.Fatal("carve shell materialized an unrelated voxel from the same brick")
	}

	removal := component.RemovalDef()
	restored := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{}, 24, provider, &removal)
	reloadedMap := volume.NewXBrickMap()
	reloadedMap.SetVoxel(8, 4, 4, 3)
	restored.ApplyRemovals(reloadedMap)
	if found, _ := reloadedMap.GetVoxel(8, 4, 4); found {
		t.Fatal("restored carved voxel is not empty")
	}
	if _, value := reloadedMap.GetVoxel(7, 4, 4); value != 3 {
		t.Fatalf("expected restored backing shell material, got %d", value)
	}
	if found, _ := reloadedMap.GetVoxel(0, 0, 0); found {
		t.Fatal("restored carve shell materialized an unrelated voxel from the same brick")
	}
}

func TestTerrainColumnsUseVoxelBackingMaterializer(t *testing.T) {
	chunk := &content.TerrainChunkDef{
		TerrainID:       "terrain",
		SourceHash:      "terrain-source",
		Coord:           content.TerrainChunkCoordDef{},
		ChunkSize:       16,
		VoxelResolution: 1,
		SolidValue:      3,
		Columns: []content.TerrainChunkColumnDef{{
			X: 2, Z: 2, FilledVoxels: 24,
		}},
	}
	component := NewVoxelBackingComponent(
		content.VoxelBackingOwnerTerrain,
		chunk.TerrainID,
		chunk.SourceHash,
		[3]int{},
		chunk.ChunkSize,
		NewTerrainColumnVoxelBacking(chunk),
		nil,
	)
	xbm := volume.NewXBrickMap()
	component.MaterializeSphere(xbm, mgl32.Vec3{2, 18, 2}, 1)
	if _, value := xbm.GetVoxel(2, 19, 2); value != 3 {
		t.Fatalf("expected terrain column backing to materialize through generic path, got %d", value)
	}
	if found, _ := xbm.GetVoxel(3, 19, 2); found {
		t.Fatal("terrain backing filled outside authored column")
	}
}

func TestPlaneTreeBackingUnionsOnlyExactVolumeBounds(t *testing.T) {
	def := testPlaneTreeBackingDef()
	def.PlaneTree.Planes[0].Distance = 0
	def.PlaneTree.Volumes = []content.VoxelBackingPlaneVolumeDef{
		{Root: 0, BoundsMin: [3]int{0, 0, 0}, BoundsMax: [3]int{4, 16, 16}},
		{Root: 0, BoundsMin: [3]int{8, 0, 0}, BoundsMax: [3]int{12, 16, 16}},
	}
	provider, err := NewPlaneTreeVoxelBacking(def)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		voxel [3]int
		solid bool
	}{
		{voxel: [3]int{2, 4, 4}, solid: true},
		{voxel: [3]int{6, 4, 4}, solid: false},
		{voxel: [3]int{10, 4, 4}, solid: true},
	} {
		if solid := provider.VoxelValue(test.voxel) != 0; solid != test.solid {
			t.Fatalf("exact volume voxel %v solid=%t, want %t", test.voxel, solid, test.solid)
		}
	}
}

func TestPlaneTreeBackingSurfaceSupportExtendsOnlyBehindSurface(t *testing.T) {
	def := testPlaneTreeBackingDef()
	def.PlaneTree.Leaves[0].Solid = false
	def.SurfaceSupports = []content.VoxelBackingSurfaceSupportDef{{
		Vertices: [3][3]float32{{2, 8, 2}, {12, 8, 2}, {2, 8, 12}},
		Normal:   [3]float32{0, 1, 0},
		Depth:    4,
	}}
	provider, err := NewPlaneTreeVoxelBacking(def)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		voxel [3]int
		solid bool
	}{
		{voxel: [3]int{3, 7, 3}, solid: true},
		{voxel: [3]int{3, 4, 3}, solid: true},
		{voxel: [3]int{3, 2, 3}, solid: false},
		{voxel: [3]int{3, 9, 3}, solid: false},
		{voxel: [3]int{14, 7, 14}, solid: false},
	} {
		if solid := provider.VoxelValue(test.voxel) != 0; solid != test.solid {
			t.Fatalf("surface support voxel %v solid=%t, want %t", test.voxel, solid, test.solid)
		}
	}
}

func TestPlaneTreeBackingSurfaceSupportRequiresSurfaceEntry(t *testing.T) {
	def := testPlaneTreeBackingDef()
	def.PlaneTree.Leaves[0].Solid = false
	def.SurfaceSupports = []content.VoxelBackingSurfaceSupportDef{{
		Vertices: [3][3]float32{{2, 8, 2}, {12, 8, 2}, {2, 8, 12}},
		Normal:   [3]float32{0, 1, 0},
		Depth:    4,
	}}
	provider, err := NewPlaneTreeVoxelBacking(def)
	if err != nil {
		t.Fatal(err)
	}

	side := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{}, 16, provider, nil)
	sideMap := volume.NewXBrickMap()
	for range 2 {
		side.MaterializeSphere(sideMap, mgl32.Vec3{3.5, 4.5, 3.5}, 0.1)
		volume.Sphere(sideMap, mgl32.Vec3{3.5, 4.5, 3.5}, 0.1, 0)
	}
	if sideMap.GetVoxelCount() != 0 {
		t.Fatalf("side edit activated unrelated ground support: %d voxels", sideMap.GetVoxelCount())
	}

	top := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, "world", "source", [3]int{}, 16, provider, nil)
	topMap := volume.NewXBrickMap()
	top.MaterializeSphere(topMap, mgl32.Vec3{3.5, 7.5, 3.5}, 0.1)
	volume.Sphere(topMap, mgl32.Vec3{3.5, 7.5, 3.5}, 0.1, 0)
	if _, value := topMap.GetVoxel(3, 6, 3); value != 7 {
		t.Fatalf("surface edit did not activate ground support, got %d", value)
	}
	top.MaterializeSphere(topMap, mgl32.Vec3{3.5, 6.5, 3.5}, 0.1)
	volume.Sphere(topMap, mgl32.Vec3{3.5, 6.5, 3.5}, 0.1, 0)
	if _, value := topMap.GetVoxel(3, 5, 3); value != 7 {
		t.Fatalf("continued tunnel did not retain ground support, got %d", value)
	}
}

func testPlaneTreeBackingDef() *content.VoxelBackingDef {
	return &content.VoxelBackingDef{
		SchemaVersion: content.CurrentVoxelBackingSchemaVersion,
		Kind:          content.VoxelBackingKindPlaneTreeV1,
		SourceHash:    "source",
		BoundsMin:     [3]int{0, 0, 0},
		BoundsMax:     [3]int{16, 16, 16},
		SolidValue:    7,
		PlaneTree: &content.VoxelBackingPlaneTreeDef{
			Root:   0,
			Planes: []content.VoxelBackingPlaneDef{{Normal: [3]float32{1, 0, 0}, Distance: 4}},
			Nodes:  []content.VoxelBackingPlaneNodeDef{{Plane: 0, Children: [2]int32{-1, -2}}},
			Leaves: []content.VoxelBackingPlaneLeafDef{{Solid: true}, {Solid: false}},
		},
	}
}
