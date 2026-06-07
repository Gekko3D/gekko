package hl1

import (
	"fmt"
	"os"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

const (
	MDLIdentGoldSrc = "IDST"
	MDLVersion10    = 10
	mdlHeaderSize   = 244
)

type MDLInfo struct {
	Name                 string              `json:"name,omitempty"`
	Version              int                 `json:"version"`
	Length               int                 `json:"length"`
	EyePosition          importcommon.Vec3   `json:"eye_position,omitempty"`
	RenderBounds         importcommon.Bounds `json:"render_bounds,omitempty"`
	HitboxBounds         importcommon.Bounds `json:"hitbox_bounds,omitempty"`
	Flags                int                 `json:"flags,omitempty"`
	BoneCount            int                 `json:"bone_count,omitempty"`
	HitboxCount          int                 `json:"hitbox_count,omitempty"`
	SequenceCount        int                 `json:"sequence_count,omitempty"`
	TextureCount         int                 `json:"texture_count,omitempty"`
	SkinRefCount         int                 `json:"skin_ref_count,omitempty"`
	SkinFamilyCount      int                 `json:"skin_family_count,omitempty"`
	BodyPartCount        int                 `json:"body_part_count,omitempty"`
	AttachmentCount      int                 `json:"attachment_count,omitempty"`
	Textures             []MDLTextureInfo    `json:"textures,omitempty"`
	BodyParts            []MDLBodyPartInfo   `json:"body_parts,omitempty"`
	DecodedTriangleCount int                 `json:"decoded_triangle_count,omitempty"`
	DecodedTextureCount  int                 `json:"decoded_texture_count,omitempty"`
}

type MDLTextureInfo struct {
	Name   string `json:"name"`
	Flags  int    `json:"flags,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Index  int    `json:"index,omitempty"`
}

type MDLTexturePixels struct {
	Info    MDLTextureInfo
	Pixels  []byte
	Palette [][3]uint8
}

type MDLGeometry struct {
	Info      MDLInfo
	Textures  []MDLTexturePixels
	Triangles []MDLTriangle
}

type MDLTriangle struct {
	TextureIndex int
	Vertices     [3]MDLTriangleVertex
}

type MDLTriangleVertex struct {
	Position    importcommon.Vec3
	NormalIndex int
	Texel       [2]int
	UV          [2]float32
}

type MDLBodyPartInfo struct {
	Name       string         `json:"name"`
	ModelCount int            `json:"model_count"`
	Base       int            `json:"base,omitempty"`
	Models     []MDLModelInfo `json:"models,omitempty"`
}

type MDLModelInfo struct {
	Name           string  `json:"name"`
	Type           int     `json:"type,omitempty"`
	BoundingRadius float32 `json:"bounding_radius,omitempty"`
	MeshCount      int     `json:"mesh_count,omitempty"`
	VertexCount    int     `json:"vertex_count,omitempty"`
	NormalCount    int     `json:"normal_count,omitempty"`
	GroupCount     int     `json:"group_count,omitempty"`
	TriangleCount  int     `json:"triangle_count,omitempty"`
}

func LoadMDLInfo(path string) (MDLInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MDLInfo{}, err
	}
	geometry, err := ParseMDLGeometry(data)
	if err != nil {
		return MDLInfo{}, err
	}
	return geometry.Info, nil
}

func LoadMDLGeometry(path string) (MDLGeometry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MDLGeometry{}, err
	}
	return ParseMDLGeometry(data)
}

func ParseMDLInfo(data []byte) (MDLInfo, error) {
	if len(data) < mdlHeaderSize {
		return MDLInfo{}, fmt.Errorf("mdl too small: %d bytes", len(data))
	}
	ident := string(data[0:4])
	if ident != MDLIdentGoldSrc {
		return MDLInfo{}, fmt.Errorf("unsupported mdl ident %q", ident)
	}
	version := int(readInt32(data, 4))
	if version != MDLVersion10 {
		return MDLInfo{}, fmt.Errorf("unsupported mdl version %d", version)
	}
	info := MDLInfo{
		Name:    cString(data[8:72]),
		Version: version,
		Length:  int(readInt32(data, 72)),
		EyePosition: importcommon.Vec3{
			X: readFloat32(data, 76),
			Y: readFloat32(data, 80),
			Z: readFloat32(data, 84),
		},
		RenderBounds: importcommon.Bounds{
			Min: importcommon.Vec3{X: readFloat32(data, 88), Y: readFloat32(data, 92), Z: readFloat32(data, 96)},
			Max: importcommon.Vec3{X: readFloat32(data, 100), Y: readFloat32(data, 104), Z: readFloat32(data, 108)},
		},
		HitboxBounds: importcommon.Bounds{
			Min: importcommon.Vec3{X: readFloat32(data, 112), Y: readFloat32(data, 116), Z: readFloat32(data, 120)},
			Max: importcommon.Vec3{X: readFloat32(data, 124), Y: readFloat32(data, 128), Z: readFloat32(data, 132)},
		},
		Flags:           int(readInt32(data, 136)),
		BoneCount:       int(readInt32(data, 140)),
		HitboxCount:     int(readInt32(data, 156)),
		SequenceCount:   int(readInt32(data, 164)),
		TextureCount:    int(readInt32(data, 180)),
		SkinRefCount:    int(readInt32(data, 192)),
		SkinFamilyCount: int(readInt32(data, 196)),
		BodyPartCount:   int(readInt32(data, 204)),
		AttachmentCount: int(readInt32(data, 212)),
	}
	if info.Length <= 0 || info.Length > len(data) {
		info.Length = len(data)
	}
	info.Textures = parseMDLTextures(data, int(readInt32(data, 184)), info.TextureCount)
	info.BodyParts = parseMDLBodyParts(data, int(readInt32(data, 208)), info.BodyPartCount)
	return info, nil
}

func ParseMDLGeometry(data []byte) (MDLGeometry, error) {
	info, err := ParseMDLInfo(data)
	if err != nil {
		return MDLGeometry{}, err
	}
	geometry := MDLGeometry{
		Info:     info,
		Textures: parseMDLTexturePixels(data, info.Textures),
	}
	for _, part := range decodeMDLBodyParts(data, int(readInt32(data, 208)), info.BodyPartCount) {
		for _, model := range part.models {
			geometry.Triangles = append(geometry.Triangles, decodeMDLModelTriangles(data, model, info, geometry.Textures)...)
		}
	}
	geometry.Info.DecodedTriangleCount = len(geometry.Triangles)
	geometry.Info.DecodedTextureCount = len(geometry.Textures)
	return geometry, nil
}

func parseMDLTextures(data []byte, offset int, count int) []MDLTextureInfo {
	const textureSize = 80
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/textureSize {
		return nil
	}
	out := make([]MDLTextureInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*textureSize
		out = append(out, MDLTextureInfo{
			Name:   cString(data[base : base+64]),
			Flags:  int(readInt32(data, base+64)),
			Width:  int(readInt32(data, base+68)),
			Height: int(readInt32(data, base+72)),
			Index:  int(readInt32(data, base+76)),
		})
	}
	return out
}

func parseMDLTexturePixels(data []byte, textures []MDLTextureInfo) []MDLTexturePixels {
	out := make([]MDLTexturePixels, 0, len(textures))
	for _, texture := range textures {
		if texture.Width <= 0 || texture.Height <= 0 || texture.Index < 0 {
			continue
		}
		pixelCount := texture.Width * texture.Height
		pixelStart := texture.Index
		if pixelStart > len(data) || pixelCount > len(data)-pixelStart {
			continue
		}
		paletteStart := pixelStart + pixelCount
		if paletteStart > len(data) || 256 > (len(data)-paletteStart)/3 {
			continue
		}
		pixels := append([]byte(nil), data[pixelStart:pixelStart+pixelCount]...)
		palette := make([][3]uint8, 256)
		for i := range palette {
			base := paletteStart + i*3
			palette[i] = [3]uint8{data[base], data[base+1], data[base+2]}
		}
		out = append(out, MDLTexturePixels{Info: texture, Pixels: pixels, Palette: palette})
	}
	return out
}

func parseMDLBodyParts(data []byte, offset int, count int) []MDLBodyPartInfo {
	const bodyPartSize = 76
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/bodyPartSize {
		return nil
	}
	out := make([]MDLBodyPartInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*bodyPartSize
		modelCount := int(readInt32(data, base+64))
		modelIndex := int(readInt32(data, base+72))
		part := MDLBodyPartInfo{
			Name:       cString(data[base : base+64]),
			ModelCount: modelCount,
			Base:       int(readInt32(data, base+68)),
			Models:     parseMDLModels(data, modelIndex, modelCount),
		}
		out = append(out, part)
	}
	return out
}

type decodedMDLBodyPart struct {
	info   MDLBodyPartInfo
	models []decodedMDLModel
}

type decodedMDLModel struct {
	info        MDLModelInfo
	vertexIndex int
	vertices    []importcommon.Vec3
	meshes      []decodedMDLMesh
}

type decodedMDLMesh struct {
	triangleCommandIndex int
	skinRef              int
}

func decodeMDLBodyParts(data []byte, offset int, count int) []decodedMDLBodyPart {
	const bodyPartSize = 76
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/bodyPartSize {
		return nil
	}
	out := make([]decodedMDLBodyPart, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*bodyPartSize
		modelCount := int(readInt32(data, base+64))
		modelIndex := int(readInt32(data, base+72))
		info := MDLBodyPartInfo{
			Name:       cString(data[base : base+64]),
			ModelCount: modelCount,
			Base:       int(readInt32(data, base+68)),
		}
		out = append(out, decodedMDLBodyPart{
			info:   info,
			models: decodeMDLModels(data, modelIndex, modelCount),
		})
	}
	return out
}

func decodeMDLModels(data []byte, offset int, count int) []decodedMDLModel {
	const modelSize = 112
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/modelSize {
		return nil
	}
	out := make([]decodedMDLModel, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*modelSize
		meshCount := int(readInt32(data, base+72))
		meshIndex := int(readInt32(data, base+76))
		vertexCount := int(readInt32(data, base+80))
		vertexIndex := int(readInt32(data, base+88))
		model := decodedMDLModel{
			info: MDLModelInfo{
				Name:           cString(data[base : base+64]),
				Type:           int(readInt32(data, base+64)),
				BoundingRadius: readFloat32(data, base+68),
				MeshCount:      meshCount,
				VertexCount:    vertexCount,
				NormalCount:    int(readInt32(data, base+92)),
				GroupCount:     int(readInt32(data, base+104)),
			},
			vertexIndex: vertexIndex,
			vertices:    parseMDLVertices(data, vertexIndex, vertexCount),
			meshes:      decodeMDLMeshes(data, meshIndex, meshCount),
		}
		out = append(out, model)
	}
	return out
}

func parseMDLVertices(data []byte, offset int, count int) []importcommon.Vec3 {
	const vertexSize = 12
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/vertexSize {
		return nil
	}
	out := make([]importcommon.Vec3, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*vertexSize
		out = append(out, importcommon.Vec3{
			X: readFloat32(data, base),
			Y: readFloat32(data, base+4),
			Z: readFloat32(data, base+8),
		})
	}
	return out
}

func decodeMDLMeshes(data []byte, offset int, count int) []decodedMDLMesh {
	const meshSize = 20
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/meshSize {
		return nil
	}
	out := make([]decodedMDLMesh, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*meshSize
		out = append(out, decodedMDLMesh{
			triangleCommandIndex: int(readInt32(data, base+4)),
			skinRef:              int(readInt32(data, base+8)),
		})
	}
	return out
}

func decodeMDLModelTriangles(data []byte, model decodedMDLModel, info MDLInfo, textures []MDLTexturePixels) []MDLTriangle {
	out := make([]MDLTriangle, 0)
	for _, mesh := range model.meshes {
		textureIndex := mdlTextureIndexForSkinRef(data, info, mesh.skinRef)
		out = append(out, decodeMDLTriangleCommands(data, mesh.triangleCommandIndex, textureIndex, model.vertices, textures)...)
	}
	return out
}

func decodeMDLTriangleCommands(data []byte, offset int, textureIndex int, vertices []importcommon.Vec3, textures []MDLTexturePixels) []MDLTriangle {
	if offset < 0 || offset+2 > len(data) {
		return nil
	}
	out := make([]MDLTriangle, 0)
	cursor := offset
	for cursor+2 <= len(data) {
		rawCount := int(readInt16(data, cursor))
		cursor += 2
		if rawCount == 0 {
			break
		}
		count := rawCount
		if count < 0 {
			count = -count
		}
		if count < 3 || count > 4096 || count > (len(data)-cursor)/8 {
			break
		}
		commandVertices := make([]MDLTriangleVertex, 0, count)
		for i := 0; i < count; i++ {
			vertexIndex := int(readInt16(data, cursor))
			normalIndex := int(readInt16(data, cursor+2))
			s := int(readInt16(data, cursor+4))
			t := int(readInt16(data, cursor+6))
			cursor += 8
			commandVertices = append(commandVertices, mdlTriangleVertex(vertexIndex, normalIndex, s, t, textureIndex, vertices, textures))
		}
		if rawCount < 0 {
			for i := 1; i+1 < len(commandVertices); i++ {
				out = append(out, MDLTriangle{TextureIndex: textureIndex, Vertices: [3]MDLTriangleVertex{commandVertices[0], commandVertices[i], commandVertices[i+1]}})
			}
			continue
		}
		for i := 0; i+2 < len(commandVertices); i++ {
			if i%2 == 0 {
				out = append(out, MDLTriangle{TextureIndex: textureIndex, Vertices: [3]MDLTriangleVertex{commandVertices[i], commandVertices[i+1], commandVertices[i+2]}})
			} else {
				out = append(out, MDLTriangle{TextureIndex: textureIndex, Vertices: [3]MDLTriangleVertex{commandVertices[i+1], commandVertices[i], commandVertices[i+2]}})
			}
		}
	}
	return out
}

func mdlTriangleVertex(vertexIndex int, normalIndex int, s int, t int, textureIndex int, vertices []importcommon.Vec3, textures []MDLTexturePixels) MDLTriangleVertex {
	out := MDLTriangleVertex{
		NormalIndex: normalIndex,
		Texel:       [2]int{s, t},
	}
	if vertexIndex >= 0 && vertexIndex < len(vertices) {
		out.Position = vertices[vertexIndex]
	}
	if textureIndex >= 0 && textureIndex < len(textures) {
		texture := textures[textureIndex].Info
		if texture.Width > 0 {
			out.UV[0] = float32(s) / float32(texture.Width)
		}
		if texture.Height > 0 {
			out.UV[1] = float32(t) / float32(texture.Height)
		}
	}
	return out
}

func mdlTextureIndexForSkinRef(data []byte, info MDLInfo, skinRef int) int {
	skinIndex := int(readInt32(data, 200))
	if skinRef >= 0 && info.SkinRefCount > 0 && skinRef < info.SkinRefCount && skinIndex >= 0 && skinIndex+2 <= len(data) && skinRef < (len(data)-skinIndex)/2 {
		textureIndex := int(readInt16(data, skinIndex+skinRef*2))
		if textureIndex >= 0 && textureIndex < info.TextureCount {
			return textureIndex
		}
	}
	return skinRef
}

func parseMDLModels(data []byte, offset int, count int) []MDLModelInfo {
	const modelSize = 112
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/modelSize {
		return nil
	}
	out := make([]MDLModelInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*modelSize
		meshCount := int(readInt32(data, base+72))
		meshIndex := int(readInt32(data, base+76))
		out = append(out, MDLModelInfo{
			Name:           cString(data[base : base+64]),
			Type:           int(readInt32(data, base+64)),
			BoundingRadius: readFloat32(data, base+68),
			MeshCount:      meshCount,
			VertexCount:    int(readInt32(data, base+80)),
			NormalCount:    int(readInt32(data, base+92)),
			GroupCount:     int(readInt32(data, base+104)),
			TriangleCount:  countMDLModelTriangles(data, meshIndex, meshCount),
		})
	}
	return out
}

func countMDLModelTriangles(data []byte, offset int, meshCount int) int {
	const meshSize = 20
	if meshCount <= 0 || offset < 0 || offset > len(data) || meshCount > (len(data)-offset)/meshSize {
		return 0
	}
	total := 0
	for i := 0; i < meshCount; i++ {
		base := offset + i*meshSize
		total += int(readInt32(data, base))
	}
	return total
}
