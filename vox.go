package gekko

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/go-gl/mathgl/mgl32"
)

const (
	VOXMagicNumber = "VOX "
)

type Voxel struct {
	X, Y, Z    uint32
	ColorIndex byte
}

type VoxModel struct {
	SizeX, SizeY, SizeZ uint32
	Voxels              []Voxel
}

type VoxelEdit struct {
	Entity EntityId
	Pos    [3]int
	Val    uint8
}

type SphereCarve struct {
	Entity         EntityId
	Center         mgl32.Vec3
	Radius         float32
	Value          uint8
	DensityFalloff bool
}

type VoxelEditQueue struct {
	BudgetPerFrame int
	Edits          []VoxelEdit
	Spheres        []SphereCarve
}

type VoxPalette [256][4]byte // RGBA colors

type VoxFile struct {
	Version      int
	Models       []VoxModel
	Palette      VoxPalette
	VoxMaterials []VoxMaterial
	Nodes        map[int]VoxNode
}

type VoxNodeType int

const (
	VoxNodeTransform VoxNodeType = iota
	VoxNodeGroup
	VoxNodeShape
)

type VoxNode struct {
	ID         int
	Type       VoxNodeType
	Attributes map[string]string

	// Transform Node
	ChildID    int
	ReservedID int
	LayerID    int
	Frames     []VoxTransformFrame

	// Group Node
	ChildrenIDs []int

	// Shape Node
	Models []VoxShapeModel
}

type VoxTransformFrame struct {
	Rotation   byte // index in rotation enum? MagicaVoxel uses a bitmask or similar for orientation
	LocalTrans [3]float32
	Attributes map[string]string
}

type VoxShapeModel struct {
	ModelID    int
	Attributes map[string]string
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
		Nodes:   make(map[int]VoxNode),
	}

	// Default palette
	voxFile.Palette = defaultPalette()

	// Track current model index
	currentModelIndex := -1

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
			currentModelIndex++
			if currentModelIndex >= len(voxFile.Models) {
				voxFile.Models = append(voxFile.Models, VoxModel{})
			}
			model := &voxFile.Models[currentModelIndex]
			if len(chunkData) >= 12 {
				model.SizeX = binary.LittleEndian.Uint32(chunkData[0:4])
				model.SizeY = binary.LittleEndian.Uint32(chunkData[4:8])
				model.SizeZ = binary.LittleEndian.Uint32(chunkData[8:12])
			} else {
				return nil, errors.New("SIZE chunk too small")
			}
		case "XYZI":
			if currentModelIndex < 0 || currentModelIndex >= len(voxFile.Models) {
				return nil, errors.New("XYZI chunk without preceding SIZE or invalid index")
			}
			model := &voxFile.Models[currentModelIndex]
			numVoxels := binary.LittleEndian.Uint32(chunkData[:4])
			model.Voxels = make([]Voxel, numVoxels)
			for i := 0; i < int(numVoxels); i++ {
				offset := 4 + i*4
				if offset+3 >= len(chunkData) {
					return nil, errors.New("XYZI chunk data overflow")
				}
				model.Voxels[i] = Voxel{
					X:          uint32(chunkData[offset]),
					Y:          uint32(chunkData[offset+1]),
					Z:          uint32(chunkData[offset+2]),
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
		case "nTRN":
			node, err := parseTransformNode(chunkData)
			if err != nil {
				return nil, err
			}
			voxFile.Nodes[node.ID] = node
		case "nGRP":
			node, err := parseGroupNode(chunkData)
			if err != nil {
				return nil, err
			}
			voxFile.Nodes[node.ID] = node
		case "nSHP":
			node, err := parseShapeNode(chunkData)
			if err != nil {
				return nil, err
			}
			voxFile.Nodes[node.ID] = node
		}
	}

	printDebugInfo(voxFile)

	return voxFile, nil
}

