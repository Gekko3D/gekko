package gekko

import (
	"bytes"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestPrepareImportedWorldChunkGeometryAppliesPrecomputedAux(t *testing.T) {
	chunk := &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{},
		ChunkSize:          32,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 1, Y: 1, Z: 1, Value: 2}},
		NonEmptyVoxelCount: 1,
	}
	auxBytes := make([]byte, volume.VoxelAuxRecordBytes)
	auxBytes[len(auxBytes)-1] = 42
	aux := &content.ImportedWorldChunkAuxDef{
		WorldID:         chunk.WorldID,
		Coord:           chunk.Coord,
		ChunkSize:       chunk.ChunkSize,
		VoxelResolution: chunk.VoxelResolution,
		Records: []content.ImportedWorldBrickAuxDef{{
			Origin: [3]int{0, 0, 0},
			Bytes:  auxBytes,
		}},
	}

	xbm := prepareImportedWorldChunkGeometry(chunk, aux)
	brick := xbm.Sectors[[3]int{0, 0, 0}].GetBrick(0, 0, 0)
	if brick == nil {
		t.Fatal("expected prepared brick")
	}
	if !bytes.Equal(brick.PrecomputedAux, auxBytes) {
		t.Fatalf("expected prepared brick to carry aux bytes")
	}
}
