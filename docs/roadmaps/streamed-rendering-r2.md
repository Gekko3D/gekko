# R2: Shadow dependency invalidation

## R2a ownership decision

The GPU manager owns point/spot shadow dependencies. R2a is a long-term step
within the existing live-input renderer architecture. The authoritative contract
is [local shadow cache dependencies](../renderer/runtime.md#local-shadow-cache-dependencies).
Directional invalidation, cadence, tier budgets, point-face rotation, shader
layouts and ECS publication retain their existing ownership.

Exact scalar snapshots fit the existing renderer's compatibility with public
in-place mutations. Comparing current selected casters handles removals, movement
and camera-dependent caster-group replacement without introducing producer event
requirements. R2a used one generation per point light to keep six-face publication coherent;
R2i narrows acknowledgement to independent current faces.
Allocation-owned upload epochs follow existing geometry/material lifetimes;
shadow-relevant opacity snapshots avoid invalidation on identical palette refresh.
Stable volume membership reuse limits repeated intersection work while retaining
live scalar comparisons.

## Alternatives and remaining work

Global invalidation is conservative but repeats unrelated GPU work. A spatial
change index or dirty-region queue could reduce CPU scans further, but requires
broader producer/publication and retention decisions. Exact snapshots are the
chosen boundary; no new ECS extraction architecture or total-memory ceiling is
introduced. Local snapshots still add CPU and retained-memory cost.

R2i adds face-specific point dependencies; clipmap scrolling and dirty texels
remain separate steps. R2e/R2f remove periodic rebuilds after dependency-driven
reuse is established; R2g removes camera heuristics from ordinary scheduling.
Automated scheduling checks alone do not prove visual parity or net frame gains.
Native shadow readback and a CPU diagnostic are retained in the
[delivery record](streamed-rendering-content-optimization.md#r2a-local-shadow-dependency-invalidation).

## R2b ownership decision

Directional dependencies extend the same GPU-manager-owned snapshots per cascade.
The projection, inverse projection, cascade parameters, effective resolution and
emitter/owner metadata define each layer's inputs. Dependencies use current
selected casters and the same allocation-owned upload epochs as local shadows.

Membership must follow the shader's actual orthographic rays. Those rays begin
at the inverse projection's near plane and continue downstream without a far
plane cap. Finite directional selection volumes cannot safely bound dependencies
when the selected scene also contains downstream casters. Unsupported or invalid
projection/bounds inputs must retain conservative membership. Periodic cadence,
forced camera refresh, scene caster selection and cached map/transform publication
keep their existing owners. No shader traversal or ECS changes are required.


## R2d ownership decision

Shadow dispatch owns an immutable record snapshot for each call. Queue writes
cannot publish distinct bucket contents to one storage buffer before a shared
submission: earlier encoded passes can observe later writes. Encoder-ordered
copies publish each bucket immediately before its compute pass. This preserves
resolution-specific work sizes, the existing 24-byte records and shader bindings.

An immutable packet per call also supports multiple dispatch calls before one
submit without relying on frame counters or caller slice lifetime. Permanent
per-resolution buffers would still need a policy for same-resolution repeated
calls. Shader base-index or dynamic-offset designs add a broader layout contract;
ordered copies fit the existing resource manager. Native parity is verified by the R2d diagnostic; recording cost remains comparable
in the measured fixture. Transient packet allocation is a tradeoff; GPU timing and
full gameplay visual checks remain unverified.


## R2e ownership decision

The GPU manager schedules local maps from dependency validity, not age. Once all
current point/spot inputs are acknowledged, periodic work cannot improve the map.
Local tier budgets and point-face rotation still bound dirty and initial work;
only recorded updates acknowledge dependencies. Unknown uploads retain the
conservative fallback. Directional cadence and forced camera refresh stay intact.

Keeping periodic local refresh spends GPU work after the same inputs have already
been rendered. Removing directional cadence together would broaden the change;
spatial change queues require a separate publication contract. Dependency-driven
local reuse is the next step within the existing ownership boundary. Continuous
dirty-light priority is unchanged; this step does not redesign fairness or caster
selection. Scheduler tests and native cached-map parity validate the change; full
application frame timing requires separate measurement.


## R2f ownership decision

Directional maps follow the same dependency-driven validity contract as local
maps. Age alone does not change a cascade's projection or caster inputs, so valid
cascades remain cached. Each cascade acknowledges its generation independently.
Explicit forced refresh and the app's camera-motion force path remain available;
cached transforms publish together with their scheduled maps.

Keeping periodic rebuilds repeats GPU work with identical inputs. Removing the
camera force path simultaneously would broaden verification to moving-camera
behavior, so it remains separate. This step changes only GPU-manager scheduling;
projection generation, caster selection, shader traversal and publication remain
with their current owners. CPU scheduling and native fresh-reference parity
validate idle reuse and dependent changes; frame-time gains require measurement.


## R2g ownership decision

App orchestration requests ordinary shadow scheduling from dependency validity.
Camera-history availability and motion diagnostics do not force cascade rebuilds:
current projection and selected caster inputs already define per-cascade validity.
The GPU manager retains its explicit forced-refresh API. Initial maps remain dirty
until recorded, and cached transforms publish with their scheduled maps.

Keeping both camera heuristics and dependency invalidation duplicates authority.
Removing only the heuristic fits the current live-input architecture; projection
stabilization is a separate change with visual consequences. Actual moving-camera
projection changes still rebuild cascades. This step touches app shadow recording,
its behavioral tests and native Render verification, with no shader or projection
changes. It removes redundant work when camera metadata/history changes without
changing map inputs; it does not promise broad moving-camera cache reuse.


## R2h ownership decision

Directional projection generation owns world-anchored lateral texel snapping.
The previous centered view projects its own center to approximately zero, so
rounding that position does not stabilize the world grid. A fixed light basis
projects the world center before XY quantization. Camera-relative frustum fitting
keeps extents independent of lateral translation; light-space depth remains live.
The returned culling view must match the generated projection.

Snapping needs conservative coverage: for resolutions at least two, expanding the
fit by one base texel covers the maximum half-final-texel center shift. A single
texel map uses an unsnapped conservative fit; zero resolution retains the existing
default. Exact dependency comparisons remain unchanged. Intrinsics, orientation,
sun direction, depth changes, grid crossings and selected caster changes still
invalidate their actual inputs. Cached maps and transforms remain paired.

Keeping the centered snap repeats directional GPU work during sub-texel movement.
Ignoring small matrix differences would hide real input changes, so approximate
cache comparisons are rejected. World anchoring repairs the intended projection
contract with a bounded grid/coverage change; it is not a scrolling clipmap or
partial-map update. Geometry coverage tests and native Render readbacks validate
stable cells and crossings. Full gameplay appearance and GPU/FPS gains remain
separate measurements.


## R2i ownership decision

The GPU manager owns independent point-face dependency generations. Point rays
use six fixed world-axis cones, so a caster change need only invalidate faces
whose rays can intersect its current or previous selected bounds. Conservative
AABB plane separation includes seams, origin contact and numerical uncertainty.
Unknown bounds or unsupported light/face inputs retain membership. Dependencies
cover the full downstream cone: shader traversal is not capped by light range,
and casters selected for another light can still appear in a point map. Source
radius does not narrow membership. Scene caster selection retains its owner.

Face membership includes the union of world cones and cones over float32
render-relative bounds/light positions published to the GPU. Rebasing can move
the published footprint through rounding, so origin changes recompute membership.
They do not themselves change generations: identical selected inputs and members
retain acknowledgements. The app rebases to camera position every update; forcing
all point maps on origin changes would defeat existing camera-motion reuse.
Invalid rebased inputs retain membership. This preserves world-input cache
semantics, not bitwise fresh-map parity across arbitrary coordinate rebasing.

Each face snapshots the existing live scalar caster/upload inputs and its light,
face/layer assignment and effective resolution. Membership indices reuse stable
bounds; removed layers release references. Shared caster capture remains once per
preparation. Only recorded updates acknowledge a face. All six faces must be
current before GPU light serialization enables the point shadow, but unaffected
faces retain acknowledgements across local edits and partial refresh. Global light
or unattributed upload changes still invalidate every face.

Scheduling filters valid faces before applying existing tier face budgets and
rotation. Per-tier light budgets and nearest-light priority remain unchanged.
Whole-light generations repeat up to six maps for isolated edits; a spatial
change index needs broader producer contracts. Independent exact snapshots fit
the current live-input architecture without shader, GPU-layout or ECS changes.
CPU scans and retained per-face snapshots add cost; native scheduling/readback
parity proves avoided map work, while gameplay frame-time gains need measurement.


## R2j ownership decision

The GPU manager incrementally refreshes membership for unchanged shadow volumes.
One preparation still captures all live selected caster scalar inputs. When the
ordered identities and count remain stable, changed world-bound indices form a
delta from the preceding membership revision. An owner at that revision rechecks
only those indices, preserving sorted membership and live comparisons of every
retained caster. Removal from an old volume and entry into a new one are both
observed; dependency acknowledgement remains owned by recorded map updates.

Cold owners, structural selection changes, changed light/projection keys, point
origin changes and missing delta history retain full scans. The delta applies
only to its immediate source revision; it is not a retained producer event queue.
Invalid bounds and unsupported projections retain existing conservative behavior.
The cumulative `ShadowMembershipIntersectionCount` reports caster-volume predicate
evaluations, including full fallbacks, so avoided work can be measured directly.
It does not count the full live scalar capture or retained-member comparisons.

Full rescans repeat intersection math for untouched casters. A spatial index or
producer dirty queue needs broader publication and retention contracts. Bounds
deltas are a long-term extension of current manager-owned snapshots and require
no producer notifications, shader/layout changes or scheduling policy changes.
CPU work measurements and native shadow parity verify the step; general scene
scans, moving-volume work and net gameplay frame-time gains remain separate.


## R2k ownership decision

The GPU manager retains exact caster scalar snapshots once in its shared selected
inventory. Each unchanged snapshot carries a manager-owned change token. Shadow
volume owners retain compact tokens alongside their sorted member indices instead
of repeating full snapshots. Every changed scalar snapshot receives a new token,
including identity and uploaded-content changes; tokens cannot identify different
snapshots after retirement or reset. Unchanged snapshots retain their tokens across
selection reorder and source-index shifts, including duplicate placements. Live
public inputs remain captured each
preparation, and unknown upload epochs remain a separate conservative invalidator.
Membership, volume keys, generations and recorded-update acknowledgements preserve
their current contracts. Structural selection changes use reusable temporary shared
storage and identity remapping; cleared old snapshots release source references.
Stable selection needs no identity-map lookup or full-key copy.

`ShadowDependencyMemberStorageBytes()` reports retained backing-capacity bytes for
volume member indices and dependency records across all shadow families. It excludes
shared scalar inventory, membership scratch, owner headers, source content and GPU
storage. The compact representation bounds freshly warmed owner storage without
turning existing cache budgets into a process-memory ceiling.

Repeated owner snapshots duplicate both scalar comparisons and retained pointers.
Producer dirty notifications could remove live capture but need broader publication
contracts. Compact manager-owned tokens extend the existing exact-input architecture
without changing ECS, shader layout, selection or scheduling policy. CPU timing and
storage measurements plus native map parity verify this step; full scalar capture,
moving-volume intersections and gameplay frame-time gains remain separate.


## R2l ownership decision

The GPU manager shares point-light caster classification within one dependency
preparation. A conservative six-face mask normalizes each selected caster's world
and float32 render-relative bounds once per point light when membership work is
needed. Only faces requiring membership work enter the mask; packed normalization
only checks bits not already covered by the world footprint. Individual face
owners consume mask bits while preserving their sorted
membership, exact keys, scalar tokens and recorded-update acknowledgements.
Masks include the union of world and rebased footprints, existing numerical guards,
seams and unbounded downstream cones. Invalid inputs retain conservative coverage.

Preparation-local, index-addressed scratch cannot survive a light or preparation
boundary. Duplicate selected occurrences remain distinct indices. Owners may be
grouped by point light for preparation without changing scheduling priority or
face budgets. Idle/scalar-only preparation does not classify casters. The cumulative
`ShadowPointMembershipClassificationCount` counts actual caster/light footprint
classifications; `ShadowMembershipIntersectionCount` still counts each consuming
caster-volume membership predicate.

Repeated per-face normalization adds CPU work when bounds, point inputs or camera
rebasing require membership scans. Cross-frame footprint caching needs additional
retention/invalidation contracts; shared preparation-local masks extend current
manager-owned snapshots without changing ECS, shader layout, selection or cache
policy. CPU work/timing and native map parity verify this step. Full live scalar
capture, moving-volume predicate counts and gameplay FPS remain separate limits.
