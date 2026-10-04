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

`voxelcodec.DefaultLimits()` returns an independent value with the unchanged
default profile. Default limits: 32 MiB encoded and decoded bytes, 16,384 bricks, 1,048,576 occupied
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

`content.VoxelObjectPayloadDef` supports schema-2 and opt-in schema-3 overrides
in C1 kind `voxel_object_override`. Generic frame version 1 and existing kinds are unchanged.
Metadata binds `Mode`, `PlacementID`, `ItemID`, `Lattice` and optional
`BaseIdentity`. Lattice contains a finite positive float32 `VoxelResolution` and
nonempty UTF-8 `RasterizationVersion` of at most 128 bytes. Owner IDs are nonempty
UTF-8 strings of at most 1,024 bytes. Metadata must equal the typed canonical JSON
encoding; unknown fields, alternative ordering and whitespace are rejected.

- Schema 2 `full`: masks describe complete geometry; primary bytes are nonzero
  materials. Secondary layers are absent and `BaseIdentity` is empty.
- Schema 2 `base_delta`: masks describe assigned cells; primary bytes are presence
  marker 1. Mandatory secondary bytes are final materials including explicit zero
  removals, and `BaseIdentity` is lowercase SHA-256 hex.
- Schema 3 `hybrid_delta`: `ReplacementBricks` selects complete brick geometry.
  Listed records have nonzero primary materials and no secondary layer. A listed
  coordinate with no physical record explicitly clears all 512 base cells.
  Unlisted records retain schema-2 assignment semantics. Base identity is required.

All modes reject auxiliary layers and bake versions. Empty documents are valid;
allocated bricks still require nonempty masks. Coordinates reconstruct into
signed int32 voxels using floor division across negative brick seams. New final
assignments are unique; duplicate coordinates and zero full records are rejected.
Replacement selectors are unique and sorted lexicographically by X, Y, Z in
canonical metadata. Encoding sorts an owned copy; metadata validation and decoding
require canonical order. Each selected whole 8×8×8 cube must fit signed int32,
including empty clears: brick axes range from -268435456 through 268435455.
The logical brick limit counts the union of selectors and physical records once.
Empty selectors also obey the codec bounds; selector metadata and final merged
geometry remain bounded by the explicit profile.

`VoxelObjectBaseIdentity(base, lattice, codec)` hashes the separate
`voxel_object_base` domain: canonical actual geometry and lattice/rasterization
metadata, with no owner IDs, paths, runtime IDs, transforms or renderer data.
Legacy base records retain ordered last-write-wins semantics; zeros remove cells
before hashing. Dictionary and compression choices do not change identity.

`EncodeVoxelObjectPayload` and `DecodeVoxelObjectPayload` convert owned records
without mutating inputs. Schema 0 encoding defaults to 2; compiled decoding
requires 2 or 3 explicitly. `SaveVoxelObjectPayload` uses existing synced atomic file
replacement. `LoadVoxelObjectPayload` uses bounded C1 `ReadFrame` for compiled
files. Explicit codecs are borrowed and their limits/lifetime remain authoritative;
nil selects a reusable default. Adapter temporaries scale with actual input
records, not coordinate extent; profile validation follows conversion. This is
not a pre-admission bound on those temporaries.
`ValidateVoxelObjectPayloadMetadata` validates only schema, mode, owner, lattice
and base identity, plus canonical schema-3 selectors, without inspecting voxel
records.
Valid schema-2 metadata remains allocation-free.
Callers must separately validate assignments and codec capacity.
`VoxelObjectPayloadMetadataSize` validates canonical typed metadata and returns its
JSON byte length without inspecting voxel records or mutating inputs. It uses
bounded JSON temporaries; it is not an allocation-free profile admission API.
`Codec.Limits()` returns normalized immutable profile values with an independent
bounds copy, including after close; encoding and decoding still reject closed codecs.

