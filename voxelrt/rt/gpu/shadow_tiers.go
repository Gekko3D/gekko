package gpu

import (
	"encoding/binary"
	"hash/fnv"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	shadowAtlasLayerResolution = uint32(1024)
	shadowLayerParamsSizeBytes = 16
	shadowTierCount            = 4
)

type ShadowLayerParams struct {
	Layer               uint32
	ArrayLayer          uint32
	LightIndex          uint32
	CascadeIndex        uint32
	Kind                uint32
	Tier                uint32
	EffectiveResolution uint32
	CadenceFrames       uint32
	UVScale             [2]float32
	LightSignature      uint64
}

type shadowCacheState struct {
	Initialized             bool
	LastUpdatedFrame        uint64
	LastLightSignature      uint64
	LastSceneRevision       uint64
	LastVoxelUploadRevision uint64
}

func shadowTierName(tier uint32) string {
	switch tier {
	case core.ShadowTierHero:
		return "hero"
	case core.ShadowTierNear:
		return "near"
	case core.ShadowTierMedium:
		return "medium"
	case core.ShadowTierFar:
		return "far"
	default:
		return "unknown"
	}
}

func shadowTierResolution(tier uint32) uint32 {
	switch tier {
	case core.ShadowTierHero:
		return 512
	case core.ShadowTierNear:
		return 256
	case core.ShadowTierMedium:
		return 128
	case core.ShadowTierFar:
		return 128
	default:
		return shadowAtlasLayerResolution
	}
}

func pointShadowTierResolution(tier uint32) uint32 {
	switch tier {
	case core.ShadowTierHero:
		return 256
	case core.ShadowTierNear:
		return 128
	case core.ShadowTierMedium:
		return 128
	case core.ShadowTierFar:
		return 128
	default:
		return 128
	}
}

func shadowTierCadence(tier uint32) uint32 {
	switch tier {
	case core.ShadowTierHero:
		return 1
	case core.ShadowTierNear:
		return 2
	case core.ShadowTierMedium:
		return 4
	case core.ShadowTierFar:
		return 8
	default:
		return 1
	}
}

func pointShadowFacesPerFrame(tier uint32) int {
	switch tier {
	case core.ShadowTierHero:
		return 3
	case core.ShadowTierNear:
		return 2
	case core.ShadowTierMedium:
		return 1
	case core.ShadowTierFar:
		return 1
	default:
		return 1
	}
}

func directionalCascadeTier(cascadeIdx uint32) uint32 {
	if cascadeIdx == 0 {
		return core.ShadowTierHero
	}
	return core.ShadowTierNear
}

func classifySpotShadowTier(camPos, lightPos mgl32.Vec3, thresholds [3]float32) uint32 {
	dist := lightPos.Sub(camPos).Len()
	heroDist := thresholds[0]
	nearDist := thresholds[1]
	mediumDist := thresholds[2]
	if heroDist <= 0 {
		heroDist = 24.0
	}
	if nearDist <= heroDist {
		nearDist = 56.0
	}
	if mediumDist <= nearDist {
		mediumDist = 120.0
	}
	switch {
	case dist <= heroDist:
		return core.ShadowTierHero
	case dist <= nearDist:
		return core.ShadowTierNear
	case dist <= mediumDist:
		return core.ShadowTierMedium
	default:
		return core.ShadowTierFar
	}
}

func buildShadowLayerParamsData(params []ShadowLayerParams) []byte {
	if len(params) == 0 {
		return make([]byte, shadowLayerParamsSizeBytes)
	}
	buf := make([]byte, 0, len(params)*shadowLayerParamsSizeBytes)
	for _, p := range params {
		entry := make([]byte, shadowLayerParamsSizeBytes)
		binary.LittleEndian.PutUint32(entry[0:4], math.Float32bits(p.UVScale[0]))
		binary.LittleEndian.PutUint32(entry[4:8], math.Float32bits(p.UVScale[1]))
		binary.LittleEndian.PutUint32(entry[8:12], math.Float32bits(float32(p.EffectiveResolution)))
		invRes := float32(1.0)
		if p.EffectiveResolution > 0 {
			invRes = 1.0 / float32(p.EffectiveResolution)
		}
		binary.LittleEndian.PutUint32(entry[12:16], math.Float32bits(invRes))
		buf = append(buf, entry...)
	}
	return buf
}

func hashShadowSignature(values ...uint32) uint64 {
	h := fnv.New64a()
	var buf [4]byte
	for _, v := range values {
		binary.LittleEndian.PutUint32(buf[:], v)
		_, _ = h.Write(buf[:])
	}
	return h.Sum64()
}

func hashMat4Signature(mat [16]float32, extras ...uint32) uint64 {
	values := make([]uint32, 0, len(mat)+len(extras))
	for _, v := range mat {
		values = append(values, math.Float32bits(v))
	}
	values = append(values, extras...)
	return hashShadowSignature(values...)
}

func (m *GpuBufferManager) ensureShadowCacheCapacity(numLayers uint32) {
	if numLayers == 0 {
		m.ShadowLayerParams = m.ShadowLayerParams[:0]
		m.shadowCacheStates = m.shadowCacheStates[:0]
		m.shadowCachedCascades = m.shadowCachedCascades[:0]
		return
	}
	if uint32(cap(m.ShadowLayerParams)) < numLayers {
		m.ShadowLayerParams = make([]ShadowLayerParams, numLayers)
	} else {
		m.ShadowLayerParams = m.ShadowLayerParams[:numLayers]
	}
	if uint32(cap(m.shadowCacheStates)) < numLayers {
		m.shadowCacheStates = make([]shadowCacheState, numLayers)
	} else {
		m.shadowCacheStates = m.shadowCacheStates[:numLayers]
	}
	if uint32(cap(m.shadowCachedCascades)) < numLayers {
		m.shadowCachedCascades = make([]core.DirectionalShadowCascade, numLayers)
	} else {
		m.shadowCachedCascades = m.shadowCachedCascades[:numLayers]
	}
}
