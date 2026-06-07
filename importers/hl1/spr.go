package hl1

import (
	"fmt"
	"os"
)

const (
	SPRIdentGoldSrc = "IDSP"
	SPRVersion2     = 2
	sprHeaderSize   = 40
)

type SPRInfo struct {
	Version        int     `json:"version"`
	Type           int     `json:"type"`
	TextureFormat  int     `json:"texture_format"`
	BoundingRadius float32 `json:"bounding_radius"`
	MaxWidth       int     `json:"max_width"`
	MaxHeight      int     `json:"max_height"`
	FrameCount     int     `json:"frame_count"`
	BeamLength     float32 `json:"beam_length,omitempty"`
	SyncType       int     `json:"sync_type"`
	DecodedFrames  int     `json:"decoded_frame_count,omitempty"`
}

type SPRGeometry struct {
	Info    SPRInfo
	Palette [][3]uint8
	Frames  []SPRFrame
}

type SPRFrame struct {
	OriginX int
	OriginY int
	Width   int
	Height  int
	Pixels  []byte
}

func LoadSPRGeometry(path string) (SPRGeometry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SPRGeometry{}, err
	}
	return ParseSPRGeometry(data)
}

func ParseSPRGeometry(data []byte) (SPRGeometry, error) {
	if len(data) < sprHeaderSize+2 {
		return SPRGeometry{}, fmt.Errorf("spr too small: %d bytes", len(data))
	}
	if string(data[0:4]) != SPRIdentGoldSrc {
		return SPRGeometry{}, fmt.Errorf("unsupported spr ident %q", string(data[0:4]))
	}
	version := int(readInt32(data, 4))
	if version != SPRVersion2 {
		return SPRGeometry{}, fmt.Errorf("unsupported spr version %d", version)
	}
	info := SPRInfo{
		Version:        version,
		Type:           int(readInt32(data, 8)),
		TextureFormat:  int(readInt32(data, 12)),
		BoundingRadius: readFloat32(data, 16),
		MaxWidth:       int(readInt32(data, 20)),
		MaxHeight:      int(readInt32(data, 24)),
		FrameCount:     int(readInt32(data, 28)),
		BeamLength:     readFloat32(data, 32),
		SyncType:       int(readInt32(data, 36)),
	}
	if info.MaxWidth <= 0 || info.MaxHeight <= 0 || info.FrameCount <= 0 {
		return SPRGeometry{}, fmt.Errorf("invalid spr dimensions/frames: %dx%d frames=%d", info.MaxWidth, info.MaxHeight, info.FrameCount)
	}
	offset := sprHeaderSize
	paletteCount := int(readUint16(data, offset))
	offset += 2
	if paletteCount <= 0 || paletteCount > 256 {
		return SPRGeometry{}, fmt.Errorf("invalid spr palette size %d", paletteCount)
	}
	if offset+paletteCount*3 > len(data) {
		return SPRGeometry{}, fmt.Errorf("spr palette out of range")
	}
	palette := make([][3]uint8, paletteCount)
	for i := 0; i < paletteCount; i++ {
		base := offset + i*3
		palette[i] = [3]uint8{data[base], data[base+1], data[base+2]}
	}
	offset += paletteCount * 3
	frames := make([]SPRFrame, 0, info.FrameCount)
	for i := 0; i < info.FrameCount; i++ {
		frame, next, err := parseSPRFrame(data, offset)
		if err != nil {
			return SPRGeometry{}, fmt.Errorf("frame %d: %w", i, err)
		}
		frames = append(frames, frame)
		offset = next
	}
	info.DecodedFrames = len(frames)
	return SPRGeometry{Info: info, Palette: palette, Frames: frames}, nil
}

func parseSPRFrame(data []byte, offset int) (SPRFrame, int, error) {
	if offset+20 > len(data) {
		return SPRFrame{}, offset, fmt.Errorf("frame header out of range")
	}
	frameType := int(readInt32(data, offset))
	if frameType != 0 {
		return SPRFrame{}, offset, fmt.Errorf("unsupported grouped spr frame type %d", frameType)
	}
	originX := int(readInt32(data, offset+4))
	originY := int(readInt32(data, offset+8))
	width := int(readInt32(data, offset+12))
	height := int(readInt32(data, offset+16))
	if width <= 0 || height <= 0 {
		return SPRFrame{}, offset, fmt.Errorf("invalid frame size %dx%d", width, height)
	}
	pixelOffset := offset + 20
	pixelCount := width * height
	if pixelCount < 0 || pixelOffset+pixelCount > len(data) {
		return SPRFrame{}, offset, fmt.Errorf("frame pixels out of range")
	}
	pixels := append([]byte(nil), data[pixelOffset:pixelOffset+pixelCount]...)
	return SPRFrame{OriginX: originX, OriginY: originY, Width: width, Height: height, Pixels: pixels}, pixelOffset + pixelCount, nil
}
