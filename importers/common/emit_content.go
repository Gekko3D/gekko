package common

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/gekko3d/gekko/content"
)

type ImportedWorldEmitOptions struct {
	WorldID            string
	ChunkSize          int
	VoxelResolution    float32
	ChunkDirectoryName string
	SourceBuildVersion string
	SourceHash         string
	SourceMaterials    []Material
	Tags               []string
}

type ImportedWorldSaveOptions struct {
	ChunkPayloadKind string
}

type ImportedWorldEmission struct {
	Manifest        *content.ImportedWorldDef
	Chunks          map[[3]int]*content.ImportedWorldChunkDef
	ProxyChunks     map[string]*content.ImportedWorldChunkDef
	TotalVoxelCount int
}

func BuildImportedWorldEmission(voxels []Voxel, materials []Material, opts ImportedWorldEmitOptions) (ImportedWorldEmission, error) {
	if opts.WorldID == "" {
		return ImportedWorldEmission{}, fmt.Errorf("world id is empty")
	}
	if opts.ChunkSize <= 0 {
		return ImportedWorldEmission{}, fmt.Errorf("chunk size must be positive")
	}
	if opts.VoxelResolution <= 0 {
		return ImportedWorldEmission{}, fmt.Errorf("voxel resolution must be positive")
	}
	if opts.ChunkDirectoryName == "" {
		opts.ChunkDirectoryName = "chunks"
	}
	materializedVoxels, materialPalette, runtimeMaterials := importedWorldMaterializedVoxels(voxels, materials)
	chunks := make(map[[3]int]*content.ImportedWorldChunkDef)
	for _, voxel := range materializedVoxels {
		if voxel.Palette == 0 {
			continue
		}
		coord := [3]int{
			floorDiv(voxel.X, opts.ChunkSize),
			floorDiv(voxel.Y, opts.ChunkSize),
			floorDiv(voxel.Z, opts.ChunkSize),
		}
		chunk := chunks[coord]
		if chunk == nil {
			chunk = &content.ImportedWorldChunkDef{
				WorldID:         opts.WorldID,
				SchemaVersion:   content.CurrentImportedWorldChunkSchemaVersion,
				Coord:           content.TerrainChunkCoordDef{X: coord[0], Y: coord[1], Z: coord[2]},
				ChunkSize:       opts.ChunkSize,
				VoxelResolution: opts.VoxelResolution,
				Tags:            append([]string(nil), opts.Tags...),
			}
			chunks[coord] = chunk
		}
		outVoxel := content.ImportedWorldVoxelDef{
			X:             voxel.X - coord[0]*opts.ChunkSize,
			Y:             voxel.Y - coord[1]*opts.ChunkSize,
			Z:             voxel.Z - coord[2]*opts.ChunkSize,
			Value:         voxel.Palette,
			MaterialValue: voxel.MaterialValue,
		}
		if outVoxel.MaterialValue == outVoxel.Value {
			outVoxel.MaterialValue = 0
		}
		chunk.Voxels = append(chunk.Voxels, outVoxel)
	}
	coords := sortedChunkCoords(chunks)
	entries := make([]content.ImportedWorldChunkEntryDef, 0, len(coords))
	total := 0
	for _, coord := range coords {
		chunk := chunks[coord]
		sort.Slice(chunk.Voxels, func(i, j int) bool {
			if chunk.Voxels[i].X != chunk.Voxels[j].X {
				return chunk.Voxels[i].X < chunk.Voxels[j].X
			}
			if chunk.Voxels[i].Y != chunk.Voxels[j].Y {
				return chunk.Voxels[i].Y < chunk.Voxels[j].Y
			}
			return chunk.Voxels[i].Z < chunk.Voxels[j].Z
		})
		chunk.NonEmptyVoxelCount = len(chunk.Voxels)
		total += chunk.NonEmptyVoxelCount
		entries = append(entries, content.ImportedWorldChunkEntryDef{
			Coord:              chunk.Coord,
			ChunkPath:          filepath.ToSlash(filepath.Join(opts.ChunkDirectoryName, fmt.Sprintf("%s_%d_%d_%d.gkchunk", opts.WorldID, coord[0], coord[1], coord[2]))),
			NonEmptyVoxelCount: chunk.NonEmptyVoxelCount,
			Tags:               append([]string(nil), opts.Tags...),
		})
	}
	chunksByCoord := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(chunks))
	for coord, chunk := range chunks {
		chunksByCoord[content.TerrainChunkCoordDef{X: coord[0], Y: coord[1], Z: coord[2]}] = chunk
	}
	sectors := content.BuildImportedWorldSectors(entries, opts.ChunkSize, opts.VoxelResolution, content.DefaultImportedWorldSectorTargetWorldSize)
	sectors, proxyChunks := content.BuildImportedWorldSectorProxyChunks(sectors, chunksByCoord, content.ImportedWorldSectorProxyOptions{
		WorldID:         opts.WorldID,
		ChunkSize:       opts.ChunkSize,
		VoxelResolution: opts.VoxelResolution,
		Tags:            opts.Tags,
	})

	return ImportedWorldEmission{
		Manifest: &content.ImportedWorldDef{
			WorldID:            opts.WorldID,
			SchemaVersion:      content.CurrentImportedWorldSchemaVersion,
			Kind:               content.ImportedWorldKindVoxelWorld,
			ChunkSize:          opts.ChunkSize,
			VoxelResolution:    opts.VoxelResolution,
			Palette:            paletteFromMaterials(materials),
			MaterialPalette:    materialPalette,
			Materials:          runtimeMaterials,
			SourceMaterials:    importedWorldMaterialsFromMaterials(opts.SourceMaterials),
			SourceBuildVersion: opts.SourceBuildVersion,
			SourceHash:         opts.SourceHash,
			Tags:               append([]string(nil), opts.Tags...),
			Entries:            entries,
			Sectors:            sectors,
		},
		Chunks:          chunks,
		ProxyChunks:     proxyChunks,
		TotalVoxelCount: total,
	}, nil
}

