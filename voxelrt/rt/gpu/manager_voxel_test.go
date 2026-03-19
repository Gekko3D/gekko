package gpu

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestBuildObjectParamsBytesIncludesShadowAndTerrainMetadata(t *testing.T) {
	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	obj.XBrickMap.Sectors[[3]int{0, 0, 0}] = volume.NewSector(0, 0, 0)
	obj.LODThreshold = 72.5
	obj.ShadowGroupID = 42
	obj.ShadowSeamWorldEpsilon = 0.75
	obj.IsTerrainChunk = true
	obj.TerrainGroupID = 77
	obj.TerrainChunkCoord = [3]int{-2, 0, 3}
	obj.TerrainChunkSize = 32

	alloc := &ObjectGpuAllocation{
		MaterialOffset: 7,
	}

	buf := buildObjectParamsBytes(obj, alloc)
	if len(buf) != objectParamsSizeBytes {
		t.Fatalf("expected %d bytes, got %d", objectParamsSizeBytes, len(buf))
	}
	if got := binary.LittleEndian.Uint32(buf[0:4]); got != obj.XBrickMap.ID {
		t.Fatalf("expected map ID %d, got %d", obj.XBrickMap.ID, got)
	}
	if got := binary.LittleEndian.Uint32(buf[12:16]); got != alloc.MaterialOffset*4 {
		t.Fatalf("expected material base %d, got %d", alloc.MaterialOffset*4, got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(buf[20:24])); got != obj.LODThreshold {
		t.Fatalf("expected LOD threshold %v, got %v", obj.LODThreshold, got)
	}
	if got := binary.LittleEndian.Uint32(buf[24:28]); got != 1 {
		t.Fatalf("expected sector count 1, got %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[32:36]); got != obj.ShadowGroupID {
		t.Fatalf("expected shadow group %d, got %d", obj.ShadowGroupID, got)
	}
	if got := math.Float32frombits(binary.LittleEndian.Uint32(buf[36:40])); got != obj.ShadowSeamWorldEpsilon {
		t.Fatalf("expected seam epsilon %v, got %v", obj.ShadowSeamWorldEpsilon, got)
	}
	if got := binary.LittleEndian.Uint32(buf[40:44]); got != 1 {
		t.Fatalf("expected terrain flag 1, got %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[44:48]); got != obj.TerrainGroupID {
		t.Fatalf("expected terrain group %d, got %d", obj.TerrainGroupID, got)
	}
	if got := int32(binary.LittleEndian.Uint32(buf[48:52])); got != int32(obj.TerrainChunkCoord[0]) {
		t.Fatalf("expected terrain chunk x %d, got %d", obj.TerrainChunkCoord[0], got)
	}
	if got := int32(binary.LittleEndian.Uint32(buf[52:56])); got != int32(obj.TerrainChunkCoord[1]) {
		t.Fatalf("expected terrain chunk y %d, got %d", obj.TerrainChunkCoord[1], got)
	}
	if got := int32(binary.LittleEndian.Uint32(buf[56:60])); got != int32(obj.TerrainChunkCoord[2]) {
		t.Fatalf("expected terrain chunk z %d, got %d", obj.TerrainChunkCoord[2], got)
	}
	if got := binary.LittleEndian.Uint32(buf[60:64]); got != uint32(obj.TerrainChunkSize) {
		t.Fatalf("expected terrain chunk size %d, got %d", obj.TerrainChunkSize, got)
	}
}

func TestBuildTerrainChunkLookupIncludesVisibleTerrainChunksOnly(t *testing.T) {
	scene := &core.Scene{
		VisibleObjects: []*core.VoxelObject{
			{
				IsTerrainChunk:    true,
				TerrainGroupID:    1001,
				TerrainChunkCoord: [3]int{-1, 0, 0},
				TerrainChunkSize:  32,
			},
			{
				IsTerrainChunk:    true,
				TerrainGroupID:    1001,
				TerrainChunkCoord: [3]int{0, 0, 0},
				TerrainChunkSize:  32,
			},
			{
				IsTerrainChunk: false,
			},
		},
	}

	entries, params := buildTerrainChunkLookup(scene)
	if params.GridSize == 0 {
		t.Fatal("expected non-empty terrain lookup")
	}
	if got := findTerrainChunkLookupObjectID(entries, params, 1001, [3]int32{-1, 0, 0}); got != 0 {
		t.Fatalf("expected lookup for left terrain chunk to resolve object 0, got %d", got)
	}
	if got := findTerrainChunkLookupObjectID(entries, params, 1001, [3]int32{0, 0, 0}); got != 1 {
		t.Fatalf("expected lookup for origin terrain chunk to resolve object 1, got %d", got)
	}
	if got := findTerrainChunkLookupObjectID(entries, params, 9999, [3]int32{0, 0, 0}); got != -1 {
		t.Fatalf("expected missing terrain group lookup to fail, got %d", got)
	}
	if got := findTerrainChunkLookupObjectID(entries, params, 1001, [3]int32{2, 0, 0}); got != -1 {
		t.Fatalf("expected missing terrain chunk lookup to fail, got %d", got)
	}
}
