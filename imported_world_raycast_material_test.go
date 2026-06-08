package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/content"
)

func TestImportedWorldMaterialForRaycastHitResolvesBaseWorldMaterial(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&TransformComponent{})
	entity := cmd.AddEntity(&AuthoredImportedWorldChunkRefComponent{
		LevelID:    "level-a",
		WorldID:    "world-a",
		ChunkCoord: [3]int{0, 0, 0},
	})
	app.FlushCommands()

	state := &StreamedLevelRuntimeState{
		BaseWorldID: "world-a",
		BaseWorldMaterialLookup: NewImportedWorldMaterialLookup(&content.ImportedWorldDef{
			WorldID: "world-a",
			Materials: []content.ImportedWorldMaterialDef{{
				ID:                7,
				PaletteIndex:      4,
				SourceTextureName: "METAL01",
				Kind:              "metal",
				CollisionKind:     "solid",
				Tags:              []string{"material:metal"},
			}},
		}),
	}

	resolved, ok := ImportedWorldMaterialForRaycastHit(cmd, state, RaycastHit{
		Hit:          true,
		Entity:       entity,
		PaletteIndex: 4,
		Pos:          [3]int{1, 2, 3},
	})
	if !ok || resolved.Material.Kind != "metal" || resolved.ChunkRef.WorldID != "world-a" {
		t.Fatalf("expected imported-world material hit, got %+v ok=%t", resolved, ok)
	}
	if !content.ImportedWorldMaterialHasTag(resolved.Material, "material:metal") {
		t.Fatalf("expected resolved material tags, got %+v", resolved.Material.Tags)
	}
}

func TestImportedWorldMaterialForRaycastHitRejectsWrongWorld(t *testing.T) {
	app := NewApp()
	cmd := app.Commands()
	cmd.AddEntity(&TransformComponent{})
	entity := cmd.AddEntity(&AuthoredImportedWorldChunkRefComponent{
		LevelID: "level-a",
		WorldID: "other-world",
	})
	app.FlushCommands()

	state := &StreamedLevelRuntimeState{
		BaseWorldID: "world-a",
		BaseWorldMaterialLookup: NewImportedWorldMaterialLookup(&content.ImportedWorldDef{
			WorldID: "world-a",
			Materials: []content.ImportedWorldMaterialDef{{
				PaletteIndex: 4,
				Kind:         "metal",
			}},
		}),
	}
	if _, ok := ImportedWorldMaterialForRaycastHit(cmd, state, RaycastHit{Hit: true, Entity: entity, PaletteIndex: 4}); ok {
		t.Fatal("expected material resolver to reject imported chunks from a different world")
	}
}
