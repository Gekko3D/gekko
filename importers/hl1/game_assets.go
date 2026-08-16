package hl1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

const GameAssetManifestSchemaVersion = 2

const (
	hl1HGruntWeaponMP5             = 1
	hl1HGruntWeaponHandGrenade     = 2
	hl1HGruntWeaponGrenadeLauncher = 4
	hl1HGruntWeaponShotgun         = 8
)

type GameAssetImportResult struct {
	ManifestPath string
	Manifest     *GameAssetManifest
	LibraryPath  string
	Library      *content.AssetLibraryDef
}

type GameAssetManifest struct {
	SchemaVersion int                       `json:"schema_version"`
	Source        importcommon.SourceInfo   `json:"source"`
	Assets        []GameAssetManifestEntry  `json:"assets,omitempty"`
	Catalog       *GameAssetCatalog         `json:"catalog,omitempty"`
	Diagnostics   []importcommon.Diagnostic `json:"diagnostics,omitempty"`
}

// GameAssetCatalog is ActionGame-facing. Asset entries retain importer detail;
// this section exposes stable player, clip, anchor, and weapon IDs.
type GameAssetCatalog struct {
	Players           []GameAssetPlayerCatalogEntry `json:"players,omitempty"`
	WeaponWorldModels []GameAssetWeaponCatalogEntry `json:"weapon_world_models,omitempty"`
}

type GameAssetPlayerCatalogEntry struct {
	ID                    string                               `json:"id"`
	SourceRef             string                               `json:"source_ref"`
	AssetPath             string                               `json:"asset_path"`
	BodygroupModels       []int                                `json:"bodygroup_models"`
	SkinFamily            int                                  `json:"skin_family"`
	HeadMarkerID          string                               `json:"head_marker_id"`
	RightHandMarkerID     string                               `json:"right_hand_marker_id"`
	LeftHandMarkerID      string                               `json:"left_hand_marker_id,omitempty"`
	UpperBodyMarkerID     string                               `json:"upper_body_marker_id"`
	AimMarkerIDs          []string                             `json:"aim_marker_ids,omitempty"`
	SequenceActivities    []GameAssetPlayerSequenceActivity    `json:"sequence_activities,omitempty"`
	CrouchGait            GameAssetPlayerCrouchGait            `json:"crouch_gait"`
	WeaponPresentation    GameAssetPlayerWeaponPresentation    `json:"weapon_presentation"`
	DirectionalLocomotion GameAssetPlayerDirectionalLocomotion `json:"directional_locomotion"`
}

// GameAssetPlayerSequenceActivity preserves GoldSrc activity values from the
// sequence descriptor. ActionGame consumes only cataloged values.
type GameAssetPlayerSequenceActivity struct {
	ClipID   string `json:"clip_id"`
	Activity int    `json:"activity"`
}

const (
	GameAssetPlayerCrouchGaitSupported   = "supported"
	GameAssetPlayerCrouchGaitUnsupported = "unsupported"
	HL1ActivityCrouch                    = 17
	HL1ActivityCrouchIdle                = 18
)

// GameAssetPlayerCrouchGait is authored import output. BoneMask uses generated
// asset item IDs, so consumers never derive a mask from source bone names.
type GameAssetPlayerCrouchGait struct {
	Status           string                          `json:"status"`
	CrouchClipID     string                          `json:"crouch_clip_id,omitempty"`
	CrouchIdleClipID string                          `json:"crouch_idle_clip_id,omitempty"`
	Locomotion       GameAssetPlayerCrouchLocomotion `json:"locomotion,omitempty"`
	BaseStances      []GameAssetPlayerStanceClip     `json:"base_stances,omitempty"`
	BoneMask         []string                        `json:"bone_mask,omitempty"`
	Diagnostic       string                          `json:"diagnostic,omitempty"`
}

type GameAssetPlayerStanceClip struct {
	Stance string `json:"stance"`
	ClipID string `json:"clip_id"`
}

// GameAssetPlayerWeaponPresentation is a verified, source-name allowlisted
// contract. Consumers select IDs here; they never infer animation meaning.
type GameAssetPlayerWeaponPresentation struct {
	Status        string                        `json:"status"`
	Stances       []GameAssetPlayerWeaponStance `json:"stances,omitempty"`
	UpperBodyMask []string                      `json:"upper_body_mask,omitempty"`
	Diagnostic    string                        `json:"diagnostic,omitempty"`
}

type GameAssetPlayerWeaponStance struct {
	Stance             string   `json:"stance"`
	AimClipID          string   `json:"aim_clip_id,omitempty"`
	RecoilClipID       string   `json:"recoil_clip_id,omitempty"`
	CrouchAimClipID    string   `json:"crouch_aim_clip_id,omitempty"`
	CrouchRecoilClipID string   `json:"crouch_recoil_clip_id,omitempty"`
	BoneMask           []string `json:"bone_mask,omitempty"`
	AttachmentMode     string   `json:"attachment_mode,omitempty"`
}

const (
	GameAssetPlayerWeaponPresentationSupported   = "supported"
	GameAssetPlayerWeaponPresentationUnsupported = "unsupported"
)

// GameAssetPlayerCrouchLocomotion is a source-verified directional contract.
// DefaultClipID plus face_travel is an explicit presentation fallback when a
// source only supplies generic ACT_CROUCH gait, as vanilla Crossfire does.
type GameAssetPlayerCrouchLocomotion struct {
	Directional      GameAssetPlayerDirectionalClipSet `json:"directional,omitempty"`
	DefaultClipID    string                            `json:"default_clip_id,omitempty"`
	Fallback         string                            `json:"fallback"`
	BackwardFallback string                            `json:"backward_fallback,omitempty"`
}

const (
	GameAssetPlayerLocomotionFallbackFaceTravel   = "face_travel"
	GameAssetPlayerLocomotionFallbackUnsupported  = "unsupported"
	GameAssetPlayerBackwardFallbackReverseForward = "reverse_forward"
)

// GameAssetPlayerDirectionalLocomotion is source-verified presentation data.
// Empty directions require Fallback; ActionGame must not guess a strafe clip.
type GameAssetPlayerDirectionalLocomotion struct {
	Walk             GameAssetPlayerDirectionalClipSet `json:"walk,omitempty"`
	Run              GameAssetPlayerDirectionalClipSet `json:"run,omitempty"`
	TurnInPlace      *GameAssetPlayerTurnInPlace       `json:"turn_in_place,omitempty"`
	Fallback         string                            `json:"fallback"`
	BackwardFallback string                            `json:"backward_fallback,omitempty"`
}

type GameAssetPlayerTurnInPlace struct {
	LeftClipID               string  `json:"left_clip_id,omitempty"`
	RightClipID              string  `json:"right_clip_id,omitempty"`
	StartAngleDegrees        float32 `json:"start_angle_degrees,omitempty"`
	StopAngleDegrees         float32 `json:"stop_angle_degrees,omitempty"`
	TurnRateDegreesPerSecond float32 `json:"turn_rate_degrees_per_second,omitempty"`
	PlaybackSpeed            float32 `json:"playback_speed,omitempty"`
}

type GameAssetPlayerDirectionalClipSet struct {
	Forward       string `json:"forward,omitempty"`
	Backward      string `json:"backward,omitempty"`
	Left          string `json:"left,omitempty"`
	Right         string `json:"right,omitempty"`
	ForwardLeft   string `json:"forward_left,omitempty"`
	ForwardRight  string `json:"forward_right,omitempty"`
	BackwardLeft  string `json:"backward_left,omitempty"`
	BackwardRight string `json:"backward_right,omitempty"`
}

type GameAssetWeaponCatalogEntry struct {
	ID        string `json:"id"`
	SourceRef string `json:"source_ref"`
	AssetPath string `json:"asset_path"`
}

type GameAssetManifestEntry struct {
	Kind                             string                               `json:"kind"`
	SourceRef                        string                               `json:"source_ref"`
	SourcePath                       string                               `json:"source_path,omitempty"`
	GeneratedAssetPath               string                               `json:"generated_asset_path,omitempty"`
	GeneratedVoxelCount              int                                  `json:"generated_voxel_count,omitempty"`
	GeneratedVoxelResolution         float32                              `json:"generated_voxel_resolution,omitempty"`
	GeneratedVoxelResolutionCategory string                               `json:"generated_voxel_resolution_category,omitempty"`
	GeneratedVoxelizationProfile     *MDLVoxelizationProfile              `json:"generated_voxelization_profile,omitempty"`
	CompatibilityFallback            bool                                 `json:"compatibility_fallback,omitempty"`
	CatalogKind                      string                               `json:"catalog_kind,omitempty"`
	CatalogID                        string                               `json:"catalog_id,omitempty"`
	BodygroupModels                  []int                                `json:"bodygroup_models,omitempty"`
	SkinFamily                       int                                  `json:"skin_family,omitempty"`
	HeadMarkerID                     string                               `json:"head_marker_id,omitempty"`
	RightHandMarkerID                string                               `json:"right_hand_marker_id,omitempty"`
	LeftHandMarkerID                 string                               `json:"left_hand_marker_id,omitempty"`
	UpperBodyMarkerID                string                               `json:"upper_body_marker_id,omitempty"`
	AimMarkerIDs                     []string                             `json:"aim_marker_ids,omitempty"`
	SequenceActivities               []GameAssetPlayerSequenceActivity    `json:"sequence_activities,omitempty"`
	CrouchGait                       GameAssetPlayerCrouchGait            `json:"crouch_gait,omitempty"`
	WeaponPresentation               GameAssetPlayerWeaponPresentation    `json:"weapon_presentation,omitempty"`
	DirectionalLocomotion            GameAssetPlayerDirectionalLocomotion `json:"directional_locomotion,omitempty"`
	SizeBytes                        int64                                `json:"size_bytes,omitempty"`
	SHA256                           string                               `json:"sha256,omitempty"`
	Resolved                         bool                                 `json:"resolved"`
	UsedBy                           []string                             `json:"used_by,omitempty"`
	ConvertState                     string                               `json:"convert_state,omitempty"`
	ModelInfo                        *MDLInfo                             `json:"model_info,omitempty"`
	SpriteInfo                       *SPRInfo                             `json:"sprite_info,omitempty"`
	GeneratedExtras                  []GameAssetGeneratedExtra            `json:"generated_extras,omitempty"`
	generatedAsset                   *content.AssetDef                    `json:"-"`
	generatedAnimations              []MDLAnimationDocuments              `json:"-"`
}

// GameAssetGeneratedExtra is an additional generic asset emitted from one
// source document, such as body-worn equipment separated from a held prop.
type GameAssetGeneratedExtra struct {
	Key        string `json:"key"`
	AssetPath  string `json:"asset_path"`
	VoxelCount int    `json:"voxel_count,omitempty"`
	asset      *content.AssetDef
}

// LoadGameAssetManifest reads the ActionGame-facing HL1 asset catalog.
func LoadGameAssetManifest(path string) (*GameAssetManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var manifest GameAssetManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != GameAssetManifestSchemaVersion {
		return nil, fmt.Errorf("unsupported game asset manifest schema version %d", manifest.SchemaVersion)
	}
	return &manifest, nil
}

