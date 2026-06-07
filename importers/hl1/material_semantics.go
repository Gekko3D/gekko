package hl1

import (
	"path/filepath"
	"strings"
)

type hl1MaterialSemantics struct {
	Kind          string
	CollisionKind string
	Transparent   bool
	EmitsLight    bool
	Emissive      float32
	Roughness     float32
	Metallic      float32
	Transparency  float32
	Tags          []string
}

func materialSemantics(textureName string) hl1MaterialSemantics {
	raw := strings.TrimSpace(textureName)
	rawLower := strings.ToLower(raw)
	name := normalizedMaterialTextureName(raw)
	kind := "structural"
	collision := "solid"
	roughness := float32(0.9)
	metallic := float32(0)
	transparency := float32(0)
	transparent := false
	tags := []string{"source:hl1"}
	addTag := func(tag string) {
		for _, existing := range tags {
			if existing == tag {
				return
			}
		}
		tags = append(tags, tag)
	}
	if raw != "" {
		addTag("source_texture:" + filepath.ToSlash(raw))
	}
	switch {
	case isCandidateEmissiveTextureName(raw):
		kind = "emissive"
		roughness = 0.45
		addTag("material:emissive")
	case name == "sky":
		kind = "sky"
		collision = "none"
		addTag("material:sky")
	case strings.Contains(name, "aaatrigger") || strings.Contains(name, "trigger"):
		kind = "trigger"
		collision = "none"
		transparent = true
		transparency = 1
		addTag("material:tool")
	case strings.Contains(name, "clip"):
		kind = "clip"
		collision = "solid"
		transparent = true
		transparency = 1
		addTag("material:tool")
	case strings.Contains(name, "origin"):
		kind = "origin"
		collision = "none"
		transparent = true
		transparency = 1
		addTag("material:tool")
	case name == "null" || name == "skip" || name == "hint":
		kind = "tool"
		collision = "none"
		transparent = true
		transparency = 1
		addTag("material:tool")
	case strings.Contains(name, "slime"):
		kind = "slime"
		collision = "liquid"
		transparency = 0.35
		addTag("material:liquid")
	case strings.Contains(name, "lava"):
		kind = "lava"
		collision = "liquid"
		transparency = 0.2
		addTag("material:liquid")
		addTag("material:emissive")
	case strings.Contains(name, "water") || strings.HasPrefix(rawLower, "!"):
		kind = "water"
		collision = "liquid"
		transparency = 0.45
		addTag("material:liquid")
	case strings.Contains(name, "ladder"):
		kind = "ladder"
		collision = "ladder"
		transparent = true
		transparency = 0.35
		addTag("material:ladder")
	case strings.HasPrefix(name, "{"):
		kind = "transparent"
		collision = "none"
		transparent = true
		transparency = 0.45
		addTag("material:transparent")
	case containsAny(name, "glass", "window"):
		kind = "glass"
		transparent = true
		transparency = 0.55
		roughness = 0.08
		addTag("material:glass")
	case containsAny(name, "grate", "fence", "chain"):
		kind = "grate"
		transparent = true
		transparency = 0.35
		metallic = 0.65
		roughness = 0.55
		addTag("material:metal")
		addTag("material:cutout")
	case containsAny(name, "metal", "metl", "steel", "pipe", "vent", "duct", "rail", "trim"):
		kind = "metal"
		metallic = 0.85
		roughness = 0.48
		addTag("material:metal")
	case containsAny(name, "crete", "concrete", "cement", "cinder", "brick", "block", "stone", "rock"):
		kind = "concrete"
		roughness = 0.95
		addTag("material:masonry")
	case containsAny(name, "wood", "crate", "pallet", "plank", "board"):
		kind = "wood"
		roughness = 0.8
		addTag("material:wood")
	case containsAny(name, "dirt", "mud", "sand", "grass", "ground"):
		kind = "terrain"
		roughness = 1
		addTag("material:terrain")
	case containsAny(name, "tile", "floor", "wall", "ceiling"):
		addTag("material:architectural")
	default:
		addTag("material:structural")
	}
	emitsLight := isCandidateEmissiveTextureName(raw) || kind == "lava"
	emissive := float32(0)
	if emitsLight {
		emissive = emissiveStrengthForTextureName(raw)
		if emissive <= 0 {
			emissive = 1.8
		}
	}
	return hl1MaterialSemantics{
		Kind:          kind,
		CollisionKind: collision,
		Transparent:   transparent,
		EmitsLight:    emitsLight,
		Emissive:      emissive,
		Roughness:     roughness,
		Metallic:      metallic,
		Transparency:  transparency,
		Tags:          tags,
	}
}

func normalizedMaterialTextureName(textureName string) string {
	name := normalizedEmissiveTextureName(textureName)
	for len(name) > 0 && (name[0] == '{' || name[0] == '!' || name[0] == '~') {
		name = name[1:]
	}
	return name
}

func containsAny(name string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(name, needle) {
			return true
		}
	}
	return false
}
