package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func TestC3f3PublisherRejectsClosedOriginAndConflictingWarmProvenance(t *testing.T) {
	for _, failure := range []string{"closed-origin", "conflicting-provenance"} {
		t.Run(failure, func(t *testing.T) {
			_, path, _ := c3d3Fixture(t)
			owner := NewRuntimeContentLoader()
			packet, err := prepareCompiledAssetPacket(path, owner, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer packet.release()
			assets := c3d3Server()
			loader := owner
			shape := packet.shapes[packet.parts["shape"]]
			var existingID AssetId
			var existingGeometry *volume.XBrickMap
			var existingSnapshot map[[3]int]uint8
			conflictingBase := strings.Repeat("0", 64)
			if conflictingBase == shape.baseIdentity {
				conflictingBase = strings.Repeat("1", 64)
			}
			if failure == "closed-origin" {
				scope := owner.NewScope()
				loader = scope.Loader()
				scope.Close()
			} else {
				key := "compiled-asset-shape:" + shape.contentID
				existingID = assets.RegisterSharedVoxelGeometryWithCacheKey(key, shape.source, key)
				assets.recordVerifiedAuthoredVoxelBase(existingID, shape.lattice, conflictingBase)
				geometry, ok := assets.GetVoxelGeometry(existingID)
				if !ok {
					t.Fatal("warm conflict fixture missing geometry")
				}
				existingGeometry = geometry.XBrickMap
				existingGeometry.SetVoxel(0, 0, 0, 99)
				existingSnapshot = c3cGeometry(VoxelObjectSnapshotFromXBrickMap(existingGeometry).Voxels)
			}
			prepared, err := publishCompiledAssetPacket(packet, assets, loader)
			if err == nil || prepared != nil {
				t.Fatal("publisher accepted invalid ownership/provenance")
			}
			if len(assets.voxPalettes) != 0 {
				t.Fatal("rejected publisher published first-part palette")
			}
			if failure == "closed-origin" {
				if len(assets.voxModels) != 0 {
					t.Fatal("closed-origin publisher published geometry")
				}
				return
			}
			geometry, ok := assets.getVoxelGeometry(existingID)
			keyID, warm := assets.SharedVoxelGeometryByCacheKey("compiled-asset-shape:" + shape.contentID)
			if !ok || !warm || keyID != existingID || len(assets.voxModels) != 1 || geometry.XBrickMap != existingGeometry || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap).Voxels), existingSnapshot) || assets.authoredVoxelBaseIdentity(existingID, shape.lattice) != conflictingBase {
				t.Fatal("publisher bypassed warm conflict or replaced existing geometry/provenance")
			}
			if shape.registration.charge() != 0 {
				t.Fatal("warm conflict retained unused packet registration")
			}
		})
	}
}

func c3f3Runtime(t *testing.T, count, units int) (*s1gFixture, string) {
	t.Helper()
	f, _, path, _ := c3d4aRuntime(t)
	first := f.runtime.PlacementsByChunk[ChunkCoord{}][0]
	f.runtime.PlacementsByChunk[ChunkCoord{}] = nil
	for i := 0; i < count; i++ {
		placement := first
		placement.PlacementID = s1gID(0, i)
		placement.Transform.Position = content.Vec3{float32(i + 1), 1, 1}
		f.runtime.PlacementsByChunk[ChunkCoord{}] = append(f.runtime.PlacementsByChunk[ChunkCoord{}], placement)
	}
	f.runtime.Config.MaxPlacementCommitUnitsPerFrame = units
	return f, path
}
func c3f3Packet(t *testing.T, p streamedPreparedChunk, path string) *compiledAssetPacket {
	t.Helper()
	key, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if p.Err != nil || len(p.compiledAssets) != 1 || p.compiledAssets[key] == nil {
		t.Fatalf("worker missing unique canonical compiled packet: err=%v packets=%d", p.Err, len(p.compiledAssets))
	}
	return p.compiledAssets[key]
}

