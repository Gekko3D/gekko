package common

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gekko3d/gekko/content"
	contentderived "github.com/gekko3d/gekko/content/derived"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func saveImportedWorldEmissionChunk(path string, chunk *content.ImportedWorldChunkDef, kind string, codec *voxelcodec.Codec) (content.ImportedWorldChunkSaveResult, error) {
	if kind == content.ImportedWorldChunkPayloadBrickZstdBinaryV1 {
		return content.SaveImportedWorldChunkCompiledWithCodec(path, chunk, codec)
	}
	return content.SaveImportedWorldChunkWithOptionsResult(path, chunk, content.ImportedWorldChunkSaveOptions{PayloadKind: kind})
}

type embeddedImportedChunkPlan struct {
	chunk, fitting    *content.ImportedWorldChunkDef
	hash              string
	size              int
	previousAux       *content.ImportedWorldChunkAuxDef
	geometryUnchanged bool
}

func planEmbeddedImportedChunk(chunk *content.ImportedWorldChunkDef, priorPath string, codec *voxelcodec.Codec) (embeddedImportedChunkPlan, error) {
	hash, size, err := content.ImportedWorldChunkCompiledGeometryIdentity(chunk, codec)
	if err != nil {
		return embeddedImportedChunkPlan{}, err
	}
	// Geometry identity is nonmutating; fitting uses the same actual occupied
	// count that an ordinary save historically normalized before baking.
	fitting := *chunk
	fitting.NonEmptyVoxelCount = 0
	for _, voxel := range chunk.Voxels {
		if voxel.Value != 0 {
			fitting.NonEmptyVoxelCount++
		}
	}
	plan := embeddedImportedChunkPlan{chunk: chunk, fitting: &fitting, hash: hash, size: size}
	if priorPath != "" {
		previous, err := content.LoadImportedWorldChunkWithCodec(priorPath, codec)
		if err == nil {
			oldHash, oldSize, identityErr := content.ImportedWorldChunkCompiledGeometryIdentity(previous, codec)
			plan.geometryUnchanged = identityErr == nil && oldHash == hash && oldSize == size
			if plan.geometryUnchanged {
				plan.previousAux = previous.EmbeddedAux
			}
		}
	}
	return plan, nil
}

