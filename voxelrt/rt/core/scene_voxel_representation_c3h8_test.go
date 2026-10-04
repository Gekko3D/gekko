package core

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func c3h8Map(cells ...[3]int) *volume.XBrickMap {
	m := volume.NewXBrickMap()
	for _, p := range cells {
		m.SetVoxel(p[0], p[1], p[2], 7)
	}
	return m
}
func c3h8ApproxMat(t *testing.T, got, want mgl32.Mat4) {
	t.Helper()
	for i := range got {
		if math.IsNaN(float64(got[i])) || math.IsInf(float64(got[i]), 0) || math.Abs(float64(got[i]-want[i])) > 1e-5 {
			t.Fatalf("matrix element%d got%v want%v", i, got, want)
		}
	}
}
func c3h8ApproxBounds(t *testing.T, got *[2]mgl32.Vec3, want [2]mgl32.Vec3) {
	t.Helper()
	if got == nil {
		t.Fatal("missing render bounds")
	}
	for end := range want {
		for axis := range want[end] {
			if math.IsNaN(float64(got[end][axis])) || math.IsInf(float64(got[end][axis]), 0) || math.Abs(float64(got[end][axis]-want[end][axis])) > 1e-4 {
				t.Fatalf("bounds got%v want%v", *got, want)
			}
		}
	}
}
func c3h8WorldBounds(matrix mgl32.Mat4, min, max mgl32.Vec3) [2]mgl32.Vec3 {
	result := [2]mgl32.Vec3{{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}, {-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}}
	for corner := 0; corner < 8; corner++ {
		var p mgl32.Vec3
		for axis := 0; axis < 3; axis++ {
			p[axis] = min[axis]
			if corner&(1<<axis) != 0 {
				p[axis] = max[axis]
			}
		}
		world := matrix.Mul4x1(p.Vec4(1)).Vec3()
		for axis := range world {
			if world[axis] < result[0][axis] {
				result[0][axis] = world[axis]
			}
			if world[axis] > result[1][axis] {
				result[1][axis] = world[axis]
			}
		}
	}
	return result
}

func TestC3h8DefaultExactLegacyAndInvalidSelection(t *testing.T) {
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
	obj.UpdateWorldAABB()
	bounds := obj.WorldAABB
	o2w, w2o := obj.Transform.ObjectToWorld(), obj.Transform.WorldToObject()
	if obj.RenderRepresentationValid() || obj.RenderVoxelMap() != obj.XBrickMap || obj.RenderObjectToWorld() != o2w || obj.RenderWorldToObject() != w2o || obj.RenderWorldBounds() != bounds {
		t.Fatal("default selection changes legacy identity/maps/matrices/bounds")
	}
	obj.ClearRenderRepresentation()
	if obj.RenderWorldBounds() != bounds {
		t.Fatal("clear with no rep changes legacy pointer")
	}
	coarse := c3h8Map([3]int{0, 0, 0})
	for _, kind := range []string{"nil-map", "empty-map", "same-full-map", "nil-transform", "nil-full-map"} {
		t.Run(kind, func(t *testing.T) {
			o := NewVoxelObject()
			o.XBrickMap = c3h8Map([3]int{1, 0, 0})
			if !o.SetRenderLOD2(coarse) {
				t.Fatal("valid setup rejected")
			}
			candidate := coarse
			switch kind {
			case "nil-map":
				candidate = nil
			case "empty-map":
				candidate = volume.NewXBrickMap()
			case "same-full-map":
				candidate = o.XBrickMap
			case "nil-transform":
				o.Transform = nil
			case "nil-full-map":
				o.XBrickMap = nil
			}
			if o.SetRenderLOD2(candidate) || o.RenderRepresentationValid() {
				t.Fatal("invalid setter accepted/retained previous representation")
			}
			if o.RenderVoxelMap() != o.XBrickMap {
				t.Fatal("invalid setter didn't clear old render map")
			}
		})
	}
	var absent *VoxelObject
	if absent.SetRenderLOD2(coarse) || absent.RenderRepresentationValid() || absent.RenderVoxelMap() != nil || absent.RenderWorldBounds() != nil {
		t.Fatal("nil object accepted representation")
	}
	absent.ClearRenderRepresentation()
}

