package gekko

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

func c3f4bFixture(t *testing.T) string {
	t.Helper()
	input, path, def := c3d3Fixture(t)
	def.MaterialAnimations = []content.AssetMaterialAnimationDef{{ID: "pulse", Kind: "palette", FPS: 12, Mode: "loop", PaletteIndices: []uint8{3, 4}, UVScroll: &content.AssetMaterialUVScrollDef{Velocity: [2]float32{.25, -.5}}, Tags: []string{"animated"}, Frames: []content.AssetMaterialAnimationFrameDef{{Duration: .1, Colors: [][4]uint8{{1, 2, 3, 4}}, EmissiveColors: [][4]uint8{{5, 6, 7, 8}}, Emission: []float32{.4}, Roughness: []float32{.2}, Transparency: []float32{.3}}}}}
	for _, id := range []string{"alias", "binding"} {
		material := def.Materials[0]
		material.ID = id + "-material"
		if id == "binding" {
			material.BaseColor = [4]uint8{90, 80, 70, 255}
			material.Metallic = .1
		}
		def.Materials = append(def.Materials, material)
		part := def.Parts[0]
		part.ID, part.Name = id, id
		shape := *part.Source.VoxelShape
		shape.Palette = append([]content.AssetVoxelPaletteEntryDef(nil), shape.Palette...)
		for i := range shape.Palette {
			shape.Palette[i].MaterialID = material.ID
		}
		part.Source.VoxelShape = &shape
		def.Parts = append(def.Parts, part)
	}
	for _, id := range []string{"geometry", "ordered"} {
		part := def.Parts[0]
		part.ID, part.Name = id, id
		shape := *part.Source.VoxelShape
		if id == "geometry" {
			shape.Voxels = []content.VoxelObjectVoxelDef{{X: 40, Y: -2, Value: 3}}
		} else {
			shape.Palette = append([]content.AssetVoxelPaletteEntryDef(nil), shape.Palette...)
			shape.Palette[0], shape.Palette[1] = shape.Palette[1], shape.Palette[0]
		}
		part.Source.VoxelShape = &shape
		def.Parts = append(def.Parts, part)
	}
	c3cWrite(t, input, def)
	if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	return path
}

func c3f4bDrained(t *testing.T, packet *compiledAssetPacket) {
	t.Helper()
	for _, shape := range packet.shapes {
		if shape.registration.charge() != 0 {
			t.Fatal("terminal packet retained geometry registration")
		}
	}
	for _, palette := range packet.palettes {
		if palette.registration.charge() != 0 || palette.registration.source != nil || palette.registration.palette != nil {
			t.Fatal("terminal packet retained palette publication storage")
		}
	}
}

