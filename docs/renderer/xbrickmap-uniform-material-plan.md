# XBrickMap Uniform-Material Payload Reduction Plan

This document started as the execution plan for reducing `XBrickMap` payload upload and payload texture usage for non-solid bricks that do not need per-voxel material variation.

Current status:

- implemented
- runtime contract finalized
- upload-frame counters added

Use this document as the design and outcome record for the change.
For the live runtime contract, also see:

- `docs/renderer/runtime.md`
- `reports/2026-04-18-xbrickmap-uniform-material-status.md`

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

- Dense occupancy removed exact empty/non-empty checks from the payload texture path, but non-solid bricks still upload a full `8x8x8` payload texture and still require payload reads when a hit needs material identity.
- Many sparse authored bricks are likely uniform in material even though they are sparse in occupancy.

Goal:

- Add a `uniform-material sparse brick` mode that keeps sparse occupancy but avoids per-voxel payload storage and payload texture fetches.
- Preserve the current visual contract and keep `XBrickMap` CPU-authoritative.
- Reduce payload atlas pressure and queue upload volume without destabilizing traversal.

Explicit v1 boundaries:

- Do not redesign `XBrickMap`.
- Do not remove dense occupancy.
- Do not add compressed variable-length payload formats.
- Do not alter visible shading semantics or material resolution rules.

## Confidence Gate

Confidence: High

Why:

- The optimization is local and extends the existing brick state machine rather than replacing it.
- The branch conditions are clear:
  - solid brick
  - sparse uniform-material brick
  - sparse per-voxel-material brick
- The current code already supports dense occupancy and solid-brick fast paths, so the new mode fits the established design.

Key assumptions:

- A meaningful fraction of sparse non-solid bricks are uniform in material.
- The extra shader branch is cheaper than uploading and reading full payload texture data for those bricks.
- A fixed-size but explicit `BrickRecord` is acceptable long-term.

What would raise confidence further:

- Scene-level measurement of how many sparse non-solid bricks collapse to one material.
- Before/after counts for payload atlas writes and payload-backed bricks.

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
  - do not change the rendering backend or shader binding model without explicit need

## Global Contracts And Guardrails

Apply to all tasks in this plan:

- Preserve current visible behavior for solid bricks and mixed-material sparse bricks.
- Keep dense occupancy as the exact occupancy source for sparse bricks.
- Only skip payload atlas allocation/upload when every occupied voxel in the brick resolves to the same non-zero palette index.
- Preserve transparency semantics:
  - uniform sparse bricks must resolve transparency using the brick-level palette/material exactly as solid bricks already do.
- Keep all voxel-data shader consumers synchronized:
  - opaque
  - shadow
  - transparent overlay
  - particles occupancy checks if affected

End-of-phase commands:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Additional commands by task are listed below.

## Frozen Terms

These names are fixed for this plan:

- `solid brick`:
  - full `8x8x8` occupancy and one palette/material for the entire brick
- `uniform sparse brick`:
  - sparse occupancy, one palette/material for every occupied voxel, no per-voxel payload storage
- `payload brick`:
  - sparse occupancy with per-voxel palette/material variation requiring payload storage
- `dense occupancy`:
  - exact `8x8x8` occupancy bitset already used for sparse hit confirmation

No additions or redefinitions without updating this document.

## Canonical Brick-State Contract

### Brick modes

Freeze these brick modes for v1:

- `BrickFlagSolid`
- `BrickFlagUniformMaterial`

Rules:

- `Solid`:
  - whole brick is occupied
  - `material_index` stores the single palette index
  - no dense occupancy allocation required
  - no payload atlas allocation required
- `UniformMaterial`:
  - sparse occupancy
  - dense occupancy required
  - `material_index` stores the single palette index for all occupied voxels
  - no payload atlas allocation required
- default sparse mode:
  - sparse occupancy
  - dense occupancy required
  - payload atlas allocation required
  - `payload_offset` and `payload_page` point into the payload atlas

### Detection rule

For a non-solid brick:

1. scan only occupied voxels
2. if all occupied voxels resolve to the same non-zero palette index:
   - mark `BrickFlagUniformMaterial`
3. otherwise:
   - keep the payload-backed sparse mode

### Shader rule

After dense occupancy confirms a sparse voxel hit:

- if `Solid`:
  - palette = `material_index`
- else if `UniformMaterial`:
  - palette = `material_index`
- else:
  - palette = `load_u8(...)`

