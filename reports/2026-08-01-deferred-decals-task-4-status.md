# Deferred Decals Task 4 Status

- Date: 2026-08-01
- Owner: ActionGame / VoxelRT bridge
- Related plan: [`deferred-decals-plan.md`](../docs/renderer/deferred-decals-plan.md)

## Goal

Add the ActionGame-owned bounded decal pool and opt in ActionGame to the generic
VoxelRT decal feature.

## Scope

- Retain up to 4096 renderer decal records, evicting the oldest record.
- Copy frame-lifetime impact data before the next actuation-stage clear.
- Remove records for deleted targets, same-target destruction carves, and arena
  reset transitions.
- Submit the retained list through the shared renderer bridge and reuse the
  existing sprite-atlas cache.

Not included: blood/soot placement rays, atlas expansion, or visual GPU checks.

## Invariant

Decal retention remains ActionGame-owned; it creates neither ECS entities nor
voxel edits. Consumers without the registered `DecalFeature` clear decal input
and do not record the decal pass.

## Confidence

- Confidence: High
- Why: existing impact events, destruction-carve metadata, and the generic
  decal replacement API provide the required boundaries.
- SME alignment required: No

## Verification

- Passed: `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup`
- Passed: `cd /Users/ddevidch/code/go/gekko3d/actiongame && env GOCACHE=/tmp/gekko3d-gocache go test .`
- Passed: `git -C /Users/ddevidch/code/go/gekko3d/gekko diff --check`

No unit tests were added, per request. Manual visual validation remains for the
placement and atlas work in task 5.
