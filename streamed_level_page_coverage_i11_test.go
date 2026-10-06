package gekko

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

// Coverage tests own their fixtures independently of the handoff test suite.
// Bounds are world-space metadata; no referenced payload file exists.
type i11CoverageFixture struct {
	app      *App
	cmd      *Commands
	owner    *StreamedLevelRuntimeState
	renderer *VoxelRtState
	terrain  *content.TerrainChunkManifestDef
	poi      *content.ImportedWorldDef
	tickets  map[StreamedPageKey]uint64
}

func i11CoveragePage(layer StreamedPageLayer, level uint8, x float32, children ...uint32) content.StreamPageDef {
	p := i10VoxelPage(level, x, 0, 0)
	p.BoundsMax = [3]float32{x + 2, 2, 2}
	p.ChildPageIndices = children
	if layer == StreamedPageTerrain {
		p.Payload.Kind = content.TerrainHeightTilePayloadKind
		p.Payload.SampleSpacing = 2
		p.Payload.VoxelResolution = 2
		p.Payload.HeightScale = 2
		p.Payload.PayloadSizeBytes = 2
	}
	return p
}

func i11CoverageDefinitions() (*content.TerrainChunkManifestDef, *content.ImportedWorldDef) {
	_, terrain, poi := i10PageFixture()
	terrain.Pages = []content.StreamPageDef{
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRoot, 0, 1),
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRegional, 0),
	}
	poi.Pages = []content.StreamPageDef{
		i11CoveragePage(StreamedPagePOI, content.StreamPageLevelRoot, 0, 1),
		i11CoveragePage(StreamedPagePOI, content.StreamPageLevelRegional, 0),
	}
	terrain.Pages[1].CoverageGroup = "bunker"
	poi.Pages[1].CoverageGroup = "bunker"
	return terrain, poi
}

func i11CoverageNew(t *testing.T, terrain *content.TerrainChunkManifestDef, poi *content.ImportedWorldDef) *i11CoverageFixture {
	t.Helper()
	level := &content.LevelDef{}
	if _, err := content.BuildLevelStreamingIndex(level, terrain, poi); err != nil {
		t.Fatalf("invalid coverage fixture: %v", err)
	}
	app, cmd, owner := newStreamedRuntimeHarness(t)
	owner.Generation = 7
	if err := owner.ConfigurePageControlPlane(level, terrain, poi, i10Profile()); err != nil {
		t.Fatal(err)
	}
	f := &i11CoverageFixture{app: app, cmd: cmd, owner: owner, renderer: &VoxelRtState{streamedVoxelTickets: map[uint64]*streamedVoxelTicket{}}, terrain: terrain, poi: poi, tickets: map[StreamedPageKey]uint64{}}
	for layer, pages := range [][]content.StreamPageDef{terrain.Pages, poi.Pages} {
		for i := range pages {
			key := f.key(StreamedPageLayer(layer), uint32(i))
			ticket := uint64(1000 + layer*100 + i)
			entity := cmd.AddEntity(&StreamedVoxelRenderComponent{Ticket: ticket, Generation: owner.Generation})
			f.renderer.streamedVoxelTickets[ticket] = &streamedVoxelTicket{status: StreamedVoxelRenderStatus{State: StreamedVoxelRenderReady, Entity: entity, Generation: owner.Generation}}
			if err := owner.BindPageRenderTicket(key, entity, ticket); err != nil {
				t.Fatal(err)
			}
			f.tickets[key] = ticket
		}
	}
	app.FlushCommands()
	return f
}

