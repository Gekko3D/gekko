package hl1

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
	"github.com/go-gl/mathgl/mgl32"
)

func TestBuildGameAssetImportCatalogsAndCopiesMapAssets(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	outDir := filepath.Join(dir, "out")
	wadPath := filepath.Join(gameDir, "valve", "halflife.wad")
	modelPath := filepath.Join(gameDir, "valve", "models", "w_9mmhandgun.mdl")
	npcModelPath := filepath.Join(gameDir, "valve", "models", "barney.mdl")
	spritePath := filepath.Join(gameDir, "valve", "sprites", "glow01.spr")
	soundPath := filepath.Join(gameDir, "valve", "sound", "buttons", "bell1.wav")
	mustWriteFile(t, wadPath, []byte("wad"))
	mustWriteFile(t, modelPath, syntheticMDLWithBoneAndSequence())
	mustWriteFile(t, npcModelPath, syntheticMDL())
	mustWriteFile(t, spritePath, syntheticSPR())
	mustWriteFile(t, soundPath, []byte("sound"))

	summary := ImportSummary{
		Map: importcommon.MapImport{
			Entities: []importcommon.Entity{
				{
					ClassName: "worldspawn",
					KeyValues: map[string]string{
						"wad": "\\quiver\\valve\\halflife.wad",
					},
				},
				{
					ClassName: "weapon_9mmhandgun",
				},
				{
					ClassName: "monster_barney",
				},
				{
					ClassName: "env_sprite",
					KeyValues: map[string]string{
						"model": "sprites/glow01.spr",
					},
				},
				{
					ClassName: "ambient_generic",
					KeyValues: map[string]string{
						"message": "buttons/bell1.wav",
					},
				},
			},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{
				Kind:     "hl1",
				GameDir:  gameDir,
				MapName:  "crossfire",
				WADPaths: []string{wadPath},
			},
		},
	}
	result, err := BuildGameAssetImport(ImportOptions{
		GameDir:                  gameDir,
		OutputRoot:               outDir,
		VoxelResolution:          0.2,
		GameAssetVoxelResolution: 0.07,
		PickupVoxelResolution:    0.03,
	}, summary)
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 5 {
		t.Fatalf("assets = %+v", result.Manifest.Assets)
	}
	assertHL1AssetEntry(t, result.Manifest.Assets, "wad", wadPath, "used_for_texture_bake")
	modelEntry := assertHL1AssetEntry(t, result.Manifest.Assets, "model", modelPath, "generated_voxel_asset")
	if modelEntry.ModelInfo == nil || modelEntry.ModelInfo.TextureCount != 1 || modelEntry.ModelInfo.BodyPartCount != 1 {
		t.Fatalf("expected parsed model info, got %+v", modelEntry.ModelInfo)
	}
	if modelEntry.GeneratedAssetPath == "" || modelEntry.GeneratedVoxelCount == 0 {
		t.Fatalf("expected generated voxel asset metadata, got %+v", modelEntry)
	}
	if modelEntry.GeneratedVoxelResolution != 0.03 {
		t.Fatalf("expected pickup model voxel resolution 0.03, got %+v", modelEntry)
	}
	if modelEntry.GeneratedVoxelResolutionCategory != string(HL1VoxelResolutionCategoryPickup) {
		t.Fatalf("expected pickup model category, got %+v", modelEntry)
	}
	if modelEntry.generatedAsset == nil || len(modelEntry.generatedAsset.Parts) != 1 || modelEntry.generatedAsset.Parts[0].VoxelResolution != 0.03 {
		t.Fatalf("expected generated pickup model asset resolution 0.03, got %+v", modelEntry.generatedAsset)
	}
	staticTag := false
	for _, tag := range modelEntry.generatedAsset.Tags {
		staticTag = staticTag || tag == "generated:mdl_static_world_model"
	}
	if modelEntry.generatedAsset.Skeleton != nil || len(modelEntry.generatedAsset.AnimationClips) != 0 || !staticTag {
		t.Fatalf("expected pickup model to use the static world-model profile, got %+v", modelEntry.generatedAsset)
	}
	npcModelEntry := assertHL1AssetEntry(t, result.Manifest.Assets, "model", npcModelPath, "generated_voxel_asset")
	if npcModelEntry.GeneratedVoxelResolution != DefaultNPCVoxelResolution {
		t.Fatalf("expected npc model to use npc voxel resolution %f, got %+v", DefaultNPCVoxelResolution, npcModelEntry)
	}
	if npcModelEntry.GeneratedVoxelResolutionCategory != string(HL1VoxelResolutionCategoryNPC) {
		t.Fatalf("expected npc model category, got %+v", npcModelEntry)
	}
	spriteEntry := assertHL1AssetEntry(t, result.Manifest.Assets, "sprite", spritePath, "generated_voxel_asset")
	if spriteEntry.SpriteInfo == nil || spriteEntry.SpriteInfo.FrameCount != 1 || spriteEntry.GeneratedAssetPath == "" || spriteEntry.GeneratedVoxelCount == 0 {
		t.Fatalf("expected generated sprite asset metadata, got %+v", spriteEntry)
	}
	if spriteEntry.GeneratedVoxelResolution != 0.07 {
		t.Fatalf("expected generic game asset voxel resolution 0.07, got %+v", spriteEntry)
	}
	if spriteEntry.GeneratedVoxelResolutionCategory != string(HL1VoxelResolutionCategoryStaticProp) {
		t.Fatalf("expected static prop sprite category, got %+v", spriteEntry)
	}
	assertHL1AssetEntry(t, result.Manifest.Assets, "sound", soundPath, "cataloged_source_only")
	if len(result.Manifest.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %+v", result.Manifest.Diagnostics)
	}
	if err := SaveGameAssetImport(result); err != nil {
		t.Fatalf("SaveGameAssetImport failed: %v", err)
	}
	if _, err := os.Stat(result.ManifestPath); err != nil {
		t.Fatalf("expected manifest %s: %v", result.ManifestPath, err)
	}
	for _, entry := range result.Manifest.Assets {
		if !entry.Resolved {
			continue
		}
		if _, err := os.Stat(entry.OutputPath); err != nil {
			t.Fatalf("expected copied asset %s: %v", entry.OutputPath, err)
		}
		if entry.GeneratedAssetPath != "" {
			if _, err := os.Stat(entry.GeneratedAssetPath); err != nil {
				t.Fatalf("expected generated asset %s: %v", entry.GeneratedAssetPath, err)
			}
		}
	}
}

