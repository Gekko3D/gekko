package core

import (
	"bytes"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func s1l14RenderInput(t *testing.T, o *VoxelObject) ManagedGeometryInput {
	t.Helper()
	owner := volume.NewManagedXBrickMap(o.XBrickMap)
	o.SetManagedGeometryProducerWithGenerationReader(o.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) { v, ok := owner.CaptureGeometry(); return v, 7, ok }, nil, func() (uint64, bool) { return 7, true })
	in, ok := o.CaptureManagedGeometryInput()
	if !ok {
		t.Fatal("input")
	}
	return in
}

func TestS1l14ManagedSelectionUnitTransformIndependentAuthorityAndMatchingClear(t *testing.T) {
	o := NewVoxelObject()
	o.XBrickMap = c3h8Map([3]int{1, 0, 0})
	in := s1l14RenderInput(t, o)
	o.Transform.Position = mgl32.Vec3{10, -4, 2}
	o.Transform.Scale = mgl32.Vec3{.5, 2, 3}
	o.Transform.Pivot = mgl32.Vec3{1, .5, -2}
	o.Transform.Rotation = mgl32.QuatRotate(.4, mgl32.Vec3{0, 0, 1})
	o.UpdateWorldAABB()
	authority, bounds := o.XBrickMap, o.WorldAABB
	savedBounds := *bounds
	selected := c3h8Map([3]int{-32, 0, 0})
	minimum, maximum := mgl32.Vec3{-32, 0, 0}, mgl32.Vec3{32, 32, 32}
	if !o.SetManagedRenderGeometry(in, selected, minimum, maximum) {
		t.Fatal("qualified selection refused")
	}
	c3h8ApproxMat(t, o.RenderObjectToWorld(), o.Transform.ObjectToWorld())
	c3h8ApproxMat(t, o.RenderWorldToObject(), o.Transform.WorldToObject())
	c3h8ApproxBounds(t, o.RenderWorldBounds(), c3h8WorldBounds(o.Transform.ObjectToWorld(), minimum, maximum))
	if !o.MatchesManagedGeometrySource(in) {
		t.Fatal("render selection invalidates authority producer")
	}
	authority.SetVoxel(400, 0, 0, 8)
	if o.RenderVoxelMap() != selected {
		t.Fatal("authority edit implicitly replaces display")
	}
	selected.SetVoxel(1000, 0, 0, 9)
	c3h8ApproxBounds(t, o.RenderWorldBounds(), c3h8WorldBounds(o.Transform.ObjectToWorld(), minimum, maximum))
	if o.XBrickMap != authority || o.WorldAABB != bounds || *bounds != savedBounds {
		t.Fatal("render selection changed CPU authority bounds")
	}
	if o.ClearManagedRenderGeometry(ManagedGeometryInput{}) || o.RenderVoxelMap() != selected {
		t.Fatal("foreign clear changed selection")
	}
	if !o.ClearManagedRenderGeometry(in) || o.RenderVoxelMap() != authority {
		t.Fatal("matching clear did not restore authority")
	}
}

func TestS1l14ManagedInitialUnreadyAndPublicationRebuildAllPassBounds(t *testing.T) {
	s := NewScene()
	s.Lights = []Light{testShadowCastingDirectionalLight()}
	o := NewVoxelObject()
	o.XBrickMap = c3h8Map([3]int{1, 0, 0})
	o.MaterialTable = []Material{DefaultMaterial(), {Transmission: .25}}
	in := s1l14RenderInput(t, o)
	s.AddObject(o)
	s.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	old := bytes.Clone(s.BVHNodesBytes)
	if !o.SetManagedRenderGeometry(in, nil, mgl32.Vec3{}, mgl32.Vec3{}) {
		t.Fatal("nil target must install active unready selection")
	}
	s.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	if o.RenderVoxelMap() != nil || o.RenderWorldBounds() != nil || len(s.VisibleObjects)+len(s.TransparentVisibleObjects)+len(s.ShadowObjects) != 0 {
		t.Fatal("initial unready geometry entered passes")
	}
	hit := s.Raycast(Ray{Origin: mgl32.Vec3{-2, .5, .5}, Direction: mgl32.Vec3{1, 0, 0}}, 10)
	if hit == nil || hit.Coord != [3]int{1, 0, 0} {
		t.Fatal("hidden rendering disabled authority picking")
	}
	if !o.SetManagedRenderGeometry(in, c3h8Map([3]int{-32, 0, 0}), mgl32.Vec3{-32, 0, 0}, mgl32.Vec3{0, 32, 32}) {
		t.Fatal("publication")
	}
	s.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	if len(s.VisibleObjects) != 1 || len(s.TransparentVisibleObjects) != 1 || len(s.ShadowObjects) != 1 || bytes.Equal(old, s.BVHNodesBytes) {
		t.Fatal("managed publication did not update pass bounds")
	}
	if len(s.TransparentBVHNodesBytes) == 0 || len(s.ShadowBVHNodesBytes) == 0 {
		t.Fatal("managed publication missing pass BVH")
	}
}

