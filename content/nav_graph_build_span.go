package content

import (
	"fmt"
	"sort"
)

const (
	NavSpanBuildUnknownOpenInterval   = "unknown_open_interval"
	NavSpanBuildTruncatedOpenInterval = "truncated_open_interval"
)

// NavSpanBuildChunk is effective occupancy for one chunk. Missing voxels in a
// known chunk are empty; missing or explicitly unknown chunks stay unknown.
// SourceHash is required for known halo chunks. BlockedVoxels subtract
// clearance but never provide support.
type NavSpanBuildChunk struct {
	Coord         TerrainChunkCoordDef
	Known         bool
	SourceHash    string
	SolidVoxels   [][3]int
	BlockedVoxels [][3]int
}

type NavSpanBuildInput struct {
	NavID           string
	BuilderVersion  string
	SourceHash      string
	ChunkSize       int
	VoxelResolution float32
	Center          NavSpanBuildChunk
	Halo            []NavSpanBuildChunk
}

type NavSpanBuildDiagnostic struct {
	Code     string
	Rejected bool
	X        int
	Y        int
	Z        int
}

type NavSpanBuildResult struct {
	Source      NavSourceTileDef
	Diagnostics []NavSpanBuildDiagnostic
}

type navSpanBuildOccupancy struct {
	known   bool
	solid   []uint64
	blocked []uint64
}

// BuildNavSourceSpans extracts supported open intervals whose support voxel is
// in Center. Unknown occupancy caps an interval and rejects zero-height spans.
func BuildNavSourceSpans(input NavSpanBuildInput) (NavSpanBuildResult, error) {
	chunks, err := buildNavSpanOccupancy(input)
	if err != nil {
		return NavSpanBuildResult{}, err
	}

	result := NavSpanBuildResult{Source: NavSourceTileDef{
		NavID:          input.NavID,
		SchemaVersion:  CurrentNavSourceTileSchemaVersion,
		Coord:          input.Center.Coord,
		BuilderVersion: input.BuilderVersion,
		SourceHash:     input.SourceHash,
		DependencyHash: navSpanBuildDependencyHash(input),
	}}
	centerSolids := append([][3]int(nil), input.Center.SolidVoxels...)
	sort.Slice(centerSolids, func(i, j int) bool {
		if centerSolids[i][0] != centerSolids[j][0] {
			return centerSolids[i][0] < centerSolids[j][0]
		}
		if centerSolids[i][2] != centerSolids[j][2] {
			return centerSolids[i][2] < centerSolids[j][2]
		}
		return centerSolids[i][1] < centerSolids[j][1]
	})
	for i, voxel := range centerSolids {
		if i > 0 && voxel == centerSolids[i-1] {
			continue
		}
		x, y, z := voxel[0], voxel[1], voxel[2]
		above := sampleNavSpanOccupancy(chunks, input.Center.Coord, input.ChunkSize, x, y+1, z)
		if above == navVoxelUnknown {
			result.Diagnostics = append(result.Diagnostics, NavSpanBuildDiagnostic{Code: NavSpanBuildUnknownOpenInterval, Rejected: true, X: x, Y: y + 1, Z: z})
			continue
		}
		if above == navVoxelSolid {
			continue
		}

		ceilingY := y + 2
		ceilingState := sampleNavSpanOccupancy(chunks, input.Center.Coord, input.ChunkSize, x, ceilingY, z)
		for ceilingState == navVoxelEmpty {
			ceilingY++
			ceilingState = sampleNavSpanOccupancy(chunks, input.Center.Coord, input.ChunkSize, x, ceilingY, z)
		}
		if ceilingState == navVoxelUnknown {
			result.Diagnostics = append(result.Diagnostics, NavSpanBuildDiagnostic{Code: NavSpanBuildTruncatedOpenInterval, X: x, Y: y + 1, Z: z})
		}

		supportHeight := float32((input.Center.Coord.Y*input.ChunkSize)+(y+1)) * input.VoxelResolution
		ceilingHeight := float32((input.Center.Coord.Y*input.ChunkSize)+ceilingY) * input.VoxelResolution
		result.Source.Spans = append(result.Source.Spans, NavSpanDef{
			ID:            uint32(len(result.Source.Spans)),
			X:             x,
			Y:             y + 1,
			Z:             z,
			SupportHeight: supportHeight,
			CeilingHeight: ceilingHeight,
			Headroom:      ceilingHeight - supportHeight,
		})
	}
	calculateNavSpanClearance(input, chunks, result.Source.Spans)
	if validation := ValidateNavSourceTile(&result.Source); validation.HasErrors() {
		return NavSpanBuildResult{}, fmt.Errorf("invalid navigation source tile: %s", validation.Error())
	}
	return result, nil
}

type navVoxelState uint8

const (
	navVoxelUnknown navVoxelState = iota
	navVoxelEmpty
	navVoxelSolid
)

