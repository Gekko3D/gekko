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

## Architecture Decision

Use `voxlife` as a reference implementation, not as the data model. The Gekko
importer should read the original source data directly, translate it into a
source-aware import IR, then emit normal Gekko content. The canonical result is
a streamed Gekko base world, not a MagicaVoxel scene.

The importer architecture should be source-extensible from the start:

```text
source adapter
  HL1 BSP/WAD/entities
  Minecraft region/block/entity data
  future importers
    -> shared import IR
    -> Gekko content emitter
    -> .gkworld/.gkchunk/.gklevel/.gkasset where appropriate
```

Half-Life 1 and Minecraft are different source formats, but they should share
the boring parts of the pipeline: material records, voxel chunk emission,
diagnostics, generated level assembly, editor progress/report UI, and runtime
UAT loading. HL1 needs BSP-aware solid reconstruction; Minecraft is already
block-native and should bypass that step.

Do not extend Gekko formats before the first direct importer proves the missing
data. Phase 1 should use current `.gkworld`, `.gkchunk`, and `.gklevel` fields
plus an import report. Add versioned schema fields only when more than one
consumer needs to inspect or edit the generated data.

The first UAT target is:

- import one locally supplied HL1 map without committing copyrighted assets
- generate `actiongame/assets/levels/<map>.gklevel`
- stream it through `StartStreamedLevelRuntime(...)`
- spawn the player from `info_player_start`
- verify collision, scale, orientation, lighting diagnostics, chunk streaming,
  and at least one destructive edit against imported solid matter

Visual fidelity is a core requirement, not optional polish. Imported HL1 maps
should look as close to the original as the voxel renderer and current palette
path allow. The importer therefore must bake original BSP/WAD texture samples
onto visible voxel surfaces. Per-texture average colors are acceptable only as
diagnostics and fallback for missing/unsupported texture data.

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

- Whether HL1 imported geometry should be destructible by default, read-only by
  default, or split into read-only world geometry plus selected destructible
  gameplay brush assets. The UAT should exercise destruction, but the default
  authoring policy still needs an explicit decision.
- First playable scope: geometry plus player spawn and collision is the minimum;
  lights and changelevel trigger metadata are recommended for the first UAT;
  broader HL1 entity conversion is not part of the first slice.
- Whether to add compact/binary `.gkchunk` payload support before the first
  playable map or after proving one small map with JSON chunks.
- Where fixture data should live. Do not commit copyrighted HL1 maps, textures,
  WADs, or extracted assets.
- Whether generated HL1 trigger/lights should extend `.gklevel` directly or be
  represented first as typed markers and sidecar import metadata.
- Whether actiongame should expose generated level selection through a CLI flag,
  environment variable, config file, or developer-only menu.

Classification:

- This is a long-term architecture step.
- A `.vox` bridge is tactical only. It can be used to compare the direct Gekko
  bake against `voxlife`, but should not shape the Gekko schema.
- The correct first implementation surface is a shared importer library plus a
  CLI. Editor UI should come after generated content validates, streams, and
  loads in `actiongame`.

Best-matching alternative:

- Prefer direct `BSP/WAD/entity -> HL1 import IR -> .gkworld/.gkchunk/.gklevel`.
- Avoid `BSP -> .vox -> Gekko`, except as a temporary debug path.
- Avoid storing whole maps as `.gkasset`; use `.gkasset` only for reusable props
  and gameplay prefabs.
- Avoid adding a general scripting language as the first answer to HL1
  behavior. Prefer typed actiongame systems for ladders, triggers, doors,
  lifts, trains, and pickups; revisit scripting only after those contracts are
  understood.

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
  - `LadderVolumes []LevelLadderVolumeDef`
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
- It does not perform actual next-level loading for `trigger_changelevel` yet.
- It maps only the implemented HL1 entity classes to actiongame behavior.
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
- Emit `level.player` with HL1 standing-hull dimensions so actiongame camera
  and collision match the imported map scale:
  - height: `72 HU = 1.8288m`
  - eye height: `64 HU = 1.6256m`
  - radius: `16 HU = 0.4064m`
  - step height: `18 HU = 0.4572m`
- Generate landmark and unsupported entity markers using new string `Kind`
  values, because `LevelMarkerDef.Kind` is already a string.
- Preserve richer entity data in the import report or sidecar metadata until
  `.gklevel` grows first-class trigger/light schema.

Phase 2 should extend `.gklevel` with first-class records if multiple consumers
need to inspect or edit the data:

- `Lights []LevelLightDef`
- `TriggerVolumes []LevelTriggerVolumeDef`
- `DamageVolumes []LevelDamageVolumeDef`
- `ChangeLevels []LevelChangeLevelDef`
- `Chargers []LevelChargerDef`
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

Implemented compact payload support keeps JSON available for fixtures and
debugging while allowing imported maps to use a binary RLE chunk payload:

- Keep `.gkchunk` extension.
- `sparse_json_v1` is the readable fallback/debug payload.
- `dense_rle_binary_v1` stores chunk-local voxels as dense linear value runs
  with a small metadata header and payload hash.
- `payload_kind`, `payload_hash`, and `payload_size_bytes` are recorded where
  available.
