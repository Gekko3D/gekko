package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/core"
)

func s2jAddColor(f *s3cVoxelFixture, x float32, color [4]uint8, extra ...any) (EntityId, VoxelModelComponent) {
	model := f.voxelModel()
	model.VoxelPalette = f.server.CreateSimplePalette(color)
	return f.cmd.AddEntity(append([]any{s3cTransform(x), model}, extra...)...), model
}

func TestS2jMaterialCandidatesBoundedAmidDistinctHiddenAndVisiblePins(t *testing.T) {
	const pins = 32
	f := newS3cVoxelFixture(t)
	pinned := make([]EntityId, pins)
	colors := make([][4]uint8, pins)
	for i := range pinned {
		colors[i] = [4]uint8{uint8(i + 1), 70, 80, 255}
		var extra []any
		if i%2 == 0 {
			extra = []any{VoxelRenderHiddenComponent{}, StreamedVoxelRenderComponent{Ticket: uint64(i + 1), Generation: 1, Priority: StreamedVoxelPriorityPrefetch}}
		}
		pinned[i], _ = s2jAddColor(f, float32(i), colors[i], extra...)
	}
	f.app.FlushCommands()
	f.sync()
	initial := s2cStats(t, f.state, pins, pins, 0)
	charge := initial.Bytes / pins
	if charge == 0 || initial.PinnedBytes != initial.Bytes || initial.EvictionCandidateVisits != 0 {
		t.Fatalf("distinct visible/hidden pins did not establish the fixture: %+v", initial)
	}
	warm := make([]EntityId, 4)
	borrowed := make([][]core.Material, 4)
	saved := make([][]core.Material, 4)
	for i := range warm {
		warm[i], _ = s2jAddColor(f, float32(40+i), [4]uint8{uint8(180 + i), 20, 30, 255})
		f.app.FlushCommands()
		f.sync()
		borrowed[i] = s2cTable(t, f.state, warm[i])
		saved[i] = append([]core.Material(nil), borrowed[i]...)
		f.cmd.AddComponents(warm[i], VoxelRenderHiddenComponent{})
		f.app.FlushCommands()
		f.sync()
	}
	// Reuse the oldest inactive backing, then release its pin. Its saved
	// recency must protect it over the other three inactive keys.
	f.cmd.RemoveComponents(warm[0], VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	if !reflect.DeepEqual(s2cTable(t, f.state, warm[0]), borrowed[0]) {
		t.Fatal("warm hit changed retained material content")
	}
	f.cmd.AddComponents(warm[0], VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	before := s2cStats(t, f.state, pins+4, pins+4, 1)
	f.state.SetVoxelMaterialTableCacheBudgetBytes(int64((pins + 1) * charge))
	f.sync()
	after := s2cStats(t, f.state, pins+1, pins+4, 1)
	if after.Bytes != (pins+1)*charge || after.PinnedBytes != pins*charge || after.Evictions-before.Evictions != 3 ||
		after.EvictionCandidateVisits-before.EvictionCandidateVisits != 3 {
		t.Fatalf("pressure must select only its three inactive victims: before=%+v after=%+v", before, after)
	}
	for i, entity := range pinned {
		s2cColor(t, f.state, entity, colors[i])
		if i%2 == 0 && f.state.GetVoxelObject(entity).RenderEnabled {
			t.Fatal("hidden streamed material pin became visible")
		}
	}
	for i := range borrowed {
		if !reflect.DeepEqual(borrowed[i], saved[i]) {
			t.Fatal("eviction changed caller-held material backing")
		}
	}
	f.sync()
	if got := f.state.VoxelMaterialTableCacheStats(); got != after {
		t.Fatalf("idle sync/read changed cache counters: before=%+v after=%+v", after, got)
	}
	f.cmd.RemoveComponents(warm[0], VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	s2cStats(t, f.state, pins+1, pins+4, 2)
	if !reflect.DeepEqual(s2cTable(t, f.state, warm[0]), borrowed[0]) {
		t.Fatal("recently hit inactive key was selected over older keys")
	}
	f.cmd.AddComponents(warm[0], VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.state.SetVoxelMaterialTableCacheBudgetBytes(1)
	f.sync()
	pressure := s2cStats(t, f.state, pins, pins+4, 2)
	if pressure.Bytes != pins*charge || pressure.PinnedBytes != pressure.Bytes || pressure.PressureBytes != pressure.Bytes-1 || pressure.EvictionCandidateVisits != 4 {
		t.Fatalf("inactive trim did not preserve all pinned excess: %+v", pressure)
	}
	f.sync()
	if got := f.state.VoxelMaterialTableCacheStats(); got != pressure {
		t.Fatalf("all-pinned pressure selected candidates or altered observations: before=%+v after=%+v", pressure, got)
	}
}

func TestS2jMaterialSavedRecencyEmptyCleanupAndRebuild(t *testing.T) {
	f := newS3cVoxelFixture(t)
	a, aModel := s2jAddColor(f, 1, [4]uint8{40, 80, 120, 255})
	f.app.FlushCommands()
	f.sync()
	charge := s2cStats(t, f.state, 1, 1, 0).Bytes
	borrowed := s2cTable(t, f.state, a)
	saved := append([]core.Material(nil), borrowed...)
	b, _ := s2jAddColor(f, 2, [4]uint8{180, 20, 30, 255})
	f.app.FlushCommands()
	f.sync()
	f.cmd.AddComponents(b, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync() // A's active maintenance stamp is newer than B's saved stamp.
	f.cmd.AddComponents(a, VoxelRenderHiddenComponent{})
	c, _ := s2jAddColor(f, 3, [4]uint8{20, 180, 30, 255})
	f.app.FlushCommands()
	f.state.SetVoxelMaterialTableCacheBudgetBytes(int64(2 * charge))
	f.sync()
	stats := s2cStats(t, f.state, 2, 3, 0)
	if stats.Evictions != 1 || stats.EvictionCandidateVisits != 1 || stats.Bytes != 2*charge || stats.PinnedBytes != charge {
		t.Fatalf("newly inactive A lost its later saved recency: %+v", stats)
	}
	f.cmd.RemoveComponents(a, VoxelRenderHiddenComponent{})
	f.cmd.AddComponents(c, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	s2cStats(t, f.state, 2, 3, 1)
	if !reflect.DeepEqual(s2cTable(t, f.state, a), borrowed) {
		t.Fatal("recent active A failed to retain its warm material content")
	}
	// B was the victim: making it active must build rather than hit a dead key.
	f.cmd.RemoveComponents(b, VoxelRenderHiddenComponent{})
	f.app.FlushCommands()
	f.sync()
	stats = s2cStats(t, f.state, 2, 4, 1)
	if stats.Evictions != 2 || stats.EvictionCandidateVisits != 2 || stats.PinnedBytes != 2*charge {
		t.Fatalf("evicted B did not rebuild and retire inactive C: %+v", stats)
	}
	for _, entity := range []EntityId{a, b, c} {
		f.cmd.RemoveEntity(entity)
	}
	f.app.FlushCommands()
	f.state.SetVoxelMaterialTableCacheBudgetBytes(-1)
	f.sync()
	empty := s2cStats(t, f.state, 0, 4, 1)
	if empty.Bytes != 0 || empty.PinnedBytes != 0 || empty.Evictions != 4 || empty.EvictionCandidateVisits != 4 {
		t.Fatalf("empty cleanup retained candidate owners or counted dead victims: %+v", empty)
	}
	f.sync()
	if got := f.state.VoxelMaterialTableCacheStats(); got != empty {
		t.Fatal("empty idle sync changed cumulative candidate observations")
	}
	rebuilt := f.cmd.AddEntity(s3cTransform(4), aModel)
	f.app.FlushCommands()
	f.sync()
	stats = s2cStats(t, f.state, 1, 5, 1)
	if stats.Bytes != charge || stats.PinnedBytes != charge || stats.EvictionCandidateVisits != 4 ||
		!reflect.DeepEqual(s2cTable(t, f.state, rebuilt), saved) || !reflect.DeepEqual(borrowed, saved) {
		t.Fatalf("rebuild after empty cleanup changed material content or selected active victim: %+v", stats)
	}
	s2cColor(t, f.state, rebuilt, [4]uint8{40, 80, 120, 255})
}
