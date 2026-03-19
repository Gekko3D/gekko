package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type StreamedLevelObserverComponent struct {
	Radius int
}

type PostSpawnPlacementContext struct {
	ChunkCoord  ChunkCoord
	LevelID     string
	Placement   AuthoredPlacementSpawnDef
	RootEntity  EntityId
	SpawnResult AuthoredAssetSpawnResult
}

type PostSpawnPlacementHook func(cmd *Commands, ctx PostSpawnPlacementContext)

type PostSpawnTerrainContext struct {
	ChunkCoord ChunkCoord
	LevelID    string
	TerrainID  string
	RootEntity EntityId
}

type PostSpawnTerrainHook func(cmd *Commands, ctx PostSpawnTerrainContext)

type StreamedLevelRuntimeConfig struct {
	LevelPath          string
	Loader             *RuntimeContentLoader
	MaxVolumeInstances int
	TerrainGroupID     uint32
	PlacementHooks     []PostSpawnPlacementHook
	TerrainHooks       []PostSpawnTerrainHook
}

type StreamedLevelRuntimeModule struct{}

type StreamedLevelRuntimeState struct {
	mu sync.RWMutex

	Initialized bool
	InitErr     error

	Config                    StreamedLevelRuntimeConfig
	Loader                    *RuntimeContentLoader
	Level                     *content.LevelDef
	LevelID                   string
	LevelPath                 string
	LevelRoot                 EntityId
	ChunkSize                 float32
	StreamingRadius           int
	TerrainID                 string
	TerrainPalette            AssetId
	BaseWorldID               string
	BaseWorldPalette          AssetId
	BaseWorldCollisionEnabled bool

	DesiredChunks        map[ChunkCoord]struct{}
	PendingLoads         map[ChunkCoord]struct{}
	PreparedLoads        chan streamedPreparedChunk
	LoadedChunks         map[ChunkCoord]*streamedLoadedChunk
	PlacementsByChunk    map[ChunkCoord][]streamedPlacementInstance
	PlacementChunk       map[string]ChunkCoord
	ObjectChunk          map[string]ChunkCoord
	TerrainEntries       map[ChunkCoord]content.TerrainChunkEntryDef
	ImportedWorldEntries map[ChunkCoord]content.ImportedWorldChunkEntryDef

	WorldDeltaPath string
	WorldDataDir   string
	WorldDelta     *content.WorldDeltaDef

	placementOverrideMap map[string]content.LevelTransformDef
	deletedPlacementIDs  map[string]struct{}
	terrainOverrideMap   map[string]content.TerrainChunkOverrideDef
	voxelOverrideMap     map[string]content.VoxelObjectOverrideDef
}

type streamedPlacementInstance struct {
	PlacementID string
	VolumeID    string
	AssetPath   string
	Transform   content.LevelTransformDef
}

type streamedLoadedChunk struct {
	TerrainEntities       map[EntityId]struct{}
	ImportedWorldEntities map[EntityId]struct{}
	PlacementRoots        map[string]EntityId
	OwnedEntities         map[EntityId]struct{}
	ObjectEntities        map[string]EntityId
}

type streamedPreparedChunk struct {
	Coord              ChunkCoord
	TerrainChunk       *content.TerrainChunkDef
	ImportedWorldChunk *content.ImportedWorldChunkDef
	PlacementItems     []streamedPlacementInstance
	ObjectSnapshots    map[string]*content.VoxelObjectSnapshotDef
	Err                error
}

type streamedChunkLoadJob struct {
	Coord                     ChunkCoord
	LevelPath                 string
	Loader                    *RuntimeContentLoader
	TerrainManifestPath       string
	TerrainEntry              *content.TerrainChunkEntryDef
	TerrainOverride           *content.TerrainChunkOverrideDef
	ImportedWorldManifestPath string
	ImportedWorldEntry        *content.ImportedWorldChunkEntryDef
	Placements                []streamedPlacementInstance
	VoxelOverrides            map[string]content.VoxelObjectOverrideDef
	WorldDeltaPath            string
}

func (StreamedLevelRuntimeModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&StreamedLevelRuntimeState{
		DesiredChunks:        make(map[ChunkCoord]struct{}),
		PendingLoads:         make(map[ChunkCoord]struct{}),
		PreparedLoads:        make(chan streamedPreparedChunk, 256),
		LoadedChunks:         make(map[ChunkCoord]*streamedLoadedChunk),
		PlacementsByChunk:    make(map[ChunkCoord][]streamedPlacementInstance),
		PlacementChunk:       make(map[string]ChunkCoord),
		ObjectChunk:          make(map[string]ChunkCoord),
		TerrainEntries:       make(map[ChunkCoord]content.TerrainChunkEntryDef),
		ImportedWorldEntries: make(map[ChunkCoord]content.ImportedWorldChunkEntryDef),
		placementOverrideMap: make(map[string]content.LevelTransformDef),
		deletedPlacementIDs:  make(map[string]struct{}),
		terrainOverrideMap:   make(map[string]content.TerrainChunkOverrideDef),
		voxelOverrideMap:     make(map[string]content.VoxelObjectOverrideDef),
	})
	app.UseSystem(System(updateStreamedLevelObserverSystem).InStage(PreUpdate).RunAlways())
	app.UseSystem(System(commitPreparedStreamedChunksSystem).InStage(Update).RunAlways())
}

