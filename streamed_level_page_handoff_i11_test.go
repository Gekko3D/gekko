package gekko

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/go-gl/mathgl/mgl32"
)

type i11HandoffFixture struct {
	app      *App
	cmd      *Commands
	owner    *StreamedLevelRuntimeState
	renderer *VoxelRtState
	level    *content.LevelDef
	poi      *content.ImportedWorldDef
	keys     []StreamedPageKey
	entities map[uint32]EntityId
	tickets  map[uint32]uint64
}

func i11NewHandoffFixture(t *testing.T) *i11HandoffFixture {
	t.Helper()
	app, cmd, owner := newStreamedRuntimeHarness(t)
	owner.Generation = 11
	level, _, poi := i10PageFixture()
	// Both branches have distinct complete child cohorts. A distant sibling must
	// be prepared but must not recursively refine merely because of closure.
	poi.Pages = []content.StreamPageDef{i10VoxelPage(3, 0, 0, 0), i10VoxelPage(2, 0, 0, 0), i10VoxelPage(2, 64, 0, 0), i10VoxelPage(1, 0, 0, 0), i10VoxelPage(1, 16, 0, 0), i10VoxelPage(1, 64, 0, 0), i10VoxelPage(1, 80, 0, 0)}
	poi.Pages[0].BoundsMax = [3]float32{128, 128, 128}
	poi.Pages[0].Payload.ChunkSize = 128
	poi.Pages[0].ChildPageIndices = []uint32{1, 2}
	for _, i := range []int{1, 2} {
		p := &poi.Pages[i]
		p.BoundsMax = [3]float32{p.BoundsMin[0] + 32, 32, 32}
		p.Payload.ChunkSize = 32
	}
	poi.Pages[1].ChildPageIndices = []uint32{3, 4}
	poi.Pages[2].ChildPageIndices = []uint32{5, 6}
	poi.RootPageIndices = []uint32{0}
	profile := StreamedPageProfile{Macro: StreamedPageDistances{1, 3, 5}, Regional: StreamedPageDistances{1, 3, 5}, FullPOI: StreamedPageDistances{1, 3, 5}, SelectionCellSize: 1}
	if err := owner.ConfigurePageControlPlane(level, nil, poi, profile); err != nil {
		t.Fatal(err)
	}
	f := &i11HandoffFixture{app: app, cmd: cmd, owner: owner, renderer: &VoxelRtState{streamedVoxelTickets: map[uint64]*streamedVoxelTicket{}}, level: level, poi: poi, entities: map[uint32]EntityId{}, tickets: map[uint32]uint64{}}
	for i := range poi.Pages {
		f.keys = append(f.keys, StreamedPageKey{Layer: StreamedPagePOI, OwnerID: poi.WorldID, SourceHash: poi.SourceHash, PageIndex: uint32(i)})
	}
	return f
}
func (f *i11HandoffFixture) ready(t *testing.T, indices ...uint32) {
	t.Helper()
	for _, i := range indices {
		entity, ok := f.entities[i]
		ticket := uint64(700 + i)
		if !ok {
			entity = f.cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: ticket, Generation: f.owner.Generation})
			f.entities[i] = entity
			f.tickets[i] = ticket
			if err := f.owner.BindPageRenderTicket(f.keys[i], entity, ticket); err != nil {
				t.Fatal(err)
			}
		}
		f.renderer.streamedVoxelTickets[ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: entity, Generation: f.owner.Generation}}
	}
	f.app.FlushCommands()
}
func (f *i11HandoffFixture) selectAt(t *testing.T, xs ...float32) StreamedPageSelection {
	t.Helper()
	var observers []StreamedPageObserver
	for i, x := range xs {
		observers = append(observers, i10Observer(EntityId(i+1), x, .2, .2))
	}
	return i10Select(t, f.owner, observers...)
}
func (f *i11HandoffFixture) plan(t *testing.T) StreamedPageHandoffPlan {
	t.Helper()
	p, e := f.owner.PlanPageHandoff(f.cmd, f.renderer)
	if e != nil {
		t.Fatal(e)
	}
	if p.Generation != f.owner.Generation || p.Revision == 0 {
		t.Fatalf("unqualified plan %+v", p)
	}
	return p
}
func (f *i11HandoffFixture) accept(t *testing.T, p StreamedPageHandoffPlan) {
	t.Helper()
	if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
}
func i11WantKeys(t *testing.T, got []StreamedPageKey, f *i11HandoffFixture, indices ...uint32) {
	t.Helper()
	var want []StreamedPageKey
	for _, i := range indices {
		want = append(want, f.keys[i])
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("keys=%+v want %+v", got, want)
	}
}
func i11Frontier(t *testing.T, f *i11HandoffFixture) []StreamedPageKey {
	t.Helper()
	keys, err := f.owner.PageVisibleFrontier()
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func i11ClonePlan(t *testing.T, p StreamedPageHandoffPlan) StreamedPageHandoffPlan {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var copy StreamedPageHandoffPlan
	if err := json.Unmarshal(raw, &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}
func i11Bootstrap(t *testing.T, f *i11HandoffFixture) {
	t.Helper()
	f.ready(t, 0)
	f.selectAt(t, .2)
	p := f.plan(t)
	i11WantKeys(t, p.Visible, f, 0)
	f.accept(t, p)
}

func TestI11BootstrapRequiresWholeRootSetAndIsPollOnly(t *testing.T) {
	f := i11NewHandoffFixture(t)
	if _, err := f.owner.PlanPageHandoff(f.cmd, f.renderer); err == nil {
		t.Fatal("handoff planned without successful selection")
	}
	f.selectAt(t, .2)
	p := f.plan(t)
	i11WantKeys(t, p.Before, f)
	i11WantKeys(t, p.Visible, f)
	if len(p.Blocked) == 0 {
		t.Fatal("missing root has no diagnostic")
	}
	f.ready(t, 0, 1, 2, 3, 4, 5, 6)
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 0)
	i11WantKeys(t, p.Show, f, 0)
	i11WantKeys(t, p.Hide, f)
	beforeAdds := len(f.app.pendingAdditions)
	beforeStatus := map[uint64]StreamedVoxelRenderStatus{}
	for ticket, r := range f.renderer.streamedVoxelTickets {
		beforeStatus[ticket] = r.status
	}
	if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
	i11WantKeys(t, i11Frontier(t, f), f)
	if len(f.app.pendingAdditions) != beforeAdds {
		t.Fatal("planning/validation published entities")
	}
	f.accept(t, p)
	i11WantKeys(t, i11Frontier(t, f), f, 0)
	for ticket, want := range beforeStatus {
		if !reflect.DeepEqual(f.renderer.streamedVoxelTickets[ticket].status, want) {
			t.Fatal("handoff changed renderer status")
		}
	}
	for _, entity := range f.entities {
		if !f.cmd.EntityExists(entity) || hasComponentOfType[VoxelRenderHiddenComponent](f.cmd, entity) {
			t.Fatal("handoff changed entity visibility/ownership")
		}
	}
	if len(f.app.pendingAdditions) != beforeAdds {
		t.Fatal("acceptance published entities")
	}
}

func TestI11ChildCohortsRefineOneLevelAndBranchesProgressIndependently(t *testing.T) {
	f := i11NewHandoffFixture(t)
	i11Bootstrap(t, f)
	f.ready(t, 1, 3, 4, 5, 6)
	p := f.plan(t)
	i11WantKeys(t, p.Visible, f, 0)
	if len(p.Blocked) == 0 {
		t.Fatal("missing child cohort member not diagnosed")
	}
	f.ready(t, 2)
	p = f.plan(t)
	i11WantKeys(t, p.Before, f, 0)
	i11WantKeys(t, p.Visible, f, 1, 2)
	i11WantKeys(t, p.Hide, f, 0)
	f.accept(t, p)
	// Only near branch refines, although its complete cohort includes far child4.
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 2, 3, 4)
	i11WantKeys(t, p.Show, f, 3, 4)
	i11WantKeys(t, p.Hide, f, 1)
	f.accept(t, p)
	f.selectAt(t, .2, 64.2)
	f.renderer.streamedVoxelTickets[f.tickets[6]].status.State = StreamedVoxelRenderUploading
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 2, 3, 4)
	f.ready(t, 6)
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 3, 4, 5, 6)
	f.accept(t, p)
}

