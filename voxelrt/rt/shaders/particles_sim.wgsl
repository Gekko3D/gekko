// particles_sim.wgsl

struct Particle {
    pos: vec3<f32>,
    size: f32,
    color: vec4<f32>,
    velocity: vec3<f32>,
    life: f32,
    max_life: f32,
    gravity: f32,
    drag: f32,
    pad3: f32,
};

struct EmitterParams {
    pos: vec3<f32>,
    spawn_count: u32,

    rot: vec4<f32>,

    life_min: f32,
    life_max: f32,
    speed_min: f32,
    speed_max: f32,

    size_min: f32,
    size_max: f32,
    gravity: f32,
    drag: f32,

    color_min: vec4<f32>,
    color_max: vec4<f32>,

    cone_angle: f32,
    pad1: f32,
    pad2: f32,
    pad3: f32,
};

struct SimulationParams {
    dt: f32,
    seed: u32,
    max_particles: u32,
    emitter_count: u32,
};

struct Counters {
    dead_count: atomic<u32>,
    alive_count: atomic<u32>,
    spawn_request_count: atomic<u32>,
    pad: u32,
};

struct DrawIndirectArgs {
    vertex_count: u32,
    instance_count: atomic<u32>,
    first_vertex: u32,
    first_instance: u32,
};

struct SpawnRequest {
    emitter_idx: u32,
};

@group(0) @binding(0) var<uniform> params: SimulationParams;
@group(0) @binding(1) var<storage, read_write> particles: array<Particle>;
@group(0) @binding(2) var<storage, read_write> dead_pool: array<u32>;
@group(0) @binding(3) var<storage, read_write> alive_list: array<u32>;
@group(0) @binding(4) var<storage, read_write> counters: Counters;
@group(0) @binding(5) var<storage, read_write> draw_args: DrawIndirectArgs;
@group(1) @binding(0) var<storage, read> emitters: array<EmitterParams>;
@group(1) @binding(1) var<storage, read> spawn_requests: array<SpawnRequest>;

// PCG hash for random numbers
fn pcg_hash(input: u32) -> u32 {
    var state = input * 747796405u + 2891336453u;
    var word = ((state >> ((state >> 28u) + 4u)) ^ state) * 277803737u;
    return (word >> 22u) ^ word;
}

fn rand_f32(state: ptr<function, u32>) -> f32 {
    *state = pcg_hash(*state);
    return f32(*state) / 4294967295.0;
}

fn quat_rotate(q: vec4<f32>, v: vec3<f32>) -> vec3<f32> {
    let t = 2.0 * cross(q.xyz, v);
    return v + q.w * t + cross(q.xyz, t);
}

// SIMULATE: Update existing particles
@compute @workgroup_size(64)
fn simulate(@builtin(global_invocation_id) id: vec3<u32>) {
    let idx = id.x;
    if (idx >= params.max_particles) { return; }

    var p = particles[idx];
    if (p.life < p.max_life) {
        // Integrate using particle's own gravity/drag
        let gravity = p.gravity; 
        let drag = max(0.0, 1.0 - p.drag * params.dt);
        
        p.velocity.y -= gravity * params.dt;
        p.velocity *= drag;
        p.pos += p.velocity * params.dt;
        p.life += params.dt;
        
        if (p.life >= p.max_life) {
            p.life = p.max_life;
            let dead_idx = atomicAdd(&counters.dead_count, 1u);
            dead_pool[dead_idx] = idx;
        } else {
            let alive_idx = atomicAdd(&counters.alive_count, 1u);
            alive_list[alive_idx] = idx;
        }
        particles[idx] = p;
    }
}

// SPAWN: Initialize new particles from dead pool
@compute @workgroup_size(64)
fn spawn(@builtin(global_invocation_id) id: vec3<u32>) {
    let request_idx = id.x;
    let spawn_total = atomicLoad(&counters.spawn_request_count);
    if (request_idx >= spawn_total) { return; }

    // Try to get a dead particle index
    // Using atomicAdd with -1 (0xFFFFFFFF)
    let dead_pool_top = atomicAdd(&counters.dead_count, 0xFFFFFFFFu);
    if (dead_pool_top == 0u) {
        // No dead particles available, restore count and bail
        atomicAdd(&counters.dead_count, 1u);
        return;
    }
    let p_idx = dead_pool[dead_pool_top - 1u];

    // Initialize particle
    let emitter_idx = spawn_requests[request_idx].emitter_idx;
    let em = emitters[emitter_idx];
    
    var state = params.seed ^ pcg_hash(request_idx);
    
    var p: Particle;
    p.pos = em.pos;
    
    // Cone sampling
    let axis = vec3<f32>(0.0, 1.0, 0.0);
    let theta_max = em.cone_angle * 0.0174533; // deg to rad
    let u = rand_f32(&state);
    let v = rand_f32(&state);
    let cos_theta = mix(cos(theta_max), 1.0, u);
    let sin_theta = sqrt(1.0 - cos_theta * cos_theta);
    let phi = 6.283185 * v;
    let local_dir = vec3<f32>(cos(phi) * sin_theta, cos_theta, sin(phi) * sin_theta);
    let world_dir = quat_rotate(em.rot, local_dir);
    
    let speed = mix(em.speed_min, em.speed_max, rand_f32(&state));
    p.velocity = world_dir * speed;
    
    p.life = 0.0;
    p.max_life = mix(em.life_min, em.life_max, rand_f32(&state));
    p.size = mix(em.size_min, em.size_max, rand_f32(&state));
    p.color = mix(em.color_min, em.color_max, rand_f32(&state));
    p.gravity = em.gravity;
    p.drag = em.drag;
    
    particles[p_idx] = p;
    let alive_idx = atomicAdd(&counters.alive_count, 1u);
    alive_list[alive_idx] = p_idx;
}

@compute @workgroup_size(1)
fn init_draw_args() {
    atomicStore(&counters.alive_count, 0u);
}

@compute @workgroup_size(1)
fn finalize_draw_args() {
    atomicStore(&draw_args.instance_count, atomicLoad(&counters.alive_count));
}
