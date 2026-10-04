package gekko

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3g10Fixture(t *testing.T) (string, string, *content.AssetDef) {
	t.Helper()
	input, _, asset := c3cFixture(t)
	source := filepath.Dir(input)
	writeNamedSceneVoxFixture(t, filepath.Join(source, "models.vox"))
	material := asset.Materials[0]
	material.ID = "second"
	material.BaseColor[0] = 77
	asset.Materials = append(asset.Materials, material)
	transform := asset.Parts[0].Transform
	for i, id := range []string{"proc", "proc-second", "vox", "scene"} {
		p := content.AssetPartDef{ID: id, Name: id, ParentID: "root", Transform: transform, ModelScale: 1.5, VoxelResolution: .125, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube", Params: c3g1Params(), MaterialID: "mat"}}
		if i == 1 {
			p.Source.MaterialID = "second"
		}
		if i >= 2 {
			p.Source = content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: "models.vox", ModelIndex: 1}
			if i == 3 {
				p.Source.Kind = content.AssetSourceKindVoxSceneNode
				p.Source.NodeName = "arm"
			}
		}
		asset.Parts = append(asset.Parts, p)
	}
	eligible := asset.Parts[0]
	eligible.ID = "eligible"
	eligible.Name = "Eligible"
	payload := *eligible.Source.VoxelShape
	payload.Palette = []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "mat"}}
	payload.Voxels = append([]content.VoxelObjectVoxelDef(nil), payload.Voxels...)
	for i := range payload.Voxels {
		payload.Voxels[i].Value = 3
	}
	eligible.Source.VoxelShape = &payload
	asset.Parts = append(asset.Parts, eligible)
	c3cWrite(t, input, asset)
	return input, filepath.Join(t.TempDir(), "shipping.gkmodelassetc"), asset
}

func TestC3g10MixedModelCompileSourceFreeAndNoOp(t *testing.T) {
	input, output, asset := c3g10Fixture(t)
	sourceBefore := c3cRead(t, input)
	result, err := CompileAuthoredModelAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HeaderWrote || result.ModelsWritten != 2 || result.ModelsReused != 0 || result.ShapesWritten != 2 || result.LODsWritten != 1 || result.DependenciesWritten != 4 {
		t.Fatalf("unique physical counts %+v", result)
	}
	header, hi, err := content.LoadCompiledAssetModelHeader(output, nil)
	if err != nil || hi != result.HeaderInfo || len(header.Models) != 4 || len(header.Shapes) != 3 || len(header.LODs) != 1 {
		t.Fatal("mixed closure refs", err)
	}
	if !bytes.Equal(sourceBefore, c3cRead(t, input)) {
		t.Fatal("compiler mutated authoring file")
	}
	refs := map[string]content.CompiledAssetModelRefDef{}
	for _, ref := range header.Models {
		refs[ref.PartID] = ref
		frame := c3cRead(t, filepath.Join(filepath.Dir(output), ref.Path))
		sum := sha256.Sum256(frame)
		if ref.Path != "models/"+hex.EncodeToString(sum[:])+".gkmodel" {
			t.Fatal("model path is not physical frame hash")
		}
		model, info, err := content.LoadCompiledAssetModel(filepath.Join(filepath.Dir(output), ref.Path), nil)
		if err != nil || info.ContentID != ref.ContentID || info.DecodedBytes != ref.DecodedBytes || info.EncodedBytes != ref.EncodedBytes {
			t.Fatal("model reference frame mismatch", err)
		}
		var original content.AssetPartDef
		for _, p := range asset.Parts {
			if p.ID == ref.PartID {
				original = p
			}
		}
		built, err := compileAssetPartModel(asset, original, input)
		if err != nil {
			t.Fatal(err)
		}
		if model.Dimensions != built.definition.Dimensions || !reflect.DeepEqual(c3g4Primary(model.Bricks), c3g4Primary(built.definition.Bricks)) {
			t.Fatal("shipping model differs from owned adapter")
		}
	}
	if refs["proc"].ContentID != refs["proc-second"].ContentID || refs["proc"].PaletteID == refs["proc-second"].PaletteID || refs["vox"].ContentID != refs["scene"].ContentID {
		t.Fatal("geometry/palette dedup coupled or scene transform baked")
	}
	for _, p := range header.Asset.Parts {
		if p.ID == "vox" && (p.Source.Kind != content.AssetSourceKindVoxModel || p.Source.Path != "models.vox") {
			t.Fatal("original VOX descriptor lost")
		}
	}
	if files, err := os.ReadDir(filepath.Join(filepath.Dir(output), "dependencies")); err != nil || len(files) != 4 {
		t.Fatal("VOX original incorrectly copied as shipping dependency", err)
	}
	paths := []string{output}
	for _, ref := range header.Models {
		paths = append(paths, filepath.Join(filepath.Dir(output), ref.Path))
	}
	snapshots := map[string][]byte{}
	mtimes := map[string]time.Time{}
	for _, path := range paths {
		snapshots[path] = c3cRead(t, path)
		stat, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		mtimes[path] = stat.ModTime()
	}
	repeat, err := CompileAuthoredModelAssetWithOptions(input, output, nil, CompiledAssetCompileOptions{EnableLOD2: true})
	if err != nil || repeat.HeaderWrote || repeat.ModelsWritten != 0 || repeat.ModelsReused != 2 || repeat.ShapesWritten != 0 || repeat.ShapesReused != 2 || repeat.LODsWritten != 0 || repeat.LODsReused != 1 || repeat.DependenciesWritten != 0 || repeat.DependenciesReused != 4 {
		t.Fatalf("no-op counts %+v %v", repeat, err)
	}
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil || !stat.ModTime().Equal(mtimes[path]) || !bytes.Equal(c3cRead(t, path), snapshots[path]) {
			t.Fatal("no-op rewrote immutable artifact", path, err)
		}
	}
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(output, assets, NewRuntimeContentLoader())
	if err != nil || prepared.parts["proc"].model == (AssetId{}) || prepared.parts["vox"].model == (AssetId{}) || prepared.parts["eligible"].compiledLOD == (AssetId{}) || prepared.parts["proc"].compiledLOD != (AssetId{}) {
		t.Fatal("shipping closure still requires original sources or misroutes model LOD", err)
	}
}