func TestC3f3WorkerPacketDedupAndPacketOnlyCommitPreservesGlobalGeometry(t *testing.T) {
	f, path := c3f3Runtime(t, 2, 2)
	beforeGeometry := len(f.assets.voxModels)
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	packet := c3f3Packet(t, prepared, path)
	if len(f.assets.voxModels) != beforeGeometry || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != 0 {
		t.Fatal("worker published AssetServer geometry or ECS entities")
	}
	shape := packet.shapes[packet.parts["body"]]
	if shape == nil || shape.registration.charge() <= 0 {
		t.Fatal("worker omitted owned registration")
	}
	expected := c3cGeometry(VoxelObjectSnapshotFromXBrickMap(shape.source).Voxels)
	handle := shape.registration
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range header.Shapes {
		if err := os.Remove(filepath.Join(filepath.Dir(path), ref.Path)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	f.runtime.Loader.Clear()
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	first := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 0), "body")
	second := placementItemEntityByIDForStreamedTest(f.cmd, s1gID(0, 1), "body")
	if first == 0 || second == 0 {
		t.Fatal("packet-only repeated placements failed to publish")
	}
	a := s3cComponent[VoxelModelComponent](t, f.cmd, first)
	b := s3cComponent[VoxelModelComponent](t, f.cmd, second)
	if a.GeometryAsset() != b.GeometryAsset() {
		t.Fatal("packet publication registered duplicate ordinary geometry")
	}
	global := a.GeometryAsset()
	geometry, ok := f.assets.getVoxelGeometry(global)
	if !ok || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(geometry.XBrickMap).Voxels), expected) {
		t.Fatal("packet-only commit changed geometry")
	}
	if f.assets.authoredVoxelBaseIdentity(global, shape.lattice) != shape.baseIdentity {
		t.Fatal("packet publication lost verified original base")
	}
	palette, ok := f.assets.GetVoxelPalette(a.VoxelPalette)
	if !ok || palette.VoxPalette[1] != [4]uint8{100, 100, 100, 255} {
		t.Fatal("packet-only commit lost material binding")
	}
	if handle.charge() != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("committed packet retained pending handles/credit")
	}
	shape.source.SetVoxel(0, 0, 0, 99)
	if found, value := geometry.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
		t.Fatal("packet source aliases adopted live geometry")
	}
	if err := EnableManagedVoxelGeometry(f.cmd, f.assets, first); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	if err := ApplyManagedVoxelWrites(f.cmd, f.assets, first, p1dWrites(volume.VoxelWrite{Value: 77})); err != nil {
		t.Fatal(err)
	}
	if found, value := geometry.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
		t.Fatal("managed packet edit leaked into ordinary shared geometry")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.assets.getVoxelGeometry(global); !ok {
		t.Fatal("stream Stop deleted adopted ordinary global geometry")
	}
	prepared.release()
	packet.release()
	if _, ok := f.assets.getVoxelGeometry(global); !ok {
		t.Fatal("late envelope release deleted adopted global geometry")
	}
}

