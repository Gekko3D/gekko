package derived

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func i09Fixture(t *testing.T) (*content.ImportedWorldDef, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) {
	t.Helper()
	d, chunks := i06Source()
	d.SourceHash = strings.Repeat("a", 64)
	d.Materials = []content.ImportedWorldMaterialDef{{ID: 6, PaletteIndex: 6}, {ID: 7, PaletteIndex: 7}, {ID: 9, PaletteIndex: 9, Kind: "glass", Transparent: true}}
	for coord, c := range chunks {
		c.Voxels = append(c.Voxels, content.ImportedWorldVoxelDef{X: 3, Y: 0, Z: 0, Value: 3, MaterialValue: 9})
		c.NonEmptyVoxelCount = len(c.Voxels)
		chunks[coord] = c
	}
	for i := range d.Entries {
		e := &d.Entries[i]
		c := chunks[e.Coord]
		result, err := content.SaveImportedWorldChunkWithOptionsResult(filepath.Join(t.TempDir(), "full.gkchunk"), c, content.ImportedWorldChunkSaveOptions{PayloadKind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1})
		if err != nil {
			t.Fatal(err)
		}
		if result.PayloadKind != content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
			t.Fatal("material fixture did not auto-upgrade binary kind")
		}
		e.NonEmptyVoxelCount = len(c.Voxels)
		e.PayloadKind, e.PayloadHash, e.PayloadSizeBytes = result.PayloadKind, result.PayloadHash, result.PayloadSizeBytes
	}
	d.Sectors = content.BuildImportedWorldSectors(d.Entries, d.ChunkSize, d.VoxelResolution, content.DefaultImportedWorldSectorTargetWorldSize)
	return d, chunks
}

func i09Build(t *testing.T, d *content.ImportedWorldDef, chunks map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, o IslandStreamHarnessOptions) *IslandStreamHarness {
	t.Helper()
	calls := map[content.TerrainChunkCoordDef]int{}
	b, err := BuildIslandStreamHarness(d, func(e content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
		calls[e.Coord]++
		return chunks[e.Coord], nil
	}, o)
	if err != nil || b == nil {
		t.Fatalf("harness build: %v", err)
	}
	if len(calls) != len(d.Entries) {
		t.Fatalf("source loading omitted entries: %v", calls)
	}
	for _, n := range calls {
		if n != 1 {
			t.Fatalf("source chunk loaded repeatedly: %v", calls)
		}
	}
	return b
}

func i09Tag(tags []string, prefix string) string {
	for _, tag := range tags {
		if strings.HasPrefix(tag, prefix) {
			return strings.TrimPrefix(tag, prefix)
		}
	}
	return ""
}

func i09Routes(t *testing.T, l *content.LevelDef) map[string][]content.LevelMarkerDef {
	t.Helper()
	routes := map[string][]content.LevelMarkerDef{}
	ids := map[string]bool{}
	for _, m := range l.Markers {
		if m.ID == "" || ids[m.ID] {
			t.Fatal("empty or duplicate deterministic marker identity")
		}
		ids[m.ID] = true
		if m.Kind != "stream_benchmark_waypoint" {
			continue
		}
		name := i09Tag(m.Tags, "route:")
		order := i09Tag(m.Tags, "order:")
		if name == "" || len(order) != 3 {
			t.Fatalf("malformed route marker %+v", m)
		}
		if _, err := strconv.Atoi(order); err != nil {
			t.Fatal(err)
		}
		routes[name] = append(routes[name], m)
	}
	for name, markers := range routes {
		sort.Slice(markers, func(i, j int) bool { return i09Tag(markers[i].Tags, "order:") < i09Tag(markers[j].Tags, "order:") })
		for i, m := range markers {
			n, _ := strconv.Atoi(i09Tag(m.Tags, "order:"))
			if n != i {
				t.Fatalf("route %s has noncontiguous order", name)
			}
		}
		routes[name] = markers
	}
	return routes
}

func i09RouteLength(markers []content.LevelMarkerDef) float64 {
	var total float64
	for i := 1; i < len(markers); i++ {
		a, b := markers[i-1].Transform.Position, markers[i].Transform.Position
		total += math.Hypot(float64(a[0]-b[0]), float64(a[2]-b[2]))
	}
	return total
}

