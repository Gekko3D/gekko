package hl1

import (
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gekko3d/gekko/content"
	importcommon "github.com/gekko3d/gekko/importers/common"
	"github.com/go-gl/mathgl/mgl32"
)

const MarkerKindHL1PlayerSpawn = "hl1_player_spawn"
const MarkerKindHL1Door = "hl1_func_door"
const MarkerKindHL1DoorRotating = "hl1_func_door_rotating"
const MarkerKindHL1Button = "hl1_func_button"
const MarkerKindHL1MovingBrush = "hl1_moving_brush"
const MarkerKindHL1Pickup = "hl1_pickup"
const MovingBrushKindHL1Door = "hl1_func_door"
const MovingBrushKindHL1DoorRotating = "hl1_func_door_rotating"
const MovingBrushKindHL1Button = "hl1_func_button"
const MovingBrushKindHL1Plat = "hl1_func_plat"
const UseTriggerKindHL1Button = "hl1_func_button"
const TriggerVolumeKindHL1TriggerOnce = "hl1_trigger_once"
const TriggerVolumeKindHL1TriggerMultiple = "hl1_trigger_multiple"
const MultiTargetKindHL1MultiManager = "hl1_multi_manager"
const TargetRelayKindHL1TriggerRelay = "hl1_trigger_relay"
const BreakableKindHL1FuncBreakable = "hl1_func_breakable"
const DefaultMaxEmissiveSurfaceLights = 64
const DefaultHL1LadderClimbSpeed = 200 * HammerUnitMeters

const minEmissiveSurfaceLightVoxels = 8

type GeneratedLevelResult struct {
	LevelPath          string
	Level              *content.LevelDef
	LightFixtureAssets []GeneratedAssetResult
	MovingBrushAssets  []GeneratedAssetResult
	BreakableAssets    []GeneratedAssetResult
}

type GeneratedAssetResult struct {
	AssetPath string
	Asset     *content.AssetDef
}

func BuildGeneratedLevel(opts ImportOptions, summary ImportSummary, manifestPath string, voxelizedWorld ...VoxelizeResult) (GeneratedLevelResult, error) {
	return buildGeneratedLevel(opts, summary, manifestPath, nil, voxelizedWorld...)
}

func BuildGeneratedLevelWithGameAssets(opts ImportOptions, summary ImportSummary, manifestPath string, gameAssets GameAssetImportResult, voxelizedWorld ...VoxelizeResult) (GeneratedLevelResult, error) {
	return buildGeneratedLevel(opts, summary, manifestPath, &gameAssets, voxelizedWorld...)
}

func buildGeneratedLevel(opts ImportOptions, summary ImportSummary, manifestPath string, gameAssets *GameAssetImportResult, voxelizedWorld ...VoxelizeResult) (GeneratedLevelResult, error) {
	if manifestPath == "" {
		return GeneratedLevelResult{}, fmt.Errorf("manifest path is empty")
	}
	levelPath := generatedLevelPath(opts)
	if levelPath == "" {
		return GeneratedLevelResult{}, fmt.Errorf("output root and map name are required for level emission")
	}
	level := content.NewLevelDef(summary.Report.Source.MapName)
	level.ChunkSize = opts.ChunkSize
	level.VoxelResolution = opts.VoxelResolution
	level.Tags = []string{"source:hl1", "debug:surface_voxel"}
	level.Player = hl1LevelPlayerDef()
	directionalCastsShadows := true
	level.Environment = &content.LevelEnvironmentDef{
		Preset:                  "fullmoonnight_gi",
		DirectionalCastsShadows: &directionalCastsShadows,
		Tags:                    []string{"source:hl1"},
	}
	level.BaseWorld = &content.LevelBaseWorldDef{
		Kind:              content.ImportedWorldKindVoxelWorld,
		ManifestPath:      filepath.ToSlash(relativeOrBase(filepath.Dir(levelPath), manifestPath)),
		ReadOnlyByDefault: false,
		CollisionEnabled:  true,
		Tags:              []string{"source:hl1", "debug:surface_voxel"},
	}
	waterFaces := summary.AllFaces
	if len(waterFaces) == 0 {
		waterFaces = summary.WorldFaces
	}
	level.WaterBodies = buildHL1WaterBodies(summary.BSP, waterFaces, opts.VoxelResolution)
	if spawn, ok := firstImportEntityByClass(summary.Map.Entities, "info_player_start"); ok {
		level.Markers = append(level.Markers, content.LevelMarkerDef{
			ID:   "hl1_player_spawn_0",
			Name: "info_player_start",
			Kind: MarkerKindHL1PlayerSpawn,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{spawn.WorldPosition.X, spawn.WorldPosition.Y, spawn.WorldPosition.Z},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "classname:info_player_start"},
		})
	}
	level.Markers = append(level.Markers, buildHL1GameplayMarkers(summary.Map.Entities)...)
	level.Pickups = buildHL1Pickups(summary.Map.Entities, levelPath, gameAssets)
	level.LadderVolumes = buildHL1LadderVolumes(summary.Map.Entities, opts.VoxelResolution)
	movingBrushes, movingBrushAssets, err := buildHL1MovingBrushes(opts, summary, levelPath)
	if err != nil {
		return GeneratedLevelResult{}, err
	}
	level.MovingBrushes = movingBrushes
	level.UseTriggers = buildHL1UseTriggers(summary.Map.Entities)
	level.TriggerVolumes = buildHL1TriggerVolumes(summary.Map.Entities)
	level.MultiTargets = buildHL1MultiTargets(summary.Map.Entities)
	level.TargetRelays = buildHL1TargetRelays(summary.Map.Entities)
	breakables, breakableAssets, err := buildHL1Breakables(opts, summary, levelPath)
	if err != nil {
		return GeneratedLevelResult{}, err
	}
	level.Breakables = breakables
	level.Placements = append(level.Placements, buildHL1GeneratedAssetPlacements(summary.Map.Entities, levelPath, gameAssets)...)
	lights, err := buildHL1LevelLights(opts, summary.Map.Entities)
	if err != nil {
		return GeneratedLevelResult{}, err
	}
	if opts.EmitEmissiveSurfaceLights && len(voxelizedWorld) > 0 {
		lights = append(lights, buildHL1EmissiveSurfaceLights(opts, voxelizedWorld[0], len(lights))...)
	}
	level.Lights = lights
	if opts.EmitLightFixtures {
		assets, err := attachHL1LightFixtures(opts, levelPath, level)
		if err != nil {
			return GeneratedLevelResult{}, err
		}
		content.EnsureLevelIDs(level)
		return GeneratedLevelResult{LevelPath: filepath.Clean(levelPath), Level: level, LightFixtureAssets: assets, MovingBrushAssets: movingBrushAssets, BreakableAssets: breakableAssets}, nil
	}
	content.EnsureLevelIDs(level)
	return GeneratedLevelResult{LevelPath: filepath.Clean(levelPath), Level: level, MovingBrushAssets: movingBrushAssets, BreakableAssets: breakableAssets}, nil
}

func hl1LevelPlayerDef() *content.LevelPlayerDef {
	return &content.LevelPlayerDef{
		SpawnKind:  MarkerKindHL1PlayerSpawn,
		Height:     72 * HammerUnitMeters,
		EyeHeight:  64 * HammerUnitMeters,
		Radius:     16 * HammerUnitMeters,
		StepHeight: 18 * HammerUnitMeters,
		Tags:       []string{"source:hl1", "hull:standing"},
	}
}

