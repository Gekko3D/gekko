package gekko

import (
	"math"
	"slices"
	"strings"
)

// VoxelMaterialFingerprintBudgetBytes bounds conservatively accounted retained
// snapshots and their metadata, excluding assets, temporary effective palettes,
// material tables and GPU allocations. It is not a total process memory limit.
const VoxelMaterialFingerprintBudgetBytes = 8 << 20

type voxelMaterialFingerprint struct {
	palette     VoxelPaletteAsset
	fingerprint uint64
	bytes       uint64
}

func (s *VoxelRtState) beginMaterialFingerprintSync(server *AssetServer) {
	if s.materialFingerprintServer != server {
		s.materialFingerprints = nil
		s.materialFingerprintBytes = 0
		s.materialFingerprintsPruned = false
		s.materialFingerprintServer = server
	}
	s.publishMaterialFingerprintOwnership()
}

func (s *VoxelRtState) publishMaterialFingerprintOwnership() {
	s.VoxelMaterialFingerprintCount = len(s.materialFingerprints)
	s.VoxelMaterialFingerprintBytes = s.materialFingerprintBytes
}

func (s *VoxelRtState) materialFingerprint(id AssetId, palette *VoxelPaletteAsset) uint64 {
	if s == nil {
		return materialTableFingerprint(palette)
	}
	// Exported diagnostics are observations, never authoritative admission data.
	defer s.publishMaterialFingerprintOwnership()
	old, present := s.materialFingerprints[id]
	if present && palette != nil && equalMaterialFingerprintPalette(&old.palette, palette) {
		return old.fingerprint
	}
	s.VoxelMaterialFingerprintBuildCount++
	fingerprint := materialTableFingerprint(palette)
	// Replacement admission credits the old entry, without ever copying an
	// oversized or unsupported input. Failed admission drops the prior snapshot.
	available := uint64(VoxelMaterialFingerprintBudgetBytes) - (s.materialFingerprintBytes - old.bytes)
	bytes, supported := materialFingerprintBytes(palette, available)
	if !supported {
		if present {
			delete(s.materialFingerprints, id)
			s.materialFingerprintBytes -= old.bytes
			s.materialFingerprintsPruned = true
			if len(s.materialFingerprints) == 0 {
				s.materialFingerprints = nil
			}
		}
		return fingerprint
	}
	if s.materialFingerprints == nil {
		s.materialFingerprints = make(map[AssetId]voxelMaterialFingerprint)
	}
	s.materialFingerprints[id] = voxelMaterialFingerprint{cloneMaterialFingerprintPalette(palette), fingerprint, bytes}
	s.materialFingerprintBytes = s.materialFingerprintBytes - old.bytes + bytes
	return fingerprint
}

func (s *VoxelRtState) pruneMaterialFingerprints(used map[AssetId]materialTableCacheKey) {
	count := 0
	for id := range s.materialFingerprints {
		if _, present := used[id]; present {
			count++
		}
	}
	if count == len(s.materialFingerprints) && !s.materialFingerprintsPruned {
		return
	}
	var retained map[AssetId]voxelMaterialFingerprint
	var bytes uint64
	if count != 0 {
		retained = make(map[AssetId]voxelMaterialFingerprint, count)
		for id, entry := range s.materialFingerprints {
			if _, present := used[id]; present {
				retained[id] = entry
				bytes += entry.bytes
			}
		}
	}
	// Rebuild after removal so historical map capacity cannot retain peak users.
	s.materialFingerprints = retained
	s.materialFingerprintBytes = bytes
	s.publishMaterialFingerprintOwnership()
	s.materialFingerprintsPruned = false
}

