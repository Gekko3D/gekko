package hl1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

const GameAssetManifestSchemaVersion = 1

type GameAssetImportResult struct {
	ManifestPath string
	Manifest     *GameAssetManifest
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
	UpperBodyMarkerID     string                               `json:"upper_body_marker_id"`
	AimMarkerIDs          []string                             `json:"aim_marker_ids,omitempty"`
	ClipIDs               []string                             `json:"clip_ids,omitempty"`
	DirectionalLocomotion GameAssetPlayerDirectionalLocomotion `json:"directional_locomotion"`
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
	Fallback         string                            `json:"fallback"`
	BackwardFallback string                            `json:"backward_fallback,omitempty"`
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
	OutputPath                       string                               `json:"output_path,omitempty"`
	GeneratedAssetPath               string                               `json:"generated_asset_path,omitempty"`
	GeneratedVoxelCount              int                                  `json:"generated_voxel_count,omitempty"`
	GeneratedVoxelResolution         float32                              `json:"generated_voxel_resolution,omitempty"`
	GeneratedVoxelResolutionCategory string                               `json:"generated_voxel_resolution_category,omitempty"`
	CompatibilityFallback            bool                                 `json:"compatibility_fallback,omitempty"`
	CatalogKind                      string                               `json:"catalog_kind,omitempty"`
	CatalogID                        string                               `json:"catalog_id,omitempty"`
	BodygroupModels                  []int                                `json:"bodygroup_models,omitempty"`
	SkinFamily                       int                                  `json:"skin_family,omitempty"`
	HeadMarkerID                     string                               `json:"head_marker_id,omitempty"`
	RightHandMarkerID                string                               `json:"right_hand_marker_id,omitempty"`
	UpperBodyMarkerID                string                               `json:"upper_body_marker_id,omitempty"`
	AimMarkerIDs                     []string                             `json:"aim_marker_ids,omitempty"`
	ClipIDs                          []string                             `json:"clip_ids,omitempty"`
	DirectionalLocomotion            GameAssetPlayerDirectionalLocomotion `json:"directional_locomotion,omitempty"`
	SizeBytes                        int64                                `json:"size_bytes,omitempty"`
	SHA256                           string                               `json:"sha256,omitempty"`
	Resolved                         bool                                 `json:"resolved"`
	UsedBy                           []string                             `json:"used_by,omitempty"`
	ConvertState                     string                               `json:"convert_state,omitempty"`
	ModelInfo                        *MDLInfo                             `json:"model_info,omitempty"`
	SpriteInfo                       *SPRInfo                             `json:"sprite_info,omitempty"`
	generatedAsset                   *content.AssetDef                    `json:"-"`
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
	gameDir := strings.TrimSpace(opts.GameDir)
	if gameDir == "" {
		gameDir = strings.TrimSpace(summary.Report.Source.GameDir)
	}
	if gameDir == "" {
		gameDir = InferGameDirFromBSPPath(summary.Report.Source.BSPPath)
	}
	manifestPath := filepath.Join(outputRoot, "hl1_assets", mapName, "manifest.gkhl1assets")
	manifest := &GameAssetManifest{
		SchemaVersion: GameAssetManifestSchemaVersion,
		Source:        summary.Report.Source,
	}
	manifest.Source.GameDir = gameDir
	collector := newHL1AssetCollector(gameDir, outputRoot, mapName, EffectiveHL1VoxelResolutionPolicy(opts))
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
		collector.addCatalogModels("player", hl1CatalogModelPaths(gameDir, true))
	}
	if opts.ImportAllWeaponWorldModels {
		collector.addCatalogModels("weapon_world", hl1CatalogModelPaths(gameDir, false))
	}
	manifest.Assets, manifest.Diagnostics = collector.buildEntries()
	manifest.Catalog = buildGameAssetCatalog(manifest.Assets)
	return GameAssetImportResult{ManifestPath: manifestPath, Manifest: manifest}, nil
}

