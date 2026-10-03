package gekko

import (
	"container/heap"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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

type StreamedLevelDeltaMode string

const (
	StreamedLevelDeltaPersistent StreamedLevelDeltaMode = "persistent"
	StreamedLevelDeltaFresh      StreamedLevelDeltaMode = "fresh"
)

type StreamedLevelRuntimeConfig struct {
	LevelPath                       string
	NavigationManifestPath          string
	DeltaMode                       StreamedLevelDeltaMode
	Loader                          *RuntimeContentLoader
	StreamingRadius                 int
	StreamingKeepRadius             int
	StreamingPrefetchRadius         int
	StreamingCollisionRadius        int
	StreamingDestructionRadius      int
	MaxVolumeInstances              int
	MaxPrepareJobs                  int
	MaxStreamingWorkItems           int
	MaxPreparedGeometryCacheEntries int
	MaxPreparedGeometryCacheBytes   int64
	MaxDecodedContentCacheBytes     int64
	MaxPendingPreparedBytes         int64
	MaxPendingPersistenceBytes      int64
	MaxChunkCommitsPerFrame         int
	MaxPlacementCommitUnitsPerFrame int
	MaxStreamingCommitMillis        int
	MetricsLogInterval              time.Duration
	DisableSectorProxies            bool
	RetainSectorProxies             bool
	MetricsSink                     func(StreamedLevelRuntimeMetrics)
	TerrainGroupID                  uint32
	AutoSpawnPlayer                 bool
	PlayerSpawnKind                 string
	PlayerConfig                    *GroundedPlayerControllerConfig
	PlacementHooks                  []PostSpawnPlacementHook
	TerrainHooks                    []PostSpawnTerrainHook
}

type StreamedLevelRuntimeMetrics struct {
	ActiveChunkCommitCount                 int
	PlacementCommitUnitsLastFrame          int
	PendingPersistenceCount                int
	PendingPersistenceBytes                int64
	PendingPersistenceMaxBytes             int64
	PendingPersistenceOverBudgetBytes      int64
	PendingPersistenceAdmissionRetries     int
	PendingPersistenceOversizedAdmissions  int
	DirtyPinnedChunkCount                  int
	PersistenceFailureCount                int
	PersistenceLastError                   string
	ObserverSelectionBuildCount            uint64
	ObserverSelectionChunkVisitCount       uint64
	StreamingWorkCount                     int
	StreamingWorkMaxCount                  int
	StreamingWorkOverBudgetCount           int
	StreamingWorkCarryoverCount            int
	StreamingWorkAdmissionBlockedCount     uint64
	PrepareDispatchCount                   uint64
	LastPrepareDispatchCoord               ChunkCoord
	LastPrepareDispatchKind                string
	DecodedContentCacheEntries             int
	DecodedContentCacheBytes               int64
	DecodedContentCachePinnedBytes         int64
	DecodedContentCacheMaxBytes            int64
	DecodedContentCacheOverBudgetBytes     int64
	DecodedContentCacheHits                int
	DecodedContentCacheMisses              int
	DecodedContentCacheEvictions           int
	DecodedContentCacheLoadWaits           int
	DecodedContentCacheOversizedBypasses   int
	PendingPreparedBytes                   int64
	PendingPreparedMaxBytes                int64
	PendingPreparedOverBudgetBytes         int64
	PendingPreparedAdmissionRetries        int
	PendingPreparedOversizedAdmissions     int
	DesiredChunkCount                      int
	DesiredLoadableChunkCount              int
	KeepChunkCount                         int
	KeepLoadableChunkCount                 int
	CollisionChunkCount                    int
	CollisionLoadableChunkCount            int
	DestructionChunkCount                  int
	DestructionLoadableChunkCount          int
	DesiredSectorCount                     int
	KeepSectorCount                        int
	DesiredSectorFullLoadedCount           int
	KeepSectorFullLoadedCount              int
	PendingLoadCount                       int
	PendingProxyLoadCount                  int
	ActivePrepareJobCount                  int
	ActiveChunkPrepareJobCount             int
	ActiveProxyPrepareJobCount             int
	PreparedQueueDepth                     int
	PreparedChunkQueueDepth                int
	PreparedProxyQueueDepth                int
	PreparedGeometryCacheEntries           int
	PreparedGeometryCacheBytes             int64
	PreparedGeometryCachePreparedBytes     int64
	PreparedGeometryCacheAssetBytes        int64
	PreparedGeometryCachePinnedBytes       int64
	PreparedGeometryCacheMaxBytes          int64
	PreparedGeometryCacheOverBudgetBytes   int64
	PreparedGeometryCacheBuildWaits        int
	PreparedGeometryCacheOversizedBypasses int
	PreparedGeometryCacheVoxels            int
	PreparedGeometryCacheHits              int
	PreparedGeometryCacheMisses            int
	PreparedGeometryCacheEvictions         int
	PreparedGeometryAssetRegisters         int
	PreparedGeometryAssetAdoptions         int
	PreparedGeometryAssetReuses            int
	AuxSidecarHitCount                     int
	AuxSidecarMissCount                    int
	LoadedChunkCount                       int
	LoadedSectorProxyCount                 int
	LoadedSectorProxyFullReadyCount        int
	LoadedSectorProxyFullPendingCount      int
	LoadedSectorProxyOutOfKeepCount        int
	ChunksCommittedLastFrame               int
	ProxyChunksCommittedLastFrame          int
	FullChunksCommittedLastFrame           int
	CollisionChunksCommittedLastFrame      int
	EntitiesCommittedLastFrame             int
	CommitBudgetHitLastFrame               bool
	CommitBudgetReason                     string
	NavigationRebuildActive                bool
	NavigationRebuildQueuedTileCount       int
	NavigationEditIgnoredCount             uint64
	NavigationRebuildStartedCount          uint64
	NavigationRebuildCompletedCount        uint64
	NavigationRebuildDiscardedCount        uint64
	NavigationRebuildPublishedCount        uint64
	NavigationRebuildRequestedRevision     uint64
	NavigationRebuildCompletedRevision     uint64
	NavigationRebuildDiscardedRevision     uint64
	NavigationRebuildPublishedRevision     uint64
	NavigationRebuildLastReason            string
	NavigationRebuildTerminalReason        string
	NavigationRebuildLastDirtyCount        int

	PreparedGeometryCacheEvictionCandidateVisits int
	PreparedGeometryCacheStorageReferenceVisits  int
	DecodedContentCacheEvictionCandidateVisits   int

	PreparedChunkCount    int
	PrepareErrorCount     int
	PrepareCancelledCount int
	LastPrepareCoord      ChunkCoord
	LastPrepareDuration   time.Duration
	TotalPrepareDuration  time.Duration

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

	GPUVoxelSectorsUploaded           int
	GPUVoxelBricksUploaded            int
	GPUVoxelDirtySectorsPending       int
	GPUVoxelDirtyBricksPending        int
	GPUVoxelRuntimeNormalBakeDuration time.Duration
	GPUVoxelUploadRevision            uint64
	GPURetainedVoxelMapEntries        int
	GPURetainedVoxelMapSectors        int
	GPURetainedVoxelMapHits           int
	GPURetainedVoxelMapMisses         int
	GPURetainedVoxelMapEvictions      int
	RendererSceneStructureRevision    uint64
}

func (m StreamedLevelRuntimeMetrics) LogLine() string {
	return fmt.Sprintf(
		"streaming metrics: desired=%d desired_loadable=%d keep=%d keep_loadable=%d collision=%d collision_loadable=%d destruction=%d destruction_loadable=%d desired_sectors=%d desired_sectors_full=%d keep_sectors=%d keep_sectors_full=%d pending=%d pending_proxy=%d active_prepare=%d active_prepare_chunks=%d active_prepare_proxies=%d prepared_queue=%d prepared_chunks=%d prepared_proxies=%d aux_hits=%d aux_misses=%d loaded=%d loaded_proxies=%d proxy_full_ready=%d proxy_full_pending=%d proxy_out_of_keep=%d committed_total=%d full_committed_total=%d commit_world_ms=%.3f commit_world_register_ms=%.3f commit_flushes=%d runtime_normal_bake_ms=%.3f observer_selection_builds=%d observer_selection_chunk_visits=%d",
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
		m.AuxSidecarHitCount,
		m.AuxSidecarMissCount,
		m.LoadedChunkCount,
		m.LoadedSectorProxyCount,
		m.LoadedSectorProxyFullReadyCount,
		m.LoadedSectorProxyFullPendingCount,
		m.LoadedSectorProxyOutOfKeepCount,
		m.CommittedChunkCount,
		m.FullChunkCommitCount,
		durationMillis(m.LastCommitWorldDuration),
		durationMillis(m.LastCommitWorldRegisterDuration),
		m.LastCommitFlushCount,
		durationMillis(m.GPUVoxelRuntimeNormalBakeDuration),
		m.ObserverSelectionBuildCount,
		m.ObserverSelectionChunkVisitCount,
	)
}

func durationMillis(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func streamedMetricsToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return strings.Join(strings.Fields(value), "_")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

type StreamedLevelRuntimeModule struct{}

type StreamedLevelRuntimeState struct {
	mu   sync.RWMutex
	jobs sync.WaitGroup

	Initialized bool
	InitErr     error
	Generation  uint64

	observerSelection         *streamedObserverSelectionOwner
	observerSelectionRevision uint64
	prepareScheduler          streamedPrepareScheduler
	readyCommits              streamedReadyOwner
	streamingWork             streamedWorkOwner
	terrainGeometryAssets     map[EntityId]streamedGeometryAssetLease
	snapshotGeometryAssets    map[EntityId]streamedGeometryAssetLease

	renderManaged     bool
	nextRenderTicket  uint64
	renderTicketBatch streamedRenderTicketBatch
	renderTargets     map[EntityId]streamedRenderTarget
	retiredRenderIDs  map[uint64]struct{}

	Config                     StreamedLevelRuntimeConfig
	Loader                     *RuntimeContentLoader
	metadataScope              *RuntimeContentLoadScope
	ownsLoader                 bool
	pendingPrepared            *streamedPendingPreparedOwner
	pendingChunkCostHints      map[ChunkCoord]int64
	pendingProxyCostHints      map[ChunkCoord]int64
	chunkPrepareCancels        map[ChunkCoord]chan struct{}
	proxyPrepareCancels        map[ChunkCoord]chan struct{}
	Level                      *content.LevelDef
	LevelID                    string
	LevelPath                  string
	LevelRoot                  EntityId
	PlayerEntity               EntityId
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
	BaseWorldBacking           VoxelBackingProvider
	BaseWorldBackingSourceHash string
	BaseWorldPalette           AssetId
	BaseWorldMaterialLookup    ImportedWorldMaterialLookup
	BaseWorldCollisionEnabled  bool
	MarkerEntities             map[string]EntityId
	LightEntities              map[string]EntityId

	// Demand maps are runtime-owned read-only views. Consumers must not mutate
	// them or assume that a view remains current across selection updates.
	DesiredChunks                 map[ChunkCoord]struct{}
	KeepChunks                    map[ChunkCoord]struct{}
	CollisionChunks               map[ChunkCoord]struct{}
	DestructionChunks             map[ChunkCoord]struct{}
	DesiredSectors                map[ChunkCoord]struct{}
	KeepSectors                   map[ChunkCoord]struct{}
	DesiredProxySectors           map[ChunkCoord]struct{}
	KeepProxySectors              map[ChunkCoord]struct{}
	PendingLoads                  map[ChunkCoord]struct{}
	PendingProxyLoads             map[ChunkCoord]struct{}
	PreparedLoads                 chan streamedPreparedChunk
	PreparedProxyLoads            chan streamedPreparedSectorProxy
	PreparedGeometryCache         *streamedPreparedGeometryCache
	activePrepareMu               sync.Mutex
	activeChunkPrepares           int
	activeProxyPrepares           int
	LoadedChunks                  map[ChunkCoord]*streamedLoadedChunk
	LoadedSectorProxies           map[ChunkCoord]*streamedLoadedSectorProxy
	PlacementsByChunk             map[ChunkCoord][]streamedPlacementInstance
	PlacementChunk                map[string]ChunkCoord
	ObjectChunk                   map[string]ChunkCoord
	TerrainEntries                map[ChunkCoord]content.TerrainChunkEntryDef
	ImportedWorldSectors          map[ChunkCoord]content.ImportedWorldSectorDef
	ImportedChunkSector           map[ChunkCoord]ChunkCoord
	ImportedWorldEntries          map[ChunkCoord]content.ImportedWorldChunkEntryDef
	BaseNavManifestPath           string
	BaseNavManifest               *content.NavGraphManifestDef
	NavigationSources             []content.NavSourceTileDef
	NavigationGraphs              []content.NavGraphTileDef
	NavigationRevision            uint64
	navigationQuery               *content.NavGraphQuery
	navigationDisabled            map[string]struct{}
	navigationOpenDoors           map[string]struct{}
	navigationBlockers            map[string]content.NavBlockerDef
	navigationOverlayDisabled     map[string]struct{}
	navigationOverlayOpenDoors    map[string]struct{}
	navigationOverlayBlockers     map[string]content.NavBlockerDef
	navigationOverlayRequestedGen uint64
	navigationOverlayActive       bool
	navigationOverlays            chan streamedNavigationOverlayResult
	navigationDesired             map[content.TerrainChunkCoordDef]struct{}
	navigationLoadedGen           uint64
	navigationRequestedGen        uint64
	navigationPendingGen          uint64
	navigationPendingSources      []content.NavSourceTileDef
	navigationPendingGraphs       []content.NavGraphTileDef
	navigationLoadActive          bool
	navigationLoads               chan streamedNavigationLoadResult
	navigationRebuildActive       bool
	navigationRebuilds            chan streamedNavigationRebuildResult
	navigationEditAnalysisActive  bool
	navigationEditAnalyses        chan streamedNavigationEditAnalysisResult
	navigationEditAnalysisPending map[content.TerrainChunkCoordDef]streamedNavigationEditAnalysisItem
	navigationEditAnalysisSince   time.Time
	navigationEditAnalysisAt      time.Time
	navigationVoxelSnapshots      map[EntityId]navigationVoxelSnapshot
	navigationEditRevisions       map[EntityId]uint64
	navigationEditGeneration      uint64
	navigationQueuedEdits         map[content.TerrainChunkCoordDef]navigationQueuedEdit
	navigationIgnoredRemovals     map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{}
	navigationEditQueuedSince     time.Time
	navigationEditLastQueuedAt    time.Time
	navigationEditBlockers        map[string]navigationEditBlocker
	navigationRetireAtLoad        map[uint64]uint64
	worldDeltaWriter              func(string, *content.WorldDeltaDef) error
	persistenceTransaction        *streamedPersistenceTransaction
	persistenceIntents            map[ChunkCoord]*streamedPersistenceIntent
	persistenceBytes              int64
	persistenceCmd                *Commands
	worldDeltaSaveActive          bool
	worldDeltaSaveRequestedGen    uint64
	worldDeltaSaveActiveGen       uint64
	worldDeltaSavePending         *content.WorldDeltaDef
	worldDeltaSaves               chan streamedWorldDeltaSaveResult
	runtimeEditPersistenceMu      sync.Mutex
	importedEditCaptures          map[voxelWorldDirtyChunkKey]*streamedImportedCaptureToken

	WorldDeltaPath   string
	WorldDataDir     string
	WorldDelta       *content.WorldDeltaDef
	sessionDeltaDir  string
	Metrics          StreamedLevelRuntimeMetrics
	nextMetricsLogAt time.Time

	placementOverrideMap     map[string]content.LevelTransformDef
	deletedPlacementIDs      map[string]struct{}
	terrainOverrideMap       map[string]content.TerrainChunkOverrideDef
	importedWorldOverrideMap map[string]content.ImportedWorldChunkOverrideDef
	voxelOverrideMap         map[string]content.VoxelObjectOverrideDef
	voxelBackingRemovalMap   map[string]content.VoxelBackingRemovalDef
}

type streamedPlacementInstance struct {
	PlacementID string
	VolumeID    string
	AssetPath   string
	Transform   content.LevelTransformDef
	Tags        []string
}

type streamedLoadedChunk struct {
	importedEmptyGeneration     uint64
	TerrainEntities             map[EntityId]struct{}
	ImportedWorldEntities       map[EntityId]struct{}
	ImportedWorldGeometryAssets []streamedGeometryAssetLease
	PlacementRoots              map[string]EntityId
	OwnedEntities               map[EntityId]struct{}
	ObjectEntities              map[string]EntityId
}

type streamedLoadedSectorProxy struct {
	Entity        EntityId
	LOD           content.ImportedWorldLODDef
	GeometryAsset streamedGeometryAssetLease
}

// Keep the registering server with every acquired ID so nil-cache cleanup
// releases the exact asset owned by this runtime load.
type streamedGeometryAssetLease struct {
	ID     AssetId
	Server *AssetServer
}

func (lease streamedGeometryAssetLease) release(cache *streamedPreparedGeometryCache) {
	cache.releaseAssetID(lease.Server, lease.ID)
}

type streamedPreparedChunk struct {
	prepareCancel                         <-chan struct{}
	loadScope                             *RuntimeContentLoadScope
	pendingCredit                         *streamedPendingPreparedCredit
	registration                          *streamedGeometryRegistration
	terrainRegistration                   *streamedGeometryRegistration
	preparedTerrainGeometry               *volume.XBrickMap
	retryCost                             int64
	Generation                            uint64
	Coord                                 ChunkCoord
	TerrainChunk                          *content.TerrainChunkDef
	ImportedWorldChunk                    *content.ImportedWorldChunkDef
	ImportedWorldAux                      *content.ImportedWorldChunkAuxDef
	ImportedWorldAuxHit                   bool
	ImportedWorldAuxMiss                  bool
	PreparedImportedWorldGeometry         *volume.XBrickMap
	PreparedImportedWorldGeometryCacheKey string
	PlacementItems                        []streamedPlacementInstance
	ObjectSnapshots                       map[string]*content.VoxelObjectSnapshotDef
	objectSnapshotGeometry                map[string]*streamedObjectSnapshotGeometry
	PrepareDuration                       time.Duration
	Err                                   error
}

type streamedChunkLoadJob struct {
	prepareCancel             <-chan struct{}
	Generation                uint64
	Coord                     ChunkCoord
	LevelPath                 string
	Loader                    *RuntimeContentLoader
	TerrainManifestPath       string
	TerrainEntry              *content.TerrainChunkEntryDef
	TerrainOverride           *content.TerrainChunkOverrideDef
	ImportedWorldManifestPath string
	ImportedWorldEntry        *content.ImportedWorldChunkEntryDef
	ImportedWorldOverride     *content.ImportedWorldChunkOverrideDef
	HasImportedWorldBacking   bool
	Placements                []streamedPlacementInstance
	VoxelOverrides            map[string]content.VoxelObjectOverrideDef
	WorldDeltaPath            string
	PreparedGeometryCache     *streamedPreparedGeometryCache
}

type streamedSectorProxyLoadJob struct {
	prepareCancel         <-chan struct{}
	Generation            uint64
	SectorCoord           ChunkCoord
	ManifestPath          string
	LOD                   content.ImportedWorldLODDef
	Loader                *RuntimeContentLoader
	PreparedGeometryCache *streamedPreparedGeometryCache
}

type streamedPreparedSectorProxy struct {
	prepareCancel            <-chan struct{}
	loadScope                *RuntimeContentLoadScope
	pendingCredit            *streamedPendingPreparedCredit
	registration             *streamedGeometryRegistration
	retryCost                int64
	Generation               uint64
	SectorCoord              ChunkCoord
	LOD                      content.ImportedWorldLODDef
	Chunk                    *content.ImportedWorldChunkDef
	Aux                      *content.ImportedWorldChunkAuxDef
	AuxHit                   bool
	AuxMiss                  bool
	PreparedGeometry         *volume.XBrickMap
	PreparedGeometryCacheKey string
	Err                      error
	PrepareDuration          time.Duration
}

func (StreamedLevelRuntimeModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&VoxelWorldDirtyChunks{Imported: make(map[voxelWorldDirtyChunkKey]*content.ImportedWorldChunkDef)})
	cmd.AddResources(&StreamedLevelRuntimeState{
		DesiredChunks:                 make(map[ChunkCoord]struct{}),
		KeepChunks:                    make(map[ChunkCoord]struct{}),
		CollisionChunks:               make(map[ChunkCoord]struct{}),
		DesiredSectors:                make(map[ChunkCoord]struct{}),
		KeepSectors:                   make(map[ChunkCoord]struct{}),
		DesiredProxySectors:           make(map[ChunkCoord]struct{}),
		KeepProxySectors:              make(map[ChunkCoord]struct{}),
		PendingLoads:                  make(map[ChunkCoord]struct{}),
		PendingProxyLoads:             make(map[ChunkCoord]struct{}),
		PreparedLoads:                 make(chan streamedPreparedChunk, 256),
		PreparedProxyLoads:            make(chan streamedPreparedSectorProxy, 256),
		PreparedGeometryCache:         newStreamedPreparedGeometryCache(defaultStreamedPreparedGeometryCacheEntries),
		LoadedChunks:                  make(map[ChunkCoord]*streamedLoadedChunk),
		LoadedSectorProxies:           make(map[ChunkCoord]*streamedLoadedSectorProxy),
		PlacementsByChunk:             make(map[ChunkCoord][]streamedPlacementInstance),
		PlacementChunk:                make(map[string]ChunkCoord),
		ObjectChunk:                   make(map[string]ChunkCoord),
		TerrainEntries:                make(map[ChunkCoord]content.TerrainChunkEntryDef),
		ImportedWorldSectors:          make(map[ChunkCoord]content.ImportedWorldSectorDef),
		ImportedChunkSector:           make(map[ChunkCoord]ChunkCoord),
		ImportedWorldEntries:          make(map[ChunkCoord]content.ImportedWorldChunkEntryDef),
		MarkerEntities:                make(map[string]EntityId),
		LightEntities:                 make(map[string]EntityId),
		placementOverrideMap:          make(map[string]content.LevelTransformDef),
		deletedPlacementIDs:           make(map[string]struct{}),
		terrainOverrideMap:            make(map[string]content.TerrainChunkOverrideDef),
		importedWorldOverrideMap:      make(map[string]content.ImportedWorldChunkOverrideDef),
		voxelOverrideMap:              make(map[string]content.VoxelObjectOverrideDef),
		voxelBackingRemovalMap:        make(map[string]content.VoxelBackingRemovalDef),
		navigationDesired:             make(map[content.TerrainChunkCoordDef]struct{}),
		navigationLoads:               make(chan streamedNavigationLoadResult, 2),
		navigationOverlays:            make(chan streamedNavigationOverlayResult, 2),
		navigationRebuilds:            make(chan streamedNavigationRebuildResult, 2),
		navigationEditAnalyses:        make(chan streamedNavigationEditAnalysisResult, 2),
		worldDeltaSaves:               make(chan streamedWorldDeltaSaveResult, 2),
		navigationEditAnalysisPending: make(map[content.TerrainChunkCoordDef]streamedNavigationEditAnalysisItem),
		navigationVoxelSnapshots:      make(map[EntityId]navigationVoxelSnapshot),
		navigationEditRevisions:       make(map[EntityId]uint64),
		navigationQueuedEdits:         make(map[content.TerrainChunkCoordDef]navigationQueuedEdit),
		navigationEditBlockers:        make(map[string]navigationEditBlocker),
		navigationRetireAtLoad:        make(map[uint64]uint64),
		navigationDisabled:            make(map[string]struct{}),
		navigationOpenDoors:           make(map[string]struct{}),
		navigationBlockers:            make(map[string]content.NavBlockerDef),
		navigationOverlayDisabled:     make(map[string]struct{}),
		navigationOverlayOpenDoors:    make(map[string]struct{}),
		navigationOverlayBlockers:     make(map[string]content.NavBlockerDef),
	})
	app.UseSystem(System(updateStreamedLevelObserverSystem).ProfileCategory("streaming").InStage(PreUpdate).RunAlways())
	app.UseSystem(System(commitPreparedStreamedChunksSystem).ProfileCategory("streaming").InStage(Update).RunAlways())
	app.UseSystem(System(streamedLevelNavigationSystem).ProfileCategory("streaming").InStage(Update).RunAlways())
	app.UseSystem(System(streamedLevelNavigationOverlaySystem).ProfileCategory("streaming").InStage(PostUpdate).RunAlways())
	app.UseSystem(System(streamedLevelRuntimeEditedNavigationSystem).ProfileCategory("streaming").InStage(PostUpdate).RunAlways())
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

	if cfg.MaxStreamingWorkItems < 0 {
		return fmt.Errorf("streaming work item budget is negative")
	}
	if cfg.MaxPendingPersistenceBytes < 0 {
		return fmt.Errorf("pending persistence byte budget is negative")
	}
	if cfg.MaxPendingPreparedBytes < 0 {
		return fmt.Errorf("pending prepared byte budget is negative")
	}
	baseLoader := cfg.Loader
	if baseLoader == nil {
		baseLoader = NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: cfg.MaxDecodedContentCacheBytes})
	}
	metadataScope := baseLoader.NewScope()
	published := false
	defer func() {
		if !published {
			metadataScope.Close()
		}
	}()
	loader := metadataScope.Loader()

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

	worldDeltaPath, sessionDeltaDir, worldDelta, err := streamedLevelWorldDelta(cfg, level)
	if err != nil {
		state.InitErr = err
		return err
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

	state.prepareScheduler = streamedPrepareScheduler{}
	state.readyCommits = streamedReadyOwner{}
	state.renderManaged = voxelRtStateFromApp(cmd.app) != nil
	state.streamingWork.blocked = 0
	state.Initialized = true
	state.InitErr = nil
	state.Generation++
	state.Config = cfg
	state.Loader = baseLoader
	state.metadataScope = metadataScope
	state.ownsLoader = cfg.Loader == nil
	published = true
	state.pendingPrepared = newStreamedPendingPreparedOwner(cfg.MaxPendingPreparedBytes)
	state.pendingChunkCostHints = make(map[ChunkCoord]int64)
	state.pendingProxyCostHints = make(map[ChunkCoord]int64)
	state.chunkPrepareCancels = make(map[ChunkCoord]chan struct{})
	state.proxyPrepareCancels = make(map[ChunkCoord]chan struct{})
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
	state.sessionDeltaDir = sessionDeltaDir
	state.releaseObserverSelection()
	state.Metrics = StreamedLevelRuntimeMetrics{}
	state.persistenceTransaction = nil
	state.persistenceIntents = nil
	state.persistenceBytes = 0
	state.persistenceCmd = cmd
	refreshStreamedPersistenceMetrics(state)
	state.nextMetricsLogAt = time.Time{}
	state.TerrainID = ""
	state.TerrainPalette = AssetId{}
	state.BaseWorldID = ""
	state.BaseWorldManifest = nil
	state.BaseWorldBacking = nil
	state.BaseWorldBackingSourceHash = ""
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
	state.PreparedGeometryCache = newStreamedPreparedGeometryCache(cfg.MaxPreparedGeometryCacheEntries, cfg.MaxPreparedGeometryCacheBytes)
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
	state.voxelBackingRemovalMap = make(map[string]content.VoxelBackingRemovalDef)
	state.BaseNavManifestPath = ""
	state.BaseNavManifest = nil
	state.NavigationSources = nil
	state.NavigationGraphs = nil
	state.NavigationRevision = 0
	state.navigationQuery = nil
	state.navigationDisabled = make(map[string]struct{})
	state.navigationOpenDoors = make(map[string]struct{})
	state.navigationBlockers = make(map[string]content.NavBlockerDef)
	state.navigationOverlayDisabled = make(map[string]struct{})
	state.navigationOverlayOpenDoors = make(map[string]struct{})
	state.navigationOverlayBlockers = make(map[string]content.NavBlockerDef)
	state.navigationOverlayRequestedGen = 0
	state.navigationOverlayActive = false
	state.navigationDesired = make(map[content.TerrainChunkCoordDef]struct{})
	state.navigationLoadedGen = 0
	state.navigationRequestedGen = 0
	state.navigationPendingGen = 0
	state.navigationPendingSources = nil
	state.navigationPendingGraphs = nil
	state.navigationLoadActive = false
	state.navigationRebuildActive = false
	state.navigationEditAnalysisActive = false
	state.navigationEditAnalysisPending = make(map[content.TerrainChunkCoordDef]streamedNavigationEditAnalysisItem)
	state.importedEditCaptures = nil
	state.navigationEditAnalysisSince = time.Time{}
	state.navigationEditAnalysisAt = time.Time{}
	state.navigationVoxelSnapshots = make(map[EntityId]navigationVoxelSnapshot)
	state.navigationEditRevisions = make(map[EntityId]uint64)
	state.navigationEditGeneration = 0
	state.navigationQueuedEdits = make(map[content.TerrainChunkCoordDef]navigationQueuedEdit)
	state.navigationIgnoredRemovals = make(map[content.TerrainChunkCoordDef]map[navigationRemovedVoxel]struct{})
	state.navigationEditQueuedSince = time.Time{}
	state.navigationEditLastQueuedAt = time.Time{}
	state.navigationEditBlockers = make(map[string]navigationEditBlocker)
	state.navigationRetireAtLoad = make(map[uint64]uint64)
	state.worldDeltaSaveActive = false
	state.worldDeltaSaveRequestedGen = 0
	state.worldDeltaSaveActiveGen = 0
	state.worldDeltaSavePending = nil

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
	for _, removal := range worldDelta.VoxelBackingRemovals {
		state.voxelBackingRemovalMap[voxelBackingRemovalRuntimeKey(removal.OwnerKind, removal.OwnerID, removal.ChunkCoord)] = removal
		switch removal.OwnerKind {
		case content.VoxelBackingOwnerTerrain:
			delete(state.terrainOverrideMap, terrainChunkRuntimeKey(removal.OwnerID, removal.ChunkCoord))
		case content.VoxelBackingOwnerImportedWorld:
			delete(state.importedWorldOverrideMap, importedWorldChunkRuntimeKey(removal.OwnerID, removal.ChunkCoord))
		}
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
		if manifest.Backing != nil {
			backingPath := content.ResolveDocumentPath(manifest.Backing.Path, manifestPath)
			backingDef, loadErr := loader.LoadVoxelBacking(backingPath)
			if loadErr != nil {
				state.InitErr = loadErr
				return loadErr
			}
			if backingDef.Kind != manifest.Backing.Kind || backingDef.SourceHash != manifest.Backing.SourceHash || backingDef.BoundsMin != manifest.Backing.BoundsMin || backingDef.BoundsMax != manifest.Backing.BoundsMax {
				err = fmt.Errorf("base world voxel backing metadata does not match manifest")
				state.InitErr = err
				return err
			}
			state.BaseWorldBacking, err = NewPlaneTreeVoxelBacking(backingDef)
			if err != nil {
				state.InitErr = err
				return err
			}
			state.BaseWorldBackingSourceHash = backingDef.SourceHash
		}
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
	if err := configureStreamedNavigationManifest(state, level, cfg); err != nil {
		state.InitErr = err
		return err
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
	for _, node := range level.PathNodes {
		spawnAuthoredLevelPathNode(cmd, state.LevelRoot, level.ID, node)
	}
	for _, trigger := range level.UseTriggers {
		spawnAuthoredLevelUseTrigger(cmd, state.LevelRoot, level.ID, trigger)
	}
	for _, trigger := range level.TriggerVolumes {
		spawnAuthoredLevelTriggerVolume(cmd, state.LevelRoot, level.ID, trigger)
	}
	for _, volume := range level.DamageVolumes {
		spawnAuthoredLevelDamageVolume(cmd, state.LevelRoot, level.ID, volume)
	}
	for _, change := range level.ChangeLevels {
		spawnAuthoredLevelChangeLevel(cmd, state.LevelRoot, level.ID, change)
	}
	for _, charger := range level.Chargers {
		if _, err := spawnAuthoredLevelCharger(cmd, assets, loader, state.LevelRoot, level.ID, cfg.LevelPath, charger); err != nil {
			state.InitErr = err
			return err
		}
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
	for _, npc := range level.NPCs {
		if _, err := spawnAuthoredLevelNPC(cmd, assets, loader, state.LevelRoot, level.ID, cfg.LevelPath, npc); err != nil {
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
				state.PlayerEntity = SpawnGroundedPlayerAtMarkerWithConfig(cmd, marker, *cfg.PlayerConfig)
			} else if level.Player != nil {
				state.PlayerEntity = SpawnGroundedPlayerAtMarkerWithConfig(cmd, marker, groundedPlayerConfigFromLevelPlayer(level.Player))
			} else {
				state.PlayerEntity = SpawnGroundedPlayerAtMarker(cmd, marker)
			}
		}
	}
	cmd.app.FlushCommands()
	TransformHierarchySystem(cmd)
	return nil
}

func RestartStreamedLevelRuntime(cmd *Commands, assets *AssetServer, cfg StreamedLevelRuntimeConfig) error {
	if err := StopStreamedLevelRuntime(cmd); err != nil {
		return err
	}
	if err := StartStreamedLevelRuntime(cmd, assets, cfg); err != nil {
		_ = StopStreamedLevelRuntime(cmd)
		return err
	}
	return nil
}

func StopStreamedLevelRuntime(cmd *Commands) error {
	if cmd == nil || cmd.app == nil {
		return fmt.Errorf("commands is nil")
	}
	state := streamedLevelRuntimeStateFromApp(cmd.app)
	if state == nil {
		return fmt.Errorf("streamed level runtime resource is missing")
	}
	if !state.Initialized {
		return nil
	}

	state.prepareScheduler = streamedPrepareScheduler{}
	cancelAllStreamedPreparation(state)
	if err := joinStreamedPersistence(cmd, state); err != nil {
		return err
	}
	if err := saveStreamedWorldDeltaNow(state); err != nil {
		return err
	}
	if len(state.readyCommits.activeChunks) > 0 {
		for coord := range state.readyCommits.activeChunks {
			if err := persistChunkOverrides(cmd, state, coord, streamedLoadedOrActiveChunk(state, coord)); err != nil {
				return err
			}
		}
		if err := saveStreamedWorldDeltaNow(state); err != nil {
			return err
		}
	}
	waitForStreamedJobsAndDrain(state)
	resetStreamedDrainedScheduling(state)
	for coord, loaded := range state.LoadedChunks {
		if err := persistChunkOverrides(cmd, state, coord, loaded); err != nil {
			resetStreamedDrainedScheduling(state)
			return err
		}
	}
	waitForStreamedJobsAndDrain(state)
	resetStreamedDrainedScheduling(state)
	if err := saveStreamedWorldDeltaNow(state); err != nil {
		return err
	}
	state.Generation++
	state.Initialized = false
	state.releaseObserverSelection()
	// Include targets flushed by a commit that later failed before LoadedChunks
	// publication. The level-root descendant cleanup below removes their CPU
	// entities; their tickets have the same retirement rules as completed chunks.
	for entity := range state.renderTargets {
		retireStreamedRenderTarget(cmd, state, entity)
	}

	var stopErr error
	removed := make(map[EntityId]struct{})
	for coord := range state.LoadedChunks {
		for eid := range state.LoadedChunks[coord].OwnedEntities {
			removed[eid] = struct{}{}
		}
		removeStreamedChunk(cmd, state, coord)
	}
	for coord := range state.readyCommits.activeChunks {
		for entity := range streamedLoadedOrActiveChunk(state, coord).OwnedEntities {
			removed[entity] = struct{}{}
		}
		removeStreamedChunk(cmd, state, coord)
	}
	for coord := range state.LoadedSectorProxies {
		removed[state.LoadedSectorProxies[coord].Entity] = struct{}{}
		unloadStreamedSectorProxy(cmd, state, coord)
	}
	root, player := state.LevelRoot, state.PlayerEntity
	MakeQuery1[Parent](cmd).Map(func(eid EntityId, _ *Parent) bool {
		if _, alreadyRemoved := removed[eid]; !alreadyRemoved && isEntityOrDescendantOf(cmd, eid, root) {
			cmd.RemoveEntity(eid)
			removed[eid] = struct{}{}
		}
		return true
	})
	if root != 0 {
		cmd.RemoveEntity(root)
	}
	if player != 0 {
		cmd.RemoveEntity(player)
	}
	cmd.app.FlushCommands()
	state.releaseAllStreamedTerrainGeometryAssets()
	state.releaseAllStreamedSnapshotGeometryAssets()
	state.PreparedGeometryCache.close(assetServerFromApp(cmd.app))
	carryStreamedWorkAfterStop(state)
	refreshStreamedRuntimeMetricsCounts(state)

	clearVoxelWorldDirtyChunks(cmd.app, state.BaseWorldID)
	if state.sessionDeltaDir != "" {
		if err := os.RemoveAll(state.sessionDeltaDir); err != nil && stopErr == nil {
			stopErr = err
		}
	}
	drainStreamedPreparedResults(state)
	state.InitErr = nil
	// Remove every shallow decoded reference before releasing the session scope.
	clear(state.MarkerEntities)
	clear(state.LightEntities)
	state.TerrainID, state.BaseWorldID, state.BaseWorldBackingSourceHash = "", "", ""
	clear(state.TerrainEntries)
	clear(state.ImportedWorldEntries)
	clear(state.ImportedWorldSectors)
	clear(state.ImportedChunkSector)
	clear(state.PlacementsByChunk)
	clear(state.PlacementChunk)
	clear(state.ObjectChunk)
	clear(state.placementOverrideMap)
	clear(state.deletedPlacementIDs)
	clear(state.terrainOverrideMap)
	clear(state.importedWorldOverrideMap)
	clear(state.voxelOverrideMap)
	clear(state.voxelBackingRemovalMap)
	state.Level, state.WorldDelta = nil, nil
	state.BaseWorldManifest, state.BaseWorldBacking = nil, nil
	state.BaseWorldMaterialLookup = ImportedWorldMaterialLookup{}
	state.metadataScope.Close()
	state.metadataScope = nil
	if state.ownsLoader {
		state.Loader.Clear()
	}
	state.Loader = nil
	refreshStreamedContentOwnerMetrics(state)
	state.LevelID, state.LevelPath, state.WorldDeltaPath, state.WorldDataDir, state.sessionDeltaDir = "", "", "", "", ""
	state.LevelRoot, state.PlayerEntity = 0, 0
	state.BaseNavManifest, state.navigationQuery = nil, nil
	state.BaseWorldManifest, state.BaseWorldBacking = nil, nil
	state.NavigationSources, state.NavigationGraphs = nil, nil
	state.navigationPendingSources, state.navigationPendingGraphs = nil, nil
	state.navigationPendingGen = 0
	state.LoadedChunks = make(map[ChunkCoord]*streamedLoadedChunk)
	state.LoadedSectorProxies = make(map[ChunkCoord]*streamedLoadedSectorProxy)
	state.PendingLoads = make(map[ChunkCoord]struct{})
	state.PendingProxyLoads = make(map[ChunkCoord]struct{})
	clear(state.chunkPrepareCancels)
	clear(state.proxyPrepareCancels)
	state.navigationIgnoredRemovals = nil
	state.navigationEditAnalysisPending = nil
	state.importedEditCaptures = nil
	state.navigationVoxelSnapshots = nil
	state.worldDeltaSavePending = nil
	state.persistenceIntents = nil
	state.persistenceBytes = 0
	refreshStreamedPersistenceMetrics(state)
	state.navigationLoadActive, state.navigationRebuildActive, state.navigationEditAnalysisActive, state.worldDeltaSaveActive = false, false, false, false
	refreshStreamedRuntimeMetricsCounts(state)
	return stopErr
}

func clearVoxelWorldDirtyChunks(app *App, worldID string) {
	dirty := voxelWorldDirtyChunksFromApp(app)
	if dirty == nil {
		return
	}
	dirty.mu.Lock()
	defer dirty.mu.Unlock()
	for key := range dirty.Imported {
		if worldID == "" || key.WorldID == worldID {
			delete(dirty.Imported, key)
		}
	}
}

func drainStreamedPreparedResults(state *StreamedLevelRuntimeState) {
	drainStreamedReadyResults(state)
	for {
		select {
		case prepared := <-state.PreparedLoads:
			acknowledgeStreamedChunkPreparation(state, prepared)
			finishStreamedWorkAttempt(state, prepared.Generation, prepared.prepareCancel)
			prepared.release()
			continue
		default:
		}
		break
	}
	for {
		select {
		case prepared := <-state.PreparedProxyLoads:
			acknowledgeStreamedProxyPreparation(state, prepared)
			finishStreamedWorkAttempt(state, prepared.Generation, prepared.prepareCancel)
			prepared.release()
			continue
		default:
		}
		break
	}
	for {
		select {
		case <-state.navigationLoads:
			continue
		default:
		}
		break
	}
	for {
		select {
		case <-state.navigationOverlays:
			continue
		default:
		}
		break
	}
	for {
		select {
		case <-state.navigationRebuilds:
			continue
		default:
		}
		break
	}
	for {
		select {
		case result := <-state.navigationEditAnalyses:
			finishStreamedImportedAnalysis(state, result)
			continue
		default:
		}
		break
	}
}

func waitForStreamedJobsAndDrain(state *StreamedLevelRuntimeState) {
	cancelAllStreamedPreparation(state)
	drainStreamedReadyResults(state)
	done := make(chan struct{})
	go func() {
		state.jobs.Wait()
		close(done)
	}()
	for {
		select {
		case prepared := <-state.PreparedLoads:
			acknowledgeStreamedChunkPreparation(state, prepared)
			finishStreamedWorkAttempt(state, prepared.Generation, prepared.prepareCancel)
			prepared.release()
		case prepared := <-state.PreparedProxyLoads:
			acknowledgeStreamedProxyPreparation(state, prepared)
			finishStreamedWorkAttempt(state, prepared.Generation, prepared.prepareCancel)
			prepared.release()
		case <-state.navigationLoads:
		case <-state.navigationOverlays:
		case <-state.navigationRebuilds:
		case result := <-state.navigationEditAnalyses:
			finishStreamedImportedAnalysis(state, result)
		case <-done:
			drainStreamedPreparedResults(state)
			clearStreamedDrainedChunkPreparation(state)
			clear(state.proxyPrepareCancels)
			return
		}
	}
}

func streamedLevelWorldDelta(cfg StreamedLevelRuntimeConfig, level *content.LevelDef) (string, string, *content.WorldDeltaDef, error) {
	mode := cfg.DeltaMode
	if mode == "" {
		mode = StreamedLevelDeltaPersistent
	}
	delta := &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: level.ID}
	if mode == StreamedLevelDeltaFresh {
		dir, err := os.MkdirTemp("", "gekko-streamed-level-")
		if err != nil {
			return "", "", nil, err
		}
		return filepath.Join(dir, "session.gkworlddelta"), dir, delta, nil
	}
	if mode != StreamedLevelDeltaPersistent {
		return "", "", nil, fmt.Errorf("unsupported streamed level delta mode %q", mode)
	}
	path := content.DefaultWorldDeltaPath(cfg.LevelPath)
	loaded, err := content.LoadWorldDelta(path)
	if err == nil {
		return path, "", loaded, nil
	}
	if !os.IsNotExist(err) {
		return "", "", nil, err
	}
	return path, "", delta, nil
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
	defer beginStreamedRenderTicketBatch(state)()
	refreshStreamedRenderResidency(cmd, state)
	if state != nil {
		state.persistenceCmd = cmd
		commitStreamedWorldDeltaSave(state)
		if state.InitErr != nil {
			_ = commitStreamedPersistence(cmd, state, false)
		}
	}
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}
	defer reconcileStreamedRenderResidency(cmd, state)
	start := time.Now()
	defer func() {
		state.Metrics.ObserverUpdateDuration = time.Since(start)
		refreshStreamedRuntimeMetricsCounts(state)
		recordStreamingRendererPressure(cmd, state)
		recordStreamingProfilerDuration(cmd.app, state.Metrics.ObserverUpdateDuration)
		maybeEmitStreamedRuntimeMetrics(state, time.Now())
	}()

	updateStreamedObserverSelection(cmd, state)
	advanceStreamedPreparationSchedule(state)
	cancelSatisfiedStreamedPreparation(state)
	for _, id := range state.readyCommits.activeChunks {
		result := state.readyCommits.results[id]
		if !streamedActiveCommitCurrent(state, result.chunk) {
			result.active.cancelling = true
			cancelStreamedActiveChunkPreparation(state, result.chunk)
		}
	}
	_ = commitStreamedPersistence(cmd, state, true)
	for coord, intent := range state.persistenceIntents {
		if streamedLoadedOrActiveChunk(state, coord) != intent.Loaded {
			delete(state.persistenceIntents, coord)
		}
	}
	refreshStreamedPersistenceMetrics(state)
	desired, keep := state.DesiredChunks, state.KeepChunks
	pruneStreamedPendingCostHints(state)
	requestStreamedNavigationResidency(state, desired)
	for coord := range state.LoadedChunks {
		if streamedLoadedChunkNeedsResidencyUpgrade(cmd, state, coord) {
			// ponytail: reload once instead of duplicating the commit path; switch to
			// in-place upgrades only if transition latency is measurable.
			requestStreamedChunkPersistence(cmd, state, coord)
			continue
		}
		if _, ok := keep[coord]; ok {
			continue
		}
		if streamedChunkNeedsRenderProxyBeforeUnload(cmd, state, coord) {
			if sectorCoord, ok := state.ImportedChunkSector[coord]; ok {
				addStreamedTemporaryProxyDemand(state, sectorCoord)
			}
			continue
		}
		if sectorCoord, ok := state.ImportedChunkSector[coord]; ok && !state.renderManaged {
			setStreamedSectorProxyHidden(cmd, state, sectorCoord, false)
		}
		requestStreamedChunkPersistence(cmd, state, coord)
	}
	for coord := range state.readyCommits.activeChunks {
		if active := activeStreamedChunkCommit(state, coord); active != nil && active.cancelling {
			requestStreamedChunkPersistence(cmd, state, coord)
		}
	}
	refreshStreamedPersistenceMetrics(state)
	startStreamedWorldDeltaSave(state)
	if state.InitErr != nil {
		return
	}
	for sectorCoord := range state.LoadedSectorProxies {
		if _, ok := state.KeepProxySectors[sectorCoord]; ok {
			continue
		}
		if state.renderManaged && streamedRenderSectorHasProxy(state, sectorCoord) && !streamedRenderSectorReady(cmd, state, sectorCoord) {
			continue
		}
		if state.streamedSectorProxyUnloadEnabled() {
			unloadStreamedSectorProxy(cmd, state, sectorCoord)
		}
	}
	cancelObsoleteStreamedPreparation(state)
	scheduleStreamedPreparation(cmd, state)
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
	if state.chunkPrepareCancels == nil {
		state.chunkPrepareCancels = make(map[ChunkCoord]chan struct{})
	}
	cancelStreamedPreparation(state.chunkPrepareCancels[job.Coord])
	cancel := make(chan struct{})
	state.chunkPrepareCancels[job.Coord] = cancel
	job.prepareCancel = cancel
	acquireStreamedWorkAttempt(state, job.Generation, cancel)
	recordStreamedPreparationDispatch(state, job.Coord, "full")
	owner, results, jobs := state.pendingPrepared, state.PreparedLoads, &state.jobs
	activeMu, active := &state.activePrepareMu, &state.activeChunkPrepares
	activeMu.Lock()
	*active++
	activeMu.Unlock()
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		defer func() {
			activeMu.Lock()
			*active--
			activeMu.Unlock()
		}()
		result := prepareStreamedChunkLoad(job)
		job = streamedChunkLoadJob{}
		result = admitStreamedPreparedChunk(owner, result)
		if streamedPreparationCancelled(result.prepareCancel) {
			result = cancelledStreamedPreparedChunk(result)
		}
		results <- result
	}()
}

