package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestP3cMaterialsHelpWithoutGPU(t *testing.T) {
	for _, flag := range []string{"-h", "-help"} {
		var output bytes.Buffer
		if err := run([]string{flag}, &output); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		for _, want := range []string{"-materials", "atlas", "packed", `(default "atlas")`} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("%s help missing %q: %s", flag, want, output.String())
			}
		}
	}
}

func TestP3cMaterialsAndNormalPoliciesParseIndependentlyBeforeGPU(t *testing.T) {
	for _, mode := range []string{"dense", "packed"} {
		for _, materials := range []string{"atlas", "packed"} {
			var output bytes.Buffer
			// A deliberately invalid workload stops execution after policy
			// validation, without invoking a valid native/GPU benchmark.
			err := run([]string{"-mode", mode, "-materials", materials, "-workload", "invalid"}, &output)
			if err == nil || !strings.Contains(err.Error(), "workload") {
				t.Errorf("mode=%s materials=%s error=%v, want accepted policies then workload rejection", mode, materials, err)
			}
		}
	}
}

func TestP3cMaterialsRejectInvalidPolicyBeforeGPU(t *testing.T) {
	for _, policy := range []string{"", "dense", "automatic", "Atlas", "PACKED"} {
		t.Run(policy, func(t *testing.T) {
			var output bytes.Buffer
			err := run([]string{"-materials", policy}, &output)
			if err == nil {
				t.Fatal("invalid material storage accepted")
			}
			// An unknown-flag error is not policy validation. Require the valid
			// choices, which also establishes that validation preceded GPU setup.
			for _, want := range []string{"materials", "atlas", "packed"} {
				if !strings.Contains(strings.ToLower(err.Error()), want) {
					t.Errorf("error = %q, want material policy diagnostic containing %q", err, want)
				}
			}
		})
	}
}
