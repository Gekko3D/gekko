package content

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	NavSpanRejectedHeadroom  = "insufficient_headroom"
	NavSpanRejectedClearance = "insufficient_clearance"
)

type NavSpanProfileDiagnostic struct {
	Code string
	Span uint32
}

type NavSpanProfileResult struct {
	Graph       NavGraphTileDef
	Diagnostics []NavSpanProfileDiagnostic
}

type navSpanClearanceInterval struct {
	start int
	end   int
}

const navSpanDistanceInfinity = int64(1 << 60)

// Clearance is the largest conservative cylinder radius that stays outside
// solid and blocker voxels for the span's complete open interval. Spans sharing
// an interval reuse one exact squared-distance field over the center tile and
// its halo.
func calculateNavSpanClearance(input NavSpanBuildInput, chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, spans []NavSpanDef) {
	groups := make(map[navSpanClearanceInterval][]int)
	for i, span := range spans {
		layers := int(math.Ceil(float64(span.Headroom / input.VoxelResolution)))
		interval := navSpanClearanceInterval{start: span.Y, end: span.Y + layers}
		groups[interval] = append(groups[interval], i)
	}
	intervals := make([]navSpanClearanceInterval, 0, len(groups))
	for interval := range groups {
		intervals = append(intervals, interval)
	}
	sort.Slice(intervals, func(i, j int) bool {
		if intervals[i].start != intervals[j].start {
			return intervals[i].start < intervals[j].start
		}
		return intervals[i].end < intervals[j].end
	})

	width := 3*input.ChunkSize + 2
	origin := input.ChunkSize + 1
	field := make([]int64, width*width)
	line := make([]int64, width)
	transformed := make([]int64, width)
	hull := make([]navSpanDistanceLine, width)
	for _, interval := range intervals {
		for z := range width {
			for x := range width {
				index := x + width*z
				if navSpanIntervalColumnObstructed(input, chunks, x-origin, z-origin, interval.start, interval.end) {
					field[index] = 0
				} else {
					field[index] = navSpanDistanceInfinity
				}
			}
		}
		navSpanSquaredIntervalDistanceField(field, width, width, line, transformed, hull)
		for _, spanIndex := range groups[interval] {
			span := &spans[spanIndex]
			squared := field[(span.X+origin)+width*(span.Z+origin)]
			span.ClearanceRadius = float32(math.Sqrt(float64(squared))*0.5) * input.VoxelResolution
		}
	}
}

func navSpanIntervalColumnObstructed(input NavSpanBuildInput, chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, x, z, startY, endY int) bool {
	coord := input.Center.Coord
	coord.X += floorDivNavSpan(x, input.ChunkSize)
	coord.Z += floorDivNavSpan(z, input.ChunkSize)
	localX := positiveModNavSpan(x, input.ChunkSize)
	localZ := positiveModNavSpan(z, input.ChunkSize)
	for y := startY; y < endY; {
		coord.Y = input.Center.Coord.Y + floorDivNavSpan(y, input.ChunkSize)
		chunk, exists := chunks[coord]
		if !exists || !chunk.known {
			return true
		}
		localY := positiveModNavSpan(y, input.ChunkSize)
		count := min(endY-y, input.ChunkSize-localY)
		start := localY + input.ChunkSize*(localX+input.ChunkSize*localZ)
		if navSpanBitsAny(chunk.solid, start, start+count) || navSpanBitsAny(chunk.blocked, start, start+count) {
			return true
		}
		y += count
	}
	return false
}

func navSpanBitsAny(bits []uint64, start, end int) bool {
	if len(bits) == 0 || start >= end {
		return false
	}
	firstWord := start / 64
	lastWord := (end - 1) / 64
	firstMask := ^uint64(0) << uint(start%64)
	lastMask := ^uint64(0) >> uint(63-(end-1)%64)
	if firstWord == lastWord {
		return bits[firstWord]&firstMask&lastMask != 0
	}
	if bits[firstWord]&firstMask != 0 || bits[lastWord]&lastMask != 0 {
		return true
	}
	for word := firstWord + 1; word < lastWord; word++ {
		if bits[word] != 0 {
			return true
		}
	}
	return false
}

