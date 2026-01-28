package gpu

import (
	"math"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
	"github.com/go-gl/mathgl/mgl32"
)

// GizmoVertex matches the WGSL VertexInput
type GizmoVertex struct {
	Pos [3]float32
}

// GizmoInstance matches the WGSL instance attributes
type GizmoInstance struct {
	ModelMat mgl32.Mat4
	Color    [4]float32
}

type GizmoRenderPass struct {
	Pipeline       *wgpu.RenderPipeline
	BindGroup      *wgpu.BindGroup
	DepthBindGroup *wgpu.BindGroup
	VertexBuffer   *wgpu.Buffer
	VertexCount    uint32
	ShapeOffsets   map[core.GizmoType]uint32
	ShapeCounts    map[core.GizmoType]uint32
	InstanceBuffer *wgpu.Buffer
	InstanceCap    uint32
	GizmosByShape  map[core.GizmoType][]GizmoInstance
	Device         *wgpu.Device
}

func NewGizmoRenderPass(device *wgpu.Device, format wgpu.TextureFormat) (*GizmoRenderPass, error) {
	shaderModule, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label:          "GizmoShader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaders.GizmoWGSL},
	})
	if err != nil {
		return nil, err
	}

	// Create Bind Group Layout for Camera (Group 0)
	bgl, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "GizmoCameraBGL",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex,
				Buffer: wgpu.BufferBindingLayout{
					Type:             wgpu.BufferBindingTypeUniform,
					MinBindingSize:   256, // Size of CameraData (Struct size in shader)
					HasDynamicOffset: false,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	// Create Bind Group Layout for Depth Texture (Group 1)
	depthBgl, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "GizmoDepthBGL",
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageFragment,
				Texture: wgpu.TextureBindingLayout{
					SampleType:    wgpu.TextureSampleTypeUnfilterableFloat,
					ViewDimension: wgpu.TextureViewDimension2D,
					Multisampled:  false,
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}

	pipelineLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			bgl,
			depthBgl,
		},
	})
	if err != nil {
		return nil, err
	}

	pipeline, err := device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Label:  "GizmoPipeline",
		Layout: pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     shaderModule,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{
				{
					ArrayStride: uint64(unsafe.Sizeof(GizmoVertex{})),
					StepMode:    wgpu.VertexStepModeVertex,
					Attributes: []wgpu.VertexAttribute{
						{
							Format:         wgpu.VertexFormatFloat32x3,
							Offset:         0,
							ShaderLocation: 0,
						},
					},
				},
				{
					ArrayStride: uint64(unsafe.Sizeof(GizmoInstance{})),
					StepMode:    wgpu.VertexStepModeInstance,
					Attributes: []wgpu.VertexAttribute{
						{
							Format:         wgpu.VertexFormatFloat32x4,
							Offset:         0,
							ShaderLocation: 2,
						},
						{
							Format:         wgpu.VertexFormatFloat32x4,
							Offset:         16,
							ShaderLocation: 3,
						},
						{
							Format:         wgpu.VertexFormatFloat32x4,
							Offset:         32,
							ShaderLocation: 4,
						},
						{
							Format:         wgpu.VertexFormatFloat32x4,
							Offset:         48,
							ShaderLocation: 5,
						},
						{
							Format:         wgpu.VertexFormatFloat32x4,
							Offset:         64,
							ShaderLocation: 6,
						},
					},
				},
			},
		},
		Fragment: &wgpu.FragmentState{
			Module:     shaderModule,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    format,
					WriteMask: wgpu.ColorWriteMaskAll,
					Blend: &wgpu.BlendState{
						Color: wgpu.BlendComponent{
							Operation: wgpu.BlendOperationAdd,
							SrcFactor: wgpu.BlendFactorSrcAlpha,
							DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						},
						Alpha: wgpu.BlendComponent{
							Operation: wgpu.BlendOperationAdd,
							SrcFactor: wgpu.BlendFactorOne,
							DstFactor: wgpu.BlendFactorOneMinusSrcAlpha,
						},
					},
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:  wgpu.PrimitiveTopologyLineList,
			FrontFace: wgpu.FrontFaceCCW,
			CullMode:  wgpu.CullModeNone,
		},
		DepthStencil: nil, // No Depth Testing for now (avoids attachment requirement)
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		return nil, err
	}

	p := &GizmoRenderPass{
		Pipeline:      pipeline,
		Device:        device,
		ShapeOffsets:  make(map[core.GizmoType]uint32),
		ShapeCounts:   make(map[core.GizmoType]uint32),
		GizmosByShape: make(map[core.GizmoType][]GizmoInstance),
	}

	// 1. Generate Unit Shapes
	var vertices []GizmoVertex
	addShape := func(t core.GizmoType, shapeVertices []GizmoVertex) {
		p.ShapeOffsets[t] = uint32(len(vertices))
		p.ShapeCounts[t] = uint32(len(shapeVertices))
		vertices = append(vertices, shapeVertices...)
	}

	// Unit Line (0,0,0) to (0,0,1) - we use Z as the forward axis for line
	// Note: We'll transform this by a matrix that points it from P1 to P2
	addShape(core.GizmoLine, []GizmoVertex{
		{Pos: [3]float32{0, 0, 0}},
		{Pos: [3]float32{0, 0, 1}},
	})

	// Unit Cube -0.5 to 0.5
	cubeVerts := []GizmoVertex{}
	min, max := float32(-0.5), float32(0.5)
	// Bottom
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{min, min, min}}, GizmoVertex{Pos: [3]float32{max, min, min}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{max, min, min}}, GizmoVertex{Pos: [3]float32{max, min, max}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{max, min, max}}, GizmoVertex{Pos: [3]float32{min, min, max}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{min, min, max}}, GizmoVertex{Pos: [3]float32{min, min, min}})
	// Top
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{min, max, min}}, GizmoVertex{Pos: [3]float32{max, max, min}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{max, max, min}}, GizmoVertex{Pos: [3]float32{max, max, max}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{max, max, max}}, GizmoVertex{Pos: [3]float32{min, max, max}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{min, max, max}}, GizmoVertex{Pos: [3]float32{min, max, min}})
	// Sides
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{min, min, min}}, GizmoVertex{Pos: [3]float32{min, max, min}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{max, min, min}}, GizmoVertex{Pos: [3]float32{max, max, min}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{max, min, max}}, GizmoVertex{Pos: [3]float32{max, max, max}})
	cubeVerts = append(cubeVerts, GizmoVertex{Pos: [3]float32{min, min, max}}, GizmoVertex{Pos: [3]float32{min, max, max}})
	addShape(core.GizmoCube, cubeVerts)

	// Unit Sphere (3 rings)
	sphereVerts := []GizmoVertex{}
	steps := 32
	angleStep := float32(2.0 * math.Pi / float64(steps))
	for i := 0; i < steps; i++ {
		a1, a2 := float32(i)*angleStep, float32(i+1)*angleStep
		c1, s1 := float32(math.Cos(float64(a1))), float32(math.Sin(float64(a1)))
		c2, s2 := float32(math.Cos(float64(a2))), float32(math.Sin(float64(a2)))
		sphereVerts = append(sphereVerts, GizmoVertex{Pos: [3]float32{c1, s1, 0}}, GizmoVertex{Pos: [3]float32{c2, s2, 0}})
		sphereVerts = append(sphereVerts, GizmoVertex{Pos: [3]float32{c1, 0, s1}}, GizmoVertex{Pos: [3]float32{c2, 0, s2}})
		sphereVerts = append(sphereVerts, GizmoVertex{Pos: [3]float32{0, c1, s1}}, GizmoVertex{Pos: [3]float32{0, c2, s2}})
	}
	addShape(core.GizmoSphere, sphereVerts)

	// Unit Rect (XY Plane)
	rectVerts := []GizmoVertex{
		{Pos: [3]float32{min, min, 0}}, {Pos: [3]float32{max, min, 0}},
		{Pos: [3]float32{max, min, 0}}, {Pos: [3]float32{max, max, 0}},
		{Pos: [3]float32{max, max, 0}}, {Pos: [3]float32{min, max, 0}},
		{Pos: [3]float32{min, max, 0}}, {Pos: [3]float32{min, min, 0}},
	}
	addShape(core.GizmoRect, rectVerts)

	// Unit Circle (XY Plane)
	circleVerts := []GizmoVertex{}
	for i := 0; i < steps; i++ {
		a1, a2 := float32(i)*angleStep, float32(i+1)*angleStep
		circleVerts = append(circleVerts,
			GizmoVertex{Pos: [3]float32{float32(math.Cos(float64(a1))), float32(math.Sin(float64(a1))), 0}},
			GizmoVertex{Pos: [3]float32{float32(math.Cos(float64(a2))), float32(math.Sin(float64(a2))), 0}})
	}
	addShape(core.GizmoCircle, circleVerts)

	// Upload Static Vertex Buffer
	p.VertexCount = uint32(len(vertices))
	vSize := uint64(len(vertices) * int(unsafe.Sizeof(GizmoVertex{})))
	p.VertexBuffer, _ = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "GizmoUnitVertexBuffer",
		Size:  vSize,
		Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
	})
	device.GetQueue().WriteBuffer(p.VertexBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), vSize))

	return p, nil
}

