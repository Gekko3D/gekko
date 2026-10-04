package volume

import "cmp"

// Nodes are immutable after construction. AVL path copying lets edits retain
// old views and lets sealed forks share all unchanged subtrees. The empty-value
// specialization stores coordinates only; geometry uses private record pointers.
type managedIndexNode[T comparable] struct {
	coord        [3]int
	value        T
	left, right  *managedIndexNode[T]
	height, size int
}

func managedIndexHeight[T comparable](node *managedIndexNode[T]) int {
	if node == nil {
		return 0
	}
	return node.height
}

func managedIndexSize[T comparable](node *managedIndexNode[T]) int {
	if node == nil {
		return 0
	}
	return node.size
}

func newManagedIndexNode[T comparable](coord [3]int, value T, left, right *managedIndexNode[T]) *managedIndexNode[T] {
	return &managedIndexNode[T]{
		coord: coord, value: value, left: left, right: right,
		height: 1 + max(managedIndexHeight(left), managedIndexHeight(right)),
		size:   1 + managedIndexSize(left) + managedIndexSize(right),
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

func buildManagedIndex[T comparable](keys [][3]int, value func([3]int) T) *managedIndexNode[T] {
	if len(keys) == 0 {
		return nil
	}
	middle := len(keys) / 2
	return newManagedIndexNode(keys[middle], value(keys[middle]), buildManagedIndex(keys[:middle], value), buildManagedIndex(keys[middle+1:], value))
}

func managedIndexAt[T comparable](node *managedIndexNode[T], index int) *managedIndexNode[T] {
	if index < 0 || index >= managedIndexSize(node) {
		return nil
	}
	for node != nil {
		leftSize := managedIndexSize(node.left)
		if index < leftSize {
			node = node.left
		} else if index == leftSize {
			return node
		} else {
			index -= leftSize + 1
			node = node.right
		}
	}
	return nil
}

func findManagedIndex[T comparable](node *managedIndexNode[T], coord [3]int) *managedIndexNode[T] {
	for node != nil {
		order := compareManagedTopologyCoords(coord, node.coord)
		if order < 0 {
			node = node.left
		} else if order > 0 {
			node = node.right
		} else {
			return node
		}
	}
	return nil
}

// Synchronous consumers walk every record once without repeated rank searches.
func walkManagedIndex[T comparable](node *managedIndexNode[T], visit func([3]int, T)) {
	if node == nil {
		return
	}
	walkManagedIndex(node.left, visit)
	visit(node.coord, node.value)
	walkManagedIndex(node.right, visit)
}

func updateManagedIndex[T comparable](node *managedIndexNode[T], coord [3]int, value T, present bool) *managedIndexNode[T] {
	if node == nil {
		if present {
			return newManagedIndexNode(coord, value, nil, nil)
		}
		return nil
	}
	order := compareManagedTopologyCoords(coord, node.coord)
	if order < 0 {
		left := updateManagedIndex(node.left, coord, value, present)
		if left == node.left {
			return node
		}
		return balanceManagedIndex(node.coord, node.value, left, node.right)
	}
	if order > 0 {
		right := updateManagedIndex(node.right, coord, value, present)
		if right == node.right {
			return node
		}
		return balanceManagedIndex(node.coord, node.value, node.left, right)
	}
	if present {
		if node.value == value {
			return node
		}
		return newManagedIndexNode(coord, value, node.left, node.right)
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
	right := updateManagedIndex(node.right, successor.coord, successor.value, false)
	return balanceManagedIndex(successor.coord, successor.value, node.left, right)
}

// Rebuild and rotate without mutating any node visible to a prior root.
func balanceManagedIndex[T comparable](coord [3]int, value T, left, right *managedIndexNode[T]) *managedIndexNode[T] {
	balance := managedIndexHeight(left) - managedIndexHeight(right)
	if balance > 1 {
		if managedIndexHeight(left.left) >= managedIndexHeight(left.right) {
			return newManagedIndexNode(left.coord, left.value, left.left, newManagedIndexNode(coord, value, left.right, right))
		}
		pivot := left.right
		return newManagedIndexNode(pivot.coord, pivot.value,
			newManagedIndexNode(left.coord, left.value, left.left, pivot.left),
			newManagedIndexNode(coord, value, pivot.right, right))
	}
	if balance < -1 {
		if managedIndexHeight(right.right) >= managedIndexHeight(right.left) {
			return newManagedIndexNode(right.coord, right.value, newManagedIndexNode(coord, value, left, right.left), right.right)
		}
		pivot := right.left
		return newManagedIndexNode(pivot.coord, pivot.value,
			newManagedIndexNode(coord, value, left, pivot.left),
			newManagedIndexNode(right.coord, right.value, pivot.right, right.right))
	}
	return newManagedIndexNode(coord, value, left, right)
}
