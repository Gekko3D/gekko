# XBrickMap Hot-Path Optimization Plan

This document is the live execution plan for improving `XBrickMap` rendering performance without requiring an immediate full `64-tree` migration.

It is written for agent execution. Each task includes:

- goal
- expected impact
- file ownership
- constraints
- verification
- acceptance criteria
- a prompt seed you can hand to an agent

## Opening Context

System:

- `voxelrt` sparse voxel rendering path in `gekko`

Problem:

- Compared with other voxel engines using `64-tree` render structures, the current `xbrickmap` path appears slower on equivalent scenes.
- The current renderer already uses local bitmasks effectively at the sector and microcell levels, but it still pays extra lookup, probing, and payload fetch costs in the hot path.

Goal:

- Materially improve `xbrickmap` traversal performance before committing to a structural migration.
- Preserve current visible behavior and CPU-authoritative editing semantics.
- Produce measurable improvements in primary-ray, shadow-ray, and occupancy-query-heavy paths.

Explicit v1 boundaries:

- Do not replace `xbrickmap` with `64-tree`.
- Do not change ECS bridge ownership or object lifecycle.
- Do not change material semantics, transparency semantics, or visible shading rules.
- Do not broaden scope into unrelated renderer optimization work.

## Confidence Gate

Confidence: High

Why:

- The first two optimizations are local, low-risk, and directly supported by the current data layout.
- The per-brick dense occupancy work is larger, but the value proposition is technically clear.
- Direct object-local sector indexing is more architectural, but it can be isolated behind the existing lookup interface.

Key assumptions:

- Performance regressions are dominated by lookup indirection, repeated probing, and unnecessary payload fetches rather than only by BVH or frame-graph issues.
- `xbrickmap` remains the editable source-of-truth structure during this work.
- Shader and GPU layout changes are acceptable as long as bind groups and tests remain correct.

What would raise confidence further:

- Capture before/after numbers for representative scenes.
- Add targeted CPU-side upload/build observability around voxel-heavy work.

Consult SME dev required?: No

## Technology Stack

- Language: Go
- Renderer GPU API: WebGPU via `github.com/cogentcore/webgpu/wgpu`
- Shader language: WGSL
- Core modules in scope:
  - `voxelrt/rt/gpu`
  - `voxelrt/rt/shaders`
  - `voxelrt/rt/volume`
- Constraint:
  - do not change the framework or rendering backend

## Global Contracts And Guardrails

Apply to all tasks in this plan:

- Preserve current visible behavior unless a task explicitly calls out a temporary instrumentation-only change.
- Preserve CPU-authoritative editing through `Scene` and `XBrickMap`.
- Prefer additive data-layout evolution over destructive redesign.
- Keep shader bind-group layouts synchronized across all passes that consume voxel data.
- If a task changes `BrickRecord`, sector lookup layout, payload bindings, or occupancy buffers, it must update all relevant shader consumers in the same task.
- If a task changes invalidation or caching behavior, it must name the source of truth and invalidation condition explicitly.

End-of-phase commands:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`

Additional commands by phase are listed below.

## Frozen Terms

These names are fixed for this plan:

- `sector grid`: the current hash-probed lookup from `(sx, sy, sz, object base)` to GPU sector index
- `brick mask`: the `64-bit` sector-level mask for present bricks
- `micro mask`: the current `64-bit` per-brick occupancy mask over `2x2x2` microcells
- `dense occupancy`: a proposed per-brick exact `8x8x8` occupancy bitset used for empty/non-empty rejection before payload fetch
- `payload fetch`: any read from the 3D voxel payload atlas used to obtain palette/material values
- `direct sector indexing`: object-local lookup that removes hash probing for sector lookup

No additions or redefinitions without updating this document.

## Baseline Findings

Current hot-path issues:

1. Sector lookup is a linear-probe hash lookup in shader code.
2. Shader code uploads `grid_mask` but still uses modulo instead of mask-based wrapping.
3. `sample_occupancy_local` computes and reads `BrickRecord` before checking the sector's `brick_mask`.
4. For non-solid bricks, exact empty/non-empty rejection still depends on a payload texture read.
5. Non-solid bricks upload a full `8x8x8` payload even when geometry inside the brick is sparse.
6. Current `tree64` refinement still re-enters `sample_occupancy_local`, so the dormant LOD path does not bypass the `xbrickmap` occupancy path cleanly.

Primary code references:

- `voxelrt/rt/gpu/manager_scene.go`
- `voxelrt/rt/gpu/manager_voxel.go`
- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- `voxelrt/rt/shaders/transparent_overlay.wgsl`
- `voxelrt/rt/shaders/particles_sim.wgsl`
- `voxelrt/rt/volume/xbrickmap.go`

## Benchmark Protocol

Every task that claims a performance win must use the same workload definition:

- Renderer module root:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko`
- Suggested visual workload when a desktop session is available:
  - `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox`
  - `env GOCACHE=/tmp/gekko3d-gocache go run .`
