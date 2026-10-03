package gekko

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func p5cSnapshot(t *testing.T, path string, value uint8, large bool) {
	t.Helper()
	voxels := []content.VoxelObjectVoxelDef{{X: 1, Y: 2, Z: 3, Value: value}, {X: 9, Y: 10, Z: 11, Value: value}}
	if large {
		voxels = nil
		for i := 0; i < 32; i++ {
			voxels = append(voxels, content.VoxelObjectVoxelDef{X: i * 32, Value: value})
		}
	}
	if err := content.SaveVoxelObjectSnapshot(path, &content.VoxelObjectSnapshotDef{Voxels: voxels}); err != nil {
		t.Fatal(err)
	}
}

func p5cRuntime(t *testing.T, count int, large bool) (*s1gFixture, string) {
	t.Helper()
	f := s1gRuntime(t, []int{count}, 1, true, false)
	path := filepath.Join(t.TempDir(), "saved.gkvoxobj")
	p5cSnapshot(t, path, 3, large)
	for i := 0; i < count; i++ {
		id := s1gID(0, i)
		f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(id, "body")] = content.VoxelObjectOverrideDef{PlacementID: id, ItemID: "body", SnapshotPath: path}
	}
	return f, path
}

func p5cAsset(t *testing.T, f *s1gFixture, entity EntityId) (AssetId, VoxelGeometryAsset) {
	t.Helper()
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	id := model.OverrideGeometry
	asset, ok := f.assets.GetVoxelGeometry(id)
	if id == (AssetId{}) || !ok || asset.XBrickMap == nil || asset.XBrickMap.GetVoxelCount() == 0 {
		t.Fatal("snapshot did not publish a usable private override asset")
	}
	return id, asset
}

func p5cAssetPresent(t *testing.T, f *s1gFixture, id AssetId, want bool) {
	t.Helper()
	if _, got := f.assets.GetVoxelGeometry(id); got != want {
		t.Fatalf("exact asset %s present=%t, want %t", id, got, want)
	}
}

func TestP5cSnapshotWorkerAdoptionIsolationAndDurableReload(t *testing.T) {
	f, _ := p5cRuntime(t, 2, false)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	_, first := s1gPlacement(t, f, s1gID(0, 0))
	f.commitStage()
	_, second := s1gPlacement(t, f, s1gID(0, 1))
	firstID, firstAsset := p5cAsset(t, f, first)
	secondID, secondAsset := p5cAsset(t, f, second)
	if p5aAdoptions(t, f.runtime) != 2 {
		t.Fatal("two current saved snapshots did not adopt two independent worker registrations")
	}
	for _, asset := range []VoxelGeometryAsset{firstAsset, secondAsset} {
		if asset.LocalMin != (mgl32.Vec3{1, 2, 3}) || asset.LocalMax != (mgl32.Vec3{10, 11, 12}) {
			t.Fatalf("snapshot registration changed bounds: min=%v max=%v", asset.LocalMin, asset.LocalMax)
		}
	}
	if firstID == secondID || firstAsset.XBrickMap == secondAsset.XBrickMap {
		t.Fatal("editable snapshot instances shared registered storage")
	}
	firstAsset.XBrickMap.SetVoxel(1, 2, 3, 7)
	MarkVoxelEntityPersistenceDirty(f.cmd, first)
	f.app.FlushCommands()
	s1gValue(t, f, second, [3]int{1, 2, 3}, 3)
	unrelated := f.assets.RegisterSharedVoxelGeometry(secondAsset.XBrickMap, "unrelated")
	if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	p5cAssetPresent(t, f, firstID, false)
	p5cAssetPresent(t, f, secondID, false)
	p5cAssetPresent(t, f, unrelated, true)
	for id := range f.hooks {
		delete(f.hooks, id)
	}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	_, reloadedFirst := s1gPlacement(t, f, s1gID(0, 0))
	f.commitStage()
	_, reloadedSecond := s1gPlacement(t, f, s1gID(0, 1))
	s1gValue(t, f, reloadedFirst, [3]int{1, 2, 3}, 7)
	s1gValue(t, f, reloadedSecond, [3]int{1, 2, 3}, 3)
	if p5aAdoptions(t, f.runtime) != 4 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("reload did not adopt current durable snapshots or retained pending ownership")
	}
}

