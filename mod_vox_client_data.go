package gekko

import (
	"fmt"

	"github.com/go-gl/mathgl/mgl32"
)

func createVoxelUniforms(cmd *Commands, server *AssetServer, rState *voxelRenderState) {
	//creates model and palette uniforms for new voxel entities
	seen := map[EntityId]bool{}
	MakeQuery1[TransformComponent](cmd).Map(
		func(entityId EntityId, transform *TransformComponent) bool {
			seen[entityId] = true
			if _, ok := rState.entityVoxInstanceIds[entityId]; !ok {
				//vox instance is not loaded yet, loading
				voxAsset, paletteAssetId, paletteAsset := findVoxelModelAsset(entityId, cmd, server)
				if nil != voxAsset {
					//new vox instance goes to the last index
					voxInstanceId := len(rState.transformsUniforms)
					modelMx := buildModelMatrix(transform)
					rState.transformsUniforms = append(rState.transformsUniforms, transformsUniform{
						ModelMx:    modelMx,
						InvModelMx: modelMx.Inv(),
					})
					//creates or gets palette id
					paletteId := createOrGetPaletteIndex(paletteAssetId, paletteAsset, rState)
					//pushes model voxels into voxels pool
					macroGrid := createMacroGrid(voxAsset, rState)

					rState.voxelInstancesUniform = append(rState.voxelInstancesUniform, voxelInstanceUniform{
						Size: [3]uint32{
							voxAsset.VoxModel.SizeX,
							voxAsset.VoxModel.SizeY,
							voxAsset.VoxModel.SizeZ,
						},
						PaletteId: paletteId,
						MacroGrid: macroGrid,
					})
					// compute and store world-space AABB for this instance (matches macro grid bounds)
					localMin := mgl32.Vec3{0, 0, 0}
					localMax := mgl32.Vec3{
						float32(macroGrid.Size[0] * macroGrid.BrickSize[0]),
						float32(macroGrid.Size[1] * macroGrid.BrickSize[1]),
						float32(macroGrid.Size[2] * macroGrid.BrickSize[2]),
					}
					minV, maxV := computeWorldAABB(modelMx, localMin, localMax)
					rState.instanceAABBsUniform = append(rState.instanceAABBsUniform, aabbUniform{
						Min: mgl32.Vec4{minV.X(), minV.Y(), minV.Z(), 0},
						Max: mgl32.Vec4{maxV.X(), maxV.Y(), maxV.Z(), 0},
					})

					rState.entityVoxInstanceIds[entityId] = voxInstanceId
					rState.instanceIdToEntity[voxInstanceId] = entityId
					rState.paletteIds[paletteAssetId] = paletteId

					rState.isVoxelPoolUpdated = true
				}
			}
			return true
		})
	// Remove instances for entities no longer present
	var toRemove []EntityId
	for e := range rState.entityVoxInstanceIds {
		if !seen[e] {
			toRemove = append(toRemove, e)
		}
	}
	for _, e := range toRemove {
		instId := rState.entityVoxInstanceIds[e]
		removeVoxelInstance(e, instId, rState)
	}
}

// macroGrid - grid of voxel bricks.
// each non-empty macroGrid element points to bricks array element
// each brick points to voxel pool offset
/*
	removeVoxelInstance removes a voxel instance by swapping with the last element to keep
	related arrays dense and updates entity/index mappings accordingly. It also marks
	voxel instance data dirty so GPU buffers get updated next frame.
*/
func removeVoxelInstance(entityId EntityId, instId int, rState *voxelRenderState) {
	last := len(rState.transformsUniforms) - 1
	if instId < 0 || instId > last {
		return
	}

	if instId != last {
		// move last into instId position
		rState.transformsUniforms[instId] = rState.transformsUniforms[last]
		rState.voxelInstancesUniform[instId] = rState.voxelInstancesUniform[last]
		rState.instanceAABBsUniform[instId] = rState.instanceAABBsUniform[last]

		// remap swapped entity index
		if lastEntity, ok := rState.instanceIdToEntity[last]; ok {
			rState.entityVoxInstanceIds[lastEntity] = instId
			rState.instanceIdToEntity[instId] = lastEntity
			delete(rState.instanceIdToEntity, last)
		}
	} else {
		// removing last element, just drop reverse mapping
		delete(rState.instanceIdToEntity, last)
	}

	// shrink slices
	rState.transformsUniforms = rState.transformsUniforms[:last]
	rState.voxelInstancesUniform = rState.voxelInstancesUniform[:last]
	rState.instanceAABBsUniform = rState.instanceAABBsUniform[:last]

	// drop mapping for removed entity
	delete(rState.entityVoxInstanceIds, entityId)

	// ensure GPU instance buffer is updated (due to swap/removal)
	rState.isVoxelPoolUpdated = true
}

