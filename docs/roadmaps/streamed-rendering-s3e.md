# S3e: Explicit component publication revisions

Date: 2026-10-02. Status: implemented, both adversarial reviews closed, verified.
Parent: [optimization roadmap](streamed-rendering-content-optimization.md), S3.
Prerequisites: S3c/S3d shared storage ownership and live mutation compatibility.

## Scope and confidence

Primary owner: committed ECS component publication. Consumers: future
incremental extraction and other main-thread component consumers. Confidence:
High after tracing outer mutations, recycled rows, command sanitation/flush,
type registration and shared wrappers. Independent Sol 6.1 review confirmed
this permanent prerequisite. SME alignment required: No.
`base/skills-manifest.md` is absent; workflow and owning runtime/ECS docs read.

Known: structural revisions invalidate membership/rows, not component values.
Hierarchy, animation, brushes and external consumers still write public fields.
The roadmap requires versions/events at owning mutation boundaries before
relying on incremental extraction. Complete producer migration is a separate
cross-module task. Asset getters expose aliased mutable maps/slices, so asset
publication stamps alone would not establish immutable content.

Add aggregate publication revisions per component type, plus an explicit mark
for direct writes. Keep existing hierarchy/bridge live reads. This slice removes
no extraction work and establishes no performance gain; it supplies the shared
owner and public notification contract needed by later producer migration.
It does not supply an entity worklist or automatically detect field changes.

## Architecture and invariants

1. Expose main-thread `ComponentRevision(reflect.Type) uint64` and
   `MarkComponentChanged(EntityId, reflect.Type) bool` on both `Ecs` and
   `Commands`. Preserve all existing method signatures and component layouts.
   Revisions belong to shared `ecsStorage`, not copied wrappers or row storage.
2. A revision is aggregate for the entire component type in one storage owner,
   not for an entity. Zero means no publication yet. Preserve a type's last
   revision when its final entity/component disappears, so removals remain
   observable and re-admission cannot reset an earlier version. Independent
   owners have independent sequences. Compare revisions within the same owner.
3. Accept a struct type or one pointer to a struct as the same canonical type.
   Nil types, non-struct types, and deeper pointers are invalid. Reads return
   zero and marks return false for nil/zero owners or invalid types. Unknown
   reads/marks must not register component IDs or allocate publication history.
   Reading revisions never builds queries, refreshes hierarchy or flushes.
4. Each type owns a publication sequence. Advance it by one after a successful
   outer committed mutation affecting that type. Consumers normally compare
   sequence equality; it is not a semantic count of changed values.
   Insertion publishes every present type; AddComponents publishes supplied
   types, including equal-value/same-archetype replacement; RemoveComponents
   publishes only types actually present and removed; RemoveEntity publishes
   all types formerly present. Deduplicate duplicate supplied types.
5. Copying unchanged components during migration publishes no value change for
   those types. Removing an absent type preserves existing conservative row
   relocation/structural revision, but publishes no component change. Internal
   recycling during migration must not announce removal. Publication follows
   successful committed index/group synchronization, at the existing flush.
6. Enqueueing, empty flushes, reads/query type registration, sanitized nil/empty
   component commands, missing-entity commands and direct field writes leave
   these revisions unchanged. Empty entities have no component publication.
   Keep existing flush order, including removals before additions.
7. MarkComponentChanged publishes immediately for a currently committed live
   entity that has the requested type. Return true only then. It does not write
   a value, compare values, migrate rows, enqueue work, flush or advance the
   structural revision. An owner may explicitly publish equal values. An
   enqueued new entity/component cannot be marked until committed; a pending
   removal stays markable until its current component is removed at flush.
8. Preserve legacy public-pointer writes: they remain visible through current
   live hierarchy/bridge behavior but are invisible to this API unless their
   owner explicitly marks them. No renderer, physics, animation or editor may
   treat the new stamp as a complete dirty contract yet. Producer migration and
   a bounded entity change journal, if needed, require subsequent designs.
9. Retain only one scalar revision per published component type. Retain no
   component values, entity tombstones, component pointers, queries or archetype
   references for this owner. Lifetime follows ECS storage. This is not a
   concurrent API, general memory budget or semantic value-equality counter.

Alternatives: mandatory setters require a broad public mutation migration;
per-entity history or a change journal adds lifetime/overflow/consumer-cursor
contracts before they are needed. Aggregate type revisions fit the committed
owner now and avoid entity tombstones. Asset/bridge caching remains dependent
on complete mutable-input ownership rather than pointer identity alone.

## Functionality tests and corner cases

The continuing user-authorized Sol 6.1 tests-first, adversarial review,
implementation, review and commit workflow authorizes focused new tests only.
Protect public revision/mark behavior and component values; never private maps,
type IDs, row layouts or helper call order. Existing tests remain unchanged.

- Nil/zero/invalid type reads and marks: safe, zero/false; canonical value and
  pointer types agree. Unseen types and query registration do not publish.
- Flush visibility and attribution: insertion, same archetype insertion,
  addition/replacement (including equal value), removal and final entity
  removal update only actual affected types; unrelated types retain revisions.
