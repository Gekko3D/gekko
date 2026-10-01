package gpu

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

// Prepared records are borrowed until the next preparation/reset. Historical
// comparisons below always copy the bytes rather than retaining those views.
func s3bClone(b sceneRecordBatch) sceneRecordBatch {
	return sceneRecordBatch{instances: bytes.Clone(b.instances), bvh: bytes.Clone(b.bvh), params: bytes.Clone(b.params)}
}

type s3bWork struct{ instances, params, bvh, uploads uint64 }

func s3bWorkOf(m *GpuBufferManager) s3bWork {
	return s3bWork{m.SceneInstanceRecordBuildCount, m.SceneObjectParamRecordBuildCount, m.SceneBVHBuildCount, m.SceneRecordUploadCount}
}

func s3bFixture(n int) (*GpuBufferManager, *core.Scene) {
	m := &GpuBufferManager{
		Allocations:         make(map[*volume.XBrickMap]*ObjectGpuAllocation),
		MaterialAllocations: make(map[*core.VoxelObject]*MaterialGpuAllocation),
	}
	s := core.NewScene()
	for i := 0; i < n; i++ {
		o := core.NewVoxelObject()
		o.XBrickMap.SetVoxel(0, 0, 0, 1)
		o.Transform.Position = mgl32.Vec3{float32(i * 7), 2, -20}
		s.AddObject(o)
		m.Allocations[o.XBrickMap] = &ObjectGpuAllocation{DirectLookup: defaultDirectSectorLookupMetadata()}
		m.MaterialAllocations[o] = &MaterialGpuAllocation{MaterialOffset: uint32(10 + i)}
	}
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	return m, s
}

func s3bWord(t *testing.T, data []byte, offset int, want uint32) {
	t.Helper()
	if len(data) < offset+4 {
		t.Fatalf("record too short for word at %d: %d bytes", offset, len(data))
	}
	if got := binary.LittleEndian.Uint32(data[offset:]); got != want {
		t.Fatalf("shader word at %d = %#x, want %#x", offset, got, want)
	}
}

func s3bBatchParity(t *testing.T, m *GpuBufferManager, got sceneRecordBatch, objects []*core.VoxelObject, origin mgl32.Vec3) {
	t.Helper()
	for _, c := range []struct {
		name      string
		got, want []byte
	}{
		{"instances", got.instances, buildInstanceData(objects, origin)},
		{"BVH", got.bvh, buildRenderBVHData(objects, origin)},
		{"parameters", got.params, buildObjectParamsData(objects, m.Allocations, m.MaterialAllocations)},
	} {
		if !bytes.Equal(c.got, c.want) {
			t.Fatalf("%s differ from fresh shader records (got %d bytes, want %d)", c.name, len(c.got), len(c.want))
		}
	}
	for i, obj := range objects {
		row := got.instances[i*208 : (i+1)*208]
		s3bWord(t, row, 192, uint32(i))
		for j, v := range obj.Transform.ObjectToWorld() {
			// The origin is an independent world-space translation of the
			// forward matrix; all other elements retain their original bits.
			if j >= 12 && j <= 14 {
				v -= origin[j-12]
			}
			s3bWord(t, row, j*4, math.Float32bits(v))
		}
		localMin, localMax := obj.XBrickMap.ComputeAABB()
		for axis := 0; axis < 3; axis++ {
			s3bWord(t, row, 160+axis*4, math.Float32bits(localMin[axis]))
			s3bWord(t, row, 176+axis*4, math.Float32bits(localMax[axis]))
			worldMin, worldMax := float32(0), float32(0)
			if obj.WorldAABB != nil {
				worldMin = obj.WorldAABB[0][axis] - origin[axis]
				worldMax = obj.WorldAABB[1][axis] - origin[axis]
			}
			s3bWord(t, row, 128+axis*4, math.Float32bits(worldMin))
			s3bWord(t, row, 144+axis*4, math.Float32bits(worldMax))
		}
		for _, offset := range []int{140, 156, 172, 188, 196, 200, 204} {
			s3bWord(t, row, offset, 0)
		}
		params := got.params[i*128 : (i+1)*128]
		if m.Allocations[obj.XBrickMap] != nil {
			s3bWord(t, params, 0, obj.XBrickMap.ID)
			s3bWord(t, params, 24, uint32(len(obj.XBrickMap.Sectors)))
			s3bWord(t, params, 16, ^uint32(0))
		}
	}
	// Independently validate each shader leaf's index and bounds against the
	// same pass row, including objects whose world bounds are absent.
	seen := make([]bool, len(objects))
	for offset := 0; offset < len(got.bvh) && len(objects) > 0; offset += 64 {
		node := got.bvh[offset : offset+64]
		if binary.LittleEndian.Uint32(node[44:]) != 1 {
			continue
		}
		i := int(binary.LittleEndian.Uint32(node[40:]))
		if i >= len(objects) || seen[i] {
			t.Fatalf("invalid or duplicate shader leaf index %d", i)
		}
		seen[i] = true
		for axis := 0; axis < 3; axis++ {
			minB, maxB := float32(0), float32(0)
			if objects[i].WorldAABB != nil {
				minB = objects[i].WorldAABB[0][axis] - origin[axis]
				maxB = objects[i].WorldAABB[1][axis] - origin[axis]
			}
			s3bWord(t, node, axis*4, math.Float32bits(minB))
			s3bWord(t, node, 16+axis*4, math.Float32bits(maxB))
		}
	}
	for i, ok := range seen {
		if !ok {
			t.Fatalf("missing shader leaf for pass row %d", i)
		}
	}
}

