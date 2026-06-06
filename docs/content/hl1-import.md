# Half-Life 1 Level Import Plan

This document is the implementation plan for importing Half-Life 1 GoldSrc
maps into the Gekko editor and loading them in `actiongame`.

The long-term pipeline should be:

```text
HL1 BSP v30 + WAD textures + entity lump
  -> Gekko HL1 import IR
  -> generated .gkworld + .gkchunk base-world geometry
  -> generated .gklevel markers/environment/triggers
  -> optional .gkasset props and reusable gameplay prefabs
```

The main rule: do not make MagicaVoxel `.vox` the canonical interchange
format. `.vox` is useful for debug inspection and visual parity checks, but the
Gekko importer should preserve BSP, WAD, texture, material, entity, trigger,
and source-diagnostic data directly.

## Alignment Gate

Known from local docs and code:

- `~/code/other/voxlife` already reads HL1 BSP files, WAD textures, entity
  lumps, lights, landmarks, player starts, changelevel triggers, sky metadata,
  and selected NPC spawns. It then writes Teardown XML plus MagicaVoxel `.vox`
  brush assets.
- Gekko already has the correct large-world shape:
  - `.gklevel` is the top-level authored assembly document.
  - `.gkworld` is an imported-world manifest.
  - `.gkchunk` stores streamed imported-world voxel chunks.
  - `.gkasset` is for reusable authored objects and props, not whole maps.
- `actiongame` already starts a streamed level runtime through
  `StartStreamedLevelRuntime(...)`, currently hardcoded to
  `assets/levels/nuke-lvl.gklevel`.
- `gekko-editor` already has a base-world import workflow, but the existing
  workflow imports `.vox` through `BakeImportedWorldFromVoxFileWithProgress`.
- `content.LevelDef` schema version 3 already has `BaseWorld`, `Environment`,
  and `Markers`.
- `content.ImportedWorldDef` and `content.ImportedWorldChunkDef` schema version
  1 already round-trip JSON manifests and sparse JSON voxel chunks.

Uncertain or still requiring alignment before implementation:

- Whether HL1 imported geometry should be read-only by default, destructible by
  default, or split into read-only world geometry plus selected destructible
  gameplay brush assets.
- First playable scope: geometry-only, geometry plus spawns/lights/triggers, or
  a broader HL1 entity conversion.
- Whether to add compact/binary `.gkchunk` payload support before the first
  playable map or after proving one small map with JSON chunks.
- Where fixture data should live. Do not commit copyrighted HL1 maps, textures,
  WADs, or extracted assets.
- Whether generated HL1 trigger/lights should extend `.gklevel` directly or be
  represented first as typed markers and sidecar import metadata.

Classification:

- This is a long-term architecture step.
- A `.vox` bridge is tactical only. It can be used to compare the direct Gekko
  bake against `voxlife`, but should not shape the Gekko schema.
- The correct first implementation surface is a shared importer library plus a
  CLI. Editor UI should come after generated content validates and loads in
  `actiongame`.

Best-matching alternative:

- Prefer direct `BSP/WAD/entity -> HL1 import IR -> .gkworld/.gkchunk/.gklevel`.
- Avoid `BSP -> .vox -> Gekko`, except as a temporary debug path.
- Avoid storing whole maps as `.gkasset`; use `.gkasset` only for reusable props
  and gameplay prefabs.

## Current Gekko Seams

Use these existing systems before adding new schema.

### Imported world content

Files:

- `gekko/content/imported_world.go`
- `gekko/content/imported_world_io.go`
- `gekko/content/imported_world_validation.go`
- `gekko/content/imported_world_test.go`

Important structs and functions:

- `content.ImportedWorldDef`
  - `WorldID`
  - `SchemaVersion`
  - `Kind`
  - `ChunkSize`
  - `VoxelResolution`
  - `Palette []ImportedWorldPaletteColor`
  - `SourceBuildVersion`
  - `SourceHash`
  - `Tags`
  - `Entries []ImportedWorldChunkEntryDef`
