package gekko

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func s4aDelta() content.WorldDeltaDef {
	return content.WorldDeltaDef{
		SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: "s4a",
		PlacementTransformOverrides: []content.PlacementTransformOverrideDef{{PlacementID: "placement", Transform: content.LevelTransformDef{Position: content.Vec3{1, 2, 3}}}},
		PlacementDeletions:          []content.PlacementDeletionDef{{PlacementID: "deleted"}},
		TerrainChunkOverrides:       []content.TerrainChunkOverrideDef{{TerrainID: "terrain", SnapshotPath: "terrain.gkchunk"}},
		ImportedWorldChunkOverrides: []content.ImportedWorldChunkOverrideDef{{WorldID: "world", SnapshotPath: "imported.gkchunk"}},
		VoxelBackingRemovals:        []content.VoxelBackingRemovalDef{{OwnerKind: content.VoxelBackingOwnerImportedWorld, OwnerID: "world", Bricks: []content.VoxelBackingRemovalBrickDef{{Coord: [3]int{1, 2, 3}, Bits: [16]uint32{1}, Material: 4}}}},
		NavigationSourceOverrides:   []content.NavigationSourceOverrideDef{{NavID: "nav", SourceHash: "source"}},
		NavigationGraphOverrides:    []content.NavigationGraphOverrideDef{{NavID: "nav", AgentProfileID: "agent", TilePath: "graph"}},
		VoxelObjectOverrides:        []content.VoxelObjectOverrideDef{{PlacementID: "placement", ItemID: "item", SnapshotPath: "object.gkvoxobj"}},
	}
}

func s4aMutateDelta(delta *content.WorldDeltaDef) {
	delta.PlacementTransformOverrides[0].Transform.Position[0] = 9
	delta.PlacementDeletions[0].PlacementID = "changed"
	delta.TerrainChunkOverrides[0].SnapshotPath = "changed"
	delta.ImportedWorldChunkOverrides[0].SnapshotPath = "changed"
	delta.VoxelBackingRemovals[0].OwnerID = "changed"
	delta.VoxelBackingRemovals[0].Bricks[0].Bits[0] = 8
	delta.VoxelBackingRemovals[0].Bricks[0].Material = 7
	delta.NavigationSourceOverrides[0].SourceHash = "changed"
	delta.NavigationGraphOverrides[0].TilePath = "changed"
	delta.VoxelObjectOverrides[0].SnapshotPath = "changed"
}

func TestS4aWorkerManifestCaptureOwnsEveryMutableSlice(t *testing.T) {
	if got := copyWorldDeltaForNav(nil); !reflect.DeepEqual(got, content.WorldDeltaDef{}) {
		t.Fatal("nil manifest capture must be empty")
	}
	for _, direction := range []string{"source", "capture"} {
		t.Run(direction, func(t *testing.T) {
			source, expected := s4aDelta(), s4aDelta()
			captured := copyWorldDeltaForNav(&source)
			if !reflect.DeepEqual(captured, expected) {
				t.Fatal("worker capture lost manifest content")
			}
			if direction == "source" {
				s4aMutateDelta(&source)
				if !reflect.DeepEqual(captured, expected) {
					t.Fatal("live manifest edits changed captured worker content")
				}
			} else {
				s4aMutateDelta(&captured)
				if !reflect.DeepEqual(source, expected) {
					t.Fatal("captured manifest edits changed live content")
				}
			}
		})
	}
}

func s4aState(t *testing.T) (*App, *Commands, *StreamedLevelRuntimeState) {
	t.Helper()
	app, cmd, state := newStreamedRuntimeHarness(t)
	state.LevelID, state.Level = "s4a", content.NewLevelDef("s4a")
	state.Level.ChunkSize = 16
	state.WorldDeltaPath = filepath.Join(t.TempDir(), "world.gkworlddelta")
	state.WorldDataDir = content.DefaultWorldDeltaDataDir(state.WorldDeltaPath)
	state.WorldDelta = &content.WorldDeltaDef{SchemaVersion: content.CurrentWorldDeltaSchemaVersion, LevelID: "s4a"}
	return app, cmd, state
}

