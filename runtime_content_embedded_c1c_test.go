package gekko

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/derived"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestC1cEmbeddedPublicConversionAndScopedOwnership(t *testing.T) {
	chunk := &content.ImportedWorldChunkDef{WorldID: "embedded", SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion, ChunkSize: 16, VoxelResolution: .5, NonEmptyVoxelCount: 2, Voxels: []content.ImportedWorldVoxelDef{{X: 1, Y: 2, Z: 3, Value: 4, MaterialValue: 255}, {X: 9, Y: 10, Z: 11, Value: 3}}}
	hash, size, err := content.ImportedWorldChunkCompiledGeometryIdentity(chunk, nil)
	if err != nil {
		t.Fatal(err)
	}
	aux := derived.BuildImportedWorldChunkAux(chunk, nil, hash, size, false)
	aux.Records[0].Bytes[1087] = 231 // Includes a word for an unoccupied cell.
	path := filepath.Join(t.TempDir(), "chunk.gkchunk")
	if _, err := content.SaveImportedWorldChunkCompiledWithAux(path, chunk, aux, nil); err != nil {
		t.Fatal(err)
	}
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1 << 20})
	a, b := loader.NewScope(), loader.NewScope()
	t.Cleanup(a.Close)
	t.Cleanup(b.Close)
	shared, err := a.Loader().LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatal(err)
	}
	charge := loader.Stats().Bytes
	withoutAux := *shared
	withoutAux.EmbeddedAux = nil
	normalBytes := int64(0)
	for _, record := range shared.EmbeddedAux.Records {
		normalBytes += int64(len(record.Bytes))
	}
	graphCharge := runtimeContentGraphCharge(shared)
	if graphCharge < runtimeContentGraphCharge(&withoutAux)+normalBytes || charge != graphCharge {
		t.Fatal("decoded owner omitted embedded storage charge")
	}
	again, err := b.Loader().LoadImportedWorldChunk(path)
	stats := loader.Stats()
	if err != nil || again != shared || stats.Entries != 1 || stats.Bytes != charge || stats.PinnedBytes != charge || charge <= 0 {
		t.Fatalf("embedded graph duplicated/lost scoped ownership: %+v %v", stats, err)
	}
	first, second := ImportedWorldChunkToXBrickMap(shared), ImportedWorldChunkToXBrickMap(shared)
	if occupied, value := first.GetVoxel(1, 2, 3); !occupied || value != 255 {
		t.Fatal("conversion lost effective material")
	}
	want := append([]byte(nil), shared.EmbeddedAux.Records[0].Bytes...)
	conflict := *shared.EmbeddedAux
	conflict.Records = []content.ImportedWorldBrickAuxDef{{Origin: [3]int{}, Bytes: make([]byte, volume.VoxelAuxRecordBytes)}}
	prepared := prepareImportedWorldChunkGeometry(shared, &conflict)
	overridePrepared := prepareImportedWorldChunkGeometry(shared, nil)
	if !bytes.Equal(overridePrepared.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux, want) {
		t.Fatal("override preparation lost automatic embedded normals")
	}
	if !bytes.Equal(prepared.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux, want) {
		t.Fatal("explicit sidecar argument displaced embedded priority")
	}
	brick := first.Sectors[[3]int{}].GetBrick(0, 0, 0)
	other := second.Sectors[[3]int{}].GetBrick(0, 0, 0)
	if !bytes.Equal(brick.PrecomputedAux, want) || !bytes.Equal(other.PrecomputedAux, want) {
		t.Fatal("public conversion failed to apply embedded normals")
	}
	brick.PrecomputedAux[1087] ^= 1
	first.SetVoxel(1, 2, 3, 7)
	if !bytes.Equal(shared.EmbeddedAux.Records[0].Bytes, want) || !bytes.Equal(other.PrecomputedAux, want) {
		t.Fatal("live edit mutated decoded/other geometry normals")
	}
	loader.Clear()
	a.Close()
	if loader.Stats().PinnedBytes != charge {
		t.Fatal("remaining lease lost embedded graph")
	}
	b.Close()
	loader.Clear()
	if stats := loader.Stats(); stats.Bytes != 0 || stats.Entries != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("embedded graph leaked after final lease: %+v", stats)
	}
	if !bytes.Equal(shared.EmbeddedAux.Records[0].Bytes, want) {
		t.Fatal("lease release invalidated retained decoded bytes")
	}
}

