package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestI07TerrainPagesPreserveBackingQueriesAndRuntimeGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "terrain.gkterrainmanifest")
	d := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "paged-ground", SourceHash: strings.Repeat("a", 64), ChunkSize: 128, VoxelResolution: 2}
	tiles := map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
	for x := 0; x < 2; x++ {
		coord := content.TerrainChunkCoordDef{X: x}
		tile := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: d.TerrainID, SourceHash: d.SourceHash, Coord: coord, WorldOrigin: [3]float32{float32(x * 256), 0, 0}, SampleWidth: 128, SampleHeight: 128, SampleSpacing: 2, HeightOffset: 20, HeightScale: 100, HeightSamples: make([]uint16, 128*128)}
		for i := range tile.HeightSamples {
			tile.HeightSamples[i] = 10000
		}
		relative := "source" + string(rune('0'+x)) + ".gkchunk"
		saved, e := content.SaveTerrainHeightTile(filepath.Join(dir, relative), tile)
		if e != nil {
			t.Fatal(e)
		}
		d.Entries = append(d.Entries, content.TerrainChunkEntryDef{Coord: coord, WorldOrigin: tile.WorldOrigin, TerrainID: d.TerrainID, SourceHash: d.SourceHash, ChunkSize: 128, VoxelResolution: 2, ChunkPath: relative, PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: saved.PayloadHash, PayloadSizeBytes: saved.PayloadSizeBytes, HeightOffset: 20, HeightScale: 100})
		tiles[coord] = tile
	}
	for z := 0; z < 2; z++ {
		for x := 0; x < 4; x++ {
			origin := [3]float32{float32(x * 128), 0, float32(z * 128)}
			d.Pages = append(d.Pages, content.StreamPageDef{Level: content.StreamPageLevelRegional, BoundsMin: [3]float32{origin[0], 20, origin[2]}, BoundsMax: [3]float32{origin[0] + 128, 120, origin[2] + 128}, Payload: content.StreamPagePayloadDef{Kind: content.TerrainHeightTilePayloadKind, Path: "regional" + string(rune('0'+len(d.Pages))) + ".gkchunk", WorldOrigin: origin, ChunkSize: 64, SampleSpacing: 2, VoxelResolution: 1, HeightOffset: 20, HeightScale: 100, PayloadHash: strings.Repeat("b", 64), PayloadSizeBytes: 8192}})
		}
	}
	for _, spec := range []struct {
		level              uint8
		spacing, res, span float32
		children           []uint32
	}{{content.StreamPageLevelMacro, 4, 4, 512, []uint32{0, 1, 2, 3, 4, 5, 6, 7}}, {content.StreamPageLevelRoot, 16, 16, 2048, []uint32{8}}} {
		d.Pages = append(d.Pages, content.StreamPageDef{Level: spec.level, BoundsMin: [3]float32{0, 20, 0}, BoundsMax: [3]float32{spec.span, 120, spec.span}, ChildPageIndices: spec.children, Payload: content.StreamPagePayloadDef{Kind: content.TerrainHeightTilePayloadKind, Path: "coarse" + string(rune('0'+len(d.Pages))) + ".gkchunk", ChunkSize: 128, SampleSpacing: spec.spacing, VoxelResolution: spec.res, HeightOffset: 20, HeightScale: 100, PayloadHash: strings.Repeat("c", 64), PayloadSizeBytes: 32768}})
	}
	d.RootPageIndices = []uint32{9}
	if e := content.SaveTerrainChunkManifest(path, d); e != nil {
		t.Fatal(e)
	}
	loaded, e := content.LoadTerrainChunkManifest(path)
	if e != nil || loaded.PageIndex == nil || loaded.PageIndex.SourceOnly {
		t.Fatalf("tooling page manifest: %v", e)
	}
	field, e := NewTerrainHeightField(loaded, path, TerrainHeightFieldOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range loaded.Entries {
		if e = field.LoadTile(entry.Coord); e != nil {
			t.Fatal(e)
		}
	}
	for _, x := range []float32{127.75, 128, 128.25, 255.75, 256, 256.25} {
		s := field.SampleGroundXZ(x, 100)
		want := float32(20 + 10000.0/65535*100)
		if s.Status != TerrainHeightPresent || s.Generation != 2 || math.Abs(float64(s.Height-want)) > 1e-5 || s.Normal.Sub(mgl32.Vec3{0, 1, 0}).Len() > 1e-6 {
			t.Fatalf("backing seam across source/visual boundary x%g: %+v", x, s)
		}
	}
	snapshot := field.Snapshot()
	if !field.RemoveTile(content.TerrainChunkCoordDef{X: 1}) {
		t.Fatal("resident source removal failed")
	}
	if field.SampleGroundXZ(256, 100).Status != TerrainHeightNotResident || snapshot.SampleGroundXZ(256, 100).Status != TerrainHeightPresent {
		t.Fatal("visual pages changed source snapshot/removal semantics")
	}
	if got, e := NewRuntimeContentLoader().LoadTerrainChunkManifest(path); e == nil || got != nil {
		t.Fatal("live runtime admitted terrain pages before terrain surface/collision handoff")
	}
	legacyPath := filepath.Join(dir, "legacy.gkterrainmanifest")
	legacy := &content.TerrainChunkManifestDef{SchemaVersion: 2, TerrainID: "legacy", ChunkSize: 4, VoxelResolution: 1}
	if e = content.SaveTerrainChunkManifest(legacyPath, legacy); e != nil {
		t.Fatal(e)
	}
	if got, e := NewRuntimeContentLoader().LoadTerrainChunkManifest(legacyPath); e != nil || got.SchemaVersion != 2 {
		t.Fatalf("legacy runtime changed: %v", e)
	}
	// Source definitions remain independently owned by their backing tiles.
	if tiles[content.TerrainChunkCoordDef{X: 1}].HeightSamples[0] != 10000 {
		t.Fatal("manifest/query processing mutated authored source")
	}
}
