package assets

type Voxel struct {
	X, Y, Z    uint32
	ColorIndex byte
}

type VoxModel struct {
	SizeX, SizeY, SizeZ uint32
	Voxels              []Voxel
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
