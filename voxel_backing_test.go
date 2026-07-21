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
			xbm.SetVoxel(15, 2, 2, 7)
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

	first, _, firstBacking := addChunk(0)
	_, secondMap, secondBacking := addChunk(1)
	queue := &DestructionQueue{Events: []DestructionEvent{{
		Entity: first, Center: mgl32.Vec3{15.5, 2, 2}, Radius: 2, CarveOnly: true,
	}}}
	destructionSystem(state, queue, cmd, server)

	if found, _ := secondMap.GetVoxel(0, 2, 2); found {
		t.Fatal("expected carve to cross into adjacent backed chunk")
	}
	if _, value := secondMap.GetVoxel(7, 2, 2); value != 7 {
		t.Fatalf("expected adjacent touched brick to materialize, got %d", value)
	}
	if len(firstBacking.Removals) == 0 || len(secondBacking.Removals) == 0 {
		t.Fatal("expected removal masks in both crossed chunks")
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
	if _, value := xbm.GetVoxel(2, 23, 2); value != 3 {
		t.Fatalf("expected terrain column backing to materialize through generic path, got %d", value)
	}
	if found, _ := xbm.GetVoxel(3, 23, 2); found {
		t.Fatal("terrain backing filled outside authored column")
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
