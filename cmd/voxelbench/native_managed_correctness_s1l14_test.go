package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/gpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// This is a correctness smoke, not a benchmark. It uses a real headless device,
// production G-buffer shader/bindings, texture readbacks and submission fences.
// No timestamp queries, wall-clock timings, fake executors or alternate shaders
// are used for scene rendering. The native benchmark's capture shader only reads
// the production depth/normal/material attachments without modifying them.
func TestNativeManagedPublicationS1l14Correctness(t *testing.T) {
	if os.Getenv("GEKKO_NATIVE_S1L14") != "1" {
		t.Skip("set GEKKO_NATIVE_S1L14=1 to run real-device managed publication correctness")
	}
	// z=4 independently tests a thin sheet whose occupied-bounds normal
	// tie-break differs from conservative [0,32] display bounds. Keep the z=24
	// case for the existing structural replacement/current seam coverage.
	for _, sheetDepth := range []int{4, 24} {
		t.Run(fmt.Sprintf("sheet-z%d", sheetDepth), func(t *testing.T) {
			instance := wgpu.CreateInstance(nil)
			defer instance.Release()
			adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{PowerPreference: wgpu.PowerPreferenceHighPerformance})
			if err != nil {
				t.Fatalf("native adapter (enabled runs must not skip): %v", err)
			}
			defer adapter.Release()
			t.Logf("native adapter: %v", adapter.GetInfo())
			device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{RequiredLimits: &wgpu.RequiredLimits{Limits: adapter.GetLimits().Limits}})
			if err != nil {
				t.Fatalf("native device: %v", err)
			}
			defer device.Release()
			queue := device.GetQueue()
			defer queue.Release()
			pipeline, err := createPipeline(device, shaders.GBufferWGSL, "S1l14 production G-buffer correctness", nil)
			if err != nil {
				t.Fatalf("production G-buffer pipeline: %v", err)
			}
			defer pipeline.Release()
			lighting, err := lightingLayoutProvider(device)
			if err != nil {
				t.Fatalf("production scene binding layout: %v", err)
			}
			defer lighting.Release()

			newObject := func() *core.VoxelObject {
				o := core.NewVoxelObject()
				o.AllowOcclusionCulling = false
				o.CastsShadows = false
				o.MaterialTable = []core.Material{{}, core.NewMaterial([4]uint8{200, 110, 65, 255}, [4]uint8{}), core.NewMaterial([4]uint8{65, 145, 210, 255}, [4]uint8{})}
				// A visible plate crosses the x=32 sector seam. Later edits affect both
				// seam sectors, and a nearer plate adds two coordinates in z=1.
				for x := 28; x < 37; x++ {
					for y := 8; y < 25; y++ {
						o.XBrickMap.SetVoxel(x, y, sheetDepth, uint8(1+(x+y)%2))
					}
				}
				return o
			}
			newNative := func(o *core.VoxelObject) *nativeBench {
				m := gpu.NewGpuBufferManager(device, core.NewProfiler())
				m.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
				m.CreateGBufferTextures(64, 64)
				s := core.NewScene()
				s.AddObject(o)
				camera := core.NewCameraState()
				camera.Position = mgl32.Vec3{32, 16, 68}
				camera.LookAt = mgl32.Vec3{32, 16, 16}
				camera.Far = 160
				return &nativeBench{device: device, queue: queue, manager: m, pipeline: pipeline, lighting: lighting, scene: s, object: o, camera: camera, cfg: settings{Width: 64, Height: 64, Batch: 1}}
			}
			ordinary := newNative(newObject())
			defer releaseManager(ordinary.manager)
			managed := newNative(newObject())
			defer releaseManager(managed.manager)
			owner := volume.NewManagedXBrickMap(managed.object.XBrickMap)
			generation := uint64(7)
			managed.object.SetManagedGeometryProducerWithGenerationReader(managed.object.XBrickMap,
				func() (volume.ManagedGeometryView, uint64, bool) {
					v, ok := owner.CaptureGeometry()
					return v, generation, ok
				},
				func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
					v, ok := owner.CaptureSector(c)
					return v, generation, ok
				},
				func() (uint64, bool) { return generation, true })
			managed.manager.SetManagedGeometryAdmissionBudget(gpu.DefaultManagedGeometryAdmissionBudget())
			managed.manager.SetManagedGeometryFrameBudget(gpu.ManagedGeometryFrameBudget{Enabled: true, MaxEntries: 1})

			// One service/commit/native update and one completed submission per frame.
			// Recreating the real scene bind groups is required after buffer generations
			// change; textures are handled explicitly by the resize exercise below.
			frame := func(n *nativeBench) {
				t.Helper()
				aspect := float32(n.cfg.Width) / float32(n.cfg.Height)
				view := n.camera.GetViewMatrix()
				proj := n.camera.ProjectionMatrix(aspect)
				vp := proj.Mul4(view)
				n.manager.PrepareManagedGeometryFrame(n.scene)
				n.scene.Commit(n.camera.ExtractFrustum(vp), core.SceneCommitOptions{OcclusionMode: core.OcclusionOff, CameraPosition: n.camera.Position})
				recreated := n.manager.UpdateScene(n.scene, n.camera, aspect, mgl32.Vec3{})
				if recreated || n.manager.GBufferBindGroup0 == nil || !n.manager.GBufferSceneBindGroupCurrent() {
					n.manager.CreateGBufferBindGroups(n.pipeline, n.lighting)
				}
				n.manager.UpdateCamera(vp, view.Inv(), proj.Inv(), n.camera.Position, mgl32.Vec3{30, 60, 50}, mgl32.Vec3{.2, .2, .2}, mgl32.Vec3{}, 1, 0, n.camera.FarPlane(), 0, 0, 0, uint32(n.cfg.Width), uint32(n.cfg.Height), core.DefaultLightingQualityConfig())
				e, eerr := device.CreateCommandEncoder(nil)
				if eerr != nil {
					t.Fatalf("native frame encoder: %v", eerr)
				}
				err := n.submit(e)
				e.Release()
				if err != nil {
					t.Fatalf("native frame submission: %v", err)
				}
			}
			captureFrame := func(n *nativeBench, hidden bool) capture {
				t.Helper()
				c, err := n.capture()
				if hidden {
					if err == nil || err.Error() != "G-buffer capture has no valid hit pixels" || c.HitPixels != 0 {
						t.Fatalf("hidden native frame: capture %+v error %v", c, err)
					}
				} else if err != nil {
					t.Fatalf("native G-buffer readback: %v", err)
				}
				return c
			}
			drain := func(n *nativeBench, ready func() bool) {
				t.Helper()
				for i := 0; i < 1024; i++ {
					frame(n)
					if ready() {
						return
					}
				}
				t.Fatal("native correctness work did not drain in 1024 frames")
			}
			ordinaryReady := func() bool {
				o := ordinary.object
				ready, _, _ := ordinary.manager.RenderVoxelObjectReady(o, o.RenderVoxelMap(), o.XBrickMap.Revision)
				return ready && !ordinary.manager.VoxelGPUWorkStats().Pending && ordinary.manager.VoxelDirtySectorsPending == 0 && ordinary.manager.VoxelDirtyBricksPending == 0
			}
			managedReady := func() bool {
				s, ok := managed.manager.ManagedGeometryGPUStatus(managed.object)
				return ok && s.CurrentInput.SameSource(s.CurrentInput) && s.CurrentGeneration == generation && !s.Pending
			}
			compare := func(label string) capture {
				t.Helper()
				want, got := captureFrame(ordinary, false), captureFrame(managed, false)
				if got.Normal != want.Normal || got.Material != want.Material || got.HitPixels != want.HitPixels {
					if got.Normal != want.Normal {
						s1l14NativeNormalDiagnostics(t, ordinary, managed)
					}
					t.Fatalf("%s native normal/material/hit-count mismatch\nordinary %+v\nmanaged  %+v", label, want, got)
				}
				if got.Depth != want.Depth {
					// Conservative managed sector bounds move the float32 DDA entry
					// compared with tight ordinary occupied bounds. Apple M4 Pro showed
					// identical hit masks/normals/materials and at most 4 ULP depth drift.
					// Bound every finite hit by BOTH 8 ULP and 1e-4 world units; unchanged
					// current-image comparisons elsewhere remain exact capture hashes.
					difference := s1l14NativeDepthDiagnostics(t, ordinary, managed)
					if difference.MaskDifferences != 0 || difference.NonfiniteDifferences != 0 || difference.NonHitDifferences != 0 || difference.MaxAbs > 1e-4 || difference.MaxULP > 8 {
						for _, sample := range difference.Samples {
							t.Log(sample)
						}
						t.Fatalf("%s native depth outside tolerance: %+v", label, difference)
					}
					t.Logf("%s native depth: %d differing hit pixels, max absolute %.9g world units, max %d ULP; exact hit mask", label, difference.DifferingPixels, difference.MaxAbs, difference.MaxULP)
				}
				return got
			}
			drain(ordinary, ordinaryReady)
			captureFrame(ordinary, false)
			frame(managed)
			if managed.object.RenderVoxelMap() != nil || len(managed.scene.VisibleObjects) != 0 {
				t.Fatal("initial incomplete generation entered native scene records")
			}
			captureFrame(managed, true)
			drain(managed, managedReady)
			// Stall continuity compares managed current with its own published image;
			// ordinary-vs-managed depth rounding does not enter that exact oracle.
			baseline := compare("initial complete publication")
			current := managed.object.RenderVoxelMap()

			apply := func(writes []volume.VoxelWrite) {
				previous := generation
				for _, w := range writes {
					owner.SetVoxel(w.X, w.Y, w.Z, w.Value)
					managed.object.XBrickMap.SetVoxel(w.X, w.Y, w.Z, w.Value)
					ordinary.object.XBrickMap.SetVoxel(w.X, w.Y, w.Z, w.Value)
				}
				generation++
				managed.manager.NotifyManagedGeometryGPUContent(managed.object, previous, generation, writes)
			}
			var additions []volume.VoxelWrite
			for x := 28; x < 37; x++ {
				for y := 10; y < 23; y++ {
					additions = append(additions, volume.VoxelWrite{X: x, Y: y, Z: 40, Value: 2})
				}
			}
			apply(additions)
			managed.manager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
			for i := 0; i < 12; i++ {
				frame(managed)
			}
			if managed.object.RenderVoxelMap() != current {
				t.Fatal("unuploaded structural successor replaced current native coverage")
			}
			if got := captureFrame(managed, false); got != baseline {
				t.Fatalf("stalled structural stage changed native attachments: got %+v want %+v", got, baseline)
			}
			managed.manager.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
			// A structural managed generation bakes against its complete final bounds.
			// Ordinary incremental edits only invalidate local halos: the new z=40
			// plate lies outside the old z=24 plate's halo, yet changes the bounds-based
			// orientation tie-break of that isolated surface. Refresh every ordinary
			// sidecar here so this reference represents a fresh final generation rather
			// than cached normals from its earlier bounds. Pure content edits below
			// continue through the ordinary incremental path without this refresh.
			for coordinate, sector := range ordinary.object.XBrickMap.Sectors {
				if sector == nil {
					continue
				}
				for index := 0; index < 64; index++ {
					if sector.BrickMask64&(uint64(1)<<index) != 0 {
						ordinary.object.XBrickMap.MarkBrickNormalDirty([6]int{coordinate[0], coordinate[1], coordinate[2], index % 4, (index / 4) % 4, index / 16})
					}
				}
			}
			drain(ordinary, ordinaryReady)
			drain(managed, managedReady)
			successor := compare("structural successor")
			if successor.Depth == baseline.Depth {
				t.Fatal("structural fixture did not change rendered depth")
			}
			current = managed.object.RenderVoxelMap()

			apply([]volume.VoxelWrite{{X: 31, Y: 16, Z: 40, Value: 1}, {X: 32, Y: 16, Z: 40, Value: 1}, {X: 31, Y: 17, Z: 40}, {X: 32, Y: 17, Z: 40}})
			drain(ordinary, ordinaryReady)
			drain(managed, managedReady)
			edited := compare("current content and seam normals")
			if managed.object.RenderVoxelMap() != current {
				t.Fatal("same-topology edit replaced entire native render selection")
			}
			if edited.Material == successor.Material && edited.Depth == successor.Depth {
				t.Fatal("content fixture made no visible change")
			}

			for _, n := range []*nativeBench{ordinary, managed} {
				n.cfg.Width, n.cfg.Height = 80, 72
				n.manager.CreateGBufferTextures(uint32(n.cfg.Width), uint32(n.cfg.Height))
				n.manager.CreateGBufferBindGroups(n.pipeline, n.lighting)
				frame(n)
			}
			resized := compare("resized textures and recreated bindings")
			managed.manager.SetManagedGeometryFrameBudget(gpu.ManagedGeometryFrameBudget{})
			managed.manager.SetVoxelUploadBudget(gpu.VoxelUploadBudget{})
			for i := 0; i < 4; i++ {
				frame(managed)
			}
			if managed.object.RenderVoxelMap() != current {
				t.Fatal("disable cleared native coverage before ordinary target ready")
			}
			if got := captureFrame(managed, false); got != resized {
				t.Fatal("disable while replacement paused changed native attachments")
			}
			if managed.manager.ManagedGeometryAdmissionStats().TotalStageBytes == 0 {
				t.Fatal("disable erased still-owned snapshot charge")
			}
			managed.manager.SetVoxelUploadBudget(gpu.DefaultVoxelUploadBudget())
			drain(managed, func() bool {
				o := managed.object
				if o.RenderVoxelMap() != o.XBrickMap {
					return false
				}
				ready, _, _ := managed.manager.RenderVoxelObjectReady(o, o.XBrickMap, o.XBrickMap.Revision)
				return ready && managed.manager.ManagedGeometryAdmissionStats() == (gpu.ManagedGeometryAdmissionStats{})
			})
			compare("ordinary compatibility handoff")
			managed.scene.Objects = nil
			drain(managed, func() bool {
				return len(managed.manager.Allocations) == 0 && len(managed.manager.MaterialAllocations) == 0 && managed.manager.ManagedGeometryAdmissionStats() == (gpu.ManagedGeometryAdmissionStats{})
			})
			captureFrame(managed, true)
			// Complete and age the actual removal submission before public resources
			// are released. Fence completion comes from this device, never frame fakes.
			for i := 0; i < gpu.RetiredBufferFrameDelay+2; i++ {
				frame(managed)
			}
		})
	}
}

