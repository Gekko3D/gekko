package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type s3jFixture struct {
	app          *App
	cmd          *Commands
	root, target EntityId
}

func s3jNew(t *testing.T, live LocalTransformComponent) s3jFixture {
	t.Helper()
	app := NewApp()
	cmd := app.Commands()
	root := cmd.AddEntity(s3dWorld(10), s3gUnrelated{7}, AuthoredAssetRootComponent{AssetID: "asset"},
		AnimationPlayerComponent{ClipID: "base", Time: 0.25, Speed: 1, Playing: true},
		AuthoredAssetAnimationSetComponent{DefaultClipID: "base", Clips: map[string]content.AssetAnimationClipDef{
			"base": {ID: "base", Duration: 2},
		}, BindTransforms: map[string]LocalTransformComponent{"bone": s3dLocal(0)}})
	world := s3dWorld(40)
	world.Pivot = mgl32.Vec3{7, 8, 9}
	target := cmd.AddEntity(world, live, Parent{Entity: root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "bone"})
	app.FlushCommands()
	return s3jFixture{app: app, cmd: cmd, root: root, target: target}
}

func s3jPosition(id string, x float32) content.AssetAnimationTrackDef {
	return content.AssetAnimationTrackDef{TargetID: id, PositionKeys: []content.AssetVec3KeyDef{{Value: content.Vec3{x, 0, 0}}}}
}

func s3jClip(tracks ...content.AssetAnimationTrackDef) content.AssetAnimationClipDef {
	return content.AssetAnimationClipDef{Duration: 2, Tracks: tracks}
}

func s3jSample(t *testing.T, f s3jFixture, caller string, dt float64) {
	t.Helper()
	if caller == "public" {
		if !SampleAuthoredAssetAnimation(f.cmd, f.root) {
			t.Fatal("eligible authored asset rejected")
		}
	} else {
		assetAnimationSystem(&Time{Dt: dt}, f.cmd)
	}
}

func s3jBits(local LocalTransformComponent) [10]uint32 {
	return s3gBits(local.Position, local.Rotation, local.Scale)
}

// Capture committed values and publication sequences independently of the sampler.
// Each fixture has only one selected destination that may change.
func s3jCheckSample(t *testing.T, f s3jFixture, caller string, dt float64, want LocalTransformComponent, changed bool) {
	t.Helper()
	old := *s3cComponent[LocalTransformComponent](t, f.cmd, f.target)
	world := *s3cComponent[TransformComponent](t, f.cmd, f.target)
	rootWorld := *s3cComponent[TransformComponent](t, f.cmd, f.root)
	parent := *s3cComponent[Parent](t, f.cmd, f.target)
	unrelated := *s3cComponent[s3gUnrelated](t, f.cmd, f.root)
	before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
	s3jSample(t, f, caller, dt)
	s3gLocal(t, f.cmd, f.target, want)
	got := *s3cComponent[LocalTransformComponent](t, f.cmd, f.target)
	if actual := s3jBits(old) != s3jBits(got); actual != changed {
		t.Fatalf("actual destination bits changed=%v want=%v: before=%v after=%v", actual, changed, s3jBits(old), s3jBits(got))
	}
	if *s3cComponent[TransformComponent](t, f.cmd, f.target) != world || *s3cComponent[TransformComponent](t, f.cmd, f.root) != rootWorld {
		t.Fatal("sampler changed deferred World or Pivot")
	}
	if *s3cComponent[Parent](t, f.cmd, f.target) != parent || *s3cComponent[s3gUnrelated](t, f.cmd, f.root) != unrelated {
		t.Fatal("sampler changed Parent or unrelated data")
	}
	publication := 0
	if changed {
		publication = 1
	}
	s3gPublications(t, f.cmd, before, structural, [4]int{0, publication, 0, 0})
}

