// voxelrt/shaders/debug.wgsl

// ============== STRUCTS ==============

struct CameraData {
    view_proj: mat4x4<f32>,
    inv_view: mat4x4<f32>,
    inv_proj: mat4x4<f32>,
    cam_pos: vec4<f32>,
    light_pos: vec4<f32>,
    ambient_color: vec4<f32>,
    debug_mode: u32,
    pad0: u32,
    pad1: u32,
    pad2: u32,
};

struct Instance {
    object_to_world: mat4x4<f32>,
    world_to_object: mat4x4<f32>,
    aabb_min: vec4<f32>,
    aabb_max: vec4<f32>,
    local_aabb_min: vec4<f32>,
    local_aabb_max: vec4<f32>,
    object_id: u32,
    padding: array<u32, 3>,
};

struct BVHNode {
    aabb_min: vec4<f32>,
    aabb_max: vec4<f32>,
    left: i32,
    right: i32,
    leaf_first: i32,
    leaf_count: i32,
    padding: vec4<i32>,
};

struct Ray {
    origin: vec3<f32>,
    dir: vec3<f32>,
    inv_dir: vec3<f32>,
};

// ============== BIND GROUPS ==============

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<storage, read> instances: array<Instance>;
@group(0) @binding(2) var<storage, read> nodes: array<BVHNode>;

@group(1) @binding(0) var out_tex: texture_storage_2d<rgba8unorm, write>;

// ============== HELPERS ==============

fn intersect_aabb(ray: Ray, min_b: vec3<f32>, max_b: vec3<f32>) -> vec2<f32> {
    let t0s = (min_b - ray.origin) * ray.inv_dir;
    let t1s = (max_b - ray.origin) * ray.inv_dir;
    let tsmaller = min(t0s, t1s);
    let tbigger = max(t0s, t1s);
    let tmin = max(tsmaller.x, max(tsmaller.y, tsmaller.z));
    let tmax = min(tbigger.x, min(tbigger.y, tbigger.z));
    return vec2<f32>(tmin, tmax);
}

