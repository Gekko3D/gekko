package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestS1iBridgeAdmissionPolicyFollowsStreamedHiddenOwner(t *testing.T) {
	if core.NewVoxelObject().VoxelGPUAdmissionOptional {
		t.Fatal("ordinary core objects must default to required admission")
	}
	f := newStreamedVoxelFixture(t)
	f.sync()
	obj := f.state.GetVoxelObject(f.entity)
	if obj == nil || !obj.VoxelGPUAdmissionOptional || obj.RenderEnabled {
		t.Fatal("hidden streamed selected geometry did not opt into admission")
	}
	f.cmd.RemoveComponents(f.entity, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	if f.state.GetVoxelObject(f.entity) != obj || obj.VoxelGPUAdmissionOptional || !obj.RenderEnabled {
		t.Fatal("revealed streamed owner retained optional policy or changed identity")
	}
	f.cmd.AddComponents(f.entity, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	if !obj.VoxelGPUAdmissionOptional {
		t.Fatal("rehidden streamed owner did not regain optional policy")
	}
	f.cmd.RemoveComponents(f.entity, &StreamedVoxelRenderComponent{}, &VoxelRenderHiddenComponent{})
	f.flush()
	f.sync()
	if obj := f.state.GetVoxelObject(f.entity); obj == nil || obj.VoxelGPUAdmissionOptional || !obj.RenderEnabled {
		t.Fatal("ordinary owner retained stale optional metadata")
	}
}

func TestS1iCompiledLocalWaitRetainsRequiredAdmission(t *testing.T) {
	for _, streamed := range []bool{false, true} {
		f := c3h13cBridge(t, streamed)
		f.sync()
		obj := f.state.GetVoxelObject(f.entity)
		if obj == nil || obj.RenderEnabled || obj.RenderVoxelMap() == obj.XBrickMap {
			t.Fatal("fixture failed to establish compiled coarse startup wait")
		}
		if obj.VoxelGPUAdmissionOptional {
			t.Fatal("compiled local readiness wait was mistaken for explicit streamed hiding")
		}
		c3h13cUploaded(f.state, obj, obj.RenderVoxelMap())
		f.state.refreshCompiledAssetLODStatuses()
		f.sync()
		if !obj.RenderEnabled || obj.VoxelGPUAdmissionOptional {
			t.Fatal("startup completion did not preserve required admission")
		}
	}
}
