package gekko

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/go-gl/mathgl/mgl32"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

const (
	VOXMagicNumber = "VOX "
)

type Voxel struct {
	X, Y, Z    uint32
	ColorIndex byte
}

type VoxPhysicsData struct {
	Corners       []Voxel
	Edges         []Voxel
	Faces         []Voxel
	Inside        []Voxel
	Mass          float32
	CenterOfMass  mgl32.Vec3
	InertiaTensor mgl32.Mat3
}

type VoxModel struct {
	SizeX, SizeY, SizeZ uint32
	Voxels              []Voxel
	PhysicsData         *VoxPhysicsData
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

	for i := range voxFile.Models {
		voxFile.Models[i].AnalyzePhysics()
	}

	return voxFile, nil
}

// AnalyzePhysicsFromMap creates acceleration data from a sparse XBrickMap
func AnalyzePhysicsFromMap(xbm *volume.XBrickMap) *VoxPhysicsData {
	if xbm == nil || xbm.GetVoxelCount() == 0 {
		return nil
	}

	// 1. Collect all active voxels
	var voxels []Voxel
	// Pre-allocate assuming some density, though exact count is available
	// But GetVoxelCount is cached in XBrickMap? Yes.
	voxels = make([]Voxel, 0, xbm.GetVoxelCount())

	for sCoord, sector := range xbm.Sectors {
		sx, sy, sz := sCoord[0], sCoord[1], sCoord[2]

		// Iterate 4x4x4 bricks in sector
		for bx := 0; bx < volume.SectorBricks; bx++ {
			for by := 0; by < volume.SectorBricks; by++ {
				for bz := 0; bz < volume.SectorBricks; bz++ {
					brick := sector.GetBrick(bx, by, bz)
					if brick == nil || brick.IsEmpty() {
						continue
					}

					// Iterate voxels in brick (8x8x8)
					for vx := 0; vx < volume.BrickSize; vx++ {
						for vy := 0; vy < volume.BrickSize; vy++ {
							for vz := 0; vz < volume.BrickSize; vz++ {
								val := brick.Payload[vx][vy][vz]
								if val != 0 {
									gx := sx*volume.SectorSize + bx*volume.BrickSize + vx
									gy := sy*volume.SectorSize + by*volume.BrickSize + vy
									gz := sz*volume.SectorSize + bz*volume.BrickSize + vz
									voxels = append(voxels, Voxel{uint32(gx), uint32(gy), uint32(gz), val})
								}
							}
						}
					}
				}
			}
		}
	}

	data := &VoxPhysicsData{}
	// Calculate mass properties
	totalMass := float32(0.0)
	weightedPos := mgl32.Vec3{0, 0, 0}
	voxelMass := float32(1.0)     // Assume uniform density for now
	voxelUnitSize := float32(0.1) // 10cm voxels

	for _, v := range voxels {
		// Use VoxelUnitSize scale
		pos := mgl32.Vec3{float32(v.X) * voxelUnitSize, float32(v.Y) * voxelUnitSize, float32(v.Z) * voxelUnitSize}
		totalMass += voxelMass
		weightedPos = weightedPos.Add(pos.Mul(voxelMass))
	}

	if totalMass > 0 {
		data.Mass = totalMass
		data.CenterOfMass = weightedPos.Mul(1.0 / totalMass)

		// Calculate Inertia Tensor
		// I = sum( m * ( ||r||^2 * I - r * rT ) ) + I_voxel
		// For a cube, I_voxel = m * s^2 / 6 * Identity
		var Ixx, Iyy, Izz, Ixy, Ixz, Iyz float32

		// Voxel inertia (point mass approximation usually not enough for close packing, but let's use point mass + sphere/cube term)
		// For a cube of side 's' and mass 'm', moment of inertia is m*s^2/6
		// s = voxelUnitSize
		voxelI := voxelMass * (voxelUnitSize * voxelUnitSize) / 6.0

		for _, v := range voxels {
			pos := mgl32.Vec3{float32(v.X) * voxelUnitSize, float32(v.Y) * voxelUnitSize, float32(v.Z) * voxelUnitSize}
			r := pos.Sub(data.CenterOfMass)

			Ixx += voxelMass * (r.Y()*r.Y() + r.Z()*r.Z())
			Iyy += voxelMass * (r.X()*r.X() + r.Z()*r.Z())
			Izz += voxelMass * (r.X()*r.X() + r.Y()*r.Y())

			Ixy -= voxelMass * r.X() * r.Y()
			Ixz -= voxelMass * r.X() * r.Z()
			Iyz -= voxelMass * r.Y() * r.Z()
		}

		// Add voxel intrinsic inertia
		Ixx += float32(len(voxels)) * voxelI
		Iyy += float32(len(voxels)) * voxelI
		Izz += float32(len(voxels)) * voxelI

		data.InertiaTensor = mgl32.Mat3{
			Ixx, Ixy, Ixz,
			Ixy, Iyy, Iyz,
			Ixz, Iyz, Izz,
		}
	}

	// 2. Build fast lookup grid
	grid := make(map[[3]uint32]bool)
	for _, v := range voxels {
		grid[[3]uint32{v.X, v.Y, v.Z}] = true
	}

	delta := [6][3]int{
		{1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, -1},
	}

	// 3. Categorize
	for _, v := range voxels {
		emptyNeighbors := 0
		for _, d := range delta {
			nx, ny, nz := int(v.X)+d[0], int(v.Y)+d[1], int(v.Z)+d[2]
			// We only care about neighbors within the object.
			// Bounds check is implicit: if it's not in the grid, it's empty space relative to object.
			if !grid[[3]uint32{uint32(nx), uint32(ny), uint32(nz)}] {
				emptyNeighbors++
			}
		}

		switch emptyNeighbors {
		case 0:
			data.Inside = append(data.Inside, v)
		case 1:
			data.Faces = append(data.Faces, v)
		case 2:
			data.Edges = append(data.Edges, v)
		default:
			data.Corners = append(data.Corners, v)
		}
	}

	return data
}

