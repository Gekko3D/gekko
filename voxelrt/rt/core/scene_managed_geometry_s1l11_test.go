package core_test

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestS1l11CoreSourceMatchIsPureAndGenerationIndependent(t *testing.T) {
	owner := volume.NewManagedXBrickMap(nil)
	obj := core.NewVoxelObject()
	obj.XBrickMap = volume.NewXBrickMap()
	full, sector := 0, 0
	generation := uint64(7)
	qualified := true
	capture := func() (volume.ManagedGeometryView, uint64, bool) {
		full++
		v, ok := owner.CaptureGeometry()
		return v, generation, ok && qualified
	}
	read := func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
		sector++
		v, ok := owner.CaptureSector(c)
		return v, generation, ok && qualified
	}
	obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, capture, read)
	old := s1l6Capture(t, obj)
	generation = 9
	newer := s1l6Capture(t, obj)
	qualified = false
	generation = 0 // Live producer qualification belongs to the bridge.
	valid := true
	n := testing.AllocsPerRun(100, func() {
		valid = valid && obj.MatchesManagedGeometrySource(old) && obj.MatchesManagedGeometrySource(newer)
	})
	if !valid || n != 0 || full != 2 || sector != 0 {
		t.Fatalf("match=%v allocations=%g callbacks=%d/%d", valid, n, full, sector)
	}
	s1l6Unavailable(t, obj)
	s1l10CoreUnavailable(t, obj, old, [3]int{})
	if full != 3 || sector != 1 {
		t.Fatal("ordinary capture callback behavior changed")
	}
}

func TestS1l11CoreSourceMatchRejectsMissingForeignAndSelection(t *testing.T) {
	for _, kind := range []string{"nil", "missing", "zero", "foreign", "derivative", "nil-derivative", "gpu", "terrain", "planet", "adjacency", "terrain-group", "planet-group", "lod", "invalid-lod", "reinstall", "clear", "nil-full"} {
		t.Run(kind, func(t *testing.T) {
			source := volume.NewXBrickMap()
			source.SetVoxel(0, 0, 0, 1)
			owner := volume.NewManagedXBrickMap(source)
			obj := core.NewVoxelObject()
			obj.XBrickMap = source.Copy()
			calls := 0
			capture := func() (volume.ManagedGeometryView, uint64, bool) {
				calls++
				v, ok := owner.CaptureGeometry()
				return v, 3, ok
			}
			obj.SetManagedGeometryProducer(obj.XBrickMap, capture)
			expected := s1l6Capture(t, obj)
			if !obj.MatchesManagedGeometrySource(expected) {
				t.Fatal("legacy full-only attachment should match")
			}
			switch kind {
			case "nil":
				obj = nil
			case "missing":
				obj = core.NewVoxelObject()
			case "zero":
				expected = core.ManagedGeometryInput{}
			case "foreign":
				other := core.NewVoxelObject()
				other.XBrickMap = volume.NewXBrickMap()
				other.SetManagedGeometryProducer(other.XBrickMap, capture)
				expected = s1l6Capture(t, other)
			case "derivative":
				obj.XBrickMap = obj.XBrickMap.Copy()
			case "nil-derivative":
				obj.XBrickMap = nil
			case "gpu":
				obj.XBrickMap.GPUEditMode = true
			case "terrain":
				obj.IsTerrainChunk = true
			case "planet":
				obj.IsPlanetTile = true
			case "adjacency":
				obj.VoxelAdjacencyGroupID = 1
			case "terrain-group":
				obj.TerrainGroupID = 1
			case "planet-group":
				obj.PlanetTileGroupID = 1
			case "lod", "invalid-lod":
				coarse := obj.XBrickMap.Copy()
				if !obj.SetRenderLOD2(coarse) {
					t.Fatal("LOD setup")
				}
				if kind == "invalid-lod" {
					coarse.SetVoxel(0, 0, 0, 2)
				}
			case "reinstall":
				obj.SetManagedGeometryProducer(obj.XBrickMap, capture)
			case "clear":
				obj.SetManagedGeometryProducer(nil, nil)
			case "nil-full":
				obj.SetManagedGeometryProducerWithSectorReader(obj.XBrickMap, nil, func(c [3]int) (volume.ManagedSectorView, uint64, bool) {
					t.Fatal("reader invoked")
					return volume.ManagedSectorView{}, 0, false
				})
			}
			before := calls
			if obj.MatchesManagedGeometrySource(expected) || calls != before {
				t.Fatal("invalid attachment matched or captured")
			}
		})
	}
}
