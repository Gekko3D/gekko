package gekko

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/content/voxelcodec"
)

func c3d1Paths(t *testing.T, codec *voxelcodec.Codec) (string, string, voxelcodec.Info, voxelcodec.Info) {
	t.Helper()
	input, output, _ := c3cFixture(t)
	if _, err := CompileAuthoredAsset(input, output, codec); err != nil {
		t.Fatal(err)
	}
	header, headerInfo, err := content.LoadCompiledAssetHeader(output, codec)
	if err != nil {
		t.Fatal(err)
	}
	shapePath := filepath.Join(filepath.Dir(output), header.Shapes[0].Path)
	_, shapeInfo, err := content.LoadCompiledAssetShape(shapePath, codec)
	if err != nil {
		t.Fatal(err)
	}
	return output, shapePath, headerInfo, shapeInfo
}

func TestC3d1FixedProfileSharedOwnerScopesAndSingleflight(t *testing.T) {
	dictionary := c3cCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 47, Bytes: bytes.Repeat([]byte("compiled_asset_header compiled_asset_shape geometry lattice metadata"), 8)}})
	wrong := c3cCodec(t, voxelcodec.Options{})
	headerPath, shapePath, headerInfo, shapeInfo := c3d1Paths(t, dictionary)
	options := RuntimeContentLoaderOptions{CompiledAssetCodec: dictionary, ImportedWorldCodec: wrong}
	owner := NewRuntimeContentLoader(options)
	options.CompiledAssetCodec = wrong
	a, b := owner.NewScope(), owner.NewScope()
	defer a.Close()
	defer b.Close()
	type result struct {
		header *content.CompiledAssetHeaderDef
		shape  *content.CompiledAssetShapeDef
		info   voxelcodec.Info
		err    error
		kind   string
	}
	results := make(chan result, 6)
	ready := make(chan struct{}, 6)
	start := make(chan struct{})
	for _, loader := range []*RuntimeContentLoader{owner, a.Loader(), b.Loader()} {
		for _, kind := range []string{"header", "shape"} {
			loader, kind := loader, kind
			go func() {
				ready <- struct{}{}
				<-start
				r := result{kind: kind}
				if kind == "header" {
					r.header, r.info, r.err = loader.LoadCompiledAssetHeader(headerPath)
				} else {
					r.shape, r.info, r.err = loader.LoadCompiledAssetShape(shapePath)
				}
				results <- r
			}()
		}
	}
	for i := 0; i < 6; i++ {
		<-ready
	}
	close(start)
	completed := make([]result, 6)
	for i := range completed {
		completed[i] = <-results
	}
	var header *content.CompiledAssetHeaderDef
	var shape *content.CompiledAssetShapeDef
	for _, r := range completed {
		if r.err != nil {
			t.Fatal("owner/scoped load failed", r.err)
		}
		if r.kind == "header" {
			if r.info != headerInfo {
				t.Fatal("header info differs from direct bounded IO")
			}
			if header == nil {
				header = r.header
			} else if header != r.header {
				t.Fatal("header definitions not shared within owner")
			}
		} else {
			if r.info != shapeInfo {
				t.Fatal("shape info differs from direct bounded IO")
			}
			if shape == nil {
				shape = r.shape
			} else if shape != r.shape {
				t.Fatal("shape definitions not shared within owner")
			}
		}
	}
	stats := owner.Stats()
	if stats.Entries != 2 || stats.Misses-stats.LoadWaits != 2 || stats.Hits+stats.Misses != 6 || stats.Bytes <= 0 || stats.PinnedBytes != stats.Bytes {
		t.Fatalf("typed loads bypass shared ownership/singleflight: %+v", stats)
	}
	infoCopy := headerInfo
	infoCopy.ContentID = "changed"
	_, cachedInfo, err := owner.LoadCompiledAssetHeader(headerPath)
	if err != nil || cachedInfo != headerInfo || cachedInfo == infoCopy {
		t.Fatal("returned Info aliases cache state", err)
	}
	a.Close()
	if _, info, err := a.Loader().LoadCompiledAssetShape(shapePath); err == nil || info != (voxelcodec.Info{}) {
		t.Fatal("closed scope accepted cached compiled shape")
	}
	if _, _, err := dictionary.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("loader closed borrowed codec", err)
	}
}

