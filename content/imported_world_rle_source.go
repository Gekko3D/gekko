package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"os"
)

// ErrImportedWorldChunkNotRLE requests the caller's ordinary JSON/compiled
// loading path. Recognized RLE corruption and IO errors never return it.
var ErrImportedWorldChunkNotRLE = errors.New("imported world chunk is not RLE")

// ImportedWorldChunkRLESource owns a validated immutable encoded file. It keeps
// the complete backing allocation, including the header, and no voxel array.
// Metadata and iteration expose independent values without exposing that file.
type ImportedWorldChunkRLESource struct {
	data          []byte
	metadata      ImportedWorldChunkDef
	payloadOffset int
	runWidth      int
}

// LoadImportedWorldChunkRLESource validates all runs before returning a source.
// Legacy hash/count/size hints retain their existing semantics; no compiled
// codec limits apply. Unsafe lattice/count arithmetic is rejected before any
// per-voxel storage could be constructed.
func LoadImportedWorldChunkRLESource(path string) (*ImportedWorldChunkRLESource, error) {
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
	if n != len(prefix) || !bytes.Equal(prefix[:], importedWorldChunkDenseRLEMagic) {
		return nil, ErrImportedWorldChunkNotRLE
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	const headerSize = 12
	if len(data) < headerSize {
		return nil, fmt.Errorf("imported world RLE metadata length is truncated")
	}
	if !bytes.Equal(data[:8], importedWorldChunkDenseRLEMagic) {
		return nil, fmt.Errorf("imported world RLE prefix changed during read")
	}
	metadataLength := uint64(binary.LittleEndian.Uint32(data[8:headerSize]))
	if metadataLength == 0 || metadataLength > uint64(len(data)-headerSize) {
		return nil, fmt.Errorf("imported world RLE metadata length is invalid")
	}
	payloadOffset := headerSize + int(metadataLength)
	var meta importedWorldChunkBinaryMetadata
	if err := json.Unmarshal(data[headerSize:payloadOffset], &meta); err != nil {
		return nil, err
	}
	if meta.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		return nil, fmt.Errorf("unsupported imported world chunk schema version %d", meta.SchemaVersion)
	}
	runWidth := 5
	switch meta.PayloadKind {
	case ImportedWorldChunkPayloadDenseRLEBinaryV1:
	case ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1:
		runWidth = 6
	default:
		return nil, fmt.Errorf("unsupported imported world RLE payload kind %q", meta.PayloadKind)
	}
	if meta.ChunkSize <= 0 {
		return nil, fmt.Errorf("imported world RLE chunk size must be positive")
	}
	size := uint64(meta.ChunkSize)
	if size > math.MaxUint64/size || size*size > math.MaxUint64/size {
		return nil, fmt.Errorf("imported world RLE chunk lattice overflows")
	}
	totalCells := size * size * size
	payload := data[payloadOffset:]
	if meta.PayloadHash != "" {
		hash := sha256.Sum256(payload)
		if hex.EncodeToString(hash[:]) != meta.PayloadHash {
			return nil, fmt.Errorf("imported world chunk binary payload hash mismatch")
		}
	}
	if len(payload) < 4 {
		return nil, fmt.Errorf("imported world RLE run count is truncated")
	}
	runCount := binary.LittleEndian.Uint32(payload[:4])
	if (len(payload)-4)%runWidth != 0 || uint64((len(payload)-4)/runWidth) != uint64(runCount) {
		return nil, fmt.Errorf("imported world RLE payload length does not match run count")
	}
	var cursor, occupied uint64
	for offset := 4; offset < len(payload); offset += runWidth {
		length := uint64(binary.LittleEndian.Uint32(payload[offset+runWidth-4 : offset+runWidth]))
		if length == 0 {
			return nil, fmt.Errorf("imported world RLE run has zero length")
		}
		if length > totalCells-cursor {
			return nil, fmt.Errorf("imported world RLE run exceeds chunk bounds")
		}
		cursor += length
		if payload[offset] != 0 {
			occupied += length
		}
	}
	if cursor != totalCells {
		return nil, fmt.Errorf("imported world RLE payload covers %d cells, expected %d", cursor, totalCells)
	}
	if occupied > uint64(^uint(0)>>1) {
		return nil, fmt.Errorf("imported world RLE occupied count is not representable")
	}
	return &ImportedWorldChunkRLESource{
		data: data, payloadOffset: payloadOffset, runWidth: runWidth,
		metadata: ImportedWorldChunkDef{
			WorldID: meta.WorldID, SchemaVersion: meta.SchemaVersion, Coord: meta.Coord,
			ChunkSize: meta.ChunkSize, VoxelResolution: meta.VoxelResolution,
			PayloadKind: meta.PayloadKind, PayloadHash: meta.PayloadHash, PayloadSizeBytes: meta.PayloadSizeBytes,
			NonEmptyVoxelCount: int(occupied), Tags: append([]string(nil), meta.Tags...),
		},
	}, nil
}

// Metadata returns detached header storage with the actual occupied count and
// no materialized voxel records. Legacy resolution and size metadata survive.
func (s *ImportedWorldChunkRLESource) Metadata() *ImportedWorldChunkDef {
	if s == nil {
		return nil
	}
	metadata := s.metadata
	metadata.Tags = append([]string(nil), s.metadata.Tags...)
	return &metadata
}

// Voxels yields nonzero primary cells in X-fast order. Every invocation owns
// its cursor, so sequences are reusable and safe to iterate concurrently.
func (s *ImportedWorldChunkRLESource) Voxels() iter.Seq[ImportedWorldVoxelDef] {
	return func(yield func(ImportedWorldVoxelDef) bool) {
		if s == nil {
			return
		}
		payload := s.data[s.payloadOffset:]
		var cursor uint64
		for offset := 4; offset < len(payload); offset += s.runWidth {
			value := payload[offset]
			var material uint8
			if s.runWidth == 6 {
				material = payload[offset+1]
				if material == value {
					material = 0
				}
			}
			length := uint64(binary.LittleEndian.Uint32(payload[offset+s.runWidth-4 : offset+s.runWidth]))
			if value != 0 {
				for n := uint64(0); n < length; n++ {
					x, y, z := importedWorldVoxelCoordsFromLinear(cursor+n, s.metadata.ChunkSize)
					if !yield(ImportedWorldVoxelDef{X: x, Y: y, Z: z, Value: value, MaterialValue: material}) {
						return
					}
				}
			}
			cursor += length
		}
	}
}
