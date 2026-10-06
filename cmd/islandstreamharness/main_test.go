package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func i09CLISource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "gasworks.gkworld")
	c := &content.ImportedWorldChunkDef{WorldID: "cli-gasworks", ChunkSize: 4, VoxelResolution: 1, Voxels: []content.ImportedWorldVoxelDef{{X: 1, Y: 1, Z: 1, Value: 1}}, NonEmptyVoxelCount: 1}
	result, e := content.SaveImportedWorldChunkWithOptionsResult(filepath.Join(dir, "full.gkchunk"), c, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
	if e != nil {
		t.Fatal(e)
	}
	d := &content.ImportedWorldDef{SchemaVersion: 2, WorldID: c.WorldID, Kind: content.ImportedWorldKindVoxelWorld, SourceHash: strings.Repeat("a", 64), ChunkSize: 4, VoxelResolution: 1, Entries: []content.ImportedWorldChunkEntryDef{{ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: result.PayloadKind, PayloadHash: result.PayloadHash, PayloadSizeBytes: result.PayloadSizeBytes}}, Sectors: []content.ImportedWorldSectorDef{{BoundsMax: [3]float32{4, 4, 4}, FullChunkRefs: []content.TerrainChunkCoordDef{{}}}}}
	if e = content.SaveImportedWorld(p, d); e != nil {
		t.Fatal(e)
	}
	return p
}
func i09CLIArgs(source, out string) []string {
	return []string{"-gasworks", source, "-out", out, "-playable-span", "256", "-coverage-span", "512", "-root-span", "256", "-macro-span", "64", "-regional-span", "16", "-height-tile-span", "256", "-height-sample-spacing", "2"}
}
func i09CLITree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	r := map[string][]byte{}
	e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(dir, p)
		if e != nil {
			return e
		}
		r[filepath.ToSlash(rel)], e = os.ReadFile(p)
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func TestI09CLIExactDocumentedFlagsPublishQualifiedFixture(t *testing.T) {
	source := i09CLISource(t)
	input := i09CLITree(t, filepath.Dir(source))
	out := filepath.Join(t.TempDir(), "harness")
	var stdout, stderr bytes.Buffer
	if code := run(i09CLIArgs(source, out), &stdout, &stderr); code != 0 {
		t.Fatalf("CLI failed code=%d stderr=%s", code, stderr.String())
	}
	lp := filepath.Join(out, "island_streaming_harness.gklevel")
	l, e := content.LoadLevel(lp)
	if e != nil {
		t.Fatal(e)
	}
	if l.StreamingBounds == nil || l.Terrain == nil || l.BaseWorld == nil {
		t.Fatal("incomplete fixture level")
	}
	if r := content.ValidateLevel(l, content.LevelValidationOptions{DocumentPath: lp}); r.HasErrors() {
		t.Fatalf("invalid CLI level: %+v", r.Issues)
	}
	tm, e := content.LoadTerrainChunkManifest(content.ResolveDocumentPath(l.Terrain.ManifestPath, lp))
	if e != nil {
		t.Fatal(e)
	}
	wp := content.ResolveDocumentPath(l.BaseWorld.ManifestPath, lp)
	poi, e := content.LoadImportedWorld(wp)
	if e != nil {
		t.Fatal(e)
	}
	if tm.SchemaVersion != 3 || poi.SchemaVersion != 3 {
		t.Fatal("CLI did not emit explicit3 layers")
	}
	if len(poi.Entries) != 1 || content.ResolveImportedWorldChunkPath(poi.Entries[0], wp) != filepath.Join(filepath.Dir(source), "full.gkchunk") {
		t.Fatal("CLI copied or moved original FULL payload")
	}
	if !reflect.DeepEqual(input, i09CLITree(t, filepath.Dir(source))) {
		t.Fatal("CLI wrote input asset tree")
	}
	before := i09CLITree(t, out)
	stdout.Reset()
	stderr.Reset()
	if code := run(i09CLIArgs(source, out), &stdout, &stderr); code != 0 {
		t.Fatalf("repeat failed: %s", stderr.String())
	}
	if !reflect.DeepEqual(before, i09CLITree(t, out)) {
		t.Fatal("repeat CLI output not byte-stable")
	}
}
func TestI09CLIRejectsInvalidFlagsBeforePublication(t *testing.T) {
	source := i09CLISource(t)
	cases := map[string][]string{"missing-required": {}, "unknown": {"-unsupported"}, "negative": {"-playable-span", "-1"}, "nan": {"-coverage-span", "NaN"}, "infinite": {"-root-span", "+Inf"}, "collapsed": {"-coverage-span", "128"}, "hierarchy": {"-macro-span", "63"}, "source-alignment": {"-regional-span", "15"}, "height-layout": {"-height-sample-spacing", ".3"}, "height-size": {"-height-tile-span", "128"}, "positional": {"unexpected"}}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "absent")
			args := i09CLIArgs(source, out)
			if name == "missing-required" {
				args = nil
			} else {
				args = append(args, extra...)
			}
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code == 0 {
				t.Fatal("invalid CLI accepted")
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatalf("invalid CLI created output: %v", e)
			}
		})
	}
}
func TestI09CLIMissingMalformedOrUnqualifiedSourceIsFailClosed(t *testing.T) {
	for _, bad := range []string{"missing", "malformed", "header-owner", "header-grid", "body"} {
		t.Run(bad, func(t *testing.T) {
			source := i09CLISource(t)
			dir := filepath.Dir(source)
			switch bad {
			case "missing":
				source = filepath.Join(dir, "absent.gkworld")
			case "malformed":
				if e := os.WriteFile(source, []byte(`{"schema_version":2,"entries":`), 0600); e != nil {
					t.Fatal(e)
				}
			case "header-owner", "header-grid":
				c, e := content.LoadImportedWorldChunk(filepath.Join(dir, "full.gkchunk"))
				if e != nil {
					t.Fatal(e)
				}
				if bad == "header-owner" {
					c.WorldID = "wrong"
				} else {
					c.ChunkSize = 8
				}
				if e = content.SaveImportedWorldChunkWithOptions(filepath.Join(dir, "full.gkchunk"), c, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1}); e != nil {
					t.Fatal(e)
				}
			case "body":
				p := filepath.Join(dir, "full.gkchunk")
				raw, e := os.ReadFile(p)
				if e != nil {
					t.Fatal(e)
				}
				raw[len(raw)-1] ^= 1
				if e = os.WriteFile(p, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			out := filepath.Join(t.TempDir(), "absent")
			var stdout, stderr bytes.Buffer
			if code := run(i09CLIArgs(source, out), &stdout, &stderr); code == 0 {
				t.Fatal("invalid source accepted")
			}
			if _, e := os.Stat(out); !os.IsNotExist(e) {
				t.Fatalf("source failure created output: %v", e)
			}
		})
	}
}
func TestI09CLIHelpAndOutputOverlapAreSafe(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-h"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help failed: %d", code)
	}
	help := stdout.String() + stderr.String()
	for _, flag := range []string{"gasworks", "out", "playable-span", "coverage-span", "root-span", "macro-span", "regional-span", "height-tile-span", "height-sample-spacing"} {
		if !strings.Contains(help, flag) {
			t.Fatalf("help missing %s", flag)
		}
	}
	source := i09CLISource(t)
	before := i09CLITree(t, filepath.Dir(source))
	if code := run(i09CLIArgs(source, filepath.Dir(source)), &stdout, &stderr); code == 0 {
		t.Fatal("CLI source/output overlap accepted")
	}
	if !reflect.DeepEqual(before, i09CLITree(t, filepath.Dir(source))) {
		t.Fatal("overlap changed source files")
	}
}