## Persistence And Allocation Rules

- `BrickRecord` stays fixed-size.
- `BrickRecord` is now `32 bytes` and uses explicit fields:
  - `material_index`
  - `payload_offset`
  - `occupancy_mask_lo`
  - `occupancy_mask_hi`
  - `payload_page`
  - `flags`
  - `dense_occupancy_word_base`
- `dense_occupancy_word_base` remains the exact occupancy source for sparse bricks.
- Payload allocation lifetime:
  - release payload slots when a brick becomes `Solid` or `UniformMaterial`
- Dense occupancy lifetime:
  - keep dense occupancy for `UniformMaterial` and payload-backed sparse bricks
  - release dense occupancy for `Solid` bricks

## Benchmark Protocol

Every task that claims a performance win must use the same workload definition:

- Renderer module root:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko`
- Suggested visual workload when a desktop session is available:
  - `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox`
  - `env GOCACHE=/tmp/gekko3d-gocache go run .`

If precise GPU timings are unavailable, report:

- `VoxelPayBrk`
- `VoxelUniBrk`
- `VoxelPaySkip`
- `VoxelPayBytes`

## Implementation Outcome

Implemented results:

1. `BrickFlagUniformMaterial` was added and is maintained on the CPU-authoritative `Brick`.
2. Uniform sparse bricks now skip payload atlas allocation and payload texture upload.
3. Shader consumers resolve palette from `material_index` for `Solid` and `UniformMaterial` bricks.
4. `BrickRecord` was migrated from the older overloaded layout to an explicit `32-byte` layout.
5. Upload-frame observability counters were added:
   - `VoxelUniBrk`
   - `VoxelPayBrk`
   - `VoxelPaySkip`
   - `VoxelPayBytes`

Verification used during implementation:

- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app`

Notes from `examples/testing-vox`:

- the demo benchmark runs successfully in env-gated benchmark mode
- the new counters are upload-frame counters, so steady-state capture windows can legitimately report zeros after startup uploads finish

## Recommended Execution Order

Historical note: the steps below are retained as the original execution sequence.

Do the work in this order:

1. Freeze the new brick-mode and GPU-record contract.
2. Add CPU-side detection and allocation changes.
3. Update shader consumers to branch on the new mode before payload fetch.
4. Add observability and scene-level counting if needed.

Do not start Task 2 before Task 1 is complete.
Do not start Task 3 before Task 2 lands.

## Parallelization Rules

Parallel-safe now:

- Task 1 can run alone.

Not parallel-safe:

- Task 2 and Task 3 both touch brick-mode semantics.
- Task 3 touches all shader consumers and related bind/layout assumptions.

If using multiple agents, merge in sequence.

## Task Breakdown For Agents

### Task 1: Freeze uniform-sparse brick contract

Goal:

- Define the exact CPU and GPU contract for `BrickFlagUniformMaterial` before code changes start.

Expected impact:

- No direct performance gain.
- Prevents semantic drift between CPU upload code and shader consumers.

Own these files:

- this document
- optionally a short addendum in `docs/renderer/runtime.md` if the brick-state contract is documented there

Do not touch:

- runtime code

Requirements:

- Freeze the new flag name and meaning.
- State exactly when dense occupancy exists.
- State exactly when payload atlas allocation is skipped.
- State exactly how `material_index`, `payload_offset`, and `payload_page` are interpreted in each brick mode.

Verification:

- design review only

Acceptance criteria:

- A single approved contract exists for solid, uniform sparse, and payload-backed sparse bricks.

Prompt seed:

```text
Freeze the brick-state contract for a new uniform sparse material mode in VoxelRT.

Scope:
- Design only. No code changes.

Requirements:
- Define exact semantics for flags, explicit `BrickRecord` fields, dense occupancy, and payload allocation.
- Keep the contract compatible with the current XBrickMap and shader traversal model.
```

### Task 2: CPU-side uniform-sparse detection and upload changes

Goal:

- Detect sparse uniform-material bricks on upload and skip payload atlas allocation/upload for them.

Expected impact:

- Lower payload atlas pressure.
- Lower queue upload volume for uniform sparse bricks.

Own these files:

