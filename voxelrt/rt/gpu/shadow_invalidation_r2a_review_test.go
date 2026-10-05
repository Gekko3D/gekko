package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestR2aExternalRevisionDuringTrackedUpload(t *testing.T) {
	m, scene, camera := r2aFixture(t, core.LightTypeSpot)
	scene.Objects[0].XBrickMap.DirtyBricks[[6]int{}] = true
	calls := 0
	m.serviceVoxelUploads(scene, func(w voxelUploadWork) bool {
		calls++
		// An external producer can publish an unattributed write while this
		// tracked upload executes. Both revisions must remain observable.
		m.VoxelUploadRevision++
		return true
	})
	if calls != 1 || m.VoxelUploadBytes == 0 {
		t.Fatal("fixture did not execute one successful tracked upload")
	}
	r2aCommit(m, scene, camera)
	r2aAssert(t, m, scene, camera, r2aWarmFrame+1, []int{0, 1})
}
