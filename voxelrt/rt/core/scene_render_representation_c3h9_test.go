package core

import (
	"bytes"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestC3h9SceneCullsAndBuildsAllPassesFromRenderBounds(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
	obj.MaterialTable = []Material{DefaultMaterial(), {Transmission: .25}}
	scene.AddObject(obj)
	planes := [6]mgl32.Vec4{{-1, 0, 0, .75}}
	scene.Commit(planes, SceneCommitOptions{})
	fullBounds := obj.WorldAABB
	fullValue := *fullBounds
	if len(scene.VisibleObjects) != 0 {
		t.Fatal("fixture full geometry should be outside x<=.75")
	}
	if !obj.SetRenderLOD2(c3h8Map([3]int{0, 0, 0})) {
		t.Fatal("setup")
	}
	scene.Commit(planes, SceneCommitOptions{})
	if len(scene.VisibleObjects) != 1 || scene.VisibleObjects[0] != obj || len(scene.TransparentVisibleObjects) != 1 || len(scene.ShadowObjects) != 1 {
		t.Fatal("render expansion not used consistently by visible/transparent/shadow passes")
	}
	if obj.WorldAABB != fullBounds || *obj.WorldAABB != fullValue {
		t.Fatal("render commit replaced CPU authoritative bounds")
	}
	if len(scene.BVHNodesBytes) == 0 || len(scene.TransparentBVHNodesBytes) == 0 || len(scene.ShadowBVHNodesBytes) == 0 {
		t.Fatal("selected representation missing pass BVH")
	}
}

func TestC3h9SceneRenderBoundsDirtyIndependentOfFullBounds(t *testing.T) {
	scene := NewScene()
	scene.Lights = []Light{testShadowCastingDirectionalLight()}
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
	obj.MaterialTable = []Material{DefaultMaterial(), {Transmission: .25}}
	scene.AddObject(obj)
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	full := obj.WorldAABB
	legacy := [3][]byte{bytes.Clone(scene.BVHNodesBytes), bytes.Clone(scene.TransparentBVHNodesBytes), bytes.Clone(scene.ShadowBVHNodesBytes)}
	before := [3]uint64{scene.visibleBVHRevision, scene.transparentBVHRevision, scene.shadowBVHRevision}
	coarse := c3h8Map([3]int{0, 0, 0})
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	after := [3]uint64{scene.visibleBVHRevision, scene.transparentBVHRevision, scene.shadowBVHRevision}
	selected := [3][]byte{bytes.Clone(scene.BVHNodesBytes), bytes.Clone(scene.TransparentBVHNodesBytes), bytes.Clone(scene.ShadowBVHNodesBytes)}
	for i := range before {
		if after[i] <= before[i] || bytes.Equal(legacy[i], selected[i]) {
			t.Fatal("render selection didn't rebuild changed pass bounds", i)
		}
	}
	if obj.WorldAABB != full {
		t.Fatal("render dirty handling overwrote full bounds")
	}
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	stable := [3]uint64{scene.visibleBVHRevision, scene.transparentBVHRevision, scene.shadowBVHRevision}
	if stable != after {
		t.Fatal("stable render selection unnecessarily rebuilds BVHs")
	}
	obj.ClearRenderRepresentation()
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	restored := [3][]byte{scene.BVHNodesBytes, scene.TransparentBVHNodesBytes, scene.ShadowBVHNodesBytes}
	for i := range legacy {
		if !bytes.Equal(restored[i], legacy[i]) {
			t.Fatal("clear didn't restore full pass bounds", i)
		}
	}
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("setup")
	}
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	coarse.SetVoxel(1, 0, 0, 7)
	scene.Commit([6]mgl32.Vec4{}, SceneCommitOptions{})
	if len(scene.VisibleObjects) != 0 || len(scene.TransparentVisibleObjects) != 0 || len(scene.ShadowObjects) != 0 {
		t.Fatal("invalid active representation entered GPU passes")
	}
	if obj.WorldAABB != full || len(scene.Objects) != 1 || scene.Objects[0] != obj {
		t.Fatal("invalid render selection changed full CPU object")
	}
	hit := scene.Raycast(Ray{Origin: mgl32.Vec3{-2, .5, .5}, Direction: mgl32.Vec3{1, 0, 0}}, 10)
	if hit == nil || hit.Coord != [3]int{1, 0, 0} {
		t.Fatal("invalid rendering disabled authoritative CPU picking")
	}
}

