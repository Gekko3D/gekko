# Editor User Guide

This guide explains what the current editor UI does at a practical level.

Use it together with:

- [`workflows.md`](workflows.md) for end-to-end authoring flows
- [`../content/asset-format.md`](../content/asset-format.md) for `.gkasset`
- [`../content/levels.md`](../content/levels.md) for `.gklevel`
- [`integration.md`](integration.md) for engine/editor integration details

## Shared Concepts

- Selection
  - Click an authored preview object to select it.
  - Most context actions only appear when something is selected.
- Transform gizmo
  - `move`, `rotate`, and `scale` act on the currently selected item.
  - In the Level Editor the top-bar button is shown as `xform: move`, `xform: rotate`, or `xform: scale`.
  - In the Asset Editor the same modes are cycled with the `mode` button in the selection toolbar.
- Focus
  - `focus` moves the editor camera to frame the selected item.
- Workflow messages
  - The dim status text in the UI is not decorative. It usually tells you what mode is active, what click is expected next, or why an action failed.

## Asset Editor

The Asset Editor is for authoring a single `.gkasset` document.

### Main Browser / Document Actions

- `new asset`
  - Creates a new empty asset document.
- `save`
  - Saves the current asset to its existing path.
- `save as`
  - Saves the current asset under a new path.
- `.vox`
  - Source-library list for importing voxel assets.
- `.gkasset`
  - Asset-library list for loading existing authored assets.

### Procedural Shapes Panel

- `draw box`
  - Starts the direct box brush tool.
  - Use left-click drag in the viewport to define the box footprint.
  - Release to create an additive cube part.
- `draw cut`
  - Starts the direct cut brush tool.
  - Use left-click drag in the viewport to define the cut footprint.
  - Release to create a subtractive cube part.
  - The preview uses the same cube gizmo language as the Level Editor.
- `sphere`, `cone`, `pyramid`
  - Select a preset primitive preview, then add it to the asset.
  - These are not drag-authored from the viewport like `draw box` / `draw cut`.
- `wall`, `floor`, `pillar`
  - Blockout presets.
  - They create common cube-based parts with useful default proportions.

### Materials Panel

- Material preset buttons create reusable asset materials.
- If a voxel-backed part is selected when you create a material, the editor assigns that material immediately.

### Selection Toolbar

This toolbar appears when an item is selected.

- `focus`
  - Frames the selected part, light, emitter, or marker in the viewport.
- `parent to`
  - Starts reparenting mode. The selected item becomes the child and you choose a new parent source.
- `remove parent`
  - Clears the selected item's parent while preserving its world transform.
- `duplicate part`
  - Duplicates the selected part.
- `add light`
  - Creates a child light under the selected part.
- `add emitter`
  - Creates a child particle emitter under the selected part.
- `add marker`
  - Creates a child marker under the selected part.
- `kind: ...`
  - Only shown for markers.
  - Cycles the marker kind.
- `rename`
  - Renames the selected item.
- `remove`
  - Deletes the selected item.
- `reset xform`
  - Resets the selected item's transform to identity.
  - Position becomes `0,0,0`.
  - Rotation becomes identity.
  - Scale becomes `1,1,1`.
  - Pivot is preserved.
  - Parenting is preserved.
- `mode`
  - Cycles the transform gizmo mode between move, rotate, and scale.

### Asset Hierarchy / Item List

- Parts are shown by part name.
- Subtractive parts are shown with a `[cut]` suffix.
- Markers are shown with their marker kind, for example `[muzzle]`.
- Hidden / locked / solo state is editor-only view state and is not serialized into `.gkasset`.

### Asset Editor Shortcuts

- `Shift+A`
  - Start `draw box`
- `Shift+S`
  - Start `draw cut`
- `F`
  - Focus selected item
- `Ctrl+D`
  - Duplicate selected part
- `Ctrl+Shift+D`
  - Duplicate selected part and reapply the last move offset, if one has been captured from a previous move
- `Esc`
  - Cancel the active box / cut tool

### Notes About Cuts In Assets

- Asset cuts are authored as subtractive parts inside the asset.
- The editor keeps the subtractive volume visible for editing and also shows the collapsed boolean result when needed.
- `draw cut` is the direct viewport workflow.
- `cut box`, if exposed by a preset path, means "spawn a default subtract cube part" rather than "enter viewport drag mode".