func StartStreamedLevelRuntime(cmd *Commands, assets *AssetServer, cfg StreamedLevelRuntimeConfig) error {
	if cmd == nil || cmd.app == nil {
		return fmt.Errorf("commands is nil")
	}
	state := streamedLevelRuntimeStateFromApp(cmd.app)
	if state == nil {
		return fmt.Errorf("streamed level runtime resource is missing")
	}
	if state.Initialized {
		return nil
	}
	if cfg.LevelPath == "" {
		return fmt.Errorf("level path is empty")
	}

	loader := cfg.Loader
	if loader == nil {
		loader = NewRuntimeContentLoader()
	}

	level, err := loader.LoadLevel(cfg.LevelPath)
	if err != nil {
		state.InitErr = err
		return err
	}
	if validation := content.ValidateLevel(level, content.LevelValidationOptions{DocumentPath: cfg.LevelPath}); validation.HasErrors() {
		err = fmt.Errorf("level validation failed: %s", validation.Error())
		state.InitErr = err
		return err
	}

	worldDeltaPath := content.DefaultWorldDeltaPath(cfg.LevelPath)
	worldDelta, err := content.LoadWorldDelta(worldDeltaPath)
	if err != nil {
		if !os.IsNotExist(err) {
			state.InitErr = err
			return err
		}
		worldDelta = &content.WorldDeltaDef{
			SchemaVersion: content.CurrentWorldDeltaSchemaVersion,
			LevelID:       level.ID,
		}
	}
	if worldDelta.LevelID == "" {
		worldDelta.LevelID = level.ID
	}
	if worldDelta.LevelID != level.ID {
		err = fmt.Errorf("world delta level id %q does not match level %q", worldDelta.LevelID, level.ID)
		state.InitErr = err
		return err
	}

	rootEntity := cmd.AddEntity(
		&TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&LocalTransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		&AuthoredLevelRootComponent{LevelID: level.ID},
	)

	chunkSize := float32(level.ChunkSize)
	if chunkSize <= 0 {
		chunkSize = 32
	}
	streamingRadius := level.StreamingRadius
	if streamingRadius < 0 {
		streamingRadius = 0
	}

	state.Initialized = true
	state.InitErr = nil
	state.Config = cfg
	state.Loader = loader
	state.Level = level
	state.LevelID = level.ID
	state.LevelPath = cfg.LevelPath
	state.LevelRoot = rootEntity
	state.ChunkSize = chunkSize
	state.StreamingRadius = streamingRadius
	state.WorldDeltaPath = worldDeltaPath
	state.WorldDataDir = content.DefaultWorldDeltaDataDir(worldDeltaPath)
	state.WorldDelta = worldDelta
	state.TerrainID = ""
	state.TerrainPalette = AssetId{}
	state.BaseWorldID = ""
	state.BaseWorldPalette = AssetId{}
	state.BaseWorldCollisionEnabled = false
	state.DesiredChunks = make(map[ChunkCoord]struct{})
	state.PendingLoads = make(map[ChunkCoord]struct{})
	state.LoadedChunks = make(map[ChunkCoord]*streamedLoadedChunk)
	state.PlacementsByChunk = make(map[ChunkCoord][]streamedPlacementInstance)
	state.PlacementChunk = make(map[string]ChunkCoord)
	state.ObjectChunk = make(map[string]ChunkCoord)
	state.TerrainEntries = make(map[ChunkCoord]content.TerrainChunkEntryDef)
	state.ImportedWorldEntries = make(map[ChunkCoord]content.ImportedWorldChunkEntryDef)
	state.placementOverrideMap = make(map[string]content.LevelTransformDef)
	state.deletedPlacementIDs = make(map[string]struct{})
	state.terrainOverrideMap = make(map[string]content.TerrainChunkOverrideDef)
	state.voxelOverrideMap = make(map[string]content.VoxelObjectOverrideDef)

	for _, override := range worldDelta.PlacementTransformOverrides {
		state.placementOverrideMap[override.PlacementID] = override.Transform
	}
	for _, deletion := range worldDelta.PlacementDeletions {
		state.deletedPlacementIDs[deletion.PlacementID] = struct{}{}
	}
	for _, override := range worldDelta.TerrainChunkOverrides {
		state.terrainOverrideMap[terrainChunkRuntimeKey(override.TerrainID, override.ChunkCoord)] = override
	}
	for _, override := range worldDelta.VoxelObjectOverrides {
		state.voxelOverrideMap[voxelObjectRuntimeKey(override.PlacementID, override.ItemID)] = override
	}

	if level.Terrain != nil && level.Terrain.ManifestPath != "" {
		manifestPath := content.ResolveDocumentPath(level.Terrain.ManifestPath, cfg.LevelPath)
		manifest, err := loader.LoadTerrainChunkManifest(manifestPath)
		if err != nil {
			state.InitErr = err
			return err
		}
		chunkWorldSize := float32(manifest.ChunkSize) * manifest.VoxelResolution * VoxelSize
		if absf(chunkWorldSize-chunkSize) > 1e-4 {
			err = fmt.Errorf("terrain chunk world size %.4f does not match level chunk size %.4f", chunkWorldSize, chunkSize)
			state.InitErr = err
			return err
		}
		state.TerrainID = manifest.TerrainID
		for _, entry := range manifest.Entries {
			state.TerrainEntries[chunkCoordFromTerrain(entry.Coord)] = entry
		}
		if assets != nil {
			state.TerrainPalette = assets.CreateSimplePalette([4]uint8{120, 120, 120, 255})
		}
	}
	if level.BaseWorld != nil && level.BaseWorld.ManifestPath != "" {
		manifestPath := content.ResolveDocumentPath(level.BaseWorld.ManifestPath, cfg.LevelPath)
		manifest, err := loader.LoadImportedWorld(manifestPath)
		if err != nil {
			state.InitErr = err
			return err
		}
		chunkWorldSize := float32(manifest.ChunkSize) * manifest.VoxelResolution * VoxelSize
		if absf(chunkWorldSize-chunkSize) > 1e-4 {
			err = fmt.Errorf("base world chunk world size %.4f does not match level chunk size %.4f", chunkWorldSize, chunkSize)
			state.InitErr = err
			return err
		}
		state.BaseWorldID = manifest.WorldID
		state.BaseWorldCollisionEnabled = level.BaseWorld.CollisionEnabled
		for _, entry := range manifest.Entries {
			state.ImportedWorldEntries[chunkCoordFromTerrain(entry.Coord)] = entry
		}
		if assets != nil {
			state.BaseWorldPalette = assets.CreateSimplePalette([4]uint8{160, 160, 160, 255})
		}
	}

	placements, err := buildEffectiveStreamedPlacementIndex(level, cfg.LevelPath, state.placementOverrideMap, state.deletedPlacementIDs, cfg.MaxVolumeInstances)
	if err != nil {
		state.InitErr = err
		return err
	}
	for _, placement := range placements {
		coord := ChunkCoordFromPosition(levelTransformToComponent(placement.Transform).Position, chunkSize)
		state.PlacementsByChunk[coord] = append(state.PlacementsByChunk[coord], placement)
		state.PlacementChunk[placement.PlacementID] = coord
	}

	applyLevelEnvironment(cmd, level.Environment)
	cmd.app.FlushCommands()
	return nil
}

