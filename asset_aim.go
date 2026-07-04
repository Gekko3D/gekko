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
	current, ok := authoredAimFrameDirection(cmd, heldAssetRoot, rig)
	if !ok {
		return false
	}
	root, _ := cmd.GetComponent(characterRoot, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if root == nil {
		return false
	}
	toLocal := root.Rotation.Inverse()
	current = toLocal.Rotate(current).Normalize()
	target := toLocal.Rotate(targetDirection).Normalize()
	forward, up, _, ok := authoredAimAxes(rig)
	if !ok {
		return false
	}
	// The horizontal reference points toward positive camera yaw. The pitch
	// axis points the other way (forward × up), so keep the two distinct.
	yawRight := up.Cross(forward).Normalize()
	yaw := authoredAimNormalize(authoredAimYaw(target, forward, yawRight) - authoredAimYaw(current, forward, yawRight))
	pitch := authoredAimPitch(target, up) - authoredAimPitch(current, up)
	return applyAuthoredAimOffset(cmd, characterRoot, rig, aimBones, yaw, pitch)
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
	return applyAuthoredAimOffset(cmd, characterRoot, rig, aimBones, yaw, pitch)
}

func applyAuthoredAimOffset(cmd *Commands, characterRoot EntityId, rig content.CharacterAimRigDef, aimBones []EntityId, yaw, pitch float32) bool {
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
	worldDeltas := make(map[EntityId]mgl32.Quat, len(aimBones))
	applied := false
	for index, bone := range aimBones {
		local, _ := cmd.GetComponent(bone, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
		if local == nil {
			continue
		}
		weight := float32(1) / float32(len(aimBones))
		if index < len(rig.BoneWeights) {
			weight = rig.BoneWeights[index]
		}
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
		yawAxis := parentRotation.Inverse().Rotate(worldYawAxis).Normalize()
		pitchAxis := parentRotation.Inverse().Rotate(worldPitchAxis).Normalize()
		yawRotation := mgl32.QuatRotate(mgl32.DegToRad(yaw*weight), yawAxis)
		pitchRotation := mgl32.QuatRotate(mgl32.DegToRad(pitch*weight), pitchAxis)
		local.Rotation = yawRotation.Mul(pitchRotation).Mul(local.Rotation).Normalize()
		worldDeltas[bone] = mgl32.QuatRotate(mgl32.DegToRad(yaw*weight), worldYawAxis).
			Mul(mgl32.QuatRotate(mgl32.DegToRad(pitch*weight), worldPitchAxis)).
			Mul(parentDelta).
			Normalize()
		applied = true
	}
	return applied
}

func authoredAimFrameDirection(cmd *Commands, heldAssetRoot EntityId, rig content.CharacterAimRigDef) (mgl32.Vec3, bool) {
	if attachment, _ := cmd.GetComponent(heldAssetRoot, reflect.TypeOf(AuthoredAssetAttachmentComponent{})).(*AuthoredAssetAttachmentComponent); attachment != nil && attachment.AimMarker != 0 && attachment.AimFrame != nil {
		marker, _ := cmd.GetComponent(attachment.AimMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
		if marker != nil {
			frame := AssetLocalTransformFromDef(attachment.AimFrame.Frame)
			return marker.Rotation.Mul(frame.Rotation).Rotate(mgl32.Vec3{0, 0, -1}), true
		}
	}
	markerKind := rig.MuzzleMarkerKind
	if markerKind == "" {
		markerKind = content.AssetMarkerKindMuzzle
	}
	marker, ok := FindFirstAuthoredAssetMarkerByKind(cmd, heldAssetRoot, markerKind)
	if !ok || !authoredAimMarkerCalibrated(marker.Marker.Tags) {
		return mgl32.Vec3{}, false
	}
	return marker.Transform.Rotation.Rotate(mgl32.Vec3{0, 0, -1}), true
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
