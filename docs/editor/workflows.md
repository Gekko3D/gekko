# Editor Workflows

This guide describes the intended authoring workflows in the current editor.

For button-by-button reference, see [`user-guide.md`](user-guide.md).

## Asset Editor Workflows

### Create A New Procedural Asset

1. Open the Asset Editor.
2. Click `new asset`.
3. Open the `Brushes` side-dock tab.
4. Choose one of:
   - `draw box` for an additive cube
   - `draw cut` for a subtractive cube
   - `sphere`, `cone`, `pyramid` for preset primitives
   - `wall`, `floor`, `pillar` for blockout presets
5. For `draw box` or `draw cut`, drag in the viewport and release.
6. Select the new part and use the transform gizmo to move, rotate, or scale it.
7. Save the asset.

### Carve A Hole In An Asset

1. Open an existing asset with additive geometry already present.
2. Click `draw cut` or press `Shift+S`.
3. Left-click drag in the viewport to define the subtractive cube footprint.
4. Release to create the cut part.
5. Reposition or resize the cut using the transform gizmo if needed.
6. Save.

Expected behavior:

- The visible cut cube is an authored subtractive part.
- The actual boolean result preview should line up with the cut volume.
- Cuts are not a temporary draft step in the asset editor. They are normal authored parts with subtract semantics.

### Build A Hierarchy

1. Select a part, light, emitter, or marker.
2. Click `parent to`.
3. Choose the new parent source item.
4. If needed, use `remove parent` later to detach while preserving world placement.

Use this for:

- weapon sockets
- grouped procedural pieces
- lights or emitters attached to moving authored parts

### Add Gameplay / FX Anchors

1. Select a parent part.
2. Click `add marker`, `add light`, or `add emitter`.
3. Move the new child to the desired local position.
4. For markers, cycle `kind: ...` until the correct semantic type is selected.

### Import VOX Geometry As An Asset

1. Select a `.vox` source in the Asset Editor browser.
2. Choose the import mode:
   - single model
   - scene node
   - scene hierarchy
   - scene flattened
3. If scale is unclear, run `calibrate scale`.
4. Import into the asset.
5. Clean up hierarchy, pivots, materials, and markers as needed.
6. Save the `.gkasset`.

Recommended use:

- `single model` for one mesh-like source
- `scene hierarchy` when you need authored parent-child structure preserved
- `scene flattened` when you want a simpler asset without imported hierarchy nodes

### Duplicate Repeated Parts

1. Select a part.
2. Press `Ctrl+D` or click `duplicate part`.
3. Move the duplicate to the next location.
4. Press `Ctrl+Shift+D` to repeat the duplication using the same move step.

This is intended for repeated ribs, pillars, windows, and similar modular layouts.

## Level Editor Workflows

### Block Out A Space With Brushes

1. Open or create a level.
2. Open the `Brushes` panel.
3. Use `draw box` to create additive brush masses.
4. Use `draw cut` to subtract openings, shafts, and trims.
5. Use `sphere`, `cone`, `cylinder`, `capsule`, `pyramid`, and `ramp` for secondary shapes.
6. Keep an eye on brush bake status.
7. If auto-rebake is off, click `rebake now` when ready.

Recommended pattern:

- Start with additive box brushes
- Add ramps and curved primitives after the main massing works
- Use cuts after the major space is stable

### Author Cuts In The Level Editor

1. Click `draw cut` or press `Shift+S`.
2. Drag in the viewport to define the subtract cube.
3. Release to commit the brush.
4. Select the cut brush later if it needs more adjustment.

Important:

- Current cuts are direct subtract brushes.
- There is no separate "apply cut draft" phase in the normal workflow.
- The same cube gizmo language is used for subtract cube previews and authored subtract cube brushes.

### Organize Brushes With Layers

1. Click `new layer`.
2. Rename the layer.
3. Put related brushes together.
4. Use layer `hide`, `lock`, and `solo` states while editing.
5. Use `layer up` / `layer down` to control logical order.

Recommended layer splits:

- shell / structure
- trims
- collision-only helpers
- gameplay blockers
- subtractive cuts

### Place Authored Assets Into A Level

1. Select a `.gkasset` from the level browser.
2. Enter the desired placement mode.
3. Place the asset into the level.
4. Use the transform gizmo to refine placement.

Use placement mode when:

- you want reusable authored assets in the level
- you want detail beyond what is convenient to express with brushes

Use brush authoring when:

- you are shaping navigable space
- you need fast large-scale boolean blockout

### Build And Edit Terrain

1. Open the `Terrain` panel.
2. Either import a PNG heightmap or attach an existing `.gkterrain`.
3. Click `edit terrain`.
4. Choose a terrain brush mode:
   - `raise`
   - `lower`
   - `flat`
   - `smooth`
5. Adjust radius and strength.
6. Paint in the viewport.
7. Save terrain.

Recommended pattern:

- use PNG import to establish the broad landform
- use brush editing for roads, platforms, berms, smoothing, and play-area cleanup

### Import A Large VOX Scene As Base World

1. Open the `Base World` panel.
2. Select a source `.vox`.
3. Fill in manifest path and world id.
4. Set or calibrate scale.
5. Choose whether to normalize origin.
6. Click `bake` or `bake + attach`.
7. Use preview and focus to verify the result in-level.

Use base world import when:

- the source is too large to treat as a normal authored asset
- you want chunked world data backing the level

### Add Markers And Placement Volumes

Markers:

1. Select the marker panel / relevant placement context.
2. Create the marker.
3. Move it to the exact gameplay location.
4. Use focus if needed to verify orientation and placement.

Placement volumes:

1. Create a new box or sphere volume, or volume set.
2. Assign the source asset or set.
3. Choose the rule mode.
4. Tune density / count / constraints.
5. Validate preview warnings before relying on the result.

## Choosing The Right Tool

Use Asset Editor when:

- you are building a reusable object
- you need hierarchy, markers, emitters, lights, or reusable materials
- the result should be referenced by many levels

Use Level Editor brushes when:

- you are shaping level massing or collision volumes
- you need fast additive/subtractive CSG
- the geometry is specific to one level

Use asset placement when:

- the level needs repeated authored props or modular pieces

Use terrain when:

- the surface is heightfield-driven

Use base-world import when:

- the source is a large world-scale voxel scene rather than a normal prop-sized asset

## Common Confusions

### `draw cut` vs `cut`

- `draw cut`
  - starts a viewport authoring tool
- `cut`
  - means the selected authored brush or part already has subtract operation

### `mode` vs `xform`

- `mode`
  - usually placement mode
- `xform`
  - transform gizmo mode

### `reset xform`

- resets local transform values
- does not delete or recreate the item
- does not change parent relationships
- does not change source geometry or material assignments

### Why The Status Message Matters

The editor currently uses short labels and compact panels. The workflow/status line is the authoritative explanation of what the editor expects next, especially for:

- active brush tools
- cut tools
- calibration
- import / bake progress
- validation failures