`ResolveVoxelObjectPayload(payload, base, lattice, placementID, itemID, codec)`
validates owner/lattice bindings and independently hashes the supplied base before
applying deltas. Hybrid resolution clears selected base bricks before applying
records. It returns owned schema-1 full snapshot records, or nil on error.
Incoming assignments and merged geometry obey the codec's logical limits. An
empty delta preserves the base; removing every base cell yields empty geometry.

Existing `SaveVoxelObjectSnapshot`/`LoadVoxelObjectSnapshot` and schema-1 JSON
remain unchanged. The new decoder also accepts legacy schema-0/1 JSON as explicit
schema-1 unbound full payloads. Legacy resolution preserves ordered records and
whole-file JSON acceptance, without applying C1 limits. Compiled schema 1 and
schema-2/3 JSON are rejected. Legacy payloads reject explicit hybrid mode or
nonempty replacement selectors.
`CurrentVoxelObjectPayloadSchemaVersion` remains 2 and
`HybridVoxelObjectPayloadSchemaVersion` explicitly opts into 3.

Runtime writing selects schema 3 only when `EnableHybridVoxelObjectDeltas` is
true and its capture/cost policy qualifies. Schema-2 defaults remain fixed.
Loading and managed restoration accept both versions. The runtime's shape-only
[override loading contract](streaming-and-worlds.md#ordinary-object-override-loading)
adds bound schema-2/3 resolution. [Runtime persistence](streaming-and-worlds.md#ordinary-object-override-persistence)
selects sparse deltas through existing S4 publication. Explicit managed Enable
can restore canonical history under the loading contract above.

## Compiled ordinary asset shape frames

`CompiledAssetShapeDef` is a content-owned canonical geometry section, not an
asset header or an authored source definition. Explicit schema 1 contains a
`VoxelObjectLatticeDef` and owned C1 `Bricks`. The document kind is
`compiled_asset_shape`; typed metadata is byte-canonical JSON containing
`schema_version` then `lattice`. A nonempty bake version, secondary/material
layer or auxiliary layer is invalid, including an empty nonnil layer. Every
occupied primary value is nonzero. The whole 8×8×8 cube at each brick coordinate
must fit signed int32: every axis is in `[-268435456, 268435455]`.

`EncodeCompiledAssetShape` preserves inputs and canonicalizes brick ordering.
`DecodeCompiledAssetShape` returns independent canonical bricks without expanding
per-voxel records. Empty geometry is valid. Generic C1 cardinality, checksum,
identity, dictionary and profile limits remain authoritative. Adapter validation
adds schema, lattice, kind, metadata and portable-coordinate requirements.
Errors return nil data/definitions and zero `Info`.

`CompiledAssetShapeBaseIdentity` hashes exactly the existing `voxel_object_base`
projection: metadata contains only `lattice`, with the same primary bricks and
no bake version. Its identity and decoded size equal `VoxelObjectBaseIdentity`
for equivalent actual geometry. It validates shape semantics first, then applies
logical codec limits to that projection; compiled-owner metadata and encoded-byte
limits do not govern identity-only work. It returns empty identity and zero size
on error. This preserves the existing E2 base domain.

`SaveCompiledAssetShape` uses the existing synced atomic replacement helper.
`LoadCompiledAssetShape` uses bounded C1 `ReadFrame`, with no JSON fallback.
Explicit codecs are borrowed; nil selects the reusable default profile. Closed
codecs fail, and callers own the supplied codec's lifetime. Inputs must already
represent canonical actual geometry; this adapter does not interpret source
history or apply ModelScale.

These APIs establish the part-frame format only. They do not select compiled
assets, construct runtime geometry or change authoring/editor loading. Asset
headers, compiler emission and runtime canonical-base access follow the
[C3 ownership design](../roadmaps/streamed-rendering-c3.md).

## Compiled ordinary model frames

`CompiledAssetModelDef` is a separate canonical post-scale primary geometry
section for source-model adapters. Schema 1 uses C1 kind `compiled_asset_model`;
byte-canonical metadata contains `schema_version`, `lattice`, then `dimensions`
(an explicit three-element uint32 array). Model identity includes declared
dimensions; `CompiledAssetModelBaseIdentity` projects only primary geometry and
lattice into the existing `voxel_object_base` domain. This projection does not
qualify a new source kind for managed edits.

Dimensions are source metadata, not occupied bounds or allocation sizes. Zero,
mixed-zero and full uint32 dimensions are valid, and occupied geometry may lie
outside them. Later runtime adoption must preserve declared model dimensions and
existing all-zero-dimension occupied-bounds fallback. Portable signed brick
coordinates, primary-only layers, lattice validation, owned decoding, canonical
ordering and codec limits match the shape-frame contract. Encode/decode/save/load
APIs borrow explicit codecs and return zero results on failure; save uses atomic
replacement and load uses bounded frame reads without authoring fallback.

This frame does not preserve raw sample order, duplicates or zero-color rows.
Legacy VOX/procedural APIs retain their raw-model contracts. Future compiler
adapters must bake surface/material tables from original inputs before primary
canonicalization. Compiled-model collapse remains unsupported: any future offline
collapse must consume original ordered sources before canonicalization. Existing
shape/header schemas, bytes and APIs are unchanged. Header references, compiler
emission and runtime adoption are separate dependent batches.

### Offline source-model preparation

The private compiler adapter builds owned CPU model frames and palettes for the
seven authored primitives, `vox_model` and `vox_scene_node`. Procedural generators
apply scale once; VOX uses the existing `ScaleVoxModel` once. Scene nodes select
through the existing inspector/resolver without baking scene transforms into
geometry. Lattice rasterization identity is `gekko-compiled-model-v1`, separate
from authored inline-shape qualification.

VOX inputs load privately per call. Palette colors/material maps and normalized
surface facts remain independently owned across compiler calls; surface facts
use original unscaled samples. Returned dependencies identify the resolved VOX
source. Primary packing reuses the inline compiler's portable checks and value
order; C1 encoding canonicalizes brick order. Declared dimensions remain separate
from occupied geometry.

Compiler inputs require finite positive scale, resolution and used primitive
parameters. VOX downscale uses existing float32 coordinate multiplication,
rounded/clamped dimensions and votes including zero. Tied highest color votes
fail explicitly because the legacy scaler's winner is nondeterministic; lower
count ties are accepted. Legacy scaling behavior is unchanged. Scene-node
preparation rejects cycles and out-of-range shape references before inspecting;
plain model selection still ignores unused scene graphs. Missing edges and DAG
sharing keep inspector semantics.

This CPU preparation boundary does not enable these sources in shipping compiler
headers or runtime loading. Header/palette serialization, closure publication and
verified runtime adoption remain separate batches; collapse and new edit-base
qualification remain excluded.

## Conservative asset LOD geometry construction

The private compiler builder constructs a separate in-memory 2× derivative from
canonical primary-only shape geometry. It occupies every coarse cell touched by
source geometry: signed coordinate `p` maps to `floor(p/2)`, and coarse cell `q`
covers source interval `[2q, 2q+2)`. Mapping stays anchored at zero; it does not
shift by occupied bounds or rescale to match source extents. Conservative coverage
can thicken surfaces and close narrow openings.

One nonzero occupied palette value is required across the whole part. Empty,
single-voxel, mixed-value and nonreducing geometry is ineligible. Malformed schema,
lattice, brick coordinates, layers or occupancy/value cardinality fail explicitly
before an ineligible result can hide another malformed brick. Construction visits
occupied bits rather than the bounding-box volume, including signed portable
extrema. Output bricks are sorted with values in occupancy order and own their
mutable storage independently of input and other calls.

The result retains the unchanged source lattice, sole value, source/coarse voxel
counts and occupied bounds with inclusive minima and exclusive `int64` maxima.
Bounds describe their respective coordinate grids. Reduction identity is
`occupancy-or-zero-anchored-2x-v1`. This result is not an authoritative shape or
base. A caller must bind it to the original shape identity through the separate
derivative frame contract below. Compiler qualification and opt-in are defined below. Runtime source/material
qualification and selection remain separate integration work. Level-0 geometry
remains authoritative.

## Compiled ordinary asset LOD frames

`CompiledAssetLODDef` owns C1 kind `compiled_asset_lod`, schema 1, factor 2 and
reduction version `occupancy-or-zero-anchored-2x-v1`. Canonical metadata binds the
original shape's C1 `SourceContentID`, unchanged `SourceLattice`, sole nonzero
`Value`, `SourceVoxelCount` and source/coarse occupied bounds. Minima are inclusive;
maxima are exclusive `int64` coordinates in their respective grids. Coarse bricks
contain only primary geometry; secondary or auxiliary layers are invalid,
including empty nonnil layers. The frame has no normal bake version or E2 base
projection and cannot be decoded as a shape, header or voxel-object payload.

Source bounds fit the signed-int32 source grid, including exclusive maximum
`2147483648`. Coarse bounds equal the actual occupied geometry and the signed
floor mapping of source bounds. Coarse brick coordinates fit
`[-134217728, 134217727]`. Source count must exceed coarse count and fit the summed
fine-cell capacity of occupied coarse cells clipped to the declared source bounds.
This rejects impossible sparse-edge counts without allocating or iterating a
source bounding volume. Source count is metadata; codec voxel limits apply to
actual coarse geometry. Structural validation cannot prove absent-source coverage,
source provenance or palette opacity. Compiler/runtime source and material
qualification remain required before selection.

`EncodeCompiledAssetLOD` preserves input and shares C1 canonical sorting and
identity. `DecodeCompiledAssetLOD` owns its decoded geometry and rejects noncanonical
metadata, unsupported kinds/versions and malformed bounds, counts or layers.
`SaveCompiledAssetLOD` uses synced atomic replacement; `LoadCompiledAssetLOD` uses
bounded C1 `ReadFrame` without authoring fallback. Errors return nil data or
definitions and zero `Info`. Explicit codecs remain borrowed; nil uses the existing
default profile. Existing shape/header formats and default compilation are unchanged.
Versioned header references and compiler opt-in are defined below. Runtime selection
remains separate integration work.

## Compiled ordinary model asset headers

`CompiledAssetModelHeaderDef` is a separate C1 envelope with kind
`compiled_asset_model_header`, schema 1 and compiler version
`gekko-compiled-model-asset-v1`. Existing header definitions, APIs and bytes remain
unchanged. Mixed metadata retains original model source kinds, inline shape
references and existing inline-only LOD references. Each model part has one
explicit frame reference binding path, model/base identities, encoded/decoded
sizes and a baked palette identity. Source paths are offline provenance; decoding
never opens them or follows frame/dependency references.

Canonical metadata sorts copied shape/LOD/model references by part ID and palette
tables by palette ID, preserving ordered asset metadata. Tables are limited to
4,096 parts, model references, palettes and material rows per palette; generic
codec metadata/decoded/encoded limits remain authoritative. Original descriptors,
material references, hierarchy and finite positive model scale/resolution/used
parameters validate before a copied group view reuses legacy inline closure rules.
Public `ValidateAsset` retains source-file checks; only private compiled metadata
validation skips those checks. Collapse and model LOD references are unsupported.

Model paths cannot overlap inline shape or LOD paths. Repeated paths require
identical physical metadata; repeated logical model identities require consistent
base identity/decoded size but may use different encoded profiles and part palettes.
Every baked palette is uniquely identified and used. Palette identity is SHA-256
of `compiled_asset_model_palette-v1` plus newline plus canonical static payload
JSON, excluding only its ID. Payload retains colors, ordered VOX material rows,
surface facts and PBR fields. Nil/empty containers remain distinct. Material
properties support nil, bool, UTF-8 strings and finite float32/float64 values;
integer/nested values are excluded to preserve portable numeric round trips.
Decoded numeric properties are float64, accepted by existing renderer accessors.
Surface keys are nonzero, with normalized unique tags and normalized kinds.
No registered IDs, palette source paths, animations or frame overrides persist.

Encode/decode/save/load borrow codecs, return owned decoded metadata and zero
results on failure, and preserve synced atomic save/bounded read behavior. Typed
canonical re-marshal rejects unknown fields, fixed-array padding/truncation and
noncanonical JSON. Private runtime selection uses lowercase `.gkmodelassetc`;
`.gkassetc` continues selecting only the legacy header kind, with no probing or
fallback. Whole-closure verification checks every model/inline/LOD reference before
preparation, including physical sizes, supported lattice, logical/base identities
and palette binding. Verification scopes borrow frames and own metadata/palettes;
cancellation or a closed origin releases only their child leases. Original model
source files are never reopened. The E2 canonical boundary still accepts only
inline parts and proves only the selected shape, including within mixed headers.
Compiler emission and public runtime adoption remain separate integration work;
legacy readers reject this explicit new envelope.

## Compiled ordinary asset headers

`CompiledAssetHeaderDef` is an empty-brick C1 document of kind
`compiled_asset_header`. The default remains schema 1 and compiler version
`gekko-compiled-asset-v1`, with unchanged canonical bytes. Typed metadata contains `schema_version`,
`compiler_version`, `asset` and optional `shapes`, in that order. It has no
normal bake version. The asset metadata retains schema 4, ordered parts,
hierarchy/transforms/pivots, palettes/materials, skeleton/animation references,
lights, emitters and markers. Inline voxel arrays must be empty; group parts
must not carry a voxel-shape payload. Other source kinds and static collapse
are unsupported in this version.

Asset, item, skeleton bone and joint IDs are explicit nonblank UTF-8. Existing authored validation
applies without source-file I/O or normalization. Inline parts require explicit
finite positive ModelScale and voxel resolution. At most 4,096 asset parts and
shape references are accepted. C1 metadata/body limits independently bound the
header. External animation and texture references remain shipping dependencies;
header loading neither reads nor resolves them.

Each inline part has exactly one `CompiledAssetShapeRefDef`; groups have none.
References contain `part_id`, `path`, `content_id`, `base_identity`,
`encoded_bytes` and `decoded_bytes`. Paths are canonical relative slash paths,
without parent traversal, backslashes, colons, NUL or invalid UTF-8. Hashes are
64 lowercase hexadecimal characters. Encoded size is at least 96 bytes; decoded
size is positive. Both obey the borrowed codec's corresponding byte ceilings.
Multiple parts may reference one path only with identical hashes and sizes.
Encoding sorts an owned reference-list copy by part ID, preserving asset part
order and caller metadata. Decoding requires byte-canonical typed JSON and owns
all nested data. No runtime geometry is published by these APIs.

`EncodeCompiledAssetHeader`, `DecodeCompiledAssetHeader`,
`SaveCompiledAssetHeader` and `LoadCompiledAssetHeader` retain C3a borrowed-codec,
atomic replacement, bounded frame read and zero-result failure rules. Header
validation proves reference structure only. Compiler/runtime adoption must
verify each referenced frame's identity and sizes, lattice and existing base
projection before publishing geometry. A header alone cannot prove those links.

Explicit schema 2 pairs with `gekko-compiled-asset-v2` and adds optional `lods`
after `shapes`. Schema 1 rejects nonempty LOD references; empty tables are omitted
in both versions. Authoritative shape references remain mandatory and unchanged.
Each `CompiledAssetLODRefDef` contains `part_id`, `path`, `content_id`,
`source_content_id`, `encoded_bytes`, `decoded_bytes`, `factor` and
`reduction_version`. At most 4,096 references are accepted, with at most one per
voxel part. `source_content_id` equals that part's ordinary shape ContentID;
factor is 2 and reduction version is `occupancy-or-zero-anchored-2x-v1`.
Paths, hashes and size limits follow shape-reference rules. LOD paths cannot
also name authoritative shapes. Shared LOD paths require identical identities,
source, factor, reducer and sizes. Repeated logical LOD ContentIDs across different
paths require identical source, factor, reducer and decoded size; encoded size may
differ between physical encodings. An owned LOD table is sorted by part ID.
Header validation performs no derivative-file IO and proves neither source
coverage nor opaque eligibility. Existing compiler defaults and runtime selection
remain unchanged; derivative loading and qualification require separate integration.

The header and independent shape frames avoid a new regional pack/index format.
Explicit runtime selection and coherent dependency emission follow the
[C3 design](../roadmaps/streamed-rendering-c3.md); existing authoring readers are
unchanged.

## Ordinary asset compiler emission

From the engine module, compile an explicit shipping artifact with:

```sh
go run ./cmd/assetcompile -in path/to/source.gkasset -out path/to/shipping/asset.gkassetc
```

Both flags are required; `-h` shows usage. The CLI requires the exact lowercase
`.gkassetc` output suffix before reading source files and uses the default codec
profile. Input extensions do not determine the authoring format; strict schema-4
JSON validation applies. The CLI exposes no profile or dictionary-training flags.

`gekko.CompileAuthoredAsset(inputPath, outputPath, codec)` produces an explicit
compiled header and its complete inline-shape dependency closure. It supports
`voxel_shape` and groups with independent parts; VOX/procedural adapters and
static collapse remain unsupported. Source schema 4 and persisted nonblank IDs
are required before existing normalization. Existing validation, animation
resolution and `ModelScale` construction define parity. The compiler leaves
source files unchanged. Inputs and dependencies must remain stable during a run.

Shapes use `shapes/<encoded-byte-sha256>.gkshape`; references retain C1 logical
content/base identities and exact frame sizes. Unique identical frames share a
file; different codec encodings receive different physical paths. Rigs and
opaque texture bytes are copied exactly into `dependencies`; animation sets
retain their format with rebased rig references. Asset animation/texture paths
are rebased into this output closure. External animation documents stay JSON.

Use `-lod2` or
`gekko.CompileAuthoredAssetWithOptions(inputPath, outputPath, codec,
CompiledAssetCompileOptions{EnableLOD2: true})` to emit conservative 2× derivatives.
Disabled options preserve the original API's schema-1 closure bytes. Enabled
options select the explicit schema-2 header even when no part is eligible.
Derivatives use the actual post-scale level-0 geometry, signed zero-anchored OR,
and original shape C1 ContentID; level-0 geometry and base identities stay unchanged.

Eligibility is per part: exactly one occupied nonzero palette value, strict voxel
count reduction, and its normalized material with alpha 255 and finite zero
transparency. Unused palette entries or materials do not disqualify a part.
Any material animation targeting that local palette value disqualifies it,
regardless of animation kind or frames. Separate geometric/skeleton animation
parts retain eligibility. Valid ineligible parts omit LOD references; invalid
source content still fails compilation.

LOD files use `lods/<encoded-byte-sha256>.gklod`, through the same complete
closure preflight, immutable publication and header-last owner. Parts share files
by exact encoded bytes, while opacity eligibility remains per part. The new
`CompiledAssetCompileDetailedResult` embeds the unchanged legacy result and adds
unique `LODsWritten`/`LODsReused` counts; failures return a zero detailed result.
Default CLI output is unchanged; opt-in appends a LOD count line. Runtime
preparation verifies all declared derivatives against their authoritative sources
before publication, while consumers still publish level-0 geometry only. Current
source/material qualification, display/readiness ownership and GPU/visual
verification remain pending.

All validation and encoding precede publication. Source/output aliases and
conflicting immutable files fail explicitly. Immutable files use verified reuse
or atomic no-clobber creation; reused files and directories are synchronized
before acknowledgment. The header is published atomically last. Existing header
comparison checks size before reading. A no-op build preserves existing files. Compilation never deletes older artifacts;
unreferenced immutable files can remain after a failure. Prepublication errors
preserve the previous header. Final atomic publication errors can leave a
complete old or new header without a durable acknowledgment.

`CompiledAssetCompileResult` reports header `Info`, `HeaderWrote` and unique
shape/dependency written/reused counts. Failures return a zero result. Codec
ownership remains with the caller. This compiler does not select runtime inputs
or alter existing authoring loading APIs.
