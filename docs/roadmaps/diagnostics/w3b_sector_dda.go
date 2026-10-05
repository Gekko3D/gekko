//go:build ignore

// W3b native helper contract probe. Every entry compiles with an exported
// production shader, but only the shared sector DDA helper is reachable.
// Run: go build -o /tmp/gekko-w3b-dda docs/roadmaps/diagnostics/w3b_sector_dda.go
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

const probeCapacity = 1024

type vec3 [3]float32
type fixture struct {
	name                  string
	origin, dir, min, max vec3
	start, end            float32
	invalid               bool
}
type interval struct {
	cell       [3]int32
	start, end float64
}

func fixtures() []fixture {
	var out []fixture
	for _, count := range []int{15, 16, 17, 63, 64, 65, 511, 512, 513, 600} {
		for _, sign := range []float32{1, -1} {
			f := fixture{name: fmt.Sprintf("axis/%d/%g", count, sign), origin: vec3{0, 4, 4}, dir: vec3{sign, 0, 0}, min: vec3{0, 0, 0}, max: vec3{float32(count * 32), 8, 8}, start: .001, end: float32(count * 32)}
			if sign < 0 {
				f.origin[0] = f.max[0]
			}
			out = append(out, f)
		}
	}
	out = append(out,
		fixture{name: "near-negative-sector-face", origin: vec3{10, 20.7692317963, 56.6666641235}, dir: vec3{-.2489039153, -.3990150988, -1.385720849}, min: vec3{0, 0, 0}, max: vec3{8, 8, 1}, start: 40.1726303101, end: 40.1761474609},
		fixture{name: "two-axis-ties", origin: vec3{4, 4, 4}, dir: vec3{1, 1, 0}, min: vec3{0, 0, 0}, max: vec3{128, 128, 8}, start: .001, end: 124},
		fixture{name: "three-axis-ties", origin: vec3{4, 4, 4}, dir: vec3{1, 1, 1}, min: vec3{0, 0, 0}, max: vec3{128, 128, 128}, start: .001, end: 124},
		fixture{name: "negative-corner-ties", origin: vec3{0, 0, 0}, dir: vec3{-1, -1, -1}, min: vec3{-128, -128, -128}, max: vec3{0, 0, 0}, start: 0, end: 128},
		fixture{name: "positive-boundary-start", origin: vec3{32, 4, 4}, dir: vec3{1, 0, 0}, min: vec3{-64, 0, 0}, max: vec3{128, 8, 8}, start: 0, end: 96},
		fixture{name: "negative-boundary-start", origin: vec3{32, 4, 4}, dir: vec3{-1, 0, 0}, min: vec3{-64, 0, 0}, max: vec3{128, 8, 8}, start: 0, end: 96},
		fixture{name: "unnormalized-transform", origin: vec3{4, 4, 4}, dir: vec3{2, .5, .25}, min: vec3{0, 0, 0}, max: vec3{256, 128, 64}, start: .001, end: 126},
		fixture{name: "tiny-positive", origin: vec3{4, 4, 4}, dir: vec3{1e-7, 0, 0}, min: vec3{0, 0, 0}, max: vec3{96, 8, 8}, start: 0, end: 9.2e8},
		fixture{name: "tiny-negative", origin: vec3{92, 4, 4}, dir: vec3{-1e-7, 0, 0}, min: vec3{0, 0, 0}, max: vec3{96, 8, 8}, start: 0, end: 9.2e8},
		fixture{name: "tiny-secondary", origin: vec3{4, 4, 4}, dir: vec3{1, 1e-7, -1e-7}, min: vec3{0, 0, 0}, max: vec3{192, 8, 8}, start: .001, end: 188},
		fixture{name: "large-local-offset", origin: vec3{65536, 4, 4}, dir: vec3{1, 0, 0}, min: vec3{65536, 0, 0}, max: vec3{65536 + 640, 8, 8}, start: .001, end: 640},
		// Delta=0.5 is below the ULP of t=2^24. Integer progress must
		// terminate even when adjacent boundary times round to equal f32s.
		fixture{name: "float32-time-stagnation", origin: vec3{-1073741824, 4, 4}, dir: vec3{64, 0, 0}, min: vec3{0, 0, 0}, max: vec3{512, 8, 8}, start: 16777216, end: 16777224},
	)
	base := fixture{name: "invalid", origin: vec3{4, 4, 4}, dir: vec3{1, 0, 0}, min: vec3{0, 0, 0}, max: vec3{96, 8, 8}, start: 0, end: 92, invalid: true}
	for _, kind := range []string{"nan-origin", "inf-direction", "nan-bounds", "inf-end", "zero-direction", "zero-axis-outside", "unordered-bounds", "empty-interval", "degenerate-clipped", "flat-bounds", "unsafe-coordinate", "i32-exclusive-upper", "below-i32-lower", "derived-overflow"} {
		f := base
		f.name = kind
		switch kind {
		case "nan-origin":
			f.origin[0] = float32(math.NaN())
		case "inf-direction":
			f.dir[0] = float32(math.Inf(1))
		case "nan-bounds":
			f.min[1] = float32(math.NaN())
		case "inf-end":
			f.end = float32(math.Inf(1))
		case "zero-direction":
			f.dir = vec3{}
		case "zero-axis-outside":
			f.origin[1] = 64
		case "unordered-bounds":
			f.min[0] = 100
		case "empty-interval":
			f.start = f.end
		case "degenerate-clipped":
			f.max = f.min
			f.end = 0
		case "flat-bounds":
			f.max[1] = f.min[1]
		case "unsafe-coordinate":
			f.min[0] = 1e20
			f.max[0] = 2e20
			f.origin[0] = 1e20
		case "i32-exclusive-upper":
			f.min[0] = 68719476736
			f.max[0] = f.min[0] + 8192
			f.origin[0] = f.min[0]
		case "below-i32-lower":
			f.min[0] = -68719476736 - 8192
			f.max[0] = f.min[0] + 4096
			f.origin[0] = f.min[0]
		case "derived-overflow":
			f.dir[0] = math.MaxFloat32
			f.start = 2
			f.end = 3
		}
		out = append(out, f)
	}
	return out
}

