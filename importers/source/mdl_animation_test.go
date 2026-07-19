package source

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestTargetBindingsAcceptGoldSrcBipedNamingVariants(t *testing.T) {
	donor := []Bone{
		{Name: "Bip01", Parent: -1},
		{Name: "Bip01 Pelvis", Parent: 0},
		{Name: "Bip01 L Leg", Parent: 1},
		{Name: "Bip01 L Leg1", Parent: 2},
		{Name: "Bip01 L Foot", Parent: 3},
		{Name: "Bip01 L Arm", Parent: 0},
		{Name: "Bip01 L Arm1", Parent: 5},
		{Name: "Bip01 L Arm2", Parent: 6},
		{Name: "Bip01 L Hand", Parent: 7},
	}
	targets := []struct{ id, name, parent string }{
		{"root", "Bip01", ""}, {"pelvis", "Bip01 Pelvis", "root"},
		{"thigh", "Bip01 L Thigh", "pelvis"}, {"calf", "Bip01 L Calf", "thigh"}, {"foot", "Bip01 L Foot", "calf"},
		{"clavicle", "Bip01 L Clavicle", "root"}, {"upperarm", "Bip01 L UpperArm", "clavicle"},
		{"forearm", "Bip01 L Forearm", "upperarm"}, {"hand", "Bip01 L Hand", "forearm"},
	}
	asset := &content.AssetDef{Skeleton: &content.AssetSkeletonDef{}}
	for _, target := range targets {
		asset.Skeleton.Bones = append(asset.Skeleton.Bones, content.AssetBoneDef{ID: target.id, Name: target.name, ParentID: target.parent})
		asset.Parts = append(asset.Parts, content.AssetPartDef{ID: target.id, ParentID: target.parent})
	}
	bindings, err := targetBindings(asset, donor, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index, binding := range bindings {
		if binding.TargetID == "" {
			t.Fatalf("donor bone %q was not mapped", donor[index].Name)
		}
	}
}

func TestTargetBindingsIgnoreGenericHelperNameCollisions(t *testing.T) {
	donor := []Bone{{Name: "Bip01", Parent: -1}, {Name: "Bip01 Pelvis", Parent: 0}, {Name: "Bone03", Parent: 1}}
	asset := &content.AssetDef{
		Skeleton: &content.AssetSkeletonDef{Bones: []content.AssetBoneDef{
			{ID: "root", Name: "Bip01"}, {ID: "pelvis", Name: "Bip01 Pelvis", ParentID: "root"},
			{ID: "dummy", Name: "Dummy", ParentID: "root"}, {ID: "helper", Name: "Bone03", ParentID: "dummy"},
		}},
		Parts: []content.AssetPartDef{
			{ID: "root"}, {ID: "pelvis", ParentID: "root"}, {ID: "dummy", ParentID: "root"}, {ID: "helper", ParentID: "dummy"},
		},
	}
	bindings, err := targetBindings(asset, donor, nil)
	if err != nil {
		t.Fatal(err)
	}
	if bindings[0].TargetID == "" || bindings[1].TargetID == "" || bindings[2].TargetID != "" {
		t.Fatalf("bindings = %+v", bindings)
	}
}
