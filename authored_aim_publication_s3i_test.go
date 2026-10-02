package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type s3iFixture struct {
	app                      *App
	cmd                      *Commands
	root, bone, held, marker EntityId
}

func s3iAimFixture(t *testing.T, attachmentFrame bool) s3iFixture {
	t.Helper()
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10), s3gUnrelated{7})
	bone := s3dChild(cmd, root, 2)
	held := s3dChild(cmd, bone, 3)
	marker := cmd.AddEntity(s3dWorld(-100), s3dLocal(0), Parent{Entity: held},
		AuthoredAssetRefComponent{ItemID: "muzzle", Kind: AuthoredItemKindMarker},
		AuthoredMarkerComponent{Kind: content.AssetMarkerKindMuzzle, Tags: []string{"aim:calibrated"}})
	if attachmentFrame {
		cmd.AddComponents(held, AuthoredAssetAttachmentComponent{AimMarker: marker,
			AimFrame: &content.AssetAttachmentAimFrameDef{MarkerID: "muzzle", Frame: content.AssetTransformDef{
				Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1},
			}}})
	}
	app.FlushCommands()
	s3cComponent[TransformComponent](t, cmd, bone).Pivot = mgl32.Vec3{7, 8, 9}
	TransformHierarchySystem(cmd)
	return s3iFixture{app, cmd, root, bone, held, marker}
}

func s3iForward(t *testing.T, rotation mgl32.Quat, want mgl32.Vec3) {
	t.Helper()
	for _, value := range []float32{rotation.W, rotation.V[0], rotation.V[1], rotation.V[2]} {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatalf("nonfinite rotation: %v", rotation)
		}
	}
	if math.Abs(float64(rotation.Dot(rotation)-1)) > 1e-4 {
		t.Fatalf("non-unit rotation: %v", rotation)
	}
	if got := rotation.Rotate(mgl32.Vec3{0, 0, -1}); got.Sub(want).Len() > 1e-4 {
		t.Fatalf("forward=%v want=%v", got, want)
	}
}

func TestS3iOffsetPublishesLocalBeforeHierarchy(t *testing.T) {
	f := s3iAimFixture(t, false)
	oldLocal := *s3cComponent[LocalTransformComponent](t, f.cmd, f.bone)
	oldParent := *s3cComponent[Parent](t, f.cmd, f.bone)
	worlds := map[EntityId]TransformComponent{}
	for _, id := range []EntityId{f.root, f.bone, f.held, f.marker} {
		worlds[id] = *s3cComponent[TransformComponent](t, f.cmd, id)
	}
	before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
	if !ApplyAuthoredAimOffset(f.cmd, f.root, content.CharacterAimRigDef{}, []EntityId{f.bone}, 90, 0) {
		t.Fatal("valid bone rejected")
	}
	quarter := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
	want := oldLocal
	want.Rotation = quarter
	s3gLocal(t, f.cmd, f.bone, want)
	s3iForward(t, s3cComponent[LocalTransformComponent](t, f.cmd, f.bone).Rotation, mgl32.Vec3{-1, 0, 0})
	if *s3cComponent[Parent](t, f.cmd, f.bone) != oldParent {
		t.Fatal("aim changed Parent")
	}
	for id, world := range worlds {
		if *s3cComponent[TransformComponent](t, f.cmd, id) != world {
			t.Errorf("aim propagated world or changed Pivot for %d", id)
		}
	}
	s3gPublications(t, f.cmd, before, structural, [4]int{0, 1, 0, 0})
	before = s3gRevisions(f.cmd)
	TransformHierarchySystem(f.cmd)
	s3gPublications(t, f.cmd, before, structural, [4]int{1, 0, 0, 0})
	s3dTRS(t, f.cmd, f.bone, mgl32.Vec3{12, 0, 0}, oldLocal.Scale, quarter)
	for _, id := range []EntityId{f.held, f.marker} {
		s3dTRS(t, f.cmd, id, mgl32.Vec3{12, 0, -3}, oldLocal.Scale, quarter)
	}
	if s3cComponent[TransformComponent](t, f.cmd, f.bone).Pivot != worlds[f.bone].Pivot {
		t.Fatal("hierarchy changed bone Pivot")
	}
}

