package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

var s3fPublicationTypes = [4]reflect.Type{
	reflect.TypeOf(TransformComponent{}),
	reflect.TypeOf(LocalTransformComponent{}),
	reflect.TypeOf(Parent{}),
	reflect.TypeOf(s3cRevisionMarker{}),
}

func s3fRevisions(cmd *Commands) [4]uint64 {
	var revisions [4]uint64
	for i, typ := range s3fPublicationTypes {
		revisions[i] = cmd.ComponentRevision(typ)
	}
	return revisions
}

func s3fPublications(t *testing.T, cmd *Commands, before [4]uint64, changed [4]bool) {
	t.Helper()
	after := s3fRevisions(cmd)
	for i, typ := range s3fPublicationTypes {
		if changed[i] && after[i] <= before[i] {
			t.Errorf("%v must publish immediately: before=%v after=%v", typ, before, after)
		}
		if !changed[i] && after[i] != before[i] {
			t.Errorf("%v must retain revision: before=%v after=%v", typ, before, after)
		}
	}
}

func s3fUpdate(t *testing.T, cmd *Commands, changed [4]bool) {
	t.Helper()
	before, structural := s3fRevisions(cmd), cmd.StructuralRevision()
	TransformHierarchySystem(cmd)
	s3fPublications(t, cmd, before, changed)
	if cmd.StructuralRevision() != structural {
		t.Fatal("hierarchy output publication changed structural revision")
	}
}

// Exact destination bits are the public value contract, including signed zero
// and NaN payloads. Do not use the owner's private comparison helper here.
func s3fTRSBits(position mgl32.Vec3, rotation mgl32.Quat, scale mgl32.Vec3) [10]uint32 {
	values := [10]float32{position[0], position[1], position[2], rotation.W, rotation.V[0], rotation.V[1], rotation.V[2], scale[0], scale[1], scale[2]}
	var bits [10]uint32
	for i, value := range values {
		bits[i] = math.Float32bits(value)
	}
	return bits
}

func s3fMirror(t *testing.T, cmd *Commands, root EntityId) {
	t.Helper()
	w := s3cComponent[TransformComponent](t, cmd, root)
	l := s3cComponent[LocalTransformComponent](t, cmd, root)
	if s3fTRSBits(l.Position, l.Rotation, l.Scale) != s3fTRSBits(w.Position, w.Rotation, w.Scale) {
		t.Fatalf("root %d local TRS does not mirror world bits: local=%+v world=%+v", root, *l, *w)
	}
}

func TestS3fChildPropagationPublishesOnlyWorldImmediately(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10), s3cRevisionMarker{})
	child := s3dChild(cmd, root, 2)
	leaf := s3dChild(cmd, child, 3)
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{true})
	s3dPosition(t, cmd, child, 12)
	s3dPosition(t, cmd, leaf, 15)
	for range 2 {
		s3fUpdate(t, cmd, [4]bool{})
	}
	// The source write is unmarked; only derived world outputs are published.
	w := s3cComponent[TransformComponent](t, cmd, root)
	w.Position = mgl32.Vec3{20, 30, 40}
	w.Rotation = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 0, 1})
	w.Scale = mgl32.Vec3{2, 3, 4}
	s3fUpdate(t, cmd, [4]bool{true})
	s3dTRS(t, cmd, child, mgl32.Vec3{20, 34, 40}, w.Scale, w.Rotation)
	s3dTRS(t, cmd, leaf, mgl32.Vec3{20, 40, 40}, w.Scale, w.Rotation)
	s3cComponent[LocalTransformComponent](t, cmd, child).Position[0] = 4
	s3fUpdate(t, cmd, [4]bool{true})
	s3dTRS(t, cmd, child, mgl32.Vec3{20, 38, 40}, w.Scale, w.Rotation)
	s3dTRS(t, cmd, leaf, mgl32.Vec3{20, 44, 40}, w.Scale, w.Rotation)
	s3fUpdate(t, cmd, [4]bool{})
}

