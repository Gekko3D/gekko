package gekko

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type AuthoredAssetSpawnResult struct {
	RootEntity         EntityId
	Entities           []EntityId
	AssetID            string
	EntitiesByAssetID  map[string]EntityId
	ItemKindsByAssetID map[string]AuthoredItemKind
	PartIDs            map[string]struct{}
	Collapsed          bool
	CollapsedPartIDs   map[string]struct{}
}

type AuthoredAssetSpawnOptions struct {
	DocumentPath                   string
	CollapseVoxelParts             VoxelPartCollapseMode
	OverrideCastShadows            *bool
	OverrideShadowMaxDistance      *float32
	OverrideShadowCasterGroupID    uint64
	OverrideShadowCasterGroupLimit *int
}

type PreparedAuthoredAsset struct {
	def          *content.AssetDef
	documentPath string
	animations   *content.ResolvedAssetAnimations
	parts        map[string]preparedAuthoredPart
}

type preparedAuthoredPart struct {
	model   AssetId
	palette AssetId
}

func SpawnAuthoredAsset(cmd *Commands, assets *AssetServer, def *content.AssetDef, rootTransform TransformComponent) (AuthoredAssetSpawnResult, error) {
	return SpawnAuthoredAssetWithOptions(cmd, assets, def, rootTransform, AuthoredAssetSpawnOptions{})
}

func SpawnAuthoredAssetWithOptions(cmd *Commands, assets *AssetServer, def *content.AssetDef, rootTransform TransformComponent, opts AuthoredAssetSpawnOptions) (AuthoredAssetSpawnResult, error) {
	return spawnAuthoredAssetWithOptions(cmd, assets, def, nil, rootTransform, opts)
}

func SpawnPreparedAuthoredAsset(cmd *Commands, assets *AssetServer, prepared *PreparedAuthoredAsset, rootTransform TransformComponent) (AuthoredAssetSpawnResult, error) {
	if prepared == nil {
		return AuthoredAssetSpawnResult{}, fmt.Errorf("prepared asset is nil")
	}
	return spawnAuthoredAssetWithOptions(cmd, assets, prepared.def, prepared, rootTransform, AuthoredAssetSpawnOptions{DocumentPath: prepared.documentPath})
}

