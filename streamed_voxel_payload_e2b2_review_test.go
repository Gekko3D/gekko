package gekko

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestE2b2ReviewNULPrefixSiblingPlacementsResolveTheirOwnOverrides(t *testing.T) {
	f, part, path := e2b2Runtime(t)
	first, second := "P", "P\x00nested"
	placement := f.runtime.PlacementsByChunk[ChunkCoord{}][0]
	placement.PlacementID = first
	sibling := placement
	sibling.PlacementID = second
	sibling.Transform.Position[0] += 4
	f.runtime.PlacementsByChunk[ChunkCoord{}] = []streamedPlacementInstance{placement, sibling}
	e2b2Save(t, f, path, e2b2Payload(t, part, first, "body", content.VoxelObjectPayloadFull, 3, false))
	e2b2Save(t, f, filepath.Join(t.TempDir(), "sibling.gkvoxobj"), e2b2Payload(t, part, second, "body", content.VoxelObjectPayloadFull, 7, false))
	e2b2Prepared(t, f)
	f.commitStage()
	if f.hooks[first] != 1 || f.hooks[second] != 0 || p5aAdoptions(t, f.runtime) != 1 {
		t.Fatal("first placement unit validated or published another selected sibling")
	}
	f.commitStage()
	firstBody := e2b2Body(t, f, first)
	secondBody := e2b2Body(t, f, second)
	p1dVoxel(t, s4cLive(t, f.cmd, f.assets, firstBody), -9, 3)
	p1dVoxel(t, s4cLive(t, f.cmd, f.assets, secondBody), -9, 7)
	if p5aAdoptions(t, f.runtime) != 2 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("sibling overrides failed independent adoption and ownership release")
	}
}

func TestE2b2ReviewCollapsedLegacyUnusedItemRetainsDirectLookupSemantics(t *testing.T) {
	f, _, path := e2b2Runtime(t)
	asset := p5eShapeDef()
	assetPath := filepath.Join(filepath.Dir(f.runtime.Config.LevelPath), "placement.gkasset")
	if err := content.SaveAsset(assetPath, asset); err != nil {
		t.Fatal(err)
	}
	id := s1gID(0, 0)
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(_ *Commands, context PostSpawnPlacementContext) {
		if !context.SpawnResult.Collapsed || context.SpawnResult.EntitiesByAssetID["left"] != 0 {
			t.Fatal("fixture did not suppress the individual collapsed item")
		}
		f.hooks[context.Placement.PlacementID]++
	}}
	p5cSnapshot(t, path, 3, false)
	f.runtime.voxelOverrideMap[voxelObjectRuntimeKey(id, "left")] = content.VoxelObjectOverrideDef{PlacementID: id, ItemID: "left", SnapshotPath: path}
	e2b2Prepared(t, f)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	f.commitStage()
	if f.runtime.LoadedChunks[ChunkCoord{}] == nil || f.hooks[id] != 1 || p5aAdoptions(t, f.runtime) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("unused legacy collapsed item changed publication or ownership semantics")
	}
	root := placementEntityByIDForStreamedTest(f.cmd, id)
	if root == 0 || s4cLive(t, f.cmd, f.assets, onlyVoxelEntityForSpawnTest(t, f.cmd, root)).GetVoxelCount() == 0 {
		t.Fatal("collapsed placement failed to publish authored geometry")
	}
}
