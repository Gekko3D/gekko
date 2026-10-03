package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestP1dAliasForeignOverrideRawHelperClonesBorrower(t *testing.T) {
	f, owner, _ := p1dFixture(t)
	p1dEnable(t, f, owner)
	p1dApply(t, f, owner, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	ownerOverride := s3cComponent[VoxelModelComponent](t, f.cmd, owner).OverrideGeometry
	model := f.voxelModel()
	model.OverrideGeometry = ownerOverride
	borrower := f.cmd.AddEntity(s3cTransform(20), model)
	f.app.FlushCommands()
	_, borrowerOverride, raw, err := EnsureEditableVoxelGeometry(f.cmd, f.server, borrower)
	if err != nil {
		t.Fatal(err)
	}
	if borrowerOverride == (AssetId{}) || borrowerOverride == ownerOverride {
		t.Fatal("raw helper exposed another entity's managed override")
	}
	p1dVoxel(t, raw, 0, 2)
	raw.SetVoxel(0, 0, 0, 9)
	f.app.FlushCommands()
	if s3cComponent[VoxelModelComponent](t, f.cmd, borrower).OverrideGeometry != borrowerOverride {
		t.Fatal("raw helper failed to commit borrower override")
	}
	p1dChanges(t, f, owner, volume.VoxelWrite{Value: 2})
	current, _, _ := currentVoxelMapForEntity(f.cmd, owner)
	p1dVoxel(t, current, 0, 2)
	current, _, _ = currentVoxelMapForEntity(f.cmd, borrower)
	p1dVoxel(t, current, 0, 9)
}

func TestP1dAliasSphereOnExposedInheritedSourceClonesBorrower(t *testing.T) {
	f, borrower, _ := p1dFixture(t)
	sibling := f.add(20)
	f.app.FlushCommands()
	asset, ok := f.server.GetVoxelGeometry(f.model)
	if !ok {
		t.Fatal("missing source asset")
	}
	rawSource := asset.XBrickMap
	rawSource.SetVoxel(0, 0, 0, 2)
	f.sync()
	obj := f.state.GetVoxelObject(borrower)
	center := obj.Transform.ObjectToWorld().Mul4x1(mgl32.Vec3{0.5, 0.5, 0.5}.Vec4(1)).Vec3()
	f.state.VoxelSphereEdit(borrower, center, 0.25, 3)
	f.app.FlushCommands()
	f.sync()
	if override := s3cComponent[VoxelModelComponent](t, f.cmd, borrower).OverrideGeometry; override == (AssetId{}) || override == f.model {
		t.Fatal("sphere edited inherited exposed source without borrower override")
	}
	p1dVoxel(t, rawSource, 0, 2)
	p1dVoxel(t, f.state.GetVoxelObject(sibling).XBrickMap, 0, 2)
	p1dVoxel(t, f.state.GetVoxelObject(borrower).XBrickMap, 0, 3)
	current, _, _ := currentVoxelMapForEntity(f.cmd, borrower)
	p1dVoxel(t, current, 0, 3)
}
