package gekko

import (
	"errors"
	"testing"
)

func TestS2eLoadedReplacementKeepsObsoleteDispatchCancelledThroughResidencyUpgrade(t *testing.T) {
	cmd, state, assets, _, path := s2eRuntime(t, false)
	failure := errors.New("obsolete decode completed after replacement upgrade")
	_, release, done := s2eHoldDecode(t, state.Loader, path, failure)
	s2eDispatchHeld(t, cmd, state)

	// Publish a real same-coordinate owner while the earlier worker waits. A
	// missing collider then asks the runtime to upgrade this clean replacement.
	entity, _ := s2aCommitImported(t, cmd, assets, state, ChunkCoord{}, "review-replacement")
	if state.LoadedChunks[ChunkCoord{}] == nil || !hasComponentOfType[ColliderComponent](cmd, entity) {
		t.Fatal("fixture did not publish a loaded replacement with collision")
	}
	cmd.RemoveComponents(entity, &ColliderComponent{})
	cmd.app.FlushCommands()
	if hasComponentOfType[ColliderComponent](cmd, entity) {
		t.Fatal("fixture did not request a collision residency upgrade")
	}
	updateStreamedLevelObserverSystem(cmd, state)
	if _, desired := state.DesiredChunks[ChunkCoord{}]; !desired || state.LoadedChunks[ChunkCoord{}] != nil {
		t.Fatal("fixture did not remove the clean replacement while retaining demand")
	}

	release()
	if shared := s2bWait(t, done); !errors.Is(shared.err, failure) {
		t.Fatalf("independent shared decode lost its error: %v", shared.err)
	}
	s2eWaitPrepared(t, state, false)
	commitPreparedStreamedChunksSystem(cmd, assets, state)
	s2eAssertCancelled(t, state, 1)
	if state.LoadedChunks[ChunkCoord{}] != nil {
		t.Fatal("obsolete completion replaced the owner awaiting residency upgrade")
	}

	s2bUntil(t, func() bool {
		updateStreamedLevelObserverSystem(cmd, state)
		commitPreparedStreamedChunksSystem(cmd, assets, state)
		cmd.app.FlushCommands()
		return s2eLoaded(state, false) || state.InitErr != nil
	})
	if !s2eLoaded(state, false) || state.InitErr != nil {
		t.Fatalf("fresh dispatch could not restore residency after upgrade: %v", state.InitErr)
	}
	if len(state.LoadedChunks[ChunkCoord{}].ImportedWorldEntities) != 1 {
		t.Fatal("fresh dispatch did not restore the unique imported geometry owner")
	}
	for fresh := range state.LoadedChunks[ChunkCoord{}].ImportedWorldEntities {
		if !hasComponentOfType[ColliderComponent](cmd, fresh) {
			t.Fatal("fresh owner did not restore required collision")
		}
		model := mustVoxelModelComponentForLevelTest(t, cmd, fresh)
		if geometry, ok := ResolveVoxelGeometryMap(assets, &model); !ok || geometry == nil || geometry.GetVoxelCount() == 0 {
			t.Fatal("fresh owner has unusable geometry")
		}
	}
}
