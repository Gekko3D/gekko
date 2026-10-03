package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

const importedCompiledMagic = "GKBRCK1\n"

var defaultImportedCompiledCodec = sync.OnceValues(func() (*voxelcodec.Codec, error) {
	return voxelcodec.New(voxelcodec.Options{})
})

// Owner metadata is hashed inside the canonical frame. Derived identity and
// encoded/decompressed lengths are intentionally not self-referential fields.
type importedWorldCompiledMetadata struct {
	WorldID            string               `json:"world_id"`
	SchemaVersion      int                  `json:"schema_version"`
	Coord              TerrainChunkCoordDef `json:"coord"`
	ChunkSize          int                  `json:"chunk_size"`
	VoxelResolution    float32              `json:"voxel_resolution"`
	NonEmptyVoxelCount int                  `json:"non_empty_voxel_count"`
	Tags               []string             `json:"tags,omitempty"`
}

func validateImportedCompiledMetadata(m importedWorldCompiledMetadata) error {
	if m.SchemaVersion != CurrentImportedWorldChunkSchemaVersion || m.ChunkSize <= 0 || int64(m.ChunkSize) > (int64(math.MaxInt32)+1)*8 || m.NonEmptyVoxelCount < 0 || m.VoxelResolution <= 0 || math.IsInf(float64(m.VoxelResolution), 0) || math.IsNaN(float64(m.VoxelResolution)) {
		return fmt.Errorf("invalid compiled imported schema/lattice/count")
	}
	return nil
}

// SaveImportedWorldChunkCompiledWithCodec preserves raw primary/secondary bytes
// in independently bounded bricks. The caller owns codec and its lifetime.
func SaveImportedWorldChunkCompiledWithCodec(path string, def *ImportedWorldChunkDef, codec *voxelcodec.Codec) (ImportedWorldChunkSaveResult, error) {
	if def == nil {
		return ImportedWorldChunkSaveResult{}, fmt.Errorf("compiled chunk is nil")
	}
	if codec == nil {
		var err error
		codec, err = defaultImportedCompiledCodec()
		if err != nil {
			return ImportedWorldChunkSaveResult{}, err
		}
	}
	EnsureImportedWorldChunkDefaults(def)
	metadata := importedWorldCompiledMetadata{WorldID: def.WorldID, SchemaVersion: def.SchemaVersion, Coord: def.Coord, ChunkSize: def.ChunkSize, VoxelResolution: def.VoxelResolution, Tags: def.Tags}
	if err := validateImportedCompiledMetadata(metadata); err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	voxels := make([]ImportedWorldVoxelDef, 0, len(def.Voxels))
	for _, v := range def.Voxels {
		if v.Value == 0 {
			continue
		}
		if v.X < 0 || v.Y < 0 || v.Z < 0 || v.X >= def.ChunkSize || v.Y >= def.ChunkSize || v.Z >= def.ChunkSize {
			return ImportedWorldChunkSaveResult{}, fmt.Errorf("occupied imported voxel outside chunk lattice")
		}
		voxels = append(voxels, v)
	}
	// Sorting records costs actual occupancy only; no cubic index arithmetic or
	// allocation is required for a sparse chunk's potentially large dimensions.
	sort.Slice(voxels, func(i, j int) bool { return importedCompiledVoxelLess(voxels[i], voxels[j]) })
	bricks := make(map[[3]int32]*voxelcodec.Brick)
	for i, v := range voxels {
		if i > 0 && v.X == voxels[i-1].X && v.Y == voxels[i-1].Y && v.Z == voxels[i-1].Z {
			return ImportedWorldChunkSaveResult{}, fmt.Errorf("duplicate occupied imported voxel")
		}
		coord := [3]int32{int32(v.X / 8), int32(v.Y / 8), int32(v.Z / 8)}
		b := bricks[coord]
		if b == nil {
			b = &voxelcodec.Brick{Coord: coord}
			bricks[coord] = b
		}
		linear := v.X%8 + 8*(v.Y%8) + 64*(v.Z%8)
		b.Occupancy[linear/64] |= uint64(1) << uint(linear%64)
		// Global x-fast traversal also preserves local bit order within each brick.
		b.Values = append(b.Values, v.Value)
		b.Materials = append(b.Materials, v.MaterialValue)
	}
	metadata.NonEmptyVoxelCount = len(voxels)
	meta, err := json.Marshal(metadata)
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	doc := voxelcodec.Document{Kind: "imported_chunk", Metadata: meta, Bricks: make([]voxelcodec.Brick, 0, len(bricks))}
	for _, b := range bricks {
		doc.Bricks = append(doc.Bricks, *b)
	}
	frame, info, err := codec.Encode(doc)
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	wrote, err := writeFileIfChanged(path, frame, 0644)
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	def.PayloadKind, def.PayloadHash, def.PayloadSizeBytes, def.NonEmptyVoxelCount = ImportedWorldChunkPayloadBrickZstdBinaryV1, info.ContentID, int(info.DecodedBytes), len(voxels)
	return ImportedWorldChunkSaveResult{Wrote: wrote, PayloadKind: def.PayloadKind, PayloadHash: def.PayloadHash, PayloadSizeBytes: def.PayloadSizeBytes}, nil
}