func TestI11CoarseningKeepsFullSubtreeUntilAncestorReady(t *testing.T) {
	f := i11NewHandoffFixture(t)
	i11Bootstrap(t, f)
	f.ready(t, 1, 2, 3, 4)
	f.accept(t, f.plan(t))
	f.accept(t, f.plan(t))
	i11WantKeys(t, i11Frontier(t, f), f, 2, 3, 4)
	f.selectAt(t)
	f.renderer.streamedVoxelTickets[f.tickets[0]].status.State = StreamedVoxelRenderUploading
	p := f.plan(t)
	i11WantKeys(t, p.Visible, f, 2, 3, 4)
	i11WantKeys(t, p.Hide, f)
	for _, key := range append(append([]StreamedPageKey{}, p.Before...), p.Visible...) {
		if !i10Contains(p.Retain, key.Layer, key.PageIndex) {
			t.Fatal("active subtree not retained")
		}
	}
	f.ready(t, 0)
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 0)
	i11WantKeys(t, p.Hide, f, 2, 3, 4)
	i11WantKeys(t, p.Show, f, 0)
	for _, i := range []uint32{0, 1, 2, 3, 4} {
		if !i10Contains(p.Retain, f.keys[i].Layer, i) {
			t.Fatalf("visible ancestor/page%d not retained", i)
		}
	}
	f.accept(t, p)
	i11WantKeys(t, i11Frontier(t, f), f, 0)
}

