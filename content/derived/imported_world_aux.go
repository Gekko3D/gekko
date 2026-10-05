package derived

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func EnsureImportedWorldAuxSidecarsForManifest(manifestPath string) error {
	return EnsureImportedWorldAuxSidecarsForManifestWithCodec(manifestPath, nil)
}

// EnsureImportedWorldAuxSidecarsForManifestWithCodec borrows the fixed compiled
// profile. Validated embedded normals satisfy the requirement without sidecars.
func EnsureImportedWorldAuxSidecarsForManifestWithCodec(manifestPath string, codec *voxelcodec.Codec) error {
	manifestPath = strings.TrimSpace(manifestPath)
	if manifestPath == "" {
		return fmt.Errorf("imported-world manifest path is empty")
	}
	manifest, err := content.LoadImportedWorld(manifestPath)
	if err != nil {
		return err
	}
	if manifest.SchemaVersion == 3 {
		return fmt.Errorf("immutable v3 pages require the page bake publisher")
	}
	if importedWorldManifestAuxRefsCurrent(manifestPath, manifest) {
		return nil
	}

	observedEmbedded, changed := false, false
	chunksByCoord := make(map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if entry.NonEmptyVoxelCount == 0 {
			continue
		}
		chunk, err := content.LoadImportedWorldChunkWithCodec(content.ResolveImportedWorldChunkPath(entry, manifestPath), codec)
		if err != nil {
			return err
		}
		chunksByCoord[entry.Coord] = chunk
	}

	for i := range manifest.Entries {
		entry := &manifest.Entries[i]
		chunk := chunksByCoord[entry.Coord]
		if chunk == nil || chunk.NonEmptyVoxelCount == 0 {
			continue
		}
		if chunk.EmbeddedAux != nil {
			observedEmbedded = true
			continue
		}
		sourceHash := firstNonEmptyString(entry.PayloadHash, chunk.PayloadHash)
		sourceSize := firstPositiveInt(entry.PayloadSizeBytes, chunk.PayloadSizeBytes)
		if importedWorldAuxRefCurrent(manifestPath, entry.Aux, sourceHash, sourceSize) {
			continue
		}
		aux := BuildImportedWorldChunkAux(chunk, chunksByCoord, sourceHash, sourceSize, true)
		if aux == nil {
			continue
		}
		auxPath := content.DefaultImportedWorldChunkAuxPath(entry.ChunkPath)
		if err := content.SaveImportedWorldChunkAux(content.ResolveDocumentPath(auxPath, manifestPath), aux); err != nil {
			return err
		}
		entry.Aux = content.ImportedWorldChunkAuxRef(auxPath, aux)
		changed = true
	}

	for i := range manifest.Sectors {
		for j := range manifest.Sectors[i].LODs {
			lod := &manifest.Sectors[i].LODs[j]
			if strings.TrimSpace(lod.ChunkPath) == "" || lod.NonEmptyVoxelCount == 0 {
				continue
			}
			proxyPath := content.ResolveDocumentPath(lod.ChunkPath, manifestPath)
			if importedWorldAuxRefCurrent(manifestPath, lod.Aux, lod.PayloadHash, lod.PayloadSizeBytes) {
				continue
			}
			chunk, err := content.LoadImportedWorldChunkWithCodec(proxyPath, codec)
			if err != nil {
				return err
			}
			if chunk.EmbeddedAux != nil {
				observedEmbedded = true
				continue
			}
			sourceHash := firstNonEmptyString(lod.PayloadHash, chunk.PayloadHash)
			sourceSize := firstPositiveInt(lod.PayloadSizeBytes, chunk.PayloadSizeBytes)
			aux := BuildImportedWorldChunkAux(chunk, map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef{chunk.Coord: chunk}, sourceHash, sourceSize, false)
			if aux == nil {
				continue
			}
			auxPath := content.DefaultImportedWorldChunkAuxPath(lod.ChunkPath)
			if err := content.SaveImportedWorldChunkAux(filepath.Join(filepath.Dir(manifestPath), filepath.FromSlash(auxPath)), aux); err != nil {
				return err
			}
			lod.Aux = content.ImportedWorldChunkAuxRef(auxPath, aux)
			changed = true
		}
	}
	if observedEmbedded && !changed {
		return nil
	}
	return content.SaveImportedWorld(manifestPath, manifest)
}

