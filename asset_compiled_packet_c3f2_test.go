package gekko

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func TestC3f2PacketOwnsUniquePostscaleSourcesAndRegistrations(t *testing.T) {
	input, path, authored := c3d3Fixture(t)
	expected := VoxelObjectSnapshotFromXBrickMap(buildAuthoredVoxelShapeMap(authored.Parts[0]))
	if err := os.RemoveAll(filepath.Dir(input)); err != nil {
		t.Fatal(err)
	}
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	cached, _, err := caller.Loader().LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	cachedShape, _, err := caller.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), cached.Shapes[0].Path))
	if err != nil {
		t.Fatal(err)
	}
	before := owner.Stats()
	headerBytes, _ := json.Marshal(cached)
	shapeBytes, _ := json.Marshal(cachedShape)
	packet, err := prepareCompiledAssetPacket(path, caller.Loader(), nil)
	if err != nil || packet == nil {
		t.Fatal("compiled packet preparation failed", err)
	}
	defer packet.release()
	if len(packet.parts) != 2 || len(packet.shapes) != 1 || packet.parts["shape"] != packet.parts["duplicate"] {
		t.Fatal("packet duplicated shared primary sources")
	}
	shape := packet.shapes[packet.parts["shape"]]
	if shape == nil || shape.source == nil || shape.registration == nil || shape.contentID != packet.parts["shape"] {
		t.Fatal("packet shape missing owned geometry/registration")
	}
	if !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(shape.source).Voxels), c3cGeometry(expected.Voxels)) {
		t.Fatal("packet changed signed postscale geometry")
	}
	lattice := authoredVoxelShapeLattice(authored.Parts[0].VoxelResolution)
	baseID, _, err := content.VoxelObjectBaseIdentity(expected, lattice, nil)
	if err != nil || shape.lattice != lattice || shape.baseIdentity != baseID {
		t.Fatal("packet original base/lattice changed", err)
	}
	if shape.registration.charge() <= 0 {
		t.Fatal("packet registration lacks pending storage ownership")
	}
	if count, available := shape.registration.countFor(shape.source); !available || count != len(expected.Voxels) {
		t.Fatal("packet registration not bound to exact source")
	}
	shape.source.SetVoxel(-18, 0, 0, 99)
	registered, _, _, _, taken := shape.registration.take(shape.source)
	if !taken {
		t.Fatal("packet registration unavailable")
	}
	if found, value := registered.XBrickMap.GetVoxel(-18, 0, 0); !found || value == 99 {
		t.Fatal("packet source aliases registration geometry")
	}
	packet.def.Materials[0].Tags[0] = "changed"
	packet.def.Parts[0].Source.VoxelShape.Palette[0].MaterialID = "changed"
	if packet.def.Parts[2].Source.VoxelShape.Palette[0].MaterialID != "mat" {
		t.Fatal("duplicate primary geometry aliased per-part palette metadata")
	}
	packet.animations.Clips[0].Tracks[0].PositionKeys[0].Value[0] = 99
	afterHeader, _ := json.Marshal(cached)
	afterShape, _ := json.Marshal(cachedShape)
	if !bytes.Equal(headerBytes, afterHeader) || !bytes.Equal(shapeBytes, afterShape) {
		t.Fatal("packet mutable metadata/geometry aliases cached definitions")
	}
	after := owner.Stats()
	if after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("packet preparation leaked decoded ownership or released caller pins")
	}
	second, err := prepareCompiledAssetPacket(path, caller.Loader(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.release()
	if second.def.Materials[0].Tags[0] == "changed" || second.animations.Clips[0].Tracks[0].PositionKeys[0].Value[0] == 99 {
		t.Fatal("packet metadata/animation arrays alias another packet")
	}
	secondShape := second.shapes[second.parts["shape"]]
	if !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(secondShape.source).Voxels), c3cGeometry(expected.Voxels)) {
		t.Fatal("packet source mutation changed next preparation")
	}
	alias := second
	registration := secondShape.registration
	second.release()
	alias.release()
	if registration.charge() != 0 {
		t.Fatal("aliased packet release retained registration storage")
	}
	if _, available := registration.countFor(secondShape.source); available {
		t.Fatal("released packet registration still available")
	}
	if shape.registration.charge() != 0 {
		t.Fatal("release of already consumed packet registration restored ownership")
	}
}

