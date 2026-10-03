package gekko

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func e2c3Fixture(t *testing.T, kind string, enabled bool) (*s1gFixture, content.AssetPartDef, EntityId, []volume.VoxelWrite) {
	t.Helper()
	f, part, _ := e2b2Runtime(t)
	part.Source.VoxelShape.Voxels = nil
	var writes []volume.VoxelWrite
	if kind == "mixed" {
		for _, origin := range []int{-8, 0, 8} {
			for z := 0; z < 8; z++ {
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						part.Source.VoxelShape.Voxels = append(part.Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: origin + x, Y: y, Z: z, Value: 1})
						if origin == -8 {
							value := uint8(0)
							if z == 0 && y < 3 && x < 4 {
								value = 7 + uint8((x+y)%2)
							}
							writes = append(writes, volume.VoxelWrite{X: origin + x, Y: y, Z: z, Value: value})
						}
						if origin == 8 {
							writes = append(writes, volume.VoxelWrite{X: origin + x, Y: y, Z: z})
						}
					}
				}
			}
		}
		writes = append(writes, volume.VoxelWrite{Value: 9})
	} else {
		cells := 33
		if kind == "strict34" {
			cells = 34
		}
		if kind == "record_growth" {
			cells = 512
		}
		for i := 0; i < cells; i++ {
			value := uint8(1)
			if kind == "record_growth" {
				value = 7
				if i < 35 {
					value = 1 + uint8(i%2)
				}
			}
			part.Source.VoxelShape.Voxels = append(part.Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: i % 8, Y: (i / 8) % 8, Z: i / 64, Value: value})
		}
		if kind == "record_growth" {
			for i := 0; i < 34; i++ {
				writes = append(writes, volume.VoxelWrite{X: i % 8, Y: (i / 8) % 8, Z: i / 64})
			}
			writes = append(writes, volume.VoxelWrite{X: 34 % 8, Y: (34 / 8) % 8, Z: 34 / 64, Value: 7})
		} else {
			for i := 1; i < cells; i++ {
				writes = append(writes, volume.VoxelWrite{X: i % 8, Y: (i / 8) % 8, Z: i / 64})
			}
			writes = append(writes, volume.VoxelWrite{Value: 7})
		}
	}
	part.Source.VoxelShape.Palette = []content.AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "mat"}, {Value: 2, MaterialID: "mat"}, {Value: 7, MaterialID: "mat"}}
	path := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
	asset, err := content.LoadAsset(path)
	if err != nil {
		t.Fatal(err)
	}
	asset.Parts[0] = part
	if err := content.SaveAsset(path, asset); err != nil {
		t.Fatal(err)
	}
	if f.runtime.Config.EnableHybridVoxelObjectDeltas {
		t.Fatal("default configuration unexpectedly enables hybrid selection")
	}
	f.runtime.Config.EnableHybridVoxelObjectDeltas = enabled
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := e2b2Body(t, f, s1gID(0, 0))
	e2b3Enable(t, f, eid)
	e2b3Qualified(t, f, eid, s1gID(0, 0), part)
	e2b4Edit(t, f, eid, writes...)
	return f, part, eid, writes
}

func e2c3Values(records []content.VoxelObjectVoxelDef) map[[3]int]uint8 {
	result := make(map[[3]int]uint8)
	for _, v := range records {
		if v.Value != 0 {
			result[[3]int{v.X, v.Y, v.Z}] = v.Value
		}
	}
	return result
}

