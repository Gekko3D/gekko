package gekko

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type s3gUnrelated struct{ Value int }

var s3gTypes = [4]reflect.Type{reflect.TypeOf(TransformComponent{}), reflect.TypeOf(LocalTransformComponent{}), reflect.TypeOf(Parent{}), reflect.TypeOf(s3gUnrelated{})}

func s3gRevisions(cmd *Commands) [4]uint64 {
	var result [4]uint64
	for i, typ := range s3gTypes {
		result[i] = cmd.ComponentRevision(typ)
	}
	return result
}

// -1 leaves hierarchy-owned output unconstrained when floating arithmetic can
// repair it. 0 requires unchanged publication; 1 requires an advance.
func s3gPublications(t *testing.T, cmd *Commands, before [4]uint64, structural uint64, want [4]int) {
	t.Helper()
	after := s3gRevisions(cmd)
	for i, change := range want {
		if change == 0 && after[i] != before[i] {
			t.Errorf("%v revision changed: before=%d after=%d", s3gTypes[i], before[i], after[i])
		}
		if change == 1 && after[i] <= before[i] {
			t.Errorf("%v changed committed value without publication: before=%d after=%d", s3gTypes[i], before[i], after[i])
		}
	}
	if cmd.StructuralRevision() != structural {
		t.Error("helper changed committed structural revision")
	}
}

func s3gBits(position mgl32.Vec3, rotation mgl32.Quat, scale mgl32.Vec3) [10]uint32 {
	values := [10]float32{position[0], position[1], position[2], rotation.W, rotation.V[0], rotation.V[1], rotation.V[2], scale[0], scale[1], scale[2]}
	var result [10]uint32
	for i, value := range values {
		result[i] = math.Float32bits(value)
	}
	return result
}

func s3gLocal(t *testing.T, cmd *Commands, id EntityId, want LocalTransformComponent) {
	t.Helper()
	got := s3cComponent[LocalTransformComponent](t, cmd, id)
	if math.Abs(float64(got.Rotation.Dot(got.Rotation)-1)) > 1e-4 {
		t.Fatalf("non-unit local rotation: %v", got.Rotation)
	}
	if got.Position.Sub(want.Position).Len() > 1e-4 || got.Scale.Sub(want.Scale).Len() > 1e-4 || math.Abs(float64(got.Rotation.Dot(want.Rotation))) < 1-1e-4 {
		t.Fatalf("local=%+v want=%+v", *got, want)
	}
	for _, bits := range s3gBits(got.Position, got.Rotation, got.Scale) {
		if math.IsNaN(float64(math.Float32frombits(bits))) || math.IsInf(float64(math.Float32frombits(bits)), 0) {
			t.Fatalf("nonfinite local=%+v", *got)
		}
	}
}

func TestS3gWorldSetterPublishesLocalBeforeHierarchy(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	parentWorld := TransformComponent{Position: mgl32.Vec3{10, 20, 30}, Rotation: mgl32.Quat{W: 0, V: mgl32.Vec3{0, 0, 1}}, Scale: mgl32.Vec3{2, 3, 4}, Pivot: mgl32.Vec3{8, 9, 10}}
	parent := cmd.AddEntity(parentWorld, s3gUnrelated{7})
	child := s3dChild(cmd, parent, 1)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	oldWorld := *s3cComponent[TransformComponent](t, cmd, child)
	desired := TransformComponent{Position: mgl32.Vec3{6, 11, 46}, Rotation: parentWorld.Rotation, Scale: mgl32.Vec3{4, 9, 16}, Pivot: mgl32.Vec3{99, 99, 99}}
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !setEntityWorldTransform(cmd, child, desired) {
		t.Fatal("valid world conversion rejected")
	}
	s3gLocal(t, cmd, child, LocalTransformComponent{Position: mgl32.Vec3{2, 3, 4}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 3, 4}})
	if *s3cComponent[TransformComponent](t, cmd, child) != oldWorld || *s3cComponent[TransformComponent](t, cmd, parent) != parentWorld {
		t.Fatal("setter changed world source or destination")
	}
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
	for _, equivalent := range []TransformComponent{desired, {Position: desired.Position, Rotation: mgl32.Quat{W: 0, V: mgl32.Vec3{0, 0, 2}}, Scale: desired.Scale}} {
		before = s3gRevisions(cmd)
		if !setEntityWorldTransform(cmd, child, equivalent) {
			t.Fatal("equivalent conversion rejected")
		}
		s3gPublications(t, cmd, before, structural, [4]int{})
	}
	before = s3gRevisions(cmd)
	TransformHierarchySystem(cmd)
	s3gPublications(t, cmd, before, structural, [4]int{1, 0, 0, 0})
	s3dTRS(t, cmd, child, desired.Position, desired.Scale, desired.Rotation)
	if s3cComponent[TransformComponent](t, cmd, child).Pivot != oldWorld.Pivot {
		t.Fatal("setter/hierarchy changed pivot")
	}
}

