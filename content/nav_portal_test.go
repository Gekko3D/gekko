package content

import "testing"

func TestApplyNavTilePortalsConnectsPolygonsOverlappingChunkBoundary(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{25.6, 1, 25.6},
		Vertices: []Vec3{
			{22.8, 0, 0},
			{25.8, 0, 0},
			{25.8, 0, 25.8},
			{22.8, 0, 25.8},
		},
		Polygons: []NavPolygonDef{{
			ID:       "left_boundary",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{25.6, 0, 0},
		BoundsMax:      [3]float32{51.2, 1, 25.6},
		Vertices: []Vec3{
			{25.6, 0, 0},
			{26.2, 0, 0},
			{26.2, 0, 25.8},
			{25.6, 0, 25.8},
		},
		Polygons: []NavPolygonDef{{
			ID:       "right_boundary",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 1 || len(right.Portals) != 1 {
		t.Fatalf("expected bidirectional boundary portals, left=%+v right=%+v", left.Portals, right.Portals)
	}
	portal := left.Portals[0]
	if portal.ToTileCoord != right.Coord || portal.ToPolygonID != "right_boundary" {
		t.Fatalf("unexpected portal target: %+v", portal)
	}
	if portal.Start[0] != left.BoundsMax[0] || portal.End[0] != left.BoundsMax[0] {
		t.Fatalf("expected portal segment on shared tile boundary, got %+v", portal)
	}
	if portal.Start[2] != 0 || portal.End[2] < 25.599 {
		t.Fatalf("expected portal to span overlapping seam interval, got %+v", portal)
	}
}

func TestApplyNavTilePortalsConnectsCrossTileRampBoundary(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{4, 2, 4},
		Vertices: []Vec3{
			{0, 0, 0},
			{4, 0, 0},
			{4, 0.4, 4},
			{0, 0.4, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "left_ramp",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{4, 0, 0},
		BoundsMax:      [3]float32{8, 2, 4},
		Vertices: []Vec3{
			{4, 0, 0},
			{8, 0, 0},
			{8, 0.4, 4},
			{4, 0.4, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "right_ramp",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 1 || len(right.Portals) != 1 {
		t.Fatalf("expected bidirectional ramp portals, left=%+v right=%+v", left.Portals, right.Portals)
	}
	portal := left.Portals[0]
	if !navAlmostEqual(portal.Start[1], 0, 1e-4) || !navAlmostEqual(portal.End[1], 0.4, 1e-4) {
		t.Fatalf("expected ramp portal to follow seam height, got %+v", portal)
	}
}

func TestApplyNavTilePortalsUsesBoundarySpansInsteadOfBoundsGap(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{4, 2, 4},
		Vertices: []Vec3{
			{0, 0, 0},
			{4, 0, 0},
			{4, 0, 1},
			{3, 0, 1},
			{3, 0, 3},
			{4, 0, 3},
			{4, 0, 4},
			{0, 0, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "left_with_seam_gap",
			Vertices: []int{0, 1, 2, 3, 4, 5, 6, 7},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
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
			ID:       "right_full_edge",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 2 || len(right.Portals) != 2 {
		t.Fatalf("expected two split seam portals, left=%+v right=%+v", left.Portals, right.Portals)
	}
	first := left.Portals[0]
	second := left.Portals[1]
	if first.Start[2] > second.Start[2] {
		first, second = second, first
	}
	if !navAlmostEqual(first.Start[2], 0, 1e-4) || !navAlmostEqual(first.End[2], 1, 1e-4) {
		t.Fatalf("expected first portal to cover only lower seam span, got %+v", first)
	}
	if !navAlmostEqual(second.Start[2], 3, 1e-4) || !navAlmostEqual(second.End[2], 4, 1e-4) {
		t.Fatalf("expected second portal to cover only upper seam span, got %+v", second)
	}
}

func TestApplyNavTilePortalsKeepsDirectAndCrossingBoundarySpans(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{4, 2, 5},
		Vertices: []Vec3{
			{0, 0, 0},
			{4, 0, 0},
			{4, 0, 1},
			{3, 0, 1},
			{3, 0, 3},
			{5, 0, 3},
			{5, 0, 4},
			{3, 0, 4},
			{3, 0, 5},
			{0, 0, 5},
		},
		Polygons: []NavPolygonDef{{
			ID:       "left_direct_and_crossing",
			Vertices: []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{4, 0, 0},
		BoundsMax:      [3]float32{8, 2, 5},
		Vertices: []Vec3{
			{4, 0, 0},
			{8, 0, 0},
			{8, 0, 5},
			{4, 0, 5},
		},
		Polygons: []NavPolygonDef{{
			ID:       "right_full_edge",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 2 || len(right.Portals) != 2 {
		t.Fatalf("expected direct and crossing seam spans to portal, left=%+v right=%+v", left.Portals, right.Portals)
	}
	first := left.Portals[0]
	second := left.Portals[1]
	if first.Start[2] > second.Start[2] {
		first, second = second, first
	}
	if !navAlmostEqual(first.Start[2], 0, 1e-4) || !navAlmostEqual(first.End[2], 1, 1e-4) {
		t.Fatalf("expected direct boundary span 0..1, got %+v", first)
	}
	if !navAlmostEqual(second.Start[2], 3, 1e-4) || !navAlmostEqual(second.End[2], 4, 1e-4) {
		t.Fatalf("expected crossing boundary span 3..4, got %+v", second)
	}
}

func TestApplyNavTilePortalsConnectsCrossTileStepWithinAgentProfile(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
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
			ID:       "lower_step",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{4, 0, 0},
		BoundsMax:      [3]float32{8, 2, 4},
		Vertices: []Vec3{
			{4, 0.25, 0},
			{8, 0.25, 0},
			{8, 0.25, 4},
			{4, 0.25, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "upper_step",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 1 || len(right.Portals) != 1 {
		t.Fatalf("expected bidirectional step portals, left=%+v right=%+v", left.Portals, right.Portals)
	}
	portal := left.Portals[0]
	if portal.Start[1] <= 0 || portal.End[1] <= 0 {
		t.Fatalf("expected portal height between lower and upper step, got %+v", portal)
	}
	if portal.ToTileCoord != right.Coord || portal.ToPolygonID != "upper_step" {
		t.Fatalf("unexpected step portal target: %+v", portal)
	}
}

func TestApplyNavTilePortalsConnectsCrossTileStepByStepHeightWhenSlopeWouldReject(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
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
			ID:       "lower_step",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{4, 0, 0},
		BoundsMax:      [3]float32{8, 2, 4},
		Vertices: []Vec3{
			{4, 0.4, 0},
			{8, 0.4, 0},
			{8, 0.4, 4},
			{4, 0.4, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "upper_step",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 1 || len(right.Portals) != 1 {
		t.Fatalf("expected step-height portal despite slope-sized raster cell, left=%+v right=%+v", left.Portals, right.Portals)
	}
}

func TestApplyNavTilePortalsConnectsHorizontalNeighborAcrossExactYBoundary(t *testing.T) {
	lower := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{0, 0, 0},
		BoundsMax:      [3]float32{8, 8, 8},
		Vertices: []Vec3{
			{7, 8, 1},
			{8, 8, 1},
			{8, 8, 3},
			{7, 8, 3},
		},
		Polygons: []NavPolygonDef{{
			ID:       "voxel_top_boundary",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	upperRight := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 1, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{8, 8, 0},
		BoundsMax:      [3]float32{16, 16, 8},
		Vertices: []Vec3{
			{8, 8, 1},
			{9, 8, 1},
			{9, 8, 3},
			{8, 8, 3},
		},
		Polygons: []NavPolygonDef{{
			ID:       "source_exact_boundary",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{lower, upperRight}, navPortalTestProfiles())

	if len(lower.Portals) != 1 || len(upperRight.Portals) != 1 {
		t.Fatalf("expected mixed Y-owner horizontal boundary portals, lower=%+v upperRight=%+v", lower.Portals, upperRight.Portals)
	}
	if lower.Portals[0].ToTileCoord != upperRight.Coord || lower.Portals[0].ToPolygonID != "source_exact_boundary" {
		t.Fatalf("unexpected mixed-owner portal target: %+v", lower.Portals[0])
	}
}

func TestApplyNavTilePortalsRejectsCrossTileStepAboveAgentProfile(t *testing.T) {
	left := &NavTileDef{
		NavID:          "nav-demo",
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
			ID:       "lower_step",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}
	right := &NavTileDef{
		NavID:          "nav-demo",
		Coord:          TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
		AgentProfileID: DefaultNavAgentProfileID,
		BoundsMin:      [3]float32{4, 0, 0},
		BoundsMax:      [3]float32{8, 2, 4},
		Vertices: []Vec3{
			{4, 0.8, 0},
			{8, 0.8, 0},
			{8, 0.8, 4},
			{4, 0.8, 4},
		},
		Polygons: []NavPolygonDef{{
			ID:       "upper_step",
			Vertices: []int{0, 1, 2, 3},
			Area:     NavTraversalWalk,
		}},
	}

	applyNavTilePortals([]*NavTileDef{left, right}, navPortalTestProfiles())

	if len(left.Portals) != 0 || len(right.Portals) != 0 {
		t.Fatalf("expected tall step to be rejected, left=%+v right=%+v", left.Portals, right.Portals)
	}
}

func navPortalTestProfiles() map[string]NavAgentProfileDef {
	profile := DefaultHL1NavAgentProfile()
	profile.StepHeight = 0.5
	profile.NavCellSize = 0.5
	profile.MaxSlopeDegrees = 45
	EnsureNavAgentProfileDefaults(&profile)
	return map[string]NavAgentProfileDef{profile.ID: profile}
}
