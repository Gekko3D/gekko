package gekko

import (
	"math"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/go-gl/mathgl/mgl32"
)

const DefaultDecalNormalCutoff float32 = 0.5

// DecalInstance is one retained surface projector submitted to VoxelRT.
// Its local +Z direction faces the intended receiver normal.
type DecalInstance struct {
	Position     mgl32.Vec3
	Rotation     mgl32.Quat
	HalfExtents  [3]float32
	Color        [4]float32
	NormalCutoff float32

	Texture     AssetId
	SpriteIndex uint32
	AtlasCols   uint32
	AtlasRows   uint32
}

type decalSyncItem struct {
	instance app_rt.DecalInstanceInput
	atlasKey string
}

// SetRuntimeDecals replaces the current retained decal list. It copies decals
// so callers may reuse their slice after this call.
func (state *VoxelRtState) SetRuntimeDecals(decals []DecalInstance) {
	if state == nil {
		return
	}
	state.runtimeDecals = append(state.runtimeDecals[:0], decals...)
}

// ClearRuntimeDecals removes every retained renderer decal.
func (state *VoxelRtState) ClearRuntimeDecals() {
	if state != nil {
		state.runtimeDecals = nil
	}
}

func runtimeDecalInputs(decals []DecalInstance) ([]app_rt.DecalInstanceInput, []app_rt.DecalBatchInput) {
	itemsByAtlas := make(map[string][]decalSyncItem)
	atlasOrder := make([]string, 0)
	for _, decal := range decals {
		item, ok := decalSyncItemFromInstance(decal)
		if !ok {
			continue
		}
		if _, seen := itemsByAtlas[item.atlasKey]; !seen {
			atlasOrder = append(atlasOrder, item.atlasKey)
		}
		itemsByAtlas[item.atlasKey] = append(itemsByAtlas[item.atlasKey], item)
	}

	instances := make([]app_rt.DecalInstanceInput, 0, len(decals))
	batches := make([]app_rt.DecalBatchInput, 0, len(atlasOrder))
	for _, atlasKey := range atlasOrder {
		items := itemsByAtlas[atlasKey]
		batches = append(batches, app_rt.DecalBatchInput{
			AtlasKey:      atlasKey,
			FirstInstance: uint32(len(instances)),
			InstanceCount: uint32(len(items)),
		})
		for _, item := range items {
			instances = append(instances, item.instance)
		}
	}
	return instances, batches
}

func decalSyncItemFromInstance(decal DecalInstance) (decalSyncItem, bool) {
	if !decalInstanceFinite(decal) || !decalInstanceHasSurface(decal) ||
		decal.AtlasCols == 0 || decal.AtlasRows == 0 ||
		uint64(decal.SpriteIndex) >= uint64(decal.AtlasCols)*uint64(decal.AtlasRows) {
		return decalSyncItem{}, false
	}

	rotation, ok := normalizedDecalRotation(decal.Rotation)
	if !ok {
		return decalSyncItem{}, false
	}
	return decalSyncItem{
		atlasKey: decalAtlasKey(decal.Texture),
		instance: app_rt.DecalInstanceInput{
			Position:    [4]float32{decal.Position.X(), decal.Position.Y(), decal.Position.Z()},
			Rotation:    [4]float32{rotation.V.X(), rotation.V.Y(), rotation.V.Z(), rotation.W},
			HalfExtents: [4]float32{decal.HalfExtents[0], decal.HalfExtents[1], decal.HalfExtents[2], clampDecalNormalCutoff(decal.NormalCutoff)},
			Color:       decal.Color,
			Atlas:       [4]uint32{decal.SpriteIndex, decal.AtlasCols, decal.AtlasRows},
		},
	}, true
}

func decalInstanceFinite(decal DecalInstance) bool {
	for _, value := range decal.Position {
		if !decalFinite(value) {
			return false
		}
	}
	for _, value := range []float32{decal.Rotation.V.X(), decal.Rotation.V.Y(), decal.Rotation.V.Z(), decal.Rotation.W, decal.NormalCutoff} {
		if !decalFinite(value) {
			return false
		}
	}
	for _, value := range decal.HalfExtents {
		if !decalFinite(value) {
			return false
		}
	}
	for _, value := range decal.Color {
		if !decalFinite(value) {
			return false
		}
	}
	return true
}

func decalInstanceHasSurface(decal DecalInstance) bool {
	return decal.HalfExtents[0] > 0 && decal.HalfExtents[1] > 0 && decal.HalfExtents[2] > 0 && decal.Color[3] > 0
}

func normalizedDecalRotation(rotation mgl32.Quat) (mgl32.Quat, bool) {
	x, y, z, w := float64(rotation.V.X()), float64(rotation.V.Y()), float64(rotation.V.Z()), float64(rotation.W)
	lengthSquared := x*x + y*y + z*z + w*w
	if lengthSquared == 0 {
		return mgl32.Quat{}, false
	}
	return rotation.Scale(float32(1 / math.Sqrt(lengthSquared))), true
}

func clampDecalNormalCutoff(cutoff float32) float32 {
	return min(1, max(-1, cutoff))
}

func decalAtlasKey(id AssetId) string {
	if id == (AssetId{}) {
		return ""
	}
	return id.String()
}

func decalFinite(value float32) bool {
	return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
}
