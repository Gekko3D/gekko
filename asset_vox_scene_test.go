package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestInspectVoxSceneReturnsNamedNodesAndModels(t *testing.T) {
	vox := namedHierarchyVoxForTest()

	inspection := InspectVoxScene(vox, 1.0)
	if len(inspection.Nodes) < 4 {
		t.Fatalf("expected scene nodes, got %+v", inspection.Nodes)
	}
	if len(inspection.ModelEntries) != 2 {
		t.Fatalf("expected 2 model entries, got %+v", inspection.ModelEntries)
	}

	body := findSceneNodeByName(t, inspection, "body")
	arm := findSceneNodeByName(t, inspection, "arm")
	group := findSceneNodeByID(t, inspection, 1)
	if body.DescendantModelCount != 2 {
		t.Fatalf("expected body descendant count 2, got %d", body.DescendantModelCount)
	}
	if group.ParentNodeID != body.NodeID {
		t.Fatalf("expected group parent to be body, got %d", group.ParentNodeID)
	}
	if arm.ParentNodeID != group.NodeID {
		t.Fatalf("expected arm parent to be group, got %d", arm.ParentNodeID)
	}

	if inspection.ModelEntries[0].Name != "body" {
		t.Fatalf("expected first model entry name to use transform name, got %+v", inspection.ModelEntries[0])
	}
	if inspection.ModelEntries[1].Name != "arm" {
		t.Fatalf("expected second model entry name to use transform name, got %+v", inspection.ModelEntries[1])
	}
}

func TestFilterVoxSceneSubtreeEntriesPreservesSubtreeSelection(t *testing.T) {
	vox := namedHierarchyVoxForTest()

	inspection := InspectVoxScene(vox, 1.0)
	body := findSceneNodeByName(t, inspection, "body")
	arm := findSceneNodeByName(t, inspection, "arm")

	bodyEntries := FilterVoxSceneSubtreeEntries(inspection, body.NodeID)
	armEntries := FilterVoxSceneSubtreeEntries(inspection, arm.NodeID)

	if len(bodyEntries) != 2 {
		t.Fatalf("expected body subtree to include both models, got %+v", bodyEntries)
	}
	if len(armEntries) != 1 || armEntries[0].Name != "arm" {
		t.Fatalf("expected arm subtree to include one arm model, got %+v", armEntries)
	}
}

func TestExtractVoxHierarchyPreservesWorldTransformsForSceneTraversal(t *testing.T) {
	vox := namedHierarchyVoxForTest()

	instances := ExtractVoxHierarchy(vox, 1.0)
	if len(instances) != 2 {
		t.Fatalf("expected 2 extracted instances, got %+v", instances)
	}

	if !vec3ApproxEqual(instances[0].Transform.Position, mgl32.Vec3{1, 0, 0}.Mul(VoxelSize), 1e-5) {
		t.Fatalf("unexpected root model position %v", instances[0].Transform.Position)
	}
	if !vec3ApproxEqual(instances[1].Transform.Position, mgl32.Vec3{1, 2, 0}.Mul(VoxelSize), 1e-5) {
		t.Fatalf("unexpected child model position %v", instances[1].Transform.Position)
	}
}

func namedHierarchyVoxForTest() *VoxFile {
	return &VoxFile{
		Models: []VoxModel{
			{SizeX: 2, SizeY: 2, SizeZ: 2},
			{SizeX: 4, SizeY: 2, SizeZ: 2},
		},
		Nodes: map[int]VoxNode{
			0: {
				ID:         0,
				Type:       VoxNodeTransform,
				Attributes: map[string]string{"_name": "body"},
				ChildID:    1,
				Frames: []VoxTransformFrame{{
					Rotation:   0,
					LocalTrans: [3]float32{1, 0, 0},
				}},
			},
			1: {
				ID:          1,
				Type:        VoxNodeGroup,
				ChildrenIDs: []int{2, 4},
			},
			2: {
				ID:     2,
				Type:   VoxNodeShape,
				Models: []VoxShapeModel{{ModelID: 0}},
			},
			4: {
				ID:         4,
				Type:       VoxNodeTransform,
				Attributes: map[string]string{"_name": "arm"},
				ChildID:    5,
				Frames: []VoxTransformFrame{{
					Rotation:   0,
					LocalTrans: [3]float32{0, 2, 0},
				}},
			},
			5: {
				ID:     5,
				Type:   VoxNodeShape,
				Models: []VoxShapeModel{{ModelID: 1}},
			},
		},
	}
}

func findSceneNodeByName(t *testing.T, inspection VoxSceneInspection, name string) VoxSceneNodeInfo {
	t.Helper()
	for _, node := range inspection.Nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("missing scene node %q in %+v", name, inspection.Nodes)
	return VoxSceneNodeInfo{}
}

func findSceneNodeByID(t *testing.T, inspection VoxSceneInspection, nodeID int) VoxSceneNodeInfo {
	t.Helper()
	for _, node := range inspection.Nodes {
		if node.NodeID == nodeID {
			return node
		}
	}
	t.Fatalf("missing scene node %d in %+v", nodeID, inspection.Nodes)
	return VoxSceneNodeInfo{}
}
