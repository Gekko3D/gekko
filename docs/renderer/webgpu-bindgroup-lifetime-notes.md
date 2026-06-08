# WebGPU Bind Group Lifetime Notes

Date: 2026-06-08

This note records the renderer-side changes and diagnostics made while chasing
native WebGPU validation crashes during imported-world streaming. It is intended
as a handoff for future work, especially if the engine migrates away from the
current `github.com/cogentcore/webgpu` fork or changes native WebGPU bindings.

## Context

The crash appeared under action-game stress runs on the imported HL1 `gasworks`
level while chunks and sector proxies were streaming in. The recurring native
panic was:

```text
Error in wgpuQueueSubmit: Validation Error

Caused by:
  In a set_bind_group command
    BindGroup with '<label>' label is invalid
```

Observed labels included:

- unnamed bind groups before labels were added
- `Transparent Scene BG0`
- `GBuffer Scene BG0`

The action-game run used the streaming metrics logs under `reports/`, usually
with:

```sh
GEKKO_ACTIONGAME_LEVEL=../gekko-editor/assets/gasworks_128.gklevel
GEKKO_STREAMING_RADIUS=6
GEKKO_STREAMING_COLLISION_RADIUS=2
GEKKO_STREAMING_DESTRUCTION_RADIUS=1
GEKKO_STREAMING_KEEP_RADIUS=8
GEKKO_STREAMING_PREFETCH_RADIUS=7
GEKKO_STREAMING_METRICS_INTERVAL_MS=500
GEKKO_STREAMING_MAX_COMMITS_PER_FRAME=1
GEKKO_STREAMING_MAX_COMMIT_MS=4
```

## Working Theory

The engine frequently grows and replaces GPU buffers while streaming. Bind
groups created before a replacement can still be referenced by command encoders
or by feature/core cached pass state. Native WebGPU validation reports the bind
group label when the bind group itself or a resource referenced by it has become
invalid by submit time.

The renderer is single-threaded at the engine stage level:

1. `PreRender` runs ECS-to-renderer sync and `RtApp.Update()`.
2. `Render` records and submits `RtApp.Render()`.

So the leading theory is not an update/render data race. The risk is stale
cached WebGPU handles and release ordering across frames.

## WebGPU Binding Diagnostics

No lasting changes were made to `~/code/go/webgpu`.

The PR diagnostic attempted to use `cogentcore/webgpu#13` because it appeared
to address memory/lifetime issues in the bindings. That branch was not a quick
drop-in for this engine:

- it first failed around missing vendored platform files
- after local adjustments, it exposed API drift such as renamed or missing
  WebGPU wrapper types and methods
- the workspace was restored to the existing forked binding afterward

This result does not prove PR #13 or upstream `main` is unusable. It only means
the current engine code is coupled to the present binding API and would need a
small migration pass before those bindings can be tested fairly.

If testing another binding revision, treat it as an A/B diagnostic:

1. isolate the dependency change in a temporary branch or worktree
2. run compile tests before changing engine lifetime code
3. if it compiles, rerun the same gasworks stress command and compare crash
   labels and timing
4. restore the dependency if it does not compile quickly

## Engine-Level Changes Made

### Bind Group Labels

Bind group descriptors were labelled so native validation errors identify the
pass-level owner. Relevant labels include:

- `Transparent Scene BG0`
- `Transparent Voxel BG1`
- `Transparent Inputs BG2`
- `Transparent Tiles BG3`
- `GBuffer Scene BG0`
- `GBuffer Textures BG1`
- `GBuffer Voxel BG2`
- `Lighting Scene BG0`

When a validation panic names a label, start from the matching
`Create*BindGroups` function in `voxelrt/rt/gpu/`.

### Transparent Scene Bind Group Tracking

Files:

- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/app/feature_transparency.go`
- `voxelrt/rt/gpu/manager_alloc_test.go`
- `voxelrt/rt/app/feature_registry_test.go`

The transparent overlay now records the exact source buffers used for
`Transparent Scene BG0`:

- camera buffer
- transparent instance buffer
- transparent BVH buffer
- lights buffer
- shadow-layer params buffer

The transparency feature treats its bind groups as stale when the stored source
buffers no longer match the current manager buffers or when
`SceneBindingRevision` changes. Rebuilding the transparent bind groups fixed an
earlier `Transparent Scene BG0` stale-handle crash during stress testing.

### GBuffer Scene Bind Group Tracking

Files:

- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/gpu/manager_render_setup.go`
- `voxelrt/rt/app/app_frame.go`
- `voxelrt/rt/gpu/manager_alloc_test.go`

`GBuffer Scene BG0` now records the source buffers used when it is created:

- camera buffer
- visible instance buffer
- visible BVH buffer

Before recording the G-buffer pass, `recordGBufferPass` checks whether
`GBuffer Scene BG0` still matches the current source buffers. If it is stale,
the G-buffer bind groups are rebuilt before `SetBindGroup`.

The profiler also records `GBufferBG0Current` so logs can show whether the pass
had to refresh this bind group.

