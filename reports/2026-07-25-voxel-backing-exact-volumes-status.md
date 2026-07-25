# Voxel Backing Exact Volumes Status

- Date: 2026-07-25
- Owner: authored content / HL1 importer / destruction runtime
- Confidence: High

## Goal

Prevent edits beside thin HL1 brush surfaces from creating voxels while
preserving supported top-down craters and continued tunnelling.

## Root Cause

HL1 backing previously contained only the world BSP root plus 1.5 m support
bands behind every opaque upward face. At Crossfire world position
`(1.60078, -39.60006, 40.40497)`, the world classifier is empty but support
triangle 2097 from `func_wall` model 22 classifies the point as solid. A pistol
edit therefore materialized a face-neighbor shell outside its carve.

## Change

- Plane-tree backing now stores bounded exact classifier roots for the world
  model and each static `func_wall` / `func_illusionary` model baked into the
  imported world.
- Runtime unions those exact volumes without filling space between their
  bounds.
- Thin upward-face support remains available for intentional ground depth, but
  materialization activates it only when the edit sphere reaches the authored
  surface or continues within a band whose removal history was seeded there.
- Existing version-1 sidecars without bounded volumes still load through the
  legacy root.

Rejected alternatives:

- Remove all support bands: regresses the central Crossfire RPG platform.
- Disable backing for pistol edits: weapon-specific and leaves other side edits
  broken.
- Renderer guard: cannot fix real voxels already inserted into `XBrickMap`.

## Verification

- Focused:
  - `env GOCACHE=/tmp/gekko3d-gocache go test ./content/... ./importers/hl1/... .`
- Real Crossfire temporary reimport:
  - 22 exact plane volumes
  - 2,103 surface supports
  - reported side coordinate: zero added voxels for all six axis normals
  - RPG-style top edit: 134 backing voxels remained around the crater
- Crossfire's local `.gkvoxelbacking` was replaced from the validated reimport;
  chunk geometry and navigation were not regenerated.

## Remaining Manual Check

The user should test pistol side hits and RPG top hits in ActionGame. No visual
or GPU run was performed by Codex.