func TestC3f4bWorkerPaletteDedupTransferAndSourceFreePublication(t *testing.T) {
	path := c3f4bFixture(t)
	f, _ := c3f3Runtime(t, 2, 2)
	for i := range f.runtime.PlacementsByChunk[ChunkCoord{}] {
		f.runtime.PlacementsByChunk[ChunkCoord{}][i].AssetPath = path
	}
	prepared := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(f.runtime, ChunkCoord{}))
	defer prepared.release()
	packet := c3f3Packet(t, prepared, path)
	if len(f.assets.voxModels) != 0 || len(f.assets.voxPalettes) != 0 || placementEntityByIDForStreamedTest(f.cmd, s1gID(0, 0)) != 0 {
		t.Fatal("worker palette preparation published AssetServer/ECS state")
	}
	if len(packet.shapes) != 2 || len(packet.partPalettes) != 6 || len(packet.palettes) != 3 || packet.partPalettes["shape"] != packet.partPalettes["duplicate"] || packet.partPalettes["shape"] != packet.partPalettes["alias"] || packet.partPalettes["shape"] != packet.partPalettes["geometry"] || packet.partPalettes["shape"] == packet.partPalettes["binding"] || packet.partPalettes["shape"] == packet.partPalettes["ordered"] {
		t.Fatal("palette dedup must use full result key rather than geometry or authored binding IDs")
	}
	owned := make(map[string]*VoxelPaletteAsset)
	expected := make(map[string]VoxelPaletteAsset)
	for _, part := range packet.def.Parts {
		if part.Source.Kind != content.AssetSourceKindVoxelShape {
			continue
		}
		built, err := buildAuthoredVoxelShapePalette(packet.def, part)
		if err != nil {
			t.Fatal(err)
		}
		key := packet.partPalettes[part.ID]
		palette := packet.palettes[key]
		if key != voxelPaletteAssetCacheKey(built) || palette == nil || !reflect.DeepEqual(*palette.source, built) || palette.registration.key != key || palette.registration.charge() <= 0 {
			t.Fatal("worker lost full material/animation palette tables")
		}
		owned[key], expected[key] = palette.registration.palette, built
	}
	// Prebuilt palettes, not mutable authoring lookup metadata, are the publication input.
	packet.def.Materials = nil
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	f.runtime.Loader.Clear()
	result, err := publishCompiledAssetPacket(packet, f.assets, f.runtime.Loader)
	if err != nil {
		t.Fatal("packet-only palette publication rebuilt source lookup or reread files", err)
	}
	for _, partID := range []string{"shape", "duplicate", "alias", "binding", "geometry", "ordered"} {
		id, ok := PreparedAuthoredAssetPartPalette(result, partID)
		key := packet.partPalettes[partID]
		got, exists := f.assets.GetVoxelPalette(id)
		if !ok || !exists || !reflect.DeepEqual(got, expected[key]) || reflect.ValueOf(got.Materials).UnsafePointer() != reflect.ValueOf(owned[key].Materials).UnsafePointer() || reflect.ValueOf(got.Animations).UnsafePointer() != reflect.ValueOf(owned[key].Animations).UnsafePointer() {
			t.Fatal("main publication changed/recloned prepared palette or lost binding")
		}
	}
	firstID, _ := PreparedAuthoredAssetPartPalette(result, "shape")
	aliasID, _ := PreparedAuthoredAssetPartPalette(result, "alias")
	differentID, _ := PreparedAuthoredAssetPartPalette(result, "binding")
	if firstID != aliasID || firstID == differentID {
		t.Fatal("prepared palette IDs lost exact-key sharing")
	}
	c3f4bDrained(t, packet)
	live, _ := f.assets.GetVoxelPalette(firstID)
	live.Materials[0].Property["_rough"] = float32(.99)
	live.Animations[0].Frames[0].Emission[0] = .99
	key := packet.partPalettes["shape"]
	if !reflect.DeepEqual(*packet.palettes[key].source, expected[key]) {
		t.Fatal("live global palette mutation aliased packet rebuild source")
	}
	again, err := publishCompiledAssetPacket(packet, f.assets, f.runtime.Loader)
	if err != nil {
		t.Fatal(err)
	}
	againID, _ := PreparedAuthoredAssetPartPalette(again, "shape")
	warm, _ := f.assets.GetVoxelPalette(againID)
	if againID != firstID || warm.Materials[0].Property["_rough"] != float32(.99) || warm.Animations[0].Frames[0].Emission[0] != .99 {
		t.Fatal("repeat consumed packet overwrote mutable global palette")
	}
	packet.release()
	prepared.release()
	if _, exists := f.assets.GetVoxelPalette(firstID); !exists {
		t.Fatal("late packet release deleted ordinary global palette")
	}
	// There is intentionally no public palette delete API. Exercise defensive cold rebuild privately.
	f.assets.mu.Lock()
	delete(f.assets.voxPalettes, firstID)
	delete(f.assets.voxPaletteKeys, key)
	f.assets.mu.Unlock()
	rebuilt, err := publishCompiledAssetPacket(packet, f.assets, f.runtime.Loader)
	if err != nil {
		t.Fatal("consumed palette did not rebuild from packet source", err)
	}
	rebuiltID, ok := PreparedAuthoredAssetPartPalette(rebuilt, "shape")
	restored, exists := f.assets.GetVoxelPalette(rebuiltID)
	if !ok || !exists || rebuiltID == firstID || !reflect.DeepEqual(restored, expected[key]) {
		t.Fatal("cold palette rebuild returned deleted ID or mutated source")
	}
	f.assets.mu.Lock()
	delete(f.assets.voxPalettes, rebuiltID)
	f.assets.mu.Unlock()
	if invalid, err := publishCompiledAssetPacket(packet, f.assets, f.runtime.Loader); err == nil || invalid != nil {
		t.Fatal("stale existing palette key must reject without cold fallback")
	}
	packet.release()
}

