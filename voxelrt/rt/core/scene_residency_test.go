package core

import (
	"bytes"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestRenderResidencyVisibilityAndBVHs(t *testing.T) {
	for _, transparent := range []bool{false, true} {
		name := "opaque"
		if transparent {
			name = "transparent"
		}
		t.Run(name, func(t *testing.T) {
			scene := NewScene()
			scene.Lights = []Light{testShadowCastingDirectionalLight()}
			obj := NewVoxelObject()
			if !obj.RenderEnabled {
				t.Fatal("new objects must render by default")
			}
			obj.XBrickMap.SetVoxel(0, 0, 0, 1)
			obj.Transform.Position = mgl32.Vec3{0, 0, -10}
			obj.MaterialTable = []Material{DefaultMaterial(), DefaultMaterial()}
			if transparent {
				obj.MaterialTable[1].Transparency = 0.4
			}
			geometry := obj.XBrickMap
			scene.AddObject(obj)
			opts := SceneCommitOptions{
				OcclusionMode: OcclusionConservative, HiZData: []float32{100}, HiZW: 1, HiZH: 1,
				LastViewProj: testSceneViewProj(),
			}
			// Re-enabling must rebuild render BVHs even when geometry/AABB did not change.
			for _, enabled := range []bool{true, false, true, false} {
				obj.RenderEnabled = enabled
				scene.Commit(testSceneFrustumPlanes(), opts)
				if len(scene.Objects) != 1 || scene.Objects[0] != obj || obj.XBrickMap != geometry {
					t.Fatal("visibility changes must retain resident object and geometry")
				}
				want := 0
				if enabled {
					want = 1
				}
				if len(scene.VisibleObjects) != want || len(scene.ShadowObjects) != want {
					t.Fatalf("enabled=%v: visible=%d shadow=%d, want %d", enabled, len(scene.VisibleObjects), len(scene.ShadowObjects), want)
				}
				wantTransparent := 0
				if enabled && transparent {
					wantTransparent = 1
				}
				if len(scene.TransparentVisibleObjects) != wantTransparent {
					t.Fatal("transparent render list disagrees with visibility")
				}
				for name, check := range map[string]struct {
					data      []byte
					populated bool
				}{
					"visible":     {scene.BVHNodesBytes, enabled},
					"transparent": {scene.TransparentBVHNodesBytes, enabled && transparent},
					"shadow":      {scene.ShadowBVHNodesBytes, enabled},
				} {
					// Empty BVHs are zero sentinels; populated BVHs encode renderable leaves.
					if len(check.data) == 0 || bytes.Equal(check.data, make([]byte, len(check.data))) == check.populated {
						t.Fatalf("enabled=%v: %s BVH does not describe its render list", enabled, name)
					}
				}
				if scene.OcclusionStats.FrustumVisible != want || scene.OcclusionStats.HiZEligible != want {
					t.Fatalf("enabled=%v: visibility counts include disabled residents: %+v", enabled, scene.OcclusionStats)
				}
				if !enabled && scene.OcclusionStats != (OcclusionStats{}) {
					t.Fatalf("disabled object contributed Hi-Z statistics: %+v", scene.OcclusionStats)
				}
			}
		})
	}
}

func TestRenderDisabledResidentStillSupportsCPURaycast(t *testing.T) {
	scene := NewScene()
	obj := NewVoxelObject()
	obj.RenderEnabled = false
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj.Transform.Position = mgl32.Vec3{0, 0, -10}
	scene.AddObject(obj)
	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})
	ray := Ray{Origin: mgl32.Vec3{0.5, 0.5, 0}, Direction: mgl32.Vec3{0, 0, -1}}
	hit := scene.Raycast(ray, 100)
	if hit == nil || hit.Object != obj || hit.Coord != ([3]int{0, 0, 0}) {
		t.Fatalf("CPU query must hit hidden resident geometry, got %+v", hit)
	}
	obj.XBrickMap.SetVoxel(0, 0, 0, 0)
	scene.Commit(testSceneFrustumPlanes(), SceneCommitOptions{})
	if hit := scene.Raycast(ray, 100); hit != nil {
		t.Fatal("CPU query must observe authoritative voxel edits")
	}
}
