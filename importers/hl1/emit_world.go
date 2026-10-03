package hl1

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	importcommon "github.com/gekko3d/gekko/importers/common"
)

const DebugSurfaceVoxelBuildVersion = "hl1_debug_surface_voxel_v1"
const DebugSolidVoxelBuildVersion = "hl1_debug_solid_voxel_v1"

type DebugWorldMode string

const (
	DebugWorldModeSurface DebugWorldMode = "surface"
	DebugWorldModeSolid   DebugWorldMode = "solid"
)

type DebugWorldEmissionResult struct {
	ManifestPath string
	BackingPath  string
	Backing      *content.VoxelBackingDef
	Emission     importcommon.ImportedWorldEmission
	Voxelize     VoxelizeResult
	Mode         DebugWorldMode
	PayloadKind  string
	EmbedNormals bool
	ChunkCodec   *voxelcodec.Codec
	Diagnostics  []importcommon.Diagnostic
}

func BuildDebugSurfaceWorld(opts ImportOptions) (DebugWorldEmissionResult, error) {
	return BuildDebugWorld(opts, DebugWorldModeSurface)
}

func BuildDebugSolidWorld(opts ImportOptions) (DebugWorldEmissionResult, error) {
	return BuildDebugWorld(opts, DebugWorldModeSolid)
}

