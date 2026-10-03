package voxelcodec

import (
	"encoding/binary"
	"fmt"
	"math/bits"
	"sort"
	"unicode/utf8"
)

func occupied(o [8]uint64) int {
	n := 0
	for _, word := range o {
		n += bits.OnesCount64(word)
	}
	return n
}
func channelMode(values []uint8) byte {
	if values == nil {
		return 0
	}
	for _, v := range values {
		if v != values[0] {
			return 2
		}
	}
	return 1
}
func channelSize(values []uint8) int {
	if values == nil {
		return 0
	}
	if channelMode(values) == 1 {
		return 1
	}
	return len(values)
}

func encodeBody(doc Document, l Limits) ([]byte, error) {
	if len(doc.Kind) == 0 || len(doc.Kind) > l.MaxKindBytes || !utf8.ValidString(doc.Kind) || len(doc.NormalBakeVersion) > l.MaxBakeVersionBytes || !utf8.ValidString(doc.NormalBakeVersion) || len(doc.Metadata) > l.MaxMetadataBytes || len(doc.Bricks) > l.MaxBricks {
		return nil, fmt.Errorf("document exceeds metadata/brick limits")
	}
	size := int64(14) + int64(len(doc.Kind)) + int64(len(doc.Metadata)) + int64(len(doc.NormalBakeVersion))
	if size > l.MaxDecodedBytes {
		return nil, fmt.Errorf("document exceeds decoded limit")
	}
	voxels := 0
	for _, b := range doc.Bricks {
		n := occupied(b.Occupancy)
		if n == 0 || n > l.MaxVoxels-voxels || len(b.Values) != n || (b.Materials != nil && len(b.Materials) != n) || len(b.Aux) > l.MaxAuxBytes || !l.inBounds(b.Coord) {
			return nil, fmt.Errorf("invalid brick cardinality/bounds")
		}
		for _, value := range b.Values {
			if value == 0 {
				return nil, fmt.Errorf("occupied primary value is zero")
			}
		}
		voxels += n
		brickSize := int64(80) + int64(channelSize(b.Values)) + int64(channelSize(b.Materials))
		if b.Aux != nil {
			brickSize += 4 + int64(len(b.Aux))
		}
		if brickSize > l.MaxDecodedBytes-size {
			return nil, fmt.Errorf("document exceeds decoded limit")
		}
		size += brickSize
	}
	if size > l.MaxDecodedBytes {
		return nil, fmt.Errorf("document exceeds decoded limit")
	}
	bricks := append([]Brick(nil), doc.Bricks...)
	sort.Slice(bricks, func(i, j int) bool { return coordLess(bricks[i].Coord, bricks[j].Coord) })
	for i := 1; i < len(bricks); i++ {
		if bricks[i].Coord == bricks[i-1].Coord {
			return nil, fmt.Errorf("duplicate brick coordinate")
		}
	}
	out := make([]byte, 0, int(size))
	u16 := func(v uint16) { out = binary.LittleEndian.AppendUint16(out, v) }
	u32 := func(v uint32) { out = binary.LittleEndian.AppendUint32(out, v) }
	u16(version)
	u16(uint16(len(doc.Kind)))
	out = append(out, doc.Kind...)
	u32(uint32(len(doc.Metadata)))
	out = append(out, doc.Metadata...)
	u16(uint16(len(doc.NormalBakeVersion)))
	out = append(out, doc.NormalBakeVersion...)
	u32(uint32(len(bricks)))
	for _, b := range bricks {
		for _, coord := range b.Coord {
			u32(uint32(coord))
		}
		for _, word := range b.Occupancy {
			out = binary.LittleEndian.AppendUint64(out, word)
		}
		primary, secondary, aux := channelMode(b.Values), channelMode(b.Materials), byte(0)
		if b.Aux != nil {
			aux = 1
		}
		out = append(out, primary, secondary, aux, 0)
		if primary == 1 {
			out = append(out, b.Values[0])
		} else {
			out = append(out, b.Values...)
		}
		if secondary == 1 {
			out = append(out, b.Materials[0])
		} else if secondary == 2 {
			out = append(out, b.Materials...)
		}
		if aux == 1 {
			u32(uint32(len(b.Aux)))
			out = append(out, b.Aux...)
		}
	}
	return out, nil
}

type bodyReader struct {
	data   []byte
	offset int
}

