# Voxel Normal Ramp Status

- Date: 2026-07-07
- Owner: VoxelRT sparse voxel storage
- Consumers: imported-world auxiliary sidecars, GPU voxel upload

## Goal

Make a thin 3:1 voxel staircase shade as a ramp instead of falling back to a flat tread normal.

## Change

- Preserve the existing radius-2 fit for ordinary surface cases.
- When a two-sided sheet has a degenerate immediate gradient and exactly one exposed axis, use a radius-4 fit before the deterministic fallback.
- Bump imported-world normal sidecars to `voxel_normals_surface_fit_v2`, forcing unchanged chunks to regenerate their auxiliary data.
- Add a 3:1 staircase regression test.

## Verification

- Passed: `env GOCACHE=/private/tmp/gekko-go-build go test ./voxelrt/rt/volume ./voxelrt/rt/gpu`
- Passed: `env GOCACHE=/private/tmp/gekko-go-build go test ./content/derived ./importers/common`
- Passed: Crossfire world emission regenerated 18 full and 18 proxy auxiliary sidecars as `voxel_normals_surface_fit_v2`; no sidecars were reused.
- Passed: `actiongame` root package compiles with `go test .`.
- Blocked unrelated: `go test ./importers/hl1` does not compile because `game_assets_test.go` compares a struct containing a slice; `actiongame go test ./...` is blocked by an unused `math` import in `startup_test.go`.

## Follow-up

Visually check Crossfire shallow ramps, thin sheets, corners, and chunk seams in a GPU session.
