package content

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type importedPageQualification struct {
	worldID                            string
	coord                              TerrainChunkCoordDef
	side                               int
	resolution                         float32
	kind, hash                         string
	size, count, sectorCost, brickCost int
}

func (q importedPageQualification) header(world string, coord TerrainChunkCoordDef, side int, resolution float32, kind, hash string, size, count int) error {
	if world != q.worldID || coord != q.coord || side != q.side || resolution != q.resolution || kind != q.kind || !importedPageHashEqual(hash, q.hash) || size != q.size || count < 0 || count > q.side*q.side*q.side || (q.count >= 0 && count != q.count) {
		return fmt.Errorf("imported page payload owner/grid/identity/count mismatch")
	}
	return nil
}

// LoadImportedWorldPagePayload qualifies the existing checksummed decoder with
// manifest ownership before voxel expansion. Leaf pages may reuse full chunks;
// independent page chunks use local Coord zero and explicit page WorldOrigin.
func LoadImportedWorldPagePayload(manifest *ImportedWorldDef, manifestPath string, pageIndex uint32) (*ImportedWorldChunkDef, error) {
	if strings.TrimSpace(manifestPath) == "" {
		return nil, fmt.Errorf("imported page manifest path is empty")
	}
	if _, err := ValidateImportedWorldV3(manifest); err != nil {
		return nil, err
	}
	if uint64(pageIndex) >= uint64(len(manifest.Pages)) {
		return nil, fmt.Errorf("imported page index out of range")
	}
	p := manifest.Pages[pageIndex].Payload
	q := importedPageContext(manifest, p)
	return loadQualifiedImportedPage(ResolveDocumentPath(p.Path, manifestPath), q)
}

func importedPageContext(manifest *ImportedWorldDef, p StreamPagePayloadDef) importedPageQualification {
	q := importedPageQualification{worldID: manifest.WorldID, side: p.ChunkSize, resolution: p.VoxelResolution, kind: p.Kind, hash: p.PayloadHash, size: p.PayloadSizeBytes, count: -1, sectorCost: p.OccupiedSectorCount, brickCost: p.OccupiedBrickCount}
	for _, entry := range manifest.Entries {
		if cleanImportedPagePath(entry.ChunkPath) == cleanImportedPagePath(p.Path) {
			q.coord = entry.Coord
			q.count = entry.NonEmptyVoxelCount
			break
		}
	}
	return q
}

func loadQualifiedImportedPage(path string, q importedPageQualification) (*ImportedWorldChunkDef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var prefix [12]byte
	if _, err = f.ReadAt(prefix[:], 0); err != nil && err != io.EOF {
		return nil, err
	}
	var chunk *ImportedWorldChunkDef
	if string(prefix[:8]) == importedCompiledMagic {
		if q.kind != ImportedWorldChunkPayloadBrickZstdBinaryV1 {
			return nil, fmt.Errorf("imported page binary kind mismatch")
		}
		codec, err := defaultImportedCompiledCodec()
		if err != nil {
			return nil, err
		}
		doc, info, err := codec.ReadFrame(f, 0, stat.Size())
		if err != nil {
			return nil, err
		}
		var metadata importedWorldCompiledMetadata
		if err = json.Unmarshal(doc.Metadata, &metadata); err != nil {
			return nil, err
		}
		if err = q.header(metadata.WorldID, metadata.Coord, metadata.ChunkSize, metadata.VoxelResolution, ImportedWorldChunkPayloadBrickZstdBinaryV1, info.ContentID, int(info.DecodedBytes), metadata.NonEmptyVoxelCount); err != nil {
			return nil, err
		}
		chunk, err = importedWorldChunkFromCompiled(doc, info, codec)
		if err != nil {
			return nil, err
		}
	} else if bytes.Equal(prefix[:8], importedWorldChunkDenseRLEMagic) {
		metadataSize := binary.LittleEndian.Uint32(prefix[8:])
		if metadataSize == 0 || metadataSize > 65536 {
			return nil, fmt.Errorf("imported page RLE metadata exceeds bound")
		}
		metadata := make([]byte, int(metadataSize))
		if _, err = f.ReadAt(metadata, 12); err != nil {
			return nil, err
		}
		var m importedWorldChunkBinaryMetadata
		if err = json.Unmarshal(metadata, &m); err != nil {
			return nil, err
		}
		if m.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
			return nil, fmt.Errorf("imported page chunk schema mismatch")
		}
		if err = q.header(m.WorldID, m.Coord, m.ChunkSize, m.VoxelResolution, m.PayloadKind, m.PayloadHash, m.PayloadSizeBytes, m.NonEmptyVoxelCount); err != nil {
			return nil, err
		}
		runBytes := 5
		if q.kind == ImportedWorldChunkPayloadDenseRLEMaterialBinaryV1 {
			runBytes = 6
		} else if q.kind != ImportedWorldChunkPayloadDenseRLEBinaryV1 {
			return nil, fmt.Errorf("imported page RLE kind mismatch")
		}
		cells := q.side * q.side * q.side
		maxBytes := 4 + runBytes*cells
		if q.size < 4 || q.size > maxBytes || stat.Size() != 12+int64(metadataSize)+int64(q.size) {
			return nil, fmt.Errorf("imported page RLE body size mismatch")
		}
		raw := make([]byte, q.size)
		if _, err = f.ReadAt(raw, 12+int64(metadataSize)); err != nil {
			return nil, err
		}
		if err = qualifyImportedRLERuns(raw, runBytes, cells, m.NonEmptyVoxelCount); err != nil {
			return nil, err
		}
		// Decode the same qualified header and body; do not reopen an unqualified path.
		frame := make([]byte, 12+len(metadata)+len(raw))
		copy(frame, prefix[:])
		copy(frame[12:], metadata)
		copy(frame[12+len(metadata):], raw)
		chunk, err = loadImportedWorldChunkDenseRLEBinary(frame)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("imported page payload requires an existing checksummed binary codec")
	}
	if err = qualifyImportedOccupancy(chunk, q); err != nil {
		return nil, err
	}
	return chunk, nil
}

