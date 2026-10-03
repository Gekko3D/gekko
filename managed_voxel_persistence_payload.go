package gekko

import (
	"math"
	"path/filepath"
	"slices"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// This token stays on the coordinator. Worker inputs contain only owned records
// and immutable scalar metadata, never managed owners or live map pointers.
type managedVoxelPersistenceToken struct {
	entry      *managedVoxelGeometry
	generation uint64
	binding    *managedVoxelPersistenceBinding
}

// Admission conservatively treats each assignment as a distinct codec brick.
// This guarantees the default profile fits without materializing or encoding
// records; larger candidates use the unchanged full snapshot path.
func managedVoxelPersistenceCandidate(cmd *Commands, assets *AssetServer, state *StreamedLevelRuntimeState, eid EntityId, owner, item string) (content.VoxelObjectPayloadDef, *managedVoxelGeometry, managedVoxelPayloadPlan, bool) {
	identity, lattice, qualified := managedVoxelPersistenceBase(cmd, assets, state, eid, owner, item)
	if !qualified {
		return content.VoxelObjectPayloadDef{}, nil, managedVoxelPayloadPlan{}, false
	}
	_, entry, _ := managedVoxelEntity(cmd, assets, eid)
	count, tracked := entry.owner.TrackedChangeCount()
	limits := voxelcodec.DefaultLimits()
	if !tracked || count < 0 || count > limits.MaxBricks || count > limits.MaxVoxels {
		return content.VoxelObjectPayloadDef{}, nil, managedVoxelPayloadPlan{}, false
	}
	// The original base must have a valid profile proof. Its decoded geometry
	// growth remains conservatively bounded when the loader merges this delta.
	// One final assignment can add a brick or expand a uniform primary
	// channel; 80 bytes of header plus 512 values bounds either operation.
	base := entry.persistenceBinding
	if base == nil || base.baseBricks < 0 || base.baseBricks > limits.MaxBricks || base.baseVoxels < 0 || base.baseVoxels > limits.MaxVoxels || base.baseDecodedBytes <= 0 || base.baseDecodedBytes > limits.MaxDecodedBytes || int64(count) > (limits.MaxDecodedBytes-base.baseDecodedBytes)/(80+512) {
		return content.VoxelObjectPayloadDef{}, nil, managedVoxelPayloadPlan{}, false
	}
	bricks, voxels, counted := entry.owner.CurrentGeometryCounts()
	if !counted || bricks < 0 || bricks > limits.MaxBricks || voxels < 0 || voxels > limits.MaxVoxels {
		return content.VoxelObjectPayloadDef{}, nil, managedVoxelPayloadPlan{}, false
	}
	payload := content.VoxelObjectPayloadDef{SchemaVersion: content.CurrentVoxelObjectPayloadSchemaVersion, Mode: content.VoxelObjectPayloadBaseDelta, PlacementID: owner, ItemID: item, Lattice: lattice, BaseIdentity: identity}
	if content.ValidateVoxelObjectPayloadMetadata(&payload) != nil {
		return content.VoxelObjectPayloadDef{}, nil, managedVoxelPayloadPlan{}, false
	}
	portable := true
	entry.owner.VisitTrackedChanges(func(w volume.VoxelWrite) bool {
		portable = w.X >= math.MinInt32 && w.X <= math.MaxInt32 && w.Y >= math.MinInt32 && w.Y <= math.MaxInt32 && w.Z >= math.MinInt32 && w.Z <= math.MaxInt32
		return portable
	})
	plan := managedVoxelPayloadPlan{records: count}
	if portable && state.Config.EnableHybridVoxelObjectDeltas {
		payload, plan = planManagedVoxelHybrid(payload, entry, count)
	}
	return payload, entry, plan, portable
}

func captureManagedVoxelPersistencePayload(payload content.VoxelObjectPayloadDef, entry *managedVoxelGeometry, plan managedVoxelPayloadPlan) *content.VoxelObjectPayloadDef {
	payload.Voxels = make([]content.VoxelObjectVoxelDef, plan.records)
	i := 0
	if plan.selectors > 0 {
		payload.ReplacementBricks = make([][3]int32, 0, plan.selectors)
		entry.owner.VisitChangedBricks(func(key [6]int, count, current int) bool {
			if _, selected := managedVoxelBrickReplacement(entry.owner, key, count, current); selected {
				payload.ReplacementBricks = append(payload.ReplacementBricks, managedVoxelSelector(key))
				origin := [3]int{key[0]*volume.SectorSize + key[3]*volume.BrickSize, key[1]*volume.SectorSize + key[4]*volume.BrickSize, key[2]*volume.SectorSize + key[5]*volume.BrickSize}
				entry.owner.VisitCurrentBrickVoxels(key, func(local [3]int, value uint8) bool {
					payload.Voxels[i] = content.VoxelObjectVoxelDef{X: origin[0] + local[0], Y: origin[1] + local[1], Z: origin[2] + local[2], Value: value}
					i++
					return true
				})
			}
			return true
		})
		slices.SortFunc(payload.ReplacementBricks, managedVoxelSelectorCompare)
	}
	entry.owner.VisitTrackedChanges(func(w volume.VoxelWrite) bool {
		if len(payload.ReplacementBricks) > 0 {
			brick := func(v int) int32 {
				q := v / volume.BrickSize
				if v%volume.BrickSize < 0 {
					q--
				}
				return int32(q)
			}
			key := [3]int32{brick(w.X), brick(w.Y), brick(w.Z)}
			if _, selected := slices.BinarySearchFunc(payload.ReplacementBricks, key, managedVoxelSelectorCompare); selected {
				return true
			}
		}
		payload.Voxels[i] = content.VoxelObjectVoxelDef{X: w.X, Y: w.Y, Z: w.Z, Value: w.Value}
		i++
		return true
	})
	slices.SortFunc(payload.Voxels, func(a, b content.VoxelObjectVoxelDef) int {
		for _, pair := range [3][2]int{{a.Z, b.Z}, {a.Y, b.Y}, {a.X, b.X}} {
			if pair[0] < pair[1] {
				return -1
			}
			if pair[0] > pair[1] {
				return 1
			}
		}
		return 0
	})
	return &payload
}

func managedVoxelPersistencePayloadBytes(payload content.VoxelObjectPayloadDef, plan managedVoxelPayloadPlan, n *int64) bool {
	return persistenceAdd(n, 1, int64(unsafe.Sizeof(managedVoxelPersistenceToken{}))) && persistenceAdd(n, 1, int64(unsafe.Sizeof(content.VoxelObjectPayloadDef{}))) && persistenceAdd(n, int64(plan.records), int64(unsafe.Sizeof(content.VoxelObjectVoxelDef{}))) && persistenceAdd(n, int64(plan.selectors), int64(unsafe.Sizeof([3]int32{}))) && persistenceAdd(n, int64(len(payload.BaseIdentity)+len(payload.Lattice.RasterizationVersion)+len(payload.PlacementID)+len(payload.ItemID)), 1)
}

// Explicit owner IDs are used only when they exactly reconstruct the actual
// owned key and a trusted bound delta qualifies; legacy keys keep their parsing.
func managedVoxelPersistenceObjectIDs(cmd *Commands, state *StreamedLevelRuntimeState, key string, eid EntityId) (string, string) {
	if ref, found := managedVoxelItemRef(cmd, eid); found && voxelObjectRuntimeKey(ref.PlacementID, ref.ItemID) == key {
		if _, _, _, ok := managedVoxelPersistenceCandidate(cmd, assetServerFromApp(cmd.app), state, eid, ref.PlacementID, ref.ItemID); ok {
			return ref.PlacementID, ref.ItemID
		}
	}
	return splitVoxelObjectRuntimeKey(key)
}

func managedVoxelPersistenceFresh(cmd *Commands, state *StreamedLevelRuntimeState, entity streamedPersistenceEntity) bool {
	token := entity.Managed
	if token == nil {
		return false
	}
	payload := entity.Input.ObjectPayload
	identity, lattice, ok := managedVoxelPersistenceBase(cmd, assetServerFromApp(cmd.app), state, entity.Entity, payload.PlacementID, payload.ItemID)
	_, entry, enabled := managedVoxelEntity(cmd, assetServerFromApp(cmd.app), entity.Entity)
	return ok && enabled && entry == token.entry && entry.generation == token.generation && identity == payload.BaseIdentity && lattice == payload.Lattice
}

// Durable baseline applicability is weaker than freshness: ordinary edits and
// exposure still leave the captured baseline valid for the same authored owner.
func managedVoxelPersistenceBaselineApplies(cmd *Commands, state *StreamedLevelRuntimeState, entity streamedPersistenceEntity) bool {
	token := entity.Managed
	if token == nil || token.binding == nil {
		return false
	}
	binding := token.binding
	payload := entity.Input.ObjectPayload
	ref, found := managedVoxelItemRef(cmd, entity.Entity)
	vmc, exists := voxelModelComponentForEdit(cmd, entity.Entity)
	if !found || !exists || binding.state != state || binding.generation != state.Generation || binding.levelID != state.LevelID || binding.levelPath != filepath.Clean(state.LevelPath) || ref.LevelID != binding.ref.LevelID || ref.AssetID != binding.ref.AssetID || ref.VolumeID != binding.ref.VolumeID || ref.PlacementID != payload.PlacementID || ref.ItemID != payload.ItemID || managedVoxelResolvedPath(ref.AssetPath, state.LevelPath) != binding.assetPath || VoxelResolutionOrDefault(&vmc) != payload.Lattice.VoxelResolution {
		return false
	}
	coord, owned := managedVoxelStreamMembership(state, entity.Entity, payload.PlacementID, payload.ItemID)
	actual, selected := managedVoxelSelectedAssetPath(state, coord, payload.PlacementID)
	return owned && selected && actual == binding.assetPath
}
