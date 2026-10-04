package gekko

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

// Fixture encoding uses the existing brick layout; expected reduced cells below
// are explicit and do not call a reduction implementation.
func c3h1Shape(cells ...[3]int64) *content.CompiledAssetShapeDef {
	byCoord := make(map[[3]int32]*voxelcodec.Brick)
	for _, cell := range cells {
		var coord [3]int32
		var local [3]int
		for axis, p := range cell {
			q, r := p/8, p%8
			if r < 0 {
				q--
				r += 8
			}
			coord[axis], local[axis] = int32(q), int(r)
		}
		brick := byCoord[coord]
		if brick == nil {
			brick = &voxelcodec.Brick{Coord: coord}
			byCoord[coord] = brick
		}
		bit := local[0] + 8*local[1] + 64*local[2]
		brick.Occupancy[bit/64] |= uint64(1) << uint(bit%64)
	}
	shape := &content.CompiledAssetShapeDef{SchemaVersion: content.CurrentCompiledAssetShapeSchemaVersion, Lattice: content.VoxelObjectLatticeDef{VoxelResolution: .125, RasterizationVersion: "authored-c3h1"}}
	for _, brick := range byCoord {
		for _, word := range brick.Occupancy {
			for word != 0 {
				brick.Values = append(brick.Values, 7)
				word &= word - 1
			}
		}
		shape.Bricks = append(shape.Bricks, *brick)
	}
	sort.Slice(shape.Bricks, func(i, j int) bool { return c3h1CoordLess(shape.Bricks[i].Coord, shape.Bricks[j].Coord) })
	return shape
}

func c3h1CoordLess(a, b [3]int32) bool {
	for axis := 0; axis < 3; axis++ {
		if a[axis] != b[axis] {
			return a[axis] < b[axis]
		}
	}
	return false
}

func c3h1Cells(t *testing.T, bricks []voxelcodec.Brick) map[[3]int64]uint8 {
	t.Helper()
	cells := make(map[[3]int64]uint8)
	for i, brick := range bricks {
		if i > 0 && !c3h1CoordLess(bricks[i-1].Coord, brick.Coord) {
			t.Fatal("output bricks are not strictly signed lexicographic")
		}
		if brick.Materials != nil || brick.Aux != nil {
			t.Fatal("output has secondary layers")
		}
		rank := 0
		for bit := 0; bit < 512; bit++ {
			if brick.Occupancy[bit/64]&(uint64(1)<<uint(bit%64)) == 0 {
				continue
			}
			if rank >= len(brick.Values) {
				t.Fatal("output value cardinality too small")
			}
			p := [3]int64{int64(brick.Coord[0])*8 + int64(bit%8), int64(brick.Coord[1])*8 + int64(bit/8%8), int64(brick.Coord[2])*8 + int64(bit/64)}
			cells[p] = brick.Values[rank]
			rank++
		}
		if rank == 0 || rank != len(brick.Values) {
			t.Fatal("empty output brick or excessive values")
		}
	}
	return cells
}

func c3h1Check(t *testing.T, source *content.CompiledAssetShapeDef, expected [][3]int64, sourceCount int64, sourceMin, sourceMax, coarseMin, coarseMax [3]int64) *compiledAssetLOD2xGeometry {
	t.Helper()
	got, err := buildCompiledAssetLOD2x(source)
	if err != nil || got == nil {
		t.Fatalf("eligible reduction: got %v, err %v", got, err)
	}
	want := make(map[[3]int64]uint8)
	for _, p := range expected {
		want[p] = 7
	}
	if cells := c3h1Cells(t, got.Bricks); !reflect.DeepEqual(cells, want) {
		t.Fatalf("coarse cells: got %v want %v", cells, want)
	}
	if got.Value != 7 || got.SourceVoxelCount != sourceCount || got.CoarseVoxelCount != int64(len(expected)) {
		t.Fatalf("wrong value/counts: %+v", got)
	}
	if got.SourceMin != sourceMin || got.SourceMax != sourceMax || got.CoarseMin != coarseMin || got.CoarseMax != coarseMax {
		t.Fatalf("wrong exclusive bounds: %+v", got)
	}
	if got.SourceLattice != source.Lattice {
		t.Fatalf("source lattice changed: %+v", got.SourceLattice)
	}
	if got.ReductionVersion != "occupancy-or-zero-anchored-2x-v1" {
		t.Fatal("wrong reduction version")
	}
	if compiledAssetLOD2xReductionVersion != got.ReductionVersion {
		t.Fatal("reduction constant differs from result")
	}
	return got
}