func TestS3fRootMirrorsPublishOnlyChangedLocalDestination(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10), s3dLocal(99))
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{false, true})
	s3fMirror(t, cmd, root)
	s3dPosition(t, cmd, root, 10)
	s3fUpdate(t, cmd, [4]bool{})
	for _, field := range []string{"position", "rotation", "scale"} {
		t.Run(field, func(t *testing.T) {
			w := s3cComponent[TransformComponent](t, cmd, root)
			switch field {
			case "position":
				w.Position = mgl32.Vec3{11, 12, 13}
			case "rotation":
				w.Rotation = mgl32.QuatRotate(0.75, mgl32.Vec3{0, 1, 0})
			case "scale":
				w.Scale = mgl32.Vec3{2, 3, 4}
			}
			want := *w
			s3fUpdate(t, cmd, [4]bool{false, true})
			s3fMirror(t, cmd, root)
			if *s3cComponent[TransformComponent](t, cmd, root) != want {
				t.Fatal("mirroring changed the authoritative source")
			}
			s3fUpdate(t, cmd, [4]bool{})
		})
	}
	// The source has not changed since the last mirror. Compare the live local
	// destination, rather than relying on a cached source or output snapshot.
	s3cComponent[LocalTransformComponent](t, cmd, root).Position[0] = -500
	s3fUpdate(t, cmd, [4]bool{false, true})
	s3fMirror(t, cmd, root)
	s3fUpdate(t, cmd, [4]bool{})
}

func TestS3fEqualOutputsDoNotPublishForInitialBuildOrChangedInputs(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3dWorld(10), s3dLocal(10))
	second := cmd.AddEntity(s3dWorld(10))
	child := cmd.AddEntity(s3dWorld(12), s3dLocal(2), Parent{Entity: first})
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, child, 12)
	// An unrelated type migrates the row, but copied transform values do not
	// become hierarchy output publications after committed structural work.
	cmd.AddComponents(child, s3cRevisionMarker{})
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{})
	s3cComponent[Parent](t, cmd, child).Entity = second
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, child, 12)
	// Normalization masks this input-only quaternion change.
	s3cComponent[LocalTransformComponent](t, cmd, child).Rotation.W = 2
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, child, 12)
	// Identity composition masks the changed local position sign bit.
	s3cComponent[LocalTransformComponent](t, cmd, child).Position[1] = math.Float32frombits(0x80000000)
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, child, 12)
	s3fUpdate(t, cmd, [4]bool{})
}

func TestS3fChildDirectWorldRepairPublishesAgainstLiveDestination(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10))
	child := s3dChild(cmd, root, 2)
	leaf := s3dChild(cmd, child, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3cComponent[TransformComponent](t, cmd, child).Position[0] = 999
	s3fUpdate(t, cmd, [4]bool{true})
	s3dPosition(t, cmd, child, 12)
	s3dPosition(t, cmd, leaf, 15)
	s3fUpdate(t, cmd, [4]bool{})
}