func TestEffectiveHL1VoxelResolutionPolicyUsesNamedDefaultsAndLegacyAliases(t *testing.T) {
	defaults := EffectiveHL1VoxelResolutionPolicy(ImportOptions{})
	if defaults.World != 0.1 || defaults.BrushModel != 0.1 || defaults.Fixture != 0.05 || defaults.StaticProp != 0.05 || defaults.Pickup != 0.01 {
		t.Fatalf("unexpected default policy: %+v", defaults)
	}
	policy := EffectiveHL1VoxelResolutionPolicy(ImportOptions{
		VoxelResolution: 0.2,
		VoxelResolutionPolicy: HL1VoxelResolutionPolicy{
			BrushModel: 0.12,
			Fixture:    0.06,
			StaticProp: 0.04,
			Pickup:     0.015,
		},
		GameAssetVoxelResolution: 0.07,
		PickupVoxelResolution:    0.03,
	})
	if policy.World != 0.2 || policy.BrushModel != 0.12 || policy.Fixture != 0.06 || policy.StaticProp != 0.07 || policy.Pickup != 0.03 {
		t.Fatalf("unexpected effective policy: %+v", policy)
	}
}

func TestHL1GameAssetResolutionCategorySeparatesPickupsFromStaticProps(t *testing.T) {
	if got := hl1VoxelResolutionCategoryForGameAssetEntry(&GameAssetManifestEntry{UsedBy: []string{"pickup:weapon_357.model"}}); got != HL1VoxelResolutionCategoryPickup {
		t.Fatalf("pickup category = %q", got)
	}
	if got := hl1VoxelResolutionCategoryForGameAssetEntry(&GameAssetManifestEntry{UsedBy: []string{"npc:monster_barney.model"}}); got != HL1VoxelResolutionCategoryNPC {
		t.Fatalf("npc category = %q", got)
	}
	if got := hl1VoxelResolutionCategoryForGameAssetEntry(&GameAssetManifestEntry{UsedBy: []string{"env_sprite.model"}}); got != HL1VoxelResolutionCategoryStaticProp {
		t.Fatalf("static prop category = %q", got)
	}
}

func TestBuildGameAssetImportReportsMissingReferences(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	summary := ImportSummary{
		Map: importcommon.MapImport{
			Entities: []importcommon.Entity{{
				ClassName: "monster_scientist",
				KeyValues: map[string]string{
					"model": "models/scientist.mdl",
				},
			}},
		},
		Report: importcommon.ImportReport{
			Source: importcommon.SourceInfo{Kind: "hl1", GameDir: gameDir, MapName: "testmap"},
		},
	}
	result, err := BuildGameAssetImport(ImportOptions{GameDir: gameDir, OutputRoot: filepath.Join(dir, "out")}, summary)
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 1 || result.Manifest.Assets[0].Resolved {
		t.Fatalf("expected one unresolved asset, got %+v", result.Manifest.Assets)
	}
	if len(result.Manifest.Diagnostics) != 1 || result.Manifest.Diagnostics[0].Code != "hl1.asset_missing" {
		t.Fatalf("expected missing asset diagnostic, got %+v", result.Manifest.Diagnostics)
	}
}

func TestBuildGameAssetImportUsesTripmineCompatibilityFallback(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	outDir := filepath.Join(dir, "out")
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "p_tripmine.mdl"), syntheticMDLWithBoneAndSequence())
	summary := ImportSummary{
		Map:    importcommon.MapImport{Entities: []importcommon.Entity{{ClassName: "weapon_tripmine"}}},
		Report: importcommon.ImportReport{Source: importcommon.SourceInfo{Kind: "hl1", GameDir: gameDir, MapName: "testmap"}},
	}
	result, err := BuildGameAssetImport(ImportOptions{GameDir: gameDir, OutputRoot: outDir}, summary)
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 1 {
		t.Fatalf("expected one fallback asset, got %+v", result.Manifest.Assets)
	}
	entry := result.Manifest.Assets[0]
	if entry.SourceRef != "models/p_tripmine.mdl" || !entry.Resolved || !entry.CompatibilityFallback || entry.generatedAsset == nil {
		t.Fatalf("expected resolved tripmine fallback asset, got %+v", entry)
	}
	if !strings.Contains(strings.Join(entry.generatedAsset.Tags, ","), "source:compatibility_fallback") {
		t.Fatalf("expected generated fallback tag, got %+v", entry.generatedAsset.Tags)
	}
	pickups := buildHL1Pickups(summary.Map.Entities, filepath.Join(outDir, "levels", "testmap.gklevel"), &result)
	if len(pickups) != 1 || !strings.Contains(pickups[0].AssetPath, "p_tripmine.gkasset") {
		t.Fatalf("expected pickup to reference fallback asset, got %+v", pickups)
	}
}