func BuildGameAssetImport(opts ImportOptions, summary ImportSummary) (GameAssetImportResult, error) {
	mapName := summary.Report.Source.MapName
	if mapName == "" {
		mapName = trimMapName(opts.MapName, opts.BSPPath)
	}
	if mapName == "" {
		mapName = "hl1_map"
	}
	outputRoot := strings.TrimSpace(opts.OutputRoot)
	if outputRoot == "" {
		return GameAssetImportResult{}, fmt.Errorf("output root is required")
	}
	assetOutputRoot := strings.TrimSpace(opts.AssetOutputRoot)
	libraryPath := strings.TrimSpace(opts.AssetLibraryPath)
	centralAssets := assetOutputRoot != "" || libraryPath != ""
	if centralAssets && (assetOutputRoot == "" || libraryPath == "") {
		return GameAssetImportResult{}, fmt.Errorf("central asset output requires both asset output root and asset library path")
	}
	gameDir := strings.TrimSpace(opts.GameDir)
	if gameDir == "" {
		gameDir = strings.TrimSpace(summary.Report.Source.GameDir)
	}
	if gameDir == "" {
		gameDir = InferGameDirFromBSPPath(summary.Report.Source.BSPPath)
	}
	manifestPath := filepath.Join(outputRoot, "hl1_assets", mapName, "manifest.gkhl1assets")
	if centralAssets {
		manifestPath = filepath.Join(assetOutputRoot, "hl1", "manifests", mapName+".gkhl1assets")
	} else {
		assetOutputRoot = outputRoot
		libraryPath = filepath.Join(outputRoot, "hl1_assets", mapName, "assets.gkassetlibrary")
	}
	manifest := &GameAssetManifest{
		SchemaVersion: GameAssetManifestSchemaVersion,
		Source:        summary.Report.Source,
	}
	manifest.Source.GameDir = gameDir
	var existingLibrary *content.AssetLibraryDef
	if centralAssets {
		loaded, err := content.LoadAssetLibrary(libraryPath)
		if err == nil {
			existingLibrary = loaded
		} else if !os.IsNotExist(err) {
			return GameAssetImportResult{}, err
		}
	}
	collector := newHL1AssetCollector(gameDir, opts.ResourceDirs, assetOutputRoot, mapName, centralAssets, EffectiveHL1VoxelResolutionPolicy(opts))
	catalogDirs := hl1CatalogResourceDirs(gameDir, opts.ResourceDirs)
	for _, wadPath := range summary.Report.Source.WADPaths {
		collector.addAbsolute("wad", wadPath, "worldspawn.wad")
	}
	for _, entity := range summary.Map.Entities {
		usedBy := entity.ClassName
		if usedBy == "" {
			usedBy = "entity"
		}
		if _, ok := hl1PickupClass(entity.ClassName); ok {
			usedBy = "pickup:" + strings.ToLower(strings.TrimSpace(entity.ClassName))
			collector.addPickupModelRefs(hl1PickupModelRefs(entity.ClassName), usedBy+".model")
		}
		if _, ok := hl1NPCClass(entity.ClassName); ok {
			usedBy = "npc:" + strings.ToLower(strings.TrimSpace(entity.ClassName))
			if modelRef := hl1NPCModelRef(entity.ClassName, entity); modelRef != "" {
				collector.addRef(modelRef, usedBy+".model")
			}
		}
		for key, value := range entity.KeyValues {
			if strings.EqualFold(key, "wad") {
				continue
			}
			for _, ref := range extractHL1AssetRefs(value) {
				collector.addRef(ref, usedBy+"."+key)
			}
		}
	}
	if opts.ImportAllPlayerModels {
		for _, resourceDir := range catalogDirs {
			collector.addCatalogModels("player", resourceDir, hl1CatalogModelPaths(resourceDir, true))
		}
	}
	if opts.ImportAllNPCModels {
		for _, resourceDir := range catalogDirs {
			collector.addCatalogModels("npc", resourceDir, hl1NPCModelPaths(resourceDir))
		}
	}
	if opts.ImportAllStaticProps {
		for _, resourceDir := range catalogDirs {
			collector.addCatalogModels("static_prop", resourceDir, hl1StaticPropModelPaths(resourceDir))
		}
	}
	if opts.ImportAllWeaponWorldModels {
		for _, resourceDir := range catalogDirs {
			collector.addCatalogModels("weapon_world", resourceDir, hl1CatalogModelPaths(resourceDir, false))
			collector.addCatalogModels("weapon_held", resourceDir, hl1HeldWeaponModelPaths(resourceDir))
		}
	}
	manifest.Assets, manifest.Diagnostics = collector.buildEntries()
	if existingLibrary != nil {
		reuseExistingHL1Assets(manifest.Assets, existingLibrary, libraryPath)
	}
	manifest.Catalog = buildGameAssetCatalog(manifest.Assets)
	library := buildHL1AssetLibrary(manifest.Assets, libraryPath)
	if existingLibrary != nil {
		var err error
		library, err = mergeHL1AssetLibraries(pruneHL1CatalogEntries(existingLibrary, opts), library)
		if err != nil {
			return GameAssetImportResult{}, err
		}
	}
	return GameAssetImportResult{ManifestPath: manifestPath, Manifest: manifest, LibraryPath: libraryPath, Library: library}, nil
}

func pruneHL1CatalogEntries(library *content.AssetLibraryDef, opts ImportOptions) *content.AssetLibraryDef {
	if library == nil {
		return nil
	}
	pruned := *library
	pruned.Entries = make([]content.AssetLibraryEntryDef, 0, len(library.Entries))
	for _, entry := range library.Entries {
		selected := opts.ImportAllPlayerModels && hasAnyString(entry.Tags, "player")
		selected = selected || opts.ImportAllNPCModels && hl1AssetLibraryEntryIsNPC(entry)
		selected = selected || opts.ImportAllStaticProps && hasAnyString(entry.Tags, "static_prop")
		selected = selected || opts.ImportAllWeaponWorldModels && hasAnyString(entry.Tags, "weapon_world", "weapon_held")
		if !selected {
			pruned.Entries = append(pruned.Entries, entry)
		}
	}
	return &pruned
}

func reuseExistingHL1Assets(entries []GameAssetManifestEntry, library *content.AssetLibraryDef, libraryPath string) {
	if library == nil {
		return
	}
	byIdentity := make(map[string]string)
	npcByIdentity := make(map[string]string)
	for _, entry := range library.Entries {
		if !hasAnyString(entry.Tags, "player", "npc", "static_prop", "weapon_world", "weapon_held") {
			continue
		}
		sourceRef := strings.TrimPrefix(firstStringWithPrefix(entry.Tags, "source_ref:"), "source_ref:")
		sourceHash := strings.TrimPrefix(firstStringWithPrefix(entry.Tags, "source_sha256:"), "source_sha256:")
		config := strings.TrimPrefix(firstStringWithPrefix(entry.Tags, "import_config:"), "import_config:")
		if config == "" {
			continue
		}
		path, err := content.ResolveAssetLibraryPath(library, libraryPath, entry.Key)
		if err == nil {
			if _, err := content.LoadAsset(path); err != nil {
				continue
			}
			for _, identity := range []string{sourceRef, sourceHash} {
				if identity == "" {
					continue
				}
				key := identity + "\x00" + config
				if _, exists := byIdentity[key]; !exists {
					byIdentity[key] = path
				}
				if hasAnyString(entry.Tags, "npc") {
					npcByIdentity[identity] = path
				}
			}
		}
	}
	for index := range entries {
		entry := &entries[index]
		if entry.CatalogKind != "" || entry.GeneratedAssetPath == "" {
			continue
		}
		config := hl1AssetImportConfig(*entry)
		path := byIdentity[strings.ToLower(filepath.ToSlash(entry.SourceRef))+"\x00"+config]
		if path == "" && entry.SHA256 != "" {
			path = byIdentity[entry.SHA256+"\x00"+config]
		}
		if path == "" && gameAssetEntryUsedByNPC(*entry) {
			path = npcByIdentity[strings.ToLower(filepath.ToSlash(entry.SourceRef))]
			if path == "" && entry.SHA256 != "" {
				path = npcByIdentity[entry.SHA256]
			}
		}
		if path != "" {
			entry.GeneratedAssetPath = path
			entry.generatedAsset = nil
			entry.ConvertState = "reused_catalog_asset"
		}
	}
}

func gameAssetEntryUsedByNPC(entry GameAssetManifestEntry) bool {
	for _, usedBy := range entry.UsedBy {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "npc:") {
			return true
		}
	}
	return false
}

func firstStringWithPrefix(values []string, prefix string) string {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return value
		}
	}
	return ""
}

func hasAnyString(values []string, wanted ...string) bool {
	for _, value := range values {
		for _, candidate := range wanted {
			if value == candidate {
				return true
			}
		}
	}
	return false
}

func hl1AssetLibraryEntryIsNPC(entry content.AssetLibraryEntryDef) bool {
	if hasAnyString(entry.Tags, "player") {
		return false
	}
	if hasAnyString(entry.Tags, "npc") {
		return true
	}
	sourceRef := strings.TrimPrefix(firstStringWithPrefix(entry.Tags, "source_ref:"), "source_ref:")
	return sourceRef != "" && hl1KnownActorModel(filepath.Base(sourceRef))
}

func mergeHL1AssetLibraries(existing, incoming *content.AssetLibraryDef) (*content.AssetLibraryDef, error) {
	if existing == nil {
		return incoming, nil
	}
	if incoming == nil {
		return existing, nil
	}
	byKey := make(map[string]content.AssetLibraryEntryDef, len(existing.Entries)+len(incoming.Entries))
	for _, entry := range existing.Entries {
		byKey[entry.Key] = entry
	}
	for _, entry := range incoming.Entries {
		if prior, ok := byKey[entry.Key]; ok && filepath.Clean(prior.AssetPath) != filepath.Clean(entry.AssetPath) {
			return nil, fmt.Errorf("asset library key %q conflicts: %q != %q", entry.Key, prior.AssetPath, entry.AssetPath)
		}
		byKey[entry.Key] = entry
	}
	existing.Entries = existing.Entries[:0]
	for _, entry := range byKey {
		existing.Entries = append(existing.Entries, entry)
	}
	sort.Slice(existing.Entries, func(i, j int) bool { return existing.Entries[i].Key < existing.Entries[j].Key })
	return existing, nil
}

func (c *hl1AssetCollector) addCatalogModels(kind, resourceDir string, paths []string) {
	for _, path := range paths {
		refRoot := resourceDir
		if rel, err := filepath.Rel(c.gameDir, path); err == nil && !strings.HasPrefix(rel, "..") {
			refRoot = c.gameDir
		}
		ref, err := filepath.Rel(refRoot, path)
		if err != nil || strings.HasPrefix(ref, "..") {
			continue
		}
		ref = filepath.ToSlash(ref)
		if kind == "npc" {
			if rel, err := filepath.Rel(resourceDir, path); err == nil && !strings.HasPrefix(rel, "..") {
				ref = filepath.ToSlash(rel)
			}
		}
		if kind == "weapon_world" || kind == "weapon_held" {
			// Catalog identity is source-layout independent so an overlay fills a
			// missing base-game model without creating a competing library key.
			ref = filepath.ToSlash(filepath.Join("valve", "models", filepath.Base(path)))
		}
		if kind == "npc" {
			info, err := LoadMDLInfo(path)
			if err != nil {
				c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.npc_model_parse_failed", Subject: ref, Message: err.Error()})
				continue
			}
			for _, variant := range hl1NPCModelVariants(ref, info) {
				id := safeMDLAssetID(strings.TrimSuffix(ref, filepath.Ext(ref))) + variant.suffix
				c.addCatalogModel(kind, ref, path, id, variant.bodygroupModels, 0)
			}
			continue
		}
		if kind != "player" {
			id := safeMDLAssetID(strings.TrimSuffix(ref, filepath.Ext(ref)))
			c.addCatalogModel(kind, ref, path, id, nil, 0)
			continue
		}
		info, err := LoadMDLInfo(path)
		if err != nil {
			c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_model_parse_failed", Subject: ref, Message: err.Error()})
			continue
		}
		for _, variant := range hl1PlayerModelVariants(info) {
			id := hl1PlayerCatalogID(ref, variant.bodygroupModels, variant.skinFamily)
			c.addCatalogModel(kind, ref, path, id, variant.bodygroupModels, variant.skinFamily)
		}
	}
}

type hl1NPCModelVariant struct {
	bodygroupModels []int
	suffix          string
}

