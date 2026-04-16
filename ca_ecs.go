package gekko

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

// CellularType identifies the supported cellular volume simulation family.
//
// Production support is currently limited to smoke and fire volumes.
type CellularType uint32

const (
	CellularSmoke CellularType = iota
	CellularFire
	// Deprecated: sand volumes are not implemented by the runtime.
	// Only CellularSmoke and CellularFire are currently supported.
	CellularSand
	// Deprecated: water volumes are not implemented by the runtime.
	// Only CellularSmoke and CellularFire are currently supported.
	CellularWater
)

func (t CellularType) String() string {
	switch t {
	case CellularSmoke:
		return "CellularSmoke"
	case CellularFire:
		return "CellularFire"
	case CellularSand:
		return "CellularSand"
	case CellularWater:
		return "CellularWater"
	default:
		return fmt.Sprintf("CellularType(%d)", t)
	}
}

func (t CellularType) IsSupported() bool {
	return t == CellularSmoke || t == CellularFire
}

type CAVolumePreset uint32

const (
	CAVolumePresetDefault CAVolumePreset = iota
	CAVolumePresetTorch
	CAVolumePresetCampfire
	CAVolumePresetJetFlame
	CAVolumePresetExplosion
)

type CAVolumeAnchorMode uint32

const (
	CAVolumeAnchorCenter CAVolumeAnchorMode = iota // Default: Transform position is the volume center
	CAVolumeAnchorCorner                           // Legacy behavior: Transform position is the volume corner
	CAVolumeAnchorCustom                           // Transform position is a custom local anchor
)

// CellularVolumeComponent configures a small 3D grid and its volumetric CA behavior.
//
// Supported runtime types are CellularSmoke and CellularFire only.
// When UseIntensity is false, enabled volumes target full intensity (1.0).
// When Disabled is true, the target intensity is forced to zero.
// The grid is in local space of the entity's TransformComponent.
type CellularVolumeComponent struct {
	Resolution   [3]int // e.g., [32, 48, 32]
	Type         CellularType
	Preset       CAVolumePreset
	Disabled     bool
	AnchorMode   CAVolumeAnchorMode
	CustomAnchor mgl32.Vec3
	UseIntensity bool
	Intensity    float32
	FadeInRate   float32 // intensity units per second; <= 0 snaps to target
	FadeOutRate  float32 // intensity units per second; <= 0 snaps to target
	TickRate     float32 // Hz (10-20 is fine)

	// Rule parameters (interpreted per Type)
	Diffusion   float32 // 0..1
	Buoyancy    float32 // upward bias for smoke/fire
	Cooling     float32 // reduces temp per tick (fire)
	Dissipation float32 // reduces density per tick (all)

	// Optional render overrides for GPU volumetric CA. When disabled, preset defaults are used.
	UseAppearanceOverride bool
	ScatterColor          [3]float32
	Extinction            float32
	Emission              float32
	UseShadowTintOverride bool
	ShadowTint            [3]float32
	UseAbsorptionOverride bool
	AbsorptionColor       [3]float32

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

	_nextDensity      []float32
	_gpuStepsPending  uint32
	_currentIntensity float32
	_intensityInited  bool
}

func (cv *CellularVolumeComponent) Validate() error {
	if cv == nil {
		return nil
	}
	if !cv.Type.IsSupported() {
		return fmt.Errorf("unsupported cellular volume type %s: only CellularSmoke and CellularFire are supported", cv.Type)
	}
	return nil
}

func mustValidateCellularVolumeComponent(cv *CellularVolumeComponent) {
	if err := cv.Validate(); err != nil {
		panic(err)
	}
}

// UsesGPUVolume reports whether this component belongs to the managed GPU CA path.
// Supported fire/smoke volumes stay resident in the GPU system even at zero intensity.
func (cv *CellularVolumeComponent) UsesGPUVolume() bool {
	mustValidateCellularVolumeComponent(cv)
	return cv != nil
}

// HasVisibleGPUVolume reports whether the volume should currently contribute visible output.
func (cv *CellularVolumeComponent) HasVisibleGPUVolume() bool {
	mustValidateCellularVolumeComponent(cv)
	if cv == nil {
		return false
	}
	return cv.targetIntensity() > 0.001 || cv.CurrentIntensity() > 0.001
}

