// voxelrt/shaders/transparent_overlay.wgsl
// Single-layer transparency overlay: raycast first transparent hit before opaque depth,
// output color with alpha and let hardware blending composite over the lit image.

// ============== CONSTANTS ==============
const SECTOR_SIZE: f32 = 32.0;
const BRICK_SIZE: f32 = 8.0;
const EPS: f32 = 1e-3;
const FAR_T: f32 = 60000.0;
const PI: f32 = 3.14159265359;

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
  screen_size: vec2<f32>,
  pad2: vec2<f32>,
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
  shadow_group_id: u32,
  shadow_seam_epsilon: f32,
  is_terrain_chunk: u32,
  terrain_group_id: u32,
  terrain_chunk: vec4<i32>,
};

struct SectorGridEntry {
  coords: vec4<i32>, // sx, sy, sz, 0
  base_idx: u32,
  sector_idx: i32,
  padding: vec2<u32>,
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
@group(2) @binding(2) var in_shadow_maps : texture_2d_array<f32>;

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

fn blocky_normal_os(voxel_center_os: vec3<f32>, aabb_center_os: vec3<f32>, params: ObjectParams) -> vec3<f32> {
  let vi = vec3<i32>(floor(voxel_center_os));
  let dx = sample_occupancy(vi + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
  let dy = sample_occupancy(vi + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
  let dz = sample_occupancy(vi + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi + vec3<i32>(0, 0, -1), params);
  let grad = vec3<f32>(dx, dy, dz);
  let ax = abs(grad.x);
  let ay = abs(grad.y);
  let az = abs(grad.z);
  if (max(ax, max(ay, az)) > 0.05) {
    if (ax >= ay && ax >= az) { return vec3<f32>(-select(1.0, -1.0, grad.x < 0.0), 0.0, 0.0); }
    if (ay >= ax && ay >= az) { return vec3<f32>(0.0, -select(1.0, -1.0, grad.y < 0.0), 0.0); }
    return vec3<f32>(0.0, 0.0, -select(1.0, -1.0, grad.z < 0.0));
  }

  let dir_c = voxel_center_os - aabb_center_os;
  let adx = abs(dir_c.x);
  let ady = abs(dir_c.y);
  let adz = abs(dir_c.z);
  if (adx >= ady && adx >= adz) { return vec3<f32>(select(1.0, -1.0, dir_c.x < 0.0), 0.0, 0.0); }
  if (ady >= adx && ady >= adz) { return vec3<f32>(0.0, select(1.0, -1.0, dir_c.y < 0.0), 0.0); }
  return vec3<f32>(0.0, 0.0, select(1.0, -1.0, dir_c.z < 0.0));
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

// ============== Traversal (WBOIT accumulation) ==============

const MIN_ROUGHNESS: f32 = 0.045;
const PBR_EPSILON: f32 = 1e-4;

fn saturate(v: f32) -> f32 {
  return clamp(v, 0.0, 1.0);
}

fn srgb_channel_to_linear(v: f32) -> f32 {
  if (v <= 0.04045) {
    return v / 12.92;
  }
  return pow((v + 0.055) / 1.055, 2.4);
}

fn srgb_to_linear(c: vec3<f32>) -> vec3<f32> {
  return vec3<f32>(
    srgb_channel_to_linear(c.x),
    srgb_channel_to_linear(c.y),
    srgb_channel_to_linear(c.z),
  );
}

fn dielectric_f0_from_ior(ior_input: f32) -> vec3<f32> {
  var ior = ior_input;
  if (ior <= 1.01) {
    ior = 1.5;
  }
  let reflectance = (ior - 1.0) / (ior + 1.0);
  return vec3<f32>(reflectance * reflectance);
}

fn fresnel_schlick(cos_theta: f32, F0: vec3<f32>) -> vec3<f32> {
  return F0 + (1.0 - F0) * pow(saturate(1.0 - cos_theta), 5.0);
}

fn fresnel_schlick_roughness(cos_theta: f32, F0: vec3<f32>, roughness: f32) -> vec3<f32> {
  return F0 + (max(vec3<f32>(1.0 - roughness), F0) - F0) * pow(saturate(1.0 - cos_theta), 5.0);
}

fn distribution_ggx(NdotH: f32, roughness: f32) -> f32 {
  let a = max(roughness, MIN_ROUGHNESS);
  let alpha = a * a;
  let alpha2 = alpha * alpha;
  let denom = NdotH * NdotH * (alpha2 - 1.0) + 1.0;
  return alpha2 / max(PI * denom * denom, PBR_EPSILON);
}

fn geometry_schlick_ggx(NdotX: f32, roughness: f32) -> f32 {
  let r = roughness + 1.0;
  let k = (r * r) * 0.125;
  return NdotX / max(NdotX * (1.0 - k) + k, PBR_EPSILON);
}

fn geometry_smith(NdotV: f32, NdotL: f32, roughness: f32) -> f32 {
  return geometry_schlick_ggx(NdotV, roughness) * geometry_schlick_ggx(NdotL, roughness);
}

fn calculate_lighting(
  p: vec3<f32>,
  n: vec3<f32>,
  base_color: vec3<f32>,
  roughness: f32,
  metalness: f32,
  ior: f32,
  emissive: vec3<f32>,
  receiver_shadow_group_id: u32,
  receiver_shadow_seam_epsilon: f32
) -> vec3<f32> {
  let V = normalize(uCamera.cam_pos.xyz - p);
  let NdotV = max(dot(n, V), 0.0);
  let dielectric_f0 = dielectric_f0_from_ior(ior);
  let F0 = mix(dielectric_f0, base_color, metalness);
  let ambient_fresnel = fresnel_schlick_roughness(NdotV, F0, roughness);
  let ambient_kd = (vec3<f32>(1.0) - ambient_fresnel) * (1.0 - metalness);
  var total_light = (ambient_kd * base_color + ambient_fresnel) * uCamera.ambient_color.xyz + emissive;
  
  for (var i = 0u; i < uCamera.num_lights; i++) {
    let light = lights[i];
    let light_type = u32(light.params.z);
    var L = vec3<f32>(0.0);
    var attenuation = 1.0;
    
    if (light_type == 1u) {
      L = normalize(-light.direction.xyz);
    } else {
      let dist_vec = light.position.xyz - p;
      let dist = length(dist_vec);
      let range = light.params.x;
      L = normalize(dist_vec);
      if (dist > range) {
        attenuation = 0.0;
      } else {
        let dist_sq = dist * dist;
        let factor = dist / range;
        let smooth_factor = max(0.0, 1.0 - factor * factor);
        let inv_sq = 1.0 / (dist_sq + 1.0);
        attenuation = inv_sq * smooth_factor * smooth_factor * 50.0;
        if (light_type == 2u) {
          let spot_dir = normalize(light.direction.xyz);
          let cos_cur = dot(-L, spot_dir);
          let cos_cone = light.params.y;
          if (cos_cur < cos_cone) {
            attenuation = 0.0;
          } else {
            let spot_att = smoothstep(cos_cone, cos_cone + 0.1, cos_cur);
            attenuation *= spot_att;
          }
        }
      }
    }

    if (attenuation <= 0.0) {
      continue;
    }

    if (light_type != 0u) {
      var pos_ws = p;
      if (light_type == 1u) {
        let receiver_offset = 0.25;
        pos_ws = p + n * receiver_offset;
      }
      let pos_ls = light.view_proj * vec4<f32>(pos_ws, 1.0);
      let proj_pos = pos_ls.xyz / pos_ls.w;
      let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);

      if (pos_ls.w > 0.0 && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0 && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0) {
        let tex_dim = textureDimensions(in_shadow_maps);
        let base_px_f = shadow_uv * vec2<f32>(f32(tex_dim.x), f32(tex_dim.y));
        let base_px = vec2<i32>(
          i32(clamp(base_px_f.x, 0.0, f32(tex_dim.x - 1u))),
          i32(clamp(base_px_f.y, 0.0, f32(tex_dim.y - 1u)))
        );
        let layer = i32(i);

        var my_depth_n = clamp(proj_pos.z, -1.0, 1.0);
        var my_depth_m = 0.0;
        if (light_type == 2u) {
          let receiver_offset = 0.25;
          let pos_off = p + n * receiver_offset;
          my_depth_m = distance(light.position.xyz, pos_off);
        }

        var baseBias = 1.5 / f32(tex_dim.x);
        var slopeBias = 0.002;
        if (light_type == 2u) {
          baseBias = 3.0 / f32(tex_dim.x);
          slopeBias = 0.01;
        }
        let NdL_shadow = max(dot(n, L), 0.0);
        let bias = baseBias + slopeBias * (1.0 - NdL_shadow);

        let max_px = vec2<i32>(i32(tex_dim.x) - 1, i32(tex_dim.y) - 1);
        var visibility = 0.0;
        var radius: i32 = 1;
        if (light_type == 2u) { radius = 2; }
        let kernel = radius * 2 + 1;
        let sample_count = f32(kernel * kernel);
        for (var dy: i32 = -radius; dy <= radius; dy = dy + 1) {
          for (var dx: i32 = -radius; dx <= radius; dx = dx + 1) {
            let off = base_px + vec2<i32>(dx, dy);
            let clamped_off = clamp(off, vec2<i32>(0, 0), max_px);
            let shadow_sample = textureLoad(in_shadow_maps, clamped_off, layer, 0);
            let sampled_depth = shadow_sample.r;
            let sampled_shadow_group_id = u32(shadow_sample.g + 0.5);
            let same_shadow_group =
              receiver_shadow_group_id != 0u &&
              sampled_shadow_group_id == receiver_shadow_group_id;
            if (light_type == 2u) {
              let baseBiasM = 0.05;
              let slopeBiasM = 0.1;
              let biasM = baseBiasM + slopeBiasM * (1.0 - NdL_shadow);
              let receiver_minus_occluder = my_depth_m - sampled_depth;
              let seam_lit = same_shadow_group && receiver_minus_occluder <= receiver_shadow_seam_epsilon;
              visibility += select(0.0, 1.0, seam_lit || sampled_depth >= my_depth_m - biasM);
            } else {
              let sampled_depth_n = clamp(sampled_depth, -1.0, 1.0);
              let seam_pos_ls = light.view_proj * vec4<f32>(pos_ws - L * receiver_shadow_seam_epsilon, 1.0);
              let seam_depth_n = clamp(seam_pos_ls.z / seam_pos_ls.w, -1.0, 1.0);
              let seam_epsilon_n = abs(seam_depth_n - my_depth_n);
              let receiver_minus_occluder = my_depth_n - sampled_depth_n;
              let seam_lit = same_shadow_group && receiver_minus_occluder <= seam_epsilon_n;
              visibility += select(0.0, 1.0, seam_lit || sampled_depth_n >= my_depth_n - bias);
            }
          }
        }
        attenuation *= visibility / sample_count;
      }
    }
    
    if (attenuation <= 0.0) {
      continue;
    }

    let NdotL = max(dot(n, L), 0.0);
    if (NdotL <= 0.0 || NdotV <= 0.0) {
      continue;
    }

    let H = normalize(V + L);
    let NdotH = max(dot(n, H), 0.0);
    let HdotV = max(dot(H, V), 0.0);
    let rough = max(roughness, MIN_ROUGHNESS);
    let fresnel = fresnel_schlick(HdotV, F0);
    let D = distribution_ggx(NdotH, rough);
    let G = geometry_smith(NdotV, NdotL, rough);
    let specular = (D * G * fresnel) / max(4.0 * NdotV * NdotL, PBR_EPSILON);
    let kS = fresnel;
    let kD = (vec3<f32>(1.0) - kS) * (1.0 - metalness);
    let diffuse = kD * base_color / PI;
    let radiance = light.color.xyz * attenuation * light.color.w;

    total_light += (diffuse + specular) * radiance * NdotL;
  }
  
  return total_light;
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
            let density_sigma: f32 = 1.0;
            let k: f32 = 8.0;

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
                        let base_col = srgb_to_linear(materials[mat_idx].xyz);
                        let emissive = srgb_to_linear(materials[mat_idx + 1u].xyz) * max(materials[mat_idx + 3u].x, 0.0);
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
                              let p_hit_os = ray_os.origin + dir * (t_micro + dt * 0.5);
                              let voxel_center_os = floor(p_hit_os) + 0.5;
                              let aabb_center_os = (inst.local_aabb_min.xyz + inst.local_aabb_max.xyz) * 0.5;
                              let n_os = blocky_normal_os(voxel_center_os, aabb_center_os, params);
                              let n_ws = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                              let pos_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                              let z = clamp(t_micro / max(t_limit, 1e-4), 0.0, 1.0);
                              let color = calculate_lighting(pos_ws, n_ws, base_col, pbr.x, pbr.y, pbr.z, emissive, params.shadow_group_id, params.shadow_seam_epsilon);
                              let w = max(1e-3, alpha_step) * pow(1.0 - z, k);
                              accum_rgb += color * alpha_step * w;
                              accum_a += alpha_step;
                              accum_w += alpha_step * w;
                              if (accum_a > 4.0) { break; }
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
                              let base_col = srgb_to_linear(materials[mat_idx].xyz);
                              let emissive = srgb_to_linear(materials[mat_idx + 1u].xyz) * max(materials[mat_idx + 3u].x, 0.0);
                              let pbr = materials[mat_idx + 2u];
                              let trans = clamp(pbr.w, 0.0, 1.0);
                              if (trans > 0.001) {
                                let t_next = min(t_max_micro.x, min(t_max_micro.y, t_max_micro.z));
                                let dt = max(0.0, t_next - t_micro);
                                if (dt > 0.0) {
                                  let dt_ws = dt * d_ws_scale;
                                  let a0 = clamp(1.0 - trans, 0.0, 1.0);
                                  let alpha_step = 1.0 - exp(-density_sigma * a0 * dt_ws);
                                   let p_hit_os = ray_os.origin + dir * (t_micro + dt * 0.5);
                                   let voxel_center_os = floor(p_hit_os) + 0.5;
                                   let aabb_center_os = (inst.local_aabb_min.xyz + inst.local_aabb_max.xyz) * 0.5;
                                   let n_os = blocky_normal_os(voxel_center_os, aabb_center_os, params);
                                   let n_ws = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                                   let pos_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                   let z = clamp(t_micro / max(t_limit, 1e-4), 0.0, 1.0);
                                   let color = calculate_lighting(pos_ws, n_ws, base_col, pbr.x, pbr.y, pbr.z, emissive, params.shadow_group_id, params.shadow_seam_epsilon);
                                   let w = max(1e-3, alpha_step) * pow(1.0 - z, k);
                                   accum_rgb += color * alpha_step * w;
                                   accum_a += alpha_step;
                                   accum_w += alpha_step * w;
                                   if (accum_a > 4.0) { break; }
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