func TestC3h8ZeroAnchoredSignedBoundsWithRotationScaleAndPivot(t *testing.T) {
	obj := NewVoxelObject()
	full := c3h8Map([3]int{-3, -1, 1}, [3]int{3, 5, 3})
	coarse := c3h8Map([3]int{-2, -1, 0}, [3]int{1, 2, 1})
	obj.XBrickMap = full
	obj.Transform.Position = mgl32.Vec3{11, -5, 3}
	obj.Transform.Scale = mgl32.Vec3{.5, 2, 1.5}
	obj.Transform.Pivot = mgl32.Vec3{1.25, -.75, .5}
	obj.Transform.Rotation = mgl32.QuatRotate(.7, mgl32.Vec3{0, 0, 1}).Mul(mgl32.QuatRotate(.3, mgl32.Vec3{1, 0, 0}))
	obj.UpdateWorldAABB()
	fullBounds := obj.WorldAABB
	fullBoundsValue := *fullBounds
	fullRevision, coarseRevision := full.Revision, coarse.Revision
	fullCells, coarseCells := full.GetVoxelCount(), coarse.GetVoxelCount()
	legacyO2W, legacyW2O := obj.Transform.ObjectToWorld(), obj.Transform.WorldToObject()
	obj.Transform.Dirty = true
	full.AABBDirty = true
	if !obj.SetRenderLOD2(coarse) || !obj.RenderRepresentationValid() || obj.RenderVoxelMap() != coarse {
		t.Fatal("valid LOD not selected")
	}
	wantMatrix := legacyO2W.Mul4(mgl32.Scale3D(2, 2, 2))
	wantInverse := mgl32.Scale3D(.5, .5, .5).Mul4(legacyW2O)
	c3h8ApproxMat(t, obj.RenderObjectToWorld(), wantMatrix)
	c3h8ApproxMat(t, obj.RenderWorldToObject(), wantInverse)
	c3h8ApproxMat(t, obj.RenderWorldToObject().Mul4(obj.RenderObjectToWorld()), mgl32.Ident4())
	// Actual coarse cells have bounds[-2,-1,0]..[2,3,2], including approved
	// expansion beyond odd fine extrema. This is not an extent-ratio transform.
	wantBounds := c3h8WorldBounds(wantMatrix, mgl32.Vec3{-2, -1, 0}, mgl32.Vec3{2, 3, 2})
	c3h8ApproxBounds(t, obj.RenderWorldBounds(), wantBounds)
	coarse.AABBDirty = true
	c3h8ApproxBounds(t, obj.RenderWorldBounds(), wantBounds)
	if obj.XBrickMap != full || obj.WorldAABB != fullBounds || *obj.WorldAABB != fullBoundsValue || !obj.Transform.Dirty || !full.AABBDirty || full.Revision != fullRevision || coarse.Revision != coarseRevision || full.GetVoxelCount() != fullCells || coarse.GetVoxelCount() != coarseCells {
		t.Fatal("render representation mutated authoritative map/transform/bounds")
	}
	c3h8ApproxMat(t, obj.Transform.ObjectToWorld(), legacyO2W)
	c3h8ApproxMat(t, obj.Transform.WorldToObject(), legacyW2O)
	obj.Transform.Position = mgl32.Vec3{-4, 9, 2}
	obj.Transform.Scale = mgl32.Vec3{2, .25, 3}
	obj.Transform.Pivot = mgl32.Vec3{-.5, 2, 1}
	obj.Transform.Dirty = true
	changed := obj.Transform.ObjectToWorld().Mul4(mgl32.Scale3D(2, 2, 2))
	c3h8ApproxMat(t, obj.RenderObjectToWorld(), changed)
	c3h8ApproxBounds(t, obj.RenderWorldBounds(), c3h8WorldBounds(changed, mgl32.Vec3{-2, -1, 0}, mgl32.Vec3{2, 3, 2}))
	if !obj.RenderRepresentationValid() || obj.WorldAABB != fullBounds || *obj.WorldAABB != fullBoundsValue || !obj.Transform.Dirty {
		t.Fatal("instance transform invalidates geometry or mutates full bounds")
	}
	// Another caller may update full matrices/bounds and clear Dirty before this
	// getter. Render bounds must still observe the changed matrix itself.
	obj.Transform.Position[0] += 20
	obj.Transform.Dirty = true
	obj.UpdateWorldAABB()
	if obj.Transform.Dirty {
		t.Fatal("full AABB fixture didn't clear Dirty")
	}
	changed = obj.Transform.ObjectToWorld().Mul4(mgl32.Scale3D(2, 2, 2))
	c3h8ApproxBounds(t, obj.RenderWorldBounds(), c3h8WorldBounds(changed, mgl32.Vec3{-2, -1, 0}, mgl32.Vec3{2, 3, 2}))
	replacement := NewTransform()
	replacement.Position = mgl32.Vec3{3, 8, -9}
	replacement.Scale = mgl32.Vec3{1.25, .75, 2}
	replacement.Pivot = mgl32.Vec3{2, -1, .25}
	replacement.Dirty = false
	obj.Transform = replacement
	replacedMatrix := replacement.ObjectToWorld().Mul4(mgl32.Scale3D(2, 2, 2))
	c3h8ApproxMat(t, obj.RenderObjectToWorld(), replacedMatrix)
	c3h8ApproxMat(t, obj.RenderWorldToObject(), mgl32.Scale3D(.5, .5, .5).Mul4(replacement.WorldToObject()))
	c3h8ApproxBounds(t, obj.RenderWorldBounds(), c3h8WorldBounds(replacedMatrix, mgl32.Vec3{-2, -1, 0}, mgl32.Vec3{2, 3, 2}))
	if !obj.RenderRepresentationValid() || obj.Transform.Dirty {
		t.Fatal("transform pointer replacement invalidated geometry or changed clean Dirty state")
	}
	obj.ClearRenderRepresentation()
	if obj.RenderRepresentationValid() || obj.RenderVoxelMap() != full || obj.RenderWorldBounds() != obj.WorldAABB || obj.RenderObjectToWorld() != obj.Transform.ObjectToWorld() || obj.RenderWorldToObject() != obj.Transform.WorldToObject() {
		t.Fatal("clear didn't restore exact full geometry representation")
	}
}