func buildHL1GeneratedAssetPlacements(entities []importcommon.Entity, levelPath string, gameAssets *GameAssetImportResult) []content.LevelPlacementDef {
	if gameAssets == nil || gameAssets.Manifest == nil {
		return nil
	}
	generatedByRef := generatedHL1AssetPathsByRef(gameAssets.Manifest.Assets)
	if len(generatedByRef) == 0 {
		return nil
	}
	levelDir := filepath.Dir(levelPath)
	placements := make([]content.LevelPlacementDef, 0)
	countsByBase := map[string]int{}
	for _, entity := range entities {
		if _, ok := hl1PickupClass(entity.ClassName); ok {
			continue
		}
		if entity.KeyValues == nil {
			continue
		}
		modelRef := strings.TrimSpace(entity.KeyValues["model"])
		assetKind := hl1AssetKindForRef(modelRef)
		if modelRef == "" || strings.HasPrefix(modelRef, "*") || (assetKind != "model" && assetKind != "sprite") {
			continue
		}
		assetPath := generatedByRef[normalizedHL1AssetRef(modelRef)]
		if assetPath == "" {
			continue
		}
		base := safeHL1AssetBaseName(modelRef)
		index := countsByBase[base]
		countsByBase[base]++
		placements = append(placements, content.LevelPlacementDef{
			ID:        fmt.Sprintf("hl1_%s_%s_%d", assetKind, base, index),
			AssetPath: filepath.ToSlash(relativeOrBase(levelDir, assetPath)),
			Transform: content.LevelTransformDef{
				Position: content.Vec3{entity.WorldPosition.X, entity.WorldPosition.Y, entity.WorldPosition.Z},
				Rotation: hl1StaticModelRotation(entity),
				Scale:    content.Vec3{1, 1, 1},
			},
			PlacementMode: content.LevelPlacementModeFree3D,
			Tags: []string{
				"source:hl1",
				"classname:" + strings.ToLower(entity.ClassName),
				"model:" + filepath.ToSlash(modelRef),
				"kind:static_" + assetKind,
			},
		})
	}
	return placements
}

func generatedHL1AssetPathsByRef(entries []GameAssetManifestEntry) map[string]string {
	out := map[string]string{}
	for _, entry := range entries {
		if (entry.Kind != "model" && entry.Kind != "sprite") || entry.GeneratedAssetPath == "" || entry.ConvertState != "generated_voxel_asset" {
			continue
		}
		out[normalizedHL1AssetRef(entry.SourceRef)] = entry.GeneratedAssetPath
	}
	return out
}

func normalizedHL1AssetRef(ref string) string {
	return strings.ToLower(filepath.ToSlash(filepath.Clean(filepath.FromSlash(strings.TrimSpace(ref)))))
}

func hl1StaticModelRotation(entity importcommon.Entity) content.Quat {
	yaw := float32(0)
	if angles, ok := parseEntityVec3(entity, "angles"); ok {
		yaw = angles.Y
	} else if value, ok := parseEntityFloat(entity, "angle"); ok && value >= 0 {
		yaw = value
	}
	return quatDefFromMGL(mgl32.QuatRotate(float32(float64(yaw)*math.Pi/180), mgl32.Vec3{0, 1, 0}))
}

func SaveGeneratedLevel(result GeneratedLevelResult) error {
	if result.Level == nil {
		return fmt.Errorf("level is nil")
	}
	for _, asset := range append(append(append([]GeneratedAssetResult(nil), result.LightFixtureAssets...), result.MovingBrushAssets...), result.BreakableAssets...) {
		if asset.Asset == nil {
			return fmt.Errorf("generated asset is nil")
		}
		if err := os.MkdirAll(filepath.Dir(asset.AssetPath), 0755); err != nil {
			return err
		}
		if err := content.SaveAsset(asset.AssetPath, asset.Asset); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(result.LevelPath), 0755); err != nil {
		return err
	}
	return content.SaveLevel(result.LevelPath, result.Level)
}

