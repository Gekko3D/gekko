package gekko

import (
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestI10PageControlCannotIgnoreReferencedLegacyTerrainSource(t *testing.T) {
	level, terrain, poi := i10PageFixture()
	state := i10Configure(t, terrain, poi, i10Profile())
	observer := i10Observer(1, .2, .2, .2)
	before := i10Select(t, state, observer)
	// A source-only level terrain is an actual render layer in the legacy
	// runtime, not absent terrain. A v3-only control plane must not silently
	// authorize startup using just the provided POI roots.
	level.Terrain = &content.LevelTerrainDef{Kind: content.TerrainKindHeightfield, SourcePath: "authored.gkterrain"}
	if err := state.ConfigurePageControlPlane(level, nil, poi, i10Profile()); err == nil {
		t.Fatal("referenced source-only terrain was silently omitted from page ownership")
	}
	if after := i10Select(t, state, observer); !reflect.DeepEqual(before, after) {
		t.Fatal("rejected incomplete layer configuration replaced previous demand")
	}
}
