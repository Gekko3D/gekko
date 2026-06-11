package content

import "testing"

func TestValidateNavTileTopologyAcceptsReciprocalBoundaryPortals(t *testing.T) {
	left, right := navValidationTopologyTestTiles()
	applyNavTilePortals([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())

	result := ValidateNavTileTopology([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	if result.HasErrors() {
		t.Fatalf("expected reciprocal boundary portals to validate, got %+v", result.Issues)
	}
}

func TestValidateNavTileTopologyRejectsMissingTargetTile(t *testing.T) {
	left, right := navValidationTopologyTestTiles()
	applyNavTilePortals([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())

	result := ValidateNavTileTopology([]*NavTileDef{left}, navValidationTopologyTestProfiles())
	if !result.HasErrors() {
		t.Fatal("expected topology validation to reject missing portal target tile")
	}
	assertHasNavValidationCode(t, result, "missing_portal_target_tile")
}

func TestValidateNavTileTopologyRejectsMissingTargetPolygon(t *testing.T) {
	left, right := navValidationTopologyTestTiles()
	applyNavTilePortals([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	left.Portals[0].ToPolygonID = "missing"

	result := ValidateNavTileTopology([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	if !result.HasErrors() {
		t.Fatal("expected topology validation to reject missing portal target polygon")
	}
	assertHasNavValidationCode(t, result, "missing_portal_to_polygon")
}

func TestValidateNavTileTopologyRejectsOneWayPortal(t *testing.T) {
	left, right := navValidationTopologyTestTiles()
	applyNavTilePortals([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	right.Portals = nil

	result := ValidateNavTileTopology([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	if !result.HasErrors() {
		t.Fatal("expected topology validation to reject one-way portal")
	}
	assertHasNavValidationCode(t, result, "missing_reciprocal_portal")
}

func TestValidateNavTileTopologyRejectsInvalidPortalBoundarySegment(t *testing.T) {
	left, right := navValidationTopologyTestTiles()
	applyNavTilePortals([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	left.Portals[0].Start[0] = 3.5

	result := ValidateNavTileTopology([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	if !result.HasErrors() {
		t.Fatal("expected topology validation to reject off-boundary portal segment")
	}
	assertHasNavValidationCode(t, result, "invalid_portal_boundary_segment")
}

func TestValidateNavTileTopologyRejectsInvalidPortalHeight(t *testing.T) {
	left, right := navValidationTopologyTestTiles()
	applyNavTilePortals([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	left.Portals[0].Start[1] = 0.5

	result := ValidateNavTileTopology([]*NavTileDef{left, right}, navValidationTopologyTestProfiles())
	if !result.HasErrors() {
		t.Fatal("expected topology validation to reject invalid portal height")
	}
	assertHasNavValidationCode(t, result, "invalid_portal_height")
}

func navValidationTopologyTestTiles() (*NavTileDef, *NavTileDef) {
	left := &NavTileDef{
		NavID:          "nav-topology-test",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{4, 2, 4},
		Vertices: []Vec3{
			{0, 0, 0},
			{4, 0, 0},
			{4, 0, 4},
			{0, 0, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "left",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-topology-test",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{4, 0, 0},
		BoundsMax:      [3]float32{8, 2, 4},
		Vertices: []Vec3{
			{4, 0, 0},
			{8, 0, 0},
			{8, 0, 4},
			{4, 0, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "right",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	return left, right
}

func navValidationTopologyTestProfiles() map[string]NavAgentProfileDef {
	return map[string]NavAgentProfileDef{
		DefaultNavAgentProfileID: DefaultHL1NavAgentProfile(),
	}
}
