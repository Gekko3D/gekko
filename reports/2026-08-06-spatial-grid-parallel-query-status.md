# STATUS_REPORT

## Goal

Remove concurrent writes from parallel physics spatial-grid queries.

## Ownership and invariant

- Primary owner: Gekko spatial grid.
- Consumer: asynchronous and synchronous physics broadphase.
- Grid construction is single-threaded; completed buckets are immutable during
  parallel queries.

## Change

- Keep each cell bucket sorted during insertion.
- Remove lazy query-time bucket sorting and its shared `sorted` map.

## Verification

- Passed focused spatial-grid and physics tests.
- Passed focused spatial-grid/physics tests under the Go race detector.
- No tests added.