// WantsGPUVolumeSimulation reports whether the volume should advance simulation this frame.
// Volumes remain allocated in the GPU CA system when false so they can resume without reseeding.
func (cv *CellularVolumeComponent) WantsGPUVolumeSimulation() bool {
	mustValidateCellularVolumeComponent(cv)
	if cv == nil {
		return false
	}
	return cv.HasVisibleGPUVolume()
}

func clamp01(v float32) float32 {
	return float32(math.Max(0.0, math.Min(1.0, float64(v))))
}

func (cv *CellularVolumeComponent) targetIntensity() float32 {
	mustValidateCellularVolumeComponent(cv)
	if cv == nil || cv.Disabled {
		return 0
	}
	if !cv.UseIntensity {
		return 1
	}
	return clamp01(cv.Intensity)
}

func (cv *CellularVolumeComponent) CurrentIntensity() float32 {
	if cv == nil {
		return 0
	}
	if !cv._intensityInited {
		return cv.targetIntensity()
	}
	return clamp01(cv._currentIntensity)
}

func (cv *CellularVolumeComponent) advanceIntensity(dt float32) {
	if cv == nil {
		return
	}
	target := cv.targetIntensity()
	if !cv._intensityInited {
		cv._currentIntensity = target
		cv._intensityInited = true
		return
	}
	if dt <= 0 {
		dt = 1.0 / 60.0
	}
	if target > cv._currentIntensity {
		if cv.FadeInRate <= 0 {
			cv._currentIntensity = target
		} else {
			cv._currentIntensity = min(target, cv._currentIntensity+cv.FadeInRate*dt)
		}
	} else if target < cv._currentIntensity {
		if cv.FadeOutRate <= 0 {
			cv._currentIntensity = target
		} else {
			cv._currentIntensity = max(target, cv._currentIntensity-cv.FadeOutRate*dt)
		}
	}
	cv._currentIntensity = clamp01(cv._currentIntensity)
}

func (cv *CellularVolumeComponent) AnchorLocal() mgl32.Vec3 {
	mustValidateCellularVolumeComponent(cv)
	if cv == nil {
		return mgl32.Vec3{}
	}
	switch cv.AnchorMode {
	case CAVolumeAnchorCenter:
		return mgl32.Vec3{
			float32(max(0, cv.Resolution[0])) * 0.5,
			float32(max(0, cv.Resolution[1])) * 0.5,
			float32(max(0, cv.Resolution[2])) * 0.5,
		}
	case CAVolumeAnchorCustom:
		return cv.CustomAnchor
	case CAVolumeAnchorCorner:
		fallthrough
	default:
		return mgl32.Vec3{}
	}
}

func (cv *CellularVolumeComponent) AnchorWorld(tr *TransformComponent) mgl32.Vec3 {
	if cv == nil || tr == nil {
		return mgl32.Vec3{}
	}
	anchor := cv.AnchorLocal()
	return mgl32.Vec3{
		anchor.X() * tr.Scale.X() * VoxelSize,
		anchor.Y() * tr.Scale.Y() * VoxelSize,
		anchor.Z() * tr.Scale.Z() * VoxelSize,
	}
}

func (cv *CellularVolumeComponent) VolumeOrigin(tr *TransformComponent) mgl32.Vec3 {
	if cv == nil || tr == nil {
		return mgl32.Vec3{}
	}
	return tr.Position.Sub(tr.Rotation.Rotate(cv.AnchorWorld(tr)))
}

