package gekko

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3h6Path(t *testing.T, codec *voxelcodec.Codec) (string, voxelcodec.Info) {
	t.Helper()
	source := c3h5Source(t, c3h1Shape(c3h5Columns(1, 5)...))
	lod := c3h5Typed(t, c3h5LOD(t, source, [3]int64{0, 0, 0}, [3]int64{2, 0, 0}))
	path := filepath.Join(t.TempDir(), "lod.gklod")
	info, err := content.SaveCompiledAssetLOD(path, lod, codec)
	if err != nil {
		t.Fatal(err)
	}
	return path, info
}

func TestC3h6SharedOwnerScopesSingleflightAndProfileSnapshot(t *testing.T) {
	dict := c3cCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 61, Bytes: bytes.Repeat([]byte("compiled_asset_lod source_content_id source_lattice occupancy-or-zero-anchored-2x-v1"), 8)}})
	wrong := c3cCodec(t, voxelcodec.Options{})
	path, wantInfo := c3h6Path(t, dict)
	options := RuntimeContentLoaderOptions{CompiledAssetCodec: dict, ImportedWorldCodec: wrong}
	owner := NewRuntimeContentLoader(options)
	options.CompiledAssetCodec = wrong
	a, b := owner.NewScope(), owner.NewScope()
	defer a.Close()
	defer b.Close()
	type result struct {
		def  *content.CompiledAssetLODDef
		info voxelcodec.Info
		err  error
	}
	results := make(chan result, 3)
	ready := make(chan struct{}, 3)
	start := make(chan struct{})
	for _, loader := range []*RuntimeContentLoader{owner, a.Loader(), b.Loader()} {
		loader := loader
		go func() {
			ready <- struct{}{}
			<-start
			d, i, e := loader.LoadCompiledAssetLOD(path)
			results <- result{d, i, e}
		}()
	}
	for i := 0; i < 3; i++ {
		<-ready
	}
	close(start)
	var shared *content.CompiledAssetLODDef
	for i := 0; i < 3; i++ {
		r := <-results
		if r.err != nil || r.def == nil || r.info != wantInfo {
			t.Fatalf("load %+v", r)
		}
		if shared == nil {
			shared = r.def
		} else if shared != r.def {
			t.Fatal("cache failed shared owner definition")
		}
	}
	stats := owner.Stats()
	if stats.Entries != 1 || stats.Misses-stats.LoadWaits != 1 || stats.Hits+stats.Misses != 3 || stats.Bytes <= 0 || stats.PinnedBytes != stats.Bytes {
		t.Fatalf("LOD bypasses common singleflight/ownership: %+v", stats)
	}
	_, info, err := owner.LoadCompiledAssetLOD(path)
	if err != nil {
		t.Fatal(err)
	}
	info.ContentID = "caller-mutated"
	_, again, err := a.Loader().LoadCompiledAssetLOD(path)
	if err != nil || again != wantInfo {
		t.Fatal("returned Info mutates cache", err)
	}
	if _, _, err := dict.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("loader closed borrowed codec", err)
	}
	dict.Close()
	if hit, hi, err := b.Loader().LoadCompiledAssetLOD(path); err != nil || hit != shared || hi != wantInfo {
		t.Fatal("warm decoded hit consulted closed codec", err)
	}
	missing := filepath.Join(t.TempDir(), "miss.gklod")
	if err := os.WriteFile(missing, c3cRead(t, path), 0600); err != nil {
		t.Fatal(err)
	}
	if out, info, err := owner.LoadCompiledAssetLOD(missing); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed borrowed codec miss admitted data")
	}
	if owner.Stats().Entries != 1 {
		t.Fatal("closed miss changed admitted entries")
	}
	a.Close()
	if out, info, err := a.Loader().LoadCompiledAssetLOD(path); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed scope accepted warm hit")
	}
}

