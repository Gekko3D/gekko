package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Use the public wire contract to keep the tests executable before report gains
// P3c fields. Unknown JSON fields currently disappear, producing runtime RED.
func p3cReport(t *testing.T, version int, mode, materials string) report {
	t.Helper()
	r := pairedReport(mode)
	r.Version = version
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if materials == "" {
		delete(wire, "material_storage")
	} else {
		wire["material_storage"], err = json.Marshal(materials)
		if err != nil {
			t.Fatal(err)
		}
	}
	data, err = json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func p3cMaterialPair(t *testing.T, mode string) (report, report) {
	t.Helper()
	atlas := p3cReport(t, 2, mode, "atlas")
	packed := p3cReport(t, 2, mode, "packed")
	// Distinct measured durations for the material policy axis, independent of
	// dense/packed normals. CPU costs and capacity must not determine the ratio.
	atlas.RawTicks = []uint64{100, 120, 200, 240, 300, 360}
	atlas.GPU = &sampleSummary{Count: 3, MedianTicks: 20, P95Ticks: 30}
	packed.RawTicks = []uint64{100, 130, 200, 260, 300, 390}
	packed.GPU = &sampleSummary{Count: 3, MedianTicks: 30, P95Ticks: 45}
	atlas.Initial.UpdateNS, packed.Initial.UpdateNS = 999999, 1
	atlas.CPU, packed.CPU = []cpuCost{{UpdateNS: 9999}}, []cpuCost{{UpdateNS: 1}}
	atlas.AuxiliaryCapacityBytes, packed.AuxiliaryCapacityBytes = 1000, 1
	return atlas, packed
}

func TestP3cReportMaterialWireContract(t *testing.T) {
	field, ok := reflect.TypeOf(report{}).FieldByName("Materials")
	if !ok || field.Type.Kind() != reflect.String || field.Tag.Get("json") != "material_storage" {
		t.Errorf("report requires Materials string with json tag material_storage, got %+v exists=%v", field, ok)
	}
	for _, materials := range []string{"atlas", "packed"} {
		data, err := json.Marshal(p3cReport(t, 2, "dense", materials))
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Version   int    `json:"version"`
			Mode      string `json:"mode"`
			Materials string `json:"material_storage"`
		}
		if err := json.Unmarshal(data, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Version != 2 || wire.Mode != "dense" || wire.Materials != materials {
			t.Errorf("round-trip = %+v, want v2 dense normals and %s materials", wire, materials)
		}
	}
}

func TestP3cReportHasSeparateMaterialRatioJSON(t *testing.T) {
	// The CLI must have independent report slots so a material-axis comparison
	// cannot be mislabeled as a packed-normal/dense-normal result.
	ratioKeys := map[string]bool{}
	typ := reflect.TypeOf(report{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		for _, key := range []string{"packed_material_to_atlas_median_ratio,omitempty", "packed_to_dense_median_ratio,omitempty"} {
			if field.Tag.Get("json") != key {
				continue
			}
			ratioKeys[key] = true
			if field.Type != reflect.TypeOf((*float64)(nil)) {
				t.Errorf("ratio field %s must be optional *float64, got %v", key, field.Type)
				continue
			}
			r := reflect.New(typ)
			value := 1.5
			r.Elem().Field(i).Set(reflect.ValueOf(&value))
			data, err := json.Marshal(r.Interface())
			if err != nil {
				t.Fatal(err)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatal(err)
			}
			wireKey := key[:len(key)-len(",omitempty")]
			if string(wire[wireKey]) != "1.5" {
				t.Errorf("%s JSON = %s, want 1.5", wireKey, wire[wireKey])
			}
			other := "packed_to_dense_median_ratio"
			if wireKey == other {
				other = "packed_material_to_atlas_median_ratio"
			}
			if _, present := wire[other]; present {
				t.Errorf("setting %s also emitted unrelated %s", wireKey, other)
			}
		}
	}
	for _, key := range []string{"packed_material_to_atlas_median_ratio,omitempty", "packed_to_dense_median_ratio,omitempty"} {
		if !ratioKeys[key] {
			t.Errorf("report missing ratio JSON field %s", key)
		}
	}
}

func TestP3cReportSeparatesRetainedAtlasCapacityFromAssignedPayload(t *testing.T) {
	for _, tt := range []struct {
		field string
		key   string
		value uint64
	}{
		{"PayloadAtlasCapacityBytes", "payload_atlas_physical_capacity_bytes", 65536},
		{"PayloadAssignedBytes", "payload_assigned_bytes", 512},
		{"AuxiliaryCapacityBytes", "auxiliary_physical_capacity_bytes", 2048},
	} {
		field, ok := reflect.TypeOf(report{}).FieldByName(tt.field)
		if !ok || field.Type.Kind() != reflect.Uint64 || field.Tag.Get("json") != tt.key {
			t.Errorf("report requires %s uint64 with JSON key %s, got %+v exists=%v", tt.field, tt.key, field, ok)
			continue
		}
		r := reflect.New(reflect.TypeOf(report{}))
		r.Elem().FieldByName(tt.field).SetUint(tt.value)
		data, err := json.Marshal(r.Interface())
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]uint64
		// Decode only the named metric; other report fields have differing types.
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatal(err)
		}
		metric, err := json.Marshal(map[string]json.RawMessage{tt.key: raw[tt.key]})
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(metric, &wire); err != nil {
			t.Fatal(err)
		}
		if wire[tt.key] != tt.value {
			t.Errorf("%s = %d, want %d", tt.key, wire[tt.key], tt.value)
		}
	}
}

