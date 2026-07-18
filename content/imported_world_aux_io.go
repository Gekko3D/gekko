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
	"path/filepath"
)

var importedWorldChunkAuxMagic = []byte{'G', 'K', 'A', 'U', 'X', '1', '\n'}

type importedWorldChunkAuxMetadata struct {
	WorldID                string               `json:"world_id"`
	SchemaVersion          int                  `json:"schema_version"`
	Coord                  TerrainChunkCoordDef `json:"coord"`
	ChunkSize              int                  `json:"chunk_size"`
	VoxelResolution        float32              `json:"voxel_resolution"`
	PayloadKind            string               `json:"payload_kind"`
	PayloadHash            string               `json:"payload_hash,omitempty"`
	PayloadSizeBytes       int                  `json:"payload_size_bytes,omitempty"`
	NormalBakeVersion      string               `json:"normal_bake_version,omitempty"`
	SourcePayloadHash      string               `json:"source_payload_hash,omitempty"`
	SourcePayloadSizeBytes int                  `json:"source_payload_size_bytes,omitempty"`
}

type ImportedWorldChunkAuxSaveResult struct {
	Wrote            bool
	PayloadKind      string
	PayloadHash      string
	PayloadSizeBytes int
}

func SaveImportedWorldChunkAux(path string, def *ImportedWorldChunkAuxDef) error {
	_, err := SaveImportedWorldChunkAuxWithResult(path, def)
	return err
}

func SaveImportedWorldChunkAuxWithResult(path string, def *ImportedWorldChunkAuxDef) (ImportedWorldChunkAuxSaveResult, error) {
	if def == nil {
		return ImportedWorldChunkAuxSaveResult{}, fmt.Errorf("imported world chunk aux is nil")
	}
	EnsureImportedWorldChunkAuxDefaults(def)
	if def.SchemaVersion != CurrentImportedWorldChunkAuxSchemaVersion {
		return ImportedWorldChunkAuxSaveResult{}, fmt.Errorf("unsupported imported world chunk aux schema version %d", def.SchemaVersion)
	}
	payload, err := encodeImportedWorldChunkAuxPayload(def.Records)
	if err != nil {
		return ImportedWorldChunkAuxSaveResult{}, err
	}
	hash := sha256.Sum256(payload)
	def.PayloadKind = ImportedWorldChunkAuxPayloadBinaryV1
	def.PayloadHash = hex.EncodeToString(hash[:])
	def.PayloadSizeBytes = len(payload)

	meta := importedWorldChunkAuxMetadata{
		WorldID:                def.WorldID,
		SchemaVersion:          def.SchemaVersion,
		Coord:                  def.Coord,
		ChunkSize:              def.ChunkSize,
		VoxelResolution:        def.VoxelResolution,
		PayloadKind:            def.PayloadKind,
		PayloadHash:            def.PayloadHash,
		PayloadSizeBytes:       def.PayloadSizeBytes,
		NormalBakeVersion:      def.NormalBakeVersion,
		SourcePayloadHash:      def.SourcePayloadHash,
		SourcePayloadSizeBytes: def.SourcePayloadSizeBytes,
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return ImportedWorldChunkAuxSaveResult{}, err
	}
	if len(metaData) > math.MaxUint32 {
		return ImportedWorldChunkAuxSaveResult{}, fmt.Errorf("imported world chunk aux metadata is too large")
	}
	var out bytes.Buffer
	out.Write(importedWorldChunkAuxMagic)
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(metaData)))
	out.Write(lenBuf[:])
	out.Write(metaData)
	out.Write(payload)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ImportedWorldChunkAuxSaveResult{}, err
	}
	wrote, err := writeFileIfChanged(path, out.Bytes(), 0644)
	if err != nil {
		return ImportedWorldChunkAuxSaveResult{}, err
	}
	return ImportedWorldChunkAuxSaveResult{
		Wrote:            wrote,
		PayloadKind:      def.PayloadKind,
		PayloadHash:      def.PayloadHash,
		PayloadSizeBytes: def.PayloadSizeBytes,
	}, nil
}

