package gekko

import (
	"hash/fnv"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type AuthoredImportedWorldSpawnDef struct {
	LevelID          string
	WorldID          string
	ShadowGroupID    uint32
	Chunk            *content.ImportedWorldChunkDef
	CollisionEnabled bool
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
		xbm.SetVoxel(voxel.X, voxel.Y, voxel.Z, voxel.Value)
	}
	return xbm
}

func spawnAuthoredImportedWorldChunkEntity(cmd *Commands, parent EntityId, palette AssetId, def AuthoredImportedWorldSpawnDef) EntityId {
	if cmd == nil || def.Chunk == nil {
		return 0
	}
	comps := []any{
		&TransformComponent{
			Position: importedWorldChunkPosition(def.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{def.Chunk.VoxelResolution, def.Chunk.VoxelResolution, def.Chunk.VoxelResolution},
		},
		&LocalTransformComponent{
			Position: importedWorldChunkPosition(def.Chunk),
			Rotation: mgl32.QuatIdent(),
			Scale:    mgl32.Vec3{def.Chunk.VoxelResolution, def.Chunk.VoxelResolution, def.Chunk.VoxelResolution},
		},
		&Parent{Entity: parent},
		&VoxelModelComponent{
			VoxelPalette:           palette,
			PivotMode:              PivotModeCorner,
			CustomMap:              ImportedWorldChunkToXBrickMap(def.Chunk),
			ShadowGroupID:          def.ShadowGroupID,
			ShadowSeamWorldEpsilon: def.Chunk.VoxelResolution,
		},
		&AuthoredImportedWorldChunkRefComponent{
			LevelID:    def.LevelID,
			WorldID:    def.WorldID,
			ChunkCoord: [3]int{def.Chunk.Coord.X, def.Chunk.Coord.Y, def.Chunk.Coord.Z},
		},
	}
	if def.CollisionEnabled {
		comps = append(comps,
			&RigidBodyComponent{IsStatic: true},
			&ColliderComponent{},
			&AABBComponent{},
		)
	}
	return cmd.AddEntity(comps...)
}

func importedWorldChunkPosition(chunk *content.ImportedWorldChunkDef) mgl32.Vec3 {
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
	if assets == nil || def == nil || len(def.Palette) == 0 {
		return AssetId{}
	}
	var palette VoxPalette
	nonZero := false
	for i, color := range def.Palette {
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
	return assets.CreateVoxelPalette(palette, nil)
}
