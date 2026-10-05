//go:build ignore

// Native owning-grid invariant probe. Initialization, loop condition, counter
// and advancement come from production WGSL. Only payload/shading work is
// replaced by a coordinate trace; this does not test rendered material aliasing.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/gekko3d/gekko/voxelrt/rt/shaders"
)

const probeCapacity = 1024

type vec3 [3]float32
type fixture struct {
	name                  string
	origin, dir, min, max vec3
	start, end            float32
	numerical             bool
}
type interval struct {
	cell       [3]int32
	start, end float64
}
type variant struct {
	name, source string
	grid         int
	micro        bool
	ordinal      int
}

var activeVariant variant

// Balanced braces preserve the complete original stepping block, including its
// tie policy and trailing EPS/clip checks. Unexpected source shape fails closed.
func closingBrace(source string, open int) int {
	depth := 0
	for i := open; i < len(source); i++ {
		switch source[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	panic("unbalanced production loop")
}
func loopSource(v variant) string {
	coordinate, time, marker := "brick_pos", "t_brick", "var t_brick = t_curr;"
	if v.micro {
		coordinate, time, marker = "voxel_pos", "t_micro", "var t_micro = t_brick;"
	}
	start := 0
	for i := 0; i <= v.ordinal; i++ {
		relative := strings.Index(v.source[start:], marker)
		if relative < 0 {
			panic("production initialization not found: " + v.name)
		}
		start += relative
		if i < v.ordinal {
			start += len(marker)
		}
	}
	while := start + strings.Index(v.source[start:], "while (")
	if while < start {
		panic("production while not found")
	}
	open := while + strings.Index(v.source[while:], "{")
	end := closingBrace(v.source, open)
	body := v.source[open+1 : end]
	stepName := "t_max_brick"
	if v.micro {
		stepName = "t_max_micro"
	}
	advance := strings.LastIndex(body, "if ("+stepName+".x < "+stepName+".y)")
	if advance < 0 {
		panic("production advancement not found: " + v.name)
	}
	// Keep the original counter increment when present. No counter is created
	// after production adopts coordinate-bounded conditions.
	prefix := ""
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "iter_") || strings.HasPrefix(trimmed, "it_") {
		semi := strings.Index(trimmed, ";")
		if semi < 0 {
			panic("counter increment lacks semicolon")
		}
		prefix = trimmed[:semi+1]
	}
	init := v.source[start : open+1]
	prelude := `const EPS:f32=1e-3;const BRICK_SIZE:f32=8.0;
struct Ray {origin:vec3<f32>,dir:vec3<f32>,inv_dir:vec3<f32>}
struct BrickRecord {occupancy_mask_lo:u32,occupancy_mask_hi:u32}
struct W3bInput {origin:vec4<f32>,direction:vec4<f32>,lower:vec4<f32>,upper:vec4<f32>,clip:vec4<f32>}
@group(3) @binding(14) var<storage,read> w3b_inputs:array<W3bInput>;
@group(3) @binding(15) var<storage,read_write> w3b_outputs:array<vec4<u32>>;
@compute @workgroup_size(1) fn w3b_probe(@builtin(global_invocation_id) id:vec3<u32>){
 if(id.x>=arrayLength(&w3b_inputs)){return;}let input=w3b_inputs[id.x];let base=id.x*2049u;
 let dir=input.direction.xyz;let safe=select(dir,select(vec3<f32>(1e-6),vec3<f32>(-1e-6),dir<vec3<f32>(0.0)),abs(dir)<vec3<f32>(1e-6));
 let inv_dir=1.0/safe;let ray=Ray(input.origin.xyz,dir,inv_dir);let ray_os=ray;let step=vec3<i32>(sign(dir));
 let sector_origin=input.lower.xyz;let bvid=vec3<u32>(0u);let brick=BrickRecord(0u,0u);
 let t_curr=input.clip.x;let t_sector_exit=input.clip.y;let t_brick_exit=input.clip.y;let t_limit=input.clip.y;
 var count=0u;var overflow=0u;
`
	if v.micro {
		prelude += "var t_brick=t_curr;\n"
	} else if !strings.Contains(init, "let t_delta_brick") {
		prelude += "let t_delta_brick=abs(BRICK_SIZE*inv_dir);\n"
	}
	trace := fmt.Sprintf(`
 if(count>=1024u){overflow=1u;break;}
 w3b_outputs[base+1u+count*2u]=vec4<u32>(bitcast<vec3<u32>>(%s),0u);
 let trace_exit=min(min(%s.x,%s.y),min(%s.z,input.clip.y));
 w3b_outputs[base+2u+count*2u]=vec4<u32>(bitcast<u32>(%s),bitcast<u32>(trace_exit),0u,0u);count+=1u;
`, coordinate, stepName, stepName, stepName, time)
	return prelude + init + prefix + trace + body[advance:] + "\n}\nw3b_outputs[base]=vec4<u32>(count,1u,overflow,0u);\n}\n"
}

func cases(grid int, micro bool) []fixture {
	size := float32(grid)
	if !micro {
		size *= 8
	}
	var out []fixture
	for axis := 0; axis < 3; axis++ {
		for _, sign := range []float32{-1, 1} {
			f := fixture{name: fmt.Sprintf("axis%d/%g", axis, sign), origin: vec3{.5, .5, .5}, min: vec3{}, max: vec3{size, size, size}, start: .001, end: size - .5}
			f.dir[axis] = sign
			if sign < 0 {
				f.origin[axis] = size - .5
			}
			out = append(out, f)
		}
	}
	for _, sign := range []float32{-1, 1} {
		f := fixture{name: fmt.Sprintf("diagonal/%g", sign), origin: vec3{.5, .5, .5}, dir: vec3{sign, sign, sign}, max: vec3{size, size, size}, start: .001, end: size - .5}
		if sign < 0 {
			f.origin = vec3{size - .5, size - .5, size - .5}
		}
		out = append(out, f)
		two := f
		two.name = fmt.Sprintf("two-axis-ties/%g", sign)
		two.dir[2] = 0
		two.origin[2] = .5
		out = append(out, two)
	}
	out = append(out, fixture{name: "short-parent-clip", origin: vec3{.5, .5, .5}, dir: vec3{1, .5, .25}, max: vec3{size, size, size}, start: .001, end: .2})
	out = append(out, fixture{name: "mixed-sign-unnormalized", origin: vec3{.5, size - .5, .5}, dir: vec3{2, -.5, .25}, max: vec3{size, size, size}, start: .001, end: (size - .5) / 2})
	out = append(out, fixture{name: "translated-negative-grid", origin: vec3{-31.5, -31.5, -31.5}, dir: vec3{1, 1, 1}, min: vec3{-32, -32, -32}, max: vec3{-32 + size, -32 + size, -32 + size}, start: .001, end: size - .5})
	out = append(out, fixture{name: "float32-time-stagnation", origin: vec3{-16777216, .5, .5}, dir: vec3{1, 0, 0}, max: vec3{size, size, size}, start: 16777216, end: 16777216 + size, numerical: true})
	// Accepted numerical clip: actual direction is tiny, while existing inner
	// traversal intentionally retains the safe reciprocal. This isolates the
	// owning-grid boundary contract rather than claiming exact geometry.
	out = append(out, fixture{name: "tiny-direction-grid-overrun", origin: vec3{4.076377868652344, .5, .5}, dir: vec3{7.757638087468877e-8, 0, 0}, max: vec3{size, size, size}, start: .001, end: float32(float64(size-4.076377868652344) / 7.757638087468877e-8), numerical: true})
	out = append(out, fixture{name: "tiny-negative-direction-grid-overrun", origin: vec3{size - 4.076377868652344, .5, .5}, dir: vec3{-7.757638087468877e-8, 0, 0}, max: vec3{size, size, size}, start: .001, end: float32(float64(size-4.076377868652344) / 7.757638087468877e-8), numerical: true})
	return out
}

func oracle(f fixture, grid int, micro bool) []interval {
	cellSize := float64(1)
	if !micro {
		cellSize = 8
	}
	var out []interval
	for x := 0; x < grid; x++ {
		for y := 0; y < grid; y++ {
			for z := 0; z < grid; z++ {
				cell := [3]int32{int32(x), int32(y), int32(z)}
				near, far := float64(f.start), float64(f.end)
				hit := true
				for a := 0; a < 3; a++ {
					lower, upper := float64(f.min[a])+float64(cell[a])*cellSize, float64(f.min[a])+float64(cell[a]+1)*cellSize
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

func check(f fixture, data []byte) error {
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset:]) }
	count := int(word(0))
	if word(8) != 0 {
		return fmt.Errorf("diagnostic record guard reached")
	}
	grid := activeVariant.grid
	bound := 1 + 3*(grid-1)
	var previous [3]int32
	seen := map[[3]int32]bool{}
	positive := map[[3]int32]interval{}
	for i := 0; i < count; i++ {
		o := 16 + i*32
		cell := [3]int32{int32(word(o)), int32(word(o + 4)), int32(word(o + 8))}
		if seen[cell] {
			return fmt.Errorf("repeated coordinate %v", cell)
		}
		seen[cell] = true
		for a := 0; a < 3; a++ {
			if cell[a] < 0 || cell[a] >= int32(grid) {
				return fmt.Errorf("production loop entered out-of-grid coordinate %v at iteration %d (grid [0,%d)^3)", cell, i, grid)
			}
		}
		if i > 0 {
			changed := 0
			for a := 0; a < 3; a++ {
				delta := cell[a] - previous[a]
				if delta != 0 {
					changed++
					if f.dir[a] == 0 || delta != int32(math.Copysign(1, float64(f.dir[a]))) {
						return fmt.Errorf("invalid signed progress %v -> %v", previous, cell)
					}
				}
			}
			if changed != 1 {
				return fmt.Errorf("coordinate did not advance exactly one axis: %v -> %v", previous, cell)
			}
		}
		start, end := float64(math.Float32frombits(word(o+16))), float64(math.Float32frombits(word(o+20)))
		if math.IsNaN(start) || math.IsNaN(end) || math.IsInf(start, 0) || math.IsInf(end, 0) {
			return fmt.Errorf("nonfinite traced interval")
		}
		if end > start {
			positive[cell] = interval{cell, start, end}
		}
		previous = cell
	}
	if count == 0 || count > bound {
		return fmt.Errorf("owning-grid visits %d exceed structural bound %d", count, bound)
	}
	if !f.numerical {
		want := oracle(f, grid, activeVariant.micro)
		if len(positive) != len(want) {
			return fmt.Errorf("positive interval count %d != independent slab count %d", len(positive), len(want))
		}
		for _, w := range want {
			g, ok := positive[w.cell]
			if !ok {
				return fmt.Errorf("missing geometric cell %v", w.cell)
			}
			tolerance := .002
			if math.Abs(g.start-w.start) > tolerance || math.Abs(g.end-w.end) > tolerance {
				return fmt.Errorf("interval %+v != float64 slab %+v", g, w)
			}
		}
	}
	return nil
}
func run(device *wgpu.Device, queue *wgpu.Queue, name, source string, cases []fixture) error {
	module, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{Label: "W3c " + name, WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: source}})
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
	passed := 0
	var failures []string
	for i, f := range cases {
		if err = check(f, data[i*(1+probeCapacity*2)*16:]); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", f.name, err))
		} else {
			passed++
		}
	}
	fmt.Printf("%s: %d/%d source-derived loop fixtures PASS\n", name, passed, len(cases))
	if len(failures) != 0 {
		return fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return nil
}

func main() {
	dump := flag.String("dump-wgsl", "", "save exact source-derived probe modules and exit")
	flag.Parse()
	variants := []variant{{"gbuffer-brick", shaders.GBufferWGSL, 4, false, 0}, {"shadow-brick", shaders.ShadowMapWGSL, 4, false, 0}, {"transparent-brick", shaders.TransparentOverlayWGSL, 4, false, 0}, {"gbuffer-voxel", shaders.GBufferWGSL, 8, true, 0}, {"shadow-voxel", shaders.ShadowMapWGSL, 8, true, 0}, {"transparent-solid-voxel", shaders.TransparentOverlayWGSL, 8, true, 0}, {"transparent-mixed-voxel", shaders.TransparentOverlayWGSL, 8, true, 1}}
	if *dump != "" {
		if err := os.MkdirAll(*dump, 0755); err != nil {
			panic(err)
		}
		for _, v := range variants {
			if err := os.WriteFile(filepath.Join(*dump, v.name+".wgsl"), []byte(loopSource(v)), 0644); err != nil {
				panic(err)
			}
		}
		return
	}
	instance := wgpu.CreateInstance(nil)
	defer instance.Release()
	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{PowerPreference: wgpu.PowerPreferenceHighPerformance})
	if err != nil {
		panic(err)
	}
	defer adapter.Release()
	device, err := adapter.RequestDevice(&wgpu.DeviceDescriptor{RequiredLimits: &wgpu.RequiredLimits{Limits: adapter.GetLimits().Limits}})
	if err != nil {
		panic(err)
	}
	defer device.Release()
	queue := device.GetQueue()
	defer queue.Release()
	failed := false
	for _, v := range variants {
		activeVariant = v
		if err := run(device, queue, v.name, loopSource(v), cases(v.grid, v.micro)); err != nil {
			failed = true
			fmt.Printf("%s: FAIL: %v\n", v.name, err)
		}
	}
	if failed {
		os.Exit(1)
	}
}
