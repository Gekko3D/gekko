package core

import (
	"math"
	"testing"
)

func TestP4ImmutableMaterialTableOwnsRowsAndBindings(t *testing.T) {
	input := []Material{DefaultMaterial(), {BaseColor: [4]uint8{1, 2, 3, 4}}}
	want := append([]Material(nil), input...)
	handle, err := NewImmutableMaterialTable(input, "opaque full palette semantics")
	if err != nil {
		t.Fatal(err)
	}
	identity := handle.Identity()
	input[1].BaseColor[0] = 99
	copyRows := handle.MaterialTable()
	copyRows[0].IOR = 99
	if !handle.Matches(want) || handle.Identity() != identity {
		t.Fatal("constructor or accessor exposed immutable backing storage")
	}
	a, b := NewVoxelObject(), NewVoxelObject()
	a.SetImmutableMaterialTable(handle)
	b.SetImmutableMaterialTable(handle)
	a.MaterialTable[1].BaseColor[0] = 88
	if b.MaterialTable[1].BaseColor[0] != 1 || !handle.Matches(want) {
		t.Fatal("objects sharing certification also share mutable legacy rows")
	}
	if a.ImmutableMaterialTable() != nil || b.ImmutableMaterialTable() != handle {
		t.Fatal("raw mutation must detach only the edited object's certification")
	}
	a.MaterialTable[1].BaseColor[0] = 1
	if a.ImmutableMaterialTable() != nil {
		t.Fatal("restoring rows silently resurrected invalidated certification")
	}
	a.SetImmutableMaterialTable(handle)
	a.SetImmutableMaterialTable(nil)
	if a.ImmutableMaterialTable() != nil || !handle.Matches(a.MaterialTable) {
		t.Fatal("nil setter must clear certification while preserving legacy rows")
	}
}

func TestP4ImmutableMaterialValidationAndIdentity(t *testing.T) {
	for _, rows := range [][]Material{nil, {}, make([]Material, 256)} {
		if _, err := NewImmutableMaterialTable(rows, "semantics"); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []struct {
		rows []Material
		key  string
	}{{nil, ""}, {make([]Material, 257), "semantics"}} {
		if _, err := NewImmutableMaterialTable(input.rows, input.key); err == nil {
			t.Fatal("accepted uncertifiable material input")
		}
	}
	base := []Material{DefaultMaterial()}
	a, err := NewImmutableMaterialTable(base, "surface:wood")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewImmutableMaterialTable(append([]Material(nil), base...), "surface:wood")
	c, _ := NewImmutableMaterialTable(base, "surface:stone")
	changed := append([]Material(nil), base...)
	changed[0].BaseColor[0]--
	d, _ := NewImmutableMaterialTable(changed, "surface:wood")
	if a.Identity() != b.Identity() || a.Identity() == c.Identity() || a.Identity() == d.Identity() {
		t.Fatal("identity must include both complete opaque semantics and exact rows")
	}
	if a.Matches(nil) || a.Matches(append(base, Material{})) {
		t.Fatal("row matching ignored local table length")
	}
}

func TestP4ImmutableMaterialMatchesEveryFloatBit(t *testing.T) {
	fields := []struct {
		name string
		set  func(*Material, float32)
	}{
		{"emission", func(m *Material, v float32) { m.Emission = v }},
		{"transmission", func(m *Material, v float32) { m.Transmission = v }},
		{"density", func(m *Material, v float32) { m.Density = v }},
		{"refraction", func(m *Material, v float32) { m.Refraction = v }},
		{"roughness", func(m *Material, v float32) { m.Roughness = v }},
		{"metalness", func(m *Material, v float32) { m.Metalness = v }},
		{"IOR", func(m *Material, v float32) { m.IOR = v }},
		{"transparency", func(m *Material, v float32) { m.Transparency = v }},
	}
	for _, field := range fields {
		t.Run(field.name, func(t *testing.T) {
			for _, bits := range [][2]uint32{{0, 0x80000000}, {0x7fc00023, 0x7fc00024}} {
				a, b := []Material{{}}, []Material{{}}
				field.set(&a[0], math.Float32frombits(bits[0]))
				field.set(&b[0], math.Float32frombits(bits[1]))
				handle, err := NewImmutableMaterialTable(a, "exact")
				if err != nil {
					t.Fatal(err)
				}
				other, err := NewImmutableMaterialTable(b, "exact")
				if err != nil {
					t.Fatal(err)
				}
				if !handle.Matches(a) || handle.Matches(b) || handle.Identity() == other.Identity() {
					t.Fatal("material certification merged signed zero or NaN payload bits")
				}
			}
		})
	}
}
