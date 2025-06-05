package gekko

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type Voxel struct {
	X, Y, Z, ColorIndex uint8
}

type VoxModel struct {
	SizeX, SizeY, SizeZ int32
	Voxels              []Voxel
	Palette             [256][4]uint8
}

func LoadMagicaVoxel(path string) (*VoxModel, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(file)

	var magic [4]byte
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}
	if string(magic[:]) != "VOX " {
		return nil, fmt.Errorf("not a valid VOX file")
	}

	var version int32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	if version != 150 {
		return nil, fmt.Errorf("unsupported VOX version: %d", version)
	}

	model := &VoxModel{}
	model.Palette = DefaultPalette() // fallback palette

	if err := parseChunks(r, model); err != nil {
		return nil, err
	}

	fmt.Printf("Model size: %dx%dx%d\n", model.SizeX, model.SizeY, model.SizeZ)
	fmt.Printf("Voxel count: %d\n", len(model.Voxels))
	return model, nil
}

func parseChunks(r *bytes.Reader, model *VoxModel) error {
	for r.Len() > 0 {
		var chunkID [4]byte
		if err := binary.Read(r, binary.LittleEndian, &chunkID); err != nil {
			return err
		}

		var contentSize, childrenSize int32
		if err := binary.Read(r, binary.LittleEndian, &contentSize); err != nil {
			return err
		}
		if err := binary.Read(r, binary.LittleEndian, &childrenSize); err != nil {
			return err
		}

		content := make([]byte, contentSize)
		if _, err := io.ReadFull(r, content); err != nil {
			return err
		}
		contentReader := bytes.NewReader(content)

		switch string(chunkID[:]) {
		case "MAIN":
			// Children follow immediately
		case "SIZE":
			binary.Read(contentReader, binary.LittleEndian, &model.SizeX)
			binary.Read(contentReader, binary.LittleEndian, &model.SizeY)
			binary.Read(contentReader, binary.LittleEndian, &model.SizeZ)
		case "XYZI":
			var numVoxels int32
			binary.Read(contentReader, binary.LittleEndian, &numVoxels)
			model.Voxels = make([]Voxel, numVoxels)
			for i := 0; i < int(numVoxels); i++ {
				var v Voxel
				binary.Read(contentReader, binary.LittleEndian, &v)
				model.Voxels[i] = v
			}
		case "RGBA":
			for i := 0; i < 256; i++ {
				binary.Read(contentReader, binary.LittleEndian, &model.Palette[i])
			}
		default:
			// Skip unknown chunks
		}

		if childrenSize > 0 {
			if err := parseChunks(r, model); err != nil {
				return err
			}
		}
	}
	return nil
}

func DefaultPalette() [256][4]uint8 {
	// MagicaVoxel default palette (you could hardcode it from palette.png)
	var p [256][4]uint8
	for i := range p {
		p[i] = [4]uint8{255, 255, 255, 255} // white as fallback
	}
	return p
}
