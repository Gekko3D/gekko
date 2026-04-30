# Coding Conventions — Gekko Engine

## Language & Tooling

- **Language:** Go 1.24 module in a Go 1.25 workspace
- **Formatter:** `gofmt`
- **Testing:** `go test` with the smallest matching package path
- **Assertion Library:** `github.com/stretchr/testify`
- **Linter:** no repo-local linter config found as of 2026-04-20

## Naming Conventions

| Element | Convention | Example |
|---|---|---|
| Exported types and funcs | PascalCase | `NewApp`, `PhysicsModule` |
| Unexported helpers | mixedCase | `sanitizeComponents` |
| Constants and enum values | PascalCase prefixes | `RenderModeLit`, `CollisionEventEnter` |
| Package names | short lowercase | `content`, `physics`, `ecs` |
| Test files | `_test.go` beside production files | `app_test.go`, `mod_physics_loop_test.go` |

## Project Patterns

### Runtime And Modules

- Modules implement `Install(app *App, cmd *Commands)`
- Systems are registered via `System(...).InStage(...).RunAlways()` or state-aware variants
- System dependencies are pointer parameters resolved from `app.resources`
- Missing resources fail at runtime with reflective resolution panics

### ECS And Mutation Rules

- Use `Commands` for structural ECS mutation
- Do not assume `AddEntity`, `AddComponents`, `RemoveComponents`, `RemoveEntity`, or `ChangeState` are immediately visible
- Stage placement is usually the right fix when visibility feels one frame late

### Content And Serialization

- JSON content loaders normalize and validate after deserialize
- Schema-version checks happen at load/save boundaries
- Content/runtime behavior is split between `content/` definitions and root-package spawn helpers

### Renderer Integration

- Read renderer docs before editing low-level `voxelrt/rt/...` code
- Prefer changing ECS-facing bridge files first when behavior is observable at engine level
- Renderer-facing runtime changes often need both package-level renderer tests and root-package confidence checks

### Error Handling

- Return `error` values for validation and IO failures
- Use early returns for guard conditions
- Keep helper functions small and explicit rather than hiding side effects

### Testing

- Keep tests beside the code they validate
- Use the narrowest package or root-package verification path that matches the change
- Broad `go test ./...` is a final sweep, not the default first step

## Things AI Agents Should Avoid

- Do not run commands from the workspace root for this module’s work
- Do not rewrite `AGENTS.md` or canonical docs casually; keep changes surgical
- Do not make assumptions about consumer-module compatibility when shared engine contracts changed
- Do not use `examples/testing` as a success signal until its known compile issue is resolved
- Do not revert unrelated user changes in a dirty worktree

