# C3: Compiled ordinary assets

Status: approved direction; C3a shape frames, C3b headers and C3c offline
compiler, C3d1 decoded-cache integration, C3d2 canonical-base adoption and C3d3
direct runtime preparation/spawning and C3d4a ordinary level placements complete.
Ordinary worker preparation remains separate; special level consumers still use JSON.
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
