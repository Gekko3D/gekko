package gpu

type CAVolumeRenderDefaults struct {
	ScatterColor    [3]float32
	ShadowTint      [3]float32
	AbsorptionColor [3]float32
	Extinction      float32
	Emission        float32
}

type CAVolumePresetDefinition struct {
	Sim         CAPresetData
	SmokeRender CAVolumeRenderDefaults
	FireRender  CAVolumeRenderDefaults
}

var caPresetDefinitions = [8]CAVolumePresetDefinition{
	{
		Sim: CAPresetData{
			SmokeSeed:       0.02,
			FireSeed:        0.08,
			SmokeInject:     0.14,
			FireInject:      0.45,
			Diffusion:       0.12,
			Buoyancy:        0.85,
			Cooling:         0.08,
			Dissipation:     0.02,
			SmokeDensityCut: 0.14,
			FireHeatCut:     0.04,
			SigmaTSmoke:     1.0,
			SigmaTFire:      0.32,
			AlphaScaleSmoke: 1.35,
			AlphaScaleFire:  1.35,
			AbsorptionScale: 1.0,
			ScatterScale:    1.0,
			EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
			FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
		},
		SmokeRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.72, 0.72, 0.72},
			ShadowTint:      [3]float32{0.45, 0.45, 0.46},
			AbsorptionColor: [3]float32{0.28, 0.29, 0.31},
			Extinction:      1.35,
			Emission:        0.0,
		},
		FireRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{1.0, 0.48, 0.1},
			ShadowTint:      [3]float32{0.62, 0.18, 0.04},
			AbsorptionColor: [3]float32{0.54, 0.12, 0.03},
			Extinction:      0.5,
			Emission:        5.5,
		},
	},
	{
		Sim: CAPresetData{
			SmokeSeed:       0.014,
			FireSeed:        0.12,
			SmokeInject:     0.08,
			FireInject:      0.55,
			Diffusion:       0.06,
			Buoyancy:        1.15,
			Cooling:         0.14,
			Dissipation:     0.04,
			SmokeDensityCut: 0.035,
			FireHeatCut:     0.04,
			SigmaTSmoke:     1.0,
			SigmaTFire:      0.32,
			AlphaScaleSmoke: 1.35,
			AlphaScaleFire:  1.35,
			AbsorptionScale: 1.0,
			ScatterScale:    1.0,
			EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
			FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
		},
		SmokeRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.78, 0.38, 0.12},
			ShadowTint:      [3]float32{0.42, 0.12, 0.04},
			AbsorptionColor: [3]float32{0.34, 0.08, 0.02},
			Extinction:      0.3,
			Emission:        10.5,
		},
		FireRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.78, 0.38, 0.12},
			ShadowTint:      [3]float32{0.42, 0.12, 0.04},
			AbsorptionColor: [3]float32{0.34, 0.08, 0.02},
			Extinction:      0.3,
			Emission:        10.5,
		},
	},
	{
		Sim: CAPresetData{
			SmokeSeed:       0.022,
			FireSeed:        0.06,
			SmokeInject:     0.12,
			FireInject:      0.38,
			Diffusion:       0.14,
			Buoyancy:        0.72,
			Cooling:         0.06,
			Dissipation:     0.015,
			SmokeDensityCut: 0.02,
			FireHeatCut:     0.04,
			SigmaTSmoke:     1.0,
			SigmaTFire:      0.32,
			AlphaScaleSmoke: 1.35,
			AlphaScaleFire:  1.35,
			AbsorptionScale: 1.0,
			ScatterScale:    1.0,
			EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
			FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
		},
		SmokeRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.34, 0.35, 0.38},
			ShadowTint:      [3]float32{0.2, 0.18, 0.16},
			AbsorptionColor: [3]float32{0.14, 0.11, 0.09},
			Extinction:      0.72,
			Emission:        0.0,
		},
		FireRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{1.0, 0.42, 0.1},
			ShadowTint:      [3]float32{0.54, 0.14, 0.04},
			AbsorptionColor: [3]float32{0.42, 0.1, 0.03},
			Extinction:      0.42,
			Emission:        7.2,
		},
	},
	{
		Sim: CAPresetData{
			SmokeSeed:       0.002,
			FireSeed:        0.18,
			SmokeInject:     0.04,
			FireInject:      1.15,
			Diffusion:       0.02,
			Buoyancy:        2.4,
			Cooling:         0.22,
			Dissipation:     0.08,
			SmokeDensityCut: 0.025,
			FireHeatCut:     0.015,
			SigmaTSmoke:     1.0,
			SigmaTFire:      1.02,
			AlphaScaleSmoke: 1.35,
			AlphaScaleFire:  1.65,
			AbsorptionScale: 0.38,
			ScatterScale:    0.1,
			EmberTint:       [4]float32{0.24, 0.34, 0.7, 1.0},
			FireCoreTint:    [4]float32{1.0, 1.0, 1.0, 1.0},
		},
		SmokeRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.16, 0.22, 0.34},
			ShadowTint:      [3]float32{0.08, 0.12, 0.2},
			AbsorptionColor: [3]float32{0.05, 0.08, 0.16},
			Extinction:      0.12,
			Emission:        10.8,
		},
		FireRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.16, 0.22, 0.34},
			ShadowTint:      [3]float32{0.08, 0.12, 0.2},
			AbsorptionColor: [3]float32{0.05, 0.08, 0.16},
			Extinction:      0.12,
			Emission:        10.8,
		},
	},
	{
		Sim: CAPresetData{
			SmokeSeed:       0.015,
			FireSeed:        0.16,
			SmokeInject:     0.28,
			FireInject:      0.65,
			Diffusion:       0.14,
			Buoyancy:        1.45,
			Cooling:         0.12,
			Dissipation:     0.028,
			SmokeDensityCut: 0.012,
			FireHeatCut:     0.05,
			SigmaTSmoke:     1.1,
			SigmaTFire:      0.28,
			AlphaScaleSmoke: 1.0,
			AlphaScaleFire:  1.0,
			AbsorptionScale: 1.0,
			ScatterScale:    1.0,
			EmberTint:       [4]float32{0.62, 0.16, 0.04, 1.0},
			FireCoreTint:    [4]float32{1.0, 0.72, 0.38, 1.0},
		},
		SmokeRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.58, 0.52, 0.46},
			ShadowTint:      [3]float32{0.24, 0.18, 0.14},
			AbsorptionColor: [3]float32{0.1, 0.08, 0.06},
			Extinction:      0.82,
			Emission:        24.0,
		},
		FireRender: CAVolumeRenderDefaults{
			ScatterColor:    [3]float32{0.58, 0.52, 0.46},
			ShadowTint:      [3]float32{0.24, 0.18, 0.14},
			AbsorptionColor: [3]float32{0.1, 0.08, 0.06},
			Extinction:      0.82,
			Emission:        24.0,
		},
	},
}

func CAVolumePresetDefinitionFor(preset uint32) CAVolumePresetDefinition {
	if preset >= uint32(len(caPresetDefinitions)) {
		return caPresetDefinitions[0]
	}
	return caPresetDefinitions[preset]
}

func CAVolumeRenderDefaultsFor(preset uint32, volumeType uint32) CAVolumeRenderDefaults {
	def := CAVolumePresetDefinitionFor(preset)
	if volumeType == 1 {
		return def.FireRender
	}
	return def.SmokeRender
}
