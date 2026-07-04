package gekko

import (
	"math"
	"reflect"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// ApplyAuthoredAimRig turns a character's authored aim chain so its held
// asset's calibrated aim frame faces targetDirection. The caller owns the
// target (camera, AI, or script); this is intentionally game-neutral.
func ApplyAuthoredAimRig(cmd *Commands, characterRoot, heldAssetRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, targetDirection mgl32.Vec3) bool {
	if cmd == nil || characterRoot == 0 || heldAssetRoot == 0 || len(aimBones) == 0 || targetDirection.LenSqr() <= 1e-8 {
		return false
	}
	TransformHierarchySystem(cmd)
	frame, ok := authoredAimFrame(cmd, heldAssetRoot, rig)
	if !ok {
		return false
	}
	root, _ := cmd.GetComponent(characterRoot, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if root == nil {
		return false
	}
	return applyAuthoredAimDirections(cmd, characterRoot, heldAssetRoot, root, rig, aimBones, frame, targetDirection, targetDirection)
}

// ApplyAuthoredAimRigAtPoint turns an authored aim chain toward a world-space
// target. Games choose that target from their camera, AI, or interaction ray;
// the generic solver accounts for the held asset's actual muzzle position.
func ApplyAuthoredAimRigAtPoint(cmd *Commands, characterRoot, heldAssetRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, targetPoint mgl32.Vec3) bool {
	if cmd == nil || characterRoot == 0 || heldAssetRoot == 0 || len(aimBones) == 0 {
		return false
	}
	TransformHierarchySystem(cmd)
	frame, ok := authoredAimFrame(cmd, heldAssetRoot, rig)
	if !ok {
		return false
	}
	targetDirection := targetPoint.Sub(frame.Position)
	if targetDirection.LenSqr() <= 1e-8 {
		return false
	}
	root, _ := cmd.GetComponent(characterRoot, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if root == nil {
		return false
	}
	return applyAuthoredAimDirections(cmd, characterRoot, heldAssetRoot, root, rig, aimBones, frame, targetDirection, targetDirection)
}

// ApplyAuthoredAimRigAtRay aims at a point on a world-space ray. Its pitch
// follows the muzzle-to-point vector while its yaw follows the ray heading,
// which remains stable when the point is nearly straight above or below.
func ApplyAuthoredAimRigAtRay(cmd *Commands, characterRoot, heldAssetRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, targetPoint, rayDirection mgl32.Vec3) bool {
	if cmd == nil || characterRoot == 0 || heldAssetRoot == 0 || len(aimBones) == 0 || rayDirection.LenSqr() <= 1e-8 {
		return false
	}
	TransformHierarchySystem(cmd)
	frame, ok := authoredAimFrame(cmd, heldAssetRoot, rig)
	if !ok {
		return false
	}
	targetDirection := targetPoint.Sub(frame.Position)
	if targetDirection.LenSqr() <= 1e-8 {
		return false
	}
	root, _ := cmd.GetComponent(characterRoot, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if root == nil {
		return false
	}
	return applyAuthoredAimDirections(cmd, characterRoot, heldAssetRoot, root, rig, aimBones, frame, targetDirection, rayDirection)
}

func applyAuthoredAimDirections(cmd *Commands, characterRoot, heldAssetRoot EntityId, root *TransformComponent, rig content.CharacterAimRigDef, aimBones []EntityId, currentFrame authoredAimFrameState, targetDirection, yawDirection mgl32.Vec3) bool {
	if root == nil || currentFrame.Direction.LenSqr() <= 1e-8 || targetDirection.LenSqr() <= 1e-8 {
		return false
	}
	if rig.YawLimitDegrees <= 0 && rig.PitchLimitDegrees <= 0 {
		_, authoredUp, _, ok := authoredAimAxes(rig)
		if !ok {
			return false
		}
		targetFrame, ok := authoredAimFrameForDirection(
			targetDirection,
			root.Rotation.Rotate(authoredUp),
			currentFrame.Rotation.Rotate(mgl32.Vec3{0, 1, 0}),
		)
		if !ok {
			return false
		}
		return applyAuthoredAimWorldDelta(cmd, heldAssetRoot, rig, aimBones, root, targetFrame.Mul(currentFrame.Rotation.Inverse()).Normalize())
	}
	toLocal := root.Rotation.Inverse()
	current := toLocal.Rotate(currentFrame.Direction).Normalize()
	target := toLocal.Rotate(targetDirection).Normalize()
	yawTarget := toLocal.Rotate(yawDirection).Normalize()
	forward, up, _, ok := authoredAimAxes(rig)
	if !ok {
		return false
	}
	// The horizontal reference points toward positive camera yaw. The pitch
	// axis points the other way (forward × up), so keep the two distinct.
	yawRight := up.Cross(forward).Normalize()
	yaw := float32(0)
	if yawTarget.Sub(up.Mul(yawTarget.Dot(up))).LenSqr() > 1e-8 {
		yaw = authoredAimNormalize(authoredAimYaw(yawTarget, forward, yawRight) - authoredAimYaw(current, forward, yawRight))
	}
	pitch := authoredAimPitch(target, up) - authoredAimPitch(current, up)
	return applyAuthoredAimOffset(cmd, characterRoot, heldAssetRoot, rig, aimBones, yaw, pitch)
}

// ApplyAuthoredAimOffset applies camera-relative yaw and pitch in the
// character's authored root basis. It is useful while an attachment has no
// calibrated aim frame yet. Each root-space axis is converted to the current
// parent space of its bone, so authored bind rotations remain valid.
func ApplyAuthoredAimOffset(cmd *Commands, characterRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, yaw, pitch float32) bool {
	if cmd == nil || characterRoot == 0 || len(aimBones) == 0 {
		return false
	}
	TransformHierarchySystem(cmd)
	return applyAuthoredAimOffset(cmd, characterRoot, 0, rig, aimBones, yaw, pitch)
}

func applyAuthoredAimOffset(cmd *Commands, characterRoot, heldAssetRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, yaw, pitch float32) bool {
	root, _ := cmd.GetComponent(characterRoot, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if root == nil {
		return false
	}
	_, up, pitchAxis, ok := authoredAimAxes(rig)
	if !ok {
		return false
	}
	yaw = authoredAimClamp(yaw, rig.YawLimitDegrees)
	pitch = authoredAimClamp(pitch, rig.PitchLimitDegrees)
	worldYawAxis := root.Rotation.Rotate(up).Normalize()
	worldPitchAxis := root.Rotation.Rotate(pitchAxis).Normalize()
	worldDelta := mgl32.QuatRotate(mgl32.DegToRad(pitch), worldPitchAxis).
		Mul(mgl32.QuatRotate(mgl32.DegToRad(yaw), worldYawAxis)).
		Normalize()
	return applyAuthoredAimWorldDelta(cmd, heldAssetRoot, rig, aimBones, root, worldDelta)
}

func applyAuthoredAimWorldDelta(cmd *Commands, heldAssetRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, root *TransformComponent, worldDelta mgl32.Quat) bool {
	if root == nil {
		return false
	}
	worldDeltas := make(map[EntityId]mgl32.Quat, len(aimBones))
	weights := authoredAimWeights(cmd, heldAssetRoot, rig, aimBones)
	applied := false
	for index, bone := range aimBones {
		local, _ := cmd.GetComponent(bone, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
		if local == nil {
			continue
		}
		weight := weights[index]
		parentRotation := root.Rotation
		parentDelta := mgl32.QuatIdent()
		if parent, exists := parentForEntity(cmd, bone); exists {
			if transform, _ := cmd.GetComponent(parent.Entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent); transform != nil {
				parentRotation = transform.Rotation
			}
			if delta, exists := worldDeltas[parent.Entity]; exists {
				parentDelta = delta
			}
		}
		parentRotation = parentDelta.Mul(parentRotation).Normalize()
		boneDelta := mgl32.QuatSlerp(mgl32.QuatIdent(), worldDelta, weight)
		localDelta := parentRotation.Inverse().Mul(boneDelta).Mul(parentRotation).Normalize()
		local.Rotation = localDelta.Mul(local.Rotation).Normalize()
		worldDeltas[bone] = boneDelta.Mul(parentDelta).Normalize()
		applied = true
	}
	return applied
}

func authoredAimWeights(cmd *Commands, heldAssetRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId) []float32 {
	weights := make([]float32, len(aimBones))
	for index := range aimBones {
		weights[index] = float32(1) / float32(len(aimBones))
		if index < len(rig.BoneWeights) {
			weights[index] = rig.BoneWeights[index]
		}
	}
	if heldAssetRoot == 0 {
		return weights
	}
	drivingWeight := float32(0)
	for index, bone := range aimBones {
		if isEntityOrDescendantOf(cmd, heldAssetRoot, bone) {
			drivingWeight += weights[index]
		}
	}
	if drivingWeight <= 1e-8 {
		return weights
	}
	for index, bone := range aimBones {
		if isEntityOrDescendantOf(cmd, heldAssetRoot, bone) {
			weights[index] /= drivingWeight
		}
	}
	return weights
}

type authoredAimFrameState struct {
	Position  mgl32.Vec3
	Rotation  mgl32.Quat
	Direction mgl32.Vec3
}

func authoredAimFrame(cmd *Commands, heldAssetRoot EntityId, rig content.CharacterAimRigDef) (authoredAimFrameState, bool) {
	if attachment, _ := cmd.GetComponent(heldAssetRoot, reflect.TypeOf(AuthoredAssetAttachmentComponent{})).(*AuthoredAssetAttachmentComponent); attachment != nil && attachment.AimMarker != 0 && attachment.AimFrame != nil {
		marker, _ := cmd.GetComponent(attachment.AimMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
		if marker != nil {
			frame := attachment.AimFrame.Frame
			position := marker.Position.Add(marker.Rotation.Rotate(mgl32.Vec3{
				frame.Position[0] * marker.Scale.X(),
				frame.Position[1] * marker.Scale.Y(),
				frame.Position[2] * marker.Scale.Z(),
			}))
			rotation := marker.Rotation.Mul(contentQuat(frame.Rotation)).Normalize()
			return authoredAimFrameState{Position: position, Rotation: rotation, Direction: rotation.Rotate(mgl32.Vec3{0, 0, -1}).Normalize()}, true
		}
	}
	markerKind := rig.MuzzleMarkerKind
	if markerKind == "" {
		markerKind = content.AssetMarkerKindMuzzle
	}
	marker, ok := FindFirstAuthoredAssetMarkerByKind(cmd, heldAssetRoot, markerKind)
	if !ok || !authoredAimMarkerCalibrated(marker.Marker.Tags) {
		return authoredAimFrameState{}, false
	}
	rotation := marker.Transform.Rotation.Normalize()
	return authoredAimFrameState{Position: marker.Transform.Position, Rotation: rotation, Direction: rotation.Rotate(mgl32.Vec3{0, 0, -1}).Normalize()}, true
}

// authoredAimFrameForDirection preserves a complete orientation while aiming.
// QuatBetweenVectors matches only forward and leaves roll arbitrary, which
// twists authored weapon-hold poses when the target is off-axis.
func authoredAimFrameForDirection(direction, referenceUp, fallbackUp mgl32.Vec3) (mgl32.Quat, bool) {
	if direction.LenSqr() <= 1e-8 {
		return mgl32.Quat{}, false
	}
	forward := direction.Normalize()
	up := referenceUp.Sub(forward.Mul(referenceUp.Dot(forward)))
	if up.LenSqr() <= 1e-8 {
		up = fallbackUp.Sub(forward.Mul(fallbackUp.Dot(forward)))
	}
	if up.LenSqr() <= 1e-8 {
		return mgl32.Quat{}, false
	}
	up = up.Normalize()
	right := forward.Cross(up).Normalize()
	return mgl32.Mat4ToQuat(mgl32.Mat4FromCols(
		mgl32.Vec4{right.X(), right.Y(), right.Z(), 0},
		mgl32.Vec4{up.X(), up.Y(), up.Z(), 0},
		mgl32.Vec4{-forward.X(), -forward.Y(), -forward.Z(), 0},
		mgl32.Vec4{0, 0, 0, 1},
	)).Normalize(), true
}

func authoredAimAxes(rig content.CharacterAimRigDef) (mgl32.Vec3, mgl32.Vec3, mgl32.Vec3, bool) {
	forward := mgl32.Vec3{rig.ForwardAxis[0], rig.ForwardAxis[1], rig.ForwardAxis[2]}
	if forward.LenSqr() <= 1e-8 {
		forward = mgl32.Vec3{0, 0, -1}
	}
	forward = forward.Normalize()
	up := mgl32.Vec3{rig.UpAxis[0], rig.UpAxis[1], rig.UpAxis[2]}
	if up.LenSqr() <= 1e-8 {
		up = mgl32.Vec3{0, 1, 0}
	}
	up = up.Sub(forward.Mul(up.Dot(forward)))
	if up.LenSqr() <= 1e-8 {
		return mgl32.Vec3{}, mgl32.Vec3{}, mgl32.Vec3{}, false
	}
	up = up.Normalize()
	return forward, up, forward.Cross(up).Normalize(), true
}

func authoredAimMarkerCalibrated(tags []string) bool {
	for _, tag := range tags {
		if strings.EqualFold(strings.TrimSpace(tag), "aim:calibrated") {
			return true
		}
	}
	return false
}

func authoredAimYaw(direction, forward, right mgl32.Vec3) float32 {
	return float32(math.Atan2(float64(direction.Dot(right)), float64(direction.Dot(forward))) * 180 / math.Pi)
}

func authoredAimPitch(direction, up mgl32.Vec3) float32 {
	return float32(math.Asin(float64(max(-1, min(1, direction.Dot(up))))) * 180 / math.Pi)
}

func authoredAimNormalize(degrees float32) float32 {
	for degrees > 180 {
		degrees -= 360
	}
	for degrees < -180 {
		degrees += 360
	}
	return degrees
}

func authoredAimClamp(value, limit float32) float32 {
	if limit <= 0 {
		return value
	}
	if value < -limit {
		return -limit
	}
	if value > limit {
		return limit
	}
	return value
}
