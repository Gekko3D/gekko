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
	MarkerEntities          map[string]EntityId
	ExpandedVolumeInstances []content.PlacementVolumePreviewInstance
}

type AuthoredPlacementSpawnDef struct {
	PlacementID string
	VolumeID    string
	AssetPath   string
	Transform   content.LevelTransformDef
}

type AuthoredTerrainSpawnDef struct {
	LevelID        string
	TerrainID      string
	TerrainGroupID uint32
	Chunk          *content.TerrainChunkDef
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
		MarkerEntities:        make(map[string]EntityId),
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

	for _, placement := range def.Placements {
		placementResult, err := spawnAuthoredLevelPlacement(cmd, assets, loader, result.RootEntity, def.ID, opts.LevelPath, AuthoredPlacementSpawnDef{
			PlacementID: placement.ID,
			AssetPath:   placement.AssetPath,
			Transform:   placement.Transform,
		})
		if err != nil {
			return result, err
		}
		result.PlacementRootEntities[placement.ID] = placementResult.RootEntity
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
			placementResult, err := spawnAuthoredLevelPlacement(cmd, assets, loader, result.RootEntity, def.ID, opts.LevelPath, AuthoredPlacementSpawnDef{
				PlacementID: placementID,
				VolumeID:    volumeDef.ID,
				AssetPath:   assetPath,
				Transform:   instance.Transform,
			})
			if err != nil {
				return result, err
			}
			result.PlacementRootEntities[placementID] = placementResult.RootEntity
		}
	}
	for _, marker := range def.Markers {
		entity := spawnAuthoredLevelMarker(cmd, result.RootEntity, def.ID, marker)
		result.MarkerEntities[marker.ID] = entity
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

func spawnAuthoredLevelMarker(cmd *Commands, parent EntityId, levelID string, marker content.LevelMarkerDef) EntityId {
	transform := levelTransformToComponent(marker.Transform)
	transform.Scale = mgl32.Vec3{1, 1, 1}
	return cmd.AddEntity(
		&transform,
		&LocalTransformComponent{
			Position: transform.Position,
			Rotation: transform.Rotation,
			Scale:    transform.Scale,
		},
		&Parent{Entity: parent},
		&AuthoredMarkerComponent{Kind: marker.Kind, Tags: append([]string(nil), marker.Tags...)},
		&AuthoredLevelMarkerRefComponent{
			LevelID:  levelID,
			MarkerID: marker.ID,
			Name:     marker.Name,
			Kind:     marker.Kind,
		},
	)
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

		entity := spawnAuthoredTerrainChunkEntity(cmd, rootEntity, palette, AuthoredTerrainSpawnDef{
			LevelID:        def.ID,
			TerrainID:      manifest.TerrainID,
			TerrainGroupID: terrainGroupID,
			Chunk:          chunk,
		})
		result.TerrainChunkEntities[content.TerrainChunkKey(chunk.Coord)] = entity
	}

	return nil
}

func spawnAuthoredLevelPlacement(cmd *Commands, assets *AssetServer, loader *RuntimeContentLoader, parent EntityId, levelID string, levelPath string, placement AuthoredPlacementSpawnDef) (AuthoredAssetSpawnResult, error) {
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}
	resolvedAssetPath := content.ResolveDocumentPath(placement.AssetPath, levelPath)
	assetDef, err := loader.LoadAsset(resolvedAssetPath)
	if err != nil {
		return AuthoredAssetSpawnResult{}, fmt.Errorf("load asset %s: %w", placement.AssetPath, err)
	}
	spawnResult, err := SpawnAuthoredAssetWithOptions(cmd, assets, assetDef, levelTransformToComponent(placement.Transform), AuthoredAssetSpawnOptions{
		DocumentPath: resolvedAssetPath,
	})
	if err != nil {
		return AuthoredAssetSpawnResult{}, fmt.Errorf("spawn asset %s for placement %s: %w", placement.AssetPath, placement.PlacementID, err)
	}
	cmd.AddComponents(
		spawnResult.RootEntity,
		&Parent{Entity: parent},
		&AuthoredLevelPlacementRefComponent{
			LevelID:     levelID,
			PlacementID: placement.PlacementID,
			AssetPath:   filepath.Clean(placement.AssetPath),
			VolumeID:    placement.VolumeID,
		},
	)
	for itemID, eid := range spawnResult.EntitiesByAssetID {
		cmd.AddComponents(eid, &AuthoredLevelItemRefComponent{
			LevelID:     levelID,
			PlacementID: placement.PlacementID,
			ItemID:      itemID,
			AssetID:     spawnResult.AssetID,
			AssetPath:   filepath.Clean(placement.AssetPath),
			VolumeID:    placement.VolumeID,
		})
	}
	return spawnResult, nil
}

func spawnAuthoredTerrainChunkEntity(cmd *Commands, parent EntityId, palette AssetId, terrain AuthoredTerrainSpawnDef) EntityId {
	chunkMap := terrainChunkToXBrickMap(terrain.Chunk)
	return cmd.AddEntity(
		&TransformComponent{
			Position: terrainChunkPosition(terrain.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{terrain.Chunk.VoxelResolution, terrain.Chunk.VoxelResolution, terrain.Chunk.VoxelResolution},
		},
		&LocalTransformComponent{
			Position: terrainChunkPosition(terrain.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{terrain.Chunk.VoxelResolution, terrain.Chunk.VoxelResolution, terrain.Chunk.VoxelResolution},
		},
		&Parent{Entity: parent},
		&VoxelModelComponent{
			VoxelPalette:      palette,
			PivotMode:         PivotModeCorner,
			CustomMap:         chunkMap,
			IsTerrainChunk:    true,
			TerrainGroupID:    terrain.TerrainGroupID,
			TerrainChunkCoord: [3]int{terrain.Chunk.Coord.X, terrain.Chunk.Coord.Y, terrain.Chunk.Coord.Z},
			TerrainChunkSize:  terrain.Chunk.ChunkSize,
		},
		&AuthoredTerrainChunkRefComponent{
			LevelID:    terrain.LevelID,
			TerrainID:  terrain.TerrainID,
			ChunkCoord: [3]int{terrain.Chunk.Coord.X, terrain.Chunk.Coord.Y, terrain.Chunk.Coord.Z},
		},
	)
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
