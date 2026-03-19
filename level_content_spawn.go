package gekko

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

const DefaultRuntimeMaxVolumeInstances = 2048

type AuthoredLevelSpawnOptions struct {
	LevelPath          string
	MaxVolumeInstances int
	TerrainGroupID     uint32
}

type AuthoredLevelSpawnResult struct {
	RootEntity              EntityId
	LevelID                 string
	PlacementRootEntities   map[string]EntityId
	TerrainChunkEntities    map[string]EntityId
	ExpandedVolumeInstances []content.PlacementVolumePreviewInstance
}

func LoadAndSpawnAuthoredLevel(path string, cmd *Commands, assets *AssetServer, loader *RuntimeContentLoader, opts AuthoredLevelSpawnOptions) (AuthoredLevelSpawnResult, error) {
	if strings.TrimSpace(path) == "" {
		return AuthoredLevelSpawnResult{}, fmt.Errorf("level path is empty")
	}
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}
	def, err := loader.LoadLevel(path)
	if err != nil {
		return AuthoredLevelSpawnResult{}, err
	}
	if opts.LevelPath == "" {
		opts.LevelPath = path
	}
	return SpawnAuthoredLevel(cmd, assets, loader, def, opts)
}

func SpawnAuthoredLevel(cmd *Commands, assets *AssetServer, loader *RuntimeContentLoader, def *content.LevelDef, opts AuthoredLevelSpawnOptions) (AuthoredLevelSpawnResult, error) {
	result := AuthoredLevelSpawnResult{
		PlacementRootEntities: make(map[string]EntityId),
		TerrainChunkEntities:  make(map[string]EntityId),
	}
	if cmd == nil {
		return result, fmt.Errorf("commands is nil")
	}
	if def == nil {
		return result, fmt.Errorf("level definition is nil")
	}
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}
	if validation := content.ValidateLevel(def, content.LevelValidationOptions{DocumentPath: opts.LevelPath}); validation.HasErrors() {
		return result, fmt.Errorf("level validation failed: %s", validation.Error())
	}

	result.LevelID = def.ID
	result.RootEntity = cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LocalTransformComponent{
			Position: mgl32.Vec3{0, 0, 0},
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&AuthoredLevelRootComponent{LevelID: def.ID},
	)

	spawnPlacement := func(placementID string, volumeID string, assetPath string, transform content.LevelTransformDef) error {
		resolvedAssetPath := content.ResolveDocumentPath(assetPath, opts.LevelPath)
		assetDef, err := loader.LoadAsset(resolvedAssetPath)
		if err != nil {
			return fmt.Errorf("load asset %s: %w", assetPath, err)
		}
		spawnResult, err := SpawnAuthoredAssetWithOptions(cmd, assets, assetDef, levelTransformToComponent(transform), AuthoredAssetSpawnOptions{
			DocumentPath: resolvedAssetPath,
		})
		if err != nil {
			return fmt.Errorf("spawn asset %s for placement %s: %w", assetPath, placementID, err)
		}
		cmd.AddComponents(
			spawnResult.RootEntity,
			&Parent{Entity: result.RootEntity},
			&AuthoredLevelPlacementRefComponent{
				LevelID:     def.ID,
				PlacementID: placementID,
				AssetPath:   filepath.Clean(assetPath),
				VolumeID:    volumeID,
			},
		)
		result.PlacementRootEntities[placementID] = spawnResult.RootEntity
		return nil
	}

	for _, placement := range def.Placements {
		if err := spawnPlacement(placement.ID, "", placement.AssetPath, placement.Transform); err != nil {
			return result, err
		}
	}

	maxVolumeInstances := opts.MaxVolumeInstances
	if maxVolumeInstances <= 0 {
		maxVolumeInstances = DefaultRuntimeMaxVolumeInstances
	}
	for _, volumeDef := range def.PlacementVolumes {
		expanded, err := content.ExpandPlacementVolumePreview(volumeDef, content.PlacementVolumeExpandOptions{
			LevelDocumentPath: opts.LevelPath,
			MaxInstances:      maxVolumeInstances,
		})
		if err != nil {
			return result, fmt.Errorf("expand placement volume %s: %w", volumeDef.ID, err)
		}
		result.ExpandedVolumeInstances = append(result.ExpandedVolumeInstances, expanded.Instances...)
		for index, instance := range expanded.Instances {
			placementID := fmt.Sprintf("%s:%d", volumeDef.ID, index)
			assetPath := authoredPathForLevel(instance.AssetPath, opts.LevelPath)
			if err := spawnPlacement(placementID, volumeDef.ID, assetPath, instance.Transform); err != nil {
				return result, err
			}
		}
	}

	if def.Terrain != nil && strings.TrimSpace(def.Terrain.ManifestPath) != "" {
		if err := spawnAuthoredTerrain(cmd, assets, loader, result.RootEntity, def, opts, &result); err != nil {
			return result, err
		}
	}

	applyLevelEnvironment(cmd, def.Environment)
	cmd.app.FlushCommands()
	TransformHierarchySystem(cmd)
	return result, nil
}