func TestS3jEntryPointsPublishLocalBeforeHierarchy(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		t.Run(caller, func(t *testing.T) {
			f := s3jNew(t, s3dLocal(0))
			set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
			set.Clips["base"] = s3jClip(content.AssetAnimationTrackDef{TargetID: "bone", PositionKeys: []content.AssetVec3KeyDef{
				{Time: 0, Value: content.Vec3{}}, {Time: 1, Value: content.Vec3{8, 0, 0}},
			}})
			quarter := mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
			set.Clips["overlay"] = s3jClip(content.AssetAnimationTrackDef{TargetID: "bone",
				RotationKeys: []content.AssetQuatKeyDef{{Value: content.Quat{quarter.V[0], quarter.V[1], quarter.V[2], quarter.W}}},
				ScaleKeys:    []content.AssetVec3KeyDef{{Value: content.Vec3{2, 3, 4}}},
			})
			player := s3cComponent[AnimationPlayerComponent](t, f.cmd, f.root)
			player.Layers = []AnimationLayer{{ClipID: "overlay", Time: 0.25, Weight: 1}}
			wantTime, wantX := float32(0.25), float32(2)
			if caller == "system" {
				wantTime, wantX = 0.5, 4
			}
			want := LocalTransformComponent{Position: mgl32.Vec3{wantX, 0, 0}, Rotation: quarter, Scale: mgl32.Vec3{2, 3, 4}}
			s3jCheckSample(t, f, caller, 0.25, want, true)
			if player.Time != wantTime || len(player.Layers) != 1 || player.Layers[0].Time != wantTime {
				t.Fatalf("playback times differ: player=%g layers=%+v want=%g", player.Time, player.Layers, wantTime)
			}
			// World propagation is owned by the later hierarchy pass.
			before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
			TransformHierarchySystem(f.cmd)
			s3dTRS(t, f.cmd, f.target, mgl32.Vec3{10 + wantX, 0, 0}, want.Scale, want.Rotation)
			s3gPublications(t, f.cmd, before, structural, [4]int{1, 0, 0, 0})
			if s3cComponent[TransformComponent](t, f.cmd, f.target).Pivot != (mgl32.Vec3{7, 8, 9}) {
				t.Fatal("hierarchy changed Pivot")
			}
		})
	}
}

func TestS3jCompletePoseComparisonIgnoresIntermediateWrites(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		for _, pose := range []string{"base_restores", "override_restores", "ordered_additive_restores", "base_changes", "overlay_changes"} {
			t.Run(caller+"/"+pose, func(t *testing.T) {
				f := s3jNew(t, s3dLocal(9))
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				player := s3cComponent[AnimationPlayerComponent](t, f.cmd, f.root)
				set.Clips["base"] = s3jClip(s3jPosition("bone", 3))
				set.Clips["override"] = s3jClip(s3jPosition("bone", 9))
				set.Clips["add"] = s3jClip(s3jPosition("bone", 4))
				wantX := float32(9)
				switch pose {
				case "base_restores":
					set.Clips["base"] = s3jClip(s3jPosition("bone", 9))
				case "override_restores":
					player.Layers = []AnimationLayer{{ClipID: "override", Weight: 1, Paused: true}}
				case "ordered_additive_restores":
					set.Clips["override"] = s3jClip(s3jPosition("bone", 5))
					player.Layers = []AnimationLayer{{ClipID: "override", Weight: 1}, {ClipID: "add", Weight: 1, Mode: AnimationLayerAdditive}}
				case "base_changes":
					wantX = 3
				case "overlay_changes":
					set.Clips["override"] = s3jClip(s3jPosition("bone", 11))
					player.Layers = []AnimationLayer{{ClipID: "override", Weight: 1}}
					wantX = 11
				}
				s3jCheckSample(t, f, caller, 0, s3dLocal(wantX), wantX != 9)
				settled := *s3cComponent[LocalTransformComponent](t, f.cmd, f.target)
				s3jCheckSample(t, f, caller, 0, settled, false)
			})
		}
	}
}

