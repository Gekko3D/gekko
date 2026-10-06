package gekko

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
	"github.com/go-gl/mathgl/mgl32"
)

func createVolumeTexels(voxModel *VoxModel, palette *VoxPalette) []uint8 {
	volume := make([]uint8, voxModel.SizeX*voxModel.SizeY*voxModel.SizeZ*4)
	for _, v := range voxModel.Voxels {
		idx := (int32(v.Z)*int32(voxModel.SizeY*voxModel.SizeX) + int32(v.Y)*int32(voxModel.SizeX) + int32(v.X)) * 4
		color := palette[v.ColorIndex]
		volume[idx+0] = color[0]
		volume[idx+1] = color[1]
		volume[idx+2] = color[2]
		volume[idx+3] = 255
	}
	return volume
}

func (server *AssetServer) CreateVoxelBasedTexture(voxModel *VoxModel, palette *VoxPalette) AssetId {
	volumeTexels := createVolumeTexels(voxModel, palette)
	return server.CreateTextureFromTexels(volumeTexels[:], voxModel.SizeX, voxModel.SizeY, voxModel.SizeZ, TextureDimension3D, TextureFormatRGBA8Unorm)
}

func xBrickMapFromVoxModel(model VoxModel) *volume.XBrickMap {
	xbm := volume.BuildXBrickMap(func(yield func(volume.VoxelWrite) bool) {
		for _, v := range model.Voxels {
			if !yield(volume.VoxelWrite{X: int(v.X), Y: int(v.Y), Z: int(v.Z), Value: v.ColorIndex}) {
				return
			}
		}
	})
	xbm.ComputeAABB()
	xbm.ClearDirty()
	return xbm
}

func buildVoxelGeometryAsset(model VoxModel, sourcePath string) VoxelGeometryAsset {
	xbm := xBrickMapFromVoxModel(model)
	localMin := xbm.GetAABBMin()
	localMax := xbm.GetAABBMax()
	if model.SizeX != 0 || model.SizeY != 0 || model.SizeZ != 0 {
		localMin = mgl32.Vec3{0, 0, 0}
		localMax = mgl32.Vec3{float32(model.SizeX), float32(model.SizeY), float32(model.SizeZ)}
	}
	return VoxelGeometryAsset{
		VoxModel:     model,
		XBrickMap:    xbm,
		LocalMin:     localMin,
		LocalMax:     localMax,
		BrickSize:    [3]uint32{8, 8, 8},
		SourcePath:   sourcePath,
		RuntimeOwned: true,
	}
}

func voxelGeometryCacheKey(model VoxModel, sourcePath string) string {
	payload, _ := json.Marshal(struct {
		Source string
		Model  VoxModel
	}{
		Source: sourcePath,
		Model:  model,
	})
	return string(payload)
}

func voxelPaletteCacheKey(palette VoxPalette, materials []VoxMaterial, sourcePath string) string {
	payload, _ := json.Marshal(struct {
		Source    string
		Palette   VoxPalette
		Materials []VoxMaterial
	}{
		Source:    sourcePath,
		Palette:   palette,
		Materials: materials,
	})
	return string(payload)
}

func voxelPaletteAssetCacheKey(asset VoxelPaletteAsset) string {
	key, _ := checkedVoxelPaletteAssetCacheKey(asset)
	return key
}

func checkedVoxelPaletteAssetCacheKey(asset VoxelPaletteAsset) (string, error) {
	payload, err := json.Marshal(struct {
		Palette                VoxPalette
		Materials              []VoxMaterial
		SurfaceMaterials       map[uint8]VoxelSurfaceMaterial
		Animations             []VoxelPaletteAnimation
		MaterialFrameOverrides map[uint8]VoxelPaletteMaterialFrameOverride
		IsPBR                  bool
		Roughness              float32
		Metalness              float32
		Emission               float32
		IOR                    float32
		Transparency           float32
		SourcePath             string
	}{
		Palette:                asset.VoxPalette,
		Materials:              asset.Materials,
		SurfaceMaterials:       asset.SurfaceMaterials,
		Animations:             asset.Animations,
		MaterialFrameOverrides: asset.MaterialFrameOverrides,
		IsPBR:                  asset.IsPBR,
		Roughness:              asset.Roughness,
		Metalness:              asset.Metalness,
		Emission:               asset.Emission,
		IOR:                    asset.IOR,
		Transparency:           asset.Transparency,
		SourcePath:             asset.SourcePath,
	})
	return string(payload), err
}

