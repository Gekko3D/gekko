# Phase 3: Calculate Clearance And Profile Support

## Status

- Confidence: High
- Owner: engine content; runtime blocker adapters remain later work
- Architecture: long-term replacement step, not a bridge
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`

## Scope And Decision

- Measure profile-independent horizontal clearance from each span-column center
  to the nearest solid, blocker, or unknown voxel AABB over the span's complete
  open interval.
- Use a conservative vertical cylinder model. Touching an obstacle at the
  reported radius is allowed; intersection is not.
- Represent blocker metadata as build-input voxel cells. Blockers affect
  clearance but never create support spans.
- Filter source spans into profile graph span IDs by headroom and radius.
- Emit stable `insufficient_headroom` and `insufficient_clearance` rejection
  codes.
- Leave transition generation, authored blocker schemas, runtime adapters, and
  clearance optimization to their owning later phases.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content -run '^(TestBuildNavSourceSpans|TestNavSpanClearanceAndProfileSupport)$'`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `git diff --check`

The table-driven clearance test covers open space, solid walls, blocker-only
subtraction, low headroom, exact-radius contact, stable rejection reasons, and
small/large profile monotonicity. No manual visual/GPU check is required
because Phase 3 has no renderer or runtime integration.

## Results

- Focused clearance tests: pass.
- Content suite: pass.
- Pure-Go content suite: pass.
- Manual visual/GPU checks: intentionally not run.