func TestC3f2PacketFailureCancellationAndClosedOriginLeaveNoPins(t *testing.T) {
	for _, kind := range []string{"later-frame", "animation", "cancel-before", "cancel-cooperative", "closed-origin"} {
		t.Run(kind, func(t *testing.T) {
			_, path, _ := c3d3Fixture(t)
			owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
			caller := owner.NewScope()
			defer caller.Close()
			header, _, err := caller.Loader().LoadCompiledAssetHeader(path)
			if err != nil {
				t.Fatal(err)
			}
			shapePath := filepath.Join(filepath.Dir(path), header.Shapes[0].Path)
			cachedShape, _, err := caller.Loader().LoadCompiledAssetShape(shapePath)
			if err != nil {
				t.Fatal(err)
			}
			before := owner.Stats()
			selected := path
			origin := caller.Loader()
			var cancel func() bool
			switch kind {
			case "later-frame", "animation":
				bad, _, err := content.LoadCompiledAssetHeader(path, nil)
				if err != nil {
					t.Fatal(err)
				}
				selected = filepath.Join(filepath.Dir(path), "bad.gkassetc")
				if kind == "later-frame" {
					if len(bad.Shapes) < 2 {
						t.Fatal("later-frame fixture needs an earlier valid reference")
					}
					bad.Shapes[len(bad.Shapes)-1].Path = "bad.gkshape"
					if err := os.WriteFile(filepath.Join(filepath.Dir(path), "bad.gkshape"), []byte("invalid"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					bad.Asset.AnimationSetPaths[0] = "dependencies/absent.gkanim"
				}
				if _, err := content.SaveCompiledAssetHeader(selected, bad, nil); err != nil {
					t.Fatal(err)
				}
			case "cancel-before":
				cancel = func() bool { return true }
			case "cancel-cooperative":
				seenUncancelled := false
				cancel = func() bool {
					if !seenUncancelled {
						seenUncancelled = true
						return false
					}
					return true
				}
			case "closed-origin":
				closed := owner.NewScope()
				closed.Close()
				origin = closed.Loader()
			}
			if packet, err := prepareCompiledAssetPacket(selected, origin, cancel); err == nil || packet != nil {
				if packet != nil {
					packet.release()
				}
				t.Fatal("failed/canceled packet returned usable output")
			}
			after := owner.Stats()
			if after.Entries != before.Entries || after.Bytes != before.Bytes || after.PinnedBytes != before.PinnedBytes {
				t.Fatal("failed/canceled packet leaked private pins or revoked caller pins")
			}
			if got, _, err := caller.Loader().LoadCompiledAssetHeader(path); err != nil || got != header {
				t.Fatal("caller accepted header lost", err)
			}
			if got, _, err := caller.Loader().LoadCompiledAssetShape(shapePath); err != nil || got != cachedShape {
				t.Fatal("caller accepted shape lost", err)
			}
		})
	}
}

func TestC3f2PacketWarmCodecClosureAndPrivatePinDrain(t *testing.T) {
	_, path, _ := c3d3Fixture(t)
	codec := c3cCodec(t, voxelcodec.Options{})
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec, MaxCacheBytes: -1})
	caller := owner.NewScope()
	defer caller.Close()
	header, _, err := caller.Loader().LoadCompiledAssetHeader(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range header.Shapes {
		if _, _, err := caller.Loader().LoadCompiledAssetShape(filepath.Join(filepath.Dir(path), ref.Path)); err != nil {
			t.Fatal(err)
		}
	}
	before := owner.Stats()
	if err := codec.Close(); err != nil {
		t.Fatal(err)
	}
	packet, err := prepareCompiledAssetPacket(path, caller.Loader(), nil)
	if err != nil || packet == nil {
		t.Fatal("warm packet depends on closed borrowed codec", err)
	}
	packet.release()
	if after := owner.Stats(); after.Entries != before.Entries || after.PinnedBytes != before.PinnedBytes {
		t.Fatal("warm packet changed caller decoded ownership")
	}
	caller.Close()
	if stats := owner.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatal("packet retained private decoded pins")
	}
	// A cold default owner also leaves no decoded storage behind after packet construction.
	cold := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	ready, err := prepareCompiledAssetPacket(path, cold, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ready.release()
	if stats := cold.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatal("packet construction retained its decode scope")
	}
}