func TestC3h1DenseAndThinCoverage(t *testing.T) {
	var dense [][3]int64
	for z := int64(0); z < 2; z++ {
		for y := int64(0); y < 2; y++ {
			for x := int64(0); x < 2; x++ {
				dense = append(dense, [3]int64{x, y, z})
			}
		}
	}
	c3h1Check(t, c3h1Shape(dense...), [][3]int64{{0, 0, 0}}, 8, [3]int64{0, 0, 0}, [3]int64{2, 2, 2}, [3]int64{0, 0, 0}, [3]int64{1, 1, 1})
	upperValue := c3h1Shape(dense...)
	for i := range upperValue.Bricks {
		for j := range upperValue.Bricks[i].Values {
			upperValue.Bricks[i].Values[j] = 255
		}
	}
	upperResult, err := buildCompiledAssetLOD2x(upperValue)
	if err != nil || upperResult == nil {
		t.Fatalf("sole palette value255 reduction: %v", err)
	}
	if upperResult.Value != 255 || !reflect.DeepEqual(c3h1Cells(t, upperResult.Bricks), map[[3]int64]uint8{{0, 0, 0}: 255}) {
		t.Fatal("sole palette value255 changed")
	}
	for _, z := range []int64{0, 1} {
		t.Run([]string{"even plane", "odd plane"}[z], func(t *testing.T) {
			source := c3h1Shape([3]int64{0, 0, z}, [3]int64{1, 0, z}, [3]int64{0, 1, z}, [3]int64{1, 1, z})
			c3h1Check(t, source, [][3]int64{{0, 0, 0}}, 4, [3]int64{0, 0, z}, [3]int64{2, 2, z + 1}, [3]int64{0, 0, 0}, [3]int64{1, 1, 1})
		})
	}
	// Fine x=1 is an opening. The approved coverage policy fills its coarse cell.
	source := c3h1Shape([3]int64{0, 0, 0}, [3]int64{0, 1, 0}, [3]int64{2, 0, 0}, [3]int64{2, 1, 0})
	c3h1Check(t, source, [][3]int64{{0, 0, 0}, {1, 0, 0}}, 4, [3]int64{0, 0, 0}, [3]int64{3, 2, 1}, [3]int64{0, 0, 0}, [3]int64{2, 1, 1})
}

func TestC3h1SignedZeroAnchoredAcrossAxes(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		other, third := (axis+1)%3, (axis+2)%3
		var sourceCells, expected [][3]int64
		for _, p := range []int64{-9, -8, -7, -2, -1, 0, 1, 7, 8, 9} {
			for _, q := range []int64{2, 3} {
				var cell [3]int64
				cell[axis], cell[other], cell[third] = p, q, -3
				sourceCells = append(sourceCells, cell)
			}
		}
		for _, p := range []int64{-5, -4, -1, 0, 3, 4} {
			var cell [3]int64
			cell[axis], cell[other], cell[third] = p, 1, -2
			expected = append(expected, cell)
		}
		var smin, smax, cmin, cmax [3]int64
		smin[axis], smin[other], smin[third] = -9, 2, -3
		smax[axis], smax[other], smax[third] = 10, 4, -2
		cmin[axis], cmin[other], cmin[third] = -5, 1, -2
		cmax[axis], cmax[other], cmax[third] = 5, 2, -1
		c3h1Check(t, c3h1Shape(sourceCells...), expected, 20, smin, smax, cmin, cmax)
	}
}

func TestC3h1PortableExtremaSparseAndDeterministic(t *testing.T) {
	const lo, hi int64 = -2147483648, 2147483647
	source := c3h1Shape([3]int64{lo, lo, lo}, [3]int64{lo + 1, lo + 1, lo + 1}, [3]int64{hi - 1, hi - 1, hi - 1}, [3]int64{hi, hi, hi}, [3]int64{-1, 0, 0}, [3]int64{-2, 1, 1})
	expected := [][3]int64{{-1073741824, -1073741824, -1073741824}, {1073741823, 1073741823, 1073741823}, {-1, 0, 0}}
	snapshot := func() string {
		data, err := json.Marshal(source)
		if err != nil {
			t.Fatalf("serialize source snapshot: %v", err)
		}
		return string(data)
	}
	before := snapshot()
	got := c3h1Check(t, source, expected, 6, [3]int64{lo, lo, lo}, [3]int64{hi + 1, hi + 1, hi + 1}, [3]int64{-1073741824, -1073741824, -1073741824}, [3]int64{1073741824, 1073741824, 1073741824})
	if snapshot() != before {
		t.Fatal("first reduction mutated source")
	}
	for i, j := 0, len(source.Bricks)-1; i < j; i, j = i+1, j-1 {
		source.Bricks[i], source.Bricks[j] = source.Bricks[j], source.Bricks[i]
	}
	before = snapshot()
	reordered, err := buildCompiledAssetLOD2x(source)
	if err != nil || !reflect.DeepEqual(got, reordered) {
		t.Fatalf("input order changed canonical result: %v", err)
	}
	if snapshot() != before {
		t.Fatal("reduction mutated reversed source")
	}
	outputSnapshot := c3h1Cells(t, reordered.Bricks)
	// Returned storage belongs to each call, independent of source and other calls.
	got.Bricks[0].Values[0] = 99
	got.Bricks[0].Occupancy[0] = 0
	got.Bricks[0].Coord[0] = 42
	if reflect.DeepEqual(got, reordered) {
		t.Fatal("mutation did not take effect")
	}
	c3h1Check(t, source, expected, 6, [3]int64{lo, lo, lo}, [3]int64{hi + 1, hi + 1, hi + 1}, [3]int64{-1073741824, -1073741824, -1073741824}, [3]int64{1073741824, 1073741824, 1073741824})
	if cells := c3h1Cells(t, reordered.Bricks); !reflect.DeepEqual(cells, outputSnapshot) {
		t.Fatal("separate result shares storage")
	}
	source.Bricks[0].Values[0] = 3
	if cells := c3h1Cells(t, reordered.Bricks); !reflect.DeepEqual(cells, outputSnapshot) {
		t.Fatal("result aliases source")
	}
}

