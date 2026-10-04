package gekko

import (
	"bytes"
	"encoding/json"
	"math/bits"
	"reflect"
	"runtime"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// Reproduce the previous converter through the public ordered builder. Input is
// already authenticated canonical primary-only C1 geometry, as at the production boundary.
func c3g13Reference(shape *content.CompiledAssetShapeDef) *volume.XBrickMap {
	geometry := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, brick := range shape.Bricks {
			index := 0
			for word, mask := range brick.Occupancy {
				for mask != 0 {
					linear := word*64 + bits.TrailingZeros64(mask)
					if !yield(volume.VoxelWrite{X: int(brick.Coord[0])*8 + linear%8, Y: int(brick.Coord[1])*8 + (linear/8)%8, Z: int(brick.Coord[2])*8 + linear/64, Value: brick.Values[index]}) {
						return
					}
					index++
					mask &= mask - 1
				}
			}
		}
	})
	geometry.ComputeAABB()
	geometry.ClearDirty()
	return geometry
}

func c3g13Canonical(t *testing.T, bricks []voxelcodec.Brick) *content.CompiledAssetShapeDef {
	t.Helper()
	shape := &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: authoredVoxelShapeLattice(.125), Bricks: bricks}
	frame, _, err := content.EncodeCompiledAssetShape(shape, nil)
	if err != nil {
		t.Fatal("fixture is not typed-valid", err)
	}
	canonical, _, err := content.DecodeCompiledAssetShape(frame, nil)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
func c3g13Equal(t *testing.T, got, want *volume.XBrickMap) {
	t.Helper()
	a, b := *got, *want
	a.ID, b.ID = 0, 0
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("map primary/flags/atlas/masks/bounds/revisions/dirty state differ:\ngot %+v\nwant %+v", a, b)
	}
}

func TestC3g13CanonicalGeometryMatchesOrderedBuilder(t *testing.T) {
	solid := voxelcodec.Brick{Coord: [3]int32{0, 0, 0}, Values: make([]uint8, 512)}
	dense := voxelcodec.Brick{Coord: [3]int32{1, 0, 0}, Values: make([]uint8, 512)}
	for i := range solid.Occupancy {
		solid.Occupancy[i], dense.Occupancy[i] = ^uint64(0), ^uint64(0)
	}
	for i := range solid.Values {
		solid.Values[i] = 6
		dense.Values[i] = uint8(3 + i%2)
	}
	for name, bricks := range map[string][]voxelcodec.Brick{
		"empty":                 nil,
		"signed-sector-revisit": {{Coord: [3]int32{-5, 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{1}}, {Coord: [3]int32{-4, 0, 0}, Occupancy: [8]uint64{3}, Values: []uint8{2, 3}}, {Coord: [3]int32{-4, 4, 0}, Occupancy: [8]uint64{1}, Values: []uint8{4}}, {Coord: [3]int32{-3, 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{5}}, {Coord: [3]int32{-1, -1, -1}, Occupancy: [8]uint64{uint64(1) << 63, 1, 0, 0, 0, 0, 0, uint64(1) << 63}, Values: []uint8{7, 8, 9}}},
		"sparse-uniform":        {{Coord: [3]int32{-1, 0, 0}, Occupancy: [8]uint64{1, 1}, Values: []uint8{3, 3}}},
		"solid-and-dense":       {solid, dense},
		"portable-endpoints":    {{Coord: [3]int32{-268435456, 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{2}}, {Coord: [3]int32{268435455, 0, 0}, Occupancy: [8]uint64{0, 0, 0, 0, 0, 0, 0, uint64(1) << 63}, Values: []uint8{3}}},
	} {
		t.Run(name, func(t *testing.T) {
			shape := c3g13Canonical(t, bricks)
			before, _ := json.Marshal(shape)
			expected := c3g13Reference(shape)
			got, count := compiledShapeGeometry(shape)
			other, otherCount := compiledShapeGeometry(shape)
			c3g13Equal(t, got, expected)
			if count != expected.GetVoxelCount() || count != otherCount || got.ID == other.ID || got.ID == expected.ID {
				t.Fatal("count or independent map identity changed")
			}
			if got.Revision != uint64(count) {
				t.Fatal("initial global canonical revision count changed")
			}
			c3h12aIndependentStorage(t, got, other, expected)
			// A raw primary edit must not alias another build or typed Values storage.
			if count > 0 {
				snapshot := VoxelObjectSnapshotFromXBrickMap(got)
				v := snapshot.Voxels[0]
				got.SetVoxel(v.X, v.Y, v.Z, 99)
				c3g13Equal(t, other, expected)
			}
			after, _ := json.Marshal(shape)
			if !bytes.Equal(before, after) {
				t.Fatal("converter mutated canonical source")
			}
		})
	}
}

func TestC3g13ConvertedGeometryRetainsEditAndAuxHaloSemantics(t *testing.T) {
	shape := c3g13Canonical(t, []voxelcodec.Brick{{Occupancy: [8]uint64{uint64(1) << 7}, Values: []uint8{3}}, {Coord: [3]int32{1, 0, 0}, Occupancy: [8]uint64{1}, Values: []uint8{4}}})
	got, _ := compiledShapeGeometry(shape)
	expected := c3g13Reference(shape)
	for _, geometry := range []*volume.XBrickMap{got, expected} {
		for _, sector := range geometry.Sectors {
			for _, brick := range sector.PackedBricks {
				brick.PrecomputedAux = []byte{1, 2, 3}
			}
		}
	}
	for _, write := range []volume.VoxelWrite{{X: 7, Value: 5}, {X: 8, Value: 0}, {X: 8, Y: -1, Value: 7}, {X: -1, Value: 8}, {X: 7, Value: 0}} {
		got.SetVoxel(write.X, write.Y, write.Z, write.Value)
		expected.SetVoxel(write.X, write.Y, write.Z, write.Value)
		got.ComputeAABB()
		expected.ComputeAABB()
		c3g13Equal(t, got, expected)
		a, av := got.GetVoxel(write.X, write.Y, write.Z)
		b, bv := expected.GetVoxel(write.X, write.Y, write.Z)
		if a != b || av != bv {
			t.Fatal("edited getter differs")
		}
	}
	if len(got.DirtyBricks) == 0 || len(got.DirtySectors) == 0 {
		t.Fatal("live edits failed renderer dirty coverage")
	}
}

// Resource evidence is reported rather than asserted against wall-clock or a
// brittle allocation count: the ordered reference necessarily allocates roughly
// three owners per isolated sector/brick, close to the direct construction floor.
func BenchmarkC3g13CompiledGeometry(b *testing.B) {
	shape := &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: authoredVoxelShapeLattice(.125)}
	for i := 0; i < 256; i++ {
		brick := voxelcodec.Brick{Coord: [3]int32{int32(i * 4), 0, 0}, Values: make([]uint8, 512)}
		for w := range brick.Occupancy {
			brick.Occupancy[w] = ^uint64(0)
		}
		for v := range brick.Values {
			brick.Values[v] = uint8(v%2 + 3)
		}
		shape.Bricks = append(shape.Bricks, brick)
	}
	for _, reference := range []bool{true, false} {
		name := "converter"
		if reference {
			name = "ordered-reference"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(256 * 512)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				var geometry *volume.XBrickMap
				if reference {
					geometry = c3g13Reference(shape)
				} else {
					geometry, _ = compiledShapeGeometry(shape)
				}
				runtime.KeepAlive(geometry)
			}
		})
	}
}
