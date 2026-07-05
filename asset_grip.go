package gekko

import (
	"math"
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// ResetAuthoredAssetAttachmentMount restores an attached asset's authored
// mount from its stable host marker. It allows presentation code to reparent
// a prop for procedural aiming without losing the authored resting pose.
func ResetAuthoredAssetAttachmentMount(cmd *Commands, root EntityId) bool {
	if cmd == nil || root == 0 {
		return false
	}
	attachment, _ := cmd.GetComponent(root, reflect.TypeOf(AuthoredAssetAttachmentComponent{})).(*AuthoredAssetAttachmentComponent)
	if attachment == nil || attachment.ParentMarker == 0 {
		return false
	}
	TransformHierarchySystem(cmd)
	marker, _ := cmd.GetComponent(attachment.ParentMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if marker == nil {
		return false
	}
	mount := AssetTransformFromDef(attachment.MountTransform)
	world := TransformComponent{
		Position: marker.Position.Add(marker.Rotation.Rotate(mgl32.Vec3{
			mount.Position.X() * marker.Scale.X(),
			mount.Position.Y() * marker.Scale.Y(),
			mount.Position.Z() * marker.Scale.Z(),
		})),
		Rotation: marker.Rotation.Mul(mount.Rotation).Normalize(),
		Scale:    mgl32.Vec3{marker.Scale.X() * mount.Scale.X(), marker.Scale.Y() * mount.Scale.Y(), marker.Scale.Z() * mount.Scale.Z()},
	}
	return setEntityWorldTransform(cmd, root, world)
}

// ApplyAuthoredAssetAttachmentAimOffset shifts a procedurally aimed asset in
// its authored aim-frame basis. It is applied after aiming, so forward stays
// forward regardless of the host marker's local axes.
func ApplyAuthoredAssetAttachmentAimOffset(cmd *Commands, root EntityId) bool {
	if cmd == nil || root == 0 {
		return false
	}
	attachment, _ := cmd.GetComponent(root, reflect.TypeOf(AuthoredAssetAttachmentComponent{})).(*AuthoredAssetAttachmentComponent)
	if attachment == nil || attachment.AimOffset == nil {
		return true
	}
	TransformHierarchySystem(cmd)
	frame, ok := authoredAimFrame(cmd, root, content.CharacterAimRigDef{})
	if !ok {
		return false
	}
	rootWorld, _ := cmd.GetComponent(root, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if rootWorld == nil {
		return false
	}
	offset := attachment.AimOffset
	worldOffset := frame.Rotation.Rotate(mgl32.Vec3{offset[0], offset[1], offset[2]})
	return setEntityWorldTransform(cmd, root, TransformComponent{
		Position: rootWorld.Position.Add(worldOffset),
		Rotation: rootWorld.Rotation,
		Scale:    rootWorld.Scale,
	})
}

// AimAuthoredAssetAtDirection turns a detached presentation asset toward a
// direction while preserving the calibrated aim frame's position. It is
// source-neutral: the asset supplies the frame through its attachment or
// muzzle marker.
func AimAuthoredAssetAtDirection(cmd *Commands, root EntityId, targetDirection, referenceUp mgl32.Vec3) bool {
	return aimAuthoredAssetAtDirection(cmd, root, 0, targetDirection, referenceUp)
}

// AimAuthoredAssetAtDirectionAroundMarker aims an asset while preserving a
// stable authored pivot marker. Weapon rigs use their primary grip as that
// pivot, so aiming cannot pull the weapon out of the holding hand.
func AimAuthoredAssetAtDirectionAroundMarker(cmd *Commands, root, pivotMarker EntityId, targetDirection, referenceUp mgl32.Vec3) bool {
	return aimAuthoredAssetAtDirection(cmd, root, pivotMarker, targetDirection, referenceUp)
}

// AuthoredAssetAttachmentGripPosition returns a grip target after applying
// the attachment-owned calibration frame, if one exists for the marker.
func AuthoredAssetAttachmentGripPosition(cmd *Commands, root EntityId, markerKind string) (mgl32.Vec3, bool) {
	if cmd == nil || root == 0 || markerKind == "" {
		return mgl32.Vec3{}, false
	}
	marker, ok := FindFirstAuthoredAssetMarkerByKind(cmd, root, markerKind)
	if !ok {
		return mgl32.Vec3{}, false
	}
	attachment, _ := cmd.GetComponent(root, reflect.TypeOf(AuthoredAssetAttachmentComponent{})).(*AuthoredAssetAttachmentComponent)
	if attachment == nil {
		return marker.Transform.Position, true
	}
	for _, grip := range attachment.GripFrames {
		if grip.MarkerID != marker.Ref.ItemID {
			continue
		}
		return marker.Transform.Position.Add(marker.Transform.Rotation.Rotate(mgl32.Vec3{
			grip.Frame.Position[0] * marker.Transform.Scale.X(),
			grip.Frame.Position[1] * marker.Transform.Scale.Y(),
			grip.Frame.Position[2] * marker.Transform.Scale.Z(),
		})), true
	}
	return marker.Transform.Position, true
}

func aimAuthoredAssetAtDirection(cmd *Commands, root, pivotMarker EntityId, targetDirection, referenceUp mgl32.Vec3) bool {
	if cmd == nil || root == 0 || targetDirection.LenSqr() <= 1e-8 {
		return false
	}
	TransformHierarchySystem(cmd)
	frame, ok := authoredAimFrame(cmd, root, content.CharacterAimRigDef{})
	if !ok {
		return false
	}
	up := referenceUp
	if up.LenSqr() <= 1e-8 {
		up = frame.Rotation.Rotate(mgl32.Vec3{0, 1, 0})
	}
	targetFrame, ok := authoredAimFrameForDirection(targetDirection, up, frame.Rotation.Rotate(mgl32.Vec3{0, 1, 0}))
	if !ok {
		return false
	}
	delta := targetFrame.Mul(frame.Rotation.Inverse()).Normalize()
	rootWorld, _ := cmd.GetComponent(root, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if rootWorld == nil {
		return false
	}
	pivot := frame.Position
	if pivotMarker != 0 {
		marker, _ := cmd.GetComponent(pivotMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
		if marker == nil {
			return false
		}
		pivot = marker.Position
	}
	return setEntityWorldTransform(cmd, root, TransformComponent{
		Position: pivot.Add(delta.Rotate(rootWorld.Position.Sub(pivot))),
		Rotation: delta.Mul(rootWorld.Rotation).Normalize(),
		Scale:    rootWorld.Scale,
	})
}

// ApplyAuthoredTwoBoneGrip positions a character hand marker on a weapon grip
// marker by rotating its shoulder and elbow bones after animation sampling.
// The three-bone chain is discovered from authored hierarchy, not bone names.
func ApplyAuthoredTwoBoneGrip(cmd *Commands, handMarker, gripMarker EntityId) bool {
	if cmd == nil || handMarker == 0 || gripMarker == 0 {
		return false
	}
	grip, _ := cmd.GetComponent(gripMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if grip == nil {
		return false
	}
	return ApplyAuthoredTwoBoneGripAt(cmd, handMarker, grip.Position)
}

// ApplyAuthoredTwoBoneGripAt positions a character hand marker at a world
// target. The target may be a raw asset marker or an attachment calibration.
func ApplyAuthoredTwoBoneGripAt(cmd *Commands, handMarker EntityId, gripPosition mgl32.Vec3) bool {
	if cmd == nil || handMarker == 0 {
		return false
	}
	end, ok := parentForEntity(cmd, handMarker)
	if !ok {
		return false
	}
	mid, ok := parentForEntity(cmd, end.Entity)
	if !ok {
		return false
	}
	shoulder, ok := parentForEntity(cmd, mid.Entity)
	if !ok {
		return false
	}
	TransformHierarchySystem(cmd)
	rootWorld, _ := cmd.GetComponent(shoulder.Entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	midWorld, _ := cmd.GetComponent(mid.Entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	handWorld, _ := cmd.GetComponent(handMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if rootWorld == nil || midWorld == nil || handWorld == nil {
		return false
	}
	// A calibrated grip can already coincide with the animated hand. Avoid
	// solving a zero-length correction, which would make the elbow rotation
	// undefined and propagate NaNs through the presentation hierarchy.
	if handWorld.Position.Sub(gripPosition).LenSqr() <= 1e-8 {
		return true
	}
	first := midWorld.Position.Sub(rootWorld.Position)
	second := handWorld.Position.Sub(midWorld.Position)
	l1, l2 := first.Len(), second.Len()
	toGrip := gripPosition.Sub(rootWorld.Position)
	if l1 <= 1e-5 || l2 <= 1e-5 || toGrip.LenSqr() <= 1e-8 {
		return false
	}
	distance := min(max(toGrip.Len(), float32(math.Abs(float64(l1-l2)))+1e-4), l1+l2-1e-4)
	direction := toGrip.Normalize()
	bend := first.Sub(direction.Mul(first.Dot(direction)))
	if bend.LenSqr() <= 1e-8 {
		bend = rootWorld.Rotation.Rotate(mgl32.Vec3{0, 0, 1}).Sub(direction.Mul(rootWorld.Rotation.Rotate(mgl32.Vec3{0, 0, 1}).Dot(direction)))
	}
	if bend.LenSqr() <= 1e-8 {
		return false
	}
	bend = bend.Normalize()
	cosine := max(-1, min(1, (l1*l1+distance*distance-l2*l2)/(2*l1*distance)))
	desiredMid := rootWorld.Position.Add(direction.Mul(cosine * l1)).Add(bend.Mul(float32(math.Sqrt(float64(max(0, 1-cosine*cosine)))) * l1))
	if !setEntityWorldRotation(cmd, shoulder.Entity, mgl32.QuatBetweenVectors(first, desiredMid.Sub(rootWorld.Position)).Mul(rootWorld.Rotation).Normalize()) {
		return false
	}
	TransformHierarchySystem(cmd)
	midWorld, _ = cmd.GetComponent(mid.Entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	handWorld, _ = cmd.GetComponent(handMarker, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if midWorld == nil || handWorld == nil {
		return false
	}
	forearm := handWorld.Position.Sub(midWorld.Position)
	targetForearm := gripPosition.Sub(midWorld.Position)
	if forearm.LenSqr() <= 1e-8 || targetForearm.LenSqr() <= 1e-8 {
		return false
	}
	return setEntityWorldRotation(cmd, mid.Entity, mgl32.QuatBetweenVectors(forearm, targetForearm).Mul(midWorld.Rotation).Normalize())
}

func setEntityWorldTransform(cmd *Commands, entity EntityId, world TransformComponent) bool {
	local, _ := cmd.GetComponent(entity, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent)
	if local == nil {
		return false
	}
	if parent, ok := parentForEntity(cmd, entity); ok {
		parentWorld, _ := cmd.GetComponent(parent.Entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
		if parentWorld == nil || parentWorld.Scale.X() == 0 || parentWorld.Scale.Y() == 0 || parentWorld.Scale.Z() == 0 {
			return false
		}
		local.Position = parentWorld.Rotation.Inverse().Rotate(world.Position.Sub(parentWorld.Position))
		local.Position = mgl32.Vec3{local.Position.X() / parentWorld.Scale.X(), local.Position.Y() / parentWorld.Scale.Y(), local.Position.Z() / parentWorld.Scale.Z()}
		local.Rotation = parentWorld.Rotation.Inverse().Mul(world.Rotation).Normalize()
		local.Scale = mgl32.Vec3{world.Scale.X() / parentWorld.Scale.X(), world.Scale.Y() / parentWorld.Scale.Y(), world.Scale.Z() / parentWorld.Scale.Z()}
		return true
	}
	local.Position, local.Rotation, local.Scale = world.Position, world.Rotation, world.Scale
	return true
}

func setEntityWorldRotation(cmd *Commands, entity EntityId, rotation mgl32.Quat) bool {
	world, _ := cmd.GetComponent(entity, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if world == nil {
		return false
	}
	return setEntityWorldTransform(cmd, entity, TransformComponent{Position: world.Position, Rotation: rotation, Scale: world.Scale})
}
