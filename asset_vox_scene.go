package gekko

import (
	"fmt"
	"sort"

	"github.com/go-gl/mathgl/mgl32"
)

type VoxSceneNodeInfo struct {
	NodeID               int
	ParentNodeID         int
	Name                 string
	ChildNodeIDs         []int
	ModelIndices         []int
	LocalTransform       TransformComponent
	WorldTransform       TransformComponent
	DescendantModelCount int
	DuplicateName        bool
}

type VoxSceneModelEntry struct {
	Key            string
	NodeID         int
	ParentNodeID   int
	ParentModelKey string
	Name           string
	SourceNodeName string
	ModelIndex     int
	WorldTransform TransformComponent
}

type VoxSceneInspection struct {
	RootNodeIDs    []int
	Nodes          []VoxSceneNodeInfo
	ModelEntries   []VoxSceneModelEntry
	DuplicateNames map[string]bool
}

func InspectVoxScene(voxFile *VoxFile, voxelScale float32) VoxSceneInspection {
	result := VoxSceneInspection{
		DuplicateNames: make(map[string]bool),
	}
	if voxFile == nil {
		return result
	}

	if len(voxFile.Nodes) == 0 {
		for i := range voxFile.Models {
			result.ModelEntries = append(result.ModelEntries, VoxSceneModelEntry{
				Key:            fmt.Sprintf("flat:%d", i),
				NodeID:         i,
				ParentNodeID:   -1,
				Name:           fallbackModelName(i),
				SourceNodeName: fallbackModelName(i),
				ModelIndex:     i,
				WorldTransform: modelWorldTransform(voxFile.Models[i], mgl32.Vec3{}, mgl32.QuatIdent(), mgl32.Vec3{1, 1, 1}, voxelScale),
			})
		}
		return result
	}

	rootNodeIDs := voxSceneRootNodeIDs(voxFile)
	result.RootNodeIDs = append(result.RootNodeIDs, rootNodeIDs...)

	nodesByID := make(map[int]*VoxSceneNodeInfo, len(voxFile.Nodes))

	var traverse func(nodeID int, parentNodeID int, parentWorld TransformComponent, inheritedName string, parentModelKey string)
	traverse = func(nodeID int, parentNodeID int, parentWorld TransformComponent, inheritedName string, parentModelKey string) {
		node, ok := voxFile.Nodes[nodeID]
		if !ok {
			return
		}

		info := &VoxSceneNodeInfo{
			NodeID:       nodeID,
			ParentNodeID: parentNodeID,
			Name:         fallbackNodeName(nodeID),
			LocalTransform: TransformComponent{
				Rotation: mgl32.QuatIdent(),
				Scale:    mgl32.Vec3{1, 1, 1},
			},
			WorldTransform: parentWorld,
		}

		if explicitName, ok := node.Attributes["_name"]; ok && explicitName != "" {
			info.Name = explicitName
		}

		childInheritedName := inheritedName
		if _, ok := node.Attributes["_name"]; ok && node.Attributes["_name"] != "" {
			childInheritedName = node.Attributes["_name"]
		}

		switch node.Type {
		case VoxNodeTransform:
			info.LocalTransform = voxTransformNodeLocalTransform(node, voxelScale)
			info.WorldTransform = LocalTransformToWorld(parentWorld, false, LocalTransformComponent{
				Position: info.LocalTransform.Position,
				Rotation: info.LocalTransform.Rotation,
				Scale:    info.LocalTransform.Scale,
			})
			info.ChildNodeIDs = []int{node.ChildID}
			nodesByID[nodeID] = info
			traverse(node.ChildID, nodeID, info.WorldTransform, childInheritedName, parentModelKey)
		case VoxNodeGroup:
			info.WorldTransform = parentWorld
			info.ChildNodeIDs = append(info.ChildNodeIDs, node.ChildrenIDs...)
			nodesByID[nodeID] = info
			for _, childID := range node.ChildrenIDs {
				traverse(childID, nodeID, info.WorldTransform, childInheritedName, parentModelKey)
			}
		case VoxNodeShape:
			info.WorldTransform = parentWorld
			for idx, model := range node.Models {
				info.ModelIndices = append(info.ModelIndices, model.ModelID)
				entryName := preferredSceneModelName(node, childInheritedName, model.ModelID)
				entrySourceName := preferredSceneSourceName(node, childInheritedName, nodeID, model.ModelID)
				key := fmt.Sprintf("%d:%d:%d", nodeID, idx, model.ModelID)
				result.ModelEntries = append(result.ModelEntries, VoxSceneModelEntry{
					Key:            key,
					NodeID:         nodeID,
					ParentNodeID:   parentNodeID,
					ParentModelKey: parentModelKey,
					Name:           entryName,
					SourceNodeName: entrySourceName,
					ModelIndex:     model.ModelID,
					WorldTransform: modelWorldTransform(voxFile.Models[model.ModelID], parentWorld.Position, parentWorld.Rotation, parentWorld.Scale, voxelScale),
				})
			}
			nodesByID[nodeID] = info
		}
	}

	identity := TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	for _, rootNodeID := range rootNodeIDs {
		traverse(rootNodeID, -1, identity, "", "")
	}

	sort.Ints(result.RootNodeIDs)
	nodeIDs := make([]int, 0, len(nodesByID))
	for nodeID := range nodesByID {
		nodeIDs = append(nodeIDs, nodeID)
	}
	sort.Ints(nodeIDs)
	nameCounts := make(map[string]int, len(nodesByID))
	for _, nodeID := range nodeIDs {
		nameCounts[nodesByID[nodeID].Name]++
	}
	for _, nodeID := range nodeIDs {
		node := nodesByID[nodeID]
		for _, entry := range result.ModelEntries {
			if entry.NodeID == nodeID || isDescendantNode(nodesByID, entry.NodeID, nodeID) {
				node.DescendantModelCount++
			}
		}
		node.DuplicateName = nameCounts[node.Name] > 1
		if node.DuplicateName {
			result.DuplicateNames[node.Name] = true
		}
		result.Nodes = append(result.Nodes, *node)
	}

	return result
}