func updateStreamedLevelObserverSystem(cmd *Commands, state *StreamedLevelRuntimeState) {
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}

	desired := make(map[ChunkCoord]struct{})
	MakeQuery2[TransformComponent, StreamedLevelObserverComponent](cmd).Map(func(id EntityId, transform *TransformComponent, observer *StreamedLevelObserverComponent) bool {
		if transform == nil || observer == nil {
			return true
		}
		radius := observer.Radius
		if radius <= 0 {
			radius = state.StreamingRadius
		}
		center := ChunkCoordFromPosition(transform.Position, state.ChunkSize)
		for _, coord := range center.NeighborsWithin(radius) {
			desired[coord] = struct{}{}
		}
		return true
	})

	state.DesiredChunks = desired
	for coord := range state.LoadedChunks {
		if _, ok := desired[coord]; ok {
			continue
		}
		if err := unloadStreamedChunk(cmd, state, coord); err != nil && state.InitErr == nil {
			state.InitErr = err
		}
	}
	if state.InitErr != nil {
		return
	}
	for coord := range desired {
		if _, ok := state.LoadedChunks[coord]; ok {
			continue
		}
		if _, ok := state.PendingLoads[coord]; ok {
			continue
		}
		state.PendingLoads[coord] = struct{}{}
		job := buildStreamedChunkLoadJob(state, coord)
		go func() {
			state.PreparedLoads <- prepareStreamedChunkLoad(job)
		}()
	}
}

func commitPreparedStreamedChunksSystem(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState) {
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}
	for {
		select {
		case prepared := <-state.PreparedLoads:
			delete(state.PendingLoads, prepared.Coord)
			if prepared.Err != nil {
				if state.InitErr == nil {
					state.InitErr = prepared.Err
				}
				continue
			}
			if _, stillDesired := state.DesiredChunks[prepared.Coord]; !stillDesired {
				continue
			}
			if _, alreadyLoaded := state.LoadedChunks[prepared.Coord]; alreadyLoaded {
				continue
			}
			if err := commitPreparedStreamedChunk(cmd, assets, state, prepared); err != nil {
				if state.InitErr == nil {
					state.InitErr = err
				}
			}
		default:
			return
		}
	}
}

func buildEffectiveStreamedPlacementIndex(level *content.LevelDef, levelPath string, overrides map[string]content.LevelTransformDef, deletions map[string]struct{}, maxVolumeInstances int) ([]streamedPlacementInstance, error) {
	placements := make([]streamedPlacementInstance, 0, len(level.Placements)+len(level.PlacementVolumes)*4)
	for _, placement := range level.Placements {
		placements = append(placements, streamedPlacementInstance{
			PlacementID: placement.ID,
			AssetPath:   placement.AssetPath,
			Transform:   effectiveLevelTransform(placement.ID, placement.Transform, overrides),
		})
	}
	if maxVolumeInstances <= 0 {
		maxVolumeInstances = DefaultRuntimeMaxVolumeInstances
	}
	for _, volumeDef := range level.PlacementVolumes {
		expanded, err := content.ExpandPlacementVolumePreview(volumeDef, content.PlacementVolumeExpandOptions{
			LevelDocumentPath: levelPath,
			MaxInstances:      maxVolumeInstances,
		})
		if err != nil {
			return nil, fmt.Errorf("expand placement volume %s: %w", volumeDef.ID, err)
		}
		for index, instance := range expanded.Instances {
			placementID := fmt.Sprintf("%s:%d", volumeDef.ID, index)
			placements = append(placements, streamedPlacementInstance{
				PlacementID: placementID,
				VolumeID:    volumeDef.ID,
				AssetPath:   authoredPathForLevel(instance.AssetPath, levelPath),
				Transform:   effectiveLevelTransform(placementID, instance.Transform, overrides),
			})
		}
	}

	filtered := placements[:0]
	for _, placement := range placements {
		if _, deleted := deletions[placement.PlacementID]; deleted {
			continue
		}
		filtered = append(filtered, placement)
	}
	return filtered, nil
}

