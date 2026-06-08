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
}
{
"classname" "weapon_9mmhandgun"
"origin" "16 0 0"
}
{
"classname" "item_healthkit"
"origin" "32 0 0"
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
	if len(summary.Report.PickupEntityCounts) != 2 || summary.Report.PickupEntityCounts[0].ClassName != "item_healthkit" || summary.Report.PickupEntityCounts[1].ClassName != "weapon_9mmhandgun" {
		t.Fatalf("pickup counts = %+v", summary.Report.PickupEntityCounts)
	}
	if len(summary.Report.UnclassifiedMaterials) != 0 {
		t.Fatalf("unclassified materials = %+v", summary.Report.UnclassifiedMaterials)
	}
	if len(summary.Report.DiagnosticSeverityCounts) != 1 || summary.Report.DiagnosticSeverityCounts[0].Name != "info" || summary.Report.DiagnosticSeverityCounts[0].Count != 1 {
		t.Fatalf("diagnostic counts = %+v %+v", summary.Report.DiagnosticSeverityCounts, summary.Report.DiagnosticCodeCounts)
	}
	if len(summary.Report.DiagnosticCodeCounts) != 1 || summary.Report.DiagnosticCodeCounts[0].Name != "hl1.entity_unsupported" || summary.Report.DiagnosticCodeCounts[0].Count != 1 {
		t.Fatalf("diagnostic code counts = %+v", summary.Report.DiagnosticCodeCounts)
	}
	if len(summary.Report.Diagnostics) != 1 || summary.Report.Diagnostics[0].Code != "hl1.entity_unsupported" || summary.Report.Diagnostics[0].Subject != "monster_barney" {
		t.Fatalf("diagnostics = %+v", summary.Report.Diagnostics)
	}
}

func TestAppendHL1TargetDiagnosticsReportsTriggerGraphHealth(t *testing.T) {
	report := importcommon.ImportReport{}
	appendHL1TargetDiagnostics(&report, []importcommon.Entity{
		{
			ClassName: "func_door",
			KeyValues: map[string]string{
				"targetname": "door_a",
			},
		},
		{
			ClassName: "trigger_once",
			KeyValues: map[string]string{
				"target": "manager_a",
			},
		},
		{
			ClassName: "multi_manager",
			KeyValues: map[string]string{
				"targetname": "manager_a",
				"door_a":     "0.25",
				"door_b":     "nope",
				"door_c":     "0.5",
			},
		},
	})

	if len(report.UnresolvedTargetCounts) != 1 || report.UnresolvedTargetCounts[0] != (importcommon.NamedCount{Name: "door_c", Count: 1}) {
		t.Fatalf("unresolved target counts = %+v", report.UnresolvedTargetCounts)
	}
	if len(report.SkippedMultiTargetCounts) != 1 || report.SkippedMultiTargetCounts[0] != (importcommon.NamedCount{Name: "door_b", Count: 1}) {
		t.Fatalf("skipped multi-target counts = %+v", report.SkippedMultiTargetCounts)
	}
	if len(report.Diagnostics) != 2 {
		t.Fatalf("diagnostics = %+v", report.Diagnostics)
	}
	if report.Diagnostics[0].Code != "hl1.multi_manager_output_skipped" || report.Diagnostics[1].Code != "hl1.target_unresolved" {
		t.Fatalf("diagnostic codes = %+v", report.Diagnostics)
	}
}

func TestAppendHL1ReviewDiagnosticsReportsImportCoverageProblems(t *testing.T) {
	report := importcommon.ImportReport{}
	appendHL1ReviewDiagnostics(&report, []importcommon.Entity{
		{
			ClassName:    "func_train",
			BrushModelID: 1,
			KeyValues: map[string]string{
				"targetname": "train_a",
				"target":     "missing_corner",
			},
		},
		{
			ClassName: "path_corner",
			KeyValues: map[string]string{
				"targetname": "corner_a",
				"target":     "missing_next",
			},
		},
		{ClassName: "path_corner"},
		{
			ClassName: "func_door",
			KeyValues: map[string]string{
				"targetname": "door_without_model",
			},
		},
		{ClassName: "monster_scientist"},
	})

	assertHasDiagnosticCode(t, report.Diagnostics, "hl1.train_path_target_unresolved")
	assertHasDiagnosticCode(t, report.Diagnostics, "hl1.path_corner_next_unresolved")
	assertHasDiagnosticCode(t, report.Diagnostics, "hl1.path_corner_targetname_missing")
	assertHasDiagnosticCode(t, report.Diagnostics, "hl1.moving_brush_model_missing")
	assertHasDiagnosticCode(t, report.Diagnostics, "hl1.entity_unsupported")
}

