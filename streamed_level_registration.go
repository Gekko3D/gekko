package gekko

import (
	"sync"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Each result owns a distinct registration copy. Envelope aliases share this
// single-use handle, never the mutable copy itself. Shared source maps remain
// immutable cache inputs and are never transferred to the renderer.
type streamedGeometryRegistration struct {
	mu         sync.Mutex
	source     *volume.XBrickMap
	geometry   *volume.XBrickMap
	min, max   mgl32.Vec3
	voxelCount int
	bytes      int64
}

func prepareStreamedGeometryRegistration(source *volume.XBrickMap) *streamedGeometryRegistration {
	if source == nil {
		return nil
	}
	geometry := source.Copy()
	min, max := geometry.ComputeAABB()
	geometry.ClearDirty()
	return &streamedGeometryRegistration{
		source: source, geometry: geometry, min: min, max: max,
		voxelCount: geometry.GetVoxelCount(), bytes: streamedPendingGeometryCharge(geometry),
	}
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
	return registration.bytes
}

func (registration *streamedGeometryRegistration) take(source *volume.XBrickMap) (VoxelGeometryAsset, bool) {
	if registration == nil {
		return VoxelGeometryAsset{}, false
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if registration.geometry == nil || registration.source != source {
		return VoxelGeometryAsset{}, false
	}
	asset := VoxelGeometryAsset{
		XBrickMap: registration.geometry, LocalMin: registration.min, LocalMax: registration.max,
		BrickSize: [3]uint32{8, 8, 8}, RuntimeOwned: true,
	}
	registration.source, registration.geometry = nil, nil
	return asset, true
}

func (registration *streamedGeometryRegistration) release() {
	if registration == nil {
		return
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	registration.source, registration.geometry = nil, nil
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
