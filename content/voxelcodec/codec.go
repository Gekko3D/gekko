package voxelcodec

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/klauspost/compress/zstd"
)

type Codec struct {
	lifetime           sync.RWMutex
	encodeMu, decodeMu sync.Mutex
	closed             bool
	limits             Limits
	encoder            *zstd.Encoder
	decoder            *zstd.Decoder
	dictionaryID       uint32
	dictionaryHash     [32]byte
}

func New(options Options) (*Codec, error) {
	limits, err := normalizedLimits(options.Limits)
	if err != nil {
		return nil, err
	}
	c := &Codec{limits: limits}
	encOptions := []zstd.EOption{zstd.WithEncoderCRC(true), zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithWindowSize(1 << 20)}
	decOptions := []zstd.DOption{zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max(limits.MaxDecodedBytes, 1<<20))), zstd.WithDecoderMaxWindow(1 << 20), zstd.WithDecodeAllCapLimit(true)}
	if d := options.Dictionary; d != nil {
		if d.ID == 0 || len(d.Bytes) == 0 || len(d.Bytes) > limits.MaxDictionaryBytes {
			return nil, fmt.Errorf("invalid dictionary ID/size")
		}
		dictionary := append([]byte(nil), d.Bytes...)
		c.dictionaryID, c.dictionaryHash = d.ID, sha256.Sum256(dictionary)
		encOptions = append(encOptions, zstd.WithEncoderDictRaw(d.ID, dictionary))
		decOptions = append(decOptions, zstd.WithDecoderDictRaw(d.ID, dictionary))
	}
	c.encoder, err = zstd.NewWriter(nil, encOptions...)
	if err != nil {
		return nil, err
	}
	c.decoder, err = zstd.NewReader(nil, decOptions...)
	if err != nil {
		c.encoder.Close()
		return nil, err
	}
	return c, nil
}

func (c *Codec) Close() error {
	c.lifetime.Lock()
	defer c.lifetime.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.encoder.Close()
	c.decoder.Close()
	return err
}

func (c *Codec) Encode(doc Document) ([]byte, Info, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	if c.closed {
		return nil, Info{}, ErrClosed
	}
	c.encodeMu.Lock()
	defer c.encodeMu.Unlock()
	body, err := encodeBody(doc, c.limits)
	if err != nil {
		return nil, Info{}, err
	}
	compressed := c.encoder.EncodeAll(body, nil)
	if int64(len(compressed)) > c.limits.MaxEncodedBytes-frameHeaderBytes {
		return nil, Info{}, fmt.Errorf("encoded frame exceeds limit")
	}
	var emitted zstd.Header
	if err := emitted.Decode(compressed); err != nil {
		return nil, Info{}, err
	}
	dictionaryID, dictionaryHash := emitted.DictionaryID, [32]byte{}
	if dictionaryID != 0 {
		if dictionaryID != c.dictionaryID {
			return nil, Info{}, fmt.Errorf("encoder emitted unavailable dictionary")
		}
		dictionaryHash = c.dictionaryHash
	}
	contentHash := sha256.Sum256(body)
	frame := make([]byte, frameHeaderBytes, len(compressed)+frameHeaderBytes)
	copy(frame, frameMagic)
	binary.LittleEndian.PutUint16(frame[8:], version)
	binary.LittleEndian.PutUint64(frame[12:], uint64(len(compressed)))
	binary.LittleEndian.PutUint64(frame[20:], uint64(len(body)))
	binary.LittleEndian.PutUint32(frame[28:], dictionaryID)
	copy(frame[32:], dictionaryHash[:])
	copy(frame[64:], contentHash[:])
	frame = append(frame, compressed...)
	return frame, frameInfo(frameHeaderBytes+int64(len(compressed)), int64(len(body)), dictionaryID, dictionaryHash, contentHash), nil
}

// Identity validates canonical content without compressing or applying encoded-size limits.
func (c *Codec) Identity(doc Document) (string, int64, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	if c.closed {
		return "", 0, ErrClosed
	}
	c.encodeMu.Lock()
	defer c.encodeMu.Unlock()
	body, err := encodeBody(doc, c.limits)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.Sum256(body)
	return hex.EncodeToString(hash[:]), int64(len(body)), nil
}

func frameInfo(encoded, decoded int64, id uint32, dictionary, content [32]byte) Info {
	info := Info{EncodedBytes: encoded, DecodedBytes: decoded, DictionaryID: id, ContentID: hex.EncodeToString(content[:])}
	if id != 0 {
		info.DictionaryHash = hex.EncodeToString(dictionary[:])
	}
	return info
}

func (c *Codec) Decode(frame []byte) (Document, Info, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	if c.closed {
		return Document{}, Info{}, ErrClosed
	}
	c.decodeMu.Lock()
	defer c.decodeMu.Unlock()
	return c.decode(frame)
}

