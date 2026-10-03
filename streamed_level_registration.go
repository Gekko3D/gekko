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
}

func (p streamedPreparedChunk) releaseObjectSnapshotGeometry() {
	for _, packet := range p.objectSnapshotGeometry {
		packet.registration.release()
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
