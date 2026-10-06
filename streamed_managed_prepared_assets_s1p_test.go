package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Warming through metadata and ID access deliberately does not borrow a mutable
// map. The actual streaming worker, commit and renderer bridge remain under test.
func s1pWarm(t *testing.T, f *s1gFixture, path, part string) (AssetId, AssetId) {
	t.Helper()
	prepared, err := LoadAndPrepareAuthoredAsset(path, f.assets, f.runtime.Loader)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := PreparedAuthoredAssetPartGeometry(prepared, part)
	if !ok {
		t.Fatal("warm geometry ID missing")
	}
	palette, ok := PreparedAuthoredAssetPartPalette(prepared, part)
	if !ok {
		t.Fatal("warm palette ID missing")
	}
	return id, palette
}

func s1pManaged(t *testing.T, f *s1gFixture, eid EntityId, shared AssetId) VoxelModelComponent {
	t.Helper()
	model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	if model.SharedGeometry != shared || model.OverrideGeometry == (AssetId{}) || f.assets.managedVoxelEntry(model.OverrideGeometry) == nil {
		t.Fatal("untouched verified warm geometry did not receive independent prepared managed authority")
	}
	if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != model.OverrideGeometry {
		t.Fatal("warm override missing chunk lease")
	}
	return model
}

func TestS1pUntouchedWarmWorkerBridgeTransfersIndependentCandidates(t *testing.T) {
	for _, kind := range []string{"shape", "model"} {
		t.Run(kind, func(t *testing.T) {
			f, path := s1nRuntime(t, true, 2)
			part := "body"
			if kind == "model" {
				path, _ = c3g9Fixture(t, "models")
				part = "model"
				for i := range f.runtime.PlacementsByChunk[ChunkCoord{}] {
					f.runtime.PlacementsByChunk[ChunkCoord{}][i].AssetPath = path
				}
				f.runtime.Config.PlacementHooks = nil
			}
			shared, palette := s1pWarm(t, f, path, part)
			before := len(f.assets.voxModels)
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			if len(f.assets.voxModels) != before || len(f.assets.managedVoxelGeometry) != 0 {
				t.Fatal("warm worker published live assets")
			}
			f.commitStage()
			first := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), part)
			second := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 1), part)
			a, b := s1pManaged(t, f, first, shared), s1pManaged(t, f, second, shared)
			if a.OverrideGeometry == b.OverrideGeometry || a.VoxelPalette != palette || b.VoxelPalette != palette {
				t.Fatal("warm placement ownership or palette changed")
			}
			want := 2
			if kind == "model" {
				want = 4
			}
			if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Entries != want {
				t.Fatalf("first derivatives=%d want %d", stats.Entries, want)
			}
			state := s1nRenderer(f)
			s1nBridge(f, state)
			left, right := s1l6Input(t, state.GetVoxelObject(first)), s1l6Input(t, state.GetVoxelObject(second))
			if left.SameSource(right) || state.GetVoxelObject(first).XBrickMap == state.GetVoxelObject(second).XBrickMap {
				t.Fatal("warm siblings share producer or renderer map")
			}
			authority, _ := f.assets.getVoxelGeometry(a.OverrideGeometry)
			global, _ := f.assets.getVoxelGeometry(shared)
			if authority.XBrickMap == global.XBrickMap || authority.XBrickMap == state.GetVoxelObject(first).XBrickMap {
				t.Fatal("warm authority aliases global or renderer map")
			}
			if kind == "model" && (authority.LocalMin != (mgl32.Vec3{}) || authority.LocalMax != (mgl32.Vec3{2, 0, 4}) || a.PivotMode != PivotModeCenter || a.VoxelResolution != .125) {
				t.Fatal("warm model lost declared bounds or lattice")
			}
			snapshot := VoxelObjectSnapshotFromXBrickMap(s1l6Map(t, left))
			voxel := snapshot.Voxels[0]
			if err := ApplyManagedVoxelWrites(f.cmd, f.assets, first, p1dWrites(volume.VoxelWrite{X: voxel.X, Y: voxel.Y, Z: voxel.Z, Value: 7})); err != nil {
				t.Fatal(err)
			}
			if ok, value := s1l6Map(t, right).GetVoxel(voxel.X, voxel.Y, voxel.Z); !ok || value != voxel.Value {
				t.Fatal("warm sibling inherited managed edit")
			}
			if ok, value := global.XBrickMap.GetVoxel(voxel.X, voxel.Y, voxel.Z); !ok || value != voxel.Value {
				t.Fatal("warm managed edit reached global source")
			}
			if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Adoptions != uint64(want) || stats.Entries != 0 || stats.Bytes != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
				t.Fatal("warm transfers leaked derivatives or pending credit")
			}
		})
	}
}

