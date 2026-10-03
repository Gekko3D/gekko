package hl1

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/derived"
	"github.com/gekko3d/gekko/content/voxelcodec"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestC1dHL1EmbeddedDictionaryWorldAndGeneratedLevel(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "world.gkworld")
	c, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 62, Bytes: bytes.Repeat([]byte(`{"world_id":"hl1-c1d","schema_version":1,"chunk_size":32,"voxel_resolution":1,"source_payload_hash":""}`), 32)}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	e, err := importcommon.BuildImportedWorldEmission([]importcommon.Voxel{{X: 31, Y: 2, Z: 3, Palette: 1}, {X: 32, Y: 2, Z: 3, Palette: 1}}, []importcommon.Material{{ID: 1, PaletteIndex: 1}}, importcommon.ImportedWorldEmitOptions{WorldID: "hl1-c1d", ChunkSize: 32, VoxelResolution: 1})
	if err != nil {
		t.Fatal(err)
	}
	result := DebugWorldEmissionResult{ManifestPath: manifestPath, Emission: e, PayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true, ChunkCodec: c}
	if _, err := SaveDebugWorldWithStats(result); err != nil {
		t.Fatal(err)
	}
	entry := e.Manifest.Entries[0]
	path := content.ResolveImportedWorldChunkPath(entry, manifestPath)
	wire, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, info, err := c.Decode(wire); err != nil || info.DictionaryID != 62 {
		t.Fatal("fixture/emitter omitted dictionary profile", err)
	}
	if err := derived.EnsureImportedWorldAuxSidecarsForManifestWithCodec(manifestPath, c); err != nil {
		t.Fatal(err)
	}
	levelPath := filepath.Join(root, "demo.gklevel")
	level := content.NewLevelDef("demo")
	level.BaseWorld = &content.LevelBaseWorldDef{Kind: content.ImportedWorldKindVoxelWorld, ManifestPath: content.AuthorDocumentPath(manifestPath, levelPath), ReadOnlyByDefault: true}
	if err := SaveGeneratedLevel(GeneratedLevelResult{LevelPath: levelPath, Level: level, ChunkCodec: c}); err != nil {
		t.Fatal(err)
	}
	loaded, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range loaded.Entries {
		if entry.Aux != nil {
			t.Fatal("generated-level ensure created full sidecar")
		}
	}
	for _, sector := range loaded.Sectors {
		for _, lod := range sector.LODs {
			if lod.Aux != nil {
				t.Fatal("generated-level ensure created proxy sidecar")
			}
		}
	}
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".gkaux" {
			t.Fatal("embedded ensure emitted redundant sidecar", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(wire, after) {
		t.Fatal("generated-level ensure rewrote embedded frame")
	}
	if _, _, err := c.Encode(voxelcodec.Document{Kind: "still-owned"}); err != nil {
		t.Fatal("save/ensure closed caller codec", err)
	}
	// Geometry-only compiled content still receives sidecar backfill with the profile.
	chunk, err := content.LoadImportedWorldChunkWithCodec(path, c)
	if err != nil {
		t.Fatal(err)
	}
	save, err := content.SaveImportedWorldChunkCompiledWithAux(path, chunk, nil, c)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Entries[0].PayloadHash, loaded.Entries[0].PayloadSizeBytes = save.PayloadHash, save.PayloadSizeBytes
	if err := content.SaveImportedWorld(manifestPath, loaded); err != nil {
		t.Fatal(err)
	}
	if err := derived.EnsureImportedWorldAuxSidecarsForManifestWithCodec(manifestPath, c); err != nil {
		t.Fatal(err)
	}
	loaded, err = content.LoadImportedWorld(manifestPath)
	if err != nil || loaded.Entries[0].Aux == nil {
		t.Fatal("geometry-only dictionary chunk failed sidecar backfill", err)
	}
	aux, err := content.LoadImportedWorldChunkAux(content.ResolveDocumentPath(loaded.Entries[0].Aux.AuxPath, manifestPath))
	if err != nil || aux.SourcePayloadHash != save.PayloadHash || aux.SourcePayloadSizeBytes != save.PayloadSizeBytes {
		t.Fatal("backfill bound sidecar to wrong geometry", err)
	}
}

func TestC1dHL1RustProfileRejectsEmbedding(t *testing.T) {
	if _, err := ApplyHL1ExportProfile(ImportOptions{ExportProfile: HL1ExportProfileRustyVoxelRTInteropV1, ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true}); err == nil {
		t.Fatal("Rust JSON profile accepted embedded compiled normals")
	}
	opts, err := ApplyHL1ExportProfile(ImportOptions{ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true})
	if err != nil || !opts.EmbedNormals || opts.ChunkPayloadKind != content.ImportedWorldChunkPayloadBrickZstdBinaryV1 {
		t.Fatal("default profile discarded explicit compiled embedding", err)
	}
}

func TestC1dHL1IncompatibleEmbeddingRejectsBeforeBackingWrite(t *testing.T) {
	root := t.TempDir()
	backing := &content.VoxelBackingDef{SchemaVersion: content.CurrentVoxelBackingSchemaVersion, Kind: content.VoxelBackingKindPlaneTreeV1, BoundsMax: [3]int{1, 1, 1}, SolidValue: 1, PlaneTree: &content.VoxelBackingPlaneTreeDef{Root: -1, Leaves: []content.VoxelBackingPlaneLeafDef{{Solid: true}}}}
	if err := content.ValidateVoxelBacking(backing); err != nil {
		t.Fatal("invalid backing fixture", err)
	}
	_, err := SaveDebugWorldWithStats(DebugWorldEmissionResult{ManifestPath: filepath.Join(root, "invalid.gkworld"), BackingPath: filepath.Join(root, "backing.gkbacking"), Backing: backing, Emission: importcommon.ImportedWorldEmission{Manifest: &content.ImportedWorldDef{WorldID: "invalid"}}, PayloadKind: content.ImportedWorldChunkPayloadSparseJSONV1, EmbedNormals: true})
	if err == nil {
		t.Fatal("incompatible debug-world embedding accepted")
	}
	files, err := os.ReadDir(root)
	if err != nil || len(files) != 0 {
		t.Fatal("invalid embedding wrote backing/content before rejection", err)
	}
}

func TestC1dHL1PublicBuildersCarryEmbeddingAndCodec(t *testing.T) {
	root := t.TempDir()
	bspPath := filepath.Join(root, "valve", "maps", "compiled.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{Entities: `{"classname" "worldspawn"}`, Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}}, Planes: []Plane{{Normal: vec3(0, 1, 0)}}, Vertices: []importcommon.Vec3{vec3(0, 0, 0), vec3(16, 0, 0), vec3(16, 0, 16), vec3(0, 0, 16)}, TexInfos: []TexInfo{{MipTex: 0}}, Faces: []FaceHeader{{FirstEdge: 0, EdgeCount: 4}}, Edges: []Edge{{A: 0, B: 1}, {A: 1, B: 2}, {A: 2, B: 3}, {A: 0, B: 3}}, SurfEdges: []int32{0, 1, 2, -3}, Models: []Model{{FaceCount: 1}}}))
	codec, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 63, Bytes: bytes.Repeat([]byte(`{"world_id":"compiled","schema_version":1,"chunk_size":32,"source_payload_hash":""}`), 32)}})
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	opts := ImportOptions{GameDir: root, BSPPath: bspPath, MapName: "compiled", OutputRoot: filepath.Join(root, "out"), ChunkSize: 32, VoxelResolution: .1, ChunkPayloadKind: content.ImportedWorldChunkPayloadBrickZstdBinaryV1, EmbedNormals: true, ChunkCodec: codec}
	world, err := BuildDebugWorld(opts, DebugWorldModeSurface)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveDebugWorld(world); err != nil {
		t.Fatal(err)
	}
	summary, err := BuildImportSummary(opts)
	if err != nil {
		t.Fatal(err)
	}
	level, err := BuildGeneratedLevel(opts, summary, world.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveGeneratedLevel(level); err != nil {
		t.Fatal(err)
	}
	manifest, err := content.LoadImportedWorld(world.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Entries) == 0 {
		t.Fatal("public builder emitted no geometry")
	}
	for _, entry := range manifest.Entries {
		path := content.ResolveImportedWorldChunkPath(entry, world.ManifestPath)
		wire, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, info, err := codec.Decode(wire); err != nil || info.DictionaryID != 63 {
			t.Fatal("public builder/save lost selected compiled dictionary profile", err)
		}
		chunk, err := content.LoadImportedWorldChunkWithCodec(path, codec)
		if err != nil || chunk.EmbeddedAux == nil || entry.Aux != nil {
			t.Fatal("public builder/save lost embedding or generated redundant sidecar", err)
		}
	}
	if err := filepath.Walk(filepath.Dir(world.ManifestPath), func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filepath.Ext(path) == ".gkaux" {
			t.Fatal("public generated-level save created redundant sidecar")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "caller"}); err != nil {
		t.Fatal("public save closed borrowed profile", err)
	}
}
