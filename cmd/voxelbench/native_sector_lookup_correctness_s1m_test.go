package main

import (
	"os"
	"testing"
)

// Reuse production G-buffer rendering, readbacks, actual submissions and resize
// from S1l14 with bounded lookup enabled on the sealed managed fixture. Normal,
// material and hit-mask parity are exact; only finite hit depth uses the existing
// eight-ULP AND 1e-4-world-unit tolerance. This is no performance measurement.
func TestNativeSectorLookupS1mCorrectness(t *testing.T) {
	if os.Getenv("GEKKO_NATIVE_S1M") != "1" {
		t.Skip("set GEKKO_NATIVE_S1M=1 for real-device bounded lookup correctness")
	}
	nativeManagedPublicationCorrectness(t, true)
}
