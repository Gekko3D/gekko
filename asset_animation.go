package gekko

import (
	"maps"
	"math"
	"reflect"
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
			ProfileCategory("animation").
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(assetAnimationSystem).
			ProfileCategory("animation").
			InStage(Update).
			RunAlways(),
	)
	app.UseSystem(
		System(npcAnimationEventDeliverySystem).
			ProfileCategory("animation").
			InStage(Update).
			RunAlways(),
	)
}

type AnimationPlayerComponent struct {
	ClipID           string
	Time             float32
	Speed            float32
	Playing          bool
	Loop             bool
	Completed        bool
	BlendValue       float32
	HasBlendValue    bool
	CrossedEvents    []AnimationEvent
	RootMotionPolicy AnimationRootMotionPolicy
	Layers           []AnimationLayer
}

// AnimationEvent is a crossed authored event for one sampled clip.
type AnimationEvent struct {
	ClipID  string
	Frame   int
	ID      int
	Type    int
	Options string
}

// AnimationRootMotionPolicy controls position keys on authored root targets.
// Controller-driven actors use Locked so animation never moves gameplay state.
type AnimationRootMotionPolicy string

const (
	AnimationRootMotionApply  AnimationRootMotionPolicy = "apply"
	AnimationRootMotionLocked AnimationRootMotionPolicy = "locked"
)

type AnimationLayerMode string

const (
	AnimationLayerOverride AnimationLayerMode = "override"
	AnimationLayerAdditive AnimationLayerMode = "additive"
)

// AnimationLayer is an ordered local-transform overlay. BoneMask contains
// authored item IDs, never source-bone names resolved at runtime.
type AnimationLayer struct {
	// Tag is consumer-owned identity for replacing a persistent overlay.
	Tag              string
	ClipID           string
	Time             float32
	Speed            float32
	Weight           float32
	Mode             AnimationLayerMode
	BoneMask         []string
	RootMotionPolicy AnimationRootMotionPolicy
	Loop             bool
	LoopOverride     bool
	BlendValue       float32
	HasBlendValue    bool
}

type AuthoredAssetAnimationSetComponent struct {
	DefaultClipID  string
	Clips          map[string]content.AssetAnimationClipDef
	BindTransforms map[string]LocalTransformComponent
	JointTargets   map[string]string
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
			rootTargets := animationRootTargetIDs(cmd, parentByEntity, root, rootRef.AssetID)
			applyAnimationLayers(player, animationSet, targets, rootTargets, dt)
			return true
		})
}

