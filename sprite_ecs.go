package gekko

import (
	"unsafe"

	"github.com/go-gl/mathgl/mgl32"
)

type BillboardMode uint32

const (
	BillboardSpherical   BillboardMode = 0
	BillboardCylindrical BillboardMode = 1 // Y-aligned
	BillboardFixed       BillboardMode = 2
)

// SpriteComponent represents a 2D billboard in the world or on the screen.
type SpriteAlphaMode uint32

const (
	SpriteAlphaTexture SpriteAlphaMode = iota
	SpriteAlphaLuminance
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

	Texture       AssetId
	IsUI          bool // If true, Position is screen-space pixels and Size is pixels
	BillboardMode BillboardMode
	Unlit         bool
	AlphaMode     SpriteAlphaMode
}

// SpriteInstance matches WGSL layout in sprites.wgsl
// Std430: vec3 (16-align), vec4 (16-align), f32 (4-align)
type SpriteInstance struct {
	Pos  [3]float32
	IsUI uint32 // 16 bytes total

	Size      [2]float32
	IsUnlit   uint32
	AlphaMode uint32 // 16 bytes

	Color [4]float32 // 16 bytes

	SpriteIndex   uint32
	AtlasCols     uint32
	AtlasRows     uint32
	BillboardMode BillboardMode // 16 bytes
}

type SpriteBatch struct {
	AtlasKey      string
	FirstInstance uint32
	InstanceCount uint32
}

func appendSpriteInstance(spriteInstances *[]SpriteInstance, batches *[]SpriteBatch, sp *SpriteComponent) {
	if sp == nil || !sp.Enabled {
		return
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

	inst := SpriteInstance{
		Pos:           [3]float32{sp.Position.X(), sp.Position.Y(), sp.Position.Z()},
		IsUI:          isUIVal,
		Size:          sp.Size,
		Color:         sp.Color,
		SpriteIndex:   sp.SpriteIndex,
		AtlasCols:     cols,
		AtlasRows:     rows,
		BillboardMode: sp.BillboardMode,
		AlphaMode:     uint32(sp.AlphaMode),
	}
	if sp.Unlit {
		inst.IsUnlit = 1
	}

	atlasKey := spriteAtlasKey(sp.Texture)
	instanceIndex := uint32(len(*spriteInstances))
	if len(*batches) == 0 || (*batches)[len(*batches)-1].AtlasKey != atlasKey {
		*batches = append(*batches, SpriteBatch{
			AtlasKey:      atlasKey,
			FirstInstance: instanceIndex,
		})
	}
	(*batches)[len(*batches)-1].InstanceCount++
	*spriteInstances = append(*spriteInstances, inst)
}

// spritesSync collects sprite data for the GPU.
func spritesSync(state *VoxelRtState, cmd *Commands) ([]byte, uint32, []SpriteBatch) {
	spriteInstances := make([]SpriteInstance, 0, 32)
	batches := make([]SpriteBatch, 0, 8)

	MakeQuery1[SpriteComponent](cmd).Map(func(eid EntityId, sp *SpriteComponent) bool {
		appendSpriteInstance(&spriteInstances, &batches, sp)
		return true
	})
	if state != nil {
		for i := range state.runtimeSprites {
			appendSpriteInstance(&spriteInstances, &batches, &state.runtimeSprites[i])
		}
	}

	var spriteBytes []byte
	spriteCount := uint32(len(spriteInstances))
	if spriteCount > 0 {
		spriteBytes = unsafe.Slice((*byte)(unsafe.Pointer(&spriteInstances[0])), len(spriteInstances)*int(unsafe.Sizeof(SpriteInstance{})))
	}

	return spriteBytes, spriteCount, batches
}

func spriteAtlasKey(id AssetId) string {
	if id == (AssetId{}) {
		return ""
	}
	return id.String()
}