func BuildDebugWorld(opts ImportOptions, mode DebugWorldMode) (DebugWorldEmissionResult, error) {
	var profileErr error
	opts, profileErr = ApplyHL1ExportProfile(opts)
	if profileErr != nil {
		return DebugWorldEmissionResult{}, profileErr
	}
	if opts.ChunkPayloadKind == "" {
		opts.ChunkPayloadKind = DefaultChunkPayloadKind
	}
	if _, err := content.NormalizeImportedWorldChunkPayloadKind(opts.ChunkPayloadKind); err != nil {
		return DebugWorldEmissionResult{}, err
	}
	if opts.EmbedNormals && opts.ChunkPayloadKind != content.ImportedWorldChunkPayloadBrickZstdBinaryV1 {
		return DebugWorldEmissionResult{}, fmt.Errorf("embedded normals require brick_zstd_binary_v1")
	}
	summary, err := BuildImportSummary(opts)
	if err != nil {
		return DebugWorldEmissionResult{}, err
	}
	bsp, err := LoadBSP(summary.Report.Source.BSPPath)
	if err != nil {
		return DebugWorldEmissionResult{}, err
	}
	faces, err := bsp.WorldFaces()
	if err != nil {
		return DebugWorldEmissionResult{}, err
	}
	if len(summary.BakeFaces) > 0 {
		faces = summary.BakeFaces
	}
	liquidFaces := summary.AllFaces
	if len(liquidFaces) == 0 {
		liquidFaces = faces
	}
	wads, wadDiagnostics := LoadResolvedWADs(summary.Report.Source.WADPaths)
	if len(wadDiagnostics) > 0 {
		summary.Report.Diagnostics = append(summary.Report.Diagnostics, wadDiagnostics...)
	}
	textureStore := NewTextureStore(bsp.Textures, wads)
	materialColors := materialColorMap(summary.Map.Materials)
	var voxelized VoxelizeResult
	sourceBuildVersion := DebugSurfaceVoxelBuildVersion
	tags := []string{"source:hl1", "debug:surface_voxel"}
	switch mode {
	case "", DebugWorldModeSurface:
		mode = DebugWorldModeSurface
		voxelized = VoxelizeFacesCPU(faces, VoxelizeOptions{
			VoxelResolution:     opts.VoxelResolution,
			TextureStore:        textureStore,
			LightingData:        bsp.LightingData,
			BakeStaticLightmaps: opts.BakeStaticLightmaps,
			MaterialColors:      materialColors,
		})
	case DebugWorldModeSolid:
		sourceBuildVersion = DebugSolidVoxelBuildVersion
		tags = []string{"source:hl1", "debug:solid_voxel"}
		voxelized, err = VoxelizeBSPSolidCPU(bsp, faces, summary.Map.Entities, VoxelizeOptions{
			VoxelResolution:     opts.VoxelResolution,
			MaxSolidSampleCells: opts.MaxSolidSampleCells,
			SolidBandDepth:      opts.SolidBandDepth,
			TextureStore:        textureStore,
			LightingData:        bsp.LightingData,
			BakeStaticLightmaps: opts.BakeStaticLightmaps,
			MaterialColors:      materialColors,
		})
		if err != nil {
			return DebugWorldEmissionResult{}, err
		}
	default:
		return DebugWorldEmissionResult{}, fmt.Errorf("unsupported debug world mode %q", mode)
	}
	// BakeFaces deliberately excludes liquid geometry from the solid world, but
	// the level emitter still needs its top-surface occupancy for water patches.
	voxelized.LiquidTopCells = collectLiquidTopCells(bsp, liquidFaces, VoxelizeOptions{VoxelResolution: opts.VoxelResolution})
	tags = append(tags, HL1ExportProfileTags(opts.ExportProfile)...)
	worldID := summary.Report.Source.MapName
	if worldID == "" {
		worldID = "hl1_debug_world"
	}
	materials := summary.Map.Materials
	if len(voxelized.Materials) > 0 {
		materials = voxelized.Materials
	}
	emission, err := importcommon.BuildImportedWorldEmission(voxelized.Voxels, materials, importcommon.ImportedWorldEmitOptions{
		WorldID:            worldID,
		ChunkSize:          opts.ChunkSize,
		VoxelResolution:    opts.VoxelResolution,
		ChunkDirectoryName: "chunks",
		SourceBuildVersion: sourceBuildVersion,
		SourceHash:         summary.Report.Source.BSPHash,
		SourceMaterials:    summary.Map.Materials,
		Tags:               tags,
	})
	if err != nil {
		return DebugWorldEmissionResult{}, err
	}
	backing, err := buildDebugWorldVoxelBacking(bsp, emission.Manifest, opts.VoxelResolution, faces, summary.Map.Entities)
	if err != nil {
		return DebugWorldEmissionResult{}, err
	}
	if backing != nil {
		ensureDebugWorldBackingChunks(&emission, backing, tags)
	}
	animationDiagnostics := ApplyHL1MaterialAnimations(emission.Manifest, textureStore)
	animationDiagnostics = append(animationDiagnostics, ApplyHL1ScrollMaterialAnimations(emission.Manifest, textureStore)...)
	ApplyHL1SectorVisibility(emission.Manifest, bsp)
	manifestPath := generatedWorldPath(opts)
	if manifestPath == "" {
		return DebugWorldEmissionResult{}, fmt.Errorf("output root and map name are required for debug world emission")
	}
	backingPath := ""
	if backing != nil {
		backingName := strings.TrimSuffix(filepath.Base(manifestPath), filepath.Ext(manifestPath)) + ".gkvoxelbacking"
		backingPath = filepath.Join(filepath.Dir(manifestPath), backingName)
		emission.Manifest.Backing = &content.VoxelBackingRefDef{
			Path:       filepath.ToSlash(backingName),
			Kind:       backing.Kind,
			SourceHash: backing.SourceHash,
			BoundsMin:  backing.BoundsMin,
			BoundsMax:  backing.BoundsMax,
		}
	}
	return DebugWorldEmissionResult{
		ManifestPath: filepath.Clean(manifestPath),
		BackingPath:  filepath.Clean(backingPath),
		Backing:      backing,
		Emission:     emission,
		Voxelize:     voxelized,
		Mode:         mode,
		PayloadKind:  opts.ChunkPayloadKind,
		EmbedNormals: opts.EmbedNormals,
		ChunkCodec:   opts.ChunkCodec,
		Diagnostics:  animationDiagnostics,
	}, nil
}

func materialColorMap(materials []importcommon.Material) map[int][4]uint8 {
	out := make(map[int][4]uint8, len(materials))
	for _, material := range materials {
		if material.ID <= 0 || material.BaseColor == ([4]uint8{}) {
			continue
		}
		out[material.ID] = material.BaseColor
	}
	return out
}

func SaveDebugSurfaceWorld(result DebugWorldEmissionResult) error {
	return SaveDebugWorld(result)
}

func SaveDebugWorld(result DebugWorldEmissionResult) error {
	_, err := SaveDebugWorldWithStats(result)
	return err
}