fn get_ray(uv: vec2<f32>) -> Ray {
    let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    let clip = vec4<f32>(ndc, 1.0, 1.0);
    var view = camera.inv_proj * clip;
    view = view / view.w;
    let world_target = (camera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
    let origin = camera.cam_pos.xyz;
    let dir = normalize(world_target - origin);
    return Ray(origin, dir, 1.0 / dir);
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
    let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
    let new_dir = normalize((mat * vec4<f32>(ray.dir, 0.0)).xyz);
    return Ray(new_origin, new_dir, 1.0 / new_dir);
}

// ============== MAIN ==============

@compute @workgroup_size(8, 8, 1)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let dims = textureDimensions(out_tex);
    if (global_id.x >= dims.x || global_id.y >= dims.y) {
        return;
    }

    let uv = vec2<f32>(f32(global_id.x) / f32(dims.x), f32(global_id.y) / f32(dims.y));
    let ray = get_ray(uv);

    // Initial color should stay unchanged if we don't hit any debug lines.
    // However, write-only storage textures don't allow reading.
    // In a separate pass without blending, we can't easily avoid overwriting.
    // UNLESS we only write if we hit something? NO, even then it writes to that pixel.
    
    // To solve this in compute without read_write:
    // This shader MUST be responsible for both or we use a separate composite pass.
    // BUT the user asked for a separate pass.
    
    // If we want a separate pass to overlay, we should use a Graphics Pass with Blending.
    // Or we use a temporary texture?
    
    // Let's assume for now we want to keep it simple and just move the code.
    // If I move the code but can't read the background, the background will be lost.
    
    // Let's reconsider. If I move it to a graphics pass that draws lines, it's perfect.
    // But then I need to feed it bvh nodes as vertex data? bit overkill for now.
    
    // Let's try to see if Cogentcore webgpu supports read_write.
    // Or I just use a more advanced shader path.
    
    // Actually, I can just use a separate debug shader that IS the core raytrace but with debug logic?
    // No, that's not what was asked.
    
    // Let's try to use a graphics pass for debug overlay.
    // It will render over the quad.
    
    // Wait, let's look at `raytrace.wgsl` again. It writes the sky/hit color at the end.
    // If I move the debug logic out, I can't overlay it in compute easily without read access.
    
    // I will propose using a graphics pass for the debug drawing.
    // This involves:
    // 1. A simple vertex shader that takes BVH nodes/AABBs as instances.
    // 2. A fragment shader that outputs the color.
    // This is MUCH more efficient too.
    
    // But I'll need to prepare the lines.
    
    // Easiest "Separate Path" in compute:
    // Use TWO textures. `raytrace` -> `texA`. `debug` -> `texB` (reading `texA`).
    // Then `fullscreen` shows `texB`.
    
    // Let's do that.
    
    var debug_color = vec4<f32>(0.0);
    var hit_debug = false;

    var stack_debug: array<i32, 64>;
    var sp_debug = 0;
    stack_debug[sp_debug] = 0;
    sp_debug = sp_debug + 1;

    while (sp_debug > 0) {
        sp_debug = sp_debug - 1;
        let node_idx = stack_debug[sp_debug];
        let node = nodes[node_idx];

        let t_vals = intersect_aabb(ray, node.aabb_min.xyz, node.aabb_max.xyz);
        if (t_vals.x <= t_vals.y && t_vals.y > 0.0) {
            var is_leaf = node.leaf_count > 0;
            var node_color = vec4<f32>(1.0, 0.6, 0.0, 1.0); // Orange-Yellow for internal
            if (is_leaf) {
                node_color = vec4<f32>(0.0, 1.0, 0.0, 1.0); // Green for leaf
            }

            let hit_p = ray.origin + ray.dir * t_vals.x;
            let edge_dist = abs(hit_p - node.aabb_min.xyz);
            let edge_dist2 = abs(hit_p - node.aabb_max.xyz);
            let edge_min = min(edge_dist, edge_dist2);
            
            var edge_count = 0;
            let thickness: f32 = 0.05 * (1.0 + t_vals.x * 0.05);
            if (edge_min.x < thickness) { edge_count = edge_count + 1; }
            if (edge_min.y < thickness) { edge_count = edge_count + 1; }
            if (edge_min.z < thickness) { edge_count = edge_count + 1; }
            
            if (edge_count >= 2) {
                debug_color = node_color;
                hit_debug = true;
            }

            if (is_leaf) {
                let inst = instances[node.leaf_first];
                let ray_ws_obj = transform_ray(ray, inst.world_to_object);
                
                // Cyan Inst
                let t_inst = intersect_aabb(ray, inst.aabb_min.xyz, inst.aabb_max.xyz);
                if (t_inst.x <= t_inst.y && t_inst.y > 0.0) {
                    let hit_p_inst = ray.origin + ray.dir * t_inst.x;
                    let ed_i = abs(hit_p_inst - inst.aabb_min.xyz);
                    let ed2_i = abs(hit_p_inst - inst.aabb_max.xyz);
                    let em_i = min(ed_i, ed2_i);
                    var ec_i = 0;
                    if (em_i.x < 0.03) { ec_i = ec_i + 1; }
                    if (em_i.y < 0.03) { ec_i = ec_i + 1; }
                    if (em_i.z < 0.03) { ec_i = ec_i + 1; }
                    if (ec_i >= 2) {
                        debug_color = vec4<f32>(0.0, 0.8, 1.0, 1.0);
                        hit_debug = true;
                    }
                }

                // OBB Magenta
                let t_obb = intersect_aabb(ray_ws_obj, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
                if (t_obb.x <= t_obb.y && t_obb.y > 0.0) {
                    let hit_p_os = ray_ws_obj.origin + ray_ws_obj.dir * t_obb.x;
                    let ed_o = abs(hit_p_os - inst.local_aabb_min.xyz);
                    let ed2_o = abs(hit_p_os - inst.local_aabb_max.xyz);
                    let em_o = min(ed_o, ed2_o);
                    var ec_o = 0;
                    if (em_o.x < 0.05) { ec_o = ec_o + 1; }
                    if (em_o.y < 0.05) { ec_o = ec_o + 1; }
                    if (em_o.z < 0.05) { ec_o = ec_o + 1; }
                    if (ec_o >= 2) {
                        debug_color = vec4<f32>(1.0, 0.0, 1.0, 1.0);
                        hit_debug = true;
                    }
                }
            } else {
                if (node.left != -1 && sp_debug < 64) { 
                    stack_debug[sp_debug] = node.left; 
                    sp_debug = sp_debug + 1; 
                }
                if (node.right != -1 && sp_debug < 64) { 
                    stack_debug[sp_debug] = node.right; 
                    sp_debug = sp_debug + 1; 
                }
            }
        }
    }

    if (hit_debug) {
        textureStore(out_tex, global_id.xy, debug_color);
    }
}
