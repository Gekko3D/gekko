package hl1

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	importcommon "github.com/gekko3d/gekko/importers/common"
)

const (
	MDLIdentGoldSrc = "IDST"
	MDLVersion10    = 10
	mdlHeaderSize   = 244

	maxMDLSequenceCount      = 2048
	maxMDLSequenceFrameCount = 2048
	maxMDLSequenceBlendCount = 4
	mdlSequenceRecordSize176 = 176
	mdlSequenceRecordSize180 = 180
	mdlSequenceEventSize     = 76
	mdlSequenceFlagLooping   = 1
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
	Bones                []MDLBoneInfo       `json:"bones,omitempty"`
	Sequences            []MDLSequenceInfo   `json:"sequences,omitempty"`
	Textures             []MDLTextureInfo    `json:"textures,omitempty"`
	BodyParts            []MDLBodyPartInfo   `json:"body_parts,omitempty"`
	Hitboxes             []MDLHitboxInfo     `json:"hitboxes,omitempty"`
	Attachments          []MDLAttachmentInfo `json:"attachments,omitempty"`
	DecodedTriangleCount int                 `json:"decoded_triangle_count,omitempty"`
	DecodedTextureCount  int                 `json:"decoded_texture_count,omitempty"`
}

type MDLBoneInfo struct {
	Name          string            `json:"name"`
	Parent        int               `json:"parent"`
	Position      importcommon.Vec3 `json:"position,omitempty"`
	Rotation      importcommon.Vec3 `json:"rotation,omitempty"`
	PositionScale importcommon.Vec3 `json:"position_scale,omitempty"`
	RotationScale importcommon.Vec3 `json:"rotation_scale,omitempty"`
}

type MDLHitboxInfo struct {
	Bone   int                 `json:"bone"`
	Group  int                 `json:"group"`
	Bounds importcommon.Bounds `json:"bounds"`
}

type MDLAttachmentInfo struct {
	Name    string               `json:"name,omitempty"`
	Type    int                  `json:"type,omitempty"`
	Bone    int                  `json:"bone"`
	Origin  importcommon.Vec3    `json:"origin,omitempty"`
	Vectors [3]importcommon.Vec3 `json:"vectors,omitempty"`
}

type MDLSequenceInfo struct {
	Name            string                   `json:"name"`
	Flags           int                      `json:"flags,omitempty"`
	Loop            bool                     `json:"loop,omitempty"`
	Activity        int                      `json:"activity"`
	ActivityWeight  int                      `json:"activity_weight"`
	FPS             float32                  `json:"fps,omitempty"`
	FrameCount      int                      `json:"frame_count,omitempty"`
	NumBlends       int                      `json:"num_blends,omitempty"`
	BlendType       [2]int                   `json:"blend_type,omitempty"`
	BlendStart      [2]float32               `json:"blend_start,omitempty"`
	BlendEnd        [2]float32               `json:"blend_end,omitempty"`
	AnimIndex       int                      `json:"anim_index,omitempty"`
	SeqGroup        int                      `json:"seq_group,omitempty"`
	Events          []MDLSequenceEventInfo   `json:"events,omitempty"`
	BoneAnimations  []MDLBoneAnimationInfo   `json:"bone_animations,omitempty"`
	BlendAnimations [][]MDLBoneAnimationInfo `json:"blend_animations,omitempty"`
}

type MDLSequenceEventInfo struct {
	Frame   int    `json:"frame"`
	ID      int    `json:"id"`
	Type    int    `json:"type,omitempty"`
	Options string `json:"options,omitempty"`
}

type MDLBoneAnimationInfo struct {
	BoneIndex      int                 `json:"bone_index"`
	PositionFrames []importcommon.Vec3 `json:"position_frames,omitempty"`
	RotationFrames []importcommon.Vec3 `json:"rotation_frames,omitempty"`
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
	Info                 MDLInfo
	Textures             []MDLTexturePixels
	Triangles            []MDLTriangle
	AnimationDiagnostics []string
}

// MDLGeometryOptions selects source-model variants before voxelization.
type MDLGeometryOptions struct {
	// BodygroupModels holds one model index for every body part. A missing
	// index keeps the historical behavior of decoding every model in that part.
	BodygroupModels []int
	// SkinFamily selects one GoldSrc skin family. Zero is the GoldSrc default.
	SkinFamily int
	// DefaultBodygroups is retained for existing callers that need first model
	// from every bodygroup.
	DefaultBodygroups bool
}