func spawnAuthoredTerrain(cmd *Commands, assets *AssetServer, loader *RuntimeContentLoader, rootEntity EntityId, def *content.LevelDef, opts AuthoredLevelSpawnOptions, result *AuthoredLevelSpawnResult) error {
	manifestPath := content.ResolveDocumentPath(def.Terrain.ManifestPath, opts.LevelPath)
	manifest, err := loader.LoadTerrainChunkManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load terrain manifest %s: %w", def.Terrain.ManifestPath, err)
	}

	terrainGroupID := opts.TerrainGroupID
	if terrainGroupID == 0 {
		terrainGroupID = stableTerrainGroupID(def.ID, manifest.TerrainID)
	}
	palette := AssetId{}
	if assets != nil {
		palette = assets.CreateSimplePalette([4]uint8{120, 120, 120, 255})
	}

	for _, entry := range manifest.Entries {
		if entry.NonEmptyVoxelCount == 0 {
			continue
		}
		chunkPath := content.ResolveTerrainChunkPath(entry, manifestPath)
		chunk, err := loader.LoadTerrainChunk(chunkPath)
		if err != nil {
			return fmt.Errorf("load terrain chunk %s: %w", entry.ChunkPath, err)
		}
		if chunk.NonEmptyVoxelCount == 0 {
			continue
		}

		chunkMap := terrainChunkToXBrickMap(chunk)
		entity := cmd.AddEntity(
			&TransformComponent{
				Position: terrainChunkPosition(chunk),
				Rotation: mgl32.QuatIdent(),
				Scale:    mgl32.Vec3{chunk.VoxelResolution, chunk.VoxelResolution, chunk.VoxelResolution},
			},
			&LocalTransformComponent{
				Position: terrainChunkPosition(chunk),
				Rotation: mgl32.QuatIdent(),
				Scale:    mgl32.Vec3{chunk.VoxelResolution, chunk.VoxelResolution, chunk.VoxelResolution},
			},
			&Parent{Entity: rootEntity},
			&VoxelModelComponent{
				VoxelPalette:      palette,
				PivotMode:         PivotModeCorner,
				CustomMap:         chunkMap,
				IsTerrainChunk:    true,
				TerrainGroupID:    terrainGroupID,
				TerrainChunkCoord: [3]int{chunk.Coord.X, chunk.Coord.Y, chunk.Coord.Z},
				TerrainChunkSize:  chunk.ChunkSize,
			},
			&AuthoredTerrainChunkRefComponent{
				LevelID:    def.ID,
				TerrainID:  manifest.TerrainID,
				ChunkCoord: [3]int{chunk.Coord.X, chunk.Coord.Y, chunk.Coord.Z},
			},
		)
		result.TerrainChunkEntities[content.TerrainChunkKey(chunk.Coord)] = entity
	}

	return nil
}

func terrainChunkToXBrickMap(chunk *content.TerrainChunkDef) *volume.XBrickMap {
	xbm := volume.NewXBrickMap()
	if chunk == nil {
		return xbm
	}
	for _, column := range chunk.Columns {
		for y := 0; y < column.FilledVoxels; y++ {
			xbm.SetVoxel(column.X, y, column.Z, chunk.SolidValue)
		}
	}
	return xbm
}

func terrainChunkPosition(chunk *content.TerrainChunkDef) mgl32.Vec3 {
	if chunk == nil {
		return mgl32.Vec3{}
	}
	chunkWorldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution * VoxelSize
	return mgl32.Vec3{
		float32(chunk.Coord.X) * chunkWorldSize,
		float32(chunk.Coord.Y) * chunkWorldSize,
		float32(chunk.Coord.Z) * chunkWorldSize,
	}
}

func authoredPathForLevel(resolvedPath string, levelPath string) string {
	if resolvedPath == "" {
		return ""
	}
	if levelPath == "" {
		return filepath.Clean(resolvedPath)
	}
	return content.AuthorDocumentPath(resolvedPath, levelPath)
}

func levelTransformToComponent(def content.LevelTransformDef) TransformComponent {
	rotation := mgl32.Quat{W: def.Rotation[3], V: mgl32.Vec3{def.Rotation[0], def.Rotation[1], def.Rotation[2]}}
	if rotation == (mgl32.Quat{}) {
		rotation = mgl32.QuatIdent()
	}
	scale := mgl32.Vec3{def.Scale[0], def.Scale[1], def.Scale[2]}
	if scale == (mgl32.Vec3{}) {
		scale = mgl32.Vec3{1, 1, 1}
	}
	return TransformComponent{
		Position: mgl32.Vec3{def.Position[0], def.Position[1], def.Position[2]},
		Rotation: rotation,
		Scale:    scale,
	}
}

func applyLevelEnvironment(cmd *Commands, env *content.LevelEnvironmentDef) {
	if cmd == nil {
		return
	}

	preset := ""
	if env != nil {
		preset = strings.TrimSpace(strings.ToLower(env.Preset))
	}

	ambientIntensity := float32(0.1)
	directionalIntensity := float32(1.5)
	switch preset {
	case "orbit":
		ambientIntensity = 0.12
		directionalIntensity = 1.65
	case "space":
		ambientIntensity = 0.08
		directionalIntensity = 1.35
	case "":
	default:
	}

	cmd.AddEntity(
		&LightComponent{
			Type:      LightTypeAmbient,
			Intensity: ambientIntensity,
			Color:     [3]float32{1, 1, 1},
			Range:     40,
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 100, 0},
			Rotation: mgl32.QuatRotate(mgl32.DegToRad(-45), mgl32.Vec3{1, 0, 0}),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LightComponent{
			Type:      LightTypeDirectional,
			Intensity: directionalIntensity,
			Color:     [3]float32{1, 1, 1},
			Range:     1000,
		},
	)
}

func stableTerrainGroupID(levelID string, terrainID string) uint32 {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(levelID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(terrainID))
	value := hasher.Sum32()
	if value == 0 {
		return 1
	}
	return value
}
