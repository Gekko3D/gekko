package gekko

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func e2b3Enable(t *testing.T, f *s1gFixture, eid EntityId) AssetId {
	t.Helper()
	if err := EnableManagedVoxelGeometry(f.cmd, f.assets, eid); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	return mustVoxelModelComponentForLevelTest(t, f.cmd, eid).OverrideGeometry
}

func e2b3Qualified(t *testing.T, f *s1gFixture, eid EntityId, id string, part content.AssetPartDef) {
	t.Helper()
	_, lattice, identity := e2b1Canonical(t, part)
	got, gotLattice, ok := managedVoxelPersistenceBase(f.cmd, f.assets, f.runtime, eid, id, "body")
	if !ok || got != identity || gotLattice != lattice {
		t.Fatalf("persistence base=(%q,%+v,%v), want (%q,%+v,true)", got, gotLattice, ok, identity, lattice)
	}
}

func e2b3Fallback(t *testing.T, f *s1gFixture, state *StreamedLevelRuntimeState, eid EntityId, placement, item string) {
	t.Helper()
	identity, lattice, ok := managedVoxelPersistenceBase(f.cmd, f.assets, state, eid, placement, item)
	if ok || identity != "" || lattice != (content.VoxelObjectLatticeDef{}) {
		t.Fatalf("unqualified persistence base=(%q,%+v,%v)", identity, lattice, ok)
	}
}

func e2b3Pristine(t *testing.T) (*s1gFixture, content.AssetPartDef, EntityId) {
	t.Helper()
	f, part, _ := e2b2Runtime(t)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	return f, part, e2b2Body(t, f, s1gID(0, 0))
}

func TestE2b3PristineStreamOwnedBindingAndSourceDeletion(t *testing.T) {
	for _, placement := range []string{s1gID(0, 0), "P\x00nested"} {
		t.Run(placement, func(t *testing.T) {
			f, part, _ := e2b2Runtime(t)
			f.runtime.PlacementsByChunk[ChunkCoord{}][0].PlacementID = placement
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			f.commitStage()
			eid := e2b2Body(t, f, placement)
			if hasComponentOfType[StreamedVoxelRenderComponent](f.cmd, eid) {
				t.Fatal("ordinary authored body unexpectedly acquired a render ticket")
			}
			source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
			override := e2b3Enable(t, f, eid)
			e2b3Qualified(t, f, eid, placement, part)
			if placement == "P\x00nested" {
				e2b3Fallback(t, f, f.runtime, eid, "P", "nested\x00body")
			}
			if lease := f.runtime.snapshotGeometryAssets[eid]; lease.ID != override || lease.Server != f.assets {
				t.Fatal("stream-owned managed override lacks exact existing lifetime lease")
			}
			if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: -9, Value: 7})); err != nil {
				t.Fatal(err)
			}
			e2b3Qualified(t, f, eid, placement, part)
			if !f.assets.DeleteVoxelGeometry(source) {
				t.Fatal("source deletion failed")
			}
			e2b3Qualified(t, f, eid, placement, part)
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			p5cAssetPresent(t, f, override, false)
		})
	}
}

