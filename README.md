# gekko

Gekko is a Go game engine with an ECS core and a voxel real-time renderer (`voxelrt`).

For workspace-level orientation, module-specific commands, and agent notes, see [AGENTS.md](/Users/ddevidch/code/go/gekko3d/gekko/AGENTS.md).

## Repository layout

- `voxelrt/` - renderer implementation and renderer docs
- `assets/` - shared asset IDs, handles, and voxel asset types
- `ecs/` - ECS internals extracted from root package (query maps, archetype keys, slice helpers)
- `physics/` - physics types and world helpers
- `actiongame/` - playable module/example

## Renderer docs

- [voxelrt/ARCHITECTURE.md](voxelrt/ARCHITECTURE.md)
- [voxelrt/rt/RENDERER.md](voxelrt/rt/RENDERER.md)
- [voxelrt/rt/PARTICLES.md](voxelrt/rt/PARTICLES.md)
- [voxelrt/rt/EDITOR.md](voxelrt/rt/EDITOR.md)

## Bridge entry points

The ECS-to-renderer bridge lives in:

- `mod_voxelrt_client.go`
- `mod_voxelrt_client_systems.go`
- `mod_voxelrt_client_materials.go`
- `mod_voxelrt_client_skybox.go`
