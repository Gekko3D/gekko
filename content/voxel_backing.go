package content

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	CurrentVoxelBackingSchemaVersion = 1
	VoxelBackingKindPlaneTreeV1      = "plane_tree_v1"

	VoxelBackingOwnerImportedWorld = "imported_world"
	VoxelBackingOwnerTerrain       = "terrain"
)

// VoxelBackingRefDef attaches an optional immutable solid-volume classifier to
// an imported world. Bounds are global voxel coordinates with an exclusive max;
// they are duplicated here so streaming can index backing-only chunks without
// loading the sidecar first.
type VoxelBackingRefDef struct {
	Path       string `json:"path"`
	Kind       string `json:"kind"`
	SourceHash string `json:"source_hash,omitempty"`
	BoundsMin  [3]int `json:"bounds_min"`
	BoundsMax  [3]int `json:"bounds_max"`
}

// VoxelBackingDef is source-neutral authored support data. PlaneTreeV1 is a
// compact point classifier; runtime-generated terrain uses the same provider
// contract without needing a sidecar.
type VoxelBackingDef struct {
	SchemaVersion   int                             `json:"schema_version"`
	Kind            string                          `json:"kind"`
	SourceHash      string                          `json:"source_hash,omitempty"`
	BoundsMin       [3]int                          `json:"bounds_min"`
	BoundsMax       [3]int                          `json:"bounds_max"`
	SolidValue      uint8                           `json:"solid_value"`
	PlaneTree       *VoxelBackingPlaneTreeDef       `json:"plane_tree,omitempty"`
	SurfaceSupports []VoxelBackingSurfaceSupportDef `json:"surface_supports,omitempty"`
}

type VoxelBackingPlaneTreeDef struct {
	Root    int32                        `json:"root"`
	Volumes []VoxelBackingPlaneVolumeDef `json:"volumes,omitempty"`
	Planes  []VoxelBackingPlaneDef       `json:"planes"`
	Nodes   []VoxelBackingPlaneNodeDef   `json:"nodes"`
	Leaves  []VoxelBackingPlaneLeafDef   `json:"leaves"`
}

// VoxelBackingPlaneVolumeDef references one exact solid classifier in the
// shared plane tree. Bounds are global voxel coordinates with an exclusive max.
type VoxelBackingPlaneVolumeDef struct {
	Root      int32  `json:"root"`
	BoundsMin [3]int `json:"bounds_min"`
	BoundsMax [3]int `json:"bounds_max"`
}

type VoxelBackingPlaneDef struct {
	Normal   [3]float32 `json:"normal"`
	Distance float32    `json:"distance"`
}

type VoxelBackingPlaneNodeDef struct {
	Plane    uint32   `json:"plane"`
	Children [2]int32 `json:"children"`
}

type VoxelBackingPlaneLeafDef struct {
	Solid bool `json:"solid"`
}

// VoxelBackingSurfaceSupportDef adds a finite inward solid band behind an
// authored surface. Coordinates and depth are in global voxel units.
type VoxelBackingSurfaceSupportDef struct {
	Vertices [3][3]float32 `json:"vertices"`
	Normal   [3]float32    `json:"normal"`
	Depth    float32       `json:"depth"`
}

// VoxelBackingRemovalDef is a removal-only overlay on an immutable backing.
// Each brick stores one bit per 8x8x8 voxel as 16 uint32 words.
type VoxelBackingRemovalDef struct {
	OwnerKind  string                        `json:"owner_kind"`
	OwnerID    string                        `json:"owner_id"`
	SourceHash string                        `json:"source_hash,omitempty"`
	ChunkCoord TerrainChunkCoordDef          `json:"chunk_coord"`
	Bricks     []VoxelBackingRemovalBrickDef `json:"bricks,omitempty"`
}

type VoxelBackingRemovalBrickDef struct {
	Coord    [3]int     `json:"coord"`
	Bits     [16]uint32 `json:"bits"`
	Material uint8      `json:"material,omitempty"`
}

