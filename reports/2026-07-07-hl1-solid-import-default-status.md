# HL1 Solid Import Default Status

- Date: 2026-07-07
- Owner: authored-content / HL1 importer

## Goal

Make BSP-aware solid voxelization the CLI and editor default with a two-voxel
solid band.

## Scope

- Default `solid-band-depth` is `2`.
- CLI default world mode is `solid`.
- Editor defaults and HL1 import documentation match.

Existing generated `.gkworld` files are unchanged and require reimport from
their source BSP to receive the new default.

## Invariant

Only cells that BSP `PointContents` classifies as solid are filled; each voxel
coordinate has one occupancy entry, so fill does not duplicate or intrude into
reachable playable space.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1/...`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/hl1import`
- `env GOCACHE=/tmp/gekko3d-gocache go test ./src/modules/level_editor`
- `env GOCACHE=/tmp/gekko3d-gocache go run ./cmd/hl1import -h` confirms
  `-debug-world-mode` defaults to `solid` and `-solid-band-depth` defaults to
  `2`.

Not run: GPU/manual validation. No source BSP or generated `.gkworld` is
present in this workspace to reimport and inspect.
