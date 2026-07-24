# Optional Voxel Backing Status

- Date: 2026-07-21
- Owner: authored-content / streamed runtime / destruction

## Goal

Allow removal-only destruction to continue through shell-like imported voxel
geometry without storing every interior voxel eagerly. Keep the mechanism
source-neutral so terrain and later destructive world renderers can use the
same runtime contract.

## Known State

- `XBrickMap` is the shared render, collision, raycast, and destruction
  geometry authority. Missing voxels currently mean empty space.
- Imported HL1 worlds store explicit surface or shallow solid-band chunks.
  Their BSP already provides a deterministic solid/empty classifier at import
  time.
- Terrain chunks already describe implicit solid columns, but destruction
  persistence snapshots rebuilt columns and therefore could not represent
  caves correctly.
- Streamed imported-world persistence currently writes complete edited chunk
  snapshots. This cannot regenerate solid matter behind an imported shell.

## Architecture Decision

Add an optional immutable `VoxelBacking` beside `XBrickMap`:

- backing providers classify or materialize base voxels for one finite chunk;
- the live `XBrickMap` remains a sparse cache and the only downstream geometry
  representation;
- destruction materializes only bricks touched by an edit before carving;
- a sparse per-brick removal mask is applied whenever a brick is materialized;
- worlds without a backing keep their existing behavior.

This is a long-term architecture step, not a temporary repair. The provider
contract is generic. The first providers are authored terrain columns and a
source-neutral plane-tree sidecar emitted from HL1 BSP data.

The initial persistence contract is removal-only. Voxel additions and mutable
base generators are intentionally outside this change.

## Alternatives Rejected

- Eagerly fill every imported solid voxel: simple, but makes memory and chunk
  payload size scale with solid volume.
- Keep an HL1 BSP object in the game runtime: compact, but couples the runtime
  and future terrain rendering to one importer.
- Generate an arbitrary fixed thickness after every hit: hides some holes but
  does not support reliable tunnelling or persistence.
- Replace `XBrickMap` with a second voxel store: duplicates renderer, physics,
  raycast, navigation, and editing ownership.

## Intended Touch Points

- `content`: optional backing sidecar schema and sparse removal delta schema
- `importers/hl1`: emit a generic plane-tree backing from BSP solid semantics
- terrain/imported-world spawn and streamed loading: attach providers and
  restore removal masks
- destruction: materialize touched bricks, then use the existing sphere carve
- world delta/navigation dirty flow: save removals and keep edited geometry as
  the downstream navigation source

No shader or renderer storage change is planned.

## Invariants

- Backing data is immutable and finite; outside its bounds is not materialized.
- Removal masks only turn base-solid voxels into empty voxels.
- Materializing the same brick repeatedly is deterministic and never restores
  a removed voxel.
- `XBrickMap` remains authoritative after materialization for rendering,
  physics, raycast, and navigation.
- Existing levels without backing metadata load and destruct exactly as before.

## Verification Plan

Automated checks:

- provider materialization and bounds
- repeated edits do not restore removed voxels
- removal-mask JSON round trip
- terrain columns and generic plane-tree providers use the same materializer
- streamed reload applies saved removal masks
- edits spanning adjacent loaded chunks are routed to both chunks

Manual checks:

- reimport Crossfire, shoot through a thick wall and ground, then continue the
  tunnel across brick and chunk boundaries
- verify no skybox/interior-shell hole appears and collision follows edits
- restart the level and verify removed voxels remain removed
- inspect GPU memory/upload pressure while digging; only touched bricks should
  become explicit

The manual visual/GPU checks cannot be replaced by unit tests and remain a
release gate for this feature.

## Implemented

- Added optional `.gkvoxelbacking` plane-tree content and manifest references.
- Added the generic runtime `VoxelBackingProvider` / `VoxelBackingComponent`
  contract with finite local bounds, sparse carve-shell materialization, authored
  material preservation, and removal-mask replay.
- Routed one destruction event across all intersecting loaded chunks belonging
  to the same backing owner/source.
- Added terrain-column and plane-tree providers to the same materializer.
- Added inline `VoxelBackingRemovals` world-delta persistence. Backed removals
  no longer use terrain column reconstruction or imported full-chunk snapshots.
- HL1 emission converts BSP planes to global voxel coordinates, emits a sibling
  sidecar, and catalogs finite backing-only chunks so tunnels can cross chunks
  without authored surface voxels.
- Backing sidecars can also carry compact, source-neutral surface-support
  triangles. HL1 imports add a 1.5 m inward band behind opaque upward-facing
  structural surfaces, preventing one explosive crater from crossing a thin
  floor while repeated removal still permits tunnelling.
- Runtime carving materializes an explicit one-voxel shell beyond the edit,
  including when removal persistence is restored, so an untouched adjacent
  backing brick cannot appear as sky or missing collision.
- Voxel-object world bounds track the live map revision, keeping BVH traversal
  valid when sparse backing materialization expands geometry beyond the
  previously explicit shell.
- Existing worlds without backing remain on their previous explicit snapshot
  and destruction paths.

## Verification Result

Automated:

- `env GOCACHE=/tmp/gekko3d-voxel-backing-gocache go test ./gekko/... -count=1`
- `env GOCACHE=/tmp/gekko3d-voxel-backing-gocache go test ./actiongame/... -count=1`
- `env GOCACHE=/tmp/gekko3d-voxel-backing-gocache go test ./gekko-editor/... -count=1`

The tests cover repeated materialization, authored surface material
preservation, terrain heights beyond one horizontal chunk size, cross-chunk
destruction routing, sidecar/content round trips, backing-only chunk emission,
and sparse world-delta reload.

Real import smoke:

- Reimported the original Crossfire BSP into a temporary directory with solid
  band depth 2. The emitted world validated successfully.
- Explicit voxel count stayed `6,027,664`.
- The finite BSP bounds produced 288 chunk catalog entries: 18 occupied and 270
  small backing-only chunks.
- The plane tree contains 7,004 planes, 2,982 nodes, and 1,856 leaves. Its JSON
  sidecar is about 1.2 MiB, independent of solid voxel volume.
- Incremental reimport skipped all 288 unchanged chunks and all 18 proxies.

Crossfire thin-floor follow-up:

- Reproduced the low central RPG field failure: exact BSP backing ended after
  about 0.25 m and the next surface was 3.6 m below.
- Reimport added 2,103 compact surface-support triangles; the backing sidecar
  grew from about 1.2 MiB to 1.9 MiB without adding explicit voxels.
- Runtime diagnostics verified generated material in both render and physics
  maps 1.1 m below the original field, plus the corresponding GPU sector,
  brick, occupancy, and material upload.
- User-run interactive validation on the low central RPG field passed after
  fixing stale world-AABB/BVH bounds; the crater now renders generated backing
  instead of the skybox.

Remaining manual stress checks:

- Shoot a backed vertical wall and verify no brick-shaped geometry appears
  beside the crater.
- Continue a tunnel across brick and chunk boundaries.
- Restart after destruction and verify the persisted removal shape.
- Inspect GPU memory/upload pressure during prolonged digging.
