# Direct Sector Lookup Threshold Tuning Plan

This document is the live execution plan for safely tuning `XBrickMap` direct-sector lookup thresholds in `voxelrt`.

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

- `voxelrt` hybrid sector lookup for `XBrickMap`

Problem:

- The renderer now supports both:
  - hash-probed sector lookup
  - direct object-local sector lookup
- The choice between them is currently controlled by fixed constants:
  - `DirectSectorLookupMaxCells`
  - `DirectSectorLookupDensityMax`
- Those defaults are heuristic, not tuned against representative project content.

Goal:

- Measure how the current thresholds behave on real scenes.
- Add safe temporary controls for experimentation.
- Change defaults only if the data clearly supports it.

Explicit v1 boundaries:

- Do not redesign lookup structures.
- Do not add runtime-adaptive lookup mode switching.
- Do not change shader layout or object-param layout.
- Do not make lookup mode selection unstable frame-to-frame.

## Confidence Gate

Confidence: High

Why:

- This is a policy-tuning pass over existing behavior.
- The work is mostly observability plus optional default constant updates.
- Rollback is trivial if measurements are inconclusive.

Key assumptions:

- Different content sets may want different threshold values.
- Coarse scene-level counters are enough to evaluate the current qualification policy.
- Temporary env overrides are the safest way to experiment without repeated code edits.

What would raise confidence further:

- Sample data from at least two or three representative scenes.

Consult SME dev required?: No

## Technology Stack

- Language: Go
- Renderer GPU API: WebGPU via `github.com/cogentcore/webgpu/wgpu`
- Core modules in scope:
  - `voxelrt/rt/gpu`
- Constraint:
  - keep this as a measurement-and-tuning pass, not a structural optimization project

## Global Contracts And Guardrails

Apply to all tasks in this plan:

- Preserve current lookup semantics unless a default threshold change is explicitly justified by collected data.
- Keep lookup mode selection deterministic for a given scene and configuration.
- Avoid adding persistent debug noise or heavy runtime overhead.
- Env overrides must be optional and default to current behavior when unset.
- If measurements are inconclusive, do not change defaults.

End-of-phase commands:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

## Frozen Terms

These names are fixed for this plan:

- `direct lookup candidate`:
  - an `XBrickMap` whose sector bounds are evaluated against threshold policy
- `bounds volume`:
  - `(extentX * extentY * extentZ)` in sector space
- `density ratio`:
  - `boundsVolume / liveSectors`
- `qualified map`:
  - a map assigned to direct lookup mode
- `fallback map`:
  - a map that stays on hash lookup

No additions or redefinitions without updating this document.

## Benchmark And Sampling Protocol

For each representative scene or workload, record:

- scene name
- camera setup if relevant
- number of direct-lookup-qualified maps
- number of hash-fallback maps
- total candidate maps
- per-map:
  - live sector count
  - bounds volume
  - density ratio
  - qualified/fallback outcome
- coarse frame behavior if available

If visual runs are available, use:

- `cd /Users/ddevidch/code/go/gekko3d/examples/testing-vox`
- `env GOCACHE=/tmp/gekko3d-gocache go run .`

If only code-level validation is available, limit claims to:

- qualification distribution
- table size impact
- expected lookup-mode changes under alternate thresholds

## Recommended Execution Order

Do the work in this order:

1. Add low-cost counters and reporting.
2. Add env overrides for experimental thresholds.
3. Sample representative scenes.
4. Decide whether default constants should change.

Do not change defaults before Task 3 is complete.

## Parallelization Rules

Parallel-safe now:

- Task 1 can run alone.

Not parallel-safe:

- Task 2 depends on Task 1 if reporting should include the override values.
- Task 3 depends on Tasks 1 and 2.
- Task 4 depends on collected measurements.

If using multiple agents, merge in sequence.

## Task Breakdown For Agents

### Task 1: Add coarse direct-lookup qualification counters

Goal:

- Report how current direct-lookup qualification behaves on real content.

Expected impact:

- No direct performance gain.
- Makes the current policy visible and reviewable.

Own these files:

- `voxelrt/rt/gpu/manager_scene.go`
- minimal related debug/output files only if needed

Do not touch:

- shaders
- object param layout
- lookup qualification semantics

Requirements:

- Report at least:
  - total candidate maps
  - direct-qualified maps
  - hash-fallback maps
  - per-map live sector count
  - per-map bounds volume
  - per-map density ratio