func (f *i11CoverageFixture) key(layer StreamedPageLayer, index uint32) StreamedPageKey {
	if layer == StreamedPageTerrain {
		return StreamedPageKey{Layer: layer, OwnerID: f.terrain.TerrainID, SourceHash: f.terrain.SourceHash, PageIndex: index}
	}
	return StreamedPageKey{Layer: layer, OwnerID: f.poi.WorldID, SourceHash: f.poi.SourceHash, PageIndex: index}
}
func (f *i11CoverageFixture) plan(t *testing.T) StreamedPageHandoffPlan {
	t.Helper()
	p, err := f.owner.PlanPageHandoff(f.cmd, f.renderer)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (f *i11CoverageFixture) bootstrap(t *testing.T) {
	t.Helper()
	if _, err := f.owner.UpdatePageSelection(nil); err != nil {
		t.Fatal(err)
	}
	p := f.plan(t)
	if len(p.Visible) != len(f.terrain.RootPageIndices)+len(f.poi.RootPageIndices) {
		t.Fatalf("incomplete bootstrap: %+v", p)
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
}
func (f *i11CoverageFixture) selectNear(t *testing.T, positions ...float32) {
	t.Helper()
	observers := make([]StreamedPageObserver, 0, len(positions))
	for i, x := range positions {
		observers = append(observers, i10Observer(EntityId(i+1), x, 0, 0))
	}
	if _, err := f.owner.UpdatePageSelection(observers); err != nil {
		t.Fatal(err)
	}
}
func i11CoverageProof(p StreamedPageHandoffPlan) []StreamedPageCoverageStatus {
	out := make([]StreamedPageCoverageStatus, 0, len(p.CoverageGroups))
	for _, group := range p.CoverageGroups {
		out = append(out, StreamedPageCoverageStatus{GroupID: group.GroupID, Generation: p.Generation, PlanRevision: p.Revision, Ready: true})
	}
	return out
}
func i11CoverageWantKeys(t *testing.T, got []StreamedPageKey, want ...StreamedPageKey) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("frontier=%+v want=%+v", got, want)
	}
	for _, k := range want {
		found := false
		for _, g := range got {
			found = found || g == k
		}
		if !found {
			t.Fatalf("missing qualified key %+v in %+v", k, got)
		}
	}
}
func i11CoverageWantGroup(t *testing.T, p StreamedPageHandoffPlan, id string, activate bool) {
	t.Helper()
	for _, g := range p.CoverageGroups {
		if g.GroupID == id && g.Activate == activate {
			return
		}
	}
	t.Fatalf("missing group %s activate=%v: %+v", id, activate, p.CoverageGroups)
}

func TestI11CoverageQualifiedMembersWaitForCompleteGroup(t *testing.T) {
	terrain, poi := i11CoverageDefinitions()
	f := i11CoverageNew(t, terrain, poi)
	f.bootstrap(t)
	f.selectNear(t, 0)
	missing := f.key(StreamedPagePOI, 1)
	f.renderer.streamedVoxelTickets[f.tickets[missing]].status.State = StreamedVoxelRenderUploading
	p := f.plan(t)
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 0), f.key(StreamedPagePOI, 0))
	if len(p.Show) != 0 || len(p.Hide) != 0 || len(p.CoverageGroups) != 0 {
		t.Fatalf("partial replacement: %+v", p)
	}
	if len(p.Blocked) == 0 {
		t.Fatal("missing group coverage had no diagnostic")
	}
	f.renderer.streamedVoxelTickets[f.tickets[missing]].status.State = StreamedVoxelRenderReady
	p = f.plan(t)
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 1), f.key(StreamedPagePOI, 1))
	i11CoverageWantKeys(t, p.Show, f.key(StreamedPageTerrain, 1), f.key(StreamedPagePOI, 1))
	i11CoverageWantKeys(t, p.Hide, f.key(StreamedPageTerrain, 0), f.key(StreamedPagePOI, 0))
	if len(p.CoverageGroups) != 1 {
		t.Fatalf("duplicate/absent requirement: %+v", p.CoverageGroups)
	}
	i11CoverageWantGroup(t, p, "bunker", true)
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, i11CoverageProof(p)); err != nil {
		t.Fatal(err)
	}
}

