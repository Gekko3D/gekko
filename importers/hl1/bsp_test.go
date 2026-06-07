package hl1

import (
	"encoding/binary"
	"math"
	"testing"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

func TestParseBSPExtractsCoreLumps(t *testing.T) {
	data := syntheticBSP(t, syntheticBSPConfig{
		Entities: `{
"classname" "worldspawn"
"wad" "valve/test.wad"
}
{
"classname" "info_player_start"
"origin" "128 64 32"
"angle" "90"
}`,
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 0, 1), Dist: 0}},
		Vertices: []importcommon.Vec3{vec3(0, 0, 0)},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces:    []FaceHeader{{PlaneID: 0, FirstEdge: 0, EdgeCount: 0, TexInfoID: 0}},
		Edges:    []Edge{{A: 0, B: 0}},
		SurfEdges: []int32{
			0,
		},
		Models: []Model{{
			Min:       vec3(-16, -32, 0),
			Max:       vec3(128, 64, 96),
			FirstFace: 0,
			FaceCount: 0,
		}},
	})
	bsp, err := ParseBSP(data, "test.bsp")
	if err != nil {
		t.Fatalf("ParseBSP failed: %v", err)
	}
	if len(bsp.Entities) != 2 {
		t.Fatalf("entities = %d", len(bsp.Entities))
	}
	if len(bsp.Textures) != 1 || bsp.Textures[0].Name != "TESTWALL" {
		t.Fatalf("textures = %+v", bsp.Textures)
	}
	if len(bsp.Models) != 1 || bsp.Models[0].FaceCount != 0 {
		t.Fatalf("models = %+v", bsp.Models)
	}
	if len(bsp.Planes) != 1 || len(bsp.Vertices) != 1 || len(bsp.TexInfos) != 1 || len(bsp.Faces) != 1 || len(bsp.Edges) != 1 || len(bsp.SurfEdges) != 1 {
		t.Fatalf("geometry counts: planes=%d vertices=%d texinfos=%d faces=%d edges=%d surfedges=%d", len(bsp.Planes), len(bsp.Vertices), len(bsp.TexInfos), len(bsp.Faces), len(bsp.Edges), len(bsp.SurfEdges))
	}
	bounds, ok := bsp.WorldBoundsGekko()
	if !ok {
		t.Fatalf("missing world bounds")
	}
	if bounds.Min.X >= bounds.Max.X || bounds.Min.Y >= bounds.Max.Y || bounds.Min.Z >= bounds.Max.Z {
		t.Fatalf("bad converted bounds: %+v", bounds)
	}
}

func TestModelFacesReconstructsOrderedSquare(t *testing.T) {
	data := syntheticBSP(t, syntheticBSPConfig{
		Entities: "{}",
		Textures: []syntheticTexture{
			{Name: "TESTWALL", Width: 64, Height: 64},
			{Name: "SKY", Width: 64, Height: 64},
		},
		Planes: []Plane{{Normal: vec3(0, 0, 1), Dist: 0}},
		Vertices: []importcommon.Vec3{
			vec3(0, 0, 0),
			vec3(64, 0, 0),
			vec3(64, 64, 0),
			vec3(0, 64, 0),
		},
		TexInfos: []TexInfo{{
			S:      TextureAxis{Axis: vec3(1, 0, 0), Shift: 4},
			T:      TextureAxis{Axis: vec3(0, 1, 0), Shift: 8},
			MipTex: 1,
		}},
		Faces: []FaceHeader{{
			PlaneID:   0,
			FirstEdge: 0,
			EdgeCount: 4,
			TexInfoID: 0,
		}},
		Edges: []Edge{
			{A: 0, B: 1},
			{A: 1, B: 2},
			{A: 2, B: 3},
			{A: 0, B: 3},
		},
		SurfEdges: []int32{0, 1, 2, -3},
		Models: []Model{{
			Min:       vec3(0, 0, 0),
			Max:       vec3(64, 64, 0),
			FirstFace: 0,
			FaceCount: 1,
		}},
	})
	bsp, err := ParseBSP(data, "square.bsp")
	if err != nil {
		t.Fatalf("ParseBSP failed: %v", err)
	}
	faces, err := bsp.WorldFaces()
	if err != nil {
		t.Fatalf("WorldFaces failed: %v", err)
	}
	if len(faces) != 1 {
		t.Fatalf("faces = %d", len(faces))
	}
	face := faces[0]
	if face.TextureName != "SKY" || face.TextureID != 1 {
		t.Fatalf("texture = id %d name %q", face.TextureID, face.TextureName)
	}
	if len(face.Vertices) != 4 {
		t.Fatalf("vertices = %d", len(face.Vertices))
	}
	want := []importcommon.Vec3{vec3(0, 0, 0), vec3(64, 0, 0), vec3(64, 64, 0), vec3(0, 64, 0)}
	for i := range want {
		if face.Vertices[i] != want[i] {
			t.Fatalf("vertex %d = %+v, want %+v", i, face.Vertices[i], want[i])
		}
	}
	if face.TexInfo.S.Shift != 4 || face.TexInfo.T.Shift != 8 {
		t.Fatalf("texinfo = %+v", face.TexInfo)
	}
	if face.Bounds.Min != vec3(0, 0, 0) || face.Bounds.Max != vec3(64, 64, 0) {
		t.Fatalf("bounds = %+v", face.Bounds)
	}
}