func (r *bodyReader) read(n int) ([]byte, error) {
	if n < 0 || n > len(r.data)-r.offset {
		return nil, fmt.Errorf("truncated document")
	}
	data := r.data[r.offset : r.offset+n]
	r.offset += n
	return data, nil
}
func (r *bodyReader) number(n int) (uint64, error) {
	data, err := r.read(n)
	if err != nil {
		return 0, err
	}
	switch n {
	case 2:
		return uint64(binary.LittleEndian.Uint16(data)), nil
	case 4:
		return uint64(binary.LittleEndian.Uint32(data)), nil
	case 8:
		return binary.LittleEndian.Uint64(data), nil
	}
	panic("invalid number width")
}
func (r *bodyReader) layer(width, limit int) ([]byte, error) {
	n, err := r.number(width)
	if err != nil {
		return nil, err
	}
	if n > uint64(limit) || n > uint64(len(r.data)-r.offset) {
		return nil, fmt.Errorf("invalid layer length")
	}
	return r.read(int(n))
}
func (r *bodyReader) channel(mode byte, n int, primary bool) ([]byte, error) {
	if mode == 0 && !primary {
		return nil, nil
	}
	if mode != 1 && mode != 2 {
		return nil, fmt.Errorf("invalid channel mode")
	}
	length := n
	if mode == 1 {
		length = 1
	}
	raw, err := r.read(length)
	if err != nil {
		return nil, err
	}
	if primary {
		for _, value := range raw {
			if value == 0 {
				return nil, fmt.Errorf("occupied primary value is zero")
			}
		}
	}
	if mode == 2 && channelMode(raw) != 2 {
		return nil, fmt.Errorf("noncanonical mixed channel")
	}
	out := make([]byte, n)
	if mode == 1 {
		for i := range out {
			out[i] = raw[0]
		}
	} else {
		copy(out, raw)
	}
	return out, nil
}

func decodeBody(data []byte, l Limits) (Document, error) {
	r := bodyReader{data: data}
	v, err := r.number(2)
	if err != nil || v != version {
		return Document{}, fmt.Errorf("invalid document version")
	}
	kind, err := r.layer(2, l.MaxKindBytes)
	if err != nil || len(kind) == 0 || !utf8.Valid(kind) {
		return Document{}, fmt.Errorf("invalid kind")
	}
	metadata, err := r.layer(4, l.MaxMetadataBytes)
	if err != nil {
		return Document{}, err
	}
	bake, err := r.layer(2, l.MaxBakeVersionBytes)
	if err != nil || !utf8.Valid(bake) {
		return Document{}, fmt.Errorf("invalid bake version")
	}
	count, err := r.number(4)
	if err != nil || count > uint64(l.MaxBricks) || count > uint64((len(data)-r.offset)/81) {
		return Document{}, fmt.Errorf("invalid brick count")
	}
	doc := Document{Kind: string(kind), Metadata: append([]byte(nil), metadata...), NormalBakeVersion: string(bake)}
	if count > 0 {
		doc.Bricks = make([]Brick, 0, int(count))
	}
	voxels := 0
	for i := uint64(0); i < count; i++ {
		var b Brick
		for axis := range b.Coord {
			n, e := r.number(4)
			if e != nil {
				return Document{}, e
			}
			b.Coord[axis] = int32(uint32(n))
		}
		if !l.inBounds(b.Coord) || (len(doc.Bricks) > 0 && !coordLess(doc.Bricks[len(doc.Bricks)-1].Coord, b.Coord)) {
			return Document{}, fmt.Errorf("noncanonical/out-of-bounds coordinate")
		}
		for word := range b.Occupancy {
			n, e := r.number(8)
			if e != nil {
				return Document{}, e
			}
			b.Occupancy[word] = n
		}
		n := occupied(b.Occupancy)
		if n == 0 || n > l.MaxVoxels-voxels {
			return Document{}, fmt.Errorf("invalid occupied count")
		}
		voxels += n
		modes, e := r.read(4)
		if e != nil {
			return Document{}, e
		}
		if modes[2] > 1 || modes[3] != 0 {
			return Document{}, fmt.Errorf("invalid layer/reserved mode")
		}
		b.Values, e = r.channel(modes[0], n, true)
		if e != nil {
			return Document{}, e
		}
		b.Materials, e = r.channel(modes[1], n, false)
		if e != nil {
			return Document{}, e
		}
		if modes[2] == 1 {
			raw, e := r.layer(4, l.MaxAuxBytes)
			if e != nil {
				return Document{}, e
			}
			b.Aux = make([]byte, len(raw))
			copy(b.Aux, raw)
		}
		doc.Bricks = append(doc.Bricks, b)
	}
	if r.offset != len(data) {
		return Document{}, fmt.Errorf("trailing document bytes")
	}
	return doc, nil
}
