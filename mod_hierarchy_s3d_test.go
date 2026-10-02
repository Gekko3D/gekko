package gekko

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func s3dWorld(x float32) TransformComponent {
	return TransformComponent{Position: mgl32.Vec3{x, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
}
func s3dLocal(x float32) LocalTransformComponent {
	return LocalTransformComponent{Position: mgl32.Vec3{x, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}}
}
func s3dChild(cmd *Commands, parent EntityId, x float32) EntityId {
	return cmd.AddEntity(s3dWorld(-100), s3dLocal(x), Parent{Entity: parent})
}
func s3dTRS(t *testing.T, cmd *Commands, id EntityId, pos, scale mgl32.Vec3, rotation mgl32.Quat) {
	t.Helper()
	w := s3cComponent[TransformComponent](t, cmd, id)
	for _, value := range []float32{w.Position[0], w.Position[1], w.Position[2], w.Scale[0], w.Scale[1], w.Scale[2], w.Rotation.W, w.Rotation.V[0], w.Rotation.V[1], w.Rotation.V[2]} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatalf("entity %d has nonfinite TRS: %+v", id, *w)
		}
	}
	if math.Abs(float64(w.Rotation.Dot(w.Rotation)-1)) > 1e-4 {
		t.Fatalf("entity %d has non-unit world rotation: %v", id, w.Rotation)
	}
	if w.Position.Sub(pos).Len() > 1e-4 || w.Scale.Sub(scale).Len() > 1e-4 || 1-float32(math.Abs(float64(w.Rotation.Dot(rotation)))) > 1e-4 {
		t.Fatalf("entity %d TRS=%+v, want position=%v scale=%v rotation=%v", id, *w, pos, scale, rotation)
	}
}
func s3dPosition(t *testing.T, cmd *Commands, id EntityId, x float32) {
	t.Helper()
	s3dTRS(t, cmd, id, mgl32.Vec3{x, 0, 0}, mgl32.Vec3{1, 1, 1}, mgl32.QuatIdent())
}
func s3dStats(t *testing.T, cmd *Commands, builds, compositions uint64, count int) {
	t.Helper()
	got := cmd.TransformHierarchyStats()
	if got.TopologyBuildCount != builds || got.CompositionCount != compositions || got.TransformCount != count {
		t.Fatalf("hierarchy stats=%+v, want builds=%d compositions=%d transforms=%d", got, builds, compositions, count)
	}
}
func s3dIdle(t *testing.T, cmd *Commands) {
	t.Helper()
	before := cmd.TransformHierarchyStats()
	TransformHierarchySystem(cmd)
	if after := cmd.TransformHierarchyStats(); after != before {
		t.Fatalf("idle invocation did work: before=%+v after=%+v", before, after)
	}
}

