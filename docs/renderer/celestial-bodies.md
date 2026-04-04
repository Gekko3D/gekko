# Celestial Bodies Rendering

This document scopes a generic rendering feature for large spherical bodies such as planets, moons, gas giants, and stars.

The goal is to add reusable renderer capability without baking "space game" assumptions into the core renderer.

## Design Goals

- Keep the renderer generic and optional.
- Avoid voxel-shell atmospheres for distant planets.
- Support correct parallax from world-positioned bodies.
- Make the feature reusable across games that need large spherical bodies.
- Skip all work when no celestial bodies exist in the scene.

## Non-Goals

- Full close-range planetary terrain traversal in phase 1.
- Orbital gameplay rules.
- Game-specific solar-system logic.

## Proposed Architecture

### ECS / Engine Layer

Add a generic component, for example `CelestialBodyComponent`, with fields such as:

- `Radius`
- `AtmosphereRadius`
- `SurfaceSeed`
- `CloudSeed`
- `SurfaceColors`
- `AtmosphereColors`
- `CloudCoverage`
- `CloudSpeed`
- `Emission`
- `BodyType`

Add an optional `CelestialBodiesModule` that:

- registers render sync systems
- uploads body data to GPU buffers
- enables the renderer pass only when the module is installed

### Renderer Layer

Add an optional celestial-body pass that runs after depth/G-buffer generation and before final resolve.

The pass should:

- reconstruct a camera ray per pixel
- intersect the ray against analytic spheres in world space
- compare against scene depth so regular scene geometry can occlude the body
- shade the visible surface analytically
- add atmosphere scattering
- add a cloud shell

The pass should be skipped when there are no celestial bodies.

## Why Analytic Rendering

Analytic sphere rendering is the right fit for distant and medium-range planets because:

- silhouette quality is stable
- atmosphere thickness looks correct
- cloud rendering is cheap
- memory use is small
- visual quality does not depend on dense voxel shells

Voxel terrain remains a separate concern for future close-range planetary gameplay.

## Phase 1

Deliver a minimal reusable planet-quality pass:

1. `CelestialBodyComponent`
2. GPU buffer for celestial-body instances
3. One analytic sphere surface model with directional lighting
4. One analytic atmosphere model
5. `spacegame_go` wired as the first client

Phase 1 does not need:

- orbit simulation
- cloud shadows
- multiple scattering
- near-surface terrain transition

## Phase 2

- Add cloud shell rendering with scrolling spherical noise.
- Add body presets for rocky planet, moon, gas giant, and star.
- Add multiple bodies per frame with simple sorting/culling.

## Phase 3

- Add eclipses / body-to-body shadowing where practical.
- Add orbital transform helpers in gameplay or scene modules.
- Add transition rules for swapping from analytic distant rendering to close-range terrain rendering.

## Integration Contract

Games that do not need celestial bodies should:

- not install `CelestialBodiesModule`, or
- install it and create zero body entities

In either case, the feature should have effectively zero visual or runtime impact.

## First Implementation Notes

For this codebase, the clean first insertion point is a dedicated fullscreen pass similar in shape to other screen-space passes, but fed by explicit celestial-body buffers instead of transparent voxel materials.

`spacegame_go` should stop using the temporary voxel atmosphere shell once phase 1 is complete.
