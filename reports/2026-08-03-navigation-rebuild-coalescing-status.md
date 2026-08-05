# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-03
- Project/Area: Gekko streamed navigation runtime
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`

## Summary
- Goal: prevent small or rapid voxel edits from causing redundant navigation publication work.
- Scope: shared no-op voxel edit detection, profile-aware removal impact checks, edit-local safety overlays, bounded rebuild coalescing, and resident topology equality before reload.
- Non-scope: coarser navigation resolution and cancellation of an already-running bake.

## Invariants
- Removals keep the previous immutable graph fully usable while grounded NPC movement validates the live voxel world.
- Removal-only edits enter the bake queue only after removed obstruction can open traversal or cumulative live support no longer covers the grounded footprint for every agent profile.
- Additions install blockers only over their edited world bounds.
- A rebuild starts after 100 ms without another edit or after 250 ms total, whichever comes first.
- Stale generations never publish.
- Rebuilt delta sidecars remain authoritative even when equal resident topology lets runtime skip its reload.

## Verification
- Focused destruction and navigation tests pass.
- Engine root suite passes except the pre-existing renderer architecture audit at `mod_voxelrt_client_systems.go:760`.
- Content and ActionGame suites pass.
- Existing local-destruction benchmark: 41.774 ms/op, 5.40 MB/op, 24,708 allocs/op for one rebuild on Apple M4 Pro.
- Removal edits do not build or publish a navigation overlay.
- Rebuilt tiles retain the existing profile-bounded gap-jump connections for crossable holes.
- Immutable navigation publication remains allocation-free: 240.4 ns/op, 0 B/op, 0 allocs/op on Apple M4 Pro.

## 2026-08-05 Follow-up
- `/tmp/actiongame-bot.log` showed pistol removals publishing graph revisions 2-4 and invalidating route dependency epochs.
- Root cause: the first removal filter treated any removed voxel inside a potential body volume as a traversal opening, even when the remaining wall could not fit an agent capsule.
- Fix: exact ignored removal cells are retained and checked against effective live occupancy (including immutable backing), all manifest profiles, and the shared grounded-motor footprint. Only the connected damage component touched by the newest edit is evaluated; openings queue when locally walkable cells connect graph spans that were not already mutually reachable. Boundary erosion touching multiple sides of one reachable area is ignored.
- No weapon, voxel-count, crater-radius, or timing threshold was added.
- The tilde debug menu separates the general navigation-view revision from the resident graph revision, and reports rebuild state, queued tiles, started/completed/published rebuild counts, ignored edits, and the exact trigger reason (`removal_support_lost`, `removal_new_connection`, or a conservative fallback).
- A later single-shot trace identified the remaining fallback as `unknown_edit`: immutable-backing materialization could mark a chunk changed without carrying the sphere edit metadata. Backing removal now skips provider-empty cells and records the sphere whenever either backing or explicit geometry changes, so real pistol removals reach the semantic classifier instead of conservatively rebaking.
- ActionGame `go test ./...` passes. The engine root suite compiles and reaches only the pre-existing renderer architecture audit failure at `mod_voxelrt_client_systems.go:760`; no tests were added.

# END_STATUS_REPORT
