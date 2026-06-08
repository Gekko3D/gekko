package hl1

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

const (
	ImporterName                    = "gekko-hl1import"
	ImporterVersion                 = "hl1_import_report_v1"
	DefaultImportedWorldChunkSize   = 256
	DefaultImportedVoxelResolution  = 0.1
	DefaultGameAssetVoxelResolution = 0.08
	DefaultPickupVoxelResolution    = 0.04
	DefaultImportedSolidBandDepth   = DefaultSolidBandDepth
	DefaultImportedMaxSampledCells  = DefaultMaxSolidSampleCells
	DefaultChunkPayloadKind         = content.ImportedWorldChunkPayloadDenseRLEBinaryV1
)

type HL1LightMode string

const (
	HL1LightModeFaithful   HL1LightMode = "faithful"
	HL1LightModePointProxy HL1LightMode = "point-proxy"
)

type ImportOptions struct {
	GameDir                   string
	MapName                   string
	BSPPath                   string
	OutputRoot                string
	ChunkSize                 int
	VoxelResolution           float32
	GameAssetVoxelResolution  float32
	PickupVoxelResolution     float32
	MaxSolidSampleCells       int64
	SolidBandDepth            int
	ChunkPayloadKind          string
	LightMode                 HL1LightMode
	BakeStaticLightmaps       bool
	EmitLightFixtures         bool
	EmitEmissiveSurfaceLights bool
	MaxEmissiveSurfaceLights  int
	EmitGameAssets            bool
}

type ImportSummary struct {
	Map        importcommon.MapImport
	Report     importcommon.ImportReport
	BSP        *BSP
	WorldFaces []Face
	BakeFaces  []Face
	AllFaces   []Face
}

