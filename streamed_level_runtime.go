package gekko

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

type StreamedLevelObserverComponent struct {
	Radius            int
	KeepRadius        int
	PrefetchRadius    int
	CollisionRadius   int
	DestructionRadius int
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
	LevelPath                  string
	Loader                     *RuntimeContentLoader
	StreamingRadius            int
	StreamingKeepRadius        int
	StreamingPrefetchRadius    int
	StreamingCollisionRadius   int
	StreamingDestructionRadius int
	MaxVolumeInstances         int
	MaxPrepareJobs             int
	MaxChunkCommitsPerFrame    int
	MaxStreamingCommitMillis   int
	MetricsLogInterval         time.Duration
	DisableSectorProxies       bool
	RetainSectorProxies        bool
	MetricsSink                func(StreamedLevelRuntimeMetrics)
	TerrainGroupID             uint32
	AutoSpawnPlayer            bool
	PlayerSpawnKind            string
	PlayerConfig               *GroundedPlayerControllerConfig
	PlacementHooks             []PostSpawnPlacementHook
	TerrainHooks               []PostSpawnTerrainHook
}

type StreamedLevelRuntimeMetrics struct {
	DesiredChunkCount                 int
	DesiredLoadableChunkCount         int
	KeepChunkCount                    int
	KeepLoadableChunkCount            int
	CollisionChunkCount               int
	CollisionLoadableChunkCount       int
	DestructionChunkCount             int
	DestructionLoadableChunkCount     int
	DesiredSectorCount                int
	KeepSectorCount                   int
	DesiredSectorFullLoadedCount      int
	KeepSectorFullLoadedCount         int
	PendingLoadCount                  int
	PendingProxyLoadCount             int
	ActivePrepareJobCount             int
	ActiveChunkPrepareJobCount        int
	ActiveProxyPrepareJobCount        int
	PreparedQueueDepth                int
	PreparedChunkQueueDepth           int
	PreparedProxyQueueDepth           int
	LoadedChunkCount                  int
	LoadedSectorProxyCount            int
	LoadedSectorProxyFullReadyCount   int
	LoadedSectorProxyFullPendingCount int
	LoadedSectorProxyOutOfKeepCount   int
	ChunksCommittedLastFrame          int
	ProxyChunksCommittedLastFrame     int
	FullChunksCommittedLastFrame      int
	CollisionChunksCommittedLastFrame int
	EntitiesCommittedLastFrame        int
	CommitBudgetHitLastFrame          bool
	CommitBudgetReason                string

	PreparedChunkCount   int
	PrepareErrorCount    int
	LastPrepareCoord     ChunkCoord
	LastPrepareDuration  time.Duration
	TotalPrepareDuration time.Duration

	CommittedChunkCount             int
	ProxyChunkCommitCount           int
	FullChunkCommitCount            int
	CollisionChunkCommitCount       int
	CommitErrorCount                int
	LastCommitCoord                 ChunkCoord
	LastCommitEntityCount           int
	LastCommitFlushCount            int
	LastCommitDuration              time.Duration
	LastCommitTerrainDuration       time.Duration
	LastCommitWorldDuration         time.Duration
	LastCommitWorldVoxelCount       int
	LastCommitWorldBuildDuration    time.Duration
	LastCommitWorldRegisterDuration time.Duration
	LastCommitWorldEntityDuration   time.Duration
	LastCommitPlacementDuration     time.Duration
	LastCommitFlushDuration         time.Duration
	TotalCommitDuration             time.Duration

	ObserverUpdateDuration time.Duration
	CommitSystemDuration   time.Duration

	GPUVoxelSectorsUploaded        int
	GPUVoxelBricksUploaded         int
	GPUVoxelDirtySectorsPending    int
	GPUVoxelDirtyBricksPending     int
	GPUVoxelUploadRevision         uint64
	RendererSceneStructureRevision uint64
}

