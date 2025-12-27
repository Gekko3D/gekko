# Debug Compute Shader (debug.wgsl)

This document describes the compute debug shader used to visualize acceleration structures and light gizmos into the intermediate storage texture.

Related
- RENDERER.md — pass ordering and where the debug pass fits
- shaders/GBUFFER.md — main geometry pass
- shaders/DEFERRED_LIGHTING.md — lighting pass that normally writes to the storage texture

## Purpose

- Provides on-screen visualization to inspect:
  - Top-level BVH node bounds (internal nodes vs leaves)
  - Per-instance AABBs (world/object space)
  - Light gizmos (spheres for point/spot lights)
- Writes directly to the same storage texture as deferred lighting. When enabled, it can override the lit image for debugging.

## Bindings and Data

Group 0
- binding(0): CameraData (uniform)
  - view_proj, inv_view, inv_proj, cam_pos, ambient/light/debug fields (same layout used by other passes)
- binding(1): instances — array<Instance> (read-only storage)
  - Instance contains world/object transforms, world AABB, local AABB, object_id, padding
- binding(2): nodes — array<BVHNode> (read-only storage)
  - BVH node bounds and child/leaf data
- binding(3): lights — array<Light> (read-only storage)
  - Light params (position, direction, color, params)

Group 1
- binding(0): out_tex — texture_storage_2d<rgba8unorm, write>
  - The debug shader writes colors here per-pixel

Notes
- The shader may reference bindings in a “dummy” manner to prevent the compiler from stripping them when conditionally unused.

## Ray Setup

- Per compute invocation (tile 8×8), convert global_id.xy to UV in [0,1].
- Build a ray from camera through that pixel:
  - ndc = (2*uv-1, flip Y)
  - view = inv_proj * clip; world = inv_view * view
  - origin = camera.cam_pos.xyz; dir = normalize(world - origin)
- inv_dir = 1/dir to accelerate AABB slabs test.

## Visualization Logic

- Draw lights as spheres:
  - Iterate lights; for point/spot (type != Directional):
    - Solve ray-sphere intersection with small radius (e.g., 0.5).
    - If hit, set debug_color to light color (scaled for spot).

- Traverse top-level BVH:
  - Non-recursive stack (array<i32,64>) over nodes[].
  - For each node AABB hit:
    - If internal, draw orange-yellow edges when near AABB boundary.
    - If leaf:
      - Draw world AABB edges in cyan when near boundary.
      - Also draw object-space local AABB (magenta) after transforming the ray into object space.

Edge detection
- Compute distance to nearest face of AABB at first intersection t_min.
- If two or more axes are within a small thickness (scaled by distance), treat as an edge and set debug_color.

Write-out
- If any debug element was “hit” closer than the current closest_t_debug, write debug_color to out_tex at global_id.xy.

## Workgroup and Dispatch

- @workgroup_size(8,8,1)
- Dispatched once across the screen (no Z dimension)

## Interactions

- In rt/app/app.go, the debug pass is optional (guarded by a DebugMode). When enabled, it runs after deferred lighting and writes to the same storage texture used by the fullscreen blit, allowing you to inspect scene structures.
- The pass does not depend on G-Buffer outputs; it reconstructs rays directly from camera matrices.

## Tuning and Notes

- Thickness for edge detection scales with distance to keep line thickness visually stable.
- Leaf/internal colors are distinct for readability.
- The shader avoids altering pixels when nothing is hit to leave the underlying image untouched, but in practice it writes when hit_debug = true; configure usage to suit your debug workflow.

## Future Ideas

- Toggle modes via camera.debug_mode to visualize:
  - BVH depth heatmap
  - Per-node occupancy or instance IDs
  - Object wireframes and normals
- Add a separate output target instead of overwriting the deferred image (side-by-side compare).
