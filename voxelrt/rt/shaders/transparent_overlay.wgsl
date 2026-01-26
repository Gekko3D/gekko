// voxelrt/shaders/transparent_overlay.wgsl
// Single-layer transparency overlay: raycast first transparent hit before opaque depth,
// output color with alpha and let hardware blending composite over the lit image.

// ============== CONSTANTS ==============
const SECTOR_SIZE: f32 = 32.0;
const BRICK_SIZE: f32 = 8.0;
const EPS: f32 = 1e-3;
const FAR_T: f32 = 60000.0;

// ============== STRUCTS (match gbuffer/deferred) ==============
struct CameraData {
  view_proj: mat4x4<f32>,
  inv_view: mat4x4<f32>,
  inv_proj: mat4x4<f32>,
  cam_pos: vec4<f32>,
  light_pos: vec4<f32>,
  ambient_color: vec4<f32>,
  debug_mode: u32,
  render_mode: u32,
  num_lights: u32,
  pad1: u32,
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

struct SectorRecord {
  origin_vox: vec4<i32>,
  brick_table_index: u32,
  brick_mask_lo: u32,
  brick_mask_hi: u32,
  padding: u32,
};

struct BrickRecord {
  atlas_offset: u32,
  occupancy_mask_lo: u32,
  occupancy_mask_hi: u32,
  flags: u32,
};

struct Tree64Node {
  mask_lo: u32,
  mask_hi: u32,
  child_ptr: u32,
  data: u32,
};

struct Light {
  position: vec4<f32>,
  direction: vec4<f32>,
  color: vec4<f32>,
  params: vec4<f32>, // x: range, y: cos_cone, z: type, w: pad
  view_proj: mat4x4<f32>,
  inv_view_proj: mat4x4<f32>,
};

struct ObjectParams {
  sector_table_base: u32,
  brick_table_base: u32,
  payload_base: u32,
  material_table_base: u32,
  tree64_base: u32,
  lod_threshold: f32,
  sector_count: u32,
  padding: u32,
};

struct SectorGridEntry {
  coords: vec3<i32>,
  base_idx: u32,
  sector_idx: i32,
  paddings: array<u32, 3>,
};

struct SectorGridParams {
  grid_size: u32,
  grid_mask: u32,
  padding0: u32,
  padding1: u32,
};

struct Ray {
  origin: vec3<f32>,
  dir: vec3<f32>,
  inv_dir: vec3<f32>,
};

struct TransparentHit {
  hit: bool,
  t: f32,
  color: vec3<f32>,
  alpha: f32,
  thickness: f32,
  normal: vec3<f32>,
  pos_ws: vec3<f32>,
  palette_idx: u32,
  material_base: u32,
};

// ============== BIND GROUPS ==============

// Group 0: Scene-level
@group(0) @binding(0) var<uniform> uCamera : CameraData;
@group(0) @binding(1) var<storage, read> instances : array<Instance>;
@group(0) @binding(2) var<storage, read> nodes     : array<BVHNode>;
@group(0) @binding(3) var<storage, read> lights    : array<Light>;

// Group 1: Voxel data and materials
@group(1) @binding(0) var<storage, read> sectors              : array<SectorRecord>;
@group(1) @binding(1) var<storage, read> bricks               : array<BrickRecord>;
@group(1) @binding(2) var voxel_payload: texture_3d<u32>;
@group(1) @binding(3) var<storage, read> materials            : array<vec4<f32>>;
@group(1) @binding(4) var<storage, read> object_params        : array<ObjectParams>;
@group(1) @binding(5) var<storage, read> tree64_nodes         : array<Tree64Node>;
@group(1) @binding(6) var<storage, read> sector_grid          : array<SectorGridEntry>;
@group(1) @binding(7) var<storage, read> sector_grid_params   : SectorGridParams;

// Group 2: GBuffer inputs
@group(2) @binding(0) var in_depth    : texture_2d<f32>;      // stores ray t in .r
@group(2) @binding(1) var in_material : texture_2d<f32>;      // not strictly needed for this pass but reserved

// ============== HELPERS (copied/trimmed from gbuffer) ==============
fn bit_test64(mask_lo: u32, mask_hi: u32, idx: u32) -> bool {
  if (idx < 32u) {
    return (mask_lo & (1u << idx)) != 0u;
  } else {
    return (mask_hi & (1u << (idx - 32u))) != 0u;
  }
}

fn popcnt64_lower(mask_lo: u32, mask_hi: u32, idx: u32) -> u32 {
  if (idx == 0u) { return 0u; }
  if (idx < 32u) {
    let mask = (1u << idx) - 1u;
    return countOneBits(mask_lo & mask);
  } else if (idx == 32u) {
    return countOneBits(mask_lo);
  } else {
    let hi_mask = (1u << (idx - 32u)) - 1u;
    return countOneBits(mask_lo) + countOneBits(mask_hi & hi_mask);
  }
}

fn intersect_aabb(ray: Ray, min_b: vec3<f32>, max_b: vec3<f32>) -> vec2<f32> {
  let t0s = (min_b - ray.origin) * ray.inv_dir;
  let t1s = (max_b - ray.origin) * ray.inv_dir;
  let tsmaller = min(t0s, t1s);
  let tbigger = max(t0s, t1s);
  let tmin = max(tsmaller.x, max(tsmaller.y, tsmaller.z));
  let tmax = min(tbigger.x, min(tbigger.y, tbigger.z));
  return vec2<f32>(tmin, tmax);
}

fn make_safe_dir(d: vec3<f32>) -> vec3<f32> {
  let eps = 1e-6;
  let sx = select(d.x, (select(1.0, -1.0, d.x < 0.0)) * eps, abs(d.x) < eps);
  let sy = select(d.y, (select(1.0, -1.0, d.y < 0.0)) * eps, abs(d.y) < eps);
  let sz = select(d.z, (select(1.0, -1.0, d.z < 0.0)) * eps, abs(d.z) < eps);
  return vec3<f32>(sx, sy, sz);
}

fn step_to_next_cell(p: vec3<f32>, dir: vec3<f32>, inv_dir: vec3<f32>, cell_size: f32) -> f32 {
  let cell = floor(p / cell_size);
  let next_bound = select(cell * cell_size, (cell + 1.0) * cell_size, dir > vec3<f32>(0.0));
  let t_to_bound = (next_bound - p) * inv_dir;
  var t_min = FAR_T;
  if (abs(dir.x) > 1e-6 && t_to_bound.x > 0.0) { t_min = min(t_min, t_to_bound.x); }
  if (abs(dir.y) > 1e-6 && t_to_bound.y > 0.0) { t_min = min(t_min, t_to_bound.y); }
  if (abs(dir.z) > 1e-6 && t_to_bound.z > 0.0) { t_min = min(t_min, t_to_bound.z); }
  return t_min + EPS;
}

fn load_u8(packed_offset: u32, voxel_idx: u32) -> u32 {
  let ax = (packed_offset >> 20u) & 0x3FFu;
  let ay = (packed_offset >> 10u) & 0x3FFu;
  let az = packed_offset & 0x3FFu;

  let vx = voxel_idx % 8u;
  let vy = (voxel_idx / 8u) % 8u;
  let vz = voxel_idx / 64u;

  let coords = vec3<u32>(ax + vx, ay + vy, az + vz);
  return textureLoad(voxel_payload, vec3<i32>(coords), 0).r;
}

fn get_ray_from_uv(uv: vec2<f32>) -> Ray {
  let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
  let clip = vec4<f32>(ndc, 1.0, 1.0);
  var view = uCamera.inv_proj * clip; view = view / view.w;
  let world_target = (uCamera.inv_view * vec4<f32>(view.xyz, 1.0)).xyz;
  let origin = uCamera.cam_pos.xyz;
  let dir = normalize(world_target - origin);
  let safe_dir = make_safe_dir(dir);
  return Ray(origin, dir, 1.0 / safe_dir);
}

fn transform_ray(ray: Ray, mat: mat4x4<f32>) -> Ray {
  let new_origin = (mat * vec4<f32>(ray.origin, 1.0)).xyz;
  let new_dir = (mat * vec4<f32>(ray.dir, 0.0)).xyz;
  let safe_dir = make_safe_dir(new_dir);
  return Ray(new_origin, new_dir, 1.0 / safe_dir);
}

// ============== Sector grid lookup cache (same pattern as gbuffer) ==============
var<private> g_cached_sector_id: i32 = -1;
var<private> g_cached_sector_coords: vec3<i32> = vec3<i32>(-999, -999, -999);
var<private> g_cached_sector_base: u32 = 0xFFFFFFFFu;

fn find_sector_cached(sx: i32, sy: i32, sz: i32, params: ObjectParams) -> i32 {
  if (sx == g_cached_sector_coords.x && sy == g_cached_sector_coords.y && sz == g_cached_sector_coords.z &&
      params.sector_table_base == g_cached_sector_base && g_cached_sector_id != -1) {
    return g_cached_sector_id;
  }
  // Linear-probe hash grid as in gbuffer
  let size = sector_grid_params.grid_size;
  if (size == 0u) { return -1; }
  let h = (u32(sx) * 73856093u ^ u32(sy) * 19349663u ^ u32(sz) * 83492791u ^ params.sector_table_base * 99999989u) % size;
  for (var i = 0u; i < 128u; i++) {
    let idx = (h + i) % size;
    let entry = sector_grid[idx];
    if (entry.sector_idx == -1) { break; }
    if (entry.coords.x == sx && entry.coords.y == sy && entry.coords.z == sz && entry.base_idx == params.sector_table_base) {
      g_cached_sector_id = entry.sector_idx;
      g_cached_sector_coords = vec3<i32>(sx, sy, sz);
      g_cached_sector_base = params.sector_table_base;
      return entry.sector_idx;
    }
  }
  g_cached_sector_id = -1;
  return -1;
}

// Return 1 if occupied, 0 otherwise
fn sample_occupancy(v: vec3<i32>, params: ObjectParams) -> f32 {
  let sx = v.x >> 5u;
  let sy = v.y >> 5u;
  let sz = v.z >> 5u;
  let sector_idx = find_sector_cached(sx, sy, sz, params);
  if (sector_idx < 0) { return 0.0; }
  let sector = sectors[sector_idx];
  let bx = (v.x >> 3u) & 3;
  let by = (v.y >> 3u) & 3;
  let bz = (v.z >> 3u) & 3;
  let bvid = vec3<u32>(u32(bx), u32(by), u32(bz));
  let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;
  let packed_idx = sector.brick_table_index + brick_idx_local;
  let b_flags = bricks[packed_idx].flags;
  if (b_flags == 0u) {
    let vx = v.x & 7;
    let vy = v.y & 7;
    let vz = v.z & 7;
    let vvid = vec3<u32>(u32(vx), u32(vy), u32(vz));
    let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
    let b_atlas = bricks[packed_idx].atlas_offset;
    let palette_idx = load_u8(b_atlas, voxel_idx);
    return select(0.0, 1.0, palette_idx != 0u);
  }
  return 1.0;
}

// Estimate normal via occupancy gradient
fn estimate_normal(p: vec3<f32>, params: ObjectParams) -> vec3<f32> {
  let vi = vec3<i32>(floor(p));
  let dx = sample_occupancy(vi + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
  let dy = sample_occupancy(vi + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
  let dz = sample_occupancy(vi + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi + vec3<i32>(0, 0, -1), params);
  let grad = vec3<f32>(dx, dy, dz);
  if (length(grad) < 0.01) { return vec3<f32>(0.0); }
  return -normalize(grad);
}

// Estimate thickness in WORLD SPACE along the ray inside occupied voxels, starting just after the hit.
// Marches per-voxel using DDA until leaving occupancy or reaching t_max_obj.
// World-space length is accumulated using the mapped direction magnitude.
fn estimate_thickness_ws(start_t: f32, ray: Ray, t_max_obj: f32, params: ObjectParams, obj_to_world: mat4x4<f32>) -> f32 {
  var t = start_t + EPS;
  var pos = ray.origin + ray.dir * t;
  var thickness_ws: f32 = 0.0;
  let dir_ws = (obj_to_world * vec4<f32>(ray.dir, 0.0)).xyz;
  let d_ws_scale = length(dir_ws);
  var steps: i32 = 0;
  loop {
    if (t >= t_max_obj || steps >= 256) { break; }
    let occ = sample_occupancy(vec3<i32>(floor(pos)), params);
    if (occ < 0.5) { break; }
    let dt = step_to_next_cell(pos, ray.dir, ray.inv_dir, 1.0);
    thickness_ws += dt * d_ws_scale;
    t += dt;
    pos = pos + ray.dir * dt;
    steps += 1;
  }
  return max(thickness_ws, 0.0);
}

// ============== Traversal for first transparent hit (micro grid path only) ==============
fn first_transparent_in_instance(ray_ws: Ray, inst: Instance, t_enter: f32, t_exit: f32, t_limit: f32) -> TransparentHit {
  var outHit = TransparentHit(false, FAR_T, vec3<f32>(0.0), 0.0, 0.0, vec3<f32>(0.0), vec3<f32>(0.0), 0u, 0u);
  let params = object_params[inst.object_id];

  // Transform into object space
  let ray = transform_ray(ray_ws, inst.world_to_object);
  let t_obj = intersect_aabb(ray, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
  var t_curr = max(t_obj.x, 0.0) + EPS;
  let t_max_obj = min(t_obj.y, t_limit);
  if (t_curr >= t_max_obj) { return outHit; }

  // Sector stepping
  let dir = ray.dir;
  let inv_dir = ray.inv_dir;
  let step = vec3<i32>(sign(dir));
  let t_delta_sector = abs(SECTOR_SIZE * inv_dir);
  let sector_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
  var sector_pos = vec3<i32>(floor(((ray.origin + dir * t_curr) - sector_bias) / SECTOR_SIZE));
  var t_max_sector = (vec3<f32>(sector_pos) * SECTOR_SIZE + select(vec3<f32>(0.0), vec3<f32>(SECTOR_SIZE), step > vec3<i32>(0)) - ray.origin) * inv_dir;

  var it_sect = 0;
  while (t_curr < t_max_obj && it_sect < 64) {
    it_sect += 1;
    let sector_idx = find_sector_cached(sector_pos.x, sector_pos.y, sector_pos.z, params);
    let t_sector_exit = min(min(min(t_max_sector.x, t_max_sector.y), t_max_sector.z), t_max_obj);

    if (sector_idx >= 0) {
      let sector = sectors[sector_idx];
      let sector_origin = vec3<f32>(sector.origin_vox.xyz);

      // Brick stepping within sector
      var t_brick = t_curr;
      let brick_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
      var brick_pos = vec3<i32>(floor((((ray.origin + dir * t_brick) - sector_origin) - brick_bias) / BRICK_SIZE));
      brick_pos = clamp(brick_pos, vec3<i32>(0), vec3<i32>(3));
      var t_max_brick = (sector_origin + vec3<f32>(brick_pos) * BRICK_SIZE + select(vec3<f32>(0.0), vec3<f32>(BRICK_SIZE), step > vec3<i32>(0)) - ray.origin) * inv_dir;
      let t_delta_brick = abs(BRICK_SIZE * inv_dir);

      var it_brick = 0;
      while (t_brick < t_sector_exit && it_brick < 64) {
        it_brick += 1;
        if (all(brick_pos >= vec3<i32>(0)) && all(brick_pos < vec3<i32>(4))) {
          let bvid = vec3<u32>(u32(brick_pos.x), u32(brick_pos.y), u32(brick_pos.z));
          let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;

          if (bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) {
            let packed_idx = sector.brick_table_index + brick_idx_local;
            let b_flags = bricks[packed_idx].flags;
            let b_atlas = bricks[packed_idx].atlas_offset;
            var t_brick_exit = min(min(min(t_max_brick.x, t_max_brick.y), t_max_brick.z), t_sector_exit);

            if (b_flags == 1u) {
              let palette_idx = b_atlas;
              let mat_base = params.material_table_base;
              let mat_idx = mat_base + palette_idx * 4u;
              let pbr = materials[mat_idx + 2u]; // x=roughness, y=metalness, z=ior, w=transparency
              let alpha = clamp(pbr.w, 0.0, 1.0);
              if (alpha > 0.001) {
                // Transparent solid; compute hit data
                let t_hit = t_brick;
                let p_hit_os = ray.origin + dir * (t_brick + EPS);
                let n_os = estimate_normal(p_hit_os, params);
                let n_ws = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                let pos_ws = (inst.object_to_world * vec4<f32>(p_hit_os, 1.0)).xyz;
                let thick = estimate_thickness_ws(t_hit, ray, t_max_obj, params, inst.object_to_world);
                outHit = TransparentHit(true, t_hit, vec3<f32>(0.0), alpha, thick, n_ws, pos_ws, palette_idx, mat_base);
                return outHit;
              }
            } else {
              // Micro brick: step voxels and check per-voxel palette
              var t_micro = t_brick;
              let brick_origin = sector_origin + vec3<f32>(bvid) * BRICK_SIZE;
              let voxel_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
              var voxel_pos = vec3<i32>(floor(((ray.origin + dir * t_micro) - brick_origin) - voxel_bias));
              voxel_pos = clamp(voxel_pos, vec3<i32>(0), vec3<i32>(7));
              var t_max_micro = (brick_origin + vec3<f32>(voxel_pos) * 1.0 + select(vec3<f32>(0.0), vec3<f32>(1.0), step > vec3<i32>(0)) - ray.origin) * inv_dir;
              let t_delta_1 = abs(1.0 * inv_dir);
              let b_mask_lo = bricks[packed_idx].occupancy_mask_lo;
              let b_mask_hi = bricks[packed_idx].occupancy_mask_hi;

              var it_micro = 0;
              while (t_micro < t_brick_exit && it_micro < 32) {
                it_micro += 1;
                let vvid = vec3<u32>(u32(voxel_pos.x), u32(voxel_pos.y), u32(voxel_pos.z));
                let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
                let mvid = vvid / 2u;
                let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;

                if (bit_test64(b_mask_lo, b_mask_hi, micro_idx)) {
                  let palette_idx = load_u8(b_atlas, voxel_idx);
                  if (palette_idx != 0u) {
                    let mat_base = params.material_table_base;
                    let mat_idx = mat_base + palette_idx * 4u;
                    let pbr = materials[mat_idx + 2u];
                    let alpha = clamp(pbr.w, 0.0, 1.0);
                    if (alpha > 0.001) {
                      let t_hit = t_micro;
                      let voxel_center_os = brick_origin + vec3<f32>(voxel_pos) + 0.5;
                      let n_os = estimate_normal(voxel_center_os, params);
                      let n_ws = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                      let pos_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                      let thick = estimate_thickness_ws(t_hit, ray, t_max_obj, params, inst.object_to_world);
                      outHit = TransparentHit(true, t_hit, vec3<f32>(0.0), alpha, thick, n_ws, pos_ws, palette_idx, mat_base);
                      return outHit;
                    }
                  }
                }

                // Advance to next voxel cell
                if (t_max_micro.x < t_max_micro.y) {
                  if (t_max_micro.x < t_max_micro.z) { voxel_pos.x += step.x; t_micro = t_max_micro.x; t_max_micro.x += t_delta_1.x; }
                  else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                } else {
                  if (t_max_micro.y < t_max_micro.z) { voxel_pos.y += step.y; t_micro = t_max_micro.y; t_max_micro.y += t_delta_1.y; }
                  else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                }
                t_micro += EPS;
                if (t_micro >= t_limit) { break; }
              }
            }
          }
        }

        // Advance brick
        if (t_max_brick.x < t_max_brick.y) {
          if (t_max_brick.x < t_max_brick.z) { brick_pos.x += step.x; t_brick = t_max_brick.x; t_max_brick.x += t_delta_brick.x; }
          else { brick_pos.z += step.z; t_brick = t_max_brick.z; t_max_brick.z += t_delta_brick.z; }
        } else {
          if (t_max_brick.y < t_max_brick.z) { brick_pos.y += step.y; t_brick = t_max_brick.y; t_max_brick.y += t_delta_brick.y; }
          else { brick_pos.z += step.z; t_brick = t_max_brick.z; t_max_brick.z += t_delta_brick.z; }
        }

        if (t_brick >= t_limit) { break; }
      }
    }

    // Advance sector
    if (t_max_sector.x < t_max_sector.y) {
      if (t_max_sector.x < t_max_sector.z) { sector_pos.x += step.x; t_curr = t_max_sector.x; t_max_sector.x += t_delta_sector.x; }
      else { sector_pos.z += step.z; t_curr = t_max_sector.z; t_max_sector.z += t_delta_sector.z; }
    } else {
      if (t_max_sector.y < t_max_sector.z) { sector_pos.y += step.y; t_curr = t_max_sector.y; t_max_sector.y += t_delta_sector.y; }
      else { sector_pos.z += step.z; t_curr = t_max_sector.z; t_max_sector.z += t_delta_sector.z; }
    }
  }

  return outHit;
}

// ============== VS/FS ==============
struct VSOut {
  @builtin(position) position : vec4<f32>,
  @location(0) uv : vec2<f32>,
};

@vertex
fn vs_main(@builtin(vertex_index) vi : u32) -> VSOut {
  var out : VSOut;
  let x = f32((vi << 1u) & 2u);
  let y = f32(vi & 2u);
  out.position = vec4<f32>(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);
  out.uv = vec2<f32>(x, y);
  return out;
}

struct FSOut {
  @location(0) accum: vec4<f32>,  // rgb = color * a * w, a = a * w
  @location(1) weight: f32,       // = a * w
};

@fragment
fn fs_main(@builtin(position) frag_pos: vec4<f32>, @location(0) uv: vec2<f32>) -> FSOut {
  let dims = textureDimensions(in_depth);
  let ipos = vec2<i32>( clamp(i32(frag_pos.x), 0, i32(dims.x) - 1),
                        clamp(i32(frag_pos.y), 0, i32(dims.y) - 1) );
  let t_opaque = textureLoad(in_depth, ipos, 0).r;
  var t_limit = t_opaque;
  if (t_limit >= FAR_T) { t_limit = FAR_T; }

  let uv_screen = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(dims.x), f32(dims.y));
  let ray = get_ray_from_uv(uv_screen);

  var accum_rgb = vec3<f32>(0.0);
  var accum_a = 0.0;
  var accum_w = 0.0;

  var stack: array<i32, 64>;
  var sp = 0;
  let n_nodes = arrayLength(&nodes);
  if (n_nodes > 0u) { stack[sp] = 0; sp += 1; }

  var it = 0;
  while (sp > 0 && it < 128) {
    it += 1;
    sp -= 1;
    let idx = stack[sp];
    if (idx < 0 || u32(idx) >= n_nodes) { continue; }

    let node = nodes[idx];
    let t_vals = intersect_aabb(ray, node.aabb_min.xyz, node.aabb_max.xyz);
    if (t_vals.x <= t_vals.y && t_vals.y > 0.0 && t_vals.x < t_limit) {
      if (node.leaf_count > 0) {
        for (var li = 0; li < node.leaf_count; li = li + 1) {
          let inst = instances[u32(node.leaf_first + li)];
          let t_inst = intersect_aabb(ray, inst.aabb_min.xyz, inst.aabb_max.xyz);
          if (t_inst.x <= t_inst.y && t_inst.y > 0.0 && t_inst.x < t_limit) {
            let params = object_params[inst.object_id];
            let ray_os = transform_ray(ray, inst.world_to_object);
            let t_obj = intersect_aabb(ray_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz);
            var t_curr = max(max(t_obj.x, t_inst.x), 0.0) + EPS;
            let t_max_obj = min(min(t_obj.y, t_inst.y), t_limit);
            if (t_curr >= t_max_obj) { continue; }

            let dir = ray_os.dir;
            let inv_dir = ray_os.inv_dir;
            let step = vec3<i32>(sign(dir));
            let t_delta_sector = abs(SECTOR_SIZE * inv_dir);
            let sector_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
            var sector_pos = vec3<i32>(floor(((ray_os.origin + dir * t_curr) - sector_bias) / SECTOR_SIZE));
            var t_max_sector = (vec3<f32>(sector_pos) * SECTOR_SIZE + select(vec3<f32>(0.0), vec3<f32>(SECTOR_SIZE), step > vec3<i32>(0)) - ray_os.origin) * inv_dir;

            let dir_ws = (inst.object_to_world * vec4<f32>(ray_os.dir, 0.0)).xyz;
            let d_ws_scale = length(dir_ws);
            let density_sigma: f32 = 0.2;
            let k: f32 = 4.0;

            var it_sect = 0;
            while (t_curr < t_max_obj && it_sect < 64) {
              it_sect += 1;
              let sector_idx = find_sector_cached(sector_pos.x, sector_pos.y, sector_pos.z, params);
              let t_sector_exit = min(min(min(t_max_sector.x, t_max_sector.y), t_max_sector.z), t_max_obj);

              if (sector_idx >= 0) {
                let sector = sectors[sector_idx];
                let sector_origin = vec3<f32>(sector.origin_vox.xyz);

                var t_brick = t_curr;
                let brick_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
                var brick_pos = vec3<i32>(floor((((ray_os.origin + dir * t_brick) - sector_origin) - brick_bias) / BRICK_SIZE));
                brick_pos = clamp(brick_pos, vec3<i32>(0), vec3<i32>(3));
                var t_max_brick = (sector_origin + vec3<f32>(brick_pos) * BRICK_SIZE + select(vec3<f32>(0.0), vec3<f32>(BRICK_SIZE), step > vec3<i32>(0)) - ray_os.origin) * inv_dir;
                let t_delta_brick = abs(BRICK_SIZE * inv_dir);

                var it_brick = 0;
                while (t_brick < t_sector_exit && it_brick < 64) {
                  it_brick += 1;
                  if (all(brick_pos >= vec3<i32>(0)) && all(brick_pos < vec3<i32>(4))) {
                    let bvid = vec3<u32>(u32(brick_pos.x), u32(brick_pos.y), u32(brick_pos.z));
                    let brick_idx_local = bvid.x + bvid.y * 4u + bvid.z * 16u;

                    if (bit_test64(sector.brick_mask_lo, sector.brick_mask_hi, brick_idx_local)) {
                      let packed_idx = sector.brick_table_index + brick_idx_local;
                      let b_flags = bricks[packed_idx].flags;
                      let b_atlas = bricks[packed_idx].atlas_offset;
                      var t_brick_exit = min(min(min(t_max_brick.x, t_max_brick.y), t_max_brick.z), t_sector_exit);

                      if (b_flags == 1u) {
                        let palette_idx = b_atlas;
                        let mat_base = params.material_table_base;
                        let mat_idx = mat_base + palette_idx * 4u;
                        let base_col = materials[mat_idx].xyz;
                        let emissive = materials[mat_idx + 1u].xyz;
                        let pbr = materials[mat_idx + 2u];
                        let trans = clamp(pbr.w, 0.0, 1.0);
                        if (trans > 0.001) {
                          var t_micro = t_brick;
                          let brick_origin = sector_origin + vec3<f32>(bvid) * BRICK_SIZE;
                          let voxel_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
                          var voxel_pos = vec3<i32>(floor(((ray_os.origin + dir * t_micro) - brick_origin) - voxel_bias));
                          voxel_pos = clamp(voxel_pos, vec3<i32>(0), vec3<i32>(7));
                          var t_max_micro = (brick_origin + vec3<f32>(voxel_pos) * 1.0 + select(vec3<f32>(0.0), vec3<f32>(1.0), step > vec3<i32>(0)) - ray_os.origin) * inv_dir;
                          let t_delta_1 = abs(1.0 * inv_dir);
                          var it_micro = 0;
                          while (t_micro < t_brick_exit && it_micro < 32) {
                            it_micro += 1;
                            let t_next = min(t_max_micro.x, min(t_max_micro.y, t_max_micro.z));
                            let dt = max(0.0, t_next - t_micro);
                            if (dt > 0.0) {
                              let dt_ws = dt * d_ws_scale;
                              let a0 = clamp(1.0 - trans, 0.0, 1.0);
                              let alpha_step = 1.0 - exp(-density_sigma * a0 * dt_ws);
                              let z = clamp(t_micro / max(t_limit, 1e-4), 0.0, 1.0);
                              let color = base_col * uCamera.ambient_color.xyz + emissive;
                              let w = max(1e-3, alpha_step) * pow(1.0 - z, k);
                              accum_rgb += color * alpha_step * w;
                              accum_a += alpha_step;
                              accum_w += alpha_step * w;
                            }
                            if (t_max_micro.x < t_max_micro.y) {
                              if (t_max_micro.x < t_max_micro.z) { voxel_pos.x += step.x; t_micro = t_max_micro.x; t_max_micro.x += t_delta_1.x; }
                              else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                            } else {
                              if (t_max_micro.y < t_max_micro.z) { voxel_pos.y += step.y; t_micro = t_max_micro.y; t_max_micro.y += t_delta_1.y; }
                              else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                            }
                            t_micro += EPS;
                            if (t_micro >= t_limit) { break; }
                          }
                        }
                      } else {
                        var t_micro = t_brick;
                        let brick_origin = sector_origin + vec3<f32>(bvid) * BRICK_SIZE;
                        let voxel_bias = select(vec3<f32>(0.0), vec3<f32>(EPS), step < vec3<i32>(0));
                        var voxel_pos = vec3<i32>(floor(((ray_os.origin + dir * t_micro) - brick_origin) - voxel_bias));
                        voxel_pos = clamp(voxel_pos, vec3<i32>(0), vec3<i32>(7));
                        var t_max_micro = (brick_origin + vec3<f32>(voxel_pos) * 1.0 + select(vec3<f32>(0.0), vec3<f32>(1.0), step > vec3<i32>(0)) - ray_os.origin) * inv_dir;
                        let t_delta_1 = abs(1.0 * inv_dir);
                        let b_mask_lo = bricks[packed_idx].occupancy_mask_lo;
                        let b_mask_hi = bricks[packed_idx].occupancy_mask_hi;
                        var it_micro = 0;
                        while (t_micro < t_brick_exit && it_micro < 32) {
                          it_micro += 1;
                          let vvid = vec3<u32>(u32(voxel_pos.x), u32(voxel_pos.y), u32(voxel_pos.z));
                          let mvid = vvid / 2u;
                          let micro_idx = mvid.x + mvid.y * 4u + mvid.z * 16u;
                          let process = bit_test64(b_mask_lo, b_mask_hi, micro_idx);
                          if (process) {
                            let voxel_idx = vvid.x + vvid.y * 8u + vvid.z * 64u;
                            let palette_idx = load_u8(b_atlas, voxel_idx);
                            if (palette_idx != 0u) {
                              let mat_base = params.material_table_base;
                              let mat_idx = mat_base + palette_idx * 4u;
                              let base_col = materials[mat_idx].xyz;
                              let emissive = materials[mat_idx + 1u].xyz;
                              let pbr = materials[mat_idx + 2u];
                              let trans = clamp(pbr.w, 0.0, 1.0);
                              if (trans > 0.001) {
                                let t_next = min(t_max_micro.x, min(t_max_micro.y, t_max_micro.z));
                                let dt = max(0.0, t_next - t_micro);
                                if (dt > 0.0) {
                                  let dt_ws = dt * d_ws_scale;
                                  let a0 = clamp(1.0 - trans, 0.0, 1.0);
                                  let alpha_step = 1.0 - exp(-density_sigma * a0 * dt_ws);
                                  let z = clamp(t_micro / max(t_limit, 1e-4), 0.0, 1.0);
                                  let color = base_col * uCamera.ambient_color.xyz + emissive;
                                  let w = max(1e-3, alpha_step) * pow(1.0 - z, k);
                                  accum_rgb += color * alpha_step * w;
                                  accum_a += alpha_step;
                                  accum_w += alpha_step * w;
                                }
                              }
                            }
                          }
                          if (t_max_micro.x < t_max_micro.y) {
                            if (t_max_micro.x < t_max_micro.z) { voxel_pos.x += step.x; t_micro = t_max_micro.x; t_max_micro.x += t_delta_1.x; }
                            else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                          } else {
                            if (t_max_micro.y < t_max_micro.z) { voxel_pos.y += step.y; t_micro = t_max_micro.y; t_max_micro.y += t_delta_1.y; }
                            else { voxel_pos.z += step.z; t_micro = t_max_micro.z; t_max_micro.z += t_delta_1.z; }
                          }
                          t_micro += EPS;
                          if (t_micro >= t_limit) { break; }
                        }
                      }
                    }
                  }

                  if (t_max_brick.x < t_max_brick.y) {
                    if (t_max_brick.x < t_max_brick.z) { brick_pos.x += step.x; t_brick = t_max_brick.x; t_max_brick.x += t_delta_brick.x; }
                    else { brick_pos.z += step.z; t_brick = t_max_brick.z; t_max_brick.z += t_delta_brick.z; }
                  } else {
                    if (t_max_brick.y < t_max_brick.z) { brick_pos.y += step.y; t_brick = t_max_brick.y; t_max_brick.y += t_delta_brick.y; }
                    else { brick_pos.z += step.z; t_brick = t_max_brick.z; t_max_brick.z += t_delta_brick.z; }
                  }

                  if (t_brick >= t_limit) { break; }
                }
              }

              if (t_max_sector.x < t_max_sector.y) {
                if (t_max_sector.x < t_max_sector.z) { sector_pos.x += step.x; t_curr = t_max_sector.x; t_max_sector.x += t_delta_sector.x; }
                else { sector_pos.z += step.z; t_curr = t_max_sector.z; t_max_sector.z += t_delta_sector.z; }
              } else {
                if (t_max_sector.y < t_max_sector.z) { sector_pos.y += step.y; t_curr = t_max_sector.y; t_max_sector.y += t_delta_sector.y; }
                else { sector_pos.z += step.z; t_curr = t_max_sector.z; t_max_sector.z += t_delta_sector.z; }
              }
            }
          }
        }
      } else {
        if (node.left != -1 && sp < 64) { stack[sp] = node.left; sp += 1; }
        if (node.right != -1 && sp < 64) { stack[sp] = node.right; sp += 1; }
      }
    }
  }

  return FSOut(vec4<f32>(accum_rgb, accum_a), accum_w);
}