func TestP3cCompareMaterialRatioIgnoresArgumentOrderAndNormalMode(t *testing.T) {
	for _, mode := range []string{"dense", "packed"} {
		atlas, packed := p3cMaterialPair(t, mode)
		for _, pair := range [][2]report{{atlas, packed}, {packed, atlas}} {
			ratio, err := compareReports(pair[0], pair[1])
			if err != nil || ratio == nil || *ratio != 1.5 {
				t.Errorf("%s normals ratio=%v error=%v, want packed-material/atlas=1.5", mode, ratio, err)
			}
		}
	}
}

func TestP3cCompareNormalAxisWithEitherMaterialPolicy(t *testing.T) {
	for _, materials := range []string{"atlas", "packed"} {
		a, b := p3cReport(t, 2, "dense", materials), p3cReport(t, 2, "packed", materials)
		for _, pair := range [][2]report{{a, b}, {b, a}} {
			ratio, err := compareReports(pair[0], pair[1])
			if err != nil || ratio == nil || *ratio != .5 {
				t.Errorf("%s materials ratio=%v error=%v, want packed-normal/dense-normal=0.5", materials, ratio, err)
			}
		}
	}
}

func TestP3cCompareRejectsInvalidPolicyAxes(t *testing.T) {
	tests := []struct {
		name       string
		versionA   int
		versionB   int
		modeA      string
		modeB      string
		materialsA string
		materialsB string
	}{
		{"same policies", 2, 2, "dense", "dense", "atlas", "atlas"},
		{"both policies differ", 2, 2, "dense", "packed", "atlas", "packed"},
		{"missing v2 material", 2, 2, "dense", "packed", "", "atlas"},
		{"both missing v2 material", 2, 2, "dense", "packed", "", ""},
		{"invalid material", 2, 2, "dense", "packed", "atlas", "dense"},
		{"case-sensitive material", 2, 2, "dense", "packed", "atlas", "Atlas"},
		{"invalid normal on material axis", 2, 2, "automatic", "automatic", "atlas", "packed"},
		{"mixed report versions", 1, 2, "dense", "packed", "", "atlas"},
		{"unsupported version zero", 0, 0, "dense", "packed", "atlas", "atlas"},
		{"unsupported version three", 3, 3, "dense", "packed", "atlas", "atlas"},
		{"v1 cannot declare packed materials", 1, 1, "dense", "packed", "", "packed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := p3cReport(t, tt.versionA, tt.modeA, tt.materialsA)
			b := p3cReport(t, tt.versionB, tt.modeB, tt.materialsB)
			for _, pair := range [][2]report{{a, b}, {b, a}} {
				ratio, err := compareReports(pair[0], pair[1])
				if err == nil || ratio != nil {
					t.Errorf("ratio=%v error=%v, want rejected policy axes", ratio, err)
				}
			}
		})
	}
}

