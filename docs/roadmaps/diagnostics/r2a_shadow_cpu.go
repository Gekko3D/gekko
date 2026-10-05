//go:build ignore

// CPU-only local-shadow scheduling diagnostic. No GPU/frame-time claim.
package main

import (
	"fmt"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
	"runtime"
	"testing"
)

func main() {
	runtime.GOMAXPROCS(1)
	for _, count := range []int{32, 1000} {
		s := core.NewScene()
		camera := core.NewCameraState()
		camera.Position = mgl32.Vec3{0, 2, 500}
		model := volume.NewXBrickMap()
		model.SetVoxel(0, 0, 0, 1)
		model.ClearDirty()
		for i := 0; i < count; i++ {
			o := core.NewVoxelObject()
			o.XBrickMap = model
			o.Transform.Position = mgl32.Vec3{float32(i%8)*32 + float32(i/8%8)*.5, 0, float32(i/64%8) * .5}
			s.AddObject(o)
		}
		for i := 0; i < 8; i++ {
			s.Lights = append(s.Lights, core.Light{Position: [4]float32{float32(i)*32 + 2, 8, 2, 0}, Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{16, .6, float32(core.LightTypeSpot), 1}})
		}
		s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{CameraPosition: camera.Position})
		m := gpu.GpuBufferManager{}
		m.UpdateLights(s, camera, 1)
		seen := map[uint32]core.ShadowUpdate{}
		for f := uint64(0); f < 16; f++ {
			u := m.BuildShadowUpdates(s, camera, f, false)
			for _, v := range u {
				seen[v.ShadowLayer] = v
			}
			m.RecordShadowUpdates(u, f, s.ShadowRevision())
		}
		all := []core.ShadowUpdate{}
		for _, u := range seen {
			all = append(all, u)
		}
		m.RecordShadowUpdates(all, 100, s.ShadowRevision())
		if len(m.BuildShadowUpdates(s, camera, 101, false)) != 0 {
			panic("warm not idle")
		}
		result := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m.UpdateLights(s, camera, 1)
				if len(m.BuildShadowUpdates(s, camera, 101, false)) != 0 {
					panic("idle changed")
				}
			}
		})
		fmt.Printf("%d selected casters, 8 local lights, idle UpdateLights+BuildShadowUpdates: %s %s\n", len(s.ShadowObjects), result.String(), result.MemString())
	}
}
