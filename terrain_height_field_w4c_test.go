package gekko

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func w4cFieldFixture(t *testing.T, size int, coords []content.TerrainChunkCoordDef) (*content.TerrainChunkManifestDef, map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef, string) {
	t.Helper()
	m := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "plane", SourceHash: "source", ChunkSize: size, VoxelResolution: 1}
	tiles := map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef{}
	for _, c := range coords {
		d := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: m.TerrainID, SourceHash: m.SourceHash, Coord: c, WorldOrigin: [3]float32{float32(c.X * size), 0, float32(c.Z * size)}, SampleWidth: size, SampleHeight: size, SampleSpacing: 1, HeightOffset: 20, HeightScale: 100, HeightSamples: make([]uint16, size*size)}
		raw := make([]byte, 2*size*size)
		for z := 0; z < size; z++ {
			for x := 0; x < size; x++ {
				v := uint16(1000 + 100*(c.X*size+x) + 200*(c.Z*size+z))
				d.HeightSamples[z*size+x] = v
				binary.LittleEndian.PutUint16(raw[(z*size+x)*2:], v)
			}
		}
		h := sha256.Sum256(raw)
		m.Entries = append(m.Entries, content.TerrainChunkEntryDef{Coord: c, WorldOrigin: d.WorldOrigin, ChunkSize: size, VoxelResolution: 1, TerrainID: m.TerrainID, SourceHash: m.SourceHash, ChunkPath: filepath.Join("tiles", content.TerrainChunkKey(c)+".bin"), PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: hex.EncodeToString(h[:]), PayloadSizeBytes: len(raw)})
		tiles[c] = d
	}
	return m, tiles, filepath.Join(t.TempDir(), "height.gkterrainmanifest")
}
func w4cField(t *testing.T, m *content.TerrainChunkManifestDef, p string, cap int) *TerrainHeightField {
	t.Helper()
	f, e := NewTerrainHeightField(m, p, TerrainHeightFieldOptions{MaxResidentTiles: cap})
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func w4cPublish(t *testing.T, f *TerrainHeightField, tiles map[content.TerrainChunkCoordDef]*content.TerrainHeightTileDef) {
	t.Helper()
	for _, d := range tiles {
		if e := f.PublishTile(d); e != nil {
			t.Fatal(e)
		}
	}
}
func w4cStatus(t *testing.T, s TerrainHeightSample, want TerrainHeightSampleStatus, gen uint64) {
	t.Helper()
	if s.Status != want || s.Generation != gen || s.TerrainID != "plane" || s.SourceHash != "source" {
		t.Fatalf("sample %+v want status%v gen%d", s, want, gen)
	}
	if want != TerrainHeightPresent && (s.Height != 0 || s.Normal != (mgl32.Vec3{})) {
		t.Fatalf("unready geometry %+v", s)
	}
}
func w4cPlane(t *testing.T, s TerrainHeightSample, x, z float32) {
	t.Helper()
	want := float32(20 + (1000+100*(float64(x)-.5)+200*(float64(z)-.5))/65535*100)
	n := (mgl32.Vec3{-float32(10000.0 / 65535), 1, -float32(20000.0 / 65535)}).Normalize()
	if math.Abs(float64(s.Height-want)) > 3e-5 || s.Normal.Sub(n).Len() > 3e-5 {
		t.Fatalf("plane (%g,%g): %+v want height%g normal%v", x, z, s, want, n)
	}
}

func TestW4cHeightFieldSignedSeamsAndPhysicalPlane(t *testing.T) {
	coords := []content.TerrainChunkCoordDef{{X: -1, Z: -1}, {X: 0, Z: -1}, {X: -1, Z: 0}, {X: 0, Z: 0}}
	m, tiles, p := w4cFieldFixture(t, 2, coords)
	f := w4cField(t, m, p, 0)
	w4cStatus(t, f.SampleGroundXZ(0, 0), TerrainHeightNotResident, 0)
	w4cPublish(t, f, tiles)
	for _, v := range [][2]float32{{0, 0}, {-.25, -.25}, {.25, -.25}, {-.5, -.5}, {.5, .5}, {-1, -1}} {
		s := f.SampleGroundXZ(v[0], v[1])
		w4cStatus(t, s, TerrainHeightPresent, 4)
		w4cPlane(t, s, v[0], v[1])
	}
	for _, v := range [][2]float32{{2, 0}, {-2.1, 0}, {1.75, 0}, {-1.75, 0}} {
		w4cStatus(t, f.SampleGroundXZ(v[0], v[1]), TerrainHeightOutside, 4)
	}
	for _, v := range [][2]float32{{float32(math.NaN()), 0}, {0, float32(math.Inf(1))}, {math.MaxFloat32, 0}} {
		w4cStatus(t, f.SampleGroundXZ(v[0], v[1]), TerrainHeightInvalid, 4)
	}
	s := f.SampleGroundXZ(0, 0)
	for _, rangeY := range [][2]float32{{s.Height, s.Height}, {s.Height - 1, s.Height + 1}, {s.Height + 1, s.Height + 2}} {
		r := f.ProbeGroundXZ(0, 0, rangeY[0], rangeY[1])
		w4cStatus(t, r.Sample, TerrainHeightPresent, 4)
		want := rangeY[0] <= s.Height && s.Height <= rangeY[1]
		if r.Hit != want {
			t.Fatalf("probe %+v", r)
		}
	}
	for _, b := range [][2]float32{{2, 1}, {float32(math.NaN()), 2}, {0, float32(math.Inf(1))}} {
		r := f.ProbeGroundXZ(0, 0, b[0], b[1])
		w4cStatus(t, r.Sample, TerrainHeightInvalid, 4)
		if r.Hit {
			t.Fatal("invalid probe hit")
		}
	}
}

func TestW4cHeightFieldExactCenterDependenciesAndPrecedence(t *testing.T) {
	coords := []content.TerrainChunkCoordDef{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 0, Z: 1}, {X: 1, Z: 1}}
	m, tiles, p := w4cFieldFixture(t, 1, coords)
	f := w4cField(t, m, p, 0)
	for _, c := range coords[:3] {
		if e := f.PublishTile(tiles[c]); e != nil {
			t.Fatal(e)
		}
	}
	s := f.SampleGroundXZ(.5, .5)
	w4cStatus(t, s, TerrainHeightPresent, 3)
	w4cPlane(t, s, .5, .5)
	w4cStatus(t, f.SampleGroundXZ(.75, .75), TerrainHeightNotResident, 3)
	// Required normal dependencies remain required even when their height coefficients vanish.
	if !f.RemoveTile(coords[1]) {
		t.Fatal("remove required tile")
	}
	w4cStatus(t, f.SampleGroundXZ(.5, .5), TerrainHeightNotResident, 4)
	d := *tiles[coords[0]]
	d.SurfaceMask = []byte{0}
	raw := []byte{0, 0, 0}
	binary.LittleEndian.PutUint16(raw, d.HeightSamples[0])
	h := sha256.Sum256(raw)
	m.Entries[0].PayloadHash = hex.EncodeToString(h[:])
	m.Entries[0].PayloadSizeBytes = 3
	masked := w4cField(t, m, p, 0)
	if e := masked.PublishTile(&d); e != nil {
		t.Fatal(e)
	}
	w4cStatus(t, masked.SampleGroundXZ(.5, .5), TerrainHeightNotResident, 1)
	for _, c := range coords[1:3] {
		if e := masked.PublishTile(tiles[c]); e != nil {
			t.Fatal(e)
		}
	}
	w4cStatus(t, masked.SampleGroundXZ(.5, .5), TerrainHeightNoSurface, 3)
	// Undeclared normal dependency outranks a loaded masked corner.
	m.Entries = m.Entries[:1]
	outside := w4cField(t, m, p, 0)
	if e := outside.PublishTile(&d); e != nil {
		t.Fatal(e)
	}
	w4cStatus(t, outside.SampleGroundXZ(.5, .5), TerrainHeightOutside, 1)
}