func TestBuildGameAssetImportCatalogsPlayerAndWeaponWorldModels(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	outDir := filepath.Join(dir, "out")
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "player", "gordon", "gordon.mdl"), syntheticMDLWithBoneAndSequence())
	mustWriteFile(t, filepath.Join(gameDir, "valve_downloads", "models", "player", "alyx", "alyx.mdl"), syntheticMDLWithBoneAndSequence())
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "w_shotgun.mdl"), syntheticMDLWithBoneAndSequence())
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "w_shotgunt.mdl"), syntheticMDLWithBoneAndSequence())
	result, err := BuildGameAssetImport(ImportOptions{
		GameDir:                    gameDir,
		MapName:                    "catalog",
		OutputRoot:                 outDir,
		ImportAllPlayerModels:      true,
		ImportAllWeaponWorldModels: true,
	}, ImportSummary{})
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 3 {
		t.Fatalf("expected two players and one weapon world model, got %+v", result.Manifest.Assets)
	}
	var players, weapons int
	for _, entry := range result.Manifest.Assets {
		switch entry.CatalogKind {
		case "player":
			players++
			if entry.GeneratedVoxelResolutionCategory != string(HL1VoxelResolutionCategoryNPC) {
				t.Fatalf("expected player resolution category, got %+v", entry)
			}
		case "weapon_world":
			weapons++
			if entry.GeneratedVoxelResolutionCategory != string(HL1VoxelResolutionCategoryPickup) {
				t.Fatalf("expected weapon resolution category, got %+v", entry)
			}
		}
	}
	if players != 2 || weapons != 1 {
		t.Fatalf("catalog kinds = players %d weapons %d, entries=%+v", players, weapons, result.Manifest.Assets)
	}
	if result.Manifest.Catalog == nil || len(result.Manifest.Catalog.Players) != 0 || len(result.Manifest.Catalog.WeaponWorldModels) != 1 {
		t.Fatalf("expected only supported weapon in catalog, got %+v", result.Manifest.Catalog)
	}
	missingAnchors := false
	for _, diagnostic := range result.Manifest.Diagnostics {
		if diagnostic.Code == "hl1.player_required_anchor_missing" {
			missingAnchors = true
			break
		}
	}
	if !missingAnchors {
		t.Fatalf("expected deterministic missing-anchor diagnostic, got %+v", result.Manifest.Diagnostics)
	}
}

func TestHL1PlayerModelVariantsChooseEveryBodygroupAndSkin(t *testing.T) {
	variants := hl1PlayerModelVariants(MDLInfo{
		BodyParts:       []MDLBodyPartInfo{{ModelCount: 2}, {ModelCount: 3}},
		SkinFamilyCount: 2,
	})
	if len(variants) != 12 {
		t.Fatalf("expected 12 explicit player variants, got %+v", variants)
	}
	for _, variant := range variants {
		if len(variant.bodygroupModels) != 2 || variant.bodygroupModels[0] < 0 || variant.bodygroupModels[0] >= 2 || variant.bodygroupModels[1] < 0 || variant.bodygroupModels[1] >= 3 || variant.skinFamily < 0 || variant.skinFamily >= 2 {
			t.Fatalf("invalid explicit variant %+v", variant)
		}
	}
}

func TestBuildMDLVoxelAssetEmitsVerifiedPlayerAnchorsAndCatalogLinks(t *testing.T) {
	geometry := MDLGeometry{
		Info: MDLInfo{Bones: []MDLBoneInfo{{Name: "Bip01 Spine", Parent: -1}, {Name: "Bip01 Spine1", Parent: 0}, {Name: "Bip01 Spine2", Parent: 1}, {Name: "Bip01 Spine3", Parent: 2}, {Name: "Bip01 Neck", Parent: 3}, {Name: "Bip01 Head", Parent: 4}, {Name: "Bip01 R Hand", Parent: 4}}},
		Triangles: []MDLTriangle{{Vertices: [3]MDLTriangleVertex{
			{Position: importcommon.Vec3{X: 0, Y: 0, Z: 0}, BoneIndex: 0},
			{Position: importcommon.Vec3{X: 1, Y: 0, Z: 0}, BoneIndex: 0},
			{Position: importcommon.Vec3{X: 0, Y: 1, Z: 0}, BoneIndex: 0},
		}}},
	}
	anchors := hl1PlayerSemanticAnchorBones(geometry.Info.Bones)
	asset, _, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{VoxelResolution: 0.02, SemanticAnchors: anchors})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	markerParents := map[string]string{}
	for _, marker := range asset.Markers {
		markerParents[marker.ID] = marker.ParentID
	}
	if len(markerParents) != 8 || markerParents["head"] != "bone_05_bip01_head" || markerParents["right_hand"] != "bone_06_bip01_r_hand" || markerParents["upper_body"] != "bone_00_bip01_spine" || markerParents["aim_spine3"] != "bone_03_bip01_spine3" || markerParents["aim_neck"] != "bone_04_bip01_neck" {
		t.Fatalf("expected verified bone markers, got %+v", asset.Markers)
	}
	partParent := map[string]string{}
	for _, part := range asset.Parts {
		partParent[part.ID] = part.ParentID
	}
	if partParent["bone_05_bip01_head"] != "bone_04_bip01_neck" || partParent["bone_06_bip01_r_hand"] != "bone_04_bip01_neck" {
		t.Fatalf("expected head and hand bones parented to neck, got %+v", asset.Parts)
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{}); validation.HasErrors() {
		t.Fatalf("expected anchored player asset to validate, got %+v", validation.Issues)
	}
	catalog := buildGameAssetCatalog([]GameAssetManifestEntry{{
		CatalogKind:        "player",
		CatalogID:          "player_b0_s0",
		SourceRef:          "valve/models/player/gordon/gordon.mdl",
		GeneratedAssetPath: "generated/gordon.gkasset",
		BodygroupModels:    []int{0},
		HeadMarkerID:       "head",
		RightHandMarkerID:  "right_hand",
		UpperBodyMarkerID:  "upper_body",
		AimMarkerIDs:       append([]string(nil), hl1PlayerAimMarkerIDs...),
		ClipIDs:            []string{"mdl_idle"},
	}})
	if catalog == nil || len(catalog.Players) != 1 || catalog.Players[0].HeadMarkerID != "head" || catalog.Players[0].RightHandMarkerID != "right_hand" || catalog.Players[0].UpperBodyMarkerID != "upper_body" || len(catalog.Players[0].AimMarkerIDs) != len(hl1PlayerAimMarkerIDs) || len(catalog.Players[0].ClipIDs) != 1 {
		t.Fatalf("expected player catalog link, got %+v", catalog)
	}
}

func TestHL1PlayerDirectionalLocomotionUsesOnlyVerifiedSequences(t *testing.T) {
	locomotion := hl1PlayerDirectionalLocomotion([]content.AssetAnimationClipDef{
		{ID: "mdl_walk", Name: "walk"},
		{ID: "mdl_run", Name: "run"},
		{ID: "mdl_walk_sideways", Name: "walk_sideways"},
	})
	if locomotion.Walk.Forward != "mdl_walk" || locomotion.Run.Forward != "mdl_run" || locomotion.Walk.Left != "" || locomotion.Fallback != GameAssetPlayerLocomotionFallbackFaceTravel || locomotion.BackwardFallback != GameAssetPlayerBackwardFallbackReverseForward {
		t.Fatalf("expected explicit forward clips and face-travel fallback, got %+v", locomotion)
	}
	unsupported := hl1PlayerDirectionalLocomotion([]content.AssetAnimationClipDef{{ID: "mdl_unknown", Name: "unknown"}})
	if unsupported.Fallback != GameAssetPlayerLocomotionFallbackUnsupported {
		t.Fatalf("expected unsupported unknown sequence set, got %+v", unsupported)
	}
}