- Migration: copied values keep their revision; removing an absent type has
  structural churn but no value publication; duplicate arguments publish once
  per type while preserving existing final component values.
- Explicit marks: direct writes alone retain revision; valid mark immediately
  publishes only the requested type while preserving values/membership. Marks
  without a value change are valid publications. Missing entity/component and
  not-yet-committed additions return false; pending removals remain markable.
- Multiple buffered commands: current removal/addition flush order and ignored
  commands do not lose a legitimate publication or publish copied types.
- Lifetime: row growth/reuse, removal/re-admission and another entity of the
  same type retain a monotonic aggregate stamp; independent owners stay isolated
  and copied wrappers observe/mark the shared owner.
- Compatibility: a direct transform/model field write without a mark still
  reaches the actual existing voxel bridge and leaves its publication revision
  unchanged. Existing S3c/S3d/hierarchy/animation/brush tests remain authoritative.

Missing declarations may produce compile-failure RED. Root and independent
Sol 6.1 review tests before implementation, then review production after GREEN.

## Files and verification

Production: `ecs.go`, `commands.go`, and a focused private publication owner
file if useful. Canonical docs: engine ECS/runtime, parent roadmap and this
record. No component serialization, scheduling, renderer/shader, asset getter
or producer changes. Preserve user work and stage only this slice.

Run focused S3e RED/GREEN; existing ECS/query/S3c/S3d/hierarchy/bridge/animation/
brush checks; engine root tests and focused race; affected ActionGame and editor
production compilation. This API-only change alters no drawing/input behavior,
so no new windowed smoke is needed. Previous unrelated sample/editor test
failures remain documented in S3d/S3c; do not broaden into their repair.

## Execution record

Root narrowed the next step after S3d. Independent Sol 6.1 review recommended
aggregate storage-owned component publication revisions rather than premature
per-entity history, asset immutability assumptions or extraction cache work.
Sol 6.1 added six public ECS contract tests and one real bridge compatibility
test in two new files. The focused command stopped RED with missing API
declarations; no production stubs or existing tests changed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3e' -count=1
```

Root reviewed all test bodies. Independent Sol 6.1 adversarial review found no
actionable gaps or architectural blockers. Tests were frozen before production
implementation. Unknown lookup registration and source membership capture
remain production review gates because public tests cannot expose those
internals without coupling to implementation details.

Before authoring tests, the existing focused ECS/query/S3c/S3d/hierarchy/bridge/
animation/brush baseline passed (0.694s).

Sol 6.1 implemented `ecs_component_revision.go`, ECS outer publication hooks and
Commands forwarding, then stopped GREEN. Root and independent Sol 6.1 reviewed
the production code from the consumer contract. Both found no actionable
blockers: reads/marks use nonregistering lookups; marks validate live entity,
row and component membership; removal source types are captured before recycle;
publication follows completed outer index/group synchronization. Internal
recycling and copied values publish nothing. The shared owner retains only a
scalar per published type. Both new test files retained their frozen hashes;
existing tests remained unchanged.

Exact final commands, run from `gekko` unless a consumer path is listed:

```sh
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^TestS3e' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -run '^Test(Ecs_|EcsReflect_|Query_|S3c|S3d|TransformHierarchy|ReparentPreservingWorldTransform|VoxelRtSystem|StreamedVoxel|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
env GOCACHE=/tmp/gekko3d-gocache go test . -count=1
env GOCACHE=/tmp/gekko3d-gocache go test -race . -run '^Test(Ecs_|EcsReflect_|Query_|S3c|S3d|S3e|TransformHierarchy|ReparentPreservingWorldTransform|VoxelRtSystem|StreamedVoxel|AuthoredAssetAnimation|NPCAnimation|MovingBrush)' -count=1
```

All passed: S3e 1.032s; prior focused regression 1.176s; root package 10.270s;
focused race 2.147s. The race linker emitted the previously observed macOS
`LC_DYSYMTAB` warning and completed successfully.

Consumer verification:

- `actiongame`: `env GOCACHE=/tmp/gekko3d-gocache go test ./... -run '^$'`
  passed compilation of all packages without running gameplay tests.
- `gekko-editor`: `env GOCACHE=/tmp/gekko3d-gocache go build ./...` passed
  (exit 0), with the previously observed denied module stat-cache write warning.

`gofmt -l` for all five changed/new Go files and `git diff --check` were clean.
No windowed smoke or engine-wide sweep was required for this API-only slice.
The earlier unrelated editor/sample test failures were not rerun; their
baselines remain in S3c/S3d. Consumer builds and unit tests establish compilation
and the publication contract, not rendered parity or a performance gain.

Canonical ECS/runtime docs and the parent roadmap describe the API and limits.
S3 remains partial. The next prerequisite is complete notifications at mutable
producer boundaries; aggregate revisions cannot yet justify skipping live
hierarchy or renderer extraction. Entity worklists and mutable asset ownership
still need their own designs before dependent caching.
