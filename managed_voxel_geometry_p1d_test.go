package gekko

import (
	"iter"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func p1dWrites(writes ...volume.VoxelWrite) iter.Seq[volume.VoxelWrite] {
	return func(yield func(volume.VoxelWrite) bool) {
		for _, w := range writes {
			if !yield(w) {
				return
			}
		}
	}
}

func p1dFixture(t *testing.T) (*s3cVoxelFixture, EntityId, *volume.XBrickMap) {
	t.Helper()
	f := newS3cVoxelFixture(t)
	f.cmd.AddResources(f.server, f.state)
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 1)
	source.SetVoxel(1, 0, 0, 1)
	f.model = f.server.RegisterManagedVoxelGeometry(source, "p1d-source")
	eid := f.add(0, RigidBodyComponent{Mass: 1})
	f.app.FlushCommands()
	return f, eid, source
}

func p1dEnable(t *testing.T, f *s3cVoxelFixture, eid EntityId) {
	t.Helper()
	if err := EnableManagedVoxelGeometry(f.cmd, f.server, eid); err != nil {
		t.Fatal(err)
	}
}

func p1dApply(t *testing.T, f *s3cVoxelFixture, eid EntityId, writes ...volume.VoxelWrite) {
	t.Helper()
	if err := ApplyManagedVoxelWrites(f.cmd, f.server, eid, p1dWrites(writes...)); err != nil {
		t.Fatal(err)
	}
}

func p1dChanges(t *testing.T, f *s3cVoxelFixture, eid EntityId, want ...volume.VoxelWrite) {
	t.Helper()
	got, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid)
	if !tracked || len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
		t.Fatalf("changes=%v tracked=%v, want %v tracked", got, tracked, want)
	}
}

func p1dVoxel(t *testing.T, xbm *volume.XBrickMap, x int, want uint8) {
	t.Helper()
	if xbm == nil {
		t.Fatal("missing voxel map")
	}
	present, value := xbm.GetVoxel(x, 0, 0)
	if present != (want != 0) || value != want {
		t.Fatalf("voxel %d=(%v,%d), want %d", x, present, value, want)
	}
}

func TestP1dPendingEnableEditsAndSharedSourceIsolation(t *testing.T) {
	f, eid, source := p1dFixture(t)
	sibling := f.add(20)
	f.app.FlushCommands()
	p1dEnable(t, f, eid)
	p1dEnable(t, f, sibling)
	p1dApply(t, f, eid, volume.VoxelWrite{X: 0, Value: 2})
	p1dEnable(t, f, eid)
	p1dApply(t, f, eid, volume.VoxelWrite{X: 1, Value: 3})
	// Edit resolution sees pending ownership, while normal ECS reads stay buffered.
	if s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry != (AssetId{}) {
		t.Fatal("enable bypassed buffered commands")
	}
	p1dChanges(t, f, eid, volume.VoxelWrite{X: 0, Value: 2}, volume.VoxelWrite{X: 1, Value: 3})
	f.app.FlushCommands()
	a := s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry
	b := s3cComponent[VoxelModelComponent](t, f.cmd, sibling).OverrideGeometry
	if a == (AssetId{}) || b == (AssetId{}) || a == b || a == f.model || b == f.model {
		t.Fatal("entities need independent overrides")
	}
	source.SetVoxel(0, 0, 0, 9)
	f.sync()
	p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 2)
	p1dVoxel(t, f.state.GetVoxelObject(sibling).XBrickMap, 0, 1)
	p1dChanges(t, f, sibling)
	p1dChanges(t, f, eid, volume.VoxelWrite{X: 0, Value: 2}, volume.VoxelWrite{X: 1, Value: 3})
}

