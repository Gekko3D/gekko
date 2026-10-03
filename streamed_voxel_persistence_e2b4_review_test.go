package gekko

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestE2b4MergedGeometryLimitFallsBackToReloadableFullSnapshot(t *testing.T) {
	f, part, _ := e2b2Runtime(t)
	count := voxelcodec.DefaultLimits().MaxBricks
	part.Source.VoxelShape.Voxels = make([]content.VoxelObjectVoxelDef, count)
	for i := range part.Source.VoxelShape.Voxels {
		part.Source.VoxelShape.Voxels[i] = content.VoxelObjectVoxelDef{X: (i % 128) * 8, Z: (i / 128) * 8, Value: 1}
	}
	path := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
	asset, err := content.LoadAsset(path)
	if err != nil {
		t.Fatal(err)
	}
	asset.Parts[0] = part
	if err := content.SaveAsset(path, asset); err != nil {
		t.Fatal(err)
	}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := e2b2Body(t, f, s1gID(0, 0))
	override := e2b3Enable(t, f, eid)
	e2b3Qualified(t, f, eid, s1gID(0, 0), part)
	e2b4Edit(t, f, eid, volume.VoxelWrite{X: -9, Value: 7})
	deltaPath := f.runtime.WorldDeltaPath
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	payload := e2b4DurablePath(t, deltaPath)
	if payload.SchemaVersion != 1 || payload.Mode != content.VoxelObjectPayloadFull {
		t.Fatalf("delta whose merged geometry exceeds default brick limit must use legacy full fallback, got schema=%d mode=%q", payload.SchemaVersion, payload.Mode)
	}
	resolved, err := content.ResolveVoxelObjectPayload(payload, nil, content.VoxelObjectLatticeDef{}, s1gID(0, 0), "body", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Voxels) != count+1 {
		t.Fatalf("reloaded %d cells, want %d", len(resolved.Voxels), count+1)
	}
	current := XBrickMapFromVoxelObjectSnapshot(resolved)
	p1dVoxel(t, current, -9, 7)
	for _, cell := range part.Source.VoxelShape.Voxels {
		present, value := current.GetVoxel(cell.X, cell.Y, cell.Z)
		if !present || value != cell.Value {
			t.Fatalf("reload lost base cell %+v", cell)
		}
	}
	p5cAssetPresent(t, f, override, false)
}
