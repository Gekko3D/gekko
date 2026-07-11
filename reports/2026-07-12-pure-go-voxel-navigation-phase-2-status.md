# Phase 2: Extract Profile-Independent Surface Spans

## Status

- Confidence: High
- Owner: engine content; runtime occupancy adapters remain later work
- Architecture: long-term replacement step, not a bridge
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`

## Scope And Decision

- Accept one known center chunk plus known/unknown adjacent halo chunks.
- Represent known chunk occupancy as sparse solid voxel coordinates; absent
  voxels are empty only inside known chunks.
- Emit center-owned solid-to-empty spans in deterministic X/Z/Y order.
- Cap open intervals at unknown context, diagnose truncation, and reject spans
  with no known open voxel.
- Leave clearance, agent filtering, transitions, regions, routing, runtime
  adapters, and persistence changes to later phases.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content -run '^TestBuildNavSourceSpans$'`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `git diff --check`

Focused table test covers flat ground, a hole, stacked floors, deterministic
IDs, known upper halo, and unknown-halo rejection. No manual visual/GPU check
is required because Phase 2 has no renderer or runtime integration.

## Results

- Focused span test: pass.
- Content suite: pass.
- Pure-Go content suite: pass.
- Manual visual/GPU checks: intentionally not run.
