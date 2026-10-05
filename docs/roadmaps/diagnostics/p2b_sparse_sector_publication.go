//go:build ignore

// Native P2b sparse sector upload/publication diagnostic. Run from engine cwd.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/app"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"math/bits"
	"runtime"
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

var uploadedBytes, uploadedRecords uint64

func warm(a *app.App) {
	for frame := 0; frame < 256; frame++ {
		glfw.PollEvents()
		a.Update()
		uploadedBytes += a.BufferManager.VoxelUploadBytes
		uploadedRecords += uint64(a.BufferManager.VoxelBricksUploaded)
		a.Render()
		a.Device.Poll(true, nil)
		ready := true
		for _, o := range a.Scene.Objects {
			ok, _, _ := a.BufferManager.RenderVoxelObjectReady(o, o.RenderVoxelMap(), o.RenderVoxelMap().Revision)
			ready = ready && ok
		}
		if ready && !a.BufferManager.VoxelGPUWorkStats().Pending {
			return
		}
	}
	panic("auxiliary fixture never became ready")
}
func verify(a *app.App, o *core.VoxelObject) {
	m := a.BufferManager
	lo, hi := o.XBrickMap.ComputeAABB()
	opts := volume.VoxelNormalBakeOptions{BoundsMin: lo, BoundsMax: hi, HasBounds: true, SampleOccupancy: func(p [3]int) bool { occupied, _ := o.XBrickMap.GetVoxel(p[0], p[1], p[2]); return occupied }}
	for coord, s := range o.XBrickMap.Sectors {
		info, ok := m.SectorToInfo[s]
		require(ok, "missing sector header")
		header := readBuffer(a, m.SectorTableBuf, uint64(info.SlotIndex)*32, 32)
		for axis := 0; axis < 3; axis++ {
			require(int32(binary.LittleEndian.Uint32(header[axis*4:])) == int32(coord[axis]*32), "GPU sector origin differs from CPU authority")
		}
		require(binary.LittleEndian.Uint32(header[16:]) == info.BrickTableIndex, "GPU sector base differs from assigned range")
		mask := uint64(binary.LittleEndian.Uint32(header[20:])) | uint64(binary.LittleEndian.Uint32(header[24:]))<<32
		require(mask == s.BrickMask64, "GPU sector mask differs from CPU authority")
		// P2b publication remains valid for both legacy dense and P2c packed
		// record layouts; use the header's published base/mask/layout together.
		layout := binary.LittleEndian.Uint32(header[28:])
		require(layout == 0 || layout == 1, "unknown sector record layout")
		count := 64
		if layout == 1 {
			count = bits.OnesCount64(mask)
		}
		if count == 0 {
			continue
		}
		base := binary.LittleEndian.Uint32(header[16:])
		records := readBuffer(a, m.BrickTableBuf, uint64(base)*gpu.BrickRecordSize, uint64(count)*gpu.BrickRecordSize)
		for i := 0; i < 64; i++ {
			b := s.GetBrick(i%4, (i/4)%4, i/16)
			if b == nil {
				continue
			}
			recordIndex := i
			if layout == 1 {
				recordIndex = bits.OnesCount64(mask & ((uint64(1) << i) - 1))
			}
			word := binary.LittleEndian.Uint32(records[recordIndex*int(gpu.BrickRecordSize)+24:])
			require(word != gpu.VoxelAuxInvalidWordBase, "occupied brick has no auxiliary offset")
			require(uint64(word)*4+volume.VoxelAuxRecordBytes <= m.DenseOccupancyBuf.GetSize(), "native auxiliary offset exceeds physical buffer")
			origin := [3]int{coord[0]*32 + i%4*8, coord[1]*32 + (i/4)%4*8, coord[2]*32 + i/16*8}
			got := readBuffer(a, m.DenseOccupancyBuf, uint64(word)*4, volume.VoxelAuxRecordBytes)
			want := volume.BuildVoxelAuxBytes(b, origin, opts)
			if !bytes.Equal(got, want) {
				for index := range got {
					if got[index] != want[index] {
						panic(fmt.Sprintf("GPU auxiliary differs at sector %v brick %d byte %d: got=%d want=%d offset=%d", coord, i, index, got[index], want[index], word*4))
					}
				}
			}
		}
	}
}
func main() {
	baseline := flag.Bool("baseline", false, "record old capacity without enforcing sparse sizing")
	flag.Parse()
	runtime.LockOSThread()
	must(glfw.Init())
	defer glfw.Terminate()
	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(320, 240, "P2b sparse sector publication", nil, nil)
	must(err)
	defer window.Destroy()
	a := app.NewApp(window)
	a.FeatureConfig = app.AppFeatureConfig{AutoRegisterDefaults: false}
	must(a.Init())
	defer a.Shutdown()
	a.OcclusionMode = core.OcclusionOff
	a.Camera.Position, a.Camera.LookAt = mgl32.Vec3{0, 1, 14}, mgl32.Vec3{0, 1, 0}
	o := core.NewVoxelObject()
	o.MaterialTable = []core.Material{core.DefaultMaterial(), core.NewMaterial([4]uint8{190, 110, 50, 255}, [4]uint8{})}
	volume.Cube(o.XBrickMap, mgl32.Vec3{}, mgl32.Vec3{7, 7, 7}, 1)
	for i := 1; i < 127; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 1)
	}
	// Keep object bounds fixed during growth/removal. Isolated-voxel normal
	// tie breaking uses the object midpoint; clean records retain their baked
	// values when distant edits move that midpoint, independently of migration.
	o.XBrickMap.SetVoxel(4095*32, 0, 0, 1)
	a.Scene.AddObject(o)
	warm(a)
	verify(a, o)
	old := a.BufferManager.DenseOccupancyBuf.GetSize()
	fmt.Printf("128 sparse sectors: content bytes=%d, brick records=%d, auxiliary capacity=%d bytes\n", uploadedBytes, uploadedRecords, old)
	if *baseline {
		return
	}
	require(uploadedRecords == 128, "sparse sectors still upload inactive brick records")
	require(uploadedBytes == 128+128*(32+gpu.BrickRecordSize+volume.VoxelAuxRecordBytes), "sparse sector byte charges differ from actual occupied records")
	require(old <= 2*128*volume.VoxelAuxRecordBytes, "auxiliary capacity still scales with 64 potential sector records")
	// Add a second brick within an existing sector, including a normal seam.
	o.XBrickMap.SetVoxel(8, 0, 0, 1)
	warm(a)
	verify(a, o)
	// Exercise both mask words and the highest brick index, then clear one.
	for _, index := range []int{31, 32, 63} {
		o.XBrickMap.SetVoxel(index%4*8, (index/4)%4*8, index/16*8, 1)
	}
	warm(a)
	verify(a, o)
	o.XBrickMap.SetVoxel(0, 0, 16, 0)
	warm(a)
	verify(a, o)
	// Replacement preserves the final occupancy count and recycles safe slots.
	o.XBrickMap.SetVoxel(32, 0, 0, 0)
	o.XBrickMap.SetVoxel(40, 0, 0, 1)
	warm(a)
	verify(a, o)
	// Demand beyond current capacity must grow and preserve all previous bytes.
	count := int(a.BufferManager.DenseOccupancyBuf.GetSize()/volume.VoxelAuxRecordBytes) + 16
	require(count < 4096, "unexpected diagnostic auxiliary capacity")
	for i := 128; i < 128+count; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 1)
	}
	warm(a)
	require(a.BufferManager.DenseOccupancyBuf.GetSize() > old, "fixture did not force auxiliary growth")
	verify(a, o)
	// Removal does not shrink physical capacity; surviving normals remain exact.
	for i := 128; i < 128+count; i++ {
		o.XBrickMap.SetVoxel(i*32, 0, 0, 0)
	}
	warm(a)
	verify(a, o)
	// Reuse a previously published slot with content writes paused. Its old GPU
	// header remains physically present, but the new sector must be unreachable.
	m := a.BufferManager
	previous := m.SectorToInfo[o.XBrickMap.Sectors[[3]int{1, 0, 0}]]
	o.XBrickMap.SetVoxel(40, 0, 0, 0)
	o.XBrickMap.SetVoxel(4094*32, 0, 0, 1)
	m.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
	a.Update()
	a.Render()
	a.Device.Poll(true, nil)
	fresh := m.SectorToInfo[o.XBrickMap.Sectors[[3]int{4094, 0, 0}]]
	require(fresh.SlotIndex == previous.SlotIndex, "fixture did not reuse the retired sector slot")
	grid := readBuffer(a, m.SectorGridBuf, 0, m.SectorGridBuf.GetSize())
	for i := 0; i+32 <= len(grid); i += 32 {
		if binary.LittleEndian.Uint32(grid[i+20:]) != ^uint32(0) && binary.LittleEndian.Uint32(grid[i+16:]) == o.XBrickMap.ID && int32(binary.LittleEndian.Uint32(grid[i:])) == 4094 {
			panic("deferred reused sector exposes stale GPU header through native hash lookup")
		}
	}
	m.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
	warm(a)
	verify(a, o)
	grid = readBuffer(a, m.SectorGridBuf, 0, m.SectorGridBuf.GetSize())
	found := false
	for i := 0; i+32 <= len(grid); i += 32 {
		if binary.LittleEndian.Uint32(grid[i+20:]) == fresh.SlotIndex && binary.LittleEndian.Uint32(grid[i+16:]) == o.XBrickMap.ID && int32(binary.LittleEndian.Uint32(grid[i:])) == 4094 {
			found = true
		}
	}
	require(found, "completed sector header did not become reachable through native hash lookup")
	fmt.Printf("PASS: sparse native records, exact upload charges, fitted normals, edits, growth %d -> %d bytes and deferred reused-slot publication\n", old, m.DenseOccupancyBuf.GetSize())
}
