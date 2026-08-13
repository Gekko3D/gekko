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
	if modelEntry.generatedAsset.Skeleton != nil || len(modelEntry.generatedAsset.AnimationSetPaths) != 0 || !staticTag {
		t.Fatalf("expected pickup model to use the static world-model profile, got %+v", modelEntry.generatedAsset)
	}
	npcModelEntry := assertHL1AssetEntry(t, result.Manifest.Assets, "model", npcModelPath, "generated_voxel_asset")
	if npcModelEntry.GeneratedVoxelResolution != DefaultNPCVoxelResolution {
		t.Fatalf("expected npc model to use npc voxel resolution %f, got %+v", DefaultNPCVoxelResolution, npcModelEntry)
	}
	if npcModelEntry.GeneratedVoxelResolutionCategory != string(HL1VoxelResolutionCategoryNPC) {
		t.Fatalf("expected npc model category, got %+v", npcModelEntry)
	}
	if npcModelEntry.GeneratedVoxelizationProfile == nil || npcModelEntry.GeneratedVoxelizationProfile.ID != "hl1_npc_rigid_v3" || !npcModelEntry.GeneratedVoxelizationProfile.RespectMaskedTextures || !npcModelEntry.GeneratedVoxelizationProfile.FillClosedInterior || !npcModelEntry.GeneratedVoxelizationProfile.PartitionBySkeletonSegments || npcModelEntry.GeneratedVoxelizationProfile.JointCapVoxels != 1 {
		t.Fatalf("expected durable npc voxelization profile provenance, got %+v", npcModelEntry.GeneratedVoxelizationProfile)
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
	manifestData, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifestData), `"output_path"`) {
		t.Fatal("manifest retained obsolete copied-source output paths")
	}
	if _, err := os.Stat(filepath.Join(dir, "out", "hl1_assets", "testmap", "files")); !os.IsNotExist(err) {
		t.Fatalf("import created an obsolete copied-source directory: %v", err)
	}
	for _, entry := range result.Manifest.Assets {
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

func TestBuildGameAssetImportCatalogsStaticProps(t *testing.T) {
	dir := t.TempDir()
	gameDir := filepath.Join(dir, "hl")
	outDir := filepath.Join(dir, "out")
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "office_table.mdl"), syntheticMDL())
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "barney.mdl"), syntheticMDL())
	mustWriteFile(t, filepath.Join(gameDir, "valve", "models", "w_shotgun.mdl"), syntheticMDL())
	mustWriteFile(t, filepath.Join(gameDir, "gearbox", "models", "unrelated_prop.mdl"), syntheticMDL())

	result, err := BuildGameAssetImport(ImportOptions{
		GameDir:              gameDir,
		MapName:              "catalog",
		OutputRoot:           outDir,
		ImportAllStaticProps: true,
	}, ImportSummary{})
	if err != nil {
		t.Fatalf("BuildGameAssetImport failed: %v", err)
	}
	if len(result.Manifest.Assets) != 1 {
		t.Fatalf("expected only the static prop, got %+v", result.Manifest.Assets)
	}
	entry := result.Manifest.Assets[0]
	if entry.CatalogKind != "static_prop" || entry.GeneratedVoxelizationProfile == nil || entry.GeneratedVoxelizationProfile.ID != "hl1_static_prop_solid_v1" {
		t.Fatalf("unexpected static prop entry: %+v", entry)
	}
	if entry.generatedAsset == nil || entry.generatedAsset.Skeleton != nil || len(entry.generatedAsset.AnimationSetPaths) != 0 {
		t.Fatalf("expected a static-pose asset, got %+v", entry.generatedAsset)
	}
	if len(result.Library.Entries) != 1 {
		t.Fatalf("expected one library entry, got %+v", result.Library.Entries)
	}
	libraryEntry := result.Library.Entries[0]
	if !strings.HasPrefix(libraryEntry.Key, "props.imported.") || !hasTag(libraryEntry.Tags, "prop") || !hasTag(libraryEntry.Tags, "group:furniture") || !hasTag(libraryEntry.Tags, "classification:inferred") {
		t.Fatalf("unexpected library entry: %+v", libraryEntry)
	}
}

func TestFillMDLSurfaceClosedInteriorPreservesMaterialMetadata(t *testing.T) {
	voxels := make(map[[3]int]mdlVoxelSample)
	want := mdlVoxelSample{Color: [4]uint8{120, 80, 40, 255}, TextureName: "wood_crate", TextureFlags: mdlTextureFlagMasked}
	for x := 0; x < 3; x++ {
		for y := 0; y < 3; y++ {
			for z := 0; z < 3; z++ {
				if x == 0 || x == 2 || y == 0 || y == 2 || z == 0 || z == 2 {
					voxels[[3]int{x, y, z}] = want
				}
			}
		}
	}

	fillMDLSurfaceClosedInterior(voxels)
	if len(voxels) != 27 || voxels[[3]int{1, 1, 1}] != want {
		t.Fatalf("closed fill did not preserve the nearest source material: center=%+v count=%d", voxels[[3]int{1, 1, 1}], len(voxels))
	}
	materials, _ := mdlAssetMaterialsAndPalette(voxels)
	if len(materials) != 1 || !hasTag(materials[0].Tags, "source_texture:wood_crate") || !hasTag(materials[0].Tags, "source_texture_flags:64") || !hasTag(materials[0].Tags, "alpha:masked") || !hasTag(materials[0].Tags, "kind:wood") {
		t.Fatalf("source material metadata was not retained: %+v", materials)
	}
}

