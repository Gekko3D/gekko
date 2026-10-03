package gekko

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func p5dInstallRenderer(f *streamedRenderHarness) {
	f.renderer = newVoxelRtStateTest()
	f.cmd.AddResources(f.renderer)
	f.app.FlushCommands()
	refreshStreamedRenderResidency(f.cmd, f.runtime)
}

func p5dBridge(f *streamedRenderHarness) {
	voxelRtSystem(nil, f.renderer, f.assets, &Time{Dt: 1.0 / 60}, f.cmd, nil)
}

func p5dStats(t *testing.T, assets *AssetServer, entries int, adoptions uint64) int64 {
	t.Helper()
	stats := assets.PreparedVoxelRendererCopyStats()
	if stats.Entries != entries || stats.Adoptions != adoptions || stats.Bytes < 0 || (stats.Bytes == 0) != (entries == 0) {
		t.Fatalf("renderer copy stats=%+v, want entries=%d adoptions=%d", stats, entries, adoptions)
	}
	if assets.PreparedVoxelRendererCopyStats() != stats {
		t.Fatal("renderer copy stats read mutated retained ownership")
	}
	return stats.Bytes
}

func TestP5dManagedTerrainRendererCopySingleUseAndIsolation(t *testing.T) {
	f, _, _ := p5bRuntime(t, []ChunkCoord{{}})
	p5dInstallRenderer(f)
	s1fPrepared(t, f, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	f.runtime.PreparedLoads <- prepared
	source := prepared.preparedTerrainGeometry
	f.commitStage()
	entity, _, asset := p5bTerrainAsset(t, f, ChunkCoord{})
	p5dStats(t, f.assets, 1, 0)
	model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
	sibling := f.cmd.AddEntity(s3cTransform(30), model)
	f.app.FlushCommands()
	p5dBridge(f)
	p5dStats(t, f.assets, 0, 1)
	a, b := f.renderer.GetVoxelObject(entity), f.renderer.GetVoxelObject(sibling)
	if a == nil || b == nil || a.XBrickMap == asset.XBrickMap || b.XBrickMap == asset.XBrickMap || a.XBrickMap == b.XBrickMap ||
		a.XBrickMap.ID == asset.XBrickMap.ID || b.XBrickMap.ID == asset.XBrickMap.ID || a.XBrickMap.ID == b.XBrickMap.ID || !a.XBrickMap.StructureDirty {
		t.Fatal("first renderer copy lost independent runtime identity or fresh structural work")
	}
	marker := s3cComponent[StreamedVoxelRenderComponent](t, f.cmd, entity)
	status, ok := f.renderer.StreamedVoxelStatus(marker.Ticket)
	if !ok || status.Entity != entity || status.MapID != a.XBrickMap.ID {
		t.Fatal("streamed readiness did not target the adopted runtime map")
	}
	p5bTerrainGeometry(t, source, false)
	p5bTerrainGeometry(t, asset.XBrickMap, false)
	p5bTerrainGeometry(t, a.XBrickMap, false)
	p5bTerrainGeometry(t, b.XBrickMap, false)
	a.XBrickMap.SetVoxel(9, 0, 11, 7)
	p5bTerrainGeometry(t, source, false)
	p5bTerrainGeometry(t, asset.XBrickMap, false)
	p5bTerrainGeometry(t, b.XBrickMap, false)
	p5dBridge(f)
	p5dStats(t, f.assets, 0, 1)
	if occupied, value := a.XBrickMap.GetVoxel(9, 0, 11); !occupied || value != 7 {
		t.Fatal("later bridge resynchronized the mutable adopted runtime map")
	}
}

func TestP5dFirstBridgeRejectsChangedSourceAndSharingScope(t *testing.T) {
	for _, change := range []string{"raw payload", "raw aux", "empty aux", "source replacement", "shared scope"} {
		t.Run(change, func(t *testing.T) {
			f, _, _ := p5bRuntime(t, []ChunkCoord{{}})
			p5dInstallRenderer(f)
			s1fPrepared(t, f, ChunkCoord{}, false)
			f.commitStage()
			entity, id, asset := p5bTerrainAsset(t, f, ChunkCoord{})
			p5dStats(t, f.assets, 1, 0)
			brick := asset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0)
			revision := asset.XBrickMap.Revision
			model := mustVoxelModelComponentForLevelTest(t, f.cmd, entity)
			expected := asset.XBrickMap
			switch change {
			case "raw payload":
				brick.Payload[2][0][3] = 9
			case "raw aux":
				brick.PrecomputedAux = []byte{7, 8, 9}
			case "empty aux":
				brick.PrecomputedAux = make([]byte, 0, 8)
			case "source replacement":
				replacement := asset.XBrickMap.Copy()
				replacement.SetVoxel(2, 0, 3, 9)
				model.OverrideGeometry = f.assets.RegisterSharedVoxelGeometry(replacement, "replacement")
				registered, _ := f.assets.GetVoxelGeometry(model.OverrideGeometry)
				expected = registered.XBrickMap
				f.cmd.AddComponents(entity, model)
			case "shared scope":
				model.ShareTerrainGeometry = true
				f.cmd.AddComponents(entity, model)
			}
			f.app.FlushCommands()
			if asset.XBrickMap.Revision != revision {
				t.Fatal("raw prebridge edits unexpectedly advanced the source revision")
			}
			p5dBridge(f)
			entries := 0
			if change == "source replacement" {
				entries = 1 // The untouched original asset still owns its candidate.
			}
			p5dStats(t, f.assets, entries, 0)
			obj := f.renderer.GetVoxelObject(entity)
			if obj == nil || obj.XBrickMap == nil {
				t.Fatal("fallback did not create renderer geometry")
			}
			if change == "shared scope" {
				if obj.XBrickMap != expected {
					t.Fatal("changed sharing scope did not preserve current source sharing")
				}
			} else if obj.XBrickMap == expected {
				t.Fatal("editable fallback shared current source storage")
			}
			if change == "raw payload" || change == "source replacement" {
				if occupied, value := obj.XBrickMap.GetVoxel(2, 0, 3); !occupied || value != 9 {
					t.Fatal("fallback copied stale prepared voxel values")
				}
			}
			if change == "raw aux" || change == "empty aux" {
				aux := obj.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux
				if change == "raw aux" && (len(aux) != 3 || aux[0] != 7 || aux[2] != 9) {
					t.Fatal("fallback lost raw auxiliary edits")
				}
				if change == "empty aux" && (aux == nil || len(aux) != 0 || cap(aux) != 8) {
					t.Fatal("fallback changed nonnil empty auxiliary Copy semantics")
				}
			}
			f.assets.DeleteVoxelGeometry(id)
			p5dStats(t, f.assets, 0, 0)
		})
	}
}

