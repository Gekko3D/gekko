package gekko

import (
	"fmt"
	"os"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

func createVoxelRenderState(windowState *WindowState, gpuState *GpuState) *voxelRenderState {
	aspect := float32(windowState.WindowHeight) / float32(windowState.WindowWidth)
	screenQuadVertices := []sqVertex{
		{pos: [4]float32{-1., 1., 0., 1.}, texCoord: [2]float32{0., 0.}},
		{pos: [4]float32{1., 1., 0., 1.}, texCoord: [2]float32{12., 0.}},
		{pos: [4]float32{-1., -1., 0., 1.}, texCoord: [2]float32{0., 12. * aspect}},
		{pos: [4]float32{1., -1., 0., 1.}, texCoord: [2]float32{12., 12. * aspect}},
	}
	screenQuadIndices := []uint16{0, 2, 1, 1, 2, 3}

	shaderData, err := os.ReadFile("gekko/shaders/raycasting.wgsl")
	if err != nil {
		panic(err)
	}
	blitPipeline := createRenderPipeline("voxel", string(shaderData), sqVertex{}, gpuState)

	// Create compute pipeline from the same WGSL (entry: cs_main)
	computeShader, err := gpuState.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "voxel_compute",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: string(shaderData)},
	})
	if err != nil {
		panic(err)
	}
	defer computeShader.Release()

	computePipeline, err := gpuState.device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     computeShader,
			EntryPoint: "cs_main",
		},
	})
	if err != nil {
		panic(err)
	}

	// Create output texture for compute (storage + sample)
	outputTexture, err := gpuState.device.CreateTexture(&wgpu.TextureDescriptor{
		Size: wgpu.Extent3D{
			Width:              uint32(windowState.WindowWidth),
			Height:             uint32(windowState.WindowHeight),
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatRGBA8Unorm,
		Usage:         wgpu.TextureUsageTextureBinding | wgpu.TextureUsageStorageBinding,
	})
	if err != nil {
		panic(err)
	}
	outputTextureView, err := outputTexture.CreateView(nil)
	if err != nil {
		panic(err)
	}

	// Sampler for blitting
	outputSampler, err := gpuState.device.CreateSampler(&wgpu.SamplerDescriptor{
		AddressModeU:  wgpu.AddressModeClampToEdge,
		AddressModeV:  wgpu.AddressModeClampToEdge,
		AddressModeW:  wgpu.AddressModeClampToEdge,
		MagFilter:     wgpu.FilterModeLinear,
		MinFilter:     wgpu.FilterModeLinear,
		MipmapFilter:  wgpu.MipmapFilterModeLinear,
		LodMinClamp:   0.,
		LodMaxClamp:   1.,
		Compare:       wgpu.CompareFunctionUndefined,
		MaxAnisotropy: 1,
	})
	if err != nil {
		panic(err)
	}

	vertexBuffer, indexBuffer := createVertexIndexBuffers(MakeAnySlice(screenQuadVertices), screenQuadIndices, gpuState.device)

	cameraUniform := voxelCameraUniform{
		ViewProjMx:    mgl32.Ident4(),
		InvViewProjMx: mgl32.Ident4().Inv(),
		Position:      mgl32.Vec4{0.0, 0.0, 0.0, 0.0},
	}
	//bindGroup and buffers are defined according to raycasting.wgsl shader code

	return &voxelRenderState{
		screenQuadVertices: screenQuadVertices,
		screenQuadIndices:  screenQuadIndices,
		vertexBuffer:       vertexBuffer,
		indexBuffer:        indexBuffer,
		vertexCount:        uint32(len(screenQuadIndices)),
		blitPipeline:       blitPipeline,
		computePipeline:    computePipeline,
		outputTexture:      outputTexture,
		outputTextureView:  outputTextureView,
		outputSampler:      outputSampler,
		renderParametersUniform: renderParametersUniform{
			WindowWidth:     uint32(windowState.WindowWidth),
			WindowHeight:    uint32(windowState.WindowHeight),
			EmptyBlockValue: EmptyBrickValue,
		},
		cameraUniform:                cameraUniform,
		visibleListParametersUniform: visibleListParametersUniform{Count: 0},
		entityVoxInstanceIds:         map[EntityId]int{},
		instanceIdToEntity:           map[int]EntityId{},
		paletteIds:                   map[AssetId]uint32{},
	}
}

