# Compiled Voxel Frames

`content/voxelcodec` owns the shared lossless CPU/disk format used by compiled
content. Authoring JSON and legacy payload readers remain supported. The
[architecture decision](../roadmaps/streamed-rendering-c1.md) records rationale
and delivery boundaries.

## Shared logical contract

`Document` contains a bounded nonempty UTF-8 `Kind`, opaque `Metadata`, a bounded UTF-8
`NormalBakeVersion`, and `Bricks`. Each brick contains:

- `Coord [3]int32`, in units of eight voxels in its owner's source lattice;
- `Occupancy [8]uint64`, with bit `x + 8*y + 64*z`;
- `Values []uint8`, one nonzero primary byte per occupied cell in bit order;
- optional `Materials []uint8`, one raw secondary byte per occupied cell;
- optional `Aux []byte`, preserved exactly, including empty nonnil layers.

Primary and secondary bytes remain separate. Secondary zero is valid and must
not be confused with an absent layer or mixed mode. Normal data stays opaque:
preserve every fitted-normal validity/two-sided bit, occupancy byte and word for
unoccupied cells. No normal recomputation or six-neighbor replacement occurs.

An empty document is valid. Allocated bricks must be occupied, have exact channel
cardinality and unique coordinates. Encoding sorts a copy of the brick list by
signed `(x,y,z)` and leaves caller slices unchanged. Uniform channels use one
byte; mixed channels use occupied bytes. Decoding requires canonical channel
modes and strict coordinate order, rejects unknown modes and owns its output.

Content identity is lowercase SHA-256 of canonical uncompressed document bytes,
including codec version, kind, metadata, bake version and layers. Dictionary and
compression choices do not change identity. Owner metadata must carry its lattice,
source references and versions; including it inside the checksummed/hash-verified
body binds it to the geometry. The codec treats metadata as opaque bytes; it does
not normalize JSON. Imported metadata uses deterministic owner-defined JSON.

`Identity(Document) (string, int64, error)` returns that hash and canonical body
length without compression. It retains logical limits, input immutability and
terminal Close, shares encode serialization, and returns zero results on error.
The encoded-size limit applies only when producing a frame.

## Wire format v1

Integers are little-endian. Frame header is 96 bytes:

| Offset | Field |
| --- | --- |
| 0 | Eight-byte magic `GKBRCK1\n` |
| 8 | Codec version `uint16`, exactly 1 |
| 10 | Reserved flags `uint16`, zero |
| 12 | Compressed body length `uint64` |
| 20 | Uncompressed body length `uint64` |
| 28 | Dictionary ID `uint32`, zero when absent |
| 32 | Dictionary SHA-256, 32 zero bytes when absent |
| 64 | Canonical document SHA-256, 32 bytes |

Exactly one checksummed ordinary zstd frame follows. Reject skippable frames,
concatenated frames, trailing bytes, missing checksums and inconsistent zstd
dictionary headers. Any present zstd content-size field must match the outer
decoded length; a missing field is valid. Validate the encoded extent and decoded limit
before allocating; verify exact decoded length and SHA-256 before publication.

Canonical body starts with version `uint16`, kind length `uint16` and bytes,
metadata length `uint32` and bytes, bake-version length `uint16` and bytes, then
brick count `uint32`. Each brick stores three signed `int32` coordinates, eight
occupancy `uint64` words, four mode bytes (primary, secondary, aux, reserved),
then channel/layer data. Primary modes: 1 uniform, 2 mixed. Secondary modes:
0 absent, 1 uniform, 2 mixed. Aux modes: 0 absent, 1 raw length `uint32` plus
bytes. Reserved mode byte is zero. Canonical mixed channels contain at least
two distinct bytes. No live GPU offsets, flags or renderer revision counters.

The imported document kind is `imported_chunk`. Its metadata includes world ID,
schema, chunk coordinate, chunk size, voxel resolution, occupied count and tags.
The imported adapter validates metadata/lattice against every brick/cell.
`PayloadHash` is document identity; `PayloadSizeBytes` is canonical decoded body
length for this new kind. Old kinds retain their original size/hash definitions.

### Imported embedded normals

An optional imported `aux` metadata extension contains `schema_version` (exactly
1), `source_payload_hash` and `source_payload_size_bytes`. The source is the
geometry-only C1a document: original imported metadata without this extension,
unchanged raw primary/material channels, no brick auxiliary bytes and an empty
bake version. `ImportedWorldChunkCompiledGeometryIdentity` computes this hash
and body size without mutating the input. The final frame identity includes the
extension, bake version and all normal bytes; it is never its own source hash.