func TestS3fInvalidGraphsPublishOnlyActiveOutputsAndRecovery(t *testing.T) {
	for _, kind := range []string{"missing", "self", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			root := cmd.AddEntity(s3dWorld(10))
			a := s3dChild(cmd, root, 1)
			b := s3dChild(cmd, a, 2)
			other := cmd.AddEntity(s3dWorld(100), s3dLocal(100))
			live := s3dChild(cmd, other, 4)
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			frozenA := *s3cComponent[TransformComponent](t, cmd, a)
			frozenB := *s3cComponent[TransformComponent](t, cmd, b)
			edge := s3cComponent[Parent](t, cmd, a)
			switch kind {
			case "missing":
				edge.Entity = EntityId(999999)
			case "self":
				edge.Entity = a
			case "cycle":
				edge.Entity = b
			}
			s3cComponent[LocalTransformComponent](t, cmd, a).Position[0] = 7
			s3fUpdate(t, cmd, [4]bool{})
			if *s3cComponent[TransformComponent](t, cmd, a) != frozenA || *s3cComponent[TransformComponent](t, cmd, b) != frozenB {
				t.Fatal("invalid branch changed retained world values")
			}
			// Isolate each active output type while the invalid branch remains.
			s3cComponent[LocalTransformComponent](t, cmd, other).Position[0] = -99
			s3fUpdate(t, cmd, [4]bool{false, true})
			s3fMirror(t, cmd, other)
			s3cComponent[LocalTransformComponent](t, cmd, live).Position[0] = 8
			s3fUpdate(t, cmd, [4]bool{true})
			s3dPosition(t, cmd, live, 108)
			if *s3cComponent[TransformComponent](t, cmd, a) != frozenA || *s3cComponent[TransformComponent](t, cmd, b) != frozenB {
				t.Fatal("active unrelated branch disturbed invalid world values")
			}
			edge.Entity = root
			s3fUpdate(t, cmd, [4]bool{true})
			s3dPosition(t, cmd, a, 17)
			s3dPosition(t, cmd, b, 19)
			s3fUpdate(t, cmd, [4]bool{})
		})
	}
}

func TestS3fIncompleteRolesReadSourcesWithoutPublication(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	parentOnly := cmd.AddEntity(Parent{Entity: EntityId(999999)})
	noWorld := cmd.AddEntity(s3dLocal(5), Parent{Entity: parentOnly})
	source := cmd.AddEntity(s3dWorld(70), Parent{Entity: parentOnly})
	blocked := s3dChild(cmd, parentOnly, 1)
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, blocked, -100)
	s3cComponent[TransformComponent](t, cmd, source).Position[0] = 80
	s3cComponent[LocalTransformComponent](t, cmd, noWorld).Position[0] = 6
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, source, 80)
	s3dPosition(t, cmd, blocked, -100)
	if s3cComponent[LocalTransformComponent](t, cmd, noWorld).Position[0] != 6 {
		t.Fatal("incomplete local role was overwritten")
	}
}