func TestS1pEveryMutableBorrowPermanentlyDisqualifiesWarmReuse(t *testing.T) {
	for _, api := range []string{"GetVoxelGeometry", "GetVoxelModel", "ResolveVoxelGeometry", "ResolveVoxelGeometryMap", "getVoxelGeometry", "resolveVoxelGeometry", "resolveVoxelGeometryMap"} {
		for _, timing := range []string{"before-worker", "after-worker"} {
			t.Run(api+"/"+timing, func(t *testing.T) {
				f, path := s1nRuntime(t, true, 1)
				shared, _ := s1pWarm(t, f, path, "body")
				borrow := func() {
					vm := VoxelModelComponent{SharedGeometry: shared}
					ok := false
					switch api {
					case "GetVoxelGeometry":
						_, ok = f.assets.GetVoxelGeometry(shared)
					case "GetVoxelModel":
						_, ok = f.assets.GetVoxelModel(shared)
					case "ResolveVoxelGeometry":
						_, _, ok = ResolveVoxelGeometry(f.assets, &vm)
					case "ResolveVoxelGeometryMap":
						_, ok = ResolveVoxelGeometryMap(f.assets, &vm)
					case "getVoxelGeometry":
						_, ok = f.assets.getVoxelGeometry(shared)
					case "resolveVoxelGeometry":
						_, _, ok = resolveVoxelGeometry(f.assets, &vm)
					case "resolveVoxelGeometryMap":
						_, ok = resolveVoxelGeometryMap(f.assets, &vm)
					}
					if !ok {
						t.Fatal("borrow fixture failed")
					}
				}
				if timing == "before-worker" {
					borrow()
				}
				s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
				if timing == "after-worker" {
					borrow()
				}
				// Even repeat verified preparation must not recertify an exposed asset.
				if id, _ := s1pWarm(t, f, path, "body"); id != shared {
					t.Fatal("warm prepare replaced global ID")
				}
				f.commitStage()
				model := s3cComponent[VoxelModelComponent](t, f.cmd, s1nBody(t, f, 0))
				if model.OverrideGeometry != (AssetId{}) || model.SharedGeometry != shared {
					t.Fatal("borrowed unedited source regained prepared authority")
				}
				state := s1nRenderer(f)
				s1nBridge(f, state)
				s1l6NoInput(t, state.GetVoxelObject(s1nBody(t, f, 0)))
				if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Entries != 0 || stats.Bytes != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
					t.Fatal("borrow fallback retained candidate credit")
				}
			})
		}
	}
}

func TestS1pRawPayloadAndAuxWritesPreserveWarmCompatibility(t *testing.T) {
	for _, timing := range []string{"before-worker", "after-worker"} {
		t.Run(timing, func(t *testing.T) {
			f, path := s1nRuntime(t, true, 1)
			shared, _ := s1pWarm(t, f, path, "body")
			raw, ok := f.assets.GetVoxelGeometry(shared)
			if !ok {
				t.Fatal("raw source missing")
			}
			mutate := func() {
				revision := raw.XBrickMap.Revision
				brick := raw.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0)
				brick.Payload[0][0][0] = 9
				brick.PrecomputedAux = []byte{17, 23, 31}
				if raw.XBrickMap.Revision != revision {
					t.Fatal("raw fixture unexpectedly changed revision")
				}
			}
			if timing == "before-worker" {
				mutate()
			}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			if timing == "after-worker" {
				mutate()
			}
			f.commitStage()
			eid := s1nBody(t, f, 0)
			model := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if model.OverrideGeometry != (AssetId{}) {
				t.Fatal("raw edited source received stale worker authority")
			}
			state := s1nRenderer(f)
			s1nBridge(f, state)
			obj := state.GetVoxelObject(eid)
			s1l6NoInput(t, obj)
			p1dVoxel(t, obj.XBrickMap, 0, 9)
			cloneID, ok := f.assets.CloneVoxelGeometry(shared)
			if !ok {
				t.Fatal("current compatibility clone failed")
			}
			cloned, _ := f.assets.GetVoxelGeometry(cloneID)
			brick := cloned.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0)
			if len(brick.PrecomputedAux) != 3 || brick.PrecomputedAux[0] != 17 || cloned.XBrickMap == raw.XBrickMap {
				t.Fatal("compatibility copy lost current aux or aliases global")
			}
			raw.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux[0] = 99
			if brick.PrecomputedAux[0] != 17 {
				t.Fatal("compatibility auxiliary storage aliases global")
			}
		})
	}
}