func TestE2b3BindingRejectsChangedRefsLatticeStateAndForgedMembership(t *testing.T) {
	f, part, eid := e2b3Pristine(t)
	id := s1gID(0, 0)
	source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
	e2b3Enable(t, f, eid)
	e2b3Qualified(t, f, eid, id, part)
	ref := s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid)
	original := *ref
	for _, field := range []string{"level", "asset", "placement", "item", "path"} {
		*ref = original
		switch field {
		case "level":
			ref.LevelID = "different"
		case "asset":
			ref.AssetID = "different"
		case "placement":
			ref.PlacementID = "different"
		case "item":
			ref.ItemID = "different"
		case "path":
			ref.AssetPath = "different.gkasset"
		}
		e2b3Fallback(t, f, f.runtime, eid, id, "body")
		if field == "level" {
			if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: 40, Value: 7})); err != nil {
				t.Fatal("reference drift disabled full-fallback managed editing", err)
			}
		}
	}
	*ref = original
	vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	vmc.VoxelResolution = 2
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	vmc.VoxelResolution = part.VoxelResolution
	otherState := &StreamedLevelRuntimeState{Generation: f.runtime.Generation, LevelID: f.runtime.LevelID}
	e2b3Fallback(t, f, otherState, eid, id, "body")
	e2b3Fallback(t, f, nil, eid, id, "body")
	f.runtime.Generation++
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	f.runtime.Generation--
	key := voxelObjectRuntimeKey(id, "body")
	coord := f.runtime.ObjectChunk[key]
	delete(f.runtime.ObjectChunk, key)
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	f.runtime.ObjectChunk[key] = coord
	chunk := f.runtime.LoadedChunks[coord]
	chunk.ObjectEntities[key] = 0
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	chunk.ObjectEntities[key] = eid
	delete(chunk.OwnedEntities, eid)
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	chunk.OwnedEntities[eid] = struct{}{}
	e2b3Fallback(t, f, f.runtime, eid, "wrong", "body")
	e2b3Fallback(t, f, f.runtime, eid, id, "wrong")
	changed := original
	changed.AssetID = "pending-other"
	f.cmd.AddComponents(eid, changed)
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	f.cmd.AddComponents(eid, original)
	e2b3Qualified(t, f, eid, id, part)
	f.app.FlushCommands()
	f.cmd.RemoveComponents(eid, AuthoredLevelItemRefComponent{})
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	f.cmd.AddComponents(eid, original)
	e2b3Qualified(t, f, eid, id, part)
	f.app.FlushCommands()
	// Identical authored references are insufficient without actual chunk membership.
	model := f.assets.RegisterSharedVoxelGeometry(volume.NewXBrickMap(), "outside")
	fake := f.cmd.AddEntity(s3cTransform(30), VoxelModelComponent{VoxelModel: source, VoxelResolution: part.VoxelResolution}, original)
	f.app.FlushCommands()
	fakeOverride := e2b3Enable(t, f, fake)
	e2b3Fallback(t, f, f.runtime, fake, id, "body")
	if _, leased := f.runtime.snapshotGeometryAssets[fake]; leased {
		t.Fatal("forged authored reference acquired stream-owned lease")
	}
	_, _ = f.assets.GetVoxelGeometry(mustVoxelModelComponentForLevelTest(t, f.cmd, eid).OverrideGeometry)
	e2b3Fallback(t, f, f.runtime, eid, id, "body")
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	p5cAssetPresent(t, f, fakeOverride, true)
	p5cAssetPresent(t, f, model, true)
	f.assets.DeleteVoxelGeometry(fakeOverride)
	f.assets.DeleteVoxelGeometry(model)
}

func TestE2b3DifferentCanonicalGeometryForAuthoredRefUsesFullFallback(t *testing.T) {
	f, part, eid := e2b3Pristine(t)
	other := part
	shape := *part.Source.VoxelShape
	shape.Voxels = append([]content.VoxelObjectVoxelDef(nil), shape.Voxels...)
	shape.Voxels[0].Value = 2
	other.Source.VoxelShape = &shape
	geometry, err := authoredVoxelShapeGeometry(f.assets, other)
	if err != nil {
		t.Fatal(err)
	}
	vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	vmc.VoxelModel, vmc.SharedGeometry = geometry, geometry
	f.cmd.AddComponents(eid, vmc)
	f.app.FlushCommands()
	e2b3Enable(t, f, eid)
	e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: 40, Value: 7})); err != nil {
		t.Fatal(err)
	}
	if changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.assets, eid); !tracked || len(changes) != 1 {
		t.Fatal("binding fallback disabled ordinary managed editing")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	p5cAssetPresent(t, f, geometry, true)
}

func TestE2b3SelfConsistentForeignAuthoredRefCannotReplaceActualPlacementOwner(t *testing.T) {
	f, part, eid := e2b3Pristine(t)
	shape := *part.Source.VoxelShape
	shape.Voxels = append([]content.VoxelObjectVoxelDef(nil), shape.Voxels...)
	shape.Voxels[0].Value = 2
	shape.Palette = append(append([]content.AssetVoxelPaletteEntryDef(nil), shape.Palette...), content.AssetVoxelPaletteEntryDef{Value: 2, MaterialID: "mat"})
	part.Source.VoxelShape = &shape
	foreignAsset := content.NewAssetDef("foreign-authored-B")
	foreignAsset.ID = "foreign-authored-B"
	foreignAsset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: false}
	foreignAsset.Parts = []content.AssetPartDef{part}
	foreignAsset.Materials = []content.AssetMaterialDef{{ID: "mat", Name: "mat", BaseColor: [4]uint8{100, 100, 100, 255}}}
	foreignPath := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "foreign-B.gkasset")
	if err := content.SaveAsset(foreignPath, foreignAsset); err != nil {
		t.Fatal(err)
	}
	geometry, err := authoredVoxelShapeGeometry(f.assets, part)
	if err != nil {
		t.Fatal(err)
	}
	model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	model.VoxelModel, model.SharedGeometry, model.OverrideGeometry = geometry, geometry, AssetId{}
	ref := *s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid)
	ref.AssetID, ref.AssetPath = foreignAsset.ID, foreignPath
	f.cmd.AddComponents(eid, model, ref)
	e2b3Enable(t, f, eid)
	_, lattice, identity := e2b1Canonical(t, part)
	got, gotLattice, canonical := managedVoxelGeometryBase(f.cmd, f.assets, eid)
	if !canonical || got != identity || gotLattice != lattice {
		t.Fatal("fixture did not retain self-consistent foreign shape provenance")
	}
	e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: 40, Value: 7})); err != nil {
		t.Fatal("foreign authored binding fallback disabled managed editing", err)
	}
}

