package gekko

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type AnimationModule struct{}

func (AnimationModule) Install(app *App, cmd *Commands) {
	app.UseSystem(
		System(npcAnimationSystem).
			InStage(Update).
			RunAlways(),
	)
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

func npcAnimationSystem(cmd *Commands) {
	parentByEntity := animationParentIndex(cmd)
	MakeQuery2[NPCComponent, NPCAnimationComponent](cmd).
		Map(func(npcEntity EntityId, _ *NPCComponent, anim *NPCAnimationComponent) bool {
			assetRoot, player, animationSet, ok := npcAnimationAssetRoot(cmd, parentByEntity, npcEntity)
			if !ok || assetRoot == 0 || player == nil || animationSet == nil {
				return true
			}
			clipID := selectNPCAnimationClipID(animationSet, anim.State, anim.FallbackClipID)
			if clipID == "" {
				return true
			}
			if player.ClipID != clipID {
				player.ClipID = clipID
				player.Time = 0
			}
			if player.Speed == 0 {
				player.Speed = 1
			}
			player.Playing = true
			if clip, ok := animationSet.Clips[clipID]; ok {
				player.Loop = clip.Loop
			}
			anim.ActiveClipID = clipID
			return true
		})
}

func npcAnimationAssetRoot(cmd *Commands, parentByEntity map[EntityId]EntityId, npcEntity EntityId) (EntityId, *AnimationPlayerComponent, *AuthoredAssetAnimationSetComponent, bool) {
	var rootEntity EntityId
	var outPlayer *AnimationPlayerComponent
	var outSet *AuthoredAssetAnimationSetComponent
	MakeQuery4[AuthoredAssetRootComponent, Parent, AnimationPlayerComponent, AuthoredAssetAnimationSetComponent](cmd).
		Map(func(eid EntityId, _ *AuthoredAssetRootComponent, parent *Parent, player *AnimationPlayerComponent, animationSet *AuthoredAssetAnimationSetComponent) bool {
			if parent == nil || parent.Entity != npcEntity {
				return true
			}
			rootEntity = eid
			outPlayer = player
			outSet = animationSet
			return false
		})
	if rootEntity != 0 {
		return rootEntity, outPlayer, outSet, true
	}
	MakeQuery3[AuthoredAssetRootComponent, AnimationPlayerComponent, AuthoredAssetAnimationSetComponent](cmd).
		Map(func(eid EntityId, _ *AuthoredAssetRootComponent, player *AnimationPlayerComponent, animationSet *AuthoredAssetAnimationSetComponent) bool {
			if !animationEntityDescendsFrom(parentByEntity, eid, npcEntity) {
				return true
			}
			rootEntity = eid
			outPlayer = player
			outSet = animationSet
			return false
		})
	return rootEntity, outPlayer, outSet, rootEntity != 0
}

func selectNPCAnimationClipID(animationSet *AuthoredAssetAnimationSetComponent, state string, fallbackClipID string) string {
	if animationSet == nil || len(animationSet.Clips) == 0 {
		return ""
	}
	state = normalizeNPCAnimationToken(state)
	if state == "" {
		state = NPCAnimationStateIdle
	}
	candidates := npcAnimationClipCandidates(state)
	clipIDs := sortedAnimationClipIDs(animationSet.Clips)
	bestClipID := ""
	bestScore := 0
	for _, candidate := range candidates {
		for _, clipID := range clipIDs {
			clip := animationSet.Clips[clipID]
			score := npcAnimationClipMatchScore(clip, candidate)
			if score > bestScore {
				bestScore = score
				bestClipID = clipID
			}
		}
	}
	if bestClipID != "" {
		return bestClipID
	}
	if fallbackClipID != "" {
		if _, ok := animationSet.Clips[fallbackClipID]; ok {
			return fallbackClipID
		}
	}
	if animationSet.DefaultClipID != "" {
		if _, ok := animationSet.Clips[animationSet.DefaultClipID]; ok {
			return animationSet.DefaultClipID
		}
	}
	if len(clipIDs) > 0 {
		return clipIDs[0]
	}
	return ""
}

func npcAnimationClipMatchScore(clip content.AssetAnimationClipDef, candidate string) int {
	candidate = normalizeNPCAnimationToken(candidate)
	if candidate == "" {
		return 0
	}
	score := 0
	for _, value := range []string{clip.ID, clip.Name} {
		normalized := normalizeNPCAnimationToken(value)
		score = maxNPCAnimationScore(score, npcAnimationTokenMatchScore(normalized, candidate))
	}
	for _, tag := range clip.Tags {
		normalized := normalizeNPCAnimationToken(tag)
		score = maxNPCAnimationScore(score, npcAnimationTokenMatchScore(normalized, candidate)/2)
	}
	if score == 0 {
		return 0
	}
	if npcAnimationClipHasTag(clip, "generated:sequence_clip") {
		score += 1000
	}
	if npcAnimationClipHasTag(clip, "generated:bind_pose_clip") {
		score -= 100
	}
	return score
}

func npcAnimationTokenMatchScore(normalized string, candidate string) int {
	if normalized == "" {
		return 0
	}
	switch {
	case normalized == candidate:
		return 400
	case strings.HasPrefix(normalized, candidate):
		return 350 - minNPCAnimationInt(len(normalized)-len(candidate), 100)
	case strings.Contains(normalized, candidate):
		return 200 - minNPCAnimationInt(len(normalized)-len(candidate), 100)
	default:
		return 0
	}
}

func npcAnimationClipHasTag(clip content.AssetAnimationClipDef, tag string) bool {
	normalizedTag := normalizeNPCAnimationToken(tag)
	for _, value := range clip.Tags {
		if normalizeNPCAnimationToken(value) == normalizedTag {
			return true
		}
	}
	return false
}

func maxNPCAnimationScore(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func minNPCAnimationInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func sortedAnimationClipIDs(clips map[string]content.AssetAnimationClipDef) []string {
	ids := make([]string, 0, len(clips))
	for id := range clips {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids
}

func npcAnimationClipCandidates(state string) []string {
	switch state {
	case NPCAnimationStateWalk:
		return []string{"walk", "move", "run"}
	case NPCAnimationStateRun:
		return []string{"run", "walk", "move"}
	case NPCAnimationStateAttack:
		return []string{"attack", "shoot", "fire", "melee", "range"}
	case NPCAnimationStatePain:
		return []string{"pain", "flinch", "hit"}
	case NPCAnimationStateDeath:
		return []string{"death", "die", "dead"}
	case NPCAnimationStateIdle:
		fallthrough
	default:
		return []string{"idle", "stand", "wait"}
	}
}

func npcAnimationClipMatches(clip content.AssetAnimationClipDef, candidate string) bool {
	return npcAnimationClipMatchScore(clip, candidate) > 0
}

func normalizeNPCAnimationToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
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