func TestW4cHeightFieldPublicationOwnershipCapacityAndSnapshots(t *testing.T) {
	coords := []content.TerrainChunkCoordDef{{X: 0, Z: 0}, {X: 1, Z: 0}}
	m, tiles, p := w4cFieldFixture(t, 2, coords)
	f := w4cField(t, m, p, 1)
	d := tiles[coords[0]]
	if e := f.PublishTile(d); e != nil {
		t.Fatal(e)
	}
	snap := f.Snapshot()
	old := snap.SampleGroundXZ(.5, .5)
	w4cStatus(t, old, TerrainHeightPresent, 1)
	// Constructor and publication consume private copies of manifest references and tile payload.
	m.TerrainID = "mutated"
	m.Entries[0].PayloadHash = "bad"
	d.HeightSamples[0] = 65535
	w4cPlane(t, f.SampleGroundXZ(.5, .5), .5, .5)
	if e := f.PublishTile(tiles[coords[1]]); e == nil {
		t.Fatal("capacity exceeded")
	}
	if f.ResidentTileCount() != 1 {
		t.Fatal("capacity failure changed residents")
	}
	w4cStatus(t, f.SampleGroundXZ(.5, .5), TerrainHeightPresent, 1)
	replacement := *d
	replacement.HeightSamples = append([]uint16(nil), d.HeightSamples...)
	replacement.HeightSamples[0] = 1000
	replacement.HeightOffset = 30
	if e := f.PublishTile(&replacement); e != nil {
		t.Fatal(e)
	}
	s := f.SampleGroundXZ(.5, .5)
	w4cStatus(t, s, TerrainHeightPresent, 2)
	if math.Abs(float64(s.Height-old.Height-10)) > 3e-5 {
		t.Fatal("offset replacement")
	}
	w4cStatus(t, snap.SampleGroundXZ(.5, .5), TerrainHeightPresent, 1)
	w4cPlane(t, snap.SampleGroundXZ(.5, .5), .5, .5)
	for name, mutate := range map[string]func(*content.TerrainHeightTileDef){"source": func(d *content.TerrainHeightTileDef) { d.SourceHash = "bad" }, "terrain": func(d *content.TerrainHeightTileDef) { d.TerrainID = "bad" }, "coord": func(d *content.TerrainHeightTileDef) { d.Coord.Z++ }, "origin": func(d *content.TerrainHeightTileDef) { d.WorldOrigin[0]++ }, "spacing": func(d *content.TerrainHeightTileDef) { d.SampleSpacing = 2 }, "dims": func(d *content.TerrainHeightTileDef) { d.SampleHeight = 1 }, "payload": func(d *content.TerrainHeightTileDef) { d.HeightSamples = []uint16{1, 2, 3, 4} }, "metadata": func(d *content.TerrainHeightTileDef) { d.HeightScale = float32(math.NaN()) }} {
		t.Run(name, func(t *testing.T) {
			bad := replacement
			mutate(&bad)
			if e := f.PublishTile(&bad); e == nil {
				t.Fatal("invalid publication accepted")
			}
			got := f.SampleGroundXZ(.5, .5)
			w4cStatus(t, got, TerrainHeightPresent, 2)
			if got.Height != s.Height {
				t.Fatal("failure changed height")
			}
		})
	}
	if f.RemoveTile(coords[1]) {
		t.Fatal("absent removal true")
	}
	if !f.RemoveTile(coords[0]) || f.ResidentTileCount() != 0 {
		t.Fatal("removal")
	}
	w4cStatus(t, f.SampleGroundXZ(.5, .5), TerrainHeightNotResident, 3)
	w4cStatus(t, snap.SampleGroundXZ(.5, .5), TerrainHeightPresent, 1)
	if e := f.PublishTile(tiles[coords[1]]); e != nil {
		t.Fatal("freed capacity", e)
	}
	w4cStatus(t, f.SampleGroundXZ(2.5, .5), TerrainHeightPresent, 4)
}