func TestS3gUnparentedSetterUsesActualLocalBitsAndPreservesWorld(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	world := s3dWorld(70)
	world.Pivot = mgl32.Vec3{7, 8, 9}
	root := cmd.AddEntity(world, s3dLocal(70), s3gUnrelated{7})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	structural := cmd.StructuralRevision()
	baseline := TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 3, 4}}
	before := s3gRevisions(cmd)
	if !setEntityWorldTransform(cmd, root, baseline) {
		t.Fatal("unparented setter rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
	// Exercise every stored TRS channel. Assignment preserves exact bits here;
	// no normalization or arithmetic is part of the unparented contract.
	for channel := 0; channel < 10; channel++ {
		t.Run(fmt.Sprintf("channel_%d", channel), func(t *testing.T) {
			desired := baseline
			fields := []*float32{&desired.Position[0], &desired.Position[1], &desired.Position[2], &desired.Rotation.W, &desired.Rotation.V[0], &desired.Rotation.V[1], &desired.Rotation.V[2], &desired.Scale[0], &desired.Scale[1], &desired.Scale[2]}
			*fields[channel] = 0
			setEntityWorldTransform(cmd, root, desired)
			for _, bits := range []uint32{0x80000000, 0x7fc01234, 0x7fc01234, 0x7fc01235} {
				old := *s3cComponent[LocalTransformComponent](t, cmd, root)
				*fields[channel] = math.Float32frombits(bits)
				before = s3gRevisions(cmd)
				if !setEntityWorldTransform(cmd, root, desired) {
					t.Fatal("bit-preserving assignment rejected")
				}
				local := s3cComponent[LocalTransformComponent](t, cmd, root)
				wantBits := s3gBits(desired.Position, desired.Rotation, desired.Scale)
				if s3gBits(local.Position, local.Rotation, local.Scale) != wantBits {
					t.Fatal("assignment lost destination TRS bits")
				}
				changed := 0
				if s3gBits(old.Position, old.Rotation, old.Scale) != wantBits {
					changed = 1
				}
				s3gPublications(t, cmd, before, structural, [4]int{0, changed, 0, 0})
			}
			desired.Pivot = mgl32.Vec3{99, 98, 97}
			before = s3gRevisions(cmd)
			setEntityWorldTransform(cmd, root, desired)
			s3gPublications(t, cmd, before, structural, [4]int{})
			if *s3cComponent[TransformComponent](t, cmd, root) != world {
				t.Fatal("unparented setter changed authoritative world or pivot")
			}
		})
	}
	// A silent external local edit must be compared against the live destination.
	setEntityWorldTransform(cmd, root, baseline)
	s3cComponent[LocalTransformComponent](t, cmd, root).Position[0] = 999
	before = s3gRevisions(cmd)
	setEntityWorldTransform(cmd, root, baseline)
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
}

func TestS3gRotationWrapperPublishesOnlyChangedLocal(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	parent := cmd.AddEntity(s3dWorld(10))
	child := s3dChild(cmd, parent, 2)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	world := *s3cComponent[TransformComponent](t, cmd, child)
	rotation := mgl32.Quat{W: 0, V: mgl32.Vec3{0, 1, 0}}
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !setEntityWorldRotation(cmd, child, rotation) {
		t.Fatal("rotation rejected")
	}
	s3gLocal(t, cmd, child, LocalTransformComponent{Position: mgl32.Vec3{2, 0, 0}, Rotation: rotation, Scale: mgl32.Vec3{1, 1, 1}})
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
	if *s3cComponent[TransformComponent](t, cmd, child) != world {
		t.Fatal("rotation wrapper propagated world prematurely")
	}
	before = s3gRevisions(cmd)
	if !setEntityWorldRotation(cmd, child, mgl32.Quat{W: 0, V: mgl32.Vec3{0, 2, 0}}) {
		t.Fatal("equivalent rotation rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{})
}

func TestS3gRejectedSettersLeaveCommittedValuesAndRevisions(t *testing.T) {
	for _, kind := range []string{"missing local", "missing parent world", "zero x", "zero y", "zero z", "missing entity", "rotation missing world"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			parentWorld := s3dWorld(10)
			if kind == "zero x" {
				parentWorld.Scale[0] = 0
			}
			if kind == "zero y" {
				parentWorld.Scale[1] = 0
			}
			if kind == "zero z" {
				parentWorld.Scale[2] = 0
			}
			parent := cmd.AddEntity(parentWorld)
			if kind == "missing parent world" {
				parent = cmd.AddEntity(s3gUnrelated{})
			}
			child := s3dChild(cmd, parent, 2)
			if kind == "missing local" {
				child = cmd.AddEntity(s3dWorld(12), Parent{Entity: parent})
			}
			if kind == "rotation missing world" {
				child = cmd.AddEntity(s3dLocal(2), Parent{Entity: parent})
			}
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
			local, _ := cmd.GetComponent(child, s3gTypes[1]).(*LocalTransformComponent)
			var saved LocalTransformComponent
			if local != nil {
				saved = *local
			}
			target := child
			if kind == "missing entity" {
				target = EntityId(999999)
			}
			ok := false
			if kind == "rotation missing world" {
				ok = setEntityWorldRotation(cmd, target, mgl32.QuatIdent())
			} else {
				ok = setEntityWorldTransform(cmd, target, s3dWorld(100))
			}
			if ok {
				t.Fatal("invalid setter accepted")
			}
			if local != nil && *local != saved {
				t.Fatal("failed setter changed local")
			}
			s3gPublications(t, cmd, before, structural, [4]int{})
		})
	}
}

func TestS3gReparentPublishesCommittedParentAndLocal(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3dWorld(10), s3gUnrelated{7})
	second := cmd.AddEntity(TransformComponent{Position: mgl32.Vec3{-3, 1, 4}, Rotation: mgl32.QuatRotate(0.7, mgl32.Vec3{0, 1, 0}), Scale: mgl32.Vec3{2, 3, 4}})
	child := s3dChild(cmd, first, 2)
	leaf := s3dChild(cmd, child, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	oldWorld := *s3cComponent[TransformComponent](t, cmd, child)
	oldLeaf := *s3cComponent[TransformComponent](t, cmd, leaf)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !ReparentPreservingWorldTransform(cmd, child, second) {
		t.Fatal("reparent rejected")
	}
	if s3cComponent[Parent](t, cmd, child).Entity != second {
		t.Fatal("new Parent not committed")
	}
	s3gPublications(t, cmd, before, structural, [4]int{-1, 1, 1, 0})
	s3dTRS(t, cmd, child, oldWorld.Position, oldWorld.Scale, oldWorld.Rotation)
	s3dTRS(t, cmd, leaf, oldLeaf.Position, oldLeaf.Scale, oldLeaf.Rotation)
}

func TestS3gReparentEqualLocalPublishesOnlyChangedParent(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3dWorld(10))
	second := cmd.AddEntity(s3dWorld(10))
	child := s3dChild(cmd, first, 2)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !ReparentPreservingWorldTransform(cmd, child, second) {
		t.Fatal("equal-pose reparent rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{0, 0, 1, 0})
	if s3cComponent[Parent](t, cmd, child).Entity != second {
		t.Fatal("equal-pose parent not changed")
	}
	before = s3gRevisions(cmd)
	if !ReparentPreservingWorldTransform(cmd, child, second) {
		t.Fatal("current-parent reparent rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{})
}

func TestS3gFailedReparentRollsBackWithoutAttemptPublication(t *testing.T) {
	for _, kind := range []string{"missing local", "missing world", "missing parent", "missing parent world", "zero x", "zero y", "zero z", "zero entity", "zero target", "self", "missing entity", "nil commands"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			first := cmd.AddEntity(s3dWorld(10))
			nextWorld := s3dWorld(20)
			if kind == "zero x" {
				nextWorld.Scale[0] = 0
			}
			if kind == "zero y" {
				nextWorld.Scale[1] = 0
			}
			if kind == "zero z" {
				nextWorld.Scale[2] = 0
			}
			second := cmd.AddEntity(nextWorld)
			if kind == "missing parent world" {
				second = cmd.AddEntity(s3gUnrelated{})
			}
			child := s3dChild(cmd, first, 2)
			if kind == "missing local" {
				child = cmd.AddEntity(s3dWorld(12), Parent{Entity: first})
			}
			if kind == "missing world" {
				child = cmd.AddEntity(s3dLocal(2), Parent{Entity: first})
			}
			if kind == "missing parent" {
				child = cmd.AddEntity(s3dWorld(12), s3dLocal(12))
			}
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
			parent, _ := cmd.GetComponent(child, s3gTypes[2]).(*Parent)
			local, _ := cmd.GetComponent(child, s3gTypes[1]).(*LocalTransformComponent)
			var savedParent Parent
			var savedLocal LocalTransformComponent
			if parent != nil {
				savedParent = *parent
			}
			if local != nil {
				savedLocal = *local
			}
			targetCmd, entity, target := cmd, child, second
			if kind == "zero entity" {
				entity = 0
			}
			if kind == "zero target" {
				target = 0
			}
			if kind == "self" {
				target = child
			}
			if kind == "missing entity" {
				entity = EntityId(999999)
			}
			if kind == "nil commands" {
				targetCmd = nil
			}
			if ReparentPreservingWorldTransform(targetCmd, entity, target) {
				t.Fatal("invalid reparent accepted")
			}
			if parent != nil && *parent != savedParent {
				t.Fatal("tentative Parent leaked")
			}
			if local != nil && *local != savedLocal {
				t.Fatal("failed conversion changed local")
			}
			s3gPublications(t, cmd, before, structural, [4]int{})
		})
	}
}

func TestS3gFailedReparentStillPublishesLegitimateHierarchyOutputs(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3dWorld(10))
	invalid := cmd.AddEntity(TransformComponent{Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{0, 1, 1}})
	child := s3dChild(cmd, first, 2)
	other := cmd.AddEntity(s3dWorld(100), s3dLocal(100))
	leaf := s3dChild(cmd, other, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	savedLocal := *s3cComponent[LocalTransformComponent](t, cmd, child)
	// Unmarked source changes belong to their writer. The call's initial hierarchy
	// evaluation is nevertheless obligated to publish its changed derived output.
	s3cComponent[LocalTransformComponent](t, cmd, leaf).Position[0] = 4
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if ReparentPreservingWorldTransform(cmd, child, invalid) {
		t.Fatal("zero-scale reparent accepted")
	}
	s3gPublications(t, cmd, before, structural, [4]int{1, 0, 0, 0})
	s3dPosition(t, cmd, leaf, 104)
	if s3cComponent[Parent](t, cmd, child).Entity != first || *s3cComponent[LocalTransformComponent](t, cmd, child) != savedLocal {
		t.Fatal("failed reparent leaked attempted writes")
	}
}

func TestS3gHelpersKeepQueuedMutationsAtExplicitFlush(t *testing.T) {
	for _, operation := range []string{"setter", "reparent", "restore"} {
		t.Run(operation, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			first := cmd.AddEntity(s3dWorld(10))
			second := cmd.AddEntity(s3dWorld(20))
			child := cmd.AddEntity(s3dWorld(12), s3dLocal(2), Parent{Entity: first}, AuthoredAssetAttachmentComponent{ParentMarker: second, MountTransform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Position: content.Vec3{4, 0, 0}, Scale: content.Vec3{1, 1, 1}}})
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			doomed := cmd.AddEntity(s3gUnrelated{9})
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			// Snapshot only after unrelated insertion commits, then queue all mutation
			// classes. Destination replacement must win only at the explicit flush.
			pending := cmd.AddEntity(s3dWorld(500), s3dLocal(1))
			cmd.RemoveEntity(doomed)
			cmd.RemoveComponents(child, Parent{})
			cmd.AddComponents(child, s3dLocal(77), s3gUnrelated{11})
			before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
			want := [4]int{0, 1, 0, 0}
			var ok bool
			switch operation {
			case "setter":
				ok = setEntityWorldTransform(cmd, child, s3dWorld(15))
			case "reparent":
				ok = ReparentPreservingWorldTransform(cmd, child, second)
				want = [4]int{0, 1, 1, 0}
			case "restore":
				ok = RestoreAuthoredAssetAttachmentMount(cmd, child)
				want = [4]int{1, 1, 1, 0}
			}
			if !ok {
				t.Fatal("committed helper rejected with commands pending")
			}
			s3gPublications(t, cmd, before, structural, want)
			if cmd.EntityExists(pending) || !cmd.EntityExists(doomed) || cmd.GetComponent(child, s3gTypes[2]) == nil || cmd.GetComponent(child, s3gTypes[3]) != nil {
				t.Fatal("helper flushed pending structural work")
			}
			local := s3cComponent[LocalTransformComponent](t, cmd, child)
			wantX := float32(5)
			if operation == "reparent" {
				wantX = -8
			}
			if operation == "restore" {
				wantX = 4
			}
			if local.Position[0] != wantX {
				t.Fatalf("helper used pending destination: local=%+v", *local)
			}
			app.FlushCommands()
			if !cmd.EntityExists(pending) || cmd.EntityExists(doomed) || cmd.GetComponent(child, s3gTypes[2]) != nil || s3cComponent[LocalTransformComponent](t, cmd, child).Position[0] != 77 || s3cComponent[s3gUnrelated](t, cmd, child).Value != 11 {
				t.Fatal("queued commands lost their explicit flush boundary")
			}
		})
	}
}

