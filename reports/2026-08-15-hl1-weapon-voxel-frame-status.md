# STATUS_REPORT

## Metadata

- Owner: Codex
- Date: 2026-08-15
- Project/Area: HL1 game-asset importer and ActionGame weapon assets
- Related Links: `importers/hl1/mdl_voxel_asset.go`, `importers/hl1/game_assets.go`

## Executive Summary

- Goal: voxelize nearly cardinal HL1 weapon geometry without diagonal stair-step artifacts while preserving its source pose.
- Current state: catalogued world and held weapons sample in a snapped voxel frame and apply the inverse residual rotation to `mdl_surface`.
- Progress: regenerated ActionGame's 54 catalogue weapon assets from `/Users/ddevidch/code/other/hl`.
- Risk/Blocker: visual inspection in a running ActionGame remains outstanding.

## Scope

- In scope: static `weapon_world` and `weapon_held` imports with a usable source weapon bone.
- Non-scope: animated models, non-weapon static props, and the special composite `p_egon` presentation.

## Verification

- Passed: `env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1 -run '^$'`
- Passed: `go run ./cmd/hl1import -assets-only ... -import-all-weapon-world-models` against the local HL1 install; held pistol and shotgun assets now have residual part rotations, while the cardinal world shotgun remains identity.
- Existing failure: the full `go test ./importers/hl1` fails in `TestBuildGameAssetImportCatalogsAndCopiesMapAssets` because it expects `JointCapVoxels == 1`, but the current NPC profile sets it to `0`; this change does not touch that profile.
- Existing failure: `cd ../actiongame && env GOCACHE=/tmp/gekko3d-gocache go test ./...` reaches `src/modules/startup` and fails because `characters.hgrunt.b0.s0` lacks `generated_reload_mp5`; this task only regenerated weapon assets.

## Next Step

- Open ActionGame and inspect a held shotgun/pistol and a world pickup; barrels and flat panels should no longer stair-step along the residual source tilt.

# END_STATUS_REPORT