func TestS3iPublicCallersPublishOnlyLocal(t *testing.T) {
	for _, caller := range []string{"offset", "direction", "point", "ray"} {
		for _, limited := range []bool{false, true} {
			name := caller + "/unlimited_marker"
			if limited {
				name = caller + "/limited_attachment_frame"
			}
			t.Run(name, func(t *testing.T) {
				f := s3iAimFixture(t, limited)
				rig := content.CharacterAimRigDef{}
				if caller != "offset" {
					// The sole driving bone's authored weight is normalized by
					// held ancestry; aiming still reaches the target/clamp.
					rig.BoneWeights = []float32{0.25}
				}
				angle := float32(90)
				if limited {
					rig.YawLimitDegrees, rig.PitchLimitDegrees, angle = 45, 45, 45
				}
				world, local := *s3cComponent[TransformComponent](t, f.cmd, f.bone), *s3cComponent[LocalTransformComponent](t, f.cmd, f.bone)
				heldWorld, markerWorld := *s3cComponent[TransformComponent](t, f.cmd, f.held), *s3cComponent[TransformComponent](t, f.cmd, f.marker)
				before, structural := s3hRevisions(f.cmd), f.cmd.StructuralRevision()
				direction := mgl32.Vec3{-1, 0, 0}
				point := markerWorld.Position.Add(direction.Mul(10))
				applied := false
				switch caller {
				case "offset":
					applied = ApplyAuthoredAimOffset(f.cmd, f.root, rig, []EntityId{f.bone}, 90, 0)
				case "direction":
					applied = ApplyAuthoredAimRig(f.cmd, f.root, f.held, rig, []EntityId{f.bone}, direction)
				case "point":
					applied = ApplyAuthoredAimRigAtPoint(f.cmd, f.root, f.held, rig, []EntityId{f.bone}, point)
				case "ray":
					applied = ApplyAuthoredAimRigAtRay(f.cmd, f.root, f.held, rig, []EntityId{f.bone}, point, direction)
				}
				if !applied {
					t.Fatal("valid calibrated public call rejected")
				}
				local.Rotation = mgl32.QuatRotate(mgl32.DegToRad(angle), mgl32.Vec3{0, 1, 0})
				s3gLocal(t, f.cmd, f.bone, local)
				if *s3cComponent[TransformComponent](t, f.cmd, f.bone) != world || *s3cComponent[TransformComponent](t, f.cmd, f.held) != heldWorld || *s3cComponent[TransformComponent](t, f.cmd, f.marker) != markerWorld {
					t.Fatal("public call changed deferred world output")
				}
				s3hPublications(t, f.cmd, before, [5]bool{false, true, false, false, false})
				if f.cmd.StructuralRevision() != structural {
					t.Fatal("aim changed structure")
				}
			})
		}
	}
}

func TestS3iSkippedBonesKeepIndexedWeightsAndLaterPublication(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(0), s3gUnrelated{7})
	first := cmd.AddEntity(s3dLocal(0))
	later := cmd.AddEntity(s3dLocal(0))
	noLocal := cmd.AddEntity(s3dWorld(30))
	app.FlushCommands()
	TransformHierarchySystem(cmd)
	// The first present bone is quiet. Only the later bone can publish; skipped
	// entries must not move its authored weight or create missing Local values.
	bones := []EntityId{0, noLocal, first, EntityId(999999), later, noLocal, 0}
	rig := content.CharacterAimRigDef{BoneWeights: []float32{1, 1, 0, 1, 0.5, 1, 1}}
	before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
	if !ApplyAuthoredAimOffset(cmd, root, rig, bones, 90, 0) {
		t.Fatal("present bones rejected")
	}
	s3gLocal(t, cmd, first, s3dLocal(0))
	want := s3dLocal(0)
	want.Rotation = mgl32.QuatRotate(mgl32.DegToRad(45), mgl32.Vec3{0, 1, 0})
	s3gLocal(t, cmd, later, want)
	s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
	for _, id := range []EntityId{0, noLocal, EntityId(999999)} {
		if cmd.GetComponent(id, reflect.TypeOf(LocalTransformComponent{})) != nil {
			t.Fatalf("created skipped Local for %d", id)
		}
	}
	before = s3gRevisions(cmd)
	if ApplyAuthoredAimOffset(cmd, root, rig, []EntityId{0, noLocal, EntityId(999999)}, 90, 0) {
		t.Fatal("no present Local reported applied")
	}
	s3gPublications(t, cmd, before, structural, [4]int{})
}