func SaveImportedWorldEmission(manifestPath string, emission ImportedWorldEmission) error {
	return SaveImportedWorldEmissionWithOptions(manifestPath, emission, ImportedWorldSaveOptions{
		ChunkPayloadKind: content.ImportedWorldChunkPayloadSparseJSONV1,
	})
}

func SaveImportedWorldEmissionWithOptions(manifestPath string, emission ImportedWorldEmission, opts ImportedWorldSaveOptions) error {
	if emission.Manifest == nil {
		return fmt.Errorf("manifest is nil")
	}
	payloadKind, err := content.NormalizeImportedWorldChunkPayloadKind(opts.ChunkPayloadKind)
	if err != nil {
		return err
	}
	emission.Manifest.ChunkPayloadKind = payloadKind
	if payloadKind == content.ImportedWorldChunkPayloadDenseRLEBinaryV1 && importedWorldEmissionHasMaterialValues(emission) {
		emission.Manifest.ChunkPayloadKind = content.ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1
	}
	manifestDir := filepath.Dir(manifestPath)
	for i := range emission.Manifest.Entries {
		entry := &emission.Manifest.Entries[i]
		coord := [3]int{entry.Coord.X, entry.Coord.Y, entry.Coord.Z}
		chunk := emission.Chunks[coord]
		if chunk == nil {
			return fmt.Errorf("missing chunk for coord %v", coord)
		}
		if err := content.SaveImportedWorldChunkWithOptions(filepath.Join(manifestDir, filepath.FromSlash(entry.ChunkPath)), chunk, content.ImportedWorldChunkSaveOptions{
			PayloadKind: payloadKind,
		}); err != nil {
			return err
		}
		entry.PayloadKind = chunk.PayloadKind
		entry.PayloadHash = chunk.PayloadHash
		entry.PayloadSizeBytes = chunk.PayloadSizeBytes
	}
	for path, chunk := range emission.ProxyChunks {
		if err := content.SaveImportedWorldChunkWithOptions(filepath.Join(manifestDir, filepath.FromSlash(path)), chunk, content.ImportedWorldChunkSaveOptions{
			PayloadKind: payloadKind,
		}); err != nil {
			return err
		}
		updateImportedWorldSectorLODMetadata(emission.Manifest.Sectors, path, chunk)
	}
	return content.SaveImportedWorld(manifestPath, emission.Manifest)
}