func TestModelFacesRejectsBadRefs(t *testing.T) {
	data := syntheticBSP(t, syntheticBSPConfig{
		Entities: "{}",
		Textures: []syntheticTexture{{Name: "TESTWALL", Width: 64, Height: 64}},
		Planes:   []Plane{{Normal: vec3(0, 0, 1), Dist: 0}},
		Vertices: []importcommon.Vec3{vec3(0, 0, 0), vec3(1, 0, 0), vec3(1, 1, 0)},
		TexInfos: []TexInfo{{MipTex: 0}},
		Faces:    []FaceHeader{{PlaneID: 0, FirstEdge: 0, EdgeCount: 3, TexInfoID: 0}},
		Edges:    []Edge{{A: 0, B: 1}},
		SurfEdges: []int32{
			0, 99, 0,
		},
		Models: []Model{{FirstFace: 0, FaceCount: 1}},
	})
	bsp, err := ParseBSP(data, "badrefs.bsp")
	if err != nil {
		t.Fatalf("ParseBSP failed: %v", err)
	}
	if _, err := bsp.WorldFaces(); err == nil {
		t.Fatalf("WorldFaces accepted bad edge ref")
	}
}

func TestPointContentsHammerClassifiesSyntheticTree(t *testing.T) {
	data := syntheticBSP(t, syntheticBSPConfig{
		Entities: "{}",
		Planes:   []Plane{{Normal: vec3(1, 0, 0), Dist: 0}},
		Nodes: []Node{{
			PlaneID:  0,
			Children: [2]int16{-2, -1},
		}},
		Leafs: []Leaf{
			{Contents: ContentsEmpty},
			{Contents: ContentsSolid},
		},
		Models: []Model{{
			HeadNodes: [4]int32{0, -1, -1, -1},
		}},
	})
	bsp, err := ParseBSP(data, "contents.bsp")
	if err != nil {
		t.Fatalf("ParseBSP failed: %v", err)
	}
	solid, err := bsp.PointContentsHammer(0, vec3(1, 0, 0))
	if err != nil {
		t.Fatalf("PointContentsHammer solid failed: %v", err)
	}
	if solid != ContentsSolid || !IsSolidContent(solid) {
		t.Fatalf("solid contents = %d", solid)
	}
	empty, err := bsp.PointContentsHammer(0, vec3(-1, 0, 0))
	if err != nil {
		t.Fatalf("PointContentsHammer empty failed: %v", err)
	}
	if empty != ContentsEmpty || IsSolidContent(empty) {
		t.Fatalf("empty contents = %d", empty)
	}
	gekkoSolid, err := bsp.PointContentsGekko(0, HammerToGekko(vec3(1, 0, 0)))
	if err != nil {
		t.Fatalf("PointContentsGekko failed: %v", err)
	}
	if gekkoSolid != ContentsSolid {
		t.Fatalf("gekko contents = %d", gekkoSolid)
	}
}

