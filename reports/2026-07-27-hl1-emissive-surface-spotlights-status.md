# STATUS_REPORT

## Goal

Prevent imported HL1 emissive-surface fill lights from illuminating through
their backing geometry without paying for six point-shadow faces per light.

## Scope

- HL1 importer-generated emissive surface lights only.
- Existing authored point and spot lights are unchanged.
- No content schema or renderer changes.

## Completed

- Synthetic clusters infer their direction from exposed voxel faces.
- Clusters with a reliable direction emit 150-degree shadow-casting spotlights,
  nudged into exposed space.
- Ambiguous clusters retain the previous unshadowed point-light fallback.
- Crossfire was reimported locally: all 64 synthetic clusters resolved to
  shadowed spotlights across 13 distinct orientations.
- Crossfire navigation remained
  `worlds/crossfire-nav-v4/crossfire.gknav`; all 288 world chunks were reused.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1 -count=1`
- Temporary and checked-in Crossfire reimports completed successfully.
- `git diff --check`

## Open Check

- Run Actiongame and visually confirm exterior leakage is gone and the 150-degree
  cone preserves the intended indoor fill.
- Profile the resulting 64 spot-shadow layers. They are six times fewer than
  equivalent point-shadow faces, but the current fixed-size RGBA32F shadow-array
  allocation remains a separate memory concern.

# END_STATUS_REPORT
