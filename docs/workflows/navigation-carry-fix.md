# Navigation publication and moving-brush regression repair

## Scope and confidence

- Primary owner: engine runtime (`navigation_graph_runtime.go`).
- Affected consumer: `actiongame` navigation and shared character motors.
- Confidence: High. The existing asynchronous snapshot contract and motor
  ownership establish the required behavior. `base/skills-manifest.md` is absent;
  `docs/workflows/agent-task-loop.md` and owning documentation were read.
- SME alignment required: No. This is a permanent repair within existing
  ownership boundaries, with no format, renderer, or public API changes.

The carry regression test bypasses the motor's support update and creates an NPC
without a motor. The normal update order establishes physical support before
moving brushes. Repair the fixture and assertions; do not restore position-only
carrying or add a second NPC movement owner.

The navigation regression comes from discarding every successful overlay build
whose captured overlay request has since changed. Overlay changes must not
indefinitely prevent current topology from becoming usable.

## Design and invariants

1. Keep one background overlay build active at a time. Publish a successful
   complete snapshot if its runtime and topology generations are still valid,
   even when another overlay request arrived during the build. Start a follow-up
   build from the latest desired overlay. This also permits overlay-only
   publications to progress under repeated changes.
2. Publish query, captured overlay maps, source/graph residency, and revision
   together. Preserve every blocker captured by the snapshot. Keep desired maps
   independent from published maps so newer requests remain queued.
3. Overlays remain asynchronous. A published snapshot can lag newer desired
   intent until a later build completes; this repair does not introduce an
   immediate live-overlay guarantee. Physical movement continues to validate
   against live collision. Do not rebuild query indexes synchronously or discard
   valid completed work on every moving-overlay update.
4. Reject obsolete runtime or topology results. Ignore an error from a superseded
   overlay request, retry the latest request, and preserve fatal handling for a
   current request. Do not publish partial data or invalid query snapshots.
5. Retire only destruction blockers covered by the published load generation.
   Preserve later edit blockers and snapshot immutability, including old service
   values and route tile dependencies.
6. Carry only motors whose live grounded contacts identify the brush. Verify
   players and NPCs through actual support probing before brush motion; actors
   outside support, airborne actors, and actors bearing on another entity do
   not become riders based only on nearby coordinates.

Alternatives considered: accepting only the newest overlay request preserves the
starvation defect; publishing source/graph metadata without its matching query
breaks snapshot consistency; rebuilding synchronously moves index construction
onto the update thread. Serial complete snapshot publication matches the
existing ownership and asynchronous contract.

## TDD and review plan

The user explicitly authorized test changes through the requested TDD workflow.
Use Sol 6.1 for the test and implementation subagents. First produce tests only
and stop at RED. Review tests adversarially before implementing production code.

Coverage by functionality:

- Carrying: supported player and motor-owned NPC move with an elevator; support
  identity and camera/local transforms remain consistent; unsupported or
  airborne actors are not carried. Use normal support probing, not fabricated
  private state, wherever practical.
- Navigation progress: repeated overlay changes during current residency and
  overlay-only builds still produce usable public service snapshots. Verify
  actual projection/routes/reachability and eventual latest-overlay convergence,
  rather than exact helper order or revision counts.
- Safety and lifecycle: captured blockers stay effective across unrelated
  overlay churn; newer desired blockers survive and become effective on the
  follow-up publication; obsolete topology/runtime results and superseded errors
  cannot replace the current query; current failures retain their error path.
- Destruction and immutability: retire covered edit blockers only, preserve newer
  blockers, retain old service behavior after publication, and invalidate route
  dependencies only for changed topology.

Expected files: the two existing regression test files, a focused navigation
publication test file if needed, `navigation_graph_runtime.go`, and canonical
`docs/content/levels.md` / `docs/content/streaming-and-worlds.md`. Character motor
production code changes require an observed functional defect in the repaired
fixture; do not change it solely to match the old fixture.

Verification: targeted navigation/carry tests for RED and GREEN, engine root
package tests, focused race checks, and affected `actiongame` build/tests. No
windowed or GPU check is required because this repair changes neither rendering
nor presentation tuning. Record exact commands and results below after review.

## Execution record

### Tests and adversarial review

Sol 6.1 authored tests only and stopped at RED. Root review required these
corrections before production implementation:

- Blocked spans are inactive; fix the test helper's opposite assumption.
- Allow asynchronous lag without requiring exactly one stale snapshot.
- Replace inert door/ladder IDs with a real gated route to verify queued door and
  disabled-traversal intent through route actions and reachability.
- Generate failures with invalid blocker bounds rather than rewriting worker
  result messages; cover both residency and overlay-only failures.
- Verify old service immutability from a valid baseline captured before blockers
  arrive, including route dependency epochs.
- Establish a nearby actor's distinct physical support inside the former
  coordinate-only carry tolerance, and exercise the authored carrier role.

The corrected carry fixture passes without motor production changes. Navigation
RED demonstrates blocked publication of current residency, overlay-only progress,
queued door/traversal intent, and generation-qualified edit retirement. Existing
blocker/dependency and retirement coverage, obsolete result fences, and current
versus superseded errors pass. The RED log is
`/tmp/gekko-navigation-carry-red.log`.

### Production and final review

Sol 6.1 changed only the overlay-generation rejection condition: superseded
errors are rejected, while successful complete snapshots continue through the
existing runtime and topology fences. Query construction, the single active
worker, copied desired maps, atomic commit, tile epochs, and edit retirement
retain their existing owners. No character motor production changes were needed.
Canonical streaming and level documentation now describes these contracts.

Root reviewed the production diff, required two wording corrections, and
verified the final engine and consumer checks. A separate Sol 6.1 adversarial
review inspected generation validity, error handling, desired intent retention,
edit retirement, immutability, epochs, and test assumptions; it found no remaining
actionable issues.

### Verification

From `/Users/ddevidch/code/go/gekko3d/gekko`:

```sh
# Root confirmed RED before production, then the implementation agent ran GREEN.
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(MovingBrush|GroundedPlayerLandsOnMovingBrush|StreamedNavigation|RuntimeNavigation|NavigationEditBlockers)' -count=1

# Implementation agent: GREEN, including both repaired baseline failures.
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(MovingBrush|GroundedPlayerLandsOnMovingBrush|StreamedNavigation|RuntimeNavigation|NavigationEditBlockers)' -count=1

# Root final sweep: every engine package passed or had no test files.
env GOCACHE=/tmp/gekko3d-gocache go test ./... -count=1
git diff --check
```

From `/Users/ddevidch/code/go/gekko3d/actiongame`, root verified:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup -run '^TestActionGame(NPCNavigation|Navigation|SharedActor|FirstPersonCamera|ControlStage)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go build ./...
```

The focused race check emitted the existing macOS `LC_DYSYMTAB` linker warning
and passed. The actiongame build emitted a sandbox module stat-cache warning and
exited successfully. Full engine output is in
`/tmp/gekko-navigation-carry-engine.log`.

No windowed gameplay or GPU verification was run. The repair affects navigation
publication and a motor test fixture; rendering and presentation are unchanged.
Asynchronous overlay lag remains an explicit limitation, and character movement
continues to validate live collision. The unrelated deletion of
`.github/pull_request_template.md` remains outside this commit.
