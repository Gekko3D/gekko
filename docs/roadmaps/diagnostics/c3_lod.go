//go:build ignore

// Diagnostic only; excluded from normal builds. Run from the engine module:
// env GOCACHE=/tmp/gekko3d-gocache go run docs/roadmaps/diagnostics/c3_lod.go
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	gekko "github.com/gekko3d/gekko"
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"image"
	"image/color"
	"image/png"
	"math/bits"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Stats struct {
	Voxels, Bricks, Sectors, SolidBricks, UniformSparseBricks, MixedBricks int
	EstimatedFreshGeometryUploadBytes                                      int64
}
type Quality struct {
	OccupancyORCells, SampledCells, LostOccupiedCells, MixedMaterialCells, MixedMaterialCellsRetained int
	SourceVoxelsInSurvivingCells, SourceVoxelsInLostCells, EmptySourceVoxelsFilledWithinRetainedCells int
	SampleCenterMismatch                                                                              int
}
type Run struct {
	Milliseconds    float64
	TotalAllocBytes uint64
}
type Shape struct {
	ContentID       string
	Parts           []string
	Full, Sampled   Stats
	Quality         Quality
	Runs            []Run
	Refused         bool
	RuntimeEligible bool
}
type Asset struct {
	Input, InputSHA256, Header                        string
	PartReferences                                    int
	Shapes                                            []Shape
	FullUnique, SampledUnique, RuntimeEffectiveUnique Stats
	RuntimeEligibleShapes, RuntimeFallbackShapes      int
	Runs                                              []Run
	MedianMilliseconds                                float64
	MedianTotalAllocBytes                             uint64
}
type Probe struct {
	Name          string
	Full, Sampled Stats
	Quality       Quality
	PNG           string
}
type Evidence struct {
	GoVersion, Platform, Method, UploadEstimate, ProjectionLegend string
	GOMAXPROCS, Runs                                              int
	Assets                                                        []Asset
	Probes                                                        []Probe
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func floor2(n int) int {
	if n < 0 && n%2 != 0 {
		return n/2 - 1
	}
	return n / 2
}
func visit(m *volume.XBrickMap, f func(int, int, int, uint8)) {
	keys := make([][3]int, 0, len(m.Sectors))
	for k := range m.Sectors {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		for a := 0; a < 3; a++ {
			if keys[i][a] != keys[j][a] {
				return keys[i][a] < keys[j][a]
			}
		}
		return false
	})
	for _, k := range keys {
		s := m.Sectors[k]
		for i := 0; i < 64; i++ {
			b := s.GetBrick(i%4, i/4%4, i/16)
			if b == nil {
				continue
			}
			for z := 0; z < 8; z++ {
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						v := b.VoxelValue(x, y, z)
						if v != 0 {
							f(k[0]*32+i%4*8+x, k[1]*32+i/4%4*8+y, k[2]*32+i/16*8+z, v)
						}
					}
				}
			}
		}
	}
}
func fingerprint(m *volume.XBrickMap) string {
	h := sha256.New()
	var b [25]byte
	visit(m, func(x, y, z int, v uint8) {
		binary.LittleEndian.PutUint64(b[0:8], uint64(x))
		binary.LittleEndian.PutUint64(b[8:16], uint64(y))
		binary.LittleEndian.PutUint64(b[16:24], uint64(z))
		b[24] = v
		h.Write(b[:])
	})
	return hex.EncodeToString(h.Sum(nil))
}
func stats(m *volume.XBrickMap) Stats {
	s := Stats{Sectors: len(m.Sectors)}
	s.EstimatedFreshGeometryUploadBytes = int64(s.Sectors) * (32 + 64*32)
	visit(m, func(_, _, _ int, _ uint8) { s.Voxels++ })
	if s.Voxels != m.GetVoxelCount() {
		panic("logical payload count disagrees with GetVoxelCount")
	}
	for _, sec := range m.Sectors {
		for _, b := range sec.PackedBricks {
			s.Bricks++
			s.EstimatedFreshGeometryUploadBytes += int64(volume.VoxelAuxWordCount * 4)
			if b.Flags&volume.BrickFlagSolid != 0 {
				s.SolidBricks++
			} else if b.Flags&volume.BrickFlagUniformMaterial != 0 {
				s.UniformSparseBricks++
			} else {
				s.MixedBricks++
				s.EstimatedFreshGeometryUploadBytes += 512
			}
		}
	}
	return s
}