func TestP5dPendingAndUnbridgedAssetOwnershipThroughTerminalCleanup(t *testing.T) {
	for _, terminal := range []string{"cancel", "stop", "failed stop retry"} {
		t.Run(terminal, func(t *testing.T) {
			f, observer, _ := p5bRuntime(t, []ChunkCoord{{}, {X: 1}})
			p5dInstallRenderer(f)
			for _, coord := range []ChunkCoord{{}, {X: 1}} {
				s1fPrepared(t, f, coord, false)
			}
			first, deferred := <-f.runtime.PreparedLoads, <-f.runtime.PreparedLoads
			f.runtime.PreparedLoads <- first
			f.runtime.PreparedLoads <- deferred
			baseline := runtimeContentGraphCharge(streamedPreparedChunk{Generation: deferred.Generation, Coord: deferred.Coord, TerrainChunk: deferred.TerrainChunk, PlacementItems: deferred.PlacementItems, ObjectSnapshots: deferred.ObjectSnapshots})
			source := deferred.preparedTerrainGeometry
			registered := source.Copy()
			registered.ClearDirty()
			runtimeCopy := registered.Copy()
			minimum := baseline + s2aCharge(t, source) + s2aCharge(t, registered) + s2aCharge(t, runtimeCopy)
			f.commitStage()
			refreshStreamedRuntimeMetricsCounts(f.runtime)
			pending := f.runtime.Metrics.PendingPreparedBytes
			if f.runtime.Metrics.PreparedChunkQueueDepth != 1 || pending < minimum {
				t.Fatalf("deferred managed terrain must charge decoded input plus three maps: pending=%d minimum=%d", pending, minimum)
			}
			retained := p5dStats(t, f.assets, 1, 0)
			entity, id, asset := p5bTerrainAsset(t, f, ChunkCoord{})
			unrelated := f.assets.RegisterSharedVoxelGeometry(asset.XBrickMap, "unrelated")
			if terminal == "cancel" {
				s3aMove(f.cmd, observer, mgl32.Vec3{1600, 1, 1})
				f.observerStage()
				f.commitStage()
			} else {
				if terminal == "failed stop retry" {
					original := f.runtime.WorldDeltaPath
					blocker := filepath.Join(t.TempDir(), "file")
					if err := os.WriteFile(blocker, []byte("not a directory"), 0600); err != nil {
						t.Fatal(err)
					}
					f.runtime.WorldDeltaPath = filepath.Join(blocker, "delta.gkworlddelta")
					t.Cleanup(func() { f.runtime.WorldDeltaPath = original })
					if err := StopStreamedLevelRuntime(f.cmd); err == nil {
						t.Fatal("blocked persistence did not reject Stop")
					}
					if p5dStats(t, f.assets, 1, 0) != retained || !f.cmd.EntityExists(entity) || f.runtime.Metrics.PendingPreparedBytes != pending {
						t.Fatal("failed Stop released unbridged asset candidate or pending third map")
					}
					f.runtime.WorldDeltaPath = original
				}
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			}
			p5dStats(t, f.assets, 0, 0)
			if _, exists := f.assets.GetVoxelGeometry(id); exists || f.cmd.EntityExists(entity) || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("terminal cleanup retained adopted asset, entity or worker charge")
			}
			if _, exists := f.assets.GetVoxelGeometry(unrelated); !exists {
				t.Fatal("candidate cleanup deleted unrelated public registration")
			}
		})
	}
}

func TestP5dCPUOnlyCaptureAndLateRendererKeepDefensiveBridge(t *testing.T) {
	var absent *AssetServer
	p5dStats(t, absent, 0, 0)
	for _, lateBeforeCommit := range []bool{false, true} {
		t.Run(map[bool]string{false: "CPU-only capture", true: "late renderer"}[lateBeforeCommit], func(t *testing.T) {
			f, _, _ := p5bRuntime(t, []ChunkCoord{{}})
			s1fPrepared(t, f, ChunkCoord{}, false)
			if lateBeforeCommit {
				p5dInstallRenderer(f)
			}
			f.commitStage()
			entity, _, asset := p5bTerrainAsset(t, f, ChunkCoord{})
			p5dStats(t, f.assets, 0, 0)
			if !lateBeforeCommit {
				p5dInstallRenderer(f)
			}
			p5dBridge(f)
			p5dStats(t, f.assets, 0, 0)
			obj := f.renderer.GetVoxelObject(entity)
			if obj == nil || obj.XBrickMap == asset.XBrickMap || !obj.XBrickMap.StructureDirty {
				t.Fatal("CPU-only job capture changed current defensive renderer copying")
			}
			p5bTerrainGeometry(t, obj.XBrickMap, false)
		})
	}
}