func buildHL1GameplayMarkers(entities []importcommon.Entity) []content.LevelMarkerDef {
	markers := make([]content.LevelMarkerDef, 0)
	countsByClass := map[string]int{}
	for _, entity := range entities {
		kind, ok := hl1GameplayMarkerKind(entity.ClassName)
		if !ok || entity.BrushModelID <= 0 {
			continue
		}
		className := strings.ToLower(entity.ClassName)
		index := countsByClass[className]
		countsByClass[className]++
		bounds := entity.BrushWorldBounds
		center := content.Vec3{
			(bounds.Min.X + bounds.Max.X) * 0.5,
			(bounds.Min.Y + bounds.Max.Y) * 0.5,
			(bounds.Min.Z + bounds.Max.Z) * 0.5,
		}
		markers = append(markers, content.LevelMarkerDef{
			ID:   fmt.Sprintf("hl1_%s_%d", className, index),
			Name: hl1EntityDisplayName(entity, className),
			Kind: kind,
			Transform: content.LevelTransformDef{
				Position: center,
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: hl1GameplayMarkerTags(entity, bounds),
		})
	}
	return markers
}

func hl1GameplayMarkerKind(className string) (string, bool) {
	switch strings.ToLower(className) {
	case "func_door", "momentary_door":
		return MarkerKindHL1Door, true
	case "func_door_rotating":
		return MarkerKindHL1DoorRotating, true
	case "func_button":
		return MarkerKindHL1Button, true
	case "func_plat", "func_train":
		return MarkerKindHL1MovingBrush, true
	default:
		return "", false
	}
}

func buildHL1Pickups(entities []importcommon.Entity, levelPath string, gameAssets *GameAssetImportResult) []content.LevelPickupDef {
	pickups := make([]content.LevelPickupDef, 0)
	countsByClass := map[string]int{}
	generatedByRef := generatedHL1AssetPathsByRef(nil)
	if gameAssets != nil && gameAssets.Manifest != nil {
		generatedByRef = generatedHL1AssetPathsByRef(gameAssets.Manifest.Assets)
	}
	levelDir := filepath.Dir(levelPath)
	for _, entity := range entities {
		pickup, ok := hl1PickupClass(entity.ClassName)
		if !ok {
			continue
		}
		className := strings.ToLower(entity.ClassName)
		assetPath := ""
		if modelRef := hl1PickupModelRef(className); modelRef != "" {
			if generatedPath := generatedByRef[normalizedHL1AssetRef(modelRef)]; generatedPath != "" {
				assetPath = filepath.ToSlash(relativeOrBase(levelDir, generatedPath))
			}
		}
		index := countsByClass[className]
		countsByClass[className]++
		pickups = append(pickups, content.LevelPickupDef{
			ID:        fmt.Sprintf("hl1_pickup_%s_%d", className, index),
			Name:      hl1EntityDisplayName(entity, className),
			Kind:      MarkerKindHL1Pickup,
			AssetPath: assetPath,
			Category:  pickup.Category,
			Item:      pickup.Item,
			Amount:    hl1PickupDefaultAmount(pickup),
			ClassName: className,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{entity.WorldPosition.X, entity.WorldPosition.Y, entity.WorldPosition.Z},
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			TargetName: hl1StringKey(entity, "targetname"),
			SpawnFlags: hl1IntKey(entity, "spawnflags"),
			SourceTag:  "hl1:" + className,
			Tags:       hl1PickupTags(entity, pickup),
		})
	}
	return pickups
}

type hl1PickupInfo struct {
	Category string
	Item     string
}

func hl1PickupClass(className string) (hl1PickupInfo, bool) {
	className = strings.ToLower(strings.TrimSpace(className))
	switch {
	case strings.HasPrefix(className, "weapon_"):
		return hl1PickupInfo{Category: "weapon", Item: strings.TrimPrefix(className, "weapon_")}, true
	case strings.HasPrefix(className, "ammo_"):
		return hl1PickupInfo{Category: "ammo", Item: strings.TrimPrefix(className, "ammo_")}, true
	}
	switch className {
	case "item_healthkit", "item_battery", "item_suit", "item_longjump", "weaponbox":
		return hl1PickupInfo{Category: "item", Item: strings.TrimPrefix(className, "item_")}, true
	default:
		return hl1PickupInfo{}, false
	}
}

func hl1PickupModelRef(className string) string {
	switch strings.ToLower(strings.TrimSpace(className)) {
	case "weapon_9mmhandgun":
		return "models/w_9mmhandgun.mdl"
	case "weapon_357":
		return "models/w_357.mdl"
	case "weapon_9mmar":
		return "models/w_9mmar.mdl"
	case "weapon_shotgun":
		return "models/w_shotgun.mdl"
	case "weapon_crossbow":
		return "models/w_crossbow.mdl"
	case "weapon_rpg":
		return "models/w_rpg.mdl"
	case "weapon_gauss":
		return "models/w_gauss.mdl"
	case "weapon_egon":
		return "models/w_egon.mdl"
	case "weapon_hornetgun":
		return "models/w_hgun.mdl"
	case "weapon_handgrenade":
		return "models/w_grenade.mdl"
	case "weapon_satchel":
		return "models/w_satchel.mdl"
	case "weapon_tripmine":
		return "models/w_tripmine.mdl"
	case "weapon_snark":
		return "models/w_squeak.mdl"
	case "ammo_9mmclip":
		return "models/w_9mmclip.mdl"
	case "ammo_9mmbox":
		return "models/w_9mmbox.mdl"
	case "ammo_9mmar":
		return "models/w_9mmarclip.mdl"
	case "ammo_argrenades":
		return "models/w_argrenade.mdl"
	case "ammo_buckshot":
		return "models/w_shotbox.mdl"
	case "ammo_357":
		return "models/w_357ammobox.mdl"
	case "ammo_crossbow":
		return "models/w_crossbow_clip.mdl"
	case "ammo_gaussclip":
		return "models/w_gaussammo.mdl"
	case "ammo_rpgclip":
		return "models/w_rpgammo.mdl"
	case "item_healthkit":
		return "models/w_medkit.mdl"
	case "item_battery":
		return "models/w_battery.mdl"
	case "item_suit":
		return "models/w_suit.mdl"
	case "item_longjump":
		return "models/w_longjump.mdl"
	case "weaponbox":
		return "models/w_weaponbox.mdl"
	default:
		return ""
	}
}

func hl1PickupDefaultAmount(pickup hl1PickupInfo) int {
	switch pickup.Category {
	case "ammo":
		switch pickup.Item {
		case "9mmclip", "glockclip":
			return 17
		case "9mmbox", "9mmar", "mp5clip":
			return 50
		case "argrenades":
			return 2
		case "buckshot":
			return 12
		default:
			return 1
		}
	case "item":
		switch pickup.Item {
		case "healthkit":
			return 15
		case "battery":
			return 15
		default:
			return 1
		}
	default:
		return 1
	}
}

func hl1PickupTags(entity importcommon.Entity, pickup hl1PickupInfo) []string {
	className := strings.ToLower(entity.ClassName)
	tags := []string{
		"source:hl1",
		"classname:" + className,
		"pickup:hl1",
		"pickup_category:" + pickup.Category,
		"pickup_item:" + pickup.Item,
	}
	for _, key := range []string{"targetname", "spawnflags"} {
		if entity.KeyValues == nil {
			break
		}
		value := strings.TrimSpace(entity.KeyValues[key])
		if value == "" {
			continue
		}
		tags = append(tags, "hl1_"+key+":"+strings.ReplaceAll(value, " ", ","))
	}
	return tags
}

func hl1EntityDisplayName(entity importcommon.Entity, fallback string) string {
	if entity.KeyValues != nil {
		if value := strings.TrimSpace(entity.KeyValues["targetname"]); value != "" {
			return value
		}
	}
	return fallback
}

func hl1GameplayMarkerTags(entity importcommon.Entity, bounds importcommon.Bounds) []string {
	className := strings.ToLower(entity.ClassName)
	tags := []string{
		"source:hl1",
		"classname:" + className,
		fmt.Sprintf("model:%d", entity.BrushModelID),
		"bounds_min:" + hl1Vec3Tag(bounds.Min),
		"bounds_max:" + hl1Vec3Tag(bounds.Max),
	}
	for _, key := range []string{"targetname", "target", "master", "message", "speed", "wait", "delay", "lip", "height", "angle", "angles", "movedir", "spawnflags", "sounds", "dmg"} {
		if entity.KeyValues == nil {
			break
		}
		value := strings.TrimSpace(entity.KeyValues[key])
		if value == "" {
			continue
		}
		tags = append(tags, "hl1_"+key+":"+strings.ReplaceAll(value, " ", ","))
	}
	return tags
}

func hl1PointEntityTags(entity importcommon.Entity) []string {
	className := strings.ToLower(entity.ClassName)
	tags := []string{
		"source:hl1",
		"classname:" + className,
	}
	for _, key := range []string{"targetname", "target", "master", "message", "delay", "spawnflags"} {
		if entity.KeyValues == nil {
			break
		}
		value := strings.TrimSpace(entity.KeyValues[key])
		if value == "" {
			continue
		}
		tags = append(tags, "hl1_"+key+":"+strings.ReplaceAll(value, " ", ","))
	}
	return tags
}

func buildHL1MovingBrushes(opts ImportOptions, summary ImportSummary, levelPath string) ([]content.LevelMovingBrushDef, []GeneratedAssetResult, error) {
	entities := summary.Map.Entities
	out := make([]content.LevelMovingBrushDef, 0)
	assets := make([]GeneratedAssetResult, 0)
	var textureStore *TextureStore
	materialColors := materialColorMap(summary.Map.Materials)
	if summary.BSP != nil {
		wads, _ := LoadResolvedWADs(summary.Report.Source.WADPaths)
		textureStore = NewTextureStore(summary.BSP.Textures, wads)
	}
	countsByClass := map[string]int{}
	for _, entity := range entities {
		kind, ok := hl1MovingBrushKind(entity.ClassName)
		if !ok || entity.BrushModelID <= 0 {
			continue
		}
		className := strings.ToLower(entity.ClassName)
		index := countsByClass[className]
		countsByClass[className]++
		bounds := entity.BrushWorldBounds
		center, halfExtents := contentBoundsCenterHalfExtents(bounds)
		brush := content.LevelMovingBrushDef{
			ID:                fmt.Sprintf("hl1_moving_%s_%d", className, index),
			Name:              hl1EntityDisplayName(entity, className),
			Kind:              kind,
			BoundsCenter:      center,
			BoundsHalfExtents: halfExtents,
			MoveDirection:     hl1MoveDirection(entity),
			MoveDistance:      hl1MoveDistance(entity),
			Speed:             hl1FloatKey(entity, "speed") * HammerUnitMeters,
			Wait:              hl1FloatKey(entity, "wait"),
			Lip:               hl1FloatKey(entity, "lip") * HammerUnitMeters,
			TargetName:        hl1StringKey(entity, "targetname"),
			Target:            hl1StringKey(entity, "target"),
			SourceTag:         "hl1:" + className,
			Tags:              hl1GameplayMarkerTags(entity, bounds),
		}
		if summary.BSP != nil && hl1MovingBrushHasSeparateVisual(className) {
			asset, visualOrigin, err := buildHL1MovingBrushAsset(opts, summary.BSP, textureStore, materialColors, entity, brush.ID)
			if err != nil {
				return nil, nil, err
			}
			if asset.Asset != nil {
				brush.AssetPath = filepath.ToSlash(relativeOrBase(filepath.Dir(levelPath), asset.AssetPath))
				brush.VisualOrigin = visualOrigin
				assets = append(assets, asset)
			}
		}
		out = append(out, brush)
	}
	return out, assets, nil
}

func buildHL1Breakables(opts ImportOptions, summary ImportSummary, levelPath string) ([]content.LevelBreakableDef, []GeneratedAssetResult, error) {
	entities := summary.Map.Entities
	out := make([]content.LevelBreakableDef, 0)
	assets := make([]GeneratedAssetResult, 0)
	var textureStore *TextureStore
	materialColors := materialColorMap(summary.Map.Materials)
	if summary.BSP != nil {
		wads, _ := LoadResolvedWADs(summary.Report.Source.WADPaths)
		textureStore = NewTextureStore(summary.BSP.Textures, wads)
	}
	count := 0
	for _, entity := range entities {
		if !strings.EqualFold(entity.ClassName, "func_breakable") || entity.BrushModelID <= 0 {
			continue
		}
		bounds := entity.BrushWorldBounds
		center, halfExtents := contentBoundsCenterHalfExtents(bounds)
		breakable := content.LevelBreakableDef{
			ID:                fmt.Sprintf("hl1_breakable_func_breakable_%d", count),
			Name:              hl1EntityDisplayName(entity, "func_breakable"),
			Kind:              BreakableKindHL1FuncBreakable,
			BoundsCenter:      center,
			BoundsHalfExtents: halfExtents,
			Health:            hl1BreakableHealth(entity),
			Material:          hl1StringKey(entity, "material"),
			SpawnObject:       hl1StringKey(entity, "spawnobject"),
			SpawnFlags:        hl1IntKey(entity, "spawnflags"),
			TargetName:        hl1StringKey(entity, "targetname"),
			Target:            hl1StringKey(entity, "target"),
			Delay:             hl1FloatKey(entity, "delay"),
			SourceTag:         "hl1:func_breakable",
			Tags:              hl1BreakableTags(entity, bounds),
		}
		if summary.BSP != nil {
			asset, visualOrigin, err := buildHL1BreakableAsset(opts, summary.BSP, textureStore, materialColors, entity, breakable.ID)
			if err != nil {
				return nil, nil, err
			}
			if asset.Asset != nil {
				breakable.AssetPath = filepath.ToSlash(relativeOrBase(filepath.Dir(levelPath), asset.AssetPath))
				breakable.VisualOrigin = visualOrigin
				assets = append(assets, asset)
			}
		}
		out = append(out, breakable)
		count++
	}
	return out, assets, nil
}

func hl1BreakableHealth(entity importcommon.Entity) float32 {
	health := hl1FloatKey(entity, "health")
	if health <= 0 {
		return 1
	}
	return health
}

func hl1BreakableTags(entity importcommon.Entity, bounds importcommon.Bounds) []string {
	tags := hl1GameplayMarkerTags(entity, bounds)
	for _, key := range []string{"health", "material", "spawnobject", "explodemagnitude", "gibmodel"} {
		value := hl1StringKey(entity, key)
		if value == "" {
			continue
		}
		tags = append(tags, "hl1_"+key+":"+strings.ReplaceAll(value, " ", ","))
	}
	return tags
}

func buildHL1UseTriggers(entities []importcommon.Entity) []content.LevelUseTriggerDef {
	out := make([]content.LevelUseTriggerDef, 0)
	count := 0
	for _, entity := range entities {
		if !strings.EqualFold(entity.ClassName, "func_button") || entity.BrushModelID <= 0 {
			continue
		}
		bounds := entity.BrushWorldBounds
		center, halfExtents := contentBoundsCenterHalfExtents(bounds)
		out = append(out, content.LevelUseTriggerDef{
			ID:                fmt.Sprintf("hl1_use_func_button_%d", count),
			Name:              hl1EntityDisplayName(entity, "func_button"),
			Kind:              UseTriggerKindHL1Button,
			BoundsCenter:      center,
			BoundsHalfExtents: halfExtents,
			TargetName:        hl1StringKey(entity, "targetname"),
			Target:            hl1StringKey(entity, "target"),
			SourceTag:         "hl1:func_button",
			Tags:              hl1GameplayMarkerTags(entity, bounds),
		})
		count++
	}
	return out
}

func buildHL1TriggerVolumes(entities []importcommon.Entity) []content.LevelTriggerVolumeDef {
	out := make([]content.LevelTriggerVolumeDef, 0)
	countsByClass := map[string]int{}
	for _, entity := range entities {
		kind, once, ok := hl1TriggerVolumeKind(entity.ClassName)
		if !ok || entity.BrushModelID <= 0 {
			continue
		}
		className := strings.ToLower(entity.ClassName)
		index := countsByClass[className]
		countsByClass[className]++
		bounds := entity.BrushWorldBounds
		center, halfExtents := contentBoundsCenterHalfExtents(bounds)
		out = append(out, content.LevelTriggerVolumeDef{
			ID:                fmt.Sprintf("hl1_trigger_%s_%d", className, index),
			Name:              hl1EntityDisplayName(entity, className),
			Kind:              kind,
			BoundsCenter:      center,
			BoundsHalfExtents: halfExtents,
			TargetName:        hl1StringKey(entity, "targetname"),
			Target:            hl1StringKey(entity, "target"),
			Delay:             hl1FloatKey(entity, "delay"),
			Wait:              hl1FloatKey(entity, "wait"),
			Once:              once,
			SourceTag:         "hl1:" + className,
			Tags:              hl1GameplayMarkerTags(entity, bounds),
		})
	}
	return out
}

func hl1TriggerVolumeKind(className string) (string, bool, bool) {
	switch strings.ToLower(className) {
	case "trigger_once":
		return TriggerVolumeKindHL1TriggerOnce, true, true
	case "trigger_multiple":
		return TriggerVolumeKindHL1TriggerMultiple, false, true
	default:
		return "", false, false
	}
}

func buildHL1MultiTargets(entities []importcommon.Entity) []content.LevelMultiTargetDef {
	out := make([]content.LevelMultiTargetDef, 0)
	count := 0
	for _, entity := range entities {
		if !strings.EqualFold(entity.ClassName, "multi_manager") {
			continue
		}
		events := hl1MultiManagerEvents(entity)
		if len(events) == 0 {
			continue
		}
		out = append(out, content.LevelMultiTargetDef{
			ID:         fmt.Sprintf("hl1_multi_manager_%d", count),
			Name:       hl1EntityDisplayName(entity, "multi_manager"),
			TargetName: hl1StringKey(entity, "targetname"),
			Delay:      hl1FloatKey(entity, "delay"),
			Events:     events,
			SourceTag:  "hl1:multi_manager",
			Tags:       hl1PointEntityTags(entity),
		})
		count++
	}
	return out
}

func buildHL1TargetRelays(entities []importcommon.Entity) []content.LevelTargetRelayDef {
	out := make([]content.LevelTargetRelayDef, 0)
	count := 0
	for _, entity := range entities {
		if !strings.EqualFold(entity.ClassName, "trigger_relay") {
			continue
		}
		out = append(out, content.LevelTargetRelayDef{
			ID:           fmt.Sprintf("hl1_trigger_relay_%d", count),
			Name:         hl1EntityDisplayName(entity, "trigger_relay"),
			Kind:         TargetRelayKindHL1TriggerRelay,
			TargetName:   hl1StringKey(entity, "targetname"),
			Target:       hl1StringKey(entity, "target"),
			Delay:        hl1FloatKey(entity, "delay"),
			KillTarget:   hl1StringKey(entity, "killtarget"),
			TriggerState: hl1TriggerRelayState(entity),
			SpawnFlags:   hl1IntKey(entity, "spawnflags"),
			SourceTag:    "hl1:trigger_relay",
			Tags:         hl1PointEntityTags(entity),
		})
		count++
	}
	return out
}

func hl1TriggerRelayState(entity importcommon.Entity) int {
	state := hl1IntKey(entity, "triggerstate")
	if state < 0 || state > 2 {
		return 2
	}
	return state
}

func hl1MultiManagerEvents(entity importcommon.Entity) []content.LevelTargetEventDef {
	if entity.KeyValues == nil {
		return nil
	}
	keys := make([]string, 0, len(entity.KeyValues))
	for key := range entity.KeyValues {
		key = strings.TrimSpace(key)
		if key == "" || hl1MultiManagerReservedKey(key) {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	events := make([]content.LevelTargetEventDef, 0, len(keys))
	for _, key := range keys {
		delay, err := strconv.ParseFloat(strings.TrimSpace(entity.KeyValues[key]), 32)
		if err != nil || delay < 0 {
			continue
		}
		events = append(events, content.LevelTargetEventDef{Target: hl1MultiManagerTargetName(key), Delay: float32(delay)})
	}
	return events
}

func hl1MultiManagerReservedKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "classname", "targetname", "target", "origin", "angles", "angle", "model", "spawnflags", "delay", "wait", "message", "master", "globalname":
		return true
	default:
		return false
	}
}

func hl1MultiManagerTargetName(key string) string {
	key = strings.TrimSpace(key)
	hash := strings.LastIndex(key, "#")
	if hash < 0 || hash == len(key)-1 {
		return key
	}
	for _, r := range key[hash+1:] {
		if r < '0' || r > '9' {
			return key
		}
	}
	return key[:hash]
}

func hl1MovingBrushKind(className string) (string, bool) {
	switch strings.ToLower(className) {
	case "func_door", "momentary_door":
		return MovingBrushKindHL1Door, true
	case "func_door_rotating":
		return MovingBrushKindHL1DoorRotating, true
	case "func_button":
		return MovingBrushKindHL1Button, true
	case "func_plat":
		return MovingBrushKindHL1Plat, true
	default:
		return "", false
	}
}

func hl1MovingBrushHasSeparateVisual(className string) bool {
	switch strings.ToLower(className) {
	case "func_door", "momentary_door", "func_button", "func_plat":
		return true
	default:
		return false
	}
}

func buildHL1MovingBrushAsset(opts ImportOptions, bsp *BSP, textureStore *TextureStore, materialColors map[int][4]uint8, entity importcommon.Entity, brushID string) (GeneratedAssetResult, content.Vec3, error) {
	if bsp == nil || entity.BrushModelID <= 0 {
		return GeneratedAssetResult{}, content.Vec3{}, nil
	}
	faces, err := bsp.ModelFaces(entity.BrushModelID)
	if err != nil {
		return GeneratedAssetResult{}, content.Vec3{}, fmt.Errorf("model %d moving brush faces: %w", entity.BrushModelID, err)
	}
	voxelized := VoxelizeFacesCPU(faces, VoxelizeOptions{
		VoxelResolution: opts.VoxelResolution,
		TextureStore:    textureStore,
		MaterialColors:  materialColors,
	})
	if len(voxelized.Voxels) == 0 {
		return GeneratedAssetResult{}, content.Vec3{}, nil
	}
	localVoxels, visualOrigin := localizeHL1MovingBrushVoxels(voxelized.Voxels, opts.VoxelResolution)
	asset := content.NewAssetDef(brushID)
	asset.Tags = []string{"source:hl1", "moving_brush", "classname:" + strings.ToLower(entity.ClassName)}
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	asset.Materials = assetMaterialsForHL1Voxels(voxelized.Materials, localVoxels)
	asset.Parts = []content.AssetPartDef{{
		ID:              "brush",
		Name:            "brush",
		VoxelResolution: opts.VoxelResolution,
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxelShape,
			VoxelShape: &content.AssetVoxelShapeDef{
				Palette: assetVoxelPaletteForMaterials(asset.Materials),
				Voxels:  localVoxels,
			},
		},
		Tags: []string{"source:hl1", "moving_brush"},
	}}
	path := filepath.Join(opts.OutputRoot, "assets", "hl1", "moving_brushes", brushID+".gkasset")
	return GeneratedAssetResult{AssetPath: filepath.Clean(path), Asset: asset}, visualOrigin, nil
}

