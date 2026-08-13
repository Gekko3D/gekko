# NPC Locomotion Presentation Status

- Date: 2026-08-13
- Status: Implemented; live visual verification pending
- Primary owner: engine animation/content contract
- Affected consumer: ActionGame NPC presentation
- Confidence: High
- SME alignment required?: No

## Scope

Add a reusable NPC locomotion presentation input without giving animation
authority over movement or species actions. ActionGame uses it for Barney's
travel-facing, aim-locked backpedal, gait speed, and turn-in-place behavior.

## Preserved Invariants

- Grounded motor state remains authoritative for actor movement.
- Explicit draw, holster, attack, reload, pain, and death clips outrank
  locomotion presentation.
- AI and possession continue through the same actor intent and species action
  executor.
- Root motion stays locked outside the existing death policy.
- Missing directional clips use declared fallbacks rather than guessed clip
  names.

## Implementation

- `CharacterDirectionalLocomotionDef` now optionally carries turn-in-place
  clips and tuning.
- The HL1 asset-library adapter preserves that optional authored definition.
- `NPCAnimationComponent` accepts a locomotion clip and playback rate; the
  engine falls back to semantic selection when that clip is absent.
- ActionGame stages NPC presentation during Control, before animation sampling,
  so stopping movement clears the locomotion override in the same frame.
- Barney faces travel while holstered, preserves aim-facing for authored
  aim-relative locomotion and reverse backpedaling, honors `face_travel` for
  missing side/diagonal clips (including backward diagonals), and uses authored
  left/right turn clips while stationary.

## Verification

- `gekko: env GOCACHE=/tmp/gekko3d-gocache go test .` — passed
- `gekko: env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` — passed
- `gekko: env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1/...` — passed
- `actiongame: env GOCACHE=/tmp/gekko3d-gocache go test ./...` — passed
- `spacegame_go: env GOCACHE=/tmp/gekko3d-gocache go test ./...` — passed
- `gekko-editor: env GOCACHE=/tmp/gekko3d-gocache go build ./...` — passed
- `gekko-editor: env GOCACHE=/tmp/gekko3d-gocache go test ./...` — blocked by
  pre-existing missing `level_editor` test helpers (`collectUIButtonLabelsForTest`
  and `containsButtonOrLabel`); no test files were changed.

## Remaining Verification

Run ActionGame and visually check Barney in both AI and possession modes:
holstered forward/side/backward travel, drawn-pistol backpedal and lateral
movement, stop-to-idle, stationary camera yaw correction, and action
interruptions during each locomotion case.