func TestMDLAnimationTracksUseBoneLocalSpace(t *testing.T) {
	bones := []MDLBoneInfo{{Name: "root", Parent: -1}, {Name: "child", Parent: 0}}
	bind := []mdlBoneFrameTransform{{Position: importcommon.Vec3{}, Rotation: mgl32.QuatIdent()}, {Position: importcommon.Vec3{X: 10}, Rotation: mgl32.QuatIdent()}}
	frame := []mdlBoneFrameTransform{{Position: importcommon.Vec3{X: 5}, Rotation: mgl32.QuatIdent()}, {Position: importcommon.Vec3{X: 15}, Rotation: mgl32.QuatIdent()}}
	position, _ := mdlLocalAnimationTransform(1, 0, false, bind, frame, bones)
	if !approxContentVec3(position, content.Vec3{0.254, 0, 0}, 1e-5) {
		t.Fatalf("child local position = %+v, want bind-local offset", position)
	}
}

func TestParseMDLInfoReadsGoldSrcHeaderMetadata(t *testing.T) {
	info, err := ParseMDLInfo(syntheticMDL())
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if info.Name != "test_model" || info.Version != MDLVersion10 || info.TextureCount != 1 || info.BodyPartCount != 1 {
		t.Fatalf("unexpected mdl info: %+v", info)
	}
	if len(info.Textures) != 1 || info.Textures[0].Name != "test_texture.bmp" || info.Textures[0].Width != 64 || info.Textures[0].Height != 32 {
		t.Fatalf("unexpected textures: %+v", info.Textures)
	}
	if len(info.BodyParts) != 1 || len(info.BodyParts[0].Models) != 1 {
		t.Fatalf("unexpected body parts: %+v", info.BodyParts)
	}
	model := info.BodyParts[0].Models[0]
	if model.Name != "body_model" || model.MeshCount != 1 || model.VertexCount != 8 || model.TriangleCount != 1 {
		t.Fatalf("unexpected model metadata: %+v", model)
	}
}

func TestParseMDLGeometryDecodesTexturePixelsAndTriangleCommands(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDL())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	if geometry.Info.DecodedTextureCount != 1 || len(geometry.Textures) != 1 {
		t.Fatalf("expected one decoded texture, got info=%+v textures=%d", geometry.Info, len(geometry.Textures))
	}
	if len(geometry.Textures[0].Pixels) != 64*32 || len(geometry.Textures[0].Palette) != 256 {
		t.Fatalf("unexpected texture payload: pixels=%d palette=%d", len(geometry.Textures[0].Pixels), len(geometry.Textures[0].Palette))
	}
	if geometry.Info.DecodedTriangleCount != 1 || len(geometry.Triangles) != 1 {
		t.Fatalf("expected one decoded triangle, got info=%+v triangles=%d", geometry.Info, len(geometry.Triangles))
	}
	tri := geometry.Triangles[0]
	if tri.TextureIndex != 0 {
		t.Fatalf("expected triangle texture 0, got %d", tri.TextureIndex)
	}
	if tri.Vertices[2].Position.Z != 1 || tri.Vertices[1].Texel != [2]int{32, 0} || tri.Vertices[2].UV != [2]float32{0, 1} {
		t.Fatalf("unexpected triangle vertices: %+v", tri.Vertices)
	}
}

func TestParseMDLGeometryAppliesVertexBoneBindPose(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneTranslation(10, 20, 30))
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	if len(geometry.Triangles) != 1 {
		t.Fatalf("triangles = %d", len(geometry.Triangles))
	}
	tri := geometry.Triangles[0]
	if tri.Vertices[0].Position != (importcommon.Vec3{X: 10, Y: 20, Z: 30}) ||
		tri.Vertices[1].Position != (importcommon.Vec3{X: 11, Y: 20, Z: 30}) ||
		tri.Vertices[2].Position != (importcommon.Vec3{X: 10, Y: 20, Z: 31}) {
		t.Fatalf("bone-transformed vertices = %+v", tri.Vertices)
	}
}

func TestParseMDLInfoReadsBonesAndSequences(t *testing.T) {
	info, err := ParseMDLInfo(syntheticMDLWithBoneAndSequence())
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if len(info.Bones) != 1 || info.Bones[0].Name != "root" || info.Bones[0].Parent != -1 {
		t.Fatalf("unexpected bones: %+v", info.Bones)
	}
	if info.Bones[0].Position != (importcommon.Vec3{X: 10, Y: 20, Z: 30}) {
		t.Fatalf("unexpected bone position: %+v", info.Bones[0].Position)
	}
	if len(info.Sequences) != 1 || info.Sequences[0].Name != "idle" || info.Sequences[0].FPS != 30 || info.Sequences[0].FrameCount != 16 {
		t.Fatalf("unexpected sequences: %+v", info.Sequences)
	}
}

func TestParseMDLInfoReadsMultipleGoldSrcSequenceRecords(t *testing.T) {
	info, err := ParseMDLInfo(syntheticMDLWithTwoGoldSrcSequences())
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if len(info.Sequences) != 2 {
		t.Fatalf("expected two sequences, got %+v", info.Sequences)
	}
	if info.Sequences[0].Name != "idle" || info.Sequences[0].FrameCount != 16 {
		t.Fatalf("unexpected first sequence: %+v", info.Sequences[0])
	}
	if info.Sequences[1].Name != "walk" || info.Sequences[1].FrameCount != 24 || info.Sequences[1].FPS != 20 {
		t.Fatalf("unexpected second sequence: %+v", info.Sequences[1])
	}
}

