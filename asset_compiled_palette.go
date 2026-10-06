package gekko

import (
	"fmt"
	"reflect"
	"sync"
)

// A registration owns one independent publication copy. Its immutable sealed key
// survives transfer/release to permit verified warm reuse, never another cold
// transfer. The caller owns source and proves its key before adoption.
type compiledPaletteRegistration struct {
	mu      sync.Mutex
	source  *VoxelPaletteAsset
	palette *VoxelPaletteAsset
	key     string
	bytes   int64
}

func cloneCompiledPaletteSlice[T any](source []T) []T {
	if source == nil {
		return nil
	}
	cloned := make([]T, len(source))
	copy(cloned, source)
	return cloned
}

func immutableCompiledPaletteProperty(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.TypeOf(value).Kind() {
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

func prepareCompiledPaletteRegistration(source *VoxelPaletteAsset) (*compiledPaletteRegistration, error) {
	return preparePaletteRegistrationWithKey(source, "")
}

// An explicit sealed key preserves the ordinary VOX palette namespace.
func preparePaletteRegistrationWithKey(source *VoxelPaletteAsset, sealedKey string) (*compiledPaletteRegistration, error) {
	palette, err := clonePalettePublication(source)
	if err != nil {
		return nil, err
	}

	// The exact existing key preserves nil/empty distinctions and scalar types.
	// A failed JSON marshal produces an empty key; private preparation rejects it
	// rather than using the public creator's historical fallback behavior.
	key := sealedKey
	if key == "" {
		key = voxelPaletteAssetCacheKey(*palette)
	}
	if key == "" {
		return nil, fmt.Errorf("compiled palette cannot produce a valid cache key")
	}
	return &compiledPaletteRegistration{source: source, palette: palette, key: key, bytes: compiledPalettePublicationCharge(palette)}, nil
}

// Clone the complete palette graph, preserving container shape and scalar types.
// Both packet sources and publication copies use this independent ownership pass.
func clonePalettePublication(source *VoxelPaletteAsset) (*VoxelPaletteAsset, error) {
	if source == nil {
		return nil, fmt.Errorf("compiled palette source is nil")
	}
	palette := *source
	palette.Materials = cloneCompiledPaletteSlice(source.Materials)
	for i := range palette.Materials {
		properties := source.Materials[i].Property
		if properties == nil {
			continue
		}
		palette.Materials[i].Property = make(map[string]interface{}, len(properties))
		for key, value := range properties {
			if !immutableCompiledPaletteProperty(value) {
				return nil, fmt.Errorf("compiled palette property %q is not an immutable JSON scalar", key)
			}
			palette.Materials[i].Property[key] = value
		}
	}
	if source.SurfaceMaterials != nil {
		palette.SurfaceMaterials = make(map[uint8]VoxelSurfaceMaterial, len(source.SurfaceMaterials))
		for index, surface := range source.SurfaceMaterials {
			surface.Tags = cloneCompiledPaletteSlice(surface.Tags)
			palette.SurfaceMaterials[index] = surface
		}
	}
	if source.MaterialFrameOverrides != nil {
		palette.MaterialFrameOverrides = make(map[uint8]VoxelPaletteMaterialFrameOverride, len(source.MaterialFrameOverrides))
		for index, override := range source.MaterialFrameOverrides {
			palette.MaterialFrameOverrides[index] = override
		}
	}
	palette.Animations = cloneCompiledPaletteSlice(source.Animations)
	for i := range palette.Animations {
		original := source.Animations[i]
		animation := &palette.Animations[i]
		animation.PaletteIndices = cloneCompiledPaletteSlice(original.PaletteIndices)
		animation.Tags = cloneCompiledPaletteSlice(original.Tags)
		if original.UVScroll != nil {
			copy := *original.UVScroll
			animation.UVScroll = &copy
		}
		animation.Frames = cloneCompiledPaletteSlice(original.Frames)
		for j := range animation.Frames {
			frame := &animation.Frames[j]
			originalFrame := original.Frames[j]
			frame.Colors = cloneCompiledPaletteSlice(originalFrame.Colors)
			frame.EmissiveColors = cloneCompiledPaletteSlice(originalFrame.EmissiveColors)
			frame.Emission = cloneCompiledPaletteSlice(originalFrame.Emission)
			frame.Roughness = cloneCompiledPaletteSlice(originalFrame.Roughness)
			frame.Transparency = cloneCompiledPaletteSlice(originalFrame.Transparency)
		}
	}
	return &palette, nil
}

func (registration *compiledPaletteRegistration) clearLocked() {
	registration.source = nil
	registration.palette = nil
	registration.bytes = 0
}

func (registration *compiledPaletteRegistration) release() {
	if registration == nil {
		return
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	registration.clearLocked()
}

func (registration *compiledPaletteRegistration) charge() int64 {
	if registration == nil {
		return 0
	}
	registration.mu.Lock()
	defer registration.mu.Unlock()
	return registration.bytes
}

// Lock order is server then registration. Cold publication transfers prepared
// nested storage directly; warm reuse drains only the unused publication copy.
func (server *AssetServer) adoptCompiledAssetPalette(cacheKey string, source *VoxelPaletteAsset, registration *compiledPaletteRegistration) (AssetId, bool) {
	if server == nil || source == nil || registration == nil || cacheKey == "" {
		return AssetId{}, false
	}
	server.ensureVoxelStorage()
	server.mu.Lock()
	defer server.mu.Unlock()
	registration.mu.Lock()
	defer registration.mu.Unlock()
	if registration.key != cacheKey || registration.palette != nil && registration.source != source {
		return AssetId{}, false
	}
	if id, warm := server.voxPaletteKeys[cacheKey]; warm {
		if _, exists := server.voxPalettes[id]; !exists {
			return AssetId{}, false
		}
		registration.clearLocked()
		return id, true
	}
	if registration.palette == nil {
		return AssetId{}, false
	}
	id := makeAssetId()
	server.voxPalettes[id] = *registration.palette
	server.voxPaletteKeys[cacheKey] = id
	registration.clearLocked()
	return id, true
}
