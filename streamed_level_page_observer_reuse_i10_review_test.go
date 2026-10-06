package gekko

import (
	"reflect"
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestI10UnchangedVisualObserverAvoidsQueriesDuringNonvisualRosterChanges(t *testing.T) {
	poi, _ := i10Branches(mgl32.Vec3{25, 0, 0}, mgl32.Vec3{500, 0, 0})
	state := i10Configure(t, nil, poi, i10Profile())
	visual := i10Observer(1, .2, .2, .2)
	visual.Velocity = mgl32.Vec3{1, 0, 0}
	first := i10Select(t, state, visual)
	if first.PageCandidateVisits == 0 {
		t.Fatal("positive control must visit spatial candidates")
	}
	npc := i10Observer(2, .2, .2, .2)
	npc.Visual = false
	for _, observers := range [][]StreamedPageObserver{{visual, npc}, {visual}} {
		next := i10Select(t, state, observers...)
		if next.PageCandidateVisits != first.PageCandidateVisits {
			t.Errorf("unchanged visual demand repeated spatial queries: visits %d, initial %d", next.PageCandidateVisits, first.PageCandidateVisits)
		}
		if !reflect.DeepEqual(first.Desired, next.Desired) || !reflect.DeepEqual(first.Keep, next.Keep) || !reflect.DeepEqual(first.Prefetch, next.Prefetch) || !reflect.DeepEqual(first.Requests, next.Requests) {
			t.Fatal("nonvisual roster change altered visual demand")
		}
	}
}