func buildHL1BreakableAsset(opts ImportOptions, bsp *BSP, textureStore *TextureStore, materialColors map[int][4]uint8, entity importcommon.Entity, breakableID string) (GeneratedAssetResult, content.Vec3, error) {
	if bsp == nil || entity.BrushModelID <= 0 {
		return GeneratedAssetResult{}, content.Vec3{}, nil
	}
	faces, err := bsp.ModelFaces(entity.BrushModelID)
	if err != nil {
		return GeneratedAssetResult{}, content.Vec3{}, fmt.Errorf("model %d breakable faces: %w", entity.BrushModelID, err)
	}
	voxelized := VoxelizeFacesCPU(faces, VoxelizeOptions{
		VoxelResolution: opts.VoxelResolution,
		TextureStore:    textureStore,
		MaterialColors:  materialColors,
	})
	if len(voxelized.Voxels) == 0 {
		return GeneratedAssetResult{}, content.Vec3{}, nil
	}
	localVoxels, visualOrigin := localizeHL1MovingBrushVoxels(voxelized.Voxels, opts.VoxelResolution)
	asset := content.NewAssetDef(breakableID)
	asset.Tags = []string{"source:hl1", "breakable", "classname:" + strings.ToLower(entity.ClassName)}
	asset.Runtime = &content.AssetRuntimeDef{CollapseVoxelParts: true}
	asset.Materials = assetMaterialsForHL1Voxels(voxelized.Materials, localVoxels)
	asset.Parts = []content.AssetPartDef{{
		ID:              "breakable",
		Name:            "breakable",
		VoxelResolution: opts.VoxelResolution,
		Transform: content.AssetTransformDef{
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Source: content.AssetSourceDef{
			Kind: content.AssetSourceKindVoxelShape,
			VoxelShape: &content.AssetVoxelShapeDef{
				Palette: assetVoxelPaletteForMaterials(asset.Materials),
				Voxels:  localVoxels,
			},
		},
		Tags: []string{"source:hl1", "breakable"},
	}}
	path := filepath.Join(opts.OutputRoot, "assets", "hl1", "breakables", breakableID+".gkasset")
	return GeneratedAssetResult{AssetPath: filepath.Clean(path), Asset: asset}, visualOrigin, nil
}

func localizeHL1MovingBrushVoxels(voxels []importcommon.Voxel, resolution float32) ([]content.VoxelObjectVoxelDef, content.Vec3) {
	minX, minY, minZ := voxels[0].X, voxels[0].Y, voxels[0].Z
	for _, voxel := range voxels[1:] {
		minX = min(minX, voxel.X)
		minY = min(minY, voxel.Y)
		minZ = min(minZ, voxel.Z)
	}
	out := make([]content.VoxelObjectVoxelDef, 0, len(voxels))
	for _, voxel := range voxels {
		out = append(out, content.VoxelObjectVoxelDef{
			X:     voxel.X - minX,
			Y:     voxel.Y - minY,
			Z:     voxel.Z - minZ,
			Value: voxel.Palette,
		})
	}
	return out, content.Vec3{float32(minX) * resolution, float32(minY) * resolution, float32(minZ) * resolution}
}

func assetMaterialsForHL1Voxels(materials []importcommon.Material, voxels []content.VoxelObjectVoxelDef) []content.AssetMaterialDef {
	used := map[uint8]struct{}{}
	for _, voxel := range voxels {
		if voxel.Value != 0 {
			used[voxel.Value] = struct{}{}
		}
	}
	byPalette := map[uint8]importcommon.Material{}
	for _, material := range materials {
		if material.PaletteIndex != 0 {
			byPalette[material.PaletteIndex] = material
		} else if material.ID > 0 && material.ID <= 255 {
			byPalette[uint8(material.ID)] = material
		}
	}
	keys := make([]int, 0, len(used))
	for value := range used {
		keys = append(keys, int(value))
	}
	sort.Ints(keys)
	out := make([]content.AssetMaterialDef, 0, len(keys))
	for _, key := range keys {
		value := uint8(key)
		material := byPalette[value]
		baseColor := material.BaseColor
		if baseColor == ([4]uint8{}) {
			baseColor = [4]uint8{180, 180, 180, 255}
		}
		materialID := fmt.Sprintf("mat_%d", value)
		out = append(out, assetMaterialFromHL1Material(materialID, materialID, material, baseColor, []string{"source_asset:moving_brush"}))
	}
	return out
}

func assetMaterialFromHL1Material(id, name string, material importcommon.Material, baseColor [4]uint8, extraTags []string) content.AssetMaterialDef {
	if baseColor == ([4]uint8{}) {
		baseColor = [4]uint8{180, 180, 180, 255}
	}
	roughness := material.Roughness
	if roughness <= 0 {
		roughness = 1
	}
	transparency := material.Transparency
	if baseColor[3] < 255 {
		transparency = maxFloat32(transparency, 1-float32(baseColor[3])/255)
	}
	tags := append([]string{"source:hl1"}, material.Tags...)
	tags = appendUniqueString(tags, "kind:"+material.Kind)
	if material.SourceTextureName != "" {
		tags = appendUniqueString(tags, "source_texture:"+material.SourceTextureName)
	}
	tags = append(tags, extraTags...)
	return content.AssetMaterialDef{
		ID:           id,
		Name:         name,
		BaseColor:    baseColor,
		Roughness:    roughness,
		Metallic:     material.Metallic,
		Emissive:     material.Emissive,
		Transparency: transparency,
		Tags:         tags,
	}
}

func assetVoxelPaletteForMaterials(materials []content.AssetMaterialDef) []content.AssetVoxelPaletteEntryDef {
	out := make([]content.AssetVoxelPaletteEntryDef, 0, len(materials))
	for _, material := range materials {
		var value int
		if _, err := fmt.Sscanf(material.ID, "mat_%d", &value); err != nil || value <= 0 || value > 255 {
			continue
		}
		out = append(out, content.AssetVoxelPaletteEntryDef{
			Value:      uint8(value),
			MaterialID: material.ID,
		})
	}
	return out
}

func contentBoundsCenterHalfExtents(bounds importcommon.Bounds) (content.Vec3, content.Vec3) {
	center := content.Vec3{
		(bounds.Min.X + bounds.Max.X) * 0.5,
		(bounds.Min.Y + bounds.Max.Y) * 0.5,
		(bounds.Min.Z + bounds.Max.Z) * 0.5,
	}
	halfExtents := content.Vec3{
		maxf32((bounds.Max.X-bounds.Min.X)*0.5, 0.001),
		maxf32((bounds.Max.Y-bounds.Min.Y)*0.5, 0.001),
		maxf32((bounds.Max.Z-bounds.Min.Z)*0.5, 0.001),
	}
	return center, halfExtents
}

func hl1MoveDirection(entity importcommon.Entity) content.Vec3 {
	if strings.EqualFold(entity.ClassName, "func_plat") {
		return content.Vec3{0, 1, 0}
	}
	if movedir := hl1StringKey(entity, "movedir"); movedir != "" {
		parts := strings.Fields(strings.ReplaceAll(movedir, ",", " "))
		if len(parts) == 3 {
			x, xErr := strconv.ParseFloat(parts[0], 32)
			y, yErr := strconv.ParseFloat(parts[1], 32)
			z, zErr := strconv.ParseFloat(parts[2], 32)
			if xErr == nil && yErr == nil && zErr == nil {
				return normalizeContentVec3(content.Vec3{float32(x), float32(z), -float32(y)})
			}
		}
	}
	angle := hl1FloatKey(entity, "angle")
	switch angle {
	case -1:
		return content.Vec3{0, 1, 0}
	case -2:
		return content.Vec3{0, -1, 0}
	default:
		rad := float64(angle) * math.Pi / 180
		return normalizeContentVec3(content.Vec3{float32(math.Cos(rad)), 0, -float32(math.Sin(rad))})
	}
}

func hl1MoveDistance(entity importcommon.Entity) float32 {
	if strings.EqualFold(entity.ClassName, "func_plat") {
		if height := hl1FloatKey(entity, "height"); height > 0 {
			return height * HammerUnitMeters
		}
	}
	return 0
}

func normalizeContentVec3(v content.Vec3) content.Vec3 {
	length := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	if length <= 1e-6 {
		return content.Vec3{1, 0, 0}
	}
	return content.Vec3{v[0] / length, v[1] / length, v[2] / length}
}

func hl1FloatKey(entity importcommon.Entity, key string) float32 {
	value := hl1StringKey(entity, key)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return 0
	}
	return float32(parsed)
}

