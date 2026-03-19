package content

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExpandPlacementVolumePreviewDeterministic(t *testing.T) {
	tmpDir := t.TempDir()
	assetPath := filepath.Join(tmpDir, "assets", "rock.gkasset")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	volume := NewPlacementVolumeDef(PlacementVolumeKindSphere)
	volume.ID = "volume-1"
	volume.AssetPath = filepath.Base(assetPath)
	volume.Rule = PlacementVolumeRuleDef{Mode: PlacementVolumeRuleModeCount, Count: 6}
	volume.Transform = LevelTransformDef{
		Position: Vec3{10, 20, 30},
		Rotation: Quat{0, 0, 0, 1},
		Scale:    Vec3{1, 1, 1},
	}

	left, err := ExpandPlacementVolumePreview(volume, PlacementVolumeExpandOptions{
		LevelDocumentPath: filepath.Join(tmpDir, "assets", "field.gklevel"),
		MaxInstances:      128,
	})
	if err != nil {
		t.Fatalf("ExpandPlacementVolumePreview failed: %v", err)
	}
	right, err := ExpandPlacementVolumePreview(volume, PlacementVolumeExpandOptions{
		LevelDocumentPath: filepath.Join(tmpDir, "assets", "field.gklevel"),
		MaxInstances:      128,
	})
	if err != nil {
		t.Fatalf("ExpandPlacementVolumePreview failed: %v", err)
	}

	if !reflect.DeepEqual(left, right) {
		t.Fatalf("expected deterministic expansion, left=%+v right=%+v", left, right)
	}
}

func TestExpandPlacementVolumePreviewClampsCount(t *testing.T) {
	tmpDir := t.TempDir()
	assetPath := filepath.Join(tmpDir, "assets", "rock.gkasset")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(assetPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	volume := NewPlacementVolumeDef(PlacementVolumeKindBox)
	volume.ID = "volume-clamp"
	volume.AssetPath = filepath.Base(assetPath)
	volume.Rule = PlacementVolumeRuleDef{Mode: PlacementVolumeRuleModeCount, Count: 300}
	result, err := ExpandPlacementVolumePreview(volume, PlacementVolumeExpandOptions{
		LevelDocumentPath: filepath.Join(tmpDir, "assets", "field.gklevel"),
		MaxInstances:      128,
	})
	if err != nil {
		t.Fatalf("ExpandPlacementVolumePreview failed: %v", err)
	}
	if !result.Clamped || len(result.Instances) != 128 || result.RequestedCount != 300 {
		t.Fatalf("unexpected clamp result: %+v", result)
	}
}

func TestExpandPlacementVolumePreviewUsesWeightedAssetSetDeterministically(t *testing.T) {
	tmpDir := t.TempDir()
	assetDir := filepath.Join(tmpDir, "assets")
	if err := os.MkdirAll(assetDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.gkasset", "b.gkasset"} {
		if err := os.WriteFile(filepath.Join(assetDir, name), []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	setPath := filepath.Join(assetDir, "rocks.gkset")
	if err := SaveAssetSet(setPath, &AssetSetDef{
		Name: "rocks",
		Entries: []AssetSetEntryDef{
			{AssetPath: "a.gkasset", Weight: 3},
			{AssetPath: "b.gkasset", Weight: 1},
		},
	}); err != nil {
		t.Fatalf("SaveAssetSet failed: %v", err)
	}

	volume := NewPlacementVolumeDef(PlacementVolumeKindSphere)
	volume.ID = "volume-set"
	volume.AssetPath = ""
	volume.AssetSetPath = "rocks.gkset"
	volume.Rule = PlacementVolumeRuleDef{Mode: PlacementVolumeRuleModeCount, Count: 8}
	levelPath := filepath.Join(assetDir, "field.gklevel")

	left, err := ExpandPlacementVolumePreview(volume, PlacementVolumeExpandOptions{LevelDocumentPath: levelPath, MaxInstances: 128})
	if err != nil {
		t.Fatalf("ExpandPlacementVolumePreview failed: %v", err)
	}
	right, err := ExpandPlacementVolumePreview(volume, PlacementVolumeExpandOptions{LevelDocumentPath: levelPath, MaxInstances: 128})
	if err != nil {
		t.Fatalf("ExpandPlacementVolumePreview failed: %v", err)
	}
	if !reflect.DeepEqual(left, right) {
		t.Fatalf("expected deterministic weighted selection, left=%+v right=%+v", left, right)
	}
}
