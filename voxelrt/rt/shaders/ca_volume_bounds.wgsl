struct CAParams {
  dt: f32,
  elapsed: f32,
  volume_count: u32,
  atlas_width: u32,
  atlas_height: u32,
  atlas_depth: u32,
  pad1: u32,
  pad2: u32,
  pad3: u32,
  pad4: u32,
  pad5: u32,
  pad6: u32,
};

struct VolumeRecord {
  local_to_world: mat4x4<f32>,
  world_to_local: mat4x4<f32>,
  sim_params: vec4<f32>,
  render_params: vec4<f32>,
  scatter_color: vec4<f32>,
  shadow_tint: vec4<f32>,
  absorption_color: vec4<f32>,
  grid: vec4<f32>,
};

struct VolumeBounds {
  min_coord: vec4<u32>,
  max_coord: vec4<u32>,
};

@group(0) @binding(0) var<uniform> params: CAParams;
@group(0) @binding(1) var<storage, read> volumes: array<VolumeRecord>;
@group(0) @binding(2) var<storage, read_write> bounds_out: array<VolumeBounds>;
@group(1) @binding(0) var ca_field: texture_3d<f32>;

fn packed_volume_type(v: VolumeRecord) -> u32 {
  return u32(v.render_params.z + 0.5) & 7u;
}

fn packed_volume_preset(v: VolumeRecord) -> u32 {
  return u32(v.render_params.z + 0.5) >> 3u;
}

fn sample_field(local: vec3<u32>, z0: u32) -> vec2<f32> {
  return textureLoad(ca_field, vec3<i32>(i32(local.x), i32(local.y), i32(z0 + local.z)), 0).xy;
}

var<workgroup> wg_min: array<atomic<u32>, 3>;
var<workgroup> wg_max: array<atomic<u32>, 3>;
var<workgroup> wg_found: atomic<u32>;

@compute @workgroup_size(256, 1, 1)
fn compute_bounds(
  @builtin(local_invocation_id) lid: vec3<u32>,
  @builtin(workgroup_id) wid: vec3<u32>
) {
  let idx = wid.x;
  if (idx >= params.volume_count) {
    return;
  }

  let v = volumes[idx];
  let res = vec3<u32>(u32(v.grid.x), u32(v.grid.y), u32(v.grid.z));
  let z0 = u32(v.grid.w);
  let total_cells = res.x * res.y * res.z;
  let volume_type = packed_volume_type(v);
  let preset = packed_volume_preset(v);

  // Initialize workgroup shared memory
  if (lid.x == 0u) {
    atomicStore(&wg_min[0], 0xFFFFFFFFu);
    atomicStore(&wg_min[1], 0xFFFFFFFFu);
    atomicStore(&wg_min[2], 0xFFFFFFFFu);
    atomicStore(&wg_max[0], 0u);
    atomicStore(&wg_max[1], 0u);
    atomicStore(&wg_max[2], 0u);
    atomicStore(&wg_found, 0u);
  }
  workgroupBarrier();

  // Parallel grid traversal
  for (var i = lid.x; i < total_cells; i = i + 256u) {
    let z = i / (res.x * res.y);
    let rem = i % (res.x * res.y);
    let y = rem / res.x;
    let x = rem % res.x;

    let s = sample_field(vec3<u32>(x, y, z), z0);
    var occupied = s.x > 0.025 || s.y > 0.04;
    if (preset == 3u && volume_type == 1u) {
      occupied = s.x > 0.003 || s.y > 0.015;
    } else if (preset == 4u) {
      occupied = s.x > 0.02 || s.y > 0.03;
    }

    if (occupied) {
      atomicMin(&wg_min[0], x);
      atomicMin(&wg_min[1], y);
      atomicMin(&wg_min[2], z);
      atomicMax(&wg_max[0], x);
      atomicMax(&wg_max[1], y);
      atomicMax(&wg_max[2], z);
      atomicStore(&wg_found, 1u);
    }
  }
  workgroupBarrier();

  // Aggregate results and apply expansion
  if (lid.x == 0u) {
    if (atomicLoad(&wg_found) == 0u) {
      bounds_out[idx] = VolumeBounds(vec4<u32>(0u), vec4<u32>(0u));
    } else {
      let min_coord = vec3<u32>(atomicLoad(&wg_min[0]), atomicLoad(&wg_min[1]), atomicLoad(&wg_min[2]));
      let max_coord = vec3<u32>(atomicLoad(&wg_max[0]), atomicLoad(&wg_max[1]), atomicLoad(&wg_max[2]));

      var expand_lo = 3u;
      var expand_hi = 4u;
      if (preset == 3u && volume_type == 1u) {
        expand_lo = 8u;
        expand_hi = 10u;
      }

      let expand_min = vec3<u32>(
        select(0u, min_coord.x - expand_lo, min_coord.x >= expand_lo),
        select(0u, min_coord.y - expand_lo, min_coord.y >= expand_lo),
        select(0u, min_coord.z - expand_lo, min_coord.z >= expand_lo),
      );
      let expand_max = vec3<u32>(
        min(max_coord.x + 1u + expand_hi, res.x),
        min(max_coord.y + 1u + expand_hi, res.y),
        min(max_coord.z + 1u + expand_hi, res.z),
      );
      bounds_out[idx] = VolumeBounds(vec4<u32>(expand_min, 0u), vec4<u32>(expand_max, 0u));
    }
  }
}