func importedWorldEmissionHasMaterialValues(emission ImportedWorldEmission) bool {
	for _, chunk := range emission.Chunks {
		for _, voxel := range chunk.Voxels {
			if voxel.Value != 0 && voxel.MaterialValue != 0 && voxel.MaterialValue != voxel.Value {
				return true
			}
		}
	}
	for _, chunk := range emission.ProxyChunks {
		for _, voxel := range chunk.Voxels {
			if voxel.Value != 0 && voxel.MaterialValue != 0 && voxel.MaterialValue != voxel.Value {
				return true
			}
		}
	}
	return false
}

func updateImportedWorldSectorLODMetadata(sectors []content.ImportedWorldSectorDef, path string, chunk *content.ImportedWorldChunkDef) {
	for i := range sectors {
		for j := range sectors[i].LODs {
			lod := &sectors[i].LODs[j]
			if lod.ChunkPath != path {
				continue
			}
			lod.PayloadKind = chunk.PayloadKind
			lod.PayloadHash = chunk.PayloadHash
			lod.PayloadSizeBytes = chunk.PayloadSizeBytes
			lod.NonEmptyVoxelCount = chunk.NonEmptyVoxelCount
			return
		}
	}
}

func paletteFromMaterials(materials []Material) []content.ImportedWorldPaletteColor {
	palette := make([]content.ImportedWorldPaletteColor, 256)
	palette[1] = content.ImportedWorldPaletteColor{180, 180, 180, 255}
	for i, material := range materials {
		index := int(material.PaletteIndex)
		if index <= 0 {
			index = i + 1
		}
		if index <= 0 || index >= len(palette) {
			continue
		}
		color := material.BaseColor
		if color == ([4]uint8{}) {
			color = [4]uint8{180, 180, 180, 255}
		}
		palette[index] = content.ImportedWorldPaletteColor{color[0], color[1], color[2], color[3]}
	}
	return palette
}

func importedWorldMaterialsFromMaterials(materials []Material) []content.ImportedWorldMaterialDef {
	out := make([]content.ImportedWorldMaterialDef, 0, len(materials))
	for i, material := range materials {
		index := int(material.PaletteIndex)
		if index <= 0 {
			index = i + 1
		}
		if index <= 0 || index > 255 {
			continue
		}
		out = append(out, content.ImportedWorldMaterialDef{
			ID:                material.ID,
			PaletteIndex:      uint8(index),
			SourceTextureName: material.SourceTextureName,
			BaseColor:         content.ImportedWorldPaletteColor{material.BaseColor[0], material.BaseColor[1], material.BaseColor[2], material.BaseColor[3]},
			Kind:              material.Kind,
			CollisionKind:     material.CollisionKind,
			Transparent:       material.Transparent,
			EmitsLight:        material.EmitsLight,
			Emissive:          material.Emissive,
			Roughness:         material.Roughness,
			Metallic:          material.Metallic,
			Transparency:      material.Transparency,
			SourceWAD:         material.SourceWAD,
			Size:              material.Size,
			Tags:              append([]string(nil), material.Tags...),
		})
	}
	return out
}

type importedWorldMaterialKey struct {
	ColorPalette uint8
	Kind         string
	Transparent  bool
	EmitsLight   bool
}

func importedWorldMaterializedVoxels(voxels []Voxel, colorMaterials []Material) ([]Voxel, []content.ImportedWorldPaletteColor, []content.ImportedWorldMaterialDef) {
	colorPalette := paletteFromMaterials(colorMaterials)
	materialByPalette := map[uint8]Material{}
	for _, material := range colorMaterials {
		index := material.PaletteIndex
		if index == 0 && material.ID > 0 && material.ID <= 255 {
			index = uint8(material.ID)
		}
		if index != 0 {
			materialByPalette[index] = material
		}
	}
	materialPalette := make([]content.ImportedWorldPaletteColor, 256)
	materials := make([]content.ImportedWorldMaterialDef, 0)
	valuesByKey := map[importedWorldMaterialKey]uint8{}
	out := make([]Voxel, 0, len(voxels))
	nextValue := uint8(1)
	for _, voxel := range voxels {
		if voxel.Palette == 0 {
			continue
		}
		key := importedWorldMaterialKeyForVoxel(voxel, materialByPalette[voxel.Palette])
		value, ok := valuesByKey[key]
		if !ok {
			if nextValue == 0 {
				voxel.MaterialValue = voxel.Palette
				out = append(out, voxel)
				continue
			}
			value = nextValue
			nextValue++
			valuesByKey[key] = value
			source := materialByPalette[voxel.Palette]
			material := importedWorldRuntimeMaterialForVoxel(value, voxel, source, colorPalette)
			materialPalette[value] = material.BaseColor
			materials = append(materials, material)
		}
		voxel.MaterialValue = value
		out = append(out, voxel)
	}
	return out, materialPalette, materials
}

