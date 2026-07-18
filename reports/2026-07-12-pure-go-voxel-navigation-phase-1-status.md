# Phase 1: Define New Graph Contracts

## Status

- Confidence: High
- Owner: engine content; later runtime and actiongame phases are consumers
- Architecture: long-term replacement step, intentionally breaking
- Source: `docs/roadmaps/pure-go-voxel-navigation-generation-plan.md`

## Scope

- Define profile-independent source spans and profile-specific graph tiles.
- Define span, region, manifest, route, validation, and JSON persistence contracts.
- Reuse `Vec3` and `TerrainChunkCoordDef`; add no dependency or compatibility API.
- Keep generation, queries, runtime streaming, and gameplay out of Phase 1.

## Decision

Use separate `.gknavsource` and `.gknavgraph` tiles under one `.gknav`
manifest. Source tiles own spans once. Profile graph tiles reference accepted
span IDs and own directed transitions plus compressed regions.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- `env CGO_ENABLED=0 GOCACHE=/tmp/gekko3d-gocache-nocgo go test ./content/...`
- `git diff --check`

No manual visual/GPU check is required: Phase 1 has no generation, runtime, or
rendering behavior.

## Non-Scope

- Span extraction, clearance, edge generation, region compression, routing.
- Binary payloads, compatibility loaders, default profiles, runtime services.

## Results

- Content suite: pass.
- Pure-Go content suite: pass.
- `git diff --check`: pass.
- Manual visual/GPU checks: intentionally not run; no visual or runtime behavior exists.
