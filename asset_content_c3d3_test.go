package gekko

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func c3d3Fixture(t *testing.T) (string, string, *content.AssetDef) {
	t.Helper()
	input, out, asset := c3cFixture(t)
	asset.Materials[0].Roughness = 0.35
	asset.Materials[0].Metallic = 0.65
	asset.Materials[0].Emissive = 0.25
	asset.Materials[0].Tags = []string{"surface:metal", "material-tag"}
	c3cWrite(t, input, asset)
	var encoded bytes.Buffer
	pixel := image.NewRGBA(image.Rect(0, 0, 1, 1))
	pixel.SetRGBA(0, 0, color.RGBA{R: 20, G: 40, B: 60, A: 255})
	if err := png.Encode(&encoded, pixel); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(input), "sprite.png"), encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	out = strings.TrimSuffix(out, ".gkasset") + ".gkassetc"
	if _, err := CompileAuthoredAsset(input, out, nil); err != nil {
		t.Fatal(err)
	}
	return input, out, asset
}
func c3d3Server() *AssetServer {
	assets := newSpawnTestAssetServer()
	assets.textures = make(map[AssetId]TextureAsset)
	assets.textureKeys = make(map[string]AssetId)
	return assets
}
func c3d3CopyClosure(t *testing.T, from, to string) {
	t.Helper()
	if err := filepath.WalkDir(from, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		return os.WriteFile(dest, c3cRead(t, path), 0600)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestC3d3CompiledPreparationSpawnParitySharingAndHeaderOwnership(t *testing.T) {
	input, path, authored := c3d3Fixture(t)
	legacyAssets, compiledAssets := c3d3Server(), c3d3Server()
	legacyDef, err := content.LoadAsset(input)
	if err != nil {
		t.Fatal(err)
	}
	legacyDef.Emitters[0].Emitter.TexturePath = filepath.Join(filepath.Dir(input), "sprite.png")
	legacy, err := PrepareAuthoredAsset(legacyAssets, legacyDef, input)
	if err != nil {
		t.Fatal(err)
	}
	appLegacy := NewApp()
	root := TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
	legacyResult, err := SpawnPreparedAuthoredAsset(appLegacy.Commands(), legacyAssets, legacy, root)
	if err != nil {
		t.Fatal(err)
	}
	appLegacy.FlushCommands()
	loader := NewRuntimeContentLoader()
	cached, _, err := loader.LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(cached)
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	compiled, err := LoadAndPrepareAuthoredAsset(path, compiledAssets, loader)
	if err != nil {
		t.Fatal("compiled preparation failed", err)
	}
	if !PreparedAuthoredAssetHasMarkerKind(compiled, content.AssetMarkerKindMuzzle) {
		t.Fatal("compiled preparation lost marker")
	}
	for _, partID := range []string{"shape", "duplicate"} {
		actual, ok := PreparedAuthoredAssetPartGeometry(compiled, partID)
		expected, expectedOK := PreparedAuthoredAssetPartGeometry(legacy, partID)
		if !ok || !expectedOK {
			t.Fatal("prepared shape missing")
		}
		ag, _ := compiledAssets.getVoxelGeometry(actual)
		eg, _ := legacyAssets.getVoxelGeometry(expected)
		if !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(ag.XBrickMap).Voxels), c3cGeometry(VoxelObjectSnapshotFromXBrickMap(eg.XBrickMap).Voxels)) {
			t.Fatal("compiled preparation rescaled or changed signed primary geometry")
		}
		if got, ok := PreparedAuthoredAssetPartLocalTransform(compiled, partID); !ok {
			t.Fatal("compiled transform missing")
		} else if want, _ := PreparedAuthoredAssetPartLocalTransform(legacy, partID); got != want {
			t.Fatal("compiled local transform changed")
		}
	}
	shapeID, _ := PreparedAuthoredAssetPartGeometry(compiled, "shape")
	duplicateID, _ := PreparedAuthoredAssetPartGeometry(compiled, "duplicate")
	if shapeID != duplicateID {
		t.Fatal("identical primary geometry not shared")
	}
	again, err := LoadAndPrepareAuthoredAsset(path, compiledAssets, loader)
	if err != nil {
		t.Fatal(err)
	}
	againID, _ := PreparedAuthoredAssetPartGeometry(again, "shape")
	if againID != shapeID {
		t.Fatal("repeat preparation registered new geometry")
	}
	copiedRoot := t.TempDir()
	c3d3CopyClosure(t, filepath.Dir(path), copiedRoot)
	copyPrepared, err := LoadAndPrepareAuthoredAsset(filepath.Join(copiedRoot, filepath.Base(path)), compiledAssets, loader)
	if err != nil {
		t.Fatal(err)
	}
	copiedID, _ := PreparedAuthoredAssetPartGeometry(copyPrepared, "shape")
	if copiedID != shapeID {
		t.Fatal("physical closure location changed geometry sharing")
	}
	after, _ := json.Marshal(cached)
	if !bytes.Equal(before, after) {
		t.Fatal("compiled preparation mutated cached header metadata")
	}
	appCompiled := NewApp()
	compiledResult, err := SpawnPreparedAuthoredAsset(appCompiled.Commands(), compiledAssets, compiled, root)
	if err != nil {
		t.Fatal("compiled spawn failed after source deletion", err)
	}
	appCompiled.FlushCommands()
	for _, part := range authored.Parts {
		eid := compiledResult.EntitiesByAssetID[part.ID]
		legacyEID := legacyResult.EntitiesByAssetID[part.ID]
		if *s3cComponent[LocalTransformComponent](t, appCompiled.Commands(), eid) != *s3cComponent[LocalTransformComponent](t, appLegacy.Commands(), legacyEID) {
			t.Fatal("compiled ECS local transform differs")
		}
		if part.ParentID != "" && s3cComponent[Parent](t, appCompiled.Commands(), eid).Entity != compiledResult.EntitiesByAssetID[part.ParentID] {
			t.Fatal("compiled ECS hierarchy lost")
		}
		if part.Source.Kind == content.AssetSourceKindVoxelShape {
			vmc := s3cComponent[VoxelModelComponent](t, appCompiled.Commands(), eid)
			old := s3cComponent[VoxelModelComponent](t, appLegacy.Commands(), legacyEID)
			if vmc.VoxelResolution != old.VoxelResolution || vmc.PivotMode != old.PivotMode || vmc.CustomPivot != old.CustomPivot {
				t.Fatal("compiled ECS voxel lattice/pivot differs")
			}
			palette, ok := compiledAssets.GetVoxelPalette(vmc.VoxelPalette)
			oldPalette, oldOK := legacyAssets.GetVoxelPalette(old.VoxelPalette)
			palette.SourcePath = ""
			oldPalette.SourcePath = ""
			if !ok || !oldOK || !reflect.DeepEqual(palette, oldPalette) {
				t.Fatal("compiled ECS palette/material semantic tables differ")
			}
		}
	}
	marker := s3cComponent[AuthoredMarkerComponent](t, appCompiled.Commands(), compiledResult.EntitiesByAssetID["marker"])
	oldMarker := s3cComponent[AuthoredMarkerComponent](t, appLegacy.Commands(), legacyResult.EntitiesByAssetID["marker"])
	if !reflect.DeepEqual(marker, oldMarker) {
		t.Fatal("compiled ECS marker differs")
	}
	animations := s3cComponent[AuthoredAssetAnimationSetComponent](t, appCompiled.Commands(), compiledResult.RootEntity)
	oldAnimations := s3cComponent[AuthoredAssetAnimationSetComponent](t, appLegacy.Commands(), legacyResult.RootEntity)
	if !reflect.DeepEqual(animations, oldAnimations) {
		t.Fatal("compiled skeleton/rig/direct animation binding differs")
	}
	emitter := s3cComponent[ParticleEmitterComponent](t, appCompiled.Commands(), compiledResult.EntitiesByAssetID["emitter"])
	if emitter.Texture == (AssetId{}) || !bytes.Equal(compiledAssets.textures[emitter.Texture].Texels, []byte{20, 40, 60, 255}) {
		t.Fatal("compiled emitter failed header-relative texture resolution")
	}
	_, lattice, baseID := e2b1Canonical(t, authored.Parts[0])
	if compiledAssets.authoredVoxelBaseIdentity(shapeID, lattice) != baseID {
		t.Fatal("compiled geometry lost original authored provenance")
	}
	shapeEntity := compiledResult.EntitiesByAssetID["shape"]
	if err := EnableManagedVoxelGeometry(appCompiled.Commands(), compiledAssets, shapeEntity); err != nil {
		t.Fatal(err)
	}
	appCompiled.FlushCommands()
	if identity, gotLattice, ok := managedVoxelGeometryBase(appCompiled.Commands(), compiledAssets, shapeEntity); !ok || identity != baseID || gotLattice != lattice {
		t.Fatal("compiled spawn did not qualify original managed base")
	}
	sharedBefore, _ := compiledAssets.getVoxelGeometry(shapeID)
	beforeValues := c3cGeometry(VoxelObjectSnapshotFromXBrickMap(sharedBefore.XBrickMap).Voxels)
	if err := ApplyManagedVoxelWrites(appCompiled.Commands(), compiledAssets, shapeEntity, p1dWrites(volume.VoxelWrite{X: -18, Value: 77})); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(sharedBefore.XBrickMap).Voxels), beforeValues) {
		t.Fatal("managed compiled edit changed shared source geometry")
	}
	sibling := s3cComponent[VoxelModelComponent](t, appCompiled.Commands(), compiledResult.EntitiesByAssetID["duplicate"])
	if sibling.GeometryAsset() != shapeID {
		t.Fatal("managed compiled edit changed sibling geometry binding")
	}
	own := s3cComponent[VoxelModelComponent](t, appCompiled.Commands(), shapeEntity)
	managed, ok := compiledAssets.getVoxelGeometry(own.GeometryAsset())
	if !ok {
		t.Fatal("managed compiled geometry missing")
	}
	if found, value := managed.XBrickMap.GetVoxel(-18, 0, 0); !found || value != 77 {
		t.Fatal("compiled managed edit missing")
	}
	mutable, ok := compiledAssets.GetVoxelGeometry(shapeID)
	if !ok {
		t.Fatal("compiled geometry missing")
	}
	mutable.XBrickMap.SetVoxel(-18, 0, 0, 99)
	if _, err := LoadAndPrepareAuthoredAsset(path, compiledAssets, loader); err != nil {
		t.Fatal(err)
	}
	if compiledAssets.authoredVoxelBaseIdentity(shapeID, lattice) != baseID {
		t.Fatal("original provenance was recomputed from mutated warm geometry")
	}
}

