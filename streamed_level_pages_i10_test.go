package gekko

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// Metadata-only fixtures deliberately reference nonexistent files: configuring
// selection must not decode payloads or publish entities.
func i10PageFixture() (*content.LevelDef, *content.TerrainChunkManifestDef, *content.ImportedWorldDef) {
	hash := strings.Repeat("a", 64)
	terrain := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "terrain-i10", SourceHash: hash, ChunkSize: 128, VoxelResolution: 2, RootPageIndices: []uint32{0}, Pages: []content.StreamPageDef{{Level: content.StreamPageLevelRoot, BoundsMin: [3]float32{0, 0, 0}, BoundsMax: [3]float32{2, 2, 2}, Payload: content.StreamPagePayloadDef{Kind: content.TerrainHeightTilePayloadKind, Path: "absent-height", ChunkSize: 1, SampleSpacing: 2, VoxelResolution: 2, HeightScale: 2, PayloadHash: hash, PayloadSizeBytes: 2}}}}
	poi := &content.ImportedWorldDef{SchemaVersion: 3, WorldID: "poi-i10", SourceHash: hash, ChunkSize: 4, VoxelResolution: .1, Pages: []content.StreamPageDef{i10VoxelPage(content.StreamPageLevelRoot, 0, 0, 0)}, RootPageIndices: []uint32{0}}
	return &content.LevelDef{}, terrain, poi
}
func i10VoxelPage(level uint8, x, y, z float32) content.StreamPageDef {
	return content.StreamPageDef{Level: level, BoundsMin: [3]float32{x, y, z}, BoundsMax: [3]float32{x + 1, y + 1, z + 1}, Payload: content.StreamPagePayloadDef{Kind: content.ImportedWorldChunkPayloadDenseRLEBinaryV1, Path: fmt.Sprintf("absent-%d-%g-%g-%g", level, x, y, z), WorldOrigin: [3]float32{x, y, z}, ChunkSize: 1, VoxelResolution: 1, PayloadHash: strings.Repeat("b", 64), PayloadSizeBytes: 8}, Tags: []string{"owned"}}
}
func i10Configure(t *testing.T, terrain *content.TerrainChunkManifestDef, poi *content.ImportedWorldDef, profile StreamedPageProfile) *StreamedLevelRuntimeState {
	t.Helper()
	s := &StreamedLevelRuntimeState{Generation: 7}
	if err := s.ConfigurePageControlPlane(&content.LevelDef{}, terrain, poi, profile); err != nil {
		t.Fatal(err)
	}
	return s
}
func i10Profile() StreamedPageProfile {
	return StreamedPageProfile{Macro: StreamedPageDistances{Desired: 1, Keep: 2, Prefetch: 1}, Regional: StreamedPageDistances{Desired: 1, Keep: 2, Prefetch: 1}, FullPOI: StreamedPageDistances{Desired: 10, Keep: 20, Prefetch: 30}, SelectionCellSize: 1}
}