func TestS3iNoOpAndZeroRequestsCompareActualDestination(t *testing.T) {
	for _, rotationCase := range []string{"identity", "normalization", "signed_zero"} {
		for _, zeroWeight := range []bool{false, true} {
			if rotationCase == "signed_zero" && zeroWeight {
				continue
			}
			name := rotationCase
			if zeroWeight {
				name += "/zero_weight"
			} else {
				name += "/zero_angle"
			}
			t.Run(name, func(t *testing.T) {
				app := NewApp()
				cmd := app.Commands()
				root := cmd.AddEntity(s3dWorld(0), s3gUnrelated{7})
				local := s3dLocal(2)
				if rotationCase == "normalization" {
					local.Rotation.W = 2
				} else if rotationCase == "signed_zero" {
					local.Rotation.V[0] = math.Float32frombits(0x80000000)
				}
				bone := cmd.AddEntity(local) // No World or Parent: hierarchy cannot mask the writer.
				app.FlushCommands()
				rig, yaw := content.CharacterAimRigDef{}, float32(0)
				if zeroWeight {
					rig.BoneWeights, yaw = []float32{0}, 90
				}
				before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
				old := s3cComponent[LocalTransformComponent](t, cmd, bone)
				oldBits := s3gBits(old.Position, old.Rotation, old.Scale)
				if !ApplyAuthoredAimOffset(cmd, root, rig, []EntityId{bone}, yaw, 0) {
					t.Fatal("existing Local must report applied even when equal")
				}
				want := s3dLocal(2)
				s3gLocal(t, cmd, bone, want)
				got := s3cComponent[LocalTransformComponent](t, cmd, bone)
				bits := s3gBits(got.Position, got.Rotation, got.Scale)
				changed := oldBits != bits
				if changed != (rotationCase != "identity") {
					t.Fatal("zero request did not produce expected actual destination bit change")
				}
				if rotationCase == "signed_zero" && (oldBits[4] != 0x80000000 || bits[4] != 0) {
					t.Fatal("identity quaternion write did not canonicalize selected signed zero")
				}
				change := 0
				if changed {
					change = 1
				}
				s3gPublications(t, cmd, before, structural, [4]int{0, change, 0, 0})
				before = s3gRevisions(cmd)
				if !ApplyAuthoredAimOffset(cmd, root, rig, []EntityId{bone}, yaw, 0) {
					t.Fatal("repeat rejected")
				}
				got = s3cComponent[LocalTransformComponent](t, cmd, bone)
				if s3gBits(got.Position, got.Rotation, got.Scale) != bits {
					t.Fatal("settled repeat changed actual destination bits")
				}
				s3gPublications(t, cmd, before, structural, [4]int{})
			})
		}
	}
	t.Run("root_world_remains_authoritative", func(t *testing.T) {
		app := NewApp()
		cmd := app.Commands()
		world := s3dWorld(10)
		world.Pivot = mgl32.Vec3{7, 8, 9}
		root := cmd.AddEntity(world, s3dLocal(10), s3gUnrelated{7})
		app.FlushCommands()
		TransformHierarchySystem(cmd)
		before, structural := s3gRevisions(cmd), cmd.StructuralRevision()
		if !ApplyAuthoredAimOffset(cmd, root, content.CharacterAimRigDef{}, []EntityId{root}, 90, 0) {
			t.Fatal("root Local rejected as aim bone")
		}
		want := s3dLocal(10)
		want.Rotation = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
		s3gLocal(t, cmd, root, want)
		s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
		before = s3gRevisions(cmd)
		TransformHierarchySystem(cmd)
		s3gLocal(t, cmd, root, s3dLocal(10))
		s3gPublications(t, cmd, before, structural, [4]int{0, 1, 0, 0})
		if *s3cComponent[TransformComponent](t, cmd, root) != world {
			t.Fatal("aim or hierarchy changed authoritative root World/Pivot")
		}
	})
}

