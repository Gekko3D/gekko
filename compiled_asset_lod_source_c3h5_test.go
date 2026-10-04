package gekko

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gekko3d/gekko/content"
)

func c3h5Source(t *testing.T, s *content.CompiledAssetShapeDef) verifiedCompiledAssetShape {
	t.Helper()
	frame, info, err := content.EncodeCompiledAssetShape(s, nil)
	if err != nil {
		t.Fatal("source fixture encode", err)
	}
	out, oi, err := content.DecodeCompiledAssetShape(frame, nil)
	if err != nil || oi != info {
		t.Fatal("source fixture decode", err)
	}
	base, _, err := content.CompiledAssetShapeBaseIdentity(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	return verifiedCompiledAssetShape{definition: out, contentID: oi.ContentID, baseIdentity: base}
}
func c3h5Bounds(t *testing.T, s *content.CompiledAssetShapeDef) ([3]int64, [3]int64) {
	t.Helper()
	var min, max [3]int64
	first := true
	for p := range c3h1Cells(t, s.Bricks) {
		for axis, v := range p {
			if first || v < min[axis] {
				min[axis] = v
			}
			if first || v+1 > max[axis] {
				max[axis] = v + 1
			}
		}
		first = false
	}
	return min, max
}
func c3h5LOD(t *testing.T, s verifiedCompiledAssetShape, coarse ...[3]int64) *content.CompiledAssetLODDef {
	t.Helper()
	geometry := c3h1Shape(coarse...)
	smin, smax := c3h5Bounds(t, s.definition)
	cmin, cmax := c3h5Bounds(t, geometry)
	return &content.CompiledAssetLODDef{SchemaVersion: 1, SourceContentID: s.contentID, SourceLattice: s.definition.Lattice, Factor: 2, ReductionVersion: content.CompiledAssetLOD2xReductionVersion, Value: 7, SourceVoxelCount: int64(len(c3h1Cells(t, s.definition.Bricks))), SourceMin: smin, SourceMax: smax, CoarseMin: cmin, CoarseMax: cmax, Bricks: geometry.Bricks}
}
func c3h5Typed(t *testing.T, lod *content.CompiledAssetLODDef) *content.CompiledAssetLODDef {
	t.Helper()
	frame, _, err := content.EncodeCompiledAssetLOD(lod, nil)
	if err != nil {
		t.Fatalf("bad fixture is not structurally valid typed LOD: %+v err=%v", lod, err)
	}
	out, _, err := content.DecodeCompiledAssetLOD(frame, nil)
	if err != nil {
		t.Fatal("typed LOD decode fixture", err)
	}
	return out
}
func c3h5Columns(xs ...int64) [][3]int64 {
	var cells [][3]int64
	for _, x := range xs {
		for y := int64(0); y < 2; y++ {
			for z := int64(0); z < 2; z++ {
				cells = append(cells, [3]int64{x, y, z})
			}
		}
	}
	return cells
}
func c3h5Check(t *testing.T, lod *content.CompiledAssetLODDef, source verifiedCompiledAssetShape, wantErr bool) {
	t.Helper()
	beforeLOD, _ := json.Marshal(lod)
	beforeSource, _ := json.Marshal(source.definition)
	for i := 0; i < 2; i++ {
		err := validateCompiledAssetLODSource(lod, source)
		if (err != nil) != wantErr {
			t.Fatalf("verification err=%v wantErr=%v", err, wantErr)
		}
	}
	afterLOD, _ := json.Marshal(lod)
	afterSource, _ := json.Marshal(source.definition)
	if string(beforeLOD) != string(afterLOD) || string(beforeSource) != string(afterSource) {
		t.Fatal("verifier mutates borrowed source/LOD")
	}
}

func TestC3h5SignedCoverageAndSparseExtrema(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		other, third := (axis+1)%3, (axis+2)%3
		var sourceCells, coarseCells [][3]int64
		for _, x := range []int64{-9, -8, -7, -2, -1, 0, 1, 7, 8, 9} {
			for _, y := range []int64{2, 3} {
				var p [3]int64
				p[axis], p[other], p[third] = x, y, -3
				sourceCells = append(sourceCells, p)
			}
		}
		for _, x := range []int64{-5, -4, -1, 0, 3, 4} {
			var p [3]int64
			p[axis], p[other], p[third] = x, 1, -2
			coarseCells = append(coarseCells, p)
		}
		source := c3h5Source(t, c3h1Shape(sourceCells...))
		lod := c3h5Typed(t, c3h5LOD(t, source, coarseCells...))
		c3h5Check(t, lod, source, false)
	}
	// Huge sparse AABB spans all portable fine axes. The helper must visit occupied
	// bricks/cells only; a bounding-volume walk cannot finish this fixed guard.
	fine := [][3]int64{{-2147483648, -2147483648, -2147483648}, {-2147483647, -2147483648, -2147483648}, {2147483646, 2147483647, 2147483647}, {2147483647, 2147483647, 2147483647}}
	coarse := [][3]int64{{-1073741824, -1073741824, -1073741824}, {1073741823, 1073741823, 1073741823}}
	source := c3h5Source(t, c3h1Shape(fine...))
	lod := c3h5Typed(t, c3h5LOD(t, source, coarse...))
	done := make(chan error, 1)
	go func() { done <- validateCompiledAssetLODSource(lod, source) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal("extreme sparse coverage rejected", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("verifier scanned source bounding volume")
	}
	c3h5Check(t, lod, source, false)
}

func TestC3h5RejectValidTypedMetadataMismatches(t *testing.T) {
	cases := map[string]func(*content.CompiledAssetLODDef){
		"source-logical-id":  func(l *content.CompiledAssetLODDef) { l.SourceContentID = strings.Repeat("a", 64) },
		"source-base-id":     func(l *content.CompiledAssetLODDef) {},
		"lattice-resolution": func(l *content.CompiledAssetLODDef) { l.SourceLattice.VoxelResolution *= 2 },
		"lattice-version":    func(l *content.CompiledAssetLODDef) { l.SourceLattice.RasterizationVersion = "other" },
		"actual-count":       func(l *content.CompiledAssetLODDef) { l.SourceVoxelCount = 9 },
		"source-min":         func(l *content.CompiledAssetLODDef) { l.SourceMin[0] = 0 },
		"source-max":         func(l *content.CompiledAssetLODDef) { l.SourceMax[0] = 5 },
		"sole-value": func(l *content.CompiledAssetLODDef) {
			l.Value = 9
			for i := range l.Bricks {
				for j := range l.Bricks[i].Values {
					l.Bricks[i].Values[j] = 9
				}
			}
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			source := c3h5Source(t, c3h1Shape(c3h5Columns(1, 5)...))
			lod := c3h5LOD(t, source, [3]int64{0, 0, 0}, [3]int64{2, 0, 0})
			change(lod)
			if name == "source-base-id" {
				lod.SourceContentID = source.baseIdentity
			}
			c3h5Check(t, c3h5Typed(t, lod), source, true)
		})
	}
}

