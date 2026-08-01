# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-02
- Project/Area: dynamic destructible navigation tuning
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`

## Executive Summary
- Goal: complete Phase 7 by tuning only demonstrated bottlenecks.
- Confidence: High.
- Primary owner: streamed navigation publication and content query diagnostics.
- Result: moved exact epoch preparation off the main thread; retained every other current setting because measured budgets pass.

## Preserved Invariants
- Epoch comparison remains exact and still covers tile topology, seams, and blocker overlays.
- The worker compares against an immutable previous query; the guarded main-thread commit publishes the already-versioned snapshot atomically.
- Stale load and overlay generations remain unable to publish.

## Measurements
Reference: Darwin/arm64, Apple M4 Pro, Go `1.25.4`.

- Crossfire bundle: `28,887,737` bytes; budget `150 MB`.
- Retained carrier-overlay heap: `225,428,592` bytes; budget `256 MB`.
- Resident index: `1,480.678 ms`.
- Ordinary route, 1,000 samples: p50 `0.001000 ms`, p95 `0.001250 ms`, p99 `0.001500 ms`; p95 budget `2 ms`.
- Carrier route, 1,000 samples: p50 `0.019875 ms`, p95 `0.027666 ms`, p99 `0.035416 ms`; p95 budget `5 ms`.
- Exact full-snapshot epoch preparation: `289.505 ms`, now off-thread.
- Main-thread prepared-snapshot commit: `7.893 ns/op`, zero allocations; budget `0.5 ms` p95.
- Synthetic route dependency validation: `16.63 ns/op`, zero allocations.
- Synthetic epoch preparation: `0.437 ms/op`, `2,544 B/op`, 9 allocations.
- Synthetic dense route: `5.224 us/op`, `464 B/op`, 9 allocations.
- Synthetic carrier/blocker route: `156.923 us/op`, `69,360 B/op`, 1,080 allocations.
- Synthetic local destruction rebuild: `43.995 ms/op`, off-thread.

## Decisions
- Route workers: keep one. Solve latency is orders of magnitude below budget and no queue-wait miss is measured.
- Gzip: keep the standard-library default. The bundle is below budget by more than 5x; no bake/load trade-off justifies another setting.
- Scratch pooling: skip. Current route allocation has no measured GC impact; pooling would add ownership complexity.
- Resolution: keep `0.1 m` for Crossfire. Disk and resident-memory budgets pass without sacrificing narrow traversal fidelity.
- Publication: retain the existing exact `reflect.DeepEqual` comparison, but prepare epochs in the existing worker before the guarded pointer swap.

## Verification
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` — pass.
- `env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/navdiag` — pass.
- `env GOCACHE=/tmp/gekko3d-gocache go test . -run 'Navigation|NavGraph'` — pass.
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` — all packages pass except the pre-existing root renderer audit at `mod_voxelrt_client_systems.go:745`.
- `BenchmarkStreamedNavigationCommit` — `7.893 ns/op`, zero allocations.
- `BenchmarkNavGraphPhase0` — current synthetic measurements recorded above.
- Crossfire ordinary and carrier `navdiag -measure-runs=1000` — pass with zero hard validation errors.

## Remaining Manual Gate
- No windowed gameplay check was required for this scheduling-only change. The Phase 6 Crossfire/Gasworks/lab manual acceptance list remains pending independently.

# END_STATUS_REPORT
