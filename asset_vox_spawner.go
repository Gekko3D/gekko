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

// Decode MagicaVoxel rotation byte to Quaternion and Scale
// Ported from dot_vox (Rust) to ensure correct handling of all 48 cases including reflections.
func decodeVoxRotation(r byte) (mgl32.Quat, mgl32.Vec3) {
	index_nz1 := int(r & 3)
	index_nz2 := int((r >> 2) & 3)
	flip := int((r >> 4) & 7)

	si := mgl32.Vec3{1.0, 1.0, 1.0}
	sf := mgl32.Vec3{-1.0, -1.0, -1.0}

	const SQRT_2_2 = float32(0.70710678) // sqrt(2)/2

	// Helper to create Quat from [x, y, z, w]
	q := func(x, y, z, w float32) mgl32.Quat {
		return mgl32.Quat{W: w, V: mgl32.Vec3{x, y, z}}
	}

	var quats [4]mgl32.Quat
	var mapping [8]int
	var scales [8]mgl32.Vec3

	// Default scales mapping (alternating si/sf) common to many cases in dot_vox
	// But dot_vox defines them explicitly per case.
	// si, sf, sf, si, sf, si, si, sf
	scales_standard := [8]mgl32.Vec3{si, sf, sf, si, sf, si, si, sf}
	// sf, si, si, sf, si, sf, sf, si
	scales_inverted := [8]mgl32.Vec3{sf, si, si, sf, si, sf, sf, si}

	switch {
	case index_nz1 == 0 && index_nz2 == 1:
		quats = [4]mgl32.Quat{
			q(0.0, 0.0, 0.0, 1.0),
			q(0.0, 0.0, 1.0, 0.0),
			q(0.0, 1.0, 0.0, 0.0),
			q(1.0, 0.0, 0.0, 0.0),
		}
		mapping = [8]int{0, 3, 2, 1, 1, 2, 3, 0}
		scales = scales_standard

	case index_nz1 == 0 && index_nz2 == 2:
		quats = [4]mgl32.Quat{
			q(0.0, SQRT_2_2, SQRT_2_2, 0.0),
			q(SQRT_2_2, 0.0, 0.0, SQRT_2_2),
			q(SQRT_2_2, 0.0, 0.0, -SQRT_2_2),
			q(0.0, SQRT_2_2, -SQRT_2_2, 0.0),
		}
		mapping = [8]int{3, 0, 1, 2, 2, 1, 0, 3}
		scales = scales_inverted

	case index_nz1 == 1 && index_nz2 == 2:
		quats = [4]mgl32.Quat{
			q(0.5, 0.5, 0.5, -0.5),
			q(0.5, -0.5, 0.5, 0.5),
			q(0.5, -0.5, -0.5, -0.5),
			q(0.5, 0.5, -0.5, 0.5),
		}
		mapping = [8]int{0, 3, 2, 1, 1, 2, 3, 0}
		scales = scales_standard

	case index_nz1 == 1 && index_nz2 == 0:
		quats = [4]mgl32.Quat{
			q(0.0, 0.0, SQRT_2_2, SQRT_2_2),
			q(0.0, 0.0, SQRT_2_2, -SQRT_2_2),
			q(SQRT_2_2, SQRT_2_2, 0.0, 0.0),
			q(SQRT_2_2, -SQRT_2_2, 0.0, 0.0),
		}
		mapping = [8]int{3, 0, 1, 2, 2, 1, 0, 3}
		scales = scales_inverted

	case index_nz1 == 2 && index_nz2 == 0:
		quats = [4]mgl32.Quat{
			q(0.5, 0.5, 0.5, 0.5),
			q(0.5, -0.5, -0.5, 0.5),
			q(0.5, 0.5, -0.5, -0.5),
			q(0.5, -0.5, 0.5, -0.5),
		}
		mapping = [8]int{0, 3, 2, 1, 1, 2, 3, 0}
		scales = scales_standard

	case index_nz1 == 2 && index_nz2 == 1:
		quats = [4]mgl32.Quat{
			q(0.0, SQRT_2_2, 0.0, -SQRT_2_2),
			q(SQRT_2_2, 0.0, SQRT_2_2, 0.0),
			q(0.0, SQRT_2_2, 0.0, SQRT_2_2),
			q(SQRT_2_2, 0.0, -SQRT_2_2, 0.0),
		}
		mapping = [8]int{3, 0, 1, 2, 2, 1, 0, 3}
		scales = scales_inverted

	default:
		// Fallback for invalid rotation
		return mgl32.QuatIdent(), si
	}

	// Returned rotation/scales from dot_vox are in MagicaVoxel basis (Z-up).
	// Engine uses Y-up and we remap axes as: X := X, Y := Z_vox, Z := Y_vox.
	// To convert rotation, swap Y and Z components of quaternion's vector part.
	// To convert scale flips, also swap Y and Z components.
	rotVox := quats[mapping[flip]]
	scaleVox := scales[flip]

	rotEng := mgl32.Quat{
		W: rotVox.W,
		V: mgl32.Vec3{rotVox.V.X(), rotVox.V.Z(), rotVox.V.Y()},
	}
	scaleEng := mgl32.Vec3{scaleVox.X(), scaleVox.Z(), scaleVox.Y()}

	return rotEng, scaleEng
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

		// Decode Rotation and Scale using dot_vox logic
		if len(node.Frames) > 0 {
			f := node.Frames[0]
			vSize := VoxelSize
			pos = mgl32.Vec3{f.LocalTrans[0], f.LocalTrans[1], f.LocalTrans[2]}.Mul(vSize * voxelScale)
			rot, scale = decodeVoxRotation(f.Rotation)
		} else {
			rot = mgl32.QuatIdent()
			scale = mgl32.Vec3{1, 1, 1}
		}

		// Fix 2: Pivot Offset Logic Moved to VoxNodeShape
		// We no longer apply the centering offset here. The Transform node represents the Pivot Point.
		// The Shape node will spawn a child entity offset by -Size/2 to center the mesh on this pivot.

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
		// Shape nodes hold model references
		// In MagicaVoxel, models pivot around their center.
		// Since the parent TransformNode is positioned at the Pivot Point (Joint),
		// we must spawn the Mesh as a Child Entity offset by -Size/2.
		for _, m := range node.Models {
			modelAssetId := server.CreateVoxelModel(voxFile.Models[m.ModelID], voxelScale)

			// Calculate centering offset
			model := voxFile.Models[m.ModelID]
			centerOffset := mgl32.Vec3{
				float32(model.SizeX) * -0.5,
				float32(model.SizeY) * -0.5,
				float32(model.SizeZ) * -0.5,
			}

			// Scale the offset to world units (using the same VoxelSize as translation)
			vSize := VoxelSize
			centerOffset = centerOffset.Mul(vSize * voxelScale)

			// Create a child entity for the mesh.
			// Rotation is Identity because the rotation is handled by the Parent TransformNode.
			// Scale is 1.0 because Scale is also handled by Parent TransformNode (usually).
			// Wait, the Parent Scale might need to apply to the Offset?
			// LocalTransform translates relative to parent. Parent scale scales the child's position?
			// In most engines, ChildPosition IS scaled by ParentScale.
			// So if ParentScale is (1,1,1), good. If ParentScale is (-1,-1,-1) [Reflection],
			// The Child Position (Offset) will be flipped too.
			// If Offset is (-5, -5, -5) and ParentScale is (-1), EffPos is (5,5,5).
			// This might be what we want to keep the mesh inside the reflection?

			cmd.AddEntity(
				&LocalTransformComponent{Position: centerOffset, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&TransformComponent{Position: centerOffset, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
				&Parent{Entity: parentEntity}, // Attached to the TransformNode (Pivot)
				&VoxelModelComponent{
					VoxelModel:   modelAssetId,
					VoxelPalette: paletteId,
				},
			)
		}
		// Shape nodes are leaves in the scene graph for purposes of hierarchy (they attach to their parent nTRN)
	}
}

type VoxModelInstance struct {
	ModelIndex int
	Transform  TransformComponent
}

// ExtractVoxHierarchy traverses the VOX scene graph and calculates the global transform for each shape node.
func ExtractVoxHierarchy(voxFile *VoxFile, voxelScale float32) []VoxModelInstance {
	instances := make([]VoxModelInstance, 0)
	if voxFile == nil {
		return instances
	}

	if len(voxFile.Nodes) == 0 {
		// Fallback for flat structure
		rot := mgl32.QuatIdent()
		scale := mgl32.Vec3{1, 1, 1}
		for i, m := range voxFile.Models {
			vSize := VoxelSize
			centerOffset := mgl32.Vec3{
				float32(m.SizeX) * -0.5,
				float32(m.SizeY) * -0.5,
				float32(m.SizeZ) * -0.5,
			}.Mul(vSize * voxelScale)

			instances = append(instances, VoxModelInstance{
				ModelIndex: i,
				Transform: TransformComponent{
					Position: centerOffset,
					Rotation: rot,
					Scale:    scale,
				},
			})
		}
		return instances
	}

	rootNodes := make([]int, 0)
	isChild := make(map[int]bool)
	for _, n := range voxFile.Nodes {
		for _, childH := range n.ChildrenIDs {
			isChild[childH] = true
		}
		if n.Type == VoxNodeTransform && n.ChildID != 0 {
			isChild[n.ChildID] = true
		}
	}
	for id := range voxFile.Nodes {
		if !isChild[id] && (voxFile.Nodes[id].Type == VoxNodeTransform || voxFile.Nodes[id].Type == VoxNodeGroup) {
			rootNodes = append(rootNodes, id)
		}
	}

	if len(rootNodes) == 0 {
		rootNodes = append(rootNodes, 0) // Try node 0 if no clear roots
	}

	var traverse func(nodeID int, parentPos mgl32.Vec3, parentRot mgl32.Quat, parentScale mgl32.Vec3)
	traverse = func(nodeID int, parentPos mgl32.Vec3, parentRot mgl32.Quat, parentScale mgl32.Vec3) {
		node, ok := voxFile.Nodes[nodeID]
		if !ok {
			return
		}

		currentPos := parentPos
		currentRot := parentRot
		currentScale := parentScale

		switch node.Type {
		case VoxNodeTransform:
			if len(node.Frames) > 0 {
				f := node.Frames[0]
				vSize := VoxelSize
				localPos := mgl32.Vec3{f.LocalTrans[0], f.LocalTrans[1], f.LocalTrans[2]}.Mul(vSize * voxelScale)
				localRot, localScale := decodeVoxRotation(f.Rotation)

				// Transform local position by parent state
				// This is equivalent to applying parent scale (which scales offset), parent rot, then adding parent pos
				scaledLocal := mgl32.Vec3{
					localPos.X() * parentScale.X(),
					localPos.Y() * parentScale.Y(),
					localPos.Z() * parentScale.Z(),
				}
				rotatedLocal := parentRot.Rotate(scaledLocal)

				currentPos = parentPos.Add(rotatedLocal)
				currentRot = parentRot.Mul(localRot)
				currentScale = mgl32.Vec3{
					parentScale.X() * localScale.X(),
					parentScale.Y() * localScale.Y(),
					parentScale.Z() * localScale.Z(),
				}
			}
			traverse(node.ChildID, currentPos, currentRot, currentScale)
		case VoxNodeGroup:
			for _, cid := range node.ChildrenIDs {
				traverse(cid, currentPos, currentRot, currentScale)
			}
		case VoxNodeShape:
			for _, m := range node.Models {
				instances = append(instances, VoxModelInstance{
					ModelIndex: m.ModelID,
					Transform: TransformComponent{
						Position: currentPos,
						Rotation: currentRot,
						Scale:    currentScale,
					},
				})
			}
		}
	}

	for _, id := range rootNodes {
		traverse(id, mgl32.Vec3{0, 0, 0}, mgl32.QuatIdent(), mgl32.Vec3{1, 1, 1})
	}

	return instances
}
