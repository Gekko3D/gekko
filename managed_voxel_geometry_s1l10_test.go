package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l10BridgeSector(t *testing.T, obj *core.VoxelObject, expected core.ManagedGeometryInput, coord [3]int) core.ManagedGeometrySectorInput {
	t.Helper()
	input, ok := obj.CaptureManagedGeometrySector(expected, coord)
	if !ok || !input.SameSource(expected) || input.Sector().Coord() != coord || input.Generation() < expected.Generation() {
		t.Fatal("qualified current-sector input unavailable")
	}
	return input
}
func s1l10BridgeUnavailable(t *testing.T, obj *core.VoxelObject, expected core.ManagedGeometryInput, coord [3]int) {
	t.Helper()
	input, ok := obj.CaptureManagedGeometrySector(expected, coord)
	copied, present := input.Sector().CopySector()
	if ok || input.Generation() != 0 || input.SameSource(expected) || input.Sector().Coord() != [3]int{} || input.Sector().Present() || input.Sector().RetainedBytes() != 0 || input.Sector().CopyBytes() != 0 || copied != nil || present {
		t.Fatal("ineligible binding returned sector input")
	}
}
func s1l10BridgeMap(t *testing.T, input core.ManagedGeometrySectorInput) *volume.XBrickMap {
	t.Helper()
	result := volume.NewXBrickMap()
	if sector, ok := input.Sector().CopySector(); ok {
		result.Sectors[input.Sector().Coord()] = sector
	}
	return result
}
func s1l10BridgeFrozen(t *testing.T, input core.ManagedGeometrySectorInput, want *volume.Sector, retained, copied uint64) {
	t.Helper()
	sector, ok := input.Sector().CopySector()
	if ok != (want != nil) || !reflect.DeepEqual(sector, want) || input.Sector().RetainedBytes() != retained || input.Sector().CopyBytes() != copied {
		t.Fatal("historical sector data or charges changed")
	}
}

func TestS1l10BridgeRegisteredEnabledAndUnchangedSync(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "registered", true: "enabled"}[enabled], func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			if enabled {
				p1dEnable(t, f, eid)
				f.app.FlushCommands()
			}
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			expected := s1l6Input(t, obj)
			sector := s1l10BridgeSector(t, obj, expected, [3]int{})
			p1dVoxel(t, s1l10BridgeMap(t, sector), 0, 1)
			f.sync()
			again := s1l6Input(t, obj)
			if !again.SameSource(expected) || again.Generation() != expected.Generation() {
				t.Fatal("unchanged sync replaced attachment")
			}
			if !s1l10BridgeSector(t, obj, expected, [3]int{}).SameSource(again) {
				t.Fatal("callbacks do not share attachment")
			}
			missing := s1l10BridgeSector(t, obj, expected, [3]int{-9, 2, 4})
			if missing.Sector().Present() || missing.Sector().CopyBytes() != 0 || missing.Sector().RetainedBytes() != 0 {
				t.Fatal("missing coordinate not a qualified absence")
			}
		})
	}
	f := newS3cVoxelFixture(t)
	f.model = f.server.RegisterManagedVoxelGeometry(volume.NewXBrickMap(), "s1l10-empty")
	eid := f.add(0)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	if s1l10BridgeSector(t, obj, s1l6Input(t, obj), [3]int{}).Sector().Present() {
		t.Fatal("empty source gained sector")
	}
}

func TestS1l10BridgeOldInputReadsNewFinalizedContentAndPanicPrefix(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	sibling := f.add(20)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	p1dEnable(t, f, sibling)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	expected := s1l6Input(t, obj)
	old := s1l10BridgeSector(t, obj, expected, [3]int{})
	want, _ := old.Sector().CopySector()
	retained, copied := old.Sector().RetainedBytes(), old.Sector().CopyBytes()
	otherObject := f.state.GetVoxelObject(sibling)
	other := s1l6Input(t, otherObject)
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 1})
	if next := s1l10BridgeSector(t, obj, expected, [3]int{}); next.Generation() != old.Generation() {
		t.Fatal("no-op published generation")
	}
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2}, volume.VoxelWrite{X: 64, Value: 4})
	changed := s1l10BridgeSector(t, obj, expected, [3]int{})
	if changed.Generation() <= old.Generation() {
		t.Fatal("old input failed to read current publication without sync")
	}
	p1dVoxel(t, s1l10BridgeMap(t, changed), 0, 2)
	added := s1l10BridgeSector(t, obj, expected, [3]int{2, 0, 0})
	p1dVoxel(t, s1l10BridgeMap(t, added), 64, 4)
	panicValue := &struct{}{}
	func() {
		defer func() {
			if recover() != panicValue {
				t.Fatal("producer panic changed")
			}
		}()
		_ = ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
			s1l10BridgeUnavailable(t, obj, expected, [3]int{})
			yield(volume.VoxelWrite{X: 1, Value: 3})
			s1l10BridgeUnavailable(t, obj, expected, [3]int{})
			panic(panicValue)
		})
	}()
	prefix := s1l10BridgeSector(t, obj, expected, [3]int{})
	if prefix.Generation() <= changed.Generation() {
		t.Fatal("panic prefix not published after finalization")
	}
	p1dVoxel(t, s1l10BridgeMap(t, prefix), 1, 3)
	p1dApply(t, f, eid, volume.VoxelWrite{}, volume.VoxelWrite{X: 1})
	absent := s1l10BridgeSector(t, obj, expected, [3]int{})
	if absent.Sector().Present() || absent.Sector().CopyBytes() != 0 || absent.Sector().RetainedBytes() != 0 {
		t.Fatal("removed coordinate unavailable or charged")
	}
	untouched := s1l10BridgeSector(t, otherObject, other, [3]int{})
	if untouched.Generation() != other.Generation() || untouched.SameSource(expected) {
		t.Fatal("sibling attachment inherited publication")
	}
	p1dVoxel(t, s1l10BridgeMap(t, untouched), 0, 1)
	s1l10BridgeFrozen(t, old, want, retained, copied)
}

