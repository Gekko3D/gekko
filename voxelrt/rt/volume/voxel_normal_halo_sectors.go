package volume

// VisitVoxelNormalHaloSectors visits the target sector first, then the fitted
// normal halo, without duplicates or allocated storage. False stops traversal.
// Native integer halo arithmetic may wrap; the target is always mandatory.
func VisitVoxelNormalHaloSectors(x, y, z int, visit func([3]int) bool) {
	if visit == nil {
		return
	}
	target, _ := sectorBrickKeyForVoxel(x, y, z)
	if !visit(target) {
		return
	}
	minKey, _ := sectorBrickKeyForVoxel(x-VoxelNormalExtendedSurfaceFitRadius, y-VoxelNormalExtendedSurfaceFitRadius, z-VoxelNormalExtendedSurfaceFitRadius)
	maxKey, _ := sectorBrickKeyForVoxel(x+VoxelNormalExtendedSurfaceFitRadius, y+VoxelNormalExtendedSurfaceFitRadius, z+VoxelNormalExtendedSurfaceFitRadius)
	for sx := minKey[0]; sx <= maxKey[0]; sx++ {
		for sy := minKey[1]; sy <= maxKey[1]; sy++ {
			for sz := minKey[2]; sz <= maxKey[2]; sz++ {
				key := [3]int{sx, sy, sz}
				if key != target && !visit(key) {
					return
				}
			}
		}
	}
}
