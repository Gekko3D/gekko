package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s1l6Input(t *testing.T, obj *core.VoxelObject) core.ManagedGeometryInput {
	t.Helper()
	input, ok := obj.CaptureManagedGeometryInput()
	if !ok || !input.SameSource(input) {
		t.Fatal("ordinary qualified sealed binding unavailable")
	}
	return input
}
func s1l6NoInput(t *testing.T, obj *core.VoxelObject) {
	t.Helper()
	input, ok := obj.CaptureManagedGeometryInput()
	if ok || input.SameSource(input) || input.Geometry().Len() != 0 || input.Generation() != 0 {
		t.Fatal("ineligible binding returned a managed input")
	}
}
func s1l6Map(t *testing.T, input core.ManagedGeometryInput) *volume.XBrickMap {
	t.Helper()
	result := volume.NewXBrickMap()
	for i := 0; i < input.Geometry().Len(); i++ {
		key, ok := input.Geometry().Coord(i)
		sector, copied := input.Geometry().CopySector(i)
		if !ok || !copied {
			t.Fatal("capture has unreadable sector")
		}
		result.Sectors[key] = sector
	}
	return result
}
func s1l6Frozen(t *testing.T, input core.ManagedGeometryInput, want *volume.XBrickMap, retained, copied uint64) {
	t.Helper()
	if !reflect.DeepEqual(s1l6Map(t, input).Sectors, want.Sectors) || input.Geometry().RetainedBytes() != retained || input.Geometry().CopyBytes() != copied {
		t.Fatal("historical payload/masks/aux or charges changed")
	}
}

func TestS1l6OrdinaryRegisteredAndEnabledBindings(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "registered", true: "enabled"}[enabled], func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			if enabled {
				p1dEnable(t, f, eid)
				f.app.FlushCommands()
			}
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			old := s1l6Input(t, obj)
			p1dVoxel(t, s1l6Map(t, old), 0, 1)
			f.sync()
			again := s1l6Input(t, obj)
			if !old.SameSource(again) || old.Generation() != again.Generation() {
				t.Fatal("unchanged frame replaced attachment or publication")
			}
			if obj.RenderVoxelMap() != obj.XBrickMap {
				t.Fatal("managed input changed synchronous render selection")
			}
		})
	}
	// A sealed empty source is available and differs from an unavailable source.
	f := newS3cVoxelFixture(t)
	f.model = f.server.RegisterManagedVoxelGeometry(volume.NewXBrickMap(), "s1l6-empty")
	eid := f.add(0)
	f.app.FlushCommands()
	f.sync()
	input := s1l6Input(t, f.state.GetVoxelObject(eid))
	if input.Geometry().Len() != 0 || input.Geometry().CopyBytes() != 0 || input.Geometry().RetainedBytes() != 0 {
		t.Fatal("empty capture has geometry charge")
	}
}

func TestS1l6BridgeRejectsRawUnqualifiedAndSpecialOwners(t *testing.T) {
	for _, kind := range []string{"raw", "unqualified", "terrain", "planet", "retained", "lod", "backing", "streamed", "imported"} {
		t.Run(kind, func(t *testing.T) {
			f := newS3cVoxelFixture(t)
			source := volume.NewXBrickMap()
			source.SetVoxel(0, 0, 0, 1)
			if kind == "unqualified" {
				source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = make([]byte, volume.VoxelAuxRecordBytes+1)
			}
			if kind == "raw" {
				f.model = f.server.RegisterSharedVoxelGeometry(source, "s1l6-raw")
			} else {
				f.model = f.server.RegisterManagedVoxelGeometry(source, "s1l6-source")
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
		})
	}
}

func TestS1l6ManagedEditsNoOpsPanicPrefixAndSiblingIsolation(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	sibling := f.add(20)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	p1dEnable(t, f, sibling)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	other := s1l6Input(t, f.state.GetVoxelObject(sibling))
	old := s1l6Input(t, obj)
	want := s1l6Map(t, old)
	retained, copied := old.Geometry().RetainedBytes(), old.Geometry().CopyBytes()
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 1})
	f.sync()
	if next := s1l6Input(t, obj); next.Generation() != old.Generation() || !next.SameSource(old) {
		t.Fatal("no-op changed publication")
	}
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
	edited := s1l6Input(t, obj)
	if edited.Generation() <= old.Generation() || !old.SameSource(edited) {
		t.Fatal("tracked edit lost generation or source identity")
	}
	p1dVoxel(t, s1l6Map(t, edited), 0, 2)
	panicValue := &struct{}{}
	func() {
		defer func() {
			if recover() != panicValue {
				t.Fatal("producer panic changed")
			}
		}()
		_ = ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
			s1l6NoInput(t, obj)
			yield(volume.VoxelWrite{X: 1, Value: 3})
			s1l6NoInput(t, obj)
			panic(panicValue)
		})
	}()
	prefix := s1l6Input(t, obj)
	if prefix.Generation() <= edited.Generation() || !prefix.SameSource(old) {
		t.Fatal("panic prefix not finalized before capture")
	}
	p1dVoxel(t, s1l6Map(t, prefix), 1, 3)
	unchanged := s1l6Input(t, f.state.GetVoxelObject(sibling))
	if !unchanged.SameSource(other) || unchanged.Generation() != other.Generation() || unchanged.SameSource(prefix) {
		t.Fatal("sibling inherited edited attachment")
	}
	p1dVoxel(t, s1l6Map(t, unchanged), 0, 1)
	mutable := s1l6Map(t, prefix)
	mutable.SetVoxel(0, 0, 0, 9)
	p1dVoxel(t, s1l6Map(t, s1l6Input(t, obj)), 0, 2)
	current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	p1dVoxel(t, current, 0, 2)
	s1l6Frozen(t, old, want, retained, copied)
}

