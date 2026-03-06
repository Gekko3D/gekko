package gekko

import (
	"unsafe"

	"github.com/go-gl/mathgl/mgl32"
)

// SpriteComponent represents a 2D billboard in the world or on the screen.
type SpriteComponent struct {
	Enabled bool

	// Position is either World Space ([3]float32) or Screen Space (pixels)
	Position mgl32.Vec3
	Size     [2]float32
	Color    [4]float32

	SpriteIndex uint32
	AtlasCols   uint32
	AtlasRows   uint32

	Texture AssetId
	IsUI    bool // If true, Position is screen-space pixels and Size is pixels
}

// SpriteInstance matches WGSL layout in sprites.wgsl
// Std430: vec3 (16-align), vec4 (16-align), f32 (4-align)
type SpriteInstance struct {
	Pos  [3]float32
	IsUI uint32 // 16 bytes total

	Size     [2]float32
	Padding1 [2]float32 // 16 bytes

	Color [4]float32 // 16 bytes

	SpriteIndex uint32
	AtlasCols   uint32
	AtlasRows   uint32
	Padding2    uint32 // 16 bytes
}

// spritesSync collects sprite data for the GPU.
func spritesSync(state *VoxelRtState, cmd *Commands) ([]byte, uint32, AssetId) {
	spriteInstances := make([]SpriteInstance, 0, 32)
	var firstAtlas AssetId

	MakeQuery1[SpriteComponent](cmd).Map(func(eid EntityId, sp *SpriteComponent) bool {
		if sp == nil || !sp.Enabled {
			return true
		}

		if firstAtlas == (AssetId{}) && sp.Texture != (AssetId{}) {
			firstAtlas = sp.Texture
		}

		cols := sp.AtlasCols
		if cols == 0 {
			cols = 1
		}
		rows := sp.AtlasRows
		if rows == 0 {
			rows = 1
		}

		isUIVal := uint32(0)
		if sp.IsUI {
			isUIVal = 1
		}

		// Pack Params
		inst := SpriteInstance{
			Pos:         [3]float32{sp.Position.X(), sp.Position.Y(), sp.Position.Z()},
			IsUI:        isUIVal,
			Size:        sp.Size,
			Color:       sp.Color,
			SpriteIndex: sp.SpriteIndex,
			AtlasCols:   cols,
			AtlasRows:   rows,
		}
		spriteInstances = append(spriteInstances, inst)

		return true
	})

	var spriteBytes []byte
	spriteCount := uint32(len(spriteInstances))
	if spriteCount > 0 {
		spriteBytes = unsafe.Slice((*byte)(unsafe.Pointer(&spriteInstances[0])), len(spriteInstances)*int(unsafe.Sizeof(SpriteInstance{})))
	}

	return spriteBytes, spriteCount, firstAtlas
}
