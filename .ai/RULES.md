# Gekko AI Code Assist Rules

This file defines the shared AI workflow for the `gekko` engine repo.

## Override Precedence

Project-wide defaults live in `.ai/`. Machine-local or user-local overrides belong in `.ai.local/`.

1. Read `.ai/RULES.md`
2. Read `.ai.local/RULES.local.md` if it exists
3. Read `.ai/conventions.md`
4. Read `.ai.local/conventions.local.md` if it exists
5. Use `AGENTS.md` as the Codex-specific execution contract

Do not commit `.ai.local/`.

## Step 0: Orient Before Editing

Before writing code or docs:

1. Read `.ai/context.md`
2. Read `.ai/conventions.md`
3. Read `AGENTS.md`
4. Read `base/skills-manifest.md`
5. Read the smallest matching canonical docs under `docs/`
6. Check `.ai/features/` for overlapping work
7. Check `.ai/known-issues.md` when working near renderer, workspace, or verification paths

## Step 1: Create Task Context For Non-Trivial Work

Create a feature context in `.ai/features/` when the task:

- changes behavior, docs, or workflow beyond a tiny isolated fix
- crosses engine and consumer boundaries
- may need handoff or async review
- should leave behind explicit invariants or verification notes

Naming:

- ticketed work: `.ai/features/[TICKET]-short-description.md`
- non-ticketed work: `.ai/features/TASK-YYYYMMDD-short-description.md`

For each non-trivial task, record:

- objective and scope
- primary owner bucket
- invariant that must remain true
- planned verification command
- docs or consumer modules that may also need checking

## Step 2: Run The Confidence Gate

Use the repo-local collaboration workflow:

- classify the task with `base/skills-manifest.md`
- use the confidence rubric from `AGENTS.md`
- if confidence is not `High`, narrow scope or write a status report in `reports/`

Default mapping:

- `High`: one owner bucket, clear invariant, clear verification path
- `Medium`: owner known, but consumer impact or invariant still fuzzy
- `Low`: ownership or success criteria unclear

## Step 3: Make Changes Conservatively

Hard rules:

- do not bypass `Commands` buffering rules unless the owning runtime code already does so intentionally
- do not assume same-stage ECS visibility after `AddEntity`, `AddComponents`, or `ChangeState`
- do not run broad verification first when a smaller package check exists
- do not run Go commands from the workspace root unless the task explicitly targets another module
- do not overwrite user changes in dirty files without explicit direction

Docs rules:

- if behavior or workflow expectations change, update canonical docs under `docs/`
- also log the change in the active feature context when one exists
- use status reports in `reports/` for broad or handoff-prone work

## Step 4: Verify Using The Smallest Matching Command

Run commands from `/Users/ddevidch/code/go/gekko3d/gekko`.

Use `env GOCACHE=/tmp/gekko3d-gocache ...` in this environment.

Default checks:

- root package runtime change:
  - `env GOCACHE=/tmp/gekko3d-gocache go test .`
- content/schema change:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- ECS compile check:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./ecs/...`
- physics compile check:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./physics/...`
- broad refactor final sweep only:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./...`

Cross-module checks are required when shared engine behavior affects:

- `gekko-editor`
- `actiongame`
- `spacegame_go`
- workspace examples that exercise the touched runtime path

Known caveat:

- `examples/testing` is not a reliable confidence signal because it currently fails outside this repo due to a stale exported texture-format reference

## Step 5: Complete The Task

Before reporting completion:

- update the active feature context if one exists
- record exact verification commands run
- note anything intentionally not verified
- update canonical docs when the task changed behavior or workflow
- add a changelog entry for repo-level workflow additions or notable engine changes

Do not push or create PRs without an explicit user request.

## Common Task Shapes

### Docs Or Workflow Change

1. Read `AGENTS.md` and matching `docs/workflows/...`
2. Update canonical docs directly
3. Write or update a status report if the workflow surface changed broadly
4. Verify internal consistency of the new guidance

### Engine Behavior Change

1. Pick the owner bucket first
2. State the invariant before editing
3. Update code and docs together if runtime expectations changed
4. Run the smallest matching `go test` command
5. Verify affected consumer modules only if the boundary changed

### Code Review

1. Read `.ai/context.md`, `.ai/conventions.md`, and `AGENTS.md`
2. Focus findings on bugs, regressions, risks, and missing verification
3. Report findings first, ordered by severity