func TestI11PrefetchAndKeepDoNotAuthorizeVisibility(t *testing.T) {
	f := i11NewHandoffFixture(t)
	f.ready(t, 0, 1, 2, 3, 4, 5, 6)
	observer := i10Observer(1, -2.2, .2, .2)
	observer.Velocity = mgl32.Vec3{1, 0, 0}
	selection := i10Select(t, f.owner, observer)
	if !i10Contains(selection.Prefetch, StreamedPagePOI, 3) || i10Contains(selection.Desired, StreamedPagePOI, 3) {
		t.Fatal("fixture must request prefetch-only detail")
	}
	p := f.plan(t)
	f.accept(t, p)
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 0)
	i11WantKeys(t, p.Show, f)
	if !i10Contains(p.Retain, StreamedPagePOI, 3) {
		t.Fatal("prefetched detail not retained")
	}
	observer.Velocity = mgl32.Vec3{}
	selection = i10Select(t, f.owner, observer)
	if !i10Contains(selection.Keep, StreamedPagePOI, 3) || i10Contains(selection.Desired, StreamedPagePOI, 3) {
		t.Fatal("fixture must be keep-only")
	}
	p = f.plan(t)
	i11WantKeys(t, p.Visible, f, 0)
}

func TestI11AcceptanceRequiresCurrentFullVisibleEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*i11HandoffFixture)
	}{
		{"missing entity", func(f *i11HandoffFixture) { f.cmd.RemoveEntity(f.entities[2]); f.app.FlushCommands() }},
		{"missing marker", func(f *i11HandoffFixture) {
			f.cmd.RemoveComponents(f.entities[2], &StreamedVoxelRenderComponent{})
			f.app.FlushCommands()
		}},
		{"marker ticket", func(f *i11HandoffFixture) {
			f.cmd.AddComponents(f.entities[2], &StreamedVoxelRenderComponent{Ticket: 999, Generation: 11})
			f.app.FlushCommands()
		}},
		{"marker generation", func(f *i11HandoffFixture) {
			f.cmd.AddComponents(f.entities[2], &StreamedVoxelRenderComponent{Ticket: f.tickets[2], Generation: 10})
			f.app.FlushCommands()
		}},
		{"missing renderer", func(f *i11HandoffFixture) { delete(f.renderer.streamedVoxelTickets, f.tickets[2]) }},
		{"status entity", func(f *i11HandoffFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[2]].status.Entity = f.entities[1]
		}},
		{"status generation", func(f *i11HandoffFixture) { f.renderer.streamedVoxelTickets[f.tickets[2]].status.Generation = 10 }},
		{"pending", func(f *i11HandoffFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[2]].status.State = StreamedVoxelRenderPendingBridge
		}},
		{"uploading", func(f *i11HandoffFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[2]].status.State = StreamedVoxelRenderUploading
		}},
		{"failed", func(f *i11HandoffFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[2]].status.State = StreamedVoxelRenderFailed
		}},
		{"cancelled", func(f *i11HandoffFixture) {
			f.renderer.streamedVoxelTickets[f.tickets[2]].status.State = StreamedVoxelRenderCancelled
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := i11NewHandoffFixture(t)
			i11Bootstrap(t, f)
			f.ready(t, 1, 2)
			f.accept(t, f.plan(t))
			f.ready(t, 3, 4)
			p := f.plan(t)
			i11WantKeys(t, p.Visible, f, 2, 3, 4)
			before := i11Frontier(t, f)
			tc.mutate(f)
			if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, nil); err == nil {
				t.Fatal("stale full-frontier evidence validated")
			}
			if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err == nil {
				t.Fatal("stale full-frontier evidence accepted")
			}
			if !reflect.DeepEqual(before, i11Frontier(t, f)) {
				t.Fatal("failed accept changed frontier")
			}
		})
	}
	f := i11NewHandoffFixture(t)
	f.ready(t, 0)
	f.selectAt(t)
	p := f.plan(t)
	if err := f.owner.ValidatePageHandoff(nil, f.renderer, p, nil); err == nil {
		t.Fatal("nil commands validated")
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, nil, p, nil); err == nil {
		t.Fatal("nil renderer accepted")
	}
}

