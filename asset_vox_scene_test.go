package gekko

import (
	"strings"
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

func TestResolveVoxSceneNodeModelResolvesSingleModelNodeWithoutModelIndex(t *testing.T) {
	inspection := InspectVoxScene(namedHierarchyVoxForTest(), 1.0)

	entry, err := ResolveVoxSceneNodeModel(inspection, "arm", -1)
	if err != nil {
		t.Fatalf("ResolveVoxSceneNodeModel returned error: %v", err)
	}
	if entry.ModelIndex != 1 {
		t.Fatalf("expected arm to resolve model 1, got %+v", entry)
	}
}

func TestResolveVoxSceneNodeModelResolvesSubtreeModelByIndex(t *testing.T) {
	inspection := InspectVoxScene(namedHierarchyVoxForTest(), 1.0)

	entry, err := ResolveVoxSceneNodeModel(inspection, "body", 1)
	if err != nil {
		t.Fatalf("ResolveVoxSceneNodeModel returned error: %v", err)
	}
	if entry.ModelIndex != 1 {
		t.Fatalf("expected body subtree to resolve model 1, got %+v", entry)
	}
}

func TestResolveVoxSceneNodeModelRejectsAmbiguousOrInvalidReferences(t *testing.T) {
	inspection := InspectVoxScene(namedHierarchyVoxForTest(), 1.0)

	if _, err := ResolveVoxSceneNodeModel(inspection, "", -1); err == nil || !strings.Contains(err.Error(), "requires node_name") {
		t.Fatalf("expected missing node_name error, got %v", err)
	}
	if _, err := ResolveVoxSceneNodeModel(inspection, "missing", -1); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected missing node_name resolution error, got %v", err)
	}
	if _, err := ResolveVoxSceneNodeModel(inspection, "body", -1); err == nil || !strings.Contains(err.Error(), "model_index is required") {
		t.Fatalf("expected ambiguous subtree error, got %v", err)
	}
	if _, err := ResolveVoxSceneNodeModel(inspection, "arm", 0); err == nil || !strings.Contains(err.Error(), "does not contain model_index 0") {
		t.Fatalf("expected subtree/model mismatch error, got %v", err)
	}

	dupInspection := InspectVoxScene(duplicateNamedHierarchyVoxForSceneTest(), 1.0)
	if _, err := ResolveVoxSceneNodeModel(dupInspection, "arm", -1); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected duplicate-name ambiguity error, got %v", err)
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

func duplicateNamedHierarchyVoxForSceneTest() *VoxFile {
	return &VoxFile{
		Models: []VoxModel{
			{SizeX: 2, SizeY: 2, SizeZ: 2},
			{SizeX: 2, SizeY: 2, SizeZ: 2},
		},
		Nodes: map[int]VoxNode{
			0: {ID: 0, Type: VoxNodeGroup, ChildrenIDs: []int{1, 3}},
			1: {
				ID:         1,
				Type:       VoxNodeTransform,
				Attributes: map[string]string{"_name": "arm"},
				ChildID:    2,
				Frames:     []VoxTransformFrame{{Rotation: 0}},
			},
			2: {ID: 2, Type: VoxNodeShape, Models: []VoxShapeModel{{ModelID: 0}}},
			3: {
				ID:         3,
				Type:       VoxNodeTransform,
				Attributes: map[string]string{"_name": "arm"},
				ChildID:    4,
				Frames:     []VoxTransformFrame{{Rotation: 0}},
			},
			4: {ID: 4, Type: VoxNodeShape, Models: []VoxShapeModel{{ModelID: 1}}},
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
