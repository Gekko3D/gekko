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
		0.8,
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
	if got := floatAt(buf, 220); got != 0.7 {
		t.Fatalf("expected sun intensity at offset 220, got %v", got)
	}
	if got := floatAt(buf, 236); got != 0.8 {
		t.Fatalf("expected sky ambient mix at offset 236, got %v", got)
	}
	if got := binary.LittleEndian.Uint32(buf[244:]); got != 12 {
		t.Fatalf("expected render mode 12 at offset 244, got %d", got)
	}
	if got := floatAt(buf, 280); got != 0.65 {
		t.Fatalf("expected directional shadow softness 0.65 at offset 280, got %v", got)
	}
	if got := floatAt(buf, 284); got != 0.4 {
		t.Fatalf("expected spot shadow softness 0.4 at offset 284, got %v", got)
	}
}

func TestBuildInstanceDataKeepsBinaryLayout(t *testing.T) {
	obj := core.NewVoxelObject()
	obj.XBrickMap.SetVoxel(0, 0, 0, 1)
	obj.WorldAABB = &[2]mgl32.Vec3{{1, 2, 3}, {4, 5, 6}}

	buf := buildInstanceData([]*core.VoxelObject{obj})
	if len(buf) != 208 {
		t.Fatalf("expected 208-byte instance record, got %d", len(buf))
	}
	if got := floatAt(buf, 128); got != 1 {
		t.Fatalf("expected world AABB min x at offset 128, got %v", got)
	}
	if got := floatAt(buf, 144); got != 4 {
		t.Fatalf("expected world AABB max x at offset 144, got %v", got)
	}
	if got := binary.LittleEndian.Uint32(buf[192:196]); got != 0 {
		t.Fatalf("expected instance id 0 at offset 192, got %d", got)
	}
}

func TestBuildLightsDataUsesStableRecordSize(t *testing.T) {
	light := core.Light{
		Position:   [4]float32{1, 2, 3, 4},
		Direction:  [4]float32{5, 6, 7, 8},
		Color:      [4]float32{9, 10, 11, 12},
		Params:     [4]float32{13, 14, 15, 16},
		ShadowMeta: [4]uint32{17, 18, 19, 20},
	}
	buf := buildLightsData([]core.Light{light})
	if len(buf) != lightSizeBytes {
		t.Fatalf("expected %d-byte light record, got %d", lightSizeBytes, len(buf))
	}
	if got := floatAt(buf, 0); got != 1 {
		t.Fatalf("expected position x at offset 0, got %v", got)
	}
	if got := floatAt(buf, 12); got != 4 {
		t.Fatalf("expected source radius in position.w at offset 12, got %v", got)
	}
	if got := binary.LittleEndian.Uint32(buf[64:68]); got != 17 {
		t.Fatalf("expected shadow meta x at offset 64, got %d", got)
	}
	if got := binary.LittleEndian.Uint32(buf[76:80]); got != 20 {
		t.Fatalf("expected emitter link in shadow meta w at offset 76, got %d", got)
	}
}