type MDLTriangle struct {
	TextureIndex int
	Vertices     [3]MDLTriangleVertex
}

type MDLTriangleVertex struct {
	Position      importcommon.Vec3
	LocalPosition importcommon.Vec3
	BoneIndex     int
	NormalIndex   int
	Texel         [2]int
	UV            [2]float32
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
	info, err := ParseMDLInfo(data)
	if err != nil || info.TextureCount != 0 {
		return info, err
	}
	if companionPath := mdlTextureCompanionPath(path); companionPath != "" {
		if companionData, readErr := os.ReadFile(companionPath); readErr == nil {
			if companion, parseErr := ParseMDLInfo(companionData); parseErr == nil {
				info.TextureCount, info.Textures = companion.TextureCount, companion.Textures
				info.SkinRefCount, info.SkinFamilyCount = companion.SkinRefCount, companion.SkinFamilyCount
			}
		}
	}
	return info, nil
}

func LoadMDLGeometry(path string) (MDLGeometry, error) {
	return LoadMDLGeometryWithOptions(path, MDLGeometryOptions{})
}

func LoadMDLGeometryWithOptions(path string, opts MDLGeometryOptions) (MDLGeometry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MDLGeometry{}, err
	}
	geometry, err := ParseMDLGeometryWithOptions(data, opts)
	if err != nil {
		return MDLGeometry{}, err
	}
	if len(geometry.Textures) == 0 {
		if companionPath := mdlTextureCompanionPath(path); companionPath != "" {
			if companionData, readErr := os.ReadFile(companionPath); readErr == nil {
				if textures, textureInfos := parseMDLExternalTexturePixels(companionData); len(textures) > 0 {
					companionInfo, infoErr := ParseMDLInfo(companionData)
					if infoErr != nil {
						return MDLGeometry{}, infoErr
					}
					geometry, err = parseMDLGeometryWithExternalTexturesOptions(data, textures, textureInfos, companionData, &companionInfo, opts)
					if err != nil {
						return MDLGeometry{}, err
					}
				}
			}
		}
	}
	geometry.AnimationDiagnostics = decodeMDLExternalSequenceGroups(data, path, &geometry.Info, nil)
	return geometry, nil
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
	info.Bones = parseMDLBones(data, int(readInt32(data, 144)), info.BoneCount)
	info.Hitboxes = parseMDLHitboxes(data, int(readInt32(data, 160)), info.HitboxCount)
	info.Sequences = parseMDLSequences(data, int(readInt32(data, 168)), info.SequenceCount, info.Bones)
	info.Textures = parseMDLTextures(data, int(readInt32(data, 184)), info.TextureCount)
	info.BodyParts = parseMDLBodyParts(data, int(readInt32(data, 208)), info.BodyPartCount)
	info.Attachments = parseMDLAttachments(data, int(readInt32(data, 216)), info.AttachmentCount)
	return info, nil
}

func ParseMDLGeometry(data []byte) (MDLGeometry, error) {
	return ParseMDLGeometryWithOptions(data, MDLGeometryOptions{})
}

func ParseMDLGeometryWithOptions(data []byte, opts MDLGeometryOptions) (MDLGeometry, error) {
	return parseMDLGeometryWithExternalTexturesOptions(data, nil, nil, nil, nil, opts)
}

func ParseMDLGeometryWithExternalTextures(data []byte, externalTextures []MDLTexturePixels, externalTextureInfos []MDLTextureInfo) (MDLGeometry, error) {
	return parseMDLGeometryWithExternalTexturesOptions(data, externalTextures, externalTextureInfos, nil, nil, MDLGeometryOptions{})
}