func TestP5cResumableSnapshotUsesCurrentFileAuthority(t *testing.T) {
	for _, change := range []string{"same path", "new path", "removed override", "missing file", "reordered entries", "legacy captured"} {
		t.Run(change, func(t *testing.T) {
			f, path := p5cRuntime(t, 1, false)
			if change == "reordered entries" {
				// Duplicate coordinates make authored order observable: the worker
				// captures last-write 9; the reversed current file must produce 3.
				if err := content.SaveVoxelObjectSnapshot(path, &content.VoxelObjectSnapshotDef{Voxels: []content.VoxelObjectVoxelDef{
					{X: 1, Y: 2, Z: 3, Value: 3}, {X: 1, Y: 2, Z: 3, Value: 9},
				}}); err != nil {
					t.Fatal(err)
				}
			}
			if change == "legacy captured" {
				f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 0
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			key := voxelObjectRuntimeKey(s1gID(0, 0), "body")
			switch change {
			case "same path", "legacy captured":
				p5cSnapshot(t, path, 9, false)
			case "new path":
				current := filepath.Join(t.TempDir(), "replacement.gkvoxobj")
				p5cSnapshot(t, current, 9, false)
				f.runtime.voxelOverrideMap[key] = content.VoxelObjectOverrideDef{PlacementID: s1gID(0, 0), ItemID: "body", SnapshotPath: current}
			case "removed override":
				delete(f.runtime.voxelOverrideMap, key)
			case "missing file":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "reordered entries":
				snapshot, err := content.LoadVoxelObjectSnapshot(path)
				if err != nil {
					t.Fatal(err)
				}
				snapshot.Voxels[0], snapshot.Voxels[1] = snapshot.Voxels[1], snapshot.Voxels[0]
				if err := content.SaveVoxelObjectSnapshot(path, snapshot); err != nil {
					t.Fatal(err)
				}
			}
			// The harness's convenience wrapper rejects expected fatal commits.
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			wantAdoptions := 0
			if change == "legacy captured" {
				wantAdoptions = 1
			}
			if got := p5aAdoptions(t, f.runtime); got != wantAdoptions {
				t.Fatalf("snapshot authority adoption count=%d, want %d", got, wantAdoptions)
			}
			if change == "missing file" {
				if f.runtime.InitErr == nil || f.hooks[s1gID(0, 0)] != 0 || f.runtime.LoadedChunks[ChunkCoord{}] != nil {
					t.Fatal("missing authoritative file did not remain fatal before placement hook/publication")
				}
				return
			}
			_, body := s1gPlacement(t, f, s1gID(0, 0))
			if change == "removed override" {
				if model := mustVoxelModelComponentForLevelTest(t, f.cmd, body); model.OverrideGeometry != (AssetId{}) {
					t.Fatal("removed override still applied captured snapshot")
				}
			} else {
				want := uint8(9)
				if change == "reordered entries" || change == "legacy captured" {
					want = 3
				}
				s1gValue(t, f, body, [3]int{1, 2, 3}, want)
			}
		})
	}
}

func TestP5cPendingSnapshotOwnershipThroughPartialCancellationAndStopRetry(t *testing.T) {
	for _, cancel := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel", false: "failed stop retry"}[cancel], func(t *testing.T) {
			f, _ := p5cRuntime(t, 2, true)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			captured := <-f.runtime.PreparedLoads
			f.runtime.PreparedLoads <- captured
			// Measure decoded/envelope inputs separately from the two physical
			// maps per snapshot. No packet bookkeeping or future field layout is
			// assumed, and decoded voxel storage cannot satisfy the map charge.
			baseline := runtimeContentGraphCharge(streamedPreparedChunk{
				Generation: captured.Generation, Coord: captured.Coord,
				PlacementItems: captured.PlacementItems, ObjectSnapshots: captured.ObjectSnapshots,
			})
			var geometryBytes int64
			for _, snapshot := range captured.ObjectSnapshots {
				source := XBrickMapFromVoxelObjectSnapshot(snapshot)
				source.ComputeAABB()
				source.ClearDirty()
				copy := source.Copy()
				copy.ClearDirty()
				geometryBytes += s2aCharge(t, source) + s2aCharge(t, copy)
			}
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			if bytes := f.runtime.Metrics.PendingPreparedBytes; geometryBytes == 0 || bytes < baseline+geometryBytes {
				t.Fatalf("snapshots must retain decoded inputs plus source and registration maps: pending=%d baseline=%d geometry=%d", bytes, baseline, geometryBytes)
			}
			f.commitStage()
			_, body := s1gPlacement(t, f, s1gID(0, 0))
			id, asset := p5cAsset(t, f, body)
			unrelated := f.assets.RegisterSharedVoxelGeometry(asset.XBrickMap, "unrelated")
			pending := f.runtime.Metrics.PendingPreparedBytes
			if p5aAdoptions(t, f.runtime) != 1 || pending <= 0 || f.runtime.Metrics.ActiveChunkCommitCount != 1 {
				t.Fatal("fixture did not retain an adopted partial placement and its pending remainder")
			}
			if cancel {
				s3aMove(f.cmd, f.observer, mgl32.Vec3{1600, 1, 1})
				f.observerStage()
				f.commitStage()
			} else {
				asset.XBrickMap.SetVoxel(0, 0, 0, 7)
				MarkVoxelEntityPersistenceDirty(f.cmd, body)
				f.app.FlushCommands()
				f.runtime.WorldDelta.SchemaVersion = -1
				t.Cleanup(func() {
					if f.runtime.WorldDelta != nil {
						f.runtime.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
					}
				})
				if err := StopStreamedLevelRuntime(f.cmd); err == nil {
					t.Fatal("invalid durable manifest did not reject Stop")
				}
				p5cAssetPresent(t, f, id, true)
				if !f.cmd.EntityExists(body) || f.runtime.Metrics.PendingPreparedBytes != pending {
					t.Fatal("failed Stop released adopted entity or pending remainder")
				}
				f.runtime.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			}
			p5cAssetPresent(t, f, id, false)
			p5cAssetPresent(t, f, unrelated, true)
			if f.cmd.EntityExists(body) || f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 0 {
				t.Fatal("durable terminal cleanup retained partial entity or worker ownership")
			}
		})
	}
}
