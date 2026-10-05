//go:build ignore

// Native P4 material sharing: real allocation, queued uploads, growth, isolation,
// shader parity and private dynamic rows. Run from the engine module in a desktop session.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"runtime"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func require(ok bool, message string) {
	if !ok {
		panic(message)
	}
}
func table(color [4]uint8) []core.Material {
	rows := make([]core.Material, 256)
	rows[0] = core.DefaultMaterial()
	rows[1] = core.NewMaterial(color, [4]uint8{20, 10, 0, 255})
	rows[1].Emission = .2
	return rows
}
func sealed(rows []core.Material, key string) *core.ImmutableMaterialTable {
	owner, err := core.NewImmutableMaterialTable(rows, key)
	must(err)
	return owner
}
func encoded(rows []core.Material) []byte {
	result := make([]byte, 256*64)
	for i, material := range rows {
		values := [16]float32{}
		for j := 0; j < 4; j++ {
			values[j] = float32(material.BaseColor[j]) / 255
			values[4+j] = float32(material.Emissive[j]) / 255
		}
		values[8], values[9], values[10], values[11] = material.Roughness, material.Metalness, material.IOR, material.Transparency
		values[12], values[13], values[14], values[15] = material.Emission, material.Transmission, material.Density, material.Refraction
		for j, value := range values {
			binary.LittleEndian.PutUint32(result[i*64+j*4:], math.Float32bits(value))
		}
	}
	return result
}
func submit(a *app.App, encoder *wgpu.CommandEncoder) {
	command, err := encoder.Finish(nil)
	must(err)
	a.Queue.Submit(command)
	command.Release()
	a.Device.Poll(true, nil)
}
func mapped(a *app.App, buffer *wgpu.Buffer, size uint64) []byte {
	done := false
	buffer.MapAsync(wgpu.MapModeRead, 0, size, func(status wgpu.BufferMapAsyncStatus) {
		require(status == wgpu.BufferMapAsyncStatusSuccess, "readback map failed")
		done = true
	})
	a.Device.Poll(true, nil)
	require(done, "readback did not finish")
	data := append([]byte(nil), buffer.GetMappedRange(0, uint(size))...)
	buffer.Unmap()
	return data
}
func readBuffer(a *app.App, source *wgpu.Buffer, offset, size uint64) []byte {
	buffer, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(err)
	defer buffer.Release()
	encoder, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer encoder.Release()
	encoder.CopyBufferToBuffer(source, offset, buffer, 0, size)
	submit(a, encoder)
	return mapped(a, buffer, size)
}
func installOutput(a *app.App) {
	texture, err := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "P4 albedo readback output", Size: wgpu.Extent3D{Width: a.Config.Width, Height: a.Config.Height, DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA16Float, Usage: wgpu.TextureUsageStorageBinding | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc})
	must(err)
	view, err := texture.CreateView(nil)
	must(err)
	a.StorageView.Release()
	a.StorageTexture.Release()
	a.StorageTexture, a.StorageView = texture, view
	a.BufferManager.CreateLightingBindGroups(a.LightingPipeline, view)
}
func image(a *app.App) []byte { return readTexture(a, a.StorageTexture) }