func TestC3f2GroupOnlyPacketHasNoGeometryHandles(t *testing.T) {
	input, path, asset := c3d3Fixture(t)
	asset.Parts = asset.Parts[1:2]
	asset.Markers = nil
	asset.Emitters = nil
	asset.AnimationSetPaths = asset.AnimationSetPaths[:1]
	c3cWrite(t, input, asset)
	if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
		t.Fatal(err)
	}
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	packet, err := prepareCompiledAssetPacket(path, owner, nil)
	if err != nil || packet == nil {
		t.Fatal("group-only compiled packet rejected", err)
	}
	defer packet.release()
	if len(packet.parts) != 0 || len(packet.shapes) != 0 || len(packet.def.Parts) != 1 || packet.def.Parts[0].Source.Kind != content.AssetSourceKindGroup {
		t.Fatal("group-only packet invented geometry")
	}
	if packet.animations == nil || len(packet.animations.Clips) != 1 {
		t.Fatal("group-only packet lost animation metadata")
	}
	if stats := owner.Stats(); stats.Entries != 0 || stats.PinnedBytes != 0 {
		t.Fatal("group-only packet retained decoded pins")
	}
	packet.release()
}

func TestC3f2DistinctShapePacketsKeepSeparateSourcesAndDrainAllHandles(t *testing.T) {
	input, path, asset := c3d3Fixture(t)
	distinct := asset.Parts[0]
	distinct.ID = "distinct"
	distinct.Name = "Distinct"
	distinct.VoxelResolution = 0.5
	distinct.Source.VoxelShape = &content.AssetVoxelShapeDef{Palette: append([]content.AssetVoxelPaletteEntryDef(nil), asset.Parts[0].Source.VoxelShape.Palette...), Voxels: []content.VoxelObjectVoxelDef{{X: -4, Y: 1, Z: 2, Value: 3}}}
	asset.Parts = append(asset.Parts, distinct)
	c3cWrite(t, input, asset)
	if _, err := CompileAuthoredAsset(input, path, nil); err != nil {
		t.Fatal(err)
	}
	packet, err := prepareCompiledAssetPacket(path, NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1}), nil)
	if err != nil || packet == nil {
		t.Fatal("distinct-shape packet rejected", err)
	}
	defer packet.release()
	if len(packet.parts) != 3 || len(packet.shapes) != 2 || packet.parts["shape"] != packet.parts["duplicate"] || packet.parts["distinct"] == packet.parts["shape"] {
		t.Fatal("packet confused duplicate and distinct primary shapes")
	}
	first, second := packet.shapes[packet.parts["shape"]], packet.shapes[packet.parts["distinct"]]
	if first == nil || second == nil || first.source == second.source || first.registration == second.registration {
		t.Fatal("distinct packet sources or registrations shared ownership")
	}
	handles := []*streamedGeometryRegistration{first.registration, second.registration}
	for _, part := range []content.AssetPartDef{asset.Parts[0], distinct} {
		shape := packet.shapes[packet.parts[part.ID]]
		expected := VoxelObjectSnapshotFromXBrickMap(buildAuthoredVoxelShapeMap(part))
		lattice := authoredVoxelShapeLattice(part.VoxelResolution)
		identity, _, err := content.VoxelObjectBaseIdentity(expected, lattice, nil)
		if err != nil || shape.contentID != packet.parts[part.ID] || shape.lattice != lattice || shape.baseIdentity != identity || !reflect.DeepEqual(c3cGeometry(VoxelObjectSnapshotFromXBrickMap(shape.source).Voxels), c3cGeometry(expected.Voxels)) {
			t.Fatal("distinct packet part geometry/base/lattice mismatch", part.ID, err)
		}
		if count, available := shape.registration.countFor(shape.source); !available || count != len(expected.Voxels) {
			t.Fatal("distinct packet registration not bound to own source")
		}
	}
	first.source.SetVoxel(-18, 0, 0, 99)
	if found, _ := second.source.GetVoxel(-18, 0, 0); found {
		t.Fatal("distinct packet source mutation leaked to sibling")
	}
	alias := packet
	packet.release()
	alias.release()
	for _, handle := range handles {
		if handle.charge() != 0 {
			t.Fatal("packet release retained a distinct registration")
		}
	}
}