func TestC3h1Ineligible(t *testing.T) {
	mixed := c3h1Shape([3]int64{0, 0, 0}, [3]int64{1, 0, 0}, [3]int64{32, 0, 0}, [3]int64{33, 0, 0})
	mixed.Bricks[1].Values[0] = 8
	mixed.Bricks[1].Values[1] = 8
	for name, source := range map[string]*content.CompiledAssetShapeDef{
		"empty": c3h1Shape(), "single": c3h1Shape([3]int64{0, 0, 0}),
		"no count reduction": c3h1Shape([3]int64{0, 0, 0}, [3]int64{2, 0, 0}), "mixed separate regions": mixed,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := buildCompiledAssetLOD2x(source)
			if err != nil || got != nil {
				t.Fatalf("valid ineligible: got %v err %v", got, err)
			}
		})
	}
}

func TestC3h1Malformed(t *testing.T) {
	valid := func() *content.CompiledAssetShapeDef { return c3h1Shape([3]int64{0, 0, 0}, [3]int64{1, 0, 0}) }
	cases := map[string]func(*content.CompiledAssetShapeDef){
		"schema":               func(s *content.CompiledAssetShapeDef) { s.SchemaVersion++ },
		"resolution zero":      func(s *content.CompiledAssetShapeDef) { s.Lattice.VoxelResolution = 0 },
		"resolution negative":  func(s *content.CompiledAssetShapeDef) { s.Lattice.VoxelResolution = -1 },
		"resolution NaN":       func(s *content.CompiledAssetShapeDef) { s.Lattice.VoxelResolution = float32(math.NaN()) },
		"resolution infinity":  func(s *content.CompiledAssetShapeDef) { s.Lattice.VoxelResolution = float32(math.Inf(1)) },
		"version empty":        func(s *content.CompiledAssetShapeDef) { s.Lattice.RasterizationVersion = "" },
		"version too long":     func(s *content.CompiledAssetShapeDef) { s.Lattice.RasterizationVersion = strings.Repeat("a", 129) },
		"version invalid UTF8": func(s *content.CompiledAssetShapeDef) { s.Lattice.RasterizationVersion = string([]byte{255}) },
		"empty brick": func(s *content.CompiledAssetShapeDef) {
			s.Bricks = append(s.Bricks, voxelcodec.Brick{Coord: [3]int32{1, 0, 0}})
		},
		"too few values":  func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Values = s.Bricks[0].Values[:1] },
		"too many values": func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Values = append(s.Bricks[0].Values, 7) },
		"zero value":      func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Values[1] = 0 },
		"duplicate brick": func(s *content.CompiledAssetShapeDef) { s.Bricks = append(s.Bricks, s.Bricks[0]) },
		"below portable":  func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Coord[0] = -268435457 },
		"above portable":  func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Coord[2] = 268435456 },
		"materials":       func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Materials = []byte{1, 1} },
		"empty materials": func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Materials = []byte{} },
		"aux":             func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Aux = []byte{1} },
		"empty aux":       func(s *content.CompiledAssetShapeDef) { s.Bricks[0].Aux = []byte{} },
	}
	t.Run("nil", func(t *testing.T) {
		got, err := buildCompiledAssetLOD2x(nil)
		if err == nil || got != nil {
			t.Fatalf("nil accepted: %v %v", got, err)
		}
	})
	for name, breakShape := range cases {
		t.Run(name, func(t *testing.T) {
			source := valid()
			breakShape(source)
			got, err := buildCompiledAssetLOD2x(source)
			if err == nil || got != nil {
				t.Fatalf("malformed accepted: %v %v", got, err)
			}
		})
	}
	// A valid ineligible prefix cannot suppress a later malformed brick.
	for _, prefix := range []string{"mixed", "single"} {
		t.Run("failure after "+prefix, func(t *testing.T) {
			source := valid()
			if prefix == "mixed" {
				source.Bricks[0].Values[1] = 8
			} else {
				source = c3h1Shape([3]int64{0, 0, 0})
			}
			source.Bricks = append(source.Bricks, voxelcodec.Brick{Coord: [3]int32{9, 0, 0}, Occupancy: [8]uint64{1}, Values: []byte{0}})
			for order := 0; order < 2; order++ {
				got, err := buildCompiledAssetLOD2x(source)
				if err == nil || got != nil {
					t.Fatalf("ineligibility masked malformed input: %v %v", got, err)
				}
				source.Bricks[0], source.Bricks[1] = source.Bricks[1], source.Bricks[0]
			}
		})
	}
}