func (m StreamedLevelRuntimeMetrics) LogLine() string {
	return fmt.Sprintf(
		"streaming metrics: desired=%d desired_loadable=%d keep=%d keep_loadable=%d collision=%d collision_loadable=%d destruction=%d destruction_loadable=%d desired_sectors=%d desired_sectors_full=%d keep_sectors=%d keep_sectors_full=%d pending=%d pending_proxy=%d active_prepare=%d active_prepare_chunks=%d active_prepare_proxies=%d prepared_queue=%d prepared_chunks=%d prepared_proxies=%d loaded=%d loaded_proxies=%d proxy_full_ready=%d proxy_full_pending=%d proxy_out_of_keep=%d committed_frame=%d proxy_committed_frame=%d full_committed_frame=%d collision_committed_frame=%d entities_frame=%d budget_hit=%t budget_reason=%s prepared_total=%d prepare_last_ms=%.3f prepare_total_ms=%.3f committed_total=%d proxy_committed_total=%d full_committed_total=%d collision_committed_total=%d commit_last_ms=%.3f commit_terrain_ms=%.3f commit_world_ms=%.3f commit_world_voxels=%d commit_world_build_ms=%.3f commit_world_register_ms=%.3f commit_world_entity_ms=%.3f commit_placements_ms=%.3f commit_flush_ms=%.3f commit_flushes=%d commit_total_ms=%.3f commit_system_ms=%.3f gpu_voxel_sectors_up=%d gpu_voxel_bricks_up=%d gpu_voxel_dirty_sectors=%d gpu_voxel_dirty_bricks=%d gpu_upload_revision=%d scene_structure_revision=%d",
		m.DesiredChunkCount,
		m.DesiredLoadableChunkCount,
		m.KeepChunkCount,
		m.KeepLoadableChunkCount,
		m.CollisionChunkCount,
		m.CollisionLoadableChunkCount,
		m.DestructionChunkCount,
		m.DestructionLoadableChunkCount,
		m.DesiredSectorCount,
		m.DesiredSectorFullLoadedCount,
		m.KeepSectorCount,
		m.KeepSectorFullLoadedCount,
		m.PendingLoadCount,
		m.PendingProxyLoadCount,
		m.ActivePrepareJobCount,
		m.ActiveChunkPrepareJobCount,
		m.ActiveProxyPrepareJobCount,
		m.PreparedQueueDepth,
		m.PreparedChunkQueueDepth,
		m.PreparedProxyQueueDepth,
		m.LoadedChunkCount,
		m.LoadedSectorProxyCount,
		m.LoadedSectorProxyFullReadyCount,
		m.LoadedSectorProxyFullPendingCount,
		m.LoadedSectorProxyOutOfKeepCount,
		m.ChunksCommittedLastFrame,
		m.ProxyChunksCommittedLastFrame,
		m.FullChunksCommittedLastFrame,
		m.CollisionChunksCommittedLastFrame,
		m.EntitiesCommittedLastFrame,
		m.CommitBudgetHitLastFrame,
		m.CommitBudgetReason,
		m.PreparedChunkCount,
		durationMillis(m.LastPrepareDuration),
		durationMillis(m.TotalPrepareDuration),
		m.CommittedChunkCount,
		m.ProxyChunkCommitCount,
		m.FullChunkCommitCount,
		m.CollisionChunkCommitCount,
		durationMillis(m.LastCommitDuration),
		durationMillis(m.LastCommitTerrainDuration),
		durationMillis(m.LastCommitWorldDuration),
		m.LastCommitWorldVoxelCount,
		durationMillis(m.LastCommitWorldBuildDuration),
		durationMillis(m.LastCommitWorldRegisterDuration),
		durationMillis(m.LastCommitWorldEntityDuration),
		durationMillis(m.LastCommitPlacementDuration),
		durationMillis(m.LastCommitFlushDuration),
		m.LastCommitFlushCount,
		durationMillis(m.TotalCommitDuration),
		durationMillis(m.CommitSystemDuration),
		m.GPUVoxelSectorsUploaded,
		m.GPUVoxelBricksUploaded,
		m.GPUVoxelDirtySectorsPending,
		m.GPUVoxelDirtyBricksPending,
		m.GPUVoxelUploadRevision,
		m.RendererSceneStructureRevision,
	)
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

type StreamedLevelRuntimeModule struct{}

type StreamedLevelRuntimeState struct {
	mu sync.RWMutex

	Initialized bool
	InitErr     error

	Config                     StreamedLevelRuntimeConfig
	Loader                     *RuntimeContentLoader
	Level                      *content.LevelDef
	LevelID                    string
	LevelPath                  string
	LevelRoot                  EntityId
	ChunkSize                  float32
	StreamingRadius            int
	StreamingKeepRadius        int
	StreamingPrefetchRadius    int
	StreamingCollisionRadius   int
	StreamingDestructionRadius int
	TerrainID                  string
	TerrainPalette             AssetId
	BaseWorldID                string
	BaseWorldManifest          *content.ImportedWorldDef
	BaseWorldPalette           AssetId
	BaseWorldMaterialLookup    ImportedWorldMaterialLookup
	BaseWorldCollisionEnabled  bool
	MarkerEntities             map[string]EntityId
	LightEntities              map[string]EntityId

	DesiredChunks        map[ChunkCoord]struct{}
	KeepChunks           map[ChunkCoord]struct{}
	CollisionChunks      map[ChunkCoord]struct{}
	DestructionChunks    map[ChunkCoord]struct{}
	DesiredSectors       map[ChunkCoord]struct{}
	KeepSectors          map[ChunkCoord]struct{}
	DesiredProxySectors  map[ChunkCoord]struct{}
	KeepProxySectors     map[ChunkCoord]struct{}
	PendingLoads         map[ChunkCoord]struct{}
	PendingProxyLoads    map[ChunkCoord]struct{}
	PreparedLoads        chan streamedPreparedChunk
	PreparedProxyLoads   chan streamedPreparedSectorProxy
	activePrepareMu      sync.Mutex
	activeChunkPrepares  int
	activeProxyPrepares  int
	LoadedChunks         map[ChunkCoord]*streamedLoadedChunk
	LoadedSectorProxies  map[ChunkCoord]*streamedLoadedSectorProxy
	PlacementsByChunk    map[ChunkCoord][]streamedPlacementInstance
	PlacementChunk       map[string]ChunkCoord
	ObjectChunk          map[string]ChunkCoord
	TerrainEntries       map[ChunkCoord]content.TerrainChunkEntryDef
	ImportedWorldSectors map[ChunkCoord]content.ImportedWorldSectorDef
	ImportedChunkSector  map[ChunkCoord]ChunkCoord
	ImportedWorldEntries map[ChunkCoord]content.ImportedWorldChunkEntryDef

	WorldDeltaPath   string
	WorldDataDir     string
	WorldDelta       *content.WorldDeltaDef
	Metrics          StreamedLevelRuntimeMetrics
	nextMetricsLogAt time.Time

	placementOverrideMap     map[string]content.LevelTransformDef
	deletedPlacementIDs      map[string]struct{}
	terrainOverrideMap       map[string]content.TerrainChunkOverrideDef
	importedWorldOverrideMap map[string]content.ImportedWorldChunkOverrideDef
	voxelOverrideMap         map[string]content.VoxelObjectOverrideDef
}

type streamedPlacementInstance struct {
	PlacementID string
	VolumeID    string
	AssetPath   string
	Transform   content.LevelTransformDef
	Tags        []string
}

type streamedLoadedChunk struct {
	TerrainEntities       map[EntityId]struct{}
	ImportedWorldEntities map[EntityId]struct{}
	PlacementRoots        map[string]EntityId
	OwnedEntities         map[EntityId]struct{}
	ObjectEntities        map[string]EntityId
}

type streamedLoadedSectorProxy struct {
	Entity EntityId
	LOD    content.ImportedWorldLODDef
}

type streamedPreparedChunk struct {
	Coord                         ChunkCoord
	TerrainChunk                  *content.TerrainChunkDef
	ImportedWorldChunk            *content.ImportedWorldChunkDef
	PreparedImportedWorldGeometry *volume.XBrickMap
	PlacementItems                []streamedPlacementInstance
	ObjectSnapshots               map[string]*content.VoxelObjectSnapshotDef
	PrepareDuration               time.Duration
	Err                           error
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
	ImportedWorldOverride     *content.ImportedWorldChunkOverrideDef
	Placements                []streamedPlacementInstance
	VoxelOverrides            map[string]content.VoxelObjectOverrideDef
	WorldDeltaPath            string
}

type streamedSectorProxyLoadJob struct {
	SectorCoord  ChunkCoord
	ManifestPath string
	LOD          content.ImportedWorldLODDef
	Loader       *RuntimeContentLoader
}

type streamedPreparedSectorProxy struct {
	SectorCoord      ChunkCoord
	LOD              content.ImportedWorldLODDef
	Chunk            *content.ImportedWorldChunkDef
	PreparedGeometry *volume.XBrickMap
	Err              error
	PrepareDuration  time.Duration
}

func (StreamedLevelRuntimeModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&StreamedLevelRuntimeState{
		DesiredChunks:            make(map[ChunkCoord]struct{}),
		KeepChunks:               make(map[ChunkCoord]struct{}),
		CollisionChunks:          make(map[ChunkCoord]struct{}),
		DesiredSectors:           make(map[ChunkCoord]struct{}),
		KeepSectors:              make(map[ChunkCoord]struct{}),
		DesiredProxySectors:      make(map[ChunkCoord]struct{}),
		KeepProxySectors:         make(map[ChunkCoord]struct{}),
		PendingLoads:             make(map[ChunkCoord]struct{}),
		PendingProxyLoads:        make(map[ChunkCoord]struct{}),
		PreparedLoads:            make(chan streamedPreparedChunk, 256),
		PreparedProxyLoads:       make(chan streamedPreparedSectorProxy, 256),
		LoadedChunks:             make(map[ChunkCoord]*streamedLoadedChunk),
		LoadedSectorProxies:      make(map[ChunkCoord]*streamedLoadedSectorProxy),
		PlacementsByChunk:        make(map[ChunkCoord][]streamedPlacementInstance),
		PlacementChunk:           make(map[string]ChunkCoord),
		ObjectChunk:              make(map[string]ChunkCoord),
		TerrainEntries:           make(map[ChunkCoord]content.TerrainChunkEntryDef),
		ImportedWorldSectors:     make(map[ChunkCoord]content.ImportedWorldSectorDef),
		ImportedChunkSector:      make(map[ChunkCoord]ChunkCoord),
		ImportedWorldEntries:     make(map[ChunkCoord]content.ImportedWorldChunkEntryDef),
		MarkerEntities:           make(map[string]EntityId),
		LightEntities:            make(map[string]EntityId),
		placementOverrideMap:     make(map[string]content.LevelTransformDef),
		deletedPlacementIDs:      make(map[string]struct{}),
		terrainOverrideMap:       make(map[string]content.TerrainChunkOverrideDef),
		importedWorldOverrideMap: make(map[string]content.ImportedWorldChunkOverrideDef),
		voxelOverrideMap:         make(map[string]content.VoxelObjectOverrideDef),
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

	chunkSize := float32(level.ChunkSize) * level.VoxelResolution
	if chunkSize <= 0 {
		chunkSize = 32
	}
	streamingRadius := cfg.StreamingRadius
	if streamingRadius < 0 {
		streamingRadius = 0
	}
	streamingKeepRadius := cfg.StreamingKeepRadius
	if streamingKeepRadius < streamingRadius {
		streamingKeepRadius = streamingRadius
	}
	streamingPrefetchRadius := cfg.StreamingPrefetchRadius
	if streamingPrefetchRadius < streamingRadius {
		streamingPrefetchRadius = streamingRadius
	}
	streamingCollisionRadius := cfg.StreamingCollisionRadius
	if streamingCollisionRadius <= 0 {
		streamingCollisionRadius = streamingRadius
	}
	streamingDestructionRadius := cfg.StreamingDestructionRadius
	if streamingDestructionRadius <= 0 {
		streamingDestructionRadius = streamingCollisionRadius
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
	state.StreamingKeepRadius = streamingKeepRadius
	state.StreamingPrefetchRadius = streamingPrefetchRadius
	state.StreamingCollisionRadius = streamingCollisionRadius
	state.StreamingDestructionRadius = streamingDestructionRadius
	state.WorldDeltaPath = worldDeltaPath
	state.WorldDataDir = content.DefaultWorldDeltaDataDir(worldDeltaPath)
	state.WorldDelta = worldDelta
	state.Metrics = StreamedLevelRuntimeMetrics{}
	state.nextMetricsLogAt = time.Time{}
	state.TerrainID = ""
	state.TerrainPalette = AssetId{}
	state.BaseWorldID = ""
	state.BaseWorldManifest = nil
	state.BaseWorldPalette = AssetId{}
	state.BaseWorldMaterialLookup = ImportedWorldMaterialLookup{}
	state.BaseWorldCollisionEnabled = false
	state.MarkerEntities = make(map[string]EntityId)
	state.LightEntities = make(map[string]EntityId)
	state.DesiredChunks = make(map[ChunkCoord]struct{})
	state.KeepChunks = make(map[ChunkCoord]struct{})
	state.CollisionChunks = make(map[ChunkCoord]struct{})
	state.DestructionChunks = make(map[ChunkCoord]struct{})
	state.DesiredSectors = make(map[ChunkCoord]struct{})
	state.KeepSectors = make(map[ChunkCoord]struct{})
	state.DesiredProxySectors = make(map[ChunkCoord]struct{})
	state.KeepProxySectors = make(map[ChunkCoord]struct{})
	state.PendingLoads = make(map[ChunkCoord]struct{})
	state.PendingProxyLoads = make(map[ChunkCoord]struct{})
	state.LoadedChunks = make(map[ChunkCoord]*streamedLoadedChunk)
	state.LoadedSectorProxies = make(map[ChunkCoord]*streamedLoadedSectorProxy)
	state.PlacementsByChunk = make(map[ChunkCoord][]streamedPlacementInstance)
	state.PlacementChunk = make(map[string]ChunkCoord)
	state.ObjectChunk = make(map[string]ChunkCoord)
	state.TerrainEntries = make(map[ChunkCoord]content.TerrainChunkEntryDef)
	state.ImportedWorldSectors = make(map[ChunkCoord]content.ImportedWorldSectorDef)
	state.ImportedChunkSector = make(map[ChunkCoord]ChunkCoord)
	state.ImportedWorldEntries = make(map[ChunkCoord]content.ImportedWorldChunkEntryDef)
	state.placementOverrideMap = make(map[string]content.LevelTransformDef)
	state.deletedPlacementIDs = make(map[string]struct{})
	state.terrainOverrideMap = make(map[string]content.TerrainChunkOverrideDef)
	state.importedWorldOverrideMap = make(map[string]content.ImportedWorldChunkOverrideDef)
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
	for _, override := range worldDelta.ImportedWorldChunkOverrides {
		state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(override.WorldID, override.ChunkCoord)] = override
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
		if manifest.ChunkSize != level.ChunkSize {
			err = fmt.Errorf("terrain chunk size %d does not match level chunk size %d", manifest.ChunkSize, level.ChunkSize)
			state.InitErr = err
			return err
		}
		if absf(manifest.VoxelResolution-level.VoxelResolution) > 1e-4 {
			err = fmt.Errorf("terrain voxel size %.4f does not match level voxel size %.4f", manifest.VoxelResolution, level.VoxelResolution)
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
		if manifest.ChunkSize != level.ChunkSize {
			err = fmt.Errorf("base world chunk size %d does not match level chunk size %d", manifest.ChunkSize, level.ChunkSize)
			state.InitErr = err
			return err
		}
		if absf(manifest.VoxelResolution-level.VoxelResolution) > 1e-4 {
			err = fmt.Errorf("base world voxel size %.4f does not match level voxel size %.4f", manifest.VoxelResolution, level.VoxelResolution)
			state.InitErr = err
			return err
		}
		state.BaseWorldID = manifest.WorldID
		state.BaseWorldManifest = manifest
		state.BaseWorldMaterialLookup = NewImportedWorldMaterialLookup(manifest)
		state.BaseWorldCollisionEnabled = level.BaseWorld.CollisionEnabled
		entriesByCoord := make(map[content.TerrainChunkCoordDef]content.ImportedWorldChunkEntryDef, len(manifest.Entries))
		for _, entry := range manifest.Entries {
			entriesByCoord[entry.Coord] = entry
		}
		for _, sector := range manifest.Sectors {
			sectorCoord := chunkCoordFromTerrain(sector.Coord)
			state.ImportedWorldSectors[sectorCoord] = sector
			for _, ref := range sector.FullChunkRefs {
				entry, ok := entriesByCoord[ref]
				if !ok {
					err = fmt.Errorf("base world sector %s references missing chunk %s", content.TerrainChunkKey(sector.Coord), content.TerrainChunkKey(ref))
					state.InitErr = err
					return err
				}
				chunkCoord := chunkCoordFromTerrain(ref)
				state.ImportedWorldEntries[chunkCoord] = entry
				state.ImportedChunkSector[chunkCoord] = sectorCoord
			}
		}
		if assets != nil {
			state.BaseWorldPalette = ImportedWorldPaletteAsset(assets, manifest)
			if state.BaseWorldPalette == (AssetId{}) {
				state.BaseWorldPalette = assets.CreateSimplePalette([4]uint8{160, 160, 160, 255})
			}
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
	for _, marker := range level.Markers {
		state.MarkerEntities[marker.ID] = spawnAuthoredLevelMarker(cmd, state.LevelRoot, level.ID, marker)
	}
	for _, light := range level.Lights {
		entity, err := spawnAuthoredLevelLight(cmd, state.LevelRoot, level.ID, light)
		if err != nil {
			state.InitErr = err
			return err
		}
		state.LightEntities[light.ID] = entity
	}
	for _, water := range level.WaterBodies {
		spawnAuthoredLevelWaterBody(cmd, state.LevelRoot, level.ID, water)
	}
	for _, ladder := range level.LadderVolumes {
		spawnAuthoredLevelLadderVolume(cmd, state.LevelRoot, level.ID, ladder)
	}
	for _, brush := range level.MovingBrushes {
		if _, err := spawnAuthoredLevelMovingBrush(cmd, assets, loader, state.LevelRoot, level.ID, cfg.LevelPath, brush); err != nil {
			state.InitErr = err
			return err
		}
	}
	for _, trigger := range level.UseTriggers {
		spawnAuthoredLevelUseTrigger(cmd, state.LevelRoot, level.ID, trigger)
	}
	for _, trigger := range level.TriggerVolumes {
		spawnAuthoredLevelTriggerVolume(cmd, state.LevelRoot, level.ID, trigger)
	}
	for _, multi := range level.MultiTargets {
		spawnAuthoredLevelMultiTarget(cmd, state.LevelRoot, level.ID, multi)
	}
	for _, relay := range level.TargetRelays {
		spawnAuthoredLevelTargetRelay(cmd, state.LevelRoot, level.ID, relay)
	}
	for _, breakable := range level.Breakables {
		if _, err := spawnAuthoredLevelBreakable(cmd, assets, loader, state.LevelRoot, level.ID, cfg.LevelPath, breakable); err != nil {
			state.InitErr = err
			return err
		}
	}
	for _, pickup := range level.Pickups {
		if _, err := spawnAuthoredLevelPickup(cmd, assets, loader, state.LevelRoot, level.ID, cfg.LevelPath, pickup); err != nil {
			state.InitErr = err
			return err
		}
	}
	if cfg.AutoSpawnPlayer {
		playerMarkerKind := cfg.PlayerSpawnKind
		if playerMarkerKind == "" && level.Player != nil && level.Player.SpawnKind != "" {
			playerMarkerKind = level.Player.SpawnKind
		}
		if playerMarkerKind == "" {
			playerMarkerKind = content.LevelMarkerKindPlayerSpawn
		}
		if marker, ok := FindFirstLevelMarkerByKind(level, playerMarkerKind); ok {
			if err := ensureStreamedChunkLoadedForPosition(cmd, assets, state, marker.Transform.Position); err != nil {
				state.InitErr = err
				return err
			}
			if cfg.PlayerConfig != nil {
				SpawnGroundedPlayerAtMarkerWithConfig(cmd, marker, *cfg.PlayerConfig)
			} else if level.Player != nil {
				SpawnGroundedPlayerAtMarkerWithConfig(cmd, marker, groundedPlayerConfigFromLevelPlayer(level.Player))
			} else {
				SpawnGroundedPlayerAtMarker(cmd, marker)
			}
		}
	}
	cmd.app.FlushCommands()
	return nil
}

func groundedPlayerConfigFromLevelPlayer(player *content.LevelPlayerDef) GroundedPlayerControllerConfig {
	if player == nil {
		return GroundedPlayerControllerConfig{}
	}
	return GroundedPlayerControllerConfig{
		Height:           player.Height,
		EyeHeight:        player.EyeHeight,
		Radius:           player.Radius,
		Speed:            player.Speed,
		SprintMultiplier: player.SprintMultiplier,
		Sensitivity:      player.Sensitivity,
		JumpSpeed:        player.JumpSpeed,
		Gravity:          player.Gravity,
		StepHeight:       player.StepHeight,
		GroundProbe:      player.GroundProbe,
	}
}

func updateStreamedLevelObserverSystem(cmd *Commands, state *StreamedLevelRuntimeState) {
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}
	start := time.Now()
	defer func() {
		state.Metrics.ObserverUpdateDuration = time.Since(start)
		refreshStreamedRuntimeMetricsCounts(state)
		recordStreamingRendererPressure(cmd, state)
		recordStreamingProfilerDuration(cmd.app, state.Metrics.ObserverUpdateDuration)
		maybeEmitStreamedRuntimeMetrics(state, time.Now())
	}()

	desired := make(map[ChunkCoord]struct{})
	keep := make(map[ChunkCoord]struct{})
	collision := make(map[ChunkCoord]struct{})
	destruction := make(map[ChunkCoord]struct{})
	desiredSectors := make(map[ChunkCoord]struct{})
	keepSectors := make(map[ChunkCoord]struct{})
	MakeQuery2[TransformComponent, StreamedLevelObserverComponent](cmd).Map(func(id EntityId, transform *TransformComponent, observer *StreamedLevelObserverComponent) bool {
		if transform == nil || observer == nil {
			return true
		}
		loadRadius := observer.Radius
		if loadRadius <= 0 {
			loadRadius = state.StreamingRadius
		}
		keepRadius := observer.KeepRadius
		if keepRadius <= 0 {
			keepRadius = state.StreamingKeepRadius
		}
		if keepRadius < loadRadius {
			keepRadius = loadRadius
		}
		prefetchRadius := observer.PrefetchRadius
		if prefetchRadius <= 0 {
			prefetchRadius = state.StreamingPrefetchRadius
		}
		if prefetchRadius < loadRadius {
			prefetchRadius = loadRadius
		}
		collisionRadius := observer.CollisionRadius
		if collisionRadius <= 0 {
			collisionRadius = state.StreamingCollisionRadius
		}
		if collisionRadius <= 0 {
			collisionRadius = loadRadius
		}
		destructionRadius := observer.DestructionRadius
		if destructionRadius <= 0 {
			destructionRadius = state.StreamingDestructionRadius
		}
		if destructionRadius <= 0 {
			destructionRadius = collisionRadius
		}
		center := ChunkCoordFromPosition(transform.Position, state.ChunkSize)
		currentChunks := make(map[ChunkCoord]struct{})
		prefetchChunks := make(map[ChunkCoord]struct{})
		keepChunks := make(map[ChunkCoord]struct{})
		for _, coord := range center.NeighborsWithin(loadRadius) {
			currentChunks[coord] = struct{}{}
		}
		for _, coord := range center.NeighborsWithin(collisionRadius) {
			collision[coord] = struct{}{}
		}
		for _, coord := range center.NeighborsWithin(destructionRadius) {
			destruction[coord] = struct{}{}
		}
		for _, coord := range center.NeighborsWithin(prefetchRadius) {
			desired[coord] = struct{}{}
			prefetchChunks[coord] = struct{}{}
		}
		for _, coord := range center.NeighborsWithin(keepRadius) {
			keep[coord] = struct{}{}
			keepChunks[coord] = struct{}{}
		}
		observerDesiredSectors, observerKeepSectors := streamedObserverImportedSectorSets(state, currentChunks, prefetchChunks, keepChunks)
		mergeChunkCoordSet(desiredSectors, observerDesiredSectors)
		mergeChunkCoordSet(keepSectors, observerKeepSectors)
		return true
	})

	desired = streamedFilterImportedChunksBySectors(state, desired, desiredSectors)
	keep = streamedFilterImportedChunksBySectors(state, keep, keepSectors)
	streamedAddImportedSectorChunks(state, desired, desiredSectors)
	streamedAddImportedSectorChunks(state, keep, keepSectors)
	desiredProxySectors := copyChunkCoordSet(desiredSectors)
	keepProxySectors := copyChunkCoordSet(keepSectors)
	if !state.Config.DisableSectorProxies {
		proxyFallbackSectors := streamedProxyFallbackSectors(state)
		mergeChunkCoordSet(desiredProxySectors, proxyFallbackSectors)
		mergeChunkCoordSet(keepProxySectors, proxyFallbackSectors)
	}

	state.DesiredChunks = desired
	state.KeepChunks = keep
	state.CollisionChunks = collision
	state.DestructionChunks = destruction
	state.DesiredSectors = desiredSectors
	state.KeepSectors = keepSectors
	state.DesiredProxySectors = desiredProxySectors
	state.KeepProxySectors = keepProxySectors
	for coord := range state.LoadedChunks {
		if _, ok := keep[coord]; ok {
			continue
		}
		if streamedChunkNeedsProxyBeforeUnload(state, coord) {
			if sectorCoord, ok := state.ImportedChunkSector[coord]; ok {
				state.DesiredProxySectors[sectorCoord] = struct{}{}
				state.KeepProxySectors[sectorCoord] = struct{}{}
			}
			continue
		}
		if sectorCoord, ok := state.ImportedChunkSector[coord]; ok {
			setStreamedSectorProxyHidden(cmd, state, sectorCoord, false)
		}
		if err := unloadStreamedChunk(cmd, state, coord); err != nil && state.InitErr == nil {
			state.InitErr = err
		}
	}
	if state.InitErr != nil {
		return
	}
	for sectorCoord := range state.LoadedSectorProxies {
		if _, ok := state.KeepProxySectors[sectorCoord]; ok {
			continue
		}
		if state.streamedSectorProxyUnloadEnabled() {
			unloadStreamedSectorProxy(cmd, state, sectorCoord)
		}
	}
	activePrepares := streamedActivePrepareJobCounts(state)
	maxPrepareJobs := streamedMaxPrepareJobs(state)
	if !state.Config.DisableSectorProxies {
		for sectorCoord := range state.DesiredProxySectors {
			if activePrepares >= maxPrepareJobs {
				break
			}
			if _, ok := state.LoadedSectorProxies[sectorCoord]; ok {
				reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sectorCoord)
				continue
			}
			if _, ok := state.PendingProxyLoads[sectorCoord]; ok {
				continue
			}
			sector, ok := state.ImportedWorldSectors[sectorCoord]
			if !ok || len(sector.LODs) == 0 {
				continue
			}
			state.PendingProxyLoads[sectorCoord] = struct{}{}
			job := buildStreamedSectorProxyLoadJob(state, sectorCoord, sector.LODs[0])
			startStreamedSectorProxyPrepareJob(state, job)
			activePrepares++
		}
	}
	for coord := range desired {
		if activePrepares >= maxPrepareJobs {
			break
		}
		if _, ok := state.LoadedChunks[coord]; ok {
			continue
		}
		if _, ok := state.PendingLoads[coord]; ok {
			continue
		}
		if !streamedChunkHasLoadableContent(state, coord) {
			continue
		}
		state.PendingLoads[coord] = struct{}{}
		job := buildStreamedChunkLoadJob(state, coord)
		startStreamedChunkPrepareJob(state, job)
		activePrepares++
	}
}

func streamedMaxPrepareJobs(state *StreamedLevelRuntimeState) int {
	if state == nil {
		return 1
	}
	if state.Config.MaxPrepareJobs > 0 {
		return state.Config.MaxPrepareJobs
	}
	return 2
}

func streamedActivePrepareJobCounts(state *StreamedLevelRuntimeState) int {
	if state == nil {
		return 0
	}
	state.activePrepareMu.Lock()
	defer state.activePrepareMu.Unlock()
	return state.activeChunkPrepares + state.activeProxyPrepares
}

func startStreamedChunkPrepareJob(state *StreamedLevelRuntimeState, job streamedChunkLoadJob) {
	if state == nil {
		return
	}
	state.activePrepareMu.Lock()
	state.activeChunkPrepares++
	state.activePrepareMu.Unlock()
	go func() {
		result := prepareStreamedChunkLoad(job)
		state.PreparedLoads <- result
		state.activePrepareMu.Lock()
		state.activeChunkPrepares--
		state.activePrepareMu.Unlock()
	}()
}

func startStreamedSectorProxyPrepareJob(state *StreamedLevelRuntimeState, job streamedSectorProxyLoadJob) {
	if state == nil {
		return
	}
	state.activePrepareMu.Lock()
	state.activeProxyPrepares++
	state.activePrepareMu.Unlock()
	go func() {
		result := prepareStreamedSectorProxyLoad(job)
		state.PreparedProxyLoads <- result
		state.activePrepareMu.Lock()
		state.activeProxyPrepares--
		state.activePrepareMu.Unlock()
	}()
}

func streamedObserverImportedSectorSets(state *StreamedLevelRuntimeState, currentChunks, prefetchChunks, keepChunks map[ChunkCoord]struct{}) (map[ChunkCoord]struct{}, map[ChunkCoord]struct{}) {
	desired := make(map[ChunkCoord]struct{})
	keep := make(map[ChunkCoord]struct{})
	if state == nil {
		return desired, keep
	}
	currentSectors := streamedSectorsForChunks(state, currentChunks)
	prefetchSectors := streamedSectorsForChunks(state, prefetchChunks)
	keepSectors := streamedSectorsForChunks(state, keepChunks)
	if streamedSectorsHaveVisibilityMetadata(state, currentSectors) {
		for sectorCoord := range currentSectors {
			mergeChunkCoordSet(desired, streamedVisibleImportedSectorsForCurrentSector(state, sectorCoord))
		}
		mergeChunkCoordSet(keep, keepSectors)
		mergeChunkCoordSet(keep, desired)
		return desired, keep
	}
	mergeChunkCoordSet(desired, prefetchSectors)
	mergeChunkCoordSet(keep, keepSectors)
	return desired, keep
}

func streamedSectorsHaveVisibilityMetadata(state *StreamedLevelRuntimeState, sectors map[ChunkCoord]struct{}) bool {
	if state == nil {
		return false
	}
	for sectorCoord := range sectors {
		sector, ok := state.ImportedWorldSectors[sectorCoord]
		if !ok {
			continue
		}
		if len(sector.VisibleSectorRefs) > 0 || len(sector.AdjacentSectorRefs) > 0 || len(sector.SourceLeafIDs) > 0 {
			return true
		}
	}
	return false
}

func streamedVisibleImportedSectorsForCurrentSector(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) map[ChunkCoord]struct{} {
	out := map[ChunkCoord]struct{}{sectorCoord: {}}
	if state == nil {
		return out
	}
	sector, ok := state.ImportedWorldSectors[sectorCoord]
	if !ok {
		return out
	}
	for _, ref := range sector.VisibleSectorRefs {
		out[chunkCoordFromTerrain(ref)] = struct{}{}
	}
	for _, ref := range sector.AdjacentSectorRefs {
		out[chunkCoordFromTerrain(ref)] = struct{}{}
	}
	return out
}

func streamedFilterImportedChunksBySectors(state *StreamedLevelRuntimeState, chunks map[ChunkCoord]struct{}, sectors map[ChunkCoord]struct{}) map[ChunkCoord]struct{} {
	if state == nil || len(chunks) == 0 || len(state.ImportedChunkSector) == 0 {
		return chunks
	}
	out := make(map[ChunkCoord]struct{}, len(chunks))
	for coord := range chunks {
		sectorCoord, isImportedWorldChunk := state.ImportedChunkSector[coord]
		if !isImportedWorldChunk {
			out[coord] = struct{}{}
			continue
		}
		if _, ok := sectors[sectorCoord]; ok {
			out[coord] = struct{}{}
			continue
		}
		if streamedChunkHasNonImportedLoadableContent(state, coord) {
			out[coord] = struct{}{}
		}
	}
	return out
}

func streamedChunkHasNonImportedLoadableContent(state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if state == nil {
		return false
	}
	if entry, ok := state.TerrainEntries[coord]; ok && entry.NonEmptyVoxelCount > 0 {
		return true
	}
	if state.TerrainID != "" {
		if _, ok := state.terrainOverrideMap[terrainChunkRuntimeKey(state.TerrainID, terrainCoordFromChunk(coord))]; ok {
			return true
		}
	}
	if len(state.PlacementsByChunk[coord]) > 0 {
		return true
	}
	return false
}

func streamedAddImportedSectorChunks(state *StreamedLevelRuntimeState, chunks map[ChunkCoord]struct{}, sectors map[ChunkCoord]struct{}) {
	if state == nil {
		return
	}
	for sectorCoord := range sectors {
		sector, ok := state.ImportedWorldSectors[sectorCoord]
		if !ok {
			continue
		}
		for _, ref := range sector.FullChunkRefs {
			chunkCoord := chunkCoordFromTerrain(ref)
			entry, ok := state.ImportedWorldEntries[chunkCoord]
			if !ok || entry.NonEmptyVoxelCount <= 0 {
				continue
			}
			chunks[chunkCoord] = struct{}{}
		}
	}
}

func mergeChunkCoordSet(dst, src map[ChunkCoord]struct{}) {
	for coord := range src {
		dst[coord] = struct{}{}
	}
}

func copyChunkCoordSet(src map[ChunkCoord]struct{}) map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{}, len(src))
	for coord := range src {
		out[coord] = struct{}{}
	}
	return out
}

func streamedProxyFallbackSectors(state *StreamedLevelRuntimeState) map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{})
	if state == nil {
		return out
	}
	for sectorCoord, sector := range state.ImportedWorldSectors {
		if len(sector.LODs) > 0 {
			out[sectorCoord] = struct{}{}
		}
	}
	return out
}

