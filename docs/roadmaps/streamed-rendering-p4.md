# P4: Immutable GPU material sharing

Status: scoped proposal; implementation boundary awaiting alignment.

## Current ownership

`core.VoxelObject.MaterialTable` is a public mutable slice. Bridge material caches
share CPU slices, but GPU material allocation, readiness and upload acknowledgement
are per object. Pointer/length acknowledgement does not detect arbitrary in-place
row mutations. Animated palette assets and nested asset aliases are mutable.

The renderer's effective-palette fingerprint intentionally excludes gameplay
surface metadata. It is not a complete P4 identity. The full palette cache encoding
in `asset_vox_model.go` includes surface kinds/tags, animations/bindings, overrides,
material properties and provenance. Existing asset IDs and retained encoding strings
do not certify immutability after publication. Encoding failure must never become
an empty-key sharing collision.

## Proposed integrated first slice

Implement static immutable material sharing across the bridge and GPU manager.
The bridge owns a sealed copy of render rows and an exact opaque semantic identity;
the GPU manager interns immutable blocks and owns object attachments separately.
Uncertified public tables, animations and overrides keep private allocations.
Do not infer immutability from pointer equality, asset IDs or rendering fingerprints.

Preserve the existing full palette identity initially, including provenance, to
avoid merging distinct gameplay semantics. Capture identity once for an owned
snapshot; validate live changes before reusing its proof. Unsupported identity
encoding falls back to private ownership. A live edit or binding replacement must
switch the affected attachment without modifying another object's block.

Admission reserves by distinct block rather than object count. Upload each shared
block once per material-buffer generation. Reference ownership, final-reference
release, queued writes, growth migration and readiness must agree before publishing
an object offset. Shadow dependencies remain object-scoped when attachments change.
Keep 256-entry local material addressing and existing shader layouts.

Primary owners: bridge material extraction and GPU resource management. Affected
files include `mod_voxelrt_client_materials.go`, `mod_voxelrt_client_systems.go`,
`voxelrt/rt/core` material bindings, GPU manager allocation/admission, scheduling,
execution, readiness, scene records, shadow dependencies and growth migration.
Consumers include the editor and voxel gameplay/demo apps.

## Alternative boundary

A smaller P4a can first build and consume a private sealed material snapshot in the
bridge cache, retaining exact full semantics and static eligibility. This is a
long-term ownership prerequisite, but provides no shared GPU-memory reduction yet.
Avoid a standalone unused public immutable API. Integrated sharing is preferable
when the first delivery should demonstrate fewer material blocks and uploads;
the smaller foundation is preferable when ownership changes must land separately.

## Verification

Use the authorized Sol 6.1 test-to-RED, root test review, implementation-to-GREEN,
root code review and commit workflow after selecting the first slice. Protect
observable sharing and isolation, full semantic distinctions, mutation/replacement,
final-owner release, deferred uploads, buffer growth and shadow acknowledgement.

Run `env GOCACHE=/tmp/gekko3d-gocache go test .` and
`env GOCACHE=/tmp/gekko3d-gocache go test ./voxelrt/rt/gpu ./voxelrt/rt/core ./voxelrt/rt/app`.
Build affected consumers from their module directories. Integrated sharing also
requires native GPU readback of material offsets/content through growth and
appearance checks for transparency, emission, animation and instance recoloring.
CPU allocation counts alone do not establish native parity or frame-time gains.