func TestS3gRestoreMountPublishesOwnWritesAndPropagatesDescendants(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3dWorld(10))
	marker := cmd.AddEntity(s3dWorld(20))
	attachment := AuthoredAssetAttachmentComponent{ParentMarker: marker, MountTransform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Position: content.Vec3{4, 0, 0}, Scale: content.Vec3{2, 3, 4}}}
	root := cmd.AddEntity(s3dWorld(12), s3dLocal(2), Parent{Entity: first}, attachment, s3gUnrelated{7})
	leaf := s3dChild(cmd, root, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	// The mounted-transform evaluation itself runs hierarchy first. Its repair of
	// this unrelated world is a separate legitimate World publication.
	other := cmd.AddEntity(s3dWorld(100))
	otherChild := s3dChild(cmd, other, 1)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3cComponent[TransformComponent](t, cmd, otherChild).Position[0] = 999
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !RestoreAuthoredAssetAttachmentMount(cmd, root) {
		t.Fatal("restore rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{1, 1, 1, 0})
	if s3cComponent[Parent](t, cmd, root).Entity != marker {
		t.Fatal("authored Parent not restored")
	}
	s3gLocal(t, cmd, root, LocalTransformComponent{Position: mgl32.Vec3{4, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 3, 4}})
	s3dTRS(t, cmd, root, mgl32.Vec3{24, 0, 0}, mgl32.Vec3{2, 3, 4}, mgl32.QuatIdent())
	s3dTRS(t, cmd, leaf, mgl32.Vec3{30, 0, 0}, mgl32.Vec3{2, 3, 4}, mgl32.QuatIdent())
	s3dPosition(t, cmd, otherChild, 101)
	before = s3gRevisions(cmd)
	if !RestoreAuthoredAssetAttachmentMount(cmd, root) {
		t.Fatal("idempotent restore rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{})
}

func TestS3gRestoreMountPublishesOnlyActualDestinations(t *testing.T) {
	for _, kind := range []string{"Parent only", "local only", "target lacks World"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			first := cmd.AddEntity(s3dWorld(10))
			marker := cmd.AddEntity(s3dWorld(10))
			if kind == "target lacks World" {
				marker = cmd.AddEntity(s3gUnrelated{7})
			}
			parent := first
			x := float32(4)
			if kind == "local only" {
				parent = marker
				x = 2
			}
			root := cmd.AddEntity(s3dWorld(14), s3dLocal(x), Parent{Entity: parent}, AuthoredAssetAttachmentComponent{ParentMarker: marker, MountTransform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Position: content.Vec3{4, 0, 0}, Scale: content.Vec3{1, 1, 1}}})
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			oldWorld := *s3cComponent[TransformComponent](t, cmd, root)
			before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
			if !RestoreAuthoredAssetAttachmentMount(cmd, root) {
				t.Fatal("existing restore semantics rejected target")
			}
			want := [4]int{0, 0, 1, 0}
			if kind == "local only" {
				want = [4]int{1, 1, 0, 0}
			}
			s3gPublications(t, cmd, before, structural, want)
			if s3cComponent[Parent](t, cmd, root).Entity != marker || s3cComponent[LocalTransformComponent](t, cmd, root).Position[0] != 4 {
				t.Fatal("restore did not commit mount")
			}
			if kind == "target lacks World" && *s3cComponent[TransformComponent](t, cmd, root) != oldWorld {
				t.Fatal("invalid hierarchy target changed retained World")
			}
		})
	}
}

func TestS3gRejectedRestoreDoesNotPublishAttempt(t *testing.T) {
	for _, kind := range []string{"nil commands", "zero root", "missing entity", "missing attachment", "zero marker", "missing Parent", "missing local"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			marker := cmd.AddEntity(s3dWorld(10))
			attachment := AuthoredAssetAttachmentComponent{ParentMarker: marker, MountTransform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}}
			components := []any{s3dWorld(12), s3dLocal(2), Parent{Entity: marker}, attachment, s3gUnrelated{7}}
			if kind == "zero marker" {
				attachment.ParentMarker = 0
				components[3] = attachment
			}
			if kind == "missing attachment" {
				components = append(components[:3], components[4:]...)
			}
			if kind == "missing Parent" {
				components = append(components[:2], components[3:]...)
			}
			if kind == "missing local" {
				components = append(components[:1], components[2:]...)
			}
			root := cmd.AddEntity(components...)
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
			targetCmd, target := cmd, root
			if kind == "nil commands" {
				targetCmd = nil
			}
			if kind == "zero root" {
				target = 0
			}
			if kind == "missing entity" {
				target = EntityId(999999)
			}
			if RestoreAuthoredAssetAttachmentMount(targetCmd, target) {
				t.Fatal("invalid restore accepted")
			}
			s3gPublications(t, cmd, before, structural, [4]int{})
		})
	}
}

