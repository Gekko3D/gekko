package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"testing"
)

func TestE2b5SynchronousLoadedDeltaHookRestoresBindingAndLease(t *testing.T) {
	f, part, path := e2b2Runtime(t)
	placement := s1gID(0, 0)
	e2b2Save(t, f, path, e2b2Payload(t, part, placement, "body", content.VoxelObjectPayloadBaseDelta, 7, false))
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 0
	var eid EntityId
	var override AssetId
	f.runtime.Config.PlacementHooks = append(f.runtime.Config.PlacementHooks, func(cmd *Commands, context PostSpawnPlacementContext) {
		eid = context.SpawnResult.EntitiesByAssetID["body"]
		if f.runtime.LoadedChunks[ChunkCoord{}] != nil {
			t.Fatal("hook fixture already published loaded chunk")
		}
		if err := EnableManagedVoxelGeometry(cmd, f.assets, eid); err != nil {
			t.Fatal(err)
		}
		e2b3Qualified(t, f, eid, placement, part)
		vmc, ok := voxelModelComponentForEdit(cmd, eid)
		if !ok {
			t.Fatal("pending override missing")
		}
		override = vmc.OverrideGeometry
		if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != override {
			t.Fatal("synchronous restore lacks exact lease")
		}
		if err := ApplyManagedVoxelWrites(cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: 1, Value: 4})); err != nil {
			t.Fatal(err)
		}
	})
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	if eid == 0 || f.runtime.LoadedChunks[ChunkCoord{}] == nil {
		t.Fatal("synchronous restore did not publish")
	}
	e2b3Qualified(t, f, eid, placement, part)
	deltaPath := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	e2b4Delta(t, e2b4DurablePath(t, deltaPath), part, placement, "body", []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {}, {X: 1, Value: 4}})
	p5cAssetPresent(t, f, override, false)
}

func TestE2b5ManagedOrWrongInitialLatticeCannotRestoreLater(t *testing.T) {
	for _, kind := range []string{"already_managed_exact_lease", "initial_lattice"} {
		t.Run(kind, func(t *testing.T) {
			f, part, path := e2b2Runtime(t)
			placement := s1gID(0, 0)
			e2b2Save(t, f, path, e2b2Payload(t, part, placement, "body", content.VoxelObjectPayloadBaseDelta, 7, false))
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := e2b2Body(t, f, placement)
			vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if kind == "already_managed_exact_lease" {
				old := vmc.GeometryAsset()
				source, ok := f.assets.getVoxelGeometry(old)
				if !ok {
					t.Fatal("snapshot missing")
				}
				managed := f.assets.RegisterManagedVoxelGeometry(source.XBrickMap, "")
				vmc.OverrideGeometry = managed
				f.runtime.snapshotGeometryAssets[eid] = streamedGeometryAssetLease{ID: managed, Server: f.assets}
				if !f.assets.DeleteVoxelGeometry(old) {
					t.Fatal("fixture did not retire old lease")
				}
			} else {
				vmc.VoxelResolution = 2
			}
			f.cmd.AddComponents(eid, &vmc)
			override := e2b3Enable(t, f, eid)
			e2b3Fallback(t, f, f.runtime, eid, placement, "body")
			changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, eid)
			if !tracked || len(changes) != 0 {
				t.Fatalf("fallback inherited authored delta history: %v,%v", changes, tracked)
			}
			current, _, ok := currentVoxelMapForEntity(f.cmd, eid)
			if !ok {
				t.Fatal("fallback geometry missing")
			}
			p1dVoxel(t, current, 0, 0)
			p1dVoxel(t, current, -9, 7)
			s3cComponent[VoxelModelComponent](t, f.cmd, eid).VoxelResolution = 1
			if again := e2b3Enable(t, f, eid); again != override {
				t.Fatal("repeated enable replaced own override")
			}
			e2b3Fallback(t, f, f.runtime, eid, placement, "body")
			e2b4Edit(t, f, eid, volume.VoxelWrite{X: 1, Value: 4})
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			p5cAssetPresent(t, f, override, false)
		})
	}
}
