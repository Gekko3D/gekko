package content

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAssetDetectsDuplicateIDsIncludingRoot(t *testing.T) {
	def := NewAssetDef("duplicates")
	def.Parts = []AssetPartDef{
		{ID: def.ID, Name: "part-a", Source: proceduralCubeSource(), Transform: identityTransform()},
		{ID: def.ID, Name: "part-b", Source: proceduralCubeSource(), Transform: identityTransform()},
	}

	result := ValidateAsset(def, AssetValidationOptions{})
	if !result.HasErrors() {
		t.Fatal("expected validation errors")
	}
	assertHasValidationCode(t, result, "duplicate_id")
}

func TestValidateAssetDetectsBrokenParentAndCycleAndUnsupportedParentTarget(t *testing.T) {
	def := NewAssetDef("hierarchy")
	def.Parts = []AssetPartDef{
		{ID: "a", Name: "a", ParentID: "b", Source: proceduralCubeSource(), Transform: identityTransform()},
		{ID: "b", Name: "b", ParentID: "a", Source: proceduralCubeSource(), Transform: identityTransform()},
		{ID: "orphan", Name: "orphan", ParentID: "missing", Source: proceduralCubeSource(), Transform: identityTransform()},
		{ID: "bad-target", Name: "bad-target", ParentID: "light-parent", Source: proceduralCubeSource(), Transform: identityTransform()},
	}
	def.Lights = []AssetLightDef{
		{
			ID:        "light",
			Name:      "light",
			ParentID:  "missing",
			Transform: identityTransform(),
			Type:      AssetLightTypePoint,
		},
		{
			ID:        "light-parent",
			Name:      "light-parent",
			Transform: identityTransform(),
			Type:      AssetLightTypePoint,
		},
		{
			ID:        "light-child",
			Name:      "light-child",
			ParentID:  "light-parent",
			Transform: identityTransform(),
			Type:      AssetLightTypePoint,
		},
	}

	result := ValidateAsset(def, AssetValidationOptions{})
	assertHasValidationCode(t, result, "hierarchy_cycle")
	assertHasValidationCode(t, result, "broken_parent_reference")
	assertHasValidationCode(t, result, "unsupported_parent_target")
}

func TestValidateAssetDetectsEmptyNamesAndMarkerKind(t *testing.T) {
	def := NewAssetDef("")
	def.Parts = []AssetPartDef{{ID: "part", Source: proceduralCubeSource(), Transform: identityTransform()}}
	def.Markers = []AssetMarkerDef{{ID: "marker", Name: "", Kind: "", Transform: identityTransform()}}

	result := ValidateAsset(def, AssetValidationOptions{})
	assertHasValidationCode(t, result, "empty_name")
	assertHasValidationCode(t, result, "empty_marker_kind")
}

func TestValidateAssetValidatesVoxAndSceneNodePayloads(t *testing.T) {
	def := NewAssetDef("sources")
	def.Parts = []AssetPartDef{
		{
			ID:   "vox-model",
			Name: "vox-model",
			Source: AssetSourceDef{
				Kind:       AssetSourceKindVoxModel,
				ModelIndex: -1,
			},
			Transform: identityTransform(),
		},
		{
			ID:   "scene-node",
			Name: "scene-node",
			Source: AssetSourceDef{
				Kind:     AssetSourceKindVoxSceneNode,
				Path:     "",
				NodeName: "",
			},
			Transform: identityTransform(),
		},
	}

	result := ValidateAsset(def, AssetValidationOptions{})
	assertHasValidationCode(t, result, "invalid_source_payload")
}

func TestValidateAssetAcceptsGroupPartSource(t *testing.T) {
	def := NewAssetDef("group")
	def.Parts = []AssetPartDef{{
		ID:   "group",
		Name: "group",
		Source: AssetSourceDef{
			Kind: AssetSourceKindGroup,
		},
		Transform: identityTransform(),
	}}

	result := ValidateAsset(def, AssetValidationOptions{})
	if result.HasErrors() {
		t.Fatalf("expected group source to validate, got %+v", result.Issues)
	}
}

func TestValidateAssetValidatesProceduralPrimitivePayload(t *testing.T) {
	def := NewAssetDef("procedural")
	def.Parts = []AssetPartDef{
		{
			ID:   "bad-primitive",
			Name: "bad-primitive",
			Source: AssetSourceDef{
				Kind:      AssetSourceKindProceduralPrimitive,
				Primitive: "capsule",
			},
			Transform: identityTransform(),
		},
		{
			ID:   "missing-param",
			Name: "missing-param",
			Source: AssetSourceDef{
				Kind:      AssetSourceKindProceduralPrimitive,
				Primitive: "cone",
				Params:    map[string]float32{"radius": 1},
			},
			Transform: identityTransform(),
		},
	}

	result := ValidateAsset(def, AssetValidationOptions{})
	assertHasValidationCode(t, result, "invalid_source_payload")
}

func TestValidateAssetDetectsMissingSourceFilesWithDocumentRelativeFallback(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(assetsDir, "crate.vox"), []byte("vox"), 0644); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	def := NewAssetDef("files")
	def.Parts = []AssetPartDef{
		{
			ID:   "relative",
			Name: "relative",
			Source: AssetSourceDef{
				Kind:       AssetSourceKindVoxModel,
				Path:       "crate.vox",
				ModelIndex: 0,
			},
			Transform: identityTransform(),
		},
		{
			ID:   "missing",
			Name: "missing",
			Source: AssetSourceDef{
				Kind:       AssetSourceKindVoxModel,
				Path:       "missing.vox",
				ModelIndex: 0,
			},
			Transform: identityTransform(),
		},
		{
			ID:   "raw-path",
			Name: "raw-path",
			Source: AssetSourceDef{
				Kind:       AssetSourceKindVoxModel,
				Path:       filepath.Join("assets", "crate.vox"),
				ModelIndex: 0,
			},
			Transform: identityTransform(),
		},
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd failed: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("Chdir failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	result := ValidateAsset(def, AssetValidationOptions{DocumentPath: filepath.Join(assetsDir, "asset.gkasset")})
	if result.HardErrorCount != 1 {
		t.Fatalf("expected exactly one hard error, got %+v", result.Issues)
	}
	assertHasValidationCode(t, result, "missing_source_file")
}

func assertHasValidationCode(t *testing.T, result AssetValidationResult, code string) {
	t.Helper()
	for _, issue := range result.Issues {
		if issue.Code == code {
			return
		}
	}
	t.Fatalf("expected validation code %q in %+v", code, result.Issues)
}

func proceduralCubeSource() AssetSourceDef {
	return AssetSourceDef{
		Kind:      AssetSourceKindProceduralPrimitive,
		Primitive: "cube",
		Params: map[string]float32{
			"sx": 1,
			"sy": 1,
			"sz": 1,
		},
	}
}

func identityTransform() AssetTransformDef {
	return AssetTransformDef{
		Rotation: Quat{0, 0, 0, 1},
		Scale:    Vec3{1, 1, 1},
	}
}