func spawnAuthoredAssetWithOptions(cmd *Commands, assets *AssetServer, def *content.AssetDef, prepared *PreparedAuthoredAsset, rootTransform TransformComponent, opts AuthoredAssetSpawnOptions) (AuthoredAssetSpawnResult, error) {
	result := AuthoredAssetSpawnResult{
		EntitiesByAssetID:  make(map[string]EntityId),
		ItemKindsByAssetID: make(map[string]AuthoredItemKind),
		PartIDs:            make(map[string]struct{}),
	}
	if def == nil {
		return result, fmt.Errorf("asset definition is nil")
	}
	result.AssetID = def.ID
	var animations *content.ResolvedAssetAnimations
	if prepared == nil {
		if validation := content.ValidateAsset(def, content.AssetValidationOptions{DocumentPath: opts.DocumentPath}); validation.HasErrors() {
			return result, fmt.Errorf("asset validation failed: %s", validation.Error())
		}
		var err error
		animations, err = content.ResolveAssetAnimations(def, opts.DocumentPath)
		if err != nil {
			return result, fmt.Errorf("asset animation resolution failed: %w", err)
		}
		content.NormalizeAssetDef(def)
		if err := ValidateAssetHierarchy(def); err != nil {
			return result, err
		}
	} else {
		animations = prepared.animations
	}
	if collapsed, err := trySpawnCollapsedAuthoredAsset(cmd, assets, def, rootTransform, opts, &result); collapsed || err != nil {
		return result, err
	}
	shadowSettings := effectiveAuthoredVoxelShadowSettings(def, opts)

	result.RootEntity = cmd.AddEntity(
		&rootTransform,
		&LocalTransformComponent{
			Position: rootTransform.Position,
			Rotation: rootTransform.Rotation,
			Scale:    rootTransform.Scale,
		},
		&AuthoredAssetRootComponent{AssetID: def.ID},
	)
	result.Entities = append(result.Entities, result.RootEntity)

	for _, part := range def.Parts {
		var (
			eid EntityId
			err error
		)
		if prepared == nil {
			eid, err = spawnAuthoredPart(cmd, assets, def, part, opts.DocumentPath, shadowSettings)
		} else if preparedPart, ok := prepared.parts[part.ID]; ok {
			eid, err = spawnAuthoredPartWithAssets(cmd, def, part, shadowSettings, preparedPart.model, preparedPart.palette)
		} else {
			err = fmt.Errorf("prepared asset missing part %s", part.ID)
		}
		if err != nil {
			return result, err
		}
		result.Entities = append(result.Entities, eid)
		result.EntitiesByAssetID[part.ID] = eid
		result.ItemKindsByAssetID[part.ID] = AuthoredItemKindPart
		result.PartIDs[part.ID] = struct{}{}
	}
	for _, light := range def.Lights {
		eid, err := spawnAuthoredLight(cmd, def.ID, light)
		if err != nil {
			return result, err
		}
		result.Entities = append(result.Entities, eid)
		result.EntitiesByAssetID[light.ID] = eid
		result.ItemKindsByAssetID[light.ID] = AuthoredItemKindLight
	}
	for _, emitter := range def.Emitters {
		eid, err := spawnAuthoredEmitter(cmd, assets, def.ID, emitter)
		if err != nil {
			return result, err
		}
		result.Entities = append(result.Entities, eid)
		result.EntitiesByAssetID[emitter.ID] = eid
		result.ItemKindsByAssetID[emitter.ID] = AuthoredItemKindEmitter
	}
	for _, marker := range def.Markers {
		eid, err := spawnAuthoredMarker(cmd, def.ID, marker)
		if err != nil {
			return result, err
		}
		result.Entities = append(result.Entities, eid)
		result.EntitiesByAssetID[marker.ID] = eid
		result.ItemKindsByAssetID[marker.ID] = AuthoredItemKindMarker
	}
	cmd.app.FlushCommands()
	for _, entity := range result.EntitiesByAssetID {
		if ref, _ := cmd.GetComponent(entity, reflect.TypeOf(AuthoredAssetRefComponent{})).(*AuthoredAssetRefComponent); ref != nil {
			ref.RootEntity = result.RootEntity
		}
	}

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

	if animationSet := newAuthoredAssetAnimationSetComponent(animations, result, cmd); animationSet != nil {
		defaultClip, ok := animationSet.Clips[animationSet.DefaultClipID]
		cmd.AddComponents(result.RootEntity,
			&AnimationPlayerComponent{
				ClipID:  animationSet.DefaultClipID,
				Speed:   1,
				Playing: true,
				Loop:    ok && defaultClip.Loop,
			},
			animationSet,
		)
		cmd.app.FlushCommands()
	}

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

// PrepareAuthoredAsset validates content and builds shared CPU-side resources
// without creating ECS entities. Treat the returned asset as immutable.
func PrepareAuthoredAsset(assets *AssetServer, def *content.AssetDef, documentPath string) (*PreparedAuthoredAsset, error) {
	if def == nil {
		return nil, fmt.Errorf("asset definition is nil")
	}
	if validation := content.ValidateAsset(def, content.AssetValidationOptions{DocumentPath: documentPath}); validation.HasErrors() {
		return nil, fmt.Errorf("asset validation failed: %s", validation.Error())
	}
	animations, err := content.ResolveAssetAnimations(def, documentPath)
	if err != nil {
		return nil, fmt.Errorf("asset animation resolution failed: %w", err)
	}
	content.NormalizeAssetDef(def)
	if err := ValidateAssetHierarchy(def); err != nil {
		return nil, err
	}
	prepared := &PreparedAuthoredAsset{
		def: def, documentPath: documentPath, animations: animations,
		parts: make(map[string]preparedAuthoredPart, len(def.Parts)),
	}
	for _, part := range def.Parts {
		model, palette, err := modelAndPaletteFromSource(assets, def, part, documentPath)
		if err != nil {
			return nil, err
		}
		prepared.parts[part.ID] = preparedAuthoredPart{model: model, palette: palette}
	}
	return prepared, nil
}

func LoadAndPrepareAuthoredAsset(path string, assets *AssetServer, loader *RuntimeContentLoader) (*PreparedAuthoredAsset, error) {
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}
	def, err := loader.LoadAsset(path)
	if err != nil {
		return nil, err
	}
	return PrepareAuthoredAsset(assets, def, path)
}

func LoadAndSpawnAuthoredAssetFromLibrary(library *content.AssetLibraryDef, libraryPath, key string, cmd *Commands, assets *AssetServer, rootTransform TransformComponent) (AuthoredAssetSpawnResult, error) {
	path, err := content.ResolveAssetLibraryPath(library, libraryPath, key)
	if err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	return LoadAndSpawnAuthoredAsset(path, cmd, assets, rootTransform)
}

