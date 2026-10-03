package volume

func (m *ManagedXBrickMap) addChangedBrickAssignment(key [6]int) {
	if m.changedBrickCounts == nil {
		m.changedBrickCounts = make(map[[6]int]int)
	}
	m.changedBrickCounts[key]++
}

// VisitChangedBricks visits unordered changed-brick inventory without allocation
// or reading geometry. Keys contain sector XYZ followed by local brick XYZ (0..3).
// Zero-current bricks retain their final removal history.
// False stops traversal; the result still reports sealed availability. Callbacks
// may call the two target read visitors, but must not mutate, expose, Fork,
// or recursively visit inventory; target callbacks must not reenter the owner.
func (m *ManagedXBrickMap) VisitChangedBricks(visit func(key [6]int, assignmentCount, currentVoxelCount int) bool) bool {
	if m.base == nil {
		return false
	}
	for key, count := range m.changedBrickCounts {
		if !visit(key, count, m.brickVoxelCounts[key]) {
			break
		}
	}
	return true
}

// Validate the entire native-coordinate sector cube before multiplication.
func managedBrickKeyOrigin(key [6]int) ([3]int, bool) {
	maxInt := int(^uint(0) >> 1)
	minInt := -maxInt - 1
	for axis := 0; axis < 3; axis++ {
		if key[axis] < minInt/SectorSize || key[axis] > maxInt/SectorSize || key[axis+3] < 0 || key[axis+3] >= SectorSize/BrickSize {
			return [3]int{}, false
		}
	}
	return [3]int{key[0]*SectorSize + key[3]*BrickSize, key[1]*SectorSize + key[4]*BrickSize, key[2]*SectorSize + key[5]*BrickSize}, true
}

// VisitChangedBrickAssignments reads at most the target's 512 local cells.
// Final zero removals are included. Invalid or absent keys emit no callbacks;
// false stops traversal without changing the sealed-availability result.
// Callbacks must not mutate, expose, or reenter the single owner.
func (m *ManagedXBrickMap) VisitChangedBrickAssignments(key [6]int, visit func(local [3]int, value uint8) bool) bool {
	if m.base == nil {
		return false
	}
	origin, valid := managedBrickKeyOrigin(key)
	if !valid || m.changedBrickCounts[key] == 0 {
		return true
	}
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				local := [3]int{x, y, z}
				value, exists := m.changes[[3]int{origin[0] + x, origin[1] + y, origin[2] + z}]
				if exists && !visit(local, value) {
					return true
				}
			}
		}
	}
	return true
}

// VisitCurrentBrickVoxels reads actual nonzero primary values from at most the
// target's 512 local cells, including unchanged targets. Invalid or absent keys
// emit no callbacks. False stops traversal while preserving availability.
// Callbacks must not mutate, expose, or reenter the single owner.
func (m *ManagedXBrickMap) VisitCurrentBrickVoxels(key [6]int, visit func(local [3]int, value uint8) bool) bool {
	if m.base == nil {
		return false
	}
	if _, valid := managedBrickKeyOrigin(key); !valid {
		return true
	}
	sector := m.current.Sectors[[3]int{key[0], key[1], key[2]}]
	if sector == nil {
		return true
	}
	brick := sector.GetBrick(key[3], key[4], key[5])
	if brick == nil {
		return true
	}
	for z := 0; z < BrickSize; z++ {
		for y := 0; y < BrickSize; y++ {
			for x := 0; x < BrickSize; x++ {
				value := brick.VoxelValue(x, y, z)
				if value != 0 && !visit([3]int{x, y, z}, value) {
					return true
				}
			}
		}
	}
	return true
}
