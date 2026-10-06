package gekko

import (
	"reflect"
	"strings"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

type i10GateFixture struct {
	app      *App
	cmd      *Commands
	owner    *StreamedLevelRuntimeState
	renderer *VoxelRtState
	roots    []StreamedPageKey
	entities []EntityId
	tickets  []uint64
}

func i10NewGateFixture(t *testing.T) *i10GateFixture {
	t.Helper()
	app, cmd, owner := newStreamedRuntimeHarness(t)
	owner.Generation = 7
	level, terrain, poi := i10PageFixture()
	if err := owner.ConfigurePageControlPlane(level, terrain, poi, StreamedPageProfile{}); err != nil {
		t.Fatal(err)
	}
	selection, err := owner.UpdatePageSelection(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.PinnedRoots) != 2 || selection.PinnedRoots[0].PageIndex != selection.PinnedRoots[1].PageIndex || selection.PinnedRoots[0].Layer == selection.PinnedRoots[1].Layer {
		t.Fatalf("fixture requires distinct layers with equal numeric root indices: %+v", selection.PinnedRoots)
	}
	return &i10GateFixture{app: app, cmd: cmd, owner: owner, renderer: &VoxelRtState{streamedVoxelTickets: map[uint64]*streamedVoxelTicket{}}, roots: selection.PinnedRoots}
}
func (f *i10GateFixture) ready(t *testing.T) {
	t.Helper()
	for i, key := range f.roots {
		ticket := uint64(101 + i)
		entity := f.cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: ticket, Generation: f.owner.Generation})
		f.entities = append(f.entities, entity)
		f.tickets = append(f.tickets, ticket)
		f.renderer.streamedVoxelTickets[ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: entity, Generation: f.owner.Generation}}
		if err := f.owner.BindPageRenderTicket(key, entity, ticket); err != nil {
			t.Fatal(err)
		}
	}
	f.app.FlushCommands()
}
func (f *i10GateFixture) collision() StreamedPageSpawnCollisionStatus {
	return StreamedPageSpawnCollisionStatus{Generation: f.owner.Generation, Ready: true}
}
func i10AssertBlocked(t *testing.T, status StreamedPageStartupStatus) {
	t.Helper()
	if status.Eligible {
		t.Fatalf("incomplete current evidence admitted startup: %+v", status)
	}
}

func TestI10PageStartupNeedsEveryLayerRootAndSpawnCollision(t *testing.T) {
	f := i10NewGateFixture(t)
	i10AssertBlocked(t, f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision()))
	f.ready(t)
	for _, collision := range []StreamedPageSpawnCollisionStatus{{}, {Generation: 6, Ready: true}, {Generation: 7}, {Generation: 7, Ready: true, Failure: "collision failed"}} {
		status := f.owner.PageStartupStatus(f.cmd, f.renderer, collision)
		i10AssertBlocked(t, status)
		if status.RequiredRoots != 2 || status.ReadyRoots != 2 {
			t.Fatalf("render roots lost while collision blocked: %+v", status)
		}
		if collision.Generation == 7 && collision.Failure != "" && !strings.Contains(status.Failure, collision.Failure) {
			t.Fatalf("current collision failure omitted: %+v", status)
		}
	}
	before := len(f.app.pendingAdditions)
	status := f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision())
	if !status.Eligible || !status.CollisionReady || status.RequiredRoots != 2 || status.ReadyRoots != 2 || status.Generation != 7 || status.Failure != "" {
		t.Fatalf("complete current evidence blocked: %+v", status)
	}
	if len(f.app.pendingAdditions) != before {
		t.Fatal("startup polling published an entity")
	}
	i10AssertBlocked(t, f.owner.PageStartupStatus(nil, f.renderer, f.collision()))
	i10AssertBlocked(t, f.owner.PageStartupStatus(f.cmd, nil, f.collision()))
}