func TestC3g10LegacyPipelineGoldenAndProfileOwnership(t *testing.T) {
	input, oldOut, _ := c3cFixture(t)
	if _, err := CompileAuthoredAsset(input, oldOut, nil); err != nil {
		t.Fatal(err)
	}
	old, _, err := content.LoadCompiledAssetHeader(oldOut, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := sha256.Sum256(c3cRead(t, oldOut)); hex.EncodeToString(got[:]) != "be00fc5d5c0a8fda12369988711a0c966824a1bb3553e6b2a8d0be28f3c2b8e5" {
		t.Fatal("legacy compiler header bytes changed")
	}
	if got := sha256.Sum256(c3cRead(t, filepath.Join(filepath.Dir(oldOut), old.Shapes[0].Path))); hex.EncodeToString(got[:]) != "eb87a31ff439481954c5976f972b501462467fae7bbe309b8dda8b311143e539" {
		t.Fatal("legacy shape bytes changed")
	}
	mixed, output, _ := c3g10Fixture(t)
	if result, err := CompileAuthoredAsset(mixed, oldOut, nil); err == nil || result != (CompiledAssetCompileResult{}) {
		t.Fatal("old compiler accepted model kinds")
	}
	plain, err := CompileAuthoredModelAsset(mixed, output, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := content.LoadCompiledAssetModelHeader(output, nil)
	if err != nil {
		t.Fatal(err)
	}
	codec := c3cCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 91, Bytes: bytes.Repeat([]byte("compiled_asset_model_header compiled_asset_model colors dimensions lattice"), 16)}})
	dict, err := CompileAuthoredModelAsset(mixed, output, codec)
	if err != nil || dict.ModelsWritten != 2 || dict.HeaderInfo.ContentID == plain.HeaderInfo.ContentID || dict.HeaderInfo.DictionaryID != 91 {
		t.Fatal("physical profiles changed model logical identity", err)
	}
	second, _, err := content.LoadCompiledAssetModelHeader(output, codec)
	if err != nil {
		t.Fatal(err)
	}
	// Physical reference paths differ, so header logical identity also differs even though each model CID is stable.
	for i := range first.Models {
		if first.Models[i].ContentID != second.Models[i].ContentID || first.Models[i].Path == second.Models[i].Path {
			t.Fatal("model dictionary namespace aliases physical profile")
		}
	}
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec})
	if s, err := verifyCompiledAssetInput(output, owner, nil); err != nil {
		t.Fatal("shipping borrowed profile verification", err)
	} else {
		s.close()
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "borrowed-probe"}); err != nil {
		t.Fatal("compiler closed borrowed codec", err)
	}
}

