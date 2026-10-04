# C3: Compiled ordinary assets

Status: approved direction; C3a shape frames, C3b headers and C3c offline
compiler, C3d1 decoded-cache integration, C3d2 canonical-base adoption and C3d3
direct runtime preparation/spawning, C3d4a ordinary level placements and C3e
compiler CLI, C3f1 private shared adoption, C3f2 CPU packets and C3f3 streamed
worker integration, C3d4b NPC adaptation, C3d4c first-part level consumers and
C3f4a private palette adoption, C3f4b worker palette integration and C3f4c
publication accounting complete. C3h0 diagnostics and C3h1 conservative geometry
construction, C3h2 source-bound derivative frames, C3h3 versioned headers and
C3h4 compiler opt-in, C3h5 exact source validation and C3h6 scoped derivative
reads, C3h7 complete-closure source verification, C3h8 core render
representation boundary, C3h9 renderer consumer migration and C3h10 full-detail
upload staging, C3h11 pure qualification guards and C3h12a owned derivative
packets/accounting, C3h12b ordinary derivative adoption and C3h13a explicit
instance intent, C3h13b instance qualification, C3h13c runtime activation and
C3g1 pure primitive extraction, C3g2 canonical model frames and C3g3 pure palette
construction, C3g4 owned source-model preparation, C3g5 model headers, C3g6 scoped
reads, C3g7 whole model closure verification, C3g8 owned model packets/adoption and C3g9
public consumer integration, C3g10 shipping model emission and C3g11 CLI selection
complete. Opt-in,
single-material opaque LOD is approved; compiler
source-kind adapters remain separate work; mixed-material/transparency reduction stays conditional.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md#c3-compile-heavy-assets-once-add-asset-lod).

## Authority and ownership

Compiled shipping artifacts are explicit runtime inputs. The offline compiler
checks authoring sources and records compiler/rasterization identities. Runtime
validates artifact integrity and supported versions without rereading original
voxel JSON or VOX sources. Do not discover hidden sidecars, replace authored paths
silently, or fall back from corrupt explicitly selected compiled content.
Authoring `.gkasset` files and existing loading APIs retain their contracts.

Use independent C1 frames for canonical part geometry. A later versioned asset
header must retain stable asset/part IDs, hierarchy, transforms, pivots, material
and palette semantics, skeleton/animation bindings, markers and other supported
metadata. It must bound tables and frame references before allocation and bind
each frame's identity. Process-local IDs and GPU allocation offsets never persist.
This is the long-term compiled-content path, not a parallel cache or residency
service. Existing `RuntimeContentLoader` scopes own decoded content; `AssetServer`
and existing prepared-resource publication own registered geometry.

Start with inline `voxel_shape` and transform-only groups. Preserve separate
animated parts. The compiler must use actual post-scale geometry and existing
rasterization semantics, including signed coordinates. Unsupported source kinds
and unsupported static collapse fail explicitly until their own compiler adapters
exist. VOX declared dimensions, zero-color samples and collapse eligibility cannot
be inferred from occupied bricks alone. Asset LOD is separate work.

Level-0 compiled geometry remains collision and edit authority. Runtime adoption
must provide one canonical-base access boundary for authored and compiled inputs.
It must cover managed binding, delta restore and override resolution, preserving
placement/path membership proofs, original-base identities and independent public
mutable copies. Removing inline samples without that boundary would break E2
qualification/reload. No runtime adoption occurs before this dependency is resolved.

## Delivery

C3a adds a typed shape-frame owner over C1, without asset-header or runtime changes.
It accepts canonical nonzero primary geometry and lattice, preserves the existing
`voxel_object_base` identity projection, and returns independently owned bricks.
It avoids expanding decoded geometry into per-voxel records. Canonical contracts
belong in [compiled content](../content/compiled-voxels.md).

C3b defines a bounded empty-brick C1 header with typed metadata and independent
shape references. This avoids a new regional pack or TOC. It preserves external
animation/texture references as dependencies, without loading them. Compiler
emission must rebase or copy those dependencies coherently before standalone
shipping is claimed. Emit eligible geometry through the existing construction
path after the header contract is established. Runtime integration follows
through existing loader/registration owners,
with the canonical-base boundary above. Verify source parity, material/pivot and
hierarchy preservation, malformed content, scoped sharing, edit isolation and
save/evict/reload before claiming runtime loading gains. Separate tests and
implementation agents plus independent PRE/POST reviews apply to these boundaries.

