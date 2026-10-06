package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestS1l14EngineQualifiedFeedKeepsHiddenGPUStageAndCPUContentCoherent(t *testing.T) {
	f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(31, 32, 80))
	m.SetManagedGeometryFrameBudget(gpu.DefaultManagedGeometryFrameBudget())
	scene := f.state.RtApp.Scene
	scene.AddObject(obj)
	for i := 0; i < 8; i++ {
		m.PrepareManagedGeometryFrame(scene)
	}
	before, ok := m.ManagedGeometryGPUStatus(obj)
	if !ok || before.StageReady || before.CurrentInput.SameSource(before.CurrentInput) || obj.RenderVoxelMap() != nil {
		t.Fatal("headless prepare published without native coverage")
	}
	frozen, _ := m.ManagedGeometryStage(obj)
	previous, ok := obj.CurrentManagedGeometryGeneration(in)
	if !ok {
		t.Fatal("scalar qualification")
	}
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31, Y: 16, Z: 16, Value: 3})
	current, ok := obj.CurrentManagedGeometryGeneration(in)
	if !ok || current != previous+1 {
		t.Fatal("engine edit did not publish exactly one generation")
	}
	// Engine complete batches must reconcile target plus seam halo sparsely.
	if attempts := m.ServiceManagedGeometryReconciliation(obj, in, 100); attempts != 2 {
		t.Fatalf("qualified seam feed attempts %d want 2", attempts)
	}
	s1l13RootCoherent(t, m, obj, true)
	s1l13RootCurrent(t, m, obj, in, 0)
	s1l13RootCurrent(t, m, obj, in, 1)
	for i := 0; i < 8; i++ {
		m.PrepareManagedGeometryFrame(scene)
	}
	after, ok := m.ManagedGeometryGPUStatus(obj)
	if !ok || after.StageReady || after.CurrentInput.SameSource(after.CurrentInput) || obj.RenderVoxelMap() != nil {
		t.Fatal("edit feed bypassed hidden GPU/material certificate")
	}
	if !before.StageInput.SameSource(after.StageInput) {
		t.Fatal("ordinary content edit replaced producer identity")
	}
	if frozen.Len() != 3 {
		t.Fatal("GPU consumer changed frozen CPU topology")
	}
	scene.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	if len(scene.VisibleObjects)+len(scene.ShadowObjects) != 0 {
		t.Fatal("unuploaded managed stage entered frame passes")
	}
}

func TestS1l14EngineEditFeedDoesNotTurnPendingBindingIntoPublication(t *testing.T) {
	for _, kind := range []string{"expose", "pending-remove", "pending-model"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, obj, m, in := s1l11FeedFixture(t, s1l11Source(16))
			m.SetManagedGeometryFrameBudget(gpu.DefaultManagedGeometryFrameBudget())
			scene := f.state.RtApp.Scene
			scene.AddObject(obj)
			m.PrepareManagedGeometryFrame(scene)
			switch kind {
			case "expose":
				vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
				f.server.GetVoxelGeometry(vmc.OverrideGeometry)
			case "pending-remove":
				f.cmd.RemoveEntity(eid)
			case "pending-model":
				vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
				vmc.OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 2)
				f.cmd.AddComponents(eid, vmc)
			}
			if _, ok := obj.CurrentManagedGeometryGeneration(in); ok {
				t.Fatal("pending binding qualified producer")
			}
			m.PrepareManagedGeometryFrame(scene)
			if status, ok := m.ManagedGeometryGPUStatus(obj); ok && status.StageReady {
				t.Fatal("invalid engine binding qualified GPU publication")
			}
		})
	}
}