func TestP1dNoOpRevertAndStableRendererDerivative(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	derivative := obj.XBrickMap
	derivative.ClearDirty()
	revision := derivative.Revision
	if err := ApplyManagedVoxelWrites(f.cmd, f.server, eid, nil); err != nil {
		t.Fatal(err)
	}
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 1})
	f.app.FlushCommands()
	f.sync()
	if derivative.Revision != revision {
		t.Fatal("no-op changed renderer revision")
	}
	p1dChanges(t, f, eid)
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	f.sync()
	if f.state.GetVoxelObject(eid) != obj || obj.XBrickMap != derivative {
		t.Fatal("tracked edit replaced renderer identity")
	}
	p1dVoxel(t, derivative, 0, 2)
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 1})
	f.app.FlushCommands()
	f.sync()
	p1dChanges(t, f, eid)
	p1dVoxel(t, derivative, 0, 1)
}

func TestP1dSealedPhysicsAndPersistenceIgnorePoisonedRenderer(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	f.sync()
	derivative := f.state.GetVoxelObject(eid).XBrickMap
	derivative.SetVoxel(0, 0, 0, 9)
	derivative.SetVoxel(1, 0, 0, 0)
	f.state.markRuntimeEditedVoxelEntity(eid)
	mapForSave, dirty, exists := currentVoxelMapForEntity(f.cmd, eid)
	if !exists || !dirty || mapForSave == derivative {
		t.Fatal("save must select dirty sealed authority before runtime marker")
	}
	p1dVoxel(t, mapForSave, 0, 2)
	p1dVoxel(t, mapForSave, 1, 1)
	_, _, cache := newVoxelPhysicsPrecalcTestHarness()
	VoxPhysicsPreCalcSystem(f.cmd, f.server, f.state, cache)
	f.app.FlushCommands()
	grid := mustPhysicsModel(t, f.cmd, eid).Grid
	if grid == nil {
		t.Fatal("missing collision grid")
	}
	if present, value := grid.GetVoxel(0, 0, 0); !present || value != 2 {
		t.Fatal("physics consumed renderer poison")
	}
	if present, value := grid.GetVoxel(1, 0, 0); !present || value != 1 {
		t.Fatal("physics lost authoritative voxel")
	}
	if f.state.IsEntityEmpty(eid) {
		t.Fatal("nonempty sealed authority reported empty")
	}
	p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2})
}

func TestP1dOrderedProducerPanicPublishesTrackedPrefix(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	panicValue := &struct{ label string }{"producer"}
	func() {
		defer func() {
			if got := recover(); got != panicValue {
				t.Fatalf("panic=%v, want original", got)
			}
		}()
		_ = ApplyManagedVoxelWrites(f.cmd, f.server, eid, func(yield func(volume.VoxelWrite) bool) {
			yield(volume.VoxelWrite{Value: 2})
			p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2})
			yield(volume.VoxelWrite{X: 1, Value: 0})
			panic(panicValue)
		})
	}()
	f.app.FlushCommands()
	f.sync()
	p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2}, volume.VoxelWrite{X: 1})
	current, dirty, _ := currentVoxelMapForEntity(f.cmd, eid)
	if !dirty {
		t.Fatal("panic prefix lost persistence dirty state")
	}
	p1dVoxel(t, current, 1, 0)
	p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 2)
}

func TestP1dMutableAssetExposurePermanentlyDisablesTracking(t *testing.T) {
	for _, path := range []string{"geometry", "model", "resolve", "map"} {
		t.Run(path, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
			f.app.FlushCommands()
			vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			var raw *volume.XBrickMap
			switch path {
			case "geometry":
				asset, _ := f.server.GetVoxelGeometry(vmc.OverrideGeometry)
				raw = asset.XBrickMap
			case "model":
				asset, _ := f.server.GetVoxelModel(vmc.OverrideGeometry)
				raw = asset.XBrickMap
			case "resolve":
				_, asset, _ := ResolveVoxelGeometry(f.server, vmc)
				raw = asset.XBrickMap
			case "map":
				raw, _ = ResolveVoxelGeometryMap(f.server, vmc)
			}
			p1dVoxel(t, raw, 0, 2)
			raw.SetVoxel(0, 0, 0, 7)
			if changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid); tracked || len(changes) != 0 {
				t.Fatal("mutable exposure retained history")
			}
			current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
			p1dVoxel(t, current, 0, 7)
			f.sync()
			p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 7)
		})
	}
}

