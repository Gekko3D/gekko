# STATUS_REPORT

## Goal

Remove per-frame whole-navigation scans from actiongame tactical bot scoring.

## Scope

- Cache revision-stable tactical graph metadata in actiongame.
- Build five diverse representatives plus on-demand fallback IDs per one-metre
  region bucket and a cell index once per resident graph revision.
- Scan only sampled navigation tiles intersecting tactical radius.
- Preserve radius coverage with 16 angular by 8 radial bins.
- Reuse baked span references and skip runtime-blocked spans.
- Compact blocker connectivity once per immutable overlay query.
- Confirm samples before bin selection with one overlay-aware reachability pass.
- Use active overlay exit counts for escape scoring.
- Build overlay queries off the game thread and atomically swap only the latest
  resident-generation result.
- Publish current-residency queries with their captured overlay generation and
  coalesce later overlay changes into the next build.
- Reuse immutable resident navigation snapshots without copying the full
  manifest on the game thread.
- Rebuild spatial metadata only when resident graph data changes.
- Budget one due tactical bot update per frame.
- Keep navigation schemas, route behavior, and planner limits unchanged.

## Confidence

- High: overlay and resident graph revisions are tracked separately.
- Invariant: tactical candidates remain resident, reachable, unblocked, bounded, and deterministic.
- Engine API change: read-only `IsSpanBlocked`, counted `ReachableSpans`, and
  `NavigationGraphRevision`; no schema or mutation change.

## Verification

- `gekko`: `env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1`
- `actiongame`: `env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1`
- Both modules: `env GOCACHE=/tmp/gekko3d-gocache-vet go vet ./...`
- `gekko`: `env GOCACHE=/tmp/gekko3d-gocache-race go test -race . ./content -count=1`
- `actiongame`: `env GOCACHE=/tmp/gekko3d-gocache-race go test -race ./src/modules/startup -count=1`
- Focused regressions cover production-density radius coverage, diverse bucket
  representatives and fallback, blocker walls, blocker detours, deferred and
  stale overlay swaps, failed-stop recovery, active exit scores, and disabled
  ladder traversals.

## Open Questions

- None. Add a shared navigation spatial index only if another measured caller needs it.