func TestAddGeneratedLevelAssetsToLibraryGroupsBrushProps(t *testing.T) {
	dir := t.TempDir()
	result := GameAssetImportResult{
		LibraryPath: filepath.Join(dir, "hl1_assets", "crossfire", "assets.gkassetlibrary"),
		Library:     content.NewAssetLibraryDef("HL1 imported assets"),
	}
	generated := GeneratedLevelResult{
		Level: content.NewLevelDef("crossfire"),
		StaticBrushAssets: []GeneratedAssetResult{{
			AssetPath: filepath.Join(dir, "assets", "hl1", "static_brushes", "hl1_static_func_wall_0.gkasset"),
			Asset:     content.NewAssetDef("hl1_static_func_wall_0"),
		}},
	}
	if err := AddGeneratedLevelAssetsToLibrary(&result, generated); err != nil {
		t.Fatalf("AddGeneratedLevelAssetsToLibrary failed: %v", err)
	}
	if len(result.Library.Entries) != 1 {
		t.Fatalf("library entries = %+v", result.Library.Entries)
	}
	entry := result.Library.Entries[0]
	if entry.Key != "maps.crossfire.brushes.hl1_static_func_wall_0" || !hasTag(entry.Tags, "group:brushes") || !hasTag(entry.Tags, "source_kind:bsp_brush") || !hasTag(entry.Tags, "scope:crossfire") {
		t.Fatalf("unexpected generated asset entry: %+v", entry)
	}
	got, err := content.ResolveAssetLibraryPath(result.Library, result.LibraryPath, entry.Key)
	if err != nil {
		t.Fatalf("ResolveAssetLibraryPath failed: %v", err)
	}
	if filepath.Clean(got) != filepath.Clean(generated.StaticBrushAssets[0].AssetPath) {
		t.Fatalf("resolved asset path = %q", got)
	}
}

func TestMergeHL1AssetLibrariesIsIdempotentAndRejectsPathConflicts(t *testing.T) {
	existing := content.NewAssetLibraryDef("game assets")
	existing.Entries = []content.AssetLibraryEntryDef{{Key: "characters.robo", AssetPath: "content/robo.gkasset"}}
	incoming := content.NewAssetLibraryDef("import")
	incoming.Entries = []content.AssetLibraryEntryDef{{Key: "characters.robo", AssetPath: "content/robo.gkasset"}, {Key: "maps.crossfire.moving.door", AssetPath: "content/maps/crossfire/door.gkasset"}}

	merged, err := mergeHL1AssetLibraries(existing, incoming)
	if err != nil || len(merged.Entries) != 2 || merged.Entries[0].Key != "characters.robo" || merged.Entries[1].Key != "maps.crossfire.moving.door" {
		t.Fatalf("merged library = %+v, err=%v", merged, err)
	}
	secondMap := content.NewAssetLibraryDef("gasworks")
	secondMap.Entries = []content.AssetLibraryEntryDef{{Key: "maps.gasworks.moving.door", AssetPath: "content/maps/gasworks/door.gkasset"}}
	merged, err = mergeHL1AssetLibraries(merged, secondMap)
	if err != nil || len(merged.Entries) != 3 {
		t.Fatalf("multi-map merge = %+v, err=%v", merged, err)
	}
	merged, err = mergeHL1AssetLibraries(merged, incoming)
	if err != nil || len(merged.Entries) != 3 {
		t.Fatalf("repeated import changed catalog = %+v, err=%v", merged, err)
	}
	conflict := content.NewAssetLibraryDef("conflict")
	conflict.Entries = []content.AssetLibraryEntryDef{{Key: "maps.crossfire.moving.door", AssetPath: "other/door.gkasset"}}
	if _, err := mergeHL1AssetLibraries(merged, conflict); err == nil {
		t.Fatal("conflicting key was accepted")
	}
}

