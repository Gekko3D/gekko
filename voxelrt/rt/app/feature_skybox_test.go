package app

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
)

func TestSkyboxInputHandoffCopiesLayerData(t *testing.T) {
	layers := []SkyboxLayerInput{
		{ColorA: [3]float32{1, 2, 3}, Threshold: 4},
	}
	app := &App{}

	app.SetSkyboxInput(SkyboxInput{
		Width:  64,
		Height: 32,
		Layers: layers,
		Smooth: true,
	})
	layers[0].ColorA[0] = 99

	resources := app.skyboxResources()
	if resources == nil || !resources.InputDirty {
		t.Fatal("expected pending dirty skybox input")
	}
	if got := resources.PendingInput.Layers[0].ColorA[0]; got != 1 {
		t.Fatalf("expected skybox input layers to be copied, got first color component %f", got)
	}
}

func TestSkyboxLayerInputPacksGpuLayer(t *testing.T) {
	layers := skyboxGPULayers([]SkyboxLayerInput{
		{
			ColorA:      [3]float32{1, 2, 3},
			ColorB:      [3]float32{4, 5, 6},
			Offset:      [3]float32{7, 8, 9},
			Threshold:   0.25,
			Opacity:     0.5,
			Scale:       2,
			Persistence: 0.6,
			Lacunarity:  2.1,
			Seed:        13,
			Octaves:     4,
			BlendMode:   2,
			Invert:      true,
			LayerType:   3,
		},
	})

	if len(layers) != 1 {
		t.Fatalf("expected one GPU skybox layer, got %d", len(layers))
	}
	layer := layers[0]
	if layer.ColorA != [4]float32{1, 2, 3, 0.25} {
		t.Fatalf("ColorA = %+v", layer.ColorA)
	}
	if layer.ColorB != [4]float32{4, 5, 6, 0.5} {
		t.Fatalf("ColorB = %+v", layer.ColorB)
	}
	if layer.Offset != [4]float32{7, 8, 9, 2} {
		t.Fatalf("Offset = %+v", layer.Offset)
	}
	if layer.Persistence != 0.6 || layer.Lacunarity != 2.1 || layer.Seed != 13 || layer.Octaves != 4 ||
		layer.BlendMode != 2 || layer.Invert != 1 || layer.LayerType != 3 {
		t.Fatalf("unexpected packed layer fields: %+v", layer)
	}
}

func TestSkyboxUpdateNodeAppliesPendingInput(t *testing.T) {
	app := &App{
		features:      []Feature{&SkyboxFeature{}},
		BufferManager: &gpu.GpuBufferManager{},
	}
	app.SetSkyboxInput(SkyboxInput{
		Width:  64,
		Height: 32,
		Layers: []SkyboxLayerInput{
			{ColorA: [3]float32{1, 1, 1}},
		},
		Smooth: true,
	})

	node := defaultRenderGraphNode(RenderNodeFeatureSkyboxUpdate)
	if err := node.Update(app); err != nil {
		t.Fatalf("skybox update node returned error: %v", err)
	}
	if app.skyboxResources().InputDirty {
		t.Fatal("expected skybox update node to consume pending input")
	}
}

func TestClearSkyboxInputClearsPendingState(t *testing.T) {
	app := &App{}
	app.SetSkyboxInput(SkyboxInput{
		Width:  64,
		Height: 32,
		Layers: []SkyboxLayerInput{
			{ColorA: [3]float32{1, 1, 1}},
		},
	})

	app.ClearSkyboxInput()

	resources := app.skyboxResources()
	if resources == nil {
		t.Fatal("expected skybox resources to remain allocated")
	}
	if resources.InputDirty {
		t.Fatal("expected pending skybox input to be clean after clear")
	}
	if len(resources.PendingInput.Layers) != 0 {
		t.Fatalf("expected cleared skybox layers, got %d", len(resources.PendingInput.Layers))
	}
}
