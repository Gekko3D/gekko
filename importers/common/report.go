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

type NamedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type ImportReport struct {
	Source                      SourceInfo    `json:"source"`
	GeneratedLevelPath          string        `json:"generated_level_path,omitempty"`
	GeneratedWorldPath          string        `json:"generated_world_path,omitempty"`
	ChunkCount                  int           `json:"chunk_count,omitempty"`
	NonEmptyVoxelCount          int           `json:"non_empty_voxel_count,omitempty"`
	MaterialCount               int           `json:"material_count,omitempty"`
	MaterialAnimationCount      int           `json:"material_animation_count,omitempty"`
	AnimatedMaterialCount       int           `json:"animated_material_count,omitempty"`
	MaterialAnimationFrameCount int           `json:"material_animation_frame_count,omitempty"`
	MaterialKindCounts          []EntityCount `json:"material_kind_counts,omitempty"`
	UnclassifiedMaterials       []string      `json:"unclassified_materials,omitempty"`
	PaletteCount                int           `json:"palette_count,omitempty"`
	ModelCount                  int           `json:"model_count,omitempty"`
	FaceCount                   int           `json:"face_count,omitempty"`
	SkyFaceCount                int           `json:"sky_face_count,omitempty"`
	EntityCounts                []EntityCount `json:"entity_counts,omitempty"`
	UnsupportedEntityCounts     []EntityCount `json:"unsupported_entity_counts,omitempty"`
	MovingBrushEntityCounts     []EntityCount `json:"moving_brush_entity_counts,omitempty"`
	PathNodeEntityCounts        []EntityCount `json:"path_node_entity_counts,omitempty"`
	LadderEntityCounts          []EntityCount `json:"ladder_entity_counts,omitempty"`
	ChargerEntityCounts         []EntityCount `json:"charger_entity_counts,omitempty"`
	PickupEntityCounts          []EntityCount `json:"pickup_entity_counts,omitempty"`
	TriggerEntityCounts         []EntityCount `json:"trigger_entity_counts,omitempty"`
	BreakableEntityCounts       []EntityCount `json:"breakable_entity_counts,omitempty"`
	UnresolvedTargetCounts      []NamedCount  `json:"unresolved_target_counts,omitempty"`
	SkippedMultiTargetCounts    []NamedCount  `json:"skipped_multi_target_counts,omitempty"`
	BoundsBeforeConversion      Bounds        `json:"bounds_before_conversion,omitempty"`
	BoundsAfterConversion       Bounds        `json:"bounds_after_conversion,omitempty"`
	Diagnostics                 []Diagnostic  `json:"diagnostics,omitempty"`
	DiagnosticSeverityCounts    []NamedCount  `json:"diagnostic_severity_counts,omitempty"`
	DiagnosticCodeCounts        []NamedCount  `json:"diagnostic_code_counts,omitempty"`
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

func NamedCounts(names []string) []NamedCount {
	counts := make(map[string]int, len(names))
	for _, name := range names {
		if name == "" {
			name = "<missing>"
		}
		counts[name]++
	}
	out := make([]NamedCount, 0, len(counts))
	for name, count := range counts {
		out = append(out, NamedCount{Name: name, Count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func PopulateImportReportDiagnosticCounts(report *ImportReport) {
	if report == nil {
		return
	}
	severities := make([]string, 0, len(report.Diagnostics))
	codes := make([]string, 0, len(report.Diagnostics))
	for _, diagnostic := range report.Diagnostics {
		severities = append(severities, string(diagnostic.Severity))
		codes = append(codes, diagnostic.Code)
	}
	report.DiagnosticSeverityCounts = NamedCounts(severities)
	report.DiagnosticCodeCounts = NamedCounts(codes)
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
