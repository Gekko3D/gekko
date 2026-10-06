package gekko

import (
	"fmt"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Only streamed legacy packets install this marker. Direct and compiled
// preparation retain their existing collapse/source resolution contracts.
type legacyPreparedCollapse struct {
	adopted *collapsedAuthoredVoxelBuild
}

type legacyCollapseCandidate struct {
	key             string
	geometry        *legacyAssetGeometry
	paletteKey      string
	palette         *compiledAssetPacketPalette
	voxelResolution float32
	partIDs         map[string]struct{}
}

// Compose the exact TRS used by TransformHierarchySystem, relative to an
// identity asset root. Parent voxel pivots/resolutions do not affect this TRS.
func legacyCollapseWorldTransforms(def *content.AssetDef) map[string]TransformComponent {
	definitions := make(map[string]content.AssetPartDef, len(def.Parts))
	for _, part := range def.Parts {
		definitions[part.ID] = part
	}
	worlds := make(map[string]TransformComponent, len(def.Parts))
	var resolve func(string) TransformComponent
	resolve = func(id string) TransformComponent {
		if world, exists := worlds[id]; exists {
			return world
		}
		part := definitions[id]
		parent := TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
		if part.ParentID != "" {
			parent = resolve(part.ParentID)
		}
		local := AssetLocalTransformFromDef(part.Transform)
		world := AssetTransformFromDef(part.Transform)
		position := mgl32.Vec3{local.Position.X() * parent.Scale.X(), local.Position.Y() * parent.Scale.Y(), local.Position.Z() * parent.Scale.Z()}
		world.Position = parent.Position.Add(parent.Rotation.Rotate(position))
		world.Rotation = parent.Rotation.Mul(local.Rotation).Normalize()
		world.Scale = mgl32.Vec3{parent.Scale.X() * local.Scale.X(), parent.Scale.Y() * local.Scale.Y(), parent.Scale.Z() * local.Scale.Z()}
		worlds[id] = world
		return world
	}
	for _, part := range def.Parts {
		resolve(part.ID)
	}
	return worlds
}

func resolveLegacyCollapseParts(def *content.AssetDef, source func(content.AssetPartDef) (VoxelGeometryAsset, VoxelPaletteAsset, error)) ([]authoredCollapseResolvedPart, error) {
	if len(def.Lights) > 0 || len(def.Emitters) > 0 || len(def.Markers) > 0 {
		return nil, fmt.Errorf("voxel collapse only supports voxel-backed parts and groups")
	}
	worlds := legacyCollapseWorldTransforms(def)
	parts := make([]authoredCollapseResolvedPart, 0, len(def.Parts))
	for _, part := range def.Parts {
		if part.Source.Kind == content.AssetSourceKindGroup {
			continue
		}
		geometry, palette, err := source(part)
		if err != nil {
			return nil, err
		}
		model := VoxelModelComponent{VoxelResolution: part.VoxelResolution}
		if part.Source.Kind == content.AssetSourceKindVoxelShape {
			model.PivotMode = PivotModeCustom
			model.CustomPivot = mgl32.Vec3(part.Transform.Pivot)
		}
		world := worlds[part.ID]
		world.Pivot = authoredCollapsePivot(model, geometry)
		parts = append(parts, authoredCollapseResolvedPart{def: part, world: world, geometry: geometry, paletteAsset: palette, voxelResolution: VoxelResolutionOrDefault(&model)})
	}
	return parts, nil
}

func resolvePreparedLegacyCollapseParts(assets *AssetServer, prepared *PreparedAuthoredAsset) ([]authoredCollapseResolvedPart, error) {
	if assets == nil {
		return nil, fmt.Errorf("voxel collapse requires asset server")
	}
	return resolveLegacyCollapseParts(prepared.def, func(part content.AssetPartDef) (VoxelGeometryAsset, VoxelPaletteAsset, error) {
		entry := prepared.parts[part.ID]
		geometry, ok := assets.getVoxelGeometry(entry.model)
		if !ok {
			return VoxelGeometryAsset{}, VoxelPaletteAsset{}, fmt.Errorf("missing geometry for part %s", part.ID)
		}
		palette, ok := assets.GetVoxelPalette(entry.palette)
		if !ok {
			return VoxelGeometryAsset{}, VoxelPaletteAsset{}, fmt.Errorf("missing palette for part %s", part.ID)
		}
		return geometry, palette, nil
	})
}

// Semantic collapse failures retain expanded fallback. Allocation/publication
// preparation failures remain real worker errors and release through the packet.
func prepareLegacyCollapseCandidate(packet *legacyAssetPacket, loader *RuntimeContentLoader, cancelled func() bool) (*legacyCollapseCandidate, error) {
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	parts, err := resolveLegacyCollapseParts(packet.def, func(part content.AssetPartDef) (VoxelGeometryAsset, VoxelPaletteAsset, error) {
		return packet.geometries[packet.parts[part.ID]].source, *packet.palettes[packet.partPalettes[part.ID]].source, nil
	})
	if err != nil || len(parts) < 2 {
		return nil, nil
	}
	var palette *VoxelPaletteAsset
	for _, part := range parts {
		if content.EffectiveAssetSourceOperation(part.def.Source) != content.AssetShapeOperationAdd || voxelGeometryIsEmpty(part.geometry) {
			continue
		}
		if palette == nil {
			copy := part.paletteAsset
			palette = &copy
		} else if !paletteAssetsEquivalent(*palette, part.paletteAsset) {
			return nil, nil
		}
	}
	if palette == nil {
		return nil, nil
	}
	resolution := parts[0].voxelResolution
	key, err := collapseGeometryCacheKey(packet.def, packet.documentPath, resolution)
	if err != nil {
		return nil, nil
	}
	combined := volume.NewXBrickMap()
	partIDs := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		if err := checkCompiledAssetWork(loader, cancelled); err != nil {
			return nil, err
		}
		if absf(part.voxelResolution-resolution) > 1e-5 {
			return nil, nil
		}
		partIDs[part.def.ID] = struct{}{}
		if voxelGeometryIsEmpty(part.geometry) {
			continue
		}
		if err := bakeResolvedPartIntoComposite(combined, part, resolution); err != nil {
			return nil, nil
		}
	}
	if err := checkCompiledAssetWork(loader, cancelled); err != nil {
		return nil, err
	}
	minB, maxB := combined.ComputeAABB()
	combined.ClearDirty()
	geometry := VoxelGeometryAsset{XBrickMap: combined, LocalMin: minB, LocalMax: maxB, BrickSize: [3]uint32{8, 8, 8}, SourcePath: key, RuntimeOwned: true}
	ownedPalette, err := clonePalettePublication(palette)
	if err != nil {
		return nil, err
	}
	paletteKey := voxelPaletteAssetCacheKey(*ownedPalette)
	registration, err := preparePaletteRegistrationWithKey(ownedPalette, paletteKey)
	if err != nil {
		return nil, err
	}
	return &legacyCollapseCandidate{key: key, geometry: &legacyAssetGeometry{source: geometry, registration: prepareLegacyGeometryRegistration(key, geometry)}, paletteKey: paletteKey, palette: &compiledAssetPacketPalette{source: ownedPalette, registration: registration}, voxelResolution: resolution, partIDs: partIDs}, nil
}

