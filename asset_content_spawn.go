package gekko

import (
	"fmt"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type AuthoredAssetSpawnResult struct {
	RootEntity        EntityId
	EntitiesByAssetID map[string]EntityId
	PartIDs           map[string]struct{}
}

func SpawnAuthoredAsset(cmd *Commands, assets *AssetServer, def *content.AssetDef, rootTransform TransformComponent) (AuthoredAssetSpawnResult, error) {
	result := AuthoredAssetSpawnResult{
		EntitiesByAssetID: make(map[string]EntityId),
		PartIDs:           make(map[string]struct{}),
	}
	if def == nil {
		return result, fmt.Errorf("asset definition is nil")
	}
	if err := ValidateAssetHierarchy(def); err != nil {
		return result, err
	}

	result.RootEntity = cmd.AddEntity(
		&rootTransform,
		&LocalTransformComponent{
			Position: rootTransform.Position,
			Rotation: rootTransform.Rotation,
			Scale:    rootTransform.Scale,
		},
	)

	for _, part := range def.Parts {
		eid, err := spawnAuthoredPart(cmd, assets, part)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[part.ID] = eid
		result.PartIDs[part.ID] = struct{}{}
	}
	for _, light := range def.Lights {
		eid, err := spawnAuthoredLight(cmd, light)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[light.ID] = eid
	}
	for _, emitter := range def.Emitters {
		eid, err := spawnAuthoredEmitter(cmd, assets, emitter)
		if err != nil {
			return result, err
		}
		result.EntitiesByAssetID[emitter.ID] = eid
	}
	cmd.app.FlushCommands()

	attachToParent := func(itemID, parentID string) error {
		eid, ok := result.EntitiesByAssetID[itemID]
		if !ok {
			return fmt.Errorf("spawned entity missing for asset id %s", itemID)
		}
		if parentID == "" {
			return nil
		}
		parentEntity, exists := result.EntitiesByAssetID[parentID]
		if !exists {
			return fmt.Errorf("missing parent %s for %s", parentID, itemID)
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
	cmd.app.FlushCommands()

	TransformHierarchySystem(cmd)
	return result, nil
}

func ValidateAssetHierarchy(def *content.AssetDef) error {
	if def == nil {
		return fmt.Errorf("asset definition is nil")
	}

	partIDs := make(map[string]struct{}, len(def.Parts))
	for _, part := range def.Parts {
		partIDs[part.ID] = struct{}{}
	}

	for _, part := range def.Parts {
		if part.ParentID == "" {
			continue
		}
		if part.ParentID == part.ID {
			return fmt.Errorf("part %s cannot parent itself", part.ID)
		}
		if _, ok := partIDs[part.ParentID]; !ok {
			return fmt.Errorf("part %s has unsupported or missing parent %s", part.ID, part.ParentID)
		}
	}
	for _, light := range def.Lights {
		if light.ParentID == "" {
			continue
		}
		if _, ok := partIDs[light.ParentID]; !ok {
			return fmt.Errorf("light %s has unsupported or missing parent %s", light.ID, light.ParentID)
		}
	}
	for _, emitter := range def.Emitters {
		if emitter.ParentID == "" {
			continue
		}
		if _, ok := partIDs[emitter.ParentID]; !ok {
			return fmt.Errorf("emitter %s has unsupported or missing parent %s", emitter.ID, emitter.ParentID)
		}
	}

	visiting := make(map[string]bool, len(def.Parts))
	visited := make(map[string]bool, len(def.Parts))
	partByID := make(map[string]content.AssetPartDef, len(def.Parts))
	for _, part := range def.Parts {
		partByID[part.ID] = part
	}

	var visit func(string) error
	visit = func(id string) error {
		if id == "" || visited[id] {
			return nil
		}
		if visiting[id] {
			return fmt.Errorf("hierarchy cycle detected at %s", id)
		}
		visiting[id] = true
		if parentID := partByID[id].ParentID; parentID != "" {
			if err := visit(parentID); err != nil {
				return err
			}
		}
		visiting[id] = false
		visited[id] = true
		return nil
	}

	for _, part := range def.Parts {
		if err := visit(part.ID); err != nil {
			return err
		}
	}
	return nil
}

func spawnAuthoredPart(cmd *Commands, assets *AssetServer, part content.AssetPartDef) (EntityId, error) {
	tr := AssetTransformFromDef(part.Transform)
	local := AssetLocalTransformFromDef(part.Transform)
	comps := []any{&tr, &local}

	model, palette, err := modelAndPaletteFromSource(assets, part)
	if err != nil {
		return 0, err
	}
	if model != (AssetId{}) {
		comps = append(comps, &VoxelModelComponent{VoxelModel: model, VoxelPalette: palette})
	}

	return cmd.AddEntity(comps...), nil
}

func spawnAuthoredLight(cmd *Commands, light content.AssetLightDef) (EntityId, error) {
	tr := AssetTransformFromDef(light.Transform)
	local := AssetLocalTransformFromDef(light.Transform)
	lightType, err := AssetLightTypeToEngine(light.Type)
	if err != nil {
		return 0, err
	}
	return cmd.AddEntity(
		&tr,
		&local,
		&LightComponent{
			Type:      lightType,
			Color:     light.Color,
			Intensity: light.Intensity,
			Range:     light.Range,
			ConeAngle: light.ConeAngle,
		},
	), nil
}

func spawnAuthoredEmitter(cmd *Commands, assets *AssetServer, emitter content.AssetEmitterDef) (EntityId, error) {
	tr := AssetTransformFromDef(emitter.Transform)
	local := AssetLocalTransformFromDef(emitter.Transform)
	emitterComp, err := ParticleEmitterFromContent(emitter.Emitter, assets)
	if err != nil {
		return 0, err
	}
	return cmd.AddEntity(&tr, &local, &emitterComp), nil
}

func modelAndPaletteFromSource(assets *AssetServer, part content.AssetPartDef) (AssetId, AssetId, error) {
	if assets == nil {
		return AssetId{}, AssetId{}, nil
	}

	switch part.Source.Kind {
	case content.AssetSourceKindVoxModel:
		voxFile, err := LoadVoxFile(part.Source.Path)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
		if part.Source.ModelIndex < 0 || part.Source.ModelIndex >= len(voxFile.Models) {
			return AssetId{}, AssetId{}, fmt.Errorf("model index %d out of range for %s", part.Source.ModelIndex, part.Source.Path)
		}
		model := assets.CreateVoxelModelFromSource(voxFile.Models[part.Source.ModelIndex], part.ModelScale, part.Source.Path)
		palette := assets.CreateVoxelPaletteFromSource(voxFile.Palette, voxFile.VoxMaterials, part.Source.Path)
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
		palette := assets.CreatePBRPalette([4]uint8{255, 255, 255, 255}, 1, 0, 0, 1)
		return model, palette, nil
	case content.AssetSourceKindVoxSceneNode:
		return AssetId{}, AssetId{}, fmt.Errorf("vox_scene_node spawn not implemented yet")
	default:
		return AssetId{}, AssetId{}, fmt.Errorf("unsupported asset source kind %q", part.Source.Kind)
	}
}

func LocalTransformToWorld(parentWorld TransformComponent, parentIsVoxel bool, local LocalTransformComponent) TransformComponent {
	vSize := float32(1.0)
	if parentIsVoxel {
		vSize = VoxelSize
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