func TestS3jBlendMasksAndRootPositionLock(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		for _, path := range []string{"base_blend", "layer_blend", "masked_out", "masked_in", "base_root_lock", "layer_root_lock", "nested_position_unlocked"} {
			t.Run(caller+"/"+path, func(t *testing.T) {
				f := s3jNew(t, s3dLocal(0))
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				player := s3cComponent[AnimationPlayerComponent](t, f.cmd, f.root)
				blend := content.AssetAnimationClipDef{Duration: 2, Blend1D: &content.AssetAnimationBlend1DDef{Default: 50,
					Samples: []content.AssetAnimationBlendSampleDef{{Value: 0, Tracks: []content.AssetAnimationTrackDef{s3jPosition("bone", 0)}}, {Value: 100, Tracks: []content.AssetAnimationTrackDef{s3jPosition("bone", 10)}}},
				}}
				want := s3dLocal(0)
				switch path {
				case "base_blend":
					set.Clips["base"] = blend
					player.BlendValue, player.HasBlendValue, want.Position[0] = 25, true, 2.5
				case "layer_blend":
					set.Clips["blend"] = blend
					player.Layers = []AnimationLayer{{ClipID: "blend", Weight: 0.5, BlendValue: 50, HasBlendValue: true}}
					want.Position[0] = 2.5
				case "masked_out", "masked_in":
					set.Clips["overlay"] = s3jClip(s3jPosition("bone", 6))
					mask := "other"
					if path == "masked_in" {
						mask, want.Position[0] = "bone", 6
					}
					player.Layers = []AnimationLayer{{ClipID: "overlay", Weight: 1, BoneMask: []string{mask}}}
				case "base_root_lock", "layer_root_lock", "nested_position_unlocked":
					track := s3jPosition("bone", 6)
					track.ScaleKeys = []content.AssetVec3KeyDef{{Value: content.Vec3{2, 2, 2}}}
					if path == "layer_root_lock" {
						set.Clips["overlay"] = s3jClip(track)
						player.Layers = []AnimationLayer{{ClipID: "overlay", Weight: 1, RootMotionPolicy: AnimationRootMotionLocked}}
					} else {
						set.Clips["base"] = s3jClip(track)
						player.RootMotionPolicy = AnimationRootMotionLocked
					}
					want.Scale = mgl32.Vec3{2, 2, 2}
					if path == "nested_position_unlocked" {
						middle := f.cmd.AddEntity(Parent{Entity: f.root})
						f.cmd.AddComponents(f.target, Parent{Entity: middle})
						f.app.FlushCommands()
						want.Position[0] = 6
					}
				}
				s3jCheckSample(t, f, caller, 0, want, path != "masked_out")
				s3jCheckSample(t, f, caller, 0, *s3cComponent[LocalTransformComponent](t, f.cmd, f.target), false)
			})
		}
	}
}

func TestS3jBindOmittedChannelsAndLayerRetirement(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		for _, path := range []string{"bind_only", "omitted_channels", "unbound_live_pose", "removed_layer", "zero_weight", "expired_layer", "paused_layer"} {
			if caller == "public" && path == "expired_layer" {
				continue // Public sampling deliberately does not advance a layer.
			}
			t.Run(caller+"/"+path, func(t *testing.T) {
				live := s3dLocal(9)
				live.Scale = mgl32.Vec3{3, 4, 5}
				f := s3jNew(t, live)
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				player := s3cComponent[AnimationPlayerComponent](t, f.cmd, f.root)
				want, dt := s3dLocal(0), float64(0)
				switch path {
				case "omitted_channels":
					set.Clips["base"] = s3jClip(s3jPosition("bone", 2))
					want.Position[0] = 2
				case "unbound_live_pose":
					delete(set.BindTransforms, "bone")
					set.Clips["base"] = s3jClip(s3jPosition("bone", 2))
					want = live
					want.Position[0] = 2
				case "removed_layer", "zero_weight", "expired_layer", "paused_layer":
					set.Clips["base"] = s3jClip(s3jPosition("bone", 2))
					set.Clips["overlay"] = s3jClip(s3jPosition("bone", 9))
					player.Layers = []AnimationLayer{{ClipID: "overlay", Time: 1.9, Weight: 1}}
					s3jCheckSample(t, f, caller, 0, s3dLocal(9), true)
					want.Position[0] = 2
					switch path {
					case "removed_layer":
						player.Layers = nil
					case "zero_weight":
						player.Layers[0].Weight = 0
					case "expired_layer":
						dt = 0.2
					case "paused_layer":
						player.Playing, player.Layers[0].Paused = false, true
						want, dt = s3dLocal(9), 0.2
					}
				}
				changed := path != "paused_layer"
				s3jCheckSample(t, f, caller, dt, want, changed)
				if path == "expired_layer" || path == "zero_weight" || path == "removed_layer" {
					if len(player.Layers) != 0 {
						t.Fatalf("retired layer retained: %+v", player.Layers)
					}
				}
				if path == "paused_layer" && (player.Time != 0.25 || player.Layers[0].Time != 1.9) {
					t.Fatal("paused playback advanced")
				}
				s3jCheckSample(t, f, caller, 0, *s3cComponent[LocalTransformComponent](t, f.cmd, f.target), false)
			})
		}
	}
}