func i09HasTag(markers []content.LevelMarkerDef, tag string) bool {
	for _, m := range markers {
		for _, s := range m.Tags {
			if s == tag {
				return true
			}
		}
	}
	return false
}

func i09PublicEqual(a, b *IslandStreamHarness) bool {
	return reflect.DeepEqual([]any{a.Level, a.Terrain, a.POI, a.SourceTiles, a.TerrainPageTiles, a.POIPageChunks}, []any{b.Level, b.Terrain, b.POI, b.SourceTiles, b.TerrainPageTiles, b.POIPageChunks})
}

// Draft geometry is qualified with explicitly fictional hashes for metadata
// validation only. Publication tests qualify actual emitted bodies and headers.
func i09ValidateDraftGeometry(t *testing.T, b *IslandStreamHarness) {
	t.Helper()
	var terrain content.TerrainChunkManifestDef
	var poi content.ImportedWorldDef
	if err := json.Unmarshal(i06JSON(t, b.Terrain), &terrain); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(i06JSON(t, b.POI), &poi); err != nil {
		t.Fatal(err)
	}
	for i := range terrain.Entries {
		e := &terrain.Entries[i]
		tile := b.SourceTiles[e.Coord]
		if tile == nil || tile.TerrainID != terrain.TerrainID || tile.SourceHash != terrain.SourceHash {
			t.Fatal("backing tile owner/generation lost")
		}
		if err := content.ValidateTerrainHeightTile(tile); err != nil {
			t.Fatal(err)
		}
		e.PayloadHash = strings.Repeat("a", 64)
		e.PayloadSizeBytes = len(i07Raw(tile))
		e.PayloadKind = content.TerrainHeightTilePayloadKind
	}
	for i := range terrain.Pages {
		p := &terrain.Pages[i].Payload
		tile := b.TerrainPageTiles[p.Path]
		if tile == nil || tile.TerrainID != terrain.TerrainID || tile.SourceHash != terrain.SourceHash {
			t.Fatal("visual tile owner/generation lost")
		}
		if err := content.ValidateTerrainHeightTile(tile); err != nil {
			t.Fatal(err)
		}
		p.PayloadHash = strings.Repeat("b", 64)
		p.PayloadSizeBytes = len(i07Raw(tile))
	}
	for i := range poi.Pages {
		p := &poi.Pages[i].Payload
		if c := b.POIPageChunks[p.Path]; c != nil {
			if c.WorldID != poi.WorldID {
				t.Fatal("coarse payload owner lost")
			}
			p.Kind = content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1
			p.PayloadHash = strings.Repeat("c", 64)
			p.PayloadSizeBytes = 32
			p.Aux = nil
		}
	}
	if _, err := content.ValidateTerrainPageManifest(&terrain); err != nil {
		t.Fatalf("draft terrain geometry: %v", err)
	}
	if _, err := content.ValidateImportedWorldV3(&poi); err != nil {
		t.Fatalf("draft POI geometry: %v", err)
	}
	if _, err := content.BuildLevelStreamingIndex(b.Level, &terrain, &poi); err != nil {
		t.Fatalf("authored bounds or membership: %v", err)
	}
}