func TestS1l14ManagedSelectionRefusesForeignLODAndSpecialOwners(t *testing.T) {
	for _, kind := range []string{"foreign", "stale-source", "lod", "gpu", "terrain", "planet", "adjacency"} {
		t.Run(kind, func(t *testing.T) {
			o := NewVoxelObject()
			o.XBrickMap = c3h8Map([3]int{1, 0, 0})
			in := s1l14RenderInput(t, o)
			switch kind {
			case "foreign":
				other := NewVoxelObject()
				other.XBrickMap = o.XBrickMap
				in = s1l14RenderInput(t, other)
			case "stale-source":
				s1l14RenderInput(t, o)
			case "lod":
				if !o.SetRenderLOD2(c3h8Map([3]int{0, 0, 0})) {
					t.Fatal("LOD setup")
				}
			case "gpu":
				o.XBrickMap.GPUEditMode = true
			case "terrain":
				o.IsTerrainChunk = true
			case "planet":
				o.IsPlanetTile = true
			case "adjacency":
				o.VoxelAdjacencyGroupID = 1
			}
			before := o.RenderVoxelMap()
			if o.SetManagedRenderGeometry(in, c3h8Map([3]int{0, 0, 0}), mgl32.Vec3{}, mgl32.Vec3{32, 32, 32}) || o.RenderVoxelMap() != before {
				t.Fatal("ineligible install changed owner selection")
			}
		})
	}
}

func TestS1l14MatchingManagedClearSurvivesProducerDetachment(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "detach", true: "replace"}[replace], func(t *testing.T) {
			o := NewVoxelObject()
			o.XBrickMap = c3h8Map([3]int{1, 0, 0})
			in := s1l14RenderInput(t, o)
			target := c3h8Map([3]int{0, 0, 0})
			if !o.SetManagedRenderGeometry(in, target, mgl32.Vec3{}, mgl32.Vec3{32, 32, 32}) {
				t.Fatal("install")
			}
			foreign := ManagedGeometryInput{}
			if replace {
				foreign = s1l14RenderInput(t, o)
			} else {
				o.SetManagedGeometryProducer(nil, nil)
			}
			if o.ClearManagedRenderGeometry(foreign) {
				t.Fatal("foreign identity cleared detached selection")
			}
			if !o.ClearManagedRenderGeometry(in) || o.RenderVoxelMap() != o.XBrickMap {
				t.Fatal("stored matching identity cannot clear detached selection")
			}
		})
	}
}

func TestS1l14FullCaptureRechecksAttachmentAndSelectionAfterCallback(t *testing.T) {
	for _, kind := range []string{"reattach", "lod", "terrain", "expose"} {
		t.Run(kind, func(t *testing.T) {
			o := NewVoxelObject()
			o.XBrickMap = c3h8Map([3]int{1, 0, 0})
			owner := volume.NewManagedXBrickMap(o.XBrickMap)
			o.SetManagedGeometryProducer(o.XBrickMap, func() (volume.ManagedGeometryView, uint64, bool) {
				v, ok := owner.CaptureGeometry()
				switch kind {
				case "reattach":
					o.SetManagedGeometryProducer(nil, nil)
				case "lod":
					o.SetRenderLOD2(c3h8Map([3]int{0, 0, 0}))
				case "terrain":
					o.IsTerrainChunk = true
				case "expose":
					o.XBrickMap = o.XBrickMap.Copy()
				}
				return v, 7, ok
			})
			if in, ok := o.CaptureManagedGeometryInput(); ok || in.SameSource(in) {
				t.Fatal("callback-invalidated capture qualified")
			}
		})
	}
}

func TestS1l14ManagedClearMatchesPublicationInputGeneration(t *testing.T) {
	o := NewVoxelObject()
	o.XBrickMap = c3h8Map([3]int{1, 0, 0})
	old := s1l14RenderInput(t, o)
	newer := old
	newer.generation++
	target := c3h8Map([3]int{0, 0, 0})
	if !o.SetManagedRenderGeometry(newer, target, mgl32.Vec3{}, mgl32.Vec3{32, 32, 32}) {
		t.Fatal("install newer publication")
	}
	if o.ClearManagedRenderGeometry(old) || o.RenderVoxelMap() != target {
		t.Fatal("older same-source input cleared newer publication")
	}
	if !o.ClearManagedRenderGeometry(newer) {
		t.Fatal("matching publication clear refused")
	}
}

