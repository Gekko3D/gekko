package hl1

import (
	"os"
	"path/filepath"
	"testing"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestBuildImportSummaryUsesSyntheticBSPAndWAD(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "testmap.bsp")
	wadPath := filepath.Join(dir, "valve", "test.wad")
	mustWriteFile(t, wadPath, syntheticWAD([]string{"TESTWALL"}))
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
"wad" "valve/test.wad"
}
{
"classname" "info_player_start"
"origin" "128 64 32"
"angle" "90"
}
{
"classname" "monster_barney"
"origin" "0 0 0"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Models:   []Model{{Min: vec3(-16, -16, -16), Max: vec3(16, 16, 16)}},
	}))
	summary, err := BuildImportSummary(ImportOptions{
		GameDir:         dir,
		MapName:         "testmap",
		OutputRoot:      filepath.Join(dir, "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportSummary failed: %v", err)
	}
	if summary.Report.Source.BSPPath != bspPath {
		t.Fatalf("bsp path = %q, want %q", summary.Report.Source.BSPPath, bspPath)
	}
	if len(summary.Report.Source.WADPaths) != 1 || summary.Report.Source.WADPaths[0] != wadPath {
		t.Fatalf("wad paths = %+v", summary.Report.Source.WADPaths)
	}
	if summary.Report.MaterialCount != 1 {
		t.Fatalf("material count = %d", summary.Report.MaterialCount)
	}
	if len(summary.Report.MaterialKindCounts) != 1 || summary.Report.MaterialKindCounts[0].ClassName != "structural" {
		t.Fatalf("material kind counts = %+v", summary.Report.MaterialKindCounts)
	}
	if len(summary.Report.UnsupportedEntityCounts) != 1 || summary.Report.UnsupportedEntityCounts[0].ClassName != "monster_barney" {
		t.Fatalf("unsupported = %+v", summary.Report.UnsupportedEntityCounts)
	}
	if len(summary.Report.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", summary.Report.Diagnostics)
	}
}

func TestHL1MaterialSemanticsClassifiesCommonTextures(t *testing.T) {
	tests := []struct {
		name        string
		kind        string
		collision   string
		metallic    bool
		transparent bool
		emitsLight  bool
		tag         string
	}{
		{name: "METALWALL01", kind: "metal", collision: "solid", metallic: true, tag: "material:metal"},
		{name: "GLASS01", kind: "glass", collision: "solid", transparent: true, tag: "material:glass"},
		{name: "!WATERBLUE", kind: "water", collision: "liquid", tag: "material:liquid"},
		{name: "{LADDER1", kind: "ladder", collision: "ladder", transparent: true, tag: "material:ladder"},
		{name: "+0LIGHT1", kind: "emissive", collision: "solid", emitsLight: true, tag: "material:emissive"},
	}
	for _, tt := range tests {
		semantics := materialSemantics(tt.name)
		if semantics.Kind != tt.kind || semantics.CollisionKind != tt.collision {
			t.Fatalf("%s classified as %+v, want kind=%s collision=%s", tt.name, semantics, tt.kind, tt.collision)
		}
		if tt.metallic && semantics.Metallic <= 0 {
			t.Fatalf("%s expected metallic semantics, got %+v", tt.name, semantics)
		}
		if tt.transparent && (!semantics.Transparent || semantics.Transparency <= 0) {
			t.Fatalf("%s expected transparent semantics, got %+v", tt.name, semantics)
		}
		if tt.emitsLight && (!semantics.EmitsLight || semantics.Emissive <= 0) {
			t.Fatalf("%s expected emissive semantics, got %+v", tt.name, semantics)
		}
		if !hasTag(semantics.Tags, tt.tag) {
			t.Fatalf("%s missing tag %q in %+v", tt.name, tt.tag, semantics.Tags)
		}
	}
}

func TestBuildImportSummaryExcludesDoorBrushFacesFromStaticBakeSet(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "brushmap.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
}
{
"classname" "func_door"
"model" "*1"
}
{
"classname" "trigger_multiple"
"model" "*2"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 1, 0), Dist: 0}},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0), vec3(16, 0, 0), vec3(16, 0, 16), vec3(0, 0, 16),
			vec3(32, 0, 0), vec3(48, 0, 0), vec3(48, 0, 16), vec3(32, 0, 16),
			vec3(64, 0, 0), vec3(80, 0, 0), vec3(80, 0, 16), vec3(64, 0, 16),
		},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces: []FaceHeader{
			{PlaneID: 0, FirstEdge: 0, EdgeCount: 4, TexInfoID: 0},
			{PlaneID: 0, FirstEdge: 4, EdgeCount: 4, TexInfoID: 0},
			{PlaneID: 0, FirstEdge: 8, EdgeCount: 4, TexInfoID: 0},
		},
		Edges: []Edge{
			{A: 0, B: 1}, {A: 1, B: 2}, {A: 2, B: 3}, {A: 0, B: 3},
			{A: 4, B: 5}, {A: 5, B: 6}, {A: 6, B: 7}, {A: 4, B: 7},
			{A: 8, B: 9}, {A: 9, B: 10}, {A: 10, B: 11}, {A: 8, B: 11},
		},
		SurfEdges: []int32{0, 1, 2, -3, 4, 5, 6, -7, 8, 9, 10, -11},
		Models: []Model{
			{FirstFace: 0, FaceCount: 1},
			{FirstFace: 1, FaceCount: 1},
			{FirstFace: 2, FaceCount: 1},
		},
	}))
	summary, err := BuildImportSummary(ImportOptions{
		GameDir:         dir,
		MapName:         "brushmap",
		OutputRoot:      filepath.Join(dir, "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportSummary failed: %v", err)
	}
	if len(summary.WorldFaces) != 1 {
		t.Fatalf("world faces = %d", len(summary.WorldFaces))
	}
	if len(summary.BakeFaces) != 1 {
		t.Fatalf("bake faces = %d, want only world faces; moving door is emitted separately", len(summary.BakeFaces))
	}
	if len(summary.AllFaces) != 3 {
		t.Fatalf("all faces = %d", len(summary.AllFaces))
	}
}

func TestHL1IntegrationLoadsRealMapWhenConfigured(t *testing.T) {
	gameDir := os.Getenv("GEKKO_HL1_GAME_DIR")
	if gameDir == "" {
		t.Skip("GEKKO_HL1_GAME_DIR not set")
	}
	summary, err := BuildImportSummary(ImportOptions{
		GameDir:         gameDir,
		MapName:         "c1a0",
		OutputRoot:      filepath.Join(t.TempDir(), "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildImportSummary real map failed: %v", err)
	}
	if len(summary.Report.EntityCounts) == 0 {
		t.Fatalf("real map produced no entity counts")
	}
	if summary.Report.MaterialCount == 0 {
		t.Fatalf("real map produced no materials")
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := mkdirAll(filepath.Dir(path)); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := writeFile(path, data); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