func hl1NPCModelVariants(sourceRef string, info MDLInfo) []hl1NPCModelVariant {
	models := make([]int, len(info.BodyParts))
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(sourceRef), filepath.Ext(sourceRef)))
	if base == "barney" {
		for index, part := range info.BodyParts {
			if strings.EqualFold(strings.TrimSpace(part.Name), "gun") && part.ModelCount >= 3 {
				models[index] = 2
				return []hl1NPCModelVariant{{bodygroupModels: models, suffix: "_gone"}}
			}
		}
	}
	if base != "hgrunt" {
		return []hl1NPCModelVariant{{bodygroupModels: models}}
	}
	headGroup, gunGroup := -1, -1
	for index, part := range info.BodyParts {
		switch strings.ToLower(strings.TrimSpace(part.Name)) {
		case "heads":
			if part.ModelCount >= 4 {
				headGroup = index
			}
		case "weapons":
			if part.ModelCount >= 3 {
				gunGroup = index
			}
		}
	}
	if headGroup < 0 || gunGroup < 0 {
		return []hl1NPCModelVariant{{bodygroupModels: models}}
	}
	variant := func(head, gun int, suffix string) hl1NPCModelVariant {
		selection := append([]int(nil), models...)
		selection[headGroup], selection[gunGroup] = head, gun
		return hl1NPCModelVariant{bodygroupModels: selection, suffix: suffix}
	}
	return []hl1NPCModelVariant{
		variant(0, 0, ""),
		variant(3, 0, "_m203"),
		variant(2, 1, "_shotgun"),
		variant(0, 2, "_nogun"),
		variant(3, 2, "_m203_nogun"),
		variant(2, 2, "_shotgun_nogun"),
	}
}

func hl1HGruntCatalogSuffixForWeapons(weapons int) string {
	if weapons == 0 {
		weapons = hl1HGruntWeaponMP5 | hl1HGruntWeaponHandGrenade
	}
	if weapons&hl1HGruntWeaponShotgun != 0 {
		return "_shotgun"
	}
	if weapons&hl1HGruntWeaponMP5 == 0 {
		return "_nogun"
	}
	if weapons&hl1HGruntWeaponGrenadeLauncher != 0 {
		return "_m203"
	}
	return ""
}

type hl1PlayerModelVariant struct {
	bodygroupModels []int
	skinFamily      int
}

func hl1PlayerModelVariants(info MDLInfo) []hl1PlayerModelVariant {
	variants := []hl1PlayerModelVariant{{bodygroupModels: make([]int, len(info.BodyParts))}}
	for partIndex, part := range info.BodyParts {
		if part.ModelCount <= 0 {
			return nil
		}
		next := make([]hl1PlayerModelVariant, 0, len(variants)*part.ModelCount)
		for _, variant := range variants {
			for model := 0; model < part.ModelCount; model++ {
				selection := append([]int(nil), variant.bodygroupModels...)
				selection[partIndex] = model
				next = append(next, hl1PlayerModelVariant{bodygroupModels: selection})
			}
		}
		variants = next
	}
	skinFamilies := info.SkinFamilyCount
	if skinFamilies <= 0 {
		skinFamilies = 1
	}
	out := make([]hl1PlayerModelVariant, 0, len(variants)*skinFamilies)
	for _, variant := range variants {
		for skin := 0; skin < skinFamilies; skin++ {
			variant.skinFamily = skin
			out = append(out, variant)
		}
	}
	return out
}

func hl1PlayerCatalogID(sourceRef string, bodygroupModels []int, skinFamily int) string {
	parts := []string{safeMDLAssetID(strings.TrimSuffix(sourceRef, filepath.Ext(sourceRef)))}
	for _, model := range bodygroupModels {
		parts = append(parts, fmt.Sprintf("b%d", model))
	}
	return strings.Join(append(parts, fmt.Sprintf("s%d", skinFamily)), "_")
}

func hl1CatalogModelPaths(gameDir string, players bool) []string {
	var out []string
	_ = filepath.WalkDir(gameDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".mdl") {
			return nil
		}
		clean := filepath.ToSlash(filepath.Clean(path))
		isPlayer := strings.Contains(strings.ToLower(clean), "/models/player/")
		if players != isPlayer {
			return nil
		}
		if !players {
			base := strings.ToLower(filepath.Base(path))
			if !strings.HasPrefix(base, "w_") || hl1TextureCompanionModel(path) {
				return nil
			}
		}
		out = append(out, filepath.Clean(path))
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		left, right := hl1CatalogModelSourcePriority(out[i]), hl1CatalogModelSourcePriority(out[j])
		if left != right {
			return left < right
		}
		return out[i] < out[j]
	})
	return out
}

func hl1CatalogModelSourcePriority(path string) int {
	clean := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	switch {
	case strings.HasPrefix(clean, "valve/models/") || strings.Contains(clean, "/valve/models/"):
		return 0
	case strings.HasPrefix(clean, "valve_downloads/models/") || strings.Contains(clean, "/valve_downloads/models/"):
		return 1
	default:
		return 2
	}
}

func hl1HeldWeaponModelPaths(gameDir string) []string {
	var out []string
	_ = filepath.WalkDir(gameDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".mdl") {
			return nil
		}
		base := strings.ToLower(filepath.Base(path))
		if strings.HasPrefix(base, "p_") && !hl1TextureCompanionModel(path) {
			out = append(out, filepath.Clean(path))
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		left, right := hl1CatalogModelSourcePriority(out[i]), hl1CatalogModelSourcePriority(out[j])
		if left != right {
			return left < right
		}
		return out[i] < out[j]
	})
	return out
}

func hl1StaticPropModelPaths(gameDir string) []string {
	var out []string
	_ = filepath.WalkDir(gameDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".mdl") {
			return nil
		}
		clean := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
		base := strings.ToLower(filepath.Base(path))
		if strings.Contains(clean, "/models/player/") || strings.HasPrefix(base, "p_") || strings.HasPrefix(base, "v_") || strings.HasPrefix(base, "w_") || hl1TextureCompanionModel(path) || hl1KnownActorModel(base) {
			return nil
		}
		out = append(out, filepath.Clean(path))
		return nil
	})
	sort.Strings(out)
	return out
}

func hl1NPCModelPaths(gameDir string) []string {
	var out []string
	_ = filepath.WalkDir(gameDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(path), ".mdl") {
			return nil
		}
		clean := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
		if strings.Contains(clean, "/models/player/") || hl1TextureCompanionModel(path) || !hl1KnownActorModel(filepath.Base(path)) {
			return nil
		}
		out = append(out, filepath.Clean(path))
		return nil
	})
	sort.Strings(out)
	return out
}

func hl1KnownActorModel(base string) bool {
	base = strings.TrimSuffix(strings.ToLower(base), ".mdl")
	switch base {
	case "agrunt", "apache", "baby_headcrab", "barnacle", "barney", "big_mom", "bullsquid", "cockroach", "controller", "garg", "gman", "hassassin", "headcrab", "hgrunt", "hornet", "houndeye", "icky", "islave", "leech", "miniturret", "nihilanth", "osprey", "scientist", "sentry", "snark", "tentacle2", "turret", "zombie":
		return true
	default:
		return false
	}
}

func hl1TextureCompanionModel(path string) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if !strings.HasSuffix(strings.ToLower(base), "t") {
		return false
	}
	plain := base[:len(base)-1] + filepath.Ext(path)
	return fileExists(filepath.Join(filepath.Dir(path), plain))
}

