package gpu

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"unsafe"
)

// Each leaf is an exact physical lookup edge, independent of mutable allocation
// maps and brick pointer arrays. Frozen roots remain valid across later writes.
type sectorLookupInventoryEntry struct {
	coordinate [3]int
	sector     *volume.Sector
	slot       uint32
	bricks     [64]*volume.Brick
	inputBytes uint64
	eligible   bool
}
type sectorLookupInventoryNode struct {
	left, right                  *sectorLookupInventoryNode
	entry                        sectorLookupInventoryEntry
	height, count, eligibleCount int
	min, max                     [3]int
	bytes                        uint64
}

func sectorLookupInventoryHeight(n *sectorLookupInventoryNode) int {
	if n == nil {
		return 0
	}
	return n.height
}
func sectorLookupInventoryCount(n *sectorLookupInventoryNode) int {
	if n == nil {
		return 0
	}
	return n.count
}
func sectorLookupInventoryRebuild(entry sectorLookupInventoryEntry, left, right *sectorLookupInventoryNode) *sectorLookupInventoryNode {
	n := &sectorLookupInventoryNode{entry: entry, left: left, right: right, height: 1 + max(sectorLookupInventoryHeight(left), sectorLookupInventoryHeight(right)), count: 1 + sectorLookupInventoryCount(left) + sectorLookupInventoryCount(right), min: entry.coordinate, max: entry.coordinate, bytes: addRetainedVoxelBytes(uint64(unsafe.Sizeof(sectorLookupInventoryNode{})), entry.inputBytes)}
	if entry.eligible {
		n.eligibleCount = 1
	}
	for _, child := range []*sectorLookupInventoryNode{left, right} {
		if child == nil {
			continue
		}
		n.bytes = addRetainedVoxelBytes(n.bytes, child.bytes)
		n.eligibleCount += child.eligibleCount
		for a := 0; a < 3; a++ {
			n.min[a] = min(n.min[a], child.min[a])
			n.max[a] = max(n.max[a], child.max[a])
		}
	}
	return n
}
func sectorLookupInventoryBalance(n *sectorLookupInventoryNode) *sectorLookupInventoryNode {
	balance := sectorLookupInventoryHeight(n.left) - sectorLookupInventoryHeight(n.right)
	if balance > 1 {
		l := n.left
		if sectorLookupInventoryHeight(l.left) < sectorLookupInventoryHeight(l.right) {
			r := l.right
			l = sectorLookupInventoryRebuild(r.entry, sectorLookupInventoryRebuild(l.entry, l.left, r.left), r.right)
		}
		return sectorLookupInventoryRebuild(l.entry, l.left, sectorLookupInventoryRebuild(n.entry, l.right, n.right))
	}
	if balance < -1 {
		r := n.right
		if sectorLookupInventoryHeight(r.right) < sectorLookupInventoryHeight(r.left) {
			l := r.left
			r = sectorLookupInventoryRebuild(l.entry, l.left, sectorLookupInventoryRebuild(r.entry, l.right, r.right))
		}
		return sectorLookupInventoryRebuild(r.entry, sectorLookupInventoryRebuild(n.entry, n.left, r.left), r.right)
	}
	return n
}
func sectorLookupInventorySet(n *sectorLookupInventoryNode, entry sectorLookupInventoryEntry) *sectorLookupInventoryNode {
	if n == nil {
		return sectorLookupInventoryRebuild(entry, nil, nil)
	}
	c := managedGeometryContentCompare(entry.coordinate, n.entry.coordinate)
	if c == 0 {
		if n.entry == entry {
			return n
		}
		return sectorLookupInventoryRebuild(entry, n.left, n.right)
	}
	if c < 0 {
		return sectorLookupInventoryBalance(sectorLookupInventoryRebuild(n.entry, sectorLookupInventorySet(n.left, entry), n.right))
	}
	return sectorLookupInventoryBalance(sectorLookupInventoryRebuild(n.entry, n.left, sectorLookupInventorySet(n.right, entry)))
}
func sectorLookupInventoryDelete(n *sectorLookupInventoryNode, coordinate [3]int) *sectorLookupInventoryNode {
	if n == nil {
		return nil
	}
	c := managedGeometryContentCompare(coordinate, n.entry.coordinate)
	if c < 0 {
		return sectorLookupInventoryBalance(sectorLookupInventoryRebuild(n.entry, sectorLookupInventoryDelete(n.left, coordinate), n.right))
	}
	if c > 0 {
		return sectorLookupInventoryBalance(sectorLookupInventoryRebuild(n.entry, n.left, sectorLookupInventoryDelete(n.right, coordinate)))
	}
	if n.left == nil {
		return n.right
	}
	if n.right == nil {
		return n.left
	}
	next := n.right
	for next.left != nil {
		next = next.left
	}
	return sectorLookupInventoryBalance(sectorLookupInventoryRebuild(next.entry, n.left, sectorLookupInventoryDelete(n.right, next.entry.coordinate)))
}
func sectorLookupInventoryFind(n *sectorLookupInventoryNode, coordinate [3]int) (sectorLookupInventoryEntry, bool) {
	for n != nil {
		c := managedGeometryContentCompare(coordinate, n.entry.coordinate)
		if c == 0 {
			return n.entry, true
		}
		if c < 0 {
			n = n.left
		} else {
			n = n.right
		}
	}
	return sectorLookupInventoryEntry{}, false
}
func sectorLookupInventoryAt(n *sectorLookupInventoryNode, index int) (sectorLookupInventoryEntry, bool) {
	if index < 0 || index >= sectorLookupInventoryCount(n) {
		return sectorLookupInventoryEntry{}, false
	}
	for n != nil {
		left := sectorLookupInventoryCount(n.left)
		if index == left {
			return n.entry, true
		}
		if index < left {
			n = n.left
		} else {
			index -= left + 1
			n = n.right
		}
	}
	return sectorLookupInventoryEntry{}, false
}
func (m *GpuBufferManager) refreshSectorLookupInventory(xbm *volume.XBrickMap, coordinate [3]int) {
	a := m.Allocations[xbm]
	if !m.ownsVoxelAllocation(xbm, a) {
		return
	}
	sector := a.Sectors[coordinate]
	info, ok := m.SectorToInfo[sector]
	if sector == nil || !ok {
		m.dropSectorLookupInventory(xbm, coordinate)
		return
	}
	entry := sectorLookupInventoryEntry{coordinate: coordinate, sector: sector, slot: info.SlotIndex, eligible: !info.pending}
	if rows := a.Bricks[coordinate]; rows != nil {
		entry.bricks = *rows
	}
	entry.inputBytes = sectorLookupRetainedInputBytes(entry)
	if old, exists := sectorLookupInventoryFind(a.lookupRoot, coordinate); exists {
		if old == entry {
			return
		}
		sectorLookupPinEntry(&a.lookupSectorPins, &a.lookupBrickPins, old, -1)
	}
	sectorLookupPinEntry(&a.lookupSectorPins, &a.lookupBrickPins, entry, 1)
	a.lookupRoot = sectorLookupInventorySet(a.lookupRoot, entry)
}
func (m *GpuBufferManager) dropSectorLookupInventory(xbm *volume.XBrickMap, coordinate [3]int) {
	if a := m.Allocations[xbm]; a != nil {
		if old, exists := sectorLookupInventoryFind(a.lookupRoot, coordinate); exists {
			sectorLookupPinEntry(&a.lookupSectorPins, &a.lookupBrickPins, old, -1)
		}
		a.lookupRoot = sectorLookupInventoryDelete(a.lookupRoot, coordinate)
	}
}