func streamedProxySectorDesired(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) bool {
	if state == nil {
		return false
	}
	if state.DesiredProxySectors != nil {
		_, ok := state.DesiredProxySectors[sectorCoord]
		return ok
	}
	_, ok := state.DesiredSectors[sectorCoord]
	return ok
}

func streamedSectorsForChunks(state *StreamedLevelRuntimeState, chunks map[ChunkCoord]struct{}) map[ChunkCoord]struct{} {
	out := make(map[ChunkCoord]struct{})
	if state == nil {
		return out
	}
	for coord := range chunks {
		if sectorCoord, ok := state.ImportedChunkSector[coord]; ok {
			out[sectorCoord] = struct{}{}
		}
	}
	return out
}

func streamedChunkNeedsProxyBeforeUnload(state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if state == nil || state.Config.DisableSectorProxies {
		return false
	}
	sectorCoord, ok := state.ImportedChunkSector[coord]
	if !ok {
		return false
	}
	sector, ok := state.ImportedWorldSectors[sectorCoord]
	if !ok || len(sector.LODs) == 0 {
		return false
	}
	if _, loaded := state.LoadedSectorProxies[sectorCoord]; loaded {
		return false
	}
	return true
}

func streamedChunkHasLoadableContent(state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if state == nil {
		return false
	}
	if entry, ok := state.TerrainEntries[coord]; ok && entry.NonEmptyVoxelCount > 0 {
		return true
	}
	if state.TerrainID != "" {
		if _, ok := state.terrainOverrideMap[terrainChunkRuntimeKey(state.TerrainID, terrainCoordFromChunk(coord))]; ok {
			return true
		}
	}
	if entry, ok := state.ImportedWorldEntries[coord]; ok && entry.NonEmptyVoxelCount > 0 {
		return true
	}
	if state.BaseWorldID != "" {
		if _, ok := state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(state.BaseWorldID, terrainCoordFromChunk(coord))]; ok {
			return true
		}
	}
	if len(state.PlacementsByChunk[coord]) > 0 {
		return true
	}
	return false
}

