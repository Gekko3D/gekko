package gekko

import (
	"fmt"
	"iter"
	"math"
	"reflect"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Managed entries share AssetServer's geometry lifetime. Operations run on the
// engine thread; callers exclusively own source and exposed raw maps.
type managedVoxelGeometry struct {
	authoredBase       authoredVoxelBase
	persistenceBinding *managedVoxelPersistenceBinding
	owner              *volume.ManagedXBrickMap
	exposed            bool
	generation         uint64
	entity             EntityId
	app                *App
	producerActive     bool
}

type managedVoxelBinding struct {
	derivative       *volume.XBrickMap
	id               AssetId
	entry            *managedVoxelGeometry
	generation       uint64
	exposed          bool
	producerAttached bool
}

// RegisterManagedVoxelGeometry seals a defensive CPU copy. GPU-first sources
// require their existing owner and are rejected without changing their mode.
func (server *AssetServer) RegisterManagedVoxelGeometry(source *volume.XBrickMap, sourcePath string) AssetId {
	if server == nil || (source != nil && source.GPUEditMode) {
		return AssetId{}
	}
	return server.registerManagedVoxelOwner(volume.NewManagedXBrickMap(source), sourcePath, nil, 0)
}

func (server *AssetServer) registerManagedVoxelOwner(owner *volume.ManagedXBrickMap, path string, app *App, entity EntityId) AssetId {
	snapshot := owner.Snapshot()
	minB, maxB := snapshot.ComputeAABB()
	snapshot.ClearDirty()
	server.ensureVoxelStorage()
	id := makeAssetId()
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.managedVoxelGeometry == nil {
		server.managedVoxelGeometry = make(map[AssetId]*managedVoxelGeometry)
	}
	server.voxModels[id] = VoxelGeometryAsset{XBrickMap: snapshot, LocalMin: minB, LocalMax: maxB, BrickSize: [3]uint32{8, 8, 8}, SourcePath: path, RuntimeOwned: true}
	server.managedVoxelGeometry[id] = &managedVoxelGeometry{owner: owner, app: app, entity: entity}
	return id
}

func (server *AssetServer) managedVoxelEntry(id AssetId) *managedVoxelGeometry {
	if server == nil {
		return nil
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.managedVoxelGeometry[id]
}

// Pending component resolution is local to editing; committed ECS queries keep
// their stage-flush semantics. Later queued additions win just as at flush.
func voxelEditComponents(cmd *Commands, eid EntityId) []any {
	if cmd == nil || cmd.app == nil {
		return nil
	}
	components := append([]any(nil), cmd.GetAllComponents(eid)...)
	cmd.app.cmdMutex.Lock()
	defer cmd.app.cmdMutex.Unlock()
	for _, entity := range cmd.app.pendingRemovals {
		if entity == eid {
			return nil
		}
	}
	for _, add := range cmd.app.pendingAdditions {
		if add.eid == eid {
			components = append(components, add.components...)
		}
	}
	// Flush inserts pending entities, removes components, then adds components.
	for _, removal := range cmd.app.pendingCompRemovals {
		if removal.eid != eid {
			continue
		}
		for _, removed := range removal.components {
			typ := reflect.TypeOf(removed)
			if typ.Kind() == reflect.Pointer {
				typ = typ.Elem()
			}
			kept := components[:0]
			for _, comp := range components {
				current := reflect.TypeOf(comp)
				if current.Kind() == reflect.Pointer {
					current = current.Elem()
				}
				if current != typ {
					kept = append(kept, comp)
				}
			}
			components = kept
		}
	}
	for _, add := range cmd.app.pendingCompAdds {
		if add.eid == eid {
			components = append(components, add.components...)
		}
	}
	return components
}

func managedVoxelEntity(cmd *Commands, server *AssetServer, eid EntityId) (VoxelModelComponent, *managedVoxelGeometry, bool) {
	vmc, ok := voxelModelComponentForEdit(cmd, eid)
	if !ok {
		return vmc, nil, false
	}
	entry := server.managedVoxelEntry(vmc.GeometryAsset())
	return vmc, entry, entry != nil && vmc.OverrideGeometry != (AssetId{}) && entry.entity == eid && entry.app == cmd.app
}

func validateManagedVoxelEntity(cmd *Commands, assets *AssetServer, eid EntityId) (VoxelModelComponent, error) {
	vmc, ok := voxelModelComponentForEdit(cmd, eid)
	if !ok || assets == nil {
		return vmc, fmt.Errorf("entity %d has no voxel geometry", eid)
	}
	if vmc.IsTerrainChunk || vmc.ShareTerrainGeometry || vmc.IsPlanetTile || vmc.RetainRendererGeometry {
		return vmc, fmt.Errorf("entity %d requires its existing geometry owner", eid)
	}
	for _, comp := range voxelEditComponents(cmd, eid) {
		typ := reflect.TypeOf(comp)
		if typ == nil {
			continue
		}
		if typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		switch typ {
		case reflect.TypeOf(EntityLODComponent{}), reflect.TypeOf(PlanetBodyComponent{}), reflect.TypeOf(AuthoredTerrainChunkRefComponent{}), reflect.TypeOf(AuthoredImportedWorldChunkRefComponent{}), reflect.TypeOf(VoxelBackingComponent{}), reflect.TypeOf(StreamedVoxelRenderComponent{}):
			return vmc, fmt.Errorf("entity %d requires its existing %s owner", eid, typ.Name())
		}
	}
	asset, ok := assets.getVoxelGeometry(vmc.GeometryAsset())
	if !ok || asset.XBrickMap == nil || asset.XBrickMap.GPUEditMode {
		return vmc, fmt.Errorf("entity %d has no eligible CPU geometry", eid)
	}
	return vmc, nil
}

// Managed sources only qualify ordinary CPU entity ownership. Rejection never
// promotes a shared source; callers must explicitly register dense legacy data
// for streamed, retained, terrain, planet, backing or LOD owners.
func managedVoxelRuntimeQualification(cmd *Commands, assets *AssetServer, eid EntityId) error {
	vmc, ok := voxelModelComponentForEdit(cmd, eid)
	if !ok || assets.managedVoxelEntry(vmc.GeometryAsset()) == nil {
		return nil
	}
	_, err := validateManagedVoxelEntity(cmd, assets, eid)
	return err
}

// EnableManagedVoxelGeometry gives an ordinary entity an independent sealed
// override. Unsupported ownership paths reject before queuing any mutation.
func EnableManagedVoxelGeometry(cmd *Commands, assets *AssetServer, eid EntityId) error {
	vmc, err := validateManagedVoxelEntity(cmd, assets, eid)
	if err != nil {
		return err
	}
	if _, _, enabled := managedVoxelEntity(cmd, assets, eid); enabled {
		// Repeated enable may adopt lifetime ownership, but never invent or
		// rebind construction provenance after the initial enablement.
		leaseManagedVoxelOverride(cmd, assets, eid, vmc.GeometryAsset())
		return nil
	}
	source, _ := assets.getVoxelGeometry(vmc.GeometryAsset())
	entry := assets.managedVoxelEntry(vmc.GeometryAsset())
	owner, restoredBase, restoredBinding := restoreManagedVoxelPersistenceOwner(cmd, assets, eid, vmc, source.XBrickMap)
	if owner == nil {
		if entry != nil && !entry.exposed && entry.app == nil {
			owner = entry.owner.Fork()
		} else {
			owner = volume.NewManagedXBrickMap(source.XBrickMap)
		}
	}
	sourceRefs := vmc
	vmc.OverrideGeometry = assets.registerManagedVoxelOwner(owner, source.SourcePath, cmd.app, eid)
	if restoredBinding != nil {
		restored := assets.managedVoxelEntry(vmc.OverrideGeometry)
		restored.authoredBase, restored.persistenceBinding = restoredBase, restoredBinding
	} else {
		assets.qualifyManagedAuthoredVoxelBase(sourceRefs, entry, vmc.OverrideGeometry)
	}
	// Preserve authored pivot bounds; current collision bounds come from payload.
	assets.mu.Lock()
	override := assets.voxModels[vmc.OverrideGeometry]
	override.LocalMin, override.LocalMax = source.LocalMin, source.LocalMax
	assets.voxModels[vmc.OverrideGeometry] = override
	assets.mu.Unlock()
	cmd.AddComponents(eid, &vmc)
	if restoredBinding != nil {
		leaseManagedVoxelOverride(cmd, assets, eid, vmc.OverrideGeometry)
	} else {
		captureManagedVoxelPersistenceBinding(cmd, assets, eid, vmc.OverrideGeometry)
	}
	return nil
}

// ManagedVoxelGeometryChanges reports construction-relative final assignments
// only for explicitly enabled overrides. It is safe inside an ordered producer.
func ManagedVoxelGeometryChanges(cmd *Commands, assets *AssetServer, eid EntityId) ([]volume.VoxelWrite, bool) {
	_, entry, enabled := managedVoxelEntity(cmd, assets, eid)
	if !enabled || entry.exposed {
		return nil, false
	}
	return entry.owner.TrackedChanges()
}

// ApplyManagedVoxelWrites consumes writes once on the engine thread. Producers
// may query tracked changes, but must not mutate, expose, reenter or publish the
// target. Finalization publishes the applied prefix even when a producer panics.
func ApplyManagedVoxelWrites(cmd *Commands, assets *AssetServer, eid EntityId, writes iter.Seq[volume.VoxelWrite]) error {
	vmc, entry, enabled := managedVoxelEntity(cmd, assets, eid)
	if !enabled {
		return fmt.Errorf("entity %d has no enabled managed geometry", eid)
	}
	if _, err := validateManagedVoxelEntity(cmd, assets, eid); err != nil {
		return err
	}
	if writes == nil {
		return nil
	}
	id := vmc.GeometryAsset()
	previousGeneration := entry.generation
	var accepted []volume.VoxelWrite
	publicationComplete := false
	entry.producerActive = true
	defer func() {
		entry.producerActive = false
		if publicationComplete {
			notifyManagedVoxelGeometryContent(cmd, assets, eid, id, entry, accepted, previousGeneration)
		}
	}()
	defer func() {
		if len(accepted) == 0 {
			return
		}
		assets.mu.Lock()
		asset := assets.voxModels[id]
		if !entry.exposed {
			asset.XBrickMap = entry.owner.CopyChangedSectors(asset.XBrickMap, asset.XBrickMap.Revision)
		}
		asset.XBrickMap.ComputeAABB()
		assets.voxModels[id] = asset
		entry.generation++
		assets.mu.Unlock()
		if state := voxelRtStateFromApp(cmd.app); state != nil {
			if binding, ok := state.managedVoxelBindings[eid]; ok && binding.entry == entry && binding.id == id && !entry.exposed {
				if obj := state.GetVoxelObject(eid); obj != nil && obj.XBrickMap != nil && obj.XBrickMap == binding.derivative {
					obj.XBrickMap.ApplyVoxelWrites(func(yield func(volume.VoxelWrite) bool) {
						for _, w := range accepted {
							if !yield(w) {
								return
							}
						}
					})
					binding.generation = entry.generation
					state.managedVoxelBindings[eid] = binding
				}
			}
		}
		MarkVoxelEntityPersistenceDirty(cmd, eid)
		publicationComplete = true
	}()
	sequence := func(yield func(volume.VoxelWrite) bool) {
		for w := range writes {
			var previous uint8
			if entry.exposed {
				asset, _ := assets.getVoxelGeometry(id)
				_, previous = asset.XBrickMap.GetVoxel(w.X, w.Y, w.Z)
			} else {
				_, previous = entry.owner.GetVoxel(w.X, w.Y, w.Z)
			}
			if !yield(w) {
				return
			}
			if previous != w.Value {
				accepted = append(accepted, w)
			}
		}
	}
	if entry.exposed {
		asset, _ := assets.getVoxelGeometry(id)
		asset.XBrickMap.ApplyVoxelWrites(sequence)
	} else {
		entry.owner.ApplyVoxelWrites(sequence)
	}
	return nil
}

// PromoteRuntimeVoxelGeometry permanently selects the current renderer map as
// dense authority. Prior public asset exposure keeps its exact authority pointer.
func PromoteRuntimeVoxelGeometry(cmd *Commands, assets *AssetServer, state *VoxelRtState, eid EntityId) (*volume.XBrickMap, error) {
	vmc, ok := voxelModelComponentForEdit(cmd, eid)
	if !ok || state == nil {
		return nil, fmt.Errorf("entity %d has no attached geometry", eid)
	}
	id := vmc.GeometryAsset()
	entry := assets.managedVoxelEntry(id)
	obj := state.GetVoxelObject(eid)
	if entry == nil || obj == nil || obj.XBrickMap == nil {
		return nil, fmt.Errorf("entity %d has no managed runtime geometry", eid)
	}
	if err := managedVoxelRuntimeQualification(cmd, assets, eid); err != nil {
		return nil, err
	}
	binding, ok := state.managedVoxelBindings[eid]
	if !ok || binding.id != id || binding.entry != entry {
		return nil, fmt.Errorf("entity %d renderer geometry is stale", eid)
	}
	// An inherited source must remain isolated from sibling entities. Qualify the
	// current attachment before queuing the independent override.
	if _, _, enabled := managedVoxelEntity(cmd, assets, eid); !enabled {
		priorExposed := entry.exposed
		if err := EnableManagedVoxelGeometry(cmd, assets, eid); err != nil {
			return nil, err
		}
		vmc, entry, _ = managedVoxelEntity(cmd, assets, eid)
		id = vmc.GeometryAsset()
		binding.id, binding.entry = id, entry
		if priorExposed {
			// Public source exposure already selected asset authority. Preserve its
			// current content through the clone instead of adopting a stale derivative.
			asset, _ := assets.GetVoxelGeometry(id)
			obj.XBrickMap = asset.XBrickMap
		}
	}
	assets.mu.Lock()
	asset := assets.voxModels[id]
	if entry.exposed {
		if obj.XBrickMap != asset.XBrickMap && state.RtApp != nil {
			state.RtApp.Scene.StructureRevision++
		}
		obj.XBrickMap = asset.XBrickMap
	} else {
		asset.XBrickMap = obj.XBrickMap
		asset.XBrickMap.ComputeAABB()
		entry.owner = nil
		entry.exposed = true
		entry.authoredBase = authoredVoxelBase{}
		entry.generation++
		assets.voxModels[id] = asset
	}
	assets.mu.Unlock()
	binding.derivative = obj.XBrickMap
	binding.exposed = true
	binding.generation = entry.generation
	state.managedVoxelBindings[eid] = binding
	state.instanceGeometrySources[eid] = asset.XBrickMap
	state.instanceObjectScopedGeometry[eid] = false
	state.markRuntimeEditedVoxelEntity(eid)
	return asset.XBrickMap, nil
}

func managedSphereWrites(center mgl32.Vec3, radius float32, value uint8) iter.Seq[volume.VoxelWrite] {
	return func(yield func(volume.VoxelWrite) bool) {
		r2 := radius * radius
		minB, maxB := [3]int{}, [3]int{}
		for i := 0; i < 3; i++ {
			minB[i] = int(math.Floor(float64(center[i] - radius)))
			maxB[i] = int(math.Ceil(float64(center[i] + radius)))
		}
		for x := minB[0]; x <= maxB[0]; x++ {
			for y := minB[1]; y <= maxB[1]; y++ {
				for z := minB[2]; z <= maxB[2]; z++ {
					dx, dy, dz := float32(x)-center[0]+0.5, float32(y)-center[1]+0.5, float32(z)-center[2]+0.5
					if dx*dx+dy*dy+dz*dz <= r2 && !yield(volume.VoxelWrite{X: x, Y: y, Z: z, Value: value}) {
						return
					}
				}
			}
		}
	}
}
