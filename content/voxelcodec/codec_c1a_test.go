package voxelcodec_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"math"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/klauspost/compress/zstd"
)

func c1aCodec(t *testing.T, options voxelcodec.Options) *voxelcodec.Codec {
	t.Helper()
	c, err := voxelcodec.New(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	return c
}

func c1aDocument() voxelcodec.Document {
	aux := make([]byte, 1088)
	for i := range aux {
		aux[i] = byte(i)
	}
	return voxelcodec.Document{Kind: "test", Metadata: []byte{0, 255, 1}, NormalBakeVersion: "fitted-v1", Bricks: []voxelcodec.Brick{
		{Coord: [3]int32{3, -1, 0}, Occupancy: [8]uint64{3}, Values: []uint8{4, 4}, Materials: []uint8{0, 0}, Aux: []byte{}},
		{Coord: [3]int32{-4, 2, 7}, Occupancy: [8]uint64{1 << 63, 1, 0, 0, 0, 0, 0, 1 << 63}, Values: []uint8{255, 7, 1}, Materials: []uint8{0, 7, 255}, Aux: aux},
		{Coord: [3]int32{0, 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{1}},
	}}
}

func TestC1aLosslessCanonicalLayersAndOwnedResults(t *testing.T) {
	c := c1aCodec(t, voxelcodec.Options{})
	doc := c1aDocument()
	frame, info, err := c.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc, c1aDocument()) {
		t.Fatal("encoding mutated caller ordering or layers")
	}
	reordered := c1aDocument()
	slices.Reverse(reordered.Bricks)
	other, otherInfo, err := c.Encode(reordered)
	if err != nil || !bytes.Equal(frame, other) || info.ContentID != otherInfo.ContentID {
		t.Fatal("canonical ordering changed frame or content identity")
	}
	decoded, decodedInfo, err := c.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	want := c1aDocument()
	want.Bricks = []voxelcodec.Brick{want.Bricks[1], want.Bricks[2], want.Bricks[0]}
	if !reflect.DeepEqual(decoded, want) || decodedInfo != info || int64(info.EncodedBytes) != int64(len(frame)) || info.DictionaryID != 0 || info.DictionaryHash != "" {
		t.Fatal("round trip changed raw channels, optional layers, metadata or frame information")
	}
	var zheader zstd.Header
	if err := zheader.Decode(frame[96:]); err != nil || zheader.Skippable || !zheader.HasCheckSum || !zheader.HasFCS || zheader.DictionaryID != 0 || zheader.FrameContentSize != uint64(info.DecodedBytes) {
		t.Fatal("ordinary checksum/content-size frame header disagrees with codec information")
	}
	for _, change := range []func(*voxelcodec.Document){func(d *voxelcodec.Document) { d.Kind = "other" }, func(d *voxelcodec.Document) { d.Metadata[0] = 3 }, func(d *voxelcodec.Document) { d.NormalBakeVersion = "fitted-v2" }, func(d *voxelcodec.Document) { d.Bricks[0].Aux = nil }} {
		changed := c1aDocument()
		change(&changed)
		_, changedInfo, err := c.Encode(changed)
		if err != nil || changedInfo.ContentID == info.ContentID {
			t.Fatal("identity omitted a logical metadata/layer distinction")
		}
	}
	decoded.Bricks[0].Values[0] = 2
	again, _, err := c.Decode(frame)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal("later decode reused caller-owned output slices")
	}
	if _, _, err := c.Encode(voxelcodec.Document{Kind: "empty"}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(frame, other) {
		t.Fatal("later operations changed retained encoded bytes")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal("Close is not idempotent")
	}
	if _, _, err := c.Encode(doc); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("Encode after Close did not return ErrClosed")
	}
	if _, _, err := c.Decode(frame); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("Decode after Close did not return ErrClosed")
	}
	if _, _, err := c.ReadFrame(bytes.NewReader(frame), 0, int64(len(frame))); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("ReadFrame after Close did not return ErrClosed")
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatal("Close invalidated retained decoded output")
	}
}