func TestC3d1PinnedPressureClearAndBorrowerLifetime(t *testing.T) {
	headerPath, shapePath, _, _ := c3d1Paths(t, nil)
	codec := c3cCodec(t, voxelcodec.Options{})
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: 1, CompiledAssetCodec: codec})
	a, b := owner.NewScope(), owner.NewScope()
	defer a.Close()
	defer b.Close()
	header, _, err := a.Loader().LoadCompiledAssetHeader(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	shape, _, err := a.Loader().LoadCompiledAssetShape(shapePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, loader := range []*RuntimeContentLoader{b.Loader()} {
		if h, _, err := loader.LoadCompiledAssetHeader(headerPath); err != nil || h != header {
			t.Fatal("second scope header mismatch", err)
		}
		if s, _, err := loader.LoadCompiledAssetShape(shapePath); err != nil || s != shape {
			t.Fatal("second scope shape mismatch", err)
		}
	}
	stats := owner.Stats()
	if stats.Entries != 2 || stats.PinnedBytes != stats.Bytes || stats.OverBudgetBytes != stats.Bytes-1 {
		t.Fatalf("compiled entries did not pin above budget: %+v", stats)
	}
	a.Close()
	if next := owner.Stats(); next.PinnedBytes != stats.PinnedBytes {
		t.Fatal("one scope released another scope's pins")
	}
	b.Close()
	if next := owner.Stats(); next.Entries != 0 || next.Bytes != 0 || next.PinnedBytes != 0 {
		t.Fatalf("released overbudget compiled entries not evicted: %+v", next)
	}
	if header.Asset.ID != "asset" || len(shape.Bricks) == 0 || len(shape.Bricks[0].Values) == 0 {
		t.Fatal("eviction invalidated borrower storage")
	}
	c := owner.NewScope()
	defer c.Close()
	if _, _, err := c.Loader().LoadCompiledAssetShape(shapePath); err != nil {
		t.Fatal(err)
	}
	owner.Clear()
	if stats := owner.Stats(); stats.Entries != 1 || stats.PinnedBytes != stats.Bytes {
		t.Fatal("Clear revoked live scope ownership")
	}
	c.Close()
	if stats := owner.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatal("closed cleared scope retained overbudget storage")
	}
	if header.Asset.ID != "asset" || len(shape.Bricks) == 0 {
		t.Fatal("Clear invalidated earlier borrowers")
	}
	if _, _, err := codec.Encode(voxelcodec.Document{Kind: "probe"}); err != nil {
		t.Fatal("cache lifetime closed borrowed codec", err)
	}
}

func TestC3d1FailuresProfilesNilLoaderAndLegacyJSON(t *testing.T) {
	dictionary := c3cCodec(t, voxelcodec.Options{Dictionary: &voxelcodec.Dictionary{ID: 53, Bytes: bytes.Repeat([]byte("compiled_asset_shape compiled_asset_header lattice schema_version"), 8)}})
	headerPath, shapePath, _, _ := c3d1Paths(t, dictionary)
	wrong := NewRuntimeContentLoader()
	if def, info, err := wrong.LoadCompiledAssetHeader(headerPath); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("wrong dictionary header returned data")
	}
	if def, info, err := wrong.LoadCompiledAssetShape(shapePath); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("wrong dictionary shape returned data")
	}
	if stats := wrong.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatal("decode failures admitted cache entries")
	}
	loader := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: dictionary})
	if def, info, err := loader.LoadCompiledAssetShape(headerPath); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("header accepted as shape")
	}
	if def, info, err := loader.LoadCompiledAssetHeader(shapePath); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("shape accepted as header")
	}
	if def, err := loader.LoadAsset(headerPath); err == nil || def != nil {
		t.Fatal("legacy JSON loader autodetected compiled header")
	}
	corrupt := filepath.Join(t.TempDir(), "corrupt.gkshape")
	frame := c3cRead(t, shapePath)
	if err := os.WriteFile(corrupt, frame[:len(frame)-1], 0600); err != nil {
		t.Fatal(err)
	}
	if def, info, err := loader.LoadCompiledAssetShape(corrupt); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("truncated shape returned data")
	}
	if stats := loader.Stats(); stats.Entries != 0 || stats.Bytes != 0 {
		t.Fatal("cross-kind/malformed failures admitted cache entries")
	}
	cached, cachedInfo, err := loader.LoadCompiledAssetHeader(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	dictionary.Close()
	if def, info, err := loader.LoadCompiledAssetHeader(headerPath); err != nil || def != cached || info != cachedInfo {
		t.Fatal("decoded hit depends on codec remaining open", err)
	}
	loader.Clear()
	if def, info, err := loader.LoadCompiledAssetHeader(headerPath); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("fresh load used closed codec")
	}
	if def, info, err := loader.LoadCompiledAssetShape(shapePath); err == nil || def != nil || info != (voxelcodec.Info{}) {
		t.Fatal("fresh shape used closed codec")
	}
	input, plainPath, _ := c3cFixture(t)
	if _, err := CompileAuthoredAsset(input, plainPath, nil); err != nil {
		t.Fatal(err)
	}
	if def, err := loader.LoadAsset(input); err != nil || def.ID != "asset" {
		t.Fatal("closed compiled codec affected legacy JSON", err)
	}
	var nilLoader *RuntimeContentLoader
	header, info, err := nilLoader.LoadCompiledAssetHeader(plainPath)
	direct, directInfo, directErr := content.LoadCompiledAssetHeader(plainPath, nil)
	if err != nil || directErr != nil || info != directInfo || header.Asset.ID != direct.Asset.ID {
		t.Fatal("nil loader header not bounded default IO", err)
	}
	plainShape := filepath.Join(filepath.Dir(plainPath), header.Shapes[0].Path)
	if shape, info, err := nilLoader.LoadCompiledAssetShape(plainShape); err != nil || len(shape.Bricks) == 0 || info.ContentID != header.Shapes[0].ContentID {
		t.Fatal("nil loader shape failed", err)
	}
}

