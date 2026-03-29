# Workflow: Add a Content Type

Use this playbook when introducing a new authored content document or extending an existing one.

## Goal

A content-type change is complete only when:

- schema types exist
- defaults and normalization exist
- load/save exists
- validation exists
- runtime consumption exists
- editor impact is understood
- docs are updated

## Steps

1. Define the schema in `gekko/content`.

Add or extend:

- the main `Def` structs
- schema version constants
- defaulting helpers such as `Ensure*Defaults`

2. Add load/save helpers.

Typical files:

- `*_io.go`

3. Add validation.

Typical files:

- `*_validation.go`

Validation should catch:

- missing required fields
- invalid enum values
- impossible combinations
- broken file references
- version mismatches

4. Add runtime consumption.

Ask where the content is used:

- eager authored spawn
- streamed runtime
- renderer bridge
- gameplay systems
- editor preview

5. Add tests at both schema and runtime boundaries.

Typical coverage:

- round trip
- validation failures
- minimal valid document
- integration with the runtime consumer

6. Check editor impact.

Inspect:

- `gekko-editor/src/formats`
- the relevant workflow module

If the editor aliases the schema directly, engine changes are editor changes.

## Current High-Value Examples

- assets:
  - `asset.go`
  - `io.go`
  - `validation.go`
- levels:
  - `level.go`
  - `level_io.go`
  - `level_validation.go`
- imported worlds:
  - `imported_world.go`
  - `imported_world_io.go`
  - `imported_world_validation.go`
- world deltas:
  - `world_delta.go`
  - `world_delta_io.go`

## Common Mistakes

- adding schema fields without updating defaults
- updating save/load without updating validation
- updating validation without updating editor assumptions
- forgetting document-relative path rules
- documenting a feature before runtime support actually exists

## After the Change

Update the relevant docs under:

- `docs/content/`
- `docs/assets/` if runtime asset ownership changes
- `docs/editor/` if editor workflows are affected

Then add the right verification commands to:

- [`../engine/verification.md`](../engine/verification.md)