## Ordinary worker preparation boundary

Use owned CPU packets with transient decoded scopes and P5 registration copies.
[Packet and publication contracts](../assets/runtime-assets.md#compiled-ordinary-asset-preparation)
retain direct warm preparation and ordinary `AssetServer` lifetime. Imported
cache leases could delete geometry still used by direct callers; a parallel
prepared cache adds another owner without demonstrated need.

Streamed integration uses existing pending admission and placement commit gates.
Compiler source-kind adapters, asset LOD and renderer-copy optimization remain
separate work.

Palette preparation uses the existing full JSON identity and ordinary palette
owner with independent source/publication storage. This avoids sharing packet
maps with public mutable assets or adding another cache. The
[publication contract](../assets/runtime-assets.md#compiled-ordinary-asset-preparation)
owns palette transfer and pending lifetime. Within one fixed owned asset definition, invocation-local
memoization uses exact JSON of the ordered value/material-ID binding slice.
This avoids repeated table construction while retaining final deduplication by
the sealed full palette key. Neither memo nor geometry identity becomes a
persistent palette cache.

## Offline compiler boundary

The first compiler accepts inline shapes and groups, with separate parts and
existing animation bindings. It rejects unsupported adapters and missing
persisted IDs before normalization can synthesize IDs. Existing normalization,
validation and construction define source parity; the compiler does not invent
new rasterization rules. Authoring and dependency inputs must remain stable
during compilation.

Compile complete geometry and dependency closure before publishing. Copy rigs
and textures exactly; rewrite animation-set rig references and asset dependency
paths into the output closure. Use encoded-byte hashes for physical filenames,
retaining C1 logical identities for geometry references. Different codec profiles
therefore cannot overwrite each other's frames. Immutable files require exact
verification or atomic no-clobber creation; publish the asset header last.
Validation and dependency failures preserve the previous header. Final atomic
publication errors may leave a complete old or new header without a durability
acknowledgment, following existing atomic-save semantics. Unreferenced immutable
files may remain after failure; compilation does not delete earlier artifacts.

## Cold-loading evidence

Three sequential local runs used Go 1.25.4 on darwin/arm64, `GOMAXPROCS=1`, warmed
filesystem caches and 100 ms benchmarks. Cold preparation uses a fresh
`AssetServer`; warm preparation reuses registered resources. Prepared spawning
uses a fresh ECS app with shared prepared resources. No renderer/GPU runs.
The local ActionGame corpus contains 433 assets with inline geometry; samples
below are existing local assets, not shipped test fixtures.

| Asset | Voxel records | JSON bytes | Load | Cold preparation | Load + preparation | Warm preparation | Prepared spawn |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `valve_models_w_357ammobox` | 2,195 | 401,482 | 2.48 ms | 1.72 ms | 4.10 ms | 0.78 ms | 0.007 ms |
| `valve_models_stealth` | 116,880 | 14,409,906 | 81.86 ms | 60.22 ms | 137.81 ms | 7.94 ms | 0.005 ms |
| `models_nihilanth` | 875,196 | 109,794,701 | 590.50 ms | 576.53 ms | 1,162.78 ms | 181.11 ms | 0.67 ms |

The largest sample allocates 1,082,877,968 bytes during load plus preparation,
including transient allocations; this is not retained memory or RSS. It has 126
parts and external animation bindings. Parse and preparation both matter;
existing prepared spawning is already cheap. A small procedural collapsed
asteroid takes 0.41 ms for cold load/spawn and 0.11 ms for prepared spawn, whose
existing collapse still resolves sources. It does not justify prioritizing
procedural collapse over large inline geometry.

These are baseline measurements, not compiled-content savings. They exclude
application startup, GPU upload, scheduling and filesystem cold-cache behavior.
Animation resolution and source validation remain part of preparation. No FPS
or production-frequency claim is made.

Temporary harness: `/tmp/gekko-c3-measure.go`; evidence:
`/tmp/gekko-c3-expanded-{1,2,3}.json`. Run from the engine module with
`env GOCACHE=/tmp/gekko3d-gocache GEKKO_C3_OUT=/tmp/gekko-c3-expanded-N.json go run /tmp/gekko-c3-measure.go`.

## Compiled runtime measurements

Three sequential runs compared current direct APIs with Go 1.25.4, darwin/arm64,
`GOMAXPROCS=1`, warmed filesystem caches and 100 ms benchmarks. Cold preparation
uses `LoadAndPrepareAuthoredAsset`, a fresh `AssetServer` and a fresh loader with
decoded retention disabled; warm preparation shares its loader/server. Cold
spawning uses `LoadAndSpawnAuthoredAsset` with a fresh ECS app/server. These
end-to-end paths include animation resolution and verification. Medians:

| Asset | Cold preparation ms, JSON / compiled | Cold spawn ms, JSON / compiled | Preparation allocated bytes, JSON / compiled |
| --- | ---: | ---: | ---: |
| `valve_models_w_357ammobox` | 4.39 / 2.85 | 4.11 / 2.97 | 4,071,464 / 2,017,360 |
| `valve_models_stealth` | 142.60 / 16.09 | 142.21 / 16.15 | 136,825,232 / 1,390,861 |
| `models_nihilanth` | 1,161.50 / 270.15 | 1,181.32 / 270.69 | 1,086,660,704 / 145,225,720 |

Large-sample cold preparation improves 8.86× and 4.30×, with 98.98% and 86.64%
fewer allocated bytes. Allocations include transient work, not retained memory
or RSS. Warm preparation is mixed: ammo regresses from 0.74 to 1.24 ms and
allocates 11.92% more bytes; stealth improves from 8.07 to 0.15 ms, and nihilanth
from 189.79 to 131.48 ms. Reuse an existing prepared asset when possible; prepared
spawning has unchanged allocation counts and no demonstrated speed benefit.

Exact shipping closures, including referenced animation/rig/texture files and
unique shape frames, shrink from 401,482 to 7,165 bytes for ammo, 14,409,906 to
6,502 for stealth and 128,446,555 to 19,295,577 for nihilanth. The initial compiler
source restrictions still apply. These local CPU measurements exclude startup,
GPU upload, scheduling and filesystem cold-cache behavior; they establish no FPS
or production-frequency benefit.

Harness: `/tmp/gekko-c3-compiled-measure.go`; evidence:
`/tmp/gekko-c3-compiled-{1,2,3}.json`. Reproduce from the engine with
`env GOCACHE=/tmp/gekko3d-gocache GEKKO_C3_OUT=/tmp/gekko-c3-compiled-N.json go run /tmp/gekko-c3-compiled-measure.go`.

## Worker packet CPU diagnostic

Three single invocations per asset used Go 1.25.4, darwin/arm64,
`GOMAXPROCS=1`, warm filesystem caches and fresh app/server/loader instances
with decoded retention disabled. Direct compiled preparation/spawning is compared
with synchronous CPU packet preparation followed by publication/spawning.
These are local medians, not robust benchmarks. Combined medians use each run's
preparation/publication sum; they are not sums of separate stage medians.

| Asset | Direct preparation/spawn ms | CPU packet preparation ms | Publication/spawn ms | Combined ms |
| --- | ---: | ---: | ---: | ---: |
| ammo | 2.915 | 2.771 | 0.592 | 3.364 |
| stealth | 16.254 | 16.824 | 0.048 | 16.873 |
| nihilanth | 299.786 | 260.029 | 25.008 | 286.363 |

Work shifts out of publication; total CPU time is mixed. Median allocated bytes
increase from 2,069,304 to 2,331,232 for ammo (+12.66%), 1,440,560 to 1,571,184
for stealth (+9.07%) and 147,783,808 to 149,577,064 for nihilanth (+1.21%).
Actual scheduler, pending admission, commit transaction and GPU costs are excluded.
No FPS, RSS or pixel-parity claim follows from this diagnostic.

Local harness: `/tmp/gekko-c3f3-measure_test.go`; evidence:
`/tmp/gekko-c3f3-measure.json`. Temporarily copy the harness into the engine as
`c3f3_measure_tmp_test.go`, run
`env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestC3f3Measure$' -count=1`,
then remove that temporary file. The harness is not part of the test suite.


## Worker palette publication diagnostic (C3f4b)

The same three-run CPU harness and exclusions above compare C3f3 with C3f4b.
Fresh app/server/loader instances and warm filesystem caches remain unchanged.

| Asset | Preparation ms | Publication/spawn ms | Combined ms | Combined allocated bytes |
| --- | ---: | ---: | ---: | ---: |
| ammo | 3.420 | 0.022 | 3.438 | 2,755,888 |
| stealth | 16.656 | 0.016 | 16.670 | 1,578,256 |
| nihilanth | 299.544 | 1.295 | 301.052 | 171,897,616 |

Compared with C3f3, nihilanth publication falls from 25.008 to 1.295 ms and
publication allocations from 21,421,704 to 2,839,576 bytes (86.74% lower).
Combined allocations rise 14.92%; combined time rises 5.13% in these separate
local runs. Ammo combined allocations rise 18.22%; stealth rises 0.45%.
The improvement is reduced main-thread publication work, with additional worker
cost. This does not demonstrate lower total CPU time or memory use. Allocation
overhead needs a separate follow-up; no FPS, RSS or pixel-parity claim follows.

Local harness: `/tmp/gekko-c3f4b-measure_test.go`; evidence:
`/tmp/gekko-c3f4b-measure.json`. Temporarily copy the harness into the engine as
`c3f4b_measure_tmp_test.go`, run
`env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestC3f4bMeasure$' -count=1`,
then remove that temporary file.


## Publication accounting diagnostic (C3f4c)

A three-preparation allocation profile of nihilanth identified constructor
bookkeeping as avoidable worker work. The constructor now counts independent
mutable clones directly and retains exact string alias accounting. General
loader accounting and publication ownership remain unchanged.

The same three-run stage harness gives these local medians:

| Asset | Preparation ms | Publication/spawn ms | Combined ms | Combined allocated bytes |
| --- | ---: | ---: | ---: | ---: |
| ammo | 3.275 | 0.025 | 3.297 | 2,622,200 |
| stealth | 16.107 | 0.026 | 16.166 | 1,577,424 |
| nihilanth | 294.985 | 1.152 | 296.031 | 166,089,776 |

Compared with C3f4b, combined allocated bytes decrease 4.85%, 0.05% and 3.38%
respectively; nihilanth saves 5,807,840 bytes. Exact charge parity and lower
estimator allocation counts are tested. Total allocations still exceed C3f3;
these separate local timing samples do not prove a total CPU improvement.
The earlier scheduler/GPU/RSS/pixel exclusions still apply.

Local harness/evidence: `/tmp/gekko-c3f4c-measure_test.go` and
`/tmp/gekko-c3f4c-measure.json`; reproduce using the temporary-file procedure
above with `^TestC3f4cMeasure$`. The diagnostic profile harness is
`/tmp/gekko-c3f4c-profile_test.go`, with baseline allocation evidence
`/tmp/gekko-c3f4c-alloc.pprof`; it is not part of the test suite.


## Asset LOD diagnostic (C3h0)

The reproducible [diagnostic harness](diagnostics/c3_lod.go) compiles the same
three local authoring inputs used above, then reconstructs canonical shapes and
measures existing `Resample(.5)`. Geometry is deduplicated by content identity
within each asset; part references remain independent. Three invocations per
shape use Go 1.25.4, darwin/arm64, `GOMAXPROCS=1` and warm filesystem caches.
Medians use sums across unique shapes. GC, decoding, source construction and
quality scans are excluded; production resampling stdout is included.

| Asset | Reduction ms | Allocated bytes | Estimated geometry writes: full / sampled bytes | Lost occupied coarse cells |
| --- | ---: | ---: | ---: | ---: |
| ammo | 0.143 | 19,120 | 67,840 / 35,840 | 291 / 602 |
| stealth | 4.055 | 241,032 | 606,016 / 159,872 | 2,004 / 17,664 |
| nihilanth | 35.104 | 3,046,888 | 11,617,216 / 2,634,880 | 46,435 / 155,793 |

All 1, 1 and 59 unique shapes satisfy existing runtime eligibility: more than one
source voxel, nonempty sampled geometry and strictly fewer sampled voxels.
Effective geometry totals therefore equal raw sampled totals for these assets.
Estimated fresh geometry writes decrease 47.17%, 73.62% and 77.32%. Estimates
include sector records, all brick table rows, occupancy/normal auxiliary storage
and mixed-brick payloads. They exclude material/lookup writes, allocator capacity,
normal baking work and actual upload timing. This is potential write reduction,
not measured GPU memory, frame-time or FPS improvement.

Coverage prevents adopting the existing sampler as the C3 default. Raw sampling
erases even-coordinate one-voxel planes; the existing runtime falls back to full
geometry for these empty proxies. Within the raw reduction, odd-coordinate planes
survive but thicken when expanded to the source grid. The tested one-voxel opening closes under both
sampling and occupancy OR. Adjacent designated transparent/opaque material IDs
share four coarse cells; sampling selects the opaque ID, while occupancy OR
leaves the material choice unresolved. Real assets have 437, 0 and 106,291
mixed-material coarse cells. Those counts do not classify real opacity.
CPU XY projections show these probes only; they omit engine extent/pivot
compensation, lighting and opacity rendering. No rendered quality acceptance
or GPU handoff verification is claimed.

Run from the engine module:

```sh
env GOCACHE=/tmp/gekko3d-gocache go run docs/roadmaps/diagnostics/c3_lod.go -out /tmp/gekko-c3-lod
```

The local HL1 inputs must exist; `-inputs` accepts other comma-separated eligible
assets. Output includes input hashes, individual samples, raw/effective geometry
counts, sampling completeness checks, source content preservation assertions and
CPU PNGs.
Local evidence: `/tmp/gekko-c3-lod/evidence.json`. The harness is excluded from
normal builds. Assertions also cover signed coordinates and asymmetric bounds.

Before production LOD:

- Visual policy approved after this diagnostic: start with opt-in, single-material
  opaque LOD using conservative occupancy OR, accepting thicker surfaces and closed
  narrow openings. Mixed-material/transparency reduction remains conditional;
  production defaults stay unchanged.
- Define versioned derivative references binding source identity/lattice, reduction
  semantics, material mapping and bounds. Existing artifacts/readers stay valid.
- Qualify current runtime source content. Original provenance and geometry IDs
  cannot validate exposed mutable storage; edits/exposure need a safe full-detail
  fallback or qualified regeneration.
- Implement coarse display/fine staging through existing streaming ownership and
  readiness, then verify fixed-view rendering, transparency and delayed handoff.

[Runtime boundary](../renderer/runtime.md#compiled-asset-lod-boundary).
No production renderer, format, cache or collision behavior changes in C3h0.


## Conservative geometry construction (C3h1)

The compiler now has a pure single-value 2× occupancy-OR builder with signed,
zero-anchored mapping, independent canonical output and explicit source/coarse
bounds. [Construction contract](../content/compiled-voxels.md#conservative-asset-lod-geometry-construction).
This is a long-term prerequisite, not runtime LOD activation. Source lattice and
level-0 authority remain unchanged; opacity qualification and versioned derivative
frames precede compiler opt-in and runtime binding. Existing schema-1 artifacts
and default compilation remain unchanged.


## Source-bound derivative frames (C3h2)

Added a separate typed C1 LOD frame with original shape identity, source lattice,
reduction semantics and structurally checked geometry/counts/bounds.
[Frame contract](../content/compiled-voxels.md#compiled-ordinary-asset-lod-frames).
Legacy frames and default compilation remain unchanged. Compiler opt-in,
opacity qualification and runtime source/readiness ownership
remain separate batches. Frame metadata alone proves neither source coverage nor
opaque eligibility.

## Versioned derivative references (C3h3)

Added explicit schema-2 headers with optional source-bound LOD references while
preserving schema-1 bytes and compiler defaults. Canonical validation bounds
references and protects shared-path and logical identity consistency.
[Contract](../content/compiled-voxels.md#compiled-ordinary-asset-headers).
Compiler opt-in, actual derivative loading, current material/source qualification
and renderer interaction/readiness ownership remain pending.

## Compiler LOD opt-in (C3h4)

Added explicit options API and CLI `-lod2`, preserving the original API/result
layout, schema-1 default bytes and default CLI output. Eligible per-part opaque
geometry emits source-bound derivatives through the existing immutable closure
and header-last publisher. [Contract](../content/compiled-voxels.md#ordinary-asset-compiler-emission).
Runtime derivative loading, current source/material qualification, authoritative
interaction versus display geometry and readiness handoff remain pending. GPU
quality and performance are not verified by this offline batch.

## Exact source validation (C3h5)

Added an unwired private verifier for exact occupancy-OR consistency against an
authenticated decoded source, using occupied-bit passes and temporary brick
indexes. [Contract](../assets/runtime-assets.md#compiled-lod-source-validation).
Derivative loading, current runtime source/material qualification and renderer
interaction/readiness ownership remain separate work.

## Scoped derivative reads (C3h6)

Added explicit typed LOD reads through existing `RuntimeContentLoader` ownership,
with shared read-only definitions, coarse-only graph charges and independent typed
keys. [Contract](../assets/runtime-assets.md#decoded-content-lifetime).
Reads do not follow references or publish geometry. Preparation source checks,
current source/material qualification and renderer handoff remain pending.

## Complete derivative verification (C3h7)

Common compiled preparation now verifies every declared derivative and its exact
authenticated source before any publication, including unused parts. Physical
aliases retain independent IO and size checks. Existing child scopes and
cancellation boundaries protect caller pins. [Contract](../assets/runtime-assets.md#compiled-ordinary-asset-preparation).
Consumers still publish level-0 geometry only. Current mutable source/material
qualification and renderer interaction/readiness ownership remain pending.

## Core render representation boundary (C3h8)

Added private optional render geometry with exact legacy defaults, signed
zero-anchored 2× matrices, independent coarse bounds and tracked invalidation.
Public scene geometry and CPU picking remain authoritative. [Contract](../renderer/runtime.md#authoritative-geometry-and-render-representations).
Recommended cold fallback is approved: hide an invalid coarse representation
until full upload is ready. GPU migration, current source/material qualification
and runtime activation remain pending; no rendering performance claim yet.


## Renderer consumer migration (C3h9)

Scene selection/BVHs and GPU allocation, uploads, normals, records and lookup now
use the optional render representation. CPU authority and legacy full readiness
remain unchanged. Equal-count retained-map swaps invalidate lookup; stale queued
uploads cannot acknowledge newer targets. Corrected point-shadow distance tests
in both culling owners. [Contract](../renderer/runtime.md#authoritative-geometry-and-render-representations).
Fine staging, current source/material qualification and activation follow.
Verification and native probe limits are recorded in the parent delivery entry.


## Full-detail upload staging (C3h10)

A valid coarse representation can stage authoritative full geometry through the
existing GPU owner and shared budget. Request generations protect cancellation
and restart; both maps participate in lookup and active retention pins. Display
and CPU authority stay unchanged until explicit ready promotion.
[Contract](../renderer/runtime.md#authoritative-geometry-and-render-representations).
Native lookup/readback verifies delayed uploads, pressure, restart and exact full
promotion. Current source/material qualification and runtime activation follow;
commands and limits remain in the parent delivery entry.


## Current geometry/material guards (C3h11)

Added unwired exact primary comparison and conservative current-material checks.
Immutable baseline ownership is established at publication, not through repeated
alias scans. [Contract](../renderer/runtime.md#authoritative-geometry-and-render-representations).
Warm equal-input scans allocate zero: median 0.372 µs/32 bricks, 7.109 µs/492,
102.239 µs/7,036 and 196.960 µs/16,384 dense bricks on Apple M4 Pro.
These are CPU guard costs, not GPU/frame timing. Owned proof publication and
runtime activation follow; boundary commands and diagnostic limits are in the
parent delivery entry.


## Owned derivative packets (C3h12a)

Workers now retain verified coarse geometry, independent immutable full/coarse
baselines and single-use coarse registration copies. Per-part membership stays
separate from full-source sharing. Pending charges include unique proof storage
and retained geometry-handle metadata. [Contract](../assets/runtime-assets.md#compiled-lod-source-validation).
AssetServer adoption and runtime activation follow. Focused/race, engine and
consumer boundary commands are recorded in the parent delivery entry.


## Ordinary derivative adoption (C3h12b)

Direct, worker and selected first-part publication now share ordinary coarse
assets and independently owned private proofs. Warm edits survive; deleted assets
rebuild from authenticated packet baselines. Deletion drains association counters
without revoking the surviving asset. Prepared membership remains per declared
part/input despite shared full geometry. [Contract](../assets/runtime-assets.md#compiled-lod-source-validation).
Per-instance intent propagation and renderer activation follow; commands and
remaining limits are recorded in the parent delivery entry.


## Explicit instance intent (C3h13a)

Prepared parts and all four selected first-part level consumers now attach private
declared full/coarse intent to actual voxel entities. Shared geometry never grants
opt-in; overrides retain original IDs. Ownership callback/flush order, full CPU
components and the first-part tuple contract stay intact.
[Contract](../assets/runtime-assets.md#compiled-lod-source-validation).
Qualified renderer activation follows; boundary commands and remaining limits
are in the parent delivery entry.


## Instance qualification (C3h13b)

A private candidate checks declared IDs against actual object-owned fine geometry,
current coarse storage, effective resolution, ordinary lattice metadata and current
used material/animation state. One sync shares exact geometry comparisons and
immutable namespace construction; membership and instance guards remain fresh.
[Contract](../renderer/runtime.md#authoritative-geometry-and-render-representations).
Renderer activation and streamed readiness integration follow.

A temporary CPU benchmark measured 1,000 ordinary objects sharing synthetic
16,384-brick fine and 2,048-brick coarse maps on Apple M4 Pro, Go 1.25.4,
darwin/arm64. Three 200 ms warm runs of the actual candidate gave median
0.379 ms, 752 allocated bytes and six allocations per sync. A fresh context per
placement gave 264.499 ms, 752,000 bytes and 6,000 allocations. This isolates
qualification sharing; it measures neither scene sync, GPU upload nor FPS.
Harness: `/tmp/gekko-c3h13b-measurement_test.go`; final evidence:
`/tmp/gekko-c3h13b-measurement-final.log`.
Boundary commands and remaining limits are recorded in the parent delivery entry.


## Qualified renderer activation (C3h13c)

Declared ordinary instances now select coarse display without replacing fine CPU
geometry. Existing voxel bands request coarse hold; fine demand stages uploads and
promotes only after exact prior readiness is revalidated on the next sync. Current
eligibility loss uses the approved cold full wait. Opt-in streamed tickets qualify
the selected target; legacy full tickets and terminal latching remain intact.
[Contract](../renderer/runtime.md#authoritative-geometry-and-render-representations).

Native hidden GLFW/WebGPU readback passed signed geometry with a narrow opening:
coarse-only startup left fine unallocated and dirty queues untouched; paused fine
staging preserved exact coarse depth/normal bytes. One sector/64 bricks per frame
completed fine uploads in four frames (3,168 bytes each), followed by next-sync
promotion. Promoted depth/normal bytes exactly matched an independent cold full
render. Tracked edits retained initialized full visibility; alpha loss during
coarse display waited for cold full readiness. Root inspected coarse/fine depth
and fine normals: approved thickening/closed opening, then restored fine opening.
The terminal coarse ticket remained latched.

Temporary harness and bridge: `/tmp/gekko-c3h13c-gpu-probe.go`,
`/tmp/gekko-c3h13c-probe-bridge.go`; log:
`/tmp/gekko-c3h13c-gpu-probe-native.log`; readbacks:
`/tmp/gekko-c3h13c-gpu-probe-output`. Helpers were removed from the engine before
boundary checks. This verifies handoff/G-buffer parity, not FPS, independent
transparent-overlay appearance or shadow images. Parent delivery records commands.

## Pure authored primitive extraction (C3g1)

Seven generators now expose privately owned CPU models without asset registration.
Public constructors preserve exact output, warm identity and declared bounds;
[the creation contract](../assets/runtime-assets.md#voxel-models-and-palettes)
owns those invariants. Fourteen frozen baseline models cover fractional and zero
inputs, including capsule axis and ramp nil geometry. Focused tests, full engine
tests and five consumer builds passed. This is an offline adapter prerequisite;
no loading or rendering speed gain is claimed.

## Canonical model frames (C3g2)

A separate typed C1 model frame binds declared dimensions to canonical primary
geometry without changing existing shape/header formats. The
[model-frame contract](../content/compiled-voxels.md#compiled-ordinary-model-frames)
keeps raw history and runtime collapse outside this compact boundary. Focused and
race content tests, full engine tests and five consumer builds passed. This
format prerequisite has no runtime performance claim; compiler/header/adoption
batches remain next.

## Pure source palette construction (C3g3)

Authored material, procedural and VOX palette data can now be built without asset
registration. Historical palette JSON, source/asset cache domains, borrowed VOX
material maps, raw used-slot surface facts and nil/error precedence are preserved.
[The runtime asset contract](../assets/runtime-assets.md#voxel-models-and-palettes)
owns these rules. Focused/race tests, full engine tests and five consumer builds
passed. Owned offline adapters remain next; no direct loading speed gain claimed.

## Owned source-model preparation (C3g4)

Private CPU adapters cover seven primitives, VOX model selection and scene-node
selection through shared generation, scale and primary packing. Owned palettes
preserve original surface channels. The
[compiler preparation contract](../content/compiled-voxels.md#offline-source-model-preparation)
rejects nondeterministic top-vote downscales and malformed scene graphs without
changing legacy APIs. Focused/race, full engine and five consumer builds passed.
Shipping headers and runtime adoption remain next; no loading speed gain claimed.

## Model asset headers (C3g5)

Separate typed headers bind model frames and static palette tables while retaining
source kinds and legacy inline/LOD references. Existing header APIs/bytes and
public source-file validation remain unchanged. The
[model-header contract](../content/compiled-voxels.md#compiled-ordinary-model-asset-headers)
owns closure, identity and no-authoring-IO rules. Focused/race content checks,
full engine tests and five consumer builds passed. Decoder cache integration,
shipping compiler emission and verified runtime adoption remain next.

## Scoped model reads (C3g6)

Two typed model/header leaves use the existing decoded owner, fixed borrowed codec,
scopes and separate cache kinds. Warm entries preserve existing path/kind lifetime;
no dependency following or new cache owner. [Decoded ownership](../assets/runtime-assets.md#decoded-content-lifetime)
owns the contract. Focused/race, full engine tests and five consumer builds passed.
C3g5 is `5d61df0`; verified runtime selection/adoption and shipping compilation
remain next. No loading speed gain claimed yet.

## Verified model input (C3g7)

Explicit `.gkmodelassetc` selection authenticates the complete mixed closure without
original VOX IO. Session metadata/palettes own their nested storage; frames borrow
child scope leases. Existing `.gkassetc`, JSON and selected-inline E2 contracts
remain unchanged. [The header contract](../content/compiled-voxels.md#compiled-ordinary-model-asset-headers)
owns selection and verification. Focused/race, full engine and five consumer builds
passed. C3g6 is `33163a1`; owned packets/adoption and shipping emission remain next.

## Owned model packets and adoption (C3g8)

Models now use existing owned CPU packets, palette handles, pending charges and
ordinary AssetServer lifetime. Declared dimensions/bounds survive cold transfer;
warm mutable owners and inline/LOD behavior stay preserved. New model raster
provenance does not qualify E2. [Runtime ownership](../assets/runtime-assets.md#compiled-ordinary-asset-preparation)
records the contract. Redundant public hierarchy validation is skipped only for
already validated model headers, removing authoring source stats. Focused/race,
full engine and five consumer builds passed. C3g7 is `55d2303`; public consumers
and shipping emission remain next. No loading speed claim yet.

## Model consumer integration (C3g9)

Direct loading, level/NPC routes, first-part consumers and streamed preparation/
commit now select model headers through the shared strict classifier. Models keep
center pivots, full geometry, declared bounds and baked palettes; mixed inline
custom pivots/LOD intent remain unchanged. [Runtime consumers](../assets/runtime-assets.md#compiled-ordinary-asset-preparation)
own the contract. Focused/race, full engine and five consumer builds passed,
including packet-only commits after file removal and Stop cleanup. C3g8 is
`2a6fe77`; shipping compilation and measured source-loading gains remain next.

## Shipping model emission (C3g10)

A separate model compiler emits mixed typed closures through shared preflight and
header-last publication. Old public compiler APIs/results and physical bytes stay
fixed. Owned source adapters bake palettes and dimensions offline; original VOX
files are alias-protected provenance, not shipping dependencies.
[Compiler emission](../content/compiled-voxels.md#compiled-model-asset-emission)
owns these rules. Focused/race, full engine and five consumer builds passed, including
legacy byte goldens, source-free roundtrip, profile isolation and failure durability.
C3g9 is `6f0dbe3`; CLI selection and loading measurements remain next.

## Model compiler CLI (C3g11)

The existing CLI now selects `.gkmodelassetc` explicitly and adds model file counts.
Old `.gkassetc` stdout/API and flags remain preserved; `-lod2` stays inline-only.
[CLI usage](../content/compiled-voxels.md#compiled-model-asset-emission) owns the
contract. `env GOCACHE=/tmp/gekko3d-gocache go test ./cmd/assetcompile -count=1`
and full engine `go test ./...` passed. No consumer API/runtime change or new GPU
check. C3g10 is `5fdf3e1`; representative source-loading measurements remain next.
