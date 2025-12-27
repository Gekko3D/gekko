# VoxelRT Editor: Interactive Voxel Editing

This document explains how interactive voxel editing works in VoxelRT: picking, brush application, copy‑on‑write safety, data flow to GPU, and example controls.

Related
- rt/editor/editor.go — editor logic (ray construction, picking, brush application)
- rt/app/app.go — integration (HandleClick, COW, scene commit)
- rt/core/* — scene, camera, voxel object types
- RENDERER.md — how scene changes propagate to GPU

## Overview

The editor lets you add or remove voxels directly in the scene by:
1) Constructing a camera ray from mouse position (screen space → world space).
2) Ray‑casting into the scene to find the hit object/voxel coordinates.
3) Applying a brush to the object’s XBrickMap (sphere brush for build/erase).
4) Committing the scene change; the renderer syncs buffers on the next Update().

Editing is ECS‑driven in client apps (e.g., actiongame) and integrated into the VoxelRT app via a small event hook.

## Data Flow

- Input → Ray
  - editor.GetPickRay(mouseX, mouseY, width, height, camera)
    - Reconstructs a world‑space ray using the camera forward/right/up, a fixed FOV (60°), and aspect ratio.

- Ray → Hit
  - editor.Pick(scene, ray) → *HitResult
    - Broad phase: intersect world AABBs of voxel objects to cull.
    - Narrow phase: transform ray to object space and call obj.XBrickMap.RayMarch(ro, rd, tMin, tMax).
    - Produces:
      - Object: *core.VoxelObject
      - Coord: [3]int voxel coordinate in object space
      - T: world distance to hit (for nearest selection)
      - Normal: world‑space normal at hit

- Hit → Edit
  - editor.ApplyBrush(obj, centerCoord, normal)
    - Spherical brush:
      - For build (BrushValue != 0): offsets center by rounded normal so added voxels appear “on” the surface.
      - For erase (BrushValue == 0): uses the hit coordinate directly.
      - Iterates a sphere of radius BrushRadius and calls obj.XBrickMap.SetVoxel(x,y,z, BrushValue).

- Safety (COW)
  - app.HandleClick detects if multiple scene objects share the same XBrickMap (copy‑on‑write logic).
  - If shared_count > 1, it clones the XBrickMap before applying the brush to avoid unintended edits of other instances.

- Scene → GPU
  - After a brush operation, the app calls:
    - a.Scene.Commit()
    - It does not call UpdateScene/CreateBindGroups immediately (to avoid render loop races).
  - On the next App.Update(), BufferManager.UpdateScene() detects changes and:
    - Reallocates/re‑uploads buffers as needed (instances, bricks/sectors, materials).
    - Rebuilds dependent bind groups (G‑Buffer, Lighting, Shadows) if buffers are recreated.
  - Rendering picks up the updated scene automatically that frame.

## Core Types

- Editor (rt/editor/editor.go)
  - BrushRadius: float32 (default 2.0)
  - BrushValue: uint8 (default 1; set to 0 for erase)
  - GetPickRay: screen → world ray
  - Pick: raycast against voxel scene
  - ApplyBrush: spherical brush in object space using XBrickMap.SetVoxel

- HitResult
  - Object: *core.VoxelObject
  - Coord: voxel coordinate [3]int in object space
  - Normal: world‑space normal for offsetting build operations
  - T: distance for nearest‑hit selection

- App.HandleClick (rt/app/app.go)
  - COW for shared XBrickMap
  - editor.ApplyBrush and a.Scene.Commit()

## Coordinate Spaces

- Picking transforms the world‑space ray to object space for accurate voxel queries.
- Brush coordinates are in object space; the hit normal is transformed to world space for UI/logic, then rounded to offset the center for building on the surface.
- Objects with transforms (scale/rotation/translation) are handled by converting between world/object spaces via object transforms.

## Example Controls (reference implementation)

In the example client (actiongame):
- Toggle editing: ‘E’
- Mouse Left (LMB): Add/build (BrushValue = palette index, e.g., 1)
- Mouse Right (RMB): Remove/erase (BrushValue = 0)
- +/-: Increase/decrease BrushRadius
- UI overlay shows current brush radius and mode

Note: Input bindings live in the client app; VoxelRT exposes the editor and a HandleClick hook. You can wire your own bindings and UI.

## Performance and Stability

- Broad‑phase AABB checks minimize expensive object‑space ray marches.
- RayMarch in XBrickMap is bounded (t ranges and max steps).
- Copy‑on‑write prevents hidden edits across instances that share voxel data.
- Scene.Commit() defers GPU sync to the render loop to avoid race conditions.

## Extensibility

- Brush shapes:
  - Add cube/ellipsoid/line brushes or falloff masks (Gaussian) in ApplyBrush.
- Material painting:
  - Support painting palette/material indices without changing occupancy.
- Undo/redo:
  - Record voxel deltas per operation (sparse diffs) for history stacks.
- Multi‑object edits:
  - Apply a brush to all objects intersected by pick ray or within a volume.
- Editor modes:
  - Surface vs volume fill, rectangle/box selection, flood fill.
- Gizmos:
  - Visualize brush sphere, hit normal, and target voxel.

## Troubleshooting

- “Nothing happens on click”:
  - Ensure the object has a valid WorldAABB and XBrickMap; confirm pick ray hits object AABB.
  - Check that BrushValue (add/remove) is set correctly.
- “Other instances also change”:
  - Verify COW path is active: app.HandleClick clones XBrickMap when shared.
- “Lag after edits”:
  - Large edits can trigger reallocation of GPU buffers; consider chunked edits or throttling brush frequency.
- “Normals look wrong”:
  - Ensure transforms are correct; hit normal is transformed to world with object_to_world.

## Minimal Integration Checklist

- Create an Editor instance (already built into App.NewApp()).
- Hook input:
  - On click (and/or drag), call app.HandleClick(button, action) or duplicate its logic with your input system.
- Optional:
  - Add on‑screen UI to show brush size/mode.
  - Bind keys to adjust BrushRadius/BrushValue and to toggle “editing mode”.