func SaveGameAssetImport(result GameAssetImportResult) error {
	if result.Manifest == nil {
		return fmt.Errorf("game asset manifest is nil")
	}
	type stagedFile struct{ temporary, target string }
	var staged []stagedFile
	stagedTargets := map[string]struct{}{}
	defer func() {
		for _, file := range staged {
			_ = os.Remove(file.temporary)
		}
	}()
	stage := func(target string, save func(string) error) error {
		target = filepath.Clean(target)
		if _, exists := stagedTargets[target]; exists {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		file, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
		if err != nil {
			return err
		}
		temporary := file.Name()
		if err := file.Close(); err != nil {
			_ = os.Remove(temporary)
			return err
		}
		if err := save(temporary); err != nil {
			_ = os.Remove(temporary)
			return err
		}
		staged = append(staged, stagedFile{temporary: temporary, target: target})
		stagedTargets[target] = struct{}{}
		return nil
	}
	for i := range result.Manifest.Assets {
		entry := &result.Manifest.Assets[i]
		if entry.generatedAsset != nil && entry.GeneratedAssetPath != "" {
			preserveCompatibleAnimationSetPaths(entry.GeneratedAssetPath, entry.generatedAsset)
			for _, generated := range entry.generatedAnimations {
				if generated.Rig != nil {
					if err := stage(generated.RigPath, func(path string) error { return content.SaveAnimationRig(path, generated.Rig) }); err != nil {
						return err
					}
				}
				if generated.Set != nil {
					if err := stage(generated.SetPath, func(path string) error { return content.SaveAnimationSet(path, generated.Set) }); err != nil {
						return err
					}
				}
			}
			if err := stage(entry.GeneratedAssetPath, func(path string) error { return content.SaveAsset(path, entry.generatedAsset) }); err != nil {
				return fmt.Errorf("stage generated asset %s: %w", entry.GeneratedAssetPath, err)
			}
		}
		for _, extra := range entry.GeneratedExtras {
			if extra.asset == nil || extra.AssetPath == "" {
				continue
			}
			if err := stage(extra.AssetPath, func(path string) error { return content.SaveAsset(path, extra.asset) }); err != nil {
				return fmt.Errorf("stage generated asset %s: %w", extra.AssetPath, err)
			}
		}
	}
	if result.Library != nil && result.LibraryPath != "" {
		if err := stage(result.LibraryPath, func(path string) error { return content.SaveAssetLibrary(path, result.Library) }); err != nil {
			return err
		}
	}
	data, err := json.MarshalIndent(result.Manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := stage(result.ManifestPath, func(path string) error { return os.WriteFile(path, data, 0644) }); err != nil {
		return err
	}
	for _, file := range staged {
		if err := os.Rename(file.temporary, file.target); err != nil {
			return fmt.Errorf("replace %s: %w", file.target, err)
		}
	}
	staged = nil
	return nil
}

func preserveCompatibleAnimationSetPaths(path string, generated *content.AssetDef) {
	if generated == nil || generated.Skeleton == nil {
		return
	}
	existing, err := content.LoadAsset(path)
	if err != nil || !reflect.DeepEqual(existing.Skeleton, generated.Skeleton) {
		return
	}
	for _, ref := range existing.AnimationSetPaths {
		name := filepath.Base(filepath.ToSlash(ref))
		if strings.HasPrefix(name, "animation.") && strings.HasSuffix(name, ".gkanim") {
			continue
		}
		generated.AnimationSetPaths = appendUniqueString(generated.AnimationSetPaths, ref)
	}
}

func buildHL1AssetLibrary(entries []GameAssetManifestEntry, libraryPath string) *content.AssetLibraryDef {
	library := content.NewAssetLibraryDef("HL1 imported assets")
	byBaseKey := make(map[string][]GameAssetManifestEntry)
	for _, entry := range entries {
		if entry.ConvertState == "reused_catalog_asset" {
			continue
		}
		key := hl1GenericAssetKey(entry)
		if key != "" && entry.GeneratedAssetPath != "" {
			byBaseKey[key] = append(byBaseKey[key], entry)
		}
		for _, extra := range entry.GeneratedExtras {
			if extra.Key == "" || extra.AssetPath == "" {
				continue
			}
			extraEntry := entry
			extraEntry.GeneratedAssetPath = extra.AssetPath
			byBaseKey[extra.Key] = append(byBaseKey[extra.Key], extraEntry)
		}
	}
	byKey := make(map[string]GameAssetManifestEntry)
	for baseKey, candidates := range byBaseKey {
		sort.Slice(candidates, func(i, j int) bool { return hl1LibraryEntryLess(candidates[i], candidates[j]) })
		for index, entry := range candidates {
			key := baseKey
			if index > 0 {
				key += ".source." + safeMDLAssetID(strings.TrimSuffix(entry.SourceRef, filepath.Ext(entry.SourceRef)))
			}
			if prior, exists := byKey[key]; exists && filepath.Clean(prior.GeneratedAssetPath) != filepath.Clean(entry.GeneratedAssetPath) {
				continue
			}
			byKey[key] = entry
		}
	}
	for key, entry := range byKey {
		path, err := filepath.Rel(filepath.Dir(libraryPath), entry.GeneratedAssetPath)
		if err != nil {
			continue
		}
		libraryEntry := content.AssetLibraryEntryDef{Key: key, AssetPath: filepath.ToSlash(path), Tags: hl1AssetLibraryTags(entry)}
		if entry.CatalogKind == "player" {
			libraryEntry.Character = hl1CharacterPresentation(entry)
		}
		library.Entries = append(library.Entries, libraryEntry)
	}
	sort.Slice(library.Entries, func(i, j int) bool { return library.Entries[i].Key < library.Entries[j].Key })
	return library
}

func hl1LibraryEntryLess(left, right GameAssetManifestEntry) bool {
	leftPath, rightPath := left.SourcePath, right.SourcePath
	if leftPath == "" {
		leftPath = left.SourceRef
	}
	if rightPath == "" {
		rightPath = right.SourceRef
	}
	leftPriority, rightPriority := hl1CatalogModelSourcePriority(leftPath), hl1CatalogModelSourcePriority(rightPath)
	if leftPriority != rightPriority {
		return leftPriority < rightPriority
	}
	if strings.ToLower(left.SourceRef) != strings.ToLower(right.SourceRef) {
		return strings.ToLower(left.SourceRef) < strings.ToLower(right.SourceRef)
	}
	return left.GeneratedAssetPath < right.GeneratedAssetPath
}

func AddGeneratedLevelAssetsToLibrary(result *GameAssetImportResult, generated GeneratedLevelResult) error {
	if result == nil || strings.TrimSpace(result.LibraryPath) == "" {
		return nil
	}
	if result.Library == nil {
		result.Library = content.NewAssetLibraryDef("HL1 imported assets")
	}
	mapName := "map"
	if generated.Level != nil {
		if name := safeMDLAssetID(generated.Level.Name); name != "" {
			mapName = name
		}
	}
	type assetGroup struct {
		group      string
		sourceKind string
		assets     []GeneratedAssetResult
	}
	groups := []assetGroup{
		{group: "brushes", sourceKind: "bsp_brush", assets: generated.StaticBrushAssets},
		{group: "moving", sourceKind: "bsp_brush", assets: generated.MovingBrushAssets},
		{group: "fixtures", sourceKind: "bsp_brush", assets: generated.ChargerAssets},
		{group: "breakables", sourceKind: "bsp_brush", assets: generated.BreakableAssets},
		{group: "fixtures", sourceKind: "generated_fixture", assets: generated.LightFixtureAssets},
	}
	byKey := make(map[string]content.AssetLibraryEntryDef, len(result.Library.Entries))
	for _, entry := range result.Library.Entries {
		byKey[entry.Key] = entry
	}
	for _, set := range groups {
		for _, generatedAsset := range set.assets {
			if generatedAsset.Asset == nil || strings.TrimSpace(generatedAsset.AssetPath) == "" {
				continue
			}
			assetID := safeMDLAssetID(generatedAsset.Asset.Name)
			if assetID == "" {
				assetID = safeMDLAssetID(strings.TrimSuffix(filepath.Base(generatedAsset.AssetPath), filepath.Ext(generatedAsset.AssetPath)))
			}
			if assetID == "" {
				continue
			}
			assetPath, err := filepath.Rel(filepath.Dir(result.LibraryPath), generatedAsset.AssetPath)
			if err != nil {
				return err
			}
			key := strings.Join([]string{"maps", mapName, set.group, assetID}, ".")
			tags := append([]string(nil), generatedAsset.Asset.Tags...)
			for _, material := range generatedAsset.Asset.Materials {
				for _, tag := range material.Tags {
					tags = appendUniqueString(tags, tag)
				}
			}
			for _, tag := range []string{"source:hl1", "prop", "source_kind:" + set.sourceKind, "group:" + set.group, "scope:" + mapName} {
				tags = appendUniqueString(tags, tag)
			}
			entry := content.AssetLibraryEntryDef{Key: key, AssetPath: filepath.ToSlash(assetPath), Tags: tags}
			if prior, ok := byKey[key]; ok && filepath.Clean(prior.AssetPath) != filepath.Clean(entry.AssetPath) {
				return fmt.Errorf("asset library key %q conflicts: %q != %q", key, prior.AssetPath, entry.AssetPath)
			}
			byKey[key] = entry
		}
	}
	result.Library.Entries = result.Library.Entries[:0]
	for _, entry := range byKey {
		result.Library.Entries = append(result.Library.Entries, entry)
	}
	sort.Slice(result.Library.Entries, func(i, j int) bool { return result.Library.Entries[i].Key < result.Library.Entries[j].Key })
	return nil
}

func hl1AssetLibraryTags(entry GameAssetManifestEntry) []string {
	tags := []string{"source:hl1", "source_kind:" + entry.Kind, "scope:global", "import_config:" + hl1AssetImportConfig(entry)}
	if entry.SourceRef != "" {
		tags = append(tags, "source_ref:"+strings.ToLower(filepath.ToSlash(entry.SourceRef)))
	}
	if entry.SHA256 != "" {
		tags = append(tags, "source_sha256:"+entry.SHA256)
	}
	if entry.CatalogKind != "" {
		tags = append(tags, entry.CatalogKind)
	}
	switch entry.CatalogKind {
	case "player":
		tags = append(tags, "group:characters")
	case "npc":
		tags = append(tags, "group:characters")
	case "weapon_world", "weapon_held":
		tags = append(tags, "group:weapons")
	case "":
		tags = append(tags, "group:environment")
	}
	if entry.CatalogKind != "static_prop" {
		return tags
	}
	group := hl1StaticPropGroup(entry.SourceRef)
	tags = append(tags, "prop", "group:"+group)
	if group != "other" {
		tags = append(tags, "classification:inferred")
	}
	if entry.generatedAsset != nil {
		for _, material := range entry.generatedAsset.Materials {
			for _, tag := range material.Tags {
				tags = appendUniqueString(tags, tag)
			}
		}
	}
	return tags
}

func hl1StaticPropGroup(sourceRef string) string {
	name := strings.ToLower(strings.TrimSuffix(filepath.Base(sourceRef), filepath.Ext(sourceRef)))
	switch {
	case containsAny(name, "table", "desk", "chair", "bench", "cabinet", "locker", "shelf"):
		return "furniture"
	case containsAny(name, "crate", "box", "barrel", "pallet", "container"):
		return "containers"
	case containsAny(name, "sandbag", "barricade", "barrier"):
		return "cover"
	case containsAny(name, "lamp", "light", "fixture"):
		return "fixtures"
	case containsAny(name, "pipe", "vent", "duct", "machine", "generator"):
		return "industrial"
	case containsAny(name, "plant", "tree", "cactus"):
		return "vegetation"
	default:
		return "other"
	}
}

func hl1CharacterPresentation(entry GameAssetManifestEntry) *content.CharacterPresentationDef {
	def := &content.CharacterPresentationDef{
		HeadMarkerID: entry.HeadMarkerID, RightHandMarkerID: entry.RightHandMarkerID, LeftHandMarkerID: entry.LeftHandMarkerID, UpperBodyMarkerID: entry.UpperBodyMarkerID,
		AimMarkerIDs: append([]string(nil), entry.AimMarkerIDs...),
		AimRig: content.CharacterAimRigDef{
			Status:           content.CharacterPresentationSupported,
			MuzzleMarkerKind: content.AssetMarkerKindMuzzle,
			// HL1's player bind basis is +X forward, +Y up. This is adapted
			// into generic authored aim data; no HL1 identity reaches runtime.
			// GoldSrc supplies bind transforms and animation keys, but no joint
			// limits. Leave limits unconstrained rather than inventing them; a
			// generic asset may author limits when it has that information.
			ForwardAxis: content.Vec3{1, 0, 0},
			UpAxis:      content.Vec3{0, 1, 0},
			BoneWeights: []float32{0.08, 0.12, 0.16, 0.20, 0.24, 0.20},
		},
		CrouchGait: content.CharacterCrouchGaitDef{Status: entry.CrouchGait.Status, CrouchClipID: entry.CrouchGait.CrouchClipID, CrouchIdleClipID: entry.CrouchGait.CrouchIdleClipID, BoneMask: append([]string(nil), entry.CrouchGait.BoneMask...), Diagnostic: entry.CrouchGait.Diagnostic,
			Locomotion: content.CharacterCrouchLocomotionDef{DefaultClipID: entry.CrouchGait.Locomotion.DefaultClipID, Fallback: entry.CrouchGait.Locomotion.Fallback, BackwardFallback: entry.CrouchGait.Locomotion.BackwardFallback, Directional: hl1CharacterDirectionalClips(entry.CrouchGait.Locomotion.Directional)}},
		WeaponPresentation:    content.CharacterWeaponPresentationDef{Status: entry.WeaponPresentation.Status, UpperBodyMask: append([]string(nil), entry.WeaponPresentation.UpperBodyMask...), Diagnostic: entry.WeaponPresentation.Diagnostic},
		DirectionalLocomotion: content.CharacterDirectionalLocomotionDef{Walk: hl1CharacterDirectionalClips(entry.DirectionalLocomotion.Walk), Run: hl1CharacterDirectionalClips(entry.DirectionalLocomotion.Run), TurnInPlace: hl1CharacterTurnInPlace(entry.DirectionalLocomotion.TurnInPlace), Fallback: entry.DirectionalLocomotion.Fallback, BackwardFallback: entry.DirectionalLocomotion.BackwardFallback},
	}
	for _, stance := range entry.CrouchGait.BaseStances {
		def.CrouchGait.BaseStances = append(def.CrouchGait.BaseStances, content.CharacterStanceClipDef{Stance: stance.Stance, ClipID: stance.ClipID})
	}
	for _, stance := range entry.WeaponPresentation.Stances {
		def.WeaponPresentation.Stances = append(def.WeaponPresentation.Stances, content.CharacterWeaponStanceDef{Stance: stance.Stance, AimClipID: stance.AimClipID, RecoilClipID: stance.RecoilClipID, CrouchAimClipID: stance.CrouchAimClipID, CrouchRecoilClipID: stance.CrouchRecoilClipID, BoneMask: append([]string(nil), stance.BoneMask...), AttachmentMode: stance.AttachmentMode})
	}
	return def
}

func hl1CharacterDirectionalClips(in GameAssetPlayerDirectionalClipSet) content.CharacterDirectionalClipSetDef {
	return content.CharacterDirectionalClipSetDef{Forward: in.Forward, Backward: in.Backward, Left: in.Left, Right: in.Right, ForwardLeft: in.ForwardLeft, ForwardRight: in.ForwardRight, BackwardLeft: in.BackwardLeft, BackwardRight: in.BackwardRight}
}

func hl1CharacterTurnInPlace(in *GameAssetPlayerTurnInPlace) *content.CharacterTurnInPlaceDef {
	if in == nil {
		return nil
	}
	return &content.CharacterTurnInPlaceDef{LeftClipID: in.LeftClipID, RightClipID: in.RightClipID, StartAngleDegrees: in.StartAngleDegrees, StopAngleDegrees: in.StopAngleDegrees, TurnRateDegreesPerSecond: in.TurnRateDegreesPerSecond, PlaybackSpeed: in.PlaybackSpeed}
}

// hl1AddHeldWeaponPresentationMarkers adapts the shared GoldSrc p_ model
// convention into generic gun aim and hand-grip anchors after hand rebasing.
// A left-grip marker is opt-in: its presence is the generic authored signal
// that runtime should solve a two-handed hold.
func hl1AddHeldWeaponPresentationMarkers(asset *content.AssetDef, twoHanded bool) {
	hl1AddHeldWeaponPresentationMarkersWithAimFrame(asset, content.Quat{}, twoHanded)
}

func hl1AddHeldWeaponPresentationMarkersWithAimFrame(asset *content.AssetDef, aimFrame content.Quat, twoHanded bool) {
	if asset == nil || len(asset.Parts) == 0 || asset.Parts[0].Source.VoxelShape == nil {
		return
	}
	voxels := asset.Parts[0].Source.VoxelShape.Voxels
	if len(voxels) == 0 {
		return
	}
	maxX, minY, maxY, minZ, maxZ := voxels[0].X, voxels[0].Y, voxels[0].Y, voxels[0].Z, voxels[0].Z
	for _, voxel := range voxels[1:] {
		maxX = max(maxX, voxel.X)
		minY, maxY = min(minY, voxel.Y), max(maxY, voxel.Y)
		minZ, maxZ = min(minZ, voxel.Z), max(maxZ, voxel.Z)
	}
	resolution := asset.Parts[0].VoxelResolution
	if resolution <= 0 {
		return
	}
	if aimFrame == (content.Quat{}) {
		aimFrame = content.Quat{0, -0.70710677, 0, 0.70710677}
	}
	center := content.Vec3{float32(minY+maxY) * resolution * 0.5, float32(minZ+maxZ) * resolution * 0.5}
	asset.Markers = append(asset.Markers, content.AssetMarkerDef{
		ID: "muzzle", Name: "muzzle", ParentID: asset.Parts[0].ID, Kind: content.AssetMarkerKindMuzzle,
		Transform: content.AssetTransformDef{
			Position: content.Vec3{(float32(maxX) + 0.5) * resolution, center[0], center[1]},
			Rotation: aimFrame,
			Scale:    content.Vec3{1, 1, 1},
		},
		// The p_ convention supplies a deterministic barrel axis, so this is a
		// generic calibrated aim marker rather than an importer-only hint.
		Tags: []string{"source:hl1", "generated:held_weapon_muzzle", "aim:calibrated"},
	}, content.AssetMarkerDef{
		ID: "right_grip", Name: "right_grip", ParentID: asset.Parts[0].ID, Kind: content.AssetMarkerKindRightGrip,
		Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		Tags:      []string{"source:hl1", "generated:held_weapon_grip"},
	})
	if !twoHanded {
		return
	}
	asset.Markers = append(asset.Markers, content.AssetMarkerDef{
		ID: "left_grip", Name: "left_grip", ParentID: asset.Parts[0].ID, Kind: content.AssetMarkerKindLeftGrip,
		Transform: content.AssetTransformDef{Position: content.Vec3{float32(maxX) * resolution * 0.45, center[0], center[1]}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		Tags:      []string{"source:hl1", "generated:held_weapon_grip"},
	})
}

// hl1HeldWeaponUsesLeftGrip contains source-format knowledge only. Unknown
// props default to one hand so importer guesses cannot pull an avatar arm to
// a synthetic target; authors can add a left_grip marker later in the editor.
func hl1HeldWeaponUsesLeftGrip(entry *GameAssetManifestEntry) bool {
	if entry == nil {
		return false
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)))
	switch base {
	case "p_9mmar", "p_shotgun", "p_rpg", "p_crossbow", "p_gauss":
		return true
	default:
		return false
	}
}

func hl1GenericAssetKey(entry GameAssetManifestEntry) string {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)))
	switch entry.CatalogKind {
	case "player":
		if base == "" {
			return ""
		}
		parts := []string{"characters", base}
		for _, model := range entry.BodygroupModels {
			parts = append(parts, fmt.Sprintf("b%d", model))
		}
		return strings.Join(append(parts, fmt.Sprintf("s%d", entry.SkinFamily)), ".")
	case "weapon_world":
		switch base {
		case "w_9mmhandgun":
			return "weapons.handgun"
		case "w_357":
			return "weapons.revolver"
		case "w_9mmar":
			return "weapons.assault_rifle"
		case "w_shotgun":
			return "weapons.shotgun"
		case "w_crowbar":
			return "weapons.crowbar"
		case "w_tripmine":
			return "weapons.tripmine"
		case "w_hgun":
			return "weapons.hivegun"
		case "w_satchel":
			return "weapons.satchel"
		default:
			// Keep non-gameplay world-model variants distinct without exposing
			// their source names to runtime profiles.
			return "weapons.imported." + safeMDLAssetID(strings.TrimSuffix(entry.SourceRef, filepath.Ext(entry.SourceRef)))
		}
	case "weapon_held":
		// The importer catalog identity is authoritative when a source collection
		// has aliases for the same held presentation.
		switch {
		case strings.HasSuffix(strings.ToLower(entry.CatalogID), "_p_crossbow"):
			return "weapons.crossbow.held"
		case strings.HasSuffix(strings.ToLower(entry.CatalogID), "_p_grenade"):
			return "weapons.hand_grenade.held"
		case strings.HasSuffix(strings.ToLower(entry.CatalogID), "_p_gauss"):
			return "weapons.gauss.held"
		}
		switch base {
		case "p_9mmhandgun":
			return "weapons.handgun.held"
		case "p_357":
			return "weapons.revolver.held"
		case "p_crowbar":
			return "weapons.crowbar.held"
		case "p_9mmar":
			return "weapons.assault_rifle.held"
		case "p_shotgun":
			return "weapons.shotgun.held"
		case "p_rpg":
			return "weapons.rpg.held"
		case "p_crossbow":
			return "weapons.crossbow.held"
		case "p_grenade":
			return "weapons.hand_grenade.held"
		case "p_gauss":
			return "weapons.gauss.held"
		case "p_egon":
			return "weapons.egon.held"
		case "p_tripmine":
			return "weapons.tripmine.held"
		case "p_hgun":
			return "weapons.hivegun.held"
		case "p_satchel":
			return "weapons.satchel.held"
		case "p_satchel_radio":
			return "weapons.satchel.radio.held"
		default:
			return "weapons.held.imported." + safeMDLAssetID(strings.TrimSuffix(entry.SourceRef, filepath.Ext(entry.SourceRef)))
		}
	case "npc":
		if id := safeMDLAssetID(strings.TrimSuffix(entry.SourceRef, filepath.Ext(entry.SourceRef))); id != "" {
			key := "models.imported." + id
			if strings.EqualFold(strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)), "hgrunt") {
				suffix := strings.TrimPrefix(entry.CatalogID, id)
				suffix = strings.TrimPrefix(suffix, "_")
				if suffix != "" {
					key += "." + strings.ReplaceAll(suffix, "_", ".")
				}
			}
			return key
		}
	case "static_prop":
		if id := safeMDLAssetID(strings.TrimSuffix(entry.SourceRef, filepath.Ext(entry.SourceRef))); id != "" {
			return "props.imported." + id
		}
	}
	if entry.CatalogKind == "" && entry.GeneratedAssetPath != "" {
		if id := safeMDLAssetID(strings.TrimSuffix(entry.SourceRef, filepath.Ext(entry.SourceRef))); id != "" {
			return entry.Kind + "s.imported." + id
		}
	}
	return ""
}

