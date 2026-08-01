# Dynamic Navigation Phase 2 Status

## Status

- Phase: 2 — Replace Resident Query Indexes
- Confidence: High
- Primary owner: engine content navigation queries
- Affected consumers: streamed navigation runtime, diagnostics, Actiongame navigation
- SME alignment required: No

## Implemented

- Added immutable `NavSnapshot` and private resident tile storage.
- Replaced nested span, region, adjacency, and column maps with one tile lookup,
  dense slices, acceptance bitsets, column offsets, and local/exceptional CSR.
- Derive ordinary local edge cost, step, width, headroom, and clearance from
  resident spans and voxel resolution.
- Replaced per-route region visibility maps with dense column membership checks.
- Kept runtime publication as an off-thread build followed by a locked query
  pointer replacement; published resident slices are not mutated.
- Updated `navdiag` retained-heap measurement to isolate the Phase 2 baseline
  snapshot from Phase 3 blocker/carrier overlays and temporary decoded tiles.

## Preserved Invariant

Published queries remain immutable, route reachability matches the independent
Dijkstra oracle, and ordinary edge costs remain deterministic and positive.

## Measurements

Reference: Darwin/arm64, Apple M4 Pro.

- Crossfire resident snapshot: `217,517,928` bytes (`207.44 MiB`), below the
  `256 MB` acceptance gate.
- Crossfire resident index build: `1,224.642 ms`.
- Crossfire measured stair route: p50 `0.001042 ms`, p95/p99 `0.010958 ms`.
- Synthetic dense-flat index: `3.252 ms/op`, `2,406,270 B/op`.
- Synthetic dense-flat route: `4.752 us/op`, `384 B/op`, 6 allocations.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` — pass
- `env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/navdiag` — pass
- Small-graph ordinary, blocker-detour, blocker-split, and cross-tile routes — match Dijkstra reachability.
- `env GOCACHE=/tmp/gekko3d-gocache go test .` — blocked only by the pre-existing
  renderer bridge audit failure at `mod_voxelrt_client_systems.go:745`.

## Deferred To Phase 3

Blocker and carrier queries still build the existing global span overlay. Phase
3 owns replacing that storage and search with affected-region components.