- Pin for each before/after comparison:
  - scene or level
  - camera position/path
  - resolution
  - warmup frame count
  - measured frame count or capture duration
  - whether timing is coarse frame time, CPU-side build/upload timing, or inferred from counters

If a task cannot measure frame time directly, it must report:

- removed probes
- removed buffer reads
- removed texture loads
- or reduced uploaded bytes

Important limitation:

- GPU timestamp queries are not available in the current Go WebGPU bindings used by this repo.
- Do not treat CPU timing as proof of shader hot-path improvement.
- Use CPU-side observability only for build/upload/rebuild work and coarse frame-level comparisons.

## Recommended Execution Order

Do the work in this order:

1. Replace modulo with mask-based wraparound in sector lookup.
2. Early-test `sector.brick_mask` before `BrickRecord` fetch in occupancy helpers.
3. Optionally add CPU-side upload/build observability if better coarse measurement is needed.
4. Introduce dense per-brick occupancy bits and shift empty/non-empty rejection out of the payload texture.
5. Investigate and, if justified, replace hashed sector lookup with direct object-local sector indexing.

Do not start Task 4 before Tasks 1 and 2 are complete.
Do not start Task 5 implementation before the Task 5 design spike is complete.

## Parallelization Rules

Parallel-safe now:

- Task 0 can run alone.
- Task 1 can run alone.
- Task 2 can run alone.

Not parallel-safe:

- Task 3A, 3B, and 3C all touch shared GPU layout and shader bindings.
- Task 5A and 5B both touch sector lookup contracts.

If using multiple agents, keep one agent per disjoint write set and merge in sequence.

## Task Breakdown For Agents

### Task 0: Optional CPU-side upload/build observability

Goal:

- Add coarse CPU-side observability for upload, rebuild, and dirty-churn work.

Expected impact:

- No direct performance gain.
- Helps explain build/upload costs and dirty-work churn.
- Does not directly validate shader hot-path improvements.

Own these files:

- `voxelrt/rt/app/profiler.go`
- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/manager_scene.go`
- minimal related debug output files only if required

Do not touch:

- voxel storage layout
- shader bind-group layout
- `BrickRecord`

Requirements:

- Add counters or scope timing for CPU-side work such as:
  - sector-grid rebuilds
  - dirty-sector and dirty-brick counts
  - payload upload counts or bytes
  - dense-occupancy upload counts or bytes if Task 3 lands later
- Keep instrumentation lightweight and optional in normal runs.
- Make timing source explicit in debug output.
- Do not present CPU timing as proof of shader traversal wins.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`

Acceptance criteria:

- CPU-side upload/build counters exist for coarse before/after comparison.
- Instrumentation makes its own limitations explicit.

Prompt seed:

```text
Add optional CPU-side observability for XBrickMap upload/build work.

Scope:
- Own only profiler and minimal renderer debug/output support code needed to compare later optimization tasks.

Requirements:
- Add cheap, explicit instrumentation for CPU-side upload/build/rebuild work.
- Do not change visible rendering behavior.
- Keep normal runtime overhead minimal.
- Do not claim shader-level proof from CPU timing.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 1: Replace modulo-based probe wrap with mask-based wrap

Goal:

- Remove unnecessary modulo operations from sector-grid hashing and probe wraparound in shader code.

Expected impact:

- Small but low-risk hot-path improvement.
- Reduces ALU cost on every sector lookup.

Own these files:

- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- `voxelrt/rt/shaders/transparent_overlay.wgsl`
- `voxelrt/rt/shaders/particles_sim.wgsl`
- minimal comments or assertions in `voxelrt/rt/gpu/manager_scene.go` if needed

Do not touch:

- `BrickRecord`
- payload atlas layout
- occupancy semantics

Requirements:

- Use `grid_mask` consistently for hash-table wraparound because `grid_size` is forced to a power of two.
- Keep current hash function semantics unless needed to preserve mask-based indexing correctness.
- Keep all passes using the same sector-lookup contract.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- No `% grid_size` remains in live shader sector-grid lookup code.
- All affected shader variants compile and tests pass.
- Visible behavior is unchanged.

Prompt seed:

```text
Optimize sector-grid wraparound in VoxelRT shaders by replacing modulo with mask-based indexing where grid_size is guaranteed power-of-two.

