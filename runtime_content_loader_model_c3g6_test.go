package gekko

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3g6Frames(t *testing.T, codec *voxelcodec.Codec) (string, string, voxelcodec.Info, voxelcodec.Info) {
	t.Helper()
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "model.gkvox")
	headerPath := filepath.Join(dir, "header.gkasset")
	model := &content.CompiledAssetModelDef{SchemaVersion: 1, Lattice: content.VoxelObjectLatticeDef{VoxelResolution: .125, RasterizationVersion: "gekko-compiled-model-v1"}, Dimensions: [3]uint32{2, 3, 4}, Bricks: []voxelcodec.Brick{{Occupancy: [8]uint64{1}, Values: []uint8{3}}}}
	modelInfo, err := content.SaveCompiledAssetModel(modelPath, model, codec)
	if err != nil {
		t.Fatal(err)
	}
	base, _, err := content.CompiledAssetModelBaseIdentity(model, codec)
	if err != nil {
		t.Fatal(err)
	}
	palette := content.CompiledAssetModelPaletteDef{IsPBR: true, Roughness: 1, IOR: 1.5}
	for i := range palette.Colors {
		palette.Colors[i] = [4]uint8{255, 255, 255, 255}
	}
	palette.ID, err = content.CompiledAssetModelPaletteIdentity(&palette)
	if err != nil {
		t.Fatal(err)
	}
	asset := &content.AssetDef{SchemaVersion: 4, ID: "loader-model", Name: "Loader model", Parts: []content.AssetPartDef{{ID: "part", Name: "Part", Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}, ModelScale: 1, VoxelResolution: .125, Source: content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube", Params: map[string]float32{"sx": 2, "sy": 3, "sz": 4}}}}}
	header := &content.CompiledAssetModelHeaderDef{SchemaVersion: 1, CompilerVersion: content.CurrentCompiledAssetModelHeaderCompilerVersion, Asset: asset, Models: []content.CompiledAssetModelRefDef{{PartID: "part", Path: "missing/not-followed.gkvox", ContentID: modelInfo.ContentID, BaseIdentity: base, EncodedBytes: modelInfo.EncodedBytes, DecodedBytes: modelInfo.DecodedBytes, PaletteID: palette.ID}}, Palettes: []content.CompiledAssetModelPaletteDef{palette}}
	headerInfo, err := content.SaveCompiledAssetModelHeader(headerPath, header, codec)
	if err != nil {
		t.Fatal(err)
	}
	return modelPath, headerPath, modelInfo, headerInfo
}

func TestC3g6ScopedSharedModelLoadsAndCharge(t *testing.T) {
	modelPath, headerPath, mi, hi := c3g6Frames(t, nil)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1})
	a, b := owner.NewScope(), owner.NewScope()
	defer a.Close()
	defer b.Close()
	type result struct {
		model  *content.CompiledAssetModelDef
		header *content.CompiledAssetModelHeaderDef
		info   voxelcodec.Info
		err    error
	}
	start, done := make(chan struct{}), make(chan result, 8)
	for i := 0; i < 8; i++ {
		l := a.Loader()
		if i%2 == 1 {
			l = b.Loader()
		}
		header := i >= 4
		go func() {
			<-start
			r := result{}
			if header {
				r.header, r.info, r.err = l.LoadCompiledAssetModelHeader(headerPath)
			} else {
				r.model, r.info, r.err = l.LoadCompiledAssetModel(modelPath)
			}
			done <- r
		}()
	}
	close(start)
	var model *content.CompiledAssetModelDef
	var header *content.CompiledAssetModelHeaderDef
	for i := 0; i < 8; i++ {
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		if r.model != nil {
			if r.info != mi {
				t.Fatal("model info lost")
			}
			if model != nil && model != r.model {
				t.Fatal("model scope pointer sharing lost")
			}
			model = r.model
		} else {
			if r.header == nil || r.info != hi {
				t.Fatal("header result lost")
			}
			if header != nil && header != r.header {
				t.Fatal("header scope pointer sharing lost")
			}
			header = r.header
		}
	}
	stats := owner.Stats()
	if stats.Entries != 2 || stats.Bytes <= 0 || stats.PinnedBytes != stats.Bytes || stats.OverBudgetBytes != stats.Bytes-1 || stats.Misses-stats.LoadWaits != 2 {
		t.Fatalf("typed wrappers bypass shared owner: %+v", stats)
	}
	// Selective release must recognize the public definitions behind typed wrappers.
	a.Loader().releaseScopedValue(model)
	a.Loader().releaseScopedValue(header)
	if next := owner.Stats(); next.Bytes != stats.Bytes || next.PinnedBytes != stats.PinnedBytes {
		t.Fatal("selective release revoked second borrower")
	}
	b.Loader().releaseScopedValue(model)
	if next := owner.Stats(); next.Entries != 1 || next.Bytes <= 0 || next.Bytes >= stats.Bytes || next.PinnedBytes != next.Bytes {
		t.Fatalf("model release did not remove only model storage: %+v", next)
	}
	owner.Clear()
	if owner.Stats().Entries != 1 {
		t.Fatal("Clear revoked pinned header")
	}
	b.Close()
	if next := owner.Stats(); next.Entries != 0 || next.Bytes != 0 || next.PinnedBytes != 0 {
		t.Fatal("final release retained storage", next)
	}
	if model.Bricks[0].Values[0] != 3 || header.Asset.ID != "loader-model" {
		t.Fatal("cache eviction invalidated borrowers")
	}
	a.Close()
	if out, info, err := a.Loader().LoadCompiledAssetModel(modelPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed scope accepted model")
	}
	if out, info, err := a.Loader().LoadCompiledAssetModelHeader(headerPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed scope accepted header")
	}
}

