# G-Buffer Compaction Note

Scope:
- Task I from `docs/renderer/optimization-playbook.md`
- Reduce G-buffer bandwidth without changing GPU resource classes or renderer architecture

Current layout:
- `GBufferDepth`: `RGBA32Float`
- `GBufferNormal`: `RGBA16Float`
- `GBufferMaterial`: `RGBA32Float`
- `GBufferPosition`: `RGBA32Float`

Proposed layout:
- Keep `GBufferDepth`, `GBufferNormal`, and `GBufferMaterial`
- Remove `GBufferPosition`
- Reconstruct world-space hit position in deferred lighting from:
  - screen UV
  - `CameraData.inv_proj`
  - `CameraData.inv_view`
  - `CameraData.cam_pos`
  - stored linear ray depth in `GBufferDepth.r`

Why this cut first:
- `GBufferPosition` is the single most expensive dedicated target that is only consumed by deferred lighting.
- Removing it avoids one full-screen `RGBA32Float` write in G-buffer and one full-screen sampled read in lighting.
- This is a conservative compaction step: it reduces bandwidth without changing material packing, shadow-map formats, or transparent passes.

Behavioral tradeoff:
- Deferred lighting uses reconstructed world hit position instead of the previously stored voxel-center world position.
- That may slightly smooth some per-voxel lighting boundaries compared with the old “uniform lighting per voxel center” behavior.
- The debug G-buffer view can still show a position visualization by reconstructing world position on demand in the lighting shader.

Invalidation / resize correctness:
- No new cache is introduced.
- Resize behavior stays under the existing G-buffer recreation path; the removed target simply stops being allocated and rebound.

Claim type:
- Structural-only optimization claim in this patch.
- Expected savings: one fewer `RGBA32Float` full-screen render target write and read per frame.
