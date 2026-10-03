# P1c: Sealed managed voxel ownership

Status: volume boundary complete; runtime integration awaits authority alignment.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md).

## Approved boundary

The user approved opt-in managed geometry on 2026-10-03. Managed owners retain
an immutable construction base and tracked edits. Public mutable exposure
irreversibly promotes that owner to dense authority and requires the existing
full-snapshot persistence fallback. Existing public mutable callers retain their
contracts. This is a long-term ownership boundary, not a temporary cache.

The alternative is a breaking public map migration across all consumers before
sharing. The opt-in boundary preserves compatibility while allowing independent
instances to share sealed geometry. Restricting existing raw writes or relying
on revision notifications from exposed maps would lose supported edits.

## Initial volume scope

Introduce `volume.ManagedXBrickMap` without changing `XBrickMap`, asset access,
renderer extraction, collision, navigation or persistence. Construction copies
the source defensively. Sealed forks share private bricks and independently own
map metadata. Writes detach affected bricks, including fitted-normal halo
neighbors, before using existing dense edit semantics. Inline dense payloads
remain in this first batch; public compact storage is not claimed.

The [managed ownership contract](../renderer/editing.md#managed-voxel-ownership)
defines snapshots, exposure, final changes, ordering and exclusive access.

## Follow-up dependencies

Runtime integration must qualify geometry explicitly and distinguish internal
sealed reads from public mutable exposure. Use existing asset and streaming
owners, byte accounting and dense fallback; do not introduce parallel residency.
E2 adds versioned base-bound delta encoding and S4 publication only after those
owners can retain tracking. Collision and navigation keep authoritative source
geometry and existing synchronous publication semantics. Public compact payloads,
progressive edits and asynchronous physics remain separate decisions.

Direct exported `VoxelRtState.RtApp.Scene.Objects` and related scene slices expose
`VoxelObject.XBrickMap` without a getter. Getter hooks therefore cannot reliably
detect public runtime exposure. Promoting at scene attachment preserves raw
authority but ends sealed tracking there. A separate dense renderer derivative
retains managed authority only with explicit rules for raw derivative writes,
pointer replacement and collision/persistence precedence. That extraction and
compatibility decision remains open; the approved volume boundary does not
silently make existing scene-map writes unsupported.

Use separate tests and implementation agents and independent PRE/POST reviews.
Verify source/fork/snapshot isolation, normal halos, solid expansion, tombstones,
removal/reinsertion, tracked reverts, raw exposure and panic prefixes. Run focused
and race checks, then engine and consumer boundary checks. GPU visual validation
begins with runtime integration; this isolated API does not alter rendered scenes.