Embedded normal records require the current `ImportedWorldNormalBakeVersion`,
matching auxiliary world/schema/coordinate/lattice and geometry source identity.
Each unique aligned nonnegative record origin must fit signed int32 and identify
an occupied source brick. Each record contains exactly 1,088 bytes; its first 64 little-endian
occupancy bytes must equal the source occupancy. Preserve every remaining byte,
including normal words for unoccupied cells. Partial coverage is valid, but a
declared layer requires at least one record. Imported embedded documents require
byte-canonical owner metadata and present raw material layers on every brick,
making geometry identities and no-op resaves reproducible. Geometry-only C1a
frames retain their acceptance of absent materials and noncanonical owner JSON.

Decode validates these bindings before publishing `EmbeddedAux`. The field is
excluded from authored JSON, retains the geometry source identity and owns its
records. Auxiliary payload hash/size use the existing aux-record encoding in
canonical coordinate order. `SaveImportedWorldChunkCompiledWithAux` accepts an
explicit layer; nil removes it. Successful saves publish an independent canonical
copy or clear the field, leaving the supplied auxiliary input unchanged. Existing
compiled save APIs retain the field; stale geometry rejects retained normals
until rebaked or explicitly removed. Matching resaves return `Wrote=false`.

Public conversion trusts this field as validated shipping data and copies it
once. Callers assigning it directly must meet the same eligibility contract.
Full/proxy preparation prefers embedded records, skips sidecar loading, includes
the selected normal identity in prepared keys and retains existing sidecar
fallback when no embedded layer exists. Existing decoded graph accounting and
leases own the records; live geometry copies remain independent. Legacy auxiliary
readers/application, normal fitting, halos, collision and navigation retain their
contracts.

### Imported compiler emission

`importers/common.ImportedWorldSaveOptions.EmbedNormals` requires the explicit
compiled kind. `ChunkCodec` borrows the caller's profile for compiled output and
previous-frame reads; nil selects the bounded dictionary-free default. Legacy
kinds ignore the profile. Incompatible embedding options fail before output.

Embedded emission compares geometry-source identities before overwriting prior
frames. Full normals reuse validated prior records only when the chunk and its
27-cell neighborhood are unchanged. Changed, deleted or moved coordinates
invalidate neighboring full records; missing or unreadable prior frames require
rebaking. Proxy normals retain local-only fitting and geometry-based reuse.
Neighborhood selection uses bounded indexed lookups; fitting retains its current
cost. Save each final frame once and publish its final manifest identity, clearing
the sidecar reference. Existing sidecar files remain. Equivalent fresh emissions
preserve bytes and file times. Existing geometry write/skip counters count final
frames; auxiliary reuse counters include actual embedded reuse, while sidecar
write/skip counters stay zero.

`EnsureImportedWorldAuxSidecarsForManifestWithCodec` accepts the same borrowed
profile. Validated embedded records satisfy the normal requirement without
sidecars. Geometry-only compiled chunks still receive sidecar backfill bound to
their geometry payload identity. The original ensure function uses nil; legacy
valid-reference fast paths remain. Embedded checks without reference changes
leave the manifest unchanged. These APIs never close the supplied codec.
Automatic dictionary selection, distribution and training remain separate.

## Bounds and ownership

`voxelcodec.New(Options)` owns reusable encoder/decoder resources. Encode and
decode each permit one active operation; Close is terminal and idempotent. No
global frame/dictionary cache. Returned bytes/documents remain independent of
later operations and Close. Callers keep input slices stable during calls.

Default limits: 32 MiB encoded and decoded bytes, 16,384 bricks, 1,048,576 occupied
voxels, 1 MiB metadata/aux per layer, 64 KiB dictionary, 64-byte kind and 128-byte
bake version. Positive explicit limits may change these ceilings; negative or
unrepresentable limits fail. Optional inclusive brick bounds validate coordinates.
Check counts, overflow, cardinality, remaining bytes and expanded channels before
allocating output slices. These finite work/storage bounds are not a heap/RSS cap.

`New(Options) (*Codec, error)`, `Encode(Document) ([]byte, Info, error)` and
`Decode([]byte) (Document, Info, error)` expose the owned codec. Info's encoded
length includes the 96-byte header and compressed body; decoded length counts
the canonical body. `ReadFrame(io.ReaderAt, offset, length int64)` returns
`(Document, Info, error)` and validates and reads only that independent
range. A future pack owns the TOC and verifies its range references separately.

An optional `Dictionary` has a nonzero caller-assigned ID and raw history bytes.
Copy bytes on construction, bound size and identify them by full SHA-256.
Dictionary ID/hash and the zstd frame's actual ID must agree. Missing or mismatched
dictionaries fail before decompression; ID reuse cannot silently select different
bytes. A configured dictionary codec also accepts dictionary-free frames.
Trained dictionary production and default
selection require corpus measurements and remain later compiler work.