func commitPreparedStreamedChunksSystem(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState) {
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}
	start := time.Now()
	state.Metrics.ChunksCommittedLastFrame = 0
	state.Metrics.ProxyChunksCommittedLastFrame = 0
	state.Metrics.FullChunksCommittedLastFrame = 0
	state.Metrics.CollisionChunksCommittedLastFrame = 0
	state.Metrics.EntitiesCommittedLastFrame = 0
	state.Metrics.CommitBudgetHitLastFrame = false
	state.Metrics.CommitBudgetReason = ""
	defer func() {
		state.Metrics.CommitSystemDuration = time.Since(start)
		refreshStreamedRuntimeMetricsCounts(state)
		recordStreamingRendererPressure(cmd, state)
		recordStreamingProfilerDuration(cmd.app, state.Metrics.CommitSystemDuration)
		maybeEmitStreamedRuntimeMetrics(state, time.Now())
	}()
	for {
		if streamedCommitFrameBudgetHit(state, start) {
			return
		}
		select {
		case prepared := <-state.PreparedProxyLoads:
			delete(state.PendingProxyLoads, prepared.SectorCoord)
			if prepared.Err != nil {
				state.Metrics.PrepareErrorCount++
				if state.InitErr == nil {
					state.InitErr = prepared.Err
				}
				continue
			}
			if !streamedProxySectorDesired(state, prepared.SectorCoord) {
				continue
			}
			if _, alreadyLoaded := state.LoadedSectorProxies[prepared.SectorCoord]; alreadyLoaded {
				continue
			}
			entityCount, err := commitPreparedStreamedSectorProxy(cmd, assets, state, prepared)
			if err != nil {
				state.Metrics.CommitErrorCount++
				if state.InitErr == nil {
					state.InitErr = err
				}
				continue
			}
			state.Metrics.ChunksCommittedLastFrame++
			state.Metrics.ProxyChunksCommittedLastFrame++
			state.Metrics.EntitiesCommittedLastFrame += entityCount
			continue
		default:
		}
		if streamedCommitFrameBudgetHit(state, start) {
			return
		}
		select {
		case prepared := <-state.PreparedLoads:
			delete(state.PendingLoads, prepared.Coord)
			recordPreparedStreamedChunkMetrics(state, prepared)
			if prepared.Err != nil {
				state.Metrics.PrepareErrorCount++
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
			collisionCommitCountBefore := state.Metrics.CollisionChunkCommitCount
			entityCount, err := commitPreparedStreamedChunk(cmd, assets, state, prepared)
			if err != nil {
				state.Metrics.CommitErrorCount++
				if state.InitErr == nil {
					state.InitErr = err
				}
				continue
			}
			state.Metrics.ChunksCommittedLastFrame++
			state.Metrics.FullChunksCommittedLastFrame++
			if state.Metrics.CollisionChunkCommitCount > collisionCommitCountBefore {
				state.Metrics.CollisionChunksCommittedLastFrame++
			}
			state.Metrics.EntitiesCommittedLastFrame += entityCount
			if sectorCoord, ok := state.ImportedChunkSector[prepared.Coord]; ok {
				reconcileStreamedSectorProxyAfterFullCommit(cmd, state, sectorCoord)
			}
		default:
			return
		}
	}
}