func buildNavSpanOccupancy(input NavSpanBuildInput) (map[TerrainChunkCoordDef]navSpanBuildOccupancy, error) {
	if input.ChunkSize <= 0 {
		return nil, fmt.Errorf("navigation span chunk size must be positive")
	}
	if !finite(input.VoxelResolution) || input.VoxelResolution <= 0 {
		return nil, fmt.Errorf("navigation span voxel resolution must be finite and positive")
	}
	maxInt := int(^uint(0) >> 1)
	if input.ChunkSize > maxInt/input.ChunkSize || input.ChunkSize*input.ChunkSize > (maxInt-63)/input.ChunkSize {
		return nil, fmt.Errorf("navigation span chunk size is too large")
	}
	if !input.Center.Known {
		return nil, fmt.Errorf("navigation span center chunk must be known")
	}

	chunks := make(map[TerrainChunkCoordDef]navSpanBuildOccupancy, len(input.Halo)+1)
	all := append([]NavSpanBuildChunk{input.Center}, input.Halo...)
	for i, chunk := range all {
		if _, exists := chunks[chunk.Coord]; exists {
			return nil, fmt.Errorf("duplicate navigation span chunk %s", TerrainChunkKey(chunk.Coord))
		}
		if i > 0 && (absNavSpanInt(chunk.Coord.X-input.Center.Coord.X) > 1 || absNavSpanInt(chunk.Coord.Y-input.Center.Coord.Y) > 1 || absNavSpanInt(chunk.Coord.Z-input.Center.Coord.Z) > 1) {
			return nil, fmt.Errorf("navigation span halo chunk %s is not adjacent to center", TerrainChunkKey(chunk.Coord))
		}
		if !chunk.Known {
			if chunk.SourceHash != "" || len(chunk.SolidVoxels) != 0 || len(chunk.BlockedVoxels) != 0 {
				return nil, fmt.Errorf("unknown navigation span chunk %s cannot contain source data", TerrainChunkKey(chunk.Coord))
			}
			chunks[chunk.Coord] = navSpanBuildOccupancy{}
			continue
		}
		if i > 0 && chunk.SourceHash == "" {
			return nil, fmt.Errorf("known navigation span halo chunk %s requires source hash", TerrainChunkKey(chunk.Coord))
		}
		occupancy := navSpanBuildOccupancy{known: true}
		cellCount := input.ChunkSize * input.ChunkSize * input.ChunkSize
		if len(chunk.SolidVoxels) > 0 {
			occupancy.solid = make([]uint64, (cellCount+63)/64)
		}
		if len(chunk.BlockedVoxels) > 0 {
			occupancy.blocked = make([]uint64, (cellCount+63)/64)
		}
		for _, voxel := range chunk.SolidVoxels {
			x, y, z := voxel[0], voxel[1], voxel[2]
			if x < 0 || y < 0 || z < 0 || x >= input.ChunkSize || y >= input.ChunkSize || z >= input.ChunkSize {
				return nil, fmt.Errorf("navigation span voxel %v is outside chunk %s", voxel, TerrainChunkKey(chunk.Coord))
			}
			navSpanSetBit(occupancy.solid, y+input.ChunkSize*(x+input.ChunkSize*z))
		}
		for _, voxel := range chunk.BlockedVoxels {
			x, y, z := voxel[0], voxel[1], voxel[2]
			if x < 0 || y < 0 || z < 0 || x >= input.ChunkSize || y >= input.ChunkSize || z >= input.ChunkSize {
				return nil, fmt.Errorf("navigation blocker voxel %v is outside chunk %s", voxel, TerrainChunkKey(chunk.Coord))
			}
			navSpanSetBit(occupancy.blocked, y+input.ChunkSize*(x+input.ChunkSize*z))
		}
		chunks[chunk.Coord] = occupancy
	}
	return chunks, nil
}

func navSpanBuildDependencyHash(input NavSpanBuildInput) string {
	hashes := make(map[TerrainChunkCoordDef]string, len(input.Halo))
	for _, chunk := range input.Halo {
		if chunk.Known {
			hashes[chunk.Coord] = chunk.SourceHash
		}
	}
	return navGraphDependencyHash(input.Center.Coord, func(coord TerrainChunkCoordDef) (string, bool) {
		hash, known := hashes[coord]
		return hash, known
	})
}

func sampleNavSpanOccupancy(chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, center TerrainChunkCoordDef, chunkSize, x, y, z int) navVoxelState {
	chunk, index, known := sampleNavSpanBuildCell(chunks, center, chunkSize, x, y, z)
	if !known {
		return navVoxelUnknown
	}
	if navSpanHasBit(chunk.solid, index) {
		return navVoxelSolid
	}
	return navVoxelEmpty
}

func sampleNavSpanBuildCell(chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, center TerrainChunkCoordDef, chunkSize, x, y, z int) (navSpanBuildOccupancy, int, bool) {
	coord := center
	coord.X += floorDivNavSpan(x, chunkSize)
	coord.Y += floorDivNavSpan(y, chunkSize)
	coord.Z += floorDivNavSpan(z, chunkSize)
	chunk, exists := chunks[coord]
	if !exists || !chunk.known {
		return navSpanBuildOccupancy{}, 0, false
	}
	x = positiveModNavSpan(x, chunkSize)
	y = positiveModNavSpan(y, chunkSize)
	z = positiveModNavSpan(z, chunkSize)
	return chunk, y + chunkSize*(x+chunkSize*z), true
}

func floorDivNavSpan(value, divisor int) int {
	quotient := value / divisor
	if value < 0 && value%divisor != 0 {
		quotient--
	}
	return quotient
}

func positiveModNavSpan(value, divisor int) int {
	result := value % divisor
	if result < 0 {
		result += divisor
	}
	return result
}

func absNavSpanInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func navSpanSetBit(bits []uint64, index int) {
	bits[index/64] |= uint64(1) << uint(index%64)
}

func navSpanHasBit(bits []uint64, index int) bool {
	return len(bits) != 0 && bits[index/64]&(uint64(1)<<uint(index%64)) != 0
}
