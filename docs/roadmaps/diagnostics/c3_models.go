//go:build ignore

// Run from the engine module: go run docs/roadmaps/diagnostics/c3_models.go -engine .
// CPU-only diagnostic: compilation and parity are outside timed preparation loops.
package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	gekko "github.com/gekko3d/gekko"
	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

type scenario struct {
	name, path string
	index      int
	cube       bool
}
type measurement struct {
	N             int
	Milliseconds  float64
	Bytes, Allocs int64
}
type series struct {
	Variant                                              string
	Runs                                                 []measurement
	MedianMilliseconds, MinMilliseconds, MaxMilliseconds float64
	MedianBytes, MinBytes, MaxBytes                      int64
	MedianAllocs, MinAllocs, MaxAllocs                   int64
}
type report struct {
	Scenario                                                  string
	ModelIndex                                                int
	Scale                                                     float32
	RawSelected, RawWholeFile, ActualPrimary                  int
	Dimensions                                                [3]uint32
	SourceVOXBytes, AuthoringJSONBytes, CompleteShippingBytes int64
	PrimarySHA256, ModelCID, BaseIdentity                     string
	ModelInfo                                                 voxelcodec.Info
	Series                                                    []series
	ShippingDirectory                                         string
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func primaryHash(g gekko.VoxelGeometryAsset) (string, int) {
	voxels := gekko.VoxelObjectSnapshotFromXBrickMap(g.XBrickMap).Voxels
	h := sha256.New()
	var b [25]byte
	for _, v := range voxels {
		binary.LittleEndian.PutUint64(b[0:8], uint64(int64(v.X)))
		binary.LittleEndian.PutUint64(b[8:16], uint64(int64(v.Y)))
		binary.LittleEndian.PutUint64(b[16:24], uint64(int64(v.Z)))
		b[24] = v.Value
		_, err := h.Write(b[:])
		must(err)
	}
	return hex.EncodeToString(h.Sum(nil)), len(voxels)
}
func prepare(path string, assets *gekko.AssetServer, loader *gekko.RuntimeContentLoader) (gekko.VoxelGeometryAsset, gekko.VoxelPaletteAsset) {
	p, err := gekko.LoadAndPrepareAuthoredAsset(path, assets, loader)
	must(err)
	id, ok := gekko.PreparedAuthoredAssetPartGeometry(p, "model")
	if !ok {
		panic("geometry part absent")
	}
	g, ok := assets.GetVoxelGeometry(id)
	if !ok {
		panic("geometry asset absent")
	}
	id, ok = gekko.PreparedAuthoredAssetPartPalette(p, "model")
	if !ok {
		panic("palette part absent")
	}
	pal, ok := assets.GetVoxelPalette(id)
	if !ok {
		panic("palette asset absent")
	}
	return g, pal
}
func shippingBytes(root string) int64 {
	var total int64
	must(filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	}))
	return total
}
func measure(path, variant string, warm bool) series {
	result := series{Variant: variant}
	var assets *gekko.AssetServer
	var loader *gekko.RuntimeContentLoader
	if warm {
		assets = &gekko.AssetServer{}
		loader = gekko.NewRuntimeContentLoader()
		_, _ = prepare(path, assets, loader)
	}
	for i := 0; i < 3; i++ {
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				a, l := assets, loader
				if !warm {
					a = &gekko.AssetServer{}
					l = gekko.NewRuntimeContentLoader()
				}
				p, err := gekko.LoadAndPrepareAuthoredAsset(path, a, l)
				if err != nil {
					b.Fatal(err)
				}
				runtime.KeepAlive(p)
				runtime.KeepAlive(a)
				runtime.KeepAlive(l)
			}
		})
		if r.N <= 0 {
			panic("benchmark did not complete an operation")
		}
		result.Runs = append(result.Runs, measurement{N: r.N, Milliseconds: float64(r.T) / float64(r.N) / float64(time.Millisecond), Bytes: r.AllocedBytesPerOp(), Allocs: r.AllocsPerOp()})
	}
	ms := []float64{}
	bs, as := []int64{}, []int64{}
	for _, r := range result.Runs {
		ms = append(ms, r.Milliseconds)
		bs = append(bs, r.Bytes)
		as = append(as, r.Allocs)
	}
	sort.Float64s(ms)
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	sort.Slice(as, func(i, j int) bool { return as[i] < as[j] })
	result.MinMilliseconds, result.MedianMilliseconds, result.MaxMilliseconds = ms[0], ms[1], ms[2]
	result.MinBytes, result.MedianBytes, result.MaxBytes = bs[0], bs[1], bs[2]
	result.MinAllocs, result.MedianAllocs, result.MaxAllocs = as[0], as[1], as[2]
	runtime.KeepAlive(assets)
	runtime.KeepAlive(loader)
	return result
}

