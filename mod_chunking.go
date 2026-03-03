package gekko

import (
	"fmt"
	"math"

	"github.com/go-gl/mathgl/mgl32"
)

// ChunkCoord represents integral chunk coordinates within the world grid.
type ChunkCoord struct {
	X int
	Y int
	Z int
}

// FromPosition computes the chunk coordinate containing the given position.
func ChunkCoordFromPosition(position mgl32.Vec3, chunkSize float32) ChunkCoord {
	if chunkSize <= 0 {
		return ChunkCoord{}
	}

	return ChunkCoord{
		X: int(math.Floor(float64(position.X() / chunkSize))),
		Y: int(math.Floor(float64(position.Y() / chunkSize))),
		Z: int(math.Floor(float64(position.Z() / chunkSize))),
	}
}

// ToCenter converts a chunk coordinate to the world-space center point for that chunk.
func (c ChunkCoord) ToCenter(chunkSize float32) mgl32.Vec3 {
	if chunkSize <= 0 {
		return mgl32.Vec3{}
	}

	return mgl32.Vec3{
		(float32(c.X) + 0.5) * chunkSize,
		(float32(c.Y) + 0.5) * chunkSize,
		(float32(c.Z) + 0.5) * chunkSize,
	}
}

// NeighborsWithin returns all chunk coordinates within the provided Chebyshev radius.
func (c ChunkCoord) NeighborsWithin(radius int) []ChunkCoord {
	if radius < 0 {
		radius = 0
	}

	size := 2*radius + 1
	results := make([]ChunkCoord, 0, size*size*size)
	for dx := -radius; dx <= radius; dx++ {
		for dy := -radius; dy <= radius; dy++ {
			for dz := -radius; dz <= radius; dz++ {
				results = append(results, ChunkCoord{X: c.X + dx, Y: c.Y + dy, Z: c.Z + dz})
			}
		}
	}

	return results
}

func (c ChunkCoord) String() string {
	return fmt.Sprintf("%d:%d:%d", c.X, c.Y, c.Z)
}

// ChunkObserverCallback is invoked when a chunk is loaded or unloaded for an observer.
type ChunkObserverCallback func(cmd *Commands, observer EntityId, coord ChunkCoord)

// ChunkObserverComponent tracks chunk streaming state for an observer entity.
type ChunkObserverComponent struct {
	Radius       int
	ChunkSize    float32
	LoadedChunks map[ChunkCoord]struct{}
	LastCenter   ChunkCoord
	Initialized  bool

	OnChunkLoaded   ChunkObserverCallback
	OnChunkUnloaded ChunkObserverCallback
	ShouldLoad      func(coord ChunkCoord) bool

	lastRadius int
}

// NewChunkObserver constructs a chunk observer component with common defaults.
func NewChunkObserver(radius int, chunkSize float32) *ChunkObserverComponent {
	if radius < 0 {
		radius = 0
	}

	return &ChunkObserverComponent{
		Radius:       radius,
		ChunkSize:    chunkSize,
		LoadedChunks: make(map[ChunkCoord]struct{}),
		lastRadius:   -1,
	}
}

// WithCallbacks sets load/unload callbacks and returns the receiver for chaining.
func (c *ChunkObserverComponent) WithCallbacks(onLoad, onUnload ChunkObserverCallback) *ChunkObserverComponent {
	if c == nil {
		return c
	}

	c.OnChunkLoaded = onLoad
	c.OnChunkUnloaded = onUnload
	return c
}

// WithFilter sets the chunk inclusion predicate used during streaming.
func (c *ChunkObserverComponent) WithFilter(filter func(coord ChunkCoord) bool) *ChunkObserverComponent {
	if c == nil {
		return c
	}

	c.ShouldLoad = filter
	return c
}

// Reset clears the observer's cached state and marks it uninitialized.
func (c *ChunkObserverComponent) Reset() {
	if c == nil {
		return
	}

	c.LoadedChunks = make(map[ChunkCoord]struct{})
	c.Initialized = false
	c.lastRadius = -1
}

// ChunkTrackerResource tracks active chunk observers to handle component/entity removals.
type ChunkTrackerResource struct {
	ActiveObservers map[EntityId]*ChunkObserverComponent
}

// ChunkObserverModule wires chunk streaming into the application lifecycle.
type ChunkObserverModule struct{}

func (ChunkObserverModule) Install(app *App, cmd *Commands) {
	cmd.AddResources(&ChunkTrackerResource{
		ActiveObservers: make(map[EntityId]*ChunkObserverComponent),
	})
	app.UseSystem(
		System(UpdateChunkObserversSystem).
			InStage(PreUpdate).
			RunAlways(),
	)
}

// UpdateChunkObserversSystem streams chunks around registered observers.
func UpdateChunkObserversSystem(cmd *Commands, tracker *ChunkTrackerResource) {
	currentActive := make(map[EntityId]bool)

	MakeQuery2[TransformComponent, ChunkObserverComponent](cmd).Map(func(id EntityId, transform *TransformComponent, observer *ChunkObserverComponent) bool {
		currentActive[id] = true
		tracker.ActiveObservers[id] = observer
		if observer == nil || transform == nil {
			return true
		}

		if observer.ChunkSize <= 0 {
			return true
		}

		if observer.Radius < 0 {
			observer.Radius = 0
		}

		currentCenter := ChunkCoordFromPosition(transform.Position, observer.ChunkSize)

		if observer.Initialized && currentCenter == observer.LastCenter && observer.lastRadius == observer.Radius {
			// Nothing to do this frame.
			return true
		}

		desired := make(map[ChunkCoord]struct{})
		for _, chunk := range currentCenter.NeighborsWithin(observer.Radius) {
			if observer.ShouldLoad != nil && !observer.ShouldLoad(chunk) {
				continue
			}
			desired[chunk] = struct{}{}
		}

		// Unload stale chunks.
		for coord := range observer.LoadedChunks {
			if _, ok := desired[coord]; ok {
				continue
			}

			if observer.OnChunkUnloaded != nil {
				observer.OnChunkUnloaded(cmd, id, coord)
			}

			delete(observer.LoadedChunks, coord)
		}

		// Load new chunks.
		for coord := range desired {
			if _, ok := observer.LoadedChunks[coord]; ok {
				continue
			}

			if observer.OnChunkLoaded != nil {
				observer.OnChunkLoaded(cmd, id, coord)
			}

			observer.LoadedChunks[coord] = struct{}{}
		}

		observer.LastCenter = currentCenter
		observer.lastRadius = observer.Radius
		observer.Initialized = true

		return true
	})

	for id, observer := range tracker.ActiveObservers {
		if !currentActive[id] {
			// Entity or component was removed
			for coord := range observer.LoadedChunks {
				if observer.OnChunkUnloaded != nil {
					observer.OnChunkUnloaded(cmd, id, coord)
				}
			}
			observer.Reset()
			delete(tracker.ActiveObservers, id)
		}
	}
}
