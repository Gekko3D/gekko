package gekko

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func c3g7Fixture(t *testing.T) (string, *content.CompiledAssetModelHeaderDef) {
	t.Helper()
	oldPath, old := c3h7Fixture(t)
	modelPath, modelHeaderPath, mi, _ := c3g6Frames(t, nil)
	model, _, err := content.LoadCompiledAssetModel(modelPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := content.CompiledAssetModelBaseIdentity(model, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(oldPath), "model.gkvox"), data, 0600); err != nil {
		t.Fatal(err)
	}
	mh, _, err := content.LoadCompiledAssetModelHeader(modelHeaderPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	palette := mh.Palettes[0]
	palette.Materials = []content.CompiledAssetModelMaterialDef{{ID: 3, Property: map[string]any{"name": "owned", "rough": .25}}}
	palette.SurfaceMaterials = map[uint8]content.CompiledAssetModelSurfaceMaterialDef{3: {Kind: "stone", Tags: []string{"solid"}}}
	palette.ID, err = content.CompiledAssetModelPaletteIdentity(&palette)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"model", "other-model"} {
		p := mh.Asset.Parts[0]
		p.ID = id
		p.Name = id
		if id == "model" {
			p.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "original-never-read.vox", ModelIndex: 0}
		}
		old.Asset.Parts = append(old.Asset.Parts, p)
	}
	h := &content.CompiledAssetModelHeaderDef{SchemaVersion: 1, CompilerVersion: content.CurrentCompiledAssetModelHeaderCompilerVersion, Asset: old.Asset, Shapes: old.Shapes, LODs: old.LODs, Palettes: []content.CompiledAssetModelPaletteDef{palette}}
	for _, id := range []string{"model", "other-model"} {
		h.Models = append(h.Models, content.CompiledAssetModelRefDef{PartID: id, Path: "model.gkvox", ContentID: mi.ContentID, BaseIdentity: base, EncodedBytes: mi.EncodedBytes, DecodedBytes: mi.DecodedBytes, PaletteID: palette.ID})
	}
	path := strings.TrimSuffix(oldPath, ".gkassetc") + ".gkmodelassetc"
	c3g7Save(t, path, h)
	return path, h
}
func c3g7Save(t *testing.T, path string, h *content.CompiledAssetModelHeaderDef) {
	t.Helper()
	if _, err := content.SaveCompiledAssetModelHeader(path, h, nil); err != nil {
		t.Fatal(err)
	}
}

func TestC3g7ExplicitSelectorAndMixedCanonicalBoundary(t *testing.T) {
	path, h := c3g7Fixture(t)
	for _, p := range []string{path, "input.gkassetc"} {
		if !isCompiledAssetPath(p) {
			t.Fatal("compiled path selector missing", p)
		}
	}
	for _, p := range []string{"input.GKMODELASSETC", "input.gkmodelassetc.json", "input.gkasset", "input.json"} {
		if isCompiledAssetPath(p) {
			t.Fatal("compiled selector widened", p)
		}
	}
	owner := NewRuntimeContentLoader()
	input, err := loadRuntimeAssetInput(owner, path)
	if err != nil || input.compiledModel == nil || input.compiled != nil || input.definition != input.compiledModel.Asset {
		t.Fatal("new typed input route", err)
	}
	// The original .gkassetc and JSON entry points retain strict kind selection.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".gkassetc", ".json"} {
		wrong := filepath.Join(t.TempDir(), "wrong"+suffix)
		if err := os.WriteFile(wrong, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadRuntimeAssetInput(NewRuntimeContentLoader(), wrong); err == nil {
			t.Fatal("wrong typed/JSON entry accepted model frame")
		}
	}
	if _, err := content.LoadAsset(path); err == nil {
		t.Fatal("public JSON LoadAsset widened")
	}
	ref := h.Shapes[0]
	proof, err := loadRuntimeAssetCanonicalPart(owner, path, ref.PartID, true, runtimeAssetCanonicalOptions{proveAuthoredBase: true})
	if err != nil || proof.identity != ref.BaseIdentity || proof.geometry == nil || proof.geometry.GetVoxelCount() != 4 {
		t.Fatal("mixed inline compiled proof lost", err)
	}
	for _, needBase := range []bool{false, true} {
		if out, err := loadRuntimeAssetCanonicalPart(owner, path, "model", needBase, runtimeAssetCanonicalOptions{}); err == nil || !reflect.DeepEqual(out, runtimeAssetCanonicalPart{}) {
			t.Fatal("new model acquired E2 canonical authority")
		}
	}
}

