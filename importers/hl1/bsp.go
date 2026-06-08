package hl1

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"strings"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

const (
	BSPVersion30 = 30
	bspLumpCount = 15
)

type LumpID int

const (
	LumpEntities LumpID = iota
	LumpPlanes
	LumpTextures
	LumpVertices
	LumpVisibility
	LumpNodes
	LumpTexInfo
	LumpFaces
	LumpLighting
	LumpClipNodes
	LumpLeafs
	LumpMarkSurfaces
	LumpEdges
	LumpSurfEdges
	LumpModels
)

var lumpNames = [...]string{
	"entities",
	"planes",
	"textures",
	"vertices",
	"visibility",
	"nodes",
	"texinfo",
	"faces",
	"lighting",
	"clipnodes",
	"leafs",
	"marksurfaces",
	"edges",
	"surfedges",
	"models",
}

func (id LumpID) String() string {
	if id < 0 || int(id) >= len(lumpNames) {
		return fmt.Sprintf("lump_%d", id)
	}
	return lumpNames[id]
}

type Lump struct {
	Offset int `json:"offset"`
	Length int `json:"length"`
}

type BSP struct {
	Path           string
	Version        int
	SHA256         string
	Lumps          [bspLumpCount]Lump
	EntityText     string
	Entities       []RawEntity
	Planes         []Plane
	Nodes          []Node
	Vertices       []importcommon.Vec3
	Textures       []Texture
	VisibilityData []byte
	LightingData   []byte
	TexInfos       []TexInfo
	Faces          []FaceHeader
	Edges          []Edge
	SurfEdges      []int32
	Leafs          []Leaf
	Models         []Model
	Diagnostics    []importcommon.Diagnostic
}

type Plane struct {
	Normal importcommon.Vec3
	Dist   float32
	Type   int32
}

type Node struct {
	PlaneID   uint32
	Children  [2]int16
	Min       [3]int16
	Max       [3]int16
	FirstFace uint16
	FaceCount uint16
}

type Leaf struct {
	Contents         int32
	VisibilityOffset int32
	Min              [3]int16
	Max              [3]int16
	FirstMarkSurface uint16
	MarkSurfaceCount uint16
	AmbientLevels    [4]byte
}

type Texture struct {
	Name      string
	Width     uint32
	Height    uint32
	Embedded  bool
	BaseColor [4]uint8
	Pixels    TexturePixels
}

type Model struct {
	Min       importcommon.Vec3
	Max       importcommon.Vec3
	Origin    importcommon.Vec3
	HeadNodes [4]int32
	VisLeafs  int32
	FirstFace int32
	FaceCount int32
}

const (
	ContentsEmpty       int32 = -1
	ContentsSolid       int32 = -2
	ContentsWater       int32 = -3
	ContentsSlime       int32 = -4
	ContentsLava        int32 = -5
	ContentsSky         int32 = -6
	ContentsOrigin      int32 = -7
	ContentsClip        int32 = -8
	ContentsTranslucent int32 = -15
)

type TextureAxis struct {
	Axis  importcommon.Vec3
	Shift float32
}

type TexInfo struct {
	S      TextureAxis
	T      TextureAxis
	MipTex int32
	Flags  int32
}

type FaceHeader struct {
	PlaneID   uint16
	Side      int16
	FirstEdge int32
	EdgeCount int16
	TexInfoID int16
	Styles    [4]byte
	LightOfs  int32
}

type Edge struct {
	A uint16
	B uint16
}

type Face struct {
	ModelID     int
	FaceID      int
	PlaneID     int
	Side        int
	Vertices    []importcommon.Vec3
	Normal      importcommon.Vec3
	TextureID   int
	TextureName string
	TexInfo     TexInfo
	Styles      [4]byte
	LightOfs    int32
	Bounds      importcommon.Bounds
}

func LoadBSP(path string) (*BSP, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseBSP(data, path)
}

