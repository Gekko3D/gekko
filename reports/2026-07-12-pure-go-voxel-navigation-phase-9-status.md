# Phase 9: Runtime Streaming And Delta Rebuilds

## Status

- Confidence: High for automated contracts; manual map/GPU gate remains.
- Owner: engine runtime and content persistence.
- Consumers: streamed levels now expose revisioned graph residency; actiongame
  locomotion remains Phase 10.
- Architecture: long-term voxel graph replacement, not a bridge.

## Implemented

- Optional level `navigation.manifest_path` contract and validation.
- Background source/profile graph residency using streamed level observers.
- Immutable resident graph snapshots with one atomic revision increment per
  accepted load set.
- Imported-world dirty-chunk notifications that preserve empty destruction
  snapshots before entity removal.
- Halo-expanded delta source generation through existing span/graph builders.
- Neighbor graph reconnection and override publication beside world delta data.
- Explicit empty source/graph overrides suppressing stale static tiles.
- Runtime route service returning navigation revision.
- Actiongame F12 camera-local overlay for accepted/rejected spans, region
  bounds, and transitions. It refreshes on navigation revision swaps and
  supports a development manifest override through `GEKKO_NAV_GRAPH`.
- Navigation source schema v2 persists compact solid/blocker runs. Current
  builder `voxel_graph_v4` derives per-profile clearance above the reachable
  step envelope and merges ordinary walk/stair/step surfaces into ground
  regions. Full, delta, and cross-tile generation share these paths.

## Preserved Invariant

- Full and delta generation use identical span, clearance, region, and boundary
  builders.
- Old resident topology remains visible until complete replacement residency is
  ready; mixed old/new neighboring transitions are never published.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test . ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- Focused fixtures cover removed/restored floor, voxel blocker, seam transition,
  explicit empty override, full/delta parity, residency, route change, and
  revision swap.
- Ramp regression covers the same physical slope at 0.1 m and 0.2 m voxel
  resolution, plus a tall-wall control that remains clearance-rejected.
- Crossfire v4 rebake: 18 source/graph tiles, 966,299 spans, 783,595 accepted,
  163 regions, 3,089,256 span transitions, 376 region transitions, and 0 hard
  errors. Output: `/tmp/crossfire-nav-v4-ground-regions/crossfire.gknav`.
- Actiongame suite passes when skipping one unrelated existing fixture failure:
  `TestShotgunProfileTurnsWorldModelForward` reports `missing shotgun attachment`.
- F12 lifecycle fixture verifies show/hide, accepted/rejected span colors,
  region/link creation, and revision capture.

## Actiongame Visual Check

Bake a graph, run matching level with `GEKKO_NAV_GRAPH` pointing at the
generated manifest, then press F12. Overlay is camera-local and refreshes after
movement or a navigation revision swap:

Existing pre-v4 bundles and navigation delta overrides must be rebaked.

- green: accepted spans
- red: profile-rejected spans
- blue: region bounds
- orange: region transitions

## Manual Gate Remaining

- Run F12 edit/remove/restore checks with rendered Crossfire and Gasworks worlds.
- Confirm route revision changes after GPU-visible destruction and record map
  coordinates/outcomes.
