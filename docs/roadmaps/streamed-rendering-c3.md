# C3: Compiled ordinary assets

Status: approved direction; C3a shape frames, C3b headers and C3c offline
compiler, C3d1 decoded-cache integration, C3d2 canonical-base adoption and C3d3
direct runtime preparation/spawning, C3d4a ordinary level placements and C3e
compiler CLI, C3f1 private shared adoption, C3f2 CPU packets and C3f3 streamed
worker integration, C3d4b NPC adaptation, C3d4c first-part level consumers and
C3f4a private palette adoption and C3f4b worker palette integration and C3f4c publication accounting complete. C3h0 asset LOD diagnostics complete; reduction/material policy remains gated. Compiler source-kind adapters and production asset LOD remain separate work.
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

- Align the visual policy: center-nearest loses coverage; conservative occupancy
  OR can thicken surfaces and close narrow openings. Material/opacity reduction
  and asset eligibility need explicit acceptance. Keep production defaults unchanged.
- Define versioned derivative references binding source identity/lattice, reduction
  semantics, material mapping and bounds. Existing artifacts/readers stay valid.
- Qualify current runtime source content. Original provenance and geometry IDs
  cannot validate exposed mutable storage; edits/exposure need a safe full-detail
  fallback or qualified regeneration.
- Implement coarse display/fine staging through existing streaming ownership and
  readiness, then verify fixed-view rendering, transparency and delayed handoff.

[Runtime boundary](../renderer/runtime.md#compiled-asset-lod-boundary).
No production renderer, format, cache or collision behavior changes in C3h0.
