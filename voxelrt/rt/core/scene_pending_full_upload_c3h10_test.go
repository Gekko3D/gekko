package core

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
)

func TestC3h10PendingFullUploadIdentityAndAuthority(t *testing.T) {
	var absent *VoxelObject
	if absent.SetPendingFullUpload() || absent.PendingFullUploadMap() != nil || absent.PendingFullUploadGeneration() != 0 {
		t.Fatal("nil object accepted pending upload")
	}
	absent.ClearPendingFullUpload()
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{1, 0, 0}, [3]int{65, 0, 0})
	obj.UpdateWorldAABB()
	if obj.SetPendingFullUpload() {
		t.Fatal("ordinary full display accepted redundant pending upload")
	}
	coarse := c3h8Map([3]int{0, 0, 0})
	if !obj.SetRenderLOD2(coarse) {
		t.Fatal("fixture selection")
	}
	full, transform, bounds := obj.XBrickMap, obj.Transform, obj.WorldAABB
	fullBounds, displayMatrix, displayBounds := *bounds, obj.RenderObjectToWorld(), *obj.RenderWorldBounds()
	fullSectors, fullBricks := len(full.DirtySectors), len(full.DirtyBricks)
	coarseSectors, coarseBricks := len(coarse.DirtySectors), len(coarse.DirtyBricks)
	fullStructure, coarseStructure, transformDirty := full.StructureDirty, coarse.StructureDirty, transform.Dirty
	if !obj.SetPendingFullUpload() || obj.PendingFullUploadMap() != full {
		t.Fatal("staging must borrow current authoritative full map")
	}
	first := obj.PendingFullUploadGeneration()
	if first == 0 || !obj.SetPendingFullUpload() || obj.PendingFullUploadGeneration() != first {
		t.Fatal("idempotent staging must preserve nonzero request generation")
	}
	if obj.XBrickMap != full || obj.Transform != transform || obj.WorldAABB != bounds || *bounds != fullBounds || obj.RenderVoxelMap() != coarse || obj.RenderObjectToWorld() != displayMatrix || *obj.RenderWorldBounds() != displayBounds {
		t.Fatal("pending upload changed CPU authority or display selection")
	}
	if len(full.DirtySectors) != fullSectors || len(full.DirtyBricks) != fullBricks || len(coarse.DirtySectors) != coarseSectors || len(coarse.DirtyBricks) != coarseBricks || full.StructureDirty != fullStructure || coarse.StructureDirty != coarseStructure || transform.Dirty != transformDirty {
		t.Fatal("staging mutated dirty flags")
	}
	obj.ClearPendingFullUpload()
	if obj.PendingFullUploadMap() != nil || obj.PendingFullUploadGeneration() != 0 || obj.RenderVoxelMap() != coarse {
		t.Fatal("cancel must remove staging without clearing display")
	}
	if !obj.SetPendingFullUpload() || obj.PendingFullUploadGeneration() == first || obj.PendingFullUploadGeneration() == 0 {
		t.Fatal("cancel/restage same map/revision reused request identity")
	}
	second := obj.PendingFullUploadGeneration()
	if !obj.SetRenderLOD2(coarse) || obj.PendingFullUploadMap() != nil || obj.PendingFullUploadGeneration() != 0 {
		t.Fatal("reselecting coarse must drop pending request")
	}
	if !obj.SetPendingFullUpload() || obj.PendingFullUploadGeneration() == second {
		t.Fatal("new selection reused pending request identity")
	}
	obj.ClearRenderRepresentation()
	if obj.PendingFullUploadMap() != nil || obj.PendingFullUploadGeneration() != 0 || obj.RenderVoxelMap() != full {
		t.Fatal("explicit restore retained pending request")
	}
}

func TestC3h10PendingFullUploadInvalidationAndInstanceChanges(t *testing.T) {
	for name, mutate := range map[string]func(*VoxelObject){
		"source edit":        func(o *VoxelObject) { o.XBrickMap.SetVoxel(2, 0, 0, 1) },
		"coarse edit":        func(o *VoxelObject) { o.RenderVoxelMap().SetVoxel(2, 0, 0, 1) },
		"source replace":     func(o *VoxelObject) { o.XBrickMap = c3h8Map([3]int{1, 0, 0}) },
		"nil transform":      func(o *VoxelObject) { o.Transform = nil },
		"terrain":            func(o *VoxelObject) { o.IsTerrainChunk = true },
		"planet":             func(o *VoxelObject) { o.IsPlanetTile = true },
		"voxel group":        func(o *VoxelObject) { o.VoxelAdjacencyGroupID = 1 },
		"terrain group":      func(o *VoxelObject) { o.TerrainGroupID = 1 },
		"planet group":       func(o *VoxelObject) { o.PlanetTileGroupID = 1 },
		"rejected selection": func(o *VoxelObject) { o.SetRenderLOD2(nil) },
	} {
		t.Run(name, func(t *testing.T) {
			obj := NewVoxelObject()
			obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
			if !obj.SetRenderLOD2(c3h8Map([3]int{})) || !obj.SetPendingFullUpload() {
				t.Fatal("fixture")
			}
			mutate(obj)
			if obj.PendingFullUploadMap() != nil || obj.PendingFullUploadGeneration() != 0 || obj.SetPendingFullUpload() {
				t.Fatal("invalid selection retained or accepted pending work")
			}
		})
	}
	obj := NewVoxelObject()
	obj.XBrickMap = c3h8Map([3]int{1, 0, 0})
	if !obj.SetRenderLOD2(c3h8Map([3]int{})) || !obj.SetPendingFullUpload() {
		t.Fatal("fixture")
	}
	full, generation := obj.XBrickMap, obj.PendingFullUploadGeneration()
	transform := *obj.Transform
	transform.Position = mgl32.Vec3{4, -2, 8}
	transform.Scale = mgl32.Vec3{.5, 2, 1}
	obj.Transform = &transform
	obj.Transform.Dirty = true
	obj.RenderEnabled = false
	obj.VoxelUploadOrder = 44
	obj.UpdateWorldAABB()
	if obj.PendingFullUploadMap() != full || obj.PendingFullUploadGeneration() != generation || !obj.SetPendingFullUpload() {
		t.Fatal("instance-only changes invalidated unchanged full upload")
	}
}