Scope:
- Own only the shader lookup helpers and minimal supporting comments/assertions if needed.

Requirements:
- Keep the existing sector-grid contract.
- Update all voxel-data consumers consistently.
- Do not change rendering behavior.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 2: Early-test sector brick mask before BrickRecord fetch

Goal:

- Avoid unnecessary `BrickRecord` reads when the sector's `brick_mask` already proves the brick is absent.

Expected impact:

- Moderate win in occupancy-heavy helpers.
- Benefits AO, normal estimation, shadow occupancy checks, and any path that calls `sample_occupancy_local`.

Own these files:

- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- `voxelrt/rt/shaders/transparent_overlay.wgsl`
- `voxelrt/rt/shaders/particles_sim.wgsl`

Do not touch:

- CPU voxel storage layout
- payload atlas upload code
- `BrickRecord`

Requirements:

- Check `sector.brick_mask` before computing or dereferencing the brick table entry.
- Apply the same logic to all occupancy helper variants, not only the main opaque pass.
- Keep the solid-brick fast path intact.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Occupancy helpers return early for absent bricks without touching `bricks[packed_idx]`.
- All affected shader variants stay behaviorally identical.
- GPU tests pass.

Prompt seed:

```text
Optimize VoxelRT occupancy helpers to reject absent bricks using sector.brick_mask before any BrickRecord fetch.

Scope:
- Own only WGSL occupancy helper code in the voxel-rendering shader set.

Requirements:
- Apply consistently across opaque, shadow, transparent, and particle consumers.
- Do not change payload or material semantics.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 3A: Freeze dense-occupancy data contract

Goal:

- Define the exact CPU and GPU data contract for per-brick dense occupancy before implementation work starts.

Expected impact:

- No direct performance gain.
- Prevents churn and merge conflicts in later implementation tasks.

Own these files:

- this document
- optionally a short design note under `gekko/docs/renderer/` if a separate note is clearer

Do not touch:

- runtime code
- shader code

Requirements:

- Decide exact representation for per-brick dense occupancy.
- Define whether the bitset lives inline in `BrickRecord`, in a side buffer, or in a dedicated occupancy buffer.
- Freeze indexing order and word packing.
- State how solid bricks interact with dense occupancy.
- State how empty bricks interact with dense occupancy.

Recommended default:

- Use a dedicated storage buffer for exact brick occupancy bits.
- Store `512` bits per non-solid brick as `16 x u32`.
- Keep `BrickRecord` small and use one new pointer/offset field into the dense occupancy buffer.
- Preserve the current micro mask as the coarse early-out layer.

Verification:

- Design review only

Acceptance criteria:

- A single approved dense-occupancy layout exists.
- CPU and WGSL indexing order is frozen in prose before implementation starts.

Frozen contract:

- Representation:
  - Use a dedicated storage buffer named `dense_occupancy_words`.
  - Store exact occupancy only for non-solid bricks.
  - Each dense brick entry is `512 bits = 16 x u32 = 64 bytes`.
  - The buffer is a flat `array<u32>` on the GPU and a flat `[]uint32`/packed byte upload on the CPU.
- `BrickRecord` change:
  - Extend `BrickRecord` by one `u32` field named `dense_occupancy_word_base`.
  - New `BrickRecord` field order:
    - `atlas_offset: u32`
    - `occupancy_mask_lo: u32`
    - `occupancy_mask_hi: u32`
    - `atlas_page: u32`
    - `flags: u32`
    - `dense_occupancy_word_base: u32`
  - New `BrickRecord` size: `24 bytes`.
  - `dense_occupancy_word_base` is the word index into `dense_occupancy_words`, not a byte offset.
  - Invalid sentinel: `0xFFFFFFFF`.
- Binding contract:
  - Add the dense occupancy buffer as a new voxel-data binding after the current lookup buffers rather than reordering existing bindings.
  - Freeze the intended WGSL resource as:
    - `@group(2) @binding(13) var<storage, read> dense_occupancy_words: array<u32>;`
  - Existing bindings `0..12` keep their current meaning.
- Indexing order:
  - Dense occupancy uses the same voxel linearization as brick payload upload order.
  - Voxel linear index inside a brick is:
    - `linear = x + y*8 + z*64`
  - Word selection:
    - `word_index = linear >> 5`
    - `bit_index = linear & 31`
  - Occupancy test:
    - occupied when `(dense_occupancy_words[dense_occupancy_word_base + word_index] & (1u << bit_index)) != 0`.
  - This ordering is frozen for both CPU packing and WGSL reads.
- Relationship to existing masks:
  - Keep `occupancy_mask_lo/hi` as the coarse `2x2x2` micro-mask early-out.
  - Dense occupancy is the exact per-voxel refinement layer for non-solid bricks.
  - Dense occupancy must never replace or reinterpret the existing micro-mask contract.
- Solid-brick semantics:
  - `BrickFlagSolid` remains authoritative.
  - Solid bricks do not allocate dense occupancy words.
  - Solid bricks must set `dense_occupancy_word_base = 0xFFFFFFFF`.
  - Shader logic must treat a solid brick as fully occupied without consulting the dense buffer.
- Empty-brick semantics:
  - Empty bricks are represented by absence in the sector `brick_mask`, as today.
  - Empty bricks do not allocate dense occupancy words.
  - Any zeroed or cleared GPU brick record must leave `dense_occupancy_word_base = 0xFFFFFFFF`.
  - There is no valid "allocated but empty" dense entry in v1.
- Allocation and lifetime:
  - Dense occupancy allocation granularity is exactly one brick entry (`16` words).
  - Allocation is owned by the GPU-side brick upload lifecycle, not by sectors or objects directly.
  - Re-upload of an unchanged non-solid brick may reuse its existing dense slot.
  - Brick replacement, brick deletion, solid<->non-solid transitions, and sector teardown must release or replace the dense slot in the same lifecycle path that already manages payload slots.
- CPU build rule:
  - CPU packing must derive dense occupancy from `brick.Payload[x][y][z] != 0`.
  - Dense occupancy for a non-solid brick must exactly match payload non-zero voxels.
  - `TryCompress`/solid-brick promotion may skip dense generation entirely once the brick is marked solid.
- Shader consumption rule for later tasks:
  - Traversal checks stay ordered as:
    - sector `brick_mask`
    - brick `occupancy_mask`
    - solid-brick fast path or dense occupancy test
    - payload fetch only after dense occupancy confirms the voxel is occupied for non-solid bricks
  - Task `3C` must preserve visible behavior and only use dense occupancy to remove unnecessary payload reads.

Prompt seed:

```text
Produce a concrete data contract for dense per-brick occupancy in VoxelRT.