func importedWorldMaterialKeyForVoxel(voxel Voxel, source Material) importedWorldMaterialKey {
	kind := source.Kind
	transparent := source.Transparent
	emitsLight := source.EmitsLight
	switch voxel.SolidKind {
	case "glass", "transparent", "grate", "ladder":
		kind = voxel.SolidKind
		transparent = true
	case "emissive":
		emitsLight = true
	}
	return importedWorldMaterialKey{
		ColorPalette: voxel.Palette,
		Kind:         kind,
		Transparent:  transparent,
		EmitsLight:   emitsLight,
	}
}

func importedWorldRuntimeMaterialForVoxel(value uint8, voxel Voxel, source Material, colorPalette []content.ImportedWorldPaletteColor) content.ImportedWorldMaterialDef {
	color := content.ImportedWorldPaletteColor{180, 180, 180, 255}
	if int(voxel.Palette) < len(colorPalette) && colorPalette[voxel.Palette] != (content.ImportedWorldPaletteColor{}) {
		color = colorPalette[voxel.Palette]
	}
	material := content.ImportedWorldMaterialDef{
		ID:                int(value),
		PaletteIndex:      value,
		SourceTextureName: source.SourceTextureName,
		BaseColor:         color,
		Kind:              source.Kind,
		CollisionKind:     source.CollisionKind,
		Transparent:       source.Transparent,
		EmitsLight:        source.EmitsLight,
		Emissive:          source.Emissive,
		Roughness:         source.Roughness,
		Metallic:          source.Metallic,
		Transparency:      source.Transparency,
		SourceWAD:         source.SourceWAD,
		Size:              source.Size,
		Tags:              append([]string(nil), source.Tags...),
	}
	if material.Kind == "" {
		material.Kind = "baked_texture"
	}
	if material.Roughness == 0 {
		material.Roughness = 0.9
	}
	switch voxel.SolidKind {
	case "glass":
		material.Kind = "glass"
		material.Transparent = true
		material.Transparency = maxFloat32(material.Transparency, 0.55)
		material.Roughness = 0.08
		material.Tags = appendUniqueString(material.Tags, "material:glass")
	case "transparent":
		material.Kind = "transparent"
		material.Transparent = true
		material.Transparency = maxFloat32(material.Transparency, 0.45)
		material.Tags = appendUniqueString(material.Tags, "material:transparent")
	case "grate":
		material.Kind = "grate"
		material.Transparent = true
		material.Transparency = maxFloat32(material.Transparency, 0.35)
		material.Metallic = maxFloat32(material.Metallic, 0.65)
		material.Roughness = 0.55
		material.Tags = appendUniqueString(material.Tags, "material:cutout")
	case "ladder":
		material.Kind = "ladder"
		material.Transparent = true
		material.Transparency = maxFloat32(material.Transparency, 0.35)
		material.Tags = appendUniqueString(material.Tags, "material:ladder")
	case "emissive":
		material.EmitsLight = true
		if material.Emissive <= 0 {
			material.Emissive = 2.0
		}
		material.Tags = appendUniqueString(material.Tags, "material:emissive")
	}
	return material
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func sortedChunkCoords(chunks map[[3]int]*content.ImportedWorldChunkDef) [][3]int {
	coords := make([][3]int, 0, len(chunks))
	for coord := range chunks {
		coords = append(coords, coord)
	}
	sort.Slice(coords, func(i, j int) bool {
		if coords[i][0] != coords[j][0] {
			return coords[i][0] < coords[j][0]
		}
		if coords[i][1] != coords[j][1] {
			return coords[i][1] < coords[j][1]
		}
		return coords[i][2] < coords[j][2]
	})
	return coords
}

func floorDiv(value int, divisor int) int {
	if value >= 0 {
		return value / divisor
	}
	return -((-value + divisor - 1) / divisor)
}
