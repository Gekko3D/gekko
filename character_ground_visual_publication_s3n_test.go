package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

// Public queries expose committed values and membership; float bits keep NaNs
// comparable. Revision stamps are checked only for changed versus unchanged.
type s3nValues struct {
	transforms [2]map[EntityId][10]uint32
	pivots     map[EntityId][3]uint32
	parents    map[EntityId]EntityId
	unrelated  map[EntityId]int
}

func s3nSnapshot(cmd *Commands) s3nValues {
	result := s3nValues{s3lSnapshot(cmd), map[EntityId][3]uint32{}, map[EntityId]EntityId{}, map[EntityId]int{}}
	MakeQuery1[TransformComponent](cmd).Map(func(id EntityId, tr *TransformComponent) bool {
		result.pivots[id] = [3]uint32{math.Float32bits(tr.Pivot[0]), math.Float32bits(tr.Pivot[1]), math.Float32bits(tr.Pivot[2])}
		return true
	})
	MakeQuery1[Parent](cmd).Map(func(id EntityId, parent *Parent) bool {
		result.parents[id] = parent.Entity
		return true
	})
	MakeQuery1[s3gUnrelated](cmd).Map(func(id EntityId, other *s3gUnrelated) bool {
		result.unrelated[id] = other.Value
		return true
	})
	return result
}

func s3nStep(t *testing.T, cmd *Commands, call func(), y float32, changed ...EntityId) {
	t.Helper()
	want := s3nSnapshot(cmd)
	for _, id := range changed {
		bits, present := want.transforms[1][id]
		if !present || bits[1] == math.Float32bits(y) {
			t.Fatalf("invalid expected changed Local Y for entity %d", id)
		}
		bits[1] = math.Float32bits(y)
		want.transforms[1][id] = bits
	}
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	call()
	if got := s3nSnapshot(cmd); !reflect.DeepEqual(got, want) {
		t.Errorf("helper changed committed membership or fields beyond expected Local Y: got=%+v want=%+v", got, want)
	}
	after := s3gRevisions(cmd)
	for i, typ := range s3gTypes {
		wantChanged := i == 1 && len(changed) != 0
		if (after[i] != before[i]) != wantChanged {
			t.Errorf("%v publication changed=%v, want %v (before=%d after=%d)", typ, after[i] != before[i], wantChanged, before[i], after[i])
		}
	}
	if cmd.StructuralRevision() != structural {
		t.Error("helper changed committed structural revision")
	}
}

func TestS3nSelectedChildrenAndActualDestination(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	world := s3mPose(mgl32.Vec3{70, 80, 90})
	world.Pivot[0] = math.Float32frombits(0x7fc04567)
	parent := cmd.AddEntity(world, s3gUnrelated{1})
	otherParent := cmd.AddEntity(world, s3gUnrelated{2})
	local := s3lLocal(s3mPose(mgl32.Vec3{1, 9, 3}))
	local.Position[0] = math.Float32frombits(0x7fc01234)
	local.Rotation.V[2] = math.Float32frombits(0x7fc01235)
	local.Scale[0] = math.Float32frombits(0x7fc01236)
	both := cmd.AddEntity(world, local, Parent{Entity: parent}, s3gUnrelated{3})
	localOnly := cmd.AddEntity(local, Parent{Entity: parent}, s3gUnrelated{4})
	quiet := local
	quiet.Position[1] = 2
	cmd.AddEntity(world, quiet, Parent{Entity: parent}, s3gUnrelated{5})
	cmd.AddEntity(world, local, s3gUnrelated{6}) // Missing Parent.
	missingLocal := cmd.AddEntity(world, Parent{Entity: parent}, s3gUnrelated{7})
	cmd.AddEntity(world, local, Parent{Entity: otherParent}, s3gUnrelated{8})
	cmd.AddEntity(world, local, Parent{Entity: both}, s3gUnrelated{9}) // Grandchild.
	app.FlushCommands()
	call := func() { ApplyCharacterVisualGroundOffsetToChildren(cmd, parent, 2) }
	s3nStep(t, cmd, call, 2, both, localOnly)
	s3nStep(t, cmd, call, 2)
	// An unmarked external destination edit must be repaired and published.
	s3cComponent[LocalTransformComponent](t, cmd, localOnly).Position[1] = 99
	s3nStep(t, cmd, call, 2, localOnly)
	s3nStep(t, cmd, call, 2)
	s3nStep(t, cmd, func() { ApplyCharacterVisualGroundOffsetToChildren(nil, parent, 3) }, 3)
	s3nStep(t, cmd, func() { ApplyCharacterVisualGroundOffsetToChildren(cmd, EntityId(999999), 3) }, 3)
	if cmd.GetComponent(missingLocal, s3gTypes[1]) != nil || cmd.GetComponent(localOnly, s3gTypes[0]) != nil {
		t.Fatal("helper created missing Local or World")
	}
}

func TestS3nExactYBitsWithMissingParentEntity(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	const parent EntityId = 999999
	local := s3lLocal(s3mPose(mgl32.Vec3{}))
	local.Position[2] = math.Float32frombits(0x7fc04567)
	local.Rotation.W = math.Float32frombits(0x7fc04568)
	local.Scale[1] = math.Float32frombits(0x7fc04569)
	id := cmd.AddEntity(local, Parent{Entity: parent}, s3gUnrelated{1})
	app.FlushCommands()
	for _, step := range []struct {
		bits    uint32
		changed bool
	}{{0, false}, {0x80000000, true}, {0x80000000, false}, {0, true}, {0x7fc01234, true}, {0x7fc01234, false}, {0x7fc01235, true}} {
		y := math.Float32frombits(step.bits)
		var changed []EntityId
		if step.changed {
			changed = []EntityId{id}
		}
		s3nStep(t, cmd, func() { ApplyCharacterVisualGroundOffsetToChildren(cmd, parent, y) }, y, changed...)
	}
	if cmd.EntityExists(parent) {
		t.Fatal("helper created absent parent")
	}
}