// Enumerate individual sector slabs in float64, independently of DDA stepping,
// direction reciprocal clamps, accumulated boundary times and tie ordering.
func oracle(f fixture) []interval {
	if f.invalid {
		return nil
	}
	lo, hi := [3]int32{}, [3]int32{}
	for a := 0; a < 3; a++ {
		lo[a] = int32(math.Floor(float64(f.min[a]) / 32))
		hi[a] = int32(math.Floor(float64(f.max[a]) / 32))
	}
	var out []interval
	for x := lo[0]; x <= hi[0]; x++ {
		for y := lo[1]; y <= hi[1]; y++ {
			for z := lo[2]; z <= hi[2]; z++ {
				cell := [3]int32{x, y, z}
				near, far := float64(f.start), float64(f.end)
				hit := true
				for a := 0; a < 3; a++ {
					lower, upper := float64(cell[a])*32, float64(cell[a]+1)*32
					lower = math.Max(lower, float64(f.min[a]))
					upper = math.Min(upper, float64(f.max[a]))
					if lower >= upper {
						hit = false
					}
					o, d := float64(f.origin[a]), float64(f.dir[a])
					if d == 0 {
						if o < lower || o >= upper {
							hit = false
						}
						continue
					}
					t0, t1 := (lower-o)/d, (upper-o)/d
					if t0 > t1 {
						t0, t1 = t1, t0
					}
					near = math.Max(near, t0)
					far = math.Min(far, t1)
				}
				if hit && near < far {
					out = append(out, interval{cell, near, far})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].start < out[j].start })
	return out
}

const probeWGSL = `
struct W3bInput { origin:vec4<f32>, direction:vec4<f32>, lower:vec4<f32>, upper:vec4<f32>, clip:vec4<f32> }
@group(3) @binding(14) var<storage,read> w3b_inputs:array<W3bInput>;
@group(3) @binding(15) var<storage,read_write> w3b_outputs:array<vec4<u32>>;
@compute @workgroup_size(1) fn w3b_probe(@builtin(global_invocation_id) id:vec3<u32>) {
 if(id.x>=arrayLength(&w3b_inputs)){return;}
 let input=w3b_inputs[id.x];let base=id.x*2049u;
 var state=sector_dda_init(input.origin.xyz,input.direction.xyz,input.lower.xyz,input.upper.xyz,input.clip.x,input.clip.y);
 let initial_active=state.running;var count=0u;
 while(state.running && count<1024u){
  w3b_outputs[base+1u+count*2u]=vec4<u32>(bitcast<vec3<u32>>(state.position),0u);
  w3b_outputs[base+2u+count*2u]=vec4<u32>(bitcast<u32>(state.t),bitcast<u32>(state.exit_t),0u,0u);
  count+=1u;sector_dda_advance(&state);
 }
 w3b_outputs[base]=vec4<u32>(count,select(0u,1u,initial_active),select(0u,1u,state.running),0u);
}`

func check(f fixture, data []byte) error {
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset:]) }
	count := int(word(0))
	if word(8) != 0 {
		return fmt.Errorf("probe record guard exceeded (%d)", count)
	}
	if f.invalid {
		if count != 0 || word(4) != 0 {
			return fmt.Errorf("invalid input remained active (%d visits)", count)
		}
		return nil
	}
	budget := 1
	clipLo, clipHi := [3]int32{}, [3]int32{}
	for a := 0; a < 3; a++ {
		startPoint := float64(f.origin[a]) + float64(f.dir[a])*float64(f.start)
		endPoint := float64(f.origin[a]) + float64(f.dir[a])*float64(f.end)
		startCell, endCell := math.Floor(startPoint/32), math.Floor(endPoint/32)
		if f.dir[a] < 0 && startPoint == startCell*32 {
			startCell--
		}
		lower := math.Max(math.Floor(float64(f.min[a])/32), math.Min(startCell, endCell))
		upper := math.Min(math.Floor(float64(f.max[a])/32), math.Max(startCell, endCell))
		clipLo[a], clipHi[a] = int32(lower), int32(upper)
		budget += int(upper - lower)
	}
	if count == 0 || count > budget {
		return fmt.Errorf("visits=%d exceeds structural bound=%d or valid ray rejected", count, budget)
	}
	want := oracle(f)
	seen := map[[3]int32]interval{}
	positive := map[[3]int32]interval{}
	var previous interval
	for i := 0; i < count; i++ {
		offset := 16 + i*32
		got := interval{cell: [3]int32{int32(word(offset)), int32(word(offset + 4)), int32(word(offset + 8))}, start: float64(math.Float32frombits(word(offset + 16))), end: float64(math.Float32frombits(word(offset + 20)))}
		for a := 0; a < 3; a++ {
			if got.cell[a] < clipLo[a] || got.cell[a] > clipHi[a] {
				return fmt.Errorf("coordinate %v exceeds clipped limits %v..%v", got.cell, clipLo, clipHi)
			}
		}
		if math.IsNaN(got.start) || math.IsNaN(got.end) || math.IsInf(got.start, 0) || math.IsInf(got.end, 0) || got.end < got.start {
			return fmt.Errorf("invalid interval %+v", got)
		}
		if _, exists := seen[got.cell]; exists {
			return fmt.Errorf("repeated coordinate %v", got.cell)
		}
		seen[got.cell] = got
		if i > 0 {
			changed := 0
			for a := 0; a < 3; a++ {
				delta := got.cell[a] - previous.cell[a]
				if delta != 0 {
					changed++
					if f.dir[a] == 0 || delta != int32(math.Copysign(1, float64(f.dir[a]))) {
						return fmt.Errorf("nonmonotone coordinate %+v -> %+v", previous, got)
					}
				}
			}
			if changed != 1 || got.start < previous.start {
				return fmt.Errorf("no single-axis/time progress %+v -> %+v", previous, got)
			}
		}
		if got.end > got.start {
			positive[got.cell] = got
		}
		previous = got
	}
	// At large t adjacent crossings can round to the same float32. Verify the
	// coordinates still cover every float64 geometric interval, including those
	// collapsed by output precision, rather than pretending all are positive.
	positiveCount := len(positive)
	ordinary := true
	for _, w := range want {
		g, exists := seen[w.cell]
		if !exists {
			return fmt.Errorf("missed float64 sector %+v", w)
		}
		// Compare every interval, including collapsed ones. A zero-width
		// implementation must not pass merely by visiting correct coordinates.
		ulp := func(value float64) float64 {
			v := float32(value)
			return math.Abs(float64(math.Nextafter32(v, float32(math.Inf(1)))) - float64(v))
		}
		tolerance := math.Max(1e-5, 4*math.Max(ulp(w.start), ulp(w.end)))
		if math.Abs(g.start-w.start) > tolerance || math.Abs(g.end-w.end) > tolerance {
			return fmt.Errorf("sector interval %+v != float64 %+v (tolerance %g)", g, w, tolerance)
		}
		if float32(w.end) > float32(w.start) {
			if g.end <= g.start {
				return fmt.Errorf("representable positive interval collapsed: %+v != %+v", g, w)
			}
		} else {
			ordinary = false
		}
		delete(positive, w.cell)
	}
	if ordinary && positiveCount != len(want) {
		return fmt.Errorf("ordinary positive interval count=%d, float64 count=%d", positiveCount, len(want))
	}
	if len(positive) != 0 {
		return fmt.Errorf("unexpected positive geometric sectors: %+v", positive)
	}
	for _, limit := range []int{16, 64, 512} {
		if len(want) <= limit {
			continue
		}
		// Treat only the last geometric sector as occupied. It must remain
		// reachable after the legacy visit budgets; all preceding sectors miss.
		last := want[len(want)-1].cell
		index := -1
		for i := 0; i < count; i++ {
			o := 16 + i*32
			if [3]int32{int32(word(o)), int32(word(o + 4)), int32(word(o + 8))} == last {
				index = i
			}
		}
		if index < limit {
			return fmt.Errorf("late candidate %v did not exercise >%d visits (index %d)", last, limit, index)
		}
	}
	return nil
}

