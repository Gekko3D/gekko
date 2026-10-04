package gekko

import (
	"math"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

var c3h11SectorKeys = [][3]int{{-2, 1, -3}, {1, -2, 0}}

func c3h11Geometry() *volume.XBrickMap {
	m := volume.NewXBrickMap()
	m.Revision = 23
	for i, key := range c3h11SectorKeys {
		s := volume.NewSector(key[0], key[1], key[2])
		for j := 0; j < 2; j++ {
			b := volume.NewBrick()
			b.SetVoxel(j*7, j*7, j*7, uint8(7+i*2+j))
			b.Flags = volume.BrickFlagUniformMaterial
			b.AtlasOffset = uint32(7 + i*2 + j)
			b.PrecomputedAux = []byte{1, 2, 3}
			s.PackedBricks = append(s.PackedBricks, b)
		}
		s.BrickMask64 = 1 | uint64(1)<<63
		m.Sectors[key] = s
		m.SectorRevisions[key] = 11
		m.DirtySectors[key] = true
		m.DirtyBricks[[6]int{key[0], key[1], key[2], 0, 0, 0}] = true
	}
	return m
}

// Copy raw storage without normalization, including malformed fixtures. This
// serves both as an independently owned baseline and a before-call snapshot.
func c3h11OwnedCopy(m *volume.XBrickMap) *volume.XBrickMap {
	if m == nil {
		return nil
	}
	out := *m
	if m.Sectors != nil {
		out.Sectors = make(map[[3]int]*volume.Sector, len(m.Sectors))
		for key, s := range m.Sectors {
			if s == nil {
				out.Sectors[key] = nil
				continue
			}
			sc := *s
			if s.PackedBricks != nil {
				sc.PackedBricks = make([]*volume.Brick, len(s.PackedBricks))
				for i, b := range s.PackedBricks {
					if b != nil {
						bc := *b
						if b.PrecomputedAux != nil {
							bc.PrecomputedAux = append([]byte{}, b.PrecomputedAux...)
						}
						sc.PackedBricks[i] = &bc
					}
				}
			}
			out.Sectors[key] = &sc
		}
	}
	if m.DirtySectors != nil {
		out.DirtySectors = make(map[[3]int]bool, len(m.DirtySectors))
		for k, v := range m.DirtySectors {
			out.DirtySectors[k] = v
		}
	}
	if m.DirtyBricks != nil {
		out.DirtyBricks = make(map[[6]int]bool, len(m.DirtyBricks))
		for k, v := range m.DirtyBricks {
			out.DirtyBricks[k] = v
		}
	}
	if m.SectorRevisions != nil {
		out.SectorRevisions = make(map[[3]int]uint64, len(m.SectorRevisions))
		for k, v := range m.SectorRevisions {
			out.SectorRevisions[k] = v
		}
	}
	return &out
}

func c3h11CheckGeometry(t *testing.T, current, baseline *volume.XBrickMap, want bool) {
	t.Helper()
	beforeCurrent, beforeBaseline := c3h11OwnedCopy(current), c3h11OwnedCopy(baseline)
	for repeat := 0; repeat < 2; repeat++ {
		if got := compiledAssetPrimaryGeometryMatches(current, baseline); got != want {
			t.Errorf("geometry qualification = %v, want %v", got, want)
		}
	}
	if !reflect.DeepEqual(current, beforeCurrent) || !reflect.DeepEqual(baseline, beforeBaseline) {
		t.Fatal("geometry qualification mutated or repaired borrowed storage")
	}
}

func TestC3h11ExactPrimaryGeometry(t *testing.T) {
	mutations := []struct {
		name string
		edit func(*volume.XBrickMap)
	}{
		{"raw occupied payload", func(m *volume.XBrickMap) { m.Sectors[c3h11SectorKeys[0]].PackedBricks[0].Payload[0][0][0]++ }},
		{"raw payload outside occupancy", func(m *volume.XBrickMap) { m.Sectors[c3h11SectorKeys[0]].PackedBricks[1].Payload[0][6][5] = 7 }},
		{"brick flags", func(m *volume.XBrickMap) {
			m.Sectors[c3h11SectorKeys[1]].PackedBricks[1].Flags ^= volume.BrickFlagSolid
		}},
		{"brick occupancy", func(m *volume.XBrickMap) { m.Sectors[c3h11SectorKeys[1]].PackedBricks[0].OccupancyMask64 ^= 1 << 19 }},
		{"sector mask same cardinality", func(m *volume.XBrickMap) { m.Sectors[c3h11SectorKeys[0]].BrickMask64 = 2 | uint64(1)<<63 }},
		{"packed order", func(m *volume.XBrickMap) {
			s := m.Sectors[c3h11SectorKeys[1]]
			s.PackedBricks[0], s.PackedBricks[1] = s.PackedBricks[1], s.PackedBricks[0]
		}},
		{"signed sector relocation", func(m *volume.XBrickMap) {
			old := c3h11SectorKeys[0]
			key := [3]int{2, 1, -3}
			s := m.Sectors[old]
			delete(m.Sectors, old)
			s.Coords = key
			m.Sectors[key] = s
		}},
		{"sector removal", func(m *volume.XBrickMap) { delete(m.Sectors, c3h11SectorKeys[1]) }},
		{"extra empty sector", func(m *volume.XBrickMap) {
			key := [3]int{-5, 0, 0}
			m.Sectors[key] = volume.NewSector(key[0], key[1], key[2])
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			current := c3h11Geometry()
			baseline := c3h11OwnedCopy(current)
			tc.edit(current)
			if current.ID != baseline.ID || current.Revision != baseline.Revision {
				t.Fatal("raw edit fixture must preserve metadata identity/revision")
			}
			c3h11CheckGeometry(t, current, baseline, false)
		})
	}
	t.Run("independent equal geometry", func(t *testing.T) {
		current := c3h11Geometry()
		c3h11CheckGeometry(t, current, c3h11OwnedCopy(current), true)
	})
	t.Run("bookkeeping and GPU outputs do not define geometry", func(t *testing.T) {
		current := c3h11Geometry()
		baseline := c3h11OwnedCopy(current)
		current.ID++
		current.Revision++
		current.ClearDirty()
		if current.StructureDirty == baseline.StructureDirty {
			t.Fatal("fixture must differ in structure bookkeeping")
		}
		current.AABBDirty = false
		current.CachedMin[0], current.CachedMax[2] = -999, 999
		for key, s := range current.Sectors {
			current.SectorRevisions[key]++
			for _, b := range s.PackedBricks {
				b.AtlasOffset = 99
				b.PrecomputedAux = []byte{99}
			}
		}
		c3h11CheckGeometry(t, current, baseline, true)
	})
}

func TestC3h11MalformedGeometryFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		edit func(*volume.XBrickMap) *volume.XBrickMap
	}{
		{"nil", func(*volume.XBrickMap) *volume.XBrickMap { return nil }},
		{"empty", func(*volume.XBrickMap) *volume.XBrickMap { return volume.NewXBrickMap() }},
		{"only empty sectors", func(m *volume.XBrickMap) *volume.XBrickMap {
			for _, s := range m.Sectors {
				s.BrickMask64 = 0
				s.PackedBricks = nil
			}
			return m
		}},
		{"allocated empty brick", func(m *volume.XBrickMap) *volume.XBrickMap {
			key := c3h11SectorKeys[0]
			m.Sectors = map[[3]int]*volume.Sector{key: {Coords: key, BrickMask64: 1, PackedBricks: []*volume.Brick{volume.NewBrick()}}}
			return m
		}},
		{"nil sectors", func(m *volume.XBrickMap) *volume.XBrickMap { m.Sectors = nil; return m }},
		{"GPU edit", func(m *volume.XBrickMap) *volume.XBrickMap { m.GPUEditMode = true; return m }},
		{"nil sector", func(m *volume.XBrickMap) *volume.XBrickMap { m.Sectors[c3h11SectorKeys[0]] = nil; return m }},
		{"key coordinate mismatch", func(m *volume.XBrickMap) *volume.XBrickMap { m.Sectors[c3h11SectorKeys[0]].Coords[0]++; return m }},
		{"too few packed bricks", func(m *volume.XBrickMap) *volume.XBrickMap {
			s := m.Sectors[c3h11SectorKeys[0]]
			s.PackedBricks = s.PackedBricks[:1]
			return m
		}},
		{"too many packed bricks", func(m *volume.XBrickMap) *volume.XBrickMap {
			s := m.Sectors[c3h11SectorKeys[0]]
			s.PackedBricks = append(s.PackedBricks, volume.NewBrick())
			return m
		}},
		{"nil packed bricks", func(m *volume.XBrickMap) *volume.XBrickMap {
			m.Sectors[c3h11SectorKeys[0]].PackedBricks = nil
			return m
		}},
		{"nil brick", func(m *volume.XBrickMap) *volume.XBrickMap {
			m.Sectors[c3h11SectorKeys[0]].PackedBricks[1] = nil
			return m
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			good := c3h11Geometry()
			bad := tc.edit(c3h11OwnedCopy(good))
			c3h11CheckGeometry(t, bad, good, false)
			c3h11CheckGeometry(t, good, bad, false)
			c3h11CheckGeometry(t, bad, c3h11OwnedCopy(bad), false)
		})
	}
}

