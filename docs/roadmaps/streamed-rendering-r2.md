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

Face-specific point dependencies, clipmap scrolling, dirty texels and removal of
directional periodic cadence remain separate steps. R2e removes periodic local
refresh after dependency-driven reuse is established.
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
