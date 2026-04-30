# Skill: Onboard Or Refresh Repo Knowledge

Use this skill to populate or refresh the `.ai/` knowledge layer from the current codebase.

## Steps

1. Read `AGENTS.md`, `base/skills-manifest.md`, and `docs/README.md`.
2. Inspect the current module layout, `go.mod`, and the most relevant docs for runtime, renderer, content, and workflows.
3. Update `.ai/docs/PROJECT.md`, `.ai/docs/ARCHITECTURE.md`, `.ai/docs/DEPENDENCIES.md`, and `.ai/docs/DEPLOYMENT.md` from real repo state.
4. Regenerate `.ai/context.md`, `.ai/conventions.md`, and `.ai/known-issues.md` from those findings.
5. Leave `[NEEDS VERIFICATION]` markers only where the repo does not provide the answer.
6. Do not invent CI, owners, or release automation that the repo does not document.