func TestParseMDLInfoSkipsImpossibleSequenceAnimationDecode(t *testing.T) {
	info, err := ParseMDLInfo(syntheticMDLWithImpossibleSequenceAnimation())
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if len(info.Sequences) != 1 {
		t.Fatalf("expected one sequence, got %+v", info.Sequences)
	}
	if len(info.Sequences[0].BoneAnimations) != 0 {
		t.Fatalf("expected malformed sequence to skip decoded animation frames, got %+v", info.Sequences[0].BoneAnimations)
	}
}

func TestParseMDLInfoDecodesSequenceAnimationFrames(t *testing.T) {
	info, err := ParseMDLInfo(syntheticMDLWithBoneSequenceAnimation())
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if len(info.Sequences) != 1 || len(info.Sequences[0].BoneAnimations) != 1 {
		t.Fatalf("expected decoded bone animation, got %+v", info.Sequences)
	}
	animation := info.Sequences[0].BoneAnimations[0]
	if len(animation.PositionFrames) != 2 || len(animation.RotationFrames) != 2 {
		t.Fatalf("expected two decoded frames, got %+v", animation)
	}
	if animation.PositionFrames[0].X != 10 || animation.PositionFrames[1].X != 12 {
		t.Fatalf("unexpected decoded position frames: %+v", animation.PositionFrames)
	}
	if animation.RotationFrames[0].Z != 0 || animation.RotationFrames[1].Z != 1 {
		t.Fatalf("unexpected decoded rotation frames: %+v", animation.RotationFrames)
	}
}

func TestBuildMDLVoxelAssetEmitsSkeletonAndBindPoseClip(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneAndSequence())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	asset, voxelCount, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{Name: "barney", SourceRef: "models/barney.mdl", VoxelResolution: 0.02})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	if voxelCount == 0 {
		t.Fatal("expected generated voxels")
	}
	if asset.Skeleton == nil || len(asset.Skeleton.Bones) != 1 || asset.Skeleton.Bones[0].ID != "bone_00_root" {
		t.Fatalf("expected emitted skeleton, got %+v", asset.Skeleton)
	}
	if len(asset.Parts) != 2 || asset.Parts[0].ID != "bone_00_root" || asset.Parts[1].ID != "bone_00_root_voxels" || asset.Parts[1].ParentID != "bone_00_root" {
		t.Fatalf("expected rigid bone group plus voxel child, got %+v", asset.Parts)
	}
	if !approxContentVec3(asset.Parts[0].Transform.Position, content.Vec3{0.254, 0.762, -0.508}, 1e-5) {
		t.Fatalf("unexpected bone group position: %+v", asset.Parts[0].Transform.Position)
	}
	if len(asset.AnimationClips) != 1 || asset.AnimationClips[0].ID != "mdl_idle" || len(asset.AnimationClips[0].Tracks) != 1 {
		t.Fatalf("expected bind-pose animation clip, got %+v", asset.AnimationClips)
	}
	if asset.AnimationClips[0].Tracks[0].TargetID != "bone_00_root" {
		t.Fatalf("expected clip to target bone group, got %+v", asset.AnimationClips[0].Tracks[0])
	}
	if asset.Runtime == nil || asset.Runtime.CollapseVoxelParts {
		t.Fatalf("expected animated mdl asset to keep voxel parts uncollapsed, got %+v", asset.Runtime)
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{}); validation.HasErrors() {
		t.Fatalf("expected rigid MDL asset to validate, got %+v", validation.Issues)
	}
}

func TestBuildMDLVoxelAssetEmitsDecodedSequenceClip(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneSequenceAnimation())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	asset, _, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{Name: "barney", SourceRef: "models/barney.mdl", VoxelResolution: 0.02})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	if len(asset.AnimationClips) != 1 || len(asset.AnimationClips[0].Tracks) != 1 {
		t.Fatalf("expected one decoded clip track, got %+v", asset.AnimationClips)
	}
	track := asset.AnimationClips[0].Tracks[0]
	if len(track.PositionKeys) != 2 || len(track.RotationKeys) != 2 {
		t.Fatalf("expected per-frame keys, got %+v", track)
	}
	if !approxContentVec3(track.PositionKeys[0].Value, content.Vec3{0.254, 0.762, -0.508}, 1e-5) ||
		!approxContentVec3(track.PositionKeys[1].Value, content.Vec3{0.3048, 0.762, -0.508}, 1e-5) {
		t.Fatalf("unexpected decoded clip positions: %+v", track.PositionKeys)
	}
	if track.RotationKeys[1].Value == (content.Quat{0, 0, 0, 1}) {
		t.Fatalf("expected decoded rotation key to change, got %+v", track.RotationKeys)
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{}); validation.HasErrors() {
		t.Fatalf("expected decoded MDL asset to validate, got %+v", validation.Issues)
	}
}

func TestBuildMDLVoxelAssetLocksPlayerRootMotion(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneSequenceAnimation())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	asset, _, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{VoxelResolution: 0.02, LockRootMotion: true})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	keys := asset.AnimationClips[0].Tracks[0].PositionKeys
	if len(keys) != 2 || !approxContentVec3(keys[0].Value, keys[1].Value, 1e-5) {
		t.Fatalf("expected locked root motion, got %+v", keys)
	}
}

func TestBuildMDLVoxelAssetStaticPoseBakesWorldModelIntoSingleVoxelPart(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneSequenceAnimation())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	asset, voxelCount, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{
		Name:            "w_shotgun",
		SourceRef:       "models/w_shotgun.mdl",
		VoxelResolution: 0.02,
		StaticPose:      true,
	})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	if voxelCount == 0 || asset.Skeleton != nil || len(asset.AnimationClips) != 0 {
		t.Fatalf("expected static asset without skeleton or clips, got voxels=%d skeleton=%+v clips=%+v", voxelCount, asset.Skeleton, asset.AnimationClips)
	}
	if len(asset.Parts) != 1 || asset.Parts[0].Source.VoxelShape == nil {
		t.Fatalf("expected one static voxel part, got %+v", asset.Parts)
	}
	part := asset.Parts[0]
	if part.Transform.Position != (content.Vec3{}) || part.Transform.Rotation != (content.Quat{0, 0, 0, 1}) || part.Transform.Scale != (content.Vec3{1, 1, 1}) {
		t.Fatalf("expected transform baked into voxels, got %+v", part.Transform)
	}
	if len(part.Source.VoxelShape.Voxels) != voxelCount {
		t.Fatalf("expected %d baked voxels, got %d", voxelCount, len(part.Source.VoxelShape.Voxels))
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{}); validation.HasErrors() {
		t.Fatalf("expected static MDL asset to validate, got %+v", validation.Issues)
	}
}