func TestS3dHierarchyDeepSelectiveInputsAndWorldRepair(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(1), s3dLocal(99))
	a := s3dChild(cmd, root, 1)
	b := s3dChild(cmd, a, 1)
	c := s3dChild(cmd, b, 1)
	other := cmd.AddEntity(s3dWorld(20))
	leaf := s3dChild(cmd, other, 2)
	app.FlushCommands()
	s3dStats(t, cmd, 0, 0, 0) // Reading must not prepare the newly committed topology.
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 4, 6)
	for i, id := range []EntityId{a, b, c} {
		s3dPosition(t, cmd, id, float32(i+2))
	}
	s3dPosition(t, cmd, leaf, 22)
	for range 3 {
		s3dIdle(t, cmd)
	}
	revision := cmd.StructuralRevision()
	rw := s3cComponent[TransformComponent](t, cmd, root)
	rw.Position = mgl32.Vec3{10, 20, 30}
	rw.Rotation = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 0, 1})
	rw.Scale = mgl32.Vec3{2, 3, 4}
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 7, 6)
	for i, id := range []EntityId{a, b, c} {
		s3dTRS(t, cmd, id, mgl32.Vec3{10, 22 + float32(i)*2, 30}, rw.Scale, rw.Rotation)
	}
	s3dPosition(t, cmd, leaf, 22)
	al := s3cComponent[LocalTransformComponent](t, cmd, a)
	al.Position = mgl32.Vec3{2, 0, 0}
	al.Scale = mgl32.Vec3{2, 1, 1}
	al.Rotation = rw.Rotation
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 10, 6)
	for i, id := range []EntityId{a, b, c} {
		s3dTRS(t, cmd, id, mgl32.Vec3{10 - float32(i)*4, 24, 30}, mgl32.Vec3{4, 3, 4}, mgl32.QuatRotate(mgl32.DegToRad(180), mgl32.Vec3{0, 0, 1}))
	}
	// Repair a consumer's direct child-world edit; its descendant sees the repaired output.
	s3cComponent[TransformComponent](t, cmd, b).Position = mgl32.Vec3{999, 999, 999}
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 11, 6)
	s3dTRS(t, cmd, b, mgl32.Vec3{6, 24, 30}, mgl32.Vec3{4, 3, 4}, mgl32.QuatRotate(mgl32.DegToRad(180), mgl32.Vec3{0, 0, 1}))
	s3dTRS(t, cmd, c, mgl32.Vec3{2, 24, 30}, mgl32.Vec3{4, 3, 4}, mgl32.QuatRotate(mgl32.DegToRad(180), mgl32.Vec3{0, 0, 1}))
	rl := s3cComponent[LocalTransformComponent](t, cmd, root)
	*rl = s3dLocal(500)
	s3dIdle(t, cmd)
	if rl.Position != rw.Position || rl.Rotation != rw.Rotation || rl.Scale != rw.Scale {
		t.Fatal("root local must mirror authoritative world TRS")
	}
	if cmd.StructuralRevision() != revision {
		t.Fatal("direct TRS edits changed committed membership")
	}
}

func TestS3dHierarchyPivotAndExactFloatBits(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(1))
	child := s3dChild(cmd, root, 0)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 1, 2)
	s3cComponent[TransformComponent](t, cmd, root).Pivot = mgl32.Vec3{7, 8, 9}
	s3cComponent[TransformComponent](t, cmd, child).Pivot = mgl32.Vec3{4, 5, 6}
	s3dIdle(t, cmd)
	s3dPosition(t, cmd, child, 1)
	// Both finite inputs produce the same pose, but the sign-bit change is still work.
	local := s3cComponent[LocalTransformComponent](t, cmd, child)
	local.Position[0] = math.Float32frombits(0x80000000)
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 2, 2)
	s3dPosition(t, cmd, child, 1)
	s3dIdle(t, cmd)
	local.Position[0] = math.Float32frombits(0x7fc01234)
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 3, 2)
	if !math.IsNaN(float64(s3cComponent[TransformComponent](t, cmd, child).Position[0])) {
		t.Fatal("NaN input was sanitized")
	}
	s3dIdle(t, cmd)
	local.Position[0] = math.Float32frombits(0x7fc01235)
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 4, 2)
	s3dIdle(t, cmd)
	local.Position[0] = 2
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, child, 3)
	if s3cComponent[TransformComponent](t, cmd, child).Pivot != (mgl32.Vec3{4, 5, 6}) {
		t.Fatal("composition changed render pivot")
	}
}

func TestS3dHierarchyReparentAndIncompleteRoles(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	first := cmd.AddEntity(s3dWorld(1))
	// Parent without Local is a world-space source even when its own Parent is invalid.
	second := cmd.AddEntity(s3dWorld(20), Parent{Entity: EntityId(999999)})
	parentOnly := cmd.AddEntity(Parent{Entity: first})
	noWorld := cmd.AddEntity(s3dLocal(5), Parent{Entity: first})
	noLocal := cmd.AddEntity(s3dWorld(70), Parent{Entity: first})
	child := s3dChild(cmd, first, 2)
	descendant := s3dChild(cmd, child, 3)
	blocked := s3dChild(cmd, parentOnly, 1)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 2, 6)
	s3dPosition(t, cmd, noLocal, 70)
	s3dPosition(t, cmd, blocked, -100)
	if s3cComponent[LocalTransformComponent](t, cmd, noWorld).Position[0] != 5 {
		t.Fatal("entity without world must retain its local")
	}
	revision := cmd.StructuralRevision()
	s3cComponent[Parent](t, cmd, child).Entity = second
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, child, 22)
	s3dPosition(t, cmd, descendant, 25)
	if cmd.TransformHierarchyStats().TopologyBuildCount != 2 {
		t.Fatal("direct Parent edit must rebuild resolution")
	}
	s3dIdle(t, cmd)
	before := *s3cComponent[TransformComponent](t, cmd, child)
	if !ReparentPreservingWorldTransform(cmd, child, first) {
		t.Fatal("preserving-world reparent failed")
	}
	s3dTRS(t, cmd, child, before.Position, before.Scale, before.Rotation)
	s3dPosition(t, cmd, descendant, 25)
	s3dIdle(t, cmd)
	if cmd.StructuralRevision() != revision {
		t.Fatal("direct reparent unexpectedly committed structure")
	}
}