type Bin struct {
	Materials [4]uint64
	Count     int
}

func bins(m *volume.XBrickMap) map[[3]int]Bin {
	out := map[[3]int]Bin{}
	visit(m, func(x, y, z int, v uint8) {
		k := [3]int{floor2(x), floor2(y), floor2(z)}
		b := out[k]
		b.Materials[v/64] |= uint64(1) << uint(v%64)
		b.Count++
		out[k] = b
	})
	return out
}
func mixed(b Bin) bool {
	n := 0
	for _, w := range b.Materials {
		n += bits.OnesCount64(w)
	}
	return n > 1
}
func quality(src, lod *volume.XBrickMap) Quality {
	bs := bins(src)
	q := Quality{OccupancyORCells: len(bs)}
	visit(lod, func(x, y, z int, v uint8) {
		q.SampledCells++
		found, w := src.GetVoxel(2*x+1, 2*y+1, 2*z+1)
		if !found || v != w {
			q.SampleCenterMismatch++
		}
	})
	for k, b := range bs {
		found, sampledValue := lod.GetVoxel(k[0], k[1], k[2])
		centerFound, centerValue := src.GetVoxel(2*k[0]+1, 2*k[1]+1, 2*k[2]+1)
		if found != centerFound || sampledValue != centerValue {
			panic("sample completeness/value mismatch")
		}
		if mixed(b) {
			q.MixedMaterialCells++
			if found {
				q.MixedMaterialCellsRetained++
			}
		}
		if !found {
			q.LostOccupiedCells++
			q.SourceVoxelsInLostCells += b.Count
		} else {
			q.SourceVoxelsInSurvivingCells += b.Count
		}
	}
	q.EmptySourceVoxelsFilledWithinRetainedCells = 8*q.SampledCells - q.SourceVoxelsInSurvivingCells
	if q.SampleCenterMismatch != 0 {
		panic("resampling not equivalent to odd-center sampling")
	}
	return q
}
func geometry(shape *content.CompiledAssetShapeDef) *volume.XBrickMap {
	m := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, b := range shape.Bricks {
			i := 0
			for w, mask := range b.Occupancy {
				for mask != 0 {
					linear := w*64 + bits.TrailingZeros64(mask)
					if !yield(volume.VoxelWrite{X: int(b.Coord[0])*8 + linear%8, Y: int(b.Coord[1])*8 + linear/8%8, Z: int(b.Coord[2])*8 + linear/64, Value: b.Values[i]}) {
						return
					}
					i++
					mask &= mask - 1
				}
			}
		}
	})
	m.ComputeAABB()
	m.ClearDirty()
	return m
}
func add(a *Stats, b Stats) {
	a.Voxels += b.Voxels
	a.Bricks += b.Bricks
	a.Sectors += b.Sectors
	a.SolidBricks += b.SolidBricks
	a.UniformSparseBricks += b.UniformSparseBricks
	a.MixedBricks += b.MixedBricks
	a.EstimatedFreshGeometryUploadBytes += b.EstimatedFreshGeometryUploadBytes
}
func asset(input, root string, n int) Asset {
	raw, err := os.ReadFile(input)
	must(err)
	hash := sha256.Sum256(raw)
	a := Asset{Input: input, InputSHA256: hex.EncodeToString(hash[:]), Header: filepath.Join(root, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input)), "asset.gkassetc"), Runs: make([]Run, n)}
	_, err = gekko.CompileAuthoredAsset(input, a.Header, nil)
	must(err)
	hdr, _, err := content.LoadCompiledAssetHeader(a.Header, nil)
	must(err)
	a.PartReferences = len(hdr.Shapes)
	byID := map[string]int{}
	for _, ref := range hdr.Shapes {
		if i, ok := byID[ref.ContentID]; ok {
			a.Shapes[i].Parts = append(a.Shapes[i].Parts, ref.PartID)
			continue
		}
		def, info, err := content.LoadCompiledAssetShape(filepath.Join(filepath.Dir(a.Header), ref.Path), nil)
		must(err)
		if info.ContentID != ref.ContentID {
			panic("identity mismatch")
		}
		src := geometry(def)
		sourceHash := fingerprint(src)
		s := Shape{ContentID: ref.ContentID, Parts: []string{ref.PartID}, Full: stats(src)}
		var last *volume.XBrickMap
		for r := 0; r < n; r++ {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			last = src.Resample(.5)
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			run := Run{Milliseconds: float64(elapsed.Nanoseconds()) / 1e6, TotalAllocBytes: after.TotalAlloc - before.TotalAlloc}
			s.Runs = append(s.Runs, run)
			a.Runs[r].Milliseconds += run.Milliseconds
			a.Runs[r].TotalAllocBytes += run.TotalAllocBytes
			if fingerprint(src) != sourceHash {
				panic("resample mutated source")
			}
		}
		s.Refused = last == src
		s.Sampled = stats(last)
		s.RuntimeEligible = !s.Refused && s.Full.Voxels > 1 && s.Sampled.Voxels > 0 && s.Sampled.Voxels < s.Full.Voxels
		if !s.Refused {
			s.Quality = quality(src, last)
		}
		byID[ref.ContentID] = len(a.Shapes)
		a.Shapes = append(a.Shapes, s)
		add(&a.FullUnique, s.Full)
		add(&a.SampledUnique, s.Sampled)
		if s.RuntimeEligible {
			add(&a.RuntimeEffectiveUnique, s.Sampled)
			a.RuntimeEligibleShapes++
		} else {
			add(&a.RuntimeEffectiveUnique, s.Full)
			a.RuntimeFallbackShapes++
		}
	}
	ms := make([]float64, n)
	alloc := make([]uint64, n)
	for i, r := range a.Runs {
		ms[i] = r.Milliseconds
		alloc[i] = r.TotalAllocBytes
	}
	sort.Float64s(ms)
	sort.Slice(alloc, func(i, j int) bool { return alloc[i] < alloc[j] })
	a.MedianMilliseconds = ms[n/2]
	a.MedianTotalAllocBytes = alloc[n/2]
	check, err := os.ReadFile(input)
	must(err)
	if sha256.Sum256(check) != hash {
		panic("authoring input changed")
	}
	return a
}
func probe(name string, src *volume.XBrickMap, root string) Probe {
	before := fingerprint(src)
	lod := src.Resample(.5)
	if lod == src {
		panic("synthetic resample refused")
	}
	p := Probe{Name: name, Full: stats(src), Sampled: stats(lod), Quality: quality(src, lod), PNG: filepath.Join(root, name+"-CPU-XY.png")}
	if fingerprint(src) != before {
		panic("probe source changed")
	}
	bs := bins(src)
	const cell = 10
	img := image.NewRGBA(image.Rect(0, 0, 3*32*cell+2*cell, 32*cell))
	pal := []color.RGBA{{18, 18, 18, 255}, {25, 190, 220, 255}, {230, 155, 45, 255}}
	for panel := 0; panel < 3; panel++ {
		for y := -8; y < 24; y++ {
			for x := -8; x < 24; x++ {
				c := pal[0]
				switch panel {
				case 0:
					_, v := src.GetVoxel(x, y, 1)
					if v > 0 {
						c = pal[int(v)%len(pal)]
					}
				case 1:
					_, v := lod.GetVoxel(floor2(x), floor2(y), floor2(1))
					if v > 0 {
						c = pal[int(v)%len(pal)]
					}
				case 2:
					b, ok := bs[[3]int{floor2(x), floor2(y), floor2(1)}]
					if ok {
						c = color.RGBA{170, 170, 170, 255}
						if mixed(b) {
							c = color.RGBA{225, 60, 190, 255}
						}
					}
				}
				for py := 0; py < cell; py++ {
					for px := 0; px < cell; px++ {
						img.SetRGBA(panel*(33*cell)+(x+8)*cell+px, (23-y)*cell+py, c)
					}
				}
			}
		}
	}
	f, err := os.Create(p.PNG)
	must(err)
	must(png.Encode(f, img))
	must(f.Close())
	return p
}
func probes(root string) []Probe {
	out := []Probe{}
	for _, x := range []int{0, 1, -2, -1} {
		m := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
			for y := -6; y < 8; y++ {
				for z := -2; z < 4; z++ {
					yield(volume.VoxelWrite{X: x, Y: y, Z: z, Value: 1})
				}
			}
		})
		out = append(out, probe(fmt.Sprintf("plane-x-%d", x), m, root))
	}
	opening := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for x := 0; x < 8; x++ {
			for y := 0; y < 8; y++ {
				for z := 0; z < 2; z++ {
					if x != 2 {
						yield(volume.VoxelWrite{X: x, Y: y, Z: z, Value: 2})
					}
				}
			}
		}
	})
	out = append(out, probe("one-voxel-opening-x2", opening, root))
	materials := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for x := 0; x < 2; x++ {
			for y := 0; y < 8; y++ {
				for z := 0; z < 2; z++ {
					yield(volume.VoxelWrite{X: x, Y: y, Z: z, Value: uint8(x + 1)})
				}
			}
		}
	})
	out = append(out, probe("adjacent-transparent-1-opaque-2", materials, root))
	sparse := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, v := range []volume.VoxelWrite{{X: -5, Y: -3, Z: -1, Value: 1}, {X: 3, Y: 5, Z: 7, Value: 2}, {X: -2, Y: 2, Z: 0, Value: 1}, {X: -7, Y: 8, Z: 3, Value: 2}, {X: 9, Y: -5, Z: 1, Value: 1}} {
			yield(v)
		}
	})
	out = append(out, probe("signed-asymmetric-sparse", sparse, root))
	return out
}
func main() {
	input := flag.String("inputs", "../actiongame/assets/content/hl1/models/valve_models_w_357ammobox.gkasset,../actiongame/assets/content/hl1/models/valve_models_stealth.gkasset,../actiongame/assets/content/hl1/models/models_nihilanth.gkasset", "comma separated authoring files")
	root := flag.String("out", "/tmp/gekko-c3-lod", "evidence and compiled closure directory")
	runs := flag.Int("runs", 3, "odd positive repeated resampling invocations")
	flag.Parse()
	if *runs < 1 || *runs%2 == 0 {
		panic("runs must be odd positive")
	}
	runtime.GOMAXPROCS(1)
	must(os.MkdirAll(*root, 0755))
	e := Evidence{GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH, GOMAXPROCS: 1, Runs: *runs, Method: "Compile authoring closures once, warm decoded immutable shapes; deduplicate ContentID within each asset, keep part references. GC before each shape resample; time Resample(.5) including its production stdout. Sum each run across unique shapes; median of run sums. TotalAlloc includes production resample, excludes decoding/source construction/quality scans. No GPU, FPS, RSS or renderer visual parity measured.", UploadEstimate: "Estimated fresh geometry scheduler writes only: each sector 32+64*32 B, occupied brick aux VoxelAuxWordCount*4=1088 B, plus 512 B for mixed material payload. Solid/uniform sparse skip payload. Excludes material tables, object lookup, allocator capacities, queue overhead, upload time; not actual GPU memory or upload measurements. Aux includes occupancy and normal storage; excludes normal baking work/time. RuntimeEffectiveUnique follows existing >1 source, nonempty and strictly fewer sampled voxel eligibility, otherwise full source fallback.", ProjectionLegend: "CPU XY z=1 projection, fixed source voxel grid [-8,23]. Left full source: cyan material 1, orange material 2. Middle .5 sampled geometry expanded 2x to source cells. Right occupancy OR coarse cells expanded2x: gray any occupancy, magenta multiple material IDs with no material winner. Material1 designated transparent and material2 opaque only for semantic probe; no opacity rendering. Panels ignore engine extent/pivot proxy adjustment and are not GPU appearance."}
	for _, path := range strings.Split(*input, ",") {
		if path != "" {
			e.Assets = append(e.Assets, asset(path, *root, *runs))
		}
	}
	e.Probes = probes(*root)
	data, err := json.MarshalIndent(e, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Join(*root, "evidence.json"), append(data, '\n'), 0644))
	fmt.Println("Evidence:", filepath.Join(*root, "evidence.json"))
}
