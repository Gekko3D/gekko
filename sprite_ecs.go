package gekko

import (
	app_rt "github.com/gekko3d/gekko/voxelrt/rt/app"
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

func appendSpriteInstance(spriteInstances *[]app_rt.SpriteInstanceInput, batches *[]app_rt.SpriteBatchInput, sp *SpriteComponent) {
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

	inst := app_rt.SpriteInstanceInput{
		Pos:           [3]float32{sp.Position.X(), sp.Position.Y(), sp.Position.Z()},
		IsUI:          isUIVal,
		Size:          sp.Size,
		Color:         sp.Color,
		SpriteIndex:   sp.SpriteIndex,
		AtlasCols:     cols,
		AtlasRows:     rows,
		BillboardMode: uint32(sp.BillboardMode),
		AlphaMode:     uint32(sp.AlphaMode),
	}
	if sp.Unlit {
		inst.IsUnlit = 1
	}

	atlasKey := spriteAtlasKey(sp.Texture)
	instanceIndex := uint32(len(*spriteInstances))
	if len(*batches) == 0 || (*batches)[len(*batches)-1].AtlasKey != atlasKey {
		*batches = append(*batches, app_rt.SpriteBatchInput{
			AtlasKey:      atlasKey,
			FirstInstance: instanceIndex,
		})
	}
	(*batches)[len(*batches)-1].InstanceCount++
	*spriteInstances = append(*spriteInstances, inst)
}

// spritesSync collects sprite data for the GPU.
func spritesSync(state *VoxelRtState, cmd *Commands) ([]app_rt.SpriteInstanceInput, []app_rt.SpriteBatchInput) {
	spriteInstances := make([]app_rt.SpriteInstanceInput, 0, 32)
	batches := make([]app_rt.SpriteBatchInput, 0, 8)

	MakeQuery1[SpriteComponent](cmd).Map(func(eid EntityId, sp *SpriteComponent) bool {
		appendSpriteInstance(&spriteInstances, &batches, sp)
		return true
	})
	if state != nil {
		for i := range state.runtimeSprites {
			appendSpriteInstance(&spriteInstances, &batches, &state.runtimeSprites[i])
		}
	}

	return spriteInstances, batches
}

func spriteAtlasKey(id AssetId) string {
	if id == (AssetId{}) {
		return ""
	}
	return id.String()
}