func TestS3dHierarchyInvalidGraphsAndImmediateRecovery(t *testing.T) {
	for _, kind := range []string{"missing", "self", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			root := cmd.AddEntity(s3dWorld(10))
			a := s3dChild(cmd, root, 1)
			b := s3dChild(cmd, a, 2)
			c := s3dChild(cmd, b, 3)
			other := cmd.AddEntity(s3dWorld(100))
			live := s3dChild(cmd, other, 4)
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			frozen := map[EntityId]TransformComponent{}
			for _, id := range []EntityId{a, b, c} {
				frozen[id] = *s3cComponent[TransformComponent](t, cmd, id)
			}
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
			s3cComponent[TransformComponent](t, cmd, other).Position[0] = 200
			before := cmd.TransformHierarchyStats().CompositionCount
			TransformHierarchySystem(cmd)
			if cmd.TransformHierarchyStats().CompositionCount != before+1 {
				t.Fatal("invalid graph must compose only unrelated live branch")
			}
			for id, w := range frozen {
				if got := *s3cComponent[TransformComponent](t, cmd, id); got != w {
					t.Fatalf("invalid %s graph partially composed entity %d: %+v", kind, id, got)
				}
			}
			s3dPosition(t, cmd, live, 204)
			s3dIdle(t, cmd)
			edge.Entity = root
			TransformHierarchySystem(cmd)
			s3dPosition(t, cmd, a, 17)
			s3dPosition(t, cmd, b, 19)
			s3dPosition(t, cmd, c, 22)
			s3dIdle(t, cmd)
			// Committed parent-world removal invalidates descendants; re-admission recovers now.
			cmd.RemoveComponents(root, TransformComponent{})
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			s3cComponent[LocalTransformComponent](t, cmd, a).Position[0] = 9
			TransformHierarchySystem(cmd)
			s3dPosition(t, cmd, c, 22)
			cmd.AddComponents(root, s3dWorld(30))
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			s3dPosition(t, cmd, a, 39)
			s3dPosition(t, cmd, b, 41)
			s3dPosition(t, cmd, c, 44)
			s3dIdle(t, cmd)
		})
	}
}

