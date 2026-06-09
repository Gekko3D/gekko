package gekko

import (
	"math"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type AnimationModule struct{}

func (AnimationModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(assetAnimationSystem).
			InStage(Update).
			RunAlways(),
	)
}

type AnimationPlayerComponent struct {
	ClipID  string
	Time    float32
	Speed   float32
	Playing bool
	Loop    bool
}

type AuthoredAssetAnimationSetComponent struct {
	DefaultClipID  string
	Clips          map[string]content.AssetAnimationClipDef
	BindTransforms map[string]LocalTransformComponent
}

func assetAnimationSystem(time *Time, cmd *Commands) {
	dt := float32(0)
	if time != nil {
		dt = float32(time.Dt)
	}

	parentByEntity := animationParentIndex(cmd)
	MakeQuery3[AuthoredAssetRootComponent, AnimationPlayerComponent, AuthoredAssetAnimationSetComponent](cmd).
		Map(func(root EntityId, rootRef *AuthoredAssetRootComponent, player *AnimationPlayerComponent, animationSet *AuthoredAssetAnimationSetComponent) bool {
			if animationSet == nil || len(animationSet.Clips) == 0 {
				return true
			}
			if player.ClipID == "" {
				player.ClipID = animationSet.DefaultClipID
			}
			clip, ok := animationSet.Clips[player.ClipID]
			if !ok {
				return true
			}

			advanceAnimationPlayer(player, clip, dt)
			targets := animationTargetsForRoot(cmd, parentByEntity, root, rootRef.AssetID)
			applyAnimationClip(clip, player.Time, animationSet.BindTransforms, targets)
			return true
		})
}

func animationParentIndex(cmd *Commands) map[EntityId]EntityId {
	parentByEntity := make(map[EntityId]EntityId)
	MakeQuery1[Parent](cmd).Map(func(eid EntityId, parent *Parent) bool {
		parentByEntity[eid] = parent.Entity
		return true
	})
	return parentByEntity
}

func animationTargetsForRoot(cmd *Commands, parentByEntity map[EntityId]EntityId, root EntityId, assetID string) map[string]*LocalTransformComponent {
	targets := make(map[string]*LocalTransformComponent)
	MakeQuery2[AuthoredAssetRefComponent, LocalTransformComponent](cmd).Map(func(eid EntityId, ref *AuthoredAssetRefComponent, local *LocalTransformComponent) bool {
		if ref.AssetID != assetID || !animationEntityDescendsFrom(parentByEntity, eid, root) {
			return true
		}
		targets[ref.ItemID] = local
		return true
	})
	return targets
}

func animationEntityDescendsFrom(parentByEntity map[EntityId]EntityId, entity EntityId, root EntityId) bool {
	for i := 0; i < 64; i++ {
		parent, ok := parentByEntity[entity]
		if !ok {
			return false
		}
		if parent == root {
			return true
		}
		entity = parent
	}
	return false
}

func advanceAnimationPlayer(player *AnimationPlayerComponent, clip content.AssetAnimationClipDef, dt float32) {
	if player == nil || !player.Playing || dt == 0 {
		return
	}
	speed := player.Speed
	if speed == 0 {
		speed = 1
	}
	player.Time += dt * speed
	if clip.Duration <= 0 {
		return
	}
	if player.Loop {
		player.Time = positiveMod(player.Time, clip.Duration)
		return
	}
	if player.Time > clip.Duration {
		player.Time = clip.Duration
		player.Playing = false
	} else if player.Time < 0 {
		player.Time = 0
		player.Playing = false
	}
}

func positiveMod(value float32, divisor float32) float32 {
	if divisor <= 0 {
		return value
	}
	result := float32(math.Mod(float64(value), float64(divisor)))
	if result < 0 {
		result += divisor
	}
	return result
}

func applyAnimationClip(clip content.AssetAnimationClipDef, sampleTime float32, bindTransforms map[string]LocalTransformComponent, targets map[string]*LocalTransformComponent) {
	for _, track := range clip.Tracks {
		target, ok := targets[track.TargetID]
		if !ok {
			continue
		}
		bind := *target
		if authoredBind, ok := bindTransforms[track.TargetID]; ok {
			bind = authoredBind
		}
		target.Position = bind.Position
		target.Rotation = bind.Rotation
		target.Scale = bind.Scale

		if len(track.PositionKeys) > 0 {
			target.Position = sampleVec3Keys(track.PositionKeys, sampleTime)
		}
		if len(track.RotationKeys) > 0 {
			target.Rotation = sampleQuatKeys(track.RotationKeys, sampleTime)
		}
		if len(track.ScaleKeys) > 0 {
			target.Scale = sampleVec3Keys(track.ScaleKeys, sampleTime)
		}
	}
}

