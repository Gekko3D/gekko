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

	objectToWorld mgl32.Mat4
	worldToObject mgl32.Mat4
	matricesValid bool
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

func (t *Transform) updateMatrices() {
	if t == nil {
		return
	}
	if t.matricesValid && !t.Dirty {
		return
	}

	// M = T * R * S * PivotTranslate
	translate := mgl32.Translate3D(t.Position.X(), t.Position.Y(), t.Position.Z())
	rotate := t.Rotation.Mat4()
	scale := mgl32.Scale3D(t.Scale.X(), t.Scale.Y(), t.Scale.Z())

	// The pivot naturally offsets the object within its own local space before it gets oriented/placed.
	pivotTranslate := mgl32.Translate3D(-t.Pivot.X(), -t.Pivot.Y(), -t.Pivot.Z())

	// inv(M) = inv(PivotTranslate) * inv(S) * inv(R) * inv(T)
	// Since we know component matrices, we can invert them cheaply.
	invPivotTranslate := mgl32.Translate3D(t.Pivot.X(), t.Pivot.Y(), t.Pivot.Z())
	invScale := mgl32.Scale3D(1.0/t.Scale.X(), 1.0/t.Scale.Y(), 1.0/t.Scale.Z())
	invRotate := t.Rotation.Conjugate().Mat4()
	invTranslate := mgl32.Translate3D(-t.Position.X(), -t.Position.Y(), -t.Position.Z())

	t.objectToWorld = translate.Mul4(rotate).Mul4(scale).Mul4(pivotTranslate)
	t.worldToObject = invPivotTranslate.Mul4(invScale).Mul4(invRotate).Mul4(invTranslate)
	t.matricesValid = true
}

func (t *Transform) ObjectToWorld() mgl32.Mat4 {
	t.updateMatrices()
	return t.objectToWorld
}

func (t *Transform) WorldToObject() mgl32.Mat4 {
	t.updateMatrices()
	return t.worldToObject
}