func TestS1l10BridgeNormalHaloAndHistoricalSector(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	source := volume.NewXBrickMap()
	source.SetVoxel(31, 0, 0, 1)
	source.SetVoxel(32, 0, 0, 2)
	source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = []byte{4, 5, 6}
	source.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux = []byte{7, 8, 9}
	f.model = f.server.RegisterManagedVoxelGeometry(source, "s1l10-halo")
	eid := f.add(0)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	expected := s1l6Input(t, obj)
	old := s1l10BridgeSector(t, obj, expected, [3]int{1, 0, 0})
	want, _ := old.Sector().CopySector()
	retained, copied := old.Sector().RetainedBytes(), old.Sector().CopyBytes()
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31, Value: 3})
	current := s1l10BridgeSector(t, obj, expected, [3]int{1, 0, 0})
	neighbor, _ := current.Sector().CopySector()
	if neighbor.PackedBricks[0].PrecomputedAux != nil || current.Generation() <= old.Generation() {
		t.Fatal("current read missed finalized normal halo")
	}
	authority, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	if !reflect.DeepEqual(neighbor, authority.Sectors[[3]int{1, 0, 0}]) {
		t.Fatal("current sector differs from publication")
	}
	obj.SetManagedGeometryProducer(obj.XBrickMap, nil)
	s1l10BridgeFrozen(t, old, want, retained, copied)
}

func TestS1l10BridgeInvalidationBeforeAndAfterSync(t *testing.T) {
	for _, kind := range []string{"exposure", "promotion", "derivative", "pending-rebind", "pending-component-removal", "pending-transform-removal", "pending-special-owner", "pending-entity-removal", "asset-release", "direct-rebind"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			f.app.FlushCommands()
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			expected := s1l6Input(t, obj)
			old := s1l10BridgeSector(t, obj, expected, [3]int{})
			want, _ := old.Sector().CopySector()
			retained, copied := old.Sector().RetainedBytes(), old.Sector().CopyBytes()
			vmc := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			switch kind {
			case "exposure":
				raw, _ := f.server.GetVoxelGeometry(vmc.OverrideGeometry)
				raw.XBrickMap.SetVoxel(0, 0, 0, 8)
			case "promotion":
				if _, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid); err != nil {
					t.Fatal(err)
				}
			case "derivative":
				obj.XBrickMap = obj.XBrickMap.Copy()
			case "pending-rebind":
				vmc.OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 4)
				f.cmd.AddComponents(eid, vmc)
			case "pending-component-removal":
				f.cmd.RemoveComponents(eid, VoxelModelComponent{})
			case "pending-transform-removal":
				f.cmd.RemoveComponents(eid, TransformComponent{})
			case "pending-special-owner":
				f.cmd.AddComponents(eid, VoxelBackingComponent{})
			case "pending-entity-removal":
				f.cmd.RemoveEntity(eid)
			case "asset-release":
				f.server.DeleteVoxelGeometry(vmc.OverrideGeometry)
			case "direct-rebind":
				s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 4)
			}
			s1l10BridgeUnavailable(t, obj, expected, [3]int{})
			s1l10BridgeFrozen(t, old, want, retained, copied)
			f.app.FlushCommands()
			f.sync()
			// Sync may create a new qualified attachment; the old identity must still refuse.
			s1l10BridgeUnavailable(t, obj, expected, [3]int{})
			if kind == "derivative" {
				replacement := s1l6Input(t, obj)
				if replacement.SameSource(expected) {
					t.Fatal("repaired derivative reused stale source")
				}
				s1l10BridgeSector(t, obj, replacement, [3]int{})
			}
			s1l10BridgeFrozen(t, old, want, retained, copied)
		})
	}
}

func TestS1l10BridgeRejectsRawUnqualifiedAndSpecialOwners(t *testing.T) {
	for _, kind := range []string{"raw", "unqualified", "terrain", "planet", "retained", "lod", "backing", "streamed", "imported"} {
		t.Run(kind, func(t *testing.T) {
			f := newS3cVoxelFixture(t)
			source := volume.NewXBrickMap()
			source.SetVoxel(0, 0, 0, 1)
			if kind == "unqualified" {
				source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
			}
			if kind == "raw" {
				f.model = f.server.RegisterSharedVoxelGeometry(source, "s1l10-raw")
			} else {
				f.model = f.server.RegisterManagedVoxelGeometry(source, "s1l10-source")
			}
			var extra []any
			switch kind {
			case "lod":
				extra = []any{EntityLODComponent{}}
			case "backing":
				extra = []any{VoxelBackingComponent{}}
			case "streamed":
				extra = []any{StreamedVoxelRenderComponent{}}
			case "imported":
				extra = []any{AuthoredImportedWorldChunkRefComponent{}}
			}
			eid := f.add(0, extra...)
			f.app.FlushCommands()
			vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			switch kind {
			case "terrain":
				vmc.IsTerrainChunk = true
			case "planet":
				vmc.IsPlanetTile = true
			case "retained":
				vmc.RetainRendererGeometry = true
			}
			f.sync()
			s1l6NoInput(t, f.state.GetVoxelObject(eid))
			s1l10BridgeUnavailable(t, f.state.GetVoxelObject(eid), core.ManagedGeometryInput{}, [3]int{})
		})
	}
}