func TestPruneHL1CatalogEntriesOnlyReplacesSelectedKinds(t *testing.T) {
	library := content.NewAssetLibraryDef("game")
	library.Entries = []content.AssetLibraryEntryDef{
		{Key: "characters.old", Tags: []string{"player"}},
		{Key: "props.old", Tags: []string{"static_prop"}},
		{Key: "maps.crossfire.door", Tags: []string{"scope:crossfire"}},
	}
	pruned := pruneHL1CatalogEntries(library, ImportOptions{ImportAllPlayerModels: true})
	if len(pruned.Entries) != 2 || pruned.Entries[0].Key != "props.old" || pruned.Entries[1].Key != "maps.crossfire.door" {
		t.Fatalf("pruned entries = %+v", pruned.Entries)
	}
	if len(library.Entries) != 3 {
		t.Fatal("pruning mutated the loaded catalog")
	}
}

func TestCentralHL1GeneratedAssetPathsStayOutsideLevelOutput(t *testing.T) {
	opts := ImportOptions{MapName: "crossfire", OutputRoot: filepath.Join("levels", "crossfire"), AssetOutputRoot: filepath.Join("assets", "content")}
	got := generatedHL1LevelAssetPath(opts, "moving_brushes", "door")
	want := filepath.Join("assets", "content", "hl1", "maps", "crossfire", "moving_brushes", "door.gkasset")
	if got != want {
		t.Fatalf("generated asset path = %q, want %q", got, want)
	}
}

func TestReuseExistingHL1PlayerAssetForMapReference(t *testing.T) {
	dir := t.TempDir()
	libraryPath := filepath.Join(dir, "game.gkassetlibrary")
	assetPath := filepath.Join(dir, "content", "robo.gkasset")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveAsset(assetPath, content.NewAssetDef("robo")); err != nil {
		t.Fatal(err)
	}
	library := content.NewAssetLibraryDef("game")
	library.Entries = []content.AssetLibraryEntryDef{{
		Key: "characters.robo.b0.s0", AssetPath: "content/robo.gkasset",
		Tags: []string{"player", "source_ref:models/player/robo/robo.mdl", "import_config:/0///0"},
	}}
	entries := []GameAssetManifestEntry{{SourceRef: "MODELS/PLAYER/ROBO/ROBO.MDL", GeneratedAssetPath: filepath.Join(dir, "unused.gkasset"), generatedAsset: content.NewAssetDef("unused")}}
	reuseExistingHL1Assets(entries, library, libraryPath)
	want := filepath.Join(dir, "content", "robo.gkasset")
	if entries[0].GeneratedAssetPath != want || entries[0].generatedAsset != nil || entries[0].ConvertState != "reused_catalog_asset" {
		t.Fatalf("reused entry = %+v", entries[0])
	}
}

