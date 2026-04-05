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
    ao_quality: vec4<f32>,
};

struct CelestialBodyParams {
    body_count: u32,
    time_seconds: f32,
    pad1: u32,
    pad2: u32,
};

struct CelestialBody {
    center_radius: vec4<f32>,
    surface_color: vec4<f32>,
    atmosphere_color: vec4<f32>,
    cloud_color: vec4<f32>,
    params: vec4<f32>, // x atmosphere_radius, y cloud_radius, z cloud_coverage, w emission
    noise: vec4<f32>,  // x surface_seed, y surface_scale, z cloud_seed, w cloud_scale
    art_primary: vec4<f32>,   // x atmosphere_density, y atmosphere_falloff, z atmosphere_glow, w cloud_opacity
    art_secondary: vec4<f32>, // x cloud_sharpness, y cloud_drift_speed, z cloud_banding
    art_tertiary: vec4<f32>,  // x surface_biome_mix, y cloud_tint_warmth, z night_side_fill, w terminator_softness
    flags: vec4<f32>,  // x disable_surface, y surface_occlusion_bias
};

struct VsOut {
    @builtin(position) position: vec4<f32>,
    @location(0) uv: vec2<f32>,
};

struct CelestialHit {
    index: i32,
    atmo_entry: f32,
    atmo_exit: f32,
    surface_t: f32,
};

struct CelestialLayer {
    color: vec3<f32>,
    alpha: f32,
};

@group(0) @binding(0) var<uniform> camera: CameraData;
@group(0) @binding(1) var<uniform> body_params: CelestialBodyParams;
@group(0) @binding(2) var<storage, read> bodies: array<CelestialBody>;

@group(1) @binding(0) var in_depth: texture_2d<f32>;

fn saturate(v: f32) -> f32 {
    return clamp(v, 0.0, 1.0);
}

fn saturate3(v: vec3<f32>) -> vec3<f32> {
    return clamp(v, vec3<f32>(0.0), vec3<f32>(1.0));
}

fn hash13(p: vec3<f32>) -> f32 {
    let q = fract(p * 0.1031);
    let q2 = q + dot(q, q.yzx + 33.33);
    return fract((q2.x + q2.y) * q2.z);
}

fn value_noise3(p: vec3<f32>) -> f32 {
    let i = floor(p);
    let f = fract(p);
    let u = f * f * (3.0 - 2.0 * f);

    let n000 = hash13(i + vec3<f32>(0.0, 0.0, 0.0));
    let n100 = hash13(i + vec3<f32>(1.0, 0.0, 0.0));
    let n010 = hash13(i + vec3<f32>(0.0, 1.0, 0.0));
    let n110 = hash13(i + vec3<f32>(1.0, 1.0, 0.0));
    let n001 = hash13(i + vec3<f32>(0.0, 0.0, 1.0));
    let n101 = hash13(i + vec3<f32>(1.0, 0.0, 1.0));
    let n011 = hash13(i + vec3<f32>(0.0, 1.0, 1.0));
    let n111 = hash13(i + vec3<f32>(1.0, 1.0, 1.0));

    let x00 = mix(n000, n100, u.x);
    let x10 = mix(n010, n110, u.x);
    let x01 = mix(n001, n101, u.x);
    let x11 = mix(n011, n111, u.x);
    let y0 = mix(x00, x10, u.y);
    let y1 = mix(x01, x11, u.y);
    return mix(y0, y1, u.z);
}

fn fbm3(p: vec3<f32>) -> f32 {
    var total = 0.0;
    var amplitude = 0.5;
    var frequency = 1.0;
    for (var i = 0; i < 4; i++) {
        total += value_noise3(p * frequency) * amplitude;
        frequency *= 2.0;
        amplitude *= 0.5;
    }
    return total;
}

fn ridge_noise3(p: vec3<f32>) -> f32 {
    return 1.0 - abs(fbm3(p) * 2.0 - 1.0);
}

fn rayleigh_phase(cos_theta: f32) -> f32 {
    return 0.75 * (1.0 + cos_theta * cos_theta);
}

fn hg_phase(cos_theta: f32, g: f32) -> f32 {
    let g2 = g * g;
    let denom = max(1.0 + g2 - 2.0 * g * cos_theta, 1e-3);
    return (1.0 - g2) / pow(denom, 1.5);
}

fn make_primary_ray(uv: vec2<f32>) -> vec3<f32> {
    let ndc = vec2<f32>(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0);
    let clip = vec4<f32>(ndc, 1.0, 1.0);
    let view = camera.inv_proj * clip;
    let view_dir = normalize(view.xyz / max(view.w, 1e-5));
    let world = camera.inv_view * vec4<f32>(view_dir, 0.0);
    return normalize(world.xyz);
}