func saveEmbeddedImportedWorldEmission(manifestPath string, emission ImportedWorldEmission, opts ImportedWorldSaveOptions) (ImportedWorldSaveStats, error) {
	var stats ImportedWorldSaveStats
	if emission.Manifest.SchemaVersion != 0 && emission.Manifest.SchemaVersion != content.CurrentImportedWorldSchemaVersion {
		return stats, fmt.Errorf("unsupported imported world schema version %d", emission.Manifest.SchemaVersion)
	}
	manifestDir := filepath.Dir(manifestPath)
	previous := loadPreviousImportedWorldManifest(manifestPath)
	previousEntries := previousImportedWorldEntriesByPath(previous)
	previousLODs := previousImportedWorldLODsByPath(previous)
	plans := make(map[content.TerrainChunkCoordDef]embeddedImportedChunkPlan, len(emission.Manifest.Entries))
	currentPaths := make(map[string]content.TerrainChunkCoordDef, len(emission.Manifest.Entries))
	changed := make(map[content.TerrainChunkCoordDef]bool, len(emission.Manifest.Entries))
	fittingChunks := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(emission.Manifest.Entries))
	// Read prior data and compute all current geometry identities before any
	// frame writes, so old files cannot be mistaken for already rewritten ones.
	for _, entry := range emission.Manifest.Entries {
		chunk := emission.Chunks[[3]int{entry.Coord.X, entry.Coord.Y, entry.Coord.Z}]
		if chunk == nil {
			return stats, fmt.Errorf("missing chunk for coord %v", entry.Coord)
		}
		priorPath := ""
		prior := previousEntries[entry.ChunkPath]
		if prior != nil && prior.Coord == entry.Coord {
			priorPath = content.ResolveImportedWorldChunkPath(*prior, manifestPath)
		}
		plan, err := planEmbeddedImportedChunk(chunk, priorPath, opts.ChunkCodec)
		if err != nil {
			return stats, err
		}
		plans[entry.Coord], fittingChunks[entry.Coord] = plan, plan.fitting
		currentPaths[entry.ChunkPath] = entry.Coord
		changed[entry.Coord] = !plan.geometryUnchanged
	}
	for path, prior := range previousEntries {
		if coord, exists := currentPaths[path]; !exists || coord != prior.Coord {
			changed[prior.Coord] = true
		}
	}
	proxyPlans := make(map[string]embeddedImportedChunkPlan, len(emission.ProxyChunks))
	proxyPaths := make([]string, 0, len(emission.ProxyChunks))
	for path, chunk := range emission.ProxyChunks {
		priorPath := ""
		if prior := previousLODs[path]; prior != nil {
			priorPath = content.ResolveDocumentPath(prior.ChunkPath, manifestPath)
		}
		plan, err := planEmbeddedImportedChunk(chunk, priorPath, opts.ChunkCodec)
		if err != nil {
			return stats, err
		}
		proxyPlans[path] = plan
		proxyPaths = append(proxyPaths, path)
	}
	sort.Strings(proxyPaths)
	emission.Manifest.ChunkPayloadKind = content.ImportedWorldChunkPayloadBrickZstdBinaryV1
	for i := range emission.Manifest.Entries {
		entry := &emission.Manifest.Entries[i]
		plan := plans[entry.Coord]
		aux := plan.previousAux
		if aux != nil && !embeddedImportedNeighborhoodChanged(entry.Coord, changed) {
			stats.ChunkAuxReused++
		} else {
			aux = contentderived.BuildImportedWorldChunkAux(plan.fitting, embeddedImportedNeighbors(entry.Coord, fittingChunks), plan.hash, plan.size, true)
		}
		result, err := content.SaveImportedWorldChunkCompiledWithAux(filepath.Join(manifestDir, filepath.FromSlash(entry.ChunkPath)), plan.chunk, aux, opts.ChunkCodec)
		if err != nil {
			return stats, err
		}
		if result.Wrote {
			stats.ChunksWritten++
		} else {
			stats.ChunksSkipped++
		}
		entry.PayloadKind, entry.PayloadHash, entry.PayloadSizeBytes = result.PayloadKind, result.PayloadHash, result.PayloadSizeBytes
		entry.NonEmptyVoxelCount, entry.Aux = plan.chunk.NonEmptyVoxelCount, nil
	}
	for _, path := range proxyPaths {
		plan := proxyPlans[path]
		aux := plan.previousAux
		if aux != nil {
			stats.ProxyAuxReused++
		} else {
			aux = contentderived.BuildImportedWorldChunkAux(plan.fitting, nil, plan.hash, plan.size, false)
		}
		result, err := content.SaveImportedWorldChunkCompiledWithAux(filepath.Join(manifestDir, filepath.FromSlash(path)), plan.chunk, aux, opts.ChunkCodec)
		if err != nil {
			return stats, err
		}
		if result.Wrote {
			stats.ProxyChunksWritten++
		} else {
			stats.ProxyChunksSkipped++
		}
		updateImportedWorldSectorLODMetadata(emission.Manifest.Sectors, path, plan.chunk)
		updateImportedWorldSectorLODAuxMetadata(emission.Manifest.Sectors, path, nil)
	}
	// Preserve file times for the entire equivalent embedded emission, including
	// its small owner manifest, without changing legacy SaveImportedWorld behavior.
	content.EnsureImportedWorldDefaults(emission.Manifest)
	manifestBytes, err := json.MarshalIndent(emission.Manifest, "", "  ")
	if err != nil {
		return stats, err
	}
	if previousBytes, err := os.ReadFile(manifestPath); err == nil && bytes.Equal(previousBytes, manifestBytes) {
		return stats, nil
	}
	return stats, content.SaveImportedWorld(manifestPath, emission.Manifest)
}

// Offset only immediate neighbors without overflowing the caller's int lattice.
func embeddedImportedNeighborCoord(coord content.TerrainChunkCoordDef, x, y, z int) (content.TerrainChunkCoordDef, bool) {
	values, deltas := [3]int{coord.X, coord.Y, coord.Z}, [3]int{x, y, z}
	maxInt := int(^uint(0) >> 1)
	for axis, delta := range deltas {
		if delta < 0 && values[axis] == -maxInt-1 || delta > 0 && values[axis] == maxInt {
			return content.TerrainChunkCoordDef{}, false
		}
		values[axis] += delta
	}
	return content.TerrainChunkCoordDef{X: values[0], Y: values[1], Z: values[2]}, true
}

func embeddedImportedNeighborhoodChanged(coord content.TerrainChunkCoordDef, changed map[content.TerrainChunkCoordDef]bool) bool {
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if neighbor, valid := embeddedImportedNeighborCoord(coord, x, y, z); valid && changed[neighbor] {
					return true
				}
			}
		}
	}
	return false
}

func embeddedImportedNeighbors(coord content.TerrainChunkCoordDef, chunks map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef) map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef {
	neighbors := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, 27)
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if neighbor, valid := embeddedImportedNeighborCoord(coord, x, y, z); valid && chunks[neighbor] != nil {
					neighbors[neighbor] = chunks[neighbor]
				}
			}
		}
	}
	return neighbors
}
