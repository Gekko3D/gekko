package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestR2aMaterialRefreshPreservesUnchangedOpacity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    bool
		succeed bool
		invalid []int
	}{
		{name: "identical buffer refresh", succeed: true},
		{name: "in-place opacity edit", edit: true, succeed: true, invalid: []int{0}},
		{name: "failed opacity write", edit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, scene, camera := r2aFixture(t, core.LightTypeSpot)
			obj := scene.Objects[0]
			geometryRevision := obj.XBrickMap.Revision
			if tc.edit {
				obj.MaterialTable[1].Transparency = 1
			}
			m.MaterialBufferGeneration++
			calls := 0
			m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
				calls++
				if w.kind != voxelUploadMaterial {
					t.Fatal("fixture requested geometry work during material refresh")
				}
				return tc.succeed
			})
			if calls != 2 || obj.XBrickMap.Revision != geometryRevision {
				t.Fatal("fixture must attempt both palettes without editing geometry")
			}
			if tc.succeed && (m.VoxelMaterialsUploaded != 2 || m.VoxelUploadBytes == 0) {
				t.Fatal("fixture did not successfully refresh both palettes")
			}
			if !tc.succeed && m.VoxelUploadBytes != 0 {
				t.Fatal("failed material execution published written bytes")
			}
			r2aCommit(m, scene, camera)
			r2aAssert(t, m, scene, camera, r2aWarmFrame+1, tc.invalid)
		})
	}
}