func ParseBSP(data []byte, path string) (*BSP, error) {
	const headerSize = 4 + bspLumpCount*8
	if len(data) < headerSize {
		return nil, fmt.Errorf("bsp too small: %d bytes", len(data))
	}
	version := int(readInt32(data, 0))
	if version != BSPVersion30 {
		return nil, fmt.Errorf("unsupported bsp version %d", version)
	}
	out := &BSP{
		Path:    path,
		Version: version,
		SHA256:  sha256Hex(data),
	}
	for i := 0; i < bspLumpCount; i++ {
		base := 4 + i*8
		offset := int(readInt32(data, base))
		length := int(readInt32(data, base+4))
		if offset < 0 || length < 0 || offset > len(data) || length > len(data)-offset {
			return nil, fmt.Errorf("%s lump out of range: offset=%d length=%d file=%d", LumpID(i), offset, length, len(data))
		}
		out.Lumps[i] = Lump{Offset: offset, Length: length}
	}
	entityData := lumpBytes(data, out.Lumps[LumpEntities])
	out.EntityText = strings.TrimRight(string(entityData), "\x00")
	entities, err := ParseEntities(out.EntityText)
	if err != nil {
		return nil, fmt.Errorf("parse entity lump: %w", err)
	}
	out.Entities = entities
	out.Planes = parsePlanes(lumpBytes(data, out.Lumps[LumpPlanes]), &out.Diagnostics)
	out.Nodes = parseNodes(lumpBytes(data, out.Lumps[LumpNodes]), &out.Diagnostics)
	out.Vertices = parseVertices(lumpBytes(data, out.Lumps[LumpVertices]), &out.Diagnostics)
	out.Textures = parseTextures(lumpBytes(data, out.Lumps[LumpTextures]), &out.Diagnostics)
	out.VisibilityData = append([]byte(nil), lumpBytes(data, out.Lumps[LumpVisibility])...)
	out.LightingData = append([]byte(nil), lumpBytes(data, out.Lumps[LumpLighting])...)
	out.TexInfos = parseTexInfos(lumpBytes(data, out.Lumps[LumpTexInfo]), &out.Diagnostics)
	out.Faces = parseFaces(lumpBytes(data, out.Lumps[LumpFaces]), &out.Diagnostics)
	out.Edges = parseEdges(lumpBytes(data, out.Lumps[LumpEdges]), &out.Diagnostics)
	out.SurfEdges = parseSurfEdges(lumpBytes(data, out.Lumps[LumpSurfEdges]), &out.Diagnostics)
	out.Leafs = parseLeafs(lumpBytes(data, out.Lumps[LumpLeafs]), &out.Diagnostics)
	out.Models = parseModels(lumpBytes(data, out.Lumps[LumpModels]), &out.Diagnostics)
	return out, nil
}

func (b *BSP) WorldBoundsHammer() (importcommon.Bounds, bool) {
	if b == nil || len(b.Models) == 0 {
		return importcommon.Bounds{}, false
	}
	return importcommon.Bounds{Min: b.Models[0].Min, Max: b.Models[0].Max}, true
}

func (b *BSP) WorldBoundsGekko() (importcommon.Bounds, bool) {
	if b == nil || len(b.Models) == 0 {
		return importcommon.Bounds{}, false
	}
	model := b.Models[0]
	return HammerBoundsToGekko(model.Min, model.Max), true
}

func (b *BSP) ModelFaces(modelID int) ([]Face, error) {
	if b == nil {
		return nil, fmt.Errorf("bsp is nil")
	}
	if modelID < 0 || modelID >= len(b.Models) {
		return nil, fmt.Errorf("model id %d out of range", modelID)
	}
	model := b.Models[modelID]
	if model.FirstFace < 0 || model.FaceCount < 0 || int(model.FirstFace+model.FaceCount) > len(b.Faces) {
		return nil, fmt.Errorf("model %d face range out of range: first=%d count=%d faces=%d", modelID, model.FirstFace, model.FaceCount, len(b.Faces))
	}
	out := make([]Face, 0, model.FaceCount)
	for i := int32(0); i < model.FaceCount; i++ {
		faceID := int(model.FirstFace + i)
		face, err := b.reconstructFace(modelID, faceID, b.Faces[faceID])
		if err != nil {
			return nil, err
		}
		out = append(out, face)
	}
	return out, nil
}