func TestW4cHeightFieldActualRelativeLoadAndPublicValidation(t *testing.T) {
	m, tiles, p := w4cFieldFixture(t, 2, []content.TerrainChunkCoordDef{{X: 0, Z: 0}})
	d := tiles[content.TerrainChunkCoordDef{}]
	if e := content.ValidateTerrainHeightTileManifest(m); e != nil {
		t.Fatal(e)
	}
	if e := content.ValidateTerrainHeightTileReference(m.Entries[0], d); e != nil {
		t.Fatal(e)
	}
	if _, e := content.SaveTerrainHeightTile(content.ResolveTerrainChunkPath(m.Entries[0], p), d); e != nil {
		t.Fatal(e)
	}
	if e := content.SaveTerrainChunkManifest(p, m); e != nil {
		t.Fatal(e)
	}
	if content.ValidateTerrainHeightTileManifest(nil) == nil || content.ValidateTerrainHeightTileManifest(&content.TerrainChunkManifestDef{SchemaVersion: 2}) == nil {
		t.Fatal("public manifest validator accepted nil/legacy")
	}
	if content.ValidateTerrainHeightTileReference(m.Entries[0], nil) == nil {
		t.Fatal("public reference accepted nil")
	}
	badRef := m.Entries[0]
	badRef.SourceHash = "other"
	if content.ValidateTerrainHeightTileReference(badRef, d) == nil {
		t.Fatal("public reference accepted mismatch")
	}
	loaded, e := content.LoadTerrainChunkManifest(p)
	if e != nil {
		t.Fatal(e)
	}
	f := w4cField(t, loaded, p, 0)
	if e = f.LoadTile(content.TerrainChunkCoordDef{}); e != nil {
		t.Fatal(e)
	}
	w4cStatus(t, f.SampleGroundXZ(.5, .5), TerrainHeightPresent, 1)
	w4cPlane(t, f.SampleGroundXZ(.5, .5), .5, .5)
	if e = f.LoadTile(content.TerrainChunkCoordDef{}); e != nil {
		t.Fatal(e)
	}
	w4cStatus(t, f.SampleGroundXZ(.5, .5), TerrainHeightPresent, 2)
	filePath := content.ResolveTerrainChunkPath(m.Entries[0], p)
	file, e := os.ReadFile(filePath)
	if e != nil {
		t.Fatal(e)
	}
	file[len(file)-1] ^= 1
	if e = os.WriteFile(filePath, file, 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.LoadTile(content.TerrainChunkCoordDef{}); e == nil {
		t.Fatal("corrupt load accepted")
	}
	unchanged := f.SampleGroundXZ(.5, .5)
	w4cStatus(t, unchanged, TerrainHeightPresent, 2)
	w4cPlane(t, unchanged, .5, .5)
	if e = f.LoadTile(content.TerrainChunkCoordDef{X: 9}); e == nil {
		t.Fatal("unknown load")
	}
	w4cStatus(t, f.SampleGroundXZ(.5, .5), TerrainHeightPresent, 2)
	if _, e := NewRuntimeContentLoader().LoadTerrainChunkManifest(p); e == nil {
		t.Fatal("live v3 guard removed")
	}
	for _, v := range []struct {
		m   *content.TerrainChunkManifestDef
		p   string
		cap int
	}{{nil, p, 0}, {m, " ", 0}, {m, p, -1}, {&content.TerrainChunkManifestDef{SchemaVersion: 2}, p, 0}} {
		if _, e := NewTerrainHeightField(v.m, v.p, TerrainHeightFieldOptions{MaxResidentTiles: v.cap}); e == nil {
			t.Fatal("invalid owner accepted")
		}
	}
	invalidManifest := *m
	invalidManifest.Entries = append([]content.TerrainChunkEntryDef(nil), m.Entries...)
	invalidManifest.Entries[0].PayloadHash = "bad"
	if _, e := NewTerrainHeightField(&invalidManifest, p, TerrainHeightFieldOptions{}); e == nil {
		t.Fatal("owner accepted invalid reference")
	}
	var nilOwner *TerrainHeightField
	var nilSnap *TerrainHeightSnapshot
	if nilOwner.SampleGroundXZ(0, 0).Status != TerrainHeightInvalid || nilSnap.SampleGroundXZ(0, 0).Status != TerrainHeightInvalid || nilOwner.ProbeGroundXZ(0, 0, 0, 1).Hit || nilSnap.ProbeGroundXZ(0, 0, 0, 1).Hit {
		t.Fatal("nil queries")
	}
}

func TestW4cHeightFieldSaddleGradientAndDefaultBudget(t *testing.T) {
	m, tiles, p := w4cFieldFixture(t, 2, []content.TerrainChunkCoordDef{{}})
	d := tiles[content.TerrainChunkCoordDef{}]
	d.HeightSamples = []uint16{1000, 1100, 1200, 1600}
	raw := make([]byte, 8)
	for i, v := range d.HeightSamples {
		binary.LittleEndian.PutUint16(raw[i*2:], v)
	}
	h := sha256.Sum256(raw)
	m.Entries[0].PayloadHash = hex.EncodeToString(h[:])
	f := w4cField(t, m, p, 0)
	w4cPublish(t, f, tiles)
	s := f.SampleGroundXZ(1, 1)
	w4cStatus(t, s, TerrainHeightPresent, 1)
	want := float32(20 + 1225.0/65535*100)
	n := (mgl32.Vec3{-float32(25000.0 / 65535), 1, -float32(35000.0 / 65535)}).Normalize()
	if math.Abs(float64(s.Height-want)) > 3e-5 || s.Normal.Sub(n).Len() > 3e-5 {
		t.Fatalf("saddle %+v want%g %v", s, want, n)
	}
	var coords []content.TerrainChunkCoordDef
	for i := 0; i < 257; i++ {
		coords = append(coords, content.TerrainChunkCoordDef{X: i})
	}
	m, tiles, p = w4cFieldFixture(t, 1, coords)
	f = w4cField(t, m, p, 0)
	for _, c := range coords[:256] {
		if e := f.PublishTile(tiles[c]); e != nil {
			t.Fatal(e)
		}
	}
	if f.ResidentTileCount() != 256 {
		t.Fatal("default budget")
	}
	if e := f.PublishTile(tiles[coords[256]]); e == nil {
		t.Fatal("default exceeded256")
	}
	if e := f.PublishTile(tiles[coords[0]]); e != nil {
		t.Fatal("replacement at default capacity", e)
	}
}

func TestW4cHeightFieldConcurrentPublicationAndSnapshotReads(t *testing.T) {
	m, tiles, p := w4cFieldFixture(t, 2, []content.TerrainChunkCoordDef{{}})
	f := w4cField(t, m, p, 0)
	d := tiles[content.TerrainChunkCoordDef{}]
	if e := f.PublishTile(d); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			copy := *d
			if i%2 == 0 {
				copy.HeightOffset = 30
			}
			if e := f.PublishTile(&copy); e != nil {
				t.Error(e)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			current := f.SampleGroundXZ(.5, .5)
			offsetCurrent := float32(20)
			if current.Generation%2 == 0 {
				offsetCurrent = 30
			}
			if current.Status != TerrainHeightPresent || math.Abs(float64(current.Height-(offsetCurrent+float32(1000.0/65535*100)))) > 3e-5 {
				t.Errorf("owner non-atomic sample %+v", current)
				return
			}
			snap := f.Snapshot()
			s := snap.SampleGroundXZ(.5, .5)
			if s.Status != TerrainHeightPresent {
				t.Errorf("concurrent status %+v", s)
				return
			}
			offset := float32(20)
			if s.Generation%2 == 0 {
				offset = 30
			}
			want := offset + float32(1000.0/65535*100)
			if math.Abs(float64(s.Height-want)) > 3e-5 {
				t.Errorf("non-atomic generation and height %+v", s)
				return
			}
			r := snap.ProbeGroundXZ(.5, .5, s.Height, s.Height)
			if !r.Hit || r.Sample.Generation != s.Generation || r.Sample.Height != s.Height {
				t.Errorf("snapshot changed %+v -> %+v", s, r)
				return
			}
		}
	}()
	wg.Wait()
}

