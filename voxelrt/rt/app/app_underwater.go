package app

import (
	"encoding/binary"
	"math"

	"github.com/cogentcore/webgpu/wgpu"
)

const underwaterParamsSize = 48

// UnderwaterInput is a camera-local medium applied by the final scene resolve.
type UnderwaterInput struct {
	Color              [3]float32
	AbsorptionColor    [3]float32
	Strength           float32
	DistortionPixels   float32
	NearSurface        float32
	ScatteringStrength float32
	Time               float32
}

func (a *App) ApplyUnderwaterInput(input UnderwaterInput) {
	if a == nil {
		return
	}
	if input.Strength < 0 {
		input.Strength = 0
	}
	if input.Strength > 1 {
		input.Strength = 1
	}
	a.ensureUnderwaterParamsBuffer()
	if a.UnderwaterParamsBuf == nil || a.Queue == nil {
		return
	}
	values := [12]float32{
		input.Color[0], input.Color[1], input.Color[2], input.Strength,
		input.AbsorptionColor[0], input.AbsorptionColor[1], input.AbsorptionColor[2], input.DistortionPixels,
		input.Time, input.NearSurface, input.ScatteringStrength,
	}
	data := make([]byte, underwaterParamsSize)
	for i, value := range values {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(value))
	}
	a.Queue.WriteBuffer(a.UnderwaterParamsBuf, 0, data)
}

func (a *App) ClearUnderwaterInput() {
	a.ApplyUnderwaterInput(UnderwaterInput{})
}

func (a *App) ensureUnderwaterParamsBuffer() {
	if a == nil || a.UnderwaterParamsBuf != nil || a.Device == nil {
		return
	}
	buffer, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Underwater Params",
		Size:  underwaterParamsSize,
		Usage: wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err == nil {
		a.UnderwaterParamsBuf = buffer
		if a.Queue != nil {
			a.Queue.WriteBuffer(buffer, 0, make([]byte, underwaterParamsSize))
		}
	}
}
