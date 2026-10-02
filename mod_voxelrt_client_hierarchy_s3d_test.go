package gekko

import "testing"

func TestS3dHierarchyDescendantReachesRealVoxelBridgeSameFrame(t *testing.T) {
	f := newS3cVoxelFixture(t)
	root := f.cmd.AddEntity(s3cTransform(1))
	other := f.cmd.AddEntity(s3cTransform(20))
	middle := s3dChild(f.cmd, root, 2)
	leaf := f.cmd.AddEntity(s3dWorld(-100), s3dLocal(3), Parent{Entity: middle}, f.voxelModel())
	f.app.FlushCommands()
	TransformHierarchySystem(f.cmd)
	f.sync()
	obj := s3cResident(t, f, leaf, 6, f.model)
	s3dStats(t, f.cmd, 1, 2, 4)
	s3dIdle(t, f.cmd)
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	s3cComponent[TransformComponent](t, f.cmd, root).Position[0] = 5
	TransformHierarchySystem(f.cmd)
	f.sync()
	if s3cResident(t, f, leaf, 10, f.model) != obj {
		t.Fatal("ancestor motion replaced renderer identity")
	}
	s3dStats(t, f.cmd, 1, 4, 4)
	s3cComponent[LocalTransformComponent](t, f.cmd, middle).Position[0] = 4
	TransformHierarchySystem(f.cmd)
	f.sync()
	s3cResident(t, f, leaf, 12, f.model)
	s3dStats(t, f.cmd, 1, 6, 4)
	s3cComponent[Parent](t, f.cmd, middle).Entity = other
	TransformHierarchySystem(f.cmd)
	f.sync()
	s3cResident(t, f, leaf, 27, f.model)
	if f.cmd.TransformHierarchyStats().TopologyBuildCount != 2 {
		t.Fatal("reparent did not rebuild hierarchy resolution")
	}
	s3dIdle(t, f.cmd)
	f.sync()
	s3cInventory(t, f.state, 1, 1)
	if f.state.GetVoxelObject(leaf) != obj {
		t.Fatal("reparent replaced renderer identity")
	}
}