func BuildImportSummary(opts ImportOptions) (ImportSummary, error) {
	bspPath := opts.BSPPath
	if bspPath == "" {
		var err error
		bspPath, err = FindMapPath(opts.GameDir, opts.MapName)
		if err != nil {
			return ImportSummary{}, err
		}
	}
	if opts.GameDir == "" {
		opts.GameDir = InferGameDirFromBSPPath(bspPath)
	}
	bsp, err := LoadBSP(bspPath)
	if err != nil {
		return ImportSummary{}, err
	}
	source := importcommon.SourceInfo{
		Kind:            "hl1",
		GameDir:         opts.GameDir,
		MapName:         trimMapName(opts.MapName, bspPath),
		BSPPath:         bspPath,
		BSPHash:         bsp.SHA256,
		ImporterName:    ImporterName,
		ImporterVersion: ImporterVersion,
	}
	mapImport := importcommon.MapImport{
		Source:      source,
		Diagnostics: append([]importcommon.Diagnostic(nil), bsp.Diagnostics...),
	}
	if bounds, ok := bsp.WorldBoundsGekko(); ok {
		mapImport.Bounds = bounds
	}

	worldspawn := firstEntityByClass(bsp.Entities, "worldspawn")
	if wadValue := worldspawn.Value("wad"); wadValue != "" {
		source.WADPaths = ResolveWADPaths(opts.GameDir, wadValue)
		mapImport.Source.WADPaths = append([]string(nil), source.WADPaths...)
	}
	wads, wadDiagnostics := LoadResolvedWADs(source.WADPaths)
	mapImport.Diagnostics = append(mapImport.Diagnostics, wadDiagnostics...)
	mapImport.Materials = materialsFromBSPTextures(bsp.Textures, wads)
	mapImport.Diagnostics = append(mapImport.Diagnostics, missingTextureDiagnostics(bsp.Textures, wads)...)
	mapImport.Entities, mapImport.Lights, mapImport.Triggers = extractEntities(bsp)
	worldFaces, faceErr := bsp.WorldFaces()
	if faceErr != nil {
		mapImport.Diagnostics = append(mapImport.Diagnostics, importcommon.Diagnostic{
			Severity: importcommon.SeverityWarning,
			Code:     "hl1.world_faces_reconstruct_failed",
			Subject:  "model:0",
			Message:  faceErr.Error(),
		})
	}
	bakeFaces := append([]Face(nil), worldFaces...)
	allFaces := append([]Face(nil), worldFaces...)
	brushClassByModel := brushClassByModelID(mapImport.Entities)
	for modelID := 1; modelID < len(bsp.Models); modelID++ {
		modelFaces, err := bsp.ModelFaces(modelID)
		if err != nil {
			mapImport.Diagnostics = append(mapImport.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.model_faces_reconstruct_failed",
				Subject:  fmt.Sprintf("model:%d", modelID),
				Message:  err.Error(),
			})
			continue
		}
		allFaces = append(allFaces, modelFaces...)
		if visibleBrushEntityClass(brushClassByModel[modelID]) {
			bakeFaces = append(bakeFaces, modelFaces...)
		}
	}

	classNames := make([]string, 0, len(bsp.Entities))
	unsupported := make([]string, 0, len(bsp.Entities))
	for _, entity := range bsp.Entities {
		className := entity.ClassName()
		classNames = append(classNames, className)
		if !supportedClass(className) {
			unsupported = append(unsupported, className)
		}
	}

	report := importcommon.ImportReport{
		Source:                  source,
		GeneratedLevelPath:      generatedLevelPath(opts),
		GeneratedWorldPath:      generatedWorldPath(opts),
		MaterialCount:           len(mapImport.Materials),
		MaterialKindCounts:      materialKindCounts(mapImport.Materials),
		UnclassifiedMaterials:   unclassifiedMaterialTextures(mapImport.Materials),
		PaletteCount:            min(len(mapImport.Materials), 255),
		ModelCount:              len(bsp.Models),
		FaceCount:               len(worldFaces),
		SkyFaceCount:            countSkyFaces(worldFaces),
		EntityCounts:            importcommon.EntityCounts(classNames),
		UnsupportedEntityCounts: importcommon.EntityCounts(unsupported),
		MovingBrushEntityCounts: importcommon.EntityCounts(hl1MovingBrushEntityClassNames(mapImport.Entities)),
		PathNodeEntityCounts:    importcommon.EntityCounts(hl1PathNodeEntityClassNames(mapImport.Entities)),
		LadderEntityCounts:      importcommon.EntityCounts(hl1LadderEntityClassNames(mapImport.Entities)),
		ChargerEntityCounts:     importcommon.EntityCounts(hl1ChargerEntityClassNames(mapImport.Entities)),
		PickupEntityCounts:      importcommon.EntityCounts(hl1PickupClassNames(mapImport.Entities)),
		TriggerEntityCounts:     importcommon.EntityCounts(hl1TriggerEntityClassNames(mapImport.Entities)),
		BreakableEntityCounts:   importcommon.EntityCounts(hl1BreakableEntityClassNames(mapImport.Entities)),
		Diagnostics:             append([]importcommon.Diagnostic(nil), mapImport.Diagnostics...),
	}
	appendHL1TargetDiagnostics(&report, mapImport.Entities)
	appendHL1ReviewDiagnostics(&report, mapImport.Entities)
	if bounds, ok := bsp.WorldBoundsHammer(); ok {
		report.BoundsBeforeConversion = bounds
	}
	if bounds, ok := bsp.WorldBoundsGekko(); ok {
		report.BoundsAfterConversion = bounds
	}
	importcommon.PopulateImportReportDiagnosticCounts(&report)
	report.Source.WADPaths = append([]string(nil), source.WADPaths...)
	mapImport.Source = report.Source
	return ImportSummary{Map: mapImport, Report: report, BSP: bsp, WorldFaces: worldFaces, BakeFaces: bakeFaces, AllFaces: allFaces}, nil
}

func brushClassByModelID(entities []importcommon.Entity) map[int]string {
	out := make(map[int]string)
	for _, entity := range entities {
		if entity.BrushModelID <= 0 {
			continue
		}
		if _, exists := out[entity.BrushModelID]; exists {
			continue
		}
		out[entity.BrushModelID] = entity.ClassName
	}
	return out
}

func visibleBrushEntityClass(className string) bool {
	switch strings.ToLower(className) {
	case "func_wall",
		"func_illusionary",
		"func_breakable",
		"func_healthcharger",
		"func_recharge":
		return true
	default:
		return false
	}
}

func InferGameDirFromBSPPath(bspPath string) string {
	cleaned := filepath.Clean(bspPath)
	dir := filepath.Dir(cleaned)
	if strings.EqualFold(filepath.Base(dir), "maps") {
		parent := filepath.Dir(dir)
		if strings.EqualFold(filepath.Base(parent), "valve") || strings.EqualFold(filepath.Base(parent), "valve_downloads") {
			return filepath.Dir(parent)
		}
	}
	return ""
}