func TestS3gResetMountCallerPublishesLocalWithoutPrematurePropagation(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	marker := cmd.AddEntity(s3dWorld(20))
	currentParent := cmd.AddEntity(s3dWorld(10))
	root := cmd.AddEntity(s3dWorld(12), s3dLocal(2), Parent{Entity: currentParent}, AuthoredAssetAttachmentComponent{ParentMarker: marker, MountTransform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Position: content.Vec3{4, 0, 0}, Scale: content.Vec3{1, 1, 1}}}, s3gUnrelated{7})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !ResetAuthoredAssetAttachmentMount(cmd, root) {
		t.Fatal("reset rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
	if s3cComponent[Parent](t, cmd, root).Entity != currentParent {
		t.Fatal("reset changed hierarchy ownership")
	}
	s3gLocal(t, cmd, root, s3dLocal(14))
	s3dPosition(t, cmd, root, 12)
	before = s3gRevisions(cmd)
	TransformHierarchySystem(cmd)
	s3gPublications(t, cmd, before, structural, [4]int{1, 0, 0, 0})
	s3dPosition(t, cmd, root, 24)
	before = s3gRevisions(cmd)
	if !ResetAuthoredAssetAttachmentMount(cmd, root) {
		t.Fatal("idempotent reset rejected")
	}
	s3gPublications(t, cmd, before, structural, [4]int{})
}

func TestS3gRestoreMountUsesExactLocalBitsOnUnresolvedHost(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	host := cmd.AddEntity(s3gUnrelated{7})
	world := s3dWorld(12)
	world.Pivot = mgl32.Vec3{7, 8, 9}
	mount := content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}
	root := cmd.AddEntity(world, s3dLocal(0), Parent{Entity: host}, AuthoredAssetAttachmentComponent{ParentMarker: host, MountTransform: mount})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	structural := cmd.StructuralRevision()
	attachment := s3cComponent[AuthoredAssetAttachmentComponent](t, cmd, root)
	for _, step := range []struct {
		name    string
		nanBits uint32
		changed int
	}{{"signed zero", 0, 1}, {"new NaN", 0x7fc01234, 1}, {"identical NaN", 0x7fc01234, 0}, {"changed NaN payload", 0x7fc01235, 1}} {
		t.Run(step.name, func(t *testing.T) {
			attachment.MountTransform.Position[0] = math.Float32frombits(0x80000000)
			attachment.MountTransform.Position[1] = math.Float32frombits(step.nanBits)
			before := s3gRevisions(cmd)
			if !RestoreAuthoredAssetAttachmentMount(cmd, root) {
				t.Fatal("existing unresolved-host restore semantics rejected mount")
			}
			local := s3cComponent[LocalTransformComponent](t, cmd, root)
			if s3gBits(local.Position, local.Rotation, local.Scale) != s3gBits(mgl32.Vec3(attachment.MountTransform.Position), mgl32.QuatIdent(), mgl32.Vec3{1, 1, 1}) {
				t.Fatal("restore lost authored destination TRS bits")
			}
			if *s3cComponent[TransformComponent](t, cmd, root) != world {
				t.Fatal("unresolved host changed retained World or Pivot")
			}
			s3gPublications(t, cmd, before, structural, [4]int{0, step.changed, 0, 0})
		})
	}
}