Scope:
- Design only. No code changes.

Requirements:
- Freeze memory layout, indexing order, and solid/empty brick semantics.
- Prefer a design that minimizes BrickRecord churn and payload fetches.
- Keep XBrickMap editable semantics intact.
```

### Task 3B: CPU-side dense-occupancy build, upload, and tests

Goal:

- Build and upload exact per-brick occupancy bits for non-solid bricks.

Expected impact:

- Enables later shader tasks to reject empty voxels without texture reads.

Own these files:

- `voxelrt/rt/volume/xbrickmap.go`
- `voxelrt/rt/volume/xbrickmap_edit.go`
- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/gpu/manager_voxel.go`
- `voxelrt/rt/gpu/manager_voxel_test.go`
- `voxelrt/rt/volume/*_test.go` only as needed

Do not touch:

- main traversal shader logic beyond any required bind-group/layout placeholders

Requirements:

- Generate exact occupancy bits from brick payload for non-solid bricks.
- Keep solid-brick compression path working without redundant dense data where possible.
- Upload dense occupancy alongside existing brick and payload uploads.
- Handle allocation, lifetime, and invalidation correctly for dirty-brick updates and brick replacement.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Exact occupancy buffer exists on GPU for non-solid bricks.
- Dirty-brick updates keep dense occupancy in sync.
- Tests cover packing correctness and upload lifecycle.

