# STATUS_REPORT

## Metadata

- Owner: engine content and streamed navigation runtime
- Date: 2026-08-01
- Project/Area: dynamic destructible navigation rework, Phase 0
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`
- Reviewers Requested: navigation/runtime owner

## Executive Summary

- Goal: establish a green correctness gate and reproducible baseline before replacing navigation persistence and query indexes.
- Current state: all navigation content tests pass; current JSON sidecars remain unchanged.
- Progress: repaired the invalid jump fixture, added a test-only Dijkstra reachability oracle, generated-graph benchmarks, and `navdiag` measurement output.
- Risk/Blocker: the existing Crossfire bundle is far above future phase budgets; async publication and dependency-aware replan counters do not exist yet.
- Ask: use this report as the Phase 1 comparison baseline.

## Changes

- Region compression now preserves traversal and gate metadata when it emits region transitions. The repaired jump fixture therefore validates a real compression invariant instead of constructing an invalid graph tile.
- `navDijkstraReachability` is test-only and walks declared span transitions directly, including cross-tile links and optional blocker overlap. Small ordinary, detour, and blocker-split routes must agree with the production query on reachability.
- `BenchmarkNavGraphPhase0` generates dense-flat, stacked, multi-region, carrier-blocked, and locally destroyed worlds in memory. The destruction case writes only temporary sidecars.
- `navdiag -measure-runs=N` emits exact bundle bytes, full load/decode/validation time, query-index construction time, retained heap delta, and route p50/p95/p99 when given `-start` and `-end`.

## Baseline

Reference: Darwin/arm64, Apple M4 Pro (as reported by Go benchmarks), Go `1.25.4`.

Crossfire bundle:

- complete bundle: `1,260,582,291` bytes (`1.260 GB`)
- source sidecars: `264,899,029` bytes
- graph sidecars: `995,480,680` bytes
- 288 source tiles, 288 graph tiles, 966,299 spans, 783,595 accepted spans
- 3,089,460 span transitions, 217 regions, 11,384 region transitions
- full load/decode/validation: `8731.294 ms`
- query index construction: `3076.356 ms`
- retained heap delta after load and index: `1,402,639,536` bytes

Crossfire stair route (`9.75,-42.80,35.95` to `9.75,-43.00,35.75`, 20 samples):

- p50: `0.001458 ms`
- p95: `0.004958 ms`
- p99: `0.020125 ms`

Synthetic benchmark (`-benchtime=1s -benchmem`):

- dense flat index: `3.605 ms/op`, `3,366,209 B/op`
- dense flat route: `1.646 µs/op`, `320 B/op`
- stacked index: `3.273 ms/op`, `3,142,514 B/op`
- multi-region route: `404.820 µs/op`, `331,273 B/op`
- carrier-blocked route: `202.209 µs/op`, `374,433 B/op`
- local destruction delta rebuild: `2.652 ms/op`, `1,845,383 B/op`

The bundle and retained heap exceed the later Crossfire targets (`150 MB` and `256 MB`), as expected for the pre-rework JSON/map representation.

## Unavailable Baseline Metrics

- Async destruction-to-blocker and main-thread publish latency: Phase 4 has not introduced those operations.
- Dependency-aware retained versus replanned route counts: Phase 5 does not yet track tile epochs or route dependencies.
- Production global-span-search count: no counter exists; current carrier/blocker routing still uses the known global fallback.

## Verification

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./content/...
env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/navdiag
env GOCACHE=/tmp/gekko3d-gocache go test ./content -run '^$' -bench '^BenchmarkNavGraphPhase0$' -benchtime=1s -benchmem
env GOCACHE=/tmp/gekko3d-gocache go run ./cmd/navdiag \
  -nav ../actiongame/assets/levels/crossfire/worlds/crossfire-nav-v4/crossfire.gknav \
  -start 9.75,-42.80,35.95 -end 9.75,-43.00,35.75 -measure-runs 20 -json
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup
```

No manual GPU or gameplay check is required: Phase 0 only changes content tests, benchmarks, diagnostics, and documentation.

The optional broad engine sweep, `env GOCACHE=/tmp/gekko3d-gocache go test ./...`, still fails in the unrelated root test `TestVoxelRtBridgeRendererInternalsTouchpointsAreAudited`: `mod_voxelrt_client_systems.go:745` has an unaudited `BufferManager` touchpoint. All content packages and the direct ActionGame consumer package pass.
