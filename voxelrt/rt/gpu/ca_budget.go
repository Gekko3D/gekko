package gpu

type CAVolumeBudgetConfig struct {
	MaxManagedVolumes     int
	MaxResolutionAxis     int
	MaxCellsPerVolume     int
	MaxAtlasCells         int
	MaxStepsPerVolume     int
	MaxTotalStepsPerFrame int
	StepReduceDistance    float32
	StepSuspendDistance   float32
	BehindCameraDot       float32
}

func DefaultCAVolumeBudgetConfig() CAVolumeBudgetConfig {
	return CAVolumeBudgetConfig{
		MaxManagedVolumes:     24,
		MaxResolutionAxis:     64,
		MaxCellsPerVolume:     160000,
		MaxAtlasCells:         4 * 1024 * 1024,
		MaxStepsPerVolume:     4,
		MaxTotalStepsPerFrame: 32,
		StepReduceDistance:    40,
		StepSuspendDistance:   90,
		BehindCameraDot:       -0.1,
	}
}

func (cfg CAVolumeBudgetConfig) WithDefaults() CAVolumeBudgetConfig {
	def := DefaultCAVolumeBudgetConfig()
	if cfg.MaxManagedVolumes <= 0 {
		cfg.MaxManagedVolumes = def.MaxManagedVolumes
	}
	if cfg.MaxResolutionAxis <= 0 {
		cfg.MaxResolutionAxis = def.MaxResolutionAxis
	}
	if cfg.MaxCellsPerVolume <= 0 {
		cfg.MaxCellsPerVolume = def.MaxCellsPerVolume
	}
	if cfg.MaxAtlasCells <= 0 {
		cfg.MaxAtlasCells = def.MaxAtlasCells
	}
	if cfg.MaxStepsPerVolume <= 0 {
		cfg.MaxStepsPerVolume = def.MaxStepsPerVolume
	}
	if cfg.MaxTotalStepsPerFrame <= 0 {
		cfg.MaxTotalStepsPerFrame = def.MaxTotalStepsPerFrame
	}
	if cfg.StepReduceDistance <= 0 {
		cfg.StepReduceDistance = def.StepReduceDistance
	}
	if cfg.StepSuspendDistance <= 0 {
		cfg.StepSuspendDistance = def.StepSuspendDistance
	}
	if cfg.StepSuspendDistance < cfg.StepReduceDistance {
		cfg.StepSuspendDistance = cfg.StepReduceDistance
	}
	if cfg.BehindCameraDot == 0 {
		cfg.BehindCameraDot = def.BehindCameraDot
	}
	if cfg.BehindCameraDot < -1 {
		cfg.BehindCameraDot = -1
	}
	if cfg.BehindCameraDot > 1 {
		cfg.BehindCameraDot = 1
	}
	return cfg
}