func TestC3d1CompactBrickStorageChargedWithoutVoxelExpansion(t *testing.T) {
	dir := t.TempDir()
	codec := c3cCodec(t, voxelcodec.Options{})
	var charges [2]int64
	for index, count := range []int{1, 512} {
		brick := voxelcodec.Brick{Values: bytes.Repeat([]byte{7}, count)}
		for i := 0; i < count; i++ {
			brick.Occupancy[i/64] |= uint64(1) << uint(i%64)
		}
		path := filepath.Join(dir, []string{"sparse.gkshape", "dense.gkshape"}[index])
		if _, err := content.SaveCompiledAssetShape(path, &content.CompiledAssetShapeDef{SchemaVersion: 1, Lattice: content.VoxelObjectLatticeDef{VoxelResolution: 0.25, RasterizationVersion: "gekko-voxel-shape-v1"}, Bricks: []voxelcodec.Brick{brick}}, codec); err != nil {
			t.Fatal(err)
		}
		owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{CompiledAssetCodec: codec})
		shape, _, err := owner.LoadCompiledAssetShape(path)
		if err != nil || len(shape.Bricks[0].Values) != count {
			t.Fatal("typed compact shape changed", err)
		}
		charges[index] = owner.Stats().Bytes
	}
	if charges[1] < charges[0]+511 {
		t.Fatalf("primary values omitted from storage charge: sparse=%d dense=%d", charges[0], charges[1])
	}
	if charges[1] >= 512*int64(unsafe.Sizeof(content.VoxelObjectVoxelDef{})) {
		t.Fatalf("compact brick cache charged expanded voxel records: %d", charges[1])
	}
}

func TestC3d1ReleaseRejectedDefinitionsPreservesOtherPins(t *testing.T) {
	headerPath, shapePath, _, _ := c3d1Paths(t, nil)
	owner := NewRuntimeContentLoader(RuntimeContentLoaderOptions{MaxCacheBytes: -1})
	a, b := owner.NewScope(), owner.NewScope()
	defer a.Close()
	defer b.Close()
	header, _, err := a.Loader().LoadCompiledAssetHeader(headerPath)
	if err != nil {
		t.Fatal(err)
	}
	shape, _, err := a.Loader().LoadCompiledAssetShape(shapePath)
	if err != nil {
		t.Fatal(err)
	}
	if other, _, err := b.Loader().LoadCompiledAssetHeader(headerPath); err != nil || other != header {
		t.Fatal("header sharing", err)
	}
	if other, _, err := b.Loader().LoadCompiledAssetShape(shapePath); err != nil || other != shape {
		t.Fatal("shape sharing", err)
	}
	initial := owner.Stats()
	a.Loader().releaseScopedValue(header)
	a.Loader().releaseScopedValue(shape)
	if stats := owner.Stats(); stats.Entries != 2 || stats.PinnedBytes != initial.PinnedBytes {
		t.Fatal("rejected consumer released another scope's pins")
	}
	b.Loader().releaseScopedValue(header)
	remaining := owner.Stats()
	if remaining.Entries != 1 || remaining.PinnedBytes <= 0 || remaining.PinnedBytes >= initial.PinnedBytes {
		t.Fatalf("released header pin retained or unrelated shape dropped: %+v", remaining)
	}
	if other, _, err := b.Loader().LoadCompiledAssetShape(shapePath); err != nil || other != shape {
		t.Fatal("release invalidated unrelated shape", err)
	}
	b.Loader().releaseScopedValue(shape)
	if stats := owner.Stats(); stats.Entries != 0 || stats.Bytes != 0 || stats.PinnedBytes != 0 {
		t.Fatalf("rejected definitions still pinned: %+v", stats)
	}
	if header.Asset.ID != "asset" || len(shape.Bricks) == 0 {
		t.Fatal("rejected definition release invalidated borrower")
	}
}