func TestS3iLocalOnlyPreservesUntouchedFloatBits(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(0), s3gUnrelated{7})
	local := s3dLocal(0)
	local.Position = mgl32.Vec3{math.Float32frombits(0x7fc01234), math.Float32frombits(0x80000000), 3}
	local.Scale = mgl32.Vec3{1, math.Float32frombits(0x7fc05678), math.Float32frombits(0x80000000)}
	parent := Parent{Entity: EntityId(999999)}
	bone := cmd.AddEntity(local, parent)
	app.FlushCommands()
	structural := cmd.StructuralRevision()
	for _, yaw := range []float32{0, 90} {
		before := s3gRevisions(cmd)
		oldBits := s3gBits(local.Position, local.Rotation, local.Scale)
		if !ApplyAuthoredAimOffset(cmd, root, content.CharacterAimRigDef{}, []EntityId{bone}, yaw, 0) {
			t.Fatal("Local-only bone rejected")
		}
		local = *s3cComponent[LocalTransformComponent](t, cmd, bone)
		bits := s3gBits(local.Position, local.Rotation, local.Scale)
		for _, index := range []int{0, 1, 2, 7, 8, 9} {
			if bits[index] != oldBits[index] {
				t.Fatalf("untouched channel %d changed bits", index)
			}
		}
		change := 0
		if bits != oldBits {
			change = 1
		}
		s3gPublications(t, cmd, before, structural, [4]int{0, change, 0, 0})
		if yaw == 0 && bits != oldBits {
			t.Fatal("identity write changed exact rotation bits")
		}
		forward := mgl32.Vec3{0, 0, -1}
		if yaw == 90 {
			forward = mgl32.Vec3{-1, 0, 0}
		}
		s3iForward(t, local.Rotation, forward)
	}
	if cmd.GetComponent(bone, reflect.TypeOf(TransformComponent{})) != nil || *s3cComponent[Parent](t, cmd, bone) != parent {
		t.Fatal("aim created World or changed unresolved Parent")
	}
}

