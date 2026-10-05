package gpu

import (
	"encoding/binary"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

// A block owns physical storage; attachments retain object-local compatibility
// acknowledgements and shadow dependencies.
type materialGPUBlock struct {
	table       *core.ImmutableMaterialTable
	rows        []core.Material
	data        []byte
	attachments map[*core.VoxelObject]*MaterialGpuAllocation
	allocation  MaterialGpuAllocation
	refs        int
}

func (m *GpuBufferManager) releaseMaterialAllocation(obj *core.VoxelObject) {
	a := m.MaterialAllocations[obj]
	delete(m.MaterialAllocations, obj)
	if a == nil {
		return
	}
	if b := a.block; b != nil {
		delete(b.attachments, obj)
		b.refs--
		if b.refs != 0 {
			return
		}
		delete(m.materialBlocks, b.table.Identity())
	}
	if a.MaterialCapacity > 0 {
		m.MaterialAlloc.FreeSlot(a.MaterialOffset / materialBlockCapacity)
	}
}

func materialAttachmentMatches(a *MaterialGpuAllocation, table *core.ImmutableMaterialTable) bool {
	if a == nil {
		return false
	}
	if a.block == nil {
		return table == nil
	}
	return table != nil && a.block.table.Identity() == table.Identity()
}

// Pin desired resident identities across detach/attach, preserving uploaded
// content and offsets even when all of their old attachments are replaced.
func (m *GpuBufferManager) prepareMaterialAttachments(objects []*core.VoxelObject) {
	if m.MaterialAllocations == nil {
		m.MaterialAllocations = make(map[*core.VoxelObject]*MaterialGpuAllocation)
	}
	if m.materialBlocks == nil {
		m.materialBlocks = make(map[string]*materialGPUBlock)
	}
	if m.managedMaterialObjects == nil {
		m.managedMaterialObjects = make(map[*core.VoxelObject]bool)
	}
	seen := make(map[*core.VoxelObject]bool)
	pinned := make(map[*materialGPUBlock]bool)
	for _, obj := range objects {
		if geometry := m.Allocations[obj.RenderVoxelMap()]; geometry != nil {
			geometry.materialOwner = m
		}
		if table := obj.ImmutableMaterialTable(); table != nil {
			m.managedMaterialObjects[obj] = true
			if b := m.materialBlocks[table.Identity()]; b != nil && !pinned[b] {
				b.refs++
				pinned[b] = true
			}
		}
	}
	for _, obj := range objects {
		if seen[obj] {
			continue
		}
		seen[obj] = true
		if a := m.MaterialAllocations[obj]; a != nil && !materialAttachmentMatches(a, obj.ImmutableMaterialTable()) {
			m.releaseMaterialAllocation(obj)
		}
	}
	clear(seen)
	for _, obj := range objects {
		if seen[obj] {
			continue
		}
		seen[obj] = true
		table := obj.ImmutableMaterialTable()
		a := m.MaterialAllocations[obj]
		if a == nil {
			a = &MaterialGpuAllocation{MaterialTableLen: -1}
			if table != nil {
				b := m.materialBlocks[table.Identity()]
				if b == nil {
					b = &materialGPUBlock{table: table, allocation: MaterialGpuAllocation{MaterialOffset: m.MaterialAlloc.Alloc() * materialBlockCapacity, MaterialCapacity: materialBlockCapacity, MaterialTableLen: -1}}
					b.rows = table.MaterialTable()
					b.data = make([]byte, materialBlockCapacity*64)
					copy(b.data, buildMaterialData(b.rows))
					b.attachments = make(map[*core.VoxelObject]*MaterialGpuAllocation)
					m.materialBlocks[table.Identity()] = b
				}
				b.refs++
				a.block = b
				b.attachments[obj] = a
				a.MaterialOffset, a.MaterialCapacity = b.allocation.MaterialOffset, b.allocation.MaterialCapacity
			}
			m.MaterialAllocations[obj] = a
		}
		if m.managedMaterialObjects[obj] {
			a.managedOwner = m
		}
		if a.block == nil && a.MaterialCapacity < uint32(materialUploadRows(len(obj.MaterialTable))) {
			if a.MaterialCapacity > 0 {
				m.MaterialAlloc.FreeSlot(a.MaterialOffset / materialBlockCapacity)
			}
			a.MaterialOffset = m.MaterialAlloc.Alloc() * materialBlockCapacity
			a.MaterialCapacity = materialBlockCapacity
			a.MaterialTableLen = -1
		}
		if b := a.block; b != nil && b.allocation.MaterialTableLen >= 0 && b.allocation.BufferGeneration == m.MaterialBufferGeneration {
			m.acknowledgeMaterialAttachment(obj, a, b)
		}
	}
	for b := range pinned {
		b.refs--
	}
}

func materialBindingReady(obj *core.VoxelObject, allocation *MaterialGpuAllocation, generation uint64) bool {
	if allocation == nil {
		return false
	}
	table := obj.ImmutableMaterialTable()
	if table != nil && allocation.block == nil {
		return false
	}
	if block := allocation.block; block != nil {
		if table == nil || table.Identity() != block.table.Identity() || block.allocation.MaterialTableLen < 0 || block.allocation.BufferGeneration != generation {
			return false
		}
	}
	ptr, length := materialTableIdentity(obj.MaterialTable)
	return allocation.MaterialTablePtr == ptr && allocation.MaterialTableLen == length && allocation.BufferGeneration == generation && allocation.MaterialCapacity >= uint32(materialUploadRows(length))
}

// Manual legacy allocations retain their historical publication behavior. A
// certified binding and its later private replacements qualify acknowledgement
// against the owning manager's live generation at each encoding call.
func materialPublicationReady(obj *core.VoxelObject, geometry *ObjectGpuAllocation, allocation *MaterialGpuAllocation) bool {
	var owner *GpuBufferManager
	if allocation != nil {
		owner = allocation.managedOwner
	}
	if owner == nil && geometry != nil && geometry.materialOwner != nil && geometry.materialOwner.managedMaterialObjects[obj] {
		owner = geometry.materialOwner
	}
	if owner == nil {
		return obj.ImmutableMaterialTable() == nil
	}
	return materialBindingReady(obj, allocation, owner.MaterialBufferGeneration)
}

func writeRejectedObjectParams(dst []byte) {
	clear(dst[:objectParamsSizeBytes])
	binary.LittleEndian.PutUint32(dst[16:20], ^uint32(0))
	binary.LittleEndian.PutUint32(dst[108:112], LookupModeDirect)
	binary.LittleEndian.PutUint32(dst[124:128], DirectSectorLookupInvalid)
}

func (m *GpuBufferManager) acknowledgeMaterialAttachment(obj *core.VoxelObject, a *MaterialGpuAllocation, b *materialGPUBlock) {
	a.MaterialTablePtr, a.MaterialTableLen = materialTableIdentity(obj.MaterialTable)
	a.BufferGeneration = b.allocation.BufferGeneration
	a.HasTransparency = b.allocation.HasTransparency
	a.shadowOpacity = b.allocation.shadowOpacity
	a.shadowUploadEpoch = b.allocation.shadowUploadEpoch
}

// A fully queued migration preserves sealed content. Unacknowledged blocks and
// externally reset generations still require service before publication.
func (m *GpuBufferManager) advanceMaterialBufferGeneration(copied bool) {
	previous := m.MaterialBufferGeneration
	m.MaterialBufferGeneration++
	if !copied {
		return
	}
	for _, b := range m.materialBlocks {
		if b.allocation.MaterialTableLen >= 0 && b.allocation.BufferGeneration == previous {
			b.allocation.BufferGeneration = m.MaterialBufferGeneration
		}
	}
}