func TestW4cHeightFieldNormalOnlyMasksAndPerCornerPhysicalHeights(t *testing.T) {
	coords := []content.TerrainChunkCoordDef{{X: 0, Z: 0}, {X: 1, Z: 0}, {X: 0, Z: 1}, {X: 1, Z: 1}}
	for _, maskedIndex := range []int{1, 3} {
		m, tiles, p := w4cFieldFixture(t, 1, coords)
		d := tiles[coords[maskedIndex]]
		d.SurfaceMask = []byte{0}
		raw := []byte{0, 0, 0}
		binary.LittleEndian.PutUint16(raw, d.HeightSamples[0])
		h := sha256.Sum256(raw)
		m.Entries[maskedIndex].PayloadHash = hex.EncodeToString(h[:])
		m.Entries[maskedIndex].PayloadSizeBytes = 3
		f := w4cField(t, m, p, 0)
		w4cPublish(t, f, tiles)
		want := TerrainHeightNoSurface
		if maskedIndex == 3 {
			want = TerrainHeightPresent
		}
		w4cStatus(t, f.SampleGroundXZ(.5, .5), want, 4)
	}
	m, tiles, p := w4cFieldFixture(t, 1, coords)
	offsets := []float32{10, 20, 30, 40}
	scales := []float32{50, 100, 150, 200}
	physical := [4]float64{}
	for i, c := range coords {
		d := tiles[c]
		d.HeightOffset = offsets[i]
		d.HeightScale = scales[i]
		physical[i] = float64(offsets[i]) + float64(d.HeightSamples[0])/65535*float64(scales[i])
	}
	f := w4cField(t, m, p, 0)
	w4cPublish(t, f, tiles)
	s := f.SampleGroundXZ(.75, .75)
	w4cStatus(t, s, TerrainHeightPresent, 4)
	tx, tz := .25, .25
	h00, h10, h01, h11 := physical[0], physical[1], physical[2], physical[3]
	want := (h00*(1-tx)+h10*tx)*(1-tz) + (h01*(1-tx)+h11*tx)*tz
	dx := (h10-h00)*(1-tz) + (h11-h01)*tz
	dz := (h01-h00)*(1-tx) + (h11-h10)*tx
	n := (mgl32.Vec3{-float32(dx), 1, -float32(dz)}).Normalize()
	if math.Abs(float64(s.Height)-want) > 3e-5 || s.Normal.Sub(n).Len() > 3e-5 {
		t.Fatalf("per-corner physical interpolation %+v want%g %v", s, want, n)
	}
}