Important status: this source-buffer tracking alone did not stop a later
`GBuffer Scene BG0` validation crash in
`reports/streaming-gasworks-128-gbuffer-bg0-refresh.log`.

### Retired Bind Group Fencing

Files:

- `voxelrt/rt/gpu/manager_alloc.go`
- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/gpu/manager_render_setup.go`
- `voxelrt/rt/gpu/manager_alloc_test.go`

The manager already had delayed retirement for replaced buffers. Bind groups
now also participate in the retirement system. Replaced bind groups are not
released immediately; they are associated with the next queue submission and
released only after the submission is observed complete, with a frame-delay
fallback for unsubmitted resources.

### Retired Bind Group Buffer Pinning

Files:

- `voxelrt/rt/gpu/manager_alloc.go`
- `voxelrt/rt/gpu/manager.go`
- `voxelrt/rt/gpu/manager_render_setup.go`
- `voxelrt/rt/gpu/manager_alloc_test.go`

The latest engine-side hardening lets a retired bind group pin the buffers it
was created from. A retired buffer is not released while any retired bind group
still records it as a source buffer. Retirement now advances bind groups before
buffers, so a bind group gets the first chance to release before its source
buffers are considered.

Current pinned bind groups:

- `GBuffer Scene BG0`
- `Transparent Scene BG0`

This is deliberately narrower than pinning every resource globally. It encodes
the WebGPU ownership rule for the two scene bind groups already implicated by
validation labels.

Important status: this pinning patch passed automated tests, but still needs a
manual gasworks GPU stress run before being considered validated.

## Verification Already Run

After the G-buffer tracking and bind-group pinning changes:

```sh
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/app -count=1
env GOCACHE=/tmp/gekko3d-gocache go test ./...

cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Automated tests only validate source-buffer tracking, stale detection, and
retirement bookkeeping. They do not prove native WebGPU lifetime correctness.

## Pending Manual GPU Check

Run:

```sh
cd /Users/ddevidch/code/go/gekko3d/actiongame
GEKKO_ACTIONGAME_LEVEL=../gekko-editor/assets/gasworks_128.gklevel \
GEKKO_STREAMING_RADIUS=6 \
GEKKO_STREAMING_COLLISION_RADIUS=2 \
GEKKO_STREAMING_DESTRUCTION_RADIUS=1 \
GEKKO_STREAMING_KEEP_RADIUS=8 \
GEKKO_STREAMING_PREFETCH_RADIUS=7 \
GEKKO_STREAMING_METRICS_INTERVAL_MS=500 \
GEKKO_STREAMING_MAX_COMMITS_PER_FRAME=1 \
GEKKO_STREAMING_MAX_COMMIT_MS=4 \
go run . 2>&1 | tee ../reports/streaming-gasworks-128-bindgroup-buffer-pinning.log
```

Visual/GPU criteria:

- no `wgpuQueueSubmit` validation panic
- no persistent holes after full chunks replace sector proxies
- low-LOD sector proxies should eventually yield to detailed chunks
- destruction edits should still persist through the imported-world delta path

If it crashes, record the exact bind group label and nearby streaming metrics.

## Migration Notes For Other WebGPU Bindings

If migrating to a different Go WebGPU binding, preserve these engine-level
invariants:

- bind groups must not be released immediately after replacement if any command
  buffer may still reference them
- source buffers used by a retired bind group must not be released before the
  retired bind group itself is safe to release
- bind-group builders must label descriptors wherever the binding supports it
- scene-buffer growth must trigger bind-group invalidation for every pass that
  reads those buffers
- `App.Update()` owns scene-buffer replacement and dependent bind-group rebuilds
- `App.Render()` should only record pass work against bind groups that are
  current for that frame
- queue-submission fences should be preferred over frame-count-only release
  delays when the binding exposes submission completion

Specific wrappers/API surface to re-check during migration:

- queue submission return value and completion polling
- buffer and bind-group release semantics
- whether bind groups retain strong references to buffers internally
- descriptor labels and whether validation reports them
- pipeline layout and bind-group layout retrieval APIs
- `WholeSize` and buffer binding size conventions
- command encoder/pass encoder lifetime and submit behavior

If the new binding gives reliable object lifetime ownership, the engine-side
pinning may still be harmless, but it should be reviewed for unnecessary memory
retention under heavy streaming.

## Open Questions

- Does the latest bind-group buffer pinning remove the remaining
  `GBuffer Scene BG0` crash?
- Are `GBuffer Voxel BG2` and other voxel-reading bind groups also vulnerable
  when voxel atlas buffers grow, or are their existing rebuild paths sufficient?
- Should retired bind-group pinning be generalized to all bind groups that
  reference replaceable buffers, or kept scoped to observed crash labels?
- Is the current forked `webgpu` binding releasing native handles earlier than
  upstream `cogentcore/webgpu` would?
- Would an upstream binding version expose better queue-completion or ownership
  semantics that let this engine simplify retirement?

