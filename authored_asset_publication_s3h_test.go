package gekko

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

var s3hTypes = [...]reflect.Type{
	reflect.TypeOf(TransformComponent{}), reflect.TypeOf(LocalTransformComponent{}),
	reflect.TypeOf(Parent{}), reflect.TypeOf(AuthoredAssetAttachmentComponent{}), reflect.TypeOf(s3gUnrelated{}),
}

func s3hRevisions(cmd *Commands) [5]uint64 {
	var revisions [5]uint64
	for i, typ := range s3hTypes {
		revisions[i] = cmd.ComponentRevision(typ)
	}
	return revisions
}

// Component revisions are aggregate publication stamps, not event counts.
func s3hPublications(t *testing.T, cmd *Commands, before [5]uint64, want [5]bool) {
	t.Helper()
	for i, after := range s3hRevisions(cmd) {
		if want[i] && after <= before[i] {
			t.Errorf("%v changed committed value without publication: before=%d after=%d", s3hTypes[i], before[i], after)
		}
		if !want[i] && after != before[i] {
			t.Errorf("%v published unchanged value: before=%d after=%d", s3hTypes[i], before[i], after)
		}
	}
}

func s3hAttachment(x float32) content.AssetAttachmentDef {
	return content.AssetAttachmentDef{ID: "mount", Transform: content.AssetTransformDef{
		Position: content.Vec3{x, 0, 0}, Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1},
	}}
}

func s3hMarkerComponents() []any {
	return []any{AuthoredAssetRefComponent{ItemID: "surface", Kind: AuthoredItemKindMarker}, AuthoredMarkerComponent{Kind: content.AssetMarkerKindSurfaceMount}}
}

func TestS3hAttachPublishesInitialLocalAndCommitsMount(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	host := cmd.AddEntity(s3dWorld(10))
	root := cmd.AddEntity(s3dWorld(2), s3dLocal(2), s3gUnrelated{7})
	leaf := s3dChild(cmd, root, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	attachment := s3hAttachment(4)
	attachment.Transform.Scale = content.Vec3{2, 3, 4}
	before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
	if err := AttachAuthoredAssetRoot(cmd, root, host, attachment); err != nil {
		t.Fatal(err)
	}
	// No grip marker: Restore assigns exactly the already-written Local value.
	// Migration carries Local but publishes only supplied Parent/Attachment.
	s3hPublications(t, cmd, before, [5]bool{true, true, true, true, false})
	if cmd.StructuralRevision() <= structural || s3cComponent[Parent](t, cmd, root).Entity != host {
		t.Fatal("attachment did not immediately commit Parent")
	}
	stored := s3cComponent[AuthoredAssetAttachmentComponent](t, cmd, root)
	if stored.AttachmentID != attachment.ID || stored.ParentMarker != host || stored.MountTransform != attachment.Transform {
		t.Fatal("committed attachment lost authored mount")
	}
	s3gLocal(t, cmd, root, LocalTransformComponent{Position: mgl32.Vec3{4, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 3, 4}})
	s3dTRS(t, cmd, root, mgl32.Vec3{14, 0, 0}, mgl32.Vec3{2, 3, 4}, mgl32.QuatIdent())
	s3dTRS(t, cmd, leaf, mgl32.Vec3{20, 0, 0}, mgl32.Vec3{2, 3, 4}, mgl32.QuatIdent())
	before, structural = s3hRevisions(cmd), cmd.StructuralRevision()
	if err := AttachAuthoredAssetRoot(cmd, root, host, attachment); err != nil {
		t.Fatal(err)
	}
	s3hPublications(t, cmd, before, [5]bool{false, false, true, true, false})
	if cmd.StructuralRevision() <= structural {
		t.Fatal("equal supplied components lost existing replacement semantics")
	}
}

func TestS3hAttachComparesEveryLiveLocalTRSBit(t *testing.T) {
	for channel := 0; channel < 10; channel++ {
		t.Run(fmt.Sprintf("channel_%d", channel), func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			// No root World and no host World: hierarchy cannot publish Local.
			host := cmd.AddEntity(s3gUnrelated{7})
			root := cmd.AddEntity(s3dLocal(0))
			app.FlushCommands()
			attachment := s3hAttachment(0)
			fields := []*float32{&attachment.Transform.Position[0], &attachment.Transform.Position[1], &attachment.Transform.Position[2], &attachment.Transform.Rotation[3], &attachment.Transform.Rotation[0], &attachment.Transform.Rotation[1], &attachment.Transform.Rotation[2], &attachment.Transform.Scale[0], &attachment.Transform.Scale[1], &attachment.Transform.Scale[2]}
			*fields[channel] = 0
			if err := AttachAuthoredAssetRoot(cmd, root, host, attachment); err != nil {
				t.Fatal(err)
			}
			for _, bits := range []uint32{0x80000000, 0x7fc01234, 0x7fc01234, 0x7fc01235} {
				old := *s3cComponent[LocalTransformComponent](t, cmd, root)
				*fields[channel] = math.Float32frombits(bits)
				before := s3hRevisions(cmd)
				if err := AttachAuthoredAssetRoot(cmd, root, host, attachment); err != nil {
					t.Fatal(err)
				}
				local := s3cComponent[LocalTransformComponent](t, cmd, root)
				var wantBits [10]uint32
				for i, field := range fields {
					wantBits[i] = math.Float32bits(*field)
				}
				if s3gBits(local.Position, local.Rotation, local.Scale) != wantBits {
					t.Fatal("raw attachment conversion changed TRS bits")
				}
				s3hPublications(t, cmd, before, [5]bool{false, s3gBits(old.Position, old.Rotation, old.Scale) != wantBits, true, true, false})
			}
			// Compare the live destination, including an unmarked external edit.
			s3cComponent[LocalTransformComponent](t, cmd, root).Position[0] = 999
			before := s3hRevisions(cmd)
			if err := AttachAuthoredAssetRoot(cmd, root, host, attachment); err != nil {
				t.Fatal(err)
			}
			s3hPublications(t, cmd, before, [5]bool{false, true, true, true, false})
		})
	}
}

