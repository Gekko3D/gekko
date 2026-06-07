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
const DefaultMaxEmissiveSurfaceLights = 64

const minEmissiveSurfaceLightVoxels = 8

type GeneratedLevelResult struct {
	LevelPath          string
	Level              *content.LevelDef
	LightFixtureAssets []GeneratedAssetResult
}

type GeneratedAssetResult struct {
	AssetPath string
	Asset     *content.AssetDef
}

func BuildGeneratedLevel(opts ImportOptions, summary ImportSummary, manifestPath string, voxelizedWorld ...VoxelizeResult) (GeneratedLevelResult, error) {
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
		return GeneratedLevelResult{LevelPath: filepath.Clean(levelPath), Level: level, LightFixtureAssets: assets}, nil
	}
	content.EnsureLevelIDs(level)
	return GeneratedLevelResult{LevelPath: filepath.Clean(levelPath), Level: level}, nil
}

func SaveGeneratedLevel(result GeneratedLevelResult) error {
	if result.Level == nil {
		return fmt.Errorf("level is nil")
	}
	for _, asset := range result.LightFixtureAssets {
		if asset.Asset == nil {
			return fmt.Errorf("light fixture asset is nil")
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
