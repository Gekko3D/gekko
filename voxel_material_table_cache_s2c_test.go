package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func s2cStats(t *testing.T, state *VoxelRtState, entries int, builds, hits uint64) VoxelMaterialTableCacheStats {
	t.Helper()
	s := state.VoxelMaterialTableCacheStats()
	if s.Entries != entries || s.Builds != builds || s.Hits != hits {
		t.Fatalf("cache entries/builds/hits=%d/%d/%d, want %d/%d/%d", s.Entries, s.Builds, s.Hits, entries, builds, hits)
	}
	if state.VoxelMaterialTableCacheStats() != s {
		t.Fatal("stats reads changed cache observations")
	}
	return s
}

func s2cMaintained(t *testing.T, state *VoxelRtState) {
	t.Helper()
	s := state.VoxelMaterialTableCacheStats()
	pressure := uint64(0)
	if s.PinnedBytes > s.MaxBytes {
		pressure = s.PinnedBytes - s.MaxBytes
	}
	if s.PinnedBytes > s.Bytes || s.PressureBytes != pressure || (s.Bytes > s.MaxBytes && s.Bytes != s.PinnedBytes) {
		t.Fatalf("maintenance left invalid ownership/pressure: %+v", s)
	}
	if (s.Entries == 0) != (s.Bytes == 0) {
		t.Fatalf("cache entries/bytes inconsistent: %+v", s)
	}
}

func s2cTable(t *testing.T, state *VoxelRtState, entity EntityId) []core.Material {
	t.Helper()
	obj := state.GetVoxelObject(entity)
	if obj == nil || len(obj.MaterialTable) != 256 {
		t.Fatalf("entity %d missing complete CPU material table", entity)
	}
	return obj.MaterialTable
}

func s2cColor(t *testing.T, state *VoxelRtState, entity EntityId, want [4]uint8) {
	t.Helper()
	if got := s2cTable(t, state, entity)[1].BaseColor; got != want {
		t.Fatalf("entity %d material color=%v, want %v", entity, got, want)
	}
}