// Diagnostic readback of the already-rendered production depth attachment. The
// conversion shader only loads pixels; it does not replace scene traversal or
// alter production attachments. Per-pixel metrics establish the bounded depth
// rounding oracle while normals/materials and unchanged current images stay exact.
func s1l14NativeDepthReadback(t *testing.T, n *nativeBench) []byte {
	t.Helper()
	return s1l14NativeAttachmentReadback(t, n, n.manager.DepthView)
}

func s1l14NativeAttachmentReadback(t *testing.T, n *nativeBench, view *wgpu.TextureView) []byte {
	t.Helper()
	size := uint64(n.cfg.Width) * uint64(n.cfg.Height) * 16
	storage, err := n.device.CreateBuffer(&wgpu.BufferDescriptor{Label: "S1l14 diagnostic depth storage", Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	if err != nil {
		t.Fatalf("depth diagnostic storage: %v", err)
	}
	defer storage.Release()
	readback, err := n.device.CreateBuffer(&wgpu.BufferDescriptor{Label: "S1l14 diagnostic depth readback", Size: size, Usage: wgpu.BufferUsageCopyDst | wgpu.BufferUsageMapRead})
	if err != nil {
		t.Fatalf("depth diagnostic readback: %v", err)
	}
	defer readback.Release()
	bgl, err := n.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{Entries: []wgpu.BindGroupLayoutEntry{
		{Binding: 0, Visibility: wgpu.ShaderStageCompute, Texture: wgpu.TextureBindingLayout{SampleType: wgpu.TextureSampleTypeUnfilterableFloat, ViewDimension: wgpu.TextureViewDimension2D}},
		{Binding: 1, Visibility: wgpu.ShaderStageCompute, Buffer: wgpu.BufferBindingLayout{Type: wgpu.BufferBindingTypeStorage}},
	}})
	if err != nil {
		t.Fatalf("depth diagnostic binding layout: %v", err)
	}
	defer bgl.Release()
	layout, err := n.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{BindGroupLayouts: []*wgpu.BindGroupLayout{bgl}})
	if err != nil {
		t.Fatalf("depth diagnostic pipeline layout: %v", err)
	}
	defer layout.Release()
	pipeline, err := createPipeline(n.device, captureWGSL, "S1l14 diagnostic depth pixels", layout)
	if err != nil {
		t.Fatalf("depth diagnostic pipeline: %v", err)
	}
	defer pipeline.Release()
	bg, err := n.device.CreateBindGroup(&wgpu.BindGroupDescriptor{Layout: bgl, Entries: []wgpu.BindGroupEntry{{Binding: 0, TextureView: view}, {Binding: 1, Buffer: storage, Size: wgpu.WholeSize}}})
	if err != nil {
		t.Fatalf("depth diagnostic bindings: %v", err)
	}
	defer bg.Release()
	e, err := n.device.CreateCommandEncoder(nil)
	if err != nil {
		t.Fatalf("depth diagnostic encoder: %v", err)
	}
	defer e.Release()
	p := e.BeginComputePass(nil)
	p.SetPipeline(pipeline)
	p.SetBindGroup(0, bg, nil)
	p.DispatchWorkgroups(uint32((n.cfg.Width+7)/8), uint32((n.cfg.Height+7)/8), 1)
	err = p.End()
	p.Release()
	if err != nil {
		t.Fatalf("depth diagnostic pass: %v", err)
	}
	if err = e.CopyBufferToBuffer(storage, 0, readback, 0, size); err != nil {
		t.Fatalf("depth diagnostic copy: %v", err)
	}
	if err = n.submit(e); err != nil {
		t.Fatalf("depth diagnostic submission: %v", err)
	}
	data, err := readBuffer(n.device, readback, size)
	if err != nil {
		t.Fatalf("depth diagnostic mapped pixels: %v", err)
	}
	return data
}