func hl1AssetImportConfig(entry GameAssetManifestEntry) string {
	profile := ""
	if entry.GeneratedVoxelizationProfile != nil {
		profile = entry.GeneratedVoxelizationProfile.ID
	}
	models := make([]string, len(entry.BodygroupModels))
	for index, model := range entry.BodygroupModels {
		models[index] = strconv.Itoa(model)
	}
	return strings.Join([]string{
		entry.GeneratedVoxelResolutionCategory,
		strconv.FormatFloat(float64(entry.GeneratedVoxelResolution), 'g', -1, 32),
		profile,
		strings.Join(models, ","),
		strconv.Itoa(entry.SkinFamily),
	}, "/")
}

func deterministicHL1ID(kind string, identity ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	for _, value := range identity {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(value))
	}
	return kind + "." + hex.EncodeToString(h.Sum(nil)[:8])
}

type hl1AssetCollector struct {
	gameDir               string
	resourceDirs          []string
	outputRoot            string
	mapName               string
	centralAssets         bool
	voxelResolutionPolicy HL1VoxelResolutionPolicy
	entries               map[string]*GameAssetManifestEntry
	diagnostics           []importcommon.Diagnostic
}

func newHL1AssetCollector(gameDir string, resourceDirs []string, outputRoot, mapName string, centralAssets bool, policy HL1VoxelResolutionPolicy) *hl1AssetCollector {
	if policy == (HL1VoxelResolutionPolicy{}) {
		policy = DefaultHL1VoxelResolutionPolicy()
	}
	return &hl1AssetCollector{
		gameDir:               filepath.Clean(gameDir),
		resourceDirs:          hl1ResourceDirs(gameDir, resourceDirs),
		outputRoot:            filepath.Clean(outputRoot),
		mapName:               mapName,
		centralAssets:         centralAssets,
		voxelResolutionPolicy: policy,
		entries:               map[string]*GameAssetManifestEntry{},
	}
}

func hl1ResourceDirs(gameDir string, overlays []string) []string {
	dirs := make([]string, 0, len(overlays)+1)
	for _, dir := range append([]string{gameDir}, overlays...) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		for _, existing := range dirs {
			if existing == dir {
				dir = ""
				break
			}
		}
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func hl1CatalogResourceDirs(gameDir string, overlays []string) []string {
	gameDir = filepath.Clean(strings.TrimSpace(gameDir))
	var roots []string
	if strings.EqualFold(filepath.Base(gameDir), "valve") || strings.EqualFold(filepath.Base(gameDir), "valve_downloads") {
		roots = append(roots, gameDir)
	} else {
		for _, name := range []string{"valve", "valve_downloads"} {
			path := filepath.Join(gameDir, name)
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				roots = append(roots, path)
			}
		}
		if len(roots) == 0 && gameDir != "." {
			roots = append(roots, gameDir)
		}
	}
	return hl1ResourceDirs("", append(roots, overlays...))
}

func (c *hl1AssetCollector) addAbsolute(kind, path, usedBy string) {
	if strings.TrimSpace(path) == "" {
		return
	}
	c.add(kind, filepath.ToSlash(path), path, usedBy)
}

func (c *hl1AssetCollector) addRef(ref, usedBy string) {
	kind := hl1AssetKindForRef(ref)
	if kind == "" {
		return
	}
	sourcePath := c.resolveRef(ref, kind)
	c.add(kind, ref, sourcePath, usedBy)
}

func (c *hl1AssetCollector) addPickupModelRefs(refs []string, usedBy string) {
	for index, ref := range refs {
		sourcePath := c.resolveRef(ref, "model")
		if sourcePath == "" || !fileExists(sourcePath) {
			continue
		}
		c.add("model", ref, sourcePath, usedBy)
		if index == 0 {
			return
		}
		key := "model:" + strings.ToLower(filepath.ToSlash(ref))
		entry := c.entries[key]
		if entry != nil {
			entry.CompatibilityFallback = true
			if entry.generatedAsset != nil {
				entry.generatedAsset.Tags = appendUniqueString(entry.generatedAsset.Tags, "source:compatibility_fallback")
			}
		}
		return
	}
	if len(refs) > 0 {
		c.addRef(refs[0], usedBy)
	}
}

func (c *hl1AssetCollector) add(kind, sourceRef, sourcePath, usedBy string) {
	c.addWithKey(kind, sourceRef, sourcePath, usedBy, kind+":"+strings.ToLower(filepath.ToSlash(sourceRef)), nil)
}

