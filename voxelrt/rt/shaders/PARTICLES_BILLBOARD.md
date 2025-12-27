# Particles Billboard Render Shader (particles_billboard.wgsl)

This document explains the instanced billboard particle shader used to contribute transparency into the weighted blended OIT (WBOIT) accumulation targets. It covers inputs/outputs, vertex expansion, manual depth testing strategy, stability tweaks, weighting, and blending.

Related
- PARTICLES.md — CPU simulation, ECS components, GPU uploads, render pipeline
- RENDERER.md — frame graph and pass ordering (Accumulation → Resolve)
- shaders/GBUFFER.md — provides the depth “t” texture sampled here
- shaders/RESOLVE_TRANSPARENCY.md — resolves opaque + accumulated transparency into the swapchain

## Responsibilities

- Expand per-instance data into a camera-facing quad in the vertex stage.
- In the fragment stage:
  - Reconstruct the view ray for the current pixel.
  - Sample scene depth “t” from the G-Buffer depth texture.
  - Compute particle-ray distance “t_particle” and discard if behind scene.
  - Apply a circular soft mask.
  - Emit a depth-weighted contribution into two render targets (accum color and weight) for WBOIT.
- This pass is written into accumulation targets and does not write depth.

## Bindings and Data

Group 0 (Camera + Instances)
- binding(0): CameraData (uniform)
  - view_proj, inv_view, inv_proj
  - cam_pos, light_pos, ambient_color
  - debug_mode, render_mode (layout matches other passes)
- binding(1): instances (read-only storage)
  - ParticleInstance:
    - pos: vec3<f32>
    - size: f32
    - color: vec4<f32> (RGBA 0..1)

Group 1 (G-Buffer)
- binding(0): gbuf_depth: texture_2d<f32> (RGBA32Float sampled as UnfilterableFloat)
  - x = scene depth “t” (ray distance along camera ray)
  - y/z/w = reserved

Notes
- Sampling type must be UnfilterableFloat to match RGBA32F and app-side pipeline/BGL creation.
- The shader assumes “t” is measured along the camera ray as produced in the G-Buffer pass.

## Vertex Stage

Inputs
- Per-vertex: implicit vertex_index (0..5) to form two triangles (quad).
- Per-instance: instance_index (selects ParticleInstance).

Process
- Derive camera right/up from camera.inv_view (column 0 = right, column 1 = up).
- Define unit quad corners in CCW (-0.5,-0.5), (0.5,-0.5), (0.5,0.5), (-0.5,0.5) and emit two triangles.
- World position of a vertex:
  - world_pos = instance.pos + (right * corner.x + up * corner.y) * instance.size
- Outputs:
  - gl_Position = camera.view_proj * vec4(world_pos, 1)
  - color = instance.color
  - world_pos (optional, for masks)
  - quad_uv = corner + 0.5 (0..1 across quad)
  - world_center = instance.pos (used for depth test stability)

Rationale
- Billboard alignment via camera basis makes quads face the camera without per-particle rotation.
- world_center provides a stable reference for depth comparison.

## Fragment Stage

Inputs
- Builtin position (screen-space)
- VS outputs (color, world_pos, quad_uv, world_center)

Steps
1) Clamp pixel coords for depth fetch
   - px = clamp(floor(gl_FragCoord.x), [0..W-1])
   - py = clamp(floor(gl_FragCoord.y), [0..H-1])
   - scene = textureLoad(gbuf_depth, ivec2(px,py), 0)
   - t_scene = scene.x

2) Reconstruct camera ray direction for this pixel
   - Convert (px,py) to NDC, to view via inv_proj, to world via inv_view:
     - dir = normalize(world_target - camera.cam_pos)

3) Compute particle-ray distance
   - t_particle = dot(world_center - cam_pos, dir)

4) Manual depth test (with epsilon)
   - if t_particle > t_scene - epsilon: discard
   - epsilon defaults to 3e-3 (tuned to reduce grazing-angle popping)

5) Circular soft mask
   - d = length(quad_uv - 0.5) * 2.0
   - mask = 1.0 - smoothstep(0.8, 1.0, d)

6) Weighted blended OIT outputs (two RTs)
   - alpha = clamp(color.a * mask, 0..1)
   - z = clamp(t_particle / max(t_scene, 1e-4), 0..1)
   - k ≈ 8.0
   - w = max(1e-3, alpha) * pow(1.0 - z, k)
   - accum.rgb += color.rgb * alpha * w
   - accum.a   += alpha * w
   - weight    += alpha * w

FSOut
- RT[0]: accum (RGBA16F) — rgb accumulates premultiplied color; a accumulates alpha*w
- RT[1]: weight (R16F) — accumulates alpha*w

## Blending and Ordering

- Both color attachments use additive blending:
  - SrcFactor = One, DstFactor = One, Operation = Add
- Drawn in the Accumulation pass together with the Transparent Overlay pass.
- Final color is produced in the Resolve Transparency pass by:
  - transp = accum.rgb / max(weight, 1e-5)
  - out = saturate(opaque + transp)

## Stability Tweaks Implemented

- Use world_center to compute t_particle instead of per-vertex position.
- Clamp pixel coords before textureLoad to avoid sampling outside image bounds.
- Larger epsilon (3e-3) to reduce popping at grazing angles.
- Mask uses smoothstep for soft edge.

## Tuning

- Edge softness: adjust smoothstep range (e.g., 0.75..1.0 for softer).
- Epsilon: increase to 5e-3 or 1e-2 if popping persists at extreme angles.
- Weight exponent k: larger k biases contributions toward front-most surfaces.
- Instance.size: controls quad footprint; larger sizes help visibility for smoke-like effects.

## Common Issues

- “Particles disappear at certain angles”
  - Increase epsilon; ensure dir reconstruction matches the G-Buffer ray definition.
  - Confirm BGL1 is UnfilterableFloat for RGBA32F depth.
- “No particles after refactor”
  - Ensure TransparentAccum/Weight textures exist and are bound.
  - Confirm the Resolve pass is wired with opaque/accum/weight textures.
- “Halos or hard cutouts”
  - Widen the smoothstep range and/or reduce alpha.
  - Consider soft particles (fade with |t_scene - t_particle|).

## Future Work

- Soft particles: depth-delta based fade width.
- Velocity-based anisotropic stretch (streaked billboards).
- Texture atlas and animated flipbooks for diverse looks.
- Per-particle normal approximation for anisotropic highlights.