func (server *AssetServer) ensureVoxelStorage() {
	if server == nil {
		return
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.voxModels == nil {
		server.voxModels = make(map[AssetId]VoxelGeometryAsset)
	}
	if server.voxModelKeys == nil {
		server.voxModelKeys = make(map[string]AssetId)
	}
	if server.voxPalettes == nil {
		server.voxPalettes = make(map[AssetId]VoxelPaletteAsset)
	}
	if server.voxPaletteKeys == nil {
		server.voxPaletteKeys = make(map[string]AssetId)
	}
	if server.voxFiles == nil {
		server.voxFiles = make(map[AssetId]*VoxFile)
	}
}

func (server *AssetServer) RegisterSharedVoxelGeometry(xbm *volume.XBrickMap, sourcePath string) AssetId {
	return server.RegisterSharedVoxelGeometryWithCacheKey("", xbm, sourcePath)
}

// Engine-thread adoption consumes only a worker-owned independent copy.
// Public registration methods keep their defensive-copy contract.
func (server *AssetServer) adoptStreamedVoxelGeometry(registration *streamedGeometryRegistration, source *volume.XBrickMap) (AssetId, bool) {
	id, _, adopted := server.adoptStreamedVoxelGeometryWithStorage(registration, source)
	return id, adopted
}

// Return private storage directly to the cache before the new asset ID escapes.
func (server *AssetServer) adoptStreamedVoxelGeometryWithStorage(registration *streamedGeometryRegistration, source *volume.XBrickMap) (AssetId, *streamedGeometryStorageDescriptor, bool) {
	if server == nil {
		return AssetId{}, nil, false
	}
	asset, rendererCopy, rendererBytes, descriptor, taken := registration.take(source)
	if !taken {
		return AssetId{}, nil, false
	}
	return server.installAdoptedStreamedVoxelGeometry(asset, rendererCopy, rendererBytes, descriptor)
}

func (server *AssetServer) adoptStreamedVoxelSourceWithStorage(registration *streamedGeometryRegistration, source *streamedGeometrySource) (AssetId, *streamedGeometryStorageDescriptor, bool) {
	if server == nil {
		return AssetId{}, nil, false
	}
	asset, rendererCopy, rendererBytes, descriptor, taken := registration.takeSource(source)
	if !taken {
		return AssetId{}, nil, false
	}
	return server.installAdoptedStreamedVoxelGeometry(asset, rendererCopy, rendererBytes, descriptor)
}

func (server *AssetServer) installAdoptedStreamedVoxelGeometry(asset VoxelGeometryAsset, rendererCopy *volume.XBrickMap, rendererBytes int64, descriptor *streamedGeometryStorageDescriptor) (AssetId, *streamedGeometryStorageDescriptor, bool) {
	server.ensureVoxelStorage()
	id := makeAssetId()
	server.mu.Lock()
	server.voxModels[id] = asset
	if rendererCopy != nil {
		if server.preparedVoxelRendererCopies == nil {
			server.preparedVoxelRendererCopies = make(map[AssetId]preparedVoxelRendererCopy)
		}
		server.preparedVoxelRendererCopies[id] = preparedVoxelRendererCopy{
			source: asset.XBrickMap, geometry: rendererCopy, bytes: rendererBytes,
		}
		server.preparedVoxelRendererCopyStats.Entries++
		server.preparedVoxelRendererCopyStats.Bytes += rendererBytes
	}
	server.mu.Unlock()
	return id, descriptor, true
}

// PreparedVoxelRendererCopyStats reports asset-owned first-admission candidates.
// Bytes exclude the registered source and renderer-owned maps; Adoptions counts
// successful runtime transfers. This is not a total memory bound.
type PreparedVoxelRendererCopyStats struct {
	Entries   int
	Bytes     int64
	Adoptions uint64
}

type preparedVoxelRendererCopy struct {
	source, geometry *volume.XBrickMap
	bytes            int64
}

// PreparedVoxelRendererCopyStats observes scalar accounting without geometry work.
func (server *AssetServer) PreparedVoxelRendererCopyStats() PreparedVoxelRendererCopyStats {
	if server == nil {
		return PreparedVoxelRendererCopyStats{}
	}
	server.mu.RLock()
	defer server.mu.RUnlock()
	return server.preparedVoxelRendererCopyStats
}

// Caller holds server.mu. Removal releases only sidecar references.
func (server *AssetServer) removePreparedVoxelRendererCopyLocked(id AssetId) (preparedVoxelRendererCopy, bool) {
	candidate, ok := server.preparedVoxelRendererCopies[id]
	if ok {
		delete(server.preparedVoxelRendererCopies, id)
		server.preparedVoxelRendererCopyStats.Entries--
		server.preparedVoxelRendererCopyStats.Bytes -= candidate.bytes
		if len(server.preparedVoxelRendererCopies) == 0 {
			server.preparedVoxelRendererCopies = nil
		}
	}
	return candidate, ok
}

// Detach once on actual admission, including shared-scope or stale-source visits.
// Validate exact registered/source identity under the server lock; content is
// inspected later on the engine thread, outside that lock.
func (server *AssetServer) takePreparedVoxelRendererCopy(id AssetId, source *volume.XBrickMap) *volume.XBrickMap {
	if server == nil {
		return nil
	}
	server.mu.Lock()
	defer server.mu.Unlock()
	candidate, ok := server.removePreparedVoxelRendererCopyLocked(id)
	asset, registered := server.voxModels[id]
	if !ok || !registered || source == nil || asset.XBrickMap != source || candidate.source != source {
		return nil
	}
	return candidate.geometry
}

// Main-thread first admission preserves defensive copying for mutable public
// sources. Even shared admission consumes and discards its visited candidate.
func (server *AssetServer) voxelRuntimeGeometryMap(id AssetId, source *volume.XBrickMap, objectScoped bool) *volume.XBrickMap {
	candidate := server.takePreparedVoxelRendererCopy(id, source)
	if objectScoped && candidate != nil && preparedVoxelRendererCopyMatches(source, candidate) {
		server.mu.Lock()
		server.preparedVoxelRendererCopyStats.Adoptions++
		server.mu.Unlock()
		return candidate
	}
	return voxelRuntimeGeometryMap(source, objectScoped)
}

// Compare every value preserved by XBrickMap.Copy, including direct edits that
// bypass revisions. Fields Copy resets (identity, GPU ownership and dirtiness)
// deliberately do not participate. Malformed pointers are never transferable.
func preparedVoxelRendererCopyMatches(source, candidate *volume.XBrickMap) bool {
	if source == nil || candidate == nil || source.AABBDirty != candidate.AABBDirty ||
		source.Revision != candidate.Revision || len(source.SectorRevisions) != len(candidate.SectorRevisions) ||
		len(source.Sectors) != len(candidate.Sectors) {
		return false
	}
	for i := 0; i < 3; i++ {
		if math.Float32bits(source.CachedMin[i]) != math.Float32bits(candidate.CachedMin[i]) ||
			math.Float32bits(source.CachedMax[i]) != math.Float32bits(candidate.CachedMax[i]) {
			return false
		}
	}
	for key, revision := range source.SectorRevisions {
		if copied, present := candidate.SectorRevisions[key]; !present || copied != revision {
			return false
		}
	}
	for key, sector := range source.Sectors {
		copied, present := candidate.Sectors[key]
		if !present || sector == nil || copied == nil || sector.Coords != copied.Coords ||
			sector.BrickMask64 != copied.BrickMask64 || len(sector.PackedBricks) != len(copied.PackedBricks) {
			return false
		}
		for i, brick := range sector.PackedBricks {
			copyBrick := copied.PackedBricks[i]
			if brick == nil || copyBrick == nil || brick.Payload != copyBrick.Payload ||
				brick.OccupancyMask64 != copyBrick.OccupancyMask64 || brick.Flags != copyBrick.Flags ||
				brick.AtlasOffset != copyBrick.AtlasOffset ||
				(brick.PrecomputedAux != nil && len(brick.PrecomputedAux) == 0) ||
				(copyBrick.PrecomputedAux != nil && len(copyBrick.PrecomputedAux) == 0) ||
				!bytes.Equal(brick.PrecomputedAux, copyBrick.PrecomputedAux) {
				return false
			}
		}
	}
	return true
}

func (server *AssetServer) SharedVoxelGeometryByCacheKey(cacheKey string) (AssetId, bool) {
	if server == nil || cacheKey == "" {
		return AssetId{}, false
	}
	server.ensureVoxelStorage()
	server.mu.RLock()
	id, ok := server.voxModelKeys[cacheKey]
	server.mu.RUnlock()
	return id, ok
}

func (server *AssetServer) RegisterSharedVoxelGeometryWithCacheKey(cacheKey string, xbm *volume.XBrickMap, sourcePath string) AssetId {
	if xbm == nil {
		return AssetId{}
	}
	server.ensureVoxelStorage()
	if cacheKey != "" {
		server.mu.RLock()
		if id, ok := server.voxModelKeys[cacheKey]; ok {
			server.mu.RUnlock()
			return id
		}
		server.mu.RUnlock()
	}
	copied := xbm.Copy()
	minB, maxB := copied.ComputeAABB()
	copied.ClearDirty()

	id := makeAssetId()
	server.mu.Lock()
	if cacheKey != "" {
		if cachedID, ok := server.voxModelKeys[cacheKey]; ok {
			server.mu.Unlock()
			return cachedID
		}
		server.voxModelKeys[cacheKey] = id
	}
	server.voxModels[id] = VoxelGeometryAsset{XBrickMap: copied, LocalMin: minB, LocalMax: maxB, BrickSize: [3]uint32{8, 8, 8}, SourcePath: sourcePath, RuntimeOwned: true}
	server.mu.Unlock()
	return id
}

func (server *AssetServer) CreateVoxelGeometry(model VoxModel, resolution float32) AssetId {
	return server.CreateVoxelGeometryFromSource(model, resolution, "")
}

func (server *AssetServer) CreateVoxelGeometryFromSource(model VoxModel, resolution float32, sourcePath string) AssetId {
	server.ensureVoxelStorage()
	if resolution != 1.0 && resolution > 0 {
		model = ScaleVoxModel(model, resolution)
	}
	cacheKey := voxelGeometryCacheKey(model, sourcePath)
	server.mu.RLock()
	if id, ok := server.voxModelKeys[cacheKey]; ok {
		server.mu.RUnlock()
		return id
	}
	server.mu.RUnlock()
	id := makeAssetId()
	server.mu.Lock()
	if cachedID, ok := server.voxModelKeys[cacheKey]; ok {
		server.mu.Unlock()
		return cachedID
	}
	server.voxModels[id] = buildVoxelGeometryAsset(model, sourcePath)
	server.voxModelKeys[cacheKey] = id
	server.mu.Unlock()
	return id
}

func (server *AssetServer) CloneVoxelGeometry(id AssetId) (AssetId, bool) {
	asset, ok := server.getVoxelGeometry(id)
	if !ok || asset.XBrickMap == nil {
		return AssetId{}, false
	}
	return server.RegisterSharedVoxelGeometry(asset.XBrickMap, asset.SourcePath), true
}

func (server *AssetServer) DeleteVoxelGeometry(id AssetId) bool {
	if server == nil || id == (AssetId{}) {
		return false
	}
	server.ensureVoxelStorage()
	server.mu.Lock()
	defer server.mu.Unlock()
	server.removePreparedVoxelRendererCopyLocked(id)
	server.revokeCompiledAssetWarmCertificateLocked(id)
	if _, ok := server.voxModels[id]; !ok {
		return false
	}
	server.removeCompiledAssetLODBindingsLocked(id)
	delete(server.voxModels, id)
	delete(server.managedVoxelGeometry, id)
	delete(server.authoredVoxelBases, id)
	for key, cachedID := range server.voxModelKeys {
		if cachedID == id {
			delete(server.voxModelKeys, key)
		}
	}
	return true
}

func (server *AssetServer) CreateVoxelModel(model VoxModel, resolution float32) AssetId {
	return server.CreateVoxelGeometryFromSource(model, resolution, "")
}

func (server *AssetServer) CreateVoxelModelFromSource(model VoxModel, resolution float32, sourcePath string) AssetId {
	return server.CreateVoxelGeometryFromSource(model, resolution, sourcePath)
}

func (server *AssetServer) CreateVoxelFile(voxFile *VoxFile) AssetId {
	server.ensureVoxelStorage()
	id := makeAssetId()
	server.mu.Lock()
	server.voxFiles[id] = voxFile
	server.mu.Unlock()
	// Automatically register all models in the file
	// (Note: some models might not be referenced by nodes, but we store them anyway)
	return id
}

func ScaleVoxModel(model VoxModel, scale float32) VoxModel {
	if scale <= 0 || scale == 1.0 {
		return model
	}
	newSizeX := uint32(math.Round(float64(float32(model.SizeX) * scale)))
	newSizeY := uint32(math.Round(float64(float32(model.SizeY) * scale)))
	newSizeZ := uint32(math.Round(float64(float32(model.SizeZ) * scale)))

	if newSizeX == 0 {
		newSizeX = 1
	}
	if newSizeY == 0 {
		newSizeY = 1
	}
	if newSizeZ == 0 {
		newSizeZ = 1
	}

	newVoxels := make([]Voxel, 0)

	if scale > 1.0 {
		// Upscaling
		for _, v := range model.Voxels {
			startX := uint32(float32(v.X) * scale)
			startY := uint32(float32(v.Y) * scale)
			startZ := uint32(float32(v.Z) * scale)
			endX := uint32(float32(v.X+1) * scale)
			endY := uint32(float32(v.Y+1) * scale)
			endZ := uint32(float32(v.Z+1) * scale)

			for x := startX; x < endX; x++ {
				for y := startY; y < endY; y++ {
					for z := startZ; z < endZ; z++ {
						if x < newSizeX && y < newSizeY && z < newSizeZ {
							newVoxels = append(newVoxels, Voxel{
								X: x, Y: y, Z: z,
								ColorIndex: v.ColorIndex,
							})
						}
					}
				}
			}
		}
	} else {
		// Downscaling with voting approximation
		type coord struct{ x, y, z uint32 }
		groups := make(map[coord]map[byte]int)
		for _, v := range model.Voxels {
			nx := uint32(float32(v.X) * scale)
			ny := uint32(float32(v.Y) * scale)
			nz := uint32(float32(v.Z) * scale)
			if nx >= newSizeX {
				nx = newSizeX - 1
			}
			if ny >= newSizeY {
				ny = newSizeY - 1
			}
			if nz >= newSizeZ {
				nz = newSizeZ - 1
			}
			c := coord{nx, ny, nz}
			if groups[c] == nil {
				groups[c] = make(map[byte]int)
			}
			groups[c][v.ColorIndex]++
		}

		for c, counts := range groups {
			maxCount := 0
			var bestColor byte
			for idx, count := range counts {
				if count > maxCount {
					maxCount = count
					bestColor = idx
				}
			}
			newVoxels = append(newVoxels, Voxel{
				X: c.x, Y: c.y, Z: c.z,
				ColorIndex: bestColor,
			})
		}
	}

	return VoxModel{
		SizeX: newSizeX, SizeY: newSizeY, SizeZ: newSizeZ,
		Voxels: newVoxels,
	}
}

func (server *AssetServer) CreateVoxelPalette(palette VoxPalette, materials []VoxMaterial) AssetId {
	return server.CreateVoxelPaletteFromSource(palette, materials, "")
}

func (server *AssetServer) CreateVoxelPaletteFromSource(palette VoxPalette, materials []VoxMaterial, sourcePath string) AssetId {
	server.ensureVoxelStorage()
	cacheKey := voxelPaletteCacheKey(palette, materials, sourcePath)
	server.mu.RLock()
	if id, ok := server.voxPaletteKeys[cacheKey]; ok {
		server.mu.RUnlock()
		return id
	}
	server.mu.RUnlock()
	id := makeAssetId()
	server.mu.Lock()
	if cachedID, ok := server.voxPaletteKeys[cacheKey]; ok {
		server.mu.Unlock()
		return cachedID
	}
	server.voxPalettes[id] = VoxelPaletteAsset{
		VoxPalette: palette,
		Materials:  materials,
		SourcePath: sourcePath,
	}
	server.voxPaletteKeys[cacheKey] = id
	server.mu.Unlock()
	return id
}

func (server *AssetServer) CreateVoxelPaletteAsset(asset VoxelPaletteAsset) AssetId {
	server.ensureVoxelStorage()
	cacheKey := voxelPaletteAssetCacheKey(asset)
	server.mu.RLock()
	if id, ok := server.voxPaletteKeys[cacheKey]; ok {
		server.mu.RUnlock()
		return id
	}
	server.mu.RUnlock()

	id := makeAssetId()
	server.mu.Lock()
	if cachedID, ok := server.voxPaletteKeys[cacheKey]; ok {
		server.mu.Unlock()
		return cachedID
	}
	server.voxPalettes[id] = asset
	server.voxPaletteKeys[cacheKey] = id
	server.mu.Unlock()
	return id
}

func (server *AssetServer) CreateSimplePalette(rgba [4]uint8) AssetId {
	var p VoxPalette
	for i := range p {
		p[i] = rgba
	}
	return server.CreateVoxelPalette(p, nil)
}

func (server *AssetServer) CreatePBRPalette(rgba [4]uint8, roughness, metalness, emission, ior float32) AssetId {
	return server.CreatePBRPaletteWithTransparency(rgba, roughness, metalness, emission, ior, 0.0)
}

func (server *AssetServer) CreatePBRPaletteWithTransparency(rgba [4]uint8, roughness, metalness, emission, ior, transparency float32) AssetId {
	var p VoxPalette
	for i := range p {
		p[i] = rgba
	}
	return server.CreateVoxelPaletteAsset(VoxelPaletteAsset{
		VoxPalette:   p,
		IsPBR:        true,
		Roughness:    roughness,
		Metalness:    metalness,
		Emission:     emission,
		IOR:          ior,
		Transparency: transparency,
	})
}
