package gekko

import (
	"fmt"
	"math"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

const compiledAssetModelRasterizationVersion = "gekko-compiled-model-v1"

// compiledAssetModelSource owns CPU-only compiler data. It retains no registered
// assets or borrowed authoring storage. Original raw samples are consumed before
// primary canonicalization, never used for compiled runtime collapse.
type compiledAssetModelSource struct {
	definition   *content.CompiledAssetModelDef
	palette      VoxelPaletteAsset
	dependencies []string
}

func compiledModelPositiveFinite(value float32) bool {
	return value > 0 && !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}

func compileAssetPartModel(asset *content.AssetDef, part content.AssetPartDef, documentPath string) (*compiledAssetModelSource, error) {
	if !compiledModelPositiveFinite(part.ModelScale) || !compiledModelPositiveFinite(part.VoxelResolution) {
		return nil, fmt.Errorf("compiled model requires finite positive scale and resolution")
	}
	var model VoxModel
	var palette VoxelPaletteAsset
	var dependencies []string
	var err error
	switch part.Source.Kind {
	case content.AssetSourceKindProceduralPrimitive:
		spec, exists := content.ProceduralPrimitiveSpecFor(part.Source.Primitive)
		if !exists {
			return nil, fmt.Errorf("unsupported procedural primitive %q", part.Source.Primitive)
		}
		for _, key := range spec.Params {
			value := part.Source.Params[key]
			if !compiledModelPositiveFinite(value) || !compiledModelPositiveFinite(value*part.ModelScale) {
				return nil, fmt.Errorf("compiled primitive requires finite positive parameter %s", key)
			}
		}
		model, err = buildProceduralPrimitiveModel(part.Source.Primitive, part.Source.Params, part.ModelScale)
		if err != nil {
			return nil, err
		}
		palette, err = buildAuthoredProceduralPalette(asset, part)
	case content.AssetSourceKindVoxModel, content.AssetSourceKindVoxSceneNode:
		sourcePath := content.ResolveDocumentPath(part.Source.Path, documentPath)
		file, loadErr := LoadVoxFile(sourcePath)
		if loadErr != nil {
			return nil, loadErr
		}
		index := part.Source.ModelIndex
		if part.Source.Kind == content.AssetSourceKindVoxSceneNode {
			if err := validateCompiledVoxScene(file); err != nil {
				return nil, err
			}
			resolved, resolveErr := ResolveVoxSceneNodeModel(InspectVoxScene(file, 1.0), part.Source.NodeName, part.Source.ModelIndex)
			if resolveErr != nil {
				return nil, fmt.Errorf("%s (%s): %w", part.Name, part.Source.Path, resolveErr)
			}
			index = resolved.ModelIndex
		}
		if index < 0 || index >= len(file.Models) {
			return nil, fmt.Errorf("%s (%s): model index %d out of range", part.Name, part.Source.Path, index)
		}
		raw := file.Models[index]
		if part.ModelScale < 1 {
			if err := validateCompiledVoxDownscale(raw, part.ModelScale); err != nil {
				return nil, err
			}
		}
		// Palette surface facts belong to original samples, including colors that a
		// later scale may discard. This private file owns its material maps.
		palette, err = buildAuthoredVoxFilePalette(asset, part, file.Palette, file.VoxMaterials, raw, sourcePath)
		if err != nil {
			return nil, err
		}
		model = ScaleVoxModel(raw, part.ModelScale)
		dependencies = []string{sourcePath}
	default:
		return nil, fmt.Errorf("unsupported compiled model source %q", part.Source.Kind)
	}
	if err != nil {
		return nil, err
	}
	bricks, err := compileAssetPrimaryBricks(xBrickMapFromVoxModel(model))
	if err != nil {
		return nil, err
	}
	return &compiledAssetModelSource{
		definition: &content.CompiledAssetModelDef{SchemaVersion: content.CurrentCompiledAssetModelSchemaVersion, Lattice: content.VoxelObjectLatticeDef{VoxelResolution: part.VoxelResolution, RasterizationVersion: compiledAssetModelRasterizationVersion}, Dimensions: [3]uint32{model.SizeX, model.SizeY, model.SizeZ}, Bricks: bricks},
		palette:    palette, dependencies: dependencies,
	}, nil
}

// validateCompiledVoxDownscale rejects only ambiguous highest-vote colors. The
// arithmetic deliberately matches ScaleVoxModel, including zero votes and clamps;
// it does not change that public constructor's historical tie behavior.
func validateCompiledVoxDownscale(model VoxModel, scale float32) error {
	dims := [3]uint32{model.SizeX, model.SizeY, model.SizeZ}
	for i := range dims {
		dims[i] = uint32(math.Round(float64(float32(dims[i]) * scale)))
		if dims[i] == 0 {
			dims[i] = 1
		}
	}
	groups := make(map[[3]uint32]map[byte]int)
	for _, voxel := range model.Voxels {
		coord := [3]uint32{voxel.X, voxel.Y, voxel.Z}
		for i := range coord {
			coord[i] = uint32(float32(coord[i]) * scale)
			if coord[i] >= dims[i] {
				coord[i] = dims[i] - 1
			}
		}
		if groups[coord] == nil {
			groups[coord] = make(map[byte]int)
		}
		groups[coord][voxel.ColorIndex]++
	}
	for _, counts := range groups {
		best, winners := 0, 0
		for _, count := range counts {
			if count > best {
				best, winners = count, 1
			} else if count == best {
				winners++
			}
		}
		if winners > 1 {
			return fmt.Errorf("ambiguous compiled VOX downscale color vote")
		}
	}
	return nil
}

// The public inspector assumes acyclic nodes and in-range shape references.
// Guard only the scene-node adapter; plain model selection ignores scene graphs.
func validateCompiledVoxScene(file *VoxFile) error {
	for _, node := range file.Nodes {
		if node.Type == VoxNodeShape {
			for _, model := range node.Models {
				if model.ModelID < 0 || model.ModelID >= len(file.Models) {
					return fmt.Errorf("compiled VOX scene model index out of range")
				}
			}
		}
	}
	state := make(map[int]uint8, len(file.Nodes))
	var visit func(int) error
	visit = func(id int) error {
		node, exists := file.Nodes[id]
		if !exists || state[id] == 2 {
			return nil
		}
		if state[id] == 1 {
			return fmt.Errorf("compiled VOX scene contains a cycle")
		}
		state[id] = 1
		switch node.Type {
		case VoxNodeTransform:
			if err := visit(node.ChildID); err != nil {
				return err
			}
		case VoxNodeGroup:
			for _, child := range node.ChildrenIDs {
				if err := visit(child); err != nil {
					return err
				}
			}
		}
		state[id] = 2
		return nil
	}
	for id := range file.Nodes {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// compileAssetPrimaryBricks shares the existing inline compiler's literal primary
// packing and portable-coordinate checks. C1 encoding canonicalizes brick order.
func compileAssetPrimaryBricks(geometry *volume.XBrickMap) ([]voxelcodec.Brick, error) {
	var bricks []voxelcodec.Brick
	for sectorCoord, sector := range geometry.Sectors {
		for bz := 0; bz < 4; bz++ {
			for by := 0; by < 4; by++ {
				for bx := 0; bx < 4; bx++ {
					brick := sector.GetBrick(bx, by, bz)
					if brick == nil {
						continue
					}
					var encoded voxelcodec.Brick
					for axis, local := range [3]int{bx, by, bz} {
						// Check sector bounds before multiplying native coordinates.
						if sectorCoord[axis] < -67108864 || sectorCoord[axis] > 67108863 {
							return nil, fmt.Errorf("compiled geometry exceeds portable coordinates")
						}
						coord := int64(sectorCoord[axis])*4 + int64(local)
						if coord < -268435456 || coord > 268435455 {
							return nil, fmt.Errorf("compiled geometry exceeds portable coordinates")
						}
						encoded.Coord[axis] = int32(coord)
					}
					for z := 0; z < 8; z++ {
						for y := 0; y < 8; y++ {
							for x := 0; x < 8; x++ {
								value := brick.VoxelValue(x, y, z)
								if value == 0 {
									continue
								}
								linear := x + 8*y + 64*z
								encoded.Occupancy[linear/64] |= uint64(1) << uint(linear%64)
								encoded.Values = append(encoded.Values, value)
							}
						}
					}
					if len(encoded.Values) > 0 {
						bricks = append(bricks, encoded)
					}
				}
			}
		}
	}
	return bricks, nil
}