func FindMapPath(gameDir string, mapName string) (string, error) {
	if strings.TrimSpace(mapName) == "" {
		return "", fmt.Errorf("map name is empty")
	}
	if filepath.IsAbs(mapName) {
		return mapName, nil
	}
	name := mapName
	if filepath.Ext(name) == "" {
		name += ".bsp"
	}
	candidates := []string{
		filepath.Join(gameDir, "valve", "maps", name),
		filepath.Join(gameDir, "valve_downloads", "maps", name),
		filepath.Join(gameDir, "maps", name),
		filepath.Join(gameDir, name),
	}
	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate, nil
		}
	}
	return candidates[0], nil
}

func extractEntities(bsp *BSP) ([]importcommon.Entity, []importcommon.Light, []importcommon.Trigger) {
	if bsp == nil {
		return nil, nil, nil
	}
	entities := make([]importcommon.Entity, 0, len(bsp.Entities))
	var lights []importcommon.Light
	var triggers []importcommon.Trigger
	for _, raw := range bsp.Entities {
		className := raw.ClassName()
		entity := importcommon.Entity{
			ClassName: className,
			KeyValues: raw.Map(),
		}
		if origin, ok := parseVec3(raw.Value("origin")); ok {
			entity.SourceOrigin = origin
			entity.WorldPosition = HammerToGekko(origin)
		}
		if angle, ok := parseFloat32(raw.Value("angle")); ok {
			entity.SourceAngles = importcommon.Vec3{Y: angle}
		}
		if modelID, ok := brushModelID(raw.Value("model")); ok {
			entity.BrushModelID = modelID
			if modelID >= 0 && modelID < len(bsp.Models) {
				model := bsp.Models[modelID]
				entity.BrushWorldBounds = HammerBoundsToGekko(model.Min, model.Max)
			}
		}
		entities = append(entities, entity)
		switch strings.ToLower(className) {
		case "light", "light_spot", "light_environment":
			lights = append(lights, lightFromEntity(raw, entity.WorldPosition))
		case "trigger_changelevel":
			triggers = append(triggers, importcommon.Trigger{
				Kind:      "trigger_changelevel",
				Bounds:    entity.BrushWorldBounds,
				TargetMap: raw.Value("map"),
				Landmark:  raw.Value("landmark"),
			})
		}
	}
	return entities, lights, triggers
}

func lightFromEntity(raw RawEntity, position importcommon.Vec3) importcommon.Light {
	light := importcommon.Light{
		Name:     raw.Value("targetname"),
		Position: position,
		Style:    raw.Value("style"),
	}
	fields := strings.Fields(raw.Value("_light"))
	if len(fields) >= 3 {
		for i := 0; i < 3; i++ {
			value, err := strconv.Atoi(fields[i])
			if err == nil {
				light.Color[i] = uint8(max(0, min(value, 255)))
			}
		}
	}
	if len(fields) >= 4 {
		if intensity, err := strconv.ParseFloat(fields[3], 32); err == nil {
			light.Intensity = float32(intensity)
		}
	}
	return light
}

func materialsFromBSPTextures(textures []Texture, wads []*WAD) []importcommon.Material {
	out := make([]importcommon.Material, 0, len(textures))
	for i, texture := range textures {
		baseColor := texture.BaseColor
		sourceWAD := ""
		if baseColor == ([4]uint8{}) {
			for _, wad := range wads {
				if color, ok := wad.TextureColor(texture.Name); ok {
					baseColor = color
					sourceWAD = wad.Path
					break
				}
			}
		}
		semantics := materialSemantics(texture.Name)
		out = append(out, importcommon.Material{
			ID:                i + 1,
			PaletteIndex:      uint8(min(i+1, 255)),
			SourceTextureName: texture.Name,
			BaseColor:         baseColor,
			Kind:              semantics.Kind,
			CollisionKind:     semantics.CollisionKind,
			Transparent:       semantics.Transparent,
			EmitsLight:        semantics.EmitsLight,
			Emissive:          semantics.Emissive,
			Roughness:         semantics.Roughness,
			Metallic:          semantics.Metallic,
			Transparency:      semantics.Transparency,
			SourceWAD:         sourceWAD,
			Size:              [2]uint32{texture.Width, texture.Height},
			Tags:              semantics.Tags,
		})
	}
	return out
}

func emissiveStrengthForTextureName(textureName string) float32 {
	if !isCandidateEmissiveTextureName(textureName) {
		return 0
	}
	return 2.0
}