func TestS3hAttachEarlyErrorsKeepCommittedValuesAndPendingWork(t *testing.T) {
	for _, kind := range []string{"nil commands", "zero root", "zero host", "empty id", "missing local", "missing aim marker"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			host := cmd.AddEntity(s3dWorld(10))
			components := []any{s3dWorld(12), Parent{Entity: host}, AuthoredAssetAttachmentComponent{AttachmentID: "old", ParentMarker: host}, s3gUnrelated{7}}
			if kind != "missing local" {
				components = append(components, s3dLocal(2))
			}
			root := cmd.AddEntity(components...)
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			oldWorld := *s3cComponent[TransformComponent](t, cmd, root)
			var oldLocal LocalTransformComponent
			if kind != "missing local" {
				oldLocal = *s3cComponent[LocalTransformComponent](t, cmd, root)
			}
			pending := cmd.AddEntity(s3gUnrelated{9})
			attachment := s3hAttachment(4)
			targetCmd, targetRoot, targetHost := cmd, root, host
			switch kind {
			case "nil commands":
				targetCmd = nil
			case "zero root":
				targetRoot = 0
			case "zero host":
				targetHost = 0
			case "empty id":
				attachment.ID = ""
			case "missing aim marker":
				attachment.Aim = &content.AssetAttachmentAimFrameDef{MarkerID: "missing"}
			}
			before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
			if AttachAuthoredAssetRoot(targetCmd, targetRoot, targetHost, attachment) == nil {
				t.Fatal("invalid attachment accepted")
			}
			s3hPublications(t, cmd, before, [5]bool{})
			if cmd.StructuralRevision() != structural || cmd.EntityExists(pending) || *s3cComponent[TransformComponent](t, cmd, root) != oldWorld || s3cComponent[Parent](t, cmd, root).Entity != host || s3cComponent[AuthoredAssetAttachmentComponent](t, cmd, root).AttachmentID != "old" {
				t.Fatal("early error changed committed state or flushed pending work")
			}
			if kind != "missing local" && *s3cComponent[LocalTransformComponent](t, cmd, root) != oldLocal {
				t.Fatal("early error changed Local")
			}
			app.FlushCommands()
			if !cmd.EntityExists(pending) {
				t.Fatal("early error discarded pending work")
			}
		})
	}
}

