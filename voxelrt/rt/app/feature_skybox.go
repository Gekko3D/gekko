package app

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

// SkyboxFeature owns skybox generation pipeline bootstrap.
type SkyboxFeature struct{}

type SkyboxLayerInput struct {
	ColorA      [3]float32
	ColorB      [3]float32
	Offset      [3]float32
	Threshold   float32
	Opacity     float32
	Scale       float32
	Persistence float32
	Lacunarity  float32
	Seed        int32
	Octaves     int32
	BlendMode   uint32
	Invert      bool
	LayerType   uint32
}

type SkyboxInput struct {
	Width      uint32
	Height     uint32
	Layers     []SkyboxLayerInput
	SunDir     [4]float32
	SunColor   [4]float32
	SunParams  [4]float32
	DiskColor  [4]float32
	DiskParams [4]float32
	Smooth     bool
}

type SkyboxResources struct {
	PendingInput SkyboxInput
	InputDirty   bool
}

func (f *SkyboxFeature) Name() string {
	return "skybox"
}

func (f *SkyboxFeature) Enabled(*App) bool {
	return true
}

func (f *SkyboxFeature) GraphNodeNames() []string {
	return []string{RenderNodeFeatureSkyboxUpdate}
}

func (f *SkyboxFeature) Setup(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.CreateSkyboxGenPipeline(shaders.SkyboxWGSL)
	return nil
}

func (f *SkyboxFeature) Resize(*App, uint32, uint32) error {
	return nil
}

func (f *SkyboxFeature) OnSceneBuffersRecreated(*App) error {
	return nil
}

func (f *SkyboxFeature) Update(*App) error {
	return nil
}

func (f *SkyboxFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *SkyboxFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.SkyboxResources = nil
	if a.BufferManager != nil {
		a.BufferManager.SkyboxGenPipeline = nil
	}
}

func (a *App) SetSkyboxInput(input SkyboxInput) {
	if a == nil {
		return
	}
	resources := a.ensureSkyboxResources()
	resources.PendingInput = cloneSkyboxInput(input)
	resources.InputDirty = true
}

func (a *App) ClearSkyboxInput() {
	if a == nil || a.SkyboxResources == nil {
		return
	}
	a.SkyboxResources.PendingInput = SkyboxInput{}
	a.SkyboxResources.InputDirty = false
}

func (a *App) ApplySkyboxInput() {
	if a == nil || a.SkyboxResources == nil || !a.SkyboxResources.InputDirty {
		return
	}
	if a.BufferManager == nil {
		return
	}

	input := a.SkyboxResources.PendingInput
	a.SkyboxResources.InputDirty = false
	if input.Width == 0 || input.Height == 0 || len(input.Layers) == 0 {
		return
	}

	gpuLayers := skyboxGPULayers(input.Layers)
	a.BufferManager.UpdateSkyboxGPU(
		input.Width,
		input.Height,
		gpuLayers,
		input.SunDir,
		input.SunColor,
		input.SunParams,
		input.DiskColor,
		input.DiskParams,
		input.Smooth,
		a.LightingPipeline,
		a.StorageView,
	)
}

func (a *App) skyboxResources() *SkyboxResources {
	if a == nil {
		return nil
	}
	return a.SkyboxResources
}

func (a *App) ensureSkyboxResources() *SkyboxResources {
	if a.SkyboxResources == nil {
		a.SkyboxResources = &SkyboxResources{}
	}
	return a.SkyboxResources
}

func cloneSkyboxInput(input SkyboxInput) SkyboxInput {
	cloned := input
	if len(input.Layers) > 0 {
		cloned.Layers = append([]SkyboxLayerInput(nil), input.Layers...)
	}
	return cloned
}

func skyboxGPULayers(layers []SkyboxLayerInput) []gpu.GpuSkyboxLayer {
	if len(layers) == 0 {
		return nil
	}
	gpuLayers := make([]gpu.GpuSkyboxLayer, 0, len(layers))
	for _, layer := range layers {
		invert := uint32(0)
		if layer.Invert {
			invert = 1
		}
		gpuLayers = append(gpuLayers, gpu.GpuSkyboxLayer{
			ColorA:      [4]float32{layer.ColorA[0], layer.ColorA[1], layer.ColorA[2], layer.Threshold},
			ColorB:      [4]float32{layer.ColorB[0], layer.ColorB[1], layer.ColorB[2], layer.Opacity},
			Offset:      [4]float32{layer.Offset[0], layer.Offset[1], layer.Offset[2], layer.Scale},
			Persistence: layer.Persistence,
			Lacunarity:  layer.Lacunarity,
			Seed:        layer.Seed,
			Octaves:     layer.Octaves,
			BlendMode:   layer.BlendMode,
			Invert:      invert,
			LayerType:   layer.LayerType,
		})
	}
	return gpuLayers
}
