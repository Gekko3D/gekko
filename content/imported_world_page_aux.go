package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Current normal sidecars contain 16 occupancy words and 256 normal words
// (volume.VoxelAuxWordCount), each four bytes. Legacy decoding remains separate.
const importedPageNormalRecordBytes = (16 + 256) * 4

func loadQualifiedImportedPageAux(path string, q importedPageQualification, ref *ImportedWorldChunkAuxRefDef) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return err
	}
	var prefix [11]byte
	if _, err = io.ReadFull(f, prefix[:]); err != nil {
		return err
	}
	if !bytes.Equal(prefix[:7], importedWorldChunkAuxMagic) {
		return fmt.Errorf("imported page aux magic mismatch")
	}
	metadataSize := binary.LittleEndian.Uint32(prefix[7:])
	if metadataSize == 0 || metadataSize > 65536 {
		return fmt.Errorf("imported page aux metadata exceeds bound")
	}
	metadata := make([]byte, int(metadataSize))
	if _, err = io.ReadFull(f, metadata); err != nil {
		return err
	}
	var m importedWorldChunkAuxMetadata
	if err = json.Unmarshal(metadata, &m); err != nil {
		return err
	}
	if m.SchemaVersion != CurrentImportedWorldChunkAuxSchemaVersion || m.WorldID != q.worldID || m.Coord != q.coord || m.ChunkSize != q.side || m.VoxelResolution != q.resolution || m.PayloadKind != ImportedWorldChunkAuxPayloadBinaryV1 || m.PayloadKind != ref.PayloadKind || !importedPageHashEqual(m.PayloadHash, ref.PayloadHash) || m.PayloadSizeBytes != ref.PayloadSizeBytes || !importedPageHashEqual(m.SourcePayloadHash, q.hash) || !importedPageHashEqual(m.SourcePayloadHash, ref.SourcePayloadHash) || m.SourcePayloadSizeBytes != q.size || m.SourcePayloadSizeBytes != ref.SourcePayloadSizeBytes || m.NormalBakeVersion != ImportedWorldNormalBakeVersion || m.NormalBakeVersion != ref.NormalBakeVersion {
		return fmt.Errorf("imported page normal aux owner/grid/identity mismatch")
	}
	brickSide := (q.side + importedPageCostBrickSide - 1) / importedPageCostBrickSide
	maxRecords := brickSide * brickSide * brickSide
	bodySize := stat.Size() - int64(len(prefix)) - int64(metadataSize)
	if bodySize < 4 || bodySize > int64(4+maxRecords*(16+importedPageNormalRecordBytes)) || bodySize != int64(ref.PayloadSizeBytes) {
		return fmt.Errorf("imported page aux payload exceeds grid bound or differs from reference")
	}
	body := make([]byte, int(bodySize))
	if _, err = io.ReadFull(f, body); err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	if !importedPageHashEqual(hex.EncodeToString(hash[:]), ref.PayloadHash) {
		return fmt.Errorf("imported page aux payload hash mismatch")
	}
	count := uint64(binary.LittleEndian.Uint32(body[:4]))
	if count > uint64(maxRecords) || count > uint64((len(body)-4)/16) {
		return fmt.Errorf("imported page aux record count exceeds grid/body bound")
	}
	seen := make(map[[3]int32]bool, int(count))
	offset := 4
	for i := uint64(0); i < count; i++ {
		if len(body)-offset < 16 {
			return fmt.Errorf("imported page aux record is truncated")
		}
		var origin [3]int32
		for axis := 0; axis < 3; axis++ {
			origin[axis] = int32(binary.LittleEndian.Uint32(body[offset : offset+4]))
			offset += 4
			if origin[axis] < 0 || int(origin[axis]) >= q.side || int(origin[axis])%importedPageCostBrickSide != 0 {
				return fmt.Errorf("imported page aux record origin is outside grid or unaligned")
			}
		}
		if seen[origin] {
			return fmt.Errorf("imported page aux duplicate record origin")
		}
		seen[origin] = true
		size := binary.LittleEndian.Uint32(body[offset : offset+4])
		offset += 4
		if size != importedPageNormalRecordBytes || len(body)-offset < int(size) {
			return fmt.Errorf("imported page aux normal record shape mismatch")
		}
		offset += int(size)
	}
	if offset != len(body) {
		return fmt.Errorf("imported page aux trailing bytes")
	}
	// Decode the same qualified bytes only after all count/length checks, avoiding
	// allocation controlled by malformed record counts in the legacy decoder.
	_, err = decodeImportedWorldChunkAuxPayload(body)
	return err
}