func TestS3iRejectedPublicArgumentsStayQuiet(t *testing.T) {
	for _, reason := range []string{"nil", "zero_root", "zero_held", "empty_bones", "missing_root_world", "missing_frame", "uncalibrated", "invalid_axes", "zero_direction", "zero_point", "zero_ray"} {
		t.Run(reason, func(t *testing.T) {
			f := s3iAimFixture(t, false)
			cmd, root, held, bones := f.cmd, f.root, f.held, []EntityId{f.bone}
			rig, direction := content.CharacterAimRigDef{}, mgl32.Vec3{-1, 0, 0}
			point := s3cComponent[TransformComponent](t, f.cmd, f.marker).Position.Add(direction)
			switch reason {
			case "nil":
				cmd = nil
			case "zero_root":
				root = 0
			case "zero_held":
				held = 0
			case "empty_bones":
				bones = nil
			case "missing_root_world":
				root = EntityId(999999)
			case "missing_frame":
				rig.MuzzleMarkerKind = "missing"
			case "uncalibrated":
				s3cComponent[AuthoredMarkerComponent](t, f.cmd, f.marker).Tags = nil
			case "invalid_axes":
				rig.ForwardAxis, rig.UpAxis = content.Vec3{0, 1, 0}, content.Vec3{0, 1, 0}
			case "zero_direction":
				direction = mgl32.Vec3{}
			case "zero_point":
				point = s3cComponent[TransformComponent](t, f.cmd, f.marker).Position
			}
			world, local, parent := *s3cComponent[TransformComponent](t, f.cmd, f.bone), *s3cComponent[LocalTransformComponent](t, f.cmd, f.bone), *s3cComponent[Parent](t, f.cmd, f.bone)
			before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
			applied := false
			switch reason {
			case "zero_point":
				applied = ApplyAuthoredAimRigAtPoint(cmd, root, held, rig, bones, point)
			case "zero_ray":
				applied = ApplyAuthoredAimRigAtRay(cmd, root, held, rig, bones, point, mgl32.Vec3{})
			default:
				applied = ApplyAuthoredAimRig(cmd, root, held, rig, bones, direction)
			}
			if applied {
				t.Fatal("invalid public arguments accepted")
			}
			if *s3cComponent[TransformComponent](t, f.cmd, f.bone) != world || *s3cComponent[LocalTransformComponent](t, f.cmd, f.bone) != local || *s3cComponent[Parent](t, f.cmd, f.bone) != parent {
				t.Fatal("rejected call changed bone")
			}
			s3gPublications(t, f.cmd, before, structural, [4]int{})
			// Offset has its own validation entry point.
			if reason == "nil" || reason == "zero_root" || reason == "empty_bones" || reason == "missing_root_world" || reason == "invalid_axes" {
				if ApplyAuthoredAimOffset(cmd, root, rig, bones, 90, 0) {
					t.Fatal("invalid offset accepted")
				}
				s3gPublications(t, f.cmd, before, structural, [4]int{})
			}
		})
	}
}

func TestS3iFalseAfterInitialHierarchyKeepsLegitimatePublications(t *testing.T) {
	f := s3iAimFixture(t, false)
	rootLocal := s3dLocal(999)
	f.cmd.AddComponents(f.root, rootLocal)
	f.app.FlushCommands()
	// Both a root mirror and a child-world repair happen before frame rejection.
	s3cComponent[TransformComponent](t, f.cmd, f.bone).Position = mgl32.Vec3{999, 0, 0}
	boneLocal, parent := *s3cComponent[LocalTransformComponent](t, f.cmd, f.bone), *s3cComponent[Parent](t, f.cmd, f.bone)
	before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
	if ApplyAuthoredAimRig(f.cmd, f.root, f.held, content.CharacterAimRigDef{MuzzleMarkerKind: "missing"}, []EntityId{f.bone}, mgl32.Vec3{-1, 0, 0}) {
		t.Fatal("missing frame accepted")
	}
	s3gPublications(t, f.cmd, before, structural, [4]int{1, 1, 0, 0})
	s3gLocal(t, f.cmd, f.root, s3dLocal(10))
	s3dTRS(t, f.cmd, f.bone, mgl32.Vec3{12, 0, 0}, mgl32.Vec3{1, 1, 1}, mgl32.QuatIdent())
	if *s3cComponent[LocalTransformComponent](t, f.cmd, f.bone) != boneLocal || *s3cComponent[Parent](t, f.cmd, f.bone) != parent {
		t.Fatal("false call changed aim inputs")
	}
}