func TestI10PageStartupRejectsMissingStaleAndNonreadyEvidence(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*i10GateFixture)
		failure string
	}{
		{"unknown ticket", func(f *i10GateFixture) { delete(f.renderer.streamedVoxelTickets, f.tickets[0]) }, ""},
		{"pending bridge", func(f *i10GateFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[0]].status.State = StreamedVoxelRenderPendingBridge
		}, ""},
		{"uploading", func(f *i10GateFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[0]].status.State = StreamedVoxelRenderUploading
		}, ""},
		{"failed", func(f *i10GateFixture) {
			s := &f.renderer.streamedVoxelTickets[f.tickets[0]].status
			s.State = StreamedVoxelRenderFailed
			s.Failure = "root upload failed"
		}, "root upload failed"},
		{"cancelled", func(f *i10GateFixture) {
			s := &f.renderer.streamedVoxelTickets[f.tickets[0]].status
			s.State = StreamedVoxelRenderCancelled
			s.Failure = "root cancelled"
		}, "root cancelled"},
		{"wrong status entity", func(f *i10GateFixture) { f.renderer.streamedVoxelTickets[f.tickets[0]].status.Entity = f.entities[1] }, ""},
		{"wrong status generation", func(f *i10GateFixture) { f.renderer.streamedVoxelTickets[f.tickets[0]].status.Generation = 6 }, ""},
		{"stale failed status", func(f *i10GateFixture) {
			s := &f.renderer.streamedVoxelTickets[f.tickets[0]].status
			s.Generation = 6
			s.State = StreamedVoxelRenderFailed
			s.Failure = "stale failure"
		}, ""},
		{"missing marker", func(f *i10GateFixture) {
			f.cmd.RemoveComponents(f.entities[0], &StreamedVoxelRenderComponent{})
			f.app.FlushCommands()
		}, ""},
		{"marker ticket changed", func(f *i10GateFixture) {
			f.cmd.AddComponents(f.entities[0], &StreamedVoxelRenderComponent{Ticket: 999, Generation: 7})
			f.app.FlushCommands()
		}, ""},
		{"marker generation changed", func(f *i10GateFixture) {
			f.cmd.AddComponents(f.entities[0], &StreamedVoxelRenderComponent{Ticket: f.tickets[0], Generation: 6})
			f.app.FlushCommands()
		}, ""},
		{"entity disappeared", func(f *i10GateFixture) { f.cmd.RemoveEntity(f.entities[0]); f.app.FlushCommands() }, ""},
		{"owner generation changed", func(f *i10GateFixture) { f.owner.Generation = 8 }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := i10NewGateFixture(t)
			f.ready(t)
			if !f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision()).Eligible {
				t.Fatal("positive control failed")
			}
			tc.mutate(f)
			status := f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision())
			i10AssertBlocked(t, status)
			if tc.failure != "" && !strings.Contains(status.Failure, tc.failure) {
				t.Fatalf("current failure omitted: %+v", status)
			}
			if tc.failure == "" && strings.Contains(status.Failure, "stale failure") {
				t.Fatalf("stale failure reported as current: %+v", status)
			}
		})
	}
}

func TestI10PageBindingsValidateAtomicallyAndHaveNoRendererSideEffects(t *testing.T) {
	f := i10NewGateFixture(t)
	f.ready(t)
	if err := f.owner.BindPageRenderTicket(f.roots[0], f.entities[0], f.tickets[0]); err != nil {
		t.Fatal("idempotent bind:", err)
	}
	for _, tc := range []struct {
		name   string
		key    StreamedPageKey
		entity EntityId
		ticket uint64
	}{
		{"duplicate entity", f.roots[1], f.entities[0], 999},
		{"duplicate ticket", f.roots[1], EntityId(999), f.tickets[0]},
		{"zero entity", f.roots[0], 0, 999}, {"zero ticket", f.roots[0], EntityId(999), 0},
		{"wrong owner", StreamedPageKey{Layer: f.roots[0].Layer, OwnerID: "other", SourceHash: f.roots[0].SourceHash, PageIndex: f.roots[0].PageIndex}, EntityId(999), 999},
		{"wrong source", StreamedPageKey{Layer: f.roots[0].Layer, OwnerID: f.roots[0].OwnerID, SourceHash: "other", PageIndex: f.roots[0].PageIndex}, EntityId(999), 999},
		{"unknown index", StreamedPageKey{Layer: f.roots[0].Layer, OwnerID: f.roots[0].OwnerID, SourceHash: f.roots[0].SourceHash, PageIndex: ^uint32(0)}, EntityId(999), 999},
		{"unknown layer", StreamedPageKey{Layer: StreamedPageLayer(255), OwnerID: f.roots[0].OwnerID, SourceHash: f.roots[0].SourceHash, PageIndex: 0}, EntityId(999), 999},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := f.owner.BindPageRenderTicket(tc.key, tc.entity, tc.ticket); err == nil {
				t.Fatal("invalid bind accepted")
			}
			if !f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision()).Eligible {
				t.Fatal("failed bind destroyed valid evidence")
			}
		})
	}
	level, terrain, poi := i10PageFixture()
	terrain.RootPageIndices = []uint32{999}
	if err := f.owner.ConfigurePageControlPlane(level, terrain, poi, StreamedPageProfile{}); err == nil {
		t.Fatal("invalid config accepted")
	}
	if !f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision()).Eligible {
		t.Fatal("failed config destroyed valid evidence")
	}
	replacement := f.cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: 303, Generation: 7})
	f.app.FlushCommands()
	f.renderer.streamedVoxelTickets[303] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: replacement, Generation: 7}}
	if err := f.owner.BindPageRenderTicket(f.roots[0], replacement, 303); err != nil {
		t.Fatal("fresh rebind:", err)
	}
	if !f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision()).Eligible {
		t.Fatal("fresh current evidence blocked")
	}
	if _, ok := f.renderer.StreamedVoxelStatus(f.tickets[0]); !ok {
		t.Fatal("binding forgot old renderer ticket")
	}
	level, terrain, poi = i10PageFixture()
	if err := f.owner.ConfigurePageControlPlane(level, terrain, poi, StreamedPageProfile{}); err != nil {
		t.Fatal(err)
	}
	status := f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision())
	i10AssertBlocked(t, status)
	if status.ReadyRoots != 0 {
		t.Fatal("successful reconfiguration reused previous bindings")
	}
}

