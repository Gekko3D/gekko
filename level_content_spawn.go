package gekko

import (
	"encoding/json"
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
	BrushRootEntities       map[string]EntityId
	TerrainChunkEntities    map[string]EntityId
	MarkerEntities          map[string]EntityId
	ExpandedVolumeInstances []content.PlacementVolumePreviewInstance
}

type AuthoredPlacementSpawnDef struct {
	PlacementID                    string
	VolumeID                       string
	AssetPath                      string
	Transform                      content.LevelTransformDef
	OverrideCastShadows            *bool
	OverrideShadowMaxDistance      *float32
	OverrideShadowCasterGroupID    uint64
	OverrideShadowCasterGroupLimit *int
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
		BrushRootEntities:     make(map[string]EntityId),
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
	brushes := content.LevelBrushes(def)
	if len(brushes) > 0 {
		brushRoot, err := spawnAuthoredLevelBrushes(cmd, assets, result.RootEntity, def)
		if err != nil {
			return result, err
		}
		for _, brush := range brushes {
			result.BrushRootEntities[brush.ID] = brushRoot
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
			placementResult, err := spawnAuthoredLevelPlacement(cmd, assets, loader, result.RootEntity, def.ID, opts.LevelPath, AuthoredPlacementSpawnDef{
				PlacementID:                    placementID,
				VolumeID:                       volumeDef.ID,
				AssetPath:                      assetPath,
				Transform:                      instance.Transform,
				OverrideCastShadows:            volumeDef.CastsShadows,
				OverrideShadowMaxDistance:      optionalPositiveFloat32Pointer(volumeDef.ShadowMaxDistance),
				OverrideShadowCasterGroupID:    stablePlacementVolumeShadowGroupID(def.ID, volumeDef.ID),
				OverrideShadowCasterGroupLimit: optionalPositiveIntPointer(volumeDef.MaxShadowCasters),
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

		entity := spawnAuthoredTerrainChunkEntity(cmd, assets, rootEntity, palette, AuthoredTerrainSpawnDef{
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
		DocumentPath:                   resolvedAssetPath,
		OverrideCastShadows:            placement.OverrideCastShadows,
		OverrideShadowMaxDistance:      placement.OverrideShadowMaxDistance,
		OverrideShadowCasterGroupID:    placement.OverrideShadowCasterGroupID,
		OverrideShadowCasterGroupLimit: placement.OverrideShadowCasterGroupLimit,
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

func spawnAuthoredLevelBrushes(cmd *Commands, assets *AssetServer, parent EntityId, level *content.LevelDef) (EntityId, error) {
	build, err := buildCollapsedAuthoredLevelBrushes(assets, level)
	if err != nil {
		return 0, fmt.Errorf("spawn level brushes: %w", err)
	}
	entity := cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LocalTransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&Parent{Entity: parent},
	)
	for _, batch := range build.batches {
		cmd.AddEntity(
			&TransformComponent{
				Rotation: mgl32.QuatIdent(),
				Scale:    mgl32.Vec3{1, 1, 1},
			},
			&LocalTransformComponent{
				Rotation: mgl32.QuatIdent(),
				Scale:    mgl32.Vec3{1, 1, 1},
			},
			&Parent{Entity: entity},
			&VoxelModelComponent{
				OverrideGeometry: batch.geometry,
				VoxelPalette:     batch.palette,
				VoxelResolution:  build.voxelResolution,
				PivotMode:        PivotModeCorner,
			},
		)
	}
	return entity, nil
}

type collapsedLevelBrushBuild struct {
	batches         []authoredLevelBrushBakeBatch
	palette         AssetId
	voxelResolution float32
}

type levelBrushBakeResolveCache struct {
	models   map[string]AssetId
	palettes map[string]AssetId
}

type AuthoredLevelBrushBakeResult struct {
	Batches         []AuthoredLevelBrushBakeBatch
	Palette         AssetId
	VoxelResolution float32
}

type AuthoredLevelBrushBakeBatch struct {
	Geometry AssetId
	Palette  AssetId
}

func BakeAuthoredLevelBrushes(assets *AssetServer, level *content.LevelDef) (AuthoredLevelBrushBakeResult, error) {
	build, err := buildCollapsedAuthoredLevelBrushes(assets, level)
	if err != nil {
		return AuthoredLevelBrushBakeResult{}, err
	}
	return AuthoredLevelBrushBakeResult{
		Batches:         cloneAuthoredLevelBrushBakeBatches(build.batches),
		Palette:         build.palette,
		VoxelResolution: build.voxelResolution,
	}, nil
}

func buildCollapsedAuthoredLevelBrushes(assets *AssetServer, level *content.LevelDef) (collapsedLevelBrushBuild, error) {
	result := collapsedLevelBrushBuild{}
	if assets == nil {
		return result, fmt.Errorf("level brush bake requires asset server")
	}
	if level == nil {
		return result, fmt.Errorf("level definition is nil")
	}
	content.EnsureLevelIDs(level)
	brushes := content.LevelBrushes(level)
	if len(brushes) == 0 {
		return result, fmt.Errorf("level has no brushes")
	}

	voxelResolution := level.VoxelResolution
	if voxelResolution <= 0 {
		voxelResolution = VoxelSize
	}

	cache := levelBrushBakeResolveCache{
		models:   make(map[string]AssetId),
		palettes: make(map[string]AssetId),
	}
	resolved := make([]levelBrushResolvedPart, 0, len(brushes))
	for _, brush := range brushes {
		model, err := cachedLevelBrushModelAsset(assets, &cache, brush)
		if err != nil {
			return result, fmt.Errorf("brush %s: %w", brush.ID, err)
		}
		geometry, ok := assets.GetVoxelGeometry(model)
		if !ok {
			return result, fmt.Errorf("brush %s: missing voxel geometry", brush.ID)
		}
		paletteID, err := cachedAuthoredLevelBrushPalette(assets, &cache, level, brush)
		if err != nil {
			return result, fmt.Errorf("brush %s: %w", brush.ID, err)
		}
		palette, ok := assets.GetVoxelPalette(paletteID)
		if !ok {
			return result, fmt.Errorf("brush %s: missing voxel palette", brush.ID)
		}
		paletteKey, err := authoredLevelBrushPaletteCacheKey(level, brush)
		if err != nil {
			return result, fmt.Errorf("brush %s: %w", brush.ID, err)
		}
		resolved = append(resolved, levelBrushResolvedPart{
			brush:    brush,
			brushKey: paletteKey,
			part: authoredCollapseResolvedPart{
				def: content.AssetPartDef{
					ID:   brush.ID,
					Name: brush.Name,
					Source: content.AssetSourceDef{
						Kind:       content.AssetSourceKindProceduralPrimitive,
						Primitive:  brush.Primitive,
						Params:     brush.Params,
						MaterialID: brush.MaterialID,
						Operation:  brush.Operation,
					},
				},
				world:           levelTransformToComponent(brush.Transform),
				geometry:        geometry,
				paletteAsset:    palette,
				voxelResolution: voxelResolution,
			},
		})
	}

	batches, err := bakeLevelBrushBatches(assets, level, resolved, voxelResolution)
	if err != nil {
		return result, err
	}
	if len(batches) == 0 {
		return result, fmt.Errorf("level brush bake requires at least one non-empty additive brush")
	}
	result.batches = batches
	if len(batches) == 1 {
		result.palette = batches[0].palette
	}
	result.voxelResolution = voxelResolution
	return result, nil
}

func cachedLevelBrushModelAsset(assets *AssetServer, cache *levelBrushBakeResolveCache, brush content.LevelBrushDef) (AssetId, error) {
	key, err := levelBrushModelCacheKey(brush)
	if err != nil {
		return AssetId{}, err
	}
	if cache != nil && cache.models != nil {
		if id, ok := cache.models[key]; ok {
			return id, nil
		}
	}
	id, err := levelBrushModelAsset(assets, brush)
	if err != nil {
		return AssetId{}, err
	}
	if cache != nil && cache.models != nil {
		cache.models[key] = id
	}
	return id, nil
}

func cachedAuthoredLevelBrushPalette(assets *AssetServer, cache *levelBrushBakeResolveCache, level *content.LevelDef, brush content.LevelBrushDef) (AssetId, error) {
	key, err := authoredLevelBrushPaletteCacheKey(level, brush)
	if err != nil {
		return AssetId{}, err
	}
	if cache != nil && cache.palettes != nil {
		if id, ok := cache.palettes[key]; ok {
			return id, nil
		}
	}
	id, err := authoredLevelBrushPalette(assets, level, brush)
	if err != nil {
		return AssetId{}, err
	}
	if cache != nil && cache.palettes != nil {
		cache.palettes[key] = id
	}
	return id, nil
}

func levelBrushModelCacheKey(brush content.LevelBrushDef) (string, error) {
	payload := struct {
		Primitive string
		Params    map[string]float32
	}{
		Primitive: brush.Primitive,
		Params:    brush.Params,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func authoredLevelBrushPaletteCacheKey(level *content.LevelDef, brush content.LevelBrushDef) (string, error) {
	payload := struct {
		MaterialID string
		Material   *content.LevelMaterialDef
	}{
		MaterialID: brush.MaterialID,
	}
	if brush.MaterialID != "" {
		material, ok := content.FindLevelMaterialByID(level, brush.MaterialID)
		if !ok {
			return "", fmt.Errorf("missing material %s", brush.MaterialID)
		}
		payload.Material = &material
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func levelBrushModelAsset(assets *AssetServer, brush content.LevelBrushDef) (AssetId, error) {
	if assets == nil {
		return AssetId{}, nil
	}
	params := brush.Params
	switch brush.Primitive {
	case "cube":
		return assets.CreateCubeModel(params["sx"], params["sy"], params["sz"], 1), nil
	case "sphere":
		return assets.CreateSphereModel(params["radius"], 1), nil
	case "cone":
		return assets.CreateConeModel(params["radius"], params["height"], 1), nil
	case "pyramid":
		return assets.CreatePyramidModel(params["size"], params["height"], 1), nil
	case "cylinder":
		return assets.CreateCylinderModel(params["radius"], params["height"], 1), nil
	case "capsule":
		return assets.CreateCapsuleModel(params["radius"], params["height"], 1), nil
	case "ramp":
		return assets.CreateRampModel(params["sx"], params["sy"], params["sz"], 1), nil
	default:
		return AssetId{}, fmt.Errorf("unsupported level brush primitive %q", brush.Primitive)
	}
}

func authoredLevelBrushPalette(assets *AssetServer, level *content.LevelDef, brush content.LevelBrushDef) (AssetId, error) {
	if assets == nil {
		return AssetId{}, nil
	}
	if brush.MaterialID == "" {
		return assets.CreatePBRPalette([4]uint8{255, 255, 255, 255}, 1, 0, 0, 1.5), nil
	}
	material, ok := content.FindLevelMaterialByID(level, brush.MaterialID)
	if !ok {
		return AssetId{}, fmt.Errorf("missing material %s", brush.MaterialID)
	}
	return assets.CreatePBRPaletteWithTransparency(
		material.BaseColor,
		material.Roughness,
		material.Metallic,
		material.Emissive,
		material.IOR,
		material.Transparency,
	), nil
}

func levelBrushCollapseGeometryCacheKey(level *content.LevelDef, voxelResolution float32) (string, error) {
	payload := struct {
		Version         string
		LevelID         string
		Materials       []content.LevelMaterialDef
		BrushLayers     []content.LevelBrushLayerDef
		VoxelResolution float32
	}{
		Version:         "collapsed-authored-level-brushes-v3",
		LevelID:         level.ID,
		Materials:       level.Materials,
		BrushLayers:     level.BrushLayers,
		VoxelResolution: voxelResolution,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

type levelBrushResolvedPart struct {
	brush    content.LevelBrushDef
	brushKey string
	part     authoredCollapseResolvedPart
}

type authoredLevelBrushBatchBuild struct {
	key          string
	palette      AssetId
	composite    *volume.XBrickMap
	geometryKey  string
	brushes      []string
	firstBrushID string
}

func bakeLevelBrushBatches(assets *AssetServer, level *content.LevelDef, resolved []levelBrushResolvedPart, voxelResolution float32) ([]authoredLevelBrushBakeBatch, error) {
	if len(resolved) == 0 {
		return nil, nil
	}
	orderedKeys := make([]string, 0, len(resolved))
	batchesByKey := map[string]*authoredLevelBrushBatchBuild{}
	for _, resolvedPart := range resolved {
		part := resolvedPart.part
		operation := content.EffectiveAssetSourceOperation(part.def.Source)
		switch operation {
		case content.AssetShapeOperationSubtract:
			for _, key := range orderedKeys {
				batch := batchesByKey[key]
				if batch == nil {
					continue
				}
				if err := bakeResolvedPartIntoComposite(batch.composite, part, voxelResolution); err != nil {
					return nil, fmt.Errorf("brush %s bake failed: %w", part.def.ID, err)
				}
			}
		case content.AssetShapeOperationAdd:
			if voxelGeometryIsEmpty(part.geometry) {
				continue
			}
			batch := batchesByKey[resolvedPart.brushKey]
			if batch == nil {
				geometryKey, err := levelBrushBatchGeometryCacheKey(level, voxelResolution, resolvedPart.brushKey)
				if err != nil {
					return nil, err
				}
				batch = &authoredLevelBrushBatchBuild{
					key:          resolvedPart.brushKey,
					palette:      assets.CreateVoxelPaletteAsset(part.paletteAsset),
					composite:    volume.NewXBrickMap(),
					geometryKey:  geometryKey,
					firstBrushID: part.def.ID,
				}
				batchesByKey[resolvedPart.brushKey] = batch
				orderedKeys = append(orderedKeys, resolvedPart.brushKey)
			}
			batch.brushes = append(batch.brushes, part.def.ID)
			clearingPart := levelBrushResolvedPartAsSubtract(part)
			for _, key := range orderedKeys {
				if key == resolvedPart.brushKey {
					continue
				}
				otherBatch := batchesByKey[key]
				if otherBatch == nil {
					continue
				}
				if err := bakeResolvedPartIntoComposite(otherBatch.composite, clearingPart, voxelResolution); err != nil {
					return nil, fmt.Errorf("brush %s overwrite clear failed: %w", part.def.ID, err)
				}
			}
			if err := bakeResolvedPartIntoComposite(batch.composite, part, voxelResolution); err != nil {
				return nil, fmt.Errorf("brush %s bake failed: %w", part.def.ID, err)
			}
		default:
			return nil, fmt.Errorf("brush %s has unsupported operation %q", part.def.ID, part.def.Source.Operation)
		}
	}
	result := make([]authoredLevelBrushBakeBatch, 0, len(orderedKeys))
	for _, key := range orderedKeys {
		batch := batchesByKey[key]
		if batch == nil || batch.composite == nil || batch.composite.GetVoxelCount() == 0 {
			continue
		}
		batch.composite.ComputeAABB()
		batch.composite.ClearDirty()
		result = append(result, authoredLevelBrushBakeBatch{
			geometry: assets.RegisterSharedVoxelGeometryWithCacheKey(batch.geometryKey, batch.composite, batch.geometryKey),
			palette:  batch.palette,
		})
	}
	return result, nil
}

func levelBrushBatchGeometryCacheKey(level *content.LevelDef, voxelResolution float32, batchKey string) (string, error) {
	baseKey, err := levelBrushCollapseGeometryCacheKey(level, voxelResolution)
	if err != nil {
		return "", err
	}
	payload := struct {
		Version  string
		BaseKey  string
		BatchKey string
	}{
		Version:  "collapsed-authored-level-brush-batch-v1",
		BaseKey:  baseKey,
		BatchKey: batchKey,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func cloneAuthoredLevelBrushBakeBatches(in []authoredLevelBrushBakeBatch) []AuthoredLevelBrushBakeBatch {
	if len(in) == 0 {
		return nil
	}
	out := make([]AuthoredLevelBrushBakeBatch, 0, len(in))
	for _, batch := range in {
		out = append(out, AuthoredLevelBrushBakeBatch{
			Geometry: batch.geometry,
			Palette:  batch.palette,
		})
	}
	return out
}

func levelBrushResolvedPartAsSubtract(part authoredCollapseResolvedPart) authoredCollapseResolvedPart {
	clearing := part
	clearing.def = part.def
	clearing.def.Source = part.def.Source
	clearing.def.Source.Operation = content.AssetShapeOperationSubtract
	return clearing
}

type authoredLevelBrushBakeBatch struct {
	geometry AssetId
	palette  AssetId
}

func optionalPositiveFloat32Pointer(value float32) *float32 {
	if value <= 0 {
		return nil
	}
	out := value
	return &out
}

func optionalPositiveIntPointer(value int) *int {
	if value <= 0 {
		return nil
	}
	out := value
	return &out
}

func spawnAuthoredTerrainChunkEntity(cmd *Commands, assets *AssetServer, parent EntityId, palette AssetId, terrain AuthoredTerrainSpawnDef) EntityId {
	chunkMap := terrainChunkToXBrickMap(terrain.Chunk)
	overrideGeometry := AssetId{}
	if assets != nil {
		overrideGeometry = assets.RegisterSharedVoxelGeometry(chunkMap, "")
	}
	return cmd.AddEntity(
		&TransformComponent{
			Position: terrainChunkPosition(terrain.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{terrain.Chunk.VoxelResolution / VoxelSize, terrain.Chunk.VoxelResolution / VoxelSize, terrain.Chunk.VoxelResolution / VoxelSize},
		},
		&LocalTransformComponent{
			Position: terrainChunkPosition(terrain.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{terrain.Chunk.VoxelResolution / VoxelSize, terrain.Chunk.VoxelResolution / VoxelSize, terrain.Chunk.VoxelResolution / VoxelSize},
		},
		&Parent{Entity: parent},
		&VoxelModelComponent{
			VoxelPalette:      palette,
			PivotMode:         PivotModeCorner,
			OverrideGeometry:  overrideGeometry,
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
	chunkWorldSize := float32(chunk.ChunkSize) * chunk.VoxelResolution
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

type environmentPresetConfig struct {
	ambientIntensity     float32
	directionalIntensity float32
	ambientColor         [3]float32
	directionalColor     [3]float32
	directionalRotation  mgl32.Quat
	skyAmbientMix        float32
	skySun               *SkyboxSunComponent
	spawnSkybox          func(*Commands)
}

func canonicalEnvironmentPresetName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = strings.ReplaceAll(name, "-", "")
	name = strings.ReplaceAll(name, "_", "")
	return name
}

func directionalLightDirection(rotation mgl32.Quat) mgl32.Vec3 {
	return rotation.Rotate(mgl32.Vec3{1, -1, 0}.Normalize())
}

func environmentPreset(name string) environmentPresetConfig {
	daylightRotation := mgl32.QuatRotate(mgl32.DegToRad(-50), mgl32.Vec3{1, 0, 0}).
		Mul(mgl32.QuatRotate(mgl32.DegToRad(20), mgl32.Vec3{0, 1, 0}))
	daylightDir := directionalLightDirection(daylightRotation)

	cfg := environmentPresetConfig{
		ambientIntensity:     0.1,
		directionalIntensity: 1.5,
		ambientColor:         [3]float32{1, 1, 1},
		directionalColor:     [3]float32{1, 1, 1},
		directionalRotation:  mgl32.QuatRotate(mgl32.DegToRad(-45), mgl32.Vec3{1, 0, 0}),
		skyAmbientMix:        0.60,
	}

	switch canonicalEnvironmentPresetName(name) {
	case "orbit":
		cfg.ambientIntensity = 0.26
		cfg.directionalIntensity = 1.55
	case "space":
		cfg.ambientIntensity = 0.08
		cfg.directionalIntensity = 1.35
	case "", "daylight":
		cfg.ambientIntensity = 0.22
		cfg.directionalIntensity = 1.85
		cfg.ambientColor = [3]float32{0.78, 0.84, 0.92}
		cfg.directionalColor = [3]float32{1.0, 0.95, 0.86}
		cfg.directionalRotation = daylightRotation
		cfg.skyAmbientMix = 0.72
		cfg.skySun = &SkyboxSunComponent{
			Direction:              daylightDir,
			Intensity:              1.2,
			HaloColor:              mgl32.Vec3{1.0, 0.92, 0.78},
			CoreGlowStrength:       2.0,
			CoreGlowExponent:       1000.0,
			AtmosphereExponent:     100.0,
			AtmosphereGlowStrength: 0.5,
			DiskColor:              mgl32.Vec3{1.5, 1.4, 1.2},
			DiskStrength:           1.0,
			DiskStart:              0.9998,
			DiskEnd:                0.9999,
		}
		cfg.spawnSkybox = spawnDaylightSkybox
	case "fullmoonnight":
		moonRotation := mgl32.QuatRotate(mgl32.DegToRad(-18), mgl32.Vec3{1, 0, 0}).
			Mul(mgl32.QuatRotate(mgl32.DegToRad(140), mgl32.Vec3{0, 1, 0}))
		moonDir := directionalLightDirection(moonRotation)
		cfg.ambientIntensity = 0.0005
		cfg.directionalIntensity = 0.085
		cfg.ambientColor = [3]float32{0.05, 0.07, 0.11}
		cfg.directionalColor = [3]float32{0.22, 0.26, 0.34}
		cfg.directionalRotation = moonRotation
		cfg.skyAmbientMix = 0.025
		cfg.skySun = &SkyboxSunComponent{
			Direction:              moonDir,
			Intensity:              0.5,
			HaloColor:              mgl32.Vec3{0.64, 0.7, 0.84},
			CoreGlowStrength:       0.2,
			CoreGlowExponent:       1800.0,
			AtmosphereExponent:     220.0,
			AtmosphereGlowStrength: 0.015,
			DiskColor:              mgl32.Vec3{0.92, 0.95, 1.02},
			DiskStrength:           1.7,
			DiskStart:              0.99918,
			DiskEnd:                0.99968,
		}
		cfg.spawnSkybox = spawnFullmoonNightSkybox
	case "fullmoonnightgi":
		moonRotation := mgl32.QuatRotate(mgl32.DegToRad(-18), mgl32.Vec3{1, 0, 0}).
			Mul(mgl32.QuatRotate(mgl32.DegToRad(140), mgl32.Vec3{0, 1, 0}))
		moonDir := directionalLightDirection(moonRotation)
		cfg.ambientIntensity = 0.004
		cfg.directionalIntensity = 0.16
		cfg.ambientColor = [3]float32{0.08, 0.1, 0.15}
		cfg.directionalColor = [3]float32{0.26, 0.3, 0.4}
		cfg.directionalRotation = moonRotation
		cfg.skyAmbientMix = 0.08
		cfg.skySun = &SkyboxSunComponent{
			Direction:              moonDir,
			Intensity:              0.58,
			HaloColor:              mgl32.Vec3{0.66, 0.72, 0.86},
			CoreGlowStrength:       0.24,
			CoreGlowExponent:       1800.0,
			AtmosphereExponent:     220.0,
			AtmosphereGlowStrength: 0.03,
			DiskColor:              mgl32.Vec3{0.94, 0.97, 1.04},
			DiskStrength:           1.75,
			DiskStart:              0.99918,
			DiskEnd:                0.99968,
		}
		cfg.spawnSkybox = spawnFullmoonNightSkybox
	case "sunsetdusk":
		sunsetRotation := mgl32.QuatRotate(mgl32.DegToRad(-9), mgl32.Vec3{1, 0, 0}).
			Mul(mgl32.QuatRotate(mgl32.DegToRad(28), mgl32.Vec3{0, 1, 0}))
		sunsetDir := directionalLightDirection(sunsetRotation)
		cfg.ambientIntensity = 0.12
		cfg.directionalIntensity = 0.95
		cfg.ambientColor = [3]float32{0.36, 0.28, 0.34}
		cfg.directionalColor = [3]float32{1.0, 0.62, 0.36}
		cfg.directionalRotation = sunsetRotation
		cfg.skyAmbientMix = 0.68
		cfg.skySun = &SkyboxSunComponent{
			Direction:              sunsetDir,
			Intensity:              1.0,
			HaloColor:              mgl32.Vec3{1.0, 0.62, 0.34},
			CoreGlowStrength:       2.4,
			CoreGlowExponent:       800.0,
			AtmosphereExponent:     45.0,
			AtmosphereGlowStrength: 1.1,
			DiskColor:              mgl32.Vec3{1.36, 0.9, 0.48},
			DiskStrength:           1.1,
			DiskStart:              0.99972,
			DiskEnd:                0.9999,
		}
		cfg.spawnSkybox = spawnSunsetDuskSkybox
	case "stormovercast":
		stormRotation := mgl32.QuatRotate(mgl32.DegToRad(-62), mgl32.Vec3{1, 0, 0}).
			Mul(mgl32.QuatRotate(mgl32.DegToRad(-12), mgl32.Vec3{0, 1, 0}))
		stormDir := directionalLightDirection(stormRotation)
		cfg.ambientIntensity = 0.16
		cfg.directionalIntensity = 0.55
		cfg.ambientColor = [3]float32{0.42, 0.46, 0.52}
		cfg.directionalColor = [3]float32{0.74, 0.78, 0.82}
		cfg.directionalRotation = stormRotation
		cfg.skyAmbientMix = 0.78
		cfg.skySun = &SkyboxSunComponent{
			Direction:              stormDir,
			Intensity:              0.28,
			HaloColor:              mgl32.Vec3{0.84, 0.88, 0.92},
			CoreGlowStrength:       0.45,
			CoreGlowExponent:       520.0,
			AtmosphereExponent:     30.0,
			AtmosphereGlowStrength: 0.22,
			DiskColor:              mgl32.Vec3{0.94, 0.96, 1.0},
			DiskStrength:           0.18,
			DiskStart:              0.99982,
			DiskEnd:                0.99994,
		}
		cfg.spawnSkybox = spawnStormOvercastSkybox
	}

	return cfg
}

func applyLevelEnvironment(cmd *Commands, env *content.LevelEnvironmentDef) {
	if cmd == nil {
		return
	}

	// Missing or empty environment means authored content owns the sky and lighting.
	if env == nil || env.Preset == "" {
		return
	}
	cfg := environmentPreset(env.Preset)

	cmd.AddEntity(
		&LightComponent{
			Type:      LightTypeAmbient,
			Intensity: cfg.ambientIntensity,
			Color:     cfg.ambientColor,
			Range:     40,
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Position: mgl32.Vec3{0, 100, 0},
			Rotation: cfg.directionalRotation,
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LightComponent{
			Type:      LightTypeDirectional,
			Intensity: cfg.directionalIntensity,
			Color:     cfg.directionalColor,
			Range:     1000,
		},
	)

	cmd.AddEntity(&SkyAmbientComponent{SkyMix: cfg.skyAmbientMix})
	if cfg.skySun != nil {
		cmd.AddEntity(cfg.skySun)
	}
	if cfg.spawnSkybox != nil {
		cfg.spawnSkybox(cmd)
	}
}

func spawnDaylightSkybox(cmd *Commands) {
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerGradient,
		Resolution: [2]int{1024, 512},
		ColorA:     mgl32.Vec3{0.96, 0.74, 0.52},
		ColorB:     mgl32.Vec3{0.19, 0.42, 0.78},
		Opacity:    1.0,
		Priority:   0,
		Smooth:     true,
		BlendMode:  SkyboxBlendAlpha,
	})

	cmd.AddEntity(&SkyboxLayerComponent{
		NoiseType:   SkyboxNoisePerlin,
		Seed:        42,
		Scale:       4.5,
		Octaves:     4,
		Persistence: 0.5,
		Lacunarity:  2.0,
		Resolution:  [2]int{1024, 512},
		ColorA:      mgl32.Vec3{1.0, 0.98, 0.95},
		ColorB:      mgl32.Vec3{0.77, 0.82, 0.9},
		Threshold:   0.52,
		Opacity:     0.82,
		Priority:    1,
		Smooth:      true,
		BlendMode:   SkyboxBlendAlpha,
		WindSpeed:   mgl32.Vec3{0.015, 0.008, 0},
	})

	cmd.AddEntity(&SkyboxLayerComponent{
		NoiseType:  SkyboxNoisePerlin,
		Seed:       999,
		Scale:      11.0,
		Octaves:    2,
		Resolution: [2]int{1024, 512},
		ColorA:     mgl32.Vec3{0.97, 0.97, 1.0},
		ColorB:     mgl32.Vec3{0.73, 0.78, 0.86},
		Threshold:  0.6,
		Opacity:    0.38,
		Priority:   2,
		Smooth:     true,
		BlendMode:  SkyboxBlendAlpha,
		WindSpeed:  mgl32.Vec3{-0.02, 0.012, 0.006},
	})
}

func spawnFullmoonNightSkybox(cmd *Commands) {
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerGradient,
		Resolution: [2]int{1024, 512},
		ColorA:     mgl32.Vec3{0.008, 0.012, 0.03},
		ColorB:     mgl32.Vec3{0.0, 0.0, 0.008},
		Opacity:    1.0,
		Priority:   0,
		Smooth:     true,
		BlendMode:  SkyboxBlendAlpha,
	})
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerStars,
		Resolution: [2]int{1024, 512},
		Seed:       2077,
		Scale:      1.0,
		ColorA:     mgl32.Vec3{0.9, 0.94, 1.0},
		ColorB:     mgl32.Vec3{0.72, 0.8, 1.0},
		Threshold:  0.991,
		Opacity:    1.0,
		Priority:   1,
		Smooth:     false,
		BlendMode:  SkyboxBlendAdd,
	})
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:   SkyboxLayerNoise,
		NoiseType:   SkyboxNoisePerlin,
		Seed:        91,
		Scale:       3.8,
		Octaves:     3,
		Persistence: 0.55,
		Lacunarity:  2.0,
		Resolution:  [2]int{1024, 512},
		ColorA:      mgl32.Vec3{0.016, 0.024, 0.055},
		ColorB:      mgl32.Vec3{0.038, 0.05, 0.09},
		Threshold:   0.72,
		Opacity:     0.1,
		Priority:    2,
		Smooth:      true,
		BlendMode:   SkyboxBlendAlpha,
		WindSpeed:   mgl32.Vec3{0.003, 0.001, 0},
	})
}

func spawnSunsetDuskSkybox(cmd *Commands) {
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerGradient,
		Resolution: [2]int{1024, 512},
		ColorA:     mgl32.Vec3{0.98, 0.46, 0.22},
		ColorB:     mgl32.Vec3{0.16, 0.18, 0.42},
		Opacity:    1.0,
		Priority:   0,
		Smooth:     true,
		BlendMode:  SkyboxBlendAlpha,
	})
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:   SkyboxLayerNoise,
		NoiseType:   SkyboxNoisePerlin,
		Seed:        7,
		Scale:       5.4,
		Octaves:     4,
		Persistence: 0.52,
		Lacunarity:  2.0,
		Resolution:  [2]int{1024, 512},
		ColorA:      mgl32.Vec3{1.0, 0.72, 0.5},
		ColorB:      mgl32.Vec3{0.54, 0.28, 0.34},
		Threshold:   0.5,
		Opacity:     0.68,
		Priority:    1,
		Smooth:      true,
		BlendMode:   SkyboxBlendAlpha,
		WindSpeed:   mgl32.Vec3{0.01, 0.006, 0},
	})
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerStars,
		Resolution: [2]int{1024, 512},
		Seed:       14,
		Scale:      1.0,
		ColorA:     mgl32.Vec3{1.0, 0.98, 0.94},
		ColorB:     mgl32.Vec3{0.84, 0.86, 1.0},
		Threshold:  0.997,
		Opacity:    0.18,
		Priority:   2,
		Smooth:     false,
		BlendMode:  SkyboxBlendAdd,
	})
}

func spawnStormOvercastSkybox(cmd *Commands) {
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:  SkyboxLayerGradient,
		Resolution: [2]int{1024, 512},
		ColorA:     mgl32.Vec3{0.52, 0.56, 0.62},
		ColorB:     mgl32.Vec3{0.17, 0.19, 0.24},
		Opacity:    1.0,
		Priority:   0,
		Smooth:     true,
		BlendMode:  SkyboxBlendAlpha,
	})
	cmd.AddEntity(&SkyboxLayerComponent{
		LayerType:   SkyboxLayerNoise,
		NoiseType:   SkyboxNoisePerlin,
		Seed:        303,
		Scale:       2.8,
		Octaves:     5,
		Persistence: 0.58,
		Lacunarity:  2.1,
		Resolution:  [2]int{1024, 512},
		ColorA:      mgl32.Vec3{0.78, 0.8, 0.84},
		ColorB:      mgl32.Vec3{0.28, 0.31, 0.36},
		Threshold:   0.36,
		Opacity:     0.92,
		Priority:    1,
		Smooth:      true,
		BlendMode:   SkyboxBlendAlpha,
		WindSpeed:   mgl32.Vec3{0.018, 0.009, 0},
	})
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

func stablePlacementVolumeShadowGroupID(levelID string, volumeID string) uint64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(levelID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(volumeID))
	value := hasher.Sum64()
	if value == 0 {
		return 1
	}
	return value
}