func TestC3f4bPalettePendingChargeAndTerminalDrain(t *testing.T) {
	for _, terminal := range []string{"deferred", "cancelled", "stale", "stop", "failure"} {
		t.Run(terminal, func(t *testing.T) {
			path := c3f4bFixture(t)
			f, _ := c3f3Runtime(t, 2, 2)
			for i := range f.runtime.PlacementsByChunk[ChunkCoord{}] {
				f.runtime.PlacementsByChunk[ChunkCoord{}][i].AssetPath = path
			}
			prepared := prepareStreamedChunkLoad(buildStreamedChunkLoadJob(f.runtime, ChunkCoord{}))
			defer prepared.release()
			packet := c3f3Packet(t, prepared, path)
			var minimum, paletteCopies int64
			var paletteSources []*VoxelPaletteAsset
			for _, shape := range packet.shapes {
				minimum += streamedPendingGeometryCharge(shape.source) + shape.registration.charge()
			}
			for key, palette := range packet.palettes {
				paletteSources = append(paletteSources, palette.source)
				minimum += palette.registration.charge() + int64(len(key))
				paletteCopies += palette.registration.charge()
			}
			minimum += runtimeContentGraphCharge(struct {
				Definition *content.AssetDef
				Animations *content.ResolvedAssetAnimations
				Palettes   []*VoxelPaletteAsset
			}{packet.def, packet.animations, paletteSources})
			if charge := streamedPreparedChunkCharge(prepared); charge < minimum {
				t.Fatalf("pending charge omits palette source/copy/key: %d < %d", charge, minimum)
			}
			one := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"one": packet})
			two := streamedCompiledAssetPacketsCharge(map[string]*compiledAssetPacket{"one": packet, "alias": packet})
			if extra := two - one; extra < 0 || extra >= paletteCopies {
				t.Fatal("packet alias charged duplicate palette sources/publication copies")
			}
			switch terminal {
			case "deferred":
				owner := newStreamedPendingPreparedOwner(1)
				blocking, ok := owner.reserve(1)
				if !ok {
					t.Fatal("deferral fixture failed to occupy budget")
				}
				defer blocking.release()
				if result := admitStreamedPreparedChunk(owner, prepared); result.retryCost <= 0 || result.pendingCredit != nil || owner.snapshot().Bytes != 1 {
					t.Fatal("palette packet did not defer at pending boundary")
				}
			case "cancelled":
				cancel := make(chan struct{})
				close(cancel)
				prepared.prepareCancel = cancel
				admitStreamedPreparedChunk(newStreamedPendingPreparedOwner(1<<30), prepared)
			case "stale":
				prepared.Generation = f.runtime.Generation - 1
				f.runtime.PreparedLoads <- admitStreamedPreparedChunk(f.runtime.pendingPrepared, prepared)
				f.commitStage()
			case "stop":
				f.runtime.PreparedLoads <- admitStreamedPreparedChunk(f.runtime.pendingPrepared, prepared)
				if err := StopStreamedLevelRuntime(f.cmd); err != nil {
					t.Fatal(err)
				}
			case "failure":
				prepared.Err = fmt.Errorf("later worker failure")
				admitStreamedPreparedChunk(newStreamedPendingPreparedOwner(1<<30), prepared)
			}
			c3f4bDrained(t, packet)
			packet.release()
			prepared.release()
			if len(f.assets.voxPalettes) != 0 {
				t.Fatal("discarded packet published palette assets")
			}
		})
	}
}

