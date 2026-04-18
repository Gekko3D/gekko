package gpu

import (
	"encoding/binary"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/cogentcore/webgpu/wgpu"
)

const tileLightParamsSizeBytes = 32
const tileLightNearPlaneClamp = 1e-4

func buildTileLightParamsData(screenW, screenH, tilesX, tilesY uint32) []byte {
	buf := make([]byte, tileLightParamsSizeBytes)
	binary.LittleEndian.PutUint32(buf[0:], TiledLightingTileSize)
	binary.LittleEndian.PutUint32(buf[4:], tilesX)
	binary.LittleEndian.PutUint32(buf[8:], tilesY)
	binary.LittleEndian.PutUint32(buf[12:], TiledLightingMaxLightsPerTile)
	binary.LittleEndian.PutUint32(buf[16:], screenW)
	binary.LittleEndian.PutUint32(buf[20:], screenH)
	binary.LittleEndian.PutUint32(buf[24:], tilesX*tilesY)
	binary.LittleEndian.PutUint32(buf[28:], 0)
	return buf
}

func (m *GpuBufferManager) UpdateTiledLightingResources(screenW, screenH uint32) bool {
	if screenW == 0 || screenH == 0 {
		return false
	}

	tilesX := (screenW + TiledLightingTileSize - 1) / TiledLightingTileSize
	tilesY := (screenH + TiledLightingTileSize - 1) / TiledLightingTileSize
	if tilesX == 0 {
		tilesX = 1
	}
	if tilesY == 0 {
		tilesY = 1
	}

	m.TileLightTilesX = tilesX
	m.TileLightTilesY = tilesY

	recreated := false
	if m.ensureBuffer("TileLightParamsBuf", &m.TileLightParamsBuf, buildTileLightParamsData(screenW, screenH, tilesX, tilesY), wgpu.BufferUsageUniform, 0) {
		recreated = true
	}

	headerBytes := int(tilesX * tilesY * 16)
	if m.ensureBuffer("TileLightHeadersBuf", &m.TileLightHeadersBuf, nil, wgpu.BufferUsageStorage, headerBytes) {
		recreated = true
	}

	indexBytes := int(tilesX * tilesY * TiledLightingMaxLightsPerTile * 4)
	if m.ensureBuffer("TileLightIndicesBuf", &m.TileLightIndicesBuf, nil, wgpu.BufferUsageStorage, indexBytes) {
		recreated = true
	}

	return recreated
}

func (m *GpuBufferManager) CreateTiledLightCullBindGroups(pipeline *wgpu.ComputePipeline) {
	if pipeline == nil {
		return
	}
	var err error
	m.TiledLightCullBindGroup0, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.CameraBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.LightsBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}

	m.TiledLightCullBindGroup1, err = m.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: m.TileLightParamsBuf, Size: wgpu.WholeSize},
			{Binding: 1, Buffer: m.TileLightHeadersBuf, Size: wgpu.WholeSize},
			{Binding: 2, Buffer: m.TileLightIndicesBuf, Size: wgpu.WholeSize},
		},
	})
	if err != nil {
		panic(err)
	}
}

func (m *GpuBufferManager) DispatchTiledLightCull(encoder *wgpu.CommandEncoder, pipeline *wgpu.ComputePipeline) {
	if pipeline == nil || m.TiledLightCullBindGroup0 == nil || m.TiledLightCullBindGroup1 == nil {
		return
	}
	wgX := (m.TileLightTilesX + 7) / 8
	wgY := (m.TileLightTilesY + 7) / 8
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(pipeline)
	pass.SetBindGroup(0, m.TiledLightCullBindGroup0, nil)
	pass.SetBindGroup(1, m.TiledLightCullBindGroup1, nil)
	pass.DispatchWorkgroups(wgX, wgY, 1)
	_ = pass.End()
}

type tiledLightCoverage struct {
	fullscreen bool
	visible    bool
	minX       int
	maxX       int
	minY       int
	maxY       int
}