// SampleAuthoredAssetAnimation applies an authored asset's current base clip
// and layers without advancing time. Gameplay can use it after changing a
// pose selection, before adding a procedural post-animation adjustment.
func SampleAuthoredAssetAnimation(cmd *Commands, root EntityId) bool {
	if cmd == nil || root == 0 {
		return false
	}
	player, _ := cmd.GetComponent(root, reflect.TypeOf(AnimationPlayerComponent{})).(*AnimationPlayerComponent)
	animationSet, _ := cmd.GetComponent(root, reflect.TypeOf(AuthoredAssetAnimationSetComponent{})).(*AuthoredAssetAnimationSetComponent)
	rootRef, _ := cmd.GetComponent(root, reflect.TypeOf(AuthoredAssetRootComponent{})).(*AuthoredAssetRootComponent)
	if player == nil || animationSet == nil || rootRef == nil || len(animationSet.Clips) == 0 {
		return false
	}
	if player.ClipID == "" {
		player.ClipID = animationSet.DefaultClipID
	}
	parentByEntity := animationParentIndex(cmd)
	targets := animationTargetsForRoot(cmd, parentByEntity, root, rootRef.AssetID)
	rootTargets := animationRootTargetIDs(cmd, parentByEntity, root, rootRef.AssetID)
	applyAnimationLayers(player, animationSet, targets, rootTargets, 0)
	return true
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

func animationRootTargetIDs(cmd *Commands, parentByEntity map[EntityId]EntityId, root EntityId, assetID string) map[string]struct{} {
	roots := map[string]struct{}{}
	MakeQuery1[AuthoredAssetRefComponent](cmd).Map(func(eid EntityId, ref *AuthoredAssetRefComponent) bool {
		if ref.AssetID == assetID && parentByEntity[eid] == root {
			roots[ref.ItemID] = struct{}{}
		}
		return true
	})
	return roots
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
	if player == nil {
		return
	}
	player.CrossedEvents = player.CrossedEvents[:0]
	if !player.Playing || dt == 0 {
		return
	}
	speed := player.Speed
	if speed == 0 {
		speed = 1
	}
	previous := player.Time
	player.Time += dt * speed
	if clip.Duration <= 0 {
		return
	}
	if player.Loop {
		appendCrossedAnimationEvents(player, clip, previous, player.Time, true)
		player.Time = positiveMod(player.Time, clip.Duration)
		return
	}
	if player.Time >= clip.Duration {
		player.Time = clip.Duration
		player.Playing = false
		player.Completed = true
	} else if player.Time <= 0 {
		player.Time = 0
		player.Playing = false
		player.Completed = true
	}
	appendCrossedAnimationEvents(player, clip, previous, player.Time, false)
}

func appendCrossedAnimationEvents(player *AnimationPlayerComponent, clip content.AssetAnimationClipDef, start, end float32, loop bool) {
	if player == nil || len(clip.Events) == 0 || clip.Duration <= 0 || end == start {
		return
	}
	if end > start {
		if !loop {
			appendForwardAnimationEvents(player, clip, start, end)
			return
		}
		for current := start; current < end; {
			cycle := float32(math.Floor(float64(current / clip.Duration)))
			cycleEnd := (cycle + 1) * clip.Duration
			limit := min(end, cycleEnd)
			appendForwardAnimationEvents(player, clip, current-cycle*clip.Duration, limit-cycle*clip.Duration)
			current = limit
		}
		return
	}
	if !loop {
		appendReverseAnimationEvents(player, clip, end, start, false)
		return
	}
	for current := start; current > end; {
		cycle := float32(math.Ceil(float64(current/clip.Duration))) - 1
		cycleStart := cycle * clip.Duration
		limit := max(end, cycleStart)
		appendReverseAnimationEvents(player, clip, limit-cycleStart, current-cycleStart, limit == cycleStart)
		current = limit
	}
}

// Forward crossings use (start, end]; reverse crossings use [start, end).
func appendForwardAnimationEvents(player *AnimationPlayerComponent, clip content.AssetAnimationClipDef, start, end float32) {
	for _, event := range clip.Events {
		if (event.Time > start || (start == 0 && event.Time == 0)) && event.Time <= end {
			player.CrossedEvents = append(player.CrossedEvents, AnimationEvent{ClipID: player.ClipID, Frame: event.Frame, ID: event.ID, Type: event.Type, Options: event.Options})
		}
	}
}

func appendReverseAnimationEvents(player *AnimationPlayerComponent, clip content.AssetAnimationClipDef, start, end float32, includeDurationAtStart bool) {
	for i := len(clip.Events) - 1; i >= 0; i-- {
		event := clip.Events[i]
		if (event.Time >= start && event.Time < end) || (includeDurationAtStart && event.Time == clip.Duration) {
			player.CrossedEvents = append(player.CrossedEvents, AnimationEvent{ClipID: player.ClipID, Frame: event.Frame, ID: event.ID, Type: event.Type, Options: event.Options})
		}
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
			clipID, playbackSpeed := anim.ExplicitClipID, anim.ExplicitPlaybackSpeed
			if clipID != "" && playbackSpeed == 0 {
				playbackSpeed = 1
			}
			if clipID == "" && anim.LocomotionClipID != "" {
				if _, ok := animationSet.Clips[anim.LocomotionClipID]; ok {
					clipID, playbackSpeed = anim.LocomotionClipID, anim.LocomotionPlaybackSpeed
				}
			}
			if clipID == "" {
				clipID = selectNPCAnimationClipID(animationSet, anim.State, anim.FallbackClipID)
			}
			if clipID == "" {
				return true
			}
			if _, ok := animationSet.Clips[clipID]; !ok {
				if anim.ExplicitClipID != "" && (anim.FailedRequestID != anim.RequestID || anim.FailedClipID != clipID) {
					anim.AppliedRequestID = anim.RequestID
					anim.FailedRequestID, anim.FailedClipID, anim.FailureReason = anim.RequestID, clipID, "exact_clip_missing"
					anim.ActiveClipID, anim.Completed = "", true
					player.Playing, player.Completed = false, true
				}
				return true
			}
			if player.ClipID != clipID || anim.RequestID != anim.AppliedRequestID {
				player.ClipID = clipID
				player.Time = 0
				if playbackSpeed < 0 {
					player.Time = animationSet.Clips[clipID].Duration
				}
				player.Playing = true
				player.Completed = false
				anim.AppliedRequestID = anim.RequestID
				anim.FailedRequestID, anim.FailedClipID, anim.FailureReason = 0, "", ""
			}
			if playbackSpeed == 0 {
				playbackSpeed = 1
			}
			player.Speed = playbackSpeed
			if clip, ok := animationSet.Clips[clipID]; ok {
				player.Loop = clip.Loop
			}
			player.RootMotionPolicy = AnimationRootMotionLocked
			if anim.State == NPCAnimationStateDeath {
				player.RootMotionPolicy = AnimationRootMotionApply
			}
			player.BlendValue, player.HasBlendValue = anim.BlendValue, anim.HasBlendValue
			anim.ActiveClipID = clipID
			return true
		})
}