func TestC3f3PendingChargeDedupAndTerminalHandleDrain(t *testing.T) {
	for _, terminal := range []string{"deferred", "cancelled", "stale"} {
		t.Run(terminal, func(t *testing.T) {
			f, path := c3f3Runtime(t, 2, 2)
			job := buildStreamedChunkLoadJob(f.runtime, ChunkCoord{})
			one := job
			one.Placements = one.Placements[:1]
			single := prepareStreamedChunkLoad(one)
			singlePacket := c3f3Packet(t, single, path)
			defer single.release()
			both := prepareStreamedChunkLoad(job)
			packet := c3f3Packet(t, both, path)
			defer both.release()
			source := packet.shapes[packet.parts["body"]]
			minimum := runtimeContentGraphCharge(packet.def) + runtimeContentGraphCharge(packet.animations) + streamedPendingGeometryCharge(source.source) + source.registration.charge()
			if charge := streamedPreparedChunkCharge(both); charge < minimum {
				t.Fatalf("pending charge omitted packet metadata/source/registration: %d < %d", charge, minimum)
			}
			sourcePair := streamedPendingGeometryCharge(singlePacket.shapes[singlePacket.parts["body"]].source) + singlePacket.shapes[singlePacket.parts["body"]].registration.charge()
			if extra := streamedPreparedChunkCharge(both) - streamedPreparedChunkCharge(single); extra < 0 || extra >= sourcePair {
				t.Fatal("repeated placement charged duplicate source/registration")
			}
			handle := source.registration
			switch terminal {
			case "deferred":
				owner := newStreamedPendingPreparedOwner(1)
				blocking, ok := owner.reserve(1)
				if !ok {
					t.Fatal("budget fixture reservation")
				}
				defer blocking.release()
				result := admitStreamedPreparedChunk(owner, both)
				if result.retryCost <= 0 || result.pendingCredit != nil || owner.snapshot().Bytes != 1 {
					t.Fatal("packet deferral changed pending owner")
				}
			case "cancelled":
				cancel := make(chan struct{})
				both.prepareCancel = cancel
				close(cancel)
				result := admitStreamedPreparedChunk(newStreamedPendingPreparedOwner(1<<30), both)
				if len(result.compiledAssets) != 0 {
					t.Fatal("cancelled result retained packet")
				}
			case "stale":
				both.Generation = f.runtime.Generation - 1
				both = admitStreamedPreparedChunk(f.runtime.pendingPrepared, both)
				f.runtime.PreparedLoads <- both
				f.commitStage()
				if placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
					t.Fatal("stale generation published packet or retained credit")
				}
			}
			if handle.charge() != 0 {
				t.Fatal("terminal path retained packet registration")
			}
			if len(f.assets.voxModels) != 0 {
				t.Fatal("discarded packet published AssetServer geometry")
			}
		})
	}
}

