package content

import (
	"encoding/json"
	"fmt"
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
	SchemaVersion int                       `json:"schema_version"`
	Kind          string                    `json:"kind"`
	SourceHash    string                    `json:"source_hash,omitempty"`
	BoundsMin     [3]int                    `json:"bounds_min"`
	BoundsMax     [3]int                    `json:"bounds_max"`
	SolidValue    uint8                     `json:"solid_value"`
	PlaneTree     *VoxelBackingPlaneTreeDef `json:"plane_tree,omitempty"`
}

type VoxelBackingPlaneTreeDef struct {
	Root   int32                      `json:"root"`
	Planes []VoxelBackingPlaneDef     `json:"planes"`
	Nodes  []VoxelBackingPlaneNodeDef `json:"nodes"`
	Leaves []VoxelBackingPlaneLeafDef `json:"leaves"`
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
	Coord [3]int     `json:"coord"`
	Bits  [16]uint32 `json:"bits"`
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
	return nil
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