func countSkyFaces(faces []Face) int {
	count := 0
	for _, face := range faces {
		if strings.EqualFold(face.TextureName, "sky") {
			count++
		}
	}
	return count
}

func materialKindCounts(materials []importcommon.Material) []importcommon.EntityCount {
	kinds := make([]string, 0, len(materials))
	for _, material := range materials {
		kind := material.Kind
		if kind == "" {
			kind = "unknown"
		}
		kinds = append(kinds, kind)
	}
	return importcommon.EntityCounts(kinds)
}

func unclassifiedMaterialTextures(materials []importcommon.Material) []string {
	var out []string
	for _, material := range materials {
		if material.Kind != "structural" || !materialHasTag(material.Tags, "material:structural") {
			continue
		}
		if material.SourceTextureName == "" {
			continue
		}
		out = append(out, material.SourceTextureName)
	}
	return out
}

func materialHasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func hl1PickupClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		if _, ok := hl1PickupClass(entity.ClassName); ok {
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func hl1MovingBrushEntityClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		if _, ok := hl1MovingBrushKind(entity.ClassName); ok {
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func hl1PathNodeEntityClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		if strings.EqualFold(entity.ClassName, "path_corner") {
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func hl1LadderEntityClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		if strings.EqualFold(entity.ClassName, "func_ladder") {
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func hl1ChargerEntityClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		if _, _, ok := hl1ChargerKind(entity.ClassName); ok {
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func hl1TriggerEntityClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		switch strings.ToLower(strings.TrimSpace(entity.ClassName)) {
		case "trigger_once", "trigger_multiple", "trigger_hurt", "trigger_changelevel", "multi_manager", "trigger_relay":
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func hl1BreakableEntityClassNames(entities []importcommon.Entity) []string {
	out := make([]string, 0)
	for _, entity := range entities {
		if strings.EqualFold(entity.ClassName, "func_breakable") {
			out = append(out, strings.ToLower(entity.ClassName))
		}
	}
	return out
}

func appendHL1TargetDiagnostics(report *importcommon.ImportReport, entities []importcommon.Entity) {
	if report == nil {
		return
	}
	targetNames := hl1TargetNameSet(entities)
	var unresolvedTargets []string
	var skippedMultiTargets []string
	for _, entity := range entities {
		className := strings.ToLower(strings.TrimSpace(entity.ClassName))
		if target := hl1EntityKey(entity, "target"); target != "" {
			if _, ok := targetNames[target]; !ok {
				unresolvedTargets = append(unresolvedTargets, target)
				report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.target_unresolved",
					Subject:  className + ":" + target,
					Message:  "entity target does not match any imported targetname",
				})
			}
		}
		if className != "multi_manager" {
			continue
		}
		for _, skipped := range hl1SkippedMultiManagerOutputs(entity) {
			skippedMultiTargets = append(skippedMultiTargets, skipped)
			report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.multi_manager_output_skipped",
				Subject:  skipped,
				Message:  "multi_manager output key was skipped because its value is not a non-negative delay",
			})
		}
		for _, event := range hl1MultiManagerEvents(entity) {
			target := strings.TrimSpace(event.Target)
			if target == "" {
				continue
			}
			if _, ok := targetNames[target]; ok {
				continue
			}
			unresolvedTargets = append(unresolvedTargets, target)
			report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.target_unresolved",
				Subject:  className + ":" + target,
				Message:  "multi_manager output target does not match any imported targetname",
			})
		}
	}
	report.UnresolvedTargetCounts = importcommon.NamedCounts(unresolvedTargets)
	report.SkippedMultiTargetCounts = importcommon.NamedCounts(skippedMultiTargets)
}