func TestS1l6EqualCountAttachmentReplacement(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	old := s1l6Input(t, obj)
	source := volume.NewXBrickMap()
	source.SetVoxel(64, 0, 0, 7)
	id := f.server.RegisterManagedVoxelGeometry(source, "s1l6-equal-count")
	s3cComponent[VoxelModelComponent](t, f.cmd, eid).VoxelModel = id
	s1l6NoInput(t, obj)
	f.sync()
	next := s1l6Input(t, f.state.GetVoxelObject(eid))
	if next.Generation() != old.Generation() || next.Geometry().Len() != old.Geometry().Len() || next.SameSource(old) || old.SameSource(next) {
		t.Fatal("equal-generation equal-count replacement reused attachment")
	}
	p1dVoxel(t, s1l6Map(t, next), 64, 7)
	p1dVoxel(t, s1l6Map(t, old), 0, 1)
}

func TestS1l6CaptureInvalidationBeforeAndAfterSync(t *testing.T) {
	for _, kind := range []string{"exposure", "promotion", "derivative", "pending-rebind", "pending-component-removal", "pending-transform-removal", "pending-special-owner", "pending-entity-removal", "asset-release", "direct-rebind"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			f.app.FlushCommands()
			f.sync()
			obj := f.state.GetVoxelObject(eid)
			old := s1l6Input(t, obj)
			want := s1l6Map(t, old)
			retained, copied := old.Geometry().RetainedBytes(), old.Geometry().CopyBytes()
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
			s1l6NoInput(t, obj)
			s1l6Frozen(t, old, want, retained, copied)
			f.app.FlushCommands()
			f.sync()
			// A replaced derivative can establish a fresh qualified attachment at sync.
			if kind != "derivative" {
				s1l6NoInput(t, obj)
			}
			s1l6Frozen(t, old, want, retained, copied)
		})
	}
}

func TestS1l6HaloAndSectorReplacementPreserveHistoricalInput(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	source := volume.NewXBrickMap()
	source.SetVoxel(31, 0, 0, 1)
	source.SetVoxel(32, 0, 0, 2)
	source.Sectors[[3]int{}].PackedBricks[0].PrecomputedAux = []byte{4, 5, 6}
	source.Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux = []byte{7, 8, 9}
	f.model = f.server.RegisterManagedVoxelGeometry(source, "s1l6-halo")
	eid := f.add(0)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	old := s1l6Input(t, obj)
	want := s1l6Map(t, old)
	retained, copied := old.Geometry().RetainedBytes(), old.Geometry().CopyBytes()
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31, Value: 3})
	current := s1l6Input(t, obj)
	authority, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	if !reflect.DeepEqual(s1l6Map(t, current).Sectors, authority.Sectors) {
		t.Fatal("capture missed finalized halo geometry")
	}
	if aux := s1l6Map(t, current).Sectors[[3]int{1, 0, 0}].PackedBricks[0].PrecomputedAux; aux != nil {
		t.Fatal("neighbor normal halo was not invalidated")
	}
	p1dApply(t, f, eid, volume.VoxelWrite{X: 31}, volume.VoxelWrite{X: 64, Value: 4})
	next := s1l6Input(t, obj)
	if next.Geometry().Len() != old.Geometry().Len() || !next.SameSource(old) || next.Generation() <= current.Generation() {
		t.Fatal("equal-count topology replacement lost publication")
	}
	p1dVoxel(t, s1l6Map(t, next), 31, 0)
	p1dVoxel(t, s1l6Map(t, next), 64, 4)
	obj.SetManagedGeometryProducer(obj.XBrickMap, nil)
	s1l6NoInput(t, obj)
	f.cmd.RemoveEntity(eid)
	f.app.FlushCommands()
	f.sync()
	s1l6NoInput(t, obj)
	s1l6Frozen(t, old, want, retained, copied)
}
