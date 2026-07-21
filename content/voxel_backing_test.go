package content

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestVoxelBackingRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.gkvoxelbacking")
	def := &VoxelBackingDef{
		Kind:       VoxelBackingKindPlaneTreeV1,
		SourceHash: "source",
		BoundsMin:  [3]int{-8, 0, -8},
		BoundsMax:  [3]int{8, 16, 8},
		SolidValue: 4,
		PlaneTree: &VoxelBackingPlaneTreeDef{
			Root:   0,
			Planes: []VoxelBackingPlaneDef{{Normal: [3]float32{0, 1, 0}, Distance: 2}},
			Nodes:  []VoxelBackingPlaneNodeDef{{Plane: 0, Children: [2]int32{-1, -2}}},
			Leaves: []VoxelBackingPlaneLeafDef{{Solid: true}, {Solid: false}},
		},
	}
	if err := SaveVoxelBacking(path, def); err != nil {
		t.Fatalf("SaveVoxelBacking failed: %v", err)
	}
	loaded, err := LoadVoxelBacking(path)
	if err != nil {
		t.Fatalf("LoadVoxelBacking failed: %v", err)
	}
	if !reflect.DeepEqual(def, loaded) {
		t.Fatalf("voxel backing round trip mismatch: want=%+v got=%+v", def, loaded)
	}
}