// Account before cloning with bounded arithmetic. The base charge covers the
// fixed palette, entry, map allocation/buckets and allocator metadata. Variable
// charges conservatively cover exact-length backing, maps, interfaces and strings.
func materialFingerprintBytes(p *VoxelPaletteAsset, limit uint64) (uint64, bool) {
	if p == nil || limit < 4096 {
		return 0, false
	}
	bytes := uint64(4096)
	add := func(n int, size uint64) bool {
		if uint64(n) > (limit-bytes)/size {
			return false
		}
		bytes += uint64(n) * size
		return true
	}
	if !add(len(p.SourcePath), 1) || !add(len(p.Materials), 64) || !add(len(p.Animations), 256) || !add(len(p.MaterialFrameOverrides), 128) {
		return 0, false
	}
	for _, material := range p.Materials {
		if !add(1, 128) || !add(len(material.Property), 128) {
			return 0, false
		}
		for key, value := range material.Property {
			if !add(len(key), 1) {
				return 0, false
			}
			switch value := value.(type) {
			case float32, float64, int:
			case string:
				if !add(len(value), 1) {
					return 0, false
				}
			default:
				return 0, false
			}
		}
	}
	for _, animation := range p.Animations {
		if !add(len(animation.ID), 1) || !add(len(animation.Kind), 1) || !add(len(animation.Mode), 1) ||
			!add(len(animation.PaletteIndices), 1) || !add(len(animation.Frames), 256) || !add(len(animation.Tags), 32) {
			return 0, false
		}
		if animation.UVScroll != nil && !add(1, 128) {
			return 0, false
		}
		for _, tag := range animation.Tags {
			if !add(len(tag), 1) {
				return 0, false
			}
		}
		for _, frame := range animation.Frames {
			if !add(len(frame.Colors), 4) || !add(len(frame.EmissiveColors), 4) || !add(len(frame.Emission), 4) ||
				!add(len(frame.Roughness), 4) || !add(len(frame.Transparency), 4) {
				return 0, false
			}
		}
	}
	return bytes, true
}

func equalMaterialFingerprintPalette(a, b *VoxelPaletteAsset) bool {
	if a.VoxPalette != b.VoxPalette || a.IsPBR != b.IsPBR || a.SourcePath != b.SourcePath ||
		math.Float32bits(a.Roughness) != math.Float32bits(b.Roughness) || math.Float32bits(a.Metalness) != math.Float32bits(b.Metalness) ||
		math.Float32bits(a.Emission) != math.Float32bits(b.Emission) || math.Float32bits(a.IOR) != math.Float32bits(b.IOR) ||
		math.Float32bits(a.Transparency) != math.Float32bits(b.Transparency) ||
		len(a.Materials) != len(b.Materials) || len(a.Animations) != len(b.Animations) || len(a.MaterialFrameOverrides) != len(b.MaterialFrameOverrides) {
		return false
	}
	for i, material := range a.Materials {
		other := b.Materials[i]
		if material.ID != other.ID || material.Type != other.Type || math.Float32bits(material.Weight) != math.Float32bits(other.Weight) || len(material.Property) != len(other.Property) {
			return false
		}
		for key, value := range material.Property {
			otherValue, present := other.Property[key]
			if !present || !equalMaterialFingerprintProperty(value, otherValue) {
				return false
			}
		}
	}
	for i, animation := range a.Animations {
		other := b.Animations[i]
		if animation.ID != other.ID || animation.Kind != other.Kind || animation.Mode != other.Mode || math.Float32bits(animation.FPS) != math.Float32bits(other.FPS) ||
			!slices.Equal(animation.PaletteIndices, other.PaletteIndices) || !slices.Equal(animation.Tags, other.Tags) || len(animation.Frames) != len(other.Frames) ||
			(animation.UVScroll == nil) != (other.UVScroll == nil) {
			return false
		}
		if animation.UVScroll != nil && (math.Float32bits(animation.UVScroll.Velocity[0]) != math.Float32bits(other.UVScroll.Velocity[0]) ||
			math.Float32bits(animation.UVScroll.Velocity[1]) != math.Float32bits(other.UVScroll.Velocity[1])) {
			return false
		}
		for j, frame := range animation.Frames {
			otherFrame := other.Frames[j]
			if math.Float32bits(frame.Duration) != math.Float32bits(otherFrame.Duration) || !slices.Equal(frame.Colors, otherFrame.Colors) ||
				!slices.Equal(frame.EmissiveColors, otherFrame.EmissiveColors) || !equalMaterialFingerprintFloats(frame.Emission, otherFrame.Emission) ||
				!equalMaterialFingerprintFloats(frame.Roughness, otherFrame.Roughness) || !equalMaterialFingerprintFloats(frame.Transparency, otherFrame.Transparency) {
				return false
			}
		}
	}
	for index, override := range a.MaterialFrameOverrides {
		other, present := b.MaterialFrameOverrides[index]
		if !present || override.EmissiveColor != other.EmissiveColor || override.HasEmissiveColor != other.HasEmissiveColor ||
			override.HasEmission != other.HasEmission || override.HasRoughness != other.HasRoughness || override.HasTransparency != other.HasTransparency ||
			math.Float32bits(override.Emission) != math.Float32bits(other.Emission) || math.Float32bits(override.Roughness) != math.Float32bits(other.Roughness) ||
			math.Float32bits(override.Transparency) != math.Float32bits(other.Transparency) {
			return false
		}
	}
	return true
}