func e2c3Resolved(t *testing.T, payload *content.VoxelObjectPayloadDef, part content.AssetPartDef) *volume.XBrickMap {
	t.Helper()
	base, lattice, identity := e2b1Canonical(t, part)
	if payload.PlacementID != s1gID(0, 0) || payload.ItemID != "body" || payload.BaseIdentity != identity || payload.Lattice != lattice {
		t.Fatal("chosen delta lost original authored binding")
	}
	resolved, err := content.ResolveVoxelObjectPayload(payload, base, lattice, s1gID(0, 0), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	return XBrickMapFromVoxelObjectSnapshot(resolved)
}

func e2c3Hybrid(t *testing.T, payload *content.VoxelObjectPayloadDef) {
	t.Helper()
	if payload.SchemaVersion != 3 || payload.Mode != content.VoxelObjectPayloadHybridDelta || !reflect.DeepEqual(payload.ReplacementBricks, [][3]int32{{-1, 0, 0}, {1, 0, 0}}) || len(payload.Voxels) != 13 {
		t.Fatalf("chosen mixed payload schema=%d mode=%q selectors=%v records=%d", payload.SchemaVersion, payload.Mode, payload.ReplacementBricks, len(payload.Voxels))
	}
	for _, v := range payload.Voxels {
		if v.X >= 8 {
			t.Fatal("empty replacement emitted records")
		}
		if v.X < 0 && v.Value == 0 {
			t.Fatal("replacement captured removal assignments")
		}
	}
}

func TestE2c3SelectionRequiresStrictSavingsAndBoundedRecordGrowth(t *testing.T) {
	for _, kind := range []string{"default_off", "tie33", "strict34", "record_growth"} {
		t.Run(kind, func(t *testing.T) {
			fixtureKind := kind
			enabled := kind != "default_off"
			if !enabled {
				fixtureKind = "mixed"
			}
			f, part, _, _ := e2c3Fixture(t, fixtureKind, enabled)
			path := f.runtime.WorldDeltaPath
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			payload := e2b4DurablePath(t, path)
			if kind == "strict34" {
				if payload.SchemaVersion != 3 || payload.Mode != content.VoxelObjectPayloadHybridDelta || !reflect.DeepEqual(payload.ReplacementBricks, [][3]int32{{0, 0, 0}}) || !reflect.DeepEqual(payload.Voxels, []content.VoxelObjectVoxelDef{{Value: 7}}) {
					t.Fatal("strict 34-assignment savings failed to choose one-record hybrid")
				}
			} else if payload.SchemaVersion != 2 || payload.Mode != content.VoxelObjectPayloadBaseDelta || len(payload.ReplacementBricks) != 0 {
				t.Fatalf("%s must retain schema2 assignments, got schema%d/%s", kind, payload.SchemaVersion, payload.Mode)
			}
			resolved := e2c3Resolved(t, payload, part)
			if kind == "record_growth" {
				if len(VoxelObjectSnapshotFromXBrickMap(resolved).Voxels) != 478 {
					t.Fatal("record-growth fallback lost geometry")
				}
			} else if kind != "default_off" {
				p1dVoxel(t, resolved, 0, 7)
				if len(VoxelObjectSnapshotFromXBrickMap(resolved).Voxels) != 1 {
					t.Fatal("tie/strict selection changed final geometry")
				}
			}
		})
	}
}

func TestE2c3AsyncHybridAdmissionReloadAndTrackingRestoration(t *testing.T) {
	f, part, eid, _ := e2c3Fixture(t, "mixed", true)
	expected, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	expectedValues := e2c3Values(VoxelObjectSnapshotFromXBrickMap(expected).Voxels)
	override := s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry
	f.runtime.Config.MaxPendingPersistenceBytes = 16 << 10
	entered, release := e2b4HoldManifest(t, f)
	e2b4Depart(f)
	s4cDriveUntilWriterEntered(t, f.cmd, f.runtime, entered)
	metrics := f.runtime.Metrics
	if metrics.PendingPersistenceBytes <= 0 || metrics.PendingPersistenceBytes > 16<<10 || metrics.PendingPersistenceOversizedAdmissions != 0 {
		t.Fatalf("hybrid retained assignment-sized capture instead of chosen records/selectors: %+v", metrics)
	}
	release()
	e2b4Unloaded(t, f)
	payload := e2b4Durable(t, f)
	e2c3Hybrid(t, payload)
	if !reflect.DeepEqual(e2c3Values(VoxelObjectSnapshotFromXBrickMap(e2c3Resolved(t, payload, part)).Voxels), expectedValues) {
		t.Fatal("hybrid async save changed geometry")
	}
	p5cAssetPresent(t, f, override, false)
	// Readers and explicit restoration accept schema three with selection disabled.
	f.runtime.Config.EnableHybridVoxelObjectDeltas = false
	f.hooks[s1gID(0, 0)] = 0
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid = e2b2Body(t, f, s1gID(0, 0))
	e2b3Enable(t, f, eid)
	e2b3Qualified(t, f, eid, s1gID(0, 0), part)
	changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, eid)
	if !tracked || len(changes) != 1025 {
		t.Fatalf("hybrid reload lost original-base history: %d,%v", len(changes), tracked)
	}
	model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	borrower := f.cmd.AddEntity(&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}, &model)
	f.app.FlushCommands()
	borrowerGeometry := e2b3Enable(t, f, borrower)
	e2b4Edit(t, f, borrower, volume.VoxelWrite{X: -8, Value: 99})
	current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	p1dVoxel(t, current, -8, 7)
	e2b3Qualified(t, f, eid, s1gID(0, 0), part)
	e2b4Edit(t, f, eid, volume.VoxelWrite{X: -8, Value: 10})
	path := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	saved := e2b4DurablePath(t, path)
	if saved.SchemaVersion != 2 || saved.Mode != content.VoxelObjectPayloadBaseDelta {
		t.Fatal("disabled selection changed restored owner's v2 save contract")
	}
	p1dVoxel(t, e2c3Resolved(t, saved, part), -8, 10)
	f.assets.DeleteVoxelGeometry(borrowerGeometry)
	f.cmd.RemoveEntity(borrower)
	f.app.FlushCommands()
}

