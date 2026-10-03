# C1: Lossless compiled voxel frames

Status: C1a, C1b and C1c complete. Compiler emission and neighborhood reuse are
next. The user approved the optional C1
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

C1b fixes an optional borrowed codec profile on the existing loader owner; see
[decoded ownership](../assets/runtime-assets.md#decoded-content-lifetime).
Per-load profiles would require new key semantics and could alias dictionaries.
A fixed owner fits existing scope inheritance and runtime loader injection.

### C1c: Embedded fitted normals

Use two identities. The final frame hashes all canonical bytes, including normals
and their bake version. Its geometry-source projection is exactly the C1a
`imported_chunk` document: original owner metadata, raw primary/material channels,
no auxiliary metadata/layers and an empty bake version. Store that projection's
hash/decoded size in an optional versioned `aux` metadata extension. Recompute
and verify it before publishing decoded imported content. A final-frame source
hash inside its own metadata would be circular; legacy sidecar references retain
their existing rules.

Public additions:

- `Codec.Identity(Document) (string, int64, error)` validates the canonical body
  and returns SHA-256/decoded size without compression. It shares encode
  serialization, retains logical limits and terminal Close, and returns zero
  values on failure. Encoded-size limits do not apply to identity-only work.
- `ImportedWorldChunkCompiledGeometryIdentity(*ImportedWorldChunkDef, *Codec)`
  returns `(string, int, error)` for that geometry projection, without mutating
  input. Nil codec selects the default.
- `ImportedWorldChunkDef.EmbeddedAux *ImportedWorldChunkAuxDef` uses `json:"-"`;
  runtime shipping data does not become authored JSON.
- `SaveImportedWorldChunkCompiledWithAux(path, chunk, aux, codec)` returns the
  existing save result. Nil aux explicitly writes geometry only; nil codec uses
  the default. Existing compiled save APIs retain `chunk.EmbeddedAux` when set.
  Successful saves publish an independently owned canonical auxiliary copy;
  explicit removal clears the field. The supplied auxiliary input is unchanged.

The metadata extension contains `schema_version` (exactly aux schema 1),
`source_payload_hash` and `source_payload_size_bytes`. It is excluded only by the
owner's geometry projection; generic codec metadata remains opaque. Embedded
input requires matching world/coordinate/lattice/schema, the current fitted bake
version, matching geometry-source identity, and unique aligned nonnegative
records for existing occupied bricks. Each record is exactly 1,088 bytes; its
first 64 occupancy bytes must match the corresponding source brick. Preserve all
normal bytes, including words for unoccupied cells. Partial record coverage is
valid; a declared embedded layer must contain at least one record. Reject invalid
embedding without changing old aux loading/application acceptance.
Embedded documents require the imported writer's canonical metadata bytes and
present raw material layers on every brick. This keeps the geometry projection
and no-op resaves reproducible. Geometry-only imported frames retain C1a's
acceptance of absent secondary layers and noncanonical owner JSON.

Decoded EmbeddedAux retains the geometry-source hash/size and owns its record
bytes. Its auxiliary payload hash/size use the existing aux-record encoding over
canonical coordinate order. No-op compiled resaves retain layers and Wrote=false;
changed geometry rejects stale normals until rebaked or explicitly removed.

Validated embedded normals take priority over sidecar references; otherwise keep
the current sidecar fallback. Existing fitting, dense construction, live copies,
halo invalidation, registration, collision and navigation remain. The decoded
chunk graph owns embedded storage and existing graph charging/pins count it once.
Full/proxy results use the selected normal identity, including P1b sources.
Public conversion trusts `EmbeddedAux` as validated shipping data, applies an
independent copy once, and ignores a competing sidecar argument. Callers assigning
the field directly must satisfy the same eligibility contract; conversion does
not repeat hashing with a potentially different codec profile or limits.

Scope: shared identity helper, imported compiled adapter/schema, imported geometry
conversion, full/proxy aux selection and owning docs. Compiler emission flags and
neighborhood reuse follow separately. Separate tests/implementation and independent
PRE/POST reviews cover identities, exact bytes, stale/malformed layers, no-op
resaves, public conversion, full/proxy selection, ownership and existing fallback.
Run focused/race checks, engine/consumer boundary checks and native normal-byte,
readiness/edit/cleanup checks. No shader or fitting change; no pixel/FPS claim.

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