func TestC1cEmbeddedFullProxyPriorityAndCompactRegistration(t *testing.T) {
	f, _, sidecarBytes := p5aRuntime(t, true, true)
	cfg := f.runtime.Config
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	level, err := content.LoadLevel(cfg.LevelPath)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := content.ResolveDocumentPath(level.BaseWorld.ManifestPath, cfg.LevelPath)
	world, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	entry := &world.Entries[0]
	path := content.ResolveImportedWorldChunkPath(*entry, manifestPath)
	chunk, err := content.LoadImportedWorldChunk(path)
	if err != nil {
		t.Fatal(err)
	}
	hash, size, err := content.ImportedWorldChunkCompiledGeometryIdentity(chunk, nil)
	if err != nil {
		t.Fatal(err)
	}
	aux := derived.BuildImportedWorldChunkAux(chunk, nil, hash, size, false)
	aux.Records[0].Bytes[1087] = 219
	result, err := content.SaveImportedWorldChunkCompiledWithAux(path, chunk, aux, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry.PayloadKind, entry.PayloadHash, entry.PayloadSizeBytes = result.PayloadKind, result.PayloadHash, result.PayloadSizeBytes
	lod := &world.Sectors[0].LODs[0]
	lod.PayloadHash, lod.PayloadSizeBytes = result.PayloadHash, result.PayloadSizeBytes
	// Leave old, different sidecar references in place: valid embedded data wins.
	if err := content.SaveImportedWorld(manifestPath, world); err != nil {
		t.Fatal(err)
	}
	cfg.CompactPreparedGeometry = true
	if err := StartStreamedLevelRuntime(f.cmd, f.assets, cfg); err != nil {
		t.Fatal(err)
	}
	updateStreamedObserverSelection(f.cmd, f.runtime)
	s1fPrepared(t, f, ChunkCoord{}, false)
	s1fPrepared(t, f, ChunkCoord{}, true)
	full := <-f.runtime.PreparedLoads
	proxy := <-f.runtime.PreparedProxyLoads
	if full.Err != nil || proxy.Err != nil || !full.ImportedWorldAuxHit || full.ImportedWorldAuxMiss || !proxy.AuxHit || proxy.AuxMiss || full.ImportedWorldAux == nil || proxy.Aux == nil {
		t.Fatal("embedded full/proxy selection failed", full.Err, proxy.Err)
	}
	want := full.ImportedWorldChunk.EmbeddedAux.Records[0].Bytes
	if bytes.Equal(want, sidecarBytes) || !bytes.Equal(full.ImportedWorldAux.Records[0].Bytes, want) || !bytes.Equal(proxy.Aux.Records[0].Bytes, want) {
		t.Fatal("sidecar displaced validated embedded bytes")
	}
	if !strings.Contains(full.PreparedImportedWorldGeometryCacheKey, full.ImportedWorldAux.PayloadHash) || !strings.Contains(proxy.PreparedGeometryCacheKey, proxy.Aux.PayloadHash) {
		t.Fatal("prepared identity omitted selected normal layer")
	}
	f.runtime.PreparedLoads <- full
	f.runtime.PreparedProxyLoads <- proxy
	f.commitStage()
	f.commitStage()
	_, fullAsset := p5aAsset(t, f, false)
	_, proxyAsset := p5aAsset(t, f, true)
	p5aGeometry(t, fullAsset.XBrickMap, want)
	p5aGeometry(t, proxyAsset.XBrickMap, want)
	fullAsset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux[volume.VoxelAuxRecordBytes-1] ^= 1
	if !bytes.Equal(proxyAsset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux, want) || !bytes.Equal(full.ImportedWorldChunk.EmbeddedAux.Records[0].Bytes, want) {
		t.Fatal("compact registration shared mutable normal storage")
	}
}