func TestS3hAttachPreservesItsExistingFlush(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	host := cmd.AddEntity(s3dWorld(10))
	root := cmd.AddEntity(s3dWorld(2), s3dLocal(2))
	doomed := cmd.AddEntity(s3gUnrelated{1})
	edited := cmd.AddEntity(s3gUnrelated{2}, s3cRevisionMarker{})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	pending := cmd.AddEntity(s3gUnrelated{3})
	cmd.RemoveEntity(doomed)
	cmd.RemoveComponents(edited, s3cRevisionMarker{})
	cmd.AddComponents(edited, s3gUnrelated{4})
	before := s3hRevisions(cmd)
	if err := AttachAuthoredAssetRoot(cmd, root, host, s3hAttachment(4)); err != nil {
		t.Fatal(err)
	}
	s3hPublications(t, cmd, before, [5]bool{true, true, true, true, true})
	if !cmd.EntityExists(pending) || cmd.EntityExists(doomed) || cmd.GetComponent(edited, reflect.TypeOf(s3cRevisionMarker{})) != nil || s3cComponent[s3gUnrelated](t, cmd, edited).Value != 4 {
		t.Fatal("attachment changed existing pending-command flush behavior")
	}
	s3dPosition(t, cmd, root, 14)
}

func TestS3hSurfacePublishesOwnDestinationsWithoutHierarchyMasking(t *testing.T) {
	cases := []struct {
		name             string
		position, normal mgl32.Vec3
	}{
		{"translation", mgl32.Vec3{5, 0, 0}, mgl32.Vec3{0, 0, 1}},
		{"rotation", mgl32.Vec3{}, mgl32.Vec3{0, 1, 0}},
		{"signed zero", mgl32.Vec3{math.Float32frombits(0x80000000), 0, 0}, mgl32.Vec3{0, 0, 1}},
	}
	for _, withLocal := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("local_%t/%s", withLocal, tc.name), func(t *testing.T) {
				app := NewApp()
				cmd := app.Commands()
				components := []any{s3dWorld(0), s3gUnrelated{7}}
				if withLocal {
					// A root can itself be a marker. No derived World exists to mask
					// a missing publication of the authoritative root write.
					components = append(components, s3dLocal(0))
					components = append(components, s3hMarkerComponents()...)
				}
				root := cmd.AddEntity(components...)
				if !withLocal {
					// Lookup accepts this descendant. A missing-World barrier keeps
					// its World fixed, isolating the root's direct World publication.
					barrier := cmd.AddEntity(Parent{Entity: root})
					marker := append([]any{s3dWorld(0), s3dLocal(0), Parent{Entity: barrier}}, s3hMarkerComponents()...)
					cmd.AddEntity(marker...)
				}
				app.FlushCommands()
				TransformHierarchySystem(cmd)
				old := *s3cComponent[TransformComponent](t, cmd, root)
				before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
				if !AlignAuthoredAssetSurfaceMount(cmd, root, tc.position, tc.normal) {
					t.Fatal("existing surface lookup rejected")
				}
				world := s3hFiniteWorld(t, cmd, root)
				actual := s3gBits(world.Position, world.Rotation, world.Scale)
				if actual == s3gBits(old.Position, old.Rotation, old.Scale) {
					t.Fatal("isolated fixture did not change actual World TRS bits")
				}
				if world.Position != tc.position || world.Rotation.Rotate(mgl32.Vec3{0, 0, 1}).Sub(tc.normal).Len() > 1e-4 {
					t.Fatal("isolated surface root missed target position/normal")
				}
				if tc.name == "rotation" && world.Position != old.Position {
					t.Fatal("rotation-only fixture changed Position")
				}
				if tc.name == "signed zero" && math.Float32bits(world.Position[0]) != 0x80000000 {
					t.Fatal("surface assignment lost signed zero Position")
				}
				if withLocal {
					local := s3cComponent[LocalTransformComponent](t, cmd, root)
					if s3gBits(local.Position, local.Rotation, local.Scale) != actual {
						t.Fatal("surface Local copy lost actual World TRS bits")
					}
				}
				s3hPublications(t, cmd, before, [5]bool{true, withLocal, false, false, false})
				if cmd.StructuralRevision() != structural || !withLocal && cmd.GetComponent(root, s3hTypes[1]) != nil {
					t.Fatal("surface alignment changed membership or created Local")
				}
			})
		}
	}
}

