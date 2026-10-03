package gekko

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func e2b2Runtime(t *testing.T) (*s1gFixture, content.AssetPartDef, string) {
	t.Helper()
	f := s1gRuntime(t, []int{1}, 1, true, false)
	assetPath := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
	asset, err := content.LoadAsset(assetPath)
	if err != nil {
		t.Fatal(err)
	}
	part := asset.Parts[0]
	part.ModelScale, part.VoxelResolution = 1, 1
	part.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Voxels: []content.VoxelObjectVoxelDef{{Value: 1}, {X: 1, Value: 1}}, Palette: []content.AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "mat"}}}}
	asset.Materials = []content.AssetMaterialDef{{ID: "mat", Name: "mat", BaseColor: [4]uint8{100, 100, 100, 255}}}
	asset.Parts[0] = part
	if err := content.SaveAsset(assetPath, asset); err != nil {
		t.Fatal(err)
	}
	return f, part, filepath.Join(t.TempDir(), "override.gkvoxobj")
}

func e2b2Payload(t *testing.T, part content.AssetPartDef, placement, item, mode string, value uint8, empty bool) *content.VoxelObjectPayloadDef {
	t.Helper()
	_, lattice, identity := e2b1Canonical(t, part)
	p := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: mode, PlacementID: placement, ItemID: item, Lattice: lattice}
	if mode == content.VoxelObjectPayloadBaseDelta {
		p.BaseIdentity = identity
		p.Voxels = []content.VoxelObjectVoxelDef{{Value: 0}}
		if empty {
			p.Voxels = append(p.Voxels, content.VoxelObjectVoxelDef{X: 1, Value: 0})
		} else {
			p.Voxels = append(p.Voxels, content.VoxelObjectVoxelDef{X: -9, Value: value})
		}
	} else if !empty {
		p.Voxels = []content.VoxelObjectVoxelDef{{X: -9, Value: value}}
	}
	return p
}

func e2b2Save(t *testing.T, f *s1gFixture, path string, payload *content.VoxelObjectPayloadDef) {
	t.Helper()
	if _, err := content.SaveVoxelObjectPayload(path, payload, nil); err != nil {
		t.Fatal(err)
	}
	f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(payload.PlacementID, payload.ItemID)] = content.VoxelObjectOverrideDef{PlacementID: payload.PlacementID, ItemID: payload.ItemID, SnapshotPath: path}
}

func e2b2Body(t *testing.T, f *s1gFixture, placement string) EntityId {
	t.Helper()
	body := placementItemEntityByIDForStreamedTest(f.cmd, placement, "body")
	if body == 0 || f.hooks[placement] != 1 || f.runtime.LoadedChunks[ChunkCoord{}] == nil {
		t.Fatal("placement did not publish one atomic hook and chunk")
	}
	return body
}

func e2b2Prepared(t *testing.T, f *s1gFixture) {
	t.Helper()
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	if prepared.Err != nil {
		prepared.release()
		t.Fatal("valid v2 preparation failed before latest-state transition", prepared.Err)
	}
	f.runtime.PreparedLoads <- prepared
}

