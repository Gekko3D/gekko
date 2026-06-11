package content

import "math"

type navBuildChunkReach struct {
	Horizontal int
	Vertical   int
}

func navBuildChunkReachForProfiles(chunkSize int, voxelResolution float32, profiles []NavAgentProfileDef) navBuildChunkReach {
	if chunkSize <= 0 || voxelResolution <= 0 {
		return navBuildChunkReach{}
	}
	if len(profiles) == 0 {
		profiles = []NavAgentProfileDef{DefaultHL1NavAgentProfile()}
	}
	var out navBuildChunkReach
	for _, profile := range profiles {
		reach := navBuildChunkReachForProfile(chunkSize, voxelResolution, profile)
		if reach.Horizontal > out.Horizontal {
			out.Horizontal = reach.Horizontal
		}
		if reach.Vertical > out.Vertical {
			out.Vertical = reach.Vertical
		}
	}
	return out
}

func navBuildChunkReachForProfile(chunkSize int, voxelResolution float32, profile NavAgentProfileDef) navBuildChunkReach {
	if chunkSize <= 0 || voxelResolution <= 0 {
		return navBuildChunkReach{}
	}
	EnsureNavAgentProfileDefaults(&profile)
	horizontalCells := int(math.Ceil(float64(navBuildProfileHorizontalReach(profile) / voxelResolution)))
	verticalCells := int(math.Ceil(float64(navBuildProfileVerticalReach(profile) / voxelResolution)))
	return navBuildChunkReach{
		Horizontal: navBuildCellsToChunkReach(horizontalCells, chunkSize),
		Vertical:   navBuildCellsToChunkReach(verticalCells, chunkSize),
	}
}

func navBuildCellsToChunkReach(cells int, chunkSize int) int {
	if cells <= 0 || chunkSize <= 0 {
		return 0
	}
	return int(math.Ceil(float64(cells) / float64(chunkSize)))
}

func navBuildProfileHorizontalReach(profile NavAgentProfileDef) float32 {
	maxReach := profile.Radius
	if profile.MaxJumpDistance > maxReach {
		maxReach = profile.MaxJumpDistance
	}
	return maxReach
}

func navBuildProfileVerticalReach(profile NavAgentProfileDef) float32 {
	maxReach := profile.Height
	if profile.CrouchHeight > maxReach {
		maxReach = profile.CrouchHeight
	}
	if profile.StepHeight > maxReach {
		maxReach = profile.StepHeight
	}
	if profile.MaxDropHeight > maxReach {
		maxReach = profile.MaxDropHeight
	}
	if profile.MaxJumpUp > maxReach {
		maxReach = profile.MaxJumpUp
	}
	if profile.MaxJumpDown > maxReach {
		maxReach = profile.MaxJumpDown
	}
	return maxReach
}

func navBuildCoordWithinReach(center TerrainChunkCoordDef, coord TerrainChunkCoordDef, reach navBuildChunkReach) bool {
	return absNavCoordDelta(center.X, coord.X) <= reach.Horizontal &&
		absNavCoordDelta(center.Y, coord.Y) <= reach.Vertical &&
		absNavCoordDelta(center.Z, coord.Z) <= reach.Horizontal
}
