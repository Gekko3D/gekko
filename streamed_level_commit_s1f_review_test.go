package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestS1fCommitHookStopInvalidatesRetainedFrontier(t *testing.T) {
	f, _ := s1fRuntime(t, []s1dChunkSpec{{coord: ChunkCoord{}}, {coord: ChunkCoord{X: 1}}}, 1, 1)
	// Reuse real authored terrain to call Stop from its production spawn hook.
	terrainPath := s2bWorldPath(t)
	terrainLevel, err := content.LoadLevel(terrainPath)
	if err != nil {
		t.Fatal(err)
	}
	level, err := content.LoadLevel(f.runtime.LevelPath)
	if err != nil {
		t.Fatal(err)
	}
	level.Terrain = terrainLevel.Terrain
	level.Terrain.SourcePath = content.ResolveDocumentPath(level.Terrain.SourcePath, terrainPath)
	level.Terrain.ManifestPath = content.ResolveDocumentPath(level.Terrain.ManifestPath, terrainPath)
	if err := content.SaveLevel(f.runtime.LevelPath, level); err != nil {
		t.Fatal(err)
	}
	config := f.runtime.Config
	config.MaxChunkCommitsPerFrame = 2
	if err := RestartStreamedLevelRuntime(f.cmd, f.assets, config); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	updateStreamedObserverSelection(f.cmd, f.runtime)
	for _, coord := range []ChunkCoord{{}, {X: 1}} {
		s1fPrepared(t, f, coord, false)
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	if f.runtime.Metrics.PreparedChunkQueueDepth != 2 || f.runtime.Metrics.PendingPreparedBytes <= 0 {
		t.Fatal("fixture did not publish two owned full results before frontier capture")
	}
	s1eWork(t, f.runtime, 2, 32, 0, 0)
	hooks := 0
	f.runtime.Config.TerrainHooks = []PostSpawnTerrainHook{func(cmd *Commands, _ PostSpawnTerrainContext) {
		hooks++
		if err := StopStreamedLevelRuntime(cmd); err != nil {
			t.Fatal(err)
		}
	}}
	var caught any
	func() {
		defer func() { caught = recover() }()
		commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
		f.app.FlushCommands()
	}()
	if caught != nil {
		t.Fatalf("successful hook Stop left a consumable stale frontier entry: %v", caught)
	}
	if hooks != 1 || f.runtime.Initialized {
		t.Fatal("terrain hook did not successfully stop the runtime")
	}
	refreshStreamedRuntimeMetricsCounts(f.runtime)
	metrics := f.runtime.Metrics
	if metrics.PreparedQueueDepth != 0 || metrics.PreparedChunkQueueDepth != 0 || metrics.PreparedProxyQueueDepth != 0 || metrics.PendingPreparedBytes != 0 {
		t.Fatal("hook Stop left retained prepared depth or pending payload ownership")
	}
	s1eWork(t, f.runtime, 0, 32, 0, 0)
}