func TestPointContentsRejectsBadRefs(t *testing.T) {
	data := syntheticBSP(t, syntheticBSPConfig{
		Entities: "{}",
		Models: []Model{{
			HeadNodes: [4]int32{99, -1, -1, -1},
		}},
	})
	bsp, err := ParseBSP(data, "badcontents.bsp")
	if err != nil {
		t.Fatalf("ParseBSP failed: %v", err)
	}
	if _, err := bsp.PointContentsHammer(0, vec3(0, 0, 0)); err == nil {
		t.Fatalf("PointContentsHammer accepted bad node ref")
	}
}

func TestContentClassification(t *testing.T) {
	if !IsSolidContent(ContentsSolid) || !IsSolidContent(ContentsClip) {
		t.Fatalf("solid contents not classified")
	}
	if !IsPlayableEmptyContent(ContentsEmpty) || !IsPlayableEmptyContent(ContentsWater) {
		t.Fatalf("playable empty contents not classified")
	}
	if IsPlayableEmptyContent(ContentsSky) {
		t.Fatalf("sky classified as playable empty")
	}
}

func TestParseBSPRejectsBadVersion(t *testing.T) {
	data := make([]byte, 4+bspLumpCount*8)
	binary.LittleEndian.PutUint32(data[0:], 29)
	if _, err := ParseBSP(data, "bad.bsp"); err == nil {
		t.Fatalf("ParseBSP accepted unsupported version")
	}
}

func TestParseBSPRejectsBadLumpBounds(t *testing.T) {
	data := make([]byte, 4+bspLumpCount*8)
	binary.LittleEndian.PutUint32(data[0:], BSPVersion30)
	binary.LittleEndian.PutUint32(data[4:], 9999)
	binary.LittleEndian.PutUint32(data[8:], 10)
	if _, err := ParseBSP(data, "bad.bsp"); err == nil {
		t.Fatalf("ParseBSP accepted bad lump bounds")
	}
}

type syntheticBSPConfig struct {
	Entities  string
	Textures  []syntheticTexture
	Planes    []Plane
	Nodes     []Node
	Vertices  []importcommon.Vec3
	TexInfos  []TexInfo
	Faces     []FaceHeader
	Edges     []Edge
	SurfEdges []int32
	Leafs     []Leaf
	Models    []Model
}

type syntheticTexture struct {
	Name   string
	Width  uint32
	Height uint32
}

func syntheticBSP(t *testing.T, cfg syntheticBSPConfig) []byte {
	t.Helper()
	headerSize := 4 + bspLumpCount*8
	data := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(data[0:], BSPVersion30)
	setLump := func(id LumpID, payload []byte) {
		offset := len(data)
		data = append(data, payload...)
		base := 4 + int(id)*8
		binary.LittleEndian.PutUint32(data[base:], uint32(offset))
		binary.LittleEndian.PutUint32(data[base+4:], uint32(len(payload)))
	}
	setLump(LumpEntities, append([]byte(cfg.Entities), 0))
	setLump(LumpPlanes, syntheticPlaneLump(cfg.Planes))
	setLump(LumpNodes, syntheticNodeLump(cfg.Nodes))
	setLump(LumpTextures, syntheticTextureLump(cfg.Textures))
	setLump(LumpVertices, syntheticVertexLump(cfg.Vertices))
	setLump(LumpTexInfo, syntheticTexInfoLump(cfg.TexInfos))
	setLump(LumpFaces, syntheticFaceLump(cfg.Faces))
	setLump(LumpEdges, syntheticEdgeLump(cfg.Edges))
	setLump(LumpSurfEdges, syntheticSurfEdgeLump(cfg.SurfEdges))
	setLump(LumpLeafs, syntheticLeafLump(cfg.Leafs))
	setLump(LumpModels, syntheticModelLump(cfg.Models))
	return data
}

