# Deferred Lighting Compute Shader (deferred_lighting.wgsl)

This document describes the responsibilities, inputs/outputs, bindings, and lighting model used by the deferred lighting compute shader.

Related:
- RENDERER.md — overview of the frame pipeline and resource orchestration
- shaders/GBUFFER.md — producer of the inputs consumed here
- shaders/SHADOW_MAP.md — shadowing data referenced by lights
- PARTICLES.md — billboard particles are composited after this pass

## Responsibilities

- For each screen tile/pixel:
  - Read G-Buffer surfaces (depth t, normal, material, position).
  - Evaluate lighting for the visible surface using the scene’s lights and ambient term.
  - Sample shadow maps when applicable.
  - Write the final lit color into a storage texture (RGBA8), to be blitted to the swapchain.

This pass performs shading only; geometry was resolved in the G-Buffer compute pass.

## Threading and Dispatch

- Workgroup size: 8×8 threads (tile-based).
- Dispatch: ( (Width+7)/8, (Height+7)/8, 1 ) — see RENDERER.md.

## Bindings and Resources

The bind group layout is created in rt/app/app.go and mirrored by the shader:

Group 0 — Camera + Lights
- binding(0): CameraData (uniform)
  - view_proj, inv_view, inv_proj (for reconstructions)
  - cam_pos
  - light_pos (convenience, typically from the first light)
  - ambient_color
  - debug_mode, render_mode (see “Render Modes”)
- binding(1): Lights buffer (read-only storage, array of core.Light)

Light layout (CPU side):
- Position: [x,y,z,w], where w=1 for point, 0 for directional; shader can ignore w.
- Direction: [x,y,z,0]
- Color: [r,g,b,intensity] — intensity is placed in the alpha channel on CPU.
- Params: [range, cosCone, type, pad]
  - type: float encoding of enum (Directional, Point, Spot).
  - cosCone: cosine(spot cone half-angle) for spotlights.

Group 1 — G-Buffer inputs + Output + Shadow maps
- binding(0): Depth (RGBA32F, sampled as UnfilterableFloat)
  - x = ray distance t from camera to hit
- binding(1): Normal (RGBA16F, sampled as Float)
- binding(2): Material (RGBA32F, sampled as UnfilterableFloat)
  - PBR payload (baseColor, roughness, metalness, IOR/transparency, emission hints)
- binding(3): Position (RGBA32F, sampled as UnfilterableFloat)
- binding(4): Output color (storage RGBA8, write-only)
- binding(5): Shadow maps array (2D array texture), format defined by shadow pipeline

Group 2 — Materials/Sectors
- binding(3): Read-only storage buffer with materials/sectors/acceleration-data views required by lighting.

Notes
- Sample types must match those specified in app.go.
- The shader reads depth as ray-distance t (not NDC z), consistent with G-Buffer.

## Inputs and Reconstruction

- World position P:
  - Provided directly via Position G-Buffer, or reconstructed from (cam_pos + dir * t).
  - The current pipeline keeps Position in a dedicated texture for flexibility.

- Normal N:
  - From Normal G-Buffer (world-space unit vector).
  - Ensure normalization and correct orientation.

- Material M:
  - PBR parameters; ensure packing matches this shader’s interpretation.

- Ambient:
  - From CameraData.ambient_color (RGB).

## Lighting Model

Per-light contribution depends on type:
- Directional:
  - L = normalize(-light.direction.xyz)
  - Attenuation = 1.0 (no distance falloff)
- Point:
  - L = normalize(light.position.xyz - P)
  - Distance falloff = smooth attenuation with light.params[0] (range)
- Spot:
  - As Point, plus spotlight factor:
    - spotTerm = saturate((dot(L, -light.direction) - cosCone) / (1 - cosCone))
    - Optionally squared for tighter falloff; multiplied into attenuation.

Common terms:
- NdotL = max(dot(N, L), 0)
- Diffuse:
  - Lambert: baseColor * NdotL
- Specular (optional/simple):
  - A simple Blinn-Phong or roughness-adjusted term can be applied.
  - The implementation can be minimal to keep the pass “cheap”.

Emission:
- Material or palette-based emission may add directly to the final color.

Shadows:
- If the light casts shadows, sample shadow map(s) for the light using a world-to-light transform (provided/derived in the shadow system).
- A PCF or simple compare reduces hard edges.
- shadowFactor in [0,1] multiplies the light contribution.

Final:
- Accumulate ambient + sum(lightContrib * shadowFactor) → write RGBA8.

## Render Modes (Debugging)

Render mode is provided via CameraData.render_mode and can switch the output:
- RenderModeLit (0): full shading (default).
- RenderModeAlbedo (1): output baseColor from material buffer.
- RenderModeNormals (2): visualize world normals.
- RenderModeGBuffer (3): visualize an aspect of the G-Buffer (e.g., depth t mapped to grayscale).

These are useful for diagnosing pipeline and material issues.

## Output

- One storage texture (RGBA8 Unorm), written per-pixel.
- This texture is then sampled and drawn by the fullscreen pass to the swapchain.

## Edge Cases and Tuning

- Depth “miss” pixels:
  - If t is invalid (e.g., <=0 or >= INF sentinel), skip shading and write background (black).
- Normalization:
  - Always normalize N and any direction vectors to avoid artifacts.
- Energy conservation:
  - If specular is added, consider clamping or balancing diffuse/specular to keep images stable.
- HDR considerations:
  - Output is RGBA8; tone mapping (if any) should happen before write.
  - Keep light intensities reasonable for the current LDR target or extend the pipeline to HDR.

## Performance Notes

- Use 8×8 tiles for good occupancy; profile if changing.
- Minimize texture reads (e.g., only fetch position if required).
- Pack material channels compactly to reduce memory bandwidth.
- Limit shadow samples (PCF kernel) for performance; expand with a quality toggle if needed.

## Interactions

- Consumes all G-Buffer outputs and shadow maps.
- Produces the final lit color used by:
  - Fullscreen blit to swapchain.
  - Particles pass (composited additively after this stage).
  - Text overlay (drawn last).
