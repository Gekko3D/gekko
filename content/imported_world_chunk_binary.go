package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
)

var importedWorldChunkDenseRLEMagic = []byte{'G', 'K', 'C', 'H', 'N', 'K', '1', '\n'}

type importedWorldChunkBinaryMetadata struct {
	WorldID            string               `json:"world_id"`
	SchemaVersion      int                  `json:"schema_version"`
	Coord              TerrainChunkCoordDef `json:"coord"`
	ChunkSize          int                  `json:"chunk_size"`
	VoxelResolution    float32              `json:"voxel_resolution"`
	PayloadKind        string               `json:"payload_kind"`
	PayloadHash        string               `json:"payload_hash,omitempty"`
	PayloadSizeBytes   int                  `json:"payload_size_bytes,omitempty"`
	NonEmptyVoxelCount int                  `json:"non_empty_voxel_count,omitempty"`
	Tags               []string             `json:"tags,omitempty"`
}

type importedWorldChunkRLERun struct {
	value  uint8
	length uint32
}

func NormalizeImportedWorldChunkPayloadKind(kind string) (string, error) {
	switch kind {
	case "", ImportedWorldChunkPayloadSparseJSONV1:
		return ImportedWorldChunkPayloadSparseJSONV1, nil
	case ImportedWorldChunkPayloadDenseRLEBinaryV1:
		return ImportedWorldChunkPayloadDenseRLEBinaryV1, nil
	default:
		return "", fmt.Errorf("unsupported imported world chunk payload kind %q", kind)
	}
}

func isImportedWorldChunkDenseRLEBinary(data []byte) bool {
	return bytes.HasPrefix(data, importedWorldChunkDenseRLEMagic)
}

func saveImportedWorldChunkDenseRLEBinary(path string, def *ImportedWorldChunkDef) error {
	payload, nonEmpty, err := encodeImportedWorldChunkDenseRLEPayload(def)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	def.PayloadKind = ImportedWorldChunkPayloadDenseRLEBinaryV1
	def.PayloadHash = hex.EncodeToString(hash[:])
	def.PayloadSizeBytes = len(payload)
	def.NonEmptyVoxelCount = nonEmpty
	meta := importedWorldChunkBinaryMetadata{
		WorldID:            def.WorldID,
		SchemaVersion:      def.SchemaVersion,
		Coord:              def.Coord,
		ChunkSize:          def.ChunkSize,
		VoxelResolution:    def.VoxelResolution,
		PayloadKind:        def.PayloadKind,
		PayloadHash:        def.PayloadHash,
		PayloadSizeBytes:   def.PayloadSizeBytes,
		NonEmptyVoxelCount: def.NonEmptyVoxelCount,
		Tags:               append([]string(nil), def.Tags...),
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if len(metaData) > math.MaxUint32 {
		return fmt.Errorf("imported world chunk binary metadata is too large")
	}
	var out bytes.Buffer
	out.Write(importedWorldChunkDenseRLEMagic)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(metaData)))
	out.Write(lenBuf[:])
	out.Write(metaData)
	out.Write(payload)
	return os.WriteFile(path, out.Bytes(), 0644)
}

func loadImportedWorldChunkDenseRLEBinary(data []byte) (*ImportedWorldChunkDef, error) {
	if len(data) < len(importedWorldChunkDenseRLEMagic)+4 {
		return nil, fmt.Errorf("imported world chunk binary payload is truncated")
	}
	offset := len(importedWorldChunkDenseRLEMagic)
	metaLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if metaLen <= 0 || offset+metaLen > len(data) {
		return nil, fmt.Errorf("imported world chunk binary metadata length is invalid")
	}
	var meta importedWorldChunkBinaryMetadata
	if err := json.Unmarshal(data[offset:offset+metaLen], &meta); err != nil {
		return nil, err
	}
	offset += metaLen
	if meta.PayloadKind != ImportedWorldChunkPayloadDenseRLEBinaryV1 {
		return nil, fmt.Errorf("unsupported imported world chunk binary payload kind %q", meta.PayloadKind)
	}
	payload := data[offset:]
	if meta.PayloadHash != "" {
		hash := sha256.Sum256(payload)
		if got := hex.EncodeToString(hash[:]); got != meta.PayloadHash {
			return nil, fmt.Errorf("imported world chunk binary payload hash mismatch")
		}
	}
	chunk := &ImportedWorldChunkDef{
		WorldID:            meta.WorldID,
		SchemaVersion:      meta.SchemaVersion,
		Coord:              meta.Coord,
		ChunkSize:          meta.ChunkSize,
		VoxelResolution:    meta.VoxelResolution,
		PayloadKind:        meta.PayloadKind,
		PayloadHash:        meta.PayloadHash,
		PayloadSizeBytes:   meta.PayloadSizeBytes,
		NonEmptyVoxelCount: meta.NonEmptyVoxelCount,
		Tags:               append([]string(nil), meta.Tags...),
	}
	if chunk.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		return nil, fmt.Errorf("unsupported imported world chunk schema version %d", chunk.SchemaVersion)
	}
	voxels, nonEmpty, err := decodeImportedWorldChunkDenseRLEPayload(payload, chunk.ChunkSize, chunk.NonEmptyVoxelCount)
	if err != nil {
		return nil, err
	}
	chunk.Voxels = voxels
	chunk.NonEmptyVoxelCount = nonEmpty
	EnsureImportedWorldChunkDefaults(chunk)
	return chunk, nil
}

