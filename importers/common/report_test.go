package common

import "testing"

func TestPopulateImportReportDiagnosticCounts(t *testing.T) {
	report := ImportReport{
		Diagnostics: []Diagnostic{
			{Severity: SeverityWarning, Code: "hl1.texture_missing"},
			{Severity: SeverityWarning, Code: "hl1.texture_missing"},
			{Severity: SeverityInfo, Code: "hl1.asset_skipped"},
		},
	}
	PopulateImportReportDiagnosticCounts(&report)

	if len(report.DiagnosticSeverityCounts) != 2 {
		t.Fatalf("severity counts = %+v", report.DiagnosticSeverityCounts)
	}
	if report.DiagnosticSeverityCounts[0] != (NamedCount{Name: "info", Count: 1}) {
		t.Fatalf("unexpected first severity count: %+v", report.DiagnosticSeverityCounts)
	}
	if report.DiagnosticSeverityCounts[1] != (NamedCount{Name: "warning", Count: 2}) {
		t.Fatalf("unexpected second severity count: %+v", report.DiagnosticSeverityCounts)
	}
	if len(report.DiagnosticCodeCounts) != 2 {
		t.Fatalf("code counts = %+v", report.DiagnosticCodeCounts)
	}
	if report.DiagnosticCodeCounts[0] != (NamedCount{Name: "hl1.asset_skipped", Count: 1}) {
		t.Fatalf("unexpected first code count: %+v", report.DiagnosticCodeCounts)
	}
	if report.DiagnosticCodeCounts[1] != (NamedCount{Name: "hl1.texture_missing", Count: 2}) {
		t.Fatalf("unexpected second code count: %+v", report.DiagnosticCodeCounts)
	}
}
