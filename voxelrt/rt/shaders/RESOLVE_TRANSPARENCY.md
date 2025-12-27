# Resolve Transparency Shader (resolve_transparency.wgsl)

Purpose
- Fullscreen resolve that composites the opaque lit image with accumulated transparency written by the Accumulation pass (Transparent Overlay + Particles).
- Implements the final step of weighted blended order-independent transparency (WBOIT).

Related
- RENDERER.md — frame graph and pass ordering
- shaders/TRANSPARENT_OVERLAY.md — accumulation of transparent voxel overlay
- shaders/PARTICLES_BILLBOARD.md — accumulation of billboard particles
- rt/app/app.go — setupResolvePipeline (bindings, targets)
- rt/gpu/manager.go — creation of accumulation targets

Inputs and Bindings (BGL0)
- binding(0): tOpaque: texture_2d<f32>
  - Opaque lit color written by the deferred lighting compute pass (StorageTexture RGBA8, sampled as float).
- binding(1): tAccum: texture_2d<f32>
  - RGBA16Float accumulation target. rgb accumulates premultiplied color; a accumulates alpha*w (same payload as weight).
- binding(2): tWeight: texture_2d<f32>
  - R16Float accumulation target for the summed weight (alpha*w).
- binding(3): sampler
  - Filtering sampler (not used for filtering in the reference impl; integer texel fetch is preferred).

Output
- Swapchain color target (same format as surface), fully composed image:
  - out = saturate(opaque + transp), where transp = accum.rgb / max(weight, 1e-5)

Algorithm
1) Compute integer pixel coordinates from interpolated uv or builtin position
   - Use textureDimensions to get width/height.
   - Derive integer ipos via clamp and floor to avoid filtering artifacts.
2) Fetch values with textureLoad
   - copq = textureLoad(tOpaque,  ipos, 0).rgb
   - acc  = textureLoad(tAccum,   ipos, 0).rgb
   - w    = textureLoad(tWeight,  ipos, 0).r
3) Reconstruct transparent color
   - transp = acc / max(w, 1e-5)
4) Compose
   - col = saturate(copq + transp)
   - return vec4(col, 1.0)

Notes and Rationale
- Integer texel fetch avoids sampling across pixel boundaries and keeps resolve consistent with accumulation.
- WBOIT is an approximation that avoids sorting; results are stable and order-independent for a wide range of scenes.
- The alpha channel of the output is set to 1.0 (opaque) since we present to the swapchain; adjust if you need alpha in offscreen targets.

Troubleshooting
- “Transparency missing after refactor”
  - Ensure ResolveBG binds all three textures and the sampler (see setupResolvePipeline).
  - Recreate ResolveBG after resize, as texture views change.
- “Overly bright or washed-out transparency”
  - Check that particles/overlay are writing premultiplied color into accum (rgb *= alpha*w) and weight (alpha*w).
  - Verify additive blending (src=one, dst=one) is enabled on both accumulation targets.
- “Artifacts at edges”
  - Prefer integer texel fetch (textureLoad). Avoid linear filtering for resolve inputs.

Future Work
- Optional tone mapping and gamma control during resolve.
- Dithering or blue-noise to reduce banding in low-weight regions.
- Alternative weighting curves (k) and exposure tuning for different scene scales.
