# Shadow Map Compute Shader (shadow_map.wgsl)

This document describes the responsibilities, inputs/outputs, resource bindings, traversal algorithm, and output encoding used by the shadow map compute shader.

Related
- RENDERER.md — pipeline overview and pass ordering
- shaders/GBUFFER.md — geometry pass that provides inputs to lighting
- shaders/DEFERRED_LIGHTING.md — consumes shadow maps for shading

## Responsibilities

- For each light layer and for each pixel in the shadow map:
  - Generate a ray in light space that corresponds to that pixel.
  - Traverse the voxel scene to find the first occluder.
  - Encode a shadow depth per pixel into a 2D array texture.

The shader runs entirely in compute and writes results into an array texture, one layer per light.

## Threading and Dispatch

- Workgroup size: 8×8×1 threads.
- The Z dimension of the dispatch encodes the light index (array layer).
- For each invocation:
  - global_id.xy is pixel coord
  - global_id.z is light index

## Bindings and Resources

Group 0 — Scene acceleration and light data
- binding(1): instances — array<Instance> (read-only storage)
- binding(2): nodes — array<BVHNode> (read-only storage)
- binding(3): lights — array<Light> (read-only storage)

Key struct fields
- Light:
  - position, direction, color, params (x=range, y=cosCone, z=type)
  - view_proj, inv_view_proj — for raster-to-ray conversion per light
- Instance:
  - object_to_world, world_to_object
  - aabb_min/max (world)
  - local_aabb_min/max (object)
  - object_id (+ padding)
- BVHNode: binary BVH over instances with child indices and leaf ranges

Group 1 — Output
- binding(0): out_shadow_map — texture_storage_2d_array<rgba32float, write>

Group 2 — Voxel data (read-only storage)
- binding(0): sectors — sector table per object
- binding(1): bricks — metadata (occupancy masks, flags, atlas offsets)
- binding(2): voxel_payload — byte-addressable payload (palette indices)
- binding(4): object_params — per-object table bases and counts
- binding(5): tree64_nodes — optional hierarchical occupancy (Tree64)
- binding(6): sector_grid — hashing/lookup for sparse sector grid
- binding(7): sector_grid_params — parameters for the sparse grid

Notes
- The shader currently forces a dummy access to tree64_nodes to ensure the binding remains live, even when traverse_tree64 is not called (prevents layout mismatch).

## Pixel-to-Ray (Light-space)

For each texel (x,y) in the shadow map layer for light i:
1) Compute UV = (x+0.5, y+0.5) / dim
2) Convert to NDC: ndc = (2*uv-1, with flipped Y)
3) Reconstruct two clip-space points: near(-1), far(+1)
4) Multiply by inv_view_proj to get world-space positions
5) Ray:
   - origin = near.xyz/near.w
   - dir = normalize(far.xyz/far.w - origin)
   - inv_dir = 1/safe_dir (with epsilon to avoid division by 0)

This produces a world-space ray for the specific light's projection.

## Traversal Overview

- Top-level BVH traversal (array nodes):
  - Intersect ray with node AABBs.
  - If hit, either push children or test leaf instances.

- Per-instance intersection:
  - Intersect ray with instance AABB.
  - If hit, traverse the object's voxel representation:
    - traverse_xbrickmap: 3-level grid (sectors → bricks → micro-voxels)
      - Sectors: 32^3 world blocks, sparse via sector_grid hash table
      - Bricks: 8^3 blocks inside each sector; presence via 64-bit masks
      - Micro-voxels: 2^3 groups (occupancy masks) and 8^3 actual voxels sampled from payload atlas; early-out on first solid voxel (palette != 0)
    - traverse_tree64: optional hierarchical Tree64 (disabled in this build for shadows to avoid LOD popping; xbrickmap forced for stable hard shadows)

- The first hit along the ray becomes the occluder.

Implementation details
- step_to_next_cell computes parametric t to cross next grid boundary with an EPS bias.
- find_sector_cached caches last sector lookup to reduce hash probes on coherent rays.

## Output Encoding

- If hit:
  - Directional lights: store light-space NDC depth in [-1,1] at the hit center
    - depth = clamp((view_proj * vec4(voxel_center_ws,1)).z / w, -1, 1)
  - Spot lights: store linear distance from light to hit center (meters)
    - depth_m = distance(light.position.xyz, hit_pos)
- If miss:
  - Directional: store 1.0 (far)
  - Spot: store light.params.x (range) as “far” distance

All values are written to out_shadow_map as vec4(depth, 0, 0, 0).

Rationale
- Directional: NDC depth allows a normalized compare in the lighting pass.
- Spot: linear meters cooperate better with range-limited radial shadows.

## Tuning and Stability

- Shadows are computed against the voxel center for blocky/stable hard edges.
- LOD choice: shadow pass forces full xbrickmap traversal (no Tree64 LOD) to reduce flicker/popping.
- EPS values:
  - Used in step_to_next_cell and AABB entry to avoid self-intersections and zero-division.
- Iteration caps:
  - Bounds the maximum steps through sectors/bricks/voxels for safety.

## Interactions

- Deferred lighting samples the shadow maps per light:
  - Directional: transform surface to light space; compare NDC z with stored value (with bias/PCF).
  - Spot: compare linear distance from light; threshold by stored depth_m (with bias/PCF).
- The renderer configures a 2D array texture sized to the desired resolution and number of lights.

## Debugging Tips

- Visualize a single shadow layer by blitting the array slice (separate debug path).
- Inject high-intensity test lights to validate range and cone logic.
- Validate the light view_proj matrices: project known points and check continuity.

## Future Improvements

- Percentage-closer filtering (PCF) or PCSS in lighting pass to soften edges.
- Depth packing optimizations (e.g., R32F or R16F) to reduce bandwidth.
- Light-type-specific atlas layouts and varying resolutions per light.
- Re-enable and tune Tree64 traversal for shadow maps with stable LOD (possibly cone tracing with conservative tests).
