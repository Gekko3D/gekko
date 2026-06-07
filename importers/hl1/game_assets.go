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
	Diagnostics   []importcommon.Diagnostic `json:"diagnostics,omitempty"`
}

type GameAssetManifestEntry struct {
	Kind                string            `json:"kind"`
	SourceRef           string            `json:"source_ref"`
	SourcePath          string            `json:"source_path,omitempty"`
	OutputPath          string            `json:"output_path,omitempty"`
	GeneratedAssetPath  string            `json:"generated_asset_path,omitempty"`
	GeneratedVoxelCount int               `json:"generated_voxel_count,omitempty"`
	SizeBytes           int64             `json:"size_bytes,omitempty"`
	SHA256              string            `json:"sha256,omitempty"`
	Resolved            bool              `json:"resolved"`
	UsedBy              []string          `json:"used_by,omitempty"`
	ConvertState        string            `json:"convert_state,omitempty"`
	ModelInfo           *MDLInfo          `json:"model_info,omitempty"`
	SpriteInfo          *SPRInfo          `json:"sprite_info,omitempty"`
	generatedAsset      *content.AssetDef `json:"-"`
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
	collector := newHL1AssetCollector(gameDir, outputRoot, mapName)
	if opts.VoxelResolution > 0 {
		collector.voxelResolution = opts.VoxelResolution
	}
	for _, wadPath := range summary.Report.Source.WADPaths {
		collector.addAbsolute("wad", wadPath, "worldspawn.wad")
	}
	for _, entity := range summary.Map.Entities {
		usedBy := entity.ClassName
		if usedBy == "" {
			usedBy = "entity"
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
	manifest.Assets, manifest.Diagnostics = collector.buildEntries()
	return GameAssetImportResult{ManifestPath: manifestPath, Manifest: manifest}, nil
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
	gameDir         string
	outputRoot      string
	mapName         string
	voxelResolution float32
	entries         map[string]*GameAssetManifestEntry
	diagnostics     []importcommon.Diagnostic
}

func newHL1AssetCollector(gameDir, outputRoot, mapName string) *hl1AssetCollector {
	return &hl1AssetCollector{
		gameDir:         filepath.Clean(gameDir),
		outputRoot:      filepath.Clean(outputRoot),
		mapName:         mapName,
		voxelResolution: DefaultImportedVoxelResolution,
		entries:         map[string]*GameAssetManifestEntry{},
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

func (c *hl1AssetCollector) add(kind, sourceRef, sourcePath, usedBy string) {
	key := kind + ":" + strings.ToLower(filepath.ToSlash(sourceRef))
	entry := c.entries[key]
	if entry == nil {
		entry = &GameAssetManifestEntry{
			Kind:         kind,
			SourceRef:    filepath.ToSlash(sourceRef),
			ConvertState: hl1AssetConvertState(kind),
		}
		c.entries[key] = entry
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
		geometry, err := LoadMDLGeometry(entry.SourcePath)
		if err != nil {
			c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.mdl_parse_failed",
				Subject:  entry.SourceRef,
				Message:  err.Error(),
			})
		} else {
			entry.ModelInfo = &geometry.Info
			assetPath := filepath.Join(c.outputRoot, "hl1_assets", c.mapName, "generated", "models", safeHL1AssetBaseName(entry.SourceRef)+".gkasset")
			asset, voxelCount, err := BuildMDLVoxelAsset(geometry, MDLVoxelAssetOptions{
				Name:            strings.TrimSuffix(filepath.Base(entry.SourceRef), filepath.Ext(entry.SourceRef)),
				SourceRef:       entry.SourceRef,
				VoxelResolution: c.voxelResolution,
			})
			if err != nil {
				c.diagnostics = append(c.diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.mdl_voxelize_failed",
					Subject:  entry.SourceRef,
					Message:  err.Error(),
				})
			} else if asset != nil {
				entry.GeneratedAssetPath = filepath.Clean(assetPath)
				entry.GeneratedVoxelCount = voxelCount
				entry.generatedAsset = asset
				entry.ConvertState = "generated_voxel_asset"
			}
		}
	} else if kind == "sprite" {
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
				VoxelResolution: c.voxelResolution,
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
				entry.generatedAsset = asset
				entry.ConvertState = "generated_voxel_asset"
			}
		}
	}
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
		return out[i].SourceRef < out[j].SourceRef
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		return diagnostics[i].Subject < diagnostics[j].Subject
	})
	return out, diagnostics
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