func hl1IntKey(entity importcommon.Entity, key string) int {
	value := hl1StringKey(entity, key)
	if value == "" {
		return 0
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}
	return parsed
}

func hl1StringKey(entity importcommon.Entity, key string) string {
	if entity.KeyValues == nil {
		return ""
	}
	return strings.TrimSpace(entity.KeyValues[key])
}

func buildHL1LadderVolumes(entities []importcommon.Entity, voxelResolution float32) []content.LevelLadderVolumeDef {
	volumes := make([]content.LevelLadderVolumeDef, 0)
	padding := float32(math.Max(float64(voxelResolution*0.5), 0.05))
	count := 0
	for _, entity := range entities {
		if !strings.EqualFold(entity.ClassName, "func_ladder") || entity.BrushModelID <= 0 {
			continue
		}
		bounds := entity.BrushWorldBounds
		center := content.Vec3{
			(bounds.Min.X + bounds.Max.X) * 0.5,
			(bounds.Min.Y + bounds.Max.Y) * 0.5,
			(bounds.Min.Z + bounds.Max.Z) * 0.5,
		}
		halfExtents := content.Vec3{
			maxf32((bounds.Max.X-bounds.Min.X)*0.5+padding, padding),
			maxf32((bounds.Max.Y-bounds.Min.Y)*0.5+padding, padding),
			maxf32((bounds.Max.Z-bounds.Min.Z)*0.5+padding, padding),
		}
		volumes = append(volumes, content.LevelLadderVolumeDef{
			ID:                fmt.Sprintf("hl1_func_ladder_%d", count),
			Name:              hl1EntityDisplayName(entity, "func_ladder"),
			BoundsCenter:      center,
			BoundsHalfExtents: halfExtents,
			ClimbSpeed:        DefaultHL1LadderClimbSpeed,
			SourceTag:         "hl1:func_ladder",
			Tags:              hl1GameplayMarkerTags(entity, bounds),
		})
		count++
	}
	return volumes
}

