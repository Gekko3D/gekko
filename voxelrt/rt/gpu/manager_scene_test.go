package gpu

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func floatAt(buf []byte, offset int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(buf[offset:]))
}

func TestBuildCameraUniformDataPacksExpectedMatrices(t *testing.T) {
	viewProj := mgl32.Ident4().Mul4(mgl32.Scale3D(2, 3, 4))
	invView := mgl32.Translate3D(5, 6, 7)
	invProj := mgl32.Scale3D(8, 9, 10)

	buf := buildCameraUniformData(
		viewProj,
		invView,
		invProj,
		mgl32.Vec3{1, 2, 3},
		mgl32.Vec3{4, 5, 6},
		mgl32.Vec3{0.1, 0.2, 0.3},
		0.7,
		11,
		12,
		13,
		1280,
		720,
		core.DefaultLightingQualityConfig(),
	)

	if got := floatAt(buf, 0); got != viewProj[0] {
		t.Fatalf("expected viewProj[0] at offset 0, got %v", got)
	}
	if got := floatAt(buf, 64); got != invView[0] {
		t.Fatalf("expected invView[0] at offset 64, got %v", got)
	}
	if got := floatAt(buf, 128); got != invProj[0] {
		t.Fatalf("expected invProj[0] at offset 128, got %v", got)
	}
	if got := floatAt(buf, 236); got != 0.7 {
		t.Fatalf("expected sky ambient mix at offset 236, got %v", got)
	}
	if got := binary.LittleEndian.Uint32(buf[244:]); got != 12 {
		t.Fatalf("expected render mode 12 at offset 244, got %d", got)
	}
}