func TestE2b3ManagedLeaseCleanupUnloadStopAndRemovedEntity(t *testing.T) {
	for _, cleanup := range []string{"unload", "stop", "removed-then-unload"} {
		t.Run(cleanup, func(t *testing.T) {
			f, _, eid := e2b3Pristine(t)
			source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
			override := e2b3Enable(t, f, eid)
			if cleanup == "removed-then-unload" {
				f.cmd.RemoveEntity(eid)
				f.app.FlushCommands()
			}
			var err error
			if cleanup == "stop" {
				err = StopStreamedLevelRuntime(f.cmd)
			} else {
				err = unloadStreamedChunk(f.cmd, f.runtime, ChunkCoord{})
			}
			if err != nil {
				t.Fatal(err)
			}
			f.app.FlushCommands()
			p5cAssetPresent(t, f, override, false)
			p5cAssetPresent(t, f, source, true)
			if _, leased := f.runtime.snapshotGeometryAssets[eid]; leased {
				t.Fatal("retired entity retained managed lease")
			}
		})
	}
}

func TestE2b3SnapshotLeaseReplacementClonesBeforeDeletingOldAsset(t *testing.T) {
	f, _ := p5cRuntime(t, 1, false)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	_, eid := s1gPlacement(t, f, s1gID(0, 0))
	old, asset := p5cAsset(t, f, eid)
	unrelated := f.assets.RegisterSharedVoxelGeometry(asset.XBrickMap, "unrelated")
	current := e2b3Enable(t, f, eid)
	if current == old || f.runtime.snapshotGeometryAssets[eid].ID != current || len(f.runtime.snapshotGeometryAssets) != 1 {
		t.Fatal("managed clone did not replace exact prior lifetime lease")
	}
	p5cAssetPresent(t, f, old, false)
	mapForSave, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	if present, value := mapForSave.GetVoxel(1, 2, 3); !present || value != 3 {
		t.Fatal("old leased snapshot deleted before independent clone")
	}
	e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	p5cAssetPresent(t, f, current, false)
	p5cAssetPresent(t, f, unrelated, true)
}

func TestE2b3HookQualificationBeforePublicationAndFatalLeasePins(t *testing.T) {
	for _, fatal := range []bool{false, true} {
		t.Run(map[bool]string{false: "synchronous", true: "fatal-resumable"}[fatal], func(t *testing.T) {
			f, part, _ := e2b2Runtime(t)
			if !fatal {
				f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 0
			}
			var eid EntityId
			var override AssetId
			f.runtime.Config.PlacementHooks = append(f.runtime.Config.PlacementHooks, func(cmd *Commands, context PostSpawnPlacementContext) {
				eid = context.SpawnResult.EntitiesByAssetID["body"]
				if f.runtime.LoadedChunks[ChunkCoord{}] != nil {
					t.Fatal("fixture hook ran after chunk publication")
				}
				if err := EnableManagedVoxelGeometry(cmd, f.assets, eid); err != nil {
					t.Fatal(err)
				}
				e2b3Qualified(t, f, eid, context.Placement.PlacementID, part)
				if err := ApplyManagedVoxelWrites(cmd, f.assets, eid, p1dWrites(volume.VoxelWrite{X: -9, Value: 7})); err != nil {
					t.Fatal(err)
				}
				if fatal {
					f.runtime.InitErr = errors.New("E2b3 hook failure")
				}
			})
			s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
			commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
			f.app.FlushCommands()
			if eid == 0 {
				t.Fatal("hook did not execute")
			}
			override = mustVoxelModelComponentForLevelTest(t, f.cmd, eid).OverrideGeometry
			if override == (AssetId{}) || f.runtime.snapshotGeometryAssets[eid].ID != override {
				t.Fatal("hook-created override lacked scoped transaction ownership")
			}
			if fatal {
				if f.runtime.InitErr == nil || f.runtime.LoadedChunks[ChunkCoord{}] != nil || f.runtime.Metrics.PendingPreparedBytes <= 0 || f.runtime.Metrics.ActiveChunkCommitCount != 1 {
					t.Fatal("fatal hook failed to pin transaction ownership")
				}
			} else if f.runtime.InitErr != nil || f.runtime.LoadedChunks[ChunkCoord{}] == nil {
				t.Fatal("synchronous qualified hook failed publication")
			}
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			p5cAssetPresent(t, f, override, false)
			if f.runtime.Metrics.PendingPreparedBytes != 0 || f.runtime.Metrics.ActiveChunkCommitCount != 0 {
				t.Fatal("stop retained hook-created lease or transaction pin")
			}
		})
	}
}