func TestC3h6SelectiveReleaseClearAndBorrowerLifetime(t *testing.T) {
	first, fi := c3h6Path(t, nil)
	second, _ := c3h6Path(t, nil)
	_, shapePath, _, _ := c3d1Paths(t, nil)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	a, b := owner.NewScope(), owner.NewScope()
	defer a.Close()
	defer b.Close()
	lod, _, err := a.Loader().LoadCompiledAssetLOD(first)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := a.Loader().LoadCompiledAssetLOD(second)
	if err != nil {
		t.Fatal(err)
	}
	shape, _, err := a.Loader().LoadCompiledAssetShape(shapePath)
	if err != nil {
		t.Fatal(err)
	}
	if shared, info, err := b.Loader().LoadCompiledAssetLOD(first); err != nil || shared != lod || info != fi {
		t.Fatal("second scope did not share LOD", err)
	}
	before := owner.Stats()
	if before.Entries != 3 || before.Bytes <= 0 || before.PinnedBytes != before.Bytes {
		t.Fatal("wrong initial pins", before)
	}
	a.Loader().releaseScopedValue(lod)
	if s := owner.Stats(); s.Entries != 3 || s.Bytes != before.Bytes || s.PinnedBytes != before.PinnedBytes {
		t.Fatal("release revoked another consumer's pin", s)
	}
	b.Loader().releaseScopedValue(lod)
	remaining := owner.Stats()
	if remaining.Entries != 2 || remaining.Bytes >= before.Bytes || remaining.Bytes <= 0 || remaining.PinnedBytes != remaining.Bytes {
		t.Fatal("final selective release did not evict only LOD", remaining)
	}
	if hit, _, err := a.Loader().LoadCompiledAssetLOD(second); err != nil || hit != other {
		t.Fatal("selective release invalidated other LOD", err)
	}
	if hit, _, err := a.Loader().LoadCompiledAssetShape(shapePath); err != nil || hit != shape {
		t.Fatal("selective release invalidated shape kind", err)
	}
	owner.Clear()
	if s := owner.Stats(); s.Entries != 2 || s.Bytes != remaining.Bytes || s.PinnedBytes != remaining.Bytes {
		t.Fatal("Clear revoked live pins", s)
	}
	a.Close()
	b.Close()
	if s := owner.Stats(); s.Entries != 0 || s.Bytes != 0 || s.PinnedBytes != 0 {
		t.Fatal("closed no-warm-retention scopes leave decoded storage", s)
	}
	if lod.Value != 7 || len(lod.Bricks) == 0 || other.SourceContentID == "" || len(shape.Bricks) == 0 {
		t.Fatal("eviction invalidates borrowed storage")
	}
}

