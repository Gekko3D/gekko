# G-Buffer Compute Shader (gbuffer.wgsl)

This document explains the responsibilities, inputs/outputs, and algorithm used by gbuffer.wgsl. The shader rasterizes (via compute) the voxel scene into a set of screen-space buffers that are later consumed by the deferred lighting pass.

Related:
- RENDERER.md — overall frame and resource pipeline
- DEFERRED_LIGHTING.md — how these outputs are consumed
- SHADOW_MAP.md — shadowing data used by lighting

## Responsibilities

- For each screen pixel:
  - Reconstruct the camera ray.
  - Intersect against the voxel scene acceleration structures.
  - Produce G-Buffer outputs:
    - Depth t (ray distance along view ray)
    - World-space normal
    - Material/PBR payload
    - World position (optional; see packing notes)
- Provide stable, consistent data for the lighting shader.

## Threading and Dispatch

- Workgroup size: 8×8 threads (tile-based).
- Dispatch: ( (Width+7)/8, (Height+7)/8, 1 ).

This size is a balance between occupancy and per-invocation memory traffic. See RENDERER.md for details.

## Bindings and Resources

The precise buffer layout is defined in code (see rt/gpu/manager.go and shaders.go), but conceptually:

Inputs:
- CameraData (uniform): view_proj, inv_view, inv_proj, camera position, debug flags.
- Scene buffers (read-only storage):
  - Voxel instances/sectors/bricks/BVH nodes (implementation-specific).
  - Materials table (PBR properties).
  - Lights are not required here (lighting is deferred).
- Samplers and other resources as needed for brick or atlas access (if applicable).

Outputs (storage/sampled textures created by BufferManager):
- Depth: RGBA32Float (sampled in later passes as UnfilterableFloat)
  - x = ray distance t from camera to intersection (linear distance along view ray)
  - y/z/w = reserved/unused (implementation-defined)
- Normal: RGBA16Float (sampled as Float)
  - xyz = world-space unit normal
  - w   = optional packing channel
- Material: RGBA32Float (sampled as UnfilterableFloat)
  - Contains PBR payload (e.g., base color, roughness, metalness, IOR, emission power)
  - Packing is implementation-defined and mirrored in the lighting shader
- Position: RGBA32Float (sampled as UnfilterableFloat)
  - World-space hit position (or compressed form)
  - If world position is not required by the lighting shader, this can be repurposed

Notes:
- The sample types must match the bind group layouts used by deferred_lighting.wgsl.
- Depth is stored as a linear ray distance “t” (not NDC z), which is convenient for ray-based comparisons (e.g., particles).

## Algorithm Outline

For each pixel (x,y):
1) Reconstruct camera ray direction
   - Use inv_proj and inv_view with the screen-space pixel center mapped to NDC.
   - dir = normalize(world_target - cam_pos).

2) Ray-scene intersection
   - Traverse voxel acceleration structure (e.g., BVH over brick sectors).
   - Perform brick/voxel intersection tests until the nearest hit is found or miss.

3) Surface shading payload (geometric only)
   - Compute world-space position: P = cam_pos + dir * t.
   - Fetch material parameters from material table using the voxel index/palette index.
   - Compute geometric normal (e.g., face normal from traversal or gradient from density if available).

4) Write G-Buffer targets
   - Depth.x = t.
   - Normal.rgb = world normal (unit vector).
   - Material.rgba = PBR payload (see DEFERRED_LIGHTING.md for how fields are interpreted).
   - Position.rgba = world position (or implementation-defined packing).

5) Miss handling
   - If no intersection, write sentinel values (e.g., t=+INF or 0) as expected by the lighting stage.
   - Lighting shader should skip shading on missing geometry.

## Packing Notes

- Depth is linear and stored as a float in RGBA32F.x to avoid precision artifacts and to support manual ray comparisons in later passes (e.g., particles).
- Normal is in RGBA16F to save bandwidth while preserving quality; ensure unit length and consistent orientation.
- Material packing should be kept in sync with the PBR evaluation in deferred lighting. Common fields:
  - baseColor (RGB in 0–1)
  - roughness (0–1)
  - metalness (0–1)
  - IOR and/or transparency
  - emissive intensity (as a separate channel or derived from palette)
- Position can be omitted if lighting only needs depth and normal; current pipeline retains it for flexibility.

## Edge Cases and Tuning

- Ray-voxel robustness: ensure robust traversal and epsilon handling to avoid self-intersections or missing thin features.
- Tile boundaries: guard accesses for tiles at image edges.
- Large scenes: use 32-bit floats for ray t and positions; consider culling sectors/bricks before fine intersection.
- Debug modes (optional):
  - Override outputs with visualizations (e.g., show normals, t, material index) to help diagnose issues.

## Interactions

- Deferred Lighting consumes all G-Buffer outputs and combines them with lights and shadows.
- Particles sample Depth.x and compare it against ray distance to emulate depth testing (see PARTICLES_BILLBOARD.md).
- Debug compute shader may visualize G-Buffers for inspection.

## Testing Tips

- Render normal visualization to validate correct orientation.
- Compare reconstructed world position against known geometry for accuracy.
- Validate material channel ranges (0–1 or physically plausible).
- Stress-test with small/highly-detailed bricks and with large camera motion.
