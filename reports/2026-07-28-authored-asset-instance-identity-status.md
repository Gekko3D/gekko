# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-07-28
- Project/Area: authored asset runtime
- Reviewers Requested: engine/runtime owner

## Executive Summary
- Goal: prevent runtime systems from mixing entities from separate spawns of the same authored asset.
- Scope: add spawned-root ownership to `AuthoredAssetRefComponent`, expose one ownership predicate, and migrate ActionGame avatar grounding.
- Non-scope: changing serialized `.gkasset` IDs or adding a second generated instance-ID system.
- Current state: implemented and verified across engine, ActionGame, and editor.
- Confidence: High.
- SME alignment required?: No.

## Invariant
- `AssetID` identifies the authored document and may repeat across spawned instances.
- `RootEntity` identifies the one runtime spawn that owns an authored item.

## Proposed Change
- Populate `AuthoredAssetRefComponent.RootEntity` in the shared spawn path.
- Add an engine predicate for exact instance ownership, with hierarchy fallback for manually constructed legacy refs.
- Document the distinction and use the predicate in ActionGame grounding.

## Verification
- Passed: `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test .`
- Passed: `cd actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup -run Avatar`
- Passed: `cd actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'`
- Passed: `cd gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Partial: `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
  - authored-asset and root packages passed
  - unrelated `content/TestCompressNavGraphRegions/required_action_splits_regions` failed alongside pre-existing navigation worktree changes

# END_STATUS_REPORT
