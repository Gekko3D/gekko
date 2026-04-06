package gekko

import (
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

// WaterSurfaceComponent describes a horizontal stylized water body rendered by
// the dedicated water surface accumulation pass.
type WaterSurfaceComponent struct {
	Disabled bool

	HalfExtents [2]float32
	Depth       float32

	Color           [3]float32
	AbsorptionColor [3]float32

	Opacity    float32
	Roughness  float32
	Refraction float32

	FlowDirection [2]float32
	FlowSpeed     float32
	WaveAmplitude float32
}

func (w *WaterSurfaceComponent) Enabled() bool {
	if w == nil || w.Disabled {
		return false
	}
	ext := w.NormalizedHalfExtents()
	return ext[0] > 0 && ext[1] > 0 && w.NormalizedDepth() > 0
}

func (w *WaterSurfaceComponent) NormalizedHalfExtents() [2]float32 {
	if w == nil {
		return [2]float32{}
	}
	ext := w.HalfExtents
	if ext[0] < 0 {
		ext[0] = 0
	}
	if ext[1] < 0 {
		ext[1] = 0
	}
	return ext
}

func (w *WaterSurfaceComponent) NormalizedDepth() float32 {
	if w == nil || w.Depth <= 0 {
		return 0
	}
	return w.Depth
}

func (w *WaterSurfaceComponent) NormalizedColor() [3]float32 {
	if w == nil {
		return [3]float32{0.14, 0.45, 0.82}
	}
	color := w.Color
	if color == ([3]float32{}) {
		color = [3]float32{0.14, 0.45, 0.82}
	}
	for i := range color {
		color[i] = clampWaterFloat(color[i], 0, 1)
	}
	return color
}

func (w *WaterSurfaceComponent) NormalizedAbsorptionColor() [3]float32 {
	if w == nil {
		return [3]float32{0.22, 0.44, 0.78}
	}
	color := w.AbsorptionColor
	if color == ([3]float32{}) {
		color = [3]float32{0.22, 0.44, 0.78}
	}
	for i := range color {
		color[i] = clampWaterFloat(color[i], 0, 4)
	}
	return color
}

func (w *WaterSurfaceComponent) NormalizedOpacity() float32 {
	if w == nil || w.Opacity <= 0 {
		return 0.68
	}
	return clampWaterFloat(w.Opacity, 0.05, 0.98)
}

func (w *WaterSurfaceComponent) NormalizedRoughness() float32 {
	if w == nil || w.Roughness <= 0 {
		return 0.16
	}
	return clampWaterFloat(w.Roughness, 0.02, 1.0)
}

func (w *WaterSurfaceComponent) NormalizedRefraction() float32 {
	if w == nil || w.Refraction <= 0 {
		return 0.22
	}
	return clampWaterFloat(w.Refraction, 0.0, 1.0)
}

func (w *WaterSurfaceComponent) NormalizedFlowDirection() [2]float32 {
	if w == nil {
		return [2]float32{1, 0}
	}
	dir := w.FlowDirection
	length := math.Hypot(float64(dir[0]), float64(dir[1]))
	if length < 1e-6 {
		return [2]float32{1, 0}
	}
	inv := float32(1.0 / length)
	return [2]float32{dir[0] * inv, dir[1] * inv}
}

func (w *WaterSurfaceComponent) NormalizedFlowSpeed() float32 {
	if w == nil || w.FlowSpeed <= 0 {
		return 0.9
	}
	return clampWaterFloat(w.FlowSpeed, 0.05, 8.0)
}

func (w *WaterSurfaceComponent) NormalizedWaveAmplitude() float32 {
	if w == nil || w.WaveAmplitude <= 0 {
		return 0.025
	}
	return clampWaterFloat(w.WaveAmplitude, 0.0, 0.15)
}

func (w *WaterSurfaceComponent) WorldCenter(tr *TransformComponent) mgl32.Vec3 {
	if tr == nil {
		return mgl32.Vec3{}
	}
	return tr.Position
}

func (w *WaterSurfaceComponent) WorldHalfExtents(tr *TransformComponent) [2]float32 {
	ext := w.NormalizedHalfExtents()
	if tr == nil {
		return ext
	}
	return [2]float32{
		ext[0] * absWaterFloat(tr.Scale.X()),
		ext[1] * absWaterFloat(tr.Scale.Z()),
	}
}

func (w *WaterSurfaceComponent) WorldDepth(tr *TransformComponent) float32 {
	depth := w.NormalizedDepth()
	if tr == nil {
		return depth
	}
	return depth * absWaterFloat(tr.Scale.Y())
}

func clampWaterFloat(v, minV, maxV float32) float32 {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func absWaterFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	if v == 0 {
		return 1
	}
	return v
}
