package gpu

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func TestMaterialTableIdentityTracksBackingSliceReplacement(t *testing.T) {
	tableA := make([]core.Material, 2)
	tableB := make([]core.Material, 2)

	ptrA, lenA := materialTableIdentity(tableA)
	ptrB, lenB := materialTableIdentity(tableB)

	if ptrA == 0 || ptrB == 0 {
		t.Fatal("expected non-zero table identity pointers")
	}
	if ptrA == ptrB {
		t.Fatal("expected distinct backing slices to produce distinct identities")
	}
	if lenA != 2 || lenB != 2 {
		t.Fatalf("expected identity lengths to match slice lengths, got %d and %d", lenA, lenB)
	}
}

func TestBuildMaterialDataUsesOneRecordPerMaterialAndZeroFallback(t *testing.T) {
	empty := buildMaterialData(nil)
	if len(empty) != materialBlockCapacity*64 {
		t.Fatalf("expected empty material upload buffer size %d, got %d", materialBlockCapacity*64, len(empty))
	}

	table := []core.Material{
		{
			BaseColor:    [4]uint8{1, 2, 3, 4},
			Emissive:     [4]uint8{5, 6, 7, 8},
			Roughness:    0.1,
			Metalness:    0.2,
			IOR:          1.3,
			Transparency: 0.4,
			Emission:     0.5,
			Transmission: 0.6,
			Density:      0.7,
			Refraction:   0.8,
		},
		{
			BaseColor: [4]uint8{9, 10, 11, 12},
		},
	}
	data := buildMaterialData(table)
	if len(data) != len(table)*64 {
		t.Fatalf("expected %d bytes, got %d", len(table)*64, len(data))
	}
}