- Keep overhead low.
- Make reporting optional or debug-oriented if needed.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Current qualification behavior can be inspected without changing code again.

Prompt seed:

```text
Add coarse counters/reporting for direct sector lookup qualification in VoxelRT.

Scope:
- Own only the CPU-side lookup-qualification path and minimal reporting support.

Requirements:
- Report candidate count, qualified count, fallback count, bounds volume, live sectors, and density ratio.
- Keep runtime overhead low.
- Do not change lookup policy yet.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 2: Add temporary env overrides for threshold experimentation

Goal:

- Allow threshold experiments without repeated code edits.

Expected impact:

- No direct performance gain.
- Makes policy experiments cheap and reversible.

Own these files:

- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/gpu/manager_scene.go`
- optional doc note in `docs/renderer/runtime.md` or this file if needed

Do not touch:

- shaders
- bind-group layout
- object params

Requirements:

- Add optional env overrides for:
  - `DirectSectorLookupMaxCells`
  - `DirectSectorLookupDensityMax`
- Defaults must remain unchanged when env vars are unset or invalid.
- Keep parsing deterministic and simple.

Suggested names:

- `GEKKO_XBM_DIRECT_MAX_CELLS`
- `GEKKO_XBM_DIRECT_DENSITY_MAX`

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Thresholds can be overridden safely via environment variables.
- Invalid env values do not crash or silently corrupt behavior.

Prompt seed:

```text
Add temporary env overrides for direct sector lookup threshold tuning in VoxelRT.

Scope:
- Own only CPU-side threshold constants and parsing.

Requirements:
- Support overriding max cell count and density threshold.
- Preserve current behavior when env vars are unset.
- Keep the implementation simple and deterministic.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

### Task 3: Collect representative scene samples

Goal:

- Gather enough data to decide whether the current defaults are appropriate.

Expected impact:

- No direct performance gain.
- Produces the evidence needed for a safe default change or a justified no-op.

Own these files:

- no code changes required unless light reporting adjustments are needed
- produce notes or a short report artifact if requested

Do not touch:

- lookup implementation

Requirements:

- Sample at least two or three representative scenes or workloads.
- Compare current defaults against at least one looser and one tighter override set.
- Record qualification distribution and any coarse frame behavior that is available.

Verification:

- measurement only

Acceptance criteria:

- A short data summary exists showing whether more maps should use direct lookup, fewer should, or current defaults are already reasonable.

Prompt seed:

```text
Collect representative scene data for VoxelRT direct sector lookup threshold tuning.

Scope:
- Measurement only unless tiny reporting changes are needed.

Requirements:
- Compare current defaults with at least one tighter and one looser threshold configuration.
- Report qualification counts, bounds volume, live sectors, density ratio, and any coarse frame observations.
- Do not change defaults yet.
```

### Task 4: Decide and, only if justified, update default thresholds

Goal:

- Change the hardcoded defaults only if the sampled data clearly supports it.

Expected impact:

- Better default lookup-mode policy for project content.

Own these files:

- `voxelrt/rt/gpu/manager.go`
- optional docs note in `docs/renderer/runtime.md` or this file

Do not touch:

- shaders
- object param layout
- lookup algorithms

Requirements:

- Summarize the data that justifies the change.
- Update defaults only if the win is clear and the memory tradeoff is acceptable.
- If data is weak or mixed, keep current defaults and document that choice.

Verification:

- `cd /Users/ddevidch/code/go/gekko3d/gekko`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu`

Acceptance criteria:

- Either:
  - defaults are changed with an explicit rationale
- or:
  - defaults remain unchanged with an explicit rationale

Prompt seed:

```text
Use collected scene data to decide whether VoxelRT direct sector lookup defaults should change.

Scope:
- Own only the default threshold constants and minimal accompanying documentation.

Requirements:
- Change defaults only if the data clearly supports it.
- Otherwise leave them unchanged and document why.
- Do not change the lookup algorithm itself.

Verification:
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
```

## Recommended First Agent Wave

If you want the safest path, start with:

1. Task 1
2. Task 2
3. Task 3

Only do Task 4 if the measurements show a clear better default.

## Artifacts To Produce During Execution

For each completed task, produce:

- exact files changed
- verification command results
- a short note on what the data means

For Task 3 and Task 4, also produce:

- the tested threshold values
- the qualification distribution per sampled scene
- the final recommendation

## Skills Applied

- `generate-spec`
  - produced a deterministic, phase-based execution artifact for agent work
