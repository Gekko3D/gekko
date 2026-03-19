package gekko

import (
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type AuthoredImportedWorldSpawnDef struct {
	LevelID          string
	WorldID          string
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
			VoxelPalette: palette,
			PivotMode:    PivotModeCorner,
			CustomMap:    ImportedWorldChunkToXBrickMap(def.Chunk),
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