// Disjoint roots avoid sibling coverage obscuring each independent distance test.
func i10Branches(points ...mgl32.Vec3) (*content.ImportedWorldDef, []uint32) {
	_, _, d := i10PageFixture()
	d.Pages = nil
	d.RootPageIndices = nil
	var leaves []uint32
	for _, p := range points {
		base := uint32(len(d.Pages))
		d.RootPageIndices = append(d.RootPageIndices, base)
		for level := content.StreamPageLevelRoot; ; level-- {
			page := i10VoxelPage(level, p[0], p[1], p[2])
			if level > 0 {
				page.ChildPageIndices = []uint32{uint32(len(d.Pages) + 1)}
			}
			d.Pages = append(d.Pages, page)
			if level == 0 {
				break
			}
		}
		leaves = append(leaves, base+3)
	}
	return d, leaves
}
func i10Observer(id EntityId, x, y, z float32) StreamedPageObserver {
	return StreamedPageObserver{ID: id, Position: mgl32.Vec3{x, y, z}, Visual: true}
}
func i10Select(t *testing.T, s *StreamedLevelRuntimeState, obs ...StreamedPageObserver) StreamedPageSelection {
	t.Helper()
	out, e := s.UpdatePageSelection(obs)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func i10Contains(keys []StreamedPageKey, layer StreamedPageLayer, index uint32) bool {
	for _, k := range keys {
		if k.Layer == layer && k.PageIndex == index {
			return true
		}
	}
	return false
}
func i10Want(t *testing.T, keys []StreamedPageKey, layer StreamedPageLayer, index uint32, want bool) {
	t.Helper()
	if got := i10Contains(keys, layer, index); got != want {
		t.Fatalf("page %d/%d membership=%v want %v: %+v", layer, index, got, want, keys)
	}
}
func i10RequestPriority(t *testing.T, out StreamedPageSelection, key StreamedPageKey, want StreamedVoxelPriority) {
	t.Helper()
	for _, r := range out.Requests {
		if r.Key == key {
			if r.Priority != want {
				t.Fatalf("priority=%v want %v", r.Priority, want)
			}
			return
		}
	}
	t.Fatalf("missing request %+v", key)
}

func TestI10PageReferenceProfileAndIndependentPinnedRoots(t *testing.T) {
	want := StreamedPageProfile{Macro: StreamedPageDistances{6144, 7168, 8192}, Regional: StreamedPageDistances{1536, 2048, 2560}, FullPOI: StreamedPageDistances{384, 512, 384}, SelectionCellSize: 32}
	if got := IslandReferenceStreamedPageProfile(); got != want {
		t.Fatalf("profile=%+v", got)
	}
	level, terrain, poi := i10PageFixture()
	s := &StreamedLevelRuntimeState{Generation: 7}
	if e := s.ConfigurePageControlPlane(level, terrain, poi, StreamedPageProfile{}); e != nil {
		t.Fatal(e)
	}
	out := i10Select(t, s)
	if len(out.PinnedRoots) != 2 || len(out.Desired) != 2 || len(out.Keep) != 2 || len(out.Prefetch) != 0 || len(out.Requests) != 2 {
		t.Fatalf("pinned roots: %+v", out)
	}
	for _, k := range out.PinnedRoots {
		if k.PageIndex != 0 || k.SourceHash != terrain.SourceHash {
			t.Fatalf("identity: %+v", k)
		}
		i10RequestPriority(t, out, k, StreamedVoxelPriorityFallback)
	}
	if out.PinnedRoots[0].Layer != StreamedPageTerrain || out.PinnedRoots[0].OwnerID != terrain.TerrainID || out.PinnedRoots[1].Layer != StreamedPagePOI || out.PinnedRoots[1].OwnerID != poi.WorldID {
		t.Fatal("layer identities collided")
	}
	far := i10Select(t, s, i10Observer(1, 100000, 100000, 100000))
	if !reflect.DeepEqual(out.PinnedRoots, far.PinnedRoots) {
		t.Fatal("distant observer unpinned roots")
	}
}

func TestI10PageClosedCellDistanceSignedAndTierThresholds(t *testing.T) {
	// [0,1] selection cell reaches x=11 at closed distance10, but
	// diagonal (11,11) and y=12 lie beyond Euclidean/vertical distance10.
	d, leaf := i10Branches(mgl32.Vec3{11, 0, 0}, mgl32.Vec3{11.01, 0, 0}, mgl32.Vec3{11, 11, 0}, mgl32.Vec3{0, 12, 0}, mgl32.Vec3{-12, 0, 0})
	s := i10Configure(t, nil, d, i10Profile())
	out := i10Select(t, s, i10Observer(1, .25, .25, .25))
	for i, want := range []bool{true, false, false, false, false} {
		i10Want(t, out.Desired, StreamedPagePOI, leaf[i], want)
	}
	neg := i10Select(t, i10Configure(t, nil, d, i10Profile()), i10Observer(1, -.25, .25, .25))
	i10Want(t, neg.Desired, StreamedPagePOI, leaf[4], true)
	for _, tier := range []uint8{content.StreamPageLevelMacro, content.StreamPageLevelRegional, content.StreamPageLevelLeaf} {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			_, _, p := i10PageFixture()
			p.Pages = []content.StreamPageDef{i10VoxelPage(content.StreamPageLevelRoot, 0, 0, 0), i10VoxelPage(tier, 6, 0, 0)}
			p.Pages[0].BoundsMax[0] = 7
			p.Pages[0].ChildPageIndices = []uint32{1}
			profile := i10Profile()
			profile.Macro = StreamedPageDistances{5, 6, 5}
			profile.Regional = StreamedPageDistances{5, 6, 5}
			profile.FullPOI = StreamedPageDistances{5, 6, 5}
			owner := i10Configure(t, nil, p, profile)
			i10Want(t, i10Select(t, owner, i10Observer(1, .2, 0, 0)).Desired, StreamedPagePOI, 1, true)
			owner = i10Configure(t, nil, p, profile)
			i10Want(t, i10Select(t, owner, i10Observer(1, -.2, 0, 0)).Desired, StreamedPagePOI, 1, false)
		})
	}
}