func (c *hl1AssetCollector) addCatalogModels(kind string, paths []string) {
	for _, path := range paths {
		ref, err := filepath.Rel(c.gameDir, path)
		if err != nil || strings.HasPrefix(ref, "..") {
			continue
		}
		ref = filepath.ToSlash(ref)
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
	sort.Strings(out)
	return out
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
	for i := range result.Manifest.Assets {
		entry := &result.Manifest.Assets[i]
		if !entry.Resolved || entry.SourcePath == "" || entry.OutputPath == "" {
			continue
		}
		if err := copyHL1AssetFile(entry.SourcePath, entry.OutputPath); err != nil {
			result.Manifest.Diagnostics = append(result.Manifest.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.asset_copy_failed",
				Subject:  entry.SourcePath,
				Message:  err.Error(),
			})
		}
		if entry.generatedAsset != nil && entry.GeneratedAssetPath != "" {
			if err := os.MkdirAll(filepath.Dir(entry.GeneratedAssetPath), 0755); err != nil {
				result.Manifest.Diagnostics = append(result.Manifest.Diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.generated_asset_save_failed",
					Subject:  entry.GeneratedAssetPath,
					Message:  err.Error(),
				})
			} else if err := content.SaveAsset(entry.GeneratedAssetPath, entry.generatedAsset); err != nil {
				result.Manifest.Diagnostics = append(result.Manifest.Diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.generated_asset_save_failed",
					Subject:  entry.GeneratedAssetPath,
					Message:  err.Error(),
				})
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(result.ManifestPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result.Manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(result.ManifestPath, data, 0644)
}

type hl1AssetCollector struct {
	gameDir               string
	outputRoot            string
	mapName               string
	voxelResolutionPolicy HL1VoxelResolutionPolicy
	entries               map[string]*GameAssetManifestEntry
	diagnostics           []importcommon.Diagnostic
}

func newHL1AssetCollector(gameDir, outputRoot, mapName string, policy HL1VoxelResolutionPolicy) *hl1AssetCollector {
	if policy == (HL1VoxelResolutionPolicy{}) {
		policy = DefaultHL1VoxelResolutionPolicy()
	}
	return &hl1AssetCollector{
		gameDir:               filepath.Clean(gameDir),
		outputRoot:            filepath.Clean(outputRoot),
		mapName:               mapName,
		voxelResolutionPolicy: policy,
		entries:               map[string]*GameAssetManifestEntry{},
	}
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
	entry.OutputPath = filepath.Join(c.outputRoot, "hl1_assets", c.mapName, "files", hl1AssetOutputRelPath(entry.SourcePath, c.gameDir, kind, entry.SourceRef))
	if kind == "model" {
		category, voxelResolution := c.voxelResolutionForEntry(entry)
		staticPose := category == HL1VoxelResolutionCategoryPickup
		geometryOptions := MDLGeometryOptions{BodygroupModels: entry.BodygroupModels, SkinFamily: entry.SkinFamily}
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
			}
			assetPath := filepath.Join(c.outputRoot, "hl1_assets", c.mapName, "generated", "models", assetName+".gkasset")
			entry.GeneratedVoxelResolution = voxelResolution
			entry.GeneratedVoxelResolutionCategory = string(category)
			anchors := map[string]int(nil)
			if entry.CatalogKind == "player" {
				anchors = hl1PlayerSemanticAnchorBones(geometry.Info.Bones)
				if !hl1PlayerHasRequiredAnchors(anchors) {
					c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_required_anchor_missing", Subject: entry.CatalogID, Message: "missing verified HL1 player head, hand, or aim-chain bone"})
					entry.ConvertState = "unsupported_player_avatar"
					return
				}
			}
			asset, voxelCount, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{
				Name:            strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)),
				SourceRef:       entry.SourceRef,
				VoxelResolution: voxelResolution,
				StaticPose:      staticPose,
				SemanticAnchors: anchors,
				LockRootMotion:  entry.CatalogKind == "player",
			})
			if err != nil {
				c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.mdl_voxelize_failed",
					Subject:  entry.SourceRef,
					Message:  err.Error(),
				})
			} else if asset != nil {
				if entry.CatalogKind == "player" && !hl1PlayerAssetHasRequiredMarkers(asset) {
					c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{Severity: importcommon.SeverityWarning, Code: "hl1.player_anchor_unresolved", Subject: entry.CatalogID, Message: "verified player anchors could not resolve to generated bone parts"})
					entry.ConvertState = "unsupported_player_avatar"
					return
				}
				entry.GeneratedAssetPath = filepath.Clean(assetPath)
				entry.GeneratedVoxelCount = voxelCount
				entry.generatedAsset = asset
				entry.ConvertState = "generated_voxel_asset"
				for _, clip := range asset.AnimationClips {
					entry.ClipIDs = append(entry.ClipIDs, clip.ID)
				}
				if entry.CatalogKind == "player" {
					entry.HeadMarkerID = "head"
					entry.RightHandMarkerID = "right_hand"
					entry.UpperBodyMarkerID = "upper_body"
					entry.AimMarkerIDs = append([]string(nil), hl1PlayerAimMarkerIDs...)
					entry.DirectionalLocomotion = hl1PlayerDirectionalLocomotion(asset.AnimationClips)
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
			assetPath := filepath.Join(c.outputRoot, "hl1_assets", c.mapName, "generated", "sprites", safeHL1AssetBaseName(entry.SourceRef)+".gkasset")
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
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "npc:") {
				return HL1VoxelResolutionCategoryNPC
			}
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(usedBy)), "catalog:player:") {
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
	candidates := hl1AssetPathCandidates(c.gameDir, cleaned, kind)
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
	anchors := map[string]int{"head": -1, "right_hand": -1, "upper_body": -1, "aim_spine": -1, "aim_spine1": -1, "aim_spine2": -1, "aim_spine3": -1, "aim_neck": -1}
	for index, bone := range bones {
		switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(bone.Name), " ", "")) {
		case "bip01head":
			anchors["head"] = index
		case "bip01rhand":
			anchors["right_hand"] = index
		case "bip01spine":
			anchors["upper_body"] = index
			anchors["aim_spine"] = index
		case "bip01spine1":
			anchors["aim_spine1"] = index
		case "bip01spine2":
			anchors["aim_spine2"] = index
		case "bip01spine3":
			anchors["aim_spine3"] = index
		case "bip01neck":
			anchors["aim_neck"] = index
		}
	}
	return anchors
}