func TestE2b2WorkerFullDeltaEmptyAndNULBinding(t *testing.T) {
	for _, mode := range []string{content.VoxelObjectPayloadFull, content.VoxelObjectPayloadBaseDelta} {
		for _, empty := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "-negative", true: "-empty"}[empty], func(t *testing.T) {
				f, part, path := e2b2Runtime(t)
				id := s1gID(0, 0)
				if !empty && mode == content.VoxelObjectPayloadFull {
					id += "\x00nested"
					f.runtime.PlacementsByChunk[ChunkCoord{}][0].PlacementID = id
				}
				payload := e2b2Payload(t, part, id, "body", mode, 7, empty)
				e2b2Save(t, f, path, payload)
				// A live shared asset cache is unrelated to the canonical file base.
				geometry, err := authoredVoxelShapeGeometry(f.assets, part)
				if err != nil {
					t.Fatal(err)
				}
				asset, _ := f.assets.GetVoxelGeometry(geometry)
				asset.XBrickMap.SetVoxel(1, 0, 0, 9)
				s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
				captured := <-f.runtime.PreparedLoads
				if captured.Err != nil {
					captured.release()
					t.Fatal("valid v2 worker preparation failed", captured.Err)
				}
				snapshot := captured.ObjectSnapshots[voxelObjectRuntimeKey(id, "body")]
				if snapshot == nil {
					captured.release()
					t.Fatal("worker failed to retain resolved owned snapshot")
				}
				f.runtime.PreparedLoads <- captured
				refreshStreamedRuntimeMetricsCounts(f.runtime)
				if f.runtime.Metrics.PendingPreparedBytes <= 0 {
					t.Fatal("worker snapshot escaped pending accounting")
				}
				f.commitStage()
				body := e2b2Body(t, f, id)
				live := s4cLive(t, f.cmd, f.assets, body)
				if empty {
					if live.GetVoxelCount() != 0 {
						t.Fatal("empty override resurrected base")
					}
				} else {
					p1dVoxel(t, live, -9, 7)
					p1dVoxel(t, live, 0, 0)
					want := uint8(0)
					if mode == content.VoxelObjectPayloadBaseDelta {
						want = 1
					}
					p1dVoxel(t, live, 1, want)
				}
				if p5aAdoptions(t, f.runtime) != 1 || f.runtime.Metrics.PendingPreparedBytes != 0 {
					t.Fatal("current resolved v2 snapshot did not adopt/release existing worker ownership")
				}
				model := mustVoxelModelComponentForLevelTest(t, f.cmd, body)
				override := model.OverrideGeometry
				unrelated := f.assets.RegisterSharedVoxelGeometry(live, "unrelated")
				if err := unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{}); err != nil {
					t.Fatal(err)
				}
				f.app.FlushCommands()
				p5cAssetPresent(t, f, override, false)
				p5cAssetPresent(t, f, unrelated, true)
			})
		}
	}
}

func TestE2b2WorkerRejectsBindingAndUnsupportedOrigins(t *testing.T) {
	for _, mismatch := range []string{"placement", "item", "resolution", "rasterization", "base", "key-fields", "missing-part", "nonshape", "collapse", "nonshape-full", "collapse-full", "changed-authored-base"} {
		t.Run(mismatch, func(t *testing.T) {
			f, part, path := e2b2Runtime(t)
			id := s1gID(0, 0)
			payload := e2b2Payload(t, part, id, "body", content.VoxelObjectPayloadBaseDelta, 7, false)
			if mismatch == "nonshape-full" || mismatch == "collapse-full" {
				payload = e2b2Payload(t, part, id, "body", content.VoxelObjectPayloadFull, 7, false)
			}
			switch mismatch {
			case "placement":
				payload.PlacementID = "wrong"
			case "item":
				payload.ItemID = "wrong"
			case "resolution":
				payload.Lattice.VoxelResolution = 2
			case "rasterization":
				payload.Lattice.RasterizationVersion = "unsupported-rasterization"
			case "base":
				payload.BaseIdentity = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			case "missing-part":
				payload.ItemID = "ghost"
			}
			if _, err := content.SaveVoxelObjectPayload(path, payload, nil); err != nil {
				t.Fatal(err)
			}
			item := "body"
			if mismatch == "missing-part" {
				item = "ghost"
			}
			ref := content.VoxelObjectOverrideDef{PlacementID: id, ItemID: item, SnapshotPath: path}
			if mismatch == "key-fields" {
				ref.PlacementID = "unselected"
			}
			f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(id, item)] = ref
			if mismatch == "nonshape" || mismatch == "collapse" || mismatch == "nonshape-full" || mismatch == "collapse-full" || mismatch == "changed-authored-base" {
				assetPath := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
				asset, err := content.LoadAsset(assetPath)
				if err != nil {
					t.Fatal(err)
				}
				if mismatch == "nonshape" || mismatch == "nonshape-full" {
					asset.Parts[0].Source = testProceduralPartSource()
				} else if mismatch == "changed-authored-base" {
					asset.Parts[0].Source.VoxelShape.Voxels[1].Value = 2
				} else {
					asset.Runtime.CollapseVoxelParts = true
				}
				if err := content.SaveAsset(assetPath, asset); err != nil {
					t.Fatal(err)
				}
			}
			prepared := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(f.runtime, ChunkCoord{}))
			defer prepared.release()
			if prepared.Err == nil || len(prepared.ObjectSnapshots) != 0 || f.runtime.LoadedChunks[ChunkCoord{}] != nil || f.hooks[id] != 0 {
				t.Fatal("invalid v2 preparation retained or published geometry", mismatch)
			}
		})
	}
}

