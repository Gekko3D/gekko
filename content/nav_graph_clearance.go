package content

import (
	"fmt"
	"math"
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

// Clearance is the largest conservative cylinder radius that stays outside
// solid and blocker voxels for the span's complete open interval.
func calculateNavSpanClearance(input NavSpanBuildInput, chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, spans []NavSpanDef) {
	for i := range spans {
		spans[i].ClearanceRadius = navSpanClearanceRadius(input, chunks, spans[i])
	}
}

func navSpanClearanceRadius(input NavSpanBuildInput, chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, span NavSpanDef) float32 {
	best := float32(math.Inf(1))
	for ring := 0; ; ring++ {
		for dx := -ring; dx <= ring; dx++ {
			for dz := -ring; dz <= ring; dz++ {
				if max(absNavSpanInt(dx), absNavSpanInt(dz)) != ring || !navSpanColumnObstructed(input, chunks, span, dx, dz) {
					continue
				}
				xDistance := max(float32(absNavSpanInt(dx))-0.5, 0) * input.VoxelResolution
				zDistance := max(float32(absNavSpanInt(dz))-0.5, 0) * input.VoxelResolution
				best = min(best, float32(math.Hypot(float64(xDistance), float64(zDistance))))
			}
		}
		if best <= (float32(ring)+0.5)*input.VoxelResolution {
			return best
		}
	}
}

func navSpanColumnObstructed(input NavSpanBuildInput, chunks map[TerrainChunkCoordDef]navSpanBuildOccupancy, span NavSpanDef, dx, dz int) bool {
	layers := int(math.Ceil(float64(span.Headroom / input.VoxelResolution)))
	for y := span.Y; y < span.Y+layers; y++ {
		chunk, index, known := sampleNavSpanBuildCell(chunks, input.Center.Coord, input.ChunkSize, span.X+dx, y, span.Z+dz)
		if !known || chunk.solid[index] || chunk.blocked[index] {
			return true
		}
	}
	return false
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
