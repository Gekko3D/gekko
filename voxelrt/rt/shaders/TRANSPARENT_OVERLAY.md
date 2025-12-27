# Transparent Overlay Shader (transparent_overlay.wgsl)

Purpose
- Fullscreen transparency pass that raycasts the nearest transparent voxel surface per pixel up to the opaque limit from the G-Buffer depth “t”.
- Shades the hit with direct lighting and emits a weighted contribution into the WBOIT accumulation targets (accum color and weight).
- Complements particles_billboard.wgsl; both write into the same accumulation targets and are later resolved by resolve_transparency.wgsl.

Related
- RENDERER.md — updated frame graph and pass ordering (Accumulation → Resolve)
- PARTICLES.md — WBOIT accumulation and resolve notes shared with this pass
- shaders/RESOLVE_TRANSPARENCY.md — final composite of opaque + transparency
- shaders/GBUFFER.md — definitions for G-Buffer outputs including depth “t”
- rt/app/app.go — setupTransparentOverlayPipeline, accumulation and resolve passes
- rt/gpu/manager.go — CreateTransparentOverlayBindGroups, gbuffer/transparency textures

Outputs (WBOIT)
- RT[0] accum: RGBA16Float
  - rgb: accumulates premultiplied color = color.rgb * alpha * w
  - a: accumulates alpha * w
- RT[1] weight: R16Float
  - accumulates alpha * w
- Weighting (same as particles):
  - z = clamp(t_hit / max(t_opaque, 1e-4), 0..1)
  - k ≈ 8.0
  - w = max(1e-3, alpha) * pow(1.0 - z, k)

Pass ordering
- Runs in the Accumulation render pass together with Particles (billboards).
- Resolve pass composites opaque + accum/weight onto the swapchain, then Text overlays.

Bindings and Data

Group 0 (fragment — scene-level)
- binding(0): CameraData (uniform)
  - view_proj, inv_view, inv_proj
  - cam_pos, light_pos, ambient_color
  - debug_mode, render_mode (layout shared across passes)
- binding(1): instances (read-only storage) — voxel instances (Instance)
- binding(2): nodes (read-only storage) — top-level BVH nodes (BVHNode)
- binding(3): lights (read-only storage) — array<Light> with params and matrices

Group 1 (fragment — voxel data)
- binding(0): sectors (read-only storage) — array<SectorRecord>
- binding(1): bricks (read-only storage) — array<BrickRecord> (occupancy, flags, atlas offsets)
- binding(2): voxel_payload (read-only storage) — array<atomic<u32>> packed 8-bit voxels
- binding(3): materials (read-only storage) — array<vec4<f32>> material table (base/emissive/pbr/…)
- binding(4): object_params (read-only storage) — array<ObjectParams> (bases, counts)
- binding(5): tree64_nodes (read-only storage) — reserved for future octree/Tree64
- binding(6): sector_grid (read-only storage) — hash grid mapping sector coords to sector indices
- binding(7): sector_grid_params (read-only storage) — grid_size/mask etc.

Group 2 (fragment — G-Buffer inputs)
- binding(0): in_depth: texture_2d<f32> — RGBA32Float (UnfilterableFloat) t in .r
- binding(1): in_material: texture_2d<f32> — RGBA32Float (UnfilterableFloat), reserved for future use

Pipeline and targets (rt/app/app.go)
- setupTransparentOverlayPipeline builds a fullscreen triangle pipeline:
  - Color targets:
    - [0] RGBA16Float with additive blend (src=one, dst=one)
    - [1] R16Float with additive blend (src=one, dst=one)
- G-Buffer depth and material bound via BGL2; material currently not required but reserved.

Algorithm Overview

1) Opaque limit
- Fetch per-pixel t_opaque from in_depth.r via integer texel coords:
  - ipos = clamp(floor(pixel), [0..dims-1])
  - t_limit = min(t_opaque, FAR_T)

2) Build camera ray (screen → world)
- Derive UV from integer pixel coords (center of the pixel).
- Convert to NDC, then to view via inv_proj, then to world via inv_view:
  - dir = normalize(world_target - camera.cam_pos)

3) Top-level BVH traversal
- Use an explicit small stack (e.g., 64) to traverse BVHNode hierarchy.
- For each node intersected before min(current_hit.t, t_limit):
  - If leaf: iterate instances and intersect their world-space AABBs; for candidates, test inside instance (object space traversal).
  - Else: push children.

4) Per-instance transparent raymarch (object space)
- Transform ray to object space using instance.world_to_object.
- Clip to local AABB; early-exit if empty.
- Step through Sector → Brick → Micro-voxel hierarchy with 3D-DDA:
  - Sector stepping: 32^3 space aligned grid; use sector hash grid for O(1) lookup.
  - Brick stepping: 8^3 bricks per 32^3 sector; occupancy masks for early rejection.
  - Micro-voxel stepping: for non-solid bricks, step 1^3 voxels; check palette indices via voxel_payload.
- First transparent hit when:
  - Solid brick flagged as transparent (material pbr.w > 0), or
  - Micro-voxel palette_idx != 0 and material at that palette has alpha > threshold.

5) Shading
- Simple direct lighting (no shadows) + ambient + emissive:
  - Color = base_color * ambient + emissive + sum_lights(diffuse+spec)
  - Light types: directional, point, spot (see WGSL for details)
- Compute world-space normal:
  - Estimate via occupancy gradient in object space; transform to world with inverse-transpose.

6) WBOIT contribution
- Depth-normalized z = t_hit / t_opaque; compute weight w with exponent k.
- Write:
  - accum.rgb += color * alpha * w
  - accum.a   += alpha * w
  - weight    += alpha * w

Stability details present in WGSL
- Safe ray directions (avoid div-by-zero) via make_safe_dir
- clamped integer pixel fetch for in_depth
- small EPS bias for stepping across grid boundaries

Key Structures (abbreviated)
- CameraData, Instance, BVHNode, SectorRecord, BrickRecord, Light, ObjectParams
- SectorGridEntry/grid params for cached hash-grid lookup (find_sector_cached)

Tuning
- Transparency threshold: minimum alpha to consider a transparent contribution (pbr.w).
- Weight exponent k: increases front-weighting of contributions; default ≈ 8.
- Epsilon for grid stepping and depth tests to mitigate popping/precision issues.
- Lighting multipliers (attenuation, emissive scaling) to fit scene scale.

Troubleshooting
- “No transparency visible”
  - Ensure TransparentAccum/Weight textures and views exist (BufferManager.CreateGBufferTextures).
  - Verify overlay pipeline is bound and draws a fullscreen triangle.
  - Check that ResolveBG binds opaque/accum/weight/sampler and resolve pass runs.
- “Seams or popping at edges”
  - Increase EPS used in stepping and/or normal estimation radius.
  - Validate integer pixel coords for depth fetch and consistent camera ray reconstruction.
- “Overly bright results”
  - Reduce emissive or ambient; adjust weight exponent k or clamp inputs before accumulation.

Notes
- This pass focuses on the first transparent surface along the ray prior to opaque t. Complex stacks of transparent volumes rely on the WBOIT approximation rather than per-fragment sorting.
- in_material binding is currently reserved for future material-aware decisions (e.g., refractive flags or soft blending hints).

Future Work
- Soft transparency thickness using depth delta near opaque surface (soft “overlays”).
- Shadowed transparency and ambient occlusion heuristics.
- Material-driven scattering or refractive approximations (screen-space).
