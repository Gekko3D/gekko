package gekko

import (
	"path/filepath"
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestRuntimeNavigationServiceUnavailableReportsNotAttempted(t *testing.T) {
	service := RuntimeNavigationService{}

	route, err := service.FindRoute(RuntimeNavigationRouteRequest{
		Start: content.Vec3{0, 0, 0},
		End:   content.Vec3{1, 0, 0},
	})
	if err != nil {
		t.Fatalf("FindRoute failed: %v", err)
	}
	if route.Found || route.RefinementStatus != content.NavRouteRefinementStatusNotAttempted || route.RefinementReason != content.NavRouteRefinementReasonNavigationUnavailable {
		t.Fatalf("expected navigation unavailable result, got %+v", route)
	}
}

func TestRuntimeNavigationServiceFindRouteWrapsHierarchicalNav(t *testing.T) {
	root := t.TempDir()
	navPath := filepath.Join(root, "worlds", "route.gknav")
	leftCoord := content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	rightCoord := content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	leftPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_0_0_0.gknavtile")
	rightPath := filepath.Join(root, "worlds", "route_navtiles", "tiny_1_0_0.gknavtile")
	if err := content.SaveNavTile(leftPath, runtimeNavServiceTestTile("nav-route", "tiny", leftCoord, "left", content.Vec3{7, 1, 0}, content.Vec3{8, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile left failed: %v", err)
	}
	if err := content.SaveNavTile(rightPath, runtimeNavServiceTestTile("nav-route", "tiny", rightCoord, "right", content.Vec3{8, 1, 0}, content.Vec3{9, 1, 1})); err != nil {
		t.Fatalf("SaveNavTile right failed: %v", err)
	}
	manifest := runtimeNavServiceTestManifest("nav-route", "tiny", navPath, map[content.TerrainChunkCoordDef]string{
		leftCoord:  leftPath,
		rightCoord: rightPath,
	})
	service := NewRuntimeNavigationService(manifest, navPath, nil, "")

	route, err := service.FindRoute(RuntimeNavigationRouteRequest{
		Start: content.Vec3{7.5, 1, 0.5},
		End:   content.Vec3{8.5, 1, 0.5},
		Options: content.NavHierarchicalRouteOptions{
			LocalPath: content.NavPathOptions{AgentProfileID: "tiny", MaxTileSearchRadius: 1},
		},
	})
	if err != nil {
		t.Fatalf("FindRoute failed: %v", err)
	}
	if !route.Found || !route.Refined || route.RefinementStatus != content.NavRouteRefinementStatusRefined {
		t.Fatalf("expected refined runtime navigation route, got %+v", route)
	}
	if len(route.SectorPath.SectorCoords) != 2 || len(route.LocalPath.Steps) != 2 {
		t.Fatalf("expected sector and local path steps, got %+v", route)
	}
}

func TestRuntimeNavigationServiceFromStreamedLevelStateSnapshotsNavInputs(t *testing.T) {
	manifest := &content.NavManifestDef{
		NavID:           "nav-state",
		SchemaVersion:   content.CurrentNavManifestSchemaVersion,
		BuilderVersion:  content.DefaultNavBuilderVersion,
		ChunkSize:       8,
		VoxelResolution: 1,
		AgentProfiles:   []content.NavAgentProfileDef{content.DefaultHL1NavAgentProfile()},
	}
	delta := &content.WorldDeltaDef{LevelID: "level-state"}
	state := &StreamedLevelRuntimeState{
		BaseNavManifestPath: "worlds/state.gknav",
		BaseNavManifest:     manifest,
		WorldDeltaPath:      "levels/state.gkworlddelta",
		WorldDelta:          delta,
		NavigationRevision:  42,
	}

	service := RuntimeNavigationServiceFromStreamedLevelState(state)
	if !service.Available() || service.BaseNavManifest != manifest || service.BaseNavManifestPath != "worlds/state.gknav" || service.WorldDelta != delta || service.WorldDeltaPath != "levels/state.gkworlddelta" || service.NavigationRevision != 42 {
		t.Fatalf("unexpected runtime navigation service snapshot: %+v", service)
	}
}

func runtimeNavServiceTestManifest(navID string, profileID string, navPath string, tilePaths map[content.TerrainChunkCoordDef]string) *content.NavManifestDef {
	manifest := &content.NavManifestDef{
		NavID:           navID,
		SchemaVersion:   content.CurrentNavManifestSchemaVersion,
		BuilderVersion:  content.DefaultNavBuilderVersion,
		ChunkSize:       8,
		VoxelResolution: 1,
		AgentProfiles: []content.NavAgentProfileDef{{
			ID:              profileID,
			Radius:          0.2,
			Height:          1,
			StepHeight:      0.5,
			MaxSlopeDegrees: 45,
		}},
		Sectors: []content.NavSectorEntryDef{
			{
				Coord:     content.TerrainChunkCoordDef{X: 0, Y: 0, Z: 0},
				BoundsMin: [3]float32{0, 0, 0},
				BoundsMax: [3]float32{8, 8, 8},
			},
			{
				Coord:     content.TerrainChunkCoordDef{X: 1, Y: 0, Z: 0},
				BoundsMin: [3]float32{8, 0, 0},
				BoundsMax: [3]float32{16, 8, 8},
			},
		},
	}
	for coord, path := range tilePaths {
		manifest.Tiles = append(manifest.Tiles, content.NavTileEntryDef{
			Coord:          coord,
			AgentProfileID: profileID,
			TilePath:       content.AuthorDocumentPath(path, navPath),
			BoundsMin:      [3]float32{float32(coord.X) * 8, float32(coord.Y) * 8, float32(coord.Z) * 8},
			BoundsMax:      [3]float32{float32(coord.X+1) * 8, float32(coord.Y+1) * 8, float32(coord.Z+1) * 8},
		})
	}
	return manifest
}

func runtimeNavServiceTestTile(navID string, profileID string, coord content.TerrainChunkCoordDef, polygonID string, min content.Vec3, max content.Vec3) *content.NavTileDef {
	return &content.NavTileDef{
		NavID:          navID,
		SchemaVersion:  content.CurrentNavTileSchemaVersion,
		Coord:          coord,
		AgentProfileID: profileID,
		BuilderVersion: content.DefaultNavBuilderVersion,
		PayloadKind:    content.NavTilePayloadJSONV1,
		BoundsMin:      [3]float32{float32(coord.X) * 8, float32(coord.Y) * 8, float32(coord.Z) * 8},
		BoundsMax:      [3]float32{float32(coord.X+1) * 8, float32(coord.Y+1) * 8, float32(coord.Z+1) * 8},
		Vertices: []content.Vec3{
			{min[0], min[1], min[2]},
			{max[0], min[1], min[2]},
			{max[0], min[1], max[2]},
			{min[0], min[1], max[2]},
		},
		Polygons: []content.NavPolygonDef{{
			ID:       polygonID,
			Vertices: []int{0, 1, 2, 3},
			Area:     content.NavTraversalWalk,
		}},
	}
}
