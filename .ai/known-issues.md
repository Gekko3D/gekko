# Known Issues & Tech Debt — Gekko Engine

> Review this file before modifying affected areas.

## Active Known Issues

### [ISSUE-001] Workspace Sample `examples/testing` Does Not Compile

- **Severity:** Medium
- **Affected Area:** Workspace consumer outside this repo: `examples/testing/main.go`
- **Description:** The sample still references `TextureFormatR8Uint`, which is no longer exported by the engine surface.
- **Workaround:** Do not use `examples/testing` as a verification target for engine work.
- **Ticket:** [NEEDS VERIFICATION]
- **AI Impact:** A broad workspace sweep can fail even when changes in `gekko/` are correct.

### [ISSUE-002] Default Go Cache Location Can Fail In This Environment

- **Severity:** Medium
- **Affected Area:** Local verification commands
- **Description:** Plain `go test` may fail when Go tries to trim or write the default cache path.
- **Workaround:** Use `env GOCACHE=/tmp/gekko3d-gocache ...`.
- **Ticket:** [NEEDS VERIFICATION]
- **AI Impact:** Prefer the documented cache override for every local Go command in this environment.

### [ISSUE-003] Windowed Apps Depend On The Correct Working Directory

- **Severity:** Low
- **Affected Area:** `gekko-editor`, sample apps, asset-loading paths
- **Description:** Some demos and apps resolve assets relative to their own module directory.
- **Workaround:** Run apps from the module directory named in `AGENTS.md`.
- **Ticket:** [NEEDS VERIFICATION]
- **AI Impact:** Failed asset loading does not necessarily mean the code is wrong; check `cwd` first.

## Tech Debt Register

| ID | Description | Priority | Effort | Ticket |
|---|---|---|---|---|
| TD-001 | Root-package ownership remains broad, so runtime, content, and bridge behavior are still tightly coupled in many files. | High | Large | [NEEDS VERIFICATION] |
| TD-002 | Much of the collaboration workflow is doc-driven rather than enforced by scripts or CI automation. | Medium | Medium | [NEEDS VERIFICATION] |

## Gotchas & Warnings

- Many high-value engine behaviors still live in the root `gekko` package rather than subpackages.
- `PhysicsUpdate` is a real stage inserted before the usual render/update stages; do not assume the default order from older docs or memory.
- Renderer changes often need both `docs/renderer/*` reading and bridge-file inspection before edits.