type s1l14NativeDepthDifference struct {
	DifferingPixels, MaskDifferences, NonfiniteDifferences, NonHitDifferences int
	MaxAbs                                                                    float64
	MaxULP                                                                    uint64
	Samples                                                                   []string
}

func s1l14NativeDepthDiagnostics(t *testing.T, ordinary, managed *nativeBench) s1l14NativeDepthDifference {
	t.Helper()
	want, got := s1l14NativeDepthReadback(t, ordinary), s1l14NativeDepthReadback(t, managed)
	if len(want) != len(got) {
		t.Fatalf("depth attachment lengths ordinary=%d managed=%d", len(want), len(got))
	}
	ordered := func(bits uint32) uint32 {
		if bits>>31 != 0 {
			return ^bits
		}
		return bits | 1<<31
	}
	hit := func(value float32, far float32) bool {
		return !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0) && value > 0 && value < far
	}
	var result s1l14NativeDepthDifference
	for offset := 0; offset < len(want); offset += 16 {
		wb, gb := binary.LittleEndian.Uint32(want[offset:]), binary.LittleEndian.Uint32(got[offset:])
		w, g := math.Float32frombits(wb), math.Float32frombits(gb)
		wh, gh := hit(w, ordinary.camera.FarPlane()), hit(g, managed.camera.FarPlane())
		if wh != gh {
			result.MaskDifferences++
		}
		if wb == gb {
			continue
		}
		result.DifferingPixels++
		abs := math.Abs(float64(g) - float64(w))
		wo, goBits := uint64(ordered(wb)), uint64(ordered(gb))
		ulp := wo - goBits
		if goBits > wo {
			ulp = goBits - wo
		}
		if math.IsNaN(abs) || math.IsInf(abs, 0) {
			result.NonfiniteDifferences++
		} else if wh && gh {
			if abs > result.MaxAbs {
				result.MaxAbs = abs
			}
			if ulp > result.MaxULP {
				result.MaxULP = ulp
			}
		} else if !wh && !gh {
			result.NonHitDifferences++
		}
		if len(result.Samples) < 8 {
			pixel := offset / 16
			result.Samples = append(result.Samples, fmt.Sprintf("depth pixel (%d,%d): ordinary=%.9g [0x%08x] managed=%.9g [0x%08x] abs=%.9g ULP=%d hit=%v/%v", pixel%ordinary.cfg.Width, pixel/ordinary.cfg.Width, w, wb, g, gb, abs, ulp, wh, gh))
		}
	}
	return result
}

