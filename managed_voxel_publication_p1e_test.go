package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestP1eRuntimeAuthorityPublicationReusesImmutableUntouchedSectors(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 1)
	source.SetVoxel(128, 0, 0, 1)
	f.model = f.server.RegisterManagedVoxelGeometry(source, "p1e-publication")
	eid := f.add(0, RigidBodyComponent{Mass: 1})
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	geometry := s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry
	header, _ := f.server.getVoxelGeometry(geometry)
	originalMin, originalMax := header.LocalMin, header.LocalMax
	f.sync()
	derivative := f.state.GetVoxelObject(eid).XBrickMap
	previous, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	previousID, previousRevision := previous.ID, previous.Revision
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	f.sync()
	current, dirty, exists := currentVoxelMapForEntity(f.cmd, eid)
	if !exists || !dirty || current == previous || current.ID == previousID || current.Revision <= previousRevision {
		t.Fatal("tracked edit did not publish a new authoritative snapshot")
	}
	if current.Sectors[[3]int{4, 0, 0}] != previous.Sectors[[3]int{4, 0, 0}] || current.Sectors[[3]int{}] == previous.Sectors[[3]int{}] {
		t.Fatal("runtime publication must reuse immutable untouched sectors and copy edited sectors")
	}
	if f.state.GetVoxelObject(eid).XBrickMap != derivative {
		t.Fatal("authority publication replaced renderer derivative")
	}
	p1dVoxel(t, derivative, 0, 2)
	p1dVoxel(t, previous, 0, 1)
	if previous.Revision != previousRevision || previous.ID != previousID {
		t.Fatal("publication mutated previous snapshot metadata")
	}
	_, _, cache := newVoxelPhysicsPrecalcTestHarness()
	VoxPhysicsPreCalcSystem(f.cmd, f.server, f.state, cache)
	f.app.FlushCommands()
	grid := mustPhysicsModel(t, f.cmd, eid).Grid
	if grid == nil {
		t.Fatal("missing authoritative collision grid")
	}
	if present, value := grid.GetVoxel(0, 0, 0); !present || value != 2 {
		t.Fatal("collision missed incrementally published edit")
	}
	p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2})
	p1dApply(t, f, eid, volume.VoxelWrite{X: 129, Value: 3})
	header, _ = f.server.getVoxelGeometry(geometry)
	if header.LocalMin != originalMin || header.LocalMax != originalMax {
		t.Fatal("bounds-expanding publication changed authored pivot header")
	}
	p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2}, volume.VoxelWrite{X: 129, Value: 3})
	p1dVoxel(t, previous, 128, 1)
	p1dVoxel(t, current, 128, 1)
	p1dVoxel(t, previous, 129, 0)
	p1dVoxel(t, current, 129, 0)
	if present, value := grid.GetVoxel(128, 0, 0); !present || value != 1 {
		t.Fatal("later edit mutated previously published collision snapshot")
	}
	if present, _ := grid.GetVoxel(129, 0, 0); present {
		t.Fatal("later bounds expansion mutated prior collision snapshot")
	}
}
