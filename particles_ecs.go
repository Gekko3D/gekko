package gekko

import (
	"math"
	"math/rand"
	"runtime"
	"sync"
	"time"

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

// RNG-backed variant (no global rand contention)
func sampleDirectionRng(rot mgl32.Quat, coneDeg float32, rng *rand.Rand) mgl32.Vec3 {
	axis := mgl32.Vec3{0, 1, 0}
	if coneDeg <= 0.0 {
		return rot.Rotate(axis).Normalize()
	}
	thetaMax := float32(math.Pi) * (coneDeg / 180.0)
	u := rng.Float32()
	v := rng.Float32()
	cosTheta := lerp(float32(math.Cos(float64(thetaMax))), 1.0, u)
	sinTheta := float32(math.Sqrt(float64(1.0 - cosTheta*cosTheta)))
	phi := 2.0 * float32(math.Pi) * v
	local := mgl32.Vec3{
		float32(math.Cos(float64(phi))) * sinTheta,
		cosTheta,
		float32(math.Sin(float64(phi))) * sinTheta,
	}
	return rot.Rotate(local).Normalize()
}

// Worker-pool infrastructure and scratch buffers to minimize allocations.

var (
	instBufPool = sync.Pool{
		New: func() any {
			b := make([]core.ParticleInstance, 0, 1024)
			return &b
		},
	}
	// Reused backing storage for the final packed instances per frame.
	particlesScratch []core.ParticleInstance

	// Optional far-distance culling (skip emitters entirely beyond this distance)
	farCullDistanceSq float32 = 200.0 * 200.0 // 200m
)

// emitterJob is a snapshot of an emitter and its pool (to avoid ECS data races)
type emitterJob struct {
	pos mgl32.Vec3
	rot mgl32.Quat
	em  ParticleEmitterComponent // value copy
	pl  *particlePool
}

// simulateEmitter integrates and packs particles for a single emitter into out.
// Returns the slice window with appended instances.
func simulateEmitter(job emitterJob, dt float32, camPos mgl32.Vec3, rng *rand.Rand, out []core.ParticleInstance) []core.ParticleInstance {
	// Optional early distance cull
	distSq := job.pos.Sub(camPos).LenSqr()
	if distSq > farCullDistanceSq {
		return out
	}

	// LOD factor (reuse thresholds from original)
	lodFactor := float32(1.0)
	if distSq > 100.0*100.0 {
		lodFactor = 0.1
	} else if distSq > 50.0*50.0 {
		lodFactor = 0.5
	}

	pl := job.pl
	em := job.em

	// Spawn
	pl.spawnAcc += em.SpawnRate * dt * lodFactor
	spawnCount := int(pl.spawnAcc)
	if spawnCount > 0 {
		pl.spawnAcc -= float32(spawnCount)
	}
	if rem := em.MaxParticles - pl.alive; spawnCount > rem {
		spawnCount = rem
	}

	for i := 0; i < spawnCount; i++ {
		idx := pl.alive
		pl.alive++

		pl.pos[idx] = job.pos

		dir := sampleDirectionRng(job.rot, em.ConeAngleDegrees, rng)
		speed := lerp(em.StartSpeedRange[0], em.StartSpeedRange[1], rng.Float32())
		pl.vel[idx] = dir.Mul(speed)

		pl.age[idx] = 0
		pl.life[idx] = lerp(em.LifetimeRange[0], em.LifetimeRange[1], rng.Float32())
		pl.size[idx] = lerp(em.StartSizeRange[0], em.StartSizeRange[1], rng.Float32())

		var c [4]float32
		for j := 0; j < 4; j++ {
			c[j] = lerp(em.StartColorMin[j], em.StartColorMax[j], rng.Float32())
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
			// swap-remove
			last := pl.alive - 1
			pl.pos[i] = pl.pos[last]
			pl.vel[i] = pl.vel[last]
			pl.age[i] = pl.age[last]
			pl.life[i] = pl.life[last]
			pl.size[i] = pl.size[last]
			pl.color[i] = pl.color[last]
			pl.alive--
			continue
		}

		pl.vel[i] = v
		pl.pos[i] = p
		pl.age[i] = age
		i++
	}

	// Pack
	for i = 0; i < pl.alive; i++ {
		p := pl.pos[i]
		life := pl.life[i]
		if life <= 0 {
			life = 1.0
		}
		vel := pl.vel[i]
		out = append(out, core.ParticleInstance{
			Pos:      [3]float32{p.X(), p.Y(), p.Z()},
			Size:     pl.size[i],
			Color:    pl.color[i],
			Velocity: [3]float32{vel.X(), vel.Y(), vel.Z()},
			LifePct:  pl.age[i] / life,
		})
	}
	return out
}

// particlesCollect updates all emitters and returns packed instances for rendering.
// Parallel over emitters, with per-job buffers to avoid contention and minimal allocations.
func particlesCollect(state *VoxelRtState, t *Time, cmd *Commands) []core.ParticleInstance {
	// dt sanitize
	dt := float32(t.Dt)
	if dt <= 0 {
		dt = 1.0 / 60.0
	}

	// Snapshot camera position once
	camPos := state.rtApp.Camera.Position

	// Phase 1: collect jobs and ensure pools on the main goroutine (avoid map races)
	jobs := make([]emitterJob, 0, 32)
	MakeQuery2[TransformComponent, ParticleEmitterComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, em *ParticleEmitterComponent) bool {
		if em == nil || !em.Enabled || em.MaxParticles <= 0 {
			return true
		}
		pl := ensurePool(state, eid, em.MaxParticles)

		// Build job snapshot
		job := emitterJob{
			pos: tr.Position,
			rot: tr.Rotation,
			em:  *em, // value copy
			pl:  pl,
		}
		// Optional: very far cull at collection stage (cheap early out)
		if job.pos.Sub(camPos).LenSqr() > farCullDistanceSq {
			return true
		}
		jobs = append(jobs, job)
		return true
	})

	// Prepare final scratch buffer (reuse backing across frames)
	particlesScratch = particlesScratch[:0]

	if len(jobs) == 0 {
		// Still allow CA to append some particles
		_ = bridgeCellsToParticles(cmd, &particlesScratch, 2000)
		return particlesScratch
	}

	// Phase 2: worker pool
	workerCount := runtime.GOMAXPROCS(0)
	if workerCount > 8 {
		workerCount = 8
	}
	if workerCount > len(jobs) {
		workerCount = len(jobs)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	jobCh := make(chan emitterJob)
	resCh := make(chan *[]core.ParticleInstance)

	var wg sync.WaitGroup
	wg.Add(workerCount)

	seedBase := time.Now().UnixNano()

	for w := 0; w < workerCount; w++ {
		go func(widx int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seedBase + int64(widx+1)*0x9e3779b1))
			for job := range jobCh {
				// Get a buffer from pool for this job
				bufPtr := instBufPool.Get().(*[]core.ParticleInstance)
				buf := (*bufPtr)[:0]
				buf = simulateEmitter(job, dt, camPos, rng, buf)
				*bufPtr = buf
				resCh <- bufPtr
			}
		}(w)
	}

	go func() {
		wg.Wait()
		close(resCh)
	}()

	// Feed jobs
	for _, j := range jobs {
		jobCh <- j
	}
	close(jobCh)

	// Aggregate results to final slice and return buffers to pool
	for bufPtr := range resCh {
		buf := *bufPtr
		// Ensure capacity grows geometrically to reduce copies
		if len(particlesScratch)+len(buf) > cap(particlesScratch) {
			newCap := cap(particlesScratch)*3/2 + len(buf) + 1
			if newCap < 1024 {
				newCap = 1024
			}
			newArr := make([]core.ParticleInstance, len(particlesScratch), newCap)
			copy(newArr, particlesScratch)
			particlesScratch = newArr
		}
		particlesScratch = append(particlesScratch, buf...)
		// Reset and put back
		*bufPtr = (*bufPtr)[:0]
		instBufPool.Put(bufPtr)
	}

	// Bridge CA volumes to particles (cheap volumetric look). Cap to avoid spikes.
	_ = bridgeCellsToParticles(cmd, &particlesScratch, 2000)

	return particlesScratch
}