func run(device *wgpu.Device, queue *wgpu.Queue, name, source string, cases []fixture) error {
	module, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "W3b " + name, WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: source + probeWGSL}})
	if err != nil {
		return fmt.Errorf("compile helper probe: %w", err)
	}
	defer module.Release()
	pipeline, err := device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{Compute: wgpu.ProgrammableStageDescriptor{Module: module, EntryPoint: "w3b_probe"}})
	if err != nil {
		return fmt.Errorf("helper pipeline: %w", err)
	}
	defer pipeline.Release()
	input := make([]byte, len(cases)*80)
	for i, f := range cases {
		for v, values := range []vec3{f.origin, f.dir, f.min, f.max, {f.start, f.end, 0}} {
			for a, value := range values {
				binary.LittleEndian.PutUint32(input[i*80+v*16+a*4:], math.Float32bits(value))
			}
		}
	}
	in, err := device.CreateBuffer(&wgpu.BufferDescriptor{Size: uint64(len(input)), Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	defer in.Release()
	if err = queue.WriteBuffer(in, 0, input); err != nil {
		return err
	}
	size := uint64(len(cases) * (1 + probeCapacity*2) * 16)
	out, err := device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageStorage | wgpu.BufferUsageCopySrc})
	if err != nil {
		return err
	}
	defer out.Release()
	read, err := device.CreateBuffer(&wgpu.BufferDescriptor{Size: size, Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst})
	if err != nil {
		return err
	}
	defer read.Release()
	groups := make([]*wgpu.BindGroup, 4)
	for i := range groups {
		desc := &wgpu.BindGroupDescriptor{Layout: pipeline.GetBindGroupLayout(uint32(i))}
		defer desc.Layout.Release()
		if i == 3 {
			desc.Entries = []wgpu.BindGroupEntry{{Binding: 14, Buffer: in, Size: wgpu.WholeSize}, {Binding: 15, Buffer: out, Size: wgpu.WholeSize}}
		}
		groups[i], err = device.CreateBindGroup(desc)
		if err != nil {
			return err
		}
		defer groups[i].Release()
	}
	encoder, err := device.CreateCommandEncoder(nil)
	if err != nil {
		return err
	}
	defer encoder.Release()
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(pipeline)
	for i, g := range groups {
		pass.SetBindGroup(uint32(i), g, nil)
	}
	pass.DispatchWorkgroups(uint32(len(cases)), 1, 1)
	pass.End()
	pass.Release()
	encoder.CopyBufferToBuffer(out, 0, read, 0, size)
	command, err := encoder.Finish(nil)
	if err != nil {
		return err
	}
	defer command.Release()
	queue.Submit(command)
	done := false
	status := wgpu.BufferMapAsyncStatusUnknown
	if err = read.MapAsync(wgpu.MapModeRead, 0, size, func(s wgpu.BufferMapAsyncStatus) { done = true; status = s }); err != nil {
		return err
	}
	device.Poll(true, nil)
	if !done || status != wgpu.BufferMapAsyncStatusSuccess {
		return fmt.Errorf("readback status %v, done %v", status, done)
	}
	data := read.GetMappedRange(0, uint(size))
	defer read.Unmap()
	for i, f := range cases {
		if err = check(f, data[i*(1+probeCapacity*2)*16:]); err != nil {
			return fmt.Errorf("%s: %w", f.name, err)
		}
	}
	fmt.Printf("%s: %d helper fixtures PASS\n", name, len(cases))
	return nil
}

