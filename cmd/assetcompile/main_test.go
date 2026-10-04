package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
)

func cliAssetFixture(t *testing.T) (string, string, *content.AssetDef) {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "source.authoring")
	output := filepath.Join(root, "compiled", "asset.gkassetc")
	transform := content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	asset := &content.AssetDef{ID: "asset", SchemaVersion: 4, Name: "CLI", Materials: []content.AssetMaterialDef{{ID: "mat", Name: "Material", IOR: 1.5}}, Parts: []content.AssetPartDef{{ID: "group", Name: "Group", Transform: transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindGroup}}, {ID: "shape", Name: "Shape", ParentID: "group", ModelScale: 1, VoxelResolution: 0.25, Transform: transform, Source: content.AssetSourceDef{Kind: content.AssetSourceKindVoxelShape, VoxelShape: &content.AssetVoxelShapeDef{Palette: []content.AssetVoxelPaletteEntryDef{{Value: 7, MaterialID: "mat"}}, Voxels: []content.VoxelObjectVoxelDef{{X: -1, Value: 7}, {X: 8, Y: 1, Value: 7}}}}}}}
	cliWriteAsset(t, input, asset)
	return input, output, asset
}
func cliWriteAsset(t *testing.T, path string, asset *content.AssetDef) {
	t.Helper()
	data, err := json.Marshal(asset)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func cliRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestAssetcompileWritesTypedClosureAndNoOpReusesFiles(t *testing.T) {
	input, output, asset := cliAssetFixture(t)
	source := cliRead(t, input)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal("CLI compile failed", err, stderr.String())
	}
	header, headerInfo, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil || header.Asset.ID != asset.ID || len(header.Shapes) != 1 || header.Asset.Parts[0].ID != "group" || header.Asset.Parts[1].ParentID != "group" {
		t.Fatal("CLI did not publish typed header", err)
	}
	shapePath := filepath.Join(filepath.Dir(output), header.Shapes[0].Path)
	shape, shapeInfo, err := content.LoadCompiledAssetShape(shapePath, nil)
	if err != nil || shapeInfo.ContentID != header.Shapes[0].ContentID || len(shape.Bricks) != 2 || shape.Bricks[0].Coord != [3]int32{-1, 0, 0} || shape.Bricks[1].Coord != [3]int32{1, 0, 0} || shape.Lattice.VoxelResolution != 0.25 {
		t.Fatal("CLI did not publish referenced signed geometry", err)
	}
	if !bytes.Equal(source, cliRead(t, input)) {
		t.Fatal("CLI changed authoring input")
	}
	oldHeader, oldShape := cliRead(t, output), cliRead(t, shapePath)
	fixed := time.Date(2001, time.January, 2, 3, 4, 5, 0, time.UTC)
	for _, path := range []string{output, shapePath} {
		if err := os.Chtimes(path, fixed, fixed); err != nil {
			t.Fatal(err)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal("CLI no-op failed", err)
	}
	if !bytes.Equal(oldHeader, cliRead(t, output)) || !bytes.Equal(oldShape, cliRead(t, shapePath)) {
		t.Fatal("CLI no-op changed immutable files")
	}
	for _, path := range []string{output, shapePath} {
		stat, err := os.Stat(path)
		if err != nil || !stat.ModTime().Equal(fixed) {
			t.Fatal("CLI no-op replaced existing file", path, err)
		}
	}
	if _, info, err := content.LoadCompiledAssetHeader(output, nil); err != nil || info != headerInfo {
		t.Fatal("CLI no-op changed header identity", err)
	}
}

func TestAssetcompileFlagValidationAndHelpDoNotWrite(t *testing.T) {
	input, output, _ := cliAssetFixture(t)
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("existing output")
	if err := os.WriteFile(output, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	source := cliRead(t, input)
	cases := map[string][]string{"missing-all": {}, "missing-in": {"-out", output}, "missing-out": {"-in", input}, "unknown": {"-in", input, "-out", output, "-unknown"}, "positional": {"-in", input, "-out", output, "extra"}}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(args, &stdout, &stderr); err == nil {
				t.Fatal("invalid CLI flags accepted")
			}
			if !bytes.Equal(sentinel, cliRead(t, output)) || !bytes.Equal(source, cliRead(t, input)) {
				t.Fatal("flag validation changed files")
			}
		})
	}
	for _, suffix := range []string{".gkasset", ".GKASSETC", ".gkassetc.extra", ""} {
		t.Run("suffix"+suffix, func(t *testing.T) {
			wrong := filepath.Join(filepath.Dir(output), "wrong"+suffix)
			var stdout, stderr bytes.Buffer
			err := run([]string{"-in", filepath.Join(t.TempDir(), "missing.authoring"), "-out", wrong}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), ".gkassetc") {
				t.Fatal("output suffix not validated before reading source", err)
			}
			if _, err := os.Stat(wrong); !os.IsNotExist(err) {
				t.Fatal("invalid suffix created output")
			}
		})
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-h", "-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal("help failed", err)
	}
	usage := stdout.String() + stderr.String()
	if !strings.Contains(usage, "-in") || !strings.Contains(usage, "-out") {
		t.Fatal("help omitted required inputs")
	}
	if !bytes.Equal(sentinel, cliRead(t, output)) || !bytes.Equal(source, cliRead(t, input)) {
		t.Fatal("help compiled or changed files")
	}
}

func TestAssetcompileCompilerErrorsPreservePublishedHeaderAndSource(t *testing.T) {
	for _, kind := range []string{"unsupported", "missing-id"} {
		t.Run(kind, func(t *testing.T) {
			input, output, asset := cliAssetFixture(t)
			var stdout, stderr bytes.Buffer
			if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
				t.Fatal(err)
			}
			old := cliRead(t, output)
			if kind == "unsupported" {
				asset.Parts[1].Source = content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube"}
			} else {
				asset.Parts[1].ID = ""
			}
			cliWriteAsset(t, input, asset)
			source := cliRead(t, input)
			if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err == nil {
				t.Fatal("compiler failure swallowed by CLI")
			}
			if !bytes.Equal(old, cliRead(t, output)) || !bytes.Equal(source, cliRead(t, input)) {
				t.Fatal("CLI error changed published header or authoring source")
			}
		})
	}
}
