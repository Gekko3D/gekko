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
  ao_quality: vec4<f32>, // x: AO sample count, y: AO radius, z: directional shadow softness, w: spot shadow softness
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
  atlas_page: u32,
  flags: u32,
};

struct Tree64Node {
  mask_lo: u32,
  mask_hi: u32,
  child_ptr: u32,
  data: u32,
};

struct DirectionalShadowCascade {
  view_proj: mat4x4<f32>,
  inv_view_proj: mat4x4<f32>,
  params: vec4<f32>, // x: split_far, y: texel_world_size, z: depth_scale_to_ndc, w: reserved
};

struct Light {
  position: vec4<f32>,
  direction: vec4<f32>,
  color: vec4<f32>,
  params: vec4<f32>, // x: range, y: cos_cone, z: type, w: pad
  shadow_meta: vec4<u32>,
  view_proj: mat4x4<f32>,
  inv_view_proj: mat4x4<f32>,
  directional_cascades: array<DirectionalShadowCascade, 2>,
};

struct DirectionalCascadeSelection {
  primary_index: u32,
  secondary_index: u32,
  blend: f32,
};

struct PointShadowLookup {
  face: u32,
  uv: vec2<f32>,
};

struct ShadowLayerParams {
  viewport_scale: vec2<f32>,
  effective_resolution: f32,
  inv_effective_resolution: f32,
};

struct TileLightListParams {
  tile_size: u32,
  tiles_x: u32,
  tiles_y: u32,
  max_lights_per_tile: u32,
  screen_width: u32,
  screen_height: u32,
  num_tiles: u32,
  pad0: u32,
};