func encodeImportedWorldChunkDenseRLEPayload(def *ImportedWorldChunkDef) ([]byte, int, error) {
	if def.ChunkSize <= 0 {
		return nil, 0, fmt.Errorf("imported world chunk size must be positive")
	}
	totalCells, err := importedWorldChunkTotalCells(def.ChunkSize)
	if err != nil {
		return nil, 0, err
	}
	voxels := make([]ImportedWorldVoxelDef, 0, len(def.Voxels))
	for _, voxel := range def.Voxels {
		if voxel.Value == 0 {
			continue
		}
		if _, ok := importedWorldVoxelLinearIndex(voxel, def.ChunkSize); !ok {
			return nil, 0, fmt.Errorf("imported world voxel (%d,%d,%d) is outside chunk size %d", voxel.X, voxel.Y, voxel.Z, def.ChunkSize)
		}
		voxels = append(voxels, voxel)
	}
	sort.Slice(voxels, func(i, j int) bool {
		left, _ := importedWorldVoxelLinearIndex(voxels[i], def.ChunkSize)
		right, _ := importedWorldVoxelLinearIndex(voxels[j], def.ChunkSize)
		return left < right
	})
	runs := make([]importedWorldChunkRLERun, 0, len(voxels)*2+1)
	cursor := uint64(0)
	var previousIndex uint64
	for i, voxel := range voxels {
		index, _ := importedWorldVoxelLinearIndex(voxel, def.ChunkSize)
		if i > 0 && index == previousIndex {
			return nil, 0, fmt.Errorf("duplicate imported world voxel at linear index %d", index)
		}
		if index > cursor {
			runs = appendImportedWorldChunkRLERun(runs, 0, index-cursor)
		}
		runs = appendImportedWorldChunkRLERun(runs, voxel.Value, 1)
		cursor = index + 1
		previousIndex = index
	}
	if cursor < totalCells {
		runs = appendImportedWorldChunkRLERun(runs, 0, totalCells-cursor)
	}
	if len(runs) > math.MaxUint32 {
		return nil, 0, fmt.Errorf("imported world chunk RLE run count exceeds uint32")
	}
	var out bytes.Buffer
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(len(runs)))
	out.Write(buf[:])
	for _, run := range runs {
		out.WriteByte(run.value)
		binary.LittleEndian.PutUint32(buf[:], run.length)
		out.Write(buf[:])
	}
	return out.Bytes(), len(voxels), nil
}

func decodeImportedWorldChunkDenseRLEPayload(payload []byte, chunkSize int, expectedNonEmpty int) ([]ImportedWorldVoxelDef, int, error) {
	if chunkSize <= 0 {
		return nil, 0, fmt.Errorf("imported world chunk size must be positive")
	}
	totalCells, err := importedWorldChunkTotalCells(chunkSize)
	if err != nil {
		return nil, 0, err
	}
	if len(payload) < 4 {
		return nil, 0, fmt.Errorf("imported world chunk RLE payload is truncated")
	}
	runCount := int(binary.LittleEndian.Uint32(payload[:4]))
	offset := 4
	if len(payload)-offset != runCount*5 {
		return nil, 0, fmt.Errorf("imported world chunk RLE payload length does not match run count")
	}
	capacityHint := expectedNonEmpty
	if capacityHint < 0 {
		capacityHint = 0
	}
	voxels := make([]ImportedWorldVoxelDef, 0, capacityHint)
	cursor := uint64(0)
	for i := 0; i < runCount; i++ {
		value := payload[offset]
		length := uint64(binary.LittleEndian.Uint32(payload[offset+1 : offset+5]))
		offset += 5
		if length == 0 {
			return nil, 0, fmt.Errorf("imported world chunk RLE run %d has zero length", i)
		}
		if cursor+length > totalCells {
			return nil, 0, fmt.Errorf("imported world chunk RLE run %d exceeds chunk bounds", i)
		}
		if value != 0 {
			for n := uint64(0); n < length; n++ {
				x, y, z := importedWorldVoxelCoordsFromLinear(cursor+n, chunkSize)
				voxels = append(voxels, ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: value})
			}
		}
		cursor += length
	}
	if cursor != totalCells {
		return nil, 0, fmt.Errorf("imported world chunk RLE payload covers %d cells, expected %d", cursor, totalCells)
	}
	return voxels, len(voxels), nil
}

func appendImportedWorldChunkRLERun(runs []importedWorldChunkRLERun, value uint8, length uint64) []importedWorldChunkRLERun {
	for length > 0 {
		part := length
		if part > math.MaxUint32 {
			part = math.MaxUint32
		}
		if len(runs) > 0 && runs[len(runs)-1].value == value && uint64(runs[len(runs)-1].length)+part <= math.MaxUint32 {
			runs[len(runs)-1].length += uint32(part)
		} else {
			runs = append(runs, importedWorldChunkRLERun{value: value, length: uint32(part)})
		}
		length -= part
	}
	return runs
}

func importedWorldChunkTotalCells(chunkSize int) (uint64, error) {
	size := uint64(chunkSize)
	total := size * size * size
	if total == 0 {
		return 0, fmt.Errorf("imported world chunk size must be positive")
	}
	return total, nil
}

func importedWorldVoxelLinearIndex(voxel ImportedWorldVoxelDef, chunkSize int) (uint64, bool) {
	if voxel.X < 0 || voxel.Y < 0 || voxel.Z < 0 || voxel.X >= chunkSize || voxel.Y >= chunkSize || voxel.Z >= chunkSize {
		return 0, false
	}
	size := uint64(chunkSize)
	return uint64(voxel.X) + size*(uint64(voxel.Y)+size*uint64(voxel.Z)), true
}

func importedWorldVoxelCoordsFromLinear(index uint64, chunkSize int) (int, int, int) {
	size := uint64(chunkSize)
	z := index / (size * size)
	rem := index % (size * size)
	y := rem / size
	x := rem % size
	return int(x), int(y), int(z)
}