// AttachAuthoredAssetRoot mounts an already spawned asset root to a marker
// using a transform authored in an external .gkattachments library.
func AttachAuthoredAssetRoot(cmd *Commands, root, parentMarker EntityId, attachment content.AssetAttachmentDef) error {
	if cmd == nil || root == 0 || parentMarker == 0 {
		return fmt.Errorf("asset attachment requires a root and parent marker")
	}
	if attachment.ID == "" {
		return fmt.Errorf("asset attachment id is required")
	}
	attached := &AuthoredAssetAttachmentComponent{AttachmentID: attachment.ID, ParentMarker: parentMarker, MountTransform: attachment.Transform, GripFrames: append([]content.AssetAttachmentGripFrameDef(nil), attachment.GripFrames...)}
	if attachment.AimOffset != nil {
		offset := *attachment.AimOffset
		attached.AimOffset = &offset
	}
	if attachment.Aim != nil {
		marker, ok := FindAuthoredAssetMarkerByID(cmd, root, attachment.Aim.MarkerID)
		if !ok {
			return fmt.Errorf("asset attachment %q aim marker %q not found", attachment.ID, attachment.Aim.MarkerID)
		}
		aim := *attachment.Aim
		attached.AimMarker, attached.AimFrame = marker.Entity, &aim
	}
	local, _ := cmd.GetComponent(root, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
	if local == nil {
		return fmt.Errorf("asset attachment root %d has no local transform", root)
	}
	*local = AssetLocalTransformFromDef(attachment.Transform)
	cmd.AddComponents(root,
		&Parent{Entity: parentMarker},
		attached,
	)
	cmd.app.FlushCommands()
	TransformHierarchySystem(cmd)
	return nil
}

// LoadAndAttachAuthoredAsset loads a child .gkasset, then mounts its root at
// the supplied host marker. Callers resolve attachment asset refs to paths.
func LoadAndAttachAuthoredAsset(path string, cmd *Commands, assets *AssetServer, parentMarker EntityId, attachment content.AssetAttachmentDef) (AuthoredAssetSpawnResult, error) {
	spawned, err := LoadAndSpawnAuthoredAsset(path, cmd, assets, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	if err := AttachAuthoredAssetRoot(cmd, spawned.RootEntity, parentMarker, attachment); err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	return spawned, nil
}

func AttachPreparedAuthoredAsset(prepared *PreparedAuthoredAsset, cmd *Commands, assets *AssetServer, parentMarker EntityId, attachment content.AssetAttachmentDef) (AuthoredAssetSpawnResult, error) {
	spawned, err := SpawnPreparedAuthoredAsset(cmd, assets, prepared, TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}})
	if err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	if err := AttachAuthoredAssetRoot(cmd, spawned.RootEntity, parentMarker, attachment); err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	return spawned, nil
}