func TestP1dExplicitRuntimePromotionBindsExactCurrentMap(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	replacement := obj.XBrickMap.Copy()
	replacement.SetVoxel(0, 0, 0, 6)
	obj.XBrickMap = replacement
	raw, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
	if err != nil || raw != replacement {
		t.Fatalf("promotion=%p err=%v, want %p", raw, err, replacement)
	}
	raw.SetVoxel(0, 0, 0, 8)
	f.app.FlushCommands()
	current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	if current != replacement {
		t.Fatal("promotion copied renderer pointer")
	}
	p1dVoxel(t, current, 0, 8)
	if _, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid); tracked {
		t.Fatal("promotion retained tracking")
	}
}

func TestP1dPromotionOrderKeepsCurrentAuthority(t *testing.T) {
	for _, order := range []string{"getter-first", "runtime-first"} {
		t.Run(order, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
			f.app.FlushCommands()
			f.sync()
			id := s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry
			var authority *volume.XBrickMap
			if order == "getter-first" {
				asset, _ := f.server.GetVoxelGeometry(id)
				authority = asset.XBrickMap
				authority.SetVoxel(0, 0, 0, 7)
				// Promote before another renderer sync, while the derivative is stale.
				promoted, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
				if err != nil {
					t.Fatal(err)
				}
				if promoted != authority {
					t.Fatal("runtime promotion replaced already exposed asset authority")
				}
				p1dVoxel(t, promoted, 0, 7)
				authority = promoted
			} else {
				var err error
				authority, err = PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
				if err != nil {
					t.Fatal(err)
				}
				asset, _ := f.server.GetVoxelGeometry(id)
				if asset.XBrickMap != authority {
					t.Fatal("getter copied promoted runtime authority")
				}
			}
			f.app.FlushCommands()
			f.sync()
			again, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
			if err != nil || again != authority {
				t.Fatal("repeat promotion changed authority")
			}
			// Supported public raw payload writes have no revision notification.
			authority.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][0][0] = 8
			current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
			p1dVoxel(t, current, 0, 8)
			_, _, cache := newVoxelPhysicsPrecalcTestHarness()
			VoxPhysicsPreCalcSystem(f.cmd, f.server, f.state, cache)
			f.app.FlushCommands()
			grid := mustPhysicsModel(t, f.cmd, eid).Grid
			if grid == nil {
				t.Fatal("missing promoted collision grid")
			}
			if present, value := grid.GetVoxel(0, 0, 0); !present || value != 8 {
				t.Fatal("collision lost unnotified raw payload")
			}
			if _, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid); tracked {
				t.Fatal("promotion restored tracking")
			}
		})
	}
}

func TestP1dRendererNormalsStayPrivate(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	derivative := f.state.GetVoxelObject(eid).XBrickMap
	derivative.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux = []byte{9, 8, 7}
	current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
	if aux := current.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux; len(aux) != 0 {
		t.Fatal("renderer normal cache leaked into sealed authority")
	}
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	f.sync()
	if derivative.Sectors[[3]int{}].GetBrick(0, 0, 0).PrecomputedAux != nil {
		t.Fatal("tracked edit did not invalidate renderer normal cache")
	}
	p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2})
}

func TestP1dSharedAssetExposureRefreshesExistingAndNewInstances(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	f.sync()
	asset, _ := f.server.GetVoxelGeometry(f.model)
	asset.XBrickMap.Sectors[[3]int{}].GetBrick(0, 0, 0).Payload[0][0][0] = 7
	second := f.add(20)
	f.app.FlushCommands()
	f.sync()
	p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 7)
	p1dVoxel(t, f.state.GetVoxelObject(second).XBrickMap, 0, 7)
}