func TestS1pPlacementHookBorrowRevokesOnlyLaterTransfers(t *testing.T) {
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "cold-publication", true: "warm-publication"}[warm], func(t *testing.T) {
			f, path := s1nRuntime(t, true, 2)
			if warm {
				s1pWarm(t, f, path, "body")
			}
			calls := 0
			f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, context PostSpawnPlacementContext) {
				calls++
				eid := context.SpawnResult.EntitiesByAssetID["body"]
				vm := s3cComponent[VoxelModelComponent](t, cmd, eid)
				if calls == 1 {
					if vm.OverrideGeometry == (AssetId{}) {
						t.Fatal("first transfer did not qualify before exposure")
					}
					raw, _ := f.assets.GetVoxelGeometry(vm.SharedGeometry)
					raw.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][0][0] = 8
				} else if vm.OverrideGeometry != (AssetId{}) {
					t.Fatal("later transfer trusted stale publication eligibility after hook borrow")
				}
			}}
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			if calls != 2 {
				t.Fatal("hook fixture did not visit both placements")
			}
			state := s1nRenderer(f)
			s1nBridge(f, state)
			first, second := state.GetVoxelObject(s1nBody(t, f, 0)), state.GetVoxelObject(s1nBody(t, f, 1))
			p1dVoxel(t, s1l6Map(t, s1l6Input(t, first)), 0, 1)
			s1l6NoInput(t, second)
			p1dVoxel(t, second.XBrickMap, 0, 8)
			if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Adoptions != 1 || stats.Entries != 0 {
				t.Fatal("revoked later candidate was adopted or retained")
			}
		})
	}
}

func TestS1pPendingFramesAndNewChunkReuseRemainEligible(t *testing.T) {
	f, path := s1nRuntime(t, true, 2)
	shared, _ := s1pWarm(t, f, path, "body")
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 1
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	first := s1nBody(t, f, 0)
	state := s1nRenderer(f)
	s1nBridge(f, state)
	a := s1pManaged(t, f, first, shared)
	if placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 1), "body") != 0 || f.runtime.pendingPrepared.snapshot().Bytes == 0 {
		t.Fatal("fixture did not retain a pending sibling")
	}
	f.commitStage()
	b := s1pManaged(t, f, s1nBody(t, f, 1), shared)
	if a.OverrideGeometry == b.OverrideGeometry {
		t.Fatal("pending sibling reused managed owner")
	}
	s1nBridge(f, state)
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, first, p1dWrites(volume.VoxelWrite{Value: 7})); err != nil {
		t.Fatal(err)
	}
	coord := ChunkCoord{X: 1}
	placement := f.runtime.PlacementsByChunk[ChunkCoord{}][0]
	placement.PlacementID = s1gID(1, 0)
	placement.Transform.Position = content.Vec3{17, 1, 1}
	f.runtime.PlacementsByChunk[coord] = []streamedPlacementInstance{placement}
	f.runtime.DesiredChunks[coord] = struct{}{}
	f.runtime.KeepChunks[coord] = struct{}{}
	s1fPrepared(t, f.streamedRenderHarness, coord, false)
	f.commitStage()
	third := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(1, 0), "body")
	c := s1pManaged(t, f, third, shared)
	if c.OverrideGeometry == a.OverrideGeometry || c.OverrideGeometry == b.OverrideGeometry {
		t.Fatal("new chunk reused another placement owner")
	}
	s1nBridge(f, state)
	for _, eid := range []EntityId{first, s1nBody(t, f, 1), third} {
		s1l6Input(t, state.GetVoxelObject(eid))
	}
	if stats := f.assets.PreparedVoxelRendererCopyStats(); stats.Adoptions != 3 || stats.Entries != 0 || f.runtime.pendingPrepared.snapshot().Bytes != 0 {
		t.Fatal("multi-frame/new-chunk warm ownership leaked")
	}
}

