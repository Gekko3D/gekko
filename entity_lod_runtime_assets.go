package gekko

import (
	"fmt"
	"math"
)

const (
	entityLODGeneratedAssetVersion = "e009-v3"
	entityLODSimplifiedScale       = 0.5
	entityLODImpostorTextureSize   = 64
	entityLODDotTextureSize        = 8
)

func entityLODCacheKey(kind string, geometryID, paletteID AssetId) string {
	return fmt.Sprintf("entity-lod:%s:%s:%s:%s", entityLODGeneratedAssetVersion, kind, geometryID, paletteID)
}

func (server *AssetServer) ensureTextureStorage() {
	if server == nil {
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.textures == nil {
		server.textures = make(map[AssetId]TextureAsset)
	}
	if server.textureKeys == nil {
		server.textureKeys = make(map[string]AssetId)
	}
}

func (server *AssetServer) createTextureFromTexelsWithCacheKey(cacheKey string, texels []uint8, texWidth uint32, texHeight uint32, texDepth uint32, dimension TextureDimension, format TextureFormat) AssetId {
	if server == nil {
		return AssetId{}
	}
	server.ensureTextureStorage()
	if cacheKey != "" {
		server.mu.RLock()
		if id, ok := server.textureKeys[cacheKey]; ok {
			server.mu.RUnlock()
			return id
		}
		server.mu.RUnlock()
	}

	id := makeAssetId()
	server.mu.Lock()
	defer server.mu.Unlock()
	if cacheKey != "" {
		if cachedID, ok := server.textureKeys[cacheKey]; ok {
			return cachedID
		}
		server.textureKeys[cacheKey] = id
	}
	server.textures[id] = TextureAsset{
		Version:   0,
		Texels:    texels,
		Width:     texWidth,
		Height:    texHeight,
		Depth:     texDepth,
		Dimension: dimension,
		Format:    format,
	}
	return id
}

func entityLODGeometryBounds(asset *VoxelGeometryAsset) (minX, minY, minZ, maxX, maxY, maxZ int, ok bool) {
	if asset == nil || asset.XBrickMap == nil {
		return 0, 0, 0, 0, 0, 0, false
	}
	minB, maxB := asset.XBrickMap.ComputeAABB()
	if maxB.Sub(minB).LenSqr() <= 0 {
		return 0, 0, 0, 0, 0, 0, false
	}
	return int(math.Floor(float64(minB.X()))),
		int(math.Floor(float64(minB.Y()))),
		int(math.Floor(float64(minB.Z()))),
		int(math.Ceil(float64(maxB.X()))),
		int(math.Ceil(float64(maxB.Y()))),
		int(math.Ceil(float64(maxB.Z()))),
		true
}

func entityLODGeometryExtents(asset *VoxelGeometryAsset) (extentX, extentY, extentZ float32) {
	if asset == nil {
		return 0, 0, 0
	}
	minB, maxB := asset.LocalMin, asset.LocalMax
	if maxB.Sub(minB).LenSqr() <= 0 && asset.XBrickMap != nil {
		minB, maxB = asset.XBrickMap.ComputeAABB()
	}
	extents := maxB.Sub(minB)
	return max(0, extents.X()), max(0, extents.Y()), max(0, extents.Z())
}

func entityLODProxyScaleAdjust(source, proxy *VoxelGeometryAsset) (scaleX, scaleY, scaleZ float32) {
	sourceX, sourceY, sourceZ := entityLODGeometryExtents(source)
	proxyX, proxyY, proxyZ := entityLODGeometryExtents(proxy)
	return entityLODProxyAxisAdjust(sourceX, proxyX),
		entityLODProxyAxisAdjust(sourceY, proxyY),
		entityLODProxyAxisAdjust(sourceZ, proxyZ)
}

func entityLODProxyAxisAdjust(sourceExtent, proxyExtent float32) float32 {
	if sourceExtent <= 0 || proxyExtent <= 0 {
		return 1
	}
	return sourceExtent / proxyExtent
}

func (server *AssetServer) entityLODSimplifiedGeometry(geometryID, paletteID AssetId, source *VoxelGeometryAsset) (AssetId, *VoxelGeometryAsset, bool) {
	if server == nil || source == nil || source.XBrickMap == nil {
		return AssetId{}, nil, false
	}
	sourceCount := source.XBrickMap.GetVoxelCount()
	if sourceCount <= 1 {
		return AssetId{}, nil, false
	}

	proxy := source.XBrickMap.Resample(entityLODSimplifiedScale)
	if proxy == nil || proxy == source.XBrickMap {
		return AssetId{}, nil, false
	}
	if proxy.GetVoxelCount() <= 0 || proxy.GetVoxelCount() >= sourceCount {
		return AssetId{}, nil, false
	}

	cacheKey := entityLODCacheKey("simplified", geometryID, paletteID)
	id := server.RegisterSharedVoxelGeometryWithCacheKey(cacheKey, proxy, source.SourcePath)
	if id == (AssetId{}) {
		return AssetId{}, nil, false
	}
	asset, ok := server.GetVoxelGeometry(id)
	if !ok || asset.XBrickMap == nil {
		return AssetId{}, nil, false
	}
	return id, &asset, true
}

func (server *AssetServer) entityLODImpostorTexture(geometryID, paletteID AssetId, source *VoxelGeometryAsset) (AssetId, bool) {
	if server == nil || source == nil || source.XBrickMap == nil {
		return AssetId{}, false
	}
	cacheKey := entityLODCacheKey("impostor", geometryID, paletteID)
	if cached, ok := server.entityLODTextureByCacheKey(cacheKey); ok && cached.Width > 0 && cached.Height > 0 {
		server.mu.RLock()
		id := server.textureKeys[cacheKey]
		server.mu.RUnlock()
		if id != (AssetId{}) {
			return id, true
		}
	}
	paletteAsset, ok := server.GetVoxelPalette(paletteID)
	if !ok {
		return AssetId{}, false
	}
	minX, minY, minZ, maxX, maxY, maxZ, ok := entityLODGeometryBounds(source)
	if !ok || maxX <= minX || maxY <= minY || maxZ <= minZ {
		return AssetId{}, false
	}

	const size = entityLODImpostorTextureSize
	texels := make([]uint8, size*size*4)
	zBuffer := make([]float32, size*size)
	for i := range zBuffer {
		zBuffer[i] = float32(math.Inf(-1))
	}

	spanX := float32(maxX - minX)
	spanY := float32(maxY - minY)
	if spanX <= 0 || spanY <= 0 {
		return AssetId{}, false
	}

	occupied := false
	for gx := minX; gx < maxX; gx++ {
		for gy := minY; gy < maxY; gy++ {
			for gz := minZ; gz < maxZ; gz++ {
				found, value := source.XBrickMap.GetVoxel(gx, gy, gz)
				if !found {
					continue
				}
				color := paletteAsset.VoxPalette[value]
				alpha := color[3]
				if alpha == 0 {
					// Fully transparent voxels should not stamp an impostor pixel or
					// occlude visible voxels behind them.
					continue
				}
				// Stamp the full projected footprint instead of a single texel so
				// thin voxel models do not alias into sparse, semi-transparent cards.
				x0 := int(math.Floor(float64((float32(gx)-float32(minX))/spanX * float32(size))))
				x1 := int(math.Ceil(float64((float32(gx+1)-float32(minX))/spanX * float32(size))))
				y0 := int(math.Floor(float64((float32(gy)-float32(minY))/spanY * float32(size))))
				y1 := int(math.Ceil(float64((float32(gy+1)-float32(minY))/spanY * float32(size))))
				if x1 <= x0 {
					x1 = x0 + 1
				}
				if y1 <= y0 {
					y1 = y0 + 1
				}
				if x0 < 0 {
					x0 = 0
				}
				if y0 < 0 {
					y0 = 0
				}
				if x1 > size {
					x1 = size
				}
				if y1 > size {
					y1 = size
				}
				depth := float32(gz) + 0.5
				for px := x0; px < x1; px++ {
					for pyTex := y0; pyTex < y1; pyTex++ {
						py := size - 1 - pyTex
						idx := py*size + px
						if depth < zBuffer[idx] {
							continue
						}
						zBuffer[idx] = depth
						texels[idx*4+0] = color[0]
						texels[idx*4+1] = color[1]
						texels[idx*4+2] = color[2]
						// Use fully opaque impostor texels for readability. Sparse
						// per-voxel alpha makes the card composite as a hazy overlay.
						texels[idx*4+3] = 255
						occupied = true
					}
				}
			}
		}
	}
	if !occupied {
		return AssetId{}, false
	}
	entityLODDilateTransparentRGB(texels, size, size)
	return server.createTextureFromTexelsWithCacheKey(cacheKey, texels, size, size, 1, TextureDimension2D, TextureFormatRGBA8UnormSrgb), true
}

func entityLODDilateTransparentRGB(texels []uint8, width, height int) {
	if width <= 0 || height <= 0 || len(texels) != width*height*4 {
		return
	}
	var fallbackR, fallbackG, fallbackB, fallbackCount int
	for i := 0; i < len(texels); i += 4 {
		if texels[i+3] == 0 {
			continue
		}
		fallbackR += int(texels[i+0])
		fallbackG += int(texels[i+1])
		fallbackB += int(texels[i+2])
		fallbackCount++
	}
	if fallbackCount == 0 {
		return
	}
	working := make([]uint8, len(texels))
	copy(working, texels)
	for pass := 0; pass < width+height; pass++ {
		changed := false
		next := make([]uint8, len(working))
		copy(next, working)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				idx := (y*width + x) * 4
				if working[idx+3] != 0 {
					continue
				}
				var sumR, sumG, sumB, count int
				for oy := -1; oy <= 1; oy++ {
					ny := y + oy
					if ny < 0 || ny >= height {
						continue
					}
					for ox := -1; ox <= 1; ox++ {
						nx := x + ox
						if nx < 0 || nx >= width || (ox == 0 && oy == 0) {
							continue
						}
						nIdx := (ny*width + nx) * 4
						if working[nIdx+3] == 0 {
							continue
						}
						sumR += int(working[nIdx+0])
						sumG += int(working[nIdx+1])
						sumB += int(working[nIdx+2])
						count++
					}
				}
				if count == 0 {
					continue
				}
				next[idx+0] = uint8(sumR / count)
				next[idx+1] = uint8(sumG / count)
				next[idx+2] = uint8(sumB / count)
				changed = true
			}
		}
		working = next
		if !changed {
			break
		}
	}
	fillR := uint8(fallbackR / fallbackCount)
	fillG := uint8(fallbackG / fallbackCount)
	fillB := uint8(fallbackB / fallbackCount)
	for i := 0; i < len(working); i += 4 {
		if working[i+3] != 0 {
			continue
		}
		if working[i+0] == 0 && working[i+1] == 0 && working[i+2] == 0 {
			working[i+0] = fillR
			working[i+1] = fillG
			working[i+2] = fillB
		}
	}
	copy(texels, working)
}