func (b *BSP) WorldFaces() ([]Face, error) {
	return b.ModelFaces(0)
}

func (b *BSP) PointContentsHammer(modelID int, point importcommon.Vec3) (int32, error) {
	if b == nil {
		return 0, fmt.Errorf("bsp is nil")
	}
	if modelID < 0 || modelID >= len(b.Models) {
		return 0, fmt.Errorf("model id %d out of range", modelID)
	}
	nodeID := b.Models[modelID].HeadNodes[0]
	for {
		if nodeID < 0 {
			leafID := int(^nodeID)
			if leafID < 0 || leafID >= len(b.Leafs) {
				return 0, fmt.Errorf("leaf id %d out of range", leafID)
			}
			return b.Leafs[leafID].Contents, nil
		}
		if int(nodeID) >= len(b.Nodes) {
			return 0, fmt.Errorf("node id %d out of range", nodeID)
		}
		node := b.Nodes[int(nodeID)]
		if int(node.PlaneID) >= len(b.Planes) {
			return 0, fmt.Errorf("node %d plane id %d out of range", nodeID, node.PlaneID)
		}
		plane := b.Planes[int(node.PlaneID)]
		dist := point.X*plane.Normal.X + point.Y*plane.Normal.Y + point.Z*plane.Normal.Z - plane.Dist
		childIndex := 1
		if dist >= 0 {
			childIndex = 0
		}
		nodeID = int32(node.Children[childIndex])
	}
}

func (b *BSP) PointContentsGekko(modelID int, point importcommon.Vec3) (int32, error) {
	return b.PointContentsHammer(modelID, GekkoToHammer(point))
}

func IsSolidContent(contents int32) bool {
	switch contents {
	case ContentsSolid, ContentsClip, ContentsOrigin:
		return true
	default:
		return false
	}
}

func IsPlayableEmptyContent(contents int32) bool {
	switch contents {
	case ContentsEmpty, ContentsWater, ContentsSlime, ContentsLava, ContentsTranslucent:
		return true
	default:
		return false
	}
}

func IsLiquidContent(contents int32) bool {
	switch contents {
	case ContentsWater, ContentsSlime, ContentsLava, ContentsTranslucent:
		return true
	default:
		return false
	}
}

func (b *BSP) reconstructFace(modelID int, faceID int, header FaceHeader) (Face, error) {
	if int(header.PlaneID) >= len(b.Planes) {
		return Face{}, fmt.Errorf("face %d plane id %d out of range", faceID, header.PlaneID)
	}
	if header.EdgeCount < 3 {
		return Face{}, fmt.Errorf("face %d has %d edges", faceID, header.EdgeCount)
	}
	if header.FirstEdge < 0 || int(header.FirstEdge)+int(header.EdgeCount) > len(b.SurfEdges) {
		return Face{}, fmt.Errorf("face %d surfedge range out of range: first=%d count=%d surfedges=%d", faceID, header.FirstEdge, header.EdgeCount, len(b.SurfEdges))
	}
	if header.TexInfoID < 0 || int(header.TexInfoID) >= len(b.TexInfos) {
		return Face{}, fmt.Errorf("face %d texinfo id %d out of range", faceID, header.TexInfoID)
	}
	vertices := make([]importcommon.Vec3, 0, header.EdgeCount)
	for i := 0; i < int(header.EdgeCount); i++ {
		surfEdge := b.SurfEdges[int(header.FirstEdge)+i]
		edgeID := int(surfEdge)
		if edgeID < 0 {
			edgeID = -edgeID
		}
		if edgeID < 0 || edgeID >= len(b.Edges) {
			return Face{}, fmt.Errorf("face %d edge id %d out of range", faceID, edgeID)
		}
		edge := b.Edges[edgeID]
		vertexID := int(edge.A)
		if surfEdge < 0 {
			vertexID = int(edge.B)
		}
		if vertexID < 0 || vertexID >= len(b.Vertices) {
			return Face{}, fmt.Errorf("face %d vertex id %d out of range", faceID, vertexID)
		}
		vertices = append(vertices, b.Vertices[vertexID])
	}
	texInfo := b.TexInfos[header.TexInfoID]
	textureID := int(texInfo.MipTex)
	textureName := ""
	if textureID >= 0 && textureID < len(b.Textures) {
		textureName = b.Textures[textureID].Name
	}
	normal := b.Planes[header.PlaneID].Normal
	if header.Side != 0 {
		normal = importcommon.Vec3{X: -normal.X, Y: -normal.Y, Z: -normal.Z}
	}
	return Face{
		ModelID:     modelID,
		FaceID:      faceID,
		PlaneID:     int(header.PlaneID),
		Side:        int(header.Side),
		Vertices:    vertices,
		Normal:      normal,
		TextureID:   textureID,
		TextureName: textureName,
		TexInfo:     texInfo,
		Styles:      header.Styles,
		LightOfs:    header.LightOfs,
		Bounds:      boundsForVertices(vertices),
	}, nil
}

