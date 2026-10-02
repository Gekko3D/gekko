package gekko

import (
	"errors"
	"os"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestS4cRemovedImportedOwnerDiscardsHeldCapture(t *testing.T) {
	app, cmd, state, assets := s2aStartRuntime(t, StreamedLevelRuntimeConfig{MaxPreparedGeometryCacheBytes: 1})
	entity, geometry := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "removed-owner")
	MarkVoxelEntityPersistenceDirty(cmd, entity)
	app.FlushCommands()
	release := s4cHoldImportedWriter(t, cmd, state)
	defer release()
	cmd.RemoveEntity(entity)
	app.FlushCommands()
	release()
	s4cDrive(t, cmd, state, func() bool { return len(state.LoadedChunks) == 0 })
	if cmd.EntityExists(entity) {
		t.Fatal("unload resurrected removed imported entity")
	}
	if _, present := assets.GetVoxelGeometry(geometry); present {
		t.Fatal("removed imported owner retained geometry lease")
	}
	if state.Metrics.PendingPersistenceCount != 0 || state.Metrics.PendingPersistenceBytes != 0 || state.Metrics.DirtyPinnedChunkCount != 0 {
		t.Fatalf("removed imported owner retained persistence ownership: %+v", state.Metrics)
	}
	delta, err := content.LoadWorldDelta(state.WorldDeltaPath)
	if errors.Is(err, os.ErrNotExist) {
		return // Discarding an unsaved removed owner need not create a manifest.
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.ImportedWorldChunkOverrides) != 0 {
		t.Fatalf("stale capture published a removed imported owner: %+v", delta.ImportedWorldChunkOverrides)
	}
}
