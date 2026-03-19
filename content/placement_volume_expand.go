package content

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"

	"github.com/go-gl/mathgl/mgl32"
)

type PlacementVolumeExpandOptions struct {
	LevelDocumentPath string
	MaxInstances      int
}

type PlacementVolumePreviewInstance struct {
	VolumeID  string            `json:"volume_id"`
	AssetPath string            `json:"asset_path"`
	Transform LevelTransformDef `json:"transform"`
}

type PlacementVolumeExpandResult struct {
	Instances      []PlacementVolumePreviewInstance `json:"instances,omitempty"`
	RequestedCount int                              `json:"requested_count"`
	Clamped        bool                             `json:"clamped,omitempty"`
}

type weightedAssetPath struct {
	path   string
	weight float32
}

func ExpandPlacementVolumePreview(volume PlacementVolumeDef, opts PlacementVolumeExpandOptions) (PlacementVolumeExpandResult, error) {
	result := PlacementVolumeExpandResult{}
	if err := validatePlacementVolumeForExpansion(volume); err != nil {
		return result, err
	}

	requestedCount := placementVolumeRequestedCount(volume)
	result.RequestedCount = requestedCount
	if requestedCount <= 0 {
		return result, nil
	}

	maxInstances := opts.MaxInstances
	count := requestedCount
	if maxInstances > 0 && count > maxInstances {
		count = maxInstances
		result.Clamped = true
	}

	assetPaths, err := resolvePlacementVolumeAssetPaths(volume, opts.LevelDocumentPath)
	if err != nil {
		return result, err
	}
	if len(assetPaths) == 0 {
		return result, fmt.Errorf("placement volume %s resolved zero asset paths", volume.ID)
	}

	rng := rand.New(rand.NewSource(placementVolumeSeed(volume)))
	volumeRotation := levelDefQuat(volume.Transform)
	volumePosition := levelDefPosition(volume.Transform)
	result.Instances = make([]PlacementVolumePreviewInstance, 0, count)
	for i := 0; i < count; i++ {
		localOffset := samplePlacementVolumeOffset(rng, volume)
		worldOffset := volumeRotation.Rotate(localOffset)
		result.Instances = append(result.Instances, PlacementVolumePreviewInstance{
			VolumeID:  volume.ID,
			AssetPath: weightedAssetPathChoice(rng, assetPaths),
			Transform: LevelTransformDef{
				Position: vec3ToLevel(volumePosition.Add(worldOffset)),
				Rotation: quatToLevel(volumeRotation),
				Scale:    Vec3{1, 1, 1},
			},
		})
	}

	return result, nil
}

func validatePlacementVolumeForExpansion(volume PlacementVolumeDef) error {
	if strings.TrimSpace(volume.ID) == "" {
		return fmt.Errorf("placement volume id is required")
	}
	switch volume.Kind {
	case PlacementVolumeKindSphere:
		if volume.Radius <= 0 {
			return fmt.Errorf("placement volume %s radius must be positive", volume.ID)
		}
	case PlacementVolumeKindBox:
		if volume.Extents[0] <= 0 || volume.Extents[1] <= 0 || volume.Extents[2] <= 0 {
			return fmt.Errorf("placement volume %s extents must be positive", volume.ID)
		}
	default:
		return fmt.Errorf("placement volume %s kind %q is unsupported", volume.ID, volume.Kind)
	}
	if err := validatePlacementVolumeSource(volume); err != nil {
		return err
	}
	switch volume.Rule.Mode {
	case PlacementVolumeRuleModeCount:
		if volume.Rule.Count <= 0 {
			return fmt.Errorf("placement volume %s count must be positive", volume.ID)
		}
	case PlacementVolumeRuleModeDensity:
		if volume.Rule.DensityPer1000Volume <= 0 {
			return fmt.Errorf("placement volume %s density_per_1000_volume must be positive", volume.ID)
		}
	default:
		return fmt.Errorf("placement volume %s rule mode %q is unsupported", volume.ID, volume.Rule.Mode)
	}
	return nil
}

func resolvePlacementVolumeAssetPaths(volume PlacementVolumeDef, levelDocumentPath string) ([]weightedAssetPath, error) {
	if strings.TrimSpace(volume.AssetPath) != "" {
		return []weightedAssetPath{{
			path:   ResolveDocumentPath(volume.AssetPath, levelDocumentPath),
			weight: 1,
		}}, nil
	}

	assetSetPath := ResolveDocumentPath(volume.AssetSetPath, levelDocumentPath)
	assetSet, err := LoadAssetSet(assetSetPath)
	if err != nil {
		return nil, err
	}
	if validation := ValidateAssetSet(assetSet, AssetSetValidationOptions{DocumentPath: assetSetPath}); validation.HasErrors() {
		return nil, fmt.Errorf("asset set validation failed: %s", validation.Error())
	}
	paths := make([]weightedAssetPath, 0, len(assetSet.Entries))
	for _, entry := range assetSet.Entries {
		paths = append(paths, weightedAssetPath{
			path:   ResolveDocumentPath(entry.AssetPath, assetSetPath),
			weight: entry.Weight,
		})
	}
	return paths, nil
}