func TestS3dHierarchyCommittedGrowthReplacementMigrationAndRecycle(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10))
	first := s3dChild(cmd, root, 1)
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 1, 0, 0)
	app.FlushCommands()
	// Getters report the last prepared owner without refreshing at flush.
	s3dStats(t, cmd, 1, 0, 0)
	TransformHierarchySystem(cmd)
	s3dStats(t, cmd, 2, 1, 2)
	children := map[EntityId]float32{first: 11}
	for i := 2; i <= 65; i++ {
		children[s3dChild(cmd, root, float32(i))] = 10 + float32(i)
	}
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	if cmd.TransformHierarchyStats().TransformCount != 66 {
		t.Fatal("same-archetype growth lost tracked entities")
	}
	for id, x := range children {
		s3dPosition(t, cmd, id, x)
	}
	s3dIdle(t, cmd)
	cmd.AddComponents(first, s3dLocal(101), s3dWorld(-500))
	s3dIdle(t, cmd)
	s3dPosition(t, cmd, first, 11)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, first, 111)
	children[first] = 111
	// An unrelated component migrates the row; later direct edits must use current columns.
	cmd.AddComponents(first, s3cRevisionMarker{})
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3cComponent[LocalTransformComponent](t, cmd, first).Position[0] = 102
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, first, 112)
	children[first] = 112
	cmd.RemoveComponents(first, s3cRevisionMarker{})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	for id, x := range children {
		s3dPosition(t, cmd, id, x)
	}
	cmd.RemoveEntity(first)
	s3dIdle(t, cmd)
	s3dPosition(t, cmd, first, 112)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	delete(children, first)
	recycled := s3dChild(cmd, root, 202)
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	children[recycled] = 212
	for id, x := range children {
		s3dPosition(t, cmd, id, x)
	}
	if cmd.EntityExists(first) || cmd.TransformHierarchyStats().TransformCount != 66 {
		t.Fatal("recycled row retained retired identity or wrong count")
	}
	// Local removal changes role to authoritative world source; restoring it resumes ownership.
	cmd.RemoveComponents(recycled, LocalTransformComponent{})
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3cComponent[TransformComponent](t, cmd, recycled).Position[0] = 300
	s3dIdle(t, cmd)
	s3dPosition(t, cmd, recycled, 300)
	cmd.AddComponents(recycled, s3dLocal(5))
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, recycled, 15)
	// Parent removal turns world+local into an authoritative root only at flush.
	cmd.RemoveComponents(recycled, Parent{})
	prepared := cmd.TransformHierarchyStats()
	s3dIdle(t, cmd)
	if cmd.TransformHierarchyStats() != prepared {
		t.Fatal("queued Parent removal refreshed stats")
	}
	if s3cComponent[LocalTransformComponent](t, cmd, recycled).Position[0] != 5 {
		t.Fatal("queued Parent removal changed child role")
	}
	app.FlushCommands()
	if cmd.TransformHierarchyStats() != prepared {
		t.Fatal("stats getter prepared committed Parent removal")
	}
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, recycled, 15)
	if s3cComponent[LocalTransformComponent](t, cmd, recycled).Position[0] != 15 {
		t.Fatal("new root did not mirror preserved world")
	}
	s3cComponent[TransformComponent](t, cmd, recycled).Position[0] = 25
	s3dIdle(t, cmd)
	if s3cComponent[LocalTransformComponent](t, cmd, recycled).Position[0] != 25 {
		t.Fatal("new root world ceased being authoritative")
	}
	cmd.AddComponents(recycled, Parent{Entity: root})
	prepared = cmd.TransformHierarchyStats()
	s3dIdle(t, cmd)
	s3dPosition(t, cmd, recycled, 25)
	app.FlushCommands()
	if cmd.TransformHierarchyStats() != prepared {
		t.Fatal("stats getter prepared committed Parent restoration")
	}
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, recycled, 35)
	s3dIdle(t, cmd)
	for id := range children {
		cmd.RemoveEntity(id)
	}
	cmd.RemoveEntity(root)
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	if cmd.TransformHierarchyStats().TransformCount != 0 {
		t.Fatal("empty owner retained transform membership")
	}
	s3dIdle(t, cmd)
	nextRoot := cmd.AddEntity(s3dWorld(40))
	nextChild := s3dChild(cmd, nextRoot, 6)
	s3dIdle(t, cmd)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, nextChild, 46)
	if cmd.TransformHierarchyStats().TransformCount != 2 {
		t.Fatal("refilled owner retained stale membership")
	}
	s3dIdle(t, cmd)
}

