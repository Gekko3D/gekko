package gekko

import (
	"sort"

	gpu_rt "github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/go-gl/mathgl/mgl32"
)

type DebrisMidfieldCellRecord struct {
	BandID               string
	CellID               string
	RadialIndex          int
	AngularIndex         int
	VerticalIndex        int
	PositionViewSpace    [3]float32
	PlaneNormalViewSpace [3]float32
	InnerRadiusMeters    float32
	OuterRadiusMeters    float32
	Seed                 uint32
	Tint                 [3]float32
	Opacity              float32
	DensityScale         float32
	ApproachFade         float32
	DistanceMeters       float32
	GapInnerRadius       float32
	GapOuterRadius       float32
	LightDirViewSpace    [3]float32
	ActiveHandoff        bool
	HandoffExact         bool
	HandoffRadiusMeters  float32
	AsteroidID           string
}

type DebrisMidfieldVisualComponent struct {
	Disabled bool
	Cells    []DebrisMidfieldCellRecord
}

func (c *DebrisMidfieldVisualComponent) Enabled() bool {
	return c != nil && !c.Disabled
}

type debrisMidfieldCandidate struct {
	entityID EntityId
	record   DebrisMidfieldCellRecord
}

func buildDebrisMidfieldHosts(cmd *Commands) []gpu_rt.DebrisMidfieldHost {
	if cmd == nil {
		return nil
	}
	candidates := make([]debrisMidfieldCandidate, 0)
	MakeQuery1[DebrisMidfieldVisualComponent](cmd).Map(func(eid EntityId, component *DebrisMidfieldVisualComponent) bool {
		if !component.Enabled() {
			return true
		}
		for _, cell := range component.Cells {
			if debrisMidfieldRecordRenderable(cell) {
				candidates = append(candidates, debrisMidfieldCandidate{entityID: eid, record: cell})
			}
		}
		return true
	})
	sortDebrisMidfieldCandidates(candidates)
	if len(candidates) > gpu_rt.MaxDebrisMidfieldCells {
		candidates = candidates[:gpu_rt.MaxDebrisMidfieldCells]
	}
	hosts := make([]gpu_rt.DebrisMidfieldHost, 0, len(candidates))
	seenCells := map[string]struct{}{}
	for _, candidate := range candidates {
		cell := candidate.record
		identity := cell.CellID
		if cell.AsteroidID != "" {
			identity = cell.AsteroidID
		}
		if _, exists := seenCells[identity]; exists {
			continue
		}
		seenCells[identity] = struct{}{}
		hosts = append(hosts, gpu_rt.DebrisMidfieldHost{
			BandID:               cell.BandID,
			CellID:               cell.CellID,
			AsteroidID:           cell.AsteroidID,
			RadialIndex:          int32(cell.RadialIndex),
			AngularIndex:         int32(cell.AngularIndex),
			VerticalIndex:        int32(cell.VerticalIndex),
			PositionViewSpace:    mgl32.Vec3{cell.PositionViewSpace[0], cell.PositionViewSpace[1], cell.PositionViewSpace[2]},
			PlaneNormalViewSpace: normalizedOr(cell.PlaneNormalViewSpace, mgl32.Vec3{0, 1, 0}),
			InnerRadiusMeters:    cell.InnerRadiusMeters,
			OuterRadiusMeters:    cell.OuterRadiusMeters,
			Seed:                 cell.Seed,
			Tint:                 cell.Tint,
			Opacity:              cell.Opacity,
			DensityScale:         cell.DensityScale,
			ApproachFade:         cell.ApproachFade,
			DistanceMeters:       cell.DistanceMeters,
			GapInnerRadius:       cell.GapInnerRadius,
			GapOuterRadius:       cell.GapOuterRadius,
			LightDirViewSpace:    normalizedOr(cell.LightDirViewSpace, mgl32.Vec3{0, 0, 1}),
			ActiveHandoff:        cell.ActiveHandoff,
			HandoffExact:         cell.HandoffExact,
			HandoffRadiusMeters:  cell.HandoffRadiusMeters,
		})
	}
	return hosts
}

func sortDebrisMidfieldCandidates(candidates []debrisMidfieldCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a := candidates[i].record
		b := candidates[j].record
		if a.HandoffExact != b.HandoffExact {
			return a.HandoffExact
		}
		if a.ActiveHandoff != b.ActiveHandoff {
			return a.ActiveHandoff
		}
		if a.Opacity != b.Opacity {
			return a.Opacity > b.Opacity
		}
		if a.ApproachFade != b.ApproachFade {
			return a.ApproachFade > b.ApproachFade
		}
		if a.DistanceMeters != b.DistanceMeters {
			return a.DistanceMeters < b.DistanceMeters
		}
		if a.BandID != b.BandID {
			return a.BandID < b.BandID
		}
		if a.RadialIndex != b.RadialIndex {
			return a.RadialIndex < b.RadialIndex
		}
		if a.AngularIndex != b.AngularIndex {
			return a.AngularIndex < b.AngularIndex
		}
		if a.VerticalIndex != b.VerticalIndex {
			return a.VerticalIndex < b.VerticalIndex
		}
		if a.CellID != b.CellID {
			return a.CellID < b.CellID
		}
		return candidates[i].entityID < candidates[j].entityID
	})
}

func debrisMidfieldRecordRenderable(record DebrisMidfieldCellRecord) bool {
	if record.BandID == "" || record.CellID == "" {
		return false
	}
	if record.InnerRadiusMeters < 0 || record.OuterRadiusMeters <= record.InnerRadiusMeters {
		return false
	}
	if record.Opacity < 0 || record.DensityScale < 0 || record.ApproachFade < 0 {
		return false
	}
	if record.DensityScale > 1 || record.ApproachFade > 1 {
		return false
	}
	if !isFiniteFloat32(record.Opacity) ||
		!isFiniteFloat32(record.DensityScale) ||
		!isFiniteFloat32(record.ApproachFade) ||
		!isFiniteFloat32(record.DistanceMeters) ||
		!isFiniteFloat32(record.HandoffRadiusMeters) ||
		!isFiniteFloat32(record.InnerRadiusMeters) ||
		!isFiniteFloat32(record.OuterRadiusMeters) ||
		!isFiniteFloat32(record.GapInnerRadius) ||
		!isFiniteFloat32(record.GapOuterRadius) {
		return false
	}
	if record.HandoffExact && record.HandoffRadiusMeters <= 0 {
		return false
	}
	for _, values := range [][]float32{
		record.PositionViewSpace[:],
		record.PlaneNormalViewSpace[:],
		record.Tint[:],
		record.LightDirViewSpace[:],
	} {
		for _, value := range values {
			if !isFiniteFloat32(value) {
				return false
			}
		}
	}
	return vectorLenSqr(record.PlaneNormalViewSpace) > 1e-8 &&
		vectorLenSqr(record.LightDirViewSpace) > 1e-8
}
