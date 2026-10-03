package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestS3vOverrideSelectionLoadsOnlySelectedRuntimeSnapshots(t *testing.T) {
	const placements, unrelated = 12, 512
	f, _ := p5cRuntime(t, placements, false)
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = 0
	corrupt := filepath.Join(t.TempDir(), "corrupt.gkvoxobj")
	if err := os.WriteFile(corrupt, []byte("invalid snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < unrelated; i++ {
		path := corrupt
		if i%2 == 0 {
			path = filepath.Join(t.TempDir(), "missing.gkvoxobj")
		}
		id := fmt.Sprintf("unrelated-%d", i)
		f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(id, "body")] = content.VoxelObjectOverrideDef{PlacementID: id, ItemID: "body", SnapshotPath: path}
	}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	if visits := f.runtime.Metrics.VoxelOverrideSelectionKeyVisitsLastJob; visits != placements+unrelated {
		t.Fatalf("selection visited %d world keys, want one pass over %d", visits, placements+unrelated)
	}
	f.commitStage()
	for i := 0; i < placements; i++ {
		_, body := s1gPlacement(t, f, s1gID(0, i))
		s1gValue(t, f, body, [3]int{1, 2, 3}, 3)
	}
	if f.runtime.Metrics.VoxelOverrideSelectionKeyVisitsLastJob != placements+unrelated {
		t.Fatal("commit/metrics observation reset the last-job selection diagnostic")
	}
	// A builder invocation with no selected placements must skip world keys.
	job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{X: 100})
	prepared := prepareStreamedChunkLoad(job)
	defer prepared.release()
	if prepared.Err != nil || len(prepared.ObjectSnapshots) != 0 || f.runtime.Metrics.VoxelOverrideSelectionKeyVisitsLastJob != 0 {
		t.Fatalf("empty selection loaded world snapshots or retained visits: error=%v snapshots=%d visits=%d", prepared.Err, len(prepared.ObjectSnapshots), f.runtime.Metrics.VoxelOverrideSelectionKeyVisitsLastJob)
	}
}

func TestS3vOverrideSelectionPreservesFullPrefixAndOriginalValueAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selected.gkvoxobj")
	p5cSnapshot(t, path, 3, false)
	missing := filepath.Join(t.TempDir(), "missing.gkvoxobj")
	coord := ChunkCoord{}
	state := &StreamedLevelRuntimeState{
		PlacementsByChunk: map[ChunkCoord][]streamedPlacementInstance{coord: {
			{PlacementID: "root\x00nested"}, {PlacementID: "a"}, {PlacementID: "a\x00b"}, {PlacementID: ""}, {PlacementID: "a"},
		}},
		voxelOverrideMap: make(map[string]content.VoxelObjectOverrideDef),
	}
	selected := []string{"root\x00nested\x00body", "a\x00b", "a\x00b\x00body", "\x00body", "root\x00nested\x00\x00"}
	for _, key := range selected {
		// Selection follows the original map key, while preparation must load
		// the original value's path even when its IDs disagree with that key.
		state.voxelOverrideMap[key] = content.VoxelObjectOverrideDef{PlacementID: "not-selected", ItemID: "different-item", SnapshotPath: path}
	}
	for _, key := range []string{"a\x00", "root\x00nested\x00", "a", "outside\x00body", ""} {
		state.voxelOverrideMap[key] = content.VoxelObjectOverrideDef{PlacementID: "a", ItemID: "body", SnapshotPath: missing}
	}
	job := buildStreamedChunkLoadJob(state, coord)
	prepared := prepareStreamedChunkLoad(job)
	defer prepared.release()
	if prepared.Err != nil || len(prepared.ObjectSnapshots) != len(selected) {
		t.Fatalf("full prefix selection loaded the wrong snapshots: error=%v snapshots=%d want=%d", prepared.Err, len(prepared.ObjectSnapshots), len(selected))
	}
	for _, key := range selected {
		snapshot := prepared.ObjectSnapshots[key]
		if snapshot == nil || len(snapshot.Voxels) != 2 || snapshot.Voxels[0].Value != 3 {
			t.Fatalf("original selected key/value lost snapshot authority: %q", key)
		}
	}
	if visits := state.Metrics.VoxelOverrideSelectionKeyVisitsLastJob; visits != len(state.voxelOverrideMap) {
		t.Fatalf("duplicate/overlapping placement IDs multiplied world-key visits: got=%d want=%d", visits, len(state.voxelOverrideMap))
	}
	originalPlacements := state.PlacementsByChunk[coord]
	for _, selection := range []struct {
		id   string
		keys []string
	}{
		{"a\x00b", []string{"a\x00b\x00body"}},
		{"a", []string{"a\x00b", "a\x00b\x00body"}},
	} {
		state.PlacementsByChunk[coord] = []streamedPlacementInstance{{PlacementID: selection.id}}
		result := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(state, coord))
		if result.Err != nil || len(result.ObjectSnapshots) != len(selection.keys) {
			result.release()
			t.Fatalf("standalone prefix %q loaded wrong snapshots: error=%v count=%d", selection.id, result.Err, len(result.ObjectSnapshots))
		}
		for _, key := range selection.keys {
			if result.ObjectSnapshots[key] == nil {
				result.release()
				t.Fatalf("standalone prefix %q missed original key %q", selection.id, key)
			}
		}
		result.release()
	}
	state.PlacementsByChunk[coord] = originalPlacements
	corrupt := filepath.Join(t.TempDir(), "corrupt.gkvoxobj")
	if err := os.WriteFile(corrupt, []byte("invalid snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, badPath := range []string{missing, corrupt} {
		state.voxelOverrideMap[selected[0]] = content.VoxelObjectOverrideDef{PlacementID: "not-selected", SnapshotPath: badPath}
		failed := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(state, coord))
		failed.release()
		if failed.Err == nil {
			t.Fatal("selected invalid snapshot stopped being a preparation error")
		}
		if visits := state.Metrics.VoxelOverrideSelectionKeyVisitsLastJob; visits != len(state.voxelOverrideMap) {
			t.Fatalf("failed preparation changed builder selection visits: %d", visits)
		}
	}
}