func TestS3dHierarchyNilReadersAndStorageOwnership(t *testing.T) {
	var nilEcs *Ecs
	var nilCmd *Commands
	for name, got := range map[string]TransformHierarchyStats{
		"nil ecs": nilEcs.TransformHierarchyStats(), "zero ecs": (&Ecs{}).TransformHierarchyStats(),
		"nil commands": nilCmd.TransformHierarchyStats(), "zero commands": (&Commands{}).TransformHierarchyStats(),
		"commands without ecs": (&Commands{app: &App{}}).TransformHierarchyStats(),
	} {
		if got != (TransformHierarchyStats{}) {
			t.Fatalf("%s stats=%+v, want zero", name, got)
		}
	}
	for _, cmd := range []*Commands{nil, &Commands{}, &Commands{app: &App{}}} {
		TransformHierarchySystem(cmd)
	}
	app := NewApp()
	cmd := app.Commands()
	shared := *app.ecs
	root := cmd.AddEntity(s3dWorld(1))
	child := s3dChild(cmd, root, 2)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	if shared.TransformHierarchyStats() != cmd.TransformHierarchyStats() {
		t.Fatal("wrapper copied before preparation did not share owner counters")
	}
	// Calling through a copied wrapper must reuse the same storage owner.
	copiedCmd := &Commands{app: &App{ecs: &shared}}
	s3dIdle(t, copiedCmd)
	s3cComponent[LocalTransformComponent](t, copiedCmd, child).Position[0] = 3
	TransformHierarchySystem(copiedCmd)
	s3dPosition(t, cmd, child, 4)
	s3dStats(t, cmd, 1, 2, 2)
	other := NewApp()
	otherCmd := other.Commands()
	// Same structural stamp and entity IDs, different type-registration order and source values.
	otherRoot := otherCmd.AddEntity(s3cRevisionMarker{}, s3dWorld(100))
	otherChild := otherCmd.AddEntity(Parent{Entity: otherRoot}, s3dLocal(5), s3dWorld(-100))
	other.FlushCommands()
	if otherCmd.StructuralRevision() != cmd.StructuralRevision() {
		t.Fatal("fixture must have equal structural stamps")
	}
	s3dStats(t, otherCmd, 0, 0, 0)
	TransformHierarchySystem(otherCmd)
	s3dStats(t, otherCmd, 1, 1, 2)
	s3dPosition(t, otherCmd, otherChild, 105)
	s3dPosition(t, cmd, child, 4)
	s3dStats(t, cmd, 1, 2, 2)
	s3dIdle(t, otherCmd)
	s3dIdle(t, cmd)
	cmd.RemoveEntity(child)
	app.FlushCommands()
	if shared.TransformHierarchyStats() != cmd.TransformHierarchyStats() || shared.TransformHierarchyStats().TransformCount != 2 {
		t.Fatal("reader refreshed committed removal")
	}
	TransformHierarchySystem(cmd)
	if shared.TransformHierarchyStats() != cmd.TransformHierarchyStats() || shared.TransformHierarchyStats().TransformCount != 1 {
		t.Fatal("copied wrapper did not observe prepared removal")
	}
	s3dStats(t, otherCmd, 1, 1, 2)
	empty := NewApp()
	emptyCmd := empty.Commands()
	s3dStats(t, emptyCmd, 0, 0, 0)
	TransformHierarchySystem(emptyCmd)
	s3dStats(t, emptyCmd, 1, 0, 0)
	s3dIdle(t, emptyCmd)
}

func TestS3dHierarchyAllDirectParentEditsPrecedeComposition(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10))
	a := s3dChild(cmd, root, 1)
	b := s3dChild(cmd, a, 2)
	c := s3dChild(cmd, b, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, a, 11)
	s3dPosition(t, cmd, b, 13)
	s3dPosition(t, cmd, c, 16)
	before := cmd.TransformHierarchyStats()
	revision := cmd.StructuralRevision()
	// Reverse a dependency without changing committed membership. Composing as
	// Parent edits are discovered would use an old parent world or a false cycle.
	s3cComponent[Parent](t, cmd, a).Entity = b
	s3cComponent[Parent](t, cmd, b).Entity = root
	TransformHierarchySystem(cmd)
	s3dPosition(t, cmd, b, 12)
	s3dPosition(t, cmd, a, 13)
	s3dPosition(t, cmd, c, 15)
	if cmd.TransformHierarchyStats().TopologyBuildCount != before.TopologyBuildCount+1 {
		t.Fatal("one invocation with several Parent edits must rebuild topology once")
	}
	if cmd.StructuralRevision() != revision {
		t.Fatal("Parent field edits changed committed membership")
	}
	s3dIdle(t, cmd)
}
