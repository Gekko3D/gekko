package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

type WaterSplashEffectComponent struct {
	Disabled bool

	Texture      AssetId
	AtlasCols    uint32
	AtlasRows    uint32
	SplashSprite uint32
	SpraySprite  uint32
	FlashSprite  uint32

	MinImpactSpeed float32
	StrengthScale  float32

	DropletColorMin [4]float32
	DropletColorMax [4]float32
	SprayColorMin   [4]float32
	SprayColorMax   [4]float32
}

type WaterEffectsState struct {
	DefaultSplash WaterSplashEffectComponent
}

type WaterEffectsModule struct {
	DefaultSplash WaterSplashEffectComponent
}

func (mod WaterEffectsModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&WaterEffectsState{DefaultSplash: mod.DefaultSplash})
	app.UseSystem(
		System(waterSplashEffectsSystem).
			InStage(PostUpdate).
			RunAlways(),
	)
}

func waterSplashEffectsSystem(cmd *Commands, interactions *WaterInteractionState, effects *WaterEffectsState) {
	if cmd == nil || interactions == nil || effects == nil {
		return
	}

	configByWater := make(map[EntityId]WaterSplashEffectComponent)
	MakeQuery1[WaterSplashEffectComponent](cmd).Map(func(eid EntityId, splash *WaterSplashEffectComponent) bool {
		if splash != nil {
			configByWater[eid] = *splash
		}
		return true
	})

	for _, impact := range interactions.ImpactEvents() {
		cfg, ok := configByWater[impact.WaterEntity]
		if !ok {
			cfg = effects.DefaultSplash
		}
		spawnWaterSplashEffect(cmd, impact, cfg)
	}
}

func spawnWaterSplashEffect(cmd *Commands, impact WaterImpactEvent, cfg WaterSplashEffectComponent) {
	if cmd == nil || cfg.Disabled {
		return
	}

	minSpeed := cfg.normalizedMinImpactSpeed()
	if impact.Speed < minSpeed {
		return
	}

	strength := clampWaterFloat(impact.Strength*cfg.normalizedStrengthScale(), 0.4, 1.9)
	radius := clampWaterFloat(impact.Radius, 0.2, 3.0)
	radiusScale := clampWaterFloat(0.85+radius*0.45, 0.9, 2.2)
	skim := impact.Kind == WaterDisturbanceSkim
	emitterRotation := waterSplashRotationForImpact(impact)
	cols, rows := cfg.normalizedAtlasGrid()
	flashSize := (1.05 + 0.82*strength) * radiusScale
	crownCone := float32(20)
	crownGravity := float32(14.0)
	crownDrag := float32(1.4)
	sprayCone := float32(64)
	sprayGravity := float32(10.0)
	sprayDrag := float32(2.0)
	if skim {
		crownCone = 42
		crownGravity = 9.0
		crownDrag = 2.1
		sprayCone = 78
		sprayGravity = 7.5
		sprayDrag = 2.8
		flashSize *= 1.25
	}

	cmd.AddEntity(
		&TransformComponent{Position: impact.Position, Rotation: emitterRotation, Scale: mgl32.Vec3{1, 1, 1}},
		&ParticleEmitterComponent{
			Enabled:          true,
			MaxParticles:     4096,
			SpawnRate:        780.0 * strength * radiusScale,
			LifetimeRange:    [2]float32{0.45, 0.8},
			StartSpeedRange:  [2]float32{3.9 * strength, 6.8 * strength * radiusScale},
			StartSizeRange:   [2]float32{0.12 * radiusScale, 0.22 * radiusScale},
			StartColorMin:    cfg.normalizedDropletColorMin(),
			StartColorMax:    cfg.normalizedDropletColorMax(),
			Gravity:          crownGravity,
			Drag:             crownDrag,
			ConeAngleDegrees: crownCone,
			SpriteIndex:      cfg.normalizedSplashSprite(),
			AtlasCols:        cols,
			AtlasRows:        rows,
			Texture:          cfg.Texture,
			AlphaMode:        SpriteAlphaLuminance,
		},
		&LifetimeComponent{TimeLeft: 0.2},
	)

	cmd.AddEntity(
		&TransformComponent{Position: impact.Position, Rotation: emitterRotation, Scale: mgl32.Vec3{1, 1, 1}},
		&ParticleEmitterComponent{
			Enabled:          true,
			MaxParticles:     4096,
			SpawnRate:        640.0 * strength * radiusScale,
			LifetimeRange:    [2]float32{0.28, 0.55 + 0.08*radiusScale},
			StartSpeedRange:  [2]float32{3.2 * strength, 5.2 * strength * radiusScale},
			StartSizeRange:   [2]float32{0.16 * radiusScale, 0.3 * radiusScale},
			StartColorMin:    cfg.normalizedSprayColorMin(),
			StartColorMax:    cfg.normalizedSprayColorMax(),
			Gravity:          sprayGravity,
			Drag:             sprayDrag,
			ConeAngleDegrees: sprayCone,
			SpriteIndex:      cfg.normalizedSpraySprite(),
			AtlasCols:        cols,
			AtlasRows:        rows,
			Texture:          cfg.Texture,
			AlphaMode:        SpriteAlphaLuminance,
		},
		&LifetimeComponent{TimeLeft: 0.16},
	)

	cmd.AddEntity(
		&SpriteComponent{
			Enabled:       true,
			Position:      impact.Position.Add(mgl32.Vec3{0, 0.03, 0}),
			Size:          [2]float32{flashSize, flashSize},
			Color:         [4]float32{0.9, 0.98, 1.0, 0.82},
			SpriteIndex:   cfg.normalizedFlashSprite(),
			AtlasCols:     cols,
			AtlasRows:     rows,
			Texture:       cfg.Texture,
			BillboardMode: BillboardSpherical,
			Unlit:         true,
			AlphaMode:     SpriteAlphaLuminance,
		},
		&LifetimeComponent{TimeLeft: 0.12},
	)
}