func printDebugInfo(voxFile *VoxFile) {
	fmt.Printf("VOX File Version: %d\n", voxFile.Version)
	fmt.Printf("Number of Models: %d\n", len(voxFile.Models))
	fmt.Printf("Number of Nodes: %d\n", len(voxFile.Nodes))

	if len(voxFile.Models) > 0 {
		model := voxFile.Models[0]
		fmt.Printf("First Model Size: %dx%dx%d\n", model.SizeX, model.SizeY, model.SizeZ)
		fmt.Printf("Number of Voxels: %d\n", len(model.Voxels))
	}
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
		if len(data) < 4 {
			break
		}
		keyLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		if len(data) < keyLen {
			break
		}
		key := string(data[:keyLen])
		data = data[keyLen:]

		if len(data) < 4 {
			break
		}
		valueLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		if len(data) < valueLen {
			break
		}
		value := string(data[:valueLen])
		data = data[valueLen:]

		// Convert to appropriate type based on key
		switch key {
		case "_weight", "_rough", "_metal", "_emit", "_ior", "_trans", "_flux":
			var val float32
			_, err := fmt.Sscanf(value, "%f", &val)
			if err == nil {
				mat.Property[key] = val
				if key == "_weight" {
					mat.Weight = val
				}
			} else {
				mat.Property[key] = value
			}
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

func parseTransformNode(data []byte) (VoxNode, error) {
	node := VoxNode{Type: VoxNodeTransform, Attributes: make(map[string]string)}
	node.ID = int(binary.LittleEndian.Uint32(data[0:4]))
	data = data[4:]

	attr, nextData := parseDICT(data)
	node.Attributes = attr
	data = nextData

	node.ChildID = int(binary.LittleEndian.Uint32(data[0:4]))
	node.ReservedID = int(binary.LittleEndian.Uint32(data[4:8]))
	node.LayerID = int(binary.LittleEndian.Uint32(data[8:12]))
	numFrames := int(binary.LittleEndian.Uint32(data[12:16]))
	data = data[16:]

	for i := 0; i < numFrames; i++ {
		frameAttr, nextData := parseDICT(data)
		data = nextData
		frame := VoxTransformFrame{Attributes: frameAttr}
		if val, ok := frameAttr["_t"]; ok {
			fmt.Sscanf(val, "%f %f %f", &frame.LocalTrans[0], &frame.LocalTrans[1], &frame.LocalTrans[2])
		}
		if val, ok := frameAttr["_r"]; ok {
			var r int
			fmt.Sscanf(val, "%d", &r)
			frame.Rotation = byte(r)
		}
		node.Frames = append(node.Frames, frame)
	}

	return node, nil
}

func parseGroupNode(data []byte) (VoxNode, error) {
	node := VoxNode{Type: VoxNodeGroup, Attributes: make(map[string]string)}
	node.ID = int(binary.LittleEndian.Uint32(data[0:4]))
	data = data[4:]

	attr, nextData := parseDICT(data)
	node.Attributes = attr
	data = nextData

	numChildren := int(binary.LittleEndian.Uint32(data[0:4]))
	data = data[4:]

	for i := 0; i < numChildren; i++ {
		childID := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		node.ChildrenIDs = append(node.ChildrenIDs, childID)
	}

	return node, nil
}

func parseShapeNode(data []byte) (VoxNode, error) {
	node := VoxNode{Type: VoxNodeShape, Attributes: make(map[string]string)}
	node.ID = int(binary.LittleEndian.Uint32(data[0:4]))
	data = data[4:]

	attr, nextData := parseDICT(data)
	node.Attributes = attr
	data = nextData

	numModels := int(binary.LittleEndian.Uint32(data[0:4]))
	data = data[4:]

	for i := 0; i < numModels; i++ {
		modelID := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		modelAttr, nextData := parseDICT(data)
		data = nextData
		model := VoxShapeModel{ModelID: modelID, Attributes: modelAttr}
		node.Models = append(node.Models, model)
	}

	return node, nil
}

func parseDICT(data []byte) (map[string]string, []byte) {
	res := make(map[string]string)
	numElems := int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]

	for i := 0; i < numElems; i++ {
		keyLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		key := string(data[:keyLen])
		data = data[keyLen:]

		valLen := int(binary.LittleEndian.Uint32(data[:4]))
		data = data[4:]
		val := string(data[:valLen])
		data = data[valLen:]

		res[key] = val
	}
	return res, data
}