func s3bPrepare(t *testing.T, m *GpuBufferManager, s *core.Scene, origin mgl32.Vec3) sceneRecordBatches {
	t.Helper()
	got := m.prepareSceneRecords(s, origin)
	s3bBatchParity(t, m, got.visible, s.VisibleObjects, origin)
	s3bBatchParity(t, m, got.transparent, s.TransparentVisibleObjects, origin)
	s3bBatchParity(t, m, got.shadow, s.ShadowObjects, origin)
	return got
}

func s3bIdle(t *testing.T, m *GpuBufferManager, s *core.Scene, origin mgl32.Vec3) {
	t.Helper()
	before := s3bWorkOf(m)
	s3bPrepare(t, m, s, origin)
	if got := s3bWorkOf(m); got != before {
		t.Fatalf("unchanged preparation compiled/uploaded records: before %+v, after %+v", before, got)
	}
}

func s3bTargetedWork(t *testing.T, before, after uint64, objects int) {
	t.Helper()
	if delta := after - before; delta == 0 || delta >= uint64(objects) {
		t.Fatalf("single-object change encoded %d templates among %d objects; want positive work below a whole-scene rebuild", delta, objects)
	}
}

func TestS3bFirstAndIdlePreserveAllPassRecords(t *testing.T) {
	m, s := s3bFixture(6)
	s.TransparentVisibleObjects = []*core.VoxelObject{s.Objects[4], s.Objects[1]}
	s.ShadowObjects = []*core.VoxelObject{s.Objects[5], s.Objects[2], s.Objects[4]}
	origin := mgl32.Vec3{100, -20, 300}
	s3bPrepare(t, m, s, origin)
	if got := s3bWorkOf(m); got != (s3bWork{instances: 6, params: 6, bvh: 3}) {
		t.Fatalf("first preparation work = %+v; want six unique rows, three nonempty BVHs and no uploads", got)
	}
	if m.SceneRecordObjectCount != 6 {
		t.Fatalf("retained objects = %d, want 6", m.SceneRecordObjectCount)
	}
	s3bIdle(t, m, s, origin)
}

