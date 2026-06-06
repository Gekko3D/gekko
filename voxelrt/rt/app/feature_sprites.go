package app

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

// SpriteFeature owns sprite pipeline lifecycle and accumulation rendering.
type SpriteFeature struct{}

type SpriteResources struct {
	Pipeline *wgpu.RenderPipeline
}

type SpriteInstanceInput struct {
	Pos  [3]float32
	IsUI uint32

	Size      [2]float32
	IsUnlit   uint32
	AlphaMode uint32

	Color [4]float32

	SpriteIndex   uint32
	AtlasCols     uint32
	AtlasRows     uint32
	BillboardMode uint32
}

type SpriteBatchInput struct {
	AtlasKey      string
	FirstInstance uint32
	InstanceCount uint32
}

func (f *SpriteFeature) Name() string {
	return "sprites"
}

func (f *SpriteFeature) GraphNodeNames() []string {
	return []string{RenderNodeCoreAccumulation}
}

func (f *SpriteFeature) GraphPassStages() []FeaturePassStage {
	return []FeaturePassStage{FeaturePassStageAccumulation}
}

func (f *SpriteFeature) Enabled(*App) bool {
	return true
}

func (f *SpriteFeature) Setup(a *App) error {
	if a == nil {
		return nil
	}
	a.setupSpritesPipeline()
	return nil
}

func (f *SpriteFeature) Resize(a *App, _, _ uint32) error {
	if a == nil {
		return nil
	}
	a.setupSpritesPipeline()
	return nil
}

func (f *SpriteFeature) OnSceneBuffersRecreated(a *App) error {
	if a == nil || a.BufferManager == nil {
		return nil
	}
	a.BufferManager.RebuildSpriteBindGroups(a.spritePipeline())
	return nil
}

func (f *SpriteFeature) Update(*App) error {
	return nil
}

func (f *SpriteFeature) Render(*App, *wgpu.CommandEncoder, *wgpu.TextureView) error {
	return nil
}

func (f *SpriteFeature) Shutdown(a *App) {
	if a == nil {
		return
	}
	a.SpriteResources = nil
}

func (f *SpriteFeature) HasPassStage(a *App, stage FeaturePassStage) bool {
	pipeline := a.spritePipeline()
	return stage == FeaturePassStageAccumulation &&
		a != nil &&
		a.BufferManager != nil &&
		pipeline != nil &&
		a.BufferManager.HasSpriteContribution()
}

func (f *SpriteFeature) RenderPassStage(a *App, stage FeaturePassStage, pass *wgpu.RenderPassEncoder) error {
	if stage != FeaturePassStageAccumulation {
		return nil
	}
	if a == nil || pass == nil || a.BufferManager == nil {
		return nil
	}
	pipeline := a.spritePipeline()
	if pipeline == nil || !a.BufferManager.HasSpriteContribution() || a.BufferManager.SpritesBindGroup1 == nil {
		return nil
	}

	pass.SetPipeline(pipeline)
	pass.SetBindGroup(1, a.BufferManager.SpritesBindGroup1, nil)
	for _, batch := range a.BufferManager.SpriteBatches {
		if batch.BindGroup0 == nil || batch.InstanceCount == 0 {
			continue
		}
		pass.SetBindGroup(0, batch.BindGroup0, nil)
		pass.Draw(6, batch.InstanceCount, 0, batch.FirstInstance)
	}
	return nil
}

func (a *App) SpritePipeline() *wgpu.RenderPipeline {
	return a.spritePipeline()
}

func (a *App) spritePipeline() *wgpu.RenderPipeline {
	if a == nil || a.SpriteResources == nil {
		return nil
	}
	return a.SpriteResources.Pipeline
}

func (a *App) ApplySpriteInput(instances []SpriteInstanceInput, batches []SpriteBatchInput) {
	if a == nil || a.BufferManager == nil {
		return
	}
	spriteBytes, spriteCount := spriteInstanceBytes(instances)
	a.BufferManager.UpdateSprites(spriteBytes, spriteCount)
	a.BufferManager.SyncSpriteBatches(a.spritePipeline(), spriteBatchDescs(batches))
}

func (a *App) ClearSpriteInput() {
	if a == nil || a.BufferManager == nil {
		return
	}
	a.BufferManager.SpriteCount = 0
	a.BufferManager.SpriteBatches = a.BufferManager.SpriteBatches[:0]
}

func spriteInstanceBytes(instances []SpriteInstanceInput) ([]byte, uint32) {
	count := uint32(len(instances))
	if count == 0 {
		return nil, 0
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&instances[0])), len(instances)*int(unsafe.Sizeof(SpriteInstanceInput{}))), count
}

func spriteBatchDescs(batches []SpriteBatchInput) []gpu.SpriteBatchDesc {
	if len(batches) == 0 {
		return nil
	}
	descs := make([]gpu.SpriteBatchDesc, 0, len(batches))
	for _, batch := range batches {
		descs = append(descs, gpu.SpriteBatchDesc{
			AtlasKey:      batch.AtlasKey,
			FirstInstance: batch.FirstInstance,
			InstanceCount: batch.InstanceCount,
		})
	}
	return descs
}