func maxf32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func hl1Vec3Tag(v importcommon.Vec3) string {
	return fmt.Sprintf("%.4f,%.4f,%.4f", v.X, v.Y, v.Z)
}

func attachHL1LightFixtures(opts ImportOptions, levelPath string, level *content.LevelDef) ([]GeneratedAssetResult, error) {
	if level == nil {
		return nil, fmt.Errorf("level is nil")
	}
	mapName := trimKnownExt(filepath.Base(opts.MapName), ".bsp")
	if mapName == "" {
		mapName = "hl1_map"
	}
	assets := make([]GeneratedAssetResult, 0, len(level.Lights))
	for i := range level.Lights {
		light := &level.Lights[i]
		if light.Type == content.LevelLightTypeAmbient {
			continue
		}
		linkID := hl1EmitterLinkID(mapName, light.ID)
		light.EmitterLinkID = linkID
		if light.SourceRadius == 0 {
			light.SourceRadius = 0.12
		}
		assetPath := generatedLightFixtureAssetPath(opts, light.ID)
		if assetPath == "" {
			return nil, fmt.Errorf("output root and map name are required for light fixture asset emission")
		}
		asset := buildHL1LightFixtureAsset(mapName, *light, linkID)
		assets = append(assets, GeneratedAssetResult{
			AssetPath: filepath.Clean(assetPath),
			Asset:     asset,
		})
		level.Placements = append(level.Placements, content.LevelPlacementDef{
			ID:        light.ID + "_emitter",
			AssetPath: filepath.ToSlash(relativeOrBase(filepath.Dir(levelPath), assetPath)),
			Transform: content.LevelTransformDef{
				Position: light.Transform.Position,
				Rotation: light.Transform.Rotation,
				Scale:    content.Vec3{1, 1, 1},
			},
			PlacementMode: content.LevelPlacementModeFree3D,
			Tags:          []string{"source:hl1", "kind:light_emitter", "light:" + light.ID},
		})
	}
	return assets, nil
}