func streamedCommitFrameBudgetHit(state *StreamedLevelRuntimeState, start time.Time) bool {
	if state == nil {
		return false
	}
	if maxCommits := state.Config.MaxChunkCommitsPerFrame; maxCommits > 0 && state.Metrics.ChunksCommittedLastFrame >= maxCommits {
		state.Metrics.CommitBudgetHitLastFrame = true
		state.Metrics.CommitBudgetReason = "chunk_count"
		return true
	}
	if budgetMillis := state.Config.MaxStreamingCommitMillis; budgetMillis > 0 && time.Since(start) >= time.Duration(budgetMillis)*time.Millisecond {
		state.Metrics.CommitBudgetHitLastFrame = true
		state.Metrics.CommitBudgetReason = "time"
		return true
	}
	return false
}

func refreshStreamedRuntimeMetricsCounts(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	state.Metrics.DesiredChunkCount = len(state.DesiredChunks)
	state.Metrics.DesiredLoadableChunkCount = streamedLoadableChunkCount(state, state.DesiredChunks)
	state.Metrics.KeepChunkCount = len(state.KeepChunks)
	state.Metrics.KeepLoadableChunkCount = streamedLoadableChunkCount(state, state.KeepChunks)
	state.Metrics.CollisionChunkCount = len(state.CollisionChunks)
	state.Metrics.CollisionLoadableChunkCount = streamedLoadableChunkCount(state, state.CollisionChunks)
	state.Metrics.DestructionChunkCount = len(state.DestructionChunks)
	state.Metrics.DestructionLoadableChunkCount = streamedLoadableChunkCount(state, state.DestructionChunks)
	state.Metrics.DesiredSectorCount = len(state.DesiredSectors)
	state.Metrics.KeepSectorCount = len(state.KeepSectors)
	state.Metrics.DesiredSectorFullLoadedCount = streamedFullLoadedSectorCount(state, state.DesiredSectors)
	state.Metrics.KeepSectorFullLoadedCount = streamedFullLoadedSectorCount(state, state.KeepSectors)
	state.Metrics.PendingLoadCount = len(state.PendingLoads)
	state.Metrics.PendingProxyLoadCount = len(state.PendingProxyLoads)
	state.Metrics.ActiveChunkPrepareJobCount, state.Metrics.ActiveProxyPrepareJobCount = streamedActivePrepareJobBreakdown(state)
	state.Metrics.ActivePrepareJobCount = state.Metrics.ActiveChunkPrepareJobCount + state.Metrics.ActiveProxyPrepareJobCount
	if state.PreparedLoads != nil {
		state.Metrics.PreparedChunkQueueDepth = len(state.PreparedLoads)
	} else {
		state.Metrics.PreparedChunkQueueDepth = 0
	}
	if state.PreparedProxyLoads != nil {
		state.Metrics.PreparedProxyQueueDepth = len(state.PreparedProxyLoads)
	} else {
		state.Metrics.PreparedProxyQueueDepth = 0
	}
	state.Metrics.PreparedQueueDepth = state.Metrics.PreparedChunkQueueDepth + state.Metrics.PreparedProxyQueueDepth
	state.Metrics.LoadedChunkCount = len(state.LoadedChunks)
	state.Metrics.LoadedSectorProxyCount = len(state.LoadedSectorProxies)
	state.Metrics.LoadedSectorProxyFullReadyCount, state.Metrics.LoadedSectorProxyFullPendingCount, state.Metrics.LoadedSectorProxyOutOfKeepCount = streamedSectorProxyResidencyCounts(state)
}

