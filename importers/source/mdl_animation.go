// Package source imports animation-only data from Source-engine MDL files.
// It deliberately does not import meshes, materials, or runtime retargeting.
package source

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	mdlIdent   = "IDST"
	mdlVersion = 48

	studioAnimRawPos  = 0x01
	studioAnimRawRot  = 0x02
	studioAnimAnimPos = 0x04
	studioAnimAnimRot = 0x08
	studioAnimDelta   = 0x10
	studioAnimRawRot2 = 0x20

	studioSequenceLooping    = 0x01
	studioSequenceDelta      = 0x04
	studioSequencePost       = 0x10
	studioSequenceCycle      = 0x80
	studioSequenceRealtime   = 0x100
	studioSequenceLocal      = 0x200
	studioSequenceWorld      = 0x4000
	studioBoneFixedAlignment = 0x00100000
	metersPerSourceUnit      = 0.0254
)

type Bone struct {
	Name      string
	Parent    int
	Bind      transform
	Euler     mgl32.Vec3
	PosScl    mgl32.Vec3
	RotScl    mgl32.Vec3
	Alignment mgl32.Quat
	Flags     int
}

type Clip struct {
	Name     string
	FPS      float32
	Loop     bool
	Frames   [][]transform // Global, in Gekko coordinates.
	Animated []bool
}

type AnimationSource struct {
	Bones []Bone
	Clips []Clip
}

// SequenceInfo describes a Source sequence before it is imported into an
// authored asset. Unavailable sequences are listed explicitly for editor use.
type SequenceInfo struct {
	Name        string
	FPS         float32
	Frames      int
	Duration    float32
	Loop        bool
	Previewable bool
	Reason      string
}

type BakeOptions struct {
	// SequenceNames selects Source sequence labels. An empty list imports all
	// local, non-blended sequences.
	SequenceNames []string
	// ClipPrefix is prepended to stable generated IDs. It defaults to source_.
	ClipPrefix string
	// LockRootMotion preserves controller ownership of the actor transform.
	LockRootMotion bool
}

type transform struct {
	Position mgl32.Vec3
	Rotation mgl32.Quat
}

type mdlAnimationDesc struct {
	offset         int
	name           string
	fps            float32
	frames         int
	flags          int
	animBlock      int
	animIndex      int
	ikRules        int
	localHierarchy int
	sectionIndex   int
	sectionFrames  int
}

type mdlSequence struct {
	name       string
	flags      int
	anims      []int
	weights    []float32
	autoLayers int
	ikLocks    int
}

type mdlAnimRecord struct {
	bone  int
	flags byte
	data  int
}

// LoadMDLAnimations decodes Source MDL v48 in-file animation blocks. Source
// external animation blocks are rejected explicitly because their companion
// format is separate from the model and cannot safely fall back to bind pose.
func LoadMDLAnimations(path string, sequenceNames []string) (*AnimationSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseMDLAnimations(data, sequenceNames)
}

// ListMDLSequences lists every donor sequence without decoding frames or
// changing an authored asset.
func ListMDLSequences(path string) ([]SequenceInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ListMDLAnimationSequences(data)
}

func ListMDLAnimationSequences(data []byte) ([]SequenceInfo, error) {
	r := mdlReader{data: data}
	if len(data) < 196 {
		return nil, fmt.Errorf("source mdl too small: %d bytes", len(data))
	}
	if string(data[:4]) != mdlIdent {
		return nil, fmt.Errorf("unsupported source mdl ident %q", string(data[:4]))
	}
	version, err := r.i32(4)
	if err != nil {
		return nil, err
	}
	if version != mdlVersion {
		return nil, fmt.Errorf("unsupported source mdl version %d", version)
	}
	boneCount, err := r.i32(156)
	if err != nil {
		return nil, err
	}
	animCount, err := r.i32(180)
	if err != nil {
		return nil, err
	}
	animIndex, err := r.i32(184)
	if err != nil {
		return nil, err
	}
	anims, err := parseAnimationDescs(r, animIndex, animCount)
	if err != nil {
		return nil, err
	}
	sequenceCount, err := r.i32(188)
	if err != nil {
		return nil, err
	}
	sequenceIndex, err := r.i32(192)
	if err != nil {
		return nil, err
	}
	sequences, err := parseSequences(r, sequenceIndex, sequenceCount, boneCount)
	if err != nil {
		return nil, err
	}

	infos := make([]SequenceInfo, 0, len(sequences))
	for _, sequence := range sequences {
		info := SequenceInfo{Name: sequence.name, Loop: sequence.flags&studioSequenceLooping != 0}
		if len(sequence.anims) != 1 {
			info.Reason = fmt.Sprintf("%d blend animations unsupported", len(sequence.anims))
			infos = append(infos, info)
			continue
		}
		anim := sequence.anims[0]
		if anim < 0 || anim >= len(anims) {
			info.Reason = "animation reference is invalid"
			infos = append(infos, info)
			continue
		}
		desc := anims[anim]
		info.FPS = desc.fps
		info.Frames = desc.frames
		if reason := unsupportedSequenceReason(sequence, desc); reason != "" {
			info.Reason = reason
			infos = append(infos, info)
			continue
		}
		switch {
		case desc.frames <= 0 || desc.frames > 8192:
			info.Reason = "frame count is invalid"
		case desc.fps <= 0 || desc.fps > 240:
			info.Reason = "FPS is invalid"
		case desc.animBlock != 0:
			info.Reason = "external animation block unsupported"
		default:
			info.Duration = sourceClipDuration(desc.frames, desc.fps)
			info.Previewable = true
		}
		infos = append(infos, info)
	}
	return infos, nil
}

