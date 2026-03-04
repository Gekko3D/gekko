package ecs

import (
	"encoding/binary"
	"hash/fnv"
	"slices"
)

// CanonicalizeKey sorts and deduplicates component IDs.
func CanonicalizeKey(key []uint32) []uint32 {
	dedup := make(map[uint32]struct{}, len(key))
	for _, v := range key {
		dedup[v] = struct{}{}
	}

	res := make([]uint32, 0, len(dedup))
	for k := range dedup {
		res = append(res, k)
	}
	slices.Sort(res)
	return res
}

// CombineKeys merges, deduplicates and sorts two archetype keys.
func CombineKeys(a, b []uint32) []uint32 {
	merged := make([]uint32, 0, len(a)+len(b))
	merged = append(merged, a...)
	merged = append(merged, b...)
	return CanonicalizeKey(merged)
}

// ArchetypeID is a stable 64-bit hash of a canonical archetype key.
func ArchetypeID(key []uint32) uint64 {
	hash := fnv.New64a()
	for _, id := range key {
		b := make([]byte, 8)
		binary.LittleEndian.PutUint64(b, uint64(id))
		hash.Write(b)
	}
	return hash.Sum64()
}