func buildHL1LightFixtureAsset(mapName string, light content.LevelLightDef, linkID uint32) *content.AssetDef {
	color := lightFixtureColorUint8(light.Color)
	castsShadows := false
	return &content.AssetDef{
		ID:            mapName + "_" + light.ID + "_emitter_asset",
		SchemaVersion: content.CurrentAssetSchemaVersion,
		Name:          "HL1 light emitter " + light.ID,
		Tags:          []string{"source:hl1", "kind:light_emitter"},
		Runtime:       &content.AssetRuntimeDef{CastsShadows: &castsShadows},
		Materials: []content.AssetMaterialDef{{
			ID:        "bulb",
			Name:      "bulb",
			BaseColor: color,
			Roughness: 0.25,
			Metallic:  0,
			Emissive:  maxFloat32(2.5, minFloat32(6, light.Intensity)),
			IOR:       1.5,
		}},
		Parts: []content.AssetPartDef{{
			ID:              "bulb_part",
			Name:            "bulb",
			VoxelResolution: 0.08,
			ModelScale:      1,
			EmitterLinkID:   linkID,
			Source: content.AssetSourceDef{
				Kind:      content.AssetSourceKindVoxelShape,
				Operation: content.AssetShapeOperationAdd,
				VoxelShape: &content.AssetVoxelShapeDef{
					Palette: []content.AssetVoxelPaletteEntryDef{{Value: 1, MaterialID: "bulb"}},
					Voxels: []content.VoxelObjectVoxelDef{
						{X: 1, Y: 1, Z: 1, Value: 1},
						{X: 0, Y: 1, Z: 1, Value: 1},
						{X: 2, Y: 1, Z: 1, Value: 1},
						{X: 1, Y: 0, Z: 1, Value: 1},
						{X: 1, Y: 2, Z: 1, Value: 1},
						{X: 1, Y: 1, Z: 0, Value: 1},
						{X: 1, Y: 1, Z: 2, Value: 1},
					},
				},
			},
			Transform: content.AssetTransformDef{
				Rotation: content.Quat{0, 0, 0, 1},
				Scale:    content.Vec3{1, 1, 1},
			},
			Tags: []string{"source:hl1", "kind:light_emitter"},
		}},
	}
}

func lightFixtureColorUint8(color [3]float32) [4]uint8 {
	return [4]uint8{
		uint8(max(0, min(int(color[0]*255+0.5), 255))),
		uint8(max(0, min(int(color[1]*255+0.5), 255))),
		uint8(max(0, min(int(color[2]*255+0.5), 255))),
		255,
	}
}

func hl1EmitterLinkID(mapName string, lightID string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(mapName))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(lightID))
	id := h.Sum32()
	if id == 0 {
		return 1
	}
	return id
}

func firstImportEntityByClass(entities []importcommon.Entity, className string) (importcommon.Entity, bool) {
	for _, entity := range entities {
		if strings.EqualFold(entity.ClassName, className) {
			return entity, true
		}
	}
	return importcommon.Entity{}, false
}

func buildHL1LevelLights(opts ImportOptions, entities []importcommon.Entity) ([]content.LevelLightDef, error) {
	lightMode := normalizedHL1LightMode(opts.LightMode)
	if !isValidHL1LightMode(lightMode) {
		return nil, fmt.Errorf("unsupported HL1 light mode %q", opts.LightMode)
	}
	lights := make([]content.LevelLightDef, 0)
	lights = append(lights, content.LevelLightDef{
		ID:           "hl1_ambient_0",
		Name:         "hl1_ambient",
		Type:         content.LevelLightTypeAmbient,
		Color:        [3]float32{0.82, 0.78, 0.70},
		Intensity:    0.035,
		Range:        40,
		SourceTag:    "hl1:ambient",
		Tags:         []string{"source:hl1", "classname:ambient"},
		Transform:    content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}},
		SourceRadius: 0,
	})
	index := 0
	for _, entity := range entities {
		className := strings.ToLower(entity.ClassName)
		if className != "light" && className != "light_spot" {
			continue
		}
		color, rawIntensity := hl1LightColorIntensity(entity)
		lightType := content.LevelLightTypePoint
		coneAngle := float32(0)
		rotation := content.Quat{0, 0, 0, 1}
		dir := mgl32.Vec3{0, -1, 0}
		if className == "light_spot" {
			dir = hl1SpotDirectionGekko(entity)
			if lightMode == HL1LightModeFaithful {
				lightType = content.LevelLightTypeSpot
				coneAngle = hl1SpotConeAngle(entity)
				rotation = quatDefFromMGL(spotRotationFromDirection(dir))
			}
		}
		position := nudgeHL1LightPosition(entity.WorldPosition, dir, className)
		name := entity.KeyValues["targetname"]
		if name == "" {
			name = className
		}
		lights = append(lights, content.LevelLightDef{
			ID:   fmt.Sprintf("hl1_light_%d", index),
			Name: name,
			Transform: content.LevelTransformDef{
				Position: content.Vec3{position.X(), position.Y(), position.Z()},
				Rotation: rotation,
				Scale:    content.Vec3{1, 1, 1},
			},
			Type:         lightType,
			Color:        color,
			Intensity:    hl1LightIntensity(rawIntensity),
			Range:        hl1LightRange(rawIntensity),
			ConeAngle:    coneAngle,
			CastsShadows: true,
			SourceRadius: hl1LightSourceRadius(rawIntensity),
			SourceTag:    "hl1:" + className,
			Style:        entity.KeyValues["style"],
			Tags:         []string{"source:hl1", "classname:" + className},
		})
		index++
	}
	return lights, nil
}

type hl1EmissiveSurfaceVoxel struct {
	key      [3]int
	palette  uint8
	color    [4]uint8
	emissive float32
}

type hl1EmissiveSurfaceCluster struct {
	voxels      int
	min         [3]int
	max         [3]int
	sum         [3]int64
	colorSum    [3]float64
	emissiveSum float64
}

func buildHL1EmissiveSurfaceLights(opts ImportOptions, voxelized VoxelizeResult, startIndex int) []content.LevelLightDef {
	if opts.VoxelResolution <= 0 || len(voxelized.Voxels) == 0 {
		return nil
	}
	maxLights := opts.MaxEmissiveSurfaceLights
	if maxLights <= 0 {
		maxLights = DefaultMaxEmissiveSurfaceLights
	}
	materials := emissiveMaterialLookup(voxelized.Materials)
	if len(materials) == 0 {
		return nil
	}
	pending := make(map[[3]int]hl1EmissiveSurfaceVoxel)
	for _, voxel := range voxelized.Voxels {
		if voxel.SolidKind != "emissive" || voxel.Palette < uint8(emissivePaletteStart) {
			continue
		}
		material, ok := materials[voxel.Palette]
		if !ok || !material.EmitsLight || material.Emissive <= 0 {
			continue
		}
		key := [3]int{voxel.X, voxel.Y, voxel.Z}
		pending[key] = hl1EmissiveSurfaceVoxel{
			key:      key,
			palette:  voxel.Palette,
			color:    material.BaseColor,
			emissive: material.Emissive,
		}
	}
	clusters := make([]hl1EmissiveSurfaceCluster, 0)
	queue := make([][3]int, 0, 256)
	for len(pending) > 0 {
		var seed [3]int
		for key := range pending {
			seed = key
			break
		}
		cluster := hl1EmissiveSurfaceCluster{min: seed, max: seed}
		queue = append(queue[:0], seed)
		for len(queue) > 0 {
			key := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			voxel, ok := pending[key]
			if !ok {
				continue
			}
			delete(pending, key)
			cluster.add(voxel)
			for _, neighbor := range sixNeighborKeys(key) {
				if _, ok := pending[neighbor]; ok {
					queue = append(queue, neighbor)
				}
			}
		}
		if cluster.voxels >= minEmissiveSurfaceLightVoxels {
			clusters = append(clusters, cluster)
		}
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].voxels != clusters[j].voxels {
			return clusters[i].voxels > clusters[j].voxels
		}
		if clusters[i].sum[1] != clusters[j].sum[1] {
			return clusters[i].sum[1] > clusters[j].sum[1]
		}
		return clusters[i].sum[0] < clusters[j].sum[0]
	})
	if len(clusters) > maxLights {
		clusters = clusters[:maxLights]
	}
	lights := make([]content.LevelLightDef, 0, len(clusters))
	for i, cluster := range clusters {
		lights = append(lights, cluster.toLight(opts.VoxelResolution, startIndex+i))
	}
	return lights
}

