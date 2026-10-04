package gpu

import (
	"sort"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Waiting belongs to a live request, not a shared physical map or CPU revision.
// Material requests always address the selected map and have no pending generation.
type voxelAdmissionIdentity struct {
	object            *core.VoxelObject
	mapRef            *volume.XBrickMap
	pendingGeneration uint64
	material          bool
}

type voxelAdmissionWork struct {
	candidate voxelAdmissionCandidate
	material  bool
	required  bool
	first     uint64
}

func voxelAdmissionRequest(target voxelServiceTarget, material bool) voxelAdmissionIdentity {
	return voxelAdmissionIdentity{target.object, target.mapRef, target.pendingGeneration, material}
}

func voxelAdmissionMaterialRequired(m *GpuBufferManager, obj *core.VoxelObject) bool {
	return !obj.VoxelGPUAdmissionOptional || m.MaterialAllocations[obj] != nil
}

func voxelAdmissionOrder(target voxelServiceTarget) uint64 {
	if target.object.VoxelUploadOrder != 0 {
		return target.object.VoxelUploadOrder
	}
	return uint64(target.mapRef.ID)
}

// This raw ordering also selects the geometry/joint-material representative.
// Aging changes scheduling only, never the owner of that original preflight.
func voxelAdmissionRawLess(a, b voxelAdmissionCandidate) bool {
	if a.required != b.required {
		return a.required
	}
	if a.target.object.VoxelUploadPriority != b.target.object.VoxelUploadPriority {
		return a.target.object.VoxelUploadPriority < b.target.object.VoxelUploadPriority
	}
	return voxelAdmissionStableLess(a, b)
}

func voxelAdmissionStableLess(a, b voxelAdmissionCandidate) bool {
	if oa, ob := voxelAdmissionOrder(a.target), voxelAdmissionOrder(b.target); oa != ob {
		return oa < ob
	}
	if a.target.mapRef.ID != b.target.mapRef.ID {
		return a.target.mapRef.ID < b.target.mapRef.ID
	}
	if (a.target.pendingGeneration == 0) != (b.target.pendingGeneration == 0) {
		return a.target.pendingGeneration == 0
	}
	if a.target.object.VoxelGPUAdmissionOptional != b.target.object.VoxelGPUAdmissionOptional {
		return !a.target.object.VoxelGPUAdmissionOptional
	}
	return a.index < b.index
}

func (m *GpuBufferManager) beginVoxelAdmissionFrame() {
	m.voxelAdmissionFrame++
	if m.voxelAdmissionFrame == 0 {
		// A wrapped clock cannot give a new request an inherited ancient wait.
		m.voxelAdmissionFrame = 1
		m.voxelAdmissionAges = nil
	}
}

func (m *GpuBufferManager) voxelAdmissionSchedule(candidates []voxelAdmissionCandidate) ([]voxelAdmissionWork, map[voxelAdmissionIdentity]uint64) {
	frame := m.voxelAdmissionFrame
	ages := make(map[voxelAdmissionIdentity]uint64)
	first := func(target voxelServiceTarget, material bool) uint64 {
		id := voxelAdmissionRequest(target, material)
		birth, exists := m.voxelAdmissionAges[id]
		if !exists {
			birth = frame
		}
		ages[id] = birth
		return birth
	}
	effective := func(work voxelAdmissionWork) uint64 {
		priority := uint64(work.candidate.target.object.VoxelUploadPriority)
		promotion := (frame - work.first) / 8
		return priority - min(priority, promotion)
	}
	agedLess := func(a, b voxelAdmissionWork) bool {
		if pa, pb := effective(a), effective(b); pa != pb {
			return pa < pb
		}
		if a.first != b.first {
			return a.first < b.first
		}
		// Shared required geometry grants no scheduling class to optional
		// materials. After effective priority and age, use the ordinary ties.
		return voxelAdmissionStableLess(a.candidate, b.candidate)
	}
	best := make(map[*volume.XBrickMap]voxelAdmissionWork)
	var maps []*volume.XBrickMap
	var queue []voxelAdmissionWork
	seenMaterials := make(map[*core.VoxelObject]bool)
	for _, candidate := range candidates {
		target := candidate.target
		geometry := voxelAdmissionWork{candidate: candidate, required: candidate.required, first: frame}
		if !candidate.required {
			geometry.first = first(target, false)
		}
		previous, exists := best[target.mapRef]
		if !exists {
			maps = append(maps, target.mapRef)
		}
		if !exists || (!candidate.required && agedLess(geometry, previous)) {
			best[target.mapRef] = geometry
		}
		if target.pendingGeneration != 0 || seenMaterials[target.object] {
			continue
		}
		seenMaterials[target.object] = true
		materialRequired := voxelAdmissionMaterialRequired(m, target.object)
		// An existing material owner replacing optional geometry must still pass
		// that geometry's joint trial before any material admission can publish.
		material := voxelAdmissionWork{candidate: candidate, material: true, required: materialRequired && candidate.required, first: frame}
		if !materialRequired {
			material.first = first(target, true)
		}
		queue = append(queue, material)
	}
	for _, xbm := range maps {
		queue = append(queue, best[xbm])
	}
	sort.SliceStable(queue, func(i, j int) bool {
		a, b := queue[i], queue[j]
		if a.required != b.required {
			return a.required
		}
		if !a.required {
			if agedLess(a, b) {
				return true
			}
			if agedLess(b, a) {
				return false
			}
		} else {
			if voxelAdmissionRawLess(a.candidate, b.candidate) {
				return true
			}
			if voxelAdmissionRawLess(b.candidate, a.candidate) {
				return false
			}
		}
		return !a.material && b.material
	})
	return queue, ages
}

// The final plan decides completion after any backend-refusal rebuild. Requests
// omitted from the current snapshot (detached, required or new generation) vanish.
func (m *GpuBufferManager) finishVoxelAdmissionAges(ages map[voxelAdmissionIdentity]uint64, plan voxelAdmissionPlan) {
	for id := range ages {
		if (!id.material && plan.maps[id.mapRef]) || (id.material && plan.objects[id.object]) {
			delete(ages, id)
		}
	}
	m.voxelAdmissionAges = ages
}