func BenchmarkW4c2ResidentGroundQuery(b *testing.B) {
	// Use the same public fixture contract without a testing.T-dependent builder.
	m := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "bench", SourceHash: "source", ChunkSize: 2, VoxelResolution: 1}
	var tiles []*content.TerrainHeightTileDef
	for z := -1; z <= 0; z++ {
		for x := -1; x <= 0; x++ {
			c := content.TerrainChunkCoordDef{X: x, Z: z}
			d := &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: "bench", SourceHash: "source", Coord: c, WorldOrigin: [3]float32{float32(x * 2), 0, float32(z * 2)}, SampleWidth: 2, SampleHeight: 2, SampleSpacing: 1, HeightScale: 64, HeightSamples: []uint16{1000, 1000, 1000, 1000}}
			raw := make([]byte, 8)
			for i := 0; i < 4; i++ {
				binary.LittleEndian.PutUint16(raw[i*2:], 1000)
			}
			h := sha256.Sum256(raw)
			m.Entries = append(m.Entries, content.TerrainChunkEntryDef{Coord: c, WorldOrigin: d.WorldOrigin, ChunkSize: 2, VoxelResolution: 1, TerrainID: "bench", SourceHash: "source", ChunkPath: "tile.bin", PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: hex.EncodeToString(h[:]), PayloadSizeBytes: 8})
			tiles = append(tiles, d)
		}
	}
	f, e := NewTerrainHeightField(m, filepath.Join(b.TempDir(), "manifest.json"), TerrainHeightFieldOptions{})
	if e != nil {
		b.Fatal(e)
	}
	for _, d := range tiles {
		if e = f.PublishTile(d); e != nil {
			b.Fatal(e)
		}
	}
	for _, v := range []struct {
		name string
		x, z float32
	}{{"interior", .75, .75}, {"seam", 0, 0}} {
		b.Run(v.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if f.SampleGroundXZ(v.x, v.z).Status != TerrainHeightPresent {
					b.Fatal("not resident")
				}
			}
		})
	}
}

