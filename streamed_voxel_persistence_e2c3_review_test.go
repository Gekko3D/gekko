package gekko

import (
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestE2c3PreflightChargesEveryOwnedReplacementSelector(t *testing.T) {
	f, part, _ := e2b2Runtime(t)
	part.Source.VoxelShape.Voxels = nil
	var clears []volume.VoxelWrite
	for brick := 0; brick < 32; brick++ {
		for z := 0; z < 8; z++ {
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					gx, gz := (brick%8)*8+x, (brick/8)*8+z
					part.Source.VoxelShape.Voxels = append(part.Source.VoxelShape.Voxels, content.VoxelObjectVoxelDef{X: gx, Y: y, Z: gz, Value: 1})
					clears = append(clears, volume.VoxelWrite{X: gx, Y: y, Z: gz})
				}
			}
		}
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
	f.runtime.Config.EnableHybridVoxelObjectDeltas = true
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	f.commitStage()
	eid := e2b2Body(t, f, s1gID(0, 0))
	e2b3Enable(t, f, eid)
	e2b4Edit(t, f, eid, clears[:512]...)
	intent := collectStreamedPersistenceIntent(f.cmd, f.runtime, ChunkCoord{}, f.runtime.LoadedChunks[ChunkCoord{}])
	if intent == nil || len(intent.Entities) != 1 {
		t.Fatal("fixture must retain one identical object intent")
	}
	first, err := preflightStreamedPersistence(f.cmd, f.runtime, intent)
	if err != nil {
		t.Fatal(err)
	}
	checkCapture := func(selectors int) {
		t.Helper()
		captured := captureStreamedPersistence(f.cmd, f.runtime, intent)
		if len(captured) != 1 || captured[0].Input.ObjectPayload == nil {
			t.Fatal("preflight fixture did not capture managed payload")
		}
		payload := captured[0].Input.ObjectPayload
		if payload.SchemaVersion != 3 || payload.Mode != content.VoxelObjectPayloadHybridDelta || len(payload.Voxels) != 0 || len(payload.ReplacementBricks) != selectors {
			t.Fatalf("capture schema=%d mode=%q records=%d selectors=%d", payload.SchemaVersion, payload.Mode, len(payload.Voxels), len(payload.ReplacementBricks))
		}
	}
	checkCapture(1)
	e2b4Edit(t, f, eid, clears[512:]...)
	last, err := preflightStreamedPersistence(f.cmd, f.runtime, intent)
	if err != nil {
		t.Fatal(err)
	}
	checkCapture(32)
	want := int64(31 * unsafe.Sizeof([3]int32{}))
	if last-first != want {
		t.Fatalf("additional selector backing charge=%d, want %d", last-first, want)
	}
	current, _, ok := currentVoxelMapForEntity(f.cmd, eid)
	if !ok || len(VoxelObjectSnapshotFromXBrickMap(current).Voxels) != 0 {
		t.Fatal("full-clear selector fixture lost empty authoritative geometry")
	}
}
