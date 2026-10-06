package volume

import (
	"math/bits"
	"unsafe"
)

// ManagedConstructionCharge bounds logical storage of a fresh sealed owner and
// each independent Snapshot. PeakBytes includes constructor coordinate scratch.
// These construction-only bounds are not live-owner or process-memory totals;
// map buckets, allocator overhead and GC retention are outside this domain.
type ManagedConstructionCharge struct {
	OwnerBytes, SnapshotBytes, PeakBytes uint64
}

// PreflightManagedXBrickMap inspects exclusively owned source without allocation.
// It bounds NewManagedXBrickMap and Snapshot storage before construction. Nil
// means empty; malformed or unsupported sector/auxiliary storage is refused.
// Packed occurrences count independently even when source brick pointers alias.
func PreflightManagedXBrickMap(source *XBrickMap) (ManagedConstructionCharge, bool) {
	var out ManagedConstructionCharge
	var failed bool
	add := func(dst *uint64, n uint64) {
		var carry uint64
		*dst, carry = bits.Add64(*dst, n, 0)
		failed = failed || carry != 0
	}
	ptr := uint64(unsafe.Sizeof((*Brick)(nil)))
	mapHeader := uint64(unsafe.Sizeof(XBrickMap{}))
	var mapBytes, backing, sectorHeaders, sharedHeaders, tree, counters, scratch uint64
	mapBytes = mapHeader
	if source != nil {
		if source.GPUEditMode {
			return out, false
		}
		for _, sector := range source.Sectors {
			if sector == nil || len(sector.PackedBricks) != bits.OnesCount64(sector.BrickMask64) {
				return out, false
			}
			add(&mapBytes, uint64(unsafe.Sizeof([3]int{}))+ptr)
			add(&sectorHeaders, uint64(unsafe.Sizeof(Sector{}))+uint64(len(sector.PackedBricks))*ptr)
			add(&sharedHeaders, uint64(unsafe.Sizeof(Sector{}))+2*uint64(len(sector.PackedBricks))*ptr)
			add(&tree, uint64(unsafe.Sizeof(managedTopologyNode{}))+uint64(unsafe.Sizeof(managedIndexNode[*managedSectorRecord]{}))+uint64(unsafe.Sizeof(managedSectorRecord{})))
			// Both constructor indexes sort a coordinate slice. Conservatively
			// retain both scratch arrays through the constructor peak.
			add(&scratch, 2*uint64(unsafe.Sizeof([3]int{})))
			for _, brick := range sector.PackedBricks {
				if brick == nil || len(brick.PrecomputedAux) > VoxelAuxRecordBytes || len(brick.PrecomputedAux) == 0 && cap(brick.PrecomputedAux) != 0 {
					return out, false
				}
				aux := uint64(cap(brick.PrecomputedAux))
				if n := len(brick.PrecomputedAux); n > 0 {
					// nil-append copies round to allocation classes. Twice length
					// (and the minimum byte class) bounds supported aux records.
					aux = max(aux, 8, 2*uint64(n))
				}
				add(&backing, uint64(unsafe.Sizeof(Brick{}))+aux)
				// Initial count/stale maps contain at most one entry per occurrence.
				add(&counters, 2*uint64(unsafe.Sizeof([6]int{}))+uint64(unsafe.Sizeof(int(0)))+uint64(unsafe.Sizeof(false)))
			}
		}
		for range source.SectorRevisions {
			add(&mapBytes, uint64(unsafe.Sizeof([3]int{}))+uint64(unsafe.Sizeof(uint64(0))))
		}
	}
	out.SnapshotBytes = mapBytes
	add(&out.SnapshotBytes, sectorHeaders)
	add(&out.SnapshotBytes, backing)
	out.OwnerBytes = uint64(unsafe.Sizeof(ManagedXBrickMap{}))
	// Base and current have independent map/sector headers and pointer arrays;
	// only their dense brick backing is shared.
	add(&out.OwnerBytes, mapBytes)
	add(&out.OwnerBytes, mapBytes)
	add(&out.OwnerBytes, sectorHeaders)
	add(&out.OwnerBytes, sharedHeaders)
	add(&out.OwnerBytes, backing)
	add(&out.OwnerBytes, tree)
	add(&out.OwnerBytes, counters)
	out.PeakBytes = out.OwnerBytes
	add(&out.PeakBytes, scratch)
	return out, !failed
}
