package gpu

import (
	"encoding/binary"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"

	"github.com/cogentcore/webgpu/wgpu"
)

const tileLightParamsSizeBytes = 32

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

func clipToTileUV(clip mgl32.Vec4) (mgl32.Vec2, bool) {
	if math.Abs(float64(clip.W())) < 1e-5 {
		return mgl32.Vec2{}, false
	}
	ndcX := clip.X() / clip.W()
	ndcY := clip.Y() / clip.W()
	return mgl32.Vec2{
		ndcX*0.5 + 0.5,
		-ndcY*0.5 + 0.5,
	}, true
}

func (m *GpuBufferManager) EstimateTiledLightMetrics(scene *core.Scene, viewProj, invView mgl32.Mat4, camPos mgl32.Vec3) {
	numTiles := int(m.TileLightTilesX * m.TileLightTilesY)
	if numTiles == 0 {
		m.TileLightAvgCount = 0
		m.TileLightMaxCount = 0
		return
	}

	counts := make([]int, numTiles)
	cameraRight := invView.Mul4x1(mgl32.Vec4{1, 0, 0, 0}).Vec3()
	cameraUp := invView.Mul4x1(mgl32.Vec4{0, 1, 0, 0}).Vec3()
	if cameraRight.Len() < 1e-5 {
		cameraRight = mgl32.Vec3{1, 0, 0}
	} else {
		cameraRight = cameraRight.Normalize()
	}
	if cameraUp.Len() < 1e-5 {
		cameraUp = mgl32.Vec3{0, 1, 0}
	} else {
		cameraUp = cameraUp.Normalize()
	}

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

		centerClip := viewProj.Mul4x1(pos.Vec4(1.0))
		rightClip := viewProj.Mul4x1(pos.Add(cameraRight.Mul(lightRange)).Vec4(1.0))
		upClip := viewProj.Mul4x1(pos.Add(cameraUp.Mul(lightRange)).Vec4(1.0))
		centerUV, centerOK := clipToTileUV(centerClip)
		rightUV, rightOK := clipToTileUV(rightClip)
		upUV, upOK := clipToTileUV(upClip)
		if !centerOK || !rightOK || !upOK || centerClip.W() <= 0 || rightClip.W() <= 0 || upClip.W() <= 0 {
			addFullscreen()
			continue
		}

		radiusU := tileMaxf(tileAbsf(rightUV.X()-centerUV.X()), tileAbsf(upUV.X()-centerUV.X()))
		radiusV := tileMaxf(tileAbsf(rightUV.Y()-centerUV.Y()), tileAbsf(upUV.Y()-centerUV.Y()))
		minU := centerUV.X() - radiusU
		maxU := centerUV.X() + radiusU
		minV := centerUV.Y() - radiusV
		maxV := centerUV.Y() + radiusV
		if maxU < 0.0 || minU > 1.0 || maxV < 0.0 || minV > 1.0 {
			continue
		}

		tileMinX := clampTileIndex(int(math.Floor(float64(minU*float32(m.TileLightTilesX)))), int(m.TileLightTilesX))
		tileMaxX := clampTileIndex(int(math.Floor(float64(maxU*float32(m.TileLightTilesX)))), int(m.TileLightTilesX))
		tileMinY := clampTileIndex(int(math.Floor(float64(minV*float32(m.TileLightTilesY)))), int(m.TileLightTilesY))
		tileMaxY := clampTileIndex(int(math.Floor(float64(maxV*float32(m.TileLightTilesY)))), int(m.TileLightTilesY))

		for ty := tileMinY; ty <= tileMaxY; ty++ {
			base := ty * int(m.TileLightTilesX)
			for tx := tileMinX; tx <= tileMaxX; tx++ {
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
