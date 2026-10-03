# C1: Lossless compiled voxel frames

Status: C1a complete. The user approved the optional C1
layers/dictionaries and broader compiled-asset/region-pack follow-ups on 2026-10-03.
Follow the parent roadmap's dependencies; C2 retains its measured-scale gate.

## Boundary and batches

C1a delivers a content-owned `content/voxelcodec` frame codec and one explicit
`brick_zstd_binary_v1` imported chunk kind through existing save/load dispatch.
This proves the shared schema with a real consumer. JSON authoring, both RLE
kinds, aux V1, manifests and mutable public maps retain their contracts. The first
imported adapter returns ordinary voxel records; direct resident brick decoding
is later work, not a claimed C1a benefit.

The shared codec supports optional raw material and exact auxiliary byte layers,
and an optional caller-provided raw zstd dictionary. Initial imported chunks keep
their existing auxiliary sidecars. Follow with embedded eligible normal layers
and immutable runtime dictionary configuration, then compiled assets under C3.
Range-pack wrapping reuses independent frames if C2's gate is satisfied. Neither
P1b's private layout nor GPU allocation metadata is a disk ABI.

This is the long-term compiled-content path. A whole-level compressed stream
would prevent bounded range loading; replacing old kinds would break migration
compatibility. One independently bounded frame per chunk/section best matches
the island page forest and avoids introducing another regional page index.

## Contract

The [canonical compiled-frame contract](../content/compiled-voxels.md) defines
channels, identity, wire format, limits, dictionaries and resource ownership.
Keep this format separate from private prepared storage and GPU allocation.

## Imported compatibility and verification

New compiled saving skips primary-zero records, rejects duplicate occupied cells,
validates chunk-local bounds and preserves raw secondary bytes. Limits count
actual occupied cells and expanded channels, not the entire sparse chunk volume.
Validate chunk dimensions and coordinate arithmetic without allocating a dense
chunk cube. Decoding returns
global x-fast voxel order, preserving construction history across sector boundaries.
Existing save defaults and legacy acceptance remain unchanged. Repeated equivalent
compiled saves retain `Wrote=false`. Decoder errors return no partial document.

Affected files: new shared codec and imported adapter, imported kind/IO dispatch,
`go.mod`/`go.sum`, focused content/runtime tests and owning content docs.
Use separate tests/implementation and independent PRE/POST reviews. Cover real
save/load, independent ranges, canonical identity, both channels, exact optional
layers, dictionary identity, malformed/bounded decode, output ownership, terminal
Close and existing runtime-loader reuse/effective material conversion.
Run focused content/root and race checks, then full engine/consumer checks once.
Native imported readiness/edit/collision/cleanup checks cover renderer integration;
no pixel/FPS claim. Measure file sizes and decode cost against JSON/RLE, with
separate dictionary/profile results. Subsequent batches extend the approved path.
