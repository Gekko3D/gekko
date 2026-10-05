# R2: Local shadow dependency invalidation

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

Directional volume invalidation, face-specific point dependencies, clipmap
scrolling, dirty texels and removal of periodic cadence remain separate steps.
Automated scheduling checks alone do not prove visual parity or net frame gains.
Native shadow readback and a CPU diagnostic are retained in the
[delivery record](streamed-rendering-content-optimization.md#r2a-local-shadow-dependency-invalidation).
