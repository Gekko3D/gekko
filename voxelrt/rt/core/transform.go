package core

import (
	"github.com/go-gl/mathgl/mgl32"
)

type Transform struct {
	Position mgl32.Vec3
	Rotation mgl32.Quat
	Scale    mgl32.Vec3
	Pivot    mgl32.Vec3
	Dirty    bool
}

func NewTransform() *Transform {
	return &Transform{
		Position: mgl32.Vec3{0, 0, 0},
		Rotation: mgl32.QuatIdent(),
		Scale:    mgl32.Vec3{1, 1, 1},
		Pivot:    mgl32.Vec3{0, 0, 0},
		Dirty:    true,
	}
}

func (t *Transform) ObjectToWorld() mgl32.Mat4 {
	// M = T * R * S * PivotTranslate
	translate := mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z())
	rotate := t.Rotation.Mat4()
	scale := mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z())

	// The pivot naturally offsets the object within its own local space before it gets oriented/placed.
	pivotTranslate := mgl32.Translate3D(-t.Pivot.X(), -t.Pivot.Y(), -t.Pivot.Z())

	return translate.Mul4(rotate).Mul4(scale).Mul4(pivotTranslate)
}

func (t *Transform) WorldToObject() mgl32.Mat4 {
	// inv(M) = inv(PivotTranslate) * inv(S) * inv(R) * inv(T)
	// Since we know component matrices, we can invert them cheaply.

	invPivotTranslate := mgl32.Translate3D(t.Pivot.X(), t.Pivot.Y(), t.Pivot.Z())

	// Inverse Scale
	invScale := mgl32.Scale3D(1.0/t.Scale.X(), 1.0/t.Scale.Y(), 1.0/t.Scale.Z())

	// Inverse Rotation: Conjugate/Transpose for unit quat
	invRotate := t.Rotation.Conjugate().Mat4()

	// Inverse Translate
	invTranslate := mgl32.Translate3D(-t.Position.X(), -t.Position.Y(), -t.Position.Z())

	return invPivotTranslate.Mul4(invScale).Mul4(invRotate).Mul4(invTranslate)
}