Prompt seed:

```text
Implement CPU-side dense per-brick occupancy generation and GPU upload for VoxelRT.

Scope:
- Own volume storage packing plus GPU manager upload/allocation code and tests.

Requirements:
- Follow the frozen dense-occupancy contract.
- Preserve current XBrickMap edit semantics and solid-brick fast paths.
- Keep allocation and invalidation correct for dirty sectors and dirty bricks.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 3C: Shift empty/non-empty rejection from payload texture to dense occupancy

Goal:

- Make traversal and occupancy helpers use dense occupancy bits for exact empty-voxel rejection, and only fetch payload/material when a voxel is confirmed occupied.

Expected impact:

- High-value rendering win on sparse non-solid bricks.
- Reduces random `textureLoad` usage in hot paths.

Own these files:

- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- `voxelrt/rt/shaders/transparent_overlay.wgsl`
- `voxelrt/rt/shaders/particles_sim.wgsl`
- `voxelrt/rt/gpu/manager_render_setup.go`
- `voxelrt/rt/gpu/manager_shadow.go`
- `voxelrt/rt/gpu/manager_particles.go`

Do not touch:

- CPU-side dense-occupancy packing logic except for integration fixes

Requirements:

- Use micro mask first.
- Use dense occupancy second.
- Fetch payload/material only after dense occupancy confirms a hit candidate.
- Preserve current material and transparency behavior once a voxel is confirmed occupied.
- Update every voxel-data bind group consistently.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`

Acceptance criteria:

- Exact empty-voxel rejection no longer requires payload texture fetch.
- Payload fetches occur only for confirmed occupied voxels that need palette/material.
- All affected passes compile and tests pass.

Prompt seed:

```text
Integrate dense per-brick occupancy into VoxelRT shader traversal so empty/non-empty rejection happens in buffers before payload texture fetch.

Scope:
- Own shader-side voxel traversal/occupancy logic plus bind-group integration.

Requirements:
- Preserve visible behavior and material semantics.
- Update opaque, shadow, transparent, and particle voxel consumers consistently.
- Fetch payload/material only after dense occupancy confirms occupancy.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core
```

### Task 4: Validate whether micro mask still earns its keep after dense occupancy

Goal:

- Re-measure whether the coarse `64-bit` micro mask still improves performance once dense occupancy exists.

Expected impact:

- Either confirms the current two-stage rejection path or removes redundant per-voxel work.

Own these files:

- whichever files are required for an isolated benchmark or A/B flag

Do not touch:

- unrelated renderer systems

Requirements:

- Compare:
  - micro mask + dense occupancy
  - dense occupancy only
- Use the benchmark protocol from this document.
- Keep this task read-mostly unless data strongly justifies removing the micro mask path.

Verification:

- targeted test runs only

Acceptance criteria:

- A recommendation exists, backed by measurements, on whether the micro mask remains worthwhile.

Task 4 result:

- Date:
  - `2026-04-17`
- Measurement scope:
  - Added a focused occupancy A/B benchmark in `voxelrt/rt/volume/xbrickmap_task4_bench_test.go`.
  - This is a CPU-side proxy for occupancy-query work, not a direct frame-time or GPU timestamp capture.
  - Use it to compare:
    - branch + coarse micro-mask reject + dense occupancy
    - dense occupancy only