func TestC1aDictionaryIdentityCopyAndOptionalUse(t *testing.T) {
	history := bytes.Repeat([]byte("voxel dictionary history "), 32)
	wantHash := sha256.Sum256(history)
	dictionary := &voxelcodec.Dictionary{ID: 71, Bytes: slices.Clone(history)}
	c := c1aCodec(t, voxelcodec.Options{Dictionary: dictionary})
	dictionary.Bytes[0] ^= 255
	doc := c1aDocument()
	doc.Metadata = slices.Clone(history) // Matching history makes this a dictionary-use fixture.
	frame, info, err := c.Encode(doc)
	if err != nil || info.DictionaryID != 71 || info.DictionaryHash != hex.EncodeToString(wantHash[:]) {
		t.Fatal("codec did not own dictionary bytes or report their full identity")
	}
	var header zstd.Header
	if err := header.Decode(frame[96:]); err != nil || header.DictionaryID != info.DictionaryID || !header.HasCheckSum || !header.HasFCS || header.FrameContentSize != uint64(info.DecodedBytes) {
		t.Fatal("actual zstd dictionary/checksum/content-size fields disagree with owner header")
	}
	plain := c1aCodec(t, voxelcodec.Options{})
	plainFrame, plainInfo, err := plain.Encode(doc)
	if err != nil || plainInfo.ContentID != info.ContentID {
		t.Fatal("dictionary choice changed logical content identity")
	}
	if _, _, err := c.Decode(plainFrame); err != nil {
		t.Fatal("configured dictionary prevented dictionary-free decode")
	}
	correct := c1aCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 71, Bytes: history}})
	if _, _, err := correct.Decode(frame); err != nil {
		t.Fatal(err)
	}
	for _, options := range []voxelcodec.Options{{}, {Dictionary: &voxelcodec.Dictionary{ID: 72, Bytes: history}}, {Dictionary: &voxelcodec.Dictionary{ID: 71, Bytes: bytes.Repeat([]byte{9}, len(history))}}} {
		c1aDecodeFails(t, c1aCodec(t, options), frame)
	}
	if _, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{Bytes: history}}); err == nil {
		t.Fatal("zero dictionary ID was accepted")
	}
	if _, err := voxelcodec.New(voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 71, Bytes: history}, Limits: voxelcodec.Limits{MaxDictionaryBytes: len(history) - 1}}); err == nil {
		t.Fatal("oversized caller dictionary was accepted")
	}
}

type c1aRangeReader struct {
	data       *bytes.Reader
	start, end int64
}

func (r c1aRangeReader) ReadAt(p []byte, offset int64) (int, error) {
	if offset < r.start || offset > r.end || int64(len(p)) > r.end-offset {
		return 0, errors.New("read escaped independent frame range")
	}
	return r.data.ReadAt(p, offset)
}

func TestC1aIndependentRangeAndFiniteLimits(t *testing.T) {
	c := c1aCodec(t, voxelcodec.Options{})
	frame, info, err := c.Encode(c1aDocument())
	if err != nil {
		t.Fatal(err)
	}
	container := append(bytes.Repeat([]byte{3}, 19), frame...)
	container = append(container, bytes.Repeat([]byte{4}, 23)...)
	reader := c1aRangeReader{bytes.NewReader(container), 19, 19 + int64(len(frame))}
	_, gotInfo, err := c.ReadFrame(reader, 19, int64(len(frame)))
	if err != nil || gotInfo != info {
		t.Fatal("bounded independent range could not be decoded")
	}
	for _, limits := range []voxelcodec.Limits{{MaxEncodedBytes: int64(len(frame) - 1)}, {MaxDecodedBytes: int64(info.DecodedBytes) - 1}, {MaxBricks: 2}, {MaxVoxels: 5}, {MaxMetadataBytes: 2}, {MaxAuxBytes: 256}, {MaxKindBytes: 3}, {MaxBakeVersionBytes: 8}, {Bounds: &voxelcodec.Bounds{Min: [3]int32{}, Max: [3]int32{9, 9, 9}}}} {
		c1aDecodeFails(t, c1aCodec(t, voxelcodec.Options{Limits: limits}), frame)
	}
	for _, limits := range []voxelcodec.Limits{{MaxEncodedBytes: -1}, {MaxVoxels: -1}, {Bounds: &voxelcodec.Bounds{Min: [3]int32{1, 0, 0}, Max: [3]int32{0, 0, 0}}}} {
		if _, err := voxelcodec.New(voxelcodec.Options{Limits: limits}); err == nil {
			t.Fatal("invalid limits were accepted")
		}
	}
	bound := &voxelcodec.Bounds{Min: [3]int32{-4, -1, 0}, Max: [3]int32{3, 2, 7}}
	bounded := c1aCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{Bounds: bound}})
	bound.Min, bound.Max = [3]int32{}, [3]int32{}
	if _, _, err := bounded.Decode(frame); err != nil {
		t.Fatal("New did not copy inclusive bounds")
	}
	if _, _, err := c.ReadFrame(reader, -1, int64(len(frame))); err == nil {
		t.Fatal("negative range was accepted")
	}
	if _, _, err := c.ReadFrame(reader, 19, -1); err == nil {
		t.Fatal("negative range length was accepted")
	}
	if _, _, err := c.ReadFrame(reader, math.MaxInt64, 96); err == nil {
		t.Fatal("overflowing range extent was accepted")
	}
	if _, _, err := c.ReadFrame(reader, 19, int64(len(frame))+1); err == nil {
		t.Fatal("inconsistent range extent was accepted")
	}
}