func emissiveMaterialLookup(materials []importcommon.Material) map[uint8]importcommon.Material {
	out := make(map[uint8]importcommon.Material)
	for _, material := range materials {
		if !material.EmitsLight || material.Emissive <= 0 || material.PaletteIndex == 0 {
			continue
		}
		out[material.PaletteIndex] = material
	}
	return out
}

func (cluster *hl1EmissiveSurfaceCluster) add(voxel hl1EmissiveSurfaceVoxel) {
	if cluster.voxels == 0 {
		cluster.min = voxel.key
		cluster.max = voxel.key
	}
	cluster.voxels++
	for axis := 0; axis < 3; axis++ {
		cluster.min[axis] = min(cluster.min[axis], voxel.key[axis])
		cluster.max[axis] = max(cluster.max[axis], voxel.key[axis])
		cluster.sum[axis] += int64(voxel.key[axis])
	}
	cluster.colorSum[0] += float64(voxel.color[0])
	cluster.colorSum[1] += float64(voxel.color[1])
	cluster.colorSum[2] += float64(voxel.color[2])
	cluster.emissiveSum += float64(voxel.emissive)
}

func (cluster hl1EmissiveSurfaceCluster) toLight(resolution float32, index int) content.LevelLightDef {
	count := max(1, cluster.voxels)
	center := content.Vec3{
		(float32(cluster.sum[0])/float32(count) + 0.5) * resolution,
		(float32(cluster.sum[1])/float32(count) + 0.5) * resolution,
		(float32(cluster.sum[2])/float32(count) + 0.5) * resolution,
	}
	color := [3]float32{
		float32(cluster.colorSum[0] / float64(count) / 255.0),
		float32(cluster.colorSum[1] / float64(count) / 255.0),
		float32(cluster.colorSum[2] / float64(count) / 255.0),
	}
	maxExtent := max(cluster.max[0]-cluster.min[0]+1, max(cluster.max[1]-cluster.min[1]+1, cluster.max[2]-cluster.min[2]+1))
	avgEmission := float32(cluster.emissiveSum / float64(count))
	intensity := minFloat32(3.5, maxFloat32(0.8, avgEmission*0.45+float32(math.Sqrt(float64(count)))*0.025))
	rangeMeters := minFloat32(14, maxFloat32(4, float32(maxExtent)*resolution*3.0+2.5))
	sourceRadius := minFloat32(0.8, maxFloat32(0.18, float32(maxExtent)*resolution*0.18))
	return content.LevelLightDef{
		ID:   fmt.Sprintf("hl1_emissive_light_%d", index),
		Name: "hl1_emissive_surface_light",
		Transform: content.LevelTransformDef{
			Position: center,
			Rotation: content.Quat{0, 0, 0, 1},
			Scale:    content.Vec3{1, 1, 1},
		},
		Type:         content.LevelLightTypePoint,
		Color:        color,
		Intensity:    intensity,
		Range:        rangeMeters,
		CastsShadows: false,
		SourceRadius: sourceRadius,
		SourceTag:    "hl1:emissive_surface",
		Tags:         []string{"source:hl1", "source:emissive_surface", "synthetic:surface_light"},
	}
}

func sixNeighborKeys(key [3]int) [][3]int {
	return [][3]int{
		{key[0] - 1, key[1], key[2]},
		{key[0] + 1, key[1], key[2]},
		{key[0], key[1] - 1, key[2]},
		{key[0], key[1] + 1, key[2]},
		{key[0], key[1], key[2] - 1},
		{key[0], key[1], key[2] + 1},
	}
}

func normalizedHL1LightMode(mode HL1LightMode) HL1LightMode {
	if mode == "" {
		return HL1LightModeFaithful
	}
	return HL1LightMode(strings.ToLower(strings.TrimSpace(string(mode))))
}

func isValidHL1LightMode(mode HL1LightMode) bool {
	switch mode {
	case HL1LightModeFaithful, HL1LightModePointProxy:
		return true
	default:
		return false
	}
}

func hl1LightColorIntensity(entity importcommon.Entity) ([3]float32, float32) {
	color := [3]float32{1, 0.92, 0.78}
	intensity := float32(200)
	fields := strings.Fields(entity.KeyValues["_light"])
	if len(fields) >= 3 {
		for i := 0; i < 3; i++ {
			if value, err := strconv.ParseFloat(fields[i], 32); err == nil {
				color[i] = float32(max(0, min(int(value), 255))) / 255
			}
		}
	}
	if len(fields) >= 4 {
		if value, err := strconv.ParseFloat(fields[3], 32); err == nil && value > 0 {
			intensity = float32(value)
		}
	}
	return color, intensity
}

func hl1LightIntensity(raw float32) float32 {
	if raw <= 0 {
		raw = 200
	}
	return maxFloat32(0.5, raw/50)
}

func hl1LightRange(raw float32) float32 {
	if raw <= 0 {
		raw = 200
	}
	return maxFloat32(8, raw*HammerUnitMeters*5.0)
}

func hl1LightSourceRadius(raw float32) float32 {
	if raw <= 0 {
		raw = 200
	}
	return minFloat32(0.5, maxFloat32(0.12, raw*HammerUnitMeters*0.02))
}

func hl1SpotConeAngle(entity importcommon.Entity) float32 {
	if value, ok := parseEntityFloat(entity, "_cone"); ok && value > 0 {
		return value
	}
	return 45
}

func spotRotationFromDirection(dir mgl32.Vec3) mgl32.Quat {
	if dir.LenSqr() <= 1e-8 {
		return mgl32.QuatIdent()
	}
	return mgl32.QuatBetweenVectors(mgl32.Vec3{0, -1, 0}, dir.Normalize())
}

func nudgeHL1LightPosition(position importcommon.Vec3, direction mgl32.Vec3, className string) mgl32.Vec3 {
	out := mgl32.Vec3{position.X, position.Y, position.Z}
	if direction.LenSqr() <= 1e-8 {
		direction = mgl32.Vec3{0, -1, 0}
	}
	offset := float32(0.12)
	if strings.EqualFold(className, "light_spot") {
		offset = 0.22
	}
	return out.Add(direction.Normalize().Mul(offset))
}

func hl1SpotDirectionGekko(entity importcommon.Entity) mgl32.Vec3 {
	pitch := float32(-90)
	yaw := float32(0)
	if angles, ok := parseEntityVec3(entity, "angles"); ok {
		pitch = angles.X
		yaw = angles.Y
	}
	if value, ok := parseEntityFloat(entity, "pitch"); ok {
		pitch = value
	}
	if value, ok := parseEntityFloat(entity, "angle"); ok {
		yaw = value
	}
	pitchRad := float64(pitch) * math.Pi / 180
	yawRad := float64(yaw) * math.Pi / 180
	hammerDir := importcommon.Vec3{
		X: float32(math.Cos(pitchRad) * math.Cos(yawRad)),
		Y: float32(math.Cos(pitchRad) * math.Sin(yawRad)),
		Z: float32(math.Sin(pitchRad)),
	}
	gekkoDir := hammerVectorToGekko(hammerDir)
	return mgl32.Vec3{gekkoDir.X, gekkoDir.Y, gekkoDir.Z}
}

func parseEntityVec3(entity importcommon.Entity, key string) (importcommon.Vec3, bool) {
	if entity.KeyValues == nil {
		return importcommon.Vec3{}, false
	}
	return parseVec3(entity.KeyValues[key])
}

func parseEntityFloat(entity importcommon.Entity, key string) (float32, bool) {
	if entity.KeyValues == nil {
		return 0, false
	}
	return parseFloat32(entity.KeyValues[key])
}

func quatDefFromMGL(q mgl32.Quat) content.Quat {
	if q == (mgl32.Quat{}) {
		q = mgl32.QuatIdent()
	}
	q = q.Normalize()
	return content.Quat{q.V.X(), q.V.Y(), q.V.Z(), q.W}
}

func relativeOrBase(baseDir string, target string) string {
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		return filepath.Base(target)
	}
	return rel
}
