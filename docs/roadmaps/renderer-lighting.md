# Renderer Lighting Roadmap

This document captures the renderer lighting and performance work that is still worth planning against. It replaces older overlapping notes about renderer analysis, GI strategy, and lighting implementation plans.

## Current Renderer State

The live renderer currently does all of the following:

- builds an opaque G-buffer in a compute pass
- generates a Hi-Z chain
- renders shadows
- bakes a capped batch of dirty probe-GI probes
- runs deferred lighting into an `RGBA16Float` storage target
- composites CA volumes, transparent voxels, particles, and sprites through WBOIT
- resolves to the swapchain with text and gizmos layered at the end

Probe GI is no longer hypothetical. The engine already has:

- probe placement derived from nearby scene regions
- dirty tracking tied to placement, lighting, skybox, config, and voxel uploads
- capped per-frame probe baking
- deferred-lighting sampling and debug views

## Main Constraints

- direct lighting still scales poorly with large light counts
- shadow quality and update policy are still fairly coarse
- G-buffer bandwidth is expensive
- probe GI quality is functional but still basic compared with the rest of the lighting stack
- there is limited observability for per-pass cost and lighting density

## Style Rules

Any lighting work should preserve the renderer's voxel look:

- shade from voxel-center positions
- keep normals block-aligned
- avoid wide filmic blur
- prefer stepped or tightly bounded indirect-light behavior over soft wash
- do not let indirect light erase face readability

## Recommended Priorities

### 1. Better observability

Add or improve:

- per-pass GPU and CPU timings
- visible-light and shadow-refresh counters
- light-density debug output
- probe-GI activity counters that are easy to correlate with frame cost

### 2. Light scaling

Move away from shading every light at every pixel. The next practical target is tiled or clustered light lists in deferred lighting.

### 3. Shadow cost control

Improve:

- per-light update cadence
- tiered shadow resolution
- caching for lights that do not need a fresh map every frame
- tighter directional fitting and cascade policy

### 4. Probe GI quality

The current probe path exists, but it still needs iteration:

- better active-region selection
- better dirty-region marking around edits
- clearer quality tiers for rays-per-probe and bake budget
- improved debug visualizations for stale or invalid probes

### 5. Optional local indirect detail

If probe GI remains too broad indoors, add a small local screen-space enhancement layer on top of probe GI rather than replacing it. That layer should stay conservative and should never become the sole source of indoor bounce light.

### 6. G-buffer and bandwidth cleanup

Once lighting and shadows are more stable, revisit:

- packed material formats
- lower-cost normal or position encodings where precision allows
- redundant texture traffic across deferred lighting and resolve

## Immediate Next Steps

If work starts today, the most defensible sequence is:

1. tighten instrumentation and debug views
2. implement light-list scaling
3. improve shadow update policy
4. tune probe GI quality and budgets
5. only then evaluate whether a local screen-space indirect pass is still necessary

## Out of Scope for Now

- path tracing
- full voxel cone tracing
- heavy temporal smoothing that softens the scene style
- maintaining multiple competing GI architectures in production at the same time
