# Raytrace Shader Documentation (`raytrace.wgsl`)

This document details the internal logic of the WGSL compute shader used in VoxelRT.

## Entry Point

*   **`main(global_id)`**:
    1.  Calculates UV coordinates from `global_id`.
    2.  Generates a primary ray using `get_ray()`.
    3.  Traverses the Top-Level Acceleration Structure (TLAS) to find intersected instances.
    4.  Traverses the voxel structure of the closest instance.
    5.  Calculates lighting and shadows if a hit occurs.
    6.  Writes the result to `out_tex`.

## Structures

### `HitResult`
Stores information about a ray-voxel intersection.
```wgsl
struct HitResult {
    hit: bool,              // Did we hit anything?
    t: f32,                 // Distance along ray
    palette_idx: u32,       // Voxel value (0-255)
    material_base: u32,     // Offset into material buffer
    normal: vec3<f32>,      // World-space normal
    voxel_center_ws: vec3<f32>, // World-space center of the hit voxel
};
```

### `SectorRecord` & `BrickRecord`
GPU representations of the sparse grid nodes.
*   **Sector**: `brick_mask_lo/hi` (64 bits) indicates which of the 64 bricks exist.
*   **Brick**: `occupancy_mask_lo/hi` (64 bits) indicates which 2x2x2 micro-blocks exist. `atlas_offset` points to raw voxel data.

## Traversal Logic (`traverse_xbrickmap`)

The traversal uses a **Hierarchical DDA (Digital Differential Analyzer)** algorithm.

1.  **Sector Phase (Coarse)**:
    *   Steps through the grid in 32-unit increments.
    *   Uses `find_sector_cached` to resolve the current coordinate to a Sector ID.
    *   If a Sector is found, enters the Brick Phase.

2.  **Brick Phase (Medium)**:
    *   Steps through the Sector in 8-unit increments.
    *   Checks the `brick_mask`.
    *   If a Brick bit is set, retrieves the `BrickRecord` and enters the Micro Phase.

3.  **Micro/Voxel Phase (Fine)**:
    *   **Solid Optimization**: Checks `brick.flags`. If `BRICK_FLAG_SOLID`, returns a Hit immediately with the brick's uniform value.
    *   **Micro Check**: Steps in 2-unit increments (logical check). Uses `occupancy_mask` to verify if the 2x2x2 block has any voxels.
    *   **Voxel Check**: If Micro bit is set, iterates the 8 voxels within that micro-block.
    *   **Payload Fetch**: Reads the `uint8` palette index from `voxel_payload` buffer.
    *   If `val != 0`, calculates normal and returns Hit.

## Normal Calculation (`estimate_normal`)

Normals are calculated dynamically upon impact to give a smooth or blocky appearance.
*   **Method**: Central Difference Gradient.
*   Samples occupancy of neighbors (`x+1`, `x-1`, etc.).
*   Computes gradient vector `(dx, dy, dz)`.
*   Result is normalized.
*   *Note*: For solid bricks or distinct voxels, this can produce rounded edges. The current implementation forces strict axis-aligned normals for specific blocky aesthetics in some paths (e.g. `HitResult` construction).

## Lighting & Shadowing

### Shadowing (`check_visibility`)
*   **Bias**: To prevent self-shadowing (acne), especially with the "blocky" aesthetic, the shadow ray origin is pushed significantly away from the surface:
    ```wgsl
    let bias_origin = hit_pos_ws + normal * 0.6;
    ```
    This ensures the ray starts outside the current voxel.
*   **Traversal**: Shadow rays traverse the same TLAS and voxel structures but terminate immediately upon *any* hit (`hit && t < light_dist`).

### Shading Model (`calculate_lighting`)
A simplified PBR model:
*   **Diffuse**: Lambertian (`N dot L`).
*   **Specular**: GGX-style approximation.
*   **Attenuation**: Distance squared falloff with smooth windowing for point lights.
*   **Cone**: Spot light angular falloff.

## Buffers

*   **Group 0 (Scene)**: Camera, Instances, BVH, Lights.
*   **Group 1 (Output)**: Storage Texture.
*   **Group 2 (Voxel Data)**: 
    *   `sectors`, `bricks`, `voxel_payload`: The sparse grid hierarchy.
    *   `materials`: PBR material parameters.
    *   `object_params`: Per-object offsets into the big buffers.