func TestS3fExactOutputBitsAndPivotExclusion(t *testing.T) {
	t.Run("root signed zero and identical NaN payload", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		root := cmd.AddEntity(s3dWorld(0), s3dLocal(0))
		app.FlushCommands()
		s3fUpdate(t, cmd, [4]bool{})
		w := s3cComponent[TransformComponent](t, cmd, root)
		w.Position[0] = math.Float32frombits(0x80000000)
		s3fUpdate(t, cmd, [4]bool{false, true})
		s3fMirror(t, cmd, root)
		s3fUpdate(t, cmd, [4]bool{})
		w.Position[1] = math.Float32frombits(0x7fc01234)
		s3fUpdate(t, cmd, [4]bool{false, true})
		s3fMirror(t, cmd, root)
		for range 2 {
			s3fUpdate(t, cmd, [4]bool{})
		}
		w.Position[1] = math.Float32frombits(0x7fc01235)
		s3fUpdate(t, cmd, [4]bool{false, true})
		s3fMirror(t, cmd, root)
		w.Pivot = mgl32.Vec3{7, 8, 9}
		s3fUpdate(t, cmd, [4]bool{})
		if w.Pivot != (mgl32.Vec3{7, 8, 9}) {
			t.Fatal("root mirror changed render pivot")
		}
	})
	t.Run("child signed zero output and pivot", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		root := cmd.AddEntity(s3dWorld(10))
		child := s3dChild(cmd, root, 2)
		app.FlushCommands()
		TransformHierarchySystem(cmd)
		local := s3cComponent[LocalTransformComponent](t, cmd, child)
		local.Scale[0] = 0
		s3fUpdate(t, cmd, [4]bool{true})
		local.Scale[0] = math.Float32frombits(0x80000000)
		s3fUpdate(t, cmd, [4]bool{true})
		if math.Float32bits(s3cComponent[TransformComponent](t, cmd, child).Scale[0]) != 0x80000000 {
			t.Fatal("composition lost the negative zero output bit")
		}
		s3cComponent[TransformComponent](t, cmd, root).Pivot = mgl32.Vec3{7, 8, 9}
		s3cComponent[TransformComponent](t, cmd, child).Pivot = mgl32.Vec3{4, 5, 6}
		s3fUpdate(t, cmd, [4]bool{})
		local.Position[0] = 3
		s3fUpdate(t, cmd, [4]bool{true})
		if s3cComponent[TransformComponent](t, cmd, child).Pivot != (mgl32.Vec3{4, 5, 6}) {
			t.Fatal("changed child output overwrote render pivot")
		}
		s3fUpdate(t, cmd, [4]bool{})
	})
	t.Run("child NaN destination survives recomposition and repair", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		root := cmd.AddEntity(s3dWorld(10))
		child := s3dChild(cmd, root, 2)
		app.FlushCommands()
		TransformHierarchySystem(cmd)
		s3cComponent[LocalTransformComponent](t, cmd, child).Scale[0] = math.Float32frombits(0x7fc01234)
		s3fUpdate(t, cmd, [4]bool{true})
		world := s3cComponent[TransformComponent](t, cmd, child)
		if !math.IsNaN(float64(world.Scale[0])) {
			t.Fatal("child composition did not produce the NaN scale")
		}
		want := s3fTRSBits(world.Position, world.Rotation, world.Scale)
		// Structural rebuilding forces actual composition. Snapshot revisions
		// after flush, and compare the arithmetic's actual public output bits.
		cmd.AddComponents(child, s3cRevisionMarker{})
		app.FlushCommands()
		s3fUpdate(t, cmd, [4]bool{})
		world = s3cComponent[TransformComponent](t, cmd, child)
		if s3fTRSBits(world.Position, world.Rotation, world.Scale) != want {
			t.Fatal("equal-input recomposition changed the child NaN output bits")
		}
		world.Scale[0] = 1
		s3fUpdate(t, cmd, [4]bool{true})
		world = s3cComponent[TransformComponent](t, cmd, child)
		if s3fTRSBits(world.Position, world.Rotation, world.Scale) != want {
			t.Fatal("direct child-world repair did not restore actual NaN output bits")
		}
		s3fUpdate(t, cmd, [4]bool{})
	})
}

func TestS3fPendingCommandsStaySeparateFromOutputPublications(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10))
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	child := s3dChild(cmd, root, 2)
	s3fUpdate(t, cmd, [4]bool{})
	if cmd.EntityExists(child) {
		t.Fatal("hierarchy flushed pending child insertion")
	}
	app.FlushCommands()
	// The update helper snapshots AFTER flush so structural publications cannot
	// satisfy the hierarchy's changed-output obligation.
	s3fUpdate(t, cmd, [4]bool{true})
	s3dPosition(t, cmd, child, 12)
	cmd.AddComponents(child, s3dLocal(3), s3dWorld(-500))
	s3fUpdate(t, cmd, [4]bool{})
	s3dPosition(t, cmd, child, 12)
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{true})
	s3dPosition(t, cmd, child, 13)
	cmd.RemoveComponents(child, Parent{})
	s3fUpdate(t, cmd, [4]bool{})
	if cmd.GetComponent(child, s3fPublicationTypes[2]) == nil || s3cComponent[LocalTransformComponent](t, cmd, child).Position[0] != 3 {
		t.Fatal("pending Parent removal changed committed child role")
	}
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{false, true})
	s3fMirror(t, cmd, child)
	cmd.RemoveEntity(child)
	s3fUpdate(t, cmd, [4]bool{})
	if !cmd.EntityExists(child) {
		t.Fatal("hierarchy flushed pending entity removal")
	}
	app.FlushCommands()
	s3fUpdate(t, cmd, [4]bool{})
	if cmd.EntityExists(child) {
		t.Fatal("committed entity removal remained visible")
	}
}
