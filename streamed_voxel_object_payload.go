package gekko

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gekko3d/gekko/content"
)

// Resolve payloads against canonical authored definitions, never mutable runtime
// asset geometry. Legacy snapshots retain their ordered, unbound records.
func resolveStreamedVoxelObjectPayload(loader *RuntimeContentLoader, placements []streamedPlacementInstance, key string, override content.VoxelObjectOverrideDef, levelPath, deltaPath string) (*content.VoxelObjectSnapshotDef, bool, error) {
	snapshot, v2, _, err := resolveStreamedVoxelObjectPayloadPrepared(loader, placements, key, override, levelPath, deltaPath, nil)
	return snapshot, v2, err
}

// A per-item proof comes only from fully decoded, validated input. The compiled
// worker packet supplies canonical geometry without rebuilding its base map.
func resolveStreamedVoxelObjectPayloadPrepared(loader *RuntimeContentLoader, placements []streamedPlacementInstance, key string, override content.VoxelObjectOverrideDef, levelPath, deltaPath string, packets map[string]*compiledAssetPacket) (*content.VoxelObjectSnapshotDef, bool, *streamedSnapshotProof, error) {
	snapshotPath := filepath.Clean(content.ResolveDocumentPath(override.SnapshotPath, deltaPath))
	payload, info, err := content.LoadVoxelObjectPayload(snapshotPath, nil)
	if err != nil {
		return nil, false, nil, err
	}
	proof := &streamedSnapshotProof{snapshotPath: snapshotPath, placement: override.PlacementID, item: override.ItemID, schema: payload.SchemaVersion, mode: payload.Mode, contentID: info.ContentID, lattice: payload.Lattice, baseIdentity: payload.BaseIdentity}
	if payload.SchemaVersion == content.CurrentVoxelObjectSnapshotSchemaVersion {
		snapshot, err := content.ResolveVoxelObjectPayload(payload, nil, content.VoxelObjectLatticeDef{}, "", "", nil)
		if key == voxelObjectRuntimeKey(override.PlacementID, override.ItemID) {
			bindStreamedSnapshotCompiledProof(proof, placements, packets, levelPath)
		}
		return snapshot, false, proof, err
	}
	if key != voxelObjectRuntimeKey(override.PlacementID, override.ItemID) {
		return nil, true, nil, fmt.Errorf("voxel-object override key binding mismatch")
	}
	var selected *streamedPlacementInstance
	for i := range placements {
		if placements[i].PlacementID == override.PlacementID {
			selected = &placements[i]
			break
		}
	}
	if selected == nil {
		return nil, true, nil, fmt.Errorf("voxel-object placement %q is not selected", override.PlacementID)
	}
	needBase := payload.Mode == content.VoxelObjectPayloadBaseDelta || payload.Mode == content.VoxelObjectPayloadHybridDelta
	var base *content.VoxelObjectSnapshotDef
	lattice := content.VoxelObjectLatticeDef{}
	shape := bindStreamedSnapshotCompiledProof(proof, placements, packets, levelPath)
	if shape != nil {
		lattice = shape.lattice
		if needBase {
			base = VoxelObjectSnapshotFromXBrickMap(shape.source)
		}
	} else {
		canonical, err := loadRuntimeAssetCanonicalPart(loader, content.ResolveDocumentPath(selected.AssetPath, levelPath), override.ItemID, needBase, runtimeAssetCanonicalOptions{})
		if err != nil {
			return nil, true, nil, err
		}
		lattice = canonical.lattice
		if needBase {
			base = canonical.snapshot
			if base == nil {
				base = VoxelObjectSnapshotFromXBrickMap(canonical.geometry)
			}
		}
	}
	snapshot, err := content.ResolveVoxelObjectPayload(payload, base, lattice, override.PlacementID, override.ItemID, nil)
	if err != nil {
		return nil, true, nil, err
	}
	proof.bound = shape != nil && !shape.model && needBase && shape.baseIdentity == payload.BaseIdentity && shape.lattice == payload.Lattice
	return snapshot, true, proof, nil
}

// Pure legacy placements keep direct item lookups. A prepared or current v2
// item activates validation of all selected sibling references before applying
// any override. Only actual spawned items participate in direct legacy lookup.
func resolveLatestStreamedVoxelObjectSnapshots(loader *RuntimeContentLoader, state *StreamedLevelRuntimeState, placement streamedPlacementInstance, actualItems map[string]EntityId, preparedV2 bool, prepared ...*streamedPreparedChunk) (map[string]*content.VoxelObjectSnapshotDef, error) {
	snapshots := make(map[string]*content.VoxelObjectSnapshotDef)
	selected := []streamedPlacementInstance{placement}
	activated := preparedV2
	for itemID := range actualItems {
		key := voxelObjectRuntimeKey(placement.PlacementID, itemID)
		override, exists := state.voxelOverrideMap[key]
		if !exists {
			continue
		}
		var snapshot *content.VoxelObjectSnapshotDef
		var v2 bool
		var err error
		if len(prepared) != 0 && state.Config.EnableManagedPreparedAssets {
			snapshot, v2, err = currentStreamedPreparedSnapshot(prepared[0], state, placement, key, override)
		}
		if err == nil && snapshot == nil {
			snapshot, v2, err = resolveStreamedVoxelObjectPayload(loader, selected, key, override, state.LevelPath, state.WorldDeltaPath)
		}
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
