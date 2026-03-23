# Workflow: Add a Module

Use this playbook when adding a new `gekko` module.

## Goal

A good module should:

- own a coherent subsystem
- install its own resources
- register its systems in deliberate stages
- avoid hidden dependency assumptions

## Steps

1. Choose the owner file.

Prefer a dedicated `mod_<name>.go` or `mod_<name>_module.go` file.

2. Define the module type.

Typical shape:

```go
type MyModule struct {
    // optional config
}
```

3. Implement `Install(app, cmd)`.

Inside install:

- add resources first
- then register systems
- keep stage placement explicit

4. Choose stage placement intentionally.

Ask:

- does this need fresh input?
- does it need hierarchy to have already resolved?
- does it produce data another system needs this frame?
- does it need to run before render or after gameplay updates?

5. Document dependencies.

If your systems expect resources from other modules, make that obvious in:

- module comments
- docs
- install ordering in consumers

6. Add targeted tests.

At minimum:

- resource registration sanity if nontrivial
- behavior tests for the installed systems
- stage/order-sensitive tests if the module depends on buffered visibility

## Design Rules

- Prefer one resource per subsystem root rather than many ad hoc globals.
- Avoid reading resources that no owning module is guaranteed to install.
- Avoid add/remove component churn every frame unless structural change is the feature.
- Keep module install deterministic and side-effect-light.

## Common Mistakes

- forgetting to add the resource a system parameter expects
- relying on immediate visibility of `Commands` mutations
- placing a consumer system in the same stage as its producer when it really needs the next stage
- burying cross-module assumptions in code without docs

## After Adding the Module

Update:

- [`../engine/modules.md`](../engine/modules.md)
- [`../engine/runtime.md`](../engine/runtime.md) if the module introduces a new important runtime pattern
- [`../engine/verification.md`](../engine/verification.md) if it adds a new verification path

Then verify the direct consumer module, not just `gekko`.