func TestI10PageHysteresisPrefetchAndObserverRemoval(t *testing.T) {
	d, leaf := i10Branches(mgl32.Vec3{11, 0, 0}, mgl32.Vec3{25, 0, 0}, mgl32.Vec3{-26, 0, 0}, mgl32.Vec3{0, 0, 25})
	s := i10Configure(t, nil, d, i10Profile())
	obs := i10Observer(1, .2, .2, .2)
	obs.Velocity = mgl32.Vec3{1, 0, 0}
	out := i10Select(t, s, obs)
	i10Want(t, out.Desired, StreamedPagePOI, leaf[0], true)
	i10Want(t, out.Prefetch, StreamedPagePOI, leaf[1], true)
	i10Want(t, out.Prefetch, StreamedPagePOI, leaf[2], false)
	i10Want(t, out.Prefetch, StreamedPagePOI, leaf[3], false)
	obs.Position[0] = -5.2
	obs.Velocity = mgl32.Vec3{}
	out = i10Select(t, s, obs)
	i10Want(t, out.Desired, StreamedPagePOI, leaf[0], true)
	obs.Position[0] = -11.2
	out = i10Select(t, s, obs)
	i10Want(t, out.Desired, StreamedPagePOI, leaf[0], false)
	// A formerly prefetched leaf enters Keep without entering Desired.
	s = i10Configure(t, nil, d, i10Profile())
	obs = i10Observer(1, .2, .2, .2)
	obs.Velocity = mgl32.Vec3{1, 0, 0}
	i10Select(t, s, obs)
	obs.Position[0] = 5.2
	obs.Velocity = mgl32.Vec3{}
	out = i10Select(t, s, obs)
	i10Want(t, out.Keep, StreamedPagePOI, leaf[1], true)
	i10Want(t, out.Desired, StreamedPagePOI, leaf[1], false)
	for _, r := range out.Requests {
		if r.Key.PageIndex == leaf[1] {
			t.Fatal("keep-only page requested preparation")
		}
	}
	out = i10Select(t, s)
	if len(out.Desired) != len(out.PinnedRoots) {
		t.Fatal("removed observer retained hysteresis")
	}
}

func TestI10PageCacheVelocityTeleportAndMultiObserver(t *testing.T) {
	d, leaf := i10Branches(mgl32.Vec3{25, 0, 0}, mgl32.Vec3{-26, 0, 0}, mgl32.Vec3{500, 0, 0}, mgl32.Vec3{1005, 0, 0})
	s := i10Configure(t, nil, d, i10Profile())
	a := i10Observer(1, .1, .1, .1)
	a.Velocity = mgl32.Vec3{1, 0, 0}
	b := i10Observer(2, -.1, .1, .1)
	first := i10Select(t, s, a, b)
	a.Position = mgl32.Vec3{.9, .9, .9}
	a.Velocity = mgl32.Vec3{9, 0, 0}
	same := i10Select(t, s, b, a)
	if !reflect.DeepEqual(first, same) {
		t.Fatal("unchanged cells/directions or observer order rebuilt selection")
	}
	a.Velocity = mgl32.Vec3{-1, 0, 0}
	changed := i10Select(t, s, a, b)
	if changed.BuildCount != first.BuildCount+1 || changed.PageCandidateVisits < first.PageCandidateVisits {
		t.Fatal("direction change did not rebuild")
	}
	i10Want(t, changed.Prefetch, StreamedPagePOI, leaf[1], true)
	a.Visual = false
	changed = i10Select(t, s, a)
	if len(changed.Desired) != len(changed.PinnedRoots) || len(changed.Prefetch) != 0 {
		t.Fatal("nonvisual observer requested detail")
	}
	a.Visual = true
	a.Position[0] = 1000
	changed = i10Select(t, s, a)
	i10Want(t, changed.Desired, StreamedPagePOI, leaf[3], true)
	i10Want(t, changed.Keep, StreamedPagePOI, leaf[2], false)
	near := i10Observer(3, 24, .1, .1)
	out := i10Select(t, s, near, b)
	i10Want(t, out.Desired, StreamedPagePOI, leaf[0], true)
	out = i10Select(t, s, b)
	i10Want(t, out.Desired, StreamedPagePOI, leaf[0], false)
}