func TestI11CoverageActivationAndReverseNeedCurrentCollisionProof(t *testing.T) {
	for _, activate := range []bool{true, false} {
		t.Run(fmt.Sprintf("activate=%v", activate), func(t *testing.T) {
			terrain, poi := i11CoverageDefinitions()
			f := i11CoverageNew(t, terrain, poi)
			f.bootstrap(t)
			f.selectNear(t, 0)
			p := f.plan(t)
			if !activate {
				if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, i11CoverageProof(p)); err != nil {
					t.Fatal(err)
				}
				f.selectNear(t)
				p = f.plan(t)
			}
			i11CoverageWantGroup(t, p, "bunker", activate)
			before, err := f.owner.PageVisibleFrontier()
			if err != nil {
				t.Fatal(err)
			}
			valid := i11CoverageProof(p)[0]
			bad := map[string][]StreamedPageCoverageStatus{
				"missing":          nil,
				"wrong generation": {{GroupID: valid.GroupID, Generation: valid.Generation + 1, PlanRevision: valid.PlanRevision, Ready: true}},
				"wrong revision":   {{GroupID: valid.GroupID, Generation: valid.Generation, PlanRevision: valid.PlanRevision + 1, Ready: true}},
				"nonready":         {{GroupID: valid.GroupID, Generation: valid.Generation, PlanRevision: valid.PlanRevision}},
				"failure":          {{GroupID: valid.GroupID, Generation: valid.Generation, PlanRevision: valid.PlanRevision, Ready: true, Failure: "collision unavailable"}},
				"duplicate":        {valid, valid},
				"unknown":          {valid, {GroupID: "other", Generation: valid.Generation, PlanRevision: valid.PlanRevision, Ready: true}},
			}
			for name, proof := range bad {
				t.Run(name, func(t *testing.T) {
					if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, proof); err == nil {
						t.Fatal("invalid proof validated")
					}
					if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, proof); err == nil {
						t.Fatal("invalid proof accepted")
					}
					got, err := f.owner.PageVisibleFrontier()
					if err != nil || !reflect.DeepEqual(got, before) {
						t.Fatalf("rejection changed frontier: %+v %v", got, err)
					}
				})
			}
			if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, []StreamedPageCoverageStatus{valid}); err != nil {
				t.Fatal(err)
			}
			got, _ := f.owner.PageVisibleFrontier()
			if !reflect.DeepEqual(got, before) {
				t.Fatal("validation published frontier")
			}
			if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, []StreamedPageCoverageStatus{valid}); err != nil {
				t.Fatal(err)
			}
			got, err = f.owner.PageVisibleFrontier()
			if err != nil {
				t.Fatal(err)
			}
			i11CoverageWantKeys(t, got, p.Visible...)
		})
	}
}

func i11CoverageChainDefinitions() (*content.TerrainChunkManifestDef, *content.ImportedWorldDef) {
	terrain, poi := i11CoverageDefinitions()
	// Only POI page 1 reaches the near observer. Its deliberately broad bounds
	// overlap the distant terrain counterpart. That counterpart's sibling adds
	// group second, which finally adds another distant POI root's child.
	terrain.Pages = []content.StreamPageDef{
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRoot, 100, 1, 2),
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRegional, 100),
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRegional, 200),
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRoot, 0, 4),
		i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRegional, 0),
	}
	terrain.Pages[0].BoundsMax[0] = 202
	terrain.Pages[1].CoverageGroup = "bunker"
	terrain.Pages[2].CoverageGroup = "annex"
	terrain.RootPageIndices = []uint32{0, 3}
	poi.Pages[0].BoundsMax[0] = 102
	poi.Pages[1].BoundsMax[0] = 102
	poi.Pages = append(poi.Pages, i11CoveragePage(StreamedPagePOI, content.StreamPageLevelRoot, 200, 3), i11CoveragePage(StreamedPagePOI, content.StreamPageLevelRegional, 200))
	poi.RootPageIndices = []uint32{0, 2}
	poi.Pages[3].CoverageGroup = "annex"
	return terrain, poi
}

