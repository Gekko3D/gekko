package gekko

import (
	"math"

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

type spriteSyncItem struct {
	Instance app_rt.SpriteInstanceInput
	AtlasKey string
	IsUI     bool
}

func spriteSyncItemFromComponent(sp *SpriteComponent) (spriteSyncItem, bool) {
	if sp == nil || !sp.Enabled || !spriteHasDrawableSurface(sp) {
		return spriteSyncItem{}, false
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

	return spriteSyncItem{
		Instance: inst,
		AtlasKey: spriteAtlasKey(sp.Texture),
		IsUI:     sp.IsUI,
	}, true
}

func spriteHasDrawableSurface(sp *SpriteComponent) bool {
	if sp.Size[0] <= 0 || sp.Size[1] <= 0 || sp.Color[3] <= 0 {
		return false
	}
	for i := 0; i < 2; i++ {
		if math.IsNaN(float64(sp.Size[i])) || math.IsInf(float64(sp.Size[i]), 0) {
			return false
		}
	}
	for i := 0; i < 4; i++ {
		if math.IsNaN(float64(sp.Color[i])) || math.IsInf(float64(sp.Color[i]), 0) {
			return false
		}
	}
	for i := 0; i < 3; i++ {
		if math.IsNaN(float64(sp.Position[i])) || math.IsInf(float64(sp.Position[i]), 0) {
			return false
		}
	}
	return true
}

func appendSpriteInstance(spriteInstances *[]app_rt.SpriteInstanceInput, batches *[]app_rt.SpriteBatchInput, item spriteSyncItem) {
	instanceIndex := uint32(len(*spriteInstances))
	if len(*batches) == 0 || (*batches)[len(*batches)-1].AtlasKey != item.AtlasKey {
		*batches = append(*batches, app_rt.SpriteBatchInput{
			AtlasKey:      item.AtlasKey,
			FirstInstance: instanceIndex,
		})
	}
	(*batches)[len(*batches)-1].InstanceCount++
	*spriteInstances = append(*spriteInstances, item.Instance)
}

// spritesSync collects sprite data for the GPU.
func spritesSync(state *VoxelRtState, cmd *Commands) ([]app_rt.SpriteInstanceInput, []app_rt.SpriteBatchInput) {
	items := make([]spriteSyncItem, 0, 32)

	MakeQuery1[SpriteComponent](cmd).Map(func(eid EntityId, sp *SpriteComponent) bool {
		if item, ok := spriteSyncItemFromComponent(sp); ok {
			items = append(items, item)
		}
		return true
	})
	if state != nil {
		for i := range state.runtimeSprites {
			if item, ok := spriteSyncItemFromComponent(&state.runtimeSprites[i]); ok {
				items = append(items, item)
			}
		}
	}

	spriteInstances := make([]app_rt.SpriteInstanceInput, 0, len(items))
	batches := make([]app_rt.SpriteBatchInput, 0, 8)
	appendGroupedWorldSpriteInstances(&spriteInstances, &batches, items)
	appendUISpriteInstances(&spriteInstances, &batches, items)

	return spriteInstances, batches
}

func appendGroupedWorldSpriteInstances(spriteInstances *[]app_rt.SpriteInstanceInput, batches *[]app_rt.SpriteBatchInput, items []spriteSyncItem) {
	worldAtlasOrder := make([]string, 0, 8)
	worldByAtlas := make(map[string][]spriteSyncItem, 8)
	for _, item := range items {
		if item.IsUI {
			continue
		}
		if _, ok := worldByAtlas[item.AtlasKey]; !ok {
			worldAtlasOrder = append(worldAtlasOrder, item.AtlasKey)
		}
		worldByAtlas[item.AtlasKey] = append(worldByAtlas[item.AtlasKey], item)
	}
	for _, atlasKey := range worldAtlasOrder {
		for _, item := range worldByAtlas[atlasKey] {
			appendSpriteInstance(spriteInstances, batches, item)
		}
	}
}

func appendUISpriteInstances(spriteInstances *[]app_rt.SpriteInstanceInput, batches *[]app_rt.SpriteBatchInput, items []spriteSyncItem) {
	for _, item := range items {
		if !item.IsUI {
			continue
		}
		appendSpriteInstance(spriteInstances, batches, item)
	}
}

func spriteAtlasKey(id AssetId) string {
	if id == (AssetId{}) {
		return ""
	}
	return id.String()
}
