package core

// ParticleInstance matches WGSL layout in particles_billboard.wgsl
// struct ParticleInstance { vec3 pos; float size; vec4 color; }
type ParticleInstance struct {
	Pos   [3]float32
	Size  float32
	Color [4]float32
}