func importedCompiledVoxelLess(a, b ImportedWorldVoxelDef) bool {
	if a.Z != b.Z {
		return a.Z < b.Z
	}
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.X < b.X
}

func importedWorldChunkFromCompiled(doc voxelcodec.Document, info voxelcodec.Info) (*ImportedWorldChunkDef, error) {
	if doc.Kind != "imported_chunk" || doc.NormalBakeVersion != "" {
		return nil, fmt.Errorf("unsupported imported document kind/bake layer")
	}
	var metadata importedWorldCompiledMetadata
	if err := json.Unmarshal(doc.Metadata, &metadata); err != nil {
		return nil, err
	}
	if err := validateImportedCompiledMetadata(metadata); err != nil {
		return nil, err
	}
	// The count is cross-checked from decoded bricks before using owner metadata
	// as an allocation size. Generic codec already validated channel cardinality.
	count := 0
	for _, b := range doc.Bricks {
		if b.Aux != nil {
			return nil, fmt.Errorf("embedded imported auxiliary layers are not supported")
		}
		origin := [3]int64{int64(b.Coord[0]) * 8, int64(b.Coord[1]) * 8, int64(b.Coord[2]) * 8}
		for axis := 0; axis < 3; axis++ {
			if origin[axis] < 0 || origin[axis] >= int64(metadata.ChunkSize) {
				return nil, fmt.Errorf("imported brick outside chunk lattice")
			}
		}
		for linear := 0; linear < 512; linear++ {
			if b.Occupancy[linear/64]&(uint64(1)<<uint(linear%64)) == 0 {
				continue
			}
			xyz := [3]int64{origin[0] + int64(linear%8), origin[1] + int64(linear/8%8), origin[2] + int64(linear/64)}
			for axis := 0; axis < 3; axis++ {
				if xyz[axis] >= int64(metadata.ChunkSize) {
					return nil, fmt.Errorf("occupied imported cell outside chunk lattice")
				}
			}
		}
		for _, word := range b.Occupancy {
			count += bits.OnesCount64(word)
		}
	}
	if count != metadata.NonEmptyVoxelCount {
		return nil, fmt.Errorf("imported occupied-count mismatch")
	}
	chunk := &ImportedWorldChunkDef{WorldID: metadata.WorldID, SchemaVersion: metadata.SchemaVersion, Coord: metadata.Coord, ChunkSize: metadata.ChunkSize, VoxelResolution: metadata.VoxelResolution, NonEmptyVoxelCount: count, Tags: metadata.Tags, PayloadKind: ImportedWorldChunkPayloadBrickZstdBinaryV1, PayloadHash: info.ContentID, PayloadSizeBytes: int(info.DecodedBytes), Voxels: make([]ImportedWorldVoxelDef, 0, count)}
	for _, b := range doc.Bricks {
		origin := [3]int64{int64(b.Coord[0]) * 8, int64(b.Coord[1]) * 8, int64(b.Coord[2]) * 8}
		channel := 0
		for linear := 0; linear < 512; linear++ {
			if b.Occupancy[linear/64]&(uint64(1)<<uint(linear%64)) == 0 {
				continue
			}
			xyz := [3]int64{origin[0] + int64(linear%8), origin[1] + int64(linear/8%8), origin[2] + int64(linear/64)}
			v := ImportedWorldVoxelDef{X: int(xyz[0]), Y: int(xyz[1]), Z: int(xyz[2]), Value: b.Values[channel]}
			if b.Materials != nil {
				v.MaterialValue = b.Materials[channel]
			}
			chunk.Voxels = append(chunk.Voxels, v)
			channel++
		}
	}
	sort.Slice(chunk.Voxels, func(i, j int) bool { return importedCompiledVoxelLess(chunk.Voxels[i], chunk.Voxels[j]) })
	return chunk, nil
}

// LoadImportedWorldChunkWithCodec uses the explicit profile for compiled frames.
// Legacy JSON/RLE loading ignores the profile and retains existing acceptance.
func LoadImportedWorldChunkWithCodec(path string, codec *voxelcodec.Codec) (*ImportedWorldChunkDef, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var prefix [8]byte
	n, err := file.ReadAt(prefix[:], 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if n == len(prefix) && bytes.Equal(prefix[:], []byte(importedCompiledMagic)) {
		stat, err := file.Stat()
		if err != nil {
			return nil, err
		}
		if codec == nil {
			codec, err = defaultImportedCompiledCodec()
			if err != nil {
				return nil, err
			}
		}
		doc, info, err := codec.ReadFrame(file, 0, stat.Size())
		if err != nil {
			return nil, err
		}
		return importedWorldChunkFromCompiled(doc, info)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if isImportedWorldChunkDenseRLEBinary(data) {
		return loadImportedWorldChunkDenseRLEBinary(data)
	}
	var def ImportedWorldChunkDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	EnsureImportedWorldChunkDefaults(&def)
	if def.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		return nil, fmt.Errorf("unsupported imported world chunk schema version %d", def.SchemaVersion)
	}
	return &def, nil
}
