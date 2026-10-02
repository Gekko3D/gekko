package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS4cRememberedDirtySiblingSurvivesMatchingCheckpoint(t *testing.T) {
	app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
	imported, importedGeometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "sibling-checkpoint")
	objectMap := volume.NewXBrickMap()
	objectMap.SetVoxel(0, 0, 0, 2)
	object := cmd.AddEntity(VoxelModelComponent{VoxelModel: assets.RegisterSharedVoxelGeometry(objectMap, ""), VoxelResolution: 1})
	MarkVoxelEntityPersistenceDirty(cmd, imported)
	app.FlushCommands()
	loaded := state.LoadedChunks[ChunkCoord{}]
	loaded.OwnedEntities[object] = struct{}{}
	loaded.ObjectEntities = map[string]EntityId{voxelObjectRuntimeKey("placement", "item"): object}
	liveObject := s4cLive(t, cmd, assets, object)
	liveObject.ClearDirty()
	if VoxelEntityPersistenceDirty(cmd, object) {
		t.Fatal("fixture object must begin clean")
	}

	release := s4cHoldImportedWriter(t, cmd, state)
	defer release()
	// This sibling becomes dirty after the imported checkpoint was captured.
	liveObject.SetVoxel(0, 0, 0, 7)
	updateStreamedLevelObserverSystem(cmd, state)
	// Renderer upload consumes its queues after the observer notices the edit.
	liveObject.ClearDirty()
	if !cmd.EntityExists(imported) || !cmd.EntityExists(object) {
		t.Fatal("held checkpoint released live chunk entities")
	}
	release()
	s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
	if cmd.EntityExists(imported) || cmd.EntityExists(object) {
		t.Fatal("completed unload retained chunk entities")
	}
	if _, present := assets.GetVoxelGeometry(importedGeometry); present {
		t.Fatal("completed unload retained imported geometry lease")
	}
	if state.Metrics.PendingPersistenceCount != 0 || state.Metrics.PendingPersistenceBytes != 0 || state.Metrics.DirtyPinnedChunkCount != 0 {
		t.Fatalf("completed unload retained persistence ownership: %+v", state.Metrics)
	}
	delta := s4aLoadDelta(t, state.WorldDeltaPath)
	s4aVoxel(t, s4aLoadPayload(t, "imported", s4aPayloadPath(t, delta, "imported", state.WorldDeltaPath)), 0, 1)
	if len(delta.VoxelObjectOverrides) != 1 {
		t.Fatalf("matching imported checkpoint discarded remembered sibling edit: object overrides=%+v", delta.VoxelObjectOverrides)
	}
	ref := delta.VoxelObjectOverrides[0]
	if ref.PlacementID != "placement" || ref.ItemID != "item" {
		t.Fatalf("saved sibling has wrong ownership: %+v", ref)
	}
	snapshot, err := content.LoadVoxelObjectSnapshot(content.ResolveDocumentPath(ref.SnapshotPath, state.WorldDeltaPath))
	if err != nil {
		t.Fatal(err)
	}
	s4aVoxel(t, XBrickMapFromVoxelObjectSnapshot(snapshot), 0, 7)
}
