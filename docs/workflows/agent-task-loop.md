# Workflow: Agent Task Loop

Use this playbook when an agent receives a change request and needs to converge on the right files, docs, and verification path quickly.

## Goal

An agent task is on the right track when it can answer three questions early:

- what subsystem owns the behavior
- what invariant must remain true after the edit
- what is the smallest meaningful verification command

## 1. Classify the Request

Sort the task before editing.

Use these buckets:

- engine runtime:
  - app stages
  - modules
  - ECS behavior
  - resource wiring
  - physics integration
- renderer:
  - `voxelrt`
  - render passes
  - scene extraction
  - GPU resources
- authored content:
  - `.gkasset`
  - `.gkset`
  - `.gklevel`
  - terrain
  - imported worlds
  - world deltas
- editor boundary:
  - `gekko-editor/src/formats`
  - preview integration
  - editor-specific assumptions about engine schemas
- docs-only:
  - canonical docs under `docs/`
  - `README.md`
  - `AGENTS.md`

If the change spans more than one bucket, mark the primary owner first and list the affected consumers second.

## 2. Read the Right Things First

Start with the smallest doc set that matches the bucket.

### Engine runtime

Read:

- [`../engine/runtime.md`](../engine/runtime.md)
- [`../engine/modules.md`](../engine/modules.md)
- [`../engine/ecs.md`](../engine/ecs.md)
- [`../engine/physics.md`](../engine/physics.md) when simulation is involved

Then inspect:

- `app.go`
- `app_builder.go`
- the relevant `mod_*.go` files

### Renderer

Read:

- [`../renderer/overview.md`](../renderer/overview.md)
- [`../renderer/runtime.md`](../renderer/runtime.md)
- [`../renderer/change-guide.md`](../renderer/change-guide.md)
- [`../renderer/verification.md`](../renderer/verification.md)

Then inspect the corresponding `mod_voxelrt_client*.go` bridge files before editing low-level `voxelrt/rt/...` code.

### Authored content

Read:

- [`../content/game-assets.md`](../content/game-assets.md)
- [`../content/levels.md`](../content/levels.md)
- [`../content/streaming-and-worlds.md`](../content/streaming-and-worlds.md)
- [`../content/asset-format.md`](../content/asset-format.md)

Then inspect:

- `content/*.go`
- `content/*_io.go`
- `content/*_validation.go`

### Editor boundary

Read:

- [`../editor/integration.md`](../editor/integration.md)
- the matching content doc under `../content/`

Then inspect:

- `gekko-editor/main.go`
- `gekko-editor/src/formats/...`
- the relevant editor module

## 3. State the Invariant Before Editing

Write down the rule you cannot break.

Typical examples:

- command-buffered ECS mutations are not visible until the next flush
- a module must install every resource its systems request
- eager level spawn is not the same thing as streamed runtime
- editor schema assumptions must match engine serialization and validation
- renderer scene extraction and GPU resource recreation must stay in sync

If you cannot express the invariant in one or two lines, you are probably still reading the wrong layer.

## 4. Choose the Smallest Verification Step

Use [`../engine/verification.md`](../engine/verification.md) and [`../renderer/verification.md`](../renderer/verification.md) as the source of truth.

Default commands:

- root package runtime changes:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test .`
- content schema and validation:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- ECS internals compile check:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./ecs/...`
- physics compile check:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./physics/...`
- broad engine refactor final sweep:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Cross-module checks are required when:

- engine content schemas changed and the editor consumes them
- shared runtime behavior changed and a consumer app depends on it
- renderer bridge behavior changed and a voxel demo or app exercises it

## 5. Update Canonical Docs When Behavior Moves

Update docs when you change:

- stage order or module ownership:
  - [`../engine/runtime.md`](../engine/runtime.md)
  - [`../engine/modules.md`](../engine/modules.md)
- content schema or runtime semantics:
  - the matching pages under `../content/`
- verification expectations:
  - [`../engine/verification.md`](../engine/verification.md)
  - [`../renderer/verification.md`](../renderer/verification.md)
- engine/editor boundary behavior:
  - [`../editor/integration.md`](../editor/integration.md)

Do not leave the only correct explanation stranded in code comments or commit history.

## 6. Handoff Checklist

Before handing off a task, make sure the result tells the next agent:

- what changed
- what invariant was preserved or intentionally changed
- what was verified
- what was not verified
- whether a consumer module still needs checking

## Fast Decision Table

- adding a new module:
  - read [`add-module.md`](add-module.md)
- extending a content schema:
  - read [`add-content-type.md`](add-content-type.md)
- changing runtime behavior without new schema:
  - start with [`../engine/runtime.md`](../engine/runtime.md)
- changing streamed levels, terrain, or imported worlds:
  - start with [`../content/streaming-and-worlds.md`](../content/streaming-and-worlds.md)
- changing level semantics:
  - start with [`../content/levels.md`](../content/levels.md)
- changing renderer extraction or pass wiring:
  - start with [`../renderer/change-guide.md`](../renderer/change-guide.md)