func parseMDLGeometryWithExternalTexturesOptions(data []byte, externalTextures []MDLTexturePixels, externalTextureInfos []MDLTextureInfo, externalSkinData []byte, externalSkinInfo *MDLInfo, opts MDLGeometryOptions) (MDLGeometry, error) {
	info, err := ParseMDLInfo(data)
	if err != nil {
		return MDLGeometry{}, err
	}
	textures := parseMDLTexturePixels(data, info.Textures)
	if len(textures) == 0 && len(externalTextures) > 0 {
		textures = externalTextures
		info.TextureCount = len(externalTextures)
		if len(externalTextureInfos) > 0 {
			info.Textures = externalTextureInfos
		} else {
			info.Textures = make([]MDLTextureInfo, 0, len(externalTextures))
			for _, texture := range externalTextures {
				info.Textures = append(info.Textures, texture.Info)
			}
		}
	}
	skinData, skinInfo := data, info
	if externalSkinInfo != nil && len(externalSkinData) > 0 {
		skinData, skinInfo = externalSkinData, *externalSkinInfo
		info.SkinRefCount, info.SkinFamilyCount = skinInfo.SkinRefCount, skinInfo.SkinFamilyCount
	}
	geometry := MDLGeometry{
		Info:     info,
		Textures: textures,
	}
	boneTransforms := parseMDLBoneTransforms(data, int(readInt32(data, 144)), info.BoneCount)
	for partIndex, part := range decodeMDLBodyParts(data, int(readInt32(data, 208)), info.BodyPartCount, boneTransforms) {
		models := part.models
		if partIndex < len(opts.BodygroupModels) {
			selected := opts.BodygroupModels[partIndex]
			if selected < 0 || selected >= len(models) {
				return MDLGeometry{}, fmt.Errorf("bodygroup %q model %d out of range", part.info.Name, selected)
			}
			models = models[selected : selected+1]
		} else if opts.DefaultBodygroups && len(models) > 1 {
			models = models[:1]
		}
		for _, model := range models {
			geometry.Triangles = append(geometry.Triangles, decodeMDLModelTriangles(data, model, skinData, skinInfo, geometry.Textures, opts.SkinFamily)...)
		}
	}
	geometry.Info.DecodedTriangleCount = len(geometry.Triangles)
	geometry.Info.DecodedTextureCount = len(geometry.Textures)
	return geometry, nil
}

func parseMDLExternalTexturePixels(data []byte) ([]MDLTexturePixels, []MDLTextureInfo) {
	info, err := ParseMDLInfo(data)
	if err != nil {
		return nil, nil
	}
	textures := parseMDLTexturePixels(data, info.Textures)
	return textures, info.Textures
}

func mdlTextureCompanionPath(path string) string {
	ext := filepath.Ext(path)
	if !strings.EqualFold(ext, ".mdl") {
		return ""
	}
	base := strings.TrimSuffix(path, ext)
	return base + "t" + ext
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
	vertices    []decodedMDLVertex
	meshes      []decodedMDLMesh
}

type decodedMDLMesh struct {
	triangleCommandIndex int
	skinRef              int
}

type decodedMDLVertex struct {
	Position      importcommon.Vec3
	LocalPosition importcommon.Vec3
	BoneIndex     int
}

type mdlBoneTransform struct {
	Parent   int
	Position importcommon.Vec3
	Rotation importcommon.Vec3
}

func parseMDLBones(data []byte, offset int, count int) []MDLBoneInfo {
	const boneSize = 112
	if count <= 0 || offset < mdlHeaderSize || offset > len(data) || count > (len(data)-offset)/boneSize {
		return nil
	}
	out := make([]MDLBoneInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*boneSize
		out = append(out, MDLBoneInfo{
			Name:   cString(data[base : base+32]),
			Parent: int(readInt32(data, base+32)),
			Position: importcommon.Vec3{
				X: readFloat32(data, base+64),
				Y: readFloat32(data, base+68),
				Z: readFloat32(data, base+72),
			},
			Rotation: importcommon.Vec3{
				X: readFloat32(data, base+76),
				Y: readFloat32(data, base+80),
				Z: readFloat32(data, base+84),
			},
			PositionScale: importcommon.Vec3{
				X: readFloat32(data, base+88),
				Y: readFloat32(data, base+92),
				Z: readFloat32(data, base+96),
			},
			RotationScale: importcommon.Vec3{
				X: readFloat32(data, base+100),
				Y: readFloat32(data, base+104),
				Z: readFloat32(data, base+108),
			},
		})
	}
	return out
}