func c1aDecodeFails(t *testing.T, c *voxelcodec.Codec, frame []byte) {
	t.Helper()
	doc, info, err := c.Decode(frame)
	if err == nil || !reflect.DeepEqual(doc, voxelcodec.Document{}) || !reflect.DeepEqual(info, voxelcodec.Info{}) {
		t.Fatal("malformed/bounded frame returned success or partial results")
	}
}

// These fixtures encode the documented public v1 ABI, without codec internals.
func c1aBody(coords [][3]int32) []byte {
	var b bytes.Buffer
	for _, v := range []any{uint16(1), uint16(4), []byte("test"), uint32(0), uint16(0), uint32(len(coords))} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	for _, coord := range coords {
		binary.Write(&b, binary.LittleEndian, coord)
		binary.Write(&b, binary.LittleEndian, [8]uint64{3})
		b.Write([]byte{2, 0, 0, 0, 1, 2})
	}
	return b.Bytes()
}

func c1aWire(t *testing.T, body []byte, crc bool) []byte {
	t.Helper()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderCRC(crc), zstd.WithEncoderConcurrency(1), zstd.WithWindowSize(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	compressed := encoder.EncodeAll(body, nil)
	encoder.Close()
	header := make([]byte, 96)
	copy(header, "GKBRCK1\n")
	binary.LittleEndian.PutUint16(header[8:], 1)
	binary.LittleEndian.PutUint64(header[12:], uint64(len(compressed)))
	binary.LittleEndian.PutUint64(header[20:], uint64(len(body)))
	sum := sha256.Sum256(body)
	copy(header[64:], sum[:])
	return append(header, compressed...)
}

func TestC1aRejectsMalformedFramesAndNoncanonicalChannels(t *testing.T) {
	c := c1aCodec(t, voxelcodec.Options{})
	body := c1aBody([][3]int32{{}})
	valid := c1aWire(t, body, true)
	if _, _, err := c.Decode(valid); err != nil {
		t.Fatal("documented canonical fixture was rejected:", err)
	}
	for _, mutate := range []func([]byte){func(b []byte) { b[8] = 2 }, func(b []byte) { b[10] = 1 }, func(b []byte) { b[28] = 1 }, func(b []byte) { b[32] = 1 }, func(b []byte) { b[64] ^= 1 }, func(b []byte) { b[len(b)-1] ^= 1 }, func(b []byte) { binary.LittleEndian.PutUint64(b[20:], 1<<40) }} {
		bad := slices.Clone(valid)
		mutate(bad)
		c1aDecodeFails(t, c, bad)
	}
	for _, bad := range [][]byte{valid[:95], valid[:len(valid)-1], append(slices.Clone(valid), 0), c1aWire(t, body, false)} {
		c1aDecodeFails(t, c, bad)
	}
	for _, mutate := range []func([]byte){func(b []byte) { b[0] = 2 }, func(b []byte) { b[94] = 3 }, func(b []byte) { b[95] = 3 }, func(b []byte) { b[96] = 2 }, func(b []byte) { b[97] = 1 }, func(b []byte) { b[98] = 0 }, func(b []byte) { b[99] = 1 }} {
		bad := slices.Clone(body)
		mutate(bad)
		c1aDecodeFails(t, c, c1aWire(t, bad, true))
	}
	for _, coords := range [][][3]int32{{{}, {}}, {{1, 0, 0}, {0, 0, 0}}} {
		c1aDecodeFails(t, c, c1aWire(t, c1aBody(coords), true))
	}
	concat := append(slices.Clone(valid), valid[96:]...)
	binary.LittleEndian.PutUint64(concat[12:], uint64(len(concat)-96))
	c1aDecodeFails(t, c, concat)
	skippable := append(slices.Clone(valid[:96]), []byte{0x50, 0x2a, 0x4d, 0x18, 0, 0, 0, 0}...)
	binary.LittleEndian.PutUint64(skippable[12:], 8)
	c1aDecodeFails(t, c, skippable)
	if _, _, err := c.ReadFrame(bytes.NewReader(valid[:95]), 0, 96); err == nil {
		t.Fatal("truncated header range accepted")
	}
}

func TestC1aRejectsImpossibleCountsAndBoundedUniformExpansion(t *testing.T) {
	c := c1aCodec(t, voxelcodec.Options{})
	body := c1aBody([][3]int32{{}})
	for _, mutate := range []func([]byte){func(b []byte) { binary.LittleEndian.PutUint32(b[14:], math.MaxUint32) }, func(b []byte) { binary.LittleEndian.PutUint32(b[8:], math.MaxUint32) }, func(b []byte) { b[4] = 255 }, func(b []byte) { clear(b[30:94]) }} {
		bad := slices.Clone(body)
		mutate(bad)
		c1aDecodeFails(t, c, c1aWire(t, bad, true))
	}
	oversizedAux := slices.Clone(body)
	oversizedAux[96] = 1
	oversizedAux = append(oversizedAux, 255, 255, 255, 255)
	c1aDecodeFails(t, c, c1aWire(t, oversizedAux, true))
	uniform := slices.Clone(body)
	for offset := 30; offset < 94; offset += 8 {
		binary.LittleEndian.PutUint64(uniform[offset:], math.MaxUint64)
	}
	uniform[94], uniform[95], uniform[98], uniform[99] = 1, 1, 1, 0
	frame := c1aWire(t, uniform, true)
	doc, _, err := c.Decode(frame)
	if err != nil || len(doc.Bricks[0].Values) != 512 || len(doc.Bricks[0].Materials) != 512 {
		t.Fatal("canonical uniform channels did not expand exactly")
	}
	c1aDecodeFails(t, c1aCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxVoxels: 511}}), frame)
}