func TestS3bCommittedTransformChangesCompileOnlyAffectedObject(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*core.Transform)
	}{
		{"motion", func(tr *core.Transform) { tr.Position = tr.Position.Add(mgl32.Vec3{3, -2, 5}) }},
		{"nonuniform scale", func(tr *core.Transform) { tr.Scale = mgl32.Vec3{2, 3, .5} }},
		{"pivot", func(tr *core.Transform) { tr.Pivot = mgl32.Vec3{2, -1, .25} }},
	} {
		t.Run(change.name, func(t *testing.T) {
			m, s := s3bFixture(6)
			old := s3bClone(s3bPrepare(t, m, s, mgl32.Vec3{}).visible)
			before := s3bWorkOf(m)
			change.apply(s.Objects[2].Transform)
			s.Objects[2].Transform.Dirty = true
			s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
			if s.Objects[2].Transform.Dirty {
				t.Fatal("Commit must consume the transform dirty flag before gathering")
			}
			got := s3bPrepare(t, m, s, mgl32.Vec3{}).visible
			if bytes.Equal(old.instances, got.instances) || bytes.Equal(old.bvh, got.bvh) {
				t.Fatal("committed transform change left stale instance/BVH bytes")
			}
			s3bTargetedWork(t, before.instances, m.SceneInstanceRecordBuildCount, 6)
			if m.SceneObjectParamRecordBuildCount != before.params {
				t.Fatal("transform change rebuilt parameter rows")
			}
			s3bIdle(t, m, s, mgl32.Vec3{})
		})
	}
}

func TestS3bSameBoundsRotationStillRefreshesMatrices(t *testing.T) {
	m, s := s3bFixture(6)
	o := s.Objects[2]
	o.Transform.Pivot = mgl32.Vec3{.5, .5, .5}
	o.Transform.Dirty = true
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	oldBounds := *o.WorldAABB
	old := s3bClone(s3bPrepare(t, m, s, mgl32.Vec3{}).visible)
	before := s3bWorkOf(m)
	// Exact 180-degree rotation of a cube about its center preserves its AABB.
	o.Transform.Rotation = mgl32.Quat{W: 0, V: mgl32.Vec3{0, 1, 0}}
	o.Transform.Dirty = true
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	if o.Transform.Dirty || *o.WorldAABB != oldBounds {
		t.Fatal("fixture requires consumed Dirty and exactly unchanged world bounds")
	}
	got := s3bPrepare(t, m, s, mgl32.Vec3{}).visible
	if bytes.Equal(old.instances, got.instances) {
		t.Fatal("same-bounds rotation left stale matrices")
	}
	if !bytes.Equal(old.bvh, got.bvh) || m.SceneBVHBuildCount != before.bvh {
		t.Fatal("matrix-only rotation rebuilt unchanged BVH bounds")
	}
	s3bTargetedWork(t, before.instances, m.SceneInstanceRecordBuildCount, 6)
	if m.SceneObjectParamRecordBuildCount != before.params {
		t.Fatal("rotation rebuilt parameter rows")
	}
	s3bIdle(t, m, s, mgl32.Vec3{})
}

func TestS3bSharedMapEditAndWorldBoundPresence(t *testing.T) {
	m, s := s3bFixture(6)
	s.Objects[1].XBrickMap = s.Objects[0].XBrickMap
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	old := s3bClone(s3bPrepare(t, m, s, mgl32.Vec3{}).visible)
	before := s3bWorkOf(m)
	s.Objects[0].XBrickMap.SetVoxel(2, 0, 0, 1)
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	got := s3bPrepare(t, m, s, mgl32.Vec3{}).visible
	for _, i := range []int{0, 1} {
		if bytes.Equal(old.instances[i*208:(i+1)*208], got.instances[i*208:(i+1)*208]) {
			t.Fatalf("shared-map edit left object %d stale", i)
		}
	}
	if delta := m.SceneInstanceRecordBuildCount - before.instances; delta < 2 || delta >= 6 {
		t.Fatalf("shared-map edit encoded %d instance rows", delta)
	}
	if m.SceneObjectParamRecordBuildCount != before.params {
		t.Fatal("same-sector bounds edit rebuilt unchanged parameters")
	}
	// The existing builders accept absent WorldAABB on a valid pass object.
	// Exercise presence and in-place values without changing pass membership.
	o := s.Objects[3]
	saved := *o.WorldAABB
	o.WorldAABB = nil
	s3bPrepare(t, m, s, mgl32.Vec3{1, 2, 3})
	s3bIdle(t, m, s, mgl32.Vec3{1, 2, 3})
	o.WorldAABB = &saved
	s3bPrepare(t, m, s, mgl32.Vec3{1, 2, 3})
	o.WorldAABB[1][1] += 4
	s3bPrepare(t, m, s, mgl32.Vec3{1, 2, 3})
	s3bIdle(t, m, s, mgl32.Vec3{1, 2, 3})
}