func TestI10PageCoverageClosureDoesNotRecursivelyRefineOrSeedHysteresis(t *testing.T) {
	_, _, d := i10PageFixture()
	// Root0 -> Macro1/2; Macro1 -> Regional3/4; Regional3 -> Leaf5/6.
	// Macro2's Regional7 must not be recursively selected by sibling closure.
	d.Pages = []content.StreamPageDef{
		i10VoxelPage(3, 0, 0, 0), i10VoxelPage(2, 0, 0, 0), i10VoxelPage(2, 100, 0, 0),
		i10VoxelPage(1, 0, 0, 0), i10VoxelPage(1, 50, 0, 0), i10VoxelPage(0, 0, 0, 0), i10VoxelPage(0, 20, 0, 0), i10VoxelPage(1, 100, 0, 0),
	}
	d.Pages[0].BoundsMax[0] = 101
	d.Pages[0].ChildPageIndices = []uint32{1, 2}
	d.Pages[1].BoundsMax[0] = 51
	d.Pages[1].ChildPageIndices = []uint32{3, 4}
	d.Pages[2].ChildPageIndices = []uint32{7}
	d.Pages[3].BoundsMax[0] = 21
	d.Pages[3].ChildPageIndices = []uint32{5, 6}
	profile := i10Profile()
	profile.Macro = StreamedPageDistances{1, 2, 1}
	profile.Regional = StreamedPageDistances{1, 2, 1}
	profile.FullPOI = StreamedPageDistances{1, 30, 1}
	prefetchProfile := profile
	prefetchProfile.FullPOI.Prefetch = 30
	prefetchOwner := i10Configure(t, nil, d, prefetchProfile)
	forward := i10Observer(2, -10.2, .2, .2)
	forward.Velocity = mgl32.Vec3{1, 0, 0}
	prefetched := i10Select(t, prefetchOwner, forward)
	for _, idx := range []uint32{1, 2, 3, 4, 5, 6} {
		i10Want(t, prefetched.Prefetch, StreamedPagePOI, idx, true)
		i10Want(t, prefetched.Desired, StreamedPagePOI, idx, false)
	}
	i10Want(t, prefetched.Prefetch, StreamedPagePOI, 7, false)
	s := i10Configure(t, nil, d, profile)
	out := i10Select(t, s, i10Observer(1, .2, .2, .2))
	for _, idx := range []uint32{0, 1, 2, 3, 4, 5, 6} {
		i10Want(t, out.Desired, StreamedPagePOI, idx, true)
	}
	i10Want(t, out.Desired, StreamedPagePOI, 7, false)
	// Leaf6 was only a coverage sibling. Moving near it, within Keep but
	// outside Desired, must not turn that coverage expansion into hysteresis.
	out = i10Select(t, s, i10Observer(1, 40.2, .2, .2))
	i10Want(t, out.Desired, StreamedPagePOI, 6, false)
	i10Want(t, out.Keep, StreamedPagePOI, 6, true)
}

