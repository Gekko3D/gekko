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
requirements. One generation per point light keeps six-face publication coherent.
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

Face-specific point dependencies, clipmap scrolling and dirty texels remain
separate steps. R2e/R2f remove periodic rebuilds after dependency-driven reuse is
established; R2g removes camera heuristics from ordinary scheduling.
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