func TestS3bOriginChangesInstancesAndBVHOnly(t *testing.T) {
	m, s := s3bFixture(4)
	s.TransparentVisibleObjects = []*core.VoxelObject{s.Objects[1]}
	s.ShadowObjects = []*core.VoxelObject{s.Objects[3], s.Objects[0]}
	first := s3bPrepare(t, m, s, mgl32.Vec3{})
	old := []sceneRecordBatch{s3bClone(first.visible), s3bClone(first.transparent), s3bClone(first.shadow)}
	before := s3bWorkOf(m)
	origin := mgl32.Vec3{10000, -200, 3000}
	next := s3bPrepare(t, m, s, origin)
	for i, got := range []sceneRecordBatch{next.visible, next.transparent, next.shadow} {
		if bytes.Equal(old[i].instances, got.instances) || bytes.Equal(old[i].bvh, got.bvh) {
			t.Fatalf("pass %d did not adopt render origin", i)
		}
		if !bytes.Equal(old[i].params, got.params) {
			t.Fatalf("origin changed pass %d parameters", i)
		}
	}
	if m.SceneObjectParamRecordBuildCount != before.params {
		t.Fatal("origin re-encoded parameter templates")
	}
	if m.SceneBVHBuildCount != before.bvh+3 {
		t.Fatal("origin must rebuild each changed nonempty relative BVH")
	}
	s3bIdle(t, m, s, origin)
}

