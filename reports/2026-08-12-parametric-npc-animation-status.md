# STATUS_REPORT

## Metadata
- Owner: Codex
- Date: 2026-08-12
- Project/Area: authored animation, runtime playback, HL1 NPC consumer
- Related Links: [`hl1-npc-implementation-plan.md`](../../actiongame/docs/hl1-npc-implementation-plan.md), [`asset-format.md`](../docs/content/asset-format.md)
- Reviewers Requested: engine/content and ActionGame gameplay

## Executive Summary
- Goal: preserve and play the pitch-blended attack sequences required by Barney, HECU, and alien grunts.
- Current state: the shared 1D contract, GoldSrc/Source decoders, runtime sampling, and Barney aim/lifecycle integration are implemented.
- Progress: generated Barney content now contains pitch samples at -50 and +50 degrees with a zero-degree default.
- Risk/Blocker: visual acceptance requires a desktop ActionGame smoke test after generated content is rebuilt.
- Ask: verify Barney's visible pitch range and consecutive-shot continuity after automated checks pass.

## Scope
- Add a source-neutral one-dimensional blend to `.gkanim` clips.
- Keep existing schema-v1 documents readable; new generated documents use schema v2.
- Decode GoldSrc and Source v48 one-dimensional samples and ranges.
- Drive Barney attack pitch from shared actor aim for AI and possession.
- Preserve one timeline, one completion result, and one event stream per clip.

## Non-scope
- Two-dimensional grids, blend trees, crossfade graphs, IK, Source auto-layers, mesh/material import, and editor authoring UI.

## Invariants
- Animation events request gameplay effects but never apply damage.
- Blend parameter changes do not restart playback or duplicate events.
- Root motion remains controller-owned.
- Unsupported source features fail explicitly rather than degrading silently.
- AI and possession use the same species action executor and actor aim.

## Design Decision
- Chosen: ordered 1D samples embedded in the existing clip contract and sampled by the existing animation player.
- Rejected: neutral-pose baking, because it loses aiming and repeats the same defect for HECU and alien grunts.
- Rejected: a general animation graph, because no current NPC requires it.

## Verification Plan
- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `cd gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1 ./importers/source .`
- `cd actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup ./...`
- `cd gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Manually verify level/up/down Barney shots, repeated shots, reload, interrupt, death, and possession.

## Compatibility And Rollback
- Existing `.gkanim` schema-v1 files remain valid and unchanged.
- Newly generated blend clips use schema v2; no bulk migration is required.
- Removing the v2 set reference or reverting regenerated Barney content restores the previous runtime without touching level data.

## Current Status
- Completed:
  - traced the defect through ActionGame scheduling, runtime playback, generated content, and both MDL importers;
  - confirmed required GoldSrc Barney, HECU, and alien-grunt attack sequences are 1D blends;
  - confirmed Source Barney shooting is 2x1 and neck turning is 3x1;
  - amended the NPC plan;
  - implemented schema-v2 1D clips while preserving schema-v1 reads;
  - implemented runtime interpolation without timeline restart;
  - decoded GoldSrc and Source 1D shooting samples and rejected 2D grids;
  - split Barney action completion/start around bounded schedule advancement;
  - drove AI and possessed Barney from the same signed actor-aim pitch;
  - rebuilt the local Barney asset and animation set.
- Next:
  - run the manual visual smoke test.

## Verification Result

- `gekko`: `go test ./...` and `go vet ./...` pass.
- `actiongame`: `go test ./...` and `go vet ./...` pass.
- `gekko-editor`: `go test ./...` passes; `go vet ./...` still reports the two pre-existing `ChunkObserverComponent` lock-copy findings in imported-world and terrain preview code.
- `git diff --check` passes in both changed repositories.
- Source v48 `shootgun` was baked against the Barney rig as schema v2 with samples at -50 and +50.

# END_STATUS_REPORT