func FilterVoxSceneSubtreeEntries(inspection VoxSceneInspection, rootNodeID int) []VoxSceneModelEntry {
	entries := make([]VoxSceneModelEntry, 0)
	if rootNodeID < 0 {
		return entries
	}
	nodesByID := make(map[int]VoxSceneNodeInfo, len(inspection.Nodes))
	for _, node := range inspection.Nodes {
		nodesByID[node.NodeID] = node
	}
	for _, entry := range inspection.ModelEntries {
		if entry.NodeID == rootNodeID || isDescendantNodeInfo(nodesByID, entry.NodeID, rootNodeID) {
			entries = append(entries, entry)
		}
	}
	return entries
}

func FindVoxSceneModelIndexByName(voxFile *VoxFile, nodeName string) (int, bool) {
	inspection := InspectVoxScene(voxFile, 1.0)
	for _, entry := range inspection.ModelEntries {
		if entry.SourceNodeName == nodeName {
			return entry.ModelIndex, true
		}
	}
	return 0, false
}

func ExtractVoxHierarchy(voxFile *VoxFile, voxelScale float32) []VoxModelInstance {
	inspection := InspectVoxScene(voxFile, voxelScale)
	instances := make([]VoxModelInstance, 0, len(inspection.ModelEntries))
	for _, entry := range inspection.ModelEntries {
		instances = append(instances, VoxModelInstance{
			ModelIndex: entry.ModelIndex,
			Transform:  entry.WorldTransform,
		})
	}
	return instances
}

func voxSceneRootNodeIDs(voxFile *VoxFile) []int {
	rootNodes := make([]int, 0)
	isChild := make(map[int]bool)
	for _, n := range voxFile.Nodes {
		for _, childID := range n.ChildrenIDs {
			isChild[childID] = true
		}
		if n.Type == VoxNodeTransform && n.ChildID != 0 {
			isChild[n.ChildID] = true
		}
	}
	for id := range voxFile.Nodes {
		if !isChild[id] && (voxFile.Nodes[id].Type == VoxNodeTransform || voxFile.Nodes[id].Type == VoxNodeGroup || voxFile.Nodes[id].Type == VoxNodeShape) {
			rootNodes = append(rootNodes, id)
		}
	}
	if len(rootNodes) == 0 {
		rootNodes = append(rootNodes, 0)
	}
	sort.Ints(rootNodes)
	return rootNodes
}

func voxTransformNodeLocalTransform(node VoxNode, voxelScale float32) TransformComponent {
	transform := TransformComponent{
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
	}
	if len(node.Frames) == 0 {
		return transform
	}
	frame := node.Frames[0]
	transform.Position = mgl32.Vec3{frame.LocalTrans[0], frame.LocalTrans[1], frame.LocalTrans[2]}.Mul(VoxelSize * voxelScale)
	transform.Rotation, transform.Scale = decodeVoxRotation(frame.Rotation)
	return transform
}

func preferredSceneModelName(node VoxNode, inheritedName string, modelIndex int) string {
	if inheritedName != "" {
		return inheritedName
	}
	if explicit, ok := node.Attributes["_name"]; ok && explicit != "" {
		return explicit
	}
	return fallbackModelName(modelIndex)
}

func preferredSceneSourceName(node VoxNode, inheritedName string, nodeID int, modelIndex int) string {
	if inheritedName != "" {
		return inheritedName
	}
	if explicit, ok := node.Attributes["_name"]; ok && explicit != "" {
		return explicit
	}
	if nodeID >= 0 {
		return fallbackNodeName(nodeID)
	}
	return fallbackModelName(modelIndex)
}

func fallbackNodeName(nodeID int) string {
	return fmt.Sprintf("node_%d", nodeID)
}

func fallbackModelName(modelIndex int) string {
	return fmt.Sprintf("model_%d", modelIndex)
}

func modelWorldTransform(model VoxModel, pos mgl32.Vec3, rot mgl32.Quat, scale mgl32.Vec3, voxelScale float32) TransformComponent {
	pivot := mgl32.Vec3{
		float32(model.SizeX) * 0.5,
		float32(model.SizeY) * 0.5,
		float32(model.SizeZ) * 0.5,
	}.Mul(VoxelSize * voxelScale)
	return TransformComponent{
		Position: pos,
		Rotation: rot,
		Scale:    scale,
		Pivot:    pivot,
	}
}

func isDescendantNode(nodesByID map[int]*VoxSceneNodeInfo, nodeID int, ancestorID int) bool {
	current := nodeID
	for {
		node, ok := nodesByID[current]
		if !ok || node.ParentNodeID < 0 {
			return false
		}
		if node.ParentNodeID == ancestorID {
			return true
		}
		current = node.ParentNodeID
	}
}

func isDescendantNodeInfo(nodesByID map[int]VoxSceneNodeInfo, nodeID int, ancestorID int) bool {
	current := nodeID
	for {
		node, ok := nodesByID[current]
		if !ok || node.ParentNodeID < 0 {
			return false
		}
		if node.ParentNodeID == ancestorID {
			return true
		}
		current = node.ParentNodeID
	}
}
