package volume

import (
	"cmp"
	"slices"
)

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
	return managedTopologySize(v.root)
}

// Coord reads a captured coordinate by rank in logarithmic time without
// allocation. Negative and out-of-range indices return a zero coordinate and
// false, including for the zero view.
func (v ManagedTopologyView) Coord(index int) ([3]int, bool) {
	if index < 0 || index >= v.Len() {
		return [3]int{}, false
	}
	for node := v.root; node != nil; {
		leftSize := managedTopologySize(node.left)
		if index < leftSize {
			node = node.left
		} else if index == leftSize {
			return node.coord, true
		} else {
			index -= leftSize + 1
			node = node.right
		}
	}
	return [3]int{}, false
}

// Nodes are immutable after construction. AVL path copying lets edits retain
// old views and lets sealed forks share all unchanged coordinate subtrees.
type managedTopologyNode struct {
	coord        [3]int
	left, right  *managedTopologyNode
	height, size int
}

func managedTopologyHeight(node *managedTopologyNode) int {
	if node == nil {
		return 0
	}
	return node.height
}

func managedTopologySize(node *managedTopologyNode) int {
	if node == nil {
		return 0
	}
	return node.size
}

func newManagedTopologyNode(coord [3]int, left, right *managedTopologyNode) *managedTopologyNode {
	return &managedTopologyNode{
		coord: coord, left: left, right: right,
		height: 1 + max(managedTopologyHeight(left), managedTopologyHeight(right)),
		size:   1 + managedTopologySize(left) + managedTopologySize(right),
	}
}

func compareManagedTopologyCoords(a, b [3]int) int {
	for axis := range a {
		if order := cmp.Compare(a[axis], b[axis]); order != 0 {
			return order
		}
	}
	return 0
}

// Constructor seeding uses current map keys, including allocated empty sectors,
// independently of payload occupancy, original base keys and tombstones.
func (m *ManagedXBrickMap) seedTopology() {
	keys := make([][3]int, 0, len(m.current.Sectors))
	for key := range m.current.Sectors {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareManagedTopologyCoords)
	m.topology = buildManagedTopology(keys)
}

func buildManagedTopology(keys [][3]int) *managedTopologyNode {
	if len(keys) == 0 {
		return nil
	}
	middle := len(keys) / 2
	return newManagedTopologyNode(keys[middle], buildManagedTopology(keys[:middle]), buildManagedTopology(keys[middle+1:]))
}

// Reconcile only the dense edit's target key. Halo invalidation and content
// changes do not alter membership or allocate coordinate index nodes.
func (m *ManagedXBrickMap) reconcileTopology(key [3]int, wasPresent bool) {
	_, present := m.current.Sectors[key]
	if present != wasPresent {
		m.topology = updateManagedTopology(m.topology, key, present)
	}
}

func updateManagedTopology(node *managedTopologyNode, coord [3]int, present bool) *managedTopologyNode {
	if node == nil {
		if present {
			return newManagedTopologyNode(coord, nil, nil)
		}
		return nil
	}
	order := compareManagedTopologyCoords(coord, node.coord)
	if order < 0 {
		left := updateManagedTopology(node.left, coord, present)
		if left == node.left {
			return node
		}
		return balanceManagedTopology(node.coord, left, node.right)
	}
	if order > 0 {
		right := updateManagedTopology(node.right, coord, present)
		if right == node.right {
			return node
		}
		return balanceManagedTopology(node.coord, node.left, right)
	}
	if present {
		return node
	}
	if node.left == nil {
		return node.right
	}
	if node.right == nil {
		return node.left
	}
	successor := node.right
	for successor.left != nil {
		successor = successor.left
	}
	right := updateManagedTopology(node.right, successor.coord, false)
	return balanceManagedTopology(successor.coord, node.left, right)
}

// Rebuild and rotate without mutating any node visible to a prior root.
func balanceManagedTopology(coord [3]int, left, right *managedTopologyNode) *managedTopologyNode {
	balance := managedTopologyHeight(left) - managedTopologyHeight(right)
	if balance > 1 {
		if managedTopologyHeight(left.left) >= managedTopologyHeight(left.right) {
			return newManagedTopologyNode(left.coord, left.left, newManagedTopologyNode(coord, left.right, right))
		}
		pivot := left.right
		return newManagedTopologyNode(pivot.coord,
			newManagedTopologyNode(left.coord, left.left, pivot.left),
			newManagedTopologyNode(coord, pivot.right, right))
	}
	if balance < -1 {
		if managedTopologyHeight(right.right) >= managedTopologyHeight(right.left) {
			return newManagedTopologyNode(right.coord, newManagedTopologyNode(coord, left, right.left), right.right)
		}
		pivot := right.left
		return newManagedTopologyNode(pivot.coord,
			newManagedTopologyNode(coord, left, pivot.left),
			newManagedTopologyNode(right.coord, pivot.right, right.right))
	}
	return newManagedTopologyNode(coord, left, right)
}