func s4aRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func s4aLoadDelta(t *testing.T, path string) *content.WorldDeltaDef {
	t.Helper()
	delta, err := content.LoadWorldDelta(path)
	if err != nil {
		t.Fatal(err)
	}
	return delta
}

func s4aPayloadPath(t *testing.T, delta *content.WorldDeltaDef, kind, deltaPath string) string {
	t.Helper()
	var authored string
	switch kind {
	case "terrain":
		if len(delta.TerrainChunkOverrides) != 1 {
			t.Fatal("missing terrain snapshot reference")
		}
		authored = delta.TerrainChunkOverrides[0].SnapshotPath
	case "imported":
		if len(delta.ImportedWorldChunkOverrides) != 1 {
			t.Fatal("missing imported snapshot reference")
		}
		authored = delta.ImportedWorldChunkOverrides[0].SnapshotPath
	case "object":
		if len(delta.VoxelObjectOverrides) != 1 {
			t.Fatal("missing object snapshot reference")
		}
		authored = delta.VoxelObjectOverrides[0].SnapshotPath
	}
	return content.ResolveDocumentPath(authored, deltaPath)
}

func s4aLoadPayload(t *testing.T, kind, path string) *volume.XBrickMap {
	t.Helper()
	switch kind {
	case "terrain":
		chunk, err := content.LoadTerrainChunk(path)
		if err != nil {
			t.Fatal(err)
		}
		return terrainChunkToXBrickMap(chunk)
	case "imported":
		chunk, err := content.LoadImportedWorldChunk(path)
		if err != nil {
			t.Fatal(err)
		}
		return ImportedWorldChunkToXBrickMap(chunk)
	default:
		snapshot, err := content.LoadVoxelObjectSnapshot(path)
		if err != nil {
			t.Fatal(err)
		}
		return XBrickMapFromVoxelObjectSnapshot(snapshot)
	}
}

func s4aVoxel(t *testing.T, xbm *volume.XBrickMap, x int, value uint8) {
	t.Helper()
	found, got := xbm.GetVoxel(x, 0, 0)
	if found != (value != 0) || got != value {
		t.Fatalf("payload voxel (%d,0,0)=%d found=%v, want %d", x, got, found, value)
	}
}

func TestS4aFailedManifestPreservesDurablePayloadsAndRetry(t *testing.T) {
	for _, kind := range []string{"terrain", "imported", "object"} {
		t.Run(kind, func(t *testing.T) {
			app, cmd, state := s4aState(t)
			assets := assetServerFromApp(app)
			geometry := volume.NewXBrickMap()
			geometry.SetVoxel(0, 0, 0, 1)
			model := VoxelModelComponent{VoxelModel: assets.RegisterSharedVoxelGeometry(geometry, ""), VoxelResolution: 1, TerrainChunkSize: 16}
			components := []any{model, VoxelPersistenceDirtyComponent{}}
			if kind == "terrain" {
				components = append(components, AuthoredTerrainChunkRefComponent{LevelID: "s4a", TerrainID: "terrain"})
			} else if kind == "imported" {
				components = append(components, AuthoredImportedWorldChunkRefComponent{LevelID: "s4a", WorldID: "world"})
			}
			entity := cmd.AddEntity(components...)
			app.FlushCommands()
			live, ok := ResolveVoxelGeometryMap(assets, &model)
			if !ok {
				t.Fatal("fixture geometry missing")
			}
			loaded := &streamedLoadedChunk{}
			switch kind {
			case "terrain":
				loaded.TerrainEntities = map[EntityId]struct{}{entity: {}}
			case "imported":
				loaded.ImportedWorldEntities = map[EntityId]struct{}{entity: {}}
			case "object":
				loaded.ObjectEntities = map[string]EntityId{voxelObjectRuntimeKey("placement", "item"): entity}
			}
			if err := persistChunkOverrides(cmd, state, ChunkCoord{}, loaded); err != nil {
				t.Fatal(err)
			}
			oldDelta := s4aLoadDelta(t, state.WorldDeltaPath)
			oldPath := s4aPayloadPath(t, oldDelta, kind, state.WorldDeltaPath)
			oldBytes := s4aRead(t, oldPath)
			s4aVoxel(t, s4aLoadPayload(t, kind, oldPath), 0, 1)
			live.SetVoxel(0, 0, 0, 0)
			live.SetVoxel(1, 0, 0, 9)
			state.WorldDelta.SchemaVersion = -1
			if err := persistChunkOverrides(cmd, state, ChunkCoord{}, loaded); err == nil {
				t.Fatal("invalid manifest schema must fail publication after payload attempt")
			}
			if !reflect.DeepEqual(s4aLoadDelta(t, state.WorldDeltaPath), oldDelta) || !bytes.Equal(s4aRead(t, oldPath), oldBytes) {
				t.Fatal("failed manifest publication changed previous durable references or payload")
			}
			state.WorldDelta.SchemaVersion = content.CurrentWorldDeltaSchemaVersion
			if err := persistChunkOverrides(cmd, state, ChunkCoord{}, loaded); err != nil {
				t.Fatal(err)
			}
			newPath := s4aPayloadPath(t, s4aLoadDelta(t, state.WorldDeltaPath), kind, state.WorldDeltaPath)
			if newPath == oldPath || filepath.Ext(newPath) != filepath.Ext(oldPath) || filepath.Dir(newPath) != filepath.Clean(state.WorldDataDir) {
				t.Fatal("retry must publish a distinct payload in world-data directory with original extension")
			}
			updated := s4aLoadPayload(t, kind, newPath)
			s4aVoxel(t, updated, 0, 0)
			s4aVoxel(t, updated, 1, 9)
			if !bytes.Equal(s4aRead(t, oldPath), oldBytes) {
				t.Fatal("successful retry overwrote previously referenced payload")
			}
		})
	}
}

