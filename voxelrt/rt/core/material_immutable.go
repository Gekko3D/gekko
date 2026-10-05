package core

import (
	"encoding/binary"
	"errors"
	"math"
)

// ImmutableMaterialTable owns a sealed local material table and its complete
// semantic identity. Public object tables remain independently editable copies.
type ImmutableMaterialTable struct {
	rows     []Material
	identity string
}

// NewImmutableMaterialTable seals rows together with an opaque, complete semantic
// key supplied by the owner. Local material addressing permits at most 256 rows.
func NewImmutableMaterialTable(rows []Material, semanticKey string) (*ImmutableMaterialTable, error) {
	if semanticKey == "" {
		return nil, errors.New("immutable material table requires a semantic key")
	}
	if len(rows) > 256 {
		return nil, errors.New("immutable material table exceeds 256 rows")
	}
	owned := append([]Material(nil), rows...)
	// Length prefixes keep arbitrary opaque keys and row encodings unambiguous.
	encoded := make([]byte, 16+len(semanticKey)+40*len(owned))
	binary.LittleEndian.PutUint64(encoded, uint64(len(semanticKey)))
	copy(encoded[8:], semanticKey)
	offset := 8 + len(semanticKey)
	binary.LittleEndian.PutUint64(encoded[offset:], uint64(len(owned)))
	offset += 8
	for _, row := range owned {
		for _, bits := range materialExactBits(row) {
			binary.LittleEndian.PutUint32(encoded[offset:], bits)
			offset += 4
		}
	}
	return &ImmutableMaterialTable{rows: owned, identity: string(encoded)}, nil
}

// MaterialTable returns an independent copy, never the sealed backing storage.
func (table *ImmutableMaterialTable) MaterialTable() []Material {
	if table == nil {
		return nil
	}
	return append([]Material(nil), table.rows...)
}

// Identity is an exact opaque encoding of semantic ownership and all row bits.
func (table *ImmutableMaterialTable) Identity() string {
	if table == nil {
		return ""
	}
	return table.identity
}

// Matches preserves every float bit, including signed zero and NaN payloads.
func (table *ImmutableMaterialTable) Matches(rows []Material) bool {
	if table == nil || table.identity == "" || len(rows) != len(table.rows) {
		return false
	}
	for i, row := range rows {
		if materialExactBits(row) != materialExactBits(table.rows[i]) {
			return false
		}
	}
	return true
}

func materialExactBits(row Material) [10]uint32 {
	colorBits := func(color [4]uint8) uint32 {
		return uint32(color[0]) | uint32(color[1])<<8 | uint32(color[2])<<16 | uint32(color[3])<<24
	}
	return [10]uint32{
		colorBits(row.BaseColor), colorBits(row.Emissive),
		math.Float32bits(row.Emission), math.Float32bits(row.Transmission),
		math.Float32bits(row.Density), math.Float32bits(row.Refraction),
		math.Float32bits(row.Roughness), math.Float32bits(row.Metalness),
		math.Float32bits(row.IOR), math.Float32bits(row.Transparency),
	}
}

// SetImmutableMaterialTable installs independent legacy rows. A nil or unsealed
// handle clears certification while preserving the current public table.
func (obj *VoxelObject) SetImmutableMaterialTable(table *ImmutableMaterialTable) {
	if obj == nil {
		return
	}
	obj.immutableMaterialTable = nil
	if table == nil || table.identity == "" {
		return
	}
	obj.MaterialTable = table.MaterialTable()
	obj.immutableMaterialTable = table
}

// ImmutableMaterialTable qualifies the current public rows. A mismatch detaches
// certification until the owner explicitly installs another sealed binding.
func (obj *VoxelObject) ImmutableMaterialTable() *ImmutableMaterialTable {
	if obj == nil {
		return nil
	}
	if table := obj.immutableMaterialTable; table != nil && !table.Matches(obj.MaterialTable) {
		obj.immutableMaterialTable = nil
	}
	return obj.immutableMaterialTable
}
