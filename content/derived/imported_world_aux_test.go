package derived

import (
	"encoding/binary"
	"math"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestEnsureImportedWorldAuxSidecarsForManifestBackfillsMissingRefs(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "worlds", "demo.gkworld")
	chunkPath := filepath.Join(root, "worlds", "chunks", "demo_0_0_0.gkchunk")
	chunk := &content.ImportedWorldChunkDef{
		WorldID:            "demo",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          32,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 1, Y: 1, Z: 1, Value: 1}},
		NonEmptyVoxelCount: 1,
	}
	if err := content.SaveImportedWorldChunk(chunkPath, chunk); err != nil {
		t.Fatalf("SaveImportedWorldChunk failed: %v", err)
	}
	manifest := &content.ImportedWorldDef{
		WorldID:         "demo",
		Kind:            content.ImportedWorldKindVoxelWorld,
		ChunkSize:       32,
		VoxelResolution: 1,
		Entries: []content.ImportedWorldChunkEntryDef{{
			Coord:              chunk.Coord,
			ChunkPath:          content.AuthorDocumentPath(chunkPath, manifestPath),
			NonEmptyVoxelCount: 1,
		}},
	}
	if err := content.SaveImportedWorld(manifestPath, manifest); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	if err := EnsureImportedWorldAuxSidecarsForManifest(manifestPath); err != nil {
		t.Fatalf("EnsureImportedWorldAuxSidecarsForManifest failed: %v", err)
	}
	loaded, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if len(loaded.Entries) != 1 || loaded.Entries[0].Aux == nil || loaded.Entries[0].Aux.AuxPath == "" {
		t.Fatalf("expected entry aux ref, got %+v", loaded.Entries)
	}
	aux, err := content.LoadImportedWorldChunkAux(content.ResolveDocumentPath(loaded.Entries[0].Aux.AuxPath, manifestPath))
	if err != nil {
		t.Fatalf("LoadImportedWorldChunkAux failed: %v", err)
	}
	if len(aux.Records) == 0 || aux.NormalBakeVersion != content.ImportedWorldNormalBakeVersion {
		t.Fatalf("expected baked aux records, got %+v", aux)
	}
}

func TestBuildImportedWorldChunkAuxBakesNeighborAwareNormals(t *testing.T) {
	left := &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		ChunkSize:          32,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 31, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	}
	right := &content.ImportedWorldChunkDef{
		WorldID:            "world-a",
		Coord:              content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		ChunkSize:          32,
		VoxelResolution:    1,
		Voxels:             []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}},
		NonEmptyVoxelCount: 1,
	}
	aux := BuildImportedWorldChunkAux(left, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{
		left.Coord:  left,
		right.Coord: right,
	}, "hash", 12, true)
	if aux == nil || len(aux.Records) == 0 {
		t.Fatalf("expected aux records, got %+v", aux)
	}

	var record *content.ImportedWorldBrickAuxDef
	for i := range aux.Records {
		if aux.Records[i].Origin == ([3]int{24, 0, 0}) {
			record = &aux.Records[i]
			break
		}
	}
	if record == nil {
		t.Fatalf("expected boundary brick aux, got %+v", aux.Records)
	}
	normalWord := binary.LittleEndian.Uint16(record.Bytes[volume.DenseOccupancyWordCount*4+volume.DenseOccupancyLinearIndexLocal(7, 0, 0)*2:])
	nx, ny, nz, valid := decodeAuxNormalForTest(normalWord)
	if !valid {
		t.Fatal("expected baked normal to be valid")
	}
	if math.Abs(nx+1) > 0.02 || math.Abs(ny) > 0.02 || math.Abs(nz) > 0.02 {
		t.Fatalf("expected seam-aware normal near -X, got (%.3f, %.3f, %.3f)", nx, ny, nz)
	}
}

func decodeAuxNormalForTest(word uint16) (float64, float64, float64, bool) {
	if word&volume.VoxelNormalValidBit == 0 {
		return 0, 0, 0, false
	}
	unpack := func(bits uint16) float64 {
		return float64(bits)/float64(volume.VoxelNormalOctMax)*2 - 1
	}
	x := unpack(word & 0x7f)
	y := unpack((word >> 7) & 0x7f)
	z := 1 - math.Abs(x) - math.Abs(y)
	if z < 0 {
		oldX, oldY := x, y
		x = (1 - math.Abs(oldY)) * signNotZeroForTest(oldX)
		y = (1 - math.Abs(oldX)) * signNotZeroForTest(oldY)
	}
	length := math.Sqrt(x*x + y*y + z*z)
	if length <= 1e-8 {
		return 0, 0, 0, false
	}
	return x / length, y / length, z / length, true
}

func signNotZeroForTest(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