func TestS4aNavigationPayloadAttemptPreservesPublishedImportedSnapshot(t *testing.T) {
	_, _, state := s4aState(t)
	snapshot := &content.ImportedWorldChunkDef{SchemaVersion: content.CurrentImportedWorldChunkSchemaVersion, WorldID: "world", ChunkSize: 16, VoxelResolution: 1, NonEmptyVoxelCount: 1, Voxels: []content.ImportedWorldVoxelDef{{X: 0, Y: 0, Z: 0, Value: 1}}}
	if err := persistImportedWorldRuntimeEditSnapshots(state, []*content.ImportedWorldChunkDef{snapshot}); err != nil {
		t.Fatal(err)
	}
	if err := saveStreamedWorldDeltaNow(state); err != nil {
		t.Fatal(err)
	}
	durable := s4aLoadDelta(t, state.WorldDeltaPath)
	oldPath := s4aPayloadPath(t, durable, "imported", state.WorldDeltaPath)
	oldBytes := s4aRead(t, oldPath)
	snapshot.Voxels[0] = content.ImportedWorldVoxelDef{X: 1, Y: 0, Z: 0, Value: 9}
	queueStreamedNavigationEditAnalysis(state, streamedNavigationEditAnalysisItem{
		WorldID: snapshot.WorldID, Coord: snapshot.Coord, ChunkSize: snapshot.ChunkSize,
		VoxelResolution: snapshot.VoxelResolution, Snapshot: snapshot,
	})
	startStreamedNavigationEditAnalysis(state)
	state.jobs.Wait()
	result := <-state.navigationEditAnalyses
	if result.Err != nil || len(result.Items) != 1 {
		t.Fatalf("navigation analysis payload attempt failed: %+v", result)
	}
	if !reflect.DeepEqual(s4aLoadDelta(t, state.WorldDeltaPath), durable) || !bytes.Equal(s4aRead(t, oldPath), oldBytes) {
		t.Fatal("unpublished navigation payload attempt changed durable imported snapshot")
	}
	newPath := content.ResolveDocumentPath(result.Items[0].Override.SnapshotPath, state.WorldDeltaPath)
	if newPath == oldPath || filepath.Ext(newPath) != filepath.Ext(oldPath) || filepath.Dir(newPath) != filepath.Clean(state.WorldDataDir) {
		t.Fatal("navigation attempt must prepare a distinct payload with compatible path")
	}
	s4aVoxel(t, s4aLoadPayload(t, "imported", oldPath), 0, 1)
	updated := s4aLoadPayload(t, "imported", newPath)
	s4aVoxel(t, updated, 0, 0)
	s4aVoxel(t, updated, 1, 9)
}
