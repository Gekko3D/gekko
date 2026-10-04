package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
)

func cliC3g11Model(t *testing.T, mixed bool) (string, string) {
	t.Helper()
	input, _, asset := cliAssetFixture(t)
	part := content.AssetPartDef{ID: "model", Name: "Model", ModelScale: 1.5, VoxelResolution: .125, Transform: asset.Parts[1].Transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube", Params: map[string]float32{"sx": 2, "sy": 3, "sz": 4}}}
	if mixed {
		asset.Materials[0].BaseColor = [4]uint8{1, 2, 3, 255}
		asset.Parts[1].Source.VoxelShape.Voxels = []content.VoxelObjectVoxelDef{{X: -2, Value: 7}, {X: -1, Value: 7}, {Value: 7}, {X: 1, Value: 7}}
		asset.Parts = append(asset.Parts, part)
	} else {
		asset.Parts = []content.AssetPartDef{part}
	}
	cliWriteAsset(t, input, asset)
	return input, filepath.Join(t.TempDir(), "asset.gkmodelassetc")
}

func TestC3g11ModelCLICompileAndNoOp(t *testing.T) {
	input, output := cliC3g11Model(t, false)
	source := cliRead(t, input)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal("new model selector", err)
	}
	want := "Header written: true; shapes: 0 written, 0 reused; dependencies: 0 written, 0 reused\nModels: 1 written, 0 reused\n"
	if stdout.String() != want {
		t.Fatal("model summary", stdout.String())
	}
	header, _, err := content.LoadCompiledAssetModelHeader(output, nil)
	if err != nil || len(header.Models) != 1 || len(header.Shapes) != 0 || len(header.LODs) != 0 || len(header.Palettes) != 1 {
		t.Fatal("model typed header", err)
	}
	modelPath := filepath.Join(filepath.Dir(output), header.Models[0].Path)
	model, info, err := content.LoadCompiledAssetModel(modelPath, nil)
	if err != nil || info.ContentID != header.Models[0].ContentID || model.Dimensions != ([3]uint32{3, 4, 6}) || model.Lattice.VoxelResolution != .125 {
		t.Fatal("model declared bounds/lattice", err)
	}
	palette := header.Palettes[0]
	if palette.ID != header.Models[0].PaletteID || !palette.IsPBR || palette.Roughness != 1 || palette.IOR != 1.5 || palette.Colors[1] != ([4]uint8{255, 255, 255, 255}) {
		t.Fatal("default procedural baked palette changed")
	}
	fixed := time.Date(2001, time.January, 2, 3, 4, 5, 0, time.UTC)
	oldHeader, oldModel := cliRead(t, output), cliRead(t, modelPath)
	for _, path := range []string{output, modelPath} {
		if err := os.Chtimes(path, fixed, fixed); err != nil {
			t.Fatal(err)
		}
	}
	stdout.Reset()
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "Header written: false; shapes: 0 written, 0 reused; dependencies: 0 written, 0 reused\nModels: 0 written, 1 reused\n" || !bytes.Equal(oldHeader, cliRead(t, output)) || !bytes.Equal(oldModel, cliRead(t, modelPath)) || !bytes.Equal(source, cliRead(t, input)) {
		t.Fatal("model no-op summary or bytes changed")
	}
	for _, path := range []string{output, modelPath} {
		stat, err := os.Stat(path)
		if err != nil || !stat.ModTime().Equal(fixed) {
			t.Fatal("model no-op rewrote file", err)
		}
	}
}

func TestC3g11MixedModelCLIInlineLODOnly(t *testing.T) {
	input, output := cliC3g11Model(t, true)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", input, "-out", output, "-lod2"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 3 || lines[0] != "Header written: true; shapes: 1 written, 0 reused; dependencies: 0 written, 0 reused" || !strings.Contains(stdout.String(), "Models: 1 written, 0 reused\n") || !strings.Contains(stdout.String(), "LODs: 1 written, 0 reused\n") {
		t.Fatal("mixed summaries changed", stdout.String())
	}
	header, _, err := content.LoadCompiledAssetModelHeader(output, nil)
	if err != nil || len(header.Models) != 1 || len(header.LODs) != 1 || header.LODs[0].PartID != "shape" {
		t.Fatal("LOD flag acquired model intent or lost eligible inline", err)
	}
	if _, _, err := content.LoadCompiledAssetLOD(filepath.Join(filepath.Dir(output), header.LODs[0].Path), nil); err != nil {
		t.Fatal("mixed CLI missing LOD frame", err)
	}
	// The original selector retains its exact historical stdout and rejects model source kinds.
	legacyInput, legacyOutput := cliC3h4Eligible(t)
	stdout.Reset()
	if err := run([]string{"-in", legacyInput, "-out", legacyOutput}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "Header written: true; shapes: 1 written, 0 reused; dependencies: 0 written, 0 reused\n" {
		t.Fatal("legacy CLI summary widened", stdout.String())
	}
	stdout.Reset()
	if err := run([]string{"-in", input, "-out", filepath.Join(t.TempDir(), "old.gkassetc")}, &stdout, &stderr); err == nil || stdout.Len() != 0 {
		t.Fatal("old CLI compiler accepted model input")
	}
}

func TestC3g11ModelCLIHelpAndSelectorPrecedence(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-h"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	usage := stdout.String() + stderr.String()
	if !strings.Contains(usage, ".gkassetc") || !strings.Contains(usage, ".gkmodelassetc") || !strings.Contains(usage, "inline") || !strings.Contains(usage, "-lod2") {
		t.Fatal("help omitted explicit model selector/inline LOD boundary")
	}
	for _, suffix := range []string{".GKMODELASSETC", ".gkmodelassetc.extra", ".gkmodelasset", ".json"} {
		output := filepath.Join(t.TempDir(), "out"+suffix)
		stdout.Reset()
		stderr.Reset()
		if err := run([]string{"-in", "missing-source", "-out", output}, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), ".gkmodelassetc") || stdout.Len() != 0 {
			t.Fatal("suffix validation did not precede source IO", err)
		}
		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatal("invalid selector created output")
		}
	}
	input, output := cliC3g11Model(t, false)
	if err := os.WriteFile(input, []byte(`{"schema_version":4,"id":"asset","unknown":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err == nil || stdout.Len() != 0 {
		t.Fatal("model selector accepted nonstrict authoring JSON")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("invalid authoring wrote model header")
	}
}