func streamedFullLoadedSectorCount(state *StreamedLevelRuntimeState, sectors map[ChunkCoord]struct{}) int {
	if state == nil || len(sectors) == 0 {
		return 0
	}
	count := 0
	for sectorCoord := range sectors {
		if streamedSectorFullChunksLoaded(state, sectorCoord) {
			count++
		}
	}
	return count
}

func streamedSectorProxyResidencyCounts(state *StreamedLevelRuntimeState) (fullReady, fullPending, outOfKeep int) {
	if state == nil || len(state.LoadedSectorProxies) == 0 {
		return 0, 0, 0
	}
	for sectorCoord := range state.LoadedSectorProxies {
		if _, keep := state.KeepSectors[sectorCoord]; !keep {
			outOfKeep++
		}
		if streamedSectorFullChunksLoaded(state, sectorCoord) {
			fullReady++
			continue
		}
		if _, desired := state.DesiredSectors[sectorCoord]; desired {
			fullPending++
			continue
		}
		if _, keep := state.KeepSectors[sectorCoord]; keep {
			fullPending++
		}
	}
	return fullReady, fullPending, outOfKeep
}

func streamedActivePrepareJobBreakdown(state *StreamedLevelRuntimeState) (chunks, proxies int) {
	if state == nil {
		return 0, 0
	}
	state.activePrepareMu.Lock()
	defer state.activePrepareMu.Unlock()
	return state.activeChunkPrepares, state.activeProxyPrepares
}

func streamedLoadableChunkCount(state *StreamedLevelRuntimeState, chunks map[ChunkCoord]struct{}) int {
	if state == nil || len(chunks) == 0 {
		return 0
	}
	seen := make(map[ChunkCoord]struct{})
	add := func(coord ChunkCoord) {
		if _, requested := chunks[coord]; !requested {
			return
		}
		seen[coord] = struct{}{}
	}
	for coord, entry := range state.TerrainEntries {
		if entry.NonEmptyVoxelCount > 0 {
			add(coord)
		}
	}
	if state.TerrainID != "" {
		prefix := state.TerrainID + "|"
		for key := range state.terrainOverrideMap {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			if coord, ok := parseTerrainRuntimeChunkCoord(strings.TrimPrefix(key, prefix)); ok {
				add(chunkCoordFromTerrain(coord))
			}
		}
	}
	for coord, entry := range state.ImportedWorldEntries {
		if entry.NonEmptyVoxelCount > 0 {
			add(coord)
		}
	}
	if state.BaseWorldID != "" {
		prefix := state.BaseWorldID + "|"
		for key := range state.importedWorldOverrideMap {
			if !strings.HasPrefix(key, prefix) {
				continue
			}
			if coord, ok := parseTerrainRuntimeChunkCoord(strings.TrimPrefix(key, prefix)); ok {
				add(chunkCoordFromTerrain(coord))
			}
		}
	}
	for coord, placements := range state.PlacementsByChunk {
		if len(placements) > 0 {
			add(coord)
		}
	}
	return len(seen)
}

func parseTerrainRuntimeChunkCoord(key string) (content.TerrainChunkCoordDef, bool) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 {
		return content.TerrainChunkCoordDef{}, false
	}
	x, err := strconv.Atoi(parts[0])
	if err != nil {
		return content.TerrainChunkCoordDef{}, false
	}
	y, err := strconv.Atoi(parts[1])
	if err != nil {
		return content.TerrainChunkCoordDef{}, false
	}
	z, err := strconv.Atoi(parts[2])
	if err != nil {
		return content.TerrainChunkCoordDef{}, false
	}
	return content.TerrainChunkCoordDef{X: x, Y: y, Z: z}, true
}

func recordPreparedStreamedChunkMetrics(state *StreamedLevelRuntimeState, prepared streamedPreparedChunk) {
	if state == nil {
		return
	}
	state.Metrics.PreparedChunkCount++
	state.Metrics.LastPrepareCoord = prepared.Coord
	state.Metrics.LastPrepareDuration = prepared.PrepareDuration
	state.Metrics.TotalPrepareDuration += prepared.PrepareDuration
}

func resetLastStreamedCommitBreakdown(state *StreamedLevelRuntimeState) {
	if state == nil {
		return
	}
	state.Metrics.LastCommitFlushCount = 0
	state.Metrics.LastCommitTerrainDuration = 0
	state.Metrics.LastCommitWorldDuration = 0
	state.Metrics.LastCommitWorldVoxelCount = 0
	state.Metrics.LastCommitWorldBuildDuration = 0
	state.Metrics.LastCommitWorldRegisterDuration = 0
	state.Metrics.LastCommitWorldEntityDuration = 0
	state.Metrics.LastCommitPlacementDuration = 0
	state.Metrics.LastCommitFlushDuration = 0
}

func recordImportedWorldSpawnTiming(state *StreamedLevelRuntimeState, timing AuthoredImportedWorldSpawnTiming) {
	if state == nil {
		return
	}
	state.Metrics.LastCommitWorldVoxelCount += timing.VoxelCount
	state.Metrics.LastCommitWorldBuildDuration += timing.GeometryBuildDuration
	state.Metrics.LastCommitWorldRegisterDuration += timing.GeometryRegistrationDuration
	state.Metrics.LastCommitWorldEntityDuration += timing.EntityAndComponentSpawnDuration
}

func recordStreamedCommitFlush(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || cmd.app == nil {
		return
	}
	start := time.Now()
	cmd.app.FlushCommands()
	if state != nil {
		state.Metrics.LastCommitFlushDuration += time.Since(start)
		state.Metrics.LastCommitFlushCount++
	}
}

func recordStreamingRendererPressure(cmd *Commands, state *StreamedLevelRuntimeState) {
	if cmd == nil || state == nil {
		return
	}
	rt := voxelRtStateFromApp(cmd.app)
	if rt == nil || rt.RtApp == nil {
		return
	}
	if rt.RtApp.BufferManager != nil {
		state.Metrics.GPUVoxelSectorsUploaded = rt.RtApp.BufferManager.VoxelSectorsUploaded
		state.Metrics.GPUVoxelBricksUploaded = rt.RtApp.BufferManager.VoxelBricksUploaded
		state.Metrics.GPUVoxelDirtySectorsPending = rt.RtApp.BufferManager.VoxelDirtySectorsPending
		state.Metrics.GPUVoxelDirtyBricksPending = rt.RtApp.BufferManager.VoxelDirtyBricksPending
		state.Metrics.GPUVoxelUploadRevision = rt.RtApp.BufferManager.VoxelUploadRevision
	}
	if rt.RtApp.Scene != nil {
		state.Metrics.RendererSceneStructureRevision = rt.RtApp.Scene.StructureRevision
	}
}

func recordStreamingProfilerDuration(app *App, duration time.Duration) {
	if app == nil || duration <= 0 {
		return
	}
	resource, ok := app.resources[reflect.TypeOf(Profiler{})]
	if !ok {
		return
	}
	if profiler, ok := resource.(*Profiler); ok && profiler != nil {
		profiler.StreamingTime += duration
	}
}