func (p *GizmoRenderPass) Update(queue *wgpu.Queue, gizmos []core.Gizmo) {
	// Clear previous frame data
	for k := range p.GizmosByShape {
		p.GizmosByShape[k] = p.GizmosByShape[k][:0]
	}

	for _, g := range gizmos {
		inst := GizmoInstance{
			Color: g.Color,
		}

		if g.Type == core.GizmoLine {
			// Transform P1, P2 by ModelMatrix to get world points
			wp1 := g.ModelMatrix.Mul4x1(g.P1.Vec4(1.0)).Vec3()
			wp2 := g.ModelMatrix.Mul4x1(g.P2.Vec4(1.0)).Vec3()

			diff := wp2.Sub(wp1)
			dist := diff.Len()
			if dist < 0.0001 {
				continue
			}

			dir := diff.Normalize()
			// Rotation that maps Z+ (unit line axis) to dir
			rot := mgl32.QuatBetweenVectors(mgl32.Vec3{0, 0, 1}, dir)

			inst.ModelMat = mgl32.Translate3D(wp1.X(), wp1.Y(), wp1.Z()).
				Mul4(rot.Mat4()).
				Mul4(mgl32.Scale3D(1, 1, dist))
		} else {
			inst.ModelMat = g.ModelMatrix
		}

		p.GizmosByShape[g.Type] = append(p.GizmosByShape[g.Type], inst)
	}

	// Flatten all instances into one buffer
	var allInstances []GizmoInstance
	for _, shapeType := range []core.GizmoType{core.GizmoLine, core.GizmoCube, core.GizmoSphere, core.GizmoRect, core.GizmoCircle} {
		allInstances = append(allInstances, p.GizmosByShape[shapeType]...)
	}

	if len(allInstances) == 0 {
		return
	}

	instanceCount := uint32(len(allInstances))
	sizeBytes := uint64(len(allInstances) * int(unsafe.Sizeof(GizmoInstance{})))

	if p.InstanceBuffer == nil || p.InstanceCap < instanceCount {
		if p.InstanceBuffer != nil {
			p.InstanceBuffer.Release()
		}
		p.InstanceCap = instanceCount + 128 // Margin
		p.InstanceBuffer, _ = p.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "GizmoInstanceBuffer",
			Size:  uint64(p.InstanceCap) * uint64(unsafe.Sizeof(GizmoInstance{})),
			Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
	}

	queue.WriteBuffer(p.InstanceBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&allInstances[0])), sizeBytes))
}