func buildStreamedChunkLoadJob(state *StreamedLevelRuntimeState, coord ChunkCoord) streamedChunkLoadJob {
	job := streamedChunkLoadJob{
		Coord:          coord,
		LevelPath:      state.LevelPath,
		Loader:         state.Loader,
		Placements:     append([]streamedPlacementInstance(nil), state.PlacementsByChunk[coord]...),
		VoxelOverrides: make(map[string]content.VoxelObjectOverrideDef),
		WorldDeltaPath: state.WorldDeltaPath,
	}
	if entry, ok := state.TerrainEntries[coord]; ok {
		job.TerrainEntry = &entry
		job.TerrainManifestPath = content.ResolveDocumentPath(state.Level.Terrain.ManifestPath, state.LevelPath)
	}
	if entry, ok := state.ImportedWorldEntries[coord]; ok {
		job.ImportedWorldEntry = &entry
		job.ImportedWorldManifestPath = content.ResolveDocumentPath(state.Level.BaseWorld.ManifestPath, state.LevelPath)
	}
	if override, ok := state.terrainOverrideMap[terrainChunkRuntimeKey(state.TerrainID, terrainCoordFromChunk(coord))]; ok {
		overrideCopy := override
		job.TerrainOverride = &overrideCopy
	}
	for _, placement := range job.Placements {
		prefix := placement.PlacementID + "\x00"
		for key, override := range state.voxelOverrideMap {
			if len(key) > len(prefix) && key[:len(prefix)] == prefix {
				job.VoxelOverrides[key] = override
			}
		}
	}
	return job
}

func prepareStreamedChunkLoad(job streamedChunkLoadJob) streamedPreparedChunk {
	result := streamedPreparedChunk{
		Coord:           job.Coord,
		PlacementItems:  append([]streamedPlacementInstance(nil), job.Placements...),
		ObjectSnapshots: make(map[string]*content.VoxelObjectSnapshotDef),
	}
	if job.TerrainOverride != nil {
		chunkPath := content.ResolveDocumentPath(job.TerrainOverride.SnapshotPath, job.WorldDeltaPath)
		chunk, err := job.Loader.LoadTerrainChunk(chunkPath)
		if err != nil {
			result.Err = err
			return result
		}
		result.TerrainChunk = chunk
	} else if job.TerrainEntry != nil && job.TerrainEntry.NonEmptyVoxelCount > 0 {
		chunkPath := content.ResolveTerrainChunkPath(*job.TerrainEntry, job.TerrainManifestPath)
		chunk, err := job.Loader.LoadTerrainChunk(chunkPath)
		if err != nil {
			result.Err = err
			return result
		}
		result.TerrainChunk = chunk
	}
	if job.ImportedWorldEntry != nil && job.ImportedWorldEntry.NonEmptyVoxelCount > 0 {
		chunkPath := content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath)
		chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
		if err != nil {
			result.Err = err
			return result
		}
		result.ImportedWorldChunk = chunk
	}
	for key, override := range job.VoxelOverrides {
		snapshotPath := content.ResolveDocumentPath(override.SnapshotPath, job.WorldDeltaPath)
		snapshot, err := content.LoadVoxelObjectSnapshot(snapshotPath)
		if err != nil {
			result.Err = err
			return result
		}
		result.ObjectSnapshots[key] = snapshot
	}
	return result
}

