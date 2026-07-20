package gekko

import (
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
)

// ParticleEmitterComponent controls a CPU-simulated particle emitter.
// Keep parameters minimal for a cheap MVP; can extend later.
type ParticleEmitterComponent struct {
	Enabled bool

	MaxParticles int // Conservative per-emitter live cap; <=0 is unlimited.

	SpawnRate        float32    // particles per second
	LifetimeRange    [2]float32 // seconds (min,max)
	StartSpeedRange  [2]float32 // units/sec (min,max)
	StartSizeRange   [2]float32 // world units (min,max)
	StartColorMin    [4]float32 // RGBA min (0..1)
	StartColorMax    [4]float32 // RGBA max (0..1)
	Gravity          float32    // positive acceleration downward (m/s^2)
	Drag             float32    // per-second linear drag (0..inf)
	ConeAngleDegrees float32    // 0=along emitter up axis; larger spreads
	SpriteIndex      uint32
	AtlasCols        uint32          // Number of columns in the atlas
	AtlasRows        uint32          // Number of rows in the atlas
	Texture          AssetId         // Asset ID of the texture atlas
	AlphaMode        SpriteAlphaMode // How to derive transparency from the atlas
}

type particlePool struct {
	spawnAcc float32
	live     uint32
	batches  []particleBatch
	seen     bool
}

type particleBatch struct {
	count     uint32
	remaining float32
}

func (pool *particlePool) expire(dt float32) {
	active := pool.batches[:0]
	for _, batch := range pool.batches {
		batch.remaining -= dt
		if batch.remaining > 0 {
			active = append(active, batch)
			continue
		}
		pool.live -= batch.count
	}
	pool.batches = active
}

// particlesSync collects emitter data and generates spawn requests for the GPU.
func particlesSync(state *VoxelRtState, t *Time, cmd *Commands) ([]uint32, []app_rt.ParticleEmitterInput, AssetId) {
	dt := float32(t.Dt)
	if dt <= 0 {
		dt = 1.0 / 60.0
	}

	camPos := state.RtApp.Camera.Position

	if state.particlePools == nil {
		state.particlePools = make(map[EntityId]*particlePool) // Reuse the map
	}

	emitterParams := make([]app_rt.ParticleEmitterInput, 0, 32)
	spawnRequests := make([]uint32, 0, 128)
	for _, pool := range state.particlePools {
		pool.seen = false
	}
	var firstAtlas AssetId

	MakeQuery2[TransformComponent, ParticleEmitterComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, em *ParticleEmitterComponent) bool {
		if em == nil || !em.Enabled {
			return true
		}
		if firstAtlas == (AssetId{}) && em.Texture != (AssetId{}) {
			firstAtlas = em.Texture
		}

		// Optional distance cull
		distSq := tr.Position.Sub(camPos).LenSqr()
		if distSq > 40000.0 { // 200m
			return true
		}

		// Persistent state for spawn accumulation
		var es *particlePool
		var ok bool
		es, ok = state.particlePools[eid]
		if !ok {
			es = &particlePool{}
			state.particlePools[eid] = es
		}
		es.seen = true
		es.expire(dt)

		es.spawnAcc += em.SpawnRate * dt
		spawnCount := uint32(es.spawnAcc)
		if spawnCount > 0 {
			es.spawnAcc -= float32(spawnCount)
			// Cap spawn count to avoid GPU spikes per frame if needed
			if spawnCount > 1024 {
				spawnCount = 1024
			}
			if em.MaxParticles > 0 {
				limit := uint32(em.MaxParticles)
				if es.live >= limit {
					spawnCount = 0
				} else if remaining := limit - es.live; spawnCount > remaining {
					spawnCount = remaining
				}
			}
			if spawnCount > 0 {
				maxLife := max(em.LifetimeRange[0], em.LifetimeRange[1])
				if maxLife > 0 {
					es.live += spawnCount
					es.batches = append(es.batches, particleBatch{count: spawnCount, remaining: maxLife})
				}
			}

			emitterIdx := uint32(len(emitterParams))
			for i := uint32(0); i < spawnCount; i++ {
				spawnRequests = append(spawnRequests, emitterIdx)
			}
		}

		cols := em.AtlasCols
		if cols == 0 {
			cols = 1
		}
		rows := em.AtlasRows
		if rows == 0 {
			rows = 1
		}

		// Pack Params
		p := app_rt.ParticleEmitterInput{
			Pos:         [3]float32{tr.Position.X(), tr.Position.Y(), tr.Position.Z()},
			SpawnCount:  spawnCount,
			Rot:         [4]float32{tr.Rotation.V[0], tr.Rotation.V[1], tr.Rotation.V[2], tr.Rotation.W},
			LifeMin:     em.LifetimeRange[0],
			LifeMax:     em.LifetimeRange[1],
			SpeedMin:    em.StartSpeedRange[0],
			SpeedMax:    em.StartSpeedRange[1],
			SizeMin:     em.StartSizeRange[0],
			SizeMax:     em.StartSizeRange[1],
			Gravity:     em.Gravity,
			Drag:        em.Drag,
			ColorMin:    em.StartColorMin,
			ColorMax:    em.StartColorMax,
			ConeAngle:   em.ConeAngleDegrees,
			SpriteIndex: em.SpriteIndex,
			AtlasCols:   cols,
			AtlasRows:   rows,
			AlphaMode:   uint32(em.AlphaMode),
		}
		emitterParams = append(emitterParams, p)

		return true
	})
	for eid, pool := range state.particlePools {
		if !pool.seen {
			delete(state.particlePools, eid)
		}
	}

	return spawnRequests, emitterParams, firstAtlas
}

// Keep the old bridge logic if we want to still support it, but it needs to be GPU-ified too.
// For now, let's just return empty from it or stub it.