func TestC3d3PreparationVerifiesWholeClosureBeforePublicationAndPreservesPins(t *testing.T) {
	for _, kind := range []string{"later-frame", "later-content", "later-base", "later-lattice", "missing-animation", "absolute-animation", "parent-rig", "absolute-rig"} {
		t.Run(kind, func(t *testing.T) {
			_, path, _ := c3d3Fixture(t)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			scope := owner.NewScope()
			defer scope.Close()
			accepted, _, err := scope.Loader().LoadCompiledAssetHeader(path)
			if err != nil {
				t.Fatal(err)
			}
			originalShape := filepath.Join(filepath.Dir(path), accepted.Shapes[0].Path)
			acceptedShape, _, err := scope.Loader().LoadCompiledAssetShape(originalShape)
			if err != nil {
				t.Fatal(err)
			}
			before := owner.Stats()
			bad, _, err := content.LoadCompiledAssetHeader(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			badPath := filepath.Join(filepath.Dir(path), "bad.gkassetc")
			// Break the part authored last, while earlier parts reference valid geometry.
			index := 0
			laterPath := filepath.Join(filepath.Dir(path), "later.gkshape")
			if err := os.WriteFile(laterPath, c3cRead(t, originalShape), 0600); err != nil {
				t.Fatal(err)
			}
			bad.Shapes[index].Path = "later.gkshape"
			switch kind {
			case "later-frame":
				raw := c3cRead(t, laterPath)
				raw[len(raw)-1] ^= 1
				if err := os.WriteFile(laterPath, raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "later-content":
				bad.Shapes[index].ContentID = strings.Repeat("c", 64)
			case "later-base":
				bad.Shapes[index].BaseIdentity = strings.Repeat("c", 64)
			case "later-lattice":
				bad.Asset.Parts[2].VoxelResolution *= 2
			case "missing-animation":
				bad.Asset.AnimationSetPaths[0] = "dependencies/absent.gkanim"
			case "absolute-animation":
				bad.Asset.AnimationSetPaths[0] = filepath.Join(filepath.Dir(path), bad.Asset.AnimationSetPaths[0])
			case "parent-rig", "absolute-rig":
				setPath := filepath.Join(filepath.Dir(path), bad.Asset.AnimationSetPaths[0])
				set, err := content.LoadAnimationSet(setPath)
				if err != nil {
					t.Fatal(err)
				}
				realRig := filepath.Join(filepath.Dir(setPath), set.RigPath)
				if kind == "absolute-rig" {
					set.RigPath = realRig
				} else {
					set.RigPath = "../" + filepath.Base(realRig)
					if err := os.WriteFile(filepath.Join(filepath.Dir(path), filepath.Base(realRig)), c3cRead(t, realRig), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := content.SaveAnimationSet(setPath, set); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := content.SaveCompiledAssetHeader(badPath, bad, nil); err != nil {
				t.Fatal(err)
			}
			assets := c3d3Server()
			if prepared, err := LoadAndPrepareAuthoredAsset(badPath, assets, scope.Loader()); err == nil || prepared != nil {
				t.Fatal("invalid compiled closure prepared")
			}
			if len(assets.voxModels) != 0 || len(assets.voxPalettes) != 0 {
				t.Fatal("partial compiled geometry/palette published before whole closure proof")
			}
			after := owner.Stats()
			if after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
				t.Fatalf("failed preparation revoked caller pins or leaked child scope: before=%+v after=%+v", before, after)
			}
			if got, _, err := scope.Loader().LoadCompiledAssetHeader(path); err != nil || got != accepted {
				t.Fatal("caller header pin lost", err)
			}
			if got, _, err := scope.Loader().LoadCompiledAssetShape(originalShape); err != nil || got != acceptedShape {
				t.Fatal("caller SAME shape pin lost", err)
			}
		})
	}
}

func TestC3d3StrictClosurePathsIgnoreCWDDecoys(t *testing.T) {
	for _, missing := range []string{"none", "animation", "rig"} {
		t.Run(missing, func(t *testing.T) {
			input, path, _ := c3d3Fixture(t)
			header, _, err := content.LoadCompiledAssetHeader(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			c3d3CopyClosure(t, filepath.Dir(path), cwd)
			// A cwd texture with the same relative reference must never override the closure.
			if err := os.WriteFile(filepath.Join(cwd, header.Asset.Emitters[0].Emitter.TexturePath), []byte("not a png"), 0600); err != nil {
				t.Fatal(err)
			}
			setPath := filepath.Join(filepath.Dir(path), header.Asset.AnimationSetPaths[0])
			set, err := content.LoadAnimationSet(setPath)
			if err != nil {
				t.Fatal(err)
			}
			if missing == "animation" {
				if err := os.Remove(setPath); err != nil {
					t.Fatal(err)
				}
			}
			if missing == "rig" {
				if err := os.Remove(filepath.Join(filepath.Dir(setPath), set.RigPath)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(cwd, set.RigPath), c3cRead(t, filepath.Join(filepath.Dir(input), "rig.gkrig")), 0600); err != nil {
					t.Fatal(err)
				}
			}
			t.Chdir(cwd)
			assets := c3d3Server()
			prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
			if missing != "none" {
				if err == nil || prepared != nil || len(assets.voxModels) != 0 {
					t.Fatal("compiled dependency borrowed cwd decoy or partially published")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			app := NewApp()
			if _, err := SpawnPreparedAuthoredAsset(app.Commands(), assets, prepared, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}); err != nil {
				t.Fatal("compiled texture borrowed cwd decoy", err)
			}
		})
	}
}

func TestC3d3SharedGeometryDistinctPalettesAndSelectedAPIs(t *testing.T) {
	input, path, asset := c3d3Fixture(t)
	asset.Materials = append(asset.Materials, content.AssetMaterialDef{ID: "other", Name: "Other", IOR: 1.5, BaseColor: [4]uint8{200, 100, 50, 255}})
	asset.Parts[2].Source.VoxelShape = &content.AssetVoxelShapeDef{Voxels: append([]content.VoxelObjectVoxelDef(nil), asset.Parts[0].Source.VoxelShape.Voxels...), Palette: []content.AssetVoxelPaletteEntryDef{{Value: 3, MaterialID: "other"}, {Value: 4, MaterialID: "other"}}}
	c3cWrite(t, input, asset)
	if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
		t.Fatal(err)
	}
	assets := c3d3Server()
	prepared, err := LoadAndPrepareAuthoredAsset(path, assets, nil)
	if err != nil {
		t.Fatal(err)
	}
	geometryA, _ := PreparedAuthoredAssetPartGeometry(prepared, "shape")
	geometryB, _ := PreparedAuthoredAssetPartGeometry(prepared, "duplicate")
	paletteA, _ := PreparedAuthoredAssetPartPalette(prepared, "shape")
	paletteB, _ := PreparedAuthoredAssetPartPalette(prepared, "duplicate")
	if geometryA != geometryB || paletteA == paletteB {
		t.Fatal("primary geometry sharing confused different material bindings")
	}
	a, _ := assets.GetVoxelPalette(paletteA)
	b, _ := assets.GetVoxelPalette(paletteB)
	if a.VoxPalette[3] != [4]uint8{10, 20, 30, 255} || b.VoxPalette[3] != [4]uint8{200, 100, 50, 255} {
		t.Fatal("prepared palettes lost own material binding")
	}
	metadataOnly, err := LoadAndPrepareAuthoredAsset(path, nil, nil)
	if err != nil || metadataOnly == nil || !PreparedAuthoredAssetHasMarkerKind(metadataOnly, content.AssetMarkerKindMuzzle) {
		t.Fatal("nil asset server metadata-only preparation changed", err)
	}
	if id, ok := PreparedAuthoredAssetPartGeometry(metadataOnly, "shape"); ok || id != (AssetId{}) {
		t.Fatal("nil asset server published geometry")
	}
	app := NewApp()
	if result, err := LoadAndSpawnAuthoredAsset(path, app.Commands(), assets, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}); err != nil || result.AssetID != "asset" {
		t.Fatal("selected compiled load-and-spawn failed", err)
	}
	wrong := filepath.Join(filepath.Dir(path), "asset.GKASSETC")
	if err := os.WriteFile(wrong, c3cRead(t, path), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := LoadAndPrepareAuthoredAsset(wrong, assets, nil); err == nil || result != nil {
		t.Fatal("wrong extension autodetected compiled frame")
	}
	selectedJSON := filepath.Join(filepath.Dir(path), "json.gkassetc")
	c3cWrite(t, selectedJSON, asset)
	if result, err := LoadAndPrepareAuthoredAsset(selectedJSON, assets, nil); err == nil || result != nil {
		t.Fatal("compiled preparation fell back to JSON")
	}
	scope := NewRuntimeContentLoader().NewScope()
	scope.Close()
	if result, err := LoadAndPrepareAuthoredAsset(path, assets, scope.Loader()); err == nil || result != nil {
		t.Fatal("compiled preparation bypassed closed origin")
	}
}

func TestC3d3WarmPreparationAfterBorrowedCodecClosePreservesPins(t *testing.T) {
	_, path, _ := c3d3Fixture(t)
	codec := c3cCodec(t, voxelcodec.Options{})
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec, MaxCacheBytes: -1})
	scope := owner.NewScope()
	defer scope.Close()
	header, _, err := scope.Loader().LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range header.Shapes {
		if _, _, err := scope.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), ref.Path)); err != nil {
			t.Fatal(err)
		}
	}
	before := owner.Stats()
	metadata, _ := json.Marshal(header)
	if err := codec.Close(); err != nil {
		t.Fatal(err)
	}
	prepared, err := LoadAndPrepareAuthoredAsset(path, c3d3Server(), scope.Loader())
	if err != nil || prepared == nil {
		t.Fatal("warm preparation depends on closed borrowed codec", err)
	}
	after := owner.Stats()
	afterMetadata, _ := json.Marshal(header)
	if before.Entries != after.Entries || before.Bytes != after.Bytes || before.PinnedBytes != after.PinnedBytes || !bytes.Equal(metadata, afterMetadata) {
		t.Fatal("warm preparation revoked caller pins, leaked child pins, or mutated header")
	}
	if _, ok := PreparedAuthoredAssetPartGeometry(prepared, "shape"); !ok {
		t.Fatal("warm compiled geometry missing")
	}
}
