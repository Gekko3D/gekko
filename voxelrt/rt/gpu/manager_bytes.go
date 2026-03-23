package gpu

import (
	"encoding/binary"
	"math"
)

func mat4ToBytes(m [16]float32) []byte {
	buf := make([]byte, 64)
	for i, v := range m {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func vec3ToBytesPadded(v [3]float32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(v[2]))
	return buf
}

func int3ToBytesPadded(v [3]int32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(v[0]))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(v[1]))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(v[2]))
	return buf
}

func rgbaToVec4(c [4]uint8) []byte {
	buf := make([]byte, 16)
	for i, v := range c {
		f := float32(v) / 255.0
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

func float32ToBytes(ff float32) []byte {
	bits := math.Float32bits(ff)
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, bits)
	return buf
}

func vec4ToBytes(v [4]float32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], math.Float32bits(v[0]))
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(v[1]))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(v[2]))
	binary.LittleEndian.PutUint32(buf[12:16], math.Float32bits(v[3]))
	return buf
}

func uvec4ToBytes(v [4]uint32) []byte {
	buf := make([]byte, 16)
	binary.LittleEndian.PutUint32(buf[0:4], v[0])
	binary.LittleEndian.PutUint32(buf[4:8], v[1])
	binary.LittleEndian.PutUint32(buf[8:12], v[2])
	binary.LittleEndian.PutUint32(buf[12:16], v[3])
	return buf
}
