package derived

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
)

const islandHarnessRecipe = "island_stream_harness_v1"

type IslandStreamHarnessOptions struct {
	PlayableSpan, CoverageSpan, RootSpan, MacroSpan, RegionalSpan, HeightTileSpan, HeightSampleSpacing float32
	MaxSourceTiles, MaxPages                                                                           int
}

// IslandStreamHarness is an owned, sealed offline fixture draft. It retains
// generated payloads and external FULL references, never full source geometry.
type IslandStreamHarness struct {
	Level            *content.LevelDef
	Terrain          *content.TerrainChunkManifestDef
	POI              *content.ImportedWorldDef
	SourceTiles      map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef
	TerrainPageTiles map[string]*content.TerrainHeightTileDef
	POIPageChunks    map[string]*content.ImportedWorldChunkDef
	sourceSnapshot   *content.ImportedWorldDef
	fingerprint      string
	published        bool
}

func islandHarnessCanonicalSource(source *content.ImportedWorldDef) (*content.ImportedWorldDef, error) {
	d, err := pageClone(source)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, fmt.Errorf("nil harness source")
	}
	d.PageIndex = nil
	if _, err := content.NormalizeImportedWorldPages(d); err != nil {
		return nil, err
	}
	d.SchemaVersion = content.CurrentImportedWorldSchemaVersion
	content.EnsureImportedWorldDefaults(d)
	sort.Slice(d.Entries, func(i, j int) bool { return pageCoordLess(d.Entries[i].Coord, d.Entries[j].Coord) })
	sort.Slice(d.Sectors, func(i, j int) bool { return pageCoordLess(d.Sectors[i].Coord, d.Sectors[j].Coord) })
	for i := range d.Sectors {
		s := &d.Sectors[i]
		for _, refs := range [][]content.TerrainChunkCoordDef{s.FullChunkRefs, s.VisibleSectorRefs, s.AdjacentSectorRefs} {
			sort.Slice(refs, func(i, j int) bool { return pageCoordLess(refs[i], refs[j]) })
		}
		sort.Ints(s.SourceLeafIDs)
		sort.Strings(s.Tags)
	}
	for _, materials := range [][]content.ImportedWorldMaterialDef{d.Materials, d.SourceMaterials} {
		sort.Slice(materials, func(i, j int) bool {
			if materials[i].PaletteIndex != materials[j].PaletteIndex {
				return materials[i].PaletteIndex < materials[j].PaletteIndex
			}
			return materials[i].ID < materials[j].ID
		})
	}
	sort.Slice(d.MaterialAnimations, func(i, j int) bool { return d.MaterialAnimations[i].ID < d.MaterialAnimations[j].ID })
	return d, nil
}

func islandHarnessSourceIdentity(d *content.ImportedWorldDef) (string, error) {
	c, err := islandHarnessCanonicalSource(d)
	if err != nil {
		return "", err
	}
	return pageHash(c)
}