func TestI11PlanOwnershipSupersessionAndUnchangedSelection(t *testing.T) {
	f := i11NewHandoffFixture(t)
	f.ready(t, 0)
	f.selectAt(t, .2)
	p := f.plan(t)
	pristine := i11ClonePlan(t, p)
	// Mutate the actual returned slices, not a copy, to expose outbound aliases.
	p.Visible[0].OwnerID = "tampered"
	p.Retain[0].OwnerID = "tampered"
	p.Requests[0].Priority = StreamedVoxelPriorityKeep
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err == nil {
		t.Fatal("modified plan accepted")
	}
	i11WantKeys(t, i11Frontier(t, f), f)
	// Cached updates retain the same selection revision and do not stale a plan.
	f.selectAt(t, .3)
	if err := f.owner.BindPageRenderTicket(f.keys[0], f.entities[0], f.tickets[0]); err != nil {
		t.Fatal(err)
	}
	badKey := f.keys[0]
	badKey.OwnerID = "invalid"
	if err := f.owner.BindPageRenderTicket(badKey, f.entities[0], f.tickets[0]); err == nil {
		t.Fatal("invalid binding accepted")
	}
	f.accept(t, pristine)
	exposed := i11Frontier(t, f)
	exposed[0].OwnerID = "tampered"
	i11WantKeys(t, i11Frontier(t, f), f, 0)
	old := f.plan(t)
	newer := f.plan(t)
	if newer.Revision <= old.Revision {
		t.Fatal("plan revisions not monotonic")
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, old, nil); err == nil {
		t.Fatal("superseded plan accepted")
	}
	f.accept(t, newer)
}

func TestI11PlansBecomeStaleAfterSelectionBindingOrOwnerChanges(t *testing.T) {
	for _, mode := range []string{"selection", "binding", "binding roundtrip", "reset", "reconfigure", "generation"} {
		t.Run(mode, func(t *testing.T) {
			f := i11NewHandoffFixture(t)
			f.ready(t, 0)
			f.selectAt(t, .2)
			p := f.plan(t)
			switch mode {
			case "selection":
				f.selectAt(t, 64.2)
			case "binding", "binding roundtrip":
				entity := f.cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: 900, Generation: 11})
				f.app.FlushCommands()
				f.renderer.streamedVoxelTickets[900] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: entity, Generation: 11}}
				if err := f.owner.BindPageRenderTicket(f.keys[0], entity, 900); err != nil {
					t.Fatal(err)
				}
				if mode == "binding roundtrip" {
					if err := f.owner.BindPageRenderTicket(f.keys[0], f.entities[0], f.tickets[0]); err != nil {
						t.Fatal(err)
					}
				}
			case "reset":
				f.owner.ResetPageControlPlane()
			case "reconfigure":
				if err := f.owner.ConfigurePageControlPlane(f.level, nil, f.poi, StreamedPageProfile{}); err != nil {
					t.Fatal(err)
				}
			case "generation":
				f.owner.Generation++
			}
			if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err == nil {
				t.Fatal("stale plan accepted")
			}
			if mode == "reset" || mode == "generation" {
				if _, err := f.owner.PageVisibleFrontier(); err == nil {
					t.Fatal("stale/absent owner accessor succeeded")
				}
			} else {
				i11WantKeys(t, i11Frontier(t, f), f)
			}
			if mode == "reset" || mode == "reconfigure" {
				if err := f.owner.ConfigurePageControlPlane(f.level, nil, f.poi, StreamedPageProfile{}); err != nil {
					t.Fatal(err)
				}
				f.selectAt(t)
				fresh := f.plan(t)
				if fresh.Revision <= p.Revision {
					t.Fatal("same-generation reset reused old revision")
				}
			}
		})
	}
}

