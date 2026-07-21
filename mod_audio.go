package gekko

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/ebitengine/oto/v3"
	"github.com/go-gl/mathgl/mgl32"
)

const (
	defaultAudioSampleRate       = 44100
	defaultAudioMinDistance      = float32(1)
	defaultAudioMaxDistance      = float32(40)
	defaultAudioOcclusionVolume  = float32(0.25)
	defaultAudioMaxVoices        = 64
	defaultAudioMaxQueuedSounds  = 256
	defaultAudioRaycastEndMargin = float32(0.05)
)

// AudioModule installs cached one-shot PCM WAV playback. Occlusion requires a
// VoxelRtModule in the same app; distance falloff works without it.
type AudioModule struct {
	SampleRate int
	Occlusion  bool
}

// AudioPlayback is a source-neutral request for a short sound effect.
type AudioPlayback struct {
	Path            string
	Position        mgl32.Vec3
	Volume          float32
	MinDistance     float32
	MaxDistance     float32
	OcclusionVolume float32
	Source          EntityId
	Spatial         bool
}

// AudioState owns the process-wide Oto context, decoded clip cache, and the
// short queue consumed during PreRender.
type AudioState struct {
	context    *oto.Context
	ready      <-chan struct{}
	readyNow   bool
	sampleRate int
	initErr    error
	queued     []AudioPlayback
	clips      map[string][]byte
	failed     map[string]struct{}
	players    []*oto.Player
}

func (mod AudioModule) Install(app *App, cmd *Commands) {
	sampleRate := mod.SampleRate
	if sampleRate <= 0 {
		sampleRate = defaultAudioSampleRate
	}
	state := &AudioState{
		sampleRate: sampleRate,
		clips:      make(map[string][]byte),
		failed:     make(map[string]struct{}),
	}
	state.context, state.ready, state.initErr = oto.NewContext(&oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: 1,
		Format:       oto.FormatSignedInt16LE,
	})
	if state.initErr != nil {
		log.Printf("audio disabled: %v", state.initErr)
	}
	cmd.AddResources(state)
	if mod.Occlusion {
		app.UseSystem(System(spatialAudioOcclusionSystem).InStage(PreRender).RunAlways())
		return
	}
	app.UseSystem(System(spatialAudioSystem).InStage(PreRender).RunAlways())
}

func (state *AudioState) Err() error {
	if state == nil {
		return errors.New("audio state is nil")
	}
	if state.initErr != nil {
		return state.initErr
	}
	if state.context != nil {
		return state.context.Err()
	}
	return nil
}

// Play queues a one-shot sound. It returns false when the request is invalid
// or the per-frame safety bound is full.
func (state *AudioState) Play(playback AudioPlayback) bool {
	if state == nil || state.context == nil || playback.Path == "" || playback.Volume <= 0 || len(state.queued) >= defaultAudioMaxQueuedSounds {
		return false
	}
	state.queued = append(state.queued, playback)
	return true
}

func spatialAudioSystem(cmd *Commands, state *AudioState) {
	processSpatialAudio(cmd, state, nil)
}

func spatialAudioOcclusionSystem(cmd *Commands, state *AudioState, voxRt *VoxelRtState) {
	processSpatialAudio(cmd, state, voxRt)
}

func processSpatialAudio(cmd *Commands, state *AudioState, voxRt *VoxelRtState) {
	if state == nil || state.context == nil {
		return
	}
	state.prunePlayers()
	if !state.readyNow {
		select {
		case <-state.ready:
			state.readyNow = true
		default:
			return
		}
	}

	listenerEntity, listenerPosition, hasListener := audioListener(cmd)
	queued := state.queued
	state.queued = state.queued[:0]
	for _, playback := range queued {
		gain := clampAudioGain(playback.Volume)
		if playback.Spatial {
			if !hasListener {
				continue
			}
			distance := playback.Position.Sub(listenerPosition).Len()
			gain = spatialAudioGain(distance, playback.MinDistance, playback.MaxDistance, gain)
			if gain > 0 && audioSourceOccluded(cmd, voxRt, listenerEntity, listenerPosition, playback) {
				occlusion := playback.OcclusionVolume
				if occlusion <= 0 {
					occlusion = defaultAudioOcclusionVolume
				}
				gain *= clampAudioGain(occlusion)
			}
		}
		if gain > 0 {
			state.play(playback.Path, gain)
		}
	}
}

func audioListener(cmd *Commands) (EntityId, mgl32.Vec3, bool) {
	if cmd == nil {
		return 0, mgl32.Vec3{}, false
	}
	var entity EntityId
	var position mgl32.Vec3
	MakeQuery1[CameraComponent](cmd).Map(func(eid EntityId, camera *CameraComponent) bool {
		if camera == nil {
			return true
		}
		entity, position = eid, camera.Position
		return false
	})
	return entity, position, entity != 0
}