func (m *VoxModel) AnalyzePhysics() {
	if len(m.Voxels) == 0 {
		return
	}

	m.PhysicsData = &VoxPhysicsData{}

	// Calculate mass properties
	totalMass := float32(0.0)
	weightedPos := mgl32.Vec3{0, 0, 0}
	voxelMass := float32(1.0)     // Assume uniform density for now
	voxelUnitSize := float32(0.1) // 10cm voxels

	// Create a 3D grid for fast lookup
	grid := make(map[[3]uint32]bool)
	for _, v := range m.Voxels {
		grid[[3]uint32{v.X, v.Y, v.Z}] = true

		// Use VoxelUnitSize scale
		pos := mgl32.Vec3{float32(v.X) * voxelUnitSize, float32(v.Y) * voxelUnitSize, float32(v.Z) * voxelUnitSize}
		totalMass += voxelMass
		weightedPos = weightedPos.Add(pos.Mul(voxelMass))
	}

	if totalMass > 0 {
		m.PhysicsData.Mass = totalMass
		m.PhysicsData.CenterOfMass = weightedPos.Mul(1.0 / totalMass)

		// Calculate Inertia Tensor
		var Ixx, Iyy, Izz, Ixy, Ixz, Iyz float32

		// Voxel inertia (cube)
		voxelI := voxelMass * (voxelUnitSize * voxelUnitSize) / 6.0

		for _, v := range m.Voxels {
			pos := mgl32.Vec3{float32(v.X) * voxelUnitSize, float32(v.Y) * voxelUnitSize, float32(v.Z) * voxelUnitSize}
			r := pos.Sub(m.PhysicsData.CenterOfMass)

			Ixx += voxelMass * (r.Y()*r.Y() + r.Z()*r.Z())
			Iyy += voxelMass * (r.X()*r.X() + r.Z()*r.Z())
			Izz += voxelMass * (r.X()*r.X() + r.Y()*r.Y())

			Ixy -= voxelMass * r.X() * r.Y()
			Ixz -= voxelMass * r.X() * r.Z()
			Iyz -= voxelMass * r.Y() * r.Z()
		}

		// Add voxel intrinsic inertia
		Ixx += float32(len(m.Voxels)) * voxelI
		Iyy += float32(len(m.Voxels)) * voxelI
		Izz += float32(len(m.Voxels)) * voxelI

		m.PhysicsData.InertiaTensor = mgl32.Mat3{
			Ixx, Ixy, Ixz,
			Ixy, Iyy, Iyz,
			Ixz, Iyz, Izz,
		}
	}

	delta := [6][3]int{
		{1, 0, 0}, {-1, 0, 0},
		{0, 1, 0}, {0, -1, 0},
		{0, 0, 1}, {0, 0, -1},
	}

	for _, v := range m.Voxels {
		emptyNeighbors := 0
		for _, d := range delta {
			nx, ny, nz := int(v.X)+d[0], int(v.Y)+d[1], int(v.Z)+d[2]
			if nx < 0 || ny < 0 || nz < 0 || uint32(nx) >= m.SizeX || uint32(ny) >= m.SizeY || uint32(nz) >= m.SizeZ {
				emptyNeighbors++
				continue
			}
			if !grid[[3]uint32{uint32(nx), uint32(ny), uint32(nz)}] {
				emptyNeighbors++
			}
		}

		switch emptyNeighbors {
		case 0:
			m.PhysicsData.Inside = append(m.PhysicsData.Inside, v)
		case 1:
			m.PhysicsData.Faces = append(m.PhysicsData.Faces, v)
		case 2:
			m.PhysicsData.Edges = append(m.PhysicsData.Edges, v)
		default:
			// 3 or more empty neighbors
			m.PhysicsData.Corners = append(m.PhysicsData.Corners, v)
		}
	}
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
