package gekko

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type VoxelPartCollapseMode uint8

const (
	VoxelPartCollapseDefault VoxelPartCollapseMode = iota
	VoxelPartCollapseForce
	VoxelPartCollapseDisable
)

type collapsedAuthoredVoxelBuild struct {
	geometry         AssetId
	palette          AssetId
	voxelResolution  float32
	collapsedPartIDs map[string]struct{}
}

type authoredCollapseResolvedPart struct {
	def             content.AssetPartDef
	world           TransformComponent
	geometry        VoxelGeometryAsset
	paletteAsset    VoxelPaletteAsset
	voxelResolution float32
}

type collapseVoxelSample struct {
	x, y, z int
	value   uint8
}

type voxelShadowSettings struct {
	disable          bool
	maxDistance      float32
	casterGroupID    uint64
	casterGroupLimit int
}

func trySpawnCollapsedAuthoredAsset(cmd *Commands, assets *AssetServer, def *content.AssetDef, rootTransform TransformComponent, opts AuthoredAssetSpawnOptions, result *AuthoredAssetSpawnResult) (bool, error) {
	enabled := def != nil && def.Runtime != nil && def.Runtime.CollapseVoxelParts
	switch opts.CollapseVoxelParts {
	case VoxelPartCollapseDisable:
		return false, nil
	case VoxelPartCollapseForce:
		enabled = true
	}
	if !enabled {
		return false, nil
	}

	build, err := buildCollapsedAuthoredVoxelAsset(assets, def, opts.DocumentPath)
	if err != nil {
		if opts.CollapseVoxelParts == VoxelPartCollapseForce {
			return false, err
		}
		return false, nil
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
		&CollapsedAuthoredVoxelPartsComponent{
			PartIDs: sortedCollapsedPartIDs(build.collapsedPartIDs),
		},
	)
	cmd.AddEntity(
		&TransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&LocalTransformComponent{
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{1, 1, 1},
		},
		&Parent{Entity: result.RootEntity},
		&VoxelModelComponent{
			SharedGeometry:         build.geometry,
			VoxelPalette:           build.palette,
			VoxelResolution:        build.voxelResolution,
			PivotMode:              PivotModeCorner,
			DisableShadows:         shadowSettings.disable,
			ShadowMaxDistance:      shadowSettings.maxDistance,
			ShadowCasterGroupID:    shadowSettings.casterGroupID,
			ShadowCasterGroupLimit: shadowSettings.casterGroupLimit,
		},
	)
	cmd.app.FlushCommands()
	TransformHierarchySystem(cmd)

	result.Collapsed = true
	result.CollapsedPartIDs = build.collapsedPartIDs
	for _, part := range def.Parts {
		result.PartIDs[part.ID] = struct{}{}
	}
	return true, nil
}

