package content

import (
	"testing"
)

func i07RatioManifest(spacing, resolution float32) *TerrainChunkManifestDef {
	d := i07ReviewManifest()
	d.Pages[0].Payload.ChunkSize = 1
	d.Pages[0].Payload.SampleSpacing = spacing
	d.Pages[0].Payload.VoxelResolution = resolution
	d.Pages[0].Payload.PayloadSizeBytes = 2
	d.Pages[0].BoundsMax = [3]float32{spacing, 90, spacing}
	return d
}

func TestI07RatioReviewLargeNonIntegralVisualGridRejected(t *testing.T) {
	for _, spacing := range []float32{3145727, 1 << 30} {
		d := i07RatioManifest(spacing, 3)
		if index, err := ValidateTerrainPageManifest(d); err == nil || index != nil {
			t.Errorf("nonintegral spacing/resolution ratio %g/3 accepted", spacing)
		}
	}
}

func TestI07RatioReviewExactLargeAndDecimalVisualGridsAccepted(t *testing.T) {
	for _, pair := range [][2]float32{{3145728, 3}, {.3, .1}} {
		if _, err := ValidateTerrainPageManifest(i07RatioManifest(pair[0], pair[1])); err != nil {
			t.Errorf("valid whole visual grid %g/%g rejected: %v", pair[0], pair[1], err)
		}
	}
}