- `voxelrt/rt/volume/xbrickmap.go`
- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/gpu/manager_voxel.go`
- `voxelrt/rt/gpu/manager_voxel_test.go`
- `voxelrt/rt/volume/*_test.go` only if needed

Do not touch:

- shader logic except for any temporary constants required to keep builds consistent

Requirements:

- Add `BrickFlagUniformMaterial`.
- Add a helper that classifies sparse non-solid bricks as:
  - uniform sparse
  - payload-backed sparse
- Release payload slots when a brick transitions into `Solid` or `UniformMaterial`.
- Keep dense occupancy for uniform sparse bricks.
- Add tests covering:
  - uniform sparse detection
  - mixed sparse fallback
  - slot release when payload is no longer needed

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Uniform sparse bricks no longer allocate or upload payload atlas data.
- Payload-backed sparse bricks continue to work exactly as before.
- Tests cover classification and allocation lifecycle.

Prompt seed:

```text
Implement CPU-side detection and upload changes for a new uniform sparse brick mode in VoxelRT.

Scope:
- Own volume helpers, GPU upload logic, and tests.

Requirements:
- Detect sparse bricks whose occupied voxels all share one palette index.
- Skip payload atlas allocation/upload for those bricks.
- Keep dense occupancy exactness and preserve current behavior for mixed-material sparse bricks.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/volume
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 3: Shader-side uniform-sparse material resolution

Goal:

- Teach all voxel-data shader consumers to resolve material from brick state before falling back to payload fetch.

Expected impact:

- Removes payload texture fetches for confirmed hits in uniform sparse bricks.

Own these files:

- `voxelrt/rt/shaders/gbuffer.wgsl`
- `voxelrt/rt/shaders/shadow_map.wgsl`
- `voxelrt/rt/shaders/transparent_overlay.wgsl`
- `voxelrt/rt/shaders/particles_sim.wgsl` only if material resolution or occupancy logic needs update

Do not touch:

- CPU-side upload logic except for integration fixes

Requirements:

- After dense occupancy confirms a sparse voxel hit:
  - `Solid` => use brick-level palette
  - `UniformMaterial` => use brick-level palette
  - payload-backed sparse => call `load_u8(...)`
- Preserve transparency behavior and palette resolution exactly.
- Keep all shader consumers on the same brick-mode contract.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core`

Acceptance criteria:

- Uniform sparse bricks do not use payload texture fetch for palette/material resolution.
- Mixed sparse bricks still use payload fetch exactly as before.
- Shader tests and compile-sensitive GPU tests pass.

Prompt seed:

```text
Implement shader-side support for uniform sparse bricks in VoxelRT so dense occupancy confirms the hit and brick state decides whether payload fetch is needed.

Scope:
- Own voxel-data shader consumers.

Requirements:
- Preserve current visible behavior and transparency semantics.
- Resolve palette from brick state for solid and uniform sparse bricks.
- Only use load_u8 for mixed-material sparse bricks.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core
```

### Task 4: Optional observability and rollout note

Goal:

- Add low-cost counters so scene-level wins are visible without GPU timestamp support.

Expected impact:

- No direct performance gain.
- Makes the optimization easier to validate and tune.

Own these files:

- `voxelrt/rt/gpu/manager_voxel.go`
- profiler/debug output files only if required
- optional doc update in `docs/renderer/runtime.md`

Do not touch:

- shader logic unless strictly necessary for reporting

Requirements:

- Report at least:
  - number of uniform sparse bricks
  - number of payload-backed sparse bricks
  - payload uploads skipped
- Document that `GEKKO_XBM_FORCE_HASH_LOOKUP` affects sector lookup only, not this brick-mode optimization.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Coarse counters exist for scene-level validation.
- The rollout note is explicit about what this optimization does and does not bypass.

Prompt seed:

```text
Add coarse observability for uniform sparse brick optimization in VoxelRT.

Scope:
- Own lightweight counters and minimal documentation.

Requirements:
- Report how many sparse bricks avoid payload upload.
- Keep runtime overhead low.
- Make it clear that this is coarse validation, not GPU hot-path timing.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

## Recommended First Agent Wave

If you want the highest value with the lowest merge risk, start with:

1. Task 1
2. Task 2
3. Task 3

Then stop and compare:

- payload-backed sparse brick count
- uniform sparse brick count
- payload atlas upload volume

before deciding whether Task 4 is worth adding.

## Artifacts To Produce During Execution

For each completed task, produce:

- a short note on expected or measured win
- exact files changed
- verification command results
- residual risks

If a task changes brick flags or shader interpretation, also produce:

- a brief compatibility note naming every shader consumer updated

## Skills Applied

- `generate-spec`
  - produced a deterministic, phase-based execution artifact for agent work
