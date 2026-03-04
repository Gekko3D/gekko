package gekko

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

func voxelRendering(rs *voxelRenderState, gpuState *GpuState) {
	if len(rs.voxelInstancesUniform) == 0 {
		//nothing to render
		return
	}
	nextTexture, err := gpuState.surface.GetCurrentTexture()
	if err != nil {
		panic(err)
	}
	view, err := nextTexture.CreateView(nil)
	if err != nil {
		panic(err)
	}
	defer view.Release()
	encoder, err := gpuState.device.CreateCommandEncoder(nil)
	if err != nil {
		panic(err)
	}
	defer encoder.Release()

	// Build visible-instance index list via CPU frustum culling
	{
		// We use clip-space plane-out test against the camera ViewProjection matrix
		vp := rs.cameraUniform.ViewProjMx
		visible := make([]uint32, 0, len(rs.instanceAABBsUniform))
		for i := range rs.instanceAABBsUniform {
			if aabbIntersectsFrustumClip(vp, rs.instanceAABBsUniform[i]) {
				visible = append(visible, uint32(i))
			}
		}
		// Update visible list parameters and buffers
		rs.visibleListParametersUniform.Count = uint32(len(visible))
		_ = gpuState.queue.WriteBuffer(rs.visibleListParametersBuffer, 0, toBufferBytes(rs.visibleListParametersUniform))
		if len(visible) > 0 {
			_ = gpuState.queue.WriteBuffer(rs.visibleInstanceIndicesBuffer, 0, toBufferBytes(visible))
		}
	}

	// Update GPU buffers before compute and render
	err = gpuState.queue.WriteBuffer(rs.renderParametersBuffer, 0, toBufferBytes(rs.renderParametersUniform))
	err = gpuState.queue.WriteBuffer(rs.cameraBuffer, 0, toBufferBytes(rs.cameraUniform))
	err = gpuState.queue.WriteBuffer(rs.transformsBuffer, 0, toBufferBytes(rs.transformsUniforms))
	err = gpuState.queue.WriteBuffer(rs.instanceAABBsBuffer, 0, toBufferBytes(rs.instanceAABBsUniform))

	if rs.isVoxelPoolUpdated {
		err = gpuState.queue.WriteBuffer(rs.voxelInstancesBuffer, 0, toBufferBytes(rs.voxelInstancesUniform))
		err = gpuState.queue.WriteBuffer(rs.macroIndexPoolBuffer, 0, toBufferBytes(rs.macroIndexPoolUniform))
		err = gpuState.queue.WriteBuffer(rs.brickPoolBuffer, 0, toBufferBytes(rs.brickPoolUniform))
		err = gpuState.queue.WriteBuffer(rs.voxelPoolBuffer, 0, toBufferBytes(rs.voxelPoolUniform))
		err = gpuState.queue.WriteBuffer(rs.palettesBuffer, 0, toBufferBytes(rs.palettesUniform))
		rs.isVoxelPoolUpdated = false
	}
	if err != nil {
		panic(err)
	}

	// Compute raycasting into output texture before render pass
	if rs.computePipeline != nil && rs.voxelComputeBindGroup != nil {
		computePass := encoder.BeginComputePass(nil)
		computePass.SetPipeline(rs.computePipeline)
		computePass.SetBindGroup(0, rs.voxelComputeBindGroup, nil)
		wgX := (rs.renderParametersUniform.WindowWidth + uint32(7)) / uint32(8)
		wgY := (rs.renderParametersUniform.WindowHeight + uint32(7)) / uint32(8)
		computePass.DispatchWorkgroups(wgX, wgY, 1)
		err = computePass.End()
		if err != nil {
			panic(err)
		}
	}

	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       view,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0.1, G: 0.2, B: 0.3, A: 1.0},
			},
		},
	})
	defer renderPass.Release()

	renderPass.SetPipeline(rs.blitPipeline)
	renderPass.SetIndexBuffer(rs.indexBuffer, wgpu.IndexFormatUint16, 0, wgpu.WholeSize)
	renderPass.SetVertexBuffer(0, rs.vertexBuffer, 0, wgpu.WholeSize)
	// Bind group(0) for render pass; fs_main uses @group(0), @binding(9)
	if rs.blitBindGroup != nil {
		renderPass.SetBindGroup(0, rs.blitBindGroup, nil)
	}
	renderPass.DrawIndexed(rs.vertexCount, 1, 0, 0, 0)

	err = renderPass.End()
	if err != nil {
		panic(err)
	}

	cmdBuffer, err := encoder.Finish(nil)
	if err != nil {
		panic(err)
	}
	defer cmdBuffer.Release()

	gpuState.queue.Submit(cmdBuffer)
	gpuState.surface.Present()
}

func updateModelUniforms(cmd *Commands, rState *voxelRenderState) {
	MakeQuery1[TransformComponent](cmd).Map(
		func(entityId EntityId, transform *TransformComponent) bool {
			if voxModelId, ok := rState.entityVoxInstanceIds[entityId]; ok {
				uniform := rState.transformsUniforms[voxModelId]
				modelMx := buildModelMatrix(transform)
				uniform.ModelMx = modelMx
				uniform.InvModelMx = modelMx.Inv()
				rState.transformsUniforms[voxModelId] = uniform

				// recompute world-space AABB for this instance
				mg := rState.voxelInstancesUniform[voxModelId].MacroGrid
				localMax := mgl32.Vec3{
					float32(mg.Size[0] * mg.BrickSize[0]),
					float32(mg.Size[1] * mg.BrickSize[1]),
					float32(mg.Size[2] * mg.BrickSize[2]),
				}
				minV, maxV := computeWorldAABB(modelMx, mgl32.Vec3{0, 0, 0}, localMax)
				rState.instanceAABBsUniform[voxModelId] = aabbUniform{
					Min: mgl32.Vec4{minV.X(), minV.Y(), minV.Z(), 0},
					Max: mgl32.Vec4{maxV.X(), maxV.Y(), maxV.Z(), 0},
				}
			}
			return true
		})
}