func runScenario(s scenario, temp string, prepareOnly bool) report {
	dir := filepath.Join(temp, s.name)
	must(os.MkdirAll(dir, 0700))
	input := filepath.Join(dir, "source.gkasset")
	output := filepath.Join(dir, "shipping", "asset.gkmodelassetc")
	source := content.AssetSourceDef{Kind: content.AssetSourceKindVoxModel, Path: s.path, ModelIndex: s.index}
	r := report{Scenario: s.name, ModelIndex: s.index, Scale: 1, ShippingDirectory: filepath.Dir(output)}
	if s.cube {
		source = content.AssetSourceDef{Kind: content.AssetSourceKindProceduralPrimitive, Primitive: "cube", Params: map[string]float32{"sx": 32, "sy": 32, "sz": 32}}
		r.RawSelected = 32 * 32 * 32
		r.RawWholeFile = r.RawSelected
	} else {
		file, err := gekko.LoadVoxFile(s.path)
		must(err)
		if s.index < 0 || s.index >= len(file.Models) {
			panic("selected index out of range")
		}
		r.RawSelected = len(file.Models[s.index].Voxels)
		for _, m := range file.Models {
			r.RawWholeFile += len(m.Voxels)
		}
		stat, err := os.Stat(s.path)
		must(err)
		r.SourceVOXBytes = stat.Size()
	}
	asset := &content.AssetDef{SchemaVersion: 4, ID: "diagnostic-" + s.name, Name: s.name, Parts: []content.AssetPartDef{{ID: "model", Name: "Model", ModelScale: 1, VoxelResolution: .125, Transform: content.AssetTransformDef{Rotation: content.Quat{0, 0, 0, 1}, Scale: content.Vec3{1, 1, 1}}, Source: source}}}
	must(content.SaveAsset(input, asset))
	sourceStat, err := os.Stat(input)
	must(err)
	r.AuthoringJSONBytes = sourceStat.Size()
	_, err = gekko.CompileAuthoredModelAsset(input, output, nil)
	must(err)
	header, _, err := content.LoadCompiledAssetModelHeader(output, nil)
	must(err)
	if len(header.Models) != 1 {
		panic("unexpected model references")
	}
	ref := header.Models[0]
	r.ModelCID, r.BaseIdentity = ref.ContentID, ref.BaseIdentity
	_, r.ModelInfo, err = content.LoadCompiledAssetModel(filepath.Join(filepath.Dir(output), ref.Path), nil)
	must(err)
	r.CompleteShippingBytes = shippingBytes(filepath.Dir(output))
	legacy, legacyPalette := prepare(input, &gekko.AssetServer{}, gekko.NewRuntimeContentLoader())
	compiled, compiledPalette := prepare(output, &gekko.AssetServer{}, gekko.NewRuntimeContentLoader())
	lh, ln := primaryHash(legacy)
	ch, cn := primaryHash(compiled)
	if lh != ch || ln != cn {
		panic("actual primary parity failed")
	}
	r.PrimarySHA256, r.ActualPrimary = lh, ln
	r.Dimensions = [3]uint32{legacy.VoxModel.SizeX, legacy.VoxModel.SizeY, legacy.VoxModel.SizeZ}
	if r.Dimensions != ([3]uint32{compiled.VoxModel.SizeX, compiled.VoxModel.SizeY, compiled.VoxModel.SizeZ}) || legacy.LocalMin != compiled.LocalMin || legacy.LocalMax != compiled.LocalMax {
		panic("declared dimensions/bounds parity failed")
	}
	legacyPalette.SourcePath = ""
	compiledPalette.SourcePath = ""
	lp, err := json.Marshal(legacyPalette)
	must(err)
	cp, err := json.Marshal(compiledPalette)
	must(err)
	if !reflect.DeepEqual(lp, cp) {
		panic("palette semantic parity failed")
	}
	// Release parity-only and full-file inspection graphs before measurement.
	legacy = gekko.VoxelGeometryAsset{}
	compiled = gekko.VoxelGeometryAsset{}
	legacyPalette = gekko.VoxelPaletteAsset{}
	compiledPalette = gekko.VoxelPaletteAsset{}
	runtime.GC()
	if !prepareOnly {
		r.Series = append(r.Series, measure(input, "legacy-cold", false), measure(output, "compiled-cold", false), measure(input, "legacy-warm", true), measure(output, "compiled-warm", true))
	}
	return r
}

func main() {
	testing.Init()
	engine := flag.String("engine", ".", "engine module directory")
	samples := flag.String("samples", "jet,sponza,gate,cube32", "comma-separated scenario names")
	duration := flag.Duration("duration", 100*time.Millisecond, "minimum duration per testing.Benchmark run; 3 sequential runs per variant")
	prepareOnly := flag.Bool("prepare-only", false, "compile and prove parity without timed loops")
	flag.Parse()
	absoluteEngine, err := filepath.Abs(*engine)
	must(err)
	*engine = absoluteEngine
	if *duration <= 0 {
		panic("duration must be positive")
	}
	must(flag.Set("test.benchtime", duration.String()))
	runtime.GOMAXPROCS(1)
	temp, err := os.MkdirTemp("", "gekko-c3-models-")
	must(err)
	fmt.Fprintf(os.Stderr, "Artifacts: %s; GOMAXPROCS=1; benchtime=%s; runs=3; CPU only\n", temp, duration.String())
	cases := []scenario{{name: "jet", path: filepath.Join(*engine, "../spacegame_go/assets/CamoStellarJet.vox"), index: 0}, {name: "sponza", path: filepath.Join(*engine, "../gekko-editor/assets/sponza.vox"), index: 62}, {name: "gate", path: filepath.Join(*engine, "../gekko-editor/assets/brandenburggate2.vox"), index: 3}, {name: "cube32", cube: true}}
	selected := map[string]bool{}
	for _, name := range strings.Split(*samples, ",") {
		selected[name] = true
	}
	enc := json.NewEncoder(os.Stdout)
	for _, s := range cases {
		if selected[s.name] {
			must(enc.Encode(runScenario(s, temp, *prepareOnly)))
		}
	}
}
