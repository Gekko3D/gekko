package content

import (
	"path/filepath"
	"testing"
)

func TestAssetAttachmentLibraryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hero.gkattachments")
	want := NewAssetAttachmentLibraryDef("Hero loadout")
	want.Attachments = []AssetAttachmentDef{{
		ID:             "backpack",
		Name:           "Backpack",
		ParentAssetRef: "hero",
		ParentMarkerID: "spine",
		ChildAssetRef:  "backpack",
		Transform:      AssetTransformDef{Rotation: Quat{0, 0, 0, 1}, Scale: Vec3{1, 1, 1}},
	}}
	if err := SaveAssetAttachmentLibrary(path, want); err != nil {
		t.Fatalf("save attachment library: %v", err)
	}
	got, err := LoadAssetAttachmentLibrary(path)
	if err != nil {
		t.Fatalf("load attachment library: %v", err)
	}
	if got.SchemaVersion != CurrentAssetAttachmentLibrarySchemaVersion || len(got.Attachments) != 1 || got.Attachments[0].ParentMarkerID != "spine" {
		t.Fatalf("unexpected attachment library: %+v", got)
	}
}

func TestAssetAttachmentLibraryRejectsCycles(t *testing.T) {
	def := NewAssetAttachmentLibraryDef("cycle")
	def.Attachments = []AssetAttachmentDef{
		{ID: "a", Name: "a", ParentAssetRef: "hero", ParentMarkerID: "spine", ChildAssetRef: "backpack"},
		{ID: "b", Name: "b", ParentAssetRef: "backpack", ParentMarkerID: "hook", ChildAssetRef: "hero"},
	}
	if err := ValidateAssetAttachmentLibrary(def); err == nil {
		t.Fatal("expected attachment cycle to be rejected")
	}
}