func TestS3iBufferedCommandsRemainPendingOnSuccessAndFalse(t *testing.T) {
	for _, scenario := range []string{"false", "local_removal", "local_replacement", "entity_removal"} {
		t.Run(scenario, func(t *testing.T) {
			success := scenario != "false"
			f := s3iAimFixture(t, false)
			missingLocal := f.cmd.AddEntity(s3gUnrelated{11})
			removed := f.cmd.AddEntity(s3gUnrelated{12}, s3dLocal(8))
			replaced := f.cmd.AddEntity(s3dLocal(6))
			f.app.FlushCommands()
			TransformHierarchySystem(f.cmd)
			pending := f.cmd.AddEntity(s3gUnrelated{13}, s3dLocal(4))
			f.cmd.AddComponents(missingLocal, s3dLocal(5))
			f.cmd.RemoveComponents(f.bone, LocalTransformComponent{})
			f.cmd.AddComponents(f.root, s3gUnrelated{70})
			f.cmd.AddComponents(replaced, s3dLocal(7))
			f.cmd.RemoveEntity(removed)
			before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
			oldLocals := map[EntityId]LocalTransformComponent{}
			for _, id := range []EntityId{f.bone, replaced, removed} {
				oldLocals[id] = *s3cComponent[LocalTransformComponent](t, f.cmd, id)
			}
			// Only one committed Local changes per success case, so the
			// aggregate publication cannot hide an omitted boundary writer.
			bones := []EntityId{missingLocal, f.bone, pending, replaced, removed}
			weights := []float32{0, 0, 0, 0, 0}
			changedTarget := EntityId(0)
			switch scenario {
			case "local_removal":
				weights[1], changedTarget = 1, f.bone
			case "local_replacement":
				weights[3], changedTarget = 1, replaced
			case "entity_removal":
				weights[4], changedTarget = 1, removed
			}
			root := f.root
			if !success {
				root = EntityId(999999)
			}
			if got := ApplyAuthoredAimOffset(f.cmd, root, content.CharacterAimRigDef{BoneWeights: weights}, bones, 90, 0); got != success {
				t.Fatalf("applied=%v want=%v", got, success)
			}
			change := 0
			if success {
				change = 1
			}
			s3gPublications(t, f.cmd, before, structural, [4]int{0, change, 0, 0})
			if f.cmd.GetComponent(missingLocal, reflect.TypeOf(LocalTransformComponent{})) != nil || f.cmd.GetComponent(pending, reflect.TypeOf(LocalTransformComponent{})) != nil {
				t.Fatal("pending Local became visible")
			}
			if s3cComponent[s3gUnrelated](t, f.cmd, f.root).Value != 7 || s3cComponent[s3gUnrelated](t, f.cmd, removed).Value != 12 {
				t.Fatal("queued replacement/removal flushed")
			}
			for id, old := range oldLocals {
				want := old
				if id == changedTarget {
					want.Rotation = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
				}
				s3gLocal(t, f.cmd, id, want)
				got := s3cComponent[LocalTransformComponent](t, f.cmd, id)
				changed := s3gBits(got.Position, got.Rotation, got.Scale) != s3gBits(old.Position, old.Rotation, old.Scale)
				if changed != (id == changedTarget) {
					t.Fatalf("pending target %d changed=%v, expected only %d", id, changed, changedTarget)
				}
				if id == changedTarget {
					s3iForward(t, got.Rotation, mgl32.Vec3{-1, 0, 0})
				}
			}
			f.app.FlushCommands()
			if f.cmd.StructuralRevision() <= structural {
				t.Fatal("queued commands were lost")
			}
			if f.cmd.GetComponent(f.bone, reflect.TypeOf(LocalTransformComponent{})) != nil || f.cmd.GetComponent(removed, reflect.TypeOf(LocalTransformComponent{})) != nil || f.cmd.GetComponent(removed, reflect.TypeOf(s3gUnrelated{})) != nil {
				t.Fatal("queued removal lost")
			}
			s3gLocal(t, f.cmd, missingLocal, s3dLocal(5))
			s3gLocal(t, f.cmd, pending, s3dLocal(4))
			s3gLocal(t, f.cmd, replaced, s3dLocal(7))
			if s3cComponent[s3gUnrelated](t, f.cmd, f.root).Value != 70 || s3cComponent[s3gUnrelated](t, f.cmd, pending).Value != 13 {
				t.Fatal("queued addition/replacement lost")
			}
		})
	}
}