func TestS3bEveryEncodedParameterInputTracksInPlaceMutation(t *testing.T) {
	mutations := []struct {
		name   string
		offset int
		want   uint32
		apply  func(*core.VoxelObject, *ObjectGpuAllocation, *MaterialGpuAllocation)
	}{
		{"LOD", 20, math.Float32bits(123.25), func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.LODThreshold = 123.25 }},
		{"LOD negative zero", 20, 0x80000000, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.LODThreshold = math.Float32frombits(0x80000000)
		}},
		{"LOD NaN payload", 20, 0x7fc00023, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.LODThreshold = math.Float32frombits(0x7fc00023)
		}},
		{"AO", 28, uint32(core.AmbientOcclusionModeDisabled), func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.AmbientOcclusionMode = core.AmbientOcclusionModeDisabled
		}},
		{"shadow group", 32, 19, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.ShadowGroupID = 19 }},
		{"shadow epsilon", 36, math.Float32bits(.0125), func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.ShadowSeamWorldEpsilon = .0125
		}},
		{"epsilon negative zero", 36, 0x80000000, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.ShadowSeamWorldEpsilon = math.Float32frombits(0x80000000)
		}},
		{"epsilon NaN payload", 36, 0x7fc00031, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.ShadowSeamWorldEpsilon = math.Float32frombits(0x7fc00031)
		}},
		{"terrain flag", 40, 1, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.IsTerrainChunk = true }},
		{"terrain group", 44, 29, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.TerrainGroupID = 29 }},
		{"terrain x", 48, 0xfffffffe, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.TerrainChunkCoord[0] = -2
		}},
		{"terrain y", 52, 3, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.TerrainChunkCoord[1] = 3
		}},
		{"terrain z", 56, 4, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			o.TerrainChunkCoord[2] = 4
		}},
		{"terrain size", 60, 32, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.TerrainChunkSize = 32 }},
		{"planet flag", 64, 1, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.IsPlanetTile = true }},
		{"planet group", 68, 39, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.PlanetTileGroupID = 39 }},
		{"emitter link", 72, 49, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.EmitterLinkID = 49 }},
		{"planet face", 80, 5, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.PlanetTileFace = 5 }},
		{"planet level", 84, 6, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.PlanetTileLevel = 6 }},
		{"planet x", 88, 7, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.PlanetTileX = 7 }},
		{"planet y", 92, 8, func(o *core.VoxelObject, _ *ObjectGpuAllocation, _ *MaterialGpuAllocation) { o.PlanetTileY = 8 }},
		{"lookup origin x", 96, 0xfffffffd, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.Origin[0] = -3
		}},
		{"lookup origin y", 100, 4, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.Origin[1] = 4
		}},
		{"lookup origin z", 104, 5, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.Origin[2] = 5
		}},
		{"lookup mode", 108, LookupModeDirect, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.LookupMode = LookupModeDirect
		}},
		{"lookup extent x", 112, 2, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.Extent[0] = 2
		}},
		{"lookup extent y", 116, 3, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.Extent[1] = 3
		}},
		{"lookup extent z", 120, 4, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.Extent[2] = 4
		}},
		{"lookup base", 124, 59, func(_ *core.VoxelObject, a *ObjectGpuAllocation, _ *MaterialGpuAllocation) {
			a.DirectLookup.TableBase = 59
		}},
		{"material offset", 12, 79 * 4, func(_ *core.VoxelObject, _ *ObjectGpuAllocation, a *MaterialGpuAllocation) { a.MaterialOffset = 79 }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			m, s := s3bFixture(4)
			o := s.Objects[1]
			s.TransparentVisibleObjects = []*core.VoxelObject{o, s.Objects[2]}
			s.ShadowObjects = []*core.VoxelObject{s.Objects[3], o}
			old := s3bClone(s3bPrepare(t, m, s, mgl32.Vec3{}).visible)
			before := s3bWorkOf(m)
			mutation.apply(o, m.Allocations[o.XBrickMap], m.MaterialAllocations[o])
			got := s3bPrepare(t, m, s, mgl32.Vec3{})
			s3bWord(t, got.visible.params[128:256], mutation.offset, mutation.want)
			s3bWord(t, got.transparent.params[:128], mutation.offset, mutation.want)
			s3bWord(t, got.shadow.params[128:256], mutation.offset, mutation.want)
			if bytes.Equal(old.params, got.visible.params) {
				t.Fatal("parameter mutation left stale shader bytes")
			}
			if m.SceneInstanceRecordBuildCount != before.instances || m.SceneBVHBuildCount != before.bvh {
				t.Fatal("parameter-only mutation rebuilt instances/BVHs")
			}
			s3bTargetedWork(t, before.params, m.SceneObjectParamRecordBuildCount, 4)
			s3bIdle(t, m, s, mgl32.Vec3{})
			// Revert to the fixture's public values. This also checks that
			// reused rows clear true flags, nonzero metadata and negative zero.
			before = s3bWorkOf(m)
			o.LODThreshold = 50
			o.AmbientOcclusionMode = core.AmbientOcclusionModeDefault
			o.ShadowGroupID, o.EmitterLinkID = 0, 0
			o.ShadowSeamWorldEpsilon = 0
			o.IsTerrainChunk, o.IsPlanetTile = false, false
			o.TerrainGroupID, o.PlanetTileGroupID = 0, 0
			o.TerrainChunkCoord = [3]int{}
			o.TerrainChunkSize = 0
			o.PlanetTileFace, o.PlanetTileLevel, o.PlanetTileX, o.PlanetTileY = 0, 0, 0, 0
			m.Allocations[o.XBrickMap].DirectLookup = defaultDirectSectorLookupMetadata()
			m.MaterialAllocations[o].MaterialOffset = 11
			got = s3bPrepare(t, m, s, mgl32.Vec3{})
			if !bytes.Equal(old.params, got.visible.params) {
				t.Fatal("reverted metadata left stale parameter fields")
			}
			if m.SceneInstanceRecordBuildCount != before.instances || m.SceneBVHBuildCount != before.bvh {
				t.Fatal("reverting parameters rebuilt instances/BVHs")
			}
			s3bTargetedWork(t, before.params, m.SceneObjectParamRecordBuildCount, 4)
			s3bIdle(t, m, s, mgl32.Vec3{})
		})
	}
}