func (c *hl1AssetCollector) addCatalogModel(kind, sourceRef, sourcePath, catalogID string, bodygroupModels []int, skinFamily int) {
	key := "model:" + strings.ToLower(filepath.ToSlash(sourceRef)) + "#" + catalogID
	c.addWithKey("model", sourceRef, sourcePath, "catalog:"+kind+":"+catalogID, key, func(entry *GameAssetManifestEntry) {
		entry.CatalogKind = kind
		entry.CatalogID = catalogID
		entry.BodygroupModels = append([]int(nil), bodygroupModels...)
		entry.SkinFamily = skinFamily
	})
}

func (c *hl1AssetCollector) addWithKey(kind, sourceRef, sourcePath, usedBy, key string, configure func(*GameAssetManifestEntry)) {
	entry := c.entries[key]
	if entry == nil {
		entry = &GameAssetManifestEntry{
			Kind:         kind,
			SourceRef:    filepath.ToSlash(sourceRef),
			ConvertState: hl1AssetConvertState(kind),
		}
		c.entries[key] = entry
	}
	if configure != nil {
		configure(entry)
	}
	entry.UsedBy = appendUniqueString(entry.UsedBy, usedBy)
	if sourcePath == "" {
		return
	}
	if entry.Resolved {
		return
	}
	entry.SourcePath = filepath.Clean(sourcePath)
	info, err := os.Stat(entry.SourcePath)
	if err != nil {
		return
	}
	if info.IsDir() {
		return
	}
	entry.Resolved = true
	entry.SizeBytes = info.Size()
	entry.SHA256 = fileSHA256(entry.SourcePath)
	if kind == "model" {
		category, voxelResolution := c.voxelResolutionForEntry(entry)
		voxelizationProfile := MDLVoxelizationProfileForCategory(category)
		staticPose := category == HL1VoxelResolutionCategoryPickup || entry.CatalogKind == "static_prop"
		geometryOptions := MDLGeometryOptions{BodygroupModels: entry.BodygroupModels, SkinFamily: entry.SkinFamily, DefaultBodygroups: entry.CatalogKind == "static_prop" || entry.CatalogKind == "npc" || entry.CatalogKind == "weapon_world" || entry.CatalogKind == "weapon_held"}
		geometry, err := LoadMDLGeometryWithOptions(entry.SourcePath, geometryOptions)
		if err != nil {
			c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.mdl_parse_failed",
				Subject:  entry.SourceRef,
				Message:  err.Error(),
			})
		} else {
			entry.ModelInfo = &geometry.Info
			assetName := safeHL1AssetBaseName(entry.SourceRef)
			if entry.CatalogKind != "" {
				assetName = safeHL1CatalogAssetBaseName(entry.CatalogID)
			} else {
				// A map reference and a catalog entry may name the same source MDL.
				// Keep their generated documents distinct: catalog output carries the
				// adapted presentation contract, while the map reference remains a
				// source-faithful static asset.
				assetName = safeHL1CatalogAssetBaseName(entry.SourceRef) + "_uncataloged"
			}
			assetPath := filepath.Join(c.outputRoot, "hl1_assets", c.mapName, "generated", "models", assetName+".gkasset")
			if c.centralAssets {
				assetPath = filepath.Join(c.outputRoot, "hl1", "models", assetName+".gkasset")
			}
			entry.GeneratedVoxelResolution = voxelResolution
			entry.GeneratedVoxelResolutionCategory = string(category)
			entry.GeneratedVoxelizationProfile = &voxelizationProfile
			anchors := map[string]int(nil)
			if entry.CatalogKind == "player" || entry.CatalogKind == "npc" {
				anchors = hl1PlayerSemanticAnchorBones(geometry.Info.Bones)
			}
			if entry.CatalogKind == "player" {
				if !hl1PlayerHasRequiredAnchors(anchors) {
					c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_required_anchor_missing", Subject: entry.CatalogID, Message: "missing verified HL1 player head, hand, or aim-chain bone"})
					entry.ConvertState = "unsupported_player_avatar"
					return
				}
			}
			rebaseToHand := false
			rebaseBone := 0
			if entry.CatalogKind == "weapon_held" {
				anchors = hl1PlayerSemanticAnchorBones(geometry.Info.Bones)
				if hand, ok := anchors["right_hand"]; ok {
					rebaseToHand, rebaseBone = true, hand
				}
			}
			if hl1IsEgonHeldModel(entry) {
				c.buildEgonHeldPresentation(entry, geometry, voxelResolution, voxelizationProfile, assetPath)
				return
			}
			voxelFrameBone := hl1WeaponVoxelFrameBone(entry, geometry.Info.Bones)
			built, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{
				Name:                  strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)),
				SourceRef:             entry.SourceRef,
				VoxelResolution:       voxelResolution,
				VoxelizationProfile:   voxelizationProfile,
				StaticPose:            staticPose,
				RebaseBoneIndex:       rebaseBone,
				RebaseToBone:          rebaseToHand,
				AlignStaticVoxelFrame: voxelFrameBone >= 0,
				VoxelFrameBoneIndex:   voxelFrameBone,
				SemanticAnchors:       anchors,
				LockRootMotion:        entry.CatalogKind == "player",
			})
			if err != nil {
				c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.mdl_voxelize_failed",
					Subject:  entry.SourceRef,
					Message:  err.Error(),
				})
			} else if built.Asset != nil {
				asset, voxelCount, clips := built.Asset, built.VoxelCount, built.Clips
				if entry.CatalogKind == "npc" && strings.EqualFold(strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)), "hgrunt") {
					standing, loadErr := LoadMDLAnimationClips(entry.SourcePath, []string{"standing_mp5", "standing_shotgun"}, false)
					bonesByJoint := map[string]string{}
					if asset.Skeleton == nil {
						loadErr = fmt.Errorf("HGrunt asset has no skeleton")
					} else {
						for _, bone := range asset.Skeleton.Bones {
							bonesByJoint[bone.JointID] = bone.ID
						}
					}
					remapTracks := func(tracks []content.AssetAnimationTrackDef) {
						for trackIndex := range tracks {
							boneID, ok := bonesByJoint[tracks[trackIndex].TargetID]
							if !ok {
								loadErr = fmt.Errorf("standing animation joint %q has no asset bone", tracks[trackIndex].TargetID)
								return
							}
							tracks[trackIndex].TargetID = boneID
						}
					}
					for clipIndex := range standing {
						remapTracks(standing[clipIndex].Tracks)
						if standing[clipIndex].Blend1D != nil {
							for sampleIndex := range standing[clipIndex].Blend1D.Samples {
								remapTracks(standing[clipIndex].Blend1D.Samples[sampleIndex].Tracks)
							}
						}
					}
					if loadErr == nil {
						clips = append(clips, standing...)
					} else {
						c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.hgrunt_standing_animation_missing", Subject: entry.CatalogID, Message: loadErr.Error()})
					}
				}
				entry.GeneratedVoxelResolution = mdlAssetVoxelResolution(asset, voxelResolution)
				if entry.CatalogKind == "weapon_held" {
					twoHanded := hl1HeldWeaponUsesLeftGrip(entry)
					hl1AddHeldWeaponPresentationMarkers(asset, twoHanded)
				}
				if hl1IsTripmineWorldModel(entry) {
					hl1AddTripmineSurfaceMountMarker(asset)
				}
				if entry.CatalogKind == "player" {
					hl1ConfigurePlayerHolsterMarkers(asset)
				}
				if entry.CatalogKind == "player" && !hl1PlayerAssetHasRequiredMarkers(asset) {
					c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_anchor_unresolved", Subject: entry.CatalogID, Message: "verified player anchors could not resolve to generated bone parts"})
					entry.ConvertState = "unsupported_player_avatar"
					return
				}
				generatedAnimations, err := BuildMDLAnimationDocuments(asset, clips, assetPath)
				if err != nil {
					c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.mdl_animation_externalize_failed", Subject: entry.SourceRef, Message: err.Error()})
					entry.ConvertState = "animation_externalize_failed"
					return
				}
				if generatedAnimations.Set != nil {
					entry.generatedAnimations = append(entry.generatedAnimations, generatedAnimations)
				}
				entry.GeneratedAssetPath = filepath.Clean(assetPath)
				entry.GeneratedVoxelCount = voxelCount
				entry.generatedAsset = asset
				entry.ConvertState = "generated_voxel_asset"
				if entry.CatalogKind == "player" {
					entry.HeadMarkerID = "head"
					entry.RightHandMarkerID = "right_hand"
					entry.LeftHandMarkerID = "left_hand"
					entry.UpperBodyMarkerID = "upper_body"
					entry.AimMarkerIDs = append([]string(nil), hl1PlayerAimMarkerIDs...)
					entry.SequenceActivities = hl1PlayerSequenceActivities(clips)
					entry.CrouchGait = hl1PlayerCrouchGait(asset, clips)
					if entry.CrouchGait.Status == GameAssetPlayerCrouchGaitUnsupported {
						c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_crouch_gait_unsupported", Subject: entry.CatalogID, Message: entry.CrouchGait.Diagnostic})
					}
					entry.WeaponPresentation = hl1PlayerWeaponPresentation(asset, clips)
					if entry.WeaponPresentation.Status == GameAssetPlayerWeaponPresentationUnsupported {
						c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_weapon_presentation_unsupported", Subject: entry.CatalogID, Message: entry.WeaponPresentation.Diagnostic})
					}
					entry.DirectionalLocomotion = hl1PlayerDirectionalLocomotion(clips)
				}
			}
		}
	} else if kind == "sprite" {
		category, voxelResolution := c.voxelResolutionForEntry(entry)
		geometry, err := LoadSPRGeometry(entry.SourcePath)
		if err != nil {
			c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.spr_parse_failed",
				Subject:  entry.SourceRef,
				Message:  err.Error(),
			})
		} else {
			entry.SpriteInfo = &geometry.Info
			assetName := safeHL1CatalogAssetBaseName(entry.SourceRef) + ".gkasset"
			assetPath := filepath.Join(c.outputRoot, "hl1_assets", c.mapName, "generated", "sprites", safeHL1AssetBaseName(entry.SourceRef)+".gkasset")
			if c.centralAssets {
				assetPath = filepath.Join(c.outputRoot, "hl1", "sprites", assetName)
			}
			asset, voxelCount, err := BuildSPRVoxelAsset(geometry, SPRVoxelAssetOptions{
				Name:            strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)),
				SourceRef:       entry.SourceRef,
				VoxelResolution: voxelResolution,
			})
			if err != nil {
				c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.spr_voxelize_failed",
					Subject:  entry.SourceRef,
					Message:  err.Error(),
				})
			} else if asset != nil {
				entry.GeneratedAssetPath = filepath.Clean(assetPath)
				entry.GeneratedVoxelCount = voxelCount
				entry.GeneratedVoxelResolution = voxelResolution
				entry.GeneratedVoxelResolutionCategory = string(category)
				entry.generatedAsset = asset
				entry.ConvertState = "generated_voxel_asset"
			}
		}
	}
}

func hl1WeaponVoxelFrameBone(entry *GameAssetManifestEntry, bones []MDLBoneInfo) int {
	if entry == nil || len(bones) == 0 {
		return -1
	}
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)))
	switch {
	case entry.CatalogKind == "weapon_world" || strings.HasPrefix(base, "w_"):
		return 0
	case entry.CatalogKind == "weapon_held" || strings.HasPrefix(base, "p_"):
		for index := len(bones) - 1; index >= 0; index-- {
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(bones[index].Name)), "bip01") {
				return index
			}
		}
	}
	return -1
}

func hl1IsEgonHeldModel(entry *GameAssetManifestEntry) bool {
	if entry == nil || entry.CatalogKind != "weapon_held" {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef))
	return strings.EqualFold(base, "p_egon")
}