// Persistent inverse multisets let frozen and selected lookup edges qualify
// pointer ownership without walking the coordinate inventory or entry arrays.
type sectorLookupPinNode struct {
	pointer                   unsafe.Pointer
	left, right               *sectorLookupPinNode
	key                       uintptr
	references, height, count int
}

func sectorLookupPinHeight(n *sectorLookupPinNode) int {
	if n == nil {
		return 0
	}
	return n.height
}
func sectorLookupPinCount(n *sectorLookupPinNode) int {
	if n == nil {
		return 0
	}
	return n.count
}
func sectorLookupPinMake(pointer unsafe.Pointer, key uintptr, refs int, left, right *sectorLookupPinNode) *sectorLookupPinNode {
	return &sectorLookupPinNode{pointer: pointer, key: key, references: refs, left: left, right: right, height: 1 + max(sectorLookupPinHeight(left), sectorLookupPinHeight(right)), count: 1 + sectorLookupPinCount(left) + sectorLookupPinCount(right)}
}
func sectorLookupPinBalance(n *sectorLookupPinNode) *sectorLookupPinNode {
	balance := sectorLookupPinHeight(n.left) - sectorLookupPinHeight(n.right)
	if balance > 1 {
		l := n.left
		if sectorLookupPinHeight(l.left) < sectorLookupPinHeight(l.right) {
			r := l.right
			l = sectorLookupPinMake(r.pointer, r.key, r.references, sectorLookupPinMake(l.pointer, l.key, l.references, l.left, r.left), r.right)
		}
		return sectorLookupPinMake(l.pointer, l.key, l.references, l.left, sectorLookupPinMake(n.pointer, n.key, n.references, l.right, n.right))
	}
	if balance < -1 {
		r := n.right
		if sectorLookupPinHeight(r.right) < sectorLookupPinHeight(r.left) {
			l := r.left
			r = sectorLookupPinMake(l.pointer, l.key, l.references, l.left, sectorLookupPinMake(r.pointer, r.key, r.references, l.right, r.right))
		}
		return sectorLookupPinMake(r.pointer, r.key, r.references, sectorLookupPinMake(n.pointer, n.key, n.references, n.left, r.left), r.right)
	}
	return n
}
func sectorLookupPinReferences(n *sectorLookupPinNode, key uintptr) int {
	for n != nil {
		if key == n.key {
			return n.references
		}
		if key < n.key {
			n = n.left
		} else {
			n = n.right
		}
	}
	return 0
}
func sectorLookupPinSet(n *sectorLookupPinNode, pointer unsafe.Pointer, key uintptr, refs int) *sectorLookupPinNode {
	if n == nil {
		if refs <= 0 {
			return nil
		}
		return sectorLookupPinMake(pointer, key, refs, nil, nil)
	}
	if key < n.key {
		return sectorLookupPinBalance(sectorLookupPinMake(n.pointer, n.key, n.references, sectorLookupPinSet(n.left, pointer, key, refs), n.right))
	}
	if key > n.key {
		return sectorLookupPinBalance(sectorLookupPinMake(n.pointer, n.key, n.references, n.left, sectorLookupPinSet(n.right, pointer, key, refs)))
	}
	if refs > 0 {
		return sectorLookupPinMake(pointer, key, refs, n.left, n.right)
	}
	if n.left == nil {
		return n.right
	}
	if n.right == nil {
		return n.left
	}
	next := n.right
	for next.left != nil {
		next = next.left
	}
	return sectorLookupPinBalance(sectorLookupPinMake(next.pointer, next.key, next.references, n.left, sectorLookupPinSet(n.right, next.pointer, next.key, 0)))
}
func sectorLookupPinAdjust(n *sectorLookupPinNode, pointer unsafe.Pointer, delta int) *sectorLookupPinNode {
	key := uintptr(pointer)
	if key == 0 {
		return n
	}
	return sectorLookupPinSet(n, pointer, key, sectorLookupPinReferences(n, key)+delta)
}
func sectorLookupPinEntry(sectors, bricks **sectorLookupPinNode, e sectorLookupInventoryEntry, delta int) {
	*sectors = sectorLookupPinAdjust(*sectors, unsafe.Pointer(e.sector), delta)
	for _, b := range e.bricks {
		if b != nil {
			*bricks = sectorLookupPinAdjust(*bricks, unsafe.Pointer(b), delta)
		}
	}
}

// Mutation hooks pay this fixed 64-edge accounting cost before the root is
// frozen. Freeze reads the cached conservative aggregate without a sector scan.
func sectorLookupRetainedInputBytes(e sectorLookupInventoryEntry) uint64 {
	if e.sector == nil {
		return 0
	}
	n := uint64(unsafe.Sizeof(volume.Sector{})) + uint64(cap(e.sector.PackedBricks))*uint64(unsafe.Sizeof((*volume.Brick)(nil)))
	brickBytes := func(b *volume.Brick) {
		if b != nil {
			n = addRetainedVoxelBytes(n, uint64(unsafe.Sizeof(volume.Brick{})))
			n = addRetainedVoxelBytes(n, uint64(cap(b.PrecomputedAux)))
		}
	}
	for _, b := range e.bricks {
		brickBytes(b)
	}
	// Public raw mutation may leave allocation edges and sector payload pointers
	// different. Charging both sets also conservatively permits shared aliases.
	for _, b := range e.sector.PackedBricks {
		brickBytes(b)
	}
	return n
}