func TestC3f3ConsumedPublisherRebuildsDeletedIDWithoutSourceReads(t *testing.T) {
	_, path, _ := c3d3Fixture(t)
	owner := NewRuntimeContentLoader()
	packet, err := prepareCompiledAssetPacket(path, owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer packet.release()
	assets := c3d3Server()
	first, err := publishCompiledAssetPacket(packet, assets, owner)
	if err != nil {
		t.Fatal(err)
	}
	oldID, ok := PreparedAuthoredAssetPartGeometry(first, "shape")
	if !ok {
		t.Fatal("first packet publisher missing geometry")
	}
	// A direct consumer has the same global ordinary owner; releasing stream packets cannot revoke it.
	direct, err := LoadAndPrepareAuthoredAsset(path, assets, owner)
	if err != nil {
		t.Fatal(err)
	}
	directID, _ := PreparedAuthoredAssetPartGeometry(direct, "shape")
	if directID != oldID {
		t.Fatal("packet publisher disagrees with direct compiled cache namespace")
	}
	shape := packet.shapes[packet.parts["shape"]]
	if shape.registration.charge() != 0 {
		t.Fatal("publisher failed to consume registration")
	}
	if !assets.DeleteVoxelGeometry(oldID) {
		t.Fatal("public deletion fixture failed")
	}
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range header.Shapes {
		if _, err := os.Stat(filepath.Join(filepath.Dir(path), ref.Path)); err == nil {
			if err := os.Remove(filepath.Join(filepath.Dir(path), ref.Path)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	owner.Clear()
	rebuilt, err := publishCompiledAssetPacket(packet, assets, owner)
	if err != nil {
		t.Fatal("consumed packet did not rebuild deleted ordinary ID", err)
	}
	newID, ok := PreparedAuthoredAssetPartGeometry(rebuilt, "shape")
	if !ok || newID == oldID {
		t.Fatal("consumed publisher returned missing/deleted geometry")
	}
	registered, ok := assets.getVoxelGeometry(newID)
	if !ok || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(registered.XBrickMap).Voxels), c3cGeometry(VoxelObjectSnapshotFromXBrickMap(shape.source).Voxels)) {
		t.Fatal("consumed packet rebuild lost primary source")
	}
	packet.release()
	if _, ok := assets.getVoxelGeometry(newID); !ok {
		t.Fatal("packet release deleted rebuilt ordinary global owner")
	}
}

func TestC3f3PacketPartialHookFailureStopDrainsUnusedHandles(t *testing.T) {
	f, path := c3f3Runtime(t, 2, 1)
	direct, err := LoadAndPrepareAuthoredAsset(path, f.assets, f.runtime.Loader)
	if err != nil {
		t.Fatal(err)
	}
	acceptedID, _ := PreparedAuthoredAssetPartGeometry(direct, "body")
	secondHeader, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondHeader.Asset.Parts[0].Source.VoxelShape.Voxels = []content.VoxelObjectVoxelDef{{X: 8, Value: 1}}
	secondSource := filepath.Join(t.TempDir(), "second-source.gkasset")
	c3cWrite(t, secondSource, secondHeader.Asset)
	secondPath := filepath.Join(t.TempDir(), "second.gkassetc")
	if _, err := CompileAuthoredAsset(secondSource, secondPath, nil); err != nil {
		t.Fatal(err)
	}
	f.runtime.PlacementsByChunk[ChunkCoord{}][1].AssetPath = secondPath
	var spawned []EntityId
	f.runtime.Config.PlacementHooks = append(f.runtime.Config.PlacementHooks, func(_ *Commands, context PostSpawnPlacementContext) {
		spawned = append(spawned, context.SpawnResult.Entities...)
		f.runtime.InitErr = fmt.Errorf("packet hook failure")
	})
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	if prepared.Err != nil || len(prepared.compiledAssets) != 2 {
		t.Fatal("two compiled packet fixture failed", prepared.Err)
	}
	key, err := filepath.Abs(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	unvisited := prepared.compiledAssets[filepath.Clean(key)]
	if unvisited == nil {
		t.Fatal("unvisited second compiled packet missing")
	}
	handle := unvisited.shapes[unvisited.parts["body"]].registration
	f.runtime.PreparedLoads <- prepared
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	if f.runtime.InitErr == nil || len(spawned) < 2 {
		t.Fatal("packet hook fixture did not publish owned partial placement")
	}
	if handle.charge() <= 0 {
		t.Fatal("hook fixture did not retain an unvisited packet handle")
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	for _, entity := range spawned {
		if f.cmd.EntityExists(entity) {
			t.Fatal("partial packet placement entity leaked after Stop")
		}
	}
	if handle.charge() != 0 || f.runtime.Metrics.PendingPreparedBytes != 0 {
		t.Fatal("partial failure Stop retained packet handles/credit")
	}
	if _, ok := f.assets.getVoxelGeometry(acceptedID); !ok {
		t.Fatal("packet Stop revoked independently accepted ordinary geometry")
	}
}

func TestC3f3RemainingPacketUnitsHonorLatestMoveDeleteAndOverride(t *testing.T) {
	f, path := c3f3Runtime(t, 4, 1)
	header, _, err := content.LoadCompiledAssetHeader(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	lattice := authoredVoxelShapeLattice(header.Asset.Parts[0].VoxelResolution)
	payloadPath := filepath.Join(t.TempDir(), "latest.gkvoxobj")
	placement := s1gID(0, 3)
	key := voxelObjectRuntimeKey(placement, "body")
	payload := &content.VoxelObjectPayloadDef{SchemaVersion: 2, Mode: content.VoxelObjectPayloadFull, PlacementID: placement, ItemID: "body", Lattice: lattice, Voxels: []content.VoxelObjectVoxelDef{{Value: 3}}}
	if _, err := content.SaveVoxelObjectPayload(payloadPath, payload, nil); err != nil {
		t.Fatal(err)
	}
	f.runtime.voxelOverrideMap[key] = content.VoxelObjectOverrideDef{PlacementID: placement, ItemID: "body", SnapshotPath: payloadPath}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	c3f3Packet(t, prepared, path)
	f.runtime.PreparedLoads <- prepared
	f.commitStage()
	f.runtime.deletedPlacementIDs[s1gID(0, 1)] = struct{}{}
	f.runtime.placementOverrideMap[s1gID(0, 2)] = content.LevelTransformDef{Position: content.Vec3{1601, 1, 1}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	current := content.LevelTransformDef{Position: content.Vec3{7, 2, 3}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{2, 1, 1}}
	f.runtime.placementOverrideMap[placement] = current
	payload.Voxels[0].Value = 9
	if _, err := content.SaveVoxelObjectPayload(payloadPath, payload, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		f.commitStage()
	}
	if placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 1)) != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 2)) != 0 {
		t.Fatal("packet cursor published deleted or moved stale placement")
	}
	entity := placementItemEntityByIDForStreamedTest(f.cmd, placement, "body")
	if entity == 0 {
		t.Fatal("remaining packet placement missing")
	}
	currentMap, _, ok := currentVoxelMapForEntity(f.cmd, entity)
	if !ok {
		t.Fatal("remaining packet override missing")
	}
	if found, value := currentMap.GetVoxel(0, 0, 0); !found || value != 9 {
		t.Fatal("packet cursor reapplied prepared override instead of latest payload")
	}
	root := placementEntityByIDForStreamedTest(f.cmd, placement)
	if tr := s3cComponent[LocalTransformComponent](t, f.cmd, root); tr.Position != mgl32.Vec3(current.Position) || tr.Scale != mgl32.Vec3(current.Scale) {
		t.Fatal("packet cursor ignored latest placement transform")
	}
}

func TestC3f3MismatchedPlacementPacketFallsBackToSelectedAsset(t *testing.T) {
	_, selectedPath, selectedAsset := c3d3Fixture(t)
	_, _, unrelatedPath, _ := c3d4aRuntime(t)
	loader := NewRuntimeContentLoader()
	unrelated, err := prepareCompiledAssetPacket(unrelatedPath, loader, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer unrelated.release()
	assets := c3d3Server()
	expected, err := LoadAndPrepareAuthoredAsset(selectedPath, assets, loader)
	if err != nil {
		t.Fatal(err)
	}
	expectedID, ok := PreparedAuthoredAssetPartGeometry(expected, "shape")
	if !ok {
		t.Fatal("selected asset fixture missing shape")
	}
	beforeGeometry := len(assets.voxModels)
	app := NewApp()
	placement := AuthoredPlacementSpawnDef{PlacementID: "selected", AssetPath: filepath.Base(selectedPath), Transform: content.LevelTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}
	result, err := spawnAuthoredLevelPlacementWithPacket(app.Commands(), assets, loader, 0, "level", filepath.Join(filepath.Dir(selectedPath), "level.gklevel"), placement, unrelated, nil)
	if err != nil {
		t.Fatal("mismatched packet must fall back to selected loader", err)
	}
	app.FlushCommands()
	entity := result.EntitiesByAssetID["shape"]
	if entity == 0 || result.EntitiesByAssetID["body"] != 0 {
		t.Fatal("mismatched packet published unrelated items instead of selected asset")
	}
	item := s3cComponent[AuthoredLevelItemRefComponent](t, app.Commands(), entity)
	root := s3cComponent[AuthoredLevelPlacementRefComponent](t, app.Commands(), result.RootEntity)
	model := s3cComponent[VoxelModelComponent](t, app.Commands(), entity)
	if item.AssetID != selectedAsset.ID || item.ItemID != "shape" || item.PlacementID != placement.PlacementID || item.AssetPath != placement.AssetPath || root.AssetPath != placement.AssetPath || model.GeometryAsset() != expectedID {
		t.Fatal("mismatched packet changed selected asset ownership, paths or geometry")
	}
	if _, published := assets.SharedVoxelGeometryByCacheKey("compiled-asset-shape:" + unrelated.parts["body"]); published || len(assets.voxModels) != beforeGeometry {
		t.Fatal("mismatched packet published unrelated geometry")
	}
}