func SaveDebugWorldWithStats(result DebugWorldEmissionResult) (importcommon.ImportedWorldSaveStats, error) {
	payloadKind := result.PayloadKind
	if payloadKind == "" {
		payloadKind = DefaultChunkPayloadKind
	}
	if result.EmbedNormals && payloadKind != content.ImportedWorldChunkPayloadBrickZstdBinaryV1 {
		return importcommon.ImportedWorldSaveStats{}, fmt.Errorf("embedded normals require brick_zstd_binary_v1")
	}
	if result.Backing != nil {
		backingPath := result.BackingPath
		if backingPath == "" {
			return importcommon.ImportedWorldSaveStats{}, fmt.Errorf("voxel backing path is empty")
		}
		if err := content.SaveVoxelBacking(backingPath, result.Backing); err != nil {
			return importcommon.ImportedWorldSaveStats{}, err
		}
	}
	stats, err := importcommon.SaveImportedWorldEmissionWithOptionsResult(result.ManifestPath, result.Emission, importcommon.ImportedWorldSaveOptions{
		ChunkPayloadKind: payloadKind,
		EmbedNormals:     result.EmbedNormals,
		ChunkCodec:       result.ChunkCodec,
	})
	if err != nil {
		return stats, err
	}
	if validation := content.ValidateImportedWorld(result.Emission.Manifest, content.ImportedWorldValidationOptions{DocumentPath: result.ManifestPath}); validation.HasErrors() {
		return stats, fmt.Errorf("validate emitted imported world: %s", validation.Error())
	}
	return stats, nil
}

func buildDebugWorldVoxelBacking(bsp *BSP, manifest *content.ImportedWorldDef, voxelResolution float32, surfaceFaces []Face, entities []importcommon.Entity) (*content.VoxelBackingDef, error) {
	if bsp == nil || len(bsp.Models) == 0 {
		return nil, fmt.Errorf("world BSP model is missing")
	}
	if manifest == nil || voxelResolution <= 0 {
		return nil, fmt.Errorf("imported world manifest metrics are invalid")
	}
	// Some importer unit fixtures intentionally contain only renderable faces.
	// Such a BSP has no solid classifier, so backing remains optional.
	if len(bsp.Planes) == 0 || len(bsp.Nodes) == 0 || len(bsp.Leafs) == 0 {
		return nil, nil
	}
	bounds := HammerBoundsToGekko(bsp.Models[0].Min, bsp.Models[0].Max)
	def := &content.VoxelBackingDef{
		SchemaVersion: content.CurrentVoxelBackingSchemaVersion,
		Kind:          content.VoxelBackingKindPlaneTreeV1,
		SourceHash:    bsp.SHA256,
		BoundsMin: [3]int{
			int(math.Floor(float64(bounds.Min.X / voxelResolution))),
			int(math.Floor(float64(bounds.Min.Y / voxelResolution))),
			int(math.Floor(float64(bounds.Min.Z / voxelResolution))),
		},
		BoundsMax: [3]int{
			int(math.Ceil(float64(bounds.Max.X / voxelResolution))),
			int(math.Ceil(float64(bounds.Max.Y / voxelResolution))),
			int(math.Ceil(float64(bounds.Max.Z / voxelResolution))),
		},
		SolidValue: debugWorldBackingSolidValue(manifest),
		PlaneTree: &content.VoxelBackingPlaneTreeDef{
			Root: int32(bsp.Models[0].HeadNodes[0]),
		},
	}
	if def.BoundsMax[0] <= def.BoundsMin[0] || def.BoundsMax[1] <= def.BoundsMin[1] || def.BoundsMax[2] <= def.BoundsMin[2] {
		if len(manifest.Entries) == 0 {
			return nil, fmt.Errorf("world BSP and imported chunks have no usable voxel backing bounds")
		}
		first := manifest.Entries[0].Coord
		def.BoundsMin = [3]int{first.X * manifest.ChunkSize, first.Y * manifest.ChunkSize, first.Z * manifest.ChunkSize}
		def.BoundsMax = [3]int{(first.X + 1) * manifest.ChunkSize, (first.Y + 1) * manifest.ChunkSize, (first.Z + 1) * manifest.ChunkSize}
		for _, entry := range manifest.Entries[1:] {
			coord := [3]int{entry.Coord.X, entry.Coord.Y, entry.Coord.Z}
			for axis := 0; axis < 3; axis++ {
				def.BoundsMin[axis] = min(def.BoundsMin[axis], coord[axis]*manifest.ChunkSize)
				def.BoundsMax[axis] = max(def.BoundsMax[axis], (coord[axis]+1)*manifest.ChunkSize)
			}
		}
	}
	for _, plane := range bsp.Planes {
		def.PlaneTree.Planes = append(def.PlaneTree.Planes, content.VoxelBackingPlaneDef{
			Normal:   [3]float32{plane.Normal.X, plane.Normal.Z, -plane.Normal.Y},
			Distance: plane.Dist * HammerUnitMeters / voxelResolution,
		})
	}
	for _, node := range bsp.Nodes {
		def.PlaneTree.Nodes = append(def.PlaneTree.Nodes, content.VoxelBackingPlaneNodeDef{
			Plane:    node.PlaneID,
			Children: [2]int32{int32(node.Children[0]), int32(node.Children[1])},
		})
	}
	for _, leaf := range bsp.Leafs {
		def.PlaneTree.Leaves = append(def.PlaneTree.Leaves, content.VoxelBackingPlaneLeafDef{Solid: IsSolidContent(leaf.Contents)})
	}
	modelClasses := brushClassByModelID(entities)
	for modelID, model := range bsp.Models {
		if modelID != 0 && !visibleBrushEntityClass(modelClasses[modelID]) {
			continue
		}
		bounds := HammerBoundsToGekko(model.Min, model.Max)
		volume := content.VoxelBackingPlaneVolumeDef{
			Root: model.HeadNodes[0],
			BoundsMin: [3]int{
				int(math.Floor(float64(bounds.Min.X / voxelResolution))),
				int(math.Floor(float64(bounds.Min.Y / voxelResolution))),
				int(math.Floor(float64(bounds.Min.Z / voxelResolution))),
			},
			BoundsMax: [3]int{
				int(math.Ceil(float64(bounds.Max.X / voxelResolution))),
				int(math.Ceil(float64(bounds.Max.Y / voxelResolution))),
				int(math.Ceil(float64(bounds.Max.Z / voxelResolution))),
			},
		}
		validBounds := true
		for axis := 0; axis < 3; axis++ {
			validBounds = validBounds && volume.BoundsMax[axis] > volume.BoundsMin[axis]
		}
		if validBounds {
			def.PlaneTree.Volumes = append(def.PlaneTree.Volumes, volume)
		}
	}
	def.SurfaceSupports = buildDebugWorldSurfaceSupports(surfaceFaces, voxelResolution)
	if err := content.ValidateVoxelBacking(def); err != nil {
		return nil, err
	}
	return def, nil
}