func appendHL1ReviewDiagnostics(report *importcommon.ImportReport, entities []importcommon.Entity) {
	if report == nil {
		return
	}
	pathCornerNames := map[string]struct{}{}
	pathCornerCount := 0
	for _, entity := range entities {
		if !strings.EqualFold(entity.ClassName, "path_corner") {
			continue
		}
		pathCornerCount++
		if targetName := hl1EntityKey(entity, "targetname"); targetName != "" {
			pathCornerNames[targetName] = struct{}{}
		}
	}
	trainCount := 0
	unsupportedCounts := map[string]int{}
	for _, entity := range entities {
		className := strings.ToLower(strings.TrimSpace(entity.ClassName))
		if className == "" {
			continue
		}
		if !supportedClass(className) {
			unsupportedCounts[className]++
		}
		if _, ok := hl1MovingBrushKind(className); ok && entity.BrushModelID <= 0 {
			report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.moving_brush_model_missing",
				Subject:  hl1EntityDiagnosticSubject(entity, className),
				Message:  "moving brush entity has no BSP brush model and cannot emit a visual/gameplay brush",
			})
		}
		switch className {
		case "func_train":
			trainCount++
			pathTarget := hl1MovingBrushPathTarget(entity)
			if pathTarget == "" {
				report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.train_path_target_missing",
					Subject:  hl1EntityDiagnosticSubject(entity, className),
					Message:  "func_train has no target path_corner, so path motion cannot start",
				})
			} else if _, ok := pathCornerNames[pathTarget]; !ok {
				report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.train_path_target_unresolved",
					Subject:  className + ":" + pathTarget,
					Message:  "func_train target does not match any imported path_corner targetname",
				})
			}
		case "path_corner":
			if targetName := hl1EntityKey(entity, "targetname"); targetName == "" {
				report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
					Severity: importcommon.SeverityWarning,
					Code:     "hl1.path_corner_targetname_missing",
					Subject:  hl1EntityDiagnosticSubject(entity, className),
					Message:  "path_corner has no targetname and cannot be addressed by func_train/path links",
				})
			}
			if next := hl1EntityKey(entity, "target"); next != "" {
				if _, ok := pathCornerNames[next]; !ok {
					report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
						Severity: importcommon.SeverityWarning,
						Code:     "hl1.path_corner_next_unresolved",
						Subject:  className + ":" + next,
						Message:  "path_corner target does not match any imported path_corner targetname",
					})
				}
			}
		}
	}
	if trainCount > 0 && pathCornerCount == 0 {
		report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
			Severity: importcommon.SeverityWarning,
			Code:     "hl1.train_path_nodes_missing",
			Subject:  "func_train",
			Message:  "func_train entities exist but no path_corner entities were found in the BSP",
		})
	}
	unsupportedClasses := make([]string, 0, len(unsupportedCounts))
	for className := range unsupportedCounts {
		unsupportedClasses = append(unsupportedClasses, className)
	}
	sort.Strings(unsupportedClasses)
	for _, className := range unsupportedClasses {
		count := unsupportedCounts[className]
		report.Diagnostics = append(report.Diagnostics, importcommon.Diagnostic{
			Severity: importcommon.SeverityInfo,
			Code:     "hl1.entity_unsupported",
			Subject:  className,
			Message:  fmt.Sprintf("%d entity/entities of this class are cataloged but not imported into typed gameplay yet", count),
		})
	}
}

func hl1EntityDiagnosticSubject(entity importcommon.Entity, className string) string {
	if targetName := hl1EntityKey(entity, "targetname"); targetName != "" {
		return className + ":" + targetName
	}
	if model := hl1EntityKey(entity, "model"); model != "" {
		return className + ":" + model
	}
	return className
}

func hl1TargetNameSet(entities []importcommon.Entity) map[string]struct{} {
	out := make(map[string]struct{})
	for _, entity := range entities {
		if targetName := hl1EntityKey(entity, "targetname"); targetName != "" {
			out[targetName] = struct{}{}
		}
	}
	return out
}

func hl1SkippedMultiManagerOutputs(entity importcommon.Entity) []string {
	if entity.KeyValues == nil {
		return nil
	}
	out := make([]string, 0)
	for key, value := range entity.KeyValues {
		key = strings.TrimSpace(key)
		if key == "" || hl1MultiManagerReservedKey(key) {
			continue
		}
		delay, err := strconv.ParseFloat(strings.TrimSpace(value), 32)
		if err == nil && delay >= 0 {
			continue
		}
		out = append(out, key)
	}
	return out
}

func hl1EntityKey(entity importcommon.Entity, key string) string {
	if entity.KeyValues == nil {
		return ""
	}
	return strings.TrimSpace(entity.KeyValues[key])
}

func missingTextureDiagnostics(textures []Texture, wads []*WAD) []importcommon.Diagnostic {
	var diagnostics []importcommon.Diagnostic
	for _, texture := range textures {
		if texture.Name == "" || texture.Embedded {
			continue
		}
		found := false
		for _, wad := range wads {
			if wad.HasEntry(texture.Name) {
				found = true
				break
			}
		}
		if !found {
			diagnostics = append(diagnostics, importcommon.Diagnostic{
				Severity: importcommon.SeverityWarning,
				Code:     "hl1.texture_missing",
				Subject:  texture.Name,
				Message:  "texture is not embedded and was not found in resolved WADs",
			})
		}
	}
	return diagnostics
}

