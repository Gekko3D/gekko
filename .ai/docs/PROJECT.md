# Project — Gekko Engine

## Overview

> **One-liner:** `gekko` is the shared Go engine module that powers the `gekko3d` editor, demos, and game prototypes.

This repo contains the reusable engine layer for the wider `gekko3d` workspace. It provides the runtime loop, ECS, buffered command model, authored content formats, physics integration, and the ECS-facing bridge to the voxel renderer. Most gameplay-facing consumers in the workspace depend on its contracts directly, so even local engine changes can have multi-module consequences.

The repo is also documentation-heavy. `AGENTS.md`, `base/skills-manifest.md`, and the `docs/` tree define how agents and humans are expected to classify work, preserve invariants, and choose the smallest useful verification path.

## Business Context

- **Product Area:** Game engine/runtime and tooling
- **Business Owner:** [NEEDS VERIFICATION]
- **Engineering Owner:** [NEEDS VERIFICATION]
- **Status:** Active development
- **Criticality:** Internal shared platform for the `gekko3d` workspace

## Key Users & Stakeholders

| Role | Who | How They Use It |
|---|---|---|
| Engine contributors | Repo maintainers and contributors | Build and evolve runtime, content, physics, and renderer behavior |
| Editor consumers | `gekko-editor` maintainers | Consume engine runtime and content contracts in the standalone editor |
| Game/demo consumers | `actiongame`, `spacegame_go`, example authors | Build gameplay or visual samples on the shared engine |
| Reviewing SMEs | [NEEDS VERIFICATION] | Review runtime invariants, renderer contracts, and workflow changes |

## Tech Stack Summary

| Layer | Technology |
|---|---|
| Language | Go |
| Runtime | Custom ECS/stage-based engine runtime |
| Rendering | `voxelrt` internals over WebGPU/GLFW |
| Content | JSON-authored engine formats and runtime loaders |
| Physics | In-tree physics world and module bridge |
| Workspace | Multi-module `go.work` consumer workspace |
| CI/CD | No repo-local automation found |

## Key Metrics

- No formal SLAs or throughput metrics were found in this repo
- Primary quality signals are package tests, consumer-module checks, and manual rendering/editor smoke tests

## External Documentation

- Canonical docs index: `docs/README.md`
- Contributor workflow contract: `AGENTS.md`
- Agent routing manifest: `base/skills-manifest.md`

