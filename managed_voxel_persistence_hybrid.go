package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Planning retains only scalars. Inventory is reread after admission to produce
// the two owned backing arrays; no per-brick plan or live owner reaches workers.
type managedVoxelPayloadPlan struct{ records, selectors int }

func managedVoxelSelector(key [6]int) [3]int32 {
	const bricksPerSector = volume.SectorSize / volume.BrickSize
	return [3]int32{int32(key[0]*bricksPerSector + key[3]), int32(key[1]*bricksPerSector + key[4]), int32(key[2]*bricksPerSector + key[5])}
}

func managedVoxelIntegerBytes(value int32) int {
	n := int64(value)
	size := 1
	if n < 0 {
		size++
		n = -n
	}
	for n >= 10 {
		size++
		n /= 10
	}
	return size
}

func managedVoxelSelectorBytes(key [6]int) int {
	selector := managedVoxelSelector(key)
	return 4 + managedVoxelIntegerBytes(selector[0]) + managedVoxelIntegerBytes(selector[1]) + managedVoxelIntegerBytes(selector[2])
}

// C1 has an 80-byte brick header, one uniform primary byte for assignments,
// and uniform (one byte) or mixed (occupied-count bytes) material channels.
// Cheap count/lower-bound filters precede target reads. The remaining scans are
// bounded by 512 cells and inspect actual primary values, independent of flags.
func managedVoxelBrickReplacement(owner *volume.ManagedXBrickMap, key [6]int, assignments, current int) (int, bool) {
	selectorBytes := managedVoxelSelectorBytes(key) + 1 // include its array comma
	if current >= assignments {
		return 0, false
	}
	minimumFull := 0
	if current > 0 {
		minimumFull = 81
	}
	if 81+assignments-minimumFull <= selectorBytes {
		return 0, false
	}
	secondarySize := 1
	var first uint8
	seen := false
	owner.VisitChangedBrickAssignments(key, func(_ [3]int, value uint8) bool {
		if !seen {
			first, seen = value, true
		}
		if value != first {
			secondarySize = assignments
			return false
		}
		return true
	})
	deltaBytes := 81 + secondarySize
	if deltaBytes-minimumFull <= selectorBytes {
		return 0, false
	}
	fullBytes := 0
	if current > 0 {
		primarySize := 1
		seen = false
		owner.VisitCurrentBrickVoxels(key, func(_ [3]int, value uint8) bool {
			if !seen {
				first, seen = value, true
			}
			if value != first {
				primarySize = current
				return false
			}
			return true
		})
		fullBytes = 80 + primarySize
	}
	savings := deltaBytes - fullBytes
	return savings, savings > selectorBytes
}

func planManagedVoxelHybrid(payload content.VoxelObjectPayloadDef, entry *managedVoxelGeometry, assignments int) (content.VoxelObjectPayloadDef, managedVoxelPayloadPlan) {
	fallback := managedVoxelPayloadPlan{records: assignments}
	if assignments == 0 {
		return payload, fallback
	}
	plan := fallback
	savings, selectorBytes := 0, 0
	entry.owner.VisitChangedBricks(func(key [6]int, count, current int) bool {
		saving, selected := managedVoxelBrickReplacement(entry.owner, key, count, current)
		if selected {
			plan.selectors++
			plan.records += current - count
			savings += saving
			selectorBytes += managedVoxelSelectorBytes(key) + 1
		}
		return true
	})
	if plan.selectors == 0 {
		return payload, fallback
	}
	v2Size, err := content.VoxelObjectPayloadMetadataSize(&payload)
	if err != nil {
		return payload, fallback
	}
	hybrid := payload
	hybrid.SchemaVersion, hybrid.Mode = content.HybridVoxelObjectPayloadSchemaVersion, content.VoxelObjectPayloadHybridDelta
	hybridSize, err := content.VoxelObjectPayloadMetadataSize(&hybrid)
	if err != nil {
		return payload, fallback
	}
	// Derive JSON field/array overhead from the typed serializer; integer lengths
	// below are exact without repeating owner escaping or scalar JSON rules.
	probe := hybrid
	probe.ReplacementBricks = [][3]int32{{}}
	probeSize, err := content.VoxelObjectPayloadMetadataSize(&probe)
	if err != nil {
		return payload, fallback
	}
	// [0,0,0] occupies seven bytes. Each accumulated selector includes a
	// comma; subtract the first comma from the fixed document overhead.
	overhead := hybridSize - v2Size + probeSize - hybridSize - 7 - 1
	if savings <= overhead+selectorBytes {
		return payload, fallback
	}
	return hybrid, plan
}

func managedVoxelSelectorCompare(a, b [3]int32) int {
	for axis := 0; axis < 3; axis++ {
		if a[axis] < b[axis] {
			return -1
		}
		if a[axis] > b[axis] {
			return 1
		}
	}
	return 0
}
