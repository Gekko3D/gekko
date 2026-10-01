package core

// Voxel upload priorities are shared by ordinary objects and streamed residents.
// Lower values receive service first; outstanding work gains priority with age.
const (
	VoxelUploadPriorityFallback uint8 = iota
	VoxelUploadPriorityCollision
	VoxelUploadPriorityVisible
	VoxelUploadPriorityPrefetch
	VoxelUploadPriorityKeep
)