func audioSourceOccluded(cmd *Commands, voxRt *VoxelRtState, listener EntityId, listenerPosition mgl32.Vec3, playback AudioPlayback) bool {
	if voxRt == nil {
		return false
	}
	delta := playback.Position.Sub(listenerPosition)
	distance := delta.Len()
	if distance <= defaultAudioRaycastEndMargin*2 {
		return false
	}
	hit := voxRt.RaycastFiltered(listenerPosition, delta.Mul(1/distance), distance-defaultAudioRaycastEndMargin, func(entity EntityId, known bool) bool {
		if !known {
			return true
		}
		return !isEntityOrDescendantOf(cmd, entity, listener) && !isEntityOrDescendantOf(cmd, entity, playback.Source)
	})
	return hit.Hit
}

func spatialAudioGain(distance, minDistance, maxDistance, volume float32) float32 {
	volume = clampAudioGain(volume)
	if minDistance <= 0 {
		minDistance = defaultAudioMinDistance
	}
	if maxDistance <= minDistance {
		maxDistance = defaultAudioMaxDistance
	}
	switch {
	case distance <= minDistance:
		return volume
	case distance >= maxDistance:
		return 0
	default:
		return volume * (1 - (distance-minDistance)/(maxDistance-minDistance))
	}
}

func clampAudioGain(value float32) float32 {
	return max(float32(0), min(float32(1), value))
}

func (state *AudioState) prunePlayers() {
	alive := state.players[:0]
	for _, player := range state.players {
		if player != nil && player.IsPlaying() {
			alive = append(alive, player)
		} else if player != nil {
			_ = player.Close()
		}
	}
	state.players = alive
}

func (state *AudioState) play(path string, gain float32) {
	if state == nil || state.context == nil || len(state.players) >= defaultAudioMaxVoices {
		return
	}
	pcm := state.load(path)
	if len(pcm) == 0 {
		return
	}
	player := state.context.NewPlayer(bytes.NewReader(pcm))
	player.SetVolume(float64(clampAudioGain(gain)))
	player.Play()
	state.players = append(state.players, player)
}

func (state *AudioState) load(path string) []byte {
	path = filepath.Clean(path)
	if pcm, ok := state.clips[path]; ok {
		return pcm
	}
	if _, failed := state.failed[path]; failed {
		return nil
	}
	data, err := os.ReadFile(path)
	if err == nil {
		data, err = decodePCMWAV(data, state.sampleRate)
	}
	if err != nil {
		state.failed[path] = struct{}{}
		log.Printf("audio clip %q disabled: %v", path, err)
		return nil
	}
	state.clips[path] = data
	return data
}

type pcmWAVFormat struct {
	channels   int
	sampleRate int
	bits       int
}

func decodePCMWAV(wav []byte, targetRate int) ([]byte, error) {
	if targetRate <= 0 {
		return nil, errors.New("target sample rate must be positive")
	}
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	var format pcmWAVFormat
	var audio []byte
	for offset := 12; offset+8 <= len(wav); {
		chunkSize := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		start := offset + 8
		end := start + chunkSize
		if chunkSize < 0 || end < start || end > len(wav) {
			return nil, errors.New("invalid WAV chunk size")
		}
		switch string(wav[offset : offset+4]) {
		case "fmt ":
			if chunkSize < 16 {
				return nil, errors.New("WAV fmt chunk is too short")
			}
			if binary.LittleEndian.Uint16(wav[start:start+2]) != 1 {
				return nil, errors.New("only PCM WAV files are supported")
			}
			format = pcmWAVFormat{
				channels:   int(binary.LittleEndian.Uint16(wav[start+2 : start+4])),
				sampleRate: int(binary.LittleEndian.Uint32(wav[start+4 : start+8])),
				bits:       int(binary.LittleEndian.Uint16(wav[start+14 : start+16])),
			}
		case "data":
			audio = wav[start:end]
		}
		offset = end + chunkSize%2
	}
	if format.channels < 1 || format.channels > 2 || format.sampleRate <= 0 || format.bits != 8 && format.bits != 16 {
		return nil, fmt.Errorf("unsupported PCM WAV format: %d channels, %d Hz, %d bits", format.channels, format.sampleRate, format.bits)
	}
	bytesPerFrame := format.channels * format.bits / 8
	if len(audio) == 0 || len(audio)%bytesPerFrame != 0 {
		return nil, errors.New("WAV has no complete PCM data frames")
	}
	frames := len(audio) / bytesPerFrame
	mono := make([]int16, frames)
	for frame := range frames {
		sum := 0
		for channel := range format.channels {
			offset := frame*bytesPerFrame + channel*format.bits/8
			if format.bits == 8 {
				sum += (int(audio[offset]) - 128) << 8
			} else {
				sum += int(int16(binary.LittleEndian.Uint16(audio[offset : offset+2])))
			}
		}
		mono[frame] = int16(sum / format.channels)
	}
	resampledFrames := max(1, int(math.Round(float64(frames)*float64(targetRate)/float64(format.sampleRate))))
	pcm := make([]byte, resampledFrames*2)
	for frame := range resampledFrames {
		position := float64(frame) * float64(format.sampleRate) / float64(targetRate)
		low := min(int(position), frames-1)
		high := min(low+1, frames-1)
		fraction := position - float64(low)
		sample := int16(math.Round(float64(mono[low])*(1-fraction) + float64(mono[high])*fraction))
		binary.LittleEndian.PutUint16(pcm[frame*2:], uint16(sample))
	}
	return pcm, nil
}
