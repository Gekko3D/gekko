# STATUS_REPORT

## Summary

- Goal: prevent grounded actors from becoming permanently unroutable after
  small motor drift, and prevent bot plans from retrying the same route failure
  every frame.
- Primary owner: `content` navigation query.
- Affected consumer: `actiongame` NPC navigation.
- Confidence: High.
- Status: implemented and automatically verified.

## Scope

- Cache accepted spans by world-space column.
- When exact route-start resolution fails, inspect only neighboring columns and
  project starts within one navigation voxel.
- Keep route goals exact.
- Preserve automatic navigation retry cooldown and its graph revision across
  target changes; manual targets still bypass the cooldown.

Non-scope: navigation rebakes, tactical candidate scoring, planner structure,
or broad point projection.

## Invariants

- Blocked spans are never selected.
- Stacked spans still resolve by nearest support height.
- Graph revision changes can invalidate retry backoff immediately.
- Manual navigation commands remain immediate.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env GOCACHE=/tmp/gekko3d-gocache go test .`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...` in `actiongame`
- Focused race tests for navigation start projection and retry backoff

Manual game reproduction remains useful for behavior validation; no navigation
content rebuild is required.