func sampleVec3Keys(keys []content.AssetVec3KeyDef, t float32) mgl32.Vec3 {
	if len(keys) == 0 {
		return mgl32.Vec3{}
	}
	if len(keys) == 1 || t <= keys[0].Time {
		return contentVec3(keys[0].Value)
	}
	for i := 0; i < len(keys)-1; i++ {
		left := keys[i]
		right := keys[i+1]
		if t > right.Time {
			continue
		}
		span := right.Time - left.Time
		if span <= 0 {
			return contentVec3(right.Value)
		}
		alpha := (t - left.Time) / span
		return contentVec3(left.Value).Mul(1 - alpha).Add(contentVec3(right.Value).Mul(alpha))
	}
	return contentVec3(keys[len(keys)-1].Value)
}

func sampleQuatKeys(keys []content.AssetQuatKeyDef, t float32) mgl32.Quat {
	if len(keys) == 0 {
		return mgl32.QuatIdent()
	}
	if len(keys) == 1 || t <= keys[0].Time {
		return contentQuat(keys[0].Value)
	}
	for i := 0; i < len(keys)-1; i++ {
		left := keys[i]
		right := keys[i+1]
		if t > right.Time {
			continue
		}
		span := right.Time - left.Time
		if span <= 0 {
			return contentQuat(right.Value)
		}
		alpha := (t - left.Time) / span
		return mgl32.QuatSlerp(contentQuat(left.Value), contentQuat(right.Value), alpha).Normalize()
	}
	return contentQuat(keys[len(keys)-1].Value)
}

func contentVec3(value content.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{value[0], value[1], value[2]}
}

func contentQuat(value content.Quat) mgl32.Quat {
	if value == (content.Quat{}) {
		return mgl32.QuatIdent()
	}
	return mgl32.Quat{V: mgl32.Vec3{value[0], value[1], value[2]}, W: value[3]}.Normalize()
}

func newAuthoredAssetAnimationSetComponent(def *content.AssetDef, result AuthoredAssetSpawnResult, cmd *Commands) *AuthoredAssetAnimationSetComponent {
	if def == nil || len(def.AnimationClips) == 0 {
		return nil
	}
	clips := make(map[string]content.AssetAnimationClipDef, len(def.AnimationClips))
	defaultClipID := ""
	for _, clip := range def.AnimationClips {
		if defaultClipID == "" {
			defaultClipID = clip.ID
		}
		clips[clip.ID] = cloneAssetAnimationClip(clip)
	}
	bindTransforms := make(map[string]LocalTransformComponent, len(result.EntitiesByAssetID))
	for itemID, eid := range result.EntitiesByAssetID {
		local, ok := localTransformForAnimationBind(cmd, eid)
		if ok {
			bindTransforms[itemID] = local
		}
	}
	return &AuthoredAssetAnimationSetComponent{
		DefaultClipID:  defaultClipID,
		Clips:          clips,
		BindTransforms: bindTransforms,
	}
}

func localTransformForAnimationBind(cmd *Commands, eid EntityId) (LocalTransformComponent, bool) {
	for _, comp := range cmd.GetAllComponents(eid) {
		if local, ok := comp.(*LocalTransformComponent); ok {
			return *local, true
		}
		if local, ok := comp.(LocalTransformComponent); ok {
			return local, true
		}
	}
	return LocalTransformComponent{}, false
}

func cloneAssetAnimationClip(clip content.AssetAnimationClipDef) content.AssetAnimationClipDef {
	clone := clip
	clone.Tags = append([]string(nil), clip.Tags...)
	clone.Tracks = append([]content.AssetAnimationTrackDef(nil), clip.Tracks...)
	for i := range clone.Tracks {
		clone.Tracks[i].PositionKeys = append([]content.AssetVec3KeyDef(nil), clip.Tracks[i].PositionKeys...)
		clone.Tracks[i].RotationKeys = append([]content.AssetQuatKeyDef(nil), clip.Tracks[i].RotationKeys...)
		clone.Tracks[i].ScaleKeys = append([]content.AssetVec3KeyDef(nil), clip.Tracks[i].ScaleKeys...)
	}
	return clone
}
