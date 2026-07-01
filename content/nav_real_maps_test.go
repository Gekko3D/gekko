package content

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type realMapNavFixture struct {
	Name          string
	MapName       string
	Start         Vec3
	End           Vec3
	MinSteps      int
	WantStartSnap bool
	WantEndSnap   bool
}

func TestRealMapNavPathFixtures(t *testing.T) {
	fixtures := []realMapNavFixture{
		{
			Name:     "crossfire_large_field_across_center_seam",
			MapName:  "crossfire",
			Start:    Vec3{-17.07, -43.00, 30.77},
			End:      Vec3{11.00, -43.00, 20.87},
			MinSteps: 3,
		},
		{
			Name:          "crossfire_endpoint_snap_near_center_seam",
			MapName:       "crossfire",
			Start:         Vec3{-0.15, -42.20, 16.13},
			End:           Vec3{0.15, -43.00, 16.13},
			MinSteps:      2,
			WantStartSnap: true,
		},
		{
			Name:          "gasworks_large_field_across_center_seam",
			MapName:       "gasworks",
			Start:         Vec3{-12.80, 0.10, 7.35},
			End:           Vec3{9.15, 0.10, 7.35},
			MinSteps:      2,
			WantStartSnap: true,
			WantEndSnap:   true,
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			manifest, manifestPath := loadRealMapNavManifestOrSkip(t, fixture.MapName)
			path, err := FindEffectiveNavPath(manifest, manifestPath, nil, "", fixture.Start, fixture.End, NavPathOptions{
				AgentProfileID:       DefaultNavAgentProfileID,
				MaxTileSearchRadius:  16,
				EndpointSnapDistance: 1.5,
			})
			if err != nil {
				t.Fatalf("FindEffectiveNavPath failed: %v", err)
			}
			if !path.Found {
				t.Fatalf("expected path to be found, reason=%s coord=%s result=%+v", path.FailureReason, TerrainChunkKey(path.FailureCoord), path)
			}
			if len(path.Steps) < fixture.MinSteps {
				t.Fatalf("expected at least %d corridor steps, got %d: %+v", fixture.MinSteps, len(path.Steps), path)
			}
			if len(path.Waypoints) < 2 {
				t.Fatalf("expected at least start/end movement waypoints, got %+v", path)
			}
			if path.StartSnapped != fixture.WantStartSnap {
				t.Fatalf("expected start snap=%t, got %t distance=%.3f result=%+v", fixture.WantStartSnap, path.StartSnapped, path.StartSnapDistance, path)
			}
			if path.EndSnapped != fixture.WantEndSnap {
				t.Fatalf("expected end snap=%t, got %t distance=%.3f result=%+v", fixture.WantEndSnap, path.EndSnapped, path.EndSnapDistance, path)
			}
			for _, portal := range path.Portals {
				if portal.Width <= 0 || portal.RequiredWidth <= 0 || portal.ClearanceReason == "" {
					t.Fatalf("expected portal clearance metadata, got %+v in result %+v", portal, path)
				}
			}
		})
	}
}

func TestRealMapCrossfireNavAvoidsPrimaryHL1SourcePolygons(t *testing.T) {
	manifest, manifestPath := loadRealMapNavManifestOrSkip(t, "crossfire")
	voxelDerived := 0
	for _, entry := range manifest.Tiles {
		tile, err := LoadNavTile(ResolveNavTilePath(entry, manifestPath))
		if err != nil {
			t.Fatalf("LoadNavTile %s failed: %v", TerrainChunkKey(entry.Coord), err)
		}
		for _, polygon := range tile.Polygons {
			if strings.HasPrefix(polygon.ID, "recast:") || strings.HasPrefix(polygon.ID, "raster_region:") || strings.HasPrefix(polygon.ID, "raster_cell:") || strings.HasPrefix(polygon.ID, "contour:") || strings.HasPrefix(polygon.ID, "voxel_cell:") {
				voxelDerived++
			}
			if strings.HasPrefix(polygon.ID, "surface:hl1_") {
				t.Fatalf("expected Crossfire production nav to avoid primary HL1 source polygons, got %q in tile %s", polygon.ID, TerrainChunkKey(entry.Coord))
			}
		}
	}
	if voxelDerived == 0 {
		t.Fatalf("expected Crossfire production nav to use voxel-heightfield polygons")
	}
}