func importedWorldManifestAuxRefsCurrent(manifestPath string, manifest *content.ImportedWorldDef) bool {
	if manifest == nil {
		return false
	}
	for _, entry := range manifest.Entries {
		if entry.NonEmptyVoxelCount == 0 {
			continue
		}
		if !importedWorldAuxRefCurrent(manifestPath, entry.Aux, entry.PayloadHash, entry.PayloadSizeBytes) {
			return false
		}
	}
	for _, sector := range manifest.Sectors {
		for _, lod := range sector.LODs {
			if strings.TrimSpace(lod.ChunkPath) == "" || lod.NonEmptyVoxelCount == 0 {
				continue
			}
			if !importedWorldAuxRefCurrent(manifestPath, lod.Aux, lod.PayloadHash, lod.PayloadSizeBytes) {
				return false
			}
		}
	}
	return true
}

func importedWorldAuxRefCurrent(manifestPath string, ref *content.ImportedWorldChunkAuxRefDef, sourcePayloadHash string, sourcePayloadSizeBytes int) bool {
	if ref == nil || strings.TrimSpace(ref.AuxPath) == "" {
		return false
	}
	aux, err := content.LoadImportedWorldChunkAux(content.ResolveDocumentPath(ref.AuxPath, manifestPath))
	if err != nil || aux == nil {
		return false
	}
	if ref.PayloadHash != "" && aux.PayloadHash != ref.PayloadHash {
		return false
	}
	if aux.NormalBakeVersion != content.ImportedWorldNormalBakeVersion {
		return false
	}
	if sourcePayloadHash != "" && aux.SourcePayloadHash != sourcePayloadHash {
		return false
	}
	if sourcePayloadSizeBytes > 0 && aux.SourcePayloadSizeBytes != sourcePayloadSizeBytes {
		return false
	}
	return true
}

func BuildImportedWorldChunkAux(chunk *content.ImportedWorldChunkDef, chunksByCoord map[content.TerrainChunkCoordDef]*content.ImportedWorldChunkDef, sourcePayloadHash string, sourcePayloadSizeBytes int, sampleNeighbors bool) *content.ImportedWorldChunkAuxDef {
	if chunk == nil || chunk.NonEmptyVoxelCount == 0 {
		return nil
	}
	xbm := importedWorldChunkToXBrickMap(chunk)
	minB, maxB := xbm.ComputeAABB()
	aux := &content.ImportedWorldChunkAuxDef{
		WorldID:                chunk.WorldID,
		SchemaVersion:          content.CurrentImportedWorldChunkAuxSchemaVersion,
		Coord:                  chunk.Coord,
		ChunkSize:              chunk.ChunkSize,
		VoxelResolution:        chunk.VoxelResolution,
		NormalBakeVersion:      content.ImportedWorldNormalBakeVersion,
		SourcePayloadHash:      sourcePayloadHash,
		SourcePayloadSizeBytes: sourcePayloadSizeBytes,
	}
	content.EnsureImportedWorldChunkAuxDefaults(aux)

	chunkMaps := map[content.TerrainChunkCoordDef]*volume.XBrickMap{chunk.Coord: xbm}
	if sampleNeighbors {
		for coord, neighbor := range chunksByCoord {
			if neighbor == nil || coord == chunk.Coord || neighbor.NonEmptyVoxelCount == 0 {
				continue
			}
			if absInt(coord.X-chunk.Coord.X) > 1 || absInt(coord.Y-chunk.Coord.Y) > 1 || absInt(coord.Z-chunk.Coord.Z) > 1 {
				continue
			}
			chunkMaps[coord] = importedWorldChunkToXBrickMap(neighbor)
		}
	}

	sectorKeys := make([][3]int, 0, len(xbm.Sectors))
	for key := range xbm.Sectors {
		sectorKeys = append(sectorKeys, key)
	}
	sort.Slice(sectorKeys, func(i, j int) bool {
		return coord3Less(sectorKeys[i], sectorKeys[j])
	})
	for _, sKey := range sectorKeys {
		sector := xbm.Sectors[sKey]
		if sector == nil {
			continue
		}
		for bz := 0; bz < volume.SectorBricks; bz++ {
			for by := 0; by < volume.SectorBricks; by++ {
				for bx := 0; bx < volume.SectorBricks; bx++ {
					brick := sector.GetBrick(bx, by, bz)
					if brick == nil || brick.IsEmpty() {
						continue
					}
					origin := [3]int{
						sKey[0]*volume.SectorSize + bx*volume.BrickSize,
						sKey[1]*volume.SectorSize + by*volume.BrickSize,
						sKey[2]*volume.SectorSize + bz*volume.BrickSize,
					}
					bytes := volume.BuildVoxelAuxBytes(brick, origin, volume.VoxelNormalBakeOptions{
						SampleOccupancy: importedWorldAuxSampler(chunk, chunkMaps, sampleNeighbors),
						BoundsMin:       minB,
						BoundsMax:       maxB,
						HasBounds:       true,
					})
					aux.Records = append(aux.Records, content.ImportedWorldBrickAuxDef{
						Origin: origin,
						Bytes:  bytes,
					})
				}
			}
		}
	}
	if len(aux.Records) == 0 {
		return nil
	}
	return aux
}

