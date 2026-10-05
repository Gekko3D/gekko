package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func i06World() *ImportedWorldDef {
	d := &ImportedWorldDef{WorldID: "v3-world", SchemaVersion: ImportedWorldPageSchemaVersion, SourceHash: strings.Repeat("b", 64), ChunkSize: 4, VoxelResolution: 1, Entries: []ImportedWorldChunkEntryDef{{Coord: TerrainChunkCoordDef{}, ChunkPath: "full.gkchunk", NonEmptyVoxelCount: 1, PayloadKind: ImportedWorldChunkPayloadDenseRLEBinaryV1, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 24}}}
	for i, v := range []struct {
		level uint8
		side  int
		res   float32
	}{{StreamPageLevelLeaf, 4, 1}, {StreamPageLevelRegional, 8, 1}, {StreamPageLevelMacro, 4, 4}, {StreamPageLevelRoot, 2, 16}} {
		side := float32(v.side) * v.res
		p := StreamPageDef{Level: v.level, BoundsMax: [3]float32{side, side, side}, Payload: StreamPagePayloadDef{Kind: ImportedWorldChunkPayloadDenseRLEBinaryV1, Path: "page" + string(rune('0'+i)) + ".gkchunk", ChunkSize: v.side, VoxelResolution: v.res, PayloadHash: strings.Repeat("c", 64), PayloadSizeBytes: 32}, Tags: []string{"owned"}}
		if i == 0 {
			p.LeafEntryIndices = []uint32{0}
		} else {
			p.ChildPageIndices = []uint32{uint32(i - 1)}
		}
		d.Pages = append(d.Pages, p)
	}
	d.RootPageIndices = []uint32{3}
	d.IndexedSectors = []ImportedWorldSectorV3Def{{Coord: TerrainChunkCoordDef{}, BoundsMax: [3]float32{4, 4, 4}, FullChunkIndices: []uint32{0}, VisibleSectorIndices: []uint32{0}, Tags: []string{"indoor"}}}
	return d
}
func TestI06ImportedWorldV3StrictIndexAndWireRoundTrip(t *testing.T) {
	if ImportedWorldPageSchemaVersion != 3 || CurrentImportedWorldSchemaVersion != 2 {
		t.Fatal("independent explicitv3 version")
	}
	d := i06World()
	d.Pages[1].Payload.Aux = &ImportedWorldChunkAuxRefDef{AuxPath: "page1.gkaux", PayloadKind: ImportedWorldChunkAuxPayloadBinaryV1, PayloadHash: strings.Repeat("d", 64), PayloadSizeBytes: 16, NormalBakeVersion: ImportedWorldNormalBakeVersion, SourcePayloadHash: d.Pages[1].Payload.PayloadHash, SourcePayloadSizeBytes: d.Pages[1].Payload.PayloadSizeBytes}
	before := *d
	index, e := ValidateImportedWorldV3(d)
	if e != nil {
		t.Fatal(e)
	}
	if result := ValidateImportedWorld(d, ImportedWorldValidationOptions{}); result.HasErrors() {
		t.Fatalf("public validator did not dispatchv3: %s", result.Error())
	}
	if index.LegacyDistance || !reflect.DeepEqual(index.Forest.ParentPageIndices, []int{1, 2, 3, -1}) || !reflect.DeepEqual(index.Forest.LeafOwnerPageIndices, []int{0}) || index.EntryIndexByCoord[TerrainChunkCoordDef{}] != 0 || index.SectorIndexByCoord[TerrainChunkCoordDef{}] != 0 {
		t.Fatalf("strict runtime indexes %+v", index)
	}
	index.Pages[1].Payload.Aux.AuxPath = "changed"
	if d.Pages[1].Payload.Aux.AuxPath != "page1.gkaux" {
		t.Fatal("v3 pageaux ref aliases authoredsource")
	}
	index.Pages[0].Tags[0] = "changed"
	index.Pages[0].LeafEntryIndices[0] = 99
	index.Sectors[0].FullChunkIndices[0] = 99
	if d.Pages[0].Tags[0] != "owned" || d.Pages[0].LeafEntryIndices[0] != 0 || d.IndexedSectors[0].FullChunkIndices[0] != 0 {
		t.Fatal("v3 runtime index aliases source")
	}
	p := filepath.Join(t.TempDir(), "v3.gkworld")
	if e = SaveImportedWorld(p, d); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	var wire map[string]json.RawMessage
	if e = json.Unmarshal(b, &wire); e != nil {
		t.Fatal(e)
	}
	if _, ok := wire["pages"]; !ok {
		t.Fatal("missing pages")
	}
	if _, ok := wire["root_page_indices"]; !ok {
		t.Fatal("missing roots")
	}
	if _, ok := wire["IndexedSectors"]; ok {
		t.Fatal("API view serialized")
	}
	if !strings.Contains(string(wire["sectors"]), "full_chunk_indices") || strings.Contains(string(wire["sectors"]), "full_chunk_refs") {
		t.Fatal("v3 sector wire reference meaning")
	}
	loaded, e := LoadImportedWorld(p)
	if e != nil || loaded.SchemaVersion != 3 || loaded.PageIndex == nil || loaded.PageIndex.LegacyDistance || !reflect.DeepEqual(loaded.Pages, d.Pages) || !reflect.DeepEqual(loaded.IndexedSectors, d.IndexedSectors) {
		t.Fatalf("v3 roundtrip %v", e)
	}
	if d.SchemaVersion != before.SchemaVersion || len(d.Sectors) != 0 {
		t.Fatal("save changed version/sectorAPI")
	}
	d.IndexedSectors = nil
	if _, e := ValidateImportedWorldV3(d); e != nil {
		t.Fatalf("visibility sectors are optional: %v", e)
	}
}