func TestC3h6PositiveNamespaceSeparationAtSamePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typed.frame")
	source := c3h1Shape(c3h5Columns(1, 5)...)
	si, err := content.SaveCompiledAssetShape(path, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := NewRuntimeContentLoader()
	old, oi, err := owner.LoadCompiledAssetShape(path)
	if err != nil || oi != si {
		t.Fatal(err)
	}
	verified := c3h5Source(t, source)
	lod := c3h5Typed(t, c3h5LOD(t, verified, [3]int64{0, 0, 0}, [3]int64{2, 0, 0}))
	li, err := content.SaveCompiledAssetLOD(path, lod, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, info, err := owner.LoadCompiledAssetLOD(path)
	if err != nil || info != li || !reflect.DeepEqual(out, lod) {
		t.Fatal("LOD kind collided with warm shape key", err)
	}
	hit, hi, err := owner.LoadCompiledAssetShape(path)
	if err != nil || hit != old || hi != si {
		t.Fatal("LOD replaced immutable warm shape contract", err)
	}
	if s := owner.Stats(); s.Entries != 2 || s.Misses-s.LoadWaits != 2 {
		t.Fatal("same-path content kinds not independent", s)
	}
}

func TestC3h6CompactDefinitionAndWrapperCharge(t *testing.T) {
	var charges [2]int64
	for index, count := range []int{1, 512} {
		var cells [][3]int64
		for bit := 0; bit < count; bit++ {
			cells = append(cells, [3]int64{int64(bit % 8), int64(bit / 8 % 8), int64(bit / 64)})
		}
		geometry := c3h1Shape(cells...)
		// Metadata describes the source only; no source frame is read or expanded.
		min, max := c3h5Bounds(t, geometry)
		var smin, smax [3]int64
		for axis := 0; axis < 3; axis++ {
			smin[axis] = min[axis] * 2
			smax[axis] = max[axis] * 2
		}
		lod := &content.CompiledAssetLODDef{SchemaVersion: 1, SourceContentID: bytesToC3h6Hash(), SourceLattice: geometry.Lattice, Factor: 2, ReductionVersion: content.CompiledAssetLOD2xReductionVersion, Value: 7, SourceVoxelCount: int64(count * 2), SourceMin: smin, SourceMax: smax, CoarseMin: min, CoarseMax: max, Bricks: geometry.Bricks}
		path := filepath.Join(t.TempDir(), "lod.gklod")
		if _, err := content.SaveCompiledAssetLOD(path, lod, nil); err != nil {
			t.Fatal(err)
		}
		owner := NewRuntimeContentLoader()
		out, info, err := owner.LoadCompiledAssetLOD(path)
		if err != nil {
			t.Fatal(err)
		}
		expected := runtimeContentGraphCharge(&struct {
			Definition *content.CompiledAssetLODDef
			Info       voxelcodec.Info
		}{out, info})
		charges[index] = owner.Stats().Bytes
		if charges[index] != expected {
			t.Fatalf("LOD wrapper/metadata/brick charge differs: got%d want%d", charges[index], expected)
		}
	}
	if charges[1] < charges[0]+511 {
		t.Fatal("actual coarse values omitted from charge", charges)
	}
	if charges[1] >= 512*int64(unsafe.Sizeof(content.VoxelObjectVoxelDef{})) {
		t.Fatal("LOD cache charged expanded cell/source records", charges)
	}
}
func bytesToC3h6Hash() string {
	return "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}

func TestC3h6FailureAdmissionAndNilDefaultLoader(t *testing.T) {
	path, wantInfo := c3h6Path(t, nil)
	var nilLoader *RuntimeContentLoader
	if out, info, err := nilLoader.LoadCompiledAssetLOD(path); err != nil || out == nil || info != wantInfo {
		t.Fatal("nil loader didn't use default typed IO", err)
	}
	frame := c3cRead(t, path)
	shape := filepath.Join(t.TempDir(), "shape.gkshape")
	if _, err := content.SaveCompiledAssetShape(shape, c3h1Shape([3]int64{}), nil); err != nil {
		t.Fatal(err)
	}
	loader := NewRuntimeContentLoader()
	for name, data := range map[string][]byte{"truncated": frame[:len(frame)-1], "corrupt": func() []byte { b := bytes.Clone(frame); b[len(b)-1] ^= 1; return b }(), "json": []byte(`{"schema_version":1}`)} {
		bad := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(bad, data, 0600); err != nil {
			t.Fatal(err)
		}
		if out, info, err := loader.LoadCompiledAssetLOD(bad); err == nil || out != nil || info != (voxelcodec.Info{}) {
			t.Fatal("invalid frame admitted", name)
		}
	}
	if out, info, err := loader.LoadCompiledAssetLOD(shape); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("shape accepted as LOD")
	}
	if s := loader.Stats(); s.Entries != 0 || s.Bytes != 0 {
		t.Fatal("failures admitted cache storage", s)
	}
	limited := c3cCodec(t, voxelcodec.Options{Limits: voxelcodec.Limits{MaxVoxels: 1}})
	bounded := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: limited})
	if out, info, err := bounded.LoadCompiledAssetLOD(path); err == nil || out != nil || info != (voxelcodec.Info{}) {
		t.Fatal("LOD owner ignored borrowed coarse voxel limit")
	}
	if bounded.Stats().Entries != 0 {
		t.Fatal("profile failure admitted entry")
	}
	if _, _, err := limited.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("failure closed borrowed profile", err)
	}
}
