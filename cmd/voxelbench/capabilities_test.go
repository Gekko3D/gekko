package main

import (
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
)

func TestRequiredPassTimestampFeaturesFailsClosed(t *testing.T) {
	for _, tt := range []struct {
		name     string
		features map[string]wgpu.FeatureName
	}{
		{"none", nil},
		{"encoder only", map[string]wgpu.FeatureName{"native-feature-timestamp-query-inside-encoders": wgpu.FeatureName(123456)}},
		{"irrelevant features", map[string]wgpu.FeatureName{"unrelated-feature": wgpu.FeatureNameTimestampQuery}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := requiredPassTimestampFeatures(tt.features); got != nil {
				t.Fatalf("missing pass timestamp capability accepted: %v", got)
			}
		})
	}
}

func TestRequiredPassTimestampFeaturesForwardsOnlyDiscoveredBasicValue(t *testing.T) {
	// The map contains discovered enum values; feature names determine support.
	// An arbitrary value catches replacing discovery with a hardcoded enum.
	basic := wgpu.FeatureName(123456)
	for _, withEncoder := range []bool{false, true} {
		name := "basic only"
		features := map[string]wgpu.FeatureName{"timestamp-query": basic}
		if withEncoder {
			name = "basic and encoder"
			features["native-feature-timestamp-query-inside-encoders"] = wgpu.FeatureName(654321)
		}
		t.Run(name, func(t *testing.T) {
			got := requiredPassTimestampFeatures(features)
			if len(got) != 1 || got[0] != basic {
				t.Fatalf("required features=%v, want only discovered basic value %d", got, basic)
			}
		})
	}
}
