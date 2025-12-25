# Particles Billboard Render Shader (particles_billboard.wgsl)

This document explains the instanced billboard particle shader used to composite particles on top of the deferred-lit image. It covers inputs/outputs, vertex expansion, manual depth testing strategy, stability tweaks, and blending.

Related
- PARTICLES.md — CPU simulation, ECS components, GPU uploads
- RENDERER.md — pass ordering (drawn after deferred lighting + blit)
- shaders/GBUFFER.md — provides the depth “t” texture sampled here

## Responsibilities

- Expand per-instance data into a camera-facing quad in the vertex stage.
- In the fragment stage:
  - Reconstruct the view ray for the current pixel.
  - Sample scene depth “t” from the G-Buffer depth texture.
  - Compute particle-ray distance “t_particle” and discard if behind scene.
  - Apply a circular soft mask.
  - Output premultiplied color using additive blending.

This pass is purely additive and does not write depth.

## Bindings and Data

Group 0 (Camera + Instances)
- binding(0): CameraData (uniform)
  - view_proj, inv_view, inv_proj
  - cam_pos, light_pos, ambient_color
  - debug_mode, render_mode (not used here but in the same layout as other passes)
- binding(1): instances (read-only storage)
  - ParticleInstance:
    - pos: vec3<f32>
    - size: f32
    - color: vec4<f32> (RGBA 0..1)

Group 1 (G-Buffer)
- binding(0): gbuf_depth: texture_2d<f32> (RGBA32Float sampled as UnfilterableFloat)
  - x = scene depth “t” (ray distance along camera ray)
  - y/z/w = reserved

Note
- Sampling type must be UnfilterableFloat to match RGBA32F and app-side pipeline/BGL creation.

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
  - world_pos (used for local mask computations)
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
   - Use a helper that converts (px,py) to NDC, then to view space via inv_proj, then to world via inv_view:
     - dir = normalize(world_target - camera.cam_pos)

3) Compute particle-ray distance
   - t_particle = dot(world_center - cam_pos, dir)

4) Manual depth test (with epsilon)
   - if t_particle > t_scene - epsilon: discard
   - epsilon defaults to 3e-3 in this build (tuned to reduce grazing-angle popping)

5) Circular soft mask
   - d = length(quad_uv - 0.5) * 2.0
   - mask = 1.0 - smoothstep(0.8, 1.0, d)

6) Premultiply and output
   - alpha = color.a * mask
   - rgb = color.rgb * alpha
   - return vec4(rgb, 0.0)  // additive blending: src=one, dst=one

Notes
- Alpha is zero; blending uses additive contributions from rgb.
- Stability improvements (implemented):
  - world_center for t_particle instead of per-corner world_pos
  - clamped pixel coords for textureLoad to avoid edge/rounding artifacts
  - larger epsilon (3e-3) for grazing angles

## Blending and Ordering

- Pipeline uses additive blending for both color and alpha channels:
  - SrcFactor = One, DstFactor = One, Operation = Add
- Rendered after fullscreen blit and before text (see RENDERER.md).
- Depth is evaluated manually; the swapchain depth buffer is not used.

## Tuning

- Edge softness: adjust smoothstep range (e.g., 0.75..1.0 for softer)
- Epsilon: increase to 5e-3 or 1e-2 if popping persists at extreme angles
- Size: instance.size controls billboard footprint; larger sizes help visibility
- Color: clamp smoke colors below 1.0 to avoid overly hot additive accumulation

## Common Issues

- “Particles disappear at certain angles”
  - Increase epsilon slightly; ensure dir reconstruction is consistent with G-Buffer’s ray definition.
  - Confirm Group 1 uses UnfilterableFloat for RGBA32F depth; filtering types can cause sampling artifacts.

- “Halos or hard cutouts”
  - Widen smoothstep and reduce per-pixel alpha to soften edges.
  - Consider soft particles (depth-aware fade based on |t_scene - t_particle|).

## Future Work

- Soft particles: fade width derived from depth delta or a tuned thickness.
- Velocity-based anisotropic stretch (streaked billboards).
- Texture atlas and animated flipbooks for diverse particle appearances.
- Weighted blended OIT variant to support non-additive particles without sorting.
