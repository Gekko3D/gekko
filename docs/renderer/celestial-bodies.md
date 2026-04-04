# Celestial Bodies Rendering

This document scopes a generic rendering feature for large spherical bodies such as planets, moons, gas giants, and stars.

The goal is to add reusable renderer capability without baking "space game" assumptions into the core renderer.

## Current Implementation Status

Implemented today:

- `CelestialBodyComponent` as the engine-facing data model
- ECS-to-renderer sync for body data
- optional renderer feature for celestial bodies
- renderer-side atmosphere and cloud shell
- hybrid usage in `spacegame_go`:
  - voxel surface body
  - renderer atmosphere and clouds
- exposed art controls for atmosphere and clouds
- time-driven cloud drift
- generic `CelestialMotionComponent` / `CelestialMotionModule`

Important consequence:

- the renderer feature is currently best suited for distant and medium-range spherical presentation
- close-range solid terrain is still better served by voxel geometry or a future terrain LOD system
- the hybrid planet path is now the preferred direction for rocky worlds in this repo

## Design Goals

- Keep the renderer generic and optional.
- Avoid voxel-shell atmospheres for distant planets.
- Support correct parallax from world-positioned bodies.
- Make the feature reusable across games that need large spherical bodies.
- Skip all work when no celestial bodies exist in the scene.
- Keep rocky-world surface rendering decoupled from atmosphere/cloud rendering so games can choose:
  - analytic-only
  - voxel-only
  - hybrid voxel surface plus analytic atmosphere/clouds

## Non-Goals

- Full close-range planetary terrain traversal in phase 1.
- Orbital gameplay rules.
- Game-specific solar-system logic.

## Proposed Architecture

### ECS / Engine Layer

Add a generic component, for example `CelestialBodyComponent`, with fields such as:

- `Radius`
- `AtmosphereRadius`
- `CloudRadius`
- `DisableSurface`
- `SurfaceSeed`
- `CloudSeed`
- `SurfaceColor`
- `AtmosphereColor`
- `CloudColor`
- `CloudCoverage`
- `AtmosphereDensity`
- `AtmosphereFalloff`
- `AtmosphereGlow`
- `CloudOpacity`
- `CloudSharpness`
- `CloudDriftSpeed`
- `CloudBanding`
- `Emission`

Add an optional `CelestialBodiesModule` or equivalent renderer-facing bridge that:

- registers render sync systems
- uploads body data to GPU buffers
- enables the renderer pass only when the module is installed

### Renderer Layer

Add an optional celestial-body pass that runs after depth/G-buffer generation and before final resolve.

The pass should:

- reconstruct a camera ray per pixel
- intersect the ray against analytic spheres in world space
- compare against scene depth so regular scene geometry can occlude the body
- add atmosphere scattering
- add a cloud shell
- optionally shade the visible surface analytically when `DisableSurface == false`
- allow atmosphere/clouds to render over an opaque voxel or mesh body when `DisableSurface == true`

The pass should be skipped when there are no celestial bodies.

## Why Analytic Rendering

Analytic sphere rendering is the right fit for distant and medium-range planets because:

- silhouette quality is stable
- atmosphere thickness looks correct
- cloud rendering is cheap
- memory use is small
- visual quality does not depend on dense voxel shells

Voxel terrain remains a separate concern for future close-range planetary gameplay. For rocky planets in this codebase, the practical direction is now:

- voxel or chunked terrain for the solid body
- analytic atmosphere and clouds for the large-scale shell
- analytic-only rendering reserved for bodies that benefit from it:
  - gas giants
  - stars
  - very distant moons
  - cheap far LODs

## Phase 1

Completed:

Deliver a minimal reusable planet-quality pass:

1. `CelestialBodyComponent`
2. GPU buffer for celestial-body instances
3. Atmosphere model
4. Cloud shell model
5. `spacegame_go` wired as the first client

Phase 1 does not need:

- orbit simulation
- cloud shadows
- multiple scattering
- near-surface terrain transition

## Phase 2

- Expose art controls as stable ECS fields instead of shader-only constants.
- Add time-driven cloud drift.
- Support hybrid voxel-surface plus analytic-atmosphere usage cleanly.
- Add multiple bodies per frame with simple sorting/culling.

Status:

- largely completed
- body presets are still missing
- cloud shadows are still missing
- renderer documentation should treat hybrid rocky planets as first-class usage

## Phase 3

- Add cloud shadows onto the visible surface.
- Add better eclipse / body-to-body shadowing where practical.
- Add transition rules for swapping from distant analytic/hybrid rendering to close-range terrain rendering.
- Add body presets for rocky planet, moon, gas giant, and star.

## Phase 4

- Add renderer-side controls for terminator quality and eclipse softness.
- Add support for directional-light-aware atmosphere tuning without per-game shader edits.
- Add quality/performance tiers for cloud detail and atmosphere sampling.

## Phase 5

- Add proper distant-body LOD strategy:
  - analytic-only far LOD
  - hybrid mid LOD
  - richer close LOD
- Keep transitions stable across camera movement and orbital motion.

## Integration Contract

Games that do not need celestial bodies should:

- not install `CelestialBodiesModule`, or
- install it and create zero body entities

In either case, the feature should have effectively zero visual or runtime impact.

## First Implementation Notes

For this codebase, the clean first insertion point is a dedicated fullscreen pass similar in shape to other screen-space passes, but fed by explicit celestial-body buffers instead of transparent voxel materials.

`spacegame_go` should not return to a voxel atmosphere shell. The current recommended pattern for rocky worlds is:

1. voxel terrain body
2. celestial-body component for atmosphere/clouds
3. celestial motion component for orbit / self rotation when needed

## Recommended Next Improvements

In practical priority order:

1. Cloud shadows on the voxel surface.
2. Planet body presets and authored palette/style presets.
3. Better voxel-biome style:
   - stronger biome separation
   - blockier coastlines
   - craters / deserts / lava regions
4. Terminator and eclipse controls.
5. Proper LOD strategy for solar-system scenes.

## Useful Additions To Document Later

- Debugging and tuning checklist for celestial bodies.
- Performance budget guidance for body count, cloud complexity, and LOD switching.
- Screenshot-based verification examples for:
  - side-lit atmosphere
  - cloud transparency
  - moon orbit visibility
  - eclipse / overlap cases
