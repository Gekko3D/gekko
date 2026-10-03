package gekko

import (
	"reflect"
	"slices"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p5iTransform() TransformComponent {
	return TransformComponent{Position: mgl32.Vec3{-32, 1, 0}, Rotation: mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0}), Scale: mgl32.Vec3{2, 1, 1}}
}

func p5iCompositeFixture() *volume.XBrickMap {
	x := volume.NewXBrickMap()
	x.SetVoxel(-32, 1, -3, 11)
	x.SetVoxel(-31, 1, -3, 8)
	x.SetVoxel(40, 1, 0, 7)
	x.ComputeAABB()
	x.ClearDirty()
	x.DirtyBricks[[6]int{9, 9, 9, 0, 0, 0}] = true
	for _, sector := range x.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{1, 2, 3}
		}
	}
	return x
}

func TestP5iTransformedColdCompositeSequentialParts(t *testing.T) {
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 4)
	source.SetVoxel(1, 0, 0, 5)
	before := source.Copy()
	part := authoredCollapseResolvedPart{def: content.AssetPartDef{ID: "part"}, world: p5iTransform(), geometry: VoxelGeometryAsset{XBrickMap: source}, voxelResolution: 1}
	got, want := p5iCompositeFixture(), p5iCompositeFixture()
	// Exact 90-degree Y rotation plus X scale two produces these center-selected cells.
	for repeat := 0; repeat < 2; repeat++ {
		if err := bakeResolvedPartIntoComposite(got, part, 1); err != nil {
			t.Fatal(err)
		}
		for _, w := range []volume.VoxelWrite{{X: -32, Y: 1, Z: -2, Value: 4}, {X: -32, Y: 1, Z: -1, Value: 4}, {X: -32, Y: 1, Z: -4, Value: 5}, {X: -32, Y: 1, Z: -3, Value: 5}} {
			want.SetVoxel(w.X, w.Y, w.Z, w.Value)
		}
		p5hMapParity(t, got, want)
	}
	subtract := part
	subtract.def.Source.Operation = content.AssetShapeOperationSubtract
	subtract.geometry = VoxelGeometryAsset{VoxModel: VoxModel{Voxels: []Voxel{{ColorIndex: 4}}}}
	if err := bakeResolvedPartIntoComposite(got, subtract, 1); err != nil {
		t.Fatal(err)
	}
	want.SetVoxel(-32, 1, -2, 0)
	want.SetVoxel(-32, 1, -1, 0)
	p5hMapParity(t, got, want)
	if !reflect.DeepEqual(VoxelObjectSnapshotFromXBrickMap(source), VoxelObjectSnapshotFromXBrickMap(before)) || source.Revision != before.Revision {
		t.Fatal("cold composition must not mutate source geometry")
	}
}

func TestP5iPublicColdSpawnUsesTransformedComposition(t *testing.T) {
	app, assets, def := NewApp(), newSpawnTestAssetServer(), p5eShapeDef()
	tr := p5iTransform()
	for i := range def.Parts {
		def.Parts[i].Transform.Position = content.Vec3{tr.Position.X(), tr.Position.Y(), tr.Position.Z()}
		def.Parts[i].Transform.Rotation = content.Quat{tr.Rotation.V.X(), tr.Rotation.V.Y(), tr.Rotation.V.Z(), tr.Rotation.W}
		def.Parts[i].Transform.Scale = content.Vec3{2, 1, 1}
	}
	left, right := slices.Clone(def.Parts[0].Source.VoxelShape.Voxels), slices.Clone(def.Parts[1].Source.VoxelShape.Voxels)
	spawn, err := p5eSpawn(t, app, assets, def, VoxelPartCollapseForce)
	if err != nil {
		t.Fatal(err)
	}
	model := p5eCollapsedModel(t, app, spawn)
	geometry, _ := assets.GetVoxelGeometry(model.GeometryAsset())
	want := []content.VoxelObjectVoxelDef{{X: -32, Y: 1, Z: -4, Value: 1}, {X: -32, Y: 1, Z: -3, Value: 1}, {X: -32, Y: 1, Z: -2, Value: 1}, {X: -32, Y: 1, Z: -1, Value: 1}}
	if !reflect.DeepEqual(VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap).Voxels, want) || geometry.XBrickMap.StructureDirty {
		t.Fatal("public cold spawn must publish the transformed center-selected composite with clean geometry")
	}
	if !reflect.DeepEqual(def.Parts[0].Source.VoxelShape.Voxels, left) || !reflect.DeepEqual(def.Parts[1].Source.VoxelShape.Voxels, right) {
		t.Fatal("cold spawn must preserve authored voxel records")
	}
}

func TestP5iCompositeValidationBeforeWrites(t *testing.T) {
	part := authoredCollapseResolvedPart{def: content.AssetPartDef{ID: "empty"}, world: TransformComponent{}, voxelResolution: 1}
	if err := bakeResolvedPartIntoComposite(nil, part, 1); err == nil || err.Error() != "destination map is nil" {
		t.Fatal("nil destination must retain its error precedence")
	}
	x := p5iCompositeFixture()
	before := x.Revision
	if err := bakeResolvedPartIntoComposite(x, part, 1); err == nil || err.Error() != "part empty has no voxel geometry" {
		t.Fatal("missing samples must precede zero-scale validation")
	}
	part.geometry = VoxelGeometryAsset{VoxModel: VoxModel{Voxels: []Voxel{{ColorIndex: 3}}}}
	if err := bakeResolvedPartIntoComposite(x, part, 1); err == nil || err.Error() != "part empty has zero voxel scale" || x.Revision != before {
		t.Fatal("zero-scale failure must leave destination unmodified")
	}
}