func hl1IsTripmineWorldModel(entry *GameAssetManifestEntry) bool {
	if entry == nil || entry.CatalogKind != "weapon_world" {
		return false
	}
	base := strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef))
	return strings.EqualFold(base, "w_tripmine")
}

func hl1AddTripmineSurfaceMountMarker(asset *content.AssetDef) {
	if asset == nil {
		return
	}
	asset.Markers = append(asset.Markers, content.AssetMarkerDef{
		ID: "surface_mount", Name: "surface mount", ParentID: "mdl_surface", Kind: content.AssetMarkerKindSurfaceMount,
		Transform: content.AssetTransformDef{Rotation: content.Quat{0, 1, 0, 0}, Scale: content.Vec3{1, 1, 1}},
		Tags:      []string{"source:hl1", "generated:surface_mount"},
	})
}

func (c *hl1AssetCollector) buildEgonHeldPresentation(entry *GameAssetManifestEntry, geometry MDLGeometry, resolution float32, profile MDLVoxelizationProfile, heldPath string) {
	bone := func(name string) int {
		for index, info := range geometry.Info.Bones {
			if strings.EqualFold(strings.ReplaceAll(info.Name, " ", ""), name) {
				return index
			}
		}
		return -1
	}
	upperBody, backpack, neck := bone("Bip01Spine"), bone("Bip01Spine1"), bone("Bip01Neck")
	forearm, terminalArm := bone("Bip01RArm1"), bone("Bip01RArm2")
	if upperBody < 0 || backpack < 0 || neck < 0 || forearm < 0 || terminalArm < 0 {
		c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.egon_presentation_unsupported", Subject: entry.SourceRef, Message: "missing verified Egon backpack or terminal-arm bones"})
		return
	}
	heldBuilt, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{
		Name:                "egon held",
		SourceRef:           entry.SourceRef,
		VoxelResolution:     resolution,
		VoxelizationProfile: profile,
		StaticPose:          true,
		RebaseBoneIndex:     terminalArm,
		RebaseToBone:        true,
		IncludeBoneIndices:  []int{forearm, terminalArm},
	})
	if err != nil {
		c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.egon_held_voxelize_failed", Subject: entry.SourceRef, Message: err.Error()})
		return
	}
	held, heldVoxels := heldBuilt.Asset, heldBuilt.VoxelCount
	packBuilt, err := BuildMDLVoxelAssetDocuments(geometry, MDLVoxelAssetOptions{
		Name:                "egon backpack",
		SourceRef:           entry.SourceRef,
		VoxelResolution:     resolution,
		VoxelizationProfile: profile,
		StaticPose:          true,
		RebaseBoneIndex:     upperBody,
		RebaseToBone:        true,
		IncludeBoneIndices:  []int{backpack, neck},
	})
	if err != nil {
		c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.egon_backpack_voxelize_failed", Subject: entry.SourceRef, Message: err.Error()})
		return
	}
	pack, packVoxels := packBuilt.Asset, packBuilt.VoxelCount
	hl1AddHeldWeaponPresentationMarkersWithAimFrame(held, hl1BoneLocalMuzzleFrame(geometry.Info.Bones, terminalArm), true)
	entry.GeneratedAssetPath = filepath.Clean(heldPath)
	entry.GeneratedVoxelCount = heldVoxels
	entry.GeneratedVoxelResolution = mdlAssetVoxelResolution(held, resolution)
	entry.generatedAsset = held
	entry.GeneratedExtras = []GameAssetGeneratedExtra{{
		Key:        "equipment.egon.backpack",
		AssetPath:  strings.TrimSuffix(heldPath, filepath.Ext(heldPath)) + "_backpack.gkasset",
		VoxelCount: packVoxels,
		asset:      pack,
	}}
	entry.ConvertState = "generated_composite_presentation"
}

func mdlAssetVoxelResolution(asset *content.AssetDef, fallback float32) float32 {
	if asset != nil {
		for _, part := range asset.Parts {
			if part.Source.Kind == content.AssetSourceKindVoxelShape && part.VoxelResolution > 0 {
				return part.VoxelResolution
			}
		}
	}
	return fallback
}

func assetHasMarker(asset *content.AssetDef, markerID string) bool {
	if asset == nil {
		return false
	}
	for _, marker := range asset.Markers {
		if marker.ID == markerID {
			return true
		}
	}
	return false
}

func (c *hl1AssetCollector) voxelResolutionForEntry(entry *GameAssetManifestEntry) (HL1VoxelResolutionCategory, float32) {
	category := hl1VoxelResolutionCategoryForGameAssetEntry(entry)
	return category, c.voxelResolutionPolicy.Resolution(category)
}

func hl1VoxelResolutionCategoryForGameAssetEntry(entry *GameAssetManifestEntry) HL1VoxelResolutionCategory {
	if entry != nil {
		for _, usedBy := range entry.UsedBy {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "pickup:") {
				return HL1VoxelResolutionCategoryPickup
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "catalog:weapon_world:") {
				return HL1VoxelResolutionCategoryPickup
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "catalog:weapon_held:") {
				return HL1VoxelResolutionCategoryPickup
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "npc:") {
				return HL1VoxelResolutionCategoryNPC
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "catalog:player:") {
				return HL1VoxelResolutionCategoryNPC
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "catalog:npc:") {
				return HL1VoxelResolutionCategoryNPC
			}
		}
	}
	return HL1VoxelResolutionCategoryStaticProp
}

func (c *hl1AssetCollector) resolveRef(ref, kind string) string {
	cleaned := filepath.Clean(filepath.FromSlash(strings.TrimSpace(ref)))
	if cleaned == "." || cleaned == "" || strings.HasPrefix(cleaned, "*") {
		return ""
	}
	var candidates []string
	for _, resourceDir := range c.resourceDirs {
		candidates = append(candidates, hl1AssetPathCandidates(resourceDir, cleaned, kind)...)
	}
	candidates = uniqueCleanPaths(candidates)
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

func (c *hl1AssetCollector) buildEntries() ([]GameAssetManifestEntry, []importcommon.Diagnostic) {
	out := make([]GameAssetManifestEntry, 0, len(c.entries))
	diagnostics := append([]importcommon.Diagnostic(nil), c.diagnostics...)
	for _, entry := range c.entries {
		sort.Strings(entry.UsedBy)
		out = append(out, *entry)
		if !entry.Resolved {
			diagnostics = append(diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.asset_missing",
				Subject:  entry.SourceRef,
				Message:  "referenced HL1 asset file was not found",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].SourceRef != out[j].SourceRef {
			return out[i].SourceRef < out[j].SourceRef
		}
		return out[i].CatalogID < out[j].CatalogID
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		return diagnostics[i].Subject < diagnostics[j].Subject
	})
	return out, diagnostics
}

var hl1PlayerAimMarkerIDs = []string{"aim_spine", "aim_spine1", "aim_spine2", "aim_spine3", "aim_neck", "head"}

func hl1PlayerSemanticAnchorBones(bones []MDLBoneInfo) map[string]int {
	anchors := map[string]int{"head": -1, "right_hand": -1, "left_hand": -1, "upper_body": -1, "aim_spine": -1, "aim_spine1": -1, "aim_spine2": -1, "aim_spine3": -1, "aim_neck": -1, "holster_back": -1, "holster_hip": -1}
	for index, bone := range bones {
		switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(bone.Name), " ", "")) {
		case "bip01pelvis":
			anchors["holster_hip"] = index
		case "bip01head":
			anchors["head"] = index
		case "bip01rhand":
			anchors["right_hand"] = index
		case "bip01lhand":
			anchors["left_hand"] = index
		case "bip01spine":
			anchors["upper_body"] = index
			anchors["aim_spine"] = index
		case "bip01spine1":
			anchors["aim_spine1"] = index
		case "bip01spine2":
			anchors["aim_spine2"] = index
		case "bip01spine3":
			anchors["aim_spine3"] = index
			anchors["holster_back"] = index
		case "bip01neck":
			anchors["aim_neck"] = index
		}
	}
	return anchors
}

func hl1ConfigurePlayerHolsterMarkers(asset *content.AssetDef) {
	if asset == nil {
		return
	}
	for i := range asset.Markers {
		marker := &asset.Markers[i]
		switch marker.ID {
		case "holster_back":
			marker.Kind = content.AssetMarkerKindWeaponSlot
			marker.Name = "back holster"
			marker.Transform = content.AssetTransformDef{Position: content.Vec3{0, -0.25, 0.24}, Rotation: content.Quat{0, 0, 0.9238795, 0.38268343}, Scale: content.Vec3{1, 1, 1}}
		case "holster_hip":
			marker.Kind = content.AssetMarkerKindWeaponSlot
			marker.Name = "hip holster"
			marker.Transform = content.AssetTransformDef{Position: content.Vec3{-0.18, -0.10, 0}, Rotation: content.Quat{-0.70710677, 0, 0, 0}, Scale: content.Vec3{1, 1, 1}}
		}
	}
}

func hl1PlayerHasRequiredAnchors(anchors map[string]int) bool {
	if anchors["right_hand"] < 0 || anchors["left_hand"] < 0 || anchors["upper_body"] < 0 {
		return false
	}
	for _, markerID := range hl1PlayerAimMarkerIDs {
		if anchors[markerID] < 0 {
			return false
		}
	}
	return true
}

func hl1PlayerAssetHasRequiredMarkers(asset *content.AssetDef) bool {
	if !assetHasMarker(asset, "right_hand") || !assetHasMarker(asset, "left_hand") || !assetHasMarker(asset, "upper_body") {
		return false
	}
	for _, markerID := range hl1PlayerAimMarkerIDs {
		if !assetHasMarker(asset, markerID) {
			return false
		}
	}
	return true
}

func hl1PlayerDirectionalLocomotion(clips []content.AssetAnimationClipDef) GameAssetPlayerDirectionalLocomotion {
	locomotion := GameAssetPlayerDirectionalLocomotion{Fallback: GameAssetPlayerLocomotionFallbackUnsupported}
	for _, clip := range clips {
		// ponytail: only Crossfire-verified GoldSrc names become locomotion;
		// extend after another player family is probed, never guess clip meaning.
		switch strings.ToLower(strings.TrimSpace(clip.Name)) {
		case "walk":
			locomotion.Walk.Forward = clip.ID
		case "run":
			locomotion.Run.Forward = clip.ID
		}
	}
	if locomotion.Walk.Forward != "" || locomotion.Run.Forward != "" {
		locomotion.Fallback = GameAssetPlayerLocomotionFallbackFaceTravel
		locomotion.BackwardFallback = GameAssetPlayerBackwardFallbackReverseForward
	}
	return locomotion
}

func hl1PlayerSequenceActivities(clips []content.AssetAnimationClipDef) []GameAssetPlayerSequenceActivity {
	activities := make([]GameAssetPlayerSequenceActivity, 0, len(clips))
	for _, clip := range clips {
		for _, tag := range clip.Tags {
			const prefix = "source:hl1_activity:"
			if !strings.HasPrefix(tag, prefix) {
				continue
			}
			activity, err := strconv.Atoi(strings.TrimPrefix(tag, prefix))
			if err == nil {
				activities = append(activities, GameAssetPlayerSequenceActivity{ClipID: clip.ID, Activity: activity})
			}
			break
		}
	}
	sort.Slice(activities, func(i, j int) bool { return activities[i].ClipID < activities[j].ClipID })
	return activities
}