// The source geometry and palettes were actually cold-transferred while this
// same server lock remained held. No source alias has escaped in between.
func (assets *AssetServer) adoptLegacyCollapseCandidateLocked(candidate *legacyCollapseCandidate) *collapsedAuthoredVoxelBuild {
	if _, warm := assets.voxModelKeys[candidate.key]; warm {
		return nil
	}
	// The composite palette uses the ordinary full-asset palette namespace.
	// Its warm ID/content remains authoritative just as CreateVoxelPaletteAsset.
	palette, ok, _ := assets.adoptCompiledAssetPaletteLocked(candidate.paletteKey, candidate.palette.source, candidate.palette.registration)
	if !ok {
		return nil
	}
	id, ok, cold := assets.adoptLegacyGeometryLocked(candidate.key, candidate.geometry, candidate.geometry.registration)
	if !ok || !cold {
		return nil
	}
	// The public spawn result may mutate its part-ID set. It must not expose
	// the worker packet's immutable candidate metadata.
	partIDs := make(map[string]struct{}, len(candidate.partIDs))
	for id := range candidate.partIDs {
		partIDs[id] = struct{}{}
	}
	assets.authoredVoxelCollapseStats.Builds++
	assets.authoredVoxelCollapseStats.WorkerAdoptions++
	return &collapsedAuthoredVoxelBuild{geometry: id, palette: palette, voxelResolution: candidate.voxelResolution, collapsedPartIDs: partIDs}
}