// Validate all externally supplied sizes and dictionary identity before body
// allocation. The compressed zstd extent is checked separately before decode.
func (c *Codec) header(header []byte, total int64) (Info, error) {
	if len(header) < frameHeaderBytes || total < frameHeaderBytes || total > c.limits.MaxEncodedBytes || !bytes.Equal(header[:8], []byte(frameMagic)) || binary.LittleEndian.Uint16(header[8:]) != version || binary.LittleEndian.Uint16(header[10:]) != 0 {
		return Info{}, fmt.Errorf("invalid/oversized frame header")
	}
	compressed, decoded := binary.LittleEndian.Uint64(header[12:]), binary.LittleEndian.Uint64(header[20:])
	if compressed != uint64(total-frameHeaderBytes) || compressed == 0 || decoded > uint64(c.limits.MaxDecodedBytes) || decoded == 0 {
		return Info{}, fmt.Errorf("invalid frame extent/decoded size")
	}
	id := binary.LittleEndian.Uint32(header[28:])
	var dictionary, content [32]byte
	copy(dictionary[:], header[32:64])
	copy(content[:], header[64:96])
	if (id == 0 && dictionary != ([32]byte{})) || (id != 0 && (id != c.dictionaryID || dictionary != c.dictionaryHash)) {
		return Info{}, fmt.Errorf("missing/mismatched dictionary")
	}
	return frameInfo(total, int64(decoded), id, dictionary, content), nil
}

func validateZstd(frame []byte, info Info) error {
	var header zstd.Header
	if err := header.Decode(frame); err != nil {
		return err
	}
	if header.Skippable || !header.HasCheckSum || (header.HasFCS && header.FrameContentSize != uint64(info.DecodedBytes)) || header.DictionaryID != info.DictionaryID || (!header.SingleSegment && header.WindowSize > 1<<20) || (header.SingleSegment && header.FrameContentSize > 1<<20) {
		return fmt.Errorf("inconsistent zstd frame header")
	}
	offset := header.HeaderSize
	for {
		if offset > len(frame)-3 {
			return fmt.Errorf("truncated zstd block header")
		}
		block := uint32(frame[offset]) | uint32(frame[offset+1])<<8 | uint32(frame[offset+2])<<16
		offset += 3
		kind, size := (block>>1)&3, int(block>>3)
		if kind == 3 {
			return fmt.Errorf("invalid zstd block type")
		}
		if kind == 1 {
			size = 1
		}
		if size > len(frame)-offset {
			return fmt.Errorf("truncated zstd block")
		}
		offset += size
		if block&1 != 0 {
			break
		}
	}
	if len(frame)-offset != 4 {
		return fmt.Errorf("concatenated/trailing zstd frame bytes")
	}
	return nil
}

func (c *Codec) decode(frame []byte) (Document, Info, error) {
	info, err := c.header(frame, int64(len(frame)))
	if err != nil {
		return Document{}, Info{}, err
	}
	compressed := frame[frameHeaderBytes:]
	if err := validateZstd(compressed, info); err != nil {
		return Document{}, Info{}, err
	}
	body, err := c.decoder.DecodeAll(compressed, make([]byte, 0, int(info.DecodedBytes)))
	if err != nil {
		return Document{}, Info{}, err
	}
	if int64(len(body)) != info.DecodedBytes {
		return Document{}, Info{}, fmt.Errorf("decoded length mismatch")
	}
	hash := sha256.Sum256(body)
	if hex.EncodeToString(hash[:]) != info.ContentID {
		return Document{}, Info{}, fmt.Errorf("document identity mismatch")
	}
	doc, err := decodeBody(body, c.limits)
	if err != nil {
		return Document{}, Info{}, err
	}
	return doc, info, nil
}

// ReadFrame validates an independent encoded range before allocating its body.
func (c *Codec) ReadFrame(reader io.ReaderAt, offset, length int64) (Document, Info, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	if c.closed {
		return Document{}, Info{}, ErrClosed
	}
	c.decodeMu.Lock()
	defer c.decodeMu.Unlock()
	if reader == nil || offset < 0 || length < frameHeaderBytes || offset > math.MaxInt64-length || length > c.limits.MaxEncodedBytes {
		return Document{}, Info{}, fmt.Errorf("invalid/oversized frame range")
	}
	var header [frameHeaderBytes]byte
	if n, err := reader.ReadAt(header[:], offset); (err != nil && err != io.EOF) || n != len(header) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return Document{}, Info{}, err
	}
	if _, err := c.header(header[:], length); err != nil {
		return Document{}, Info{}, err
	}
	frame := make([]byte, int(length))
	copy(frame, header[:])
	if n, err := reader.ReadAt(frame[frameHeaderBytes:], offset+frameHeaderBytes); (err != nil && err != io.EOF) || n != len(frame)-frameHeaderBytes {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return Document{}, Info{}, err
	}
	return c.decode(frame)
}