func commitPreparedStreamedChunk(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedChunk) error {
	chunk := &streamedLoadedChunk{
		TerrainEntities:       make(map[EntityId]struct{}),
		ImportedWorldEntities: make(map[EntityId]struct{}),
		PlacementRoots:        make(map[string]EntityId),
		OwnedEntities:         make(map[EntityId]struct{}),
		ObjectEntities:        make(map[string]EntityId),
	}

	if prepared.TerrainChunk != nil && prepared.TerrainChunk.NonEmptyVoxelCount > 0 {
		entity := spawnAuthoredTerrainChunkEntity(cmd, state.LevelRoot, state.TerrainPalette, AuthoredTerrainSpawnDef{
			LevelID:        state.LevelID,
			TerrainID:      terrainIDForPreparedChunk(state, prepared.TerrainChunk),
			TerrainGroupID: terrainGroupIDForStreamedState(state),
			Chunk:          prepared.TerrainChunk,
		})
		cmd.app.FlushCommands()
		clearEntityVoxelDirty(cmd, entity)
		chunk.TerrainEntities[entity] = struct{}{}
		chunk.OwnedEntities[entity] = struct{}{}
		for _, hook := range state.Config.TerrainHooks {
			hook(cmd, PostSpawnTerrainContext{
				ChunkCoord: prepared.Coord,
				LevelID:    state.LevelID,
				TerrainID:  terrainIDForPreparedChunk(state, prepared.TerrainChunk),
				RootEntity: entity,
			})
		}
	}

	if prepared.ImportedWorldChunk != nil && prepared.ImportedWorldChunk.NonEmptyVoxelCount > 0 {
		entity := spawnAuthoredImportedWorldChunkEntity(cmd, state.LevelRoot, state.BaseWorldPalette, AuthoredImportedWorldSpawnDef{
			LevelID:          state.LevelID,
			WorldID:          importedWorldIDForPreparedChunk(state, prepared.ImportedWorldChunk),
			Chunk:            prepared.ImportedWorldChunk,
			CollisionEnabled: state.BaseWorldCollisionEnabled,
		})
		cmd.app.FlushCommands()
		clearEntityVoxelDirty(cmd, entity)
		chunk.ImportedWorldEntities[entity] = struct{}{}
		chunk.OwnedEntities[entity] = struct{}{}
	}

	for _, placement := range prepared.PlacementItems {
		spawnResult, err := spawnAuthoredLevelPlacement(cmd, assets, state.Loader, state.LevelRoot, state.LevelID, state.LevelPath, AuthoredPlacementSpawnDef{
			PlacementID: placement.PlacementID,
			VolumeID:    placement.VolumeID,
			AssetPath:   placement.AssetPath,
			Transform:   placement.Transform,
		})
		if err != nil {
			return err
		}
		cmd.app.FlushCommands()
		chunk.PlacementRoots[placement.PlacementID] = spawnResult.RootEntity
		chunk.OwnedEntities[spawnResult.RootEntity] = struct{}{}
		for _, eid := range spawnResult.EntitiesByAssetID {
			chunk.OwnedEntities[eid] = struct{}{}
			key := voxelObjectRuntimeKey(placement.PlacementID, authoredItemIDForEntity(cmd, eid))
			if snapshot, ok := prepared.ObjectSnapshots[key]; ok {
				if err := applyVoxelObjectSnapshotToEntity(cmd, eid, snapshot); err != nil {
					return err
				}
				cmd.app.FlushCommands()
			}
			if entityHasVoxelModel(cmd, eid) {
				chunk.ObjectEntities[key] = eid
				state.ObjectChunk[key] = prepared.Coord
				clearEntityVoxelDirty(cmd, eid)
			}
		}
		clearEntityVoxelDirty(cmd, spawnResult.RootEntity)
		for _, hook := range state.Config.PlacementHooks {
			hook(cmd, PostSpawnPlacementContext{
				ChunkCoord:  prepared.Coord,
				LevelID:     state.LevelID,
				Placement:   AuthoredPlacementSpawnDef{PlacementID: placement.PlacementID, VolumeID: placement.VolumeID, AssetPath: placement.AssetPath, Transform: placement.Transform},
				RootEntity:  spawnResult.RootEntity,
				SpawnResult: spawnResult,
			})
		}
	}

	state.LoadedChunks[prepared.Coord] = chunk
	return nil
}

func unloadStreamedChunk(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) error {
	loaded := state.LoadedChunks[coord]
	if loaded == nil {
		return nil
	}
	if err := persistChunkOverrides(cmd, state, coord, loaded); err != nil {
		return err
	}
	for eid := range loaded.OwnedEntities {
		cmd.RemoveEntity(eid)
	}
	for objectKey := range loaded.ObjectEntities {
		delete(state.ObjectChunk, objectKey)
	}
	delete(state.LoadedChunks, coord)
	return nil
}

