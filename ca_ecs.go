package gekko

import (
	"math/rand"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// CellularType is a coarse preset for CA rules.
type CellularType uint32

const (
	CellularSmoke CellularType = iota
	CellularFire
	CellularSand
	CellularWater
)

// CellularVolumeComponent holds a small 3D grid and rule params.
// The grid is in local space of the entity's TransformComponent.
type CellularVolumeComponent struct {
	Resolution [3]int // e.g., [32, 48, 32]
	Type       CellularType
	TickRate   float32 // Hz (10-20 is fine)

	// Rule parameters (interpreted per Type)
	Diffusion   float32 // 0..1
	Buoyancy    float32 // upward bias for smoke/fire
	Cooling     float32 // reduces temp per tick (fire)
	Dissipation float32 // reduces density per tick (all)

	BridgeToParticles bool // When true, particlesCollect will spawn billboards at active cells

	// Voxel bridge (render CA as transient voxel volume)
	BridgeToVoxels    bool    // When true, creates/updates a VoxelObject from CA density
	VoxelThreshold    float32 // Density threshold for voxelization (default ~0.10 if <= 0)
	VoxelStride       int     // Downsampling stride across the CA grid (default 1 if <= 0)
	VoxelsCastShadows bool    // If true, CA voxels cast shadows (defaults to false to keep cost down)

	// Internal state
	_density []float32
	_temp    []float32
	_accum   float32
	_inited  bool
	_dirty   bool // set true whenever a simulation step occurs

	// CA→Voxels delta state
	_prevMask      []byte
	_prevStride    int
	_prevThreshold float32
	_prevType      CellularType

	_nextDensity []float32
}

func (cv *CellularVolumeComponent) ensureGrid() {
	nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
	if nx <= 0 || ny <= 0 || nz <= 0 {
		cv.Resolution = [3]int{32, 32, 32}
		nx, ny, nz = 32, 32, 32
	}
	total := nx * ny * nz
	if cv._density == nil || len(cv._density) != total {
		cv._density = make([]float32, total)
		cv._nextDensity = make([]float32, total)
		cv._temp = make([]float32, total)
		cv.seed() // Seed initial density
	}
	cv._inited = true
}

func idx3(x, y, z, nx, ny, nz int) int {
	if x < 0 || x >= nx || y < 0 || y >= ny || z < 0 || z >= nz {
		return -1
	}
	return x + y*nx + z*nx*ny
}

// seed some initial smoke/fire if empty
func (cv *CellularVolumeComponent) seed() {
	nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
	// small plume near the bottom center
	cx, cz := nx/2, nz/2
	for s := 0; s < nx*nz/16; s++ {
		x := cx + rand.Intn(nx/8) - nx/16
		z := cz + rand.Intn(nz/8) - nz/16
		y := 1 + rand.Intn(2)
		i := idx3(x, y, z, nx, ny, nz)
		if i >= 0 {
			cv._density[i] += 1.0
			if cv._density[i] > 1.0 {
				cv._density[i] = 1.0
			}
			if cv.Type == CellularFire {
				cv._temp[i] = 1.0
			}
		}
	}
}

func (cv *CellularVolumeComponent) stepSmoke(dt float32) {
	nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
	// Reuse next buffer and clear it
	next := cv._nextDensity
	if len(next) != len(cv._density) {
		next = make([]float32, len(cv._density))
		cv._nextDensity = next
	}
	for i := range next {
		next[i] = 0
	}

	// Simple diffusion (6-neighborhood) and buoyancy (upward advection)
	dif := cv.Diffusion
	if dif < 0 {
		dif = 0
	}
	if dif > 1 {
		dif = 1
	}
	decay := 1.0 - cv.Dissipation
	if decay < 0 {
		decay = 0
	}
	if decay > 1 {
		decay = 1
	}
	buoy := cv.Buoyancy
	// Clamp buoyancy to prevent negative shares
	if buoy < -1 {
		buoy = -1
	}
	if buoy > 1 {
		buoy = 1
	}

	const densityCutoff = 0.001

	for z := 0; z < nz; z++ {
		for y := 0; y < ny; y++ {
			for x := 0; x < nx; x++ {
				i := idx3(x, y, z, nx, ny, nz)
				d := cv._density[i] * decay
				if d <= densityCutoff {
					continue
				}
				share := d * dif * 0.16666667 // 1/6 to neighbors
				remain := d - share*6.0
				if remain < 0 {
					remain = 0
				}

				// Distribute to neighbors
				next[i] += remain

				if j := idx3(x+1, y, z, nx, ny, nz); j >= 0 {
					next[j] += share
				}
				if j := idx3(x-1, y, z, nx, ny, nz); j >= 0 {
					next[j] += share
				}
				if j := idx3(x, y, z+1, nx, ny, nz); j >= 0 {
					next[j] += share
				}
				if j := idx3(x, y, z-1, nx, ny, nz); j >= 0 {
					next[j] += share
				}
				// Vertical neighbors with buoyancy bias
				upShare := share * (1.0 + buoy)
				downShare := share * (1.0 - buoy)
				if j := idx3(x, y+1, z, nx, ny, nz); j >= 0 {
					next[j] += upShare
				} else {
					next[i] += upShare // hit ceiling -> keep
				}
				if j := idx3(x, y-1, z, nx, ny, nz); j >= 0 {
					next[j] += downShare
				} else {
					next[i] += downShare // hit floor -> keep
				}
			}
		}
	}
	// Swap buffers
	cv._density, cv._nextDensity = cv._nextDensity, cv._density
}

func caStepSystem(t *Time, cmd *Commands) {
	dt := float32(t.Dt)
	if dt <= 0 {
		dt = 1 / 60.0
	}

	MakeQuery2[TransformComponent, CellularVolumeComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, cv *CellularVolumeComponent) bool {
		if cv == nil {
			return true
		}
		cv.ensureGrid()
		// (seed is now inside ensureGrid)
		// accumulate time and step at TickRate
		target := float32(1.0)
		if cv.TickRate > 0 {
			target = 1.0 / cv.TickRate
		} else {
			cv.TickRate = 15.0
			target = 1.0 / 15.0
		}
		cv._accum += dt
		if cv._accum < target {
			return true
		}
		// consume accumulated (single step to avoid stutter)
		cv._accum = 0

		// continuous injection to keep plume alive
		cv.seed()

		switch cv.Type {
		case CellularSmoke, CellularFire:
			cv.stepSmoke(dt)
			// Fire cooling: lower temperature gradually, bleed into density as faint emissive hint (used by bridging)
			if cv.Type == CellularFire && cv.Cooling > 0 {
				for i := range cv._temp {
					cv._temp[i] *= (1.0 - cv.Cooling)
					if cv._temp[i] < 0 {
						cv._temp[i] = 0
					}
				}
			}
		case CellularSand:
			// TODO: basic sand settle (not implemented in MVP)
		case CellularWater:
			// TODO: basic water flow (not implemented in MVP)
		}
		// Mark density changed for bridges (particles/voxels) after a simulation step
		cv._dirty = true
		return true
	})
}