func SaveVoxelBacking(path string, def *VoxelBackingDef) error {
	if err := ValidateVoxelBacking(def); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadVoxelBacking(path string) (*VoxelBackingDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def VoxelBackingDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if err := ValidateVoxelBacking(&def); err != nil {
		return nil, err
	}
	return &def, nil
}

func ValidateVoxelBacking(def *VoxelBackingDef) error {
	if def == nil {
		return fmt.Errorf("voxel backing is nil")
	}
	if def.SchemaVersion == 0 {
		def.SchemaVersion = CurrentVoxelBackingSchemaVersion
	}
	if def.SchemaVersion != CurrentVoxelBackingSchemaVersion {
		return fmt.Errorf("unsupported voxel backing schema version %d", def.SchemaVersion)
	}
	if def.Kind != VoxelBackingKindPlaneTreeV1 {
		return fmt.Errorf("unsupported voxel backing kind %q", def.Kind)
	}
	for axis := 0; axis < 3; axis++ {
		if def.BoundsMax[axis] <= def.BoundsMin[axis] {
			return fmt.Errorf("voxel backing bounds are invalid")
		}
	}
	if def.SolidValue == 0 {
		return fmt.Errorf("voxel backing solid_value must be non-zero")
	}
	if def.PlaneTree == nil {
		return fmt.Errorf("voxel backing plane_tree is required")
	}
	if len(def.PlaneTree.Leaves) == 0 {
		return fmt.Errorf("voxel backing plane_tree leaves are required")
	}
	if err := validateVoxelBackingChild(def.PlaneTree.Root, len(def.PlaneTree.Nodes), len(def.PlaneTree.Leaves)); err != nil {
		return fmt.Errorf("voxel backing root: %w", err)
	}
	for i, volume := range def.PlaneTree.Volumes {
		if err := validateVoxelBackingChild(volume.Root, len(def.PlaneTree.Nodes), len(def.PlaneTree.Leaves)); err != nil {
			return fmt.Errorf("voxel backing volume %d root: %w", i, err)
		}
		for axis := 0; axis < 3; axis++ {
			if volume.BoundsMax[axis] <= volume.BoundsMin[axis] {
				return fmt.Errorf("voxel backing volume %d bounds are invalid", i)
			}
		}
	}
	for i, node := range def.PlaneTree.Nodes {
		if int(node.Plane) >= len(def.PlaneTree.Planes) {
			return fmt.Errorf("voxel backing node %d plane %d is out of range", i, node.Plane)
		}
		for _, child := range node.Children {
			if err := validateVoxelBackingChild(child, len(def.PlaneTree.Nodes), len(def.PlaneTree.Leaves)); err != nil {
				return fmt.Errorf("voxel backing node %d: %w", i, err)
			}
		}
	}
	for i, support := range def.SurfaceSupports {
		if !voxelBackingFinite(support.Depth) || support.Depth <= 0 {
			return fmt.Errorf("voxel backing surface support %d depth must be positive and finite", i)
		}
		normalLength2 := float32(0)
		for axis, value := range support.Normal {
			if !voxelBackingFinite(value) {
				return fmt.Errorf("voxel backing surface support %d normal axis %d is not finite", i, axis)
			}
			normalLength2 += value * value
		}
		if normalLength2 <= 1e-8 {
			return fmt.Errorf("voxel backing surface support %d normal is degenerate", i)
		}
		for vertex, values := range support.Vertices {
			for axis, value := range values {
				if !voxelBackingFinite(value) {
					return fmt.Errorf("voxel backing surface support %d vertex %d axis %d is not finite", i, vertex, axis)
				}
			}
		}
	}
	return nil
}

func voxelBackingFinite(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func validateVoxelBackingChild(child int32, nodes, leaves int) error {
	if child >= 0 {
		if int(child) >= nodes {
			return fmt.Errorf("node %d is out of range", child)
		}
		return nil
	}
	leaf := int(^child)
	if leaf < 0 || leaf >= leaves {
		return fmt.Errorf("leaf %d is out of range", leaf)
	}
	return nil
}