func TestC3g7SessionWholeClosureBorrowAndOwnedMetadata(t *testing.T) {
	path, h := c3g7Fixture(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	cachedHeader, _, err := caller.Loader().LoadCompiledAssetModelHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(filepath.Dir(path), h.Models[0].Path)
	cached, info, err := caller.Loader().LoadCompiledAssetModel(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	session, err := verifyCompiledAssetInput(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(session.models) != 2 || len(session.shapes) != 2 || len(session.lods) != 2 {
		t.Fatal("whole mixed closure missing")
	}
	for _, ref := range h.Models {
		v, ok := session.models[ref.PartID]
		if !ok || v.definition != cached || v.contentID != info.ContentID || v.baseIdentity != ref.BaseIdentity || v.palette == nil || v.palette.ID != ref.PaletteID {
			t.Fatal("verified model borrowing/identity lost")
		}
	}
	metadata := session.def
	metadata.Parts[len(metadata.Parts)-1].Source.Params["sx"] = 99
	v := session.models["model"]
	v.palette.Materials[0].Property["name"] = "changed"
	facts := v.palette.SurfaceMaterials[3]
	facts.Tags[0] = "changed"
	if cachedHeader.Asset.Parts[len(cachedHeader.Asset.Parts)-1].Source.Params["sx"] != 2 || cachedHeader.Palettes[0].Materials[0].Property["name"] != "owned" || cachedHeader.Palettes[0].SurfaceMaterials[3].Tags[0] != "solid" {
		t.Fatal("session metadata/palette aliases cache")
	}
	session.close()
	session.close()
	if after := owner.Stats(); after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("session release revoked caller pins or leaked child", after)
	}
	if cached.Bricks[0].Values[0] != 3 {
		t.Fatal("session close invalidated cached model")
	}
	second, err := verifyCompiledAssetInput(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	if second.models["model"].palette.Materials[0].Property["name"] != "owned" {
		t.Fatal("independent session shares palette mutation")
	}
}

func TestC3g7RejectUnselectedModelBindingsAndCancelledLeases(t *testing.T) {
	for name, change := range map[string]func(*content.CompiledAssetModelHeaderDef){"cid": func(h *content.CompiledAssetModelHeaderDef) {
		for i := range h.Models {
			h.Models[i].ContentID = strings.Repeat("f", 64)
		}
	}, "encoded": func(h *content.CompiledAssetModelHeaderDef) {
		for i := range h.Models {
			h.Models[i].EncodedBytes++
		}
	}, "decoded": func(h *content.CompiledAssetModelHeaderDef) {
		for i := range h.Models {
			h.Models[i].DecodedBytes++
		}
	}, "base": func(h *content.CompiledAssetModelHeaderDef) {
		for i := range h.Models {
			h.Models[i].BaseIdentity = strings.Repeat("f", 64)
		}
	}, "lattice": func(h *content.CompiledAssetModelHeaderDef) {
		h.Asset.Parts[len(h.Asset.Parts)-1].VoxelResolution = .25
	}, "missing-unselected": func(h *content.CompiledAssetModelHeaderDef) { h.Models[1].Path = "missing-model.gkvox" }} {
		t.Run(name, func(t *testing.T) {
			path, h := c3g7Fixture(t)
			change(h)
			c3g7Save(t, path, h)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			if s, err := verifyCompiledAssetInput(path, owner, nil); err == nil || s != nil {
				if s != nil {
					s.close()
				}
				t.Fatal("invalid whole closure accepted")
			}
			if stats := owner.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
				t.Fatal("failed closure leaked leases", stats)
			}
		})
	}
	path, h := c3g7Fixture(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	cached, _, err := caller.Loader().LoadCompiledAssetModel(filepath.Join(filepath.Dir(path), h.Models[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	calls := 0
	s, err := verifyCompiledAssetInput(path, caller.Loader(), func() bool { calls++; return false })
	if err != nil {
		t.Fatal(err)
	}
	s.close()
	reached := 0
	if s, err := verifyCompiledAssetInput(path, caller.Loader(), func() bool { reached++; return reached == calls }); err == nil || s != nil {
		if s != nil {
			s.close()
		}
		t.Fatal("terminal cancellation accepted")
	}
	if after := owner.Stats(); after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes || after.Entries != before.Entries {
		t.Fatal("cancellation revoked caller pins/leaked session", after)
	}
	if cached.Bricks[0].Values[0] != 3 {
		t.Fatal("cancel invalidated caller model")
	}
	caller.Close()
	if s, err := verifyCompiledAssetInput(path, caller.Loader(), nil); err == nil || s != nil {
		t.Fatal("closed origin accepted")
	}
	lateOrigin := owner.NewScope()
	lateCalls := 0
	if s, err := verifyCompiledAssetInput(path, lateOrigin.Loader(), func() bool {
		lateCalls++
		if lateCalls == calls {
			lateOrigin.Close()
		}
		return false
	}); err == nil || s != nil {
		if s != nil {
			s.close()
		}
		t.Fatal("origin closed after loads accepted")
	}
	lateOrigin.Close()
	if stats := owner.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatal("late origin closure leaked child leases", stats)
	}
	// A corrupt unselected frame cannot be hidden by first-part shape validity.
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(path), h.Models[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), h.Models[0].Path), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := verifyCompiledAssetInput(path, NewRuntimeContentLoader(), nil); err == nil || s != nil {
		t.Fatal("corrupt model closure accepted")
	}
	if proof, err := loadRuntimeAssetCanonicalPart(NewRuntimeContentLoader(), path, h.Shapes[0].PartID, true, runtimeAssetCanonicalOptions{proveAuthoredBase: true}); err != nil || proof.identity != h.Shapes[0].BaseIdentity || proof.geometry == nil {
		t.Fatal("selected inline E2 followed unrelated corrupt model", err)
	}
}

func TestC3g7RejectUnsupportedAuthenticatedModelRaster(t *testing.T) {
	path, h := c3g7Fixture(t)
	modelPath := filepath.Join(filepath.Dir(path), h.Models[0].Path)
	model, _, err := content.LoadCompiledAssetModel(modelPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	model.Lattice.RasterizationVersion = "future-model-v99"
	info, err := content.SaveCompiledAssetModel(modelPath, model, nil)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := content.CompiledAssetModelBaseIdentity(model, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range h.Models {
		h.Models[i].ContentID = info.ContentID
		h.Models[i].BaseIdentity = base
		h.Models[i].EncodedBytes = info.EncodedBytes
		h.Models[i].DecodedBytes = info.DecodedBytes
	}
	c3g7Save(t, path, h)
	if session, err := verifyCompiledAssetInput(path, NewRuntimeContentLoader(), nil); err == nil || session != nil {
		if session != nil {
			session.close()
		}
		t.Fatal("fully authenticated unsupported model raster accepted")
	}
}
