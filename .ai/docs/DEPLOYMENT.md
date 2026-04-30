# Delivery & Deployment — Gekko Engine

## Overview

- **Target:** Local development and consumption as a Go module within the `gekko3d` workspace
- **Registry:** Go module source control; no container/image registry found in this repo
- **CI/CD:** No repo-local CI workflow files found as of 2026-04-20
- **IaC:** None found in this repo
- **Strategy:** Local verification plus downstream consumer checks when shared contracts change

## Delivery Model

This repo is an engine/library module, not a deployable service. Changes are validated through package tests, consumer-module checks, and optionally desktop `go run` flows for renderer or editor behavior.

## Validation Pipeline

```text
Read owning docs -> edit code/docs -> run smallest matching go test -> run affected consumer checks if needed -> optional windowed/manual validation
```

- **Pipeline files:** none found in repo
- **Build tooling:** `go test`, `go run`, `gofmt`
- **Canonical verification guide:** `docs/engine/verification.md` and `docs/renderer/verification.md`

## Local Run Commands

Use the commands from `AGENTS.md` and run them from the owning module directory.

- Engine root package:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test .`
- Content/schema checks:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./content/...`
- Broad engine sweep:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko && env GOCACHE=/tmp/gekko3d-gocache go test ./...`
- Editor/manual rendering checks:
  - `cd /Users/ddevidch/code/go/gekko3d/gekko-editor && env GOCACHE=/tmp/gekko3d-gocache go run .`

## Release / Rollout Notes

- No formal release automation was found in this repo.
- Downstream impact is primarily through workspace consumers importing this module.
- For broad shared-runtime changes, validate at least one directly affected consumer module.

## Secrets & Configuration

No repo-local secret management or deployment configuration was found.

## Rollback

```bash
git revert <commit>
```

Or restore the previous branch state through the normal Git workflow for this module.

## Health Checks & Monitoring

- Package-level confidence comes from the smallest matching `go test` target
- Renderer/editor confidence may require a local desktop session and manual smoke validation
- No service health endpoints or deployment dashboards exist in this repo

## Post-Change Checklist

- [ ] Correct owner bucket and invariant were identified
- [ ] Smallest matching verification command was run
- [ ] Affected consumer module was checked if the boundary changed
- [ ] Canonical docs were updated if behavior or workflow expectations changed
- [ ] Any unverified paths were called out explicitly

