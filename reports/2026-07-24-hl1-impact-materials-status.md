# HL1 Impact Materials Status

- Date: 2026-07-24
- Owner: authored-content / HL1 importer

## Goal

Preserve semantic surface materials through baked HL1 voxel palettes so
Crossfire doors, terminals, glass, and wood do not all use concrete impacts.

## Scope

- Use the existing per-voxel `material_value` runtime channel.
- Prefer runtime material facts over source-palette provenance.
- Preserve useful semantics on generated moving-brush assets.
- Map computer, glass, and wood facts to distinct ActionGame impacts.

No schema, renderer, or ECS contract changes.

## Invariant

Baked albedo colors remain unchanged; only the semantic material selected by a
raycast changes.

## Confidence

- Confidence: High
- Why: the existing content/runtime contract already separates baked `value`
  from semantic `material_value`.
- SME alignment required: No

## Verification

- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test ./importers/common ./importers/hl1`
- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test .`
- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/startup`
- Reimported Crossfire:
  - bunker door asset: all 216 used palette entries are `metal`
  - animated bunker terminal: 11 bounded runtime entries are `computer`
  - navigation manifest preserved at `worlds/crossfire-nav-v4/crossfire.gknav`

Not run: manual check by firing at the bunker blast door and terminal.