func TestE2c3BlockingHybridMatchesAsyncGeometry(t *testing.T) {
	f, part, eid, _ := e2c3Fixture(t, "mixed", true)
	current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	want := e2c3Values(VoxelObjectSnapshotFromXBrickMap(current).Voxels)
	path := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	payload := e2b4DurablePath(t, path)
	e2c3Hybrid(t, payload)
	if !reflect.DeepEqual(e2c3Values(VoxelObjectSnapshotFromXBrickMap(e2c3Resolved(t, payload, part)).Voxels), want) {
		t.Fatal("blocking hybrid snapshot changed final geometry")
	}
}

func TestE2c3HybridManifestStalenessAndFailureRetainCurrentEdits(t *testing.T) {
	for _, kind := range []string{"stale_edit", "failed_manifest"} {
		t.Run(kind, func(t *testing.T) {
			f, part, eid, _ := e2c3Fixture(t, "mixed", true)
			if err := persistChunkOverrides(f.cmd, f.runtime, ChunkCoord{}, f.runtime.LoadedChunks[ChunkCoord{}]); err != nil {
				t.Fatal(err)
			}
			old := s4aLoadDelta(t, f.runtime.WorldDeltaPath)
			oldPath := s4aPayloadPath(t, old, "object", f.runtime.WorldDeltaPath)
			oldBytes := s4aRead(t, oldPath)
			e2b4Edit(t, f, eid, volume.VoxelWrite{X: -8, Value: 9})
			entered, gate := make(chan struct{}), make(chan struct{})
			var once, releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(gate) }) }
			t.Cleanup(func() { release(); f.runtime.jobs.Wait(); f.runtime.worldDeltaWriter = nil })
			marker := errors.New("held hybrid manifest failure")
			f.runtime.worldDeltaWriter = func(path string, delta *content.WorldDeltaDef) error {
				first := false
				once.Do(func() { first = true; close(entered); <-gate })
				if first && kind == "failed_manifest" {
					return marker
				}
				return content.SaveWorldDelta(path, delta)
			}
			e2b4Depart(f)
			s4cDriveUntilWriterEntered(t, f.cmd, f.runtime, entered)
			value := uint8(9)
			if kind == "stale_edit" {
				value = 10
				e2b4Edit(t, f, eid, volume.VoxelWrite{X: -8, Value: value})
			}
			release()
			f.runtime.jobs.Wait()
			err := commitStreamedPersistence(f.cmd, f.runtime, true)
			if kind == "failed_manifest" {
				if !errors.Is(err, marker) {
					t.Fatal("manifest fixture did not fail", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			f.app.FlushCommands()
			if !f.cmd.EntityExists(eid) || f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.Metrics.DirtyPinnedChunkCount != 1 {
				t.Fatal("hybrid manifest completion lost current dirty ownership")
			}
			if kind == "failed_manifest" {
				if !reflect.DeepEqual(s4aLoadDelta(t, f.runtime.WorldDeltaPath), old) || !bytes.Equal(s4aRead(t, oldPath), oldBytes) || f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(s1gID(0, 0), "body")] != old.VoxelObjectOverrides[0] {
					t.Fatal("failed hybrid manifest acknowledged changed baseline")
				}
			} else {
				durable := s4aLoadDelta(t, f.runtime.WorldDeltaPath)
				if f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(s1gID(0, 0), "body")] != durable.VoxelObjectOverrides[0] {
					t.Fatal("same-owner hybrid capture lost valid durable baseline")
				}
				p1dVoxel(t, e2c3Resolved(t, e2b4Durable(t, f), part), -8, 9)
			}
			current, dirty, _ := currentVoxelMapForEntity(f.cmd, eid)
			if !dirty {
				t.Fatal("stale/failed hybrid ACK cleared current dirty authority")
			}
			p1dVoxel(t, current, -8, value)
			e2b4Unloaded(t, f)
			payload := e2b4Durable(t, f)
			e2c3Hybrid(t, payload)
			p1dVoxel(t, e2c3Resolved(t, payload, part), -8, value)
		})
	}
}
