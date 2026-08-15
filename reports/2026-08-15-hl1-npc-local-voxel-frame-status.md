# STATUS_REPORT

## Metadata

- Owner: Codex
- Date: 2026-08-15
- Project/Area: HL1 rigid NPC asset importer
- Related Links: `importers/hl1/mdl_voxel_asset.go`, `docs/content/asset-format.md`

## Summary

- Goal: sample rigid NPC voxel parts in their source bone-local frame, without changing their bind pose or animation clips.
- Scope: HL1 rigid-bone MDL assets only; no content schema or runtime changes.
- Non-scope: blended skinning, cross-bone voxel welding, or new joint-cap behavior.
- Invariant: generated voxel children restore their source global bind rotation; their existing parent-group delta animation therefore produces the same source pose and motion.
- Risk: the global skeleton repartition pass requires one shared grid, so the local-frame profile fills each rigid part independently instead.

## Design

- Voxelize each triangle in every participating owner bone's local bind frame; keep only the samples owned by that bone.
- Attach the resulting voxel child at its bind-space position and rotation under the existing animated bone group.
- Keep the existing global-to-delta animation tracks. They still animate the parent group around the correct bone origin.

## Verification

- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1 -run 'Test(BuildMDLVoxelAsset|VoxelizeMDLGeometryByBone|MDLAnimationTracksUseBoneLocalSpace)'`.
- Passed: regenerated all 26 catalog NPC assets with `go run ./cmd/hl1import ... -assets-only -import-all-npc-models`; Barney now has 21 non-identity voxel-child bind rotations.
- Existing test mismatch: the full importer suite still asserts the previous NPC profile (`hl1_npc_rigid_v3`, global skeleton repartitioning). It was not modified because test changes were not requested.
- Manual in-game animation inspection remains required.

# END_STATUS_REPORT