func parseMDLHitboxes(data []byte, offset int, count int) []MDLHitboxInfo {
	const hitboxSize = 32
	if count <= 0 || offset < mdlHeaderSize || offset > len(data) || count > (len(data)-offset)/hitboxSize {
		return nil
	}
	out := make([]MDLHitboxInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*hitboxSize
		out = append(out, MDLHitboxInfo{
			Bone:  int(readInt32(data, base)),
			Group: int(readInt32(data, base+4)),
			Bounds: importcommon.Bounds{
				Min: importcommon.Vec3{X: readFloat32(data, base+8), Y: readFloat32(data, base+12), Z: readFloat32(data, base+16)},
				Max: importcommon.Vec3{X: readFloat32(data, base+20), Y: readFloat32(data, base+24), Z: readFloat32(data, base+28)},
			},
		})
	}
	return out
}

func parseMDLAttachments(data []byte, offset int, count int) []MDLAttachmentInfo {
	const attachmentSize = 88
	if count <= 0 || offset < mdlHeaderSize || offset > len(data) || count > (len(data)-offset)/attachmentSize {
		return nil
	}
	out := make([]MDLAttachmentInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*attachmentSize
		attachment := MDLAttachmentInfo{
			Name: cString(data[base : base+32]), Type: int(readInt32(data, base+32)), Bone: int(readInt32(data, base+36)),
			Origin: importcommon.Vec3{X: readFloat32(data, base+40), Y: readFloat32(data, base+44), Z: readFloat32(data, base+48)},
		}
		for axis := range attachment.Vectors {
			vector := base + 52 + axis*12
			attachment.Vectors[axis] = importcommon.Vec3{X: readFloat32(data, vector), Y: readFloat32(data, vector+4), Z: readFloat32(data, vector+8)}
		}
		out = append(out, attachment)
	}
	return out
}

func parseMDLSequences(data []byte, offset int, count int, bones []MDLBoneInfo) []MDLSequenceInfo {
	sequenceSize := mdlSequenceRecordSize(data, offset, count)
	if sequenceSize == 0 {
		return nil
	}
	out := make([]MDLSequenceInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*sequenceSize
		seq := MDLSequenceInfo{
			Name:           cString(data[base : base+32]),
			FPS:            readFloat32(data, base+32),
			Flags:          int(readInt32(data, base+36)),
			Activity:       int(readInt32(data, base+40)),
			ActivityWeight: int(readInt32(data, base+44)),
			FrameCount:     int(readInt32(data, base+56)),
			NumBlends:      int(readInt32(data, base+120)),
			BlendType:      [2]int{int(readInt32(data, base+128)), int(readInt32(data, base+132))},
			BlendStart:     [2]float32{readFloat32(data, base+136), readFloat32(data, base+140)},
			BlendEnd:       [2]float32{readFloat32(data, base+144), readFloat32(data, base+148)},
			AnimIndex:      int(readInt32(data, base+124)),
			SeqGroup:       int(readInt32(data, base+156)),
		}
		seq.Loop = seq.Flags&mdlSequenceFlagLooping != 0
		seq.Events = parseMDLSequenceEvents(data, int(readInt32(data, base+52)), int(readInt32(data, base+48)))
		setMDLSequenceAnimations(&seq, decodeMDLSequenceAnimationBlends(data, seq, bones))
		out = append(out, seq)
	}
	return out
}

func parseMDLSequenceEvents(data []byte, offset, count int) []MDLSequenceEventInfo {
	if count <= 0 || offset < mdlHeaderSize || offset > len(data) || count > (len(data)-offset)/mdlSequenceEventSize {
		return nil
	}
	out := make([]MDLSequenceEventInfo, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*mdlSequenceEventSize
		out = append(out, MDLSequenceEventInfo{
			Frame:   int(readInt32(data, base)),
			ID:      int(readInt32(data, base+4)),
			Type:    int(readInt32(data, base+8)),
			Options: cString(data[base+12 : base+76]),
		})
	}
	return out
}