struct TileLightHeader {
  offset: u32,
  count: u32,
  overflow: u32,
  pad0: u32,
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
@group(0) @binding(4) var<storage, read> shadow_layer_params : array<ShadowLayerParams>;

// Group 1: Voxel data and materials
@group(1) @binding(0) var<storage, read> sectors              : array<SectorRecord>;
@group(1) @binding(1) var<storage, read> bricks               : array<BrickRecord>;
@group(1) @binding(2) var voxel_payload_0: texture_3d<u32>;
@group(1) @binding(3) var voxel_payload_1: texture_3d<u32>;
@group(1) @binding(4) var voxel_payload_2: texture_3d<u32>;
@group(1) @binding(5) var voxel_payload_3: texture_3d<u32>;
@group(1) @binding(6) var<storage, read> materials            : array<vec4<f32>>;
@group(1) @binding(7) var<storage, read> object_params        : array<ObjectParams>;
@group(1) @binding(8) var<storage, read> tree64_nodes         : array<Tree64Node>;
@group(1) @binding(9) var<storage, read> sector_grid          : array<SectorGridEntry>;
@group(1) @binding(10) var<storage, read> sector_grid_params   : SectorGridParams;

// Group 2: GBuffer inputs
@group(2) @binding(0) var in_depth    : texture_2d<f32>;      // stores ray t in .r
@group(2) @binding(1) var in_material : texture_2d<f32>;      // not strictly needed for this pass but reserved
@group(2) @binding(2) var in_shadow_maps : texture_2d_array<f32>;
@group(2) @binding(3) var in_opaque_lit : texture_2d<f32>;

// Group 3: tiled light lists
@group(3) @binding(0) var<uniform> tile_light_params : TileLightListParams;
@group(3) @binding(1) var<storage, read> tile_light_headers : array<TileLightHeader>;
@group(3) @binding(2) var<storage, read> tile_light_indices : array<u32>;

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

fn load_voxel_payload(page: u32, coords: vec3<u32>) -> u32 {
  switch page {
    case 0u: { return textureLoad(voxel_payload_0, vec3<i32>(coords), 0).r; }
    case 1u: { return textureLoad(voxel_payload_1, vec3<i32>(coords), 0).r; }
    case 2u: { return textureLoad(voxel_payload_2, vec3<i32>(coords), 0).r; }
    default: { return textureLoad(voxel_payload_3, vec3<i32>(coords), 0).r; }
  }
}

fn load_u8(packed_offset: u32, atlas_page: u32, voxel_idx: u32) -> u32 {
  let ax = (packed_offset >> 20u) & 0x3FFu;
  let ay = (packed_offset >> 10u) & 0x3FFu;
  let az = packed_offset & 0x3FFu;

  let vx = voxel_idx % 8u;
  let vy = (voxel_idx / 8u) % 8u;
  let vz = voxel_idx / 64u;

  let coords = vec3<u32>(ax + vx, ay + vy, az + vz);
  return load_voxel_payload(atlas_page, coords);
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

fn clamp_uv01(uv: vec2<f32>) -> vec2<f32> {
  return clamp(uv, vec2<f32>(0.0, 0.0), vec2<f32>(1.0, 1.0));
}

fn world_to_uv(p_ws: vec3<f32>) -> vec2<f32> {
  let clip = uCamera.view_proj * vec4<f32>(p_ws, 1.0);
  let ndc = clip.xy / max(clip.w, 1e-4);
  return vec2<f32>(ndc.x * 0.5 + 0.5, -ndc.y * 0.5 + 0.5);
}

fn sample_opaque_lit(uv: vec2<f32>) -> vec3<f32> {
  let dims = textureDimensions(in_opaque_lit);
  let clamped_uv = clamp_uv01(uv);
  let px = vec2<i32>(
    clamp(i32(clamped_uv.x * f32(dims.x)), 0, i32(dims.x) - 1),
    clamp(i32(clamped_uv.y * f32(dims.y)), 0, i32(dims.y) - 1),
  );
  return textureLoad(in_opaque_lit, px, 0).xyz;
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
    let palette_idx = load_u8(b_atlas, bricks[packed_idx].atlas_page, voxel_idx);
    return select(0.0, 1.0, palette_idx != 0u);
  }
  return 1.0;
}

fn estimate_normal_os(voxel_center_os: vec3<f32>, params: ObjectParams) -> vec3<f32> {
  let vi = vec3<i32>(floor(voxel_center_os));
  let dx = sample_occupancy(vi + vec3<i32>(1, 0, 0), params) - sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
  let dy = sample_occupancy(vi + vec3<i32>(0, 1, 0), params) - sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
  let dz = sample_occupancy(vi + vec3<i32>(0, 0, 1), params) - sample_occupancy(vi + vec3<i32>(0, 0, -1), params);
  let grad = vec3<f32>(dx, dy, dz);
  if (length(grad) < 0.01) { return vec3<f32>(0.0); }
  return -normalize(grad);
}

fn axis_tiebreak_sign_os(voxel_center_os: vec3<f32>, local_aabb_min: vec3<f32>, local_aabb_max: vec3<f32>, axis: u32) -> f32 {
  let dist_to_min = voxel_center_os[axis] - local_aabb_min[axis];
  let dist_to_max = local_aabb_max[axis] - voxel_center_os[axis];
  if (dist_to_min + 1e-4 < dist_to_max) {
    return -1.0;
  }
  return 1.0;
}

fn fallback_exposed_voxel_normal_os(
  voxel_center_os: vec3<f32>,
  local_aabb_min: vec3<f32>,
  local_aabb_max: vec3<f32>,
  params: ObjectParams,
) -> vec3<f32> {
  let vi = vec3<i32>(floor(voxel_center_os));

  let occ_px = sample_occupancy(vi + vec3<i32>(1, 0, 0), params);
  let occ_nx = sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
  let occ_py = sample_occupancy(vi + vec3<i32>(0, 1, 0), params);
  let occ_ny = sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
  let occ_pz = sample_occupancy(vi + vec3<i32>(0, 0, 1), params);
  let occ_nz = sample_occupancy(vi + vec3<i32>(0, 0, -1), params);

  let empty_px = 1.0 - occ_px;
  let empty_nx = 1.0 - occ_nx;
  let empty_py = 1.0 - occ_py;
  let empty_ny = 1.0 - occ_ny;
  let empty_pz = 1.0 - occ_pz;
  let empty_nz = 1.0 - occ_nz;

  let signed_exposure = vec3<f32>(
    empty_px - empty_nx,
    empty_py - empty_ny,
    empty_pz - empty_nz,
  );
  if (length(signed_exposure) >= 0.01) {
    return normalize(signed_exposure);
  }

  let unsigned_exposure = vec3<f32>(
    empty_px + empty_nx,
    empty_py + empty_ny,
    empty_pz + empty_nz,
  );
  var tie_break = vec3<f32>(0.0);
  if (unsigned_exposure.x > 0.01) {
    tie_break.x = axis_tiebreak_sign_os(voxel_center_os, local_aabb_min, local_aabb_max, 0u);
  }
  if (unsigned_exposure.y > 0.01) {
    tie_break.y = axis_tiebreak_sign_os(voxel_center_os, local_aabb_min, local_aabb_max, 1u);
  }
  if (unsigned_exposure.z > 0.01) {
    tie_break.z = axis_tiebreak_sign_os(voxel_center_os, local_aabb_min, local_aabb_max, 2u);
  }
  if (length(tie_break) >= 0.01) {
    return normalize(tie_break);
  }

  return vec3<f32>(0.0);
}

fn has_two_sided_voxel_exposure_os(voxel_center_os: vec3<f32>, params: ObjectParams) -> bool {
  let vi = vec3<i32>(floor(voxel_center_os));
  let empty_px = 1.0 - sample_occupancy(vi + vec3<i32>(1, 0, 0), params);
  let empty_nx = 1.0 - sample_occupancy(vi + vec3<i32>(-1, 0, 0), params);
  let empty_py = 1.0 - sample_occupancy(vi + vec3<i32>(0, 1, 0), params);
  let empty_ny = 1.0 - sample_occupancy(vi + vec3<i32>(0, -1, 0), params);
  let empty_pz = 1.0 - sample_occupancy(vi + vec3<i32>(0, 0, 1), params);
  let empty_nz = 1.0 - sample_occupancy(vi + vec3<i32>(0, 0, -1), params);
  return
    (empty_px > 0.01 && empty_nx > 0.01) ||
    (empty_py > 0.01 && empty_ny > 0.01) ||
    (empty_pz > 0.01 && empty_nz > 0.01);
}

fn fallback_face_normal_os(p_hit_os: vec3<f32>, vi_hit: vec3<i32>, ray_dir_os: vec3<f32>) -> vec3<f32> {
  let p_in_voxel = p_hit_os - (vec3<f32>(vi_hit) + 0.5);
  let abs_p = abs(p_in_voxel);
  var n_os = vec3<f32>(0.0);

  if (abs_p.x >= abs_p.y && abs_p.x >= abs_p.z) {
    var nx = select(1.0, -1.0, p_in_voxel.x < 0.0);
    if (abs(p_in_voxel.x) < 1e-4) {
      nx = -select(1.0, -1.0, ray_dir_os.x < 0.0);
    }
    n_os.x = nx;
  } else if (abs_p.y >= abs_p.x && abs_p.y >= abs_p.z) {
    var ny = select(1.0, -1.0, p_in_voxel.y < 0.0);
    if (abs(p_in_voxel.y) < 1e-4) {
      ny = -select(1.0, -1.0, ray_dir_os.y < 0.0);
    }
    n_os.y = ny;
  } else {
    var nz = select(1.0, -1.0, p_in_voxel.z < 0.0);
    if (abs(p_in_voxel.z) < 1e-4) {
      nz = -select(1.0, -1.0, ray_dir_os.z < 0.0);
    }
    n_os.z = nz;
  }

  return n_os;
}

fn shadow_seam_epsilon_at_hit(voxel_center_os: vec3<f32>, local_aabb_min: vec3<f32>, local_aabb_max: vec3<f32>, seam_epsilon: f32) -> f32 {
  if (seam_epsilon <= 0.0) {
    return 0.0;
  }
  let dist_to_min = abs(voxel_center_os - local_aabb_min);
  let dist_to_max = abs(local_aabb_max - voxel_center_os);
  let near_boundary =
    dist_to_min.x <= seam_epsilon || dist_to_min.y <= seam_epsilon || dist_to_min.z <= seam_epsilon ||
    dist_to_max.x <= seam_epsilon || dist_to_max.y <= seam_epsilon || dist_to_max.z <= seam_epsilon;
  return select(0.0, seam_epsilon, near_boundary);
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

fn directional_ambient_scale(normal: vec3<f32>) -> f32 {
  let upness = saturate(normal.y * 0.5 + 0.5);
  let horizon = 1.0 - abs(normal.y);
  return 0.22 + 0.78 * upness + horizon * 0.12;
}

fn camera_forward_ws() -> vec3<f32> {
  return normalize((uCamera.inv_view * vec4<f32>(0.0, 0.0, -1.0, 0.0)).xyz);
}

fn choose_directional_cascade(light: Light, hit_pos: vec3<f32>) -> DirectionalCascadeSelection {
  let cascade_count = light.shadow_meta.z;
  if (cascade_count <= 1u) {
    return DirectionalCascadeSelection(0u, 0u, 0.0);
  }
  // Cascades are authored as view-depth slices, not spherical shells around the camera.
  let receiver_depth = max(dot(hit_pos - uCamera.cam_pos.xyz, camera_forward_ws()), 0.0);
  let split_depth = light.directional_cascades[0].params.x;
  let transition = max(4.0, max(light.directional_cascades[0].params.y * 24.0, split_depth * 0.12));
  let blend_start = max(0.0, split_depth - transition);
  let blend_end = split_depth + transition;
  if (receiver_depth <= blend_start) {
    return DirectionalCascadeSelection(0u, 0u, 0.0);
  }
  if (receiver_depth >= blend_end) {
    let far_idx = min(1u, cascade_count - 1u);
    return DirectionalCascadeSelection(far_idx, far_idx, 0.0);
  }
  let blend = smoothstep(blend_start, blend_end, receiver_depth);
  return DirectionalCascadeSelection(0u, min(1u, cascade_count - 1u), blend);
}

fn sample_directional_shadow(
  light: Light,
  p: vec3<f32>,
  n: vec3<f32>,
  L: vec3<f32>,
  receiver_shadow_group_id: u32,
  receiver_shadow_seam_epsilon: f32,
  cascade_idx: u32
) -> f32 {
  var cascade_view_proj = light.directional_cascades[0].view_proj;
  var cascade_params = light.directional_cascades[0].params;
  if (cascade_idx != 0u) {
    cascade_view_proj = light.directional_cascades[1].view_proj;
    cascade_params = light.directional_cascades[1].params;
  }
  let receiver_normal_offset_world = max(0.08, 0.50 * cascade_params.y);
  let receiver_light_offset_world = max(0.04, 0.30 * cascade_params.y);
  let compare_bias_world = max(0.08, 0.90 * cascade_params.y);
  let pos_ws = p + n * receiver_normal_offset_world + L * receiver_light_offset_world;
  let directional_compare_bias = compare_bias_world * cascade_params.z;
  let seam_pos_ls = cascade_view_proj * vec4<f32>(pos_ws - L * receiver_shadow_seam_epsilon, 1.0);
  let seam_depth_n = clamp(seam_pos_ls.z / seam_pos_ls.w, -1.0, 1.0);
  let receiver_pos_ls = cascade_view_proj * vec4<f32>(pos_ws, 1.0);
  let receiver_depth_n = clamp(receiver_pos_ls.z / receiver_pos_ls.w, -1.0, 1.0);
  let directional_seam_epsilon_n = abs(seam_depth_n - receiver_depth_n);

  let pos_ls = cascade_view_proj * vec4<f32>(pos_ws, 1.0);
  let proj_pos = pos_ls.xyz / pos_ls.w;
  let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);
  if (!(pos_ls.w > 0.0 && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0 && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0)) {
    return 1.0;
  }

  let layer = light.shadow_meta.x + cascade_idx;
  let layer_params = shadow_layer_params[layer];
  let effective_resolution = max(u32(layer_params.effective_resolution + 0.5), 1u);
  let base_px_f = shadow_uv * vec2<f32>(f32(effective_resolution), f32(effective_resolution));
  let base_px = vec2<i32>(
    i32(clamp(base_px_f.x, 0.0, f32(effective_resolution - 1u))),
    i32(clamp(base_px_f.y, 0.0, f32(effective_resolution - 1u)))
  );
  let my_depth_n = clamp(proj_pos.z, -1.0, 1.0);
  let NdL_shadow = max(dot(n, L), 0.0);
  let bias = directional_compare_bias + directional_compare_bias * 0.75 * (1.0 - NdL_shadow);
  let max_px = vec2<i32>(i32(effective_resolution) - 1, i32(effective_resolution) - 1);
  let hard_shadow_sample = textureLoad(in_shadow_maps, base_px, i32(layer), 0);
  let hard_sampled_depth_n = clamp(hard_shadow_sample.r, -1.0, 1.0);
  let hard_sampled_shadow_group_id = u32(hard_shadow_sample.g + 0.5);
  let hard_same_shadow_group =
    receiver_shadow_group_id != 0u &&
    hard_sampled_shadow_group_id == receiver_shadow_group_id;
  let hard_receiver_minus_occluder = my_depth_n - hard_sampled_depth_n;
  let hard_seam_lit = hard_same_shadow_group && hard_receiver_minus_occluder <= directional_seam_epsilon_n;
  let hard_visibility = select(0.0, 1.0, hard_seam_lit || hard_sampled_depth_n >= my_depth_n - bias);
  var visibility = 0.0;
  var sample_weight_sum = 0.0;
  for (var dy: i32 = -1; dy <= 1; dy = dy + 1) {
    for (var dx: i32 = -1; dx <= 1; dx = dx + 1) {
      let off = base_px + vec2<i32>(dx, dy);
      let clamped_off = clamp(off, vec2<i32>(0, 0), max_px);
      let shadow_sample = textureLoad(in_shadow_maps, clamped_off, i32(layer), 0);
      let sampled_depth_n = clamp(shadow_sample.r, -1.0, 1.0);
      let sampled_shadow_group_id = u32(shadow_sample.g + 0.5);
      let same_shadow_group =
        receiver_shadow_group_id != 0u &&
        sampled_shadow_group_id == receiver_shadow_group_id;
      let receiver_minus_occluder = my_depth_n - sampled_depth_n;
      let seam_lit = same_shadow_group && receiver_minus_occluder <= directional_seam_epsilon_n;
      let wx = f32(2 - abs(dx));
      let wy = f32(2 - abs(dy));
      let sample_weight = wx * wy;
      sample_weight_sum += sample_weight;
      visibility += sample_weight * select(0.0, 1.0, seam_lit || sampled_depth_n >= my_depth_n - bias);
    }
  }
  let pcf_visibility = visibility / max(sample_weight_sum, 1.0);
  let softness = saturate(uCamera.ao_quality.z);
  return mix(hard_visibility, pcf_visibility, softness);
}

fn point_shadow_face_and_uv(dir: vec3<f32>) -> PointShadowLookup {
  let abs_dir = abs(dir);
  if (abs_dir.x >= abs_dir.y && abs_dir.x >= abs_dir.z) {
    if (dir.x >= 0.0) {
      return PointShadowLookup(0u, vec2<f32>(-dir.z, -dir.y) / max(abs_dir.x, 1e-5) * 0.5 + 0.5);
    }
    return PointShadowLookup(1u, vec2<f32>(dir.z, -dir.y) / max(abs_dir.x, 1e-5) * 0.5 + 0.5);
  }
  if (abs_dir.y >= abs_dir.z) {
    if (dir.y >= 0.0) {
      return PointShadowLookup(2u, vec2<f32>(dir.x, dir.z) / max(abs_dir.y, 1e-5) * 0.5 + 0.5);
    }
    return PointShadowLookup(3u, vec2<f32>(dir.x, -dir.z) / max(abs_dir.y, 1e-5) * 0.5 + 0.5);
  }
  if (dir.z >= 0.0) {
    return PointShadowLookup(4u, vec2<f32>(dir.x, -dir.y) / max(abs_dir.z, 1e-5) * 0.5 + 0.5);
  }
  return PointShadowLookup(5u, vec2<f32>(-dir.x, -dir.y) / max(abs_dir.z, 1e-5) * 0.5 + 0.5);
}

fn sample_point_shadow(
  light: Light,
  p: vec3<f32>,
  shadow_normal: vec3<f32>,
  L: vec3<f32>,
  receiver_shadow_group_id: u32,
  receiver_shadow_seam_epsilon: f32
) -> f32 {
  let light_to_receiver = p - light.position.xyz;
  let receiver_depth_m = length(light_to_receiver);
  if (receiver_depth_m <= 1e-4) {
    return 1.0;
  }

  let lookup = point_shadow_face_and_uv(light_to_receiver / receiver_depth_m);
  let layer = light.shadow_meta.x + lookup.face;
  let layer_params = shadow_layer_params[layer];
  let effective_resolution = max(u32(layer_params.effective_resolution + 0.5), 1u);
  let texel_depth_m = receiver_depth_m * (2.0 / f32(effective_resolution));
  let receiver_offset = max(0.05, max(receiver_shadow_seam_epsilon * 0.5, texel_depth_m * 0.75));
  let pos_off = p + shadow_normal * receiver_offset;
  let my_depth_m = distance(light.position.xyz, pos_off);
  let base_px_f = clamp(lookup.uv, vec2<f32>(0.0), vec2<f32>(1.0)) * vec2<f32>(f32(effective_resolution), f32(effective_resolution));
  let base_px = vec2<i32>(
    i32(clamp(base_px_f.x, 0.0, f32(effective_resolution - 1u))),
    i32(clamp(base_px_f.y, 0.0, f32(effective_resolution - 1u)))
  );
  let shadow_sample = textureLoad(in_shadow_maps, base_px, i32(layer), 0);
  let sampled_depth = shadow_sample.r;
  let sampled_shadow_group_id = u32(shadow_sample.g + 0.5);
  let same_shadow_group =
    receiver_shadow_group_id != 0u &&
    sampled_shadow_group_id == receiver_shadow_group_id;
  let NdL_shadow = max(dot(shadow_normal, L), 0.0);
  let seam_tolerance_m = max(receiver_shadow_seam_epsilon, texel_depth_m * 1.5);
  let biasM = max(seam_tolerance_m, 0.05 + texel_depth_m + 0.08 * (1.0 - NdL_shadow));
  let receiver_minus_occluder = my_depth_m - sampled_depth;
  let seam_lit = same_shadow_group && receiver_minus_occluder <= seam_tolerance_m;
  return select(0.0, 1.0, seam_lit || sampled_depth >= my_depth_m - biasM);
}

fn absorption_coefficient(base_color: vec3<f32>, transmission: f32, density: f32) -> vec3<f32> {
  let tint = clamp(base_color, vec3<f32>(0.04), vec3<f32>(0.995));
  let colored = -log(tint) * max(transmission, 0.1);
  let neutral = vec3<f32>(0.35 * max(1.0 - transmission, 0.0));
  return (colored + neutral) * max(density, 0.0);
}

fn refraction_uv_offset(
  pos_ws: vec3<f32>,
  normal_ws: vec3<f32>,
  view_dir: vec3<f32>,
  ior: f32,
  refraction_strength: f32,
  travel_dist: f32
) -> vec2<f32> {
  if (refraction_strength <= 0.0 || ior <= 1.001) {
    return vec2<f32>(0.0, 0.0);
  }
  let incident = normalize(-view_dir);
  let eta = 1.0 / ior;
  let refr_dir = refract(incident, normal_ws, eta);
  if (length(refr_dir) < 1e-4) {
    return vec2<f32>(0.0, 0.0);
  }
  let ior_delta = max(ior - 1.0, 0.0);
  let optical_dist = clamp(travel_dist, 0.0, 20.0);
  let sample_dist = max(optical_dist, 0.35) * (0.45 + refraction_strength * 0.9 + ior_delta * 1.2);
  let straight_uv = world_to_uv(pos_ws + incident * sample_dist);
  let refr_uv = world_to_uv(pos_ws + refr_dir * sample_dist);
  let thickness_boost = 0.85 + min(optical_dist, 10.0) * 0.08;
  return (refr_uv - straight_uv) * refraction_strength * thickness_boost;
}

fn tile_index_for_frag_pos(frag_pos: vec4<f32>) -> u32 {
  let pixel = vec2<u32>(
    min(u32(max(frag_pos.x, 0.0)), max(tile_light_params.screen_width, 1u) - 1u),
    min(u32(max(frag_pos.y, 0.0)), max(tile_light_params.screen_height, 1u) - 1u),
  );
  let tile_coord = min(
    pixel / tile_light_params.tile_size,
    vec2<u32>(
      max(tile_light_params.tiles_x, 1u) - 1u,
      max(tile_light_params.tiles_y, 1u) - 1u,
    ),
  );
  return tile_coord.y * tile_light_params.tiles_x + tile_coord.x;
}

fn calculate_lighting(
  p: vec3<f32>,
  n: vec3<f32>,
  base_color: vec3<f32>,
  roughness: f32,
  metalness: f32,
  ior: f32,
  emissive: vec3<f32>,
  two_sided_lighting: bool,
  receiver_shadow_group_id: u32,
  receiver_shadow_seam_epsilon: f32,
  tile_index: u32
) -> vec3<f32> {
  let V = normalize(uCamera.cam_pos.xyz - p);
  let NdotV = max(dot(n, V), 0.0);
  let dielectric_f0 = dielectric_f0_from_ior(ior);
  let F0 = mix(dielectric_f0, base_color, metalness);
  let ambient_fresnel = fresnel_schlick_roughness(NdotV, F0, roughness);
  let ambient_kd = (vec3<f32>(1.0) - ambient_fresnel) * (1.0 - metalness);
  let diffuse_ambient_light = uCamera.ambient_color.xyz * directional_ambient_scale(n);
  let reflection_dir = reflect(-V, n);
  let rough_reflection_dir = normalize(mix(reflection_dir, n, saturate(roughness * roughness)));
  let specular_ambient_light = uCamera.ambient_color.xyz * directional_ambient_scale(rough_reflection_dir);
  var total_light = ambient_kd * base_color * diffuse_ambient_light + ambient_fresnel * specular_ambient_light + emissive;
  
  let tile_header = tile_light_headers[tile_index];
  for (var i = 0u; i < tile_header.count; i++) {
    let light = lights[tile_light_indices[tile_header.offset + i]];
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

    let shadow_normal = select(n, n * select(-1.0, 1.0, dot(n, L) >= 0.0), two_sided_lighting);

    if (light.shadow_meta.y > 0u) {
      if (light_type == 1u) {
        let selection = choose_directional_cascade(light, p);
        let primary_visibility = sample_directional_shadow(light, p, shadow_normal, L, receiver_shadow_group_id, receiver_shadow_seam_epsilon, selection.primary_index);
        let secondary_visibility = select(
          primary_visibility,
          sample_directional_shadow(light, p, shadow_normal, L, receiver_shadow_group_id, receiver_shadow_seam_epsilon, selection.secondary_index),
          selection.secondary_index != selection.primary_index,
        );
        attenuation *= mix(primary_visibility, secondary_visibility, selection.blend);
      } else if (light_type == 2u) {
        let shadow_view_proj = light.view_proj;
        let layer = light.shadow_meta.x;
        let layer_params = shadow_layer_params[layer];
        let effective_resolution = max(u32(layer_params.effective_resolution + 0.5), 1u);
        let pos_ls = shadow_view_proj * vec4<f32>(p, 1.0);
        let proj_pos = pos_ls.xyz / pos_ls.w;
        let shadow_uv = vec2<f32>(proj_pos.x * 0.5 + 0.5, -proj_pos.y * 0.5 + 0.5);

        if (pos_ls.w > 0.0 && shadow_uv.x >= 0.0 && shadow_uv.x <= 1.0 && shadow_uv.y >= 0.0 && shadow_uv.y <= 1.0) {
          let base_px_f = shadow_uv * vec2<f32>(f32(effective_resolution), f32(effective_resolution));
          let base_px = vec2<i32>(
            i32(clamp(base_px_f.x, 0.0, f32(effective_resolution - 1u))),
            i32(clamp(base_px_f.y, 0.0, f32(effective_resolution - 1u)))
          );

          let receiver_offset = max(receiver_shadow_seam_epsilon * 0.5, 0.05);
          let pos_off = p + shadow_normal * receiver_offset;
          let my_depth_m = distance(light.position.xyz, pos_off);
          let NdL_shadow = max(dot(shadow_normal, L), 0.0);
          let max_px = vec2<i32>(i32(effective_resolution) - 1, i32(effective_resolution) - 1);
          let hard_shadow_sample = textureLoad(in_shadow_maps, base_px, i32(layer), 0);
          let hard_sampled_depth = hard_shadow_sample.r;
          let hard_sampled_shadow_group_id = u32(hard_shadow_sample.g + 0.5);
          let hard_same_shadow_group =
            receiver_shadow_group_id != 0u &&
            hard_sampled_shadow_group_id == receiver_shadow_group_id;
          let baseBiasM = 0.05;
          let slopeBiasM = 0.1;
          let biasM = baseBiasM + slopeBiasM * (1.0 - NdL_shadow);
          let hard_receiver_minus_occluder = my_depth_m - hard_sampled_depth;
          let hard_seam_lit = hard_same_shadow_group && hard_receiver_minus_occluder <= receiver_shadow_seam_epsilon;
          let hard_visibility = select(0.0, 1.0, hard_seam_lit || hard_sampled_depth >= my_depth_m - biasM);
          var visibility = 0.0;
          var sample_weight_sum = 0.0;
          for (var dy: i32 = -2; dy <= 2; dy = dy + 1) {
            for (var dx: i32 = -2; dx <= 2; dx = dx + 1) {
              let off = base_px + vec2<i32>(dx, dy);
              let clamped_off = clamp(off, vec2<i32>(0, 0), max_px);
              let shadow_sample = textureLoad(in_shadow_maps, clamped_off, i32(layer), 0);
              let sampled_depth = shadow_sample.r;
              let sampled_shadow_group_id = u32(shadow_sample.g + 0.5);
              let same_shadow_group =
                receiver_shadow_group_id != 0u &&
                sampled_shadow_group_id == receiver_shadow_group_id;
              let receiver_minus_occluder = my_depth_m - sampled_depth;
              let seam_lit = same_shadow_group && receiver_minus_occluder <= receiver_shadow_seam_epsilon;
              sample_weight_sum += 1.0;
              visibility += select(0.0, 1.0, seam_lit || sampled_depth >= my_depth_m - biasM);
            }
          }
          let pcf_visibility = visibility / max(sample_weight_sum, 1.0);
          let softness = saturate(uCamera.ao_quality.w);
          attenuation *= mix(hard_visibility, pcf_visibility, softness);
        }
      } else {
        attenuation *= sample_point_shadow(light, p, shadow_normal, L, receiver_shadow_group_id, receiver_shadow_seam_epsilon);
      }
    }
    
    if (attenuation <= 0.0) {
      continue;
    }

    let NdotL = select(max(dot(n, L), 0.0), abs(dot(n, L)), two_sided_lighting);
    let NdotV_local = select(NdotV, abs(dot(n, V)), two_sided_lighting);
    if (NdotL <= 0.0 || NdotV_local <= 0.0) {
      continue;
    }

    let H = normalize(V + L);
    let NdotH = select(max(dot(n, H), 0.0), abs(dot(n, H)), two_sided_lighting);
    let HdotV = max(dot(H, V), 0.0);
    let rough = max(roughness, MIN_ROUGHNESS);
    let fresnel = fresnel_schlick(HdotV, F0);
    let D = distribution_ggx(NdotH, rough);
    let G = geometry_smith(NdotV_local, NdotL, rough);
    let specular = (D * G * fresnel) / max(4.0 * NdotV_local * NdotL, PBR_EPSILON);
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
  let tile_index = tile_index_for_frag_pos(frag_pos);
  let t_opaque = textureLoad(in_depth, ipos, 0).r;
  var t_limit = t_opaque;
  if (t_limit >= FAR_T) { t_limit = FAR_T; }

  let uv_screen = (vec2<f32>(f32(ipos.x), f32(ipos.y)) + 0.5) / vec2<f32>(f32(dims.x), f32(dims.y));
  let ray = get_ray_from_uv(uv_screen);

  var surface_rgb = vec3<f32>(0.0);
  var accum_a = 0.0;
  var accum_w = 0.0;
  var throughput = vec3<f32>(1.0);
  var distortion_sum = vec2<f32>(0.0, 0.0);
  var distortion_weight = 0.0;
  var front_z = 1.0;
  var hit_transparent = false;

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
            let k: f32 = 8.0;
            var refractive_path_ws = 0.0;

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
                        let extra = materials[mat_idx + 3u];
                        let emissive = srgb_to_linear(materials[mat_idx + 1u].xyz) * max(extra.x, 0.0);
                        let pbr = materials[mat_idx + 2u];
                        let transmission = clamp(extra.y, 0.0, 1.0);
                        let density = max(extra.z, 0.0);
                        let refraction_strength = max(extra.w, 0.0);
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
                              let p_hit_os = ray_os.origin + dir * (t_micro + dt * 0.5);
                              let voxel_center_os = floor(p_hit_os) + 0.5;
                              let vi_hit = vec3<i32>(floor(voxel_center_os));
                              var n_os = estimate_normal_os(voxel_center_os, params);
                              var two_sided_lighting = false;
                              if (length(n_os) < 0.01) {
                                n_os = fallback_exposed_voxel_normal_os(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params);
                                two_sided_lighting = has_two_sided_voxel_exposure_os(voxel_center_os, params);
                              }
                              if (length(n_os) < 0.01) {
                                n_os = fallback_face_normal_os(p_hit_os, vi_hit, dir);
                              }
                              let n_ws = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                              let pos_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                              let shadow_seam_epsilon = shadow_seam_epsilon_at_hit(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params.shadow_seam_epsilon);
                              let z = clamp(t_micro / max(t_limit, 1e-4), 0.0, 1.0);
                              let view_dir = normalize(uCamera.cam_pos.xyz - pos_ws);
                              let opacity = clamp(1.0 - trans, 0.0, 1.0);
                              let is_volumetric = transmission > 0.01 || density > 0.01 || refraction_strength > 0.01;
                              let medium_density = max(density, 0.02);
                              refractive_path_ws += dt_ws * (0.85 + min(medium_density, 3.0) * 0.3);
                              let coverage_sigma = max(0.015, medium_density * mix(0.25, 1.0, opacity));
                              let coverage_step = 1.0 - exp(-coverage_sigma * dt_ws);
                              let sigma_a = absorption_coefficient(base_col, transmission, medium_density * 0.75) * mix(0.15, 1.0, opacity);
                              let trans_step = select(vec3<f32>(1.0 - coverage_step), exp(-sigma_a * dt_ws), is_volumetric);
                              let color = calculate_lighting(pos_ws, n_ws, base_col, pbr.x, pbr.y, pbr.z, emissive, two_sided_lighting, params.shadow_group_id, shadow_seam_epsilon, tile_index);
                              let refract_off = select(
                                vec2<f32>(0.0, 0.0),
                                refraction_uv_offset(pos_ws, n_ws, view_dir, max(pbr.z, 1.001), refraction_strength, refractive_path_ws),
                                is_volumetric,
                              );
                              if (!hit_transparent) {
                                front_z = z;
                                hit_transparent = true;
                              }
                              distortion_sum += refract_off * coverage_step;
                              distortion_weight += coverage_step;
                              surface_rgb += throughput * color * coverage_step;
                              throughput *= trans_step;
                              accum_a += coverage_sigma * dt_ws;
                              if (accum_a > 3.0) { break; }
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
                            let palette_idx = load_u8(b_atlas, bricks[packed_idx].atlas_page, voxel_idx);
                            if (palette_idx != 0u) {
                              let mat_base = params.material_table_base;
                              let mat_idx = mat_base + palette_idx * 4u;
                              let base_col = srgb_to_linear(materials[mat_idx].xyz);
                              let extra = materials[mat_idx + 3u];
                              let emissive = srgb_to_linear(materials[mat_idx + 1u].xyz) * max(extra.x, 0.0);
                              let pbr = materials[mat_idx + 2u];
                              let transmission = clamp(extra.y, 0.0, 1.0);
                              let density = max(extra.z, 0.0);
                              let refraction_strength = max(extra.w, 0.0);
                              let trans = clamp(pbr.w, 0.0, 1.0);
                              if (trans > 0.001) {
                                let t_next = min(t_max_micro.x, min(t_max_micro.y, t_max_micro.z));
                                let dt = max(0.0, t_next - t_micro);
                                if (dt > 0.0) {
                                  let dt_ws = dt * d_ws_scale;
                                  let p_hit_os = ray_os.origin + dir * (t_micro + dt * 0.5);
                                  let voxel_center_os = floor(p_hit_os) + 0.5;
                                  let vi_hit = vec3<i32>(floor(voxel_center_os));
                                  var n_os = estimate_normal_os(voxel_center_os, params);
                                  var two_sided_lighting = false;
                                  if (length(n_os) < 0.01) {
                                    n_os = fallback_exposed_voxel_normal_os(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params);
                                    two_sided_lighting = has_two_sided_voxel_exposure_os(voxel_center_os, params);
                                  }
                                  if (length(n_os) < 0.01) {
                                    n_os = fallback_face_normal_os(p_hit_os, vi_hit, dir);
                                  }
                                  let n_ws = normalize((transpose(inst.world_to_object) * vec4<f32>(n_os, 0.0)).xyz);
                                  let pos_ws = (inst.object_to_world * vec4<f32>(voxel_center_os, 1.0)).xyz;
                                  let shadow_seam_epsilon = shadow_seam_epsilon_at_hit(voxel_center_os, inst.local_aabb_min.xyz, inst.local_aabb_max.xyz, params.shadow_seam_epsilon);
                                  let z = clamp(t_micro / max(t_limit, 1e-4), 0.0, 1.0);
                                  let view_dir = normalize(uCamera.cam_pos.xyz - pos_ws);
                                  let opacity = clamp(1.0 - trans, 0.0, 1.0);
                                  let is_volumetric = transmission > 0.01 || density > 0.01 || refraction_strength > 0.01;
                                  let medium_density = max(density, 0.02);
                                  refractive_path_ws += dt_ws * (0.85 + min(medium_density, 3.0) * 0.3);
                                  let coverage_sigma = max(0.015, medium_density * mix(0.25, 1.0, opacity));
                                  let coverage_step = 1.0 - exp(-coverage_sigma * dt_ws);
                                  let sigma_a = absorption_coefficient(base_col, transmission, medium_density * 0.75) * mix(0.15, 1.0, opacity);
                                  let trans_step = select(vec3<f32>(1.0 - coverage_step), exp(-sigma_a * dt_ws), is_volumetric);
                                  let color = calculate_lighting(pos_ws, n_ws, base_col, pbr.x, pbr.y, pbr.z, emissive, two_sided_lighting, params.shadow_group_id, shadow_seam_epsilon, tile_index);
                                  let refract_off = select(
                                    vec2<f32>(0.0, 0.0),
                                    refraction_uv_offset(pos_ws, n_ws, view_dir, max(pbr.z, 1.001), refraction_strength, refractive_path_ws),
                                    is_volumetric,
                                  );
                                  if (!hit_transparent) {
                                    front_z = z;
                                    hit_transparent = true;
                                  }
                                  distortion_sum += refract_off * coverage_step;
                                  distortion_weight += coverage_step;
                                  surface_rgb += throughput * color * coverage_step;
                                  throughput *= trans_step;
                                  accum_a += coverage_sigma * dt_ws;
                                  if (accum_a > 3.0) { break; }
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

  if (!hit_transparent) {
    return FSOut(vec4<f32>(0.0, 0.0, 0.0, 0.0), 0.0);
  }

  let throughput_scalar = clamp(dot(throughput, vec3<f32>(0.33333334, 0.33333334, 0.33333334)), 1e-4, 1.0);
  let coverage = clamp(1.0 - throughput_scalar, 0.0, 0.98);
  var refract_uv = uv_screen;
  if (distortion_weight > 1e-4) {
    refract_uv += distortion_sum / distortion_weight;
  }
  let opaque_bg = sample_opaque_lit(uv_screen);
  let refracted_bg = sample_opaque_lit(refract_uv);
  let final_color = surface_rgb + refracted_bg * throughput - opaque_bg * throughput_scalar;
  let alpha_weight = max(coverage, 1e-3) * pow(1.0 - clamp(front_z, 0.0, 1.0), 8.0);
  accum_w = alpha_weight;
  let accum_alpha = -log(throughput_scalar) / 2.0;

  return FSOut(vec4<f32>(final_color * alpha_weight, accum_alpha), accum_w);
}