func TestRealMapNavTraversalAreaFixtures(t *testing.T) {
	manifest, manifestPath := loadRealMapNavManifestOrSkip(t, "gasworks")
	counts := realMapNavAreaCounts(t, manifest, manifestPath)
	if counts[NavTraversalWalk] == 0 {
		t.Fatalf("expected gasworks to contain walk polygons, counts=%+v", counts)
	}
	if counts[NavTraversalRamp] == 0 {
		t.Fatalf("expected gasworks to contain ramp polygons, counts=%+v", counts)
	}
}

func TestRealMapNavPortalsReferenceRealPolygons(t *testing.T) {
	for _, mapName := range []string{"crossfire", "gasworks"} {
		t.Run(mapName, func(t *testing.T) {
			manifest, manifestPath := loadRealMapNavManifestOrSkip(t, mapName)
			tileByCoord := make(map[TerrainChunkCoordDef]*NavTileDef, len(manifest.Tiles))
			for _, entry := range manifest.Tiles {
				tile, err := LoadNavTile(ResolveNavTilePath(entry, manifestPath))
				if err != nil {
					t.Fatalf("LoadNavTile %s failed: %v", TerrainChunkKey(entry.Coord), err)
				}
				tileByCoord[entry.Coord] = tile
			}
			portalCount := 0
			for coord, tile := range tileByCoord {
				polygons := navTilePolygonIDSet(tile)
				for _, portal := range tile.Portals {
					portalCount++
					if _, ok := polygons[portal.FromPolygonID]; !ok {
						t.Fatalf("portal %q in tile %s references missing from polygon %q", portal.ID, TerrainChunkKey(coord), portal.FromPolygonID)
					}
					neighbor := tileByCoord[portal.ToTileCoord]
					if neighbor == nil {
						t.Fatalf("portal %q in tile %s references missing neighbor tile %s", portal.ID, TerrainChunkKey(coord), TerrainChunkKey(portal.ToTileCoord))
					}
					if _, ok := navTilePolygonIDSet(neighbor)[portal.ToPolygonID]; !ok {
						t.Fatalf("portal %q in tile %s references missing neighbor polygon %q in %s", portal.ID, TerrainChunkKey(coord), portal.ToPolygonID, TerrainChunkKey(portal.ToTileCoord))
					}
				}
			}
			if portalCount == 0 {
				t.Fatalf("expected %s to contain cross-tile portals", mapName)
			}
		})
	}
}

func loadRealMapNavManifestOrSkip(t *testing.T, mapName string) (*NavManifestDef, string) {
	t.Helper()
	manifestPath := filepath.Join(realMapAssetsRoot(t), "actiongame", "assets", "levels", mapName, "worlds", mapName+".gknav")
	if _, err := os.Stat(manifestPath); err != nil {
		if os.IsNotExist(err) {
			t.Skipf("real-map nav fixture %q is not present at %s", mapName, manifestPath)
		}
		t.Fatalf("stat real-map nav fixture %q failed: %v", mapName, err)
	}
	manifest, err := LoadNavManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadNavManifest %s failed: %v", manifestPath, err)
	}
	return manifest, manifestPath
}

func realMapAssetsRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func realMapNavAreaCounts(t *testing.T, manifest *NavManifestDef, manifestPath string) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, entry := range manifest.Tiles {
		tile, err := LoadNavTile(ResolveNavTilePath(entry, manifestPath))
		if err != nil {
			t.Fatalf("LoadNavTile %s failed: %v", TerrainChunkKey(entry.Coord), err)
		}
		for _, polygon := range tile.Polygons {
			counts[polygon.Area]++
		}
	}
	return counts
}

func navTilePolygonIDSet(tile *NavTileDef) map[string]struct{} {
	out := make(map[string]struct{}, len(tile.Polygons))
	for _, polygon := range tile.Polygons {
		out[polygon.ID] = struct{}{}
	}
	return out
}