func mdlSequenceRecordSize(data []byte, offset int, count int) int {
	if count <= 0 || count > maxMDLSequenceCount || offset < mdlHeaderSize || offset > len(data) {
		return 0
	}
	bestSize := 0
	bestScore := -1 << 30
	for _, size := range []int{mdlSequenceRecordSize176, mdlSequenceRecordSize180} {
		if count > (len(data)-offset)/size {
			continue
		}
		score := mdlSequenceRecordScore(data, offset, count, size)
		if score > bestScore {
			bestScore = score
			bestSize = size
		}
	}
	if bestScore <= -count*4 {
		return 0
	}
	return bestSize
}

func mdlSequenceRecordScore(data []byte, offset int, count int, size int) int {
	score := 0
	for i := 0; i < count; i++ {
		base := offset + i*size
		nameBytes := data[base : base+32]
		if isLikelyMDLLabel(nameBytes) {
			score += 2
		} else {
			score -= 4
		}
		fps := readFloat32(data, base+32)
		if fps > 0 && fps <= 240 && !math.IsNaN(float64(fps)) && !math.IsInf(float64(fps), 0) {
			score += 2
		} else {
			score -= 3
		}
		frameCount := int(readInt32(data, base+56))
		if frameCount > 0 && frameCount <= maxMDLSequenceFrameCount {
			score += 2
		} else {
			score -= 4
		}
		numBlends := int(readInt32(data, base+120))
		if numBlends >= 0 && numBlends <= maxMDLSequenceBlendCount {
			score += 1
		} else {
			score -= 3
		}
		animIndex := int(readInt32(data, base+124))
		seqGroup := int(readInt32(data, base+156))
		if seqGroup >= 0 && seqGroup <= 64 {
			score += 1
		} else {
			score -= 2
		}
		if seqGroup == 0 && animIndex != 0 {
			if animIndex >= mdlHeaderSize && animIndex < len(data) {
				score += 1
			} else {
				score -= 2
			}
		}
	}
	return score
}

func isLikelyMDLLabel(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}

func decodeMDLSequenceAnimationBlends(data []byte, seq MDLSequenceInfo, bones []MDLBoneInfo) [][]MDLBoneAnimationInfo {
	if seq.SeqGroup != 0 || seq.AnimIndex <= 0 || seq.FrameCount <= 0 || seq.FrameCount > maxMDLSequenceFrameCount || len(bones) == 0 {
		return nil
	}
	numBlends := seq.NumBlends
	if numBlends <= 0 {
		numBlends = 1
	}
	if numBlends > maxMDLSequenceBlendCount {
		return nil
	}
	const animSize = 12
	if seq.AnimIndex < 0 || seq.AnimIndex > len(data) || len(bones)*numBlends > (len(data)-seq.AnimIndex)/animSize {
		return nil
	}
	out := make([][]MDLBoneAnimationInfo, numBlends)
	for blend := 0; blend < numBlends; blend++ {
		out[blend] = make([]MDLBoneAnimationInfo, 0, len(bones))
		for boneIndex, bone := range bones {
			animBase := seq.AnimIndex + (blend*len(bones)+boneIndex)*animSize
			animation := MDLBoneAnimationInfo{
				BoneIndex:      boneIndex,
				PositionFrames: make([]importcommon.Vec3, 0, seq.FrameCount),
				RotationFrames: make([]importcommon.Vec3, 0, seq.FrameCount),
			}
			for frame := 0; frame < seq.FrameCount; frame++ {
				px := decodeMDLAnimationChannelValue(data, animBase, 0, frame, bone.Position.X, bone.PositionScale.X)
				py := decodeMDLAnimationChannelValue(data, animBase, 1, frame, bone.Position.Y, bone.PositionScale.Y)
				pz := decodeMDLAnimationChannelValue(data, animBase, 2, frame, bone.Position.Z, bone.PositionScale.Z)
				rx := decodeMDLAnimationChannelValue(data, animBase, 3, frame, bone.Rotation.X, bone.RotationScale.X)
				ry := decodeMDLAnimationChannelValue(data, animBase, 4, frame, bone.Rotation.Y, bone.RotationScale.Y)
				rz := decodeMDLAnimationChannelValue(data, animBase, 5, frame, bone.Rotation.Z, bone.RotationScale.Z)
				animation.PositionFrames = append(animation.PositionFrames, importcommon.Vec3{X: px, Y: py, Z: pz})
				animation.RotationFrames = append(animation.RotationFrames, importcommon.Vec3{X: rx, Y: ry, Z: rz})
			}
			out[blend] = append(out[blend], animation)
		}
	}
	return out
}