func TestI09DefaultHarnessRootsPartitionRoutesAndBacking(t *testing.T) {
	d, chunks := i09Fixture(t)
	b := i09Build(t, d, chunks, IslandStreamHarnessOptions{})
	if b.Level == nil || b.Terrain == nil || b.POI == nil || b.Terrain.SchemaVersion != 3 || b.POI.SchemaVersion != 3 || b.Level.StreamingBounds == nil {
		t.Fatal("missing independent v3 harness layers")
	}
	if i09Tag(b.Level.Tags, "fixture_recipe:") != "island_stream_harness_v1" {
		t.Fatal("unversioned temporary fixture recipe")
	}
	if len(b.Terrain.RootPageIndices) != 64 {
		t.Fatalf("root count %d", len(b.Terrain.RootPageIndices))
	}
	roots := map[[2]int]bool{}
	rootBytes := 0
	for _, index := range b.Terrain.RootPageIndices {
		p := b.Terrain.Pages[index]
		tile := b.TerrainPageTiles[p.Payload.Path]
		if p.Level != content.StreamPageLevelRoot || tile == nil || tile.SampleWidth != 128 || tile.SampleHeight != 128 || tile.SampleSpacing != 16 {
			t.Fatal("root profile missing payload")
		}
		if p.BoundsMax[0]-p.BoundsMin[0] != 2048 || p.BoundsMax[2]-p.BoundsMin[2] != 2048 {
			t.Fatal("root coverage span changed")
		}
		key := [2]int{int(tile.WorldOrigin[0] / 2048), int(tile.WorldOrigin[2] / 2048)}
		if key[0] < -4 || key[0] >= 4 || key[1] < -4 || key[1] >= 4 || roots[key] {
			t.Fatalf("padded root coverage %v", key)
		}
		roots[key] = true
		rootBytes += len(i07Raw(tile))
	}
	if rootBytes > 128<<20 {
		t.Fatalf("root height payload budget %d", rootBytes)
	}
	for _, p := range b.Terrain.Pages {
		if len(p.LeafEntryIndices) != 0 || p.CoverageGroup != "" {
			t.Fatal("backing source ownership or inferred replacement group")
		}
		if len(p.ChildPageIndices) == 0 {
			continue
		}
		if len(p.ChildPageIndices) != 16 {
			t.Fatal("partial child set cannot replace whole parent")
		}
		cells := map[[2]int]bool{}
		for _, child := range p.ChildPageIndices {
			c := b.Terrain.Pages[child]
			if c.Level+1 != p.Level {
				t.Fatal("missing terrain hierarchy tier")
			}
			span := (p.BoundsMax[0] - p.BoundsMin[0]) / 4
			x, z := int((c.BoundsMin[0]-p.BoundsMin[0])/span), int((c.BoundsMin[2]-p.BoundsMin[2])/span)
			if x < 0 || x >= 4 || z < 0 || z >= 4 || c.BoundsMax[0]-c.BoundsMin[0] != span || c.BoundsMax[2]-c.BoundsMin[2] != span || cells[[2]int{x, z}] {
				t.Fatal("child payloads do not partition parent XZ")
			}
			cells[[2]int{x, z}] = true
		}
	}
	routes := i09Routes(t, b.Level)
	for _, name := range []string{"boot_pan", "scale_crossing", "regional_loop", "poi_roundtrip", "boundary_ping_pong", "teleport_return"} {
		if len(routes[name]) == 0 {
			t.Fatalf("missing route %s", name)
		}
	}
	if len(routes) != 6 || !i09HasTag(routes["boot_pan"], "yaw:360") {
		t.Fatal("route recipe or startup pan missing")
	}
	cross := routes["scale_crossing"]
	if cross[0].Transform.Position[0] != -7500 || cross[len(cross)-1].Transform.Position[0] != 7500 || !i09HasTag(cross, "speed:50") || !i09HasTag(cross, "hold:10") {
		t.Fatal("15km scale crossing recipe")
	}
	loop := routes["regional_loop"]
	if math.Abs(i09RouteLength(loop)-4000) > .01 || loop[0].Transform.Position != loop[len(loop)-1].Transform.Position || !i09HasTag(loop, "hold:10") {
		t.Fatal("regional loop must close over4km")
	}
	poi := routes["poi_roundtrip"]
	if poi[0].Transform.Position[0] != -3000 || poi[len(poi)-1].Transform.Position != poi[0].Transform.Position || !i09HasTag(poi, "speed:6") || !i09HasTag(poi, "hold:10") {
		t.Fatal("POI approach/return recipe")
	}
	ping := routes["boundary_ping_pong"]
	for _, boundary := range []float32{128, 384} {
		crossings := 0
		for i := 1; i < len(ping); i++ {
			a, c := ping[i-1].Transform.Position[0], ping[i].Transform.Position[0]
			if (a < boundary && c > boundary) || (a > boundary && c < boundary) {
				crossings++
			}
		}
		if crossings < 20 {
			t.Fatalf("boundary %v has %d crossings", boundary, crossings)
		}
	}
	tele := routes["teleport_return"]
	if len(tele) < 3 || i09RouteLength(tele) < 24000 || tele[0].Transform.Position != tele[len(tele)-1].Transform.Position || !i09HasTag(tele, "teleport:true") || !i09HasTag(tele, "destination_wait:true") || !i09HasTag(tele, "hold:10") {
		t.Fatal("teleport destination/readiness recipe")
	}
	if len(b.SourceTiles) == 0 || len(b.SourceTiles) >= 4096 {
		t.Fatal("route-local backing became whole island")
	}
	// Independent point checks cover every route segment, including each teleport
	// destination. Regional payloads and backing sources must cover the corridor.
	for name, markers := range routes {
		for i, m := range markers {
			previous := m.Transform.Position
			if i > 0 && name != "teleport_return" {
				previous = markers[i-1].Transform.Position
			}
			distance := math.Hypot(float64(m.Transform.Position[0]-previous[0]), float64(m.Transform.Position[2]-previous[2]))
			n := max(1, int(math.Ceil(distance/128)))
			for step := 0; step <= n; step++ {
				f := float32(step) / float32(n)
				x, z := previous[0]+f*(m.Transform.Position[0]-previous[0]), previous[2]+f*(m.Transform.Position[2]-previous[2])
				coord := content.TerrainChunkCoordDef{X: int(math.Floor(float64(x) / 256)), Z: int(math.Floor(float64(z) / 256))}
				if b.SourceTiles[coord] == nil {
					t.Fatalf("route backing gap at%v,%v", x, z)
				}
				for _, offset := range [][2]float32{{-640, 0}, {640, 0}, {0, -640}, {0, 640}} {
					bufferX, bufferZ := x+offset[0], z+offset[1]
					if bufferX < -8192 || bufferX >= 8192 || bufferZ < -8192 || bufferZ >= 8192 {
						continue
					}
					bufferCoord := content.TerrainChunkCoordDef{X: int(math.Floor(float64(bufferX) / 256)), Z: int(math.Floor(float64(bufferZ) / 256))}
					if b.SourceTiles[bufferCoord] == nil {
						t.Fatalf("640m route keep corridor backing gap at%v,%v", bufferX, bufferZ)
					}
				}
				found := false
				for _, p := range b.Terrain.Pages {
					if p.Level == content.StreamPageLevelRegional && x >= p.BoundsMin[0] && x <= p.BoundsMax[0] && z >= p.BoundsMin[2] && z <= p.BoundsMax[2] {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("route regional gap at%v,%v", x, z)
				}
			}
		}
	}
	for _, tile := range b.SourceTiles {
		if tile.SampleWidth != 128 || tile.SampleHeight != 128 || tile.SampleSpacing != 2 || tile.HeightOffset != -64 || tile.HeightScale != 128 || len(tile.SurfaceMask) != 0 || tile.OutdoorNavCellSize != 1 || len(tile.OutdoorNavExclusionMask) != 8192 {
			t.Fatal("backing profile or overlay mask changed")
		}
		for _, sample := range []int{0, 63, 127, 8192, 16383} {
			x := float64(tile.WorldOrigin[0]) + float64(sample%128)*2 + 1
			z := float64(tile.WorldOrigin[2]) + float64(sample/128)*2 + 1
			want := float64(0)
			if x < -7500 || x > 7500 || z < -7500 || z > 7500 {
				want = -16
			}
			if math.Abs(i07Height(tile, sample)-want) > 128.0/65535 {
				t.Fatal("source analytical height recipe")
			}
		}
		for _, cell := range []int{0, 127, 255, 32768, 65535} {
			x := float64(tile.WorldOrigin[0]) + float64(cell%256) + .5
			z := float64(tile.WorldOrigin[2]) + float64(cell/256) + .5
			excluded := x < -7500 || x > 7500 || z < -7500 || z > 7500
			for _, e := range d.Entries {
				side := float64(float32(d.ChunkSize) * d.VoxelResolution)
				minX, minZ := float64(e.Coord.X)*side, float64(e.Coord.Z)*side
				if x >= minX && x <= minX+side && z >= minZ && z <= minZ+side {
					excluded = true
				}
			}
			got := tile.OutdoorNavExclusionMask[cell/8]&(1<<uint(cell%8)) != 0
			if got != excluded {
				t.Fatal("navigation exclusions disagree with authored full footprints")
			}
		}
	}
	for _, tile := range b.TerrainPageTiles {
		if tile.HeightOffset != -64 || tile.HeightScale != 128 || tile.OutdoorNavCellSize != 0 || len(tile.OutdoorNavExclusionMask) != 0 || len(tile.SurfaceMask) != 0 {
			t.Fatal("coarse overlay payload calibration/masks")
		}
		for _, sample := range []int{0, len(tile.HeightSamples) / 2, len(tile.HeightSamples) - 1} {
			x := float64(tile.WorldOrigin[0]) + (float64(sample%tile.SampleWidth)+.5)*float64(tile.SampleSpacing)
			z := float64(tile.WorldOrigin[2]) + (float64(sample/tile.SampleWidth)+.5)*float64(tile.SampleSpacing)
			want := float64(0)
			if x < -7500 || x > 7500 || z < -7500 || z > 7500 {
				want = -16
			}
			if math.Abs(i07Height(tile, sample)-want) > 128.0/65535 {
				t.Fatal("coarse analytical height recipe")
			}
		}
	}
	i09ValidateDraftGeometry(t, b)
}

func TestI09HarnessDeterminismExternalReferencesAndOpaqueOriginalCells(t *testing.T) {
	d, chunks := i09Fixture(t)
	before := append([]byte(nil), i06JSON(t, d)...)
	chunkBefore := map[content.TerrainChunkCoordDef][]byte{}
	for coord, c := range chunks {
		chunkBefore[coord] = i06JSON(t, c)
	}
	a := i09Build(t, d, chunks, IslandStreamHarnessOptions{})
	if !bytes.Equal(before, i06JSON(t, d)) {
		t.Fatal("building mutated source metadata")
	}
	for coord, c := range chunks {
		if !bytes.Equal(chunkBefore[coord], i06JSON(t, c)) {
			t.Fatal("building mutated borrowed chunk geometry")
		}
	}
	d.Entries[0], d.Entries[1] = d.Entries[1], d.Entries[0]
	for _, c := range chunks {
		for i, j := 0, len(c.Voxels)-1; i < j; i, j = i+1, j-1 {
			c.Voxels[i], c.Voxels[j] = c.Voxels[j], c.Voxels[i]
		}
	}
	b := i09Build(t, d, chunks, IslandStreamHarnessOptions{})
	if !i09PublicEqual(a, b) {
		t.Fatal("source order changed deterministic harness IDs/payloads/markers")
	}
	for _, e := range a.POI.Entries {
		found := false
		for _, source := range d.Entries {
			if source.Coord == e.Coord {
				if e.OccupiedSectorCount != 1 || e.OccupiedBrickCount != 1 {
					t.Fatal("FULL stream costs not qualified from actual tiny geometry")
				}
				source.OccupiedSectorCount, source.OccupiedBrickCount = e.OccupiedSectorCount, e.OccupiedBrickCount
				if !reflect.DeepEqual(source, e) {
					t.Fatal("FULL reference metadata changed rather than preserved")
				}
				found = true
			}
		}
		if !found {
			t.Fatal("fabricated FULL entry")
		}
	}
	rootVoxels := 0
	for _, p := range a.POI.Pages {
		if p.CoverageGroup != "" {
			t.Fatal("overlay invented replacement group")
		}
		if p.Level == content.StreamPageLevelLeaf {
			if a.POIPageChunks[p.Payload.Path] != nil {
				t.Fatal("external FULL geometry retained as generated page artifact")
			}
			continue
		}
		c := a.POIPageChunks[p.Payload.Path]
		if c == nil {
			t.Fatal("coarse original-cell aggregate missing")
		}
		for _, v := range c.Voxels {
			if content.ImportedWorldVoxelMaterialValue(v) == 9 {
				t.Fatal("transparent source leaked into silhouette")
			}
			if p.Level == content.StreamPageLevelRoot {
				rootVoxels++
				if v.Value != 1 || content.ImportedWorldVoxelMaterialValue(v) != 6 {
					t.Fatal("dominant opaque material/value tie not deterministic")
				}
			}
		}
	}
	if rootVoxels == 0 {
		t.Fatal("missing landmark mass at startup root")
	}
	levelBefore, terrainBefore, poiBefore := i06JSON(t, a.Level), i06JSON(t, a.Terrain), i06JSON(t, a.POI)
	pageBefore := map[string][]byte{}
	for path, c := range a.POIPageChunks {
		pageBefore[path] = i06JSON(t, c)
	}
	d.Entries[0].ChunkPath = "input-mutated"
	d.Materials[0].Kind = "glass"
	for _, c := range chunks {
		c.Voxels[0].Value = 255
	}
	if !bytes.Equal(levelBefore, i06JSON(t, a.Level)) || !bytes.Equal(terrainBefore, i06JSON(t, a.Terrain)) || !bytes.Equal(poiBefore, i06JSON(t, a.POI)) {
		t.Fatal("borrowed source mutation altered owned metadata")
	}
	for path, c := range a.POIPageChunks {
		if !bytes.Equal(pageBefore[path], i06JSON(t, c)) {
			t.Fatal("borrowed chunk mutation altered generated coarse geometry")
		}
	}
	if !i09PublicEqual(a, b) {
		t.Fatal("source mutation altered owned generated artifact")
	}
	a.Level.Markers[0].Tags[0] = "result-mutated"
	if i09PublicEqual(a, b) {
		t.Fatal("independent build outputs share marker ownership")
	}
}

func TestI09HarnessRejectsUnsafeOptionsBeforeSourceLoading(t *testing.T) {
	d, _ := i09Fixture(t)
	for name, options := range map[string]IslandStreamHarnessOptions{
		"negative-span": {PlayableSpan: -1}, "nan": {CoverageSpan: float32(math.NaN())}, "infinite": {RootSpan: float32(math.Inf(1))},
		"outside-coverage": {PlayableSpan: 20000, CoverageSpan: 16384}, "unaligned-coverage": {CoverageSpan: 16000}, "wrong-parent-ratio": {MacroSpan: 384},
		"wrong-source-grid": {HeightTileSpan: 255}, "wrong-sample-grid": {HeightSampleSpacing: 3}, "negative-source-budget": {MaxSourceTiles: -1}, "negative-page-budget": {MaxPages: -1},
		"source-budget": {MaxSourceTiles: 1}, "page-budget": {MaxPages: 1}, "unsafe-large": {CoverageSpan: float32(math.MaxFloat32)},
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			b, err := BuildIslandStreamHarness(d, func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
				calls++
				return nil, errors.New("must not load")
			}, options)
			if err == nil || b != nil || calls != 0 {
				t.Fatalf("invalid/capped options consumed source: b%v err%v calls%d", b, err, calls)
			}
		})
	}
	if b, err := BuildIslandStreamHarness(nil, nil, IslandStreamHarnessOptions{}); err == nil || b != nil {
		t.Fatal("nil source accepted")
	}
	if b, err := BuildIslandStreamHarness(d, nil, IslandStreamHarnessOptions{}); err == nil || b != nil {
		t.Fatal("nil chunk loader accepted")
	}
}

func TestI09HarnessFailsAtomicallyForInvalidSourceCallbacks(t *testing.T) {
	for _, mode := range []string{"callback-error", "nil-chunk", "wrong-owner", "wrong-coordinate", "duplicate-voxel"} {
		t.Run(mode, func(t *testing.T) {
			d, chunks := i09Fixture(t)
			b, err := BuildIslandStreamHarness(d, func(e content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error) {
				if mode == "callback-error" {
					return nil, errors.New("fixture load failure")
				}
				if mode == "nil-chunk" {
					return nil, nil
				}
				c := *chunks[e.Coord]
				c.Voxels = append([]content.ImportedWorldVoxelDef(nil), c.Voxels...)
				switch mode {
				case "wrong-owner":
					c.WorldID = "another-world"
				case "wrong-coordinate":
					c.Coord.X += 100
				case "duplicate-voxel":
					c.Voxels = append(c.Voxels, c.Voxels[0])
					c.NonEmptyVoxelCount++
				}
				return &c, nil
			}, IslandStreamHarnessOptions{})
			if err == nil || b != nil {
				t.Fatalf("published partial/invalid builder result: %v", err)
			}
		})
	}
}
