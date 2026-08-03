# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-03
- Project/Area: Gekko streamed navigation runtime
- Related Links: `docs/roadmaps/dynamic-destructible-navigation-rework-plan.md`

## Summary
- Goal: prevent small or rapid voxel edits from causing redundant navigation publication work.
- Scope: shared no-op voxel edit detection, edit-local safety overlays, bounded rebuild coalescing, and resident topology equality before reload.
- Non-scope: coarser navigation resolution and cancellation of an already-running bake.

## Invariants
- Removals keep the previous immutable graph fully usable while grounded NPC movement validates the live voxel world.
- Additions install blockers only over their edited world bounds.
- A rebuild starts after 100 ms without another edit or after 250 ms total, whichever comes first.
- Stale generations never publish.
- Rebuilt delta sidecars remain authoritative even when equal resident topology lets runtime skip its reload.

## Verification
- Focused destruction and navigation tests pass.
- Engine root suite passes except the pre-existing renderer architecture audit at `mod_voxelrt_client_systems.go:745`.
- Content and ActionGame suites pass.
- Existing local-destruction benchmark: 41.774 ms/op, 5.40 MB/op, 24,708 allocs/op for one rebuild on Apple M4 Pro.
- Removal edits do not build or publish a navigation overlay.
- Rebuilt tiles retain the existing profile-bounded gap-jump connections for crossable holes.
- Immutable navigation publication remains allocation-free: 240.4 ns/op, 0 B/op, 0 allocs/op on Apple M4 Pro.

# END_STATUS_REPORT