func npcAnimationEventDeliverySystem(cmd *Commands) {
	parentByEntity := animationParentIndex(cmd)
	MakeQuery2[NPCComponent, NPCAnimationComponent](cmd).
		Map(func(npcEntity EntityId, _ *NPCComponent, anim *NPCAnimationComponent) bool {
			_, player, _, ok := npcAnimationAssetRoot(cmd, parentByEntity, npcEntity)
			if !ok || player == nil || anim == nil {
				return true
			}
			anim.CrossedEvents = append(anim.CrossedEvents[:0], player.CrossedEvents...)
			player.CrossedEvents = player.CrossedEvents[:0]
			anim.Completed = player.Completed
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
	case NPCAnimationStateLadder:
		return []string{"ladder", "climb"}
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
	resetAnimationTargets(bindTransforms, targets)
	applyAnimationClipPose(clip, sampleTime, 0, false, 1, AnimationLayerOverride, nil, AnimationRootMotionApply, nil, bindTransforms, targets)
}

func applyAnimationLayers(player *AnimationPlayerComponent, animationSet *AuthoredAssetAnimationSetComponent, targets map[string]*LocalTransformComponent, rootTargets map[string]struct{}, dt float32) {
	if player == nil || animationSet == nil {
		return
	}
	resetAnimationTargets(animationSet.BindTransforms, targets)
	if base, ok := animationSet.Clips[player.ClipID]; ok {
		applyAnimationClipPose(base, player.Time, player.BlendValue, player.HasBlendValue, 1, AnimationLayerOverride, nil, player.RootMotionPolicy, rootTargets, animationSet.BindTransforms, targets)
	}
	activeLayers := player.Layers[:0]
	for i := range player.Layers {
		layer := &player.Layers[i]
		clip, ok := animationSet.Clips[layer.ClipID]
		if !ok || layer.Weight <= 0 {
			continue
		}
		if advanceAnimationLayer(layer, clip, dt) {
			continue
		}
		applyAnimationClipPose(clip, layer.Time, layer.BlendValue, layer.HasBlendValue, layer.Weight, layer.Mode, layer.BoneMask, layer.RootMotionPolicy, rootTargets, animationSet.BindTransforms, targets)
		activeLayers = append(activeLayers, *layer)
	}
	player.Layers = activeLayers
}

func applyAnimationClipPose(clip content.AssetAnimationClipDef, sampleTime, blendValue float32, hasBlendValue bool, weight float32, mode AnimationLayerMode, boneMask []string, rootMotion AnimationRootMotionPolicy, rootTargets map[string]struct{}, bindTransforms map[string]LocalTransformComponent, targets map[string]*LocalTransformComponent) {
	if clip.Blend1D == nil || len(clip.Blend1D.Samples) < 2 {
		applyAnimationClipLayer(clip, sampleTime, weight, mode, boneMask, rootMotion, rootTargets, bindTransforms, targets)
		return
	}
	if !hasBlendValue {
		blendValue = clip.Blend1D.Default
	}
	left, right, alpha := animationBlendSamples(clip.Blend1D.Samples, blendValue)
	applyAnimationTrackBlend(left.Tracks, right.Tracks, sampleTime, alpha, weight, mode, boneMask, rootMotion, rootTargets, bindTransforms, targets)
}

func animationBlendSamples(samples []content.AssetAnimationBlendSampleDef, value float32) (content.AssetAnimationBlendSampleDef, content.AssetAnimationBlendSampleDef, float32) {
	if value <= samples[0].Value {
		return samples[0], samples[0], 0
	}
	last := samples[len(samples)-1]
	if value >= last.Value {
		return last, last, 0
	}
	for index := 0; index < len(samples)-1; index++ {
		left, right := samples[index], samples[index+1]
		if value <= right.Value {
			return left, right, (value - left.Value) / (right.Value - left.Value)
		}
	}
	return last, last, 0
}

func applyAnimationTrackBlend(left, right []content.AssetAnimationTrackDef, sampleTime, alpha, weight float32, mode AnimationLayerMode, boneMask []string, rootMotion AnimationRootMotionPolicy, rootTargets map[string]struct{}, bindTransforms map[string]LocalTransformComponent, targets map[string]*LocalTransformComponent) {
	weight = max(0, min(1, weight))
	if weight == 0 || len(left) != len(right) {
		return
	}
	for index := range left {
		if left[index].TargetID != right[index].TargetID {
			return
		}
	}
	mask := animationBoneMask(boneMask)
	for index := range left {
		leftTrack, rightTrack := left[index], right[index]
		if len(mask) > 0 {
			if _, ok := mask[leftTrack.TargetID]; !ok {
				continue
			}
		}
		target, ok := targets[leftTrack.TargetID]
		if !ok {
			continue
		}
		bind := *target
		if authoredBind, ok := bindTransforms[leftTrack.TargetID]; ok {
			bind = authoredBind
		}
		if _, isRoot := rootTargets[leftTrack.TargetID]; !(isRoot && rootMotion == AnimationRootMotionLocked) && (len(leftTrack.PositionKeys) > 0 || len(rightTrack.PositionKeys) > 0) {
			leftValue := sampleVec3KeysOr(leftTrack.PositionKeys, sampleTime, bind.Position)
			rightValue := sampleVec3KeysOr(rightTrack.PositionKeys, sampleTime, bind.Position)
			target.Position = animationBlendVec3(target.Position, leftValue.Mul(1-alpha).Add(rightValue.Mul(alpha)), bind.Position, weight, mode)
		}
		if len(leftTrack.RotationKeys) > 0 || len(rightTrack.RotationKeys) > 0 {
			leftValue := sampleQuatKeysOr(leftTrack.RotationKeys, sampleTime, bind.Rotation)
			rightValue := sampleQuatKeysOr(rightTrack.RotationKeys, sampleTime, bind.Rotation)
			target.Rotation = animationBlendQuat(target.Rotation, mgl32.QuatSlerp(leftValue, rightValue, alpha).Normalize(), bind.Rotation, weight, mode)
		}
		if len(leftTrack.ScaleKeys) > 0 || len(rightTrack.ScaleKeys) > 0 {
			leftValue := sampleVec3KeysOr(leftTrack.ScaleKeys, sampleTime, bind.Scale)
			rightValue := sampleVec3KeysOr(rightTrack.ScaleKeys, sampleTime, bind.Scale)
			target.Scale = animationBlendVec3(target.Scale, leftValue.Mul(1-alpha).Add(rightValue.Mul(alpha)), bind.Scale, weight, mode)
		}
	}
}

func sampleVec3KeysOr(keys []content.AssetVec3KeyDef, time float32, fallback mgl32.Vec3) mgl32.Vec3 {
	if len(keys) == 0 {
		return fallback
	}
	return sampleVec3Keys(keys, time)
}

func sampleQuatKeysOr(keys []content.AssetQuatKeyDef, time float32, fallback mgl32.Quat) mgl32.Quat {
	if len(keys) == 0 {
		return fallback
	}
	return sampleQuatKeys(keys, time)
}

// advanceAnimationLayer reports a finished one-shot overlay. Looping clips
// retain prior behavior; non-looping overlays disappear instead of freezing.
func advanceAnimationLayer(layer *AnimationLayer, clip content.AssetAnimationClipDef, dt float32) bool {
	if layer == nil || dt == 0 {
		return false
	}
	speed := layer.Speed
	if speed == 0 {
		speed = 1
	}
	layer.Time += dt * speed
	if clip.Duration <= 0 {
		return false
	}
	loop := clip.Loop
	if layer.LoopOverride {
		loop = layer.Loop
	}
	if loop {
		layer.Time = positiveMod(layer.Time, clip.Duration)
		return false
	}
	if layer.Time >= clip.Duration {
		return true
	} else if layer.Time <= 0 {
		return true
	}
	return false
}

func resetAnimationTargets(bindTransforms map[string]LocalTransformComponent, targets map[string]*LocalTransformComponent) {
	for id, target := range targets {
		if bind, ok := bindTransforms[id]; ok {
			*target = bind
		}
	}
}

func applyAnimationClipLayer(clip content.AssetAnimationClipDef, sampleTime, weight float32, mode AnimationLayerMode, boneMask []string, rootMotion AnimationRootMotionPolicy, rootTargets map[string]struct{}, bindTransforms map[string]LocalTransformComponent, targets map[string]*LocalTransformComponent) {
	weight = max(0, min(1, weight))
	if weight == 0 {
		return
	}
	mask := animationBoneMask(boneMask)
	for _, track := range clip.Tracks {
		if len(mask) > 0 {
			if _, ok := mask[track.TargetID]; !ok {
				continue
			}
		}
		target, ok := targets[track.TargetID]
		if !ok {
			continue
		}
		bind := *target
		if authoredBind, ok := bindTransforms[track.TargetID]; ok {
			bind = authoredBind
		}
		if len(track.PositionKeys) > 0 {
			if _, isRoot := rootTargets[track.TargetID]; !(isRoot && rootMotion == AnimationRootMotionLocked) {
				sample := sampleVec3Keys(track.PositionKeys, sampleTime)
				target.Position = animationBlendVec3(target.Position, sample, bind.Position, weight, mode)
			}
		}
		if len(track.RotationKeys) > 0 {
			target.Rotation = animationBlendQuat(target.Rotation, sampleQuatKeys(track.RotationKeys, sampleTime), bind.Rotation, weight, mode)
		}
		if len(track.ScaleKeys) > 0 {
			target.Scale = animationBlendVec3(target.Scale, sampleVec3Keys(track.ScaleKeys, sampleTime), bind.Scale, weight, mode)
		}
	}
}

func animationBoneMask(ids []string) map[string]struct{} {
	if len(ids) == 0 {
		return nil
	}
	mask := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		mask[id] = struct{}{}
	}
	return mask
}

func animationBlendVec3(current, sample, bind mgl32.Vec3, weight float32, mode AnimationLayerMode) mgl32.Vec3 {
	if mode == AnimationLayerAdditive {
		return current.Add(sample.Sub(bind).Mul(weight))
	}
	return current.Mul(1 - weight).Add(sample.Mul(weight))
}

func animationBlendQuat(current, sample, bind mgl32.Quat, weight float32, mode AnimationLayerMode) mgl32.Quat {
	if mode == AnimationLayerAdditive {
		delta := sample.Mul(bind.Inverse()).Normalize()
		return current.Mul(mgl32.QuatSlerp(mgl32.QuatIdent(), delta, weight)).Normalize()
	}
	return mgl32.QuatSlerp(current, sample, weight).Normalize()
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

func newAuthoredAssetAnimationSetComponent(resolved *content.ResolvedAssetAnimations, result AuthoredAssetSpawnResult, cmd *Commands) *AuthoredAssetAnimationSetComponent {
	if resolved == nil || len(resolved.Clips) == 0 {
		return nil
	}
	clips := make(map[string]content.AssetAnimationClipDef, len(resolved.Clips))
	for _, clip := range resolved.Clips {
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
		DefaultClipID:  resolved.DefaultClipID,
		Clips:          clips,
		BindTransforms: bindTransforms,
		JointTargets:   maps.Clone(resolved.JointTargets),
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
	clone.Events = append([]content.AssetAnimationEventDef(nil), clip.Events...)
	clone.TraversalMotion = append([]content.AssetVec3KeyDef(nil), clip.TraversalMotion...)
	clone.Tracks = append([]content.AssetAnimationTrackDef(nil), clip.Tracks...)
	for i := range clone.Tracks {
		clone.Tracks[i].PositionKeys = append([]content.AssetVec3KeyDef(nil), clip.Tracks[i].PositionKeys...)
		clone.Tracks[i].RotationKeys = append([]content.AssetQuatKeyDef(nil), clip.Tracks[i].RotationKeys...)
		clone.Tracks[i].ScaleKeys = append([]content.AssetVec3KeyDef(nil), clip.Tracks[i].ScaleKeys...)
	}
	if clip.Blend1D != nil {
		clone.Blend1D = &content.AssetAnimationBlend1DDef{Parameter: clip.Blend1D.Parameter, Default: clip.Blend1D.Default, Samples: append([]content.AssetAnimationBlendSampleDef(nil), clip.Blend1D.Samples...)}
		for sample := range clone.Blend1D.Samples {
			clone.Blend1D.Samples[sample].Tracks = append([]content.AssetAnimationTrackDef(nil), clip.Blend1D.Samples[sample].Tracks...)
			for index := range clone.Blend1D.Samples[sample].Tracks {
				track := &clone.Blend1D.Samples[sample].Tracks[index]
				source := clip.Blend1D.Samples[sample].Tracks[index]
				track.PositionKeys = append([]content.AssetVec3KeyDef(nil), source.PositionKeys...)
				track.RotationKeys = append([]content.AssetQuatKeyDef(nil), source.RotationKeys...)
				track.ScaleKeys = append([]content.AssetVec3KeyDef(nil), source.ScaleKeys...)
			}
		}
	}
	return clone
}