fn ray_sphere(origin: vec3<f32>, dir: vec3<f32>, center: vec3<f32>, radius: f32) -> vec2<f32> {
    let oc = origin - center;
    let b = dot(oc, dir);
    let c = dot(oc, oc) - radius * radius;
    let h = b * b - c;
    if (h < 0.0) {
        return vec2<f32>(1e9, -1.0);
    }
    let root = sqrt(h);
    return vec2<f32>(-b - root, -b + root);
}

fn empty_hit() -> CelestialHit {
    return CelestialHit(-1, 1e9, -1.0, -1.0);
}

fn find_nearest_hit(ray_origin: vec3<f32>, ray_dir: vec3<f32>, min_entry: f32) -> CelestialHit {
    var best = empty_hit();

    for (var i = 0u; i < body_params.body_count; i++) {
        let body = bodies[i];
        let center = body.center_radius.xyz;
        let radius = body.center_radius.w;
        let atmo_radius = max(body.params.x, radius);

        let surface_hit = ray_sphere(ray_origin, ray_dir, center, radius);
        let atmo_hit = ray_sphere(ray_origin, ray_dir, center, atmo_radius);
        if (atmo_hit.y < 0.0) {
            continue;
        }

        let atmo_entry = max(atmo_hit.x, 0.0);
        if (atmo_entry < min_entry) {
            continue;
        }

        var surface_t = -1.0;
        if (surface_hit.y >= 0.0) {
            surface_t = max(surface_hit.x, 0.0);
        }

        if (atmo_entry < best.atmo_entry) {
            best = CelestialHit(i32(i), atmo_entry, atmo_hit.y, surface_t);
        }
    }

    return best;
}

fn sample_surface(
    body: CelestialBody,
    normal: vec3<f32>,
    light_dir: vec3<f32>,
    view_dir: vec3<f32>,
    sun_intensity: f32
) -> vec3<f32> {
    let base_color = body.surface_color.xyz;
    let seed = body.noise.x;
    let noise_scale = max(body.noise.y, 0.15);
    let surface_biome_mix = clamp(body.art_tertiary.x, 0.0, 1.0);
    let night_side_fill = max(body.art_tertiary.z, 0.0);
    let terminator_softness = max(body.art_tertiary.w, 0.05);

    let warped_normal = normal + vec3<f32>(
        fbm3(normal * (noise_scale * 0.45) + vec3<f32>(seed * 0.013, 2.1, -4.2)),
        fbm3(normal.yzx * (noise_scale * 0.47) + vec3<f32>(-1.3, seed * 0.017, 3.7)),
        fbm3(normal.zxy * (noise_scale * 0.43) + vec3<f32>(4.8, -2.4, seed * 0.019))
    ) - vec3<f32>(0.5);

    let continental = fbm3(warped_normal * noise_scale + vec3<f32>(seed, seed * 0.31, seed * 0.73));
    let ridge = ridge_noise3(warped_normal * (noise_scale * 2.2) + vec3<f32>(seed * 0.11, -3.7, 6.3));
    let basin = fbm3(warped_normal * (noise_scale * 0.85) + vec3<f32>(-5.6, seed * 0.07, 1.4));
    let terrain = saturate(continental * (0.56 + surface_biome_mix * 0.32) + ridge * (0.22 + surface_biome_mix * 0.28) - basin * 0.14);

    let lowland_color = base_color * mix(vec3<f32>(0.82, 0.95, 1.02), vec3<f32>(0.58, 0.86, 1.14), surface_biome_mix);
    let highland_color = base_color * mix(vec3<f32>(1.02, 1.01, 0.96), vec3<f32>(1.18, 1.04, 0.84), surface_biome_mix);
    let peak_color = mix(base_color, vec3<f32>(1.0, 1.0, 1.0), 0.3 + surface_biome_mix * 0.28);
    var albedo = mix(lowland_color, highland_color, smoothstep(0.28, 0.72, terrain));
    albedo = mix(albedo, peak_color, smoothstep(0.72, 0.94, terrain) * ridge);

    let NdotL = saturate(dot(normal, light_dir));
    let NdotV = saturate(dot(normal, view_dir));
    let wrapped_diffuse = saturate((dot(normal, light_dir) + terminator_softness) / (1.0 + terminator_softness));
    let half_vec = normalize(light_dir + view_dir);
    let spec = pow(saturate(dot(normal, half_vec)), 48.0) * (0.02 + 0.06 * ridge);
    let fresnel = pow(1.0 - NdotV, 5.0);
    let night_fill = vec3<f32>(0.012, 0.014, 0.02) * pow(1.0 - NdotL, 1.35) * night_side_fill;
    let diffuse_floor = 0.025;
    let direct_scale = max(sun_intensity, 0.0);

    return albedo * (diffuse_floor + wrapped_diffuse * 0.92 * direct_scale) + spec * direct_scale + fresnel * 0.02 + night_fill;
}