const DefaultDestructionSurfaceSupportDepth = float32(1.5)

func buildDebugWorldSurfaceSupports(faces []Face, voxelResolution float32) []content.VoxelBackingSurfaceSupportDef {
	if voxelResolution <= 0 {
		return nil
	}
	depth := DefaultDestructionSurfaceSupportDepth / voxelResolution
	supports := make([]content.VoxelBackingSurfaceSupportDef, 0, len(faces)*2)
	for _, face := range faces {
		semantics := materialSemantics(face.TextureName)
		if len(face.Vertices) < 3 || semantics.CollisionKind != "solid" || semantics.Transparent || isCutoutTexture(face.TextureName) {
			continue
		}
		normal := hammerVectorToGekko(face.Normal)
		normalLength := float32(math.Sqrt(float64(dotVec3(normal, normal))))
		if normalLength <= 1e-6 {
			continue
		}
		normal = importcommon.Vec3{X: normal.X / normalLength, Y: normal.Y / normalLength, Z: normal.Z / normalLength}
		if normal.Y < 0.5 {
			continue
		}
		vertices := make([]importcommon.Vec3, len(face.Vertices))
		for i, vertex := range face.Vertices {
			world := HammerToGekko(vertex)
			vertices[i] = importcommon.Vec3{X: world.X / voxelResolution, Y: world.Y / voxelResolution, Z: world.Z / voxelResolution}
		}
		for i := 1; i < len(vertices)-1; i++ {
			triangle := [3]importcommon.Vec3{vertices[0], vertices[i], vertices[i+1]}
			triangleNormal := crossVec3(subVec3(triangle[1], triangle[0]), subVec3(triangle[2], triangle[0]))
			if dotVec3(triangleNormal, triangleNormal) <= 1e-8 {
				continue
			}
			supports = append(supports, content.VoxelBackingSurfaceSupportDef{
				Vertices: [3][3]float32{
					{triangle[0].X, triangle[0].Y, triangle[0].Z},
					{triangle[1].X, triangle[1].Y, triangle[1].Z},
					{triangle[2].X, triangle[2].Y, triangle[2].Z},
				},
				Normal: [3]float32{normal.X, normal.Y, normal.Z},
				Depth:  depth,
			})
		}
	}
	return supports
}

