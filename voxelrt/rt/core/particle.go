package core

// ParticleInstance matches WGSL layout in particles_billboard.wgsl
// struct ParticleInstance { vec3 pos; float size; vec4 color; }
// Particle matches WGSL layout in particles_billboard.wgsl and particles_sim.wgsl
type Particle struct {
	Pos         [3]float32
	Size        float32
	Color       [4]float32
	Velocity    [3]float32
	Life        float32
	MaxLife     float32
	Gravity     float32
	Drag        float32
	SpriteIndex uint32
	AtlasCols   uint32
	AtlasRows   uint32
	Pad1        uint32 // To align to 16-bytes (WGSL 80 bytes total padding)
	Pad2        uint32
}

// ParticleInstance is used for rendering if we want a separate compact struct,
// but for now we'll just use Particle directly in the shader.
type ParticleInstance = Particle
