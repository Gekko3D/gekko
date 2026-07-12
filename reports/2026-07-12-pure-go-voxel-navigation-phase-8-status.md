# Phase 8: Persistence, Baking, And Diagnostics

## Status

- Confidence: High; implementation checklist complete. Manual representative-map
  graph visualization remains for strict gate acceptance.
- Owner: engine content and offline examples.
- Architecture: long-term voxel graph replacement step, not a bridge.
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`.

## Implemented

- One deterministic pure-Go imported-world bake using existing span, profile,
  region, boundary, and route builders.
- Complete-bundle validation before any output is created.
- Source/graph tile save, manifest-last publication, full-bundle load, and
  cross-tile reference validation.
- Aggregate graph diagnostics and build rejection counts.
- New `cmd/navbake` and `cmd/navdiag` commands.
- New `examples/navigation_graph_lab` synthetic two-tile fixture covering flat
  ground, a hole, steps, an obstacle, stacked floors, low headroom, a tile seam,
  a successful route, and an explicit unsupported-start failure.
- Lab renders source voxels, sampled accepted spans, all rejected spans, region
  bounds/centers, region transitions, and route waypoints in 3D.
- Sparse solid iteration and bitset occupancy in the shared span builder.
- Exact profile-independent squared-distance fields cached per vertical open
  interval. This replaces the per-span expanding ring/column scan while
  preserving conservative solid, blocker, and unknown-halo behavior.
- Float64 region-center accumulation prevents large-region rounding drift from
  placing serialized centers outside their bounds.
- Builder version advanced to `voxel_graph_v2`.

## Verification

- Focused full-world bake/save/load/topology test: pass.
- Content and command packages: pass.
- Pure-Go content suite: pass.
- Lab module compile: pass.
- Lab bake/load diagnostics: 2 source tiles, 2 graph tiles, 537 spans, 528
  accepted spans, 8 regions, 1,910 span transitions, 28 region transitions, 0
  hard errors.
- Lab route: found, 2 region steps, 42 waypoints.
- Invalid-bundle test confirms validation failure creates no output directory.
- User reimported Crossfire and confirmed the level, player spawn, and actiongame
  startup render correctly with the `hl1_standing` navigation debug profile.
- User accepted the navigation graph lab scene: voxel fixture, span markers,
  region bounds, transitions, route, and failure diagnostics render correctly.
- Gasworks temporary bake: 67.44 seconds, 31 source/graph tiles, 1,042,163
  spans, 636,291 accepted spans, 300 regions, 2,486,352 span transitions, 246
  region transitions, and 0 hard errors after save/reload diagnostics.
- Crossfire temporary bake: 47.05 seconds, 18 source/graph tiles, 966,299
  spans, 585,615 accepted spans, 364 regions, 2,293,744 span transitions, 208
  region transitions, and 0 hard errors after save/reload diagnostics.
- Full engine/content/command tests, pure-Go content tests, and 20 repeated
  deterministic bake tests pass.

## Representative Map Outputs

- Gasworks: `/private/tmp/gekko3d-nav/gasworks/gasworks.gknav`.
- Crossfire: `/private/tmp/gekko3d-nav/crossfire.gknav`.
- Bundles use manifest-relative `sources/` and `graphs/` paths, so separate
  output directories are required when baking multiple worlds.
- Both bundles pass save/reload diagnostics, but their graph overlays have not
  been manually inspected on the representative maps.

## Intentionally Not Added

- No polygon compatibility, legacy sidecar loader, runtime streaming, delta
  rebuild, gameplay locomotion, or parallel bake.
