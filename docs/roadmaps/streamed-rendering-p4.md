# P4: Immutable GPU material sharing

Status: integrated implementation delivered in this batch.

The ECS bridge owns static semantic snapshots and sealed render rows; the GPU
manager owns shared physical blocks and independent object attachments. This
integrated boundary replaces the proposed bridge-only prerequisite because it
establishes both ownership and measurable GPU storage/upload savings together.
The public mutable material slice remains independently editable per instance.

The full palette identity includes gameplay surface metadata and provenance.
The existing narrower rendering fingerprint is retained for CPU row construction;
it does not certify GPU sharing. Dynamic or unsupported inputs remain private.
Admission, generation migration, queued writes, retirement, readiness and shader
publication follow the [canonical runtime contract](../renderer/runtime.md#immutable-gpu-material-blocks).
Shader layouts and 256-entry local addressing are unchanged.

Primary owners: ECS material extraction and GPU resource management. Consumers:
editor, action game, space game, spacesim and voxel example. R2 follow-ups remain
recorded in the [main roadmap](streamed-rendering-content-optimization.md#r2-narrow-shadow-invalidation).

## Results and verification

The byte-backed 1,000-object workload measures one 16,384-byte block/write for
identical certified tables, versus 16,384,000 allocated/uploaded bytes and 1,000
writes for the same private-table workload: 99.9% fewer material bytes and writes.
These measure material resources, not total renderer memory or frame time.

Verification commands, native diagnostic and consumer builds are recorded in the
[main delivery entry](streamed-rendering-content-optimization.md#p4-immutable-gpu-material-sharing).
Native checks exercise GPU row readback, shared/private albedo, emission and
transparency parity, dynamic private rows, paused publication, instance edit
isolation, growth and surviving-owner release. Existing native shadow diagnostics
protect cached/fresh-map parity.

Physical buffers retain high-water capacity after release. Independent instance
rows, semantic snapshots and manager packets consume CPU memory under separate
owners. Gameplay FPS and a full interactive editor/game walkthrough are not
measured by these checks.
