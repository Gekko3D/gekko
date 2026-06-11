package content

import (
	"path/filepath"
	"testing"
)

func TestBuildNavClearanceSourceTileFiltersSameGapForDifferentAgentRadii(t *testing.T) {
	voxels := navTestFloorVoxels(1, 5, 1, 3, 0)
	for x := 1; x <= 5; x++ {
		voxels = append(voxels,
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 1, Value: 1},
			ImportedWorldVoxelDef{X: x, Y: 1, Z: 3, Value: 1},
		)
	}
	chunk := navTestChunk(7, 1.0, voxels...)

	result, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-clearance-gap",
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	cell, ok := navClearanceSourceTestCell(result.Tile, 3, 1, 2)
	if !ok {
		t.Fatalf("expected source cell in one-voxel gap, got %+v", result.Tile.Cells)
	}
	if cell.ClearanceRadius < 0.49 || cell.ClearanceRadius > 0.51 {
		t.Fatalf("expected one-voxel gap clearance around 0.5, got %.3f cell=%+v", cell.ClearanceRadius, cell)
	}
	if !NavClearanceSourceCellSupportsAgent(cell, navTestAgentProfile(0.2, 1.0, 1.0)) {
		t.Fatalf("expected small agent to fit through shared clearance source cell: %+v", cell)
	}
	if NavClearanceSourceCellSupportsAgent(cell, navTestAgentProfile(0.6, 1.0, 1.0)) {
		t.Fatalf("did not expect large agent to fit through shared clearance source cell: %+v", cell)
	}
}

func TestBuildNavClearanceSourceTileFiltersSameCellForDifferentAgentHeights(t *testing.T) {
	chunk := navTestChunk(5, 1.0,
		ImportedWorldVoxelDef{X: 2, Y: 0, Z: 2, Value: 1},
		ImportedWorldVoxelDef{X: 2, Y: 2, Z: 2, Value: 1},
	)

	result, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-clearance-headroom",
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	cell, ok := navClearanceSourceTestCell(result.Tile, 2, 1, 2)
	if !ok {
		t.Fatalf("expected source cell under ceiling, got %+v", result.Tile.Cells)
	}
	if cell.Headroom != 1.0 {
		t.Fatalf("expected one voxel of headroom, got %.3f cell=%+v", cell.Headroom, cell)
	}
	if !NavClearanceSourceCellSupportsAgent(cell, navTestAgentProfile(0.2, 1.0, 1.0)) {
		t.Fatalf("expected short agent to fit under ceiling: %+v", cell)
	}
	if NavClearanceSourceCellSupportsAgent(cell, navTestAgentProfile(0.2, 1.1, 1.0)) {
		t.Fatalf("did not expect tall agent to fit under ceiling: %+v", cell)
	}
}

func TestSaveLoadNavClearanceSourceTileRoundTripsDefaults(t *testing.T) {
	chunk := navTestChunk(4, 1.0, ImportedWorldVoxelDef{X: 1, Y: 0, Z: 1, Value: 1})
	result, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{NavID: "nav-clearance-io"})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	path := filepath.Join(t.TempDir(), "tile.gknavsource")
	if err := SaveNavClearanceSourceTile(path, result.Tile); err != nil {
		t.Fatalf("SaveNavClearanceSourceTile failed: %v", err)
	}
	loaded, err := LoadNavClearanceSourceTile(path)
	if err != nil {
		t.Fatalf("LoadNavClearanceSourceTile failed: %v", err)
	}
	if loaded.SchemaVersion != CurrentNavClearanceSourceTileSchemaVersion || loaded.Kind != NavClearanceSourceKindVoxelSpans || loaded.PayloadKind != NavClearanceSourceTilePayloadJSONV1 {
		t.Fatalf("expected clearance source defaults to round-trip, got %+v", loaded)
	}
	if len(loaded.Cells) != len(result.Tile.Cells) || len(loaded.Cells) == 0 {
		t.Fatalf("expected clearance source cells to round-trip, saved=%+v loaded=%+v", result.Tile.Cells, loaded.Cells)
	}
}

func TestBuildNavTileFromClearanceSourceFiltersProfileCells(t *testing.T) {
	chunk := navTestChunk(5, 1.0, navTestFloorVoxels(1, 3, 1, 3, 0)...)
	profile := navTestAgentProfile(0.2, 1.0, 1.0)
	sourceResult, err := BuildNavClearanceSourceTileFromImportedWorldChunk(chunk, NavClearanceSourceTileBuildOptions{
		NavID:              "nav-clearance-derived",
		MaxClearanceRadius: 1.0,
	})
	if err != nil {
		t.Fatalf("BuildNavClearanceSourceTileFromImportedWorldChunk failed: %v", err)
	}
	for i := range sourceResult.Tile.Cells {
		if sourceResult.Tile.Cells[i].X == 2 && sourceResult.Tile.Cells[i].Y == 1 && sourceResult.Tile.Cells[i].Z == 2 {
			sourceResult.Tile.Cells[i].ClearanceRadius = 0.1
		}
	}

	derived, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{
		ClearanceSource: sourceResult.Tile,
	})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk with source failed: %v", err)
	}
	if navTileHasCell(derived.Tile, 2, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("did not expect source-filtered low-clearance cell in derived profile tile, got %+v", derived.Tile.Polygons)
	}
	if !navTileHasCell(derived.Tile, 1, 1, 1, chunk.VoxelResolution) {
		t.Fatalf("expected other source-supported cells to remain walkable, got %+v", derived.Tile.Polygons)
	}

	voxelOnly, err := BuildNavTileFromImportedWorldChunk(chunk, profile, NavTileBuildOptions{})
	if err != nil {
		t.Fatalf("BuildNavTileFromImportedWorldChunk voxel-only failed: %v", err)
	}
	if !navTileHasCell(voxelOnly.Tile, 2, 1, 2, chunk.VoxelResolution) {
		t.Fatalf("expected voxel-only build to include open floor center cell, got %+v", voxelOnly.Tile.Polygons)
	}
}

func navClearanceSourceTestCell(tile *NavClearanceSourceTileDef, x, y, z int) (NavClearanceSourceCellDef, bool) {
	if tile == nil {
		return NavClearanceSourceCellDef{}, false
	}
	for _, cell := range tile.Cells {
		if cell.X == x && cell.Y == y && cell.Z == z {
			return cell, true
		}
	}
	return NavClearanceSourceCellDef{}, false
}