- `content.ImportedWorldChunkDef`
  - `WorldID`
  - `SchemaVersion`
  - `Coord`
  - `ChunkSize`
  - `VoxelResolution`
  - `Voxels []ImportedWorldVoxelDef`
  - `NonEmptyVoxelCount`
  - `Tags`
- `content.SaveImportedWorld(...)`
- `content.LoadImportedWorld(...)`
- `content.SaveImportedWorldChunk(...)`
- `content.LoadImportedWorldChunk(...)`
- `content.ResolveImportedWorldChunkPath(...)`
- `content.ValidateImportedWorld(...)`

Current limitation:

- Chunk payloads are sparse JSON voxel arrays. That is good for fixtures and
  debugging, but risky for full HL1 campaign scale.
- Palette entries currently preserve only RGBA colors. They do not preserve
  source texture names, material classes, transparency, water/slime/lava
  semantics, collision material, or emissive hints.
- Load/save currently expects exact current schema versions, so any schema bump
  needs explicit migration or compatibility handling.

### Level content

Files:

- `gekko/content/level.go`
- `gekko/content/level_io.go`
- `gekko/content/level_validation.go`
- `gekko/content/level_test.go`

Important structs and fields:

- `content.LevelDef`
  - `ChunkSize`
  - `VoxelResolution`
  - `BaseWorld *LevelBaseWorldDef`
  - `Environment *LevelEnvironmentDef`
  - `Markers []LevelMarkerDef`
- `content.LevelBaseWorldDef`
  - `Kind`
  - `ManifestPath`
  - `ReadOnlyByDefault`
  - `CollisionEnabled`
  - `Tags`
- `content.LevelMarkerDef`
  - `ID`
  - `Name`
  - `Kind`
  - `Transform`
  - `Tags`
- Existing marker kinds:
  - `player_spawn`
  - `ai_spawn`
  - `patrol_point`
  - `objective`
  - `extract_point`

Current limitation:

- `.gklevel` does not yet have first-class light definitions.
- `.gklevel` does not yet have first-class trigger volume definitions.
- `LevelEnvironmentDef` is currently only `Preset` plus tags, so HL1 skybox,
  sun direction, sun color, fog, and ambient data need either schema extension
  or import sidecar metadata.
- Validation already checks base-world manifest extension, chunk size, voxel
  resolution, manifest validity, and marker placement against solid imported
  geometry.

### Streamed runtime

Files:

- `gekko/streamed_level_runtime.go`
- `gekko/streamed_level_runtime_test.go`
- `gekko/imported_world_spawn.go`
- `gekko/runtime_content_loader.go`

Important functions:

- `StartStreamedLevelRuntime(...)`
- `ImportedWorldChunkToXBrickMap(...)`
- `spawnAuthoredImportedWorldChunkEntity(...)`
- `ImportedWorldPaletteAsset(...)`

Current runtime behavior:

- Loads `level.BaseWorld.ManifestPath`.
- Validates imported-world chunk size and voxel resolution against the level.
- Registers a palette from `ImportedWorldDef.Palette`.
- Streams chunks around the observer.
- Converts each imported chunk to `volume.XBrickMap`.
- Spawns `VoxelModelComponent` for visible geometry.
- Adds static collision when `BaseWorld.CollisionEnabled` is true.

Current limitation:

- Runtime only consumes imported geometry and palette colors.
- It does not spawn imported HL1 lights from `.gklevel`.
- It does not create trigger volumes for `trigger_changelevel`.
- It does not map imported entity classes to actiongame behavior.
- `actiongame` still needs a way to choose a generated HL1 level instead of the
  hardcoded level path.

### Editor

Files:

- `gekko-editor/src/modules/level_editor/base_world_import.go`
- `gekko-editor/src/modules/level_editor/base_world_ui.go`
- `gekko-editor/src/modules/level_editor/imported_world_preview.go`
- `gekko-editor/src/modules/level_editor/level_workflow.go`
- `gekko-editor/src/modules/level_editor/level_workflow_test.go`