func (server *AssetServer) entityLODDotTexture() AssetId {
	if server == nil {
		return AssetId{}
	}
	const size = entityLODDotTextureSize
	cacheKey := entityLODCacheKey("dot", AssetId{}, AssetId{})
	texels := make([]uint8, size*size*4)
	center := float32(size-1) * 0.5
	radius := center + 0.25
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float32(x) - center
			dy := float32(y) - center
			dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
			if dist > radius {
				continue
			}
			alpha := uint8(255)
			if dist > radius-1 {
				fade := int((radius - dist) * 255)
				if fade < 64 {
					fade = 64
				}
				if fade > 255 {
					fade = 255
				}
				alpha = uint8(fade)
			}
			idx := (y*size + x) * 4
			texels[idx+0] = 255
			texels[idx+1] = 255
			texels[idx+2] = 255
			texels[idx+3] = alpha
		}
	}
	return server.createTextureFromTexelsWithCacheKey(cacheKey, texels, size, size, 1, TextureDimension2D, TextureFormatRGBA8UnormSrgb)
}

func (server *AssetServer) entityLODTextureByCacheKey(cacheKey string) (TextureAsset, bool) {
	if server == nil || cacheKey == "" {
		return TextureAsset{}, false
	}
	server.ensureTextureStorage()
	server.mu.RLock()
	defer server.mu.RUnlock()
	id, ok := server.textureKeys[cacheKey]
	if !ok {
		return TextureAsset{}, false
	}
	asset, ok := server.textures[id]
	return asset, ok
}
