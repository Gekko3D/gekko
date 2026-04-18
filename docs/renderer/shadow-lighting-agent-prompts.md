# Shadow And Lighting Agent Prompts

This document records the agent prompts used for the shadow and lighting optimization pass.

Primary plan:

- [`shadow-lighting-narrow-optimization-plan.md`](shadow-lighting-narrow-optimization-plan.md)

## Current Status

- Agent A / Task 1: completed
- Agent B / Task 2: completed
- Agent C / Task 3: completed
- Agent D / Task 4: not launched

Use this document as historical context plus a ready-to-run prompt for Task 4 if profiling later justifies it.

Rules for all agents:

- you are not alone in the codebase
- do not revert edits made by other agents
- stay inside your assigned file ownership unless a small test/helper change is strictly required
- preserve current rendered output
- do not redesign renderer architecture
- run the listed verification commands before handoff
- report changed files explicitly in your final handoff

## Historical Launch Order

These were launched in parallel:

1. Agent A: Task 1
2. Agent B: Task 2
3. Agent C: Task 3

Task 4 was intentionally not launched.

## Current Recommendation

- do not launch any new agent from this doc unless fresh profiling shows Task 4 is still worthwhile
- if needed, launch only Agent D

## Agent A

Status:

- Completed

Name:

- `tile-cull-gating`

Ownership:

- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/pass_contribution.go`
- related tests under:
  - `voxelrt/rt/app`
  - `voxelrt/rt/gpu`

Do not edit:

- `voxelrt/rt/gpu/manager_tiled_lighting.go`
- `voxelrt/rt/gpu/shadow_*`

Prompt:

```text
Implement Task 1 from docs/renderer/shadow-lighting-narrow-optimization-plan.md.

You are not alone in the codebase. Do not revert edits made by other agents. Stay inside your ownership unless a minimal related test change is strictly required.

Task:
- Skip tiled-light cull when there are no local lights.

Own these files:
- voxelrt/rt/app/app_frame.go
- voxelrt/rt/gpu/pass_contribution.go
- minimal related tests only

Requirements:
- Gate tiled-light cull on point/spot light presence, not total scene light count.
- Preserve directional-only rendering behavior.
- Prevent stale tile-light data from leaking across frames when cull is skipped.
- Do not change lighting shader behavior.

Verification:
- cd /Users/ddevidch/code/go/gekko3d/gekko
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/app

Deliverables:
- implement the change
- add/update tests
- report changed files and any residual risk
```

## Agent B

Status:

- Completed

Name:

- `tile-pressure-reduction`

Ownership:

- `voxelrt/rt/gpu/manager_tiled_lighting.go`
- related tests under:
  - `voxelrt/rt/gpu`

Do not edit:

- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/pass_contribution.go`
- `voxelrt/rt/gpu/shadow_*`

Prompt:

```text
Implement Task 2 from docs/renderer/shadow-lighting-narrow-optimization-plan.md.

You are not alone in the codebase. Do not revert edits made by other agents. Stay inside your ownership unless a minimal related test change is strictly required.

Task:
- Reduce tiled-light pressure without changing coverage.

Own these files:
- voxelrt/rt/gpu/manager_tiled_lighting.go
- minimal related tests only

Requirements:
- Reduce unnecessary fullscreen classification for local lights.
- Preserve exact lighting coverage, especially near screen edges.
- Keep current profiler counters useful.
- Add one narrow diagnostic counter only if truly needed.
- Do not change deferred-lighting shader behavior.

Verification:
- cd /Users/ddevidch/code/go/gekko3d/gekko
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu

Deliverables:
- implement the change
- add/update tests
- report changed files, expected metric impact, and residual risk
```

## Agent C

Status:

- Completed

Name:

- `shadow-caster-reduction`

Ownership:

- `voxelrt/rt/gpu/shadow_metadata.go`
- `voxelrt/rt/core/scene.go` only if strictly required
- related tests under:
  - `voxelrt/rt/gpu`
  - `voxelrt/rt/core`

Do not edit:

- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/manager_tiled_lighting.go`
- `voxelrt/rt/gpu/manager_shadow.go`

Prompt:

```text
Implement Task 3 from docs/renderer/shadow-lighting-narrow-optimization-plan.md.

You are not alone in the codebase. Do not revert edits made by other agents. Stay inside your ownership unless a minimal related test change is strictly required.

Task:
- Reduce shadow caster count before GPU shadow work.

Own these files:
- voxelrt/rt/gpu/shadow_metadata.go
- voxelrt/rt/core/scene.go only if strictly required
- minimal related tests only

Requirements:
- Preserve visible shadow contribution.
- Reduce ShadowObjects only where culling is provably safe.
- Prefer improving existing shadow volume filtering and grouping over inventing a new shadow system.
- Do not change shadow sampling or softness behavior.

Verification:
- cd /Users/ddevidch/code/go/gekko3d/gekko
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/core

Deliverables:
- implement the change
- add/update tests
- report changed files, expected ShadowCasters impact, and residual risk
```

## Agent D

Status:

- Ready if needed
- Not launched

Name:

- `shadow-submit-cpu`

Ownership:

- `voxelrt/rt/gpu/manager_shadow.go`
- related tests under:
  - `voxelrt/rt/gpu`

Do not edit:

- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/manager_tiled_lighting.go`
- `voxelrt/rt/gpu/shadow_metadata.go`

Prompt:

```text
Implement Task 4 from docs/renderer/shadow-lighting-narrow-optimization-plan.md, but treat it as conditional work.

You are not alone in the codebase. Do not revert edits made by other agents. Stay inside your ownership unless a minimal related test change is strictly required.

Task:
- Reduce CPU-side shadow submission overhead.

Own these files:
- voxelrt/rt/gpu/manager_shadow.go
- minimal related tests only

Requirements:
- Reduce repeated CPU bucketing/serialization overhead.
- Preserve shadow update ordering and semantics.
- Do not change shader behavior or scheduling policy.
- Do not merge if Tasks 1 to 3 already remove enough frame cost and this no longer looks worthwhile.

Verification:
- cd /Users/ddevidch/code/go/gekko3d/gekko
- env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu

Deliverables:
- either implement the change or return an investigation note explaining why it should wait
- report changed files and residual risk
```

## Merge Guidance

Historical merge order:

1. Agent A
2. Agent B
3. Agent C
4. Agent D only if still justified by measurement

After each merge, compare:

- `Tile Light Cull`
- `Shadows`
- `Lighting`
- `LightListEntriesAvg`
- `LightListEntriesMax`
- `ShadowCasters`
- `ShadowUpdates`

Current note:

- only Agent D remains relevant, and only if shadow-pass CPU submission still shows up in profiling
