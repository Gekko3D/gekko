package gekko

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// Page identity is manifest-local and independent of the legacy shared grid.
type StreamedPageLayer uint8

const (
	StreamedPageTerrain StreamedPageLayer = iota
	StreamedPagePOI
)

type StreamedPageKey struct {
	Layer               StreamedPageLayer
	OwnerID, SourceHash string
	PageIndex           uint32
}
type StreamedPageDistances struct{ Desired, Keep, Prefetch float32 }
type StreamedPageProfile struct {
	Macro, Regional, FullPOI StreamedPageDistances
	SelectionCellSize        float32
}

func IslandReferenceStreamedPageProfile() StreamedPageProfile {
	return StreamedPageProfile{Macro: StreamedPageDistances{6144, 7168, 8192}, Regional: StreamedPageDistances{1536, 2048, 2560}, FullPOI: StreamedPageDistances{384, 512, 384}, SelectionCellSize: 32}
}

type StreamedPageObserver struct {
	ID                 EntityId
	Position, Velocity mgl32.Vec3
	Visual             bool
}
type StreamedPageRequest struct {
	Key      StreamedPageKey
	Priority StreamedVoxelPriority
}
type StreamedPageSelection struct {
	Desired, Keep, Prefetch, PinnedRoots []StreamedPageKey
	Requests                             []StreamedPageRequest
	BuildCount, PageCandidateVisits      uint64
}

// One main-thread owner holds detached metadata, observer history and gate
// evidence. Selection is intent only; it never changes render visibility.
type streamedPageControl struct {
	generation uint64
	index      *content.LevelStreamingIndex
	profile    StreamedPageProfile
	roots      []StreamedPageKey
	pages      map[StreamedPageKey]content.StreamPageDef
	startup    streamedPageGate
	handoff    streamedPageHandoffOwner
	observers  map[EntityId]streamedPageObserverState
	selection  StreamedPageSelection
	selected   bool
}

func streamedPageFinite(v float32) bool { return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0) }
func validateStreamedPageProfile(p StreamedPageProfile) error {
	if !streamedPageFinite(p.SelectionCellSize) || p.SelectionCellSize <= 0 {
		return fmt.Errorf("invalid page selection cell size")
	}
	for _, d := range []StreamedPageDistances{p.Macro, p.Regional, p.FullPOI} {
		if !streamedPageFinite(d.Desired) || !streamedPageFinite(d.Keep) || !streamedPageFinite(d.Prefetch) || d.Desired <= 0 || d.Keep < d.Desired || d.Prefetch < d.Desired {
			return fmt.Errorf("invalid page distances")
		}
	}
	return nil
}
func (state *StreamedLevelRuntimeState) currentPageControl() (*streamedPageControl, error) {
	if state == nil || state.pageControl == nil || state.Generation == 0 || state.Generation != state.pageControl.generation {
		return nil, fmt.Errorf("page control plane is absent or stale")
	}
	return state.pageControl, nil
}
func (state *StreamedLevelRuntimeState) ConfigurePageControlPlane(level *content.LevelDef, terrain *content.TerrainChunkManifestDef, poi *content.ImportedWorldDef, profile StreamedPageProfile) error {
	if state == nil || state.Generation == 0 || level == nil {
		return fmt.Errorf("page control requires level and nonzero generation")
	}
	if level.Terrain != nil && (level.Terrain.ManifestPath != "" || level.Terrain.SourcePath != "") && terrain == nil || level.BaseWorld != nil && level.BaseWorld.ManifestPath != "" && poi == nil {
		return fmt.Errorf("required page layer metadata is missing")
	}
	if terrain == nil && poi == nil {
		return fmt.Errorf("page control requires a visual layer")
	}
	if terrain != nil && (terrain.SchemaVersion != 3 || len(terrain.Pages) == 0 || len(terrain.RootPageIndices) == 0) || poi != nil && (poi.SchemaVersion != 3 || len(poi.Pages) == 0 || len(poi.RootPageIndices) == 0) {
		return fmt.Errorf("page control requires explicit v3 visual forests")
	}
	if profile == (StreamedPageProfile{}) {
		profile = IslandReferenceStreamedPageProfile()
	}
	if err := validateStreamedPageProfile(profile); err != nil {
		return err
	}
	index, err := content.BuildLevelStreamingIndex(level, terrain, poi)
	if err != nil {
		return err
	}
	owner := &streamedPageControl{generation: state.Generation, index: index, profile: profile, pages: make(map[StreamedPageKey]content.StreamPageDef)}
	for layer, metadata := range []*content.LevelStreamingLayerIndex{index.Terrain, index.POI} {
		if metadata == nil {
			continue
		}
		for i, p := range metadata.Pages {
			owner.pages[streamedPageKey(StreamedPageLayer(layer), metadata, uint32(i))] = p
		}
		for _, i := range metadata.RootPageIndices {
			owner.roots = append(owner.roots, streamedPageKey(StreamedPageLayer(layer), metadata, i))
		}
	}
	sortStreamedPageKeys(owner.roots)
	owner.handoff.groups = owner.handoffGroups()
	state.pageControl = owner
	return nil
}
func (state *StreamedLevelRuntimeState) ResetPageControlPlane() {
	if state != nil {
		state.pageControl = nil
	}
}
func streamedPageKey(layer StreamedPageLayer, index *content.LevelStreamingLayerIndex, page uint32) StreamedPageKey {
	return StreamedPageKey{Layer: layer, OwnerID: index.OwnerID, SourceHash: index.SourceHash, PageIndex: page}
}
func streamedPageKeyLess(a, b StreamedPageKey) bool {
	if a.Layer != b.Layer {
		return a.Layer < b.Layer
	}
	return a.PageIndex < b.PageIndex
}
func sortStreamedPageKeys(keys []StreamedPageKey) {
	sort.Slice(keys, func(i, j int) bool { return streamedPageKeyLess(keys[i], keys[j]) })
}
func cloneStreamedPageSelection(s StreamedPageSelection) StreamedPageSelection {
	s.Desired = append([]StreamedPageKey(nil), s.Desired...)
	s.Keep = append([]StreamedPageKey(nil), s.Keep...)
	s.Prefetch = append([]StreamedPageKey(nil), s.Prefetch...)
	s.PinnedRoots = append([]StreamedPageKey(nil), s.PinnedRoots...)
	s.Requests = append([]StreamedPageRequest(nil), s.Requests...)
	return s
}
