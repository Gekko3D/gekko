package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

type importedCompiledAuxMetadata struct {
	SchemaVersion          int    `json:"schema_version"`
	SourcePayloadHash      string `json:"source_payload_hash"`
	SourcePayloadSizeBytes int    `json:"source_payload_size_bytes"`
}

// ImportedWorldChunkCompiledGeometryIdentity identifies the exact geometry-only
// C1a projection without mutating the chunk or using encoded-size limits.
func ImportedWorldChunkCompiledGeometryIdentity(chunk *ImportedWorldChunkDef, codec *voxelcodec.Codec) (string, int, error) {
	if codec == nil {
		var err error
		codec, err = defaultImportedCompiledCodec()
		if err != nil {
			return "", 0, err
		}
	}
	doc, _, err := importedCompiledGeometry(chunk)
	if err != nil {
		return "", 0, err
	}
	hash, size, err := codec.Identity(doc)
	if err != nil {
		return "", 0, err
	}
	return hash, int(size), nil
}

// SaveImportedWorldChunkCompiledWithAux validates an explicit fitted-normal
// layer. Nil removes it. Only a successful save publishes owned canonical data.
func SaveImportedWorldChunkCompiledWithAux(path string, chunk *ImportedWorldChunkDef, aux *ImportedWorldChunkAuxDef, codec *voxelcodec.Codec) (ImportedWorldChunkSaveResult, error) {
	if codec == nil {
		var err error
		codec, err = defaultImportedCompiledCodec()
		if err != nil {
			return ImportedWorldChunkSaveResult{}, err
		}
	}
	doc, metadata, err := importedCompiledGeometry(chunk)
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	var owned *ImportedWorldChunkAuxDef
	if aux != nil {
		hash, size, err := codec.Identity(doc)
		if err != nil {
			return ImportedWorldChunkSaveResult{}, err
		}
		owned, err = canonicalImportedCompiledAux(doc, metadata, aux, hash, int(size))
		if err != nil {
			return ImportedWorldChunkSaveResult{}, err
		}
		metadata.Aux = &importedCompiledAuxMetadata{SchemaVersion: owned.SchemaVersion, SourcePayloadHash: hash, SourcePayloadSizeBytes: int(size)}
		doc.Metadata, err = json.Marshal(metadata)
		if err != nil {
			return ImportedWorldChunkSaveResult{}, err
		}
		doc.NormalBakeVersion = owned.NormalBakeVersion
		brickIndices := make(map[[3]int32]int, len(doc.Bricks))
		for i, b := range doc.Bricks {
			brickIndices[b.Coord] = i
		}
		for _, record := range owned.Records {
			coord := [3]int32{int32(record.Origin[0] / 8), int32(record.Origin[1] / 8), int32(record.Origin[2] / 8)}
			doc.Bricks[brickIndices[coord]].Aux = record.Bytes
		}
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
	chunk.SchemaVersion = metadata.SchemaVersion
	chunk.PayloadKind, chunk.PayloadHash, chunk.PayloadSizeBytes, chunk.NonEmptyVoxelCount = ImportedWorldChunkPayloadBrickZstdBinaryV1, info.ContentID, int(info.DecodedBytes), metadata.NonEmptyVoxelCount
	chunk.EmbeddedAux = owned
	return ImportedWorldChunkSaveResult{Wrote: wrote, PayloadKind: chunk.PayloadKind, PayloadHash: chunk.PayloadHash, PayloadSizeBytes: chunk.PayloadSizeBytes}, nil
}

func canonicalImportedCompiledAux(doc voxelcodec.Document, m importedWorldCompiledMetadata, aux *ImportedWorldChunkAuxDef, hash string, size int) (*ImportedWorldChunkAuxDef, error) {
	if aux.SchemaVersion != CurrentImportedWorldChunkAuxSchemaVersion || aux.NormalBakeVersion != ImportedWorldNormalBakeVersion || aux.WorldID != m.WorldID || aux.Coord != m.Coord || aux.ChunkSize != m.ChunkSize || aux.VoxelResolution != m.VoxelResolution || aux.SourcePayloadHash != hash || aux.SourcePayloadSizeBytes != size || len(aux.Records) == 0 || len(aux.Records) > len(doc.Bricks) {
		return nil, fmt.Errorf("invalid embedded normal ownership/version/source binding")
	}
	bricks := make(map[[3]int32][8]uint64, len(doc.Bricks))
	for _, b := range doc.Bricks {
		bricks[b.Coord] = b.Occupancy
	}
	seen := make(map[[3]int]struct{}, len(aux.Records))
	owned := *aux
	owned.Records = make([]ImportedWorldBrickAuxDef, 0, len(aux.Records))
	for _, record := range aux.Records {
		if len(record.Bytes) != 1088 {
			return nil, fmt.Errorf("invalid embedded normal record size")
		}
		for _, value := range record.Origin {
			if value < 0 || value%8 != 0 || int64(value) > 2147483647 {
				return nil, fmt.Errorf("invalid embedded normal record origin")
			}
		}
		if _, duplicate := seen[record.Origin]; duplicate {
			return nil, fmt.Errorf("duplicate embedded normal record")
		}
		seen[record.Origin] = struct{}{}
		coord := [3]int32{int32(record.Origin[0] / 8), int32(record.Origin[1] / 8), int32(record.Origin[2] / 8)}
		occupancy, found := bricks[coord]
		if !found {
			return nil, fmt.Errorf("embedded normals reference absent source brick")
		}
		for i, word := range occupancy {
			if binary.LittleEndian.Uint64(record.Bytes[i*8:]) != word {
				return nil, fmt.Errorf("embedded normal occupancy disagrees with source")
			}
		}
		owned.Records = append(owned.Records, ImportedWorldBrickAuxDef{Origin: record.Origin, Bytes: append([]byte(nil), record.Bytes...)})
	}
	sort.Slice(owned.Records, func(i, j int) bool {
		a, b := owned.Records[i].Origin, owned.Records[j].Origin
		for axis := 0; axis < 3; axis++ {
			if a[axis] != b[axis] {
				return a[axis] < b[axis]
			}
		}
		return false
	})
	payload, err := encodeImportedWorldChunkAuxPayload(owned.Records)
	if err != nil {
		return nil, err
	}
	payloadHash := sha256.Sum256(payload)
	owned.PayloadKind, owned.PayloadHash, owned.PayloadSizeBytes = ImportedWorldChunkAuxPayloadBinaryV1, hex.EncodeToString(payloadHash[:]), len(payload)
	return &owned, nil
}

func decodeImportedCompiledAux(doc voxelcodec.Document, m importedWorldCompiledMetadata, codec *voxelcodec.Codec) (*ImportedWorldChunkAuxDef, error) {
	if m.Aux == nil {
		if doc.NormalBakeVersion != "" {
			return nil, fmt.Errorf("undeclared imported bake layer")
		}
		for _, b := range doc.Bricks {
			if b.Aux != nil {
				return nil, fmt.Errorf("undeclared imported auxiliary layer")
			}
		}
		return nil, nil
	}
	canonical, err := json.Marshal(m)
	if err != nil || !bytes.Equal(canonical, doc.Metadata) {
		return nil, fmt.Errorf("noncanonical embedded imported metadata")
	}
	projection := doc
	projection.Bricks = append([]voxelcodec.Brick(nil), doc.Bricks...)
	projection.NormalBakeVersion = ""
	geometryMetadata := m
	geometryMetadata.Aux = nil
	projection.Metadata, err = json.Marshal(geometryMetadata)
	if err != nil {
		return nil, err
	}
	aux := &ImportedWorldChunkAuxDef{WorldID: m.WorldID, SchemaVersion: m.Aux.SchemaVersion, Coord: m.Coord, ChunkSize: m.ChunkSize, VoxelResolution: m.VoxelResolution, NormalBakeVersion: doc.NormalBakeVersion, SourcePayloadHash: m.Aux.SourcePayloadHash, SourcePayloadSizeBytes: m.Aux.SourcePayloadSizeBytes}
	for i, b := range doc.Bricks {
		if b.Materials == nil {
			return nil, fmt.Errorf("embedded imported raw material layer is missing")
		}
		projection.Bricks[i].Aux = nil
		if b.Aux != nil {
			origin := [3]int{int(b.Coord[0]) * 8, int(b.Coord[1]) * 8, int(b.Coord[2]) * 8}
			aux.Records = append(aux.Records, ImportedWorldBrickAuxDef{Origin: origin, Bytes: b.Aux})
		}
	}
	hash, size, err := codec.Identity(projection)
	if err != nil {
		return nil, err
	}
	return canonicalImportedCompiledAux(doc, m, aux, hash, int(size))
}
