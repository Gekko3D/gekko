package volume

import "slices"

// ManagedTopologyView is an immutable historical set of allocated sector
// coordinates. It retains only private coordinate index nodes, not geometry or
// its owner. Coordinates use signed X, then Y, then Z lexicographic order.
// A zero view is empty; captured views remain valid after edits or exposure.
type ManagedTopologyView struct {
	root *managedTopologyNode
}

// CaptureTopology captures sealed current sector membership in constant time
// without allocation. Nil or exposed owners return an empty view and false.
// The owner requires exclusive access during capture; the resulting immutable
// view may be read independently, including while its owner is later edited.
// This captures coordinates only, without freezing sector content.
func (m *ManagedXBrickMap) CaptureTopology() (ManagedTopologyView, bool) {
	if m == nil || m.base == nil {
		return ManagedTopologyView{}, false
	}
	return ManagedTopologyView{root: m.topology}, true
}

// Len returns the captured sector count in constant time without allocation.
func (v ManagedTopologyView) Len() int {
	return managedIndexSize(v.root)
}

// Coord reads a captured coordinate by rank in logarithmic time without
// allocation. Negative and out-of-range indices return a zero coordinate and
// false, including for the zero view.
func (v ManagedTopologyView) Coord(index int) ([3]int, bool) {
	if node := managedIndexAt(v.root, index); node != nil {
		return node.coord, true
	}
	return [3]int{}, false
}

// The coordinate-only specialization contains no geometry references.
type managedTopologyNode = managedIndexNode[struct{}]

// Constructor seeding uses current map keys, including allocated empty sectors,
// independently of payload occupancy, original base keys and tombstones.
func (m *ManagedXBrickMap) seedTopology() {
	keys := make([][3]int, 0, len(m.current.Sectors))
	for key := range m.current.Sectors {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareManagedTopologyCoords)
	m.topology = buildManagedIndex(keys, func([3]int) struct{} { return struct{}{} })
}

// Reconcile only the dense edit's target key. Halo invalidation and content
// changes do not alter membership or allocate coordinate index nodes.
func (m *ManagedXBrickMap) reconcileTopology(key [3]int, wasPresent bool) {
	_, present := m.current.Sectors[key]
	if present != wasPresent {
		m.topology = updateManagedIndex(m.topology, key, struct{}{}, present)
	}
}