func TestW4cHeightFieldRejectsLostHalfCellPrecision(t *testing.T) {
	if strconv.IntSize < 64 {
		t.Skip("fixture coordinates require64-bit ints")
	}
	magnitude := uint64(1) << 53
	for _, v := range []struct {
		name       string
		base       int
		spacing, x float32
	}{{"positive-ratio", int(magnitude), 1, float32(uint64(1) << 53)}, {"negative-ratio", -int(magnitude >> 1), 1, -float32(uint64(1) << 52)}, {"tiny-spacing-ratio", int(magnitude), float32(math.Ldexp(1, -53)), 1}} {
		t.Run(v.name, func(t *testing.T) {
			m := &content.TerrainChunkManifestDef{SchemaVersion: 3, TerrainID: "plane", SourceHash: "source", ChunkSize: 1, VoxelResolution: v.spacing}
			var tiles []*content.TerrainHeightTileDef
			for z := 0; z < 2; z++ {
				for i := 0; i < 2; i++ {
					c := content.TerrainChunkCoordDef{X: v.base + i, Z: z}
					origin := [3]float32{float32(float64(c.X) * float64(v.spacing)), 0, float32(z) * v.spacing}
					code := uint16(100 + i*100 + z*200)
					raw := make([]byte, 2)
					binary.LittleEndian.PutUint16(raw, code)
					h := sha256.Sum256(raw)
					m.Entries = append(m.Entries, content.TerrainChunkEntryDef{Coord: c, WorldOrigin: origin, ChunkSize: 1, VoxelResolution: v.spacing, TerrainID: "plane", SourceHash: "source", ChunkPath: "tile.bin", PayloadKind: content.TerrainHeightTilePayloadKind, PayloadHash: hex.EncodeToString(h[:]), PayloadSizeBytes: 2})
					tiles = append(tiles, &content.TerrainHeightTileDef{SchemaVersion: 1, TerrainID: "plane", SourceHash: "source", Coord: c, WorldOrigin: origin, SampleWidth: 1, SampleHeight: 1, SampleSpacing: v.spacing, HeightScale: 65535, HeightSamples: []uint16{code}})
				}
			}
			f := w4cField(t, m, filepath.Join(t.TempDir(), "manifest.json"), 0)
			for _, d := range tiles {
				if e := f.PublishTile(d); e != nil {
					t.Fatal(e)
				}
			}
			z := v.spacing * .5
			w4cStatus(t, f.SampleGroundXZ(v.x, z), TerrainHeightInvalid, 4)
			r := f.ProbeGroundXZ(v.x, z, -1000, 1000)
			w4cStatus(t, r.Sample, TerrainHeightInvalid, 4)
			if r.Hit {
				t.Fatal("precision-invalid probe hit")
			}
			w4cStatus(t, f.Snapshot().SampleGroundXZ(v.x, z), TerrainHeightInvalid, 4)
		})
	}
}
