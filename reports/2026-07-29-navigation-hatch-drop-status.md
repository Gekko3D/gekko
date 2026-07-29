# STATUS_REPORT

## Goal

Prevent navigation from baking hatch drops whose landing capsule intersects
the fully open door.

## Scope

- Primary owner: authored navigation graph generation.
- Consumer: ActionGame bot locomotion.
- Non-scope: changing HL1 import or runtime traversal collision.

## Change

- Navigation doors retain the existing moving-brush open offset.
- Inferred hatch drops are rejected when the destination standing cylinder
  overlaps any fully open door in the physical hatch group.
- Manifests without `open_offset` preserve their previous behavior.

## Verification

- `go test ./content -run '^TestConnectNavGraphDoors'`: pass.
- `go test ./content/...`: existing `TestCompressNavGraphRegions/required_action_splits_regions`
  fixture failure (`jump traversal is required`).
- Crossfire navigation rebake: pass with zero hard errors. The generated
  `hl1_moving_func_door_7` entry has open offset `[0, -1.0160013, 0]`, its
  invalid inferred drop is absent, and the real `hl1_func_ladder_1` links remain.
- `go test ./src/modules/startup`: existing failures in the locally modified
  locomotion tests `TestActionGameSharedActorContinuesDropPastRouteAcceptance`
  and `TestActionGameNPCNavigationOpensHatchBeforeLadder`.
- `go test ./src/modules/startup -run '^$'`: pass.
- Manual ActionGame playtest remains required.