## Level Editor

The Level Editor is for authoring a `.gklevel` plus linked placements, brushes, terrain, markers, placement volumes, and optional base-world data.

### Main Browser / Document Actions

- `new level`
  - Creates a new empty level document.
- `save`
  - Saves the current level.
- `save as`
  - Saves the level under a new path.
- `.gklevel`
  - Opens an existing level.
- `.gkasset library`
  - Starts placement preview for authored assets.
- `.gkset library`
  - Selects authored sets for placement workflows.

### Level Toolbar

- `draw cut`
  - Starts the direct subtract cube brush tool.
- `sphere`, `cone`, `pyramid`
  - Starts the corresponding brush authoring tool.
- `mode: ...`
  - Cycles placement mode for level placements.
  - This is for authored asset placement behavior, not for brush transform mode.
- `xform: ...`
  - Cycles the transform gizmo mode for the currently selected editable object.

### Brushes Panel

This is the main CSG/blockout panel.

- `auto rebake: on/off`
  - Controls whether brush changes trigger immediate rebake.
- `rebake now`
  - Forces a full brush rebake immediately.
- `draw box`
  - Starts the direct additive cube brush tool.
- `draw cut`
  - Starts the direct subtractive cube brush tool.
- `sphere`, `cone`, `cylinder`, `capsule`, `pyramid`, `ramp`
  - Starts primitive brush creation.
- `new layer`
  - Adds a new brush layer.
- Layer row
  - Selects the active brush layer.
- Layer actions
  - `hide` / `unhide`
  - `lock` / `unlock`
  - `solo` / clear solo
  - `layer up`
  - `layer down`
  - `delete`
- Brush row
  - Selects the brush.
  - Cut brushes are labeled with `[cut]`.
- Selected brush actions
  - `focus`
  - `duplicate`
  - operation toggle such as `cut`
  - `move up`
  - `move down`
  - `delete`
  - `hide` / `unhide`
  - `lock` / `unlock`

### Terrain Panel

- `edit terrain`
  - Enables terrain paint mode.
- `pick objects`
  - Leaves terrain paint mode and returns to normal object selection.
- `save terrain`
  - Saves the attached terrain source and baked terrain data.
- `import png`
  - Creates or updates terrain from a heightmap PNG.
- `.gkterrain library`
  - Attaches an existing terrain source.
- Brush modes
  - `raise`
  - `lower`
  - `flat`
  - `smooth`
- Brush fields
  - `Radius`
  - `Strength`
  - `Flat Y`

### Base World Panel

Use this when importing a large voxel world into the level as a base-world source.

- `preview` / `hide preview`
  - Toggles preview chunks for the attached base world.
- `focus`
  - Frames the attached base world.
- `detach`
  - Removes the base-world link from the level.
- `collision`
  - Toggles collision for the base world.
- `read only`
  - Toggles read-only-by-default behavior for imported world data.
- `.gkworld library`
  - Attaches an existing baked base world.
- `.vox` import list
  - Selects a voxel source file for baking a new base world.
- `calibrate scale`
  - Opens the two-point scale calibration workflow for the selected `.vox`.
- `bake`
  - Bakes the selected `.vox` into base-world chunk data.
- `bake + attach`
  - Bakes and immediately attaches the result to the current level.

### Level Editor Shortcuts

- `Shift+A`
  - Start additive cube brush tool
- `Shift+S`
  - Start subtract cube brush tool
- `1`
  - Start cube brush tool
- `2`
  - Start cone brush tool
- `3`
  - Start sphere brush tool
- `4`
  - Start pyramid brush tool
- `F`
  - Focus selected brush
- `D`
  - Duplicate selected brush
- Right-click during brush spawn
  - Cancel the active brush tool

## Reading The UI Safely

If a button name is short, interpret it in the context of its panel:

- `mode`
  - Asset Editor: transform gizmo mode
- `mode: ...`
  - Level Editor toolbar: placement mode
- `xform: ...`
  - Level Editor toolbar: transform gizmo mode
- `draw cut`
  - Direct viewport authoring tool
- `cut`
  - Operation state of an already-authored brush
- `reset xform`
  - Reset local transform only, not hierarchy, not source data

When in doubt, check the workflow/status message first. The current editor relies on that text to explain what input it expects next.
