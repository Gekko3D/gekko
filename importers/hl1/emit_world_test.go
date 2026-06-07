package hl1

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestBuildAndSaveDebugSurfaceWorld(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "debugmap.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{"classname" "worldspawn"}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 1, 0), Dist: 0}},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(16, 0, 0),
			vec3(16, 0, 16),
			vec3(0, 0, 16),
		},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces:    []FaceHeader{{PlaneID: 0, FirstEdge: 0, EdgeCount: 4, TexInfoID: 0}},
		Edges: []Edge{
			{A: 0, B: 1},
			{A: 1, B: 2},
			{A: 2, B: 3},
			{A: 0, B: 3},
		},
		SurfEdges: []int32{0, 1, 2, -3},
		Models:    []Model{{FirstFace: 0, FaceCount: 1}},
	}))
	result, err := BuildDebugSurfaceWorld(ImportOptions{
		GameDir:         dir,
		MapName:         "debugmap",
		OutputRoot:      filepath.Join(dir, "out"),
		ChunkSize:       32,
		VoxelResolution: 0.1,
	})
	if err != nil {
		t.Fatalf("BuildDebugSurfaceWorld failed: %v", err)
	}
	if result.Voxelize.SurfaceCount != 25 {
		t.Fatalf("surface count = %d", result.Voxelize.SurfaceCount)
	}
	if err := SaveDebugSurfaceWorld(result); err != nil {
		t.Fatalf("SaveDebugSurfaceWorld failed: %v", err)
	}
	manifest, err := content.LoadImportedWorld(result.ManifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if validation := content.ValidateImportedWorld(manifest, content.ImportedWorldValidationOptions{DocumentPath: result.ManifestPath}); validation.HasErrors() {
		t.Fatalf("ValidateImportedWorld failed: %s", validation.Error())
	}
	if manifest.SourceBuildVersion != DebugSurfaceVoxelBuildVersion {
		t.Fatalf("source build version = %q", manifest.SourceBuildVersion)
	}
}

func TestBuildAndSaveDebugSolidWorld(t *testing.T) {
	dir := t.TempDir()
	bspPath := filepath.Join(dir, "valve", "maps", "solidmap.bsp")
	mustWriteFile(t, bspPath, syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
}
{
"classname" "info_player_start"
"origin" "-20 0 0"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(1, 0, 0), Dist: 0}},
		Nodes: []Node{{
			PlaneID:  0,
			Children: [2]int16{-2, -1},
		}},
		Leafs: []Leaf{
			{Contents: ContentsEmpty},
			{Contents: ContentsSolid},
		},
		Models: []Model{{
			Min:       vec3(-40, -40, -40),
			Max:       vec3(40, 40, 40),
			HeadNodes: [4]int32{0, -1, -1, -1},
		}},
	}))
	result, err := BuildDebugSolidWorld(ImportOptions{
		GameDir:             dir,
		MapName:             "solidmap",
		OutputRoot:          filepath.Join(dir, "out"),
		ChunkSize:           32,
		VoxelResolution:     1,
		MaxSolidSampleCells: 1000,
		SolidBandDepth:      2,
	})
	if err != nil {
		t.Fatalf("BuildDebugSolidWorld failed: %v", err)
	}
	if result.Mode != DebugWorldModeSolid {
		t.Fatalf("mode = %q", result.Mode)
	}
	if result.Voxelize.FilledCount != 128 || result.Voxelize.PlayableEmptyCount != 256 {
		t.Fatalf("voxelize = %+v", result.Voxelize)
	}
	if err := SaveDebugSurfaceWorld(result); err != nil {
		t.Fatalf("SaveDebugSurfaceWorld failed: %v", err)
	}
	manifest, err := content.LoadImportedWorld(result.ManifestPath)
	if err != nil {
		t.Fatalf("LoadImportedWorld failed: %v", err)
	}
	if validation := content.ValidateImportedWorld(manifest, content.ImportedWorldValidationOptions{DocumentPath: result.ManifestPath}); validation.HasErrors() {
		t.Fatalf("ValidateImportedWorld failed: %s", validation.Error())
	}
	if manifest.SourceBuildVersion != DebugSolidVoxelBuildVersion {
		t.Fatalf("source build version = %q", manifest.SourceBuildVersion)
	}
}
