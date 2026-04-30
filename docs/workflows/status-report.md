# Workflow: Status Reports

Use this page when a task is large enough that the next human or agent should not have to reconstruct the context from diffs and chat history alone.

## When To Write One

Write a status report when:

- the task is broad, risky, or still being shaped
- confidence is `Medium` or `Low`
- the change crosses module boundaries
- the work may pause before merge
- a reviewer needs explicit options, risks, or open questions

Save reports under:

- `reports/<YYYY-MM-DD>-<topic>-status.md`

## What A Good Report Does

A useful report lets a reviewer answer these questions quickly:

- what is the goal
- what changed or is proposed
- what is in scope and out of scope
- what risks or open questions remain
- what should happen next

Keep it short, concrete, and link-heavy rather than narrative.

## Canonical Template

```md
# STATUS_REPORT

## Metadata
- Owner: <agent or human>
- Date: <YYYY-MM-DD>
- Project/Area: <subsystem>
- Related Links: <docs, commits, PRs, issues>
- Reviewers Requested: <roles or names>

## Executive Summary
- Goal: <what outcome is needed>
- Current state: <current behavior or repo state>
- Progress: <done so far>
- Risk/Blocker: <main uncertainty or blocker>
- Ask: <what reviewer should decide or verify>

## Background / Context
- Why now: <motivation>
- Scope: <explicit scope>
- Non-scope: <what this task will not do>
- Constraints: <safety, perf, compatibility, schedule>

## Current Status
- Completed:
  - <done item>
- In progress:
  - <current item>
- Next steps:
  - <next item>

## Proposed Change
- User-visible behavior: <impact>
- API/Interface changes: <contracts, schemas, install order, docs>
- Data changes: <formats, migration, none>
- Operational changes: <runbooks, rollout, none>

## Design Options
### Option A: <label>
- Approach: <summary>
- Pros:
  - <point>
- Cons:
  - <point>
- Risk:
  - <point>

### Option B: <label>
- Approach: <summary>
- Pros:
  - <point>
- Cons:
  - <point>
- Risk:
  - <point>

### Recommendation
- Chosen option: <option>
- Rationale: <why>
- Open questions:
  - <question>

## Testing Plan
- Unit tests:
  - <package or command>
- Integration tests:
  - <package or command>
- Edge cases:
  - <case>
- How to validate locally:
  - <command>

## Rollout / Compatibility
- Backward compatibility: <yes/no/details>
- Feature flags: <if any>
- Migration/Backfill: <if any>
- Rollback plan: <how to revert safely>

## Risks & Mitigations
- Risk 1: <risk>. Mitigation: <mitigation>.
- Risk 2: <risk>. Mitigation: <mitigation>.

## Specific Questions For Reviewer
1. <question>
2. <question>

# END_STATUS_REPORT
```

## Minimal Variant

If the task does not justify the full template, keep at least:

- goal
- scope and non-scope
- current blocker or open question
- verification run so far
- explicit ask for the reviewer
