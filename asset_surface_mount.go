package gekko

import (
	"reflect"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

// AlignAuthoredAssetSurfaceMount aligns an authored surface_mount marker's
// local +Z axis to a world-space surface normal. It is for world-root assets.
func AlignAuthoredAssetSurfaceMount(cmd *Commands, root EntityId, position, normal mgl32.Vec3) bool {
	if cmd == nil || root == InvalidEntityId || normal.LenSqr() <= 1e-8 {
		return false
	}
	TransformHierarchySystem(cmd)
	mount, ok := FindFirstAuthoredAssetMarkerByKind(cmd, root, content.AssetMarkerKindSurfaceMount)
	if !ok {
		return false
	}
	rootTransform, _ := cmd.GetComponent(root, reflect.TypeOf(TransformComponent{})).(*TransformComponent)
	if rootTransform == nil {
		return false
	}
	relativePosition := rootTransform.Rotation.Inverse().Rotate(mount.Transform.Position.Sub(rootTransform.Position))
	relativeRotation := rootTransform.Rotation.Inverse().Mul(mount.Transform.Rotation).Normalize()
	rotation := mgl32.QuatBetweenVectors(mgl32.Vec3{0, 0, 1}, normal.Normalize()).Mul(relativeRotation.Inverse()).Normalize()
	rootTransform.Position = position.Sub(rotation.Rotate(relativePosition))
	rootTransform.Rotation = rotation
	if local, _ := cmd.GetComponent(root, reflect.TypeOf(LocalTransformComponent{})).(*LocalTransformComponent); local != nil {
		local.Position, local.Rotation, local.Scale = rootTransform.Position, rootTransform.Rotation, rootTransform.Scale
	}
	TransformHierarchySystem(cmd)
	return true
}