func LoadAndAttachAuthoredAssetFromLibrary(library *content.AssetLibraryDef, libraryPath, key string, cmd *Commands, assets *AssetServer, parentMarker EntityId, attachment content.AssetAttachmentDef) (AuthoredAssetSpawnResult, error) {
	path, err := content.ResolveAssetLibraryPath(library, libraryPath, key)
	if err != nil {
		return AuthoredAssetSpawnResult{}, err
	}
	return LoadAndAttachAuthoredAsset(path, cmd, assets, parentMarker, attachment)
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

func spawnAuthoredPart(cmd *Commands, assets *AssetServer, def *content.AssetDef, part content.AssetPartDef, documentPath string, shadowSettings voxelShadowSettings) (EntityId, error) {
	model, palette, err := modelAndPaletteFromSource(assets, def, part, documentPath)
	if err != nil {
		return 0, err
	}
	return spawnAuthoredPartWithAssets(cmd, def, part, shadowSettings, model, palette)
}

func spawnAuthoredPartWithAssets(cmd *Commands, def *content.AssetDef, part content.AssetPartDef, shadowSettings voxelShadowSettings, model, palette AssetId) (EntityId, error) {
	tr := AssetTransformFromDef(part.Transform)
	local := AssetLocalTransformFromDef(part.Transform)
	comps := []any{
		&tr,
		&local,
		&AuthoredAssetRefComponent{AssetID: def.ID, ItemID: part.ID, Kind: AuthoredItemKindPart},
	}

	if model != (AssetId{}) {
		voxelModel := &VoxelModelComponent{
			SharedGeometry:         model,
			VoxelPalette:           palette,
			VoxelResolution:        part.VoxelResolution,
			EmitterLinkID:          part.EmitterLinkID,
			DisableShadows:         shadowSettings.disable,
			ShadowMaxDistance:      shadowSettings.maxDistance,
			ShadowCasterGroupID:    shadowSettings.casterGroupID,
			ShadowCasterGroupLimit: shadowSettings.casterGroupLimit,
		}
		if part.Source.Kind == content.AssetSourceKindVoxelShape {
			voxelModel.PivotMode = PivotModeCustom
			voxelModel.CustomPivot = mgl32.Vec3{part.Transform.Pivot[0], part.Transform.Pivot[1], part.Transform.Pivot[2]}
		}
		comps = append(comps, voxelModel)
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
			Type:         lightType,
			Color:        light.Color,
			Intensity:    light.Intensity,
			Range:        light.Range,
			ConeAngle:    light.ConeAngle,
			CastsShadows: light.CastsShadows,
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
		&AuthoredMarkerComponent{Name: marker.Name, Kind: marker.Kind, Tags: append([]string(nil), marker.Tags...)},
	), nil
}

func modelAndPaletteFromSource(assets *AssetServer, def *content.AssetDef, part content.AssetPartDef, documentPath string) (AssetId, AssetId, error) {
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
		palette, err := authoredVoxFilePalette(assets, def, part, voxFile.Palette, voxFile.VoxMaterials, voxFile.Models[part.Source.ModelIndex], sourcePath)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
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
		case "cylinder":
			model = assets.CreateCylinderModel(params["radius"], params["height"], part.ModelScale)
		case "capsule":
			model = assets.CreateCapsuleModel(params["radius"], params["height"], part.ModelScale)
		case "ramp":
			model = assets.CreateRampModel(params["sx"], params["sy"], params["sz"], part.ModelScale)
		default:
			return AssetId{}, AssetId{}, fmt.Errorf("unsupported procedural primitive %q", part.Source.Primitive)
		}
		palette, err := authoredProceduralPalette(assets, def, part)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
		return model, palette, nil
	case content.AssetSourceKindVoxelShape:
		model, err := authoredVoxelShapeGeometry(assets, part)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
		palette, err := authoredVoxelShapePalette(assets, def, part)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
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
		palette, err := authoredVoxFilePalette(assets, def, part, voxFile.Palette, voxFile.VoxMaterials, voxFile.Models[resolved.ModelIndex], sourcePath)
		if err != nil {
			return AssetId{}, AssetId{}, err
		}
		return model, palette, nil
	default:
		return AssetId{}, AssetId{}, fmt.Errorf("unsupported asset source kind %q", part.Source.Kind)
	}
}

func authoredProceduralPalette(assets *AssetServer, def *content.AssetDef, part content.AssetPartDef) (AssetId, error) {
	if assets == nil {
		return AssetId{}, nil
	}
	if part.Source.MaterialID == "" {
		return assets.CreatePBRPalette([4]uint8{255, 255, 255, 255}, 1, 0, 0, 1.5), nil
	}
	material, ok := content.FindAssetMaterialByID(def, part.Source.MaterialID)
	if !ok {
		return AssetId{}, fmt.Errorf("missing material %s for part %s", part.Source.MaterialID, part.ID)
	}
	return createAuthoredMaterialVoxelPalette(assets, material), nil
}

func authoredVoxFilePalette(assets *AssetServer, def *content.AssetDef, part content.AssetPartDef, palette VoxPalette, materials []VoxMaterial, model VoxModel, sourcePath string) (AssetId, error) {
	if part.Source.MaterialID == "" {
		return assets.CreateVoxelPaletteFromSource(palette, materials, sourcePath), nil
	}
	material, ok := content.FindAssetMaterialByID(def, part.Source.MaterialID)
	if !ok {
		return AssetId{}, fmt.Errorf("missing material %s for part %s", part.Source.MaterialID, part.ID)
	}
	return assets.CreateVoxelPaletteAsset(VoxelPaletteAsset{
		VoxPalette:       palette,
		Materials:        materials,
		SurfaceMaterials: authoredVoxelSurfaceMaterialsForModel(model, material),
		SourcePath:       sourcePath,
	}), nil
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
