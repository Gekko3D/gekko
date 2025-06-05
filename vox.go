package gekko

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	VOXMagicNumber = "VOX "
)

type Voxel struct {
	X, Y, Z, ColorIndex byte
}

type VoxModel struct {
	SizeX, SizeY, SizeZ uint32 // Changed from byte to uint32
	Voxels              []Voxel
}

type VoxPalette [256][4]byte // RGBA colors

type VoxFile struct {
	Version      int
	Models       []VoxModel
	Palette      VoxPalette
	VoxMaterials []VoxMaterial
}

type VoxMaterial struct {
	ID       int
	Type     int
	Weight   float32
	Property map[string]interface{}
}

func LoadVoxFile(filename string) (*VoxFile, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read and verify magic number
	var magic [4]byte
	if _, err := io.ReadFull(file, magic[:]); err != nil {
		return nil, err
	}
	if string(magic[:]) != VOXMagicNumber {
		return nil, errors.New("not a valid VOX file")
	}

	// Read version number
	var version int32
	if err := binary.Read(file, binary.LittleEndian, &version); err != nil {
		return nil, err
	}

	voxFile := &VoxFile{
		Version: int(version),
	}

	// Default palette
	voxFile.Palette = defaultPalette()

	// Main chunk reading loop
	for {
		var chunkID [4]byte
		if _, err := io.ReadFull(file, chunkID[:]); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		var chunkSize, childrenSize int32
		if err := binary.Read(file, binary.LittleEndian, &chunkSize); err != nil {
			return nil, err
		}
		if err := binary.Read(file, binary.LittleEndian, &childrenSize); err != nil {
			return nil, err
		}

		chunkData := make([]byte, chunkSize)
		if _, err := io.ReadFull(file, chunkData); err != nil {
			return nil, err
		}

		switch string(chunkID[:]) {
		case "MAIN":
			// MAIN chunk contains other chunks
			continue
		case "SIZE":
			if len(voxFile.Models) == 0 {
				voxFile.Models = append(voxFile.Models, VoxModel{})
			}
			model := &voxFile.Models[len(voxFile.Models)-1]
			if len(chunkData) >= 12 {
				model.SizeX = binary.LittleEndian.Uint32(chunkData[0:4])
				model.SizeY = binary.LittleEndian.Uint32(chunkData[4:8])
				model.SizeZ = binary.LittleEndian.Uint32(chunkData[8:12])
			} else {
				return nil, errors.New("SIZE chunk too small")
			}
		case "XYZI":
			model := &voxFile.Models[len(voxFile.Models)-1]
			numVoxels := binary.LittleEndian.Uint32(chunkData[:4])
			model.Voxels = make([]Voxel, numVoxels)
			for i := 0; i < int(numVoxels); i++ {
				offset := 4 + i*4
				if offset+3 >= len(chunkData) {
					return nil, errors.New("XYZI chunk data overflow")
				}
				model.Voxels[i] = Voxel{
					X:          chunkData[offset],
					Y:          chunkData[offset+1],
					Z:          chunkData[offset+2],
					ColorIndex: chunkData[offset+3],
				}
			}
		case "RGBA":
			for i := 0; i < 255; i++ {
				offset := i * 4
				if offset+3 >= len(chunkData) {
					break
				}
				voxFile.Palette[i+1][0] = chunkData[offset]
				voxFile.Palette[i+1][1] = chunkData[offset+1]
				voxFile.Palette[i+1][2] = chunkData[offset+2]
				voxFile.Palette[i+1][3] = chunkData[offset+3]
			}
		case "MATL":
			mat, err := parseMaterial(chunkData)
			if err != nil {
				return nil, err
			}
			voxFile.VoxMaterials = append(voxFile.VoxMaterials, mat)
		case "PACK":
			numModels := binary.LittleEndian.Uint32(chunkData[:4])
			if numModels > 0 {
				voxFile.Models = make([]VoxModel, numModels)
			}
		}
	}

	return voxFile, nil
}

func parseMaterial(data []byte) (VoxMaterial, error) {
	mat := VoxMaterial{
		Property: make(map[string]interface{}),
	}

	// Material ID (int32)
	mat.ID = int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]

	// Material type (int32)
	mat.Type = int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]

	// Material properties
	for len(data) > 0 {
		keyLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		key := string(data[:keyLen])
		data = data[keyLen:]

		valueLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		value := string(data[:valueLen])
		data = data[valueLen:]

		// Convert to appropriate type based on key
		switch key {
		case "_weight":
			var weight float32
			// This is a simplified approach - actual parsing would need to handle the float string
			_, err := fmt.Sscanf(value, "%f", &weight)
			if err != nil {
				return mat, err
			}
			mat.Weight = weight
		default:
			mat.Property[key] = value
		}
	}

	return mat, nil
}

func defaultPalette() VoxPalette {
	var palette VoxPalette
	for i := range palette {
		palette[i] = [4]uint8{255, 255, 255, 255} // white as fallback
	}
	return palette
}