func qualifyImportedRLERuns(raw []byte, runBytes, cells, nonEmpty int) error {
	if len(raw) < 4 {
		return fmt.Errorf("truncated imported page RLE")
	}
	runs := uint64(binary.LittleEndian.Uint32(raw[:4]))
	if runs > uint64(cells) || 4+runs*uint64(runBytes) != uint64(len(raw)) {
		return fmt.Errorf("invalid imported page RLE run count")
	}
	cursor, occupied := uint64(0), uint64(0)
	for i := uint64(0); i < runs; i++ {
		offset := 4 + int(i)*runBytes
		length := uint64(binary.LittleEndian.Uint32(raw[offset+runBytes-4 : offset+runBytes]))
		if length == 0 || cursor+length > uint64(cells) {
			return fmt.Errorf("imported page RLE run exceeds grid")
		}
		cursor += length
		if raw[offset] != 0 {
			occupied += length
		}
	}
	if cursor != uint64(cells) || occupied != uint64(nonEmpty) {
		return fmt.Errorf("imported page RLE count mismatch")
	}
	return nil
}

// Occupancy costs use the xbrickmap publication grid (volume.SectorSize and
// volume.BrickSize); keep the pure content layer independent of the renderer.
const importedPageCostSectorSide, importedPageCostBrickSide = 32, 8

func qualifyImportedOccupancy(chunk *ImportedWorldChunkDef, q importedPageQualification) error {
	if err := q.header(chunk.WorldID, chunk.Coord, chunk.ChunkSize, chunk.VoxelResolution, chunk.PayloadKind, chunk.PayloadHash, chunk.PayloadSizeBytes, chunk.NonEmptyVoxelCount); err != nil {
		return err
	}
	sectors, bricks := map[[3]int]bool{}, map[[3]int]bool{}
	count := 0
	seen := make(map[[3]int]bool, len(chunk.Voxels))
	for _, v := range chunk.Voxels {
		if v.Value == 0 {
			continue
		}
		coord := [3]int{v.X, v.Y, v.Z}
		if v.X < 0 || v.Y < 0 || v.Z < 0 || v.X >= q.side || v.Y >= q.side || v.Z >= q.side || seen[coord] {
			return fmt.Errorf("invalid qualified imported occupancy")
		}
		seen[coord] = true
		count++
		sectors[[3]int{v.X / importedPageCostSectorSide, v.Y / importedPageCostSectorSide, v.Z / importedPageCostSectorSide}] = true
		bricks[[3]int{v.X / importedPageCostBrickSide, v.Y / importedPageCostBrickSide, v.Z / importedPageCostBrickSide}] = true
	}
	if count != chunk.NonEmptyVoxelCount || (q.sectorCost > 0 && len(sectors) != q.sectorCost) || (q.brickCost > 0 && len(bricks) != q.brickCost) {
		return fmt.Errorf("imported page occupied cost/count mismatch")
	}
	return nil
}

func validateImportedWorldV3Files(d *ImportedWorldDef, opts ImportedWorldValidationOptions) ImportedWorldValidationResult {
	result := ImportedWorldValidationResult{}
	if _, err := ValidateImportedWorldV3(d); err != nil {
		result.addError("invalid_imported_page_world", err.Error())
		return result
	}
	if opts.DocumentPath == "" {
		return result
	}
	for _, entry := range d.Entries {
		q := importedPageQualification{worldID: d.WorldID, coord: entry.Coord, side: d.ChunkSize, resolution: d.VoxelResolution, kind: entry.PayloadKind, hash: entry.PayloadHash, size: entry.PayloadSizeBytes, count: entry.NonEmptyVoxelCount, sectorCost: entry.OccupiedSectorCount, brickCost: entry.OccupiedBrickCount}
		if _, err := loadQualifiedImportedPage(ResolveImportedWorldChunkPath(entry, opts.DocumentPath), q); err != nil {
			result.addError("invalid_imported_page_leaf", err.Error())
		}
		if entry.Aux != nil {
			validateImportedPageAuxFile(&result, opts.DocumentPath, entry.Aux, q)
		}
	}
	for _, page := range d.Pages {
		q := importedPageContext(d, page.Payload)
		if _, err := loadQualifiedImportedPage(ResolveDocumentPath(page.Payload.Path, opts.DocumentPath), q); err != nil {
			result.addError("invalid_imported_page_payload", err.Error())
		}
		if page.Payload.Aux != nil {
			validateImportedPageAuxFile(&result, opts.DocumentPath, page.Payload.Aux, q)
		}
	}
	return result
}
func validateImportedPageAuxFile(result *ImportedWorldValidationResult, manifestPath string, ref *ImportedWorldChunkAuxRefDef, q importedPageQualification) {
	if err := loadQualifiedImportedPageAux(ResolveDocumentPath(ref.AuxPath, manifestPath), q, ref); err != nil {
		result.addError("invalid_imported_page_aux", err.Error())
	}
}