func s3hFiniteWorld(t *testing.T, cmd *Commands, id EntityId) *TransformComponent {
	t.Helper()
	world := s3cComponent[TransformComponent](t, cmd, id)
	for _, bits := range s3gBits(world.Position, world.Rotation, world.Scale) {
		value := float64(math.Float32frombits(bits))
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("entity %d has nonfinite World TRS: %+v", id, *world)
		}
	}
	if math.Abs(float64(world.Rotation.Dot(world.Rotation)-1)) > 1e-4 {
		t.Fatalf("entity %d has non-unit World rotation: %v", id, world.Rotation)
	}
	return world
}

func TestS3hSurfaceRetainsScalePivotAndPropagatesMarker(t *testing.T) {
	for _, withLocal := range []bool{false, true} {
		t.Run(fmt.Sprintf("local_%t", withLocal), func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			world := TransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{2, 3, 4}, Pivot: mgl32.Vec3{7, 8, 9}}
			components := []any{world, s3gUnrelated{7}}
			if withLocal {
				components = append(components, LocalTransformComponent{Position: world.Position, Rotation: world.Rotation, Scale: world.Scale})
			}
			root := cmd.AddEntity(components...)
			markerLocal := LocalTransformComponent{Position: mgl32.Vec3{1, 2, 3}, Rotation: mgl32.QuatRotate(0.4, mgl32.Vec3{0, 1, 0}), Scale: mgl32.Vec3{1, 1, 1}}
			markerComponents := append([]any{s3dWorld(-100), markerLocal, Parent{Entity: root}}, s3hMarkerComponents()...)
			marker := cmd.AddEntity(markerComponents...)
			leaf := s3dChild(cmd, marker, 2)
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
			target, normal := mgl32.Vec3{11, 12, 13}, mgl32.Vec3{0, 1, 0}
			if !AlignAuthoredAssetSurfaceMount(cmd, root, target, normal) {
				t.Fatal("valid surface alignment rejected")
			}
			s3hPublications(t, cmd, before, [5]bool{true, withLocal, false, false, false})
			got := s3hFiniteWorld(t, cmd, root)
			mounted := s3hFiniteWorld(t, cmd, marker)
			if got.Scale != world.Scale || got.Pivot != world.Pivot || mounted.Position.Sub(target).Len() > 1e-4 || mounted.Rotation.Rotate(mgl32.Vec3{0, 0, 1}).Sub(normal).Len() > 1e-4 {
				t.Fatal("surface marker missed target/normal or root lost scale/pivot")
			}
			wantLeaf := mounted.Position.Add(mounted.Rotation.Rotate(mgl32.Vec3{4, 0, 0}))
			s3dTRS(t, cmd, leaf, wantLeaf, world.Scale, mounted.Rotation)
			if withLocal {
				local := s3cComponent[LocalTransformComponent](t, cmd, root)
				if s3gBits(local.Position, local.Rotation, local.Scale) != s3gBits(got.Position, got.Rotation, got.Scale) {
					t.Fatal("surface Local copy differs from actual World TRS")
				}
			}
			if cmd.StructuralRevision() != structural {
				t.Fatal("surface alignment changed committed structure")
			}
		})
	}
}

