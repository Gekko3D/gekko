package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestP1dPivotManagedCenterPreservesAuthoredPivotAndCurrentCollisionBounds(t *testing.T) {
	for _, path := range []string{"precalc", "fallback"} {
		t.Run(path, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			s3cComponent[VoxelModelComponent](t, f.cmd, eid).PivotMode = PivotModeCenter
			f.cmd.AddComponents(eid, ColliderComponent{})
			f.app.FlushCommands()
			p1dEnable(t, f, eid)
			p1dApply(t, f, eid, volume.VoxelWrite{X: 4, Value: 1})
			f.app.FlushCommands()
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			if obj.Transform.Pivot != (mgl32.Vec3{1, 0.5, 0.5}) {
				t.Fatalf("renderer moved authored center pivot to %v", obj.Transform.Pivot)
			}
			tr := s3cComponent[TransformComponent](t, f.cmd, eid)
			vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if path == "precalc" {
				_, _, cache := newVoxelPhysicsPrecalcTestHarness()
				VoxPhysicsPreCalcSystem(f.cmd, f.server, f.state, cache)
				f.app.FlushCommands()
			} else {
				model, ok := buildFallbackPhysicsModelFromVoxel(f.server, tr, vmc)
				if !ok {
					t.Fatal("managed fallback collision unavailable")
				}
				if model.CenterOffset != (mgl32.Vec3{2.5, 0.5, 0.5}) || len(model.Boxes) != 1 || model.Boxes[0].HalfExtents != (mgl32.Vec3{2.5, 0.5, 0.5}) {
					t.Fatalf("fallback ignored edited payload bounds: %+v", model)
				}
			}
			snapshot, _ := collectPhysicsSnapshot(f.cmd, &Time{Dt: 1.0 / 60}, NewPhysicsWorld(), f.server)
			if len(snapshot.Entities) != 1 || snapshot.Entities[0].Eid != eid {
				t.Fatal("managed body missing from physics snapshot")
			}
			body := snapshot.Entities[0]
			if body.Model.CenterOffset != (mgl32.Vec3{2.5, 0.5, 0.5}) {
				t.Fatalf("collision center=%v, want current payload center", body.Model.CenterOffset)
			}
			wantPosition := s3cComponent[TransformComponent](t, f.cmd, eid).Position.Add(mgl32.Vec3{1.5, 0, 0})
			if body.Pos != wantPosition {
				t.Fatalf("physics position=%v, want %v preserving authored render pivot", body.Pos, wantPosition)
			}
		})
	}
}