func syntheticPlaneLump(planes []Plane) []byte {
	data := make([]byte, 20*len(planes))
	for i, plane := range planes {
		base := i * 20
		putFloat32(data, base+0, plane.Normal.X)
		putFloat32(data, base+4, plane.Normal.Y)
		putFloat32(data, base+8, plane.Normal.Z)
		putFloat32(data, base+12, plane.Dist)
		binary.LittleEndian.PutUint32(data[base+16:], uint32(plane.Type))
	}
	return data
}

func syntheticTextureLump(textures []syntheticTexture) []byte {
	count := len(textures)
	data := make([]byte, 4+count*4)
	binary.LittleEndian.PutUint32(data[0:], uint32(count))
	for i, texture := range textures {
		offset := len(data)
		binary.LittleEndian.PutUint32(data[4+i*4:], uint32(offset))
		record := make([]byte, 40)
		copy(record[0:16], []byte(texture.Name))
		binary.LittleEndian.PutUint32(record[16:], texture.Width)
		binary.LittleEndian.PutUint32(record[20:], texture.Height)
		data = append(data, record...)
	}
	return data
}

func syntheticVertexLump(vertices []importcommon.Vec3) []byte {
	data := make([]byte, 12*len(vertices))
	for i, vertex := range vertices {
		base := i * 12
		putFloat32(data, base+0, vertex.X)
		putFloat32(data, base+4, vertex.Y)
		putFloat32(data, base+8, vertex.Z)
	}
	return data
}

func syntheticNodeLump(nodes []Node) []byte {
	data := make([]byte, 24*len(nodes))
	for i, node := range nodes {
		base := i * 24
		binary.LittleEndian.PutUint32(data[base+0:], node.PlaneID)
		binary.LittleEndian.PutUint16(data[base+4:], uint16(node.Children[0]))
		binary.LittleEndian.PutUint16(data[base+6:], uint16(node.Children[1]))
		binary.LittleEndian.PutUint16(data[base+8:], uint16(node.Min[0]))
		binary.LittleEndian.PutUint16(data[base+10:], uint16(node.Min[1]))
		binary.LittleEndian.PutUint16(data[base+12:], uint16(node.Min[2]))
		binary.LittleEndian.PutUint16(data[base+14:], uint16(node.Max[0]))
		binary.LittleEndian.PutUint16(data[base+16:], uint16(node.Max[1]))
		binary.LittleEndian.PutUint16(data[base+18:], uint16(node.Max[2]))
		binary.LittleEndian.PutUint16(data[base+20:], node.FirstFace)
		binary.LittleEndian.PutUint16(data[base+22:], node.FaceCount)
	}
	return data
}

func syntheticTexInfoLump(texInfos []TexInfo) []byte {
	data := make([]byte, 40*len(texInfos))
	for i, texInfo := range texInfos {
		base := i * 40
		putFloat32(data, base+0, texInfo.S.Axis.X)
		putFloat32(data, base+4, texInfo.S.Axis.Y)
		putFloat32(data, base+8, texInfo.S.Axis.Z)
		putFloat32(data, base+12, texInfo.S.Shift)
		putFloat32(data, base+16, texInfo.T.Axis.X)
		putFloat32(data, base+20, texInfo.T.Axis.Y)
		putFloat32(data, base+24, texInfo.T.Axis.Z)
		putFloat32(data, base+28, texInfo.T.Shift)
		binary.LittleEndian.PutUint32(data[base+32:], uint32(texInfo.MipTex))
		binary.LittleEndian.PutUint32(data[base+36:], uint32(texInfo.Flags))
	}
	return data
}

