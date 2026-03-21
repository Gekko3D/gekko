package gekko

import (
	"math"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestDecodeVoxRotationMatchesDotVoxMatrixSemantics(t *testing.T) {
	for i := 0; i < 256; i++ {
		r := byte(i)
		indexNZ1 := int(r & 3)
		indexNZ2 := int((r >> 2) & 3)
		if indexNZ1 == indexNZ2 || indexNZ1 == 3 || indexNZ2 == 3 {
			continue
		}

		rot, scale := decodeVoxRotation(r)
		got := rot.Normalize().Mat4().Mul4(mgl32.Scale3D(scale.X(), scale.Y(), scale.Z())).Mat3()
		want := referenceVoxRotationMatrix(r)

		if !mat3ApproxEqual(got, want, 1e-5) {
			t.Fatalf("rotation byte %d decoded to\n%v\nwant\n%v", i, got, want)
		}
	}
}

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

func mat3ApproxEqual(a, b mgl32.Mat3, eps float32) bool {
	for c := 0; c < 3; c++ {
		ac := a.Col(c)
		bc := b.Col(c)
		if ac.Sub(bc).Len() > eps {
			return false
		}
	}
	return true
}

func referenceVoxRotationMatrix(r byte) mgl32.Mat3 {
	indexNZ1 := int(r & 3)
	indexNZ2 := int((r >> 2) & 3)
	indexNZ3 := 3 - indexNZ1 - indexNZ2

	signs := [3]float32{1, 1, 1}
	if r&(1<<4) != 0 {
		signs[0] = -1
	}
	if r&(1<<5) != 0 {
		signs[1] = -1
	}
	if r&(1<<6) != 0 {
		signs[2] = -1
	}

	colsVox := [3]mgl32.Vec3{}
	colsVox[indexNZ1][0] = signs[0]
	colsVox[indexNZ2][1] = signs[1]
	colsVox[indexNZ3][2] = signs[2]
	matVox := mgl32.Mat3FromCols(colsVox[0], colsVox[1], colsVox[2])

	basisSwap := mgl32.Mat3FromCols(
		mgl32.Vec3{1, 0, 0},
		mgl32.Vec3{0, 0, 1},
		mgl32.Vec3{0, 1, 0},
	)
	return basisSwap.Mul3(matVox).Mul3(basisSwap)
}
