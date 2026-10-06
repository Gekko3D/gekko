package content

import (
	"math/rand"
	"reflect"
	"testing"
)

// Compare public queries to independently computed world coverage across enough
// irregular records to exercise spatial pruning, signed coordinates and ties.
func TestI08SpatialQueriesMatchWorldCoverage(t *testing.T) {
	rng := rand.New(rand.NewSource(808))
	w := &ImportedWorldDef{SchemaVersion: 2, WorldID: "spatial-review", ChunkSize: 8, VoxelResolution: .1}
	seen := map[TerrainChunkCoordDef]bool{}
	for len(w.Entries) < 512 {
		c := TerrainChunkCoordDef{X: rng.Intn(41) - 20, Y: rng.Intn(9) - 4, Z: rng.Intn(41) - 20}
		if seen[c] {
			continue
		}
		seen[c] = true
		w.Entries = append(w.Entries, ImportedWorldChunkEntryDef{Coord: c, ChunkPath: "metadata-only.gkchunk", NonEmptyVoxelCount: 1})
	}
	i, err := BuildLevelStreamingIndex(i08Level(), nil, w)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 200; n++ {
		lo := [3]float32{float32(rng.Intn(40)-20) * .8, float32(rng.Intn(8)-4) * .8, float32(rng.Intn(40)-20) * .8}
		hi := [3]float32{lo[0] + float32(rng.Intn(8))*.8, lo[1] + float32(rng.Intn(5))*.8, lo[2] + float32(rng.Intn(8))*.8}
		var want []uint32
		for id, e := range w.Entries {
			origin := [3]float32{float32(e.Coord.X) * .8, float32(e.Coord.Y) * .8, float32(e.Coord.Z) * .8}
			match := true
			for a := 0; a < 3; a++ {
				if origin[a] > hi[a] || origin[a]+.8 < lo[a] {
					match = false
				}
			}
			if match {
				want = append(want, uint32(id))
			}
		}
		got, err := i.POI.SourceIndex.Query(lo, hi)
		if err != nil || (len(got) != 0 || len(want) != 0) && !reflect.DeepEqual(got, want) {
			t.Fatalf("query %v..%v: got %v want %v error %v", lo, hi, got, want, err)
		}
	}
}
