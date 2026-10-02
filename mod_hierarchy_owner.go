package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

// TransformHierarchyStats reports cumulative topology builds and child TRS
// compositions, plus membership at the last hierarchy invocation.
type TransformHierarchyStats struct {
	TopologyBuildCount uint64
	CompositionCount   uint64
	TransformCount     int
}

// The storage owner retains locations and values, never live component aliases.
// Nonempty inventories may retain peak capacity; empty inventories release it.
type transformHierarchyOwner struct {
	initialized                bool
	revision                   uint64
	worldID, localID, parentID componentId
	batches                    []hierarchyBatch
	nodes                      []hierarchyNode
	byEntity                   map[EntityId]int
	order                      []int
	path                       []int
	stats                      TransformHierarchyStats
}

type hierarchyBatch struct {
	arch          *archetype
	start, end    int
	local, parent bool
}

type hierarchyNode struct {
	entity                            EntityId
	batch                             int
	row                               row
	parent                            EntityId
	parentIndex                       int
	child                             bool
	state                             uint8
	produced                          bool
	parentBits, localBits, outputBits hierarchyTRSBits
}

type hierarchyTRSBits [10]uint32

func hierarchyBits(position mgl32.Vec3, rotation mgl32.Quat, scale mgl32.Vec3) hierarchyTRSBits {
	return hierarchyTRSBits{
		math.Float32bits(position[0]), math.Float32bits(position[1]), math.Float32bits(position[2]),
		math.Float32bits(rotation.W), math.Float32bits(rotation.V[0]), math.Float32bits(rotation.V[1]), math.Float32bits(rotation.V[2]),
		math.Float32bits(scale[0]), math.Float32bits(scale[1]), math.Float32bits(scale[2]),
	}
}

func (h *transformHierarchyOwner) rebuildMembership(ecs *Ecs) {
	clear(h.batches[:cap(h.batches)])
	clear(h.nodes[:cap(h.nodes)])
	h.batches = h.batches[:0]
	h.nodes = h.nodes[:0]
	clear(h.byEntity)
	if h.byEntity == nil {
		h.byEntity = make(map[EntityId]int)
	}
	h.worldID, h.localID, h.parentID = identifyComponents3[TransformComponent, LocalTransformComponent, Parent](ecs)
	for _, arch := range ecs.storage.archetypes {
		if len(arch.entities) == 0 {
			continue
		}
		if _, ok := arch.componentData[h.worldID]; !ok {
			continue
		}
		_, local := arch.componentData[h.localID]
		_, parent := arch.componentData[h.parentID]
		batch := len(h.batches)
		start := len(h.nodes)
		for entity, row := range arch.entities {
			h.byEntity[entity] = len(h.nodes)
			h.nodes = append(h.nodes, hierarchyNode{entity: entity, batch: batch, row: row, child: local && parent})
		}
		h.batches = append(h.batches, hierarchyBatch{arch: arch, start: start, end: len(h.nodes), local: local, parent: parent})
	}
	h.revision = ecs.StructuralRevision()
	h.initialized = true
	h.stats.TransformCount = len(h.nodes)
	if len(h.nodes) == 0 {
		h.batches, h.nodes, h.byEntity, h.order, h.path = nil, nil, nil, nil, nil
	}
}

// Resolve each path once. A visiting ancestor marks the entire current path
// invalid, including descendants entering a cycle. Sources have no dependency.
func (h *transformHierarchyOwner) rebuildTopology() {
	h.order = h.order[:0]
	for i := range h.nodes {
		n := &h.nodes[i]
		n.state = 0
		n.parentIndex = -1
		if !n.child {
			n.state = 2
		} else if index, ok := h.byEntity[n.parent]; ok {
			n.parentIndex = index
		}
	}
	for start := range h.nodes {
		if h.nodes[start].state != 0 {
			continue
		}
		h.path = h.path[:0]
		index := start
		for index >= 0 && h.nodes[index].state == 0 {
			h.nodes[index].state = 1
			h.path = append(h.path, index)
			index = h.nodes[index].parentIndex
		}
		valid := index >= 0 && h.nodes[index].state == 2
		for j := len(h.path) - 1; j >= 0; j-- {
			n := &h.nodes[h.path[j]]
			if valid {
				n.state = 2
				h.order = append(h.order, h.path[j])
			} else {
				n.state = 3
				n.produced = false
			}
		}
	}
	h.path = h.path[:0]
	h.stats.TopologyBuildCount++
}

// These columns exist only for one invocation. Every batch reacquires its exact
// current slices, including after growth, replacement or archetype migration.
type hierarchyColumns struct {
	worlds  []TransformComponent
	locals  []LocalTransformComponent
	parents []Parent
}

func (h *transformHierarchyOwner) update(ecs *Ecs) {
	changed := !h.initialized || h.revision != ecs.StructuralRevision()
	if changed {
		h.rebuildMembership(ecs)
	}
	columns := make([]hierarchyColumns, len(h.batches))
	for i, batch := range h.batches {
		c := &columns[i]
		c.worlds = batch.arch.componentData[h.worldID].([]TransformComponent)
		if batch.local {
			c.locals = batch.arch.componentData[h.localID].([]LocalTransformComponent)
		}
		if batch.parent {
			c.parents = batch.arch.componentData[h.parentID].([]Parent)
		}
		for index := batch.start; index < batch.end; index++ {
			n := &h.nodes[index]
			if n.child {
				parent := c.parents[n.row].Entity
				if n.parent != parent {
					n.parent = parent
					n.produced = false
					changed = true
				}
			} else if batch.local && !batch.parent {
				world, local := &c.worlds[n.row], &c.locals[n.row]
				local.Position, local.Rotation, local.Scale = world.Position, world.Rotation, world.Scale
			}
		}
	}
	// All edges must be known before resolving or composing any branch.
	if changed {
		h.rebuildTopology()
	}
	for _, index := range h.order {
		n := &h.nodes[index]
		c := &columns[n.batch]
		world, local := &c.worlds[n.row], &c.locals[n.row]
		p := &h.nodes[n.parentIndex]
		parent := &columns[p.batch].worlds[p.row]
		parentBits := hierarchyBits(parent.Position, parent.Rotation, parent.Scale)
		localBits := hierarchyBits(local.Position, local.Rotation, local.Scale)
		outputBits := hierarchyBits(world.Position, world.Rotation, world.Scale)
		if n.produced && n.parentBits == parentBits && n.localBits == localBits && n.outputBits == outputBits {
			continue
		}
		scaledLocalPos := mgl32.Vec3{
			local.Position.X() * parent.Scale.X(),
			local.Position.Y() * parent.Scale.Y(),
			local.Position.Z() * parent.Scale.Z(),
		}
		world.Position = parent.Position.Add(parent.Rotation.Rotate(scaledLocalPos))
		world.Rotation = parent.Rotation.Mul(local.Rotation).Normalize()
		world.Scale = mgl32.Vec3{
			parent.Scale.X() * local.Scale.X(),
			parent.Scale.Y() * local.Scale.Y(),
			parent.Scale.Z() * local.Scale.Z(),
		}
		n.parentBits, n.localBits = parentBits, localBits
		n.outputBits = hierarchyBits(world.Position, world.Rotation, world.Scale)
		n.produced = true
		h.stats.CompositionCount++
	}
}