func TestS3hSurfaceSettledIdentityIsQuiet(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	components := append([]any{s3dWorld(0), s3dLocal(0), s3gUnrelated{7}}, s3hMarkerComponents()...)
	root := cmd.AddEntity(components...)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	for range 3 {
		before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
		if !AlignAuthoredAssetSurfaceMount(cmd, root, mgl32.Vec3{}, mgl32.Vec3{0, 0, 1}) {
			t.Fatal("identity alignment rejected")
		}
		s3hPublications(t, cmd, before, [5]bool{})
		if cmd.StructuralRevision() != structural {
			t.Fatal("identity alignment changed structure")
		}
	}
}

func TestS3hSurfaceLocalCopyUsesActualScaleBitsIndependently(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	components := append([]any{s3dWorld(0), s3dLocal(0), Parent{Entity: EntityId(999999)}, s3gUnrelated{7}}, s3hMarkerComponents()...)
	root := cmd.AddEntity(components...)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	// The accepted unresolved Parent prevents root-local hierarchy mirroring.
	// Surface math leaves Scale untouched; only the optional Local copy changes.
	for _, bits := range []uint32{0, 0x80000000, 0x80000000, 0x7fc01234, 0x7fc01234, 0x7fc01235} {
		world := s3cComponent[TransformComponent](t, cmd, root)
		world.Scale[0] = math.Float32frombits(bits)
		world.Pivot = mgl32.Vec3{7, 8, 9}
		oldWorld := s3gBits(world.Position, world.Rotation, world.Scale)
		oldLocal := *s3cComponent[LocalTransformComponent](t, cmd, root)
		before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
		if !AlignAuthoredAssetSurfaceMount(cmd, root, mgl32.Vec3{}, mgl32.Vec3{0, 0, 1}) {
			t.Fatal("existing unresolved-parent alignment rejected")
		}
		world = s3cComponent[TransformComponent](t, cmd, root)
		local := s3cComponent[LocalTransformComponent](t, cmd, root)
		actualWorld, actualLocal := s3gBits(world.Position, world.Rotation, world.Scale), s3gBits(local.Position, local.Rotation, local.Scale)
		if actualWorld != oldWorld || actualLocal != actualWorld || world.Pivot != (mgl32.Vec3{7, 8, 9}) {
			t.Fatal("surface alignment changed retained World bits/pivot or lost Local copy bits")
		}
		s3hPublications(t, cmd, before, [5]bool{false, s3gBits(oldLocal.Position, oldLocal.Rotation, oldLocal.Scale) != actualLocal, false, false, false})
		if cmd.StructuralRevision() != structural {
			t.Fatal("surface Local copy changed structure")
		}
	}
}