func startStreamedSectorProxyPrepareJob(state *StreamedLevelRuntimeState, job streamedSectorProxyLoadJob) {
	if state == nil {
		return
	}
	if state.proxyPrepareCancels == nil {
		state.proxyPrepareCancels = make(map[ChunkCoord]chan struct{})
	}
	cancelStreamedPreparation(state.proxyPrepareCancels[job.SectorCoord])
	cancel := make(chan struct{})
	state.proxyPrepareCancels[job.SectorCoord] = cancel
	job.prepareCancel = cancel
	acquireStreamedWorkAttempt(state, job.Generation, cancel)
	recordStreamedPreparationDispatch(state, job.SectorCoord, "proxy")
	owner, results, jobs := state.pendingPrepared, state.PreparedProxyLoads, &state.jobs
	activeMu, active := &state.activePrepareMu, &state.activeProxyPrepares
	activeMu.Lock()
	*active++
	activeMu.Unlock()
	jobs.Add(1)
	go func() {
		defer jobs.Done()
		defer func() {
			activeMu.Lock()
			*active--
			activeMu.Unlock()
		}()
		result := prepareStreamedSectorProxyLoad(job)
		job = streamedSectorProxyLoadJob{}
		result = admitStreamedPreparedProxy(owner, result)
		if streamedPreparationCancelled(result.prepareCancel) {
			result = cancelledStreamedPreparedProxy(result)
		}
		results <- result
	}()
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
	if entry, ok := state.ImportedWorldEntries[coord]; ok && (entry.NonEmptyVoxelCount > 0 || state.BaseWorldBacking != nil) {
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

// StreamedLevelCollisionReadyInBounds reports whether all loadable chunks
// intersecting bounds are resident with their requested collision state. It is
// a main-thread readiness check; it never requests or synchronously loads data.
func StreamedLevelCollisionReadyInBounds(cmd *Commands, state *StreamedLevelRuntimeState, boundsMin, boundsMax mgl32.Vec3) bool {
	if cmd == nil || state == nil || !state.Initialized || state.InitErr != nil || state.ChunkSize <= 0 {
		return false
	}
	for axis := 0; axis < 3; axis++ {
		if !isFiniteFloat32(boundsMin[axis]) || !isFiniteFloat32(boundsMax[axis]) {
			return false
		}
		if boundsMin[axis] > boundsMax[axis] {
			boundsMin[axis], boundsMax[axis] = boundsMax[axis], boundsMin[axis]
		}
	}
	minCoord := ChunkCoordFromPosition(boundsMin, state.ChunkSize)
	maxCoord := ChunkCoordFromPosition(boundsMax, state.ChunkSize)
	const maxReadinessChunks = 4096
	spanX, spanY, spanZ := maxCoord.X-minCoord.X+1, maxCoord.Y-minCoord.Y+1, maxCoord.Z-minCoord.Z+1
	// ponytail: readiness probes are local; add a spatial content index if a
	// caller ever needs to validate larger bounds.
	if spanX <= 0 || spanY <= 0 || spanZ <= 0 || spanX > maxReadinessChunks || spanY > maxReadinessChunks/spanX || spanZ > maxReadinessChunks/(spanX*spanY) {
		return false
	}
	for x := minCoord.X; x <= maxCoord.X; x++ {
		for y := minCoord.Y; y <= maxCoord.Y; y++ {
			for z := minCoord.Z; z <= maxCoord.Z; z++ {
				coord := ChunkCoord{X: x, Y: y, Z: z}
				if !streamedChunkHasLoadableContent(state, coord) {
					continue
				}
				if _, requested := state.CollisionChunks[coord]; !requested || state.LoadedChunks[coord] == nil || streamedLoadedChunkNeedsResidencyUpgrade(cmd, state, coord) {
					return false
				}
			}
		}
	}
	return true
}

func commitPreparedStreamedChunksSystem(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState) {
	if state != nil {
		state.Metrics.PlacementCommitUnitsLastFrame = 0
	}
	// Cache maintenance also runs on frames with no queued commits or an
	// initialization error. Workers never delete AssetServer registrations.
	if state != nil && state.Initialized {
		state.PreparedGeometryCache.trim(assets)
		if state.InitErr != nil {
			refreshStreamedRuntimeMetricsCounts(state)
		}
	}
	defer beginStreamedRenderTicketBatch(state)()
	refreshStreamedRenderResidency(cmd, state)
	if state == nil || !state.Initialized || state.InitErr != nil {
		return
	}
	defer reconcileStreamedRenderResidency(cmd, state)
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
	generation := state.Generation
	queue := captureStreamedReadyFrontier(state)
	if state.renderManaged && state.Config.MaxPlacementCommitUnitsPerFrame > 0 || len(state.readyCommits.activeChunks) > 0 {
		serviceStreamedPlacementCommitFrontier(cmd, assets, state, queue, start, generation)
		return
	}
	for {
		if !state.Initialized || state.Generation != generation {
			return
		}
		if streamedCommitFrameBudgetHit(state, start) {
			return
		}
		if queue.Len() == 0 {
			return
		}
		result, present := state.readyCommits.take(heap.Pop(&queue).(streamedReadyCandidate).id)
		if !present {
			continue
		}
		if result.proxy != nil {
			consumeStreamedPreparedProxy(cmd, assets, state, *result.proxy)
		} else {
			consumeStreamedPreparedChunk(cmd, assets, state, *result.chunk)
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
	refreshStreamedWorkMetrics(state)
	refreshStreamedContentOwnerMetrics(state)
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
	state.Metrics.NavigationRebuildActive = state.navigationRebuildActive
	state.Metrics.NavigationRebuildQueuedTileCount = len(state.navigationQueuedEdits)
	state.Metrics.ActiveChunkPrepareJobCount, state.Metrics.ActiveProxyPrepareJobCount = streamedActivePrepareJobBreakdown(state)
	state.Metrics.ActivePrepareJobCount = state.Metrics.ActiveChunkPrepareJobCount + state.Metrics.ActiveProxyPrepareJobCount
	state.Metrics.PreparedChunkQueueDepth = len(state.PreparedLoads) + state.readyCommits.chunkCount
	state.Metrics.PreparedProxyQueueDepth = len(state.PreparedProxyLoads) + state.readyCommits.proxyCount
	state.Metrics.PreparedQueueDepth = state.Metrics.PreparedChunkQueueDepth + state.Metrics.PreparedProxyQueueDepth
	state.Metrics.ActiveChunkCommitCount = len(state.readyCommits.activeChunks)
	cacheStats := state.PreparedGeometryCache.snapshot()
	state.Metrics.PreparedGeometryCacheEntries = cacheStats.Entries
	state.Metrics.PreparedGeometryCacheVoxels = cacheStats.Voxels
	state.Metrics.PreparedGeometryCacheBytes = cacheStats.Bytes
	state.Metrics.PreparedGeometryCachePreparedBytes = cacheStats.PreparedBytes
	state.Metrics.PreparedGeometryCacheAssetBytes = cacheStats.AssetBytes
	state.Metrics.PreparedGeometryCachePinnedBytes = cacheStats.PinnedBytes
	state.Metrics.PreparedGeometryCacheMaxBytes = cacheStats.MaxBytes
	state.Metrics.PreparedGeometryCacheOverBudgetBytes = cacheStats.OverBudgetBytes
	state.Metrics.PreparedGeometryCacheBuildWaits = cacheStats.BuildWaits
	state.Metrics.PreparedGeometryCacheOversizedBypasses = cacheStats.OversizedBypasses
	state.Metrics.PreparedGeometryCacheHits = cacheStats.Hits
	state.Metrics.PreparedGeometryCacheMisses = cacheStats.Misses
	state.Metrics.PreparedGeometryCacheEvictions = cacheStats.Evictions
	state.Metrics.PreparedGeometryCacheEvictionCandidateVisits = cacheStats.EvictionCandidateVisits
	state.Metrics.PreparedGeometryCacheStorageReferenceVisits = cacheStats.StorageReferenceVisits
	state.Metrics.PreparedGeometryAssetRegisters = cacheStats.AssetRegisters
	state.Metrics.PreparedGeometryAssetReuses = cacheStats.AssetReuses
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
		if entry.NonEmptyVoxelCount > 0 || state.BaseWorldBacking != nil {
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
	if prepared.ImportedWorldAuxHit {
		state.Metrics.AuxSidecarHitCount++
	}
	if prepared.ImportedWorldAuxMiss {
		state.Metrics.AuxSidecarMissCount++
	}
}

func recordPreparedStreamedSectorProxyAuxMetrics(state *StreamedLevelRuntimeState, prepared streamedPreparedSectorProxy) {
	if state == nil {
		return
	}
	if prepared.AuxHit {
		state.Metrics.AuxSidecarHitCount++
	}
	if prepared.AuxMiss {
		state.Metrics.AuxSidecarMissCount++
	}
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
		state.renderTicketBatch.pendingEntities, state.renderTicketBatch.pendingComponents = 0, 0
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
		state.Metrics.GPUVoxelRuntimeNormalBakeDuration = rt.RtApp.BufferManager.VoxelRuntimeNormalBakeDuration
		state.Metrics.GPUVoxelUploadRevision = rt.RtApp.BufferManager.VoxelUploadRevision
		retainedStats := rt.RtApp.BufferManager.RetainedVoxelMapStats()
		state.Metrics.GPURetainedVoxelMapEntries = retainedStats.Entries
		state.Metrics.GPURetainedVoxelMapSectors = retainedStats.Sectors
		state.Metrics.GPURetainedVoxelMapHits = retainedStats.Hits
		state.Metrics.GPURetainedVoxelMapMisses = retainedStats.Misses
		state.Metrics.GPURetainedVoxelMapEvictions = retainedStats.Evictions
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
		Generation:              state.Generation,
		Coord:                   coord,
		LevelPath:               state.LevelPath,
		Loader:                  state.Loader,
		PreparedGeometryCache:   state.PreparedGeometryCache,
		Placements:              append([]streamedPlacementInstance(nil), state.PlacementsByChunk[coord]...),
		VoxelOverrides:          make(map[string]content.VoxelObjectOverrideDef),
		WorldDeltaPath:          state.WorldDeltaPath,
		HasImportedWorldBacking: state.BaseWorldBacking != nil,
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
		Generation:            state.Generation,
		SectorCoord:           sectorCoord,
		LOD:                   lod,
		Loader:                state.Loader,
		PreparedGeometryCache: state.PreparedGeometryCache,
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
	addStreamedTemporaryGameplayDemand(state, coord)
	if _, ok := state.LoadedChunks[coord]; ok {
		return nil
	}
	if active := activeStreamedChunkCommit(state, coord); active != nil {
		return finishStreamedActiveChunkSynchronously(cmd, assets, state, active)
	}
	prepared := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(state, coord))
	defer prepared.release()
	recordPreparedStreamedChunkMetrics(state, prepared)
	if prepared.Err != nil {
		state.Metrics.PrepareErrorCount++
		return prepared.Err
	}
	_, err := commitPreparedStreamedChunk(cmd, assets, state, prepared)
	if err != nil {
		state.Metrics.CommitErrorCount++
	}
	prepared.release()
	refreshStreamedRuntimeMetricsCounts(state)
	recordStreamingRendererPressure(cmd, state)
	return err
}

func prepareStreamedSectorProxyLoad(job streamedSectorProxyLoadJob) (result streamedPreparedSectorProxy) {
	start := time.Now()
	result = streamedPreparedSectorProxy{prepareCancel: job.prepareCancel, Generation: job.Generation, SectorCoord: job.SectorCoord}
	var scope *RuntimeContentLoadScope
	defer func() {
		result.PrepareDuration = time.Since(start)
		result.loadScope = scope
		if streamedPreparationCancelled(result.prepareCancel) {
			result = cancelledStreamedPreparedProxy(result)
		} else if result.Err != nil {
			result.release()
			result = streamedPreparedSectorProxy{prepareCancel: result.prepareCancel, Generation: result.Generation, SectorCoord: result.SectorCoord, Err: result.Err, PrepareDuration: result.PrepareDuration}
		}
	}()
	if streamedPreparationCancelled(job.prepareCancel) {
		return result
	}
	if job.Loader == nil {
		result.Err = fmt.Errorf("streamed sector proxy loader is nil")
		return result
	}
	scope = job.Loader.NewScope()
	job.Loader = scope.Loader()
	result.LOD = job.LOD
	if strings.TrimSpace(job.LOD.ChunkPath) == "" {
		result.Err = fmt.Errorf("streamed sector proxy lod path is empty for sector %s", job.SectorCoord.String())
		return result
	}
	chunkPath := content.ResolveDocumentPath(job.LOD.ChunkPath, job.ManifestPath)
	chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
	if err != nil || streamedPreparationCancelled(job.prepareCancel) {
		result.Err = err
		return result
	}
	result.Chunk = chunk
	result.Aux, result.AuxHit = loadStreamedImportedWorldAux(job.Loader, job.LOD.Aux, job.ManifestPath)
	if streamedPreparationCancelled(job.prepareCancel) {
		return result
	}
	result.AuxMiss = !result.AuxHit
	result.PreparedGeometryCacheKey = streamedImportedWorldGeometryCacheKey("sector_proxy", chunkPath, streamedImportedWorldPayloadAndAuxHash(firstNonEmptyString(job.LOD.PayloadHash, chunk.PayloadHash), result.Aux), firstPositiveInt(job.LOD.PayloadSizeBytes, chunk.PayloadSizeBytes))
	result.PreparedGeometry, _ = job.PreparedGeometryCache.getOrBuild(result.PreparedGeometryCacheKey, func() *volume.XBrickMap {
		return prepareImportedWorldChunkGeometry(chunk, result.Aux)
	})
	if !streamedPreparationCancelled(job.prepareCancel) {
		result.registration = prepareStreamedGeometryRegistration(result.PreparedGeometry)
	}
	return result
}

func prepareStreamedChunkLoad(job streamedChunkLoadJob) (result streamedPreparedChunk) {
	start := time.Now()
	result = streamedPreparedChunk{prepareCancel: job.prepareCancel, Generation: job.Generation, Coord: job.Coord}
	var scope *RuntimeContentLoadScope
	defer func() {
		result.PrepareDuration = time.Since(start)
		result.loadScope = scope
		if streamedPreparationCancelled(result.prepareCancel) {
			result = cancelledStreamedPreparedChunk(result)
		} else if result.Err != nil {
			result.release()
			result = streamedPreparedChunk{prepareCancel: result.prepareCancel, Generation: result.Generation, Coord: result.Coord, Err: result.Err, PrepareDuration: result.PrepareDuration}
		}
	}()
	if streamedPreparationCancelled(job.prepareCancel) {
		return result
	}
	if job.Loader != nil {
		scope = job.Loader.NewScope()
		job.Loader = scope.Loader()
	}
	result.PlacementItems = append([]streamedPlacementInstance(nil), job.Placements...)
	result.ObjectSnapshots = make(map[string]*content.VoxelObjectSnapshotDef)
	result.objectSnapshotGeometry = make(map[string]*streamedObjectSnapshotGeometry)
	if job.TerrainOverride != nil {
		chunkPath := content.ResolveDocumentPath(job.TerrainOverride.SnapshotPath, job.WorldDeltaPath)
		chunk, err := job.Loader.LoadTerrainChunk(chunkPath)
		if err != nil || streamedPreparationCancelled(job.prepareCancel) {
			result.Err = err
			return result
		}
		result.TerrainChunk = chunk
	} else if job.TerrainEntry != nil && job.TerrainEntry.NonEmptyVoxelCount > 0 {
		chunkPath := content.ResolveTerrainChunkPath(*job.TerrainEntry, job.TerrainManifestPath)
		chunk, err := job.Loader.LoadTerrainChunk(chunkPath)
		if err != nil || streamedPreparationCancelled(job.prepareCancel) {
			result.Err = err
			return result
		}
		result.TerrainChunk = chunk
	}
	if streamedPreparationCancelled(job.prepareCancel) {
		return result
	}
	if job.ImportedWorldOverride != nil {
		chunkPath := content.ResolveDocumentPath(job.ImportedWorldOverride.SnapshotPath, job.WorldDeltaPath)
		chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
		if err != nil || streamedPreparationCancelled(job.prepareCancel) {
			result.Err = err
			return result
		}
		result.ImportedWorldChunk = chunk
		result.PreparedImportedWorldGeometryCacheKey = streamedImportedWorldGeometryCacheKey("imported_override", chunkPath, chunk.PayloadHash, chunk.PayloadSizeBytes)
		result.PreparedImportedWorldGeometry, _ = job.PreparedGeometryCache.getOrBuild(result.PreparedImportedWorldGeometryCacheKey, func() *volume.XBrickMap {
			return prepareImportedWorldChunkGeometry(chunk, nil)
		})
	} else if job.ImportedWorldEntry != nil && (job.ImportedWorldEntry.NonEmptyVoxelCount > 0 || job.HasImportedWorldBacking) {
		chunkPath := content.ResolveImportedWorldChunkPath(*job.ImportedWorldEntry, job.ImportedWorldManifestPath)
		chunk, err := job.Loader.LoadImportedWorldChunk(chunkPath)
		if err != nil || streamedPreparationCancelled(job.prepareCancel) {
			result.Err = err
			return result
		}
		result.ImportedWorldChunk = chunk
		result.ImportedWorldAux, result.ImportedWorldAuxHit = loadStreamedImportedWorldAux(job.Loader, job.ImportedWorldEntry.Aux, job.ImportedWorldManifestPath)
		if streamedPreparationCancelled(job.prepareCancel) {
			return result
		}
		result.ImportedWorldAuxMiss = !result.ImportedWorldAuxHit
		result.PreparedImportedWorldGeometryCacheKey = streamedImportedWorldGeometryCacheKey("imported_full", chunkPath, streamedImportedWorldPayloadAndAuxHash(firstNonEmptyString(job.ImportedWorldEntry.PayloadHash, chunk.PayloadHash), result.ImportedWorldAux), firstPositiveInt(job.ImportedWorldEntry.PayloadSizeBytes, chunk.PayloadSizeBytes))
		result.PreparedImportedWorldGeometry, _ = job.PreparedGeometryCache.getOrBuild(result.PreparedImportedWorldGeometryCacheKey, func() *volume.XBrickMap {
			return prepareImportedWorldChunkGeometry(chunk, result.ImportedWorldAux)
		})
	}
	if streamedPreparationCancelled(job.prepareCancel) {
		return result
	}
	for key, override := range job.VoxelOverrides {
		if streamedPreparationCancelled(job.prepareCancel) {
			return result
		}
		snapshotPath := content.ResolveDocumentPath(override.SnapshotPath, job.WorldDeltaPath)
		snapshot, err := content.LoadVoxelObjectSnapshot(snapshotPath)
		if err != nil || streamedPreparationCancelled(job.prepareCancel) {
			result.Err = err
			return result
		}
		result.ObjectSnapshots[key] = snapshot
		geometry := XBrickMapFromVoxelObjectSnapshot(snapshot)
		geometry.ComputeAABB()
		geometry.ClearDirty()
		result.objectSnapshotGeometry[key] = &streamedObjectSnapshotGeometry{
			snapshot: snapshot, source: geometry, registration: prepareStreamedGeometryRegistration(geometry),
		}
	}
	if streamedPreparationCancelled(job.prepareCancel) {
		return result
	}
	if result.TerrainChunk != nil && result.TerrainChunk.NonEmptyVoxelCount > 0 {
		result.preparedTerrainGeometry = terrainChunkToXBrickMap(result.TerrainChunk)
		result.preparedTerrainGeometry.ComputeAABB()
		result.preparedTerrainGeometry.ClearDirty()
		if streamedPreparationCancelled(job.prepareCancel) {
			return result
		}
		result.terrainRegistration = prepareStreamedGeometryRegistration(result.preparedTerrainGeometry)
	}
	if !job.HasImportedWorldBacking && !streamedPreparationCancelled(job.prepareCancel) {
		result.registration = prepareStreamedGeometryRegistration(result.PreparedImportedWorldGeometry)
	}
	return result
}

func prepareImportedWorldChunkGeometry(chunk *content.ImportedWorldChunkDef, aux ...*content.ImportedWorldChunkAuxDef) *volume.XBrickMap {
	if chunk == nil || chunk.NonEmptyVoxelCount == 0 {
		return nil
	}
	xbm := ImportedWorldChunkToXBrickMap(chunk)
	if len(aux) > 0 {
		ApplyImportedWorldChunkAuxToXBrickMap(xbm, aux[0])
	}
	xbm.ComputeAABB()
	xbm.ClearDirty()
	return xbm
}

func loadStreamedImportedWorldAux(loader *RuntimeContentLoader, ref *content.ImportedWorldChunkAuxRefDef, manifestPath string) (*content.ImportedWorldChunkAuxDef, bool) {
	if loader == nil || ref == nil || strings.TrimSpace(ref.AuxPath) == "" {
		return nil, false
	}
	aux, err := loader.LoadImportedWorldChunkAux(content.ResolveDocumentPath(ref.AuxPath, manifestPath))
	if err != nil || aux == nil {
		return nil, false
	}
	if ref.NormalBakeVersion != "" && aux.NormalBakeVersion != ref.NormalBakeVersion {
		loader.releaseScopedValue(aux)
		return nil, false
	}
	if ref.PayloadHash != "" && aux.PayloadHash != ref.PayloadHash {
		loader.releaseScopedValue(aux)
		return nil, false
	}
	if ref.SourcePayloadHash != "" && aux.SourcePayloadHash != ref.SourcePayloadHash {
		loader.releaseScopedValue(aux)
		return nil, false
	}
	if ref.SourcePayloadSizeBytes > 0 && aux.SourcePayloadSizeBytes != ref.SourcePayloadSizeBytes {
		loader.releaseScopedValue(aux)
		return nil, false
	}
	return aux, true
}

func streamedImportedWorldPayloadAndAuxHash(payloadHash string, aux *content.ImportedWorldChunkAuxDef) string {
	if aux == nil || aux.PayloadHash == "" {
		return payloadHash
	}
	if payloadHash == "" {
		return "aux:" + aux.PayloadHash
	}
	return payloadHash + ":aux:" + aux.PayloadHash
}

func commitPreparedStreamedSectorProxy(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedSectorProxy) (int, error) {
	defer prepared.registration.release()
	defer beginStreamedWorkCommit(state, prepared.Generation, prepared.prepareCancel)()
	defer beginStreamedRenderTicketBatch(state)()
	if cmd != nil && state != nil && voxelRtStateFromApp(cmd.app) != nil {
		state.renderManaged = true
	}
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
	if !streamedSectorProxyCommitNeeded(state, prepared.SectorCoord) {
		return 0, nil
	}
	worldStart := time.Now()
	spawnTiming := AuthoredImportedWorldSpawnTiming{}
	geometryAssetStart := time.Now()
	preparedGeometryAsset, _, adopted := state.PreparedGeometryCache.acquirePreparedAsset(assets, prepared.PreparedGeometryCacheKey, prepared.PreparedGeometry, prepared.registration)
	if adopted {
		state.Metrics.PreparedGeometryAssetAdoptions++
	}
	geometryAssetDuration := time.Since(geometryAssetStart)
	entity := spawnAuthoredImportedWorldChunkEntity(cmd, state.LevelRoot, state.BaseWorldPalette, AuthoredImportedWorldSpawnDef{
		LevelID:                 state.LevelID,
		WorldID:                 importedWorldIDForPreparedChunk(state, prepared.Chunk),
		ShadowGroupID:           importedWorldGroupIDForStreamedState(state),
		Chunk:                   prepared.Chunk,
		CollisionEnabled:        false,
		DisableTerrainMetadata:  true,
		DisableVoxelAdjacency:   true,
		DisableShadows:          true,
		DisableOcclusionCulling: true,
		ShareTerrainGeometry:    true,
		RetainRendererGeometry:  true,
		PreparedGeometry:        prepared.PreparedGeometry,
		PreparedGeometryAsset:   preparedGeometryAsset,
		Timing:                  &spawnTiming,
	})
	recordImportedWorldSpawnTiming(state, spawnTiming)
	state.Metrics.LastCommitWorldRegisterDuration += geometryAssetDuration
	state.Metrics.LastCommitWorldDuration += time.Since(worldStart)
	if !stageStreamedRenderTarget(cmd, state, entity, prepared.SectorCoord, streamedRenderProxy) && streamedSectorProxyShouldBeHidden(state, prepared.SectorCoord) {
		cmd.AddComponents(entity, &VoxelRenderHiddenComponent{})
	}
	recordStreamedCommitFlush(cmd, state)
	clearEntityVoxelDirty(cmd, entity)
	state.LoadedSectorProxies[prepared.SectorCoord] = &streamedLoadedSectorProxy{
		Entity:        entity,
		LOD:           prepared.LOD,
		GeometryAsset: streamedGeometryAssetLease{ID: preparedGeometryAsset, Server: assets},
	}
	delete(state.prepareScheduler.waiting, streamedPrepareIdentity{coord: prepared.SectorCoord, kind: streamedPrepareProxy})
	reconcileStreamedSectorProxyAfterFullCommit(cmd, state, prepared.SectorCoord)
	entityCount = 1
	committed = true
	return entityCount, nil
}

func commitPreparedStreamedChunk(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, prepared streamedPreparedChunk) (int, error) {
	defer prepared.registration.release()
	defer prepared.terrainRegistration.release()
	defer prepared.releaseObjectSnapshotGeometry()
	defer beginStreamedWorkCommit(state, prepared.Generation, prepared.prepareCancel)()
	defer beginStreamedRenderTicketBatch(state)()
	tx := newStreamedChunkCommitTransaction(false)
	start := time.Now()
	resetLastStreamedCommitBreakdown(state)
	entities := 0
	defer func() {
		if !tx.live(state) {
			return
		}
		duration := time.Since(start)
		state.Metrics.LastCommitCoord = prepared.Coord
		state.Metrics.LastCommitDuration = duration
		state.Metrics.LastCommitEntityCount = entities
		state.Metrics.TotalCommitDuration += duration
	}()
	for {
		count, _, done, err := advanceStreamedChunkCommit(cmd, assets, state, prepared, tx)
		entities += count
		if err != nil || done || !tx.live(state) {
			return entities, err
		}
	}
}

func reconcileStreamedSectorProxyAfterFullCommit(cmd *Commands, state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) {
	if state == nil || state.renderManaged {
		return
	}
	setStreamedSectorProxyHidden(cmd, state, sectorCoord, streamedSectorProxyShouldBeHidden(state, sectorCoord))
}

func streamedSectorProxyCommitNeeded(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) bool {
	if state == nil {
		return false
	}
	// Managed fallbacks remain resident even after full CPU coverage arrives.
	if state.renderManaged {
		return true
	}
	if !streamedSectorFullChunksLoaded(state, sectorCoord) {
		return true
	}
	_, keepFull := state.KeepSectors[sectorCoord]
	return !keepFull
}

func streamedSectorProxyShouldBeHidden(state *StreamedLevelRuntimeState, sectorCoord ChunkCoord) bool {
	return streamedSectorFullChunksLoaded(state, sectorCoord)
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
		retireStreamedRenderTarget(cmd, state, loaded.Entity)
		retainStreamedRendererGeometryForEntity(cmd, loaded.Entity)
		cmd.RemoveEntity(loaded.Entity)
	}
	loaded.GeometryAsset.release(state.PreparedGeometryCache)
	delete(state.LoadedSectorProxies, sectorCoord)
}

func retainStreamedRendererGeometryForEntity(cmd *Commands, eid EntityId) bool {
	if cmd == nil || eid == 0 {
		return false
	}
	rt := voxelRtStateFromApp(cmd.app)
	if rt == nil || rt.RtApp == nil || rt.RtApp.BufferManager == nil {
		return false
	}
	if rt.runtimeEditedVoxelEntity(eid) {
		return false
	}
	vmc, ok := voxelModelComponentForEntity(cmd, eid)
	if !ok || !vmc.RetainRendererGeometry {
		return false
	}
	assets := assetServerFromApp(cmd.app)
	if assets == nil {
		return false
	}
	geometry, ok := ResolveVoxelGeometryMap(assets, &vmc)
	if !ok || geometry == nil {
		return false
	}
	return rt.RtApp.BufferManager.RetainVoxelMap(geometry)
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

func streamedLoadedChunkNeedsResidencyUpgrade(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) bool {
	if cmd == nil || state == nil {
		return false
	}
	loaded := state.LoadedChunks[coord]
	if loaded == nil {
		return false
	}
	wantCollision := state.streamedImportedWorldChunkCollisionEnabled(coord)
	wantDestruction := state.streamedImportedWorldChunkDestructionEnabled(coord)
	for entity := range loaded.ImportedWorldEntities {
		if wantCollision && (cmd.GetComponent(entity, reflect.TypeOf(RigidBodyComponent{})) == nil || cmd.GetComponent(entity, reflect.TypeOf(ColliderComponent{})) == nil || cmd.GetComponent(entity, reflect.TypeOf(AABBComponent{})) == nil) {
			return true
		}
		if wantDestruction && cmd.GetComponent(entity, reflect.TypeOf(StreamedDestructionResidentComponent{})) == nil {
			return true
		}
		if wantDestruction && state.BaseWorldBacking != nil {
			if _, backed := voxelBackingForEntity(cmd, entity); !backed {
				return true
			}
		}
	}
	return false
}

func unloadStreamedChunk(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) error {
	loaded := streamedLoadedOrActiveChunk(state, coord)
	if loaded == nil {
		return nil
	}
	if active := activeStreamedChunkCommit(state, coord); active != nil && state.LoadedChunks[coord] == nil {
		active.cancelling = true
		cancelStreamedActiveChunkPreparation(state, state.readyCommits.results[active.readyID].chunk)
	}
	if err := persistChunkOverrides(cmd, state, coord, loaded); err != nil {
		return err
	}
	removeStreamedChunk(cmd, state, coord)
	return nil
}

func removeStreamedChunk(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord) {
	loaded := streamedLoadedOrActiveChunk(state, coord)
	if loaded == nil {
		return
	}
	for eid := range loaded.OwnedEntities {
		retireStreamedRenderTarget(cmd, state, eid)
		retainStreamedRendererGeometryForEntity(cmd, eid)
		if rt := voxelRtStateFromApp(cmd.app); rt != nil {
			rt.clearRuntimeEditedVoxelEntity(eid)
		}
		cmd.RemoveEntity(eid)
		delete(state.navigationVoxelSnapshots, eid)
		delete(state.navigationEditRevisions, eid)
		state.releaseStreamedTerrainGeometryAsset(eid)
		state.releaseStreamedSnapshotGeometryAsset(eid)
	}
	for _, lease := range loaded.ImportedWorldGeometryAssets {
		lease.release(state.PreparedGeometryCache)
	}
	for objectKey := range loaded.ObjectEntities {
		delete(state.ObjectChunk, objectKey)
	}
	delete(state.LoadedChunks, coord)
	if active := activeStreamedChunkCommit(state, coord); active != nil {
		finishStreamedActiveChunkCommit(state, active)
	}
}

func persistChunkOverrides(cmd *Commands, state *StreamedLevelRuntimeState, coord ChunkCoord, loaded *streamedLoadedChunk) error {
	state.persistenceCmd = cmd
	if err := joinStreamedPersistence(cmd, state); err != nil {
		return err
	}
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
		if intent := state.persistenceIntents[coord]; intent != nil && intent.Loaded == loaded {
			if _, remembered := intent.Entities[eid]; remembered {
				dirty = true
			}
		}
		if !dirty {
			continue
		}
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && backing.Dirty {
			state.recordVoxelBackingRemoval(backing)
			if backing.OwnerKind == content.VoxelBackingOwnerImportedWorld {
				snapshot := importedWorldChunkDefFromXBrickMap(backing.OwnerID, terrainCoordFromArray(backing.ChunkCoord), backing.ChunkSize, voxelResolutionForEntity(cmd, eid), xbm)
				queueSavedStreamedImportedAnalysis(state, snapshot, backing)
			}
			manifestDirty = true
			continue
		}
		vmc, ok := voxelModelComponentForEntity(cmd, eid)
		if !ok {
			continue
		}
		snapshot := terrainChunkDefFromXBrickMap(ref.TerrainID, terrainCoordFromArray(ref.ChunkCoord), vmc.TerrainChunkSize, voxelResolutionForEntity(cmd, eid), xbm)
		snapshotPath, err := writeStreamedLevelPayload(state.WorldDataDir, fmt.Sprintf("terrain_%s_%d_%d_%d.gkchunk", sanitizePathSegment(ref.TerrainID), ref.ChunkCoord[0], ref.ChunkCoord[1], ref.ChunkCoord[2]), func(path string) error {
			return content.SaveTerrainChunk(path, snapshot)
		})
		if err != nil {
			return err
		}
		override := content.TerrainChunkOverrideDef{
			TerrainID:    ref.TerrainID,
			ChunkCoord:   terrainCoordFromArray(ref.ChunkCoord),
			SnapshotPath: content.AuthorDocumentPath(snapshotPath, state.WorldDeltaPath),
		}
		state.terrainOverrideMap[terrainChunkRuntimeKey(ref.TerrainID, override.ChunkCoord)] = override
		state.InvalidateObserverSelection()
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
		if intent := state.persistenceIntents[coord]; intent != nil && intent.Loaded == loaded {
			if _, remembered := intent.Entities[eid]; remembered {
				dirty = true
			}
		}
		if !dirty {
			continue
		}
		if backing, ok := voxelBackingForEntity(cmd, eid); ok && backing.Dirty {
			state.recordVoxelBackingRemoval(backing)
			snapshot := importedWorldChunkDefFromXBrickMap(ref.WorldID, terrainCoordFromArray(ref.ChunkCoord), backing.ChunkSize, voxelResolutionForEntity(cmd, eid), xbm)
			queueSavedStreamedImportedAnalysis(state, snapshot, backing)
			manifestDirty = true
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
		if err := persistImportedWorldRuntimeEditSnapshots(state, []*content.ImportedWorldChunkDef{snapshot}); err != nil {
			return err
		}
		manifestDirty = true
	}

	for objectKey, eid := range loaded.ObjectEntities {
		placementID, itemID := splitVoxelObjectRuntimeKey(objectKey)
		xbm, dirty, exists := currentVoxelMapForEntity(cmd, eid)
		if intent := state.persistenceIntents[coord]; intent != nil && intent.Loaded == loaded {
			if _, remembered := intent.Entities[eid]; remembered {
				dirty = true
			}
		}
		if !dirty && exists {
			continue
		}
		snapshotPath, err := writeStreamedLevelPayload(state.WorldDataDir, fmt.Sprintf("object_%s_%s.gkvoxobj", sanitizePathSegment(placementID), sanitizePathSegment(itemID)), func(path string) error {
			return content.SaveVoxelObjectSnapshot(path, VoxelObjectSnapshotFromXBrickMap(xbm))
		})
		if err != nil {
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
	state.WorldDelta.VoxelBackingRemovals = mapVoxelBackingRemovals(state.voxelBackingRemovalMap)
	state.WorldDelta.VoxelObjectOverrides = mapVoxelOverrides(state.voxelOverrideMap)
	return saveStreamedWorldDeltaNow(state)
}

func persistImportedWorldRuntimeEditSnapshots(state *StreamedLevelRuntimeState, snapshots []*content.ImportedWorldChunkDef) error {
	if state == nil || state.WorldDelta == nil || strings.TrimSpace(state.WorldDeltaPath) == "" || len(snapshots) == 0 {
		return nil
	}
	if state.importedWorldOverrideMap == nil {
		state.importedWorldOverrideMap = make(map[string]content.ImportedWorldChunkOverrideDef)
	}
	changed := false
	for _, snapshot := range snapshots {
		if snapshot == nil || strings.TrimSpace(snapshot.WorldID) == "" {
			continue
		}
		invalidateStreamedImportedCapture(state, snapshot.WorldID, snapshot.Coord)
		if _, backed := state.voxelBackingRemovalMap[voxelBackingRemovalRuntimeKey(content.VoxelBackingOwnerImportedWorld, snapshot.WorldID, snapshot.Coord)]; backed {
			continue
		}
		state.runtimeEditPersistenceMu.Lock()
		snapshotPath, err := writeStreamedLevelPayload(state.WorldDataDir, fmt.Sprintf("imported_%s_%d_%d_%d.gkchunk", sanitizePathSegment(snapshot.WorldID), snapshot.Coord.X, snapshot.Coord.Y, snapshot.Coord.Z), func(path string) error {
			return content.SaveImportedWorldChunk(path, snapshot)
		})
		state.runtimeEditPersistenceMu.Unlock()
		if err != nil {
			return err
		}
		override := content.ImportedWorldChunkOverrideDef{
			WorldID:      snapshot.WorldID,
			ChunkCoord:   snapshot.Coord,
			SnapshotPath: content.AuthorDocumentPath(snapshotPath, state.WorldDeltaPath),
		}
		state.importedWorldOverrideMap[importedWorldChunkRuntimeKey(snapshot.WorldID, snapshot.Coord)] = override
		queueSavedStreamedImportedAnalysis(state, snapshot, nil)
		changed = true
	}
	if !changed {
		return nil
	}
	state.WorldDelta.PlacementTransformOverrides = mapPlacementOverrides(state.placementOverrideMap)
	state.WorldDelta.PlacementDeletions = mapPlacementDeletions(state.deletedPlacementIDs)
	state.WorldDelta.TerrainChunkOverrides = mapTerrainOverrides(state.terrainOverrideMap)
	state.WorldDelta.ImportedWorldChunkOverrides = mapImportedWorldOverrides(state.importedWorldOverrideMap)
	state.WorldDelta.VoxelBackingRemovals = mapVoxelBackingRemovals(state.voxelBackingRemovalMap)
	state.WorldDelta.VoxelObjectOverrides = mapVoxelOverrides(state.voxelOverrideMap)
	return nil
}

func terrainChunkCoordLessForRuntime(a content.TerrainChunkCoordDef, b content.TerrainChunkCoordDef) bool {
	if a.X != b.X {
		return a.X < b.X
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.Z < b.Z
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

func voxelBackingRemovalRuntimeKey(ownerKind, ownerID string, coord content.TerrainChunkCoordDef) string {
	return ownerKind + "|" + ownerID + "|" + content.TerrainChunkKey(coord)
}

func (state *StreamedLevelRuntimeState) voxelBackingRemovalFor(ownerKind, ownerID string, coord content.TerrainChunkCoordDef) *content.VoxelBackingRemovalDef {
	if state == nil {
		return nil
	}
	removal, ok := state.voxelBackingRemovalMap[voxelBackingRemovalRuntimeKey(ownerKind, ownerID, coord)]
	if !ok {
		return nil
	}
	copy := removal
	copy.Bricks = append([]content.VoxelBackingRemovalBrickDef(nil), removal.Bricks...)
	return &copy
}

func (state *StreamedLevelRuntimeState) recordVoxelBackingRemoval(backing *VoxelBackingComponent) {
	if state == nil || backing == nil {
		return
	}
	def := backing.RemovalDef()
	coord := def.ChunkCoord
	state.voxelBackingRemovalMap[voxelBackingRemovalRuntimeKey(def.OwnerKind, def.OwnerID, coord)] = def
	switch def.OwnerKind {
	case content.VoxelBackingOwnerTerrain:
		delete(state.terrainOverrideMap, terrainChunkRuntimeKey(def.OwnerID, coord))
		state.InvalidateObserverSelection()
	case content.VoxelBackingOwnerImportedWorld:
		invalidateStreamedImportedCapture(state, def.OwnerID, coord)
		delete(state.importedWorldOverrideMap, importedWorldChunkRuntimeKey(def.OwnerID, coord))
	}
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

func mapVoxelBackingRemovals(src map[string]content.VoxelBackingRemovalDef) []content.VoxelBackingRemovalDef {
	out := make([]content.VoxelBackingRemovalDef, 0, len(src))
	for _, removal := range src {
		if len(removal.Bricks) > 0 {
			out = append(out, removal)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].OwnerKind != out[j].OwnerKind {
			return out[i].OwnerKind < out[j].OwnerKind
		}
		if out[i].OwnerID != out[j].OwnerID {
			return out[i].OwnerID < out[j].OwnerID
		}
		return terrainChunkCoordLessForRuntime(out[i].ChunkCoord, out[j].ChunkCoord)
	})
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
