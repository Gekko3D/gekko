# Hi-Z (Hierarchical Z-Buffer) Shader

Location: rt/shaders/hiz.wgsl

## Purpose

The Hi-Z shader generates a hierarchical depth buffer (mip pyramid) from the G-Buffer depth texture. This pyramid is used for CPU-side occlusion culling to skip rendering objects that are completely hidden behind other geometry.

## Algorithm

The shader performs a simple 2×2 MAX reduction:

1. For each pixel in the destination mip level, sample 4 pixels from the source (2×2 block).
2. Take the maximum depth value of the 4 samples.
3. Write to the destination mip level.

Using MAX reduction ensures conservative culling: if an object's nearest point is farther than the maximum depth in the Hi-Z tile, it is definitely occluded.

## Bindings

| Group | Binding | Type | Description |
|-------|---------|------|-------------|
| 0 | 0 | texture_2d\<f32\> | Source texture (G-Buffer depth or previous mip) |
| 0 | 1 | texture_storage_2d\<r32float, write\> | Destination mip level |

## Workgroup Size

- 8×8×1 threads per workgroup
- Dispatch: `((destWidth + 7) / 8, (destHeight + 7) / 8, 1)`

## Depth Format

The Hi-Z buffer stores linear ray distance (same as G-Buffer depth), not normalized clip-space depth:

- Near: small values (e.g., 1.0)
- Far/Empty: large values (e.g., 60000.0)

This matches the G-Buffer depth output from `gbuffer.wgsl` which stores `hit_res.t` (ray distance).

## Boundary Handling

Out-of-bounds samples are treated as 60000.0 (far distance) to avoid false occlusion:

```wgsl
var val00 = 60000.0;
if (sx < srcSize.x && sy < srcSize.y) {
    val00 = textureLoad(sourceTexture, vec2<u32>(sx, sy), 0).x;
}
```

## Usage

The Hi-Z pipeline is dispatched after the G-Buffer pass:

1. **Pass 0**: G-Buffer Depth → Hi-Z Mip 0 (2:1 downsample)
2. **Pass 1..N**: Hi-Z Mip K → Hi-Z Mip K+1 (2:1 downsample)

A low-resolution mip (~64 pixels wide) is read back to CPU for occlusion testing.

## Related Files

- **rt/gpu/manager_hiz.go**: Hi-Z texture setup, mip chain dispatch, async readback
- **rt/core/scene.go**: `IsOccluded()` - CPU-side occlusion test using Hi-Z data
- **rt/app/app.go**: Integration into the render loop

## Occlusion Test Logic

On CPU, the occlusion test works as follows:

```
if (object_min_depth > tile_max_depth) {
    // Object is fully behind all surfaces in this tile → OCCLUDED
}
```

Where:
- `object_min_depth`: Nearest point of AABB in view space (approximated by clip.W)
- `tile_max_depth`: Maximum Hi-Z value in the screen region covered by the AABB

## Notes

- Hi-Z uses 1-frame-old data (temporal latency) to avoid GPU sync stalls.
- Uses async buffer mapping for CPU readback.
- Effective for static or slow-moving content; fast-moving objects may pop in for 1 frame.
