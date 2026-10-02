package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestS1eStreamingWorkOlderCapturedGenerationReleasesItsOwnAttempt(t *testing.T) {
	t.Run("discard-full-preserves-current-attempt", func(t *testing.T) {
		f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}}, 2, 1, false)
		coord := ChunkCoord{}
		f.runtime.DesiredChunks[coord] = struct{}{}
		// Match the existing S2b stale-job fixture: the job itself captures an
		// older generation before dispatch. Its worker publishes that identity.
		old := buildStreamedChunkLoadJob(f.runtime, coord)
		old.Generation--
		startStreamedChunkPrepareJob(f.runtime, old)
		s1eQueued(t, f.runtime, 1)
		s1eWork(t, f.runtime, 1, 2, 0, 0)

		// A separate decode cache lets the successor read the same valid source
		// while its completion is held. Both attempts have real worker lifetimes.
		current := buildStreamedChunkLoadJob(f.runtime, coord)
		current.Loader = NewRuntimeContentLoader()
		path := content.ResolveImportedWorldChunkPath(*current.ImportedWorldEntry, current.ImportedWorldManifestPath)
		_, release, done := s2eHoldDecode(t, current.Loader, path, nil)
		startStreamedChunkPrepareJob(f.runtime, current)
		s2bUntil(t, func() bool { return current.Loader.Stats().LoadWaits == 1 })
		s1eWork(t, f.runtime, 2, 2, 0, 0)
		f.commitStage()
		if f.runtime.LoadedChunks[coord] != nil || f.runtime.InitErr != nil {
			t.Fatal("older captured generation committed or poisoned the current world")
		}
		// Discard releases only the stale attempt. The held successor retains
		// its item despite sharing the old attempt's coordinate.
		s1eWork(t, f.runtime, 1, 2, 0, 0)
		release()
		if result := s2bWait(t, done); result.err != nil {
			t.Fatal(result.err)
		}
		s1eQueued(t, f.runtime, 1)
		f.commitStage()
		s1eImported(t, f, coord)
		s1eWork(t, f.runtime, 0, 2, 0, 0)
		if err := StopStreamedLevelRuntime(f.cmd); err != nil {
			t.Fatal(err)
		}
		s1eWork(t, f.runtime, 0, 2, 0, 0)
	})

	t.Run("stop-drains-older-proxy-without-gpu-debt", func(t *testing.T) {
		f, _ := s1eRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}, proxy: true}}, 1, 1, false)
		coord := ChunkCoord{}
		f.runtime.DesiredProxySectors[coord] = struct{}{}
		job := buildStreamedSectorProxyLoadJob(f.runtime, coord, f.runtime.ImportedWorldSectors[coord].LODs[0])
		job.Generation--
		startStreamedSectorProxyPrepareJob(f.runtime, job)
		s1eQueued(t, f.runtime, 1)
		s1eWork(t, f.runtime, 1, 1, 0, 0)
		if err := StopStreamedLevelRuntime(f.cmd); err != nil {
			t.Fatal(err)
		}
		// No renderer or committed target exists: draining a CPU result must
		// not leave a phantom retiring GPU item after successful Stop.
		s1eWork(t, f.runtime, 0, 1, 0, 0)
	})
}