func readTexture(a *app.App, texture *wgpu.Texture) []byte {
	width, height := a.Config.Width, a.Config.Height
	row := (width*8 + 255) &^ uint32(255)
	size := uint64(row) * uint64(height)
	buffer, err := a.Device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	must(err)
	defer buffer.Release()
	encoder, err := a.Device.CreateCommandEncoder(nil)
	must(err)
	defer encoder.Release()
	encoder.CopyTextureToBuffer(&wgpu.ImageCopyTexture{Texture: texture}, &wgpu.ImageCopyBuffer{Buffer: buffer, Layout: wgpu.TextureDataLayout{BytesPerRow: row, RowsPerImage: height}}, &wgpu.Extent3D{Width: width, Height: height, DepthOrArrayLayers: 1})
	submit(a, encoder)
	return mapped(a, buffer, size)
}
func warm(a *app.App) int {
	writes := 0
	for frame := 0; frame < 128; frame++ {
		glfw.PollEvents()
		a.Update()
		a.Render()
		a.Device.Poll(true, nil)
		writes += a.BufferManager.VoxelMaterialsUploaded
		ready := true
		for _, obj := range a.Scene.Objects {
			ok, _, _ := a.BufferManager.RenderVoxelObjectReady(obj, obj.RenderVoxelMap(), obj.RenderVoxelMap().Revision)
			ready = ready && ok
		}
		if ready && !a.BufferManager.VoxelGPUWorkStats().Pending {
			return writes
		}
	}
	panic("material fixture did not become ready")
}
func verify(a *app.App, obj *core.VoxelObject, rows []core.Material) {
	allocation := a.BufferManager.MaterialAllocations[obj]
	require(allocation != nil, "missing material binding")
	actual := readBuffer(a, a.BufferManager.MaterialBuf, uint64(allocation.MaterialOffset)*64, 256*64)
	require(bytes.Equal(actual, encoded(rows)), "native material rows differ")
}
func main() {
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(320, 240, "P4 material sharing", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	a.RegisterFeature(&app.TransparencyFeature{})
	must(a.Init())
	defer a.Shutdown()
	a.RenderMode = 1
	a.OcclusionMode = core.OcclusionOff
	a.Camera.Position, a.Camera.LookAt = mgl32.Vec3{0, 1, 14}, mgl32.Vec3{0, 1, 0}
	a.Scene.AmbientLight = mgl32.Vec3{.3, .3, .3}
	rows := table([4]uint8{190, 110, 50, 255})
	owner := sealed(rows, "P4 native static palette")
	model := volume.NewXBrickMap()
	volume.Cube(model, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 1)
	objects := make([]*core.VoxelObject, 2)
	for i := range objects {
		obj := core.NewVoxelObject()
		obj.XBrickMap = model
		obj.Transform.Position = mgl32.Vec3{float32(i)*4 - 2, 0, 0}
		obj.Transform.Scale = mgl32.Vec3{.25, .25, .25}
		obj.SetImmutableMaterialTable(owner)
		objects[i] = obj
		a.Scene.AddObject(obj)
	}
	require(warm(a) == 1, "equal immutable tables did not upload once")
	require(a.BufferManager.MaterialAllocations[objects[0]].MaterialOffset == a.BufferManager.MaterialAllocations[objects[1]].MaterialOffset, "equal tables did not share offset")
	verify(a, objects[0], rows)
	verify(a, objects[1], rows)
	installOutput(a)
	a.Update()
	a.Render()
	a.Device.Poll(true, nil)
	sharedImage := image(a)
	for _, obj := range objects {
		obj.SetImmutableMaterialTable(nil)
		obj.MaterialTable = append([]core.Material(nil), rows...)
	}
	warm(a)
	privateImage := image(a)
	require(bytes.Equal(sharedImage, privateImage), "shared/private opaque albedo parity failed")
	// Lit emissive output must also match a private-block reference.
	a.RenderMode = 0
	for _, obj := range objects {
		obj.SetImmutableMaterialTable(owner)
	}
	warm(a)
	litShared := image(a)
	for _, obj := range objects {
		obj.SetImmutableMaterialTable(nil)
		obj.MaterialTable = append([]core.Material(nil), rows...)
	}
	warm(a)
	require(bytes.Equal(litShared, image(a)), "shared/private emissive lighting parity failed")
	// Read actual WBOIT material-shader output, independently of final resolve.
	transparent, err := a.Device.CreateTexture(&wgpu.TextureDescriptor{Label: "P4 transparency readback", Size: wgpu.Extent3D{Width: a.Config.Width, Height: a.Config.Height, DepthOrArrayLayers: 1}, MipLevelCount: 1, SampleCount: 1, Dimension: wgpu.TextureDimension2D, Format: wgpu.TextureFormatRGBA16Float, Usage: wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding | wgpu.TextureUsageCopySrc})
	must(err)
	transparentView, err := transparent.CreateView(nil)
	must(err)
	a.BufferManager.TransparentAccumView.Release()
	a.BufferManager.TransparentAccumTex.Release()
	a.BufferManager.TransparentAccumTex, a.BufferManager.TransparentAccumView = transparent, transparentView
	glassRows := append([]core.Material(nil), rows...)
	glassRows[1].Transparency, glassRows[1].Transmission = .45, .2
	glass := sealed(glassRows, "P4 shared transparency")
	for _, obj := range objects {
		obj.SetImmutableMaterialTable(glass)
	}
	warm(a)
	glassShared := readTexture(a, transparent)
	require(!bytes.Equal(glassShared, make([]byte, len(glassShared))), "transparency fixture produced no shader output")
	for _, obj := range objects {
		obj.SetImmutableMaterialTable(nil)
		obj.MaterialTable = append([]core.Material(nil), glassRows...)
	}
	warm(a)
	require(bytes.Equal(glassShared, readTexture(a, transparent)), "shared/private transparency parity failed")
	// Dynamic/animated inputs remain private. Simulate successive evaluated
	// frames and check actual native bytes without changing the other instance.
	for frame := 0; frame < 3; frame++ {
		dynamicRows := append([]core.Material(nil), glassRows...)
		dynamicRows[1].Emission = float32(frame+1) * .3
		dynamicRows[1].BaseColor = [4]uint8{uint8(40 + frame*50), 100, 200, 255}
		objects[0].MaterialTable = dynamicRows
		require(warm(a) == 1, "private dynamic frame must upload only the changed table")
		verify(a, objects[0], dynamicRows)
		verify(a, objects[1], glassRows)
	}
	a.RenderMode = 1
	for _, obj := range objects {
		obj.SetImmutableMaterialTable(owner)
	}
	warm(a)
	// An admitted replacement must not expose an unwritten palette to shaders.
	replacement := sealed(table([4]uint8{90, 210, 80, 255}), "P4 paused replacement")
	objects[0].SetImmutableMaterialTable(replacement)
	a.BufferManager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
	a.Update()
	params := readBuffer(a, a.BufferManager.ObjectParamsBuf, 0, 2*128)
	require(binary.LittleEndian.Uint32(params[24:]) == 0, "pending native material published traversal parameters")
	require(binary.LittleEndian.Uint32(params[128+24:]) > 0, "pending replacement hid a ready shared peer")
	a.BufferManager.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
	warm(a)
	verify(a, objects[0], replacement.MaterialTable())
	objects[0].SetImmutableMaterialTable(owner)
	warm(a)
	objects[0].MaterialTable[1].BaseColor = [4]uint8{30, 180, 220, 255}
	changedRows := append([]core.Material(nil), objects[0].MaterialTable...)
	require(warm(a) == 1, "instance edit must upload one private table")
	require(a.BufferManager.MaterialAllocations[objects[0]].MaterialOffset != a.BufferManager.MaterialAllocations[objects[1]].MaterialOffset, "edited table still shared")
	verify(a, objects[0], changedRows)
	verify(a, objects[1], rows)
	require(!bytes.Equal(sharedImage, image(a)), "instance recolor did not change visible albedo")
	before := a.BufferManager.MaterialBufferGeneration
	oldSize := a.BufferManager.MaterialBuf.GetSize()
	// Populate enough distinct immutable tables to force actual buffer growth.
	count := int(oldSize/(256*64)) + 4
	require(count < 4096, "diagnostic material capacity unexpectedly large")
	for i := 0; i < count; i++ {
		obj := core.NewVoxelObject()
		obj.XBrickMap = model
		obj.Transform.Position = mgl32.Vec3{1000 + float32(i), 0, 0}
		obj.SetImmutableMaterialTable(sealed(rows, fmt.Sprintf("growth palette %d", i)))
		a.Scene.AddObject(obj)
	}
	warm(a)
	require(a.BufferManager.MaterialBufferGeneration > before, "fixture did not grow material buffer")
	verify(a, objects[0], changedRows)
	verify(a, objects[1], rows)
	// Remove all growth owners; the survivor must retain its bytes and attachment.
	a.Scene.Objects = objects[1:]
	warm(a)
	verify(a, objects[1], rows)
	fmt.Printf("PASS: shared upload, native rows, albedo/emissive/transparency parity, private dynamic rows, paused publication, instance edit isolation and buffer growth %d -> %d bytes\n", oldSize, a.BufferManager.MaterialBuf.GetSize())
}
