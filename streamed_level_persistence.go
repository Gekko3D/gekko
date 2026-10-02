package gekko

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gekko3d/gekko/content"
)

// Identity belongs only to outstanding main-thread captures. Workers carry it
// without inspecting the runtime ownership map.
type streamedImportedCaptureToken struct {
	// Edit is immutable provenance retained until terminal analysis commits.
	Edit runtimeVoxelEdit
}

func mergeStreamedImportedCaptureEdits(previous, latest runtimeVoxelEdit) runtimeVoxelEdit {
	if !previous.Valid || !latest.Valid {
		// Invalid capture bounds mean actual unknown impact, not an empty accumulator.
		return runtimeVoxelEdit{Added: previous.Added || latest.Added}
	}
	previous.include(latest)
	return previous
}

type streamedImportedCapture struct {
	Key   voxelWorldDirtyChunkKey
	Token *streamedImportedCaptureToken
}

func importedCaptureForItem(item streamedNavigationEditAnalysisItem) streamedImportedCapture {
	return streamedImportedCapture{Key: voxelWorldDirtyChunkKey{WorldID: item.WorldID, Coord: item.Coord}, Token: item.Capture}
}

func currentStreamedImportedCapture(state *StreamedLevelRuntimeState, capture streamedImportedCapture) bool {
	return capture.Token != nil && state.importedEditCaptures[capture.Key] == capture.Token
}

func finishStreamedImportedCapture(state *StreamedLevelRuntimeState, capture streamedImportedCapture) {
	if currentStreamedImportedCapture(state, capture) {
		delete(state.importedEditCaptures, capture.Key)
		if len(state.importedEditCaptures) == 0 {
			state.importedEditCaptures = nil
		}
	}
}

func finishStreamedImportedAnalysis(state *StreamedLevelRuntimeState, result streamedNavigationEditAnalysisResult) {
	for _, capture := range result.Captures {
		finishStreamedImportedCapture(state, capture)
	}
}

func invalidateStreamedImportedCapture(state *StreamedLevelRuntimeState, worldID string, coord content.TerrainChunkCoordDef) {
	delete(state.importedEditCaptures, voxelWorldDirtyChunkKey{WorldID: worldID, Coord: coord})
	if len(state.importedEditCaptures) == 0 {
		state.importedEditCaptures = nil
	}
}

func queueSavedStreamedImportedAnalysis(state *StreamedLevelRuntimeState, snapshot *content.ImportedWorldChunkDef, backing *VoxelBackingComponent) {
	if state.BaseNavManifest == nil {
		return
	}
	worldID := state.BaseNavManifest.SourceWorldID
	if worldID == "" {
		worldID = state.BaseWorldID
	}
	if snapshot.WorldID != worldID {
		return
	}
	copy := *snapshot
	copy.Voxels, copy.Tags = slices.Clone(snapshot.Voxels), slices.Clone(snapshot.Tags)
	// The saved full state is an uncategorized edit. Capture coalescing must
	// retain its unknown impact even when later captures have bounded edits.
	queueStreamedNavigationEditAnalysis(state, streamedNavigationEditAnalysisItem{
		WorldID: copy.WorldID, Coord: copy.Coord, ChunkSize: copy.ChunkSize,
		VoxelResolution: copy.VoxelResolution, Snapshot: &copy, Backing: copyNavigationVoxelBacking(backing),
	})
}

// writeStreamedLevelPayload publishes a unique, durable payload before its
// path can enter a manifest. It never changes an earlier published payload.
func writeStreamedLevelPayload(dir, name string, serialize func(string) error) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := sanitizePathSegment(strings.TrimSuffix(filepath.Base(name), ext))
	reservation, err := os.CreateTemp(dir, stem+"_*"+ext)
	if err != nil {
		return "", err
	}
	finalPath := reservation.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(finalPath)
		}
	}()
	if err := reservation.Close(); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, ".payload-*"+ext)
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := serialize(tempPath); err != nil {
		return "", err
	}
	completed, err := os.OpenFile(tempPath, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	if err := completed.Chmod(0644); err != nil {
		_ = completed.Close()
		return "", err
	}
	if err := completed.Sync(); err != nil {
		_ = completed.Close()
		return "", err
	}
	if err := completed.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", err
	}
	// After rename, failure may leave an unreferenced unique orphan. Its path
	// is never returned and no previously published payload is affected.
	renamed = true
	directory, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return "", err
	}
	if err := directory.Close(); err != nil {
		return "", err
	}
	return finalPath, nil
}
