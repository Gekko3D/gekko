package hl1

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gekko3d/gekko/content"
)

// LoadMDLAnimationClips decodes selected GoldSrc sequences into semantic-joint
// tracks. Consumers can bake those tracks onto a target rig; runtime playback
// remains source-format agnostic.
func LoadMDLAnimationClips(path string, sequenceNames []string, lockRootMotion bool) ([]content.AssetAnimationClipDef, error) {
	if len(sequenceNames) == 0 {
		return nil, fmt.Errorf("at least one GoldSrc sequence is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	info, err := ParseMDLInfo(data)
	if err != nil {
		return nil, err
	}
	if diagnostics := decodeMDLExternalSequenceGroups(data, path, &info, sequenceNames); len(diagnostics) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(diagnostics, "; "))
	}
	return buildMDLAnimationClips(info, sequenceNames, lockRootMotion)
}

func decodeMDLExternalSequenceGroups(data []byte, path string, info *MDLInfo, sequenceNames []string) []string {
	if info == nil {
		return nil
	}
	groups := map[int][]byte{}
	failedGroups := map[int]bool{}
	var diagnostics []string
	for index := range info.Sequences {
		sequence := &info.Sequences[index]
		if sequence.SeqGroup <= 0 || (len(sequenceNames) > 0 && !containsFold(sequenceNames, sequence.Name)) {
			continue
		}
		if failedGroups[sequence.SeqGroup] {
			continue
		}
		groupData := groups[sequence.SeqGroup]
		if groupData == nil {
			groupPath, err := mdlSequenceGroupPath(data, path, sequence.SeqGroup)
			if err != nil {
				diagnostics = append(diagnostics, err.Error())
				failedGroups[sequence.SeqGroup] = true
				continue
			}
			groupData, err = os.ReadFile(groupPath)
			if err != nil {
				diagnostics = append(diagnostics, fmt.Sprintf("read GoldSrc sequence group %d (%s): %v", sequence.SeqGroup, filepath.Base(groupPath), err))
				failedGroups[sequence.SeqGroup] = true
				continue
			}
			if err := validateMDLSequenceGroup(groupData); err != nil {
				diagnostics = append(diagnostics, fmt.Sprintf("GoldSrc sequence group %d (%s): %v", sequence.SeqGroup, filepath.Base(groupPath), err))
				failedGroups[sequence.SeqGroup] = true
				continue
			}
			groups[sequence.SeqGroup] = groupData
		}
		external := *sequence
		external.SeqGroup = 0
		blends := decodeMDLSequenceAnimationBlends(groupData, external, info.Bones)
		if len(blends) == 0 {
			diagnostics = append(diagnostics, fmt.Sprintf("GoldSrc sequence %q in external group %d has no decodable animation", sequence.Name, sequence.SeqGroup))
			continue
		}
		setMDLSequenceAnimations(sequence, blends)
	}
	return diagnostics
}

func ParseMDLAnimationClips(data []byte, sequenceNames []string, lockRootMotion bool) ([]content.AssetAnimationClipDef, error) {
	if len(sequenceNames) == 0 {
		return nil, fmt.Errorf("at least one GoldSrc sequence is required")
	}
	info, err := ParseMDLInfo(data)
	if err != nil {
		return nil, err
	}
	return buildMDLAnimationClips(info, sequenceNames, lockRootMotion)
}

func buildMDLAnimationClips(info MDLInfo, sequenceNames []string, lockRootMotion bool) ([]content.AssetAnimationClipDef, error) {
	skeleton := mdlAssetSkeleton(info.Bones)
	if skeleton == nil || len(skeleton.Bones) != len(info.Bones) {
		return nil, fmt.Errorf("GoldSrc model has no usable skeleton")
	}
	targets := make([]mdlAnimationBindTarget, len(info.Bones))
	for index, bone := range skeleton.Bones {
		targets[index] = mdlAnimationBindTarget{
			ID: bone.JointID, BoneIndex: index, Position: bone.Transform.Position,
			Rotation: bone.Transform.Rotation, Scale: content.Vec3{1, 1, 1},
		}
	}
	byName := make(map[string]content.AssetAnimationClipDef, len(sequenceNames))
	for _, sequence := range info.Sequences {
		name := strings.ToLower(strings.TrimSpace(sequence.Name))
		if !containsFold(sequenceNames, name) {
			continue
		}
		if reason := mdlUnsupportedBlendReason(sequence); reason != "" {
			return nil, fmt.Errorf("GoldSrc sequence %q: %s", sequence.Name, reason)
		}
		if len(sequence.BoneAnimations) == 0 && len(sequence.BlendAnimations) == 0 {
			if sequence.SeqGroup != 0 {
				return nil, fmt.Errorf("GoldSrc sequence %q requires external group %d; use LoadMDLAnimationClips", sequence.Name, sequence.SeqGroup)
			}
			return nil, fmt.Errorf("GoldSrc sequence %q has no decoded animation", sequence.Name)
		}
		clip, ok := mdlDecodedAnimationClip(sequence, info.Bones, targets, lockRootMotion)
		if !ok {
			return nil, fmt.Errorf("GoldSrc sequence %q could not be decoded", sequence.Name)
		}
		byName[name] = clip
	}
	clips := make([]content.AssetAnimationClipDef, 0, len(sequenceNames))
	for _, requested := range sequenceNames {
		clip, ok := byName[strings.ToLower(strings.TrimSpace(requested))]
		if !ok {
			return nil, fmt.Errorf("GoldSrc model does not contain requested sequence %q", requested)
		}
		clips = append(clips, clip)
	}
	return clips, nil
}

func mdlSequenceGroupPath(data []byte, mdlPath string, group int) (string, error) {
	const recordSize = 104
	if len(data) < mdlHeaderSize {
		return "", fmt.Errorf("GoldSrc model is too small for sequence groups")
	}
	count, offset := int(readInt32(data, 172)), int(readInt32(data, 176))
	if group <= 0 || group >= count || offset < 0 || offset > len(data) || count > (len(data)-offset)/recordSize {
		return "", fmt.Errorf("GoldSrc sequence group %d is out of range", group)
	}
	base := offset + group*recordSize
	name := filepath.Base(strings.ReplaceAll(cString(data[base+32:base+96]), "\\", "/"))
	if name == "" || name == "." {
		return "", fmt.Errorf("GoldSrc sequence group %d has no file name", group)
	}
	return filepath.Join(filepath.Dir(mdlPath), name), nil
}

func validateMDLSequenceGroup(data []byte) error {
	const headerSize = 76
	if len(data) < headerSize {
		return fmt.Errorf("file is too small: %d bytes", len(data))
	}
	if ident := string(data[:4]); ident != "IDSQ" {
		return fmt.Errorf("unsupported ident %q", ident)
	}
	if version := int(readInt32(data, 4)); version != MDLVersion10 {
		return fmt.Errorf("unsupported version %d", version)
	}
	if length := int(readInt32(data, 72)); length < headerSize || length > len(data) {
		return fmt.Errorf("invalid length %d", length)
	}
	return nil
}

func containsFold(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), wanted) {
			return true
		}
	}
	return false
}