func ParseMDLAnimations(data []byte, sequenceNames []string) (*AnimationSource, error) {
	r := mdlReader{data: data}
	if len(data) < 196 {
		return nil, fmt.Errorf("source mdl too small: %d bytes", len(data))
	}
	if string(data[:4]) != mdlIdent {
		return nil, fmt.Errorf("unsupported source mdl ident %q", string(data[:4]))
	}
	version, err := r.i32(4)
	if err != nil {
		return nil, err
	}
	if version != mdlVersion {
		return nil, fmt.Errorf("unsupported source mdl version %d", version)
	}

	boneCount, err := r.i32(156)
	if err != nil {
		return nil, err
	}
	boneIndex, err := r.i32(160)
	if err != nil {
		return nil, err
	}
	bones, err := parseBones(r, boneIndex, boneCount)
	if err != nil {
		return nil, err
	}
	animCount, err := r.i32(180)
	if err != nil {
		return nil, err
	}
	animIndex, err := r.i32(184)
	if err != nil {
		return nil, err
	}
	anims, err := parseAnimationDescs(r, animIndex, animCount)
	if err != nil {
		return nil, err
	}
	sequenceCount, err := r.i32(188)
	if err != nil {
		return nil, err
	}
	sequenceIndex, err := r.i32(192)
	if err != nil {
		return nil, err
	}
	sequences, err := parseSequences(r, sequenceIndex, sequenceCount, len(bones))
	if err != nil {
		return nil, err
	}

	wanted := make(map[string]struct{}, len(sequenceNames))
	for _, name := range sequenceNames {
		if key := normalizedName(name); key != "" {
			wanted[key] = struct{}{}
		}
	}
	source := &AnimationSource{Bones: bones}
	for _, sequence := range sequences {
		if len(wanted) > 0 {
			if _, ok := wanted[normalizedName(sequence.name)]; !ok {
				continue
			}
		}
		if len(sequence.anims) != 1 {
			if len(wanted) == 0 {
				continue
			}
			return nil, fmt.Errorf("source sequence %q has %d blend animations; blended sequences are unsupported", sequence.name, len(sequence.anims))
		}
		animIndex := sequence.anims[0]
		if animIndex < 0 || animIndex >= len(anims) {
			return nil, fmt.Errorf("source sequence %q references animation %d outside %d local animations", sequence.name, animIndex, len(anims))
		}
		if reason := unsupportedSequenceReason(sequence, anims[animIndex]); reason != "" {
			if len(wanted) == 0 {
				continue
			}
			return nil, fmt.Errorf("source sequence %q: %s", sequence.name, reason)
		}
		clip, err := decodeClip(r, bones, anims[animIndex], sequence)
		if err != nil {
			return nil, err
		}
		source.Clips = append(source.Clips, clip)
	}
	if len(wanted) > 0 && len(source.Clips) != len(wanted) {
		found := make(map[string]struct{}, len(source.Clips))
		for _, clip := range source.Clips {
			found[normalizedName(clip.Name)] = struct{}{}
		}
		missing := make([]string, 0, len(wanted))
		for name := range wanted {
			if _, ok := found[name]; !ok {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("source mdl does not contain requested sequences: %s", strings.Join(missing, ", "))
	}
	return source, nil
}

// AppendAnimations bakes selected Source clips into an existing authored rig.
// Every animated target bone must resolve through same-named source bones and
// matching mapped parents. Source-only helper leaves remain in bind pose; this
// prevents a friendly humanoid label from being mistaken for a compatible rig.
func AppendAnimations(asset *content.AssetDef, source *AnimationSource, opts BakeOptions) ([]string, error) {
	if asset == nil {
		return nil, fmt.Errorf("target asset is nil")
	}
	if source == nil || len(source.Bones) == 0 || len(source.Clips) == 0 {
		return nil, fmt.Errorf("source contains no animations")
	}
	if !containsString(asset.Tags, content.AssetTagSkeletonRestBasis) {
		return nil, fmt.Errorf("target asset %q has no explicit skeleton rest basis; regenerate it with the current model importer", asset.Name)
	}
	bindings, err := targetBindings(asset, source.Bones)
	if err != nil {
		return nil, err
	}
	prefix := safeID(opts.ClipPrefix)
	if prefix == "" {
		prefix = "source"
	}
	if !strings.HasSuffix(prefix, "_") {
		prefix += "_"
	}

	selected, err := selectedSourceClips(source.Clips, opts.SequenceNames)
	if err != nil {
		return nil, err
	}
	generated := make([]content.AssetAnimationClipDef, 0, len(selected))
	ids := make([]string, 0, len(selected))
	for _, sourceClip := range selected {
		clip, err := bakeClip(source.Bones, bindings, sourceClip, prefix, opts.LockRootMotion)
		if err != nil {
			return nil, err
		}
		generated = append(generated, clip)
		ids = append(ids, clip.ID)
	}

	generatedByID := make(map[string]content.AssetAnimationClipDef, len(generated))
	for _, clip := range generated {
		generatedByID[clip.ID] = clip
	}
	merged := make([]content.AssetAnimationClipDef, 0, len(asset.AnimationClips)+len(generated))
	for _, clip := range asset.AnimationClips {
		if _, replace := generatedByID[clip.ID]; !replace {
			merged = append(merged, clip)
		}
	}
	asset.AnimationClips = append(merged, generated...)
	return ids, nil
}

func selectedSourceClips(clips []Clip, names []string) ([]Clip, error) {
	if len(names) == 0 {
		return clips, nil
	}
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name = normalizedName(name); name != "" {
			wanted[name] = struct{}{}
		}
	}
	selected := make([]Clip, 0, len(wanted))
	for _, clip := range clips {
		if _, ok := wanted[normalizedName(clip.Name)]; ok {
			selected = append(selected, clip)
		}
	}
	if len(selected) != len(wanted) {
		return nil, fmt.Errorf("source does not contain every selected sequence")
	}
	return selected, nil
}

func parseBones(r mdlReader, offset, count int) ([]Bone, error) {
	if count <= 0 || count > 128 {
		return nil, fmt.Errorf("invalid source bone count %d", count)
	}
	const boneSize = 216
	if err := r.rangeOK(offset, count*boneSize); err != nil {
		return nil, err
	}
	bones := make([]Bone, count)
	for i := range bones {
		base := offset + i*boneSize
		nameOffset, err := r.i32(base)
		if err != nil {
			return nil, err
		}
		name, err := r.relativeString(base, nameOffset)
		if err != nil {
			return nil, fmt.Errorf("source bone %d: %w", i, err)
		}
		parent, err := r.i32(base + 4)
		if err != nil {
			return nil, err
		}
		if parent >= count || parent == i {
			return nil, fmt.Errorf("source bone %q has invalid parent %d", name, parent)
		}
		position, err := r.vec3(base + 32)
		if err != nil {
			return nil, err
		}
		bindRotation, err := r.quat(base + 44)
		if err != nil {
			return nil, err
		}
		rotation, err := r.vec3(base + 60)
		if err != nil {
			return nil, err
		}
		posScale, err := r.vec3(base + 72)
		if err != nil {
			return nil, err
		}
		rotScale, err := r.vec3(base + 84)
		if err != nil {
			return nil, err
		}
		alignment, err := r.quat(base + 144)
		if err != nil {
			return nil, err
		}
		flags, err := r.i32(base + 160)
		if err != nil {
			return nil, err
		}
		bones[i] = Bone{
			Name:      name,
			Parent:    parent,
			Bind:      transform{Position: position, Rotation: bindRotation},
			Euler:     rotation,
			PosScl:    posScale,
			RotScl:    rotScale,
			Alignment: alignment,
			Flags:     flags,
		}
	}
	return bones, nil
}

func parseAnimationDescs(r mdlReader, offset, count int) ([]mdlAnimationDesc, error) {
	if count < 0 || count > 4096 {
		return nil, fmt.Errorf("invalid source animation count %d", count)
	}
	const animationDescSize = 100
	if err := r.rangeOK(offset, count*animationDescSize); err != nil {
		return nil, err
	}
	out := make([]mdlAnimationDesc, count)
	for i := range out {
		base := offset + i*animationDescSize
		nameOffset, err := r.i32(base + 4)
		if err != nil {
			return nil, err
		}
		name, err := r.relativeString(base, nameOffset)
		if err != nil {
			return nil, fmt.Errorf("source animation %d: %w", i, err)
		}
		fps, err := r.f32(base + 8)
		if err != nil {
			return nil, err
		}
		flags, err := r.i32(base + 12)
		if err != nil {
			return nil, err
		}
		frames, err := r.i32(base + 16)
		if err != nil {
			return nil, err
		}
		animBlock, err := r.i32(base + 52)
		if err != nil {
			return nil, err
		}
		animIndex, err := r.i32(base + 56)
		if err != nil {
			return nil, err
		}
		ikRules, err := r.i32(base + 60)
		if err != nil {
			return nil, err
		}
		localHierarchy, err := r.i32(base + 72)
		if err != nil {
			return nil, err
		}
		sectionIndex, err := r.i32(base + 80)
		if err != nil {
			return nil, err
		}
		sectionFrames, err := r.i32(base + 84)
		if err != nil {
			return nil, err
		}
		out[i] = mdlAnimationDesc{offset: base, name: name, fps: fps, frames: frames, flags: flags, animBlock: animBlock, animIndex: animIndex, ikRules: ikRules, localHierarchy: localHierarchy, sectionIndex: sectionIndex, sectionFrames: sectionFrames}
	}
	return out, nil
}

func parseSequences(r mdlReader, offset, count, boneCount int) ([]mdlSequence, error) {
	if count < 0 || count > 4096 {
		return nil, fmt.Errorf("invalid source sequence count %d", count)
	}
	if boneCount <= 0 || boneCount > 128 {
		return nil, fmt.Errorf("invalid source bone count %d", boneCount)
	}
	const sequenceSize = 212
	if err := r.rangeOK(offset, count*sequenceSize); err != nil {
		return nil, err
	}
	out := make([]mdlSequence, count)
	for i := range out {
		base := offset + i*sequenceSize
		nameOffset, err := r.i32(base + 4)
		if err != nil {
			return nil, err
		}
		name, err := r.relativeString(base, nameOffset)
		if err != nil {
			return nil, fmt.Errorf("source sequence %d: %w", i, err)
		}
		flags, err := r.i32(base + 12)
		if err != nil {
			return nil, err
		}
		blendCount, err := r.i32(base + 56)
		if err != nil {
			return nil, err
		}
		blendOffset, err := r.i32(base + 60)
		if err != nil {
			return nil, err
		}
		if blendCount <= 0 || blendCount > 4 {
			return nil, fmt.Errorf("source sequence %q has invalid blend count %d", name, blendCount)
		}
		if err := r.rangeOK(base+blendOffset, blendCount*2); err != nil {
			return nil, fmt.Errorf("source sequence %q: %w", name, err)
		}
		ids := make([]int, blendCount)
		for j := range ids {
			value, err := r.i16(base + blendOffset + j*2)
			if err != nil {
				return nil, err
			}
			ids[j] = value
		}
		autoLayers, err := r.i32(base + 148)
		if err != nil {
			return nil, err
		}
		weightOffset, err := r.i32(base + 156)
		if err != nil {
			return nil, err
		}
		if weightOffset <= 0 {
			return nil, fmt.Errorf("source sequence %q has no bone weight list", name)
		}
		if err := r.rangeOK(base+weightOffset, boneCount*4); err != nil {
			return nil, fmt.Errorf("source sequence %q bone weights: %w", name, err)
		}
		weights := make([]float32, boneCount)
		for bone := range weights {
			weight, err := r.f32(base + weightOffset + bone*4)
			if err != nil {
				return nil, err
			}
			if weight < 0 || weight > 1 || math.IsNaN(float64(weight)) {
				return nil, fmt.Errorf("source sequence %q bone %d has invalid weight %g", name, bone, weight)
			}
			weights[bone] = weight
		}
		ikLocks, err := r.i32(base + 164)
		if err != nil {
			return nil, err
		}
		out[i] = mdlSequence{name: strings.TrimPrefix(name, "@"), flags: flags, anims: ids, weights: weights, autoLayers: autoLayers, ikLocks: ikLocks}
	}
	return out, nil
}

func unsupportedSequenceReason(sequence mdlSequence, desc mdlAnimationDesc) string {
	switch {
	case sequence.flags&studioSequenceDelta != 0 || desc.flags&studioSequenceDelta != 0:
		return "additive/delta sequence unsupported"
	case sequence.flags&studioSequencePost != 0:
		return "post-composed sequence unsupported"
	case sequence.flags&studioSequenceCycle != 0:
		return "cycle-pose sequence unsupported"
	case sequence.flags&studioSequenceRealtime != 0:
		return "realtime sequence unsupported"
	case sequence.flags&studioSequenceLocal != 0:
		return "local-context sequence unsupported"
	case sequence.flags&studioSequenceWorld != 0:
		return "world-space sequence unsupported"
	case sequence.autoLayers > 0:
		return fmt.Sprintf("%d automatic sequence layers unsupported", sequence.autoLayers)
	case sequence.ikLocks > 0 || desc.ikRules > 0:
		return "sequence IK unsupported"
	case desc.localHierarchy > 0:
		return "local hierarchy animation unsupported"
	default:
		return ""
	}
}

func decodeClip(r mdlReader, bones []Bone, desc mdlAnimationDesc, sequence mdlSequence) (Clip, error) {
	if desc.frames <= 0 || desc.frames > 8192 {
		return Clip{}, fmt.Errorf("source sequence %q has invalid frame count %d", sequence.name, desc.frames)
	}
	if desc.fps <= 0 || desc.fps > 240 {
		return Clip{}, fmt.Errorf("source sequence %q has invalid fps %.2f", sequence.name, desc.fps)
	}
	if desc.animBlock != 0 {
		return Clip{}, fmt.Errorf("source sequence %q uses external animation block %d", sequence.name, desc.animBlock)
	}
	if len(sequence.weights) != len(bones) {
		return Clip{}, fmt.Errorf("source sequence %q has %d bone weights, want %d", sequence.name, len(sequence.weights), len(bones))
	}
	clip := Clip{Name: sequence.name, FPS: desc.fps, Loop: sequence.flags&studioSequenceLooping != 0, Frames: make([][]transform, desc.frames), Animated: make([]bool, len(bones))}
	for frame := 0; frame < desc.frames; frame++ {
		animOffset, localFrame, err := animationFrameOffset(r, desc, frame)
		if err != nil {
			return Clip{}, fmt.Errorf("source sequence %q frame %d: %w", sequence.name, frame, err)
		}
		records, err := parseAnimationRecords(r, animOffset, len(bones))
		if err != nil {
			return Clip{}, fmt.Errorf("source sequence %q frame %d: %w", sequence.name, frame, err)
		}
		locals := make([]transform, len(bones))
		for i, bone := range bones {
			locals[i] = bone.Bind
		}
		for _, record := range records {
			weight := sequence.weights[record.bone]
			if weight <= 0 {
				continue
			}
			if record.flags&studioAnimDelta != 0 {
				return Clip{}, fmt.Errorf("source sequence %q bone %q contains delta animation data outside a supported delta sequence", sequence.name, bones[record.bone].Name)
			}
			local, err := decodeAnimationRecord(r, bones[record.bone], record, localFrame)
			if err != nil {
				return Clip{}, fmt.Errorf("source sequence %q frame %d bone %q: %w", sequence.name, frame, bones[record.bone].Name, err)
			}
			locals[record.bone] = blendTransform(bones[record.bone].Bind, local, weight)
			clip.Animated[record.bone] = true
		}
		clip.Frames[frame] = globalTransforms(sourceToGekkoTransforms(locals), bones)
	}
	return clip, nil
}

func animationFrameOffset(r mdlReader, desc mdlAnimationDesc, frame int) (int, int, error) {
	if desc.sectionFrames <= 0 {
		if desc.animIndex <= 0 {
			return 0, 0, fmt.Errorf("missing in-file animation index")
		}
		return desc.offset + desc.animIndex, frame, r.rangeOK(desc.offset+desc.animIndex, 4)
	}
	if desc.sectionIndex <= 0 {
		return 0, 0, fmt.Errorf("sectioned animation has no section index")
	}
	section := frame / desc.sectionFrames
	localFrame := frame % desc.sectionFrames
	if desc.frames > desc.sectionFrames && frame == desc.frames-1 {
		section = desc.frames/desc.sectionFrames + 1
		localFrame = 0
	}
	sectionOffset := desc.offset + desc.sectionIndex + section*8
	if err := r.rangeOK(sectionOffset, 8); err != nil {
		return 0, 0, err
	}
	block, err := r.i32(sectionOffset)
	if err != nil {
		return 0, 0, err
	}
	if block != 0 {
		return 0, 0, fmt.Errorf("section %d uses external animation block %d", section, block)
	}
	index, err := r.i32(sectionOffset + 4)
	if err != nil {
		return 0, 0, err
	}
	if index <= 0 {
		return 0, 0, fmt.Errorf("section %d has no animation data", section)
	}
	return desc.offset + index, localFrame, r.rangeOK(desc.offset+index, 4)
}

func parseAnimationRecords(r mdlReader, offset, boneCount int) ([]mdlAnimRecord, error) {
	records := make([]mdlAnimRecord, 0, boneCount)
	seen := make(map[int]struct{}, boneCount)
	for steps, current := 0, offset; ; steps++ {
		if steps >= boneCount {
			return nil, fmt.Errorf("animation record chain exceeds %d bones", boneCount)
		}
		if err := r.rangeOK(current, 4); err != nil {
			return nil, err
		}
		bone := int(r.data[current])
		if bone < 0 || bone >= boneCount {
			return nil, fmt.Errorf("animation record references bone %d outside %d bones", bone, boneCount)
		}
		if _, duplicate := seen[bone]; duplicate {
			return nil, fmt.Errorf("animation record repeats bone %d", bone)
		}
		seen[bone] = struct{}{}
		next, err := r.i16(current + 2)
		if err != nil {
			return nil, err
		}
		records = append(records, mdlAnimRecord{bone: bone, flags: r.data[current+1], data: current + 4})
		if next == 0 {
			return records, nil
		}
		if next < 4 {
			return nil, fmt.Errorf("invalid animation record offset %d", next)
		}
		current += next
	}
}

func decodeAnimationRecord(r mdlReader, bone Bone, record mdlAnimRecord, frame int) (transform, error) {
	delta := record.flags&studioAnimDelta != 0
	value := bone.Bind
	if delta {
		value = transform{Rotation: mgl32.QuatIdent()}
	}
	flags := record.flags
	data := record.data
	if flags&studioAnimAnimRot != 0 {
		rotation, err := r.animationEuler(data, frame, bone.RotScl)
		if err != nil {
			return transform{}, err
		}
		if !delta {
			rotation = rotation.Add(bone.Euler)
		}
		value.Rotation = eulerXYZQuat(rotation)
		if !delta && bone.Flags&studioBoneFixedAlignment != 0 {
			value.Rotation = alignQuat(bone.Alignment, value.Rotation)
		}
		data += 6
	}
	if flags&studioAnimAnimPos != 0 {
		position, err := r.animationVec(data, frame, bone.PosScl)
		if err != nil {
			return transform{}, err
		}
		if !delta {
			position = bone.Bind.Position.Add(position)
		}
		value.Position = position
		data += 6
	}
	if flags&studioAnimRawRot != 0 {
		rotation, err := r.quat48(data)
		if err != nil {
			return transform{}, err
		}
		value.Rotation = rotation
		data += 6
	}
	if flags&studioAnimRawRot2 != 0 {
		rotation, err := r.quat64(data)
		if err != nil {
			return transform{}, err
		}
		value.Rotation = rotation
		data += 8
	}
	if flags&studioAnimRawPos != 0 {
		position, err := r.vec48(data)
		if err != nil {
			return transform{}, err
		}
		value.Position = position
	}
	return value, nil
}

func blendTransform(base, value transform, weight float32) transform {
	if weight >= 1 {
		return value
	}
	return transform{
		Position: base.Position.Mul(1 - weight).Add(value.Position.Mul(weight)),
		Rotation: mgl32.QuatSlerp(base.Rotation, value.Rotation, weight).Normalize(),
	}
}

func alignQuat(reference, value mgl32.Quat) mgl32.Quat {
	if reference.W*value.W+reference.V.Dot(value.V) < 0 {
		return mgl32.Quat{V: value.V.Mul(-1), W: -value.W}
	}
	return value
}

func sourceToGekkoTransforms(locals []transform) []transform {
	out := make([]transform, len(locals))
	for i, local := range locals {
		out[i] = transform{Position: sourceToGekkoPosition(local.Position), Rotation: sourceToGekkoRotation(local.Rotation)}
	}
	return out
}

func globalTransforms(locals []transform, bones []Bone) []transform {
	out := make([]transform, len(locals))
	for i, local := range locals {
		value := local
		parent := bones[i].Parent
		if parent >= 0 && parent < len(out) {
			parentValue := out[parent]
			value.Position = parentValue.Position.Add(parentValue.Rotation.Rotate(local.Position))
			value.Rotation = parentValue.Rotation.Mul(local.Rotation).Normalize()
		}
		out[i] = value
	}
	return out
}

type targetBinding struct {
	TargetID string
	Parent   int
	Rest     transform
}

func targetBindings(asset *content.AssetDef, bones []Bone) ([]targetBinding, error) {
	if asset.Skeleton == nil || len(asset.Skeleton.Bones) == 0 {
		return nil, fmt.Errorf("target asset %q has no skeleton", asset.Name)
	}
	parts := make(map[string]content.AssetPartDef, len(asset.Parts))
	for _, part := range asset.Parts {
		parts[part.ID] = part
	}
	byName := make(map[string]content.AssetBoneDef, len(asset.Skeleton.Bones))
	for _, bone := range asset.Skeleton.Bones {
		key := normalizedName(bone.Name)
		if key != "" {
			byName[key] = bone
		}
	}
	bindings := make([]targetBinding, len(bones))
	for i, sourceBone := range bones {
		target, ok := byName[normalizedName(sourceBone.Name)]
		if !ok {
			continue
		}
		if _, ok := parts[target.ID]; !ok {
			return nil, fmt.Errorf("target skeleton bone %q has no matching part %q", target.Name, target.ID)
		}
		bindings[i] = targetBinding{TargetID: target.ID, Parent: sourceBone.Parent, Rest: assetTransform(target.Transform)}
	}
	for i, sourceBone := range bones {
		binding := bindings[i]
		if binding.TargetID == "" {
			continue
		}
		if sourceBone.Parent < 0 {
			continue
		}
		parent := bindings[sourceBone.Parent]
		if parent.TargetID == "" {
			continue
		}
		part := parts[binding.TargetID]
		if part.ParentID != parent.TargetID {
			return nil, fmt.Errorf("target bone %q parent %q does not match donor parent %q", sourceBone.Name, part.ParentID, bones[sourceBone.Parent].Name)
		}
		target := byName[normalizedName(sourceBone.Name)]
		if target.ParentID != parent.TargetID {
			return nil, fmt.Errorf("target rest bone %q parent %q does not match donor parent %q", sourceBone.Name, target.ParentID, bones[sourceBone.Parent].Name)
		}
	}
	return bindings, nil
}

func bakeClip(bones []Bone, bindings []targetBinding, sourceClip Clip, prefix string, lockRoot bool) (content.AssetAnimationClipDef, error) {
	if len(sourceClip.Frames) == 0 || len(sourceClip.Frames[0]) != len(bones) {
		return content.AssetAnimationClipDef{}, fmt.Errorf("source clip %q has no complete frames", sourceClip.Name)
	}
	if len(sourceClip.Animated) != len(bones) {
		return content.AssetAnimationClipDef{}, fmt.Errorf("source clip %q has %d animation flags, want %d", sourceClip.Name, len(sourceClip.Animated), len(bones))
	}
	for i, binding := range bindings {
		if binding.TargetID == "" {
			continue
		}
		for parent := bones[i].Parent; parent >= 0; parent = bones[parent].Parent {
			if bindings[parent].TargetID == "" {
				return content.AssetAnimationClipDef{}, fmt.Errorf("source clip %q animated bone %q has unmapped ancestor %q", sourceClip.Name, bones[i].Name, bones[parent].Name)
			}
		}
	}
	sourceBindLocals := make([]transform, len(bones))
	for i, bone := range bones {
		sourceBindLocals[i] = bone.Bind
	}
	sourceBindLocals = sourceToGekkoTransforms(sourceBindLocals)
	firstSourceLocals := localTransforms(sourceClip.Frames[0], bones)
	targetRetargetLocals := retargetRestTransforms(sourceBindLocals, bindings)
	targetRestGlobals := globalBindingTransforms(bindingRestTransforms(bindings), bindings)
	root := rootBoneIndex(bones)
	frames := make([][]transform, len(sourceClip.Frames))
	for frameIndex, sourceFrame := range sourceClip.Frames {
		if len(sourceFrame) != len(bones) {
			return content.AssetAnimationClipDef{}, fmt.Errorf("source clip %q frame %d has %d bones, want %d", sourceClip.Name, frameIndex, len(sourceFrame), len(bones))
		}
		sourceLocals := localTransforms(sourceFrame, bones)
		targetLocals := make([]transform, len(bones))
		for i, binding := range bindings {
			if binding.TargetID == "" {
				continue
			}
			positionDelta := sourceLocals[i].Position.Sub(sourceBindLocals[i].Position)
			if lockRoot && i == root {
				positionDelta = firstSourceLocals[i].Position.Sub(sourceBindLocals[i].Position)
			}
			rotationDelta := sourceBindLocals[i].Rotation.Inverse().Mul(sourceLocals[i].Rotation).Normalize()
			targetLocals[i] = transform{
				Position: binding.Rest.Position.Add(positionDelta),
				Rotation: targetRetargetLocals[i].Rotation.Mul(rotationDelta).Normalize(),
			}
		}
		targetGlobals := globalBindingTransforms(targetLocals, bindings)
		globalDeltas := make([]mgl32.Quat, len(bones))
		for i, binding := range bindings {
			if binding.TargetID != "" {
				globalDeltas[i] = targetGlobals[i].Rotation.Mul(targetRestGlobals[i].Rotation.Inverse()).Normalize()
			}
		}
		frame := make([]transform, len(bones))
		for i, binding := range bindings {
			if binding.TargetID == "" {
				continue
			}
			frame[i] = transform{Position: targetGlobals[i].Position, Rotation: globalDeltas[i]}
			if parent := bones[i].Parent; parent >= 0 && bindings[parent].TargetID != "" {
				frame[i].Position = globalDeltas[parent].Inverse().Rotate(targetGlobals[i].Position.Sub(targetGlobals[parent].Position))
				frame[i].Rotation = globalDeltas[parent].Inverse().Mul(globalDeltas[i]).Normalize()
			}
		}
		frames[frameIndex] = frame
	}

	tracks := make([]content.AssetAnimationTrackDef, 0, len(bones))
	for i, binding := range bindings {
		if binding.TargetID == "" {
			continue
		}
		positionKeys := make([]content.AssetVec3KeyDef, 0, len(sourceClip.Frames))
		rotationKeys := make([]content.AssetQuatKeyDef, 0, len(sourceClip.Frames))
		for frameIndex := range frames {
			local := frames[frameIndex][i]
			t := float32(frameIndex) / sourceClip.FPS
			positionKeys = append(positionKeys, content.AssetVec3KeyDef{Time: t, Value: content.Vec3{local.Position.X(), local.Position.Y(), local.Position.Z()}})
			rotationKeys = append(rotationKeys, content.AssetQuatKeyDef{Time: t, Value: content.Quat{local.Rotation.V.X(), local.Rotation.V.Y(), local.Rotation.V.Z(), local.Rotation.W}})
		}
		tracks = append(tracks, content.AssetAnimationTrackDef{TargetID: binding.TargetID, PositionKeys: positionKeys, RotationKeys: rotationKeys, ScaleKeys: []content.AssetVec3KeyDef{{Time: 0, Value: content.Vec3{1, 1, 1}}}})
	}
	duration := sourceClipDuration(len(sourceClip.Frames), sourceClip.FPS)
	if len(tracks) == 0 {
		return content.AssetAnimationClipDef{}, fmt.Errorf("source clip %q has no mapped tracks", sourceClip.Name)
	}
	id := prefix + safeID(sourceClip.Name)
	return content.AssetAnimationClipDef{ID: id, Name: sourceClip.Name, FPS: sourceClip.FPS, Duration: duration, Loop: sourceClip.Loop, Tracks: tracks, Tags: []string{"source:source_mdl", "source_asset:mdl", "generated:retargeted_sequence_clip"}}, nil
}

func sourceClipDuration(frames int, fps float32) float32 {
	if frames <= 1 || fps <= 0 {
		return 0
	}
	return float32(frames-1) / fps
}

func localTransforms(globals []transform, bones []Bone) []transform {
	locals := make([]transform, len(globals))
	for i, global := range globals {
		local := global
		if parent := bones[i].Parent; parent >= 0 && parent < len(globals) {
			parentGlobal := globals[parent]
			local.Position = parentGlobal.Rotation.Inverse().Rotate(global.Position.Sub(parentGlobal.Position))
			local.Rotation = parentGlobal.Rotation.Inverse().Mul(global.Rotation).Normalize()
		}
		locals[i] = local
	}
	return locals
}

func bindingRestTransforms(bindings []targetBinding) []transform {
	out := make([]transform, len(bindings))
	for i, binding := range bindings {
		out[i] = binding.Rest
	}
	return out
}

// retargetRestTransforms aligns corresponding bone segments before animation
// deltas are applied. This preserves an authored target bind pose while
// allowing donors with a different limb rest pose (for example T versus A)
// to drive the same rigid hierarchy.
func retargetRestTransforms(sourceRest []transform, bindings []targetBinding) []transform {
	out := bindingRestTransforms(bindings)
	children := make([][]int, len(bindings))
	for child, binding := range bindings {
		if binding.TargetID != "" && binding.Parent >= 0 && bindings[binding.Parent].TargetID != "" {
			children[binding.Parent] = append(children[binding.Parent], child)
		}
	}
	for bone, childIndices := range children {
		if bindings[bone].TargetID == "" || len(childIndices) != 1 {
			continue
		}
		child := childIndices[0]
		sourceDirection := sourceRest[bone].Rotation.Rotate(sourceRest[child].Position)
		targetDirection := out[bone].Rotation.Rotate(out[child].Position)
		if sourceDirection.LenSqr() <= 1e-8 || targetDirection.LenSqr() <= 1e-8 {
			continue
		}
		correction := mgl32.QuatBetweenVectors(targetDirection.Normalize(), sourceDirection.Normalize())
		out[bone].Rotation = correction.Mul(out[bone].Rotation).Normalize()
	}
	return out
}

func globalBindingTransforms(locals []transform, bindings []targetBinding) []transform {
	out := make([]transform, len(locals))
	for i, value := range locals {
		binding := bindings[i]
		if binding.TargetID != "" && binding.Parent >= 0 && binding.Parent < len(out) && bindings[binding.Parent].TargetID != "" {
			parent := out[binding.Parent]
			value.Position = parent.Position.Add(parent.Rotation.Rotate(value.Position))
			value.Rotation = parent.Rotation.Mul(value.Rotation).Normalize()
		}
		out[i] = value
	}
	return out
}

func rootBoneIndex(bones []Bone) int {
	for i, bone := range bones {
		if bone.Parent < 0 {
			return i
		}
	}
	return -1
}

func assetTransform(value content.AssetTransformDef) transform {
	rotation := mgl32.Quat{V: mgl32.Vec3{value.Rotation[0], value.Rotation[1], value.Rotation[2]}, W: value.Rotation[3]}
	if rotation == (mgl32.Quat{}) {
		rotation = mgl32.QuatIdent()
	}
	return transform{Position: mgl32.Vec3{value.Position[0], value.Position[1], value.Position[2]}, Rotation: rotation.Normalize()}
}

func sourceToGekkoPosition(value mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{value.X() * metersPerSourceUnit, value.Z() * metersPerSourceUnit, -value.Y() * metersPerSourceUnit}
}

func sourceToGekkoRotation(value mgl32.Quat) mgl32.Quat {
	col0 := sourceDirectionToGekko(value.Rotate(mgl32.Vec3{1, 0, 0}))
	col1 := sourceDirectionToGekko(value.Rotate(mgl32.Vec3{0, 0, 1}))
	col2 := sourceDirectionToGekko(value.Rotate(mgl32.Vec3{0, -1, 0}))
	return mgl32.Mat4ToQuat(mgl32.Mat3FromCols(col0, col1, col2).Mat4()).Normalize()
}

func sourceDirectionToGekko(value mgl32.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{value.X(), value.Z(), -value.Y()}
}

func eulerXYZQuat(value mgl32.Vec3) mgl32.Quat {
	return mgl32.QuatRotate(value.Z(), mgl32.Vec3{0, 0, 1}).Mul(mgl32.QuatRotate(value.Y(), mgl32.Vec3{0, 1, 0})).Mul(mgl32.QuatRotate(value.X(), mgl32.Vec3{1, 0, 0})).Normalize()
}

func normalizedName(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func safeID(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

type mdlReader struct{ data []byte }

func (r mdlReader) rangeOK(offset, size int) error {
	if offset < 0 || size < 0 || offset > len(r.data)-size {
		return fmt.Errorf("source mdl offset %d size %d outside %d bytes", offset, size, len(r.data))
	}
	return nil
}

func (r mdlReader) i32(offset int) (int, error) {
	if err := r.rangeOK(offset, 4); err != nil {
		return 0, err
	}
	return int(int32(binary.LittleEndian.Uint32(r.data[offset:]))), nil
}

func (r mdlReader) i16(offset int) (int, error) {
	if err := r.rangeOK(offset, 2); err != nil {
		return 0, err
	}
	return int(int16(binary.LittleEndian.Uint16(r.data[offset:]))), nil
}

func (r mdlReader) f32(offset int) (float32, error) {
	if err := r.rangeOK(offset, 4); err != nil {
		return 0, err
	}
	return math.Float32frombits(binary.LittleEndian.Uint32(r.data[offset:])), nil
}

func (r mdlReader) vec3(offset int) (mgl32.Vec3, error) {
	x, err := r.f32(offset)
	if err != nil {
		return mgl32.Vec3{}, err
	}
	y, err := r.f32(offset + 4)
	if err != nil {
		return mgl32.Vec3{}, err
	}
	z, err := r.f32(offset + 8)
	if err != nil {
		return mgl32.Vec3{}, err
	}
	return mgl32.Vec3{x, y, z}, nil
}

func (r mdlReader) quat(offset int) (mgl32.Quat, error) {
	x, err := r.f32(offset)
	if err != nil {
		return mgl32.Quat{}, err
	}
	y, err := r.f32(offset + 4)
	if err != nil {
		return mgl32.Quat{}, err
	}
	z, err := r.f32(offset + 8)
	if err != nil {
		return mgl32.Quat{}, err
	}
	w, err := r.f32(offset + 12)
	if err != nil {
		return mgl32.Quat{}, err
	}
	q := mgl32.Quat{V: mgl32.Vec3{x, y, z}, W: w}
	if q == (mgl32.Quat{}) {
		return mgl32.QuatIdent(), nil
	}
	return q.Normalize(), nil
}

func (r mdlReader) relativeString(base, relative int) (string, error) {
	start := base + relative
	if err := r.rangeOK(start, 1); err != nil {
		return "", err
	}
	end := start
	for end < len(r.data) && r.data[end] != 0 {
		end++
	}
	if end == len(r.data) {
		return "", fmt.Errorf("unterminated source string at %d", start)
	}
	return string(r.data[start:end]), nil
}

func (r mdlReader) animationEuler(offset, frame int, scale mgl32.Vec3) (mgl32.Vec3, error) {
	value, err := r.animationVec(offset, frame, scale)
	if err != nil {
		return mgl32.Vec3{}, err
	}
	return value, nil
}

func (r mdlReader) animationVec(offset, frame int, scale mgl32.Vec3) (mgl32.Vec3, error) {
	if err := r.rangeOK(offset, 6); err != nil {
		return mgl32.Vec3{}, err
	}
	values := [3]float32{}
	for axis := range values {
		relative, err := r.i16(offset + axis*2)
		if err != nil {
			return mgl32.Vec3{}, err
		}
		if relative > 0 {
			value, err := r.animationValue(offset+relative, frame)
			if err != nil {
				return mgl32.Vec3{}, err
			}
			values[axis] = float32(value)
		}
	}
	return mgl32.Vec3{values[0] * scale.X(), values[1] * scale.Y(), values[2] * scale.Z()}, nil
}

func (r mdlReader) animationValue(offset, frame int) (int16, error) {
	for remaining, current := frame, offset; ; {
		if err := r.rangeOK(current, 2); err != nil {
			return 0, err
		}
		valid, total := int(r.data[current]), int(r.data[current+1])
		if total == 0 || valid > total {
			return 0, fmt.Errorf("invalid animation value run valid=%d total=%d", valid, total)
		}
		if remaining < total {
			if valid == 0 {
				return 0, nil
			}
			index := valid - 1
			if remaining < valid {
				index = remaining
			}
			if err := r.rangeOK(current+2+index*2, 2); err != nil {
				return 0, err
			}
			return int16(binary.LittleEndian.Uint16(r.data[current+2+index*2:])), nil
		}
		remaining -= total
		current += 2 + valid*2
	}
}

func (r mdlReader) quat48(offset int) (mgl32.Quat, error) {
	if err := r.rangeOK(offset, 6); err != nil {
		return mgl32.Quat{}, err
	}
	x := (float32(binary.LittleEndian.Uint16(r.data[offset:])) - 32768) / 32768
	y := (float32(binary.LittleEndian.Uint16(r.data[offset+2:])) - 32768) / 32768
	zw := binary.LittleEndian.Uint16(r.data[offset+4:])
	z := (float32(zw&0x7fff) - 16384) / 16384
	w2 := maxFloat(0, 1-x*x-y*y-z*z)
	w := float32(math.Sqrt(float64(w2)))
	if zw&0x8000 != 0 {
		w = -w
	}
	return mgl32.Quat{V: mgl32.Vec3{x, y, z}, W: w}.Normalize(), nil
}

func (r mdlReader) quat64(offset int) (mgl32.Quat, error) {
	if err := r.rangeOK(offset, 8); err != nil {
		return mgl32.Quat{}, err
	}
	v := binary.LittleEndian.Uint64(r.data[offset:])
	x := (float32(v&0x1fffff) - 1048576) / 1048576.5
	y := (float32((v>>21)&0x1fffff) - 1048576) / 1048576.5
	z := (float32((v>>42)&0x1fffff) - 1048576) / 1048576.5
	w := float32(math.Sqrt(float64(maxFloat(0, 1-x*x-y*y-z*z))))
	if v>>63 != 0 {
		w = -w
	}
	return mgl32.Quat{V: mgl32.Vec3{x, y, z}, W: w}.Normalize(), nil
}

func (r mdlReader) vec48(offset int) (mgl32.Vec3, error) {
	if err := r.rangeOK(offset, 6); err != nil {
		return mgl32.Vec3{}, err
	}
	return mgl32.Vec3{halfFloat(binary.LittleEndian.Uint16(r.data[offset:])), halfFloat(binary.LittleEndian.Uint16(r.data[offset+2:])), halfFloat(binary.LittleEndian.Uint16(r.data[offset+4:]))}, nil
}

func halfFloat(value uint16) float32 {
	sign := uint32(value>>15) << 31
	exponent := int((value >> 10) & 0x1f)
	mantissa := uint32(value & 0x03ff)
	switch exponent {
	case 0:
		if mantissa == 0 {
			return math.Float32frombits(sign)
		}
		for mantissa&0x400 == 0 {
			mantissa <<= 1
			exponent--
		}
		mantissa &= 0x3ff
		exponent++
	case 31:
		return math.Float32frombits(sign | 0x7f800000 | mantissa<<13)
	}
	return math.Float32frombits(sign | uint32(exponent+112)<<23 | mantissa<<13)
}

func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