// Distances are four times squared voxel distance. Each occupied cell is a
// unit square, so a non-zero axis distance d contributes (2*d-1)^2.
func navSpanSquaredIntervalDistanceField(field []int64, width, height int, line, transformed []int64, hull []navSpanDistanceLine) {
	for z := range height {
		copy(line, field[z*width:(z+1)*width])
		navSpanSquaredIntervalDistanceTransform(line, transformed, hull)
		copy(field[z*width:(z+1)*width], transformed)
	}
	for x := range width {
		for z := range height {
			line[z] = field[x+width*z]
		}
		navSpanSquaredIntervalDistanceTransform(line[:height], transformed[:height], hull[:height])
		for z := range height {
			field[x+width*z] = transformed[z]
		}
	}
}

type navSpanDistanceLine struct {
	slope     int64
	intercept int64
}

func navSpanSquaredIntervalDistanceTransform(source, result []int64, hull []navSpanDistanceLine) {
	for i := range source {
		result[i] = source[i]
	}
	for direction := 0; direction < 2; direction++ {
		head, tail := 0, 0
		for position := range source {
			index := position
			if direction != 0 {
				index = len(source) - 1 - position
			}
			x := int64(2 * position)
			if head < tail {
				for head+1 < tail && navSpanDistanceLineValue(hull[head], x) >= navSpanDistanceLineValue(hull[head+1], x) {
					head++
				}
				result[index] = min(result[index], x*x+navSpanDistanceLineValue(hull[head], x))
			}
			if source[index] >= navSpanDistanceInfinity {
				continue
			}
			center := int64(2*position + 1)
			line := navSpanDistanceLine{slope: -2 * center, intercept: center*center + source[index]}
			for tail-head >= 2 && navSpanDistanceLineRedundant(hull[tail-2], hull[tail-1], line) {
				tail--
			}
			hull[tail] = line
			tail++
		}
	}
}

func navSpanDistanceLineValue(line navSpanDistanceLine, x int64) int64 {
	return line.slope*x + line.intercept
}

func navSpanDistanceLineRedundant(a, b, c navSpanDistanceLine) bool {
	return (b.intercept-a.intercept)*(b.slope-c.slope) >= (c.intercept-b.intercept)*(a.slope-b.slope)
}

// FilterNavSpansForProfile emits only source span IDs supported by profile.
// BuildNavSpanGraph adds transitions to the returned graph shape.
func FilterNavSpansForProfile(source NavSourceTileDef, profile NavAgentProfileDef) (NavSpanProfileResult, error) {
	if validation := ValidateNavSourceTile(&source); validation.HasErrors() {
		return NavSpanProfileResult{}, fmt.Errorf("invalid navigation source tile: %s", validation.Error())
	}
	if strings.TrimSpace(profile.ID) == "" {
		return NavSpanProfileResult{}, fmt.Errorf("navigation agent profile id is required")
	}
	if !finite(profile.Height) || profile.Height <= 0 {
		return NavSpanProfileResult{}, fmt.Errorf("navigation agent height must be finite and positive")
	}
	if !finite(profile.Radius) || profile.Radius <= 0 {
		return NavSpanProfileResult{}, fmt.Errorf("navigation agent radius must be finite and positive")
	}

	result := NavSpanProfileResult{Graph: NavGraphTileDef{
		NavID:          source.NavID,
		SchemaVersion:  CurrentNavGraphTileSchemaVersion,
		Coord:          source.Coord,
		AgentProfileID: profile.ID,
		BuilderVersion: source.BuilderVersion,
		SourceHash:     source.SourceHash,
		DependencyHash: source.DependencyHash,
	}}
	for _, span := range source.Spans {
		switch {
		case span.Headroom < profile.Height:
			result.Diagnostics = append(result.Diagnostics, NavSpanProfileDiagnostic{Code: NavSpanRejectedHeadroom, Span: span.ID})
		case span.ClearanceRadius < profile.Radius:
			result.Diagnostics = append(result.Diagnostics, NavSpanProfileDiagnostic{Code: NavSpanRejectedClearance, Span: span.ID})
		default:
			result.Graph.SpanIDs = append(result.Graph.SpanIDs, span.ID)
		}
	}
	if validation := ValidateNavGraphTile(&result.Graph); validation.HasErrors() {
		return NavSpanProfileResult{}, fmt.Errorf("invalid navigation graph tile: %s", validation.Error())
	}
	return result, nil
}
