package gekko

import (
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"sync"
)

const defaultStreamedPendingPreparedBytes int64 = 128 << 20

type streamedPendingPreparedStats struct {
	Bytes, MaxBytes, OverBudgetBytes      int64
	AdmissionRetries, OversizedAdmissions int
}
type streamedPendingPreparedOwner struct {
	mu    sync.Mutex
	stats streamedPendingPreparedStats
}
type streamedPendingPreparedCredit struct {
	owner     *streamedPendingPreparedOwner
	bytes     int64
	released  bool
	oversized bool
}

func newStreamedPendingPreparedOwner(max int64) *streamedPendingPreparedOwner {
	if max == 0 {
		max = defaultStreamedPendingPreparedBytes
	}
	return &streamedPendingPreparedOwner{stats: streamedPendingPreparedStats{MaxBytes: max}}
}
func (o *streamedPendingPreparedOwner) fits(cost int64) bool {
	return cost >= 0 && o.stats.MaxBytes >= 0 && (o.stats.Bytes == 0 || (o.stats.Bytes <= o.stats.MaxBytes && cost <= o.stats.MaxBytes-o.stats.Bytes))
}
func (o *streamedPendingPreparedOwner) canReserve(cost int64) bool {
	if o == nil {
		return true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.fits(cost)
}
func (o *streamedPendingPreparedOwner) reserve(cost int64) (*streamedPendingPreparedCredit, bool) {
	if o == nil {
		return nil, true
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.fits(cost) {
		o.stats.AdmissionRetries++
		return nil, false
	}
	o.stats.Bytes += cost
	if cost > o.stats.MaxBytes {
		o.stats.OversizedAdmissions++
	}
	return &streamedPendingPreparedCredit{owner: o, bytes: cost, oversized: cost > o.stats.MaxBytes}, true
}
func (c *streamedPendingPreparedCredit) release() {
	if c == nil {
		return
	}
	c.owner.mu.Lock()
	defer c.owner.mu.Unlock()
	if !c.released {
		c.released = true
		c.owner.stats.Bytes -= c.bytes
	}
}

// resize reconciles one credit without releasing its ownership between sizes.
func (c *streamedPendingPreparedCredit) resize(cost int64) bool {
	if c == nil || c.owner == nil {
		return false
	}
	o := c.owner
	o.mu.Lock()
	defer o.mu.Unlock()
	if c.released || cost < 0 {
		return false
	}
	other := o.stats.Bytes - c.bytes
	if cost > int64(^uint64(0)>>1)-other || (other != 0 && (other > o.stats.MaxBytes || cost > o.stats.MaxBytes-other)) {
		o.stats.AdmissionRetries++
		return false
	}
	o.stats.Bytes = other + cost
	c.bytes = cost
	if cost > o.stats.MaxBytes && !c.oversized {
		o.stats.OversizedAdmissions++
		c.oversized = true
	}
	return true
}
func (o *streamedPendingPreparedOwner) snapshot() streamedPendingPreparedStats {
	if o == nil {
		return streamedPendingPreparedStats{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	stats := o.stats
	if stats.Bytes > stats.MaxBytes {
		stats.OverBudgetBytes = stats.Bytes - stats.MaxBytes
	}
	return stats
}
func (p streamedPreparedChunk) release() {
	p.registration.release()
	p.terrainRegistration.release()
	p.releaseObjectSnapshotGeometry()
	for _, candidate := range p.managedPreparedAssets {
		candidate.release()
	}
	for _, packet := range p.compiledAssets {
		packet.release()
	}
	p.loadScope.Close()
	p.pendingCredit.release()
}
func (p streamedPreparedSectorProxy) release() {
	p.registration.release()
	p.loadScope.Close()
	p.pendingCredit.release()
}

// Copy the envelope and sever owner/geometry links before estimating. Geometry
// has its own deliberately bounded traversal, excluding borrowed GPU managers.
func streamedPreparedChunkCharge(p streamedPreparedChunk) int64 {
	geometry := p.PreparedImportedWorldGeometry
	sourceBytes := p.geometrySource.charge()
	p.geometrySource = nil
	registrationBytes := p.registration.charge()
	terrainGeometry := p.preparedTerrainGeometry
	terrainRegistrationBytes := p.terrainRegistration.charge()
	var snapshotGeometryBytes int64
	for _, packet := range p.objectSnapshotGeometry {
		snapshotGeometryBytes = runtimeContentChargeSum(snapshotGeometryBytes, streamedPendingGeometryCharge(packet.source), packet.registration.charge())
	}
	p.objectSnapshotGeometry = nil
	managedBytes := streamedManagedPreparedCharge(p.managedPreparedAssets)
	p.managedPreparedAssets = nil
	compiledAssetBytes := streamedCompiledAssetPacketsCharge(p.compiledAssets)
	p.compiledAssets = nil
	p.registration = nil
	p.terrainRegistration = nil
	p.preparedTerrainGeometry = nil
	p.PreparedImportedWorldGeometry = nil
	p.loadScope = nil
	p.pendingCredit = nil
	p.prepareCancel = nil
	p.Err = nil
	return runtimeContentChargeSum(runtimeContentGraphCharge(p), streamedPendingGeometryCharge(geometry), sourceBytes, registrationBytes,
		streamedPendingGeometryCharge(terrainGeometry), terrainRegistrationBytes, snapshotGeometryBytes, compiledAssetBytes, managedBytes)
}
func streamedPreparedProxyCharge(p streamedPreparedSectorProxy) int64 {
	geometry := p.PreparedGeometry
	sourceBytes := p.geometrySource.charge()
	p.geometrySource = nil
	registrationBytes := p.registration.charge()
	p.registration = nil
	p.PreparedGeometry = nil
	p.loadScope = nil
	p.pendingCredit = nil
	p.prepareCancel = nil
	p.Err = nil
	return runtimeContentChargeSum(runtimeContentGraphCharge(p), streamedPendingGeometryCharge(geometry), sourceBytes, registrationBytes)
}
func streamedPendingGeometryCharge(geometry *volume.XBrickMap) int64 {
	ledger := streamedGeometryStorageLedger{}
	return streamedGeometryStorageCharge(ledger.admit(geometry, streamedGeometryPrepared))
}
func admitStreamedPreparedChunk(owner *streamedPendingPreparedOwner, p streamedPreparedChunk) streamedPreparedChunk {
	if streamedPreparationCancelled(p.prepareCancel) {
		return cancelledStreamedPreparedChunk(p)
	}
	if p.Err != nil {
		p.release()
		return streamedPreparedChunk{prepareCancel: p.prepareCancel, Generation: p.Generation, Coord: p.Coord, Err: p.Err, PrepareDuration: p.PrepareDuration}
	}
	if p.retryCost > 0 {
		return p
	}
	cost := streamedPreparedChunkCharge(p)
	if streamedPreparationCancelled(p.prepareCancel) {
		return cancelledStreamedPreparedChunk(p)
	}
	if p.pendingCredit != nil {
		if p.pendingCredit.resize(cost) {
			return p
		}
		p.release()
		return streamedPreparedChunk{prepareCancel: p.prepareCancel, Generation: p.Generation, Coord: p.Coord, retryCost: cost, PrepareDuration: p.PrepareDuration}
	}
	credit, ok := owner.reserve(cost)
	if !ok {
		p.release()
		return streamedPreparedChunk{prepareCancel: p.prepareCancel, Generation: p.Generation, Coord: p.Coord, retryCost: cost, PrepareDuration: p.PrepareDuration}
	}
	p.pendingCredit = credit
	return p
}
func admitStreamedPreparedProxy(owner *streamedPendingPreparedOwner, p streamedPreparedSectorProxy) streamedPreparedSectorProxy {
	if streamedPreparationCancelled(p.prepareCancel) {
		return cancelledStreamedPreparedProxy(p)
	}
	if p.Err != nil {
		p.release()
		return streamedPreparedSectorProxy{prepareCancel: p.prepareCancel, Generation: p.Generation, SectorCoord: p.SectorCoord, Err: p.Err, PrepareDuration: p.PrepareDuration}
	}
	if p.retryCost > 0 {
		return p
	}
	cost := streamedPreparedProxyCharge(p)
	if streamedPreparationCancelled(p.prepareCancel) {
		return cancelledStreamedPreparedProxy(p)
	}
	if p.pendingCredit != nil {
		if p.pendingCredit.resize(cost) {
			return p
		}
		p.release()
		return streamedPreparedSectorProxy{prepareCancel: p.prepareCancel, Generation: p.Generation, SectorCoord: p.SectorCoord, retryCost: cost, PrepareDuration: p.PrepareDuration}
	}
	credit, ok := owner.reserve(cost)
	if !ok {
		p.release()
		return streamedPreparedSectorProxy{prepareCancel: p.prepareCancel, Generation: p.Generation, SectorCoord: p.SectorCoord, retryCost: cost, PrepareDuration: p.PrepareDuration}
	}
	p.pendingCredit = credit
	return p
}

// Once Stop has drained workers, a persistence failure must leave demand able
// to reschedule in the same generation. No decoded metadata ownership changes.
func resetStreamedDrainedScheduling(state *StreamedLevelRuntimeState) {
	clearStreamedDrainedChunkPreparation(state)
	clear(state.proxyPrepareCancels)
	clear(state.PendingProxyLoads)
	clear(state.pendingChunkCostHints)
	clear(state.pendingProxyCostHints)
	state.navigationLoadActive = false
	state.navigationOverlayActive = false
	state.navigationRebuildActive = false
	state.navigationEditAnalysisActive = false
	state.worldDeltaSaveActive = false
}

// Active ready results were deliberately not drained: preserve only their exact
// current preparation ownership if a later Stop persistence barrier fails.
func clearStreamedDrainedChunkPreparation(state *StreamedLevelRuntimeState) {
	owned := func(coord ChunkCoord, cancel <-chan struct{}) bool {
		result := state.readyCommits.results[state.readyCommits.activeChunks[coord]]
		return result.active != nil && result.chunk.Generation == state.Generation && result.chunk.prepareCancel == cancel
	}
	for coord := range state.PendingLoads {
		if !owned(coord, state.chunkPrepareCancels[coord]) {
			delete(state.PendingLoads, coord)
		}
	}
	for coord, cancel := range state.chunkPrepareCancels {
		if !owned(coord, cancel) {
			delete(state.chunkPrepareCancels, coord)
		}
	}
}

func pruneStreamedPendingCostHints(state *StreamedLevelRuntimeState) {
	for coord := range state.pendingChunkCostHints {
		if _, ok := state.DesiredChunks[coord]; !ok {
			delete(state.pendingChunkCostHints, coord)
		}
	}
	for coord := range state.pendingProxyCostHints {
		if !streamedProxySectorDesired(state, coord) {
			delete(state.pendingProxyCostHints, coord)
		}
	}
}
func refreshStreamedContentOwnerMetrics(state *StreamedLevelRuntimeState) {
	stats := state.Loader.Stats()
	state.Metrics.DecodedContentCacheEntries = stats.Entries
	state.Metrics.DecodedContentCacheBytes = stats.Bytes
	state.Metrics.DecodedContentCachePinnedBytes = stats.PinnedBytes
	state.Metrics.DecodedContentCacheMaxBytes = stats.MaxBytes
	state.Metrics.DecodedContentCacheOverBudgetBytes = stats.OverBudgetBytes
	state.Metrics.DecodedContentCacheHits = stats.Hits
	state.Metrics.DecodedContentCacheMisses = stats.Misses
	state.Metrics.DecodedContentCacheEvictions = stats.Evictions
	state.Metrics.DecodedContentCacheEvictionCandidateVisits = stats.EvictionCandidateVisits
	state.Metrics.DecodedContentCacheLoadWaits = stats.LoadWaits
	state.Metrics.DecodedContentCacheOversizedBypasses = stats.OversizedBypasses
	pending := state.pendingPrepared.snapshot()
	state.Metrics.PendingPreparedBytes = pending.Bytes
	state.Metrics.PendingPreparedMaxBytes = pending.MaxBytes
	state.Metrics.PendingPreparedOverBudgetBytes = pending.OverBudgetBytes
	state.Metrics.PendingPreparedAdmissionRetries = pending.AdmissionRetries
	state.Metrics.PendingPreparedOversizedAdmissions = pending.OversizedAdmissions
}
