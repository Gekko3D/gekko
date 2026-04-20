# Available Repo-Local Skills

These skills live under `.ai/skills/` and complement the global Codex skills already available in the environment.

| Skill | Trigger | Description |
|---|---|---|
| `init` | "refresh AI scaffold" | Verify the `.ai/` scaffold exists and repair missing onboarding files |
| `onboard` | "refresh onboarding", "re-onboard repo" | Rebuild `.ai/docs`, `context.md`, `conventions.md`, and known issues from the current codebase |
| `start-feature` | "start task", "work on ticket" | Create task context, state the invariant, and pick the smallest verification command |
| `complete-task` | "complete task", "wrap up work" | Update task context, changelog, verification notes, and close out docs |
| `resume` | "resume task", "continue work" | Restore task context and next steps from `.ai/features/` |
| `pr-review` | "review PR", "review diff" | Findings-first review aligned to repo invariants and verification expectations |
| `check-docs` | "check docs", "check tracking" | Compare task notes against docs and flag missing updates |
| `rebase` | "rebase docs" | Fold feature-context deltas into `.ai/docs` or canonical docs after merge |
| `upgrade` | "upgrade AI scaffold" | Refresh onboarding framework files while preserving repo-specific content |

Use `AGENTS.md` for the main execution contract in Codex.

