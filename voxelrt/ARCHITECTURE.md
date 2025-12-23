# VoxelRT Implementation Documentation

## Overview

VoxelRT is a GPU-accelerated voxel rendering engine written in Go and WGSL (WebGPU). It uses a sparse voxel octree/grid hybrid approach to efficiently store and render large fully-volumetric scenes with real-time editing capabilities.

## Architecture

The system consists of two main parts:
1.  **CPU Host (Go)**: Manages scene graph, voxel data structures, editing logic, and GPU buffer management.
2.  **GPU Device (WGSL)**: Performs raytracing, traversal, and shading in a compute shader.

## Data Structures

The core data structure is a multi-level sparse grid designed for efficiency and compressibility.

### Hierarchy

1.  **XBrickMap (Sparse Grid)**
    *   **Description**: The top-level container for a voxel object. It is a hash map of **Sectors**.
    *   **Key**: `[3]int` containing sector coordinates.
    *   **Storage**: CPU `map`, GPU `SectorTableBuf`.

2.  **Sector (32³ Voxels)**
    *   **Description**: A 32x32x32 region of space.
    *   **Structure**: 
        *   `PackedBricks`: A list of non-empty bricks.
        *   `BrickMask64`: A 64-bit integer where each bit represents the presence of a Brick (4x4x4 bricks per sector).
    *   **Purpose**: Coarse culling of empty space.

3.  **Brick (8³ Voxels)**
    *   **Description**: An 8x8x8 chunk of voxels.
    *   **Structure**:
        *   `OccupancyMask64`: A 64-bit integer where each bit represents a 2x2x2 "micro-block".
        *   `Payload`: An 8x8x8 array of `uint8` (palette indices).
        *   `Flags`: Meta-information (e.g., `BrickFlagSolid`).
        *   `AtlasOffset`: Pointer to the payload data in the GPU buffer.

### Optimizations

*   **Solid Bricks**: If all voxels in a brick have the same value, the `BrickFlagSolid` is set. The `AtlasOffset` then stores the voxel value directly instead of an offset. This skips payload storage and memory fetch, allowing for significant compression of large uniform areas (like walls or floors).
*   **Bitmasks**: 64-bit masks allow checking for empty sub-regions (bricks or micro-blocks) with a single bitwise operation, speeding up traversal.

## Editing Mechanism

Editing is performed on the CPU references and then synchronized to the GPU.

1.  **CPU Update**: 
    *   `XBrickMap.SetVoxel(x, y, z, val)` locates the Sector and Brick.
    *   **Insertion**: If a voxel is added in an empty area, new Sectors/Bricks are allocated. The new Brick requires a slot in the GPU Atlas.
    *   **Deletion**: If a voxel is removed, masks are updated. If a Brick/Sector becomes empty, it is removed to save memory.
    *   **Solidification**: When a brick becomes full/uniform, `TryCompress()` checks if it can be converted to a Solid Brick. If so, its Atlas slot is freed.
    *   **Dirty Flags**: Modified sectors/bricks are marked dirty (currently, the `GpuBufferManager` often rebuilds the full structure for simplicity).

2.  **GPU Synchronization (`GpuBufferManager`)**:
    *   Iterates over the scene objects.
    *   Serializes `Sectors` to `SectorTableBuf`.
    *   Serializes `Bricks` to `BrickTableBuf`.
    *   Serializes `Payload` to `VoxelPayloadBuf` (linear implementation).
    *   Updates `ObjectParams` with new offsets.

## Lighting System

VoxelRT uses a PBR-lite lighting model with support for multiple dynamic lights.

*   **Light Types**:
    1.  **Directional**: Infinite distance (Sun).
    2.  **Point**: Local source with falloff.
    3.  **Spot**: Cone-restricted point light.
*   **Data Structure**:
    *   `Light` struct: Position, Direction, Color (RGB + Intensity), Params (Range, Angle).
    *   Stored in a storage buffer array.

## Material System

Materials are defined locally in a `MaterialTable` per object, indexed by the voxel's palette value.

*   **Properties**:
    *   `BaseColor` (RGBA)
    *   `Emissive` (RGBA)
    *   `Roughness` (float)
    *   `Metalness` (float)
    *   `IOR` (float)
    *   `Transparency` (float)
*   **Lookup**: `PaletteIndex` -> `MaterialTable[Index]`.

## Shading & Rendering

Shading is performed in the compute shader (`raytrace.wgsl`).

1.  **Ray Generation**: Primary rays generated from camera data.
2.  **TLAS Traversal**: Intersection with object AABBs.
3.  **Voxel Traversal**: Hierarchical DDA through Sector -> Brick -> Micro -> Voxel.
4.  **Hit Calculation**: Returns `HitResult` (t, normal, material, position).
5.  **Shading Loop**:
    *   Iterates all lights.
    *   **Shadows**: Casts a ray from `hit_pos + normal * bias` towards light. A large bias (0.6) is used to align shadows with voxel faces ("blocky" look).
    *   **BRDF**: Cook-Torrance speculative approximation + Lambertian diffuse.
6.  **Post-Processing**: (Currently minimal) Color output to texture.

## Optimizations Summary

1.  **Hierarchical Traversal**: Skips large empty spaces (32³ then 8³ blocks).
2.  **Bitmask Culling**: Fast rejection of empty 2³ micro-blocks.
3.  **Solid Brick Compression**: Massive memory saving for uniform volumes; skips memory bandwidth for payload.
4.  **Cached Traversal**: Shader caches the last accessed Sector ID to avoid frequent hash/linear searches during stepping.