func TestP1dSphereAndRawHelpersPreserveTrackedEdits(t *testing.T) {
	for _, helper := range []string{"sphere", "ensure", "edit"} {
		t.Run(helper, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
			f.app.FlushCommands()
			f.sync()
			switch helper {
			case "sphere":
				obj := f.state.GetVoxelObject(eid)
				center := obj.Transform.ObjectToWorld().Mul4x1(mgl32.Vec3{1.5, 0.5, 0.5}.Vec4(1)).Vec3()
				f.state.VoxelSphereEdit(eid, center, 0.25, 3)
			case "ensure":
				_, _, raw, err := EnsureEditableVoxelGeometry(f.cmd, f.server, eid)
				if err != nil {
					t.Fatal(err)
				}
				raw.SetVoxel(1, 0, 0, 3)
			case "edit":
				if err := EditVoxelGeometry(f.cmd, f.server, eid, func(raw *volume.XBrickMap) error { raw.SetVoxel(1, 0, 0, 3); return nil }); err != nil {
					t.Fatal(err)
				}
			}
			f.app.FlushCommands()
			current, _, _ := currentVoxelMapForEntity(f.cmd, eid)
			p1dVoxel(t, current, 0, 2)
			p1dVoxel(t, current, 1, 3)
			if helper == "sphere" {
				p1dChanges(t, f, eid, volume.VoxelWrite{Value: 2}, volume.VoxelWrite{X: 1, Value: 3})
			} else if _, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid); tracked {
				t.Fatal("raw helper failed to promote")
			}
		})
	}
}

func TestP1dEligibilityRejectsBeforeMutation(t *testing.T) {
	for _, extra := range []any{EntityLODComponent{}, PlanetBodyComponent{}, AuthoredTerrainChunkRefComponent{}, AuthoredImportedWorldChunkRefComponent{}, VoxelBackingComponent{}, StreamedVoxelRenderComponent{}} {
		t.Run(reflect.TypeOf(extra).Name(), func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			f.cmd.AddComponents(eid, extra)
			f.app.FlushCommands()
			before := *s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			if err := EnableManagedVoxelGeometry(f.cmd, f.server, eid); err == nil {
				t.Fatal("unsupported owner accepted")
			}
			f.app.FlushCommands()
			if got := *s3cComponent[VoxelModelComponent](t, f.cmd, eid); !reflect.DeepEqual(got, before) {
				t.Fatal("rejected enable changed geometry refs")
			}
		})
	}
}

func TestP1dOwnerCleanupOnRefReplacementDeletionAndRemoval(t *testing.T) {
	for _, change := range []string{"reference", "asset", "entity"} {
		t.Run(change, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			p1dEnable(t, f, eid)
			p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
			f.app.FlushCommands()
			f.sync()
			vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
			switch change {
			case "reference":
				vmc.OverrideGeometry = f.server.CreateCubeModel(1, 1, 1, 4)
			case "asset":
				f.server.DeleteVoxelGeometry(vmc.OverrideGeometry)
			case "entity":
				f.cmd.RemoveEntity(eid)
			}
			f.app.FlushCommands()
			f.sync()
			if changes, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid); tracked || len(changes) != 0 {
				t.Fatal("retired association returned stale changes")
			}
			if change == "entity" {
				next := f.add(30)
				f.app.FlushCommands()
				f.sync()
				if _, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, next); tracked {
					t.Fatal("new entity inherited retired owner")
				}
			}
		})
	}
}