Current behavior:

- Editor can import `.vox` into a generated imported world.
- Editor can attach a generated `.gkworld` to the current or a new `.gklevel`.
- Editor previews imported-world chunks.

Current limitation:

- No HL1 source selection.
- No BSP/WAD parsing.
- No import report UI.
- No unsupported-entity diagnostics UI.
- No HL1-specific generated marker or trigger presentation.

## voxlife Reference Map

Use `~/code/other/voxlife` as a reference implementation, not as a dependency
boundary to preserve forever.

Useful code to port or translate:

- `src/hl1/read_level.cpp`
  - Opens `game_path/valve/maps/<level>.bsp`.
  - Reads entities.
  - Extracts `worldspawn.wad`.
  - Opens WADs and loads textures.
  - Detects `SKY` textures.
  - Calls GPU voxelization.
  - Extracts `light`, `light_environment`, `info_landmark`,
    `info_player_start`, `trigger_changelevel`, `worldspawn.skyname`, and a
    small set of NPC classes.
- `src/bsp/read_file.cpp` and `src/bsp/read_file.h`
  - Validates BSP version 30.
  - Parses entities, planes, textures, vertices, nodes, texinfo, faces,
    clipnodes, leafs, marksurfaces, edges, surfedges, and models.
  - Reconstructs model faces with vertices, normal, texture axes, texture
    shift, and texture id.
  - Computes brush model AABBs.
- `src/wad/read_file.cpp` and `src/wad/read_file.h`
  - Reads WAD files.
  - Indexes texture entries case-insensitively.
  - Rejects unsupported compressed entries.
- `src/bsp/read_entities.cpp` and `src/hl1/read_entities.*`
  - Tokenizes entity key/value lumps.
  - Maps selected HL1 classes into typed records.
- `src/voxel/voxelize_bsp.cpp`
  - Provides the BSP face grouping and textured voxelization approach.
  - Uses the scale `0.0254 / 0.1` to convert Hammer inches to 0.1 meter voxel
    cells.
- `src/voxel/shaders/voxelize.glsl`
  - Samples textures and writes packed voxel color.

Code to avoid as long-term architecture:

- Teardown XML/Lua output.
- MagicaVoxel writer as the canonical Gekko path.
- Any behavior that exists only because MagicaVoxel has a 256-cube practical
  model constraint.

## Format Strategy

### `.gklevel`

Use `.gklevel` for:

- Map identity and top-level assembly.
- `base_world.manifest_path`.
- `base_world.read_only_by_default`.
- `base_world.collision_enabled`.
- Player spawn marker.
- Landmark markers.
- Imported entity debug markers.
- Environment fields when schema supports them.
- Trigger volume fields when schema supports them.

Phase 1 should use existing fields only:

- Set `BaseWorld` to the generated `.gkworld`.
- Generate a `player_spawn` marker.
- Generate landmark and unsupported entity markers using new string `Kind`
  values, because `LevelMarkerDef.Kind` is already a string.
- Preserve richer entity data in the import report or sidecar metadata until
  `.gklevel` grows first-class trigger/light schema.

Phase 2 should extend `.gklevel` with first-class records if multiple consumers
need to inspect or edit the data:

- `Lights []LevelLightDef`
- `TriggerVolumes []LevelTriggerVolumeDef`
- richer `Environment`
- optional `SourceMetadata` or `ImportedEntities`

Do not store full HL1 maps in `.gklevel` brush arrays. The BSP map geometry
belongs in `.gkworld` plus `.gkchunk`.

### `.gkworld`

Use `.gkworld` for:

- Imported world identity.
- Source hash/build metadata.
- Voxel resolution.
- Chunk size.
- Palette table.
- Chunk manifest entries.

Phase 1 should use the existing schema and add metadata through `Tags`,
`SourceBuildVersion`, `SourceHash`, and the import report.

Phase 2 should consider schema version 2 with optional fields:

```go
type ImportedWorldDef struct {
    // existing fields...
    SourceKind       string                        `json:"source_kind,omitempty"`
    SourceMapName    string                        `json:"source_map_name,omitempty"`
    SourceGamePath   string                        `json:"source_game_path,omitempty"`
    SourceMaterials  []ImportedWorldMaterialDef    `json:"source_materials,omitempty"`
    ChunkPayloadKind string                        `json:"chunk_payload_kind,omitempty"`
}

type ImportedWorldMaterialDef struct {
    PaletteIndex      uint8    `json:"palette_index"`
    SourceTextureName string   `json:"source_texture_name,omitempty"`
    BaseColor         [4]uint8 `json:"base_color"`
    MaterialKind      string   `json:"material_kind,omitempty"`
    CollisionKind     string   `json:"collision_kind,omitempty"`
    EmitsLight        bool     `json:"emits_light,omitempty"`
    Transparent       bool     `json:"transparent,omitempty"`
}
```

Schema extension rules:

- Add optional fields where possible.
- Keep old manifests loadable.
- If `LoadImportedWorld(...)` continues rejecting older schema versions, add a
  migration path before bumping the version.
- Keep import diagnostics in reports even if runtime ignores them.

### `.gkchunk`

Use `.gkchunk` for:

- Streamed voxel payloads.
- Per-chunk sparse or compact voxel data.
- Chunk-local metadata only when needed.

Phase 1 can use the current JSON shape for a small map:

```go
type ImportedWorldVoxelDef struct {
    X     int   `json:"x"`
    Y     int   `json:"y"`
    Z     int   `json:"z"`
    Value uint8 `json:"value"`
}
```

Phase 2 should add compact payload support before broad campaign import:

- Keep `.gkchunk` extension.
- Add `payload_kind`, for example `sparse_json_v1`, `rle_binary_v1`, or
  `zstd_binary_v1`.
- Add `payload_hash` for deterministic validation.
- Keep a debug dump/report path so failures remain inspectable.

### `.gkasset`

Do not use `.gkasset` for whole BSP maps.

Use `.gkasset` later for:

- Reusable props.
- NPC prefabs.
- Pickup prefabs.
- Doors, buttons, breakables, or scripted objects after actiongame has systems
  for them.

## Proposed Package Layout

Prefer a new importer package outside `content`:

```text
gekko/importers/hl1/
  bsp.go
  bsp_faces.go
  bsp_test.go
  wad.go
  wad_test.go
  entities.go
  entities_test.go
  coords.go
  coords_test.go
  ir.go
  voxelize.go
  emit_content.go
  report.go

gekko/cmd/hl1import/
  main.go
```

Rationale:

- `content` should remain the schema, IO, and validation package.
- `importers/hl1` can depend on `content` to emit Gekko documents.
- Runtime code should not depend on HL1 parser internals.
- Editor and CLI can share the same importer package.

If this repository avoids top-level importer packages, a reasonable fallback is
`gekko/content/hl1import`, but that is less clean because parser code is source
format specific rather than Gekko content schema.

## Import IR

Create an internal IR before writing Gekko files. This prevents runtime schema
churn while the importer matures.

Suggested core types:

```go
type ImportOptions struct {
    GameDir             string
    MapName             string
    OutputRoot          string
    ChunkSize           int
    VoxelResolution     float32
    ReadOnlyByDefault   bool
    CollisionEnabled    bool
    NormalizeOrigin     bool
    DebugVoxPath        string
    AllowMissingTexture bool
}

type MapImport struct {
    Source      SourceInfo
    Bounds      Bounds
    Materials   []Material
    Voxels      []Voxel
    Entities    []Entity
    Lights      []Light
    Triggers    []Trigger
    PlayerSpawns []PlayerSpawn
    Landmarks   []Landmark
    Environment Environment
    Diagnostics []Diagnostic
}

type SourceInfo struct {
    GameDir      string
    MapName      string
    BSPPath      string
    WADPaths     []string
    BSPHash      string
    ImporterName string
    ImporterVersion string
}

type Material struct {
    ID                int
    PaletteIndex      uint8
    SourceTextureName string
    BaseColor         [4]uint8
    Kind              string
    CollisionKind     string
    Transparent       bool
    EmitsLight        bool
}

type Voxel struct {
    X          int
    Y          int
    Z          int
    Palette    uint8
    MaterialID int
}

type Entity struct {
    ClassName        string
    KeyValues        map[string]string
    SourceOrigin     Vec3
    WorldPosition    Vec3
    SourceAngles     Vec3
    WorldRotation    Quat
    BrushModelID     int
    BrushWorldBounds Bounds
}

type Light struct {
    Name       string
    Position   Vec3
    Color      [3]float32
    Intensity  float32
    Range      float32
    Style      string
    Source     Entity
}

type Trigger struct {
    Kind       string
    Bounds     Bounds
    TargetMap  string
    Landmark   string
    Source     Entity
}

type Diagnostic struct {
    Severity string
    Code     string
    Subject  string
    Message  string
}
```

Keep the IR package-private until the importer has at least one stable external
consumer. The stable API should be the import options, generated files, and
report.

## Coordinate And Scale Policy

GoldSrc/Hammer units are inches. Gekko imported-world `VoxelResolution` is a
world-space unit per voxel. The recommended first Gekko policy is:

```text
1 Hammer unit = 0.0254 Gekko world units
voxel_resolution = 0.1
voxel cell = floor(world_position / voxel_resolution)
```

Observed `voxlife` behavior:

- Uses `0.0254 / 0.1` when converting Hammer coordinates to 0.1 meter voxel
  cells.
- Converts positions for Teardown with an axis remap similar to:
  `{x, z, -y}` plus additional Teardown-specific scale handling.

Proposed Gekko conversion:

```text
hammer = (x, y, z)
gekko_world = (x, z, -y) * 0.0254
voxel = floor(gekko_world / voxel_resolution)
```

Do not treat this as final until verified with fixtures:

- a cube centered at Hammer origin
- a known `info_player_start`
- a known `trigger_changelevel` brush model
- one asymmetrical room where handedness mistakes are obvious

Acceptance criteria for the conversion:

- Player spawn appears in the expected room.
- Gravity/up axis matches Gekko runtime.
- Trigger bounds surround the visible trigger brush area.
- Imported world is not mirrored relative to landmarks and changelevel exits.
- Collision aligns with visible voxels.

## Material And Texture Policy

Phase 1:

- Sample WAD texture colors into a palette.
- Store palette RGBA in `ImportedWorldDef.Palette`.
- Store source texture/material details in the import report.
- Classify obvious special textures:
  - `SKY` and `sky`: skipped from solid geometry and mapped to environment
    metadata.
  - Water/slime/lava texture names: preserve as diagnostics or marker data
    until runtime supports liquid volumes.
  - Transparent textures: preserve diagnostics; render as opaque unless the
    renderer/imported-world format supports transparency.

Phase 2:

- Add `source_materials` to `.gkworld` or a companion metadata document.
- Preserve texture name, source WAD, average color, material kind, collision
  kind, transparency, and emissive hints.
- Use source material classification to drive actiongame behavior.

Palette risk:

- HL1 maps can have more meaningful texture/material classes than a uint8
  palette can represent.
- Do not collapse material identity permanently into RGBA. Keep source material
  ids in IR and reports even if phase 1 chunks only store palette values.

## CLI Contract

Create the CLI before editor UI.

Suggested command:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
go run ./cmd/hl1import \
  -game-dir /path/to/Half-Life \
  -map c1a0 \
  -out ../actiongame/assets/levels \
  -chunk-size 32 \
  -voxel-resolution 0.1 \
  -read-only=true \
  -collision=true