func TestI10PageBindingAcceptsNonrootForFutureHandoff(t *testing.T) {
	_, _, owner := newStreamedRuntimeHarness(t)
	owner.Generation = 7
	poi, leaves := i10Branches(mgl32.Vec3{})
	level, _, _ := i10PageFixture()
	if err := owner.ConfigurePageControlPlane(level, nil, poi, StreamedPageProfile{}); err != nil {
		t.Fatal(err)
	}
	key := StreamedPageKey{Layer: StreamedPagePOI, OwnerID: poi.WorldID, SourceHash: poi.SourceHash, PageIndex: leaves[0]}
	if err := owner.BindPageRenderTicket(key, EntityId(901), 902); err != nil {
		t.Fatalf("valid nonroot page binding rejected: %v", err)
	}
}

func TestI10PageControlResetAndStopClearOnlyOwnedEvidence(t *testing.T) {
	for _, mode := range []string{"reset", "uninitialized stop", "legacy stop"} {
		t.Run(mode, func(t *testing.T) {
			f := i10NewGateFixture(t)
			if mode == "legacy stop" {
				app, cmd, owner, _ := s2bStartWorld(t, StreamedLevelRuntimeConfig{})
				app.FlushCommands()
				f.app, f.cmd, f.owner = app, cmd, owner
				level, terrain, poi := i10PageFixture()
				if err := owner.ConfigurePageControlPlane(level, terrain, poi, StreamedPageProfile{}); err != nil {
					t.Fatal(err)
				}
				selection, err := owner.UpdatePageSelection(nil)
				if err != nil {
					t.Fatal(err)
				}
				f.roots = selection.PinnedRoots
			}
			f.ready(t)
			oldStatuses := map[uint64]StreamedVoxelRenderStatus{}
			for _, ticket := range f.tickets {
				s, _ := f.renderer.StreamedVoxelStatus(ticket)
				oldStatuses[ticket] = s
			}
			if mode == "reset" {
				f.owner.DesiredChunks[ChunkCoord{X: 123}] = struct{}{}
				generation := f.owner.Generation
				f.owner.ResetPageControlPlane()
				if _, ok := f.owner.DesiredChunks[ChunkCoord{X: 123}]; !ok || f.owner.Generation != generation {
					t.Fatal("page reset changed legacy demand or generation")
				}
			} else if err := StopStreamedLevelRuntime(f.cmd); err != nil {
				t.Fatal(err)
			}
			i10AssertBlocked(t, f.owner.PageStartupStatus(f.cmd, f.renderer, f.collision()))
			if _, err := f.owner.UpdatePageSelection(nil); err == nil {
				t.Fatal("cleared page metadata remains selectable")
			}
			for ticket, want := range oldStatuses {
				got, ok := f.renderer.StreamedVoxelStatus(ticket)
				if !ok || !reflect.DeepEqual(got, want) {
					t.Fatal("control-plane lifecycle mutated renderer ownership")
				}
			}
		})
	}
}

func TestI10ControlPlaneDoesNotAdmitLiveV3Runtime(t *testing.T) {
	_, path, manifest := i08RuntimeTerrain(t)
	loader := NewRuntimeContentLoader()
	if _, err := loader.LoadTerrainChunkManifest(manifest); err == nil || !strings.Contains(err.Error(), "resident height collision") {
		t.Fatalf("live height gate changed: %v", err)
	}
	if loader.Stats().Entries != 0 {
		t.Fatal("rejected v3 cached")
	}
	app, cmd, owner := newStreamedRuntimeHarness(t)
	before := len(app.pendingAdditions)
	if err := StartStreamedLevelRuntime(cmd, newSpawnTestAssetServer(), StreamedLevelRuntimeConfig{LevelPath: path, Loader: loader}); err == nil || !strings.Contains(err.Error(), "resident height collision") {
		t.Fatalf("startup gate changed: %v", err)
	}
	if len(app.pendingAdditions) != before || owner.Level != nil || len(owner.LoadedChunks) != 0 || loader.Stats().PinnedBytes != 0 {
		t.Fatal("gated v3 startup published ownership")
	}
}