func setMDLSequenceAnimations(seq *MDLSequenceInfo, blends [][]MDLBoneAnimationInfo) {
	seq.BoneAnimations, seq.BlendAnimations = nil, nil
	if len(blends) == 1 {
		seq.BoneAnimations = blends[0]
	} else if len(blends) > 1 {
		seq.BlendAnimations = blends
	}
}

func decodeMDLAnimationChannelValue(data []byte, animBase int, channel int, frame int, baseValue float32, scale float32) float32 {
	if animBase < 0 || animBase+12 > len(data) || channel < 0 || channel >= 6 || frame < 0 {
		return baseValue
	}
	offset := int(readUint16(data, animBase+channel*2))
	if offset == 0 {
		return baseValue
	}
	raw, ok := decodeMDLAnimationChannelRawValue(data, animBase+offset, frame)
	if !ok {
		return baseValue
	}
	return baseValue + float32(raw)*scale
}

func decodeMDLAnimationChannelRawValue(data []byte, offset int, frame int) (int16, bool) {
	if offset < 0 || offset+2 > len(data) || frame < 0 {
		return 0, false
	}
	cursor := offset
	remaining := frame
	for cursor+2 <= len(data) {
		valid := int(data[cursor])
		total := int(data[cursor+1])
		cursor += 2
		if total <= 0 || valid < 0 || valid > total {
			return 0, false
		}
		if cursor+valid*2 > len(data) {
			return 0, false
		}
		if remaining < total {
			if valid == 0 {
				return 0, true
			}
			valueIndex := remaining
			if valueIndex >= valid {
				valueIndex = valid - 1
			}
			return readInt16(data, cursor+valueIndex*2), true
		}
		remaining -= total
		cursor += valid * 2
	}
	return 0, false
}

func parseMDLBoneTransforms(data []byte, offset int, count int) []mdlBoneTransform {
	bones := parseMDLBones(data, offset, count)
	out := make([]mdlBoneTransform, 0, len(bones))
	for _, bone := range bones {
		out = append(out, mdlBoneTransform{
			Parent:   bone.Parent,
			Position: bone.Position,
			Rotation: bone.Rotation,
		})
	}
	return out
}

func decodeMDLBodyParts(data []byte, offset int, count int, boneTransforms []mdlBoneTransform) []decodedMDLBodyPart {
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
			models: decodeMDLModels(data, modelIndex, modelCount, boneTransforms),
		})
	}
	return out
}

func decodeMDLModels(data []byte, offset int, count int, boneTransforms []mdlBoneTransform) []decodedMDLModel {
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
		vertexInfoIndex := int(readInt32(data, base+84))
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
			vertices:    parseMDLVertices(data, vertexIndex, vertexInfoIndex, vertexCount, boneTransforms),
			meshes:      decodeMDLMeshes(data, meshIndex, meshCount),
		}
		out = append(out, model)
	}
	return out
}

func parseMDLVertices(data []byte, offset int, boneIndexOffset int, count int, boneTransforms []mdlBoneTransform) []decodedMDLVertex {
	const vertexSize = 12
	if count <= 0 || offset < 0 || offset > len(data) || count > (len(data)-offset)/vertexSize {
		return nil
	}
	boneIndices := parseMDLVertexBoneIndices(data, boneIndexOffset, count)
	out := make([]decodedMDLVertex, 0, count)
	for i := 0; i < count; i++ {
		base := offset + i*vertexSize
		localPosition := importcommon.Vec3{
			X: readFloat32(data, base),
			Y: readFloat32(data, base+4),
			Z: readFloat32(data, base+8),
		}
		position := localPosition
		boneIndex := -1
		if len(boneIndices) == count {
			boneIndex = int(boneIndices[i])
			position = transformMDLVertexByBone(position, boneIndex, boneTransforms)
		}
		out = append(out, decodedMDLVertex{
			Position:      position,
			LocalPosition: localPosition,
			BoneIndex:     boneIndex,
		})
	}
	return out
}

