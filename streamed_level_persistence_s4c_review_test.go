package gekko

import (
	"reflect"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestS4cBackingRemovalPreventsHeldFullSnapshotResurrection(t *testing.T) {
	app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
	entity, geometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "backing-ack")
	cmd.AddComponents(entity, StreamedDestructionResidentComponent{})
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	app.FlushCommands()
	entered, gate := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(gate) }) }
	t.Cleanup(func() { release(); state.jobs.Wait(); state.worldDeltaWriter = nil })
	state.worldDeltaWriter = func(path string, delta *content.WorldDeltaDef) error {
		once.Do(func() { close(entered); <-gate })
		return content.SaveWorldDelta(path, delta)
	}
	s4cDriveUntilWriterEntered(t, cmd, state, entered)
	removal := &content.VoxelBackingRemovalDef{
		OwnerKind: content.VoxelBackingOwnerImportedWorld, OwnerID: state.BaseWorldID, SourceHash: "source",
		Bricks: []content.VoxelBackingRemovalBrickDef{{Bits: [16]uint32{1}, Material: 1}},
	}
	provider := NewTerrainColumnVoxelBacking(&content.TerrainChunkDef{ChunkSize: 16, VoxelResolution: 1, SolidValue: 1, Columns: []content.TerrainChunkColumnDef{{FilledVoxels: 1}}})
	backing := NewVoxelBackingComponent(content.VoxelBackingOwnerImportedWorld, state.BaseWorldID, "source", [3]int{}, 16, provider, removal)
	backing.Dirty = true
	state.recordVoxelBackingRemoval(backing)
	state.WorldDelta.VoxelBackingRemovals = mapVoxelBackingRemovals(state.voxelBackingRemovalMap)
	cmd.AddComponents(entity, backing)
	s4cLive(t, cmd, assets, entity).SetVoxel(0, 0, 0, 0)
	cmd.AddEntity(TransformComponent{}, StreamedLevelObserverComponent{Radius: 1, KeepRadius: 1})
	app.FlushCommands()
	requestStreamedWorldDeltaSave(state)
	release()
	s4cDrive(t, cmd, state, func() bool {
		delta, err := content.LoadWorldDelta(state.WorldDeltaPath)
		return err == nil && len(delta.VoxelBackingRemovals) == 1 && state.Metrics.PendingPersistenceCount == 0
	})
	durable := s4aLoadDelta(t, state.WorldDeltaPath)
	want := []content.VoxelBackingRemovalDef{*removal}
	if len(mapImportedWorldOverrides(state.importedWorldOverrideMap)) != 0 || len(state.WorldDelta.ImportedWorldChunkOverrides) != 0 || len(durable.ImportedWorldChunkOverrides) != 0 || !reflect.DeepEqual(state.WorldDelta.VoxelBackingRemovals, want) || !reflect.DeepEqual(durable.VoxelBackingRemovals, want) {
		t.Fatalf("held full snapshot ACK resurrected geometry over newer backing removal: current=%+v durable=%+v", state.WorldDelta, durable)
	}
	if !cmd.EntityExists(entity) || state.LoadedChunks[ChunkCoord{}] == nil {
		t.Fatal("checkpoint ACK released demanded backing-removal owner")
	}
	asset, ok := assets.GetVoxelGeometry(geometry)
	if !ok || asset.XBrickMap == nil {
		t.Fatal("checkpoint ACK released live geometry lease")
	}
	s4aVoxel(t, asset.XBrickMap, 0, 0)
}

func TestS4cLegacyDirtyOwnerChangeRecapturesLatestMetadataAfterUpload(t *testing.T) {
	_, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{})
	entity, _ := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "metadata-recapture")
	live := s4cLive(t, cmd, assets, entity)
	live.SetVoxel(0, 0, 0, 9)
	release := s4cHoldImportedWriter(t, cmd, state)
	s3cComponent[AuthoredImportedWorldChunkRefComponent](t, cmd, entity).WorldID = "new-owner"
	live.ClearDirty()
	release()
	s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
	delta := s4aLoadDelta(t, state.WorldDeltaPath)
	if len(delta.ImportedWorldChunkOverrides) != 1 || delta.ImportedWorldChunkOverrides[0].WorldID != "new-owner" {
		t.Fatalf("legacy dirty recapture retained obsolete owner metadata: %+v", delta.ImportedWorldChunkOverrides)
	}
	s4cDurableValue(t, state, "imported", 9)
	if cmd.EntityExists(entity) || state.Metrics.PendingPersistenceCount != 0 || state.Metrics.PendingPersistenceBytes != 0 || state.Metrics.DirtyPinnedChunkCount != 0 {
		t.Fatalf("metadata recapture retained completed dirty ownership: %+v", state.Metrics)
	}
}