func TestE2b3RepeatedEnableSourceReplacementAndPendingCapture(t *testing.T) {
	for _, badPendingRef := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreign-replacement", true: "incorrect-pending-ref"}[badPendingRef], func(t *testing.T) {
			f, part, eid := e2b3Pristine(t)
			id := s1gID(0, 0)
			source := s3cComponent[VoxelModelComponent](t, f.cmd, eid).GeometryAsset()
			old := e2b3Enable(t, f, eid)
			originalRef := *s3cComponent[AuthoredLevelItemRefComponent](t, f.cmd, eid)
			mapForSave, _, _ := currentVoxelMapForEntity(f.cmd, eid)
			unrelated := f.assets.RegisterSharedVoxelGeometry(mapForSave, "foreign-replacement")
			model := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			model.OverrideGeometry = unrelated
			if badPendingRef {
				model.OverrideGeometry, model.VoxelModel, model.SharedGeometry = AssetId{}, source, source
				incorrect := originalRef
				incorrect.AssetID = "incorrect-pending-asset"
				f.cmd.AddComponents(eid, incorrect)
			}
			f.cmd.AddComponents(eid, model)
			current := e2b3Enable(t, f, eid)
			if current == old || current == unrelated || current == (AssetId{}) || len(f.runtime.snapshotGeometryAssets) != 1 || f.runtime.snapshotGeometryAssets[eid].ID != current {
				t.Fatal("source replacement retained old lease or took foreign ownership")
			}
			p5cAssetPresent(t, f, old, false)
			p5cAssetPresent(t, f, unrelated, true)
			if repeated := e2b3Enable(t, f, eid); repeated != current {
				t.Fatal("repeated enable duplicated current override")
			}
			if badPendingRef {
				f.cmd.AddComponents(eid, originalRef)
				f.app.FlushCommands()
				e2b3Fallback(t, f, f.runtime, eid, id, "body")
				_, lattice, identity := e2b1Canonical(t, part)
				got, gotLattice, qualified := managedVoxelGeometryBase(f.cmd, f.assets, eid)
				if !qualified || got != identity || gotLattice != lattice {
					t.Fatal("fixture did not retain ordinary canonical shape qualification")
				}
			}
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			p5cAssetPresent(t, f, current, false)
			p5cAssetPresent(t, f, unrelated, true)
			p5cAssetPresent(t, f, source, true)
		})
	}
}

func TestE2b3SynchronousHookContextUnwindsAfterPanic(t *testing.T) {
	f, part, _ := e2b2Runtime(t)
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 0
	marker := &struct{}{}
	var eid EntityId
	f.runtime.Config.PlacementHooks = append(f.runtime.Config.PlacementHooks, func(cmd *Commands, context PostSpawnPlacementContext) {
		eid = context.SpawnResult.EntitiesByAssetID["body"]
		if err := EnableManagedVoxelGeometry(cmd, f.assets, eid); err != nil {
			t.Fatal(err)
		}
		e2b3Qualified(t, f, eid, context.Placement.PlacementID, part)
		panic(marker)
	})
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	func() {
		defer func() {
			if got := recover(); got != marker {
				t.Fatalf("hook panic=%v, want original", got)
			}
		}()
		commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	}()
	f.app.FlushCommands()
	if eid == 0 {
		t.Fatal("panic hook did not execute")
	}
	override := mustVoxelModelComponentForLevelTest(t, f.cmd, eid).OverrideGeometry
	e2b3Fallback(t, f, f.runtime, eid, s1gID(0, 0), "body")
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	p5cAssetPresent(t, f, override, false)
}
