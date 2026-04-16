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

func TestCreateFrameModelBuildsHollowInterior(t *testing.T) {
	server := &AssetServer{
		voxModels:      make(map[AssetId]VoxelGeometryAsset),
		voxModelKeys:   make(map[string]AssetId),
		voxPalettes:    make(map[AssetId]VoxelPaletteAsset),
		voxPaletteKeys: make(map[string]AssetId),
		voxFiles:       make(map[AssetId]*VoxFile),
	}

	id := server.CreateFrameModel(10, 4, 8, 2, 1)
	geometry, ok := server.GetVoxelGeometry(id)
	if !ok || geometry.XBrickMap == nil {
		t.Fatal("expected frame geometry asset")
	}

	if found, value := geometry.XBrickMap.GetVoxel(0, 0, 0); !found || value != 1 {
		t.Fatalf("expected outer frame voxel at origin, got found=%v value=%d", found, value)
	}
	if found, value := geometry.XBrickMap.GetVoxel(1, 2, 6); !found || value != 1 {
		t.Fatalf("expected near-edge frame voxel, got found=%v value=%d", found, value)
	}
	if found, _ := geometry.XBrickMap.GetVoxel(3, 1, 3); found {
		t.Fatal("expected frame interior to remain hollow")
	}
}