func TestC3f4bGroupNilPublisherAndLateCancellation(t *testing.T) {
	path := c3f4bFixture(t)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	header, _, err := caller.Loader().LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	checks := 0
	packet, err := prepareCompiledAssetPacket(path, caller.Loader(), func() bool { checks++; return false })
	if err != nil || checks < 2 {
		t.Fatal("uncancelled packet calibration failed", err)
	}
	if result, err := publishCompiledAssetPacket(packet, nil, caller.Loader()); err != nil || result == nil {
		t.Fatal("nil server metadata publisher failed", err)
	}
	packet.release()
	c3f4bDrained(t, packet)
	calls := 0
	if cancelled, err := prepareCompiledAssetPacket(path, caller.Loader(), func() bool { calls++; return calls >= checks }); err == nil || cancelled != nil {
		t.Fatal("late cooperative cancellation returned packet")
	}
	after := owner.Stats()
	if after.Entries != before.Entries || after.PinnedBytes != before.PinnedBytes || after.Bytes != before.Bytes {
		t.Fatal("late palette preparation cancellation retained decoded loads or revoked caller pins")
	}
	if accepted, _, err := caller.Loader().LoadCompiledAssetHeader(path); err != nil || accepted != header {
		t.Fatal("caller header pin lost", err)
	}
	group := *header.Asset
	group.Parts = []content.AssetPartDef{header.Asset.Parts[1]}
	group.Skeleton, group.AnimationSetPaths, group.Markers, group.Emitters = nil, nil, nil, nil
	group.DefaultAnimationClipID = ""
	input := filepath.Join(t.TempDir(), "group.gkasset")
	c3cWrite(t, input, &group)
	groupPath := filepath.Join(t.TempDir(), "group.gkassetc")
	if _, err := CompileAuthoredAsset(input, groupPath, nil); err != nil {
		t.Fatal(err)
	}
	groupPacket, err := prepareCompiledAssetPacket(groupPath, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer groupPacket.release()
	if len(groupPacket.palettes) != 0 || len(groupPacket.partPalettes) != 0 {
		t.Fatal("group-only packet prepared palette handles")
	}
}

func TestC3f4bPartialHookFailureStopDrainsUnvisitedPalettes(t *testing.T) {
	firstPath, secondPath := c3f4bFixture(t), c3f4bFixture(t)
	secondHeader, _, err := content.LoadCompiledAssetHeader(secondPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range secondHeader.Asset.Materials {
		secondHeader.Asset.Materials[i].Roughness = .91
	}
	if _, err := content.SaveCompiledAssetHeader(secondPath, secondHeader, nil); err != nil {
		t.Fatal(err)
	}
	f, _ := c3f3Runtime(t, 2, 1)
	f.assets.textures = make(map[AssetId]TextureAsset)
	f.assets.textureKeys = make(map[string]AssetId)
	f.runtime.PlacementsByChunk[ChunkCoord{}][0].AssetPath = firstPath
	f.runtime.PlacementsByChunk[ChunkCoord{}][1].AssetPath = secondPath
	accepted, err := LoadAndPrepareAuthoredAsset(firstPath, f.assets, f.runtime.Loader)
	if err != nil {
		t.Fatal(err)
	}
	acceptedID, ok := PreparedAuthoredAssetPartPalette(accepted, "shape")
	if !ok {
		t.Fatal("independent accepted palette fixture missing")
	}
	var spawned []EntityId
	f.runtime.Config.PlacementHooks = []PostSpawnPlacementHook{func(cmd *Commands, context PostSpawnPlacementContext) {
		shape := context.SpawnResult.EntitiesByAssetID["shape"]
		if !cmd.EntityExists(context.RootEntity) || !cmd.EntityExists(shape) {
			t.Fatal("placement hook did not see its atomic flushed entities")
		}
		cmd.AddComponents(shape, &ColliderComponent{Shape: ShapeBox, HalfExtents: mgl32.Vec3{.5, .5, .5}})
		spawned = append(spawned, context.SpawnResult.Entities...)
		f.runtime.InitErr = fmt.Errorf("partial palette hook failure")
	}}
	s1fPrepared(t, f.streamedRenderHarness, ChunkCoord{}, false)
	prepared := <-f.runtime.PreparedLoads
	defer prepared.release()
	secondKey, err := filepath.Abs(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	unvisited := prepared.compiledAssets[secondKey]
	if prepared.Err != nil || len(prepared.compiledAssets) != 2 || unvisited == nil || len(unvisited.palettes) == 0 {
		t.Fatal("distinct second palette packet fixture failed", prepared.Err)
	}
	f.runtime.PreparedLoads <- prepared
	commitPreparedStreamedChunksSystem(f.cmd, f.assets, f.runtime)
	f.app.FlushCommands()
	if f.runtime.InitErr == nil || len(spawned) == 0 {
		t.Fatal("partial main publication did not reach hook failure")
	}
	for _, palette := range unvisited.palettes {
		if palette.registration.charge() <= 0 {
			t.Fatal("unvisited palette copy drained before pending Stop")
		}
	}
	if err := StopStreamedLevelRuntime(f.cmd); err != nil {
		t.Fatal(err)
	}
	f.app.FlushCommands()
	c3f4bDrained(t, unvisited)
	for _, entity := range spawned {
		if f.cmd.EntityExists(entity) {
			t.Fatal("partial hook failure left owned spawned entities")
		}
	}
	if _, exists := f.assets.GetVoxelPalette(acceptedID); !exists {
		t.Fatal("stream Stop revoked independently accepted ordinary palette")
	}
}
