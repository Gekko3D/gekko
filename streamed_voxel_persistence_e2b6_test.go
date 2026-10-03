package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"path/filepath"
	"testing"
)

func TestE2b6NearLimitPaintAndRemovalRemainReloadableSparseDeltas(t *testing.T) {
	for _, kind := range []string{"paint", "remove"} {
		t.Run(kind, func(t *testing.T) {
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
			value := uint8(7)
			expected := count
			if kind == "remove" {
				value = 0
				expected--
			}
			e2b4Edit(t, f, eid, volume.VoxelWrite{Value: value})
			deltaPath := f.runtime.WorldDeltaPath
			if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			payload := e2b4DurablePath(t, deltaPath)
			if payload.SchemaVersion != 2 || payload.Mode != content.VoxelObjectPayloadBaseDelta {
				t.Fatalf("in-limit %s emitted schema=%d mode=%q, want schema2 base_delta", kind, payload.SchemaVersion, payload.Mode)
			}
			e2b4Delta(t, payload, part, s1gID(0, 0), "body", []content.VoxelObjectVoxelDef{{Value: value}})
			base, lattice, _ := e2b1Canonical(t, part)
			resolved, err := content.ResolveVoxelObjectPayload(payload, base, lattice, s1gID(0, 0), "body", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(resolved.Voxels) != expected {
				t.Fatalf("resolved cells=%d, want %d", len(resolved.Voxels), expected)
			}
			current := XBrickMapFromVoxelObjectSnapshot(resolved)
			p1dVoxel(t, current, 0, value)
			for _, cell := range part.Source.VoxelShape.Voxels[1:] {
				present, v := current.GetVoxel(cell.X, cell.Y, cell.Z)
				if !present || v != 1 {
					t.Fatalf("reload lost unchanged cell %+v", cell)
				}
			}
			p5cAssetPresent(t, f, override, false)
		})
	}
}