func TestC3h8TrackedGeometryChangesFailClosed(t *testing.T) {
	for _, kind := range []string{"source-edit", "coarse-edit", "source-replacement", "nil-transform"} {
		t.Run(kind, func(t *testing.T) {
			obj := NewVoxelObject()
			full := c3h8Map([3]int{1, 0, 0})
			coarse := c3h8Map([3]int{0, 0, 0})
			obj.XBrickMap = full
			obj.UpdateWorldAABB()
			if !obj.SetRenderLOD2(coarse) {
				t.Fatal("setup")
			}
			_ = obj.RenderWorldBounds()
			switch kind {
			case "source-edit":
				full.SetVoxel(2, 0, 0, 7)
			case "coarse-edit":
				coarse.SetVoxel(1, 0, 0, 7)
			case "source-replacement":
				obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
			case "nil-transform":
				obj.Transform = nil
			}
			if obj.RenderRepresentationValid() || obj.RenderVoxelMap() != nil || obj.RenderWorldBounds() != nil {
				t.Fatal("invalid representation silently fell back to unready full geometry")
			}
			// An explicit owner transition restores the authoritative path.
			obj.ClearRenderRepresentation()
			if obj.RenderVoxelMap() != obj.XBrickMap || obj.RenderWorldBounds() != obj.WorldAABB {
				t.Fatal("explicit clear didn't restore source")
			}
		})
	}
}

func TestC3h8RenderSelectionPreservesFullCPUPickingAndObjectIdentity(t *testing.T) {
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
	coarse := c3h8Map([3]int{0, 0, 0})
	obj.UpdateWorldAABB()
	scene := &Scene{Objects: []*VoxelObject{obj}}
	before := append([]*VoxelObject(nil), scene.Objects...)
	revision := scene.StructureRevision
	ray := Ray{Origin: mgl32.Vec3{-2, .5, .5}, Direction: mgl32.Vec3{1, 0, 0}}
	beforeHit := scene.Raycast(ray, 10)
	if beforeHit == nil || beforeHit.Coord != [3]int{1, 0, 0} {
		t.Fatal("source ray fixture missed")
	}
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	afterHit := scene.Raycast(ray, 10)
	if afterHit == nil || afterHit.Object != obj || afterHit.Coord != beforeHit.Coord || math.Abs(float64(afterHit.T-beforeHit.T)) > 1e-6 {
		t.Fatal("render LOD changed authoritative picking", beforeHit, afterHit)
	}
	// Coarse coverage occupies x=.5; source geometry leaves that column empty.
	if hit := scene.Raycast(Ray{Origin: mgl32.Vec3{.5, .5, -1}, Direction: mgl32.Vec3{0, 0, 1}}, 10); hit != nil {
		t.Fatal("CPU picking followed expanded render coverage")
	}
	if !reflect.DeepEqual(scene.Objects, before) || scene.StructureRevision != revision {
		t.Fatal("render representation inserted a second scene object")
	}
}