func TestI10PageRequestsDeterministicStrongestPriorityAndDetachedOutputs(t *testing.T) {
	d, leaf := i10Branches(mgl32.Vec3{25, 0, 0}, mgl32.Vec3{-26, 0, 0})
	s := i10Configure(t, nil, d, i10Profile())
	a := i10Observer(2, .2, .2, .2)
	a.Velocity = mgl32.Vec3{1, 0, 0}
	b := i10Observer(1, 24, .2, .2)
	out := i10Select(t, s, a, b)
	key := StreamedPageKey{Layer: StreamedPagePOI, OwnerID: d.WorldID, SourceHash: d.SourceHash, PageIndex: leaf[0]}
	i10RequestPriority(t, out, key, StreamedVoxelPriorityVisible)
	i10Want(t, out.Prefetch, StreamedPagePOI, leaf[0], false)
	for i, r := range out.Requests {
		if i == 0 {
			continue
		}
		prev := out.Requests[i-1]
		if prev.Priority > r.Priority || (prev.Priority == r.Priority && (prev.Key.Layer > r.Key.Layer || (prev.Key.Layer == r.Key.Layer && prev.Key.PageIndex >= r.Key.PageIndex))) {
			t.Fatal("requests unordered or duplicate")
		}
	}
	// Every key list is independently sorted; mutate all exported slices and
	// authored metadata, then an unchanged observer update must return the seal.
	for _, keys := range [][]StreamedPageKey{out.Desired, out.Keep, out.Prefetch, out.PinnedRoots} {
		for i := 1; i < len(keys); i++ {
			if keys[i-1].Layer > keys[i].Layer || (keys[i-1].Layer == keys[i].Layer && keys[i-1].PageIndex >= keys[i].PageIndex) {
				t.Fatal("keys not stable sorted")
			}
		}
	}
	expected := i10Select(t, s, b, a)
	for _, keys := range [][]StreamedPageKey{out.Desired, out.Keep, out.Prefetch, out.PinnedRoots} {
		if len(keys) > 0 {
			keys[0].OwnerID = "changed"
			keys[0].PageIndex = 999
		}
	}
	if len(out.Requests) > 0 {
		out.Requests[0].Key.PageIndex = 999
	}
	d.WorldID = "changed"
	d.Pages[0].BoundsMax[0] = 999
	d.Pages[0].Tags[0] = "changed"
	d.RootPageIndices[0] = 999
	if got := i10Select(t, s, a, b); !reflect.DeepEqual(got, expected) {
		t.Fatal("selection aliases input/output memory")
	}
}

func TestI10PageConfigurationAndObserverErrorsAreAtomic(t *testing.T) {
	level, terrain, poi := i10PageFixture()
	s := i10Configure(t, terrain, poi, i10Profile())
	obs := i10Observer(1, .2, .2, .2)
	before := i10Select(t, s, obs)
	badProfiles := []StreamedPageProfile{i10Profile(), i10Profile(), i10Profile(), i10Profile(), {SelectionCellSize: 1}}
	badProfiles[0].SelectionCellSize = 0
	badProfiles[1].Macro.Keep = .5
	badProfiles[2].Regional.Prefetch = .5
	badProfiles[3].FullPOI.Desired = float32(math.NaN())
	for _, p := range badProfiles {
		if e := s.ConfigurePageControlPlane(level, terrain, poi, p); e == nil {
			t.Fatal("accepted invalid profile")
		}
		if got := i10Select(t, s, obs); !reflect.DeepEqual(got, before) {
			t.Fatal("failed configuration replaced selection")
		}
	}
	cases := []struct {
		name    string
		level   *content.LevelDef
		terrain *content.TerrainChunkManifestDef
		poi     *content.ImportedWorldDef
	}{
		{"nil-level", nil, terrain, poi}, {"no-layers", level, nil, nil},
		{"legacy-poi", level, terrain, &content.ImportedWorldDef{SchemaVersion: 2, WorldID: "legacy", Kind: content.ImportedWorldKindVoxelWorld, ChunkSize: 4, VoxelResolution: 1}},
		{"source-only", level, &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "source", SourceHash: "legacy-id", ChunkSize: 128, VoxelResolution: 2}, nil},
		{"missing-required-terrain", &content.LevelDef{Terrain: &content.LevelTerrainDef{ManifestPath: "required"}}, nil, poi},
		{"missing-required-poi", &content.LevelDef{BaseWorld: &content.LevelBaseWorldDef{ManifestPath: "required"}}, terrain, nil},
	}
	if _, e := content.BuildLevelStreamingIndex(level, nil, cases[2].poi); e != nil {
		t.Fatalf("legacy negative fixture must be valid metadata: %v", e)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if e := s.ConfigurePageControlPlane(c.level, c.terrain, c.poi, i10Profile()); e == nil {
				t.Fatal("accepted incomplete configuration")
			}
			if got := i10Select(t, s, obs); !reflect.DeepEqual(got, before) {
				t.Fatal("failed configuration changed owner")
			}
		})
	}
	invalid := [][]StreamedPageObserver{{i10Observer(0, 0, 0, 0)}, {obs, obs}, {i10Observer(2, float32(math.NaN()), 0, 0)}, {i10Observer(2, float32(math.Inf(1)), 0, 0)}, {i10Observer(2, math.MaxFloat32, 0, 0)}}
	v := obs
	v.Velocity[2] = float32(math.Inf(-1))
	invalid = append(invalid, []StreamedPageObserver{v})
	for _, input := range invalid {
		if _, e := s.UpdatePageSelection(input); e == nil {
			t.Fatal("accepted unsafe observer")
		}
		if got := i10Select(t, s, obs); !reflect.DeepEqual(got, before) {
			t.Fatal("invalid input partially published/cache-counted")
		}
	}
	tiny := i10Profile()
	tiny.SelectionCellSize = math.SmallestNonzeroFloat32
	small := i10Configure(t, nil, poi, tiny)
	if _, e := small.UpdatePageSelection([]StreamedPageObserver{i10Observer(1, 1, 0, 0)}); e == nil {
		t.Fatal("unsafe position/cell quotient accepted")
	}
	zero := &StreamedLevelRuntimeState{}
	if e := zero.ConfigurePageControlPlane(level, terrain, poi, i10Profile()); e == nil {
		t.Fatal("zero generation configured")
	}
	s.Generation++
	if _, e := s.UpdatePageSelection([]StreamedPageObserver{obs}); e == nil {
		t.Fatal("stale configured generation selected")
	}
	s.ResetPageControlPlane()
	if _, e := s.UpdatePageSelection(nil); e == nil {
		t.Fatal("reset owner selected")
	}
}