func debugWorldBackingSolidValue(manifest *content.ImportedWorldDef) uint8 {
	for _, material := range manifest.Materials {
		if material.PaletteIndex != 0 && material.CollisionKind == "solid" && !material.Transparent && (material.Kind == "structural" || material.Kind == "structural_fill") {
			return material.PaletteIndex
		}
	}
	for _, material := range manifest.Materials {
		if material.PaletteIndex != 0 && material.CollisionKind == "solid" && !material.Transparent {
			return material.PaletteIndex
		}
	}
	for _, material := range manifest.Materials {
		if material.PaletteIndex != 0 && !material.Transparent && !material.EmitsLight && material.AnimationID == "" && material.Kind != "emissive" && material.Kind != "glass" && material.Kind != "grate" && material.Kind != "ladder" && material.Kind != "cutout" {
			return material.PaletteIndex
		}
	}
	return 1
}

func ensureDebugWorldBackingChunks(emission *importcommon.ImportedWorldEmission, backing *content.VoxelBackingDef, tags []string) {
	if emission == nil || emission.Manifest == nil || backing == nil || emission.Manifest.ChunkSize <= 0 {
		return
	}
	chunkSize := emission.Manifest.ChunkSize
	minCoord := [3]int{
		floorDivDebugWorld(backing.BoundsMin[0], chunkSize),
		floorDivDebugWorld(backing.BoundsMin[1], chunkSize),
		floorDivDebugWorld(backing.BoundsMin[2], chunkSize),
	}
	maxCoord := [3]int{
		floorDivDebugWorld(backing.BoundsMax[0]-1, chunkSize),
		floorDivDebugWorld(backing.BoundsMax[1]-1, chunkSize),
		floorDivDebugWorld(backing.BoundsMax[2]-1, chunkSize),
	}
	existing := make(map[content.TerrainChunkCoordDef]struct{}, len(emission.Manifest.Entries))
	for _, entry := range emission.Manifest.Entries {
		existing[entry.Coord] = struct{}{}
	}
	for x := minCoord[0]; x <= maxCoord[0]; x++ {
		for y := minCoord[1]; y <= maxCoord[1]; y++ {
			for z := minCoord[2]; z <= maxCoord[2]; z++ {
				coord := content.TerrainChunkCoordDef{X: x, Y: y, Z: z}
				if _, ok := existing[coord]; ok {
					continue
				}
				chunk := &content.ImportedWorldChunkDef{
					WorldID:         emission.Manifest.WorldID,
					SchemaVersion:   content.CurrentImportedWorldChunkSchemaVersion,
					Coord:           coord,
					ChunkSize:       chunkSize,
					VoxelResolution: emission.Manifest.VoxelResolution,
					Tags:            append(append([]string(nil), tags...), "backing_only"),
				}
				emission.Chunks[[3]int{x, y, z}] = chunk
				emission.Manifest.Entries = append(emission.Manifest.Entries, content.ImportedWorldChunkEntryDef{
					Coord:     coord,
					ChunkPath: filepath.ToSlash(filepath.Join("chunks", fmt.Sprintf("%s_%d_%d_%d.gkchunk", emission.Manifest.WorldID, x, y, z))),
					Tags:      append([]string(nil), chunk.Tags...),
				})
				existing[coord] = struct{}{}
			}
		}
	}
	sort.Slice(emission.Manifest.Entries, func(i, j int) bool {
		a, b := emission.Manifest.Entries[i].Coord, emission.Manifest.Entries[j].Coord
		if a.X != b.X {
			return a.X < b.X
		}
		if a.Y != b.Y {
			return a.Y < b.Y
		}
		return a.Z < b.Z
	})
	chunksByCoord := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(emission.Chunks))
	for _, chunk := range emission.Chunks {
		chunksByCoord[chunk.Coord] = chunk
	}
	sectors := content.BuildImportedWorldSectors(emission.Manifest.Entries, chunkSize, emission.Manifest.VoxelResolution, content.DefaultImportedWorldSectorTargetWorldSize)
	emission.Manifest.Sectors, emission.ProxyChunks = content.BuildImportedWorldSectorProxyChunks(sectors, chunksByCoord, content.ImportedWorldSectorProxyOptions{
		WorldID:         emission.Manifest.WorldID,
		ChunkSize:       chunkSize,
		VoxelResolution: emission.Manifest.VoxelResolution,
		Tags:            tags,
	})
}

func floorDivDebugWorld(value, divisor int) int {
	quotient := value / divisor
	if value < 0 && value%divisor != 0 {
		quotient--
	}
	return quotient
}

func firstStructuralMaterialID(materials []importcommon.Material) int {
	for _, material := range materials {
		if material.ID > 0 && material.Kind == "structural" && material.CollisionKind == "solid" {
			return material.ID
		}
	}
	for _, material := range materials {
		if material.ID > 0 && material.CollisionKind == "solid" {
			return material.ID
		}
	}
	return 1
}