func TestVoxelizeMDLGeometryByBoneSplitsMixedBoneTriangleBySample(t *testing.T) {
	geometry := MDLGeometry{
		Info: MDLInfo{
			Bones: []MDLBoneInfo{
				{Name: "root", Parent: -1},
				{Name: "arm", Parent: 0},
			},
		},
		Triangles: []MDLTriangle{{
			TextureIndex: -1,
			Vertices: [3]MDLTriangleVertex{
				{Position: importcommon.Vec3{X: 0, Y: 0, Z: 0}, BoneIndex: 0},
				{Position: importcommon.Vec3{X: 100, Y: 0, Z: 0}, BoneIndex: 1},
				{Position: importcommon.Vec3{X: 0, Y: 0, Z: 100}, BoneIndex: 1},
			},
		}},
	}
	boneVoxels := voxelizeMDLGeometryByBone(geometry, 0.5)
	if len(boneVoxels[0]) == 0 {
		t.Fatalf("expected mixed triangle samples near root vertex to stay on bone 0, got %+v", boneVoxels)
	}
	if len(boneVoxels[1]) == 0 {
		t.Fatalf("expected mixed triangle samples near arm vertices to stay on bone 1, got %+v", boneVoxels)
	}
}

func TestLoadMDLGeometryUsesCompanionTextureModel(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "w_test.mdl")
	texturePath := filepath.Join(dir, "w_testt.mdl")
	mustWriteFile(t, mainPath, syntheticMDLWithoutEmbeddedTextures())
	mustWriteFile(t, texturePath, syntheticMDL())

	geometry, err := LoadMDLGeometry(mainPath)
	if err != nil {
		t.Fatalf("LoadMDLGeometry failed: %v", err)
	}
	if geometry.Info.DecodedTextureCount != 1 || len(geometry.Textures) != 1 {
		t.Fatalf("expected companion texture payload, got info=%+v textures=%d", geometry.Info, len(geometry.Textures))
	}
	if len(geometry.Triangles) != 1 || geometry.Triangles[0].Vertices[1].UV[0] == 0 {
		t.Fatalf("expected companion texture dimensions to drive UV decode, got %+v", geometry.Triangles)
	}
	asset, voxelCount, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{Name: "w_test", SourceRef: "models/w_test.mdl", VoxelResolution: 0.005})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	if voxelCount == 0 || len(asset.Materials) < 2 {
		t.Fatalf("expected texture-colored model voxels, voxels=%d materials=%+v", voxelCount, asset.Materials)
	}
}

func TestParseSPRGeometryDecodesPaletteAndFrame(t *testing.T) {
	geometry, err := ParseSPRGeometry(syntheticSPR())
	if err != nil {
		t.Fatalf("ParseSPRGeometry failed: %v", err)
	}
	if geometry.Info.Version != SPRVersion2 || geometry.Info.MaxWidth != 4 || geometry.Info.MaxHeight != 2 || geometry.Info.DecodedFrames != 1 {
		t.Fatalf("unexpected spr info: %+v", geometry.Info)
	}
	if len(geometry.Palette) != 256 || len(geometry.Frames) != 1 {
		t.Fatalf("unexpected spr payload: palette=%d frames=%d", len(geometry.Palette), len(geometry.Frames))
	}
	frame := geometry.Frames[0]
	if frame.OriginX != -2 || frame.OriginY != 1 || frame.Width != 4 || frame.Height != 2 || len(frame.Pixels) != 8 {
		t.Fatalf("unexpected frame: %+v", frame)
	}
}

func TestBuildSPRVoxelAssetBuildsVisibleCard(t *testing.T) {
	geometry, err := ParseSPRGeometry(syntheticSPR())
	if err != nil {
		t.Fatalf("ParseSPRGeometry failed: %v", err)
	}
	asset, voxelCount, err := BuildSPRVoxelAsset(geometry, SPRVoxelAssetOptions{Name: "glow", SourceRef: "sprites/glow01.spr", VoxelResolution: 0.1})
	if err != nil {
		t.Fatalf("BuildSPRVoxelAsset failed: %v", err)
	}
	if voxelCount != 5 || len(asset.Parts) != 1 || len(asset.Materials) == 0 {
		t.Fatalf("unexpected sprite asset: voxels=%d asset=%+v", voxelCount, asset)
	}
	if asset.Parts[0].Source.VoxelShape == nil || len(asset.Parts[0].Source.VoxelShape.Voxels) != voxelCount {
		t.Fatalf("missing voxel shape: %+v", asset.Parts[0].Source)
	}
}

func assertHL1AssetEntry(t *testing.T, entries []GameAssetManifestEntry, kind string, sourcePath string, convertState string) GameAssetManifestEntry {
	t.Helper()
	for _, entry := range entries {
		if entry.Kind == kind && filepath.Clean(entry.SourcePath) == filepath.Clean(sourcePath) {
			if !entry.Resolved {
				t.Fatalf("%s entry was not resolved: %+v", kind, entry)
			}
			if entry.ConvertState != convertState {
				t.Fatalf("%s convert state = %q, want %q", kind, entry.ConvertState, convertState)
			}
			if entry.SizeBytes == 0 || entry.SHA256 == "" || entry.OutputPath == "" {
				t.Fatalf("%s entry missing copied-file metadata: %+v", kind, entry)
			}
			return entry
		}
	}
	t.Fatalf("missing %s entry for %s in %+v", kind, sourcePath, entries)
	return GameAssetManifestEntry{}
}

