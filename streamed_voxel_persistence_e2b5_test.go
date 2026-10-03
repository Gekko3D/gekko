package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestE2b5SavedDeltaReloadRestoresOriginalTracking(t *testing.T) {
	f, part, eid := e2b3Pristine(t)
	e2b3Enable(t, f, eid)
	e2b4Edit(t, f, eid, volume.VoxelWrite{}, volume.VoxelWrite{X: -9, Value: 7})
	e2b4Depart(f)
	e2b4Unloaded(t, f)
	e2b4Delta(t, e2b4Durable(t, f), part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {}})
	f.hooks[s1gID(0, 0)] = 0
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid = e2b2Body(t, f, s1gID(0, 0))
	old := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
	geometry, ok := f.assets.getVoxelGeometry(old)
	if !ok {
		t.Fatal("loaded snapshot missing")
	}
	for _, sector := range geometry.XBrickMap.Sectors {
		for _, brick := range sector.PackedBricks {
			brick.PrecomputedAux = []byte{7, 8, 9}
		}
	}
	override := e2b3Enable(t, f, eid)
	e2b3Qualified(t, f, eid, s1gID(0, 0), part)
	changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, eid)
	if !tracked || !reflect.DeepEqual(changes, []volume.VoxelWrite{{X: -9, Value: 7}, {}}) {
		t.Fatalf("restored assignments=%v,%v", changes, tracked)
	}
	restored, ok := f.assets.getVoxelGeometry(override)
	if !ok || restored.LocalMin != geometry.LocalMin || restored.LocalMax != geometry.LocalMax {
		t.Fatal("restore lost current snapshot header")
	}
	for _, sector := range restored.XBrickMap.Sectors {
		for _, brick := range sector.PackedBricks {
			if !reflect.DeepEqual(brick.PrecomputedAux, []byte{7, 8, 9}) {
				t.Fatal("restore lost current auxiliary metadata")
			}
		}
	}
	p5cAssetPresent(t, f, old, false)
	e2b4Edit(t, f, eid, volume.VoxelWrite{Value: 1}, volume.VoxelWrite{X: 1, Value: 4})
	path := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	payload := e2b4DurablePath(t, path)
	e2b4Delta(t, payload, part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {X: 1, Value: 4}})
	base, lattice, _ := e2b1Canonical(t, part)
	resolved, err := content.ResolveVoxelObjectPayload(payload, base, lattice, s1gID(0, 0), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	current := XBrickMapFromVoxelObjectSnapshot(resolved)
	p1dVoxel(t, current, 0, 1)
	p1dVoxel(t, current, 1, 4)
	p1dVoxel(t, current, -9, 7)
	p5cAssetPresent(t, f, override, false)
}

func TestE2b5RestorationChecksCurrentPayloadAndExactSnapshotOwnership(t *testing.T) {
	for _, kind := range []string{"matching_replacement", "nul_tuple", "changed_delta", "legacy", "full", "missing_file", "foreign_lease", "raw_drift", "pending_ref", "missing_ref"} {
		t.Run(kind, func(t *testing.T) {
			f, part, path := e2b2Runtime(t)
			placement, item := s1gID(0, 0), "body"
			if kind == "nul_tuple" {
				placement, item = "P\x00nested", "body\x00nested"
				f.runtime.PlacementsByChunk[ChunkCoord{}][0].PlacementID = placement
				assetPath := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
				asset, err := content.LoadAsset(assetPath)
				if err != nil {
					t.Fatal(err)
				}
				part.ID = item
				asset.Parts = append(asset.Parts, part)
				if err := content.SaveAsset(assetPath, asset); err != nil {
					t.Fatal(err)
				}
			}
			payload := e2b2Payload(t, part, placement, item, content.VoxelObjectPayloadBaseDelta, 7, false)
			e2b2Save(t, f, path, payload)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := placementItemEntityByIDForStreamedTest(f.cmd, placement, item)
			if eid == 0 {
				t.Fatal("loaded item missing")
			}
			source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
			geometry, ok := f.assets.getVoxelGeometry(source)
			if !ok {
				t.Fatal("loaded snapshot missing")
			}
			var foreign AssetId
			expected := uint8(7)
			switch kind {
			case "matching_replacement":
				payload.Voxels = append(payload.Voxels, content.VoxelObjectVoxelDef{X: 1, Value: 1})
				if _, err := content.SaveVoxelObjectPayload(path, payload, nil); err != nil {
					t.Fatal(err)
				}
			case "changed_delta":
				payload.Voxels[1].Value = 9
				if _, err := content.SaveVoxelObjectPayload(path, payload, nil); err != nil {
					t.Fatal(err)
				}
			case "legacy":
				if err := content.SaveVoxelObjectSnapshot(path, VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap)); err != nil {
					t.Fatal(err)
				}
			case "full":
				payload.Mode = content.VoxelObjectPayloadFull
				payload.BaseIdentity = ""
				payload.Voxels = VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap).Voxels
				if _, err := content.SaveVoxelObjectPayload(path, payload, nil); err != nil {
					t.Fatal(err)
				}
			case "missing_file":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "foreign_lease":
				vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
				foreign = f.assets.RegisterSharedVoxelGeometry(geometry.XBrickMap, "")
				vmc.OverrideGeometry = foreign
				f.cmd.AddComponents(eid, &vmc)
			case "raw_drift":
				geometry.XBrickMap.SetVoxel(-9, 0, 0, 6)
				expected = 6
			case "pending_ref":
				ref := *s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid)
				ref.AssetID = "different"
				f.cmd.AddComponents(eid, &ref)
			case "missing_ref":
				f.cmd.RemoveComponents(eid, AuthoredLevelItemRefComponent{})
			}
			override := e2b3Enable(t, f, eid)
			qualified := kind == "matching_replacement" || kind == "nul_tuple"
			identity, lattice, ok := managedVoxelPersistenceBase(f.cmd, f.assets, f.runtime, eid, placement, item)
			if qualified {
				_, wantLattice, wantIdentity := e2b1Canonical(t, part)
				if !ok || identity != wantIdentity || lattice != wantLattice {
					t.Fatalf("valid restored binding=%q,%+v,%v", identity, lattice, ok)
				}
				changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, eid)
				if !tracked || !reflect.DeepEqual(changes, []volume.VoxelWrite{{X: -9, Value: 7}, {}}) {
					t.Fatalf("restored changes=%v,%v", changes, tracked)
				}
			} else if ok || identity != "" || lattice != (content.VoxelObjectLatticeDef{}) {
				t.Fatal("untrusted restoration acquired authored binding")
			}
			current, _, exists := currentVoxelMapForEntity(f.cmd, eid)
			if !exists {
				t.Fatal("fallback lost geometry")
			}
			p1dVoxel(t, current, -9, expected)
			p1dVoxel(t, current, 0, 0)
			e2b4Edit(t, f, eid, volume.VoxelWrite{X: 1, Value: 4})
			if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != override {
				t.Fatal("restored/fallback override lacks exact lifetime lease")
			}
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			p5cAssetPresent(t, f, override, false)
			if foreign != (AssetId{}) {
				p5cAssetPresent(t, f, foreign, true)
			}
		})
	}
}