// bridgeCellsToParticles appends billboard instances based on active cells.
// It returns the number appended (with a cap).
func bridgeCellsToParticles(cmd *Commands, instances *[]core.ParticleInstance, maxAppend int) int {
	added := 0
	MakeQuery2[TransformComponent, CellularVolumeComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, cv *CellularVolumeComponent) bool {
		if cv == nil || !cv.BridgeToParticles || cv._density == nil {
			return true
		}
		nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
		if nx <= 0 || ny <= 0 || nz <= 0 {
			return true
		}

		// World placement: map local [0..nx,0..ny,0..nz] to world with Transform scale+position.
		// Use a unit cell of 1.0 and scale with Transform.Scale's max component.
		cellSize := float32(1.0)
		scale := tr.Scale
		// choose largest component to keep cubic-ish
		smax := scale.X()
		if scale.Y() > smax {
			smax = scale.Y()
		}
		if scale.Z() > smax {
			smax = scale.Z()
		}
		cellSize *= smax

		// Sample every other cell to keep counts under control; threshold density
		// threshold := float32(0.05)
		threshold := float32(0.01)
		stride := 1
		// small jitter to avoid grid look
		// j := func() float32 { return (rand.Float32() - 0.5) * 0.6 * cellSize }
		j := func() float32 { return (rand.Float32() - 0.5) * 0.8 * cellSize }

		for z := 0; z < nz && added < maxAppend; z += stride {
			for y := 0; y < ny && added < maxAppend; y += stride {
				for x := 0; x < nx && added < maxAppend; x += stride {
					i := idx3(x, y, z, nx, ny, nz)
					d := cv._density[i]
					if d > threshold {
						localPos := mgl32.Vec3{float32(x)*cellSize + j(), float32(y)*cellSize + j(), float32(z)*cellSize + j()}
						wp := tr.Position.Add(tr.Rotation.Rotate(localPos))
						// bright additive colors for visibility
						// col := [4]float32{1.2, 1.2, 1.2, 1.0} // smoke
						// if cv.Type == CellularFire {
						// 	col = [4]float32{2.0, 0.8, 0.2, 1.0} // hotter fire
						// }
						// size := 1.5 * cellSize
						// gray for smoke, orange tint for fire
						col := [4]float32{0.7, 0.7, 0.7, 1.0}
						if cv.Type == CellularFire {
							col = [4]float32{1.0, 0.4, 0.1, 1.0}
						}
						size := 1.0 * cellSize
						*instances = append(*instances, core.ParticleInstance{
							Pos:      [3]float32{wp.X(), wp.Y(), wp.Z()},
							Size:     size,
							Color:    col,
							Velocity: [3]float32{0, 0, 0}, // Default (camera aligned)
							LifePct:  0.0,                 // Default
						})
						added++
					}
				}
			}
		}
		return true
	})
	return added
}

// Extend particlesCollect to bridge CA volumes to particles billboards.
// This function is called from voxelRtSystem after voxel updates.
// NOTE: this file lives in gekko package, so we can call bridgeCellsToParticles from there.
func init() {
	// Wrap original implementation by replacing the function pointer if needed.
	// Here we simply rely on particlesCollect calling this helper explicitly from its implementation file.
	// (No-op placeholder: documentation note.)
}