func TestReuseExistingHL1AssetRequiresMatchingSourceAndImportConfig(t *testing.T) {
	dir := t.TempDir()
	libraryPath := filepath.Join(dir, "game.gkassetlibrary")
	assetPath := filepath.Join(dir, "content", "shared.gkasset")
	if err := os.MkdirAll(filepath.Dir(assetPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := content.SaveAsset(assetPath, content.NewAssetDef("shared")); err != nil {
		t.Fatal(err)
	}
	config := GameAssetManifestEntry{Kind: "model", SHA256: "same", GeneratedVoxelResolutionCategory: "pickup", GeneratedVoxelResolution: 0.01, GeneratedVoxelizationProfile: &MDLVoxelizationProfile{ID: "surface"}}
	library := content.NewAssetLibraryDef("game")
	library.Entries = []content.AssetLibraryEntryDef{{
		Key: "weapons.shared", AssetPath: "content/shared.gkasset",
		Tags: []string{"weapon_world", "source_ref:overlay/models/w_test.mdl", "source_sha256:same", "import_config:" + hl1AssetImportConfig(config)},
	}}
	matching := config
	matching.SourceRef = "models/w_test.mdl"
	matching.GeneratedAssetPath = filepath.Join(dir, "unused.gkasset")
	matching.generatedAsset = content.NewAssetDef("unused")
	different := matching
	different.GeneratedVoxelResolution = 0.02
	different.GeneratedAssetPath = filepath.Join(dir, "different.gkasset")
	different.generatedAsset = content.NewAssetDef("different")
	entries := []GameAssetManifestEntry{matching, different}
	reuseExistingHL1Assets(entries, library, libraryPath)
	if entries[0].ConvertState != "reused_catalog_asset" || entries[0].GeneratedAssetPath != filepath.Join(dir, "content", "shared.gkasset") {
		t.Fatalf("matching entry = %+v", entries[0])
	}
	if entries[1].ConvertState == "reused_catalog_asset" || entries[1].GeneratedAssetPath != different.GeneratedAssetPath {
		t.Fatalf("different-config entry = %+v", entries[1])
	}
}

func TestUncatalogedGeneratedAssetsReceiveCentralLibraryKeys(t *testing.T) {
	entry := GameAssetManifestEntry{Kind: "model", SourceRef: "models/barney.mdl", GeneratedAssetPath: "barney.gkasset"}
	if key := hl1GenericAssetKey(entry); key != "models.imported.models_barney" {
		t.Fatalf("key = %q", key)
	}
	for _, test := range []struct {
		kind, source, want string
	}{
		{"weapon_world", "valve/models/w_hgun.mdl", "weapons.hivegun"},
		{"weapon_held", "valve/models/p_hgun.mdl", "weapons.hivegun.held"},
		{"weapon_world", "valve/models/w_satchel.mdl", "weapons.satchel"},
		{"weapon_held", "valve/models/p_satchel.mdl", "weapons.satchel.held"},
		{"weapon_held", "valve/models/p_satchel_radio.mdl", "weapons.satchel.radio.held"},
	} {
		entry := GameAssetManifestEntry{Kind: "model", CatalogKind: test.kind, SourceRef: test.source, GeneratedAssetPath: "hgun.gkasset"}
		if key := hl1GenericAssetKey(entry); key != test.want {
			t.Errorf("%s key = %q, want %q", test.kind, key, test.want)
		}
	}
}

func TestAssetLibraryKeepsCollidingPlayerSources(t *testing.T) {
	dir := t.TempDir()
	entries := []GameAssetManifestEntry{
		{Kind: "model", CatalogKind: "player", SourceRef: "gearbox/models/player/zombie/zombie.mdl", SourcePath: filepath.Join(dir, "gearbox", "models", "player", "zombie", "zombie.mdl"), CatalogID: "zombie_b0_s0", BodygroupModels: []int{0}, GeneratedAssetPath: filepath.Join(dir, "gearbox-zombie.gkasset")},
		{Kind: "model", CatalogKind: "player", SourceRef: "valve/models/player/zombie/zombie.mdl", SourcePath: filepath.Join(dir, "valve", "models", "player", "zombie", "zombie.mdl"), CatalogID: "zombie_b0_s0", BodygroupModels: []int{0}, GeneratedAssetPath: filepath.Join(dir, "valve-zombie.gkasset")},
	}
	library := buildHL1AssetLibrary(entries, filepath.Join(dir, "game.gkassetlibrary"))
	if len(library.Entries) != 2 {
		t.Fatalf("entries = %+v", library.Entries)
	}
	if library.Entries[0].Key != "characters.zombie.b0.s0" || library.Entries[0].AssetPath != "valve-zombie.gkasset" {
		t.Fatalf("canonical entry = %+v", library.Entries[0])
	}
	if !strings.Contains(library.Entries[1].Key, ".source.gearbox_models_player_zombie_zombie") {
		t.Fatalf("disambiguated entry = %+v", library.Entries[1])
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

func TestHL1PlayerCrouchGaitUsesActivitiesAndAuthoredBoneMask(t *testing.T) {
	asset := &content.AssetDef{
		Skeleton: &content.AssetSkeletonDef{Bones: []content.AssetBoneDef{
			{ID: "pelvis", JointID: "pelvis", Name: "Bip01 Pelvis"}, {ID: "left_leg", JointID: "left_leg", Name: "Bip01 L Leg"}, {ID: "left_calf", JointID: "left_calf", Name: "Bip01 L Leg1"}, {ID: "left_foot", JointID: "left_foot", Name: "Bip01 L Foot"},
			{ID: "right_leg", JointID: "right_leg", Name: "Bip01 R Leg"}, {ID: "right_calf", JointID: "right_calf", Name: "Bip01 R Leg1"}, {ID: "right_foot", JointID: "right_foot", Name: "Bip01 R Foot"}, {ID: "spine", JointID: "spine", Name: "Bip01 Spine"},
		}},
	}
	clips := []content.AssetAnimationClipDef{{ID: "crouch", Name: "crawl", Tags: []string{"source:hl1_activity:17"}}, {ID: "crouch_idle", Name: "crouch_idle", Tags: []string{"source:hl1_activity:18"}}, {ID: "crouch_aim", Name: "crouch_aim_onehanded"}}
	gait := hl1PlayerCrouchGait(asset, clips)
	if gait.Status != GameAssetPlayerCrouchGaitSupported || gait.CrouchClipID != "crouch" || gait.CrouchIdleClipID != "crouch_idle" || gait.Locomotion != (GameAssetPlayerCrouchLocomotion{DefaultClipID: "crouch", Fallback: GameAssetPlayerLocomotionFallbackFaceTravel, BackwardFallback: GameAssetPlayerBackwardFallbackReverseForward}) || len(gait.BaseStances) != 1 || gait.BaseStances[0] != (GameAssetPlayerStanceClip{Stance: "onehanded", ClipID: "crouch_aim"}) || len(gait.BoneMask) != 7 {
		t.Fatalf("expected verified crouch gait capability, got %+v", gait)
	}
	if unsupported := hl1PlayerCrouchGait(&content.AssetDef{}, nil); unsupported.Status != GameAssetPlayerCrouchGaitUnsupported || unsupported.Diagnostic == "" {
		t.Fatalf("expected explicit unsupported diagnostic, got %+v", unsupported)
	}
}

func TestHL1PlayerWeaponPresentationUsesAllowlistedStanceAndUpperMask(t *testing.T) {
	asset := &content.AssetDef{
		Skeleton: &content.AssetSkeletonDef{Bones: []content.AssetBoneDef{
			{ID: "spine", JointID: "spine", Name: "Bip01 Spine"}, {ID: "neck", JointID: "neck", Name: "Bip01 Neck"}, {ID: "head", JointID: "head", Name: "Bip01 Head"},
			{ID: "left_arm", JointID: "left_arm", Name: "Bip01 L UpperArm"}, {ID: "left_hand", JointID: "left_hand", Name: "Bip01 L Hand"}, {ID: "right_arm", JointID: "right_arm", Name: "Bip01 R UpperArm"}, {ID: "right_hand", JointID: "right_hand", Name: "Bip01 R Hand"},
		}},
	}
	clips := []content.AssetAnimationClipDef{{ID: "aim", Name: "ref_aim_onehanded"}, {ID: "shoot", Name: "ref_shoot_onehanded"}, {ID: "crouch_aim", Name: "crouch_aim_onehanded"}, {ID: "crouch_shoot", Name: "crouch_shoot_onehanded"}, {ID: "ignored", Name: "shoot_onehanded"}}
	presentation := hl1PlayerWeaponPresentation(asset, clips)
	if presentation.Status != GameAssetPlayerWeaponPresentationSupported || len(presentation.Stances) != 1 || presentation.Stances[0].Stance != "onehanded" || presentation.Stances[0].AimClipID != "aim" || presentation.Stances[0].RecoilClipID != "shoot" || presentation.Stances[0].CrouchAimClipID != "crouch_aim" || presentation.Stances[0].CrouchRecoilClipID != "crouch_shoot" || len(presentation.UpperBodyMask) != 7 {
		t.Fatalf("expected verified stance presentation, got %+v", presentation)
	}
	if unsupported := hl1PlayerWeaponPresentation(&content.AssetDef{}, nil); unsupported.Status != GameAssetPlayerWeaponPresentationUnsupported || unsupported.Diagnostic == "" {
		t.Fatalf("expected unsupported diagnostic, got %+v", unsupported)
	}
}

func TestParseMDLSequenceActivity(t *testing.T) {
	data := syntheticMDLWithBoneAndSequence()
	sequenceOffset := int(readInt32(data, 168))
	writeTestInt32(data, sequenceOffset+40, HL1ActivityCrouch)
	info, err := ParseMDLInfo(data)
	if err != nil {
		t.Fatalf("ParseMDLInfo failed: %v", err)
	}
	if len(info.Sequences) != 1 || info.Sequences[0].Activity != HL1ActivityCrouch {
		t.Fatalf("expected preserved crouch activity, got %+v", info.Sequences)
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

func TestGoldSrcSequenceBuildsOneDimensionalBlendClip(t *testing.T) {
	sequence := MDLSequenceInfo{Name: "shootgun", FPS: 25, FrameCount: 1, NumBlends: 2, BlendType: [2]int{8, 0}, BlendStart: [2]float32{-50, 0}, BlendEnd: [2]float32{50, 0}, AnimIndex: 1}
	bones := []MDLBoneInfo{{Name: "root"}}
	setMDLSequenceAnimations(&sequence, decodeMDLSequenceAnimationBlends(make([]byte, 25), sequence, bones))
	clip, ok := mdlDecodedAnimationClip(sequence, bones, []mdlAnimationBindTarget{{ID: "root", BoneIndex: 0, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}, true)
	if !ok || clip.Blend1D == nil || clip.Blend1D.Parameter != "pitch" || clip.Blend1D.Default != 0 || len(clip.Blend1D.Samples) != 2 || clip.Blend1D.Samples[0].Value != -50 || clip.Blend1D.Samples[1].Value != 50 {
		t.Fatalf("unexpected blended clip: %+v", clip)
	}
}

func TestParseMDLAnimationClipsUsesSemanticJointTracks(t *testing.T) {
	clips, err := ParseMDLAnimationClips(syntheticMDLWithBoneSequenceAnimation(), []string{"idle"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(clips) != 1 || clips[0].ID != "mdl_idle" || len(clips[0].Tracks) != 1 {
		t.Fatalf("unexpected clips: %+v", clips)
	}
	track := clips[0].Tracks[0]
	if track.TargetID != "root" || len(track.RotationKeys) != 2 || track.RotationKeys[0].Value == track.RotationKeys[1].Value {
		t.Fatalf("unexpected semantic animation track: %+v", track)
	}
	if _, err := ParseMDLAnimationClips(syntheticMDLWithBoneSequenceAnimation(), []string{"missing"}, true); err == nil {
		t.Fatal("missing requested sequence was accepted")
	}
}

func TestMDLSemanticJointIDRecognizesCrossbowBiped(t *testing.T) {
	for source, want := range map[string]string{
		"Xbow biped Spine2": "bip01.spine2",
		"Xbow biped L Arm1": "bip01.left.upper_arm",
		"Xbow biped R Hand": "bip01.r.hand",
	} {
		if got := mdlSemanticJointID(source); got != want {
			t.Fatalf("mdlSemanticJointID(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestLoadMDLAnimationClipsReadsExternalSequenceGroup(t *testing.T) {
	mainData, groupData := syntheticMDLWithExternalBoneSequenceAnimation()
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "hgrunt.mdl")
	mustWriteFile(t, mainPath, mainData)
	mustWriteFile(t, filepath.Join(dir, "hgrunt01.mdl"), groupData)

	clips, err := LoadMDLAnimationClips(mainPath, []string{"reload_shotgun"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(clips) != 1 || clips[0].Name != "reload_shotgun" || len(clips[0].Tracks) != 1 || clips[0].Tracks[0].RotationKeys[0].Value == clips[0].Tracks[0].RotationKeys[1].Value {
		t.Fatalf("unexpected external sequence clip: %+v", clips)
	}
}

func TestBuildMDLVoxelAssetEmitsSkeletonAndBindPoseClip(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneAndSequence())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	built, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{Name: "barney", SourceRef: "models/barney.mdl", VoxelResolution: 0.02})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	asset, voxelCount := built.Asset, built.VoxelCount
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
	if len(built.Clips) != 1 || built.Clips[0].ID != "mdl_idle" || len(built.Clips[0].Tracks) != 1 {
		t.Fatalf("expected external bind-pose animation clip, got %+v", built.Clips)
	}
	if built.Clips[0].Tracks[0].TargetID != "bone_00_root" {
		t.Fatalf("expected clip to target bone group, got %+v", built.Clips[0].Tracks[0])
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
	built, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{Name: "barney", SourceRef: "models/barney.mdl", VoxelResolution: 0.02})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	asset := built.Asset
	if len(built.Clips) != 1 || len(built.Clips[0].Tracks) != 1 {
		t.Fatalf("expected one decoded clip track, got %+v", built.Clips)
	}
	track := built.Clips[0].Tracks[0]
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
	built, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{VoxelResolution: 0.02, LockRootMotion: true})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	keys := built.Clips[0].Tracks[0].PositionKeys
	if len(keys) != 2 || !approxContentVec3(keys[0].Value, keys[1].Value, 1e-5) {
		t.Fatalf("expected locked root motion, got %+v", keys)
	}
}

func TestBuildMDLVoxelAssetStaticPoseBakesWorldModelIntoSingleVoxelPart(t *testing.T) {
	geometry, err := ParseMDLGeometry(syntheticMDLWithBoneSequenceAnimation())
	if err != nil {
		t.Fatalf("ParseMDLGeometry failed: %v", err)
	}
	built, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{
		Name:            "w_shotgun",
		SourceRef:       "models/w_shotgun.mdl",
		VoxelResolution: 0.02,
		StaticPose:      true,
	})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	asset, voxelCount := built.Asset, built.VoxelCount
	if voxelCount == 0 || asset.Skeleton != nil {
		t.Fatalf("expected static asset without skeleton, got voxels=%d skeleton=%+v", voxelCount, asset.Skeleton)
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
	boneIndex, _ := mdlTriangleBoneOwnershipAtPoint(geometry.Triangles[0], [3]importcommon.Vec3{{X: 0}, {X: 1}, {Y: 1}}, importcommon.Vec3{X: 0.3, Y: 0.3}, 2, 1)
	if boneIndex != 1 {
		t.Fatalf("expected repeated bone vertices to aggregate 0.6 ownership over bone 0's 0.4, got %d", boneIndex)
	}
}

func TestVoxelizeMDLGeometryRespectsMaskedTextureAndCoverage(t *testing.T) {
	texture := MDLTexturePixels{
		Info:    MDLTextureInfo{Flags: mdlTextureFlagMasked, Width: 2, Height: 1},
		Pixels:  []byte{1, 255},
		Palette: make([][3]uint8, 256),
	}
	texture.Palette[1] = [3]uint8{220, 40, 20}
	triangle := MDLTriangle{TextureIndex: 0, Vertices: [3]MDLTriangleVertex{
		{UV: [2]float32{0, 0}},
		{UV: [2]float32{1, 0}},
		{UV: [2]float32{0, 0}},
	}}
	triangleWorld := [3]importcommon.Vec3{{X: 0, Y: 0, Z: 0}, {X: 1, Y: 0, Z: 0}, {X: 0, Y: 1, Z: 0}}
	geometry := MDLGeometry{Textures: []MDLTexturePixels{texture}}

	single := sampleMDLTriangleVoxelColor(geometry, triangle, triangleWorld, [3]int{0, 0, 0}, 1, MDLVoxelizationProfile{CoverageSamples: 1, RespectMaskedTextures: true})
	covered := sampleMDLTriangleVoxelColor(geometry, triangle, triangleWorld, [3]int{0, 0, 0}, 1, MDLVoxelizationProfile{CoverageSamples: 7, RespectMaskedTextures: true})
	if single[3] != 0 {
		t.Fatalf("single center sample should hit masked texel, got %+v", single)
	}
	if covered != ([4]uint8{220, 40, 20, 255}) {
		t.Fatalf("coverage samples should preserve opaque texel crossing voxel, got %+v", covered)
	}
	if color, ok := sampleMDLTexture(texture, 0.75, 0); !ok || color[3] != 0 {
		t.Fatalf("masked palette index 255 should be transparent, got color=%+v ok=%v", color, ok)
	}
}

func TestFillMDLClosedInteriorCapsJointWithoutCopyingSurface(t *testing.T) {
	boneVoxels := map[int]map[[3]int]mdlVoxelSample{0: {}, 1: {}}
	for x := 0; x < 3; x++ {
		for y := 0; y < 3; y++ {
			for z := 0; z < 3; z++ {
				if x != 0 && x != 2 && y != 0 && y != 2 && z != 0 && z != 2 {
					continue
				}
				boneIndex := 0
				if x == 2 {
					boneIndex = 1
				}
				boneVoxels[boneIndex][[3]int{x, y, z}] = mdlVoxelSample{Color: [4]uint8{uint8(100 + x), 20, 20, 255}}
			}
		}
	}
	interior := fillMDLClosedInterior(boneVoxels)
	applyMDLInteriorJointCaps(boneVoxels, []MDLBoneInfo{{Parent: -1}, {Parent: 0}}, interior, 1)
	if _, ok := interior[[3]int{1, 1, 1}]; !ok {
		t.Fatal("expected closed shell center to become interior")
	}
	if _, rootHasChildSurface := boneVoxels[0][[3]int{2, 1, 1}]; rootHasChildSurface {
		t.Fatal("joint cap must not copy exterior surface into connected bone")
	}
	if _, childHasInteriorCap := boneVoxels[1][[3]int{1, 1, 1}]; !childHasInteriorCap {
		t.Fatal("expected child bone to receive interior-only joint cap")
	}
}

func TestPartitionMDLVoxelsBySkeletonHandlesForkAndBentJoint(t *testing.T) {
	leg, legSeed := [3]int{-3, -3, 0}, [3]int{-3, -1, 0}
	foot, footSeed := [3]int{-1, -11, 0}, [3]int{-3, -11, 0}
	boneVoxels := map[int]map[[3]int]mdlVoxelSample{
		0: {leg: {Color: [4]uint8{200, 20, 20, 255}}},
		1: {legSeed: {Color: [4]uint8{200, 20, 20, 255}}},
		4: {foot: {Color: [4]uint8{20, 200, 20, 255}}},
		6: {footSeed: {Color: [4]uint8{20, 200, 20, 255}}},
	}
	bones := []MDLBoneInfo{
		{Name: "pelvis", Parent: -1},
		{Name: "left_leg", Parent: 0, Position: importcommon.Vec3{X: -10}},
		{Name: "right_leg", Parent: 0, Position: importcommon.Vec3{X: 10}},
		{Name: "spine", Parent: 0, Position: importcommon.Vec3{Z: 10}},
		{Name: "left_shin", Parent: 1, Position: importcommon.Vec3{Z: -20}},
		{Name: "right_shin", Parent: 2, Position: importcommon.Vec3{Z: -20}},
		{Name: "left_foot", Parent: 4, Position: importcommon.Vec3{Z: -20}},
		{Name: "left_toe", Parent: 6, Position: importcommon.Vec3{X: 10}},
	}
	if moved := partitionMDLVoxelsBySkeleton(boneVoxels, bones, nil, 0.1); moved != 2 {
		t.Fatalf("expected fork and bent-joint voxels to move, got %d", moved)
	}
	if _, staysPelvis := boneVoxels[0][leg]; staysPelvis {
		t.Fatal("leg voxel remained on branch pelvis")
	}
	if _, movedToLeg := boneVoxels[1][leg]; !movedToLeg {
		t.Fatal("leg voxel was not assigned to nearest branch segment")
	}
	if _, staysShin := boneVoxels[4][foot]; staysShin {
		t.Fatal("foot voxel remained on shin across bent ankle")
	}
	if _, movedToFoot := boneVoxels[6][foot]; !movedToFoot {
		t.Fatal("foot voxel was not assigned to nearest foot segment")
	}
	if count := mdlBoneVoxelCount(boneVoxels); count != 4 {
		t.Fatalf("partition changed unified voxel count: %d", count)
	}
	for _, boneIndex := range []int{2, 3, 5, 7} {
		if len(boneVoxels[boneIndex]) != 0 {
			t.Fatalf("zero-weight control bone %d acquired visible voxels", boneIndex)
		}
	}
}

func TestBuildMDLRigidBoneVoxelAssetUsesPerPartPalettes(t *testing.T) {
	boneVoxels := map[int]map[[3]int]mdlVoxelSample{0: {}, 1: {}}
	for boneIndex := 0; boneIndex < 2; boneIndex++ {
		for i := 0; i < 200; i++ {
			value := boneIndex*200 + i
			boneVoxels[boneIndex][[3]int{i, boneIndex, 0}] = mdlVoxelSample{Color: [4]uint8{uint8(value), uint8(value >> 8), 30, 255}}
		}
	}
	asset, _, voxelCount, err := buildMDLRigidBoneVoxelAsset(MDLGeometry{Info: MDLInfo{Bones: []MDLBoneInfo{{Name: "root", Parent: -1}, {Name: "child", Parent: 0}}}}, MDLVoxelAssetOptions{VoxelizationProfile: DefaultMDLVoxelizationProfile()}, 0.02, boneVoxels)
	if err != nil {
		t.Fatalf("build rigid asset failed: %v", err)
	}
	if voxelCount != 400 || len(asset.Materials) != 400 {
		t.Fatalf("expected 400 preserved colors across local palettes, voxels=%d materials=%d", voxelCount, len(asset.Materials))
	}
	for _, part := range asset.Parts {
		if part.Source.VoxelShape != nil && len(part.Source.VoxelShape.Palette) != 200 {
			t.Fatalf("expected 200-color part palette, got %d for %s", len(part.Source.VoxelShape.Palette), part.ID)
		}
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{}); validation.HasErrors() {
		t.Fatalf("expected per-part palettes to validate, got %+v", validation.Issues)
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
	built, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{Name: "w_test", SourceRef: "models/w_test.mdl", VoxelResolution: 0.005})
	if err != nil {
		t.Fatalf("BuildMDLVoxelAsset failed: %v", err)
	}
	asset, voxelCount := built.Asset, built.VoxelCount
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
	rebuilt, _, err := BuildSPRVoxelAsset(geometry, SPRVoxelAssetOptions{Name: "glow", SourceRef: "sprites/glow01.spr", VoxelResolution: 0.1})
	if err != nil {
		t.Fatalf("rebuild sprite asset: %v", err)
	}
	if rebuilt.ID != asset.ID {
		t.Fatalf("sprite asset ID changed across identical imports: %q != %q", asset.ID, rebuilt.ID)
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
			if entry.SizeBytes == 0 || entry.SHA256 == "" {
				t.Fatalf("%s entry missing source provenance: %+v", kind, entry)
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

func syntheticMDLWithExternalBoneSequenceAnimation() ([]byte, []byte) {
	data := syntheticMDLWithBoneTranslation(10, 20, 30)
	const (
		sequenceSize = mdlSequenceRecordSize176
		groupSize    = 104
		groupHeader  = 76
	)
	sequenceOffset := len(data)
	groupOffset := sequenceOffset + sequenceSize
	out := append(data, make([]byte, sequenceSize+groupSize*2)...)
	writeTestInt32(out, 72, len(out))
	writeTestInt32(out, 164, 1)
	writeTestInt32(out, 168, sequenceOffset)
	writeTestInt32(out, 172, 2)
	writeTestInt32(out, 176, groupOffset)
	writeTestCString(out[sequenceOffset:sequenceOffset+32], "reload_shotgun")
	writeTestFloat32(out, sequenceOffset+32, 10)
	writeTestInt32(out, sequenceOffset+56, 2)
	writeTestInt32(out, sequenceOffset+120, 1)
	writeTestInt32(out, sequenceOffset+124, groupHeader)
	writeTestInt32(out, sequenceOffset+156, 1)
	writeTestCString(out[groupOffset+groupSize+32:groupOffset+groupSize+96], `models\hgrunt01.mdl`)

	boneOffset := len(syntheticMDL())
	writeTestFloat32(out, boneOffset+88, 0.5)
	writeTestFloat32(out, boneOffset+108, 0.1)

	positionStreamOffset := groupHeader + 12
	rotationStreamOffset := positionStreamOffset + 6
	group := make([]byte, rotationStreamOffset+6)
	copy(group[:4], "IDSQ")
	writeTestInt32(group, 4, MDLVersion10)
	writeTestCString(group[8:72], `models\hgrunt01.mdl`)
	writeTestInt32(group, 72, len(group))
	writeTestInt16(group, groupHeader, positionStreamOffset-groupHeader)
	writeTestInt16(group, groupHeader+10, rotationStreamOffset-groupHeader)
	group[positionStreamOffset], group[positionStreamOffset+1] = 2, 2
	writeTestInt16(group, positionStreamOffset+2, 0)
	writeTestInt16(group, positionStreamOffset+4, 4)
	group[rotationStreamOffset], group[rotationStreamOffset+1] = 2, 2
	writeTestInt16(group, rotationStreamOffset+2, 0)
	writeTestInt16(group, rotationStreamOffset+4, 10)
	return out, group
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