func weightedAssetPathChoice(rng *rand.Rand, assetPaths []weightedAssetPath) string {
	if len(assetPaths) == 1 {
		return assetPaths[0].path
	}
	totalWeight := float32(0)
	for _, candidate := range assetPaths {
		totalWeight += candidate.weight
	}
	if totalWeight <= 0 {
		return assetPaths[0].path
	}
	threshold := rng.Float32() * totalWeight
	accum := float32(0)
	for _, candidate := range assetPaths {
		accum += candidate.weight
		if threshold <= accum {
			return candidate.path
		}
	}
	return assetPaths[len(assetPaths)-1].path
}

func placementVolumeRequestedCount(volume PlacementVolumeDef) int {
	switch volume.Rule.Mode {
	case PlacementVolumeRuleModeCount:
		if volume.Rule.Count < 0 {
			return 0
		}
		return volume.Rule.Count
	case PlacementVolumeRuleModeDensity:
		count := int(math.Round(float64(placementVolumePhysicalVolume(volume) * volume.Rule.DensityPer1000Volume / 1000.0)))
		if count < 0 {
			return 0
		}
		return count
	default:
		return 0
	}
}

func placementVolumePhysicalVolume(volume PlacementVolumeDef) float32 {
	switch volume.Kind {
	case PlacementVolumeKindSphere:
		return float32((4.0 / 3.0) * math.Pi * float64(volume.Radius*volume.Radius*volume.Radius))
	case PlacementVolumeKindBox:
		return 8.0 * volume.Extents[0] * volume.Extents[1] * volume.Extents[2]
	default:
		return 0
	}
}

func samplePlacementVolumeOffset(rng *rand.Rand, volume PlacementVolumeDef) mgl32.Vec3 {
	switch volume.Kind {
	case PlacementVolumeKindSphere:
		for {
			sample := mgl32.Vec3{
				rngFloat32Signed(rng),
				rngFloat32Signed(rng),
				rngFloat32Signed(rng),
			}
			if sample.LenSqr() <= 1 {
				return sample.Mul(volume.Radius)
			}
		}
	case PlacementVolumeKindBox:
		return mgl32.Vec3{
			rngRange(rng, -volume.Extents[0], volume.Extents[0]),
			rngRange(rng, -volume.Extents[1], volume.Extents[1]),
			rngRange(rng, -volume.Extents[2], volume.Extents[2]),
		}
	default:
		return mgl32.Vec3{}
	}
}

func validatePlacementVolumeSource(volume PlacementVolumeDef) error {
	hasAsset := strings.TrimSpace(volume.AssetPath) != ""
	hasAssetSet := strings.TrimSpace(volume.AssetSetPath) != ""
	switch {
	case hasAsset && hasAssetSet:
		return fmt.Errorf("placement volume %s must not define both asset_path and asset_set_path", volume.ID)
	case !hasAsset && !hasAssetSet:
		return fmt.Errorf("placement volume %s must define asset_path or asset_set_path", volume.ID)
	default:
		return nil
	}
}

func placementVolumeSeed(volume PlacementVolumeDef) int64 {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(volume.ID))
	return int64(hasher.Sum64()) ^ volume.RandomSeed
}

func levelDefQuat(def LevelTransformDef) mgl32.Quat {
	if def.Rotation == (Quat{}) {
		return mgl32.QuatIdent()
	}
	return mgl32.Quat{
		V: mgl32.Vec3{def.Rotation[0], def.Rotation[1], def.Rotation[2]},
		W: def.Rotation[3],
	}
}

func levelDefPosition(def LevelTransformDef) mgl32.Vec3 {
	return mgl32.Vec3{def.Position[0], def.Position[1], def.Position[2]}
}

func quatToLevel(q mgl32.Quat) Quat {
	return Quat{q.V[0], q.V[1], q.V[2], q.W}
}

func vec3ToLevel(v mgl32.Vec3) Vec3 {
	return Vec3{v[0], v[1], v[2]}
}

func rngFloat32Signed(rng *rand.Rand) float32 {
	return rng.Float32()*2 - 1
}

func rngRange(rng *rand.Rand, minValue, maxValue float32) float32 {
	return minValue + rng.Float32()*(maxValue-minValue)
}
