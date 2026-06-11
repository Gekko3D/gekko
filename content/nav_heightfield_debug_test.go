package content

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildNavVoxelHeightfieldDebugReportsAcceptedAndRejectedSpans(t *testing.T) {
	chunk := navTestChunk(4, 1,
		ImportedWorldVoxelDef{X: 0, Y: 0, Z: 0, Value: 1},
		ImportedWorldVoxelDef{X: 1, Y: 0, Z: 0, Value: 1},
		ImportedWorldVoxelDef{X: 1, Y: 1, Z: 0, Value: 1},
	)
	profile := navTestAgentProfile(0.1, 1.0, 0.5)

	debug, err := BuildNavVoxelHeightfieldDebug(chunk, profile, NavVoxelHeightfieldDebugOptions{})
	if err != nil {
		t.Fatalf("BuildNavVoxelHeightfieldDebug failed: %v", err)
	}

	if debug.Summary.OccupiedVoxels != 3 || debug.Summary.CandidateSpans != 3 {
		t.Fatalf("unexpected summary: %+v", debug.Summary)
	}
	if debug.Summary.AcceptedSpans == 0 || debug.Summary.RejectedSpans == 0 {
		t.Fatalf("expected accepted and rejected spans, got %+v", debug.Summary)
	}
	if debug.Summary.CompactCells == 0 || debug.Summary.Regions == 0 {
		t.Fatalf("expected compact cells and regions, got %+v", debug.Summary)
	}
	if len(debug.Regions) == 0 || debug.Regions[0].Area == "" {
		t.Fatalf("expected debug regions to include traversal area, got %+v", debug.Regions)
	}
	var blockedReason string
	for _, span := range debug.Spans {
		if span.X == 1 && span.SolidY == 0 && span.Z == 0 {
			blockedReason = span.RejectReason
		}
	}
	if !strings.HasPrefix(blockedReason, "blocked_clearance:") {
		t.Fatalf("expected blocked clearance reason for covered floor span, got %q", blockedReason)
	}
}

func TestBuildNavVoxelHeightfieldDebugFromManifestLoadsNeighborContext(t *testing.T) {
	dir := t.TempDir()
	worldPath := filepath.Join(dir, "world.gkworld")
	leftPath := filepath.Join(dir, "chunks", "left.gkchunk")
	rightPath := filepath.Join(dir, "chunks", "right.gkchunk")
	left := navTestChunk(4, 1, ImportedWorldVoxelDef{X: 3, Y: 0, Z: 1, Value: 1})
	right := navTestChunk(4, 1, ImportedWorldVoxelDef{X: 0, Y: 0, Z: 1, Value: 1})
	left.WorldID = "debug-world"
	right.WorldID = "debug-world"
	left.Coord = TerrainChunkCoordDef{X: 0, Y: 0, Z: 0}
	right.Coord = TerrainChunkCoordDef{X: 1, Y: 0, Z: 0}
	if err := SaveImportedWorldChunk(leftPath, left); err != nil {
		t.Fatalf("SaveImportedWorldChunk left failed: %v", err)
	}
	if err := SaveImportedWorldChunk(rightPath, right); err != nil {
		t.Fatalf("SaveImportedWorldChunk right failed: %v", err)
	}
	world := &ImportedWorldDef{
		WorldID:         "debug-world",
		SchemaVersion:   CurrentImportedWorldSchemaVersion,
		Kind:            ImportedWorldKindVoxelWorld,
		ChunkSize:       4,
		VoxelResolution: 1,
		Entries: []ImportedWorldChunkEntryDef{
			{Coord: left.Coord, ChunkPath: AuthorDocumentPath(leftPath, worldPath), NonEmptyVoxelCount: 1},
			{Coord: right.Coord, ChunkPath: AuthorDocumentPath(rightPath, worldPath), NonEmptyVoxelCount: 1},
		},
	}
	if err := SaveImportedWorld(worldPath, world); err != nil {
		t.Fatalf("SaveImportedWorld failed: %v", err)
	}

	debug, err := BuildNavVoxelHeightfieldDebugFromImportedWorldManifestPath(worldPath, left.Coord, navTestAgentProfile(0.6, 1.0, 1.0))
	if err != nil {
		t.Fatalf("BuildNavVoxelHeightfieldDebugFromImportedWorldManifestPath failed: %v", err)
	}
	if len(debug.LoadedNeighborTiles) != 1 || debug.LoadedNeighborTiles[0] != right.Coord {
		t.Fatalf("expected right neighbor to be loaded, got %+v", debug.LoadedNeighborTiles)
	}
}
