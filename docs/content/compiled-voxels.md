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