func islandHarnessFingerprint(b *IslandStreamHarness) (string, error) {
	if b == nil {
		return "", fmt.Errorf("nil harness")
	}
	h := sha256.New()
	for _, v := range []any{b.Level, b.Terrain, b.POI, b.sourceSnapshot} {
		if err := terrainHashMetadata(h, v); err != nil {
			return "", err
		}
	}
	coords := make([]content.TerrainChunkCoordDef, 0, len(b.SourceTiles))
	for coord := range b.SourceTiles {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool { return pageCoordLess(coords[i], coords[j]) })
	for _, coord := range coords {
		if err := terrainHashMetadata(h, coord); err != nil {
			return "", err
		}
		if err := terrainHashTile(h, b.SourceTiles[coord]); err != nil {
			return "", err
		}
	}
	paths := make([]string, 0, len(b.TerrainPageTiles))
	for path := range b.TerrainPageTiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := terrainHashMetadata(h, path); err != nil {
			return "", err
		}
		if err := terrainHashTile(h, b.TerrainPageTiles[path]); err != nil {
			return "", err
		}
	}
	paths = paths[:0]
	for path := range b.POIPageChunks {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err := terrainHashMetadata(h, []any{path, b.POIPageChunks[path]}); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func islandHarnessOptions(o IslandStreamHarnessOptions) (IslandStreamHarnessOptions, error) {
	fields := []*float32{&o.PlayableSpan, &o.CoverageSpan, &o.RootSpan, &o.MacroSpan, &o.RegionalSpan, &o.HeightTileSpan, &o.HeightSampleSpacing}
	for i, d := range []float32{15000, 16384, 2048, 512, 128, 256, 2} {
		if *fields[i] == 0 {
			*fields[i] = d
		}
		if !pageFinite(float64(*fields[i])) || *fields[i] <= 0 {
			return o, fmt.Errorf("invalid harness span")
		}
	}
	if o.MaxSourceTiles == 0 {
		o.MaxSourceTiles = 4096
	}
	if o.MaxPages == 0 {
		o.MaxPages = 32768
	}
	if o.MaxSourceTiles < 1 || o.MaxSourceTiles > 4096 || o.MaxPages < 1 || o.MaxPages > 32768 || o.PlayableSpan > o.CoverageSpan || o.CoverageSpan > 16384 || o.RootSpan/o.MacroSpan != 4 || o.MacroSpan/o.RegionalSpan != 4 || o.HeightTileSpan != 128*o.HeightSampleSpacing || o.HeightSampleSpacing != 2 {
		return o, fmt.Errorf("unsupported harness profile or budget")
	}
	for _, pair := range [][2]float32{{o.CoverageSpan, o.RootSpan}, {o.CoverageSpan, o.HeightTileSpan}, {o.RootSpan, o.HeightSampleSpacing}, {o.MacroSpan, o.HeightSampleSpacing}, {o.RegionalSpan, o.HeightSampleSpacing}} {
		q := float64(pair[0]) / float64(pair[1])
		if q < 1 || q > 32768 || q != math.Round(q) {
			return o, fmt.Errorf("unaligned harness profile")
		}
	}
	if int(o.CoverageSpan/o.RootSpan)%2 != 0 || int(o.CoverageSpan/o.HeightTileSpan)%2 != 0 || o.RegionalSpan/o.HeightSampleSpacing > 128 || o.RootSpan/128 < o.HeightSampleSpacing {
		return o, fmt.Errorf("invalid harness tile lattice")
	}
	return o, nil
}

type islandHarnessRect struct{ minX, minZ, maxX, maxZ float64 }

func (r islandHarnessRect) intersects(x, z, span float64) bool {
	return r.minX <= x+span && r.maxX >= x && r.minZ <= z+span && r.maxZ >= z
}

func islandHarnessRoutes(o IslandStreamHarnessOptions) ([]content.LevelMarkerDef, []islandHarnessRect) {
	s := o.PlayableSpan / 15000
	var markers []content.LevelMarkerDef
	var corridors []islandHarnessRect
	add := func(name string, positions []content.Vec3, speed string, extra map[int][]string, teleport bool) {
		for i, p := range positions {
			tags := []string{"route:" + name, fmt.Sprintf("order:%03d", i)}
			if speed != "" {
				tags = append(tags, "speed:"+speed)
			}
			tags = append(tags, extra[i]...)
			markers = append(markers, content.LevelMarkerDef{ID: islandHarnessRecipe + ":" + name + fmt.Sprintf(":%03d", i), Name: name, Kind: "stream_benchmark_waypoint", Transform: content.LevelTransformDef{Position: p, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}, Tags: tags})
			previous := p
			if i > 0 && !teleport {
				previous = positions[i-1]
			}
			corridors = append(corridors, islandHarnessRect{float64(min(p[0], previous[0])) - 640, float64(min(p[2], previous[2])) - 640, float64(max(p[0], previous[0])) + 640, float64(max(p[2], previous[2])) + 640})
		}
	}
	add("boot_pan", []content.Vec3{{-7000 * s, 512, 0}}, "", map[int][]string{0: {"yaw:360"}}, false)
	add("scale_crossing", []content.Vec3{{-o.PlayableSpan / 2, 2, -3000 * s}, {o.PlayableSpan / 2, 2, -3000 * s}}, "50", map[int][]string{1: {"hold:10"}}, false)
	add("regional_loop", []content.Vec3{{-2000 * s, 2, 1000 * s}, {-1000 * s, 2, 1000 * s}, {-1000 * s, 2, 2000 * s}, {-2000 * s, 2, 2000 * s}, {-2000 * s, 2, 1000 * s}}, "6", map[int][]string{4: {"hold:10"}}, false)
	add("poi_roundtrip", []content.Vec3{{-3000 * s, 2, 0}, {0, 2, 0}, {-3000 * s, 2, 0}}, "6", map[int][]string{2: {"hold:10"}}, false)
	ping := []content.Vec3{{127 * s, 2, 0}}
	for i := 0; i < 10; i++ {
		ping = append(ping, content.Vec3{385 * s, 2, 0}, content.Vec3{127 * s, 2, 0})
	}
	add("boundary_ping_pong", ping, "6", map[int][]string{len(ping) - 1: {"hold:10"}}, false)
	add("teleport_return", []content.Vec3{{-6500 * s, 2, 0}, {6500 * s, 2, 0}, {-6500 * s, 2, 0}}, "", map[int][]string{1: {"teleport:true", "destination_wait:true"}, 2: {"teleport:true", "destination_wait:true", "hold:10"}}, true)
	return markers, corridors
}

// BuildIslandStreamHarness consumes one source chunk at a time. No output files
// are written; immutable publication belongs to SaveIslandStreamHarness.
func BuildIslandStreamHarness(source *content.ImportedWorldDef, loadChunk func(content.ImportedWorldChunkEntryDef) (*content.ImportedWorldChunkDef, error), options IslandStreamHarnessOptions) (*IslandStreamHarness, error) {
	o, err := islandHarnessOptions(options)
	if err != nil {
		return nil, err
	}
	if source == nil || loadChunk == nil || source.SchemaVersion < 0 || source.SchemaVersion > 2 || strings.TrimSpace(source.WorldID) == "" || len(source.Entries) > 4096 || source.ChunkSize < 1 || source.ChunkSize > 256 || !pageFinite(float64(source.VoxelResolution)) || source.VoxelResolution <= 0 {
		return nil, fmt.Errorf("invalid harness POI source")
	}
	d, err := islandHarnessCanonicalSource(source)
	if err != nil {
		return nil, err
	}
	markers, corridors := islandHarnessRoutes(o)
	fullBounds, err := islandHarnessSourceBounds(d, o)
	if err != nil {
		return nil, err
	}
	for _, bounds := range fullBounds {
		corridors = append(corridors, islandHarnessRect{float64(bounds.Min[0]) - 640, float64(bounds.Min[2]) - 640, float64(bounds.Max[0]) + 640, float64(bounds.Max[2]) + 640})
	}
	plan, err := islandHarnessTerrainPlan(o, corridors)
	if err != nil {
		return nil, err
	}
	b := &IslandStreamHarness{SourceTiles: map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}, TerrainPageTiles: map[string]*content.TerrainHeightTileDef{}, POIPageChunks: map[string]*content.ImportedWorldChunkDef{}, sourceSnapshot: d}
	poi, geometryHash, err := islandHarnessPOI(d, loadChunk, o, fullBounds, b.POIPageChunks)
	if err != nil {
		return nil, err
	}
	b.POI = poi
	if len(plan.pages)+len(poi.Pages) > o.MaxPages {
		return nil, fmt.Errorf("harness page budget exceeded")
	}
	generation, err := pageHash([]any{islandHarnessRecipe, o, d, geometryHash})
	if err != nil {
		return nil, err
	}
	if err = islandHarnessTerrain(b, o, plan, fullBounds, generation); err != nil {
		return nil, err
	}
	poi.SourceHash = generation
	for i := range poi.Pages {
		p := &poi.Pages[i]
		if c := b.POIPageChunks[p.Payload.Path]; c != nil {
			old := p.Payload.Path
			path := "worlds/poi/pages/" + generation + "/" + old
			p.Payload.Path = path
			delete(b.POIPageChunks, old)
			b.POIPageChunks[path] = c
		}
	}
	ymin, ymax := float32(-64), float32(64)
	for _, p := range poi.Pages {
		ymin = min(ymin, p.BoundsMin[1])
		ymax = max(ymax, p.BoundsMax[1])
	}
	for _, s := range poi.IndexedSectors {
		ymin = min(ymin, s.BoundsMin[1])
		ymax = max(ymax, s.BoundsMax[1])
	}
	b.Level = &content.LevelDef{ID: "island_streaming_harness:" + generation, SchemaVersion: content.CurrentLevelSchemaVersion, Name: "Island streaming harness", ChunkSize: 128, VoxelResolution: 2, Tags: []string{"fixture_recipe:" + islandHarnessRecipe}, StreamingBounds: &content.LevelStreamingBoundsDef{BoundsMin: [3]float32{-o.CoverageSpan / 2, ymin - 1, -o.CoverageSpan / 2}, BoundsMax: [3]float32{o.CoverageSpan / 2, ymax + 1, o.CoverageSpan / 2}}, Terrain: &content.LevelTerrainDef{Kind: content.TerrainKindHeightfield, ManifestPath: "terrain/" + generation + "/island_streaming_harness.gkterrainmanifest"}, BaseWorld: &content.LevelBaseWorldDef{Kind: content.ImportedWorldKindVoxelWorld, ManifestPath: "worlds/poi/" + generation + "/island_streaming_harness.gkworld", ReadOnlyByDefault: true, CollisionEnabled: true}, Markers: markers}
	b.Level.BrushLayers = []content.LevelBrushLayerDef{{ID: islandHarnessRecipe + ":" + generation + ":empty-brush-layer", Name: content.DefaultLevelBrushLayerName}}
	b.fingerprint, err = islandHarnessFingerprint(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
