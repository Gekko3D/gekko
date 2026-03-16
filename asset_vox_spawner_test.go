package gekko

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestExtractVoxHierarchyKeepsSignedScaleFromRotation(t *testing.T) {
	rotationByte := byte(0)
	localRot := mgl32.QuatIdent()
	localScale := mgl32.Vec3{1, 1, 1}
	foundSignedScale := false
	for i := 0; i < 256; i++ {
		rot, scale := decodeVoxRotation(byte(i))
		if scale != (mgl32.Vec3{1, 1, 1}) {
			rotationByte = byte(i)
			localRot = rot
			localScale = scale
			foundSignedScale = true
			break
		}
	}
	if !foundSignedScale {
		t.Fatal("expected at least one MagicaVoxel rotation byte to decompose to a signed scale")
	}

	vox := &VoxFile{
		Models: []VoxModel{
			{SizeX: 3, SizeY: 5, SizeZ: 7},
		},
		Nodes: map[int]VoxNode{
			0: {
				ID:      0,
				Type:    VoxNodeTransform,
				ChildID: 1,
				Frames: []VoxTransformFrame{
					{Rotation: 0},
				},
			},
			1: {
				ID:      1,
				Type:    VoxNodeTransform,
				ChildID: 2,
				Frames: []VoxTransformFrame{
					{
						LocalTrans: [3]float32{12, 8, -4},
						Rotation:   rotationByte,
					},
				},
			},
			2: {
				ID:   2,
				Type: VoxNodeShape,
				Models: []VoxShapeModel{
					{ModelID: 0},
				},
			},
		},
	}

	instances := ExtractVoxHierarchy(vox, 1.0)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}

	got := instances[0].Transform
	localPos := mgl32.Vec3{12, 8, -4}.Mul(VoxelSize)

	if !vec3ApproxEqual(got.Position, localPos, 1e-5) {
		t.Fatalf("expected position %v, got %v", localPos, got.Position)
	}
	if !vec3ApproxEqual(got.Scale, localScale, 1e-5) {
		t.Fatalf("expected scale %v, got %v", localScale, got.Scale)
	}
	if !quatApproxEqual(got.Rotation, localRot, 1e-5) {
		t.Fatalf("expected rotation %v, got %v", localRot, got.Rotation)
	}
}

func vec3ApproxEqual(a, b mgl32.Vec3, eps float32) bool {
	return a.Sub(b).Len() <= eps
}

func quatApproxEqual(a, b mgl32.Quat, eps float32) bool {
	if quatDistance(a, b) <= eps {
		return true
	}
	return quatDistance(a, mgl32.Quat{W: -b.W, V: b.V.Mul(-1)}) <= eps
}

func quatDistance(a, b mgl32.Quat) float32 {
	dw := a.W - b.W
	dv := a.V.Sub(b.V)
	return float32(math.Sqrt(float64(dw*dw + dv.LenSqr())))
}