- Keep a debug dump/report path so failures remain inspectable.

### `.gkasset`

Do not use `.gkasset` for whole BSP maps.

Use `.gkasset` later for:

- Reusable props.
- NPC prefabs.
- Pickup prefabs.
- Doors, buttons, breakables, platforms, and scripted objects that need to stay
  separate from the static base-world bake.

## Proposed Package Layout

Prefer importer packages outside `content`:

```text
gekko/importers/common/
  ir.go
  material.go
  diagnostics.go
  emit_content.go
  report.go

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
  extract.go
  voxelize.go

gekko/cmd/hl1import/
  main.go
```

Rationale:

- `content` should remain the schema, IO, and validation package.
- `importers/common` owns source-neutral import IR, diagnostics, and Gekko
  content emission helpers.
- `importers/hl1` owns GoldSrc parsing, coordinate conversion, entity
  extraction, texture/material classification, and BSP-aware voxelization.
- Runtime code should not depend on HL1 parser internals.
- Editor and CLI can share the same importer package.
- A future Minecraft importer should reuse `importers/common` and emit the same
  Gekko content shapes without inheriting HL1-specific BSP logic.

If this repository avoids top-level importer packages, a reasonable fallback is
`gekko/content/importers/...`, but that is less clean because parser code is
source-format specific rather than Gekko content schema.

## Import IR

Create an internal IR before writing Gekko files. This prevents runtime schema
churn while the importer matures and lets non-HL1 importers share the Gekko
emission path.

Suggested core types:

```go
type ImportOptions struct {
    SourceKind          string
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
    Kind        string
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
    SolidKind  string
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

Source-specific adapters may add private fields while parsing, but those fields
should not leak into `content` packages. For example:

- HL1 may track BSP model id, face id, leaf classification, texture axes, and
  source entity keyvalues.
- Minecraft may track region/chunk coordinates, block state, biome, block
  light, sky light, and entity NBT.
- The common emitter should only need source kind, bounds, materials, voxels,
  lights, triggers, markers, environment, and diagnostics.

## Future Minecraft Import

The Minecraft importer should be planned as a sibling source adapter, not a
fork of the HL1 importer. It should reuse:

- common material and palette mapping
- `.gkworld` manifest emission
- `.gkchunk` payload emission
- generated `.gklevel` assembly
- import reports and diagnostics
- editor progress/report UI
- actiongame UAT level selection

It should not reuse:

- BSP parsing
- WAD texture lookup
- HL1 entity classification
- BSP solid reconstruction
- GoldSrc coordinate conversion

Minecraft-specific questions to defer until that importer starts:

- block-state to Gekko material mapping
- biome color and light preservation
- entity/NBT mapping
- chunk vertical range and scale policy
- whether authored Minecraft maps should preserve block grid scale exactly or
  be rescaled for actiongame movement

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

## Solid Reconstruction And Destruction Policy

The importer must not treat the HL1 map as a generic triangle mesh. Rasterizing
only visible faces produces hollow, paper-thin walls that look acceptable until
the first destructive edit exposes empty interiors. The goal is to reconstruct
the solid complement of the BSP:

```text
playable leaf/portal space -> empty voxels
world/brush solid space    -> filled voxels
visible textured surfaces  -> boundary color/material assignment
interior solid matter      -> inherited or fallback structural material
```

Recommended approach:

1. Use BSP/model data to classify voxel cells as empty or solid.
2. Use visible faces and texture coordinates to assign boundary voxel colors
   from original WAD/BSP texture pixels.
3. Flood-fill or leaf-classify reachable playable space so rooms, corridors,
   vents, and exterior sky voids stay empty.
4. Fill solid wall/floor/ceiling volume behind visible surfaces.
5. Assign interior material from nearest source surface, dominant local
   material, or a deterministic structural fallback.
6. Generate diagnostics for suspiciously thin features, giant solid masses,
   unclassified voids, and chunk hotspots.

Important constraints:

- Do not fill the playable void. "Fill empty space" means filling the original
  mesh's solid interior, not filling rooms.
- `SKY` faces should not become physical ceiling blocks unless the source BSP
  semantics indicate actual solid matter behind them.
- Trigger, clip, ladder, water, slime, lava, and monsterclip brush semantics are
  gameplay volumes or special materials, not ordinary visible structural
  geometry.
- Brush models used by doors, lifts, trains, buttons, and breakables should be
  preserved as source entities even if phase 1 bakes them into static geometry.

Phase 1 can prove this with a conservative CPU implementation on synthetic BSP
fixtures and one locally supplied small map. If CPU bake time becomes the
blocker, port or replace the `voxlife` GPU voxelizer after the solid
classification rules are already covered by tests.

Current phase-1 solid mode is tactical: it flood-fills reachable empty space
from player starts, then fills a bounded `CONTENTS_SOLID` band outward from
that playable space. This avoids GoldSrc outside-world solid leakage while
making walls/floors destructible. It is not full brush/CSG reconstruction; deep
solid mass beyond the band is intentionally omitted until brush-plane
reconstruction exists. If the playable empty flood would exceed the sample cap,
the importer falls back to a surface-guided band: each visible non-sky surface
seeds a bounded 6-neighbor flood through adjacent BSP-solid cells to the
configured depth. It does not march along the dominant face-normal axis. Visible
boundary voxels are produced by conservative triangle-vs-voxel overlap tests,
not projected face-center sampling, so inclined surfaces should not collapse
into axis-aligned ghost sheets and partial edge cells stay filled.

Destruction UAT requirement:

- Imported chunks must be editable through the same voxel geometry path used by
  existing destruction.
- A destructive hit against a wall/floor should expose filled solid material,
  not an empty shell.
- Disconnected fragments should either split into debris through existing
  destruction behavior or be explicitly reported as unsupported for imported
  base-world chunks.

## Material And Texture Policy

Required phase-1 behavior:

- Sample original WAD/BSP texture pixels per visible surface voxel using BSP
  texinfo axes, shifts, miptex dimensions, and GoldSrc texture wrapping.
- Quantize sampled texel colors into the imported-world palette and assign the
  resulting palette/material value to each boundary voxel.
- Use per-texture average colors only as fallback when source pixels are
  missing, unsupported, or intentionally disabled for diagnostics.
- Store palette RGBA in `ImportedWorldDef.Palette`.
- Store source texture/material details in the import report.
- Preserve source face id, texture name, WAD source, and texture-coordinate
  diagnostics in the importer report so bad UV alignment can be debugged.
- Propagate nearest visible-surface material into structural fill voxels so
  destructive edits expose plausible wall/floor material instead of arbitrary
  gray fill. This is still an approximation until brush/CSG material ownership
  is reconstructed.
- Classify obvious special textures:
  - `SKY` and `sky`: skipped from solid geometry and mapped to environment
    metadata.
  - Water/slime/lava texture names, plus GoldSrc `!` animated liquid textures:
    skipped from solid voxel chunks. BSP liquid leaves are preferred for
    generated `.gklevel` water body volume/depth; mostly horizontal liquid faces
    remain a fallback when leaf bounds are not useful. Imported solid voxels are
    also carved out of BSP liquid contents so renderer water is not hidden by
    stale surface voxels.
  - Transparent textures: preserve semantic metadata and transparency hints;
    imported-world voxels still render through current voxel material support.
  - Common material names are classified into semantic kinds such as `metal`,
    `glass`, `grate`, `concrete`, `wood`, `terrain`, `ladder`, `water`,
    `slime`, `lava`, `emissive`, and tool/trigger/clip classes.

Current material output:

- `.gkworld.materials` describes the runtime chunk palette used by baked voxel
  colors.
- `.gkworld.source_materials` preserves original HL1 texture provenance:
  texture name, source WAD, average color, material kind, collision kind,
  transparency, emissive hints, roughness, metallic, and semantic tags.
- Generated moving-brush, MDL, and SPR `.gkasset` materials receive matching
  authored material metadata where the importer can infer it.

Required follow-up if palette-only bake is visibly insufficient:

- Extend imported-world/chunk material storage only after the 255-color palette
  bake proves too lossy for close visual parity. Exact per-voxel source texture
  identity is not represented yet when baked color palette entries are shared by
  multiple original textures.

Palette risk:

- HL1 maps can have more meaningful texture/material classes than a uint8
  palette can represent.
- A 255-entry global palette may posterize full-map baked textures. Prefer
  deterministic quantization first; consider chunk-local palettes or larger
  material/color tables only after measuring real maps.
- Do not collapse material identity permanently into RGBA. Keep source material
  ids in IR and reports even if phase 1 chunks only store palette values.

## How To Use The Importer

There are two supported import paths:

- `gekko-editor` level editor UI. Use this for normal authoring and quick
  iteration.
- `gekko/cmd/hl1import` CLI. Use this for repeatable tests, automation, and
  debugging.

Both paths use the same importer package and generate the same content shape:

- `<map>.gklevel`
- `worlds/<map>.gkworld`
- `worlds/chunks/*.gkchunk`
- `worlds/<map>_import_report.json`
- generated helper `.gkasset` files for imported moving brush visuals
- optional `hl1_assets/<map>/manifest.gkhl1assets` plus copied source game
  assets referenced by the imported map

The generated `.gklevel` is the file to open or run. It references the base
world plus imported lights, water, ladders, moving brushes, use triggers, and
player spawn metadata.

### Editor Import

1. Start the editor:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko-editor
go run .
```

2. Switch to **Level Editor**.
3. Open the **Base World** dock.
4. Use **Import HL1 .bsp**.
5. Enter an absolute BSP path, for example:

```text
/Users/ddevidch/code/other/hl/valve/maps/crossfire.bsp
```

6. Keep the recommended defaults for first smoke tests:

- output root: `assets`
- voxel: `0.1` for world/base geometry
- asset voxel: `0.08` for generic imported `.mdl`/`.spr` assets
- item voxel: `0.04` for pickup/item assets
- chunk: `256`
- band: `24`
- cells: `100000000`
- chunk payload: `RLE chunks`
- light mode: `faithful`
- emit lights: `on`
- game assets: `off` unless you want a local copied/cataloged asset library

7. Click **import + open**.

`import + open` opens the generated `.gklevel` after import. Use plain
**import** when you only want to generate files and inspect the report later.

Turn **game assets** on when you want the importer to copy, catalog, and
partially convert HL1 source assets used by the map. The current asset pass
records:

- WAD files resolved from `worldspawn.wad`
- `.mdl` references from entity key-values
- `.spr` references from entity key-values
- `.wav` references from entity key-values

WADs are already used for BSP texture baking. MDL files are copied, parsed, and
voxelized into generated surface `.gkasset` files under
`hl1_assets/<map>/generated/models/`. When a GoldSrc model stores geometry in
`w_foo.mdl` and texture pixels in a companion `w_foot.mdl`, the importer loads
that companion texture model before baking voxel colors. SPR files are copied,
parsed, and converted from their first indexed frame into thin emissive voxel-card
`.gkasset` files under `hl1_assets/<map>/generated/sprites/`. The manifest
entries keep source provenance, decoded metadata, generated asset path, and
generated voxel count/resolution. Imported world geometry, generic game assets,
and pickup/item assets intentionally have separate voxel-resolution settings:
small pickups need finer voxels than BSP walls and floors. Generated model
assets currently use the default static pose and texture-baked surface voxels;
they are not solid-filled or animated yet. Generated sprite assets are not true
camera-facing billboards yet; they are placed voxel cards that preserve palette
color and cutout/additive transparency well enough for first visual coverage.
When **game assets** is enabled, typed pickups try to attach the generated HL1
world model asset directly to `LevelPickupDef.AssetPath`; actiongame uses that
model as the collectible visual and falls back to the colored placeholder cube
only when no generated pickup asset is available. Other entities whose `model`
key references a converted `.mdl` or `.spr` get normal
`LevelPlacementDef` entries pointing at those generated `.gkasset` files, using
the imported entity origin and yaw. Sound files are copied/cataloged only and
remain `convert_state: cataloged_source_only` until later passes wire sounds
without losing source provenance.

### CLI Import

Suggested command:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
go run ./cmd/hl1import \
  -game-dir /path/to/Half-Life \
  -map c1a0 \
  -out ../actiongame/assets/levels \
  -chunk-size 256 \
  -voxel-resolution 0.1 \
  -game-asset-voxel-resolution 0.08 \
  -pickup-voxel-resolution 0.04 \
  -light-mode faithful \
  -emit-light-fixtures=false \
  -emit-game-assets \
  -solid-band-depth 24 \
  -max-solid-sample-cells 100000000 \
  -emit-level \
  -debug-world-mode solid
```

The CLI defaults to compact RLE binary chunks. For readable JSON chunks, add:

```bash
-chunk-payload sparse_json_v1
```

For explicit binary chunks, add:

```bash
-chunk-payload dense_rle_binary_v1
```

Local developer smoke command:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
go run ./cmd/hl1import \
  -map crossfire \
  -bsp /Users/ddevidch/code/other/hl/valve/maps/crossfire.bsp \
  -out /tmp/gekko3d-hl1import-crossfire-rle \
  -chunk-size 256 \
  -voxel-resolution 0.1 \
  -light-mode faithful \
  -emit-light-fixtures=false \
  -solid-band-depth 24 \
  -max-solid-sample-cells 100000000 \
  -emit-level \
  -debug-world-mode solid
```

This writes:

```text
/tmp/gekko3d-hl1import-crossfire-rle/crossfire.gklevel
/tmp/gekko3d-hl1import-crossfire-rle/worlds/crossfire.gkworld
/tmp/gekko3d-hl1import-crossfire-rle/worlds/crossfire_import_report.json
/tmp/gekko3d-hl1import-crossfire-rle/worlds/chunks/*.gkchunk
```

### Actiongame Smoke Test

Run the generated level in `actiongame` with:

```bash
cd /Users/ddevidch/code/go/gekko3d/actiongame
GEKKO_ACTIONGAME_LEVEL=/tmp/gekko3d-hl1import-crossfire-rle/crossfire.gklevel \
GEKKO_ACTIONGAME_PLAYER_SPAWN_KIND=hl1_player_spawn \
go run .
```

For editor-generated levels under `gekko-editor/assets`, point
`GEKKO_ACTIONGAME_LEVEL` at that generated `.gklevel` instead.

Manual check list:

- player spawns at an HL1 `info_player_start`
- world scale matches HL1 doorways and ladders
- textures are baked onto voxel surfaces
- water, lights, ladders, doors, buttons, and lifts appear where expected
- chunk streaming does not introduce severe holes or stalls
- destructive edits work on imported world voxels

Current debug world modes:

- `surface`: visible BSP faces only. Useful for visual comparison and fast
  diagnostics, but hollow and not suitable for destruction.
- `solid`: BSP leaf contents classify voxel cells, visible faces provide
  boundary material, structural fill voxels make walls/floors destructive, and
  reachable empty space is flood-filled from player starts.
  Sampling bounds come from non-sky visible faces first, with model bounds only
  as fallback, then expanded by `-solid-band-depth`. Solid candidates are the
  `PointContents`-solid cells within that depth from reachable playable space,
  or from a bounded 6-neighbor flood seeded by visible surface voxels when
  playable flood is too large.

Temporary chunk-size guidance:

- Use `-chunk-size 256` for playable HL1 smoke testing at `0.1m` voxel
  resolution.
- Smaller chunks such as `32` are useful for importer/render diagnostics, but
  they create a chunk every `3.2m` and can make streamed runtime churn visible.
- This is a tactical bridge until sector/page streaming and no-hole LOD proxies
  are implemented in [`streaming-and-worlds.md`](streaming-and-worlds.md).

Light import modes:

- `faithful`: default. HL1 `light` becomes a point light. HL1 `light_spot`
  becomes a Gekko spot light using BSP entity `pitch`, `angle`, and `_cone`.
- `point-proxy`: diagnostic fallback. HL1 `light_spot` is emitted as a point
  light at the authored lamp position, preserving color/intensity/range but
  ignoring spot cone direction. Use this when validating runtime light upload
  or when spot direction conversion is suspected.

Light fixtures:

- `-emit-light-fixtures=false` is default for CLI imports because generic
  generated bulbs can look like visible floating markers when original fixture
  meshes/textures already exist in imported geometry.
- `-emit-light-fixtures=true` is an optional diagnostic/authoring bridge.
- Each non-ambient HL1 light gets a tiny generated emissive `.gkasset` under
  `assets/hl1_light_emitters/` plus a `.gklevel` placement at the imported
  light position.
- The generated emitter part and its paired `LevelLightDef` share
  `emitter_link_id`, so renderer shadow code can avoid lamp self-occlusion for
  the associated light.
- This is an authored-object bridge, not the material migration. Source texture
  material identity and per-voxel emissive material tables remain separate
  future work.

Solid mode has a sample guard:

```bash
-max-solid-sample-cells 20000000
```

Solid band depth is in voxels:

```bash
-solid-band-depth 24
```

At the default `0.1m` voxel resolution, `24` means about `2.4m` of destructible
solid behind reachable surfaces.

Suggested output:

```text
assets/levels/c1a0.gklevel
assets/levels/worlds/c1a0.gkworld
assets/levels/worlds/c1a0_import_report.json
assets/levels/worlds/chunks/c1a0_0_0_0.gkchunk
assets/levels/worlds/chunks/...
assets/levels/hl1_assets/c1a0/manifest.gkhl1assets
assets/levels/hl1_assets/c1a0/files/...
```

Import report fields:

- source map path and hash
- resolved WAD paths
- missing WADs/textures
- generated level path
- generated world path
- generated game asset manifest path is currently printed by the CLI and stored
  in `hl1_assets/<map>/manifest.gkhl1assets`, not embedded into `.gklevel`
- chunk count
- non-empty voxel count
- material count
- palette count
- skipped faces by reason
- converted entity counts by classname
- unsupported entity counts by classname
- interactive entity counts by classname: moving brushes, path nodes, ladders,
  chargers, pickups, triggers, breakables
- review diagnostics for incomplete imported behavior, including unsupported
  entity classes, missing moving-brush models, `func_train` without usable
  `path_corner` links, unresolved path-corner chains, unresolved trigger
  targets, and skipped `multi_manager` outputs
- coordinate bounds before and after conversion
- warnings for huge chunks or suspicious scale

## Generated Level Shape

Initial generated `.gklevel` should use existing schema:

```json
{
  "schema_version": 3,
  "name": "c1a0",
  "chunk_size": 256,
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
  "water_bodies": [
    {
      "id": "hl1_water_0",
      "name": "water",
      "mode": "ExplicitRect",
      "surface_y": 1.6,
      "depth": 1.2,
      "rect_half_extents": [4.0, 3.0],
      "transform": {
        "position": [12.0, 1.6, -8.0],
        "rotation": [0, 0, 0, 1],
        "scale": [1, 1, 1]
      },
      "tags": ["source:hl1", "liquid:water"]
    }
  ],
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

Water bodies are already first-class for phase 1. Connected liquid leaves are
used to recover total volume depth and surface footprint. A connected HL1 pool
should normally emit one explicit rectangular water body; solid pillars and
other imported geometry remain responsible for occlusion and collision inside
that rectangle. Multiple water bodies are reserved for disconnected liquid
components or different liquid kinds. Concave or masked liquid shapes may need
future renderer/schema support if rectangular water plus solid occluders is not
visually good enough.

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
- Add optional local integration tests that use real HL1 files only when an
  explicit environment variable points at a developer-owned install/copy.

Acceptance criteria:

- BSP version 30 loads.
- Unsupported BSP version is rejected.
- Lump bounds are validated.
- Entity parser handles representative quoted key/value records.
- WAD lookup is case-insensitive.
- Missing external WADs produce diagnostics, not panics.
- Normal `go test` does not require Steam/Half-Life files.
- Optional integration tests can read `/Users/ddevidch/code/other/hl` when
  enabled locally.

Tests:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1/...
```

Optional local integration test shape:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
GEKKO_HL1_GAME_DIR=/Users/ddevidch/code/other/hl \
  env GOCACHE=/tmp/gekko3d-gocache \
  go test ./importers/hl1/... -run HL1Integration
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
  `.vox`, using BSP-aware solid reconstruction rather than visible-surface-only
  voxelization.

Implementation:

- Add `voxelize.go`.
- Add `emit_content.go`.
- Start with a CPU reference solid classifier and voxelizer if it is fast enough
  for small maps.
- Port or replace the `voxlife` GPU voxelizer later if CPU bake is too slow.

Acceptance criteria:

- SKY faces are skipped from solid chunks.
- Playable leaf/room space remains empty.
- Walls/floors/ceilings have filled solid volume behind the visible boundary.
- Textured faces bake original WAD/BSP texel colors onto boundary voxels using
  BSP texture coordinates.
- Texture bake output is deterministic for the same BSP/WAD inputs.
- Interior solid voxels get deterministic material assignment from nearby
  visible source surfaces or a reported fallback.
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
- Emit `hl1_landmark` markers and typed `LevelDef.ChangeLevels` records for
  `trigger_changelevel`.
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
- Apply one destructive edit to a wall or floor and confirm the imported world
  is not a hollow shell.

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

### M5b: Typed Gameplay Volumes And Moving Brushes

Goal:

- Map the first non-static HL1 entities to explicit actiongame/Gekko behavior
  contracts without introducing a general scripting language prematurely.

Recommended path:

- Implemented: `func_ladder` emits `content.LevelDef.LadderVolumes`
  records. Streamed level runtime spawns them as `LadderVolumeComponent`
  entities, and the grounded player controller consumes those volumes for
  first-pass climbing.
- Implemented: `func_door`, `momentary_door`, `func_button`, and `func_plat`
  are excluded from the static base-world bake and emitted as generated
  moving-brush voxel assets referenced by `content.LevelDef.MovingBrushes`.
  `func_button` also emits `content.LevelDef.UseTriggers`. Streamed level
  runtime spawns `MovingBrushComponent` and `UseTriggerComponent`; the grounded
  player controller can activate them with E through `target`/`target_name`
  links, and moving-brush visuals move between closed/open positions.
- Implemented first slice: `func_door_rotating` is excluded from the static
  base-world bake, emitted as a generated moving-brush voxel asset, and runtime
  rotates it between closed/open angles around the imported origin when
  triggered. Deferred: exact GoldSrc spawnflag matrix, collision bounds during
  rotation, sounds, lock/master behavior, and nonstandard pivots that need
  visual review.
- Implemented first slice: `path_corner` point entities become typed
  `content.LevelDef.PathNodes`; `func_train` emits as a generated moving-brush
  voxel asset linked to its first path node and runtime moves it along the
  path-corner chain with basic speed/wait support. Deferred: path-corner
  spawnflags, stop/start sounds, blocking behavior, and exact restart semantics.
- Implemented: `func_breakable` is excluded from the static base-world bake and
  emitted as generated breakable voxel assets referenced by
  `content.LevelDef.Breakables`. Runtime spawns `BreakableComponent`, supports
  simple health/damage removal, honors the HL1 `Only Trigger` flag for weapon
  damage, spawns a typed pickup for supported `spawnobject` values, and fires
  the breakable's `target` through the same delayed target event path used by
  triggers and moving brushes.
- Implemented first slice: HL1 `weapon_*`, `ammo_*`, `item_healthkit`,
  `item_battery`, `item_suit`, `item_longjump`, and `weaponbox` entities are
  emitted as typed `content.LevelDef.Pickups`. Runtime spawns
  `PickupComponent`; when game assets are enabled, pickups reference generated
  HL1 world-model `.gkasset` visuals. Actiongame uses those visuals when
  present, falls back to simple collectible placeholders when missing, and
  tracks basic health, armor, ammo, and owned weapon state.
- Implemented first slice: `func_healthcharger` and `func_recharge` are emitted
  as typed `content.LevelDef.Chargers`. Runtime spawns `ChargerComponent`;
  actiongame charges health or armor while E is held and the camera ray hits the
  fixture, drains a finite reserve, and preserves targetname/spawnflag/source
  metadata. Defaults use the local HL1 `skill.cfg` easy values currently found
  in the copied game assets: health charger 50 and suit charger 75. Deferred:
  exact difficulty selection, recharge timing, sounds, animated empty/active
  visual states, and global-state behavior.
- `trigger_once` and `trigger_multiple` become typed trigger volumes with
  target metadata.
- Implemented first slice: `trigger_changelevel` is emitted as typed
  `content.LevelDef.ChangeLevels`. Runtime spawns `ChangeLevelVolumeComponent`;
  actiongame records the requested target map and landmark on player overlap and
  shows a transition HUD line, but does not load the next map yet. Deferred:
  exact landmark spawn handoff, change-target behavior, and next-level
  streaming/loading policy.
- Implemented first slice: `trigger_hurt` is emitted as typed
  `content.LevelDef.DamageVolumes`. Runtime spawns `DamageVolumeComponent`;
  actiongame applies overlap damage on a conservative Gekko cadence, can fire
  the imported target after the imported delay, and target graph dispatch can
  enable, disable, toggle, or kill named damage volumes. The
  importer preserves `dmg`, `delay`, `spawnflags`, `damagetype`, `target`, and
  `targetname` metadata. Deferred: exact GoldSrc damage-type effects, skill
  scaling, master/global-state gating, and every spawnflag nuance.
- Current transitional importer behavior emits source-linked level markers for
  `func_door`, `func_door_rotating`, `func_button`, `func_plat`, `func_train`,
  and `momentary_door`. The markers preserve brush bounds, model id, target,
  targetname, speed, wait, lip, angle/angles, spawnflags, sounds, and damage
  values in tags for compatibility with existing editor/actiongame tooling.
- Implemented: HL1 target graph entities `multi_manager` and `trigger_relay`
  are imported as typed runtime dispatchers. `multi_manager` queues delayed
  outputs, while `trigger_relay` forwards to `target`, preserves `killtarget`,
  `triggerstate`, `spawnflags`, and can remove itself after firing. This covers
  common button -> relay/manager -> mover chains without adding a general
  scripting language.
- Deferred: exact target-state semantics for every possible target class,
  `trigger_relay` master/global-state gating, and the remaining target graph
  relay/action entities.
- Deferred: exact HL1 respawn timing, exact difficulty/skill-convar amounts,
  breakable gibs/explosions, pickup animation/spin/bobbing, and full weapon
  switching/fire behavior.
- Unsupported scripted sequences remain diagnostics and debug markers.

Acceptance criteria:

- Ladder volumes are generated from HL1 brush-model bounds and can be
  debug-rendered or inspected as level-owned gameplay components.
- Changelevel trigger volumes preserve target map and landmark.
- Moving brush entities remain source-linked in diagnostics even before they
  are playable.
- The implementation does not require embedding HL1 script semantics in the
  renderer or content schema.

### M6: Compact Chunk Payloads

Goal:

- Make imported maps practical beyond one or two small maps.

Implementation:

- Implemented: `.gkchunk` loader auto-detects `dense_rle_binary_v1` binary
  chunks by magic header and otherwise falls back to JSON.
- Implemented: `.gkworld` and chunk entries can record chunk payload kind,
  payload hash, and payload byte size.
- Implemented: HL1 importer defaults to `dense_rle_binary_v1`; use
  `-chunk-payload sparse_json_v1` when readable chunk dumps are needed.
- Keep JSON support for fixtures and debugging.
- Future: add optional zstd wrapping after streaming/page behavior is stable.

Acceptance criteria:

- [x] Existing JSON chunks still load.
- [x] Binary RLE chunks validate and load through the same runtime path.
- [x] Runtime streaming works with both payload kinds.
- [ ] Import report records total uncompressed and compressed sizes.

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
6. `func_ladder`
7. pickups and simple props
8. doors/buttons/lifts/trains only after actiongame has matching systems
9. NPC spawn markers, then real NPC behavior later

Acceptance criteria:

- Unsupported classes remain visible in diagnostics.
- Imported behavior is opt-in and testable.
- No fake HL1 gameplay parity is implied before systems exist.
- A general scripting system is not required for the first imported-level UAT.

## Missing Capabilities Checklist

Engine/content:

- [ ] HL1 importer package.
- [ ] Import report schema.
- [ ] Required WAD/BSP texture bake onto visible voxel surfaces.
- [ ] Deterministic palette quantization for baked texture samples.
- [ ] Structural fill material propagation from nearest visible source surface.
- [ ] Optional `.gkworld` source material metadata.
- [x] Optional `.gkchunk` compact payload support.
- [ ] Optional `.gklevel` light definitions.
- [ ] Optional `.gklevel` trigger volume definitions.
- [ ] Richer `LevelEnvironmentDef`.

Runtime:

- [ ] Load selected generated HL1 level in `actiongame`.
- [ ] Spawn/import level-owned lights.
- [ ] Apply imported player spawn rotation.
- [x] Represent changelevel trigger volumes as typed transition metadata.
- [x] Represent ladder volumes.
- [x] Represent typed door/button moving-brush metadata and use activation.
- [x] Spawn visual moving brush geometry for linear doors/buttons/platforms
      outside the static base-world bake.
- [x] Import HL1 target graph relays for `multi_manager`, `trigger_relay`, and
      delayed target firing.
- [x] Import `trigger_hurt` as typed damage volumes with basic actiongame
      overlap damage and target on/off/toggle support.
- [x] Import `func_healthcharger` and `func_recharge` as typed chargers with
      basic actiongame hold-to-use health/armor charging.
- [x] Spawn/import `func_breakable` gameplay entities with generated voxel
      assets and simple health/target behavior.
- [x] Spawn/import pickups, ammo, health, and weapons as typed pickup
      components with actiongame placeholder collection.
- [x] Attach generated HL1 world-model pickup visuals when game assets are
      enabled, with placeholder fallback for missing assets.
- [ ] Import remaining target graph relay/action entities.
- [ ] Implement exact HL1 pickup respawn/skill behavior and animated pickup
      presentation.
- [x] Spawn visual moving brush geometry for rotating doors and path-following
      trains with first-pass runtime motion semantics.
- [ ] Implement exact rotating-door flags/collision/sounds and full
      path-corner train semantics.
- [ ] Confirm imported base-world chunks can be destructively edited or
      explicitly gate destruction with a documented policy.
- [ ] Debug render unsupported imported entities.
- [ ] Validate imported collision scale visually.

Editor:

- [x] HL1 import panel in `gekko-editor` level base-world dock.
- [x] Coarse import progress for BSP read, world voxelization, level build,
      save, and report phases.
- [x] Import report path and generated content summary display.
- [ ] Import cancellation.
- [ ] Missing WAD/texture diagnostics.
- [ ] Unsupported entity diagnostics.
- [ ] Imported light/trigger inspection after schema exists.

## Verification Matrix

Parser/importer:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
env GOCACHE=/tmp/gekko3d-gocache go test ./importers/hl1/...
```

Optional local real-file importer checks:

```bash
cd /Users/ddevidch/code/go/gekko3d/gekko
GEKKO_HL1_GAME_DIR=/Users/ddevidch/code/other/hl \
  env GOCACHE=/tmp/gekko3d-gocache \
  go test ./importers/hl1/... -run HL1Integration
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
- Compare distinctive HL1 floor/wall textures against the original map or
  reference screenshots; flat average-color surfaces do not satisfy visual
  parity.
- Confirm baked texture alignment on floors, vertical walls, and inclined
  surfaces.
- Confirm palette quantization does not collapse most of the map into gray or
  one dominant material.
- Confirm collision.
- Confirm a wall/floor destructive edit exposes filled matter.
- Confirm point light placement if imported lights are enabled.
- Confirm no severe chunk-boundary artifacts.
- Confirm streaming radius loads/unloads chunks without stalls.

Manual scale-up checks:

- Import one outdoor/sky map.
- Import one map with changelevel triggers.
- Import one map with dense texture/material variety.
- Import one map with repeated tiled textures and verify tile scale/orientation
  against HL1.
- Compare report voxel counts, chunk hotspots, and file sizes before broad
  campaign import.

## Risk Log

- JSON chunk size may become the largest blocker for full campaign imports.
- Palette-only chunk material storage can still lose exact per-voxel HL1
  texture identity, but `.gkworld.source_materials` preserves source material
  provenance and semantic classification for later runtime use.
- Texture bake is required for visual parity; average-color materials are a
  fallback only and should be treated as visually incomplete.
- Global 255-color palette quantization may be too lossy for some maps and may
  force chunk-local palettes or larger imported-world material storage.
- Wrong BSP texinfo axis/shift/wrap handling will make textures look worse than
  flat colors because alignment errors are obvious on tiled HL1 surfaces.
- CPU voxelization may be too slow; GPU voxelization may be harder to test
  deterministically.
- Missing WADs and case-sensitive path differences must be reported clearly.
- `SKY`, transparent, water, slime, lava, ladder, clip, and trigger textures
  need special handling.
- Surface-only voxelization will create hollow shells; BSP-aware solid
  classification must be verified before destruction is considered supported.
- Trigger brush semantics are not the same as visible geometry.
- Moving brush models may be baked static in phase 1 unless the importer
  preserves their source ids for later behavior extraction.
- Collision scale mistakes will make imported maps feel wrong even when they
  look correct.
- Imported lighting may need renderer/runtime work before it looks like HL1.
- A premature general scripting system could couple source-game behavior too
  tightly to the engine. Typed gameplay components should come first.
- HL1 assets/maps are copyrighted. Do not commit original BSP, WAD, texture, or
  extracted asset data to this repository. Real files are acceptable for local
  manual UAT and opt-in integration tests that point at a developer-owned
  install/copy.

## Questions To Resolve Before Code Implementation

These are the alignment questions that should be answered before schema/runtime
changes, but they do not block maintaining this planning doc.

- Should imported HL1 world geometry be read-only by default?
- Is the first playable target geometry-only, or geometry plus
  spawns/lights/changelevel triggers?
- Should destruction be enabled for all imported base-world chunks in the first
  UAT, or should the UAT use a deliberately destructible subset?
- Should compact `.gkchunk` payloads be implemented before the first real map?
- Should `.gklevel` get first-class lights/triggers immediately, or should the
  first importer version use markers plus report metadata?
- Should the CLI live at `gekko/cmd/hl1import`?
- What generated level selection mechanism should `actiongame` use?
- Which small synthetic fixtures should be generated for always-on parser,
  coordinate, WAD, and voxelization tests?

## Recommended First Slice

Implement the first slice in this order:

1. Add `gekko/importers/common` and `gekko/importers/hl1` with parser-only
   tests.
2. Add entity extraction and coordinate conversion tests.
3. Add a CLI that emits an import report without writing Gekko content.
4. Add BSP-aware direct `.gkworld`/`.gkchunk` emission for a synthetic fixture.
5. Add `.gklevel` emission with `BaseWorld` and `player_spawn`.
6. Load the generated level with `StartStreamedLevelRuntime(...)`.
7. Add `actiongame` level selection.
8. Run the first manual actiongame UAT, including one destructive edit.
9. Only then add editor UI.

This sequence keeps the architecture aligned with existing Gekko content
formats while avoiding a short-lived MagicaVoxel-centered importer.