```

Suggested output:

```text
assets/levels/c1a0.gklevel
assets/levels/worlds/c1a0.gkworld
assets/levels/worlds/c1a0_import_report.json
assets/levels/worlds/chunks/c1a0_0_0_0.gkchunk
assets/levels/worlds/chunks/...
```

Import report fields:

- source map path and hash
- resolved WAD paths
- missing WADs/textures
- generated level path
- generated world path
- chunk count
- non-empty voxel count
- material count
- palette count
- skipped faces by reason
- converted entity counts by classname
- unsupported entity counts by classname
- coordinate bounds before and after conversion
- warnings for huge chunks or suspicious scale

## Generated Level Shape

Initial generated `.gklevel` should use existing schema:

```json
{
  "schema_version": 3,
  "name": "c1a0",
  "chunk_size": 32,
  "voxel_resolution": 0.1,
  "base_world": {
    "kind": "imported_voxel_world",
    "manifest_path": "worlds/c1a0.gkworld",
    "read_only_by_default": true,
    "collision_enabled": true,
    "tags": ["source:hl1", "map:c1a0"]
  },
  "environment": {
    "preset": "hl1:c1a0"
  },
  "markers": [
    {
      "name": "info_player_start",
      "kind": "player_spawn",
      "tags": ["source:hl1", "classname:info_player_start"]
    },
    {
      "name": "landmark_start",
      "kind": "hl1_landmark",
      "tags": ["source:hl1", "classname:info_landmark"]
    },
    {
      "name": "changelevel_c1a1",
      "kind": "hl1_trigger_changelevel",
      "tags": ["source:hl1", "classname:trigger_changelevel", "target_map:c1a1"]
    }
  ]
}
```

When `.gklevel` gets first-class lights and triggers, migrate generated data out
of marker-only representation.

## Implementation Milestones

### M1: Parser And Fixtures

Goal:

- Parse BSP v30, WAD metadata, texture entries, and raw entity key/value data
  without emitting Gekko content.

Implementation:

- Add `gekko/importers/hl1/bsp.go`.
- Add `gekko/importers/hl1/wad.go`.
- Add `gekko/importers/hl1/entities.go`.
- Add small synthetic fixtures generated by tests, not copied from HL1.

Acceptance criteria:

- BSP version 30 loads.
- Unsupported BSP version is rejected.
- Lump bounds are validated.
- Entity parser handles representative quoted key/value records.
- WAD lookup is case-insensitive.
- Missing external WADs produce diagnostics, not panics.

Tests:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1/...
```

### M2: Entity Extraction And Coordinates

Goal:

- Convert the HL1 source data into Gekko import IR for entities, lights,
  player starts, landmarks, triggers, and environment.

Implementation:

- Add `coords.go`.
- Add typed extraction for:
  - `worldspawn`
  - `info_player_start`
  - `info_landmark`
  - `trigger_changelevel`
  - `light`
  - `light_environment`
- Preserve all keyvalues even when a class is not supported.

Acceptance criteria:

- `worldspawn.wad` resolves to ordered candidate WAD paths.
- `worldspawn.skyname` is preserved.
- `info_player_start` produces a Gekko marker transform.
- `trigger_changelevel` resolves `model "*N"` to brush bounds.
- Unsupported classes are counted and reported.
- Coordinate conversion tests cover axis orientation and handedness.

### M3: Direct Voxel Bake To Existing `.gkworld`

Goal:

- Emit a small map as Gekko imported-world JSON chunks without going through
  `.vox`.

Implementation:

- Add `voxelize.go`.
- Add `emit_content.go`.
- Start with a CPU reference voxelizer if it is fast enough for small maps.
- Port or replace the `voxlife` GPU voxelizer later if CPU bake is too slow.

Acceptance criteria:

- SKY faces are skipped from solid chunks.
- Textured faces produce deterministic palette values.
- Voxels partition into `ImportedWorldChunkDef`.
- `content.ValidateImportedWorld(...)` passes.
- Import report includes chunk and material statistics.