func TestS1l14ExplicitLODSelectionTakesOwnershipFromManagedDisplay(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(map[bool]string{false: "current", true: "unready"}[hidden], func(t *testing.T) {
			o := NewVoxelObject()
			o.XBrickMap = c3h8Map([3]int{1, 0, 0})
			in := s1l14RenderInput(t, o)
			var target *volume.XBrickMap
			if !hidden {
				target = c3h8Map([3]int{0, 0, 0})
			}
			if !o.SetManagedRenderGeometry(in, target, mgl32.Vec3{}, mgl32.Vec3{32, 32, 32}) {
				t.Fatal("managed install")
			}
			coarse := c3h8Map([3]int{0, 0, 0})
			if !o.SetRenderLOD2(coarse) || o.RenderVoxelMap() != coarse {
				t.Fatal("managed selector masked explicit LOD owner")
			}
			c3h8ApproxMat(t, o.RenderObjectToWorld(), o.Transform.ObjectToWorld().Mul4(mgl32.Scale3D(2, 2, 2)))
			if o.MatchesManagedGeometrySource(in) || o.ClearManagedRenderGeometry(in) || o.RenderVoxelMap() != coarse {
				t.Fatal("stale managed owner disrupted LOD selection")
			}
			o.ClearRenderRepresentation()
			if o.RenderVoxelMap() != o.XBrickMap {
				t.Fatal("general clear failed to restore authority")
			}
		})
	}
}

func TestS1l14ManagedRenderLocalBoundsIndependentOfOccupiedNormalCache(t *testing.T) {
	o := NewVoxelObject()
	o.XBrickMap = c3h8Map([3]int{1, 0, 0})
	in := s1l14RenderInput(t, o)
	selected := c3h8Map([3]int{1, 1, 4})
	minimum, maximum := mgl32.Vec3{-32, 0, 0}, mgl32.Vec3{32, 32, 32}
	if !o.SetManagedRenderGeometry(in, selected, minimum, maximum) {
		t.Fatal("managed selection")
	}
	occupiedMinimum, occupiedMaximum := selected.ComputeAABB()
	if occupiedMinimum == minimum && occupiedMaximum == maximum {
		t.Fatal("fixture lacks distinct occupied and conservative bounds")
	}
	for _, edit := range [][3]int{{31, 1, 28}, {-24, 4, 8}} {
		gotMinimum, gotMaximum := o.RenderLocalBounds()
		if gotMinimum != minimum || gotMaximum != maximum {
			t.Fatalf("render local bounds used occupied normal cache: %v..%v want %v..%v", gotMinimum, gotMaximum, minimum, maximum)
		}
		selected.SetVoxel(edit[0], edit[1], edit[2], 7)
		selected.ComputeAABB()
	}
	gotMinimum, gotMaximum := o.RenderLocalBounds()
	if gotMinimum != minimum || gotMaximum != maximum {
		t.Fatal("current occupied-bounds expansion cropped conservative render coverage")
	}
	c3h8ApproxBounds(t, o.RenderWorldBounds(), c3h8WorldBounds(o.Transform.ObjectToWorld(), minimum, maximum))
	if !o.SetManagedRenderGeometry(in, nil, mgl32.Vec3{}, mgl32.Vec3{}) {
		t.Fatal("unready install")
	}
	gotMinimum, gotMaximum = o.RenderLocalBounds()
	if gotMinimum != (mgl32.Vec3{}) || gotMaximum != (mgl32.Vec3{}) {
		t.Fatal("unready render local bounds not zero")
	}
}

func TestS1l14RenderLocalBoundsPreservesDefaultAndLODGridCoordinates(t *testing.T) {
	o := NewVoxelObject()
	o.XBrickMap = c3h8Map([3]int{-3, 1, 4}, [3]int{5, 2, 7})
	wantMinimum, wantMaximum := o.XBrickMap.ComputeAABB()
	gotMinimum, gotMaximum := o.RenderLocalBounds()
	if gotMinimum != wantMinimum || gotMaximum != wantMaximum {
		t.Fatal("default local bounds changed authority grid")
	}
	coarse := c3h8Map([3]int{-1, 0, 2}, [3]int{2, 1, 3})
	if !o.SetRenderLOD2(coarse) {
		t.Fatal("LOD selection")
	}
	wantMinimum, wantMaximum = coarse.ComputeAABB()
	gotMinimum, gotMaximum = o.RenderLocalBounds()
	if gotMinimum != wantMinimum || gotMaximum != wantMaximum {
		t.Fatal("LOD local bounds changed selected grid coordinates")
	}
	var absent *VoxelObject
	gotMinimum, gotMaximum = absent.RenderLocalBounds()
	if gotMinimum != (mgl32.Vec3{}) || gotMaximum != (mgl32.Vec3{}) {
		t.Fatal("nil local bounds not zero")
	}
}
