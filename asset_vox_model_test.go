package gekko

import (
	"testing"

	"github.com/gekko3d/gekko/voxelrt/rt/volume"
)

func TestDeleteVoxelGeometryRemovesSharedCacheEntry(t *testing.T) {
	server := &AssetServer{
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}
	xbm := volume.NewXBrickMap()
	xbm.SetVoxel(0, 0, 0, 1)
	cacheKey := "test:shared-geom"

	id := server.RegisterSharedVoxelGeometryWithCacheKey(cacheKey, xbm, cacheKey)
	if _, ok := server.GetVoxelGeometry(id); !ok {
		t.Fatal("expected registered shared geometry")
	}

	if !server.DeleteVoxelGeometry(id) {
		t.Fatal("expected shared geometry delete to succeed")
	}
	if _, ok := server.GetVoxelGeometry(id); ok {
		t.Fatal("expected geometry to be removed from asset storage")
	}
	if cachedID, ok := server.voxModelKeys[cacheKey]; ok && cachedID == id {
		t.Fatal("expected geometry cache key to be removed")
	}
}