func (cv *CellularVolumeComponent) ensureGrid() {
	mustValidateCellularVolumeComponent(cv)
	nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
	if nx <= 0 || ny <= 0 || nz <= 0 {
		cv.Resolution = [3]int{32, 32, 32}
		nx, ny, nz = 32, 32, 32
	}
	if cv.UsesGPUVolume() {
		cv._inited = true
		return
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
	mustValidateCellularVolumeComponent(cv)
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
	mustValidateCellularVolumeComponent(cv)
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
				// Note: upShare + downShare = share * 2, so mass is conserved.
				upShare := share * (1.0 + buoy)
				downShare := share * (1.0 - buoy)
				if j := idx3(x, y+1, z, nx, ny, nz); j >= 0 {
					next[j] += upShare
				}
				if j := idx3(x, y-1, z, nx, ny, nz); j >= 0 {
					next[j] += downShare
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
		mustValidateCellularVolumeComponent(cv)
		cv.advanceIntensity(dt)
		if cv.Type == CellularSmoke || cv.Type == CellularFire {
			cv.ensureGrid()
			if !cv.WantsGPUVolumeSimulation() {
				cv._gpuStepsPending = 0
				return true
			}
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
			cv._accum = 0
			if cv._gpuStepsPending < 4 {
				cv._gpuStepsPending++
			}
			cv._dirty = true
			return true
		}
		if cv.Disabled {
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
		// Mark density changed for bridges (particles/voxels) after a simulation step
		cv._dirty = true
		return true
	})
}

// bridgeCellsToParticles appends billboard instances based on active cells.
// It returns the number appended (with a cap).
func bridgeCellsToParticles(cmd *Commands, instances *[]core.ParticleInstance, maxAppend int) int {
	added := 0
	// Capture camera position once for adaptive LOD
	camPos := mgl32.Vec3{}
	MakeQuery1[CameraComponent](cmd).Map(func(eid EntityId, cam *CameraComponent) bool {
		if cam != nil {
			camPos = cam.Position
		}
		return false
	})
	MakeQuery2[TransformComponent, CellularVolumeComponent](cmd).Map(func(eid EntityId, tr *TransformComponent, cv *CellularVolumeComponent) bool {
		if cv == nil {
			return true
		}
		mustValidateCellularVolumeComponent(cv)
		if !cv.BridgeToParticles || cv._density == nil {
			return true
		}
		intensity := cv.CurrentIntensity()
		if intensity <= 0.001 {
			return true
		}
		nx, ny, nz := cv.Resolution[0], cv.Resolution[1], cv.Resolution[2]
		if nx <= 0 || ny <= 0 || nz <= 0 {
			return true
		}

		cellSize := mgl32.Vec3{
			tr.Scale.X() * VoxelSize,
			tr.Scale.Y() * VoxelSize,
			tr.Scale.Z() * VoxelSize,
		}
		anchorWorld := cv.AnchorWorld(tr)

		// Adaptive sampling based on camera distance
		dist := tr.Position.Sub(camPos).Len()
		threshold := float32(0.01)
		stride := 1
		if dist > 80.0 {
			stride = 4
			threshold = 0.07
		} else if dist > 30.0 {
			stride = 2
			threshold = 0.03
		}
		// small jitter to avoid grid look
		jx := func() float32 { return (rand.Float32() - 0.5) * 0.8 * cellSize.X() }
		jy := func() float32 { return (rand.Float32() - 0.5) * 0.8 * cellSize.Y() }
		jz := func() float32 { return (rand.Float32() - 0.5) * 0.8 * cellSize.Z() }

		for z := 0; z < nz && added < maxAppend; z += stride {
			for y := 0; y < ny && added < maxAppend; y += stride {
				for x := 0; x < nx && added < maxAppend; x += stride {
					i := idx3(x, y, z, nx, ny, nz)
					d := cv._density[i]
					if d > threshold {
						localPos := mgl32.Vec3{
							float32(x)*cellSize.X() + jx(),
							float32(y)*cellSize.Y() + jy(),
							float32(z)*cellSize.Z() + jz(),
						}
						wp := tr.Position.Add(tr.Rotation.Rotate(localPos.Sub(anchorWorld)))
						// bright additive colors for visibility
						// col := [4]float32{1.2, 1.2, 1.2, 1.0} // smoke
						// if cv.Type == CellularFire {
						// 	col = [4]float32{2.0, 0.8, 0.2, 1.0} // hotter fire
						// }
						// size := 1.5 * cellSize
						// Density mapping for alpha/size and life
						dv := float32(0.0)
						if d > threshold {
							dv = (d - threshold) / (1.0 - threshold)
							if dv < 0 {
								dv = 0
							} else if dv > 1 {
								dv = 1
							}
						}
						// Color and alpha from density
						colA := 0.15 + 0.6*dv // 0.15..0.75
						col := [4]float32{0.7, 0.7, 0.7, colA}
						if cv.Type == CellularFire {
							col = [4]float32{1.0, 0.35 + 0.4*dv, 0.05, colA}
						}
						col[3] *= intensity
						// Size variation by density
						base := cellSize.Len() / 3.0
						size := base * (0.8 + 0.6*dv)
						// Life mid-range to ensure fade-in/out
						lp := 0.3 + 0.4*rand.Float32()
						*instances = append(*instances, core.ParticleInstance{
							Pos:      [3]float32{wp.X(), wp.Y(), wp.Z()},
							Size:     size,
							Color:    col,
							Velocity: [3]float32{0, 0, 0}, // Default (camera aligned)
							Life:     lp,
							MaxLife:  1.0,
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