func projectLocalLightCoverage(light core.Light, view, proj mgl32.Mat4, tilesX, tilesY uint32) tiledLightCoverage {
	lightRange := tileMaxf(light.Params[0], 0.0)
	if lightRange <= 0.0 || tilesX == 0 || tilesY == 0 {
		return tiledLightCoverage{}
	}

	pos := mgl32.Vec3{light.Position[0], light.Position[1], light.Position[2]}
	viewPos := view.Mul4x1(pos.Vec4(1.0)).Vec3()

	if viewPos.Len() <= lightRange {
		return tiledLightCoverage{
			fullscreen: true,
			visible:    true,
			minX:       0,
			maxX:       int(tilesX) - 1,
			minY:       0,
			maxY:       int(tilesY) - 1,
		}
	}

	// OpenGL-style view space looks down -Z, so a light is entirely behind the
	// camera when even its closest sphere point stays on or behind the eye plane.
	if viewPos.Z()-lightRange >= 0.0 {
		return tiledLightCoverage{}
	}

	minNDCX := float32(math.Inf(1))
	minNDCY := float32(math.Inf(1))
	maxNDCX := float32(math.Inf(-1))
	maxNDCY := float32(math.Inf(-1))
	visible := false

	for _, sx := range [...]float32{-1, 1} {
		for _, sy := range [...]float32{-1, 1} {
			for _, sz := range [...]float32{-1, 1} {
				corner := mgl32.Vec3{
					viewPos.X() + sx*lightRange,
					viewPos.Y() + sy*lightRange,
					viewPos.Z() + sz*lightRange,
				}
				if corner.Z() >= 0.0 {
					corner[2] = -tileLightNearPlaneClamp
				}

				clip := proj.Mul4x1(corner.Vec4(1.0))
				if math.Abs(float64(clip.W())) < 1e-6 {
					continue
				}

				ndc := clip.Mul(1.0 / clip.W())
				minNDCX = tileMinf(minNDCX, ndc.X())
				minNDCY = tileMinf(minNDCY, ndc.Y())
				maxNDCX = tileMaxf(maxNDCX, ndc.X())
				maxNDCY = tileMaxf(maxNDCY, ndc.Y())
				visible = true
			}
		}
	}

	if !visible {
		return tiledLightCoverage{}
	}
	if maxNDCX < -1.0 || minNDCX > 1.0 || maxNDCY < -1.0 || minNDCY > 1.0 {
		return tiledLightCoverage{}
	}

	minNDCX = clampf(minNDCX, -1.0, 1.0)
	minNDCY = clampf(minNDCY, -1.0, 1.0)
	maxNDCX = clampf(maxNDCX, -1.0, 1.0)
	maxNDCY = clampf(maxNDCY, -1.0, 1.0)

	minU := minNDCX*0.5 + 0.5
	maxU := maxNDCX*0.5 + 0.5
	minV := (-maxNDCY)*0.5 + 0.5
	maxV := (-minNDCY)*0.5 + 0.5

	return tiledLightCoverage{
		visible: true,
		minX:    clampTileIndex(int(math.Floor(float64(minU*float32(tilesX)))), int(tilesX)),
		maxX:    clampTileIndex(int(math.Floor(float64(maxU*float32(tilesX)))), int(tilesX)),
		minY:    clampTileIndex(int(math.Floor(float64(minV*float32(tilesY)))), int(tilesY)),
		maxY:    clampTileIndex(int(math.Floor(float64(maxV*float32(tilesY)))), int(tilesY)),
	}
}

func (m *GpuBufferManager) EstimateTiledLightMetrics(scene *core.Scene, viewProj, invView mgl32.Mat4, camPos mgl32.Vec3) {
	numTiles := int(m.TileLightTilesX * m.TileLightTilesY)
	if numTiles == 0 {
		m.TileLightAvgCount = 0
		m.TileLightMaxCount = 0
		return
	}

	counts := make([]int, numTiles)
	view := invView.Inv()
	proj := viewProj.Mul4(invView)

	addFullscreen := func() {
		for i := range counts {
			if counts[i] < TiledLightingMaxLightsPerTile {
				counts[i]++
			}
		}
	}

	for _, light := range scene.Lights {
		lightType := uint32(light.Params[2])
		if lightType == core.LightTypeDirectional {
			addFullscreen()
			continue
		}

		lightRange := tileMaxf(light.Params[0], 0.0)
		if lightRange <= 0.0 {
			continue
		}

		pos := mgl32.Vec3{light.Position[0], light.Position[1], light.Position[2]}
		if pos.Sub(camPos).Len() <= lightRange {
			addFullscreen()
			continue
		}

		coverage := projectLocalLightCoverage(light, view, proj, m.TileLightTilesX, m.TileLightTilesY)
		if !coverage.visible {
			continue
		}
		if coverage.fullscreen {
			addFullscreen()
			continue
		}

		for ty := coverage.minY; ty <= coverage.maxY; ty++ {
			base := ty * int(m.TileLightTilesX)
			for tx := coverage.minX; tx <= coverage.maxX; tx++ {
				idx := base + tx
				if counts[idx] < TiledLightingMaxLightsPerTile {
					counts[idx]++
				}
			}
		}
	}

	total := 0
	maxCount := 0
	for _, count := range counts {
		total += count
		if count > maxCount {
			maxCount = count
		}
	}
	m.TileLightAvgCount = total / numTiles
	m.TileLightMaxCount = maxCount
}

func clampTileIndex(v, size int) int {
	if size <= 0 {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v >= size {
		return size - 1
	}
	return v
}

func tileAbsf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func tileMaxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func tileMinf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func clampf(v, minV, maxV float32) float32 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}