func maybeEmitStreamedRuntimeMetrics(state *StreamedLevelRuntimeState, now time.Time) {
	if state == nil || state.Config.MetricsLogInterval <= 0 {
		return
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !state.nextMetricsLogAt.IsZero() && now.Before(state.nextMetricsLogAt) {
		return
	}
	state.nextMetricsLogAt = now.Add(state.Config.MetricsLogInterval)
	metrics := state.Metrics
	if state.Config.MetricsSink != nil {
		state.Config.MetricsSink(metrics)
		return
	}
	log.Print(metrics.LogLine())
}

func buildEffectiveStreamedPlacementIndex(level *content.LevelDef, levelPath string, overrides map[string]content.LevelTransformDef, deletions map[string]struct{}, maxVolumeInstances int) ([]streamedPlacementInstance, error) {
	placements := make([]streamedPlacementInstance, 0, len(level.Placements)+len(level.PlacementVolumes)*4)
	for _, placement := range level.Placements {
		placements = append(placements, streamedPlacementInstance{
			PlacementID: placement.ID,
			AssetPath:   placement.AssetPath,
			Transform:   effectiveLevelTransform(placement.ID, placement.Transform, overrides),
			Tags:        append([]string(nil), placement.Tags...),
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
				Tags:        append([]string(nil), volumeDef.Tags...),
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
	if override, ok := state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(state.BaseWorldID, terrainCoordFromChunk(coord))]; ok {
		overrideCopy := override
		job.ImportedWorldOverride = &overrideCopy
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

func buildStreamedSectorProxyLoadJob(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord, lod content.ImportedWorldLODDef) streamedSectorProxyLoadJob {
	job := streamedSectorProxyLoadJob{
		SectorCoord: sectorCoord,
		LOD:         lod,
		Loader:      state.Loader,
	}
	if state.Level != nil && state.Level.BaseWorld != nil {
		job.ManifestPath = content.ResolveDocumentPath(state.Level.BaseWorld.ManifestPath, state.LevelPath)
	}
	return job
}

func ensureStreamedChunkLoadedForPosition(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, position content.Vec3) error {
	if cmd == nil || state == nil {
		return nil
	}
	coord := ChunkCoordFromPosition(mgl32.Vec3{position[0], position[1], position[2]}, state.ChunkSize)
	if state.CollisionChunks == nil {
		state.CollisionChunks = make(map[ChunkCoord]struct{})
	}
	state.CollisionChunks[coord] = struct{}{}
	if _, ok := state.LoadedChunks[coord]; ok {
		return nil
	}
	prepared := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(state, coord))
	recordPreparedStreamedChunkMetrics(state, prepared)
	if prepared.Err != nil {
		state.Metrics.PrepareErrorCount++
		return prepared.Err
	}
	_, err := commitPreparedStreamedChunk(cmd, assets, state, prepared)
	if err != nil {
		state.Metrics.CommitErrorCount++
	}
	refreshStreamedRuntimeMetricsCounts(state)
	recordStreamingRendererPressure(cmd, state)
	return err
}

func prepareStreamedSectorProxyLoad(job streamedSectorProxyLoadJob) (result streamedPreparedSectorProxy) {
	start := time.Now()
	result = streamedPreparedSectorProxy{
		SectorCoord: job.SectorCoord,
		LOD:         job.LOD,
	}
	defer func() {
		result.PrepareDuration = time.Since(start)
	}()
	if job.Loader == nil {
		result.Err = fmt.Errorf("streamed sector proxy loader is nil")
		return result
	}
	if strings.TrimSpace(job.LOD.ChunkPath) == "" {
		result.Err = fmt.Errorf("streamed sector proxy lod path is empty for sector %s", job.SectorCoord.String())
		return result
	}
	chunkPath := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
	chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
	if err != nil {
		result.Err = err
		return result
	}
	result.Chunk = chunk
	result.PreparedGeometry = prepareImportedWorldChunkGeometry(chunk)
	return result
}

func prepareStreamedChunkLoad(job streamedChunkLoadJob) (result streamedPreparedChunk) {
	start := time.Now()
	result = streamedPreparedChunk{
		Coord:           job.Coord,
		PlacementItems:  append([]streamedPlacementInstance(nil), job.Placements...),
		ObjectSnapshots: make(map[string]*content.VoxelObjectSnapshotDef),
	}
	defer func() {
		result.PrepareDuration = time.Since(start)
	}()
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
	if job.ImportedWorldOverride != nil {
		chunkPath := content.ResolveDocumentPath(job.ImportedWorldOverride.SnapshotPath, job.WorldDeltaPath)
		chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
		if err != nil {
			result.Err = err
			return result
		}
		result.ImportedWorldChunk = chunk
		result.PreparedImportedWorldGeometry = prepareImportedWorldChunkGeometry(chunk)
	} else if job.ImportedWorldEntry != nil && job.ImportedWorldEntry.NonEmptyVoxelCount > 0 {
		chunkPath := content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath)
		chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
		if err != nil {
			result.Err = err
			return result
		}
		result.ImportedWorldChunk = chunk
		result.PreparedImportedWorldGeometry = prepareImportedWorldChunkGeometry(chunk)
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

func prepareImportedWorldChunkGeometry(chunk *content.ImportedWorldChunkDef) *volume.XBrickMap {
	if chunk == nil || chunk.NonEmptyVoxelCount == 0 {
		return nil
	}
	xbm := ImportedWorldChunkToXBrickMap(chunk)
	xbm.ComputeAABB()
	xbm.ClearDirty()
	return xbm
}

func commitPreparedStreamedSectorProxy(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedSectorProxy) (int, error) {
	start := time.Now()
	resetLastStreamedCommitBreakdown(state)
	entityCount := 0
	committed := false
	defer func() {
		duration := time.Since(start)
		state.Metrics.LastCommitCoord = prepared.SectorCoord
		state.Metrics.LastCommitDuration = duration
		state.Metrics.LastCommitEntityCount = entityCount
		state.Metrics.TotalCommitDuration += duration
		if committed {
			state.Metrics.CommittedChunkCount++
			state.Metrics.ProxyChunkCommitCount++
		}
	}()
	if cmd == nil || state == nil || prepared.Chunk == nil || prepared.Chunk.NonEmptyVoxelCount == 0 {
		return 0, nil
	}
	worldStart := time.Now()
	spawnTiming := AuthoredImportedWorldSpawnTiming{}
	entity := spawnAuthoredImportedWorldChunkEntity(cmd, state.LevelRoot, state.BaseWorldPalette, AuthoredImportedWorldSpawnDef{
		LevelID:                 state.LevelID,
		WorldID:                 importedWorldIDForPreparedChunk(state, prepared.Chunk),
		ShadowGroupID:           importedWorldGroupIDForStreamedState(state),
		Chunk:                   prepared.Chunk,
		CollisionEnabled:        false,
		DisableTerrainMetadata:  true,
		DisableShadows:          true,
		DisableOcclusionCulling: true,
		PreparedGeometry:        prepared.PreparedGeometry,
		Timing:                  &spawnTiming,
	})
	recordImportedWorldSpawnTiming(state, spawnTiming)
	state.Metrics.LastCommitWorldDuration += time.Since(worldStart)
	recordStreamedCommitFlush(cmd, state)
	clearEntityVoxelDirty(cmd, entity)
	state.LoadedSectorProxies[prepared.SectorCoord] = &streamedLoadedSectorProxy{
		Entity: entity,
		LOD:    prepared.LOD,
	}
	reconcileStreamedSectorProxyAfterFullCommit(cmd, state, prepared.SectorCoord)
	entityCount = 1
	committed = true
	return entityCount, nil
}

func commitPreparedStreamedChunk(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedChunk) (int, error) {
	start := time.Now()
	resetLastStreamedCommitBreakdown(state)
	entityCount := 0
	committed := false
	importedWorldCollisionCommitted := false
	defer func() {
		duration := time.Since(start)
		state.Metrics.LastCommitCoord = prepared.Coord
		state.Metrics.LastCommitDuration = duration
		state.Metrics.LastCommitEntityCount = entityCount
		state.Metrics.TotalCommitDuration += duration
		if committed {
			state.Metrics.CommittedChunkCount++
			state.Metrics.FullChunkCommitCount++
			if importedWorldCollisionCommitted {
				state.Metrics.CollisionChunkCommitCount++
			}
		}
	}()
	chunk := &streamedLoadedChunk{
		TerrainEntities:       make(map[EntityId]struct{}),
		ImportedWorldEntities: make(map[EntityId]struct{}),
		PlacementRoots:        make(map[string]EntityId),
		OwnedEntities:         make(map[EntityId]struct{}),
		ObjectEntities:        make(map[string]EntityId),
	}

	if prepared.TerrainChunk != nil && prepared.TerrainChunk.NonEmptyVoxelCount > 0 {
		terrainStart := time.Now()
		entity := spawnAuthoredTerrainChunkEntity(cmd, assets, state.LevelRoot, state.TerrainPalette, AuthoredTerrainSpawnDef{
			LevelID:        state.LevelID,
			TerrainID:      terrainIDForPreparedChunk(state, prepared.TerrainChunk),
			TerrainGroupID: terrainGroupIDForStreamedState(state),
			Chunk:          prepared.TerrainChunk,
		})
		state.Metrics.LastCommitTerrainDuration += time.Since(terrainStart)
		recordStreamedCommitFlush(cmd, state)
		entityCount++
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
		worldStart := time.Now()
		collisionEnabled := state.streamedImportedWorldChunkCollisionEnabled(prepared.Coord)
		destructionEnabled := state.streamedImportedWorldChunkDestructionEnabled(prepared.Coord)
		spawnTiming := AuthoredImportedWorldSpawnTiming{}
		entity := spawnAuthoredImportedWorldChunkEntity(cmd, state.LevelRoot, state.BaseWorldPalette, AuthoredImportedWorldSpawnDef{
			LevelID:            state.LevelID,
			WorldID:            importedWorldIDForPreparedChunk(state, prepared.ImportedWorldChunk),
			ShadowGroupID:      importedWorldGroupIDForStreamedState(state),
			Chunk:              prepared.ImportedWorldChunk,
			CollisionEnabled:   collisionEnabled,
			DestructionEnabled: destructionEnabled,
			PreparedGeometry:   prepared.PreparedImportedWorldGeometry,
			Timing:             &spawnTiming,
		})
		recordImportedWorldSpawnTiming(state, spawnTiming)
		state.Metrics.LastCommitWorldDuration += time.Since(worldStart)
		recordStreamedCommitFlush(cmd, state)
		entityCount++
		importedWorldCollisionCommitted = collisionEnabled
		clearEntityVoxelDirty(cmd, entity)
		chunk.ImportedWorldEntities[entity] = struct{}{}
		chunk.OwnedEntities[entity] = struct{}{}
	}

	for _, placement := range prepared.PlacementItems {
		placementStart := time.Now()
		spawnResult, err := spawnAuthoredLevelPlacement(cmd, assets, state.Loader, state.LevelRoot, state.LevelID, state.LevelPath, AuthoredPlacementSpawnDef{
			PlacementID: placement.PlacementID,
			VolumeID:    placement.VolumeID,
			AssetPath:   placement.AssetPath,
			Transform:   placement.Transform,
			Tags:        append([]string(nil), placement.Tags...),
		})
		if err != nil {
			state.Metrics.LastCommitPlacementDuration += time.Since(placementStart)
			return entityCount, err
		}
		state.Metrics.LastCommitPlacementDuration += time.Since(placementStart)
		recordStreamedCommitFlush(cmd, state)
		entityCount += 1 + len(spawnResult.EntitiesByAssetID)
		chunk.PlacementRoots[placement.PlacementID] = spawnResult.RootEntity
		chunk.OwnedEntities[spawnResult.RootEntity] = struct{}{}
		for _, eid := range spawnResult.EntitiesByAssetID {
			chunk.OwnedEntities[eid] = struct{}{}
			key := voxelObjectRuntimeKey(placement.PlacementID, authoredItemIDForEntity(cmd, eid))
			if snapshot, ok := prepared.ObjectSnapshots[key]; ok {
				snapshotStart := time.Now()
				if err := applyVoxelObjectSnapshotToEntity(cmd, eid, snapshot); err != nil {
					state.Metrics.LastCommitPlacementDuration += time.Since(snapshotStart)
					return entityCount, err
				}
				state.Metrics.LastCommitPlacementDuration += time.Since(snapshotStart)
				recordStreamedCommitFlush(cmd, state)
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
				Placement:   AuthoredPlacementSpawnDef{PlacementID: placement.PlacementID, VolumeID: placement.VolumeID, AssetPath: placement.AssetPath, Transform: placement.Transform, Tags: append([]string(nil), placement.Tags...)},
				RootEntity:  spawnResult.RootEntity,
				SpawnResult: spawnResult,
			})
		}
	}

	state.LoadedChunks[prepared.Coord] = chunk
	committed = true
	return entityCount, nil
}

func reconcileStreamedSectorProxyAfterFullCommit(cmd *Commands, state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) {
	if state == nil {
		return
	}
	setStreamedSectorProxyHidden(cmd, state, sectorCoord, streamedSectorFullChunksLoaded(state, sectorCoord))
}

func (state *StreamedLevelRuntimeState) streamedSectorProxyUnloadEnabled() bool {
	return state != nil && !state.Config.RetainSectorProxies
}

func streamedSectorFullChunksLoaded(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) bool {
	if state == nil {
		return false
	}
	sector, ok := state.ImportedWorldSectors[sectorCoord]
	if !ok || len(sector.FullChunkRefs) == 0 {
		return false
	}
	for _, ref := range sector.FullChunkRefs {
		entry, ok := state.ImportedWorldEntries[chunkCoordFromTerrain(ref)]
		if !ok || entry.NonEmptyVoxelCount <= 0 {
			continue
		}
		if _, loaded := state.LoadedChunks[chunkCoordFromTerrain(ref)]; !loaded {
			return false
		}
	}
	return true
}

func setStreamedSectorProxyHidden(cmd *Commands, state *StreamedLevelRuntimeState, sectorCoord ChunkCoord, hidden bool) {
	if cmd == nil || state == nil {
		return
	}
	loaded := state.LoadedSectorProxies[sectorCoord]
	if loaded == nil || loaded.Entity == 0 {
		return
	}
	if hidden {
		if !VoxelEntityRenderHidden(cmd, loaded.Entity) {
			cmd.AddComponents(loaded.Entity, &VoxelRenderHiddenComponent{})
		}
		return
	}
	if VoxelEntityRenderHidden(cmd, loaded.Entity) {
		cmd.RemoveComponents(loaded.Entity, &VoxelRenderHiddenComponent{})
	}
}

func unloadStreamedSectorProxy(cmd *Commands, state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) {
	if cmd == nil || state == nil {
		return
	}
	loaded := state.LoadedSectorProxies[sectorCoord]
	if loaded == nil {
		return
	}
	if loaded.Entity != 0 {
		cmd.RemoveEntity(loaded.Entity)
	}
	delete(state.LoadedSectorProxies, sectorCoord)
}

func (state *StreamedLevelRuntimeState) streamedImportedWorldChunkCollisionEnabled(coord ChunkCoord) bool {
	if state == nil || !state.BaseWorldCollisionEnabled {
		return false
	}
	if len(state.CollisionChunks) == 0 {
		return false
	}
	_, ok := state.CollisionChunks[coord]
	return ok
}

func (state *StreamedLevelRuntimeState) streamedImportedWorldChunkDestructionEnabled(coord ChunkCoord) bool {
	if state == nil {
		return false
	}
	if len(state.DestructionChunks) == 0 {
		return false
	}
	_, ok := state.DestructionChunks[coord]
	return ok
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
		if rt := voxelRtStateFromApp(cmd.app); rt != nil {
			rt.clearRuntimeEditedVoxelEntity(eid)
		}
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

	for eid := range loaded.ImportedWorldEntities {
		if len(cmd.GetAllComponents(eid)) == 0 {
			continue
		}
		ref, ok := AuthoredImportedWorldChunkRefForEntity(cmd, eid)
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
		chunkSize := vmc.TerrainChunkSize
		if chunkSize <= 0 {
			chunkSize = state.Level.ChunkSize
		}
		chunkCoord := terrainCoordFromArray(ref.ChunkCoord)
		snapshot := importedWorldChunkDefFromXBrickMap(ref.WorldID, chunkCoord, chunkSize, voxelResolutionForEntity(cmd, eid), xbm)
		snapshotPath := filepath.Join(state.WorldDataDir, fmt.Sprintf("imported_%s_%d_%d_%d.gkchunk", sanitizePathSegment(ref.WorldID), ref.ChunkCoord[0], ref.ChunkCoord[1], ref.ChunkCoord[2]))
		if err := content.SaveImportedWorldChunk(snapshotPath, snapshot); err != nil {
			return err
		}
		override := content.ImportedWorldChunkOverrideDef{
			WorldID:      ref.WorldID,
			ChunkCoord:   chunkCoord,
			SnapshotPath: content.AuthorDocumentPath(snapshotPath, state.WorldDeltaPath),
		}
		if state.importedWorldOverrideMap == nil {
			state.importedWorldOverrideMap = make(map[string]content.ImportedWorldChunkOverrideDef)
		}
		state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(ref.WorldID, override.ChunkCoord)] = override
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
	state.WorldDelta.ImportedWorldChunkOverrides = mapImportedWorldOverrides(state.importedWorldOverrideMap)
	state.WorldDelta.VoxelObjectOverrides = mapVoxelOverrides(state.voxelOverrideMap)
	return content.SaveWorldDelta(state.WorldDeltaPath, state.WorldDelta)
}

func applyVoxelObjectSnapshotToEntity(cmd *Commands, eid EntityId, snapshot *content.VoxelObjectSnapshotDef) error {
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if !ok {
		return nil
	}
	assets := assetServerFromApp(cmd.app)
	if assets == nil {
		return fmt.Errorf("asset server not available")
	}
	vmc.OverrideGeometry = assets.RegisterSharedVoxelGeometry(XBrickMapFromVoxelObjectSnapshot(snapshot), "")
	cmd.AddComponents(eid, &vmc)
	return nil
}

func currentVoxelMapForEntity(cmd *Commands, eid EntityId) (*volume.XBrickMap, bool, bool) {
	if len(cmd.GetAllComponents(eid)) == 0 {
		return nil, true, false
	}
	persistenceDirty := VoxelEntityPersistenceDirty(cmd, eid)
	if state := voxelRtStateFromApp(cmd.app); state != nil && state.runtimeEditedVoxelEntity(eid) {
		if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
			return obj.XBrickMap, true, true
		}
	}
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if !ok {
		if state := voxelRtStateFromApp(cmd.app); state != nil {
			if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
				return obj.XBrickMap, persistenceDirty || isVoxelMapDirty(obj.XBrickMap), true
			}
		}
		return nil, false, true
	}
	assets := assetServerFromApp(cmd.app)
	if assets == nil {
		if state := voxelRtStateFromApp(cmd.app); state != nil {
			if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
				return obj.XBrickMap, persistenceDirty || isVoxelMapDirty(obj.XBrickMap), true
			}
		}
		return nil, false, true
	}
	_, asset, ok := ResolveVoxelGeometry(assets, &vmc)
	if !ok || asset == nil || asset.XBrickMap == nil {
		if state := voxelRtStateFromApp(cmd.app); state != nil {
			if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
				return obj.XBrickMap, persistenceDirty || isVoxelMapDirty(obj.XBrickMap), true
			}
		}
		return nil, false, true
	}
	return asset.XBrickMap, persistenceDirty || isVoxelMapDirty(asset.XBrickMap), true
}