func persistChunkOverrides(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord, loaded *streamedLoadedChunk) error {
	manifestDirty := false
	for eid := range loaded.TerrainEntities {
		if len(cmd.GetAllComponents(eid)) == 0 {
			continue
		}
		ref, ok := AuthoredTerrainChunkRefForEntity(cmd, eid)
		if !ok {
			continue
		}
		xbm, dirty, _ := currentVoxelMapForEntity(cmd, eid)
		if !dirty {
			continue
		}
		vmc, ok := voxelModelComponentForEntity(cmd, eid)
		if !ok {
			continue
		}
		snapshot := terrainChunkDefFromXBrickMap(ref.TerrainID, terrainCoordFromArray(ref.ChunkCoord), vmc.TerrainChunkSize, voxelResolutionForEntity(cmd, eid), xbm)
		snapshotPath := filepath.Join(state.WorldDataDir, fmt.Sprintf("terrain_%s_%d_%d_%d.gkchunk", ref.TerrainID, ref.ChunkCoord[0], ref.ChunkCoord[1], ref.ChunkCoord[2]))
		if err := content.SaveTerrainChunk(snapshotPath, snapshot); err != nil {
			return err
		}
		override := content.TerrainChunkOverrideDef{
			TerrainID:    ref.TerrainID,
			ChunkCoord:   terrainCoordFromArray(ref.ChunkCoord),
			SnapshotPath: content.AuthorDocumentPath(snapshotPath, state.WorldDeltaPath),
		}
		state.terrainOverrideMap[terrainChunkRuntimeKey(ref.TerrainID, override.ChunkCoord)] = override
		manifestDirty = true
	}

	for objectKey, eid := range loaded.ObjectEntities {
		placementID, itemID := splitVoxelObjectRuntimeKey(objectKey)
		xbm, dirty, exists := currentVoxelMapForEntity(cmd, eid)
		if !dirty && exists {
			continue
		}
		snapshotPath := filepath.Join(state.WorldDataDir, fmt.Sprintf("object_%s_%s.gkvoxobj", sanitizePathSegment(placementID), sanitizePathSegment(itemID)))
		if err := content.SaveVoxelObjectSnapshot(snapshotPath, VoxelObjectSnapshotFromXBrickMap(xbm)); err != nil {
			return err
		}
		override := content.VoxelObjectOverrideDef{
			PlacementID:  placementID,
			ItemID:       itemID,
			SnapshotPath: content.AuthorDocumentPath(snapshotPath, state.WorldDeltaPath),
		}
		state.voxelOverrideMap[objectKey] = override
		manifestDirty = true
	}

	if !manifestDirty {
		return nil
	}
	state.WorldDelta.PlacementTransformOverrides = mapPlacementOverrides(state.placementOverrideMap)
	state.WorldDelta.PlacementDeletions = mapPlacementDeletions(state.deletedPlacementIDs)
	state.WorldDelta.TerrainChunkOverrides = mapTerrainOverrides(state.terrainOverrideMap)
	state.WorldDelta.VoxelObjectOverrides = mapVoxelOverrides(state.voxelOverrideMap)
	return content.SaveWorldDelta(state.WorldDeltaPath, state.WorldDelta)
}

func applyVoxelObjectSnapshotToEntity(cmd *Commands, eid EntityId, snapshot *content.VoxelObjectSnapshotDef) error {
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if !ok {
		return nil
	}
	vmc.CustomMap = XBrickMapFromVoxelObjectSnapshot(snapshot)
	cmd.AddComponents(eid, &vmc)
	return nil
}

func currentVoxelMapForEntity(cmd *Commands, eid EntityId) (*volume.XBrickMap, bool, bool) {
	if len(cmd.GetAllComponents(eid)) == 0 {
		return nil, true, false
	}
	if state := voxelRtStateFromApp(cmd.app); state != nil {
		if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
			return obj.XBrickMap, isVoxelMapDirty(obj.XBrickMap), true
		}
	}
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if !ok || vmc.CustomMap == nil {
		return nil, false, true
	}
	return vmc.CustomMap, isVoxelMapDirty(vmc.CustomMap), true
}

func clearEntityVoxelDirty(cmd *Commands, eid EntityId) {
	if state := voxelRtStateFromApp(cmd.app); state != nil {
		if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
			obj.XBrickMap.ClearDirty()
		}
	}
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if ok && vmc.CustomMap != nil {
		vmc.CustomMap.ClearDirty()
	}
}

func voxelModelComponentForEntity(cmd *Commands, eid EntityId) (VoxelModelComponent, bool) {
	for _, comp := range cmd.GetAllComponents(eid) {
		if vmc, ok := comp.(*VoxelModelComponent); ok {
			return *vmc, true
		}
		if vmc, ok := comp.(VoxelModelComponent); ok {
			return vmc, true
		}
	}
	return VoxelModelComponent{}, false
}

func authoredItemIDForEntity(cmd *Commands, eid EntityId) string {
	itemRef, ok := AuthoredLevelItemRefForEntity(cmd, eid)
	if ok {
		return itemRef.ItemID
	}
	assetRef, ok := AuthoredAssetRefForEntity(cmd, eid)
	if ok {
		return assetRef.ItemID
	}
	return ""
}

func entityHasVoxelModel(cmd *Commands, eid EntityId) bool {
	_, ok := voxelModelComponentForEntity(cmd, eid)
	return ok
}

func terrainIDForPreparedChunk(state *StreamedLevelRuntimeState, chunk *content.TerrainChunkDef) string {
	if chunk != nil && chunk.TerrainID != "" {
		return chunk.TerrainID
	}
	return state.TerrainID
}