Tests:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./content/... ./importers/hl1/...
```

### M4: Generated `.gklevel` And Actiongame Load

Goal:

- Generate a `.gklevel` with an imported base world and load it in
  `actiongame`.

Implementation:

- Emit `LevelDef.BaseWorld`.
- Emit `player_spawn` marker.
- Emit `hl1_landmark` and `hl1_trigger_changelevel` markers as transitional
  data.
- Add an `actiongame` startup option or config path so generated maps can be
  loaded without editing source every time.

Acceptance criteria:

- Generated `.gklevel` validates.
- `StartStreamedLevelRuntime(...)` loads the generated world.
- Player spawns in the expected area.
- Imported collision aligns with visible geometry.
- Chunk streaming loads and unloads without obvious stalls for one small map.

Tests:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test .

cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Manual check:

- Run `actiongame` with the generated level.
- Check scale, spawn position, orientation, collision, visible geometry,
  streaming boundaries, and absence of severe chunk gaps.

### M5: Lights, Environment, And Trigger Semantics

Goal:

- Preserve imported light/environment/trigger data in editable Gekko content.

Implementation alternatives:

- Tactical: keep lights/triggers as typed markers plus report metadata.
- Long-term: add first-class `LevelLightDef`, `LevelTriggerVolumeDef`, and
  richer `LevelEnvironmentDef`.

Recommended path:

- Add first-class schema once actiongame and editor both need to consume the
  data.

Acceptance criteria:

- Point lights appear at imported locations.
- `light_environment` creates directional/sun/ambient environment data.
- `trigger_changelevel` has bounds and target metadata.
- Editor can inspect imported lights/triggers without reading ad hoc report
  JSON.

### M6: Compact Chunk Payloads

Goal:

- Make imported maps practical beyond one or two small maps.

Implementation:

- Extend `.gkchunk` with payload kind.
- Add compact binary or compressed payload.
- Keep JSON support for fixtures and debugging.
- Update content IO, validation, runtime loader, and tests.

Acceptance criteria:

- Existing JSON chunks still load.
- Binary/compressed chunks validate.
- Runtime streaming works with both payload kinds.
- Import report records uncompressed and compressed sizes.

### M7: Editor Integration

Goal:

- Add an HL1 import workflow to the level editor after the CLI path is stable.

Implementation:

- Add an HL1 import panel next to the current `.vox` base-world import.
- Let the user select:
  - HL1 game directory
  - map name
  - output paths
  - chunk size
  - voxel resolution
  - read-only/collision flags
  - optional debug `.vox` export
- Show:
  - progress
  - chunk count
  - voxel count
  - palette/material count
  - missing WAD/texture warnings
  - unsupported entity counts
  - import report path
- Attach the generated `.gkworld` to the open or new `.gklevel`.

Editor files likely touched:

- `gekko-editor/src/modules/level_editor/base_world_import.go`
- `gekko-editor/src/modules/level_editor/base_world_ui.go`
- `gekko-editor/src/modules/level_editor/imported_world_preview.go`
- `gekko-editor/src/modules/level_editor/level_workflow.go`
- `gekko-editor/src/modules/level_editor/level_workflow_test.go`

Acceptance criteria:

- Editor can run the same importer package as the CLI.
- Import can be cancelled or surfaced as failed with diagnostics.
- Generated world can be previewed.
- Existing `.vox` import remains available until direct HL1 import replaces
  the need for it.

### M8: Actiongame Entity Mapping

Goal:

- Incrementally map selected HL1 entity classes to actiongame behavior.

Initial mapping order:

1. `info_player_start`
2. `light`
3. `light_environment`
4. `trigger_changelevel`
5. `info_landmark`
6. pickups and simple props
7. doors/buttons/ladders/water only after actiongame has matching systems
8. NPC spawn markers, then real NPC behavior later

Acceptance criteria:

- Unsupported classes remain visible in diagnostics.
- Imported behavior is opt-in and testable.
- No fake HL1 gameplay parity is implied before systems exist.

## Missing Capabilities Checklist

Engine/content:

- [ ] HL1 importer package.
- [ ] Import report schema.
- [ ] Optional `.gkworld` source material metadata.
- [ ] Optional `.gkchunk` compact payload support.
- [ ] Optional `.gklevel` light definitions.
- [ ] Optional `.gklevel` trigger volume definitions.
- [ ] Richer `LevelEnvironmentDef`.

Runtime:

- [ ] Load selected generated HL1 level in `actiongame`.
- [ ] Spawn/import level-owned lights.
- [ ] Apply imported player spawn rotation.
- [ ] Represent changelevel trigger volumes.
- [ ] Debug render unsupported imported entities.
- [ ] Validate imported collision scale visually.

Editor:

- [ ] HL1 import panel.
- [ ] Import progress and cancellation.
- [ ] Import report display.
- [ ] Missing WAD/texture diagnostics.
- [ ] Unsupported entity diagnostics.
- [ ] Imported light/trigger inspection after schema exists.

## Verification Matrix

Parser/importer:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1/...
```

