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
	Pos   [3]float32
	Color [4]float32
}

type GizmoRenderPass struct {
	Pipeline        *wgpu.RenderPipeline
	BindGroup       *wgpu.BindGroup
	VertexBuffer    *wgpu.Buffer
	VertexBufferCap uint64
	VertexCount     uint32
	Device          *wgpu.Device
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

	pipelineLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{
			bgl,
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
						{
							Format:         wgpu.VertexFormatFloat32x4,
							Offset:         12,
							ShaderLocation: 1,
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

	return &GizmoRenderPass{
		Pipeline: pipeline,
		Device:   device,
	}, nil
}

func (p *GizmoRenderPass) Update(queue *wgpu.Queue, gizmos []core.Gizmo) {
	var vertices []GizmoVertex

	// Tessellate Gizmos into Line List
	for _, g := range gizmos {
		// Color
		c := g.Color

		// 1. Line
		if g.Type == core.GizmoLine {
			// Transform P1, P2 by ModelMatrix
			p1 := g.ModelMatrix.Mul4x1(g.P1.Vec4(1.0)).Vec3()
			p2 := g.ModelMatrix.Mul4x1(g.P2.Vec4(1.0)).Vec3()

			vertices = append(vertices,
				GizmoVertex{Pos: [3]float32{p1.X(), p1.Y(), p1.Z()}, Color: c},
				GizmoVertex{Pos: [3]float32{p2.X(), p2.Y(), p2.Z()}, Color: c},
			)
			continue
		}

		// Shape Tessellation helpers
		add := func(p1, p2 mgl32.Vec3) {
			// Transform by ModelMatrix
			wp1 := g.ModelMatrix.Mul4x1(p1.Vec4(1.0)).Vec3()
			wp2 := g.ModelMatrix.Mul4x1(p2.Vec4(1.0)).Vec3()
			vertices = append(vertices,
				GizmoVertex{Pos: [3]float32{wp1.X(), wp1.Y(), wp1.Z()}, Color: c},
				GizmoVertex{Pos: [3]float32{wp2.X(), wp2.Y(), wp2.Z()}, Color: c},
			)
		}

		if g.Type == core.GizmoCube {
			// Unit Cube -0.5 to 0.5
			min := float32(-0.5)
			max := float32(0.5)
			// Bottom
			add(mgl32.Vec3{min, min, min}, mgl32.Vec3{max, min, min})
			add(mgl32.Vec3{max, min, min}, mgl32.Vec3{max, min, max})
			add(mgl32.Vec3{max, min, max}, mgl32.Vec3{min, min, max})
			add(mgl32.Vec3{min, min, max}, mgl32.Vec3{min, min, min})
			// Top
			add(mgl32.Vec3{min, max, min}, mgl32.Vec3{max, max, min})
			add(mgl32.Vec3{max, max, min}, mgl32.Vec3{max, max, max})
			add(mgl32.Vec3{max, max, max}, mgl32.Vec3{min, max, max})
			add(mgl32.Vec3{min, max, max}, mgl32.Vec3{min, max, min})
			// Sides
			add(mgl32.Vec3{min, min, min}, mgl32.Vec3{min, max, min})
			add(mgl32.Vec3{max, min, min}, mgl32.Vec3{max, max, min})
			add(mgl32.Vec3{max, min, max}, mgl32.Vec3{max, max, max})
			add(mgl32.Vec3{min, min, max}, mgl32.Vec3{min, max, max})
		} else if g.Type == core.GizmoSphere {
			// 3 Rings (XY, XZ, YZ)
			steps := 32
			angleStep := float32(2.0 * math.Pi / float64(steps))

			// XY Ring
			for i := 0; i < steps; i++ {
				a1 := float32(i) * angleStep
				a2 := float32(i+1) * angleStep
				add(mgl32.Vec3{float32(math.Cos(float64(a1))), float32(math.Sin(float64(a1))), 0},
					mgl32.Vec3{float32(math.Cos(float64(a2))), float32(math.Sin(float64(a2))), 0})
			}
			// XZ Ring
			for i := 0; i < steps; i++ {
				a1 := float32(i) * angleStep
				a2 := float32(i+1) * angleStep
				add(mgl32.Vec3{float32(math.Cos(float64(a1))), 0, float32(math.Sin(float64(a1)))},
					mgl32.Vec3{float32(math.Cos(float64(a2))), 0, float32(math.Sin(float64(a2)))})
			}
			// YZ Ring
			for i := 0; i < steps; i++ {
				a1 := float32(i) * angleStep
				a2 := float32(i+1) * angleStep
				add(mgl32.Vec3{0, float32(math.Cos(float64(a1))), float32(math.Sin(float64(a1)))},
					mgl32.Vec3{0, float32(math.Cos(float64(a2))), float32(math.Sin(float64(a2)))})
			}
		} else if g.Type == core.GizmoRect {
			// XY Plane Rectangle -0.5 to 0.5
			min := float32(-0.5)
			max := float32(0.5)
			add(mgl32.Vec3{min, min, 0}, mgl32.Vec3{max, min, 0})
			add(mgl32.Vec3{max, min, 0}, mgl32.Vec3{max, max, 0})
			add(mgl32.Vec3{max, max, 0}, mgl32.Vec3{min, max, 0})
			add(mgl32.Vec3{min, max, 0}, mgl32.Vec3{min, min, 0})
		} else if g.Type == core.GizmoCircle {
			// XY Plane Circle
			steps := 32
			angleStep := float32(2.0 * math.Pi / float64(steps))
			for i := 0; i < steps; i++ {
				a1 := float32(i) * angleStep
				a2 := float32(i+1) * angleStep
				add(mgl32.Vec3{float32(math.Cos(float64(a1))), float32(math.Sin(float64(a1))), 0},
					mgl32.Vec3{float32(math.Cos(float64(a2))), float32(math.Sin(float64(a2))), 0})
			}
		}
	}

	p.VertexCount = uint32(len(vertices))

	if p.VertexCount == 0 {
		return
	}

	// Upload
	sizeBytes := uint64(len(vertices) * int(unsafe.Sizeof(GizmoVertex{})))

	if p.VertexBuffer == nil || p.VertexBufferCap < sizeBytes {
		if p.VertexBuffer != nil {
			p.VertexBuffer.Release()
		}
		p.VertexBufferCap = sizeBytes * 2 // Growth factor
		p.VertexBuffer, _ = p.Device.CreateBuffer(&wgpu.BufferDescriptor{
			Label: "GizmoVertexBuffer",
			Size:  p.VertexBufferCap,
			Usage: wgpu.BufferUsageVertex | wgpu.BufferUsageCopyDst,
		})
	}

	queue.WriteBuffer(p.VertexBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&vertices[0])), sizeBytes))
}

func (p *GizmoRenderPass) Draw(pass *wgpu.RenderPassEncoder, cameraBindGroup *wgpu.BindGroup) {
	if p.VertexCount == 0 || p.VertexBuffer == nil {
		return
	}

	pass.SetPipeline(p.Pipeline)
	pass.SetBindGroup(0, cameraBindGroup, nil) // Reuse Camera BG (Has ViewProj)

	// Calculate size of vertex data to bind
	sizeBytes := uint64(p.VertexCount) * uint64(unsafe.Sizeof(GizmoVertex{}))
	pass.SetVertexBuffer(0, p.VertexBuffer, 0, sizeBytes)
	pass.Draw(p.VertexCount, 1, 0, 0)
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
