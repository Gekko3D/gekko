package gekko

import (
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func e2b4Edit(t *testing.T, f *s1gFixture, eid EntityId, writes ...volume.VoxelWrite) {
	t.Helper()
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(writes...)); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
}

func e2b4Durable(t *testing.T, f *s1gFixture) *content.VoxelObjectPayloadDef {
	t.Helper()
	return e2b4DurablePath(t, f.runtime.WorldDeltaPath)
}

func e2b4DurablePath(t *testing.T, path string) *content.VoxelObjectPayloadDef {
	t.Helper()
	delta := s4aLoadDelta(t, path)
	payload, _, err := content.LoadVoxelObjectPayload(s4aPayloadPath(t, delta, "object", path), nil)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func e2b4Delta(t *testing.T, payload *content.VoxelObjectPayloadDef, part content.AssetPartDef, placement, item string, want []content.VoxelObjectVoxelDef) {
	t.Helper()
	_, lattice, identity := e2b1Canonical(t, part)
	if payload.SchemaVersion != 2 || payload.Mode != content.VoxelObjectPayloadBaseDelta || payload.PlacementID != placement || payload.ItemID != item || payload.Lattice != lattice || payload.BaseIdentity != identity || !reflect.DeepEqual(payload.Voxels, want) {
		t.Fatalf("durable delta=%+v, want exact bound assignments %v", payload, want)
	}
}

func e2b4Depart(f *s1gFixture) { s3aMove(f.cmd, f.observer, mgl32.Vec3{1000, 1, 1}) }
func e2b4Unloaded(t *testing.T, f *s1gFixture) {
	t.Helper()
	s4cDrive(t, f.cmd, f.runtime, func() bool { return len(f.runtime.LoadedChunks) == 0 && f.runtime.Metrics.PendingPersistenceCount == 0 })
	if f.runtime.Metrics.PendingPersistenceBytes != 0 || f.runtime.Metrics.DirtyPinnedChunkCount != 0 {
		t.Fatalf("completed persistence retained ownership: %+v", f.runtime.Metrics)
	}
}

func e2b4HoldManifest(t *testing.T, f *s1gFixture) (<-chan struct{}, func()) {
	t.Helper()
	entered, gate := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(func() { release(); f.runtime.jobs.Wait(); f.runtime.worldDeltaWriter = nil })
	f.runtime.worldDeltaWriter = func(path string, delta *content.WorldDeltaDef) error {
		once.Do(func() { close(entered); <-gate })
		return content.SaveWorldDelta(path, delta)
	}
	return entered, release
}

func TestE2b4AsyncDeltaDurabilityReloadAndLease(t *testing.T) {
	f, part, eid := e2b3Pristine(t)
	source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
	override := e2b3Enable(t, f, eid)
	e2b4Edit(t, f, eid, volume.VoxelWrite{Value: 0}, volume.VoxelWrite{X: -9, Value: 7}, volume.VoxelWrite{X: 1, Value: 2}, volume.VoxelWrite{X: 1, Value: 1})
	e2b4Depart(f)
	e2b4Unloaded(t, f)
	e2b4Delta(t, e2b4Durable(t, f), part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{X: -9, Value: 7}, {Value: 0}})
	p5cAssetPresent(t, f, override, false)
	p5cAssetPresent(t, f, source, true)
	f.hooks[s1gID(0, 0)] = 0
	s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	reloaded := e2b2Body(t, f, s1gID(0, 0))
	current, _, ok := currentVoxelMapForEntity(f.cmd, reloaded)
	if !ok {
		t.Fatal("reloaded authority missing")
	}
	p1dVoxel(t, current, -9, 7)
	p1dVoxel(t, current, 0, 0)
	p1dVoxel(t, current, 1, 1)
	if lease := f.runtime.snapshotGeometryAssets[reloaded]; lease.ID == (AssetId{}) || lease.ID == override {
		t.Fatal("reload did not acquire independent snapshot ownership")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
}

func TestE2b4BlockingDeltaEmptyAndExplicitBindings(t *testing.T) {
	for _, kind := range []string{"revert", "remove_all", "nul_placement", "nul_item", "outside_int32", "profile_cap"} {
		t.Run(kind, func(t *testing.T) {
			f, part, _ := e2b2Runtime(t)
			placement := s1gID(0, 0)
			if kind == "nul_placement" || kind == "nul_item" {
				placement = "P\x00nested"
			}
			item := "body"
			if kind == "nul_item" {
				item = "body\x00nested"
				path := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
				asset, err := content.LoadAsset(path)
				if err != nil {
					t.Fatal(err)
				}
				part.ID = item
				asset.Parts = append(asset.Parts, part)
				if err := content.SaveAsset(path, asset); err != nil {
					t.Fatal(err)
				}
			}
			f.runtime.PlacementsByChunk[ChunkCoord{}][0].PlacementID = placement
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := e2b2Body(t, f, placement)
			if item != "body" {
				eid = placementItemEntityByIDForStreamedTest(f.cmd, placement, item)
				if eid == 0 {
					t.Fatal("NUL item missing")
				}
			}
			override := e2b3Enable(t, f, eid)
			var want []content.VoxelObjectVoxelDef
			switch kind {
			case "revert":
				e2b4Edit(t, f, eid, volume.VoxelWrite{Value: 3}, volume.VoxelWrite{Value: 1})
			case "remove_all":
				e2b4Edit(t, f, eid, volume.VoxelWrite{}, volume.VoxelWrite{X: 1})
				want = []content.VoxelObjectVoxelDef{{}, {X: 1}}
			case "profile_cap":
				writes := make([]volume.VoxelWrite, voxelcodec.DefaultLimits().MaxBricks+1)
				for i := range writes {
					writes[i] = volume.VoxelWrite{X: i % 32, Y: (i / 32) % 32, Z: i / 1024, Value: 7}
				}
				e2b4Edit(t, f, eid, writes...)
			case "outside_int32":
				e2b4Edit(t, f, eid, volume.VoxelWrite{X: 1 << 31, Value: 7})
			default:
				e2b4Edit(t, f, eid, volume.VoxelWrite{X: -9, Value: 7})
				want = []content.VoxelObjectVoxelDef{{X: -9, Value: 7}}
			}
			deltaPath := f.runtime.WorldDeltaPath
			if kind == "nul_item" {
				e2b4Depart(f)
				e2b4Unloaded(t, f)
				e2b4Delta(t, e2b4Durable(t, f), part, placement, item, want)
				f.hooks[placement] = 0
				s3aMove(f.cmd, f.observer, mgl32.Vec3{1, 1, 1})
				updateStreamedObserverSelection(f.cmd, f.runtime)
				s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
				f.commitStage()
				reloaded := placementItemEntityByIDForStreamedTest(f.cmd, placement, item)
				current, _, ok := currentVoxelMapForEntity(f.cmd, reloaded)
				if !ok {
					t.Fatal("NUL tuple reload missing")
				}
				p1dVoxel(t, current, -9, 7)
			}
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			payload := e2b4DurablePath(t, deltaPath)
			if kind == "outside_int32" || kind == "profile_cap" {
				if payload.SchemaVersion != 1 || payload.Mode != content.VoxelObjectPayloadFull {
					t.Fatalf("legacy-compatible unrepresentable delta did not fall back: %+v", payload)
				}
				x := XBrickMapFromVoxelObjectSnapshot(&content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: payload.Voxels})
				if kind == "outside_int32" {
					p1dVoxel(t, x, 1<<31, 7)
				} else if kind == "profile_cap" {
					present, value := x.GetVoxel(0, 0, 16)
					if !present || value != 7 {
						t.Fatal("profile fallback lost final assignment")
					}
				} else {
					p1dVoxel(t, x, -9, 7)
				}
			} else {
				// Canonical decoders may represent an empty assignment slice as nil or empty.
				if len(want) == 0 {
					want = payload.Voxels
					if len(want) != 0 {
						t.Fatal("reverted delta retained assignments")
					}
				}
				e2b4Delta(t, payload, part, placement, item, want)
				base, lattice, _ := e2b1Canonical(t, part)
				resolved, err := content.ResolveVoxelObjectPayload(payload, base, lattice, placement, item, nil)
				if err != nil {
					t.Fatal(err)
				}
				x := XBrickMapFromVoxelObjectSnapshot(resolved)
				if kind == "remove_all" {
					p1dVoxel(t, x, 0, 0)
					p1dVoxel(t, x, 1, 0)
				} else {
					p1dVoxel(t, x, 0, 1)
					p1dVoxel(t, x, 1, 1)
				}
			}
			p5cAssetPresent(t, f, override, false)
		})
	}
}