Content schemas and validation:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./content/...
```

Runtime streaming:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test .
```

Editor workflow:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko-editor
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Actiongame integration:

```bash
cd /Users/ddevidch/code/go/gekko3d/actiongame
env GOCACHE=/tmp/gekko3d-gocache go test ./...
```

Manual visual/GPU checks:

- Import one small indoor map.
- Open it in `gekko-editor`.
- Confirm base-world preview streams chunks around the camera.
- Run `actiongame` with the generated level.
- Confirm player spawn position and orientation.
- Confirm world scale.
- Confirm visible geometry.
- Confirm collision.
- Confirm point light placement if imported lights are enabled.
- Confirm no severe chunk-boundary artifacts.
- Confirm streaming radius loads/unloads chunks without stalls.

Manual scale-up checks:

- Import one outdoor/sky map.
- Import one map with changelevel triggers.
- Import one map with dense texture/material variety.
- Compare report voxel counts, chunk hotspots, and file sizes before broad
  campaign import.

## Risk Log

- JSON chunk size may become the largest blocker for full campaign imports.
- Palette-only material storage loses HL1 texture/material semantics unless the
  IR/report keeps source material ids.
- CPU voxelization may be too slow; GPU voxelization may be harder to test
  deterministically.
- Missing WADs and case-sensitive path differences must be reported clearly.
- `SKY`, transparent, water, slime, lava, ladder, clip, and trigger textures
  need special handling.
- Trigger brush semantics are not the same as visible geometry.
- Collision scale mistakes will make imported maps feel wrong even when they
  look correct.
- Imported lighting may need renderer/runtime work before it looks like HL1.
- HL1 assets/maps are copyrighted. Do not commit original BSP, WAD, texture, or
  extracted asset data to this repository.

## Questions To Resolve Before Code Implementation

These are the alignment questions that should be answered before schema/runtime
changes, but they do not block maintaining this planning doc.

- Should imported HL1 world geometry be read-only by default?
- Is the first playable target geometry-only, or geometry plus
  spawns/lights/changelevel triggers?
- Should compact `.gkchunk` payloads be implemented before the first real map?
- Should `.gklevel` get first-class lights/triggers immediately, or should the
  first importer version use markers plus report metadata?
- Should the CLI live at `gekko/cmd/hl1import`?
- What generated level selection mechanism should `actiongame` use?
- What non-copyrighted fixtures should be used for automated parser and
  coordinate tests?

## Recommended First Slice

Implement the first slice in this order:

1. Add `gekko/importers/hl1` with parser-only tests.
2. Add entity extraction and coordinate conversion tests.
3. Add a CLI that emits an import report without writing Gekko content.
4. Add direct `.gkworld`/`.gkchunk` emission for a synthetic fixture.
5. Add `.gklevel` emission with `BaseWorld` and `player_spawn`.
6. Load the generated level with `StartStreamedLevelRuntime(...)`.
7. Add `actiongame` level selection.
8. Only then add editor UI.

This sequence keeps the architecture aligned with existing Gekko content
formats while avoiding a short-lived MagicaVoxel-centered importer.
