package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestI05ReviewLegacySectorBoundsAndFullRefsFailClosed(t *testing.T) {
	cases := map[string]func(*ImportedWorldDef){"zero-x": func(d *ImportedWorldDef) { d.Sectors[0].BoundsMax[0] = d.Sectors[0].BoundsMin[0] }, "zero-y": func(d *ImportedWorldDef) { d.Sectors[0].BoundsMax[1] = d.Sectors[0].BoundsMin[1] }, "zero-z": func(d *ImportedWorldDef) { d.Sectors[0].BoundsMax[2] = d.Sectors[0].BoundsMin[2] }, "no-full-refs-with-proxy": func(d *ImportedWorldDef) { d.Sectors[0].FullChunkRefs = nil; d.Entries = d.Entries[2:] }}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := i05LegacyWorld()
			mutate(d)
			if got, e := NormalizeImportedWorldPages(d); e == nil || got != nil {
				t.Errorf("normalizer admitted invalid legacy sector: indexPresent=%v err=%v", got != nil, e)
			}
			b, e := json.Marshal(d)
			if e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(t.TempDir(), "bad.gkworld")
			if e = os.WriteFile(p, b, 0600); e != nil {
				t.Fatal(e)
			}
			if got, e := LoadImportedWorld(p); e == nil || got != nil {
				t.Errorf("loader admitted invalid legacy sector: worldPresent=%v err=%v", got != nil, e)
			}
		})
	}
	empty, e := NormalizeImportedWorldPages(&ImportedWorldDef{SchemaVersion: 2})
	if e != nil || len(empty.Pages) != 0 || len(empty.Sectors) != 0 {
		t.Fatalf("empty world regression: %+v %v", empty, e)
	}
}

func TestI05ReviewLegacyWhitespaceProxyPreservesRuntimeEligibility(t *testing.T) {
	d := i05LegacyWorld()
	d.Sectors[0].LODs[0].ChunkPath = " "
	index, e := NormalizeImportedWorldPages(d)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(index.ProxylessPageIndices, []uint32{1}) || index.Pages[0].Payload.Path != " " {
		t.Fatalf("legacy firstLOD eligibility/path changed: proxyless=%v payload=%+v", index.ProxylessPageIndices, index.Pages[0].Payload)
	}
	b, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "space.gkworld")
	if e = os.WriteFile(p, b, 0600); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadImportedWorld(p)
	if e != nil {
		t.Fatal(e)
	}
	if loaded.PageIndex.Pages[0].Payload.Path != " " || !reflect.DeepEqual(loaded.PageIndex.ProxylessPageIndices, []uint32{1}) {
		t.Fatal("loader changed existing render eligibility")
	}
}

func TestI05ReviewStrictForestAcceptsUppercaseSHA256(t *testing.T) {
	pages, roots, leaves, payload := i05ForestFixture()
	pages[0].Payload.PayloadHash = strings.Repeat("A", 64)
	if _, e := ValidateStreamPageForest(pages, roots, leaves, payload); e != nil {
		t.Fatalf("syntactic SHA256 rejected: %v", e)
	}
}
