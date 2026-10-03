package gekko

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p5hMapParity(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	if got.Revision != want.Revision || !reflect.DeepEqual(got.SectorRevisions, want.SectorRevisions) || got.GetVoxelCount() != want.GetVoxelCount() || !reflect.DeepEqual(got.Sectors, want.Sectors) {
		t.Fatal("construction changed ordered voxels, revisions/tombstones or brick occupancy/material encoding")
	}
	if got.StructureDirty != want.StructureDirty || !reflect.DeepEqual(got.DirtySectors, want.DirtySectors) || !reflect.DeepEqual(got.DirtyBricks, want.DirtyBricks) {
		t.Fatal("construction changed initial renderer dirtiness")
	}
	if got.AABBDirty != want.AABBDirty || got.CachedMin != want.CachedMin || got.CachedMax != want.CachedMax {
		t.Fatal("construction changed computed/captured bounds")
	}
}

func TestP5hOrdinaryAssetConstruction(t *testing.T) {
	model := VoxModel{SizeX: 48, SizeY: 12, SizeZ: 10}
	for x := uint32(0); x < 8; x++ {
		for y := uint32(0); y < 8; y++ {
			for z := uint32(0); z < 8; z++ {
				model.Voxels = append(model.Voxels, Voxel{X: x, Y: y, Z: z, ColorIndex: 3})
			}
		}
	}
	model.Voxels = append(model.Voxels,
		Voxel{X: 8, ColorIndex: 4}, Voxel{X: 9, ColorIndex: 5},
		Voxel{X: 32, ColorIndex: 7}, Voxel{X: 32, ColorIndex: 7}, Voxel{X: 32},
		Voxel{X: 40, ColorIndex: 8}, Voxel{X: 40}, Voxel{X: 40, ColorIndex: 9},
	)
	original := slices.Clone(model.Voxels)
	want := volume.NewXBrickMap()
	for _, v := range model.Voxels {
		want.SetVoxel(int(v.X), int(v.Y), int(v.Z), v.ColorIndex)
	}
	want.ComputeAABB()
	want.ClearDirty()
	server := newSpawnTestAssetServer()
	id := server.CreateVoxelGeometryFromSource(model, 1, "p5h-source.vox")
	asset, ok := server.GetVoxelGeometry(id)
	if !ok || asset.XBrickMap == nil {
		t.Fatal("expected ordinary voxel geometry asset")
	}
	p5hMapParity(t, asset.XBrickMap, want)
	if asset.LocalMin != (mgl32.Vec3{}) || asset.LocalMax != (mgl32.Vec3{48, 12, 10}) || asset.SourcePath != "p5h-source.vox" {
		t.Fatal("declared model dimensions/source must override tight geometry bounds")
	}
	secondID := server.CreateVoxelGeometryFromSource(model, 1, "p5h-independent.vox")
	second, _ := server.GetVoxelGeometry(secondID)
	asset.XBrickMap.SetVoxel(40, 0, 0, 0)
	if _, value := second.XBrickMap.GetVoxel(40, 0, 0); value != 9 || !reflect.DeepEqual(model.Voxels, original) {
		t.Fatal("ordinary construction must isolate editable maps and source records")
	}
}

func TestP5hEmptyAndUndeclaredAssetBounds(t *testing.T) {
	server := newSpawnTestAssetServer()
	empty := VoxModel{SizeX: 33, SizeY: 2, SizeZ: 4}
	emptyID := server.CreateVoxelGeometryFromSource(empty, 1, "empty.vox")
	asset, _ := server.GetVoxelGeometry(emptyID)
	if asset.XBrickMap.GetVoxelCount() != 0 || asset.LocalMax != (mgl32.Vec3{33, 2, 4}) || asset.LocalMin != (mgl32.Vec3{}) || asset.XBrickMap.StructureDirty {
		t.Fatal("empty declared model must preserve dimensions and clean editable geometry")
	}
	undeclared := VoxModel{Voxels: []Voxel{{X: 32, Y: 1, Z: 2, ColorIndex: 255}}}
	id := server.CreateVoxelGeometryFromSource(undeclared, 1, "undeclared.vox")
	asset, _ = server.GetVoxelGeometry(id)
	if asset.LocalMin != (mgl32.Vec3{32, 1, 2}) || asset.LocalMax != (mgl32.Vec3{33, 2, 3}) {
		t.Fatal("undeclared model dimensions must retain tight voxel bounds")
	}
}

func TestP5hPersistenceCaptureConstruction(t *testing.T) {
	first := streamedPersistenceBrick{}
	first.Payload[0][0][0], first.Payload[0][1][0] = 3, 4
	last := streamedPersistenceBrick{}
	last.Payload[0][0][0], last.Payload[1][0][0] = 9, 5
	negative := streamedPersistenceBrick{Coord: [3]int{-32, -8, -8}}
	negative.Payload[0][0][7] = 255
	input := streamedPersistenceInput{Bricks: []streamedPersistenceBrick{first, last, negative}, Min: mgl32.Vec3{-40, -10, -10}, Max: mgl32.Vec3{40, 35, 35}}
	original := slices.Clone(input.Bricks)
	want := volume.NewXBrickMap()
	for _, brick := range input.Bricks {
		for x := 0; x < volume.BrickSize; x++ {
			for y := 0; y < volume.BrickSize; y++ {
				for z := 0; z < volume.BrickSize; z++ {
					if value := brick.Payload[x][y][z]; value != 0 {
						want.SetVoxel(brick.Coord[0]+x, brick.Coord[1]+y, brick.Coord[2]+z, value)
					}
				}
			}
		}
	}
	want.CachedMin, want.CachedMax, want.AABBDirty = input.Min, input.Max, false
	got := persistenceInputMap(input)
	p5hMapParity(t, got, want)
	snapshot := VoxelObjectSnapshotFromXBrickMap(got)
	expected := []content.VoxelObjectVoxelDef{{X: -32, Y: -8, Z: -1, Value: 255}, {Value: 9}, {Y: 1, Value: 4}, {X: 1, Value: 5}}
	if !reflect.DeepEqual(snapshot.Voxels, expected) {
		t.Fatal("persistence snapshot changed duplicate override, zero skipping or negative coordinates")
	}
	terrain := terrainChunkDefFromXBrickMap("terrain", content.TerrainChunkCoordDef{}, 2, 1, got)
	if terrain.NonEmptyVoxelCount != 3 || !reflect.DeepEqual(terrain.Columns, []content.TerrainChunkColumnDef{{FilledVoxels: 2}, {X: 1, FilledVoxels: 1}}) {
		t.Fatal("persistence reconstruction changed serialized terrain height columns")
	}
	second := persistenceInputMap(input)
	got.SetVoxel(-32, -8, -1, 0)
	if _, value := second.GetVoxel(-32, -8, -1); value != 255 || !reflect.DeepEqual(input.Bricks, original) {
		t.Fatal("persistence reconstruction must own independent editable storage")
	}
}
