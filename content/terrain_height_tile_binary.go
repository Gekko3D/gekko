package content

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type TerrainHeightTileSaveResult struct {
	PayloadHash      string
	PayloadSizeBytes int
}

type terrainHeightTileMetadata struct {
	TerrainHeightTileDef
	PayloadKind                  string `json:"payload_kind"`
	PayloadHash                  string `json:"payload_hash"`
	PayloadSizeBytes             int    `json:"payload_size_bytes"`
	SurfaceMaskBytes             int    `json:"surface_mask_bytes"`
	OutdoorNavExclusionMaskBytes int    `json:"outdoor_nav_exclusion_mask_bytes"`
}

func terrainHeightTilePayload(d *TerrainHeightTileDef) ([]byte, TerrainHeightTileSaveResult) {
	raw := make([]byte, 2*len(d.HeightSamples)+len(d.SurfaceMask)+len(d.OutdoorNavExclusionMask))
	for i, v := range d.HeightSamples {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	offset := 2 * len(d.HeightSamples)
	copy(raw[offset:], d.SurfaceMask)
	copy(raw[offset+len(d.SurfaceMask):], d.OutdoorNavExclusionMask)
	hash := sha256.Sum256(raw)
	return raw, TerrainHeightTileSaveResult{PayloadHash: hex.EncodeToString(hash[:]), PayloadSizeBytes: len(raw)}
}

func SaveTerrainHeightTile(path string, d *TerrainHeightTileDef) (TerrainHeightTileSaveResult, error) {
	if err := ValidateTerrainHeightTile(d); err != nil {
		return TerrainHeightTileSaveResult{}, err
	}
	raw, result := terrainHeightTilePayload(d)
	m := terrainHeightTileMetadata{TerrainHeightTileDef: *d, PayloadKind: TerrainHeightTilePayloadKind, PayloadHash: result.PayloadHash, PayloadSizeBytes: result.PayloadSizeBytes, SurfaceMaskBytes: len(d.SurfaceMask), OutdoorNavExclusionMaskBytes: len(d.OutdoorNavExclusionMask)}
	metadata, err := json.Marshal(m)
	if err != nil {
		return TerrainHeightTileSaveResult{}, err
	}
	if len(metadata) == 0 || len(metadata) > 65536 {
		return TerrainHeightTileSaveResult{}, fmt.Errorf("terrain height tile metadata exceeds limit")
	}
	frame := make([]byte, 12+len(metadata)+len(raw))
	copy(frame, "GKHTIL1\n")
	binary.LittleEndian.PutUint32(frame[8:12], uint32(len(metadata)))
	copy(frame[12:], metadata)
	copy(frame[12+len(metadata):], raw)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return TerrainHeightTileSaveResult{}, err
	}
	if err := os.WriteFile(path, frame, 0644); err != nil {
		return TerrainHeightTileSaveResult{}, err
	}
	return result, nil
}

func loadTerrainHeightTile(path string) (*TerrainHeightTileDef, TerrainHeightTileSaveResult, error) {
	fail := func(err error) (*TerrainHeightTileDef, TerrainHeightTileSaveResult, error) {
		return nil, TerrainHeightTileSaveResult{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return fail(err)
	}
	defer f.Close()
	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return fail(err)
	}
	if string(header[:8]) != "GKHTIL1\n" {
		return fail(fmt.Errorf("invalid terrain height tile magic"))
	}
	n := binary.LittleEndian.Uint32(header[8:])
	if n == 0 || n > 65536 {
		return fail(fmt.Errorf("invalid terrain height tile metadata length"))
	}
	metadata := make([]byte, int(n))
	if _, err := io.ReadFull(f, metadata); err != nil {
		return fail(err)
	}
	var m terrainHeightTileMetadata
	if err := json.Unmarshal(metadata, &m); err != nil {
		return fail(err)
	}
	if m.PayloadKind != TerrainHeightTilePayloadKind || !terrainPayloadHashValid(m.PayloadHash) {
		return fail(fmt.Errorf("invalid terrain height tile payload identity"))
	}
	if err := validateTerrainHeightTileMetadata(&m.TerrainHeightTileDef, m.SurfaceMaskBytes, m.OutdoorNavExclusionMaskBytes); err != nil {
		return fail(err)
	}
	expected := 2*m.SampleWidth*m.SampleHeight + m.SurfaceMaskBytes + m.OutdoorNavExclusionMaskBytes
	if m.PayloadSizeBytes != expected || expected > terrainHeightTileMaxPayload {
		return fail(fmt.Errorf("invalid terrain height tile payload length"))
	}
	raw := make([]byte, expected)
	if _, err := io.ReadFull(f, raw); err != nil {
		return fail(err)
	}
	var trailing [1]byte
	if _, err := io.ReadFull(f, trailing[:]); err != io.EOF {
		return fail(fmt.Errorf("terrain height tile trailing data or read error: %v", err))
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != m.PayloadHash {
		return fail(fmt.Errorf("terrain height tile payload hash mismatch"))
	}
	d := m.TerrainHeightTileDef
	count := d.SampleWidth * d.SampleHeight
	d.HeightSamples = make([]uint16, count)
	for i := range d.HeightSamples {
		d.HeightSamples[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	offset := 2 * count
	if m.SurfaceMaskBytes > 0 {
		d.SurfaceMask = append([]byte(nil), raw[offset:offset+m.SurfaceMaskBytes]...)
	}
	offset += m.SurfaceMaskBytes
	if m.OutdoorNavExclusionMaskBytes > 0 {
		d.OutdoorNavExclusionMask = append([]byte(nil), raw[offset:]...)
	}
	if err := ValidateTerrainHeightTile(&d); err != nil {
		return fail(err)
	}
	return &d, TerrainHeightTileSaveResult{PayloadHash: m.PayloadHash, PayloadSizeBytes: expected}, nil
}

func LoadTerrainHeightTile(path string) (*TerrainHeightTileDef, error) {
	d, _, err := loadTerrainHeightTile(path)
	return d, err
}

func LoadTerrainHeightTileEntry(entry TerrainChunkEntryDef, manifestPath string) (*TerrainHeightTileDef, error) {
	if err := validateTerrainHeightTileEntry(entry); err != nil {
		return nil, err
	}
	d, result, err := loadTerrainHeightTile(ResolveTerrainChunkPath(entry, manifestPath))
	if err != nil {
		return nil, err
	}
	if d.TerrainID != entry.TerrainID || d.SourceHash != entry.SourceHash || d.Coord != entry.Coord || d.WorldOrigin != entry.WorldOrigin || d.SampleWidth != entry.ChunkSize || d.SampleHeight != entry.ChunkSize || d.SampleSpacing != entry.VoxelResolution || result.PayloadHash != entry.PayloadHash || result.PayloadSizeBytes != entry.PayloadSizeBytes {
		return nil, fmt.Errorf("terrain height tile reference mismatch")
	}
	return d, nil
}
