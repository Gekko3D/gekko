# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-04-18
- Project/Area: `voxelrt` / `XBrickMap` uniform-material sparse brick optimization
- Related Links: `gekko/docs/renderer/xbrickmap-uniform-material-plan.md`
- Reviewers Requested: renderer SME / shader-path reviewer

## Executive Summary
- Goal: reduce payload atlas uploads and payload texture reads for sparse bricks whose occupied voxels all use one material.
- Current state: `Solid` bricks already bypass payload storage, but sparse bricks always allocated payload and fetched per-voxel material.
- Progress: core scope implemented, and the GPU/WGSL brick record was migrated to explicit material vs payload fields.
- Risk/Blocker: shared brick-record semantics still span Go and WGSL, but the field meanings are now explicit instead of overloaded.
- Ask: reviewer should confirm the `BrickFlagUniformMaterial` contract and the new `BrickRecord` layout are acceptable.

## Background / Context
- Why now: payload-backed sparse bricks were still paying upload and lookup cost after dense occupancy made hit confirmation exact.
- Scope: CPU brick classification, GPU upload-mode selection, explicit `BrickRecord` layout migration, WGSL field updates, and unit coverage.
- Non-scope: observability counters, payload format redesign, renderer architecture changes, and material/shading semantic changes.
- Constraints: keep `BrickRecord` fixed-size, keep CPU payload authoritative, preserve transparency semantics.

## Current Status
- Completed:
  - Added `BrickFlagUniformMaterial` and CPU-side `RefreshMaterialFlags()` maintenance for edit transitions.
  - Updated GPU upload path to skip payload allocation for uniform sparse bricks while retaining dense occupancy.
  - Replaced overloaded `atlas_offset`/`atlas_page` semantics with explicit `material_index`, `payload_offset`, and `payload_page` fields in the GPU/WGSL brick record.
  - Updated `gbuffer`, `shadow_map`, `transparent_overlay`, and `particles_sim` to use the explicit brick-record fields and flag bits instead of exact `flags == 0/1`.
  - Added unit tests for brick classification transitions, upload-mode selection, and brick-record encoding.
- In progress:
  - none
- Next steps:
  - optional follow-up for coarse counters if scene-level validation is needed
  - reviewer pass on the shared Go/WGSL brick contract

## Proposed Change
- User-visible behavior: no intended visual change; sparse uniform-material bricks should render identically.
- API/Interface changes: new in-memory/WGSL flag `BrickFlagUniformMaterial`; `BrickRecord` grew from 24 to 32 bytes and now uses explicit `material_index`, `payload_offset`, and `payload_page` fields.
- Data changes: none persisted; GPU brick-table schema changed in-memory only.
- Operational changes: none

## Design Options
### Option A: CPU-authored uniform sparse mode with explicit GPU brick fields
- Approach: classify bricks in CPU memory, encode the mode in `flags`, keep dense occupancy for sparse modes, and use explicit material/payload fields in shaders before payload fetch.
- Pros:
  - local change
  - payload storage and fetches are skipped for uniform sparse bricks
  - no field overloading in the brick table
- Cons:
  - shared contract between Go and WGSL must stay synchronized
  - brick-table record is larger
- Risk:
  - a missed shader consumer would misinterpret `flags`

### Option B: infer uniform sparse only inside GPU upload
- Approach: leave CPU brick state unchanged and infer a special mode while uploading.
- Pros:
  - smaller CPU diff
- Cons:
  - CPU and GPU state can diverge after edits or copies
  - harder to reason about transitions and tests
- Risk:
  - slot lifecycle and runtime behavior become implicit

### Recommendation
- Chosen option: A
- Rationale: it keeps the CPU authoritative and makes transitions testable.
- Open questions:
  - whether coarse runtime counters are worth the added plumbing in a follow-up pass

## Testing Plan
- Unit tests:
  - `./voxelrt/rt/volume`
  - `./voxelrt/rt/gpu`
  - `./voxelrt/rt/core`
- Integration tests: none added in this pass
- Edge cases:
  - solid -> uniform sparse after voxel deletion
  - uniform sparse -> payload sparse after second material appears
  - payload slot release when payload is no longer required
- How to validate locally:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko`
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`

## Rollout / Compatibility
- Backward compatibility: yes; no serialized formats or public APIs changed.
- Feature flags: none
- Migration/Backfill: none
- Rollback plan: revert the new flag handling in volume, GPU upload mode resolution, and WGSL bit checks together.

## Risks & Mitigations
- Risk 1: shader consumers drift from CPU flag semantics. Mitigation: all known brick-flag consumers in this path were updated in one pass and unit tests cover the CPU-side transitions.
- Risk 2: future edits forget to refresh classification. Mitigation: `XBrickMap.SetVoxel` now refreshes flags immediately after payload mutation.

## Specific Questions for Reviewer
1. Is the 32-byte explicit `BrickRecord` acceptable, or is there a hard memory target that requires a denser encoding later?
2. Do we want a follow-up pass to add coarse counters for skipped payload uploads?
3. Are there any additional WGSL consumers outside the current render/particle paths that should share the new flag helpers?

# END_STATUS_REPORT