func createBuffers(gpuState *GpuState, rState *voxelRenderState) {
	//we need to create buffers only once
	if nil == rState.cameraBuffer && len(rState.voxelInstancesUniform) > 0 {
		// Ensure voxelPool has at least one element so buffer creation/bindings are valid
		if len(rState.voxelPoolUniform) == 0 {
			rState.voxelPoolUniform = append(rState.voxelPoolUniform, voxelUniform{ColorIndex: 0, Alpha: 0.0})
		}
		rState.renderParametersBuffer = createBuffer("renderParameters", rState.renderParametersUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
		rState.cameraBuffer = createBuffer("camera", rState.cameraUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
		rState.transformsBuffer = createBuffer("transforms", rState.transformsUniforms, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.voxelInstancesBuffer = createBuffer("voxelInstances", rState.voxelInstancesUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.macroIndexPoolBuffer = createBuffer("macroIndexPool", rState.macroIndexPoolUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.brickPoolBuffer = createBuffer("brickPool", rState.brickPoolUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.voxelPoolBuffer = createBuffer("voxelPool", rState.voxelPoolUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.palettesBuffer = createBuffer("palettes", rState.palettesUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.instanceAABBsBuffer = createBuffer("instanceAABBs", rState.instanceAABBsUniform, gpuState, wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)

		// Visible instances list (CPU culling)
		// allocate buffer large enough to hold all instance indices
		rState.visibleInstanceIndices = make([]uint32, len(rState.transformsUniforms))
		if len(rState.visibleInstanceIndices) == 0 {
			// keep at least 1 element to avoid zero-sized buffer
			rState.visibleInstanceIndices = []uint32{0}
		}
		rState.visibleInstanceIndicesBuffer = createBuffer("visibleInstanceIndices", rState.visibleInstanceIndices, gpuState, wgpu.BufferUsageStorage|wgpu.BufferUsageCopyDst)
		rState.visibleListParametersBuffer = createBuffer("visibleListParams", rState.visibleListParametersUniform, gpuState, wgpu.BufferUsageUniform|wgpu.BufferUsageCopyDst)
	}
}

// TODO run only once?
func createBindGroup(gpuState *GpuState, rState *voxelRenderState) {
	// Create compute bind group (group 0) once buffers and compute pipeline are available
	if rState.voxelComputeBindGroup == nil && rState.voxelPoolBuffer != nil && rState.computePipeline != nil && rState.outputTextureView != nil {
		fmt.Println("Creating compute bind group...")
		compLayout := rState.computePipeline.GetBindGroupLayout(0)
		defer compLayout.Release()
		compGroup, err := gpuState.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: compLayout,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 0, Buffer: rState.renderParametersBuffer, Size: wgpu.WholeSize},
				{Binding: 1, Buffer: rState.cameraBuffer, Size: wgpu.WholeSize},
				{Binding: 2, Buffer: rState.transformsBuffer, Size: wgpu.WholeSize},
				{Binding: 3, Buffer: rState.voxelInstancesBuffer, Size: wgpu.WholeSize},
				{Binding: 4, Buffer: rState.macroIndexPoolBuffer, Size: wgpu.WholeSize},
				{Binding: 5, Buffer: rState.brickPoolBuffer, Size: wgpu.WholeSize},
				{Binding: 6, Buffer: rState.voxelPoolBuffer, Size: wgpu.WholeSize},
				{Binding: 7, Buffer: rState.palettesBuffer, Size: wgpu.WholeSize},
				{Binding: 8, TextureView: rState.outputTextureView},
				{Binding: 10, Buffer: rState.instanceAABBsBuffer, Size: wgpu.WholeSize},
				{Binding: 11, Buffer: rState.visibleInstanceIndicesBuffer, Size: wgpu.WholeSize},
				{Binding: 12, Buffer: rState.visibleListParametersBuffer, Size: wgpu.WholeSize},
			},
		})
		if err != nil {
			panic(err)
		}
		rState.voxelComputeBindGroup = compGroup
		fmt.Println("Compute bind group created.")
	}

	// Create render bind group for group 0 (blit: textureLoad from compute output @binding(9))
	if rState.blitBindGroup == nil && rState.blitPipeline != nil && rState.outputTextureView != nil {
		fmt.Println("Creating blit bind group (render group 0)...")
		renderLayout0 := rState.blitPipeline.GetBindGroupLayout(0)
		defer renderLayout0.Release()
		blitGroup, err := gpuState.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
			Layout: renderLayout0,
			Entries: []wgpu.BindGroupEntry{
				{Binding: 9, TextureView: rState.outputTextureView},
			},
		})
		if err != nil {
			panic(err)
		}
		rState.blitBindGroup = blitGroup
		fmt.Println("Blit bind group created.")
	}
}

// renders single frame
