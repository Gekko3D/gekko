package content

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func i07ReviewManifest() *TerrainChunkManifestDef {
	hash := strings.Repeat("c", 64)
	return &TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "review-terrain", SourceHash: strings.Repeat("a", 64), ChunkSize: 128, VoxelResolution: 2, Entries: []TerrainChunkEntryDef{{TerrainID: "review-terrain", SourceHash: strings.Repeat("a", 64), ChunkSize: 128, VoxelResolution: 2, HeightOffset: 10, HeightScale: 80, ChunkPath: "source.gkchunk", PayloadKind: TerrainHeightTilePayloadKind, PayloadHash: hash, PayloadSizeBytes: 32768}}, Pages: []StreamPageDef{{Level: StreamPageLevelRoot, BoundsMin: [3]float32{0, 10, 0}, BoundsMax: [3]float32{256, 90, 256}, Payload: StreamPagePayloadDef{Kind: TerrainHeightTilePayloadKind, Path: "page.gkchunk", ChunkSize: 128, SampleSpacing: 2, VoxelResolution: 1, HeightOffset: 10, HeightScale: 80, PayloadHash: hash, PayloadSizeBytes: 32768}}}, RootPageIndices: []uint32{0}}
}

func TestI07ReviewStrictPagedSourceAndVisualCellsMustRemainDistinct(t *testing.T) {
	if _, e := ValidateTerrainPageManifest(i07ReviewManifest()); e != nil {
		t.Fatalf("ordinary control failed: %v", e)
	}
	for _, origin := range []float32{33554432, -67108864} {
		for _, owner := range []string{"page", "source"} {
			t.Run(owner+"/"+fmtFloatI07(origin), func(t *testing.T) {
				d := i07ReviewManifest()
				if origin+2 != origin || origin+256 <= origin {
					t.Fatal("fixture must collapse cells while preserving complete tile extent")
				}
				if owner == "page" {
					p := &d.Pages[0]
					p.Payload.WorldOrigin[0] = origin
					p.BoundsMin[0] = origin
					p.BoundsMax[0] = origin + 256
				} else {
					e := &d.Entries[0]
					e.Coord.X = int(origin / 256)
					e.WorldOrigin[0] = origin
				}
				if index, e := ValidateTerrainPageManifest(d); e == nil || index != nil {
					t.Fatal("collapsed per-cell spacing accepted by strict paged validator")
				}
			})
		}
	}
}
func fmtFloatI07(v float32) string {
	if math.Signbit(float64(v)) {
		return "negative"
	}
	return "positive"
}

func TestI07ReviewConflictingCleanPayloadPathsFailClosed(t *testing.T) {
	// Exact header/body aliases are compatible. A different target visual
	// resolution does not change the backing height codec header.
	control := i07ReviewManifest()
	control.Entries[0].ChunkPath = control.Pages[0].Payload.Path
	if _, e := ValidateTerrainPageManifest(control); e != nil {
		t.Fatalf("qualified exact source/page alias rejected: %v", e)
	}
	for name, mutate := range map[string]func(*TerrainChunkManifestDef){
		"page-origin": func(d *TerrainChunkManifestDef) {
			p := d.Pages[0]
			p.Payload.Path = "pages/../page.gkchunk"
			p.Payload.WorldOrigin[0] = 256
			p.BoundsMin[0] = 256
			p.BoundsMax[0] = 512
			d.Pages = append(d.Pages, p)
			d.RootPageIndices = append(d.RootPageIndices, 1)
		},
		"page-grid": func(d *TerrainChunkManifestDef) {
			p := d.Pages[0]
			p.Payload.Path = "./page.gkchunk"
			p.Payload.ChunkSize = 64
			p.Payload.SampleSpacing = 4
			p.Payload.PayloadSizeBytes = 8192
			d.Pages = append(d.Pages, p)
			d.RootPageIndices = append(d.RootPageIndices, 1)
		},
		"source-page-origin": func(d *TerrainChunkManifestDef) {
			d.Entries[0].ChunkPath = "pages/../page.gkchunk"
			d.Entries[0].Coord.X = 1
			d.Entries[0].WorldOrigin[0] = 256
		},
		"source-page-calibration": func(d *TerrainChunkManifestDef) {
			d.Entries[0].ChunkPath = "./page.gkchunk"
			d.Entries[0].HeightScale = 81
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := i07ReviewManifest()
			mutate(d)
			if index, e := ValidateTerrainPageManifest(d); e == nil || index != nil {
				t.Fatal("one clean payload path accepted incompatible immutable headers")
			}
		})
	}
}

func TestI07ReviewTerrainWireRejectsDuplicateSchemaAliases(t *testing.T) {
	for name, raw := range map[string]string{
		"same-alias":        `{"schema_version":3,"SCHEMA_VERSION":3,"terrain_id":"t","source_hash":"h","chunk_size":1,"voxel_resolution":1}`,
		"conflicting-alias": `{"schema_version":3,"Schema_Version":2,"terrain_id":"t","source_hash":"h","chunk_size":1,"voxel_resolution":1,"pages":[]}`,
		"reverse-conflict":  `{"Schema_Version":3,"schema_version":2,"terrain_id":"t","source_hash":"h","chunk_size":1,"voxel_resolution":1}`,
		"exact-duplicate":   `{"schema_version":3,"schema_version":3,"terrain_id":"t","source_hash":"h","chunk_size":1,"voxel_resolution":1}`,
		"legacy-case-pages": `{"schema_version":2,"terrain_id":"t","chunk_size":1,"voxel_resolution":1,"PaGeS":[]}`,
		"legacy-case-roots": `{"schema_version":2,"terrain_id":"t","chunk_size":1,"voxel_resolution":1,"ROOT_PAGE_INDICES":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var d TerrainChunkManifestDef
			if e := json.Unmarshal([]byte(raw), &d); e == nil {
				t.Fatalf("ambiguous/legacy page wire accepted as version%d", d.SchemaVersion)
			}
		})
	}
}