func s1l14NativeNormalDiagnostics(t *testing.T, ordinary, managed *nativeBench) {
	t.Helper()
	want := s1l14NativeAttachmentReadback(t, ordinary, ordinary.manager.NormalView)
	got := s1l14NativeAttachmentReadback(t, managed, managed.manager.NormalView)
	depth := s1l14NativeDepthReadback(t, ordinary)
	if len(want) != len(got) || len(want) != len(depth) {
		t.Fatalf("normal diagnostic attachment lengths ordinary=%d managed=%d depth=%d", len(want), len(got), len(depth))
	}
	differing, hitDifferences, nonfinite, samples, differingXYZ, differingAO := 0, 0, 0, 0, 0, 0
	var maxComponent, maxAngle, maxXYZ, maxAO float64
	minX, minY, maxX, maxY := ordinary.cfg.Width, ordinary.cfg.Height, -1, -1
	for offset := 0; offset < len(want); offset += 16 {
		var w, g [4]float32
		changed := false
		for j := range w {
			wb, gb := binary.LittleEndian.Uint32(want[offset+j*4:]), binary.LittleEndian.Uint32(got[offset+j*4:])
			w[j], g[j] = math.Float32frombits(wb), math.Float32frombits(gb)
			changed = changed || wb != gb
		}
		if !changed {
			continue
		}
		differing++
		if w[0] != g[0] || w[1] != g[1] || w[2] != g[2] {
			differingXYZ++
		}
		if w[3] != g[3] {
			differingAO++
		}
		pixel := offset / 16
		x, y := pixel%ordinary.cfg.Width, pixel/ordinary.cfg.Width
		if x < minX {
			minX = x
		}
		if x > maxX {
			maxX = x
		}
		if y < minY {
			minY = y
		}
		if y > maxY {
			maxY = y
		}
		d := math.Float32frombits(binary.LittleEndian.Uint32(depth[offset:]))
		hit := !math.IsNaN(float64(d)) && !math.IsInf(float64(d), 0) && d > 0 && d < ordinary.camera.FarPlane()
		if hit {
			hitDifferences++
		}
		var dot, wl, gl, component float64
		for j := range w {
			abs := math.Abs(float64(g[j]) - float64(w[j]))
			if math.IsNaN(abs) || math.IsInf(abs, 0) {
				nonfinite++
			} else if abs > component {
				component = abs
			}
			if j < 3 {
				if abs > maxXYZ {
					maxXYZ = abs
				}
				dot += float64(w[j]) * float64(g[j])
				wl += float64(w[j]) * float64(w[j])
				gl += float64(g[j]) * float64(g[j])
			} else if abs > maxAO {
				maxAO = abs
			}
		}
		if component > maxComponent {
			maxComponent = component
		}
		angle := float64(0)
		if wl > 0 && gl > 0 {
			cosine := dot / math.Sqrt(wl*gl)
			cosine = math.Max(-1, math.Min(1, cosine))
			angle = math.Acos(cosine) * 180 / math.Pi
			if angle > maxAngle {
				maxAngle = angle
			}
		}
		if samples < 12 {
			t.Logf("normal diagnostic pixel (%d,%d) depth=%.9g hit=%v ordinary=%v managed=%v max component=%.9g angle=%.9g degrees", x, y, d, hit, w, g, component, angle)
			samples++
		}
	}
	t.Logf("normal diagnostic summary: differing pixels=%d hit pixels=%d xyz differences=%d AO differences=%d bounds=(%d,%d)..(%d,%d) max component difference=%.9g max XYZ=%.9g max AO=%.9g max angle=%.9g degrees nonfinite components=%d", differing, hitDifferences, differingXYZ, differingAO, minX, minY, maxX, maxY, maxComponent, maxXYZ, maxAO, maxAngle, nonfinite)
}