func syntheticFaceLump(faces []FaceHeader) []byte {
	data := make([]byte, 20*len(faces))
	for i, face := range faces {
		base := i * 20
		binary.LittleEndian.PutUint16(data[base+0:], face.PlaneID)
		binary.LittleEndian.PutUint16(data[base+2:], uint16(face.Side))
		binary.LittleEndian.PutUint32(data[base+4:], uint32(face.FirstEdge))
		binary.LittleEndian.PutUint16(data[base+8:], uint16(face.EdgeCount))
		binary.LittleEndian.PutUint16(data[base+10:], uint16(face.TexInfoID))
		copy(data[base+12:base+16], face.Styles[:])
		binary.LittleEndian.PutUint32(data[base+16:], uint32(face.LightOfs))
	}
	return data
}

func syntheticEdgeLump(edges []Edge) []byte {
	data := make([]byte, 4*len(edges))
	for i, edge := range edges {
		base := i * 4
		binary.LittleEndian.PutUint16(data[base+0:], edge.A)
		binary.LittleEndian.PutUint16(data[base+2:], edge.B)
	}
	return data
}

func syntheticSurfEdgeLump(surfEdges []int32) []byte {
	data := make([]byte, 4*len(surfEdges))
	for i, surfEdge := range surfEdges {
		binary.LittleEndian.PutUint32(data[i*4:], uint32(surfEdge))
	}
	return data
}

func syntheticLeafLump(leafs []Leaf) []byte {
	data := make([]byte, 28*len(leafs))
	for i, leaf := range leafs {
		base := i * 28
		binary.LittleEndian.PutUint32(data[base+0:], uint32(leaf.Contents))
		binary.LittleEndian.PutUint32(data[base+4:], uint32(leaf.VisibilityOffset))
		binary.LittleEndian.PutUint16(data[base+8:], uint16(leaf.Min[0]))
		binary.LittleEndian.PutUint16(data[base+10:], uint16(leaf.Min[1]))
		binary.LittleEndian.PutUint16(data[base+12:], uint16(leaf.Min[2]))
		binary.LittleEndian.PutUint16(data[base+14:], uint16(leaf.Max[0]))
		binary.LittleEndian.PutUint16(data[base+16:], uint16(leaf.Max[1]))
		binary.LittleEndian.PutUint16(data[base+18:], uint16(leaf.Max[2]))
		binary.LittleEndian.PutUint16(data[base+20:], leaf.FirstMarkSurface)
		binary.LittleEndian.PutUint16(data[base+22:], leaf.MarkSurfaceCount)
		copy(data[base+24:base+28], leaf.AmbientLevels[:])
	}
	return data
}

func syntheticModelLump(models []Model) []byte {
	data := make([]byte, 64*len(models))
	for i, model := range models {
		base := i * 64
		putFloat32(data, base+0, model.Min.X)
		putFloat32(data, base+4, model.Min.Y)
		putFloat32(data, base+8, model.Min.Z)
		putFloat32(data, base+12, model.Max.X)
		putFloat32(data, base+16, model.Max.Y)
		putFloat32(data, base+20, model.Max.Z)
		putFloat32(data, base+24, model.Origin.X)
		putFloat32(data, base+28, model.Origin.Y)
		putFloat32(data, base+32, model.Origin.Z)
		binary.LittleEndian.PutUint32(data[base+36:], uint32(model.HeadNodes[0]))
		binary.LittleEndian.PutUint32(data[base+40:], uint32(model.HeadNodes[1]))
		binary.LittleEndian.PutUint32(data[base+44:], uint32(model.HeadNodes[2]))
		binary.LittleEndian.PutUint32(data[base+48:], uint32(model.HeadNodes[3]))
		binary.LittleEndian.PutUint32(data[base+52:], uint32(model.VisLeafs))
		binary.LittleEndian.PutUint32(data[base+56:], uint32(model.FirstFace))
		binary.LittleEndian.PutUint32(data[base+60:], uint32(model.FaceCount))
	}
	return data
}

func putFloat32(data []byte, offset int, value float32) {
	binary.LittleEndian.PutUint32(data[offset:], math.Float32bits(value))
}

func vec3(x, y, z float32) importcommon.Vec3 {
	return importcommon.Vec3{X: x, Y: y, Z: z}
}