func TestI06ImportedWorldV3RejectsUnsafeReferencesCoverageAndMetadata(t *testing.T) {
	cases := map[string]func(*ImportedWorldDef){"version": func(d *ImportedWorldDef) { d.SchemaVersion = 2 }, "world-id": func(d *ImportedWorldDef) { d.WorldID = " " }, "source-hash": func(d *ImportedWorldDef) { d.SourceHash = "identity" }, "grid": func(d *ImportedWorldDef) { d.ChunkSize = 0 }, "negative-leaf-cost": func(d *ImportedWorldDef) { d.Entries[0].OccupiedBrickCount = -1 },
		"leaf-kind": func(d *ImportedWorldDef) { d.Entries[0].PayloadKind = "" }, "leaf-hash": func(d *ImportedWorldDef) { d.Entries[0].PayloadHash = "" }, "leaf-size": func(d *ImportedWorldDef) { d.Entries[0].PayloadSizeBytes = 0 }, "leaf-coordinate": func(d *ImportedWorldDef) { d.Entries[0].Coord.X = 1 }, "duplicate-leaf": func(d *ImportedWorldDef) { d.Entries = append(d.Entries, d.Entries[0]) }, "page-height-kind": func(d *ImportedWorldDef) {
			d.Pages[0].Payload.Kind = TerrainHeightTilePayloadKind
			d.Pages[0].Payload.SampleSpacing = 1
		}, "payload-cube-outside": func(d *ImportedWorldDef) { d.Pages[0].Payload.WorldOrigin[0] = 1 }, "leaf-owner": func(d *ImportedWorldDef) { d.Pages[0].LeafEntryIndices = nil }, "sector-leaf-oob": func(d *ImportedWorldDef) { d.IndexedSectors[0].FullChunkIndices = []uint32{1} }, "sector-duplicate-leaf": func(d *ImportedWorldDef) { d.IndexedSectors[0].FullChunkIndices = []uint32{0, 0} }, "sector-adjacent-oob": func(d *ImportedWorldDef) { d.IndexedSectors[0].AdjacentSectorIndices = []uint32{99} },
		"sector-adjacent-duplicate": func(d *ImportedWorldDef) { d.IndexedSectors[0].AdjacentSectorIndices = []uint32{0, 0} },
		"aux-source-mismatch": func(d *ImportedWorldDef) {
			d.Pages[0].Payload.Aux = &ImportedWorldChunkAuxRefDef{AuxPath: "bad.gkaux", PayloadKind: ImportedWorldChunkAuxPayloadBinaryV1, PayloadHash: strings.Repeat("d", 64), PayloadSizeBytes: 16, NormalBakeVersion: ImportedWorldNormalBakeVersion, SourcePayloadHash: strings.Repeat("e", 64), SourcePayloadSizeBytes: d.Pages[0].Payload.PayloadSizeBytes}
		},
		"sector-pvs-oob": func(d *ImportedWorldDef) { d.IndexedSectors[0].VisibleSectorIndices = []uint32{99} }, "sector-pvs-duplicate": func(d *ImportedWorldDef) { d.IndexedSectors[0].VisibleSectorIndices = []uint32{0, 0} }}
	if got, e := ValidateImportedWorldV3(nil); e == nil || got != nil {
		t.Fatal("nil v3 accepted")
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := i06World()
			mutate(d)
			if got, e := ValidateImportedWorldV3(d); e == nil || got != nil {
				t.Fatalf("accepted invalidv3 %+v %v", got, e)
			}
			if name == "version" {
				return
			}
			p := filepath.Join(t.TempDir(), "invalid.gkworld")
			sentinel := []byte("previous manifest")
			if e := os.WriteFile(p, sentinel, 0600); e != nil {
				t.Fatal(e)
			}
			if e := SaveImportedWorld(p, d); e == nil {
				t.Fatal("saved invalidv3")
			}
			b, _ := os.ReadFile(p)
			if string(b) != string(sentinel) {
				t.Fatal("invalidsave replaced manifest")
			}
		})
	}
}

