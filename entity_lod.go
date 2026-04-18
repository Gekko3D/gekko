package gekko

import (
	"fmt"
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type EntityLODRepresentation uint32

const (
	EntityLODRepresentationFullVoxel EntityLODRepresentation = iota
	EntityLODRepresentationSimplifiedVoxel
	EntityLODRepresentationImpostor
	EntityLODRepresentationDot
)

func (r EntityLODRepresentation) String() string {
	switch r {
	case EntityLODRepresentationFullVoxel:
		return "full_voxel"
	case EntityLODRepresentationSimplifiedVoxel:
		return "simplified_voxel"
	case EntityLODRepresentationImpostor:
		return "impostor"
	case EntityLODRepresentationDot:
		return "dot"
	default:
		return "unknown"
	}
}

type EntityLODDistanceMetric uint32

const (
	EntityLODDistanceMetricOrigin EntityLODDistanceMetric = iota
)

func (m EntityLODDistanceMetric) String() string {
	switch m {
	case EntityLODDistanceMetricOrigin:
		return "origin"
	default:
		return "unknown"
	}
}

type EntityLODBand struct {
	MaxDistance    float32
	Representation EntityLODRepresentation
}

type EntityLODSelection struct {
	Distance       float32
	BandIndex      int
	MaxDistance    float32
	Representation EntityLODRepresentation
}

// EntityLODComponent defines the engine-side LOD contract for an entity.
// Bands must be sorted by ascending MaxDistance, and the final band must be
// unbounded by setting MaxDistance to 0.
type EntityLODComponent struct {
	Disabled       bool
	DistanceMetric EntityLODDistanceMetric
	Bands          []EntityLODBand

	SelectionValid       bool
	ActiveDistance       float32
	ActiveBandIndex      int
	ActiveMaxDistance    float32
	ActiveRepresentation EntityLODRepresentation
}

func (c *EntityLODComponent) Enabled() bool {
	return c != nil && !c.Disabled
}

func (c *EntityLODComponent) NormalizedDistanceMetric() EntityLODDistanceMetric {
	if c == nil {
		return EntityLODDistanceMetricOrigin
	}
	switch c.DistanceMetric {
	case EntityLODDistanceMetricOrigin:
		return c.DistanceMetric
	default:
		return EntityLODDistanceMetricOrigin
	}
}

func (c *EntityLODComponent) ClearRuntimeSelection() {
	if c == nil {
		return
	}
	c.SelectionValid = false
	c.ActiveDistance = 0
	c.ActiveBandIndex = -1
	c.ActiveMaxDistance = 0
	c.ActiveRepresentation = EntityLODRepresentationFullVoxel
}

func (c *EntityLODComponent) ApplySelection(selection EntityLODSelection) {
	if c == nil {
		return
	}
	c.SelectionValid = true
	c.ActiveDistance = selection.Distance
	c.ActiveBandIndex = selection.BandIndex
	c.ActiveMaxDistance = selection.MaxDistance
	c.ActiveRepresentation = selection.Representation
}

func (c *EntityLODComponent) Validate() error {
	if c == nil {
		return fmt.Errorf("entity LOD component is nil")
	}
	if len(c.Bands) == 0 {
		return fmt.Errorf("entity LOD requires at least one distance band")
	}

	prevMax := float32(-1)
	for i, band := range c.Bands {
		switch band.Representation {
		case EntityLODRepresentationFullVoxel,
			EntityLODRepresentationSimplifiedVoxel,
			EntityLODRepresentationImpostor,
			EntityLODRepresentationDot:
		default:
			return fmt.Errorf("entity LOD band %d has unsupported representation %d", i, band.Representation)
		}

		last := i == len(c.Bands)-1
		if !last && band.MaxDistance <= 0 {
			return fmt.Errorf("entity LOD band %d must have a positive max distance", i)
		}
		if band.MaxDistance < 0 {
			return fmt.Errorf("entity LOD band %d max distance must be >= 0", i)
		}
		if !last && band.MaxDistance <= prevMax {
			return fmt.Errorf("entity LOD bands must be strictly increasing")
		}
		if last && band.MaxDistance > 0 && band.MaxDistance <= prevMax {
			return fmt.Errorf("entity LOD bands must be strictly increasing")
		}
		prevMax = band.MaxDistance
	}

	if c.Bands[len(c.Bands)-1].MaxDistance > 0 {
		return fmt.Errorf("final entity LOD band must be unbounded (MaxDistance == 0)")
	}
	return nil
}

func SelectEntityLODByDistance(component *EntityLODComponent, distance float32) (EntityLODSelection, error) {
	if err := component.Validate(); err != nil {
		return EntityLODSelection{}, err
	}
	if distance < 0 {
		distance = 0
	}

	lastIndex := len(component.Bands) - 1
	for i, band := range component.Bands {
		maxDistance := band.MaxDistance
		if i == lastIndex && maxDistance == 0 {
			maxDistance = float32(math.Inf(1))
		}
		if distance <= maxDistance {
			return EntityLODSelection{
				Distance:       distance,
				BandIndex:      i,
				MaxDistance:    band.MaxDistance,
				Representation: band.Representation,
			}, nil
		}
	}

	last := component.Bands[lastIndex]
	return EntityLODSelection{
		Distance:       distance,
		BandIndex:      lastIndex,
		MaxDistance:    last.MaxDistance,
		Representation: last.Representation,
	}, nil
}

func SelectEntityLOD(cameraPosition mgl32.Vec3, transform *TransformComponent, component *EntityLODComponent) (EntityLODSelection, error) {
	if transform == nil {
		return EntityLODSelection{}, fmt.Errorf("entity LOD requires a transform")
	}
	switch component.NormalizedDistanceMetric() {
	case EntityLODDistanceMetricOrigin:
		return SelectEntityLODByDistance(component, transform.Position.Sub(cameraPosition).Len())
	default:
		return SelectEntityLODByDistance(component, transform.Position.Sub(cameraPosition).Len())
	}
}

func entityLODComponentForEntity(cmd *Commands, eid EntityId) (EntityLODComponent, bool) {
	if cmd == nil {
		return EntityLODComponent{}, false
	}
	for _, comp := range cmd.GetAllComponents(eid) {
		switch typed := comp.(type) {
		case *EntityLODComponent:
			return *typed, true
		case EntityLODComponent:
			return typed, true
		}
	}
	return EntityLODComponent{}, false
}