func syntheticMDL() []byte {
	const (
		headerOffset      = 0
		textureOffset     = mdlHeaderSize
		bodyPartOffset    = textureOffset + 80
		modelOffset       = bodyPartOffset + 76
		meshOffset        = modelOffset + 112
		vertexOffset      = meshOffset + 20
		triCommandOffset  = vertexOffset + 8*12
		skinOffset        = triCommandOffset + 2 + 3*8 + 2
		textureDataOffset = skinOffset + 2
		totalSize         = textureDataOffset + 64*32 + 256*3
	)
	data := make([]byte, totalSize)
	copy(data[0:4], MDLIdentGoldSrc)
	writeTestInt32(data, headerOffset+4, MDLVersion10)
	writeTestCString(data[headerOffset+8:headerOffset+72], "test_model")
	writeTestInt32(data, headerOffset+72, totalSize)
	writeTestFloat32(data, headerOffset+76, 1)
	writeTestFloat32(data, headerOffset+80, 2)
	writeTestFloat32(data, headerOffset+84, 3)
	writeTestFloat32(data, headerOffset+88, -4)
	writeTestFloat32(data, headerOffset+92, -5)
	writeTestFloat32(data, headerOffset+96, -6)
	writeTestFloat32(data, headerOffset+100, 4)
	writeTestFloat32(data, headerOffset+104, 5)
	writeTestFloat32(data, headerOffset+108, 6)
	writeTestInt32(data, headerOffset+140, 1)
	writeTestInt32(data, headerOffset+156, 2)
	writeTestInt32(data, headerOffset+164, 3)
	writeTestInt32(data, headerOffset+180, 1)
	writeTestInt32(data, headerOffset+184, textureOffset)
	writeTestInt32(data, headerOffset+192, 1)
	writeTestInt32(data, headerOffset+196, 1)
	writeTestInt32(data, headerOffset+200, skinOffset)
	writeTestInt32(data, headerOffset+204, 1)
	writeTestInt32(data, headerOffset+208, bodyPartOffset)
	writeTestInt32(data, headerOffset+212, 4)

	writeTestCString(data[textureOffset:textureOffset+64], "test_texture.bmp")
	writeTestInt32(data, textureOffset+68, 64)
	writeTestInt32(data, textureOffset+72, 32)
	writeTestInt32(data, textureOffset+76, textureDataOffset)

	writeTestCString(data[bodyPartOffset:bodyPartOffset+64], "body")
	writeTestInt32(data, bodyPartOffset+64, 1)
	writeTestInt32(data, bodyPartOffset+68, 1)
	writeTestInt32(data, bodyPartOffset+72, modelOffset)

	writeTestCString(data[modelOffset:modelOffset+64], "body_model")
	writeTestFloat32(data, modelOffset+68, 8.5)
	writeTestInt32(data, modelOffset+72, 1)
	writeTestInt32(data, modelOffset+76, meshOffset)
	writeTestInt32(data, modelOffset+80, 8)
	writeTestInt32(data, modelOffset+88, vertexOffset)
	writeTestInt32(data, modelOffset+92, 6)
	writeTestInt32(data, modelOffset+104, 0)

	writeTestInt32(data, meshOffset, 1)
	writeTestInt32(data, meshOffset+4, triCommandOffset)
	writeTestInt32(data, meshOffset+8, 0)

	writeTestVec3(data, vertexOffset+0, 0, 0, 0)
	writeTestVec3(data, vertexOffset+12, 1, 0, 0)
	writeTestVec3(data, vertexOffset+24, 0, 0, 1)
	writeTestInt16(data, triCommandOffset, 3)
	writeTestTriangleCommandVertex(data, triCommandOffset+2, 0, 0, 0, 0)
	writeTestTriangleCommandVertex(data, triCommandOffset+10, 1, 0, 32, 0)
	writeTestTriangleCommandVertex(data, triCommandOffset+18, 2, 0, 0, 32)
	writeTestInt16(data, triCommandOffset+26, 0)
	writeTestInt16(data, skinOffset, 0)
	for i := 0; i < 64*32; i++ {
		data[textureDataOffset+i] = byte(i % 256)
	}
	for i := 0; i < 256; i++ {
		base := textureDataOffset + 64*32 + i*3
		data[base] = byte(i)
		data[base+1] = byte(255 - i)
		data[base+2] = byte(i / 2)
	}
	return data
}

func syntheticMDLWithoutEmbeddedTextures() []byte {
	data := syntheticMDL()
	writeTestInt32(data, 180, 0)
	writeTestInt32(data, 184, 0)
	return data
}

func syntheticMDLWithBoneTranslation(x float32, y float32, z float32) []byte {
	data := syntheticMDL()
	const (
		textureOffset  = mdlHeaderSize
		bodyPartOffset = textureOffset + 80
		modelOffset    = bodyPartOffset + 76
		vertexCount    = 8
		boneSize       = 112
	)
	boneOffset := len(data)
	vertInfoOffset := boneOffset + boneSize
	out := append(data, make([]byte, boneSize+vertexCount)...)
	writeTestInt32(out, 72, len(out))
	writeTestInt32(out, 144, boneOffset)
	writeTestInt32(out, modelOffset+84, vertInfoOffset)
	writeTestCString(out[boneOffset:boneOffset+32], "root")
	writeTestInt32(out, boneOffset+32, -1)
	writeTestFloat32(out, boneOffset+64, x)
	writeTestFloat32(out, boneOffset+68, y)
	writeTestFloat32(out, boneOffset+72, z)
	return out
}

func syntheticMDLWithBoneAndSequence() []byte {
	data := syntheticMDLWithBoneTranslation(10, 20, 30)
	const sequenceSize = mdlSequenceRecordSize176
	sequenceOffset := len(data)
	out := append(data, make([]byte, sequenceSize)...)
	writeTestInt32(out, 72, len(out))
	writeTestInt32(out, 164, 1)
	writeTestInt32(out, 168, sequenceOffset)
	writeTestCString(out[sequenceOffset:sequenceOffset+32], "idle")
	writeTestFloat32(out, sequenceOffset+32, 30)
	writeTestInt32(out, sequenceOffset+56, 16)
	return out
}