func LoadImportedWorldChunkAux(path string) (*ImportedWorldChunkAuxDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(data, importedWorldChunkAuxMagic) {
		return nil, fmt.Errorf("imported world chunk aux magic mismatch")
	}
	if len(data) < len(importedWorldChunkAuxMagic)+4 {
		return nil, fmt.Errorf("imported world chunk aux payload is truncated")
	}
	offset := len(importedWorldChunkAuxMagic)
	metaLen := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
	offset += 4
	if metaLen <= 0 || offset+metaLen > len(data) {
		return nil, fmt.Errorf("imported world chunk aux metadata length is invalid")
	}
	var meta importedWorldChunkAuxMetadata
	if err := json.Unmarshal(data[offset:offset+metaLen], &meta); err != nil {
		return nil, err
	}
	offset += metaLen
	if meta.PayloadKind != ImportedWorldChunkAuxPayloadBinaryV1 {
		return nil, fmt.Errorf("unsupported imported world chunk aux payload kind %q", meta.PayloadKind)
	}
	payload := data[offset:]
	if meta.PayloadHash != "" {
		hash := sha256.Sum256(payload)
		if got := hex.EncodeToString(hash[:]); got != meta.PayloadHash {
			return nil, fmt.Errorf("imported world chunk aux payload hash mismatch")
		}
	}
	records, err := decodeImportedWorldChunkAuxPayload(payload)
	if err != nil {
		return nil, err
	}
	aux := &ImportedWorldChunkAuxDef{
		WorldID:                meta.WorldID,
		SchemaVersion:          meta.SchemaVersion,
		Coord:                  meta.Coord,
		ChunkSize:              meta.ChunkSize,
		VoxelResolution:        meta.VoxelResolution,
		PayloadKind:            meta.PayloadKind,
		PayloadHash:            meta.PayloadHash,
		PayloadSizeBytes:       meta.PayloadSizeBytes,
		NormalBakeVersion:      meta.NormalBakeVersion,
		SourcePayloadHash:      meta.SourcePayloadHash,
		SourcePayloadSizeBytes: meta.SourcePayloadSizeBytes,
		Records:                records,
	}
	EnsureImportedWorldChunkAuxDefaults(aux)
	if aux.SchemaVersion != CurrentImportedWorldChunkAuxSchemaVersion {
		return nil, fmt.Errorf("unsupported imported world chunk aux schema version %d", aux.SchemaVersion)
	}
	return aux, nil
}

func encodeImportedWorldChunkAuxPayload(records []ImportedWorldBrickAuxDef) ([]byte, error) {
	if len(records) > math.MaxUint32 {
		return nil, fmt.Errorf("imported world chunk aux record count exceeds uint32")
	}
	var out bytes.Buffer
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(len(records)))
	out.Write(buf[:])
	for _, record := range records {
		for _, v := range record.Origin {
			if v < -2147483648 || v > 2147483647 {
				return nil, fmt.Errorf("imported world chunk aux origin component %d exceeds int32", v)
			}
			binary.LittleEndian.PutUint32(buf[:], uint32(int32(v)))
			out.Write(buf[:])
		}
		if len(record.Bytes) > math.MaxUint32 {
			return nil, fmt.Errorf("imported world chunk aux record is too large")
		}
		binary.LittleEndian.PutUint32(buf[:], uint32(len(record.Bytes)))
		out.Write(buf[:])
		out.Write(record.Bytes)
	}
	return out.Bytes(), nil
}

func decodeImportedWorldChunkAuxPayload(payload []byte) ([]ImportedWorldBrickAuxDef, error) {
	if len(payload) < 4 {
		return nil, fmt.Errorf("imported world chunk aux payload is truncated")
	}
	count := int(binary.LittleEndian.Uint32(payload[:4]))
	offset := 4
	records := make([]ImportedWorldBrickAuxDef, 0, count)
	for i := 0; i < count; i++ {
		if offset+16 > len(payload) {
			return nil, fmt.Errorf("imported world chunk aux record %d is truncated", i)
		}
		record := ImportedWorldBrickAuxDef{}
		for axis := 0; axis < 3; axis++ {
			record.Origin[axis] = int(int32(binary.LittleEndian.Uint32(payload[offset : offset+4])))
			offset += 4
		}
		byteLen := int(binary.LittleEndian.Uint32(payload[offset : offset+4]))
		offset += 4
		if byteLen < 0 || offset+byteLen > len(payload) {
			return nil, fmt.Errorf("imported world chunk aux record %d length is invalid", i)
		}
		record.Bytes = append([]byte(nil), payload[offset:offset+byteLen]...)
		offset += byteLen
		records = append(records, record)
	}
	if offset != len(payload) {
		return nil, fmt.Errorf("imported world chunk aux payload has trailing bytes")
	}
	return records, nil
}