func waterSplashRotationForImpact(impact WaterImpactEvent) mgl32.Quat {
	if impact.Kind != WaterDisturbanceSkim {
		return mgl32.QuatIdent()
	}
	horizontal := mgl32.Vec3{impact.Velocity.X(), 0, impact.Velocity.Z()}
	if horizontal.LenSqr() < 1e-5 {
		return mgl32.QuatIdent()
	}
	target := horizontal.Normalize().Mul(0.72).Add(mgl32.Vec3{0, 0.7, 0}).Normalize()
	up := mgl32.Vec3{0, 1, 0}
	dot := clampWaterFloat(up.Dot(target), -1, 1)
	angle := float32(math.Acos(float64(dot)))
	axis := up.Cross(target)
	if axis.LenSqr() < 1e-5 {
		return mgl32.QuatIdent()
	}
	return mgl32.QuatRotate(angle, axis.Normalize())
}

func (c *WaterSplashEffectComponent) normalizedAtlasGrid() (uint32, uint32) {
	if c == nil {
		return 4, 4
	}
	cols := c.AtlasCols
	if cols == 0 {
		cols = 4
	}
	rows := c.AtlasRows
	if rows == 0 {
		rows = 4
	}
	return cols, rows
}

func (c *WaterSplashEffectComponent) normalizedSplashSprite() uint32 {
	if c == nil || c.SplashSprite == 0 {
		return 5
	}
	return c.SplashSprite
}

func (c *WaterSplashEffectComponent) normalizedSpraySprite() uint32 {
	if c == nil || c.SpraySprite == 0 {
		return 9
	}
	return c.SpraySprite
}

func (c *WaterSplashEffectComponent) normalizedFlashSprite() uint32 {
	if c == nil || c.FlashSprite == 0 {
		return 10
	}
	return c.FlashSprite
}

func (c *WaterSplashEffectComponent) normalizedMinImpactSpeed() float32 {
	if c == nil || c.MinImpactSpeed <= 0 {
		return 2.0
	}
	return c.MinImpactSpeed
}

func (c *WaterSplashEffectComponent) normalizedStrengthScale() float32 {
	if c == nil || c.StrengthScale <= 0 {
		return 1.0
	}
	return c.StrengthScale
}

func (c *WaterSplashEffectComponent) normalizedDropletColorMin() [4]float32 {
	if c == nil || c.DropletColorMin == ([4]float32{}) {
		return [4]float32{0.82, 0.94, 1.0, 0.92}
	}
	return c.DropletColorMin
}

func (c *WaterSplashEffectComponent) normalizedDropletColorMax() [4]float32 {
	if c == nil || c.DropletColorMax == ([4]float32{}) {
		return [4]float32{1.0, 1.0, 1.0, 1.0}
	}
	return c.DropletColorMax
}

func (c *WaterSplashEffectComponent) normalizedSprayColorMin() [4]float32 {
	if c == nil || c.SprayColorMin == ([4]float32{}) {
		return [4]float32{0.72, 0.9, 1.0, 0.72}
	}
	return c.SprayColorMin
}

func (c *WaterSplashEffectComponent) normalizedSprayColorMax() [4]float32 {
	if c == nil || c.SprayColorMax == ([4]float32{}) {
		return [4]float32{0.96, 1.0, 1.0, 0.9}
	}
	return c.SprayColorMax
}
