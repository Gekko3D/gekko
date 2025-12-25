# Fullscreen Blit Render Shader (fullscreen.wgsl)

This document describes the simple fullscreen pass that copies the deferred lighting output (stored in an intermediate storage texture) to the swapchain render target.

Related
- RENDERER.md — pass ordering and why we blit from a storage texture
- shaders/DEFERRED_LIGHTING.md — produces the intermediate RGBA8 storage texture that we sample here

## Responsibilities

- Render a single full-screen triangle.
- Sample the intermediate “final color” storage texture produced by the deferred lighting pass.
- Write to the current swapchain color attachment.

This pass performs no lighting or composition. It exists to decouple compute-based lighting from the swapchain format and to keep a consistent composition path for downstream passes (Particles, Text).

## Bindings and Data

Bind Group 0
- binding(0): Texture2D (sampled) — the storage texture view containing final color from deferred lighting
- binding(1): Sampler — linear sampler used for the blit

Notes
- The storage texture (RGBA8) is created with usage including TextureBinding, and a sampled view is passed to this shader.
- The render pipeline is created with this bind group layout in rt/app/app.go (RenderBG).

## Vertex Stage

- Emits a single full-screen triangle (3 vertices). No vertex buffer is needed.
- Typical approach:
  - Use SV_VertexID / builtin(vertex_index) to generate clip-space positions for the 3 vertices:
    - (-1, -3), (3, 1), (-1, 1), or a similar set
- Passes through UVs (if used) or computes them in the fragment stage from gl_FragCoord or clip-space position.

## Fragment Stage

- Samples the input texture and writes the result to the swapchain color attachment.
- No color transforms are applied (tone mapping, gamma, etc. should have been baked into the deferred output if needed).

Pseudocode
- color = textureSample(input_tex, sampler, uv)
- return color

## Ordering

- Executed after:
  - G-Buffer compute pass
  - Shadow compute pass
  - Deferred Lighting compute pass (which wrote to the storage texture)
  - Optional Debug compute pass (which may have overridden the storage texture for inspection)
- Executed before:
  - Particles billboard pass
  - Text overlay pass

This ensures Particles and Text render on top of the final lit image.

## Troubleshooting

- Black screen:
  - Confirm the storage texture view is valid and bound to binding(0).
  - Ensure the sampler is created and bound to binding(1).
  - Check that the deferred lighting pass wrote to the storage texture (not an uninitialized texture).
- Color mismatch:
  - Verify that any desired tone mapping and/or gamma correction is applied prior to this step.
- Stretching/wrong orientation:
  - Confirm UV generation matches the screen coordinate system, and no unintended Y-flip occurs.

## Performance

- Minimal cost: one triangular draw and one texture sample per pixel.
- The primary benefit is keeping compute-based lighting independent of the swapchain constraints and simplifying resource reuse.