func firstEntityByClass(entities []RawEntity, className string) RawEntity {
	for _, entity := range entities {
		if strings.EqualFold(entity.ClassName(), className) {
			return entity
		}
	}
	return RawEntity{}
}

func brushModelID(value string) (int, bool) {
	if !strings.HasPrefix(value, "*") {
		return 0, false
	}
	id, err := strconv.Atoi(strings.TrimPrefix(value, "*"))
	if err != nil {
		return 0, false
	}
	return id, true
}

func supportedClass(className string) bool {
	switch strings.ToLower(className) {
	case "worldspawn",
		"info_player_start",
		"info_landmark",
		"trigger_changelevel",
		"light",
		"light_spot",
		"light_environment",
		"func_wall",
		"func_illusionary",
		"func_breakable",
		"func_ladder",
		"func_door",
		"func_door_rotating",
		"func_button",
		"func_healthcharger",
		"func_recharge",
		"func_plat",
		"func_train",
		"path_corner",
		"trigger_once",
		"trigger_multiple",
		"trigger_hurt",
		"multi_manager",
		"trigger_relay",
		"momentary_door":
		return true
	default:
		_, ok := hl1PickupClass(className)
		return ok
	}
}

func materialKind(textureName string) string {
	return materialSemantics(textureName).Kind
}

func isCandidateEmissiveTextureName(textureName string) bool {
	name := normalizedEmissiveTextureName(textureName)
	switch {
	case strings.HasPrefix(name, "~"):
		return true
	case strings.Contains(name, "light"),
		strings.Contains(name, "lght"),
		strings.Contains(name, "lite"),
		strings.Contains(name, "lamp"),
		strings.Contains(name, "bulb"),
		strings.Contains(name, "fluor"),
		strings.Contains(name, "flour"),
		strings.Contains(name, "glow"):
		return true
	default:
		raw := strings.ToLower(strings.TrimSpace(textureName))
		return strings.HasPrefix(raw, "+") && (strings.Contains(raw, "light") || strings.Contains(raw, "lght") || strings.Contains(raw, "lite"))
	}
}

func normalizedEmissiveTextureName(textureName string) string {
	name := strings.ToLower(strings.TrimSpace(textureName))
	if len(name) > 2 && (name[0] == '+' || name[0] == '-') {
		prefix := name[1]
		if (prefix >= '0' && prefix <= '9') || (prefix >= 'a' && prefix <= 'j') {
			name = name[2:]
		}
	}
	return name
}

func collisionKind(textureName string) string {
	return materialSemantics(textureName).CollisionKind
}

func isTransparentTexture(textureName string) bool {
	return materialSemantics(textureName).Transparent
}

func isCutoutTexture(textureName string) bool {
	name := strings.ToLower(textureName)
	return strings.HasPrefix(name, "{") || strings.Contains(name, "ladder")
}

func generatedLevelPath(opts ImportOptions) string {
	if opts.OutputRoot == "" || opts.MapName == "" {
		return ""
	}
	return filepath.Join(opts.OutputRoot, trimKnownExt(filepath.Base(opts.MapName), ".bsp")+".gklevel")
}

func generatedWorldPath(opts ImportOptions) string {
	if opts.OutputRoot == "" || opts.MapName == "" {
		return ""
	}
	base := trimKnownExt(filepath.Base(opts.MapName), ".bsp")
	return filepath.Join(opts.OutputRoot, "worlds", base+".gkworld")
}

func generatedLightFixtureAssetPath(opts ImportOptions, lightID string) string {
	if opts.OutputRoot == "" || opts.MapName == "" || lightID == "" {
		return ""
	}
	base := trimKnownExt(filepath.Base(opts.MapName), ".bsp")
	return filepath.Join(opts.OutputRoot, "assets", "hl1_light_emitters", base+"_"+lightID+".gkasset")
}

func trimMapName(mapName string, bspPath string) string {
	if mapName != "" {
		return trimKnownExt(filepath.Base(mapName), ".bsp")
	}
	return trimKnownExt(filepath.Base(bspPath), ".bsp")
}

func trimKnownExt(value string, ext string) string {
	if strings.EqualFold(filepath.Ext(value), ext) {
		return strings.TrimSuffix(value, filepath.Ext(value))
	}
	return value
}