func parsePlanes(data []byte, diagnostics *[]importcommon.Diagnostic) []Plane {
	const planeSize = 20
	if len(data)%planeSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.planes_lump_misaligned", "planes", fmt.Sprintf("planes lump length %d is not multiple of %d", len(data), planeSize))
	}
	count := len(data) / planeSize
	out := make([]Plane, 0, count)
	for i := 0; i < count; i++ {
		base := i * planeSize
		out = append(out, Plane{
			Normal: importcommon.Vec3{
				X: readFloat32(data, base+0),
				Y: readFloat32(data, base+4),
				Z: readFloat32(data, base+8),
			},
			Dist: readFloat32(data, base+12),
			Type: readInt32(data, base+16),
		})
	}
	return out
}

func parseVertices(data []byte, diagnostics *[]importcommon.Diagnostic) []importcommon.Vec3 {
	const vertexSize = 12
	if len(data)%vertexSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.vertices_lump_misaligned", "vertices", fmt.Sprintf("vertices lump length %d is not multiple of %d", len(data), vertexSize))
	}
	count := len(data) / vertexSize
	out := make([]importcommon.Vec3, 0, count)
	for i := 0; i < count; i++ {
		base := i * vertexSize
		out = append(out, importcommon.Vec3{
			X: readFloat32(data, base+0),
			Y: readFloat32(data, base+4),
			Z: readFloat32(data, base+8),
		})
	}
	return out
}

func parseNodes(data []byte, diagnostics *[]importcommon.Diagnostic) []Node {
	const nodeSize = 24
	if len(data)%nodeSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.nodes_lump_misaligned", "nodes", fmt.Sprintf("nodes lump length %d is not multiple of %d", len(data), nodeSize))
	}
	count := len(data) / nodeSize
	out := make([]Node, 0, count)
	for i := 0; i < count; i++ {
		base := i * nodeSize
		out = append(out, Node{
			PlaneID: readUint32(data, base+0),
			Children: [2]int16{
				readInt16(data, base+4),
				readInt16(data, base+6),
			},
			Min: [3]int16{
				readInt16(data, base+8),
				readInt16(data, base+10),
				readInt16(data, base+12),
			},
			Max: [3]int16{
				readInt16(data, base+14),
				readInt16(data, base+16),
				readInt16(data, base+18),
			},
			FirstFace: readUint16(data, base+20),
			FaceCount: readUint16(data, base+22),
		})
	}
	return out
}

