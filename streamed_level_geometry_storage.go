package gekko

import (
	"unsafe"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

const (
	streamedGeometryPrepared = iota
	streamedGeometryAsset
	streamedGeometryPinned
)

// The voxel graph is immutable while owned by this cache. Capture object
// storage, child links and logical map entries on first admission, then update
// references without rescanning payloads during metrics/frame maintenance.
// Renderer dirty-map churn is transient upload metadata, not a new geometry
// admission, and is not dynamically charged by this ledger.
// The charge excludes map bucket slack, allocator overhead, bookkeeping and
// borrowed GPU-manager storage. Overlapping slice backings are conservatively
// charged per distinct Sector/Brick, as the production builders do not alias them.
type streamedGeometryStorageLedger struct {
	nodes                                         map[any]*streamedGeometryStorageNode
	bytes, preparedBytes, assetBytes, pinnedBytes int64
	referenceVisits                               int
	captureVisits                                 int
}

type streamedGeometryStorageNode struct {
	object   any
	bytes    int64
	refs     [3]int
	children []*streamedGeometryStorageNode
}

// Sealed while the independent registration map is private to its worker.
// Nodes describe actual storage identities with zero references; the temporary
// builder identity map is discarded before the descriptor is published.
type streamedGeometryStorageDescriptor struct {
	root          *streamedGeometryStorageNode
	nodes         []*streamedGeometryStorageNode
	geometryBytes int64
	metadataBytes int64
}

func captureStreamedGeometryStorageDescriptor(geometry *volume.XBrickMap) *streamedGeometryStorageDescriptor {
	if geometry == nil {
		return nil
	}
	builder := streamedGeometryStorageLedger{nodes: make(map[any]*streamedGeometryStorageNode)}
	descriptor := &streamedGeometryStorageDescriptor{root: builder.mapNode(geometry)}
	descriptor.nodes = make([]*streamedGeometryStorageNode, 0, len(builder.nodes))
	for _, node := range builder.nodes {
		descriptor.nodes = append(descriptor.nodes, node)
		descriptor.geometryBytes = runtimeContentChargeSum(descriptor.geometryBytes, node.bytes)
		descriptor.metadataBytes = runtimeContentChargeSum(descriptor.metadataBytes,
			int64(unsafe.Sizeof(*node)),
			runtimeContentChargeProduct(int64(cap(node.children)), int64(unsafe.Sizeof(node))))
	}
	descriptor.metadataBytes = runtimeContentChargeSum(descriptor.metadataBytes,
		int64(unsafe.Sizeof(*descriptor)),
		runtimeContentChargeProduct(int64(cap(descriptor.nodes)), int64(unsafe.Sizeof(descriptor.root))))
	return descriptor
}

// Caller holds the cache mutex. Validate the entire descriptor before inserting
// any identity, so conflicts fall back to the existing union-admission path.
func (l *streamedGeometryStorageLedger) installDescriptor(descriptor *streamedGeometryStorageDescriptor, geometry *volume.XBrickMap) *streamedGeometryStorageNode {
	if descriptor == nil || descriptor.root == nil || descriptor.root.object != geometry {
		return nil
	}
	for _, node := range descriptor.nodes {
		if node == nil || node.refs != ([3]int{}) || l.nodes[node.object] != nil {
			return nil
		}
	}
	if l.nodes == nil {
		l.nodes = make(map[any]*streamedGeometryStorageNode)
	}
	for _, node := range descriptor.nodes {
		l.nodes[node.object] = node
	}
	return descriptor.root
}

func (l *streamedGeometryStorageLedger) admit(geometry *volume.XBrickMap, kind int) *streamedGeometryStorageNode {
	if geometry == nil {
		return nil
	}
	if l.nodes == nil {
		l.nodes = make(map[any]*streamedGeometryStorageNode)
	}
	node := l.mapNode(geometry)
	l.adjust(node, kind, 1)
	return node
}

func (l *streamedGeometryStorageLedger) admitSource(source *streamedGeometrySource, kind int) *streamedGeometryStorageNode {
	if source == nil {
		return nil
	}
	if source.dense != nil && !source.qualified {
		return l.admit(source.dense, kind)
	}
	if l.nodes == nil {
		l.nodes = make(map[any]*streamedGeometryStorageNode)
	}
	node := l.nodes[source]
	if node == nil {
		node = &streamedGeometryStorageNode{object: source}
		l.nodes[source] = node
		l.captureVisits++
		if source.dense != nil {
			node.bytes = int64(unsafe.Sizeof(*source))
			node.children = []*streamedGeometryStorageNode{l.mapNode(source.dense)}
		} else {
			node.bytes = source.charge()
		}
	}
	l.adjust(node, kind, 1)
	return node
}

func (l *streamedGeometryStorageLedger) mapNode(geometry *volume.XBrickMap) *streamedGeometryStorageNode {
	if node := l.nodes[geometry]; node != nil {
		return node
	}
	charge := int64(unsafe.Sizeof(*geometry)) +
		int64(len(geometry.Sectors))*int64(unsafe.Sizeof([3]int{})+unsafe.Sizeof((*volume.Sector)(nil))) +
		int64(len(geometry.SectorRevisions))*int64(unsafe.Sizeof([3]int{})+unsafe.Sizeof(uint64(0))) +
		int64(len(geometry.DirtySectors))*int64(unsafe.Sizeof([3]int{})+unsafe.Sizeof(false)) +
		int64(len(geometry.DirtyBricks))*int64(unsafe.Sizeof([6]int{})+unsafe.Sizeof(false))
	node := &streamedGeometryStorageNode{object: geometry, bytes: charge}
	l.nodes[geometry] = node
	l.captureVisits++
	for _, sector := range geometry.Sectors {
		if sector != nil {
			node.children = append(node.children, l.sectorNode(sector))
		}
	}
	return node
}

func (l *streamedGeometryStorageLedger) sectorNode(sector *volume.Sector) *streamedGeometryStorageNode {
	if node := l.nodes[sector]; node != nil {
		return node
	}
	node := &streamedGeometryStorageNode{
		object: sector,
		bytes:  int64(unsafe.Sizeof(*sector)) + int64(cap(sector.PackedBricks))*int64(unsafe.Sizeof((*volume.Brick)(nil))),
	}
	l.nodes[sector] = node
	l.captureVisits++
	for _, brick := range sector.PackedBricks {
		if brick != nil {
			node.children = append(node.children, l.brickNode(brick))
		}
	}
	return node
}

func (l *streamedGeometryStorageLedger) brickNode(brick *volume.Brick) *streamedGeometryStorageNode {
	if node := l.nodes[brick]; node != nil {
		return node
	}
	// Brick embeds its dense CPU payload even when GPU flags mark it uniform.
	node := &streamedGeometryStorageNode{object: brick, bytes: int64(unsafe.Sizeof(*brick)) + int64(cap(brick.PrecomputedAux))}
	l.nodes[brick] = node
	l.captureVisits++
	return node
}

func (l *streamedGeometryStorageLedger) adjust(node *streamedGeometryStorageNode, kind, delta int) {
	if node == nil {
		return
	}
	l.referenceVisits++
	before := node.refs
	node.refs[kind] += delta
	after := node.refs
	l.bytes += streamedGeometryPresenceDelta(before[0]+before[1], after[0]+after[1]) * node.bytes
	l.preparedBytes += streamedGeometryPresenceDelta(before[0], after[0]) * node.bytes
	// Prefer prepared attribution if an object is shared across owner kinds;
	// every physical charge still contributes once to the total.
	beforeAsset, afterAsset := 0, 0
	if before[0] == 0 {
		beforeAsset = before[1]
	}
	if after[0] == 0 {
		afterAsset = after[1]
	}
	l.assetBytes += streamedGeometryPresenceDelta(beforeAsset, afterAsset) * node.bytes
	l.pinnedBytes += streamedGeometryPresenceDelta(before[2], after[2]) * node.bytes
	// An active parent contributes one reference per child edge and owner kind.
	// Repeated owners change this node's count without walking its children.
	if childDelta := streamedGeometryPresenceDelta(before[kind], after[kind]); childDelta != 0 {
		for _, child := range node.children {
			l.adjust(child, kind, int(childDelta))
		}
	}
	if after == ([3]int{}) {
		delete(l.nodes, node.object)
	}
}

func streamedGeometryPresenceDelta(before, after int) int64 {
	if before == 0 && after > 0 {
		return 1
	}
	if before > 0 && after == 0 {
		return -1
	}
	return 0
}

// A result's standalone charge determines whether it can ever be retained
// warmly, independently of aliases already owned by other cache keys.
func streamedGeometryStorageCharge(roots ...*streamedGeometryStorageNode) int64 {
	seen := make(map[*streamedGeometryStorageNode]struct{})
	var visit func(*streamedGeometryStorageNode) int64
	visit = func(node *streamedGeometryStorageNode) int64 {
		if node == nil {
			return 0
		}
		if _, exists := seen[node]; exists {
			return 0
		}
		seen[node] = struct{}{}
		charge := node.bytes
		for _, child := range node.children {
			charge += visit(child)
		}
		return charge
	}
	var charge int64
	for _, root := range roots {
		charge += visit(root)
	}
	return charge
}
