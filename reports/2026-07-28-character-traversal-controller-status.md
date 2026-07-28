# Character Traversal Controller Status

## Goal

- Move drop, jump, ladder, mantle, and vault execution behind one actor-neutral
  traversal state owned by the engine character motor.
- Prevent ambient ladder overlap from stealing an active traversal.

## Scope

- Primary owner: engine character runtime.
- Consumers: ActionGame player and NPC locomotion; navigation route links.
- Preserve existing authored drop, ladder, mantle, and vault behavior while
  replacing competing movement ownership.
- Extend navigation link vocabulary and stable link identity for future jump
  and window links.

## Non-scope

- Automatic jump/window link generation and map rebakes.
- New tests. User will live-test ActionGame.
- Animation asset changes.

## Contract

- One traversal state owns special movement from entry through physical
  settlement.
- Motor mode is explicit: normal, ballistic, ladder, or kinematic.
- Ladder overlap reports availability; it cannot override ballistic or
  kinematic traversal.
- Post-commit failure keeps physics active until grounded, then publishes a
  result.

## Confidence

- Confidence: High
- Why: current freeze and ownership conflict are traced through motor, NPC
  locomotion, and player traversal.
- Key assumption: existing route endpoints remain valid traversal targets.
- SME alignment required?: No; user approved proposed architecture.

## Verification

- Compile only:
  - `cd gekko && go build ./...`
  - `cd actiongame && go build ./...`
- Manual traversal validation intentionally left to user.

## Current Status

- Completed:
  - shared motor-owned traversal state and exclusive motion modes
  - ballistic drop/jump settlement, explicit ladder traversal, kinematic
    mantle/vault execution
  - ActionGame player and bot handoff/result integration
  - legacy ActionGame NPC traversal routed through the same motor executor
  - stable navigation link ID separated from gameplay owner ID
  - navigation vocabulary and profile limits for jump/vault/mantle/drop
  - obsolete phase/watchdog traversal tests removed as requested
- Intentionally deferred:
  - automatic cliff/window link generation and nav rebake; add when authored
    links have been live-validated
