package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestP1dPostCleanPromotionRawPayloadIsPersistenceDirty(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	f.state.GetVoxelObject(eid).XBrickMap.ClearDirty()
	authority, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
	if err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	revision := authority.Revision
	authority.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][0][0] = 7
	if authority.Revision != revision {
		t.Fatal("fixture raw write unexpectedly notified revision")
	}
	current, dirty, exists := currentVoxelMapForEntity(f.cmd, eid)
	if current != authority || !exists || !dirty {
		t.Fatalf("promoted persistence authority=%p dirty=%v exists=%v, want %p dirty", current, dirty, exists, authority)
	}
	p1dVoxel(t, current, 0, 7)
}

func TestP1dPostForeignManagedOverridePromotionForksOwner(t *testing.T) {
	f, owner, _ := p1dFixture(t)
	p1dEnable(t, f, owner)
	p1dApply(t, f, owner, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	ownerOverride := s3cComponent[VoxelModelComponent](t, f.cmd, owner).OverrideGeometry
	foreignModel := f.voxelModel()
	foreignModel.OverrideGeometry = ownerOverride
	borrower := f.cmd.AddEntity(s3cTransform(20), foreignModel)
	f.app.FlushCommands()
	f.sync()
	attached := f.state.GetVoxelObject(borrower).XBrickMap
	raw, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, borrower)
	if err != nil {
		t.Fatal(err)
	}
	if raw != attached {
		t.Fatal("foreign override promotion replaced current borrower pointer")
	}
	f.app.FlushCommands()
	borrowerOverride := s3cComponent[VoxelModelComponent](t, f.cmd, borrower).OverrideGeometry
	if borrowerOverride == (AssetId{}) || borrowerOverride == ownerOverride {
		t.Fatal("borrower promotion retained another entity's managed override")
	}
	raw.SetVoxel(0, 0, 0, 9)
	f.sync()
	p1dChanges(t, f, owner, volume.VoxelWrite{Value: 2})
	current, _, _ := currentVoxelMapForEntity(f.cmd, owner)
	p1dVoxel(t, current, 0, 2)
	p1dVoxel(t, f.state.GetVoxelObject(owner).XBrickMap, 0, 2)
	p1dVoxel(t, f.state.GetVoxelObject(borrower).XBrickMap, 0, 9)
}

func TestP1dPostIneligibleSourceAttachmentRejectsWithoutDataLoss(t *testing.T) {
	f, retained, _ := p1dFixture(t)
	s3cComponent[VoxelModelComponent](t, f.cmd, retained).RetainRendererGeometry = true
	sibling := f.add(20)
	f.app.FlushCommands()
	f.sync()
	if f.state.GetVoxelObject(retained) != nil {
		t.Fatal("retained renderer attached sealed managed source")
	}
	_, _, cache := newVoxelPhysicsPrecalcTestHarness()
	VoxPhysicsPreCalcSystem(f.cmd, f.server, f.state, cache)
	f.app.FlushCommands()
	if pm, ok := f.cmd.GetComponent(retained, reflect.TypeOf(PhysicsModel{})).(*PhysicsModel); ok && pm != nil && pm.Grid != nil {
		t.Fatal("ineligible registered source acquired collision qualification")
	}
	// Save selection must preserve unavailable source data rather than treating it
	// as a removed entity or an empty full snapshot.
	current, _, exists := currentVoxelMapForEntity(f.cmd, retained)
	if !exists || current == nil || current.GetVoxelCount() != 2 {
		t.Fatal("ineligible attachment discarded source persistence data")
	}
	p1dVoxel(t, current, 0, 1)
	p1dVoxel(t, current, 1, 1)
	if s3cComponent[VoxelModelComponent](t, f.cmd, retained).OverrideGeometry != (AssetId{}) {
		t.Fatal("attachment created unauthorized override")
	}
	p1dEnable(t, f, sibling)
	f.app.FlushCommands()
	f.sync()
	p1dChanges(t, f, sibling)
	p1dVoxel(t, f.state.GetVoxelObject(sibling).XBrickMap, 0, 1)
}

func TestP1dPostUnauthorizedRendererReplacementIsRepaired(t *testing.T) {
	for _, operation := range []string{"sync", "apply"} {
		t.Run(operation, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			f.app.FlushCommands()
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			poison := volume.NewXBrickMap()
			poison.SetVoxel(0, 0, 0, 9)
			poison.SetVoxel(5, 0, 0, 9)
			obj.XBrickMap = poison
			want := uint8(1)
			if operation == "apply" {
				p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
				want = 2
				p1dVoxel(t, poison, 0, 9)
			}
			f.app.FlushCommands()
			f.sync()
			if obj.XBrickMap == poison {
				t.Fatal("ordinary synchronization retained unauthorized replacement")
			}
			p1dVoxel(t, obj.XBrickMap, 0, want)
			p1dVoxel(t, obj.XBrickMap, 1, 1)
			p1dVoxel(t, obj.XBrickMap, 5, 0)
			if operation == "apply" {
				p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2})
			} else {
				p1dChanges(t, f, eid)
			}
		})
	}
}

func TestP1dPostSourceOnlySiblingSphereAndPromotionIsolation(t *testing.T) {
	for _, operation := range []string{"sphere", "promote"} {
		t.Run(operation, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			sibling := f.add(20)
			f.app.FlushCommands()
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			if operation == "sphere" {
				center := obj.Transform.ObjectToWorld().Mul4x1(mgl32.Vec3{0.5, 0.5, 0.5}.Vec4(1)).Vec3()
				f.state.VoxelSphereEdit(eid, center, 0.25, 3)
			} else {
				raw, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
				if err != nil {
					t.Fatal(err)
				}
				if raw != obj.XBrickMap {
					t.Fatal("source-only promotion lost exact runtime pointer")
				}
				raw.SetVoxel(0, 0, 0, 3)
			}
			f.app.FlushCommands()
			f.sync()
			firstModel := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if firstModel.OverrideGeometry == (AssetId{}) || firstModel.OverrideGeometry == f.model {
				t.Fatal("source-only edit failed to create private entity authority")
			}
			p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 3)
			p1dVoxel(t, f.state.GetVoxelObject(sibling).XBrickMap, 0, 1)
			if _, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, sibling); tracked {
				t.Fatal("unenabled sibling inherited entity tracking")
			}
			current, _, _ := currentVoxelMapForEntity(f.cmd, sibling)
			p1dVoxel(t, current, 0, 1)
		})
	}
}
