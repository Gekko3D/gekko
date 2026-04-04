package gekko

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

type chunkEventTracker struct {
	loads   []ChunkCoord
	unloads []ChunkCoord
}

func (t *chunkEventTracker) onLoad(_ *Commands, _ EntityId, coord ChunkCoord) {
	t.loads = append(t.loads, coord)
}

func (t *chunkEventTracker) onUnload(_ *Commands, _ EntityId, coord ChunkCoord) {
	t.unloads = append(t.unloads, coord)
}

func (t *chunkEventTracker) reset() {
	t.loads = t.loads[:0]
	t.unloads = t.unloads[:0]
}

func TestChunkObserverInitialLoad(t *testing.T) {
	app := NewApp()
	app.UseModules(ChunkObserverModule{})
	cmd := app.Commands()

	tracker := &chunkEventTracker{}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		NewChunkObserver(1, 8).
			WithCallbacks(tracker.onLoad, tracker.onUnload),
	)

	app.build()
	app.callSystems(0, execute, DynamicUpdate)

	if len(tracker.unloads) != 0 {
		t.Fatalf("expected no unloads on initial tick, got %d", len(tracker.unloads))
	}

	expected := (2*1 + 1) * (2*1 + 1) * (2*1 + 1)
	if len(tracker.loads) != expected {
		t.Fatalf("expected %d loads, got %d", expected, len(tracker.loads))
	}

	var observer *ChunkObserverComponent
	MakeQuery2[TransformComponent, ChunkObserverComponent](cmd).Map(func(id EntityId, tr *TransformComponent, ob *ChunkObserverComponent) bool {
		observer = ob
		return false
	})

	if observer == nil {
		t.Fatalf("observer component not found")
	}

	if len(observer.LoadedChunks) != expected {
		t.Fatalf("expected %d loaded chunks recorded, got %d", expected, len(observer.LoadedChunks))
	}
}

func TestChunkObserverMovement(t *testing.T) {
	app := NewApp()
	app.UseModules(ChunkObserverModule{})
	cmd := app.Commands()

	tracker := &chunkEventTracker{}
	chunkSize := float32(10)

	entity := cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{0, 0, 0}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		NewChunkObserver(1, chunkSize).
			WithCallbacks(tracker.onLoad, tracker.onUnload),
	)

	app.build()
	app.callSystems(0, execute, DynamicUpdate)

	tracker.reset()

	MakeQuery2[TransformComponent, ChunkObserverComponent](cmd).Map(func(id EntityId, tr *TransformComponent, ob *ChunkObserverComponent) bool {
		if id == entity {
			tr.Position = mgl32.Vec3{chunkSize + 0.1, 0, 0}
			return false
		}
		return true
	})

	app.callSystems(0, execute, DynamicUpdate)

	if len(tracker.loads) != 9 {
		t.Fatalf("expected 9 loads after moving one chunk, got %d", len(tracker.loads))
	}
	if len(tracker.unloads) != 9 {
		t.Fatalf("expected 9 unloads after moving one chunk, got %d", len(tracker.unloads))
	}

	seen := make(map[ChunkCoord]struct{})
	for _, coord := range tracker.loads {
		seen[coord] = struct{}{}
	}
	if len(seen) != len(tracker.loads) {
		t.Fatalf("duplicate load events detected")
	}
}

func TestChunkObserverNoDuplicateLoads(t *testing.T) {
	app := NewApp()
	app.UseModules(ChunkObserverModule{})
	cmd := app.Commands()

	tracker := &chunkEventTracker{}

	cmd.AddEntity(
		&TransformComponent{Position: mgl32.Vec3{5, 0, 5}, Rotation: mgl32.QuatIdent(), Scale: mgl32.Vec3{1, 1, 1}},
		NewChunkObserver(2, 8).
			WithCallbacks(tracker.onLoad, tracker.onUnload),
	)

	app.build()
	app.callSystems(0, execute, DynamicUpdate)

	tracker.reset()
	app.callSystems(0, execute, DynamicUpdate)

	if len(tracker.loads) != 0 {
		t.Fatalf("expected no additional loads without movement, got %d", len(tracker.loads))
	}
	if len(tracker.unloads) != 0 {
		t.Fatalf("expected no unloads without movement, got %d", len(tracker.unloads))
	}
}