func TestC3g6KindIsolationWarmLifetimeAndNilLoader(t *testing.T) {
	modelPath, headerPath, mi, hi := c3g6Frames(t, nil)
	owner := NewRuntimeContentLoader()
	model, info, err := owner.LoadCompiledAssetModel(modelPath)
	if err != nil || info != mi {
		t.Fatal(err)
	}
	header, info, err := owner.LoadCompiledAssetModelHeader(headerPath)
	if err != nil || info != hi {
		t.Fatal(err)
	}
	data, err := os.ReadFile(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modelPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	same, info, err := owner.LoadCompiledAssetModel(modelPath)
	if err != nil || same != model || info != mi {
		t.Fatal("warm immutable path contract changed", err)
	}
	other, info, err := owner.LoadCompiledAssetModelHeader(modelPath)
	if err != nil || info != hi || other == header {
		t.Fatal("same-path different kind entry collided", err)
	}
	owner.Clear()
	if out, info, err := owner.LoadCompiledAssetModel(modelPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("fresh wrong-kind frame accepted")
	}
	if fresh, info, err := owner.LoadCompiledAssetModelHeader(modelPath); err != nil || fresh == nil || fresh == other || info != hi {
		t.Fatal("Clear did not permit fresh same-path header decode", err)
	}
	var nilLoader *RuntimeContentLoader
	if out, info, err := nilLoader.LoadCompiledAssetModelHeader(headerPath); err != nil || out == nil || info != hi {
		t.Fatal("nil loader standalone header", err)
	}
	restored, _, _, _ := c3g6Frames(t, nil)
	if out, info, err := nilLoader.LoadCompiledAssetModel(restored); err != nil || out == nil || info != mi {
		t.Fatal("nil loader standalone model", err)
	}
	if out, info, err := nilLoader.LoadCompiledAssetModel(""); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("empty model path accepted")
	}
	if out, info, err := owner.LoadCompiledAssetModelHeader(""); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("empty header path accepted")
	}
}

func TestC3g6FixedBorrowedProfileAndFailedLoads(t *testing.T) {
	codec, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 73, Bytes: bytes.Repeat([]byte("compiled_asset_model_header compiled_asset_model dimensions palette source"), 16)}})
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	modelPath, headerPath, mi, hi := c3g6Frames(t, codec)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec})
	scope := owner.NewScope()
	model, info, err := scope.Loader().LoadCompiledAssetModel(modelPath)
	if err != nil || info != mi || mi.DictionaryID != 73 {
		t.Fatal("model fixed dictionary profile", err)
	}
	header, info, err := scope.Loader().LoadCompiledAssetModelHeader(headerPath)
	if err != nil || info != hi || hi.DictionaryID != 73 {
		t.Fatal("header fixed dictionary profile", err)
	}
	wrong, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 73, Bytes: []byte("wrong")}})
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	for _, profile := range []*voxelcodec.Codec{nil, wrong} {
		l := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: profile})
		if out, info, err := l.LoadCompiledAssetModel(modelPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("wrong model dictionary accepted")
		}
		if out, info, err := l.LoadCompiledAssetModelHeader(headerPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("wrong header dictionary accepted")
		}
		if stats := l.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
			t.Fatal("failed profile retained data", stats)
		}
	}
	scope.Close()
	owner.Clear()
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "borrowed-probe"}); err != nil {
		t.Fatal("scope/cache closed borrowed codec", err)
	}
	for name, data := range map[string][]byte{"authoring": []byte(`{"schema_version":4,"id":"json"}`), "corrupt": []byte("broken frame")} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if out, info, err := owner.LoadCompiledAssetModel(path); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("invalid model frame fallback")
		}
		if out, info, err := owner.LoadCompiledAssetModelHeader(path); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("invalid header frame fallback")
		}
	}
	if stats := owner.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatal("failed frames retained entries", stats)
	}
	codec.Close()
	if out, info, err := owner.LoadCompiledAssetModel(modelPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("fresh model load ignored closed codec")
	}
	if out, info, err := owner.LoadCompiledAssetModelHeader(headerPath); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("fresh header load ignored closed codec")
	}
	if model.Bricks[0].Values[0] != 3 || header.Asset.ID != "loader-model" {
		t.Fatal("profile cleanup invalidated borrowers")
	}
}