func TestS1pDeleteAndReregisterDoNotInheritOldMutableAliases(t *testing.T) {
	f, path := s1nRuntime(t, true, 1)
	old, _ := s1pWarm(t, f, path, "body")
	alias, _ := f.assets.GetVoxelGeometry(old)
	if !f.assets.DeleteVoxelGeometry(old) {
		t.Fatal("old source deletion failed")
	}
	fresh, _ := s1pWarm(t, f, path, "body")
	if fresh == old {
		t.Fatal("fresh registration reused deleted asset ID")
	}
	alias.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][0][0] = 9
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := s1nBody(t, f, 0)
	s1pManaged(t, f, eid, fresh)
	state := s1nRenderer(f)
	s1nBridge(f, state)
	p1dVoxel(t, s1l6Map(t, s1l6Input(t, state.GetVoxelObject(eid))), 0, 1)
	global, _ := f.assets.GetVoxelGeometry(fresh)
	p1dVoxel(t, global.XBrickMap, 0, 1)
	p1dVoxel(t, alias.XBrickMap, 0, 9)
}

func TestS1pWarmOptOutKeepsGlobalAndCurrentPalette(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "opt-out", true: "opt-in"}[enabled], func(t *testing.T) {
			f, _ := s1nRuntime(t, enabled, 1)
			path, _ := c3g9Fixture(t, "models")
			f.runtime.Config.PlacementHooks = nil
			f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = path
			shared, paletteID := s1pWarm(t, f, path, "model")
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			palette, _ := f.assets.GetVoxelPalette(paletteID)
			palette.SurfaceMaterials[3] = VoxelSurfaceMaterial{Kind: "current", Tags: []string{"late"}}
			palette.Materials[0].Property["rough"] = .75
			f.commitStage()
			eid := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "model")
			vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if vm.SharedGeometry != shared || vm.VoxelPalette != paletteID {
				t.Fatal("warm commit replaced shared or palette IDs")
			}
			current, _ := f.assets.GetVoxelPalette(vm.VoxelPalette)
			if current.SurfaceMaterials[3].Kind != "current" || current.Materials[0].Property["rough"] != .75 {
				t.Fatal("worker replaced current mutable palette aliases")
			}
			if enabled {
				s1pManaged(t, f, eid, shared)
			} else if vm.OverrideGeometry != (AssetId{}) {
				t.Fatal("warm opt-out adopted managed authority")
			}
		})
	}
}

// A currently registered ordinary geometry keeps its mutable metadata and IDs.
// Independent candidates may not silently install different lattice/bounds/source
// authority over that registration, even when the geometry content key matches.
func TestS1pWarmRegistrationBindingMismatchPreservesExistingGeometry(t *testing.T) {
	for _, mismatch := range []string{"dimensions", "bounds", "source", "namespace"} {
		t.Run(mismatch, func(t *testing.T) {
			f, _ := s1nRuntime(t, true, 1)
			path, _ := c3g9Fixture(t, "models")
			f.runtime.Config.PlacementHooks = nil
			f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = path
			shared, _ := s1pWarm(t, f, path, "model")
			f.assets.mu.Lock()
			asset := f.assets.voxModels[shared]
			expected := asset
			switch mismatch {
			case "dimensions":
				asset.VoxModel.SizeX = 99
			case "bounds":
				asset.LocalMax = mgl32.Vec3{20, 21, 22}
			case "source":
				asset.SourcePath = "another-source"
			case "namespace":
				for key, id := range f.assets.voxModelKeys {
					if id == shared {
						delete(f.assets.voxModelKeys, key)
						f.assets.voxModelKeys["compiled-asset-shape:"+key[len("compiled-asset-model:"):]] = shared
						break
					}
				}
			}
			f.assets.voxModels[shared] = asset
			expected = asset
			f.assets.mu.Unlock()
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "model")
			vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if mismatch == "namespace" {
				if vm.SharedGeometry == shared {
					t.Fatal("model publication adopted shape namespace registration")
				}
				s1pManaged(t, f, eid, vm.SharedGeometry)
				return
			}
			if vm.SharedGeometry != shared || vm.OverrideGeometry != (AssetId{}) {
				t.Fatal("mismatched warm registration gained incompatible prepared authority")
			}
			current, _ := f.assets.GetVoxelGeometry(shared)
			if current.VoxModel.SizeX != expected.VoxModel.SizeX || current.LocalMax != expected.LocalMax || current.SourcePath != expected.SourcePath || current.XBrickMap != expected.XBrickMap {
				t.Fatal("fallback rewrote existing registration")
			}
		})
	}
}