func TestS3nQueuedMutationBoundaries(t *testing.T) {
	for _, operation := range []string{"new entity", "add Parent", "add Local", "remove Parent", "remove Local", "remove entity", "replace Parent", "replace Local"} {
		t.Run(operation, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			const parent EntityId = 999999
			old := s3lLocal(s3mPose(mgl32.Vec3{1, 9, 3}))
			components := []any{s3gUnrelated{1}}
			if operation != "add Parent" {
				components = append(components, Parent{Entity: parent})
			}
			if operation != "add Local" {
				components = append(components, old)
			}
			id := cmd.AddEntity(components...)
			app.FlushCommands()
			queued := s3lLocal(s3mPose(mgl32.Vec3{80, 81, 82}))
			var pending EntityId
			switch operation {
			case "new entity":
				pending = cmd.AddEntity(queued, Parent{Entity: parent}, s3gUnrelated{2})
			case "add Parent":
				cmd.AddComponents(id, Parent{Entity: parent})
			case "add Local", "replace Local":
				cmd.AddComponents(id, queued)
			case "remove Parent":
				cmd.RemoveComponents(id, Parent{})
			case "remove Local":
				cmd.RemoveComponents(id, LocalTransformComponent{})
			case "remove entity":
				cmd.RemoveEntity(id)
			case "replace Parent":
				cmd.AddComponents(id, Parent{Entity: parent + 1})
			}
			var changed []EntityId
			if operation != "add Parent" && operation != "add Local" {
				changed = []EntityId{id}
			}
			call := func() { ApplyCharacterVisualGroundOffsetToChildren(cmd, parent, 2) }
			s3nStep(t, cmd, call, 2, changed...)
			if !cmd.EntityExists(id) || pending != 0 && cmd.EntityExists(pending) {
				t.Fatal("helper applied queued entity mutation")
			}
			app.FlushCommands()
			switch operation {
			case "new entity":
				if !cmd.EntityExists(pending) || *s3cComponent[LocalTransformComponent](t, cmd, pending) != queued {
					t.Fatal("queued entity addition was lost or rewritten")
				}
				changed = []EntityId{pending}
			case "add Parent":
				if s3cComponent[Parent](t, cmd, id).Entity != parent || *s3cComponent[LocalTransformComponent](t, cmd, id) != old {
					t.Fatal("queued Parent addition was lost or applied early")
				}
				changed = []EntityId{id}
			case "add Local", "replace Local":
				if *s3cComponent[LocalTransformComponent](t, cmd, id) != queued {
					t.Fatal("queued Local addition/replacement was lost or rewritten")
				}
				changed = []EntityId{id}
			case "remove Parent":
				if cmd.GetComponent(id, s3gTypes[2]) != nil || s3cComponent[LocalTransformComponent](t, cmd, id).Position[1] != 2 {
					t.Fatal("queued Parent removal was lost or changed current Local")
				}
				changed = nil
			case "remove Local":
				if cmd.GetComponent(id, s3gTypes[1]) != nil {
					t.Fatal("queued Local removal was lost")
				}
				changed = nil
			case "remove entity":
				if cmd.EntityExists(id) {
					t.Fatal("queued entity removal was lost")
				}
				changed = nil
			case "replace Parent":
				if s3cComponent[Parent](t, cmd, id).Entity != parent+1 || s3cComponent[LocalTransformComponent](t, cmd, id).Position[1] != 2 {
					t.Fatal("queued Parent replacement was lost or changed current Local")
				}
				changed = nil
			}
			s3nStep(t, cmd, call, 2, changed...)
		})
	}
}

func TestS3nHierarchyOwnsLaterWorldPropagation(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	rootPose := s3mPose(mgl32.Vec3{10, 20, 30})
	rootPose.Scale = mgl32.Vec3{2, 3, 4}
	root := cmd.AddEntity(rootPose, s3gUnrelated{1})
	childPose := s3mPose(mgl32.Vec3{1, 5, 3})
	child := cmd.AddEntity(childPose, s3lLocal(childPose), Parent{Entity: root}, s3gUnrelated{2})
	leafPose := s3mPose(mgl32.Vec3{4, 6, 2})
	leaf := cmd.AddEntity(leafPose, s3lLocal(leafPose), Parent{Entity: child}, s3gUnrelated{3})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3nStep(t, cmd, func() { ApplyCharacterVisualGroundOffsetToChildren(cmd, root, 2) }, 2, child)
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	TransformHierarchySystem(cmd)
	s3gPublications(t, cmd, before, structural, [4]int{1, 0, 0, 0})
	s3dTRS(t, cmd, child, mgl32.Vec3{12, 26, 42}, rootPose.Scale, mgl32.QuatIdent())
	s3dTRS(t, cmd, leaf, mgl32.Vec3{20, 44, 50}, rootPose.Scale, mgl32.QuatIdent())
	if s3cComponent[LocalTransformComponent](t, cmd, leaf).Position != leafPose.Position {
		t.Fatal("ground offset or hierarchy rewrote descendant Local")
	}
	s3nStep(t, cmd, func() { ApplyCharacterVisualGroundOffsetToChildren(cmd, root, 2) }, 2)
}