func TestS3jExactBitsPreserveUntouchedNaNsAndPublishSignedZero(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		for _, channel := range []string{"rotation", "position", "bind_signed_zero"} {
			t.Run(caller+"/"+channel, func(t *testing.T) {
				live := s3dLocal(0)
				live.Scale[1] = math.Float32frombits(0x7fc05678)
				if channel == "rotation" {
					live.Position[0] = math.Float32frombits(0x7fc01234)
					live.Position[1] = math.Float32frombits(0x80000000)
				} else if channel == "bind_signed_zero" {
					live = s3dLocal(0)
					live.Position[0] = math.Float32frombits(0x80000000)
				}
				f := s3jNew(t, live)
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				if channel == "position" {
					delete(set.BindTransforms, "bone")
				} else if channel == "rotation" {
					set.BindTransforms["bone"] = live
				}
				for _, step := range []string{"equal", "changed", "repeat"} {
					want := live
					if channel == "rotation" {
						rotation := mgl32.QuatIdent()
						if step != "equal" {
							rotation = mgl32.QuatRotate(mgl32.DegToRad(90), mgl32.Vec3{0, 1, 0})
						}
						set.Clips["base"] = s3jClip(content.AssetAnimationTrackDef{TargetID: "bone", RotationKeys: []content.AssetQuatKeyDef{{Value: content.Quat{rotation.V[0], rotation.V[1], rotation.V[2], rotation.W}}}})
						want.Rotation = rotation
					} else if channel == "position" {
						x := float32(0)
						if step != "equal" {
							x = 4
						}
						set.Clips["base"] = s3jClip(s3jPosition("bone", x))
						want.Position[0] = x
					} else {
						want = s3dLocal(0)
					}
					oldBits := s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, f.target))
					before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
					s3jSample(t, f, caller, 0)
					got := *s3cComponent[LocalTransformComponent](t, f.cmd, f.target)
					bits, wantBits := s3jBits(got), s3jBits(want)
					if channel == "rotation" && step != "equal" {
						// Quaternion sampling normalizes and interpolates. Check its
						// actual finite direction below, and the untouched TRS bits here.
						for index := 3; index < 7; index++ {
							wantBits[index] = bits[index]
						}
					}
					if bits != wantBits {
						t.Fatalf("%s exact TRS bits=%v want=%v", step, bits, wantBits)
					}
					changed := step == "changed"
					if channel == "bind_signed_zero" {
						changed = step == "equal"
					}
					if (bits != oldBits) != changed {
						t.Fatalf("%s actual TRS change mismatch", step)
					}
					if channel == "bind_signed_zero" && step == "equal" && (oldBits[0] != 0x80000000 || bits[0] != 0) {
						t.Fatal("bind reset did not replace negative zero with positive zero")
					}
					forward := mgl32.Vec3{0, 0, -1}
					if channel == "rotation" && step != "equal" {
						forward = mgl32.Vec3{-1, 0, 0}
					}
					s3iForward(t, got.Rotation, forward) // Finite and unit even in intentional NaN TRS fixtures.
					publication := 0
					if changed {
						publication = 1
					}
					s3gPublications(t, f.cmd, before, structural, [4]int{0, publication, 0, 0})
				}
			})
		}
	}
}