func clearEntityVoxelDirty(cmd *Commands, eid EntityId) {
	if state := voxelRtStateFromApp(cmd.app); state != nil {
		if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil {
			obj.XBrickMap.ClearDirty()
		}
	}
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if ok {
		if assets := assetServerFromApp(cmd.app); assets != nil {
			if _, asset, resolved := ResolveVoxelGeometry(assets, &vmc); resolved && asset != nil && asset.XBrickMap != nil {
				asset.XBrickMap.ClearDirty()
			}
		}
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

func importedWorldGroupIDForStreamedState(state *StreamedLevelRuntimeState) uint32 {
	if state == nil {
		return 0
	}
	return stableImportedWorldGroupID(state.LevelID, state.BaseWorldID)
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

func importedWorldChunkRuntimeKey(worldID string, coord content.TerrainChunkCoordDef) string {
	return worldID + "|" + content.TerrainChunkKey(coord)
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
	var vmc *VoxelModelComponent
	for _, comp := range cmd.GetAllComponents(eid) {
		if typed, ok := comp.(*VoxelModelComponent); ok {
			vmc = typed
		}
		if typed, ok := comp.(VoxelModelComponent); ok {
			copy := typed
			vmc = &copy
		}
		if tr, ok := comp.(*TransformComponent); ok {
			return VoxelResolutionOrDefault(vmc) * tr.Scale.X()
		}
		if tr, ok := comp.(TransformComponent); ok {
			return VoxelResolutionOrDefault(vmc) * tr.Scale.X()
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

func mapImportedWorldOverrides(src map[string]content.ImportedWorldChunkOverrideDef) []content.ImportedWorldChunkOverrideDef {
	out := make([]content.ImportedWorldChunkOverrideDef, 0, len(src))
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