func TestC1aEncodeRejectsInvalidLogicalDocuments(t *testing.T) {
	c := c1aCodec(t, voxelcodec.Options{})
	for _, mutate := range []func(*voxelcodec.Document){
		func(d *voxelcodec.Document) { d.Bricks[1].Coord = d.Bricks[0].Coord },
		func(d *voxelcodec.Document) { d.Bricks[0].Occupancy = [8]uint64{} },
		func(d *voxelcodec.Document) { d.Bricks[0].Values = d.Bricks[0].Values[:1] },
		func(d *voxelcodec.Document) { d.Bricks[0].Values[0] = 0 },
		func(d *voxelcodec.Document) { d.Bricks[0].Materials = d.Bricks[0].Materials[:1] },
	} {
		doc := c1aDocument()
		mutate(&doc)
		frame, info, err := c.Encode(doc)
		if err == nil || frame != nil || !reflect.DeepEqual(info, voxelcodec.Info{}) {
			t.Fatal("invalid logical document returned success or partial encoded output")
		}
	}
}

func TestC1aConcurrentEncodeDecodeAndTerminalClose(t *testing.T) {
	c := c1aCodec(t, voxelcodec.Options{})
	frame, info, err := c.Encode(c1aDocument())
	if err != nil {
		t.Fatal(err)
	}
	want, _, err := c.Decode(frame)
	if err != nil {
		t.Fatal(err)
	}
	firstPhase := make(chan error, 8)
	firstStart := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func(encode bool) {
			<-firstStart
			if encode {
				got, gotInfo, err := c.Encode(c1aDocument())
				if err == nil && (!bytes.Equal(got, frame) || gotInfo != info) {
					err = errors.New("concurrent encode changed logical result")
				}
				firstPhase <- err
			} else {
				got, gotInfo, err := c.Decode(frame)
				if err == nil && (!reflect.DeepEqual(got, want) || gotInfo != info) {
					err = errors.New("concurrent decode changed owned result")
				}
				firstPhase <- err
			}
		}(i%2 == 0)
	}
	close(firstStart)
	for range 8 {
		if err := <-firstPhase; err != nil {
			t.Fatal("concurrent reuse before Close failed:", err)
		}
	}
	start := make(chan struct{})
	errorsSeen := make(chan error, 12)
	var workers sync.WaitGroup
	for i := 0; i < 12; i++ {
		workers.Add(1)
		go func(operation int) {
			defer workers.Done()
			<-start
			var err error
			switch operation % 3 {
			case 0:
				_, _, err = c.Encode(c1aDocument())
			case 1:
				_, _, err = c.Decode(frame)
			case 2:
				err = c.Close()
			}
			errorsSeen <- err
		}(i)
	}
	close(start)
	workers.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil && !errors.Is(err, voxelcodec.ErrClosed) {
			t.Fatal("concurrent operation returned an unexpected failure:", err)
		}
	}
	if _, _, err := c.Decode(frame); !errors.Is(err, voxelcodec.ErrClosed) {
		t.Fatal("concurrent Close was not terminal")
	}
}