- Commands run:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko`
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume -run TestTask4MicroMaskDenseOccupancyStats -v`
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume -run '^$' -bench BenchmarkTask4Occupancy -benchmem -benchtime=300ms`
- Workloads:
  - `sparse_corners`
  - `clustered_core_4x4x4`
  - `surface_shell`
  - `checkerboard`
- Dense-check skip rates from the coarse micro mask:
  - `sparse_corners`: `87.5%`
  - `clustered_core_4x4x4`: `87.5%`
  - `surface_shell`: `12.5%`
  - `checkerboard`: `0.0%`
- CPU proxy timings:
  - `micro + dense`
    - `sparse_corners`: `1.767 ns/op`
    - `clustered_core_4x4x4`: `1.738 ns/op`
    - `surface_shell`: `2.392 ns/op`
    - `checkerboard`: `2.486 ns/op`
  - `dense only`
    - `sparse_corners`: `0.7474 ns/op`
    - `clustered_core_4x4x4`: `0.7456 ns/op`
    - `surface_shell`: `0.7458 ns/op`
    - `checkerboard`: `0.7452 ns/op`
- Interpretation:
  - The scalar CPU proxy favors `dense only` because it removes one branch and one coarse bit-test.
  - That proxy understates the value of the micro mask on GPU because it does not price in the cost of an extra storage-buffer read in the hot path.
  - The load-reduction signal is still meaningful: on sparse structured non-solid bricks, the micro mask avoids most dense-buffer checks before they happen.
- Recommendation:
  - Keep the micro mask for now.
  - Reason:
    - Sparse and clustered bricks still skip `87.5%` of dense checks.
    - Shell-like bricks still skip some work.
    - The worst case (`checkerboard`) is neutral on rejection power, but that alone is not strong enough evidence to delete the coarse mask from the GPU path.
  - Revisit only after a true frame-time comparison or GPU counter capture is available on representative scenes.

Prompt seed:

```text
Measure whether VoxelRT should keep the coarse microcell occupancy mask after dense per-brick occupancy is added.

Scope:
- Benchmark and analysis first.
- Only propose code removal if measurements are clear.