func TestI10PageTerrainDetailUsesIndependentWorldBounds(t *testing.T) {
	level, terrain, poi := i10PageFixture()
	// Terrain 2m samples use world-space bounds, independent of the POI .1m
	// grid and the backing entry's unrelated signed coordinate.
	terrain.Entries = []content.TerrainChunkEntryDef{{Coord: content.TerrainChunkCoordDef{X: -10}, WorldOrigin: [3]float32{-2560, 0, 0}, TerrainID: terrain.TerrainID, SourceHash: terrain.SourceHash, ChunkSize: 128, VoxelResolution: 2, ChunkPath: "absent-source", PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 32768, HeightScale: 2}}
	terrain.Pages = nil
	terrain.RootPageIndices = nil
	for _, x := range []float32{6, 26} {
		base := uint32(len(terrain.Pages))
		terrain.RootPageIndices = append(terrain.RootPageIndices, base)
		for _, tier := range []uint8{content.StreamPageLevelRoot, content.StreamPageLevelMacro, content.StreamPageLevelRegional} {
			p := content.StreamPageDef{Level: tier, BoundsMin: [3]float32{x, 0, 0}, BoundsMax: [3]float32{x + 2, 2, 2}, Payload: content.StreamPagePayloadDef{Kind: content.TerrainHeightTilePayloadKind, Path: fmt.Sprintf("absent-terrain-%g-%d", x, tier), WorldOrigin: [3]float32{x, 0, 0}, ChunkSize: 1, SampleSpacing: 2, VoxelResolution: 1, HeightScale: 2, PayloadHash: strings.Repeat("a", 64), PayloadSizeBytes: 2}}
			if tier > content.StreamPageLevelRegional {
				p.ChildPageIndices = []uint32{uint32(len(terrain.Pages) + 1)}
			}
			terrain.Pages = append(terrain.Pages, p)
		}
	}
	if _, e := content.BuildLevelStreamingIndex(level, terrain, poi); e != nil {
		t.Fatalf("terrain detail fixture: %v", e)
	}
	profile := i10Profile()
	profile.Macro = StreamedPageDistances{1, 2, 1}
	profile.Regional = StreamedPageDistances{5, 8, 30}
	s := i10Configure(t, terrain, poi, profile)
	obs := i10Observer(1, .2, .2, .2)
	out := i10Select(t, s, obs)
	i10Want(t, out.Desired, StreamedPageTerrain, 2, true) // [0,1] -> x6: closed5m.
	i10Want(t, out.Desired, StreamedPageTerrain, 1, true) // demanded ancestor.
	i10Want(t, out.Desired, StreamedPageTerrain, 5, false)
	obs.Velocity = mgl32.Vec3{1, 0, 0}
	out = i10Select(t, s, obs)
	i10Want(t, out.Prefetch, StreamedPageTerrain, 5, true)
	i10Want(t, out.Prefetch, StreamedPageTerrain, 4, true)
	i10Want(t, out.Prefetch, StreamedPageTerrain, 3, false) // root stays fallback desired.
	obs.Velocity = mgl32.Vec3{-1, 0, 0}
	out = i10Select(t, s, obs)
	i10Want(t, out.Prefetch, StreamedPageTerrain, 5, false)
}
