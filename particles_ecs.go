package gekko

import (
	"math"
	"math/rand"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// ParticleEmitterComponent controls a CPU-simulated particle emitter.
// Keep parameters minimal for a cheap MVP; can extend later.
type ParticleEmitterComponent struct {
	Enabled bool

	MaxParticles int

	SpawnRate        float32    // particles per second
	LifetimeRange    [2]float32 // seconds (min,max)
	StartSpeedRange  [2]float32 // units/sec (min,max)
	StartSizeRange   [2]float32 // world units (min,max)
	StartColorMin    [4]float32 // RGBA min (0..1)
	StartColorMax    [4]float32 // RGBA max (0..1)
	Gravity          float32    // positive acceleration downward (m/s^2)
	Drag             float32    // per-second linear drag (0..inf)
	ConeAngleDegrees float32    // 0=along emitter up axis; larger spreads
}

// Internal pool per emitter (SoA + ring-like behavior)
type particlePool struct {
	pos   []mgl32.Vec3
	vel   []mgl32.Vec3
	age   []float32
	life  []float32
	size  []float32
	color [][4]float32

	alive    int
	spawnAcc float32 // fractional spawns accumulator
	capacity int
}

// Ensure pool exists and sized
func ensurePool(state *VoxelRtState, eid EntityId, cap int) *particlePool {
	if state.particlePools == nil {
		state.particlePools = make(map[EntityId]*particlePool)
	}
	pl, ok := state.particlePools[eid]
	if !ok {
		pl = &particlePool{}
		state.particlePools[eid] = pl
	}
	if cap <= 0 {
		cap = 1
	}
	if pl.capacity != cap || pl.pos == nil {
		pl.capacity = cap
		pl.pos = make([]mgl32.Vec3, cap)
		pl.vel = make([]mgl32.Vec3, cap)
		pl.age = make([]float32, cap)
		pl.life = make([]float32, cap)
		pl.size = make([]float32, cap)
		pl.color = make([][4]float32, cap)
		pl.alive = 0
		pl.spawnAcc = 0
	}
	return pl
}

func lerp(a, b, t float32) float32 { return a + (b-a)*t }

// Sample a direction in a cone around the emitter's up axis (0,1,0), then rotate by emitter rotation.
// Uniform distribution over the cone.
func sampleDirection(rot mgl32.Quat, coneDeg float32) mgl32.Vec3 {
	axis := mgl32.Vec3{0, 1, 0} // emitter up axis
	if coneDeg <= 0.0 {
		return rot.Rotate(axis).Normalize()
	}
	thetaMax := float32(math.Pi) * (coneDeg / 180.0)
	u := rand.Float32()
	v := rand.Float32()
	cosTheta := lerp(float32(math.Cos(float64(thetaMax))), 1.0, u)
	sinTheta := float32(math.Sqrt(float64(1.0 - cosTheta*cosTheta)))
	phi := 2.0 * float32(math.Pi) * v

	// Local basis where Y is axis
	local := mgl32.Vec3{
		float32(math.Cos(float64(phi))) * sinTheta,
		cosTheta,
		float32(math.Sin(float64(phi))) * sinTheta,
	}
	// Rotate local basis Y to world axis via quaternion that maps Y->axis (approx using rot to orient)
	// Simplest: interpret local in emitter local space where up=Y, then rotate by emitter rotation.
	return rot.Rotate(local).Normalize()
}

// Swap-remove one particle
func (p *particlePool) killAt(i int) {
	last := p.alive - 1
	p.pos[i] = p.pos[last]
	p.vel[i] = p.vel[last]
	p.age[i] = p.age[last]
	p.life[i] = p.life[last]
	p.size[i] = p.size[last]
	p.color[i] = p.color[last]
	p.alive--
}

// particlesCollect updates all emitters and returns packed instances for rendering.
func particlesCollect(state *VoxelRtState, t *Time, cmd *Commands) []core.ParticleInstance {
	instances := make([]core.ParticleInstance, 0, 1024)

	dt := float32(t.Dt)
	if dt <= 0 {
		dt = 1.0 / 60.0
	}

	MakeQuery2[TransformComponent, ParticleEmitterComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, em *ParticleEmitterComponent) bool {
		if em == nil || !em.Enabled || em.MaxParticles <= 0 {
			// If pool exists, keep but skip simulation
			return true
		}
		pl := ensurePool(state, eid, em.MaxParticles)

		// Spawn
		pl.spawnAcc += em.SpawnRate * dt
		spawnCount := int(pl.spawnAcc)
		if spawnCount > 0 {
			pl.spawnAcc -= float32(spawnCount)
		}
		if spawnCount > (em.MaxParticles - pl.alive) {
			spawnCount = em.MaxParticles - pl.alive
		}

		for i := 0; i < spawnCount; i++ {
			idx := pl.alive
			pl.alive++

			pl.pos[idx] = tr.Position

			dir := sampleDirection(tr.Rotation, em.ConeAngleDegrees)
			speed := lerp(em.StartSpeedRange[0], em.StartSpeedRange[1], rand.Float32())
			pl.vel[idx] = dir.Mul(speed)

			pl.age[idx] = 0
			pl.life[idx] = lerp(em.LifetimeRange[0], em.LifetimeRange[1], rand.Float32())
			pl.size[idx] = lerp(em.StartSizeRange[0], em.StartSizeRange[1], rand.Float32())

			var c [4]float32
			for j := 0; j < 4; j++ {
				c[j] = lerp(em.StartColorMin[j], em.StartColorMax[j], rand.Float32())
			}
			pl.color[idx] = c
		}

		// Integrate
		drag := float32(math.Max(0, float64(1.0-em.Drag*dt)))
		grav := em.Gravity
		i := 0
		for i < pl.alive {
			v := pl.vel[i]
			// gravity downward (negative Y axis)
			v = v.Add(mgl32.Vec3{0, -grav * dt, 0})
			v = v.Mul(drag)
			p := pl.pos[i].Add(v.Mul(dt))

			age := pl.age[i] + dt
			life := pl.life[i]

			if age >= life {
				pl.killAt(i)
				continue
			}

			pl.vel[i] = v
			pl.pos[i] = p
			pl.age[i] = age
			i++
		}

		// Pack instances
		// Note: We could frustum-cull per emitter later.
		for i = 0; i < pl.alive; i++ {
			p := pl.pos[i]
			instances = append(instances, core.ParticleInstance{
				Pos:   [3]float32{p.X(), p.Y(), p.Z()},
				Size:  pl.size[i],
				Color: pl.color[i],
			})
		}
		return true
	})

	// Bridge CA volumes to particles (cheap volumetric look). Cap to avoid spikes.
	_ = bridgeCellsToParticles(cmd, &instances, 2000)

	return instances
}
