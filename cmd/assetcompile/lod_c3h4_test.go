package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func cliC3h4Eligible(t *testing.T) (string, string) {
	t.Helper()
	input, output, a := cliAssetFixture(t)
	a.Materials[0].BaseColor = [4]uint8{1, 2, 3, 255}
	a.Parts[1].Source.VoxelShape.Voxels = []content.VoxelObjectVoxelDef{{X: -2, Value: 7}, {X: -1, Value: 7}, {X: 0, Value: 7}, {X: 1, Value: 7}}
	cliWriteAsset(t, input, a)
	return input, output
}

func TestC3h4CLIOptInAndLegacyStdout(t *testing.T) {
	input, output := cliC3h4Eligible(t)
	source := cliRead(t, input)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-in", input, "-out", output}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	want := "Header written: true; shapes: 1 written, 0 reused; dependencies: 0 written, 0 reused\n"
	if stdout.String() != want {
		t.Fatal("default stdout changed", stdout.String())
	}
	other := filepath.Join(t.TempDir(), "asset.gkassetc")
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"-in", input, "-out", other, "-lod2=false"}, &stdout, &stderr); err != nil {
		t.Fatal("explicit false rejected", err)
	}
	if stdout.String() != want || !bytes.Equal(cliRead(t, output), cliRead(t, other)) {
		t.Fatal("false changes legacy output")
	}
	enabled := filepath.Join(t.TempDir(), "asset.gkassetc")
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"-in", input, "-out", enabled, "-lod2"}, &stdout, &stderr); err != nil {
		t.Fatal("LOD CLI compile", err)
	}
	if stdout.String() != want+"LODs: 1 written, 0 reused\n" {
		t.Fatal("wrong opt-in summary", stdout.String())
	}
	h, _, err := content.LoadCompiledAssetHeader(enabled, nil)
	if err != nil || h.SchemaVersion != 2 || len(h.LODs) != 1 {
		t.Fatal("flag did not compile versioned derivative", err)
	}
	if _, _, err := content.LoadCompiledAssetLOD(filepath.Join(filepath.Dir(enabled), filepath.FromSlash(h.LODs[0].Path)), nil); err != nil {
		t.Fatal("CLI published incomplete derivative closure", err)
	}
	stdout.Reset()
	if err := run([]string{"-in", input, "-out", enabled, "-lod2"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "Header written: false; shapes: 0 written, 1 reused; dependencies: 0 written, 0 reused\nLODs: 0 written, 1 reused\n" {
		t.Fatal("wrong no-op LOD counts", stdout.String())
	}
	if !bytes.Equal(source, cliRead(t, input)) {
		t.Fatal("CLI changed source")
	}
}

func TestC3h4CLIHelpAndInvalidLODFlagPreserveFiles(t *testing.T) {
	input, output := cliC3h4Eligible(t)
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("existing published header")
	if err := os.WriteFile(output, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-h"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String()+stderr.String(), "-lod2") {
		t.Fatal("help omitted LOD opt-in")
	}
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"-in", input, "-out", output, "-lod2=maybe"}, &stdout, &stderr); err == nil {
		t.Fatal("invalid bool accepted")
	}
	if !bytes.Equal(sentinel, cliRead(t, output)) {
		t.Fatal("flag failure rewrote header")
	}
}