func hl1PlayerHasRequiredAnchors(anchors map[string]int) bool {
	if anchors["right_hand"] < 0 || anchors["upper_body"] < 0 {
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
	if !assetHasMarker(asset, "right_hand") || !assetHasMarker(asset, "upper_body") {
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

func buildGameAssetCatalog(entries []GameAssetManifestEntry) *GameAssetCatalog {
	catalog := &GameAssetCatalog{}
	for _, entry := range entries {
		if entry.GeneratedAssetPath == "" || entry.CatalogID == "" {
			continue
		}
		switch entry.CatalogKind {
		case "player":
			if entry.HeadMarkerID == "" || entry.RightHandMarkerID == "" || entry.UpperBodyMarkerID == "" || len(entry.AimMarkerIDs) == 0 {
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
				UpperBodyMarkerID:     entry.UpperBodyMarkerID,
				AimMarkerIDs:          append([]string(nil), entry.AimMarkerIDs...),
				ClipIDs:               append([]string(nil), entry.ClipIDs...),
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

func hl1AssetOutputRelPath(sourcePath, gameDir, kind, sourceRef string) string {
	if gameDir != "" {
		if rel, err := filepath.Rel(gameDir, sourcePath); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return filepath.Join(kind, filepath.Base(sourceRef))
}

func copyHL1AssetFile(src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
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
