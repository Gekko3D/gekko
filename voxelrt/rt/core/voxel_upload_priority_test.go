package core

import "testing"

func TestVoxelUploadPriorityValuesAndOrdinaryObjectDefault(t *testing.T) {
	// Streamed callers persist these five scheduling values across the bridge.
	priorities := []uint8{VoxelUploadPriorityFallback, VoxelUploadPriorityCollision, VoxelUploadPriorityVisible, VoxelUploadPriorityPrefetch, VoxelUploadPriorityKeep}
	for i, priority := range priorities {
		if priority != uint8(i) {
			t.Fatalf("priority index %d = %d, want %d", i, priority, i)
		}
	}
	if obj := NewVoxelObject(); obj.VoxelUploadPriority != VoxelUploadPriorityVisible {
		t.Fatalf("ordinary object priority %d, want visible", obj.VoxelUploadPriority)
	}
}
