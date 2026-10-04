package gekko

import (
	"math/bits"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// compiledAssetPrimaryGeometryMatches compares raw primary storage against an
// immutable, independently owned verified baseline. Corresponding alias checks
// are defensive only: they do not establish independence across other positions.
// Publication must establish that ownership before calling this guard.
func compiledAssetPrimaryGeometryMatches(current, verifiedBaseline *volume.XBrickMap) bool {
	if current == nil || verifiedBaseline == nil || current == verifiedBaseline ||
		current.GPUEditMode || verifiedBaseline.GPUEditMode ||
		len(current.Sectors) == 0 || len(current.Sectors) != len(verifiedBaseline.Sectors) {
		return false
	}

	hasOccupiedBrick := false
	for key, sector := range current.Sectors {
		baselineSector := verifiedBaseline.Sectors[key]
		if sector == nil || baselineSector == nil || sector == baselineSector ||
			sector.Coords != key || baselineSector.Coords != key ||
			sector.BrickMask64 != baselineSector.BrickMask64 ||
			len(sector.PackedBricks) != bits.OnesCount64(sector.BrickMask64) ||
			len(baselineSector.PackedBricks) != bits.OnesCount64(baselineSector.BrickMask64) {
			return false
		}

		for i, brick := range sector.PackedBricks {
			baselineBrick := baselineSector.PackedBricks[i]
			if brick == nil || baselineBrick == nil || brick == baselineBrick ||
				brick.OccupancyMask64 != baselineBrick.OccupancyMask64 ||
				brick.Flags != baselineBrick.Flags || brick.Payload != baselineBrick.Payload {
				return false
			}
			if brick.OccupancyMask64 != 0 {
				hasOccupiedBrick = true
			}
		}
	}
	return hasOccupiedBrick
}

// compiledAssetLODMaterialEligible checks only the currently used palette value.
// Exact zero comparisons reject nonfinite values without normalizing signed zero.
func compiledAssetLODMaterialEligible(materials []core.Material, value uint8, animatedUsedValue bool) bool {
	if value == 0 || int(value) >= len(materials) || animatedUsedValue {
		return false
	}
	material := materials[value]
	return material.BaseColor[3] == 255 && material.Transparency == 0 && material.Transmission == 0
}