func buildModelMatrix(t *TransformComponent) mgl32.Mat4 {
	return mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z()).
		Mul4(t.Rotation.Mat4()).
		Mul4(mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z()))
}

func updateCameraUniform(cmd *Commands, rState *voxelRenderState) {
	MakeQuery1[CameraComponent](cmd).Map(
		func(entityId EntityId, camera *CameraComponent) bool {
			camMx := buildCameraMatrix(camera)
			rState.cameraUniform.ViewProjMx = camMx
			rState.cameraUniform.InvViewProjMx = camMx.Inv()
			rState.cameraUniform.Position = mgl32.Vec4{camera.Position[0], camera.Position[1], camera.Position[2], 0.0}
			//TODO add support of multiple cameras
			return false
		})
}

func buildCameraMatrix(c *CameraComponent) mgl32.Mat4 {
	view := mgl32.LookAtV(
		c.Position,
		c.LookAt,
		c.Up,
	)
	projection := mgl32.Perspective(
		c.Fov,
		c.Aspect,
		c.Near,
		c.Far,
	)
	return projection.Mul4(view)
}

// compute axis-aligned world-space AABB by transforming 8 corners of a local box
func computeWorldAABB(model mgl32.Mat4, localMin, localMax mgl32.Vec3) (mgl32.Vec3, mgl32.Vec3) {
	corners := [8]mgl32.Vec3{
		{localMin.X(), localMin.Y(), localMin.Z()},
		{localMax.X(), localMin.Y(), localMin.Z()},
		{localMin.X(), localMax.Y(), localMin.Z()},
		{localMin.X(), localMin.Y(), localMax.Z()},
		{localMax.X(), localMax.Y(), localMin.Z()},
		{localMax.X(), localMin.Y(), localMax.Z()},
		{localMin.X(), localMax.Y(), localMax.Z()},
		{localMax.X(), localMax.Y(), localMax.Z()},
	}
	p0 := model.Mul4x1(mgl32.Vec4{corners[0].X(), corners[0].Y(), corners[0].Z(), 1})
	minV := p0.Vec3()
	maxV := p0.Vec3()
	for i := 1; i < 8; i++ {
		p := model.Mul4x1(mgl32.Vec4{corners[i].X(), corners[i].Y(), corners[i].Z(), 1})
		v := p.Vec3()
		if v[0] < minV[0] {
			minV[0] = v[0]
		}
		if v[1] < minV[1] {
			minV[1] = v[1]
		}
		if v[2] < minV[2] {
			minV[2] = v[2]
		}
		if v[0] > maxV[0] {
			maxV[0] = v[0]
		}
		if v[1] > maxV[1] {
			maxV[1] = v[1]
		}
		if v[2] > maxV[2] {
			maxV[2] = v[2]
		}
	}
	return minV, maxV
}

// Return true if the AABB intersects the camera frustum defined by ViewProjection matrix.
// We do a conservative clip-space plane-out test: if all 8 corners are outside any clip plane, it's culled.
func aabbIntersectsFrustumClip(viewProj mgl32.Mat4, aabb aabbUniform) bool {
	min := aabb.Min.Vec3()
	max := aabb.Max.Vec3()
	corners := [8]mgl32.Vec4{
		{min.X(), min.Y(), min.Z(), 1},
		{max.X(), min.Y(), min.Z(), 1},
		{min.X(), max.Y(), min.Z(), 1},
		{min.X(), min.Y(), max.Z(), 1},
		{max.X(), max.Y(), min.Z(), 1},
		{max.X(), min.Y(), max.Z(), 1},
		{min.X(), max.Y(), max.Z(), 1},
		{max.X(), max.Y(), max.Z(), 1},
	}
	clip := [8]mgl32.Vec4{}
	for i := 0; i < 8; i++ {
		clip[i] = viewProj.Mul4x1(corners[i])
	}
	// Left: x < -w
	allOutside := true
	for i := 0; i < 8; i++ {
		if clip[i][0] >= -clip[i][3] {
			allOutside = false
			break
		}
	}
	if allOutside {
		return false
	}
	// Right: x > w
	allOutside = true
	for i := 0; i < 8; i++ {
		if clip[i][0] <= clip[i][3] {
			allOutside = false
			break
		}
	}
	if allOutside {
		return false
	}
	// Bottom: y < -w
	allOutside = true
	for i := 0; i < 8; i++ {
		if clip[i][1] >= -clip[i][3] {
			allOutside = false
			break
		}
	}
	if allOutside {
		return false
	}
	// Top: y > w
	allOutside = true
	for i := 0; i < 8; i++ {
		if clip[i][1] <= clip[i][3] {
			allOutside = false
			break
		}
	}
	if allOutside {
		return false
	}
	// Near: z < -w (OpenGL-style)
	allOutside = true
	for i := 0; i < 8; i++ {
		if clip[i][2] >= -clip[i][3] {
			allOutside = false
			break
		}
	}
	if allOutside {
		return false
	}
	// Far: z > w
	allOutside = true
	for i := 0; i < 8; i++ {
		if clip[i][2] <= clip[i][3] {
			allOutside = false
			break
		}
	}
	if allOutside {
		return false
	}
	return true
}