func TestS3gReparentToDescendantPublishesWritesAndRetainsCycleWorld(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	source := cmd.AddEntity(s3dWorld(10), s3gUnrelated{7})
	child := s3dChild(cmd, source, 2)
	descendant := s3dChild(cmd, child, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	childWorld := *s3cComponent[TransformComponent](t, cmd, child)
	descendantWorld := *s3cComponent[TransformComponent](t, cmd, descendant)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !ReparentPreservingWorldTransform(cmd, child, descendant) {
		t.Fatal("existing descendant reparent semantics rejected usable target World")
	}
	if s3cComponent[Parent](t, cmd, child).Entity != descendant {
		t.Fatal("descendant Parent not committed")
	}
	s3gLocal(t, cmd, child, s3dLocal(-3))
	if *s3cComponent[TransformComponent](t, cmd, child) != childWorld || *s3cComponent[TransformComponent](t, cmd, descendant) != descendantWorld {
		t.Fatal("invalid cycle changed retained World values")
	}
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 1, 0})
}

func TestS3gTwoBoneGripPartialFailurePublishesSuccessfulLocalWrite(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	source := cmd.AddEntity(s3dWorld(0), s3gUnrelated{7})
	shoulderLocal := s3dLocal(0)
	shoulderLocal.Scale = mgl32.Vec3{0, 1, 1}
	shoulder := cmd.AddEntity(s3dWorld(0), shoulderLocal, Parent{Entity: source})
	limbLocal := s3dLocal(0)
	limbLocal.Position = mgl32.Vec3{0, 1, 0}
	elbow := cmd.AddEntity(s3dWorld(0), limbLocal, Parent{Entity: shoulder})
	wrist := cmd.AddEntity(s3dWorld(0), limbLocal, Parent{Entity: elbow})
	hand := s3dChild(cmd, wrist, 0)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	oldLocal := *s3cComponent[LocalTransformComponent](t, cmd, shoulder)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	// The shoulder has a valid conversion parent. Its successful rotation leaves
	// the elbow's conversion parent with zero X scale, preserving this public
	// operation's existing partial-success, false-return behavior.
	if ApplyAuthoredTwoBoneGripAt(cmd, hand, mgl32.Vec3{1, 0, 0}) {
		t.Fatal("zero-scale elbow conversion must retain partial-failure result")
	}
	local := s3cComponent[LocalTransformComponent](t, cmd, shoulder)
	if s3gBits(local.Position, local.Rotation, local.Scale) == s3gBits(oldLocal.Position, oldLocal.Rotation, oldLocal.Scale) {
		t.Fatal("fixture did not produce successful shoulder Local write before failure")
	}
	if local.Position != oldLocal.Position || local.Scale != oldLocal.Scale {
		t.Fatal("grip rotation changed shoulder position or scale")
	}
	if s3cComponent[Parent](t, cmd, shoulder).Entity != source || s3cComponent[Parent](t, cmd, elbow).Entity != shoulder || s3cComponent[Parent](t, cmd, wrist).Entity != elbow || s3cComponent[Parent](t, cmd, hand).Entity != wrist {
		t.Fatal("partial grip changed hierarchy ownership")
	}
	s3gPublications(t, cmd, before, structural, [4]int{-1, 1, 0, 0})
}