func TestE2b4SparseAdmissionDoesNotRetainUntouchedBaseBricks(t *testing.T) {
	var retained [2]int64
	for index, sectors := range []int{1, 128} {
		t.Run([]string{"small", "large"}[index], func(t *testing.T) {
			f, part, _ := e2b2Runtime(t)
			part.Source.VoxelShape.Voxels = nil
			for i := 0; i < sectors; i++ {
				part.Source.VoxelShape.Voxels = append(part.Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: i * 32, Value: 1})
			}
			path := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
			asset, err := content.LoadAsset(path)
			if err != nil {
				t.Fatal(err)
			}
			asset.Parts[0] = part
			if err := content.SaveAsset(path, asset); err != nil {
				t.Fatal(err)
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := e2b2Body(t, f, s1gID(0, 0))
			e2b3Enable(t, f, eid)
			e2b4Edit(t, f, eid, volume.VoxelWrite{X: -9, Value: 7})
			f.runtime.Config.MaxPendingPersistenceBytes = 64 << 10
			entered, release := e2b4HoldManifest(t, f)
			e2b4Depart(f)
			s4cDriveUntilWriterEntered(t, f.cmd, f.runtime, entered)
			retained[index] = f.runtime.Metrics.PendingPersistenceBytes
			if retained[index] <= 0 || retained[index] > 64<<10 || f.runtime.Metrics.PendingPersistenceOversizedAdmissions != 0 {
				t.Errorf("sparse capture exceeded admission budget: %+v", f.runtime.Metrics)
			}
			release()
			e2b4Unloaded(t, f)
		})
	}
	if retained[1] > retained[0]+1024 {
		t.Fatalf("unchanged base sectors scaled sparse retained bytes: small=%d large=%d", retained[0], retained[1])
	}
}