func TestC3h9SpecialLatticeRejectsLODSelectionAndLaterTags(t *testing.T) {
	tags := map[string]func(*VoxelObject){"terrain": func(o *VoxelObject) { o.IsTerrainChunk = true }, "planet": func(o *VoxelObject) { o.IsPlanetTile = true }, "adjacency-group": func(o *VoxelObject) { o.VoxelAdjacencyGroupID = 1 }, "terrain-group": func(o *VoxelObject) { o.TerrainGroupID = 1 }, "planet-group": func(o *VoxelObject) { o.PlanetTileGroupID = 1 }}
	for name, tag := range tags {
		t.Run(name, func(t *testing.T) {
			obj := NewVoxelObject()
			obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
			coarse := c3h8Map([3]int{0, 0, 0})
			tag(obj)
			if obj.SetRenderLOD2(coarse) || obj.RenderRepresentationValid() || obj.RenderVoxelMap() != obj.XBrickMap {
				t.Fatal("special lattice accepted factor2 or lost default map")
			}
			obj = NewVoxelObject()
			obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
			if !obj.SetRenderLOD2(coarse) {
				t.Fatal("setup")
			}
			tag(obj)
			if obj.RenderRepresentationValid() || obj.RenderVoxelMap() != nil || obj.RenderWorldBounds() != nil {
				t.Fatal("special tag added later did not invalidate active representation")
			}
		})
	}
}

func TestC3h9HiZUsesExpandedRenderBoundsAfterWarmup(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{0, 0, 0})
	obj.Transform.Position = mgl32.Vec3{0, 0, -4}
	scene.AddObject(obj)
	hiz := make([]float32, 16)
	for i := range hiz {
		hiz[i] = 2.5
	}
	options := SceneCommitOptions{OcclusionMode: OcclusionConservative, HiZData: hiz, HiZW: 4, HiZH: 4, LastViewProj: testSceneViewProj(), DepthSlack: .1}
	frames := occlusionWarmupFrames + occlusionHysteresisFrames + 2
	for i := 0; i < frames; i++ {
		scene.Commit(testSceneFrustumPlanes(), options)
	}
	if len(scene.VisibleObjects) != 0 || !IsOccluded(*obj.WorldAABB, hiz, 4, 4, testSceneViewProj(), .1) {
		t.Fatal("full geometry fixture should be occluded after warmup")
	}
	if !obj.SetRenderLOD2(c3h8Map([3]int{0, 0, 0})) {
		t.Fatal("setup")
	}
	renderBounds := obj.RenderWorldBounds()
	if renderBounds == nil || IsOccluded(*renderBounds, hiz, 4, 4, testSceneViewProj(), .1) {
		t.Fatal("expanded coarse bounds should pass HiZ depth")
	}
	for i := 0; i < frames; i++ {
		scene.Commit(testSceneFrustumPlanes(), options)
		if len(scene.VisibleObjects) != 1 || scene.VisibleObjects[0] != obj {
			t.Fatal("HiZ incorrectly tested full bounds for selected render geometry", i)
		}
	}
}

func TestC3h9ScenePointShadowAABBVolumeDistance(t *testing.T) {
	bounds := [2]mgl32.Vec3{{0, 0, 0}, {2, 2, 2}}
	for _, c := range []struct {
		name     string
		position mgl32.Vec3
		radius   float32
		want     bool
	}{
		{"inside", mgl32.Vec3{.25, .5, .5}, .2, true},
		{"outside", mgl32.Vec3{3, 1, 1}, .5, false},
		{"tangent", mgl32.Vec3{3, 1, 1}, 1, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := intersectsScenePointShadowVolume(bounds, scenePointShadowCullVolume{Position: c.position, Range: c.radius}); got != c.want {
				t.Fatalf("intersection got%v want%v", got, c.want)
			}
		})
	}
}