func TestC3h11CorrespondingAliasRejection(t *testing.T) {
	for _, kind := range []string{"map", "sector", "brick"} {
		t.Run(kind, func(t *testing.T) {
			current := c3h11Geometry()
			baseline := c3h11OwnedCopy(current)
			key := c3h11SectorKeys[1]
			switch kind {
			case "map":
				baseline = current
			case "sector":
				baseline.Sectors[key] = current.Sectors[key]
			case "brick":
				baseline.Sectors[key].PackedBricks[1] = current.Sectors[key].PackedBricks[1]

			}
			c3h11CheckGeometry(t, current, baseline, false)
		})
	}
}

func c3h11SameMaterial(a, b core.Material) bool {
	if math.Float32bits(a.Transparency) != math.Float32bits(b.Transparency) || math.Float32bits(a.Transmission) != math.Float32bits(b.Transmission) {
		return false
	}
	// Preserve bit-level checks for intentional NaNs, then compare every field.
	a.Transparency, b.Transparency = 0, 0
	a.Transmission, b.Transmission = 0, 0
	return a == b
}

func TestC3h11CurrentMaterialEligibility(t *testing.T) {
	opaque := core.DefaultMaterial()
	cases := []struct {
		name string
		edit func(*core.Material)
	}{
		{"alpha", func(m *core.Material) { m.BaseColor[3] = 254 }},
		{"transparent alpha", func(m *core.Material) { m.BaseColor[3] = 0 }},
		{"transparency", func(m *core.Material) { m.Transparency = 0.5 }},
		{"tiny transparency", func(m *core.Material) { m.Transparency = 1e-6 }},
		{"negative transparency", func(m *core.Material) { m.Transparency = -1e-6 }},
		{"transparency NaN", func(m *core.Material) { m.Transparency = float32(math.NaN()) }},
		{"transparency infinity", func(m *core.Material) { m.Transparency = float32(math.Inf(1)) }},
		{"transmission", func(m *core.Material) { m.Transmission = 0.5 }},
		{"tiny transmission", func(m *core.Material) { m.Transmission = 1e-6 }},
		{"negative transmission", func(m *core.Material) { m.Transmission = -1e-6 }},
		{"transmission NaN", func(m *core.Material) { m.Transmission = float32(math.NaN()) }},
		{"transmission negative infinity", func(m *core.Material) { m.Transmission = float32(math.Inf(-1)) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			table := []core.Material{opaque, opaque}
			if !compiledAssetLODMaterialEligible(table, 1, false) {
				t.Fatal("opaque current table should qualify")
			}
			tc.edit(&table[1])
			before := table[1]
			if compiledAssetLODMaterialEligible(table, 1, false) {
				t.Fatal("current material mutation should reject")
			}
			// Compare float bits because NaN is intentionally present.
			if !c3h11SameMaterial(before, table[1]) {
				t.Fatal("eligibility repaired material")
			}
			table[1] = opaque
			if !compiledAssetLODMaterialEligible(table, 1, false) {
				t.Fatal("restored current material should qualify")
			}
		})
	}
	t.Run("used value only and animation", func(t *testing.T) {
		table := []core.Material{{Transparency: float32(math.NaN())}, opaque, {Transmission: 1}}
		before := append([]core.Material(nil), table...)
		table[1].Refraction, table[1].Density, table[1].Emission = 1, 2, 3
		before[1] = table[1]
		if !compiledAssetLODMaterialEligible(table, 1, false) {
			t.Fatal("opaque used value should ignore unused slots and non-opacity properties")
		}
		if compiledAssetLODMaterialEligible(table, 1, true) {
			t.Fatal("animated used value must reject even while currently opaque")
		}
		for i := range table {
			if !c3h11SameMaterial(table[i], before[i]) {
				t.Fatal("material table was mutated")
			}
		}
	})
	t.Run("signed negative zero remains opaque", func(t *testing.T) {
		mat := opaque
		mat.Transparency = float32(math.Copysign(0, -1))
		mat.Transmission = float32(math.Copysign(0, -1))
		table := []core.Material{opaque, mat}
		if !compiledAssetLODMaterialEligible(table, 1, false) {
			t.Fatal("finite signed zero should qualify")
		}
		if !c3h11SameMaterial(mat, table[1]) {
			t.Fatal("eligibility normalized signed zero")
		}
	})
	t.Run("value bounds", func(t *testing.T) {
		for _, tc := range []struct {
			table []core.Material
			value uint8
		}{{nil, 1}, {[]core.Material{}, 1}, {[]core.Material{opaque}, 1}, {[]core.Material{opaque, opaque}, 0}, {[]core.Material{opaque, opaque}, 255}} {
			if compiledAssetLODMaterialEligible(tc.table, tc.value, false) {
				t.Fatalf("value %d in length %d should reject", tc.value, len(tc.table))
			}
		}
		table := make([]core.Material, 256)
		table[255] = opaque
		if !compiledAssetLODMaterialEligible(table, 255, false) {
			t.Fatal("valid highest uint8 palette slot should qualify")
		}
	})
}
