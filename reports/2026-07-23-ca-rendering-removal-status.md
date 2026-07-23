# CA Rendering Removal

- Goal: delete CA rendering and public CA engine surface; compatibility not preserved.
- Owner: renderer. Consumers: engine scene/ECS API, voxel demo, fire demo, space game.
- Confidence: High. CA is isolated as optional feature with dedicated bridge, graph nodes, GPU resources, and shaders.
- Invariant: remaining render graph and resolve bind group stay CPU/WGSL-aligned.
- Chosen option: hard deletion. Feature-disable shim rejected by request.
- Automated verification: engine `go test ./...`, voxel demo, space game, and SpaceSim all pass.
- Manual verification: voxel demo ran for 10 seconds without WebGPU bind-group, pipeline, or initialization errors.
- Intentionally untested: visual quality; CA has no replacement rendering path to compare.