func main() {
	dump := flag.String("dump-wgsl", "", "save the exact three combined probe modules and exit before requesting a device")
	flag.Parse()
	modules := []struct{ name, source string }{{"gbuffer", shaders.GBufferWGSL}, {"shadow", shaders.ShadowMapWGSL}, {"transparent", shaders.TransparentOverlayWGSL}}
	if *dump != "" {
		if err := os.MkdirAll(*dump, 0755); err != nil {
			panic(err)
		}
		for _, module := range modules {
			if err := os.WriteFile(filepath.Join(*dump, "w3b-"+module.name+".wgsl"), []byte(module.source+probeWGSL), 0644); err != nil {
				panic(err)
			}
		}
		return
	}
	wgpu.SetLogLevel(wgpu.LogLevelError)
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{PowerPreference: wgpu.PowerPreferenceHighPerformance})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer adapter.Release()
	device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{RequiredLimits: &wgpu.RequiredLimits{Limits: adapter.GetLimits().Limits}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer device.Release()
	queue := device.GetQueue()
	defer queue.Release()
	failed := false
	for _, shader := range modules {
		if err = run(device, queue, shader.name, shader.source, fixtures()); err != nil {
			failed = true
			fmt.Fprintf(os.Stderr, "%s: FAIL: %v\n", shader.name, err)
		}
	}
	if failed {
		os.Exit(1)
	}
}