func equalMaterialFingerprintProperty(a, b any) bool {
	switch a := a.(type) {
	case float32:
		b, ok := b.(float32)
		return ok && math.Float32bits(a) == math.Float32bits(b)
	case float64:
		b, ok := b.(float64)
		return ok && math.Float64bits(a) == math.Float64bits(b)
	case int:
		b, ok := b.(int)
		return ok && a == b
	case string:
		b, ok := b.(string)
		return ok && a == b
	default:
		return false
	}
}

func equalMaterialFingerprintFloats(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i, value := range a {
		if math.Float32bits(value) != math.Float32bits(b[i]) {
			return false
		}
	}
	return true
}

func cloneMaterialFingerprintSlice[T any](source []T) []T {
	if len(source) == 0 {
		return nil
	}
	copySlice := make([]T, len(source))
	copy(copySlice, source)
	return copySlice
}

// Only called after the complete budget/support preflight. Every retained
// mutable backing and string is independently owned; SurfaceMaterials is omitted.
func cloneMaterialFingerprintPalette(source *VoxelPaletteAsset) VoxelPaletteAsset {
	palette := *source
	palette.SourcePath = strings.Clone(source.SourcePath)
	palette.SurfaceMaterials = nil
	palette.Materials = cloneMaterialFingerprintSlice(source.Materials)
	for i := range palette.Materials {
		material := &palette.Materials[i]
		property := material.Property
		material.Property = nil
		if len(property) != 0 {
			material.Property = make(map[string]interface{}, len(property))
			for key, value := range property {
				if text, ok := value.(string); ok {
					value = strings.Clone(text)
				}
				material.Property[strings.Clone(key)] = value
			}
		}
	}
	palette.Animations = cloneMaterialFingerprintSlice(source.Animations)
	for i := range palette.Animations {
		animation := &palette.Animations[i]
		animation.ID, animation.Kind, animation.Mode = strings.Clone(animation.ID), strings.Clone(animation.Kind), strings.Clone(animation.Mode)
		animation.PaletteIndices = cloneMaterialFingerprintSlice(animation.PaletteIndices)
		animation.Tags = cloneMaterialFingerprintSlice(animation.Tags)
		for j, tag := range animation.Tags {
			animation.Tags[j] = strings.Clone(tag)
		}
		if animation.UVScroll != nil {
			copyScroll := *animation.UVScroll
			animation.UVScroll = &copyScroll
		}
		animation.Frames = cloneMaterialFingerprintSlice(animation.Frames)
		for j := range animation.Frames {
			frame := &animation.Frames[j]
			frame.Colors = cloneMaterialFingerprintSlice(frame.Colors)
			frame.EmissiveColors = cloneMaterialFingerprintSlice(frame.EmissiveColors)
			frame.Emission = cloneMaterialFingerprintSlice(frame.Emission)
			frame.Roughness = cloneMaterialFingerprintSlice(frame.Roughness)
			frame.Transparency = cloneMaterialFingerprintSlice(frame.Transparency)
		}
	}
	palette.MaterialFrameOverrides = nil
	if len(source.MaterialFrameOverrides) != 0 {
		palette.MaterialFrameOverrides = make(map[uint8]VoxelPaletteMaterialFrameOverride, len(source.MaterialFrameOverrides))
		for index, override := range source.MaterialFrameOverrides {
			palette.MaterialFrameOverrides[index] = override
		}
	}
	return palette
}