func (p *GizmoRenderPass) Draw(pass *wgpu.RenderPassEncoder, cameraBindGroup *wgpu.BindGroup, depthBindGroup *wgpu.BindGroup) {
	if p.InstanceBuffer == nil {
		return
	}

	pass.SetPipeline(p.Pipeline)
	pass.SetBindGroup(0, cameraBindGroup, nil)
	if depthBindGroup != nil {
		pass.SetBindGroup(1, depthBindGroup, nil)
	}

	pass.SetVertexBuffer(0, p.VertexBuffer, 0, p.VertexBuffer.GetSize())
	pass.SetVertexBuffer(1, p.InstanceBuffer, 0, p.InstanceBuffer.GetSize())

	var instanceOffset uint32
	shapes := []core.GizmoType{core.GizmoLine, core.GizmoCube, core.GizmoSphere, core.GizmoRect, core.GizmoCircle}
	for _, shapeType := range shapes {
		instances := p.GizmosByShape[shapeType]
		count := uint32(len(instances))
		if count > 0 {
			offset := p.ShapeOffsets[shapeType]
			verts := p.ShapeCounts[shapeType]
			pass.Draw(verts, count, offset, instanceOffset)
		}
		instanceOffset += count
	}
}

// Add a helper to create the bind group
func (p *GizmoRenderPass) CreateBindGroup(cameraBuffer *wgpu.Buffer) (*wgpu.BindGroup, error) {
	return p.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "GizmoCameraBG",
		Layout: p.Pipeline.GetBindGroupLayout(0),
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0,
				Buffer:  cameraBuffer,
				Size:    256, // Must match MinBindingSize
			},
		},
	})
}

// Add a helper to create the depth bind group
func (p *GizmoRenderPass) CreateDepthBindGroup(depthView *wgpu.TextureView) (*wgpu.BindGroup, error) {
	return p.Device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "GizmoDepthBG",
		Layout: p.Pipeline.GetBindGroupLayout(1),
		Entries: []wgpu.BindGroupEntry{
			{
				Binding:     0,
				TextureView: depthView,
			},
		},
	})
}
