package gekko

import (
	"encoding/binary"
	"math"
	"testing"

	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/go-gl/mathgl/mgl32"
)

func TestAudioPCMConversionAndSpatialGain(t *testing.T) {
	pcm, err := decodePCMWAV(testPCM16WAV(11025, 1, 8, []byte{0, 255}), 22050)
	if err != nil {
		t.Fatal(err)
	}
	want := []int16{-32768, -128, 32512, 32512}
	if len(pcm) != len(want)*2 {
		t.Fatalf("resampled PCM length = %d, want %d", len(pcm), len(want)*2)
	}
	for i, expected := range want {
		got := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		if got != expected {
			t.Fatalf("resampled sample %d = %d, want %d", i, got, expected)
		}
	}

	for _, test := range []struct {
		name     string
		distance float32
		want     float32
	}{
		{name: "near", distance: 1, want: 0.8},
		{name: "middle", distance: 5, want: 0.4},
		{name: "far", distance: 9, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := spatialAudioGain(test.distance, 1, 9, 0.8); float32(math.Abs(float64(got-test.want))) > 1e-6 {
				t.Fatalf("gain = %f, want %f", got, test.want)
			}
		})
	}

	wall := testRaycastVoxelObjectAt(mgl32.Vec3{0, 0, 4})
	voxRt := &VoxelRtState{
		RtApp:          &app_rt.App{Scene: core.NewScene()},
		instanceMap:    map[EntityId]*core.VoxelObject{10: wall},
		objectToEntity: map[*core.VoxelObject]EntityId{wall: 10},
	}
	voxRt.RtApp.Scene.AddObject(wall)
	if !audioSourceOccluded(nil, voxRt, 1, mgl32.Vec3{}, AudioPlayback{Position: mgl32.Vec3{0, 0, 8}, Source: 2}) {
		t.Fatal("expected wall between listener and source to occlude sound")
	}
}

func testPCM16WAV(sampleRate, channels, bits int, samples []byte) []byte {
	data := make([]byte, 44+len(samples))
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(data[24:28], uint32(sampleRate))
	bytesPerFrame := channels * bits / 8
	binary.LittleEndian.PutUint32(data[28:32], uint32(sampleRate*bytesPerFrame))
	binary.LittleEndian.PutUint16(data[32:34], uint16(bytesPerFrame))
	binary.LittleEndian.PutUint16(data[34:36], uint16(bits))
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(len(samples)))
	copy(data[44:], samples)
	return data
}
