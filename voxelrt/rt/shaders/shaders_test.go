package shaders

import (
	"strings"
	"testing"
)

func TestBillboardShadersApplyWebGPUClipZConversion(t *testing.T) {
	required := []string{
		"clip_pos.z = clip_pos.z * 0.5 + clip_pos.w * 0.5;",
		"out.position = raster_clip_pos(world_pos);",
	}

	for _, tc := range []struct {
		name string
		code string
	}{
		{name: "particles", code: ParticlesBillboardWGSL},
		{name: "sprites", code: SpritesWGSL},
	} {
		for _, needle := range required {
			if !strings.Contains(tc.code, needle) {
				t.Fatalf("%s shader missing reverse-z raster clip conversion: %q", tc.name, needle)
			}
		}
	}
}