func i11CoverageSelectChain(t *testing.T, f *i11CoverageFixture) {
	t.Helper()
	selection, err := f.owner.UpdatePageSelection([]StreamedPageObserver{i10Observer(1, 0, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []StreamedPageKey{f.key(StreamedPageTerrain, 1), f.key(StreamedPageTerrain, 2), f.key(StreamedPagePOI, 3)} {
		for _, selected := range selection.Desired {
			if selected == key {
				t.Fatalf("chain fixture already selected supposedly induced page %+v", key)
			}
		}
	}
}

func TestI11CoverageSiblingClosureExpandsGroupsTransitively(t *testing.T) {
	terrain, poi := i11CoverageChainDefinitions()
	f := i11CoverageNew(t, terrain, poi)
	f.bootstrap(t)
	i11CoverageSelectChain(t, f)
	p := f.plan(t)
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 1), f.key(StreamedPageTerrain, 2), f.key(StreamedPageTerrain, 4), f.key(StreamedPagePOI, 1), f.key(StreamedPagePOI, 3))
	i11CoverageWantGroup(t, p, "bunker", true)
	i11CoverageWantGroup(t, p, "annex", true)
	if len(p.CoverageGroups) != 2 {
		t.Fatalf("unexpected requirements: %+v", p.CoverageGroups)
	}
	if p.CoverageGroups[0].GroupID != "annex" || p.CoverageGroups[1].GroupID != "bunker" {
		t.Fatalf("group order is not deterministic: %+v", p.CoverageGroups)
	}
	for _, key := range []StreamedPageKey{f.key(StreamedPageTerrain, 1), f.key(StreamedPageTerrain, 2), f.key(StreamedPagePOI, 3)} {
		found := false
		for _, request := range p.Requests {
			if request.Key == key && request.Priority == StreamedVoxelPriorityVisible {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing visible-priority group request %+v", key)
		}
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, i11CoverageProof(p)); err != nil {
		t.Fatal(err)
	}
}

func TestI11CoverageTransitiveRollbackPreservesUnrelatedProgress(t *testing.T) {
	terrain, poi := i11CoverageChainDefinitions()
	f := i11CoverageNew(t, terrain, poi)
	f.bootstrap(t)
	i11CoverageSelectChain(t, f)
	missing := f.key(StreamedPagePOI, 3)
	f.renderer.streamedVoxelTickets[f.tickets[missing]].status.State = StreamedVoxelRenderUploading
	p := f.plan(t)
	// The missing second POI member rolls back terrain's shared cohort, then
	// bunker must roll back the first POI proposal too. The disjoint tree moves.
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 0), f.key(StreamedPageTerrain, 4), f.key(StreamedPagePOI, 0), f.key(StreamedPagePOI, 2))
	i11CoverageWantKeys(t, p.Show, f.key(StreamedPageTerrain, 4))
	i11CoverageWantKeys(t, p.Hide, f.key(StreamedPageTerrain, 3))
	if len(p.CoverageGroups) != 0 || len(p.Blocked) == 0 {
		t.Fatalf("partial coupled group survived rollback: %+v", p)
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
	f.renderer.streamedVoxelTickets[f.tickets[missing]].status.State = StreamedVoxelRenderReady
	p = f.plan(t)
	i11CoverageWantGroup(t, p, "bunker", true)
	i11CoverageWantGroup(t, p, "annex", true)
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, i11CoverageProof(p)); err != nil {
		t.Fatal(err)
	}
}

func TestI11CoverageUnequalDepthCanAdvanceUngroupedCounterpart(t *testing.T) {
	terrain, poi := i11CoverageDefinitions()
	poi.Pages[1] = i11CoveragePage(StreamedPagePOI, content.StreamPageLevelMacro, 0, 2)
	poi.Pages = append(poi.Pages, i11CoveragePage(StreamedPagePOI, content.StreamPageLevelRegional, 0))
	poi.Pages[2].CoverageGroup = "bunker"
	f := i11CoverageNew(t, terrain, poi)
	f.bootstrap(t)
	f.selectNear(t, 0)
	p := f.plan(t)
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 0), f.key(StreamedPagePOI, 1))
	if len(p.CoverageGroups) != 0 {
		t.Fatalf("early partial group activation: %+v", p)
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
	p = f.plan(t)
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 1), f.key(StreamedPagePOI, 2))
	i11CoverageWantGroup(t, p, "bunker", true)
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, i11CoverageProof(p)); err != nil {
		t.Fatal(err)
	}
}