func TestS3bLODParameterDistinguishesSignedZero(t *testing.T) {
	m, s := s3bFixture(4)
	o := s.Objects[1]
	o.LODThreshold = 0
	s3bPrepare(t, m, s, mgl32.Vec3{})
	for _, bits := range []uint32{0x80000000, 0} {
		before := s3bWorkOf(m)
		o.LODThreshold = math.Float32frombits(bits)
		got := s3bPrepare(t, m, s, mgl32.Vec3{}).visible
		s3bWord(t, got.params[128:256], 20, bits)
		s3bTargetedWork(t, before.params, m.SceneObjectParamRecordBuildCount, 4)
		if m.SceneInstanceRecordBuildCount != before.instances || m.SceneBVHBuildCount != before.bvh {
			t.Fatal("float-bit-only metadata mutation rebuilt instances/BVHs")
		}
		s3bIdle(t, m, s, mgl32.Vec3{})
	}
}

func TestS3bAllocationAdmissionRemovalAndMapMetadata(t *testing.T) {
	m, s := s3bFixture(4)
	o := s.Objects[1]
	delete(m.Allocations, o.XBrickMap)
	got := s3bPrepare(t, m, s, mgl32.Vec3{}).visible
	if !bytes.Equal(got.params[128:256], make([]byte, 128)) {
		t.Fatal("missing geometry must encode the existing zero parameter row")
	}
	checkParamsOnly := func(mutate func(), offset int, want uint32) {
		t.Helper()
		before := s3bWorkOf(m)
		mutate()
		got := s3bPrepare(t, m, s, mgl32.Vec3{}).visible
		s3bWord(t, got.params[128:256], offset, want)
		if m.SceneInstanceRecordBuildCount != before.instances || m.SceneBVHBuildCount != before.bvh {
			t.Fatal("allocation/map metadata change rebuilt unrelated instances/BVHs")
		}
		s3bTargetedWork(t, before.params, m.SceneObjectParamRecordBuildCount, 4)
		s3bIdle(t, m, s, mgl32.Vec3{})
	}
	checkParamsOnly(func() {
		m.Allocations[o.XBrickMap] = &ObjectGpuAllocation{DirectLookup: defaultDirectSectorLookupMetadata()}
	}, 0, o.XBrickMap.ID)
	checkParamsOnly(func() { delete(m.MaterialAllocations, o) }, 12, 0)
	checkParamsOnly(func() { m.MaterialAllocations[o] = &MaterialGpuAllocation{MaterialOffset: 81} }, 12, 324)
	checkParamsOnly(func() { o.XBrickMap.ID = 123456 }, 0, 123456)
	checkParamsOnly(func() { delete(m.Allocations, o.XBrickMap) }, 0, 0)
	checkParamsOnly(func() {
		m.Allocations[o.XBrickMap] = &ObjectGpuAllocation{DirectLookup: defaultDirectSectorLookupMetadata()}
	}, 0, 123456)
	// A new map with equal bounds must refresh parameters even without an
	// instance/bounds change; allocation ownership follows the replacement.
	replacement := volume.NewXBrickMap()
	replacement.SetVoxel(0, 0, 0, 1)
	m.Allocations[replacement] = &ObjectGpuAllocation{DirectLookup: defaultDirectSectorLookupMetadata()}
	checkParamsOnly(func() { o.XBrickMap = replacement; s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{}) }, 0, replacement.ID)
	// Sector count changes through real editing also refresh bounds and records.
	before := s3bWorkOf(m)
	replacement.SetVoxel(volume.SectorSize+1, 0, 0, 1)
	s.Commit([6]mgl32.Vec4{}, core.SceneCommitOptions{})
	got = s3bPrepare(t, m, s, mgl32.Vec3{}).visible
	s3bWord(t, got.params[128:256], 24, 2)
	s3bTargetedWork(t, before.params, m.SceneObjectParamRecordBuildCount, 4)
	s3bTargetedWork(t, before.instances, m.SceneInstanceRecordBuildCount, 4)
	s3bIdle(t, m, s, mgl32.Vec3{})
}