func TestS3jSelectionIsolationAndDuplicateItemIDs(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		t.Run(caller+"/selection", func(t *testing.T) {
			f := s3jNew(t, s3dLocal(1))
			set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
			set.Clips["base"] = s3jClip(s3jPosition("bone", 7))
			// The matching descendant needs Local, but neither World nor a direct parent link.
			middle := f.cmd.AddEntity(Parent{Entity: f.root})
			f.cmd.AddComponents(f.target, Parent{Entity: middle})
			f.cmd.RemoveComponents(f.target, TransformComponent{})
			otherRoot := f.cmd.AddEntity(AuthoredAssetRootComponent{AssetID: "asset"})
			foreign := f.cmd.AddEntity(s3dLocal(1), Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "foreign", ItemID: "bone"})
			outside := f.cmd.AddEntity(s3dLocal(1), AuthoredAssetRefComponent{AssetID: "asset", ItemID: "bone"})
			other := f.cmd.AddEntity(s3dLocal(1), Parent{Entity: otherRoot}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "bone"})
			missing := f.cmd.AddEntity(s3dWorld(30), Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "bone"})
			// Ordinary acyclic root ancestry does not select the root itself.
			f.cmd.AddComponents(f.root, s3dLocal(1), AuthoredAssetRefComponent{AssetID: "asset", ItemID: "bone"})
			f.app.FlushCommands()
			before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
			parent := *s3cComponent[Parent](t, f.cmd, f.target)
			oldBits := s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, f.target))
			s3jSample(t, f, caller, 0)
			s3gLocal(t, f.cmd, f.target, s3dLocal(7))
			if s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, f.target)) == oldBits {
				t.Fatal("selected descendant did not change actual pose")
			}
			for _, id := range []EntityId{f.root, foreign, outside, other} {
				local := *s3cComponent[LocalTransformComponent](t, f.cmd, id)
				if s3jBits(local) != s3jBits(s3dLocal(1)) {
					t.Fatalf("excluded target %d changed", id)
				}
			}
			if f.cmd.GetComponent(missing, reflect.TypeOf(LocalTransformComponent{})) != nil || f.cmd.GetComponent(f.target, reflect.TypeOf(TransformComponent{})) != nil {
				t.Fatal("sampler created missing Local or World")
			}
			if *s3cComponent[Parent](t, f.cmd, f.target) != parent {
				t.Fatal("sampler changed selected ancestry")
			}
			s3gPublications(t, f.cmd, before, structural, [4]int{0, 1, 0, 0})
		})
		t.Run(caller+"/duplicate", func(t *testing.T) {
			f := s3jNew(t, s3dLocal(1))
			second := f.cmd.AddEntity(s3dWorld(50), s3dLocal(2), Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "bone"})
			f.app.FlushCommands()
			set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
			set.Clips["base"] = s3jClip(s3jPosition("bone", 7))
			before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
			s3jSample(t, f, caller, 0)
			firstBits := s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, f.target))
			secondBits := s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, second))
			firstChanged, secondChanged := firstBits != s3jBits(s3dLocal(1)), secondBits != s3jBits(s3dLocal(2))
			if firstChanged == secondChanged {
				t.Fatal("duplicate ItemID must change exactly the existing query winner")
			}
			chosen := second
			if firstChanged {
				chosen = f.target
			}
			s3gLocal(t, f.cmd, chosen, s3dLocal(7))
			s3gPublications(t, f.cmd, before, structural, [4]int{0, 1, 0, 0})
		})
		for _, changedItem := range []string{"bone", "hand", "foot"} {
			t.Run(caller+"/distinct_ids/"+changedItem, func(t *testing.T) {
				f := s3jNew(t, s3dLocal(1))
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				items := []string{"bone", "hand", "foot"}
				targets := []EntityId{f.target,
					f.cmd.AddEntity(s3dWorld(50), s3dLocal(2), Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "hand"}),
					f.cmd.AddEntity(s3dWorld(60), s3dLocal(3), Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "foot"}),
				}
				var tracks []content.AssetAnimationTrackDef
				wants := make([]LocalTransformComponent, len(items))
				for i, item := range items {
					set.BindTransforms[item] = s3dLocal(0)
					wants[i] = s3dLocal(float32(i + 1))
					if item == changedItem {
						wants[i].Position[0] += 10
					}
					tracks = append(tracks, s3jPosition(item, wants[i].Position[0]))
				}
				set.Clips["base"] = s3jClip(tracks...)
				f.app.FlushCommands()
				worlds, parents := make([]TransformComponent, len(items)), make([]Parent, len(items))
				for i, target := range targets {
					worlds[i] = *s3cComponent[TransformComponent](t, f.cmd, target)
					parents[i] = *s3cComponent[Parent](t, f.cmd, target)
				}
				for _, repeat := range []bool{false, true} {
					oldBits := make([][10]uint32, len(items))
					for i, target := range targets {
						oldBits[i] = s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, target))
					}
					before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
					s3jSample(t, f, caller, 0)
					for i, target := range targets {
						s3gLocal(t, f.cmd, target, wants[i])
						bits := s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, target))
						if bits != s3jBits(wants[i]) || (bits != oldBits[i]) != (!repeat && items[i] == changedItem) {
							t.Fatalf("selected %s actual bits or sole destination change mismatch", items[i])
						}
						if *s3cComponent[TransformComponent](t, f.cmd, target) != worlds[i] || *s3cComponent[Parent](t, f.cmd, target) != parents[i] {
							t.Fatalf("selected %s changed deferred World/Pivot or Parent", items[i])
						}
					}
					publication := 1
					if repeat {
						publication = 0
					}
					s3gPublications(t, f.cmd, before, structural, [4]int{0, publication, 0, 0})
				}
			})
		}
	}
}

