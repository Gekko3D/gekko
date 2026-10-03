package content

import (
	"fmt"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func voxelObjectSelectorLess(a, b [3]int32) bool {
	for i := 0; i < 3; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func validateVoxelObjectSelectors(selectors [][3]int32) error {
	for i, key := range selectors {
		for _, v := range key {
			if v < -268435456 || v > 268435455 {
				return fmt.Errorf("replacement brick cube outside signed int32")
			}
		}
		if i > 0 && !voxelObjectSelectorLess(selectors[i-1], key) {
			return fmt.Errorf("noncanonical replacement selectors")
		}
	}
	return nil
}

func voxelObjectSelectorSet(selectors [][3]int32) map[[3]int32]struct{} {
	selected := make(map[[3]int32]struct{}, len(selectors))
	for _, key := range selectors {
		selected[key] = struct{}{}
	}
	return selected
}

func voxelObjectHybridBricks(records []VoxelObjectVoxelDef, selectors [][3]int32) ([]voxelcodec.Brick, error) {
	selected := voxelObjectSelectorSet(selectors)
	var replacements, assignments []VoxelObjectVoxelDef
	for _, v := range records {
		if err := validateVoxelObjectCoordinate(v); err != nil {
			return nil, err
		}
		bx, _ := voxelObjectBrickCoordinate(v.X)
		by, _ := voxelObjectBrickCoordinate(v.Y)
		bz, _ := voxelObjectBrickCoordinate(v.Z)
		if _, ok := selected[[3]int32{bx, by, bz}]; ok {
			replacements = append(replacements, v)
		} else {
			assignments = append(assignments, v)
		}
	}
	full, err := voxelObjectBricks(replacements, false)
	if err != nil {
		return nil, err
	}
	delta, err := voxelObjectBricks(assignments, true)
	if err != nil {
		return nil, err
	}
	return append(full, delta...), nil
}

// Empty replacement selectors still consume logical brick ownership and bounds.
func validateVoxelObjectSelectorProfile(payload *VoxelObjectPayloadDef, doc voxelcodec.Document, limits voxelcodec.Limits) error {
	if payload == nil || payload.SchemaVersion != HybridVoxelObjectPayloadSchemaVersion {
		return nil
	}
	keys := voxelObjectSelectorSet(payload.ReplacementBricks)
	for _, brick := range doc.Bricks {
		keys[brick.Coord] = struct{}{}
	}
	if len(keys) > limits.MaxBricks {
		return fmt.Errorf("hybrid logical brick union exceeds profile")
	}
	if limits.Bounds != nil {
		for key := range keys {
			for i := 0; i < 3; i++ {
				if key[i] < limits.Bounds.Min[i] || key[i] > limits.Bounds.Max[i] {
					return fmt.Errorf("hybrid selector outside profile bounds")
				}
			}
		}
	}
	return nil
}