fn sample_clouds(
    body: CelestialBody,
    sphere_normal: vec3<f32>,
    light_dir: vec3<f32>,
    view_dir: vec3<f32>
) -> vec4<f32> {
    let seed = body.noise.z;
    let cloud_scale = max(body.noise.w, 0.15);
    let coverage = clamp(body.params.z, 0.0, 1.0);
    let cloud_opacity = clamp(body.art_primary.w, 0.0, 1.5);
    let cloud_sharpness = max(body.art_secondary.x, 0.1);
    let cloud_drift_speed = body.art_secondary.y;
    let cloud_banding = clamp(body.art_secondary.z, 0.0, 1.0);
    let cloud_tint_warmth = clamp(body.art_tertiary.y, 0.0, 1.0);
    let terminator_softness = max(body.art_tertiary.w, 0.05);
    let sun_intensity = max(camera.light_pos.w, 0.0);
    let drift_phase = body_params.time_seconds * cloud_drift_speed;
    let drift = vec3<f32>(drift_phase * 0.07, drift_phase * 0.013, -drift_phase * 0.05);

    let domain = vec3<f32>(
        fbm3(sphere_normal * (cloud_scale * 0.55) + drift + vec3<f32>(seed * 0.017, 1.7, -2.9)),
        fbm3(sphere_normal.yzx * (cloud_scale * 0.58) + drift.yzx + vec3<f32>(-4.3, seed * 0.011, 3.4)),
        fbm3(sphere_normal.zxy * (cloud_scale * 0.52) + drift.zxy + vec3<f32>(2.8, -1.6, seed * 0.023))
    ) - vec3<f32>(0.5);
    let cloud_coords = sphere_normal * cloud_scale + domain * 1.35 + drift + vec3<f32>(seed * 0.031, seed * 0.013, seed * 0.021);

    let base = fbm3(cloud_coords);
    let detail = fbm3(cloud_coords * 2.35 + vec3<f32>(4.2, -3.6, 1.1));
    let wisps = ridge_noise3(cloud_coords * 3.8 + vec3<f32>(-2.4, 5.7, 0.9));
    let bands = 1.0 - abs(sphere_normal.y);
    let field = saturate(base * 0.62 + detail * 0.23 + wisps * 0.3 + bands * cloud_banding * 0.18);

    let threshold = mix(0.82, 0.34, coverage);
    let feather = 0.11 / cloud_sharpness;
    let mask = smoothstep(threshold - feather, threshold + feather, field);

    let NdotL = saturate(dot(sphere_normal, light_dir));
    let NdotV = saturate(dot(sphere_normal, view_dir));
    let limb = pow(1.0 - NdotV, 2.2);
    let self_shadow = mix(0.58, 1.0, smoothstep(0.3, 0.88, detail));
    let softened_light = saturate((dot(sphere_normal, light_dir) + terminator_softness) / (1.0 + terminator_softness));
    let warm_tint = mix(vec3<f32>(1.0, 1.0, 1.0), vec3<f32>(1.14, 0.96, 0.82), cloud_tint_warmth * pow(1.0 - NdotL, 2.0));
    let lighting = 0.08 + softened_light * 0.84 * sun_intensity + limb * 0.18;
    let color = body.cloud_color.xyz * warm_tint * lighting * self_shadow;
    let alpha = mask * cloud_opacity * (0.05 + 0.22 * NdotL + 0.1 * limb);
    return vec4<f32>(color, alpha);
}

fn sample_cloud_layer(
    body: CelestialBody,
    ray_origin: vec3<f32>,
    ray_dir: vec3<f32>,
    center: vec3<f32>,
    cloud_radius: f32,
    depth_cap: f32
) -> vec4<f32> {
    let cloud_hit = ray_sphere(ray_origin, ray_dir, center, cloud_radius);
    if (cloud_hit.y < 0.0) {
        return vec4<f32>(0.0);
    }

    let cloud_t = max(cloud_hit.x, 0.0);
    if (cloud_t >= depth_cap) {
        return vec4<f32>(0.0);
    }

    let cloud_pos = ray_origin + ray_dir * cloud_t;
    let cloud_normal = normalize(cloud_pos - center);
    var clouds = sample_clouds(body, cloud_normal, normalize(camera.light_pos.xyz - cloud_pos), normalize(ray_origin - cloud_pos));
    clouds.a = saturate(clouds.a * 0.8);
    clouds = vec4<f32>(clouds.xyz * 1.02, clouds.a);
    return clouds;
}