func TestS3jPublicAcceptanceAndMissingBaseSystemSkip(t *testing.T) {
	for _, reason := range []string{"nil", "zero_root", "missing_root", "missing_player", "missing_set", "missing_ref", "empty_clips"} {
		t.Run("rejected/"+reason, func(t *testing.T) {
			f := s3jNew(t, s3dLocal(9))
			cmd, root := f.cmd, f.root
			switch reason {
			case "nil":
				cmd = nil
			case "zero_root":
				root = 0
			case "missing_root":
				root = EntityId(999999)
			case "missing_player":
				f.cmd.RemoveComponents(root, AnimationPlayerComponent{})
			case "missing_set":
				f.cmd.RemoveComponents(root, AuthoredAssetAnimationSetComponent{})
			case "missing_ref":
				f.cmd.RemoveComponents(root, AuthoredAssetRootComponent{})
			case "empty_clips":
				s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, root).Clips = nil
			}
			f.app.FlushCommands()
			before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
			old := *s3cComponent[LocalTransformComponent](t, f.cmd, f.target)
			if SampleAuthoredAssetAnimation(cmd, root) {
				t.Fatal("ineligible public request accepted")
			}
			if s3jBits(*s3cComponent[LocalTransformComponent](t, f.cmd, f.target)) != s3jBits(old) {
				t.Fatal("rejected request changed actual pose")
			}
			s3gPublications(t, f.cmd, before, structural, [4]int{})
		})
	}
	t.Run("eligible_without_targets", func(t *testing.T) {
		f := s3jNew(t, s3dLocal(9))
		f.cmd.RemoveComponents(f.target, AuthoredAssetRefComponent{})
		f.app.FlushCommands()
		before, structural := s3gRevisions(f.cmd), f.cmd.StructuralRevision()
		if !SampleAuthoredAssetAnimation(f.cmd, f.root) {
			t.Fatal("eligible nonempty animation set without targets rejected")
		}
		s3gLocal(t, f.cmd, f.target, s3dLocal(9))
		s3gPublications(t, f.cmd, before, structural, [4]int{})
	})
	for _, caller := range []string{"public", "system"} {
		for _, layers := range []bool{false, true} {
			name := caller + "/missing_base_bind"
			if layers {
				name = caller + "/missing_base_layer"
			}
			t.Run(name, func(t *testing.T) {
				f := s3jNew(t, s3dLocal(9))
				player := s3cComponent[AnimationPlayerComponent](t, f.cmd, f.root)
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				player.ClipID = "absent"
				set.BindTransforms["bone"] = s3dLocal(2)
				set.Clips["overlay"] = s3jClip(s3jPosition("bone", 4))
				if layers {
					player.Layers = []AnimationLayer{{ClipID: "overlay", Time: 0.5, Weight: 1}}
				}
				wantX := float32(9)
				if caller == "public" {
					wantX = 2
					if layers {
						wantX = 4
					}
				}
				s3jCheckSample(t, f, caller, 0.25, s3dLocal(wantX), caller == "public")
				if player.Time != 0.25 || (layers && player.Layers[0].Time != 0.5) {
					t.Fatal("missing-base public sampling or system skip advanced playback")
				}
			})
		}
	}
}