func createMacroGrid(voxModelAsset *VoxelModelAsset, rState *voxelRenderState) macroGridUniform {
	// Calculate macro grid dimensions
	brickSizeX := voxModelAsset.BrickSize[0]
	brickSizeY := voxModelAsset.BrickSize[1]
	brickSizeZ := voxModelAsset.BrickSize[2]
	// ceil-div without float to avoid integer truncation bugs
	macroSizeX := (voxModelAsset.VoxModel.SizeX + brickSizeX - 1) / brickSizeX
	macroSizeY := (voxModelAsset.VoxModel.SizeY + brickSizeY - 1) / brickSizeY
	macroSizeZ := (voxModelAsset.VoxModel.SizeZ + brickSizeZ - 1) / brickSizeZ
	fmt.Printf("MacroGrid Size: %dx%dx%d\n", macroSizeX, macroSizeY, macroSizeZ)
	// Group voxels by their position in brick, group bricks by their position in macroGrid
	// [brick pos] -> [voxle pos] -> voxel
	marcoGridBricks := map[[3]uint32]map[[3]uint32]Voxel{}
	for _, voxel := range voxModelAsset.VoxModel.Voxels {
		// Brick position in mackroGrid
		brickX := uint32(voxel.X) / brickSizeX
		brickY := uint32(voxel.Y) / brickSizeY
		brickZ := uint32(voxel.Z) / brickSizeZ
		brickPos := [3]uint32{brickX, brickY, brickZ}
		if _, ok := marcoGridBricks[brickPos]; !ok {
			marcoGridBricks[brickPos] = map[[3]uint32]Voxel{}
		}
		// Voxel position in brick
		voxelX := uint32(voxel.X) % brickSizeX
		voxelY := uint32(voxel.Y) % brickSizeY
		voxelZ := uint32(voxel.Z) % brickSizeZ
		voxelPos := [3]uint32{voxelX, voxelY, voxelZ}
		marcoGridBricks[brickPos][voxelPos] = voxel
	}
	// Current macroGrid offset
	currentMacroIndexOffset := uint32(len(rState.macroIndexPoolUniform))
	// Add new macroGrid to the pool, init it with empty bricks
	for mcId := uint32(0); mcId < macroSizeX*macroSizeY*macroSizeZ; mcId++ {
		rState.macroIndexPoolUniform = append(rState.macroIndexPoolUniform, EmptyBrickValue)
	}
	// Process each potential brick in the macroGrid
	for x := uint32(0); x < macroSizeX; x++ {
		for y := uint32(0); y < macroSizeY; y++ {
			for z := uint32(0); z < macroSizeZ; z++ {
				brickPos := [3]uint32{x, y, z}
				// For each non-empty brick create brick uniforms
				if brickVoxels, ok := marcoGridBricks[brickPos]; ok {
					// Calculate macroGrid index for this cell
					macroGridId := currentMacroIndexOffset + getFlatArrayIndex(x, y, z, macroSizeX, macroSizeY)

					// Detect solid-color brick: fully filled and all voxels have same color
					totalVox := brickSizeX * brickSizeY * brickSizeZ
					isSolid := false
					var solidColor uint32 = 0
					if uint32(len(brickVoxels)) == totalVox {
						first := true
						for _, v := range brickVoxels {
							if first {
								solidColor = uint32(v.ColorIndex)
								first = false
							} else if uint32(v.ColorIndex) != solidColor {
								solidColor = 0
								isSolid = false
								break
							}
							isSolid = true
						}
					}

					if isSolid {
						// Encode direct color reference: high bit set + palette color index
						rState.macroIndexPoolUniform[macroGridId] = DirectColorFlag | (solidColor & 0x7FFFFFFF)
						continue
					}

					// Non-solid brick: allocate brick and voxels
					currentBrickVoxPoolOffset := uint32(len(rState.voxelPoolUniform))
					brick := brickUniform{
						Position:   [3]uint32{x, y, z},
						DataOffset: currentBrickVoxPoolOffset,
					}
					brickId := uint32(len(rState.brickPoolUniform))
					rState.brickPoolUniform = append(rState.brickPoolUniform, brick)
					// Init brick voxels in voxel pool
					for voxPoolId := uint32(0); voxPoolId < totalVox; voxPoolId++ {
						rState.voxelPoolUniform = append(rState.voxelPoolUniform, voxelUniform{
							ColorIndex: 0, // Empty voxel
							Alpha:      0.0,
						})
					}
					// Set non-empty voxels in voxel pool
					for vx := uint32(0); vx < brickSizeX; vx++ {
						for vy := uint32(0); vy < brickSizeY; vy++ {
							for vz := uint32(0); vz < brickSizeZ; vz++ {
								voxelPos := [3]uint32{vx, vy, vz}
								if voxel, ok := brickVoxels[voxelPos]; ok {
									voxelId := currentBrickVoxPoolOffset + getFlatArrayIndex(vx, vy, vz, brickSizeX, brickSizeY)
									//TODO pass alpha from voxel
									rState.voxelPoolUniform[voxelId] = voxelUniform{
										ColorIndex: uint32(voxel.ColorIndex),
										Alpha:      1.0,
									}
								}
							}
						}
					}
					// Put new brick pointer to macroGrid
					rState.macroIndexPoolUniform[macroGridId] = brickId
				}
			}
		}
	}

	return macroGridUniform{
		Size:       [3]uint32{macroSizeX, macroSizeY, macroSizeZ},
		BrickSize:  voxModelAsset.BrickSize,
		DataOffset: currentMacroIndexOffset, // Offset in macroIndex where this model's data starts
	}
}

func getFlatArrayIndex(x, y, z, sizeX, sizeY uint32) uint32 {
	return z*sizeX*sizeY + y*sizeX + x
}

func createOrGetPaletteIndex(assetId AssetId, asset *VoxelPaletteAsset, rState *voxelRenderState) uint32 {
	if idx, ok := rState.paletteIds[assetId]; ok {
		return idx
	} else {
		palette := makeVoxelPalette(asset)
		rState.palettesUniform = append(rState.palettesUniform, palette)
		rState.paletteIds[assetId] = uint32(len(rState.palettesUniform) - 1)
		return rState.paletteIds[assetId]
	}
}

func makeVoxelPalette(asset *VoxelPaletteAsset) [256]mgl32.Vec4 {
	palette := [256]mgl32.Vec4{{}}
	for i, v := range asset.VoxPalette {
		palette[i] = mgl32.Vec4{float32(v[0]) / 255.0, float32(v[1]) / 255.0, float32(v[2]) / 255.0, float32(v[3]) / 255.0}
	}
	return palette
}

// TODO run only once?