func buildCollapsedAuthoredVoxelAsset(assets *AssetServer, def *content.AssetDef, documentPath string) (collapsedAuthoredVoxelBuild, error) {
	result := collapsedAuthoredVoxelBuild{}
	if assets == nil {
		return result, fmt.Errorf("voxel collapse requires asset server")
	}
	if def == nil {
		return result, fmt.Errorf("asset definition is nil")
	}
	if len(def.Lights) > 0 || len(def.Emitters) > 0 || len(def.Markers) > 0 {
		return result, fmt.Errorf("voxel collapse only supports voxel-backed parts and groups")
	}

	resolvedParts, err := resolveAuthoredCollapseParts(assets, def, documentPath)
	if err != nil {
		return result, err
	}
	if len(resolvedParts) < 2 {
		return result, fmt.Errorf("voxel collapse requires at least two voxel-backed parts")
	}

	voxelResolution := resolvedParts[0].voxelResolution
	paletteID, err := collapsePaletteID(assets, resolvedParts)
	if err != nil {
		return result, err
	}

	collapseKey, err := collapseGeometryCacheKey(def, documentPath, voxelResolution)
	if err != nil {
		return result, err
	}

	combined := volume.NewXBrickMap()
	result.collapsedPartIDs = make(map[string]struct{}, len(resolvedParts))
	additiveParts := 0
	for _, part := range resolvedParts {
		if absf(part.voxelResolution-voxelResolution) > 1e-5 {
			return collapsedAuthoredVoxelBuild{}, fmt.Errorf("voxel collapse requires matching voxel_resolution")
		}
		result.collapsedPartIDs[part.def.ID] = struct{}{}
		if voxelGeometryIsEmpty(part.geometry) {
			continue
		}
		if err := bakeResolvedPartIntoComposite(combined, part, voxelResolution); err != nil {
			return collapsedAuthoredVoxelBuild{}, fmt.Errorf("collapse bake failed for part %s: %w", part.def.ID, err)
		}
		if content.EffectiveAssetSourceOperation(part.def.Source) == content.AssetShapeOperationAdd {
			additiveParts++
		}
	}
	if additiveParts == 0 {
		return collapsedAuthoredVoxelBuild{}, fmt.Errorf("voxel collapse requires at least one non-empty additive voxel part")
	}
	combined.ComputeAABB()
	combined.ClearDirty()

	result.geometry = assets.RegisterSharedVoxelGeometryWithCacheKey(collapseKey, combined, collapseKey)
	result.palette = paletteID
	result.voxelResolution = voxelResolution
	return result, nil
}

func resolveAuthoredCollapseParts(assets *AssetServer, def *content.AssetDef, documentPath string) ([]authoredCollapseResolvedPart, error) {
	tempApp := NewApp()
	tempCmd := tempApp.Commands()

	rootEntity := tempCmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&AuthoredAssetRootComponent{AssetID: def.ID},
	)
	entitiesByID := make(map[string]EntityId, len(def.Parts))
	partDefsByID := make(map[string]content.AssetPartDef, len(def.Parts))

	for _, part := range def.Parts {
		switch part.Source.Kind {
		case content.AssetSourceKindGroup, content.AssetSourceKindVoxModel, content.AssetSourceKindVoxSceneNode, content.AssetSourceKindProceduralPrimitive, content.AssetSourceKindVoxelShape:
		default:
			return nil, fmt.Errorf("voxel collapse does not support source kind %q", part.Source.Kind)
		}
		eid, err := spawnAuthoredPart(tempCmd, assets, def, part, documentPath, voxelShadowSettings{})
		if err != nil {
			return nil, err
		}
		entitiesByID[part.ID] = eid
		partDefsByID[part.ID] = part
	}
	tempApp.FlushCommands()

	for _, part := range def.Parts {
		parentEntity := rootEntity
		if part.ParentID != "" {
			parentEntity = entitiesByID[part.ParentID]
		}
		tempCmd.AddComponents(entitiesByID[part.ID], &Parent{Entity: parentEntity})
	}
	tempApp.FlushCommands()
	TransformHierarchySystem(tempCmd)

	resolved := make([]authoredCollapseResolvedPart, 0, len(def.Parts))
	for _, part := range def.Parts {
		eid := entitiesByID[part.ID]
		vmc, ok := voxelModelForAuthoredCollapse(tempCmd, eid)
		if !ok {
			continue
		}
		geometry, ok := assets.GetVoxelGeometry(vmc.GeometryAsset())
		if !ok {
			return nil, fmt.Errorf("missing geometry for part %s", part.ID)
		}
		palette, ok := assets.GetVoxelPalette(vmc.VoxelPalette)
		if !ok {
			return nil, fmt.Errorf("missing palette for part %s", part.ID)
		}
		world := worldTransformForAuthoredCollapse(tempCmd, eid)
		world.Pivot = authoredCollapsePivot(vmc, geometry)
		resolved = append(resolved, authoredCollapseResolvedPart{
			def:             partDefsByID[part.ID],
			world:           world,
			geometry:        geometry,
			paletteAsset:    palette,
			voxelResolution: VoxelResolutionOrDefault(&vmc),
		})
	}
	return resolved, nil
}