func TestC3h5RejectMissingCoverageAndUnwitnessedCoarseCells(t *testing.T) {
	// Both wrong derivatives retain extrema and satisfy h2 count/capacity checks.
	source := c3h5Source(t, c3h1Shape(c3h5Columns(1, 3, 5)...))
	missing := c3h5Typed(t, c3h5LOD(t, source, [3]int64{0, 0, 0}, [3]int64{2, 0, 0}))
	c3h5Check(t, missing, source, true)
	source = c3h5Source(t, c3h1Shape(c3h5Columns(1, 5)...))
	extra := c3h5Typed(t, c3h5LOD(t, source, [3]int64{0, 0, 0}, [3]int64{1, 0, 0}, [3]int64{2, 0, 0}))
	c3h5Check(t, extra, source, true)
	// Correct interior occupancy with the same authenticated source remains valid.
	valid := c3h5Typed(t, c3h5LOD(t, source, [3]int64{0, 0, 0}, [3]int64{2, 0, 0}))
	c3h5Check(t, valid, source, false)
}

func TestC3h5RejectNilEmptyMixedAndNoReductionSource(t *testing.T) {
	source := c3h5Source(t, c3h1Shape(c3h5Columns(1, 5)...))
	lod := c3h5Typed(t, c3h5LOD(t, source, [3]int64{0, 0, 0}, [3]int64{2, 0, 0}))
	c3h5Check(t, nil, source, true)
	c3h5Check(t, lod, verifiedCompiledAssetShape{}, true)
	nilDefinition := source
	nilDefinition.definition = nil
	c3h5Check(t, lod, nilDefinition, true)
	for _, kind := range []string{"empty", "mixed", "no-reduction"} {
		t.Run(kind, func(t *testing.T) {
			s := c3h1Shape(c3h5Columns(1, 5)...)
			switch kind {
			case "empty":
				s.Bricks = nil
			case "mixed":
				s.Bricks[0].Values[0] = 9
			case "no-reduction":
				s = c3h1Shape([3]int64{1, 0, 0}, [3]int64{5, 0, 0})
			}
			actual := c3h5Source(t, s)
			forged := *lod
			forged.SourceContentID = actual.contentID
			// Forged metadata remains h2-valid; source proof is authenticated afresh.
			c3h5Check(t, c3h5Typed(t, &forged), actual, true)
		})
	}
	// Verification cannot retain one source binding and accept it for another ID.
	different := c3h1Shape(c3h5Columns(1, 5)...)
	different.Lattice.RasterizationVersion = "distinct"
	actual := c3h5Source(t, different)
	if reflect.DeepEqual(source, actual) {
		t.Fatal("distinct source fixture is identical")
	}
	c3h5Check(t, lod, actual, true)
}