func TestS2cMaterialCacheConfigurationAndDeferredMaintenance(t *testing.T) {
	if DefaultVoxelMaterialTableCacheBytes != 16<<20 {
		t.Fatalf("default cache budget=%d, want 16 MiB", DefaultVoxelMaterialTableCacheBytes)
	}
	var absent *VoxelRtState
	absent.SetVoxelMaterialTableCacheBudgetBytes(17)
	if absent.VoxelMaterialTableCacheStats() != (VoxelMaterialTableCacheStats{}) {
		t.Fatal("nil state stats must be zero")
	}
	var zero VoxelRtState
	want := VoxelMaterialTableCacheStats{MaxBytes: DefaultVoxelMaterialTableCacheBytes}
	if zero.VoxelMaterialTableCacheStats() != want || zero.VoxelMaterialTableCacheStats() != want {
		t.Fatal("zero state stats must report default budget without work")
	}
	for _, setting := range []struct {
		input int64
		max   uint64
	}{{17, 17}, {-1, 0}, {0, DefaultVoxelMaterialTableCacheBytes}} {
		zero.SetVoxelMaterialTableCacheBudgetBytes(setting.input)
		want.MaxBytes = setting.max
		if zero.VoxelMaterialTableCacheStats() != want {
			t.Fatalf("setting %d stats=%+v, want %+v", setting.input, zero.VoxelMaterialTableCacheStats(), want)
		}
	}

	f := newS3cVoxelFixture(t)
	first := f.add(1)
	f.app.FlushCommands()
	f.sync()
	charge := s2cStats(t, f.state, 1, 1, 0).Bytes
	if charge == 0 {
		t.Fatal("retained table must have positive charge")
	}
	secondModel := f.voxelModel()
	secondModel.VoxelPalette = f.server.CreateSimplePalette([4]uint8{200, 30, 40, 255})
	second := f.cmd.AddEntity(s3cTransform(2), secondModel)
	f.app.FlushCommands()
	f.sync()
	f.cmd.AddComponents(second, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	before := s2cStats(t, f.state, 2, 2, 0)
	if before.PinnedBytes != charge {
		t.Fatal("ordinary hidden object must release its table pin")
	}
	f.state.SetVoxelMaterialTableCacheBudgetBytes(int64(charge))
	after := s2cStats(t, f.state, 2, 2, 0)
	if after.MaxBytes != charge || after.Bytes != before.Bytes || after.Evictions != before.Evictions {
		t.Fatal("budget must update immediately and defer eviction to instance sync")
	}
	f.sync() // No new table lookup; changed budgets still require maintenance.
	s := s2cStats(t, f.state, 1, 2, 0)
	if s.Bytes != charge || s.PinnedBytes != charge || s.Evictions != 1 {
		t.Fatalf("idle sync did not trim warm table: %+v", s)
	}
	s2cMaintained(t, f.state)
	s2cColor(t, f.state, first, [4]uint8{40, 80, 120, 255})
	f.state.SetVoxelMaterialTableCacheBudgetBytes(-1)
	f.sync()
	s = s2cStats(t, f.state, 1, 2, 0)
	if s.MaxBytes != 0 || s.PinnedBytes != charge || s.PressureBytes != charge || s.Evictions != 1 {
		t.Fatalf("disabled warm retention lost active pin: %+v", s)
	}
	f.cmd.RemoveEntity(first)
	f.app.FlushCommands()
	f.sync()
	s = s2cStats(t, f.state, 0, 2, 0)
	if s.PinnedBytes != 0 || s.PressureBytes != 0 || s.Evictions != 2 {
		t.Fatalf("final active release did not clear disabled cache: %+v", s)
	}
	s2cMaintained(t, f.state)
}

func TestS2cSharedActiveTablesPressureAndBorrowedLifetime(t *testing.T) {
	f := newS3cVoxelFixture(t)
	f.state.SetVoxelMaterialTableCacheBudgetBytes(-1)
	first := f.add(1)
	f.app.FlushCommands()
	f.sync()
	charge := s2cStats(t, f.state, 1, 1, 0).Bytes
	borrowed := s2cTable(t, f.state, first)
	saved := append([]core.Material(nil), borrowed...)
	second := f.add(2)
	f.sync()
	s2cStats(t, f.state, 1, 1, 0) // Queued users are not committed yet.
	f.app.FlushCommands()
	f.sync()
	s := s2cStats(t, f.state, 1, 1, 1)
	if s.Bytes != charge || s.PinnedBytes != charge || s.PressureBytes != charge || p4Binding(t, f.state.GetVoxelObject(second)).Identity() != p4Binding(t, f.state.GetVoxelObject(first)).Identity() {
		t.Fatalf("shared active key must be retained/charged once: %+v", s)
	}
	model := f.voxelModel()
	model.VoxelPalette = f.server.CreateSimplePalette([4]uint8{210, 20, 30, 255})
	third := f.cmd.AddEntity(s3cTransform(3), model)
	f.app.FlushCommands()
	f.sync()
	s = s2cStats(t, f.state, 2, 2, 1)
	if s.PinnedBytes != s.Bytes || s.PressureBytes != s.Bytes || s.Evictions != 0 {
		t.Fatalf("active pressure must retain every distinct table: %+v", s)
	}
	s2cMaintained(t, f.state)
	s2cColor(t, f.state, first, [4]uint8{40, 80, 120, 255})
	s2cColor(t, f.state, second, [4]uint8{40, 80, 120, 255})
	s2cColor(t, f.state, third, [4]uint8{210, 20, 30, 255})
	f.cmd.AddComponents(first, VoxelRenderHiddenComponent{})
	f.cmd.RemoveEntity(second)
	f.sync()
	queued := s2cStats(t, f.state, 2, 2, 1)
	if queued.Bytes != s.Bytes || queued.PinnedBytes != s.PinnedBytes || queued.Evictions != 0 {
		t.Fatal("queued hide/removal must preserve committed table pins")
	}
	f.app.FlushCommands()
	f.sync()
	s = s2cStats(t, f.state, 1, 2, 1)
	if s.Bytes != charge || s.PinnedBytes != charge || s.Evictions != 1 {
		t.Fatalf("final shared user release must evict its inactive table: %+v", s)
	}
	if !reflect.DeepEqual(borrowed, saved) {
		t.Fatal("eviction cleared or recycled caller-held material backing")
	}
	f.cmd.RemoveEntity(third)
	f.app.FlushCommands()
	f.sync()
	s = s2cStats(t, f.state, 0, 2, 1)
	if s.Evictions != 2 || !reflect.DeepEqual(borrowed, saved) {
		t.Fatal("empty cache cleanup changed borrowed material backing")
	}
	s2cMaintained(t, f.state)
}

func TestS2cMaterialCacheLRUFollowsRecentActiveFrames(t *testing.T) {
	f := newS3cVoxelFixture(t)
	a := f.add(1)
	f.app.FlushCommands()
	f.sync()
	charge := s2cStats(t, f.state, 1, 1, 0).Bytes
	aTable := s2cTable(t, f.state, a)
	f.state.SetVoxelMaterialTableCacheBudgetBytes(int64(2 * charge))
	addColor := func(x float32, color [4]uint8) EntityId {
		model := f.voxelModel()
		model.VoxelPalette = f.server.CreateSimplePalette(color)
		return f.cmd.AddEntity(s3cTransform(x), model)
	}
	b := addColor(2, [4]uint8{180, 20, 30, 255})
	f.app.FlushCommands()
	f.sync()
	s2cStats(t, f.state, 2, 2, 0)
	f.cmd.AddComponents(b, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync() // A is active later than B, without a builder cache hit.
	s2cStats(t, f.state, 2, 2, 0)
	f.cmd.AddComponents(a, VoxelRenderHiddenComponent{})
	c := addColor(3, [4]uint8{20, 180, 30, 255})
	f.app.FlushCommands()
	f.sync()
	s := s2cStats(t, f.state, 2, 3, 0)
	if s.Evictions != 1 {
		t.Fatalf("third key must evict one inactive entry: %+v", s)
	}
	// A was built before B but used more recently; A must still be warm.
	f.cmd.RemoveComponents(a, VoxelRenderHiddenComponent{})
	f.cmd.AddComponents(c, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	s2cStats(t, f.state, 2, 3, 1)
	if !reflect.DeepEqual(s2cTable(t, f.state, a), aTable) {
		t.Fatal("recent active A should reuse retained table")
	}
	f.cmd.AddComponents(a, VoxelRenderHiddenComponent{})
	f.cmd.RemoveComponents(b, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	s = s2cStats(t, f.state, 2, 4, 1)
	if s.Evictions != 2 {
		t.Fatal("older B must rebuild; recent A hit must retain A over C")
	}
	s2cColor(t, f.state, b, [4]uint8{180, 20, 30, 255})
	f.cmd.AddComponents(b, VoxelRenderHiddenComponent{})
	f.cmd.RemoveComponents(c, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	s2cStats(t, f.state, 2, 5, 1)
	s2cColor(t, f.state, c, [4]uint8{20, 180, 30, 255})
	s2cMaintained(t, f.state)
}

func TestS2cAnimatedHistoricalTablesRemainBounded(t *testing.T) {
	f := newS3cVoxelFixture(t)
	frames := make([]VoxelPaletteAnimationFrame, 8)
	for i := range frames {
		frames[i].Colors = [][4]uint8{{uint8(10 + i), 80, 120, 255}}
	}
	p := VoxelPaletteAsset{Animations: []VoxelPaletteAnimation{{ID: "s2c", Kind: "palette_sequence", FPS: 1, Mode: "loop", PaletteIndices: []uint8{1}, Frames: frames}}}
	f.palette = f.server.CreateVoxelPaletteAsset(p)
	entity := f.add(1)
	f.app.FlushCommands()
	at := func(phase int) {
		voxelRtSystem(nil, f.state, f.server, &Time{Elapsed: float64(phase)}, f.cmd, nil)
		s2cColor(t, f.state, entity, frames[phase].Colors[0])
	}
	at(0)
	charge := s2cStats(t, f.state, 1, 1, 0).Bytes
	borrowed := s2cTable(t, f.state, entity)
	saved := append([]core.Material(nil), borrowed...)
	f.state.SetVoxelMaterialTableCacheBudgetBytes(int64(2 * charge))
	at(0)
	s2cStats(t, f.state, 1, 1, 0)
	for phase := 1; phase <= 3; phase++ {
		at(phase)
		s := s2cStats(t, f.state, 2, uint64(phase+1), 0)
		if s.PinnedBytes != charge || s.Bytes > 2*charge {
			t.Fatalf("historical phases exceeded retention budget: %+v", s)
		}
		s2cMaintained(t, f.state)
	}
	at(2)
	s2cStats(t, f.state, 2, 4, 1) // Recent phase remains warm.
	at(0)
	s2cStats(t, f.state, 2, 5, 1) // Evicted phase requires a new CPU table.
	for phase := 4; phase < len(frames); phase++ {
		at(phase)
		s2cStats(t, f.state, 2, uint64(phase+2), 1)
		s2cMaintained(t, f.state)
	}
	before := f.state.VoxelMaterialTableCacheStats()
	at(7)
	if f.state.VoxelMaterialTableCacheStats() != before {
		t.Fatal("idle phase added builds, hits or maintenance work counters")
	}
	if !reflect.DeepEqual(borrowed, saved) {
		t.Fatal("historical phase eviction changed borrowed material slice")
	}
}

func TestS2cHiddenStreamedObjectsPinMaterials(t *testing.T) {
	f := newStreamedVoxelFixture(t)
	f.state.SetVoxelMaterialTableCacheBudgetBytes(1)
	f.sync()
	s := s2cStats(t, f.state, 1, 1, 0)
	if s.Bytes <= 1 || s.PinnedBytes != s.Bytes || s.PressureBytes != s.Bytes-1 {
		t.Fatalf("hidden streamed object must retain/pin its table under pressure: %+v", s)
	}
	s2cColor(t, f.state, f.entity, [4]uint8{200, 200, 200, 255})
	if f.state.GetVoxelObject(f.entity).RenderEnabled {
		t.Fatal("hidden streamed material user became visible")
	}
	streamedStatus(t, f.state, 101, StreamedVoxelRenderUploading)
	f.sync()
	s2cStats(t, f.state, 1, 1, 0)
	f.cmd.RemoveEntity(f.entity)
	f.flush()
	f.sync()
	s = s2cStats(t, f.state, 0, 1, 0)
	if s.PinnedBytes != 0 || s.Evictions != 1 {
		t.Fatalf("streamed object removal must release last table pin: %+v", s)
	}
	s2cMaintained(t, f.state)
}