func TestS1pLODPublicationBorrowPreventsPlainPartWarmAdoption(t *testing.T) {
	f, _ := s1nRuntime(t, true, 1)
	path := c3h13aFixture(t, "eligible")
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil || len(header.LODs) == 0 {
		t.Fatal("LOD fixture", err)
	}
	// LOD preparation's direct full/coarse reads are mutable borrows themselves.
	// This test only accesses scalar IDs before trying ordinary worker reuse.
	shared, _ := s1pWarm(t, f, path, "shape")
	header.LODs = nil
	plain := path + "-plain.gkassetc"
	if _, err := content.SaveCompiledAssetHeader(plain, header, nil); err != nil {
		t.Fatal(err)
	}
	f.runtime.Config.PlacementHooks = nil
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = plain
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "shape")
	vm := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	if vm.SharedGeometry != shared || vm.OverrideGeometry != (AssetId{}) {
		t.Fatal("LOD borrowed full geometry regained ordinary warm certificate")
	}
	state := s1nRenderer(f)
	s1nBridge(f, state)
	s1l6NoInput(t, state.GetVoxelObject(eid))
}

func TestS1pUnverifiedMatchingCacheRegistrationNeverQualifies(t *testing.T) {
	f, path := s1nRuntime(t, true, 1)
	packet, err := prepareCompiledAssetPacket(path, f.runtime.Loader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.release()
	shape := packet.shapes[packet.parts["body"]]
	source := shape.source.Copy()
	shared := f.assets.RegisterSharedVoxelGeometryWithCacheKey("compiled-asset-shape:"+shape.contentID, source, "compiled-asset-shape:"+shape.contentID)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	vm := s3cComponent[VoxelModelComponent](t, f.cmd, s1nBody(t, f, 0))
	if vm.SharedGeometry != shared || vm.OverrideGeometry != (AssetId{}) {
		t.Fatal("unverified matching cache registration was recertified from metadata")
	}
}

func TestS1pWarmOverrideExposureAfterTransferKeepsSiblingIsolation(t *testing.T) {
	f, path := s1nRuntime(t, true, 2)
	shared, _ := s1pWarm(t, f, path, "body")
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	first, second := s1nBody(t, f, 0), s1nBody(t, f, 1)
	a := s1pManaged(t, f, first, shared)
	s1pManaged(t, f, second, shared)
	state := s1nRenderer(f)
	s1nBridge(f, state)
	sibling := s1l6Input(t, state.GetVoxelObject(second))
	raw, _ := f.assets.GetVoxelGeometry(a.OverrideGeometry)
	raw.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][0][0] = 8
	s1nBridge(f, state)
	s1l6NoInput(t, state.GetVoxelObject(first))
	p1dVoxel(t, state.GetVoxelObject(first).XBrickMap, 0, 8)
	current := s1l6Input(t, state.GetVoxelObject(second))
	if !current.SameSource(sibling) || current.Generation() != sibling.Generation() {
		t.Fatal("override exposure changed sibling producer")
	}
	p1dVoxel(t, s1l6Map(t, current), 0, 1)
	global, _ := f.assets.GetVoxelGeometry(shared)
	p1dVoxel(t, global.XBrickMap, 0, 1)
}

func TestS1pStopRestartPreservesUnborrowedGlobalEligibility(t *testing.T) {
	f, path := s1nRuntime(t, true, 1)
	// Restart reloads the persisted level rather than the fixture's in-memory
	// placement table, so its authoring reference must also use compiled input.
	level, err := content.LoadLevel(f.runtime.Config.LevelPath)
	if err != nil {
		t.Fatal(err)
	}
	level.Placements[0].AssetPath = path
	if err := content.SaveLevel(f.runtime.Config.LevelPath, level); err != nil {
		t.Fatal(err)
	}
	shared, _ := s1pWarm(t, f, path, "body")
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	oldEntity := s1nBody(t, f, 0)
	old := s1pManaged(t, f, oldEntity, shared)
	state := s1nRenderer(f)
	s1nBridge(f, state)
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	s1nBridge(f, state)
	if f.cmd.EntityExists(oldEntity) || f.assets.managedVoxelEntry(old.OverrideGeometry) != nil || state.GetVoxelObject(oldEntity) != nil {
		t.Fatal("Stop retained prior entity, managed authority or renderer object")
	}
	config := f.runtime.Config
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	freshEntity := s1nBody(t, f, 0)
	fresh := s1pManaged(t, f, freshEntity, shared)
	if fresh.OverrideGeometry == old.OverrideGeometry {
		t.Fatal("restart reused retired managed override")
	}
	s1nBridge(f, state)
	p1dVoxel(t, s1l6Map(t, s1l6Input(t, state.GetVoxelObject(s1nBody(t, f, 0)))), 0, 1)
}
