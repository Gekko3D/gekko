package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Subject  string   `json:"subject,omitempty"`
	Message  string   `json:"message"`
}

type EntityCount struct {
	ClassName string `json:"classname"`
	Count     int    `json:"count"`
}

type ImportReport struct {
	Source                  SourceInfo    `json:"source"`
	GeneratedLevelPath      string        `json:"generated_level_path,omitempty"`
	GeneratedWorldPath      string        `json:"generated_world_path,omitempty"`
	ChunkCount              int           `json:"chunk_count,omitempty"`
	NonEmptyVoxelCount      int           `json:"non_empty_voxel_count,omitempty"`
	MaterialCount           int           `json:"material_count,omitempty"`
	PaletteCount            int           `json:"palette_count,omitempty"`
	ModelCount              int           `json:"model_count,omitempty"`
	FaceCount               int           `json:"face_count,omitempty"`
	SkyFaceCount            int           `json:"sky_face_count,omitempty"`
	EntityCounts            []EntityCount `json:"entity_counts,omitempty"`
	UnsupportedEntityCounts []EntityCount `json:"unsupported_entity_counts,omitempty"`
	BoundsBeforeConversion  Bounds        `json:"bounds_before_conversion,omitempty"`
	BoundsAfterConversion   Bounds        `json:"bounds_after_conversion,omitempty"`
	Diagnostics             []Diagnostic  `json:"diagnostics,omitempty"`
}

func EntityCounts(classNames []string) []EntityCount {
	counts := make(map[string]int, len(classNames))
	for _, className := range classNames {
		if className == "" {
			className = "<missing>"
		}
		counts[className]++
	}
	out := make([]EntityCount, 0, len(counts))
	for className, count := range counts {
		out = append(out, EntityCount{ClassName: className, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ClassName < out[j].ClassName
	})
	return out
}

func SaveImportReport(path string, report ImportReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0644)
}