func TestP3cCompareLegacyVersionOneMissingMaterialsMeansAtlas(t *testing.T) {
	for _, materials := range []string{"", "atlas"} {
		a, b := p3cReport(t, 1, "dense", ""), p3cReport(t, 1, "packed", materials)
		for _, pair := range [][2]report{{a, b}, {b, a}} {
			ratio, err := compareReports(pair[0], pair[1])
			if err != nil || ratio == nil || *ratio != .5 {
				t.Fatalf("legacy reports materials=%q ratio=%v error=%v, want 0.5", materials, ratio, err)
			}
		}
	}
}

func TestP3cCompareMaterialAxisRetainsStrictEvidenceValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*report)
	}{
		{"version", func(r *report) { r.Version++ }},
		{"adapter", func(r *report) { r.Adapter = "other" }},
		{"backend", func(r *report) { r.Backend = "other" }},
		{"workload", func(r *report) { r.Settings.Workload = "dense" }},
		{"width", func(r *report) { r.Settings.Width++ }},
		{"height", func(r *report) { r.Settings.Height++ }},
		{"samples", func(r *report) { r.Settings.Samples++ }},
		{"warmup", func(r *report) { r.Settings.Warmup++ }},
		{"batch", func(r *report) { r.Settings.Batch++ }},
		{"fixture", func(r *report) { r.Fixture = "other" }},
		{"shader", func(r *report) { r.Shader = "other" }},
		{"timing method", func(r *report) { r.GPUTimingMethod = "other" }},
		{"query cohort size", func(r *report) { r.QueryCohortSize++ }},
		{"query cohort count", func(r *report) { r.QueryCohortCount++ }},
		{"query max count", func(r *report) { r.QueryMaxCount++ }},
		{"initial geometry", func(r *report) { r.InitialGeometry.ContentSHA256 = "other" }},
		{"final geometry", func(r *report) { r.FinalGeometry.OccupiedVoxels++ }},
		{"initial depth", func(r *report) { r.InitialCapture.Depth = "other" }},
		{"initial normal", func(r *report) { r.InitialCapture.Normal = "other" }},
		{"initial material", func(r *report) { r.InitialCapture.Material = "other" }},
		{"initial hits", func(r *report) { r.InitialCapture.HitPixels++ }},
		{"no initial hits", func(r *report) { r.InitialCapture.HitPixels = 0 }},
		{"final depth", func(r *report) { r.FinalCapture.Depth = "other" }},
		{"final normal", func(r *report) { r.FinalCapture.Normal = "other" }},
		{"final material", func(r *report) { r.FinalCapture.Material = "other" }},
		{"final hits", func(r *report) { r.FinalCapture.HitPixels++ }},
		{"no final hits", func(r *report) { r.FinalCapture.HitPixels = 0 }},
		{"raw count", func(r *report) { r.RawTicks = append(r.RawTicks, 400, 410) }},
		{"missing raw", func(r *report) { r.RawTicks = nil }},
		{"invalid raw", func(r *report) { r.RawTicks[1] = r.RawTicks[0] }},
		{"wrong unit", func(r *report) { r.GPUUnit = "nanoseconds" }},
		{"missing summary", func(r *report) { r.GPU = nil }},
		{"summary count", func(r *report) { r.GPU.Count++ }},
		{"summary median", func(r *report) { r.GPU.MedianTicks++ }},
		{"summary p95", func(r *report) { r.GPU.P95Ticks++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, b := p3cMaterialPair(t, "dense")
			tt.mutate(&b)
			for _, pair := range [][2]report{{a, b}, {b, a}} {
				ratio, err := compareReports(pair[0], pair[1])
				if err == nil || ratio != nil {
					t.Errorf("ratio=%v error=%v, want rejected evidence", ratio, err)
				}
			}
		})
	}
}

func TestP3cCompareMaterialAxisUnavailableTimingHasNoRatio(t *testing.T) {
	for _, mode := range []string{"dense", "packed"} {
		for _, unavailable := range []int{0, 1, 2} {
			a, b := p3cMaterialPair(t, mode)
			if unavailable != 1 {
				a.GPUAvailable, a.GPU, a.RawTicks = false, nil, nil
			}
			if unavailable != 0 {
				b.GPUAvailable, b.GPU, b.RawTicks = false, nil, nil
			}
			for _, pair := range [][2]report{{a, b}, {b, a}} {
				ratio, err := compareReports(pair[0], pair[1])
				if err != nil || ratio != nil {
					t.Errorf("%s unavailable=%d ratio=%v error=%v, want no ratio", mode, unavailable, ratio, err)
				}
			}
		}
	}
}
