package main

import (
	"bytes"
	"github.com/gekko3d/gekko/content"
	"path/filepath"
	"strings"
	"testing"
)

func TestP5nCLIStaticCollapseExplicitShippingOptIn(t *testing.T) {
	input, output, a := cliAssetFixture(t)
	a.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	cliWriteAsset(t, input, a)
	before := cliRead(t, input)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err == nil {
		t.Fatal("default CLI silently accepted collapse")
	}
	if err := run([]string{"-in", input, "-out", output, "-static-collapse"}, &stdout, &stderr); err != nil {
		t.Fatal("static collapse flag failed", err)
	}
	h, _, err := content.LoadCompiledAssetHeader(output, nil)
	if err != nil || h.SchemaVersion != 3 || !h.Asset.Runtime.CollapseVoxelParts {
		t.Fatal("CLI did not publish explicit schema3", err)
	}
	if !bytes.Equal(before, cliRead(t, input)) {
		t.Fatal("CLI mutated authoring input")
	}
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"-h"}, &stdout, &stderr); err != nil || !strings.Contains(stdout.String()+stderr.String(), "-static-collapse") {
		t.Fatal("help omitted static collapse")
	}
	model := filepath.Join(t.TempDir(), "model.gkmodelassetc")
	if err := run([]string{"-in", input, "-out", model, "-static-collapse"}, &stdout, &stderr); err == nil {
		t.Fatal("static collapse flag accepted model output")
	}
}
func TestP5nCLIStaticFalsePreservesOrdinaryBytes(t *testing.T) {
	input, output := cliC3h4Eligible(t)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "ordinary.gkassetc")
	if err := run([]string{"-in", input, "-out", other, "-static-collapse=false"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(cliRead(t, output), cliRead(t, other)) {
		t.Fatal("explicit false changed default shipping bytes")
	}
}
