package hl1

import (
	"fmt"
	"path/filepath"

	"github.com/gekko3d/gekko/content"
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
	Emission     importcommon.ImportedWorldEmission
	Voxelize     VoxelizeResult
	Mode         DebugWorldMode
	PayloadKind  string
}

func BuildDebugSurfaceWorld(opts ImportOptions) (DebugWorldEmissionResult, error) {
	return BuildDebugWorld(opts, DebugWorldModeSurface)
}

func BuildDebugSolidWorld(opts ImportOptions) (DebugWorldEmissionResult, error) {
	return BuildDebugWorld(opts, DebugWorldModeSolid)
}

func BuildDebugWorld(opts ImportOptions, mode DebugWorldMode) (DebugWorldEmissionResult, error) {
	if opts.ChunkPayloadKind == "" {
		opts.ChunkPayloadKind = DefaultChunkPayloadKind
	}
	if _, err := content.NormalizeImportedWorldChunkPayloadKind(opts.ChunkPayloadKind); err != nil {
		return DebugWorldEmissionResult{}, err
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
			VoxelResolution: opts.VoxelResolution,
			TextureStore:    textureStore,
			MaterialColors:  materialColors,
		})
	case DebugWorldModeSolid:
		sourceBuildVersion = DebugSolidVoxelBuildVersion
		tags = []string{"source:hl1", "debug:solid_voxel"}
		voxelized, err = VoxelizeBSPSolidCPU(bsp, faces, summary.Map.Entities, VoxelizeOptions{
			VoxelResolution:     opts.VoxelResolution,
			MaxSolidSampleCells: opts.MaxSolidSampleCells,
			SolidBandDepth:      opts.SolidBandDepth,
			TextureStore:        textureStore,
			MaterialColors:      materialColors,
		})
		if err != nil {
			return DebugWorldEmissionResult{}, err
		}
	default:
		return DebugWorldEmissionResult{}, fmt.Errorf("unsupported debug world mode %q", mode)
	}
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
	ApplyHL1SectorVisibility(emission.Manifest, bsp)
	manifestPath := generatedWorldPath(opts)
	if manifestPath == "" {
		return DebugWorldEmissionResult{}, fmt.Errorf("output root and map name are required for debug world emission")
	}
	return DebugWorldEmissionResult{
		ManifestPath: filepath.Clean(manifestPath),
		Emission:     emission,
		Voxelize:     voxelized,
		Mode:         mode,
		PayloadKind:  opts.ChunkPayloadKind,
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
	payloadKind := result.PayloadKind
	if payloadKind == "" {
		payloadKind = DefaultChunkPayloadKind
	}
	return importcommon.SaveImportedWorldEmissionWithOptions(result.ManifestPath, result.Emission, importcommon.ImportedWorldSaveOptions{
		ChunkPayloadKind: payloadKind,
	})
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