Requirements:
- Compare the two-stage and one-stage rejection strategies on the same workload.
- Report data, not intuition.
```

### Task 5A: Design spike for direct object-local sector indexing

Goal:

- Decide whether hashed sector lookup should be replaced for some or all objects with direct object-local indexing.

Expected impact:

- No direct performance gain in this task.
- Determines whether the largest remaining lookup cost should be addressed structurally.

Own these files:

- this document
- optionally a short renderer design note under `gekko/docs/renderer/`

Do not touch:

- runtime code

Requirements:

- Evaluate direct indexing memory cost versus current hash grid.
- Define when an object qualifies for dense direct indexing.
- Define fallback behavior for large or sparse objects.
- Define how object-local origin and bounds are packed into object params.

Recommended direction:

- Hybrid lookup:
  - dense direct indexing for compact object-local sector bounds
  - hash lookup fallback for very sparse or very large bounds

Verification:

- Design review only

Acceptance criteria:

- A documented go/no-go decision exists for hybrid direct sector indexing.
- Memory overhead and expected win are stated explicitly.

Design recommendation:

- Go for `Task 5B`, but only as a hybrid path.
- Do not replace the current hash grid for every object.
- Keep the direct path logically separate from the hash path even if both live in the same GPU buffer.
  - The direct path still operates on compact `u32` sector indices with `0xFFFFFFFF` as the empty sentinel.

Current baseline:

- The live hash grid in `manager_scene.go` uses:
  - `32 bytes` per `SectorGridEntry`
  - grid size `= next_pow2(max(1024, fallback_sector_count * 8))`
- That means the fallback hash path costs approximately:
  - `256 bytes` per live fallback sector in the nominal case
  - just under `512 bytes` per live fallback sector in the worst power-of-two rounding window
- The direct dense table would cost:
  - `4 bytes` per addressable sector cell
  - plus one small per-object metadata expansion in `ObjectParams`
  - plus padding to the enclosing `SectorGridEntry` storage format when the table is packed into the tail of `SectorGridBuf`

Break-even rule of thumb:

- Let:
  - `live_sectors = len(xbm.Sectors)`
  - `bounds_volume = dx * dy * dz` in sector space, where `dx`, `dy`, and `dz` are the object-local dense lookup extents
- Direct indexing is memory-favorable against the current `8x` hash grid when:
  - `bounds_volume <= 64 * live_sectors`
- That is the theoretical break-even line before power-of-two rounding and before accounting for the extra probe-loop work removed on the shader side.

Recommended qualification policy:

- Qualify an object for dense direct indexing only when all of the following are true:
  - the sector-space bounds are finite and computed from the current `XBrickMap.Sectors` keys
  - `bounds_volume <= 4096`
  - `bounds_volume <= 16 * live_sectors`
- Reason:
  - `4096` dense slots caps the direct table at `16 KiB` per object
  - the `16x` density rule is materially more conservative than the `64x` memory break-even line, which avoids using dense tables for very sparse objects where hash fallback remains the safer choice
- Expected qualifying shapes:
  - compact authored props
  - terrain chunks with mostly contiguous sector bounds
  - planet-tile-style objects with naturally bounded local sector ranges
- Expected fallback shapes:
  - very sparse kitbashed objects
  - objects with long thin bounds and low fill
  - unusually large edited structures whose dense sector bounds would exceed the `4096`-slot cap

Fallback behavior:

- Keep one shader-facing lookup helper:
  - `find_sector(...)`
- Dispatch inside that helper by per-object lookup mode:
  - `lookup_mode_hash = 0`
  - `lookup_mode_direct = 1`
- For `lookup_mode_hash`:
  - preserve the current global `sector_grid` probing path unchanged
- For `lookup_mode_direct`:
  - convert `(sx, sy, sz)` to object-local dense coordinates
  - reject out-of-bounds coordinates immediately
  - flatten the local coordinates to one dense-table index
  - return the stored sector index or `-1` if the slot contains `0xFFFFFFFF`

Expected win:

- The current hash path pays for:
  - hash computation
  - a probe loop of up to `16` iterations in shader code
  - one `32-byte` sector-grid read per probe
  - per-probe coordinate and base-id comparisons
- The direct path reduces that to:
  - one bounds check
  - one flatten operation
  - one compact sector-index word read from the packed `SectorGridBuf` tail
  - one sentinel comparison
- This should help most when rays or occupancy queries cross sector boundaries frequently:
  - primary-ray stepping through dense objects
  - shadow rays
  - occupancy-heavy normal or neighbor sampling
- Cached repeated accesses inside the same sector already mitigate some cost today, so the direct path is not expected to change every scene equally. The win is specifically about removing probe work when the lookup cannot reuse the cache.

Object-params packing decision:

- Expand `ObjectParams` from `96 bytes` to `128 bytes` in `Task 5B`.
- Keep the first `96 bytes` semantically unchanged.
- Append two new words of lookup metadata:
  - `direct_lookup_origin_and_mode: vec4<i32>`
    - `x = origin_sx`
    - `y = origin_sy`
    - `z = origin_sz`
    - `w = lookup_mode`
  - `direct_lookup_extent_and_base: vec4<u32>`
    - `x = extent_x`
    - `y = extent_y`
    - `z = extent_z`
    - `w = direct_sector_table_base`
- Rationale:
  - the current `96-byte` object params block already carries active terrain, planet, shadow, and emitter metadata
  - the single existing padding word is not enough for origin, bounds, mode, and table base
  - appending metadata keeps the migration explicit and avoids hidden reinterpretation of existing fields
- Negative sector coordinates remain correct because the shader will subtract the signed origin before converting to dense-table indices.

Implementation note for `Task 5B`:

- Keep the shader-facing lookup model as two logical tables:
  - the front of `SectorGridBuf` remains the hash-probed `SectorGridEntry` array
  - the tail of `SectorGridBuf` stores packed direct-lookup `u32` words
- Reason:
  - the renderer already sits close to WebGPU compute-stage storage-buffer limits in voxel passes
  - packing the direct table into the existing sector-grid buffer avoids adding another storage binding and keeps the pipelines valid
- `direct_sector_table_base` in `ObjectParams` must be expressed in packed-word units from the start of the combined `SectorGridBuf`, not from the start of the appended tail.
- If future renderer work frees a storage binding budget, a dedicated direct-lookup buffer is still a cleaner architectural option, but it is not required for the current implementation.

Prompt seed:

```text
Produce a design recommendation for replacing hashed sector lookup with object-local direct sector indexing in VoxelRT.

Scope:
- Design only. No code changes.

