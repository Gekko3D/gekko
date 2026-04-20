# Engine Runtime

This document explains how the core `gekko` app loop works outside the renderer-specific runtime.

Use this page when you need to answer:

- when systems run
- when entity and component mutations become visible
- how system arguments are resolved
- what a module can safely assume during install and execution

## Core Types

The runtime is centered on:

- `App`
  - owns stages, systems, resources, ECS storage, and buffered command queues
- `Commands`
  - buffered mutation API exposed to systems and module installers
- `Stage`
  - named execution phase such as `Prelude` or `Render`
- `Module`
  - installer that registers resources and systems into an app

Start reading in:

- `app.go`
- `app_builder.go`
- `schedule.go`
- `commands.go`

## Default Stage Order

The app builder installs stages in this order:

1. `Prelude`
2. `PreUpdate`
3. `Update`
4. `PostUpdate`
5. `PreRender`
6. `Render`
7. `PostRender`
8. `Finale`

Every stage ends with `FlushCommands()`.

That flush timing is load-bearing. If a system adds or removes entities or components, the change is not immediately visible to later code in the same system call. It becomes visible after the stage flush.

## Stateless and Stateful Apps

`NewApp()` creates a stateless app by default.

Optional stateful behavior is enabled through:

- `UseStates(initialState, finalState)`
- `UseTargetFPS(fps)` for optional main-loop frame pacing
- `OnEnter(state)`
- `OnExecute(state)`
- `OnExit(state)`

In a stateless app:

- systems registered with `RunAlways()` or without an explicit state run every frame in their stage

In a stateful app:

- stateless systems still run every frame
- stateful systems run only for their configured state and phase
- `Commands.ChangeState(...)` buffers a state change that is applied after the current execute pass

## Frame Pacing

`App.Run()` is uncapped by default.

Optional pacing can be enabled through:

- `UseTargetFPS(fps)`

Behavior:

- `fps <= 0` disables pacing
- positive values cap the main loop by sleeping after frame work completes when time remains in the frame budget
- the cap does not guarantee throughput; if one frame takes longer than budget, the app runs below target
- pacing affects the main app loop only
- physics still runs on its own fixed ticker
- if both vsync and target FPS are active, the slower limiter wins in practice

## System Registration Rules

Systems are registered through:

```go
app.UseSystem(System(mySystem).InStage(Update).RunAlways())
```

Important behavior:

- default stage is `Update`
- a system without explicit state is treated as stateless
- stateful systems require a stateful app
- `UseStage(...)` can insert extra stages before or after existing ones

## Dependency Injection

System parameters are resolved reflectively from pointer arguments.

Rules:

- `*Commands`
  - synthesized automatically for every call
- any other pointer type
  - must exist in `app.resources`
- missing dependency
  - causes a panic with the system name and unresolved type

There is no separate DI container beyond `app.resources`.

That means module ordering matters whenever one module expects another module's resources to exist by the time a system runs.

Practical rule:

- if a system asks for `*SomeResource`, some module must install that resource explicitly
- internal helper state created inside another subsystem does not satisfy ECS DI unless it is also registered in `app.resources`
- if your system depends on `*SpatialHashGrid`, install `SpatialGridModule` or add an equivalent resource yourself; `PhysicsModule`'s internal simulation grid is not the same thing

## Resource Model

Resources are registered through:

- `cmd.AddResources(...)` during module install or runtime

Internally:

- resources are keyed by their non-pointer concrete type
- adding the same resource type twice panics

Common examples:

- `*Time`
- `*Input`
- `*AssetServer`
- `*VoxelRtState`
- `*WaterInteractionState`
- `*PhysicsWorld`
- `*PhysicsProxy`

## Command Buffering

`Commands` does not mutate ECS storage immediately.

Buffered operations:

- `AddEntity`
- `AddComponents`
- `RemoveComponents`
- `RemoveEntity`
- `ChangeState`

Flush order is:

1. entity removals
2. entity additions
3. component removals
4. component additions

This ordering is deliberate:

- adding components to a removed entity in the same stage does not revive it
- remove-then-add of the same component type in one stage behaves predictably

## ECS Visibility Rules

Because commands are buffered:

- code inside one system call should not assume `AddEntity` is immediately queryable
- systems later in the same stage still do not see those changes until the stage flush
- a manual `cmd.app.FlushCommands()` should be treated as exceptional implementation detail, not a default workflow

When a feature depends on newly spawned hierarchy or metadata being visible, the usual fix is stage placement, not forcing early mutation.

## Module Install Model

`app.build()`:

1. creates the default stage list
2. initializes per-stage system storage
3. constructs one `Commands`
4. calls `Install(app, commands)` for each module in order

So module install is the time to:

- add resources
- register systems
- insert custom stages if needed

It is not the time to assume any per-frame state already exists.

## Common Failure Modes

- reflective dependency panic
  - a system asked for a resource type no module registered
- “my new entity is missing”
  - it was added this stage and has not flushed yet
- hierarchy or physics appears one frame behind
  - the system was placed in the wrong stage relative to the producer or consumer
- state transition side effects happen late
  - state changes are buffered until after execute

## Large-World Runtime Pattern

`gekko` remains a local-space engine runtime.

That means:

- `TransformComponent`, broadphase AABBs, renderer submission, and physics all stay `float32`
- large-world authoritative coordinates should live in the game/runtime layer above the engine
- the game should project authoritative coordinates into local ECS space at one explicit boundary, then sync dynamic runtime results back out intentionally

This pattern is now exercised by SpaceSim:

- high-precision world state lives in SpaceSim-owned contracts
- one active floating-origin frame defines the current local projection
- rebasing is handled in game code by recomputing projected local transforms, then refreshing any engine-side caches that depend on those local transforms

Do not treat raw ECS transforms as global gameplay truth in large-world consumers.

## Practical Reading Order

If you are debugging engine behavior:

1. `app_builder.go`
2. `schedule.go`
3. `app.go`
4. `commands.go`
5. the module that owns the affected systems

For module ownership and stage placement by subsystem, continue with [`modules.md`](modules.md).