func TestHL1ReviewEntityCountsClassifyInteractiveImports(t *testing.T) {
	entities := []importcommon.Entity{
		{ClassName: "func_train"},
		{ClassName: "func_door_rotating"},
		{ClassName: "path_corner"},
		{ClassName: "func_ladder"},
		{ClassName: "func_healthcharger"},
		{ClassName: "func_recharge"},
		{ClassName: "weapon_shotgun"},
	}
	if got := importcommon.EntityCounts(hl1MovingBrushEntityClassNames(entities)); len(got) != 2 || got[0].ClassName != "func_door_rotating" || got[1].ClassName != "func_train" {
		t.Fatalf("moving brush counts = %+v", got)
	}
	if got := importcommon.EntityCounts(hl1PathNodeEntityClassNames(entities)); len(got) != 1 || got[0].ClassName != "path_corner" {
		t.Fatalf("path node counts = %+v", got)
	}
	if got := importcommon.EntityCounts(hl1LadderEntityClassNames(entities)); len(got) != 1 || got[0].ClassName != "func_ladder" {
		t.Fatalf("ladder counts = %+v", got)
	}
	if got := importcommon.EntityCounts(hl1ChargerEntityClassNames(entities)); len(got) != 2 || got[0].ClassName != "func_healthcharger" || got[1].ClassName != "func_recharge" {
		t.Fatalf("charger counts = %+v", got)
	}
	if !supportedClass("func_ladder") {
		t.Fatalf("func_ladder should be supported")
	}
	if !supportedClass("func_conveyor") {
		t.Fatalf("func_conveyor should be supported")
	}
}

func assertHasDiagnosticCode(t *testing.T, diagnostics []importcommon.Diagnostic, code string) {
	t.Helper()
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("missing diagnostic code %q in %+v", code, diagnostics)
}

func TestHL1TriggerEntityClassNamesCountsTargetGraphClasses(t *testing.T) {
	got := importcommon.EntityCounts(hl1TriggerEntityClassNames([]importcommon.Entity{
		{ClassName: "trigger_once"},
		{ClassName: "trigger_multiple"},
		{ClassName: "trigger_hurt"},
		{ClassName: "trigger_changelevel"},
		{ClassName: "multi_manager"},
		{ClassName: "trigger_relay"},
		{ClassName: "func_door"},
	}))
	if len(got) != 6 || got[0].ClassName != "multi_manager" || got[1].ClassName != "trigger_changelevel" || got[2].ClassName != "trigger_hurt" || got[3].ClassName != "trigger_multiple" || got[4].ClassName != "trigger_once" || got[5].ClassName != "trigger_relay" {
		t.Fatalf("trigger entity counts = %+v", got)
	}
}

func TestHL1BreakableEntityClassNamesCountsFuncBreakable(t *testing.T) {
	got := importcommon.EntityCounts(hl1BreakableEntityClassNames([]importcommon.Entity{
		{ClassName: "func_breakable"},
		{ClassName: "FUNC_BREAKABLE"},
		{ClassName: "func_door"},
	}))
	if len(got) != 1 || got[0].ClassName != "func_breakable" || got[0].Count != 2 {
		t.Fatalf("breakable entity counts = %+v", got)
	}
}

func TestUnclassifiedMaterialTexturesReportsStructuralFallbacks(t *testing.T) {
	got := unclassifiedMaterialTextures([]importcommon.Material{
		{Kind: "structural", SourceTextureName: "RANDOM01", Tags: []string{"source:hl1", "material:structural"}},
		{Kind: "structural", SourceTextureName: "WALL01", Tags: []string{"source:hl1", "material:architectural"}},
		{Kind: "metal", SourceTextureName: "METAL01", Tags: []string{"source:hl1", "material:metal"}},
	})
	if len(got) != 1 || got[0] != "RANDOM01" {
		t.Fatalf("unclassified material textures = %+v", got)
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
		{name: "{LADDER1", kind: "ladder", collision: "ladder", tag: "material:ladder"},
		{name: "{FENCE1", kind: "grate", collision: "solid", metallic: true, tag: "material:cutout"},
		{name: "{BLUE", kind: "cutout", collision: "solid", tag: "material:cutout"},
		{name: "GRATE01", kind: "grate", collision: "solid", metallic: true, tag: "material:cutout"},
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