func TestE2b2LatestPlacementFileAuthorityAndRemovedOverride(t *testing.T) {
	for _, change := range []string{"same-path", "new-path", "removed", "mismatch", "missing-latest-target", "legacy-to-v2-with-missing-target"} {
		t.Run(change, func(t *testing.T) {
			f, part, path := e2b2Runtime(t)
			id := s1gID(0, 0)
			payload := e2b2Payload(t, part, id, "body", content.VoxelObjectPayloadBaseDelta, 3, false)
			e2b2Save(t, f, path, payload)
			if change == "legacy-to-v2-with-missing-target" {
				p5cSnapshot(t, path, 3, false)
			}
			e2b2Prepared(t, f)
			key := voxelObjectRuntimeKey(id, "body")
			switch change {
			case "removed":
				delete(f.runtime.voxelOverrideMap, key)
			default:
				if change == "new-path" {
					path = filepath.Join(t.TempDir(), "replacement.gkvoxobj")
				}
				payload = e2b2Payload(t, part, id, "body", content.VoxelObjectPayloadFull, 9, false)
				if change == "mismatch" {
					payload.Lattice.VoxelResolution = 2
				}
				if change == "missing-latest-target" {
					delete(f.runtime.voxelOverrideMap, key)
					payload.ItemID = "ghost"
				}
				e2b2Save(t, f, path, payload)
				if change == "legacy-to-v2-with-missing-target" {
					missing := e2b2Payload(t, part, id, "ghost", content.VoxelObjectPayloadFull, 9, false)
					e2b2Save(t, f, filepath.Join(t.TempDir(), "missing-item.gkvoxobj"), missing)
				}
			}
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			if change == "mismatch" || change == "missing-latest-target" || change == "legacy-to-v2-with-missing-target" {
				if f.runtime.InitErr == nil || f.hooks[id] != 0 || f.runtime.LoadedChunks[ChunkCoord{}] != nil {
					t.Fatal("invalid latest v2 selection published placement or hook")
				}
				if p5aAdoptions(t, f.runtime) != 0 {
					t.Fatal("invalid latest v2 selection adopted stale prepared override")
				}
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal("failed placement attempt did not release on stop", err)
				}
				f.app.FlushCommands()
				if f.runtime.Metrics.ActiveChunkCommitCount != 0 || placementEntityByIDForStreamedTest(f.cmd, id) != 0 {
					t.Fatal("stopped failed attempt retained active transaction or placement entities")
				}
			} else {
				if f.runtime.InitErr != nil {
					t.Fatal(f.runtime.InitErr)
				}
				body := e2b2Body(t, f, id)
				live := s4cLive(t, f.cmd, f.assets, body)
				if change == "removed" {
					if mustVoxelModelComponentForLevelTest(t, f.cmd, body).OverrideGeometry != (AssetId{}) {
						t.Fatal("removed override reapplied prepared payload")
					}
				} else {
					p1dVoxel(t, live, -9, 9)
					p1dVoxel(t, live, 1, 0)
				}
			}
			if p5aAdoptions(t, f.runtime) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("stale prepared resolved payload adopted or retained ownership")
			}
		})
	}
}

func TestE2b2CancelledPreparedOverrideReleasesOwnership(t *testing.T) {
	f, part, path := e2b2Runtime(t)
	id := s1gID(0, 0)
	e2b2Save(t, f, path, e2b2Payload(t, part, id, "body", content.VoxelObjectPayloadBaseDelta, 7, false))
	e2b2Prepared(t, f)
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if f.runtime.Metrics.PendingPreparedBytes <= 0 {
		t.Fatal("fixture lacks prepared ownership")
	}
	cancelStreamedPreparation(f.runtime.chunkPrepareCancels[ChunkCoord{}])
	f.commitStage()
	if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.LoadedChunks[ChunkCoord{}] != nil || f.hooks[id] != 0 || p5aAdoptions(t, f.runtime) != 0 {
		t.Fatal("cancelled resolved override retained ownership or published")
	}
}

func TestE2b2LegacyProceduralSnapshotRemainsAccepted(t *testing.T) {
	f, _ := p5cRuntime(t, 1, false)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	_, body := s1gPlacement(t, f, s1gID(0, 0))
	s1gValue(t, f, body, [3]int{1, 2, 3}, 3)
	if p5aAdoptions(t, f.runtime) != 1 {
		t.Fatal("legacy procedural override lost prepared adoption")
	}
}