Public names: `Brick.Values`, `Brick.Materials`, `Brick.Aux`; `Dictionary.ID` and
`Dictionary.Bytes`; `Options.Limits` and `Options.Dictionary *Dictionary`. Limits expose
`MaxEncodedBytes`/`MaxDecodedBytes` as `int64`; `MaxBricks`, `MaxVoxels`,
`MaxMetadataBytes`, `MaxAuxBytes`, `MaxDictionaryBytes`, `MaxKindBytes` and
`MaxBakeVersionBytes` as `int`. `Limits.Bounds` optionally points to copied
`Bounds.Min`/`Bounds.Max` coordinates. Info exposes `ContentID`, `EncodedBytes`,
`DecodedBytes`, `DictionaryID` and `DictionaryHash` (empty for ID zero).
Methods return zero result/Info on failure; post-Close operations return
`ErrClosed`. `Close()` returns an error and is safe with concurrent callers.

Pin `github.com/klauspost/compress` v1.18.7, compatible with this module's Go 1.24.
Use explicit checksum, concurrency 1, `SpeedDefault` and 1 MiB window settings;
bound decode output/window and retain checksum validation. No encoder padding.
Default imported IO reuses one bounded dictionary-free codec. Explicit
`SaveImportedWorldChunkCompiledWithCodec` and `LoadImportedWorldChunkWithCodec`
allow caller-owned profiles; legacy loading does not depend on that profile.


## Ordinary voxel-object override payloads

`content.VoxelObjectPayloadDef` adds an opt-in schema-2 override in C1 kind
`voxel_object_override`. Generic frame version 1 and existing kinds are unchanged.
Metadata binds `Mode`, `PlacementID`, `ItemID`, `Lattice` and optional
`BaseIdentity`. Lattice contains a finite positive float32 `VoxelResolution` and
nonempty UTF-8 `RasterizationVersion` of at most 128 bytes. Owner IDs are nonempty
UTF-8 strings of at most 1,024 bytes. Metadata must equal the typed canonical JSON
encoding; unknown fields, alternative ordering and whitespace are rejected.

- `full`: masks describe complete geometry, primary bytes are nonzero materials,
  secondary layers are absent and `BaseIdentity` is empty.
- `base_delta`: masks describe assigned cells, primary bytes are presence marker
  1, mandatory secondary bytes are final materials including explicit zero
  removals, and `BaseIdentity` is lowercase SHA-256 hex.

Both modes reject auxiliary layers and bake versions. Empty documents are valid;
allocated bricks still require nonempty masks. Coordinates reconstruct into
signed int32 voxels using floor division across negative brick seams. New final
assignments are unique; duplicate coordinates and zero full records are rejected.

`VoxelObjectBaseIdentity(base, lattice, codec)` hashes the separate
`voxel_object_base` domain: canonical actual geometry and lattice/rasterization
metadata, with no owner IDs, paths, runtime IDs, transforms or renderer data.
Legacy base records retain ordered last-write-wins semantics; zeros remove cells
before hashing. Dictionary and compression choices do not change identity.

`EncodeVoxelObjectPayload` and `DecodeVoxelObjectPayload` convert owned records
without mutating inputs. Schema 0 encoding defaults to 2; compiled decoding
requires 2 explicitly. `SaveVoxelObjectPayload` uses existing synced atomic file
replacement. `LoadVoxelObjectPayload` uses bounded C1 `ReadFrame` for compiled
files. Explicit codecs are borrowed and their limits/lifetime remain authoritative;
nil selects a reusable default. Adapter temporaries scale with actual input
records, not coordinate extent; profile validation follows conversion. This is
not a pre-admission bound on those temporaries.

`ResolveVoxelObjectPayload(payload, base, lattice, placementID, itemID, codec)`
validates owner/lattice bindings and independently hashes the supplied base before
applying deltas. It returns owned schema-1 full snapshot records, or nil on error.
Incoming assignments and merged geometry obey the codec's logical limits. An
empty delta preserves the base; removing every base cell yields empty geometry.

Existing `SaveVoxelObjectSnapshot`/`LoadVoxelObjectSnapshot` and schema-1 JSON
remain unchanged. The new decoder also accepts legacy schema-0/1 JSON as explicit
schema-1 unbound full payloads. Legacy resolution preserves ordered records and
whole-file JSON acceptance, without applying C1 limits. Compiled schema 1 and
schema-2 JSON are rejected. Runtime provenance, S4 publication and reload dispatch
follow separately; this content API does not automatically change existing saves.
