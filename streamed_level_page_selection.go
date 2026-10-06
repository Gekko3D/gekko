package gekko

import (
	"fmt"
	"math"
	"sort"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type streamedPageObserverKey struct {
	cell      [3]float64
	direction mgl32.Vec3
	visual    bool
}
type streamedPageObserverState struct {
	key    streamedPageObserverKey
	bounds content.StreamPageBounds
	// Only independently distance-selected pages seed hysteresis, never closure.
	desired        map[StreamedPageKey]struct{}
	prefetch, keep map[StreamedPageKey]struct{}
}

func streamedPageObserverInput(o StreamedPageObserver, size float32) (streamedPageObserverState, error) {
	out := streamedPageObserverState{key: streamedPageObserverKey{visual: o.Visual}}
	if o.ID == 0 {
		return out, fmt.Errorf("page observer ID is zero")
	}
	for a := 0; a < 3; a++ {
		if !streamedPageFinite(o.Position[a]) || !streamedPageFinite(o.Velocity[a]) {
			return out, fmt.Errorf("nonfinite page observer")
		}
		q := math.Floor(float64(o.Position[a]) / float64(size))
		if math.IsInf(q, 0) || q < -math.Ldexp(1, 63) || q >= math.Ldexp(1, 63) {
			return out, fmt.Errorf("page observer cell overflows")
		}
		out.key.cell[a] = q
		lower, upper := q*float64(size), (q+1)*float64(size)
		// Reject a collapsed stored cell before conservative rounding; broadening
		// an unrepresentable cell cannot restore its identity.
		if !streamedPageFinite(float32(lower)) || !streamedPageFinite(float32(upper)) || float32(upper) <= float32(lower) {
			return out, fmt.Errorf("page observer cell bounds lose precision")
		}
		roundedLower, err := streamedPageEnvelope(lower, false)
		if err != nil {
			return out, err
		}
		roundedUpper, err := streamedPageEnvelope(upper, true)
		if err != nil {
			return out, err
		}
		out.bounds.Min[a], out.bounds.Max[a] = roundedLower, roundedUpper
	}
	length := math.Hypot(math.Hypot(float64(o.Velocity[0]), float64(o.Velocity[1])), float64(o.Velocity[2]))
	if length != 0 {
		for a := 0; a < 3; a++ {
			out.key.direction[a] = float32(float64(o.Velocity[a]) / length)
		}
	}
	return out, nil
}
func streamedPageDistances(p StreamedPageProfile, level uint8) StreamedPageDistances {
	switch level {
	case content.StreamPageLevelMacro:
		return p.Macro
	case content.StreamPageLevelRegional:
		return p.Regional
	default:
		return p.FullPOI
	}
}
func streamedPageDistance2(a, b content.StreamPageBounds) float64 {
	var sum float64
	for axis := 0; axis < 3; axis++ {
		d := math.Max(0, math.Max(float64(a.Min[axis])-float64(b.Max[axis]), float64(b.Min[axis])-float64(a.Max[axis])))
		sum += d * d
	}
	return sum
}

// Round outward so finite stored-grid cell and swept/query envelopes never
// lose a boundary merely because nearest-f32 rounding moved it inward.
func streamedPageEnvelope(v float64, upper bool) (float32, error) {
	rounded := float32(v)
	if !streamedPageFinite(rounded) {
		return 0, fmt.Errorf("page bounds overflow")
	}
	if upper && float64(rounded) < v {
		rounded = math.Nextafter32(rounded, float32(math.Inf(1)))
	}
	if !upper && float64(rounded) > v {
		rounded = math.Nextafter32(rounded, float32(math.Inf(-1)))
	}
	if !streamedPageFinite(rounded) {
		return 0, fmt.Errorf("page bounds overflow")
	}
	return rounded, nil
}
func streamedPageSweep(b content.StreamPageBounds, direction mgl32.Vec3, extra float64) (content.StreamPageBounds, error) {
	for a := 0; a < 3; a++ {
		shift := float64(direction[a]) * extra
		lower, e := streamedPageEnvelope(float64(b.Min[a])+math.Min(0, shift), false)
		if e != nil {
			return b, e
		}
		upper, e := streamedPageEnvelope(float64(b.Max[a])+math.Max(0, shift), true)
		if e != nil {
			return b, e
		}
		b.Min[a], b.Max[a] = lower, upper
	}
	return b, nil
}
func streamedPageQueryBounds(b content.StreamPageBounds, distance float32) (content.StreamPageBounds, error) {
	for a := 0; a < 3; a++ {
		lower, e := streamedPageEnvelope(float64(b.Min[a])-float64(distance), false)
		if e != nil {
			return b, e
		}
		upper, e := streamedPageEnvelope(float64(b.Max[a])+float64(distance), true)
		if e != nil {
			return b, e
		}
		b.Min[a], b.Max[a] = lower, upper
	}
	return b, nil
}
func unionStreamedPageSets(into, from map[StreamedPageKey]struct{}) {
	for key := range from {
		into[key] = struct{}{}
	}
}
func streamedPageKeySetKeys(set map[StreamedPageKey]struct{}) []StreamedPageKey {
	out := make([]StreamedPageKey, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sortStreamedPageKeys(out)
	return out
}
func (c *streamedPageControl) layer(layer StreamedPageLayer) *content.LevelStreamingLayerIndex {
	if layer == StreamedPageTerrain {
		return c.index.Terrain
	}
	return c.index.POI
}
func (c *streamedPageControl) coverageClosure(raw map[StreamedPageKey]struct{}) map[StreamedPageKey]struct{} {
	out := make(map[StreamedPageKey]struct{}, len(raw))
	for key := range raw {
		out[key] = struct{}{}
		layer := c.layer(key.Layer)
		current := int(key.PageIndex)
		for parent := layer.Forest.ParentPageIndices[current]; parent >= 0; parent = layer.Forest.ParentPageIndices[parent] {
			out[streamedPageKey(key.Layer, layer, uint32(parent))] = struct{}{}
			for _, sibling := range layer.Pages[parent].ChildPageIndices {
				out[streamedPageKey(key.Layer, layer, sibling)] = struct{}{}
			}
		}
	}
	return out
}

// UpdatePageSelection atomically publishes detached, deterministic demand. It
// reuses a stationary observer's conservative cell and direction, and teleports
// consider only the destination cell, never the traveled segment.
func (state *StreamedLevelRuntimeState) UpdatePageSelection(observers []StreamedPageObserver) (StreamedPageSelection, error) {
	owner, err := state.currentPageControl()
	if err != nil {
		return StreamedPageSelection{}, err
	}
	next := make(map[EntityId]streamedPageObserverState, len(observers))
	unchanged := owner.selected && len(observers) == len(owner.observers)
	for _, o := range observers {
		if _, duplicate := next[o.ID]; duplicate {
			return StreamedPageSelection{}, fmt.Errorf("duplicate page observer")
		}
		entry, e := streamedPageObserverInput(o, owner.profile.SelectionCellSize)
		if e != nil {
			return StreamedPageSelection{}, e
		}
		next[o.ID] = entry
		previous, ok := owner.observers[o.ID]
		if !ok || entry.key != previous.key {
			unchanged = false
		}
	}
	if unchanged {
		return cloneStreamedPageSelection(owner.selection), nil
	}
	rawDesired, rawPrefetch, keep := make(map[StreamedPageKey]struct{}), make(map[StreamedPageKey]struct{}), make(map[StreamedPageKey]struct{})
	visits := owner.selection.PageCandidateVisits
	// Sorted observers avoid error or diagnostic dependence on caller ordering.
	ids := make([]EntityId, 0, len(next))
	for id := range next {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		observer := next[id]
		previous, exists := owner.observers[id]
		if exists && observer.key == previous.key {
			next[id] = previous
			unionStreamedPageSets(rawDesired, previous.desired)
			unionStreamedPageSets(rawPrefetch, previous.prefetch)
			unionStreamedPageSets(keep, previous.keep)
			continue
		}
		observer.desired = make(map[StreamedPageKey]struct{})
		observer.prefetch = make(map[StreamedPageKey]struct{})
		observer.keep = make(map[StreamedPageKey]struct{})
		if !observer.key.visual {
			next[id] = observer
			continue
		}
		for number, layer := range []*content.LevelStreamingLayerIndex{owner.index.Terrain, owner.index.POI} {
			if layer == nil {
				continue
			}
			// One indexed broadphase per layer. Exact tier distances below remove its
			// conservative overlap; roots are pinned separately, not distance filtered.
			maximum, extra := float32(0), float64(0)
			for _, d := range []StreamedPageDistances{owner.profile.Macro, owner.profile.Regional, owner.profile.FullPOI} {
				maximum = max(maximum, d.Keep, d.Desired)
				extra = max(extra, float64(d.Prefetch)-float64(d.Desired))
			}
			broad, e := streamedPageSweep(observer.bounds, observer.key.direction, extra)
			if e != nil {
				return StreamedPageSelection{}, e
			}
			broad, e = streamedPageQueryBounds(broad, maximum)
			if e != nil {
				return StreamedPageSelection{}, e
			}
			candidates, e := layer.PageIndex.Query(broad.Min, broad.Max)
			if e != nil {
				return StreamedPageSelection{}, e
			}
			for _, index := range candidates {
				visits++
				key := streamedPageKey(StreamedPageLayer(number), layer, index)
				if layer.Forest.ParentPageIndices[index] < 0 {
					continue
				}
				page := layer.Pages[index]
				distance := streamedPageDistances(owner.profile, page.Level)
				bounds := content.StreamPageBounds{Min: page.BoundsMin, Max: page.BoundsMax}
				squared := streamedPageDistance2(observer.bounds, bounds)
				insideKeep := squared <= float64(distance.Keep)*float64(distance.Keep)
				if insideKeep {
					observer.keep[key] = struct{}{}
				}
				_, wasDesired := previous.desired[key]
				if squared <= float64(distance.Desired)*float64(distance.Desired) || (wasDesired && insideKeep) {
					observer.desired[key] = struct{}{}
					continue
				}
				if observer.key.direction != (mgl32.Vec3{}) && distance.Prefetch > distance.Desired {
					swept, e := streamedPageSweep(observer.bounds, observer.key.direction, float64(distance.Prefetch)-float64(distance.Desired))
					if e != nil {
						return StreamedPageSelection{}, e
					}
					if streamedPageDistance2(swept, bounds) <= float64(distance.Desired)*float64(distance.Desired) {
						observer.prefetch[key] = struct{}{}
					}
				}
			}
		}
		unionStreamedPageSets(rawDesired, observer.desired)
		unionStreamedPageSets(rawPrefetch, observer.prefetch)
		unionStreamedPageSets(keep, observer.keep)
		next[id] = observer
	}
	desired := owner.coverageClosure(rawDesired)
	prefetch := owner.coverageClosure(rawPrefetch)
	for _, root := range owner.roots {
		desired[root] = struct{}{}
	}
	for key := range desired {
		delete(prefetch, key)
		keep[key] = struct{}{}
	}
	for key := range prefetch {
		keep[key] = struct{}{}
	}
	result := StreamedPageSelection{Desired: streamedPageKeySetKeys(desired), Keep: streamedPageKeySetKeys(keep), Prefetch: streamedPageKeySetKeys(prefetch), PinnedRoots: append([]StreamedPageKey(nil), owner.roots...), BuildCount: owner.selection.BuildCount + 1, PageCandidateVisits: visits}
	rootSet := make(map[StreamedPageKey]struct{}, len(owner.roots))
	for _, root := range owner.roots {
		rootSet[root] = struct{}{}
	}
	for _, key := range result.Desired {
		priority := StreamedVoxelPriorityVisible
		if _, root := rootSet[key]; root {
			priority = StreamedVoxelPriorityFallback
		}
		result.Requests = append(result.Requests, StreamedPageRequest{Key: key, Priority: priority})
	}
	for _, key := range result.Prefetch {
		result.Requests = append(result.Requests, StreamedPageRequest{Key: key, Priority: StreamedVoxelPriorityPrefetch})
	}
	sort.Slice(result.Requests, func(i, j int) bool {
		a, b := result.Requests[i], result.Requests[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return streamedPageKeyLess(a.Key, b.Key)
	})
	owner.observers, owner.selection, owner.selected = next, result, true
	return cloneStreamedPageSelection(result), nil
}
