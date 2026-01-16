[x] Transparency now looks broken, need to rework it to go through the voxel object and accumulate color/alpha.
[ ] Refactor CA-particles. Leave the ordinary particles in place, extract CA-particles from it,
implement custom user-defined materials based on map-function and CA, which can be applied to any voxel object.
[ ] Rework of the physics based on key-pointers of the model (edges and verticals points),
new physics acceleration data structure is required here. 
[ ] Rework of the collision: calculate the collision in the same model-space between the voxels.