package hl1

import "testing"

func TestParseEntities(t *testing.T) {
	entities, err := ParseEntities(`
{
"classname" "worldspawn"
"wad" "\quiver\valve\halflife.wad"
}
{
"classname" "info_player_start"
"origin" "128 64 32"
"angle" "90"
}`)
	if err != nil {
		t.Fatalf("ParseEntities failed: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("got %d entities, want 2", len(entities))
	}
	if got := entities[1].ClassName(); got != "info_player_start" {
		t.Fatalf("classname = %q", got)
	}
	if got := entities[0].Value("wad"); got != `\quiver\valve\halflife.wad` {
		t.Fatalf("wad path = %q", got)
	}
	origin, ok := parseVec3(entities[1].Value("origin"))
	if !ok {
		t.Fatalf("origin did not parse")
	}
	world := HammerToGekko(origin)
	if world.X != 128*HammerUnitMeters || world.Y != 32*HammerUnitMeters || world.Z != -64*HammerUnitMeters {
		t.Fatalf("HammerToGekko = %+v", world)
	}
}

func TestParseEntitiesRejectsMalformedInput(t *testing.T) {
	if _, err := ParseEntities(`{"classname"`); err == nil {
		t.Fatalf("ParseEntities succeeded for malformed input")
	}
}