func TestI11CoverageUnsupportedNonterminalPreservesFallbackAndDisjointProgress(t *testing.T) {
	terrain, poi := i11CoverageDefinitions()
	terrain.Pages[1].Level = content.StreamPageLevelMacro
	terrain.Pages[1].ChildPageIndices = []uint32{2}
	terrain.Pages = append(terrain.Pages, i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRegional, 0), i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRoot, 100, 4), i11CoveragePage(StreamedPageTerrain, content.StreamPageLevelRegional, 100))
	terrain.RootPageIndices = []uint32{0, 3}
	f := i11CoverageNew(t, terrain, poi)
	f.bootstrap(t) // Containment of an unsupported descendant must not block roots.
	f.selectNear(t, 0, 100)
	p := f.plan(t)
	i11CoverageWantKeys(t, p.Visible, f.key(StreamedPageTerrain, 0), f.key(StreamedPageTerrain, 4), f.key(StreamedPagePOI, 0))
	if len(p.Blocked) == 0 || len(p.CoverageGroups) != 0 {
		t.Fatalf("unsupported group lacked fail-closed diagnosis: %+v", p)
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err != nil {
		t.Fatal(err)
	}
}

func TestI11CoverageUnsupportedRootCannotPartiallyBootstrap(t *testing.T) {
	terrain, poi := i11CoverageDefinitions()
	terrain.Pages[1].CoverageGroup = ""
	terrain.Pages[0].CoverageGroup = "bunker"
	poi.Pages = append(poi.Pages, i11CoveragePage(StreamedPagePOI, content.StreamPageLevelRoot, 100))
	poi.RootPageIndices = []uint32{0, 2}
	f := i11CoverageNew(t, terrain, poi)
	f.selectNear(t)
	p := f.plan(t)
	if len(p.Visible) != 0 || len(p.Show) != 0 || len(p.Blocked) == 0 {
		t.Fatalf("unsupported root allowed partial bootstrap: %+v", p)
	}
	frontier, err := f.owner.PageVisibleFrontier()
	if err != nil || len(frontier) != 0 {
		t.Fatalf("planning changed frontier: %+v %v", frontier, err)
	}
}

func TestI11CoverageRetainedActiveMemberReadinessIsRechecked(t *testing.T) {
	terrain, poi := i11CoverageDefinitions()
	f := i11CoverageNew(t, terrain, poi)
	f.bootstrap(t)
	f.selectNear(t, 0)
	p := f.plan(t)
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, i11CoverageProof(p)); err != nil {
		t.Fatal(err)
	}
	before, err := f.owner.PageVisibleFrontier()
	if err != nil {
		t.Fatal(err)
	}
	p = f.plan(t)
	if len(p.CoverageGroups) != 0 {
		t.Fatalf("retained group required transition: %+v", p)
	}
	key := f.key(StreamedPageTerrain, 1)
	f.renderer.streamedVoxelTickets[f.tickets[key]].status.State = StreamedVoxelRenderCancelled
	if err := f.owner.ValidatePageHandoff(f.cmd, f.renderer, p, nil); err == nil {
		t.Fatal("cancelled retained member validated")
	}
	if err := f.owner.AcceptPageHandoff(f.cmd, f.renderer, p, nil); err == nil {
		t.Fatal("cancelled retained member accepted")
	}
	got, err := f.owner.PageVisibleFrontier()
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatalf("readiness rejection changed frontier: %+v %v", got, err)
	}
}