func TestI06ImportedWorldWireRejectsCrossVersionSectorReferenceKeys(t *testing.T) {
	for _, v := range []struct {
		version int
		key     string
	}{{3, "full_chunk_refs"}, {2, "full_chunk_indices"}, {1, "visible_sector_indices"}, {0, "adjacent_sector_indices"}} {
		d := i06World()

		b, e := json.Marshal(d)
		if e != nil {
			t.Fatal(e)
		}
		var fields map[string]json.RawMessage
		if e = json.Unmarshal(b, &fields); e != nil {
			t.Fatal(e)
		}
		fields["schema_version"] = json.RawMessage(strconv.Itoa(v.version))
		fields["sectors"] = json.RawMessage(`[{"coord":{"x":0,"z":0},"bounds_min":[0,0,0],"bounds_max":[4,4,4],"` + v.key + `":[]}]`)
		b, e = json.Marshal(fields)
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(t.TempDir(), "badwire.gkworld")
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		if got, e := LoadImportedWorld(p); e == nil || got != nil {
			t.Fatalf("version%d silently accepted key%s", v.version, v.key)
		}
	}
}

func TestI06LegacyWriterRejectsExplicitPageHalfContract(t *testing.T) {
	d := i06World()
	d.SchemaVersion = 2
	p := filepath.Join(t.TempDir(), "legacy.gkworld")
	sentinel := []byte("oldmanifest")
	if e := os.WriteFile(p, sentinel, 0600); e != nil {
		t.Fatal(e)
	}
	if e := SaveImportedWorld(p, d); e == nil {
		t.Fatal("legacy writer silently accepted explicitv3pages")
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != string(sentinel) {
		t.Fatal("rejected halfcontract changed manifest")
	}
}

func TestI06ImportedWorldV3SharedFullLeafPayloadMetadata(t *testing.T) {
	d := i06World()
	entry := d.Entries[0]
	d.Pages[0].Payload.Path = entry.ChunkPath
	d.Pages[0].Payload.Kind = entry.PayloadKind
	d.Pages[0].Payload.PayloadHash = entry.PayloadHash
	d.Pages[0].Payload.PayloadSizeBytes = entry.PayloadSizeBytes
	if _, e := ValidateImportedWorldV3(d); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*StreamPagePayloadDef){func(p *StreamPagePayloadDef) { p.PayloadHash = strings.Repeat("f", 64) }, func(p *StreamPagePayloadDef) { p.PayloadSizeBytes++ }, func(p *StreamPagePayloadDef) { p.Kind = ImportedWorldChunkPayloadSparseJSONV1 }} {
		bad := i06World()
		bad.Pages[0].Payload = d.Pages[0].Payload
		mutate(&bad.Pages[0].Payload)
		if got, e := ValidateImportedWorldV3(bad); e == nil || got != nil {
			t.Fatal("shared fullleaf payload refs disagree")
		}
	}
}

func TestI06ImportedWorldV3VisibilityMembershipDoesNotOwnVisualLeaves(t *testing.T) {
	d := i06World()
	first := d.IndexedSectors[0]
	first.VisibleSectorIndices = []uint32{0, 1}
	second := first
	second.Coord = TerrainChunkCoordDef{X: 1}
	second.FullChunkIndices = []uint32{0}
	second.VisibleSectorIndices = []uint32{1, 0}
	d.IndexedSectors = []ImportedWorldSectorV3Def{first, second}
	index, e := ValidateImportedWorldV3(d)
	if e != nil {
		t.Fatalf("distinct visibilitysectors cannot share fullmembership: %v", e)
	}
	if len(index.Sectors) != 2 || !reflect.DeepEqual(index.Sectors[0].FullChunkIndices, []uint32{0}) || !reflect.DeepEqual(index.Sectors[1].FullChunkIndices, []uint32{0}) || !reflect.DeepEqual(index.Forest.LeafOwnerPageIndices, []int{0}) {
		t.Fatal("visibility memberships changed visual page ownership")
	}
}
