package gekko

import (
	"fmt"
	"math"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

// The existing aux copier uses append from nil. Its capacity may exceed the
// fixed record length because of the allocator size class; capture that bound
// once using the same operation rather than assuming len equals capacity.
var streamedProxyAuxCopyCapacity = int64(cap(append([]byte(nil), make([]byte, volume.VoxelAuxRecordBytes)...)))

func prepareImportedWorldRLEGeometry(source *content.ImportedWorldChunkRLESource, aux *content.ImportedWorldChunkAuxDef) *volume.XBrickMap {
	if source == nil || source.Metadata().NonEmptyVoxelCount == 0 {
		return nil
	}
	geometry := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for voxel := range source.Voxels() {
			if !yield(volume.VoxelWrite{X: voxel.X, Y: voxel.Y, Z: voxel.Z, Value: content.ImportedWorldVoxelMaterialValue(voxel)}) {
				return
			}
		}
	})
	ApplyImportedWorldChunkAuxToXBrickMap(geometry, aux)
	geometry.ComputeAABB()
	geometry.ClearDirty()
	return geometry
}

// streamedRLEProxyPrebuildCharge bounds retained logical storage before building
// either source geometry or the independent registration copy. Bucket slack,
// allocator overhead and transient codec/IO buffers are outside this ledger.
// The optional exact cache key completes the envelope bound; callers that omit
// it get only the source/geometry/metadata bound, excluding that caller-owned key.
func streamedRLEProxyPrebuildCharge(source *content.ImportedWorldChunkRLESource, aux *content.ImportedWorldChunkAuxDef, lod content.ImportedWorldLODDef, exactCacheKey ...string) (int64, error) {
	if source == nil {
		return 0, fmt.Errorf("nil RLE proxy source")
	}
	if len(exactCacheKey) > 1 {
		return 0, fmt.Errorf("multiple RLE proxy cache keys")
	}
	metadata := source.Metadata()
	if metadata == nil || metadata.ChunkSize <= 0 || metadata.NonEmptyVoxelCount < 0 {
		return 0, fmt.Errorf("invalid RLE proxy counts")
	}
	var arithmeticErr error
	add := func(values ...int64) int64 {
		var sum int64
		for _, v := range values {
			if v < 0 || v > math.MaxInt64-sum {
				arithmeticErr = fmt.Errorf("RLE proxy charge overflow")
				return 0
			}
			sum += v
		}
		return sum
	}
	mul := func(a, b int64) int64 {
		if a < 0 || b < 0 || (b != 0 && a > math.MaxInt64/b) {
			arithmeticErr = fmt.Errorf("RLE proxy charge overflow")
			return 0
		}
		return a * b
	}
	cube := func(n int64) int64 { return mul(mul(n, n), n) }
	side := int64(metadata.ChunkSize)
	cells := cube(side)
	occupied := int64(metadata.NonEmptyVoxelCount)
	if arithmeticErr != nil || occupied > cells {
		return 0, fmt.Errorf("invalid or overflowing RLE proxy cube")
	}
	ceil := func(unit int64) int64 { return side/unit + boolInt64(side%unit != 0) }
	bricks := min(occupied, cube(ceil(volume.BrickSize)))
	sectors := min(bricks, cube(ceil(volume.SectorSize)))
	if arithmeticErr != nil {
		return 0, arithmeticErr
	}
	pointer := int64(unsafe.Sizeof((*volume.Brick)(nil)))
	sectorEntry := int64(unsafe.Sizeof([3]int{})) + pointer
	revisionEntry := int64(unsafe.Sizeof([3]int{})) + int64(unsafe.Sizeof(uint64(0)))
	dirtySectorEntry := int64(unsafe.Sizeof([3]int{})) + int64(unsafe.Sizeof(false))
	dirtyBrickEntry := int64(unsafe.Sizeof([6]int{})) + int64(unsafe.Sizeof(false))
	var auxCopies int64
	if aux != nil {
		for _, record := range aux.Records {
			if len(record.Bytes) == volume.VoxelAuxRecordBytes {
				auxCopies = add(auxCopies, streamedProxyAuxCopyCapacity)
			}
		}
	}
	// Each occupied brick's radius halo intersects this many neighboring
	// brick cells per axis, including negative-coordinate neighbors.
	haloAxis := int64(1 + 2*((volume.VoxelNormalExtendedSurfaceFitRadius+volume.BrickSize-1)/volume.BrickSize))
	haloBricks := mul(cube(haloAxis), bricks)
	common := add(int64(unsafe.Sizeof(volume.XBrickMap{})), mul(sectors, add(sectorEntry, revisionEntry)),
		mul(sectors, add(int64(unsafe.Sizeof(volume.Sector{})), mul(cube(volume.SectorBricks), pointer))),
		mul(bricks, int64(unsafe.Sizeof(volume.Brick{}))), auxCopies)
	sourceGeometry := add(common, mul(sectors, dirtySectorEntry), mul(haloBricks, dirtyBrickEntry), int64(unsafe.Sizeof(streamedGeometrySource{})))
	nodes := add(1, sectors, bricks)
	// append-grown child slices have capacity below twice the number of links;
	// the descriptor's node slice is allocated at its exact final length.
	descriptor := add(int64(unsafe.Sizeof(streamedGeometryStorageDescriptor{})),
		mul(nodes, add(int64(unsafe.Sizeof(streamedGeometryStorageNode{})), pointer)), mul(mul(2, add(sectors, bricks)), pointer))
	envelope := runtimeContentGraphCharge(streamedPreparedSectorProxy{rleSource: source, LOD: lod, Chunk: metadata, Aux: aux})
	if envelope == math.MaxInt64 {
		return 0, fmt.Errorf("RLE proxy metadata charge overflow")
	}
	var keyBytes int64
	if len(exactCacheKey) != 0 {
		keyBytes = int64(len(exactCacheKey[0]))
	}
	total := add(envelope, keyBytes, sourceGeometry, common, descriptor, int64(unsafe.Sizeof(streamedGeometryRegistration{})))
	if arithmeticErr != nil {
		return 0, arithmeticErr
	}
	return total, nil
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