func TestS3bPassOrderMembershipAndIndependence(t *testing.T) {
	for _, pass := range []string{"visible", "transparent", "shadow"} {
		t.Run(pass, func(t *testing.T) {
			m, s := s3bFixture(6)
			s.VisibleObjects = []*core.VoxelObject{s.Objects[0], s.Objects[1]}
			s.TransparentVisibleObjects = []*core.VoxelObject{s.Objects[2], s.Objects[3]}
			s.ShadowObjects = []*core.VoxelObject{s.Objects[4], s.Objects[5]}
			var list *[]*core.VoxelObject
			switch pass {
			case "visible":
				list = &s.VisibleObjects
			case "transparent":
				list = &s.TransparentVisibleObjects
			case "shadow":
				list = &s.ShadowObjects
			}
			s3bPrepare(t, m, s, mgl32.Vec3{})
			for step, order := range [][]*core.VoxelObject{{(*list)[1], (*list)[0]}, {(*list)[0]}, {(*list)[0], s.Objects[0], s.Objects[2], s.Objects[4]}, {}} {
				// Remove duplicate identities from the candidate sequence.
				unique := make([]*core.VoxelObject, 0, len(order))
				for _, obj := range order {
					found := false
					for _, x := range unique {
						if x == obj {
							found = true
						}
					}
					if !found {
						unique = append(unique, obj)
					}
				}
				before := s3bWorkOf(m)
				*list = unique
				s3bPrepare(t, m, s, mgl32.Vec3{})
				wantBVH := before.bvh
				if len(unique) > 0 {
					wantBVH++
				}
				if m.SceneBVHBuildCount != wantBVH {
					t.Fatal("one pass membership change rebuilt an unrelated nonempty BVH")
				}
				if step == 0 && (m.SceneInstanceRecordBuildCount != before.instances || m.SceneObjectParamRecordBuildCount != before.params) {
					t.Fatal("pure pass reorder must reuse existing unique instance and parameter templates while patching pass indices")
				}
				s3bIdle(t, m, s, mgl32.Vec3{})
			}
		})
	}
}

func TestS3bCommitTransparencyHiddenResidencyAndShadowOnlyOwnership(t *testing.T) {
	m, s := s3bFixture(3)
	s.Objects[0].Transform.Position = mgl32.Vec3{0, 0, -20}
	s.Objects[1].Transform.Position = mgl32.Vec3{200, 0, -20}
	s.Objects[2].RenderEnabled = false
	for _, o := range s.Objects {
		o.Transform.Dirty = true
	}
	s.Lights = []core.Light{{Direction: [4]float32{0, -1, 0, 0}, Params: [4]float32{0, 0, float32(core.LightTypeDirectional), 1}}}
	planes := (&core.CameraState{}).ExtractFrustum(mgl32.Perspective(mgl32.DegToRad(90), 1, 1, 100))
	s.Commit(planes, core.SceneCommitOptions{})
	if len(s.VisibleObjects) != 1 || len(s.ShadowObjects) != 2 || len(s.Objects) != 3 {
		t.Fatal("fixture requires visible, shadow-only and hidden resident objects")
	}
	s3bPrepare(t, m, s, mgl32.Vec3{})
	if m.SceneRecordObjectCount != 2 {
		t.Fatal("owner must retain the pass union, including shadow-only but excluding hidden resident geometry")
	}
	before := s3bWorkOf(m)
	s.Objects[0].MaterialTable = []core.Material{core.DefaultMaterial(), {Transparency: .4}}
	s.Commit(planes, core.SceneCommitOptions{})
	if len(s.TransparentVisibleObjects) != 1 {
		t.Fatal("real Commit must select transparency")
	}
	s3bPrepare(t, m, s, mgl32.Vec3{})
	if m.SceneInstanceRecordBuildCount != before.instances || m.SceneObjectParamRecordBuildCount != before.params || m.SceneBVHBuildCount != before.bvh+1 {
		t.Fatal("transparent pass admission rebuilt existing templates or unrelated BVHs")
	}
	s.Objects[0].MaterialTable[1].Transparency = 0
	s.Commit(planes, core.SceneCommitOptions{})
	got := s3bPrepare(t, m, s, mgl32.Vec3{})
	if !bytes.Equal(got.transparent.instances, make([]byte, 208)) || !bytes.Equal(got.transparent.params, make([]byte, 128)) {
		t.Fatal("transparency removal retained stale rows")
	}
	s.Objects[0].RenderEnabled = false
	s.Commit(planes, core.SceneCommitOptions{})
	s3bPrepare(t, m, s, mgl32.Vec3{})
	if m.SceneRecordObjectCount != 1 {
		t.Fatal("hidden resident removal must release its templates and retain shadow-only caster")
	}
	s3bIdle(t, m, s, mgl32.Vec3{})
}