func TestS3jBufferedTargetsStayCommittedUntilFlush(t *testing.T) {
	for _, caller := range []string{"public", "system"} {
		for _, scenario := range []string{"local_removal", "local_replacement", "entity_removal", "rejected"} {
			if caller == "system" && scenario == "rejected" {
				continue
			}
			t.Run(caller+"/"+scenario, func(t *testing.T) {
				f := s3jNew(t, s3dLocal(3))
				set := s3cComponent[AuthoredAssetAnimationSetComponent](t, f.cmd, f.root)
				set.Clips["base"] = s3jClip(s3jPosition("bone", 7), s3jPosition("added", 7), s3jPosition("pending", 7))
				missing := f.cmd.AddEntity(Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "added"})
				f.app.FlushCommands()
				pending := f.cmd.AddEntity(s3dLocal(4), Parent{Entity: f.root}, AuthoredAssetRefComponent{AssetID: "asset", ItemID: "pending"})
				f.cmd.AddComponents(missing, s3dLocal(5))
				f.cmd.AddComponents(f.root, s3gUnrelated{70})
				switch scenario {
				case "local_replacement":
					f.cmd.AddComponents(f.target, s3dLocal(8))
				case "entity_removal":
					f.cmd.RemoveEntity(f.target)
				default:
					f.cmd.RemoveComponents(f.target, LocalTransformComponent{})
				}
				structural := f.cmd.StructuralRevision()
				if scenario == "rejected" {
					before := s3gRevisions(f.cmd)
					if SampleAuthoredAssetAnimation(f.cmd, 0) {
						t.Fatal("zero root accepted with queued work")
					}
					s3gLocal(t, f.cmd, f.target, s3dLocal(3))
					s3gPublications(t, f.cmd, before, structural, [4]int{})
				} else {
					s3jCheckSample(t, f, caller, 0, s3dLocal(7), true)
				}
				if f.cmd.GetComponent(missing, reflect.TypeOf(LocalTransformComponent{})) != nil || f.cmd.GetComponent(pending, reflect.TypeOf(LocalTransformComponent{})) != nil {
					t.Fatal("queued Local addition or new entity became visible during sampling")
				}
				if s3cComponent[s3gUnrelated](t, f.cmd, f.root).Value != 7 {
					t.Fatal("queued unrelated replacement flushed during sampling")
				}
				f.app.FlushCommands()
				if f.cmd.StructuralRevision() <= structural {
					t.Fatal("queued mutations were lost")
				}
				s3gLocal(t, f.cmd, missing, s3dLocal(5))
				s3gLocal(t, f.cmd, pending, s3dLocal(4))
				if s3cComponent[s3gUnrelated](t, f.cmd, f.root).Value != 70 {
					t.Fatal("queued unrelated replacement was lost")
				}
				if scenario == "local_replacement" {
					s3gLocal(t, f.cmd, f.target, s3dLocal(8))
				} else if f.cmd.GetComponent(f.target, reflect.TypeOf(LocalTransformComponent{})) != nil {
					t.Fatal("queued Local or entity removal was lost")
				}
				if scenario == "entity_removal" && f.cmd.GetComponent(f.target, reflect.TypeOf(AuthoredAssetRefComponent{})) != nil {
					t.Fatal("queued entity removal did not remove authored reference")
				}
			})
		}
	}
}