func collapsePaletteID(assets *AssetServer, parts []authoredCollapseResolvedPart) (AssetId, error) {
	additiveParts := make([]authoredCollapseResolvedPart, 0, len(parts))
	for _, part := range parts {
		if content.EffectiveAssetSourceOperation(part.def.Source) != content.AssetShapeOperationAdd {
			continue
		}
		if voxelGeometryIsEmpty(part.geometry) {
			continue
		}
		additiveParts = append(additiveParts, part)
	}
	if len(additiveParts) == 0 {
		return AssetId{}, fmt.Errorf("no voxel parts to collapse")
	}
	first := additiveParts[0].paletteAsset
	for _, part := range additiveParts[1:] {
		if !paletteAssetsEquivalent(first, part.paletteAsset) {
			return AssetId{}, fmt.Errorf("voxel collapse requires compatible palettes")
		}
	}
	return assets.CreateVoxelPaletteAsset(first), nil
}

func voxelModelForAuthoredCollapse(cmd *Commands, eid EntityId) (VoxelModelComponent, bool) {
	for _, comp := range cmd.GetAllComponents(eid) {
		if model, ok := comp.(VoxelModelComponent); ok {
			return model, true
		}
		if model, ok := comp.(*VoxelModelComponent); ok {
			return *model, true
		}
	}
	return VoxelModelComponent{}, false
}

func worldTransformForAuthoredCollapse(cmd *Commands, eid EntityId) TransformComponent {
	for _, comp := range cmd.GetAllComponents(eid) {
		if tr, ok := comp.(TransformComponent); ok {
			return tr
		}
		if tr, ok := comp.(*TransformComponent); ok {
			return *tr
		}
	}
	return TransformComponent{}
}

func authoredCollapsePivot(vmc VoxelModelComponent, geometry VoxelGeometryAsset) mgl32.Vec3 {
	switch vmc.PivotMode {
	case PivotModeCustom:
		return vmc.CustomPivot
	case PivotModeCenter:
		if geometry.XBrickMap != nil {
			minB, maxB := geometry.XBrickMap.ComputeAABB()
			return minB.Add(maxB).Mul(0.5)
		}
		return geometry.LocalMin.Add(geometry.LocalMax).Mul(0.5)
	case PivotModeCorner:
		fallthrough
	default:
		return mgl32.Vec3{}
	}
}

func paletteAssetsEquivalent(left, right VoxelPaletteAsset) bool {
	return left.IsPBR == right.IsPBR &&
		left.Roughness == right.Roughness &&
		left.Metalness == right.Metalness &&
		left.Emission == right.Emission &&
		left.IOR == right.IOR &&
		left.Transparency == right.Transparency &&
		left.VoxPalette == right.VoxPalette &&
		reflect.DeepEqual(left.Materials, right.Materials)
}

