# STATUS_REPORT

## Metadata

- Owner: Codex
- Date: 2026-07-08
- Project/Area: authored character animation / ActionGame traversal
- Related Links: `importers/hl1/mdl.go`, `docs/content/asset-format.md`, `../actiongame/src/modules/startup/player_avatar.go`
- Reviewers Requested: gameplay and animation owner

## Executive Summary

- Goal: add durable traversal clips for HL1 humanoid assets and use them for controller-driven climbing.
- Current state: `llmngs/models/barney.mdl` is Source MDL v48. The HGrunt variants in both Crossfire and Subtransit contain `source_a_climbmount`, `source_a_climbloop`, `source_a_climbdismount`, `source_pipe_jump`, and `source_jump_water1`; those existing generated clips must be rebaked with the corrected importer before visual review.
- Progress: Source v48 inline and sectioned blocks decode; Source-only helper leaves remain in the target bind pose; incompatible mapped bones still fail explicitly. Retargeting maps the Source bind skeleton onto explicit target rest-basis metadata and derives an automatic retarget rest pose from unambiguous matched bone segments, without action-frame or idle-clip calibration. Sequence bone weights are honored and unsupported Source composition fails explicitly.
- Ask: implementation is authorized by the user. Manual third-person review remains required after baking.

## Scope

- In: Source v48 inline/sectioned animation decode; validated same-name/hierarchy transfer onto an existing authored rig; explicit player mantle traversal using the collision controller; root-locked authored clips.
- Out: Source mesh/material import, runtime retargeting, skinning, a general animation graph, and level re-import.

## Proposed Change

- Added a Source-animation-only importer/baker under `importers/source` and the `sourcemdlanim` CLI. It transfers matching bone motion through source/target bind-basis corrections, applies sequence bone weights, and leaves unmatched target bones in their authored bind transforms.
- Unsupported external animation blocks fail rather than silently emitting bind poses.
- Blended, additive/delta, automatic-layer, IK, local-context, world-space, realtime, cycle-pose, and local-hierarchy sequences also fail explicitly until their final Source pose can be evaluated faithfully.
- Added ActionGame’s explicit-jump mantle action. Gameplay validates and follows a collision-safe path; clips are presentation only.

## Invariant

- Generated tracks target existing authored bone-group IDs, retain controller authority over root movement, and never permit an unvalidated landing position.

## Testing Plan

- Importer: compile and inspect Source v48 direct/sectioned decoding and generated asset validation; no new importer tests per owner direction.
- Engine: `env GOCACHE=/tmp/gekko3d-gocache go test ./importers/source/...` and root package tests.
- Consumer: `env GOCACHE=/tmp/gekko3d-gocache go test ./...` from `actiongame`.
- Manual: run ActionGame in third-person with HGrunt; test low ledge, chest-height box, and water-pipe exit.

## Verification Run

- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1` from `gekko`.
- Passed: targeted controller/traversal tests and `go test ./importers/source -count=1` from `gekko` (the Source importer package currently has no test files).
- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test . -count=1` and `go build ./src/modules/startup` from `actiongame`.
- Manual smoke: Crossfire initialized in stateful mode with `characters.hgrunt.b0.b0.s0` selected.
- Not completed: interactive third-person traversal review. `actiongame/src/modules/startup` tests are currently blocked by its pre-existing unused `math` import in `startup_test.go`; it was not changed here.

## Risks & Mitigations

- Source has more animation variants than this first content requires. Mitigation: support v48 in-MDL data and section tables, then fail explicitly for external animation blocks.
- Named bones can share labels but differ structurally. Mitigation: require each animated donor bone and its mapped parent to match the target authored hierarchy.

# END_STATUS_REPORT
