package gekko

import (
	"github.com/go-gl/mathgl/mgl32"
)

func (server *AssetServer) SpawnHierarchicalVoxelModel(cmd *Commands, voxId AssetId, rootTransform TransformComponent, voxelScale float32) EntityId {
	server.mu.RLock()
	voxFile, ok := server.voxFiles[voxId]
	server.mu.RUnlock()
	if !ok {
		panic("Voxel file asset not found")
	}

	paletteId := server.CreateVoxelPalette(voxFile.Palette, voxFile.VoxMaterials)

	// Create a root entity to hold the global transform
	rootEntity := cmd.AddEntity(
		&TransformComponent{Position: rootTransform.Position, Rotation: rootTransform.Rotation, Scale: rootTransform.Scale},
		&LocalTransformComponent{Position: rootTransform.Position, Rotation: rootTransform.Rotation, Scale: rootTransform.Scale},
	)

	// We need a map to keep track of spawned entities by node ID to link children to parents
	nodeEntities := make(map[int]EntityId)

	// Node 0 is always the root transform in MagicaVoxel
	server.spawnVoxNode(cmd, voxFile, 0, rootEntity, nodeEntities, paletteId, voxelScale)

	return rootEntity
}

// decodeVoxRotation converts MagicaVoxel's signed permutation byte into engine-space
// rotation and scale. We build the exact permutation matrix, remap it from VOX's
// X/Z/Y basis into the engine basis, then factor the result into a proper rotation
// plus either identity scale or uniform -1 scale for reflected cases.
func decodeVoxRotation(r byte) (mgl32.Quat, mgl32.Vec3) {
	indexNZ1 := int(r & 3)
	indexNZ2 := int((r >> 2) & 3)
	if indexNZ1 == indexNZ2 || indexNZ1 == 3 || indexNZ2 == 3 {
		return mgl32.QuatIdent(), mgl32.Vec3{1, 1, 1}
	}
	indexNZ3 := 3 - indexNZ1 - indexNZ2

	signs := [3]float32{1, 1, 1}
	if r&(1<<4) != 0 {
		signs[0] = -1
	}
	if r&(1<<5) != 0 {
		signs[1] = -1
	}
	if r&(1<<6) != 0 {
		signs[2] = -1
	}

	colsVox := [3]mgl32.Vec3{}
	colsVox[indexNZ1][0] = signs[0]
	colsVox[indexNZ2][1] = signs[1]
	colsVox[indexNZ3][2] = signs[2]
	matVox := mgl32.Mat3FromCols(colsVox[0], colsVox[1], colsVox[2])

	// Basis change between MagicaVoxel coordinates (X, Y, Z) and the engine's
	// remapped coordinates (X, Z, Y). This swap is not itself a rotation, so we
	// convert the full matrix instead of trying to remap quaternion components.
	basisSwap := mgl32.Mat3FromCols(
		mgl32.Vec3{1, 0, 0},
		mgl32.Vec3{0, 0, 1},
		mgl32.Vec3{0, 1, 0},
	)
	matEng := basisSwap.Mul3(matVox).Mul3(basisSwap)

	scale := mgl32.Vec3{1, 1, 1}
	if matEng.Det() < 0 {
		scale = mgl32.Vec3{-1, -1, -1}
		matEng = mgl32.Mat3FromCols(
			matEng.Col(0).Mul(-1),
			matEng.Col(1).Mul(-1),
			matEng.Col(2).Mul(-1),
		)
	}

	return mgl32.Mat4ToQuat(matEng.Mat4()).Normalize(), scale
}

func (server *AssetServer) spawnVoxNode(cmd *Commands, voxFile *VoxFile, nodeId int, parentEntity EntityId, nodeEntities map[int]EntityId, paletteId AssetId, voxelScale float32) {
	node, ok := voxFile.Nodes[nodeId]
	if !ok {
		return
	}

	var currentEntity EntityId

	switch node.Type {
	case VoxNodeTransform:
		// Create a transform entity
		var pos mgl32.Vec3
		var rot mgl32.Quat
		var scale mgl32.Vec3

			// Transform nodes carry the scene-graph translation and signed rotation.
			if len(node.Frames) > 0 {
				f := node.Frames[0]
				vSize := VoxelSize
				pos = mgl32.Vec3{f.LocalTrans[0], f.LocalTrans[1], f.LocalTrans[2]}.Mul(vSize * voxelScale)
				rot, scale = decodeVoxRotation(f.Rotation)
		} else {
			rot = mgl32.QuatIdent()
			scale = mgl32.Vec3{1, 1, 1}
		}

			currentEntity = cmd.AddEntity(
				&LocalTransformComponent{Position: pos, Rotation: rot, Scale: scale},
				&TransformComponent{Position: pos, Rotation: rot, Scale: scale}, // Added for compatibility with query
				&Parent{Entity: parentEntity},
			)
		nodeEntities[node.ID] = currentEntity

		// Transform nodes have one child
		server.spawnVoxNode(cmd, voxFile, node.ChildID, currentEntity, nodeEntities, paletteId, voxelScale)

	case VoxNodeGroup:
		// Group nodes just collect children
		currentEntity = cmd.AddEntity(
			&LocalTransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
			&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
			&Parent{Entity: parentEntity},
		)
		nodeEntities[node.ID] = currentEntity

		for _, childID := range node.ChildrenIDs {
			server.spawnVoxNode(cmd, voxFile, childID, currentEntity, nodeEntities, paletteId, voxelScale)
		}

		case VoxNodeShape:
			// Shape nodes attach meshes under the parent pivot, offset by half-size so
			// the model rotates around its MagicaVoxel-authored center.
			for _, m := range node.Models {
				modelAssetId := server.CreateVoxelModel(voxFile.Models[m.ModelID], voxelScale)

				model := voxFile.Models[m.ModelID]
				centerOffset := mgl32.Vec3{
					float32(model.SizeX) * -0.5,
					float32(model.SizeY) * -0.5,
					float32(model.SizeZ) * -0.5,
				}

				vSize := VoxelSize
				centerOffset = centerOffset.Mul(vSize * voxelScale)

				cmd.AddEntity(
					&LocalTransformComponent{Position: centerOffset, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
					&TransformComponent{Position: centerOffset, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
					&Parent{Entity: parentEntity},
					&VoxelModelComponent{
						VoxelModel:   modelAssetId,
						VoxelPalette: paletteId,
					},
				)
			}
		}
	}

type VoxModelInstance struct {
	ModelIndex int
	Transform  TransformComponent
}
