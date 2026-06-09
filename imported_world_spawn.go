package gekko

import (
	"hash/fnv"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type AuthoredImportedWorldSpawnDef struct {
	LevelID                 string
	WorldID                 string
	ShadowGroupID           uint32
	Chunk                   *content.ImportedWorldChunkDef
	CollisionEnabled        bool
	DestructionEnabled      bool
	DisableTerrainMetadata  bool
	DisableVoxelAdjacency   bool
	DisableShadows          bool
	DisableOcclusionCulling bool
	ShareTerrainGeometry    bool
	RetainRendererGeometry  bool
	PreparedGeometry        *volume.XBrickMap
	PreparedGeometryAsset   AssetId
	Timing                  *AuthoredImportedWorldSpawnTiming
}

type AuthoredImportedWorldSpawnTiming struct {
	VoxelCount                      int
	GeometryBuildDuration           time.Duration
	GeometryRegistrationDuration    time.Duration
	EntityAndComponentSpawnDuration time.Duration
}

func ImportedWorldChunkToXBrickMap(chunk *content.ImportedWorldChunkDef) *volume.XBrickMap {
	xbm := volume.NewXBrickMap()
	if chunk == nil {
		return xbm
	}
	for _, voxel := range chunk.Voxels {
		if voxel.Value == 0 {
			continue
		}
		xbm.SetVoxel(voxel.X, voxel.Y, voxel.Z, content.ImportedWorldVoxelMaterialValue(voxel))
	}
	return xbm
}

func spawnAuthoredImportedWorldChunkEntity(cmd *Commands, parent EntityId, palette AssetId, def AuthoredImportedWorldSpawnDef) EntityId {
	if cmd == nil || def.Chunk == nil {
		return 0
	}
	if def.Timing != nil {
		*def.Timing = AuthoredImportedWorldSpawnTiming{
			VoxelCount: def.Chunk.NonEmptyVoxelCount,
		}
		if def.Timing.VoxelCount <= 0 {
			def.Timing.VoxelCount = len(def.Chunk.Voxels)
		}
	}
	overrideGeometry := def.PreparedGeometryAsset
	if assets := assetServerFromApp(cmd.app); assets != nil {
		if overrideGeometry == (AssetId{}) {
			xbm := def.PreparedGeometry
			if xbm == nil {
				buildStart := time.Now()
				xbm = ImportedWorldChunkToXBrickMap(def.Chunk)
				if def.Timing != nil {
					def.Timing.GeometryBuildDuration += time.Since(buildStart)
				}
			}
			registerStart := time.Now()
			overrideGeometry = assets.RegisterSharedVoxelGeometry(xbm, "")
			if def.Timing != nil {
				def.Timing.GeometryRegistrationDuration += time.Since(registerStart)
			}
		}
	}
	isTerrainChunk := !def.DisableTerrainMetadata
	terrainGroupID := uint32(0)
	terrainChunkCoord := [3]int{}
	terrainChunkSize := 0
	voxelAdjacencyGroupID := uint32(0)
	voxelAdjacencyChunkCoord := [3]int{}
	voxelAdjacencyChunkSize := 0
	shadowSeamWorldEpsilon := float32(0)
	if isTerrainChunk {
		terrainGroupID = def.ShadowGroupID
		terrainChunkCoord = [3]int{def.Chunk.Coord.X, def.Chunk.Coord.Y, def.Chunk.Coord.Z}
		terrainChunkSize = def.Chunk.ChunkSize
		shadowSeamWorldEpsilon = def.Chunk.VoxelResolution
	}
	if !def.DisableVoxelAdjacency {
		voxelAdjacencyGroupID = def.ShadowGroupID
		voxelAdjacencyChunkCoord = [3]int{def.Chunk.Coord.X, def.Chunk.Coord.Y, def.Chunk.Coord.Z}
		voxelAdjacencyChunkSize = def.Chunk.ChunkSize
	}
	comps := []any{
		&TransformComponent{
			Position: importedWorldChunkPosition(def.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{def.Chunk.VoxelResolution / VoxelSize, def.Chunk.VoxelResolution / VoxelSize, def.Chunk.VoxelResolution / VoxelSize},
		},
		&LocalTransformComponent{
			Position: importedWorldChunkPosition(def.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{def.Chunk.VoxelResolution / VoxelSize, def.Chunk.VoxelResolution / VoxelSize, def.Chunk.VoxelResolution / VoxelSize},
		},
		&Parent{Entity: parent},
		&VoxelModelComponent{
			VoxelPalette:             palette,
			PivotMode:                PivotModeCorner,
			OverrideGeometry:         overrideGeometry,
			DisableShadows:           def.DisableShadows,
			DisableOcclusionCulling:  def.DisableOcclusionCulling,
			ShadowGroupID:            def.ShadowGroupID,
			ShadowSeamWorldEpsilon:   shadowSeamWorldEpsilon,
			VoxelAdjacencyGroupID:    voxelAdjacencyGroupID,
			VoxelAdjacencyChunkCoord: voxelAdjacencyChunkCoord,
			VoxelAdjacencyChunkSize:  voxelAdjacencyChunkSize,
			IsTerrainChunk:           isTerrainChunk,
			ShareTerrainGeometry:     def.ShareTerrainGeometry,
			RetainRendererGeometry:   def.RetainRendererGeometry,
			TerrainGroupID:           terrainGroupID,
			TerrainChunkCoord:        terrainChunkCoord,
			TerrainChunkSize:         terrainChunkSize,
		},
		&AuthoredImportedWorldChunkRefComponent{
			LevelID:    def.LevelID,
			WorldID:    def.WorldID,
			ChunkCoord: [3]int{def.Chunk.Coord.X, def.Chunk.Coord.Y, def.Chunk.Coord.Z},
		},
	}
	if def.CollisionEnabled {
		comps = append(comps,
			&RigidBodyComponent{BodyMode: BodyModeStatic},
			&ColliderComponent{},
			&AABBComponent{},
		)
	}
	if def.DestructionEnabled {
		comps = append(comps, &StreamedDestructionResidentComponent{
			LevelID:    def.LevelID,
			WorldID:    def.WorldID,
			ChunkCoord: [3]int{def.Chunk.Coord.X, def.Chunk.Coord.Y, def.Chunk.Coord.Z},
		})
	}
	entityStart := time.Now()
	entity := cmd.AddEntity(comps...)
	if def.Timing != nil {
		def.Timing.EntityAndComponentSpawnDuration += time.Since(entityStart)
	}
	return entity
}

func importedWorldChunkPosition(chunk *content.ImportedWorldChunkDef) mgl32.Vec3 {
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

func stableImportedWorldGroupID(levelID string, worldID string) uint32 {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(levelID))
	_, _ = hasher.Write([]byte{0})
	_, _ = hasher.Write([]byte(worldID))
	value := hasher.Sum32()
	if value == 0 {
		return 1
	}
	return value
}

func ImportedWorldPaletteAsset(assets *AssetServer, def *content.ImportedWorldDef) AssetId {
	if assets == nil || def == nil || (len(def.Palette) == 0 && len(def.MaterialPalette) == 0) {
		return AssetId{}
	}
	var palette VoxPalette
	nonZero := false
	paletteColors := def.Palette
	if len(def.MaterialPalette) > 0 {
		paletteColors = def.MaterialPalette
	}
	for i, color := range paletteColors {
		if i >= len(palette) {
			break
		}
		palette[i] = [4]uint8{color[0], color[1], color[2], color[3]}
		if color != (content.ImportedWorldPaletteColor{}) {
			nonZero = true
		}
	}
	if !nonZero {
		return AssetId{}
	}
	materials := importedWorldVoxMaterials(def.Materials)
	return assets.CreateVoxelPaletteAsset(VoxelPaletteAsset{
		VoxPalette: palette,
		Materials:  materials,
		Animations: importedWorldVoxelPaletteAnimations(def.MaterialAnimations),
	})
}

func importedWorldVoxMaterials(materials []content.ImportedWorldMaterialDef) []VoxMaterial {
	out := make([]VoxMaterial, 0, len(materials))
	for _, material := range materials {
		if material.PaletteIndex == 0 {
			continue
		}
		props := map[string]interface{}{
			"_rough": material.Roughness,
			"_metal": material.Metallic,
			"_trans": material.Transparency,
		}
		if material.EmitsLight || material.Emissive > 0 {
			emissive := material.Emissive
			if emissive <= 0 {
				emissive = 2.0
			}
			props["_type"] = "_emit"
			props["_emit"] = emissive
		} else if material.Transparent {
			transparency := material.Transparency
			if transparency <= 0 {
				transparency = 0.35
			}
			props["_type"] = "_glass"
			props["_trans"] = transparency
		} else if material.Metallic > 0.5 {
			props["_type"] = "_metal"
		} else {
			props["_type"] = "_diffuse"
		}
		out = append(out, VoxMaterial{
			ID:       int(material.PaletteIndex),
			Property: props,
		})
	}
	return out
}

func importedWorldVoxelPaletteAnimations(animations []content.ImportedWorldMaterialAnimationDef) []VoxelPaletteAnimation {
	out := make([]VoxelPaletteAnimation, 0, len(animations))
	for _, animation := range animations {
		if animation.ID == "" || len(animation.PaletteIndices) == 0 || len(animation.Frames) == 0 {
			continue
		}
		frames := make([]VoxelPaletteAnimationFrame, 0, len(animation.Frames))
		for _, frame := range animation.Frames {
			colors := make([][4]uint8, 0, len(frame.Colors))
			for _, color := range frame.Colors {
				colors = append(colors, [4]uint8{color[0], color[1], color[2], color[3]})
			}
			emissiveColors := make([][4]uint8, 0, len(frame.EmissiveColors))
			for _, color := range frame.EmissiveColors {
				emissiveColors = append(emissiveColors, [4]uint8{color[0], color[1], color[2], color[3]})
			}
			if len(colors) == 0 && len(emissiveColors) == 0 && len(frame.Emission) == 0 && len(frame.Roughness) == 0 && len(frame.Transparency) == 0 {
				continue
			}
			frames = append(frames, VoxelPaletteAnimationFrame{
				Duration:       frame.Duration,
				Colors:         colors,
				EmissiveColors: emissiveColors,
				Emission:       append([]float32(nil), frame.Emission...),
				Roughness:      append([]float32(nil), frame.Roughness...),
				Transparency:   append([]float32(nil), frame.Transparency...),
			})
		}
		if len(frames) == 0 {
			continue
		}
		var uvScroll *VoxelPaletteUVScroll
		if animation.UVScroll != nil {
			uvScroll = &VoxelPaletteUVScroll{Velocity: animation.UVScroll.Velocity}
		}
		out = append(out, VoxelPaletteAnimation{
			ID:             animation.ID,
			Kind:           animation.Kind,
			FPS:            animation.FPS,
			Mode:           animation.Mode,
			PaletteIndices: append([]uint8(nil), animation.PaletteIndices...),
			Frames:         frames,
			UVScroll:       uvScroll,
			Tags:           append([]string(nil), animation.Tags...),
		})
	}
	return out
}