func TestE2b4HeldManifestRejectsChangedManagedAuthority(t *testing.T) {
	for _, kind := range []string{"edit", "expose_without_write", "ref", "lattice"} {
		t.Run(kind, func(t *testing.T) {
			f, part, eid := e2b3Pristine(t)
			override := e2b3Enable(t, f, eid)
			e2b4Edit(t, f, eid, volume.VoxelWrite{X: -9, Value: 7})
			entered, release := e2b4HoldManifest(t, f)
			e2b4Depart(f)
			s4cDriveUntilWriterEntered(t, f.cmd, f.runtime, entered)
			switch kind {
			case "edit":
				e2b4Edit(t, f, eid, volume.VoxelWrite{X: -9, Value: 9})
			case "expose_without_write":
				if _, ok := f.assets.GetVoxelGeometry(override); !ok {
					t.Fatal("owner missing")
				}
			case "ref":
				s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid).AssetID = "different"
			case "lattice":
				s3cComponent[VoxelModelComponent](t, f.cmd, eid).VoxelResolution = 2
			}
			release()
			f.runtime.jobs.Wait()
			if err := commitStreamedPersistence(f.cmd, f.runtime, true); err != nil {
				t.Fatal(err)
			}
			f.app.FlushCommands()
			if kind == "edit" || kind == "expose_without_write" {
				durable := s4aLoadDelta(t, f.runtime.WorldDeltaPath)
				if len(durable.VoxelObjectOverrides) != 1 || f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(s1gID(0, 0), "body")] != durable.VoxelObjectOverrides[0] {
					t.Fatal("same-owner stale capture lost its valid durable override baseline")
				}
				e2b4Delta(t, e2b4Durable(t, f), part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{X: -9, Value: 7}})
			}
			if kind == "ref" || kind == "lattice" {
				if override := f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(s1gID(0, 0), "body")]; override.SnapshotPath != "" {
					t.Fatal("incompatible saved binding became the current entity override baseline")
				}
			}
			if !f.cmd.EntityExists(eid) || f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.runtime.Metrics.DirtyPinnedChunkCount != 1 {
				t.Fatalf("stale managed acknowledgement released live authority: %+v", f.runtime.Metrics)
			}
			e2b4Unloaded(t, f)
			payload := e2b4Durable(t, f)
			value := uint8(7)
			if kind == "edit" {
				value = 9
				e2b4Delta(t, payload, part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{X: -9, Value: value}})
			} else if payload.SchemaVersion != 1 || payload.Mode != content.VoxelObjectPayloadFull {
				t.Fatalf("qualification loss did not recapture legacy full authority: %+v", payload)
			}
			var snapshot *content.VoxelObjectSnapshotDef
			if kind == "edit" {
				base, lattice, _ := e2b1Canonical(t, part)
				var err error
				snapshot, err = content.ResolveVoxelObjectPayload(payload, base, lattice, s1gID(0, 0), "body", nil)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				snapshot = &content.VoxelObjectSnapshotDef{SchemaVersion: 1, Voxels: payload.Voxels}
			}
			p1dVoxel(t, XBrickMapFromVoxelObjectSnapshot(snapshot), -9, value)
			p5cAssetPresent(t, f, override, false)
		})
	}
}