fn sample_atmosphere(
    body: CelestialBody,
    ray_dir: vec3<f32>,
    light_dir: vec3<f32>,
    shell_entry: f32,
    shell_exit: f32,
    shell_cap: f32,
    radius: f32,
    atmosphere_radius: f32
) -> vec4<f32> {
    let thickness = max(atmosphere_radius - radius, 1e-3);
    let atmosphere_density = max(body.art_primary.x, 0.05);
    let atmosphere_falloff = max(body.art_primary.y, 0.2);
    let atmosphere_glow = max(body.art_primary.z, 0.05);
    let terminator_softness = max(body.art_tertiary.w, 0.05);
    let sun_intensity = max(camera.light_pos.w, 0.0);
    let shell_length = max(min(shell_exit, shell_cap) - shell_entry, 0.0);
    let optical_depth = saturate(shell_length / max(thickness * (3.2 / atmosphere_density), 1e-3));
    let sample_end = min(shell_exit, shell_cap);
    let sample_pos = camera.cam_pos.xyz + ray_dir * mix(shell_entry, sample_end, 0.35);
    let shell_normal = normalize(sample_pos - body.center_radius.xyz);
    let radial_dist = distance(sample_pos, body.center_radius.xyz);
    let altitude01 = saturate((radial_dist - radius) / thickness);

    let scatter_cos = dot(light_dir, -ray_dir);
    let rayleigh = rayleigh_phase(scatter_cos);
    let mie = hg_phase(scatter_cos, 0.72);
    let horizon = pow(1.0 - saturate(dot(shell_normal, -ray_dir)), 2.0);
    let day_side = saturate((dot(shell_normal, light_dir) + terminator_softness) / (1.0 + terminator_softness));
    let sunset = pow(1.0 - day_side, 3.0) * horizon;
    let density = (1.0 - altitude01);
    let density_soft = pow(density, atmosphere_falloff);
    let edge_glow = pow(horizon, 1.15);

    let base_color = body.atmosphere_color.xyz;
    let warm_scatter = vec3<f32>(1.0, 0.52, 0.22) * sunset * 0.26;
    let scatter_color = (base_color * (0.04 + rayleigh * 0.1 + mie * 0.028 + day_side * 0.24 * sun_intensity + edge_glow * 0.46 * atmosphere_glow) + warm_scatter * sun_intensity) * (0.3 + density_soft * 0.46);
    let alpha = saturate(optical_depth * (density_soft * 0.18 + edge_glow * 0.32 * atmosphere_glow) * (0.08 + 0.14 * day_side * sun_intensity + mie * 0.03));
    return vec4<f32>(scatter_color, alpha);
}

