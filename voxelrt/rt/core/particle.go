package core

// ParticleInstance matches WGSL layout in particles_billboard.wgsl
// struct ParticleInstance { vec3 pos; float size; vec4 color; }
// Particle matches WGSL layout in particles_billboard.wgsl and particles_sim.wgsl
type Particle struct {
	Pos      [3]float32
	Size     float32
	Color    [4]float32
	Velocity [3]float32
	Life     float32
	MaxLife  float32
	Gravity  float32
	Drag     float32
	Pad      float32 // Pad to 64 bytes
}

// ParticleInstance is used for rendering if we want a separate compact struct,
// but for now we'll just use Particle directly in the shader.
type ParticleInstance = Particle
