# Dynamic Navigation Phase 1 Status

## Status

- Phase: 1 — Replace Persistent Sidecars
- Confidence: High
- Primary owner: engine content and streamed navigation runtime
- Affected consumer: Actiongame Crossfire navigation bundle and locomotion fixture
- SME alignment required: No

## Implemented

- Replaced `.gknavsource` and `.gknavgraph` JSON tiles with versioned `.gkns`
  and `.gkng` gzip-compressed binary tiles.
- Added fixed magic, binary/schema versions, endian marker, payload length,
  deterministic string tables, integer source spans, acceptance bitsets, CSR
  ordinary edges, regions, and explicit exceptional transitions.
- Added manifest SHA-256 and byte-size checks for every sidecar.
- Reused the existing bake, delta, runtime, and diagnostic APIs; no legacy
  reader or compatibility adapter remains.
- Rebaked Crossfire as `crossfire-nav-v5` and removed all generated `*-nav-v4`
  bundles for Crossfire, Gasworks, and Subtransit.

## Preserved Invariant

Tile round-trips preserve the existing in-memory navigation topology exactly,
while loads reject wrong versions, corruption, truncation, and sidecars whose
bytes do not match the manifest.

## Verification

- `env GOCACHE=/tmp/gekko3d-gocache go test ./content/...` — pass
- `env GOCACHE=/tmp/gekko3d-gocache ACTIONGAME_CROSSFIRE_NAV_TEST=1 go test ./src/modules/startup -run '^TestActionGameNPCNavigationCrossfireStairUsesFootprintSupport$'` — pass
- `env GOCACHE=/tmp/gekko3d-gocache go run ./cmd/navdiag -nav <crossfire-nav-v5>/crossfire.gknav -json` — 288 source tiles, 288 graph tiles, zero validation errors
- Crossfire bundle: `28,887,907` bytes (`5,778,340` source, `22,844,964`
  graph, `264,603` manifest), below the `150 MB` acceptance budget

## Not Verified

- The root-package test sweep is blocked by the pre-existing renderer bridge
  audit failure at `mod_voxelrt_client_systems.go:745` (`BufferManager`
  touchpoint). It is unrelated to navigation persistence.
- Gasworks and Subtransit were intentionally not rebaked because no shipped
  level references those generated diagnostic bundles.