fn shade_body_layer(hit: CelestialHit, ray_origin: vec3<f32>, ray_dir: vec3<f32>, opaque_t: f32) -> CelestialLayer {
    if (hit.index < 0) {
        return CelestialLayer(vec3<f32>(0.0), 0.0);
    }

    let body = bodies[u32(hit.index)];
    let center = body.center_radius.xyz;
    let radius = body.center_radius.w;
    let atmo_radius = max(body.params.x, radius);
    let cloud_radius = max(body.params.y, radius);
    let emission = max(body.params.w, 0.0);
    let sun_intensity = max(camera.light_pos.w, 0.0) / 1.7;
    let body_light_dir = normalize(camera.light_pos.xyz - center);
    let disable_surface = body.flags.x > 0.5;
    let surface_occlusion_bias = max(body.flags.y, 0.0);

    var atmosphere_cap = hit.atmo_exit;
    if (opaque_t < 50000.0) {
        atmosphere_cap = min(atmosphere_cap, opaque_t);
    }
    let atmosphere = sample_atmosphere(body, ray_dir, body_light_dir, hit.atmo_entry, hit.atmo_exit, atmosphere_cap, radius, atmo_radius);

    var depth_cap = hit.atmo_exit;
    if (opaque_t < 50000.0) {
        depth_cap = min(depth_cap, opaque_t);
    }
    let front_clouds = sample_cloud_layer(body, ray_origin, ray_dir, center, cloud_radius, depth_cap);

    let surface_occluded = opaque_t < 50000.0 && (hit.surface_t < 0.0 || opaque_t < hit.surface_t + surface_occlusion_bias);
    if (hit.surface_t < 0.0 || disable_surface || surface_occluded) {
        let cloud_mix = mix(atmosphere.xyz, front_clouds.xyz, front_clouds.a);
        let cloud_alpha = saturate(atmosphere.a + front_clouds.a * (1.0 - atmosphere.a));
        return CelestialLayer(cloud_mix, cloud_alpha);
    }

    let hit_pos = ray_origin + ray_dir * hit.surface_t;
    let normal = normalize(hit_pos - center);
    let surface_view_dir = normalize(ray_origin - hit_pos);
    let surface_light_dir = normalize(camera.light_pos.xyz - hit_pos);
    var surface_color = sample_surface(body, normal, surface_light_dir, surface_view_dir, sun_intensity);

    if (front_clouds.a > 0.0) {
        surface_color *= 1.0 - front_clouds.a * 0.08;
        surface_color = mix(surface_color, front_clouds.xyz, front_clouds.a);
    }

    let shell_to_surface = max(hit.surface_t - hit.atmo_entry, 0.0);
    let shell_thickness = max(atmo_radius - radius, 1e-3);
    let haze = 1.0 - exp(-shell_to_surface / max(shell_thickness * 1.2, 1e-3));
    let horizon = pow(1.0 - saturate(dot(normal, surface_view_dir)), 2.8);
    let atmo_mix = saturate(atmosphere.a * 0.55 + haze * 0.22 + horizon * 0.2);
    let final_color = mix(surface_color + emission, surface_color * 0.94 + atmosphere.xyz + emission, atmo_mix);
    return CelestialLayer(saturate3(final_color), 1.0);
}

fn composite_layers(front: CelestialLayer, back: CelestialLayer) -> vec4<f32> {
    let alpha = saturate(front.alpha + back.alpha * (1.0 - front.alpha));
    if (alpha <= 1e-4) {
        return vec4<f32>(0.0);
    }

    let premul = front.color * front.alpha + back.color * back.alpha * (1.0 - front.alpha);
    return vec4<f32>(saturate3(premul / alpha), alpha);
}

@vertex
fn vs_main(@builtin(vertex_index) vertex_index: u32) -> VsOut {
    var positions = array<vec2<f32>, 3>(
        vec2<f32>(-1.0, -3.0),
        vec2<f32>(-1.0, 1.0),
        vec2<f32>(3.0, 1.0),
    );
    let pos = positions[vertex_index];
    var out: VsOut;
    out.position = vec4<f32>(pos, 0.0, 1.0);
    out.uv = pos * vec2<f32>(0.5, -0.5) + vec2<f32>(0.5, 0.5);
    return out;
}

@fragment
fn fs_main(in: VsOut) -> @location(0) vec4<f32> {
    if (body_params.body_count == 0u) {
        return vec4<f32>(0.0);
    }

    let dims = textureDimensions(in_depth);
    let ipos = vec2<i32>(
        i32(clamp(in.uv.x * f32(dims.x), 0.0, f32(dims.x - 1u))),
        i32(clamp(in.uv.y * f32(dims.y), 0.0, f32(dims.y - 1u))),
    );
    let opaque_t = textureLoad(in_depth, ipos, 0).r;

    let ray_origin = camera.cam_pos.xyz;
    let ray_dir = make_primary_ray(in.uv);

    let front_hit = find_nearest_hit(ray_origin, ray_dir, 0.0);
    if (front_hit.index < 0) {
        return vec4<f32>(0.0);
    }

    if (opaque_t < 50000.0 && opaque_t <= front_hit.atmo_entry) {
        return vec4<f32>(0.0);
    }

    let front_layer = shade_body_layer(front_hit, ray_origin, ray_dir, opaque_t);
    if (front_layer.alpha >= 0.999) {
        return vec4<f32>(front_layer.color, front_layer.alpha);
    }

    let back_hit = find_nearest_hit(ray_origin, ray_dir, front_hit.atmo_exit + 0.05);
    if (back_hit.index < 0) {
        return vec4<f32>(front_layer.color, front_layer.alpha);
    }
    if (opaque_t < 50000.0 && opaque_t <= back_hit.atmo_entry) {
        return vec4<f32>(front_layer.color, front_layer.alpha);
    }

    let back_layer = shade_body_layer(back_hit, ray_origin, ray_dir, opaque_t);
    return composite_layers(front_layer, back_layer);
}