func syntheticMDLWithTwoGoldSrcSequences() []byte {
	data := syntheticMDLWithBoneTranslation(10, 20, 30)
	const sequenceSize = mdlSequenceRecordSize176
	sequenceOffset := len(data)
	out := append(data, make([]byte, sequenceSize*2)...)
	writeTestInt32(out, 72, len(out))
	writeTestInt32(out, 164, 2)
	writeTestInt32(out, 168, sequenceOffset)
	writeTestCString(out[sequenceOffset:sequenceOffset+32], "idle")
	writeTestFloat32(out, sequenceOffset+32, 30)
	writeTestInt32(out, sequenceOffset+56, 16)
	secondOffset := sequenceOffset + sequenceSize
	writeTestCString(out[secondOffset:secondOffset+32], "walk")
	writeTestFloat32(out, secondOffset+32, 20)
	writeTestInt32(out, secondOffset+56, 24)
	return out
}

func syntheticMDLWithImpossibleSequenceAnimation() []byte {
	data := syntheticMDLWithBoneTranslation(10, 20, 30)
	const sequenceSize = mdlSequenceRecordSize176
	sequenceOffset := len(data)
	animOffset := sequenceOffset + sequenceSize
	out := append(data, make([]byte, sequenceSize+12)...)
	writeTestInt32(out, 72, len(out))
	writeTestInt32(out, 164, 1)
	writeTestInt32(out, 168, sequenceOffset)
	writeTestCString(out[sequenceOffset:sequenceOffset+32], "idle")
	writeTestFloat32(out, sequenceOffset+32, 10)
	writeTestInt32(out, sequenceOffset+56, maxMDLSequenceFrameCount+1)
	writeTestInt32(out, sequenceOffset+120, 1)
	writeTestInt32(out, sequenceOffset+124, animOffset)
	return out
}

func syntheticMDLWithBoneSequenceAnimation() []byte {
	data := syntheticMDLWithBoneTranslation(10, 20, 30)
	const sequenceSize = mdlSequenceRecordSize176
	sequenceOffset := len(data)
	animOffset := sequenceOffset + sequenceSize
	positionStreamOffset := animOffset + 12
	rotationStreamOffset := positionStreamOffset + 6
	out := append(data, make([]byte, sequenceSize+12+6+6)...)
	writeTestInt32(out, 72, len(out))
	writeTestInt32(out, 164, 1)
	writeTestInt32(out, 168, sequenceOffset)
	writeTestCString(out[sequenceOffset:sequenceOffset+32], "idle")
	writeTestFloat32(out, sequenceOffset+32, 10)
	writeTestInt32(out, sequenceOffset+56, 2)
	writeTestInt32(out, sequenceOffset+120, 1)
	writeTestInt32(out, sequenceOffset+124, animOffset)

	boneOffset := len(syntheticMDL())
	writeTestFloat32(out, boneOffset+88, 0.5)
	writeTestFloat32(out, boneOffset+108, 0.1)

	writeTestInt16(out, animOffset+0, positionStreamOffset-animOffset)
	writeTestInt16(out, animOffset+10, rotationStreamOffset-animOffset)
	out[positionStreamOffset+0] = 2
	out[positionStreamOffset+1] = 2
	writeTestInt16(out, positionStreamOffset+2, 0)
	writeTestInt16(out, positionStreamOffset+4, 4)
	out[rotationStreamOffset+0] = 2
	out[rotationStreamOffset+1] = 2
	writeTestInt16(out, rotationStreamOffset+2, 0)
	writeTestInt16(out, rotationStreamOffset+4, 10)
	return out
}

func syntheticSPR() []byte {
	const (
		width         = 4
		height        = 2
		paletteCount  = 256
		headerSize    = sprHeaderSize
		paletteOffset = headerSize + 2
		frameOffset   = paletteOffset + paletteCount*3
		totalSize     = frameOffset + 20 + width*height
	)
	data := make([]byte, totalSize)
	copy(data[0:4], SPRIdentGoldSrc)
	writeTestInt32(data, 4, SPRVersion2)
	writeTestInt32(data, 8, 2)
	writeTestInt32(data, 12, 1)
	writeTestFloat32(data, 16, 4)
	writeTestInt32(data, 20, width)
	writeTestInt32(data, 24, height)
	writeTestInt32(data, 28, 1)
	writeTestFloat32(data, 32, 0)
	writeTestInt32(data, 36, 1)
	binary.LittleEndian.PutUint16(data[headerSize:], paletteCount)
	data[paletteOffset+1*3+0] = 255
	data[paletteOffset+1*3+1] = 64
	data[paletteOffset+1*3+2] = 16
	data[paletteOffset+2*3+0] = 32
	data[paletteOffset+2*3+1] = 128
	data[paletteOffset+2*3+2] = 255
	writeTestInt32(data, frameOffset+0, 0)
	writeTestInt32(data, frameOffset+4, -2)
	writeTestInt32(data, frameOffset+8, 1)
	writeTestInt32(data, frameOffset+12, width)
	writeTestInt32(data, frameOffset+16, height)
	copy(data[frameOffset+20:], []byte{0, 1, 1, 0, 2, 2, 1, 0})
	return data
}

func writeTestInt32(data []byte, offset int, value int) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], uint32(int32(value)))
}

func writeTestInt16(data []byte, offset int, value int) {
	binary.LittleEndian.PutUint16(data[offset:offset+2], uint16(int16(value)))
}

func writeTestFloat32(data []byte, offset int, value float32) {
	binary.LittleEndian.PutUint32(data[offset:offset+4], math.Float32bits(value))
}

func writeTestVec3(data []byte, offset int, x float32, y float32, z float32) {
	writeTestFloat32(data, offset, x)
	writeTestFloat32(data, offset+4, y)
	writeTestFloat32(data, offset+8, z)
}

func writeTestTriangleCommandVertex(data []byte, offset int, vertex int, normal int, s int, t int) {
	writeTestInt16(data, offset, vertex)
	writeTestInt16(data, offset+2, normal)
	writeTestInt16(data, offset+4, s)
	writeTestInt16(data, offset+6, t)
}

func approxContentVec3(got content.Vec3, want content.Vec3, epsilon float32) bool {
	return math.Abs(float64(got[0]-want[0])) <= float64(epsilon) &&
		math.Abs(float64(got[1]-want[1])) <= float64(epsilon) &&
		math.Abs(float64(got[2]-want[2])) <= float64(epsilon)
}

func writeTestCString(data []byte, value string) {
	copy(data, []byte(value))
}