func parseTexInfos(data []byte, diagnostics *[]importcommon.Diagnostic) []TexInfo {
	const texInfoSize = 40
	if len(data)%texInfoSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.texinfo_lump_misaligned", "texinfo", fmt.Sprintf("texinfo lump length %d is not multiple of %d", len(data), texInfoSize))
	}
	count := len(data) / texInfoSize
	out := make([]TexInfo, 0, count)
	for i := 0; i < count; i++ {
		base := i * texInfoSize
		out = append(out, TexInfo{
			S: TextureAxis{
				Axis: importcommon.Vec3{
					X: readFloat32(data, base+0),
					Y: readFloat32(data, base+4),
					Z: readFloat32(data, base+8),
				},
				Shift: readFloat32(data, base+12),
			},
			T: TextureAxis{
				Axis: importcommon.Vec3{
					X: readFloat32(data, base+16),
					Y: readFloat32(data, base+20),
					Z: readFloat32(data, base+24),
				},
				Shift: readFloat32(data, base+28),
			},
			MipTex: readInt32(data, base+32),
			Flags:  readInt32(data, base+36),
		})
	}
	return out
}

func parseFaces(data []byte, diagnostics *[]importcommon.Diagnostic) []FaceHeader {
	const faceSize = 20
	if len(data)%faceSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.faces_lump_misaligned", "faces", fmt.Sprintf("faces lump length %d is not multiple of %d", len(data), faceSize))
	}
	count := len(data) / faceSize
	out := make([]FaceHeader, 0, count)
	for i := 0; i < count; i++ {
		base := i * faceSize
		out = append(out, FaceHeader{
			PlaneID:   readUint16(data, base+0),
			Side:      readInt16(data, base+2),
			FirstEdge: readInt32(data, base+4),
			EdgeCount: readInt16(data, base+8),
			TexInfoID: readInt16(data, base+10),
			Styles:    [4]byte{data[base+12], data[base+13], data[base+14], data[base+15]},
			LightOfs:  readInt32(data, base+16),
		})
	}
	return out
}

func parseEdges(data []byte, diagnostics *[]importcommon.Diagnostic) []Edge {
	const edgeSize = 4
	if len(data)%edgeSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.edges_lump_misaligned", "edges", fmt.Sprintf("edges lump length %d is not multiple of %d", len(data), edgeSize))
	}
	count := len(data) / edgeSize
	out := make([]Edge, 0, count)
	for i := 0; i < count; i++ {
		base := i * edgeSize
		out = append(out, Edge{
			A: readUint16(data, base+0),
			B: readUint16(data, base+2),
		})
	}
	return out
}

func parseSurfEdges(data []byte, diagnostics *[]importcommon.Diagnostic) []int32 {
	const surfEdgeSize = 4
	if len(data)%surfEdgeSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.surfedges_lump_misaligned", "surfedges", fmt.Sprintf("surfedges lump length %d is not multiple of %d", len(data), surfEdgeSize))
	}
	count := len(data) / surfEdgeSize
	out := make([]int32, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, readInt32(data, i*surfEdgeSize))
	}
	return out
}

func parseLeafs(data []byte, diagnostics *[]importcommon.Diagnostic) []Leaf {
	const leafSize = 28
	if len(data)%leafSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.leafs_lump_misaligned", "leafs", fmt.Sprintf("leafs lump length %d is not multiple of %d", len(data), leafSize))
	}
	count := len(data) / leafSize
	out := make([]Leaf, 0, count)
	for i := 0; i < count; i++ {
		base := i * leafSize
		out = append(out, Leaf{
			Contents:         readInt32(data, base+0),
			VisibilityOffset: readInt32(data, base+4),
			Min: [3]int16{
				readInt16(data, base+8),
				readInt16(data, base+10),
				readInt16(data, base+12),
			},
			Max: [3]int16{
				readInt16(data, base+14),
				readInt16(data, base+16),
				readInt16(data, base+18),
			},
			FirstMarkSurface: readUint16(data, base+20),
			MarkSurfaceCount: readUint16(data, base+22),
			AmbientLevels:    [4]byte{data[base+24], data[base+25], data[base+26], data[base+27]},
		})
	}
	return out
}

