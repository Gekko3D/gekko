package content

import (
	"reflect"
	"testing"
)

func TestI08LayerPreservesOwnedVisibilityAndProxylessMembership(t *testing.T) {
	p := i05LegacyWorld()
	expected, err := NormalizeImportedWorldPages(p)
	if err != nil {
		t.Fatal(err)
	}
	i := i08Build(t, i08Level(), nil, p)
	if !reflect.DeepEqual(i.POI.Sectors, expected.Sectors) || !reflect.DeepEqual(i.POI.ProxylessPageIndices, expected.ProxylessPageIndices) {
		t.Fatal("visibility/PVS/adjacency or proxyless membership lost")
	}
	p.Sectors[0].FullChunkRefs[0] = TerrainChunkCoordDef{X: 99}
	p.Sectors[0].VisibleSectorRefs[0] = TerrainChunkCoordDef{X: 99}
	p.Sectors[0].AdjacentSectorRefs[0] = TerrainChunkCoordDef{X: 99}
	p.Sectors[0].Tags[0] = "changed"
	if !reflect.DeepEqual(i.POI.Sectors, expected.Sectors) {
		t.Fatal("layer visibility metadata aliases source")
	}
	i.POI.Sectors[0].FullChunkIndices[0] = 99
	i.POI.Sectors[0].VisibleSectorIndices[0] = 99
	i.POI.Sectors[0].AdjacentSectorIndices[0] = 99
	i.POI.Sectors[0].SourceLeafIDs[0] = 99
	i.POI.ProxylessPageIndices[0] = 99
	if expected.Sectors[0].FullChunkIndices[0] == 99 || expected.ProxylessPageIndices[0] == 99 || p.Sectors[0].SourceLeafIDs[0] == 99 {
		t.Fatal("normalized membership aliases another owner")
	}
	i08Query(t, i.POI.SectorIndex, [3]float32{.5, .5, .5}, [3]float32{.5, .5, .5}, 0)
}