func TestC3g10PreflightFailuresPreserveExistingClosure(t *testing.T) {
	input, output, asset := c3g10Fixture(t)
	_, err := CompileAuthoredModelAsset(input, output, nil)
	if err != nil {
		t.Fatal(err)
	}
	oldHeader := c3cRead(t, output)
	header, _, err := content.LoadCompiledAssetModelHeader(output, nil)
	if err != nil {
		t.Fatal(err)
	}
	modelPath := filepath.Join(filepath.Dir(output), header.Models[0].Path)
	corrupt := c3cRead(t, modelPath)
	corrupt[len(corrupt)-1] ^= 1
	if err := os.WriteFile(modelPath, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	// Introduce a new model simultaneously: existing immutable collision must preflight first.
	asset.Parts[3].Source.Params["sx"] = 7
	c3cWrite(t, input, asset)
	before, err := os.ReadDir(filepath.Join(filepath.Dir(output), "models"))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := CompileAuthoredModelAsset(input, output, nil); err == nil || !reflect.DeepEqual(result, CompiledAssetModelCompileResult{}) {
		t.Fatal("immutable collision accepted or partial result")
	}
	after, err := os.ReadDir(filepath.Join(filepath.Dir(output), "models"))
	if err != nil || len(after) != len(before) || !bytes.Equal(c3cRead(t, output), oldHeader) {
		t.Fatal("preflight failure wrote new models/header", err)
	}
	for _, suffix := range []string{".gkassetc", ".GKMODELASSETC", ".json"} {
		bad := filepath.Join(t.TempDir(), "out"+suffix)
		if result, err := CompileAuthoredModelAsset("missing-input", bad, nil); err == nil || !strings.Contains(err.Error(), ".gkmodelassetc") || !reflect.DeepEqual(result, CompiledAssetModelCompileResult{}) {
			t.Fatal("output selector not checked before input IO")
		}
	}
	for name, change := range map[string]func(*content.AssetDef){"collapse": func(a *content.AssetDef) { a.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true} }, "missing-model": func(a *content.AssetDef) { a.Parts[5].Source.Path = "missing.vox" }, "missing-material": func(a *content.AssetDef) { a.Parts[3].Source.MaterialID = "missing" }, "missing-dependency": func(a *content.AssetDef) { a.AnimationSetPaths[0] = "missing.gkanim" }} {
		t.Run(name, func(t *testing.T) {
			in, out, a := c3g10Fixture(t)
			change(a)
			c3cWrite(t, in, a)
			if result, err := CompileAuthoredModelAsset(in, out, nil); err == nil || !reflect.DeepEqual(result, CompiledAssetModelCompileResult{}) {
				t.Fatal("invalid authoring accepted")
			}
			if _, err := os.Stat(filepath.Dir(out)); err == nil {
				entries, err := os.ReadDir(filepath.Dir(out))
				if err != nil || len(entries) != 0 {
					t.Fatal("failed preflight published files")
				}
			}
		})
	}
	// Exact output extension still must not permit a source hardlink alias.
	in, out, _ := c3g10Fixture(t)
	vox := filepath.Join(filepath.Dir(in), "models.vox")
	if err := os.Link(vox, out); err != nil {
		t.Fatal(err)
	}
	original := c3cRead(t, vox)
	if result, err := CompileAuthoredModelAsset(in, out, nil); err == nil || !reflect.DeepEqual(result, CompiledAssetModelCompileResult{}) {
		t.Fatal("VOX source output alias accepted")
	}
	if !bytes.Equal(original, c3cRead(t, vox)) {
		t.Fatal("alias preflight changed original VOX")
	}
}
