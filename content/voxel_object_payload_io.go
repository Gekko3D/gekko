package content

import (
	"bytes"
	"io"
	"os"

	"github.com/gekko3d/gekko/content/voxelcodec"
)

// SaveVoxelObjectPayload durably replaces a schema 2 payload. The codec is
// borrowed and a successful result follows existing atomic file publication.
func SaveVoxelObjectPayload(path string, payload *VoxelObjectPayloadDef, codec *voxelcodec.Codec) (voxelcodec.Info, error) {
	data, info, err := EncodeVoxelObjectPayload(payload, codec)
	if err != nil {
		return voxelcodec.Info{}, err
	}
	if err := saveFileAtomically(path, data, 0644); err != nil {
		return voxelcodec.Info{}, err
	}
	return info, nil
}

// LoadVoxelObjectPayload bounds compiled frame reads through the supplied
// codec's ReaderAt path. Legacy JSON keeps existing whole-file acceptance.
func LoadVoxelObjectPayload(path string, codec *voxelcodec.Codec) (*VoxelObjectPayloadDef, voxelcodec.Info, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	defer file.Close()
	var prefix [8]byte
	n, err := file.ReadAt(prefix[:], 0)
	if err != nil && err != io.EOF {
		return nil, voxelcodec.Info{}, err
	}
	if n == len(prefix) && bytes.Equal(prefix[:], []byte(importedCompiledMagic)) {
		stat, err := file.Stat()
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		codec, err := voxelObjectCodec(codec)
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		doc, info, err := codec.ReadFrame(file, 0, stat.Size())
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		payload, err := voxelObjectPayloadFromDocument(doc)
		if err != nil {
			return nil, voxelcodec.Info{}, err
		}
		return payload, info, nil
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, voxelcodec.Info{}, err
	}
	return DecodeVoxelObjectPayload(data, codec)
}
