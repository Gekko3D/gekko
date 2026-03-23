# Physics Integration

This page documents the physics runtime at the engine-integration level.

Read it when touching:

- `PhysicsModule`
- rigid bodies and collider sync
- collision events
- voxel-aware physics
- grounded player or destruction interactions that depend on physics timing

## Architecture

The physics path is asynchronous.

Main pieces:

- `PhysicsModule`
  - installs resources and ECS-facing sync systems
- `PhysicsWorld`
  - simulation configuration and world state
- `PhysicsProxy`
  - snapshot/results bridge between ECS and the async simulation loop
- `physicsLoop(...)`
  - background simulation goroutine

Important files:

- `mod_physics_module.go`
- `mod_physics_loop.go`
- `mod_physics_collision.go`
- `mod_vox_physics.go`

## Stage Ownership

`PhysicsModule` installs:

- `PhysicsPullSystem` in `PreUpdate`
- `PhysicsPushSystem` in `PostUpdate`

That split is intentional.

Frame shape:

1. `PhysicsPullSystem`
  - applies the latest completed simulation results to ECS-visible transforms and rigid-body state
2. gameplay and engine update systems run
3. `PhysicsPushSystem`
  - snapshots current ECS state back into the async simulation

If a change needs to affect the next simulation step, it usually needs to be visible by `PostUpdate`.

## Async Snapshot Model

Physics does not mutate ECS directly from the background goroutine.

Instead:

- ECS state is captured into `PhysicsSnapshot`
- the simulation loop consumes that snapshot
- completed simulation data is published as `PhysicsResults`
- `PhysicsPullSystem` copies results back to ECS components

This avoids cross-thread ECS mutation but means the physics world is always at least one synchronization step away from immediate ECS changes.

## What `PhysicsPullSystem` Owns

`PhysicsPullSystem`:

- drains the latest results
- captures collision events
- interpolates between previous and current physics poses
- writes:
  - `TransformComponent`
  - rigid-body velocity and angular velocity
  - sleeping state
  - idle time

If visual transforms look wrong while physics state is correct, start here before changing the simulation loop.

## What `PhysicsPushSystem` Owns

`PhysicsPushSystem`:

- queries ECS rigid-body state
- builds the outgoing snapshot
- pushes gravity and dt
- tells the async loop what the current authoritative ECS-side body state is

If gameplay changes are not reaching physics, start here.

## Collision Model

The simulation currently uses:

- broadphase spatial hash grid
- narrow-phase OBB collision
- voxel-aware collision paths when voxel grids are present
- sequential impulse solving with cached contact impulses

Important note:

- voxel collision is not just “mesh collision with more triangles”
- there is explicit logic for voxel grids and voxel primitive iteration in `mod_physics_collision.go`

## Voxel Physics Layer

`mod_vox_physics.go` sits at the engine boundary between voxel content and the generic physics loop.

It prepares physics-facing voxel grid data and caches so voxel objects can participate in collision without flattening the whole renderer scene into generic geometry first.

If a bug mentions:

- destructible voxel bodies
- voxel collision shape mismatch
- stale voxel collision after edits

start in `mod_vox_physics.go` before touching the generic loop.

## Collision Events

Collision events are buffered through `PhysicsProxy`.

The important public event type is:

- `PhysicsCollisionEvent`

Event kinds:

- `enter`
- `stay`
- `exit`

`PhysicsProxy.DrainCollisionEvents()` is the handoff point for gameplay systems that want discrete collision events instead of raw body state.

## Common Failure Modes

- body moves visually but physics does not react
  - snapshot push path is wrong
- physics reacts but render pose lags or snaps
  - pull/interpolation path is wrong
- voxel collisions miss or over-penetrate
  - voxel-specific collision path or voxel-scale assumptions are wrong
- results differ by frame hitch
  - dt handling or clamping assumptions changed
- collision events duplicate or disappear
  - proxy buffering or pair-tracking changed

## Verification Strategy

After physics integration changes:

- `env GOCACHE=/tmp/gekko3d-gocache go test .`

For compile-only subpackage checks:

- `go test ./physics/...`

If grounded player, destruction, or voxel collision is involved, verify the affected consumer module too.

For broader guidance, see [`verification.md`](verification.md).