func parseMDLVertexBoneIndices(data []byte, offset int, count int) []byte {
	if count <= 0 || offset < mdlHeaderSize || offset > len(data) || count > len(data)-offset {
		return nil
	}
	return append([]byte(nil), data[offset:offset+count]...)
}

func transformMDLVertexByBone(position importcommon.Vec3, boneIndex int, boneTransforms []mdlBoneTransform) importcommon.Vec3 {
	if boneIndex < 0 || boneIndex >= len(boneTransforms) {
		return position
	}
	return transformMDLPointByBone(position, boneIndex, boneTransforms, 0)
}

func transformMDLPointByBone(point importcommon.Vec3, boneIndex int, boneTransforms []mdlBoneTransform, depth int) importcommon.Vec3 {
	if boneIndex < 0 || boneIndex >= len(boneTransforms) || depth > len(boneTransforms) {
		return point
	}
	bone := boneTransforms[boneIndex]
	rotated := rotateMDLPointXYZ(point, bone.Rotation)
	transformed := importcommon.Vec3{
		X: rotated.X + bone.Position.X,
		Y: rotated.Y + bone.Position.Y,
		Z: rotated.Z + bone.Position.Z,
	}
	if bone.Parent >= 0 && bone.Parent < len(boneTransforms) && bone.Parent != boneIndex {
		return transformMDLPointByBone(transformed, bone.Parent, boneTransforms, depth+1)
	}
	return transformed
}

func rotateMDLPointXYZ(point importcommon.Vec3, rotation importcommon.Vec3) importcommon.Vec3 {
	cx, sx := float32(math.Cos(float64(rotation.X))), float32(math.Sin(float64(rotation.X)))
	cy, sy := float32(math.Cos(float64(rotation.Y))), float32(math.Sin(float64(rotation.Y)))
	cz, sz := float32(math.Cos(float64(rotation.Z))), float32(math.Sin(float64(rotation.Z)))

	out := importcommon.Vec3{X: point.X, Y: point.Y*cx - point.Z*sx, Z: point.Y*sx + point.Z*cx}
	out = importcommon.Vec3{X: out.X*cy + out.Z*sy, Y: out.Y, Z: -out.X*sy + out.Z*cy}
	out = importcommon.Vec3{X: out.X*cz - out.Y*sz, Y: out.X*sz + out.Y*cz, Z: out.Z}
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

func decodeMDLModelTriangles(data []byte, model decodedMDLModel, skinData []byte, skinInfo MDLInfo, textures []MDLTexturePixels, skinFamily int) []MDLTriangle {
	out := make([]MDLTriangle, 0)
	for _, mesh := range model.meshes {
		textureIndex := mdlTextureIndexForSkinRef(skinData, skinInfo, mesh.skinRef, skinFamily)
		out = append(out, decodeMDLTriangleCommands(data, mesh.triangleCommandIndex, textureIndex, model.vertices, textures)...)
	}
	return out
}

func decodeMDLTriangleCommands(data []byte, offset int, textureIndex int, vertices []decodedMDLVertex, textures []MDLTexturePixels) []MDLTriangle {
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

func mdlTriangleVertex(vertexIndex int, normalIndex int, s int, t int, textureIndex int, vertices []decodedMDLVertex, textures []MDLTexturePixels) MDLTriangleVertex {
	out := MDLTriangleVertex{
		NormalIndex: normalIndex,
		Texel:       [2]int{s, t},
		BoneIndex:   -1,
	}
	if vertexIndex >= 0 && vertexIndex < len(vertices) {
		out.Position = vertices[vertexIndex].Position
		out.LocalPosition = vertices[vertexIndex].LocalPosition
		out.BoneIndex = vertices[vertexIndex].BoneIndex
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

func mdlTextureIndexForSkinRef(data []byte, info MDLInfo, skinRef int, skinFamily int) int {
	skinIndex := int(readInt32(data, 200))
	if skinFamily < 0 || skinFamily >= info.SkinFamilyCount {
		skinFamily = 0
	}
	index := skinFamily*info.SkinRefCount + skinRef
	if skinRef >= 0 && info.SkinRefCount > 0 && skinRef < info.SkinRefCount && skinIndex >= 0 && skinIndex+2 <= len(data) && index < (len(data)-skinIndex)/2 {
		textureIndex := int(readInt16(data, skinIndex+index*2))
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