func TestS3bShrinkSceneSwitchEmptyAndInvalidateReleaseOwnership(t *testing.T) {
	m, s := s3bFixture(6)
	s.TransparentVisibleObjects = []*core.VoxelObject{s.Objects[4]}
	s.ShadowObjects = []*core.VoxelObject{s.Objects[5]}
	s3bPrepare(t, m, s, mgl32.Vec3{})
	s.VisibleObjects = s.VisibleObjects[:1]
	s.TransparentVisibleObjects = nil
	s3bPrepare(t, m, s, mgl32.Vec3{})
	if m.SceneRecordObjectCount != 2 {
		t.Fatal("shrink must release removed rows while keeping shadow-only ownership")
	}
	_, other := s3bFixture(2)
	for _, o := range other.Objects {
		m.Allocations[o.XBrickMap] = &ObjectGpuAllocation{DirectLookup: defaultDirectSectorLookupMetadata()}
	}
	s3bPrepare(t, m, other, mgl32.Vec3{})
	if m.SceneRecordObjectCount != 2 {
		t.Fatal("scene switch retained historical scene objects")
	}
	before := s3bWorkOf(m)
	m.InvalidateSceneRecords()
	if m.SceneRecordObjectCount != 0 || s3bWorkOf(m) != before {
		t.Fatal("invalidation must discard ownership and preserve cumulative work counters")
	}
	s3bPrepare(t, m, other, mgl32.Vec3{})
	if m.SceneInstanceRecordBuildCount <= before.instances || m.SceneObjectParamRecordBuildCount <= before.params || m.SceneBVHBuildCount <= before.bvh {
		t.Fatal("explicit invalidation did not force fresh preparation")
	}
	s3bIdle(t, m, other, mgl32.Vec3{})
	empty := core.NewScene()
	before = s3bWorkOf(m)
	got := s3bPrepare(t, m, empty, mgl32.Vec3{})
	for _, batch := range []sceneRecordBatch{got.visible, got.transparent, got.shadow} {
		if !bytes.Equal(batch.instances, make([]byte, 208)) || !bytes.Equal(batch.bvh, make([]byte, 64)) || !bytes.Equal(batch.params, make([]byte, 128)) {
			t.Fatal("empty pass must publish exact zero sentinels without stale tails")
		}
	}
	if m.SceneRecordObjectCount != 0 || s3bWorkOf(m) != before {
		t.Fatal("empty preparation must release all templates without encoding rows or building a nonempty BVH")
	}
	s3bIdle(t, m, empty, mgl32.Vec3{})
	var nilManager *GpuBufferManager
	nilManager.InvalidateSceneRecords()
	newManager := &GpuBufferManager{}
	s3bPrepare(t, newManager, empty, mgl32.Vec3{})
	if newManager.SceneRecordObjectCount != 0 || s3bWorkOf(newManager) != (s3bWork{}) {
		t.Fatal("new empty manager must begin without record ownership or work")
	}
}