func collapseGeometryCacheKey(def *content.AssetDef, documentPath string, voxelResolution float32) (string, error) {
	payload := struct {
		Version         string
		DocumentPath    string
		AssetID         string
		Runtime         *content.AssetRuntimeDef
		Parts           []content.AssetPartDef
		VoxelResolution float32
	}{
		Version:         "collapsed-authored-voxel-v2",
		DocumentPath:    documentPath,
		AssetID:         def.ID,
		Runtime:         def.Runtime,
		Parts:           def.Parts,
		VoxelResolution: voxelResolution,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func bakeResolvedPartIntoComposite(dst *volume.XBrickMap, part authoredCollapseResolvedPart, targetResolution float32) error {
	if dst == nil {
		return fmt.Errorf("destination map is nil")
	}
	geometry := part.geometry
	if geometry.XBrickMap == nil && len(geometry.VoxModel.Voxels) > 0 {
		geometry = buildVoxelGeometryAsset(geometry.VoxModel, geometry.SourcePath)
	}
	samples := collapseVoxelSamples(geometry)
	if len(samples) == 0 {
		return fmt.Errorf("part %s has no voxel geometry", part.def.ID)
	}

	voxelScale := mgl32.Vec3{
		part.voxelResolution * part.world.Scale.X(),
		part.voxelResolution * part.world.Scale.Y(),
		part.voxelResolution * part.world.Scale.Z(),
	}
	if voxelScale.X() == 0 || voxelScale.Y() == 0 || voxelScale.Z() == 0 {
		return fmt.Errorf("part %s has zero voxel scale", part.def.ID)
	}
	invRot := part.world.Rotation.Inverse()
	epsilon := float32(1e-4)
	operation := content.EffectiveAssetSourceOperation(part.def.Source)

	for _, voxel := range samples {
		localMin := mgl32.Vec3{float32(voxel.x), float32(voxel.y), float32(voxel.z)}
		localMax := localMin.Add(mgl32.Vec3{1, 1, 1})
		worldMin, worldMax := transformedVoxelBounds(localMin, localMax, part.world, part.voxelResolution)

		minGX := int(math.Floor(float64(worldMin.X()/targetResolution - epsilon)))
		minGY := int(math.Floor(float64(worldMin.Y()/targetResolution - epsilon)))
		minGZ := int(math.Floor(float64(worldMin.Z()/targetResolution - epsilon)))
		maxGX := int(math.Ceil(float64(worldMax.X()/targetResolution + epsilon)))
		maxGY := int(math.Ceil(float64(worldMax.Y()/targetResolution + epsilon)))
		maxGZ := int(math.Ceil(float64(worldMax.Z()/targetResolution + epsilon)))

		for gx := minGX; gx < maxGX; gx++ {
			for gy := minGY; gy < maxGY; gy++ {
				for gz := minGZ; gz < maxGZ; gz++ {
					centerWorld := mgl32.Vec3{
						(float32(gx) + 0.5) * targetResolution,
						(float32(gy) + 0.5) * targetResolution,
						(float32(gz) + 0.5) * targetResolution,
					}
					local := inverseVoxelPoint(centerWorld, part.world.Position, invRot, voxelScale, part.world.Pivot)
					if local.X() < localMin.X()-epsilon || local.X() > localMax.X()+epsilon ||
						local.Y() < localMin.Y()-epsilon || local.Y() > localMax.Y()+epsilon ||
						local.Z() < localMin.Z()-epsilon || local.Z() > localMax.Z()+epsilon {
						continue
					}
					if operation == content.AssetShapeOperationSubtract {
						dst.SetVoxel(gx, gy, gz, 0)
						continue
					}
					dst.SetVoxel(gx, gy, gz, voxel.value)
				}
			}
		}
	}
	return nil
}

func collapseVoxelSamples(geometry VoxelGeometryAsset) []collapseVoxelSample {
	if len(geometry.VoxModel.Voxels) > 0 {
		samples := make([]collapseVoxelSample, 0, len(geometry.VoxModel.Voxels))
		for _, voxel := range geometry.VoxModel.Voxels {
			samples = append(samples, collapseVoxelSample{
				x:     int(voxel.X),
				y:     int(voxel.Y),
				z:     int(voxel.Z),
				value: voxel.ColorIndex,
			})
		}
		return samples
	}
	if geometry.XBrickMap == nil {
		return nil
	}
	snapshot := VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap)
	samples := make([]collapseVoxelSample, 0, len(snapshot.Voxels))
	for _, voxel := range snapshot.Voxels {
		samples = append(samples, collapseVoxelSample{
			x:     voxel.X,
			y:     voxel.Y,
			z:     voxel.Z,
			value: voxel.Value,
		})
	}
	return samples
}

func voxelGeometryIsEmpty(geometry VoxelGeometryAsset) bool {
	if len(geometry.VoxModel.Voxels) > 0 {
		return false
	}
	if geometry.XBrickMap != nil {
		return geometry.XBrickMap.GetVoxelCount() == 0
	}
	return true
}

func transformedVoxelBounds(localMin, localMax mgl32.Vec3, world TransformComponent, voxelResolution float32) (mgl32.Vec3, mgl32.Vec3) {
	corners := [8]mgl32.Vec3{
		{localMin.X(), localMin.Y(), localMin.Z()},
		{localMax.X(), localMin.Y(), localMin.Z()},
		{localMin.X(), localMax.Y(), localMin.Z()},
		{localMax.X(), localMax.Y(), localMin.Z()},
		{localMin.X(), localMin.Y(), localMax.Z()},
		{localMax.X(), localMin.Y(), localMax.Z()},
		{localMin.X(), localMax.Y(), localMax.Z()},
		{localMax.X(), localMax.Y(), localMax.Z()},
	}
	minW := mgl32.Vec3{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32}
	maxW := mgl32.Vec3{-math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for _, corner := range corners {
		worldCorner := forwardVoxelPoint(corner, world, voxelResolution)
		minW = mgl32.Vec3{
			min(minW.X(), worldCorner.X()),
			min(minW.Y(), worldCorner.Y()),
			min(minW.Z(), worldCorner.Z()),
		}
		maxW = mgl32.Vec3{
			max(maxW.X(), worldCorner.X()),
			max(maxW.Y(), worldCorner.Y()),
			max(maxW.Z(), worldCorner.Z()),
		}
	}
	return minW, maxW
}

func forwardVoxelPoint(local mgl32.Vec3, world TransformComponent, voxelResolution float32) mgl32.Vec3 {
	scaled := vec3MulComponents(local.Sub(world.Pivot), mgl32.Vec3{
		voxelResolution * world.Scale.X(),
		voxelResolution * world.Scale.Y(),
		voxelResolution * world.Scale.Z(),
	})
	return world.Position.Add(world.Rotation.Rotate(scaled))
}

func inverseVoxelPoint(worldPoint, position mgl32.Vec3, invRot mgl32.Quat, voxelScale, pivot mgl32.Vec3) mgl32.Vec3 {
	local := invRot.Rotate(worldPoint.Sub(position))
	return mgl32.Vec3{
		componentDiv(local.X(), voxelScale.X()) + pivot.X(),
		componentDiv(local.Y(), voxelScale.Y()) + pivot.Y(),
		componentDiv(local.Z(), voxelScale.Z()) + pivot.Z(),
	}
}

func componentDiv(value, scale float32) float32 {
	if scale == 0 {
		return 0
	}
	return value / scale
}

func sortedCollapsedPartIDs(ids map[string]struct{}) []string {
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func effectiveAuthoredVoxelShadowSettings(def *content.AssetDef, opts AuthoredAssetSpawnOptions) voxelShadowSettings {
	settings := voxelShadowSettings{}
	if def != nil && def.Runtime != nil {
		if def.Runtime.CastsShadows != nil {
			settings.disable = !*def.Runtime.CastsShadows
		}
		if def.Runtime.ShadowMaxDistance > 0 {
			settings.maxDistance = def.Runtime.ShadowMaxDistance
		}
	}
	if opts.OverrideCastShadows != nil {
		settings.disable = !*opts.OverrideCastShadows
	}
	if opts.OverrideShadowMaxDistance != nil {
		settings.maxDistance = maxf(0, *opts.OverrideShadowMaxDistance)
	}
	if opts.OverrideShadowCasterGroupID != 0 {
		settings.casterGroupID = opts.OverrideShadowCasterGroupID
	}
	if opts.OverrideShadowCasterGroupLimit != nil {
		if *opts.OverrideShadowCasterGroupLimit > 0 {
			settings.casterGroupLimit = *opts.OverrideShadowCasterGroupLimit
		} else {
			settings.casterGroupLimit = 0
		}
	}
	return settings
}