func parseTextures(data []byte, diagnostics *[]importcommon.Diagnostic) []Texture {
	if len(data) < 4 {
		return nil
	}
	count := int(readInt32(data, 0))
	if count < 0 || count > 1_000_000 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.texture_count_invalid", "textures", fmt.Sprintf("invalid texture count %d", count))
		return nil
	}
	if len(data) < 4+count*4 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.texture_directory_truncated", "textures", "texture directory is truncated")
		return nil
	}
	out := make([]Texture, 0, count)
	for i := 0; i < count; i++ {
		offset := int(readInt32(data, 4+i*4))
		if offset < 0 {
			continue
		}
		if offset == 0 || offset+40 > len(data) {
			appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.texture_offset_invalid", "textures", fmt.Sprintf("texture %d offset %d out of range", i, offset))
			continue
		}
		name := cString(data[offset : offset+16])
		width := readUint32(data, offset+16)
		height := readUint32(data, offset+20)
		embedded := false
		for mip := 0; mip < 4; mip++ {
			if readUint32(data, offset+24+mip*4) != 0 {
				embedded = true
				break
			}
		}
		pixels, _ := decodeMipTexture(data, offset, "bsp")
		baseColor, _ := pixels.AverageColor()
		out = append(out, Texture{Name: name, Width: width, Height: height, Embedded: embedded, BaseColor: baseColor, Pixels: pixels})
	}
	return out
}

func parseModels(data []byte, diagnostics *[]importcommon.Diagnostic) []Model {
	const modelSize = 64
	if len(data)%modelSize != 0 {
		appendDiag(diagnostics, importcommon.SeverityWarning, "hl1.models_lump_misaligned", "models", fmt.Sprintf("models lump length %d is not multiple of %d", len(data), modelSize))
	}
	count := len(data) / modelSize
	out := make([]Model, 0, count)
	for i := 0; i < count; i++ {
		base := i * modelSize
		out = append(out, Model{
			Min: importcommon.Vec3{
				X: readFloat32(data, base+0),
				Y: readFloat32(data, base+4),
				Z: readFloat32(data, base+8),
			},
			Max: importcommon.Vec3{
				X: readFloat32(data, base+12),
				Y: readFloat32(data, base+16),
				Z: readFloat32(data, base+20),
			},
			Origin: importcommon.Vec3{
				X: readFloat32(data, base+24),
				Y: readFloat32(data, base+28),
				Z: readFloat32(data, base+32),
			},
			HeadNodes: [4]int32{
				readInt32(data, base+36),
				readInt32(data, base+40),
				readInt32(data, base+44),
				readInt32(data, base+48),
			},
			VisLeafs:  readInt32(data, base+52),
			FirstFace: readInt32(data, base+56),
			FaceCount: readInt32(data, base+60),
		})
	}
	return out
}

func lumpBytes(data []byte, lump Lump) []byte {
	return data[lump.Offset : lump.Offset+lump.Length]
}

func readInt32(data []byte, offset int) int32 {
	return int32(binary.LittleEndian.Uint32(data[offset:]))
}

func readInt16(data []byte, offset int) int16 {
	return int16(binary.LittleEndian.Uint16(data[offset:]))
}

func readUint32(data []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(data[offset:])
}

func readUint16(data []byte, offset int) uint16 {
	return binary.LittleEndian.Uint16(data[offset:])
}

func readFloat32(data []byte, offset int) float32 {
	return math.Float32frombits(readUint32(data, offset))
}

func boundsForVertices(vertices []importcommon.Vec3) importcommon.Bounds {
	if len(vertices) == 0 {
		return importcommon.Bounds{}
	}
	minV := vertices[0]
	maxV := vertices[0]
	for _, vertex := range vertices[1:] {
		minV = minVec3(minV, vertex)
		maxV = maxVec3(maxV, vertex)
	}
	return importcommon.Bounds{Min: minV, Max: maxV}
}

func cString(data []byte) string {
	if idx := strings.IndexByte(string(data), 0); idx >= 0 {
		data = data[:idx]
	}
	return strings.TrimSpace(string(data))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func appendDiag(diagnostics *[]importcommon.Diagnostic, severity importcommon.Severity, code, subject, message string) {
	if diagnostics == nil {
		return
	}
	*diagnostics = append(*diagnostics, importcommon.Diagnostic{
		Severity: severity,
		Code:     code,
		Subject:  subject,
		Message:  message,
	})
}