func hl1PlayerCrouchGait(asset *content.AssetDef, clips []content.AssetAnimationClipDef) GameAssetPlayerCrouchGait {
	gait := GameAssetPlayerCrouchGait{Status: GameAssetPlayerCrouchGaitUnsupported}
	if asset == nil {
		gait.Diagnostic = "generated player asset is missing"
		return gait
	}
	activities := hl1PlayerSequenceActivities(clips)
	for _, activity := range activities {
		switch activity.Activity {
		case HL1ActivityCrouch:
			if gait.CrouchClipID == "" {
				gait.CrouchClipID = activity.ClipID
			}
		case HL1ActivityCrouchIdle:
			if gait.CrouchIdleClipID == "" {
				gait.CrouchIdleClipID = activity.ClipID
			}
		}
	}
	for _, clip := range clips {
		if stance := hl1PlayerCrouchAimStance(clip.Name); stance != "" {
			gait.BaseStances = append(gait.BaseStances, GameAssetPlayerStanceClip{Stance: stance, ClipID: clip.ID})
		}
	}
	if gait.CrouchClipID != "" {
		gait.Locomotion = GameAssetPlayerCrouchLocomotion{
			DefaultClipID:    gait.CrouchClipID,
			Fallback:         GameAssetPlayerLocomotionFallbackFaceTravel,
			BackwardFallback: GameAssetPlayerBackwardFallbackReverseForward,
		}
	}
	sort.Slice(gait.BaseStances, func(i, j int) bool { return gait.BaseStances[i].Stance < gait.BaseStances[j].Stance })
	gait.BoneMask = hl1PlayerLowerBodyBoneMask(asset.Skeleton)
	if gait.CrouchClipID == "" || gait.CrouchIdleClipID == "" || gait.Locomotion.DefaultClipID == "" || gait.Locomotion.Fallback == "" || gait.Locomotion.BackwardFallback == "" || len(gait.BaseStances) == 0 || len(gait.BoneMask) == 0 {
		gait.Diagnostic = "requires ACT_CROUCH, ACT_CROUCHIDLE, explicit crouch locomotion and backward fallbacks, a verified crouch_aim stance, and the standard lower-body bone mask"
		return gait
	}
	gait.Status = GameAssetPlayerCrouchGaitSupported
	return gait
}

func hl1PlayerCrouchAimStance(name string) string {
	// GoldSrc multiplayer's documented stance names; this importer allowlist is
	// deliberately narrower than a runtime fuzzy-name match.
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "crouch_aim_crowbar":
		return "crowbar"
	case "crouch_aim_trip":
		return "trip"
	case "crouch_aim_onehanded":
		return "onehanded"
	case "crouch_aim_python":
		return "python"
	case "crouch_aim_shotgun":
		return "shotgun"
	case "crouch_aim_gauss":
		return "gauss"
	case "crouch_aim_mp5":
		return "mp5"
	case "crouch_aim_rpg":
		return "rpg"
	case "crouch_aim_egon":
		return "egon"
	case "crouch_aim_squeak":
		return "squeak"
	case "crouch_aim_hive":
		return "hive"
	case "crouch_aim_bow":
		return "bow"
	default:
		return ""
	}
}

func hl1PlayerWeaponPresentation(asset *content.AssetDef, clips []content.AssetAnimationClipDef) GameAssetPlayerWeaponPresentation {
	presentation := GameAssetPlayerWeaponPresentation{Status: GameAssetPlayerWeaponPresentationUnsupported}
	if asset == nil {
		presentation.Diagnostic = "generated player asset is missing"
		return presentation
	}
	byName := make(map[string]string, len(clips))
	for _, clip := range clips {
		byName[strings.ToLower(strings.TrimSpace(clip.Name))] = clip.ID
	}
	for _, stance := range hl1PlayerWeaponStances {
		entry := GameAssetPlayerWeaponStance{
			Stance:             stance,
			AimClipID:          byName["ref_aim_"+stance],
			RecoilClipID:       byName["ref_shoot_"+stance],
			CrouchAimClipID:    byName["crouch_aim_"+stance],
			CrouchRecoilClipID: byName["crouch_shoot_"+stance],
		}
		entry.BoneMask = hl1PlayerArmBoneMask(asset.Skeleton)
		if stance == "crowbar" {
			entry.AttachmentMode = content.CharacterWeaponAttachmentMount
		}
		if entry.AimClipID != "" && entry.RecoilClipID != "" && entry.CrouchAimClipID != "" && entry.CrouchRecoilClipID != "" {
			presentation.Stances = append(presentation.Stances, entry)
		}
	}
	presentation.UpperBodyMask = hl1PlayerUpperBodyBoneMask(asset.Skeleton)
	if len(presentation.Stances) == 0 || len(presentation.UpperBodyMask) == 0 {
		presentation.Diagnostic = "requires allowlisted ref_aim/ref_shoot/crouch_aim/crouch_shoot stance clips and the standard upper-body bone mask"
		return presentation
	}
	presentation.Status = GameAssetPlayerWeaponPresentationSupported
	return presentation
}

var hl1PlayerWeaponStances = []string{"crowbar", "trip", "onehanded", "python", "shotgun", "gauss", "mp5", "rpg", "egon", "squeak", "hive", "bow"}

func hl1PlayerUpperBodyBoneMask(skeleton *content.AssetSkeletonDef) []string {
	return hl1PlayerWeaponBoneMask(skeleton, true)
}

func hl1PlayerArmBoneMask(skeleton *content.AssetSkeletonDef) []string {
	return hl1PlayerWeaponBoneMask(skeleton, false)
}

func hl1PlayerWeaponBoneMask(skeleton *content.AssetSkeletonDef, includeTorso bool) []string {
	if skeleton == nil {
		return nil
	}
	mask := make([]string, 0, len(skeleton.Bones))
	for _, bone := range skeleton.Bones {
		name := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(bone.Name), " ", ""))
		arm := strings.HasPrefix(name, "bip01lclavicle") || strings.HasPrefix(name, "bip01rclavicle") || strings.HasPrefix(name, "bip01lupperarm") || strings.HasPrefix(name, "bip01rupperarm") || strings.HasPrefix(name, "bip01lforearm") || strings.HasPrefix(name, "bip01rforearm") || strings.HasPrefix(name, "bip01larm") || strings.HasPrefix(name, "bip01rarm") || strings.HasPrefix(name, "bip01lhand") || strings.HasPrefix(name, "bip01rhand") || strings.HasPrefix(name, "bip01lfinger") || strings.HasPrefix(name, "bip01rfinger")
		torso := strings.HasPrefix(name, "bip01spine") || name == "bip01neck" || name == "bip01head"
		if arm || includeTorso && torso {
			mask = append(mask, bone.JointID)
		}
	}
	sort.Strings(mask)
	return mask
}

func hl1PlayerLowerBodyBoneMask(skeleton *content.AssetSkeletonDef) []string {
	if skeleton == nil {
		return nil
	}
	known := map[string]struct{}{
		"bip01pelvis": {}, "bip01lleg": {}, "bip01lleg1": {}, "bip01lfoot": {},
		"bip01rleg": {}, "bip01rleg1": {}, "bip01rfoot": {},
	}
	mask := make([]string, 0, len(known))
	for _, bone := range skeleton.Bones {
		name := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(bone.Name), " ", ""))
		if _, ok := known[name]; ok {
			mask = append(mask, bone.JointID)
		}
	}
	sort.Strings(mask)
	return mask
}

func buildGameAssetCatalog(entries []GameAssetManifestEntry) *GameAssetCatalog {
	catalog := &GameAssetCatalog{}
	for _, entry := range entries {
		if entry.GeneratedAssetPath == "" || entry.CatalogID == "" {
			continue
		}
		switch entry.CatalogKind {
		case "player":
			if entry.HeadMarkerID == "" || entry.RightHandMarkerID == "" || entry.LeftHandMarkerID == "" || entry.UpperBodyMarkerID == "" || len(entry.AimMarkerIDs) == 0 {
				continue
			}
			catalog.Players = append(catalog.Players, GameAssetPlayerCatalogEntry{
				ID:                    entry.CatalogID,
				SourceRef:             entry.SourceRef,
				AssetPath:             entry.GeneratedAssetPath,
				BodygroupModels:       append([]int(nil), entry.BodygroupModels...),
				SkinFamily:            entry.SkinFamily,
				HeadMarkerID:          entry.HeadMarkerID,
				RightHandMarkerID:     entry.RightHandMarkerID,
				LeftHandMarkerID:      entry.LeftHandMarkerID,
				UpperBodyMarkerID:     entry.UpperBodyMarkerID,
				AimMarkerIDs:          append([]string(nil), entry.AimMarkerIDs...),
				SequenceActivities:    append([]GameAssetPlayerSequenceActivity(nil), entry.SequenceActivities...),
				CrouchGait:            entry.CrouchGait,
				WeaponPresentation:    entry.WeaponPresentation,
				DirectionalLocomotion: entry.DirectionalLocomotion,
			})
		case "weapon_world":
			catalog.WeaponWorldModels = append(catalog.WeaponWorldModels, GameAssetWeaponCatalogEntry{
				ID:        entry.CatalogID,
				SourceRef: entry.SourceRef,
				AssetPath: entry.GeneratedAssetPath,
			})
		}
	}
	sort.Slice(catalog.Players, func(i, j int) bool { return catalog.Players[i].ID < catalog.Players[j].ID })
	sort.Slice(catalog.WeaponWorldModels, func(i, j int) bool { return catalog.WeaponWorldModels[i].ID < catalog.WeaponWorldModels[j].ID })
	if len(catalog.Players) == 0 && len(catalog.WeaponWorldModels) == 0 {
		return nil
	}
	return catalog
}

func extractHL1AssetRefs(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\r', '\n', ';', ',':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.Trim(field, "\"'")
		if field == "" || strings.HasPrefix(field, "*") {
			continue
		}
		if hl1AssetKindForRef(field) == "" {
			continue
		}
		out = append(out, field)
	}
	return out
}

func hl1AssetKindForRef(ref string) string {
	ext := strings.ToLower(filepath.Ext(ref))
	switch ext {
	case ".wad":
		return "wad"
	case ".mdl":
		return "model"
	case ".spr":
		return "sprite"
	case ".wav":
		return "sound"
	default:
		return ""
	}
}

func hl1AssetConvertState(kind string) string {
	switch kind {
	case "wad":
		return "used_for_texture_bake"
	case "model", "sprite":
		return "cataloged_source_only"
	case "sound":
		return "cataloged_source_only"
	default:
		return "unknown"
	}
}

func hl1AssetPathCandidates(gameDir, ref, kind string) []string {
	var out []string
	ref = filepath.Clean(ref)
	if filepath.IsAbs(ref) {
		out = append(out, ref)
		ref = strings.TrimLeft(ref, string(filepath.Separator))
	}
	if gameDir != "" {
		out = append(out, filepath.Join(gameDir, ref))
		if strings.HasPrefix(strings.ToLower(ref), "valve"+string(filepath.Separator)) {
			out = append(out, filepath.Join(gameDir, ref))
		} else {
			out = append(out, filepath.Join(gameDir, "valve", ref))
			switch kind {
			case "model":
				out = append(out, filepath.Join(gameDir, "valve", "models", ref))
			case "sprite":
				out = append(out, filepath.Join(gameDir, "valve", "sprites", ref))
			case "sound":
				out = append(out, filepath.Join(gameDir, "valve", "sound", ref))
			case "wad":
				out = append(out, filepath.Join(gameDir, "valve", filepath.Base(ref)))
			}
		}
	}
	return uniqueCleanPaths(out)
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func uniqueCleanPaths(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		cleaned := filepath.Clean(path)
		key := strings.ToLower(cleaned)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return ""
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func safeHL1AssetBaseName(sourceRef string) string {
	base := strings.TrimSuffix(filepath.Base(sourceRef), filepath.Ext(sourceRef))
	if base == "" || base == "." {
		base = "asset"
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "asset"
	}
	return b.String()
}

func safeHL1CatalogAssetBaseName(sourceRef string) string {
	ref := strings.TrimSuffix(filepath.ToSlash(sourceRef), filepath.Ext(sourceRef))
	return safeHL1AssetBaseName(strings.ReplaceAll(ref, "/", "_"))
}