func importedWorldAuxSampler(chunk *content.ImportedWorldChunkDef, chunkMaps map[content.TerrainChunkCoordDef]*volume.XBrickMap, sampleNeighbors bool) func([3]int) bool {
	return func(voxel [3]int) bool {
		if chunk == nil || chunk.ChunkSize <= 0 {
			return false
		}
		coord := chunk.Coord
		local := voxel
		if sampleNeighbors {
			offset := [3]int{
				floorDivInt(voxel[0], chunk.ChunkSize),
				floorDivInt(voxel[1], chunk.ChunkSize),
				floorDivInt(voxel[2], chunk.ChunkSize),
			}
			coord = content.TerrainChunkCoordDef{
				X: chunk.Coord.X + offset[0],
				Y: chunk.Coord.Y + offset[1],
				Z: chunk.Coord.Z + offset[2],
			}
			local = [3]int{
				positiveModInt(voxel[0], chunk.ChunkSize),
				positiveModInt(voxel[1], chunk.ChunkSize),
				positiveModInt(voxel[2], chunk.ChunkSize),
			}
		} else if voxel[0] < 0 || voxel[1] < 0 || voxel[2] < 0 || voxel[0] >= chunk.ChunkSize || voxel[1] >= chunk.ChunkSize || voxel[2] >= chunk.ChunkSize {
			return false
		}
		xbm := chunkMaps[coord]
		if xbm == nil {
			return false
		}
		occupied, _ := xbm.GetVoxel(local[0], local[1], local[2])
		return occupied
	}
}

func importedWorldChunkToXBrickMap(chunk *content.ImportedWorldChunkDef) *volume.XBrickMap {
	if chunk == nil {
		return volume.BuildXBrickMap(nil)
	}
	xbm := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, voxel := range chunk.Voxels {
			if voxel.Value == 0 {
				continue
			}
			if !yield(volume.VoxelWrite{X: voxel.X, Y: voxel.Y, Z: voxel.Z, Value: content.ImportedWorldVoxelMaterialValue(voxel)}) {
				return
			}
		}
	})
	xbm.ClearDirty()
	return xbm
}

func coord3Less(a, b [3]int) bool {
	if a[0] != b[0] {
		return a[0] < b[0]
	}
	if a[1] != b[1] {
		return a[1] < b[1]
	}
	return a[2] < b[2]
}

func floorDivInt(a, b int) int {
	q := a / b
	r := a % b
	if r != 0 && ((r < 0) != (b < 0)) {
		q--
	}
	return q
}

func positiveModInt(a, b int) int {
	r := a % b
	if r < 0 {
		r += absInt(b)
	}
	return r
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