func importedWorldIDForPreparedChunk(state *StreamedLevelRuntimeState, chunk *content.ImportedWorldChunkDef) string {
	if chunk != nil && chunk.WorldID != "" {
		return chunk.WorldID
	}
	return state.BaseWorldID
}

func terrainGroupIDForStreamedState(state *StreamedLevelRuntimeState) uint32 {
	if state.Config.TerrainGroupID != 0 {
		return state.Config.TerrainGroupID
	}
	return stableTerrainGroupID(state.LevelID, state.TerrainID)
}

func effectiveLevelTransform(placementID string, authored content.LevelTransformDef, overrides map[string]content.LevelTransformDef) content.LevelTransformDef {
	if override, ok := overrides[placementID]; ok {
		return override
	}
	return authored
}

func streamedLevelRuntimeStateFromApp(app *App) *StreamedLevelRuntimeState {
	if app == nil {
		return nil
	}
	if resource, ok := app.resources[reflect.TypeOf(StreamedLevelRuntimeState{})]; ok {
		return resource.(*StreamedLevelRuntimeState)
	}
	return nil
}

func voxelRtStateFromApp(app *App) *VoxelRtState {
	if app == nil {
		return nil
	}
	if resource, ok := app.resources[reflect.TypeOf(VoxelRtState{})]; ok {
		return resource.(*VoxelRtState)
	}
	return nil
}

func terrainChunkRuntimeKey(terrainID string, coord content.TerrainChunkCoordDef) string {
	return terrainID + "|" + content.TerrainChunkKey(coord)
}

func voxelObjectRuntimeKey(placementID string, itemID string) string {
	return placementID + "\x00" + itemID
}

func splitVoxelObjectRuntimeKey(key string) (string, string) {
	for i := 0; i < len(key); i++ {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

func terrainCoordFromChunk(coord ChunkCoord) content.TerrainChunkCoordDef {
	return content.TerrainChunkCoordDef{X: coord.X, Y: coord.Y, Z: coord.Z}
}

func chunkCoordFromTerrain(coord content.TerrainChunkCoordDef) ChunkCoord {
	return ChunkCoord{X: coord.X, Y: coord.Y, Z: coord.Z}
}

func terrainCoordFromArray(coord [3]int) content.TerrainChunkCoordDef {
	return content.TerrainChunkCoordDef{X: coord[0], Y: coord[1], Z: coord[2]}
}

func isVoxelMapDirty(xbm *volume.XBrickMap) bool {
	if xbm == nil {
		return false
	}
	return xbm.StructureDirty || len(xbm.DirtyBricks) > 0 || len(xbm.DirtySectors) > 0
}

func voxelResolutionForEntity(cmd *Commands, eid EntityId) float32 {
	for _, comp := range cmd.GetAllComponents(eid) {
		if tr, ok := comp.(*TransformComponent); ok {
			return tr.Scale.X()
		}
		if tr, ok := comp.(TransformComponent); ok {
			return tr.Scale.X()
		}
	}
	return 1
}

func mapPlacementOverrides(src map[string]content.LevelTransformDef) []content.PlacementTransformOverrideDef {
	out := make([]content.PlacementTransformOverrideDef, 0, len(src))
	for placementID, transform := range src {
		out = append(out, content.PlacementTransformOverrideDef{PlacementID: placementID, Transform: transform})
	}
	return out
}

func mapPlacementDeletions(src map[string]struct{}) []content.PlacementDeletionDef {
	out := make([]content.PlacementDeletionDef, 0, len(src))
	for placementID := range src {
		out = append(out, content.PlacementDeletionDef{PlacementID: placementID})
	}
	return out
}

func mapTerrainOverrides(src map[string]content.TerrainChunkOverrideDef) []content.TerrainChunkOverrideDef {
	out := make([]content.TerrainChunkOverrideDef, 0, len(src))
	for _, override := range src {
		out = append(out, override)
	}
	return out
}

func mapVoxelOverrides(src map[string]content.VoxelObjectOverrideDef) []content.VoxelObjectOverrideDef {
	out := make([]content.VoxelObjectOverrideDef, 0, len(src))
	for _, override := range src {
		out = append(out, override)
	}
	return out
}

func sanitizePathSegment(value string) string {
	if value == "" {
		return "empty"
	}
	out := make([]byte, 0, len(value))
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			out = append(out, c)
			continue
		}
		out = append(out, '_')
	}
	return string(out)
}