Requirements:
- Compare direct indexing, hash lookup, and a hybrid policy.
- Quantify memory tradeoffs and expected lookup savings.
- Keep current object/renderer ownership intact.
```

### Task 5B: Implement hybrid direct sector indexing behind existing lookup interface

Goal:

- Remove hash probing for qualifying objects by switching lookup to direct object-local indexing while preserving a fallback path.

Expected impact:

- Potentially large win for compact objects with many sector lookups.

Own these files:

- `voxelrt/rt/gpu/manager_scene.go`
- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- `voxelrt/rt/shaders/transparent_overlay.wgsl`
- `voxelrt/rt/shaders/particles_sim.wgsl`
- tests under `voxelrt/rt/gpu` as needed

Do not touch:

- voxel edit semantics
- payload atlas logic unless required by integration

Requirements:

- Keep one shader-facing lookup helper interface.
- Allow per-object selection between direct and hash-backed lookup.
- Keep all shader consumers on the same object-params contract.
- Preserve correctness for negative sector coordinates and sparse objects.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Acceptance criteria:

- Qualifying objects use direct lookup with no probe loop.
- Non-qualifying objects continue to render correctly through fallback lookup.
- Tests pass and measured lookups improve on representative workloads.

Representative-workload measurement pass completed on April 17, 2026:

- Harness:
  - added an env-gated benchmark mode to `examples/testing-vox/main.go`
  - `GEKKO_BENCH_WARMUP_SECONDS` controls warmup duration
  - `GEKKO_BENCH_SECONDS` controls capture duration and enables the benchmark mode
  - `GEKKO_BENCH_LABEL` tags the emitted result line
  - `GEKKO_XBM_FORCE_HASH_LOOKUP=1` forces the hash-only fallback for A/B comparison
- Workload commands used:
  - `cd /Users/ddevidch/code/go/gekko3d`
  - `env GOCACHE=/tmp/gekko3d-gocache GEKKO_BENCH_WARMUP_SECONDS=5 GEKKO_BENCH_SECONDS=10 GEKKO_BENCH_LABEL=hybrid go run ./examples/testing-vox`
  - `env GOCACHE=/tmp/gekko3d-gocache GEKKO_BENCH_WARMUP_SECONDS=5 GEKKO_BENCH_SECONDS=10 GEKKO_BENCH_LABEL=hash-only GEKKO_XBM_FORCE_HASH_LOOKUP=1 go run ./examples/testing-vox`
- Captured results on the pinned `testing-vox` scene:
  - hybrid:
    - `avg_fps=10.00`
    - `avg_frame_ms=100.00`
    - `last_fps=7.23`
    - profiler snapshot: `Buffer Update=0.109 ms`, `Scene Commit=0.107 ms`, `Scene: Instances=0.010 ms`, `Scene: Voxel=0.026 ms`
  - hash-only:
    - `avg_fps=10.00`
    - `avg_frame_ms=100.00`
    - `last_fps=7.15`
    - profiler snapshot: `Buffer Update=0.131 ms`, `Scene Commit=0.117 ms`, `Scene: Instances=0.014 ms`, `Scene: Voxel=0.029 ms`
- Interpretation:
  - this environment is presentation-limited for the sample run, so the coarse end-to-end FPS metric stayed flat between modes
  - the per-frame profiler snapshot trends slightly in favor of the hybrid direct-lookup path, but the delta is small enough that it should be treated as directional only, not a strong performance claim
  - the pass still closes the non-optional Task 5B reporting requirement: the hybrid path runs the representative workload correctly, shows no regression against forced hash fallback, and remains the recommended implementation
- Remaining follow-up:
  - no additional implementation work is required for Task 5B
  - if a cleaner performance signal is needed later, rerun the same harness on a non-present-limited desktop/GPU setup

Prompt seed:

```text
Implement hybrid object-local sector lookup in VoxelRT so compact objects use direct indexing while large or sparse objects keep the existing fallback path.

Scope:
- Own GPU manager scene packing, object params, shader lookup helpers, and tests.

Requirements:
- Keep one shader-facing lookup API.
- Preserve correctness for all current objects.
- Maintain a fallback path for sparse or oversized bounds.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core
- env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

## Recommended First Agent Wave

If you want the highest value with the lowest merge risk, start with:

1. Task 1
2. Task 2
3. Optional Task 0 if you want better coarse CPU-side observability

Then stop, compare coarse frame behavior on the same workload, and reassess before starting Task 3.

## Artifacts To Produce During Execution

For each completed task, produce:

- a short before/after measurement note
- exact files changed
- verification command results
- residual risks or unanswered questions

If a task changes data layout or bind-group layout, also produce:

- a brief compatibility note naming every pass updated

## Skills Applied

- `run-confidence-gate`
  - formalized confidence and whether SME consultation is required
- `generate-spec`
  - produced a deterministic, phase-based execution artifact for agent work
- `apply-workflow-norms`
  - kept the plan scannable, explicit about risks, and reviewer-friendly
