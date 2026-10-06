package gekko

import (
	"fmt"
	"sync"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// The worker source and captured definition are immutable. Only the independent
// registration copy transfers to a live entity through its single-use handle.
type streamedObjectSnapshotGeometry struct {
	snapshot     *content.VoxelObjectSnapshotDef
	source       *volume.XBrickMap
	registration *streamedGeometryRegistration
	managed      *streamedManagedPreparedPart
}

func (p streamedPreparedChunk) releaseObjectSnapshotGeometry() {
	for _, packet := range p.objectSnapshotGeometry {
		packet.registration.release()
		packet.managed.release()
	}
}

func streamedObjectSnapshotMatches(captured, current *content.VoxelObjectSnapshotDef) bool {
	if captured == current {
		return true
	}
	if captured == nil || current == nil || captured.SchemaVersion != current.SchemaVersion || len(captured.Voxels) != len(current.Voxels) {
		return false
	}
	for i, voxel := range captured.Voxels {
		if voxel != current.Voxels[i] {
			return false
		}
	}
	return true
}

func applyStreamedVoxelObjectSnapshotToEntity(cmd *Commands, state *StreamedLevelRuntimeState, entity EntityId, snapshot *content.VoxelObjectSnapshotDef, packet *streamedObjectSnapshotGeometry) error {
	if packet == nil {
		return applyVoxelObjectSnapshotToEntity(cmd, entity, snapshot)
	}
	defer packet.registration.release()
	vmc, ok := voxelModelComponentForEntity(cmd, entity)
	if !ok {
		return nil
	}
	assets := assetServerFromApp(cmd.app)
	if assets == nil {
		return fmt.Errorf("asset server not available")
	}
	if streamedObjectSnapshotMatches(packet.snapshot, snapshot) {
		if id, adopted := assets.adoptStreamedVoxelGeometry(packet.registration, packet.source); adopted {
			if state.snapshotGeometryAssets == nil {
				state.snapshotGeometryAssets = make(map[EntityId]streamedGeometryAssetLease)
			}
			state.snapshotGeometryAssets[entity] = streamedGeometryAssetLease{ID: id, Server: assets}
			state.Metrics.PreparedGeometryAssetAdoptions++
			vmc.OverrideGeometry = id
			cmd.AddComponents(entity, &vmc)
			return nil
		}
	}
	return applyVoxelObjectSnapshotToEntity(cmd, entity, snapshot)
}

func (state *StreamedLevelRuntimeState) releaseStreamedSnapshotGeometryAsset(entity EntityId) {
	if lease, ok := state.snapshotGeometryAssets[entity]; ok {
		lease.Server.DeleteVoxelGeometry(lease.ID)
		delete(state.snapshotGeometryAssets, entity)
	}
}

func (state *StreamedLevelRuntimeState) releaseAllStreamedSnapshotGeometryAssets() {
	for entity := range state.snapshotGeometryAssets {
		state.releaseStreamedSnapshotGeometryAsset(entity)
	}
}

// Each result owns a distinct registration copy. Envelope aliases share this
// single-use handle, never the mutable copy itself. Managed terrain handles
// also own an independent first-admission renderer copy and its calculated charge.
// Shared source maps remain immutable inputs and never transfer to the renderer.
type streamedGeometryRegistration struct {
	mu                sync.Mutex
	source            *volume.XBrickMap
	sourceHandle      *streamedGeometrySource
	geometry          *volume.XBrickMap
	rendererCopy      *volume.XBrickMap
	rendererBytes     int64
	min, max          mgl32.Vec3
	voxelCount        int
	bytes             int64
	storageDescriptor *streamedGeometryStorageDescriptor
}

func prepareStreamedGeometryRegistration(source *volume.XBrickMap) *streamedGeometryRegistration {
	return prepareStreamedGeometryRegistrationPayload(source, false)
}

// Imported full/proxy workers retain their existing charge capture for cache
// installation. Terrain and snapshots retain only their ordinary geometry charge.
func prepareCachedStreamedGeometryRegistration(source *volume.XBrickMap) *streamedGeometryRegistration {
	return prepareStreamedGeometryRegistrationPayload(source, true)
}

func prepareStreamedSourceRegistration(source *streamedGeometrySource) *streamedGeometryRegistration {
	if source == nil {
		return nil
	}
	geometry := source.registrationCopy()
	min, max := geometry.ComputeAABB()
	geometry.ClearDirty()
	registration := &streamedGeometryRegistration{sourceHandle: source, geometry: geometry, min: min, max: max, voxelCount: geometry.GetVoxelCount()}
	registration.storageDescriptor = captureStreamedGeometryStorageDescriptor(geometry)
	registration.bytes = registration.storageDescriptor.geometryBytes
	return registration
}

func (registration *streamedGeometryRegistration) countForSource(source *streamedGeometrySource) (int, bool) {
	if registration == nil {
		return 0, false
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	return registration.voxelCount, source != nil && registration.geometry != nil && registration.sourceHandle == source && !source.exposed.Load()
}

func prepareStreamedGeometryRegistrationPayload(source *volume.XBrickMap, retainDescriptor bool) *streamedGeometryRegistration {
	if source == nil {
		return nil
	}
	geometry := source.Copy()
	min, max := geometry.ComputeAABB()
	geometry.ClearDirty()
	registration := &streamedGeometryRegistration{
		source: source, geometry: geometry, min: min, max: max,
		voxelCount: geometry.GetVoxelCount(),
	}
	if retainDescriptor {
		registration.storageDescriptor = captureStreamedGeometryStorageDescriptor(geometry)
		registration.bytes = registration.storageDescriptor.geometryBytes
	} else {
		registration.bytes = streamedPendingGeometryCharge(geometry)
	}
	return registration
}

// Called only by eligible terrain workers after registration geometry is final.
// Copy retains fresh structural work and independent runtime storage.
func (registration *streamedGeometryRegistration) prepareRendererCopy() {
	if registration == nil {
		return
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if registration.geometry == nil || registration.rendererCopy != nil {
		return
	}
	registration.rendererCopy = registration.geometry.Copy()
	registration.rendererBytes = streamedPendingGeometryCharge(registration.rendererCopy)
}

func (registration *streamedGeometryRegistration) countFor(source *volume.XBrickMap) (int, bool) {
	if registration == nil {
		return 0, false
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	return registration.voxelCount, registration.geometry != nil && registration.source == source
}

func (registration *streamedGeometryRegistration) charge() int64 {
	if registration == nil {
		return 0
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if registration.geometry == nil {
		return 0
	}
	var descriptorBytes int64
	if registration.storageDescriptor != nil {
		descriptorBytes = registration.storageDescriptor.metadataBytes
	}
	return runtimeContentChargeSum(registration.bytes, registration.rendererBytes, descriptorBytes)
}

func (registration *streamedGeometryRegistration) take(source *volume.XBrickMap) (VoxelGeometryAsset, *volume.XBrickMap, int64, *streamedGeometryStorageDescriptor, bool) {
	return registration.takeIdentity(source, nil)
}

func (registration *streamedGeometryRegistration) takeSource(source *streamedGeometrySource) (VoxelGeometryAsset, *volume.XBrickMap, int64, *streamedGeometryStorageDescriptor, bool) {
	return registration.takeIdentity(nil, source)
}

func (registration *streamedGeometryRegistration) takeIdentity(source *volume.XBrickMap, handle *streamedGeometrySource) (VoxelGeometryAsset, *volume.XBrickMap, int64, *streamedGeometryStorageDescriptor, bool) {
	if registration == nil {
		return VoxelGeometryAsset{}, nil, 0, nil, false
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if registration.geometry == nil || registration.source != source || registration.sourceHandle != handle {
		return VoxelGeometryAsset{}, nil, 0, nil, false
	}
	if handle != nil && handle.exposed.Load() {
		return VoxelGeometryAsset{}, nil, 0, nil, false
	}
	asset := VoxelGeometryAsset{
		XBrickMap: registration.geometry, LocalMin: registration.min, LocalMax: registration.max,
		BrickSize: [3]uint32{8, 8, 8}, RuntimeOwned: true,
	}
	rendererCopy, rendererBytes, descriptor := registration.rendererCopy, registration.rendererBytes, registration.storageDescriptor
	registration.source, registration.geometry, registration.rendererCopy = nil, nil, nil
	registration.sourceHandle = nil
	registration.bytes, registration.rendererBytes = 0, 0
	registration.storageDescriptor = nil
	return asset, rendererCopy, rendererBytes, descriptor, true
}

func (registration *streamedGeometryRegistration) release() {
	if registration == nil {
		return
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	registration.source, registration.geometry, registration.rendererCopy = nil, nil, nil
	registration.sourceHandle = nil
	registration.bytes, registration.rendererBytes = 0, 0
	registration.storageDescriptor = nil
}

// Terrain assets remain private mutable geometry. Their exact registration is
// owned by the spawned entity, including commits that fail before publication.
func (state *StreamedLevelRuntimeState) retainStreamedTerrainGeometryAsset(entity EntityId, server *AssetServer, id AssetId) {
	if entity == 0 {
		server.DeleteVoxelGeometry(id)
		return
	}
	if state.terrainGeometryAssets == nil {
		state.terrainGeometryAssets = make(map[EntityId]streamedGeometryAssetLease)
	}
	state.terrainGeometryAssets[entity] = streamedGeometryAssetLease{ID: id, Server: server}
}

func (state *StreamedLevelRuntimeState) releaseStreamedTerrainGeometryAsset(entity EntityId) {
	if lease, ok := state.terrainGeometryAssets[entity]; ok {
		lease.Server.DeleteVoxelGeometry(lease.ID)
		delete(state.terrainGeometryAssets, entity)
	}
}

func (state *StreamedLevelRuntimeState) releaseAllStreamedTerrainGeometryAssets() {
	for entity := range state.terrainGeometryAssets {
		state.releaseStreamedTerrainGeometryAsset(entity)
	}
}
