package gekko

import (
	"fmt"
	"strings"

	"github.com/gekko3d/gekko/content"
)

// Resolve payloads against canonical authored definitions, never mutable runtime
// asset geometry. Legacy snapshots retain their ordered, unbound records.
func resolveStreamedVoxelObjectPayload(loader *RuntimeContentLoader, placements []streamedPlacementInstance, key string, override content.VoxelObjectOverrideDef, levelPath, deltaPath string) (*content.VoxelObjectSnapshotDef, bool, error) {
	payload, _, err := content.LoadVoxelObjectPayload(content.ResolveDocumentPath(override.SnapshotPath, deltaPath), nil)
	if err != nil {
		return nil, false, err
	}
	if payload.SchemaVersion == content.CurrentVoxelObjectSnapshotSchemaVersion {
		snapshot, err := content.ResolveVoxelObjectPayload(payload, nil, content.VoxelObjectLatticeDef{}, "", "", nil)
		return snapshot, false, err
	}
	if key != voxelObjectRuntimeKey(override.PlacementID, override.ItemID) {
		return nil, true, fmt.Errorf("voxel-object override key binding mismatch")
	}
	var selected *streamedPlacementInstance
	for i := range placements {
		if placements[i].PlacementID == override.PlacementID {
			selected = &placements[i]
			break
		}
	}
	if selected == nil {
		return nil, true, fmt.Errorf("voxel-object placement %q is not selected", override.PlacementID)
	}
	needBase := payload.Mode == content.VoxelObjectPayloadBaseDelta || payload.Mode == content.VoxelObjectPayloadHybridDelta
	canonical, err := loadRuntimeAssetCanonicalPart(loader, content.ResolveDocumentPath(selected.AssetPath, levelPath), override.ItemID, needBase, runtimeAssetCanonicalOptions{})
	if err != nil {
		return nil, true, err
	}
	var base *content.VoxelObjectSnapshotDef
	if needBase {
		base = canonical.snapshot
		if base == nil {
			base = VoxelObjectSnapshotFromXBrickMap(canonical.geometry)
		}
	}
	snapshot, err := content.ResolveVoxelObjectPayload(payload, base, canonical.lattice, override.PlacementID, override.ItemID, nil)
	return snapshot, true, err
}

// Pure legacy placements keep direct item lookups. A prepared or current v2
// item activates validation of all selected sibling references before applying
// any override. Only actual spawned items participate in direct legacy lookup.
func resolveLatestStreamedVoxelObjectSnapshots(loader *RuntimeContentLoader, state *StreamedLevelRuntimeState, placement streamedPlacementInstance, actualItems map[string]EntityId, preparedV2 bool) (map[string]*content.VoxelObjectSnapshotDef, error) {
	snapshots := make(map[string]*content.VoxelObjectSnapshotDef)
	selected := []streamedPlacementInstance{placement}
	activated := preparedV2
	for itemID := range actualItems {
		key := voxelObjectRuntimeKey(placement.PlacementID, itemID)
		override, exists := state.voxelOverrideMap[key]
		if !exists {
			continue
		}
		snapshot, v2, err := resolveStreamedVoxelObjectPayload(loader, selected, key, override, state.LevelPath, state.WorldDeltaPath)
		if err != nil {
			return nil, err
		}
		activated = activated || v2
		snapshots[key] = snapshot
	}
	if activated {
		for key, override := range state.voxelOverrideMap {
			// Explicit ownership disambiguates placement IDs containing NUL;
			// a nested placement can otherwise share this key prefix.
			if override.PlacementID != placement.PlacementID {
				continue
			}
			if !strings.HasPrefix(key, placement.PlacementID+"\x00") {
				continue
			}
			if _, resolved := snapshots[key]; resolved {
				continue
			}
			snapshot, _, err := resolveStreamedVoxelObjectPayload(loader, selected, key, override, state.LevelPath, state.WorldDeltaPath)
			if err != nil {
				return nil, err
			}
			snapshots[key] = snapshot
		}
	}
	return snapshots, nil
}
