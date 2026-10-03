package hl1

import (
	"fmt"
	"strings"

	"github.com/gekko3d/gekko/content"
)

func ApplyHL1ExportProfile(opts ImportOptions) (ImportOptions, error) {
	profile, err := NormalizeHL1ExportProfile(opts.ExportProfile)
	if err != nil {
		return ImportOptions{}, err
	}
	opts.ExportProfile = profile
	switch profile {
	case HL1ExportProfileDefault:
		return opts, nil
	case HL1ExportProfileRustyVoxelRTInteropV1:
		if opts.EmbedNormals {
			return ImportOptions{}, fmt.Errorf("embedded normals are incompatible with Rust JSON export")
		}
		opts.ChunkPayloadKind = content.ImportedWorldChunkPayloadSparseJSONV1
		opts.EmitGameAssets = true
		return opts, nil
	default:
		return ImportOptions{}, fmt.Errorf("unsupported HL1 export profile %q", profile)
	}
}

func NormalizeHL1ExportProfile(profile HL1ExportProfile) (HL1ExportProfile, error) {
	value := strings.TrimSpace(string(profile))
	switch value {
	case "", "default":
		return HL1ExportProfileDefault, nil
	case string(HL1ExportProfileRustyVoxelRTInteropV1), "rusty-voxelrt-interop-v1", "rusty_voxelrt":
		return HL1ExportProfileRustyVoxelRTInteropV1, nil
	default:
		return "", fmt.Errorf("unsupported HL1 export profile %q", value)
	}
}

func HL1ExportProfileTags(profile HL1ExportProfile) []string {
	switch profile {
	case HL1ExportProfileRustyVoxelRTInteropV1:
		return []string{HL1ExportProfileRustyVoxelRTInteropTag}
	default:
		return nil
	}
}