func TestS3hSurfaceRejectionsKeepAttemptedWritesAndPendingWorkQuiet(t *testing.T) {
	for _, kind := range []string{"nil commands", "zero root", "zero normal", "small normal", "missing marker", "missing root World"} {
		t.Run(kind, func(t *testing.T) {
			app := NewApp()
			cmd := app.Commands()
			components := []any{s3dLocal(0), s3gUnrelated{7}}
			if kind != "missing root World" {
				components = append(components, s3dWorld(0))
			}
			root := cmd.AddEntity(components...)
			if kind != "missing marker" {
				marker := append([]any{s3dWorld(0), s3dLocal(0), Parent{Entity: root}}, s3hMarkerComponents()...)
				cmd.AddEntity(marker...)
			}
			app.FlushCommands()
			TransformHierarchySystem(cmd)
			oldLocal := *s3cComponent[LocalTransformComponent](t, cmd, root)
			var oldWorld TransformComponent
			if kind != "missing root World" {
				oldWorld = *s3cComponent[TransformComponent](t, cmd, root)
			}
			pending := cmd.AddEntity(s3gUnrelated{9})
			targetCmd, targetRoot, normal := cmd, root, mgl32.Vec3{0, 0, 1}
			switch kind {
			case "nil commands":
				targetCmd = nil
			case "zero root":
				targetRoot = 0
			case "zero normal":
				normal = mgl32.Vec3{}
			case "small normal":
				normal = mgl32.Vec3{0, 0, 0.00001}
			}
			before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
			if AlignAuthoredAssetSurfaceMount(targetCmd, targetRoot, mgl32.Vec3{5, 6, 7}, normal) {
				t.Fatal("invalid surface alignment accepted")
			}
			s3hPublications(t, cmd, before, [5]bool{})
			if cmd.StructuralRevision() != structural || cmd.EntityExists(pending) || *s3cComponent[LocalTransformComponent](t, cmd, root) != oldLocal {
				t.Fatal("rejected surface alignment changed Local or flushed pending work")
			}
			if kind != "missing root World" && *s3cComponent[TransformComponent](t, cmd, root) != oldWorld {
				t.Fatal("rejected surface alignment changed World")
			}
			app.FlushCommands()
			if !cmd.EntityExists(pending) {
				t.Fatal("surface alignment discarded pending work")
			}
		})
	}
}

func TestS3hSurfaceSuccessDoesNotFlushPendingCommands(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	components := append([]any{s3dWorld(0), s3dLocal(0)}, s3hMarkerComponents()...)
	root := cmd.AddEntity(components...)
	doomed := cmd.AddEntity(s3gUnrelated{1})
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	pending := cmd.AddEntity(s3gUnrelated{2})
	cmd.RemoveEntity(doomed)
	cmd.RemoveComponents(root, AuthoredMarkerComponent{})
	cmd.AddComponents(root, s3dWorld(77), s3dLocal(77))
	before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
	if !AlignAuthoredAssetSurfaceMount(cmd, root, mgl32.Vec3{5, 0, 0}, mgl32.Vec3{0, 0, 1}) {
		t.Fatal("committed surface marker rejected with pending commands")
	}
	s3hPublications(t, cmd, before, [5]bool{true, true, false, false, false})
	if cmd.StructuralRevision() != structural || cmd.EntityExists(pending) || !cmd.EntityExists(doomed) || cmd.GetComponent(root, reflect.TypeOf(AuthoredMarkerComponent{})) == nil {
		t.Fatal("surface alignment flushed pending mutations")
	}
	s3dPosition(t, cmd, root, 5)
	app.FlushCommands()
	if !cmd.EntityExists(pending) || cmd.EntityExists(doomed) || cmd.GetComponent(root, reflect.TypeOf(AuthoredMarkerComponent{})) != nil || s3cComponent[LocalTransformComponent](t, cmd, root).Position[0] != 77 {
		t.Fatal("surface alignment lost queued mutations")
	}
	s3dPosition(t, cmd, root, 77)
}

func TestS3hRejectedSurfaceStillPublishesInitialHierarchyRepair(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10), s3dLocal(10), s3gUnrelated{7})
	leaf := s3dChild(cmd, root, 3)
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	s3cComponent[TransformComponent](t, cmd, leaf).Position[0] = 999
	before, structural := s3hRevisions(cmd), cmd.StructuralRevision()
	if AlignAuthoredAssetSurfaceMount(cmd, root, mgl32.Vec3{5, 6, 7}, mgl32.Vec3{0, 0, 1}) {
		t.Fatal("missing surface marker accepted")
	}
	s3hPublications(t, cmd, before, [5]bool{true, false, false, false, false})
	s3dPosition(t, cmd, root, 10)
	s3dPosition(t, cmd, leaf, 13)
	if cmd.StructuralRevision() != structural {
		t.Fatal("initial hierarchy repair changed structure")
	}
}