func TestP1dPromotionAfterSourceReplacementCannotRestoreOldMap(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	f.app.FlushCommands()
	f.sync()
	old := f.state.GetVoxelObject(eid).XBrickMap
	vmc := s3cComponent[VoxelModelComponent](t, f.cmd, eid)
	selectedSource := volume.NewXBrickMap()
	selectedSource.SetVoxel(0, 0, 0, 4)
	selected := f.server.RegisterSharedVoxelGeometry(selectedSource, "p1d-replacement")
	vmc.OverrideGeometry = selected
	raw, err := PromoteRuntimeVoxelGeometry(f.cmd, f.server, f.state, eid)
	f.app.FlushCommands()
	if s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry != selected {
		t.Fatal("promotion overwrote newly selected source")
	}
	if err == nil {
		if raw == old {
			t.Fatal("promotion rebound stale renderer map")
		}
		p1dVoxel(t, raw, 0, 4)
	}
	f.sync()
	p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 4)
}

func TestP1dRetainedAndPendingIncompatibleOwnersReject(t *testing.T) {
	for _, kind := range []string{"retained", "pending-backing"} {
		t.Run(kind, func(t *testing.T) {
			f, eid, _ := p1dFixture(t)
			if kind == "retained" {
				s3cComponent[VoxelModelComponent](t, f.cmd, eid).RetainRendererGeometry = true
			} else {
				f.cmd.AddComponents(eid, VoxelBackingComponent{})
			}
			if err := EnableManagedVoxelGeometry(f.cmd, f.server, eid); err == nil {
				t.Fatal("incompatible owner accepted")
			}
			f.app.FlushCommands()
			if s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry != (AssetId{}) {
				t.Fatal("rejected enable queued override")
			}
		})
	}
}

func TestP1dGPUFirstSourceCannotBeSilentlySealed(t *testing.T) {
	f := newS3cVoxelFixture(t)
	source := volume.NewXBrickMap()
	source.SetVoxel(0, 0, 0, 1)
	source.GPUEditMode = true
	id := f.server.RegisterManagedVoxelGeometry(source, "gpu-first")
	if id == (AssetId{}) {
		return
	}
	f.model = id
	eid := f.add(0)
	f.app.FlushCommands()
	if err := EnableManagedVoxelGeometry(f.cmd, f.server, eid); err == nil {
		t.Fatal("GPU-first source accepted managed authority")
	}
	f.app.FlushCommands()
	if s3cComponent[VoxelModelComponent](t, f.cmd, eid).OverrideGeometry != (AssetId{}) {
		t.Fatal("GPU rejection changed refs")
	}
	if !source.GPUEditMode {
		t.Fatal("registration mutated source edit mode")
	}
}

func TestP1dDestructionPromotesAndPreservesPriorManagedEdits(t *testing.T) {
	f, eid, _ := p1dFixture(t)
	p1dEnable(t, f, eid)
	p1dApply(t, f, eid, volume.VoxelWrite{Value: 2})
	f.app.FlushCommands()
	f.sync()
	obj := f.state.GetVoxelObject(eid)
	center := obj.Transform.ObjectToWorld().Mul4x1(mgl32.Vec3{1.5, 0.5, 0.5}.Vec4(1)).Vec3()
	if !processDestructionEvents(f.state, []DestructionEvent{{Entity: eid, Center: center, Radius: 0.25, CarveOnly: true, RetainEntity: true}}, f.cmd, f.server) {
		t.Fatal("destruction rejected ordinary managed entity")
	}
	f.app.FlushCommands()
	f.sync()
	current, dirty, exists := currentVoxelMapForEntity(f.cmd, eid)
	if !exists || !dirty {
		t.Fatal("destruction lost retained dirty entity")
	}
	p1dVoxel(t, current, 0, 2)
	p1dVoxel(t, current, 1, 0)
	p1dVoxel(t, f.state.GetVoxelObject(eid).XBrickMap, 0, 2)
	if _, tracked := ManagedVoxelGeometryChanges(f.cmd, f.server, eid); tracked {
		t.Fatal("raw destruction retained sealed tracking")
	}
}
