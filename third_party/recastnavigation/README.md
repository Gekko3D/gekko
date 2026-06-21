This directory vendors the Recast geometry-processing library from:

https://github.com/recastnavigation/recastnavigation

Only the `Recast` module is included here. Detour is intentionally not vendored
because Gekko keeps its existing persisted nav tile/query format and uses Recast
only as the build-time voxel/contour/polygonization step.

The upstream files retain their original zlib-style license headers.