func TestI11BootstrapNeverPublishesPartialIndependentRoots(t *testing.T) {
	g := i10NewGateFixture(t)
	i10Select(t, g.owner)
	bind := func(i int) {
		ticket := uint64(1200 + i)
		entity := g.cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: ticket, Generation: g.owner.Generation})
		g.renderer.streamedVoxelTickets[ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: entity, Generation: g.owner.Generation}}
		if err := g.owner.BindPageRenderTicket(g.roots[i], entity, ticket); err != nil {
			t.Fatal(err)
		}
		g.app.FlushCommands()
	}
	bind(0)
	partial, err := g.owner.PlanPageHandoff(g.cmd, g.renderer)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Visible) != 0 || len(partial.Show) != 0 || len(partial.Blocked) == 0 {
		t.Fatalf("partial independent roots admitted: %+v", partial)
	}
	bind(1)
	complete, err := g.owner.PlanPageHandoff(g.cmd, g.renderer)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(complete.Visible, g.roots) {
		t.Fatal("complete root set not proposed")
	}
	if err := g.owner.AcceptPageHandoff(g.cmd, g.renderer, complete, nil); err != nil {
		t.Fatal(err)
	}
	frontier, err := g.owner.PageVisibleFrontier()
	if err != nil || !reflect.DeepEqual(frontier, g.roots) {
		t.Fatalf("logical roots not acknowledged: %+v %v", frontier, err)
	}
	if g.owner.PageStartupStatus(g.cmd, g.renderer, StreamedPageSpawnCollisionStatus{}).Eligible {
		t.Fatal("handoff bypassed independent spawn collision gate")
	}
}

func TestI11EveryPublicPlanFieldIsSealed(t *testing.T) {
	f := i11NewHandoffFixture(t)
	f.ready(t, 0)
	f.selectAt(t, .2)
	original := f.plan(t)
	mutations := []struct {
		name   string
		change func(*StreamedPageHandoffPlan)
	}{
		{"generation", func(p *StreamedPageHandoffPlan) { p.Generation++ }},
		{"revision", func(p *StreamedPageHandoffPlan) { p.Revision++ }},
		{"before", func(p *StreamedPageHandoffPlan) { p.Before = []StreamedPageKey{f.keys[0]} }},
		{"visible", func(p *StreamedPageHandoffPlan) { p.Visible = nil }},
		{"show", func(p *StreamedPageHandoffPlan) { p.Show = nil }},
		{"hide", func(p *StreamedPageHandoffPlan) { p.Hide = []StreamedPageKey{f.keys[0]} }},
		{"retain", func(p *StreamedPageHandoffPlan) { p.Retain = nil }},
		{"requests", func(p *StreamedPageHandoffPlan) { p.Requests = nil }},
		{"blocked", func(p *StreamedPageHandoffPlan) {
			p.Blocked = []StreamedPageHandoffBlock{{Key: f.keys[0], Reason: "forged"}}
		}},
		{"coverage groups", func(p *StreamedPageHandoffPlan) {
			p.CoverageGroups = []StreamedPageCoverageTransition{{GroupID: "forged", Activate: true}}
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			p := i11ClonePlan(t, original)
			tc.change(&p)
			if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, nil); err == nil {
				t.Fatal("modified plan validated")
			}
			if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err == nil {
				t.Fatal("modified plan accepted")
			}
			i11WantKeys(t, i11Frontier(t, f), f)
			if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, original, nil); err != nil {
				t.Fatalf("invalid plan damaged pending snapshot: %v", err)
			}
		})
	}
	f.accept(t, original)
}
