package gekko

import (
	"errors"
	"fmt"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type AuthoredAssetSpawnResult struct {
	RootEntity         EntityId
	AssetID            string
	EntitiesByAssetID  map[string]EntityId
	ItemKindsByAssetID map[string]AuthoredItemKind
	PartIDs            map[string]struct{}
	Collapsed          bool
	CollapsedPartIDs   map[string]struct{}
}

type AuthoredAssetSpawnOptions struct {
	DocumentPath       string
	CollapseVoxelParts VoxelPartCollapseMode
}

func SpawnAuthoredAsset(cmd *Commands, assets *AssetServer, def *content.AssetDef, rootTransform TransformComponent) (AuthoredAssetSpawnResult, error) {
	return SpawnAuthoredAssetWithOptions(cmd, assets, def, rootTransform, AuthoredAssetSpawnOptions{})
}

func SpawnAuthoredAssetWithOptions(cmd *Commands, assets *AssetServer, def *content.AssetDef, rootTransform TransformComponent, opts AuthoredAssetSpawnOptions) (AuthoredAssetSpawnResult, error) {
	result := AuthoredAssetSpawnResult{
		EntitiesByAssetID:  make(map[string]EntityId),
		ItemKindsByAssetID: make(map[string]AuthoredItemKind),
		PartIDs:            make(map[string]struct{}),
	}
	if def == nil {
		return result, fmt.Errorf("asset definition is nil")
	}
	result.AssetID = def.ID
	if validation := content.ValidateAsset(def, content.AssetValidationOptions{DocumentPath: opts.DocumentPath}); validation.HasErrors() {
		return result, fmt.Errorf("asset validation failed: %s", validation.Error())
	}
	content.NormalizeAssetDef(def)
	if err := ValidateAssetHierarchy(def); err != nil {
		return result, err
	}
	if collapsed, err := trySpawnCollapsedAuthoredAsset(cmd, assets, def, rootTransform, opts, &result); collapsed || err != nil {
		return result, err
	}

	result.RootEntity = cmd.AddEntity(
		&rootTransform,
		&LocalTransformComponent{
			Position: rootTransform.Position,
			Rotation: rootTransform.Rotation,
			Scale:    rootTransform.Scale,
		},
		&AuthoredAssetRootComponent{AssetID: def.ID},
	)

	for _, part := range def.Parts {
		eid, err := spawnAuthoredPart(cmd, assets, def.ID, part, opts.DocumentPath)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[part.ID] = eid
		result.ItemKindsByAssetID[part.ID] = AuthoredItemKindPart
		result.PartIDs[part.ID] = struct{}{}
	}
	for _, light := range def.Lights {
		eid, err := spawnAuthoredLight(cmd, def.ID, light)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[light.ID] = eid
		result.ItemKindsByAssetID[light.ID] = AuthoredItemKindLight
	}
	for _, emitter := range def.Emitters {
		eid, err := spawnAuthoredEmitter(cmd, assets, def.ID, emitter)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[emitter.ID] = eid
		result.ItemKindsByAssetID[emitter.ID] = AuthoredItemKindEmitter
	}
	for _, marker := range def.Markers {
		eid, err := spawnAuthoredMarker(cmd, def.ID, marker)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[marker.ID] = eid
		result.ItemKindsByAssetID[marker.ID] = AuthoredItemKindMarker
	}
	cmd.app.FlushCommands()

	attachToParent := func(itemID, parentID string) error {
		eid, ok := result.EntitiesByAssetID[itemID]
		if !ok {
			return fmt.Errorf("spawned entity missing for asset id %s", itemID)
		}
		parentEntity := result.RootEntity
		if parentID != "" {
			var exists bool
			parentEntity, exists = result.EntitiesByAssetID[parentID]
			if !exists {
				return fmt.Errorf("missing parent %s for %s", parentID, itemID)
			}
		}
		cmd.AddComponents(eid, &Parent{Entity: parentEntity})
		return nil
	}

	for _, part := range def.Parts {
		if err := attachToParent(part.ID, part.ParentID); err != nil {
			return result, err
		}
	}
	for _, light := range def.Lights {
		if err := attachToParent(light.ID, light.ParentID); err != nil {
			return result, err
		}
	}
	for _, emitter := range def.Emitters {
		if err := attachToParent(emitter.ID, emitter.ParentID); err != nil {
			return result, err
		}
	}
	for _, marker := range def.Markers {
		if err := attachToParent(marker.ID, marker.ParentID); err != nil {
			return result, err
		}
	}
	cmd.app.FlushCommands()

	TransformHierarchySystem(cmd)
	return result, nil
}

func LoadAndSpawnAuthoredAsset(path string, cmd *Commands, assets *AssetServer, rootTransform TransformComponent) (AuthoredAssetSpawnResult, error) {
	def, err := content.LoadAsset(path)
	if err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	return SpawnAuthoredAssetWithOptions(cmd, assets, def, rootTransform, AuthoredAssetSpawnOptions{DocumentPath: path})
}

func ValidateAssetHierarchy(def *content.AssetDef) error {
	validation := content.ValidateAsset(def, content.AssetValidationOptions{})
	for _, issue := range validation.Issues {
		switch issue.Code {
		case "broken_parent_reference", "unsupported_parent_target", "hierarchy_cycle":
			return errors.New(issue.Message)
		}
	}
	return nil
}

func spawnAuthoredPart(cmd *Commands, assets *AssetServer, assetID string, part content.AssetPartDef, documentPath string) (EntityId, error) {
	tr := AssetTransformFromDef(part.Transform)
	local := AssetLocalTransformFromDef(part.Transform)
	comps := []any{
		&tr,
		&local,
		&AuthoredAssetRefComponent{AssetID: assetID, ItemID: part.ID, Kind: AuthoredItemKindPart},
	}

	model, palette, err := modelAndPaletteFromSource(assets, part, documentPath)
	if err != nil {
		return 0, err
	}
	if model != (AssetId{}) {
		comps = append(comps, &VoxelModelComponent{
			SharedGeometry:  model,
			VoxelPalette:    palette,
			VoxelResolution: part.VoxelResolution,
		})
	}

	return cmd.AddEntity(comps...), nil
}

func spawnAuthoredLight(cmd *Commands, assetID string, light content.AssetLightDef) (EntityId, error) {
	tr := AssetTransformFromDef(light.Transform)
	local := AssetLocalTransformFromDef(light.Transform)
	lightType, err := AssetLightTypeToEngine(light.Type)
	if err != nil {
		return 0, err
	}
	return cmd.AddEntity(
		&tr,
		&local,
		&AuthoredAssetRefComponent{AssetID: assetID, ItemID: light.ID, Kind: AuthoredItemKindLight},
		&LightComponent{
			Type:      lightType,
			Color:     light.Color,
			Intensity: light.Intensity,
			Range:     light.Range,
			ConeAngle: light.ConeAngle,
		},
	), nil
}

func spawnAuthoredEmitter(cmd *Commands, assets *AssetServer, assetID string, emitter content.AssetEmitterDef) (EntityId, error) {
	tr := AssetTransformFromDef(emitter.Transform)
	local := AssetLocalTransformFromDef(emitter.Transform)
	emitterComp, err := ParticleEmitterFromContent(emitter.Emitter, assets)
	if err != nil {
		return 0, err
	}
	return cmd.AddEntity(
		&tr,
		&local,
		&AuthoredAssetRefComponent{AssetID: assetID, ItemID: emitter.ID, Kind: AuthoredItemKindEmitter},
		&emitterComp,
	), nil
}

func spawnAuthoredMarker(cmd *Commands, assetID string, marker content.AssetMarkerDef) (EntityId, error) {
	tr := AssetTransformFromDef(marker.Transform)
	local := AssetLocalTransformFromDef(marker.Transform)
	return cmd.AddEntity(
		&tr,
		&local,
		&AuthoredAssetRefComponent{AssetID: assetID, ItemID: marker.ID, Kind: AuthoredItemKindMarker},
		&AuthoredMarkerComponent{Kind: marker.Kind, Tags: append([]string(nil), marker.Tags...)},
	), nil
}

func modelAndPaletteFromSource(assets *AssetServer, part content.AssetPartDef, documentPath string) (AssetId, AssetId, error) {
	if assets == nil {
		return AssetId{}, AssetId{}, nil
	}

	sourcePath := content.ResolveDocumentPath(part.Source.Path, documentPath)

	switch part.Source.Kind {
	case content.AssetSourceKindGroup:
		return AssetId{}, AssetId{}, nil
	case content.AssetSourceKindVoxModel:
		voxFile, err := LoadVoxFile(sourcePath)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
		if part.Source.ModelIndex < 0 || part.Source.ModelIndex >= len(voxFile.Models) {
			return AssetId{}, AssetId{}, fmt.Errorf("model index %d out of range for %s", part.Source.ModelIndex, part.Source.Path)
		}
		model := assets.CreateVoxelModelFromSource(voxFile.Models[part.Source.ModelIndex], part.ModelScale, sourcePath)
		palette := assets.CreateVoxelPaletteFromSource(voxFile.Palette, voxFile.VoxMaterials, sourcePath)
		return model, palette, nil
	case content.AssetSourceKindProceduralPrimitive:
		model := AssetId{}
		params := part.Source.Params
		switch part.Source.Primitive {
		case "cube":
			model = assets.CreateCubeModel(params["sx"], params["sy"], params["sz"], part.ModelScale)
		case "sphere":
			model = assets.CreateSphereModel(params["radius"], part.ModelScale)
		case "cone":
			model = assets.CreateConeModel(params["radius"], params["height"], part.ModelScale)
		case "pyramid":
			model = assets.CreatePyramidModel(params["size"], params["height"], part.ModelScale)
		default:
			return AssetId{}, AssetId{}, fmt.Errorf("unsupported procedural primitive %q", part.Source.Primitive)
		}
		palette := assets.CreatePBRPalette([4]uint8{255, 255, 255, 255}, 1, 0, 0, 1.5)
		return model, palette, nil
	case content.AssetSourceKindVoxSceneNode:
		voxFile, err := LoadVoxFile(sourcePath)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
		resolved, err := ResolveVoxSceneNodeModel(InspectVoxScene(voxFile, 1.0), part.Source.NodeName, part.Source.ModelIndex)
		if err != nil {
			return AssetId{}, AssetId{}, fmt.Errorf("%s (%s): %w", part.Name, part.Source.Path, err)
		}
		if resolved.ModelIndex < 0 || resolved.ModelIndex >= len(voxFile.Models) {
			return AssetId{}, AssetId{}, fmt.Errorf("%s (%s): resolved model index %d out of range", part.Name, part.Source.Path, resolved.ModelIndex)
		}
		model := assets.CreateVoxelModelFromSource(voxFile.Models[resolved.ModelIndex], part.ModelScale, sourcePath)
		palette := assets.CreateVoxelPaletteFromSource(voxFile.Palette, voxFile.VoxMaterials, sourcePath)
		return model, palette, nil
	default:
		return AssetId{}, AssetId{}, fmt.Errorf("unsupported asset source kind %q", part.Source.Kind)
	}
}

func LocalTransformToWorld(parentWorld TransformComponent, parentIsVoxel bool, parentVoxelResolution float32, local LocalTransformComponent) TransformComponent {
	vSize := float32(1.0)
	if parentIsVoxel {
		vSize = parentVoxelResolution
		if vSize <= 0 {
			vSize = VoxelSize
		}
	}
	scaledPivot := mgl32.Vec3{
		parentWorld.Pivot.X() * vSize,
		parentWorld.Pivot.Y() * vSize,
		parentWorld.Pivot.Z() * vSize,
	}
	diff := local.Position.Sub(scaledPivot)
	scaledLocalPos := mgl32.Vec3{
		diff.X() * parentWorld.Scale.X(),
		diff.Y() * parentWorld.Scale.Y(),
		diff.Z() * parentWorld.Scale.Z(),
	}
	return TransformComponent{
		Position: parentWorld.Position.Add(parentWorld.Rotation.Rotate(scaledLocalPos)),
		Rotation: parentWorld.Rotation.Mul(local.Rotation).Normalize(),
		Scale: mgl32.Vec3{
			parentWorld.Scale.X() * local.Scale.X(),
			parentWorld.Scale.Y() * local.Scale.Y(),
			parentWorld.Scale.Z() * local.Scale.Z(),
		},
	}
}
